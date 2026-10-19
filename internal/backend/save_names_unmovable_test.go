package backend

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// failRenamesOf makes every rename a conversion makes of a path ending in one
// of the names fail as a locked file's does, until the test ends.
func failRenamesOf(t *testing.T, names ...string) {
	t.Helper()
	original := saveNamesRename
	t.Cleanup(func() { saveNamesRename = original })
	saveNamesRename = func(from, to string) error {
		for _, name := range names {
			if strings.HasSuffix(from, string(filepath.Separator)+name) {
				return &os.LinkError{Op: "rename", Old: from, New: to, Err: os.ErrPermission}
			}
		}
		return original(from, to)
	}
}

// countRenames counts the renames the store makes from here on, until the
// test ends.
func countRenames(t *testing.T) *int {
	t.Helper()
	original := saveNamesRename
	t.Cleanup(func() { saveNamesRename = original })
	renames := new(int)
	saveNamesRename = func(from, to string) error {
		*renames++
		return original(from, to)
	}
	return renames
}

// retrySaveNamesAtOnce has every operation take a conversion that could not
// be carried out up again, as it does once saveNamesRetry has passed.
func retrySaveNamesAtOnce(t *testing.T) {
	t.Helper()
	retry := saveNamesRetry
	saveNamesRetry = 0
	t.Cleanup(func() { saveNamesRetry = retry })
}

// stoppedConversion is the record of a conversion that stopped in a state,
// with the names it moves, as a test builds it by hand.
func stoppedConversion(state saveNamesState, plan ...string) string {
	record := "owner/" + saveNamesRecord + "=" + saveNamesHeader + "\n"
	for _, known := range saveNamesStates {
		if known.state == state {
			record += known.word + "\n"
		}
	}
	for _, path := range plan {
		record += strconv.Quote(path) + "\n"
	}
	return record
}

// One name of an earlier build's folder that cannot be renamed — a file
// locked in the Finder, or one another program holds open on Windows — used
// to stop the conversion there, and every later operation on the folder failed
// finishing it: every save of the game unreadable, and none writable, where
// the earlier build had read and written them all. The names that had moved
// go back, the record goes, and the folder is read as the earlier build left
// it: every save readable, a write refused, and the conversion taken up again
// once the name can move.
func TestANameThatCannotMoveUndoesTheConversion(t *testing.T) {
	retrySaveNamesAtOnce(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	state := []string{
		"owner/rms/Option=options",
		"owner/rms/Slot=slot progress",
		"owner/rms/.index=Option\nSlot",
	}
	leftoverBuild(t, parent, state)
	original := saveNamesRename
	failRenamesOf(t, "Slot")
	store := NewDirectorySaveStore(root)
	saves := map[string]string{"rms/Option": "options", "rms/Slot": "slot progress", "rms/.index": "Option\nSlot"}
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock="), "after the undone conversion")
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatalf("the export: %v", err)
	}
	wantEntries(t, listed, saves, "the export")
	if err := store.StoreSave("rms/Option", []byte("refused")); err == nil {
		t.Fatal("a write into the folder that could not be converted was taken")
	}
	saveNamesRename = original
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, []string{
		encodedRecord,
		"owner/rms/#Option=options",
		"owner/rms/#Slot=slot progress",
		"owner/rms/.index=Option\nSlot",
		leftoverReserved + "/lock=",
	}, "after the name could move")
}

// A directory that cannot be renamed undoes the conversion the same way: the
// names below it that had moved to their temporary names move back, and the
// whole of it is read as the earlier build left it.
func TestADirectoryThatCannotMoveUndoesTheConversion(t *testing.T) {
	retrySaveNamesAtOnce(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	state := []string{
		"owner/Data/Other=other",
		"owner/Data/Save=progress",
		"owner/Top=top",
	}
	leftoverBuild(t, parent, state)
	original := saveNamesRename
	failRenamesOf(t, "Data")
	store := NewDirectorySaveStore(root)
	saves := map[string]string{"Data/Other": "other", "Data/Save": "progress", "Top": "top"}
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock="), "after the conversion that could not move Data")
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatalf("the export: %v", err)
	}
	wantEntries(t, listed, saves, "the export")
	saveNamesRename = original
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, []string{
		encodedRecord,
		"owner/#Data/#Other=other",
		"owner/#Data/#Save=progress",
		"owner/#Top=top",
		leftoverReserved + "/lock=",
	}, "after the directory could move")
}

// No earlier name is left in a folder the record calls encoded. On a volume
// that ignores case — the default on macOS, iOS and Windows — an earlier name
// a '#' would have changed is the encoded name of another key there. Left in
// place while the rest converted, "#save" stood on the name "Save" was to
// take, the second pass failed on it, and every operation on the folder failed
// from then on, the lock long gone, with Save's progress under a temporary
// name. Left in place, "Save" was the file the key "save" was read from and
// written to, and the first write of "save" replaced Save's progress.
func TestANameThatCannotMoveIsNotLeftWhereAnotherKeyIsWritten(t *testing.T) {
	retrySaveNamesAtOnce(t)
	t.Run("an earlier name on another key's encoded name", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "owner")
		state := []string{"owner/rms/#save=hash save", "owner/rms/Save=upper save"}
		leftoverBuild(t, parent, state)
		original := saveNamesRename
		failRenamesOf(t, "#save")
		store := NewDirectorySaveStore(root)
		saves := map[string]string{"rms/#save": "hash save", "rms/Save": "upper save"}
		wantSaves(t, store, saves)
		leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock="), "while #save could not move")
		saveNamesRename = original
		wantSaves(t, store, saves)
		if err := store.StoreSave("rms/Save", []byte("later")); err != nil {
			t.Fatalf("a write once the folder converted: %v", err)
		}
		wantSaves(t, store, map[string]string{"rms/#save": "hash save", "rms/Save": "later"})
	})
	t.Run("an earlier name on its lowercase twin's name", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "owner")
		state := []string{"owner/rms/Save=upper save", "owner/rms/index=Save"}
		leftoverBuild(t, parent, state)
		original := saveNamesRename
		failRenamesOf(t, "Save")
		store := NewDirectorySaveStore(root)
		if data, found, err := store.ReadSave("rms/save"); err != nil || found {
			t.Fatalf("rms/save, which has no save, read %q (found %t, %v)", data, found, err)
		}
		if err := store.StoreSave("rms/save", []byte("lower")); err == nil {
			t.Fatal("a write into the folder that could not be converted was taken")
		}
		wantSaves(t, store, map[string]string{"rms/Save": "upper save", "rms/index": "Save"})
		saveNamesRename = original
		if err := store.StoreSave("rms/save", []byte("lower")); err != nil {
			t.Fatalf("a write once the folder converted: %v", err)
		}
		wantSaves(t, store, map[string]string{"rms/Save": "upper save", "rms/save": "lower", "rms/index": "Save"})
	})
}

// A conversion that stopped with a name under its temporary one, when the
// name could not move again and so had to be undone, found the earlier name
// taken by something put there while it was stopped — an earlier build that
// cannot see the temporary name wrote the key again, a copy or a sync client
// brought it back. The undo renamed the older file over it, and the later
// progress was gone. What is there is later, and it stays: the moved file
// goes, and a moved directory fills in only the names the one there lacks.
func TestAnUndoNeverWritesOverANamePutBackWhileItWasStopped(t *testing.T) {
	retrySaveNamesAtOnce(t)
	t.Run("a file", func(t *testing.T) {
		parent := t.TempDir()
		leftoverBuild(t, parent, []string{
			stoppedConversion(saveNamesMoving, "rms/Save", "rms/a#Top"),
			"owner/rms/" + saveNamesTemporary(0) + "=older progress",
			"owner/rms/Save=later progress",
			"owner/rms/a#Top=top",
		})
		failRenamesOf(t, "a#Top")
		wantSaves(t, NewDirectorySaveStore(filepath.Join(parent, "owner")), map[string]string{
			"rms/Save": "later progress", "rms/a#Top": "top",
		})
		leftoverExpect(t, parent, []string{
			"owner/rms/Save=later progress", "owner/rms/a#Top=top", leftoverReserved + "/lock=",
		}, "after the undo")
	})
	t.Run("a directory", func(t *testing.T) {
		parent := t.TempDir()
		moved := "owner/" + saveNamesTemporary(1) + "/"
		leftoverBuild(t, parent, []string{
			stoppedConversion(saveNamesMoving, "Data/Save", "Data", "a#Top"),
			moved + saveNamesTemporary(0) + "=older progress",
			moved + "other=other",
			"owner/Data/Save=later progress",
			"owner/a#Top=top",
		})
		failRenamesOf(t, "a#Top")
		wantSaves(t, NewDirectorySaveStore(filepath.Join(parent, "owner")), map[string]string{
			"Data/Save": "later progress", "Data/other": "other", "a#Top": "top",
		})
		leftoverExpect(t, parent, []string{
			"owner/Data/Save=later progress", "owner/Data/other=other", "owner/a#Top=top", leftoverReserved + "/lock=",
		}, "after the undo")
	})
}

// A conversion that can neither finish nor be undone — a temporary name that
// cannot move again, in either direction — used to fail every operation on
// the folder until it could. Every save is read through the record instead,
// wherever the conversion left it; a write, an export and a checkpoint are
// refused, as only a finished conversion says where each name belongs; and
// the next operation once the name can move finishes it.
func TestAConversionThatCanNeitherFinishNorBeUndoneIsReadThroughItsRecord(t *testing.T) {
	retrySaveNamesAtOnce(t)
	for _, stopped := range []struct {
		name   string
		state  saveNamesState
		top    string
		locked []string
	}{
		// The first pass met a name it cannot move, and the undo a temporary
		// name it cannot move back.
		{"moving back", saveNamesMoving, "a#Top", []string{"a#Top", saveNamesTemporary(0)}},
		// The second pass met a temporary name it cannot move on.
		{"placing", saveNamesPlacing, saveNamesTemporary(1), []string{saveNamesTemporary(0)}},
	} {
		t.Run(stopped.name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "owner")
			leftoverBuild(t, parent, []string{
				stoppedConversion(stopped.state, "rms/Save", "rms/a#Top"),
				"owner/rms/" + saveNamesTemporary(0) + "=progress",
				"owner/rms/" + stopped.top + "=top",
				"owner/rms/slot=slot",
			})
			original := saveNamesRename
			failRenamesOf(t, stopped.locked...)
			store := NewDirectorySaveStore(root)
			saves := map[string]string{"rms/Save": "progress", "rms/a#Top": "top", "rms/slot": "slot"}
			wantSaves(t, store, saves)
			err := store.StoreSave("rms/slot", []byte("refused"))
			if err == nil || !errors.Is(err, os.ErrPermission) {
				t.Fatalf("a write into the folder stopped mid-conversion answered %v", err)
			}
			if _, err := ReadSaveTree(root); err == nil {
				t.Fatal("an export of the folder stopped mid-conversion listed part of it")
			}
			if _, err := store.SnapshotSaves(); err == nil {
				t.Fatal("a checkpoint of the folder stopped mid-conversion listed part of it")
			}
			saveNamesRename = original
			wantSaves(t, store, saves)
			leftoverExpect(t, parent, []string{
				encodedRecord,
				"owner/rms/#Save=progress",
				"owner/rms/a###Top=top",
				"owner/rms/slot=slot",
				leftoverReserved + "/lock=",
			}, "after the conversion finished")
		})
	}
}

// Each try of a conversion a name stops costs a fsynced record and every name
// renamed twice, and every operation made one: a title reading its save every
// frame paid it every frame, and a synced folder saw its names renamed back
// and forth. One that could not be carried out is not tried again until
// saveNamesRetry has passed or the folder has changed.
func TestAConversionThatCouldNotBeCarriedOutIsNotTriedEveryOperation(t *testing.T) {
	retry := saveNamesRetry
	saveNamesRetry = time.Hour
	t.Cleanup(func() { saveNamesRetry = retry })
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	leftoverBuild(t, parent, []string{"owner/rms/Alpha=alpha", "owner/rms/a#Top=top", "owner/rms/Beta=beta"})
	failRenamesOf(t, "a#Top")
	renames := countRenames(t)
	store := NewDirectorySaveStore(root)
	saves := map[string]string{"rms/Alpha": "alpha", "rms/a#Top": "top", "rms/Beta": "beta"}
	wantSaves(t, store, saves)
	if *renames == 0 {
		t.Fatal("the first operation did not try the conversion")
	}
	*renames = 0
	for range 20 {
		wantSaves(t, store, saves)
	}
	if *renames != 0 {
		t.Fatalf("operations within the retry interval renamed %d names", *renames)
	}
	if err := os.WriteFile(filepath.Join(root, "added"), []byte("by hand"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantSaves(t, store, saves)
	if *renames == 0 {
		t.Fatal("the folder changed and the conversion was not tried again")
	}
	*renames = 0
	saveNamesRetry = 0
	wantSaves(t, store, saves)
	if *renames == 0 {
		t.Fatal("the retry interval passed and the conversion was not tried again")
	}
}

// A key spelled as the record, or as a temporary name of a conversion, is no
// earlier name of the record or of the save under that temporary name. Looked
// up as one in an encoded folder, a read of ".wfeature-names#1" through the
// save API answered the record as a save and moved it to the key's encoded
// name, and the folder's names read as an earlier build's from then on.
func TestAKeySpelledAsTheRecordIsStoredLikeAnyOther(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	leftoverBuild(t, parent, []string{encodedRecord, "owner/rms/#Save=progress"})
	store := NewDirectorySaveStore(root)
	for _, key := range []string{saveNamesRecord, "rms/" + saveNamesTemporary(0)} {
		if data, found, err := store.ReadSave(key); err != nil || found {
			t.Fatalf("%s read %q (found %t, %v), want no save", key, data, found, err)
		}
	}
	leftoverExpect(t, parent, []string{encodedRecord, "owner/rms/#Save=progress", leftoverReserved + "/lock="}, "after the reads")
	if err := store.StoreSave(saveNamesRecord, []byte("a save")); err != nil {
		t.Fatal(err)
	}
	wantSaves(t, store, map[string]string{saveNamesRecord: "a save", "rms/Save": "progress"})
	leftoverExpect(t, parent, []string{
		encodedRecord, "owner/rms/#Save=progress", "owner/" + encodeSaveName(saveNamesRecord) + "=a save", leftoverReserved + "/lock=",
	}, "after the write")
}
