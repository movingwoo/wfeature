package skt

import (
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

// The vendor's text input, which is two halves. A title that draws its own
// field implements `com.xce.lcdui.TextComponent` and hands it to the
// platform's `TextComponentHandler`; a title that wants the platform's field
// makes an `XTextField` instead. Both type here, with the keypad input method
// every other text field on every platform in this runtime uses.
//
// **The component is where the text lives, and the interface is why.** It has
// `insert`, `replace`, `delete` and `moveCursor` and no way to read a
// character back, so the handset's input method never held the text: it sent
// the edits and the title kept them. That is exactly what a multi-tap cycle
// needs — `insert` for a new character and `replace` for the next letter on
// the same key — and it is what this does, against a component it cannot read.
// One local title's own implementation settles each one: `replace` writes at
// `caret - 1`, `clear` empties the field, and `moveCursor` takes the raw key
// code rather than a direction, switching on the two the pad sends.
//
// What no local evidence settles is Hangul. A handset composed it in the input
// method, out of the jamo the keypad carries; this types the Latin and numeric
// modes the shared editor has. A title's field takes what it is sent either
// way — see `inputMode` for what it reports it is in.

// textInputState is the input method's own state: which component it is
// editing and where the multi-tap cycle it is running has got to. There is one
// of it because a handset has one input method.
type textInputState struct {
	component *jvm.Object
	mode      textinput.Mode
	// The cycle in progress: which key it belongs to, how far through that
	// key's characters it has gone, and when the last press was. cycleKey is
	// zero when the character has been committed and the next press of the
	// same key starts a new one.
	cycleKey rune
	cyclePos int
	lastKey  time.Time
}

// textComponentHandler answers the handler singleton, making it on the first
// call. A handset has one input method, so a title that asks twice gets the
// same object — one local title keeps it in a static, and answering a new
// object each time would give each caller a different composition state.
func (runtime *Runtime) textComponentHandler(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	state := runtime.skvm()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.textHandler == nil {
		handler, err := vm.NewObject(skvm.TextComponentHandlerClass, "()V")
		if err != nil {
			return jvm.VoidValue(), err
		}
		state.textHandler = handler
	}
	return jvm.ReferenceValue(state.textHandler), nil
}

// setTextComponent attaches the component the input method edits, or detaches
// it when a title passes null — which is how the local title turns its field
// off. Attaching starts a fresh composition either way: the cycle belongs to
// the field that was being typed into.
func (runtime *Runtime) setTextComponent(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := referenceArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	component, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	state.textInput.component = component
	state.textInput.endCycle()
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

// clearTextComponentHandler ends the composition in progress — the letter the
// same key is still cycling through — and leaves the component attached and
// its text alone. That is the reading the local title's own code settles: it
// calls this from `moveCursor`, where a cycle that kept running would write
// the next letter over whatever the caret had moved to.
func (runtime *Runtime) clearTextComponentHandler(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := referenceArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	state.textInput.endCycle()
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

// endCycle commits whatever character the cycle was on, so the next press of
// the same key inserts rather than replaces.
func (state *textInputState) endCycle() {
	state.cycleKey = 0
	state.cyclePos = 0
}

// textComponentInputMode answers which mode the input method is in, which a
// title reads to draw the indicator a handset showed beside a field.
//
// The five modes are a bit each. Which bit is which is not documented
// anywhere available here; what settles it is the order one local title draws
// them in — its switch maps 16, 1, 2, 8 and 4 onto indicators 0 to 4, and the
// order a Korean handset showed was Hangul, capitals, small letters, digits,
// symbols. So 16 is Hangul, and the three this runtime can be in are the
// three below. A title that reads the mode gets an indicator that matches what
// its next key press will produce, which is the whole of what it asks for.
func (runtime *Runtime) textComponentInputMode(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := referenceArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	mode := state.textInput.mode
	state.mu.Unlock()
	return jvm.IntValue(vendorInputMode(mode)), nil
}

// Input mode bits, in the order the local title's indicator switch names them.
const (
	vendorModeUppercase int32 = 1
	vendorModeLowercase int32 = 2
	vendorModeSymbol    int32 = 4
	vendorModeNumeric   int32 = 8
	vendorModeHangul    int32 = 16
)

func vendorInputMode(mode textinput.Mode) int32 {
	switch mode {
	case textinput.ModeUppercase:
		return vendorModeUppercase
	case textinput.ModeNumeric:
		return vendorModeNumeric
	}
	return vendorModeLowercase
}

// textComponentKeyReleased answers that the input method did not take the
// release. Composition happens on the press, and a title that gets the release
// anyway is free to act on it.
func (runtime *Runtime) textComponentKeyReleased(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := referenceArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := intArgument(arguments, 1); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(0), nil
}

// textComponentKeyPressed types one key into the attached component and
// answers whether the input method took it. What it does not take reaches the
// game: a title routes every key here first, and its pad has to keep working
// while a field is on screen.
func (runtime *Runtime) textComponentKeyPressed(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := referenceArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	keyCode, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	component := state.textInput.component
	edit := state.textInput.press(keyCode, runtime.editorClock())
	state.mu.Unlock()
	if component == nil || edit.kind == editNone {
		return jvm.IntValue(0), nil
	}
	if err := runtime.applyTextEdit(vm, component, edit); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(1), nil
}

// textEdit is one edit the input method makes to a component: which of the
// interface's methods to call and with what.
type textEditKind uint8

const (
	editNone textEditKind = iota
	editInsert
	editReplace
	editDelete
	editMoveCursor
	editModeChanged
)

type textEdit struct {
	kind      textEditKind
	character rune
	keyCode   int32
}

// press runs one key through the multi-tap cycle and says what the component
// should be told. The component holds the text, so the limit it is filled to
// is the component's to enforce: this reports the edit and the caller applies
// it only while `size` is under `getMaxSize`.
func (state *textInputState) press(keyCode int32, now time.Time) textEdit {
	switch keyCode {
	case KeyCodeClear:
		state.endCycle()
		return textEdit{kind: editDelete}
	case KeyCodeLeft, KeyCodeRight:
		state.endCycle()
		return textEdit{kind: editMoveCursor, keyCode: keyCode}
	case '*':
		state.mode = state.mode.Next()
		state.endCycle()
		return textEdit{kind: editModeChanged}
	case '#':
		state.endCycle()
		return textEdit{kind: editDelete}
	}
	if keyCode < 0 || keyCode > 0x7f {
		return textEdit{}
	}
	characters, ok := textinput.Characters(rune(keyCode))
	if !ok {
		return textEdit{}
	}
	if state.mode == textinput.ModeNumeric {
		state.endCycle()
		return textEdit{kind: editInsert, character: rune(keyCode)}
	}
	options := []rune(characters)
	// A second press of the same key inside the commit delay writes the next
	// letter over the one it just produced, which is what `replace` is for.
	if state.cycleKey == rune(keyCode) && now.Sub(state.lastKey) < textinput.CommitDelay {
		state.cyclePos = (state.cyclePos + 1) % len(options)
		state.lastKey = now
		return textEdit{kind: editReplace, character: state.mode.Apply(options[state.cyclePos])}
	}
	state.cycleKey, state.cyclePos, state.lastKey = rune(keyCode), 0, now
	return textEdit{kind: editInsert, character: state.mode.Apply(options[0])}
}

// applyTextEdit calls the component the way the handset's input method did.
// Every call is virtual, because the component is the title's own class.
func (runtime *Runtime) applyTextEdit(vm *jvm.VM, component *jvm.Object, edit textEdit) error {
	switch edit.kind {
	case editModeChanged:
		// Nothing is inserted, but the indicator the title draws from
		// getInputMode has changed.
		_, err := vm.InvokeVirtual(component, "repaint", "()V")
		return err
	case editMoveCursor:
		// The component decides what its own caret does with the key; one
		// local title inserts a space when the caret is already at the end.
		_, err := vm.InvokeVirtual(component, "moveCursor", "(I)V", jvm.IntValue(edit.keyCode))
		return err
	case editDelete:
		if _, err := vm.InvokeVirtual(component, "delete", "()V"); err != nil {
			return err
		}
	case editReplace:
		if _, err := vm.InvokeVirtual(component, "replace", "(C)V", jvm.IntValue(int32(edit.character))); err != nil {
			return err
		}
	case editInsert:
		full, err := textComponentIsFull(vm, component)
		if err != nil {
			return err
		}
		if full {
			return nil
		}
		if _, err := vm.InvokeVirtual(component, "insert", "(C)V", jvm.IntValue(int32(edit.character))); err != nil {
			return err
		}
	default:
		return nil
	}
	_, err := vm.InvokeVirtual(component, "repaint", "()V")
	return err
}

// textComponentIsFull asks the component whether another character fits. The
// two numbers are the only way to know: the interface hands out edits and
// never a character, so the platform cannot count the text itself.
func textComponentIsFull(vm *jvm.VM, component *jvm.Object) (bool, error) {
	size, err := vm.InvokeVirtual(component, "size", "()I")
	if err != nil {
		return false, err
	}
	limit, err := vm.InvokeVirtual(component, "getMaxSize", "()I")
	if err != nil {
		return false, err
	}
	length, err := size.Int32()
	if err != nil {
		return false, err
	}
	maxSize, err := limit.Int32()
	if err != nil {
		return false, err
	}
	return maxSize > 0 && length >= maxSize, nil
}

// initXTextFieldWithText builds the field a title asks for by hand: the text
// it starts with, the size it stops at, the constraints, and the Canvas it
// belongs to. The three-argument prefix is read as text-then-size-then-
// constraints because the one local call site passes ("", 10, 0) for a name
// of up to ten characters, and 10 is not a constraint any of them names.
func (runtime *Runtime) initXTextFieldWithText(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "XTextField is null")
	}
	text, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	maxSize, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if maxSize <= 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "XTextField max size is not positive")
	}
	constraints, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	owner, err := referenceArgument(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runes := []rune(text)
	if int32(len(runes)) > maxSize {
		runes = runes[:maxSize]
	}
	receiver.Native = &xTextFieldData{text: runes, maxSize: maxSize, constraints: constraints}
	// The Canvas is kept as a cheat root rather than on the field, because
	// what a search has to be able to reach is the screen the field is on.
	state := runtime.skvm()
	state.mu.Lock()
	state.textFieldOwner = owner
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

// xTextFieldKeyPressed types into the platform's own field. Here the platform
// holds the text, so it is the shared editor itself rather than the cycle
// above — the same one a MIDP TextBox on this platform types with.
func (runtime *Runtime) xTextFieldKeyPressed(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	keyCode, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	editor := data.editor()
	switch keyCode {
	case KeyCodeLeft:
		editor.MoveCaret(-1)
		return jvm.VoidValue(), nil
	case KeyCodeRight:
		editor.MoveCaret(1)
		return jvm.VoidValue(), nil
	case KeyCodeClear:
		editor.Backspace()
		data.text = []rune(editor.Text())
		return jvm.VoidValue(), nil
	}
	if keyCode < 0 || keyCode > 0x7f {
		return jvm.VoidValue(), nil
	}
	if editor.Key(rune(keyCode), runtime.editorClock()) {
		data.text = []rune(editor.Text())
	}
	return jvm.VoidValue(), nil
}

// editor is the field's keypad editor, made on first use and kept in step with
// text the title set through setText.
func (data *xTextFieldData) editor() *textinput.State {
	if data.input == nil {
		data.input = textinput.New(string(data.text), int(data.maxSize))
		return data.input
	}
	if data.input.Text() != string(data.text) {
		data.input.SetText(string(data.text))
	}
	data.input.SetMaxRunes(int(data.maxSize))
	return data.input
}
