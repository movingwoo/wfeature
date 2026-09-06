package backend

import (
	"sync"
	"time"
)

// The handset's motor.
//
// Every platform here already answers the guest's vibration calls, and none of
// them did anything with the answer: one counted the request as a diagnostic,
// one returned success without reading its arguments, and one set a flag that
// was written in two places and read nowhere. The browser has
// `navigator.vibrate` and there is an Android build, so what was missing was
// not the ability to vibrate but a boundary to carry the request across.
//
// **The core reports; the Host decides.** What a platform runtime owns is the
// state the guest asked for — how hard and for how long — and nothing else. It
// does not know whether there is a motor, whether the person turned vibration
// off, or whether this Host can vibrate at all, and it must not: a core that
// took the action itself would have to be told about every Host, and a Host
// that could not act would have no way to say so. A platform that does not
// implement vibration reports nothing at all, and a Host that reads nothing
// does nothing — which is what makes "not wired up yet" and "the guest is not
// asking" the same silence, correctly.
//
// The contract the guests are written against is the WIPI one, and it is worth
// stating because two of its rules are not what a reader would guess:
//
//   - `level` is a percentage from 0 to 100, not an index. 0 is off; 100 is the
//     strongest the hardware has; the values between are mapped onto whatever
//     discrete steps a handset actually had. It is *not* the same units as the
//     `VIBRATORLEVEL` handset property, which is a count of those steps — a
//     title reads "3" there and still passes 1..100.
//   - a duration of zero with a level above zero means **until stopped**, not
//     "do nothing". A title that asks for a continuous buzz and gets a no-op is
//     a title whose vibration silently never happens.
//
// A request made while one is running replaces it and restarts the clock, which
// is also the specified behavior.

// vibrationMaxLevel is the top of the guest's scale.
const vibrationMaxLevel = 100

// Vibration is what the guest has asked the motor to do. It is a request
// rather than an event: a Host that polls it every tick reads the same request
// until the guest makes another one.
type Vibration struct {
	// Request counts the requests the guest has made, starting at one. It is
	// what lets a Host act once per request while reading state every tick: a
	// number it has already seen is the request it already acted on, and a
	// number that moved is a new one — including a repeat of the same level and
	// duration, which the guest meant as a second buzz.
	//
	// Zero means the guest has never asked for anything.
	Request uint64
	// Level is the strength the guest asked for, 0 to 100. Zero means the motor
	// should be off, which is how the guest turns it off.
	Level int
	// Duration is the time the guest asked for. Zero with a Level above zero
	// means until the guest stops it; see Indefinite.
	Duration time.Duration
	// Remaining is how much of Duration is left. It is zero for a request that
	// has run out and zero for an indefinite one, which Duration tells apart —
	// a Host that only ever acts on a new Request never has to look at it, and
	// one that has to re-arm a motor it cannot hand a duration to does.
	Remaining time.Duration
}

// Indefinite reports a request the guest expects to run until it stops it.
func (v Vibration) Indefinite() bool { return v.Level > 0 && v.Duration == 0 }

// Active reports whether the motor should be running now.
func (v Vibration) Active() bool { return v.Level > 0 && (v.Indefinite() || v.Remaining > 0) }

// Vibrator records what a guest has asked for. A platform runtime owns one and
// a Host reads it; nothing here drives anything.
//
// Its zero value is usable and reports that nothing has been asked for.
type Vibrator struct {
	mutex sync.Mutex
	// now is the clock, replaceable so a test can watch a request run out
	// without waiting for it.
	//
	// It is the wall clock rather than the guest's. A vibration is a thing a
	// person feels in the room, so a game running at a quarter speed still
	// asked for the milliseconds it asked for; scaling them would make the
	// speed control silently change how the handset feels.
	now      func() time.Time
	request  uint64
	level    int
	duration time.Duration
	deadline time.Time
}

// Vibrate records one request: a level from 0 to 100 and a duration in
// milliseconds, which is the guest's own contract. A level of zero is how a
// guest turns the motor off, and it is recorded as a request like any other so
// that a Host reading the counter learns about it.
//
// Values outside the range are clamped rather than refused. They arrive from
// game code this project does not control, and a title that passes 150 means
// "as hard as you can" — refusing the call would drop a vibration the guest
// asked for, which is a worse answer than the one the hardware would have
// given.
func (v *Vibrator) Vibrate(level, milliseconds int) {
	if v == nil {
		return
	}
	if level < 0 {
		level = 0
	}
	if level > vibrationMaxLevel {
		level = vibrationMaxLevel
	}
	if milliseconds < 0 {
		milliseconds = 0
	}
	duration := time.Duration(milliseconds) * time.Millisecond

	v.mutex.Lock()
	defer v.mutex.Unlock()
	v.request++
	v.level = level
	v.duration = duration
	// A request that names a time gets a deadline; an indefinite one does not,
	// and a level of zero is not running at all.
	if level > 0 && duration > 0 {
		v.deadline = v.clock().Add(duration)
	} else {
		v.deadline = time.Time{}
	}
}

// Stop records that the guest turned the motor off. It is the same request a
// level of zero makes; it exists because that is the call the guests make.
func (v *Vibrator) Stop() { v.Vibrate(0, 0) }

// State reports the current request. Level and Duration are what the guest
// asked for and stay that way after the request has run out, because that is
// what it asked for; Remaining is what says whether it is still running.
func (v *Vibrator) State() Vibration {
	if v == nil {
		return Vibration{}
	}
	v.mutex.Lock()
	defer v.mutex.Unlock()
	state := Vibration{Request: v.request, Level: v.level, Duration: v.duration}
	if !v.deadline.IsZero() {
		if remaining := v.deadline.Sub(v.clock()); remaining > 0 {
			state.Remaining = remaining
		}
	}
	return state
}

// SetClock replaces the clock this vibrator measures with. It is for tests;
// nothing in a running emulator calls it.
func (v *Vibrator) SetClock(now func() time.Time) {
	v.mutex.Lock()
	defer v.mutex.Unlock()
	v.now = now
}

// clock is the vibrator's time source. The caller holds the mutex.
func (v *Vibrator) clock() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}
