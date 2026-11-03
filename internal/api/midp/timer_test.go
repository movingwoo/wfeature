package midp

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// countingTaskClass is a title's TimerTask: a subclass whose run() is its own.
// The runs are counted in Go rather than in a guest field because the assert
// is about how many times the worker came back, not about what the task did.
const countingTaskClass = "net/wfeature/CountingTask"

func defineCountingTask(t *testing.T, machine *jvm.VM, runs *atomic.Int32) {
	t.Helper()
	definition := jvm.ClassDefinition{
		Name:      countingTaskClass,
		SuperName: TimerTaskClass,
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: emptyInit},
			{Name: "run", Descriptor: "()V", Access: jvm.AccessPublic, Body: func(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
				runs.Add(1)
				return jvm.VoidValue(), nil
			}},
		},
	}
	if err := machine.DefineClass(definition); err != nil {
		t.Fatalf("DefineClass(%s) error = %v", countingTaskClass, err)
	}
}

func timerMachine(t *testing.T, runs *atomic.Int32) *jvm.VM {
	t.Helper()
	machine := jvm.New(nil, jvm.Options{})
	if err := Define(machine); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	defineCountingTask(t, machine, runs)
	return machine
}

func waitForRuns(t *testing.T, runs *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runs.Load() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task ran %d times, want at least %d", runs.Load(), want)
}

// A one-shot schedule runs the task once, on a thread that is not the one that
// scheduled it: a title schedules from its frame loop and keeps drawing.
func TestTimerRunsAScheduledTaskOnce(t *testing.T) {
	var runs atomic.Int32
	machine := timerMachine(t, &runs)

	timer, err := machine.NewObject(TimerClass, "()V")
	if err != nil {
		t.Fatalf("new Timer() error = %v", err)
	}
	task, err := machine.NewObject(countingTaskClass, "()V")
	if err != nil {
		t.Fatalf("new CountingTask() error = %v", err)
	}
	if _, err := machine.InvokeVirtual(timer, "schedule", "(Ljava/util/TimerTask;J)V",
		jvm.ReferenceValue(task), jvm.LongValue(1)); err != nil {
		t.Fatalf("Timer.schedule() error = %v", err)
	}
	waitForRuns(t, &runs, 1)

	// scheduledExecutionTime is when the run was due, so it is a clock
	// reading rather than a zero once the task has run.
	value, err := machine.InvokeVirtual(task, "scheduledExecutionTime", "()J")
	if err != nil {
		t.Fatalf("TimerTask.scheduledExecutionTime() error = %v", err)
	}
	if when, _ := value.Int64(); when <= 0 {
		t.Errorf("scheduledExecutionTime() = %d, want a clock reading", when)
	}
}

// A repeating schedule keeps coming back, and cancelling the Timer is what
// stops it. The count is read twice after the cancel: the first read is what
// the run in flight may still add, and the second is what proves nothing else
// did.
func TestTimerCancelStopsARepeatingTask(t *testing.T) {
	var runs atomic.Int32
	machine := timerMachine(t, &runs)

	timer, err := machine.NewObject(TimerClass, "()V")
	if err != nil {
		t.Fatalf("new Timer() error = %v", err)
	}
	task, err := machine.NewObject(countingTaskClass, "()V")
	if err != nil {
		t.Fatalf("new CountingTask() error = %v", err)
	}
	if _, err := machine.InvokeVirtual(timer, "scheduleAtFixedRate", "(Ljava/util/TimerTask;JJ)V",
		jvm.ReferenceValue(task), jvm.LongValue(1), jvm.LongValue(1)); err != nil {
		t.Fatalf("Timer.scheduleAtFixedRate() error = %v", err)
	}
	waitForRuns(t, &runs, 3)

	if _, err := machine.InvokeVirtual(timer, "cancel", "()V"); err != nil {
		t.Fatalf("Timer.cancel() error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	settled := runs.Load()
	time.Sleep(50 * time.Millisecond)
	if final := runs.Load(); final != settled {
		t.Errorf("task ran %d more times after cancel", final-settled)
	}

	// A cancelled Timer takes no further schedule, which is how a title that
	// reuses one finds out rather than waiting for a task that never runs.
	next, err := machine.NewObject(countingTaskClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	_, err = machine.InvokeVirtual(timer, "schedule", "(Ljava/util/TimerTask;J)V",
		jvm.ReferenceValue(next), jvm.LongValue(1))
	if err == nil {
		t.Fatal("scheduling on a cancelled Timer succeeded")
	}
}

// Cancelling the task rather than the Timer stops that task, and the boolean
// says whether this call was the one that did it.
func TestTimerTaskCancelAnswersWhetherItStoppedTheTask(t *testing.T) {
	var runs atomic.Int32
	machine := timerMachine(t, &runs)

	timer, err := machine.NewObject(TimerClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	task, err := machine.NewObject(countingTaskClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.InvokeVirtual(timer, "schedule", "(Ljava/util/TimerTask;JJ)V",
		jvm.ReferenceValue(task), jvm.LongValue(1), jvm.LongValue(1)); err != nil {
		t.Fatalf("Timer.schedule() error = %v", err)
	}
	waitForRuns(t, &runs, 2)

	value, err := machine.InvokeVirtual(task, "cancel", "()Z")
	if err != nil {
		t.Fatalf("TimerTask.cancel() error = %v", err)
	}
	if first, _ := value.Int32(); first != 1 {
		t.Errorf("first cancel() = %d, want 1", first)
	}
	value, err = machine.InvokeVirtual(task, "cancel", "()Z")
	if err != nil {
		t.Fatalf("TimerTask.cancel() error = %v", err)
	}
	if second, _ := value.Int32(); second != 0 {
		t.Errorf("second cancel() = %d, want 0", second)
	}

	time.Sleep(50 * time.Millisecond)
	settled := runs.Load()
	time.Sleep(50 * time.Millisecond)
	if final := runs.Load(); final != settled {
		t.Errorf("task ran %d more times after its own cancel", final-settled)
	}
}

// A schedule with a negative delay is a mistake in the title rather than a
// task that is due immediately, and the specification's exception is what says
// so where the mistake is.
func TestTimerRefusesANegativeDelay(t *testing.T) {
	var runs atomic.Int32
	machine := timerMachine(t, &runs)

	timer, err := machine.NewObject(TimerClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	task, err := machine.NewObject(countingTaskClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.InvokeVirtual(timer, "schedule", "(Ljava/util/TimerTask;J)V",
		jvm.ReferenceValue(task), jvm.LongValue(-1)); err == nil {
		t.Fatal("scheduling with a negative delay succeeded")
	}
	if runs.Load() != 0 {
		t.Errorf("refused schedule still ran the task %d times", runs.Load())
	}
}

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
