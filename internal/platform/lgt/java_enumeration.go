package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/util/Enumeration`, which `Vector.elements()`, `Hashtable.keys()` and
// `Hashtable.elements()` answer.
//
// An enumeration is an object of one of this platform's classes — the class
// library calls them VectorEnumerator and HashtableEnumerator — and a title
// reaches it only through the interface: `e.hasMoreElements()` is an interface
// call, which asks javaInterfaceTable where the receiver's class keeps the
// interface's methods. **A platform class has no record to carry an interface
// table**, so javaPlatformInterfaces is its table: Enumeration's two methods at
// the class's own first slots, 10 and 11, in the order the interface declares
// them.
//
// A VectorEnumerator walks the vector as it is, the way the class library's
// does: an element added before the walk reaches the end is seen. A
// HashtableEnumerator hands out the table's keys or values as they were when
// it was asked for, in the order the class library's enumerator visits them —
// from the last bucket to the first, each chain from its head. That differs
// from the library only for a title that changes the table while it walks it.

const (
	javaEnumerationInterface     = "java/util/Enumeration"
	javaVectorEnumeratorClass    = "java/util/VectorEnumerator"
	javaHashtableEnumeratorClass = "java/util/HashtableEnumerator"
)

// javaPlatformInterfaces is the interface table of the platform classes that
// implement an interface a title calls through.
var javaPlatformInterfaces = map[string][]javaInterface{
	javaVectorEnumeratorClass:    {{Name: javaEnumerationInterface, Slot: 10}},
	javaHashtableEnumeratorClass: {{Name: javaEnumerationInterface, Slot: 10}},
}

// javaInterfacesOf is a class's interface table: the one this platform
// gives a platform class, and otherwise its record's.
func javaInterfacesOf(class *javaRuntimeClass) []javaInterface {
	if class.Handle == 0 {
		if declared, ok := javaPlatformInterfaces[class.Name]; ok {
			return declared
		}
	}
	return class.Record.Interfaces
}

// javaEnumeration is what one enumeration walks: a vector, live, or the items
// a hashtable held, and how far it has got.
type javaEnumeration struct {
	vector uint32
	items  []uint32
	next   int
}

// newJavaEnumeration builds an enumeration object of one of the two classes.
func (client *Client) newJavaEnumeration(class string, enumeration *javaEnumeration) (uint32, error) {
	prepared, err := client.preparePlatformJavaClass(class)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(prepared)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().enumerations[object] = enumeration
	return object, nil
}

func (client *Client) javaEnumerationOf(object uint32) (*javaEnumeration, error) {
	enumeration, ok := client.javaRuntimeState().enumerations[object]
	if !ok || enumeration == nil {
		return nil, fmt.Errorf("the object at %#x is not an enumeration this platform built", object)
	}
	return enumeration, nil
}

// javaEnumerationItems is everything an enumeration walks; how far it has got
// is its own count.
func (client *Client) javaEnumerationItems(enumeration *javaEnumeration) []uint32 {
	if enumeration.vector != 0 {
		return client.javaRuntimeState().vectors[enumeration.vector]
	}
	return enumeration.items
}

// javaEnumerationHasMore is `hasMoreElements()`, slot 10.
func javaEnumerationHasMore(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	enumeration, err := client.javaEnumerationOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return javaBoolean(enumeration.next < len(client.javaEnumerationItems(enumeration))), nil
}

// javaEnumerationNext is `nextElement()`, slot 11. Past the end is the
// NoSuchElementException the class library throws, naming the enumerator.
func javaEnumerationNext(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	enumeration, err := client.javaEnumerationOf(arguments[0])
	if err != nil {
		return 0, err
	}
	items := client.javaEnumerationItems(enumeration)
	if enumeration.next >= len(items) {
		name := "HashtableEnumerator"
		if enumeration.vector != 0 {
			name = "VectorEnumerator"
		}
		return 0, client.throwJavaPlatform(thread, javaNoSuchElementClass, ": "+name)
	}
	element := items[enumeration.next]
	enumeration.next++
	return element, nil
}

// javaVectorElements is `Vector.elements()`, slot 17.
func javaVectorElements(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, err := client.javaVectorOf(arguments[0]); err != nil {
		return 0, err
	}
	return client.newJavaEnumeration(javaVectorEnumeratorClass, &javaEnumeration{vector: arguments[0]})
}

// javaEnumerationSlots is the two methods both classes answer.
var javaEnumerationSlots = map[uint32]javaBakedSlot{
	10: {Called: "hasMoreElements()Z", Method: javaPlatformMethod{Words: 1, Implementat: javaEnumerationHasMore}},
	11: {Called: "nextElement()Ljava/lang/Object;", Method: javaPlatformMethod{Words: 1, Implementat: javaEnumerationNext}},
}
