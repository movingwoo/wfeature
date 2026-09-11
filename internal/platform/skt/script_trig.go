package skt

import (
	"fmt"
	"math"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func scriptTrigCall(op byte, vm *sgsvm.VM) error {
	if op < 0xa5 || op > 0xaa {
		return fmt.Errorf("unsupported SGS trigonometric operation 0x%02x", op)
	}
	if err := vm.Require(1); err != nil {
		return err
	}
	vm.Push(scriptTrig(op, vm.Pop()))
	return vm.Error()
}

// SGS angles are integer degrees and trigonometric ratios are scaled by 100.
// Evaluating the functions avoids embedding the original runtime's tables.
func scriptTrig(op byte, value int16) int16 {
	if op <= 0xa7 {
		angle := int(value) % 360
		if angle < 0 {
			angle += 360
		}
		radians := float64(angle) * math.Pi / 180
		switch op {
		case 0xa5:
			return int16(math.Round(100 * math.Sin(radians)))
		case 0xa6:
			return int16(math.Round(100 * math.Cos(radians)))
		default:
			angle %= 180
			if angle == 90 {
				return 0
			}
			return int16(math.Round(100 * math.Tan(float64(angle)*math.Pi/180)))
		}
	}
	if value < -100 || value > 100 || op == 0xaa && value == -100 {
		return 32767
	}
	ratio := float64(value) / 100
	var radians float64
	switch op {
	case 0xa8:
		radians = math.Asin(ratio)
	case 0xa9:
		radians = math.Acos(ratio)
	default:
		radians = math.Atan(ratio)
	}
	return int16(math.Round(radians * 180 / math.Pi))
}
