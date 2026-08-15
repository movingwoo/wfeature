package gdbstub

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// ErrKilled ends a guest run because the debugger's client asked for it.
var ErrKilled = errors.New("gdb client killed the target")

// CoreTarget debugs an ARM core.
//
// The hard part of debugging a cooperative core is where "stopped" lives. It
// lives on the guest's own goroutine: the debug hook parks there, holding the
// core's turn, so nothing else in the guest advances while a client is
// looking. The protocol loop runs on the connection's goroutine and the two
// hand off through channels — the hook publishes a stop, the loop releases it.
type CoreTarget struct {
	core *armcore.Core

	stops   chan armcore.DebugStop
	resumes chan struct{}
	thread  atomic.Pointer[armcore.Thread]

	mu       sync.Mutex
	parked   bool
	finished bool
	killed   bool
}

// NewCoreTarget attaches a debugger to a core. Attaching slows execution to
// one instruction per quantum; see armcore.AttachDebugger.
func NewCoreTarget(core *armcore.Core) *CoreTarget {
	target := &CoreTarget{
		core:    core,
		stops:   make(chan armcore.DebugStop, 1),
		resumes: make(chan struct{}),
	}
	core.AttachDebugger(target.hook)
	return target
}

// Detach removes the debugger and releases a parked guest, so a client that
// disconnects leaves the game running rather than frozen.
func (target *CoreTarget) Detach() {
	target.core.AttachDebugger(nil)
	target.release()
}

// Finished is called by whoever runs the guest when the run ends, so a client
// waiting for the next stop learns the target is gone instead of blocking
// forever.
func (target *CoreTarget) Finished() {
	target.mu.Lock()
	target.finished = true
	target.mu.Unlock()
	select {
	case target.stops <- armcore.DebugStopStep:
	default:
	}
}

// hook is the armcore.DebugHook: it parks the guest and waits.
func (target *CoreTarget) hook(_ context.Context, _ *armcore.Core, thread *armcore.Thread, stop armcore.DebugStop) error {
	target.mu.Lock()
	if target.killed {
		target.mu.Unlock()
		return ErrKilled
	}
	target.parked = true
	target.mu.Unlock()

	target.thread.Store(thread)
	select {
	case target.stops <- stop:
	default:
		// A stop nobody collected means the client is not listening; the
		// guest still parks, because stopping is what a breakpoint means.
	}
	<-target.resumes

	target.mu.Lock()
	target.parked = false
	killed := target.killed
	target.mu.Unlock()
	if killed {
		return ErrKilled
	}
	return nil
}

// release lets a parked guest continue. It never blocks: a release with
// nothing parked is what an early continue is, and dropping it is correct.
func (target *CoreTarget) release() {
	target.mu.Lock()
	parked := target.parked
	target.mu.Unlock()
	if !parked {
		return
	}
	select {
	case target.resumes <- struct{}{}:
	default:
	}
}

// context reads the parked guest's registers.
func (target *CoreTarget) context() armcore.Context {
	if thread := target.thread.Load(); thread != nil {
		return thread.Context()
	}
	return armcore.NewContext()
}

func (target *CoreTarget) Registers() ([16]uint32, uint32) {
	current := target.context()
	return current.Registers, current.CPSR
}

func (target *CoreTarget) SetRegister(index int, value uint32) error {
	thread := target.thread.Load()
	if thread == nil {
		return errors.New("no stopped thread")
	}
	return thread.SetRegister(index, value)
}

// SetCPSR is accepted and dropped: the thread exposes its registers but not
// its status word, and silently reporting success for a write that did not
// happen is better than making gdb abandon a whole register-file write over
// a field it sends unconditionally.
func (target *CoreTarget) SetCPSR(uint32) {}

func (target *CoreTarget) ReadMemory(address uint32, length int) ([]byte, error) {
	data := make([]byte, length)
	if err := target.core.Memory().Read(address, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (target *CoreTarget) WriteMemory(address uint32, data []byte) error {
	return target.core.Memory().Write(address, data)
}

func (target *CoreTarget) SetBreakpoint(address uint32)   { target.core.SetBreakpoint(address) }
func (target *CoreTarget) ClearBreakpoint(address uint32) { target.core.ClearBreakpoint(address) }

// Continue releases the guest and waits for the next stop.
func (target *CoreTarget) Continue() (Signal, error) {
	return target.resume(false)
}

// Step runs exactly one instruction.
func (target *CoreTarget) Step() (Signal, error) {
	return target.resume(true)
}

func (target *CoreTarget) resume(single bool) (Signal, error) {
	target.mu.Lock()
	if target.finished {
		target.mu.Unlock()
		return SignalKill, nil
	}
	target.mu.Unlock()
	if single {
		target.core.StepOnce()
	}
	target.release()
	stop, ok := <-target.stops
	if !ok {
		return SignalKill, nil
	}
	target.mu.Lock()
	finished := target.finished
	target.mu.Unlock()
	if finished {
		return SignalKill, nil
	}
	_ = stop
	return SignalTrap, nil
}

// Kill ends the guest run at its next opportunity.
func (target *CoreTarget) Kill() {
	target.mu.Lock()
	target.killed = true
	target.mu.Unlock()
	target.release()
}
