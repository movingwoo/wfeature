package webhost

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// resumeFixture stands up a server the way sessionFixture does, but hands back
// what it takes to open a *second* connection to it: the whole point of parking
// is that a game survives one socket and is picked up by another.
func resumeFixture(t *testing.T) (*Server, string) {
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
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "ktf"),
		LogRoot:  filepath.Join(root, "logs"),
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() {
		httpServer.Close()
		server.CloseParkedSessions()
	})
	return server, "ws://" + strings.TrimPrefix(httpServer.URL, "http://") + "/api/session"
}

func dialSession(t *testing.T, url string) *wsproto.Conn {
	t.Helper()
	connection, _, err := wsproto.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial the session: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

// waitForParked waits for the server to notice the socket is gone. Parking
// happens on the session's own goroutine after the read fails, so a test that
// looked immediately would be racing the disconnect rather than testing it.
func waitForParked(t *testing.T, server *Server, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if server.parkedCount() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%d parked sessions, want %d", server.parkedCount(), want)
}

// The phone's case, end to end: a game is running, the socket goes, and a new
// connection asks for the game back by the token the first one was given. What
// proves it is the same session rather than a new one is that no archive is
// named — a resume says only the token, and the server answers with the game's
// identity and its picture.
func TestSessionResumesAfterItsSocketDrops(t *testing.T) {
	server, url := resumeFixture(t)

	first := dialSession(t, url)
	expectMessage(t, first, serverReady)
	send(t, first, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, first, serverStarted)
	if started.Started == nil || started.Started.Token == "" {
		t.Fatal("a started game carries no resume token")
	}
	token := started.Started.Token
	expectFrame(t, first)

	// The phone goes away.
	_ = first.Close()
	waitForParked(t, server, 1)

	// And comes back on a new socket.
	second := dialSession(t, url)
	expectMessage(t, second, serverReady)
	send(t, second, clientMessage{Kind: clientResume, Token: token})
	resumed := expectMessage(t, second, serverStarted)
	if resumed.Started == nil {
		t.Fatal("the resumed session carries no description")
	}
	if resumed.Started.Platform != started.Started.Platform {
		t.Errorf("platform = %q, want %q", resumed.Started.Platform, started.Started.Platform)
	}
	if resumed.Started.Token != token {
		t.Errorf("token = %q, want the one the page already has (%q)", resumed.Started.Token, token)
	}
	// A page that has just reconnected has an empty canvas, so the picture the
	// game had is sent again without it having to run first.
	expectFrame(t, second)
	if server.parkedCount() != 0 {
		t.Errorf("%d sessions still parked, want the resumed one to be gone", server.parkedCount())
	}
}

// A token nobody parked — an expired one, or one from a server that has since
// restarted — is answered rather than failed. It is the ordinary case after a
// long absence, and the page acts on it by forgetting the token and showing the
// game list again.
func TestSessionResumeWithoutAParkedGameSaysSo(t *testing.T) {
	_, url := resumeFixture(t)

	connection := dialSession(t, url)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientResume, Token: "0123456789abcdef0123456789abcdef"})
	answer := expectMessage(t, connection, serverResumed)
	if answer.Resumed {
		t.Error("a game was resumed under a token nothing parked")
	}
	if answer.Message == "" {
		t.Error("the page was told nothing about why there is no game")
	}
	// The session is still usable: the page falls back to starting a game.
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)
}

// A game the page stopped is not parked. Stopping is the one way a person says
// they are finished with it, and coming back to a game they closed would be
// the opposite of what the token is for.
func TestSessionStoppedGameIsNotParked(t *testing.T) {
	server, url := resumeFixture(t)

	connection := dialSession(t, url)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)
	expectFrame(t, connection)

	send(t, connection, clientMessage{Kind: clientStop})
	// The stop is handled on the emulator's goroutine; giving it a moment is
	// what separates "not parked" from "not parked yet".
	time.Sleep(200 * time.Millisecond)
	_ = connection.Close()
	waitForParked(t, server, 0)
}

// The window is the reason parking is safe to do at all: a game nobody came
// back for is closed rather than held. The timer that fires it is minutes away,
// so what is tested here is the expiry itself.
func TestParkedSessionIsClosedWhenItsWindowRunsOut(t *testing.T) {
	server, url := resumeFixture(t)

	connection := dialSession(t, url)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, connection, serverStarted)
	token := started.Started.Token
	expectFrame(t, connection)

	_ = connection.Close()
	waitForParked(t, server, 1)

	server.expireSession(token)
	if server.parkedCount() != 0 {
		t.Fatalf("%d sessions parked after the window elapsed, want 0", server.parkedCount())
	}
	// The token is spent: a page that comes back late is told there is nothing
	// rather than handed a closed game.
	late := dialSession(t, url)
	expectMessage(t, late, serverReady)
	send(t, late, clientMessage{Kind: clientResume, Token: token})
	if answer := expectMessage(t, late, serverResumed); answer.Resumed {
		t.Error("an expired game was resumed")
	}
}

// A parked game has been told it is parked, and a resumed one has been told it
// is back. Two of the three platforms had the callbacks and nothing that
// called them, so this is the test that keeps the wiring rather than the
// implementation: delete the two lines in park() and resumeGame() and every
// other test here still passes.
func TestParkingTellsTheGameAndResumingTellsItAgain(t *testing.T) {
	server, url := resumeFixture(t)

	first := dialSession(t, url)
	expectMessage(t, first, serverReady)
	send(t, first, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, first, serverStarted)
	if started.Started == nil || started.Started.Token == "" {
		t.Fatal("a started game carries no resume token")
	}
	token := started.Started.Token
	expectFrame(t, first)

	_ = first.Close()
	waitForParked(t, server, 1)

	game := server.parkedGame(token)
	if game == nil {
		t.Fatal("nothing is parked under the token the page was given")
	}
	if !game.Paused() {
		t.Fatal("a parked game was never told its player went away")
	}

	second := dialSession(t, url)
	expectMessage(t, second, serverReady)
	send(t, second, clientMessage{Kind: clientResume, Token: token})
	expectMessage(t, second, serverStarted)
	expectFrame(t, second)

	// The game is no longer parked, so it is asked through the runner that
	// took it: a frame arrived, which is a game that is running again.
	if server.parkedGame(token) != nil {
		t.Fatal("the game is still parked after being resumed")
	}
}
