package lgt

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// What the threads' scheduling holds beyond the Java record: the timers and
// their tasks (java_timer.go), and the interrupts threads have not yet been
// thrown (java_thread.go).
//
// **It is a record of its own, wrapping the session record**, for the reason
// the class library's and the screen's are (checkpoint_java_library.go,
// checkpoint_java_screen.go): the checkpoint codec reads a strict schema, so a
// field added to the thread or the Java record would make every quick save an
// earlier build took unreadable. Version 7 carries the session record and the
// media, class-library and screen records (the cards' version 6 screen state
// included) beside this one; a session holding none of it is written in the
// shape an earlier build reads, and every earlier shape still loads, with no
// timer and no interrupt, which is what a build that kept neither left.
//
// A timer's run in progress needs nothing here beyond the timer: it is a guest
// thread, recorded with the rest of them, and its Thread record names the task
// it runs.
type sessionSchedulingCheckpointState struct {
	Version    uint32
	Session    sessionCheckpointState
	Media      *javaMediaState
	Library    *javaLibraryState
	Screen     *javaScreenState
	Scheduling javaSchedulingState
}

const sessionSchedulingCheckpointVersion = 7

type javaSchedulingState struct {
	// Roots is the platform block the timers' references are kept in, and
	// Order the number the latest schedule was given.
	Roots  uint32
	Order  uint64
	Timers []javaTimerState
	Tasks  []javaTimerTaskState
	// Interrupted is every Thread holding an interrupt it has not been thrown,
	// in the order of the objects.
	Interrupted []uint32
}

// javaTimerState is one timer, with the tasks queued on it in their order.
type javaTimerState struct {
	Object    uint32
	Cancelled bool
	Thread    uint32
	Queue     []uint32
}

// javaTimerTaskState is one task a title scheduled or cancelled.
type javaTimerTaskState struct {
	Object, Timer uint32
	State         uint8
	Next, Period  time.Duration
	FixedRate     bool
	Last          int64
	Order         uint64
}

// captureJavaSchedulingState records the timers, the tasks and the pending
// interrupts, or answers nil when there is none of them, which is what keeps
// such a session's record in the earlier shape.
//
// **What the collector has taken is left out.** It frees an object without
// telling this platform, so a timer or a task this platform still has an
// entry for may be gone, its address handed to something else; neither has
// work — what has work is held in the timers' block — and an entry for an
// address that is no longer a Timer or a TimerTask is one nothing can reach.
func (client *Client) captureJavaSchedulingState() (*javaSchedulingState, error) {
	runtime := client.javaRun
	if runtime == nil {
		return nil, nil
	}
	saved := &javaSchedulingState{Roots: runtime.timerRoots, Order: runtime.timerOrder}
	held := map[uint32]bool{}
	for _, object := range slices.Sorted(maps.Keys(runtime.timers)) {
		timer := runtime.timers[object]
		if timer == nil {
			return nil, fmt.Errorf("LGT checkpoint has a missing timer")
		}
		work := client.javaTimerHasWork(timer)
		if !work && !client.javaObjectLive(object, javaTimerClass) {
			continue
		}
		record := javaTimerState{Object: object, Cancelled: timer.cancelled, Queue: slices.Clone(timer.queue)}
		if thread := client.javaTimerThreadRecord(timer); thread != nil {
			record.Thread = thread.object
			if client.javaTimerRunning(timer) {
				held[thread.runnable] = true
			}
		}
		if !work && !record.Cancelled && record.Thread == 0 {
			// The same as a timer nothing has used.
			continue
		}
		for _, task := range timer.queue {
			held[task] = true
		}
		saved.Timers = append(saved.Timers, record)
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.timerTasks)) {
		task := runtime.timerTasks[object]
		if task == nil {
			return nil, fmt.Errorf("LGT checkpoint has a missing timer task")
		}
		if !held[object] && !client.javaObjectLive(object, javaTimerTaskClass) {
			continue
		}
		saved.Tasks = append(saved.Tasks, javaTimerTaskState{
			Object: object, Timer: task.timer, State: uint8(task.state), Next: task.next, Period: task.period,
			FixedRate: task.fixedRate, Last: task.last, Order: task.order,
		})
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.threads)) {
		if thread := runtime.threads[object]; thread != nil && thread.interrupted {
			saved.Interrupted = append(saved.Interrupted, object)
		}
	}
	if saved.Roots == 0 && saved.Order == 0 && len(saved.Timers) == 0 && len(saved.Tasks) == 0 &&
		len(saved.Interrupted) == 0 {
		return nil, nil
	}
	if err := client.validateJavaSchedulingState(saved); err != nil {
		return nil, err
	}
	return saved, nil
}

// validateJavaSchedulingState checks a record against the Java runtime it is
// restored onto, before any of it is applied: every queued task is a scheduled
// task of that timer, on one queue only, and every thread it names is one the
// Java record restored.
func (client *Client) validateJavaSchedulingState(saved *javaSchedulingState) error {
	invalid := func(what string) error { return fmt.Errorf("LGT checkpoint timer state has %s", what) }
	runtime := client.javaRun
	if runtime == nil {
		return fmt.Errorf("LGT checkpoint has timer state and no Java runtime")
	}
	if len(saved.Timers) > maxStateRecords || len(saved.Tasks) > maxStateRecords || len(saved.Interrupted) > maxStateRecords {
		return invalid("a table past its limit")
	}
	tasks := make(map[uint32]javaTimerTaskState, len(saved.Tasks))
	for index, task := range saved.Tasks {
		if index > 0 && saved.Tasks[index-1].Object >= task.Object || task.Object == 0 {
			return invalid("tasks out of order")
		}
		state := javaTimerTaskStatus(task.State)
		if state != javaTimerTaskScheduled && state != javaTimerTaskExecuted && state != javaTimerTaskCancelled ||
			task.Next < 0 || task.Period < 0 || task.Order > saved.Order ||
			state != javaTimerTaskCancelled && (task.Timer == 0 || task.Order == 0) ||
			state == javaTimerTaskExecuted && task.Period != 0 {
			return invalid("an impossible task")
		}
		tasks[task.Object] = task
	}
	queued, threads := map[uint32]bool{}, map[uint32]bool{}
	work := false
	for index, timer := range saved.Timers {
		if index > 0 && saved.Timers[index-1].Object >= timer.Object || timer.Object == 0 {
			return invalid("timers out of order")
		}
		if timer.Cancelled && len(timer.Queue) != 0 {
			return invalid("a cancelled timer with tasks to run")
		}
		for _, object := range timer.Queue {
			task, known := tasks[object]
			if !known || queued[object] || task.Timer != timer.Object || javaTimerTaskStatus(task.State) != javaTimerTaskScheduled {
				return invalid("a queued task that is not that timer's to run")
			}
			queued[object] = true
		}
		if len(queued) > maxJavaTimerTasks {
			return invalid("more queued tasks than a session can hold")
		}
		work = work || len(timer.Queue) != 0
		if timer.Thread == 0 {
			continue
		}
		thread := runtime.threads[timer.Thread]
		if thread == nil || threads[timer.Thread] || timer.Thread == runtime.mainThread {
			return invalid("a timer on a thread the Java record did not restore")
		}
		threads[timer.Thread] = true
		if thread.worker != nil && !thread.worker.done {
			// A run in progress: the task its thread runs is this timer's,
			// and may have been cancelled from inside its own run.
			if task, known := tasks[thread.runnable]; !known || task.Timer != timer.Object {
				return invalid("a timer running a task that is not its own")
			}
			work = true
		}
	}
	for index, object := range saved.Interrupted {
		if index > 0 && saved.Interrupted[index-1] >= object || runtime.threads[object] == nil {
			return invalid("an interrupt for a thread the Java record did not restore")
		}
	}
	if saved.Roots == 0 {
		if work {
			return invalid("timers with work and no block to hold it")
		}
		return nil
	}
	return client.validateJavaTimerRoots(saved.Roots)
}

// validateJavaTimerRoots checks that the block a record names is one the arena
// handed this platform, large enough, and no object's: the block is written
// to, so an address anywhere else would be a write over what lives there.
func (client *Client) validateJavaTimerRoots(block uint32) error {
	if size, allocated := client.arena.sizes[block]; !allocated || size < uint64(javaTimerRootWords)*4 {
		return fmt.Errorf("LGT checkpoint timer block at %#x is not one the arena handed out", block)
	}
	for object, record := range client.javaRun.objects {
		if object == block || record.block == block {
			return fmt.Errorf("LGT checkpoint timer block at %#x is an object's", block)
		}
	}
	return nil
}

// restoreJavaSchedulingState gives a restored runtime its timers, tasks and
// pending interrupts. A record without them restores none, and so does one
// from a Clet. It runs before the restored threads are started, so a timer's
// run waiting for its first slice starts as the timer's.
func (client *Client) restoreJavaSchedulingState(saved *javaSchedulingState) error {
	if saved == nil {
		return nil
	}
	if err := client.validateJavaSchedulingState(saved); err != nil {
		return err
	}
	runtime := client.javaRun
	if saved.Roots != 0 {
		// The block is as the session that took the record last wrote it, and
		// what it holds is what that session would compare its next write
		// against.
		words, err := client.readJavaTimerRoots(saved.Roots)
		if err != nil {
			return err
		}
		runtime.timerRoots, runtime.timerRootWords = saved.Roots, words
	}
	runtime.timerOrder = saved.Order
	for _, record := range saved.Tasks {
		runtime.timerTasks[record.Object] = &javaTimerTask{
			state: javaTimerTaskStatus(record.State), timer: record.Timer, next: record.Next, period: record.Period,
			fixedRate: record.FixedRate, last: record.Last, order: record.Order,
		}
	}
	for _, record := range saved.Timers {
		timer := &javaTimer{cancelled: record.Cancelled, queue: slices.Clone(record.Queue)}
		if record.Thread != 0 {
			timer.thread, timer.threadRecord = record.Thread, runtime.threads[record.Thread]
		}
		runtime.timers[record.Object] = timer
	}
	for _, object := range saved.Interrupted {
		runtime.threads[object].interrupted = true
	}
	return nil
}

// encodeLGTSchedulingRecord writes a session record with the scheduling state
// beside it, or in the shape encodeLGTScreenRecord writes when there is none.
func encodeLGTSchedulingRecord(
	saved sessionCheckpointState, media *javaMediaState, library *javaLibraryState, screen *javaScreenState,
	scheduling *javaSchedulingState,
) ([]byte, error) {
	if scheduling == nil {
		return encodeLGTScreenRecord(saved, media, library, screen)
	}
	return backend.EncodeCheckpointRecord(sessionSchedulingCheckpointState{
		Version: sessionSchedulingCheckpointVersion, Session: saved, Media: media, Library: library,
		Screen: screen, Scheduling: *scheduling,
	})
}

// decodeLGTSchedulingRecord reads every shape encodeLGTSchedulingRecord writes:
// this record, and every shape decodeLGTScreenRecord reads, which carry no
// scheduling state. The version is read first, as decodeLGTScreenRecord reads
// it, so a load of an earlier shape does not walk the record twice.
func decodeLGTSchedulingRecord(data []byte) (
	sessionCheckpointState, *javaMediaState, *javaLibraryState, *javaScreenState, *javaSchedulingState, error,
) {
	if version, ok := leadingRecordVersion(data); ok && version == sessionSchedulingCheckpointVersion {
		var wrapped sessionSchedulingCheckpointState
		wrappedErr := backend.DecodeCheckpointRecord(data, &wrapped)
		if wrappedErr == nil {
			return wrapped.Session, wrapped.Media, wrapped.Library, wrapped.Screen, &wrapped.Scheduling, nil
		}
		// Another record may share the number; the earlier shapes are asked
		// before the record is called damaged.
		saved, media, library, screen, err := decodeLGTScreenRecord(data)
		if err != nil {
			return sessionCheckpointState{}, nil, nil, nil, nil, wrappedErr
		}
		return saved, media, library, screen, nil, nil
	}
	saved, media, library, screen, err := decodeLGTScreenRecord(data)
	return saved, media, library, screen, nil, err
}
