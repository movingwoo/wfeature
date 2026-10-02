package jvm

import (
	_ "embed"
	"encoding/json"
	"testing"
)

//go:embed testdata/NativeCheckpointProbe.class
var nativeCheckpointClass []byte

func TestNativeExecutionStatePreservesBudgetAndIdentity(t *testing.T) {
	options := Options{MaxSteps: 1000}
	vm := New(arithmeticSource(), options)
	var saved NativeExecutionState
	if err := vm.RegisterContextNative("test/NativeCheckpoint", "capture", "()I", func(call *Invocation, _ []Value) (Value, error) {
		if _, err := call.InvokeStatic("Arithmetic", "sumTwice", "(I)I", IntValue(3)); err != nil {
			return VoidValue(), err
		}
		var err error
		saved, err = call.CaptureNativeState(1)
		return IntValue(42), err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeStatic("test/NativeCheckpoint", "capture", "()I"); err != nil {
		t.Fatal(err)
	}
	if saved.Steps == 0 || saved.ID == 0 {
		t.Fatalf("captured execution = %+v", saved)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded NativeExecutionState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	fresh := New(arithmeticSource(), options)
	beforeValidation := fresh.nextExecution.Load()
	if err := fresh.ValidateNativeExecutionState(decoded); err != nil || fresh.nextExecution.Load() != beforeValidation {
		t.Fatalf("detached execution validation changed ownership: %v", err)
	}
	value, err := fresh.ResumeNativeExecution(decoded, func(call *Invocation) (Value, error) {
		before, err := call.CaptureNativeState(0)
		if err != nil || before != saved {
			t.Fatalf("resumed execution = %+v, %v", before, err)
		}
		result, err := call.InvokeStatic("Arithmetic", "sumTwice", "(I)I", IntValue(3))
		after, captureErr := call.CaptureNativeState(0)
		if captureErr != nil || after.ID != before.ID || after.Steps <= before.Steps {
			t.Fatalf("resumed budget = %+v, %v", after, captureErr)
		}
		return result, err
	})
	if got, _ := value.Int32(); err != nil || got != 6 {
		t.Fatalf("resumed answer = %d, %v", got, err)
	}
	for _, change := range []func(*NativeExecutionState){
		func(s *NativeExecutionState) { s.Version++ }, func(s *NativeExecutionState) { s.MaxSteps++ },
		func(s *NativeExecutionState) { s.ID = 0 }, func(s *NativeExecutionState) { s.Steps = s.MaxSteps + 1 },
		func(s *NativeExecutionState) { s.ID = ^uint64(0) - 1 },
		func(s *NativeExecutionState) { s.ThreadRuns = -1 }, func(s *NativeExecutionState) { s.ThreadRuns = 65 },
	} {
		bad := saved
		change(&bad)
		called := false
		if _, err := fresh.ResumeNativeExecution(bad, func(*Invocation) (Value, error) { called = true; return VoidValue(), nil }); err == nil || called {
			t.Fatal("invalid execution reached its continuation")
		}
	}
}

func TestNativeExecutionStateRefusesAnOuterBytecodeFrame(t *testing.T) {
	vm := New(mapClassSource{"NativeCheckpointProbe": nativeCheckpointClass}, Options{})
	refused := false
	if err := vm.RegisterContextNative("NativeCheckpointProbe", "checkpoint", "()I", func(call *Invocation, _ []Value) (Value, error) {
		_, err := call.CaptureNativeState(1)
		refused = err != nil
		return IntValue(42), nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeStatic("NativeCheckpointProbe", "run", "()I")
	if got, _ := result.Int32(); err != nil || got != 43 || !refused {
		t.Fatalf("bytecode source after refusal = %d, %v, refused=%v", got, err, refused)
	}
	for _, name := range []string{"before", "after"} {
		value, err := vm.StaticField("NativeCheckpointProbe", name, "I")
		if got, _ := value.Int32(); err != nil || got != 1 {
			t.Fatalf("%s = %d, %v", name, got, err)
		}
	}
}

func TestNativeExecutionStateCountsEnclosingNativeCallsAndUnwindsPanic(t *testing.T) {
	vm := New(nil, Options{})
	if err := vm.RegisterContextNative("test/NativeCheckpoint", "inner", "()V", func(call *Invocation, _ []Value) (Value, error) {
		if _, err := call.CaptureNativeState(1); err == nil {
			t.Fatal("enclosing native remainder was omitted")
		}
		panic("fixture native panic")
	}); err != nil {
		t.Fatal(err)
	}
	if err := vm.RegisterContextNative("test/NativeCheckpoint", "outer", "()V", func(call *Invocation, _ []Value) (Value, error) {
		func() {
			defer func() { _ = recover() }()
			_, _ = call.InvokeStatic("test/NativeCheckpoint", "inner", "()V")
		}()
		_, err := call.CaptureNativeState(1)
		return VoidValue(), err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeStatic("test/NativeCheckpoint", "outer", "()V"); err != nil {
		t.Fatal(err)
	}
}
