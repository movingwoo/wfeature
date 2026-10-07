package jvm

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

type bytecodeThreadWait struct {
	kind                                  string
	remaining                             time.Duration
	object                                *Object
	depth                                 int
	timed, notified, reenter, interrupted bool
	notification                          chan struct{}
	arguments                             []Value // Borrowed only while waiting to enter a native monitor.
}

// ParkedThreads owns a barrier across every JVM-owned guest thread. Host
// invocations and platform producers must already be excluded by the caller.
// Every successful PauseThreads must be paired with Resume, including when
// subsequent capture or detached preparation is refused.
type ParkedThreads struct {
	vm      *VM
	mu      sync.Mutex
	parked  map[*execution]bool
	changed chan struct{}
	resume  chan struct{}
	once    sync.Once
	threads []*Object
	calls   []*execution
}

// PauseThreads stops running bytecode at instruction boundaries and wakes
// sleeping threads into the same barrier. It runs no lifecycle callback.
// Unsupported native calls may not reach a boundary; cancellation releases
// every thread already parked and leaves the VM usable.
func (vm *VM) PauseThreads(ctx context.Context) (*ParkedThreads, error) {
	if vm == nil || vm.config.GuestThreadStarter != nil {
		return nil, fmt.Errorf("JVM checkpoint barrier requires JVM-owned threads")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !vm.checkpointMu.TryLock() {
		return nil, fmt.Errorf("JVM checkpoint barrier is already held")
	}
	paused := &ParkedThreads{vm: vm, parked: make(map[*execution]bool), changed: make(chan struct{}, 1), resume: make(chan struct{})}
	vm.threadMu.Lock()
	vm.checkpointPause.Store(paused)
	close(vm.checkpointWake)
	vm.threadMu.Unlock()
	for {
		vm.threadMu.Lock()
		paused.mu.Lock()
		ready := true
		for _, thread := range vm.threads {
			if thread.execution == nil || !paused.parked[thread.execution] {
				ready = false
				break
			}
		}
		if ready {
			for thread, state := range vm.threads {
				paused.threads = append(paused.threads, thread)
				paused.calls = append(paused.calls, state.execution)
			}
			slices.SortFunc(paused.calls, func(a, b *execution) int {
				if a.id < b.id {
					return -1
				}
				if a.id > b.id {
					return 1
				}
				return 0
			})
		}
		paused.mu.Unlock()
		vm.threadMu.Unlock()
		select {
		case <-ctx.Done():
			paused.Resume()
			return nil, ctx.Err()
		case <-vm.closed:
			paused.Resume()
			return nil, ErrClosed
		default:
		}
		if ready {
			return paused, nil
		}
		select {
		case <-paused.changed:
		case <-ctx.Done():
			paused.Resume()
			return nil, ctx.Err()
		case <-vm.closed:
			paused.Resume()
			return nil, ErrClosed
		}
	}
}

// Resume releases the barrier exactly once. Relative sleeps exclude the time
// spent parked, so encoding a record does not consume their remaining delay.
func (paused *ParkedThreads) Resume() {
	if paused == nil {
		return
	}
	paused.once.Do(func() {
		vm := paused.vm
		vm.threadMu.Lock()
		vm.checkpointWake = make(chan struct{})
		vm.checkpointPause.Store(nil)
		close(paused.resume)
		vm.threadMu.Unlock()
		vm.checkpointMu.Unlock()
	})
}

func (vm *VM) notifyThreadCheckpoint() {
	if paused := vm.checkpointPause.Load(); paused != nil {
		select {
		case paused.changed <- struct{}{}:
		default:
		}
	}
}

func (vm *VM) parkThread(state *execution) error {
	if state == nil || state.thread == nil {
		return nil
	}
	if paused := vm.checkpointPause.Load(); paused != nil {
		paused.mu.Lock()
		paused.parked[state] = true
		paused.mu.Unlock()
		vm.notifyThreadCheckpoint()
		select {
		case <-paused.resume:
		case <-vm.closed:
			return ErrClosed
		}
	}
	select {
	case <-vm.closed:
		return ErrClosed
	default:
		return nil
	}
}

func (vm *VM) sleepCheckpointThread(execution *execution, duration time.Duration) error {
	state := vm.threadState(execution.thread)
	state.mu.Lock()
	if state.interrupted {
		state.interrupted = false
		state.mu.Unlock()
		return guestException("java/lang/InterruptedException", "thread interrupted")
	}
	state.mu.Unlock()
	return vm.waitCheckpointSleep(execution, duration)
}

// A restored sleep resumes after its initial interruption check. A pending
// interrupt therefore follows the same channel branch as in the source.
func (vm *VM) waitCheckpointSleep(execution *execution, duration time.Duration) error {
	state := vm.threadState(execution.thread)
	wait := &bytecodeThreadWait{kind: "sleep", remaining: duration}
	execution.wait = wait
	defer func() { execution.wait = nil }()
	for {
		vm.threadMu.Lock()
		wake := vm.checkpointWake
		vm.threadMu.Unlock()
		if vm.checkpointPause.Load() != nil {
			if err := vm.parkThread(execution); err != nil {
				return err
			}
			continue
		}
		deadline := time.Now().Add(wait.remaining)
		timer := time.NewTimer(wait.remaining)
		select {
		case <-timer.C:
			return nil
		case <-vm.closed:
			timer.Stop()
			return ErrClosed
		case <-state.wake:
			timer.Stop()
			state.mu.Lock()
			state.interrupted = false
			state.mu.Unlock()
			return guestException("java/lang/InterruptedException", "thread interrupted")
		case <-wake:
			timer.Stop()
			wait.remaining = max(deadline.Sub(time.Now()), 0)
			if err := vm.parkThread(execution); err != nil {
				return err
			}
		}
	}
}
