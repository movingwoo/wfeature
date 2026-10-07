package jvm

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

type monitor struct {
	mu      sync.Mutex
	owner   uint64
	depth   int
	signal  uint64
	changed chan struct{}
	waiters []*bytecodeThreadWait
}

func (m *monitor) changeLocked() {
	if m.changed != nil {
		close(m.changed)
	}
	m.changed = make(chan struct{})
}

// enterMonitor may park without acquiring the monitor. A bytecode caller
// represents the pending entry in its frame. Closing releases a blocked entry.
func (vm *VM) enterMonitor(state *execution, m *monitor) error {
	for {
		vm.threadMu.Lock()
		wake := vm.checkpointWake
		vm.threadMu.Unlock()
		if vm.checkpointPause.Load() != nil && state.thread != nil {
			if err := vm.parkThread(state); err != nil {
				return err
			}
			continue
		}
		m.mu.Lock()
		if m.owner == 0 || m.owner == state.id {
			m.owner, m.depth = state.id, m.depth+1
			m.mu.Unlock()
			return nil
		}
		if m.changed == nil {
			m.changed = make(chan struct{})
		}
		changed := m.changed
		m.mu.Unlock()
		select {
		case <-changed:
		case <-wake:
		case <-vm.closed:
			return ErrClosed
		}
	}
}

func (m *monitor) notify(owner uint64, all bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owner != owner || m.depth == 0 {
		return fmt.Errorf("monitor is not owned by execution %d", owner)
	}
	m.signal++
	for len(m.waiters) > 0 {
		wait := m.waiters[0]
		m.waiters = slices.Delete(m.waiters, 0, 1)
		wait.notified = true
		close(wait.notification)
		if !all {
			break
		}
	}
	return nil
}

func (m *monitor) exit(owner uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owner != owner || m.depth == 0 {
		return fmt.Errorf("monitor is not owned by execution %d", owner)
	}
	m.depth--
	if m.depth == 0 {
		m.owner = 0
		m.changeLocked()
	}
	return nil
}

func (vm *VM) waitMonitor(state *execution, object *Object, timeout time.Duration) error {
	m := &object.monitor
	m.mu.Lock()
	if m.owner != state.id || m.depth == 0 {
		m.mu.Unlock()
		return guestException("java/lang/IllegalMonitorStateException", "wait requires monitor ownership")
	}
	thread := state.thread
	if thread == nil {
		thread = vm.mainThreadObject()
	}
	if vm.consumeThreadInterrupt(thread) {
		m.mu.Unlock()
		return guestException("java/lang/InterruptedException", "thread interrupted")
	}
	wait := &bytecodeThreadWait{kind: "wait", object: object, depth: m.depth,
		timed: timeout > 0, remaining: timeout, notification: make(chan struct{})}
	m.waiters = append(m.waiters, wait)
	m.owner, m.depth = 0, 0
	m.changeLocked()
	m.mu.Unlock()
	return vm.continueMonitorWait(state, wait)
}

func (vm *VM) continueMonitorWait(state *execution, wait *bytecodeThreadWait) error {
	m := &wait.object.monitor
	state.wait = wait
	defer func() { state.wait = nil }()
	thread := state.thread
	if thread == nil {
		thread = vm.mainThreadObject()
	}
	waiter := vm.threadState(thread)
	for !wait.reenter {
		vm.threadMu.Lock()
		wake := vm.checkpointWake
		vm.threadMu.Unlock()
		if vm.checkpointPause.Load() != nil && state.thread != nil {
			if err := vm.parkThread(state); err != nil {
				return err
			}
			continue
		}
		var timer *time.Timer
		var deadline <-chan time.Time
		started := time.Now()
		if wait.timed {
			timer = time.NewTimer(wait.remaining)
			deadline = timer.C
		}
		select {
		case <-wait.notification:
			wait.reenter = true
		case <-deadline:
			wait.reenter = true
		case <-waiter.wake:
			wait.interrupted, wait.reenter = true, true
			vm.consumeThreadInterrupt(thread)
		case <-wake:
		case <-vm.closed:
			if timer != nil {
				timer.Stop()
			}
			return ErrClosed
		}
		if timer != nil {
			timer.Stop()
			wait.remaining = max(wait.remaining-time.Since(started), 0)
		}
	}
	m.mu.Lock()
	m.waiters = slices.DeleteFunc(m.waiters, func(other *bytecodeThreadWait) bool { return other == wait })
	m.mu.Unlock()
	if err := vm.enterMonitor(state, m); err != nil {
		return err
	}
	m.mu.Lock()
	m.depth = wait.depth
	m.mu.Unlock()
	if wait.interrupted {
		return guestException("java/lang/InterruptedException", "thread interrupted")
	}
	return nil
}
