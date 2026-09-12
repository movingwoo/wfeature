package skt

import (
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// Resource transfers use a word address followed by a byte offset, resource ID
// and byte count. Both directions validate and snapshot before any mutation.
func scriptCopyCall(op byte, vm *sgsvm.VM) error {
	if op != 0x87 && op != 0x88 {
		return fmt.Errorf("unsupported SGS transfer 0x%02x", op)
	}
	if err := vm.Require(4); err != nil {
		return err
	}
	a := vm.Args(4)
	address, offset, count := int(a[0]), int(a[1]), int(a[3])
	if address < 0 || offset < 0 || count < 0 || (address&0x3fff)*2+offset+count > 0x8000 {
		return fmt.Errorf("SGS resource transfer crosses a variable bank")
	}
	resource := vm.Resource(int(a[2]))
	if err := vm.Error(); err != nil {
		return err
	}
	first, last := offset/2, (offset+count+1)/2
	if count == 0 {
		first, last = 0, 1
	} // Validate the original base pointer without writing.
	if err := vm.ChargeWork((last-first)*(1+len(vm.Variables)/16) + count + 1); err != nil {
		return err
	}
	words := make([]byte, (last-first)*2)
	for i := first; i < last; i++ {
		binary.LittleEndian.PutUint16(words[(i-first)*2:], uint16(vm.AddressRead(int16(address+i))))
	}
	if err := vm.Error(); err != nil {
		return err
	}
	if op == 0x87 {
		payload := words[offset%2 : offset%2+count]
		if count == 0 {
			payload = nil
		}
		ok, err := resizeScriptResource(vm, resource, count)
		if err != nil || !ok {
			return err
		}
		copy(resource.Data, payload)
	} else {
		payload := make([]byte, count)
		for i := range payload {
			payload[i] = vm.ResourceByte(int(a[2]), i)
		}
		if err := vm.Error(); err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		copy(words[offset%2:], payload)
		for i := first; i < last; i++ {
			vm.AddressWrite(int16(address+i), int16(binary.LittleEndian.Uint16(words[(i-first)*2:])))
		}
	}
	return vm.Error()
}
