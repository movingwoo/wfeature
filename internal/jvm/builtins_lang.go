package jvm

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

// The bodies behind the java/lang declarations in corelib_lang.go, plus the
// methods on classes that were already declared and that a local title asked
// for and did not get. Each one is here because an archive links against it:
// nothing on this page is speculative surface.

func (vm *VM) registerLanguageBuiltins() {
	vm.registerBoxBuiltins()
	vm.registerCharacterBuiltins()
	vm.registerMathFloatBuiltins()
	vm.registerRuntimeBuiltins()
	vm.registerStringExtraBuiltins()

	// currentThread has to be answered from the execution rather than from the
	// VM: which thread is running is what the caller is asking, and only the
	// execution knows. The main execution has no thread object of its own, so
	// it gets one the first time it asks and keeps it — a title that compares
	// the answer against a thread it stored has to see the same object.
	vm.contextBuiltin(ThreadClass, "currentThread", "()Ljava/lang/Thread;", func(vm *VM, state *execution, _ []Value) (Value, error) {
		if state != nil && state.thread != nil {
			return ReferenceValue(state.thread), nil
		}
		return ReferenceValue(vm.mainThreadObject()), nil
	})
	vm.builtin(SystemClass, "exit", "(I)V", func(vm *VM, arguments []Value) (Value, error) {
		status, err := nativeInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return VoidValue(), vm.guestExit("System.exit", status)
	})
	// Runtime.exit is what System.exit calls in the class library, so it ends
	// a session exactly as System.exit does on each platform: through the
	// teardown the platform installed, or with the same refusal where none is.
	vm.builtin(RuntimeClass, "exit", "(I)V", func(vm *VM, arguments []Value) (Value, error) {
		if _, err := requireObject(arguments, 0); err != nil {
			return VoidValue(), err
		}
		status, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		return VoidValue(), vm.guestExit("Runtime.exit", status)
	})
}

// guestExit is the title ending itself with a status, through Options.Exit.
func (vm *VM) guestExit(caller string, status int32) error {
	if vm.config.Logger != nil {
		vm.config.Logger.Debug("guest requested exit", "call", caller, "status", status)
	}
	if vm.config.Exit == nil {
		return fmt.Errorf("%s(%d) with no platform shutdown installed", caller, status)
	}
	return vm.config.Exit(status)
}

// mainThreadObject is the Thread the execution that is not a guest thread
// answers with.
func (vm *VM) mainThreadObject() *Object {
	vm.threadMu.Lock()
	defer vm.threadMu.Unlock()
	if vm.mainThread == nil {
		vm.mainThread = &Object{ClassName: ThreadClass}
	}
	return vm.mainThread
}

// registerBoxBuiltins is the boxed-number half. Integer already had most of
// it; Long and Byte are here because two titles parse with them.
func (vm *VM) registerBoxBuiltins() {
	for _, radix := range []struct {
		name string
		base int
	}{{"toHexString", 16}, {"toOctalString", 8}, {"toBinaryString", 2}} {
		base := radix.base
		// The unsigned reading is the one the standard specifies: a negative
		// int formats as the eight hexadecimal digits of its two's complement,
		// which is what a title printing a colour or a flag word expects.
		vm.builtin(IntegerClass, radix.name, "(I)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
			value, err := nativeInt(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			return vm.stringResult(strconv.FormatUint(uint64(uint32(value)), base))
		})
	}
	// Two boxed ints are equal when their values are, which is what a title
	// searching a Vector of them is asking. Identity would answer no to every
	// number it did not itself box.
	vm.builtin(IntegerClass, "equals", "(Ljava/lang/Object;)Z", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		other, err := nativeReference(arguments, 1)
		if err != nil || other == nil {
			return IntValue(0), err
		}
		stored, ok := other.Native.(int32)
		if !ok || other.ClassName != IntegerClass || stored != value {
			return IntValue(0), nil
		}
		return IntValue(1), nil
	})
	vm.builtin(IntegerClass, "hashCode", "()I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(value), nil
	})

	// The boxed flag. Its value is the same int32 the other boxes keep, so
	// everything that reads a box reads this one too; what is its own is the
	// hash, which the specification fixes at two constants rather than at the
	// value.
	vm.builtin(BooleanClass, "<init>", "(Z)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		value, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = booleanBox(value)
		return VoidValue(), nil
	})
	vm.builtin(BooleanClass, "booleanValue", "()Z", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(value), nil
	})
	vm.builtin(BooleanClass, "equals", "(Ljava/lang/Object;)Z", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		other, err := nativeReference(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if other == nil || other.ClassName != BooleanClass {
			return booleanValue(false), nil
		}
		stored, ok := other.Native.(int32)
		return booleanValue(ok && stored == value), nil
	})
	vm.builtin(BooleanClass, "hashCode", "()I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		if value != 0 {
			return IntValue(1231), nil
		}
		return IntValue(1237), nil
	})
	vm.builtin(BooleanClass, "toString", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(booleanText(value))
	})

	vm.builtin(LongClass, "<init>", "(J)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		value, err := nativeLong(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = value
		return VoidValue(), nil
	})
	vm.builtin(LongClass, "longValue", "()J", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return LongValue(value), nil
	})
	vm.builtin(LongClass, "intValue", "()I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(value)), nil
	})
	vm.builtin(LongClass, "toString", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(strconv.FormatInt(value, 10))
	})
	vm.builtin(LongClass, "toString", "(J)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(strconv.FormatInt(value, 10))
	})
	vm.builtin(LongClass, "parseLong", "(Ljava/lang/String;)J", func(_ *VM, arguments []Value) (Value, error) {
		text, err := parsedText(arguments)
		if err != nil {
			return VoidValue(), err
		}
		value, err := parseJavaInteger(text, 10, 64)
		if err != nil {
			return VoidValue(), err
		}
		return LongValue(value), nil
	})

	vm.builtin(ByteClass, "<init>", "(B)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		value, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = int32(int8(value))
		return VoidValue(), nil
	})
	vm.builtin(ByteClass, "byteValue", "()B", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(int8(value))), nil
	})
	vm.builtin(ByteClass, "intValue", "()I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(value), nil
	})
	vm.builtin(ByteClass, "toString", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(strconv.FormatInt(int64(value), 10))
	})
	// The static form is the one a title reaches for when it has a byte and no
	// box: three local titles render a table of numbers with it, where the
	// instance form would cost them an allocation per cell.
	vm.builtin(ByteClass, "toString", "(B)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(strconv.FormatInt(int64(int8(value)), 10))
	})
	vm.builtin(ByteClass, "parseByte", "(Ljava/lang/String;)B", func(_ *VM, arguments []Value) (Value, error) {
		text, err := parsedText(arguments)
		if err != nil {
			return VoidValue(), err
		}
		value, err := parseJavaInteger(text, 10, 8)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(value)), nil
	})

	vm.builtin(ShortClass, "<init>", "(S)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		value, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = int32(int16(value))
		return VoidValue(), nil
	})
	vm.builtin(ShortClass, "shortValue", "()S", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(int16(value))), nil
	})
	vm.builtin(ShortClass, "intValue", "()I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(value), nil
	})
	vm.builtin(ShortClass, "toString", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(strconv.FormatInt(int64(value), 10))
	})
	vm.builtin(ShortClass, "parseShort", "(Ljava/lang/String;)S", func(_ *VM, arguments []Value) (Value, error) {
		text, err := parsedText(arguments)
		if err != nil {
			return VoidValue(), err
		}
		value, err := parseJavaInteger(text, 10, 16)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(value)), nil
	})

	// The rest of CLDC 1.1's boxed numbers: the radix forms of the parses and
	// the formats, the two equalities that are about values rather than
	// identity, and the widening accessors. Each is a value the specification
	// names exactly. None of them is reached by a local title — the class-wide
	// scan says the whole corpus uses `parseInt`, `toString(int)` and four
	// others — which is the reason to have them rather than not: a member
	// nothing answers stops the call that wanted it, and the calls that would
	// want these are on titles nobody has run.
	for _, parse := range []struct {
		class      string
		name       string
		descriptor string
		bits       int
	}{
		{ByteClass, "parseByte", "(Ljava/lang/String;I)B", 8},
		{ShortClass, "parseShort", "(Ljava/lang/String;I)S", 16},
	} {
		bits := parse.bits
		vm.builtin(parse.class, parse.name, parse.descriptor, func(_ *VM, arguments []Value) (Value, error) {
			value, err := parseRadix(arguments, bits)
			if err != nil {
				return VoidValue(), err
			}
			return IntValue(int32(value)), nil
		})
	}
	vm.builtin(LongClass, "parseLong", "(Ljava/lang/String;I)J", func(_ *VM, arguments []Value) (Value, error) {
		value, err := parseRadix(arguments, 64)
		if err != nil {
			return VoidValue(), err
		}
		return LongValue(value), nil
	})
	vm.builtin(IntegerClass, "valueOf", "(Ljava/lang/String;I)Ljava/lang/Integer;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := parseRadix(arguments, 32)
		if err != nil {
			return VoidValue(), err
		}
		return vm.libraryObject(IntegerClass, int32(value))
	})
	vm.builtin(IntegerClass, "toString", "(II)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		radix, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(radixText(int64(value), radix))
	})
	vm.builtin(LongClass, "toString", "(JI)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		radix, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(radixText(value, radix))
	})

	// Equality is about the value on every box, so a title looking one up in a
	// Vector finds the number it stored rather than only the object it stored.
	for _, class := range []string{ByteClass, ShortClass} {
		boxed := class
		vm.builtin(boxed, "equals", "(Ljava/lang/Object;)Z", func(_ *VM, arguments []Value) (Value, error) {
			value, err := boxedInt(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			other, err := nativeReference(arguments, 1)
			if err != nil {
				return VoidValue(), err
			}
			if other == nil || other.ClassName != boxed {
				return booleanValue(false), nil
			}
			stored, ok := other.Native.(int32)
			return booleanValue(ok && stored == value), nil
		})
		vm.builtin(boxed, "hashCode", "()I", func(_ *VM, arguments []Value) (Value, error) {
			value, err := boxedInt(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			return IntValue(value), nil
		})
	}
	vm.builtin(LongClass, "equals", "(Ljava/lang/Object;)Z", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		other, err := nativeReference(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if other == nil || other.ClassName != LongClass {
			return booleanValue(false), nil
		}
		stored, ok := other.Native.(int64)
		return booleanValue(ok && stored == value), nil
	})
	// A long's hash is its two halves folded together, which is the value the
	// standard names rather than a choice this runtime gets to make.
	vm.builtin(LongClass, "hashCode", "()I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(value ^ (value >> 32))), nil
	})

	// The widening accessors. A boxed integer read as a float is the number,
	// not its text, so the reason floats are otherwise absent from this
	// library — Java's shortest-representation printing is not Go's — does not
	// reach them.
	vm.builtin(IntegerClass, "floatValue", "()F", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return FloatValue(float32(value)), nil
	})
	vm.builtin(IntegerClass, "doubleValue", "()D", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return DoubleValue(float64(value)), nil
	})
	vm.builtin(LongClass, "floatValue", "()F", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return FloatValue(float32(value)), nil
	})
	vm.builtin(LongClass, "doubleValue", "()D", func(_ *VM, arguments []Value) (Value, error) {
		value, err := boxedLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return DoubleValue(float64(value)), nil
	})

	vm.builtin(MathClass, "min", "(JJ)J", func(_ *VM, arguments []Value) (Value, error) {
		left, right, err := longPair(arguments)
		if err != nil {
			return VoidValue(), err
		}
		return LongValue(min(left, right)), nil
	})
	vm.builtin(MathClass, "max", "(JJ)J", func(_ *VM, arguments []Value) (Value, error) {
		left, right, err := longPair(arguments)
		if err != nil {
			return VoidValue(), err
		}
		return LongValue(max(left, right)), nil
	})

}

// registerMathFloatBuiltins is the floating-point half of CLDC 1.1's Math.
//
// Each of these is exactly specified — a value the standard names rather than
// a behaviour to invent, which is this library's rule for what it declares —
// and Go's own library answers the same IEEE cases the specification lists,
// including the NaN and signed-zero ones that make max and min more than a
// comparison. The two conversions are written as the specification's own
// expressions rather than folded into a constant, because the rounding of
// `x * 180 / PI` is not the rounding of `x * (180 / PI)`.
func (vm *VM) registerMathFloatBuiltins() {
	vm.builtin(MathClass, "min", "(FF)F", floatPairBuiltin(math.Min))
	vm.builtin(MathClass, "max", "(FF)F", floatPairBuiltin(math.Max))
	vm.builtin(MathClass, "min", "(DD)D", doublePairBuiltin(math.Min))
	vm.builtin(MathClass, "max", "(DD)D", doublePairBuiltin(math.Max))
	vm.builtin(MathClass, "ceil", "(D)D", doubleBuiltin(math.Ceil))
	vm.builtin(MathClass, "floor", "(D)D", doubleBuiltin(math.Floor))
	vm.builtin(MathClass, "sqrt", "(D)D", doubleBuiltin(math.Sqrt))
	vm.builtin(MathClass, "sin", "(D)D", doubleBuiltin(math.Sin))
	vm.builtin(MathClass, "cos", "(D)D", doubleBuiltin(math.Cos))
	vm.builtin(MathClass, "tan", "(D)D", doubleBuiltin(math.Tan))
	vm.builtin(MathClass, "toDegrees", "(D)D", doubleBuiltin(func(value float64) float64 {
		return value * 180 / math.Pi
	}))
	vm.builtin(MathClass, "toRadians", "(D)D", doubleBuiltin(func(value float64) float64 {
		return value / 180 * math.Pi
	}))
}

// doubleBuiltin, doublePairBuiltin and floatPairBuiltin wrap the arithmetic
// above so each entry is the operation and nothing else. The float pair goes
// through float64 because widening a float is exact and the answer is one of
// the two arguments, so narrowing it back cannot lose anything.
func doubleBuiltin(operation func(float64) float64) func(*VM, []Value) (Value, error) {
	return func(_ *VM, arguments []Value) (Value, error) {
		value, err := arguments[0].Float64()
		if err != nil {
			return VoidValue(), err
		}
		return DoubleValue(operation(value)), nil
	}
}

func doublePairBuiltin(operation func(float64, float64) float64) func(*VM, []Value) (Value, error) {
	return func(_ *VM, arguments []Value) (Value, error) {
		left, err := arguments[0].Float64()
		if err != nil {
			return VoidValue(), err
		}
		right, err := arguments[1].Float64()
		if err != nil {
			return VoidValue(), err
		}
		return DoubleValue(operation(left, right)), nil
	}
}

func floatPairBuiltin(operation func(float64, float64) float64) func(*VM, []Value) (Value, error) {
	return func(_ *VM, arguments []Value) (Value, error) {
		left, err := arguments[0].Float32()
		if err != nil {
			return VoidValue(), err
		}
		right, err := arguments[1].Float32()
		if err != nil {
			return VoidValue(), err
		}
		return FloatValue(float32(operation(float64(left), float64(right)))), nil
	}
}

// parseRadix is the radix half of the boxed parses: the text, the base beside
// it, and the width the answer has to fit. A base outside what Character
// publishes is the caller's mistake and is refused the way an unparsable
// string is, because that is the exception the specification names.
func parseRadix(arguments []Value, bits int) (int64, error) {
	text, err := parsedText(arguments)
	if err != nil {
		return 0, err
	}
	radix, err := nativeInt(arguments, 1)
	if err != nil {
		return 0, err
	}
	if radix < characterMinRadix || radix > characterMaxRadix {
		return 0, guestException("java/lang/NumberFormatException",
			fmt.Sprintf("radix %d is outside %d..%d", radix, characterMinRadix, characterMaxRadix))
	}
	return parseJavaInteger(text, int(radix), bits)
}

func longPair(arguments []Value) (int64, int64, error) {
	left, err := nativeLong(arguments, 0)
	if err != nil {
		return 0, 0, err
	}
	right, err := nativeLong(arguments, 1)
	if err != nil {
		return 0, 0, err
	}
	return left, right, nil
}

// boxedInt and boxedLong read the value a box carries. A box holds its number
// natively rather than in a field, which is what lets the runtime hand one
// back without running a constructor.
// booleanBox normalizes what a boxed flag holds. A guest may pass any
// non-zero int for true, and two boxes of the same flag have to compare equal.
func booleanBox(value int32) int32 {
	if value != 0 {
		return 1
	}
	return 0
}

func booleanText(value int32) string {
	if value != 0 {
		return "true"
	}
	return "false"
}

func boxedInt(arguments []Value, index int) (int32, error) {
	object, err := requireObject(arguments, index)
	if err != nil {
		return 0, err
	}
	value, ok := object.Native.(int32)
	if !ok {
		return 0, fmt.Errorf("receiver is not a boxed int")
	}
	return value, nil
}

func boxedLong(arguments []Value, index int) (int64, error) {
	object, err := requireObject(arguments, index)
	if err != nil {
		return 0, err
	}
	value, ok := object.Native.(int64)
	if !ok {
		return 0, fmt.Errorf("receiver is not a boxed long")
	}
	return value, nil
}

// registerRuntimeBuiltins answers the two memory questions. A handset had a
// heap of a few hundred kilobytes and titles size their caches from these
// numbers, so what is reported is the guest heap this runtime is willing to
// give rather than the host's, which is orders of magnitude larger and would
// have a title allocate accordingly.
func (vm *VM) registerRuntimeBuiltins() {
	vm.builtin(RuntimeClass, "totalMemory", "()J", func(_ *VM, _ []Value) (Value, error) {
		return LongValue(guestHeapBytes), nil
	})
	vm.builtin(RuntimeClass, "freeMemory", "()J", func(_ *VM, _ []Value) (Value, error) {
		var statistics runtime.MemStats
		runtime.ReadMemStats(&statistics)
		used := int64(statistics.HeapAlloc)
		if used >= guestHeapBytes {
			// A host that is using more than the whole guest heap says
			// nothing about the guest; report the floor rather than a
			// negative number a title would read as an overflow.
			return LongValue(guestHeapFloorBytes), nil
		}
		free := guestHeapBytes - used
		if free < guestHeapFloorBytes {
			free = guestHeapFloorBytes
		}
		return LongValue(free), nil
	})
	// Class.forName is how one title reaches a class it names in a string. It
	// answers a class object for every name this VM can resolve, and the
	// standard exception when there is none, which the title catches.
	vm.builtin(ClassClass, "forName", "(Ljava/lang/String;)Ljava/lang/Class;", func(vm *VM, arguments []Value) (Value, error) {
		name, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		internal := strings.ReplaceAll(name, ".", "/")
		if !vm.classForNameExists(internal) {
			return VoidValue(), guestException("java/lang/ClassNotFoundException", name)
		}
		return ReferenceValue(vm.classObject(internal)), nil
	})
	// Class.newInstance is what a title that named a class in a string does
	// next: it makes the object. It runs on the caller's execution, so the
	// constructor is the caller's own code underneath it, as every other
	// library callback here is.
	vm.contextBuiltin(ClassClass, "newInstance", "()Ljava/lang/Object;", func(vm *VM, state *execution, arguments []Value) (Value, error) {
		name, err := classObjectName(arguments)
		if err != nil {
			return VoidValue(), err
		}
		object, err := vm.newInstance(state, name)
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(object), nil
	})
}

const (
	instantiationExceptionClass = "java/lang/InstantiationException"
	illegalAccessExceptionClass = "java/lang/IllegalAccessException"
)

// newInstance makes an object of the class named and runs the class's own
// no-argument constructor, refusing what CLDC 1.1 refuses: an array class, an
// interface or an abstract class, and a class with no `<init>()V` of its own,
// with InstantiationException; a class or constructor the caller may not
// reach, with IllegalAccessException. An exception the constructor throws
// reaches the caller as itself.
//
// A class the VM loads — the library's own and every class file — is
// constructed here. A class that exists only as AOT metadata is a title's own
// on a platform that compiled it, whose constructor is the platform's code; it
// is checked against that metadata and made by the platform's AOTConstructor,
// or through the AOT invoker when none is installed.
func (vm *VM) newInstance(state *execution, name string) (*Object, error) {
	call := &Invocation{vm: vm, state: state}
	if strings.HasPrefix(name, "[") {
		return nil, guestException(instantiationExceptionClass, javaClassName(name)+" is an array class")
	}
	caller := callerClassName(state)
	// Object has no declaration behind it to read, and it is public with a
	// public constructor.
	if name == ObjectClass {
		return call.NewObject(ObjectClass, "()V")
	}
	if class, err := vm.loader.Load(name); err == nil {
		constructor := class.FindMethod("<init>", "()V")
		var flags uint16
		if constructor != nil {
			flags = constructor.AccessFlags
		}
		if err := instantiable(name, class.AccessFlags, constructor != nil, flags, caller); err != nil {
			return nil, err
		}
		return call.NewObject(name, "()V")
	}
	metadata, ok := vm.AOTClass(name)
	if !ok {
		return nil, fmt.Errorf("Class.newInstance of %s, which this VM neither loads nor has as AOT metadata", name)
	}
	var constructor *AOTMethodMetadata
	for index := range metadata.Methods {
		method := &metadata.Methods[index]
		if method.Name == "<init>" && method.Descriptor == "()V" && method.AccessFlags&AccessStatic == 0 {
			constructor = method
			break
		}
	}
	var flags uint16
	if constructor != nil {
		flags = constructor.AccessFlags
	}
	if err := instantiable(name, metadata.AccessFlags, constructor != nil, flags, caller); err != nil {
		return nil, err
	}
	vm.mu.RLock()
	construct := vm.aotConstructor
	vm.mu.RUnlock()
	if construct != nil {
		return construct(call, metadata)
	}
	object, err := vm.NewAOTInstance(metadata.Address)
	if err != nil {
		return nil, err
	}
	_, handled, err := vm.invokeAOTFallback(state, name, "<init>", "()V", []Value{ReferenceValue(object)})
	if !handled {
		return nil, fmt.Errorf("Class.newInstance of %s, whose constructor no platform here runs", name)
	}
	if err != nil {
		return nil, err
	}
	return object, nil
}

// instantiable is CLDC's two refusals: what cannot be instantiated first, then
// what may not be reached. Access is the language's: a public class or one in
// the caller's package, and a constructor that is public, the caller's own
// class's when private, or in the caller's package otherwise — protected
// included, since a constructor reached through newInstance is called on no
// subclass's behalf.
//
// The caller is the class whose method is running, and a call from code this
// VM does not interpret — a platform's compiled title — has none to read. It
// is taken to be another class of the same package, the shape of a title's own
// code calling into its own classes: a private constructor is refused and the
// rest are reachable.
func instantiable(name string, classFlags uint16, hasConstructor bool, constructorFlags uint16, caller string) error {
	dotted := javaClassName(name)
	if classFlags&(AccessInterface|AccessAbstract) != 0 {
		return guestException(instantiationExceptionClass, dotted+" is an interface or an abstract class")
	}
	if !hasConstructor {
		return guestException(instantiationExceptionClass, dotted+" declares no constructor without arguments")
	}
	if classFlags&AccessPublic == 0 && caller != "" && !samePackage(caller, name) {
		return guestException(illegalAccessExceptionClass, dotted+" is not public")
	}
	switch {
	case constructorFlags&AccessPublic != 0:
	case constructorFlags&AccessPrivate != 0:
		if caller != name {
			return guestException(illegalAccessExceptionClass, dotted+"() is private")
		}
	case caller != "" && !samePackage(caller, name):
		return guestException(illegalAccessExceptionClass, dotted+"() is not visible outside its package")
	}
	return nil
}

// callerClassName is the class of the bytecode method a native was called
// from, or nothing when the call did not come from interpreted code.
func callerClassName(state *execution) string {
	if state == nil || state.topFrame == nil || state.topFrame.class == nil {
		return ""
	}
	return state.topFrame.class.Name
}

// samePackage reports whether two internal class names are in one package.
func samePackage(left, right string) bool {
	packageOf := func(name string) string {
		if index := strings.LastIndexByte(name, '/'); index >= 0 {
			return name[:index]
		}
		return ""
	}
	return packageOf(left) == packageOf(right)
}

// classForNameExists answers Class.forName's only question. A name reaches it
// three ways: an array descriptor, which has no class file and exists when its
// element type does; a class the platform compiled ahead of time, which lives
// in the AOT registry and never in the loader — a title asking for its own
// main class arrives here, and answering "not found" left one of them
// repainting an empty card for its whole run; and a class file the loader can
// read.
func (vm *VM) classForNameExists(name string) bool {
	if element, isArray := arrayElementClassName(name); isArray {
		return element == "" || vm.classForNameExists(element)
	}
	if _, registered := vm.AOTClass(name); registered {
		return true
	}
	_, err := vm.loader.Load(name)
	return err == nil
}

// arrayElementClassName reports whether a name is an array descriptor and, if
// it is, the class name of its innermost element. A primitive element gives an
// empty name: there is nothing to look up, and the type still exists.
func arrayElementClassName(name string) (string, bool) {
	if len(name) == 0 || name[0] != '[' {
		return "", false
	}
	typeInfo, err := ParseFieldDescriptor(name)
	if err != nil {
		return "", false
	}
	for typeInfo.Kind == TypeArray {
		if typeInfo.Component == nil {
			return "", false
		}
		typeInfo = *typeInfo.Component
	}
	if typeInfo.Kind != TypeReference {
		return "", true
	}
	return typeInfo.ClassName, true
}

const (
	// guestHeapBytes is what a title is told the heap is. The handsets these
	// archives shipped for had between 512KiB and 2MiB for a MIDlet; the
	// larger figure is the one that lets a title that sizes a cache from it
	// behave as it did on the later handsets, without inviting an allocation
	// no emulator would refuse but no handset could have served.
	guestHeapBytes = 2 << 20
	// guestHeapFloorBytes is the least free memory ever reported. A title that
	// reads zero free stops loading, and there is no real exhaustion here to
	// report: the host allocates on demand.
	guestHeapFloorBytes = 256 << 10
)

// registerStringExtraBuiltins is the text surface a local title reached and
// this runtime did not have.
func (vm *VM) registerStringExtraBuiltins() {
	// regionMatches compares len units of the receiver from toffset with len
	// units of other from ooffset. It is the class library's own test: a run
	// that does not fit inside its string is false rather than a failure, a
	// length of zero or less matches, and a null other is the
	// NullPointerException the call dereferences it into. Ignoring case is
	// Character's mapping — ISO Latin-1, which is all CLDC's Character maps —
	// asked twice: equal once both are upper case, or once those are lower
	// case again. LGT answers the same (internal/platform/lgt).
	vm.builtin(StringClass, "regionMatches", "(ZILjava/lang/String;II)Z", func(_ *VM, arguments []Value) (Value, error) {
		units, err := stringUnits(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		ignoreCase, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		from, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		other, err := stringUnits(arguments, 3)
		if err != nil {
			return VoidValue(), err
		}
		otherFrom, err := nativeInt(arguments, 4)
		if err != nil {
			return VoidValue(), err
		}
		length, err := nativeInt(arguments, 5)
		if err != nil {
			return VoidValue(), err
		}
		if otherFrom < 0 || from < 0 || int64(from) > int64(len(units))-int64(length) ||
			int64(otherFrom) > int64(len(other))-int64(length) {
			return IntValue(0), nil
		}
		for index := int32(0); index < length; index++ {
			if !unitsMatch(units[from+index], other[otherFrom+index], ignoreCase != 0) {
				return IntValue(0), nil
			}
		}
		return IntValue(1), nil
	})
	// startsWith(prefix, toffset) is the prefix test at an index rather than
	// at the start. An index outside the string, or one the prefix would run
	// past the end from, is false; a null prefix is a NullPointerException.
	vm.builtin(StringClass, "startsWith", "(Ljava/lang/String;I)Z", func(_ *VM, arguments []Value) (Value, error) {
		units, err := stringUnits(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		prefix, err := stringUnits(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		at, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		if at < 0 || int64(at) > int64(len(units))-int64(len(prefix)) {
			return IntValue(0), nil
		}
		return booleanValue(slices.Equal(units[at:int(at)+len(prefix)], prefix)), nil
	})
	vm.builtin(StringClass, "valueOf", "(Z)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(strconv.FormatBool(value != 0))
	})
	vm.builtin(StringClass, "indexOf", "(II)I", func(_ *VM, arguments []Value) (Value, error) {
		units, err := stringUnits(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		character, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		from, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		if from < 0 {
			from = 0
		}
		for index := int(from); index < len(units); index++ {
			if int32(units[index]) == character {
				return IntValue(int32(index)), nil
			}
		}
		return IntValue(-1), nil
	})
	vm.builtin(StringClass, "lastIndexOf", "(I)I", func(_ *VM, arguments []Value) (Value, error) {
		units, err := stringUnits(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		character, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		for index := len(units) - 1; index >= 0; index-- {
			if int32(units[index]) == character {
				return IntValue(int32(index)), nil
			}
		}
		return IntValue(-1), nil
	})
	// The bounded form starts at the index the caller names and searches back
	// from there, which is how a title walks a path backwards one separator at
	// a time.
	vm.builtin(StringClass, "lastIndexOf", "(II)I", func(_ *VM, arguments []Value) (Value, error) {
		units, err := stringUnits(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		character, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		from, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		if from >= int32(len(units)) {
			from = int32(len(units)) - 1
		}
		for index := from; index >= 0; index-- {
			if int32(units[index]) == character {
				return IntValue(index), nil
			}
		}
		return IntValue(-1), nil
	})
	vm.builtin(StringClass, "replace", "(CC)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		units, err := stringUnits(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		from, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		to, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		replaced := make([]uint16, len(units))
		copy(replaced, units)
		for index := range replaced {
			if int32(replaced[index]) == from {
				replaced[index] = uint16(to)
			}
		}
		return vm.stringResult(string(utf16.Decode(replaced)))
	})
	vm.builtin(StringClass, "valueOf", "([C)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		array, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		_, values, err := ArraySnapshot(array)
		if err != nil {
			return VoidValue(), err
		}
		text, err := charArrayString(array, 0, int32(len(values)))
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(text)
	})
	vm.builtin(StringClass, "valueOf", "([CII)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		array, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		offset, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		length, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		text, err := charArrayString(array, offset, length)
		if err != nil {
			return VoidValue(), err
		}
		return vm.stringResult(text)
	})
	vm.builtin(StringClass, "getBytes", "(Ljava/lang/String;)[B", func(vm *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		encoding, err := nativeString(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		switch charsetOf(encoding) {
		case charsetUTF8:
			return vm.byteArrayResult([]byte(value))
		case charsetPlatform:
			return vm.byteArrayResult(vm.encodePlatformString(value))
		}
		return VoidValue(), guestException("java/io/IOException", "unsupported character encoding: "+encoding)
	})
	// The four-argument constructor is the ranged form of the one that takes a
	// charset, and a title that decodes a record it read into a longer buffer
	// needs the range rather than a copy of it.
	vm.builtin(StringClass, "<init>", "([BIILjava/lang/String;)V", func(vm *VM, arguments []Value) (Value, error) {
		object, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		array, err := nativeReference(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		data, err := ByteArraySnapshot(array)
		if err != nil {
			return VoidValue(), err
		}
		offset, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		length, err := nativeInt(arguments, 3)
		if err != nil {
			return VoidValue(), err
		}
		if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(data)) {
			return VoidValue(), guestException("java/lang/IndexOutOfBoundsException", "String byte range")
		}
		encoding, err := nativeString(arguments, 4)
		if err != nil {
			return VoidValue(), err
		}
		ranged := data[int(offset) : int(offset)+int(length)]
		switch charsetOf(encoding) {
		case charsetUTF8:
			return VoidValue(), vm.setStringText(object, strings.ToValidUTF8(string(ranged), "�"))
		case charsetPlatform:
			return VoidValue(), vm.setStringText(object, vm.decodePlatformBytes(ranged))
		}
		return VoidValue(), guestException("java/io/IOException", "unsupported character encoding: "+encoding)
	})
	// A String built from a StringBuffer takes what the buffer holds now: the
	// two do not share storage afterwards, which is what a title that keeps
	// appending to the buffer it just read is relying on.
	vm.builtin(StringClass, "<init>", "(Ljava/lang/StringBuffer;)V", func(vm *VM, arguments []Value) (Value, error) {
		object, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		buffer, err := requireObject(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		data, ok := buffer.Native.(*stringBufferData)
		if !ok || data == nil {
			return VoidValue(), fmt.Errorf("argument is not a StringBuffer")
		}
		// Every read and write of the units takes the buffer's lock, because a
		// title shares a StringBuffer between threads on the strength of it
		// being synchronized.
		data.mu.Lock()
		text := string(utf16.Decode(data.units))
		data.mu.Unlock()
		return VoidValue(), vm.setStringText(object, text)
	})
	vm.builtin(StringBufferClass, "setCharAt", "(IC)V", func(_ *VM, arguments []Value) (Value, error) {
		_, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		index, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		character, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		defer data.mu.Unlock()
		if index < 0 || int(index) >= len(data.units) {
			return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException", fmt.Sprintf("index %d", index))
		}
		data.units[index] = uint16(character)
		return VoidValue(), nil
	})
	vm.builtin(StringBufferClass, "charAt", "(I)C", func(_ *VM, arguments []Value) (Value, error) {
		_, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		index, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		defer data.mu.Unlock()
		if index < 0 || int(index) >= len(data.units) {
			return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException", fmt.Sprintf("index %d", index))
		}
		return IntValue(int32(data.units[index])), nil
	})
}

// stringUnits reads a string receiver as the UTF-16 units the guest indexes,
// which is what every index a Java string method takes or answers is in.
func stringUnits(arguments []Value, index int) ([]uint16, error) {
	value, err := nativeString(arguments, index)
	if err != nil {
		return nil, err
	}
	return utf16.Encode([]rune(value)), nil
}

// unitsMatch is the comparison regionMatches makes of one pair of units: the
// same unit, or with case ignored the same once both are upper case, or once
// those are lower case again, as Character maps them.
func unitsMatch(left, right uint16, ignoreCase bool) bool {
	if left == right {
		return true
	}
	if !ignoreCase {
		return false
	}
	upperLeft, upperRight := latin1Upper(rune(left)), latin1Upper(rune(right))
	return upperLeft == upperRight || latin1Lower(upperLeft) == latin1Lower(upperRight)
}

// charArrayString reads a range of a char array as text.
func charArrayString(array *Object, offset, length int32) (string, error) {
	component, values, err := ArraySnapshot(array)
	if err != nil {
		return "", err
	}
	if component.Kind != TypeChar {
		return "", fmt.Errorf("argument is not a char array")
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(values)) {
		return "", guestException("java/lang/IndexOutOfBoundsException", "String char range")
	}
	units := make([]uint16, length)
	for index := range units {
		unit, unitErr := values[int(offset)+index].Int32()
		if unitErr != nil {
			return "", unitErr
		}
		units[index] = uint16(unit)
	}
	return string(utf16.Decode(units)), nil
}

// The charsets a title may name. There are only two answers to give: the one
// the standard requires everywhere, and the handset's own — every platform
// this runtime serves is a Korean handset whose default charset is EUC-KR, and
// that is the encoder and decoder the Host installs. A name that is neither is
// refused rather than guessed at, because decoding text with the wrong table
// produces a screen full of plausible-looking mistakes.
type charset int

const (
	charsetUnknown charset = iota
	charsetUTF8
	charsetPlatform
)

func charsetOf(name string) charset {
	normalized := strings.ToUpper(name)
	for _, cut := range []string{"-", "_", " "} {
		normalized = strings.ReplaceAll(normalized, cut, "")
	}
	switch normalized {
	case "UTF8":
		return charsetUTF8
	// Every name here is already normalized: upper case with the separators
	// taken out, because that is what the switch is reading. A name spelled
	// with its hyphens can never match and is a case that does nothing.
	case "EUCKR", "KSC5601", "KSC56011987", "KSC56011989", "MS949", "CP949", "XWINDOWS949", "WINDOWS949":
		return charsetPlatform
	}
	return charsetUnknown
}
