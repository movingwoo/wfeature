package backend

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A save is written whole every time a game commits, so a write that stops
// halfway leaves the front of the new save on the back of the old one — which
// the game reads as a save rather than as damage. The store replaces the file
// instead: what a reader sees is one version or the other, never a splice of
// the two.
func TestASaveIsReplacedRatherThanOverwritten(t *testing.T) {
	store := NewDirectorySaveStore(t.TempDir())
	const key = "db/slot.dat"

	// A long save followed by a short one. An overwrite in place would leave
	// the tail of the first behind.
	if err := store.StoreSave(key, bytes.Repeat([]byte("A"), 4096)); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave(key, []byte("B")); err != nil {
		t.Fatal(err)
	}
	stored, ok := store.LoadSave(key)
	if !ok || string(stored) != "B" {
		t.Fatalf("save = %q (%v), want the second write alone", stored, ok)
	}
}

// The temporary file a replacement goes through is the store's business and
// nobody else's: it is removed by the rename, so what is left in the tree is
// the entries a guest wrote.
func TestReplacingASaveLeavesNoTemporaryFileBehind(t *testing.T) {
	root := t.TempDir()
	store := NewDirectorySaveStore(root)
	for _, key := range []string{"db/slot.dat", "db/slot.dat", "rms/scores"} {
		if err := store.StoreSave(key, []byte("save")); err != nil {
			t.Fatal(err)
		}
	}
	var left []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasPrefix(entry.Name(), ".") {
			left = append(left, entry.Name())
		}
		return nil
	})
	if len(left) != 0 {
		t.Fatalf("the tree kept %v", left)
	}
}

// A key can be longer than a file system's own name limit, and the temporary
// file's name is the entry's plus decoration. The decoration must not be what
// pushes a save over that limit, so it is cut — on a rune boundary, because
// macOS refuses a name that is not valid UTF-8.
func TestALongEntryNameStillWrites(t *testing.T) {
	store := NewDirectorySaveStore(t.TempDir())
	// 200 bytes of Korean: past the shortening, and every rune three bytes so
	// a cut in the wrong place is a name the file system would refuse.
	key := "db/" + strings.Repeat("저", 66)
	if err := store.StoreSave(key, []byte("save")); err != nil {
		t.Fatal(err)
	}
	if stored, ok := store.LoadSave(key); !ok || string(stored) != "save" {
		t.Fatalf("save = %q (%v)", stored, ok)
	}
}

// The point of the replacement, stated as the race it prevents: a reader that
// arrives while a writer is in the middle of committing sees one whole save or
// the other, never the two spliced together. Run under -race this is also what
// says the store needs no lock of its own.
func TestAReaderNeverSeesHalfOfASave(t *testing.T) {
	store := NewDirectorySaveStore(t.TempDir())
	const key = "db/slot.dat"
	first := bytes.Repeat([]byte("A"), 1<<16)
	second := bytes.Repeat([]byte("B"), 1<<16)
	if err := store.StoreSave(key, first); err != nil {
		t.Fatal(err)
	}

	var writers sync.WaitGroup
	done := make(chan struct{})
	writers.Add(1)
	go func() {
		defer writers.Done()
		for round := 0; round < 200; round++ {
			save := first
			if round%2 == 1 {
				save = second
			}
			if err := store.StoreSave(key, save); err != nil {
				t.Error(err)
				break
			}
		}
		close(done)
	}()

	for {
		select {
		case <-done:
			writers.Wait()
			return
		default:
		}
		stored, ok := store.LoadSave(key)
		if !ok {
			// The name never stops existing: a rename replaces it in one
			// step rather than removing it and putting it back.
			t.Fatal("the save was missing while it was being replaced")
		}
		if !bytes.Equal(stored, first) && !bytes.Equal(stored, second) {
			t.Fatalf("a read saw %d bytes that are neither save", len(stored))
		}
	}
}
