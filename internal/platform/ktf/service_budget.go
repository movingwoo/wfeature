package ktf

import (
	"context"
	"errors"
	"fmt"
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
var ErrServiceStepLimit = errors.New("KTF Host service call exceeded its step allowance")

// serviceDefaultSteps is the total ARM execution one Host service call may
// spend. The longest observed legitimate call, a save load, needs
// under 100M; the rest is headroom for slower loaders on games not measured
// here, still short enough that a guest which will never return fails in
// seconds rather than hanging the Host.
const serviceDefaultSteps = 500_000_000

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
