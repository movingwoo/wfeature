package wipicrt

import (
	"testing"
	"time"
)

// hostClock is a manual clock this package did not build. A Host builds one
// clock and hands it to whichever platform opens the archive, so the clock a
// Clet is given is as likely to be another platform's as this package's own.
type hostClock struct{ now time.Time }

func (clock *hostClock) Now() time.Time                 { return clock.now }
func (clock *hostClock) Advance(duration time.Duration) { clock.now = clock.now.Add(duration) }
func (clock *hostClock) Set(instant time.Time)          { clock.now = instant }

func TestSkipToNextDeadlineMovesAHostsManualClock(t *testing.T) {
	clock := &hostClock{}
	wake := time.Time{}.Add(40 * time.Millisecond)
	client := &Client{Clock: clock, ClientWakeAt: wake}
	if !client.SkipToNextDeadline() {
		t.Fatal("SkipToNextDeadline left a Host's manual clock where it was")
	}
	if !clock.now.Equal(wake) {
		t.Fatalf("clock is at %v after the skip, want the deadline %v", clock.now, wake)
	}
}

func TestBatchClockAdvancesAHostsManualClock(t *testing.T) {
	// Inside a service call the guest's own execution moves the clock, so a
	// guest that waits there by polling it reaches the instant it waits for.
	clock := &hostClock{}
	client := &Client{Clock: clock, ServiceDepth: 1}
	client.AdvanceBatchClockTo(3 * BatchGuestStepsPerMillisecond)
	if got := clock.now.Sub(time.Time{}); got != 3*time.Millisecond {
		t.Fatalf("clock moved %v inside a service call, want 3ms", got)
	}
	// Between calls the Host moves it, and the guest's execution does not.
	client.ServiceDepth = 0
	client.AdvanceBatchClockTo(9 * BatchGuestStepsPerMillisecond)
	if got := clock.now.Sub(time.Time{}); got != 3*time.Millisecond {
		t.Fatalf("clock is %v after execution outside a service call, want it left at 3ms", got)
	}
}
