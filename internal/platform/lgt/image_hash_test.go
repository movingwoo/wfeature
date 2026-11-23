package lgt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/armcheat"
)

// The identity a cheat table is keyed by has to survive a repack, because that
// is the whole difference between a hash of the loaded image and a hash of the
// file it arrived in. One title reaches this project in more than one
// container; the addresses a table holds are true of the module in all of them.
func TestSessionImageHashNamesTheModuleRatherThanTheArchive(t *testing.T) {
	jar := fixtureJAR(t)
	one := zipOf(t, map[string][]byte{
		"app_info":     []byte("AID=0102ABCD\nPID=PF000001\nMClass=Fixture\nName=Fixture Title\n"),
		"0102ABCD.jar": jar,
	})
	// The same module in a container that says something else about it, which
	// is what a repack is.
	two := zipOf(t, map[string][]byte{
		"app_info":     []byte("AID=0102ABCD\nPID=PF000001\nMClass=Fixture\nName=Fixture Title, repacked\n"),
		"0102ABCD.jar": jar,
	})
	if bytes.Equal(one, two) {
		t.Fatal("the two archives are the same bytes, so this proves nothing")
	}

	first := startForHash(t, one)
	second := startForHash(t, two)
	if first == "" {
		t.Fatal("no image hash")
	}
	if first != second {
		t.Fatalf("repacking changed the image hash: %s then %s", first, second)
	}

	archive, err := Open(one)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Module)
	if want := hex.EncodeToString(sum[:]); first != want {
		t.Fatalf("image hash is %s, want the module's own %s", first, want)
	}
}

// Opening the console used to hash edits made to Archive.Module after load.
func TestCheatTableKeepsLoadedImageHash(t *testing.T) {
	session, err := StartSession(t.Context(), fixtureArchive(t), SessionOptions{Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(t.Context())

	module := session.Archive().Module
	loaded := sha256.Sum256(module)
	module[0] ^= 1
	state, err := session.client.captureClientState()
	if err != nil {
		t.Fatal(err)
	}
	key := session.CheatConsole().Session().TableKey()
	if state.ModuleHash != loaded || key.Image != hex.EncodeToString(loaded[:]) {
		t.Fatalf("checkpoint module %x, cheat table image %s, want %x", state.ModuleHash, key.Image, loaded)
	}
}

// A session that was never started answers nothing rather than the hash of an
// empty slice, so a caller can tell "no image" from "an image of no bytes".
func TestSessionImageHashIsEmptyWithoutAnArchive(t *testing.T) {
	for _, session := range []*Session{nil, {}, {client: &Client{}}} {
		if got := session.ImageHash(); got != "" {
			t.Fatalf("unloaded image hash = %q", got)
		}
	}
	if got := armcheat.ImageHash(nil); got != "" {
		t.Fatalf("image hash of no bytes is %q", got)
	}
}

func startForHash(t *testing.T, data []byte) string {
	t.Helper()
	session, err := StartSession(context.Background(), data, SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	return session.ImageHash()
}
