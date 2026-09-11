package sgsvm

import (
	"context"
	"slices"
	"testing"
)

func TestVectorInstructionPreservesAliasedEvaluationOrder(t *testing.T) {
	for _, tc := range []struct {
		args []int16
		want []int16
	}{
		{[]int16{1, 0, 3, 3, 1}, []int16{2, 7, 13, 20, 6, 7}},
		{[]int16{1, 3, 0, 3, 2}, []int16{2, 3, 3, 4, 6, 7}},
		{[]int16{1, 1, 1, 3, 1}, []int16{2, 6, 8, 10, 6, 7}},
		{[]int16{1, 0, 3, 3, 8}, []int16{2, -3, 2, -3, 6, 7}},
	} {
		for _, constant := range []bool{false, true} {
			values := []int16{2, 3, 4, 5, 6, 7}
			p := &Program{Data: []byte{0, 5, 71}, Variables: []Variable{{Mutable: !constant, Values: values}}}
			args := slices.Clone(tc.args)
			if constant {
				p.Constants = values
				for i := 0; i < 3; i++ {
					args[i] += 0x4000
				}
			}
			for _, value := range args {
				p.Data = append(p.Data, 6, byte(uint16(value)>>8), byte(value))
			}
			p.Data = append(p.Data, 0xb6, 0xff)
			vm := New(p, nil)
			if err := vm.Run(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(vm.Variables[0].Values, tc.want) || vm.Pop() != 71 {
				t.Fatalf("arguments %v: got %v, want %v", args, vm.Variables[0].Values, tc.want)
			}
		}
	}
}

func vectorVM() *VM {
	return New(&Program{Variables: []Variable{
		{Mutable: true, Offset: 0, Values: []int16{12, 12, 12}},
		{Mutable: true, Offset: 3, Values: []int16{3, 3, 3}},
		{Mutable: false, Offset: 0, Values: []int16{12, 12, 12}},
	}}, nil)
}

func TestVectorOperations(t *testing.T) {
	want := []int16{3, 15, 9, 36, 4, 0, 0, 15, -4, 15, 1, 96}
	for _, op := range []byte{0xb4, 0xb5, 0xb6} {
		for operation := 0; operation < 12; operation++ {
			vm := vectorVM()
			args := []int16{0, 3, 3, int16(operation)}
			expected := want[operation]
			if op == 0xb6 {
				args = []int16{0, 0x4000, 3, 3, int16(operation)}
				if operation == 0 {
					expected = 12
				}
				if operation == 8 {
					expected = -13
				}
			}
			vm.Push(71)
			for _, arg := range args {
				vm.Push(arg)
			}
			vm.vector(op)
			if vm.err != nil {
				t.Fatalf("opcode%x operator%d: %v", op, operation, vm.err)
			}
			for _, v := range vm.Variables[0].Values {
				if v != expected {
					t.Fatalf("opcode%x operator%d: got%d want%d", op, operation, v, expected)
				}
			}
			if len(vm.stack) != 1 || vm.stack[0] != 71 {
				t.Fatalf("stack=%v", vm.stack)
			}
		}
	}
}

func TestVectorOverlapIsForward(t *testing.T) {
	vm := vectorVM()
	vm.Variables[0].Values = []int16{7, 8, 9}
	for _, v := range []int16{1, 0, 2, 0} {
		vm.Push(v)
	}
	vm.vector(0xb5)
	if vm.err != nil {
		t.Fatal(vm.err)
	}
	if got := vm.Variables[0].Values; got[0] != 7 || got[1] != 7 || got[2] != 7 {
		t.Fatalf("got%v", got)
	}
}

func TestVectorRejectsInvalidRangesAndOperators(t *testing.T) {
	cases := [][]int16{
		{0, 3, 3, 12},
		{0, 3, 3, -1},
		{0, 3, 32767, 0},
		{0x3fff, 3, 2, 0},
		{-1, 3, 1, 0},
		{0, 32767, 3, 0},
		{0, 3},
	}
	for _, args := range cases {
		vm := vectorVM()
		for _, v := range args {
			vm.Push(v)
		}
		vm.vector(0xb5)
		if vm.err == nil {
			t.Fatalf("accepted %v", args)
		}
		if vm.Variables[0].Values[0] != 12 {
			t.Fatalf("invalid range changed destination: %v", args)
		}
	}
}

func TestVectorDivisionAndEmptyCount(t *testing.T) {
	for _, count := range []int16{0, -1} {
		vm := vectorVM()
		for _, v := range []int16{0, 1, count, 4} {
			vm.Push(v)
		}
		vm.vector(0xb4)
		if vm.err != nil {
			t.Fatal(vm.err)
		}
	}
	vm := vectorVM()
	for _, v := range []int16{0, 0, 1, 4} {
		vm.Push(v)
	}
	vm.vector(0xb4)
	if vm.err == nil {
		t.Fatal("accepted zero divisor")
	}
	vm = vectorVM()
	vm.Variables[0].Values[0] = -32768
	for _, v := range []int16{0, -1, 1, 4} {
		vm.Push(v)
	}
	vm.vector(0xb4)
	if vm.err != nil || vm.Variables[0].Values[0] != -32768 {
		t.Fatalf("overflow: %v %v", vm.Variables[0].Values, vm.err)
	}
}

func TestVectorScalarZeroDivisorPrecedesCount(t *testing.T) {
	for _, operation := range []int16{4, 5} {
		for _, count := range []int16{-32768, -1, 0, 1} {
			vm := vectorVM()
			for _, v := range []int16{0, 0, count, operation} {
				vm.Push(v)
			}
			vm.vector(0xb4)
			if vm.err == nil || !slices.Equal(vm.Variables[0].Values, []int16{12, 12, 12}) {
				t.Fatalf("zero divisor with count %d: error=%v values=%v", count, vm.err, vm.Variables[0].Values)
			}
		}
	}
}

func TestVectorBudgetPrecedesMutation(t *testing.T) {
	for _, op := range []byte{0xb4, 0xb5, 0xb6} {
		vm := vectorVM()
		args := []int16{0, 3, 3, 1}
		validation := 3
		if op == 0xb5 {
			validation = 6
		} else if op == 0xb6 {
			args = []int16{0, 0x4000, 3, 3, 1}
			validation = 9
		}
		vm.ChargeWork(MaxSteps - validation - 2)
		for _, v := range args {
			vm.Push(v)
		}
		vm.vector(op)
		if vm.err == nil || !slices.Equal(vm.Variables[0].Values, []int16{12, 12, 12}) {
			t.Fatalf("opcode %02x partially mutated before work exhaustion: %v", op, vm.Variables[0].Values)
		}
	}
}

func TestVectorArithmeticFaultRetainsEarlierElements(t *testing.T) {
	for _, op := range []byte{0xb5, 0xb6} {
		vm := vectorVM()
		vm.Variables[1].Values = []int16{3, 0, 3}
		args := []int16{0, 3, 3, 4}
		if op == 0xb6 {
			args = []int16{0, 0x4000, 3, 3, 4}
		}
		for _, v := range args {
			vm.Push(v)
		}
		vm.vector(op)
		if vm.err == nil || !slices.Equal(vm.Variables[0].Values, []int16{4, 12, 12}) {
			t.Fatalf("opcode %02x lost partial arithmetic result: %v", op, vm.Variables[0].Values)
		}
	}
}
