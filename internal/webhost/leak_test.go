package webhost

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// A server is left running. That is the whole deployment — one binary beside a
// games folder, on for weeks — so what a session fails to let go of is not
// collected by the process exiting the way it is for the CLI. A game runs on
// goroutines: the emulator loop, the encoder, the writer, the reader, and the
// guest's own threads underneath them. If one session leaves any of those
// behind, the hundredth session is a hundred of them.
//
// This drives whole sessions through the socket a phone uses and says the
// count comes back. It is a growth test rather than an absolute one: the
// runtime, the HTTP server and the test framework all keep goroutines of their
// own, and what matters is that playing a game and stopping it is neutral.
func TestPlayingAndStoppingAGameLeavesNoGoroutinesBehind(t *testing.T) {
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "skt"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "canvas-skt.zip"))
	if err != nil {
		t.Fatalf("read the canvas fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "skt", "canvas.zip"), archive, 0o644); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "skt"),
		LogRoot:  filepath.Join(root, "logs"),
	})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	address := "ws://" + strings.TrimPrefix(httpServer.URL, "http://") + "/api/session"

	playOnce := func() {
		connection, _, err := wsproto.Dial(address, nil)
		if err != nil {
			t.Fatalf("dial the session: %v", err)
		}
		defer connection.Close()
		// `expectMessage` only checks its deadline between messages, and a
		// stopped game sends nothing at all, so the read itself needs the
		// bound or a session that goes quiet hangs the run rather than
		// failing it.
		if err := connection.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			t.Fatalf("set a read deadline: %v", err)
		}
		expectMessage(t, connection, serverReady)
		if err := connection.WriteText(`{"kind":"start","game":"skt/canvas.zip"}`); err != nil {
			t.Fatalf("start: %v", err)
		}
		expectMessage(t, connection, serverStarted)
		// Stopping rather than only closing the socket: a game whose page
		// merely goes away is parked on purpose and is meant to hold its
		// goroutines for the resume window. This is the other path, the one
		// that must let go straight away.
		if err := connection.WriteText(`{"kind":"stop"}`); err != nil {
			t.Fatalf("stop: %v", err)
		}
		// A stop is not acknowledged, so the acknowledgement is the next
		// request: commands are handled in the order they arrive, so an answer
		// to this one is the session saying it is past the stop.
		if err := connection.WriteText(`{"kind":"report","id":1}`); err != nil {
			t.Fatalf("report: %v", err)
		}
		expectMessage(t, connection, serverResult)
	}

	// The first run is the warm-up: it is what pays for whatever the runtime,
	// the platform and the HTTP server start once and keep.
	playOnce()
	settled := settledGoroutines(t, 0)

	const rounds = 3
	for range rounds {
		playOnce()
	}
	// The slack is one, for a connection goroutine `httptest` has not reaped
	// yet on a socket the client already closed — timing rather than a leak,
	// and `settledGoroutines` gives it ten seconds to go. Clean, this run
	// grows by nothing at all; with the runner's `close(r.frames)` taken out
	// it grows by six, which is what says the test can see a leak rather than
	// only tolerate one.
	const slack = 1
	after := settledGoroutines(t, settled+slack)
	if after > settled+slack {
		buffer := make([]byte, 1<<20)
		buffer = buffer[:runtime.Stack(buffer, true)]
		t.Fatalf("goroutines grew from %d to %d over %d sessions:\n%s",
			settled, after, rounds, buffer)
	}
}

// settledGoroutines waits for the count to stop moving, or to come back under
// want, and answers with it. Goroutines wind down after the call that ended
// them returns, so reading the count straight away measures the scheduler.
func settledGoroutines(t *testing.T, want int) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	previous := -1
	stable := 0
	for time.Now().Before(deadline) {
		runtime.GC()
		count := runtime.NumGoroutine()
		if want > 0 && count <= want {
			return count
		}
		if count == previous {
			if stable++; stable >= 3 {
				return count
			}
		} else {
			previous, stable = count, 0
		}
		time.Sleep(50 * time.Millisecond)
	}
	return runtime.NumGoroutine()
}
