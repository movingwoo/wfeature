package lgt

import (
	"context"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

func TestLGTPartialFailureInvalidatesEdit(t *testing.T) {
	s := cTextInputFixture(t, 2)
	c := s.client
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var end uint32
	for p := c.clet.HandleEvent; p < c.clet.HandleEvent+256; p += 4 {
		word, err := c.readWord(p)
		if err != nil {
			t.Fatal(err)
		}
		if word == armPopPC {
			end = p
			break
		}
	}
	if end == 0 {
		t.Fatal("fixture return not found")
	}
	if err := c.writeWord(end, 0xe7f000f0); err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "abc"); err == nil {
		t.Fatal("injected callback fault did not fire")
	}
	if got, err := c.readCString(fixtureInputCompleted); err != nil || got != "abc" {
		t.Fatalf("prefix not delivered: %q %v", got, err)
	}
	if err := c.writeWord(end, armPopPC); err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "abc"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("partial failure left edit retryable: %v", err)
	}
	fresh, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Commit(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	if got, err := c.readCString(fixtureInputCompleted); err != nil || got != "next" {
		t.Fatalf("fresh edit not delivered: %q %v", got, err)
	}
}

func TestLGTFaultBeforeDeliveryRemainsRetryable(t *testing.T) {
	s := cTextInputFixture(t, 2)
	c := s.client
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.readWord(c.clet.HandleEvent)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.writeWord(c.clet.HandleEvent, 0xe7f000f0); err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "abc"); err == nil {
		t.Fatal("injected callback fault did not fire")
	}
	if err := c.writeWord(c.clet.HandleEvent, first); err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "abc"); err != nil {
		t.Fatalf("unconsumed edit could not retry: %v", err)
	}
	if got, err := c.readCString(fixtureInputCompleted); err != nil || got != "abc" {
		t.Fatalf("retry not delivered: %q %v", got, err)
	}
	if err := edit.Commit(t.Context(), "abc"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("successful delivery could be repeated: %v", err)
	}
}

func TestKnownCEncodingLimits(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"precomposed Korean", "한글", true},
		{"decomposed Korean", "한", false},
		{"supplementary Unicode", "🙂", false},
		{"newline", "first\nsecond", false},
		{"empty", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateCTextInput(tc.text)
			if (err == nil) != tc.valid {
				t.Fatalf("encoding contract: valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

// A native widget can flush its automaton after taking the completed string,
// a second input-method call inside the Host's own carrier event. That call
// is part of delivering the text, not a key the player pressed: counting it
// reported the field as changed after the whole name had reached the widget,
// and on a widget fed in batches it stopped delivery after the first one.
func TestLGTDeliveryToleratesAFlushInsideTheCarrier(t *testing.T) {
	s := cTextInputFixture(t, 3)
	c := s.client
	if err := c.writeWord(fixtureFlushAfterInput, 1); err != nil {
		t.Fatal(err)
	}
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "abc"); err != nil {
		t.Fatalf("commit through a flushing widget: %v", err)
	}
	if got, err := c.readCString(fixtureInputCompleted); err != nil || got != "abc" {
		t.Fatalf("delivered %q %v", got, err)
	}
	if err := edit.Commit(t.Context(), "abc"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("delivered edit could be repeated: %v", err)
	}
	// A key the player presses between snapshot and commit is still a change.
	edit, err = s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.callClet(t.Context(), "handleCletEvent", c.clet.HandleEvent, []uint32{EventKeyPressed, '5', 0}); err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "abc"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after a player key: %v", err)
	}
}

// A title's name widget can call the input method only from its key handler:
// one local title calls it once while starting and then not until a key
// arrives on its name screen, so the key that opened the screen bypassed the
// automaton and following the keys left the empty field unavailable. Input is
// offered from the first input-method call on, and a commit where no widget
// takes keys reports failure and leaves the offer in place.
func TestLGTCInputIsOfferedOnceTheTitleUsesTheInputMethod(t *testing.T) {
	session, err := StartSession(t.Context(), fixtureArchive(t), SessionOptions{Width: 16, Height: 8, MaxSteps: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	c := session.client
	press := func(key uint32) {
		t.Helper()
		c.SendEvent(EventKeyPressed, key, 0)
		if err := c.deliverEvents(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	// A title that never uses the input method has nothing to deliver to.
	if _, err := session.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("input offered before any input-method call: %v", err)
	}
	// A mode selected while starting, before any key, is enough.
	callSlot(t, c, slotIMSetCurrentMode, 2)
	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatalf("start-up mode selection: %v", err)
	}
	// A key the title handles without the automaton leaves it offered.
	if err := c.writeWord(fixtureRouteKeys, 1); err != nil {
		t.Fatal(err)
	}
	press('#')
	if _, err := session.TextInput(t.Context()); err != nil {
		t.Fatalf("key that bypassed the automaton withdrew input: %v", err)
	}
	// A commit there reaches no widget: it fails, and the game sees a whole
	// tap of the carrier key rather than a press that stays down. Input then
	// waits for new evidence of a field, so neither a retry nor a new edit
	// hands the game the key again; the player's next press is such
	// evidence, and input is offered for the screen the player moves to.
	if err := edit.Commit(t.Context(), "ab"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("unrouted carrier: %v", err)
	}
	if len(c.events) != 1 || c.events[0] != (pendingEvent{EventKeyReleased, hostTextInputCarrier, 0}) {
		t.Fatalf("queued after an unrouted carrier: %+v", c.events)
	}
	if err := c.deliverEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "ab"); !errors.Is(err, backend.ErrTextInputChanged) || len(c.events) != 0 {
		t.Fatalf("retry after an unrouted carrier: %v, queued %+v", err, c.events)
	}
	if _, err := session.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("input offered right after an unrouted carrier: %v", err)
	}
	press('#')
	if err := c.writeWord(fixtureRouteKeys, 0); err != nil {
		t.Fatal(err)
	}
	edit, err = session.TextInput(t.Context())
	if err != nil {
		t.Fatalf("input after the player's press: %v", err)
	}
	if err := edit.Commit(t.Context(), "ab"); err != nil {
		t.Fatal(err)
	}
	if got, err := c.readCString(fixtureInputCompleted); err != nil || got != "ab" {
		t.Fatalf("delivered %q %v", got, err)
	}
	if len(c.events) != 0 {
		t.Fatalf("a delivered carrier queued %+v", c.events)
	}
}

// A record written while input followed the keys says whether input was open
// when it was taken, and a key the title handled without the automaton had
// closed it. A title that had called the automaton by then has used the input
// method, and a load offers input to it as the running session does.
func TestLGTCInputOfferSurvivesARecordThatHadItClosed(t *testing.T) {
	archive := fixtureArchive(t)
	ctx := t.Context()
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	source, err := StartSession(ctx, archive, SessionOptions{SaveStore: store, Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close(context.Background()) })
	source.client.SendEvent(EventKeyPressed, '#', 0)
	if err := source.client.deliverEvents(ctx); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := source.CaptureCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var saved sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.Client.TextInput.Active || saved.Client.TextInput.Calls != 1 {
		t.Fatalf("recorded %+v, want a title that has called the automaton once", saved.Client.TextInput)
	}
	// An older record of a title that had called the automaton, of one that
	// had only listed or selected its modes while starting, and of one that
	// had shown no sign of the input method at all.
	for _, older := range []struct {
		name        string
		calls       uint64
		table, mode uint32
		offered     bool
	}{
		{"called the automaton", saved.Client.TextInput.Calls, 0, 0, true},
		{"listed its modes", 0, 0x1000, 0, true},
		{"selected a mode", 0, 0, 2, true},
		{"no input method", 0, 0, 0, false},
	} {
		record := saved
		record.Client.TextInput.Active = false
		record.Client.TextInput.Calls = older.calls
		record.Client.InputModeTable, record.Client.InputMode = older.table, older.mode
		encoded := checkpoint
		if encoded.Runtime, err = backend.EncodeCheckpointRecord(record); err != nil {
			t.Fatal(err)
		}
		other := boundarySaves(t, store)
		restored, err := prepareFixtureCheckpoint(t, archive, encoded, other).Commit(ctx, nil, other)
		if err != nil {
			t.Fatal(err)
		}
		_, err = restored.TextInput(ctx)
		_ = restored.Close(context.Background())
		if older.offered && err != nil || !older.offered && !errors.Is(err, backend.ErrNoTextInput) {
			t.Fatalf("%s: input after loading the older record: %v", older.name, err)
		}
	}
}
