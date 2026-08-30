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

// What a frame period costs, and why the wait a game asks for is not it.
//
// This platform charges guest time to the wall: a frame that costs the handset
// more than the interval it asked for costs the guest nothing, so a title asks
// for a frame every 10ms and gets one, at a speed no handset ever gave it. The
// sibling platform does not have that problem because its guest clock advances
// with the instructions the guest retires, and it is worth being precise about
// which of the two things that buys is wanted here.
//
// **It is the period, not the clock.** The Host sleeps until the deadline this
// returns, so a period that grows takes the wall with it: guest time and real
// time stay in step, and what changes is how many frames fit in a second. A
// title that moves its world a step per frame slows down, which is what a
// handset did to it; a title that moves by its own clock keeps its speed and
// loses frames, which is also what a handset did to it. Neither needs the
// guest's own reading of the clock to be touched, and not touching it is what
// keeps this out of the Host's bookkeeping — see the note on the five reads
// TickFor makes.
//
// **A floor belongs on the period.** Ten milliseconds is this era's idiom for
// "as soon as you can" rather than a request for a hundred frames a second,
// and the specification agrees: a timer is accurate to the resolution the
// system underneath supports. Every local title a person reported as too fast
// at 1x asks for 10ms; every title they reported as fine asks for 25ms or
// more, so the floor has to land between the two and the sibling platform's
// one frame of a sixty hertz display does. It is not a claim about a handset.
//
// **The work is added to the period, and the period runs from the deadline it
// is answering.** A handset's frame is its work and then its wait, so a frame
// that retires more instructions than the modelled handset could run in a
// millisecond is charged for the time they take on top of the period it asked
// for. But the Host has already spent real time retiring them — more of it
// than the handset would, on any scene where this bound binds at all, because
// the emulator runs slower than the handset it models. Charging the work
// against the instant the callback happens to re-arm would charge it twice:
// once on the wall it already took, and again as a wait. So a period charged
// inside a timer callback runs from the deadline that callback was due at, and
// the round's own cost comes out of it. A Host that is faster than the handset
// sleeps out the difference and the guest sees the handset's rate; a Host that
// is slower sleeps not at all and the guest sees the best the Host can do,
// which is what it saw before any of this existed.
//
// **The anchor is the deadline and not the arm**, which is the difference
// between a period and a race. A title that re-arms at the top of its frame
// arms a whole frame after the previous arm, so measuring from that arm gave
// every deadline a head start on the one before it and the period converged on
// half of what was asked — four local titles doubled their round rate before
// this was written down. Anchoring to the deadline being served advances by
// exactly one period per frame however the callback is arranged.
const (
	// minGuestFramePeriod is the floor. See above for why it is not a handset
	// measurement.
	minGuestFramePeriod = 16667 * time.Microsecond
)

// guestInstructionsPerMillisecond is the rate the work bound runs at — the
// speed of the handset this platform stands in for. It decides which scenes
// the charge above slows: one whose frame fits inside its own period at this
// rate is untouched, and one that overruns is slowed in proportion.
//
// It is a variable rather than a constant so that the measurement which picked
// it can be repeated. Nothing in the running emulator writes it.
var guestInstructionsPerMillisecond uint64 = 200_000

// framePeriodDeadline is waitDeadline for the wait that is a frame period: a
// timer's interval, or the sleep a frame loop takes in its own paint. It is a
// separate entry point because the two are not the same request — a game that
// sleeps eight milliseconds mid-frame asked to sleep, not to be shown, and
// flooring that would change what it means.
func (client *Client) framePeriodDeadline(wait time.Duration) time.Time {
	if client == nil {
		return client.waitDeadline(wait)
	}
	now := client.now()
	charged := client.chargedFramePeriod(wait)
	if charged <= 0 {
		return now
	}
	scaled := time.Duration(float64(charged) / client.speedOrDefault())
	// Outside a callback there is no deadline being answered — an initial arm,
	// a title arming a timer from its own thread — and the period runs from
	// now, which is what the guest asked for.
	from := client.servingDue
	if from.IsZero() {
		return now.Add(scaled)
	}
	// A round that overran its own period is already late, and holding it any
	// longer would charge it for work the wall has taken.
	if deadline := from.Add(scaled); deadline.After(now) {
		return deadline
	}
	return now
}

// chargedFramePeriod is a frame period plus what the frame's own work cost on
// the modelled handset, raised to the floor. What the Host has already spent
// on that work is not subtracted here: framePeriodDeadline takes it off the
// deadline instead, which is the same subtraction and keeps this reading as
// what the handset would have charged.
func (client *Client) chargedFramePeriod(wait time.Duration) time.Duration {
	if client == nil {
		return wait
	}
	wait += client.workSinceLastPeriod()
	if wait < minGuestFramePeriod {
		wait = minGuestFramePeriod
	}
	return wait
}

// workSinceLastPeriod is what the guest has retired since the last period was
// charged, as time on the modelled handset. Reading it moves the baseline, so
// the work between two frames is charged to one of them and not to both.
func (client *Client) workSinceLastPeriod() time.Duration {
	if client == nil || client.core == nil {
		return 0
	}
	steps := client.core.Steps()
	retired := steps - client.workBaseline
	client.workBaseline = steps
	if guestInstructionsPerMillisecond == 0 {
		return 0
	}
	return time.Duration(float64(retired) / float64(guestInstructionsPerMillisecond) * float64(time.Millisecond))
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
	// A title whose frame loop lives in its card paint declares its period
	// here rather than on a timer, so it is the same request and takes the
	// same charge.
	wait = client.chargedFramePeriod(wait)
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
		// A queued serial Runnable is due on the next idle pass, not now —
		// and never before the wait the client thread declared is over,
		// because that is the thread it runs on. Answering the earlier of the
		// two reported work as due at an instant the dispatch would refuse, so
		// a Host on a manual clock never advanced it and the wait never ended.
		if len(client.runtime.pendingSerial) > 0 {
			due := client.runtime.serialDueAt
			if client.clientWakeAt.After(due) {
				due = client.clientWakeAt
			}
			consider(due)
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
