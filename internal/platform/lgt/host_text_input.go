package lgt

import (
	"context"

	"github.com/movingwoo/wfeature/internal/backend"
)

const (
	javaTextConstraintAny          int32 = 0
	javaTextConstraintNumber       int32 = 1
	javaTextConstraintPassword     int32 = 2
	javaTextConstraintEmailAddress int32 = 3
	javaTextConstraintURL          int32 = 4
	javaTextConstraintPhoneNumber  int32 = 5
)

// TextInput snapshots the focused, visible LWC text component for a Host that
// composes a whole string with its native keyboard or IME. Session serializes
// this call, guest ticks and Commit because the ARM guest is not re-entrant.
func (session *Session) TextInput(ctx context.Context) (*backend.TextInput, error) {
	if session == nil || session.client == nil || session.client.javaRun == nil {
		return nil, backend.ErrNoTextInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := session.client
	runtime := client.javaRun
	target := runtime.focusedWidget
	state := runtime.widgets[target]
	if target == 0 || !client.javaTextInputAvailable(target, state) {
		return nil, backend.ErrNoTextInput
	}
	constraint := state.mode
	inputMode, password, valid := javaTextInputHints(constraint)
	if !valid {
		return nil, backend.ErrNoTextInput
	}
	text := state.text
	maxLength := int(state.maxLength)
	if maxLength < 0 {
		maxLength = 0
	}
	multiline := state.kind == javaWidgetTextBox
	revision := state.revision
	generation := runtime.widgetGeneration
	handler := state.inputHandler

	return &backend.TextInput{
		Text: text, MaxLength: maxLength, Multiline: multiline,
		Password: password, InputMode: inputMode,
		Commit: func(commitCtx context.Context, replacement string) error {
			if err := commitCtx.Err(); err != nil {
				return err
			}
			if client.javaRun != runtime || runtime.focusedWidget != target ||
				runtime.widgetGeneration != generation || runtime.widgets[target] != state ||
				state.revision != revision || state.inputHandler != handler ||
				!client.javaTextInputAvailable(target, state) {
				return backend.ErrTextInputChanged
			}
			if err := validateJavaTextInput(replacement, constraint, maxLength, multiline); err != nil {
				return err
			}
			if replacement == text {
				return nil
			}
			state.text = replacement
			state.revision++
			return nil
		},
	}, nil
}

// A listener changes the callback contract of a commit. The current LGT
// corpus installs none, and this runtime cannot yet enter notifyTextChanged
// safely from a Host commit, so such a field is refused instead of updated
// without the callback the guest requested.
func (client *Client) javaTextInputAvailable(target uint32, state *javaWidget) bool {
	if state == nil || state.kind != javaWidgetTextField && state.kind != javaWidgetTextBox ||
		!client.javaWidgetShown(target) {
		return false
	}
	if state.inputHandler == 0 {
		return true
	}
	handler := client.javaRun.widgets[state.inputHandler]
	return handler != nil && handler.listener == 0
}

func javaTextInputHints(constraint int32) (inputMode string, password, valid bool) {
	switch constraint {
	case javaTextConstraintAny:
		return "text", false, true
	case javaTextConstraintNumber:
		return "numeric", false, true
	case javaTextConstraintPassword:
		return "numeric", true, true
	case javaTextConstraintEmailAddress:
		return "email", false, true
	case javaTextConstraintURL:
		return "url", false, true
	case javaTextConstraintPhoneNumber:
		return "tel", false, true
	default:
		return "", false, false
	}
}

func validateJavaTextInput(text string, constraint int32, maxLength int, multiline bool) error {
	if err := backend.ValidateTextInput(text); err != nil {
		return err
	}
	if maxLength > 0 && len(utf16Units(text)) > maxLength {
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
	case javaTextConstraintAny:
	case javaTextConstraintNumber:
		valid = everyJavaTextRune(text, func(symbol rune) bool {
			return symbol >= '0' && symbol <= '9' || symbol == '-' || symbol == ' '
		})
	case javaTextConstraintPassword, javaTextConstraintPhoneNumber:
		valid = everyJavaTextRune(text, func(symbol rune) bool { return symbol >= '0' && symbol <= '9' })
	case javaTextConstraintEmailAddress, javaTextConstraintURL:
		valid = everyJavaTextRune(text, func(symbol rune) bool { return symbol >= 0x21 && symbol <= 0x7e })
	default:
		valid = false
	}
	if !valid {
		return backend.ErrInvalidTextInput
	}
	return nil
}

func everyJavaTextRune(text string, valid func(rune) bool) bool {
	for _, symbol := range text {
		if !valid(symbol) {
			return false
		}
	}
	return true
}
