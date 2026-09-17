package lgt

import (
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

type unreadableSaveStore struct{ writes int }

func (*unreadableSaveStore) LoadSave(string) ([]byte, bool) { return nil, false }
func (*unreadableSaveStore) ReadSave(string) ([]byte, bool, error) {
	return nil, false, errors.New("injected read failure")
}
func (store *unreadableSaveStore) StoreSave(string, []byte) error { store.writes++; return nil }

func TestUnreadableSaveCannotFallBackAndOverwriteProgress(t *testing.T) {
	client := fixtureClient(t)
	store := &unreadableSaveStore{}
	client.saveStore = store
	client.readFile("save")
	if client.saveReadError == nil {
		t.Fatal("read failure was not retained")
	}
	client.writeFile("save", []byte("replacement"))
	if store.writes != 0 {
		t.Fatal("read failure permitted an overwrite")
	}
}

func TestAuthenticationOptionViewPreservesReadFailure(t *testing.T) {
	base := &unreadableSaveStore{}
	view := newAuthenticationOptionStore(base, nil)
	if _, _, err := backend.ReadSave(view, authenticationOptionsKey); err == nil {
		t.Fatal("authentication view hid a read error")
	}
	if err := view.StoreSave(authenticationOptionsKey, make([]byte, 56)); err == nil {
		t.Fatal("unreadable original word was overwritten")
	}
	if base.writes != 0 {
		t.Fatal("failed authentication read reached persistent writes")
	}
}
