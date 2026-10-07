package midp

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

const timerCheckpointTaskClass = "net/wfeature/TimerCheckpointTask"

//go:embed java/net/wfeature/TimerCheckpointTask.class
var timerCheckpointTaskBytecode []byte

type timerCheckpointSource struct{}

func (timerCheckpointSource) ClassBytes(name string) ([]byte, bool) {
	return timerCheckpointTaskBytecode, name == timerCheckpointTaskClass
}

func timerCheckpointMachine(t *testing.T, now int64) *jvm.VM {
	t.Helper()
	machine := jvm.New(timerCheckpointSource{}, jvm.Options{
		Clock: func() int64 { return now },
		AsyncError: func(err error) {
			if !errors.Is(err, jvm.ErrClosed) {
				t.Errorf("timer worker failed: %v", err)
			}
		},
	})
	t.Cleanup(machine.Close)
	if err := Define(machine); err != nil {
		t.Fatal(err)
	}
	return machine
}

func timerCheckpointCount(t *testing.T, machine *jvm.VM, task *jvm.Object, name string) int32 {
	t.Helper()
	value, err := machine.Field(task, timerCheckpointTaskClass, name, "I")
	if err != nil {
		t.Fatal(err)
	}
	count, err := value.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func timerCheckpointWorker(t *testing.T, machine *jvm.VM) *jvm.Object {
	t.Helper()
	var found *jvm.Object
	for _, thread := range machine.ThreadObjects() {
		if thread.ClassName != TimerThreadClass {
			continue
		}
		if found != nil {
			t.Fatal("one schedule created multiple timer workers")
		}
		found = thread
	}
	if found == nil {
		t.Fatal("scheduled timer has no live worker")
	}
	return found
}

func TestTimerCheckpointResumesInsideTaskWithoutReplay(t *testing.T) {
	const scheduledAt = int64(1234567890000)
	source := timerCheckpointMachine(t, scheduledAt)
	timer, err := source.NewObject(TimerClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	task, err := source.NewObject(timerCheckpointTaskClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.InvokeVirtual(timer, "schedule", "(Ljava/util/TimerTask;J)V", jvm.ReferenceValue(task), jvm.LongValue(0)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var paused *jvm.ParkedThreads
	var saved jvm.ThreadCheckpointState
	for {
		paused, err = source.PauseThreads(ctx)
		if err != nil {
			t.Fatal(err)
		}
		saved, err = paused.CaptureState([]*jvm.Object{timer, task}, jvm.HeapCodec{})
		if err != nil {
			paused.Resume()
			t.Fatal(err)
		}
		if len(saved.Threads) == 1 && saved.Threads[0].Wait == "sleep" && timerCheckpointCount(t, source, task, "before") == 1 {
			break
		}
		paused.Resume()
		time.Sleep(time.Millisecond)
	}
	defer paused.Resume()
	frames := saved.Threads[0].Execution.Frames
	if len(frames) != 2 || frames[0].Class != TimerThreadClass || frames[1].Class != timerCheckpointTaskClass || saved.Threads[0].Remaining <= 0 {
		t.Fatalf("timer checkpoint lost worker or task continuation: %+v", saved.Threads[0])
	}

	fresh := timerCheckpointMachine(t, scheduledAt+999999)
	prepared, roots, err := fresh.PrepareThreadCheckpoint(saved, jvm.HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if len(roots) != 2 || roots[0] == timer || roots[1] == task || roots[0].ClassName != TimerClass || roots[1].ClassName != timerCheckpointTaskClass {
		t.Fatal("timer and task roots were not restored independently")
	}
	if before, after := timerCheckpointCount(t, fresh, roots[1], "before"), timerCheckpointCount(t, fresh, roots[1], "after"); before != 1 || after != 0 {
		t.Fatalf("detached task advanced or restarted: before=%d after=%d", before, after)
	}
	sourceWorker := timerCheckpointWorker(t, source)
	restoredWorker := timerCheckpointWorker(t, fresh)
	prepared.Start()
	for _, run := range []struct {
		machine *jvm.VM
		worker  *jvm.Object
	}{{source, sourceWorker}, {fresh, restoredWorker}} {
		if _, err := run.machine.InvokeVirtual(run.worker, "interrupt", "()V"); err != nil {
			t.Fatal(err)
		}
	}
	paused.Resume()

	for _, run := range []struct {
		machine *jvm.VM
		worker  *jvm.Object
		task    *jvm.Object
	}{{source, sourceWorker, task}, {fresh, restoredWorker, roots[1]}} {
		for {
			alive, err := run.machine.InvokeVirtual(run.worker, "isAlive", "()Z")
			if err != nil {
				t.Fatal(err)
			}
			if value, _ := alive.Int32(); value == 0 {
				break
			}
			if err := ctx.Err(); err != nil {
				t.Fatalf("timer worker did not finish after interruption: %v", err)
			}
			time.Sleep(time.Millisecond)
		}
		if before, after := timerCheckpointCount(t, run.machine, run.task, "before"), timerCheckpointCount(t, run.machine, run.task, "after"); before != 1 || after != 1 {
			t.Errorf("task continuation replayed or skipped: before=%d after=%d", before, after)
		}
		constructed, err := run.machine.StaticField(timerCheckpointTaskClass, "constructed", "I")
		if count, _ := constructed.Int32(); err != nil || count != 1 {
			t.Errorf("task construction replayed: constructed=%d, %v", count, err)
		}
		when, err := run.machine.InvokeVirtual(run.task, "scheduledExecutionTime", "()J")
		if millis, _ := when.Int64(); err != nil || millis != scheduledAt {
			t.Errorf("task scheduling replayed: scheduled=%d, %v, want %d", millis, err, scheduledAt)
		}
	}
}
