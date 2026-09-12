package skt

import (
	"bytes"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"testing"
)

func transferVM(constant bool) (*sgsvm.VM, int16) {
	values := []int16{0x2211, 0x4433, 0x6655}
	p := &sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: !constant, Values: values}}, Resources: []sgsvm.Resource{{Mutable: true, Data: []byte{0xaa, 0xbb, 0xcc}}}}
	address := int16(0)
	if constant {
		p.Constants = values
		address = 0x4000
	}
	return sgsvm.New(p, nil), address
}

func TestScriptResourceTransfersLittleEndianOddOffset(t *testing.T) {
	for _, constant := range []bool{false, true} {
		for _, op := range []byte{0x87, 0x88} {
			vm, address := transferVM(constant)
			vm.Push(123)
			for _, v := range []int16{address, 1, 0, 3} {
				vm.Push(v)
			}
			if err := (&ScriptSession{}).Call(op, vm); err != nil {
				t.Fatal(err)
			}
			if op == 0x87 {
				if !bytes.Equal(vm.Resources[0].Data[:3], []byte{0x22, 0x33, 0x44}) {
					t.Fatalf("export %x", vm.Resources[0].Data)
				}
				if vm.AddressRead(address) != 0x2211 {
					t.Fatal("export changed source")
				}
			} else {
				if uint16(vm.AddressRead(address)) != 0xaa11 || uint16(vm.AddressRead(address+1)) != 0xccbb || vm.AddressRead(address+2) != 0x6655 {
					t.Fatal("import damaged byte order or neighboring bytes")
				}
			}
			if vm.Pop() != 123 {
				t.Fatal("transfer changed caller stack")
			}
		}
	}
}

func TestScriptResourceTransferRejectsBeforeMutation(t *testing.T) {
	for _, op := range []byte{0x87, 0x88} {
		for _, args := range [][]int16{{-1, 0, 0, 1}, {0, -1, 0, 1}, {0, 0, 0, -1}, {0, 0, 1, 1}, {0, 4, 0, 3}, {0x3fff, 1, 0, 2}, {0, 0, 0, 7}} {
			vm, _ := transferVM(false)
			before := bytes.Clone(vm.Resources[0].Data)
			for _, v := range args {
				vm.Push(v)
			}
			if err := scriptCopyCall(op, vm); err == nil {
				t.Fatalf("opcode %02x accepted invalid span %v", op, args)
			}
			if !bytes.Equal(vm.Resources[0].Data, before) || vm.Variables[0].Values[0] != 0x2211 || vm.Variables[0].Values[1] != 0x4433 {
				t.Fatal("failed transfer partially mutated destination")
			}
		}
	}
	// A valid memory destination still must not change when resource input ends early.
	vm, _ := transferVM(false)
	for _, v := range []int16{0, 0, 0, 4} {
		vm.Push(v)
	}
	if err := scriptCopyCall(0x88, vm); err == nil {
		t.Fatal("truncated resource accepted")
	}
	if vm.Variables[0].Values[0] != 0x2211 {
		t.Fatal("truncated import partially wrote")
	}
}

func TestScriptResourceTransferLimitsAndEmptyCount(t *testing.T) {
	for _, op := range []byte{0x87, 0x88} {
		vm, _ := transferVM(false)
		vm.ChargeWork(sgsvm.MaxSteps)
		for _, v := range []int16{0, 0, 0, 2} {
			vm.Push(v)
		}
		if err := scriptCopyCall(op, vm); err == nil {
			t.Fatal("transfer ignored work budget")
		}
		vm, _ = transferVM(false)
		vm.Push(123)
		if err := scriptCopyCall(op, vm); err == nil || vm.Pop() != 123 {
			t.Fatal("underflow consumed operands")
		}
		vm, _ = transferVM(false)
		for _, v := range []int16{0, 6, 0, 0} {
			vm.Push(v)
		}
		if err := scriptCopyCall(op, vm); err != nil {
			t.Fatal(err)
		}
		if vm.Variables[0].Values[0] != 0x2211 {
			t.Fatal("empty transfer modified memory")
		}
	}
}
