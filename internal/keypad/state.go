package keypad

import (
	"fmt"
	"slices"
	"time"
)

type PadState struct {
	Version    uint32
	UsesPad    bool
	Held       []int32
	UnderThumb int32
	Pressed    bool
	DownCode   int32
	Down       bool
}

type RepeatState struct {
	Version         uint32
	Delay, Interval time.Duration
	Code            int32
	Held            bool
	Due             time.Duration
}

// CaptureState copies the ordered pad holds and the last key delivered to the
// guest. The owner must serialize input with capture and restoration.
func (pad *Pad) CaptureState() (PadState, error) {
	if pad == nil {
		return PadState{}, fmt.Errorf("keypad is nil")
	}
	saved := PadState{Version: 1, UsesPad: pad.IsPad != nil, Held: pad.held, UnderThumb: pad.underThumb, Pressed: pad.pressed, DownCode: pad.downCode, Down: pad.down}
	if _, err := RestorePad(saved, pad.IsPad); err != nil {
		return PadState{}, err
	}
	saved.Held = slices.Clone(saved.Held)
	return saved, nil
}

// RestorePad supplies the platform's key classification anew; a Go callback is
// not checkpoint data. No key event is delivered during construction.
func RestorePad(saved PadState, isPad func(int32) bool) (Pad, error) {
	if saved.Version != 1 || saved.UsesPad != (isPad != nil) || len(saved.Held) > 64 ||
		saved.Pressed != (len(saved.Held) != 0) || !saved.Pressed && saved.UnderThumb != 0 || !saved.Down && saved.DownCode != 0 {
		return Pad{}, fmt.Errorf("keypad checkpoint has invalid ownership or key counts")
	}
	seen := make(map[int32]bool, len(saved.Held))
	for _, code := range saved.Held {
		if isPad == nil || !isPad(code) || seen[code] {
			return Pad{}, fmt.Errorf("keypad checkpoint has duplicate or non-pad holds")
		}
		seen[code] = true
	}
	if saved.Pressed && !seen[saved.UnderThumb] {
		return Pad{}, fmt.Errorf("keypad checkpoint has no held key under the thumb")
	}
	return Pad{IsPad: isPad, held: slices.Clone(saved.Held), underThumb: saved.UnderThumb, pressed: saved.Pressed, downCode: saved.DownCode, down: saved.Down}, nil
}

func (repeat *Repeat) CaptureState() (RepeatState, error) {
	if repeat == nil {
		return RepeatState{}, fmt.Errorf("key repeat is nil")
	}
	saved := RepeatState{Version: 1, Delay: repeat.Delay, Interval: repeat.Interval, Code: repeat.code, Held: repeat.held, Due: repeat.due}
	_, err := RestoreRepeat(saved)
	return saved, err
}

// RestoreRepeat retains the remaining guest duration, independent of any Host
// clock. The shared session separately restores its last-tick clock anchor.
func RestoreRepeat(saved RepeatState) (Repeat, error) {
	repeat := Repeat{Delay: saved.Delay, Interval: saved.Interval, code: saved.Code, held: saved.Held, due: saved.Due}
	if saved.Version != 1 || saved.Delay < 0 || saved.Interval < 0 ||
		!saved.Held && (saved.Code != 0 || saved.Due != 0) ||
		saved.Held && (saved.Due <= 0 || saved.Due > max(repeat.delay(), repeat.interval())) {
		return Repeat{}, fmt.Errorf("key repeat checkpoint has an invalid phase")
	}
	return repeat, nil
}
