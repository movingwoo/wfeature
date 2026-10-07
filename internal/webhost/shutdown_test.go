package webhost

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// expectSocketClosed waits for the server to end a page's socket. A session
// says several things before it goes, so whatever is still on its way is read
// past; what matters is that the reading ends.
func expectSocketClosed(t *testing.T, connection *wsproto.Conn) {
	t.Helper()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}()
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the server left the page's socket open")
	}
}

// Stopping the server used to close only the games that were waiting for a
// page. A game whose page was still attached ended with the process, so its
// session was never closed — and closing is what hands the save store whatever
// a title wrote and had not finished with: a file it left open, keys a native
// package keeps in memory until a frame ends.
//
// The session is held from outside by parking it once: a parked game can be
// asked for, and resuming hands the same one back to a new socket.
func TestStoppingTheServerClosesAnAttachedGame(t *testing.T) {
	server, url := resumeFixture(t)

	first := dialSession(t, url)
	expectMessage(t, first, serverReady)
	send(t, first, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, first, serverStarted)
	token := started.Started.Token
	expectFrame(t, first)
	_ = first.Close()
	waitForParked(t, server, 1)
	game := server.parkedGame(token)
	if game == nil {
		t.Fatal("the parked game cannot be found under its token")
	}

	page := dialSession(t, url)
	expectMessage(t, page, serverReady)
	send(t, page, clientMessage{Kind: clientResume, Token: token})
	expectMessage(t, page, serverStarted)
	expectFrame(t, page)
	if server.parkedCount() != 0 || server.claimCount() != 1 || server.attachedSession(token) == nil {
		t.Fatalf("before the stop: %d parked, %d claims, attached=%v; want an attached game holding one claim",
			server.parkedCount(), server.claimCount(), server.attachedSession(token) != nil)
	}
	if !game.Running() {
		t.Fatal("the resumed game is not running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server.CloseSessions(ctx)
	if ctx.Err() != nil {
		t.Fatal("the attached game did not close before the deadline")
	}

	// CloseSessions returns once the runner has let go, so the session is
	// read here without racing the goroutine that closed it.
	if game.Running() {
		t.Error("stopping the server left the attached game's session open")
	}
	if server.parkedCount() != 0 || server.claimCount() != 0 || server.attachedSession(token) != nil {
		t.Errorf("after the stop: %d parked, %d claims, attached=%v; want nothing held",
			server.parkedCount(), server.claimCount(), server.attachedSession(token) != nil)
	}
	expectSocketClosed(t, page)

	// A second stop finds nothing to do and does not trip over the first.
	server.CloseSessions(context.Background())
}

// Once a stop has begun nothing starts and nothing is handed back. A page that
// arrives in that moment is told so rather than given a game the process is
// about to take away.
func TestAStoppingServerStartsAndResumesNothing(t *testing.T) {
	server, url := resumeFixture(t)

	parked := dialSession(t, url)
	expectMessage(t, parked, serverReady)
	send(t, parked, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	token := expectMessage(t, parked, serverStarted).Started.Token
	expectFrame(t, parked)
	_ = parked.Close()
	waitForParked(t, server, 1)

	server.CloseSessions(context.Background())

	late := dialSession(t, url)
	expectMessage(t, late, serverReady)
	send(t, late, clientMessage{Kind: clientResume, Token: token})
	if answer := expectMessage(t, late, serverResumed); answer.Resumed {
		t.Error("a game was resumed after the server began to stop")
	}
	send(t, late, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	refusal := expectMessage(t, late, serverError)
	if !strings.Contains(refusal.Message, "종료") {
		t.Errorf("the refusal was %q, want it to say the server is stopping", refusal.Message)
	}
	if server.parkedCount() != 0 || server.claimCount() != 0 {
		t.Errorf("%d parked and %d claims after a refused start, want none", server.parkedCount(), server.claimCount())
	}
}

// lockedBuffer lets a test read what a logger wrote while goroutines of the
// server may still be writing to it.
type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// A guest call that never returns must not keep the process from ending. The
// wait for a runner is bounded by the caller's deadline, and the game that did
// not let go is named in the log, because that is the one whose pending writes
// are not known to have reached the store.
func TestAStoppingServerDoesNotWaitForeverForARunner(t *testing.T) {
	var logged lockedBuffer
	server := newTestServer(t, Options{Logger: slog.New(slog.NewTextHandler(&logged, nil))})

	// A runner that holds a game and never notices the request.
	stuck := &sessionRunner{stop: make(chan struct{}), released: make(chan struct{}), admitted: true, admittedAs: "a game that does not return"}
	// One that was only ever a reserved token: it has no game to close and is
	// not waited for.
	reserved := &sessionRunner{stop: make(chan struct{}), released: make(chan struct{})}
	server.parkedMu.Lock()
	server.attached = map[string]*sessionRunner{"stuck": stuck, "reserved": reserved}
	server.parkedMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		server.CloseSessions(ctx)
	}()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the stop waited past its deadline for a runner that never lets go")
	}

	select {
	case <-stuck.stop:
	default:
		t.Error("the runner holding a game was not asked to close it")
	}
	select {
	case <-reserved.stop:
		t.Error("a runner with no game was asked to close one")
	default:
	}
	if text := logged.String(); !strings.Contains(text, "a game that does not return") || !strings.Contains(text, "level=WARN") {
		t.Errorf("the log does not name the game that was left behind:\n%s", text)
	}

	// The same stop again, and one for a runner that has let go: neither
	// closes a channel twice, and the second reports nothing new.
	close(stuck.released)
	before := logged.String()
	server.CloseSessions(context.Background())
	if logged.String() != before {
		t.Errorf("a runner that had let go was reported:\n%s", strings.TrimPrefix(logged.String(), before))
	}
}
