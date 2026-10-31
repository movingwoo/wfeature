package ktf

import (
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

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
