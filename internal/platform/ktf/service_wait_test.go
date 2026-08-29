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
