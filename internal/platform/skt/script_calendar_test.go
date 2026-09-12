package skt

import (
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"testing"
	"time"
)

func TestScriptCalendarLocalDateAndTime(t *testing.T) {
	now := time.Date(2024, time.February, 29, 23, 59, 58, 987654321, time.FixedZone("Fixture", 9*3600))
	for _, tc := range []struct {
		op   byte
		want [4]int16
	}{{0xb8, [4]int16{2024, 2, 29, 4}}, {0xb9, [4]int16{23, 59, 58, 987}}} {
		vm := sgsvm.New(&sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: true, Values: make([]int16, 4)}}}, nil)
		vm.Push(123)
		vm.Push(0)
		if err := scriptCalendarCall(tc.op, vm, now); err != nil {
			t.Fatal(err)
		}
		for i, want := range tc.want {
			if got := vm.Value(0, i); got != want {
				t.Fatalf("opcode %02x field %d: got %d want %d", tc.op, i, got, want)
			}
		}
		if vm.Pop() != 123 {
			t.Fatal("calendar changed caller stack")
		}
	}
}

func TestScriptCalendarInvalidDestinationIsAtomic(t *testing.T) {
	for _, op := range []byte{0xb8, 0xb9} {
		for _, address := range []int16{0, -1, 0x3fff, 0x7fff} {
			vm := sgsvm.New(&sgsvm.Program{Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{42, 43, 44}}}}, nil)
			vm.Push(address)
			if err := scriptCalendarCall(op, vm, time.Now()); err == nil {
				t.Fatal("invalid destination accepted")
			}
			for i, want := range []int16{42, 43, 44} {
				if vm.Variables[0].Values[i] != want {
					t.Fatal("failed calendar query partially wrote destination")
				}
			}
		}
	}
}
