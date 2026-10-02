package keypad

import (
	"reflect"
	"testing"
	"time"
)

func TestKeypadStateContinuesRolloverAndRepeatPhase(t *testing.T) {
	isPad := func(code int32) bool { return code >= 1 && code <= 4 }
	pad := Pad{IsPad: isPad}
	pad.Key(true, 1)
	pad.Key(true, 2)
	repeat := Repeat{}
	repeat.Holding(pad.Held())
	repeat.Due(450 * time.Millisecond)
	savedPad, err := pad.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	savedRepeat, err := repeat.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	freshPad, err := RestorePad(savedPad, isPad)
	if err != nil {
		t.Fatal(err)
	}
	freshRepeat, err := RestoreRepeat(savedRepeat)
	if err != nil {
		t.Fatal(err)
	}
	for _, elapsed := range []time.Duration{149 * time.Millisecond, time.Millisecond, 249 * time.Millisecond, 2 * time.Millisecond} {
		want, due := repeat.Due(elapsed)
		got, ready := freshRepeat.Due(elapsed)
		if got != want || ready != due {
			t.Fatal("restored repeat changed its phase")
		}
	}
	for _, event := range []Event{{false, 2}, {true, 3}, {false, 1}, {false, 3}} {
		if got, want := freshPad.Key(event.Pressed, event.Code), pad.Key(event.Pressed, event.Code); !reflect.DeepEqual(got, want) {
			t.Fatalf("restored rollover differs: %v, want %v", got, want)
		}
	}
	if !reflect.DeepEqual(savedPad.Held, []int32{1, 2}) {
		t.Fatal("restored pad changed the captured key list")
	}
}

func TestKeypadStateRejectsMalformedRecords(t *testing.T) {
	for _, saved := range []PadState{
		{Version: 2}, {Version: 1, Held: []int32{1}}, {Version: 1, DownCode: 1},
		{Version: 1, UsesPad: true, Held: []int32{1, 1}, Pressed: true, UnderThumb: 1},
		{Version: 1, UsesPad: true, Held: []int32{1}, Pressed: true, UnderThumb: 2},
	} {
		var isPad func(int32) bool
		if saved.UsesPad {
			isPad = func(int32) bool { return true }
		}
		if _, err := RestorePad(saved, isPad); err == nil {
			t.Fatal("malformed pad accepted")
		}
	}
	for _, saved := range []RepeatState{
		{Version: 2}, {Version: 1, Due: 1}, {Version: 1, Held: true},
		{Version: 1, Delay: -1}, {Version: 1, Interval: -1},
		{Version: 1, Held: true, Due: time.Hour},
	} {
		if _, err := RestoreRepeat(saved); err == nil {
			t.Fatal("malformed repeat accepted")
		}
	}
}
