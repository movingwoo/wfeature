package webhost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/accounts"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

func sharedHost(t *testing.T) (*Server, string) {
	t.Helper()
	root, _ := checkpointServerFiles(t)
	writeCheckpointGame(t, root, "skt", checkpointSKTBrowserArchive(t, "java"))
	store, err := accounts.Open(filepath.Join(root, "accounts.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	opts := Options{Accounts: store, GameRoot: filepath.Join(root, "games"), AddedRoot: filepath.Join(root, "ext"),
		SaveRoot: filepath.Join(root, "saves", "custom-ktf"), LogRoot: filepath.Join(root, "logs")}
	if err := os.MkdirAll(opts.AddedRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.AddedRoot, "private.zip"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, opts)
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() { server.CloseSessions(context.Background()); httpServer.Close() })
	return server, "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/session"
}

func accountRequest(server *Server, method, path string, body []byte, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "https://shared.example"+path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func signIn(t *testing.T, s *Server, action, id string) *http.Cookie {
	t.Helper()
	body, err := json.Marshal(map[string]string{"id": id, "password": "test password"})
	if err != nil {
		t.Fatal(err)
	}
	answer := accountRequest(s, "POST", "/api/"+action, body, nil)
	if answer.Code/100 != 2 {
		t.Fatalf("%s %s: %d %s", action, id, answer.Code, answer.Body.String())
	}
	cookies := answer.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
		t.Fatalf("bad login cookie attributes")
	}
	return cookies[0]
}

func accountSocket(t *testing.T, addr string, cookie *http.Cookie) *wsproto.Conn {
	t.Helper()
	conn, _, err := wsproto.Dial(addr, http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	expectMessage(t, conn, serverReady)
	return conn
}

func TestAccountFormWarnings(t *testing.T) {
	s, _ := sharedHost(t)
	signIn(t, s, "register", "player")
	const good = "test password"
	tests := []struct {
		action, id, password string
		status               int
		key                  string
	}{
		{"login", "", good, 400, "account.id-required"},
		{"login", "player", "", 400, "account.password-required"},
		{"login", "unregistered", good, 401, "account.id-not-found"},
		{"login", "player", "bad", 401, "account.wrong-password"},
		{"register", "player", good, 409, "account.id-taken"},
		{"register", "   ", good, 400, "account.id-required"},
		{"register", "ab", good, 400, "account.id-too-short"},
		{"register", strings.Repeat("x", 21), good, 400, "account.id-too-long"},
		{"register", "user name", good, 400, "account.id-whitespace"},
		{"register", "user@name", good, 400, "account.id-characters"},
		{"register", "newbie", "", 400, "account.password-required"},
		{"register", "newbie", "1234567", 400, "account.password-too-short"},
		{"register", "newbie", strings.Repeat("x", 33), 400, "account.password-too-long"},
		{"register", "newbie", strings.Repeat(" ", 8), 400, "account.password-blank"},
	}
	limits := []string{"idMin", "3", "idMax", "20", "passwordMin", "8", "passwordMax", "32"}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"id": tc.id, "password": tc.password})
			if err != nil {
				t.Fatal(err)
			}
			got := accountRequest(s, "POST", "/api/"+tc.action, body, nil)
			if got.Code != tc.status || got.Body.String() != pageSentence(t, tc.key, limits...) || got.Header().Get("Set-Cookie") != "" {
				t.Errorf("status %d, message %q, expected %d and %s", got.Code, got.Body.String(), tc.status, tc.key)
			}
		})
	}
	for _, body := range []string{"null", `{"password":false}`, `{"owner":"me"}`} {
		malformed := accountRequest(s, "POST", "/api/login", []byte(body), nil)
		if malformed.Code != 400 || malformed.Body.String() != pageSentence(t, "account.invalid-input") {
			t.Errorf("malformed request: %d %q", malformed.Code, malformed.Body.String())
		}
	}
	unauthorized := accountRequest(s, "GET", "/games.json", nil, nil)
	if unauthorized.Code != 401 || unauthorized.Body.String() != pageSentence(t, "account.login-required") {
		t.Errorf("login required: %d %q", unauthorized.Code, unauthorized.Body.String())
	}
	if body := get(t, s, "/").Body.String(); !strings.Contains(body, pageSentence(t, "account.signup-rules", limits...)) {
		t.Error("entry does not show the current registration limits")
	}
}

func TestSharedPlayerRoutesAndLibrary(t *testing.T) {
	s, addr := sharedHost(t)
	for _, path := range []string{"/games.json", "/games/ktf/checkpoint.zip", "/api/savepack", "/api/saves/owner", "/api/session", "/api/debug-log"} {
		if got := accountRequest(s, "GET", path, nil, nil); got.Code != 401 || got.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("anonymous %s: %d", path, got.Code)
		}
	}
	if conn, resp, err := wsproto.Dial(addr, nil); err == nil || resp == nil || resp.StatusCode != 401 {
		if conn != nil {
			conn.Close()
		}
		t.Fatal("anonymous upgrade was not refused")
	}
	if got := accountRequest(s, "GET", "/api/status", nil, nil); got.Code != 200 {
		t.Fatalf("status: %d", got.Code)
	}
	if got := accountRequest(s, "GET", "/api/account", nil, nil); got.Code != 200 || !strings.Contains(got.Body.String(), `"user":null`) {
		t.Fatalf("anonymous state: %d %s", got.Code, got.Body.String())
	}
	if got := accountRequest(s, "POST", "/api/register", []byte(`{"id":"one","password":"12345678","legacy":true}`), nil); got.Code != 400 {
		t.Fatalf("registration accepted extra fields: %d", got.Code)
	}
	a := signIn(t, s, "register", "first")
	list := accountRequest(s, "GET", "/games.json", nil, a)
	if list.Code != 200 || list.Header().Get("Cache-Control") != "no-store" || strings.Contains(list.Body.String(), "private") || !strings.Contains(list.Body.String(), "checkpoint") {
		t.Fatal("shared list or cache policy")
	}
	for _, action := range []struct{ method, path string }{{"GET", "/ext/private.zip"}, {"POST", "/api/games?name=other.zip"}, {"DELETE", "/api/games?game=ext/private.zip"}} {
		if got := accountRequest(s, action.method, action.path, []byte("bytes"), a); got.Code != 404 {
			t.Errorf("shared %s %s: %d", action.method, action.path, got.Code)
		}
	}
	if got := accountRequest(s, "GET", "/games/ktf/checkpoint.zip", nil, a); got.Code != 200 || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authenticated archive read/cache policy")
	}
	cross := httptest.NewRequest("POST", "https://shared.example/api/logout", nil)
	cross.Header.Set("Origin", "https://other.example")
	cross.AddCookie(a)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, cross)
	if rec.Code != 403 {
		t.Errorf("cross-site logout: %d", rec.Code)
	}
}

func TestSharedSaveRootsAndLoginRevocation(t *testing.T) {
	s, addr := sharedHost(t)
	a, b := signIn(t, s, "register", "first"), signIn(t, s, "register", "second")
	other := signIn(t, s, "login", "first")
	for _, path := range []string{"/api/saves/owner", "/api/saves/ktf/owner", "/api/saves/skt/owner"} {
		if got := accountRequest(s, "PUT", path+"/state", []byte("a"), a); got.Code != 204 {
			t.Fatalf("put: %d", got.Code)
		}
		if got := accountRequest(s, "GET", path, nil, b); !strings.Contains(got.Body.String(), `"saves":{}`) {
			t.Fatal("another account's ordinary saves visible")
		}
	}
	first := accountSocket(t, addr, a)
	second := accountSocket(t, addr, b)
	const token = "11223344556677889900112233445566"
	send(t, first, clientMessage{Kind: clientStart, Game: "games/skt/checkpoint.zip", Token: token})
	expectMessage(t, first, serverStarted)
	expectFrame(t, first)
	send(t, second, clientMessage{Kind: clientResume, Token: token, Takeover: true})
	if reply := expectMessage(t, second, serverResumed); reply.Resumed || reply.Occupied {
		t.Fatalf("foreign token found an instance")
	}
	if got := accountRequest(s, "POST", "/api/logout", nil, a); got.Code/100 != 2 {
		t.Fatalf("logout: %d", got.Code)
	}
	waitForParked(t, s, 1)
	if got := accountRequest(s, "GET", "/games.json", nil, a); got.Code != 401 {
		t.Fatal("logout token still accepted")
	}
	returning := accountSocket(t, addr, other)
	send(t, returning, clientMessage{Kind: clientResume, Token: "99887766554433221100998877665544"})
	expectMessage(t, returning, serverStarted)
	expectFrame(t, returning)
}

func TestLoginExpiryClosesOnlyItsSockets(t *testing.T) {
	s, addr := sharedHost(t)
	first := signIn(t, s, "register", "player")
	second := signIn(t, s, "login", "player")
	// Give the first login a short lifetime before its sockets subscribe.
	db, err := sql.Open("sqlite3", filepath.Join(filepath.Dir(s.gameRoot), "accounts.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	token := sha256.Sum256([]byte(first.Value))
	if _, err = db.Exec("UPDATE login_sessions SET expires_at=? WHERE token_digest=?", time.Now().Add(3*time.Second).Unix(), token[:]); err != nil {
		t.Fatal(err)
	}
	phone, idle := accountSocket(t, addr, first), accountSocket(t, addr, first)
	desktop := accountSocket(t, addr, second)
	send(t, phone, clientMessage{Kind: clientStart, Game: sharedFixtureGame})
	expectMessage(t, phone, serverStarted)
	expectFrame(t, phone)
	for _, socket := range []*wsproto.Conn{idle, phone} {
		expectSocketClosed(t, socket)
	}
	waitForParked(t, s, 1)
	if got := accountRequest(s, "GET", "/games.json", nil, first); got.Code != 401 {
		t.Fatalf("expired login: %d", got.Code)
	}
	send(t, desktop, clientMessage{Kind: clientResume})
	expectMessage(t, desktop, serverStarted)
	expectFrame(t, desktop)
}

func TestAccountTreesStayBesideACustomPersonalRoot(t *testing.T) {
	s := newTestServer(t, Options{SaveRoot: filepath.Join(t.TempDir(), "Users")})
	legacy := s.pathsFor(accounts.User{ID: "one", Legacy: true})
	other := s.pathsFor(accounts.User{ID: "two"})
	relative, err := filepath.Rel(string(legacy), string(other))
	if err != nil || filepath.IsLocal(relative) {
		t.Fatal("a bound account could traverse another account's saves")
	}
}

func TestRelativeSharedSaveRootKeepsAllPlatformsInTheAccount(t *testing.T) {
	shell, err := filepath.Abs(filepath.Join("..", "..", "web"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for _, base := range []string{".", "..", "custom-ktf"} {
		s := newTestServer(t, Options{Accounts: new(accounts.Store), SaveRoot: base, Client: os.DirFS(shell)})
		a := s.pathsFor(accounts.User{ID: "first"})
		b := s.pathsFor(accounts.User{ID: "second"})
		for platform := range platforms {
			if a.saveDirectory(platform, "owner") == b.saveDirectory(platform, "owner") {
				t.Fatalf("%q exposes %s saves", base, platform)
			}
		}
	}
}

func TestSharedListingAndOpenUseTheSameLibrary(t *testing.T) {
	s, _ := sharedHost(t)
	nested := filepath.Join(s.gameRoot, "group", "nested")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "unlisted.zip"), []byte("outside picker"), 0600); err != nil {
		t.Fatal(err)
	}
	page := signIn(t, s, "register", "player")
	if res := accountRequest(s, "GET", "/games/group/nested/unlisted.zip", nil, page); res.Code != 403 {
		t.Fatalf("archive below a picker group: %d", res.Code)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(s.gameRoot), "ext"), filepath.Join(s.gameRoot, "linked")); err != nil {
		t.Skipf("cannot create a test symlink: %v", err)
	}
	got := accountRequest(s, "GET", "/games.json", nil, page)
	if got.Code != 200 || strings.Contains(got.Body.String(), "private") {
		t.Fatalf("unexpected library: %s", got.Body.String())
	}
	if res := accountRequest(s, "GET", "/games/linked/private.zip", nil, page); res.Code != 404 {
		t.Fatalf("outside archive: %d", res.Code)
	}
}

func TestIdleAccountSocketPicksUpLocalSaveBinding(t *testing.T) {
	s, addr := sharedHost(t)
	page := signIn(t, s, "register", "player")
	conn := accountSocket(t, addr, page)
	if err := s.accounts.BindLegacy(t.Context(), "player"); err != nil {
		t.Fatal(err)
	}
	send(t, conn, clientMessage{Kind: clientStart, Game: sharedFixtureGame, ID: 1})
	started := expectMessage(t, conn, serverStarted)
	path := "/api/saves/skt/" + url.PathEscape(started.Started.SaveOwner) + "/test"
	if got := accountRequest(s, "PUT", path, []byte("busy"), page); got.Code != 409 {
		t.Fatalf("HTTP and the existing socket chose different save roots: %d", got.Code)
	}
}
