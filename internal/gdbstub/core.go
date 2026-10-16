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

	stops  chan armcore.DebugStop
	thread atomic.Pointer[armcore.Thread]

	mu sync.Mutex
	// wakeup is the parked guest's own wakeup, non-nil only while one is
	// parked. It belongs to that one park rather than to the target, so a
	// release that arrives before the guest reaches its receive still lands:
	// the channel exists from the moment the park is visible, and closing it
	// wakes a guest that is already waiting and one that is about to.
	wakeup chan struct{}
	// abandon wakes a resume waiting for a stop when its client leaves, so
	// the connection that asked can end instead of waiting for a stop that
	// may never come.
	abandon chan struct{}
	// breakpoints are the ones this target installed, which a client that
	// leaves takes with it.
	breakpoints map[uint32]struct{}
	// idle is set when a client leaves and cleared when one asks for a stop
	// again. A stop the guest reaches meanwhile — one already decided when
	// the breakpoints were cleared — does not park, because nobody is left
	// to release it.
	idle     bool
	finished bool
	killed   bool
}

// NewCoreTarget attaches a debugger to a core. Attaching slows execution to
// one instruction per quantum; see armcore.AttachDebugger.
func NewCoreTarget(core *armcore.Core) *CoreTarget {
	target := &CoreTarget{
		core:  core,
		stops: make(chan armcore.DebugStop, 1),
	}
	core.AttachDebugger(target.hook)
	return target
}

// Detach removes the debugger and releases a parked guest, so the game keeps
// running once nothing is debugging it.
func (target *CoreTarget) Detach() {
	target.mu.Lock()
	target.idle = true
	target.mu.Unlock()
	target.core.AttachDebugger(nil)
	target.release()
}

// endClient takes away the breakpoints a client set and lets a parked guest
// go, so a client that detaches or drops its connection leaves the game
// running rather than frozen, as qAttached promises. A resume still waiting
// for a stop gives up, clearing any step it had asked for. The debugger stays
// attached for the next client. Serve calls it however a connection ends.
func (target *CoreTarget) endClient() {
	target.mu.Lock()
	breakpoints := target.breakpoints
	target.breakpoints = nil
	target.idle = true
	abandon := target.abandon
	target.abandon = nil
	target.mu.Unlock()
	for address := range breakpoints {
		target.core.ClearBreakpoint(address)
	}
	if abandon != nil {
		close(abandon)
	}
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
	if target.idle {
		target.mu.Unlock()
		return nil
	}
	wakeup := make(chan struct{})
	target.wakeup = wakeup
	target.thread.Store(thread)
	// Published under the same lock as the wakeup, so a resume that finds a
	// stop waiting can tell whether the guest is still parked at it.
	select {
	case target.stops <- stop:
	default:
		// A stop nobody collected means the client is not listening; the
		// guest still parks, because stopping is what a breakpoint means.
	}
	target.mu.Unlock()
	<-wakeup

	target.mu.Lock()
	if target.wakeup == wakeup {
		target.wakeup = nil
	}
	killed := target.killed
	target.mu.Unlock()
	if killed {
		return ErrKilled
	}
	return nil
}

// release lets a parked guest continue. It never blocks: a release with
// nothing parked is what an early continue is, and dropping it is correct.
// Taking the wakeup out of the target before closing it is what makes a
// second release a no-op rather than a panic, and what keeps a stale one
// from waking the next park.
func (target *CoreTarget) release() {
	target.mu.Lock()
	wakeup := target.wakeup
	target.wakeup = nil
	target.mu.Unlock()
	if wakeup == nil {
		return
	}
	close(wakeup)
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

// cpsrFlags are N, Z, C, V and Q, and cpsrThumb is T: the parts of the status
// word a client may change. The mode stays user, the only one this core runs.
const (
	cpsrFlags = uint32(0xf8000000)
	cpsrThumb = uint32(1 << 5)
)

func (target *CoreTarget) SetRegister(index int, value uint32) error {
	thread := target.thread.Load()
	if thread == nil {
		return errors.New("no stopped thread")
	}
	if index == armcore.RegisterPC {
		// gdb sends the PC as a plain address and the instruction set in
		// CPSR, the way 'g' reports them, where the thread reads the set
		// from bit zero. So the bit is supplied: a Thumb thread stays in
		// Thumb, and an address only Thumb can execute moves an ARM thread
		// there. A CPSR write that follows switches the set when it asks to.
		current := thread.Context()
		if current.Thumb() || value&3 != 0 {
			value |= 1
		}
	}
	return thread.SetRegister(index, value)
}

// SetCPSR applies the condition flags, and the instruction set where the PC
// is valid in the one asked for — which is how gdb moves a thread between
// them. Anything it cannot apply is dropped rather than refused: reporting an
// error would make gdb abandon a whole register-file write over a field it
// sends unconditionally.
func (target *CoreTarget) SetCPSR(value uint32) {
	thread := target.thread.Load()
	if thread == nil {
		return
	}
	next := thread.Context()
	next.CPSR = next.CPSR&^cpsrFlags | value&cpsrFlags
	if value&cpsrThumb != 0 {
		next.CPSR |= cpsrThumb
	} else if next.PC()&3 == 0 {
		next.CPSR &^= cpsrThumb
	}
	_ = thread.SetContext(next)
}

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

func (target *CoreTarget) SetBreakpoint(address uint32) {
	target.mu.Lock()
	if target.breakpoints == nil {
		target.breakpoints = make(map[uint32]struct{})
	}
	target.breakpoints[address&^1] = struct{}{}
	target.idle = false
	target.mu.Unlock()
	target.core.SetBreakpoint(address)
}

func (target *CoreTarget) ClearBreakpoint(address uint32) {
	target.mu.Lock()
	delete(target.breakpoints, address&^1)
	target.mu.Unlock()
	target.core.ClearBreakpoint(address)
}

// Continue releases the guest and waits for the next stop.
func (target *CoreTarget) Continue() (Signal, error) {
	return target.resume(false, nil)
}

// Step runs exactly one instruction.
func (target *CoreTarget) Step() (Signal, error) {
	return target.resume(true, nil)
}

// resume lets the guest go and waits for its next stop. A receive on
// interrupt is the client's Ctrl-C, which stops the guest before its next
// instruction. Every change to the core's pending step happens here, on the
// resume's own goroutine, so an interrupt can neither be cleared before it
// takes effect nor outlive the stop that answers it.
func (target *CoreTarget) resume(single bool, interrupt <-chan struct{}) (Signal, error) {
	target.mu.Lock()
	if target.finished {
		target.mu.Unlock()
		return SignalKill, nil
	}
	target.idle = false
	select {
	case <-target.stops:
		if target.wakeup != nil {
			// The guest stopped while nobody was waiting: it reached a
			// breakpoint after the client set it and before it asked to
			// continue. That stop is the one this resume would have run to,
			// so it is the answer, and the guest stays where it stopped.
			// Releasing it here would report a stop the guest has already
			// left and keep the client one stop behind from then on.
			target.mu.Unlock()
			return SignalTrap, nil
		}
		// A stop whose park has already been let go is stale, and dropped.
	default:
	}
	abandon := make(chan struct{})
	target.abandon = abandon
	target.mu.Unlock()
	if single {
		target.core.StepOnce()
	}
	target.release()
	for stopped := false; !stopped; {
		select {
		case <-target.stops:
			stopped = true
		case <-interrupt:
			interrupt = nil
			target.core.StepOnce()
		case <-abandon:
			target.core.CancelStep()
			return SignalKill, ErrDetached
		}
	}
	// An interrupt a breakpoint beat to the stop must not stop the next
	// continue one instruction in.
	target.core.CancelStep()
	target.mu.Lock()
	if target.abandon == abandon {
		target.abandon = nil
	}
	finished := target.finished
	target.mu.Unlock()
	if finished {
		return SignalKill, nil
	}
	return SignalTrap, nil
}

// Kill ends the guest run at its next opportunity.
func (target *CoreTarget) Kill() {
	target.mu.Lock()
	target.killed = true
	target.mu.Unlock()
	target.release()
}
