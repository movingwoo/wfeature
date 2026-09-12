package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func scriptMathCall(op byte, vm *sgsvm.VM) error {
	switch op {
	case 0xa4:
		if err := vm.Require(1); err != nil {
			return err
		}
		value, result := vm.Pop(), int16(0)
		if value < 0 {
			result = -1
		} else if value > 0 {
			result = 1
		}
		vm.Push(result)
	case 0xab, 0xac:
		count := 2 + int(op-0xab)
		if err := vm.Require(count); err != nil {
			return err
		}
		var sum int32
		for _, value := range vm.Args(count) {
			sum += int32(value)
		}
		vm.Push(int16(sum / int32(count)))
	case 0xad:
		if err := vm.Require(2); err != nil {
			return err
		}
		a := vm.Args(2)
		vm.Push(max(a[0], a[1]))
	case 0xae:
		if err := vm.Require(3); err != nil {
			return err
		}
		a := vm.Args(3)
		vm.Push(max(a[0], a[1], a[2]))
	case 0xb0:
		if err := vm.Require(3); err != nil {
			return err
		}
		a := vm.Args(3)
		vm.Push(min(a[0], a[1], a[2]))
	case 0xb7:
		if err := vm.Require(3); err != nil {
			return err
		}
		a := vm.Args(3)
		// Endpoints are excluded; widening before arithmetic preserves the
		// original signed 32-bit comparisons at either word boundary.
		center, value, radius := int32(a[0]), int32(a[1]), int32(a[2])
		result := int16(0)
		if center-radius < value && value < center+radius {
			result = 1
		}
		vm.Push(result)
	case 0xb1, 0xb2, 0xb3:
		arguments := 2
		if op == 0xb3 {
			arguments = 3
		}
		if err := vm.Require(arguments); err != nil {
			return err
		}
		a := vm.Args(arguments)
		// The native service reads the first word even when count is zero
		// or negative, and retains the earliest index when minima are tied.
		count := max(1, int(a[1]))
		if a[0] < 0 || int(uint16(a[0])&0x3fff)+count > 0x4000 {
			return fmt.Errorf("SGS word search crosses a variable bank")
		}
		var best, index int16
		var bestDistance int32
		for i := 0; i < count; i++ {
			if err := vm.ChargeWork(1 + len(vm.Variables)/16); err != nil {
				return err
			}
			value := vm.AddressRead(a[0] + int16(i))
			if err := vm.Error(); err != nil {
				return err
			}
			better := i == 0 || op == 0xb1 && value > best || op == 0xb2 && value < best
			var distance int32
			if op == 0xb3 {
				distance = int32(value) - int32(a[2])
				if distance < 0 {
					distance = -distance
				}
				better = i == 0 || distance < bestDistance
			}
			if better {
				best, index, bestDistance = value, int16(i), distance
				// The original nearest search stops on a later exact match.
				if op == 0xb3 && i > 0 && distance == 0 {
					break
				}
			}
		}
		vm.Push(index)
	default:
		return fmt.Errorf("unsupported SGS math operation 0x%02x", op)
	}
	return vm.Error()
}
