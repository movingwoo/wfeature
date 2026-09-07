package ktf

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// armTimerRecord arms one particular timer record through MC_knlSetTimer, the
// way guest code does. It takes the record rather than allocating one, because
// what is under test is what happens when the same record is armed again.
func armTimerRecord(t *testing.T, client *Client, runtime *initializationRuntime, record, callback, param uint32, delay uint64) error {
	t.Helper()
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, callback)
	if err := runtime.client.core.Memory().Write(record, word); err != nil {
		t.Fatal(err)
	}
	for register, value := range map[int]uint32{
		0: record,
		1: uint32(delay),
		2: uint32(delay >> 32),
		3: param,
	} {
		if err := client.thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	_, err := runtime.wipicSetTimer(client.thread)
	return err
}

// newArmedTimerRecord allocates a timer record and returns it with a callback
// address the runtime can be asked to call.
func newArmedTimerRecord(t *testing.T, runtime *initializationRuntime) (record, callback uint32) {
	t.Helper()
	record, err := runtime.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	return record, returningStub(t, runtime)
}

// TestRearmingATimerReplacesIt is the queue's identity rule. MC_knlUnsetTimer
// cancels by record address and removes every entry that names it, so one
// record is one timer; arming a record that is already queued therefore has to
// re-arm that timer rather than queue a second one. A title whose frame loop
// re-arms the same record every tick and never unsets it used to add an entry
// a tick until the queue hit its ceiling and the run died there, with 256
// queued entries that were all identical.
func TestRearmingATimerReplacesIt(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	record, callback := newArmedTimerRecord(t, runtime)

	if err := armTimerRecord(t, client, runtime, record, callback, 1, 50); err != nil {
		t.Fatalf("MC_knlSetTimer() error = %v", err)
	}
	if err := armTimerRecord(t, client, runtime, record, callback, 2, 70); err != nil {
		t.Fatalf("MC_knlSetTimer() re-arm error = %v", err)
	}
	if len(runtime.pendingTimers) != 1 {
		t.Fatalf("arming one record twice queued %d timers, want 1", len(runtime.pendingTimers))
	}

	// The re-arm is the timer that stands: its parameter and its delay are the
	// ones the second call declared, not the first.
	queued := runtime.pendingTimers[0]
	if queued.param != 2 || queued.delay != 70 {
		t.Fatalf("queued timer = param %d delay %d, want the re-arm's param 2 delay 70", queued.param, queued.delay)
	}
	if want := client.framePeriodDeadline(70 * time.Millisecond); !queued.due.Equal(want) {
		t.Fatalf("queued timer due at %v, want the re-arm's deadline %v", queued.due, want)
	}

	// A frame loop that re-arms every tick runs far past the queue ceiling,
	// which is what used to end these runs.
	for round := 0; round < 2*maxPendingTimers; round++ {
		if err := armTimerRecord(t, client, runtime, record, callback, 3, 50); err != nil {
			t.Fatalf("re-arm %d error = %v", round, err)
		}
	}
	if len(runtime.pendingTimers) != 1 {
		t.Fatalf("%d re-arms queued %d timers, want 1", 2*maxPendingTimers, len(runtime.pendingTimers))
	}

	// Cancelling the record still empties the queue, because there was only
	// ever the one timer in it.
	if err := client.thread.SetRegister(0, record); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.wipicUnsetTimer(client.thread); err != nil {
		t.Fatalf("MC_knlUnsetTimer() error = %v", err)
	}
	if len(runtime.pendingTimers) != 0 {
		t.Fatalf("unsetting the record left %d timers queued", len(runtime.pendingTimers))
	}
}

// TestDistinctTimerRecordsStayDistinct is the other half of the identity rule:
// replacing is by record, so two records are two timers however alike the
// delay and callback they were armed with are.
func TestDistinctTimerRecordsStayDistinct(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	first, callback := newArmedTimerRecord(t, runtime)
	second, _ := newArmedTimerRecord(t, runtime)

	for _, record := range []uint32{first, second, first, second} {
		if err := armTimerRecord(t, client, runtime, record, callback, 0, 50); err != nil {
			t.Fatalf("MC_knlSetTimer(%#x) error = %v", record, err)
		}
	}
	if len(runtime.pendingTimers) != 2 {
		t.Fatalf("two records armed twice each queued %d timers, want 2", len(runtime.pendingTimers))
	}
}

// TestRearmingATimerLeavesJavaTasksAlone is why the replacement asks for a
// WIPI C entry. A java/util/Timer task is queued in the same slice, and it is
// cancelled through its own Timer rather than by record address, so a kernel
// call must never take its slot.
func TestRearmingATimerLeavesJavaTasksAlone(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	record, callback := newArmedTimerRecord(t, runtime)

	task := &jvm.Object{ClassName: "test/Task"}
	runtime.pendingTimers = append(runtime.pendingTimers, wipicTimer{
		pointer: record,
		task:    task,
		owner:   &jvm.Object{ClassName: "java/util/Timer"},
		due:     client.waitDeadline(time.Second),
	})

	if err := armTimerRecord(t, client, runtime, record, callback, 0, 50); err != nil {
		t.Fatalf("MC_knlSetTimer() error = %v", err)
	}
	if len(runtime.pendingTimers) != 2 {
		t.Fatalf("arming a record queued %d timers, want the task and the timer", len(runtime.pendingTimers))
	}
	if runtime.pendingTimers[0].task != task {
		t.Fatal("arming a timer record took the scheduled task's slot")
	}
}
