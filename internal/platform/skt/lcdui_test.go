package skt

import (
	"bytes"
	_ "embed"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
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

// An anchor names where a point sits against what is drawn, and a title may
// name only one of its two halves. The handset filled the other in; MIDP's own
// rule refuses it, and a WIPI title that draws its title screen with LEFT
// alone would end on its first image.
func TestAnchorFillsInTheHalfATitleLeavesOut(t *testing.T) {
	for _, probe := range []struct {
		name                 string
		anchor               int32
		image                bool
		horizontal, vertical int32
	}{
		{name: "nothing at all is left and top", anchor: 0, horizontal: anchorLeft, vertical: anchorTop},
		{name: "horizontal alone keeps top", anchor: anchorLeft, horizontal: anchorLeft, vertical: anchorTop},
		{name: "a centred image keeps top", anchor: anchorHCenter, image: true, horizontal: anchorHCenter, vertical: anchorTop},
		{name: "vertical alone keeps left", anchor: anchorBottom, horizontal: anchorLeft, vertical: anchorBottom},
		{name: "both halves are kept", anchor: anchorRight | anchorBottom, horizontal: anchorRight, vertical: anchorBottom},
	} {
		t.Run(probe.name, func(t *testing.T) {
			horizontal, vertical, err := normalizeAnchor(probe.anchor, probe.image)
			if err != nil {
				t.Fatalf("normalizeAnchor(%d) error = %v", probe.anchor, err)
			}
			if horizontal != probe.horizontal || vertical != probe.vertical {
				t.Fatalf("normalizeAnchor(%d) = %d, %d, want %d, %d",
					probe.anchor, horizontal, vertical, probe.horizontal, probe.vertical)
			}
		})
	}

	// Two bits from one group is still a title asking for two places at once,
	// and so is a bit that belongs to neither group.
	for _, anchor := range []int32{anchorLeft | anchorRight, anchorTop | anchorBottom, 1 << 20} {
		if _, _, err := normalizeAnchor(anchor, false); err == nil {
			t.Fatalf("normalizeAnchor(%d) was accepted", anchor)
		}
	}
	// BASELINE is a text anchor and VCENTER an image one, and neither is the
	// other's.
	if _, _, err := normalizeAnchor(anchorBaseline, true); err == nil {
		t.Fatal("an image accepted the text baseline anchor")
	}
	if _, _, err := normalizeAnchor(anchorVCenter, false); err == nil {
		t.Fatal("text accepted the image centre anchor")
	}
}

// An IMPLICIT List selects its focused row when a command is initiated, so a
// title that reads getSelectedIndex in commandAction acts on the row the
// player is on, whichever soft key or menu entry fired the command.
func TestImplicitListCommandsSelectTheFocusedRow(t *testing.T) {
	runtime := startUIFixture(t)

	pressKey(t, runtime, KeyCodeDown)
	pressKey(t, runtime, KeyCodeDown)
	pressKey(t, runtime, KeyCodeSoft1)
	if label := fixtureString(t, runtime, "UIMIDlet", "lastCommand"); label != "확인" {
		t.Fatalf("lastCommand() = %q, want the OK command", label)
	}
	if index := invokeFixtureInt(t, runtime, "UIMIDlet", "menuSelection"); index != 2 {
		t.Fatalf("menuSelection() after soft 1 = %d, want the focused row 2", index)
	}

	pressKey(t, runtime, KeyCodeUp)
	pressKey(t, runtime, KeyCodeSoft2)
	if index := invokeFixtureInt(t, runtime, "UIMIDlet", "menuSelection"); index != 1 {
		t.Fatalf("menuSelection() after soft 2 = %d, want the focused row 1", index)
	}

	// A row the title selects itself is the one highlighted, so the next
	// command keeps it rather than reverting to where the focus was.
	menu := fixtureStatic(t, runtime, "menu", "Ljavax/microedition/lcdui/List;")
	invokeObjectVoid(t, runtime, menu, "setSelectedIndex", "(IZ)V", jvm.IntValue(2), jvm.IntValue(1))
	pressKey(t, runtime, KeyCodeSoft1)
	if index := invokeFixtureInt(t, runtime, "UIMIDlet", "menuSelection"); index != 2 {
		t.Fatalf("menuSelection() after setSelectedIndex(2) and soft 1 = %d, want 2", index)
	}

	// Through the command menu as well.
	pressKey(t, runtime, KeyCodeUp)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "addThirdCommand")
	pressKey(t, runtime, KeyCodeUp)
	pressKey(t, runtime, KeyCodeSoft2)
	pressKey(t, runtime, KeyCodeDown)
	pressKey(t, runtime, KeyCodeFire)
	if label := fixtureString(t, runtime, "UIMIDlet", "lastCommand"); label != "뒤로" {
		t.Fatalf("lastCommand() from the menu = %q, want the BACK command", label)
	}
	if index := invokeFixtureInt(t, runtime, "UIMIDlet", "menuSelection"); index != 0 {
		t.Fatalf("menuSelection() after the menu = %d, want the focused row 0", index)
	}
}

// Deleting rows a List's focus is on moves the focus back inside the List, so
// FIRE acts on a row that exists and a quicksave taken then can be restored.
func TestListFocusStaysInsideAShrunkList(t *testing.T) {
	runtime := startUIFixture(t)
	menu := fixtureStatic(t, runtime, "menu", "Ljavax/microedition/lcdui/List;")
	content := runtime.screenState(menu, screenList)

	pressKey(t, runtime, KeyCodeDown)
	pressKey(t, runtime, KeyCodeDown)
	invokeObjectVoid(t, runtime, menu, "delete", "(I)V", jvm.IntValue(2))
	invokeObjectVoid(t, runtime, menu, "delete", "(I)V", jvm.IntValue(1))
	if content.selection < 0 || content.selection >= len(content.choice.elements) {
		t.Fatalf("List focus = %d over %d rows", content.selection, len(content.choice.elements))
	}
	pressKeySafely(t, runtime, KeyCodeFire)
	if index := invokeFixtureInt(t, runtime, "UIMIDlet", "menuSelection"); index != 0 {
		t.Fatalf("menuSelection() = %d, want the one remaining row", index)
	}

	// A MULTIPLE List toggles the row under the focus, which has to exist.
	content.choice.kind = choiceMultiple
	for _, text := range []string{"b", "c", "d"} {
		invokeObjectInt(t, runtime, menu, "append", "(Ljava/lang/String;Ljavax/microedition/lcdui/Image;)I",
			jvm.ReferenceValue(runtime.VM.NewString(text)), jvm.ReferenceValue(nil))
	}
	for range 3 {
		pressKey(t, runtime, KeyCodeDown)
	}
	invokeObjectVoid(t, runtime, menu, "delete", "(I)V", jvm.IntValue(3))
	invokeObjectVoid(t, runtime, menu, "delete", "(I)V", jvm.IntValue(2))
	pressKeySafely(t, runtime, KeyCodeFire)

	// A focus restored at -1 over a List that was empty then, and has rows
	// now, still lands on a row.
	invokeObjectVoid(t, runtime, menu, "deleteAll", "()V")
	content.choice.kind = choiceImplicit
	content.selection = -1
	invokeObjectInt(t, runtime, menu, "append", "(Ljava/lang/String;Ljavax/microedition/lcdui/Image;)I",
		jvm.ReferenceValue(runtime.VM.NewString("e")), jvm.ReferenceValue(nil))
	if content.selection != 0 {
		t.Fatalf("List focus after a row arrived = %d, want 0", content.selection)
	}
	content.selection = -1
	pressKeySafely(t, runtime, KeyCodeFire)
	if index := invokeFixtureInt(t, runtime, "UIMIDlet", "menuSelection"); index != 0 {
		t.Fatalf("menuSelection() after a restored -1 focus = %d, want 0", index)
	}
}

// RIGHT on an empty ChoiceGroup leaves its element focus at a real index, so
// the FIRE that follows the title appending options toggles one of them.
func TestChoiceGroupFocusSurvivesBeingEmpty(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showForm")
	form := fixtureStatic(t, runtime, "form", "Ljavax/microedition/lcdui/Form;")
	group := fixtureStatic(t, runtime, "group", "Ljavax/microedition/lcdui/ChoiceGroup;")
	content := runtime.screenState(form, screenForm)

	pressKey(t, runtime, KeyCodeDown)
	invokeObjectVoid(t, runtime, group, "deleteAll", "()V")
	pressKey(t, runtime, KeyCodeRight)
	if content.subSelection < 0 {
		t.Fatalf("element focus on an empty ChoiceGroup = %d", content.subSelection)
	}
	invokeObjectInt(t, runtime, group, "append", "(Ljava/lang/String;Ljavax/microedition/lcdui/Image;)I",
		jvm.ReferenceValue(runtime.VM.NewString("a")), jvm.ReferenceValue(nil))
	pressKeySafely(t, runtime, KeyCodeFire)
	if flags := invokeFixtureInt(t, runtime, "UIMIDlet", "groupFlags"); flags != 1 {
		t.Fatalf("groupFlags() = %d, want the appended element toggled", flags)
	}

	// A focus of -1 restored from a quicksave taken while it was empty.
	content.subSelection = -1
	pressKeySafely(t, runtime, KeyCodeFire)
	if flags := invokeFixtureInt(t, runtime, "UIMIDlet", "groupFlags"); flags != 0 {
		t.Fatalf("groupFlags() after a restored -1 focus = %d, want the element toggled back", flags)
	}

	// The restored -1 does not outlive the group gaining an element, so the
	// next quicksave holds a focus its restore accepts.
	invokeObjectVoid(t, runtime, group, "deleteAll", "()V")
	content.subSelection = -1
	invokeObjectInt(t, runtime, group, "append", "(Ljava/lang/String;Ljavax/microedition/lcdui/Image;)I",
		jvm.ReferenceValue(runtime.VM.NewString("b")), jvm.ReferenceValue(nil))
	if content.subSelection != 0 {
		t.Fatalf("element focus after the group gained an element = %d, want 0", content.subSelection)
	}
}
