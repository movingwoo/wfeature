package webhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Exercise the actual WebSocket and LGT AOT guest field. A rejected composition
// keeps the edit open for correction, and reopening reads the committed value
// from the guest rather than from the transport request.
func TestLGTTextInputWebSocketRoundTripAndLimitRetry(t *testing.T) {
	connection, logs := sessionFixture(t)
	archive, err := os.ReadFile(filepath.Join("..", "platform", "lgt", "testdata", "text-input.zip"))
	if err != nil {
		t.Fatal(err)
	}
	gameRoot := filepath.Join(filepath.Dir(logs), "games", "lgt")
	if err := os.MkdirAll(gameRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "text-input.zip"), archive, 0o600); err != nil {
		t.Fatal(err)
	}

	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/lgt/text-input.zip", ID: 1})
	started := expectMessage(t, connection, serverStarted)
	if started.Started == nil || started.Started.Platform != "lgt" {
		t.Fatalf("started = %+v, want LGT", started.Started)
	}

	open := func(id uint64) *textInputMessage {
		t.Helper()
		send(t, connection, clientMessage{Kind: clientText, Action: "open", ID: id})
		reply := expectMessage(t, connection, serverResult)
		if reply.ID != id || reply.TextInput == nil || reply.TextInput.Edit == 0 {
			t.Fatalf("open reply = %+v", reply)
		}
		return reply.TextInput
	}

	edit := open(2)
	if edit.Text != "" || edit.MaxLength != 16 || edit.Multiline || edit.Password || edit.InputMode != "text" {
		t.Fatalf("initial editor = %+v", edit)
	}
	send(t, connection, clientMessage{
		Kind: clientText, Action: "commit", Edit: edit.Edit,
		Text: strings.Repeat("한", 17), ID: 3,
	})
	if reply := expectMessage(t, connection, serverError); reply.ID != 3 || reply.Message != backend.ErrInvalidTextInput.Error() {
		t.Fatalf("over-limit reply = %+v", reply)
	}

	const composition = "한글 이름 😀"
	send(t, connection, clientMessage{
		Kind: clientText, Action: "commit", Edit: edit.Edit,
		Text: composition, ID: 4,
	})
	if reply := expectMessage(t, connection, serverResult); reply.ID != 4 {
		t.Fatalf("retry reply = %+v", reply)
	}

	if reopened := open(5); reopened.Text != composition {
		t.Fatalf("guest text = %q, want %q", reopened.Text, composition)
	}
	send(t, connection, clientMessage{Kind: clientStop, ID: 6})
	expectMessage(t, connection, serverResult)
}
