package ktf

import (
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A game paces itself. Its frame loop asks for a wait — Thread.sleep, a WIPI
// timer delay, an idle getNextEvent — and the length of that wait is the only
// thing that decides how fast the game runs. So the Host's job is not to pick
// a speed but to honour the waits the guest asked for, and every wait in this
// package resolves to a deadline on the Clock below.
//
// Two kinds of Host want two different answers to "what time is it". A Host
// showing the game to a person wants the wall clock, so a 50ms wait costs 50ms
// and the game runs at the speed it was written for. A Host running a batch of
// ticks to reach a first frame wants the waits to cost nothing, because it is
// measuring what the guest computes, not how long it takes. Injecting the
// clock is what lets both hold: the first passes nothing and gets the wall
// clock, the second passes a ManualClock and jumps it to each next deadline.
type Clock interface {
	Now() time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

// ManualClock is a Clock that only moves when a Host moves it. Hosts that run
// ticks in a batch — the CLI's frame probes, tests — pair it with
// Session.SkipToNextDeadline so a paced game advances a wait per tick instead
// of waiting out the wait.
type ManualClock struct {
	mutex sync.Mutex
	now   time.Time
}

// manualClockEpoch is where a manual clock starts when the caller names no
// instant: 2007-01-01T00:00:00Z, which is a plausible date for these handsets
// and lands a title's calendar arithmetic somewhere sane.
const manualClockEpochMillis = 1167609600000

// NewManualClock starts a manual clock at start. A zero start uses a fixed
// date rather than the wall clock.
//
// A manual clock exists so a run repeats — that is the whole of what a probe
// and a repro route are for — and starting it at the wall gave that away for
// timestamps that look real. A title that seeds itself from the time of day
// repeats only if the time of day does: one local title's route stopped at
// 877 ticks, then 2,353, then 877 again, and the run became reproducible the
// moment this stopped moving. Timestamps still look like real dates; they are
// just the same real dates every time.
func NewManualClock(start time.Time) *ManualClock {
	if start.IsZero() {
		start = time.UnixMilli(manualClockEpochMillis).UTC()
	}
	return &ManualClock{now: start}
}

func (clock *ManualClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

// Advance moves the clock forward by duration. Negative durations are ignored:
// guest waits are deadlines, and a clock that goes backwards would revive
// waits that already elapsed.
func (clock *ManualClock) Advance(duration time.Duration) {
	if duration <= 0 {
		return
	}
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(duration)
}

// Set moves the clock to instant, ignoring instants already past.
func (clock *ManualClock) Set(instant time.Time) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	if instant.After(clock.now) {
		clock.now = instant
	}
}

// SetSpeed scales how fast the game runs. 1 is the speed the game was written
// for: every wait costs exactly what the guest asked for. 2 halves the waits
// and runs the guest clock twice as fast, so a game that measures elapsed time
// itself speeds up by the same factor instead of fighting the change. Values
// outside [0.1, 16] clamp, and a zero or negative multiplier selects 1.
func (client *Client) SetSpeed(multiplier float64) {
	if client == nil {
		return
	}
	client.run.Lock()
	defer client.run.Unlock()
	client.speed = clampSpeed(multiplier)
}

// Speed reports the current multiplier.
func (client *Client) Speed() float64 {
	if client == nil {
		return 1
	}
	client.run.Lock()
	defer client.run.Unlock()
	return client.speedOrDefault()
}

// ClampSpeed answers the multiplier a session would actually run at. A Host
// that keeps a speed setting of its own across sessions stores this rather than
// what it was handed, so the setting it shows is the setting in force.
func ClampSpeed(multiplier float64) float64 {
	return backend.ClampSpeed(multiplier)
}

func clampSpeed(multiplier float64) float64 {
	return backend.ClampSpeed(multiplier)
}

// speedOrDefault answers the multiplier a client with no configured speed
// runs at. Every caller is covered by the run lock — either it holds the lock
// itself, or it is guest code running inside a service call the Host holds the
// lock for.
func (client *Client) speedOrDefault() float64 {
	if client == nil || client.speed <= 0 {
		return 1
	}
	return client.speed
}

// now reads the session clock. A client built without one runs on the wall
// clock, which is what an interactive Host wants.
func (client *Client) now() time.Time {
	if client == nil {
		return time.Now()
	}
	client.costs.ClockReads++
	if client.clock == nil {
		return time.Now()
	}
	return client.clock.Now()
}

// waitDeadline converts a wait the guest asked for into the instant the Host
// will hold it until. A wait is divided by the speed multiplier, so 2x speed
// halves it.
func (client *Client) waitDeadline(wait time.Duration) time.Time {
	now := client.now()
	if wait <= 0 {
		return now
	}
	return now.Add(time.Duration(float64(wait) / client.speedOrDefault()))
}

// deferClientWait records a wait the guest declared on the client thread,
// where there is nothing to park. The Host answers it by holding the client
// thread's next frame work — a paint, a timer round — until the wait has
// elapsed, so a game whose frame loop lives in paint keeps its own rate.
//
// Waits accumulate rather than replace: a paint that sleeps twice asked for
// the sum of both, and taking the later of the two would quietly halve it.
func (client *Client) deferClientWait(wait time.Duration) {
	if client == nil || wait <= 0 {
		return
	}
	base := client.now()
	if client.clientWakeAt.After(base) {
		base = client.clientWakeAt
	}
	client.clientWakeAt = base.Add(time.Duration(float64(wait) / client.speedOrDefault()))
}

// clientThreadDue reports whether the client thread has finished the wait the
// guest declared on it.
func (client *Client) clientThreadDue() bool {
	return client == nil || !client.now().Before(client.clientWakeAt)
}

// NextDeadline reports the earliest instant at which parked guest work becomes
// due — a sleeping thread's wake-up or a timer's delay — and whether any is
// parked. A Host uses it to idle exactly as long as the game asked to wait
// instead of polling, and a batch Host uses it to skip the wait entirely.
// Work that is already due answers an instant in the past.
func (client *Client) NextDeadline() (time.Time, bool) {
	if client == nil {
		return time.Time{}, false
	}
	client.run.Lock()
	defer client.run.Unlock()
	var earliest time.Time
	found := false
	consider := func(deadline time.Time) {
		if !found || deadline.Before(earliest) {
			earliest, found = deadline, true
		}
	}
	// A client wait that has already elapsed is not a deadline. It stays
	// recorded — nothing clears it, because nothing has to — and reporting it
	// answers "there is work due, in the past" forever after the one wait a
	// title ever declares on this thread. That costs two things: a Host on the
	// wall clock never idles again, and a Host on a manual clock can never
	// skip, so its clock stops. A title whose first scene is a scheduled Timer
	// task then never reaches it, because the task's deadline is a second away
	// on a clock that no longer moves.
	if client.clientWakeAt.After(client.now()) {
		consider(client.clientWakeAt)
	}
	for _, worker := range client.workers {
		consider(worker.wakeAt)
	}
	if client.runtime != nil {
		// A guest thread that has been started but not yet adopted as a worker
		// is runnable right now, and the next round is what adopts it. Leaving
		// it out reports the game as idle until whatever else is parked comes
		// due, and a Host that believes that sleeps through work that was
		// ready — one title started a thread from its paint and then sat for
		// the hundred seconds its only other thread had asked to sleep. It is
		// due rather than parked, so the deadline is the past.
		if len(client.runtime.pendingThreads) > 0 {
			consider(time.Time{})
		}
		// A queued serial Runnable is due on the next idle pass, not now.
		if len(client.runtime.pendingSerial) > 0 {
			consider(client.runtime.serialDueAt)
		}
		for _, timer := range client.runtime.pendingTimers {
			// A Java Timer task has no guest callback address and is due all
			// the same; leaving it out left the Host with nothing to wait for
			// and a title whose whole first scene is one scheduled task never
			// reached it.
			if timer.callback == 0 && timer.task == nil {
				continue
			}
			consider(timer.due)
		}
	}
	return earliest, found
}

// SkipToNextDeadline jumps a manual clock to the next deadline and reports
// whether it moved. It is how a Host that runs ticks in a batch drives a paced
// game: each tick that found nothing due skips to the next wait rather than
// waiting it out. A session on the wall clock cannot skip and answers false —
// real time is not the Host's to move.
func (client *Client) SkipToNextDeadline() bool {
	if client == nil {
		return false
	}
	manual, ok := client.clock.(*ManualClock)
	if !ok {
		return false
	}
	deadline, found := client.NextDeadline()
	if !found || !deadline.After(manual.Now()) {
		return false
	}
	manual.Set(deadline)
	return true
}
