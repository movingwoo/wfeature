package ktf

import (
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

// A text component takes the keys its own card hands it.
//
// The rule everywhere else in this toolkit is that a widget answers a key as
// unconsumed, because a widget that took one would be taking it from the card
// that is the only thing drawing and the player would be typing into something
// invisible. **A card that calls `keyNotify` on a component itself is the case
// that rule was not about.** One local title builds two `TextFieldComponent`s
// for a name entry screen, gives one focus, and from its own `keyNotify`
// forwards the key to the component and then calls `getString` on the very
// next call — so the title draws the text, and the only half that was missing
// was the component doing anything with the key.
//
// What it does with it is the handset's multi-tap keypad, which is
// `internal/textinput` and is the same automaton the other platforms type
// through. Only the keys a keypad carries are taken: a digit, the mode key,
// the two that delete. Everything else — the navigation keys, the soft keys,
// the select key — is answered as unconsumed, because a screen with a text
// field on it still has to be able to leave.

// runtimeTextComponentKeyNotify is `TextComponent.keyNotify(int type, int key)`.
// It answers zero when the component took the key, which is the same polarity a
// card's keyNotify uses: a nonzero answer is the key still travelling.
func runtimeTextComponentKeyNotify(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("TextComponent.keyNotify", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	eventType, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	key, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	// A press types; a release and a repeat do not. A handset's multi-tap
	// counts presses, and taking the repeat as well would cycle a character
	// while a thumb rested on the key.
	if eventType != KeyPressed {
		return jvm.IntValue(1), nil
	}
	editor := textEditorFor(receiver)
	changed := false
	switch {
	case key == KeyClear:
		changed = editor.Backspace()
	case key >= KeyNum0 && key <= KeyNum9, key == KeyStar, key == KeyHash:
		changed = editor.Key(rune(key), runtime.client.now())
	default:
		return jvm.IntValue(1), nil
	}
	if changed {
		receiver.Fields[componentTextField] = jvm.ReferenceValue(vm.NewString(editor.Text()))
	}
	// The mode key changes nothing and is still the component's key: a title
	// that got it back would move its own menu on it.
	return jvm.IntValue(0), nil
}

// textEditorFor answers the multi-tap state of one text component, building it
// over whatever the component holds now. The component's text is the value —
// `setString` and the box's own `insert` and `delete` write it — so the editor
// is brought back into step with it rather than owning it, and what the editor
// keeps between keys is the cycle: which key is being tapped and how far
// through its characters.
//
// It lives on the component rather than in a table on the runtime, because a
// table keyed by the object would be a Go reference to every component a title
// has ever built — and a Go reference is exactly what the object collector
// reads as a live object (see collect.go). A text component is never a
// container, so `Native` is free on one.
func textEditorFor(receiver *jvm.Object) *textinput.State {
	text := runtimeComponentText(receiver)
	limit := int(runtimeComponentMaxLength(receiver))
	editor, ok := receiver.Native.(*textinput.State)
	if !ok {
		editor = textinput.New(text, limit)
		receiver.Native = editor
		return editor
	}
	editor.SetMaxRunes(limit)
	if editor.Text() != text {
		editor.SetText(text)
	}
	return editor
}
