// Package sgsvm executes the bytecode in SKT SGS scripts.
package sgsvm

import (
	"context"
	"fmt"
)

const MaxSteps = 1_000_000

// Services supplies platform operations. Arguments and results use the same
// signed 16-bit operand stack as the bytecode instructions.
type Services interface{ Call(byte, *VM) error }

type VM struct {
	constants     []int16
	resourceViews [][]byte
	work          int
	ctx           context.Context
	exited        bool
	Program       *Program
	Variables     []Variable
	Resources     []Resource
	Services      Services
	PC            int
	stack         []int16
	returns       []int
	savedStack    int
	err           error
	ended         bool
}

func New(program *Program, services Services) *VM {
	vm := &VM{Program: program, Services: services, constants: append([]int16(nil), program.Constants...)}
	for _, v := range program.Variables {
		if !v.Mutable && v.Offset >= 0 && v.Offset+len(v.Values) <= len(vm.constants) {
			v.Values = vm.constants[v.Offset : v.Offset+len(v.Values)]
		} else {
			v.Values = append([]int16(nil), v.Values...)
		}
		vm.Variables = append(vm.Variables, v)
	}
	// Immutable descriptors address one contiguous module region. Mutable
	// descriptors copy their initial bytes out of that region into owned banks.
	var blob []byte
	for _, r := range program.Resources {
		blob = append(blob, r.Data...)
	}
	cursor := 0
	for _, r := range program.Resources {
		n := len(r.Data)
		var view []byte
		if r.Mutable {
			r.Data = append([]byte(nil), r.Data...)
		} else {
			view = blob[cursor:]
			r.Data = view[:n:n]
		}
		vm.Resources = append(vm.Resources, r)
		vm.resourceViews = append(vm.resourceViews, view)
		cursor += n
	}
	return vm
}

// Run executes one event. Every invocation is bounded, including loops with
// no platform calls; cancellation cannot leave the host inside guest code.
func (vm *VM) Run(ctx context.Context, entry uint16) error {
	if entry == 0 {
		return nil
	}
	vm.work = 0
	vm.ctx = ctx
	vm.PC = int(entry)
	vm.stack = vm.stack[:0]
	vm.returns = vm.returns[:0]
	vm.savedStack = 0
	vm.ended = false
	vm.err = nil
	for steps := 0; steps < MaxSteps; steps++ {
		if !vm.charge(1) {
			return vm.err
		}
		if steps&255 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		at := vm.PC
		op := vm.byte()
		if vm.err == nil {
			vm.instruction(op)
		}
		if vm.err != nil {
			return fmt.Errorf("SGS opcode 0x%02x at 0x%04x: %w", op, at, vm.err)
		}
		if vm.ended {
			return nil
		}
	}
	return fmt.Errorf("SGS instruction limit %d reached at 0x%04x", MaxSteps, vm.PC)
}

// ChargeWork accounts for bounded work performed inside a platform operation.
func (vm *VM) ChargeWork(n int) error { vm.charge(n); return vm.err }

func (vm *VM) charge(n int) bool {
	if vm.err != nil {
		return false
	}
	if n < 0 || n > MaxSteps-vm.work {
		vm.Fail(fmt.Errorf("SGS execution work limit reached"))
		return false
	}
	vm.work += n
	if vm.ctx != nil {
		vm.Fail(vm.ctx.Err())
	}
	return vm.err == nil
}

func (vm *VM) Error() error { return vm.err }
func (vm *VM) Exited() bool { return vm.exited }

func (vm *VM) Fail(err error) {
	if vm.err == nil {
		vm.err = err
	}
}
func (vm *VM) Pop() int16 {
	if len(vm.stack) == 0 {
		vm.Fail(fmt.Errorf("operand stack underflow"))
		return 0
	}
	v := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return v
}
func (vm *VM) Push(v int16) {
	if len(vm.stack) >= 65 {
		vm.Fail(fmt.Errorf("operand stack overflow"))
		return
	}
	vm.stack = append(vm.stack, v)
}

// Args removes arguments in call order, with the last argument on top.
func (vm *VM) Require(n int) error {
	if n < 0 || len(vm.stack) < n {
		vm.Fail(fmt.Errorf("operand stack underflow"))
	}
	return vm.err
}

func (vm *VM) Args(n int) []int16 {
	values := make([]int16, n)
	for i := n - 1; i >= 0; i-- {
		values[i] = vm.Pop()
	}
	return values
}
func (vm *VM) byte() byte {
	end := vm.Program.CodeEnd
	if end == 0 {
		end = len(vm.Program.Data)
	}
	if vm.PC < vm.Program.CodeStart || vm.PC >= end {
		vm.Fail(fmt.Errorf("instruction address out of bounds"))
		return 0
	}
	v := vm.Program.Data[vm.PC]
	vm.PC++
	return v
}
func (vm *VM) word() int16 {
	hi := vm.byte()
	lo := vm.byte()
	return int16(uint16(hi)<<8 | uint16(lo))
}
func (vm *VM) Value(variable, index int) int16 {
	if variable < 0 || variable >= len(vm.Variables) || index < 0 || index >= len(vm.Variables[variable].Values) {
		vm.Fail(fmt.Errorf("variable %d index %d out of bounds", variable, index))
		return 0
	}
	return vm.Variables[variable].Values[index]
}
func (vm *VM) Set(variable, index int, v int16) {
	if vm.err != nil {
		return
	}
	if variable < 0 || variable >= len(vm.Variables) || index < 0 || index >= len(vm.Variables[variable].Values) {
		vm.Fail(fmt.Errorf("variable %d index %d out of bounds", variable, index))
		return
	}
	// The original runtime bounds writes to its variable banks, including
	// initialized constants. Preserve that behavior inside instance-owned data.
	vm.Variables[variable].Values[index] = v
}
func (vm *VM) Resource(index int) *Resource {
	if index < 0 || index >= len(vm.Resources) {
		vm.Fail(fmt.Errorf("resource %d out of bounds", index))
		return &Resource{}
	}
	return &vm.Resources[index]
}
func truth(v bool) int16 {
	if v {
		return 1
	}
	return 0
}

func (vm *VM) instruction(op byte) {
	switch {
	case op == 0 || op == 0x47:
	case op >= 1 && op <= 4 || op >= 7 && op <= 10:
		mode := op
		if mode >= 7 {
			mode -= 6
		}
		variable := int(vm.byte())
		index := 0
		switch mode {
		case 1:
			indVar, indIndex := int(vm.byte()), int(vm.byte())
			index = int(vm.Value(indVar, indIndex))
		case 2:
			index = int(vm.Value(int(vm.byte()), 0))
		case 3:
			index = int(vm.byte())
		}
		if op <= 4 {
			vm.Push(vm.Value(variable, index))
		} else {
			v := vm.Pop()
			vm.Set(variable, index, v)
		}
	case op == 5:
		vm.Push(int16(int8(vm.byte())))
	case op == 6:
		vm.Push(vm.word())
	case op == 0x0b:
		vm.savedStack = len(vm.stack)
	case op == 0x0c:
		if vm.savedStack > cap(vm.stack) {
			vm.Fail(fmt.Errorf("saved stack out of bounds"))
		} else {
			vm.stack = vm.stack[:vm.savedStack]
		}
	case op == 0x0d:
		vm.Push(vm.Pop() + 1)
	case op == 0x0e:
		vm.Push(vm.Pop() - 1)
	case op == 0x0f:
		v := vm.Pop()
		vm.Push(v)
		vm.Push(v)
	case op == 0x10:
		vm.Push(-vm.Pop())
	case op == 0x11:
		b, a := vm.Pop(), vm.Pop()
		vm.Push(b)
		vm.Push(a)
	case op == 0x19:
		vm.Push(truth(vm.Pop() == 0))
	case op >= 0x12 && op <= 0x22:
		b, a := vm.Pop(), vm.Pop()
		var v int16
		switch op {
		case 0x12:
			v = a + b
		case 0x13:
			v = a - b
		case 0x14:
			v = a * b
		case 0x15, 0x16:
			if b == 0 {
				vm.Fail(fmt.Errorf("division by zero"))
				return
			}
			if op == 0x15 {
				v = int16(int32(a) / int32(b))
			} else {
				v = int16(int32(a) % int32(b))
			}
		case 0x17:
			v = a & b
		case 0x18:
			v = a | b
		case 0x1a:
			v = a ^ b
		case 0x1b:
			v = a >> uint16(b&31)
		case 0x1c:
			v = a << uint16(b&31)
		case 0x1d:
			v = truth(a > b)
		case 0x1e:
			v = truth(a < b)
		case 0x1f:
			v = truth(a >= b)
		case 0x20:
			v = truth(a <= b)
		case 0x21:
			v = truth(a == b)
		case 0x22:
			v = truth(a != b)
		}
		vm.Push(v)
	case op >= 0x23 && op <= 0x54:
		vm.control(op)
	case op >= 0xb4 && op <= 0xb6:
		vm.vector(op)
	case op == 0xff:
		vm.ended = true
	default:
		if vm.Services == nil {
			vm.Fail(fmt.Errorf("unsupported platform operation"))
		} else {
			vm.Fail(vm.Services.Call(op, vm))
		}
	}
}
