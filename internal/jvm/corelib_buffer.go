package jvm

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"unicode/utf16"
)

// The members of java/lang/StringBuffer that the shared character buffer in
// builtins.go did not answer: its capacity, the insert forms beside the three
// the buffer already had, and reverse. StringBuilder shares the buffer and
// none of these, because CLDC has no StringBuilder and javac only builds one
// to concatenate.
//
// A StringBuffer's capacity is the length of the array CLDC's class library
// keeps its characters in. Nothing here holds one — the units grow on demand —
// but capacity() is a number a title can read and size its own work by, and it
// follows the library's rules: 16 for an empty buffer, a String's length and
// 16 more for one built from it, the count a title names, and on outgrowing it
// `(capacity + 1) * 2`, or the length needed when that is more. LGT answers the
// same numbers for the same calls (internal/platform/lgt), so a title sees one
// class library on the three platforms.
//
// The number never shrinks: delete, deleteCharAt and a shorter setLength leave
// it where it was, as they leave the class library's array.

// bufferDefaultCapacity is what an empty buffer starts with, and what a buffer
// built from a String gets on top of the String's length.
const bufferDefaultCapacity = 16

// bufferGrown is the capacity a buffer holding `length` units has, from the
// capacity it had: unchanged while the units fit, and otherwise the class
// library's growth. A count past what an int holds stops at the largest one,
// where the library's arithmetic would have gone negative and been replaced
// by it.
func bufferGrown(capacity int32, length int) int32 {
	if int64(length) <= int64(capacity) {
		return capacity
	}
	grown := (int64(capacity) + 1) * 2
	if int64(length) > grown {
		grown = int64(length)
	}
	return int32(min(grown, math.MaxInt32))
}

// restoredBufferCapacity is the capacity of a buffer a checkpoint from before
// capacities were kept restores: none was recorded, so it is given the one its
// text would have grown an empty buffer to. A title that reads it after a load
// sees a capacity that holds what the buffer holds, and growth goes on from
// there by the same rule.
func restoredBufferCapacity(length int) int32 {
	return bufferGrown(bufferDefaultCapacity, length)
}

// insertBufferUnits puts units into the receiver's buffer at the offset in the
// second argument and answers the receiver. An offset outside the buffer is
// the StringIndexOutOfBoundsException the class library throws, checked under
// the buffer's lock so a buffer another thread just shortened is refused
// rather than written past.
func (vm *VM) insertBufferUnits(arguments []Value, inserted []uint16) (Value, error) {
	object, data, err := stringBufferArgument(arguments)
	if err != nil {
		return VoidValue(), err
	}
	offset, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	data.mu.Lock()
	defer data.mu.Unlock()
	if offset < 0 || int(offset) > len(data.units) {
		return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException",
			fmt.Sprintf("StringBuffer.insert at %d of %d", offset, len(data.units)))
	}
	units, err := vm.unitsWithRoom(data.units, len(inserted))
	if err != nil {
		return VoidValue(), err
	}
	data.units = slices.Insert(units, int(offset), inserted...)
	data.grow()
	return ReferenceValue(object), nil
}

// charArrayUnits reads every unit of a char array as it is, a lone half of a
// surrogate pair included: the buffer holds units, and decoding them through
// a Go string would replace that half.
func charArrayUnits(array *Object) ([]uint16, error) {
	component, values, err := ArraySnapshot(array)
	if err != nil {
		return nil, err
	}
	if component.Kind != TypeChar {
		return nil, fmt.Errorf("argument is not a char array")
	}
	units := make([]uint16, len(values))
	for index, value := range values {
		unit, err := value.Int32()
		if err != nil {
			return nil, err
		}
		units[index] = uint16(unit)
	}
	return units, nil
}

func (vm *VM) registerBufferBuiltins() {
	const class = StringBufferClass
	const self = "L" + class + ";"
	vm.builtin(class, "capacity", "()I", func(_ *VM, arguments []Value) (Value, error) {
		_, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		capacity := data.capacity
		data.mu.Unlock()
		return IntValue(capacity), nil
	})
	// ensureCapacity grows the number as an append of that many units would,
	// and does nothing for a minimum the buffer already has — a negative one
	// included, which the class library ignores rather than refuses. Nothing
	// is allocated: the units still grow when they are written.
	vm.builtin(class, "ensureCapacity", "(I)V", func(_ *VM, arguments []Value) (Value, error) {
		_, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		minimum, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		if minimum > data.capacity {
			data.capacity = bufferGrown(data.capacity, int(minimum))
		}
		data.mu.Unlock()
		return VoidValue(), nil
	})
	// The boolean and long forms insert what the matching append adds.
	vm.builtin(class, "insert", "(IZ)"+self, func(vm *VM, arguments []Value) (Value, error) {
		value, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		return vm.insertBufferUnits(arguments, unitsOf(strconv.FormatBool(value != 0)))
	})
	vm.builtin(class, "insert", "(IJ)"+self, func(vm *VM, arguments []Value) (Value, error) {
		value, err := nativeLong(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		return vm.insertBufferUnits(arguments, unitsOf(strconv.FormatInt(value, 10)))
	})
	// The object form is insert(offset, String.valueOf(obj)), so the object's
	// own toString runs first — on the caller's execution, as the append of an
	// object does — and the offset is checked after it, as the class library
	// evaluates the call.
	vm.contextBuiltin(class, "insert", "(ILjava/lang/Object;)"+self, func(vm *VM, state *execution, arguments []Value) (Value, error) {
		if _, _, err := stringBufferArgument(arguments); err != nil {
			return VoidValue(), err
		}
		object, err := nativeReference(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		text, err := vm.objectText(state, object)
		if err != nil {
			return VoidValue(), err
		}
		return vm.insertBufferUnits(arguments, unitsOf(text))
	})
	// The char-array form checks the offset before it reads the array, as
	// the class library does: a bad offset is a StringIndexOutOfBoundsException
	// even when the array is null too, and a null array with a good offset is
	// a NullPointerException.
	vm.builtin(class, "insert", "(I[C)"+self, func(vm *VM, arguments []Value) (Value, error) {
		_, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		offset, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		array, err := nativeReference(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		length := len(data.units)
		data.mu.Unlock()
		if offset < 0 || int(offset) > length {
			return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException",
				fmt.Sprintf("StringBuffer.insert at %d of %d", offset, length))
		}
		if array == nil {
			return VoidValue(), guestException("java/lang/NullPointerException", "StringBuffer.insert char array")
		}
		units, err := charArrayUnits(array)
		if err != nil {
			return VoidValue(), err
		}
		return vm.insertBufferUnits(arguments, units)
	})
	// reverse turns every unit round. CLDC's predates surrogate pairs, so a
	// pair comes out as its two halves the other way about, as LGT's does.
	vm.builtin(class, "reverse", "()"+self, func(_ *VM, arguments []Value) (Value, error) {
		object, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		slices.Reverse(data.units)
		data.mu.Unlock()
		return ReferenceValue(object), nil
	})
}

// unitsOf is a string's UTF-16 units.
func unitsOf(text string) []uint16 {
	return utf16.Encode([]rune(text))
}
