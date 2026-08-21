package ktf

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Guest Java threads each run their run() method on a dedicated goroutine
// with a private ARM stack and thread-local state. Execution hands off
// strictly: the Host grants one step slice and blocks until the worker parks
// (step budget exhausted or a guest sleep), finishes, or fails, so runtime
// state is never touched by two goroutines at once. Parking freezes the
// worker's whole nested Go/guest call stack, which is what lets a game whose
// main loop never returns keep progressing tick after tick.

const (
	// workerStackBase places per-worker guest stacks above the root client
	// stack and below the platform data region at 0x30000000; 64 workers of
	// 1MB each end at 0x24100000.
	workerStackBase = ThreadStackBase + uint32(ThreadStackSize)

	// defaultThreadSliceSteps bounds one worker's ARM steps per Host tick. A
	// loop that sleeps parks earlier; a loop that never sleeps still yields.
	defaultThreadSliceSteps = 2_000_000

	maxGuestWorkers = maxPendingThreads
)

var errWorkersStopped = errors.New("KTF guest threads are stopped")

type workerEvent struct {
	done bool
	err  error
}

type guestWorker struct {
	javaThread *jvm.Object
	armThread  *armcore.Thread
	stackBase  uint32
	grant      chan struct{}
	events     chan workerEvent
	// finished is closed when the worker goroutine returns, whatever ended it.
	// StopThreads waits on it so one worker has finished unwinding before the
	// next is woken; see there for why the close needs that order.
	finished chan struct{}
	// timerOwner is the java/util/Timer this worker is running a task for, and
	// nil for a guest Thread. It is what releases that Timer's thread when the
	// task returns.
	timerOwner *jvm.Object
	// wakeAt is the instant this worker becomes eligible for another slice.
	// A worker that parked because its step budget ran out leaves it in the
	// past and is granted again immediately; one that parked on a guest wait
	// carries the deadline that wait asked for. Only the worker goroutine
	// writes it and only while it holds the grant, and the grant and events
	// channels order that write against the Host's read in ServiceThreads.
	wakeAt time.Time
}

// newGuestWorker maps (or reuses) a private guest stack, derives the worker's
// logical ARM thread with the slice budget and park hook, and starts the
// goroutine that will invoke run() on the first grant.
func (client *Client) newGuestWorker(javaThread *jvm.Object) (*guestWorker, error) {
	var stackBase uint32
	if count := len(client.freeWorkerStacks); count > 0 {
		stackBase = client.freeWorkerStacks[count-1]
		client.freeWorkerStacks = client.freeWorkerStacks[:count-1]
	} else {
		if client.workerStackCount >= maxGuestWorkers {
			return nil, fmt.Errorf("KTF guest worker count exceeds %d", maxGuestWorkers)
		}
		stackBase = workerStackBase + uint32(client.workerStackCount)*uint32(ThreadStackSize)
		if err := client.core.Memory().Map(stackBase, ThreadStackSize, armcore.PermissionReadWrite); err != nil {
			return nil, fmt.Errorf("map KTF guest worker stack: %w", err)
		}
		client.workerStackCount++
	}
	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = stackBase + uint32(ThreadStackSize)
	worker := &guestWorker{
		javaThread: javaThread,
		armThread:  armcore.NewThread(initial),
		stackBase:  stackBase,
		grant:      make(chan struct{}),
		events:     make(chan workerEvent, 1),
		finished:   make(chan struct{}),
	}
	slice := client.threadSliceSteps
	if slice == 0 {
		slice = defaultThreadSliceSteps
	}
	worker.armThread.SetStepBudget(slice)
	worker.armThread.SetLimitHook(func(context.Context) error { return worker.park() })
	go worker.run(client)
	return worker, nil
}

// park reports the worker as still running and blocks until the Host grants
// the next slice. A closed grant channel aborts the guest run.
func (worker *guestWorker) park() error {
	worker.events <- workerEvent{}
	if _, ok := <-worker.grant; !ok {
		return errWorkersStopped
	}
	return nil
}

func (worker *guestWorker) run(client *Client) {
	// Closed on every exit, including the one below where the worker is
	// stopped before it ever ran: StopThreads waits for this and would wait
	// out its whole bound on a worker that returned without saying so.
	defer close(worker.finished)
	if _, ok := <-worker.grant; !ok {
		return
	}
	// The run is wrapped rather than deferred over the whole function because
	// what follows it has to happen either way: a panicking guest thread is
	// still a thread that ended, and a title waiting on isAlive would hang for
	// ever if the panic skipped it. See backend.GuestPanic for why a panic
	// becomes this worker's error instead of the process's exit.
	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = backend.GuestPanic(client.logger, "KTF guest thread", recovered)
			}
		}()
		_, err = client.vm.InvokeVirtual(worker.javaThread, "run", "()V")
	}()
	// The worker is this platform's answer to Thread.start, so it is also the
	// only place that knows the run is over. A title whose loading screen
	// waits on isAlive never leaves it otherwise.
	client.vm.EndGuestThread(worker.javaThread)
	worker.events <- workerEvent{done: true, err: err}
}

// yieldCurrentWorker parks the worker whose slice is currently granted, if
// guest code is running on one, and lets it run again on the next tick.
// While a worker runs the Host goroutine is blocked on its events channel, so
// activeWorker identifies the caller without comparing nested derived threads.
func (runtime *initializationRuntime) yieldCurrentWorker() error {
	return runtime.sleepCurrentWorker(0)
}

// sleepCurrentWorker parks the worker whose slice is currently granted until
// wait has elapsed on the session clock. Runtime natives call it when the
// guest declares a wait — Thread.sleep, an idle event-queue poll — so the wait
// the game asked for is the wait it gets, which is the whole of how a game's
// speed is set.
//
// A wait declared outside a worker cannot park anything: the caller is the
// client thread, and the Host goroutine is inside that call, so blocking would
// freeze the Host rather than the guest. This is not a rare case — a title
// can run its whole frame loop inside the card's paint and pace it with a
// sleep there — so the wait is deferred to the client thread instead of dropped: it
// returns at once and the Host holds off the client thread's next frame work
// for that long. The guest gets its frame time either side of the call rather
// than inside it, which is the same rate.
func (runtime *initializationRuntime) sleepCurrentWorker(wait time.Duration) error {
	if runtime == nil || runtime.client == nil {
		return nil
	}
	worker := runtime.client.activeWorker
	if worker == nil {
		runtime.client.deferClientWait(wait)
		return nil
	}
	worker.wakeAt = runtime.client.waitDeadline(wait)
	return worker.park()
}

// stopUnwindBound is how long StopThreads waits for one worker to finish
// unwinding before it gives up on ordering the rest. A worker that parked
// answers errWorkersStopped and returns through Go frames only, so this is
// far longer than the close takes; it is here so a guest that somehow swallows
// the abort costs a session teardown rather than the Host goroutine.
const stopUnwindBound = 5 * time.Second

// StopThreads aborts every guest worker; parked workers wake, fail their run,
// and exit. Hosts call it when tearing a session down.
//
// The workers are woken **one at a time**, and each is waited out before the
// next is touched. A worker unwinding its nested Go and guest call stack runs
// the same deferred restores of the runtime's current thread and context that
// running guest code does, so waking them together puts two goroutines inside
// the runtime at once — which the tick loop's strict handoff otherwise never
// does, and which the race detector reported over a third of the local corpus.
// Waiting here gives the close the ordering the tick loop gets from receiving
// one worker's event before granting the next.
func (client *Client) StopThreads() {
	if client == nil {
		return
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.workersStopped {
		return
	}
	client.workersStopped = true
	workers := client.workers
	client.workers = nil
	for _, worker := range workers {
		close(worker.grant)
		client.waitForWorker(worker)
	}
}

// waitForWorker blocks until one aborted worker's goroutine has returned.
// Events sent on the way out are drained rather than ignored: a park that
// races the abort blocks on a full events channel, and a worker blocked there
// never reaches its own return.
func (client *Client) waitForWorker(worker *guestWorker) {
	if worker.finished == nil {
		return
	}
	bound := client.unwindBound
	if bound <= 0 {
		bound = stopUnwindBound
	}
	timer := time.NewTimer(bound)
	defer timer.Stop()
	for {
		select {
		case <-worker.finished:
			return
		case <-worker.events:
		case <-timer.C:
			if client.logger != nil {
				class := ""
				if worker.javaThread != nil {
					class = worker.javaThread.ClassName
				}
				client.logger.Warn("KTF guest thread did not unwind when the session closed",
					"class", class, "waited", bound)
			}
			return
		}
	}
}
