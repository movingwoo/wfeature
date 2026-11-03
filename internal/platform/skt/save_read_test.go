package skt

import (
	"errors"
	"strings"
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

type unreadableScriptStore struct{ writes int }

func (*unreadableScriptStore) LoadSave(string) ([]byte, bool) { return nil, false }

func (*unreadableScriptStore) ReadSave(string) ([]byte, bool, error) {
	return nil, false, errors.New("save read failed")
}

func (store *unreadableScriptStore) StoreSave(string, []byte) error {
	store.writes++
	return nil
}

// An SGS load that cannot read the save stops rather than handing the title
// zeros it would save over the real progress.
func TestScriptSaveLoadReportsAReadError(t *testing.T) {
	store := &unreadableScriptStore{}
	s := newScriptTest(t, []byte{0xff}, store)
	s.vm.Push(16)
	s.vm.Push(33)
	if err := s.Call(0x98, s.vm); err == nil || !strings.Contains(err.Error(), "save read failed") {
		t.Fatalf("load with an unreadable save error = %v, want the read error", err)
	}
	if store.writes != 0 {
		t.Fatal("a failed load wrote the save")
	}
}
