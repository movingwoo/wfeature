package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// Device queries validate the complete destination before changing guest memory.
func scriptDeviceResult(vm *sgsvm.VM, address int16, values []int16) error {
	if address < 0 || int(uint16(address)&0x3fff)+len(values) > 0x4000 {
		return fmt.Errorf("SGS device result crosses a variable bank")
	}
	if err := vm.ChargeWork(len(values) * (1 + len(vm.Variables)/16)); err != nil {
		return err
	}
	for i := range values {
		vm.AddressRead(address + int16(i))
	}
	if err := vm.Error(); err != nil {
		return err
	}
	for i, value := range values {
		vm.AddressWrite(address+int16(i), value)
	}
	return vm.Error()
}
