package lgt

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Closing a session is what a Host does to every game it holds when it stops,
// and on this platform it is the last moment for a file the title wrote and
// never closed: those bytes are in the handle's buffer and nowhere else. A
// Host that ends a game without closing it loses them, which is why a stopping
// server closes attached games as well as parked ones.
func TestClosingASessionStoresAnUnclosedWrite(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "saves")
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{
		Width: 16, Height: 8,
		SaveStore: backend.NewDirectorySaveStore(directory),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.files = map[uint32]*openFile{
		1: {name: "progress.dat", data: []byte("written and left open"), writable: true, dirty: true},
	}
	session := &Session{client: client, archive: archive}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Read through a store of its own, the way the next run of the game would.
	key, err := fileSaveKey("progress.dat")
	if err != nil {
		t.Fatalf("the file name has no save key: %v", err)
	}
	stored, found, err := backend.ReadSave(backend.NewDirectorySaveStore(directory), key)
	if err != nil || !found || !bytes.Equal(stored, []byte("written and left open")) {
		t.Fatalf("after the close the store holds %q (found=%t, err=%v), want the bytes the title wrote", stored, found, err)
	}
}
