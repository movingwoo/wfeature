package webhost

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Two pages, one game, one save directory. The second start is refused rather
// than run: both sessions would write the same files, and the loser of that
// race loses their progress with nothing reported anywhere.
func TestSecondPageCannotStartAGameAnotherIsPlaying(t *testing.T) {
	server, url := resumeFixture(t)

	first := dialSession(t, url)
	expectMessage(t, first, serverReady)
	send(t, first, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, first, serverStarted)
	if started.Started == nil || started.Started.SaveOwner == "" {
		t.Skip("the fixture game has no saves of its own, so there is nothing to claim")
	}
	expectFrame(t, first)

	second := dialSession(t, url)
	expectMessage(t, second, serverReady)
	send(t, second, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	refusal := expectMessage(t, second, serverError)
	if refusal.Message == "" {
		t.Fatal("the second page was refused with no reason")
	}
	if !strings.Contains(refusal.Message, "canvas") {
		t.Errorf("the refusal does not name what is holding the game: %q", refusal.Message)
	}
	if server.parkedCount() != 0 {
		t.Errorf("%d sessions parked, want none", server.parkedCount())
	}
	// The first session is untouched by the refusal and still has its game.
	send(t, first, clientMessage{Kind: clientKey, Action: "press", Code: 148})
	expectFrame(t, first)
}

// A game the page stopped has given its save directory back, so the next page
// to ask for it gets it.
func TestStoppingAGameReleasesItsSaveDirectory(t *testing.T) {
	server, url := resumeFixture(t)

	first := dialSession(t, url)
	expectMessage(t, first, serverReady)
	send(t, first, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, first, serverStarted)
	if started.Started == nil || started.Started.SaveOwner == "" {
		t.Skip("the fixture game has no saves of its own, so there is nothing to claim")
	}
	expectFrame(t, first)
	send(t, first, clientMessage{Kind: clientStop})
	// Stopping is handled on the emulator's goroutine, so the claim is given
	// back a moment after the message goes out.
	deadline := time.Now().Add(10 * time.Second)
	for server.claimCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	second := dialSession(t, url)
	expectMessage(t, second, serverReady)
	send(t, second, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, second, serverStarted)
}

// A parked game is taken over rather than defended. Nobody is watching it, and
// refusing would lock a player out of their own game for the whole resume
// window because of a tab they have already closed.
func TestStartingAGameTakesOverItsParkedSession(t *testing.T) {
	server, url := resumeFixture(t)

	first := dialSession(t, url)
	expectMessage(t, first, serverReady)
	send(t, first, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, first, serverStarted)
	if started.Started == nil || started.Started.SaveOwner == "" {
		t.Skip("the fixture game has no saves of its own, so there is nothing to claim")
	}
	token := started.Started.Token
	expectFrame(t, first)

	_ = first.Close()
	waitForParked(t, server, 1)

	second := dialSession(t, url)
	expectMessage(t, second, serverReady)
	send(t, second, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, second, serverStarted)
	if server.parkedCount() != 0 {
		t.Errorf("%d sessions still parked, want the taken-over one to be closed", server.parkedCount())
	}
	if server.claimCount() != 1 {
		t.Errorf("%d save directories claimed, want the one the new game holds", server.claimCount())
	}
	// The parked game is gone, so its token buys nothing.
	third := dialSession(t, url)
	expectMessage(t, third, serverReady)
	send(t, third, clientMessage{Kind: clientResume, Token: token})
	if answer := expectMessage(t, third, serverResumed); answer.Resumed {
		t.Error("a game that was taken over was resumed as well")
	}
}

// A page that reloads is the sequence a refusal must not catch: the restart
// button drops the socket and the new document starts the same game before the
// server has noticed the old socket is gone. Nothing here waits for the park —
// that is the point.
func TestReloadingAPageCanRestartTheSameGame(t *testing.T) {
	server, url := resumeFixture(t)

	first := dialSession(t, url)
	expectMessage(t, first, serverReady)
	send(t, first, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, first, serverStarted)
	if started.Started == nil || started.Started.SaveOwner == "" {
		t.Skip("the fixture game has no saves of its own, so there is nothing to claim")
	}
	expectFrame(t, first)

	_ = first.Close()
	second := dialSession(t, url)
	expectMessage(t, second, serverReady)
	send(t, second, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	if answer := expectMessage(t, second, serverStarted); answer.Started == nil {
		t.Fatal("the reloaded page did not get its game back")
	}
	if server.parkedCount() != 0 {
		t.Errorf("%d sessions parked, want the reloaded page to have taken the game over", server.parkedCount())
	}
}

// The save API reaches the same files by another road, so it is under the same
// rule: an entry put into a directory a game holds would be gone at that
// game's next commit, which writes the file back whole.
func TestTheSaveAPIWillNotWriteUnderAGameThatIsOpen(t *testing.T) {
	saveRoot := filepath.Join(t.TempDir(), "ktf")
	server := newTestServer(t, Options{SaveRoot: saveRoot})
	owner := "0102DD43"
	put := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut,
			"/api/saves/"+owner+"/db/slot", strings.NewReader("x")))
		return recorder
	}

	// Nothing holds the directory, so the write lands and gives the claim
	// straight back: a tool holds it for its own write and no longer.
	if recorder := put(); recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if held := server.claimCount(); held != 0 {
		t.Fatalf("%d claims outlived the write, want none", held)
	}

	directory := server.saveDirectory("ktf", owner)
	if claimed, _ := server.claimSaveDirectory(directory, "어떤 게임"); !claimed {
		t.Fatal("the fixture could not take the claim")
	}
	recorder := put()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "어떤 게임") {
		t.Errorf("the refusal does not name the holder: %q", recorder.Body.String())
	}

	// A parked game holds it too. It is taken over by somebody starting the
	// same game, because that is a player who is here now; it is not taken
	// over by a tool writing a file.
	server.markSaveDirectoryParked(directory, true)
	if recorder := put(); recorder.Code != http.StatusConflict {
		t.Fatalf("status against a parked holder = %d, want 409", recorder.Code)
	}

	server.releaseSaveDirectory(directory)
	if recorder := put(); recorder.Code != http.StatusNoContent {
		t.Fatalf("status after the game closed = %d, want 204", recorder.Code)
	}
}
