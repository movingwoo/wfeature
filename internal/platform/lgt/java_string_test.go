package lgt

import (
	"strings"
	"testing"
)

// A string constant answers the object, and the same object every time: the
// module's own helper for a constant is nothing but this call and returns what
// it answers, and a title compares two constants by identity.
func TestJavaStringConstantAnswersOneObjectPerSlot(t *testing.T) {
	client := fixtureClient(t)
	units, err := client.allocateWords([]uint32{0x00420041, 0x00000043})
	if err != nil {
		t.Fatal(err)
	}
	slot, err := client.allocateWords([]uint32{0})
	if err != nil {
		t.Fatal(err)
	}

	object, err := client.takeJavaStringConstant(units, 3, slot)
	if err != nil {
		t.Fatalf("takeJavaStringConstant() error = %v", err)
	}
	if object == 0 {
		t.Fatal("the constant answered no object")
	}
	if text, ok := client.javaText(object); !ok || text != "ABC" {
		t.Errorf("the constant holds %q (%v), want %q", text, ok, "ABC")
	}
	cached, err := client.readWord(slot)
	if err != nil {
		t.Fatal(err)
	}
	if cached != object {
		t.Errorf("the cache slot holds %#x, want the object %#x", cached, object)
	}
	again, err := client.takeJavaStringConstant(units, 3, slot)
	if err != nil {
		t.Fatal(err)
	}
	if again != object {
		t.Errorf("the second ask built %#x, want the cached %#x", again, object)
	}
}

// A buffer holds text, appending grows it, and toString answers a String with
// what it holds — the chain every title builds a resource name out of.
func TestJavaStringBufferAppendsAndAnswersAString(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaStringBufferClass)
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaBufferConstructor(client, nil, nil, []uint32{buffer}); err != nil {
		t.Fatalf("the constructor failed: %v", err)
	}
	first, err := client.newJavaString("/img/")
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.newJavaString("all.mbm")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []uint32{first, second} {
		answer, err := javaBufferAppendText(client, nil, nil, []uint32{buffer, value})
		if err != nil {
			t.Fatalf("append failed: %v", err)
		}
		if answer != buffer {
			t.Errorf("append answered %#x, want the buffer %#x it is chained on", answer, buffer)
		}
	}
	text, err := javaBufferToString(client, nil, nil, []uint32{buffer})
	if err != nil {
		t.Fatalf("toString failed: %v", err)
	}
	if held, ok := client.javaText(text); !ok || held != "/img/all.mbm" {
		t.Errorf("the buffer became %q (%v), want %q", held, ok, "/img/all.mbm")
	}
	// A null appends as the four characters the language names it by, which is
	// what kept a resource name readable when a constant was answered wrongly.
	if _, err := javaBufferAppendText(client, nil, nil, []uint32{buffer, 0}); err != nil {
		t.Fatal(err)
	}
	if held, _ := client.javaText(buffer); held != "/img/all.mbmnull" {
		t.Errorf("appending null gave %q", held)
	}
}

// A String built from a byte array is the handset's own encoding, which is what
// makes a title's own Korean text readable rather than a run of replacements.
func TestJavaStringFromBytesDecodesTheHandsetEncoding(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaStringClass)
	if err != nil {
		t.Fatal(err)
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.javaArrayType(1, "B", 1)
	if err != nil {
		t.Fatal(err)
	}
	// "가나" in the encoding a Korean handset writes its own files in.
	encoded := []byte{0xb0, 0xa1, 0xb3, 0xaa}
	bytes, err := client.allocateJavaArray(array.Object, uint32(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	block, err := client.readWord(bytes + 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.core.Memory().Write(block+4, encoded); err != nil {
		t.Fatal(err)
	}
	if _, err := javaStringConstructor(client, nil, nil, []uint32{object, bytes}); err != nil {
		t.Fatalf("the constructor failed: %v", err)
	}
	if text, ok := client.javaText(object); !ok || text != "가나" {
		t.Errorf("the string is %q (%v), want %q", text, ok, "가나")
	}
}

// The String methods a title reaches through a baked slot. Each is here
// because a real module dispatches it; what each one is, and the evidence, is
// in java_api.go beside the slot number.
func TestJavaStringTrimSubstringAndCharArray(t *testing.T) {
	client := fixtureClient(t)
	object, err := client.newJavaString("\nABC \r")
	if err != nil {
		t.Fatal(err)
	}
	trimmed, err := javaStringTrim(client, nil, nil, []uint32{object})
	if err != nil {
		t.Fatalf("javaStringTrim() error = %v", err)
	}
	if text, _ := client.javaText(trimmed); text != "ABC" {
		t.Errorf("trim() answered %q, want %q", text, "ABC")
	}
	// Nothing to take off is the same object, which is what the language says.
	again, err := javaStringTrim(client, nil, nil, []uint32{trimmed})
	if err != nil {
		t.Fatal(err)
	}
	if again != trimmed {
		t.Errorf("trim() of trimmed text answered %#x, want the same object %#x", again, trimmed)
	}

	part, err := javaStringSubstring(client, nil, nil, []uint32{trimmed, 1, 3})
	if err != nil {
		t.Fatalf("javaStringSubstring() error = %v", err)
	}
	if text, _ := client.javaText(part); text != "BC" {
		t.Errorf("substring(1,3) answered %q, want %q", text, "BC")
	}
	if _, err := javaStringSubstring(client, nil, nil, []uint32{trimmed, 2, 9}); err == nil {
		t.Error("a substring past the end was accepted")
	}

	// A char[] is the guest's own array: a title measures and draws out of it,
	// so the units have to be in guest memory, two bytes apart.
	array, err := javaStringToCharArray(client, nil, nil, []uint32{trimmed})
	if err != nil {
		t.Fatalf("javaStringToCharArray() error = %v", err)
	}
	units, err := client.readJavaArrayChars(array)
	if err != nil {
		t.Fatal(err)
	}
	if got := javaTextOfUnits(units); got != "ABC" {
		t.Errorf("toCharArray() holds %q, want %q", got, "ABC")
	}
}

// Appending a number appends its decimal form, and answers the buffer so a
// chain of appends is one expression.
func TestJavaStringBufferAppendsANumber(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaStringBufferClass)
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaBufferConstructor(client, nil, nil, []uint32{buffer}); err != nil {
		t.Fatal(err)
	}
	answer, err := javaBufferAppendInt(client, nil, nil, []uint32{buffer, ^uint32(11)})
	if err != nil {
		t.Fatalf("javaBufferAppendInt() error = %v", err)
	}
	if answer != buffer {
		t.Errorf("append(int) answered %#x, want the buffer %#x", answer, buffer)
	}
	if text, _ := client.javaText(buffer); text != "-12" {
		t.Errorf("the buffer holds %q, want %q", text, "-12")
	}
}

// `String.valueOf(Object)` is null-safe and answers the same text
// `StringBuffer.append(Object)` appends, which is what keeps the two agreeing
// about an object neither of them built the characters of.
func TestStringValueOfObjectIsNullSafeAndNamesTheClass(t *testing.T) {
	client := fixtureClient(t)

	empty, err := javaStringValueOfObject(client, nil, nil, []uint32{0})
	if err != nil {
		t.Fatalf("valueOf(null) error = %v", err)
	}
	if text, _ := client.javaText(empty); text != "null" {
		t.Errorf("valueOf(null) = %q, want %q", text, "null")
	}

	held, err := client.newJavaString("already text")
	if err != nil {
		t.Fatal(err)
	}
	same, err := javaStringValueOfObject(client, nil, nil, []uint32{held})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := client.javaText(same); text != "already text" {
		t.Errorf("valueOf(String) = %q, want the string's own characters", text)
	}

	// An object with no text of its own is named by its class, which is
	// `Object.toString()`'s shape — and is the documented limit here: a class
	// that overrides toString is still answered this way.
	other, err := javaStringValueOfObject(client, nil, nil, []uint32{0x30001234})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := client.javaText(other)
	if !strings.Contains(text, "@") {
		t.Errorf("valueOf(Object) = %q, want a class-and-address form", text)
	}
}
