package sgsvm

import "fmt"

// location consumes one of the four variable addressing forms.
func (vm *VM) location(mode byte) (int, int) {
	variable, index := int(vm.byte()), 0
	switch mode {
	case 0:
		other, element := int(vm.byte()), int(vm.byte())
		index = int(vm.Value(other, element))
	case 1:
		index = int(vm.Value(int(vm.byte()), 0))
	case 2:
		index = int(vm.byte())
	}
	return variable, index
}

func (vm *VM) jump(target int) {
	if vm.err != nil {
		return
	}
	end := vm.Program.CodeEnd
	if end == 0 {
		end = len(vm.Program.Data)
	}
	if target < vm.Program.CodeStart || target >= end {
		vm.Fail(fmt.Errorf("branch target 0x%04x out of bounds", target))
		return
	}
	vm.PC = target
}

func (vm *VM) control(op byte) {
	switch {
	case op >= 0x23 && op <= 0x36:
		destination, source := (op-0x23)/5, (op-0x23)%5
		variable, index := vm.location(destination)
		var value int16
		if source == 4 {
			value = int16(int8(vm.byte()))
		} else {
			other, element := vm.location(source)
			value = vm.Value(other, element)
		}
		if vm.err == nil {
			vm.Set(variable, index, value)
		}
	case op >= 0x37 && op <= 0x3a:
		variable, index := vm.location(op - 0x37)
		delta := int16(int8(vm.byte()))
		value := vm.Value(variable, index)
		if vm.err == nil {
			vm.Set(variable, index, value+delta)
		}
	case op >= 0x3b && op <= 0x40:
		value, target := int16(int8(vm.byte())), int(uint16(vm.word()))
		left := vm.Pop()
		conditions := [...]bool{left > value, left < value, left >= value, left <= value, left == value, left != value}
		if conditions[op-0x3b] {
			vm.jump(target)
		}
	case op >= 0x41 && op <= 0x44:
		target := int(uint16(vm.word()))
		take := true
		switch op {
		case 0x42:
			take = vm.Pop() != 0
		case 0x43:
			take = vm.Pop() == 0
		case 0x44:
			if len(vm.returns) >= 17 {
				vm.Fail(fmt.Errorf("return stack overflow"))
				return
			}
			if vm.err == nil {
				vm.returns = append(vm.returns, vm.PC)
			}
		}
		if take {
			vm.jump(target)
		}
	case op == 0x45:
		if len(vm.returns) == 0 {
			vm.ended = true
			return
		}
		last := len(vm.returns) - 1
		target := vm.returns[last]
		vm.returns = vm.returns[:last]
		vm.jump(target)
	case op == 0x46:
		vm.ended = true
		vm.exited = true
	case op == 0x47:
	case op >= 0x48 && op <= 0x4b:
		variable, index := vm.location(op - 0x48)
		if len(vm.stack) == 0 {
			vm.Fail(fmt.Errorf("operand stack underflow"))
			return
		}
		if vm.err == nil {
			vm.Set(variable, index, vm.stack[len(vm.stack)-1])
		}
	case op == 0x4c || op == 0x4d:
		mode := byte(2)
		if op == 0x4d {
			mode = 3
		}
		variable, index := vm.location(mode)
		vm.Value(variable, index)
		if vm.err == nil {
			vm.Push(vm.address(variable, index))
		}
	case op == 0x4e:
		address := vm.Pop()
		value := vm.AddressRead(address)
		if vm.err == nil {
			vm.Push(value)
		}
	case op == 0x4f || op == 0x50:
		value, address := vm.Pop(), vm.Pop()
		// These stores address the unflagged bank directly.
		if uint16(address)&0x4000 != 0 {
			vm.Fail(fmt.Errorf("invalid writable address 0x%04x", uint16(address)))
			return
		}
		if vm.err == nil {
			vm.AddressWrite(address, value)
		}
		if op == 0x50 && vm.err == nil {
			vm.Push(value)
		}
	default:
		if vm.Services == nil {
			vm.Fail(fmt.Errorf("unsupported platform operation"))
		} else {
			vm.Fail(vm.Services.Call(op, vm))
		}
	}
}

func (vm *VM) address(variable, index int) int16 {
	v := vm.Variables[variable]
	offset := v.Offset + index
	if offset < 0 || offset >= 0x4000 {
		vm.Fail(fmt.Errorf("variable address out of bounds"))
		return 0
	}
	if !v.Mutable {
		offset |= 0x4000
	}
	return int16(offset)
}

func (vm *VM) addressLocation(address int16) (int, int) {
	if address < 0 {
		vm.Fail(fmt.Errorf("negative variable address"))
		return 0, 0
	}
	mutable := uint16(address)&0x4000 == 0
	offset := int(uint16(address) & 0x3fff)
	for i, v := range vm.Variables {
		if v.Mutable == mutable && offset >= v.Offset && offset-v.Offset < len(v.Values) {
			return i, offset - v.Offset
		}
	}
	vm.Fail(fmt.Errorf("variable address 0x%04x out of bounds", uint16(address)))
	return 0, 0
}

// AddressRead reads an encoded word address supplied by guest bytecode.
func (vm *VM) AddressRead(address int16) int16 {
	if address >= 0 && uint16(address)&0x4000 != 0 && len(vm.constants) > 0 {
		offset := int(uint16(address) & 0x3fff)
		if offset >= len(vm.constants) {
			vm.Fail(fmt.Errorf("constant address out of bounds"))
			return 0
		}
		return vm.constants[offset]
	}
	variable, index := vm.addressLocation(address)
	if vm.err != nil {
		return 0
	}
	return vm.Value(variable, index)
}

// AddressWrite writes an encoded word address supplied by guest bytecode.
func (vm *VM) AddressWrite(address int16, value int16) {
	if vm.err != nil {
		return
	}
	if address >= 0 && uint16(address)&0x4000 != 0 && len(vm.constants) > 0 {
		offset := int(uint16(address) & 0x3fff)
		if offset >= len(vm.constants) {
			vm.Fail(fmt.Errorf("constant address out of bounds"))
			return
		}
		vm.constants[offset] = value
		return
	}
	variable, index := vm.addressLocation(address)
	if vm.err == nil {
		vm.Set(variable, index, value)
	}
}
