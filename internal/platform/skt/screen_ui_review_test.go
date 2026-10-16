package skt

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

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

// A title's own thread rebuilding a List or a ChoiceGroup while the player
// presses keys and the Host paints. Under -race this fails when the guest's
// edits and the Host's reads do not share a lock; without the detector it
// still fails when a stale length is used to index a shorter slice.
func TestScreenEditsFromAGuestThreadDoNotRaceTheHost(t *testing.T) {
	runtime := startUIFixture(t)
	menu := fixtureStatic(t, runtime, "menu", "Ljavax/microedition/lcdui/List;")
	group := fixtureStatic(t, runtime, "group", "Ljavax/microedition/lcdui/ChoiceGroup;")

	for _, target := range []struct {
		name   string
		object *jvm.Object
		show   string
		keys   []int32
	}{
		{name: "List", object: menu, show: "showMenu", keys: []int32{KeyCodeDown, KeyCodeDown, KeyCodeFire, KeyCodeUp}},
		{name: "ChoiceGroup", object: group, show: "showForm", keys: []int32{KeyCodeDown, KeyCodeRight, KeyCodeFire, KeyCodeLeft, KeyCodeUp}},
	} {
		invokeFixtureVoid(t, runtime, "UIMIDlet", target.show)
		var guest sync.WaitGroup
		guest.Add(1)
		failures := make(chan error, 1)
		// The title's thread calls the natives the way its bytecode does,
		// through the classes they are registered on.
		go func() {
			defer guest.Done()
			for round := 0; round < 200; round++ {
				if _, err := runtime.VM.InvokeVirtual(target.object, "deleteAll", "()V"); err != nil {
					failures <- err
					return
				}
				for row := 0; row < 3; row++ {
					if _, err := runtime.VM.InvokeVirtual(target.object, "append", "(Ljava/lang/String;Ljavax/microedition/lcdui/Image;)I",
						jvm.ReferenceValue(runtime.VM.NewString("row")), jvm.ReferenceValue(nil)); err != nil {
						failures <- err
						return
					}
				}
			}
		}()
		for round := 0; round < 200; round++ {
			pressKeySafely(t, runtime, target.keys[round%len(target.keys)])
		}
		guest.Wait()
		select {
		case err := <-failures:
			t.Fatalf("%s edit error = %v", target.name, err)
		default:
		}
	}
}

// The keypad types what a TextBox's constraint allows: digits in a NUMERIC
// box rather than the letters on the key, and nothing in an UNEDITABLE one.
func TestKeypadHonoursTextBoxConstraints(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showTextBox")
	box := fixtureStatic(t, runtime, "textBox", "Ljavax/microedition/lcdui/TextBox;")
	invokeObjectVoid(t, runtime, box, "setString", "(Ljava/lang/String;)V", jvm.ReferenceValue(runtime.VM.NewString("")))
	invokeObjectVoid(t, runtime, box, "setConstraints", "(I)V", jvm.IntValue(2))

	pressKey(t, runtime, '2')
	pressKey(t, runtime, '2')
	pressKey(t, runtime, '5')
	if text := fixtureString(t, runtime, "UIMIDlet", "typedText"); text != "225" {
		t.Fatalf("NUMERIC typedText() = %q, want %q", text, "225")
	}

	invokeObjectVoid(t, runtime, box, "setConstraints", "(I)V", jvm.IntValue(textFieldUneditable))
	pressKey(t, runtime, '7')
	pressKey(t, runtime, '#')
	if text := fixtureString(t, runtime, "UIMIDlet", "typedText"); text != "225" {
		t.Fatalf("UNEDITABLE typedText() = %q, want it unchanged", text)
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

// A GameCanvas's inherited paint blits its buffer at the Graphics origin and
// under the lock the destination's other drawing calls take.
func TestGameCanvasPaintBlitsAtTheOriginUnderTheDestinationLock(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	invokeFixtureVoid(t, runtime, "UIMIDlet", "drawBuffer")
	canvas := fixtureStatic(t, runtime, "buffer", "LUIMIDlet$Buffer;")

	target := reviewContext(40, 30)
	var screen sync.Mutex
	target.screen = &screen
	target.translateX, target.translateY = 5, 3
	screen.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := runtime.gameCanvasDrawBuffer(runtime.VM, []jvm.Value{jvm.ReferenceValue(canvas), jvm.ReferenceValue(graphicsObject(target))})
		done <- err
	}()
	select {
	case <-done:
		screen.Unlock()
		t.Fatal("drawBuffer wrote while the destination's lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	screen.Unlock()
	if err := <-done; err != nil {
		t.Fatalf("drawBuffer() error = %v", err)
	}
	pixelAt := func(x, y int) []byte { index := (y*40 + x) * 4; return target.pixels[index : index+4] }
	if got := pixelAt(5, 3); got[0] != 0xff || got[1] != 0 || got[2] != 0 {
		t.Fatalf("pixel at the origin = %v, want the buffer's red", got)
	}
	for _, point := range [][2]int{{0, 0}, {4, 3}, {5, 2}} {
		if got := pixelAt(point[0], point[1]); got[0] != 0 || got[3] != 0 {
			t.Fatalf("pixel %v outside the translated buffer = %v, want it untouched", point, got)
		}
	}
}

// getKeyStates reports a key pressed since the previous call even when it was
// released before the title polled, and only once.
func TestGameCanvasReportsATapBetweenTwoPolls(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	if err := runtime.SendKey(KeyPressed, KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SendKey(KeyReleased, KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	if states := invokeFixtureInt(t, runtime, "UIMIDlet", "bufferKeyStates"); states != 1<<8 {
		t.Fatalf("bufferKeyStates() after a tap = %d, want FIRE_PRESSED", states)
	}
	if states := invokeFixtureInt(t, runtime, "UIMIDlet", "bufferKeyStates"); states != 0 {
		t.Fatalf("bufferKeyStates() on the next poll = %d, want 0", states)
	}
}

// The latch travels in the checkpoint's KeyStates beside the held bits, and a
// checkpoint written before there was a latch restores with none.
func TestGameCanvasKeyLatchCheckpointForm(t *testing.T) {
	held, latched := int32(1<<8|1<<1), int32(1<<12|1<<6)
	gotHeld, gotLatched := splitGameCanvasKeyRecord(gameCanvasKeyRecord(held, latched))
	if gotHeld != held || gotLatched != latched {
		t.Fatalf("round trip = %#x, %#x, want %#x, %#x", gotHeld, gotLatched, held, latched)
	}
	gotHeld, gotLatched = splitGameCanvasKeyRecord(1 << 8)
	if gotHeld != 1<<8 || gotLatched != 0 {
		t.Fatalf("older record = %#x, %#x, want the held bits and no latch", gotHeld, gotLatched)
	}
}

// Splitting an over-long word or trimming an over-long title costs time in
// proportion to the text, not to its square or cube.
func TestScreenTextLayoutIsLinearInTheText(t *testing.T) {
	font := &fontData{fontKey: fontKey{}, scale: 1, height: 10, baseline: 8}
	for name, call := range map[string]func(){
		"wrapText":    func() { wrapText(font, strings.Repeat("가", 6000), 40) },
		"trimToWidth": func() { trimToWidth(font, strings.Repeat("a", 50000), 100) },
	} {
		done := make(chan struct{})
		go func() { call(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s on a long unbroken text did not finish", name)
		}
	}
}

// The linear layout breaks and trims exactly where the re-measuring one did.
func TestScreenTextLayoutMatchesTheRemeasuringOne(t *testing.T) {
	fonts := []*fontData{
		{fontKey: fontKey{}, scale: 1, height: 10, baseline: 8},
		{fontKey: fontKey{style: fontBold}, scale: 2, height: 20, baseline: 16},
		{fontKey: fontKey{face: fontMonospace}, scale: 1, height: 10, baseline: 8},
	}
	alphabet := []rune("aWi.가 나\t\n")
	random := rand.New(rand.NewSource(11))
	for round := 0; round < 2000; round++ {
		font := fonts[round%len(fonts)]
		text := make([]rune, random.Intn(40))
		for index := range text {
			text[index] = alphabet[random.Intn(len(alphabet))]
		}
		width := random.Intn(90) - 5
		if got, want := wrapText(font, string(text), width), referenceWrapText(font, string(text), width); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("wrapText(%q, %d) = %q, want %q", string(text), width, got, want)
		}
		if got, want := trimToWidth(font, string(text), width), referenceTrimToWidth(font, string(text), width); got != want {
			t.Fatalf("trimToWidth(%q, %d) = %q, want %q", string(text), width, got, want)
		}
	}
}

func referenceTrimToWidth(font *fontData, text string, width int) string {
	if font == nil || width <= 0 {
		return text
	}
	runes := []rune(text)
	if font.textWidth(runes) <= width {
		return text
	}
	ellipsis := []rune("…")
	for length := len(runes) - 1; length > 0; length-- {
		if font.textWidth(append(append([]rune(nil), runes[:length]...), ellipsis...)) <= width {
			return string(runes[:length]) + "…"
		}
	}
	return ""
}

func referenceWrapText(font *fontData, text string, width int) []string {
	if text == "" {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		current := ""
		flush := func() {
			lines = append(lines, current)
			current = ""
		}
		for _, word := range strings.Split(paragraph, " ") {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}
			if font == nil || font.textWidth([]rune(candidate)) <= width {
				current = candidate
				continue
			}
			if current != "" {
				flush()
			}
			remaining := []rune(word)
			for len(remaining) > 0 {
				cut := len(remaining)
				for cut > 1 && font.textWidth(remaining[:cut]) > width {
					cut--
				}
				lines = append(lines, string(remaining[:cut]))
				remaining = remaining[cut:]
			}
			if len(lines) > 0 {
				current = lines[len(lines)-1]
				lines = lines[:len(lines)-1]
			}
		}
		if current != "" {
			flush()
		}
	}
	return lines
}

func fixtureStatic(t *testing.T, runtime *Runtime, name, descriptor string) *jvm.Object {
	t.Helper()
	value, err := runtime.VM.StaticField("UIMIDlet", name, descriptor)
	if err != nil {
		t.Fatalf("UIMIDlet.%s error = %v", name, err)
	}
	object, err := value.Reference()
	if err != nil || object == nil {
		t.Fatalf("UIMIDlet.%s = %v, %v", name, object, err)
	}
	return object
}

func invokeObjectVoid(t *testing.T, runtime *Runtime, object *jvm.Object, method, descriptor string, arguments ...jvm.Value) {
	t.Helper()
	if _, err := runtime.VM.InvokeVirtual(object, method, descriptor, arguments...); err != nil {
		t.Fatalf("%s.%s%s error = %v", object.ClassName, method, descriptor, err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
}

func invokeObjectInt(t *testing.T, runtime *Runtime, object *jvm.Object, method, descriptor string, arguments ...jvm.Value) int32 {
	t.Helper()
	value, err := runtime.VM.InvokeVirtual(object, method, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s.%s%s error = %v", object.ClassName, method, descriptor, err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	number, _ := value.Int32()
	return number
}

// pressKeySafely is pressKey that turns a Host panic into a test failure, so
// one broken press reports itself rather than ending the test binary.
func pressKeySafely(t *testing.T, runtime *Runtime, keyCode int32) {
	t.Helper()
	failure := func() (failure any) {
		defer func() { failure = recover() }()
		if err := runtime.SendKey(KeyPressed, keyCode); err != nil {
			return err
		}
		if err := runtime.RunPending(); err != nil {
			return err
		}
		return nil
	}()
	if failure != nil {
		t.Fatalf("key %d: %v", keyCode, failure)
	}
}
