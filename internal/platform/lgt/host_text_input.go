package lgt

import (
	"context"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"

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
	target := client.javaTextInputTarget()
	state := runtime.widgets[target]
	if target == 0 {
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
			if client.javaRun != runtime || client.javaTextInputTarget() != target ||
				runtime.widgetGeneration != generation || runtime.widgets[target] != state ||
				state.revision != revision || state.inputHandler != handler {
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
			// A card paints only when asked, and no key reached the title to
			// make it ask: the committed text stayed off screen until the
			// next press. Request the frame a changed component would.
			if runtime.card != 0 {
				runtime.cardDirty = true
			}
			return nil
		},
	}, nil
}

const maxCTextInputLength = 64

// cTextInput bridges a game-owned WIPI-C widget through MC_imHandleInput. C
// exposes neither the widget's value nor its cursor, so the Host truthfully
// offers an append operation: one complete, OS-composed string is returned in
// the widget's normal completion buffer at its current cursor.
//
// It is offered whenever the title has used the input method, on every
// screen. Nothing a Host can see says a widget is up: one local title calls
// the input method once while starting and then only from its name screen's
// key handler, so the key that opens that screen is handled without it and
// the empty field gets no call at all until the player presses another key.
// Following the keys made that field unavailable, and a field the player
// cannot type into is the failure that matters; an offer on a screen without
// one costs a commit that reports failure and a carrier key the game sees,
// once: the offer then waits for the input method to be called again or for
// the player to press a key (see cTextInputState.unreached).
func (session *Session) cTextInput() *backend.TextInput {
	client := session.client
	state := &client.cTextInput
	if !state.used || state.unreached || client.clet.HandleEvent == 0 {
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
			if !state.used || state.unreached || state.revision != revision || client.inputMode != mode ||
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
			// Only the guest runs until the deferred reset, so an input-method
			// call it makes is part of delivering this text.
			state.delivering = true
			delivered := false
			defer func() {
				state.pending = nil
				state.delivering = false
				if delivered {
					state.revision++
				}
			}()
			for len(state.pending) != 0 {
				before, reached := len(state.pending), state.calls
				err = client.callClet(ctx, "handleCletEvent", handler,
					[]uint32{EventKeyPressed, hostTextInputCarrier, 0})
				if err == nil && state.calls == reached {
					err = client.serviceDeferredCarrier(ctx, reached)
				}
				delivered = delivered || len(state.pending) < before
				if err != nil || len(state.pending) >= before || state.revision != revision ||
					client.inputMode != mode || client.clet.HandleEvent != handler {
					break
				}
			}
			consumed := len(state.pending) == 0
			if err != nil {
				return err
			}
			if consumed {
				return nil
			}
			if state.revision != revision || client.inputMode != mode || client.clet.HandleEvent != handler {
				return backend.ErrTextInputChanged
			}
			if delivered {
				// The widget took a prefix and stopped asking for more, which
				// is its own length limit at work: the keys a handset sent
				// past it would have gone nowhere too.
				return nil
			}
			if state.calls == calls {
				// The carrier went to the game as a key and never reached the
				// automaton: no field is taking keys on this screen. The game
				// had the press, so it gets the release a handset would send
				// after it rather than a key that stays down, and input waits
				// for new evidence of a field before it is offered again.
				state.unreached = true
				client.SendEvent(EventKeyReleased, hostTextInputCarrier, 0)
				return backend.ErrTextInputChanged
			}
			// The widget reached MC_imHandleInput but its completion buffer
			// could not hold even the first character. Nothing was inserted,
			// so the same edit can be retried.
			return backend.ErrInvalidTextInput
		},
	}
}

// Bounds on waiting for a widget that reads its keys in a timer.
const (
	maxDeferredCarrierRounds = 8
	maxDeferredCarrierWait   = time.Second
)

// serviceDeferredCarrier runs the Clet's timers after a carrier event that did
// not reach the input method. One local title queues the keys it is handed and
// reads them in its frame timer: the carrier reached the automaton only after
// the commit had returned, and the title took it as the digit it is. The KTF
// C route waits for such a timer in the same way. Timers fire in their own
// order at their own deadlines, so this is the next frames of the game run
// early; it stops at the first input-method call, or when nothing is armed
// within the bound and the carrier stays a key the title did not route.
func (client *Client) serviceDeferredCarrier(ctx context.Context, reached uint64) error {
	state := &client.cTextInput
	var waited time.Duration
	for round := 0; round < maxDeferredCarrierRounds && state.calls == reached && len(state.pending) != 0; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		wait, armed := client.nextTimerDue()
		if !armed || waited+max(wait, 0) > maxDeferredCarrierWait {
			return nil
		}
		if wait > 0 {
			waited += client.clock.advance(wait)
		}
		if err := client.serviceTimers(ctx); err != nil {
			return err
		}
	}
	return nil
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
	encoded, ok := backend.EncodeKSC5601(text)
	if !ok {
		return nil, backend.ErrInvalidTextInput
	}
	return encoded, nil
}

// javaTextInputTarget is the text component a Host edit goes to. The focused
// one comes first, whenever it can take the text. Without one, the field the
// screen holds is still the field the player means: a title can build its name
// dialog without ever giving the field focus, or tell it focus has gone while
// the dialog stays up, and a field that cannot be typed into is the failure
// that matters. So the only text component under a shown shell is taken, or,
// when no shell shows one, the only one that was never placed in a shell at
// all and that the card on the screen read while it last painted text: a
// field the title draws itself is read there, and one it built and never
// drew, or dropped and has not yet had collected, is not. Two candidates at
// the same level are two fields the Host cannot choose between, and get
// nothing.
//
// A focused field on the screen that cannot take the text — it has a
// composition listener — is still the field the player is in, and no field
// beside it is offered in its place. A focus left on a field under a hidden
// shell, or in a container this platform does not know, is no evidence of
// where the player is, and the question goes on.
func (client *Client) javaTextInputTarget() uint32 {
	runtime := client.javaRun
	if focused := runtime.focusedWidget; focused != 0 {
		state := runtime.widgets[focused]
		if client.javaTextInputAvailable(focused, state) {
			return focused
		}
		if javaTextComponent(state) && client.javaTextPlacement(focused) != javaTextUnplaced {
			return 0
		}
	}
	drawn := runtime.paintedTextCard != 0 && runtime.paintedTextCard == runtime.card
	// Level 0 is under a shown shell and level 1 drawn by the title: one
	// candidate of each, and how many each level had.
	var candidate [2]uint32
	var found [2]int
	for object, state := range runtime.widgets {
		if !client.javaTextInputAvailable(object, state) {
			continue
		}
		level := 1
		if client.javaTextPlacement(object) == javaTextUnderShownShell {
			level = 0
		} else if !drawn || !slices.Contains(runtime.paintedText, object) {
			continue
		}
		candidate[level] = object
		found[level]++
	}
	switch {
	case found[0] == 1:
		return candidate[0]
	case found[0] == 0 && found[1] == 1:
		return candidate[1]
	}
	return 0
}

// javaTextPlacement says where a text component sits for a Host edit. LGT
// draws no LWC component, so a shell's visibility is only what the title told
// it; a component whose parent chain ends without reaching a shell is one the
// title draws itself, the way a field that never joined a container is on
// KTF. A chain through a hidden shell, an unknown object or a cycle places the
// component nowhere.
type javaTextPlace uint8

const (
	javaTextUnplaced javaTextPlace = iota
	javaTextUnderShownShell
	javaTextDrawnByTitle
)

func (client *Client) javaTextPlacement(object uint32) javaTextPlace {
	runtime := client.javaRuntimeState()
	seen := map[uint32]bool{}
	for depth := 0; object != 0 && depth < maxWidgetDepth; depth++ {
		if seen[object] {
			return javaTextUnplaced
		}
		seen[object] = true
		state := runtime.widgets[object]
		if state == nil {
			return javaTextUnplaced
		}
		if state.kind == javaWidgetShell {
			if state.shown {
				return javaTextUnderShownShell
			}
			return javaTextUnplaced
		}
		if state.parent == 0 {
			return javaTextDrawnByTitle
		}
		object = state.parent
	}
	return javaTextUnplaced
}

// maxPaintTextReads bounds how many distinct text components one paint is
// tracked for. A paint that reads more is not a name screen anyone could type
// into, and it empties the set rather than tracking the rest.
const maxPaintTextReads = 16

// beginTextReads starts collecting the text components a card paint reads.
func (runtime *javaRuntime) beginTextReads() {
	runtime.painting = true
	runtime.paintTextReads, runtime.paintTextOverflow = nil, false
}

// endTextReads publishes what a paint of card read once it returned. A paint
// that failed says nothing, and neither does one that read no text: a frame
// that does not draw a field is no evidence the frame before did not. A paint
// that read more than a name screen does empties the set.
func (runtime *javaRuntime) endTextReads(card uint32, painted bool) {
	switch {
	case painted && runtime.paintTextOverflow:
		runtime.paintedText, runtime.paintedTextCard = nil, 0
	case painted && len(runtime.paintTextReads) > 0:
		runtime.paintedText, runtime.paintedTextCard = runtime.paintTextReads, card
	}
	runtime.painting = false
	runtime.paintTextReads, runtime.paintTextOverflow = nil, false
}

// noteTextRead records a text component read while the card paints.
func (runtime *javaRuntime) noteTextRead(object uint32) {
	if !runtime.painting || runtime.paintTextOverflow || slices.Contains(runtime.paintTextReads, object) {
		return
	}
	if len(runtime.paintTextReads) == maxPaintTextReads {
		runtime.paintTextOverflow = true
		return
	}
	runtime.paintTextReads = append(runtime.paintTextReads, object)
}

// forgetTextReads drops collected objects from the painted set, so that a
// handle reused for a new object is not taken for the field that was read.
func (runtime *javaRuntime) forgetTextReads(dead []uint32) {
	if len(runtime.paintedText) == 0 {
		return
	}
	runtime.paintedText = slices.DeleteFunc(runtime.paintedText, func(object uint32) bool { return slices.Contains(dead, object) })
}

// javaTextComponent reports whether a widget is a text field or a text box.
func javaTextComponent(state *javaWidget) bool {
	return state != nil && (state.kind == javaWidgetTextField || state.kind == javaWidgetTextBox)
}

// A listener changes the callback contract of a commit. The current LGT
// corpus installs none, and this runtime cannot yet enter notifyTextChanged
// safely from a Host commit, so such a field is refused instead of updated
// without the callback the guest requested.
func (client *Client) javaTextInputAvailable(target uint32, state *javaWidget) bool {
	if !javaTextComponent(state) || client.javaTextPlacement(target) == javaTextUnplaced {
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
	if _, ok := backend.EncodeKSC5601(text); !ok {
		return backend.ErrInvalidTextInput
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
