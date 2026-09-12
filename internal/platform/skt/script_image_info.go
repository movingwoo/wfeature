package skt

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// scriptSISMetadata reads only the header. Success does not imply that the
// object's compressed data or frame composition can be decoded.
func scriptSISMetadata(data []byte) ([5]int16, bool) {
	var result [5]int16
	if len(data) < 4 || !bytes.Equal(data[:3], []byte("SIS")) {
		return result, false
	}
	selector := data[3] >> 3
	if selector >= 1 && selector <= 20 {
		if len(data) < 8 {
			return result, false
		}
		var header uint64
		for _, b := range data[3:8] {
			header = header<<8 | uint64(b)
		}
		width := int16((header>>25)&31) * 8
		height := int16((header>>21)&15) * 8
		objects := (header>>15)&31 + 1
		if width == 0 || height == 0 || objects > 20 {
			return result, false
		}
		return [5]int16{1, 1, int16(selector), width, height}, true
	}
	if selector != 0 && selector != 29 && selector != 30 || len(data) < 18 {
		return result, false
	}
	frames := int(data[9])
	if frames == 0 {
		return result, false
	}
	extra := 0
	if data[12]&0x80 != 0 {
		extra = frames
	}
	if len(data) < 18+extra || data[15+extra] == 8 {
		return result, false
	}
	// The byte header replaces the outer selector's tentative decoder type.
	// Querying an unknown subtype does not establish support for decoding it.
	width, height := int16(data[6]&0xf8), int16(data[7]&0xf8)
	if width == 0 {
		width = 256
	}
	if height == 0 {
		height = 256
	}
	return [5]int16{int16(data[15+extra]), 1, int16(frames), width, height}, true
}

func scriptImageInfoCall(vm *sgsvm.VM) error {
	if err := vm.Require(7); err != nil {
		return err
	}
	a := vm.Args(7)
	resource := vm.Resource(int(a[2]))
	if err := vm.Error(); err != nil {
		return err
	}
	if a[0] == 3 {
		return fmt.Errorf("SGS host image operation is unsupported")
	}
	if a[0] != 1 || a[1] != 1 {
		vm.Push(-1)
		return vm.Error()
	}
	if err := vm.ChargeWork(273); err != nil {
		return err
	}
	if bytes.HasPrefix(resource.Data, []byte("SAF")) {
		return fmt.Errorf("SGS SAF metadata query is unsupported")
	}
	values, ok := scriptSISMetadata(resource.Data)
	if !ok {
		vm.Push(-1)
		return vm.Error()
	}
	address := a[3]
	if address < 0 || int(uint16(address)&0x3fff)+len(values) > 0x4000 {
		return fmt.Errorf("SGS image metadata crosses a variable bank")
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
	vm.Push(0)
	return vm.Error()
}
