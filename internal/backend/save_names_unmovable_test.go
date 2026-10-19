package backend

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

// One name of an earlier build's folder that cannot be renamed — a file
// locked in the Finder, or one another program holds open on Windows — used
// to stop the conversion there, and every later operation on the folder failed
// finishing it: every save of the game unreadable, and none writable, where
// the earlier build had read and written them all. A name that reads as its own
// key in an encoded folder now stays where it is, is read there, and the other
// names convert.
func TestANameThatCannotMoveStaysAndTheRestConvert(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	leftoverBuild(t, parent, []string{
		"owner/rms/Option=options",
		"owner/rms/Slot=slot progress",
		"owner/rms/.index=Option\nSlot",
	})
	failRenamesOf(t, "Option")
	store := NewDirectorySaveStore(root)
	wantSaves(t, store, map[string]string{"rms/Slot": "slot progress", "rms/.index": "Option\nSlot"})
	leftoverExpect(t, parent, []string{
		encodedRecord,
		"owner/rms/Option=options",
		"owner/rms/#Slot=slot progress",
		"owner/rms/.index=Option\nSlot",
		leftoverReserved + "/lock=",
	}, "after the conversion that could not move Option")
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatalf("the export: %v", err)
	}
	wantEntries(t, listed, map[string]string{
		"rms/Option": "options", "rms/Slot": "slot progress", "rms/.index": "Option\nSlot",
	}, "the export")
	wantSaves(t, store, map[string]string{"rms/Option": "options"})
	if err := store.StoreSave("rms/Option", []byte("new options")); err != nil {
		t.Fatalf("a write of the name that could not move: %v", err)
	}
	if err := store.StoreSave("rms/Slot", []byte("more progress")); err != nil {
		t.Fatalf("a write of a converted name: %v", err)
	}
	wantSaves(t, store, map[string]string{"rms/Option": "new options", "rms/Slot": "more progress"})
}

// A directory that cannot be renamed stays under its earlier name, and the
// names below it that had moved to their temporary names move back, so the
// whole of it is read as the earlier build left it.
func TestADirectoryThatCannotMoveStaysWhole(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	leftoverBuild(t, parent, []string{
		"owner/Data/Other=other",
		"owner/Data/Save=progress",
		"owner/Top=top",
	})
	failRenamesOf(t, "Data")
	store := NewDirectorySaveStore(root)
	wantSaves(t, store, map[string]string{"Top": "top"})
	leftoverExpect(t, parent, []string{
		encodedRecord,
		"owner/Data/Other=other",
		"owner/Data/Save=progress",
		"owner/#Top=top",
		leftoverReserved + "/lock=",
	}, "after the conversion that could not move Data")
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatalf("the export: %v", err)
	}
	wantEntries(t, listed, map[string]string{"Data/Other": "other", "Data/Save": "progress", "Top": "top"}, "the export")
	wantSaves(t, store, map[string]string{"Data/Save": "progress", "Data/Other": "other"})
}

// A name that cannot move and would read as another key in an encoded folder
// — the earlier key "a#Top", which an encoded folder reads as "aTop" — cannot
// stay. The names that had moved go back, the record goes, and the folder is
// read as the earlier build left it: every save readable, a write refused, and
// the conversion taken up again once the name can move.
func TestANameThatCanNeitherMoveNorStayUndoesTheConversion(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	state := []string{
		"owner/rms/Alpha=alpha",
		"owner/rms/a#Top=top",
		"owner/rms/slot=slot",
	}
	leftoverBuild(t, parent, state)
	original := saveNamesRename
	failRenamesOf(t, "a#Top")
	store := NewDirectorySaveStore(root)
	saves := map[string]string{"rms/Alpha": "alpha", "rms/a#Top": "top", "rms/slot": "slot"}
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock="), "after the undone conversion")
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatalf("the export: %v", err)
	}
	wantEntries(t, listed, saves, "the export")
	if err := store.StoreSave("rms/slot", []byte("refused")); err == nil {
		t.Fatal("a write into the folder that could not be converted was taken")
	}
	saveNamesRename = original
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, []string{
		encodedRecord,
		"owner/rms/#Alpha=alpha",
		"owner/rms/a###Top=top",
		"owner/rms/slot=slot",
		leftoverReserved + "/lock=",
	}, "after the name could move")
}
