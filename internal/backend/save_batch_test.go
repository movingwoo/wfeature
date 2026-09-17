package backend

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveBatchStagesAllEntriesAndRestoresFailedCommit(t *testing.T) {
	for _, existing := range []bool{false, true} {
		store := NewDirectorySaveStore(t.TempDir())
		if existing {
			if err := store.StoreSave("a", []byte("old")); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.StoreSave("b", []byte("kept")); err != nil {
			t.Fatal(err)
		}
		injected := errors.New("injected commit failure")
		err := store.storeSaves(map[string][]byte{"a": []byte("new"), "b": []byte("replacement")}, func(from, to string) error {
			if filepath.Base(from) == "new-1" {
				return injected
			}
			return os.Rename(from, to)
		})
		if !errors.Is(err, injected) {
			t.Fatalf("commit error = %v", err)
		}
		data, present, err := store.ReadSave("a")
		if err != nil || present != existing || (existing && !bytes.Equal(data, []byte("old"))) {
			t.Fatal("failed commit did not restore first entry")
		}
		if data, _, err := store.ReadSave("b"); err != nil || string(data) != "kept" {
			t.Fatal("failed commit changed second entry")
		}
		entries, err := os.ReadDir(store.root)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".batch-") {
				t.Fatal("successful rollback leaked staging files")
			}
		}
	}
}

func TestSaveBatchRejectsReadFailureBeforeAnyReplacement(t *testing.T) {
	store := NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("a", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(store.root, "b"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSaves(map[string][]byte{"a": []byte("new"), "b": []byte("invalid")}); err == nil {
		t.Fatal("unreadable target accepted")
	}
	if data, _, _ := store.ReadSave("a"); string(data) != "old" {
		t.Fatal("staging failure replaced an earlier entry")
	}
}
