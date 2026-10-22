package backend

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A listing is read back from the names on disk, so the keys have to come
// back as they were stored: case spelled out in the path, a dotted index kept,
// and a directory's entries apart from its siblings'.
func TestDirectorySaveStoreListsStoredKeys(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)

	keys, listed, err := ListSaves(store, "db")
	if err != nil || !listed || len(keys) != 0 {
		t.Fatalf("a store nothing was saved to lists %q, %t, %v; want nothing", keys, listed, err)
	}
	if _, err := os.Stat(root); err == nil {
		// Reading does not make the directory; a listing should not either.
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if entry.Name() != ".lock" {
				t.Fatalf("listing an empty store left %q under its root", entry.Name())
			}
		}
	}

	for key, data := range map[string][]byte{
		"db/save.dat":    []byte("a"),
		"db/Slots/Slot1": []byte("b"),
		"db/slots/slot1": []byte("c"),
		"fs/option":      []byte("d"),
		"rms/.index":     []byte("e"),
	} {
		if err := store.StoreSave(key, data); err != nil {
			t.Fatal(err)
		}
	}
	keys, listed, err = ListSaves(store, "db")
	if err != nil || !listed {
		t.Fatalf("listing db: %v, listed %t", err, listed)
	}
	if want := []string{"db/Slots/Slot1", "db/save.dat", "db/slots/slot1"}; !slices.Equal(keys, want) {
		t.Fatalf("db lists %q, want %q", keys, want)
	}
	for _, root := range []string{"", "/", "./"} {
		keys, _, err = ListSaves(store, root)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"db/Slots/Slot1", "db/save.dat", "db/slots/slot1", "fs/option", "rms/.index"}; !slices.Equal(keys, want) {
			t.Fatalf("%q lists %q, want %q", root, keys, want)
		}
	}
	if keys, _, err = ListSaves(store, "db/slots"); err != nil || !slices.Equal(keys, []string{"db/slots/slot1"}) {
		t.Fatalf("db/slots lists %q, %v", keys, err)
	}
	if _, _, err = ListSaves(store, "db/../fs"); err == nil {
		t.Fatal("a directory that climbs out of its key was listed")
	}
}

// A store that cannot list says so through ListSaves rather than failing,
// whether it lacks the method or stands in front of a store that does.
func TestListSavesWithoutAListingStore(t *testing.T) {
	if keys, listed, err := ListSaves(plainSaveStore{}, "db"); keys != nil || listed || err != nil {
		t.Fatalf("a store without ListSaves answered %q, %t, %v", keys, listed, err)
	}
	if keys, listed, err := ListSaves(frontSaveStore{}, "db"); keys != nil || listed || err != nil {
		t.Fatalf("a store answering ErrSavesNotListed answered %q, %t, %v", keys, listed, err)
	}
}

func TestMemoryAndDetachedSaveStoresList(t *testing.T) {
	memory, err := NewMemorySaveStore([]SaveEntry{{Key: "db/b", Data: []byte("1")}, {Key: "db/a", Data: []byte("2")}, {Key: "fs/c", Data: []byte("3")}})
	if err != nil {
		t.Fatal(err)
	}
	if keys, listed, err := ListSaves(memory, "db"); err != nil || !listed || !slices.Equal(keys, []string{"db/a", "db/b"}) {
		t.Fatalf("memory store lists %q, %t, %v", keys, listed, err)
	}

	detached := NewDetachedSaveStore()
	if keys, listed, err := ListSaves(detached, "db"); err != nil || !listed || len(keys) != 0 {
		t.Fatalf("detached store lists %q, %t, %v", keys, listed, err)
	}
	if detached.Calls() != 1 || detached.FirstCall() != `list "db"` {
		t.Fatalf("a listing reached the placeholder as %d calls, the first %q", detached.Calls(), detached.FirstCall())
	}
}

type plainSaveStore struct{}

func (plainSaveStore) LoadSave(string) ([]byte, bool) { return nil, false }
func (plainSaveStore) StoreSave(string, []byte) error { return errors.New("read-only") }

type frontSaveStore struct{ plainSaveStore }

func (frontSaveStore) ListSaves(string) ([]string, error) { return nil, ErrSavesNotListed }
