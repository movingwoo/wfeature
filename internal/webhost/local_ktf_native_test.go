package webhost

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/testlocal"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

// The carrier's earlier download package has to reach a browser through the
// same protocol as everything else, and the only way to know it does is to
// drive it: a real handshake, the real archive, real frames, and no browser
// anywhere. It is opt-in because real games are ignored local data.
func TestLocalSessionRunsTheEarlierKTFPackage(t *testing.T) {
	testlocal.Enabled(t, "WFEATURE_KTF_NATIVE_ACCEPTANCE", "run the ignored local KTF native package")
	archive, name := localKTFNativePackage(t)
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "ktf"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "ktf", name), archive, 0o644); err != nil {
		t.Fatalf("write the archive: %v", err)
	}
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "ktf"),
		LogRoot:  filepath.Join(root, "logs"),
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
	if err != nil {
		t.Fatalf("dial the session: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/ktf/" + name})
	started := expectMessage(t, connection, serverStarted)
	if started.Started == nil {
		t.Fatal("the started message carries no description")
	}
	if started.Started.Platform != "ktf" {
		t.Errorf("platform = %q, want ktf", started.Started.Platform)
	}
	if started.Started.Width != 240 || started.Started.Height != 320 {
		t.Errorf("screen = %dx%d, want 240x320", started.Started.Width, started.Started.Height)
	}
	// This platform runs on a memory image with instrumented stores, so the
	// panel's write-watch control means something here and is offered.
	if !started.Started.CanWatch {
		t.Error("a WIPI session said it cannot watch writes; the panel would hide a control that works")
	}
	// Without an owner the session would silently play with no persistence,
	// and this package's owner comes out of its module information file.
	if started.Started.SaveOwner == "" {
		t.Error("no save owner")
	}
	width, height := expectFrame(t, connection)
	if width != 240 || height != 320 {
		t.Fatalf("frame is %dx%d, want 240x320", width, height)
	}
	// The key the page sends for fire is what opens this title's own menu, so
	// a further picture after it is input reaching the game.
	send(t, connection, clientMessage{Kind: clientKey, Action: "press", Code: 148})
	send(t, connection, clientMessage{Kind: clientKey, Action: "release", Code: 148})
	expectFrame(t, connection)
}

// localKTFNativePackage finds the earlier KTF package in the ignored local
// game directory, with the name to serve it under.
func localKTFNativePackage(t *testing.T) ([]byte, string) {
	t.Helper()
	directory := filepath.Join("..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Skipf("read the local KTF game directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".zip" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if ktf.IsNativeArchive(data) {
			// The archive's own name is EUC-KR and the served path is a URL,
			// so it is served under a plain one.
			return data, "earlier-package.zip"
		}
	}
	t.Skip("no local KTF native package present")
	return nil, ""
}
