package lgt

import (
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
