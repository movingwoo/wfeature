package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// Byte memory operations retain the address bank and use little-endian words.
func scriptMemoryCall(op byte, vm *sgsvm.VM) error {
	count := 2
	if op == 0x86 {
		count = 3
	} else if op != 0x85 {
		return fmt.Errorf("unsupported SGS memory service 0x%02x", op)
	}
	if err := vm.Require(count); err != nil {
		return err
	}
	a := vm.Args(count)
	base, offset := int(a[0]), int(a[1])
	if base < 0 || offset < 0 || (base&0x3fff)+offset/2 >= 0x4000 {
		return fmt.Errorf("SGS memory byte offset out of bounds")
	}
	if err := vm.ChargeWork(1 + len(vm.Variables)/16); err != nil {
		return err
	}
	address := int16(base + offset/2)
	word := uint16(vm.AddressRead(address))
	if err := vm.Error(); err != nil {
		return err
	}
	shift := uint(8 * (offset % 2))
	if op == 0x85 {
		vm.Push(int16(byte(word >> shift)))
	} else {
		word = word & ^(uint16(255)<<shift) | uint16(byte(a[2]))<<shift
		vm.AddressWrite(address, int16(word))
	}
	return vm.Error()
}
