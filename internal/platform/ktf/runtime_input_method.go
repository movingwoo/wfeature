package ktf

import (
	"fmt"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

// A title that draws its own text field builds an InputMethodHandler, gives it
// a listener of its own, and hands it every key the field's card receives. The
// handler is the automaton: it turns keys into characters and tells the
// listener what each key did to the text. **The listener holds the text and
// the handler never reads it**, so what crosses between them is an edit, not a
// value.
//
// Which edits a listener expects is settled by three local titles rather than
// by the specification, which names the three modes of notifyTextChanged and
// nothing about how a listener applies them. All three implement it the same
// way: an insertion (-1) appends `new String(chText)` — the whole array, not
// its first `len` characters — a deletion (1) cuts `len` characters off the
// end, and a replacement (0) is ignored. So a multi-tap cycle, which on a
// keypad replaces the letter it just typed, reaches the listener as a deletion
// of that letter followed by an insertion of the next one; a replacement
// would leave those titles showing the first letter of every key.
//
// The keys are the handset's multi-tap keypad, internal/textinput, which is
// the automaton every other text field here types through. Only the digits and
// the clear key are the handler's: these titles switch modes on a soft key of
// their own and one labels `#` with a command on the same screen, so the star
// and hash keys stay theirs rather than becoming the mode key and backspace a
// text component makes of them, and so does everything that moves around the
// screen.

const (
	// Edits as notifyTextChanged names them.
	inputMethodInsert int32 = -1
	inputMethodDelete int32 = 1

	// inputMethodConstraintField is the constraint the handler was built with,
	// which decides the characters a numeric field can take whatever mode a
	// title later sets.
	inputMethodConstraintField = "host:input-constraint"
	inputMethodModeField       = "mode:I"
)

// Modes as a title sets them on its handler. The specification names the
// modes without their numbers; one local title settles them. It draws its own
// indicator — 가, A, a, 1 — opens its field under 가 with setCurrentMode(3),
// and its mode key steps the indicator through A, a and 1 with 1, 0 and 2.
// These are not the WIPI C input method's numbers, whose list starts with the
// capitals. Hangul is composed from jamo the keypad carries in a layout no
// local evidence fixes, so a handler in that mode types nothing until the
// title switches, which is where the C input method leaves Hangul too.
const (
	inputMethodModeLowercase int32 = 0
	inputMethodModeUppercase int32 = 1
	inputMethodModeNumeric   int32 = 2
	inputMethodModeHangul    int32 = 3
)

// runtimeInputMethodNotifyKeyInput is `notifyKeyInput(int keyCode, int type)`.
// It answers whether the handler processed the key, which is what the
// specification makes of the return; a handler with no listener processes
// nothing.
func runtimeInputMethodNotifyKeyInput(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("InputMethodHandler.notifyKeyInput", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	key, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	eventType, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	var listener *jvm.Object
	if value, ok := receiver.Fields[inputMethodListenerField]; ok {
		if listener, err = value.Reference(); err != nil {
			return jvm.VoidValue(), err
		}
	}
	// A press types; a release and a repeat do not, for the reason a text
	// component gives: multi-tap counts presses.
	if listener == nil || eventType != KeyPressed {
		return jvm.IntValue(0), nil
	}
	editor := inputMethodEditorFor(receiver)
	switch {
	case key == KeyClear:
		deleted := inputMethodLastCharacter(editor)
		if !editor.Backspace() {
			// Nothing this handler typed is left to take back. What the
			// listener holds beyond that is the title's own, and the key is
			// answered as unprocessed so the title can act on it.
			return jvm.IntValue(0), nil
		}
		if err := notifyInputMethodListener(vm, listener, deleted, inputMethodDelete); err != nil {
			return jvm.VoidValue(), err
		}
	case key >= KeyNum0 && key <= KeyNum9:
		mode, ok := inputMethodKeypadMode(receiver)
		if !ok {
			return jvm.IntValue(0), nil
		}
		editor.SetMode(mode)
		before := inputMethodLastCharacter(editor)
		length := len([]rune(editor.Text()))
		if !editor.Key(rune(key), runtime.client.now()) {
			return jvm.IntValue(0), nil
		}
		if len([]rune(editor.Text())) == length {
			if err := notifyInputMethodListener(vm, listener, before, inputMethodDelete); err != nil {
				return jvm.VoidValue(), err
			}
		}
		if err := notifyInputMethodListener(vm, listener, inputMethodLastCharacter(editor), inputMethodInsert); err != nil {
			return jvm.VoidValue(), err
		}
	default:
		return jvm.IntValue(0), nil
	}
	return jvm.IntValue(1), nil
}

// inputMethodKeypadMode answers the character set the handler's mode types,
// and false for a mode the keypad here has none for.
func inputMethodKeypadMode(receiver *jvm.Object) (textinput.Mode, bool) {
	if constraint, _ := receiver.Fields[inputMethodConstraintField].Int32(); constraint == textConstraintNumber ||
		constraint == textConstraintPassword || constraint == textConstraintPhoneNumber {
		return textinput.ModeNumeric, true
	}
	mode, err := receiver.Fields[inputMethodModeField].Int32()
	if err != nil {
		return 0, false
	}
	switch mode {
	case inputMethodModeLowercase:
		return textinput.ModeLowercase, true
	case inputMethodModeUppercase:
		return textinput.ModeUppercase, true
	case inputMethodModeNumeric:
		return textinput.ModeNumeric, true
	case inputMethodModeHangul:
		return 0, false
	}
	// A mode no title has been seen to set, such as the symbol card the
	// specification mentions, has no keypad layout here either.
	return 0, false
}

// inputMethodEditorFor answers the automaton's own record of what it typed,
// which is what a multi-tap cycle and the clear key work against. It lives on
// the handler for the reason a text component's editor lives on the component:
// a table keyed by the object would keep every handler a title ever built
// alive, and the checkpoint already carries an editor wherever it sits.
func inputMethodEditorFor(receiver *jvm.Object) *textinput.State {
	if editor, ok := receiver.Native.(*textinput.State); ok {
		return editor
	}
	editor := textinput.New("", 0)
	receiver.Native = editor
	return editor
}

// inputMethodLastCharacter is the character a deletion or a cycle takes back.
// The handler's caret only ever moves forward through what it typed, so that is
// the last one.
func inputMethodLastCharacter(editor *textinput.State) rune {
	text := []rune(editor.Text())
	if len(text) == 0 {
		return 0
	}
	return text[len(text)-1]
}

// notifyInputMethodListener hands one edit to the listener: the character, how
// many characters the edit covers, and which edit it is. The array holds
// exactly the edit's characters, because the titles that settled the contract
// build their insertion from the whole array.
func notifyInputMethodListener(vm *jvm.VM, listener *jvm.Object, character rune, edit int32) error {
	units := utf16.Encode([]rune{character})
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeChar}, int32(len(units)))
	if err != nil {
		return err
	}
	values := make([]jvm.Value, len(units))
	for index, unit := range units {
		values[index] = jvm.IntValue(int32(unit))
	}
	if err := array.Native.(*jvm.Array).StoreRange(0, values); err != nil {
		return err
	}
	if _, err := vm.InvokeVirtual(listener, "notifyTextChanged", "([CII)V",
		jvm.ReferenceValue(array), jvm.IntValue(1), jvm.IntValue(edit)); err != nil {
		return fmt.Errorf("notify KTF input method listener %s: %w", listener.ClassName, err)
	}
	return nil
}

// runtimeInputMethodSetListener keeps the listener the handler's edits go to. A
// different listener keeps text of its own, so what the handler typed for the
// previous one is no longer anything the new one holds.
func runtimeInputMethodSetListener(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	var previous jvm.Value
	if len(arguments) == 2 {
		if receiver, err := arguments[0].Reference(); err == nil && receiver != nil {
			previous = receiver.Fields[inputMethodListenerField]
		}
	}
	result, err := runtimeComponentSetField("InputMethodHandler.setInputMethodListener", inputMethodListenerField)(runtime, vm, arguments)
	if err != nil {
		return result, err
	}
	receiver, _ := arguments[0].Reference()
	if _, ok := receiver.Native.(*textinput.State); ok && previous != arguments[1] {
		receiver.Native = nil
	}
	return result, nil
}
