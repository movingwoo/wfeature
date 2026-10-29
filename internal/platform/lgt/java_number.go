package lgt

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/lang/Integer`, `Long`, `Short` and `Byte`: a number in an object, which
// is how a title puts one in a Vector or keys a Hashtable with one.
//
// The number lives on this side, keyed by the object, the way a String's text
// does (java_string.go). Each class's own methods take its slots from 10 in
// declaration order — Integer's byteValue, shortValue, intValue and longValue
// at 10 to 13, the others' one value method at 10 — and equals, hashCode and
// toString take Object's 3, 2 and 4. floatValue and doubleValue are left out
// with the rest of floating point.

const (
	javaIntegerClass = "java/lang/Integer"
	javaLongClass    = "java/lang/Long"
	javaShortClass   = "java/lang/Short"
	javaByteClass    = "java/lang/Byte"
)

// javaNumberOf answers the number behind an Integer, Long, Short or Byte.
func (client *Client) javaNumberOf(object uint32) (int64, error) {
	value, ok := client.javaRuntimeState().numbers[object]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a number this platform built", object)
	}
	return value, nil
}

// newJavaNumber builds an object of one of the four classes holding a value,
// which is what `Integer.valueOf` answers.
func (client *Client) newJavaNumber(class string, value int64) (uint32, error) {
	prepared, err := client.preparePlatformJavaClass(class)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(prepared)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().numbers[object] = value
	return object, nil
}

// javaNumberConstructor is `Integer(int)`, `Short(short)` and `Byte(byte)`:
// the receiver the module allocated, holding the word it was given as a
// number of that width.
func javaNumberConstructor(bits int) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		client.javaRuntimeState().numbers[arguments[0]] = javaNarrow(int64(int32(arguments[1])), bits)
		return 0, nil
	}
}

// javaLongConstructor is `Long(long)`, whose value arrives in two words, low
// first.
func javaLongConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaRuntimeState().numbers[arguments[0]] = int64(uint64(arguments[1]) | uint64(arguments[2])<<32)
	return 0, nil
}

// javaNarrow is a Java narrowing conversion: the low bits, sign-extended.
func javaNarrow(value int64, bits int) int64 {
	switch bits {
	case 8:
		return int64(int8(value))
	case 16:
		return int64(int16(value))
	case 32:
		return int64(int32(value))
	}
	return value
}

// javaNumberValue is every `xxxValue()` of an integral width: the number,
// narrowed the way the language narrows it. A long answer goes back in the
// register pair every other long here uses.
func javaNumberValue(bits int) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32) (uint32, error) {
		value, err := client.javaNumberOf(arguments[0])
		if err != nil {
			return 0, err
		}
		value = javaNarrow(value, bits)
		if bits == 64 {
			if err := thread.SetRegister(1, uint32(uint64(value)>>32)); err != nil {
				return 0, err
			}
		}
		return uint32(value), nil
	}
}

// javaNumberHashCode is hashCode, slot 2: the value for the three narrow
// classes, and `(int)(value ^ (value >>> 32))` for a Long, as the class library
// defines each.
func javaNumberHashCode(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaNumberOf(arguments[0])
	if err != nil {
		return 0, err
	}
	class, _ := client.javaClassOfObject(arguments[0])
	if class != nil && class.Name == javaLongClass {
		return uint32(uint64(value) ^ uint64(value)>>32), nil
	}
	return uint32(int32(value)), nil
}

// javaNumberEquals is equals, slot 3: an object of the same class holding the
// same value. An Integer is not equal to a Long of the same number, as the
// class library has it.
func javaNumberEquals(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaNumberOf(arguments[0])
	if err != nil {
		return 0, err
	}
	mine, _ := client.javaClassOfObject(arguments[0])
	theirs, known := client.javaClassOfObject(arguments[1])
	if mine == nil || !known || theirs.Name != mine.Name {
		return javaFalse, nil
	}
	other, err := client.javaNumberOf(arguments[1])
	if err != nil {
		return javaFalse, nil
	}
	return javaBoolean(other == value), nil
}

// javaNumberToString is toString, slot 4: the number in decimal.
func javaNumberToString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaNumberOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return client.newJavaString(strconv.FormatInt(value, 10))
}

// javaRadixOf is the radix a `toString(value, radix)` uses: a radix outside 2
// to 36 is 10, as the class library defines it, rather than a failure.
func javaRadixOf(word uint32) int {
	radix := int(int32(word))
	if radix < 2 || radix > 36 {
		return 10
	}
	return radix
}

// javaIntegerToStringRadix is `Integer.toString(int, int)`, and
// javaLongToStringRadix `Long.toString(long, int)`: the signed number in a
// radix, in lower-case digits.
func javaIntegerToStringRadix(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.newJavaString(strconv.FormatInt(int64(int32(arguments[0])), javaRadixOf(arguments[1])))
}

func javaLongToStringRadix(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value := int64(uint64(arguments[0]) | uint64(arguments[1])<<32)
	return client.newJavaString(strconv.FormatInt(value, javaRadixOf(arguments[2])))
}

// javaIntegerUnsigned is `toHexString`, `toOctalString` and `toBinaryString`:
// the 32 bits as an unsigned number in that radix, which is how the class
// library writes a negative one.
func javaIntegerUnsigned(radix int) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		return client.newJavaString(strconv.FormatUint(uint64(arguments[0]), radix))
	}
}

// javaIntegerValueOf is `Integer.valueOf(String)` and `valueOf(String, int)`:
// parseInt's number, in an Integer.
func javaIntegerValueOf(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	radix := 10
	if len(arguments) > 1 {
		radix = int(int32(arguments[1]))
	}
	value, err := client.javaParseNumber(thread, arguments[0], radix, 32)
	if err != nil {
		return 0, err
	}
	return client.newJavaNumber(javaIntegerClass, int64(int32(value)))
}

// javaParseRadix is the radix forms of parseShort and parseByte.
func javaParseRadix(bits int) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32) (uint32, error) {
		return client.javaParseNumber(thread, arguments[0], int(int32(arguments[1])), bits)
	}
}

// javaParseLong is `Long.parseLong(String)` and `parseLong(String, int)`. It is
// javaParseNumber's parse at 64 bits, answered in the register pair a long
// goes back in.
func javaParseLong(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	radix := 10
	if len(arguments) > 1 {
		radix = int(int32(arguments[1]))
	}
	text, ok := client.javaText(arguments[0])
	if !ok || arguments[0] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": parsing a null string")
	}
	if radix < 2 || radix > 36 {
		return 0, client.throwJavaPlatform(thread, javaNumberFormatClass, ": radix "+strconv.Itoa(radix))
	}
	value, err := strconv.ParseInt(strings.TrimPrefix(text, "+"), radix, 64)
	if err != nil {
		return 0, client.throwJavaPlatform(thread, javaNumberFormatClass, ": "+text)
	}
	if err := thread.SetRegister(1, uint32(uint64(value)>>32)); err != nil {
		return 0, err
	}
	return uint32(value), nil
}
