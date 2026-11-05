package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/lang/Boolean`: a flag in an object, which is how a title keeps one in a
// Vector or keys a Hashtable with one.
//
// The flag lives on this side in the table the numbers keep their values in
// (java_number.go), as 1 or 0, so a checkpoint carries it in the class
// library's record and the collector lets it go with its object, as it does an
// Integer's value. The class declares one method of its own, `booleanValue`,
// at slot 10, and `toString`, `hashCode` and `equals` over Object's 4, 2 and
// 3. **No call site anchors any of them**: the class page's order and the
// declaration-order rule are all that place slot 10 (javaBakedVirtualSlots).
// What each one answers is the page's, and the same the KTF and SKT runtimes
// answer (internal/jvm): `hashCode` is 1231 for true and 1237 for false, and
// `equals` is another Boolean holding the same flag.
//
// TRUE and FALSE are the two instances the class publishes. A title reads them
// out of the class object's storage like any other static field it asked the
// load call to number, so they are built when the class is laid out and
// written where the module reads them (initializeJavaBooleanStatics). A title
// that compares a flag against Boolean.TRUE with `==` gets the same object
// every time it reads the field, and the class object's storage is memory the
// collector reads as a root, which keeps both for the life of the title.

const javaBooleanClass = "java/lang/Boolean"

// javaBooleanHash is what hashCode answers for each flag, as the class page
// fixes it.
const (
	javaBooleanTrueHash  = 1231
	javaBooleanFalseHash = 1237
)

// javaBooleanConstructor is `Boolean(boolean)`.
func javaBooleanConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaRuntimeState().numbers[arguments[0]] = int64(javaBoolean(arguments[1] != 0))
	return 0, nil
}

// javaBooleanOf answers the flag a Boolean holds.
func (client *Client) javaBooleanOf(object uint32) (bool, error) {
	value, ok := client.javaRuntimeState().numbers[object]
	if !ok {
		return false, fmt.Errorf("the object at %#x is not a Boolean this platform built", object)
	}
	return value != 0, nil
}

// javaBooleanValue is `booleanValue()`, slot 10.
func javaBooleanValue(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaBooleanOf(arguments[0])
	return javaBoolean(value), err
}

// javaBooleanHashCode is `hashCode()`, slot 2.
func javaBooleanHashCode(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaBooleanOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if value {
		return javaBooleanTrueHash, nil
	}
	return javaBooleanFalseHash, nil
}

// javaBooleanEquals is `equals(Object)`, slot 3: another Boolean holding the
// same flag. Null and an object of any other class are not equal, whatever
// number this platform keeps for it.
func javaBooleanEquals(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaBooleanOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if !client.javaObjectIs(arguments[1], javaBooleanClass) {
		return javaFalse, nil
	}
	other, err := client.javaBooleanOf(arguments[1])
	if err != nil {
		return javaFalse, nil
	}
	return javaBoolean(other == value), nil
}

// javaBooleanToString is `toString()`, slot 4: "true" or "false".
func javaBooleanToString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaBooleanOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if value {
		return client.newJavaString("true")
	}
	return client.newJavaString("false")
}

// initializeJavaBooleanStatics builds TRUE and FALSE and writes each into the
// word the module was told it sits in. A field the module did not ask for is
// one nothing reads, and no object is built for it.
func (client *Client) initializeJavaBooleanStatics(class *javaRuntimeClass) error {
	for _, field := range []struct {
		name  string
		value bool
	}{{"TRUE", true}, {"FALSE", false}} {
		address, declared := client.javaPlatformStaticWord(class, field.name, "L"+javaBooleanClass+";")
		if !declared {
			continue
		}
		object, err := client.allocateJavaObject(class)
		if err != nil {
			return err
		}
		client.javaRuntimeState().numbers[object] = int64(javaBoolean(field.value))
		if err := client.writeWord(address, object); err != nil {
			return err
		}
	}
	return nil
}
