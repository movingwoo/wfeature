package jvm

import "errors"

var ErrClosed = errors.New("JVM closed")

// Close releases thread joins and sleeps. It does not wait for arbitrary native
// methods supplied by the Host to return.
func (vm *VM) Close() {
	if vm != nil {
		vm.closeOnce.Do(func() { close(vm.closed) })
	}
}

func threadJoin(vm *VM, execution *execution, arguments []Value) (Value, error) {
	thread, err := nativeReference(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	state := vm.threadState(thread)
	state.mu.Lock()
	alive := state.alive
	state.mu.Unlock()
	if !alive {
		return VoidValue(), nil
	}
	if vm.config.GuestThreadStarter != nil {
		return VoidValue(), errors.New("Thread.join is unsupported by the cooperative scheduler")
	}
	current := execution.thread
	if current == nil {
		current = vm.mainThreadObject()
	}
	if vm.consumeThreadInterrupt(current) {
		return VoidValue(), guestException("java/lang/InterruptedException", "thread interrupted")
	}
	return VoidValue(), vm.waitCheckpointJoin(execution, thread)
}

func (vm *VM) consumeThreadInterrupt(thread *Object) bool {
	state := vm.threadState(thread)
	state.mu.Lock()
	defer state.mu.Unlock()
	interrupted := state.interrupted
	if interrupted {
		state.interrupted = false
		select {
		case <-state.wake:
		default:
		}
	}
	return interrupted
}

func (vm *VM) waitCheckpointJoin(execution *execution, thread *Object) error {
	execution.wait = &bytecodeThreadWait{kind: "join", object: thread}
	defer func() { execution.wait = nil }()
	current := execution.thread
	if current == nil {
		current = vm.mainThreadObject()
	}
	waiter, target := vm.threadState(current), vm.threadState(thread)
	for {
		vm.threadMu.Lock()
		wake := vm.checkpointWake
		vm.threadMu.Unlock()
		if vm.checkpointPause.Load() != nil && execution.thread != nil {
			if err := vm.parkThread(execution); err != nil {
				return err
			}
			continue
		}
		select {
		case <-target.done:
			return nil
		case <-vm.closed:
			return ErrClosed
		case <-waiter.wake:
			vm.consumeThreadInterrupt(current)
			return guestException("java/lang/InterruptedException", "thread interrupted")
		case <-wake:
		}
	}
}
