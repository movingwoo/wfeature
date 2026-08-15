package jvm

import (
	"fmt"
	"sync"
	"time"
)

type monitor struct {
	mu     sync.Mutex
	cond   *sync.Cond
	owner  uint64
	depth  int
	signal uint64
}

func (m *monitor) wait(owner uint64, timeout time.Duration) error {
	m.mu.Lock()
	if m.cond == nil {
		m.cond = sync.NewCond(&m.mu)
	}
	if m.owner != owner || m.depth == 0 {
		m.mu.Unlock()
		return fmt.Errorf("monitor is not owned by execution %d", owner)
	}
	depth := m.depth
	signal := m.signal
	m.owner = 0
	m.depth = 0
	m.cond.Broadcast()
	timedOut := false
	var timer *time.Timer
	if timeout > 0 {
		timer = time.AfterFunc(timeout, func() {
			m.mu.Lock()
			timedOut = true
			m.cond.Broadcast()
			m.mu.Unlock()
		})
	}
	for m.signal == signal && !timedOut {
		m.cond.Wait()
	}
	if timer != nil {
		timer.Stop()
	}
	for m.owner != 0 && m.owner != owner {
		m.cond.Wait()
	}
	m.owner = owner
	m.depth = depth
	m.mu.Unlock()
	return nil
}

func (m *monitor) notify(owner uint64, all bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cond == nil {
		m.cond = sync.NewCond(&m.mu)
	}
	if m.owner != owner || m.depth == 0 {
		return fmt.Errorf("monitor is not owned by execution %d", owner)
	}
	m.signal++
	if all {
		m.cond.Broadcast()
	} else {
		m.cond.Signal()
	}
	return nil
}

func (m *monitor) enter(owner uint64) {
	m.mu.Lock()
	if m.cond == nil {
		m.cond = sync.NewCond(&m.mu)
	}
	for m.owner != 0 && m.owner != owner {
		m.cond.Wait()
	}
	m.owner = owner
	m.depth++
	m.mu.Unlock()
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
		m.cond.Broadcast()
	}
	return nil
}
