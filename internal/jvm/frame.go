package jvm

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

type frame struct {
	class      *classfile.Class
	method     *classfile.Member
	returnType Type
	code       *classfile.Code
	pc         int
	locals     []Value
	stack      []Value
	stackSlots int
}

// newFrame fills a frame for one method body. The frame comes from the calling
// execution rather than from the heap: a frame lives exactly as long as the
// `execute` that made it, calls nest, and nothing keeps one afterwards — an
// `ExecutionError` copies the four fields it names — so the execution can hand
// the same one back on the way out and lend it to the next call.
//
// **It was three allocations a method body and they were three of the four a
// guest call cost**: the frame, its locals and its operand stack. An execution
// belongs to one guest thread, so the free list needs no lock.
func newFrame(state *execution, class *classfile.Class, method *classfile.Member, code *classfile.Code, arguments []Value) (*frame, error) {
	returnType, err := ReturnTypeOf(method.Descriptor)
	if err != nil {
		return nil, err
	}
	result := state.takeFrame()
	result.class = class
	result.method = method
	result.returnType = returnType
	result.code = code
	result.pc = 0
	result.stackSlots = 0
	// Both slices arrive zeroed — a fresh frame's are nil and a returned one
	// was emptied on the way back — so this only has to size them.
	if locals := int(code.MaxLocals); cap(result.locals) < locals {
		result.locals = make([]Value, locals)
	} else {
		result.locals = result.locals[:locals]
	}
	if cap(result.stack) < int(code.MaxStack) {
		result.stack = make([]Value, 0, int(code.MaxStack))
	} else {
		result.stack = result.stack[:0]
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

// framePoolLimit bounds what an execution keeps. A run's list is as long as the
// deepest call it made, and past this a deep recursion unwinds through the heap
// rather than making the list a place frames go to be forgotten in.
const framePoolLimit = 64

func (state *execution) takeFrame() *frame {
	if last := len(state.framePool) - 1; last >= 0 {
		result := state.framePool[last]
		state.framePool[last] = nil
		state.framePool = state.framePool[:last]
		return result
	}
	return &frame{}
}

// releaseFrame takes a finished frame back, emptied.
//
// **Emptied to its capacity, not to its length.** Two things turn on that. A
// value left behind is a reference the list would keep alive until something
// happened to overwrite it, which is a leak with no bound in sight; and the
// next body to borrow this frame may want more locals than this one did, so
// anything past the length it used would arrive as an initialized local that
// nobody wrote. Clearing here rather than on the way out means a frame is
// cleared once per use either way.
func (state *execution) releaseFrame(result *frame) {
	if len(state.framePool) >= framePoolLimit {
		return
	}
	clear(result.locals[:cap(result.locals)])
	clear(result.stack[:cap(result.stack)])
	result.class = nil
	result.method = nil
	result.code = nil
	state.framePool = append(state.framePool, result)
}

// takeValues lends the slice an invoke pops its arguments into, which was the
// last allocation a guest call made.
//
// It can be lent because nothing keeps it: a bytecode callee's frame copies it
// into locals before the first instruction runs, and a native is handed a copy
// of its own — the static path always did that, and the instance path does it
// now too, which makes what a native may do with its arguments the same
// question on both. The caller returns it after the call rather than before,
// so a nested call takes a different one and the list is as long as the
// deepest call the run made.
func (state *execution) takeValues(count int) []Value {
	if last := len(state.valuePool) - 1; last >= 0 {
		result := state.valuePool[last]
		state.valuePool[last] = nil
		state.valuePool = state.valuePool[:last]
		if cap(result) >= count {
			return result[:count]
		}
	}
	// A miss allocates with room to spare, because the list is shared by every
	// call shape a title makes: sized to the call that missed, a two-argument
	// call would keep handing back a slice the next three-argument one cannot
	// use, and the list would allocate for ever without ever growing.
	return make([]Value, count, max(count, valuePoolFloor))
}

// valuePoolFloor is the smallest slice worth keeping. Eight covers a receiver
// and seven arguments, which is more than a guest method in this corpus takes.
const valuePoolFloor = 8

// releaseValues takes one back, emptied for the reason releaseFrame empties a
// frame: what is left in it is a reference the list would hold.
func (state *execution) releaseValues(values []Value) {
	if len(state.valuePool) >= framePoolLimit {
		return
	}
	clear(values[:cap(values)])
	state.valuePool = append(state.valuePool, values)
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
