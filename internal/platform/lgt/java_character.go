package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/lang/Character`: a char in an object, and the character tests beside
// it.
//
// The box is Boolean's arrangement (java_boolean.go): the code unit lives in
// the numbers' table, `charValue` takes slot 10 by the declaration-order rule
// alone — no call site anchors it — and `hashCode`, `equals` and `toString`
// take Object's 2, 3 and 4.
//
// **The tests answer over ISO Latin-1**, which is what CLDC says a handset
// provides by default, with the case pairs String's own case methods use
// (javaUpperUnit and javaLowerUnit), and they answer what the KTF and SKT
// runtimes answer (internal/jvm): a unit is lower case when it has an upper
// case other than itself, upper case the other way round, and a digit only from
// '0' to '9'. `digit` reads '0' to '9' and the Latin letters in either case,
// and answers -1 for anything the radix does not carry, including a radix
// outside the two bounds the class publishes.
//
// The four constants are `static final`, which a compiler usually puts in the
// caller's own code; a module that loads them instead is answered from the
// class's storage like PlayListener's (initializeJavaCharacterStatics).

const javaCharacterClass = "java/lang/Character"

// The radix bounds and the char range the class publishes, the same numbers
// Integer.toString's radix is bounded by.
const (
	javaCharacterMinRadix = 2
	javaCharacterMaxRadix = 36
	javaCharacterMinValue = 0
	javaCharacterMaxValue = 0xffff
)

// javaCharacterConstructor is `Character(char)`. A char is sixteen bits, so the
// word is cut to its code unit.
func javaCharacterConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaRuntimeState().numbers[arguments[0]] = int64(uint16(arguments[1]))
	return 0, nil
}

// javaCharacterOf answers the code unit a Character holds.
func (client *Client) javaCharacterOf(object uint32) (uint16, error) {
	value, ok := client.javaRuntimeState().numbers[object]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a Character this platform built", object)
	}
	return uint16(value), nil
}

// javaCharacterValue is `charValue()`, slot 10.
func javaCharacterValue(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaCharacterOf(arguments[0])
	return uint32(value), err
}

// javaCharacterHashCode is `hashCode()`, slot 2: the code unit itself.
func javaCharacterHashCode(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaCharacterOf(arguments[0])
	return uint32(value), err
}

// javaCharacterEquals is `equals(Object)`, slot 3: another Character holding
// the same code unit.
func javaCharacterEquals(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaCharacterOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if !client.javaObjectIs(arguments[1], javaCharacterClass) {
		return javaFalse, nil
	}
	other, err := client.javaCharacterOf(arguments[1])
	if err != nil {
		return javaFalse, nil
	}
	return javaBoolean(other == value), nil
}

// javaCharacterToString is `toString()`, slot 4: a String of the one unit, as
// `String.valueOf(char)` builds it.
func javaCharacterToString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := client.javaCharacterOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return client.newJavaString(javaTextOfUnits([]uint16{value}))
}

// javaCharacterIsLowerCase, javaCharacterIsUpperCase and javaCharacterIsDigit
// are the static tests, each over the unit its one argument carries.
func javaCharacterIsLowerCase(
	_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	unit := uint16(arguments[0])
	return javaBoolean(javaUpperUnit(unit) != unit), nil
}

func javaCharacterIsUpperCase(
	_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	unit := uint16(arguments[0])
	return javaBoolean(javaLowerUnit(unit) != unit), nil
}

func javaCharacterIsDigit(
	_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	unit := uint16(arguments[0])
	return javaBoolean(unit >= '0' && unit <= '9'), nil
}

// javaCharacterToLowerCase and javaCharacterToUpperCase are the static case
// maps: the unit's pair, or the unit itself.
func javaCharacterToLowerCase(
	_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return uint32(javaLowerUnit(uint16(arguments[0]))), nil
}

func javaCharacterToUpperCase(
	_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return uint32(javaUpperUnit(uint16(arguments[0]))), nil
}

// javaCharacterDigit is `digit(char, int)`: the unit's value in a radix, or -1.
func javaCharacterDigit(
	_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	radix := int32(arguments[1])
	if radix < javaCharacterMinRadix || radix > javaCharacterMaxRadix {
		return ^uint32(0), nil
	}
	digit := int32(-1)
	switch lower := javaLowerUnit(uint16(arguments[0])); {
	case lower >= '0' && lower <= '9':
		digit = int32(lower - '0')
	case lower >= 'a' && lower <= 'z':
		digit = int32(lower-'a') + 10
	}
	if digit < 0 || digit >= radix {
		return ^uint32(0), nil
	}
	return uint32(digit), nil
}

// initializeJavaCharacterStatics writes the four constants into the words the
// module was told they sit in, for the ones it asked for.
func (client *Client) initializeJavaCharacterStatics(class *javaRuntimeClass) error {
	for _, field := range []struct {
		name, descriptor string
		value            uint32
	}{
		{"MIN_RADIX", "I", javaCharacterMinRadix}, {"MAX_RADIX", "I", javaCharacterMaxRadix},
		{"MIN_VALUE", "C", javaCharacterMinValue}, {"MAX_VALUE", "C", javaCharacterMaxValue},
	} {
		address, declared := client.javaPlatformStaticWord(class, field.name, field.descriptor)
		if !declared {
			continue
		}
		if err := client.writeWord(address, field.value); err != nil {
			return err
		}
	}
	return nil
}
