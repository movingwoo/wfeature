package lgt

import (
	"context"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// What a parked guest thread still owes
//
// A guest thread is a goroutine parked inside a platform call, and a checkpoint
// cannot keep a goroutine. What it keeps is the guest half — the registers of
// every ARM call the thread is inside, which the core records — and, for each
// of those calls that is stopped at a platform call, the name of what that
// platform call had left to do when it parked. Restoring a thread is performing
// that remainder and then running the guest on from where it stopped.
//
// **Only a remainder this file names is restored.** A thread parked anywhere
// else is a thread whose platform call holds state in Go locals nothing here
// describes, so a checkpoint taken there is refused and the session carries on:
// the alternative is a record that loads and then answers the guest something
// the uninterrupted call would not have.
//
// The places a local title parks are few. Over the whole local library, every
// thread at every tick boundary was in one of `Thread.sleep`, `Thread.yield`,
// `Object.wait` or waiting for its first slice, one call deep. The two nested
// forms below are here because a slice can also end by its budget, and a budget
// can run out anywhere — including inside a static initialiser the thread's own
// code set off.
type javaRemainder uint8

const (
	javaRemainderNone javaRemainder = iota
	// The call has nothing left but its answer: `Thread.sleep` and
	// `Thread.yield`, which park and then return.
	javaRemainderResult
	// `Object.wait`, which gave the object's lock back before it parked and
	// has to take it again, at the depth it was held, before it returns.
	javaRemainderWait
	// A contended monitor enter, which parked to let the owner run and looks
	// again when its turn comes.
	javaRemainderMonitor
	// A class initialiser the platform entered on the thread's behalf. The
	// initialiser is the next call in the chain; what is left afterwards is
	// the answer.
	javaRemainderInitializer
	// The C library's "run this function" call, which is the same shape.
	javaRemainderFunction
)

// javaCallState is one ARM call a parked thread is inside, outermost first.
type javaCallState struct {
	Frame     armcore.CallFrame
	Remainder uint8
	// Object and Depth belong to a wait or a monitor enter: the object whose
	// lock is wanted and, for a wait, how many levels of it to take back.
	Object uint32
	Depth  int
}

type javaTryState struct {
	Buffer uint32
	Depth  int
	Saved  armcore.Context
	Armed  bool
}

// javaWorkerState is one started guest thread.
type javaWorkerState struct {
	Root         armcore.RootThreadState
	StackBase    uint32
	WakeAt       time.Duration
	Done         bool
	Yields       int
	Monitors     int
	Renewals     int
	Try          []javaTryState
	TryBuffers   []uint32
	CallDepth    int
	WaitSite     uint32
	Waits        int
	WaitReported bool
	// Calls is empty for a thread that has not had its first slice, which
	// enters `run` from the top, and for one that has finished.
	Calls []javaCallState
}

const (
	// maxJavaCapturedCalls bounds a chain. A thread nested deeper than this is
	// not parked somewhere a title parks.
	maxJavaCapturedCalls = 16
	// maxJavaMonitorDepth bounds how many levels of one lock a record may
	// claim, which is a count this platform then loops over nothing with but
	// stores in an int a guest reads back through `wait`.
	maxJavaMonitorDepth = 1 << 16
)

func captureJavaTry(frames []javaTryFrame) []javaTryState {
	saved := make([]javaTryState, 0, len(frames))
	for _, frame := range frames {
		saved = append(saved, javaTryState{Buffer: frame.Buffer, Depth: frame.Depth, Saved: frame.Saved, Armed: frame.Armed})
	}
	return saved
}

func restoreJavaTry(saved []javaTryState) []javaTryFrame {
	frames := make([]javaTryFrame, 0, len(saved))
	for _, frame := range saved {
		frames = append(frames, javaTryFrame{Buffer: frame.Buffer, Depth: frame.Depth, Saved: frame.Saved, Armed: frame.Armed})
	}
	return frames
}

// describeJavaFrame names the platform call a frame is stopped at, for the
// refusal that says why a checkpoint could not be taken.
func (client *Client) describeJavaFrame(frame armcore.CallFrame) string {
	if frame.Stop == armcore.CallStoppedAtLimit {
		return "its instruction budget"
	}
	slot := frame.Context.Registers[12]
	switch frame.SupervisorCall.Immediate {
	case svcCategoryJava:
		if index, static := javaStaticMethodParts(slot); static {
			if dispatch, ok := client.javaPlatformStaticDispatch(index); ok {
				return dispatch.Class + "." + dispatch.Called
			}
			return fmt.Sprintf("platform static method %d", index)
		}
		if slot&javaSlotVirtual != 0 {
			return client.describeJavaVirtualSlot(slot)
		}
		if name, known := javaSVCNames[slot]; known {
			return fmt.Sprintf("java interface function %#x (%s)", slot, name)
		}
		return fmt.Sprintf("java interface function %#x", slot)
	case svcCategoryWIPIC:
		return fmt.Sprintf("WIPI C slot %#x", slot)
	case svcCategoryStdlib:
		return fmt.Sprintf("stdlib slot %#x", slot)
	case svcCategoryOEM:
		return fmt.Sprintf("OEM slot %#x", slot)
	}
	return fmt.Sprintf("platform call category %d slot %#x", frame.SupervisorCall.Immediate, slot)
}

// javaFrameRemainder answers what the platform call a frame is stopped at has
// left to do. It reads the same dispatch tables the call itself was served
// from, so a record is classified the same way at capture and at restore: a
// slot that does not resolve to a call this file can finish is refused at both.
func (client *Client) javaFrameRemainder(frame armcore.CallFrame, leaf bool) (javaRemainder, error) {
	if frame.Stop == armcore.CallStoppedAtLimit {
		if !leaf {
			return 0, fmt.Errorf("LGT checkpoint has a guest call beneath an instruction boundary")
		}
		return javaRemainderNone, nil
	}
	slot := frame.Context.Registers[12]
	switch frame.SupervisorCall.Immediate {
	case svcCategoryJava:
		if index, static := javaStaticMethodParts(slot); static {
			if _, _, unnamed := client.javaLink.unnamedStaticEntry(index); !unnamed {
				if dispatch, ok := client.javaPlatformStaticDispatch(index); ok && leaf && dispatch.Method.Parks != javaRemainderNone {
					return dispatch.Method.Parks, nil
				}
			}
			break
		}
		if slot&javaSlotVirtual != 0 {
			if dispatch, ok := client.javaPlatformVirtualDispatch(slot); ok && leaf && dispatch.Method.Parks != javaRemainderNone {
				return dispatch.Method.Parks, nil
			}
			break
		}
		if slot == javaSVCMonitorEnter && leaf {
			return javaRemainderMonitor, nil
		}
		if slot == javaSVCInitializeClass && !leaf {
			return javaRemainderInitializer, nil
		}
	case svcCategoryStdlib:
		if slot == stdlibRunFunction && !leaf {
			return javaRemainderFunction, nil
		}
	}
	where := "parked inside"
	if !leaf {
		where = "running guest code for"
	}
	return 0, fmt.Errorf("LGT guest thread is %s %s, which a checkpoint cannot resume",
		where, client.describeJavaFrame(frame))
}

// captureJavaWorker records one thread. The caller holds every guest thread
// parked: this runs between whole rounds, where a slice is never in flight.
func (client *Client) captureJavaWorker(worker *javaWorker) (javaWorkerState, error) {
	if worker == nil || worker.armThread == nil {
		return javaWorkerState{}, fmt.Errorf("LGT checkpoint has a guest thread with no ARM thread")
	}
	saved := javaWorkerState{
		StackBase: worker.stackBase, WakeAt: worker.wakeAt, Done: worker.done,
		Yields: worker.yields, Monitors: worker.monitors, Renewals: worker.renewals,
		Try: captureJavaTry(worker.tryFrames), TryBuffers: append([]uint32(nil), worker.tryBuffers...),
		CallDepth: worker.callDepth, WaitSite: worker.waitSite, Waits: worker.waits,
		WaitReported: worker.waitReported,
	}
	var err error
	if saved.Root, err = client.core.CaptureRootThread(worker.armThread); err != nil {
		return javaWorkerState{}, err
	}
	if worker.done {
		return saved, nil
	}
	if worker.restored != nil {
		// Restored and not yet granted a slice: the continuation it was
		// restored with is still the whole of what it owes.
		saved.Calls = append([]javaCallState(nil), worker.restored...)
		return saved, nil
	}
	if len(worker.armThread.LiveContexts()) == 1 {
		// Started and not yet granted its first slice. It enters `run` from
		// the top, so there is no call to record.
		return saved, nil
	}
	frames, err := client.core.InspectParkedCalls(worker.armThread, nil)
	if err != nil {
		return javaWorkerState{}, err
	}
	for index, frame := range frames {
		leaf := index == len(frames)-1
		remainder, err := client.javaFrameRemainder(frame, leaf)
		if err != nil {
			return javaWorkerState{}, err
		}
		record := javaCallState{Frame: frame, Remainder: uint8(remainder)}
		switch remainder {
		case javaRemainderWait:
			if !worker.waiting.active {
				return javaWorkerState{}, fmt.Errorf("LGT guest thread is in a wait this platform did not record")
			}
			record.Object, record.Depth = worker.waiting.object, worker.waiting.depth
		case javaRemainderMonitor:
			record.Object = frame.Context.Registers[0]
		}
		saved.Calls = append(saved.Calls, record)
	}
	if leaf := saved.Calls[len(saved.Calls)-1]; javaRemainder(leaf.Remainder) != javaRemainderWait && worker.waiting.active {
		return javaWorkerState{}, fmt.Errorf("LGT guest thread recorded a wait it is not parked in")
	}
	return saved, nil
}

// validateJavaWorker checks one record against the restored runtime before any
// goroutine is started for it. It is also run at capture, so a thread that
// could not be restored is refused where the person can still see it.
func (client *Client) validateJavaWorker(saved javaWorkerState, stacks int) error {
	invalid := func(what string) error {
		return fmt.Errorf("LGT checkpoint guest thread has %s", what)
	}
	stackEnd := uint64(saved.StackBase) + javaThreadStackSize
	if saved.StackBase < javaThreadStackBase || (saved.StackBase-javaThreadStackBase)%uint32(javaThreadStackSize) != 0 ||
		uint64(saved.StackBase-javaThreadStackBase)/javaThreadStackSize >= uint64(stacks) {
		return invalid("a stack outside the ones this session mapped")
	}
	if err := client.core.Memory().ValidateRange(saved.StackBase, javaThreadStackSize, armcore.PermissionReadWrite); err != nil {
		return err
	}
	inStack := func(pointer uint32) bool {
		return pointer&3 == 0 && pointer >= saved.StackBase && uint64(pointer) <= stackEnd
	}
	if saved.Root.StepBudget != javaThreadSliceSteps || !inStack(saved.Root.Context.Registers[armcore.RegisterSP]) {
		return invalid("an incompatible budget or root stack pointer")
	}
	if saved.WakeAt < 0 || saved.Yields < 0 || saved.Yields > javaYieldBurst || saved.Monitors < 0 || saved.Monitors > maxJavaMonitorDepth ||
		saved.Renewals < 0 || saved.Renewals > maxJavaSliceRenewals || saved.Waits < 0 ||
		len(saved.Try) > maxJavaTryDepth || len(saved.TryBuffers) > maxJavaTryDepth || len(saved.Try) > len(saved.TryBuffers) ||
		len(saved.Calls) > maxJavaCapturedCalls {
		return invalid("counters outside their limits")
	}
	if saved.CallDepth != len(saved.Calls) {
		return invalid("a call depth that disagrees with its calls")
	}
	for index, frame := range saved.Try {
		// A region is opened at the depth of the call that opened it, in the
		// buffer this platform keeps for that position.
		if frame.Buffer != saved.TryBuffers[index] || frame.Depth < 1 || frame.Depth > saved.CallDepth ||
			index > 0 && saved.Try[index-1].Depth > frame.Depth {
			return invalid("a try region outside its calls")
		}
		if frame.Armed && (frame.Saved.CPSR&0x1f != 0x10 || !inStack(frame.Saved.Registers[armcore.RegisterSP])) {
			return invalid("a try region saved outside its stack")
		}
	}
	if saved.Done && len(saved.Calls) != 0 {
		return invalid("calls after it finished")
	}
	for index, record := range saved.Calls {
		frame := record.Frame
		if err := frame.Validate(); err != nil {
			return err
		}
		leaf := index == len(saved.Calls)-1
		if frame.End != returnAddress || frame.Budget != javaThreadSliceSteps || !inStack(frame.Context.Registers[armcore.RegisterSP]) || !inStack(frame.EntryStack) {
			return invalid("a call with an incompatible boundary, budget or stack")
		}
		// Every call a remainder here covers is entered with its arguments in
		// registers, so it starts on the stack its caller was using.
		parent := saved.Root.Context.Registers[armcore.RegisterSP]
		if index > 0 {
			parent = saved.Calls[index-1].Frame.Context.Registers[armcore.RegisterSP]
		}
		if frame.EntryStack != parent {
			return invalid("a call that does not start on its caller's stack")
		}
		remainder, err := client.javaFrameRemainder(frame, leaf)
		if err != nil {
			return err
		}
		if remainder != javaRemainder(record.Remainder) {
			return invalid("a call whose remainder disagrees with its platform call")
		}
		switch remainder {
		case javaRemainderWait:
			if record.Object == 0 || record.Object != frame.Context.Registers[0] || record.Depth < 1 || record.Depth > maxJavaMonitorDepth {
				return invalid("a wait on an object its call does not name")
			}
		case javaRemainderMonitor:
			if record.Object != frame.Context.Registers[0] || record.Depth != 0 {
				return invalid("a monitor its call does not name")
			}
		default:
			if record.Object != 0 || record.Depth != 0 {
				return invalid("a remainder with a lock it does not take")
			}
		}
	}
	return nil
}

// installJavaWorkerHook gives a thread the budget rule every guest thread runs
// under. A started thread and a restored one take the same one.
func (client *Client) installJavaWorkerHook(worker *javaWorker) {
	worker.armThread.SetStepBudget(javaThreadSliceSteps)
	worker.armThread.SetLimitHook(func(context.Context) error {
		// A thread holding a lock is granted another window rather than
		// parked, which is what keeps a synchronized body indivisible under a
		// scheduler that only switches at a park. The renewal count bounds it
		// so a loop inside one cannot hold the frame for ever.
		if worker.monitors > 0 && worker.renewals < maxJavaSliceRenewals {
			worker.renewals++
			return nil
		}
		worker.renewals = 0
		return worker.park()
	})
}

// restoreJavaWorker builds a parked thread from its record. Nothing runs: the
// goroutine that will carry it is started by the caller once every record has
// been checked.
func (client *Client) restoreJavaWorker(saved javaWorkerState) (*javaWorker, error) {
	root, err := client.core.RestoreRootThread(saved.Root, javaThreadSliceSteps)
	if err != nil {
		return nil, err
	}
	worker := &javaWorker{
		armThread: root, stackBase: saved.StackBase,
		grant:  make(chan context.Context),
		events: make(chan javaWorkerEvent, 1),
		wakeAt: saved.WakeAt, done: saved.Done,
		yields: saved.Yields, monitors: saved.Monitors, renewals: saved.Renewals,
		tryFrames: restoreJavaTry(saved.Try), tryBuffers: append([]uint32(nil), saved.TryBuffers...),
		callDepth: saved.CallDepth, waitSite: saved.WaitSite, waits: saved.Waits,
		waitReported: saved.WaitReported,
	}
	client.installJavaWorkerHook(worker)
	if len(saved.Calls) != 0 {
		worker.restored = append([]javaCallState(nil), saved.Calls...)
	}
	return worker, nil
}

// resumeJavaWorker is the goroutine of a restored thread that was parked
// inside `run`: wait for the first slice, finish what its platform call had
// left, and stay in the guest for as long as the title does. It ends the same
// way runJavaWorker does, because from here on it is the same thread.
func (client *Client) resumeJavaWorker(worker *javaWorker) {
	ctx, ok := <-worker.grant
	if !ok {
		return
	}
	calls := worker.restored
	worker.restored = nil
	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = backend.GuestPanic(client.logger, "LGT guest thread", recovered)
			}
		}()
		if _, callErr := client.resumeJavaCall(ctx, worker, worker.armThread, calls, 0); callErr != nil {
			// The same ending `callJavaRunnable` gives a thread whose `run`
			// threw: the language ends the thread, not the session.
			err = client.absorbUncaughtCallback(javaThreadRunMethod,
				fmt.Errorf("run a restored %s: %w", javaThreadRunMethod, callErr))
		}
	}()
	client.releaseJavaMonitors(worker)
	if client.logger != nil {
		client.logger.Debug("LGT java worker finished", "worker", fmt.Sprintf("%p", worker), "error", err)
	}
	worker.events <- javaWorkerEvent{done: true, err: err}
}

// resumeJavaCall continues one recorded call and answers what it returned.
//
// It stands where `callOn` stood when the thread parked. The depth that call
// counted is already in the thread's own record, so it is not counted again;
// what is owed is the other half — giving the depth back, and closing the try
// regions the call left open, when the call returns.
func (client *Client) resumeJavaCall(
	ctx context.Context, worker *javaWorker, parent *armcore.Thread, calls []javaCallState, index int,
) (uint32, error) {
	defer func() {
		client.javaCallDepth--
		client.dropJavaTryFrames(client.javaCallDepth)
	}()
	record := calls[index]
	complete := func(ctx context.Context, thread *armcore.Thread, _ armcore.SupervisorCall) error {
		return client.completeJavaRemainder(ctx, worker, thread, calls, index)
	}
	summary, err := client.core.ResumeCall(ctx, parent, record.Frame, complete, client.handleSupervisorCall)
	if err != nil {
		return 0, err
	}
	return summary.Context.Registers[0], nil
}

// completeJavaRemainder performs what one parked platform call had left. Each
// case is the tail of the handler it names, with the failures wrapped the way
// that handler wraps them so whatever reads the error cannot tell a restored
// thread from one that never stopped.
//
// **A restored call releases no pins.** The collector's pin list is emptied at
// the end of every round, so it is empty wherever a checkpoint is taken, and
// none of the calls here builds an object before it parks: there is nothing of
// this call's to release, and releasing to a mark it never took would drop
// another call's.
func (client *Client) completeJavaRemainder(
	ctx context.Context, worker *javaWorker, thread *armcore.Thread, calls []javaCallState, index int,
) (err error) {
	record := calls[index]
	slot := record.Frame.Context.Registers[12]
	table := "java"
	if record.Frame.SupervisorCall.Immediate == svcCategoryStdlib {
		table = "stdlib"
	} else {
		// The Java table reports a save that could not be read in place of
		// whatever the call answered; see handleJavaSVC.
		defer func() {
			if client.saveReadError != nil {
				err = client.saveReadError
			}
		}()
	}
	failed := func(err error) error { return wrapSlotError(table, slot, err) }
	unsupported := func(what string, err error) error {
		return failed(fmt.Errorf("%w (%s: %w)", ErrJavaAppUnsupported, what, err))
	}
	switch javaRemainder(record.Remainder) {
	case javaRemainderResult:
		return thread.SetRegister(0, 0)
	case javaRemainderWait:
		worker.waiting = javaWaitRecord{active: true, object: record.Object, depth: record.Depth}
		defer func() { worker.waiting = javaWaitRecord{} }()
		if err := client.finishJavaWait(ctx, worker, record.Object, record.Depth); err != nil {
			return unsupported("java/lang/Object.wait", err)
		}
		return thread.SetRegister(0, 0)
	case javaRemainderMonitor:
		if err := client.javaMonitorEnter(ctx, record.Object); err != nil {
			return unsupported("taking a lock", err)
		}
		return thread.SetRegister(0, 0)
	case javaRemainderInitializer:
		if _, err := client.resumeJavaCall(ctx, worker, thread, calls, index+1); err != nil {
			return unsupported("initialising a class", fmt.Errorf("run a static initialiser: %w", err))
		}
		return thread.SetRegister(0, 0)
	case javaRemainderFunction:
		if _, err := client.resumeJavaCall(ctx, worker, thread, calls, index+1); err != nil {
			return failed(fmt.Errorf("run the function at %#x: %w", record.Frame.Context.Registers[0], err))
		}
		return thread.SetRegister(0, 0)
	}
	return fmt.Errorf("LGT checkpoint call has no remainder")
}
