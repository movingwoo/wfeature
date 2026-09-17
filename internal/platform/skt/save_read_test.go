package skt

import (
	"errors"
	"testing"
)

type unreadableSaveStore struct{ writes int }

func (*unreadableSaveStore) LoadSave(string) ([]byte, bool) { return nil, false }
func (*unreadableSaveStore) ReadSave(string) ([]byte, bool, error) {
	return nil, false, errors.New("injected read failure")
}
func (store *unreadableSaveStore) StoreSave(string, []byte) error { store.writes++; return nil }

func TestUnreadableRecordIndexCannotCreateAnEmptyStore(t *testing.T) {
	store := &unreadableSaveStore{}
	runtime := startRecordStoreFixture(t, store)
	if _, err := runtime.openStore("save", true); err == nil {
		t.Fatal("read failure became an empty record store")
	}
	if store.writes != 0 {
		t.Fatal("read failure overwrote persistent data")
	}
	if _, _, err := runtime.xFileContents("save"); err == nil {
		t.Fatal("file read failure reported as absence")
	}
}
