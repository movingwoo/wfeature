package sgsvm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func stateTestProgram() *Program {
	return &Program{
		Data: []byte{0, 0xff}, CodeStart: 1, CodeEnd: 2, Entries: [8]uint16{1},
		Constants: []int16{10, 20, 30, 40},
		Variables: []Variable{
			{Offset: 1, Values: []int16{20, 30}},
			{Mutable: true, Values: []int16{4, 5}},
			{Offset: 2, Values: []int16{30, 40}},
		},
		Resources: []Resource{
			{Kind: 1, Data: []byte{1, 2}},
			{Mutable: true, Kind: 2, Data: []byte{3, 4}},
			{Kind: 3, Data: []byte{5, 6}},
			{Mutable: true, Kind: 4, Data: []byte{7, 8}},
		},
	}
}

func captureStateTest(t *testing.T, vm *VM, external ...[]byte) State {
	t.Helper()
	saved, err := vm.CaptureState(external)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestStateKeepsConstantAliasesAndOwnsWords(t *testing.T) {
	program := stateTestProgram()
	vm := New(program, nil)
	vm.Set(0, 1, 77)
	vm.AddressWrite(0x4000, 88)
	vm.Set(1, 0, 99)
	if err := vm.Run(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	saved := captureStateTest(t, vm)
	restored, external, err := RestoreState(program, nil, saved)
	if err != nil || len(external) != 0 {
		t.Fatalf("restore: %v, external %d", err, len(external))
	}
	if restored.Value(0, 1) != 77 || restored.Value(2, 0) != 77 || restored.AddressRead(0x4000) != 88 || restored.Value(1, 0) != 99 {
		t.Fatal("variable or complete constant bank was not restored")
	}
	restored.Set(0, 1, 66)
	if restored.Value(2, 0) != 66 || restored.AddressRead(0x4002) != 66 {
		t.Fatal("restored variable lost its constant alias")
	}
	if vm.Value(0, 1) != 77 || saved.Constants[2] != 77 || program.Constants[2] != 30 || program.Variables[0].Values[1] != 30 {
		t.Fatal("restored word bank aliases source, record or program")
	}
	saved.Constants[0], saved.Variables[1].Values[0] = -1, -1
	if restored.AddressRead(0x4000) != 88 || restored.Value(1, 0) != 99 {
		t.Fatal("restored words borrow the checkpoint record")
	}
}

func TestStateKeepsResourceRegionsAndExternalAliases(t *testing.T) {
	program := stateTestProgram()
	vm := New(program, nil)
	vm.Resources[0].Data[0] = 11
	vm.Resources[1].Data[0] = 33
	vm.Resources[2].Data[0] = 55
	saved := captureStateTest(t, vm, vm.Resources[2].Data[:1], vm.Resources[0].Data[1:])
	restored, external, err := RestoreState(program, nil, saved)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ResourceByte(0, 2) != 3 || restored.ResourceByte(0, 4) != 55 || restored.ResourceByte(1, 2) != 7 {
		t.Fatal("original module or packed mutable overread changed")
	}
	external[0][0], external[1][0] = 99, 88
	if restored.Resources[2].Data[0] != 99 || restored.ResourceByte(0, 4) != 99 || restored.Resources[0].Data[1] != 88 {
		t.Fatal("external views lost overlapping resource aliases")
	}
	if vm.Resources[2].Data[0] != 55 || program.Resources[2].Data[0] != 5 {
		t.Fatal("restored resource aliases source or program")
	}
	for _, buffer := range saved.Buffers {
		clear(buffer)
	}
	if restored.Resources[0].Data[0] != 11 || external[0][0] != 99 {
		t.Fatal("restored resource borrows the checkpoint record")
	}
}

func TestStateRetainsDetachedQueueStorageAfterResourceResize(t *testing.T) {
	program := stateTestProgram()
	vm := New(program, nil)
	oldImmutable, oldMutable := vm.Resources[0].Data, vm.Resources[1].Data
	vm.Resources[0].Data = []byte{9, 9, 9}
	vm.Resources[1].Data = []byte{8, 8, 8}
	saved := captureStateTest(t, vm, oldImmutable, oldMutable, oldMutable[1:])
	restored, external, err := RestoreState(program, nil, saved)
	if err != nil {
		t.Fatal(err)
	}
	external[0][0], external[1][1] = 44, 66
	if restored.Resources[0].Data[0] != 9 || restored.resourceViews[0][0] != 44 || restored.Resources[1].Data[1] != 8 || external[2][0] != 66 {
		t.Fatal("detached storage was rebound to a replacement resource")
	}
	if restored.ResourceByte(0, 3) != 0 || restored.Error() == nil {
		t.Fatal("resized immutable allocation acquired original module overread")
	}
}

func TestStateOriginalRegionMayStartAfterMutableResources(t *testing.T) {
	program := &Program{Resources: []Resource{{Mutable: true, Data: []byte{1, 2}}, {Data: []byte{3}}, {Data: []byte{4, 5}}}}
	vm := New(program, nil)
	saved := captureStateTest(t, vm)
	restored, _, err := RestoreState(program, nil, saved)
	if err != nil || restored.ResourceByte(1, 2) != 5 {
		t.Fatalf("original resource suffix: %v", err)
	}
}

func TestStateEncodingAndRecaptureAreDeterministic(t *testing.T) {
	program := stateTestProgram()
	vm := New(program, nil)
	detached := vm.Resources[1].Data
	vm.Resources[1].Data = []byte{8, 8, 8}
	if err := vm.Run(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	saved := captureStateTest(t, vm, vm.Resources[2].Data, detached, detached[1:])
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded State
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	restored, external, err := RestoreState(program, nil, decoded)
	if err != nil {
		t.Fatal(err)
	}
	again := captureStateTest(t, restored, external...)
	actual, err := json.Marshal(again)
	if err != nil || !bytes.Equal(encoded, actual) {
		t.Fatalf("recapture changed record: %v", err)
	}
	vm.Resources[0].Data[0]++
	vm.Set(1, 0, 90)
	if reflect.DeepEqual(saved, captureStateTest(t, vm, vm.Resources[2].Data, detached, detached[1:])) {
		t.Fatal("capture omitted changed runtime banks")
	}
}

type stateCaptureService struct{ captured error }

func (service *stateCaptureService) Call(_ byte, vm *VM) error {
	_, service.captured = vm.CaptureState(nil)
	return nil
}

func TestStateRejectsActiveOrFailedEventAndPreservesExit(t *testing.T) {
	service := &stateCaptureService{}
	program := &Program{Data: []byte{0, 0x55, 0xff}, CodeStart: 1}
	vm := New(program, service)
	if err := vm.Run(t.Context(), 1); err != nil || service.captured == nil {
		t.Fatalf("active event capture: %v, %v", err, service.captured)
	}
	captureStateTest(t, vm)
	vm.Fail(errors.New("fixture failure"))
	if _, err := vm.CaptureState(nil); err == nil {
		t.Fatal("failed VM was captured")
	}
	vm = New(&Program{Data: []byte{0, 0x46}, CodeStart: 1}, nil)
	if err := vm.Run(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	restored, _, err := RestoreState(vm.Program, nil, captureStateTest(t, vm))
	if err != nil || !restored.Exited() {
		t.Fatalf("exit state: %v", err)
	}
	vm = New(program, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := vm.Run(ctx, 1); err == nil {
		t.Fatal("cancelled event succeeded")
	}
	if _, err := vm.CaptureState(nil); err == nil {
		t.Fatal("cancelled event was captured")
	}
}

func TestStateRejectsMalformedRecordsBeforeRestoring(t *testing.T) {
	program := stateTestProgram()
	vm := New(program, nil)
	if err := vm.Run(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*State){
		"version":           func(s *State) { s.Version++ },
		"program":           func(s *State) { s.ProgramIdentity[0]++ },
		"negative PC":       func(s *State) { s.PC = -1 },
		"PC outside code":   func(s *State) { s.PC = len(program.Data) + 1 },
		"PC before event":   func(s *State) { s.PC = program.CodeStart },
		"active event":      func(s *State) { s.Ended = false },
		"exit before start": func(s *State) { s.Ended, s.PC, s.Exited = false, 0, true },
		"constant length":   func(s *State) { s.Constants = s.Constants[:1] },
		"variable count":    func(s *State) { s.Variables = nil },
		"variable offset":   func(s *State) { s.Variables[0].Offset++ },
		"variable length":   func(s *State) { s.Variables[0].Length++ },
		"variable mutable":  func(s *State) { s.Variables[0].Mutable = true },
		"constant alias":    func(s *State) { s.Variables[0].Constant = false },
		"alias payload":     func(s *State) { s.Variables[0].Values = []int16{1} },
		"mutable payload":   func(s *State) { s.Variables[1].Values = nil },
		"resource count":    func(s *State) { s.Resources = nil },
		"resource kind":     func(s *State) { s.Resources[0].Kind++ },
		"resource mutable":  func(s *State) { s.Resources[0].Mutable = true },
		"missing buffer":    func(s *State) { s.Resources[0].Data.Buffer = len(s.Buffers) + 1 },
		"negative buffer":   func(s *State) { s.Resources[0].Data.Buffer = -1 },
		"null buffer":       func(s *State) { s.Resources[0].Data.Buffer = 0 },
		"negative length":   func(s *State) { s.Resources[0].Data.Length = -1 },
		"negative offset":   func(s *State) { s.Resources[0].Data.Offset = -1 },
		"offset overflow":   func(s *State) { s.Resources[0].Data.Offset = int(^uint(0) >> 1) },
		"range overflow":    func(s *State) { s.Resources[0].Data.Length = 65535 },
		"bank overflow":     func(s *State) { s.Resources[0].Data.Length = 65536 },
		"view count":        func(s *State) { s.ResourceViews = nil },
		"view length":       func(s *State) { s.ResourceViews[0].Length-- },
		"view offset":       func(s *State) { s.ResourceViews[2].Offset-- },
		"buffer length":     func(s *State) { s.Buffers[0] = make([]byte, stateProgramLimit+1) },
		"unreferenced":      func(s *State) { s.Buffers = append(s.Buffers, []byte{1}) },
		"external count":    func(s *State) { s.External = make([]ByteSliceState, stateExternalLimit+1) },
		"external length":   func(s *State) { s.External[0].Length = stateBankLimit + 1 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			saved := captureStateTest(t, vm, vm.Resources[0].Data)
			mutate(&saved)
			if restored, external, err := RestoreState(program, nil, saved); err == nil || restored != nil || external != nil {
				t.Fatalf("malformed state accepted: %v", err)
			}
		})
	}
	other := stateTestProgram()
	other.Resources[0].Data[0]++
	if _, _, err := RestoreState(other, nil, captureStateTest(t, vm)); err == nil {
		t.Fatal("matching code with different initial resource data was accepted")
	}
}

func TestStateResourceAndStorageBudgets(t *testing.T) {
	program := &Program{Resources: make([]Resource, 258)}
	for i := range program.Resources {
		program.Resources[i].Mutable = true
	}
	vm := New(program, nil)
	saved := captureStateTest(t, vm)
	bank := make([]byte, stateBankLimit)
	for i := range saved.Resources {
		saved.Resources[i].Data = ByteSliceState{Buffer: 1, Length: len(bank)}
	}
	saved.Buffers = [][]byte{bank}
	if _, _, err := RestoreState(program, nil, saved); err == nil {
		t.Fatal("resource sum over 16 MiB accepted through shared buffers")
	}
	for i := range vm.Resources {
		vm.Resources[i].Data = bank
	}
	if _, err := vm.CaptureState(nil); err == nil {
		t.Fatal("oversized live resource sum captured")
	}
	vm = New(program, nil)
	saved = captureStateTest(t, vm)
	for i := range saved.Resources {
		saved.Resources[i].Data = ByteSliceState{Buffer: i + 1, Length: 1}
		saved.Buffers = append(saved.Buffers, make([]byte, stateProgramLimit))
	}
	if _, _, err := RestoreState(program, nil, saved); err == nil {
		t.Fatal("oversized retained buffers accepted")
	}
	if _, err := vm.CaptureState(make([][]byte, stateExternalLimit+1)); err == nil {
		t.Fatal("too many queued views captured")
	}
	if _, err := vm.CaptureState([][]byte{make([]byte, stateBankLimit+1)}); err == nil {
		t.Fatal("oversized queued view captured")
	}
}
