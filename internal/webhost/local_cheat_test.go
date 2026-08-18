package webhost

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// TestLocalCheatProbe drives the browser's cheat path against a real archive.
//
// The packaged fixtures are a MIDlet, which has no guest address space and so
// answers the refusal the unit tests pin. What the panel actually does — scan
// a running ARM game's memory and get candidates back — needs a game, and no
// archive in this repository is one. So this is env-gated like the other local
// probes:
//
//	WFEATURE_CHEAT_ARCHIVE=/abs/path/game.zip \
//	go test ./internal/webhost -run TestLocalCheatProbe -v
//
// It exists because the protocol change that reached LGT is a two-ended one:
// the runner resolving an engine off the session, and the page deciding a
// panel belongs on this platform. A build that compiles proves neither.
func TestLocalCheatProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_CHEAT_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_CHEAT_ARCHIVE to a local game archive")
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
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata"),
		LogRoot:  filepath.Join(root, "logs"),
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/local/game.zip"})
	started := expectMessage(t, connection, serverStarted)
	t.Logf("platform %s", started.Started.Platform)

	// The console answers the same vocabulary on every platform, and `regions`
	// is the one command whose answer proves an address space was actually
	// reached rather than a message composed about one. On the MIDP runtime
	// that space is the synthetic one over its object graph, and the listing
	// names classes where the ARM platforms name a module and a heap.
	send(t, connection, clientMessage{Kind: clientCheat, ID: 2, Command: "regions"})
	regions := expectMessage(t, connection, serverResult)
	if !strings.Contains(regions.Message, "region(s)") {
		t.Fatalf("regions answered %q", regions.Message)
	}
	t.Logf("regions:\n%s", regions.Message)

	// A scan is the panel's own path rather than the console's, and an
	// unknown-value scan is the one that runs before anything is known.
	send(t, connection, clientMessage{Kind: clientCheat, ID: 3, Op: "scan", Type: "u32", Filter: "unknown"})
	scan := expectMessage(t, connection, serverResult)
	if scan.Cheat == nil {
		t.Fatalf("scan answered no cheat result: %+v", scan)
	}
	if !scan.Cheat.Searchable || scan.Cheat.Count == 0 {
		t.Fatalf("scan answered searchable=%v count=%d", scan.Cheat.Searchable, scan.Cheat.Count)
	}
	t.Logf("scan candidates: %d", scan.Cheat.Count)
}
