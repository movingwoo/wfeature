package skt

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func authoredSISHeader(frames, width, height, objects byte) []byte {
	header := uint64(frames)<<35 | uint64(width)<<25 | uint64(height)<<21 | uint64(objects-1)<<15
	return []byte{'S', 'I', 'S', byte(header >> 32), byte(header >> 24), byte(header >> 16), byte(header >> 8), byte(header)}
}

func authoredSISByteHeader(selector, frames, width, height, subtype byte, delays bool) []byte {
	data := []byte{'S', 'I', 'S', selector << 3, 0, 0, width, height, 0, frames, 0, 0, 1}
	if delays {
		data[12] = 0x80
		data = append(data, make([]byte, int(frames))...)
	}
	return append(data, 0, 0, subtype, 0, 1)
}

func TestScriptSISMetadataHeaders(t *testing.T) {
	for _, tc := range []struct {
		data []byte
		want [5]int16
	}{
		{authoredSISHeader(1, 1, 1, 1), [5]int16{1, 1, 1, 8, 8}},
		{authoredSISHeader(20, 31, 15, 20), [5]int16{1, 1, 20, 248, 120}},
		{authoredSISByteHeader(0, 3, 129, 255, 2, false), [5]int16{2, 1, 3, 128, 248}},
		{authoredSISByteHeader(29, 3, 0, 0, 2, true), [5]int16{2, 1, 3, 256, 256}},
		{authoredSISByteHeader(30, 255, 1, 7, 9, true), [5]int16{9, 1, 255, 256, 256}},
		{authoredSISByteHeader(0, 1, 8, 8, 0, false), [5]int16{0, 1, 1, 8, 8}},
	} {
		got, ok := scriptSISMetadata(tc.data)
		if !ok || got != tc.want {
			t.Fatalf("header %x: got %v valid=%v want %v", tc.data, got, ok, tc.want)
		}
		for n := 0; n < len(tc.data); n++ {
			if _, ok := scriptSISMetadata(tc.data[:n]); ok {
				t.Fatalf("accepted truncated header at %d of %d", n, len(tc.data))
			}
		}
	}
	for _, data := range [][]byte{
		[]byte("PNG header"), authoredSISHeader(1, 0, 1, 1), authoredSISHeader(1, 1, 0, 1),
		authoredSISHeader(1, 1, 1, 21), authoredSISHeader(21, 1, 1, 1),
		authoredSISByteHeader(0, 0, 8, 8, 2, false), authoredSISByteHeader(0, 1, 8, 8, 8, false),
	} {
		if _, ok := scriptSISMetadata(data); ok {
			t.Fatalf("accepted malformed header %x", data)
		}
	}
}

func TestScriptImageInfoInstructionWritesBothBanks(t *testing.T) {
	for _, constant := range []bool{false, true} {
		values := []int16{91, 92, 93, 94, 95, 96, 97}
		p := &sgsvm.Program{Data: []byte{0, 5, 71}, Variables: []sgsvm.Variable{{Mutable: !constant, Values: values}}, Resources: []sgsvm.Resource{{Data: authoredSISHeader(3, 16, 12, 1)}}}
		base := int16(0)
		if constant {
			p.Constants = values
			base = 0x4000
		}
		for _, arg := range []int16{1, 1, 0, base + 1, 0, 42, 43} {
			p.Data = append(p.Data, 6, byte(uint16(arg)>>8), byte(arg))
		}
		p.Data = append(p.Data, 0xe8, 0xff)
		vm := sgsvm.New(p, &ScriptSession{})
		if err := vm.Run(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		if vm.Pop() != 0 || vm.Pop() != 71 || !slices.Equal(vm.Variables[0].Values, []int16{91, 1, 1, 3, 128, 96, 97}) {
			t.Fatalf("metadata query bank=%v values=%v", constant, vm.Variables[0].Values)
		}
	}
}

func TestScriptImageInfoFailuresDoNotMutateMemory(t *testing.T) {
	for _, tc := range []struct {
		args []int16
		data []byte
		work bool
	}{
		{[]int16{1, 1, 0, 1, 0, 0, 0}, nil, false},
		{[]int16{1, 1, 0, -1, 0, 0, 0}, nil, false},
		{[]int16{1, 1, 0, 0x3fff, 0, 0, 0}, nil, false},
		{[]int16{1, 1, 1, 0, 0, 0, 0}, nil, false},
		{[]int16{1, 1, 0, 0, 0, 0, 0}, nil, true},
		{[]int16{3, 1, 0, 0, 0, 0, 0}, nil, false},
		{[]int16{1, 1, 0, 0, 0, 0, 0}, []byte("SAF\x00"), false},
	} {
		data := tc.data
		if data == nil {
			data = authoredSISHeader(1, 1, 1, 1)
		}
		vm := sgsvm.New(&sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{1, 2, 3, 4, 5}}}, Resources: []sgsvm.Resource{{Data: data}}}, nil)
		if tc.work {
			vm.ChargeWork(sgsvm.MaxSteps)
		}
		for _, arg := range tc.args {
			vm.Push(arg)
		}
		if err := (&ScriptSession{}).Call(0xe8, vm); err == nil {
			t.Fatalf("invalid metadata query accepted: %v", tc.args)
		}
		if !slices.Equal(vm.Variables[0].Values, []int16{1, 2, 3, 4, 5}) || !bytes.Equal(vm.Resources[0].Data, data) {
			t.Fatal("failed query mutated guest state")
		}
	}
	for n := 0; n < 7; n++ {
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		for i := 0; i < n; i++ {
			vm.Push(71)
		}
		if err := (&ScriptSession{}).Call(0xe8, vm); err == nil {
			t.Fatalf("accepted %d operands", n)
		}
		for i := 0; i < n; i++ {
			if vm.Pop() != 71 {
				t.Fatal("underflow consumed caller operands")
			}
		}
	}
}

func TestScriptImageInfoFormatFailureReturnsMinusOne(t *testing.T) {
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: []byte("invalid")}}}, nil)
	vm.Push(71)
	for _, arg := range []int16{1, 1, 0, 0, 0, 0, 0} {
		vm.Push(arg)
	}
	if err := (&ScriptSession{}).Call(0xe8, vm); err != nil {
		t.Fatal(err)
	}
	if vm.Pop() != -1 || vm.Pop() != 71 {
		t.Fatal("invalid image changed result or caller stack")
	}
}
