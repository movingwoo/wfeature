package sgsvm

import (
	"context"
	"strings"
	"testing"
)

func TestScalarExecutionAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		code []byte
		want int16
	}{
		{"signed byte", []byte{5, 255}, -1},
		{"big endian word", []byte{6, 0x80, 1}, -32767},
		{"wrapping add", []byte{6, 0x7f, 255, 5, 1, 0x12}, -32768},
		{"signed division", []byte{5, 249, 5, 2, 0x15}, -3},
		{"signed remainder", []byte{5, 249, 5, 2, 0x16}, -1},
		{"arithmetic shift", []byte{5, 248, 5, 1, 0x1b}, -4},
		{"comparison", []byte{5, 255, 5, 1, 0x1e}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := append([]byte{0}, tc.code...)
			code = append(code, 0x0a, 0, 0xff)
			vm := New(&Program{Data: code, Variables: []Variable{{Mutable: true, Values: []int16{0}}}}, nil)
			if err := vm.Run(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			if got := vm.Value(0, 0); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestFailedStorePreservesVariable(t *testing.T) {
	for _, code := range [][]byte{{0, 0x0a, 0, 0xff}, {0, 5, 7, 0x0a}} {
		vm := New(&Program{Data: code, Variables: []Variable{{Mutable: true, Values: []int16{123}}}}, nil)
		if err := vm.Run(context.Background(), 1); err == nil {
			t.Fatal("malformed store accepted")
		}
		if vm.Variables[0].Values[0] != 123 {
			t.Fatal("failed store changed data")
		}
	}
}

func TestConstantInitializerReferencesAreInstanceOwned(t *testing.T) {
	program := &Program{Constants: []int16{4, 5, 6}, Variables: []Variable{{Mutable: true, Values: []int16{4}}, {Offset: 1, Values: []int16{5, 6}}}}
	vm := New(program, nil)
	if vm.AddressRead(0x4000) != 4 {
		t.Fatal("initializer belonging to mutable variable is unreachable")
	}
	vm.AddressWrite(0x4001, 9)
	if vm.Value(1, 0) != 9 || program.Constants[1] != 5 || New(program, nil).Value(1, 0) != 5 {
		t.Fatal("constant bank aliases are not instance-owned")
	}
}

func TestRunStopsOnBudgetAndCancellation(t *testing.T) {
	program := &Program{Data: []byte{0, 0x41, 0, 1}}
	if err := New(program, nil).Run(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("loop error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New(program, nil).Run(ctx, 1); err != context.Canceled {
		t.Fatalf("canceled run=%v", err)
	}
}

func TestCodeCannotBranchIntoResources(t *testing.T) {
	vm := New(&Program{Data: []byte{0, 0x41, 0, 4, 0xff}, CodeStart: 1, CodeEnd: 4}, nil)
	if err := vm.Run(context.Background(), 1); err == nil {
		t.Fatal("branch into data accepted")
	}
}
