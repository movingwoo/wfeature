package jvm

import "fmt"

// Imported identifiers must leave room for new calls without wrapping to the
// reserved zero owner or reusing an old monitor owner.
const maxRestoredExecutionID = ^uint64(0) >> 1

// NativeExecutionState records ownership and the spent bytecode budget for an
// execution whose remaining work is represented by the platform. It excludes
// bytecode frames, initializers, JVM-scheduled threads, and unknown native Go
// remainders. ThreadRuns counts the core library's forwarding Thread.run bodies.
type NativeExecutionState struct {
	Version    uint32
	ID         uint64
	Steps      uint64
	MaxSteps   uint64
	MaxFrames  int
	ThreadRuns int
}

// CaptureNativeState requires a parked execution. representedNativeCalls is
// the number of active native bodies whose complete remainder the platform
// records separately. This is currently zero, or one known leaf wait.
func (call *Invocation) CaptureNativeState(representedNativeCalls int) (NativeExecutionState, error) {
	if call == nil || call.vm == nil || call.state == nil {
		return NativeExecutionState{}, fmt.Errorf("capture JVM execution without an invocation")
	}
	state := call.state
	if representedNativeCalls < 0 || representedNativeCalls > 1 || state.threadRuns < 0 || state.threadRuns > 64 || state.nativeDepth != representedNativeCalls+state.threadRuns || state.frames != 0 || len(state.initializing) != 0 || state.thread != nil {
		return NativeExecutionState{}, fmt.Errorf("JVM execution has unrepresented state: frames=%d initializers=%d thread=%t native_calls=%d thread_runs=%d represented=%d", state.frames, len(state.initializing), state.thread != nil, state.nativeDepth, state.threadRuns, representedNativeCalls)
	}
	if state.id == 0 || state.id > maxRestoredExecutionID || state.steps > call.vm.config.MaxSteps {
		return NativeExecutionState{}, fmt.Errorf("JVM execution has invalid ownership or step counters")
	}
	return NativeExecutionState{Version: 1, ID: state.id, Steps: state.steps, MaxSteps: call.vm.config.MaxSteps, MaxFrames: call.vm.config.MaxFrames, ThreadRuns: state.threadRuns}, nil
}

// ResumeNativeExecution installs the saved execution around a platform's
// continuation. The VM must be detached or parked; heap/monitor state is restored
// separately. The callback completes the represented native bodies without
// repeating their prefixes, then continues the platform execution.
func (vm *VM) ResumeNativeExecution(saved NativeExecutionState, complete func(*Invocation) (Value, error)) (Value, error) {
	if err := vm.ValidateNativeExecutionState(saved); err != nil {
		return VoidValue(), err
	}
	if complete == nil {
		return VoidValue(), fmt.Errorf("JVM native execution has no continuation")
	}
	for next := vm.nextExecution.Load(); next < saved.ID; next = vm.nextExecution.Load() {
		if vm.nextExecution.CompareAndSwap(next, saved.ID) {
			break
		}
	}
	state := &execution{id: saved.ID, steps: saved.Steps, nativeDepth: saved.ThreadRuns, threadRuns: saved.ThreadRuns, initializing: make(map[string]bool)}
	result, err := complete(&Invocation{vm: vm, state: state})
	if saved.ThreadRuns != 0 {
		if err == nil && result.kind != ValueVoid {
			return VoidValue(), fmt.Errorf("restored Thread.run target returned a non-void value")
		}
		return VoidValue(), err
	}
	return result, err
}

// ValidateNativeExecutionState checks a parked execution before its platform
// starts any restored goroutines. It does not reserve an ID or invoke code.
func (vm *VM) ValidateNativeExecutionState(saved NativeExecutionState) error {
	if vm == nil || saved.Version != 1 || saved.ID == 0 || saved.ID > maxRestoredExecutionID || saved.MaxSteps != vm.config.MaxSteps || saved.MaxFrames != vm.config.MaxFrames || saved.Steps > saved.MaxSteps || saved.ThreadRuns < 0 || saved.ThreadRuns > 64 {
		return fmt.Errorf("JVM native execution state or policy is incompatible")
	}
	return nil
}

func (vm *VM) callNative(state *execution, entry nativeEntry, arguments []Value) (Value, error) {
	state.nativeDepth++
	defer func() { state.nativeDepth-- }()
	if entry.context != nil {
		return entry.context(vm, state, arguments)
	}
	return entry.plain(vm, arguments)
}
