package skt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptTrigQuadrantsAndScale(t *testing.T) {
	for _, tc := range []struct {
		angle                 int16
		sine, cosine, tangent int16
	}{
		{0, 0, 100, 0}, {30, 50, 87, 58}, {45, 71, 71, 100},
		{60, 87, 50, 173}, {89, 100, 2, 5729}, {90, 100, 0, 0},
		{91, 100, -2, -5729}, {135, 71, -71, -100}, {180, 0, -100, 0},
		{225, -71, -71, 100}, {270, -100, 0, 0}, {315, -71, 71, -100},
		{-45, -71, 71, -100}, {-360, 0, 100, 0},
	} {
		for i, want := range []int16{tc.sine, tc.cosine, tc.tangent} {
			op := byte(0xa5 + i)
			if got := scriptTrig(op, tc.angle); got != want {
				t.Fatalf("opcode %02x angle %d: got %d want %d", op, tc.angle, got, want)
			}
		}
	}
}

func TestScriptTrigFullWordPeriodicityAndSymmetry(t *testing.T) {
	for value := -32768; value <= 32767; value++ {
		for op := byte(0xa5); op <= 0xa7; op++ {
			period, bound := 360, int16(100)
			if op == 0xa7 {
				period, bound = 180, 5729
			}
			got := scriptTrig(op, int16(value))
			if got < -bound || got > bound {
				t.Fatalf("opcode %02x angle %d escaped range: %d", op, value, got)
			}
			if value+period <= 32767 && got != scriptTrig(op, int16(value+period)) {
				t.Fatalf("opcode %02x is not periodic at %d", op, value)
			}
			if value != -32768 {
				want := -got
				if op == 0xa6 {
					want = got
				}
				if opposite := scriptTrig(op, int16(-value)); opposite != want {
					t.Fatalf("opcode %02x symmetry at %d: got %d want %d", op, value, opposite, want)
				}
			}
		}
	}
}

func TestScriptInverseTrigFullWordDomain(t *testing.T) {
	for op := byte(0xa8); op <= 0xaa; op++ {
		previous := int16(-32768)
		if op == 0xa9 {
			previous = 180
		}
		for value := -32768; value <= 32767; value++ {
			got := scriptTrig(op, int16(value))
			valid := value >= -100 && value <= 100 && !(op == 0xaa && value == -100)
			if !valid {
				if got != 32767 {
					t.Fatalf("opcode %02x accepted %d: %d", op, value, got)
				}
				continue
			}
			if op == 0xa9 {
				if got < 0 || got > previous {
					t.Fatalf("acos is not decreasing at %d", value)
				}
			} else if got < previous || got < -90 || got > 90 {
				t.Fatalf("opcode %02x is not bounded and increasing at %d", op, value)
			}
			previous = got
			// Returning through the forward function retains the original ratio
			// within the precision lost by whole-degree inverse rounding.
			back := int(scriptTrig(op-3, got))
			if back < value-2 || back > value+2 {
				t.Fatalf("opcode %02x round trip %d -> %d -> %d", op, value, got, back)
			}
		}
	}
	for _, tc := range []struct {
		op          byte
		value, want int16
	}{
		{0xa8, -100, -90}, {0xa8, 0, 0}, {0xa8, 100, 90},
		{0xa9, -100, 180}, {0xa9, 0, 90}, {0xa9, 100, 0},
		{0xaa, -100, 32767}, {0xaa, -99, -45}, {0xaa, 0, 0}, {0xaa, 100, 45},
	} {
		if got := scriptTrig(tc.op, tc.value); got != tc.want {
			t.Fatalf("endpoint %02x %d: got %d want %d", tc.op, tc.value, got, tc.want)
		}
	}
}

func TestScriptTrigStackContract(t *testing.T) {
	vm := sgsvm.New(&sgsvm.Program{}, nil)
	if err := scriptTrigCall(0xa5, vm); err == nil {
		t.Fatal("accepted missing operand")
	}
	vm = sgsvm.New(&sgsvm.Program{}, nil)
	vm.Push(1234)
	vm.Push(30)
	if err := scriptTrigCall(0xa5, vm); err != nil {
		t.Fatal(err)
	}
	if vm.Pop() != 50 || vm.Pop() != 1234 {
		t.Fatal("service changed surrounding stack")
	}
}
