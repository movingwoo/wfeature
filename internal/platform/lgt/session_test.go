package lgt

import (
	"testing"
	"time"
)

// TestSpeedBuysGuestTimeMoreCheaply covers the setting a Host offers a person.
//
// The guest's clock here is virtual: a tick moves it and nothing else does. So
// there is no clock to run faster, and what a multiplier changes is what a
// tick of guest time is allowed to cost on the wall — which is the same thing
// from the game's side, and the only thing a Host can act on.
func TestSpeedBuysGuestTimeMoreCheaply(t *testing.T) {
	session := &Session{tick: 20 * time.Millisecond}
	if speed := session.Speed(); speed != 1 {
		t.Fatalf("a session with no setting runs at %v, want 1", speed)
	}
	if cost := session.realTime(20 * time.Millisecond); cost != 20*time.Millisecond {
		t.Errorf("20ms of guest time costs %v at the written speed, want 20ms", cost)
	}

	session.SetSpeed(2)
	if cost := session.realTime(20 * time.Millisecond); cost != 10*time.Millisecond {
		t.Errorf("20ms of guest time costs %v at twice the speed, want 10ms", cost)
	}
	session.SetSpeed(0.5)
	if cost := session.realTime(20 * time.Millisecond); cost != 40*time.Millisecond {
		t.Errorf("20ms of guest time costs %v at half the speed, want 40ms", cost)
	}

	// Every value the browser's own control carries arrives unchanged, and
	// asking for none is asking for the speed the game was written for.
	for _, offered := range []float64{0.25, 0.5, 0.75, 1, 1.5, 2, 4} {
		session.SetSpeed(offered)
		if speed := session.Speed(); speed != offered {
			t.Errorf("speed = %v, want the %v that was asked for", speed, offered)
		}
	}
	session.SetSpeed(0)
	if speed := session.Speed(); speed != 1 {
		t.Errorf("speed after asking for none = %v, want 1", speed)
	}
}
