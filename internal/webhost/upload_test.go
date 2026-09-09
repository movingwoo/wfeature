package webhost

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// upload posts one archive the way the page does: the bytes as the body, the
// name percent-encoded in the query.
func upload(t *testing.T, server *Server, name string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/games?" + url.Values{uploadNameQuery: {name}}.Encode()
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(string(content)))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

// The whole reason this route exists: on a phone there is no folder to put a
// game in, so the archive arrives over the socket and has to show up in the
// picker afterwards.
func TestAGameAddedFromThePageIsThereToPlay(t *testing.T) {
	addedRoot := t.TempDir()
	server := newTestServer(t, Options{AddedRoot: addedRoot})

	if recorder := upload(t, server, "한글이름.zip", []byte("PK\x03\x04 not really")); recorder.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", recorder.Code, recorder.Body)
	}

	written, err := os.ReadFile(filepath.Join(addedRoot, "한글이름.zip"))
	if err != nil {
		t.Fatalf("the archive is not in the added root: %v", err)
	}
	if string(written) != "PK\x03\x04 not really" {
		t.Errorf("the bytes changed on the way in: %q", written)
	}

	games := ListGames("", addedRoot)
	if len(games) != 1 || games[0].Name != "한글이름" {
		t.Fatalf("the picker lists %+v", games)
	}
	// An uploaded archive has no platform directory, and that is deliberate:
	// which platform it belongs to is read from its bytes when it is loaded,
	// and this route has not read them.
	if games[0].Group != "" {
		t.Errorf("group = %q, want the ungrouped root", games[0].Group)
	}
	// It carries the added root's prefix, which is what tells the page it may
	// remove this one, and what sends every later request to the right root.
	if !games[0].Added || !strings.HasPrefix(games[0].Path, "ext/") {
		t.Errorf("the picker offers %+v, want an added game under ext/", games[0])
	}
}

// The name is refused rather than repaired. A name that needs repairing is a
// request to write somewhere else, and the only useful answer is no.
func TestAnUploadCannotNameAPlaceOutsideTheGameRoot(t *testing.T) {
	for _, name := range []string{
		"../escape.zip",
		"sub/dir.zip",
		"back\\slash.zip",
		"",
		".hidden.zip",
		"notagame.txt",
		"noextension",
	} {
		t.Run(name, func(t *testing.T) {
			addedRoot := t.TempDir()
			server := newTestServer(t, Options{AddedRoot: addedRoot})
			recorder := upload(t, server, name, []byte("something"))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("upload %q = %d, want 400", name, recorder.Code)
			}
			entries, err := os.ReadDir(addedRoot)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("the refused upload left %d files behind", len(entries))
			}
		})
	}

	// And nothing climbs out of the root even when the parent is writable.
	parent := t.TempDir()
	addedRoot := filepath.Join(parent, "ext")
	server := newTestServer(t, Options{AddedRoot: addedRoot})
	if recorder := upload(t, server, "../escape.zip", []byte("x")); recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.zip")); !os.IsNotExist(err) {
		t.Fatal("an upload wrote outside the added root")
	}
}

// Re-uploading a corrected archive is a thing people do, and the file it
// replaces has to be gone rather than half-overwritten.
func TestUploadingTheSameNameReplacesTheArchiveWhole(t *testing.T) {
	addedRoot := t.TempDir()
	server := newTestServer(t, Options{AddedRoot: addedRoot})

	if recorder := upload(t, server, "game.zip", []byte("the first one, which is longer")); recorder.Code != http.StatusOK {
		t.Fatalf("first upload = %d", recorder.Code)
	}
	if recorder := upload(t, server, "game.zip", []byte("the second")); recorder.Code != http.StatusOK {
		t.Fatalf("second upload = %d", recorder.Code)
	}
	written, err := os.ReadFile(filepath.Join(addedRoot, "game.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "the second" {
		t.Errorf("content = %q, want the second upload whole", written)
	}
	// The temporary file the atomic write used is not left in the picker's way.
	entries, err := os.ReadDir(addedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the added root holds %d files, want one", len(entries))
	}
}

func TestAnUploadThatIsTooLargeSaysSo(t *testing.T) {
	addedRoot := t.TempDir()
	server := newTestServer(t, Options{AddedRoot: addedRoot})
	recorder := upload(t, server, "huge.zip", make([]byte, maxGameUpload+1))
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", recorder.Code)
	}
	// The sentence is for the person who picked the file, so it is in their
	// language rather than an HTTP reason phrase.
	if !strings.Contains(recorder.Body.String(), "너무 큽니다") {
		t.Errorf("body = %q", recorder.Body.String())
	}
}

func TestAnEmptyUploadIsRefused(t *testing.T) {
	addedRoot := t.TempDir()
	server := newTestServer(t, Options{AddedRoot: addedRoot})
	if recorder := upload(t, server, "empty.zip", nil); recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestOnlyPostAddsAGame(t *testing.T) {
	server := newTestServer(t, Options{AddedRoot: t.TempDir()})
	if recorder := get(t, server, "/api/games"); recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/games = %d, want 405", recorder.Code)
	}
}

// A host with nowhere to put an added game does not pretend to take one: the
// alternative is a file written beside the working directory and a picker that
// never lists it.
func TestAnUploadWithNoAddedRootIsRefused(t *testing.T) {
	server := newTestServer(t, Options{GameRoot: t.TempDir()})
	if recorder := upload(t, server, "game.zip", []byte("x")); recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}
