package armcore

import (
	"context"
	"fmt"
)

// CallStop identifies the Host operation suspended at a captured boundary.
type CallStop uint8

const (
	CallStoppedAtSupervisor CallStop = iota + 1
	CallStoppedAtLimit
	callFrameVersion = 1
	maxCapturedCalls = 64
)

// CallFrame records one derived ARM call. Its pending supervisor operation is
// completed by the platform before execution resumes at Context.PC(). Memory,
// private thread-local words, and platform continuations are separate state.
type CallFrame struct {
	Version        uint32
	Context        Context
	EntryStack     uint32
	End            uint32
	Budget         uint64
	Steps          uint64
	Window         uint64
	Stop           CallStop
	SupervisorCall SupervisorCall
}

// callCheckpoint holds the Run locals needed at an existing park boundary.
// Only the running owner writes them, under Thread.mu. Capture does not stop
// execution: the platform must already own the complete parked call chain.
type callCheckpoint struct {
	steps      uint64
	window     uint64
	end        uint32
	supervisor SupervisorCall
	stop       CallStop
}

// CaptureCalls returns the active calls below parent, outermost first. All
// calls must be parked at a supervisor boundary or inside the limit hook.
// The caller must keep the logical thread parked throughout capture.
func (core *Core) CaptureCalls(parent *Thread) ([]CallFrame, error) {
	return core.InspectParkedCalls(parent, nil)
}

// InspectParkedCalls also lets the platform capture the continuation owned by
// each source thread. inspect runs without the thread lock, while the caller
// still owns the parked logical thread. It must not resume or mutate execution.
func (core *Core) InspectParkedCalls(parent *Thread, inspect func(*Thread, CallFrame) error) ([]CallFrame, error) {
	if core == nil || parent == nil {
		return nil, fmt.Errorf("capture ARM calls without a core or parent")
	}
	frames := make([]CallFrame, 0, 4)
	current := parent
	for {
		current.mu.Lock()
		count := len(current.calls)
		var child *Thread
		if count == 1 {
			child = current.calls[0]
		}
		current.mu.Unlock()
		if count == 0 {
			break
		}
		if count != 1 || len(frames) == maxCapturedCalls {
			return nil, fmt.Errorf("ARM call chain is branched or exceeds %d calls", maxCapturedCalls)
		}
		child.mu.Lock()
		control := child.checkpoint
		parked := child.entryKnown && ((child.state == ThreadSuspended && control.stop == CallStoppedAtSupervisor) ||
			(child.state == ThreadRunning && control.stop == CallStoppedAtLimit))
		frame := CallFrame{
			Version: callFrameVersion, Context: child.context, EntryStack: child.entryStack,
			End: control.end, Budget: core.callBudget(child), Steps: control.steps,
			Window: control.window, Stop: control.stop, SupervisorCall: control.supervisor,
		}
		child.mu.Unlock()
		if !parked {
			return nil, fmt.Errorf("ARM call is not parked at a supported boundary")
		}
		if err := frame.Validate(); err != nil {
			return nil, err
		}
		if inspect != nil {
			if err := inspect(child, frame); err != nil {
				return nil, err
			}
		}
		frames = append(frames, frame)
		current = child
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("ARM parent has no parked calls")
	}
	return frames, nil
}

// ResumeCall recreates one captured call under a fresh logical parent. For a
// supervisor stop, complete must perform only the saved operation's remainder;
// it may recursively resume a child call. It must not repeat the native prefix.
// A limit stop resumes after the grant, without invoking the old park twice.
// The platform restores memory and private TLS before calling this method.
func (core *Core) ResumeCall(ctx context.Context, parent *Thread, frame CallFrame, complete, handler SupervisorCallHandler) (RunSummary, error) {
	if core == nil || parent == nil {
		return RunSummary{}, fmt.Errorf("resume ARM call without a core or parent")
	}
	if err := frame.Validate(); err != nil {
		return RunSummary{}, err
	}
	if frame.Budget != core.callBudget(parent) {
		return RunSummary{}, fmt.Errorf("captured ARM instruction budget differs from parent")
	}
	if frame.Stop == CallStoppedAtSupervisor && complete == nil {
		return RunSummary{}, fmt.Errorf("captured ARM supervisor call has no continuation")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RunSummary{}, err
	}
	_, derived, err := parent.contextForCall()
	if err != nil {
		return RunSummary{}, err
	}
	derived.context = frame.Context
	derived.entryStack, derived.entryKnown = frame.EntryStack, true
	derived.checkpoint = callCheckpoint{
		steps: frame.Steps, window: frame.Window, end: frame.End,
		supervisor: frame.SupervisorCall, stop: frame.Stop,
	}
	if frame.Stop == CallStoppedAtSupervisor {
		derived.state = ThreadSuspended
	} else {
		derived.checkpoint.window = 0
		derived.checkpoint.stop = 0
	}
	parent.addCall(derived)
	defer parent.removeCall(derived)
	if frame.Stop == CallStoppedAtSupervisor {
		if err := complete(ctx, derived, frame.SupervisorCall); err != nil {
			result := derived.Context()
			derived.fault(result)
			return RunSummary{Steps: frame.Steps, Context: result}, fmt.Errorf("complete captured ARM supervisor call: %w", err)
		}
		derived.mu.Lock()
		derived.state = ThreadReady
		derived.checkpoint.stop = 0
		derived.mu.Unlock()
	}
	return core.Run(ctx, derived, frame.End, handler)
}

func (core *Core) callBudget(thread *Thread) uint64 {
	if thread.stepBudget != 0 {
		return thread.stepBudget
	}
	return core.maxSteps
}

// Validate checks the record independently of its memory and platform state.
func (frame CallFrame) Validate() error {
	if frame.Version != callFrameVersion || frame.Budget == 0 || frame.Steps < frame.Window {
		return fmt.Errorf("invalid ARM call frame version or instruction counters")
	}
	context := frame.Context
	if context.CPSR&modeMask != modeUser || context.PC()&1 != 0 || (!context.Thumb() && context.PC()&3 != 0) {
		return fmt.Errorf("invalid ARM call frame context")
	}
	switch frame.Stop {
	case CallStoppedAtSupervisor:
		width := uint32(4)
		if context.Thumb() {
			width = 2
		}
		call := frame.SupervisorCall
		if frame.Window == 0 || frame.Window > frame.Budget || call.ResumePC != context.PC() ||
			call.Address > ^uint32(0)-width || call.Address+width != call.ResumePC {
			return fmt.Errorf("invalid ARM call frame supervisor boundary")
		}
	case CallStoppedAtLimit:
		if frame.Window < frame.Budget || frame.SupervisorCall != (SupervisorCall{}) {
			return fmt.Errorf("invalid ARM call frame instruction boundary")
		}
	default:
		return fmt.Errorf("unsupported ARM call frame stop %d", frame.Stop)
	}
	return nil
}

func (thread *Thread) suspendCall(context Context, window, steps uint64, call SupervisorCall) {
	thread.mu.Lock()
	thread.context, thread.state = context, ThreadSuspended
	thread.checkpoint.window, thread.checkpoint.steps = window, steps
	thread.checkpoint.supervisor, thread.checkpoint.stop = call, CallStoppedAtSupervisor
	thread.mu.Unlock()
}

func (thread *Thread) parkLimit(context Context, window, steps uint64) {
	thread.mu.Lock()
	thread.context = context
	thread.checkpoint.window, thread.checkpoint.steps = window, steps
	thread.checkpoint.supervisor, thread.checkpoint.stop = SupervisorCall{}, CallStoppedAtLimit
	thread.mu.Unlock()
}

func (thread *Thread) clearCallStop() {
	thread.mu.Lock()
	thread.checkpoint.stop = 0
	thread.mu.Unlock()
}
