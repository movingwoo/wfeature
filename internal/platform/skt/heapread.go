package skt

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// Reading and writing the synthetic space. A span is not stored anywhere — it
// is rendered from the fields every time it is read, and a write is decoded
// back into the field it lands on. That is what makes a scan see the game's
// state as it changes rather than a snapshot of it, and what makes a freeze a
// write to the object rather than to a shadow copy of it.
//
// A read that falls outside every span answers zeroes rather than an error.
// The engine sweeps a region in chunks and skips a chunk it cannot read, so an
// error over the gap between two spans would throw away the spans on both
// sides of it; a gap holds nothing, and nothing reads as zero.

func (heap *heapMap) read(address uint32, destination []byte) error {
	for offset := 0; offset < len(destination); {
		current := address + uint32(offset)
		entry := heap.entryAt(current)
		if entry == nil {
			offset += heap.fillGap(current, destination[offset:])
			continue
		}
		local := current - entry.base
		take := min(len(destination)-offset, int(entry.size-local))
		heap.readEntry(entry, local, destination[offset:offset+take])
		offset += take
	}
	return nil
}

// fillGap zeroes up to the next span, and answers how much it wrote.
func (heap *heapMap) fillGap(address uint32, destination []byte) int {
	length := len(destination)
	if next, ok := heap.nextBase(address); ok && uint64(next)-uint64(address) < uint64(length) {
		length = int(next - address)
	}
	if length <= 0 {
		length = len(destination)
	}
	clear(destination[:length])
	return length
}

// nextBase is the base of the first span at or after address. It binary
// searches rather than walking: a sweep crosses a gap between every pair of
// spans, and a linear answer here would make one quadratic in the graph.
func (heap *heapMap) nextBase(address uint32) (uint32, bool) {
	index := sort.Search(len(heap.entries), func(position int) bool {
		return heap.entries[position].base >= address
	})
	if index >= len(heap.entries) {
		return 0, false
	}
	return heap.entries[index].base, true
}

// readEntry renders the window [local, local+len(destination)) of one span.
func (heap *heapMap) readEntry(entry *heapEntry, local uint32, destination []byte) {
	clear(destination)
	switch entry.shape.kind {
	case shapeArray:
		heap.readArray(entry, local, destination)
	default:
		heap.readRecord(entry, local, destination)
	}
}

func (heap *heapMap) readRecord(entry *heapEntry, local uint32, destination []byte) {
	object := entry.object()
	if entry.shape.kind == shapeInstance && object == nil {
		return
	}
	end := local + uint32(len(destination))
	for _, slot := range entry.shape.slots {
		if slot.offset+slot.width <= local || slot.offset >= end {
			continue
		}
		var bytes [8]byte
		heap.encodeSlot(entry, slot, object, bytes[:slot.width])
		copyWindow(destination, local, slot.offset, bytes[:slot.width])
	}
}

func (heap *heapMap) readArray(entry *heapEntry, local uint32, destination []byte) {
	object := entry.object()
	if object == nil {
		return
	}
	width := entry.shape.elementWidth
	first := int(local / width)
	last := int((local + uint32(len(destination)) + width - 1) / width)
	last = min(last, entry.length)
	if first >= last {
		return
	}
	values, err := jvm.ArrayRange(object, first, last-first)
	if err != nil {
		return
	}
	for index, value := range values {
		offset := uint32(first+index) * width
		var bytes [8]byte
		heap.encodeValue(value, entry.shape.elementKind, bytes[:width])
		copyWindow(destination, local, offset, bytes[:width])
	}
}

// copyWindow writes a slot's bytes into the part of destination that overlaps
// it, where destination starts at windowStart within the span.
func copyWindow(destination []byte, windowStart, slotOffset uint32, bytes []byte) {
	for index := range bytes {
		position := int(slotOffset) + index - int(windowStart)
		if position >= 0 && position < len(destination) {
			destination[position] = bytes[index]
		}
	}
}

// encodeSlot renders one field of a record.
func (heap *heapMap) encodeSlot(entry *heapEntry, slot heapSlot, object *jvm.Object, destination []byte) {
	var value jvm.Value
	if entry.shape.kind == shapeStatics {
		read, ok := heap.vm.StaticFieldValue(slot.field.Class, slot.field.Name, slot.field.Descriptor)
		if !ok {
			// A class that has not initialized holds no values yet, and its
			// fields read as the zero destination already is.
			return
		}
		value = read
	} else {
		read, ok := object.FieldValue(slot.key)
		if !ok {
			// A field nobody has written yet holds its type's zero, which is
			// already what destination reads as.
			return
		}
		value = read
	}
	heap.encodeValue(value, slot.kind, destination)
}

// encodeValue renders one value little-endian. A reference reads as the
// address of what it points at, which is what lets a listing be followed from
// one object into the next; zero means null, or an object the walk has not
// mapped.
func (heap *heapMap) encodeValue(value jvm.Value, kind jvm.TypeKind, destination []byte) {
	switch kind {
	case jvm.TypeReference, jvm.TypeArray:
		reference, err := value.Reference()
		if err != nil || reference == nil {
			return
		}
		if entry := heap.byIdentity[heap.vm.Identity(reference)]; entry != nil && len(destination) >= 4 {
			binary.LittleEndian.PutUint32(destination, entry.base)
		}
	case jvm.TypeLong:
		number, err := value.Int64()
		if err == nil && len(destination) >= 8 {
			binary.LittleEndian.PutUint64(destination, uint64(number))
		}
	case jvm.TypeDouble:
		number, err := value.Float64()
		if err == nil && len(destination) >= 8 {
			binary.LittleEndian.PutUint64(destination, math.Float64bits(number))
		}
	case jvm.TypeFloat:
		number, err := value.Float32()
		if err == nil && len(destination) >= 4 {
			binary.LittleEndian.PutUint32(destination, math.Float32bits(number))
		}
	default:
		number, err := value.Int32()
		if err != nil {
			return
		}
		raw := uint32(number)
		for index := range destination {
			destination[index] = byte(raw >> (8 * index))
		}
	}
}

// write puts data back into the fields the addresses land on.
//
// A write narrower than the slot it lands in is a read-modify-write: the
// engine's one-byte type is a legitimate way to change a byte of a packed
// array, and it is also how a careless `set` lands halfway through an `int`.
// Doing it by patching the rendered bytes means both cases behave the way the
// same write against a real address space would.
func (heap *heapMap) write(address uint32, data []byte) error {
	written := 0
	for offset := 0; offset < len(data); {
		current := address + uint32(offset)
		entry := heap.entryAt(current)
		if entry == nil {
			next, ok := heap.nextBase(current)
			if !ok {
				break
			}
			offset += int(next - current)
			continue
		}
		local := current - entry.base
		take := min(len(data)-offset, int(entry.size-local))
		count, err := heap.writeEntry(entry, local, data[offset:offset+take])
		if err != nil {
			return err
		}
		written += count
		offset += take
	}
	if written == 0 {
		return fmt.Errorf("%#x is not an address this game has anything at", address)
	}
	return nil
}

func (heap *heapMap) writeEntry(entry *heapEntry, local uint32, data []byte) (int, error) {
	if entry.shape.kind == shapeArray {
		return heap.writeArray(entry, local, data)
	}
	return heap.writeRecord(entry, local, data)
}

func (heap *heapMap) writeRecord(entry *heapEntry, local uint32, data []byte) (int, error) {
	object := entry.object()
	if entry.shape.kind == shapeInstance && object == nil {
		return 0, fmt.Errorf("the object at %#x has been collected", entry.base)
	}
	end := local + uint32(len(data))
	written := 0
	for _, slot := range entry.shape.slots {
		if slot.offset+slot.width <= local || slot.offset >= end {
			continue
		}
		if slot.kind == jvm.TypeReference || slot.kind == jvm.TypeArray {
			return written, fmt.Errorf("%#x is a reference field, which this cannot rewrite",
				entry.base+slot.offset)
		}
		var bytes [8]byte
		heap.encodeSlot(entry, slot, object, bytes[:slot.width])
		patchWindow(bytes[:slot.width], local, slot.offset, data)
		value, err := decodeValue(bytes[:slot.width], slot.kind)
		if err != nil {
			return written, err
		}
		if entry.shape.kind == shapeStatics {
			err = heap.vm.SetStaticFieldValue(slot.field.Class, slot.field.Name, slot.field.Descriptor, value)
		} else {
			object.SetFieldValue(slot.key, value)
		}
		if err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func (heap *heapMap) writeArray(entry *heapEntry, local uint32, data []byte) (int, error) {
	object := entry.object()
	if object == nil {
		return 0, fmt.Errorf("the array at %#x has been collected", entry.base)
	}
	if entry.shape.elementKind == jvm.TypeReference || entry.shape.elementKind == jvm.TypeArray {
		return 0, fmt.Errorf("%#x is a reference array, which this cannot rewrite", entry.base)
	}
	width := entry.shape.elementWidth
	first := int(local / width)
	last := min(int((local+uint32(len(data))+width-1)/width), entry.length)
	written := 0
	for index := first; index < last; index++ {
		offset := uint32(index) * width
		var bytes [8]byte
		values, err := jvm.ArrayRange(object, index, 1)
		if err != nil {
			return written, err
		}
		heap.encodeValue(values[0], entry.shape.elementKind, bytes[:width])
		patchWindow(bytes[:width], local, offset, data)
		value, err := decodeValue(bytes[:width], entry.shape.elementKind)
		if err != nil {
			return written, err
		}
		if err := jvm.SetArrayElement(object, index, value); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// patchWindow copies the part of data that overlaps a slot into that slot's
// rendered bytes, where data starts at windowStart within the span.
func patchWindow(bytes []byte, windowStart, slotOffset uint32, data []byte) {
	for index := range bytes {
		position := int(slotOffset) + index - int(windowStart)
		if position >= 0 && position < len(data) {
			bytes[index] = data[position]
		}
	}
}

// decodeValue turns a slot's bytes back into the value its type holds. Every
// narrow type is stored in the interpreter as an int, so what changes between
// them is the range the bytes are read over: a byte slot writes back a signed
// byte, and a char an unsigned two-byte number.
func decodeValue(bytes []byte, kind jvm.TypeKind) (jvm.Value, error) {
	switch kind {
	case jvm.TypeLong:
		return jvm.LongValue(int64(binary.LittleEndian.Uint64(bytes))), nil
	case jvm.TypeDouble:
		return jvm.DoubleValue(math.Float64frombits(binary.LittleEndian.Uint64(bytes))), nil
	case jvm.TypeFloat:
		return jvm.FloatValue(math.Float32frombits(binary.LittleEndian.Uint32(bytes))), nil
	}
	var raw uint32
	for index := len(bytes) - 1; index >= 0; index-- {
		raw = raw<<8 | uint32(bytes[index])
	}
	switch kind {
	case jvm.TypeBoolean:
		if raw&1 != 0 {
			return jvm.IntValue(1), nil
		}
		return jvm.IntValue(0), nil
	case jvm.TypeByte:
		return jvm.IntValue(int32(int8(raw))), nil
	case jvm.TypeChar:
		return jvm.IntValue(int32(uint16(raw))), nil
	case jvm.TypeShort:
		return jvm.IntValue(int32(int16(raw))), nil
	case jvm.TypeInt:
		return jvm.IntValue(int32(raw)), nil
	}
	return jvm.VoidValue(), fmt.Errorf("a %v field is not something this can write", kind)
}

// object answers the object behind a span, whether it is held strongly for a
// freeze or weakly for everything else.
func (entry *heapEntry) object() *jvm.Object {
	if entry.pin != nil {
		return entry.pin
	}
	return entry.ref.Value()
}
