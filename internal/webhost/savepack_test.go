package webhost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
)

// savePackFixture stands up a server over a real archive and a save tree with
// something in it. The archive is the platform package's own fixture rather
// than a copy: what identifies a save here is the bytes of a game this build
// can actually open, so a stand-in would be testing the stand-in.
func savePackFixture(t *testing.T) (server *Server, directory string, identity [32]byte, game string) {
	t.Helper()
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "canvas-skt.zip"))
	if err != nil {
		t.Fatalf("read the canvas fixture: %v", err)
	}
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "skt"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "skt", "canvas.zip"), archive, 0o644); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}
	server = newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "ktf"),
	})
	summary, err := session.Inspect(archive)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	directory = server.saveDirectory(summary.Platform, summary.SaveOwner)
	if directory == "" {
		t.Fatalf("the fixture has no save directory")
	}
	return server, directory, backend.SaveIdentity(archive), "games/skt/canvas.zip"
}

// savePackSaves is the tree a fixture starts from. `rms/.index` is in it on
// purpose: it is what says a record store exists at all, and it is the entry a
// backup that skipped dotted names would silently drop.
var savePackSaves = map[string]string{
	"rms/.index": "slot0\nslot1\n",
	"rms/slot0":  "the first save",
	"rms/slot1":  "the second save",
	"fs/deep/x":  "a file the guest wrote",
}

func writeSavePackSaves(t *testing.T, directory string) {
	t.Helper()
	store := backend.NewDirectorySaveStore(directory)
	for key, value := range savePackSaves {
		if err := store.StoreSave(key, []byte(value)); err != nil {
			t.Fatalf("seed %q: %v", key, err)
		}
	}
}

func savePackRequest(t *testing.T, server *Server, method, game string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/savepack?" + savePackQuery + "=" + game
	request := httptest.NewRequest(method, target, strings.NewReader(string(body)))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func TestSavePackExportsAndImportsOneGamesSaves(t *testing.T) {
	server, directory, identity, game := savePackFixture(t)
	writeSavePackSaves(t, directory)

	exported := savePackRequest(t, server, http.MethodGet, game, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status = %d, body %q", exported.Code, exported.Body.String())
	}
	if got := exported.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("content type = %q", got)
	}
	if got := exported.Header().Get("Content-Disposition"); !strings.Contains(got, savePackExtension) {
		t.Errorf("disposition = %q, want it to name the file", got)
	}
	container := exported.Body.Bytes()

	pack, err := backend.DecodeSavePack(container)
	if err != nil {
		t.Fatalf("the exported container did not decode: %v", err)
	}
	if pack.Identity != identity {
		t.Errorf("the container does not carry the archive's identity")
	}
	if len(pack.Entries) != len(savePackSaves) {
		t.Fatalf("exported %d entries, want %d", len(pack.Entries), len(savePackSaves))
	}
	for _, entry := range pack.Entries {
		if want, ok := savePackSaves[entry.Key]; !ok || string(entry.Data) != want {
			t.Errorf("entry %q is %q", entry.Key, entry.Data)
		}
	}

	// The tree is emptied, so what the import restores can only have come from
	// the container.
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	imported := savePackRequest(t, server, http.MethodPost, game, container)
	if imported.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %q", imported.Code, imported.Body.String())
	}
	var result savePackResult
	if err := json.Unmarshal(imported.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode the import result: %v", err)
	}
	if result.Written != len(savePackSaves) || result.Removed != 0 {
		t.Errorf("import wrote %d and removed %d", result.Written, result.Removed)
	}
	store := backend.NewDirectorySaveStore(directory)
	for key, want := range savePackSaves {
		got, ok := store.LoadSave(key)
		if !ok || string(got) != want {
			t.Errorf("%q came back as %q/%v", key, got, ok)
		}
	}
}

// The two refusals are the whole reason the container has a header. Answering
// the same thing to both would send someone hunting for another copy of a file
// that was never theirs, or retrying one that will never load — so they are
// different statuses and different sentences.
func TestSavePackRefusesAnotherGamesBackupAndADamagedOne(t *testing.T) {
	server, directory, _, game := savePackFixture(t)
	writeSavePackSaves(t, directory)
	container := savePackRequest(t, server, http.MethodGet, game, nil).Body.Bytes()

	t.Run("another game", func(t *testing.T) {
		// The identity is the 32 bytes at offset 10; changing one of them is
		// exactly "this backup is somebody's real save, of a different game".
		// The checksum is over the payload, so the container is still intact.
		other := append([]byte(nil), container...)
		other[10] ^= 0xff
		if _, err := backend.DecodeSavePack(other); err != nil {
			t.Fatalf("the edited container should still decode: %v", err)
		}
		recorder := savePackRequest(t, server, http.MethodPost, game, other)
		if recorder.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
		}
		if !strings.Contains(recorder.Body.String(), "다른 게임") {
			t.Errorf("body = %q, want it to say this is another game's backup", recorder.Body.String())
		}
	})

	t.Run("damaged", func(t *testing.T) {
		damaged := append([]byte(nil), container...)
		damaged[len(damaged)-1] ^= 0x01
		recorder := savePackRequest(t, server, http.MethodPost, game, damaged)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
		}
		if !strings.Contains(recorder.Body.String(), "손상") {
			t.Errorf("body = %q, want it to say the file is damaged", recorder.Body.String())
		}
	})

	t.Run("not a backup at all", func(t *testing.T) {
		recorder := savePackRequest(t, server, http.MethodPost, game, []byte("this is a photograph"))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
		if !strings.Contains(recorder.Body.String(), "백업 파일이 아닙니다") {
			t.Errorf("body = %q", recorder.Body.String())
		}
	})

	// Nothing above reached the tree: a refused import leaves the save it was
	// pointed at exactly as it was.
	store := backend.NewDirectorySaveStore(directory)
	for key, want := range savePackSaves {
		if got, ok := store.LoadSave(key); !ok || string(got) != want {
			t.Errorf("a refused import changed %q", key)
		}
	}
}

// A restore is the moment the backup describes. An entry that is in the tree
// and not in the backup came from a later point in the story, and leaving it
// beside the restored entries builds a state the game never wrote.
func TestSavePackImportReplacesWhatIsThere(t *testing.T) {
	server, directory, _, game := savePackFixture(t)
	writeSavePackSaves(t, directory)
	container := savePackRequest(t, server, http.MethodGet, game, nil).Body.Bytes()

	store := backend.NewDirectorySaveStore(directory)
	if err := store.StoreSave("rms/slot2", []byte("saved after the backup")); err != nil {
		t.Fatal(err)
	}

	recorder := savePackRequest(t, server, http.MethodPost, game, container)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", recorder.Code, recorder.Body.String())
	}
	var result savePackResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 {
		t.Errorf("removed %d, want 1", result.Removed)
	}
	if _, ok := store.LoadSave("rms/slot2"); ok {
		t.Errorf("an entry the backup does not name survived the restore")
	}
}

// An export writes nothing, so the claim's defect — two writers on one
// directory — cannot reach it, and refusing would refuse the page against its
// own running game. An import writes, so it takes the claim a save API write
// takes and is refused with the holder's name.
func TestSavePackExportIsFreeWhileImportTakesTheClaim(t *testing.T) {
	server, directory, _, game := savePackFixture(t)
	writeSavePackSaves(t, directory)
	container := savePackRequest(t, server, http.MethodGet, game, nil).Body.Bytes()

	if claimed, holder := server.claimSaveDirectory(directory, "the game in another tab"); !claimed {
		t.Fatalf("could not take the claim: held by %q", holder)
	}
	defer server.releaseSaveDirectory(directory)

	exported := savePackRequest(t, server, http.MethodGet, game, nil)
	if exported.Code != http.StatusOK {
		t.Errorf("an export under a running game was refused: %d %q", exported.Code, exported.Body.String())
	}

	imported := savePackRequest(t, server, http.MethodPost, game, container)
	if imported.Code != http.StatusConflict {
		t.Fatalf("import status = %d, want %d", imported.Code, http.StatusConflict)
	}
	if !strings.Contains(imported.Body.String(), "the game in another tab") {
		t.Errorf("body = %q, want it to name the holder", imported.Body.String())
	}
}

// The refusal above must not outlive the window it names. Parking is what
// survives a page reload, so a parked holder refused every reload the person
// tried while the message told them to stop a game in a window that was
// already gone — and the import buttons sit on the screen where that game's
// own parked session is the likeliest holder. A parked holder is taken over
// here, the way starting the game already takes it over.
func TestSavePackImportTakesOverAParkedHolder(t *testing.T) {
	server, directory, _, game := savePackFixture(t)
	writeSavePackSaves(t, directory)
	container := savePackRequest(t, server, http.MethodGet, game, nil).Body.Bytes()

	if claimed, holder := server.claimSaveDirectory(directory, "this game, in a tab that is gone"); !claimed {
		t.Fatalf("could not take the claim: held by %q", holder)
	}
	server.markSaveDirectoryParked(directory, true)

	imported := savePackRequest(t, server, http.MethodPost, game, container)
	if imported.Code != http.StatusOK {
		t.Fatalf("import under a parked holder gave %d %q, want %d",
			imported.Code, imported.Body.String(), http.StatusOK)
	}
	// The import releases what it took, so nothing is left holding the
	// directory — a second import in a row has to work as well.
	if again := savePackRequest(t, server, http.MethodPost, game, container); again.Code != http.StatusOK {
		t.Errorf("a second import gave %d %q", again.Code, again.Body.String())
	}
}

// An empty backup is worse than no backup: it is a file the person keeps,
// believing their progress is in it.
func TestSavePackWillNotExportAGameThatHasNeverSaved(t *testing.T) {
	server, _, _, game := savePackFixture(t)
	recorder := savePackRequest(t, server, http.MethodGet, game, nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if !strings.Contains(recorder.Body.String(), "저장된 데이터가 없습니다") {
		t.Errorf("body = %q", recorder.Body.String())
	}
}

func TestSavePackRefusesAGameOutsideTheGameRoot(t *testing.T) {
	server, _, _, _ := savePackFixture(t)
	for _, game := range []string{"", "games/../../etc/passwd", "not-games/x.zip", "games/missing.zip"} {
		recorder := savePackRequest(t, server, http.MethodGet, game, nil)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("game %q gave %d, want %d", game, recorder.Code, http.StatusBadRequest)
		}
	}
}

func TestSavePackRejectsOtherMethods(t *testing.T) {
	server, _, _, game := savePackFixture(t)
	recorder := savePackRequest(t, server, http.MethodDelete, game, nil)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

// The save API listed every entry except the dotted ones, which loses the
// state that says a save exists: `rms/.index` is what makes a MIDP record
// store visible at all, and the file layers keep `.removed`, `.created` and
// `.dirs`. The rule it wanted was "not a temporary file", and a leading dot is
// not that rule.
func TestSaveAPIListsTheDottedStateAndNotTheTemporaries(t *testing.T) {
	saveRoot := filepath.Join(t.TempDir(), "ktf")
	server := newTestServer(t, Options{SaveRoot: saveRoot})
	ownerRoot := filepath.Join(filepath.Dir(saveRoot), "skt", "0102DD43")
	if err := os.MkdirAll(filepath.Join(ownerRoot, "rms"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"rms/.index":           "slot0\n",
		"rms/slot0":            "records",
		"rms/.slot0.418304559": "half a save",
	} {
		if err := os.WriteFile(filepath.Join(ownerRoot, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	listed := get(t, server, "/api/saves/skt/0102DD43")
	if listed.Code != http.StatusOK {
		t.Fatalf("status = %d", listed.Code)
	}
	var response saveResponse
	if err := json.Unmarshal(listed.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := response.Saves["rms/.index"]; !ok {
		t.Errorf("the record store index is missing from the listing: %v", keysOf(response.Saves))
	}
	if _, ok := response.Saves["rms/slot0"]; !ok {
		t.Errorf("an ordinary save is missing: %v", keysOf(response.Saves))
	}
	if _, ok := response.Saves["rms/.slot0.418304559"]; ok {
		t.Errorf("a leftover temporary file was listed as a save")
	}
}

func keysOf(saves map[string]string) []string {
	names := make([]string, 0, len(saves))
	for name := range saves {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
