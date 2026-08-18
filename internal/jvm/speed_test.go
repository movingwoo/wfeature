package jvm

import (
	"testing"
	"time"
)

// TestGuestDelayScalesWhatAWaitCosts covers the half of the speed setting the
// VM owns. A MIDlet's frame loop is a thread that sleeps, so what a multiplier
// has to reach first is the sleep: called twice as often against unscaled
// waits, the loop would simply wait the same and gain nothing.
func TestGuestDelayScalesWhatAWaitCosts(t *testing.T) {
	var speed float64
	vm := &VM{config: Options{Speed: func() float64 { return speed }}}

	speed = 2
	if got := vm.guestDelay(50 * time.Millisecond); got != 25*time.Millisecond {
		t.Errorf("a 50ms wait at twice the speed costs %v, want 25ms", got)
	}
	speed = 0.5
	if got := vm.guestDelay(50 * time.Millisecond); got != 100*time.Millisecond {
		t.Errorf("a 50ms wait at half the speed costs %v, want 100ms", got)
	}

	// A speed that says nothing leaves the wait alone, and so does a wait of
	// nothing: Object.wait(0) means "until notified" rather than "for no time".
	speed = 0
	if got := vm.guestDelay(50 * time.Millisecond); got != 50*time.Millisecond {
		t.Errorf("a 50ms wait at an unset speed costs %v, want 50ms", got)
	}
	speed = 2
	if got := vm.guestDelay(0); got != 0 {
		t.Errorf("an untimed wait became %v, want 0", got)
	}

	// A VM whose Host never installed the hook pays nothing for it.
	plain := &VM{}
	if got := plain.guestDelay(50 * time.Millisecond); got != 50*time.Millisecond {
		t.Errorf("a wait with no hook costs %v, want 50ms", got)
	}
}
