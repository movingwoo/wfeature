package ktf

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
	"unicode/utf16"

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

// The key a title forwards becomes edits its listener applies to text of its
// own. A cycle on one key takes back the letter it typed before typing the
// next, because the titles that settled the contract ignore a replacement.
func TestInputMethodHandlerTypesTheKeypadIntoItsListener(t *testing.T) {
	client, runtime := newTestRuntime(t)
	clock := NewManualClock(time.Unix(1700000000, 0))
	client.clock = clock
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	setGuestInputMethodMode(t, client, handler, inputMethodModeLowercase)

	press := func(key int32) {
		t.Helper()
		if !notifyGuestKey(t, client, handler, key, KeyPressed) {
			t.Fatalf("key %d was not processed", key)
		}
	}
	press('2')
	clock.Advance(100 * time.Millisecond)
	press('2')
	clock.Advance(2 * time.Second)
	press('2')
	press(KeyClear)
	setGuestInputMethodMode(t, client, handler, inputMethodModeUppercase)
	press('3')
	setGuestInputMethodMode(t, client, handler, inputMethodModeNumeric)
	press('3')
	clock.Advance(100 * time.Millisecond)
	press('3')

	want := []inputMethodEdit{
		{"a", 1, inputMethodInsert},
		{"a", 1, inputMethodDelete}, {"b", 1, inputMethodInsert},
		{"a", 1, inputMethodInsert},
		{"a", 1, inputMethodDelete},
		{"D", 1, inputMethodInsert},
		{"3", 1, inputMethodInsert},
		{"3", 1, inputMethodInsert},
	}
	if !reflect.DeepEqual(*edits, want) {
		t.Fatalf("edits = %+v\nwant  %+v", *edits, want)
	}
}

// What the handler leaves alone reaches the title unprocessed: a key in Hangul,
// which has no keypad layout here, the star and hash keys a title uses for its
// own mode switch and confirmation, the keys that move around the screen, and
// a release.
func TestInputMethodHandlerLeavesTheTitlesKeysAlone(t *testing.T) {
	client, runtime := newTestRuntime(t)
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	setGuestInputMethodMode(t, client, handler, inputMethodModeHangul)
	if notifyGuestKey(t, client, handler, '2', KeyPressed) {
		t.Fatal("a key in Hangul was processed")
	}
	setGuestInputMethodMode(t, client, handler, inputMethodModeLowercase)
	for _, key := range []int32{KeyStar, KeyHash, KeyUp, KeyFire, KeyLeftSoft} {
		if notifyGuestKey(t, client, handler, key, KeyPressed) {
			t.Fatalf("key %d was processed", key)
		}
	}
	for _, eventType := range []int32{KeyReleased, KeyRepeated} {
		if notifyGuestKey(t, client, handler, '2', eventType) {
			t.Fatalf("event type %d typed", eventType)
		}
	}
	setGuestInputMethodMode(t, client, handler, 7)
	if notifyGuestKey(t, client, handler, '2', KeyPressed) {
		t.Fatal("a mode no title sets was typed in")
	}
	if len(*edits) != 0 {
		t.Fatalf("listener was handed %+v", *edits)
	}
}

// The specification's own rule: a handler with no listener processes nothing.
func TestInputMethodHandlerWithoutAListenerProcessesNothing(t *testing.T) {
	client, runtime := newTestRuntime(t)
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, nil)
	setGuestInputMethodMode(t, client, handler, inputMethodModeLowercase)
	if notifyGuestKey(t, client, handler, '2', KeyPressed) {
		t.Fatal("a handler without a listener processed a key")
	}
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	setGuestInputMethodListener(t, client, handler, listener)
	setGuestInputMethodListener(t, client, handler, nil)
	if notifyGuestKey(t, client, handler, '2', KeyPressed) || len(*edits) != 0 {
		t.Fatal("a removed listener was still handed keys")
	}
}

// Clear takes back only what the handler typed. Anything else the listener
// holds is the title's, and a new listener holds none of what the handler
// typed for the one before it.
func TestInputMethodHandlerClearTakesBackOnlyWhatItTyped(t *testing.T) {
	client, runtime := newTestRuntime(t)
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	handler := newGuestInputMethodHandler(t, client, runtime, textConstraintAny, listener)
	setGuestInputMethodMode(t, client, handler, inputMethodModeNumeric)
	if notifyGuestKey(t, client, handler, KeyClear, KeyPressed) {
		t.Fatal("clear with nothing typed was processed")
	}
	notifyGuestKey(t, client, handler, '4', KeyPressed)
	// Setting the same listener again keeps the handler's record.
	setGuestInputMethodListener(t, client, handler, listener)
	if !notifyGuestKey(t, client, handler, KeyClear, KeyPressed) {
		t.Fatal("clear after typing was not processed")
	}
	notifyGuestKey(t, client, handler, '5', KeyPressed)
	other, otherEdits := recordingInputMethodListener(t, client, "test/OtherListener")
	setGuestInputMethodListener(t, client, handler, other)
	if notifyGuestKey(t, client, handler, KeyClear, KeyPressed) || len(*otherEdits) != 0 {
		t.Fatal("clear took back a character typed for the previous listener")
	}
	want := []inputMethodEdit{{"4", 1, inputMethodInsert}, {"4", 1, inputMethodDelete}, {"5", 1, inputMethodInsert}}
	if !reflect.DeepEqual(*edits, want) {
		t.Fatalf("edits = %+v, want %+v", *edits, want)
	}
}

// A numeric constraint keeps a handler on digits whatever mode it is set to.
func TestInputMethodHandlerNumericConstraintTypesDigits(t *testing.T) {
	client, runtime := newTestRuntime(t)
	listener, edits := recordingInputMethodListener(t, client, "test/NameListener")
	for _, constraint := range []int32{textConstraintNumber, textConstraintPassword, textConstraintPhoneNumber} {
		handler := newGuestInputMethodHandler(t, client, runtime, constraint, listener)
		setGuestInputMethodMode(t, client, handler, inputMethodModeUppercase)
		notifyGuestKey(t, client, handler, '7', KeyPressed)
	}
	want := []inputMethodEdit{{"7", 1, inputMethodInsert}, {"7", 1, inputMethodInsert}, {"7", 1, inputMethodInsert}}
	if !reflect.DeepEqual(*edits, want) {
		t.Fatalf("edits = %+v, want %+v", *edits, want)
	}
}

// A quick save taken between two presses of one key keeps the cycle, so the
// press after loading replaces the letter rather than adding one.
func TestInputMethodHandlerCycleSurvivesACheckpoint(t *testing.T) {
	client, source := newTestRuntime(t)
	client.clock = NewManualClock(time.Unix(1700000000, 0))
	listener, _ := recordingInputMethodListener(t, client, "test/NameListener")
	handler := &jvm.Object{ClassName: runtimeInputMethodHandlerClass, Fields: map[string]jvm.Value{
		inputMethodModeField:     jvm.IntValue(inputMethodModeLowercase),
		inputMethodListenerField: jvm.ReferenceValue(listener),
	}}
	key := func(runtime *initializationRuntime, target *jvm.Object) {
		t.Helper()
		if _, err := runtimeInputMethodNotifyKeyInput(runtime, runtime.client.vm, []jvm.Value{
			jvm.ReferenceValue(target), jvm.IntValue('2'), jvm.IntValue(KeyPressed)}); err != nil {
			t.Fatal(err)
		}
	}
	key(source, handler)
	saved, err := source.captureHeapState([]*jvm.Object{handler})
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
	freshClient.clock = NewManualClock(time.Unix(1900000000, 0))
	roots, err := fresh.restoreHeapState(parsed)
	if err != nil {
		t.Fatal(err)
	}
	restoredListener, err := roots[0].Fields[inputMethodListenerField].Reference()
	if err != nil || restoredListener == nil {
		t.Fatalf("listener did not survive: %v", err)
	}
	_, edits := recordingInputMethodListener(t, freshClient, restoredListener.ClassName)
	key(fresh, roots[0])
	want := []inputMethodEdit{{"a", 1, inputMethodDelete}, {"b", 1, inputMethodInsert}}
	if !reflect.DeepEqual(*edits, want) {
		t.Fatalf("edits after restore = %+v, want %+v", *edits, want)
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
