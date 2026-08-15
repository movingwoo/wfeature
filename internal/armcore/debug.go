package armcore

import (
	"context"
	"sync"
)

// DebugStop is why execution paused at a debugger's request.
type DebugStop uint8

const (
	// DebugStopBreakpoint is a PC that matches an installed breakpoint.
	DebugStopBreakpoint DebugStop = iota
	// DebugStopStep is the instruction after a single-step request.
	DebugStopStep
)

// DebugHook is called with the guest stopped. It runs on the guest's own
// goroutine and may block — a debugger waiting for its client is exactly what
// should stop the cooperative core, since no other guest thread can make
// progress while this one holds its turn.
//
// The hook may read and write the thread's registers and the core's memory.
// Returning an error ends the run with it, which is how a client's "kill"
// stops the session.
type DebugHook func(ctx context.Context, core *Core, thread *Thread, stop DebugStop) error

// debugState is the core's debugger attachment. It is separate from the hot
// execution path: when nothing is attached, the run loop checks one atomic
// pointer and nothing else changes.
type debugState struct {
	mu          sync.RWMutex
	hook        DebugHook
	breakpoints map[uint32]struct{}
	stepping    bool
}

// AttachDebugger installs a debug hook. A nil hook detaches.
//
// **Attaching slows execution to one instruction per quantum**, because that
// is the only way to stop exactly at a breakpoint without adding a check to
// the interpreter's inner loop — where it would cost every game that is not
// being debugged. A debugger being slower than a run is the right trade.
func (core *Core) AttachDebugger(hook DebugHook) {
	core.debug.mu.Lock()
	core.debug.hook = hook
	if core.debug.breakpoints == nil {
		core.debug.breakpoints = make(map[uint32]struct{})
	}
	core.debug.mu.Unlock()
}

// SetBreakpoint installs a breakpoint at an instruction address. The address
// is masked to the instruction it belongs to, so a client that sets one on a
// Thumb address with the low bit set still stops there.
func (core *Core) SetBreakpoint(address uint32) {
	core.debug.mu.Lock()
	if core.debug.breakpoints == nil {
		core.debug.breakpoints = make(map[uint32]struct{})
	}
	core.debug.breakpoints[address&^1] = struct{}{}
	core.debug.mu.Unlock()
}

// ClearBreakpoint removes one.
func (core *Core) ClearBreakpoint(address uint32) {
	core.debug.mu.Lock()
	delete(core.debug.breakpoints, address&^1)
	core.debug.mu.Unlock()
}

// Breakpoints lists the installed addresses.
func (core *Core) Breakpoints() []uint32 {
	core.debug.mu.RLock()
	defer core.debug.mu.RUnlock()
	addresses := make([]uint32, 0, len(core.debug.breakpoints))
	for address := range core.debug.breakpoints {
		addresses = append(addresses, address)
	}
	return addresses
}

// StepOnce makes the next resume stop after one instruction.
func (core *Core) StepOnce() {
	core.debug.mu.Lock()
	core.debug.stepping = true
	core.debug.mu.Unlock()
}

// debugAttached reports whether the run loop has to check for stops.
func (core *Core) debugAttached() bool {
	core.debug.mu.RLock()
	defer core.debug.mu.RUnlock()
	return core.debug.hook != nil
}

// debugCheck decides whether execution should stop before the instruction at
// pc, and clears a pending single step.
func (core *Core) debugCheck(pc uint32) (DebugHook, DebugStop, bool) {
	core.debug.mu.Lock()
	defer core.debug.mu.Unlock()
	if core.debug.hook == nil {
		return nil, 0, false
	}
	if core.debug.stepping {
		core.debug.stepping = false
		return core.debug.hook, DebugStopStep, true
	}
	if _, ok := core.debug.breakpoints[pc&^1]; ok {
		return core.debug.hook, DebugStopBreakpoint, true
	}
	return nil, 0, false
}
