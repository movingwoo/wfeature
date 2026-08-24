package lgt

import (
	"context"
	"testing"
)

// The slot the numbering rule puts `println(String)` at, and the one a title
// dispatches on: `OutputStream` takes 10 to 14, `PrintStream` overrides four of
// those and adds `checkError`, `setError`, nine `print` forms and ten `println`
// forms in declaration order. `setError` is protected and takes a slot all the
// same, which is what puts the String form at 34 rather than 33.
func TestPrintStreamSlotsFollowTheDeclarationOrder(t *testing.T) {
	for slot, want := range map[uint32]string{
		javaPrintCheckError: "checkError()Z",
		javaPrintString:     "print(Ljava/lang/String;)V",
		javaPrintlnEmpty:    "println()V",
		javaPrintlnInt:      "println(I)V",
		javaPrintlnString:   "println(Ljava/lang/String;)V",
		javaPrintlnObject:   "println(Ljava/lang/Object;)V",
	} {
		baked, served := javaBakedVirtualSlots[javaPrintStreamClass][slot]
		if !served {
			t.Errorf("slot %d is not served", slot)
			continue
		}
		if baked.Called != want {
			t.Errorf("slot %d stands for %q, want %q", slot, baked.Called, want)
		}
	}
	// A `long` and a `double` take two argument words; everything else takes
	// one past the receiver, and `println()` takes none.
	for slot, want := range map[uint32]int{
		javaPrintlnEmpty: 1, javaPrintlnInt: 2, javaPrintlnLong: 3, javaPrintlnDouble: 3,
	} {
		if got := javaBakedVirtualSlots[javaPrintStreamClass][slot].Method.Words; got != want {
			t.Errorf("slot %d reads %d argument words, want %d", slot, got, want)
		}
	}
}

// What each print renders. A log line that loses its subject is worth less than
// one that names it oddly, so a reference this platform holds no text for still
// prints as something.
func TestPrintRendersItsArgument(t *testing.T) {
	client := fixtureClient(t)
	text := newTestString(t, client, "loaded")
	for _, probe := range []struct {
		name      string
		slot      uint32
		arguments []uint32
		want      string
	}{
		{"a line on its own", javaPrintlnEmpty, []uint32{0}, ""},
		{"true", javaPrintlnBoolean, []uint32{0, 1}, "true"},
		{"false", javaPrintlnBoolean, []uint32{0, 0}, "false"},
		{"a character", javaPrintlnChar, []uint32{0, 'A'}, "A"},
		{"a negative int", javaPrintlnInt, []uint32{0, 0xffffffff}, "-1"},
		{"a long", javaPrintlnLong, []uint32{0, 2, 1}, "4294967298"},
		{"a float", javaPrintlnFloat, []uint32{0, 0x3fc00000}, "1.5"},
		{"a double", javaPrintlnDouble, []uint32{0, 0, 0x3ff80000}, "1.5"},
		{"a string", javaPrintlnString, []uint32{0, text}, "loaded"},
		{"a null", javaPrintlnString, []uint32{0, 0}, "null"},
	} {
		if got := client.javaPrintText(probe.slot, probe.arguments); got != probe.want {
			t.Errorf("%s printed %q, want %q", probe.name, got, probe.want)
		}
	}
}

// The whole reason `System.out` is filled in: the module compiles a print into
// a null test, a throw and a dispatch, so a null there is the title's own
// NullPointerException rather than a print that does not happen.
func TestSystemOutIsAnObjectAPrintCanDispatchOn(t *testing.T) {
	client := fixtureClient(t)
	surface := &javaSurface{
		StaticFields: []javaMemberRef{{Name: "out", Descriptor: "Ljava/io/PrintStream;"}},
		Classes:      []javaAPIClass{{Name: "java/lang/System", StaticFields: javaRun{Start: 0, Count: 1}}},
	}
	layout := newJavaLayout()
	if _, err := layout.layoutPlatformStatics(surface); err != nil {
		t.Fatal(err)
	}
	client.javaLink = &javaLink{surface: surface, layout: layout}

	class, err := client.preparePlatformJavaClass("java/lang/System")
	if err != nil {
		t.Fatalf("preparePlatformJavaClass() error = %v", err)
	}
	if class.StaticWords != 1 {
		t.Fatalf("the class object keeps %d words of statics, want 1", class.StaticWords)
	}
	// The read the module compiles: the class object's data block, past the
	// words the class object uses for itself, at the slot it was answered.
	object, err := client.readWord(class.dataBlock + javaClassDataWords*4)
	if err != nil {
		t.Fatal(err)
	}
	if object == 0 {
		t.Fatal("System.out is null, which is the title's own NullPointerException")
	}
	held, ok := client.javaClassOfObject(object)
	if !ok || held.Name != javaPrintStreamClass {
		t.Fatalf("System.out is a %v, want a %s", held, javaPrintStreamClass)
	}
	// And a dispatch through it reaches a print rather than an unimplemented
	// slot: the object's vtable holds the class's stubs, and the slot the
	// module bakes for `println(String)` is served.
	baked, served := javaBakedVirtualSlots[javaPrintStreamClass][javaPrintlnString]
	if !served {
		t.Fatal("println(String) is not served")
	}
	if _, err := baked.Method.Implementat(client, context.Background(), nil,
		[]uint32{object, newTestString(t, client, "a line")}); err != nil {
		t.Errorf("println(String) failed: %v", err)
	}
}
