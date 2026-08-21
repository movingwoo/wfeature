// Package keypad turns a Host's key holds into the pad a handset had.
//
// A phone's direction pad is one control: a thumb rolls off one direction and
// onto the next, and it cannot hold two at once. A browser's keyboard is not —
// ten fingers hold two arrow keys at a time and let go of them in whichever
// order they please — and the difference is not cosmetic, because these titles
// keep a single direction and drop it the moment a pad key goes up. Delivering
// what the keyboard did, press left, press up, release left, leaves a
// character standing still with up still held.
//
// So a Host's key events pass through a Pad, which reports the pad rather than
// the keyboard. It belongs to no platform: the WIPI and MIDP runtimes number
// their keys differently and each supplies its own IsPad, while the rule they
// share is written once here.
package keypad

// Event is one key event to deliver to the guest.
type Event struct {
	Pressed bool
	Code    int32
}

// Pad tracks what the Host says is down and answers what the guest should be
// told. The zero value treats no key as a pad key, which delivers every event
// unchanged; a Host that wants the pad rule sets IsPad.
//
// A Pad belongs to one session and is used from the goroutine that drives it,
// which is what every Host here does because guest code is not re-entrant.
type Pad struct {
	// IsPad reports whether a key code is part of the direction pad.
	IsPad func(code int32) bool

	// held is the pad keys the Host says are down, oldest first, and
	// underThumb is the one the guest has been given.
	held       []int32
	underThumb int32
	pressed    bool
}

// Key answers the events to deliver for one Host key event.
//
// Everything that is not the pad passes straight through: those keys are
// actions, and an action delivered twice — or withheld — is a bug of its own.
// For the pad, three things can happen:
//
//   - A press is delivered and becomes the direction under the thumb.
//   - A release of a direction the guest has already moved on from delivers
//     **nothing**. The pad is still reporting a direction, so as far as the
//     guest is concerned nothing has changed, and telling it would stop a
//     character who is still holding one.
//   - A release of the direction in use, with another still held, delivers a
//     press for that one: the thumb has rolled onto it. A release first would
//     stop the character for as long as it takes to start again, and this is
//     the only event here a player did not make — one per state change, which
//     is nowhere near the cadence a title reads as a double tap.
//
// When the last pad key comes up, the release names the direction the guest
// was actually given, which is not always the key the Host is reporting.
func (pad *Pad) Key(pressed bool, code int32) []Event {
	if pad.IsPad == nil || !pad.IsPad(code) {
		return []Event{{Pressed: pressed, Code: code}}
	}
	if pressed {
		pad.hold(code)
		pad.underThumb, pad.pressed = code, true
		return []Event{{Pressed: true, Code: code}}
	}
	pad.drop(code)
	if len(pad.held) == 0 {
		released := code
		if pad.pressed {
			released = pad.underThumb
		}
		pad.underThumb, pad.pressed = 0, false
		return []Event{{Pressed: false, Code: released}}
	}
	if pad.pressed && code != pad.underThumb {
		return nil
	}
	pad.underThumb, pad.pressed = pad.held[len(pad.held)-1], true
	return []Event{{Pressed: true, Code: pad.underThumb}}
}

// Forget drops what is held. A Host calls it when the guest can no longer be
// holding anything — a game that ended, a session that was resumed.
func (pad *Pad) Forget() {
	pad.held = pad.held[:0]
	pad.underThumb, pad.pressed = 0, false
}

// hold records a pad key as down, most recent last. A key already down is not
// recorded twice: a Host that repeats a held key sends exactly that, and the
// pad is a set rather than a tally.
func (pad *Pad) hold(code int32) {
	for _, key := range pad.held {
		if key == code {
			return
		}
	}
	pad.held = append(pad.held, code)
}

// drop records a pad key as up, keeping the order the rest went down in.
func (pad *Pad) drop(code int32) {
	for index, key := range pad.held {
		if key == code {
			pad.held = append(pad.held[:index], pad.held[index+1:]...)
			return
		}
	}
}
