package ktf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// newBudgetedTestRuntime is newPacedTestRuntime with a real step window, which
// the service budget needs: the renewal hook charges one window per call, and
// a core with no ceiling of its own would make the step allowance unreachable.
func newBudgetedTestRuntime(t *testing.T, clock Clock, window uint64) *Client {
	t.Helper()
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{MaxSteps: window})
	if err != nil {
		t.Fatal(err)
	}
	client.clock = clock
	client.SetSpeed(1)
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	client.runtime = runtime
	return client
}

// TestAClockPollIsNotChargedToTheStepAllowance is a title's opening sequence:
// a WIPI timer callback that draws, waits, and draws again, waiting by polling
// the platform clock inside a counted delay loop because a handset has no
// scheduler to yield to. Five seconds of that is hundreds of millions of steps
// spent passing the time, and charging them to the step allowance ended the
// title on its first screen.
func TestAClockPollIsNotChargedToTheStepAllowance(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client := newBudgetedTestRuntime(t, clock, 1000)
	client.serviceSteps = 2000
	ctx := context.Background()
	closeScope := client.beginHostService(ctx)
	defer closeScope()

	// Ten windows against an allowance of two: every one of them is a window
	// the guest spent asking the time on a clock that moved under it.
	for window := 0; window < 10; window++ {
		client.runtime.guestMillis()
		clock.Advance(100 * time.Millisecond)
		if err := client.continueHostService(ctx, client.serviceSteps); err != nil {
			t.Fatalf("window %d of a clock poll failed: %v", window, err)
		}
	}
}

// TestAFrozenClockStillEndsAServiceCall is the other Host, and the reason the
// clock has to have moved. A batch Host holds the clock still for the length
// of a service call, so a title that busy-waits there never sees its wait end;
// renewing that for free would hang the run instead of failing it in seconds.
//
// A guest that executes nothing between its reads buys no time with them
// either, so this is still what a clock nothing moves ends as.
func TestAFrozenClockStillEndsAServiceCall(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client := newBudgetedTestRuntime(t, clock, 1000)
	client.serviceSteps = 2000
	ctx := context.Background()
	closeScope := client.beginHostService(ctx)
	defer closeScope()

	var err error
	for window := 0; window < 10 && err == nil; window++ {
		client.runtime.guestMillis()
		err = client.continueHostService(ctx, client.serviceSteps)
	}
	if !errors.Is(err, ErrServiceStepLimit) {
		t.Fatalf("a clock poll on a clock that never moves ended with %v, want the step limit", err)
	}
}

// TestABatchHostClockRunsAtTheGuestsOwnPace is the title this changed for: a
// WIPI timer callback that draws its opening logo and then holds it with a
// delay loop of the guest's own — read the platform clock, spin a counted pad
// loop of some forty thousand iterations, read it again — until half a second
// has passed. On a Host showing the game to a person that is half a second; on
// a batch Host whose clock only moves between ticks it was never anything at
// all, and the title spent its whole step allowance eight ticks in without
// leaving its first screen.
func TestABatchHostClockRunsAtTheGuestsOwnPace(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client := newBudgetedTestRuntime(t, clock, 1000)
	ctx := context.Background()
	closeScope := client.beginHostService(ctx)
	defer closeScope()

	// The pad loop the guest spins between two reads of the clock, in steps.
	const padSteps = 160_000
	started := client.runtime.guestMillis()
	steps := client.core.Steps()
	for read := 0; read < 10_000; read++ {
		steps += padSteps
		client.advanceBatchClockTo(steps)
		if client.runtime.guestMillis()-started >= 500 {
			if spent := steps - client.core.Steps(); spent > serviceDefaultSteps {
				t.Fatalf("half a second of waiting cost %d steps, past the %d one call is allowed",
					spent, uint64(serviceDefaultSteps))
			}
			return
		}
	}
	t.Fatalf("half a second of waiting never passed: the clock reached %d of %d",
		client.runtime.guestMillis(), started+500)
}

// TestAHostOwnedClockOnlyMovesInsideAServiceCall keeps the change where the
// problem is. Between calls a batch Host jumps its own clock to the next
// deadline, and a clock that also crept forward under the guest's own
// execution would arrive at that jump from somewhere the Host did not put it.
func TestAHostOwnedClockOnlyMovesInsideAServiceCall(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client := newBudgetedTestRuntime(t, clock, 1000)
	before := clock.Now()
	client.advanceBatchClockTo(client.core.Steps() + 100*batchGuestStepsPerMillisecond)
	if moved := clock.Now(); !moved.Equal(before) {
		t.Fatalf("a clock outside every service call moved to %s from %s", moved, before)
	}
}

// TestAWallClockIsNotTheHostsToAdvance is the Host showing the game to a
// person. Real time passes on its own, so there is nothing here to correct and
// nothing here may touch.
func TestAWallClockIsNotTheHostsToAdvance(t *testing.T) {
	client := newBudgetedTestRuntime(t, wallClock{}, 1000)
	ctx := context.Background()
	closeScope := client.beginHostService(ctx)
	defer closeScope()
	// The assertion is that this neither panics nor type-asserts its way onto
	// a clock it does not own; a wall clock has no reading to compare.
	client.advanceBatchClockTo(client.core.Steps() + 100*batchGuestStepsPerMillisecond)
	if _, manual := client.clock.(*ManualClock); manual {
		t.Fatal("the wall-clock session ended up on a manual clock")
	}
}

// TestAWaitThatNeverEndsFailsOnTheWaitAllowance keeps the ceiling doing its
// job in the case the step allowance no longer covers: a guest polling a clock
// for an instant that will never satisfy it holds the Host goroutine, and
// nothing else on this platform can run while it does.
func TestAWaitThatNeverEndsFailsOnTheWaitAllowance(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client := newBudgetedTestRuntime(t, clock, 1000)
	client.serviceWait = time.Second
	ctx := context.Background()
	closeScope := client.beginHostService(ctx)
	defer closeScope()

	var err error
	for window := 0; window < 20 && err == nil; window++ {
		client.runtime.guestMillis()
		clock.Advance(100 * time.Millisecond)
		err = client.continueHostService(ctx, client.serviceSteps)
	}
	if !errors.Is(err, ErrServiceWaitLimit) {
		t.Fatalf("a wait that never ends failed with %v, want the wait allowance", err)
	}
}
