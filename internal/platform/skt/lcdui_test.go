package skt

import (
	"bytes"
	_ "embed"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

//go:embed testdata/ui.jar
var uiJAR []byte

func TestHighLevelScreensNavigateAndReportCommands(t *testing.T) {
	runtime := startUIFixture(t)

	// The List starts on its first element and moving down selects the next.
	pressKey(t, runtime, KeyCodeDown)
	pressKey(t, runtime, KeyCodeFire)
	if index := invokeFixtureInt(t, runtime, "UIMIDlet", "menuSelection"); index != 1 {
		t.Fatalf("menuSelection() = %d, want the second element", index)
	}
	if text := fixtureString(t, runtime, "UIMIDlet", "menuSelectionText"); text != "이어하기" {
		t.Fatalf("menuSelectionText() = %q", text)
	}
	// An IMPLICIT List reports the selection through the command listener.
	if count := invokeFixtureInt(t, runtime, "UIMIDlet", "commandCount"); count != 1 {
		t.Fatalf("commandCount() after select = %d, want 1", count)
	}
	if label := fixtureString(t, runtime, "UIMIDlet", "lastCommand"); label != "Select" {
		t.Fatalf("lastCommand() = %q, want List.SELECT_COMMAND", label)
	}

	// The first soft key runs the highest-priority command.
	pressKey(t, runtime, KeyCodeSoft1)
	if label := fixtureString(t, runtime, "UIMIDlet", "lastCommand"); label != "확인" {
		t.Fatalf("lastCommand() after soft 1 = %q, want the OK command", label)
	}
	// With exactly two commands the second soft key runs the second.
	pressKey(t, runtime, KeyCodeSoft2)
	if label := fixtureString(t, runtime, "UIMIDlet", "lastCommand"); label != "뒤로" {
		t.Fatalf("lastCommand() after soft 2 = %q, want the BACK command", label)
	}
}

func TestThirdCommandIsReachableThroughTheMenu(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "addThirdCommand")

	// Three commands do not fit two soft keys, so the second key opens the
	// menu instead of firing anything.
	before := invokeFixtureInt(t, runtime, "UIMIDlet", "commandCount")
	pressKey(t, runtime, KeyCodeSoft2)
	if count := invokeFixtureInt(t, runtime, "UIMIDlet", "commandCount"); count != before {
		t.Fatalf("opening the menu fired a command (%d -> %d)", before, count)
	}
	pressKey(t, runtime, KeyCodeDown)
	pressKey(t, runtime, KeyCodeDown)
	pressKey(t, runtime, KeyCodeFire)
	if label := fixtureString(t, runtime, "UIMIDlet", "lastCommand"); label != "도움말" {
		t.Fatalf("lastCommand() from the menu = %q, want the third command", label)
	}
}

func TestFormItemsSelectAndReportStateChanges(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showForm")

	// The StringItem has no commands, so the cursor starts past it on the
	// ChoiceGroup. Firing toggles its first element.
	pressKey(t, runtime, KeyCodeDown)
	pressKey(t, runtime, KeyCodeFire)
	if flags := invokeFixtureInt(t, runtime, "UIMIDlet", "groupFlags"); flags != 1 {
		t.Fatalf("groupFlags() = %d, want the first element selected", flags)
	}
	pressKey(t, runtime, KeyCodeRight)
	pressKey(t, runtime, KeyCodeFire)
	if flags := invokeFixtureInt(t, runtime, "UIMIDlet", "groupFlags"); flags != 3 {
		t.Fatalf("groupFlags() = %d, want both elements selected", flags)
	}
	if changes := invokeFixtureInt(t, runtime, "UIMIDlet", "itemChanges"); changes != 2 {
		t.Fatalf("itemChanges() = %d, want one per toggle", changes)
	}
	// Firing again clears the element rather than latching it.
	pressKey(t, runtime, KeyCodeFire)
	if flags := invokeFixtureInt(t, runtime, "UIMIDlet", "groupFlags"); flags != 1 {
		t.Fatalf("groupFlags() after the second toggle = %d", flags)
	}
}

func TestTextAlertAndCommandStateRoundTrip(t *testing.T) {
	runtime := startUIFixture(t)

	// "abc" -> insert "XY" at 1 -> "aXYbc" -> delete the first character.
	if state := fixtureString(t, runtime, "UIMIDlet", "textState"); state != "XYbc|4|16|홍길동" {
		t.Fatalf("textState() = %q", state)
	}
	if state := fixtureString(t, runtime, "UIMIDlet", "alertState"); state != "저장했습니다|-2|info" {
		t.Fatalf("alertState() = %q", state)
	}
	if state := fixtureString(t, runtime, "UIMIDlet", "commandState"); state != "확인|4|1" {
		t.Fatalf("commandState() = %q", state)
	}
}

func TestScreensPresentAFrameAndGameCanvasFlushes(t *testing.T) {
	framebuffer := newTestFramebuffer(t, 32, 24)
	runtime := startUIFixtureWith(t, framebuffer)

	// Becoming current is what paints a Screen: there is no application
	// paint() to call, so nothing else would ever put it on the display.
	frame, generation := framebuffer.Snapshot()
	if generation == 0 || len(frame.RGBA) == 0 {
		t.Fatal("List did not present a frame when it became current")
	}
	listFrame := append([]byte(nil), frame.RGBA...)

	invokeFixtureVoid(t, runtime, "UIMIDlet", "showForm")
	formFrame, _ := framebuffer.Snapshot()
	if bytes.Equal(formFrame.RGBA, listFrame) {
		t.Fatal("switching to the Form presented the same pixels as the List")
	}

	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	invokeFixtureVoid(t, runtime, "UIMIDlet", "drawBuffer")
	bufferFrame, _ := framebuffer.Snapshot()
	pixels := bufferFrame.RGBA
	if len(pixels) < 4 || pixels[0] != 0xff || pixels[1] != 0x00 || pixels[2] != 0x00 {
		t.Fatalf("GameCanvas flush did not reach the framebuffer: first pixel %v", pixels[:min(4, len(pixels))])
	}
}

func TestGameCanvasLatchesKeyStates(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")

	if states := invokeFixtureInt(t, runtime, "UIMIDlet", "bufferKeyStates"); states != 0 {
		t.Fatalf("bufferKeyStates() before input = %d, want 0", states)
	}
	if err := runtime.SendKey(KeyPressed, KeyCodeFire); err != nil {
		t.Fatalf("SendKey(press) error = %v", err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	if states := invokeFixtureInt(t, runtime, "UIMIDlet", "bufferKeyStates"); states != 1<<8 {
		t.Fatalf("bufferKeyStates() while pressed = %d, want FIRE_PRESSED", states)
	}
	if err := runtime.SendKey(KeyReleased, KeyCodeFire); err != nil {
		t.Fatalf("SendKey(release) error = %v", err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	if states := invokeFixtureInt(t, runtime, "UIMIDlet", "bufferKeyStates"); states != 0 {
		t.Fatalf("bufferKeyStates() after release = %d, want 0", states)
	}
}

func TestWrapTextSplitsOnSpacesAndInsideLongWords(t *testing.T) {
	font := &fontData{fontKey: fontKey{}, scale: 1, height: 10, baseline: 8}
	lines := wrapText(font, "aa bb cc", font.textWidth([]rune("aa bb")))
	if len(lines) != 2 || lines[0] != "aa bb" || lines[1] != "cc" {
		t.Fatalf("wrapText() = %q, want a split on the space", lines)
	}
	long := wrapText(font, "abcdef", font.textWidth([]rune("abc")))
	if len(long) != 2 || long[0] != "abc" || long[1] != "def" {
		t.Fatalf("wrapText() on a long word = %q", long)
	}
}

func startUIFixture(t *testing.T) *Runtime {
	t.Helper()
	return startUIFixtureWith(t, newTestFramebuffer(t, 32, 24))
}

func startUIFixtureWith(t *testing.T, framebuffer *backend.MemoryFramebuffer) *Runtime {
	t.Helper()
	archive, err := Open(uiJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, Options{Framebuffer: framebuffer})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	return runtime
}

func pressKey(t *testing.T, runtime *Runtime, keyCode int32) {
	t.Helper()
	if err := runtime.SendKey(KeyPressed, keyCode); err != nil {
		t.Fatalf("SendKey(%d) error = %v", keyCode, err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
}

func invokeFixtureVoid(t *testing.T, runtime *Runtime, className, method string) {
	t.Helper()
	if _, err := runtime.VM.InvokeStatic(className, method, "()V"); err != nil {
		t.Fatalf("%s.%s() error = %v", className, method, err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
}

func TestTextBoxTypesThroughTheKeypad(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showTextBox")

	// The box starts holding "abc"; the keypad appends to it. Two presses of
	// 2 inside the multi-tap window give one "b", not two characters.
	pressKey(t, runtime, '2')
	pressKey(t, runtime, '2')
	pressKey(t, runtime, '#') // backspace
	pressKey(t, runtime, '5') // 'j'
	if text := fixtureString(t, runtime, "UIMIDlet", "typedText"); text != "abcj" {
		t.Fatalf("typedText() = %q, want %q", text, "abcj")
	}

	// 4 and 6 are the keys a game reads as left and right, and in a field
	// they are letters: taking them as caret moves left "ghi" and "mno"
	// untypable.
	pressKey(t, runtime, '4')
	pressKey(t, runtime, '6')
	if text := fixtureString(t, runtime, "UIMIDlet", "typedText"); text != "abcjgm" {
		t.Fatalf("typedText() = %q, want %q", text, "abcjgm")
	}
}
