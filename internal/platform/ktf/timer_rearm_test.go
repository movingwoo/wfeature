package ktf

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// armTimerRecord arms one particular timer record through MC_knlSetTimer, the
// way guest code does. It takes the record rather than allocating one, because
// what is under test is what happens when the same record is armed again.
func armTimerRecord(t *testing.T, client *Client, runtime *initializationRuntime, record, callback, param uint32, delay uint64) error {
	t.Helper()
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, callback)
	if err := runtime.client.Core().Memory().Write(record, word); err != nil {
		t.Fatal(err)
	}
	for register, value := range map[int]uint32{
		0: record,
		1: uint32(delay),
		2: uint32(delay >> 32),
		3: param,
	} {
		if err := client.Thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	_, err := runtime.WIPICSetTimer(client.Thread)
	return err
}

// newArmedTimerRecord allocates a timer record and returns it with a callback
// address the runtime can be asked to call.
func newArmedTimerRecord(t *testing.T, runtime *initializationRuntime) (record, callback uint32) {
	t.Helper()
	record, err := runtime.Allocate(4)
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
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	record, callback := newArmedTimerRecord(t, runtime)

	if err := armTimerRecord(t, client, runtime, record, callback, 1, 50); err != nil {
		t.Fatalf("MC_knlSetTimer() error = %v", err)
	}
	if err := armTimerRecord(t, client, runtime, record, callback, 2, 70); err != nil {
		t.Fatalf("MC_knlSetTimer() re-arm error = %v", err)
	}
	if len(runtime.PendingTimers) != 1 {
		t.Fatalf("arming one record twice queued %d timers, want 1", len(runtime.PendingTimers))
	}

	// The re-arm is the timer that stands: its parameter and its delay are the
	// ones the second call declared, not the first.
	queued := runtime.PendingTimers[0]
	if queued.Param != 2 || queued.Delay != 70 {
		t.Fatalf("queued timer = param %d delay %d, want the re-arm's param 2 delay 70", queued.Param, queued.Delay)
	}
	if want := client.FramePeriodDeadline(70 * time.Millisecond); !queued.Due.Equal(want) {
		t.Fatalf("queued timer due at %v, want the re-arm's deadline %v", queued.Due, want)
	}

	// A frame loop that re-arms every tick runs far past the queue ceiling,
	// which is what used to end these runs.
	for round := 0; round < 2*wipicrt.MaxPendingTimers; round++ {
		if err := armTimerRecord(t, client, runtime, record, callback, 3, 50); err != nil {
			t.Fatalf("re-arm %d error = %v", round, err)
		}
	}
	if len(runtime.PendingTimers) != 1 {
		t.Fatalf("%d re-arms queued %d timers, want 1", 2*wipicrt.MaxPendingTimers, len(runtime.PendingTimers))
	}

	// Cancelling the record still empties the queue, because there was only
	// ever the one timer in it.
	if err := client.Thread.SetRegister(0, record); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.WIPICUnsetTimer(client.Thread); err != nil {
		t.Fatalf("MC_knlUnsetTimer() error = %v", err)
	}
	if len(runtime.PendingTimers) != 0 {
		t.Fatalf("unsetting the record left %d timers queued", len(runtime.PendingTimers))
	}
}

// TestDistinctTimerRecordsStayDistinct is the other half of the identity rule:
// replacing is by record, so two records are two timers however alike the
// delay and callback they were armed with are.
func TestDistinctTimerRecordsStayDistinct(t *testing.T) {
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	first, callback := newArmedTimerRecord(t, runtime)
	second, _ := newArmedTimerRecord(t, runtime)

	for _, record := range []uint32{first, second, first, second} {
		if err := armTimerRecord(t, client, runtime, record, callback, 0, 50); err != nil {
			t.Fatalf("MC_knlSetTimer(%#x) error = %v", record, err)
		}
	}
	if len(runtime.PendingTimers) != 2 {
		t.Fatalf("two records armed twice each queued %d timers, want 2", len(runtime.PendingTimers))
	}
}

// TestRearmingATimerLeavesJavaTasksAlone is why the replacement asks for a
// WIPI C entry. A java/util/Timer task is queued in the same slice, and it is
// cancelled through its own Timer rather than by record address, so a kernel
// call must never take its slot.
func TestRearmingATimerLeavesJavaTasksAlone(t *testing.T) {
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	record, callback := newArmedTimerRecord(t, runtime)

	task := &jvm.Object{ClassName: "test/Task"}
	runtime.PendingTimers = append(runtime.PendingTimers, wipicrt.WIPICTimer{
		Pointer: record,
		Task:    guestValue(task),
		Owner:   guestValue(&jvm.Object{ClassName: "java/util/Timer"}),
		Due:     client.WaitDeadline(time.Second),
	})

	if err := armTimerRecord(t, client, runtime, record, callback, 0, 50); err != nil {
		t.Fatalf("MC_knlSetTimer() error = %v", err)
	}
	if len(runtime.PendingTimers) != 2 {
		t.Fatalf("arming a record queued %d timers, want the task and the timer", len(runtime.PendingTimers))
	}
	if guestObject(runtime.PendingTimers[0].Task) != task {
		t.Fatal("arming a timer record took the scheduled task's slot")
	}
}

// goTimerCallback gives a test a timer callback that is real guest code — a
// supervisor-call stub — and runs the Go function it is given when a service
// round calls it.
func goTimerCallback(t *testing.T, client *Client, runtime *initializationRuntime, name string, body func() error) uint32 {
	t.Helper()
	if err := client.vm.RegisterNative("test/TimerRound", name, "()V", func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
		return jvm.VoidValue(), body()
	}); err != nil {
		t.Fatal(err)
	}
	stub, err := runtime.runtimeJavaStub(runtimeJavaMethod{class: "test/TimerRound", name: name, descriptor: "()V", accessFlags: 9}, false)
	if err != nil {
		t.Fatal(err)
	}
	return stub
}

// kernelTimerCall runs MC_knlSetTimer or MC_knlUnsetTimer on a thread of its
// own, the way a callback's own guest code reaches them while the client
// thread is busy running it.
func kernelTimerCall(t *testing.T, call func(*armcore.Thread) (uint32, error), registers ...uint32) {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for register, value := range registers {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := call(thread); err != nil {
		t.Fatal(err)
	}
}

func queuedTimersFor(runtime *initializationRuntime, record uint32) []wipicrt.WIPICTimer {
	var found []wipicrt.WIPICTimer
	for _, timer := range runtime.PendingTimers {
		if guestObject(timer.Task) == nil && timer.Pointer == record {
			found = append(found, timer)
		}
	}
	return found
}

// TestTimerCallbackCancelsALaterTimerInTheSameRound covers MC_knlUnsetTimer on
// a timer that sits later in the batch the round is running. Those entries are
// out of the queue while the round runs, and the cancel used to miss them: a
// due timer ran although it had been cancelled, and one not yet due was put
// back to fire later.
func TestTimerCallbackCancelsALaterTimerInTheSameRound(t *testing.T) {
	for _, due := range []bool{true, false} {
		name := "due"
		if !due {
			name = "pending"
		}
		t.Run(name, func(t *testing.T) {
			clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
			client, runtime := newPacedTestRuntime(t, clock, 1)
			first, _ := newArmedTimerRecord(t, runtime)
			second, _ := newArmedTimerRecord(t, runtime)
			secondRan := 0
			cancel := goTimerCallback(t, client, runtime, "cancel", func() error {
				kernelTimerCall(t, runtime.WIPICUnsetTimer, second)
				return nil
			})
			cancelled := goTimerCallback(t, client, runtime, "cancelled", func() error {
				secondRan++
				return nil
			})
			if err := armTimerRecord(t, client, runtime, first, cancel, 0, 0); err != nil {
				t.Fatal(err)
			}
			delay := uint64(0)
			if !due {
				delay = 1000
			}
			if err := armTimerRecord(t, client, runtime, second, cancelled, 0, delay); err != nil {
				t.Fatal(err)
			}
			// Past the shortest frame period a zero delay is held to, and
			// well short of the one-second delay.
			clock.Advance(100 * time.Millisecond)
			if _, err := client.ServiceTimers(t.Context(), 16); err != nil {
				t.Fatal(err)
			}
			if secondRan != 0 {
				t.Fatalf("a timer cancelled earlier in the round ran %d times", secondRan)
			}
			if queued := queuedTimersFor(runtime, second); len(queued) != 0 {
				t.Fatalf("a timer cancelled earlier in the round is still queued: %d entries", len(queued))
			}
		})
	}
}

// TestTimerCallbackRearmsALaterTimerInTheSameRound covers MC_knlSetTimer on a
// timer later in the batch: the re-arm replaces it, so the record stays one
// timer with the deadline the re-arm declared, instead of a new entry beside
// the stale one that the round then put back as well.
func TestTimerCallbackRearmsALaterTimerInTheSameRound(t *testing.T) {
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	first, _ := newArmedTimerRecord(t, runtime)
	second, secondCallback := newArmedTimerRecord(t, runtime)
	rearm := goTimerCallback(t, client, runtime, "rearm", func() error {
		kernelTimerCall(t, runtime.WIPICSetTimer, second, 5000, 0, 7)
		return nil
	})
	if err := armTimerRecord(t, client, runtime, first, rearm, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := armTimerRecord(t, client, runtime, second, secondCallback, 3, 1000); err != nil {
		t.Fatal(err)
	}
	// Past the shortest frame period a zero delay is held to, and well
	// short of the one-second delays.
	clock.Advance(100 * time.Millisecond)
	if _, err := client.ServiceTimers(t.Context(), 16); err != nil {
		t.Fatal(err)
	}
	queued := queuedTimersFor(runtime, second)
	if len(queued) != 1 {
		t.Fatalf("re-arming a later timer left %d entries for it, want 1", len(queued))
	}
	if queued[0].Param != 7 || queued[0].Delay != 5000 {
		t.Fatalf("queued timer = param %d delay %d, want the re-arm's param 7 delay 5000", queued[0].Param, queued[0].Delay)
	}
}

// TestFailingTimerCallbackKeepsTheRestOfTheRound is the uncaught-callback rule
// for C timers: a callback that ended in an error did not happen, and the
// timers queued behind it are still there for the next round rather than
// silently dropped with it.
func TestFailingTimerCallbackKeepsTheRestOfTheRound(t *testing.T) {
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	first, _ := newArmedTimerRecord(t, runtime)
	second, secondCallback := newArmedTimerRecord(t, runtime)
	third, thirdCallback := newArmedTimerRecord(t, runtime)
	failure := errors.New("callback failed")
	fail := goTimerCallback(t, client, runtime, "fail", func() error { return failure })
	if err := armTimerRecord(t, client, runtime, first, fail, 0, 0); err != nil {
		t.Fatal(err)
	}
	// One due behind the failure and one not yet due: neither has run.
	if err := armTimerRecord(t, client, runtime, second, secondCallback, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := armTimerRecord(t, client, runtime, third, thirdCallback, 0, 1000); err != nil {
		t.Fatal(err)
	}
	clock.Advance(100 * time.Millisecond)
	if _, err := client.ServiceTimers(t.Context(), 16); !errors.Is(err, failure) {
		t.Fatalf("ServiceTimers() error = %v, want the callback's failure", err)
	}
	for _, record := range []uint32{second, third} {
		if queued := queuedTimersFor(runtime, record); len(queued) != 1 {
			t.Fatalf("timer %#x queued behind a failing callback has %d entries, want 1", record, len(queued))
		}
	}
	if queued := queuedTimersFor(runtime, first); len(queued) != 0 {
		t.Fatalf("the one-shot that failed is queued again: %d entries", len(queued))
	}
}
