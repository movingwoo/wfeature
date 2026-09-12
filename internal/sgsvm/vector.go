package sgsvm

import "fmt"

// vector applies an operation in forward element order, preserving overlap
// behavior when the destination and source ranges share a variable bank.
func (vm *VM) vector(op byte) {
	n := 4
	if op == 0xb6 {
		n = 5
	}
	args := vm.Args(n)
	if vm.err != nil {
		return
	}
	count, operation := int(args[n-2]), args[n-1]
	if operation < 0 || operation > 11 {
		vm.Fail(fmt.Errorf("invalid vector operation %d", operation))
		return
	}
	// The scalar helper rejects a zero divisor before testing the element count.
	if op == 0xb4 && (operation == 4 || operation == 5) && args[1] == 0 {
		vm.Fail(fmt.Errorf("division by zero"))
		return
	}
	if count <= 0 {
		return
	}
	// Validate every used range before modifying memory. Arithmetic faults can
	// still occur at the element that encounters them, as in scalar execution.
	refs := []int16{args[0]}
	if op != 0xb4 {
		refs = append(refs, args[1])
	}
	if op == 0xb6 && operation != 0 && operation != 8 {
		refs = append(refs, args[2])
	}
	for _, ref := range refs {
		if ref < 0 || int(uint16(ref)&0x3fff)+count > 0x4000 {
			vm.Fail(fmt.Errorf("vector range out of bounds"))
			return
		}
		for i := 0; i < count; i++ {
			if !vm.charge(1) {
				return
			}
			vm.AddressRead(ref + int16(i))
			if vm.err != nil {
				return
			}
		}
	}
	if !vm.charge(count) {
		return
	}
	for i := 0; i < count; i++ {
		address := args[0] + int16(i)
		left := vm.AddressRead(address)
		right := args[1]
		if op == 0xb5 {
			right = vm.AddressRead(args[1] + int16(i))
		}
		if op == 0xb6 {
			left = vm.AddressRead(args[1] + int16(i))
			if operation == 0 || operation == 8 {
				right = left
			} else {
				right = vm.AddressRead(args[2] + int16(i))
			}
		}
		if vm.err != nil {
			return
		}
		var value int16
		switch operation {
		case 0:
			value = right
		case 1:
			value = left + right
		case 2:
			value = left - right
		case 3:
			value = left * right
		case 4, 5:
			if right == 0 {
				vm.Fail(fmt.Errorf("division by zero"))
				return
			}
			if operation == 4 {
				value = int16(int32(left) / int32(right))
			} else {
				value = int16(int32(left) % int32(right))
			}
		case 6:
			value = left & right
		case 7:
			value = left | right
		case 8:
			value = ^right
		case 9:
			value = left ^ right
		case 10:
			value = left >> uint16(right&31)
		case 11:
			value = left << uint16(right&31)
		}
		vm.AddressWrite(address, value)
		if vm.err != nil {
			return
		}
	}
}
