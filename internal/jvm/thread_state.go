package jvm

import (
	"fmt"
	"slices"
	"time"
)

// ThreadCheckpointState joins the heap and all JVM-owned worker continuations.
// The existing HeapState schema remains unchanged, including for AOT users.
// PlatformRoots is the prefix of Heap.Roots owned by the platform; subsequent
// ranges belong to the bytecode continuations.
type ThreadCheckpointState struct {
	Version       uint32
	PlatformRoots int
	Heap          HeapState
	Threads       []CheckpointThreadState
	Monitors      []CheckpointMonitorState
}

type CheckpointThreadState struct {
	Execution                             BytecodeExecutionState
	RootStart                             int
	Initial                               bool
	Wait                                  string
	Remaining                             time.Duration
	WaitObject                            uint32 // Index in this execution's roots.
	ReleasedDepth                         int
	Timed, Notified, Reenter, Interrupted bool
}

type CheckpointMonitorState struct {
	Object uint32 // Heap.Objects index, or zero for a class monitor.
	Class  string
	Owner  uint64
	Depth  int
}

type restoredBytecodeThread struct {
	saved CheckpointThreadState
	roots []*Object
	wait  *bytecodeThreadWait
}

func (restored *restoredBytecodeThread) run(vm *VM, state *execution) (Value, error) {
	var complete func(*Invocation) (Value, error)
	switch restored.saved.Wait {
	case "sleep":
		complete = func(call *Invocation) (Value, error) {
			if err := vm.waitCheckpointSleep(call.state, restored.saved.Remaining); err != nil {
				return VoidValue(), err
			}
			return VoidValue(), vm.threadYield()
		}
	case "join":
		complete = func(call *Invocation) (Value, error) {
			return VoidValue(), vm.waitCheckpointJoin(call.state, restored.wait.object)
		}
	case "wait":
		complete = func(call *Invocation) (Value, error) {
			return VoidValue(), vm.continueMonitorWait(call.state, restored.wait)
		}
	case "native":
		complete = func(call *Invocation) (Value, error) {
			defer func() { call.state.wait = nil }()
			wait, err := restored.saved.nativeWait(vm, restored.wait.object)
			if err != nil {
				return VoidValue(), err
			}
			return wait.ResumeCheckpointWait(call, restored.saved.Timed, restored.saved.Remaining)
		}
	}
	return vm.resumeBytecodeExecution(restored.saved.Execution, restored.roots, complete, state)
}

func cloneBytecodeState(saved BytecodeExecutionState) BytecodeExecutionState {
	saved.Frames = slices.Clone(saved.Frames)
	for i := range saved.Frames {
		saved.Frames[i].Locals = slices.Clone(saved.Frames[i].Locals)
		saved.Frames[i].Stack = slices.Clone(saved.Frames[i].Stack)
	}
	return saved
}

// CaptureState records a held barrier without running guest code. Unknown
// native remainders refuse the capture; Resume still releases the source.
func (paused *ParkedThreads) CaptureState(platformRoots []*Object, codec HeapCodec) (ThreadCheckpointState, error) {
	if paused == nil || paused.vm.checkpointPause.Load() != paused {
		return ThreadCheckpointState{}, fmt.Errorf("JVM thread checkpoint barrier is not held")
	}
	vm := paused.vm
	saved := ThreadCheckpointState{Version: 1, PlatformRoots: len(platformRoots)}
	roots := slices.Clone(platformRoots)
	for _, call := range paused.calls {
		record := CheckpointThreadState{RootStart: len(roots)}
		var callRoots []*Object
		if restored := call.restored; restored != nil {
			record = restored.saved
			record.RootStart = len(roots)
			record.Execution = cloneBytecodeState(restored.saved.Execution)
			callRoots = restored.roots
		} else if call.topFrame == nil && call.nativeDepth == 0 && call.steps == 0 {
			record.Initial = true
			record.Execution = BytecodeExecutionState{Version: 1, ID: call.id, MaxSteps: vm.config.MaxSteps, MaxFrames: vm.config.MaxFrames,
				Thread: 1, RootCount: 1}
			callRoots = []*Object{call.thread}
		} else if call.wait != nil && call.wait.kind == "native-monitor" {
			var err error
			record.Execution, callRoots, err = vm.captureNativeMonitorEntry(call)
			if err != nil {
				return ThreadCheckpointState{}, err
			}
		} else {
			nativeTail := 0
			if call.wait != nil {
				if call.wait.kind != "sleep" && call.wait.kind != "join" && call.wait.kind != "wait" && call.wait.kind != "native" {
					return ThreadCheckpointState{}, fmt.Errorf("JVM thread checkpoint has unsupported wait %q", call.wait.kind)
				}
				wait := call.wait
				nativeTail, record.Wait, record.Remaining = 1, wait.kind, wait.remaining
				record.ReleasedDepth, record.Timed, record.Notified, record.Reenter, record.Interrupted = wait.depth, wait.timed, wait.notified, wait.reenter, wait.interrupted
			}
			var err error
			record.Execution, callRoots, err = (&Invocation{vm: vm, state: call}).CaptureBytecodeState(nativeTail)
			if err != nil {
				return ThreadCheckpointState{}, err
			}
			if call.wait != nil && call.wait.object != nil {
				callRoots = append(callRoots, call.wait.object)
				record.WaitObject = uint32(len(callRoots))
				record.Execution.RootCount = len(callRoots)
			}
			if record.Wait == "native" {
				if _, err := record.nativeWait(vm, call.wait.object); err != nil {
					return ThreadCheckpointState{}, err
				}
			}
		}
		roots = append(roots, callRoots...)
		saved.Threads = append(saved.Threads, record)
	}
	var err error
	saved.Heap, err = vm.captureHeapState(roots, codec, func(object uint32, class string, owner uint64, depth int) {
		if owner != 0 || depth != 0 {
			saved.Monitors = append(saved.Monitors, CheckpointMonitorState{Object: object, Class: class, Owner: owner, Depth: depth})
		}
	})
	if err != nil {
		return ThreadCheckpointState{}, err
	}
	slices.SortFunc(saved.Monitors, func(a, b CheckpointMonitorState) int {
		if order := compareHeapText(a.Class, b.Class); order != 0 {
			return order
		}
		return compareHeapNumber(a.Object, b.Object)
	})
	if err := saved.validate(vm); err != nil {
		return ThreadCheckpointState{}, err
	}
	return saved, nil
}

func (saved ThreadCheckpointState) validate(vm *VM) error {
	invalid := func(reason string) error { return fmt.Errorf("JVM thread checkpoint: %s", reason) }
	if vm == nil || vm.config.GuestThreadStarter != nil || saved.Version != 1 || saved.PlatformRoots < 0 || saved.PlatformRoots > len(saved.Heap.Roots) || len(saved.Threads) > heapStateObjects {
		return invalid("invalid scheduler, version or roots")
	}
	if err := saved.Heap.validateThreads(vm.config, true); err != nil {
		return err
	}
	owners := make(map[uint64]bool)
	initial := make(map[uint64]bool)
	threads := make(map[uint32]bool)
	next := saved.PlatformRoots
	for _, thread := range saved.Threads {
		e := thread.Execution
		if e.ID == 0 || e.ID > saved.Heap.NextExecution || owners[e.ID] || thread.RootStart != next || e.RootCount < 1 || e.RootCount > len(saved.Heap.Roots)-next || e.Thread == 0 || uint64(e.Thread) > uint64(e.RootCount) {
			return invalid("invalid execution identity or root range")
		}
		owners[e.ID] = true
		initial[e.ID] = thread.Initial
		id := saved.Heap.Roots[next+int(e.Thread)-1]
		if id == 0 || threads[id] || saved.Heap.Objects[id-1].Thread == nil || !saved.Heap.Objects[id-1].Thread.Registered {
			return invalid("worker does not own a distinct live Thread")
		}
		threads[id] = true
		next += e.RootCount
		if thread.Initial {
			if e.Version != 1 || e.MaxSteps != vm.config.MaxSteps || e.MaxFrames != vm.config.MaxFrames || e.Steps != 0 || e.ThreadRuns != 0 || e.NativeTail || len(e.Frames) != 0 || thread.Wait != "" || thread.Remaining != 0 {
				return invalid("unstarted worker contains execution")
			}
		}
		if err := thread.validateWait(vm, saved.Heap); err != nil {
			return err
		}
	}
	if next != len(saved.Heap.Roots) {
		return invalid("unclaimed heap roots")
	}
	for i, object := range saved.Heap.Objects {
		if object.Thread != nil && object.Thread.Registered && !threads[uint32(i+1)] {
			return invalid("live Thread has no continuation")
		}
	}
	classMonitors := make(map[string]bool)
	for _, record := range saved.Heap.ClassMonitors {
		classMonitors[record.Class] = true
	}
	objects, classes := make(map[uint32]bool), make(map[string]bool)
	for _, monitor := range saved.Monitors {
		if !owners[monitor.Owner] || initial[monitor.Owner] || monitor.Depth <= 0 || monitor.Depth > 1<<20 ||
			monitor.Object > uint32(len(saved.Heap.Objects)) || monitor.Object == 0 && (!classMonitors[monitor.Class] || classes[monitor.Class]) ||
			monitor.Object != 0 && (monitor.Class != "" || objects[monitor.Object]) {
			return invalid("invalid monitor owner, depth or identity")
		}
		objects[monitor.Object], classes[monitor.Class] = true, true
	}
	return nil
}

func (thread CheckpointThreadState) validateWait(vm *VM, heap HeapState) error {
	invalid := func() error { return fmt.Errorf("JVM thread checkpoint has an invalid wait continuation") }
	e := thread.Execution
	if thread.Wait == "" {
		if e.NativeTail || thread.Remaining != 0 || thread.WaitObject != 0 || thread.ReleasedDepth != 0 || thread.Timed || thread.Notified || thread.Reenter || thread.Interrupted {
			return invalid()
		}
		return nil
	}
	if thread.Initial || !e.NativeTail || len(e.Frames) == 0 || thread.Remaining < 0 || uint64(thread.WaitObject) > uint64(e.RootCount) {
		return invalid()
	}
	switch thread.Wait {
	case "sleep":
		if thread.WaitObject != 0 || thread.ReleasedDepth != 0 || thread.Timed || thread.Notified || thread.Reenter || thread.Interrupted {
			return invalid()
		}
	case "native":
		if thread.WaitObject == 0 || heap.Roots[thread.RootStart+int(thread.WaitObject)-1] == 0 || thread.ReleasedDepth != 0 ||
			thread.Notified || thread.Reenter || thread.Interrupted || !thread.Timed && thread.Remaining != 0 {
			return invalid()
		}
		_, err := vm.checkpointNativeWaitMethod(e)
		return err
	case "join", "wait":
		if thread.WaitObject == 0 {
			return invalid()
		}
		id := heap.Roots[thread.RootStart+int(thread.WaitObject)-1]
		if id == 0 {
			return invalid()
		}
		if thread.Wait == "join" {
			if heap.Objects[id-1].Thread == nil || thread.Remaining != 0 || thread.ReleasedDepth != 0 || thread.Timed || thread.Notified || thread.Reenter || thread.Interrupted {
				return invalid()
			}
		} else if thread.ReleasedDepth <= 0 || thread.ReleasedDepth > 1<<20 || !thread.Timed && thread.Remaining != 0 || thread.Interrupted && !thread.Reenter {
			return invalid()
		}
	default:
		return invalid()
	}
	leaf := e.Frames[len(e.Frames)-1]
	class, err := vm.loader.Load(leaf.Class)
	if err != nil {
		return err
	}
	method := class.FindMethod(leaf.Method, leaf.Descriptor)
	if method == nil || method.CodeAttribute() == nil {
		return invalid()
	}
	code, pc := method.CodeAttribute().Bytecode, leaf.InvokePC
	if pc < 0 || pc > len(code)-3 {
		return invalid()
	}
	reference, err := class.ConstantPool.ReferenceAt(uint16(code[pc+1])<<8 | uint16(code[pc+2]))
	if err != nil || reference.Name != thread.Wait {
		return invalid()
	}
	if thread.Wait == "sleep" {
		owner := vm.checkpointNativeOwner(reference.Class, reference.Name, reference.Descriptor)
		if code[pc] != 0xb8 || reference.Descriptor != "(J)V" || owner != ThreadClass {
			return invalid()
		}
	} else {
		if code[pc] != 0xb6 && code[pc] != 0xb7 {
			return invalid()
		}
		owner := vm.checkpointNativeOwner(reference.Class, reference.Name, reference.Descriptor)
		if thread.Wait == "join" {
			if owner != ThreadClass || reference.Descriptor != "()V" {
				return invalid()
			}
		} else if owner != ObjectClass || reference.Descriptor != "()V" && reference.Descriptor != "(J)V" && reference.Descriptor != "(JI)V" {
			return invalid()
		}
	}
	return nil
}

func (vm *VM) checkpointNativeOwner(className, name, descriptor string) string {
	for depth := 0; className != "" && depth < 512; depth++ {
		vm.mu.RLock()
		entry, native := vm.natives[methodKey{class: className, name: name, descriptor: descriptor}]
		vm.mu.RUnlock()
		if native && (entry.context != nil || entry.plain != nil) {
			return className
		}
		class, err := vm.loader.Load(className)
		if err != nil || class.FindMethod(name, descriptor) != nil {
			return ""
		}
		className = class.SuperName
	}
	return ""
}

// PreparedThreads is a detached heap and set of validated workers. No worker
// starts until Start. The owner rebinds platform roots, devices and current
// saves first. An invalid preparation may populate the destination VM, which
// must be discarded; it never executes code or changes the source VM.
type PreparedThreads struct {
	vm      *VM
	threads []*Object
	started bool
}

func (vm *VM) PrepareThreadCheckpoint(saved ThreadCheckpointState, codec HeapCodec) (*PreparedThreads, []*Object, error) {
	if err := saved.validate(vm); err != nil {
		return nil, nil, err
	}
	heap := saved.Heap
	heap.Roots = slices.Clone(heap.Roots)
	for _, m := range saved.Monitors {
		heap.Roots = append(heap.Roots, m.Object)
	}
	roots, err := vm.restoreHeapState(heap, codec, true)
	if err != nil {
		return nil, nil, err
	}
	prepared := &PreparedThreads{vm: vm}
	for i, record := range saved.Monitors {
		var m *monitor
		if record.Object == 0 {
			m = vm.classMonitor(record.Class)
		} else {
			m = &roots[len(saved.Heap.Roots)+i].monitor
		}
		m.owner, m.depth = record.Owner, record.Depth
	}
	for _, record := range saved.Threads {
		e := record.Execution
		threadRoots := slices.Clone(roots[record.RootStart : record.RootStart+e.RootCount])
		thread := threadRoots[e.Thread-1]
		state := &execution{id: e.ID, steps: e.Steps, thread: thread, initializing: make(map[string]bool)}
		if !record.Initial {
			frames, err := vm.prepareBytecodeState(e, threadRoots)
			if err != nil {
				return nil, nil, err
			}
			var wait *bytecodeThreadWait
			if record.Wait != "" {
				wait = &bytecodeThreadWait{kind: record.Wait, remaining: record.Remaining,
					depth: record.ReleasedDepth, timed: record.Timed, notified: record.Notified, reenter: record.Reenter, interrupted: record.Interrupted}
				if record.WaitObject != 0 {
					wait.object = threadRoots[record.WaitObject-1]
				}
				if record.Wait == "native" {
					if _, err := record.nativeWait(vm, wait.object); err != nil {
						return nil, nil, err
					}
				}
				if record.Wait == "wait" {
					wait.notification = make(chan struct{})
					if wait.notified {
						close(wait.notification)
					} else if !wait.reenter {
						wait.object.monitor.waiters = append(wait.object.monitor.waiters, wait)
					}
					if wait.object.monitor.owner == e.ID {
						return nil, nil, fmt.Errorf("JVM checkpoint waiting thread still owns its released monitor")
					}
				}
			}
			if err := vm.validateBytecodeMonitors(frames, e.ID, wait); err != nil {
				return nil, nil, err
			}
			record.Execution = cloneBytecodeState(e)
			state.restored = &restoredBytecodeThread{saved: record, roots: threadRoots, wait: wait}
			state.wait = wait
		}
		vm.threadState(thread).execution = state
		prepared.threads = append(prepared.threads, thread)
	}
	return prepared, roots[:saved.PlatformRoots], nil
}

// Start grants every prepared worker its first slice. Call only after the
// platform has committed its heap, current saves, clocks and output devices.
func (prepared *PreparedThreads) Start() {
	if prepared == nil || prepared.started {
		return
	}
	prepared.started = true
	for _, thread := range prepared.threads {
		go prepared.vm.runGuestThread(thread, prepared.vm.threadState(thread))
	}
}

// Discard ends only a detached preparation. It cannot run a guest finally block
// or a platform exit callback.
func (prepared *PreparedThreads) Discard() {
	if prepared == nil || prepared.started {
		return
	}
	prepared.vm.Close()
	for _, thread := range prepared.threads {
		prepared.vm.EndGuestThread(thread)
	}
}
