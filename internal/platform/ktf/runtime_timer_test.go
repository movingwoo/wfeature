package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// The schedule forms that name a Date queue the task exactly as the delay
// forms queue it for the delay from the guest's clock to that date: a date
// already gone is a delay of nothing, the period is the delay forms' own, and the
// fixed-rate form schedules the same way here as the delay forms' does. A null
// task or date is the NullPointerException the call dereferences it into, as
// SKT answers these forms.
func TestTimerScheduleAtADateRunsFromTheGuestClock(t *testing.T) {
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	vm := client.JVM()
	timer := jvm.ReferenceValue(&jvm.Object{ClassName: runtimeTimerClass})
	task := jvm.ReferenceValue(&jvm.Object{ClassName: "test/Task"})
	date := func(offset int64) jvm.Value {
		t.Helper()
		object, err := vm.NewObject("java/util/Date", "(J)V", jvm.LongValue(runtime.GuestMillis()+offset))
		if err != nil {
			t.Fatal(err)
		}
		return jvm.ReferenceValue(object)
	}
	forms := map[string]runtimeJavaImplementation{}
	for _, method := range runtimeTimerClassDefinition().methods {
		forms[method.name+method.descriptor] = method.implementation
	}
	schedule := func(form string, arguments ...jvm.Value) wipicrt.WIPICTimer {
		t.Helper()
		implementation := forms[form]
		if implementation == nil {
			t.Fatalf("Timer has no %s", form)
		}
		if _, err := implementation(runtime, vm, append([]jvm.Value{timer, task}, arguments...)); err != nil {
			t.Fatalf("%s error = %v", form, err)
		}
		queued := runtime.PendingTimers[len(runtime.PendingTimers)-1]
		runtime.PendingTimers = runtime.PendingTimers[:len(runtime.PendingTimers)-1]
		return queued
	}
	const (
		atDate       = "schedule(Ljava/util/TimerTask;Ljava/util/Date;)V"
		atDateRepeat = "schedule(Ljava/util/TimerTask;Ljava/util/Date;J)V"
		atDateRate   = "scheduleAtFixedRate(Ljava/util/TimerTask;Ljava/util/Date;J)V"
	)
	ahead, delayed := schedule(atDate, date(250)), schedule("schedule(Ljava/util/TimerTask;J)V", jvm.LongValue(250))
	if !ahead.Due.Equal(delayed.Due) || ahead.Due.Sub(client.Now()) != 250*time.Millisecond || ahead.Period != 0 {
		t.Fatalf("a date 250 ms ahead is due %v from now with period %v; the delay form %v", ahead.Due.Sub(client.Now()), ahead.Period, delayed.Due.Sub(client.Now()))
	}
	if guestObject(ahead.Task) == nil || guestObject(ahead.Owner) == nil {
		t.Fatal("the queued task lost its task or its timer")
	}
	// A date already gone is a delay of nothing, which this platform's timer
	// service still holds to its shortest frame period.
	if gone, now := schedule(atDate, date(-5000)), schedule("schedule(Ljava/util/TimerTask;J)V", jvm.LongValue(0)); !gone.Due.Equal(now.Due) {
		t.Fatalf("a date already gone is due %v from now, want %v as a zero delay is", gone.Due.Sub(client.Now()), now.Due.Sub(client.Now()))
	}
	for _, form := range []string{atDateRepeat, atDateRate} {
		repeating := schedule(form, date(100), jvm.LongValue(40))
		if repeating.Due.Sub(client.Now()) != 100*time.Millisecond || repeating.Period != 40*time.Millisecond {
			t.Fatalf("%s is due %v from now every %v, want 100ms and 40ms", form, repeating.Due.Sub(client.Now()), repeating.Period)
		}
	}
	for name, arguments := range map[string][]jvm.Value{
		"a null task": {timer, jvm.ReferenceValue(nil), date(10)},
		"a null date": {timer, task, jvm.ReferenceValue(nil)},
	} {
		if _, err := forms[atDate](runtime, vm, arguments); !vm.IsGuestException(err, "java/lang/NullPointerException") {
			t.Errorf("schedule with %s = %v, want NullPointerException", name, err)
		}
	}
	if len(runtime.PendingTimers) != 0 {
		t.Fatalf("a refused schedule queued %d timers", len(runtime.PendingTimers))
	}
}
