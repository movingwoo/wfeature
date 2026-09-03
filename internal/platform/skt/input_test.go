package skt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

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

// A Jlet's Card is handed the WIPI key code, not this vendor's MIDP one. The
// numbers are read off two local titles that disagree about everything else:
// one keys a per-handset resource table on them, the other switches on them
// directly. Both sets are the same, and both are the EventQueue game keys.
func TestAJletReadsTheWIPIKeyCodes(t *testing.T) {
	for device, want := range map[int32]int32{
		KeyCodeUp:    wipiKeyUp,
		KeyCodeLeft:  wipiKeyLeft,
		KeyCodeRight: wipiKeyRight,
		KeyCodeDown:  wipiKeyDown,
		KeyCodeFire:  wipiKeyFire,
		KeyCodeSoft1: wipiKeySoft1,
		KeyCodeSoft2: wipiKeySoft2,
		KeyCodeClear: wipiKeyClear,
		KeyCodeCall:  wipiKeySend,
	} {
		if got := wipiKeyOfDevice(device); got != want {
			t.Errorf("wipiKeyOfDevice(%d) = %d, want %d", device, got, want)
		}
	}
	// An ITU key is its ASCII value on both sides, so it passes through.
	for _, code := range []int32{'0', '5', '9', '*', '#'} {
		if got := wipiKeyOfDevice(code); got != code {
			t.Errorf("wipiKeyOfDevice(%d) = %d, want it unchanged", code, got)
		}
	}
}

// getGameAction and getKeyCode answer in the same vocabulary the Card is
// handed, so a title that asks what it just received gets an answer it can
// compare against what it will receive next.
func TestTheGameActionsRoundTripThroughTheWIPIKeyCodes(t *testing.T) {
	for _, device := range []int32{KeyCodeUp, KeyCodeLeft, KeyCodeRight, KeyCodeDown, KeyCodeFire} {
		key := wipiKeyOfDevice(device)
		action, err := wipiGameAction(nil, []jvm.Value{jvm.IntValue(key)})
		if err != nil {
			t.Fatalf("getGameAction(%d) error = %v", key, err)
		}
		value, err := action.Int32()
		if err != nil {
			t.Fatalf("getGameAction(%d) value = %v", key, err)
		}
		back, err := wipiKeyCode(nil, []jvm.Value{jvm.IntValue(value)})
		if err != nil {
			t.Fatalf("getKeyCode(%d) error = %v", value, err)
		}
		got, err := back.Int32()
		if err != nil {
			t.Fatalf("getKeyCode(%d) value = %v", value, err)
		}
		if got != key {
			t.Errorf("getKeyCode(getGameAction(%d)) = %d, want %d", key, got, key)
		}
	}
	// The digits a handset lets stand in for the pad answer the pad's action,
	// and the digits that are only digits answer nothing.
	for digit, want := range map[int32]int32{'2': wipiKeyUp, '4': wipiKeyLeft, '6': wipiKeyRight, '8': wipiKeyDown, '5': wipiKeyFire, '0': 0} {
		action, err := wipiGameAction(nil, []jvm.Value{jvm.IntValue(digit)})
		if err != nil {
			t.Fatalf("getGameAction(%q) error = %v", digit, err)
		}
		got, err := action.Int32()
		if err != nil {
			t.Fatalf("getGameAction(%q) value = %v", digit, err)
		}
		if got != want {
			t.Errorf("getGameAction(%q) = %d, want %d", digit, got, want)
		}
	}
}
