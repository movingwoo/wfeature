package backend

import (
	"testing"
	"time"
)

func TestClampSpeedKeepsWhatAHostOffers(t *testing.T) {
	// The values the browser's own control carries all pass through unchanged:
	// a person who picks one and does not get it is worse served than one with
	// no control at all.
	for _, offered := range []float64{0.25, 0.5, 0.75, 1, 1.5, 2, 4} {
		if got := ClampSpeed(offered); got != offered {
			t.Errorf("ClampSpeed(%v) = %v, want it unchanged", offered, got)
		}
	}
	for _, testCase := range []struct{ in, want float64 }{
		{0, 1}, {-2, 1}, {0.01, SpeedFloor}, {1000, SpeedCeiling},
	} {
		if got := ClampSpeed(testCase.in); got != testCase.want {
			t.Errorf("ClampSpeed(%v) = %v, want %v", testCase.in, got, testCase.want)
		}
	}
}

func TestSpeedClockRunsAtAMultipleOfItsSource(t *testing.T) {
	source := time.Unix(0, 0)
	clock := NewSpeedClock(func() time.Time { return source })
	start := clock.Now()

	source = source.Add(10 * time.Millisecond)
	if seen := clock.Now().Sub(start); seen != 10*time.Millisecond {
		t.Fatalf("at the written speed 10ms of source is %v, want 10ms", seen)
	}

	clock.SetSpeed(2)
	// Rebasing rather than jumping: the time already seen is kept.
	if seen := clock.Now().Sub(start); seen != 10*time.Millisecond {
		t.Errorf("changing the rate moved the clock to %v, want it left at 10ms", seen)
	}
	source = source.Add(10 * time.Millisecond)
	if seen := clock.Now().Sub(start); seen != 30*time.Millisecond {
		t.Errorf("at twice the speed 20ms of source is %v, want 30ms", seen)
	}

	// What a Host waits comes back down by the same factor, and a deadline on
	// the game's clock lands where the source will reach it.
	if cost := clock.SourceDuration(50 * time.Millisecond); cost != 25*time.Millisecond {
		t.Errorf("50ms of the game's time costs %v, want 25ms", cost)
	}
	due := clock.Now().Add(50 * time.Millisecond)
	if arrives := clock.SourceInstant(due).Sub(source); arrives != 25*time.Millisecond {
		t.Errorf("a deadline 50ms away arrives in %v of source time, want 25ms", arrives)
	}

	clock.SetSpeed(0.5)
	source = source.Add(10 * time.Millisecond)
	if seen := clock.Now().Sub(start); seen != 35*time.Millisecond {
		t.Errorf("at half the speed a further 10ms of source is %v, want 35ms", seen)
	}
}

func TestSpeedClockReadsTheWallWithNoSource(t *testing.T) {
	clock := NewSpeedClock(nil)
	if clock.Now().IsZero() {
		t.Error("a clock with no source answered the zero time")
	}
	if speed := clock.Speed(); speed != 1 {
		t.Errorf("speed = %v, want 1", speed)
	}
}
