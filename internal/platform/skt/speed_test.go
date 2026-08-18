package skt

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// TestSpeedMovesTheClockTheMIDletMeasuresAgainst covers the other half of the
// setting. The VM scales what a sleep costs; this scales what the title reads
// when it asks how much time that sleep took, and the two have to agree or a
// title stepping by elapsed time would undo the change.
func TestSpeedMovesTheClockTheMIDletMeasuresAgainst(t *testing.T) {
	source := time.Unix(1, 0)
	pace := backend.NewSpeedClock(func() time.Time { return source })
	runtime := &Runtime{pace: pace, paceStart: pace.Now()}

	if speed := runtime.Speed(); speed != 1 {
		t.Fatalf("a runtime with no setting runs at %v, want 1", speed)
	}
	source = source.Add(10 * time.Millisecond)
	if elapsed := runtime.GuestElapsed(); elapsed != 10*time.Millisecond {
		t.Errorf("guest elapsed = %v, want 10ms", elapsed)
	}

	runtime.SetSpeed(2)
	source = source.Add(10 * time.Millisecond)
	if elapsed := runtime.GuestElapsed(); elapsed != 30*time.Millisecond {
		t.Errorf("guest elapsed at twice the speed = %v, want 30ms", elapsed)
	}
	// System.currentTimeMillis and RecordStore.getLastModified read this one
	// clock, so a title comparing them cannot measure a negative interval.
	if millis := runtime.clockMillis(); millis != pace.Now().UnixMilli() {
		t.Errorf("clockMillis = %d, want the guest clock's %d", millis, pace.Now().UnixMilli())
	}

	// Every value the browser's own control carries arrives unchanged.
	for _, offered := range []float64{0.25, 0.5, 0.75, 1, 1.5, 2, 4} {
		runtime.SetSpeed(offered)
		if speed := runtime.Speed(); speed != offered {
			t.Errorf("speed = %v, want the %v that was asked for", speed, offered)
		}
	}

	// A runtime a Host never started still answers rather than panicking.
	var absent *Runtime
	if speed := absent.Speed(); speed != 1 {
		t.Errorf("an absent runtime runs at %v, want 1", speed)
	}
	absent.SetSpeed(2)
	if elapsed := absent.GuestElapsed(); elapsed != 0 {
		t.Errorf("an absent runtime has seen %v pass, want 0", elapsed)
	}
}
