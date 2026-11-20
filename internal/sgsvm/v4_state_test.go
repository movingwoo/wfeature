package sgsvm

import (
	"context"
	"errors"
	"testing"
)

func wideStateProgram() *Program {
	p := wideTestProgram(0x92, 1, 0xff, 0x7a, 2, 3, 0x86)
	p.Resources = []Resource{
		{Kind: 1, Data: []byte("asset")},
		{Mutable: true, Data: []byte("initial")},
		{Mutable: true},
	}
	return p
}

func TestWideStateRoundTrip(t *testing.T) {
	p := wideStateProgram()
	calls := 0
	vm := NewWide(p, func(_ uint16, v *WideVM) error {
		calls++
		v.EndEvent()
		if _, err := v.CaptureState(); err == nil {
			t.Fatal("captured while inside a host callback")
		}
		return nil
	})
	if err := vm.Run(t.Context(), p.CodeStart); err != nil {
		t.Fatal(err)
	}
	vm.data[21] = -99
	vm.Resources[1].Data = make([]byte, 3, 11)
	copy(vm.Resources[1].Data, "set")
	saved, err := vm.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	vm.data[21], vm.Resources[1].Data[0] = 81, 'x'
	restoredCalls := 0
	restored, err := RestoreWideState(p, func(_ uint16, _ *WideVM) error {
		restoredCalls++
		return nil
	}, saved)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || restoredCalls != 0 || restored.PC != p.CodeStart+4 || restored.Exited ||
		restored.Variables[1][1] != -99 || string(restored.Resources[1].Data) != "set" ||
		cap(restored.Resources[1].Data) != 11 || &restored.Resources[0].Data[0] != &p.Resources[0].Data[0] {
		t.Fatal("restored callback, words, resources or heap capacity disagree")
	}
	saved.Words[21], saved.Mutable[0].Data[0] = 50, 'y'
	words, err := restored.Address(21, 2)
	if err != nil {
		t.Fatal(err)
	}
	words[1] = 20
	if err := restored.Run(t.Context(), p.CodeStart+6); err != nil {
		t.Fatal(err)
	}
	if restored.Variables[2][0] != 23 || words[1] != 23 || p.Wide.Variables[2][0] != 0 ||
		words[0] != -99 || string(restored.Resources[1].Data) != "set" || string(p.Resources[1].Data) != "initial" {
		t.Fatal("word alias or snapshot ownership changed")
	}
	if err := restored.Run(t.Context(), p.CodeStart); err != nil || restoredCalls != 1 || calls != 1 {
		t.Fatalf("restored host calls %d, original calls %d, error %v", restoredCalls, calls, err)
	}
}

func TestWideStateEventBoundaries(t *testing.T) {
	p := wideTestProgram(0x87)
	vm := NewWide(p, nil)
	saved, err := vm.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreWideState(p, nil, saved); err != nil || restored.PC != 0 || restored.Exited {
		t.Fatalf("initial state: %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := vm.Run(cancelled, p.CodeStart); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := vm.CaptureState(); err == nil {
		t.Fatal("captured cancelled event")
	}
	if err := vm.Run(t.Context(), p.CodeStart); err != nil {
		t.Fatal(err)
	}
	saved, err = vm.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreWideState(p, nil, saved); err != nil || !restored.Exited || restored.PC != p.CodeEnd {
		t.Fatalf("exit state: %v", err)
	}
	vm = NewWide(wideTestProgram(0x000b, 2), nil)
	if vm.Run(t.Context(), p.CodeStart) == nil {
		t.Fatal("expected missing operand")
	}
	if _, err := vm.CaptureState(); err == nil {
		t.Fatal("captured an unsuccessful instruction")
	}
}

func TestWideStateRejectsMalformedRecords(t *testing.T) {
	p := wideStateProgram()
	for range 2 {
		p.Resources = append(p.Resources, Resource{Mutable: true})
	}
	vm := NewWide(p, nil)
	for name, change := range map[string]func(*WideState){
		"version":                  func(s *WideState) { s.Version++ },
		"program":                  func(s *WideState) { s.ProgramIdentity[0] ^= 1 },
		"misaligned pc":            func(s *WideState) { s.PC = p.CodeStart + 1 },
		"below code":               func(s *WideState) { s.PC = -2 },
		"after code":               func(s *WideState) { s.PC = p.CodeEnd + 2 },
		"before first instruction": func(s *WideState) { s.PC = p.CodeStart },
		"exit before start":        func(s *WideState) { s.Exited = true },
		"word count":               func(s *WideState) { s.Words = s.Words[1:] },
		"media count":              func(s *WideState) { s.Mutable = s.Mutable[1:] },
		"capacity below length":    func(s *WideState) { s.Mutable[0].Capacity = 2 },
		"bank capacity":            func(s *WideState) { s.Mutable[0].Capacity = MaxWideSize + 1 },
		"heap incl immutable capacity": func(s *WideState) {
			for i := range s.Mutable {
				s.Mutable[i].Capacity = MaxWideSize
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			saved, err := vm.CaptureState()
			if err != nil {
				t.Fatal(err)
			}
			change(&saved)
			if got, err := RestoreWideState(p, nil, saved); got != nil || err == nil {
				t.Fatal("accepted malformed record")
			}
		})
	}
	saved, err := vm.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Program){
		"code":      func(other *Program) { other.Data[p.CodeStart] ^= 1 },
		"callbacks": func(other *Program) { other.Wide.Entries[3] = p.CodeStart },
		"bank extent": func(other *Program) {
			other.Wide.Variables[1], other.Wide.Variables[2] = other.Wide.Variables[2], other.Wide.Variables[1]
		},
		"initial word":     func(other *Program) { other.Wide.Variables[1][0]++ },
		"initial resource": func(other *Program) { other.Resources[0].Data[0]++ },
	} {
		t.Run(name, func(t *testing.T) {
			other := wideStateProgram()
			other.Resources = append(other.Resources, Resource{Mutable: true}, Resource{Mutable: true})
			change(other)
			if got, err := RestoreWideState(other, nil, saved); got != nil || err == nil {
				t.Fatal("accepted state for a different program")
			}
		})
	}
}
