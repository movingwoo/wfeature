package wipicrt

import "time"

// WIPICTimer is a registered MC_knlSetTimer request, or a task a platform
// queued beside them. See wipicDefTimer and wipicSetTimer in runtime.go, and
// serviceTimersLocked in client.go.
type WIPICTimer struct {
	// PaintedCard is what this callback last painted, when it asked for a
	// repaint, for a platform whose screen is a stack of cards: a rearmed
	// callback that requests paint owns its card between requests. The engine
	// only carries it. See RuntimeHooks.RepaintCard.
	PaintedCard any
	Pointer     uint32
	Callback    uint32
	Param       uint32
	Delay       uint64
	// Due is when the callback may run. A timer is the other way a WIPI-C
	// game paces itself, so the delay is a real wait rather than a label.
	Due time.Time
	// Task and Owner are set instead of callback when the timer is the
	// platform's own, such as a java/util/Timer task: the task is what the
	// platform runs, and the owner is what scheduled it, which is what its
	// cancel has to find. Period is how long after each run the next one is
	// due, and zero means it runs once. See ClientHooks.
	Task   any
	Owner  any
	Period time.Duration
}
