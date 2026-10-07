package webhost

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

func TestCheckpointPictureWaitsForAResetQueuedAfterTheTextDrain(t *testing.T) {
	r := &sessionRunner{outText: make(chan outboundMessage, 8)}
	r.send(serverMessage{Kind: serverResult, ID: 1})
	batch := r.drainText(nil)

	// The writer already drained text when the load finished. Its new picture
	// becomes available before the writer selects a picture for this batch.
	r.outputEpoch.Store(1)
	r.send(serverMessage{Kind: serverRestored, ID: 2})
	r.sendAudio([]audioEvent{{Kind: audioAllOff}}, false)
	picture := outboundMessage{binary: []byte("complete checkpoint picture"), redraw: true, epoch: 1, timeline: true}
	batch = r.appendPicture(batch, picture)

	var buffer bytes.Buffer
	connection := wsproto.Server(&buffer)
	var messages []wsproto.Message
	for _, message := range batch {
		if message.binary != nil {
			messages = append(messages, wsproto.Message{Opcode: wsproto.OpBinary, Payload: message.binary})
		} else {
			messages = append(messages, wsproto.Message{Opcode: wsproto.OpText, Payload: []byte(message.text)})
		}
	}
	if err := connection.WriteBatch(messages); err != nil {
		t.Fatal(err)
	}
	reader := wsproto.Client(&buffer)
	for _, kind := range []string{serverResult, serverRestored, serverAudio} {
		opcode, payload, err := reader.ReadMessage()
		if err != nil || opcode != wsproto.OpText || !bytes.Contains(payload, []byte(`"kind":"`+kind+`"`)) {
			t.Fatalf("want %s before the checkpoint picture, got opcode=%v payload=%q error=%v", kind, opcode, payload, err)
		}
	}
	opcode, payload, err := reader.ReadMessage()
	if err != nil || opcode != wsproto.OpBinary || !bytes.Equal(payload, picture.binary) {
		t.Fatalf("checkpoint picture after reset: opcode=%v payload=%q error=%v", opcode, payload, err)
	}
	if len(r.outText) != 0 {
		t.Fatal("the picture left its reset or sound queued behind it")
	}
}

func TestCheckpointResumeKeepsLaterSpeed(t *testing.T) {
	for _, parked := range []bool{false, true} {
		name := "attached"
		if parked {
			name = "parked"
		}
		t.Run(name, func(t *testing.T) {
			root, _ := checkpointServerFiles(t)
			server := checkpointServer(t, root)
			newRunner := func() *sessionRunner {
				r := &sessionRunner{server: server, frames: make(chan pendingFrame, 8), outText: make(chan outboundMessage, 64)}
				t.Cleanup(r.stopGame)
				return r
			}
			r := newRunner()
			r.startGame(t.Context(), clientMessage{Kind: clientStart, Game: "games/ktf/checkpoint.zip", ID: 1})
			if r.game == nil {
				t.Fatalf("start: %+v", readCheckpointReplies(t, r))
			}
			readCheckpointReplies(t, r)
			r.quickSave(t.Context(), clientMessage{Kind: clientQuickSave, ID: 2})
			if replies := readCheckpointReplies(t, r); len(replies) != 1 || replies[0].Kind != serverResult {
				t.Fatalf("quick save: %+v", replies)
			}
			r.quickLoad(t.Context(), clientMessage{Kind: clientQuickLoad, ID: 3})
			if replies := readCheckpointReplies(t, r); len(replies) == 0 || replies[0].Kind != serverRestored {
				t.Fatalf("quick load: %+v", replies)
			}
			r.handle(t.Context(), clientMessage{Kind: clientSpeed, Value: 2, Epoch: r.outputEpoch.Load()})
			token := r.token
			if parked {
				r.park()
				r = newRunner()
			}
			r.resumeGame(t.Context(), clientMessage{Kind: clientResume, Token: token, ID: 4})
			replies := readCheckpointReplies(t, r)
			if len(replies) == 0 || replies[0].Kind != serverStarted || replies[0].Started == nil {
				t.Fatalf("resume: %+v", replies)
			}
			info := replies[0].Started
			// The page remembers the reported speed when Restored is true. It
			// must keep the player's later choice when the connection resumes.
			if !info.Restored || info.Speed != 2 || r.game.Speed() != 2 {
				t.Fatalf("resume reports restored=%v speed=%v while live speed=%v, want 2", info.Restored, info.Speed, r.game.Speed())
			}
		})
	}
}
