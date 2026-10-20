package backend

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveEmptySaveTakesOnlyAnEntryThatHoldsNothing covers the one removal a
// store makes: an empty entry under a key with one name on disk goes, and
// everything else stays — an entry with a byte in it, a key the encoding
// changes, a key that is a directory of others, and a key that is not there.
func TestRemoveEmptySaveTakesOnlyAnEntryThatHoldsNothing(t *testing.T) {
	root := t.TempDir()
	directory := NewDirectorySaveStore(root)
	memory, err := NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]interface {
		SaveStore
		SaveReader
	}{"directory store": directory, "memory store": memory} {
		t.Run(name, func(t *testing.T) {
			for key, data := range map[string][]byte{
				"fs/option.dat":    {},
				"fs/kept.dat":      []byte("kept"),
				"fs/Upper.dat":     {},
				"fs/folder/inside": {},
			} {
				if err := store.StoreSave(key, data); err != nil {
					t.Fatal(err)
				}
			}
			removed, err := RemoveEmptySave(store, "fs/option.dat")
			if err != nil || !removed {
				t.Fatalf("the empty entry was not removed: %t, %v", removed, err)
			}
			if _, present, err := store.ReadSave("fs/option.dat"); present || err != nil {
				t.Fatalf("the removed entry still reads: %t, %v", present, err)
			}
			for _, key := range []string{"fs/kept.dat", "fs/folder", "fs/missing.dat"} {
				if removed, err := RemoveEmptySave(store, key); removed || err != nil {
					t.Errorf("%s was removed: %t, %v", key, removed, err)
				}
			}
			if data, present, _ := store.ReadSave("fs/kept.dat"); !present || string(data) != "kept" {
				t.Error("an entry with something in it did not survive")
			}
			if _, present, _ := store.ReadSave("fs/folder/inside"); !present {
				t.Error("an entry below a directory key did not survive")
			}
			if name == "directory store" {
				// A key the encoding changes has more than one name an earlier
				// build may have left it under, and stays.
				if removed, err := RemoveEmptySave(store, "fs/Upper.dat"); removed || err != nil {
					t.Errorf("a key the encoding changes was removed: %t, %v", removed, err)
				}
				if _, present, _ := store.ReadSave("fs/Upper.dat"); !present {
					t.Error("the empty entry under an encoded name did not survive")
				}
				if _, err := os.Stat(filepath.Join(root, "fs", "option.dat")); !os.IsNotExist(err) {
					t.Errorf("the removed entry's file is still on disk: %v", err)
				}
			}
		})
	}
}

// TestRemoveEmptySaveLeavesAStoreThatCannotAlone covers a store with no
// removal: nothing is removed and nothing fails.
func TestRemoveEmptySaveLeavesAStoreThatCannotAlone(t *testing.T) {
	memory, err := NewMemorySaveStore([]SaveEntry{{Key: "fs/option.dat", Data: []byte{}}})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := struct{ SaveStore }{memory}
	if removed, err := RemoveEmptySave(wrapped, "fs/option.dat"); removed || err != nil {
		t.Fatalf("a store without removal removed %t, %v", removed, err)
	}
	if _, present := memory.LoadSave("fs/option.dat"); !present {
		t.Fatal("the entry went")
	}
}
