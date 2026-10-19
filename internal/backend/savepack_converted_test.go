package backend

import (
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
