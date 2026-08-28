package jvm

import (
	_ "embed"
	"testing"
)

//go:embed testdata/CoreMembers.class
var coreMembersClass []byte

//go:embed testdata/CoreMembers$Source.class
var coreMembersSourceClass []byte

// A compiled title reaches a class-library member through its constant pool,
// which resolves the class and then the member on it. Every member this
// fixture touches had a working Go body and no declaration to resolve — the
// state four local archives stopped in — so the whole method is one call that
// either links or does not.
//
// It is the interpreted half of the same question the platform layers ask when
// they link ahead of time, and it is the cheaper half to keep: a declaration
// that goes missing again fails here rather than in a menu.
func TestCompiledCodeLinksTheCoreLibraryMembers(t *testing.T) {
	vm := New(mapClassSource{
		"CoreMembers":        coreMembersClass,
		"CoreMembers$Source": coreMembersSourceClass,
	}, Options{})
	result, err := vm.InvokeStatic("CoreMembers", "text", "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("text() error = %v", err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	// The window of the char array, the two boxed flags, an object append, the
	// byte the stream subclass read out of the protected buffer, and a long —
	// joined by a separator the replace then changes.
	text, _ := StringText(object)
	if text != "1.2:true:true:x:7:2" {
		t.Fatalf("text() = %q, want %q", text, "1.2:true:true:x:7:2")
	}
}

//go:embed testdata/Inherited.class
var inheritedClass []byte

//go:embed testdata/InheritedBase.class
var inheritedBaseClass []byte

// A field belongs to the class that declares it, whatever the code that
// touches it calls that class. A compiler names a field reference after the
// type of the expression it read, so a subclass reading an inherited field
// says its own name while the superclass that wrote it said the superclass's:
// storing under the name as written puts the two in different slots and the
// read answers a zero nothing ever wrote.
//
// This is not a corner of the library. It is every guest class that inherits a
// field — the shape a title's own class hierarchy has — and it was found by a
// stream subclass reaching for the buffer its superclass filled.
func TestAnInheritedFieldIsOneFieldUnderBothNames(t *testing.T) {
	vm := New(mapClassSource{
		"Inherited":     inheritedClass,
		"InheritedBase": inheritedBaseClass,
	}, Options{})
	object, err := vm.NewObject("Inherited", "(I)V", IntValue(21))
	if err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(object, "read", "()I")
	if err != nil {
		t.Fatal(err)
	}
	// The instance field the superclass wrote, plus the static it wrote beside
	// it: both are read back through the subclass's name for them.
	if value, _ := result.Int32(); value != 63 {
		t.Fatalf("read() = %d, want 63", value)
	}
}
