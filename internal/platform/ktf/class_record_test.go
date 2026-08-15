package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// A class this platform makes for a name it does not implement is still a
// class the guest dispatches through. The guest's virtual call reads the
// vtable pointer out of the class record and indexes it, so a record without
// one is not a lesser record but a broken one: the load lands at four bytes
// past null. An array class is where it bites, because an array's whole method
// set is Object's.
func TestFallbackClassRecordPublishesObjectsVTable(t *testing.T) {
	_, runtime := newTestRuntime(t)
	address, err := runtime.ensureJavaClass("[Lcom/example/Sprite;")
	if err != nil {
		t.Fatalf("ensureJavaClass() error = %v", err)
	}
	record, err := runtime.readAOTBytes(address, javaClassSize, "class record")
	if err != nil {
		t.Fatal(err)
	}
	vtable := binary.LittleEndian.Uint32(record[12:])
	slots := binary.LittleEndian.Uint16(record[16:])
	if vtable == 0 || slots == 0 {
		t.Fatalf("vtable = %#x with %d slots, want Object's", vtable, slots)
	}

	// Object's own vtable is what it must be, slot for slot: the guest takes
	// the slot number from the method record it resolved and applies it to
	// whatever class the object dispatches through, so a table that merely has
	// the right length would send getClass() somewhere else.
	object, err := runtime.ensureJavaClass("java/lang/Object")
	if err != nil {
		t.Fatal(err)
	}
	inherited, err := runtime.readAOTBytes(object, javaClassSize, "Object class record")
	if err != nil {
		t.Fatal(err)
	}
	if want := binary.LittleEndian.Uint16(inherited[16:]); slots != want {
		t.Fatalf("vtable holds %d slots, want Object's %d", slots, want)
	}
	entries, err := runtime.readAOTWords(vtable, uint32(slots), "vtable")
	if err != nil {
		t.Fatal(err)
	}
	objectEntries, err := runtime.readAOTWords(binary.LittleEndian.Uint32(inherited[12:]), uint32(slots), "Object vtable")
	if err != nil {
		t.Fatal(err)
	}
	for index := range entries {
		if entries[index] != objectEntries[index] {
			t.Fatalf("vtable slot %d = %#x, want Object's %#x", index, entries[index], objectEntries[index])
		}
	}

	// The registry has to know about the vtable too, because the method lookup
	// and the type check both answer from it rather than from the memory.
	metadata, ok := runtime.client.vm.AOTClassAt(address)
	if !ok {
		t.Fatal("the class was not registered")
	}
	if metadata.SuperName != "java/lang/Object" {
		t.Fatalf("SuperName = %q, want java/lang/Object", metadata.SuperName)
	}
	if len(metadata.VTable) != int(slots) {
		t.Fatalf("registered vtable holds %d slots, want %d", len(metadata.VTable), slots)
	}
}

// The guest's array-store check reads the element class out of the array
// class's descriptor at +0x14 — the word a class with fields spends on its
// field table, which an array has no use for. Leaving it zero asks the check
// about a null class, and a null class can only be waved through.
func TestArrayClassRecordCarriesItsElementClass(t *testing.T) {
	_, runtime := newTestRuntime(t)
	elementClass := func(t *testing.T, name string) uint32 {
		t.Helper()
		address, err := runtime.ensureJavaClass(name)
		if err != nil {
			t.Fatalf("ensureJavaClass(%q) error = %v", name, err)
		}
		record, err := runtime.readAOTBytes(address, javaClassSize, "class record")
		if err != nil {
			t.Fatal(err)
		}
		descriptor, err := runtime.readAOTBytes(binary.LittleEndian.Uint32(record[8:]), javaDescriptorSize, "class descriptor")
		if err != nil {
			t.Fatal(err)
		}
		return binary.LittleEndian.Uint32(descriptor[20:])
	}

	stringClass, err := runtime.ensureJavaClass("java/lang/String")
	if err != nil {
		t.Fatal(err)
	}
	if element := elementClass(t, "[Ljava/lang/String;"); element != stringClass {
		t.Fatalf("[Ljava/lang/String; element class = %#x, want %#x", element, stringClass)
	}

	byteArrayClass, err := runtime.ensureJavaClass("[B")
	if err != nil {
		t.Fatal(err)
	}
	if element := elementClass(t, "[[B"); element != byteArrayClass {
		t.Fatalf("[[B element class = %#x, want %#x", element, byteArrayClass)
	}

	// A primitive array is stored into without a check at all, so the word
	// stays what it was: a record invented for `B` would be one nothing names.
	if element := elementClass(t, "[B"); element != 0 {
		t.Fatalf("[B element class = %#x, want none", element)
	}
}

// What the element class buys is a decidable check, and a decidable check can
// answer no — which is the whole risk of writing it. These are the answers the
// array store has to get, on the classes this platform itself makes.
func TestArrayStoreCheckAnswersThroughTheElementClass(t *testing.T) {
	_, runtime := newTestRuntime(t)
	stringClass, err := runtime.ensureJavaClass("java/lang/String")
	if err != nil {
		t.Fatal(err)
	}
	text, _, err := runtime.allocateAOTInstance(stringClass)
	if err != nil {
		t.Fatal(err)
	}
	unrelated, _, err := runtime.allocateAOTInstance(writeGuestClass(t, runtime, "game/Unrelated", 0, nil, 0x21))
	if err != nil {
		t.Fatal(err)
	}
	if result := checkGuestType(t, runtime, stringClass, text); result != 1 {
		t.Errorf("String into a String[] = %d, want 1", result)
	}
	if result := checkGuestType(t, runtime, stringClass, unrelated); result != 0 {
		t.Errorf("an unrelated class into a String[] = %d, want 0", result)
	}

	// An array is a value too, and the two interfaces every array implements
	// are declared by no class record this platform writes.
	byteArrayClass, err := runtime.ensureJavaClass("[B")
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := runtime.allocateAOTArrayObject(mustAOTClass(t, runtime, byteArrayClass), 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"java/lang/Object", "java/lang/Cloneable", "java/io/Serializable"} {
		target, err := runtime.ensureJavaClass(name)
		if err != nil {
			t.Fatal(err)
		}
		if result := checkGuestType(t, runtime, target, bytes); result != 1 {
			t.Errorf("a byte[] into a %s[] = %d, want 1", name, result)
		}
	}
	if result := checkGuestType(t, runtime, stringClass, bytes); result != 0 {
		t.Errorf("a byte[] into a String[] = %d, want 0", result)
	}
}

func mustAOTClass(t *testing.T, runtime *initializationRuntime, address uint32) jvm.AOTClassMetadata {
	t.Helper()
	metadata, ok := runtime.client.vm.AOTClassAt(address)
	if !ok {
		t.Fatalf("no class is registered at %#x", address)
	}
	return metadata
}

// An inherited method has to be findable on such a class, since that is what
// the guest asks for after dispatching through it.
func TestFallbackClassAnswersAnInheritedMethodLookup(t *testing.T) {
	_, runtime := newTestRuntime(t)
	address, err := runtime.ensureJavaClass("[Lcom/example/Sprite;")
	if err != nil {
		t.Fatal(err)
	}
	method, ok, err := runtime.client.vm.FindAOTMethod(address, "getClass", "()Ljava/lang/Class;")
	if err != nil {
		t.Fatalf("FindAOTMethod() error = %v", err)
	}
	if !ok || method.Body == 0 && method.NativeBody == 0 {
		t.Fatalf("getClass() found = %v with body %#x/%#x", ok, method.Body, method.NativeBody)
	}
}

// The dispatch alias is a copy of a class record inside the platform arena,
// and it is a class record in every respect the guest can see. A title reaches
// a virtual call through the object header — which names the alias — and then
// hands the same word to the method lookup, so the alias has to resolve to its
// class. One title stopped on the first paint of its card because it did not.
func TestDispatchAliasResolvesAsItsClass(t *testing.T) {
	_, runtime := newTestRuntime(t)
	address, err := runtime.ensureJavaClass("java/lang/Object")
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := runtime.client.vm.AOTClassAt(address)
	if !ok {
		t.Fatal("java/lang/Object is not registered")
	}
	if _, err := runtime.aotVTableHeader(metadata); err != nil {
		t.Fatalf("aotVTableHeader() error = %v", err)
	}
	alias, ok := runtime.classAliases[address]
	if !ok || alias == 0 {
		t.Fatalf("no dispatch alias was made for %s", metadata.Name)
	}
	aliased, ok := runtime.client.vm.AOTClassAt(alias)
	if !ok {
		t.Fatalf("the alias at %#x resolves to no class", alias)
	}
	if aliased.Name != metadata.Name {
		t.Fatalf("the alias resolves to %s, want %s", aliased.Name, metadata.Name)
	}
	if _, found, err := runtime.client.vm.FindAOTMethod(alias, "getClass", "()Ljava/lang/Class;"); err != nil || !found {
		t.Fatalf("getClass() through the alias found = %v, error = %v", found, err)
	}
}
