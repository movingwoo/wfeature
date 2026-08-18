package skt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The synthetic space is what makes a cheat possible on a runtime whose state
// is Go objects. These pin the three properties a search depends on — a field
// is at an address, the address does not move, and a write at it lands in the
// field — plus the two refusals that keep a careless write from corrupting the
// graph.

// heapFixture builds a VM with one class of its own and returns the map over
// it. The class is declared in Go rather than compiled, because what is being
// tested is the layout of declared fields and not the class file that declared
// them.
func heapFixture(t *testing.T) (*jvm.VM, *heapMap, *jvm.Object) {
	t.Helper()
	vm := jvm.New(nil, jvm.Options{})
	if err := vm.DefineClass(jvm.ClassDefinition{
		Name:      "game/Hero",
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic,
		Fields: []jvm.FieldDefinition{
			{Name: "gold", Descriptor: "I", Access: jvm.AccessPublic},
			{Name: "hp", Descriptor: "S", Access: jvm.AccessPublic},
			{Name: "seed", Descriptor: "J", Access: jvm.AccessPublic},
			{Name: "pack", Descriptor: "[I", Access: jvm.AccessPublic},
			{Name: "party", Descriptor: "I", Access: jvm.AccessStatic},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// The object is built directly rather than through a constructor: what is
	// under test is the layout a class declares, and a fixture class with a
	// constructor would only be a longer way to the same object.
	hero := &jvm.Object{ClassName: "game/Hero", Fields: map[string]jvm.Value{}}
	// The object has to be reachable from a root or the walk will not find it,
	// which is the same rule the real graph follows.
	if err := vm.SetStaticField("game/Hero", "party", "I", jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	heap := newHeapMap(vm, func() []*jvm.Object { return []*jvm.Object{hero} })
	return vm, heap, hero
}

// fieldAddress is where a named field of a mapped object lives.
func fieldAddress(t *testing.T, heap *heapMap, object *jvm.Object, name string) uint32 {
	t.Helper()
	entry := heap.byIdentity[heap.vm.Identity(object)]
	if entry == nil {
		t.Fatalf("%s is not mapped", object.ClassName)
	}
	for _, slot := range entry.shape.slots {
		if slot.field.Name == name {
			return entry.base + slot.offset
		}
	}
	t.Fatalf("%s has no field %q in its layout", object.ClassName, name)
	return 0
}

func TestHeapMapReadsAndWritesAnObjectsFields(t *testing.T) {
	vm, heap, hero := heapFixture(t)
	if err := vm.SetField(hero, "game/Hero", "gold", "I", jvm.IntValue(1234)); err != nil {
		t.Fatal(err)
	}
	if err := vm.SetField(hero, "game/Hero", "hp", "S", jvm.IntValue(-3)); err != nil {
		t.Fatal(err)
	}
	if err := vm.SetField(hero, "game/Hero", "seed", "J", jvm.LongValue(1<<40)); err != nil {
		t.Fatal(err)
	}
	heap.refresh()

	read := func(address uint32, width int) int64 {
		t.Helper()
		buffer := make([]byte, width)
		if err := heap.read(address, buffer); err != nil {
			t.Fatal(err)
		}
		var raw uint64
		for index := width - 1; index >= 0; index-- {
			raw = raw<<8 | uint64(buffer[index])
		}
		return int64(raw)
	}
	if got := read(fieldAddress(t, heap, hero, "gold"), 4); got != 1234 {
		t.Fatalf("gold reads as %d, want the value the object holds", got)
	}
	// A short is stored in a four-byte slot, sign extended, so a search at
	// either width finds it.
	if got := int64(int32(uint32(read(fieldAddress(t, heap, hero, "hp"), 4)))); got != -3 {
		t.Fatalf("hp reads as %d, want -3", got)
	}
	if got := read(fieldAddress(t, heap, hero, "seed"), 8); got != 1<<40 {
		t.Fatalf("seed reads as %d, want 2^40", got)
	}

	// The write is the half that matters: a cheat is only a cheat if the game
	// sees it.
	if err := heap.write(fieldAddress(t, heap, hero, "gold"), []byte{0xff, 0xff, 0, 0}); err != nil {
		t.Fatal(err)
	}
	value, err := vm.Field(hero, "game/Hero", "gold", "I")
	if err != nil {
		t.Fatal(err)
	}
	if number, _ := value.Int32(); number != 0xffff {
		t.Fatalf("gold = %d after a write through the map, want 65535", number)
	}

	// A one-byte write patches one byte of the field rather than clearing it,
	// the way the same write against a real address space would.
	if err := heap.write(fieldAddress(t, heap, hero, "gold"), []byte{0x01}); err != nil {
		t.Fatal(err)
	}
	value, _ = vm.Field(hero, "game/Hero", "gold", "I")
	if number, _ := value.Int32(); number != 0xff01 {
		t.Fatalf("gold = %#x after a one-byte write, want 0xff01", number)
	}
}

func TestHeapMapReadsAndWritesArrayElements(t *testing.T) {
	vm, heap, hero := heapFixture(t)
	pack, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := jvm.SetArrayRange(pack, 0, []jvm.Value{
		jvm.IntValue(10), jvm.IntValue(20), jvm.IntValue(30), jvm.IntValue(40),
	}); err != nil {
		t.Fatal(err)
	}
	if err := vm.SetField(hero, "game/Hero", "pack", "[I", jvm.ReferenceValue(pack)); err != nil {
		t.Fatal(err)
	}
	heap.refresh()

	entry := heap.byIdentity[vm.Identity(pack)]
	if entry == nil {
		t.Fatal("an array reachable from a mapped object's field was not mapped")
	}
	if entry.size != 16 {
		t.Fatalf("an int[4] takes %d bytes, want 16 at the natural stride", entry.size)
	}
	buffer := make([]byte, 16)
	if err := heap.read(entry.base, buffer); err != nil {
		t.Fatal(err)
	}
	if buffer[0] != 10 || buffer[4] != 20 || buffer[8] != 30 || buffer[12] != 40 {
		t.Fatalf("array reads as %v, want its elements four bytes apart", buffer)
	}
	if err := heap.write(entry.base+8, []byte{99, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	values, err := jvm.ArrayRange(pack, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if number, _ := values[0].Int32(); number != 99 {
		t.Fatalf("element 2 = %d after a write through the map, want 99", number)
	}
}

// TestHeapMapReadsAndWritesStatics covers the state a title keeps in a class
// rather than in an object, which is where a lot of a J2ME game's own numbers
// live and which nothing reachable from an object would find.
func TestHeapMapReadsAndWritesStatics(t *testing.T) {
	vm, heap, _ := heapFixture(t)
	heap.refresh()

	var address uint32
	entry := heap.byIdentity[staticsIdentity("game/Hero")]
	if entry == nil {
		t.Fatal("a loaded class's statics were not mapped")
	}
	for _, slot := range entry.shape.slots {
		if slot.field.Name == "party" {
			address = entry.base + slot.offset
		}
	}
	buffer := make([]byte, 4)
	if err := heap.read(address, buffer); err != nil {
		t.Fatal(err)
	}
	if buffer[0] != 1 {
		t.Fatalf("the static reads as %v, want the 1 it holds", buffer)
	}
	if err := heap.write(address, []byte{7, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	value, err := vm.StaticField("game/Hero", "party", "I")
	if err != nil {
		t.Fatal(err)
	}
	if number, _ := value.Int32(); number != 7 {
		t.Fatalf("the static = %d after a write through the map, want 7", number)
	}
}

// TestHeapMapKeepsAnAddressAcrossWalks is what a scan is built on: a value
// found on one screen has to still be at that address on the next, however
// much the game has allocated in between.
func TestHeapMapKeepsAnAddressAcrossWalks(t *testing.T) {
	_, heap, hero := heapFixture(t)
	heap.refresh()
	before := fieldAddress(t, heap, hero, "gold")

	extra := make([]*jvm.Object, 0, 32)
	for index := 0; index < 32; index++ {
		extra = append(extra, &jvm.Object{ClassName: "game/Hero", Fields: map[string]jvm.Value{}})
	}
	heap.extraRoots = func() []*jvm.Object { return append([]*jvm.Object{hero}, extra...) }
	heap.refresh()

	if after := fieldAddress(t, heap, hero, "gold"); after != before {
		t.Fatalf("the field moved from %#x to %#x across a walk", before, after)
	}
	if len(heap.byIdentity) < 33 {
		t.Fatalf("%d spans after mapping 33 objects; the new ones were not mapped", len(heap.byIdentity))
	}
}

// TestHeapMapRefusesToRewriteAReference keeps a careless write out of the
// object graph. A reference reads as the address of what it points at so a
// listing can be followed; writing one would mean inventing an object.
func TestHeapMapRefusesToRewriteAReference(t *testing.T) {
	vm, heap, hero := heapFixture(t)
	pack, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.SetField(hero, "game/Hero", "pack", "[I", jvm.ReferenceValue(pack)); err != nil {
		t.Fatal(err)
	}
	heap.refresh()

	address := fieldAddress(t, heap, hero, "pack")
	buffer := make([]byte, 4)
	if err := heap.read(address, buffer); err != nil {
		t.Fatal(err)
	}
	entry := heap.byIdentity[vm.Identity(pack)]
	if buffer[0] == 0 && buffer[1] == 0 && buffer[2] == 0 && buffer[3] == 0 {
		t.Fatal("a reference field read as null when it points at a mapped array")
	}
	if got := uint32(buffer[0]) | uint32(buffer[1])<<8 | uint32(buffer[2])<<16 | uint32(buffer[3])<<24; got != entry.base {
		t.Fatalf("the reference reads as %#x, want the array's own address %#x", got, entry.base)
	}
	if err := heap.write(address, []byte{0, 0, 0, 0}); err == nil {
		t.Fatal("a write to a reference field was accepted")
	}
}

// TestHeapMapReadsGapsAsZero covers the sweep: the engine reads a region in
// chunks, and a chunk crossing the space between two spans must not fail, or
// the spans on both sides of the gap are thrown away with it.
func TestHeapMapReadsGapsAsZero(t *testing.T) {
	_, heap, hero := heapFixture(t)
	heap.refresh()
	entry := heap.byIdentity[heap.vm.Identity(hero)]

	buffer := make([]byte, int(entry.size)+64)
	if err := heap.read(entry.base, buffer); err != nil {
		t.Fatalf("a read running past the end of a span failed: %v", err)
	}
	for index := int(entry.size); index < len(buffer); index++ {
		if buffer[index] != 0 {
			t.Fatalf("byte %d past the span reads as %d, want zero", index, buffer[index])
		}
	}
	if err := heap.write(entry.base+entry.size+16, []byte{1, 2, 3, 4}); err == nil {
		t.Fatal("a write into the gap between spans was accepted")
	}
}

// TestHeapMapDrivesTheEngine is the whole point: the shared cheat session,
// unchanged, finds a value in a MIDlet's state and freezes it.
func TestHeapMapDrivesTheEngine(t *testing.T) {
	vm, heap, hero := heapFixture(t)
	if err := vm.SetField(hero, "game/Hero", "gold", "I", jvm.IntValue(4242)); err != nil {
		t.Fatal(err)
	}
	engine := cheat.NewSession(heapTarget{heap: heap})

	found, err := engine.Scan(cheat.ScanFilter{Op: cheat.FilterEq, A: 4242})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("a scan for a value the game holds found nothing")
	}
	address := fieldAddress(t, heap, hero, "gold")
	var seen bool
	for _, candidate := range engine.Candidates() {
		if candidate.Address == address {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("the scan did not find the field at %#x", address)
	}

	if _, err := engine.Freeze(address, cheat.ValueType{Kind: cheat.KindU32}, 9999, "gold"); err != nil {
		t.Fatal(err)
	}
	// The game writes over it, and the tick puts it back.
	if err := vm.SetField(hero, "game/Hero", "gold", "I", jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	if failed := engine.Tick(); len(failed) > 0 {
		t.Fatalf("the freeze failed at %#x", failed[0])
	}
	value, _ := vm.Field(hero, "game/Hero", "gold", "I")
	if number, _ := value.Int32(); number != 9999 {
		t.Fatalf("gold = %d after a tick with a freeze on it, want 9999", number)
	}
}

// heapTarget is the cheat target for a bare map, which is what cheatTarget is
// once the runtime's locking is taken off it.
type heapTarget struct{ heap *heapMap }

func (target heapTarget) ReadMemory(address uint32, destination []byte) error {
	return target.heap.read(address, destination)
}
func (target heapTarget) WriteMemory(address uint32, data []byte) error {
	return target.heap.write(address, data)
}
func (target heapTarget) Regions() []cheat.Region {
	target.heap.refresh()
	return target.heap.regions()
}
