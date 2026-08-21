package midp

import (
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
