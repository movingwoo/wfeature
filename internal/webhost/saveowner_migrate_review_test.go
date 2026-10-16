package webhost

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/session"
)

// The SKT owner rule and this Host's are written twice, once in each package,
// and must agree: an owner the platform answers that the Host refuses is a
// title started with nowhere to keep its saves.
func TestSKTSaveOwnersAreOwnersTheHostAccepts(t *testing.T) {
	names := []string{
		"Title: Part 2", `a*b?c"d<e>f|g`, "tab\there", "‮eman", "trailing.", " spaced ",
		strings.Repeat("가", 30), strings.Repeat("x", 65), "..", ".", "back\\slash", "DEL\x7f",
	}
	for _, name := range names {
		owner := skt.SaveOwner(skt.Descriptor{Name: name})
		if owner == "" || ownerRejected(owner) {
			t.Errorf("name %q: SKT owner %q is refused by the Host", name, owner)
		}
	}
}

// Saves the command line kept under a raw owner the Host refused move to the
// owner this build names before a start claims the directory, below the same
// platform root saveDirectory joins the owner to.
func TestAStartMovesSavesKeptUnderTheEarlierOwnerName(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the earlier owner name cannot exist on this file system")
	}
	archive := sktArchiveNamed(t, "Puzzle: Deluxe")
	summary, err := session.Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SaveOwner == "Puzzle: Deluxe" || ownerRejected(summary.SaveOwner) {
		t.Fatalf("owner %q was not made into one the Host accepts", summary.SaveOwner)
	}
	data := t.TempDir()
	server := newTestServer(t, Options{SaveRoot: filepath.Join(data, "ktf")})
	legacy := backend.NewDirectorySaveStore(filepath.Join(data, "skt", "Puzzle: Deluxe"))
	if err := legacy.StoreSave("rms/save", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	server.migrateSaveOwner(archive, summary.Platform)

	directory := server.saveDirectory(summary.Platform, summary.SaveOwner)
	if directory == "" {
		t.Fatal("no save directory for the migrated owner")
	}
	got, err := os.ReadFile(filepath.Join(directory, "rms", "save"))
	if err != nil || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("migrated save = %v, %v; want [1 2 3]", got, err)
	}
	if _, err := os.Stat(filepath.Join(data, "skt", "Puzzle: Deluxe")); !os.IsNotExist(err) {
		t.Fatalf("the earlier owner directory is still there: %v", err)
	}
}

// sktArchiveNamed answers the RecordStore fixture MIDlet packaged the way an
// SKT title ships, a .msd beside its JAR, under another name.
func sktArchiveNamed(t *testing.T, name string) []byte {
	t.Helper()
	jar, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "recordstore.jar"))
	if err != nil {
		t.Fatal(err)
	}
	descriptor := "MIDlet-Name: " + name + "\r\nMIDlet-1: " + name + ", , RecordStoreMIDlet\r\n"
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, file := range []struct {
		name    string
		content []byte
	}{{"game.msd", []byte(descriptor)}, {"game.jar", jar}} {
		entry, err := writer.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(file.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
