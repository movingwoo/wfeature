package skt

import (
	"bytes"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptDeviceIdentifierRetainsAllocatorSemantics(t *testing.T) {
	for _, length := range []int{0, 1, 16, 17, 18} {
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Mutable: true, Data: bytes.Repeat([]byte{42}, length)}}}, nil)
		vm.Push(123)
		vm.Push(0)
		if err := (&ScriptSession{}).Call(0x52, vm); err != nil {
			t.Fatal(err)
		}
		wantLength := length
		if length == 0 {
			wantLength = 2
		} else if length > 17 {
			wantLength = 1
		}
		if len(vm.Resources[0].Data) != wantLength || vm.Resources[0].Data[0] != 0 || vm.Pop() != 123 {
			t.Fatalf("identifier query failed at initial capacity %d", length)
		}
		if length > 1 && length <= 17 && vm.Resources[0].Data[1] != 42 {
			t.Fatal("identifier query cleared retained resource tail")
		}
	}
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: []byte{42}}}}, nil)
	vm.Push(1)
	if err := (&ScriptSession{}).Call(0x52, vm); err == nil || vm.Resources[0].Data[0] != 42 {
		t.Fatal("invalid identifier destination accepted or changed resource")
	}
}

func TestScriptDeviceInfoDimensionsAndBanks(t *testing.T) {
	for _, size := range [][3]int{{79, 120, 1}, {80, 119, 1}, {80, 120, 2}, {128, 127, 2}, {128, 128, 4}, {176, 175, 4}, {176, 176, 8}} {
		for _, constant := range []bool{false, true} {
			for _, op := range []byte{0x51, 0x54} {
				values := []int16{91, 92, 93, 94, 95, 96, 97}
				p := &sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: !constant, Values: values}}}
				base := int16(0)
				if constant {
					p.Constants = values
					base = 0x4000
				}
				vm := sgsvm.New(p, nil)
				s := &ScriptSession{graphics: &scriptGraphics{width: size[0], height: size[1]}}
				vm.Push(123)
				vm.Push(base + 1)
				if err := s.Call(op, vm); err != nil {
					t.Fatal(err)
				}
				want := []int16{91, int16(size[2]), 4, 256, 1, 96, 97}
				if op == 0x54 {
					want = []int16{91, 0, 0, 0, 0, 0, 97}
				}
				for i, value := range want {
					if got := vm.AddressRead(base + int16(i)); got != value {
						t.Fatalf("opcode %02x size %v bank %v word %d = %d, want %d", op, size, constant, i, got, value)
					}
				}
				if vm.Pop() != 123 {
					t.Fatal("device query changed caller stack")
				}
			}
		}
	}
}

func TestScriptDeviceInfoFailureIsAtomic(t *testing.T) {
	for _, op := range []byte{0x51, 0x54} {
		for _, address := range []int16{1, -1, 0x3fff, 0x7fff} {
			values := []int16{41, 42, 43, 44}
			vm := sgsvm.New(&sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: true, Values: values}}}, nil)
			vm.Push(address)
			s := &ScriptSession{graphics: &scriptGraphics{width: 128, height: 160}}
			if err := s.Call(op, vm); err == nil {
				t.Fatal("invalid device destination accepted")
			}
			if !slices.Equal(vm.Variables[0].Values, values) {
				t.Fatal("failed device query partially wrote destination")
			}
		}
		vm := sgsvm.New(&sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{1, 2, 3, 4, 5}}}}, nil)
		vm.ChargeWork(sgsvm.MaxSteps)
		vm.Push(0)
		s := &ScriptSession{graphics: &scriptGraphics{width: 128, height: 160}}
		if err := s.Call(op, vm); err == nil || !slices.Equal(vm.Variables[0].Values, []int16{1, 2, 3, 4, 5}) {
			t.Fatal("device query ignored work budget or mutated on failure")
		}
	}
}
