package jvm

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

//go:embed testdata/ThreadCheckpointProbe.class
var threadCheckpointClass []byte

func newThreadCheckpointVM(t *testing.T, entered ContextMethod) *VM {
	t.Helper()
	machine := New(mapClassSource{"ThreadCheckpointProbe": threadCheckpointClass}, Options{
		RenewSteps: func() error { return nil }, AsyncError: func(err error) {
			if !errors.Is(err, ErrClosed) {
				t.Errorf("guest failed: %v", err)
			}
		},
	})
	if err := machine.RegisterContextNative("ThreadCheckpointProbe", "entered", "(LThreadCheckpointProbe;)V", entered); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		paused, err := machine.PauseThreads(ctx)
		machine.Close()
		if err == nil {
			paused.Resume()
			for _, thread := range paused.threads {
				select {
				case <-machine.threadState(thread).done:
				case <-ctx.Done():
					t.Error("checkpoint worker did not terminate")
				}
			}
		}
	})
	return machine
}

func startCheckpointThread(t *testing.T, machine *VM, delay int64) *Object {
	t.Helper()
	thread, err := machine.NewObject("ThreadCheckpointProbe", "(J)V", LongValue(delay))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.InvokeVirtual(thread, "start", "()V"); err != nil {
		t.Fatal(err)
	}
	return thread
}

func checkpointTicks(t *testing.T, vm *VM, thread *Object) int32 {
	t.Helper()
	value, err := vm.Field(thread, "ThreadCheckpointProbe", "ticks", "I")
	if err != nil {
		t.Fatal(err)
	}
	count, err := value.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func waitCheckpointCount(t *testing.T, vm *VM, thread *Object, after int32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for checkpointTicks(t, vm, thread) <= after {
		if time.Now().After(deadline) {
			t.Fatal("guest did not advance")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestThreadCheckpointBarrierStopsSleepingAndRunningBytecode(t *testing.T) {
	entered := make(chan struct{}, 2)
	vm := newThreadCheckpointVM(t, func(*Invocation, []Value) (Value, error) {
		entered <- struct{}{}
		return VoidValue(), nil
	})
	spinner := startCheckpointThread(t, vm, -1)
	sleeper := startCheckpointThread(t, vm, 60000)
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("guest thread did not enter")
		}
	}
	waitCheckpointCount(t, vm, spinner, 0)
	waitCheckpointCount(t, vm, sleeper, 0)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	paused, err := vm.PauseThreads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer paused.Resume()
	if len(paused.calls) != 2 {
		t.Fatalf("parked executions = %d", len(paused.calls))
	}
	before := checkpointTicks(t, vm, spinner)
	for _, execution := range paused.calls {
		native := 0
		if execution.wait != nil {
			native = 1
			if execution.wait.kind != "sleep" || execution.wait.remaining <= 0 {
				t.Fatalf("parked wait = %+v", execution.wait)
			}
		}
		saved, roots, err := (&Invocation{vm: vm, state: execution}).CaptureBytecodeState(native)
		if err != nil || len(saved.Frames) == 0 || len(roots) == 0 {
			t.Fatalf("parked bytecode: %v", err)
		}
	}
	if _, err := vm.PauseThreads(ctx); err == nil {
		t.Fatal("second owner acquired checkpoint barrier")
	}
	time.Sleep(20 * time.Millisecond)
	if got := checkpointTicks(t, vm, spinner); got != before {
		t.Fatalf("parked guest advanced from %d to %d", before, got)
	}
	paused.Resume()
	paused.Resume()
	waitCheckpointCount(t, vm, spinner, before)
	if got := checkpointTicks(t, vm, sleeper); got != 1 {
		t.Fatalf("checkpoint repeated or finished long sleep: ticks=%d", got)
	}
}

func TestCanceledThreadCheckpointReleasesAlreadyParkedWorkers(t *testing.T) {
	blocked := make(chan struct{})
	release := make(chan struct{})
	vm := newThreadCheckpointVM(t, func(_ *Invocation, args []Value) (Value, error) {
		thread, _ := args[0].Reference()
		delay, _ := thread.Fields["ThreadCheckpointProbe.delay:J"].Int64()
		if delay == 1 {
			close(blocked)
			<-release
		}
		return VoidValue(), nil
	})
	defer close(release)
	spinner := startCheckpointThread(t, vm, -1)
	startCheckpointThread(t, vm, 1)
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("native did not block")
	}
	waitCheckpointCount(t, vm, spinner, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := vm.PauseThreads(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked checkpoint = %v", err)
	}
	if vm.checkpointPause.Load() != nil {
		t.Fatal("canceled barrier still owns the VM")
	}
	waitCheckpointCount(t, vm, spinner, checkpointTicks(t, vm, spinner))
}

func TestThreadCheckpointRestoresHeapSleepAndNestedMonitors(t *testing.T) {
	entered := make(chan struct{}, 1)
	source := newThreadCheckpointVM(t, func(*Invocation, []Value) (Value, error) {
		entered <- struct{}{}
		return VoidValue(), nil
	})
	thread := startCheckpointThread(t, source, 60000)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("guest did not enter")
	}
	waitCheckpointCount(t, source, thread, 0)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var paused *ParkedThreads
	for {
		var err error
		paused, err = source.PauseThreads(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(paused.calls) == 1 && paused.calls[0].wait != nil {
			break
		}
		paused.Resume()
		time.Sleep(time.Millisecond)
	}
	defer paused.Resume()
	saved, err := paused.CaptureState([]*Object{thread}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Monitors) != 1 || saved.Monitors[0].Depth != 2 || saved.Threads[0].Wait != "sleep" {
		t.Fatalf("captured monitors or wait: %+v, %+v", saved.Monitors, saved.Threads)
	}
	// Standalone heap capture keeps its existing contract even at the barrier.
	if _, err := source.CaptureHeapState(nil, HeapCodec{}); err == nil {
		t.Fatal("standalone heap capture omitted live thread continuations")
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var record ThreadCheckpointState
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	fresh := newThreadCheckpointVM(t, func(*Invocation, []Value) (Value, error) {
		t.Error("restored thread repeated its entry")
		return VoidValue(), nil
	})
	prepared, roots, err := fresh.PrepareThreadCheckpoint(record, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if len(roots) != 1 || roots[0] == thread || checkpointTicks(t, fresh, roots[0]) != 1 {
		t.Fatal("thread heap was not restored independently")
	}
	// Install the barrier before granting any restored worker. A capture here
	// must preserve the original continuation rather than restart Thread.run.
	barrier := make(chan *ParkedThreads, 1)
	failure := make(chan error, 1)
	go func() {
		pause, err := fresh.PauseThreads(ctx)
		if err != nil {
			failure <- err
		} else {
			barrier <- pause
		}
	}()
	for fresh.checkpointPause.Load() == nil {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(time.Millisecond)
	}
	prepared.Start()
	var restoredPause *ParkedThreads
	select {
	case err := <-failure:
		t.Fatal(err)
	case restoredPause = <-barrier:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer restoredPause.Resume()
	again, err := restoredPause.CaptureState(roots, HeapCodec{})
	if err != nil || !reflect.DeepEqual(saved, again) {
		t.Fatalf("recapture before restored grant differs: %v", err)
	}
	restoredPause.Resume()
	for _, run := range []struct {
		vm     *VM
		thread *Object
	}{{source, thread}, {fresh, roots[0]}} {
		if err := run.vm.SetField(run.thread, "ThreadCheckpointProbe", "stopped", "Z", IntValue(1)); err != nil {
			t.Fatal(err)
		}
		if _, err := run.vm.InvokeVirtual(run.thread, "interrupt", "()V"); err != nil {
			t.Fatal(err)
		}
	}
	paused.Resume()
	for _, run := range []struct {
		vm     *VM
		thread *Object
	}{{source, thread}, {fresh, roots[0]}} {
		select {
		case <-run.vm.threadState(run.thread).done:
		case <-ctx.Done():
			t.Fatal("restored wait or monitor did not finish")
		}
		if got := checkpointTicks(t, run.vm, run.thread); got != 1 {
			t.Fatalf("sleep prefix was replayed: ticks=%d", got)
		}
		run.thread.monitor.mu.Lock()
		owner, depth := run.thread.monitor.owner, run.thread.monitor.depth
		run.thread.monitor.mu.Unlock()
		if owner != 0 || depth != 0 {
			t.Fatalf("completed worker retained monitor owner=%d depth=%d", owner, depth)
		}
	}
	// Too few recursive acquisitions must fail before any worker is started.
	badVM := newThreadCheckpointVM(t, func(*Invocation, []Value) (Value, error) { return VoidValue(), nil })
	record.Monitors[0].Depth = 1
	if bad, _, err := badVM.PrepareThreadCheckpoint(record, HeapCodec{}); err == nil {
		bad.Discard()
		t.Fatal("restoration accepted an incomplete monitor depth")
	}
	badVM.Close()
}

//go:embed testdata/WaitCheckpointProbe.class
var waitCheckpointClass []byte

func newWaitCheckpointVM(t *testing.T) *VM {
	t.Helper()
	vm := New(mapClassSource{"WaitCheckpointProbe": waitCheckpointClass}, Options{
		AsyncError: func(err error) {
			if !errors.Is(err, ErrClosed) {
				t.Errorf("guest failed: %v", err)
			}
		},
	})
	t.Cleanup(func() {
		threads := vm.ThreadObjects()
		vm.Close()
		for _, thread := range threads {
			if thread == vm.mainThread {
				continue
			}
			select {
			case <-vm.threadState(thread).done:
			case <-time.After(time.Second):
				t.Error("guest did not close")
			}
		}
	})
	return vm
}

func startWaitCheckpoint(t *testing.T, vm *VM, mode int32, target *Object) *Object {
	t.Helper()
	thread, err := vm.NewObject("WaitCheckpointProbe", "(ILWaitCheckpointProbe;)V", IntValue(mode), ReferenceValue(target))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = vm.InvokeVirtual(thread, "start", "()V"); err != nil {
		t.Fatal(err)
	}
	return thread
}

func pauseCheckpointWhen(t *testing.T, vm *VM, ready func(*ParkedThreads) bool) *ParkedThreads {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		paused, err := vm.PauseThreads(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if ready(paused) {
			return paused
		}
		paused.Resume()
		time.Sleep(time.Millisecond)
	}
}

func TestThreadCheckpointRestoresMonitorWait(t *testing.T) {
	for _, action := range []string{"notify", "interrupt", "notified-reentry"} {
		t.Run(action, func(t *testing.T) {
			source := newWaitCheckpointVM(t)
			thread := startWaitCheckpoint(t, source, 1, nil)
			paused := pauseCheckpointWhen(t, source, func(p *ParkedThreads) bool {
				return len(p.calls) == 1 && p.calls[0].wait != nil && p.calls[0].wait.kind == "wait"
			})
			if action == "notified-reentry" {
				paused.Resume()
				startWaitCheckpoint(t, source, 5, thread)
				paused = pauseCheckpointWhen(t, source, func(p *ParkedThreads) bool {
					if len(p.calls) != 2 {
						return false
					}
					for _, c := range p.calls {
						if c.wait == nil || c.wait.kind == "wait" && !c.wait.reenter {
							return false
						}
					}
					return true
				})
			}
			defer paused.Resume()
			saved, err := paused.CaptureState([]*Object{thread}, HeapCodec{})
			if err != nil {
				t.Fatal(err)
			}
			fresh := newWaitCheckpointVM(t)
			prepared, roots, err := fresh.PrepareThreadCheckpoint(saved, HeapCodec{})
			if err != nil {
				t.Fatal(err)
			}
			prepared.Start()
			paused.Resume()
			for _, run := range []struct {
				vm     *VM
				thread *Object
			}{{source, thread}, {fresh, roots[0]}} {
				if action == "notified-reentry" {
					for _, other := range run.vm.ThreadObjects() {
						if other == run.thread || other == run.vm.mainThread {
							continue
						}
						if _, err := run.vm.InvokeVirtual(other, "interrupt", "()V"); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					method := "signal"
					if action == "interrupt" {
						method = "interrupt"
					}
					if _, err := run.vm.InvokeVirtual(run.thread, method, "()V"); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case <-run.vm.threadState(run.thread).done:
				case <-time.After(time.Second):
					t.Fatal("restored monitor wait did not finish")
				}
				for _, name := range []string{"before", "after"} {
					value, err := run.vm.Field(run.thread, "WaitCheckpointProbe", name, "I")
					if err != nil {
						t.Fatal(err)
					}
					got, _ := value.Int32()
					if got != 1 {
						t.Fatalf("%s=%d, want 1", name, got)
					}
				}
			}
		})
	}
}

func TestThreadCheckpointRestoresJoinAndContendedEntry(t *testing.T) {
	source := newWaitCheckpointVM(t)
	holder := startWaitCheckpoint(t, source, 0, nil)
	paused := pauseCheckpointWhen(t, source, func(p *ParkedThreads) bool { return len(p.calls) == 1 && p.calls[0].wait != nil })
	paused.Resume()
	joiner := startWaitCheckpoint(t, source, 2, holder)
	block := startWaitCheckpoint(t, source, 3, holder)
	method := startWaitCheckpoint(t, source, 4, holder)
	paused = pauseCheckpointWhen(t, source, func(p *ParkedThreads) bool {
		if len(p.calls) != 4 {
			return false
		}
		for _, c := range p.calls {
			if c.thread == holder || c.thread == joiner {
				if c.wait == nil {
					return false
				}
			} else if c.topFrame == nil {
				return false
			} else if c.thread == method && !c.topFrame.monitorPending {
				return false
			} else if c.thread == block && c.topFrame.code.Bytecode[c.topFrame.pc] != 0xc2 {
				return false
			}
		}
		return true
	})
	defer paused.Resume()
	saved, err := paused.CaptureState([]*Object{holder, joiner, block, method}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	fresh := newWaitCheckpointVM(t)
	prepared, roots, err := fresh.PrepareThreadCheckpoint(saved, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	prepared.Start()
	paused.Resume()
	for _, run := range []struct {
		vm    *VM
		roots []*Object
	}{{source, []*Object{holder, joiner, block, method}}, {fresh, roots}} {
		if _, err := run.vm.InvokeVirtual(run.roots[0], "interrupt", "()V"); err != nil {
			t.Fatal(err)
		}
		for i, thread := range run.roots {
			select {
			case <-run.vm.threadState(thread).done:
			case <-time.After(time.Second):
				t.Fatal("join or entry remained blocked")
			}
			if i > 0 {
				value, err := run.vm.Field(thread, "WaitCheckpointProbe", "after", "I")
				if err != nil {
					t.Fatal(err)
				}
				got, _ := value.Int32()
				if got != 1 {
					t.Fatalf("worker %d after=%d", i, got)
				}
			}
		}
		run.roots[0].monitor.mu.Lock()
		owner, depth := run.roots[0].monitor.owner, run.roots[0].monitor.depth
		run.roots[0].monitor.mu.Unlock()
		if owner != 0 || depth != 0 {
			t.Fatal("restored holder lock remained owned")
		}
	}
}

func TestThreadCheckpointRestoresSynchronizedNativeEntry(t *testing.T) {
	const argument = int64(0x123456789abcdef)
	register := func(vm *VM, expected **Object, calls *atomic.Int32) {
		t.Helper()
		err := vm.RegisterContextNative("WaitCheckpointProbe", "nativeWork", "(JLjava/lang/Object;)J", func(call *Invocation, args []Value) (Value, error) {
			calls.Add(1)
			if len(args) != 3 {
				return VoidValue(), fmt.Errorf("native entry has %d arguments", len(args))
			}
			receiver, receiverErr := args[0].Reference()
			value, valueErr := args[1].Int64()
			alias, aliasErr := args[2].Reference()
			if receiverErr != nil || valueErr != nil || aliasErr != nil || receiver != *expected || alias != receiver || value != argument {
				return VoidValue(), fmt.Errorf("native entry changed its receiver, reference alias, or wide argument")
			}
			receiver.monitor.mu.Lock()
			owned := receiver.monitor.owner == call.state.id && receiver.monitor.depth == 1
			receiver.monitor.mu.Unlock()
			if !owned {
				return VoidValue(), fmt.Errorf("native body does not own the receiver monitor exactly once")
			}
			return LongValue(value + 7), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	source := newWaitCheckpointVM(t)
	var sourceCalls, restoredCalls atomic.Int32
	holder := startWaitCheckpoint(t, source, 0, nil)
	register(source, &holder, &sourceCalls)
	paused := pauseCheckpointWhen(t, source, func(p *ParkedThreads) bool {
		return len(p.calls) == 1 && p.calls[0].wait != nil && p.calls[0].wait.kind == "sleep"
	})
	paused.Resume()
	waiter := startWaitCheckpoint(t, source, 6, holder)
	paused = pauseCheckpointWhen(t, source, func(p *ParkedThreads) bool {
		if len(p.calls) != 2 {
			return false
		}
		for _, call := range p.calls {
			if call.thread == waiter {
				return call.wait != nil && call.wait.kind == "native-monitor"
			}
		}
		return false
	})
	defer paused.Resume()
	saved, err := paused.CaptureState([]*Object{holder, waiter}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if sourceCalls.Load() != 0 {
		t.Fatal("native body ran before its contended entry was captured")
	}
	found := false
	for _, thread := range saved.Threads {
		if saved.Heap.Roots[thread.RootStart+int(thread.Execution.Thread)-1] != saved.Heap.Roots[1] {
			continue
		}
		found = true
		leaf := thread.Execution.Frames[len(thread.Execution.Frames)-1]
		if thread.Wait != "" || thread.Execution.NativeTail || leaf.PC != leaf.InvokePC || len(leaf.Stack) != 4 ||
			thread.Execution.Steps+1 != source.threadState(waiter).execution.steps {
			t.Fatalf("native entry was not restored to its pre-invoke stack: %+v", thread)
		}
	}
	if !found {
		t.Fatal("native waiter has no saved execution")
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var record ThreadCheckpointState
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	fresh := newWaitCheckpointVM(t)
	var restoredHolder *Object
	register(fresh, &restoredHolder, &restoredCalls)
	prepared, roots, err := fresh.PrepareThreadCheckpoint(record, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if len(roots) != 2 || roots[0] == holder || roots[1] == waiter {
		t.Fatal("native entry heap was not independently restored")
	}
	restoredHolder = roots[0]
	prepared.Start()
	paused.Resume()
	for _, run := range []struct {
		vm    *VM
		roots []*Object
		calls *atomic.Int32
	}{{source, []*Object{holder, waiter}, &sourceCalls}, {fresh, roots, &restoredCalls}} {
		if _, err := run.vm.InvokeVirtual(run.roots[0], "interrupt", "()V"); err != nil {
			t.Fatal(err)
		}
		for _, thread := range run.roots {
			select {
			case <-run.vm.threadState(thread).done:
			case <-time.After(time.Second):
				t.Fatal("contended native entry remained blocked")
			}
			for _, name := range []string{"before", "after"} {
				value, err := run.vm.Field(thread, "WaitCheckpointProbe", name, "I")
				count, valueErr := value.Int32()
				if err != nil || valueErr != nil || count != 1 {
					t.Fatalf("worker %s = %v, %v; prefix or suffix did not run once", name, value, err)
				}
			}
		}
		result, err := run.vm.Field(run.roots[1], "WaitCheckpointProbe", "nativeResult", "J")
		value, valueErr := result.Int64()
		if err != nil || valueErr != nil || value != argument+7 || run.calls.Load() != 1 {
			t.Fatalf("native result=%v, error=%v, calls=%d", result, err, run.calls.Load())
		}
		run.roots[0].monitor.mu.Lock()
		owner, depth := run.roots[0].monitor.owner, run.roots[0].monitor.depth
		run.roots[0].monitor.mu.Unlock()
		if owner != 0 || depth != 0 {
			t.Fatalf("completed native entry retained monitor owner=%d depth=%d", owner, depth)
		}
	}
	if source.threadState(waiter).execution.steps != fresh.threadState(roots[1]).execution.steps {
		t.Fatal("reconstructed native invocation changed the instruction budget")
	}
}
