package ktf

import (
	"context"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// fakeWorker queues a worker whose goroutine answers a grant the way a real
// one does — with an event — without running any guest code. Whether it
// reports itself finished is the whole of what these tests vary, because that
// is what decides whether the grant was a slice of the round or a thread
// leaving.
func fakeWorker(client *Client, name string, finishes bool) *guestWorker {
	worker := &guestWorker{
		javaThread: &jvm.Object{ClassName: name},
		armThread:  armcore.NewThread(armcore.NewContext()),
		grant:      make(chan struct{}),
		events:     make(chan workerEvent, 1),
		finished:   make(chan struct{}),
	}
	go func() {
		defer close(worker.finished)
		for range worker.grant {
			worker.events <- workerEvent{done: finishes}
			if finishes {
				return
			}
		}
	}()
	client.workers = append(client.workers, worker)
	return worker
}

// TestFinishedThreadsDoNotQueueBehindTheRoundLimit is the failure one title
// found: it starts a thread per sound effect, two to a round, and a round that
// retired one left the queue a thread longer every time until it hit the
// worker ceiling and the run ended. A thread that returns from run() is not
// competing for the next round, so retiring it is not what the limit shares
// out.
func TestFinishedThreadsDoNotQueueBehindTheRoundLimit(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, _ := newPacedTestRuntime(t, clock, 1)
	const started = 8
	for index := 0; index < started; index++ {
		fakeWorker(client, "SoundThread", true)
	}

	serviced, err := client.ServiceThreads(context.Background(), 1)
	if err != nil {
		t.Fatalf("ServiceThreads() error = %v", err)
	}
	if serviced != started {
		t.Fatalf("serviced %d finished workers in one round, want %d", serviced, started)
	}
	if len(client.workers) != 0 {
		t.Fatalf("%d workers left queued after they all finished, want 0", len(client.workers))
	}
	if len(client.freeWorkerStacks) != started {
		t.Fatalf("reclaimed %d worker stacks, want %d", len(client.freeWorkerStacks), started)
	}
}

// TestRunningThreadsStillShareTheRoundLimit is the other half of the same
// rule, and the pacing the limit is there for: a worker that parked is still
// running, so it takes one of the round's grants and the queue behind it waits
// for the next round.
func TestRunningThreadsStillShareTheRoundLimit(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, _ := newPacedTestRuntime(t, clock, 1)
	for index := 0; index < 4; index++ {
		fakeWorker(client, "LoopThread", false)
	}
	t.Cleanup(client.StopThreads)

	serviced, err := client.ServiceThreads(context.Background(), 1)
	if err != nil {
		t.Fatalf("ServiceThreads() error = %v", err)
	}
	if serviced != 1 {
		t.Fatalf("serviced %d running workers with a limit of 1, want 1", serviced)
	}
	if len(client.workers) != 4 {
		t.Fatalf("%d workers queued after a round, want 4", len(client.workers))
	}
}
