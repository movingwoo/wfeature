package webhost

import (
	"net/http"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// An export never writes an empty backup — a game that has not saved is told
// so instead — so an empty one arriving at the import is not a backup this
// build made. Restoring it would remove every save the game has and report
// success, so it is refused and the saves stay.
func TestSavePackImportRefusesABackupWithNothingInIt(t *testing.T) {
	server, directory, identity, game := savePackFixture(t)
	writeSavePackSaves(t, directory)
	empty, err := backend.EncodeSavePack(backend.SavePack{Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	recorder := savePackRequest(t, server, http.MethodPost, game, empty)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body %q; want 422", recorder.Code, recorder.Body.String())
	}
	store := backend.NewDirectorySaveStore(directory)
	for key, value := range savePackSaves {
		if data, ok := store.LoadSave(key); !ok || string(data) != value {
			t.Fatalf("%s is %q/%v after the refusal, want the save that was there", key, data, ok)
		}
	}
}
