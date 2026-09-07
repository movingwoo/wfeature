package ktf

import (
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// newFrameLoopTestWorker stands in for a guest thread without starting one.
// Its grant channel is closed from the outset, so the park a declared wait
// ends in returns immediately instead of waiting for a slice no Host is going
// to hand out. The wait's own deadline is written onto the worker either way,
// and that deadline is what these tests are about.
func newFrameLoopTestWorker(client *Client) *guestWorker {
	worker := &guestWorker{
		armThread: armcore.NewThread(armcore.NewContext()),
		grant:     make(chan struct{}),
		events:    make(chan workerEvent, 1),
	}
	close(worker.grant)
	client.workers = append(client.workers, worker)
	client.activeWorker = worker
	return worker
}

// sleepOnTestWorker declares a guest sleep of milliseconds on the worker whose
// slice is granted, the way Thread.sleep does.
func sleepOnTestWorker(t *testing.T, runtime *initializationRuntime, milliseconds int64) {
	t.Helper()
	_, err := runtimeThreadSleep(runtime, nil, []jvm.Value{jvm.LongValue(milliseconds)})
	if err != nil && !errors.Is(err, errWorkersStopped) {
		t.Fatalf("Thread.sleep(%d) error = %v", milliseconds, err)
	}
}

// TestAFrameLoopSleepIsRaisedToTheFramePeriod is the pacing rule for the third
// shape of frame loop: not a timer, not a card's paint, but a guest thread
// that publishes a frame and then declares its own wait.
//
// A game asking for Thread.sleep(0) there is asking for a frame as soon as one
// can be had, which is a request the platform answers at its own resolution —
// the same floor a timer's period gets. Answered literally it is not a fast
// game but an unbounded one: the loop takes the whole of its thread's slice
// every round and publishes thousands of frames for each one a Host collects.
func TestAFrameLoopSleepIsRaisedToTheFramePeriod(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	if _, err := runtime.wipicGetScreenFramebuffer(); err != nil {
		t.Fatal(err)
	}
	worker := newFrameLoopTestWorker(client)

	// The loop: publish a frame, then ask to wait for no time at all.
	if err := runtime.presentScreen(); err != nil {
		t.Fatalf("presentScreen() error = %v", err)
	}
	sleepOnTestWorker(t, runtime, 0)

	want := clock.Now().Add(minGuestFramePeriod)
	if !worker.wakeAt.Equal(want) {
		t.Fatalf("a frame loop's Thread.sleep(0) parks until %v, want %v", worker.wakeAt, want)
	}
}

// TestASleepThatFollowsNoFrameIsTheSleepTheGuestAsked covers the other half of
// the same rule, and it is the half that keeps the floor from becoming a tax
// on everything else. A thread that has not published a frame is not in a
// frame loop, whatever else it is doing: a loader that sleeps between chunks
// of work asked to sleep, and raising each of those waits to a frame would
// turn a few hundred short waits into a few hundred frames of load time.
func TestASleepThatFollowsNoFrameIsTheSleepTheGuestAsked(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	if _, err := runtime.wipicGetScreenFramebuffer(); err != nil {
		t.Fatal(err)
	}
	worker := newFrameLoopTestWorker(client)

	// No frame published: a zero wait is a zero wait, and parks nothing.
	sleepOnTestWorker(t, runtime, 0)
	if !worker.wakeAt.IsZero() {
		t.Fatalf("Thread.sleep(0) outside a frame loop parked until %v, want no park", worker.wakeAt)
	}

	// A frame published, but a wait longer than the floor: the length the
	// guest asked for stands.
	if err := runtime.presentScreen(); err != nil {
		t.Fatalf("presentScreen() error = %v", err)
	}
	sleepOnTestWorker(t, runtime, 500)
	if want := clock.Now().Add(500 * time.Millisecond); !worker.wakeAt.Equal(want) {
		t.Fatalf("a frame loop's Thread.sleep(500) parks until %v, want %v", worker.wakeAt, want)
	}

	// The frame is spent: the next zero wait is a zero wait again, because one
	// published frame makes one wait a frame period rather than every wait
	// after it.
	worker.wakeAt = time.Time{}
	sleepOnTestWorker(t, runtime, 0)
	if !worker.wakeAt.IsZero() {
		t.Fatalf("a second Thread.sleep(0) after one frame parked until %v, want no park", worker.wakeAt)
	}
}
