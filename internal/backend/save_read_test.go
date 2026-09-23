package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveReadDistinguishesMissingUnreadableAndEmpty(t *testing.T) {
	root := t.TempDir()
	store := NewDirectorySaveStore(root)
	if _, exists, err := ReadSave(store, "missing"); exists || err != nil {
		t.Fatalf("missing = %t, %v", exists, err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadSave(store, "directory"); err == nil {
		t.Fatal("read error reported as absence")
	}
	if err := store.StoreSave("empty", nil); err != nil {
		t.Fatal(err)
	}
	if data, exists, err := ReadSave(store, "empty"); len(data) != 0 || !exists || err != nil {
		t.Fatalf("empty = %v, %t, %v", data, exists, err)
	}
	if _, _, err := ReadSave(store, "../outside"); err == nil {
		t.Fatal("invalid path reported as absence")
	}
}

func TestSaveRecordsRejectTrailingData(t *testing.T) {
	if _, err := DecodeSaveRecords(append(EncodeSaveRecords(nil), 1)); err == nil {
		t.Fatal("trailing corruption accepted")
	}
}

func TestSaveReadBelowFileReportsMissing(t *testing.T) {
	store := NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("fs/state", []byte("progress")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fs/state/asset", "fs/state/scene/asset"} {
		if _, exists, err := ReadSave(store, name); exists || err != nil {
			t.Fatalf("read %s = %t, %v; want missing", name, exists, err)
		}
	}
	if err := store.StoreSave("fs/state/asset", []byte("invalid")); err == nil {
		t.Fatal("write below a file succeeded")
	}
	if data, exists, err := ReadSave(store, "fs/state"); string(data) != "progress" || !exists || err != nil {
		t.Fatalf("parent file = %q, %t, %v", data, exists, err)
	}
}
