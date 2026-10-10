package main

import (
	"context"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/platform/ktf"
)

type fakeStepper struct {
	ticks, entries, skips int
	budget                time.Duration
	wait                  time.Duration
	pending               bool
}

func (f *fakeStepper) Tick(context.Context) (bool, error) { f.ticks++; return true, nil }
func (f *fakeStepper) TickFor(_ context.Context, budget time.Duration) (bool, time.Duration, error) {
	f.entries++
	f.budget = budget
	return true, f.wait, nil
}
func (f *fakeStepper) SkipToNextDeadline() bool { f.skips++; return true }
func (f *fakeStepper) NextDeadline() (time.Time, bool) {
	return time.Now().Add(f.wait), f.pending
}

// A probe steps rounds on its own clock; a run on the wall clock is entered
// the way the server enters a session. A round there is the wrong unit: a
// title whose network thread yields in a blocking read made a round cost
// microseconds, and a route's tick-counted waits ended before the title's own
// next frame was due.
func TestAKTFTickOnTheWallClockIsTheServersEntry(t *testing.T) {
	probe := &fakeStepper{}
	if _, err := stepKTF(context.Background(), probe, ktf.NewManualClock(time.Time{}), false); err != nil {
		t.Fatal(err)
	}
	if probe.ticks != 1 || probe.skips != 1 || probe.entries != 0 {
		t.Fatalf("probe step = %d rounds, %d clock jumps, %d entries; want one round and one jump", probe.ticks, probe.skips, probe.entries)
	}

	play := &fakeStepper{wait: 5 * time.Millisecond, pending: true}
	started := time.Now()
	if _, err := stepKTF(context.Background(), play, nil, false); err != nil {
		t.Fatal(err)
	}
	if play.entries != 1 || play.ticks != 0 || play.budget != playEntryBudget {
		t.Fatalf("play step = %d entries of %v, %d rounds; want one entry of %v", play.entries, play.budget, play.ticks, playEntryBudget)
	}
	if elapsed := time.Since(started); elapsed < play.wait {
		t.Fatalf("play step returned after %v, before the %v the guest asked for", elapsed, play.wait)
	}

	// A guest sleeping for seconds still leaves an interrupt a prompt answer.
	long := &fakeStepper{wait: time.Hour, pending: true}
	started = time.Now()
	if _, err := stepKTF(context.Background(), long, nil, false); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 10*idlePollCeiling {
		t.Fatalf("a long guest wait held the loop for %v", elapsed)
	}
}
