package lgt

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/util/Vector`, which a title uses as a growable list of references.
//
// The module lists no virtual methods for it, so **every call but the
// constructor arrives as a slot the compiler baked** and each one is named from
// its call site the same way a String slot is. What a vector holds is kept on
// this side, keyed by the object the module allocated, exactly as a String's
// characters are: the module never reads a vector's own words.

const javaVectorClass = "java/util/Vector"

// javaVectorConstructor is `Vector()`, `Vector(int)` and `Vector(int, int)`.
// All three build the same empty list; what the arguments choose is the
// capacity `capacity()` answers and how it grows (javaVectorShape), and a
// negative capacity is the IllegalArgumentException the class library throws.
func javaVectorConstructor(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	shape := javaVectorShape{capacity: javaVectorDefaultCapacity}
	if len(arguments) > 1 {
		shape.capacity = int32(arguments[1])
	}
	if len(arguments) > 2 {
		shape.increment = int32(arguments[2])
	}
	if shape.capacity < 0 {
		return 0, client.throwJavaPlatform(thread, javaIllegalArgumentClass,
			fmt.Sprintf(": Vector(%d)", shape.capacity))
	}
	runtime := client.javaRuntimeState()
	runtime.vectors[arguments[0]] = []uint32{}
	runtime.vectorShapes[arguments[0]] = shape
	return 0, nil
}

// javaVectorShape is the capacity of the array the class library would hold a
// vector's elements in, and the step it grows by: nothing here holds the
// array, but `capacity()` answers its length, and the length follows the
// library's own rules — 10 to start with, the step on growing when one was
// given, and otherwise double, or what is needed if that is more.
type javaVectorShape struct {
	capacity, increment int32
}

const javaVectorDefaultCapacity = 10

// javaVectorShapeOf answers a vector's shape. A vector built before shapes
// were kept — restored from an earlier build's checkpoint, or answered by a
// platform call rather than constructed — is given the one an empty vector
// would have grown to holding what it holds.
func (client *Client) javaVectorShapeOf(object uint32) javaVectorShape {
	runtime := client.javaRuntimeState()
	shape, ok := runtime.vectorShapes[object]
	if !ok {
		shape = javaVectorShape{capacity: javaVectorDefaultCapacity}
		shape.grow(len(runtime.vectors[object]))
		runtime.vectorShapes[object] = shape
	}
	return shape
}

// grow is the class library's ensureCapacityHelper.
func (shape *javaVectorShape) grow(minimum int) {
	if int64(minimum) <= int64(shape.capacity) {
		return
	}
	grown := int64(shape.capacity) * 2
	if shape.increment > 0 {
		grown = int64(shape.capacity) + int64(shape.increment)
	}
	if grown < int64(minimum) {
		grown = int64(minimum)
	}
	shape.capacity = int32(min(grown, maxJavaArrayLength))
}

// javaVectorGrow makes room for `minimum` elements, the way an add does.
func (client *Client) javaVectorGrow(object uint32, minimum int) {
	shape := client.javaVectorShapeOf(object)
	shape.grow(minimum)
	client.javaRuntimeState().vectorShapes[object] = shape
}

// javaVectorOf answers the list behind a vector object.
func (client *Client) javaVectorOf(object uint32) ([]uint32, error) {
	runtime := client.javaRuntimeState()
	held, ok := runtime.vectors[object]
	if !ok {
		return nil, fmt.Errorf("the object at %#x is not a vector this platform built", object)
	}
	return held, nil
}

// javaVectorRoom reports a vector that cannot take one more element. What a
// vector holds is an Object array in the language, and no array here is longer
// than maxJavaArrayLength, so a vector that long is a runaway loop rather than
// a list; stopping it here is what keeps the loop from growing the host until
// the process dies.
func javaVectorRoom(held []uint32) error {
	if len(held) >= maxJavaArrayLength {
		return fmt.Errorf("a vector of %d elements cannot take another: %d is the most one holds",
			len(held), maxJavaArrayLength)
	}
	return nil
}

// javaVectorAdd is `addElement(Object)`: one more reference on the end.
func javaVectorAdd(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if err := javaVectorRoom(held); err != nil {
		return 0, err
	}
	client.javaVectorGrow(arguments[0], len(held)+1)
	client.javaRuntimeState().vectors[arguments[0]] = append(held, arguments[1])
	return 0, nil
}

// javaVectorIndexOf is `indexOf(Object)`, slot 19, and `indexOf(Object, int)`,
// slot 20: where the first element equal to the argument sits, from an index,
// or -1. **Equal is the argument's own equals**, as the class library asks
// it — `elem.equals(elementData[i])` — so a String the title built again
// finds the one it put in; null finds a null element. A negative start is the
// array index failure the library's loop runs into.
func javaVectorIndexOf(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	from := int64(0)
	if len(arguments) > 2 {
		from = int64(int32(arguments[2]))
	}
	if from < 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowArrayClass,
			fmt.Sprintf(": indexOf from %d", from))
	}
	for index := from; index < int64(len(held)); index++ {
		equal, err := client.javaVectorMatches(ctx, thread, arguments[1], held[index])
		if err != nil {
			return 0, err
		}
		if equal {
			return uint32(index), nil
		}
	}
	return ^uint32(0), nil
}

// javaVectorMatches is the test every search of a vector makes: null against
// null, and otherwise the object being looked for asked whether it equals the
// element.
func (client *Client) javaVectorMatches(
	ctx context.Context, thread *armcore.Thread, wanted, element uint32,
) (bool, error) {
	if wanted == 0 {
		return element == 0, nil
	}
	return client.javaEqualsOf(ctx, thread, wanted, element)
}

// javaVectorLastIndexOf is `lastIndexOf(Object)`, slot 21, and
// `lastIndexOf(Object, int)`, slot 22: the same search from the end, or from an
// index backwards. An index at or past the size is the IndexOutOfBoundsException
// the class library throws; a negative one finds nothing.
func javaVectorLastIndexOf(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	from := int64(len(held)) - 1
	if len(arguments) > 2 {
		from = int64(int32(arguments[2]))
		if from >= int64(len(held)) {
			return 0, client.throwJavaPlatform(thread, javaIndexClass,
				fmt.Sprintf(": %d >= %d", from, len(held)))
		}
	}
	for index := from; index >= 0; index-- {
		equal, err := client.javaVectorMatches(ctx, thread, arguments[1], held[index])
		if err != nil {
			return 0, err
		}
		if equal {
			return uint32(index), nil
		}
	}
	return ^uint32(0), nil
}

// javaVectorContains is `contains(Object)`, slot 18: whether indexOf finds it.
func javaVectorContains(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	index, err := javaVectorIndexOf(client, ctx, thread, arguments[:2])
	if err != nil {
		return 0, err
	}
	return javaBoolean(int32(index) >= 0), nil
}

// javaVectorRemoveElement is `removeElement(Object)`, slot 30: the first
// element indexOf finds goes, and the answer says whether there was one.
func javaVectorRemoveElement(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	index, err := javaVectorIndexOf(client, ctx, thread, arguments[:2])
	if err != nil {
		return 0, err
	}
	if int32(index) < 0 {
		return javaFalse, nil
	}
	if _, err := javaVectorRemoveAt(client, ctx, thread, []uint32{arguments[0], index}); err != nil {
		return 0, err
	}
	return javaTrue, nil
}

// javaVectorLast is `lastElement()`, slot 25. An empty vector is the
// NoSuchElementException the class library throws.
func javaVectorLast(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if len(held) == 0 {
		return 0, client.throwJavaPlatform(thread, javaNoSuchElementClass, ": the last element of an empty vector")
	}
	return held[len(held)-1], nil
}

// javaVectorCapacity is `capacity()`, slot 14.
func javaVectorCapacity(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, err := client.javaVectorOf(arguments[0]); err != nil {
		return 0, err
	}
	return uint32(client.javaVectorShapeOf(arguments[0]).capacity), nil
}

// javaVectorEnsureCapacity is `ensureCapacity(int)`, slot 12.
func javaVectorEnsureCapacity(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, err := client.javaVectorOf(arguments[0]); err != nil {
		return 0, err
	}
	if minimum := int32(arguments[1]); minimum > 0 {
		client.javaVectorGrow(arguments[0], int(min(minimum, maxJavaArrayLength)))
	}
	return 0, nil
}

// javaVectorTrimToSize is `trimToSize()`, slot 11: the capacity becomes the
// size.
func javaVectorTrimToSize(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	shape := client.javaVectorShapeOf(arguments[0])
	if int64(len(held)) < int64(shape.capacity) {
		shape.capacity = int32(len(held))
	}
	client.javaRuntimeState().vectorShapes[arguments[0]] = shape
	return 0, nil
}

// javaVectorSetSize is `setSize(int)`, slot 13: nulls on the end up to the
// size, or the elements past it gone. A negative size is the array index
// failure the class library's loop runs into.
func javaVectorSetSize(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	size := int64(int32(arguments[1]))
	if size < 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowArrayClass, fmt.Sprintf(": setSize(%d)", size))
	}
	if size > maxJavaArrayLength {
		return 0, fmt.Errorf("a vector of %d elements: %d is the most one holds", size, maxJavaArrayLength)
	}
	if size > int64(len(held)) {
		client.javaVectorGrow(arguments[0], int(size))
		held = append(held, make([]uint32, size-int64(len(held)))...)
	} else {
		held = held[:size:size]
	}
	client.javaRuntimeState().vectors[arguments[0]] = held
	return 0, nil
}

// javaVectorToString is `toString()`, slot 4: every element's own toString,
// between brackets and after commas, as the class library writes it.
func javaVectorToString(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	parts := make([]string, 0, len(held))
	for _, element := range append([]uint32(nil), held...) {
		text, err := client.javaToStringOf(ctx, thread, element)
		if err != nil {
			return 0, err
		}
		parts = append(parts, text)
	}
	return client.newJavaString("[" + strings.Join(parts, ", ") + "]")
}

// javaVectorSize is `size()`.
func javaVectorSize(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(len(held)), nil
}

// javaVectorAt is `elementAt(int)`, bounds checked the way the language is:
// past the end is a failure and not a null the caller would fault on later.
func javaVectorAt(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	index := int(int32(arguments[1]))
	if index < 0 || index >= len(held) {
		return 0, fmt.Errorf("element %d of a vector of %d", index, len(held))
	}
	return held[index], nil
}

// javaVectorCopyInto is `copyInto(Object[] anArray)`: every element, in order,
// into the start of the array. The language throws NullPointerException for a
// null array and an index exception for one too short to take them all, and
// both arrive as guest throws. Its ArrayStoreException, for an element the
// array's type cannot hold, needs the store check storeJavaArrayReference
// explains this platform cannot make. A primitive array is refused rather than
// written: the declared parameter keeps a verified caller from passing one,
// and an int[] or float[] would take the references as numbers.
func javaVectorCopyInto(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": copying a vector into a null array")
	}
	array, err := client.javaArrayElements(arguments[1])
	if err != nil {
		return 0, err
	}
	if !array.Reference || array.Width != 4 {
		return 0, fmt.Errorf("a vector copied into an array of %d-byte numbers", array.Width)
	}
	if uint64(len(held)) > uint64(array.Length) {
		return 0, client.throwJavaPlatform(thread, javaThrowArrayClass,
			fmt.Sprintf(": %d elements into an array of %d", len(held), array.Length))
	}
	data := make([]byte, 4*len(held))
	for index, element := range held {
		binary.LittleEndian.PutUint32(data[4*index:], element)
	}
	return 0, client.core.Memory().Write(array.Block+javaArrayLengthWords*4, data)
}

// javaVectorSetAt replaces an existing element without changing the size or
// shifting adjacent elements. A null reference is a valid replacement.
func javaVectorSetAt(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	index := int32(arguments[2])
	if index < 0 || uint64(index) >= uint64(len(held)) {
		return 0, client.throwJavaPlatform(thread, javaThrowArrayClass,
			fmt.Sprintf("element %d of a vector of %d", index, len(held)))
	}
	held[index] = arguments[1]
	return 0, nil
}

// javaVectorFirst is `firstElement()`. The language throws on an empty vector
// rather than answering null, so an empty one is reported here: a caller that
// gets a null back would fault on it later, somewhere that says nothing about
// where it came from.
func javaVectorFirst(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if len(held) == 0 {
		return 0, fmt.Errorf("the first element of an empty vector")
	}
	return held[0], nil
}

// javaVectorRemoveAt is `removeElementAt(int)`.
func javaVectorRemoveAt(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	index := int(int32(arguments[1]))
	if index < 0 || index >= len(held) {
		return 0, fmt.Errorf("element %d of a vector of %d", index, len(held))
	}
	client.javaRuntimeState().vectors[arguments[0]] = append(held[:index:index], held[index+1:]...)
	return 0, nil
}

// javaVectorClear is `removeAllElements()`.
func javaVectorClear(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, err := client.javaVectorOf(arguments[0]); err != nil {
		return 0, err
	}
	client.javaRuntimeState().vectors[arguments[0]] = []uint32{}
	return 0, nil
}

// javaVectorInsertAt is `insertElementAt(Object, int)`: one more reference,
// pushed in at an index rather than added on the end. The language allows the
// index to be the size, which appends; anything past it is out of bounds.
func javaVectorInsertAt(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	index := int(int32(arguments[2]))
	if index < 0 || index > len(held) {
		return 0, fmt.Errorf("insert at %d of a vector of %d", index, len(held))
	}
	if err := javaVectorRoom(held); err != nil {
		return 0, err
	}
	client.javaVectorGrow(arguments[0], len(held)+1)
	grown := make([]uint32, 0, len(held)+1)
	grown = append(grown, held[:index]...)
	grown = append(grown, arguments[1])
	grown = append(grown, held[index:]...)
	client.javaRuntimeState().vectors[arguments[0]] = grown
	return 0, nil
}

// javaVectorEmpty is `isEmpty()`, slot 16: whether the list holds nothing.
func javaVectorEmpty(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if len(held) == 0 {
		return 1, nil
	}
	return 0, nil
}

// javaStackClass is the list reached from one end; see javaPlatformSupers for
// why its own methods sit above Vector's.
const javaStackClass = "java/util/Stack"

// javaStackPush is `Stack.push(Object)`, slot 32: the element goes on the end,
// which is the top, and the call answers what it was handed.
func javaStackPush(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, err := javaVectorAdd(client, ctx, thread, arguments); err != nil {
		return 0, err
	}
	return arguments[1], nil
}

// javaStackPop is `Stack.pop()`, slot 33: the element `push` put on last comes
// off, which is the end of the list underneath. An empty stack is a failure the
// language names, and it is reported here rather than answered with null — a
// null would be dispatched on by the caller and fail somewhere else.
func javaStackPop(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if len(held) == 0 {
		return 0, fmt.Errorf("pop from an empty stack at %#x", arguments[0])
	}
	top := held[len(held)-1]
	client.javaRuntimeState().vectors[arguments[0]] = held[:len(held)-1]
	return top, nil
}

// javaStackPeek is `Stack.peek()`, slot 34: the top without taking it off. An
// empty stack is the EmptyStackException the class library throws.
func javaStackPeek(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if len(held) == 0 {
		return 0, client.throwJavaPlatform(thread, javaEmptyStackClass, ": peek at an empty stack")
	}
	return held[len(held)-1], nil
}

// javaStackSearch is `Stack.search(Object)`, slot 36: how far from the top the
// object is, counting the top as 1, by lastIndexOf's test; -1 when it is not
// there.
func javaStackSearch(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	index, err := javaVectorLastIndexOf(client, ctx, thread, arguments[:2])
	if err != nil {
		return 0, err
	}
	if int32(index) < 0 {
		return ^uint32(0), nil
	}
	return uint32(len(held) - int(int32(index))), nil
}

const (
	javaNoSuchElementClass = "java/util/NoSuchElementException"
	javaEmptyStackClass    = "java/util/EmptyStackException"
)
