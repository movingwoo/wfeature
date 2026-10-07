package jvm

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/BytecodeCheckpointProbe.class
var bytecodeCheckpointClass []byte

type bytecodeTestCheckpoint struct {
	Execution BytecodeExecutionState
	Heap      HeapState
}

func bytecodeCheckpointVM(t *testing.T) *VM {
	t.Helper()
	return New(mapClassSource{"BytecodeCheckpointProbe": bytecodeCheckpointClass}, Options{})
}

func captureBytecodeTestCheckpoint(t *testing.T, call *Invocation) bytecodeTestCheckpoint {
	t.Helper()
	execution, roots, err := call.CaptureBytecodeState(1)
	if err != nil {
		t.Fatal(err)
	}
	heap, err := call.VM().CaptureHeapState(roots, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(bytecodeTestCheckpoint{execution, heap})
	if err != nil {
		t.Fatal(err)
	}
	var saved bytecodeTestCheckpoint
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestBytecodeCheckpointContinuesNestedCallsAndExceptions(t *testing.T) {
	for _, mode := range []int32{0, 1, 2} {
		t.Run(string(rune('0'+mode)), func(t *testing.T) {
			source := bytecodeCheckpointVM(t)
			var checkpoints []bytecodeTestCheckpoint
			answer := func(stage int32) (Value, error) {
				if mode == 2 && stage == 1 {
					return VoidValue(), guestException("java/lang/IllegalStateException", "fixture native continuation")
				}
				return LongValue(int64(stage) * 10), nil
			}
			if err := source.RegisterContextNative("BytecodeCheckpointProbe", "checkpoint", "(ILjava/lang/Object;)J", func(call *Invocation, args []Value) (Value, error) {
				checkpoints = append(checkpoints, captureBytecodeTestCheckpoint(t, call))
				stage, _ := args[0].Int32()
				return answer(stage)
			}); err != nil {
				t.Fatal(err)
			}
			want, err := source.InvokeStatic("BytecodeCheckpointProbe", "run", "(I)I", IntValue(mode))
			if err != nil || len(checkpoints) == 0 {
				t.Fatalf("source execution: %v, %d checkpoints", err, len(checkpoints))
			}
			for index, saved := range checkpoints {
				fresh := bytecodeCheckpointVM(t)
				if err := fresh.RegisterContextNative("BytecodeCheckpointProbe", "checkpoint", "(ILjava/lang/Object;)J", func(call *Invocation, args []Value) (Value, error) {
					// A resumed execution can be captured at its next native
					// boundary, not only before its first instruction.
					again := captureBytecodeTestCheckpoint(t, call)
					if !reflect.DeepEqual(again, checkpoints[1]) {
						t.Fatal("recaptured continuation differs from uninterrupted execution")
					}
					stage, _ := args[0].Int32()
					return answer(stage)
				}); err != nil {
					t.Fatal(err)
				}
				roots, err := fresh.RestoreHeapState(saved.Heap, HeapCodec{})
				if err != nil {
					t.Fatal(err)
				}
				before := fresh.nextExecution.Load()
				if err := fresh.ValidateBytecodeState(saved.Execution, roots); err != nil || fresh.nextExecution.Load() != before {
					t.Fatalf("detached validation: %v", err)
				}
				got, err := fresh.ResumeBytecodeExecution(saved.Execution, roots, func(call *Invocation) (Value, error) {
					again := captureBytecodeTestCheckpoint(t, call)
					if !reflect.DeepEqual(again, saved) {
						t.Fatal("recapture before resume differs from saved continuation")
					}
					return answer(int32(index + 1))
				})
				if err != nil || got != want {
					t.Fatalf("restored answer = %v, %v; want %v", got, err, want)
				}
				for _, name := range []string{"initialized", "before", "after", "caught"} {
					want, _ := source.StaticField("BytecodeCheckpointProbe", name, "I")
					got, err := fresh.StaticField("BytecodeCheckpointProbe", name, "I")
					if err != nil || got != want {
						t.Fatalf("restored %s = %v, %v; want %v", name, got, err, want)
					}
				}
			}
		})
	}
}

func TestBytecodeCheckpointRetainsThreadRunTargetAfterMutation(t *testing.T) {
	source := bytecodeCheckpointVM(t)
	target, err := source.NewObject("BytecodeCheckpointProbe", "()V")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := source.NewObject(ThreadClass, "(Ljava/lang/Runnable;)V", ReferenceValue(target))
	if err != nil {
		t.Fatal(err)
	}
	var saved bytecodeTestCheckpoint
	if err := source.RegisterContextNative("BytecodeCheckpointProbe", "checkpoint", "(ILjava/lang/Object;)J", func(call *Invocation, args []Value) (Value, error) {
		stage, _ := args[0].Int32()
		if stage == 1 {
			if err := source.SetField(thread, ThreadClass, "target", "Ljava/lang/Runnable;", ReferenceValue(nil)); err != nil {
				t.Fatal(err)
			}
			saved = captureBytecodeTestCheckpoint(t, call)
		}
		return LongValue(10), nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.InvokeVirtual(thread, "run", "()V"); err != nil {
		t.Fatal(err)
	}
	if saved.Execution.ThreadRuns != 1 {
		t.Fatalf("forwarding depth = %d", saved.Execution.ThreadRuns)
	}
	fresh := bytecodeCheckpointVM(t)
	if err := fresh.RegisterContextNative("BytecodeCheckpointProbe", "checkpoint", "(ILjava/lang/Object;)J", func(*Invocation, []Value) (Value, error) {
		return LongValue(10), nil
	}); err != nil {
		t.Fatal(err)
	}
	roots, err := fresh.RestoreHeapState(saved.Heap, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fresh.ResumeBytecodeExecution(saved.Execution, roots, func(*Invocation) (Value, error) { return LongValue(10), nil })
	if err != nil || result.kind != ValueVoid {
		t.Fatalf("restored Thread.run = %v, %v", result, err)
	}
	want, _ := source.StaticField("BytecodeCheckpointProbe", "result", "I")
	got, err := fresh.StaticField("BytecodeCheckpointProbe", "result", "I")
	if err != nil || got != want {
		t.Fatalf("restored target result = %v, %v; want %v", got, err, want)
	}
}

func TestBytecodeCheckpointRefusesMalformedFramesBeforeContinuation(t *testing.T) {
	source := bytecodeCheckpointVM(t)
	var saved bytecodeTestCheckpoint
	if err := source.RegisterContextNative("BytecodeCheckpointProbe", "checkpoint", "(ILjava/lang/Object;)J", func(call *Invocation, _ []Value) (Value, error) {
		if saved.Execution.Version == 0 {
			saved = captureBytecodeTestCheckpoint(t, call)
		}
		return LongValue(10), nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.InvokeStatic("BytecodeCheckpointProbe", "run", "(I)I", IntValue(0)); err != nil {
		t.Fatal(err)
	}
	changes := map[string]func(*BytecodeExecutionState){
		"version":     func(s *BytecodeExecutionState) { s.Version++ },
		"identity":    func(s *BytecodeExecutionState) { s.ID = 0 },
		"budget":      func(s *BytecodeExecutionState) { s.Steps = s.MaxSteps + 1 },
		"policy":      func(s *BytecodeExecutionState) { s.MaxFrames++ },
		"roots":       func(s *BytecodeExecutionState) { s.RootCount++ },
		"thread":      func(s *BytecodeExecutionState) { s.Thread = uint32(s.RootCount + 1) },
		"empty":       func(s *BytecodeExecutionState) { s.Frames = nil },
		"class":       func(s *BytecodeExecutionState) { s.Frames[0].Class = "missing/Class" },
		"method":      func(s *BytecodeExecutionState) { s.Frames[0].Method = "missing" },
		"initializer": func(s *BytecodeExecutionState) { s.Frames[0].Method = "<clinit>" },
		"pc":          func(s *BytecodeExecutionState) { s.Frames[0].PC = -1 },
		"operand pc":  func(s *BytecodeExecutionState) { s.Frames[0].PC = s.Frames[0].InvokePC + 1 },
		"pending":     func(s *BytecodeExecutionState) { s.Frames[0].InvokePC = s.Frames[0].PC },
		"locals":      func(s *BytecodeExecutionState) { s.Frames[0].Locals = nil },
		"value":       func(s *BytecodeExecutionState) { s.Frames[0].Locals[0].Kind = 255 },
		"reference": func(s *BytecodeExecutionState) {
			s.Frames[0].Locals[0] = HeapValueState{Kind: ValueReference, Reference: uint32(s.RootCount + 1)}
		},
		"bits":          func(s *BytecodeExecutionState) { s.Frames[0].Locals[0] = HeapValueState{Kind: ValueInt, Bits: 1 << 32} },
		"operand":       func(s *BytecodeExecutionState) { s.Frames[0].Stack = []HeapValueState{{Kind: ValueVoid}} },
		"operand limit": func(s *BytecodeExecutionState) { s.Frames[0].Stack = make([]HeapValueState, 65536) },
		"monitor":       func(s *BytecodeExecutionState) { s.Frames[0].SynchronizedReceiver = 1 },
		"forwarding":    func(s *BytecodeExecutionState) { s.ThreadRuns = 65 },
		"native tail":   func(s *BytecodeExecutionState) { s.NativeTail = false },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(saved.Execution)
			var bad BytecodeExecutionState
			if err := json.Unmarshal(data, &bad); err != nil {
				t.Fatal(err)
			}
			change(&bad)
			fresh := bytecodeCheckpointVM(t)
			roots, err := fresh.RestoreHeapState(saved.Heap, HeapCodec{})
			if err != nil {
				t.Fatal(err)
			}
			before := fresh.nextExecution.Load()
			called := false
			_, err = fresh.ResumeBytecodeExecution(bad, roots, func(*Invocation) (Value, error) { called = true; return LongValue(10), nil })
			if err == nil || called || fresh.nextExecution.Load() != before {
				t.Fatalf("malformed record reached continuation: %v", err)
			}
		})
	}
}

func TestBytecodeCheckpointRefusesEnclosingNativeWithoutChangingSource(t *testing.T) {
	machine := bytecodeCheckpointVM(t)
	refused := false
	if err := machine.RegisterContextNative("BytecodeCheckpointProbe", "checkpoint", "(ILjava/lang/Object;)J", func(call *Invocation, _ []Value) (Value, error) {
		_, _, err := call.CaptureBytecodeState(1)
		refused = err != nil && strings.Contains(err.Error(), "native")
		return LongValue(10), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := machine.RegisterContextNative("test/Outer", "call", "()I", func(call *Invocation, _ []Value) (Value, error) {
		return call.InvokeStatic("BytecodeCheckpointProbe", "run", "(I)I", IntValue(0))
	}); err != nil {
		t.Fatal(err)
	}
	result, err := machine.InvokeStatic("test/Outer", "call", "()I")
	if got, _ := result.Int32(); err != nil || got != 74 || !refused {
		t.Fatalf("source after refusal = %v, %v; refused=%t", result, err, refused)
	}
}
