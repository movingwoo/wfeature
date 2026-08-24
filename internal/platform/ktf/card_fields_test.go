package ktf

import (
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
