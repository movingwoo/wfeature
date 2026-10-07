package jvm

import (
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

// CheckpointNativeWait represents the remainder of a native call that has
// already started its operation. A token's Native payload implements this
// interface and travels through HeapCodec with its explicit object references.
// Neither a Go callback nor a wait channel is part of the saved record.
type CheckpointNativeWait interface {
	// Validation runs before platform reference fixups. Check only scalar token
	// state and the native method identity; the platform validates linked state
	// after restoring all payloads and before starting the prepared workers.
	ValidateCheckpointWait(className, name, descriptor string, timed bool, remaining time.Duration) error
	// Resume completes the wait and native cleanup without repeating the
	// operation that preceded it. Guest exceptions unwind at the saved invoke.
	ResumeCheckpointWait(call *Invocation, timed bool, remaining time.Duration) (Value, error)
}

// WaitCheckpointAsGuestThread is a checkpointable platform wait. Host calls
// return false immediately. Guest interrupts finish the wait without clearing
// the interrupt flag, matching WaitAsGuestThread. A timed zero completes now;
// an untimed wait ends only on until, interruption, or VM closure. Time spent
// at a checkpoint barrier does not consume the remaining duration.
func (call *Invocation) WaitCheckpointAsGuestThread(token *Object, timed bool, remaining time.Duration, until <-chan struct{}) (bool, error) {
	if call == nil || call.state == nil || call.state.thread == nil {
		return false, nil
	}
	if call.vm == nil || token == nil || remaining < 0 || !timed && remaining != 0 {
		return false, fmt.Errorf("JVM native checkpoint wait has an invalid token or duration")
	}
	if _, ok := token.Native.(CheckpointNativeWait); !ok {
		return false, fmt.Errorf("JVM native checkpoint wait token has no continuation")
	}
	vm, execution := call.vm, call.state
	state := vm.threadState(execution.thread)
	wait := &bytecodeThreadWait{kind: "native", object: token, timed: timed, remaining: remaining}
	execution.wait = wait
	defer func() { execution.wait = nil }()
	for {
		vm.threadMu.Lock()
		wake := vm.checkpointWake
		vm.threadMu.Unlock()
		if vm.checkpointPause.Load() != nil {
			if err := vm.parkThread(execution); err != nil {
				return true, err
			}
			continue
		}
		var timer *time.Timer
		var expired <-chan time.Time
		var deadline time.Time
		if timed {
			deadline = time.Now().Add(wait.remaining)
			timer = time.NewTimer(wait.remaining)
			expired = timer.C
		}
		checkpoint := false
		var err error
		select {
		case <-expired:
		case <-until:
		case <-state.wake:
		case <-vm.closed:
			err = ErrClosed
		case <-wake:
			checkpoint = true
		}
		if timer != nil {
			timer.Stop()
		}
		if !checkpoint {
			return true, err
		}
		if timed {
			wait.remaining = max(deadline.Sub(time.Now()), 0)
		}
		if err := vm.parkThread(execution); err != nil {
			return true, err
		}
	}
}

// Resolve a native wait's declared call site without executing it. Virtual
// dispatch may name an abstract interface instead of the native implementation;
// in that case the token must explicitly accept that declared type.
func (vm *VM) checkpointNativeWaitMethod(saved BytecodeExecutionState) (classfile.Reference, error) {
	invalid := func() (classfile.Reference, error) {
		return classfile.Reference{}, fmt.Errorf("JVM native checkpoint wait has an invalid invocation")
	}
	if len(saved.Frames) == 0 {
		return invalid()
	}
	leaf := saved.Frames[len(saved.Frames)-1]
	class, err := vm.loader.Load(leaf.Class)
	if err != nil {
		return classfile.Reference{}, err
	}
	method := class.FindMethod(leaf.Method, leaf.Descriptor)
	if method == nil || method.CodeAttribute() == nil {
		return invalid()
	}
	code, pc := method.CodeAttribute().Bytecode, leaf.InvokePC
	if pc < 0 || pc > len(code)-3 || code[pc] < 0xb6 || code[pc] > 0xb9 {
		return invalid()
	}
	reference, err := class.ConstantPool.ReferenceAt(uint16(code[pc+1])<<8 | uint16(code[pc+2]))
	if err != nil || reference.Kind != classfile.MethodReference && reference.Kind != classfile.InterfaceMethodReference {
		return invalid()
	}
	if _, err := ParseMethodDescriptor(reference.Descriptor); err != nil {
		return invalid()
	}
	if code[pc] == 0xb9 && (pc > len(code)-5 || reference.Kind != classfile.InterfaceMethodReference || code[pc+3] == 0 || code[pc+4] != 0) {
		return invalid()
	}
	if owner := vm.checkpointNativeOwner(reference.Class, reference.Name, reference.Descriptor); owner != "" {
		reference.Class = owner
		return reference, nil
	}
	if code[pc] != 0xb6 && code[pc] != 0xb9 {
		return invalid()
	}
	declared, err := vm.loader.Load(reference.Class)
	if err != nil {
		return classfile.Reference{}, err
	}
	method = declared.FindMethod(reference.Name, reference.Descriptor)
	if method == nil || method.CodeAttribute() != nil || method.AccessFlags&AccessStatic != 0 ||
		method.AccessFlags&(AccessAbstract|AccessNative) == 0 || code[pc] == 0xb9 && declared.AccessFlags&AccessInterface == 0 {
		return invalid()
	}
	return reference, nil
}

func (thread CheckpointThreadState) nativeWait(vm *VM, token *Object) (CheckpointNativeWait, error) {
	if token == nil {
		return nil, fmt.Errorf("JVM native checkpoint wait has no token")
	}
	wait, ok := token.Native.(CheckpointNativeWait)
	if !ok {
		return nil, fmt.Errorf("JVM native checkpoint wait token has no continuation")
	}
	reference, err := vm.checkpointNativeWaitMethod(thread.Execution)
	if err != nil {
		return nil, err
	}
	if err := wait.ValidateCheckpointWait(reference.Class, reference.Name, reference.Descriptor, thread.Timed, thread.Remaining); err != nil {
		return nil, err
	}
	return wait, nil
}
