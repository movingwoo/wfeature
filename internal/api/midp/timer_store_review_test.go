package midp

import (
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// java.util.Timer refuses a task that is already scheduled, on any Timer.
// Starting a second worker instead ran the task twice as often, and two runs
// of it at once.
func TestTimerRefusesATaskAlreadyScheduled(t *testing.T) {
	var runs atomic.Int32
	machine := timerMachine(t, &runs)
	t.Cleanup(machine.Close)
	first, err := machine.NewObject(TimerClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	second, err := machine.NewObject(TimerClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	task, err := machine.NewObject(countingTaskClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	// A refused schedule does not count as one.
	if _, err := machine.InvokeVirtual(first, "schedule", "(Ljava/util/TimerTask;J)V", jvm.ReferenceValue(task), jvm.LongValue(-1)); err == nil {
		t.Fatal("negative delay accepted")
	}
	if _, err := machine.InvokeVirtual(first, "scheduleAtFixedRate", "(Ljava/util/TimerTask;JJ)V",
		jvm.ReferenceValue(task), jvm.LongValue(1<<30), jvm.LongValue(100)); err != nil {
		t.Fatalf("first schedule error = %v", err)
	}
	for _, timer := range []*jvm.Object{first, second} {
		_, err := machine.InvokeVirtual(timer, "scheduleAtFixedRate", "(Ljava/util/TimerTask;JJ)V",
			jvm.ReferenceValue(task), jvm.LongValue(1<<30), jvm.LongValue(100))
		if !machine.IsGuestException(err, "java/lang/IllegalStateException") {
			t.Fatalf("second schedule of one task error = %v, want IllegalStateException", err)
		}
	}
	workers := 0
	for _, thread := range machine.ThreadObjects() {
		if thread.ClassName == TimerThreadClass {
			workers++
		}
	}
	if workers != 1 {
		t.Fatalf("one task has %d timer workers", workers)
	}
}

// A Timer makes the task's worker before it marks the task scheduled. Making
// the worker can be refused by the heap budget, and a task marked with no
// worker never ran and could never be scheduled again: every later schedule
// was refused as one already made.
func TestTimerRefusedAWorkerLeavesTheTaskSchedulable(t *testing.T) {
	var runs atomic.Int32
	machine := jvm.New(nil, jvm.Options{MaxHeapBytes: 8 << 20})
	if err := Define(machine); err != nil {
		t.Fatal(err)
	}
	defineCountingTask(t, machine, &runs)
	t.Cleanup(machine.Close)
	timer, err := machine.NewObject(TimerClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	task, err := machine.NewObject(countingTaskClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	// Arrays up to the budget, then small objects until one is refused: from
	// there the budget weighs every allocation, the worker's too. A string the
	// host makes is never refused, and holding one takes the charge past the
	// limit, so the worker cannot fit however the last sample fell.
	var held []*jvm.Object
	for {
		array, err := machine.NewArray(jvm.Type{Kind: jvm.TypeInt}, 64<<10)
		if err != nil {
			break
		}
		held = append(held, array)
	}
	for refused := false; !refused; {
		object, err := machine.NewObject(countingTaskClass, "()V")
		if err != nil {
			if !machine.IsGuestException(err, "java/lang/OutOfMemoryError") {
				t.Fatal(err)
			}
			refused = true
		}
		held = append(held, object)
	}
	held = append(held, machine.NewString("held past the limit"))
	schedule := func() error {
		_, err := machine.InvokeVirtual(timer, "schedule", "(Ljava/util/TimerTask;J)V", jvm.ReferenceValue(task), jvm.LongValue(1))
		return err
	}
	if err := schedule(); !machine.IsGuestException(err, "java/lang/OutOfMemoryError") {
		t.Fatalf("a schedule whose worker the full budget refuses: %v, want OutOfMemoryError", err)
	}
	if queued, err := machine.Field(task, TimerTaskClass, "queued", "Z"); err != nil {
		t.Fatal(err)
	} else if flag, _ := queued.Int32(); flag != 0 {
		t.Fatal("the task is marked scheduled with no worker")
	}
	runtime.KeepAlive(held)
	held = nil
	runtime.GC()
	if err := schedule(); err != nil {
		t.Fatalf("the schedule once the budget has room: %v", err)
	}
	waitForRuns(t, &runs, 1)
}
