package skt

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func scriptResourceCall(op byte, vm *sgsvm.VM) error {
	count := 2
	if op == 0x7c || op == 0x83 {
		count = 1
	} else if op == 0x7e {
		count = 4
	}
	if err := vm.Require(count); err != nil {
		return err
	}
	a := vm.Args(count)
	destination := vm.Resource(int(a[0]))
	if err := vm.Error(); err != nil {
		return err
	}
	switch op {
	case 0x7a:
		ok, err := resizeScriptResource(vm, destination, int(a[1]))
		if err != nil {
			return err
		}
		if ok {
			vm.Push(1)
		} else {
			vm.Push(0)
		}
	case 0x7c:
		length, err := scriptResourceStringLength(vm, destination.Data)
		if err != nil {
			return err
		}
		vm.Push(int16(length))
	case 0x7d:
		source := vm.Resource(int(a[1]))
		if err := vm.Error(); err != nil {
			return err
		}
		length, err := scriptResourceStringLength(vm, source.Data)
		if err != nil {
			return err
		}
		ok, err := resizeScriptResource(vm, destination, len(source.Data))
		if err != nil || !ok {
			return err
		}
		copy(destination.Data, source.Data[:length+1])
	case 0x7e:
		source := vm.Resource(int(a[1]))
		if err := vm.Error(); err != nil {
			return err
		}
		offset, length := int(a[2]), int(a[3])
		if offset < 0 || offset > len(source.Data) || length < 0 {
			return fmt.Errorf("SGS substring range out of bounds")
		}
		if err := vm.ChargeWork(length/16 + 1); err != nil {
			return err
		}
		// The original runtime uses strncpy, then writes an explicit terminator.
		// A terminator before the requested length pads the remaining output.
		available := source.Data[offset : offset+min(length, len(source.Data)-offset)]
		copied := len(available)
		if end := bytes.IndexByte(available, 0); end >= 0 {
			copied = end
		} else if copied < length {
			return fmt.Errorf("SGS substring source is truncated")
		}
		// Snapshot before resizing: source and destination may be the same bank.
		data := make([]byte, length+1)
		copy(data, available[:copied])
		ok, err := resizeScriptResource(vm, destination, len(data))
		if err != nil || !ok {
			return err
		}
		copy(destination.Data, data)
	case 0x7f:
		source := vm.Resource(int(a[1]))
		if err := vm.Error(); err != nil {
			return err
		}
		leftLength, err := scriptResourceStringLength(vm, destination.Data)
		if err != nil {
			return err
		}
		rightLength, err := scriptResourceStringLength(vm, source.Data)
		if err != nil {
			return err
		}
		size := leftLength + rightLength + 1
		if size > 65535 {
			return fmt.Errorf("SGS concatenated string exceeds resource size")
		}
		if err := vm.ChargeWork(size/16 + 1); err != nil {
			return err
		}
		// Snapshot the suffix before resizing or overwriting an aliased bank.
		suffix := bytes.Clone(source.Data[:rightLength+1])
		ok, err := resizeScriptResource(vm, destination, size)
		if err != nil || !ok {
			return err
		}
		copy(destination.Data[leftLength:], suffix)
	case 0x80:
		right := vm.Resource(int(a[1]))
		if err := vm.Error(); err != nil {
			return err
		}
		leftLength, err := scriptResourceStringLength(vm, destination.Data)
		if err != nil {
			return err
		}
		rightLength, err := scriptResourceStringLength(vm, right.Data)
		if err != nil {
			return err
		}
		// The original comparison returns -1, 0, or 1 using unsigned bytes.
		vm.Push(int16(bytes.Compare(destination.Data[:leftLength], right.Data[:rightLength])))
	case 0x83:
		length, err := scriptResourceStringLength(vm, destination.Data)
		if err != nil {
			return err
		}
		data := destination.Data[:length]
		position := 0
		for position < len(data) && (data[position] == ' ' || data[position] >= '\t' && data[position] <= '\r') {
			position++
		}
		negative := position < len(data) && data[position] == '-'
		if position < len(data) && (data[position] == '+' || data[position] == '-') {
			position++
		}
		// Decimal conversion stops at the first nondigit. Retaining the low word
		// matches the original 32-bit accumulator followed by a 16-bit store.
		var value uint16
		for position < len(data) && data[position] >= '0' && data[position] <= '9' {
			value = value*10 + uint16(data[position]-'0')
			position++
		}
		if negative {
			value = -value
		}
		vm.Push(int16(value))
	case 0x84:
		if err := vm.ChargeWork(1); err != nil {
			return err
		}
		data := strconv.AppendInt(nil, int64(a[1]), 10)
		data = append(data, 0)
		ok, err := resizeScriptResource(vm, destination, len(data))
		if err != nil || !ok {
			return err
		}
		copy(destination.Data, data)
	default:
		return fmt.Errorf("unsupported SGS resource operation 0x%02x", op)
	}
	return vm.Error()
}

func scriptResourceStringLength(vm *sgsvm.VM, data []byte) (int, error) {
	if err := vm.ChargeWork(len(data)/16 + 1); err != nil {
		return 0, err
	}
	length := bytes.IndexByte(data, 0)
	if length < 0 {
		return 0, fmt.Errorf("SGS resource string is not terminated")
	}
	return length, nil
}

// The original allocator rounds growth to an even delta and retains up to
// sixteen unused bytes when shrinking. Newly exposed bytes are zeroed here.
func resizeScriptResource(vm *sgsvm.VM, resource *sgsvm.Resource, size int) (bool, error) {
	if size < 0 || size > 65535 {
		return false, fmt.Errorf("SGS resource size %d out of bounds", size)
	}
	delta := size - len(resource.Data)
	if delta > 0 && delta%2 != 0 {
		delta++
	}
	if delta >= -16 && delta <= 0 {
		return true, vm.Error()
	}
	size = len(resource.Data) + delta
	if size > 65535 {
		return false, nil
	}
	if err := vm.ChargeWork(size/16 + len(vm.Resources) + 1); err != nil {
		return false, err
	}
	total := size
	for i := range vm.Resources {
		if &vm.Resources[i] != resource {
			total += len(vm.Resources[i].Data)
		}
	}
	if total > 16<<20 {
		return false, nil
	}
	// Embedded banks are instance-owned. The original runtime permits resource
	// writes without testing the descriptor's mutable flag.
	data := make([]byte, size)
	copy(data, resource.Data)
	resource.Data = data
	return true, nil
}
