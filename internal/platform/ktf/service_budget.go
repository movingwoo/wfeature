package ktf

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// A Host service call — a timer callback, a key delivery, a card paint — runs
// guest code on the client thread, and that guest code is sometimes allowed to
// take a very long time. A title can load a whole save file and its enemy,
// drop, and map tables inside the keyNotify that selects a save slot, which is
// far past the step ceiling one bounded run gets; the games that verify their
// DRM before the first frame do the same. Failing those runs would call a
// working game broken.
//
// So the step ceiling stops being fatal on this thread: reaching it asks
// beginHostService's hook whether to continue instead. The hook continues
// while two things hold — the Host's context is live, and the call has not
// spent its whole service allowance — which is what keeps the ceiling doing
// its real job. Cancelling the context is the Host's abort path, and it is the
// only one that works mid-call: while the client thread runs, no worker
// thread, timer, or paint can run, so a guest waiting on another thread's
// progress inside a service call waits forever no matter how much execution it
// is granted. The allowance is what ends that case.
//
// **Steps are the wrong unit for a call that is waiting rather than working.**
// A title's whole opening sequence is a WIPI timer callback that draws, then
// waits, then draws again — and it waits by polling the platform clock inside
// a counted delay loop, which is what a handset with no scheduler to yield to
// does. Five seconds of that is eight hundred million steps of a loop whose
// only purpose is to pass the time, and charging them to the step allowance
// called a title that was working exactly as written a title that hung.
//
// A window the guest spent asking what time it is, on a clock that moved
// under it, is therefore renewed without being charged, and what bounds it
// instead is the session clock: a call may wait `serviceDefaultWait` before it
// fails. The clock has to have moved because that is what separates the two
// Hosts — a batch Host holds the clock still for the length of a service call,
// so a title that busy-waits there never sees the wait end, and turning that
// into a free renewal would hang the run rather than fail it in seconds.
var ErrServiceStepLimit = errors.New("KTF Host service call exceeded its step allowance")

// ErrServiceWaitLimit ends a call that has spent the whole wait allowance
// polling the clock. It is a different failure from the step limit and says so:
// the guest was waiting, not computing, and no amount of execution would have
// finished it.
var ErrServiceWaitLimit = errors.New("KTF Host service call waited past its wait allowance")

// serviceDefaultSteps is the total ARM execution one Host service call may
// spend. The longest observed legitimate call, a save load, needs
// under 100M; the rest is headroom for slower loaders on games not measured
// here, still short enough that a guest which will never return fails in
// seconds rather than hanging the Host.
const serviceDefaultSteps = 500_000_000

// serviceDefaultWait is how long one Host service call may spend waiting on
// the session clock. The longest observed legitimate wait is a title's opening
// sequence at 5.2 seconds; three times that leaves room for a slower handset
// setting without letting a guest that polls a clock it will never satisfy
// freeze the Host for longer than a person would wait before reloading.
const serviceDefaultWait = 15 * time.Second

// beginHostService opens a service scope on the client thread and answers the
// function that closes it. Scopes nest: an inner scope keeps the allowance the
// outer one opened, so a service call cannot refresh its own budget.
func (client *Client) beginHostService(ctx context.Context) func() {
	if client == nil || client.thread == nil {
		return func() {}
	}
	client.serviceDepth++
	if client.serviceDepth > 1 {
		return func() { client.serviceDepth-- }
	}
	allowance := client.serviceSteps
	if allowance == 0 {
		allowance = serviceDefaultSteps
	}
	client.serviceRemaining = allowance
	client.serviceStartedAt = client.now()
	client.serviceWindowAt = client.serviceStartedAt
	client.serviceWindowReads = client.guestClockReads
	client.thread.SetLimitHook(func(context.Context) error {
		return client.continueHostService(ctx, allowance)
	})
	return func() {
		client.serviceDepth--
		if client.serviceDepth == 0 {
			client.thread.SetLimitHook(nil)
		}
	}
}

// continueHostService answers whether an exhausted step window may be renewed.
// The context it consults is the one the Host passed to the service call, not
// the one the run carries, because a nested guest call inherits its parent's
// context and the Host's cancellation has to reach every level.
func (client *Client) continueHostService(ctx context.Context, allowance uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := client.now()
	reads := client.guestClockReads
	polled := reads > client.serviceWindowReads
	moved := now.After(client.serviceWindowAt)
	client.serviceWindowReads = reads
	client.serviceWindowAt = now
	if polled && moved {
		if waited := now.Sub(client.serviceStartedAt); waited >= client.waitAllowance() {
			return fmt.Errorf("%w of %s", ErrServiceWaitLimit, client.waitAllowance())
		}
		if client.runtime != nil {
			client.runtime.countDiagnostic("service window renewed on a clock poll")
		}
		return nil
	}
	window := client.core.MaxSteps()
	if client.serviceRemaining <= window {
		client.serviceRemaining = 0
		return fmt.Errorf("%w of %d steps", ErrServiceStepLimit, allowance)
	}
	client.serviceRemaining -= window
	if client.runtime != nil {
		// A renewal means one window's worth of guest execution passed without
		// returning to the Host, which is worth seeing in a debug log even
		// when the call finishes normally.
		client.runtime.countDiagnostic("service window renewed")
	}
	return nil
}

// waitAllowance is how long a service call may wait on the clock.
func (client *Client) waitAllowance() time.Duration {
	if client == nil || client.serviceWait <= 0 {
		return serviceDefaultWait
	}
	return client.serviceWait
}
