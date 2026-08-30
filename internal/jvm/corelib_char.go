package jvm

import (
	"strconv"
	"unicode/utf16"
)

// java/lang/Character, the boxed char and the character tests beside it.
//
// **The class was missing entirely, which is worse than a missing method.** A
// member nothing answers stops the call; a class nothing declares stops the
// resolution, so a title that puts a char in a Vector — or asks whether a key
// it was handed is a digit — dies before it reaches anything. Nothing in the
// local set names it, which is why it went unnoticed and why it is worth
// having: this is the failure that spreads quietly to titles nobody has run.
//
// Everything here is what the specification names exactly. **The character
// tests answer over ISO Latin-1**, which is what CLDC says a handset provides
// by default: a runtime is allowed to know more of Unicode and this one does
// not pretend to, because answering `isUpperCase` for a Hangul syllable one
// way here and another way on the handset is a divergence invented for no
// caller.

const (
	// characterMinRadix and characterMaxRadix bound the conversions, and they
	// are the same two numbers Integer.parseInt is bounded by.
	characterMinRadix = 2
	characterMaxRadix = 36
)

func characterDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	staticNative := AccessPublic | AccessStatic | AccessNative
	return ClassDefinition{
		Name:      CharacterClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Fields: []FieldDefinition{
			{Name: "MIN_VALUE", Descriptor: "C", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(0)},
			{Name: "MAX_VALUE", Descriptor: "C", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(0xffff)},
			{Name: "MIN_RADIX", Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(characterMinRadix)},
			{Name: "MAX_RADIX", Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(characterMaxRadix)},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(C)V", Access: native},
			{Name: "charValue", Descriptor: "()C", Access: native},
			{Name: "equals", Descriptor: "(Ljava/lang/Object;)Z", Access: native},
			{Name: "hashCode", Descriptor: "()I", Access: native},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "isDigit", Descriptor: "(C)Z", Access: staticNative},
			{Name: "isLowerCase", Descriptor: "(C)Z", Access: staticNative},
			{Name: "isUpperCase", Descriptor: "(C)Z", Access: staticNative},
			{Name: "toLowerCase", Descriptor: "(C)C", Access: staticNative},
			{Name: "toUpperCase", Descriptor: "(C)C", Access: staticNative},
			{Name: "digit", Descriptor: "(CI)I", Access: staticNative},
		},
	}
}

// latin1Lower and latin1Upper are the case pairs of ISO Latin-1. The ASCII
// half is the arithmetic everyone knows; the supplement adds the accented
// letters, whose pairs are also thirty-two apart — except for the two the
// standard leaves alone, which is why they are named rather than computed.
func latin1Lower(character rune) rune {
	switch {
	case character >= 'A' && character <= 'Z':
		return character + 32
	// 0xd7 is the multiplication sign, which sits inside the accented run and
	// is not a letter at all.
	case character >= 0xc0 && character <= 0xde && character != 0xd7:
		return character + 32
	}
	return character
}

func latin1Upper(character rune) rune {
	switch {
	case character >= 'a' && character <= 'z':
		return character - 32
	// 0xf7 is the division sign, and 0xff — small y with diaeresis — has its
	// capital outside Latin-1, so neither converts here.
	case character >= 0xe0 && character <= 0xfe && character != 0xf7:
		return character - 32
	}
	return character
}

func latin1IsLower(character rune) bool {
	return character != latin1Upper(character)
}

func latin1IsUpper(character rune) bool {
	return character != latin1Lower(character)
}

func (vm *VM) registerCharacterBuiltins() {
	vm.builtin(CharacterClass, "<init>", "(C)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		value, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		// A char is the same int32 every other box keeps, so everything that
		// reads a box reads this one too.
		object.Native = value
		return VoidValue(), nil
	})
	vm.builtin(CharacterClass, "charValue", "()C", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(value), nil
	})
	// Two boxed characters are equal when their values are, which is what a
	// title looking one up in a Hashtable is asking.
	vm.builtin(CharacterClass, "equals", "(Ljava/lang/Object;)Z", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		other, err := nativeReference(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if other == nil || other.ClassName != CharacterClass {
			return booleanValue(false), nil
		}
		stored, ok := other.Native.(int32)
		return booleanValue(ok && stored == value), nil
	})
	vm.builtin(CharacterClass, "hashCode", "()I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(value), nil
	})
	vm.builtin(CharacterClass, "toString", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(nativeStringValue(string(utf16.Decode([]uint16{uint16(value)})))), nil
	})

	vm.builtin(CharacterClass, "isDigit", "(C)Z", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return booleanValue(value >= '0' && value <= '9'), nil
	})
	for _, test := range []struct {
		name   string
		answer func(rune) bool
	}{{"isLowerCase", latin1IsLower}, {"isUpperCase", latin1IsUpper}} {
		answer := test.answer
		vm.builtin(CharacterClass, test.name, "(C)Z", func(_ *VM, arguments []Value) (Value, error) {
			value, err := nativeInt(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			return booleanValue(answer(rune(uint16(value)))), nil
		})
	}
	for _, conversion := range []struct {
		name    string
		convert func(rune) rune
	}{{"toLowerCase", latin1Lower}, {"toUpperCase", latin1Upper}} {
		convert := conversion.convert
		vm.builtin(CharacterClass, conversion.name, "(C)C", func(_ *VM, arguments []Value) (Value, error) {
			value, err := nativeInt(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			return IntValue(int32(convert(rune(uint16(value))))), nil
		})
	}
	// digit answers the value of a character in a radix, and −1 for anything
	// the radix does not carry — which includes a radix outside the two
	// bounds this class publishes.
	vm.builtin(CharacterClass, "digit", "(CI)I", func(_ *VM, arguments []Value) (Value, error) {
		character, err := nativeInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		radix, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if radix < characterMinRadix || radix > characterMaxRadix {
			return IntValue(-1), nil
		}
		digit := int32(-1)
		switch lower := latin1Lower(rune(uint16(character))); {
		case lower >= '0' && lower <= '9':
			digit = int32(lower - '0')
		case lower >= 'a' && lower <= 'z':
			digit = int32(lower-'a') + 10
		}
		if digit < 0 || digit >= radix {
			return IntValue(-1), nil
		}
		return IntValue(digit), nil
	})
}

// radixText formats a signed value the way Integer.toString(int, int) and
// Long.toString(long, int) do: a radix outside the bounds means ten, and the
// digits are the lower-case ones.
func radixText(value int64, radix int32) string {
	if radix < characterMinRadix || radix > characterMaxRadix {
		radix = 10
	}
	return strconv.FormatInt(value, int(radix))
}
