package midp

import (
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
