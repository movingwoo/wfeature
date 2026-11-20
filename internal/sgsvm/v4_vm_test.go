package sgsvm

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
)

func wideTestProgram(words ...uint16) *Program {
	const start = 128
	data := make([]byte, start+2*len(words))
	for i, w := range words {
		binary.LittleEndian.PutUint16(data[start+2*i:], w)
	}
	return &Program{CodeStart: start, CodeEnd: len(data), Data: data,
		Wide: &WideProgram{Variables: [][]int32{make([]int32, 20), {7, 11}, {0}}}}
}

func TestWideExecution(t *testing.T) {
	// Call a function that increments array[1] through an lvalue,
	// then return its result and a long immediate to the host.
	program := wideTestProgram(
		0, 0x85, 0, 150, 7, 0x1234, 0xabcd, 0x92, 43, 0xff, 0,
		0x8d, 1, 1, 0x10, 0x8f, 0x0e, 0x91, 0x86)
	calls := 0
	vm := NewWide(program, func(id uint16, vm *WideVM) error {
		a, err := vm.Args(2)
		if err != nil {
			return err
		}
		if id != 43 || a[0] != 12 || a[1] != 0x1234abcd {
			t.Fatalf("service %d args %v", id, a)
		}
		calls++
		return nil
	})
	if err := vm.Run(context.Background(), 128); err != nil || calls != 1 {
		t.Fatalf("calls %d, error %v", calls, err)
	}
	if program.Wide.Variables[1][1] != 11 || vm.Variables[1][1] != 12 {
		t.Fatal("variable banks not isolated")
	}
}

func TestWideInvalidCode(t *testing.T) {
	for _, tc := range []struct {
		words []uint16
		want  string
	}{
		{[]uint16{0x81, 0, 129}, "target"},
		{[]uint16{0x8d, 1, 2, 0xff}, "index"},
		{[]uint16{0x0b, 0}, "stack"},
		{[]uint16{7, 5}, "truncated"},
		{[]uint16{0x81, 0, 128}, "limit"},
	} {
		vm := NewWide(wideTestProgram(tc.words...), nil)
		if err := vm.Run(context.Background(), 128); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("words %x: %v", tc.words, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	vm := NewWide(wideTestProgram(0xff), nil)
	if vm.Run(ctx, 128) == nil {
		t.Fatal("cancelled execution")
	}
}

func TestWideArithmeticAndStackRegister(t *testing.T) {
	// The saved depth is one reusable register. ARM shifts use the low
	// byte as the shift count and extend the sign on right shift.
	p := wideTestProgram(5, 0xfffd, 5, 1, 0x57, 0xc,
		5, 9, 0xd, 5, 8, 0xd,
		5, 1, 5, 256, 0x58,
		0xeb, 0, 2, 0xee, 1, 0xe8, 0, 1, 0xffff,
		0x92, 17, 0xff)
	vm := NewWide(p, func(id uint16, v *WideVM) error {
		a, err := v.Args(4)
		if err != nil {
			return err
		}
		want := []int32{-2, 1, 7, 6}
		for i, x := range a {
			if x != want[i] {
				t.Fatalf("stack %v, want %v", a, want)
			}
		}
		v.EndEvent()
		return nil
	})
	if err := vm.Run(context.Background(), 128); err != nil || vm.Exited || vm.PC != p.CodeEnd-2 {
		t.Fatalf("callback PC %#x exited %t: %v", vm.PC, vm.Exited, err)
	}
	if err := vm.Run(context.Background(), p.CodeEnd-2); err != nil {
		t.Fatal(err)
	}
}

func TestWideMalformedStorePreservesData(t *testing.T) {
	for _, words := range [][]uint16{
		{0x75, 1}, {0x75, 65535, 42}, {0x0b, 1},
		{0x90}, {5, 0xffff, 5, 42, 0x90}, {0x8e, 65535},
		{5, 0, 5, 42, 0x90}, {5, 19, 5, 42, 0x91, 0xff},
	} {
		p := wideTestProgram(words...)
		vm := NewWide(p, nil)
		if err := vm.Run(context.Background(), 128); err == nil {
			t.Errorf("accepted %x", words)
		}
		if vm.Variables[1][0] != 7 || vm.Variables[0][0] != 0 || vm.Variables[0][19] != 0 {
			t.Errorf("mutated bank on %x", words)
		}
	}
}
