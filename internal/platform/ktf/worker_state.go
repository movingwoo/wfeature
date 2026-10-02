package ktf

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Each worker contributes three caller roots, in queue order: its Thread or
// TimerTask, Timer owner, and painted card. The shared heap retains aliases.
type guestWorkerState struct {
	Root           armcore.RootThreadState
	StackBase      uint32
	Wake           heapDeadline
	PublishedFrame bool
	Continuation   *workerAOTCheckpoint
}

func cloneWorkerContinuation(saved *workerAOTCheckpoint) *workerAOTCheckpoint {
	if saved == nil {
		return nil
	}
	copy := *saved
	copy.Calls = slices.Clone(saved.Calls)
	return &copy
}

func (client *Client) captureWorkerStates(now time.Time) ([]guestWorkerState, []*jvm.Object, error) {
	if len(client.workers) > maxGuestWorkers || client.workerStackCount < 0 || client.workerStackCount > maxGuestWorkers || len(client.freeWorkerStacks) > maxGuestWorkers {
		return nil, nil, fmt.Errorf("KTF worker tables exceed limits")
	}
	saved := make([]guestWorkerState, 0, len(client.workers))
	roots := make([]*jvm.Object, 0, 3*len(client.workers))
	timers := 0
	for _, worker := range client.workers {
		if worker == nil || worker.javaThread == nil || worker.armThread == nil || len(worker.events) != 0 {
			return nil, nil, fmt.Errorf("KTF worker is missing its owner or has an unconsumed event")
		}
		select {
		case <-worker.finished:
			return nil, nil, fmt.Errorf("KTF worker has already finished")
		default:
		}
		root, err := client.core.CaptureRootThread(worker.armThread)
		if err != nil {
			return nil, nil, err
		}
		record := guestWorkerState{Root: root, StackBase: worker.stackBase, Wake: captureHeapDeadline(worker.wakeAt, now), PublishedFrame: worker.publishedFrame}
		if worker.continuation != nil {
			record.Continuation = cloneWorkerContinuation(worker.continuation)
		} else if worker.started {
			continuation, err := client.runtime.captureWorkerContinuation(worker)
			if err != nil {
				return nil, nil, err
			}
			record.Continuation = &continuation
		}
		if worker.timerOwner != nil {
			if client.runningTimerTasks[worker.timerOwner] != worker {
				return nil, nil, fmt.Errorf("KTF timer worker ownership is inconsistent")
			}
			timers++
		}
		saved = append(saved, record)
		roots = append(roots, worker.javaThread, worker.timerOwner, worker.paintedCard)
	}
	if len(client.runningTimerTasks) != timers {
		return nil, nil, fmt.Errorf("KTF timer table contains an unowned worker")
	}
	if err := client.validateWorkerStacks(saved, client.workerStackCount, client.freeWorkerStacks); err != nil {
		return nil, nil, err
	}
	return saved, roots, nil
}

func (client *Client) validateWorkerStacks(workers []guestWorkerState, count int, free []uint32) error {
	if count < 0 || count > maxGuestWorkers || len(workers)+len(free) != count {
		return fmt.Errorf("KTF worker stack partition is invalid")
	}
	seen := make(map[uint32]bool, count)
	check := func(base uint32) error {
		if base < workerStackBase || uint64(base-workerStackBase)%ThreadStackSize != 0 || uint64(base-workerStackBase)/ThreadStackSize >= uint64(count) || seen[base] {
			return fmt.Errorf("KTF worker stack address is duplicate or outside its pool")
		}
		seen[base] = true
		return client.core.Memory().ValidateRange(base, ThreadStackSize, armcore.PermissionReadWrite)
	}
	for _, worker := range workers {
		if err := check(worker.StackBase); err != nil {
			return err
		}
		sp := worker.Root.Context.Registers[armcore.RegisterSP]
		if !stackContains(worker.StackBase, sp) || !worker.Wake.Set && worker.Wake.Remaining != 0 {
			return fmt.Errorf("KTF worker root stack pointer or deadline is invalid")
		}
		if saved := worker.Continuation; saved != nil {
			if len(saved.Calls) == 0 || len(saved.Calls) > int(maxAOTCallDepth) || saved.Calls[0].ARM.EntryStack != sp {
				return fmt.Errorf("KTF worker continuation has a different root stack")
			}
			for _, call := range saved.Calls {
				if !stackContains(worker.StackBase, call.ARM.EntryStack) || !stackContains(worker.StackBase, call.ARM.Context.Registers[armcore.RegisterSP]) || call.ARM.Budget != client.workerSliceBudget() {
					return fmt.Errorf("KTF worker continuation exceeds its stack or execution policy")
				}
			}
		}
	}
	for _, base := range free {
		if err := check(base); err != nil {
			return err
		}
	}
	return nil
}

func stackContains(base, address uint32) bool {
	return address >= base && uint64(address)-uint64(base) <= ThreadStackSize && address&3 == 0
}

func (client *Client) workerSliceBudget() uint64 {
	if client.threadSliceSteps != 0 {
		return client.threadSliceSteps
	}
	return defaultThreadSliceSteps
}

// decodeWorkerStates validates every continuation and owner before starting
// any goroutine. Only a fully constructed detached client may launch them.
func (client *Client) decodeWorkerStates(saved []guestWorkerState, roots []*jvm.Object, now time.Time, nextExecution uint64) ([]*guestWorker, map[*jvm.Object]*guestWorker, error) {
	if len(saved) > maxGuestWorkers || len(roots) != 3*len(saved) {
		return nil, nil, fmt.Errorf("KTF worker heap roots are incomplete")
	}
	workers := make([]*guestWorker, 0, len(saved))
	timers := make(map[*jvm.Object]*guestWorker)
	executions := make(map[uint64]bool)
	for index, record := range saved {
		javaThread, owner, card := roots[3*index], roots[3*index+1], roots[3*index+2]
		if javaThread == nil || owner != nil && timers[owner] != nil {
			return nil, nil, fmt.Errorf("KTF worker has no Java owner or duplicates a Timer")
		}
		root, err := client.core.RestoreRootThread(record.Root, client.workerSliceBudget())
		if err != nil {
			return nil, nil, err
		}
		if continuation := record.Continuation; continuation != nil {
			if err := client.runtime.validateWorkerContinuation(*continuation); err != nil {
				return nil, nil, err
			}
			id := continuation.Execution.ID
			if id > nextExecution || executions[id] {
				return nil, nil, fmt.Errorf("KTF worker execution owner is duplicate or exceeds the JVM counter")
			}
			executions[id] = true
		}
		worker := &guestWorker{javaThread: javaThread, armThread: root, stackBase: record.StackBase,
			grant: make(chan struct{}), events: make(chan workerEvent, 1), finished: make(chan struct{}),
			timerOwner: owner, paintedCard: card, publishedFrame: record.PublishedFrame,
			wakeAt: record.Wake.restore(now), continuation: cloneWorkerContinuation(record.Continuation)}
		root.SetLimitHook(func(context.Context) error { return worker.parkSlice(true) })
		workers = append(workers, worker)
		if owner != nil {
			timers[owner] = worker
		}
	}
	return workers, timers, nil
}
