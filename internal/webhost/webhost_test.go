package webhost

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// newTestServer serves the real PWA shell unless a test says otherwise: what
// the page needs has to actually be served, and a fixture would only prove the
// fixture was served.
func newTestServer(t *testing.T, options Options) *Server {
	t.Helper()
	if options.Client == nil {
		options.Client = os.DirFS(filepath.Join("..", "..", "web"))
	}
	server, err := New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return server
}

func get(t *testing.T, server *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestServesTheInstallableShell(t *testing.T) {
	server := newTestServer(t, Options{})
	for _, test := range []struct {
		target      string
		contentType string
		contains    string
	}{
		{"/", "text/html; charset=utf-8", "wfeature"},
		{"/index.html", "text/html; charset=utf-8", "wfeature"},
		{"/manifest.webmanifest", "application/manifest+json; charset=utf-8", `"standalone"`},
		{"/icon-512.png", "image/png", "PNG"},
		{"/style.css", "text/css; charset=utf-8", ""},
		{"/app.js", "text/javascript; charset=utf-8", ""},
	} {
		t.Run(test.target, func(t *testing.T) {
			recorder := get(t, server, test.target)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d", recorder.Code)
			}
			if got := recorder.Header().Get("Content-Type"); got != test.contentType {
				t.Errorf("Content-Type = %q, want %q", got, test.contentType)
			}
			// The shell changes with every build, so it is never cached; the
			// service worker is what makes the page load offline.
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q", got)
			}
			if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q", got)
			}
			if test.contains != "" && !strings.Contains(recorder.Body.String(), test.contains) {
				t.Errorf("body does not contain %q", test.contains)
			}
		})
	}
}

func TestDoesNotServeOutsideTheClientRoot(t *testing.T) {
	server := newTestServer(t, Options{})
	// The handler is registered without a mux, so nothing has folded these
	// paths before they arrive.
	for _, target := range []string{"/..%2fgo.mod", "/../go.mod", "/%2e%2e/%2e%2e/go.mod", "/subdir/..%2f..%2fgo.mod"} {
		if recorder := get(t, server, target); recorder.Code == http.StatusOK {
			t.Errorf("%s was served", target)
		}
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	server := newTestServer(t, Options{})
	if recorder := get(t, server, "/api/other"); recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestListsAndServesTheArchivesThePickerOffers(t *testing.T) {
	gameRoot := t.TempDir()
	for _, entry := range []struct{ group, name, content string }{
		{"skt", "테스트게임.zip", "PK"},
		{"skt", "메모.txt", "not a game"},
		{"ktf", "샘플게임.zip", "PK"},
		// An SKT title can arrive as a bare jar rather than the archive a
		// handset was sent, and the picker has to offer it too: which platform
		// loads a file is the engine's answer from the bytes, not the
		// extension's.
		{"skt", "미들렛.jar", "PK"},
		// A game dropped into the root rather than filed under a platform is
		// still offered: the packaged tree ships a README there too, so the
		// root is filtered by extension exactly like a platform directory.
		{"", "루트게임.zip", "PK"},
		{"", "README.txt", "put games here"},
	} {
		directory := filepath.Join(gameRoot, entry.group)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(directory, entry.name), []byte(entry.content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	server := newTestServer(t, Options{GameRoot: gameRoot})

	recorder := get(t, server, "/games.json")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	var listed []Game
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode games.json: %v", err)
	}
	want := []Game{
		{Group: "ktf", Name: "샘플게임", Path: "games/ktf/" + url.PathEscape("샘플게임.zip")},
		{Group: "skt", Name: "미들렛", Path: "games/skt/" + url.PathEscape("미들렛.jar")},
		{Group: "skt", Name: "테스트게임", Path: "games/skt/" + url.PathEscape("테스트게임.zip")},
		{Group: "", Name: "루트게임", Path: "games/" + url.PathEscape("루트게임.zip")},
	}
	if len(listed) != len(want) {
		t.Fatalf("listed %d games, want %d: %+v", len(listed), len(want), listed)
	}
	for index, game := range listed {
		if game != want[index] {
			t.Errorf("game %d = %+v, want %+v", index, game, want[index])
		}
	}

	archive := get(t, server, "/"+want[0].Path)
	if archive.Code != http.StatusOK {
		t.Fatalf("archive status = %d", archive.Code)
	}
	if got := archive.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("archive Content-Type = %q", got)
	}
	// Archives are tens of megabytes, so a relaunch revalidates instead of
	// downloading the same file again.
	if got := archive.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("archive Cache-Control = %q", got)
	}
	tag := archive.Header().Get("ETag")
	if !strings.HasPrefix(tag, `"`) || !strings.Contains(tag, "-") {
		t.Fatalf("ETag = %q", tag)
	}

	revalidate := httptest.NewRequest(http.MethodGet, "/"+want[0].Path, nil)
	revalidate.Header.Set("If-None-Match", tag)
	cached := httptest.NewRecorder()
	server.ServeHTTP(cached, revalidate)
	if cached.Code != http.StatusNotModified {
		t.Fatalf("revalidation status = %d, want 304", cached.Code)
	}

	midlet := get(t, server, "/"+want[1].Path)
	if got := midlet.Header().Get("Content-Type"); got != "application/java-archive" {
		t.Errorf("MIDlet Content-Type = %q", got)
	}

	// The ungrouped archive is one path component rather than two, which is the
	// shape the archive route has to accept for the picker's offer to load.
	if root := get(t, server, "/"+want[3].Path); root.Code != http.StatusOK {
		t.Fatalf("root archive status = %d", root.Code)
	}
}

func TestGameListIsEmptyWithoutAGameRoot(t *testing.T) {
	// A fresh install has no games yet, and the page still has to load so the
	// user can see where to put them.
	server := newTestServer(t, Options{GameRoot: filepath.Join(t.TempDir(), "missing")})
	recorder := get(t, server, "/games.json")
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != "[]" {
		t.Fatalf("status %d body %q, want 200 []", recorder.Code, recorder.Body.String())
	}
}

func TestDoesNotServeArchivesOutsideTheGameRoot(t *testing.T) {
	server := newTestServer(t, Options{GameRoot: t.TempDir()})
	for _, target := range []string{"/games/%2e%2e/escape.zip", "/games/skt/%2e%2e/%2e%2e/escape.zip", "/games/"} {
		if recorder := get(t, server, target); recorder.Code == http.StatusOK {
			t.Errorf("%s was served", target)
		}
	}
}

func TestSaveAPIRoundTripsInTheDirectoryStoreLayout(t *testing.T) {
	saveRoot := filepath.Join(t.TempDir(), "ktf")
	server := newTestServer(t, Options{SaveRoot: saveRoot})

	empty := get(t, server, "/api/saves/0102DD43")
	if empty.Code != http.StatusOK {
		t.Fatalf("status = %d", empty.Code)
	}
	if got := strings.TrimSpace(empty.Body.String()); got != `{"saves":{}}` {
		t.Fatalf("body = %q", got)
	}

	payload := []byte{1, 2, 3, 4}
	if code := put(t, server, "/api/saves/0102DD43/db/dlex00.dat", payload); code != http.StatusNoContent {
		t.Fatalf("PUT status = %d", code)
	}
	stored, err := os.ReadFile(filepath.Join(saveRoot, "0102DD43", "db", "dlex00.dat"))
	if err != nil || string(stored) != string(payload) {
		t.Fatalf("stored = %v, %v", stored, err)
	}

	listed := get(t, server, "/api/saves/0102DD43")
	var response saveResponse
	if err := json.Unmarshal(listed.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Saves["db/dlex00.dat"] != base64.StdEncoding.EncodeToString(payload) {
		t.Fatalf("saves = %v", response.Saves)
	}

	// A guest names a database the way it names a file — one opens
	// "./OptionSave" — so the key normalizes to the entry the directory store
	// writes instead of being rejected. The dot is percent-encoded because
	// URL parsing would otherwise fold it out before the handler sees it.
	if code := put(t, server, "/api/saves/0102DD43/db/%2e/slot.sav", payload); code != http.StatusNoContent {
		t.Fatalf("dotted key status = %d", code)
	}
	if _, err := os.Stat(filepath.Join(saveRoot, "0102DD43", "db", "slot.sav")); err != nil {
		t.Fatalf("dotted key did not normalize: %v", err)
	}
}

func put(t *testing.T, server *Server, target string, body []byte) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, target, strings.NewReader(string(body))))
	return recorder.Code
}

func TestSaveAPIRejectsTraversalAndSteeringOwners(t *testing.T) {
	server := newTestServer(t, Options{SaveRoot: filepath.Join(t.TempDir(), "ktf")})
	if code := put(t, server, "/api/saves/0102DD43/..%2fescape", []byte{1}); code != http.StatusBadRequest {
		t.Errorf("traversal key status = %d, want 400", code)
	}
	if recorder := get(t, server, "/api/saves/%2e%2e"); recorder.Code == http.StatusOK {
		t.Error("a traversal owner was accepted")
	}
	// A space is a legal owner: SKT titles are keyed by MIDlet-Name,
	// which carries spaces and Korean. What is rejected is what could steer a
	// path.
	for _, owner := range []string{"Sky%20Force", "%EC%98%81%EC%9B%85%EC%84%9C%EA%B8%B0"} {
		if recorder := get(t, server, "/api/saves/"+owner); recorder.Code != http.StatusOK {
			t.Errorf("owner %s status = %d, want 200", owner, recorder.Code)
		}
	}
	for _, owner := range []string{"a%5Cb", "a%00b", "a%3Ab", "a%3Fb", strings.Repeat("a", 65)} {
		if recorder := get(t, server, "/api/saves/"+owner); recorder.Code != http.StatusBadRequest {
			t.Errorf("owner %s status = %d, want 400", owner, recorder.Code)
		}
	}
}

func TestSaveAPIRoutesAPlatformSegmentToThatTree(t *testing.T) {
	root := t.TempDir()
	server := newTestServer(t, Options{SaveRoot: filepath.Join(root, "ktf")})

	// The platform-less form is KTF, the contract the page already used; a
	// platform segment reroots to a sibling directory.
	if code := put(t, server, "/api/saves/Sky%20Force/rms/scores", []byte{7}); code != http.StatusNoContent {
		t.Fatalf("ktf status = %d", code)
	}
	if code := put(t, server, "/api/saves/lgt/Sky%20Force/rms/scores", []byte{9}); code != http.StatusNoContent {
		t.Fatalf("lgt status = %d", code)
	}
	for _, test := range []struct {
		target string
		want   byte
	}{
		{"/api/saves/Sky%20Force", 7},
		{"/api/saves/lgt/Sky%20Force", 9},
	} {
		var response saveResponse
		if err := json.Unmarshal(get(t, server, test.target).Body.Bytes(), &response); err != nil {
			t.Fatalf("decode %s: %v", test.target, err)
		}
		decoded, err := base64.StdEncoding.DecodeString(response.Saves["rms/scores"])
		if err != nil || len(decoded) != 1 || decoded[0] != test.want {
			t.Fatalf("%s stored %v (%v), want [%d]", test.target, decoded, err, test.want)
		}
	}
}

func TestSaveAPIRefusesOtherMethods(t *testing.T) {
	server := newTestServer(t, Options{SaveRoot: filepath.Join(t.TempDir(), "ktf")})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/saves/0102DD43/db/x", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}

func TestSaveAPIRefusesABodyOverTheLimit(t *testing.T) {
	// Truncating would store a save the game later fails to read, which is
	// worse than refusing the write.
	server := newTestServer(t, Options{SaveRoot: filepath.Join(t.TempDir(), "ktf")})
	recorder := httptest.NewRecorder()
	body := strings.NewReader(strings.Repeat("x", maxRequestBody+1))
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/saves/0102DD43/db/big", body))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestDebugLogNamesFilesWithoutTrustingTheLabel(t *testing.T) {
	now := time.Date(2026, 8, 10, 15, 4, 5, 678*int(time.Millisecond), time.UTC)
	for _, test := range []struct{ label, want string }{
		{"../../etc/passwd", "2026-08-10T15-04-05-678-etcpasswd.log"},
		{"테스트게임", "2026-08-10T15-04-05-678-테스트게임.log"},
		{"  ..hidden  ", "2026-08-10T15-04-05-678-hidden.log"},
		{"crash\u0000\u202edrop", "2026-08-10T15-04-05-678-crashdrop.log"},
		{`C:\Windows\*?<>|`, "2026-08-10T15-04-05-678-CWindows.log"},
		{"", "2026-08-10T15-04-05-678.log"},
	} {
		if got := DebugLogName(test.label, now); got != test.want {
			t.Errorf("DebugLogName(%q) = %q, want %q", test.label, got, test.want)
		}
	}
	long := DebugLogName(strings.Repeat("a", 200), now)
	if want := len("2026-08-10T15-04-05-678-.log") + maxLabelRunes; len(long) != want {
		t.Errorf("a long label produced %d bytes, want %d", len(long), want)
	}
}

func TestDebugLogStoresAReportUnderAServerChosenName(t *testing.T) {
	if !backend.DebugBuild() {
		t.Skip("a release collects no reports; TestARELeaseServesNoDebugLogRoute is that half")
	}
	logRoot := filepath.Join(t.TempDir(), "logs")
	server := newTestServer(t, Options{LogRoot: logRoot})
	report := "wfeature debug report\nprofile: debug\n"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
		"/api/debug-log?label=%ED%85%8C%EC%8A%A4%ED%8A%B8", strings.NewReader(report)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var stored struct{ Name, Path string }
	if err := json.Unmarshal(recorder.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasSuffix(stored.Name, ".log") || !strings.Contains(stored.Name, "테스트") {
		t.Fatalf("name = %q", stored.Name)
	}
	content, err := os.ReadFile(filepath.Join(logRoot, stored.Name))
	if err != nil || string(content) != report {
		t.Fatalf("stored report = %q, %v", content, err)
	}
}

func TestDebugLogRefusesEverythingButABoundedPost(t *testing.T) {
	if !backend.DebugBuild() {
		t.Skip("a release collects no reports; TestARELeaseServesNoDebugLogRoute is that half")
	}
	server := newTestServer(t, Options{LogRoot: filepath.Join(t.TempDir(), "logs")})
	if recorder := get(t, server, "/api/debug-log"); recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", recorder.Code)
	}
	for _, body := range []io.Reader{strings.NewReader(""), strings.NewReader(strings.Repeat("x", maxDebugReport+1))} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/debug-log", body))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", recorder.Code)
		}
	}
}

// The route is the debug profile's. A release serves a page with no report
// button and no console capture behind it, so a post here is not the page it
// serves and is answered the way an address nobody serves is.
func TestAReleaseServesNoDebugLogRoute(t *testing.T) {
	if backend.DebugBuild() {
		t.Skip("this build collects reports; the tests above are that half")
	}
	logRoot := filepath.Join(t.TempDir(), "logs")
	server := newTestServer(t, Options{LogRoot: logRoot})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/debug-log",
		strings.NewReader("wfeature page log\n")))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if _, err := os.Stat(logRoot); !os.IsNotExist(err) {
		t.Fatalf("a release created the log directory anyway (%v)", err)
	}
}

// Nothing authenticates this route and the server binds every interface, so
// the reports a caller can leave behind are bounded in number as well as in
// size. The limit is far above what pressing the button produces.
func TestDebugLogRefusesReportsOverTheRate(t *testing.T) {
	if !backend.DebugBuild() {
		t.Skip("a release collects no reports")
	}
	server := newTestServer(t, Options{LogRoot: filepath.Join(t.TempDir(), "logs")})
	post := func() int {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/debug-log",
			strings.NewReader("wfeature page log\n")))
		return recorder.Code
	}
	for attempt := 0; attempt < debugLogBurst; attempt++ {
		if code := post(); code != http.StatusOK {
			t.Fatalf("report %d answered %d, want 200", attempt, code)
		}
	}
	if code := post(); code != http.StatusTooManyRequests {
		t.Fatalf("the report over the rate answered %d, want 429", code)
	}
	// The window is rolling: what was accepted a window ago does not count
	// against a report now.
	server.debugLogMu.Lock()
	for index := range server.debugLogPosts {
		server.debugLogPosts[index] = server.debugLogPosts[index].Add(-2 * debugLogWindow)
	}
	server.debugLogMu.Unlock()
	if code := post(); code != http.StatusOK {
		t.Fatalf("a report after the window answered %d, want 200", code)
	}
}

// The directory a server writes reports into is on somebody's machine and the
// server is left running for weeks, so what it keeps is bounded by age and by
// size. The newest report survives either bound: it is the one just written.
func TestReportsArePrunedByAgeAndByBudget(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	write := func(at time.Time, size int) string {
		name := DebugLogName("", at)
		if err := os.WriteFile(filepath.Join(root, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		return name
	}
	// One report past its life, one inside it, and a file this server did not
	// name, which is the user's and is never touched.
	expired := write(now.Add(-debugLogLife-time.Hour), 16)
	kept := write(now.Add(-time.Hour), 16)
	newest := write(now, 16)
	theirs := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(theirs, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed := pruneReports(root, now); removed != 1 {
		t.Fatalf("pruned %d reports, want 1", removed)
	}
	if _, err := os.Stat(filepath.Join(root, expired)); !os.IsNotExist(err) {
		t.Errorf("the expired report is still there (%v)", err)
	}
	for _, name := range []string{kept, newest} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s was dropped: %v", name, err)
		}
	}
	if _, err := os.Stat(theirs); err != nil {
		t.Errorf("a file this server did not name was removed: %v", err)
	}

	// Over budget, the oldest go first and the newest stays whatever the
	// budget says.
	big := debugLogBudget/2 + 1
	first := write(now.Add(-3*time.Minute), big)
	second := write(now.Add(-2*time.Minute), big)
	third := write(now.Add(-time.Minute), big)
	if removed := pruneReports(root, now); removed < 2 {
		t.Fatalf("pruned %d reports over the budget, want at least 2", removed)
	}
	for _, name := range []string{first, second} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s outlived the budget (%v)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, third)); err != nil {
		t.Errorf("the newest report was dropped for the budget: %v", err)
	}
}

func TestNewRequiresClientFiles(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("a server was built with no client files")
	}
}

// A user who downloads only the executable still has to be able to read the
// terms the bundled components arrive under, so the server answers for them
// without needing the repository.
func TestServesTheLicences(t *testing.T) {
	server := newTestServer(t, Options{})

	recorder := get(t, server, "/licenses")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /licenses answered %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, required := range []string{
		"MIT License",
		"The Go Authors",
		"Eunbin Jeong",
		"Reserved Font Name",
		"Christopher Serr",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("the served licences do not carry %q", required)
		}
	}
}

// The profile is the binary that is running, and from outside the process there
// is nothing else to read it from: two servers on two ports look alike, and the
// executable's path says nothing when it was started with `go run` or renamed
// into a release archive. The stop and status scripts ask this route instead,
// which is also how they tell this server from a stranger holding the port.
func TestStatusNamesTheServerItsProfileAndVersion(t *testing.T) {
	server := newTestServer(t, Options{Version: "1.2.3"})

	recorder := get(t, server, "/api/status")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/status answered %d", recorder.Code)
	}
	var status struct {
		Server  string `json:"server"`
		Profile string `json:"profile"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("the status is not JSON: %v", err)
	}
	if status.Server != "wfeature" {
		t.Errorf("server = %q, want wfeature: the scripts match on this to tell a stranger apart", status.Server)
	}
	if status.Profile != backend.BuildProfile() {
		t.Errorf("profile = %q, want %q", status.Profile, backend.BuildProfile())
	}
	if status.Version != "1.2.3" {
		t.Errorf("version = %q, want the one the binary was stamped with", status.Version)
	}
}

// An unstamped build is every build that is not a release, so the field says
// what the CLI's -version says rather than nothing at all.
func TestStatusCallsAnUnstampedBuildDev(t *testing.T) {
	server := newTestServer(t, Options{})

	recorder := get(t, server, "/api/status")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/status answered %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"version":"dev"`) {
		t.Errorf("status = %s, want a dev version", recorder.Body.String())
	}
}

// The pid is what a stop signals when the polite route cannot be used — an
// older build, or a server too wedged to serve — so the status has to carry it.
func TestStatusNamesItsOwnProcess(t *testing.T) {
	server := newTestServer(t, Options{})
	recorder := get(t, server, "/api/status")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/status answered %d", recorder.Code)
	}
	var status struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("the status is not JSON: %v", err)
	}
	if status.PID != os.Getpid() {
		t.Errorf("pid = %d, want this process %d", status.PID, os.Getpid())
	}
}

// Stopping is what the launchers ask for instead of finding the process behind
// a port. The route has to be there when the host installed a way to stop,
// absent when it did not, and closed to everything off this machine.
func TestShutdownIsLocalOnlyAndAsksTheHost(t *testing.T) {
	post := func(server *Server, from string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/shutdown", nil)
		request.RemoteAddr = from
		server.ServeHTTP(recorder, request)
		return recorder
	}

	// A host that installed no way to stop does not serve the route at all.
	silent := newTestServer(t, Options{})
	if recorder := post(silent, "127.0.0.1:5000"); recorder.Code != http.StatusNotFound {
		t.Errorf("a server with no shutdown hook answered %d, want 404", recorder.Code)
	}

	asked := make(chan struct{}, 1)
	server := newTestServer(t, Options{RequestShutdown: func() { asked <- struct{}{} }})

	if recorder := get(t, server, "/api/shutdown"); recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/shutdown answered %d, want 405", recorder.Code)
	}

	// The server binds every interface so a phone can play; a stop from that
	// phone would be a way to end somebody's game from the next room.
	for _, remote := range []string{"192.168.0.42:5000", "[2001:db8::1]:5000"} {
		if recorder := post(server, remote); recorder.Code != http.StatusForbidden {
			t.Errorf("a shutdown from %s answered %d, want 403", remote, recorder.Code)
		}
	}
	select {
	case <-asked:
		t.Fatal("a shutdown from off this machine reached the host")
	default:
	}

	for _, remote := range []string{"127.0.0.1:5000", "[::1]:5000"} {
		recorder := post(server, remote)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("a shutdown from %s answered %d, want 202", remote, recorder.Code)
		}
		select {
		case <-asked:
		case <-time.After(2 * time.Second):
			t.Fatalf("a shutdown from %s never reached the host", remote)
		}
	}
}
