package webhost

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
)

func TestTextInputCommitOwnershipAndRetry(t *testing.T) {
	game := &session.Session{}
	runner := &sessionRunner{game: game, gameCtx: context.Background(), outText: make(chan outboundMessage, 1)}
	calls := 0
	runner.textInputGame, runner.textInputID = game, 8
	runner.textInput = &backend.TextInput{Commit: func(_ context.Context, text string) error {
		calls++
		if text != "한글" {
			return backend.ErrInvalidTextInput
		}
		return nil
	}}
	send := func(edit uint64, text string, want string) {
		t.Helper()
		runner.handleTextInput(clientMessage{Kind: clientText, Action: "commit", ID: 3, Edit: edit, Text: text})
		var reply serverMessage
		if err := json.Unmarshal([]byte((<-runner.outText).text), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Kind != want || reply.ID != 3 {
			t.Fatalf("unexpected reply: %+v", reply)
		}
	}
	send(7, "한글", serverError)
	send(8, strings.Repeat("a", backend.MaxTextInputBytes+1), serverError)
	if calls != 0 {
		t.Fatal("invalid transport request reached guest")
	}
	send(8, "invalid", serverError)
	send(8, "한글", serverResult)
	send(8, "한글", serverError)
	if calls != 2 {
		t.Fatalf("commit calls = %d", calls)
	}
	runner.textInput = &backend.TextInput{Commit: func(context.Context, string) error { t.Fatal("old game commit reached"); return nil }}
	runner.game = &session.Session{}
	send(8, "한글", serverError)
}

func TestTextInputStaleCommitCannotBecomeCurrentAgain(t *testing.T) {
	game := &session.Session{}
	runner := &sessionRunner{game: game, gameCtx: context.Background(), outText: make(chan outboundMessage, 2)}
	runner.textInputGame, runner.textInputID = game, 4
	calls := 0
	runner.textInput = &backend.TextInput{Commit: func(context.Context, string) error {
		calls++
		if calls == 1 {
			return backend.ErrTextInputChanged
		}
		return nil
	}}
	commit := clientMessage{Kind: clientText, Action: "commit", ID: 5, Edit: 4, Text: "complete text"}
	runner.handleTextInput(commit)
	if reply := textInputReply(t, runner); reply.Kind != serverError || reply.Message != backend.ErrTextInputChanged.Error() {
		t.Fatalf("first stale reply = %+v", reply)
	}
	if runner.textInput != nil || runner.textInputGame != nil {
		t.Fatal("stale edit remains installed")
	}
	runner.handleTextInput(commit)
	if reply := textInputReply(t, runner); reply.Kind != serverError || reply.Message != backend.ErrTextInputChanged.Error() {
		t.Fatalf("repeated stale reply = %+v", reply)
	}
	if calls != 1 {
		t.Fatalf("stale closure calls = %d, want 1", calls)
	}
}

func TestTextInputGuestExitSettlesRequestAndEndsGame(t *testing.T) {
	game := &session.Session{}
	runner := &sessionRunner{
		server:     newTestServer(t, Options{LogRoot: t.TempDir()}),
		game:       game,
		gameCtx:    context.Background(),
		outText:    make(chan outboundMessage, 3),
		statsSince: time.Now(),
	}
	runner.textInputGame, runner.textInputID = game, 6
	runner.textInput = &backend.TextInput{Commit: func(context.Context, string) error { return session.ErrExited }}
	runner.handleTextInput(clientMessage{Kind: clientText, Action: "commit", ID: 7, Edit: 6, Text: "complete text"})

	if reply := textInputReply(t, runner); reply.Kind != serverExited {
		t.Fatalf("exit event = %+v", reply)
	}
	answer := textInputReply(t, runner)
	if answer.Kind != serverError || answer.ID != 7 || !answer.Exited {
		t.Fatalf("commit answer = %+v", answer)
	}
	if runner.game != nil || runner.textInput != nil || runner.textInputGame != nil {
		t.Fatal("runner retained game or text edit after guest exit")
	}
}

func textInputReply(t *testing.T, runner *sessionRunner) serverMessage {
	t.Helper()
	var reply serverMessage
	if err := json.Unmarshal([]byte((<-runner.outText).text), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

// Exercise the actual WebSocket and guest text field, including ownership
// changes. Reopening the editor reads the committed value back from the guest.
func TestTextInputSessionRoundTripAndResume(t *testing.T) {
	connection, logs := sessionFixture(t)
	data, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "text-input.jar"))
	if err != nil {
		t.Fatal(err)
	}
	var packed bytes.Buffer
	writer := zip.NewWriter(&packed)
	for name, contents := range map[string][]byte{
		"text-input.jar": data,
		"text-input.msd": []byte("MIDlet-Name: Text Input Fixture\nMIDlet-1: Text Input Fixture, , TextInputMIDlet\n"),
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(logs), "games", "text-input.zip"), packed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientText, Action: "open", ID: 1})
	if reply := expectMessage(t, connection, serverError); reply.Message != backend.ErrNoTextInput.Error() {
		t.Fatalf("no-game reply = %+v", reply)
	}
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/text-input.zip", ID: 2})
	started := expectMessage(t, connection, serverStarted)
	open := func(id uint64) *textInputMessage {
		t.Helper()
		send(t, connection, clientMessage{Kind: clientText, Action: "open", ID: id})
		reply := expectMessage(t, connection, serverResult)
		if reply.TextInput == nil || reply.TextInput.Edit == 0 {
			t.Fatalf("missing editor: %+v", reply)
		}
		return reply.TextInput
	}
	first := open(3)
	if first.Text != "" || first.MaxLength != 16 {
		t.Fatalf("initial editor = %+v", first)
	}
	send(t, connection, clientMessage{Kind: clientText, Action: "commit", Edit: first.Edit, Text: "한글 이름 😀", ID: 4})
	expectMessage(t, connection, serverResult)
	second := open(5)
	if second.Text != "한글 이름 😀" {
		t.Fatalf("guest text = %q", second.Text)
	}
	send(t, connection, clientMessage{Kind: clientText, Action: "commit", Edit: second.Edit, Text: strings.Repeat("한", 17), ID: 6})
	if reply := expectMessage(t, connection, serverError); reply.Message != backend.ErrInvalidTextInput.Error() {
		t.Fatalf("limit reply = %+v", reply)
	}
	send(t, connection, clientMessage{Kind: clientPark, ID: 7})
	expectMessage(t, connection, serverResult)
	send(t, connection, clientMessage{Kind: clientResume, Token: started.Started.Token, ID: 8})
	expectMessage(t, connection, serverStarted)
	send(t, connection, clientMessage{Kind: clientText, Action: "commit", Edit: second.Edit, Text: "stale", ID: 9})
	if reply := expectMessage(t, connection, serverError); reply.Message != backend.ErrTextInputChanged.Error() {
		t.Fatalf("stale reply = %+v", reply)
	}
	resumed := open(10)
	if resumed.Text != "한글 이름 😀" {
		t.Fatalf("resumed guest text = %q", resumed.Text)
	}
	send(t, connection, clientMessage{Kind: clientStop, ID: 11})
	expectMessage(t, connection, serverResult)
}
