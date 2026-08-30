package webhost

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// TestLocalStartEndingProbe drives the two-run shape a browser sees when a
// title installs itself.
//
// A title whose first run is an installer quits inside `startApp` on its
// *second* run: it reads the flag it wrote and there is nothing left to do.
// The first run is an ordinary session that ends on a tick; the second never
// gets a session at all, and the page has to be told that what refused its
// request was an ending rather than a failure.
//
// No archive in this repository does that — the packaged fixtures are a MIDlet
// that runs — so this is env-gated like the other local probes, and it needs a
// KTF archive of that shape:
//
//	WFEATURE_ENDING_ARCHIVE=/abs/path/game.zip \
//	go test ./internal/webhost -run TestLocalStartEndingProbe -v
//
// It is a probe rather than a unit test because the change is two-ended: the
// session layer telling an ending from a failure, and the page reading the
// mark. `session.TestAGuestThatEndedDuringItsStartIsAnEnding` pins the first
// and `web/session.test.mjs` the second; this is the seam between them.
func TestLocalStartEndingProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_ENDING_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_ENDING_ARCHIVE to a local archive whose first run installs it")
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "local", "game.zip"), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	// One save root across both connections: the second run is only the second
	// run because it reads what the first one wrote.
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata"),
		LogRoot:  filepath.Join(root, "logs"),
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	dial := func() *wsproto.Conn {
		t.Helper()
		connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = connection.Close() })
		return connection
	}

	first := dial()
	expectMessage(t, first, serverReady)
	send(t, first, clientMessage{Kind: clientStart, Game: "games/local/game.zip"})
	expectMessage(t, first, serverStarted)
	ending := expectMessage(t, first, serverExited)
	t.Logf("first run ended: %s", ending.Message)
	_ = first.Close()

	second := dial()
	expectMessage(t, second, serverReady)
	send(t, second, clientMessage{Kind: clientStart, Game: "games/local/game.zip"})
	answer := expectMessage(t, second, serverError)
	if !answer.Exited {
		t.Fatalf("the second run was refused as a failure: %s", answer.Message)
	}
	t.Logf("second run refused as an ending: %s", answer.Message)
}
