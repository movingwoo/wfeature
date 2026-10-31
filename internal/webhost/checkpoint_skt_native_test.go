package webhost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/testfixture"
)

// An SKT native program takes the same quick-save commands and the same text
// box over the real socket as every other title: it is offered both, a quick
// save and a live load go through, and the page's text reaches the program's
// own field before and after the load.
func TestSKTNativeQuickSaveAndTextInputOverTheSocket(t *testing.T) {
	connection, logs := sessionFixture(t)
	archive, err := testfixture.SKTCletArchive(testfixture.SKTCletOptions{FieldAtStart: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gameRoot := filepath.Join(filepath.Dir(logs), "games", "skt")
	if err := os.WriteFile(filepath.Join(gameRoot, "native.zip"), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/native.zip", ID: 1})
	started := expectMessage(t, connection, serverStarted)
	if started.Started == nil || started.Started.Platform != "skt" || !started.Started.CanCheckpoint {
		t.Fatalf("started = %+v, want an SKT program offered quick save", started.Started)
	}
	commit := func(id uint64, text string, epoch uint64) {
		t.Helper()
		send(t, connection, clientMessage{Kind: clientText, Action: "open", ID: id, Epoch: epoch})
		reply := expectMessage(t, connection, serverResult)
		if reply.ID != id || reply.TextInput == nil || reply.TextInput.Edit == 0 || !reply.TextInput.Append || reply.TextInput.MaxLength != 64 {
			t.Fatalf("open reply = %+v", reply)
		}
		send(t, connection, clientMessage{Kind: clientText, Action: "commit", Edit: reply.TextInput.Edit, Text: text, ID: id + 1, Epoch: epoch})
		if reply := expectMessage(t, connection, serverResult); reply.ID != id+1 {
			t.Fatalf("commit reply = %+v", reply)
		}
	}
	commit(2, "가A", 0)
	send(t, connection, clientMessage{Kind: clientQuickSave, ID: 4})
	if reply := expectMessage(t, connection, serverResult); reply.ID != 4 {
		t.Fatalf("quick save reply = %+v", reply)
	}
	send(t, connection, clientMessage{Kind: clientQuickLoad, ID: 5})
	restored := expectMessage(t, connection, serverRestored)
	if restored.Epoch != 1 || restored.Started == nil || !restored.Started.Restored || !restored.Started.HasCheckpoint {
		t.Fatalf("restored = %+v", restored)
	}
	commit(6, "나", 1)
	send(t, connection, clientMessage{Kind: clientStop, ID: 8, Epoch: 1})
	expectMessage(t, connection, serverResult)
}

// A native SKT program answers the cheat panel the way every native title on
// the ARM core does: the page is told it can watch writes, and a first search
// sweeps the program's memory rather than being refused as having none.
func TestSKTNativeAnswersTheCheatPanelOverTheSocket(t *testing.T) {
	connection, logs := sessionFixture(t)
	archive, err := testfixture.SKTCletArchive(testfixture.SKTCletOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gameRoot := filepath.Join(filepath.Dir(logs), "games", "skt")
	if err := os.WriteFile(filepath.Join(gameRoot, "native.zip"), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/native.zip", ID: 1})
	started := expectMessage(t, connection, serverStarted)
	if started.Started == nil || !started.Started.CanWatch {
		t.Fatalf("started = %+v, want an SKT program whose writes can be watched", started.Started)
	}
	send(t, connection, clientMessage{Kind: clientCheat, ID: 2, Op: "scan", Type: "u32", Filter: "unknown"})
	scan := expectMessage(t, connection, serverResult)
	if scan.ID != 2 || scan.Cheat == nil || !scan.Cheat.Searchable || scan.Cheat.Count == 0 {
		t.Fatalf("a first scan over an SKT program found %+v (%q)", scan.Cheat, scan.Message)
	}
	send(t, connection, clientMessage{Kind: clientStop, ID: 3})
	expectMessage(t, connection, serverResult)
}
