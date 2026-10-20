package webhost

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/testfixture"
)

// A page that leaves the screen out of a start lets the archive decide, and
// the started message carries what the title took: on KTF that is the handset
// the descriptor declares, and the page lays its canvas out from this answer
// before the first picture arrives. A page that names a screen — the default
// size included, which the page now sends when a player picks it — keeps it.
// Either way the message says what a start without a size takes, which is
// what the page's automatic choice means: a page that started on a named
// screen still has to know whether picking 자동 would change anything.
func TestKTFStartWithoutAScreenTakesTheDeclaredHandset(t *testing.T) {
	archive, err := testfixture.KTFCheckpointArchiveDeclaring("DisplaySize:176*220\n")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeCheckpointGame(t, root, "ktf", archive)
	for _, test := range []struct {
		name          string
		width, height int
		wantW, wantH  int
	}{
		{"no screen named", 0, 0, 176, 220},
		{"240x320 named", 240, 320, 240, 320},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &sessionRunner{server: checkpointServer(t, root), frames: make(chan pendingFrame, 1), outText: make(chan outboundMessage, 64)}
			r.startGame(t.Context(), clientMessage{Kind: clientStart, Game: "games/ktf/checkpoint.zip", Width: test.width, Height: test.height, ID: 1})
			if r.game == nil {
				t.Fatalf("the fixture did not start: %+v", readCheckpointReplies(t, r))
			}
			t.Cleanup(r.stopGame)
			if r.started.Width != test.wantW || r.started.Height != test.wantH {
				t.Fatalf("started message says %dx%d, want %dx%d", r.started.Width, r.started.Height, test.wantW, test.wantH)
			}
			if r.started.AutoWidth != 176 || r.started.AutoHeight != 220 {
				t.Fatalf("started message says a start without a size takes %dx%d, want the declared 176x220",
					r.started.AutoWidth, r.started.AutoHeight)
			}
		})
	}
}

// A quick save keeps the screen its game was running on, whatever a later
// start asks for: the title's memory is laid out for that screen, and a load
// that put it on another would draw every row at the wrong offset. The slot
// here is taken on the declared 176x220 and loaded by a start that names
// 240x320, which is what a player does after changing the setting.
func TestAQuickLoadAtStartKeepsTheScreenTheSlotWasTakenOn(t *testing.T) {
	archive, err := testfixture.KTFCheckpointArchiveDeclaring("DisplaySize:176*220\n")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeCheckpointGame(t, root, "ktf", archive)
	r := &sessionRunner{server: checkpointServer(t, root), frames: make(chan pendingFrame, 1), outText: make(chan outboundMessage, 64)}
	r.startGame(t.Context(), clientMessage{Kind: clientStart, Game: "games/ktf/checkpoint.zip", ID: 1})
	if r.game == nil {
		t.Fatalf("the fixture did not start: %+v", readCheckpointReplies(t, r))
	}
	t.Cleanup(r.stopGame)
	r.quickSave(t.Context(), clientMessage{Kind: clientQuickSave, ID: 2})
	if !r.started.HasCheckpoint {
		t.Fatalf("no slot was saved: %+v", readCheckpointReplies(t, r))
	}
	readCheckpointReplies(t, r)
	r.stopGame()

	r.startGame(t.Context(), clientMessage{Kind: clientStart, Game: "games/ktf/checkpoint.zip", QuickLoad: true, Width: 240, Height: 320, ID: 3})
	if r.game == nil {
		t.Fatalf("the slot did not load: %+v", readCheckpointReplies(t, r))
	}
	if !r.started.Restored || r.started.Width != 176 || r.started.Height != 220 {
		t.Fatalf("loaded slot reports restored=%v on %dx%d, want the 176x220 it was taken on",
			r.started.Restored, r.started.Width, r.started.Height)
	}
	if r.started.AutoWidth != 176 || r.started.AutoHeight != 220 {
		t.Fatalf("loaded slot says a start without a size takes %dx%d, want the declared 176x220",
			r.started.AutoWidth, r.started.AutoHeight)
	}
}
