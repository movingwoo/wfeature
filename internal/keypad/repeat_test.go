package keypad

import (
	"testing"
	"time"
)

// The handset's own numbers: nothing before 600ms of holding, then one every
// 250ms. The cadence the operating system sends instead is thirty a second,
// which is what a title reads as a run of taps.
func TestAHeldKeyRepeatsAtTheHandsetsCadence(t *testing.T) {
	var repeat Repeat
	repeat.Holding(148, true)

	if _, due := repeat.Due(599 * time.Millisecond); due {
		t.Fatal("a repeat before the handset's delay was up")
	}
	code, due := repeat.Due(time.Millisecond)
	if !due || code != 148 {
		t.Fatalf("first repeat = (%d, %v), want (148, true)", code, due)
	}
	if _, due := repeat.Due(249 * time.Millisecond); due {
		t.Fatal("a second repeat inside the interval")
	}
	if _, due := repeat.Due(time.Millisecond); !due {
		t.Fatal("no repeat once the interval was up")
	}
}

// A Host that stalled owes the backlog to nobody: the repeats it missed are
// not delivered at once, which would be the burst that reads as a run of taps.
func TestAStallDoesNotDeliverTheRepeatsItMissed(t *testing.T) {
	var repeat Repeat
	repeat.Holding(148, true)
	if _, due := repeat.Due(10 * time.Second); !due {
		t.Fatal("no repeat after ten seconds of holding")
	}
	if _, due := repeat.Due(0); due {
		t.Fatal("a second repeat with no time passed")
	}
	if _, due := repeat.Due(249 * time.Millisecond); due {
		t.Fatal("the next repeat came sooner than an interval after the last")
	}
	if _, due := repeat.Due(time.Millisecond); !due {
		t.Fatal("no repeat an interval after the last one")
	}
}

// Letting go stops it, and the next key starts the wait again rather than
// inheriting what the last one had already served.
func TestReleasingStopsTheRepeatAndAnotherKeyStartsItOver(t *testing.T) {
	var repeat Repeat
	repeat.Holding(148, true)
	if _, due := repeat.Due(500 * time.Millisecond); due {
		t.Fatal("a repeat before the delay was up")
	}
	repeat.Holding(0, false)
	if _, due := repeat.Due(time.Second); due {
		t.Fatal("a key that is not held repeated")
	}

	repeat.Holding(141, true)
	if _, due := repeat.Due(500 * time.Millisecond); due {
		t.Fatal("the new key inherited what the old one had served")
	}
	code, due := repeat.Due(100 * time.Millisecond)
	if !due || code != 141 {
		t.Fatalf("repeat = (%d, %v), want (141, true)", code, due)
	}
}

// A thumb rolling onto another direction pressed that one, so the wait starts
// again — the same key going on being held does not.
func TestHoldingTheSameKeyDoesNotRestartTheWait(t *testing.T) {
	var repeat Repeat
	// Five hundred milliseconds of holding, told to the timer once a tick the
	// way a session tells it.
	for tick := 0; tick < 5; tick++ {
		repeat.Holding(146, true)
		if _, due := repeat.Due(100 * time.Millisecond); due {
			t.Fatalf("a repeat %dms into the hold", (tick+1)*100)
		}
	}
	repeat.Holding(146, true)
	if _, due := repeat.Due(100 * time.Millisecond); !due {
		t.Fatal("no repeat once the delay was up")
	}
}

// The two numbers are settable, because a session is not the only thing that
// may want them and a test is not the only reason to change one.
func TestTheCadenceIsSettable(t *testing.T) {
	repeat := Repeat{Delay: 10 * time.Millisecond, Interval: 5 * time.Millisecond}
	repeat.Holding(35, true)
	if _, due := repeat.Due(10 * time.Millisecond); !due {
		t.Fatal("no repeat after the delay it was given")
	}
	if _, due := repeat.Due(5 * time.Millisecond); !due {
		t.Fatal("no repeat after the interval it was given")
	}
}

// A tick is not free and rarely lands on the deadline. The overshoot is
// carried, so a cadence measured over a run of ticks is the handset's rather
// than every interval rounded up to the tick that noticed it.
func TestTheCadenceIsNotRoundedUpToTheTick(t *testing.T) {
	const tick = 80 * time.Millisecond
	var repeat Repeat
	repeat.Holding(148, true)

	repeats, elapsed := 0, time.Duration(0)
	for ; elapsed < 10*time.Second; elapsed += tick {
		if _, due := repeat.Due(tick); due {
			repeats++
		}
	}
	// The delay, then one every interval: rounding each up to the next tick
	// would lose about a fifth of them.
	want := int((elapsed - HandsetRepeatDelay) / HandsetRepeatInterval)
	if repeats < want-1 || repeats > want+1 {
		t.Errorf("%d repeats over %v of 80ms ticks, want about %d", repeats, elapsed, want)
	}
}
