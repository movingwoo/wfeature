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
		if !field.open || eventType != KeyPressed {
			return jvm.IntValue(0), nil
		}
		if field.message {
			field.message = key != KeyFire
			return jvm.IntValue(0), nil
		}
		switch key {
		case KeyFire:
			field.open = false
			return jvm.IntValue(0), nil
		case KeyLeftSoft:
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
	for _, eventType := range []int32{KeyPressed, KeyReleased} {
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
	if !notifyGuestKey(t, client, handler, '7', KeyPressed) || !notifyGuestKey(t, client, handler, KeyClear, KeyPressed) {
		t.Fatal("a digit or clear in the digit mode was not processed")
	}
	for _, eventType := range []int32{KeyReleased, KeyRepeated} {
		if notifyGuestKey(t, client, handler, '7', eventType) {
			t.Fatalf("event type %d typed", eventType)
		}
	}
	for _, mode := range []int32{inputMethodModeLowercase, inputMethodModeUppercase, inputMethodModeHangul, 7} {
		setGuestInputMethodMode(t, client, handler, mode)
		if notifyGuestKey(t, client, handler, '2', KeyPressed) {
			t.Fatalf("a key in mode %d typed", mode)
		}
	}
	for _, key := range []int32{KeyStar, KeyHash, KeyUp, KeyFire, KeyLeftSoft} {
		if notifyGuestKey(t, client, handler, key, KeyPressed) {
			t.Fatalf("key %d was processed", key)
		}
	}
	numbers := newGuestInputMethodHandler(t, client, runtime, textConstraintNumber, listener)
	setGuestInputMethodMode(t, client, numbers, inputMethodModeUppercase)
	notifyGuestKey(t, client, numbers, '4', KeyPressed)
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
	if notifyGuestKey(t, client, handler, '2', KeyPressed) || runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("a handler without a listener processed a key or opened a field")
	}
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	setGuestInputMethodListener(t, client, handler, listener)
	if runtime.runtimeObjects[inputMethodOpenObject] != handler {
		t.Fatal("registering a listener did not open the field")
	}
	setGuestInputMethodListener(t, client, handler, nil)
	if notifyGuestKey(t, client, handler, '2', KeyPressed) || len(*edits) != 0 || runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("a removed listener was still handed keys or kept the field open")
	}
}

// The field is open from the moment the title registers its listener, stays
// open while the presses the card receives reach the handler — a mode switch
// counts — and closes on the first press that does not. A release closes
// nothing, because a title hands only presses to its field.
func TestATitleOwnedFieldIsOpenWhileItTakesThePresses(t *testing.T) {
	client, runtime := newTestRuntime(t)
	session := &Session{Client: client}
	listener, _ := recordingInputMethodListener(t, client, "test/NameListener")
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
	for _, key := range []int32{'2', KeyUp, KeyStar, KeyHash, KeyLeftSoft} {
		pressCardKey(t, runtime, key)
		if !available() {
			t.Fatalf("the field closed on key %d it took", key)
		}
	}
	pressCardKey(t, runtime, KeyFire)
	if available() {
		t.Fatal("the field stayed open after fire closed it")
	}
	// A title that opens its field again hands it a key, and the field is back.
	field.open = true
	pressCardKey(t, runtime, '5')
	if !available() {
		t.Fatal("the reopened field offered no text input")
	}
	// A field on a card that is no longer on top is not on the screen.
	runtime.displayCards = append(runtime.displayCards, &jvm.Object{ClassName: "test/OtherCard"})
	if available() {
		t.Fatal("a field under another card offered text input")
	}
	runtime.displayCards = runtime.displayCards[:len(runtime.displayCards)-1]
	runtime.guestEventLoop = true
	if available() {
		t.Fatal("a title running its own event loop offered text input")
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
	pressCardKey(t, runtime, KeyClear)
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
	if _, err := session.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("a closed field still offered text input: %v", err)
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
	pressCardKey(t, runtime, KeyFire)
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
	pressCardKey(t, runtime, KeyFire)
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
	pressCardKey(t, runtime, KeyFire)
	if runtime.runtimeObjects[inputMethodOpenObject] != nil {
		t.Fatal("a field confirmed before taking a key stayed open")
	}
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
	pressCardKey(t, runtime, KeyFire)
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
