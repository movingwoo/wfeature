package skt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptMemoryBytesPreserveWordAndArchive(t *testing.T) {
	for _, base := range []int16{0, 0x4000} {
		program := &sgsvm.Program{Constants: []int16{0x1234, 0x5678}, Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{0x1234, 0x5678}}}}
		vm := sgsvm.New(program, nil)
		for _, tc := range []struct{ offset, value, want int16 }{{0, -1, 0x12ff}, {1, 0x1ab, -21505}, {2, 0x101, 0x5601}, {3, -128, -32767}} {
			for _, arg := range []int16{base, tc.offset, tc.value} {
				vm.Push(arg)
			}
			if err := scriptMemoryCall(0x86, vm); err != nil {
				t.Fatal(err)
			}
			if got := vm.AddressRead(base + tc.offset/2); got != tc.want {
				t.Fatalf("base %04x offset %d: word %04x want %04x", base, tc.offset, uint16(got), uint16(tc.want))
			}
			vm.Push(base)
			vm.Push(tc.offset)
			if err := scriptMemoryCall(0x85, vm); err != nil {
				t.Fatal(err)
			}
			if got := vm.Pop(); got != int16(byte(tc.value)) {
				t.Fatalf("read signed byte: %d", got)
			}
		}
		if program.Constants[0] != 0x1234 || program.Variables[0].Values[0] != 0x1234 {
			t.Fatal("memory operation modified archive")
		}
	}
}

func TestScriptMemoryRejectsInvalidAddressWithoutMutation(t *testing.T) {
	for _, args := range [][]int16{{-1, 0, 7}, {0, -1, 7}, {0, 2, 7}, {0x3fff, 2, 7}, {0x7fff, 2, 7}, {0x4000, 2, 7}, {0}} {
		for _, op := range []byte{0x85, 0x86} {
			vm := sgsvm.New(&sgsvm.Program{Constants: []int16{0x1234}, Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{0x1234}}}}, nil)
			for _, arg := range args[:min(len(args), 2+int(op-0x85))] {
				vm.Push(arg)
			}
			if err := scriptMemoryCall(op, vm); err == nil {
				t.Fatalf("opcode %02x accepted %v", op, args)
			}
			if vm.Variables[0].Values[0] != 0x1234 {
				t.Fatal("invalid write modified memory")
			}
		}
	}
}

func TestScriptMemoryWorkBudgetStopsBeforeWrite(t *testing.T) {
	vm := sgsvm.New(&sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{0x1234}}}}, nil)
	if err := vm.ChargeWork(sgsvm.MaxSteps); err != nil {
		t.Fatal(err)
	}
	vm.Push(0)
	vm.Push(0)
	vm.Push(0)
	if err := scriptMemoryCall(0x86, vm); err == nil {
		t.Fatal("write exceeded work budget")
	}
	if vm.Variables[0].Values[0] != 0x1234 {
		t.Fatal("budget exhaustion modified memory")
	}
}
