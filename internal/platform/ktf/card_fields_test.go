package ktf

import (
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// newGuestCard allocates a bound Card the way a guest `new` does: the payload
// exists before the constructor runs.
func newGuestCard(t *testing.T, runtime *initializationRuntime) (uint32, *jvm.Object) {
	t.Helper()
	classAddress, err := runtime.ensureJavaClass(runtimeCardClass)
	if err != nil {
		t.Fatal(err)
	}
	address, object, err := runtime.allocateAOTInstance(classAddress)
	if err != nil {
		t.Fatal(err)
	}
	return address, object
}

// A title reads `w` off its own canvas rather than calling getWidth, so the
// constructor has to leave the geometry in the payload the guest reads.
func TestCardConstructorPublishesItsGeometry(t *testing.T) {
	_, runtime := newTestRuntime(t)
	address, object := newGuestCard(t, runtime)

	if _, err := runtimeCardConstructorBounds(runtime, nil, []jvm.Value{
		jvm.ReferenceValue(object),
		jvm.IntValue(3), jvm.IntValue(5), jvm.IntValue(120), jvm.IntValue(160),
	}); err != nil {
		t.Fatal(err)
	}
	constructor := runtimeJavaMethod{class: runtimeCardClass, name: "<init>", descriptor: "(IIII)V"}
	if err := runtime.publishGuestFields(object, constructor); err != nil {
		t.Fatal(err)
	}
	words, err := runtime.readGuestCardBounds(address)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []uint32{3, 5, 120, 160, 0} {
		if words[index] != want {
			t.Fatalf("published %s = %d, want %d", cardBoundsFields[index], words[index], want)
		}
	}
}

// The geometry a title reads has to follow move and resize, which are the two
// calls that change it after construction.
func TestCardResizePublishesTheNewGeometry(t *testing.T) {
	_, runtime := newTestRuntime(t)
	address, object := newGuestCard(t, runtime)
	if _, err := runtimeCardConstructor(runtime, nil, []jvm.Value{jvm.ReferenceValue(object)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeCardResize(runtime, nil, []jvm.Value{
		jvm.ReferenceValue(object), jvm.IntValue(64), jvm.IntValue(48),
	}); err != nil {
		t.Fatal(err)
	}
	resize := runtimeJavaMethod{class: runtimeCardClass, name: "resize", descriptor: "(II)V"}
	if err := runtime.publishGuestFields(object, resize); err != nil {
		t.Fatal(err)
	}
	words, err := runtime.readGuestCardBounds(address)
	if err != nil {
		t.Fatal(err)
	}
	if words[2] != 64 || words[3] != 48 {
		t.Fatalf("published size %dx%d, want 64x48", words[2], words[3])
	}
}

// The publish is refused for a subclass whose own fields live in the block,
// because writing there would overwrite them. Nothing local lays a class out
// that way — the client computes a subclass's offsets from the parent's
// declared size — but a publish that assumed so and was wrong would break a
// title silently.
func TestCardDoesNotPublishIntoASubclassThatUsesTheBlock(t *testing.T) {
	_, runtime := newTestRuntime(t)
	cardAddress, err := runtime.ensureJavaClass(runtimeCardClass)
	if err != nil {
		t.Fatal(err)
	}
	card, ok := runtime.client.vm.AOTClassAt(cardAddress)
	if !ok {
		t.Fatal("Card is not registered")
	}
	if err := runtime.client.vm.RegisterAOTClass(jvm.AOTClassMetadata{
		Address:      0x40000000,
		Name:         "game/Canvas",
		SuperName:    card.Name,
		AccessFlags:  0x0021,
		InstanceSize: 8,
		Fields:       []jvm.AOTFieldMetadata{{Address: 0x40001000, Name: "state", Descriptor: "I", Offset: 0}},
	}); err != nil {
		t.Fatal(err)
	}
	object := &jvm.Object{ClassName: "game/Canvas", Fields: map[string]jvm.Value{}}
	if runtime.guestReservesRuntimeBlock(object, runtimeCardClass, cardFieldsSize) {
		t.Fatal("a subclass whose own field is at offset zero was treated as leaving the block free")
	}
	plain := &jvm.Object{ClassName: runtimeCardClass, Fields: map[string]jvm.Value{}}
	if !runtime.guestReservesRuntimeBlock(plain, runtimeCardClass, cardFieldsSize) {
		t.Fatal("a Card does not reserve its own block")
	}
}

// A title reads `buf` off a byte sink rather than calling toByteArray, so a
// write has to leave the guest array's address in the payload the guest reads.
func TestByteSinkPublishesItsBuffer(t *testing.T) {
	client, runtime := newTestRuntime(t)
	classAddress, err := runtime.ensureJavaClass(jvm.ByteArrayOutputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	address, object, err := runtime.allocateAOTInstance(classAddress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.JVM().InvokeSpecial(object, jvm.ByteArrayOutputStreamClass, "<init>", "()V"); err != nil {
		t.Fatal(err)
	}
	write := runtimeJavaMethod{class: jvm.ByteArrayOutputStreamClass, name: "write", descriptor: "(I)V"}
	if err := runtime.publishGuestFields(object, write); err != nil {
		t.Fatal(err)
	}
	words, err := runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, 1, "byte sink buffer")
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.JVM().Field(object, jvm.ByteArrayOutputStreamClass, "buf", "[B")
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	bound, ok := client.JVM().AOTAddress(buffer)
	if !ok {
		t.Fatal("the buffer was not bound to guest memory")
	}
	if words[0] != bound {
		t.Fatalf("published buf = %#x, want the bound array at %#x", words[0], bound)
	}

	// A write that grows the array puts a different one behind the field, and
	// the payload has to follow it there.
	for index := 0; index < 200; index++ {
		if _, err := client.JVM().InvokeVirtual(object, "write", "(I)V", jvm.IntValue(int32(index))); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.publishGuestFields(object, write); err != nil {
		t.Fatal(err)
	}
	grown, err := client.JVM().Field(object, jvm.ByteArrayOutputStreamClass, "buf", "[B")
	if err != nil {
		t.Fatal(err)
	}
	after, err := grown.Reference()
	if err != nil {
		t.Fatal(err)
	}
	moved, ok := client.JVM().AOTAddress(after)
	if !ok {
		t.Fatal("the grown buffer was not bound to guest memory")
	}
	words, err = runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, 1, "byte sink buffer")
	if err != nil {
		t.Fatal(err)
	}
	if words[0] != moved {
		t.Fatalf("published buf after growth = %#x, want %#x", words[0], moved)
	}
}

// The source publishes the same word, and a title that decodes straight out of
// the array it is streaming reads it there. Nothing replaces the array, so it
// is what the guest sees for the life of the stream.
func TestByteSourcePublishesItsBuffer(t *testing.T) {
	client, runtime := newTestRuntime(t)
	classAddress, err := runtime.ensureJavaClass(jvm.ByteArrayInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	address, object, err := runtime.allocateAOTInstance(classAddress)
	if err != nil {
		t.Fatal(err)
	}
	// The lookup the guest makes: the field record has to be findable from
	// the class record, which is where a title reading buf stopped.
	if _, found, err := client.JVM().FindAOTField(classAddress, "buf", "[B"); err != nil {
		t.Fatal(err)
	} else if !found {
		t.Fatal("buf:[B does not resolve from the guest's ByteArrayInputStream record")
	}
	array := jvm.NewByteArray([]byte{1, 2, 3, 4})
	if _, err := client.JVM().InvokeSpecial(object, jvm.ByteArrayInputStreamClass, "<init>", "([B)V", jvm.ReferenceValue(array)); err != nil {
		t.Fatal(err)
	}
	construct := runtimeJavaMethod{class: jvm.ByteArrayInputStreamClass, name: "<init>", descriptor: "([B)V"}
	if err := runtime.publishGuestFields(object, construct); err != nil {
		t.Fatal(err)
	}
	words, err := runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, 1, "byte source buffer")
	if err != nil {
		t.Fatal(err)
	}
	bound, ok := client.JVM().AOTAddress(array)
	if !ok {
		t.Fatal("the source array was not bound to guest memory")
	}
	if words[0] != bound {
		t.Fatalf("published buf = %#x, want the bound array at %#x", words[0], bound)
	}
	// Reading moves a cursor and leaves the array alone, so the word the guest
	// already read stays valid.
	if _, err := client.JVM().InvokeVirtual(object, "read", "()I"); err != nil {
		t.Fatal(err)
	}
	after, err := runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, 1, "byte source buffer")
	if err != nil {
		t.Fatal(err)
	}
	if after[0] != bound {
		t.Fatalf("buf after a read = %#x, want %#x", after[0], bound)
	}
}

// The three cursors are published beside the array, because a title reads pos
// to know where its own decode stands rather than counting its reads. A name
// the record does not carry is not a field that reads zero: the guest resolves
// each of them against this record, and a miss stops the title at the link.
func TestByteSourcePublishesItsCursors(t *testing.T) {
	client, runtime := newTestRuntime(t)
	classAddress, err := runtime.ensureJavaClass(jvm.ByteArrayInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range byteSourceCursors {
		if _, found, err := client.JVM().FindAOTField(classAddress, name, "I"); err != nil {
			t.Fatal(err)
		} else if !found {
			t.Fatalf("%s:I does not resolve from the guest's ByteArrayInputStream record", name)
		}
	}
	address, object, err := runtime.allocateAOTInstance(classAddress)
	if err != nil {
		t.Fatal(err)
	}
	array := jvm.NewByteArray([]byte{1, 2, 3, 4})
	// The window constructor, because it is the one that decides all three
	// separately: reading starts at 1, ends at 4, and a reset goes back to 1.
	if _, err := client.JVM().InvokeSpecial(object, jvm.ByteArrayInputStreamClass,
		"<init>", "([BII)V", jvm.ReferenceValue(array), jvm.IntValue(1), jvm.IntValue(3)); err != nil {
		t.Fatal(err)
	}
	construct := runtimeJavaMethod{class: jvm.ByteArrayInputStreamClass, name: "<init>", descriptor: "([BII)V"}
	if err := runtime.publishGuestFields(object, construct); err != nil {
		t.Fatal(err)
	}
	bound, ok := client.JVM().AOTAddress(array)
	if !ok {
		t.Fatal("the source array was not bound to guest memory")
	}
	base := address + javaInstanceSize + javaInstanceHeader
	words, err := runtime.readAOTWords(base, 4, "byte source words")
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{bound, 1, 4, 1}; !slices.Equal(words, want) {
		t.Fatalf("published words = %v, want %v", words, want)
	}

	// A read moves pos, and the guest sees the number the stream is at.
	if _, err := client.JVM().InvokeVirtual(object, "read", "()I"); err != nil {
		t.Fatal(err)
	}
	read := runtimeJavaMethod{class: jvm.ByteArrayInputStreamClass, name: "read", descriptor: "()I"}
	if err := runtime.publishGuestFields(object, read); err != nil {
		t.Fatal(err)
	}
	words, err = runtime.readAOTWords(base, 4, "byte source words")
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{bound, 2, 4, 1}; !slices.Equal(words, want) {
		t.Fatalf("published words after a read = %v, want %v", words, want)
	}
}

// A title that boxes a flag resolves java/lang/Boolean first, and the class
// was published by nothing until it did. TRUE is a static whose word has to
// name the very object the core library holds, because a title compares the
// two with a pointer compare.
func TestBooleanResolvesAndPublishesItsInstances(t *testing.T) {
	client, runtime := newTestRuntime(t)
	classAddress, err := runtime.ensureJavaClass(jvm.BooleanClass)
	if err != nil {
		t.Fatal(err)
	}
	method, found, err := client.JVM().FindAOTMethod(classAddress, "<init>", "(Z)V")
	if err != nil {
		t.Fatal(err)
	}
	if !found || method.Body == 0 {
		t.Fatal("java/lang/Boolean.<init>(Z)V does not resolve from the guest's class record")
	}
	value, err := client.JVM().StaticField(jvm.BooleanClass, "TRUE", "Ljava/lang/Boolean;")
	if err != nil {
		t.Fatal(err)
	}
	published, err := runtimeBoxedBoolean(runtime, "TRUE")
	if err != nil {
		t.Fatal(err)
	}
	object, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	bound, ok := client.JVM().AOTAddress(object)
	if !ok {
		t.Fatal("Boolean.TRUE was not bound to guest memory")
	}
	if published != bound {
		t.Fatalf("published TRUE = %#x, want the core library's own instance at %#x", published, bound)
	}
}
