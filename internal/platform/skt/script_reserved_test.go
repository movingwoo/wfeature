package skt

import (
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"testing"
)

func TestScriptReservedAndDeviceNoopStackContracts(t *testing.T) {
	for _, tc := range []struct {
		op    byte
		count int
	}{
		{0xe3, 0}, {0xe4, 0}, {0xe5, 0}, {0xe9, 0}, {0xea, 0}, {0xeb, 0}, {0xec, 0}, {0xed, 0}, {0xee, 0}, {0xef, 0}, {0xf2, 0},
		{0xdb, 2}, {0xdc, 1}, {0xdd, 1}, {0xe0, 6},
	} {
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		vm.Push(123)
		for i := 0; i < tc.count; i++ {
			vm.Push(int16(i))
		}
		if err := (&ScriptSession{}).Call(tc.op, vm); err != nil {
			t.Fatal(err)
		}
		if vm.Pop() != 123 {
			t.Fatalf("opcode %02x changed caller stack", tc.op)
		}
	}
	for _, op := range []byte{0xdb, 0xdc, 0xdd, 0xe0, 0xe1} {
		if err := (&ScriptSession{}).Call(op, sgsvm.New(&sgsvm.Program{}, nil)); err == nil {
			t.Fatalf("opcode %02x accepted underflow", op)
		}
	}
}

func TestScriptDeviceCapabilityProbe(t *testing.T) {
	for _, input := range []int16{-32768, -1, 0, 1, 2, 32767} {
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		vm.Push(123)
		vm.Push(input)
		if err := (&ScriptSession{}).Call(0xe1, vm); err != nil {
			t.Fatal(err)
		}
		want := int16(-1)
		if input == 1 {
			want = 0
		}
		if got := vm.Pop(); got != want {
			t.Fatalf("probe %d: got %d want %d", input, got, want)
		}
		if vm.Pop() != 123 {
			t.Fatal("probe changed caller stack")
		}
	}
}
