package jvm

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

type frame struct {
	class      *classfile.Class
	method     *classfile.Member
	methodType MethodDescriptor
	code       *classfile.Code
	pc         int
	locals     []Value
	stack      []Value
	stackSlots int
}

func newFrame(class *classfile.Class, method *classfile.Member, code *classfile.Code, arguments []Value) (*frame, error) {
	methodType, err := ParseMethodDescriptor(method.Descriptor)
	if err != nil {
		return nil, err
	}
	result := &frame{
		class:      class,
		method:     method,
		methodType: methodType,
		code:       code,
		locals:     make([]Value, int(code.MaxLocals)),
		stack:      make([]Value, 0, int(code.MaxStack)),
	}
	localIndex := 0
	for _, argument := range arguments {
		if err := result.storeLocal(localIndex, argument); err != nil {
			return nil, fmt.Errorf("initialize local %d: %w", localIndex, err)
		}
		localIndex += argument.slots()
	}
	return result, nil
}

func (f *frame) push(value Value) error {
	if value.kind == ValueVoid || value.kind == valueTop {
		return fmt.Errorf("cannot push %s", value.kind)
	}
	if f.stackSlots+value.slots() > int(f.code.MaxStack) {
		return fmt.Errorf("operand stack overflow")
	}
	f.stack = append(f.stack, value)
	f.stackSlots += value.slots()
	return nil
}

func (f *frame) pop() (Value, error) {
	if len(f.stack) == 0 {
		return VoidValue(), fmt.Errorf("operand stack underflow")
	}
	index := len(f.stack) - 1
	value := f.stack[index]
	f.stack = f.stack[:index]
	f.stackSlots -= value.slots()
	return value, nil
}

func (f *frame) popKind(kind ValueKind) (Value, error) {
	value, err := f.pop()
	if err != nil {
		return VoidValue(), err
	}
	if value.kind != kind {
		return VoidValue(), fmt.Errorf("expected %s on operand stack, got %s", kind, value.kind)
	}
	return value, nil
}

func (f *frame) loadLocal(index int, kind ValueKind) (Value, error) {
	if index < 0 || index >= len(f.locals) {
		return VoidValue(), fmt.Errorf("local index %d is out of bounds", index)
	}
	value := f.locals[index]
	if value.kind != kind {
		return VoidValue(), fmt.Errorf("local %d is %s, expected %s", index, value.kind, kind)
	}
	return value, nil
}

func (f *frame) storeLocal(index int, value Value) error {
	if value.kind == ValueVoid || value.kind == valueTop {
		return fmt.Errorf("cannot store %s in a local", value.kind)
	}
	if index < 0 || index+value.slots() > len(f.locals) {
		return fmt.Errorf("local index %d is out of bounds", index)
	}
	f.locals[index] = value
	if value.slots() == 2 {
		f.locals[index+1] = Value{kind: valueTop}
	}
	return nil
}

func (f *frame) byte() (byte, error) {
	if f.pc >= len(f.code.Bytecode) {
		return 0, fmt.Errorf("unexpected end of bytecode")
	}
	value := f.code.Bytecode[f.pc]
	f.pc++
	return value, nil
}

func (f *frame) u2() (uint16, error) {
	high, err := f.byte()
	if err != nil {
		return 0, err
	}
	low, err := f.byte()
	if err != nil {
		return 0, err
	}
	return uint16(high)<<8 | uint16(low), nil
}

func (f *frame) i4() (int32, error) {
	a, err := f.byte()
	if err != nil {
		return 0, err
	}
	b, err := f.byte()
	if err != nil {
		return 0, err
	}
	c, err := f.byte()
	if err != nil {
		return 0, err
	}
	d, err := f.byte()
	if err != nil {
		return 0, err
	}
	return int32(uint32(a)<<24 | uint32(b)<<16 | uint32(c)<<8 | uint32(d)), nil
}

func (f *frame) branch(opcodePC int, offset int32) error {
	target := int64(opcodePC) + int64(offset)
	return f.jump(target)
}

func (f *frame) jump(target int64) error {
	if target < 0 || target >= int64(len(f.code.Bytecode)) {
		return fmt.Errorf("branch target %d is outside bytecode", target)
	}
	f.pc = int(target)
	return nil
}
