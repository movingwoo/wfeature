package ktf

import "testing"

func TestKeyCodeByNameCoversTheNamesRoutesAndTheCommandLineUse(t *testing.T) {
	for name, want := range map[string]int32{
		"up": KeyUp, "down": KeyDown, "left": KeyLeft, "right": KeyRight,
		"fire": KeyFire, "ok": KeyFire, "FIRE": KeyFire,
		"soft1": KeyLeftSoft, "soft2": KeyRightSoft, "clear": KeyClear,
		"call": KeyCall, "hangup": KeyHangup,
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
