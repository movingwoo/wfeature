package ktf

import (
	"context"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// boundTextField builds a text field the way a guest does — allocated in guest
// memory, then constructed — and publishes what its constructor decided.
func boundTextField(t *testing.T, text string) (*Client, *initializationRuntime, uint32, *jvm.Object) {
	t.Helper()
	client, runtime := newTestRuntime(t)
	class, err := runtime.ensureJavaClass(runtimeTextFieldComponentClass)
	if err != nil {
		t.Fatal(err)
	}
	address, field, err := runtime.allocateAOTInstance(class)
	if err != nil {
		t.Fatal(err)
	}
	callTextComponent(t, runtime, field, "<init>", runtimeTextComponentConstructorWithText,
		jvm.ReferenceValue(client.vm.NewString(text)), jvm.IntValue(0))
	return client, runtime, address, field
}

// callTextComponent runs one runtime method on a component and the publish
// that ends every runtime call.
func callTextComponent(t *testing.T, runtime *initializationRuntime, component *jvm.Object, name string, body runtimeJavaImplementation, arguments ...jvm.Value) {
	t.Helper()
	if _, err := body(runtime, runtime.client.vm, append([]jvm.Value{jvm.ReferenceValue(component)}, arguments...)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := runtime.publishGuestFields(component, runtimeJavaMethod{class: runtimeTextFieldComponentClass, name: name}); err != nil {
		t.Fatalf("publish after %s: %v", name, err)
	}
}

func textComponentWords(t *testing.T, runtime *initializationRuntime, address uint32) []uint32 {
	t.Helper()
	words, err := runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, textComponentFieldsSize/4, "test text component words")
	if err != nil {
		t.Fatal(err)
	}
	return words
}

func writeTextComponentWord(t *testing.T, runtime *initializationRuntime, address uint32, word int, value uint32) {
	t.Helper()
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, value)
	if err := runtime.client.core.Memory().Write(address+javaInstanceSize+javaInstanceHeader+uint32(word)*4, data); err != nil {
		t.Fatal(err)
	}
}

// guestChars reads the characters a published m_td holds, and requires the
// array to be exactly as long as the text: a title takes m_td.length for it.
func guestChars(t *testing.T, runtime *initializationRuntime, array uint32) string {
	t.Helper()
	length, err := runtime.guestArrayLength(array)
	if err != nil {
		t.Fatal(err)
	}
	units, valid, err := runtime.readGuestStringUnits(array, 0, length)
	if err != nil || !valid {
		t.Fatalf("m_td at %#x does not read back: valid=%v err=%v", array, valid, err)
	}
	runes := make([]rune, len(units))
	for index, unit := range units {
		runes[index] = rune(unit)
	}
	return string(runes)
}

// A title that builds its own field on a text component reads the
// specification's protected fields off it — one set m_cPos straight after the
// platform constructor and died on the lookup — so a constructed field carries
// every one this runtime holds a value for.
func TestTextComponentPublishesTheProtectedFieldsATitleReads(t *testing.T) {
	client, runtime, address, field := boundTextField(t, "abc")
	words := textComponentWords(t, runtime, address)
	if handler, ok := client.vm.AOTObject(words[textComponentHandlerWord]); !ok || handler.ClassName != runtimeInputMethodHandlerClass {
		t.Fatalf("imHandler word %#x does not name the handler", words[textComponentHandlerWord])
	}
	if got := guestChars(t, runtime, words[textComponentDataWord]); got != "abc" {
		t.Fatalf("m_td = %q", got)
	}
	if words[textComponentCountWord] != 3 || words[textComponentCursorWord] != 3 {
		t.Fatalf("charCount = %d, m_cPos = %d, want 3 and 3", words[textComponentCountWord], words[textComponentCursorWord])
	}
	if words[textComponentModeWord] != 0 || int32(words[textComponentMaxLengthWord]) != -1 || words[textComponentConstraintWord] != 0 {
		t.Fatalf("iMode = %d, maxLength = %d, constraint = %d", words[textComponentModeWord], int32(words[textComponentMaxLengthWord]), words[textComponentConstraintWord])
	}
	if font, ok := client.vm.AOTObject(words[textComponentFontWord]); !ok || font != runtime.platformFont() {
		t.Fatalf("f word %#x does not name the platform font", words[textComponentFontWord])
	}
	if display, ok := client.vm.AOTObject(words[textComponentDisplayWord]); !ok || display != runtime.defaultDisplay() {
		t.Fatalf("display word %#x does not name the display", words[textComponentDisplayWord])
	}

	class, err := runtime.ensureJavaClass(runtimeTextFieldComponentClass)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range textComponentFields {
		found, ok, err := client.vm.FindAOTField(class, record.name, record.descriptor)
		if err != nil || !ok {
			t.Fatalf("%s:%s does not resolve through a text field's class record: %v", record.name, record.descriptor, err)
		}
		if found.Offset != record.offset {
			t.Fatalf("%s resolves at offset %d, want %d", record.name, found.Offset, record.offset)
		}
	}
	if got, err := runtimeTextComponentGetMaxLength(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(field)}); err != nil || got != jvm.IntValue(-1) {
		t.Fatalf("getMaxLength() = %v/%v, want -1 for no limit", got, err)
	}
}

// The caret is a word titles move themselves — to draw it, or to put it at the
// end before they hand the platform a key — so a call that does not edit the
// text leaves it alone, and an edit leaves it where the edit did.
func TestTheCaretWordFollowsEditsAndKeepsATitlesOwnValue(t *testing.T) {
	client, runtime, address, field := boundTextField(t, "abc")
	writeTextComponentWord(t, runtime, address, textComponentCursorWord, 1)
	callTextComponent(t, runtime, field, "setMaxLength", runtimeTextComponentSetMaxLength, jvm.IntValue(8))
	words := textComponentWords(t, runtime, address)
	if words[textComponentCursorWord] != 1 || words[textComponentMaxLengthWord] != 8 {
		t.Fatalf("after setMaxLength m_cPos = %d, maxLength = %d, want the title's 1 and 8", words[textComponentCursorWord], words[textComponentMaxLengthWord])
	}

	inserted, err := client.vm.NewArray(jvm.Type{Kind: jvm.TypeChar}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := jvm.SetArrayElement(inserted, 0, jvm.IntValue('X')); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		name      string
		body      runtimeJavaImplementation
		arguments []jvm.Value
		text      string
		cursor    uint32
	}{
		{"insert", runtimeTextComponentInsert, []jvm.Value{jvm.ReferenceValue(inserted), jvm.IntValue(0), jvm.IntValue(1), jvm.IntValue(1)}, "aXbc", 2},
		{"delete", runtimeTextComponentDelete, []jvm.Value{jvm.IntValue(0), jvm.IntValue(1)}, "Xbc", 0},
		{"setString", runtimeTextComponentSetString, []jvm.Value{jvm.ReferenceValue(client.vm.NewString("hello"))}, "hello", 5},
	} {
		callTextComponent(t, runtime, field, step.name, step.body, step.arguments...)
		words := textComponentWords(t, runtime, address)
		if got := guestChars(t, runtime, words[textComponentDataWord]); got != step.text {
			t.Fatalf("after %s m_td = %q, want %q", step.name, got, step.text)
		}
		if words[textComponentCountWord] != uint32(len(step.text)) || words[textComponentCursorWord] != step.cursor {
			t.Fatalf("after %s charCount = %d, m_cPos = %d, want %d and %d", step.name, words[textComponentCountWord], words[textComponentCursorWord], len(step.text), step.cursor)
		}
	}
}

// iMode follows the handler, which is where this runtime keeps the mode, and
// keeps a value the title wrote until the handler is given another — one title
// writes its own mode number there and hands the word to the handler itself.
func TestTheModeWordFollowsTheHandlerAndKeepsATitlesOwnValue(t *testing.T) {
	client, runtime, address, field := boundTextField(t, "")
	handler, err := field.Fields[componentInputHandlerField].Reference()
	if err != nil || handler == nil {
		t.Fatal("no handler")
	}
	setMode := func(mode int32) {
		t.Helper()
		if _, err := runtimeInputMethodSetMode(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(handler), jvm.IntValue(mode)}); err != nil {
			t.Fatal(err)
		}
	}
	setMode(3)
	if words := textComponentWords(t, runtime, address); words[textComponentModeWord] != 3 {
		t.Fatalf("iMode after the handler took mode 3 = %d", words[textComponentModeWord])
	}
	writeTextComponentWord(t, runtime, address, textComponentModeWord, 99)
	callTextComponent(t, runtime, field, "setString", runtimeTextComponentSetString, jvm.ReferenceValue(client.vm.NewString("x")))
	if words := textComponentWords(t, runtime, address); words[textComponentModeWord] != 99 {
		t.Fatalf("a text edit replaced the title's iMode 99 with %d", words[textComponentModeWord])
	}
	setMode(2)
	if words := textComponentWords(t, runtime, address); words[textComponentModeWord] != 2 {
		t.Fatalf("iMode after the handler took mode 2 = %d", words[textComponentModeWord])
	}
}

// Host text edits the component outside any of its methods, so it publishes
// itself: a title drawing its own field from m_td sees the committed name.
func TestHostTextReachesTheCharacterWords(t *testing.T) {
	client, runtime, address, field := boundTextField(t, "old")
	runtime.runtimeObjects["lwc:focus"] = field
	session := &Session{Client: client}
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "홍길동"); err != nil {
		t.Fatal(err)
	}
	words := textComponentWords(t, runtime, address)
	if got := guestChars(t, runtime, words[textComponentDataWord]); got != "홍길동" {
		t.Fatalf("m_td after a Host commit = %q", got)
	}
	if words[textComponentCountWord] != 3 || words[textComponentCursorWord] != 3 {
		t.Fatalf("charCount = %d, m_cPos = %d after a Host commit, want 3 and 3", words[textComponentCountWord], words[textComponentCursorWord])
	}
}

// A component allocated under an earlier, smaller layout — one a checkpoint
// from an earlier build restored — keeps every word past the size its class
// was registered with.
func TestAComponentUnderAnEarlierLayoutKeepsTheRestOfItsPayload(t *testing.T) {
	client, runtime, address, field := boundTextField(t, "abc")
	class, ok := client.vm.AOTClass(runtimeTextComponentClass)
	if !ok {
		t.Fatal("TextComponent is not registered")
	}
	class.InstanceSize = 4
	if err := client.vm.RegisterAOTClass(class); err != nil {
		t.Fatal(err)
	}
	before := textComponentWords(t, runtime, address)
	for word := 1; word < len(before); word++ {
		writeTextComponentWord(t, runtime, address, word, 0xa5a5a5a5)
	}
	callTextComponent(t, runtime, field, "setString", runtimeTextComponentSetString, jvm.ReferenceValue(client.vm.NewString("changed")))
	after := textComponentWords(t, runtime, address)
	if after[textComponentHandlerWord] != before[textComponentHandlerWord] {
		t.Fatalf("handler word moved from %#x to %#x", before[textComponentHandlerWord], after[textComponentHandlerWord])
	}
	for word := 1; word < len(after); word++ {
		if after[word] != 0xa5a5a5a5 {
			t.Fatalf("word %d past the registered layout was written: %#x (all %x)", word, after[word], slices.Clone(after))
		}
	}
}
