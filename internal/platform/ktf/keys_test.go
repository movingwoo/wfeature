package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

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

// The pairs `Display.getGameAction` and `Display.getKeyCode` are the two
// directions of. They are written out here rather than read from the runtime's
// own table, because what is asked below is whether that table answers both
// questions — not whether it agrees with itself.
var gameActionPairs = []struct{ key, action int32 }{
	{KeyUp, 1},
	{KeyLeft, 2},
	{KeyRight, 5},
	{KeyDown, 6},
	{KeyFire, 8},
	{KeyLeftSoft, 90},
	{KeyRightSoft, 91},
	{KeyThirdSoft, 92},
	{KeyVolumeUp, 96},
	{KeyVolumeDown, 97},
	{KeyClear, 99},
}

// A game action is something a title indexes by, so it is never a key code.
//
// A title that handles the pad through actions calls `getGameAction` from its
// own `keyNotify` and uses the answer as a subscript into a table of its own.
// A key with no action reports itself, and every key code here is negative, so
// a key the forward direction did not know took such a title below the start
// of its own array. It is the reverse direction that says what the soft keys
// and the volume pair mean, and both directions have to say it.
func TestDisplayGameActionAnswersAnIndexForEveryKeyItsReverseNames(t *testing.T) {
	for _, pair := range gameActionPairs {
		action := gameActionAnswer(t, "getGameAction", runtimeDisplayGameAction, pair.key)
		if action != pair.action {
			t.Errorf("getGameAction(%d) = %d, want %d", pair.key, action, pair.action)
		}
		if action < 0 {
			t.Errorf("getGameAction(%d) = %d, which is not something a title can index by", pair.key, action)
		}
		key := gameActionAnswer(t, "getKeyCode", runtimeDisplayKeyCode, pair.action)
		if key != pair.key {
			t.Errorf("getKeyCode(%d) = %d, want %d", pair.action, key, pair.key)
		}
	}
}

// A key with no action of its own still reaches the title, which is what
// leaves the keypad's characters usable as the characters they are.
func TestDisplayGameActionReportsAKeyWithNoActionAsItself(t *testing.T) {
	for _, key := range []int32{'0', '5', '9', '*', '#'} {
		if action := gameActionAnswer(t, "getGameAction", runtimeDisplayGameAction, key); action != key {
			t.Errorf("getGameAction(%d) = %d, want the key itself", key, action)
		}
	}
	if key := gameActionAnswer(t, "getKeyCode", runtimeDisplayKeyCode, 3); key != 0 {
		t.Errorf("getKeyCode(3) = %d, want 0 for an action no key stands for", key)
	}
}

// gameActionAnswer runs one of the two translations on a bare argument list.
// Neither reads the runtime or the machine, so neither is needed to ask.
func gameActionAnswer(t *testing.T, name string,
	translate func(*initializationRuntime, *jvm.VM, []jvm.Value) (jvm.Value, error), argument int32) int32 {
	t.Helper()
	answer, err := translate(nil, nil, []jvm.Value{jvm.IntValue(argument)})
	if err != nil {
		t.Fatalf("%s(%d): %v", name, argument, err)
	}
	value, err := answer.Int32()
	if err != nil {
		t.Fatalf("%s(%d) answered %v: %v", name, argument, answer, err)
	}
	return value
}
