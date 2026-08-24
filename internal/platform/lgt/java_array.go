package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Arrays.
//
// **An array is an object like any other**, and its data block is what makes it
// one: the block at `object + 8` opens with the length and carries the elements
// from four bytes in. That is read off a bounds check rather than assumed —
// every one in a module has this shape:
//
//	ldr   r3, [r6, #8]          ; r6 is the array; r3 is its block
//	ldr   r2, [r3]              ; the length
//	add   r0, r3, r4, lsl #1    ; the element, two bytes wide here
//	cmp   r4, r2
//	blo   ok
//	bl    0x64/0x23             ; the throw, with the index and the bound
//	ok:
//	strh  r1, [r0, #4]          ; the elements start one word in
//
// The shift is what says how wide an element is, and the platform has to agree
// with it: the type call below is where the width is decided, and getting it
// wrong is not a fault the guest reports — it is two arrays sharing a word.
//
// Building one takes two calls. `0x0e` resolves the type and `0x10` allocates
// with a length. What told the two apart is what the length argument is at the
// call sites: literal 3, 20, 50 and 62 in some, and the answer to a method call
// in others, which is a length and cannot be anything else.

const (
	// The JVM's own `newarray` codes, which this table numbers its primitives
	// with, and one of its own for a reference.
	javaArrayReference = 1
	javaArrayBoolean   = 4
	javaArrayChar      = 5
	javaArrayFloat     = 6
	javaArrayDouble    = 7
	javaArrayByte      = 8
	javaArrayShort     = 9
	javaArrayInt       = 10
	javaArrayLong      = 11

	javaArrayLengthWords = 1
	// A length past this is a misread rather than an array: the largest a local
	// title asks for is in the thousands.
	maxJavaArrayLength = 1 << 22
	maxJavaDimensions  = 8
)

// javaArrayElement is what one element of an array type takes and what the type
// is called.
func javaArrayElement(code uint32) (bytes uint32, descriptor string, ok bool) {
	switch code {
	case javaArrayBoolean:
		return 1, "Z", true
	case javaArrayByte:
		return 1, "B", true
	case javaArrayChar:
		return 2, "C", true
	case javaArrayShort:
		return 2, "S", true
	case javaArrayFloat:
		return 4, "F", true
	case javaArrayInt:
		return 4, "I", true
	case javaArrayDouble:
		return 8, "D", true
	case javaArrayLong:
		return 8, "J", true
	}
	return 0, "", false
}

// resolveJavaArrayType answers the array type call: how many dimensions, the
// element class when there is one, and the element's own code.
//
// **The class argument is only meaningful for an array of references.** A
// module leaves whatever was in the register when the code names a primitive —
// two local titles pass zero and two pass a live pointer — so it is read only
// when the code asks for it.
func (client *Client) resolveJavaArrayType(
	dimensions, element, code uint32,
) (*javaRuntimeClass, error) {
	if dimensions == 0 || dimensions > maxJavaDimensions {
		return nil, fmt.Errorf("an array of %d dimensions is not one", dimensions)
	}
	bytes, descriptor, ok := javaArrayElement(code)
	if !ok {
		if code != javaArrayReference {
			return nil, fmt.Errorf("array element code %d is not one this platform reads", code)
		}
		name, named := client.readPrintableString(element)
		if !named {
			return nil, fmt.Errorf("an array of references names no class at %#x", element)
		}
		bytes, descriptor = 4, "L"+name+";"
	}
	return client.javaArrayType(dimensions, descriptor, bytes)
}

// javaArrayType lays out one array class: its name is its descriptor, and what
// the platform has to get right is the width of one element, because the
// compiled code holds that in the shift of every access.
func (client *Client) javaArrayType(
	dimensions uint32, descriptor string, bytes uint32,
) (*javaRuntimeClass, error) {
	// Only the innermost dimension holds elements of that width: every one
	// above it holds references to the arrays below.
	if dimensions > 1 {
		bytes = 4
	}
	name := descriptor
	for index := uint32(0); index < dimensions; index++ {
		name = "[" + name
	}
	class, err := client.preparePlatformJavaClass(name)
	if err != nil {
		return nil, err
	}
	class.ElementBytes = bytes
	return class, nil
}

// allocateJavaArray answers the allocation call: an object whose block is a
// length and that many elements.
func (client *Client) allocateJavaArray(object, length uint32) (uint32, error) {
	runtime := client.javaRuntimeState()
	class, ok := runtime.byObject[object]
	if !ok {
		return 0, fmt.Errorf("the array type %#x was not issued here", object)
	}
	if class.ElementBytes == 0 {
		return 0, fmt.Errorf("%s is not an array type", class.Name)
	}
	if length > maxJavaArrayLength {
		return 0, fmt.Errorf("an array of %d %s is not one", length, class.Name)
	}
	size := uint64(javaArrayLengthWords)*4 + uint64(length)*uint64(class.ElementBytes)
	block, err := client.allocate(size)
	if err != nil {
		return 0, err
	}
	if err := client.core.Memory().Write(block, make([]byte, size)); err != nil {
		return 0, err
	}
	if err := client.writeWord(block, length); err != nil {
		return 0, err
	}
	return client.allocateWords([]uint32{class.VTable, 0, block})
}

// javaArrayClassByName answers the array class one dimension in from another —
// `[B` from `[[B` — building it if this is the first ask. The name is the
// descriptor, so it is also the parser: what follows the bracket says how wide
// an element is.
func (client *Client) javaArrayClassByName(name string) (*javaRuntimeClass, error) {
	if len(name) < 2 || name[0] != '[' {
		return nil, fmt.Errorf("%q is not an array type", name)
	}
	bytes := uint32(4)
	if name[1] != '[' && name[1] != 'L' {
		width, _, ok := javaArrayElement(javaArrayCodeOf(name[1]))
		if !ok {
			return nil, fmt.Errorf("%q names no element width", name)
		}
		bytes = width
	}
	class, err := client.preparePlatformJavaClass(name)
	if err != nil {
		return nil, err
	}
	class.ElementBytes = bytes
	return class, nil
}

func javaArrayCodeOf(descriptor byte) uint32 {
	switch descriptor {
	case 'Z':
		return javaArrayBoolean
	case 'B':
		return javaArrayByte
	case 'C':
		return javaArrayChar
	case 'S':
		return javaArrayShort
	case 'F':
		return javaArrayFloat
	case 'I':
		return javaArrayInt
	case 'D':
		return javaArrayDouble
	case 'J':
		return javaArrayLong
	}
	return 0
}

// allocateJavaMultiArray answers the call that builds an array of arrays: the
// type, a pointer to one length per dimension, and how many there are. The
// outer array holds references to the arrays below it, and those are built here
// too — which is what a Java `new int[a][b]` is.
func (client *Client) allocateJavaMultiArray(object, lengths, count uint32) (uint32, error) {
	class, ok := client.javaRuntimeState().byObject[object]
	if !ok {
		return 0, fmt.Errorf("the array type %#x was not issued here", object)
	}
	if count == 0 || count > maxJavaDimensions {
		return 0, fmt.Errorf("an array of %d dimensions is not one", count)
	}
	sizes := make([]uint32, count)
	for index := range sizes {
		length, err := client.readWord(lengths + uint32(index)*4)
		if err != nil {
			return 0, err
		}
		sizes[index] = length
	}
	return client.buildJavaMultiArray(class, sizes)
}

func (client *Client) buildJavaMultiArray(class *javaRuntimeClass, sizes []uint32) (uint32, error) {
	array, err := client.allocateJavaArray(class.Object, sizes[0])
	if err != nil {
		return 0, err
	}
	if len(sizes) == 1 {
		return array, nil
	}
	inner, err := client.javaArrayClassByName(class.Name[1:])
	if err != nil {
		return 0, err
	}
	for index := uint32(0); index < sizes[0]; index++ {
		element, err := client.buildJavaMultiArray(inner, sizes[1:])
		if err != nil {
			return 0, err
		}
		if err := client.storeJavaArrayReference(array, index, element); err != nil {
			return 0, err
		}
	}
	return array, nil
}

// storeJavaArrayReference writes one reference into an array, bounds checked
// against the length the array itself carries.
//
// **A reference store is a platform call and a primitive store is not**, which
// is the shape of the compiled code: a byte or a short is written inline after
// an inline bounds check, and this is reached only for an element that is an
// object. What a real runtime does here that this does not is the store check —
// the one that answers ArrayStoreException — and it cannot be done from what
// the module hands over: an array type carries its element's name, but an
// object carries no type this platform can compare it against.
func (client *Client) storeJavaArrayReference(array, index, value uint32) error {
	block, err := client.readWord(array + 8)
	if err != nil {
		return err
	}
	length, err := client.readWord(block)
	if err != nil {
		return err
	}
	if index >= length {
		return fmt.Errorf("index %d of an array of %d", index, length)
	}
	return client.writeWord(block+4+index*4, value)
}

// javaArrayCopy is `System.arraycopy`. **The element width is the array's, not
// the call's**: the call counts elements and the platform has to turn that into
// bytes, so it asks each array's own type — which is the same width the
// compiled code shifts by at every access of it.
func javaArrayCopy(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	source, sourceFrom := arguments[0], arguments[1]
	target, targetFrom, count := arguments[2], arguments[3], arguments[4]
	sourceBytes, err := client.javaArrayElementBytes(source)
	if err != nil {
		return 0, fmt.Errorf("the source: %w", err)
	}
	targetBytes, err := client.javaArrayElementBytes(target)
	if err != nil {
		return 0, fmt.Errorf("the target: %w", err)
	}
	if sourceBytes != targetBytes {
		return 0, fmt.Errorf("a copy between %d-byte and %d-byte elements", sourceBytes, targetBytes)
	}
	sourceBlock, sourceLength, err := client.javaArrayBlock(source)
	if err != nil {
		return 0, err
	}
	targetBlock, targetLength, err := client.javaArrayBlock(target)
	if err != nil {
		return 0, err
	}
	if uint64(sourceFrom)+uint64(count) > uint64(sourceLength) ||
		uint64(targetFrom)+uint64(count) > uint64(targetLength) {
		return 0, fmt.Errorf("%d elements from %d of %d into %d of %d is past an end",
			count, sourceFrom, sourceLength, targetFrom, targetLength)
	}
	if count == 0 {
		return 0, nil
	}
	data := make([]byte, uint64(count)*uint64(sourceBytes))
	from := sourceBlock + javaArrayLengthWords*4 + sourceFrom*sourceBytes
	to := targetBlock + javaArrayLengthWords*4 + targetFrom*targetBytes
	if err := client.core.Memory().Read(from, data); err != nil {
		return 0, err
	}
	// Overlap is the caller's to have, and the language defines the copy as if
	// it went through a temporary, which is what reading it all first is.
	return 0, client.core.Memory().Write(to, data)
}

// javaArrayElementBytes answers how wide one element of an array object is.
func (client *Client) javaArrayElementBytes(object uint32) (uint32, error) {
	class, ok := client.javaClassOfObject(object)
	if !ok {
		return 0, fmt.Errorf("the array at %#x was not issued here", object)
	}
	if class.ElementBytes == 0 {
		return 0, fmt.Errorf("%s is not an array type", class.Name)
	}
	return class.ElementBytes, nil
}

// javaArrayBlock answers where an array's elements are and how many there are.
func (client *Client) javaArrayBlock(object uint32) (uint32, uint32, error) {
	block, err := client.readWord(object + 8)
	if err != nil {
		return 0, 0, err
	}
	length, err := client.readWord(block)
	if err != nil {
		return 0, 0, err
	}
	if length > maxJavaArrayLength {
		return 0, 0, fmt.Errorf("an array of %d elements is not one", length)
	}
	return block, length, nil
}

// newJavaByteArray and newJavaStringArray build an array this platform fills
// and the title reads. A call that answers an array has to allocate one the
// module can index, which is the same object shape `new` builds.
func (client *Client) newJavaByteArray(data []byte) (uint32, error) {
	class, err := client.javaArrayClassByName("[B")
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaArray(class.Object, uint32(len(data)))
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return object, nil
	}
	block, err := client.readWord(object + 8)
	if err != nil {
		return 0, err
	}
	if err := client.core.Memory().Write(block+javaArrayLengthWords*4, data); err != nil {
		return 0, err
	}
	return object, nil
}

func (client *Client) newJavaStringArray(values []string) (uint32, error) {
	class, err := client.javaArrayClassByName("[Ljava/lang/String;")
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaArray(class.Object, uint32(len(values)))
	if err != nil {
		return 0, err
	}
	block, err := client.readWord(object + 8)
	if err != nil {
		return 0, err
	}
	for index, value := range values {
		text, err := client.newJavaString(value)
		if err != nil {
			return 0, err
		}
		if err := client.writeWord(block+javaArrayLengthWords*4+uint32(index)*4, text); err != nil {
			return 0, err
		}
	}
	return object, nil
}
