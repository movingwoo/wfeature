package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRemoveEmptySaveTakesOnlyAnEntryThatHoldsNothing covers the one removal a
// store makes: an empty entry goes, under its plain name or the one the
// encoding gives it, and everything else stays — an entry with a byte in it, a
// key that is a directory of others, and a key that is not there.
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
			if removed, err := RemoveEmptySave(store, "fs/Upper.dat"); !removed || err != nil {
				t.Errorf("the empty entry under a key the encoding changes was not removed: %t, %v", removed, err)
			}
			if _, present, _ := store.ReadSave("fs/Upper.dat"); present {
				t.Error("the removed entry under an encoded name still reads")
			}
			if name == "directory store" {
				for _, file := range []string{"option.dat", encodeSaveName("Upper.dat")} {
					if _, err := os.Stat(filepath.Join(root, "fs", file)); !os.IsNotExist(err) {
						t.Errorf("the removed entry's file %s is still on disk: %v", file, err)
					}
				}
			}
		})
	}
}

// TestRemoveEmptySaveTakesEveryNameOfTheKeyOrNone covers a key an earlier
// build also left under its plain name in an encoded folder. A read takes the
// later of the two. When both are empty, both go and the key reads as no
// entry; when the older one holds a save, the empty one in front of it stays,
// because taking it away would hand a read that older save.
func TestRemoveEmptySaveTakesEveryNameOfTheKeyOrNone(t *testing.T) {
	for _, test := range []struct {
		name    string
		earlier []byte
		removed bool
	}{
		{name: "both empty", earlier: []byte{}, removed: true},
		{name: "an older save behind the empty one", earlier: []byte("older"), removed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := NewDirectorySaveStore(root)
			if err := store.StoreSave("fs/Upper.dat", nil); err != nil {
				t.Fatal(err)
			}
			encoded := filepath.Join(root, "fs", encodeSaveName("Upper.dat"))
			plain := filepath.Join(root, "fs", "Upper.dat")
			if encoded == plain {
				t.Fatal("the key's encoded name is its plain one")
			}
			if err := os.WriteFile(plain, test.earlier, 0o644); err != nil {
				t.Fatal(err)
			}
			older := time.Now().Add(-time.Hour)
			if err := os.Chtimes(plain, older, older); err != nil {
				t.Fatal(err)
			}
			if data, present, err := store.ReadSave("fs/Upper.dat"); err != nil || !present || len(data) != 0 {
				t.Fatalf("before the removal the key read %q (%t, %v), want the later, empty entry", data, present, err)
			}
			removed, err := store.RemoveEmptySave("fs/Upper.dat")
			if err != nil || removed != test.removed {
				t.Fatalf("removed = %t (%v), want %t", removed, err, test.removed)
			}
			data, present, err := store.ReadSave("fs/Upper.dat")
			if err != nil {
				t.Fatal(err)
			}
			if test.removed {
				if present {
					t.Errorf("the key still reads %q after both of its empty names went", data)
				}
				for _, path := range []string{encoded, plain} {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Errorf("%s is still on disk: %v", path, err)
					}
				}
				return
			}
			if !present || len(data) != 0 {
				t.Errorf("the key reads %q (%t), want the empty entry still in front of the older save", data, present)
			}
			if kept, err := os.ReadFile(plain); err != nil || string(kept) != "older" {
				t.Errorf("the older save is %q (%v)", kept, err)
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

// TestRemoveEmptySaveLeavesAWin32NameOtherKeysRead covers a file an earlier
// build stored on Windows under the name Win32 made of a key that is not UTF-8:
// every key Win32 spelled the same reads it, so taking it away for one of them
// takes it from the others. It stays, empty or not.
func TestRemoveEmptySaveLeavesAWin32NameOtherKeysRead(t *testing.T) {
	win32 := saveNamesWin32
	saveNamesWin32 = true
	t.Cleanup(func() { saveNamesWin32 = win32 })
	key, other := "fs/\xbc\xbc\xc0\xcc.dat", "fs/\xb0\xa1\xb3\xaa.dat"
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{"owner/fs/����.dat="})
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	wantSaves(t, store, map[string]string{key: "", other: ""})
	if removed, err := store.RemoveEmptySave(key); removed || err != nil {
		t.Fatalf("the file Win32 named was removed for one of its keys: %t, %v", removed, err)
	}
	wantSaves(t, store, map[string]string{key: "", other: ""})
}
