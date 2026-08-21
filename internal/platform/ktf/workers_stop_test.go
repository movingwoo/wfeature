package ktf

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// stubWorker is a worker whose goroutine stands in for a parked guest thread:
// it waits for a grant and, when the grant channel closes instead, does what a
// real unwind does — write the runtime's current thread and context back the
// way every deferred restore on the way out of handleSupervisorCall does — and
// returns. Two of these unwinding at once is exactly the race the detector
// reported at the end of a real session, so this file is worth running with
// -race; without it the ordering assertions below still hold.
func stubWorker(client *Client, unwind func()) *guestWorker {
	worker := &guestWorker{
		armThread: armcore.NewThread(armcore.NewContext()),
		grant:     make(chan struct{}),
		events:    make(chan workerEvent, 1),
		finished:  make(chan struct{}),
	}
	go func() {
		defer close(worker.finished)
		if _, ok := <-worker.grant; ok {
			return
		}
		unwind()
	}()
	return worker
}

// TestStoppedWorkersUnwindOneAtATime is the teardown's half of the strict
// handoff the tick loop keeps. Closing every grant channel at once woke every
// parked worker together, and each unwound its own nested call stack through
// the same two runtime fields with nothing ordering it against the others.
func TestStoppedWorkersUnwindOneAtATime(t *testing.T) {
	client, initialization := newPacedTestRuntime(t, NewManualClock(time.Unix(1700000000, 0)), 1)
	const workers = 8
	unwound := make(chan int, workers)
	for index := 0; index < workers; index++ {
		index := index
		worker := stubWorker(client, func() {
			previousThread, previousContext := initialization.currentThread, initialization.currentContext
			initialization.currentThread = armcore.NewThread(armcore.NewContext())
			initialization.currentContext = context.Background()
			// A restore is two writes with guest work between them, so the
			// window a second unwinding worker lands in is not one statement
			// wide. Yielding here makes the test's window the same shape.
			runtime.Gosched()
			initialization.currentThread, initialization.currentContext = previousThread, previousContext
			unwound <- index
		})
		client.workers = append(client.workers, worker)
	}

	client.StopThreads()

	// StopThreads returns only once the last worker has returned, so every
	// send has already landed and none of this has to wait for one.
	if count := len(unwound); count != workers {
		t.Fatalf("%d of %d workers had unwound when StopThreads returned, want all of them", count, workers)
	}
	if client.workers != nil {
		t.Fatalf("StopThreads left %d workers behind", len(client.workers))
	}
	// A Host may close a session twice — the CLI's defer and an explicit stop
	// — and the second close must not close a grant channel again.
	client.StopThreads()
}

// TestStopThreadsGivesUpOnAWorkerThatCannotAnswer keeps the teardown bounded.
// A worker that never returns is a bug elsewhere, but it must cost a session's
// close rather than the Host goroutine that is holding every other session.
func TestStopThreadsGivesUpOnAWorkerThatCannotAnswer(t *testing.T) {
	client, _ := newPacedTestRuntime(t, NewManualClock(time.Unix(1700000000, 0)), 1)
	client.unwindBound = 20 * time.Millisecond
	stuck := make(chan struct{})
	defer close(stuck)
	worker := &guestWorker{
		armThread: armcore.NewThread(armcore.NewContext()),
		grant:     make(chan struct{}),
		events:    make(chan workerEvent, 1),
		finished:  make(chan struct{}),
	}
	go func() {
		<-worker.grant
		// Never closes finished until the test lets go, which is the shape of
		// a guest that swallowed the abort.
		<-stuck
	}()
	client.workers = append(client.workers, worker)

	done := make(chan struct{})
	go func() {
		client.StopThreads()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StopThreads did not give up on a worker that never returned")
	}
}

// TestStoppedWorkerNeverGrantedStillReturns covers the worker adopted a moment
// before the session closed: its goroutine is still on its first receive, so
// it never sends an event, and a teardown that waited for one would wait out
// its whole bound.
func TestStoppedWorkerNeverGrantedStillReturns(t *testing.T) {
	client, _ := newPacedTestRuntime(t, NewManualClock(time.Unix(1700000000, 0)), 1)
	client.unwindBound = 2 * time.Second
	worker := &guestWorker{
		armThread: armcore.NewThread(armcore.NewContext()),
		grant:     make(chan struct{}),
		events:    make(chan workerEvent, 1),
		finished:  make(chan struct{}),
	}
	go worker.run(client)
	client.workers = append(client.workers, worker)

	start := time.Now()
	client.StopThreads()
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("StopThreads waited %v for a worker that had never run", waited)
	}
	select {
	case <-worker.finished:
	default:
		t.Fatal("the worker goroutine had not returned when StopThreads did")
	}
}
