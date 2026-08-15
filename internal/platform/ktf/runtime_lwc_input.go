package ktf

import (
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

// An lwc text component was a real editable field on a handset: the user typed
// into it with the keypad. Until now this runtime kept and reported the text a
// component was constructed with and nothing could change it, which is the gap
// docs/ktf.md recorded as "text input editing".
//
// The editing itself is shared with MIDP in internal/textinput; what lives
// here is which component has the focus and how a key reaches it.

// FocusTextComponent gives the keypad to one lwc text component, or takes it
// away with nil. A Host that draws a text field calls this when the user taps
// it; a game that uses no text field never does, and keys reach the cards as
// before.
func (client *Client) FocusTextComponent(component *jvm.Object) {
	if client == nil {
		return
	}
	client.textMu.Lock()
	defer client.textMu.Unlock()
	client.focusedText = component
	if component == nil {
		client.textEditor = nil
		return
	}
	client.textEditor = textinput.New(componentText(component), int(componentMaxLength(component)))
}

// FocusedTextComponent reports which component has the keypad.
func (client *Client) FocusedTextComponent() *jvm.Object {
	if client == nil {
		return nil
	}
	client.textMu.Lock()
	defer client.textMu.Unlock()
	return client.focusedText
}

// TypeKey delivers one key to the focused component. It reports whether the
// key was consumed: a component with the focus takes the keypad keys and
// leaves everything else — the navigation keys a game also wants — alone.
func (client *Client) TypeKey(key rune) bool {
	if client == nil {
		return false
	}
	client.textMu.Lock()
	editor, component := client.textEditor, client.focusedText
	client.textMu.Unlock()
	if editor == nil || component == nil {
		return false
	}
	if !isKeypadKey(key) {
		return false
	}
	// The editor is given guest time, not wall time, so a Host batching ticks
	// types the same text as one running live.
	now := client.now()
	changed := editor.Key(key, now)
	if changed {
		client.textMu.Lock()
		text := editor.Text()
		client.textMu.Unlock()
		component.Fields[componentTextField] = jvm.ReferenceValue(client.vm.NewString(text))
	}
	// The star and hash keys are the mode and backspace keys, so they are
	// consumed whether or not they changed the text.
	return true
}

// isKeypadKey reports whether a key belongs to the keypad rather than to the
// game's navigation.
func isKeypadKey(key rune) bool {
	return (key >= '0' && key <= '9') || key == '*' || key == '#'
}

// componentText reads a component's current string.
func componentText(component *jvm.Object) string {
	if component == nil {
		return ""
	}
	value, ok := component.Fields[componentTextField]
	if !ok {
		return ""
	}
	object, err := value.Reference()
	if err != nil || object == nil {
		return ""
	}
	text, _ := jvm.StringText(object)
	return text
}

// componentMaxLength reads a component's limit; zero means unlimited.
func componentMaxLength(component *jvm.Object) int32 {
	if component == nil {
		return 0
	}
	value, ok := component.Fields[componentMaxLengthField]
	if !ok {
		return 0
	}
	length, err := value.Int32()
	if err != nil || length < 0 {
		return 0
	}
	return length
}
