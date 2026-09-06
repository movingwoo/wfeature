package backend

import (
	"testing"
	"time"
)

// atTime is a clock a test moves by hand, so a request can be watched running
// out without the test waiting for it.
func atTime(start time.Time) (*Vibrator, func(time.Duration)) {
	now := start
	vibrator := &Vibrator{}
	vibrator.SetClock(func() time.Time { return now })
	return vibrator, func(step time.Duration) { now = now.Add(step) }
}

func TestVibratorReportsNothingUntilTheGuestAsks(t *testing.T) {
	vibrator, _ := atTime(time.Now())
	state := vibrator.State()
	if state.Request != 0 || state.Active() {
		t.Errorf("a guest that has not asked reports %+v", state)
	}
	// The nil vibrator is the platform that does not implement this at all,
	// and it has to be readable rather than a panic.
	var absent *Vibrator
	if absent.State().Request != 0 {
		t.Errorf("a platform with no vibrator reported a request")
	}
	absent.Vibrate(50, 100)
	absent.Stop()
}

func TestVibratorRunsOutOnItsOwn(t *testing.T) {
	vibrator, advance := atTime(time.Now())
	vibrator.Vibrate(60, 100)

	state := vibrator.State()
	if state.Request != 1 || state.Level != 60 || state.Duration != 100*time.Millisecond {
		t.Fatalf("state = %+v", state)
	}
	if !state.Active() || state.Remaining != 100*time.Millisecond {
		t.Errorf("a fresh request is not running: %+v", state)
	}

	advance(60 * time.Millisecond)
	if remaining := vibrator.State().Remaining; remaining != 40*time.Millisecond {
		t.Errorf("remaining = %v, want 40ms", remaining)
	}

	advance(60 * time.Millisecond)
	state = vibrator.State()
	if state.Active() || state.Remaining != 0 {
		t.Errorf("the request did not run out: %+v", state)
	}
	// What the guest asked for is still what it asked for. Remaining is what
	// says it is over, and the request number has not moved because the guest
	// has not asked again.
	if state.Level != 60 || state.Duration != 100*time.Millisecond || state.Request != 1 {
		t.Errorf("the request was forgotten rather than finished: %+v", state)
	}
}

// A duration of zero with a level above zero means until stopped, which is the
// guest's contract. Reading it as "do nothing" is a vibration that silently
// never happens.
func TestVibratorTreatsZeroDurationAsUntilStopped(t *testing.T) {
	vibrator, advance := atTime(time.Now())
	vibrator.Vibrate(100, 0)

	state := vibrator.State()
	if !state.Indefinite() || !state.Active() {
		t.Fatalf("a zero duration was not read as indefinite: %+v", state)
	}
	advance(time.Hour)
	if !vibrator.State().Active() {
		t.Errorf("an indefinite request stopped on its own")
	}

	vibrator.Stop()
	state = vibrator.State()
	if state.Active() || state.Level != 0 {
		t.Errorf("stop did not stop it: %+v", state)
	}
	if state.Request != 2 {
		t.Errorf("stop did not count as a request the Host can see: %+v", state)
	}
}

// A level of zero is how the guest turns the motor off. It is a request like
// any other so that a Host watching the counter learns about it.
func TestVibratorReadsLevelZeroAsOff(t *testing.T) {
	vibrator, _ := atTime(time.Now())
	vibrator.Vibrate(80, 500)
	vibrator.Vibrate(0, 500)
	state := vibrator.State()
	if state.Active() || state.Indefinite() {
		t.Errorf("level zero left the motor running: %+v", state)
	}
	if state.Request != 2 {
		t.Errorf("request = %d, want 2", state.Request)
	}
}

// A request made while one is running replaces it and restarts the clock.
func TestVibratorRestartsRatherThanQueues(t *testing.T) {
	vibrator, advance := atTime(time.Now())
	vibrator.Vibrate(30, 200)
	advance(150 * time.Millisecond)
	vibrator.Vibrate(90, 200)

	state := vibrator.State()
	if state.Level != 90 || state.Remaining != 200*time.Millisecond {
		t.Errorf("the second request did not replace the first: %+v", state)
	}
}

// The same request twice is two buzzes, and the counter is what tells a Host
// so. Comparing the level and duration instead would collapse them into one.
func TestVibratorCountsARepeatedRequestTwice(t *testing.T) {
	vibrator, _ := atTime(time.Now())
	vibrator.Vibrate(50, 100)
	first := vibrator.State().Request
	vibrator.Vibrate(50, 100)
	if second := vibrator.State().Request; second == first {
		t.Errorf("two identical requests share one number (%d)", second)
	}
}

// The values come from game code this project does not control. A title asking
// for 150 means "as hard as you can", and refusing would drop a vibration it
// asked for.
func TestVibratorClampsRatherThanRefuses(t *testing.T) {
	vibrator, _ := atTime(time.Now())
	vibrator.Vibrate(150, -1)
	state := vibrator.State()
	if state.Level != vibrationMaxLevel {
		t.Errorf("level = %d, want %d", state.Level, vibrationMaxLevel)
	}
	if state.Duration != 0 || !state.Indefinite() {
		t.Errorf("a negative duration became %v", state.Duration)
	}
	vibrator.Vibrate(-5, 10)
	if vibrator.State().Level != 0 {
		t.Errorf("a negative level did not read as off")
	}
}
