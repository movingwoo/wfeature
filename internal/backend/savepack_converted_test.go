package backend

import (
	"errors"
	"path/filepath"
	"testing"
)

// An earlier build exports a folder this build had converted with its names as
// keys, .wfeature-names#1 among them. Restored as those keys, the backup put
// the save under "rms/#Save", which no title asks for, stored the record as a
// save of its own, and removed "rms/Save", the save the title does read: the
// import left the game without its save. The names are read as the folder's
// own names are.
func TestImportingAnEarlierBuildsExportOfAConvertedFolderRestoresItsKeys(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	if err := store.StoreSave("rms/Save", []byte("progress")); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave("rms/.index", []byte("Save")); err != nil {
		t.Fatal(err)
	}
	exported := []SaveEntry{
		{Key: saveNamesRecord, Data: []byte(saveNamesHeader + "\nencoded\n")},
		{Key: "rms/#Save", Data: []byte("restored progress")},
		{Key: "rms/.index", Data: []byte("Save")},
	}
	if _, _, err := WriteSaveTree(root, exported); err != nil {
		t.Fatalf("the import: %v", err)
	}
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	saves := map[string]string{"rms/Save": "restored progress", "rms/.index": "Save"}
	wantEntries(t, listed, saves, "the folder after the import")
	wantSaves(t, store, saves)
}

// Of two names that read as one key, the encoded one is restored, as a listing
// of the folder chooses it when it cannot tell which was written later; and a
// backup without the record is restored key for key, '#' and all.
func TestAnEarlierBuildsExportIsReadAsItsFolderWasRead(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	if _, _, err := WriteSaveTree(root, []SaveEntry{
		{Key: "rms/Save", Data: []byte("the earlier build's")},
		{Key: saveNamesRecord, Data: []byte(saveNamesHeader + "\nencoded\n")},
		{Key: "rms/#Save", Data: []byte("encoded")},
	}); err != nil {
		t.Fatalf("the import: %v", err)
	}
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, map[string]string{"rms/Save": "encoded"}, "the folder after the import")

	plain := filepath.Join(t.TempDir(), "owner")
	if _, _, err := WriteSaveTree(plain, []SaveEntry{{Key: "rms/#Save", Data: []byte("a key with a hash")}}); err != nil {
		t.Fatalf("the import without the record: %v", err)
	}
	wantSaves(t, NewDirectorySaveStore(plain), map[string]string{"rms/#Save": "a key with a hash"})
}

// A converted folder an earlier build exported with nothing in it but the
// record is a backup of one entry, which passes the refusal of an empty one,
// and that entry is no save. Restored, it wrote nothing, removed every save
// the game had and answered success: the empty restore that refusal is for.
func TestAnEarlierBuildsExportOfOnlyTheRecordIsRefused(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	state := []string{"owner/rms/save=progress", "owner/rms/index=save"}
	leftoverBuild(t, parent, state)
	written, removed, err := WriteSaveTree(root, []SaveEntry{{Key: saveNamesRecord, Data: []byte(saveNamesHeader + "\nencoded\n")}})
	if !errors.Is(err, ErrSaveBackupEmpty) || written != 0 || removed != 0 {
		t.Fatalf("the import wrote %d, removed %d, answered %v; want ErrSaveBackupEmpty and nothing touched", written, removed, err)
	}
	leftoverExpect(t, parent, state, "after the refused import")
}
