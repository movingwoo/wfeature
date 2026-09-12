package skt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptMaximumUsesSignedWordsAndConsumesTwoArguments(t *testing.T) {
	for _, tc := range []struct{ a, b, want int16 }{
		{1, 2, 2}, {2, 1, 2}, {-7, -2, -2}, {-2, -7, -2},
		{-32768, 32767, 32767}, {32767, -32768, 32767},
		{-32768, -32768, -32768}, {0, 0, 0}, {-1, 0, 0},
	} {
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		vm.Push(123)
		vm.Push(tc.a)
		vm.Push(tc.b)
		if err := scriptMathCall(0xad, vm); err != nil {
			t.Fatal(err)
		}
		if got := vm.Pop(); got != tc.want {
			t.Fatalf("max(%d,%d)=%d; want %d", tc.a, tc.b, got, tc.want)
		}
		if got := vm.Pop(); got != 123 {
			t.Fatalf("maximum changed caller stack: %d", got)
		}
	}
}

func TestScriptMaximumUnderflowDoesNotConsumeAvailableArgument(t *testing.T) {
	vm := sgsvm.New(&sgsvm.Program{}, nil)
	vm.Push(123)
	if err := scriptMathCall(0xad, vm); err == nil {
		t.Fatal("missing argument accepted")
	}
	if got := vm.Pop(); got != 123 {
		t.Fatalf("underflow consumed available argument: %d", got)
	}
}

func TestScriptMinimumSearchSignedWordsAndFirstTie(t *testing.T) {
	for _, tc := range []struct {
		values      []int16
		count, want int16
	}{
		{[]int16{3, -7, -7, 1}, 4, 1}, {[]int16{32767, -32768, 0}, 3, 1},
		{[]int16{-32768, 32767}, 2, 0}, {[]int16{9}, 0, 0},
		{[]int16{9}, -32768, 0}, {[]int16{8, 7, 6}, 2, 1},
	} {
		for _, constant := range []bool{false, true} {
			p := &sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: !constant, Values: tc.values}}}
			address := int16(0)
			if constant {
				p.Constants = tc.values
				address = 0x4000
			}
			vm := sgsvm.New(p, nil)
			vm.Push(123)
			vm.Push(address)
			vm.Push(tc.count)
			if err := scriptMathCall(0xb2, vm); err != nil {
				t.Fatal(err)
			}
			if got := vm.Pop(); got != tc.want {
				t.Fatalf("minimum index=%d; want %d", got, tc.want)
			}
			if got := vm.Pop(); got != 123 {
				t.Fatalf("search changed caller stack: %d", got)
			}
		}
	}
}

func TestScriptMinimumSearchRejectsInvalidSpans(t *testing.T) {
	for _, tc := range []struct{ address, count int16 }{
		{-1, 1}, {0, 2}, {0x3fff, 2}, {0x7fff, 2},
	} {
		vm := sgsvm.New(&sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{3}}}}, nil)
		vm.Push(123)
		vm.Push(tc.address)
		vm.Push(tc.count)
		if err := scriptMathCall(0xb2, vm); err == nil {
			t.Fatalf("invalid span %d,%d accepted", tc.address, tc.count)
		}
		if got := vm.Pop(); got != 123 {
			t.Fatalf("failed search pushed result: %d", got)
		}
	}
}

func TestScriptMinimumSearchHonorsWorkLimit(t *testing.T) {
	vm := sgsvm.New(&sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{2, 1}}}}, nil)
	if err := vm.ChargeWork(sgsvm.MaxSteps - 1); err != nil {
		t.Fatal(err)
	}
	vm.Push(123)
	vm.Push(0)
	vm.Push(2)
	if err := scriptMathCall(0xb2, vm); err == nil {
		t.Fatal("minimum search ignored work limit")
	}
	if got := vm.Pop(); got != 123 {
		t.Fatalf("failed search pushed result: %d", got)
	}
}

func TestScriptMaximumThreeSignedArguments(t *testing.T) {
	for _, tc := range []struct{ a, b, c, want int16 }{
		{3, 2, 1, 3}, {1, 3, 2, 3}, {1, 2, 3, 3}, {-3, -1, -2, -1},
		{-32768, -32768, -32768, -32768}, {-32768, 32767, 0, 32767},
		{32767, -32768, 0, 32767}, {0, -32768, 32767, 32767},
	} {
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		vm.Push(123)
		vm.Push(tc.a)
		vm.Push(tc.b)
		vm.Push(tc.c)
		if err := scriptMathCall(0xae, vm); err != nil {
			t.Fatal(err)
		}
		if got := vm.Pop(); got != tc.want {
			t.Fatalf("max3(%d,%d,%d)=%d, want %d", tc.a, tc.b, tc.c, got, tc.want)
		}
		if got := vm.Pop(); got != 123 {
			t.Fatalf("maximum changed caller stack: %d", got)
		}
	}
}

func TestScriptProximityHasExclusiveUnwrappedEndpoints(t *testing.T) {
	for _, tc := range []struct{ center, value, radius, want int16 }{
		{10, 10, 2, 1}, {10, 9, 2, 1}, {10, 11, 2, 1},
		{10, 8, 2, 0}, {10, 12, 2, 0}, {10, 10, 0, 0}, {10, 10, -1, 0},
		{-10, -11, 2, 1}, {-10, -12, 2, 0}, {0, 0, -32768, 0},
		{32767, 32766, 2, 1}, {-32768, -32767, 2, 1},
		{32767, -32768, 32767, 0}, {-32768, 32767, 32767, 0},
	} {
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		vm.Push(123)
		vm.Push(tc.center)
		vm.Push(tc.value)
		vm.Push(tc.radius)
		if err := scriptMathCall(0xb7, vm); err != nil {
			t.Fatal(err)
		}
		if got := vm.Pop(); got != tc.want {
			t.Fatalf("proximity(%d,%d,%d)=%d, want %d", tc.center, tc.value, tc.radius, got, tc.want)
		}
		if got := vm.Pop(); got != 123 {
			t.Fatalf("proximity changed caller stack: %d", got)
		}
	}
}

func TestScriptThreeArgumentMathUnderflowPreservesOperands(t *testing.T) {
	for _, op := range []byte{0xae, 0xb7} {
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		vm.Push(123)
		vm.Push(456)
		if err := scriptMathCall(op, vm); err == nil {
			t.Fatal("missing argument accepted")
		}
		if vm.Pop() != 456 || vm.Pop() != 123 {
			t.Fatal("underflow consumed available operands")
		}
	}
}

func TestScriptIntegerMathServices(t *testing.T) {
	for _, tc := range []struct {
		op   byte
		args []int16
		want int16
	}{
		{0xa4, []int16{-32768}, -1}, {0xa4, []int16{0}, 0}, {0xa4, []int16{32767}, 1},
		{0xab, []int16{32767, 32767}, 32767}, {0xab, []int16{-32768, -32768}, -32768},
		{0xab, []int16{-32768, 32767}, 0}, {0xab, []int16{-2, -1}, -1},
		{0xac, []int16{-32768, -32768, -32768}, -32768}, {0xac, []int16{32767, 32767, 32767}, 32767},
		{0xac, []int16{-3, 0, 1}, 0}, {0xb0, []int16{32767, -32768, 0}, -32768},
		{0xb0, []int16{2, 1, 1}, 1},
	} {
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		vm.Push(123)
		for _, v := range tc.args {
			vm.Push(v)
		}
		if err := (&ScriptSession{}).Call(tc.op, vm); err != nil {
			t.Fatal(err)
		}
		if got := vm.Pop(); got != tc.want {
			t.Fatalf("opcode %02x args %v: got %d want %d", tc.op, tc.args, got, tc.want)
		}
		if vm.Pop() != 123 {
			t.Fatal("caller stack changed")
		}
	}
}

func TestScriptWordSearchVariants(t *testing.T) {
	for _, tc := range []struct {
		op                  byte
		values              []int16
		count, target, want int16
	}{
		{0xb1, []int16{-3, 7, 7}, 3, 0, 1}, {0xb1, []int16{-32768, 32767}, 2, 0, 1},
		{0xb1, []int16{5}, 0, 0, 0}, {0xb3, []int16{-32768, 32767}, 2, 32766, 1},
		{0xb3, []int16{32767, -32768}, 2, -32767, 1}, {0xb3, []int16{2, 4, 4}, 3, 3, 0},
		{0xb3, []int16{9, 2, 2}, 3, 2, 1}, {0xb3, []int16{4}, -1, 7, 0},
		{0xb3, []int16{4, 2}, 3, 2, 1}, // A later exact match stops before the absent third word.
	} {
		for _, constant := range []bool{false, true} {
			p := &sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: !constant, Values: tc.values}}}
			address := int16(0)
			if constant {
				address = 0x4000
				p.Constants = tc.values
			}
			vm := sgsvm.New(p, nil)
			vm.Push(123)
			vm.Push(address)
			vm.Push(tc.count)
			if tc.op == 0xb3 {
				vm.Push(tc.target)
			}
			if err := scriptMathCall(tc.op, vm); err != nil {
				t.Fatal(err)
			}
			if got := vm.Pop(); got != tc.want {
				t.Fatalf("opcode %02x: got %d want %d", tc.op, got, tc.want)
			}
			if vm.Pop() != 123 {
				t.Fatal("caller stack changed")
			}
		}
	}
}

func TestScriptAdditionalMathBounds(t *testing.T) {
	for _, op := range []byte{0xb1, 0xb3} {
		for _, workLimit := range []bool{false, true} {
			vm := sgsvm.New(&sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{1}}}}, nil)
			if workLimit {
				vm.ChargeWork(sgsvm.MaxSteps)
			}
			vm.Push(123)
			vm.Push(0)
			vm.Push(2)
			if op == 0xb3 {
				vm.Push(3)
			}
			if err := scriptMathCall(op, vm); err == nil {
				t.Fatalf("opcode %02x accepted invalid work/span", op)
			}
			if vm.Pop() != 123 {
				t.Fatal("failed search pushed a result")
			}
		}
	}
	for _, tc := range []struct {
		op byte
		n  int
	}{{0xa4, 1}, {0xab, 2}, {0xac, 3}, {0xb0, 3}, {0xb1, 2}, {0xb3, 3}} {
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		for i := 1; i < tc.n; i++ {
			vm.Push(int16(i))
		}
		if err := scriptMathCall(tc.op, vm); err == nil {
			t.Fatalf("opcode %02x accepted underflow", tc.op)
		}
		for i := tc.n - 1; i > 0; i-- {
			if vm.Pop() != int16(i) {
				t.Fatal("underflow consumed operand")
			}
		}
	}
}
