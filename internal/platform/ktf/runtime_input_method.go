package ktf

import (
	"context"
	"fmt"
	"unicode"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// A title that draws its own text field builds an InputMethodHandler, gives it
// a listener of its own, and hands it the keys the field's card receives while
// the field is open. The handler is the automaton: it turns keys into
// characters and tells the listener what each one did to the text. **The
// listener holds the text and the handler never reads it**, so what crosses
// between them is an edit, not a value.
//
// Which edits a listener expects is settled by three local titles rather than
// by the specification, which names the three modes of notifyTextChanged and
// nothing about how a listener applies them. All three implement it the same
// way: an insertion (-1) appends `new String(chText)` — the whole array, not
// its first `len` characters — a deletion (1) cuts `len` characters off the
// end, and a replacement (0) is ignored. So the handler only ever inserts and
// deletes, and an insertion's array holds exactly the inserted characters.
//
// The text itself comes from the Host's text input, as it does for every other
// field here. The handset composed letters and Hangul in its automaton; here
// the browser or the operating system composes them, and the completed text is
// handed to the listener one character per key the title forwards, so the
// title's own handling of each key — its length check, its redraw — runs as it
// would for a character typed on the handset. The keypad keeps what the WIPI C
// input method keeps: a digit in the digit mode, and the clear key.

const (
	// Edits as notifyTextChanged names them.
	inputMethodInsert int32 = -1
	inputMethodDelete int32 = 1

	inputMethodModeField = "mode:I"
	// inputMethodConstraintField is the constraint the handler was built with,
	// which decides the characters a numeric field can take whatever mode a
	// title later sets.
	inputMethodConstraintField = "host:input-constraint"
	// inputMethodOwnerField is the text component a handler was built for,
	// absent on one a title constructed itself. A mode the handler is given
	// is that component's iMode.
	inputMethodOwnerField = "host:input-owner"
	// inputMethodTitleOwnedField marks a handler the title constructed itself.
	// Only such a handler is a field of the title's own; the one a text
	// component carries is reached through the component.
	inputMethodTitleOwnedField = "host:input-title-owned"
	// inputMethodResetField marks a field the title opened while handling a
	// key another of its fields took, and that has not taken a key itself
	// since; see touchInputMethod.
	inputMethodResetField = "host:input-reset"

	// The open field, and the card it was opened on, are runtime objects so
	// that a checkpoint taken while the field is open restores it open.
	inputMethodOpenObject = "input-method:open"
	inputMethodCardObject = "input-method:card"

	// inputMethodCarrier is the key a Host character travels on. The titles
	// forward every key of the pad to their open field, and a digit is the key
	// the WIPI C input method's Host text travels on too.
	inputMethodCarrier = KeyNum0
	// maxInputMethodHostUnits bounds one Host composition, in Java chars. The
	// field's own limit is the title's, and it applies it one key at a time.
	maxInputMethodHostUnits = 64
)

// Modes as a title sets them on its handler. The specification names the
// modes without their numbers; one local title settles them. It draws its own
// indicator — 가, A, a, 1 — opens its field under 가 with setCurrentMode(3),
// and its mode key steps the indicator through A, a and 1 with 1, 0 and 2.
// These are not the WIPI C input method's numbers, whose list starts with the
// capitals. Only the digit mode changes what a key does here.
const (
	inputMethodModeLowercase int32 = 0
	inputMethodModeUppercase int32 = 1
	inputMethodModeNumeric   int32 = 2
	inputMethodModeHangul    int32 = 3
)

// javaInputState is the Host's side of a title's own field: whether the press
// being delivered reached the open handler, and whether a field took it as a
// key; the revision a Host composition was taken at; and the character in
// flight to the listener.
type javaInputState struct {
	touched, keyTaken bool
	revision          uint64
	pending           *javaInputCharacter
}

type javaInputCharacter struct {
	handler *jvm.Object
	units   []uint16
}

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
	listener, err := inputMethodListener(receiver)
	if err != nil || listener == nil {
		return jvm.IntValue(0), err
	}
	// The character in flight is delivered to the handler it was composed for,
	// whatever key carried it; a carrier forwarded anywhere else is left for
	// the Host to find undelivered.
	if pending := runtime.javaInput.pending; pending != nil && pending.handler == receiver {
		runtime.javaInput.pending = nil
		runtime.touchInputMethod(receiver, true)
		if err := notifyInputMethodListener(vm, listener, pending.units, inputMethodInsert); err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(1), nil
	}
	// The title hands its field's keys here, so the field is open.
	runtime.touchInputMethod(receiver, true)
	// A press types; a release and a repeat do not.
	if eventType != KeyPressed {
		return jvm.IntValue(0), nil
	}
	switch {
	case key == KeyClear:
		// The title forwards clear while its field has text — the first title
		// takes it as cancel when the field is empty — and a deletion is the
		// edit its listener applies to its own text.
		if err := notifyInputMethodListener(vm, listener, nil, inputMethodDelete); err != nil {
			return jvm.VoidValue(), err
		}
	case key >= KeyNum0 && key <= KeyNum9 && inputMethodTypesDigits(receiver):
		if err := notifyInputMethodListener(vm, listener, []uint16{uint16(key)}, inputMethodInsert); err != nil {
			return jvm.VoidValue(), err
		}
	default:
		return jvm.IntValue(0), nil
	}
	return jvm.IntValue(1), nil
}

// inputMethodTypesDigits answers whether a digit key types its digit: in the
// digit mode, or whatever the mode on a field built for numbers.
func inputMethodTypesDigits(receiver *jvm.Object) bool {
	switch constraint, _ := receiver.Fields[inputMethodConstraintField].Int32(); constraint {
	case textConstraintNumber, textConstraintPassword, textConstraintPhoneNumber:
		return true
	}
	mode, err := receiver.Fields[inputMethodModeField].Int32()
	return err == nil && mode == inputMethodModeNumeric
}

func inputMethodListener(receiver *jvm.Object) (*jvm.Object, error) {
	value, ok := receiver.Fields[inputMethodListenerField]
	if !ok {
		return nil, nil
	}
	return value.Reference()
}

// notifyInputMethodListener hands one edit to the listener: the characters,
// how many there are, and which edit it is. A deletion's array is empty,
// because the listeners that settled the contract read only its count.
func notifyInputMethodListener(vm *jvm.VM, listener *jvm.Object, units []uint16, edit int32) error {
	length := len(units)
	if edit == inputMethodDelete {
		length = 1
	}
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
		jvm.ReferenceValue(array), jvm.IntValue(int32(length)), jvm.IntValue(edit)); err != nil {
		return fmt.Errorf("notify KTF input method listener %s: %w", listener.ClassName, err)
	}
	return nil
}

// touchInputMethod records that the title used one of its own handlers while
// that handler has a listener, which is what an open field looks like from
// here: the first title builds a handler and registers its listener when the
// field opens, hands that handler every key of the pad while it is open, and
// sets its mode from a soft key. It marks the press being delivered as one the
// field saw; see dispatchKeyToCards. took says the handler was handed a key.
//
// A field that opens while another field is taking a key is that field reset.
// The first title does it when a fifth character overflows its four: it builds
// the field again and puts its "no more than four characters" message over
// it. The press that dismisses the message never reaches the new field, which
// is on the screen all the same, so a reset field stays open through presses
// it does not see until it has taken a key of its own.
func (runtime *initializationRuntime) touchInputMethod(handler *jvm.Object, took bool) {
	if owned, _ := handler.Fields[inputMethodTitleOwnedField].Int32(); owned == 0 {
		return
	}
	if listener, err := inputMethodListener(handler); err != nil || listener == nil {
		return
	}
	runtime.javaInput.touched = true
	if took {
		runtime.javaInput.keyTaken = true
		delete(handler.Fields, inputMethodResetField)
	}
	card := runtime.topCard()
	open := runtime.runtimeObjects[inputMethodOpenObject]
	if open == handler && runtime.runtimeObjects[inputMethodCardObject] == card {
		return
	}
	if open != handler && !took && runtime.javaInput.keyTaken {
		handler.Fields[inputMethodResetField] = jvm.IntValue(1)
	}
	runtime.runtimeObjects[inputMethodOpenObject] = handler
	if card != nil {
		runtime.runtimeObjects[inputMethodCardObject] = card
	} else {
		delete(runtime.runtimeObjects, inputMethodCardObject)
	}
	runtime.javaInput.revision++
}

// closeInputMethod forgets the open field.
func (runtime *initializationRuntime) closeInputMethod() {
	if runtime.runtimeObjects[inputMethodOpenObject] == nil {
		return
	}
	delete(runtime.runtimeObjects, inputMethodOpenObject)
	delete(runtime.runtimeObjects, inputMethodCardObject)
	runtime.javaInput.revision++
}

// unseenPress closes the open field after a press it did not see. That is the
// sign the title closed it: the first title closes its field on fire, and on
// clear when the field is empty, and neither key reaches the handler. A reset
// field that has not taken a key yet is the exception; see touchInputMethod.
func (runtime *initializationRuntime) unseenPress() {
	open := runtime.runtimeObjects[inputMethodOpenObject]
	if open == nil {
		return
	}
	if reset, _ := open.Fields[inputMethodResetField].Int32(); reset != 0 {
		return
	}
	runtime.closeInputMethod()
}

// runtimeInputMethodSetListener keeps the listener the handler's edits go to.
// Registering one on a handler the title built is the field opening; removing
// it is the field closing.
func runtimeInputMethodSetListener(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	result, err := runtimeComponentSetField("InputMethodHandler.setInputMethodListener", inputMethodListenerField)(runtime, vm, arguments)
	if err != nil {
		return result, err
	}
	receiver, _ := arguments[0].Reference()
	if listener, _ := arguments[1].Reference(); listener != nil {
		runtime.touchInputMethod(receiver, false)
	} else if runtime.runtimeObjects[inputMethodOpenObject] == receiver {
		runtime.closeInputMethod()
	}
	return result, nil
}

// inputMethodTextInputLocked offers the open field of a title's own handler to
// a Host that composes text. The field cannot be read, so the Host appends.
// The caller holds client.run.
func (client *Client) inputMethodTextInputLocked() (*backend.TextInput, error) {
	runtime := client.runtime
	handler := runtime.runtimeObjects[inputMethodOpenObject]
	card := runtime.runtimeObjects[inputMethodCardObject]
	// A title that runs its own event loop takes keys from its queue in its
	// own time, so whether a carrier was consumed is unknown; the C input
	// method leaves such a title out for the same reason.
	if handler == nil || card == nil || runtime.guestEventLoop || runtime.topCard() != card {
		return nil, backend.ErrNoTextInput
	}
	listenerValue := handler.Fields[inputMethodListenerField]
	if listener, err := listenerValue.Reference(); err != nil || listener == nil {
		return nil, backend.ErrNoTextInput
	}
	constraint, err := handler.Fields[inputMethodConstraintField].Int32()
	if err != nil {
		constraint = textConstraintAny
	}
	inputMode, password := lwcTextInputHints(constraint)
	revision := runtime.javaInput.revision
	return &backend.TextInput{Append: true, MaxLength: maxInputMethodHostUnits, InputMode: inputMode, Password: password,
		Commit: func(ctx context.Context, text string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			client.run.Lock()
			defer client.run.Unlock()
			unchanged := func() bool {
				return client.runtime == runtime && !client.workersStopped && !runtime.guestEventLoop &&
					runtime.runtimeObjects[inputMethodOpenObject] == handler && runtime.topCard() == card &&
					runtime.javaInput.revision == revision && handler.Fields[inputMethodListenerField] == listenerValue
			}
			if !unchanged() {
				return backend.ErrTextInputChanged
			}
			if err := validateInputMethodText(text, constraint); err != nil {
				return err
			}
			if text == "" {
				return nil
			}
			delivered := false
			defer func() {
				runtime.javaInput.pending = nil
				// A used composition is spent, as the C input method's is.
				if delivered {
					runtime.javaInput.revision++
				}
			}()
			defer client.beginHostService(ctx)()
			previousThread, previousContext := runtime.currentThread, runtime.currentContext
			runtime.currentThread, runtime.currentContext = client.thread, ctx
			defer func() { runtime.currentThread, runtime.currentContext = previousThread, previousContext }()
			for _, character := range text {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !unchanged() {
					return backend.ErrTextInputChanged
				}
				runtime.javaInput.pending = &javaInputCharacter{handler: handler, units: utf16.Encode([]rune{character})}
				runtime.javaInput.keyTaken = false
				err := runtime.dispatchKeyToCards(KeyPressed, inputMethodCarrier)
				consumed := runtime.javaInput.pending == nil
				runtime.javaInput.pending, runtime.javaInput.keyTaken = nil, false
				if err == nil {
					err = runtime.dispatchKeyToCards(KeyReleased, inputMethodCarrier)
				}
				if err != nil {
					return err
				}
				if !consumed {
					// The title did not hand the carrier to this field, which is
					// a press the field did not see.
					runtime.unseenPress()
					return backend.ErrTextInputChanged
				}
				delivered = true
			}
			return nil
		},
	}, nil
}

// validateInputMethodText applies the field's constraint the way a Java text
// field's Host input does, and refuses the control characters no keypad could
// have typed.
func validateInputMethodText(text string, constraint int32) error {
	if err := validateLWCTextInput(text, constraint, maxInputMethodHostUnits, false); err != nil {
		return err
	}
	for _, character := range text {
		if unicode.IsControl(character) {
			return backend.ErrInvalidTextInput
		}
	}
	return nil
}
