package lgt

import (
	"context"
	"fmt"

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

// javaVectorConstructor is `Vector()` and `Vector(int)`. The capacity is a hint
// about allocation and nothing a caller can observe, so both build the same
// empty list.
func javaVectorConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaRuntimeState().vectors[arguments[0]] = []uint32{}
	return 0, nil
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

// javaVectorAdd is `addElement(Object)`: one more reference on the end.
func javaVectorAdd(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().vectors[arguments[0]] = append(held, arguments[1])
	return 0, nil
}

// javaVectorIndexOf is `indexOf(Object)`: where the first reference equal to
// the argument sits, or -1. Equality here is the reference itself, because a
// vector holds handles and this platform has no `equals` to call on one — the
// local caller passes back a String it put in the vector itself, so the two are
// the same handle.
func javaVectorIndexOf(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaVectorOf(arguments[0])
	if err != nil {
		return 0, err
	}
	for index, element := range held {
		if element == arguments[1] {
			return uint32(index), nil
		}
	}
	return ^uint32(0), nil
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
