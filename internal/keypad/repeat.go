package keypad

import "time"

// The handset's own repeat, which the WIPI specification writes as the
// `KEYREPEAT` property "600:250": the first repeat is owed after 600ms of a key
// being held, and one more every 250ms after that.
const (
	HandsetRepeatDelay    = 600 * time.Millisecond
	HandsetRepeatInterval = 250 * time.Millisecond
)

// Repeat is the clock a held key repeats on, for the two runtimes that have a
// repeat event of their own — WIPI Java's keyNotify and a MIDP Canvas's
// keyRepeated. It is a timer rather than a translator: what a Host reports as a
// repeat is the operating system's key repeat, at a cadence the user configured
// and no handset ever had, so a Host's repeat is dropped and this makes the
// handset's instead.
//
// The time it is given is the guest's, not the wall's. Everything else about a
// session is scaled by the speed multiplier — the guest's clock and the waits
// between its callbacks — and a repeat is the handset repeating, so it runs at
// the pace the handset is running at.
//
// A Repeat belongs to one session and is used from the goroutine that drives
// it, the same discipline Pad requires.
type Repeat struct {
	// Delay and Interval override the handset's numbers. Zero means the
	// handset's.
	Delay, Interval time.Duration

	code int32
	held bool
	// due is how much guest time is left before the next repeat is owed.
	due time.Duration
}

// Holding names the key the guest currently believes is down, which is what the
// pad answers rather than what the Host is reporting. A different key restarts
// the wait from the delay: a thumb that rolled onto another direction pressed
// that one, and a handset waits again before repeating it.
func (repeat *Repeat) Holding(code int32, held bool) {
	if !held {
		repeat.Forget()
		return
	}
	if repeat.held && repeat.code == code {
		return
	}
	repeat.code, repeat.held, repeat.due = code, true, repeat.delay()
}

// Due advances the timer by elapsed guest time and answers the one repeat it
// owes, if it owes one.
//
// **At most one repeat per call, whatever the time given.** A Host asks once a
// tick, and a tick that ran long — or a session that was paused — owes the
// repeats it missed to nobody: delivering them at once is a run of events
// arriving together, which is exactly the burst that made the operating
// system's cadence read as a run of taps.
//
// **What it does keep is the phase**, up to one interval. A tick is not free
// and rarely lands on the deadline, so measuring the next interval from *here*
// rounds every one of them up to the tick: on a platform whose rounds cost
// 80ms, an interval of 250 becomes 320 and a cadence of four a second becomes
// three. Carrying the overshoot instead holds the average at the handset's
// number and leaves only the jitter. A gap longer than a whole interval is a
// stall rather than jitter, and there the phase is dropped with the backlog.
func (repeat *Repeat) Due(elapsed time.Duration) (int32, bool) {
	if !repeat.held || elapsed <= 0 {
		return 0, false
	}
	repeat.due -= elapsed
	if repeat.due > 0 {
		return 0, false
	}
	if repeat.due += repeat.interval(); repeat.due <= 0 {
		repeat.due = repeat.interval()
	}
	return repeat.code, true
}

// Forget stops repeating. A Host calls it where Pad.Forget is called, for the
// same reason: the guest can no longer be holding anything.
func (repeat *Repeat) Forget() {
	repeat.code, repeat.held, repeat.due = 0, false, 0
}

func (repeat *Repeat) delay() time.Duration {
	if repeat.Delay > 0 {
		return repeat.Delay
	}
	return HandsetRepeatDelay
}

func (repeat *Repeat) interval() time.Duration {
	if repeat.Interval > 0 {
		return repeat.Interval
	}
	return HandsetRepeatInterval
}
