package sgsvm

import (
	"context"
	"strings"
	"testing"
)

func controlVM(code ...byte) *VM {
	return New(&Program{Data: append([]byte{0}, code...), Variables: []Variable{
		{Mutable: true, Offset: 0, Values: []int16{1, 2, 3}},
		{Mutable: true, Offset: 3, Values: []int16{0, 1, 2}},
		{Mutable: false, Offset: 0, Values: []int16{4, 5, 6}},
	}}, nil)
}

func TestAssignmentForms(t *testing.T) {
	// Each location resolves to element one, whose source value is two.
	forms := [][]byte{{0, 1, 1}, {0, 1}, {0, 1}, {0}}
	for destination := byte(0); destination < 4; destination++ {
		for source := byte(0); source < 5; source++ {
			vm := controlVM()
			vm.Variables[1].Values[0] = 1
			vm.Variables[0].Values[0] = 2
			operands := append([]byte(nil), forms[destination]...)
			want := int16(2)
			if source == 4 {
				operands = append(operands, 0xfb)
				want = -5
			} else {
				operands = append(operands, forms[source]...)
			}
			vm.Program.Data = operands
			vm.Push(99)
			vm.control(0x23 + 5*destination + source)
			index := 1
			if destination == 3 {
				index = 0
			}
			if vm.err != nil || vm.Value(0, index) != want || len(vm.stack) != 1 || vm.stack[0] != 99 {
				t.Fatalf("destination %d source %d: value=%d stack=%v err=%v", destination, source, vm.Value(0, index), vm.stack, vm.err)
			}
		}
	}
}

func TestControlStackStoreKeepsValue(t *testing.T) {
	vm := controlVM(5, 0xf9, 0x4b, 0, 0x47, 0x45)
	if err := vm.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if vm.Value(0, 0) != -7 || len(vm.stack) != 1 || vm.stack[0] != -7 {
		t.Fatalf("values=%v stack=%v", vm.Variables[0].Values, vm.stack)
	}
}

func TestControlBranchAndCall(t *testing.T) {
	vm := controlVM(0x44, 0, 7, 0x36, 0, 9, 0x45)
	if err := vm.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if vm.Value(0, 0) != 9 {
		t.Fatal("return did not resume after call operands")
	}
	// A signed comparison must branch across the low-byte boundary.
	vm = controlVM()
	vm.Program.Data = make([]byte, 260)
	copy(vm.Program.Data[1:], []byte{5, 0xff, 0x3c, 0, 1, 2, 0x46})
	copy(vm.Program.Data[258:], []byte{0x45})
	if err := vm.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if vm.PC != 259 {
		t.Fatalf("PC=%d; expected BE16 target258", vm.PC)
	}
}

func TestControlRejectsMalformedExecution(t *testing.T) {
	cases := []struct {
		name    string
		code    []byte
		message string
	}{
		{"jump outside", []byte{0x41, 0xff, 0xff}, "branch target"},
		{"call outside", []byte{0x44, 0, 7}, "branch target"},
		{"truncated jump", []byte{0x41, 0}, "instruction address"},
		{"recursive call", []byte{0x44, 0, 1}, "return stack overflow"},
		{"missing condition", []byte{0x42, 0, 1}, "stack underflow"},
		{"missing store value", []byte{0x4b, 0}, "stack underflow"},
		{"invalid variable", []byte{0x36, 255, 3}, "variable"},
		{"negative index", []byte{0x36, 1, 0xff, 0x2c, 0, 1, 1}, "index -1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := controlVM(tc.code...)
			if err := vm.Run(context.Background(), 1); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v, want %s", err, tc.message)
			}
		})
	}
}

func TestControlAddresses(t *testing.T) {
	vm := controlVM(0x4c, 2, 1, 0x4e, 0x45)
	if err := vm.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if len(vm.stack) != 1 || vm.stack[0] != 5 {
		t.Fatalf("stack=%v", vm.stack)
	}
	vm = controlVM(0x4c, 0, 1, 5, 9, 0x50, 0x45)
	if err := vm.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if vm.Value(0, 1) != 9 || len(vm.stack) != 1 || vm.stack[0] != 9 {
		t.Fatalf("store result=%v stack=%v", vm.Variables[0].Values, vm.stack)
	}
	for _, ref := range []int16{-1, 100, 0x400a} {
		vm = controlVM()
		vm.AddressWrite(ref, 12)
		if vm.err == nil {
			t.Fatalf("accepted address%d", ref)
		}
	}
	vm = controlVM(0x4d, 2, 5, 8, 0x4f, 0x45)
	if err := vm.Run(context.Background(), 1); err == nil {
		t.Fatal("accepted flagged direct-store address")
	}
}
