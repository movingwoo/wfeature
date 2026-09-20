package lgt

import (
	"context"
	"unicode"
	"unicode/utf8"

	"github.com/movingwoo/wfeature/internal/backend"
	"golang.org/x/text/encoding/korean"
)

const (
	javaTextConstraintAny          int32 = 0
	javaTextConstraintNumber       int32 = 1
	javaTextConstraintPassword     int32 = 2
	javaTextConstraintEmailAddress int32 = 3
	javaTextConstraintURL          int32 = 4
	javaTextConstraintPhoneNumber  int32 = 5
)

// TextInput snapshots the active Java or C editor for a Host that composes a
// whole string with its native keyboard or IME. Session serializes this call,
// guest ticks and Commit because the ARM guest is not re-entrant.
func (session *Session) TextInput(ctx context.Context) (*backend.TextInput, error) {
	if session == nil || session.client == nil {
		return nil, backend.ErrNoTextInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if session.client.javaRun == nil {
		edit := session.cTextInput()
		if edit == nil {
			return nil, backend.ErrNoTextInput
		}
		return edit, nil
	}
	return session.javaTextInput()
}

func (session *Session) javaTextInput() (*backend.TextInput, error) {
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

const maxCTextInputLength = 64

// cTextInput bridges a game-owned WIPI-C widget through MC_imHandleInput. C
// exposes neither the widget's value nor its cursor, so the Host truthfully
// offers an append operation: one complete, OS-composed string is returned in
// the widget's normal completion buffer at its current cursor.
func (session *Session) cTextInput() *backend.TextInput {
	client := session.client
	state := &client.cTextInput
	if !state.active || client.clet.HandleEvent == 0 {
		return nil
	}
	revision, mode, handler := state.revision, client.inputMode, client.clet.HandleEvent
	// MC_imSetCurrentMode selects the handset keypad automaton, not a
	// widget validation constraint. A name widget can select N123 while
	// holding Korean text. The Host IME supplies completed text independently
	// of that keypad mode; the guest widget still owns field validation.
	return &backend.TextInput{
		MaxLength: maxCTextInputLength,
		Append:    true,
		InputMode: "text",
		Commit: func(ctx context.Context, text string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !state.active || state.revision != revision || client.inputMode != mode ||
				client.clet.HandleEvent != handler || len(state.pending) != 0 {
				return backend.ErrTextInputChanged
			}
			encoded, err := validateCTextInput(text)
			if err != nil {
				return err
			}
			if len(encoded) == 0 {
				return nil
			}

			calls := state.calls
			state.pending = encoded
			for len(state.pending) != 0 {
				before := len(state.pending)
				err = client.callClet(ctx, "handleCletEvent", handler,
					[]uint32{EventKeyPressed, hostTextInputCarrier, 0})
				if err != nil || len(state.pending) >= before || state.revision != revision {
					break
				}
			}
			consumed := len(state.pending) == 0
			state.pending = nil
			if err != nil {
				return err
			}
			if consumed {
				return nil
			}
			if state.calls == calls || state.revision != revision {
				return backend.ErrTextInputChanged
			}
			// The widget reached MC_imHandleInput but its completion buffer
			// could not hold the whole value. Nothing was inserted, so the
			// same edit can be retried with a shorter string.
			return backend.ErrInvalidTextInput
		},
	}
}

func validateCTextInput(text string) ([]byte, error) {
	if err := backend.ValidateTextInput(text); err != nil ||
		utf8.RuneCountInString(text) > maxCTextInputLength {
		return nil, backend.ErrInvalidTextInput
	}
	for _, symbol := range text {
		if symbol == 0 || unicode.IsControl(symbol) {
			return nil, backend.ErrInvalidTextInput
		}
	}
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return nil, backend.ErrInvalidTextInput
	}
	return encoded, nil
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
