package webhost

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// A browser sends a cross-site POST, PUT or DELETE that needs no preflight
// without asking anyone first — a page anywhere on the internet can aim one at
// 127.0.0.1. Such a request carries the page's Origin, and a route that
// changes something refuses one from another site: the same rule the session
// socket already keeps. A request with no Origin at all is not from a browser
// page — the stop command, curl — and is answered as before.
func TestAStateChangingRequestFromAnotherSiteIsRefused(t *testing.T) {
	root := t.TempDir()
	addedRoot := filepath.Join(root, "ext")
	saveRoot := filepath.Join(root, "savedata", "ktf")
	var stops atomic.Int32
	server := newTestServer(t, Options{
		AddedRoot:       addedRoot,
		SaveRoot:        saveRoot,
		LogRoot:         filepath.Join(root, "logs"),
		RequestShutdown: func() { stops.Add(1) },
	})
	serve := func(method, target, origin, body string) int {
		request := httptest.NewRequest(method, "http://127.0.0.1:11541"+target, strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:50000"
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		return recorder.Code
	}

	for _, origin := range []string{"https://evil.example", "http://127.0.0.1:8080", "null"} {
		if code := serve(http.MethodPost, "/api/shutdown", origin, ""); code != http.StatusForbidden {
			t.Errorf("a shutdown from %q answered %d, want 403", origin, code)
		}
		if code := serve(http.MethodPut, "/api/saves/PID0001/fs/save", origin, "junk"); code != http.StatusForbidden {
			t.Errorf("a save write from %q answered %d, want 403", origin, code)
		}
		if code := serve(http.MethodPost, "/api/games?name=game.zip", origin, "PK\x03\x04"); code != http.StatusForbidden {
			t.Errorf("an upload from %q answered %d, want 403", origin, code)
		}
		if code := serve(http.MethodDelete, "/api/games?game=ext/game.zip", origin, ""); code != http.StatusForbidden {
			t.Errorf("a removal from %q answered %d, want 403", origin, code)
		}
		if code := serve(http.MethodPost, "/api/savepack?game=games/x.zip", origin, ""); code != http.StatusForbidden {
			t.Errorf("a save import from %q answered %d, want 403", origin, code)
		}
	}
	if stops.Load() != 0 {
		t.Fatalf("a cross-site request stopped the server %d times", stops.Load())
	}
	if _, err := os.Stat(filepath.Join(saveRoot, "PID0001")); !os.IsNotExist(err) {
		t.Fatalf("a cross-site save write reached the tree (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(addedRoot, "game.zip")); !os.IsNotExist(err) {
		t.Fatalf("a cross-site upload reached the added root (%v)", err)
	}

	// The page's own requests carry its own origin, and reading is never
	// refused: a GET changes nothing.
	if code := serve(http.MethodPut, "/api/saves/PID0001/fs/save", "http://127.0.0.1:11541", "progress"); code != http.StatusNoContent && code != http.StatusOK {
		t.Errorf("the page's own save write answered %d", code)
	}
	if code := serve(http.MethodGet, "/api/saves/PID0001", "https://evil.example", ""); code != http.StatusOK {
		t.Errorf("a read from another site answered %d, want 200", code)
	}
	// The stop command sends no Origin, and it still works.
	if code := serve(http.MethodPost, "/api/shutdown", "", ""); code != http.StatusAccepted {
		t.Errorf("a shutdown with no Origin answered %d, want 202", code)
	}
}
