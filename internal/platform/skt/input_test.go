package skt

import "testing"

// The names are the ones both WIPI platforms answer to, so a scripted run reads
// the same whichever vendor it drives. CLR is the one that was missing: a title
// of this era draws "BACK:CLR" on every screen that can be left, and a run that
// cannot send it cannot get out of a settings screen — which is where that
// title writes its settings.
func TestKeyCodeByNameCoversTheNamesTheCommandLineUses(t *testing.T) {
	for name, want := range map[string]int32{
		"up": KeyCodeUp, "down": KeyCodeDown, "left": KeyCodeLeft, "right": KeyCodeRight,
		"fire": KeyCodeFire, "ok": KeyCodeFire, "FIRE": KeyCodeFire,
		"soft1": KeyCodeSoft1, "soft2": KeyCodeSoft2,
		"call": KeyCodeCall, "clear": KeyCodeClear, "clr": KeyCodeClear, "back": KeyCodeClear,
		"0": '0', "9": '9', "*": '*', "#": '#',
	} {
		got, ok := KeyCodeByName(name)
		if !ok || got != want {
			t.Errorf("KeyCodeByName(%q) = %d, %v; want %d, true", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "sideways", "10", "fire2"} {
		if _, ok := KeyCodeByName(name); ok {
			t.Errorf("KeyCodeByName(%q) resolved, want it rejected", name)
		}
	}
}

// The pad this runtime hands the shared rule is the four navigation keys and
// the digits a keypad prints them on: a local side-scroller walks on both.
func TestThePadIsTheNavigationKeysAndTheDigitsUnderThem(t *testing.T) {
	for _, code := range []int32{KeyCodeUp, KeyCodeDown, KeyCodeLeft, KeyCodeRight, '2', '4', '6', '8'} {
		if !isPadKey(code) {
			t.Errorf("%d is not counted as the pad", code)
		}
	}
	for _, code := range []int32{KeyCodeFire, KeyCodeSoft1, KeyCodeSoft2, KeyCodeClear, '1', '3', '5', '7', '9', '0', '*', '#'} {
		if isPadKey(code) {
			t.Errorf("%d is counted as the pad", code)
		}
	}
}
