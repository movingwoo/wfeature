package ktf

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// inputMethodEdit is one notifyTextChanged call as a listener receives it.
type inputMethodEdit struct {
	text   string
	length int32
	edit   int32
}

// recordingInputMethodListener stands in for a title's listener: it keeps every
// edit it is handed, the way the titles that settled the contract keep their
// own text.
func recordingInputMethodListener(t *testing.T, client *Client, class string) (*jvm.Object, *[]inputMethodEdit) {
	t.Helper()
	edits := &[]inputMethodEdit{}
	err := client.JVM().RegisterNative(class, "notifyTextChanged", "([CII)V", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		array, err := arguments[1].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, values, err := jvm.ArraySnapshot(array)
		if err != nil {
			return jvm.VoidValue(), err
		}
		units := make([]uint16, len(values))
		for index, value := range values {
			unit, err := value.Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			units[index] = uint16(unit)
		}
		length, err := arguments[2].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		edit, err := arguments[3].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		*edits = append(*edits, inputMethodEdit{text: string(utf16.Decode(units)), length: length, edit: edit})
		return jvm.VoidValue(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return &jvm.Object{ClassName: class}, edits
}

// newGuestInputMethodHandler builds a handler the way a title does: a guest
// `new`, its constructor, and the listener it registers.
func newGuestInputMethodHandler(t *testing.T, client *Client, runtime *initializationRuntime, constraint int32, listener *jvm.Object) *jvm.Object {
	t.Helper()
	class, err := runtime.ensureJavaClass(runtimeInputMethodHandlerClass)
	if err != nil {
		t.Fatal(err)
	}
	_, handler, err := runtime.allocateAOTInstance(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.vm.InvokeVirtual(handler, "<init>", "(I)V", jvm.IntValue(constraint)); err != nil {
		t.Fatal(err)
	}
	if listener != nil {
		setGuestInputMethodListener(t, client, handler, listener)
	}
	return handler
}

func setGuestInputMethodListener(t *testing.T, client *Client, handler, listener *jvm.Object) {
	t.Helper()
	if _, err := client.vm.InvokeVirtual(handler, "setInputMethodListener", "(Lorg/kwis/msp/lcdui/InputMethodListener;)V", jvm.ReferenceValue(listener)); err != nil {
		t.Fatal(err)
	}
}

func setGuestInputMethodMode(t *testing.T, client *Client, handler *jvm.Object, mode int32) {
	t.Helper()
	if _, err := client.vm.InvokeVirtual(handler, "setCurrentMode", "(I)Z", jvm.IntValue(mode)); err != nil {
		t.Fatal(err)
	}
}

func notifyGuestKey(t *testing.T, client *Client, handler *jvm.Object, key, eventType int32) bool {
	t.Helper()
	result, err := client.vm.InvokeVirtual(handler, "notifyKeyInput", "(II)Z", jvm.IntValue(key), jvm.IntValue(eventType))
	if err != nil {
		t.Fatal(err)
	}
	processed, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return processed != 0
}

// nameFieldCard is a title's card with a name field of its own on it, built
// the way the first title builds one: while the field is open the card hands
// every press to the field's handler, except the soft key that switches its
// mode and fire, which confirms and closes it.
type nameFieldCard struct {
	card    *jvm.Object
	handler *jvm.Object
	open    bool
	// closeAfter closes the field once it has been handed this many presses.
	closeAfter int
	presses    int
	// resetAfter rebuilds the field with a new handler once it has been
	// handed this many presses and puts a message over it, the way the first
	// title answers a fifth character; any key but fire is the message's.
	resetAfter int
	reset      func() *jvm.Object
	message    bool
}

func newNameFieldCard(t *testing.T, client *Client, runtime *initializationRuntime, class string, handler *jvm.Object) *nameFieldCard {
	t.Helper()
	field := &nameFieldCard{card: &jvm.Object{ClassName: class}, handler: handler, open: true, closeAfter: -1, resetAfter: -1}
	err := client.JVM().RegisterNative(class, "keyNotify", "(II)Z", func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		eventType, _ := arguments[1].Int32()
		key, _ := arguments[2].Int32()
		if !field.open || eventType != wipicrt.KeyPressed {
			return jvm.IntValue(0), nil
		}
		if field.message {
			field.message = key != wipicrt.KeyFire
			return jvm.IntValue(0), nil
		}
		switch key {
		case wipicrt.KeyFire:
			field.open = false
			return jvm.IntValue(0), nil
		case wipicrt.KeyLeftSoft:
			mode, _ := field.handler.Fields[inputMethodModeField].Int32()
			_, err := vm.InvokeVirtual(field.handler, "setCurrentMode", "(I)Z", jvm.IntValue((mode+1)%4))
			return jvm.IntValue(0), err
		}
		if _, err := vm.InvokeVirtual(field.handler, "notifyKeyInput", "(II)Z", jvm.IntValue(key), jvm.IntValue(eventType)); err != nil {
			return jvm.VoidValue(), err
		}
		field.presses++
		if field.presses == field.closeAfter {
			field.open = false
		}
		if field.presses == field.resetAfter {
			field.handler, field.message = field.reset(), true
		}
		return jvm.IntValue(0), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = append(runtime.displayCards, field.card)
	return field
}

func pressCardKey(t *testing.T, runtime *initializationRuntime, key int32) {
	t.Helper()
	for _, eventType := range []int32{wipicrt.KeyPressed, wipicrt.KeyReleased} {
		if err := runtime.dispatchKeyToCards(eventType, key); err != nil {
			t.Fatal(err)
		}
	}
}

// The keypad keeps what the WIPI C input method keeps: a digit in the digit
// mode, and clear. Letters and Hangul are the Host text input's.
func TestInputMethodHandlerKeypadTypesDigitsAndClears(t *testing.T) {
	client, runtime := newTestRuntime(t)
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	setGuestInputMethodMode(t, client, handler, inputMethodModeNumeric)
	if !notifyGuestKey(t, client, handler, '7', wipicrt.KeyPressed) || !notifyGuestKey(t, client, handler, wipicrt.KeyClear, wipicrt.KeyPressed) {
		t.Fatal("a digit or clear in the digit mode was not processed")
	}
	for _, eventType := range []int32{wipicrt.KeyReleased, wipicrt.KeyRepeated} {
		if notifyGuestKey(t, client, handler, '7', eventType) {
			t.Fatalf("event type %d typed", eventType)
		}
	}
	for _, mode := range []int32{inputMethodModeLowercase, inputMethodModeUppercase, inputMethodModeHangul, 7} {
		setGuestInputMethodMode(t, client, handler, mode)
		if notifyGuestKey(t, client, handler, '2', wipicrt.KeyPressed) {
			t.Fatalf("a key in mode %d typed", mode)
		}
	}
	for _, key := range []int32{wipicrt.KeyStar, wipicrt.KeyHash, wipicrt.KeyUp, wipicrt.KeyFire, wipicrt.KeyLeftSoft} {
		if notifyGuestKey(t, client, handler, key, wipicrt.KeyPressed) {
			t.Fatalf("key %d was processed", key)
		}
	}
	numbers := newGuestInputMethodHandler(t, client, runtime, textConstraintNumber, listener)
	setGuestInputMethodMode(t, client, numbers, inputMethodModeUppercase)
	notifyGuestKey(t, client, numbers, '4', wipicrt.KeyPressed)
	want := []inputMethodEdit{{"7", 1, inputMethodInsert}, {"", 1, inputMethodDelete}, {"4", 1, inputMethodInsert}}
	if !reflect.DeepEqual(*edits, want) {
		t.Fatalf("edits = %+v, want %+v", *edits, want)
	}
}

// The specification's own rule: a handler with no listener processes nothing,
// and no field is open behind it.
func TestInputMethodHandlerWithoutAListenerProcessesNothing(t *testing.T) {
	client, runtime := newTestRuntime(t)
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, nil)
	setGuestInputMethodMode(t, client, handler, inputMethodModeNumeric)
	if notifyGuestKey(t, client, handler, '2', wipicrt.KeyPressed) || runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("a handler without a listener processed a key or opened a field")
	}
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	setGuestInputMethodListener(t, client, handler, listener)
	if runtime.runtimeObjects[inputMethodOpenObject] != handler {
		t.Fatal("registering a listener did not open the field")
	}
	setGuestInputMethodListener(t, client, handler, nil)
	if notifyGuestKey(t, client, handler, '2', wipicrt.KeyPressed) || len(*edits) != 0 || runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("a removed listener was still handed keys or kept the field open")
	}
}

// The field is open from the moment the title registers its listener, stays
// open while the presses the card receives reach the handler — a mode switch
// counts — and closes on the first press that does not. A release closes
// nothing, because a title hands only presses to its field.
//
// Closed that way the field is still offered, because a field on the screen
// that nobody can type into is the worse mistake. A title whose field is still
// on its screen hands it every key, the Host's carrier included, so a commit
// the field does not take shows it really closed: that commit fails without
// reaching the listener, and ends the offer until the title hands the field a
// key again. An edit taken before a press the field missed is spent by it. The
// title taking the listener away ends the offer too.
func TestATitleOwnedFieldStaysOfferedUntilItsListenerIsRemoved(t *testing.T) {
	client, runtime := newTestRuntime(t)
	session := &Session{Client: client}
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	field := newNameFieldCard(t, client, runtime, "test/NameCard", nil)
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	field.handler = handler
	setGuestInputMethodMode(t, client, handler, inputMethodModeHangul)

	available := func() bool {
		t.Helper()
		edit, err := session.TextInput(t.Context())
		if err != nil && !errors.Is(err, backend.ErrNoTextInput) {
			t.Fatal(err)
		}
		return err == nil && edit.Append && edit.InputMode == "text" && edit.MaxLength == maxInputMethodHostUnits
	}
	if !available() {
		t.Fatal("an open field offered no text input")
	}
	for _, key := range []int32{'2', wipicrt.KeyUp, wipicrt.KeyStar, wipicrt.KeyHash, wipicrt.KeyLeftSoft} {
		pressCardKey(t, runtime, key)
		if !available() {
			t.Fatalf("the field closed on key %d it took", key)
		}
	}
	before, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	pressCardKey(t, runtime, wipicrt.KeyFire)
	if runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("the field stayed open after fire closed it")
	}
	if !available() {
		t.Fatal("a field closed by a press it missed is no longer offered")
	}
	if err := before.Commit(t.Context(), "a"); !errors.Is(err, backend.ErrTextInputChanged) || len(*edits) != 0 {
		t.Fatalf("an edit taken before the press answered %v, edits %+v", err, *edits)
	}
	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "a"); !errors.Is(err, backend.ErrTextInputChanged) || len(*edits) != 0 {
		t.Fatalf("a commit into the closed field answered %v, edits %+v", err, *edits)
	}
	if available() {
		t.Fatal("a field that did not take the carrier is still offered")
	}
	// A title that opens its field again hands it a key, and the field is open
	// again.
	field.open = true
	pressCardKey(t, runtime, '5')
	if !available() || runtime.runtimeObjects[inputMethodOpenObject] != handler {
		t.Fatal("the reopened field offered no text input")
	}
	// A field on a card that is no longer on top is not on the screen.
	runtime.displayCards = append(runtime.displayCards, &jvm.Object{ClassName: "test/OtherCard"})
	if available() {
		t.Fatal("a field under another card offered text input")
	}
	runtime.displayCards = runtime.displayCards[:len(runtime.displayCards)-1]
	runtime.GuestEventLoop = true
	if available() {
		t.Fatal("a title running its own event loop offered text input")
	}
	runtime.GuestEventLoop = false
	// Closed and then stripped of its listener, the field is gone.
	pressCardKey(t, runtime, wipicrt.KeyFire)
	setGuestInputMethodListener(t, client, handler, nil)
	if available() {
		t.Fatal("a field whose listener was removed offered text input")
	}
}

// Host text reaches the title's listener one character per key the title
// forwards, as insertions, so the title handles each the way it handles a key.
func TestHostTextReachesATitleOwnedFieldThroughItsKeys(t *testing.T) {
	client, runtime := newTestRuntime(t)
	session := &Session{Client: client}
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	field := newNameFieldCard(t, client, runtime, "test/NameCard", nil)
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	field.handler = handler
	setGuestInputMethodMode(t, client, handler, inputMethodModeHangul)

	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"\x00", "\n", "a\tb", strings.Repeat("a", maxInputMethodHostUnits+1)} {
		if err := edit.Commit(t.Context(), bad); !errors.Is(err, backend.ErrInvalidTextInput) {
			t.Fatalf("invalid commit %q: %v", bad, err)
		}
	}
	if len(*edits) != 0 {
		t.Fatalf("rejected text reached the listener: %+v", *edits)
	}
	if err := edit.Commit(t.Context(), "홍길동"); err != nil {
		t.Fatal(err)
	}
	want := []inputMethodEdit{{"홍", 1, inputMethodInsert}, {"길", 1, inputMethodInsert}, {"동", 1, inputMethodInsert}}
	if !reflect.DeepEqual(*edits, want) || field.presses != 3 {
		t.Fatalf("edits = %+v after %d presses, want %+v", *edits, field.presses, want)
	}
	if err := edit.Commit(t.Context(), "a"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("a spent composition was reused: %v", err)
	}
	// Clear takes the last character back, whoever typed it.
	pressCardKey(t, runtime, wipicrt.KeyClear)
	if last := (*edits)[len(*edits)-1]; last != (inputMethodEdit{"", 1, inputMethodDelete}) {
		t.Fatalf("clear handed %+v", last)
	}

	// A field the title closes partway through takes what it was handed
	// before it closed, and the rest is refused.
	*edits = nil
	field.presses, field.closeAfter = 0, 2
	edit, err = session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "abc"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("a commit into a field that closed answered %v", err)
	}
	want = []inputMethodEdit{{"a", 1, inputMethodInsert}, {"b", 1, inputMethodInsert}}
	if !reflect.DeepEqual(*edits, want) || runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatalf("edits = %+v, open = %v", *edits, runtime.runtimeObjects[inputMethodOpenObject])
	}
	// The carrier the field did not take showed it closed, so it is no longer
	// offered although the title kept its listener.
	if _, err := session.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("a field that refused the carrier is still offered: %v", err)
	}
}

// A field the title rebuilds while handling a key — the first title's answer
// to a fifth character, with a message over the new field — is on the screen
// once the message is dismissed, although the dismissing press never reaches
// it. A field that opens any other way closes on the first press it misses.
func TestAResetFieldStaysOpenBehindItsMessage(t *testing.T) {
	client, runtime := newTestRuntime(t)
	session := &Session{Client: client}
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	field := newNameFieldCard(t, client, runtime, "test/NameCard", nil)
	field.handler = newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	field.resetAfter = 2
	field.reset = func() *jvm.Object {
		return newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	}
	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "abc"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("a commit across a reset answered %v", err)
	}
	reset := field.handler
	if runtime.runtimeObjects[inputMethodOpenObject] != reset || len(*edits) != 2 {
		t.Fatalf("after the reset: open %v, edits %+v", runtime.runtimeObjects[inputMethodOpenObject], *edits)
	}
	// Text committed while the message is up is the message's key, refused,
	// and the field behind it stays open.
	edit, err = session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "x"); !errors.Is(err, backend.ErrTextInputChanged) || !field.message {
		t.Fatalf("a commit into the message answered %v", err)
	}
	pressCardKey(t, runtime, wipicrt.KeyFire)
	if field.message || runtime.runtimeObjects[inputMethodOpenObject] != reset {
		t.Fatal("dismissing the message closed the reset field")
	}
	edit, err = session.TextInput(t.Context())
	if err != nil {
		t.Fatalf("the reset field offered no text input: %v", err)
	}
	if err := edit.Commit(t.Context(), "김"); err != nil {
		t.Fatal(err)
	}
	if last := (*edits)[len(*edits)-1]; last != (inputMethodEdit{"김", 1, inputMethodInsert}) {
		t.Fatalf("the reset field was handed %+v", last)
	}
	// Having taken a key, it closes like any other field.
	pressCardKey(t, runtime, wipicrt.KeyFire)
	if runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("the reset field stayed open after fire closed it")
	}

	// A field opened outside any key, and confirmed before it takes one, is
	// closed by that confirmation.
	field.open = true
	field.handler = newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	if runtime.runtimeObjects[inputMethodOpenObject] != field.handler {
		t.Fatal("the new field did not open")
	}
	pressCardKey(t, runtime, wipicrt.KeyFire)
	if runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("a field confirmed before taking a key stayed open")
	}
	// Closed, it is still the field offered: the title kept its listener.
	if offered, _ := runtime.offeredInputMethod(); offered != field.handler {
		t.Fatalf("offered field after the confirmation = %v", offered)
	}
	if _, err := session.TextInput(t.Context()); err != nil {
		t.Fatalf("the confirmed field is no longer offered: %v", err)
	}
}

// A checkpoint taken before the offered field existed restores only the open
// one, and the open one is offered in its place. Closed by a press it missed,
// it stays offered the way a field opened since is.
func TestTheOfferedFieldFallsBackToTheOpenOne(t *testing.T) {
	client, runtime := newTestRuntime(t)
	session := &Session{Client: client}
	listener, _ := recordingInputMethodListener(t, client, "test/NameListener")
	newNameFieldCard(t, client, runtime, "test/NameCard", nil)
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	delete(runtime.runtimeObjects, inputMethodOfferedObject)
	delete(runtime.runtimeObjects, inputMethodOfferedCardObject)
	card := runtime.topCard()
	if offered, offeredCard := runtime.offeredInputMethod(); offered != handler || offeredCard != card {
		t.Fatalf("offered field = %v on %v", offered, offeredCard)
	}
	runtime.unseenPress()
	if runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("the press the field missed left it open")
	}
	if offered, offeredCard := runtime.offeredInputMethod(); offered != handler || offeredCard != card {
		t.Fatalf("after the press the offered field = %v on %v", offered, offeredCard)
	}
	if _, err := session.TextInput(t.Context()); err != nil {
		t.Fatalf("TextInput() = %v", err)
	}
}

// A commit into the offered field types every character, also when that field
// was closed by a press it did not see or restored from a checkpoint taken
// before the offered record existed: the first character reopening the field
// is the commit's own doing, not a change that spends it.
func TestHostTextFillsAnOfferedFieldThatWasNotOpen(t *testing.T) {
	for _, restored := range []bool{false, true} {
		client, runtime := newTestRuntime(t)
		session := &Session{Client: client}
		listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
		field := newNameFieldCard(t, client, runtime, "test/NameCard", nil)
		field.handler = newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
		if restored {
			delete(runtime.runtimeObjects, inputMethodOfferedObject)
			delete(runtime.runtimeObjects, inputMethodOfferedCardObject)
		} else {
			// The title handled a press itself, a cursor move, and kept the
			// field on its screen.
			runtime.unseenPress()
		}
		edit, err := session.TextInput(t.Context())
		if err != nil {
			t.Fatalf("restored %v: %v", restored, err)
		}
		if err := edit.Commit(t.Context(), "홍길동"); err != nil {
			t.Fatalf("restored %v: commit = %v, edits %+v", restored, err, *edits)
		}
		want := []inputMethodEdit{{"홍", 1, inputMethodInsert}, {"길", 1, inputMethodInsert}, {"동", 1, inputMethodInsert}}
		if !reflect.DeepEqual(*edits, want) {
			t.Fatalf("restored %v: edits = %+v, want %+v", restored, *edits, want)
		}
		if runtime.runtimeObjects[inputMethodOpenObject] != field.handler {
			t.Fatalf("restored %v: the field the commit typed into is not open", restored)
		}
	}
}

// A field the title moved on from without taking its listener away is offered
// for no more than one commit. The carrier goes where the title hands its keys
// now and types nothing there — not into another handler of the title's, even
// one in the digit mode, and not into a text component — and the commit fails.
// After it the field the title handed the carrier to is offered, or, when that
// is no handler of the title's, the old one no longer is.
func TestAnAbandonedTitleFieldGivesWayAtItsFirstCommit(t *testing.T) {
	t.Run("another handler", func(t *testing.T) {
		client, runtime := newTestRuntime(t)
		session := &Session{Client: client}
		oldListener, oldEdits := recordingInputMethodListener(t, client, "test/OldListener")
		newListener, newEdits := recordingInputMethodListener(t, client, "test/NewListener")
		field := newNameFieldCard(t, client, runtime, "test/NameCard", nil)
		next := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, newListener)
		setGuestInputMethodMode(t, client, next, inputMethodModeNumeric)
		abandoned := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, oldListener)
		field.handler = next
		if offered, _ := runtime.offeredInputMethod(); offered != abandoned {
			t.Fatalf("offered field = %v", offered)
		}
		edit, err := session.TextInput(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := edit.Commit(t.Context(), "가"); !errors.Is(err, backend.ErrTextInputChanged) {
			t.Fatalf("a commit into the abandoned field answered %v", err)
		}
		if len(*oldEdits) != 0 || len(*newEdits) != 0 {
			t.Fatalf("the carrier typed: old %+v, new %+v", *oldEdits, *newEdits)
		}
		if offered, _ := runtime.offeredInputMethod(); offered != next {
			t.Fatalf("after the commit the offered field = %v", offered)
		}
		edit, err = session.TextInput(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := edit.Commit(t.Context(), "12"); err != nil {
			t.Fatal(err)
		}
		want := []inputMethodEdit{{"1", 1, inputMethodInsert}, {"2", 1, inputMethodInsert}}
		if !reflect.DeepEqual(*newEdits, want) || len(*oldEdits) != 0 {
			t.Fatalf("edits: old %+v, new %+v", *oldEdits, *newEdits)
		}
	})
	t.Run("a text component", func(t *testing.T) {
		client, runtime := newTestRuntime(t)
		session := &Session{Client: client}
		listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
		component := newWidget(runtimeTextFieldComponentClass)
		if _, err := runtimeTextComponentConstructorWithText(runtime, client.JVM(), []jvm.Value{
			jvm.ReferenceValue(component), jvm.ReferenceValue(client.JVM().NewString("")), jvm.IntValue(textConstraintAny),
		}); err != nil {
			t.Fatal(err)
		}
		card := &jvm.Object{ClassName: "test/ComponentCard"}
		presses := 0
		if err := client.JVM().RegisterNative(card.ClassName, "keyNotify", "(II)Z", func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
			presses++
			return runtimeTextComponentKeyNotify(runtime, vm, []jvm.Value{jvm.ReferenceValue(component), arguments[1], arguments[2]})
		}); err != nil {
			t.Fatal(err)
		}
		runtime.displayCards = append(runtime.displayCards, card)
		abandoned := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
		runtime.unseenPress()
		if offered, _ := runtime.offeredInputMethod(); offered != abandoned {
			t.Fatalf("offered field = %v", offered)
		}
		edit, err := session.TextInput(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := edit.Commit(t.Context(), "가"); !errors.Is(err, backend.ErrTextInputChanged) {
			t.Fatalf("a commit into the abandoned field answered %v", err)
		}
		if presses != 2 || componentText(component) != "" || len(*edits) != 0 {
			t.Fatalf("after %d presses the component holds %q, edits %+v", presses, componentText(component), *edits)
		}
		if offered, _ := runtime.offeredInputMethod(); offered != nil {
			t.Fatalf("the field that refused the carrier is still offered: %v", offered)
		}
		// The player's own keys still type into the component.
		pressCardKey(t, runtime, '2')
		if componentText(component) != "a" {
			t.Fatalf("a key the player pressed typed %q", componentText(component))
		}
	})
}

// An edit taken before the field closed is refused after, without a key being
// sent to whatever the title shows instead.
func TestHostTextForAClosedFieldSendsNothing(t *testing.T) {
	client, runtime := newTestRuntime(t)
	session := &Session{Client: client}
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	field := newNameFieldCard(t, client, runtime, "test/NameCard", nil)
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	field.handler = handler
	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	pressCardKey(t, runtime, wipicrt.KeyFire)
	field.open = true
	presses := field.presses
	if err := edit.Commit(t.Context(), "a"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("a commit after the field closed answered %v", err)
	}
	if field.presses != presses || len(*edits) != 0 {
		t.Fatal("a stale commit sent a key")
	}
}

// A numeric field takes digits from the Host and refuses everything else, the
// way a Java text field's Host input does.
func TestHostTextForANumericFieldTakesDigits(t *testing.T) {
	client, runtime := newTestRuntime(t)
	session := &Session{Client: client}
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	field := newNameFieldCard(t, client, runtime, "test/NameCard", nil)
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintPassword, listener)
	field.handler = handler
	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if edit.InputMode != "numeric" || !edit.Password {
		t.Fatalf("edit hints = %+v", edit)
	}
	if err := edit.Commit(t.Context(), "12a"); !errors.Is(err, backend.ErrInvalidTextInput) || len(*edits) != 0 {
		t.Fatalf("letters reached a numeric field: %v", err)
	}
	if err := edit.Commit(t.Context(), "12"); err != nil || len(*edits) != 2 {
		t.Fatalf("digits: %v, %+v", err, *edits)
	}
}

// The handler a text component carries is the component's, reached through
// the component; registering a listener on it opens no field of the title's.
func TestAComponentsHandlerIsNotATitleField(t *testing.T) {
	client, runtime := newTestRuntime(t)
	listener, _ := recordingInputMethodListener(t, client, "test/NameListener")
	component := &jvm.Object{ClassName: runtimeTextFieldComponentClass, Fields: map[string]jvm.Value{}}
	attachInputMethodHandler(component, jvm.IntValue(textConstraintAny))
	handler, err := component.Fields[componentInputHandlerField].Reference()
	if err != nil || handler == nil {
		t.Fatalf("component handler: %v", err)
	}
	if _, err := runtimeInputMethodSetListener(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(handler), jvm.ReferenceValue(listener)}); err != nil {
		t.Fatal(err)
	}
	if runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("a component's handler opened a title field")
	}
}

// A quick save taken while the field is open restores it open: the open
// handler and its card are runtime objects, so the Host text input works
// before any key is pressed after loading.
func TestATitleOwnedFieldStaysOpenAcrossACheckpoint(t *testing.T) {
	client, source := newTestRuntime(t)
	listener, _ := recordingInputMethodListener(t, client, "test/NameListener")
	handler := &jvm.Object{ClassName: runtimeInputMethodHandlerClass, Fields: map[string]jvm.Value{
		inputMethodModeField:       jvm.IntValue(inputMethodModeHangul),
		inputMethodConstraintField: jvm.IntValue(textConstraintAny),
		inputMethodTitleOwnedField: jvm.IntValue(1),
		inputMethodListenerField:   jvm.ReferenceValue(listener),
	}}
	card := &jvm.Object{ClassName: "test/NameCard"}
	source.displayCards = []*jvm.Object{card}
	source.touchInputMethod(handler, false)
	saved, err := source.captureHeapState(nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var parsed runtimeHeapState
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	freshClient, fresh := newTestRuntime(t)
	if _, err := fresh.restoreHeapState(parsed); err != nil {
		t.Fatal(err)
	}
	restored := fresh.runtimeObjects[inputMethodOpenObject]
	if restored == nil || restored == handler || fresh.runtimeObjects[inputMethodCardObject] != fresh.topCard() {
		t.Fatal("the open field did not survive the checkpoint")
	}
	if fresh.runtimeObjects[inputMethodOfferedObject] != restored || fresh.runtimeObjects[inputMethodOfferedCardObject] != fresh.topCard() {
		t.Fatal("the offered field did not survive the checkpoint")
	}
	restoredListener, err := restored.Fields[inputMethodListenerField].Reference()
	if err != nil || restoredListener == nil {
		t.Fatalf("listener did not survive: %v", err)
	}
	_, edits := recordingInputMethodListener(t, freshClient, restoredListener.ClassName)
	if err := freshClient.JVM().RegisterNative("test/NameCard", "keyNotify", "(II)Z", func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		eventType, _ := arguments[1].Int32()
		key, _ := arguments[2].Int32()
		_, err := runtimeInputMethodNotifyKeyInput(fresh, vm, []jvm.Value{jvm.ReferenceValue(restored), jvm.IntValue(key), jvm.IntValue(eventType)})
		return jvm.IntValue(0), err
	}); err != nil {
		t.Fatal(err)
	}
	edit, err := (&Session{Client: freshClient}).TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "김"); err != nil {
		t.Fatal(err)
	}
	if want := []inputMethodEdit{{"김", 1, inputMethodInsert}}; !reflect.DeepEqual(*edits, want) {
		t.Fatalf("edits after restore = %+v", *edits)
	}
}

// The three members guest code died on in a sweep that held keys down for
// minutes, and the buffer edit two titles name and nothing had reached. Each
// one is looked up the way the guest does — through the class record — and
// then made to do its job.
func TestTheMembersGuestCodeReachesResolveAndWork(t *testing.T) {
	client, runtime := newTestRuntime(t)
	for _, member := range []struct{ class, name, descriptor string }{
		{"java/lang/Character", "<init>", "(C)V"},
		{"java/lang/String", "<init>", "(Ljava/lang/StringBuffer;)V"},
		{runtimeInputMethodHandlerClass, "notifyKeyInput", "(II)Z"},
		{"java/lang/StringBuffer", "deleteCharAt", "(I)Ljava/lang/StringBuffer;"},
		{"java/lang/StringBuffer", "getChars", "(II[CI)V"},
	} {
		classAddress, err := runtime.ensureJavaClass(member.class)
		if err != nil {
			t.Fatal(err)
		}
		method, found, err := client.JVM().FindAOTMethod(classAddress, member.name, member.descriptor)
		if err != nil || !found || method.Body == 0 {
			t.Fatalf("%s.%s%s does not resolve from the guest's class record: found=%v err=%v", member.class, member.name, member.descriptor, found, err)
		}
	}

	class, err := runtime.ensureJavaClass("java/lang/Character")
	if err != nil {
		t.Fatal(err)
	}
	_, boxed, err := runtime.allocateAOTInstance(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.vm.InvokeVirtual(boxed, "<init>", "(C)V", jvm.IntValue('k')); err != nil {
		t.Fatal(err)
	}
	value, err := client.vm.InvokeVirtual(boxed, "charValue", "()C")
	if character, _ := value.Int32(); err != nil || character != 'k' {
		t.Fatalf("charValue = %v/%v, want 'k'", value, err)
	}

	buffer, err := client.vm.NewObject("java/lang/StringBuffer", "(Ljava/lang/String;)V", jvm.ReferenceValue(client.vm.NewString("score 120")))
	if err != nil {
		t.Fatal(err)
	}
	class, err = runtime.ensureJavaClass("java/lang/String")
	if err != nil {
		t.Fatal(err)
	}
	_, copied, err := runtime.allocateAOTInstance(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.vm.InvokeVirtual(copied, "<init>", "(Ljava/lang/StringBuffer;)V", jvm.ReferenceValue(buffer)); err != nil {
		t.Fatal(err)
	}
	if text, ok := jvm.StringText(copied); !ok || text != "score 120" {
		t.Fatalf("String(StringBuffer) = %q/%v", text, ok)
	}
	if _, err := client.vm.InvokeVirtual(buffer, "deleteCharAt", "(I)Ljava/lang/StringBuffer;", jvm.IntValue(8)); err != nil {
		t.Fatal(err)
	}
	shortened, err := client.vm.InvokeVirtual(buffer, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := shortened.Reference(); object == nil {
		t.Fatal("StringBuffer.toString answered null")
	} else if text, ok := jvm.StringText(object); !ok || text != "score 12" {
		t.Fatalf("deleteCharAt left %q/%v", text, ok)
	}
}

// A title whose keyNotify throws after handing a press to its field survives
// that key when a person presses it, so it survives the same press when the
// Host text input sends it: the exception ends the call, the character the
// field took stays taken, and the rest of the text goes on.
func TestHostTextSurvivesAnUncaughtExceptionInKeyNotify(t *testing.T) {
	client, runtime := newTestRuntime(t)
	session := &Session{Client: client}
	listener, edits := recordingInputMethodListener(t, client, "test/ThrowingNameListener")
	var handler *jvm.Object
	if err := client.JVM().RegisterNative("test/ThrowingNameCard", "keyNotify", "(II)Z", func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		eventType, _ := arguments[1].Int32()
		key, _ := arguments[2].Int32()
		if eventType != wipicrt.KeyPressed {
			return jvm.IntValue(0), nil
		}
		if _, err := vm.InvokeVirtual(handler, "notifyKeyInput", "(II)Z", jvm.IntValue(key), jvm.IntValue(eventType)); err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), &jvm.GuestException{Object: &jvm.Object{ClassName: "java/lang/NullPointerException"}, Message: "after the field took the key"}
	}); err != nil {
		t.Fatal(err)
	}
	// The field belongs to the card on top when the title gives it a listener.
	runtime.displayCards = append(runtime.displayCards, &jvm.Object{ClassName: "test/ThrowingNameCard"})
	handler = newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	setGuestInputMethodMode(t, client, handler, inputMethodModeHangul)

	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "홍길"); err != nil {
		t.Fatalf("a commit through a keyNotify that throws answered %v", err)
	}
	want := []inputMethodEdit{{"홍", 1, inputMethodInsert}, {"길", 1, inputMethodInsert}}
	if !reflect.DeepEqual(*edits, want) {
		t.Fatalf("edits = %+v, want %+v", *edits, want)
	}
	if count, first := client.UncaughtCallbacks(); count != 2 || !strings.HasPrefix(first, "key: ") {
		t.Fatalf("uncaught callbacks = %d, first = %q", count, first)
	}
}
