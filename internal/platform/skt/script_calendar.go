package skt

import (
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// Calendar queries use local wall time. Timer services retain their separate
// virtual clock. Weekdays follow the original Sunday-zero convention.
func scriptCalendarCall(op byte, vm *sgsvm.VM, now time.Time) error {
	if err := vm.Require(1); err != nil {
		return err
	}
	address := vm.Pop()
	if address < 0 || int(uint16(address)&0x3fff)+4 > 0x4000 {
		return fmt.Errorf("SGS calendar result crosses a variable bank")
	}
	if err := vm.ChargeWork(4 * (1 + len(vm.Variables)/16)); err != nil {
		return err
	}
	for i := int16(0); i < 4; i++ {
		vm.AddressRead(address + i)
	}
	if err := vm.Error(); err != nil {
		return err
	}
	var values [4]int16
	switch op {
	case 0xb8:
		values = [4]int16{int16(now.Year()), int16(now.Month()), int16(now.Day()), int16(now.Weekday())}
	case 0xb9:
		values = [4]int16{int16(now.Hour()), int16(now.Minute()), int16(now.Second()), int16(now.Nanosecond() / 1_000_000)}
	default:
		return fmt.Errorf("unsupported SGS calendar service 0x%02x", op)
	}
	for i, v := range values {
		vm.AddressWrite(address+int16(i), v)
	}
	return vm.Error()
}
