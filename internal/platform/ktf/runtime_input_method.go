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
	// The offered field is the open one as the second pass of a Host snapshot
	// sees it: opened the same way, and closed only when the title takes its
	// listener away. A press the field misses closes the open one, which is
	// how the first title's confirmation looks — but a field still on the
	// screen after such a press is then one nobody can type into, while
	// offering a field that did close costs only a commit that fails. See
	// unseenPress.
	inputMethodOfferedObject     = "input-method:offered"
	inputMethodOfferedCardObject = "input-method:offered-card"

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
	if pending := runtime.javaInput.pending; pending != nil {
		if pending.handler != receiver {
			// The title hands its keys to this field now, so this is the
			// field that is open. The carrier is the Host's and types nothing
			// here: it was no key the player pressed, and the character it
			// carries was composed for another field. Processed, so the title
			// does not take it as a key of its own either.
			runtime.touchInputMethod(receiver, true)
			return jvm.IntValue(1), nil
		}
		// Taken once the array that carries it exists: making the array can
		// be refused, and the character stays for the next key rather than
		// being lost with the refusal.
		taken := func() {
			runtime.javaInput.pending = nil
			runtime.touchInputMethod(receiver, true)
		}
		if err := notifyInputMethodListenerAfter(vm, listener, pending.units, inputMethodInsert, taken); err != nil {
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

// hostCarrierInFlight reports whether the press being delivered carries a Host
// character, for a title's own field or for the C input method, rather than
// being a key the player pressed.
func (runtime *initializationRuntime) hostCarrierInFlight() bool {
	return runtime.javaInput.pending != nil || len(runtime.cInput.pending) != 0
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
	return notifyInputMethodListenerAfter(vm, listener, units, edit, nil)
}

// notifyInputMethodListenerAfter is notifyInputMethodListener that calls taken,
// when it is set, once the array exists and before the listener has it.
func notifyInputMethodListenerAfter(vm *jvm.VM, listener *jvm.Object, units []uint16, edit int32, taken func()) error {
	length := len(units)
	if edit == inputMethodDelete {
		length = 1
	}
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeChar}, int32(len(units)))
	if err != nil {
		return err
	}
	if taken != nil {
		taken()
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
// The field it records is both the open one and the offered one; see
// inputMethodOfferedObject.
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
	if open == handler && runtime.runtimeObjects[inputMethodCardObject] == card &&
		runtime.runtimeObjects[inputMethodOfferedObject] == handler && runtime.runtimeObjects[inputMethodOfferedCardObject] == card {
		return
	}
	if open != handler && !took && runtime.javaInput.keyTaken {
		handler.Fields[inputMethodResetField] = jvm.IntValue(1)
	}
	offered, offeredCard := runtime.offeredInputMethod()
	runtime.runtimeObjects[inputMethodOpenObject] = handler
	runtime.runtimeObjects[inputMethodOfferedObject] = handler
	if card != nil {
		runtime.runtimeObjects[inputMethodCardObject] = card
		runtime.runtimeObjects[inputMethodOfferedCardObject] = card
	} else {
		delete(runtime.runtimeObjects, inputMethodCardObject)
		delete(runtime.runtimeObjects, inputMethodOfferedCardObject)
	}
	// Opening again the field already offered on this card changes nothing a
	// composition was taken against: it is the same field, and the press that
	// opens it may be a Host character reaching it. Spending the composition
	// there would end a commit after its first character.
	if offered != handler || offeredCard != card {
		runtime.javaInput.revision++
	}
}

// closeInputMethod forgets the open field. The offered one stays; see
// inputMethodOfferedObject. A checkpoint taken before there was an offered
// field holds only the open one, which is recorded as offered here, before
// the open record that stands in for it is gone.
func (runtime *initializationRuntime) closeInputMethod() {
	open := runtime.runtimeObjects[inputMethodOpenObject]
	if open == nil {
		return
	}
	if runtime.runtimeObjects[inputMethodOfferedObject] == nil {
		runtime.runtimeObjects[inputMethodOfferedObject] = open
		if card := runtime.runtimeObjects[inputMethodCardObject]; card != nil {
			runtime.runtimeObjects[inputMethodOfferedCardObject] = card
		}
	}
	delete(runtime.runtimeObjects, inputMethodOpenObject)
	delete(runtime.runtimeObjects, inputMethodCardObject)
	runtime.javaInput.revision++
}

// withdrawInputMethod forgets the offered field, once the title has taken the
// listener of that handler away.
func (runtime *initializationRuntime) withdrawInputMethod(handler *jvm.Object) {
	if offered, _ := runtime.offeredInputMethod(); offered == nil || offered != handler {
		return
	}
	delete(runtime.runtimeObjects, inputMethodOfferedObject)
	delete(runtime.runtimeObjects, inputMethodOfferedCardObject)
	runtime.javaInput.revision++
}

// offeredInputMethod answers the offered field and its card. A checkpoint
// taken before there was an offered field restores only the open one, which
// is the same field.
func (runtime *initializationRuntime) offeredInputMethod() (handler, card *jvm.Object) {
	if handler := runtime.runtimeObjects[inputMethodOfferedObject]; handler != nil {
		return handler, runtime.runtimeObjects[inputMethodOfferedCardObject]
	}
	return runtime.runtimeObjects[inputMethodOpenObject], runtime.runtimeObjects[inputMethodCardObject]
}

// unseenPress closes the open field after a press it did not see. That is the
// sign the title closed it: the first title closes its field on fire, and on
// clear when the field is empty, and neither key reaches the handler. A reset
// field that has not taken a key yet is the exception; see touchInputMethod.
//
// The offered field stays, but a composition taken before the press is spent
// either way: whatever the press did to the screen happened after it.
func (runtime *initializationRuntime) unseenPress() {
	open := runtime.runtimeObjects[inputMethodOpenObject]
	if offered, _ := runtime.offeredInputMethod(); offered == nil && open == nil {
		return
	}
	runtime.javaInput.revision++
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
// it is the field closing, for the offered field as well as the open one.
func runtimeInputMethodSetListener(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	result, err := runtimeComponentSetField("InputMethodHandler.setInputMethodListener", inputMethodListenerField)(runtime, vm, arguments)
	if err != nil {
		return result, err
	}
	receiver, _ := arguments[0].Reference()
	if listener, _ := arguments[1].Reference(); listener != nil {
		runtime.touchInputMethod(receiver, false)
	} else if receiver != nil {
		if runtime.runtimeObjects[inputMethodOpenObject] == receiver {
			runtime.closeInputMethod()
		}
		runtime.withdrawInputMethod(receiver)
	}
	return result, nil
}

// titleFieldOffered decides whether a title's own field can take a Host
// composition: the open field under the first rules, the offered one under
// the relaxed ones, either way on the card it was opened on while that card
// is on top. A title that runs its own event loop takes keys from its queue
// in its own time, so whether a carrier was consumed is unknown; the C input
// method leaves such a title out for the same reason.
func (client *Client) titleFieldOffered(handler, card *jvm.Object, relaxed bool) bool {
	runtime := client.runtime
	if handler == nil || card == nil || runtime.guestEventLoop || runtime.topCard() != card {
		return false
	}
	// A vendor form that owns the keypad takes every press, so a carrier
	// would reach the form rather than the field. The open field has always
	// been offered regardless; the offered one is not.
	if relaxed {
		if _, form := runtime.activeVendorTextInput(); form {
			return false
		}
	}
	listener, err := handler.Fields[inputMethodListenerField].Reference()
	return err == nil && listener != nil
}

// titleFieldTextInput offers a title's own field to a Host that composes
// text. The field cannot be read, so the Host appends. The caller holds
// client.run.
func (client *Client) titleFieldTextInput(target textInputTarget) *backend.TextInput {
	runtime := client.runtime
	handler := target.handler
	listenerValue := handler.Fields[inputMethodListenerField]
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
			// The field is still the one a snapshot would choose, on its card,
			// and nothing a press did has spent the composition.
			unchanged := func() bool {
				if client.runtime != runtime || client.workersStopped ||
					runtime.javaInput.revision != revision || handler.Fields[inputMethodListenerField] != listenerValue {
					return false
				}
				current, ok := client.resolveTextInputLocked(textInputResolution{})
				return ok && current.identifies(target)
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
				// The carrier is a key, and what keyNotify throws and does not
				// catch ends that key here as it does for SendKey.
				err := client.absorbUncaughtCallback("key", runtime.dispatchKeyToCards(KeyPressed, inputMethodCarrier))
				consumed := runtime.javaInput.pending == nil
				runtime.javaInput.pending, runtime.javaInput.keyTaken = nil, false
				if err == nil {
					err = client.absorbUncaughtCallback("key", runtime.dispatchKeyToCards(KeyReleased, inputMethodCarrier))
				}
				if err != nil {
					return err
				}
				if !consumed {
					// The title did not hand the carrier to this field, which is
					// a press the field did not see: it closes the open field and
					// spends this composition. A title whose field is still on
					// its screen hands that field every key, the carrier
					// included, so the field also stops being offered — a
					// handler the title has moved on from would otherwise be
					// offered, and fail, at every commit. A reset field behind
					// its message stays open and offered, and a field the title
					// handed the carrier to instead is the one offered now; see
					// runtimeInputMethodNotifyKeyInput.
					if offered, _ := runtime.offeredInputMethod(); offered == handler {
						runtime.unseenPress()
						if runtime.runtimeObjects[inputMethodOpenObject] != handler {
							runtime.withdrawInputMethod(handler)
						}
					}
					return backend.ErrTextInputChanged
				}
				delivered = true
			}
			return nil
		},
	}
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
