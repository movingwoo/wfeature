package appserver

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// The embedded server is the whole of what a phone app runs, so what this
// checks is that starting it is enough: the page is there, the game root is
// the one it was given, and the port came back so the app can point a web view
// at it.
func TestStartServesThePageAndTakesAPort(t *testing.T) {
	root := t.TempDir()
	server, err := Start(Options{Root: root})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	if server.Port() == 0 {
		t.Fatal("no port was taken")
	}
	if want := "http://127.0.0.1:"; !strings.HasPrefix(server.URL(), want) {
		t.Errorf("URL() = %q", server.URL())
	}

	// Start returns once the port is open, so this needs no polling: an app
	// that had to wait would be an app that shows a blank web view first.
	response, err := http.Get(server.URL() + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "wfeature") {
		t.Fatalf("GET / = %d: %.80s", response.StatusCode, body)
	}

	// A game put under the root it was given is a game the picker lists, which
	// is what makes the app's container the library.
	if err := os.MkdirAll(filepath.Join(root, "games"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "games", "a game.zip"), []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	listed, err := http.Get(server.URL() + "/games.json")
	if err != nil {
		t.Fatal(err)
	}
	defer listed.Body.Close()
	var games []struct{ Name string }
	if err := json.NewDecoder(listed.Body).Decode(&games); err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].Name != "a game" {
		t.Fatalf("the picker lists %+v", games)
	}
}

// **It binds loopback and nothing else.** The phone's own container is behind
// this port, and a server on the Wi-Fi would be that container offered to the
// room with nothing in front of it.
func TestTheEmbeddedServerIsNotOnTheNetwork(t *testing.T) {
	server, err := Start(Options{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	if got := server.httpServer.Addr; got != "" {
		t.Errorf("Addr = %q", got)
	}
	// The listener's own address is the proof: a server bound to every
	// interface answers on 0.0.0.0, and this one may not.
	response, err := http.Get(server.URL() + "/api/status")
	if err != nil {
		t.Fatalf("loopback: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestStartRefusesWithNoRoot(t *testing.T) {
	if _, err := Start(Options{}); err == nil {
		t.Fatal("a server started with nowhere to keep anything")
	}
}

// Closing twice is what an app does when it is torn down twice, and it must
// not be the thing that crashes the app.
func TestCloseIsSafeTwice(t *testing.T) {
	server, err := Start(Options{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestEmbeddedServerReportsItsReleaseVersion(t *testing.T) {
	for _, version := range []string{"", "0.4.2"} {
		t.Run(version, func(t *testing.T) {
			server, err := Start(Options{Root: t.TempDir(), Version: version})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close() })
			response, err := http.Get(server.URL() + "/api/status")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var status struct {
				Version string `json:"version"`
			}
			if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
				t.Fatal(err)
			}
			want := version
			if want == "" {
				want = "dev"
			}
			if response.StatusCode != http.StatusOK || status.Version != want {
				t.Fatalf("status = %d, version = %q, want %q", response.StatusCode, status.Version, want)
			}
		})
	}
}

// The app's own page is attached to a session socket for as long as a game is
// on the screen, and the HTTP shutdown does not reach that socket. Closing the
// embedded server therefore has to close the game itself, or an app that is
// being put away takes with it whatever the title had written and not yet
// handed to the save store.
func TestCloseEndsAnAttachedSession(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "games", "skt"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "canvas-skt.zip"))
	if err != nil {
		t.Fatalf("read the canvas fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "games", "skt", "canvas.zip"), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := Start(Options{Root: root})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(server.URL(), "http://")+"/api/session", nil)
	if err != nil {
		t.Fatalf("dial the session: %v", err)
	}
	defer connection.Close()
	if err := connection.WriteText(`{"kind":"start","game":"games/skt/canvas.zip"}`); err != nil {
		t.Fatalf("start the game: %v", err)
	}
	started := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		running := false
		for {
			opcode, payload, err := connection.ReadMessage()
			if err != nil {
				return
			}
			if running || opcode != wsproto.OpText {
				continue
			}
			var message struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal(payload, &message) == nil && message.Kind == "started" {
				running = true
				close(started)
			}
		}
	}()
	select {
	case <-started:
	case <-ended:
		t.Fatal("the socket closed before the game started")
	case <-time.After(30 * time.Second):
		t.Fatal("the game did not start")
	}

	if err := server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the server closed and left the attached session's socket open")
	}
}
