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
	waiter := vm.threadState(current)
	waiter.mu.Lock()
	interrupted := waiter.interrupted
	if interrupted {
		waiter.interrupted = false
		select {
		case <-waiter.wake:
		default:
		}
	}
	waiter.mu.Unlock()
	if interrupted {
		return VoidValue(), guestException("java/lang/InterruptedException", "thread interrupted")
	}
	select {
	case <-state.done:
		return VoidValue(), nil
	case <-vm.closed:
		return VoidValue(), ErrClosed
	case <-waiter.wake:
		waiter.mu.Lock()
		waiter.interrupted = false
		waiter.mu.Unlock()
		return VoidValue(), guestException("java/lang/InterruptedException", "thread interrupted")
	}
}
