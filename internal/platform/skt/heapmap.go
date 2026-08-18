package skt

import (
	"math"
	"sort"
	"weak"

	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// A cheat engine searches an address space, and this runtime does not have one:
// a MIDlet's state is Go objects in a map, and an `int` a game keeps its gold
// in has no address to find, narrow down and freeze. That is why the panel used
// to be removed on this platform.
//
// So one is built here. The object graph is walked from its roots and every
// object, array and class's statics is given a span of a synthetic address
// space; reading a span reads the fields live, and writing one writes them
// back. The engine above it is unchanged — it asks the same three questions of
// this that it asks of the two ARM platforms.
//
// Three things make the addresses worth what a cheat needs them to be worth:
//
//   - **They are stable.** An object keeps its span for the life of the
//     session, because the span is keyed by the VM's own object identity and
//     the allocator never reuses one. A value found on one screen is still at
//     that address on the next, which is the whole point of a scan.
//   - **They are grouped by shape.** Every instance of one class lands in that
//     class's own arena, so `regions` names what a hit is in — "the address is
//     in com/x/Player" is a thing a search can act on, where "somewhere in the
//     heap" is not.
//   - **They do not keep the game's garbage alive.** An entry holds a weak
//     reference, so mapping an object does not stop it being collected; a
//     frozen address holds a strong one, because a freeze the collector can
//     silently switch off is worse than the memory it costs.
//
// What it deliberately does not do is present the object *header* — there is
// no class word, no length word, no allocation metadata, because none of that
// exists here to present. A span is exactly the declared fields, or exactly the
// elements.

const (
	// heapMapBase is where the synthetic space starts. It is high enough that
	// a small number in a listing is obviously not an address.
	heapMapBase uint32 = 0x10000000
	// heapMapLimit is where it stops. Past this the map refuses to grow rather
	// than wrapping, which would put two objects at one address.
	heapMapLimit uint32 = 0xf0000000
	// heapChunkSize is how much address space an arena claims at a time. A
	// shape with three instances wastes the rest of one chunk, which costs
	// nothing but address space, and a scan only ever sweeps the used part.
	heapChunkSize uint32 = 1 << 16
	// heapEntryAlign keeps every span eight-byte aligned, so a long or a double
	// is aligned within it and the engine's default four-byte stride lands on
	// field boundaries.
	heapEntryAlign uint32 = 8
	// maxHeapObjects bounds one walk. The graph is the guest's, and a title
	// that has allocated a million objects should make the panel slow rather
	// than make the session run out of memory.
	maxHeapObjects = 200000
	// maxHeapArrayElements bounds how far into a reference array the walk
	// follows. A huge one is a table rather than a graph, and the walk's job is
	// to reach the objects a title keeps rather than every element of one.
	maxHeapArrayElements = 1 << 20
)

// heapShapeKind says what a span holds.
type heapShapeKind uint8

const (
	shapeInstance heapShapeKind = iota
	shapeArray
	shapeStatics
)

// heapSlot is one field or element inside a span.
type heapSlot struct {
	offset uint32
	width  uint32
	// kind is the field's type. A reference slot answers the mapped address of
	// what it points at and refuses writes; everything else round-trips.
	kind jvm.TypeKind
	// key is how the interpreter names an instance field. Empty for arrays.
	key string
	// field is the declaration a statics slot writes through, which needs the
	// class, name and descriptor rather than a key.
	field jvm.HeapField
}

// heapShape is one arena: a layout, and the chunks of address space the spans
// using it were cut from.
type heapShape struct {
	name string
	kind heapShapeKind
	// slots is the record layout for an instance or a statics span. An array
	// shape has none: its layout is one repeated element.
	slots []heapSlot
	// size is the record size for an instance or statics span.
	size uint32
	// element is the stride and type of an array shape's elements.
	elementWidth uint32
	elementKind  jvm.TypeKind

	chunks []*heapChunk
}

type heapChunk struct {
	base uint32
	// capacity is the address space claimed; used is how much of it holds spans.
	capacity uint32
	used     uint32
}

// heapEntry is one mapped object, array or class.
type heapEntry struct {
	base  uint32
	size  uint32
	shape *heapShape
	// ref is the object or array behind an instance or array span. It is weak
	// so that mapping something does not keep it alive; a dead one is dropped
	// on the next walk.
	ref weak.Pointer[jvm.Object]
	// pin is the same object held strongly, set while an address inside this
	// span is frozen. See heapMap.retain.
	pin *jvm.Object
	// length is an array span's element count.
	length int
}

// heapMap is the synthetic address space.
type heapMap struct {
	vm *jvm.VM
	// extraRoots are the objects the platform holds itself. The VM's own roots
	// are its statics and its threads; a MIDlet and the screen it is showing
	// are reachable from Go rather than from either.
	extraRoots func() []*jvm.Object

	shapes map[string]*heapShape
	// byIdentity maps the VM's object identity to a span. It is keyed by the
	// number rather than by the pointer so that this map is not itself a
	// reference that keeps every object ever scanned alive.
	byIdentity map[uint32]*heapEntry
	entries    []*heapEntry
	nextChunk  uint32
}

func newHeapMap(vm *jvm.VM, extraRoots func() []*jvm.Object) *heapMap {
	return &heapMap{
		vm:         vm,
		extraRoots: extraRoots,
		shapes:     map[string]*heapShape{},
		byIdentity: map[uint32]*heapEntry{},
		nextChunk:  heapMapBase,
	}
}

// refresh walks the object graph and maps everything it reaches that is not
// mapped already, then drops the spans whose object has been collected.
//
// It runs at the start of a sweep rather than on every read: a search narrows
// by re-reading the addresses it already has, and only a fresh sweep needs to
// know about objects the game has allocated since the last one.
func (heap *heapMap) refresh() {
	if heap == nil || heap.vm == nil {
		return
	}
	heap.mapStatics()

	visited := make(map[uint32]bool, len(heap.byIdentity))
	queue := append([]*jvm.Object(nil), heap.vm.HeapRoots()...)
	if heap.extraRoots != nil {
		queue = append(queue, heap.extraRoots()...)
	}
	for len(queue) > 0 && len(visited) < maxHeapObjects {
		object := queue[0]
		queue = queue[1:]
		if object == nil {
			continue
		}
		identity := heap.vm.Identity(object)
		if identity == 0 || visited[identity] {
			continue
		}
		visited[identity] = true
		queue = append(queue, heap.mapObject(identity, object)...)
	}
	heap.prune()
}

// mapObject gives an object a span if it does not have one, and answers the
// objects its own contents name.
func (heap *heapMap) mapObject(identity uint32, object *jvm.Object) []*jvm.Object {
	component, length, isArray := jvm.ArrayComponent(object)
	if isArray {
		heap.mapArray(identity, object, component, length)
		if !component.IsReference() {
			return nil
		}
		values, err := jvm.ArrayRange(object, 0, min(length, maxHeapArrayElements))
		if err != nil {
			return nil
		}
		children := make([]*jvm.Object, 0, len(values))
		for _, value := range values {
			if child, err := value.Reference(); err == nil && child != nil {
				children = append(children, child)
			}
		}
		return children
	}

	shape := heap.instanceShape(object.ClassName)
	if heap.byIdentity[identity] == nil && shape.size > 0 {
		heap.place(shape, &heapEntry{size: shape.size, shape: shape, ref: weak.Make(object)}, identity)
	}
	var children []*jvm.Object
	for _, slot := range shape.slots {
		if slot.kind != jvm.TypeReference && slot.kind != jvm.TypeArray {
			continue
		}
		value, ok := object.FieldValue(slot.key)
		if !ok {
			continue
		}
		if child, err := value.Reference(); err == nil && child != nil {
			children = append(children, child)
		}
	}
	return children
}

func (heap *heapMap) mapArray(identity uint32, object *jvm.Object, component jvm.Type, length int) {
	if heap.byIdentity[identity] != nil || length <= 0 {
		return
	}
	shape := heap.arrayShape(component)
	size := uint64(shape.elementWidth) * uint64(length)
	if size == 0 || size > math.MaxUint32 {
		return
	}
	heap.place(shape, &heapEntry{
		size: uint32(size), shape: shape, ref: weak.Make(object), length: length,
	}, identity)
}

// mapStatics gives every loaded class a span holding its static fields. They
// are a root of the graph and also the place a lot of a title's own state
// lives, and they have no object to be reached through.
func (heap *heapMap) mapStatics() {
	shape := heap.shapes["statics"]
	if shape == nil {
		shape = &heapShape{name: "statics", kind: shapeStatics}
		heap.shapes["statics"] = shape
	}
	for _, className := range heap.vm.LoadedClasses() {
		identity := staticsIdentity(className)
		if heap.byIdentity[identity] != nil {
			continue
		}
		fields := heap.vm.StaticLayout(className)
		slots, size := recordLayout(fields)
		if size == 0 {
			continue
		}
		// A statics span carries its own slots — every class's set is different
		// — while sharing one arena, so `regions` shows one span of statics
		// rather than one per class. The class it belongs to is named by the
		// slots themselves, which each carry their declaring class.
		entry := &heapEntry{
			size:  size,
			shape: &heapShape{name: shape.name, kind: shapeStatics, slots: slots, size: size},
		}
		heap.place(shape, entry, identity)
	}
}

// staticsIdentity is the map key for a class's statics. Object identities come
// from a counter that starts at one, so the high half of the space is free for
// keys that are not objects.
func staticsIdentity(className string) uint32 {
	hash := uint32(2166136261)
	for index := 0; index < len(className); index++ {
		hash ^= uint32(className[index])
		hash *= 16777619
	}
	return hash | 0x80000000
}

// place cuts a span out of the shape's arena and records it.
func (heap *heapMap) place(arena *heapShape, entry *heapEntry, identity uint32) {
	size := (entry.size + heapEntryAlign - 1) &^ (heapEntryAlign - 1)
	chunk := arena.roomFor(size)
	if chunk == nil {
		claimed := heap.claim(max(size, heapChunkSize))
		if claimed == nil {
			return
		}
		arena.chunks = append(arena.chunks, claimed)
		chunk = claimed
	}
	entry.base = chunk.base + chunk.used
	chunk.used += size
	heap.byIdentity[identity] = entry
	heap.entries = append(heap.entries, entry)
}

func (arena *heapShape) roomFor(size uint32) *heapChunk {
	if len(arena.chunks) == 0 {
		return nil
	}
	last := arena.chunks[len(arena.chunks)-1]
	if last.capacity-last.used >= size {
		return last
	}
	return nil
}

// claim takes address space for a new chunk, or answers nil when the map is
// full. A full map stops growing rather than reusing an address, because a
// reused address is a freeze landing on something that is not what was frozen.
func (heap *heapMap) claim(size uint32) *heapChunk {
	rounded := (size + heapChunkSize - 1) &^ (heapChunkSize - 1)
	if uint64(heap.nextChunk)+uint64(rounded) > uint64(heapMapLimit) {
		return nil
	}
	chunk := &heapChunk{base: heap.nextChunk, capacity: rounded}
	heap.nextChunk += rounded
	return chunk
}

// prune drops the spans whose object has been collected and re-sorts what is
// left, which is what every lookup binary-searches.
func (heap *heapMap) prune() {
	kept := heap.entries[:0]
	dropped := 0
	for _, entry := range heap.entries {
		if entry.shape.kind != shapeStatics && entry.pin == nil && entry.ref.Value() == nil {
			entry.base = 0
			dropped++
			continue
		}
		kept = append(kept, entry)
	}
	if dropped > 0 {
		// A dropped entry is recognized by the base it was cleared of rather
		// than by searching what survived: the identity map is the size of the
		// graph, and scanning the survivors once per identity would make a walk
		// quadratic in it.
		for identity, entry := range heap.byIdentity {
			if entry.base == 0 {
				delete(heap.byIdentity, identity)
			}
		}
	}
	heap.entries = kept
	sort.Slice(heap.entries, func(left, right int) bool {
		return heap.entries[left].base < heap.entries[right].base
	})
}

// retain holds strongly exactly the objects an address in addresses lands in,
// so a frozen value keeps writing to the object it was frozen on. Anything
// else goes back to being weakly held, which is what makes an unfreeze release
// the game's memory again.
func (heap *heapMap) retain(addresses []uint32) {
	if heap == nil {
		return
	}
	wanted := map[*heapEntry]bool{}
	for _, address := range addresses {
		if entry := heap.entryAt(address); entry != nil {
			wanted[entry] = true
		}
	}
	for _, entry := range heap.entries {
		switch {
		case wanted[entry] && entry.pin == nil:
			entry.pin = entry.ref.Value()
		case !wanted[entry] && entry.pin != nil:
			entry.pin = nil
		}
	}
}

// regions reports one region per chunk, merged where a shape's chunks came out
// contiguous. Only the used part of a chunk is reported: the rest is address
// space nothing has been placed in, and sweeping it would be sweeping zeroes.
func (heap *heapMap) regions() []cheat.Region {
	var regions []cheat.Region
	for _, shape := range heap.sortedShapes() {
		for _, chunk := range shape.chunks {
			if chunk.used == 0 {
				continue
			}
			last := len(regions) - 1
			if last >= 0 && regions[last].Label == shape.name && regions[last].Base+regions[last].Size == chunk.base {
				regions[last].Size += chunk.used
				continue
			}
			regions = append(regions, cheat.Region{Base: chunk.base, Size: chunk.used, Label: shape.name})
		}
	}
	sort.Slice(regions, func(left, right int) bool { return regions[left].Base < regions[right].Base })
	return regions
}

func (heap *heapMap) sortedShapes() []*heapShape {
	shapes := make([]*heapShape, 0, len(heap.shapes))
	for _, shape := range heap.shapes {
		shapes = append(shapes, shape)
	}
	sort.Slice(shapes, func(left, right int) bool { return shapes[left].name < shapes[right].name })
	return shapes
}

// entryAt finds the span an address falls in, or nil.
func (heap *heapMap) entryAt(address uint32) *heapEntry {
	index := sort.Search(len(heap.entries), func(position int) bool {
		return heap.entries[position].base+heap.entries[position].size > address
	})
	if index >= len(heap.entries) || heap.entries[index].base > address {
		return nil
	}
	return heap.entries[index]
}

// instanceShape is the arena every instance of a class is placed in.
func (heap *heapMap) instanceShape(className string) *heapShape {
	if shape := heap.shapes[className]; shape != nil {
		return shape
	}
	slots, size := recordLayout(heap.vm.InstanceLayout(className))
	shape := &heapShape{name: className, kind: shapeInstance, slots: slots, size: size}
	heap.shapes[className] = shape
	return shape
}

// arrayShape is the arena every array of one component type is placed in.
func (heap *heapMap) arrayShape(component jvm.Type) *heapShape {
	name := "[" + component.Descriptor()
	if shape := heap.shapes[name]; shape != nil {
		return shape
	}
	shape := &heapShape{
		name:         name,
		kind:         shapeArray,
		elementWidth: elementWidth(component.Kind),
		elementKind:  component.Kind,
	}
	heap.shapes[name] = shape
	return shape
}

// recordLayout lays a class's fields out. Every field takes four bytes except
// a long or a double, which take eight — a short in a four-byte slot is still
// found by a two-byte search, and a uniform stride is what makes an arena of
// one class's instances a table a scan can walk.
func recordLayout(fields []jvm.HeapField) ([]heapSlot, uint32) {
	var slots []heapSlot
	offset := uint32(0)
	for _, field := range fields {
		width := uint32(4)
		if field.Type.Kind == jvm.TypeLong || field.Type.Kind == jvm.TypeDouble {
			width = 8
			offset = (offset + 7) &^ 7
		}
		slots = append(slots, heapSlot{
			offset: offset,
			width:  width,
			kind:   field.Type.Kind,
			key:    field.Key(),
			field:  field,
		})
		offset += width
	}
	return slots, (offset + heapEntryAlign - 1) &^ (heapEntryAlign - 1)
}

// elementWidth is an array element's stride, which is its natural width rather
// than a uniform slot: a byte array is how a game packs its map data, and
// spreading it over four bytes each would hide every pattern in it.
func elementWidth(kind jvm.TypeKind) uint32 {
	switch kind {
	case jvm.TypeBoolean, jvm.TypeByte:
		return 1
	case jvm.TypeChar, jvm.TypeShort:
		return 2
	case jvm.TypeLong, jvm.TypeDouble:
		return 8
	default:
		return 4
	}
}
