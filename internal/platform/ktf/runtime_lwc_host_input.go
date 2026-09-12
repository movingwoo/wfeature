package ktf

import (
	"context"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

const (
	componentConstraintField = "constraint:I"

	textConstraintAny          int32 = 0
	textConstraintNumber       int32 = 1
	textConstraintPassword     int32 = 2
	textConstraintEmailAddress int32 = 3
	textConstraintURL          int32 = 4
	textConstraintPhoneNumber  int32 = 5
)

// TextInput snapshots the active LWC editor for a Host that composes text with
// its native keyboard or IME. Guest focus is authoritative: Component.setFocus
// and FormComponent.setFocus both write lwc:focus while guest execution holds
// the same run lock taken here.
func (session *Session) TextInput(ctx context.Context) (*backend.TextInput, error) {
	if session == nil || session.Client == nil {
		return nil, backend.ErrNoTextInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := session.Client
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil || client.workersStopped {
		return nil, backend.ErrNoTextInput
	}
	component := client.runtime.runtimeObjects["lwc:focus"]
	multiline, ok := client.lwcTextInputKind(component)
	if !ok {
		return nil, backend.ErrNoTextInput
	}
	constraintValue, hasConstraint := component.Fields[componentConstraintField]
	constraint, ok := lwcTextConstraint(constraintValue, hasConstraint)
	if !ok {
		return nil, backend.ErrNoTextInput
	}
	listenerState, ok := lwcTextInputListenerState(component)
	if !ok || listenerState.listener != nil {
		// InputMethodListener consumes per-key composition deltas. The Host
		// supplies a completed field value, which cannot be represented by
		// that interface without a cursor or replacement range.
		return nil, backend.ErrNoTextInput
	}
	textValue, hasText := component.Fields[componentTextField]
	maxValue, hasMax := component.Fields[componentMaxLengthField]
	text := componentText(component)
	maxLength := int(componentMaxLength(component))
	inputMode, password := lwcTextInputHints(constraint)

	return &backend.TextInput{
		Text:      text,
		MaxLength: maxLength,
		Multiline: multiline,
		Password:  password,
		InputMode: inputMode,
		Commit: func(commitCtx context.Context, replacement string) error {
			if err := commitCtx.Err(); err != nil {
				return err
			}
			client.run.Lock()
			defer client.run.Unlock()
			currentListenerState, listenerStateOK := lwcTextInputListenerState(component)
			if client.runtime == nil || client.workersStopped ||
				client.runtime.runtimeObjects["lwc:focus"] != component ||
				!listenerStateOK || !sameLWCTextInputListenerState(currentListenerState, listenerState) ||
				!sameTextInputField(component, componentTextField, textValue, hasText) ||
				!sameTextInputField(component, componentConstraintField, constraintValue, hasConstraint) ||
				!sameTextInputField(component, componentMaxLengthField, maxValue, hasMax) {
				return backend.ErrTextInputChanged
			}
			if err := validateLWCTextInput(replacement, constraint, maxLength, multiline); err != nil {
				return err
			}
			if replacement == text {
				return nil
			}
			component.Fields[componentTextField] = jvm.ReferenceValue(client.vm.NewString(replacement))
			// Keep the two keypad adapters in step if either is used after a
			// Host composition. Their next key starts a fresh composition.
			textEditorFor(component).SetText(replacement)
			client.textMu.Lock()
			if client.focusedText == component {
				client.textEditor = textinput.New(replacement, maxLength)
			}
			client.textMu.Unlock()
			return nil
		},
	}, nil
}

// lwcTextInputKind follows a guest subclass to the runtime LWC field or box it
// extends. The vendor field also extends TextComponent, but its synchronous
// modal call leaves no active editor for a Host to commit to, so it is refused
// explicitly until that lifecycle exists.
func (client *Client) lwcTextInputKind(component *jvm.Object) (multiline bool, ok bool) {
	if client == nil || client.vm == nil || component == nil {
		return false, false
	}
	name := component.ClassName
	seen := make(map[string]bool)
	for depth := 0; depth < 64 && name != "" && !seen[name]; depth++ {
		seen[name] = true
		switch name {
		case runtimeTextFieldComponentClass:
			return false, true
		case runtimeTextBoxComponentClass:
			return true, true
		case runtimeGTextFieldClass, runtimeTextComponentClass:
			return false, false
		}
		class, found := client.vm.AOTClass(name)
		if !found {
			return false, false
		}
		name = class.SuperName
	}
	return false, false
}

func lwcTextConstraint(value jvm.Value, present bool) (int32, bool) {
	if !present {
		return textConstraintAny, true
	}
	constraint, err := value.Int32()
	if err != nil || constraint < textConstraintAny || constraint > textConstraintPhoneNumber {
		return 0, false
	}
	return constraint, true
}

func lwcTextInputHints(constraint int32) (inputMode string, password bool) {
	switch constraint {
	case textConstraintNumber:
		return "numeric", false
	case textConstraintPassword:
		return "numeric", true
	case textConstraintEmailAddress:
		return "email", false
	case textConstraintURL:
		return "url", false
	case textConstraintPhoneNumber:
		return "tel", false
	default:
		return "text", false
	}
}

func validateLWCTextInput(text string, constraint int32, maxLength int, multiline bool) error {
	if err := backend.ValidateTextInput(text); err != nil {
		return err
	}
	// WIPI counts Java char values. A supplementary Unicode code point is a
	// surrogate pair and consumes two positions in the guest field.
	if maxLength > 0 && len(utf16.Encode([]rune(text))) > maxLength {
		return backend.ErrInvalidTextInput
	}
	if !multiline {
		for _, symbol := range text {
			if symbol == '\r' || symbol == '\n' {
				return backend.ErrInvalidTextInput
			}
		}
	}
	valid := true
	switch constraint {
	case textConstraintAny:
	case textConstraintNumber:
		valid = everyTextRune(text, func(symbol rune) bool {
			return symbol >= '0' && symbol <= '9' || symbol == '-' || symbol == ' '
		})
	case textConstraintPassword, textConstraintPhoneNumber:
		valid = everyTextRune(text, func(symbol rune) bool { return symbol >= '0' && symbol <= '9' })
	case textConstraintEmailAddress, textConstraintURL:
		// The WIPI contract describes these as English letters, digits and
		// symbols. Printable ASCII is that character set; shape validation
		// belongs to the title because the contract specifies no grammar.
		valid = everyTextRune(text, func(symbol rune) bool { return symbol >= 0x21 && symbol <= 0x7e })
	default:
		valid = false
	}
	if !valid {
		return backend.ErrInvalidTextInput
	}
	return nil
}

func everyTextRune(text string, valid func(rune) bool) bool {
	for _, symbol := range text {
		if !valid(symbol) {
			return false
		}
	}
	return true
}

func sameTextInputField(component *jvm.Object, name string, value jvm.Value, present bool) bool {
	current, ok := component.Fields[name]
	return ok == present && (!ok || current == value)
}

type lwcInputListenerState struct {
	revision      jvm.Value
	handlerValue  jvm.Value
	hasHandler    bool
	handler       *jvm.Object
	listenerValue jvm.Value
	hasListener   bool
	listener      *jvm.Object
}

// lwcTextInputListenerState returns the exact handler and delta-listener field
// state so installing or removing one also invalidates an outstanding commit.
func lwcTextInputListenerState(component *jvm.Object) (lwcInputListenerState, bool) {
	state := lwcInputListenerState{}
	state.handlerValue, state.hasHandler = component.Fields[componentInputHandlerField]
	if !state.hasHandler {
		return state, true
	}
	handler, err := state.handlerValue.Reference()
	if err != nil {
		return lwcInputListenerState{}, false
	}
	if handler == nil {
		return state, true
	}
	state.handler = handler
	state.revision = handler.Fields[inputMethodListenerRevisionField]
	state.listenerValue, state.hasListener = handler.Fields[inputMethodListenerField]
	if !state.hasListener {
		return state, true
	}
	listener, err := state.listenerValue.Reference()
	if err != nil {
		return lwcInputListenerState{}, false
	}
	state.listener = listener
	return state, true
}

func sameLWCTextInputListenerState(left, right lwcInputListenerState) bool {
	return left.revision == right.revision &&
		left.handler == right.handler && left.hasHandler == right.hasHandler &&
		(!left.hasHandler || left.handlerValue == right.handlerValue) &&
		left.listener == right.listener && left.hasListener == right.hasListener &&
		(!left.hasListener || left.listenerValue == right.listenerValue)
}
