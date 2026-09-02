package webhost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

const testKey = "a-key-a-phone-was-given"

// A server that was not started for the network is the one that has always
// been here. This is the test that says so: every route a page uses answers
// without anything being carried, which is what a phone on the same Wi-Fi
// depends on.
func TestAServerWithNoKeyAsksForNothing(t *testing.T) {
	server := newTestServer(t, Options{})
	for _, target := range []string{"/", "/app.js", "/games.json"} {
		if recorder := get(t, server, target); recorder.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 from a server with no key", target, recorder.Code)
		}
	}
}

func TestAKeyedServerRefusesWhatDoesNotCarryTheKey(t *testing.T) {
	server := newTestServer(t, Options{AccessKey: testKey})
	for _, target := range []string{"/", "/app.js", "/games.json", "/api/saves/db/x", "/licenses"} {
		if recorder := get(t, server, target); recorder.Code != http.StatusForbidden {
			t.Errorf("GET %s = %d, want 403 without the key", target, recorder.Code)
		}
	}
	if recorder := get(t, server, "/?k=not-the-key"); recorder.Code != http.StatusForbidden {
		t.Errorf("a wrong key = %d, want 403", recorder.Code)
	}
}

// The link is used once and then it is gone: the key becomes a cookie and the
// browser is sent to the same page without it, so it is not left in the
// address bar, the history, or a screenshot of the tab.
func TestFollowingTheLinkTradesTheKeyForACookie(t *testing.T) {
	server := newTestServer(t, Options{AccessKey: testKey})

	recorder := get(t, server, "/?k="+testKey)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("following the link = %d, want a redirect", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); strings.Contains(location, testKey) {
		t.Errorf("Location = %q, want the key gone from it", location)
	}
	cookies := (&http.Response{Header: recorder.Header()}).Cookies()
	if len(cookies) != 1 || cookies[0].Name != accessCookie || cookies[0].Value != testKey {
		t.Fatalf("cookies = %+v, want the key in %s", cookies, accessCookie)
	}
	if !cookies[0].HttpOnly {
		t.Error("the key cookie is readable by scripts on the page")
	}

	// And the cookie is then the whole of what the page carries.
	request := httptest.NewRequest(http.MethodGet, "/games.json", nil)
	request.AddCookie(cookies[0])
	replay := httptest.NewRecorder()
	server.ServeHTTP(replay, request)
	if replay.Code != http.StatusOK {
		t.Fatalf("GET /games.json with the cookie = %d, want 200", replay.Code)
	}

	stale := httptest.NewRequest(http.MethodGet, "/games.json", nil)
	stale.AddCookie(&http.Cookie{Name: accessCookie, Value: "a key from another server"})
	replay = httptest.NewRecorder()
	server.ServeHTTP(replay, stale)
	if replay.Code != http.StatusForbidden {
		t.Errorf("GET /games.json with a stale cookie = %d, want 403", replay.Code)
	}
}

// /api/status is outside the gate because it is how anything tells this server
// from a stranger holding the port — including the launcher that is about to
// stop it, which has to know what it is talking to before it can be trusted
// with a key.
func TestStatusAnswersWithoutTheKey(t *testing.T) {
	server := newTestServer(t, Options{AccessKey: testKey})
	recorder := get(t, server, "/api/status")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/status = %d, want 200", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), testKey) {
		t.Error("the status answer carries the key")
	}
}

// The hole this closes: a tunnel in front of the server makes every request
// arrive from 127.0.0.1, so "only a caller on this machine" stops meaning
// anything and the whole internet inherits the stop button. The admin key is a
// second secret that never leaves the machine, and the players' key is not it.
func TestShutdownNeedsTheAdminKeyRatherThanALoopbackAddress(t *testing.T) {
	// The route answers before it asks the process to stop, so the count is
	// written on a goroutine the test is still reading.
	var stops atomic.Int64
	options := Options{
		AccessKey:       testKey,
		AdminKey:        "the-admin-key",
		RequestShutdown: func() { stops.Add(1) },
	}

	post := func(t *testing.T, server *Server, header, value string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/shutdown", nil)
		// What a tunnel agent on this machine looks like from in here.
		request.RemoteAddr = "127.0.0.1:54321"
		if header != "" {
			request.Header.Set(header, value)
		}
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		return recorder
	}

	server := newTestServer(t, options)
	if recorder := post(t, server, "", ""); recorder.Code != http.StatusForbidden {
		t.Errorf("a shutdown with no admin key = %d, want 403", recorder.Code)
	}
	// Holding the link a player was given is not being the person entitled to
	// end everybody's game.
	if recorder := post(t, server, AdminHeader, testKey); recorder.Code != http.StatusForbidden {
		t.Errorf("a shutdown with the players' key = %d, want 403", recorder.Code)
	}
	if got := stops.Load(); got != 0 {
		t.Fatalf("the server stopped %d times before anyone had the admin key", got)
	}

	if recorder := post(t, server, AdminHeader, "the-admin-key"); recorder.Code != http.StatusAccepted {
		t.Fatalf("a shutdown with the admin key = %d, want 202", recorder.Code)
	}
	// RequestShutdown is called on its own goroutine, so this waits for it.
	deadline := time.Now().Add(2 * time.Second)
	for stops.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := stops.Load(); got != 1 {
		t.Fatalf("the server was asked to stop %d times, want once", got)
	}

	// A server that was not started for the network keeps the rule it had:
	// loopback is the whole of the answer, and stop.bat still works.
	plain := newTestServer(t, Options{RequestShutdown: func() { stops.Add(1) }})
	if recorder := post(t, plain, "", ""); recorder.Code != http.StatusAccepted {
		t.Errorf("a local shutdown of an ordinary server = %d, want 202", recorder.Code)
	}
}

// A WebSocket handshake carries the key in the address like everything else,
// and is the one request that must not be redirected to take it back out — the
// client would follow the redirect and arrive without the upgrade.
func TestTheSessionSocketTakesTheKeyInTheAddress(t *testing.T) {
	server := newTestServer(t, Options{AccessKey: testKey})
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() {
		httpServer.Close()
		server.CloseParkedSessions()
	})
	address := "ws://" + strings.TrimPrefix(httpServer.URL, "http://") + "/api/session"

	if _, _, err := wsproto.Dial(address, nil); err == nil {
		t.Error("a session opened with no key")
	}

	connection, _, err := wsproto.Dial(address+"?k="+testKey, nil)
	if err != nil {
		t.Fatalf("dial with the key: %v", err)
	}
	_ = connection.Close()
}
