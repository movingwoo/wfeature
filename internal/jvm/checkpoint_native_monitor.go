package jvm

import "fmt"

// A synchronized native blocked at entry has performed no method-body work.
// Save its caller immediately before invoke, putting the already-popped
// arguments back on the logical stack. The restored interpreter then acquires
// the monitor and invokes the body once, including its ordinary return checks.
func (vm *VM) captureNativeMonitorEntry(call *execution) (BytecodeExecutionState, []*Object, error) {
	saved, roots, err := (&Invocation{vm: vm, state: call}).CaptureBytecodeState(0)
	if err != nil {
		return saved, nil, err
	}
	if saved.Steps == 0 || len(saved.Frames) == 0 || len(call.wait.arguments) == 0 {
		return saved, nil, fmt.Errorf("JVM checkpoint native monitor entry has no caller or arguments")
	}
	f := &saved.Frames[len(saved.Frames)-1]
	code := call.topFrame.code.Bytecode
	if f.InvokePC < 0 || f.InvokePC >= len(code) || code[f.InvokePC] != 0xb6 && code[f.InvokePC] != 0xb7 && code[f.InvokePC] != 0xb9 {
		return saved, nil, fmt.Errorf("JVM checkpoint native monitor entry is not an instance invocation")
	}
	f.PC = f.InvokePC
	for _, value := range call.wait.arguments {
		record := HeapValueState{Kind: value.kind, Bits: value.bits}
		if value.ref != nil {
			roots = append(roots, value.ref)
			record.Reference = uint32(len(roots))
		}
		f.Stack = append(f.Stack, record)
	}
	saved.Steps--
	saved.RootCount = len(roots)
	if _, err = vm.prepareBytecodeState(saved, roots); err != nil {
		return saved, nil, err
	}
	return saved, roots, nil
}
