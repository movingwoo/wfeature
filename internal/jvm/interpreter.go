package jvm

import (
	"errors"
	"fmt"
	"math"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

var ErrUnsupportedOpcode = errors.New("unsupported JVM opcode")

type stepResult struct {
	returned bool
	value    Value
}

func (vm *VM) execute(
	state *execution,
	class *classfile.Class,
	method *classfile.Member,
	code *classfile.Code,
	arguments []Value,
) (Value, error) {
	if state.frames >= vm.config.MaxFrames {
		return VoidValue(), ErrFrameLimit
	}
	state.frames++
	defer func() { state.frames-- }()

	frame, err := newFrame(state, class, method, code, arguments)
	if err != nil {
		return VoidValue(), fmt.Errorf("create frame for %s.%s%s: %w", class.Name, method.Name, method.Descriptor, err)
	}
	defer state.releaseFrame(frame)
	for {
		if frame.pc < 0 || frame.pc >= len(frame.code.Bytecode) {
			return VoidValue(), fmt.Errorf("method %s.%s%s fell off bytecode at pc %d", class.Name, method.Name, method.Descriptor, frame.pc)
		}
		if state.steps >= vm.config.MaxSteps {
			if err := vm.renewSteps(state); err != nil {
				return VoidValue(), err
			}
		}
		opcodePC := frame.pc
		opcode, _ := frame.byte()
		state.steps++
		if vm.traceInstructions {
			vm.config.Logger.Debug("jvm instruction",
				"class", class.Name,
				"method", method.Name,
				"descriptor", method.Descriptor,
				"pc", opcodePC,
				"opcode", fmt.Sprintf("0x%02x", opcode),
				"stack", len(frame.stack),
			)
		}

		result, err := vm.step(state, frame, opcodePC, opcode)
		if err != nil {
			var guest *GuestException
			if errors.As(err, &guest) {
				handled, handleErr := vm.handleException(frame, opcodePC, guest)
				if handleErr != nil {
					return VoidValue(), handleErr
				}
				if handled {
					continue
				}
			}
			return VoidValue(), &ExecutionError{
				Class:      class.Name,
				Method:     method.Name,
				Descriptor: method.Descriptor,
				PC:         opcodePC,
				Opcode:     opcode,
				Cause:      err,
			}
		}
		if result.returned {
			return result.value, nil
		}
	}
}

func (vm *VM) step(state *execution, frame *frame, opcodePC int, opcode byte) (stepResult, error) {
	push := func(value Value) (stepResult, error) {
		return stepResult{}, frame.push(value)
	}

	switch {
	case opcode == 0x00:
		return stepResult{}, nil
	case opcode == 0x01:
		return push(ReferenceValue(nil))
	case opcode >= 0x02 && opcode <= 0x08:
		return push(IntValue(int32(opcode) - 3))
	case opcode >= 0x09 && opcode <= 0x0a:
		return push(LongValue(int64(opcode - 0x09)))
	case opcode >= 0x0b && opcode <= 0x0d:
		return push(FloatValue(float32(opcode - 0x0b)))
	case opcode >= 0x0e && opcode <= 0x0f:
		return push(DoubleValue(float64(opcode - 0x0e)))
	case opcode == 0x10:
		value, err := frame.byte()
		if err != nil {
			return stepResult{}, err
		}
		return push(IntValue(int32(int8(value))))
	case opcode == 0x11:
		value, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		return push(IntValue(int32(int16(value))))
	case opcode == 0x12:
		index, err := frame.byte()
		if err != nil {
			return stepResult{}, err
		}
		value, err := constantValue(frame.class.ConstantPool, uint16(index), false)
		if err != nil {
			return stepResult{}, err
		}
		return push(value)
	case opcode == 0x13 || opcode == 0x14:
		index, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		value, err := constantValue(frame.class.ConstantPool, index, opcode == 0x14)
		if err != nil {
			return stepResult{}, err
		}
		return push(value)
	case opcode >= 0x15 && opcode <= 0x19:
		index, err := frame.byte()
		if err != nil {
			return stepResult{}, err
		}
		return loadLocal(frame, int(index), loadStoreKind(opcode-0x15), push)
	case opcode >= 0x1a && opcode <= 0x2d:
		group := (opcode - 0x1a) / 4
		index := int((opcode - 0x1a) % 4)
		return loadLocal(frame, index, loadStoreKind(group), push)
	case opcode >= 0x2e && opcode <= 0x35:
		value, err := arrayLoad(frame, opcode)
		if err != nil {
			return stepResult{}, err
		}
		return push(value)
	case opcode >= 0x36 && opcode <= 0x3a:
		index, err := frame.byte()
		if err != nil {
			return stepResult{}, err
		}
		return stepResult{}, storeLocal(frame, int(index), loadStoreKind(opcode-0x36))
	case opcode >= 0x3b && opcode <= 0x4e:
		group := (opcode - 0x3b) / 4
		index := int((opcode - 0x3b) % 4)
		return stepResult{}, storeLocal(frame, index, loadStoreKind(group))
	case opcode >= 0x4f && opcode <= 0x56:
		return stepResult{}, vm.arrayStore(frame, opcode)
	case opcode == 0x57:
		value, err := frame.pop()
		if err != nil {
			return stepResult{}, err
		}
		if value.slots() != 1 {
			return stepResult{}, fmt.Errorf("pop requires a category 1 value")
		}
		return stepResult{}, nil
	case opcode == 0x58:
		value, err := frame.pop()
		if err != nil {
			return stepResult{}, err
		}
		if value.slots() == 2 {
			return stepResult{}, nil
		}
		other, err := frame.pop()
		if err != nil {
			return stepResult{}, err
		}
		if value.slots() != 1 || other.slots() != 1 {
			return stepResult{}, fmt.Errorf("pop2 has an invalid operand shape")
		}
		return stepResult{}, nil
	case opcode == 0x59:
		value, err := frame.pop()
		if err != nil {
			return stepResult{}, err
		}
		if value.slots() != 1 {
			return stepResult{}, fmt.Errorf("dup requires a category 1 value")
		}
		if err := frame.push(value); err != nil {
			return stepResult{}, err
		}
		return push(value)
	case opcode >= 0x5a && opcode <= 0x5e:
		return stepResult{}, duplicateStack(frame, opcode)
	case opcode == 0x5f:
		right, err := frame.pop()
		if err != nil {
			return stepResult{}, err
		}
		left, err := frame.pop()
		if err != nil {
			return stepResult{}, err
		}
		if left.slots() != 1 || right.slots() != 1 {
			return stepResult{}, fmt.Errorf("swap requires category 1 values")
		}
		if err := frame.push(right); err != nil {
			return stepResult{}, err
		}
		return push(left)
	case opcode >= 0x60 && opcode <= 0x73:
		return binaryArithmetic(frame, opcode, push)
	case opcode >= 0x74 && opcode <= 0x77:
		return unaryNegation(frame, opcode, push)
	case opcode >= 0x78 && opcode <= 0x83:
		return integerBitOperation(frame, opcode, push)
	case opcode == 0x84:
		index, err := frame.byte()
		if err != nil {
			return stepResult{}, err
		}
		constant, err := frame.byte()
		if err != nil {
			return stepResult{}, err
		}
		return stepResult{}, incrementLocal(frame, int(index), int32(int8(constant)))
	case opcode >= 0x85 && opcode <= 0x93:
		return convertValue(frame, opcode, push)
	case opcode >= 0x94 && opcode <= 0x98:
		return compareValues(frame, opcode, push)
	case opcode >= 0x99 && opcode <= 0x9e:
		offset, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		value, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		conditions := []bool{value == 0, value != 0, value < 0, value >= 0, value > 0, value <= 0}
		if conditions[opcode-0x99] {
			return stepResult{}, frame.branch(opcodePC, int32(int16(offset)))
		}
		return stepResult{}, nil
	case opcode >= 0x9f && opcode <= 0xa4:
		offset, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		right, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		conditions := []bool{left == right, left != right, left < right, left >= right, left > right, left <= right}
		if conditions[opcode-0x9f] {
			return stepResult{}, frame.branch(opcodePC, int32(int16(offset)))
		}
		return stepResult{}, nil
	case opcode == 0xa5 || opcode == 0xa6:
		offset, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		right, err := popReference(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popReference(frame)
		if err != nil {
			return stepResult{}, err
		}
		equal := left == right
		if (opcode == 0xa5 && equal) || (opcode == 0xa6 && !equal) {
			return stepResult{}, frame.branch(opcodePC, int32(int16(offset)))
		}
		return stepResult{}, nil
	case opcode == 0xa7:
		offset, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		return stepResult{}, frame.branch(opcodePC, int32(int16(offset)))
	case opcode == 0xa8:
		offset, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		if err := frame.push(returnAddressValue(frame.pc)); err != nil {
			return stepResult{}, err
		}
		return stepResult{}, frame.branch(opcodePC, int32(int16(offset)))
	case opcode == 0xa9:
		index, err := frame.byte()
		if err != nil {
			return stepResult{}, err
		}
		return stepResult{}, returnFromSubroutine(frame, int(index))
	case opcode == 0xaa:
		return stepResult{}, tableSwitch(frame, opcodePC)
	case opcode == 0xab:
		return stepResult{}, lookupSwitch(frame, opcodePC)
	case opcode >= 0xac && opcode <= 0xb1:
		return returnValue(frame, opcode)
	case opcode >= 0xb2 && opcode <= 0xb5:
		index, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		reference, err := frame.class.ConstantPool.ReferenceAt(index)
		if err != nil || reference.Kind != classfile.FieldReference {
			return stepResult{}, fmt.Errorf("invalid field reference at constant %d: %v", index, err)
		}
		if opcode == 0xb2 {
			value, err := vm.staticValue(state, reference)
			if err != nil {
				return stepResult{}, err
			}
			return push(value)
		}
		if opcode == 0xb3 {
			value, err := frame.pop()
			if err != nil {
				return stepResult{}, err
			}
			if err := vm.setStaticValue(state, reference, value); err != nil {
				return stepResult{}, err
			}
			// The key is the resolved one, because that is the slot the write
			// went to: an observer answering "what wrote this" has to name the
			// field it can read back. Composing it is a concatenation, so it
			// waits until there is somebody to read it.
			if vm.watchingStores() {
				resolved := vm.resolveFieldReference(reference)
				vm.observeStore(StoreEvent{
					Class: resolved.Class, Key: fieldReferenceKey(resolved), Index: -1, Value: value,
					SiteClass: frame.class.Name, SiteMethod: frame.method.Name, SitePC: frame.pc,
				})
			}
			return stepResult{}, nil
		}
		if opcode == 0xb4 {
			object, err := popReference(frame)
			if err != nil {
				return stepResult{}, err
			}
			value, err := vm.instanceValue(object, reference)
			if err != nil {
				return stepResult{}, err
			}
			return push(value)
		}
		value, err := frame.pop()
		if err != nil {
			return stepResult{}, err
		}
		object, err := popReference(frame)
		if err != nil {
			return stepResult{}, err
		}
		if err := vm.setInstanceValue(object, reference, value); err != nil {
			return stepResult{}, err
		}
		// Reported after the store and only when it happened: an observer is
		// answering "what wrote this", and a store that threw wrote nothing.
		if vm.watchingStores() {
			vm.observeStore(StoreEvent{
				Object: object, Key: fieldReferenceKey(vm.resolveFieldReference(reference)), Index: -1, Value: value,
				SiteClass: frame.class.Name, SiteMethod: frame.method.Name, SitePC: frame.pc,
			})
		}
		return stepResult{}, nil
	case opcode == 0xb6 || opcode == 0xb7 || opcode == 0xb8 || opcode == 0xb9:
		index, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		reference, err := frame.class.ConstantPool.ReferenceAt(index)
		if err != nil || (reference.Kind != classfile.MethodReference && reference.Kind != classfile.InterfaceMethodReference) {
			return stepResult{}, fmt.Errorf("invalid method reference at constant %d: %v", index, err)
		}
		if opcode == 0xb9 {
			count, readErr := frame.byte()
			if readErr != nil {
				return stepResult{}, readErr
			}
			zero, readErr := frame.byte()
			if readErr != nil {
				return stepResult{}, readErr
			}
			if count == 0 || zero != 0 {
				return stepResult{}, fmt.Errorf("invalid invokeinterface operands")
			}
		}
		methodType, err := ParseMethodDescriptor(reference.Descriptor)
		if err != nil {
			return stepResult{}, err
		}
		// One slice with the receiver's slot in front of the arguments. A
		// bytecode frame's locals and a native's argument list both start with
		// `this`, so building the arguments alone and prepending the receiver
		// afterwards allocated twice per guest method call — and that was
		// eighty per cent of everything a title allocated. A static call spends
		// the leading slot and hands over the rest.
		slots := make([]Value, len(methodType.Parameters)+1)
		arguments := slots[1:]
		for argumentIndex := len(arguments) - 1; argumentIndex >= 0; argumentIndex-- {
			arguments[argumentIndex], err = frame.pop()
			if err != nil {
				return stepResult{}, err
			}
		}
		if err := validateArguments(arguments, methodType.Parameters); err != nil {
			return stepResult{}, err
		}
		if opcode == 0xb8 {
			result, err := vm.invokeStatic(state, reference.Class, reference.Name, reference.Descriptor, arguments)
			if err != nil {
				return stepResult{}, err
			}
			if methodType.Return.Kind != TypeVoid {
				return push(result)
			}
			return stepResult{}, nil
		}
		receiver, err := popReference(frame)
		if err != nil {
			return stepResult{}, err
		}
		if receiver == nil {
			return stepResult{}, guestException("java/lang/NullPointerException", "invoke "+reference.Class+"."+reference.Name+reference.Descriptor)
		}
		lookupClass := receiver.ClassName
		if opcode == 0xb7 {
			lookupClass = reference.Class
		}
		slots[0] = ReferenceValue(receiver)
		result, err := vm.invokeInstanceReceived(state, lookupClass, receiver, reference.Name, reference.Descriptor, slots)
		if err != nil {
			return stepResult{}, err
		}
		if methodType.Return.Kind != TypeVoid {
			return push(result)
		}
		return stepResult{}, nil
	case opcode == 0xbb:
		index, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		className, err := frame.class.ConstantPool.ClassName(index)
		if err != nil {
			return stepResult{}, err
		}
		object, err := vm.newObject(state, className)
		if err != nil {
			return stepResult{}, err
		}
		return push(ReferenceValue(object))
	case opcode == 0xbc:
		arrayType, err := frame.byte()
		if err != nil {
			return stepResult{}, err
		}
		component, err := primitiveArrayType(arrayType)
		if err != nil {
			return stepResult{}, err
		}
		length, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		array, err := vm.newArray(component, length)
		if err != nil {
			return stepResult{}, err
		}
		return push(ReferenceValue(array))
	case opcode == 0xbd:
		index, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		className, err := frame.class.ConstantPool.ClassName(index)
		if err != nil {
			return stepResult{}, err
		}
		component := Type{Kind: TypeReference, ClassName: className}
		if len(className) > 0 && className[0] == '[' {
			component, err = ParseFieldDescriptor(className)
			if err != nil {
				return stepResult{}, err
			}
		}
		length, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		array, err := vm.newArray(component, length)
		if err != nil {
			return stepResult{}, err
		}
		return push(ReferenceValue(array))
	case opcode == 0xbe:
		object, err := popReference(frame)
		if err != nil {
			return stepResult{}, err
		}
		array, err := objectArray(object)
		if err != nil {
			return stepResult{}, err
		}
		return push(IntValue(int32(array.Length())))
	case opcode == 0xbf:
		object, err := popReference(frame)
		if err != nil {
			return stepResult{}, err
		}
		if object == nil {
			return stepResult{}, guestException("java/lang/NullPointerException", "athrow null")
		}
		message, _ := object.Native.(string)
		return stepResult{}, &GuestException{Object: object, Message: message}
	case opcode == 0xc0 || opcode == 0xc1:
		index, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		target, err := frame.class.ConstantPool.ClassName(index)
		if err != nil {
			return stepResult{}, err
		}
		object, err := popReference(frame)
		if err != nil {
			return stepResult{}, err
		}
		if opcode == 0xc1 {
			if vm.isInstance(object, target) {
				return push(IntValue(1))
			}
			return push(IntValue(0))
		}
		if object != nil && !vm.isInstance(object, target) {
			return stepResult{}, guestException(
				"java/lang/ClassCastException",
				fmt.Sprintf("%s cannot be cast to %s", object.ClassName, target),
			)
		}
		return push(ReferenceValue(object))
	case opcode == 0xc2 || opcode == 0xc3:
		object, err := popReference(frame)
		if err != nil {
			return stepResult{}, err
		}
		if object == nil {
			return stepResult{}, guestException("java/lang/NullPointerException", "monitor operation")
		}
		if opcode == 0xc2 {
			object.monitor.enter(state.id)
			return stepResult{}, nil
		}
		if err := object.monitor.exit(state.id); err != nil {
			return stepResult{}, guestException("java/lang/IllegalMonitorStateException", err.Error())
		}
		return stepResult{}, nil
	case opcode == 0xc4:
		return wideInstruction(frame, push)
	case opcode == 0xc5:
		index, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		dimensions, err := frame.byte()
		if err != nil {
			return stepResult{}, err
		}
		if dimensions == 0 {
			return stepResult{}, fmt.Errorf("multianewarray has zero dimensions")
		}
		descriptor, err := frame.class.ConstantPool.ClassName(index)
		if err != nil {
			return stepResult{}, err
		}
		arrayType, err := ParseFieldDescriptor(descriptor)
		if err != nil || arrayType.Kind != TypeArray {
			return stepResult{}, fmt.Errorf("invalid multianewarray descriptor %q", descriptor)
		}
		lengths := make([]int32, int(dimensions))
		for dimension := len(lengths) - 1; dimension >= 0; dimension-- {
			lengths[dimension], err = popInt(frame)
			if err != nil {
				return stepResult{}, err
			}
		}
		array, err := vm.newMultiArray(arrayType, lengths)
		if err != nil {
			return stepResult{}, err
		}
		return push(ReferenceValue(array))
	case opcode == 0xc6 || opcode == 0xc7:
		offset, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		value, err := popReference(frame)
		if err != nil {
			return stepResult{}, err
		}
		if (opcode == 0xc6 && value == nil) || (opcode == 0xc7 && value != nil) {
			return stepResult{}, frame.branch(opcodePC, int32(int16(offset)))
		}
		return stepResult{}, nil
	case opcode == 0xc8:
		offset, err := frame.i4()
		if err != nil {
			return stepResult{}, err
		}
		return stepResult{}, frame.branch(opcodePC, offset)
	case opcode == 0xc9:
		offset, err := frame.i4()
		if err != nil {
			return stepResult{}, err
		}
		if err := frame.push(returnAddressValue(frame.pc)); err != nil {
			return stepResult{}, err
		}
		return stepResult{}, frame.branch(opcodePC, offset)
	default:
		return stepResult{}, fmt.Errorf("%w: 0x%02x", ErrUnsupportedOpcode, opcode)
	}
}

func constantValue(pool classfile.ConstantPool, index uint16, wide bool) (Value, error) {
	constant, err := pool.At(index)
	if err != nil {
		return VoidValue(), err
	}
	if wide {
		switch constant.Tag {
		case classfile.ConstantLong:
			return LongValue(constant.Long), nil
		case classfile.ConstantDouble:
			return DoubleValue(constant.Double), nil
		default:
			return VoidValue(), fmt.Errorf("ldc2_w constant %d has tag %d", index, constant.Tag)
		}
	}
	switch constant.Tag {
	case classfile.ConstantInteger:
		return IntValue(constant.Integer), nil
	case classfile.ConstantFloat:
		return FloatValue(constant.Float), nil
	case classfile.ConstantString:
		value, err := pool.UTF8At(constant.Index1)
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(&Object{ClassName: "java/lang/String", Native: value}), nil
	case classfile.ConstantClass:
		name, err := pool.ClassName(index)
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(&Object{ClassName: "java/lang/Class", Native: name}), nil
	default:
		return VoidValue(), fmt.Errorf("ldc constant %d has unsupported tag %d", index, constant.Tag)
	}
}

func loadStoreKind(group byte) ValueKind {
	return []ValueKind{ValueInt, ValueLong, ValueFloat, ValueDouble, ValueReference}[group]
}

func loadLocal(frame *frame, index int, kind ValueKind, push func(Value) (stepResult, error)) (stepResult, error) {
	value, err := frame.loadLocal(index, kind)
	if err != nil {
		return stepResult{}, err
	}
	return push(value)
}

func storeLocal(frame *frame, index int, kind ValueKind) error {
	value, err := frame.pop()
	if err != nil {
		return err
	}
	if value.kind != kind && !(kind == ValueReference && value.kind == valueReturnAddress) {
		return fmt.Errorf("expected %s on operand stack, got %s", kind, value.kind)
	}
	return frame.storeLocal(index, value)
}

func popInt(frame *frame) (int32, error) {
	value, err := frame.popKind(ValueInt)
	if err != nil {
		return 0, err
	}
	return value.Int32()
}

func popLong(frame *frame) (int64, error) {
	value, err := frame.popKind(ValueLong)
	if err != nil {
		return 0, err
	}
	return value.Int64()
}

func popFloat(frame *frame) (float32, error) {
	value, err := frame.popKind(ValueFloat)
	if err != nil {
		return 0, err
	}
	return value.Float32()
}

func popDouble(frame *frame) (float64, error) {
	value, err := frame.popKind(ValueDouble)
	if err != nil {
		return 0, err
	}
	return value.Float64()
}

func popReference(frame *frame) (*Object, error) {
	value, err := frame.popKind(ValueReference)
	if err != nil {
		return nil, err
	}
	return value.Reference()
}

func binaryArithmetic(frame *frame, opcode byte, push func(Value) (stepResult, error)) (stepResult, error) {
	kind := (opcode - 0x60) % 4
	operation := (opcode - 0x60) / 4
	switch kind {
	case 0:
		right, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		var result int32
		switch operation {
		case 0:
			result = left + right
		case 1:
			result = left - right
		case 2:
			result = left * right
		case 3:
			if right == 0 {
				return stepResult{}, guestException("java/lang/ArithmeticException", "/ by zero")
			}
			result = left / right
		case 4:
			if right == 0 {
				return stepResult{}, guestException("java/lang/ArithmeticException", "/ by zero")
			}
			result = left % right
		}
		return push(IntValue(result))
	case 1:
		right, err := popLong(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popLong(frame)
		if err != nil {
			return stepResult{}, err
		}
		var result int64
		switch operation {
		case 0:
			result = left + right
		case 1:
			result = left - right
		case 2:
			result = left * right
		case 3:
			if right == 0 {
				return stepResult{}, guestException("java/lang/ArithmeticException", "/ by zero")
			}
			result = left / right
		case 4:
			if right == 0 {
				return stepResult{}, guestException("java/lang/ArithmeticException", "/ by zero")
			}
			result = left % right
		}
		return push(LongValue(result))
	case 2:
		right, err := popFloat(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popFloat(frame)
		if err != nil {
			return stepResult{}, err
		}
		var result float32
		switch operation {
		case 0:
			result = left + right
		case 1:
			result = left - right
		case 2:
			result = left * right
		case 3:
			result = left / right
		case 4:
			result = float32(math.Mod(float64(left), float64(right)))
		}
		return push(FloatValue(result))
	default:
		right, err := popDouble(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popDouble(frame)
		if err != nil {
			return stepResult{}, err
		}
		var result float64
		switch operation {
		case 0:
			result = left + right
		case 1:
			result = left - right
		case 2:
			result = left * right
		case 3:
			result = left / right
		case 4:
			result = math.Mod(left, right)
		}
		return push(DoubleValue(result))
	}
}

func unaryNegation(frame *frame, opcode byte, push func(Value) (stepResult, error)) (stepResult, error) {
	switch opcode {
	case 0x74:
		value, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		return push(IntValue(-value))
	case 0x75:
		value, err := popLong(frame)
		if err != nil {
			return stepResult{}, err
		}
		return push(LongValue(-value))
	case 0x76:
		value, err := popFloat(frame)
		if err != nil {
			return stepResult{}, err
		}
		return push(FloatValue(-value))
	default:
		value, err := popDouble(frame)
		if err != nil {
			return stepResult{}, err
		}
		return push(DoubleValue(-value))
	}
}

func integerBitOperation(frame *frame, opcode byte, push func(Value) (stepResult, error)) (stepResult, error) {
	if opcode >= 0x78 && opcode <= 0x7d {
		shift, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		if opcode == 0x79 || opcode == 0x7b || opcode == 0x7d {
			value, err := popLong(frame)
			if err != nil {
				return stepResult{}, err
			}
			amount := uint32(shift) & 0x3f
			switch opcode {
			case 0x79:
				return push(LongValue(value << amount))
			case 0x7b:
				return push(LongValue(value >> amount))
			default:
				return push(LongValue(int64(uint64(value) >> amount)))
			}
		}
		value, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		amount := uint32(shift) & 0x1f
		switch opcode {
		case 0x78:
			return push(IntValue(value << amount))
		case 0x7a:
			return push(IntValue(value >> amount))
		default:
			return push(IntValue(int32(uint32(value) >> amount)))
		}
	}

	longOperation := opcode == 0x7f || opcode == 0x81 || opcode == 0x83
	if longOperation {
		right, err := popLong(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popLong(frame)
		if err != nil {
			return stepResult{}, err
		}
		switch opcode {
		case 0x7f:
			return push(LongValue(left & right))
		case 0x81:
			return push(LongValue(left | right))
		default:
			return push(LongValue(left ^ right))
		}
	}
	right, err := popInt(frame)
	if err != nil {
		return stepResult{}, err
	}
	left, err := popInt(frame)
	if err != nil {
		return stepResult{}, err
	}
	switch opcode {
	case 0x7e:
		return push(IntValue(left & right))
	case 0x80:
		return push(IntValue(left | right))
	default:
		return push(IntValue(left ^ right))
	}
}

func incrementLocal(frame *frame, index int, constant int32) error {
	value, err := frame.loadLocal(index, ValueInt)
	if err != nil {
		return err
	}
	integer, _ := value.Int32()
	return frame.storeLocal(index, IntValue(integer+constant))
}

func convertValue(frame *frame, opcode byte, push func(Value) (stepResult, error)) (stepResult, error) {
	switch opcode {
	case 0x85, 0x86, 0x87, 0x91, 0x92, 0x93:
		value, err := popInt(frame)
		if err != nil {
			return stepResult{}, err
		}
		switch opcode {
		case 0x85:
			return push(LongValue(int64(value)))
		case 0x86:
			return push(FloatValue(float32(value)))
		case 0x87:
			return push(DoubleValue(float64(value)))
		case 0x91:
			return push(IntValue(int32(int8(value))))
		case 0x92:
			return push(IntValue(int32(uint16(value))))
		default:
			return push(IntValue(int32(int16(value))))
		}
	case 0x88, 0x89, 0x8a:
		value, err := popLong(frame)
		if err != nil {
			return stepResult{}, err
		}
		if opcode == 0x88 {
			return push(IntValue(int32(value)))
		}
		if opcode == 0x89 {
			return push(FloatValue(float32(value)))
		}
		return push(DoubleValue(float64(value)))
	case 0x8b, 0x8c, 0x8d:
		value, err := popFloat(frame)
		if err != nil {
			return stepResult{}, err
		}
		if opcode == 0x8b {
			return push(IntValue(floatToInt32(float64(value))))
		}
		if opcode == 0x8c {
			return push(LongValue(floatToInt64(float64(value))))
		}
		return push(DoubleValue(float64(value)))
	default:
		value, err := popDouble(frame)
		if err != nil {
			return stepResult{}, err
		}
		if opcode == 0x8e {
			return push(IntValue(floatToInt32(value)))
		}
		if opcode == 0x8f {
			return push(LongValue(floatToInt64(value)))
		}
		return push(FloatValue(float32(value)))
	}
}

func floatToInt32(value float64) int32 {
	if math.IsNaN(value) {
		return 0
	}
	if value >= math.MaxInt32 {
		return math.MaxInt32
	}
	if value <= math.MinInt32 {
		return math.MinInt32
	}
	return int32(value)
}

func floatToInt64(value float64) int64 {
	if math.IsNaN(value) {
		return 0
	}
	if value >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	if value <= float64(math.MinInt64) {
		return math.MinInt64
	}
	return int64(value)
}

func compareValues(frame *frame, opcode byte, push func(Value) (stepResult, error)) (stepResult, error) {
	var comparison int32
	switch opcode {
	case 0x94:
		right, err := popLong(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popLong(frame)
		if err != nil {
			return stepResult{}, err
		}
		comparison = compareOrdered(left, right)
	case 0x95, 0x96:
		right, err := popFloat(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popFloat(frame)
		if err != nil {
			return stepResult{}, err
		}
		if math.IsNaN(float64(left)) || math.IsNaN(float64(right)) {
			if opcode == 0x95 {
				comparison = -1
			} else {
				comparison = 1
			}
		} else {
			comparison = compareOrdered(left, right)
		}
	case 0x97, 0x98:
		right, err := popDouble(frame)
		if err != nil {
			return stepResult{}, err
		}
		left, err := popDouble(frame)
		if err != nil {
			return stepResult{}, err
		}
		if math.IsNaN(left) || math.IsNaN(right) {
			if opcode == 0x97 {
				comparison = -1
			} else {
				comparison = 1
			}
		} else {
			comparison = compareOrdered(left, right)
		}
	}
	return push(IntValue(comparison))
}

func compareOrdered[T ~int64 | ~float32 | ~float64](left, right T) int32 {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func tableSwitch(frame *frame, opcodePC int) error {
	if err := skipSwitchPadding(frame); err != nil {
		return err
	}
	defaultOffset, err := frame.i4()
	if err != nil {
		return err
	}
	low, err := frame.i4()
	if err != nil {
		return err
	}
	high, err := frame.i4()
	if err != nil {
		return err
	}
	count := int64(high) - int64(low) + 1
	if high < low || count*4 > int64(len(frame.code.Bytecode)-frame.pc) {
		return fmt.Errorf("invalid tableswitch range %d..%d", low, high)
	}
	offsets := make([]int32, count)
	for index := range offsets {
		if offsets[index], err = frame.i4(); err != nil {
			return err
		}
	}
	key, err := popInt(frame)
	if err != nil {
		return err
	}
	selected := defaultOffset
	if key >= low && key <= high {
		selected = offsets[int64(key)-int64(low)]
	}
	return frame.branch(opcodePC, selected)
}

func lookupSwitch(frame *frame, opcodePC int) error {
	if err := skipSwitchPadding(frame); err != nil {
		return err
	}
	defaultOffset, err := frame.i4()
	if err != nil {
		return err
	}
	pairs, err := frame.i4()
	if err != nil {
		return err
	}
	if pairs < 0 || int64(pairs)*8 > int64(len(frame.code.Bytecode)-frame.pc) {
		return fmt.Errorf("invalid lookupswitch pair count %d", pairs)
	}
	key, err := popInt(frame)
	if err != nil {
		return err
	}
	selected := defaultOffset
	var previous int32
	for index := int32(0); index < pairs; index++ {
		match, err := frame.i4()
		if err != nil {
			return err
		}
		offset, err := frame.i4()
		if err != nil {
			return err
		}
		if index > 0 && match <= previous {
			return fmt.Errorf("lookupswitch keys are not strictly increasing")
		}
		previous = match
		if match == key {
			selected = offset
		}
	}
	return frame.branch(opcodePC, selected)
}

func skipSwitchPadding(frame *frame) error {
	for frame.pc%4 != 0 {
		padding, err := frame.byte()
		if err != nil {
			return err
		}
		if padding != 0 {
			return fmt.Errorf("switch padding is not zero")
		}
	}
	return nil
}

func returnValue(frame *frame, opcode byte) (stepResult, error) {
	if opcode == 0xb1 {
		if frame.methodType.Return.Kind != TypeVoid {
			return stepResult{}, fmt.Errorf("void return in non-void method")
		}
		return stepResult{returned: true, value: VoidValue()}, nil
	}
	kinds := []ValueKind{ValueInt, ValueLong, ValueFloat, ValueDouble, ValueReference}
	want := kinds[opcode-0xac]
	value, err := frame.popKind(want)
	if err != nil {
		return stepResult{}, err
	}
	if err := validateValue(value, frame.methodType.Return); err != nil {
		return stepResult{}, err
	}
	return stepResult{returned: true, value: value}, nil
}

func wideInstruction(frame *frame, push func(Value) (stepResult, error)) (stepResult, error) {
	opcode, err := frame.byte()
	if err != nil {
		return stepResult{}, err
	}
	index, err := frame.u2()
	if err != nil {
		return stepResult{}, err
	}
	if opcode >= 0x15 && opcode <= 0x19 {
		return loadLocal(frame, int(index), loadStoreKind(opcode-0x15), push)
	}
	if opcode >= 0x36 && opcode <= 0x3a {
		return stepResult{}, storeLocal(frame, int(index), loadStoreKind(opcode-0x36))
	}
	if opcode == 0x84 {
		constant, err := frame.u2()
		if err != nil {
			return stepResult{}, err
		}
		return stepResult{}, incrementLocal(frame, int(index), int32(int16(constant)))
	}
	if opcode == 0xa9 {
		return stepResult{}, returnFromSubroutine(frame, int(index))
	}
	return stepResult{}, fmt.Errorf("%w after wide: 0x%02x", ErrUnsupportedOpcode, opcode)
}

func returnFromSubroutine(frame *frame, index int) error {
	if index < 0 || index >= len(frame.locals) {
		return fmt.Errorf("local index %d is out of bounds", index)
	}
	address := frame.locals[index]
	if address.kind != valueReturnAddress {
		return fmt.Errorf("local %d is %s, expected return address", index, address.kind)
	}
	return frame.jump(int64(address.bits))
}

func primitiveArrayType(arrayType byte) (Type, error) {
	kinds := map[byte]TypeKind{
		4:  TypeBoolean,
		5:  TypeChar,
		6:  TypeFloat,
		7:  TypeDouble,
		8:  TypeByte,
		9:  TypeShort,
		10: TypeInt,
		11: TypeLong,
	}
	kind, ok := kinds[arrayType]
	if !ok {
		return Type{}, fmt.Errorf("invalid newarray type %d", arrayType)
	}
	return Type{Kind: kind}, nil
}

func objectArray(object *Object) (*Array, error) {
	if object == nil {
		return nil, guestException("java/lang/NullPointerException", "array operation")
	}
	array, ok := object.Native.(*Array)
	if !ok {
		return nil, fmt.Errorf("object of class %s is not an array", object.ClassName)
	}
	return array, nil
}

// arrayIndex answers the object as well as the array behind it, because a
// store observer identifies what was written by the object rather than by its
// storage.
func arrayIndex(frame *frame) (*Object, *Array, int, error) {
	index, err := popInt(frame)
	if err != nil {
		return nil, nil, 0, err
	}
	object, err := popReference(frame)
	if err != nil {
		return nil, nil, 0, err
	}
	array, err := objectArray(object)
	if err != nil {
		return nil, nil, 0, err
	}
	if index < 0 || int64(index) >= int64(array.Length()) {
		return nil, nil, 0, guestException(
			"java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("index %d for length %d", index, array.Length()),
		)
	}
	return object, array, int(index), nil
}

func (vm *VM) handleException(frame *frame, opcodePC int, exception *GuestException) (bool, error) {
	className := "java/lang/Throwable"
	if exception.Object != nil && exception.Object.ClassName != "" {
		className = exception.Object.ClassName
	}
	for _, handler := range frame.code.Exceptions {
		if opcodePC < int(handler.StartPC) || opcodePC >= int(handler.EndPC) {
			continue
		}
		if !vm.catches(className, handler.CatchType) {
			continue
		}
		frame.stack = frame.stack[:0]
		frame.stackSlots = 0
		if err := frame.push(ReferenceValue(exception.Object)); err != nil {
			return false, err
		}
		frame.pc = int(handler.HandlerPC)
		return true, nil
	}
	return false, nil
}

func arrayLoad(frame *frame, opcode byte) (Value, error) {
	_, array, index, err := arrayIndex(frame)
	if err != nil {
		return VoidValue(), err
	}
	value, err := array.Load(index)
	if err != nil {
		return VoidValue(), err
	}
	want := []ValueKind{ValueInt, ValueLong, ValueFloat, ValueDouble, ValueReference, ValueInt, ValueInt, ValueInt}[opcode-0x2e]
	if value.kind != want || !arrayOpcodeMatches(array.Component, opcode-0x2e) {
		return VoidValue(), fmt.Errorf("array component is %s, opcode expects %s", value.kind, want)
	}
	return value, nil
}

func (vm *VM) arrayStore(frame *frame, opcode byte) error {
	want := []ValueKind{ValueInt, ValueLong, ValueFloat, ValueDouble, ValueReference, ValueInt, ValueInt, ValueInt}[opcode-0x4f]
	value, err := frame.popKind(want)
	if err != nil {
		return err
	}
	object, array, index, err := arrayIndex(frame)
	if err != nil {
		return err
	}
	if err := validateValue(value, array.Component); err != nil {
		return err
	}
	if !arrayOpcodeMatches(array.Component, opcode-0x4f) {
		return fmt.Errorf("array component %s does not match store opcode 0x%02x", array.Component.Descriptor(), opcode)
	}
	if value.kind == ValueInt {
		integer, _ := value.Int32()
		switch array.Component.Kind {
		case TypeBoolean:
			integer &= 1
		case TypeByte:
			integer = int32(int8(integer))
		case TypeChar:
			integer = int32(uint16(integer))
		case TypeShort:
			integer = int32(int16(integer))
		}
		value = IntValue(integer)
	}
	if err := array.Store(index, value); err != nil {
		return err
	}
	vm.observeStore(StoreEvent{
		Object: object, Index: index, Value: value,
		SiteClass: frame.class.Name, SiteMethod: frame.method.Name, SitePC: frame.pc,
	})
	return nil
}

func arrayOpcodeMatches(component Type, group byte) bool {
	switch group {
	case 0:
		return component.Kind == TypeInt
	case 1:
		return component.Kind == TypeLong
	case 2:
		return component.Kind == TypeFloat
	case 3:
		return component.Kind == TypeDouble
	case 4:
		return component.IsReference()
	case 5:
		return component.Kind == TypeBoolean || component.Kind == TypeByte
	case 6:
		return component.Kind == TypeChar
	case 7:
		return component.Kind == TypeShort
	default:
		return false
	}
}

func duplicateStack(frame *frame, opcode byte) error {
	pop := func() (Value, error) { return frame.pop() }
	pushAll := func(values ...Value) error {
		for _, value := range values {
			if err := frame.push(value); err != nil {
				return err
			}
		}
		return nil
	}

	value1, err := pop()
	if err != nil {
		return err
	}
	switch opcode {
	case 0x5a:
		value2, err := pop()
		if err != nil {
			return err
		}
		if value1.slots() != 1 || value2.slots() != 1 {
			return fmt.Errorf("dup_x1 requires two category 1 values")
		}
		return pushAll(value1, value2, value1)
	case 0x5b:
		if value1.slots() != 1 {
			return fmt.Errorf("dup_x2 top value must be category 1")
		}
		value2, err := pop()
		if err != nil {
			return err
		}
		if value2.slots() == 2 {
			return pushAll(value1, value2, value1)
		}
		value3, err := pop()
		if err != nil {
			return err
		}
		if value2.slots() != 1 || value3.slots() != 1 {
			return fmt.Errorf("dup_x2 has an invalid operand shape")
		}
		return pushAll(value1, value3, value2, value1)
	case 0x5c:
		if value1.slots() == 2 {
			return pushAll(value1, value1)
		}
		value2, err := pop()
		if err != nil {
			return err
		}
		if value1.slots() != 1 || value2.slots() != 1 {
			return fmt.Errorf("dup2 has an invalid operand shape")
		}
		return pushAll(value2, value1, value2, value1)
	case 0x5d:
		if value1.slots() == 2 {
			value2, err := pop()
			if err != nil {
				return err
			}
			if value2.slots() != 1 {
				return fmt.Errorf("dup2_x1 has an invalid operand shape")
			}
			return pushAll(value1, value2, value1)
		}
		value2, err := pop()
		if err != nil {
			return err
		}
		value3, err := pop()
		if err != nil {
			return err
		}
		if value1.slots() != 1 || value2.slots() != 1 || value3.slots() != 1 {
			return fmt.Errorf("dup2_x1 has an invalid operand shape")
		}
		return pushAll(value2, value1, value3, value2, value1)
	case 0x5e:
		if value1.slots() == 2 {
			value2, err := pop()
			if err != nil {
				return err
			}
			if value2.slots() == 2 {
				return pushAll(value1, value2, value1)
			}
			value3, err := pop()
			if err != nil {
				return err
			}
			if value2.slots() != 1 || value3.slots() != 1 {
				return fmt.Errorf("dup2_x2 has an invalid operand shape")
			}
			return pushAll(value1, value3, value2, value1)
		}
		value2, err := pop()
		if err != nil {
			return err
		}
		if value2.slots() != 1 {
			return fmt.Errorf("dup2_x2 has an invalid operand shape")
		}
		value3, err := pop()
		if err != nil {
			return err
		}
		if value3.slots() == 2 {
			return pushAll(value2, value1, value3, value2, value1)
		}
		value4, err := pop()
		if err != nil {
			return err
		}
		if value3.slots() != 1 || value4.slots() != 1 {
			return fmt.Errorf("dup2_x2 has an invalid operand shape")
		}
		return pushAll(value2, value1, value4, value3, value2, value1)
	default:
		return fmt.Errorf("unsupported duplicate opcode 0x%02x", opcode)
	}
}
