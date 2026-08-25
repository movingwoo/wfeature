package lgt

import (
	"context"
	"testing"
	"time"
)

// A monitor counts: the same runner may take a lock it already holds, and it is
// only free again once it has given back as many levels as it took. A
// `synchronized` method that calls another one on the same object depends on it.
func TestJavaMonitorCountsRecursiveEntries(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	const object = 0x1234
	for level := 0; level < 3; level++ {
		if err := client.javaMonitorEnter(ctx, object); err != nil {
			t.Fatalf("entry %d: %v", level, err)
		}
	}
	if held := client.javaRuntimeState().monitors[object]; held == nil || held.count != 3 {
		t.Fatalf("the monitor is held %v, want three levels", held)
	}
	for level := 0; level < 3; level++ {
		if err := client.javaMonitorExit(object); err != nil {
			t.Fatalf("exit %d: %v", level, err)
		}
	}
	if held := client.javaRuntimeState().monitors[object]; held != nil {
		t.Errorf("the monitor is still held after as many exits as entries")
	}
	if err := client.javaMonitorExit(object); err == nil {
		t.Error("leaving a monitor that was never taken was accepted")
	}
}

// A thread that ends gives back whatever it still held. A thread that failed
// inside a synchronized body would otherwise leave the lock shut for good.
func TestJavaMonitorsAreReleasedWhenAThreadEnds(t *testing.T) {
	client := fixtureClient(t)
	worker := &javaWorker{}
	client.activeJavaWorker = worker
	if err := client.javaMonitorEnter(context.Background(), 0x2000); err != nil {
		t.Fatal(err)
	}
	if worker.monitors != 1 {
		t.Fatalf("the thread holds %d locks, want 1", worker.monitors)
	}
	client.activeJavaWorker = nil
	client.releaseJavaMonitors(worker)
	if held := client.javaRuntimeState().monitors[0x2000]; held != nil {
		t.Error("the lock survived the thread that held it")
	}
	if worker.monitors != 0 {
		t.Errorf("the thread still counts %d locks", worker.monitors)
	}
}

// A sleep made where no guest thread is running has nothing to park, and moves
// the guest's clock instead — the wait the caller asked for still passes.
func TestJavaSleepOutsideAThreadAdvancesTheClock(t *testing.T) {
	client := fixtureClient(t)
	before := client.clock.now()
	if _, err := javaThreadSleep(client, nil, nil, []uint32{50, 0}); err != nil {
		t.Fatalf("javaThreadSleep() error = %v", err)
	}
	if moved := client.clock.now() - before; moved < 50*1e6 {
		t.Errorf("the clock moved %v, want at least 50ms", moved)
	}
}

// A yield parks only when another thread could take the turn. Parking ends the
// slice, and a slice is granted once a tick, so a yield with nobody to hand to
// costs a whole tick of guest time — a third of one local title's frame rate,
// since its loop yields once per frame.
func TestAYieldOnlyParksWhenSomethingElseCanRun(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	alone := &javaWorker{}
	runtime.workers = []*javaWorker{alone}
	if client.otherJavaWorkerReady(alone) {
		t.Error("a lone thread was told something else could run")
	}

	// A second thread that is runnable now is somewhere to hand the turn to.
	other := &javaWorker{}
	runtime.workers = []*javaWorker{alone, other}
	if !client.otherJavaWorkerReady(alone) {
		t.Error("a runnable second thread was not seen")
	}

	// One that is finished, or parked on a deadline that has not arrived, is
	// not.
	other.done = true
	if client.otherJavaWorkerReady(alone) {
		t.Error("a finished thread was counted as runnable")
	}
	other.done, other.wakeAt = false, client.clock.now()+time.Hour
	if client.otherJavaWorkerReady(alone) {
		t.Error("a thread sleeping for an hour was counted as runnable")
	}
	// And one whose deadline has passed is.
	client.clock.advance(time.Second)
	other.wakeAt = 10 * time.Millisecond
	if !client.otherJavaWorkerReady(alone) {
		t.Error("a thread whose wait has passed was not seen")
	}
}

// A guest thread that has ended is never granted another slice. Its goroutine
// is gone, so the grant would block for ever and take the session with it — a
// hang with no error and no last frame. The end is recorded where the slice is
// granted rather than by the caller, because one of the three callers is a
// monitor wait, where the thread that ends is not the one the caller had in
// hand: a title whose `run` threw while holding a lock deadlocked the next tick
// that way.
func TestAFinishedGuestThreadIsNotGrantedAnotherSlice(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	worker := &javaWorker{
		grant:  make(chan context.Context),
		events: make(chan javaWorkerEvent, 1),
	}
	runtime.workers = append(runtime.workers, worker)
	// The worker's own goroutine: it takes one slice, ends inside it, files its
	// last event and stops receiving — which is what `runJavaWorker` does when
	// the guest's `run` returns or throws.
	go func() {
		<-worker.grant
		worker.events <- javaWorkerEvent{done: true}
	}()

	event, err := client.grantJavaSlice(context.Background(), worker)
	if err != nil {
		t.Fatalf("granting a slice to a thread that is ending: %v", err)
	}
	if !event.done {
		t.Fatal("the event that ended the thread was not reported as the end")
	}
	if !worker.done {
		t.Fatal("the thread was not recorded as finished, so it can be granted again")
	}

	// A second grant must answer rather than block. Nothing is receiving on the
	// grant channel any more, so without the record above this blocks for ever.
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if event, err := client.grantJavaSlice(context.Background(), worker); err != nil || !event.done {
			t.Errorf("a second grant answered %+v (%v)", event, err)
		}
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("a second grant to a finished thread blocked")
	}
}
