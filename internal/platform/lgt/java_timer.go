package lgt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/util/Timer` and `java/util/TimerTask`, CLDC 1.1's whole classes.
//
// **A task runs on a guest thread, and the guest clock times it.** A Timer is
// work for later — a delay, a body, and maybe a period — and its body is the
// title's own `run`, which may loop, sleep, or wait like any thread's. So a run
// is a guest thread this platform starts (java_thread.go): a worker with a
// stack of its own, granted slices by the session, parked by its budget or its
// sleeps, and checkpointed the way every other guest thread is. That is also
// the KTF platform's arrangement (internal/platform/ktf/runtime_timer.go): a
// due task is adopted as a worker, one at a time per Timer.
//
// When a run starts is decided here, at the start of each round
// (startDueJavaTimerTasks), against the guest clock, and the earliest deadline
// is one the tick span lands on (nextJavaThreadDue) — the same way a sleeping
// thread's wake-up is. What a run is, is the class library's:
//
//   - **One thread per Timer, its tasks in turn.** A task due while another of
//     its timer's runs is still running waits for it; a fixed-rate task that
//     fell behind runs again at once until it has caught up. The thread is a
//     Thread object of its own, built at the timer's first run and kept for the
//     next ones, which is what `Thread.currentThread()` answers inside a task.
//   - **A task is taken before it runs**: a single run leaves the queue and its
//     task is spent, and a repeating task is due again already — from the
//     deadline it was due at for a fixed rate, from the time it was started for
//     a fixed delay. So a task that cancels itself, or its timer, from inside its
//     own run is not run again.
//   - **An exception a task does not catch ends the timer**: the class
//     library's timer thread dies of it, nothing more runs, and a schedule on
//     that timer is refused as one on a cancelled timer is.
//
// Where the KTF and SKT runtimes answer a call the same way, this one does too;
// where they differ, CLDC's class library settles it, as it settled the class
// library's core (docs/history/lgt.md):
//
//   - A null task or date is the NullPointerException SKT throws (KTF stops on
//     a null task); a negative delay or period is the IllegalArgumentException
//     SKT and CLDC throw (KTF treats it as none). A period of zero is a single
//     run, which KTF and SKT both answer and CLDC refuses.
//   - A schedule on a cancelled timer, or of a task already scheduled or
//     cancelled, is the IllegalStateException SKT and CLDC throw; KTF takes it.
//   - `TimerTask.cancel()` answers whether the task had a run still to come —
//     CLDC's answer, and KTF's — and a cancelled task stays cancelled.
//     `Timer.cancel()` leaves its tasks scheduled in name, as CLDC does, so
//     cancelling one of them afterwards still answers true, as on SKT.
//   - `scheduledExecutionTime()` is the deadline its latest run was due at, in
//     the guest's epoch milliseconds, as on SKT; 0 before the first run.
//   - A date already gone is due now, without the catch-up runs of a fixed
//     rate whose first time lies in the past: KTF and SKT both answer that.
//
// **What a timer holds is kept alive by a block of its own in guest memory**
// (javaTimerRoots). A scheduled task is often held by nothing else — a title
// writes `timer.schedule(new Tick(), 0, 50)` and keeps no reference — and the
// collector only sees what guest memory or a running thread names. So every
// task with a run to come, every timer with work, its thread and the task it
// is running are written into a platform block the collector reads as a root,
// the way the class library's own timer thread holds its queue in an array.

const (
	javaTimerClass     = "java/util/Timer"
	javaTimerTaskClass = "java/util/TimerTask"
	// maxJavaTimerTasks bounds the tasks with a run still to come, over every
	// timer: the number KTF's timer service holds. A schedule past it throws
	// the OutOfMemoryError a thread started past maxJavaThreads throws, since a
	// title that keeps that many tasks waiting is looping rather than timing.
	maxJavaTimerTasks = 256
	// javaTimerRootWords is the size of the block the timers' references are
	// kept in: every queued task, and three words — the timer, its thread and
	// the task it runs — for each timer with work, of which there are at most
	// one per queued task and one per running thread.
	javaTimerRootWords = 3*(maxJavaTimerTasks+maxJavaThreads) + maxJavaTimerTasks
	// javaTimerTaskSlotRun is TimerTask's run, its first method, where the
	// declaration-order rule puts it.
	javaTimerTaskSlotRun = 10
	// javaDateSlotGetTime is Date's getTime, the anchor its run starts at.
	javaDateSlotGetTime = 10
)

// javaTimerTaskStatus is the class library's: a task is scheduled once, run,
// and cancelled; one never scheduled has no record here.
type javaTimerTaskStatus uint8

const (
	javaTimerTaskScheduled javaTimerTaskStatus = iota + 1
	// Executed is a single run that has been taken: it has no run to come.
	javaTimerTaskExecuted
	javaTimerTaskCancelled
)

// javaTimer is one Timer.
type javaTimer struct {
	// cancelled is Timer.cancel, or its thread dying of a task's exception.
	cancelled bool
	// thread is the Thread object the timer's runs are made on, built at its
	// first run; threadRecord is that object's record, which is how a record
	// put in its place by a title's own Thread at the same address — once the
	// collector has taken this one — is told from it.
	thread       uint32
	threadRecord *javaThread
	// queue is the tasks with a run still to come, in the order they were
	// scheduled.
	queue []uint32
}

// javaTimerTask is one scheduled TimerTask.
type javaTimerTask struct {
	state javaTimerTaskStatus
	timer uint32
	// next is when its next run is due on the guest clock, and period how far
	// apart its runs are, zero for a single one.
	next      time.Duration
	period    time.Duration
	fixedRate bool
	// last is the deadline its latest run was due at, in the guest's epoch
	// milliseconds: what scheduledExecutionTime answers.
	last int64
	// order is when it was scheduled, which settles two runs due at once.
	order uint64
}

// javaTimerConstructor is `Timer()`: a timer with nothing scheduled.
func javaTimerConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	if existing := runtime.timers[arguments[0]]; existing != nil && client.javaTimerHasWork(existing) {
		// A constructor runs once on an object; one run again on a timer with
		// work does not throw that work away.
		return 0, nil
	}
	client.forgetStaleJavaTimers()
	runtime.timers[arguments[0]] = &javaTimer{}
	return 0, nil
}

// javaTimerTaskConstructor is `TimerTask()`: a task nothing has scheduled.
func javaTimerTaskConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	if client.javaTimerTaskHeld(arguments[0]) {
		return 0, nil
	}
	client.forgetStaleJavaTimerTasks()
	delete(runtime.timerTasks, arguments[0])
	return 0, nil
}

// javaTimerScheduleDelay serves the forms that take a delay: `schedule(task,
// delay)` and, with a period, `schedule(task, delay, period)` and
// `scheduleAtFixedRate(task, delay, period)`. Each long is two words, low
// first.
func javaTimerScheduleDelay(periodic, fixedRate bool) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32) (uint32, error) {
		delay := javaLongArgument(arguments, 2)
		period := int64(0)
		if periodic {
			period = javaLongArgument(arguments, 4)
		}
		return 0, client.scheduleJavaTimerTask(thread, arguments[0], arguments[1], delay, period, fixedRate)
	}
}

// javaTimerScheduleDate serves the forms that name the first run by a Date:
// the delay forms, with the delay how far that date lies past the guest clock
// now, and a date already gone due at once — what the KTF and SKT runtimes
// answer. The date's time is its getTime.
func javaTimerScheduleDate(periodic, fixedRate bool) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32) (uint32, error) {
		if arguments[1] == 0 {
			return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": Timer.schedule task is null")
		}
		if arguments[2] == 0 {
			return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": Timer.schedule first time is null")
		}
		at, err := client.javaTimerDateMillis(arguments[2])
		if err != nil {
			return 0, err
		}
		delay := int64(0)
		if now := client.clock.unixMillis(); at > now {
			// The difference of two longs can wrap; a date too far off to
			// say is as far as a delay goes.
			if delay = at - now; delay < 0 {
				delay = math.MaxInt64
			}
		}
		period := int64(0)
		if periodic {
			period = javaLongArgument(arguments, 3)
		}
		return 0, client.scheduleJavaTimerTask(thread, arguments[0], arguments[1], delay, period, fixedRate)
	}
}

// javaLongArgument reads the long a call passes at a word and the next one,
// low word first.
func javaLongArgument(arguments []uint32, index int) int64 {
	return int64(uint64(arguments[index]) | uint64(arguments[index+1])<<32)
}

// scheduleJavaTimerTask puts one task on a timer's queue, refusing what the
// class library refuses in the order SKT's refuses it.
func (client *Client) scheduleJavaTimerTask(
	thread *armcore.Thread, timerObject, taskObject uint32, delay, period int64, fixedRate bool,
) error {
	if taskObject == 0 {
		return client.throwJavaPlatform(thread, javaThrowNullClass, ": Timer.schedule task is null")
	}
	if delay < 0 || period < 0 {
		return client.throwJavaPlatform(thread, javaIllegalArgumentClass,
			": Timer.schedule takes a non-negative delay and period")
	}
	if delay > math.MaxInt64-client.clock.unixMillis() {
		// What the class library refuses as a time its clock cannot reach.
		return client.throwJavaPlatform(thread, javaIllegalArgumentClass, ": Illegal execution time.")
	}
	runtime := client.javaRuntimeState()
	timer := runtime.timers[timerObject]
	if timer == nil {
		// A timer this platform did not see constructed — one a title's own
		// subclass built without the constructor — starts out empty.
		timer = &javaTimer{}
	}
	if timer.cancelled {
		return client.throwJavaPlatform(thread, javaIllegalStateClass, ": Timer already cancelled")
	}
	if task := runtime.timerTasks[taskObject]; task != nil {
		if task.state == javaTimerTaskCancelled {
			return client.throwJavaPlatform(thread, javaIllegalStateClass, ": TimerTask already cancelled")
		}
		return client.throwJavaPlatform(thread, javaIllegalStateClass, ": TimerTask already scheduled")
	}
	if queued := client.queuedJavaTimerTasks(); queued >= maxJavaTimerTasks {
		return client.throwJavaPlatform(thread, javaOutOfMemoryClass,
			fmt.Sprintf(": %d timer tasks are already waiting", queued))
	}
	if err := client.ensureJavaTimerRoots(); err != nil {
		return err
	}
	runtime.timerOrder++
	runtime.timerTasks[taskObject] = &javaTimerTask{
		state: javaTimerTaskScheduled, timer: timerObject,
		next:   javaSaturatingAdd(client.clock.now(), javaTimerDuration(delay)),
		period: javaTimerDuration(period), fixedRate: fixedRate, order: runtime.timerOrder,
	}
	timer.queue = append(timer.queue, taskObject)
	runtime.timers[timerObject] = timer
	if client.logger != nil {
		client.logger.Debug("LGT java timer task scheduled", "timer", timerObject, "task", taskObject,
			"delay", delay, "period", period, "fixedRate", fixedRate)
	}
	return client.syncJavaTimerRoots()
}

// javaIllegalStateClass is what a schedule the class library refuses throws.
const javaIllegalStateClass = "java/lang/IllegalStateException"

// javaTimerDuration is a count of milliseconds on the guest clock: none for
// none, and as far as the clock goes for one past it.
func javaTimerDuration(milliseconds int64) time.Duration {
	if milliseconds <= 0 {
		return 0
	}
	if milliseconds > math.MaxInt64/int64(time.Millisecond) {
		return math.MaxInt64
	}
	return time.Duration(milliseconds) * time.Millisecond
}

// javaSaturatingAdd is a deadline that stops at the end of the clock rather
// than wrapping round before its start.
func javaSaturatingAdd(at, delta time.Duration) time.Duration {
	if delta > 0 && at > math.MaxInt64-delta {
		return math.MaxInt64
	}
	return at + delta
}

// javaTimerDateMillis is a Date's time, which the platform keeps for every
// Date it built. A title's own Date that answers getTime with code of its own
// is refused by name: its long answer comes back in two registers, and the
// platform's own call into the title reads one.
func (client *Client) javaTimerDateMillis(date uint32) (int64, error) {
	class, known := client.javaClassOfObject(date)
	if !known {
		return 0, fmt.Errorf("the date at %#x is not an object this platform issued", date)
	}
	if class.VTable != 0 && javaDateSlotGetTime < class.Slots {
		entry, err := client.readWord(class.VTable + 4 + javaDateSlotGetTime*4)
		if err != nil {
			return 0, err
		}
		if _, stub := client.javaStubSlot(entry); entry != 0 && !stub {
			return 0, fmt.Errorf("%s answers getTime with code of its own, which a timer cannot read", class.Name)
		}
	}
	return client.javaDateOf(date)
}

// javaTimerCancel is `Timer.cancel()`: nothing more of this timer's runs, and
// nothing more scheduled on it. A run already going goes on, which is what
// the specification says; a cancel from inside one makes it the last.
func javaTimerCancel(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	timer := runtime.timers[arguments[0]]
	if timer == nil {
		timer = &javaTimer{}
		runtime.timers[arguments[0]] = timer
	}
	timer.cancelled, timer.queue = true, nil
	return 0, client.syncJavaTimerRoots()
}

// javaTimerTaskCancel is `TimerTask.cancel()`: whether the task had a run still
// to come — a single run not yet taken, or any repeating one — and from now on
// none. A task cancelled before it was ever scheduled stays cancelled, and a
// schedule of it is refused, as the class library refuses one.
func javaTimerTaskCancel(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	task := runtime.timerTasks[arguments[0]]
	if task == nil {
		runtime.timerTasks[arguments[0]] = &javaTimerTask{state: javaTimerTaskCancelled}
		return javaFalse, nil
	}
	answer := javaFalse
	if task.state == javaTimerTaskScheduled {
		answer = javaTrue
	}
	task.state = javaTimerTaskCancelled
	if timer := runtime.timers[task.timer]; timer != nil {
		timer.queue = slices.DeleteFunc(timer.queue, func(queued uint32) bool { return queued == arguments[0] })
	}
	return answer, client.syncJavaTimerRoots()
}

// javaTimerTaskScheduledTime is `TimerTask.scheduledExecutionTime()`: the
// deadline the task's latest run was due at, in the guest's epoch
// milliseconds, and 0 before its first. A long comes back in two registers.
func javaTimerTaskScheduledTime(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	last := int64(0)
	if task := client.javaRuntimeState().timerTasks[arguments[0]]; task != nil {
		last = task.last
	}
	if err := thread.SetRegister(1, uint32(uint64(last)>>32)); err != nil {
		return 0, err
	}
	return uint32(uint64(last)), nil
}

// javaGuestEpochMillis is a moment on the guest clock as the epoch
// milliseconds currentTimeMillis answers for it.
func (client *Client) javaGuestEpochMillis(at time.Duration) int64 {
	return client.clock.origin.UnixMilli() + at.Milliseconds()
}

// startDueJavaTimerTasks starts the run of every timer whose earliest task has
// come due and that is not running one already. It is the first thing a round
// of the threads does, so a run started here has its first slice in the same
// round, and the platform's own thread does it too before it hands its turn
// round (runOtherJavaWorkers).
//
// **A run that finds every stack taken waits** for one to come free rather
// than throwing: no call of the title's is in flight to throw to, and the
// class library's timer thread would already have had its stack. Such a timer
// is not reported as due either (nextJavaTimerDue), or the clock would stand
// still for a run that cannot start while the threads holding the stacks
// sleep.
func (client *Client) startDueJavaTimerTasks() error {
	runtime := client.javaRun
	if runtime == nil || len(runtime.timers) == 0 {
		return nil
	}
	now := client.clock.now()
	for _, object := range slices.Sorted(maps.Keys(runtime.timers)) {
		timer := runtime.timers[object]
		if timer == nil || timer.cancelled || len(timer.queue) == 0 || client.javaTimerRunning(timer) {
			continue
		}
		index := client.earliestJavaTimerTask(timer)
		taskObject := timer.queue[index]
		task := runtime.timerTasks[taskObject]
		if task == nil {
			return fmt.Errorf("the timer at %#x queues a task with no schedule", object)
		}
		if task.next > now || !client.javaThreadRoom() {
			continue
		}
		thread, err := client.javaTimerThread(timer)
		if err != nil {
			return err
		}
		worker, err := client.startJavaWorker(thread)
		if errors.Is(err, errJavaThreadLimit) {
			if client.logger != nil {
				client.logger.Debug("LGT java timer task waits for a stack", "timer", object, "task", taskObject)
			}
			continue
		}
		if err != nil {
			return err
		}
		due := task.next
		switch {
		case task.period == 0:
			timer.queue = slices.Delete(timer.queue, index, index+1)
			task.state = javaTimerTaskExecuted
		case task.fixedRate:
			task.next = javaSaturatingAdd(due, task.period)
		default:
			task.next = javaSaturatingAdd(now, task.period)
		}
		task.last = client.javaGuestEpochMillis(due)
		thread.runnable, thread.worker = taskObject, worker
		if client.logger != nil {
			client.logger.Debug("LGT java timer task started", "timer", object, "task", taskObject,
				"due", due, "now", now)
		}
	}
	return client.syncJavaTimerRoots()
}

// earliestJavaTimerTask is the position in a timer's queue of the task due
// first, the one scheduled first among those due at once.
func (client *Client) earliestJavaTimerTask(timer *javaTimer) int {
	tasks := client.javaRun.timerTasks
	best := 0
	for index := 1; index < len(timer.queue); index++ {
		candidate, current := tasks[timer.queue[index]], tasks[timer.queue[best]]
		if candidate == nil || current == nil {
			continue
		}
		if candidate.next < current.next || candidate.next == current.next && candidate.order < current.order {
			best = index
		}
	}
	return best
}

// nextJavaTimerDue is the wait until the earliest run a timer could start now
// comes due, on the guest clock: zero for one already due.
func (client *Client) nextJavaTimerDue(now time.Duration) (time.Duration, bool) {
	runtime := client.javaRun
	if runtime == nil || len(runtime.timers) == 0 || !client.javaThreadRoom() {
		return 0, false
	}
	earliest, found := time.Duration(0), false
	for _, timer := range runtime.timers {
		if timer == nil || timer.cancelled || len(timer.queue) == 0 || client.javaTimerRunning(timer) {
			continue
		}
		task := runtime.timerTasks[timer.queue[client.earliestJavaTimerTask(timer)]]
		if task == nil {
			continue
		}
		wait := max(task.next-now, 0)
		if !found || wait < earliest {
			earliest, found = wait, true
		}
	}
	return earliest, found
}

// javaThreadRoom reports whether a guest thread could start now: fewer than
// maxJavaThreads hold a stack.
func (client *Client) javaThreadRoom() bool {
	live := 0
	for _, worker := range client.javaRun.workers {
		if worker != nil && !worker.done {
			live++
		}
	}
	return live < maxJavaThreads
}

// javaTimerThreadRecord is the record of the Thread a timer runs on, or nil
// before its first run and once that object is gone.
func (client *Client) javaTimerThreadRecord(timer *javaTimer) *javaThread {
	if timer == nil || timer.thread == 0 || timer.threadRecord == nil {
		return nil
	}
	if record := client.javaRun.threads[timer.thread]; record == timer.threadRecord {
		return record
	}
	return nil
}

// javaTimerThread answers the Thread a timer's runs are made on, building it
// at the first run and again if the collector has taken it since. It is a
// Thread object of the platform's own class, as the class library's timer
// thread is a Thread.
func (client *Client) javaTimerThread(timer *javaTimer) (*javaThread, error) {
	if record := client.javaTimerThreadRecord(timer); record != nil {
		return record, nil
	}
	class, err := client.preparePlatformJavaClass(javaThreadClass)
	if err != nil {
		return nil, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return nil, err
	}
	record := &javaThread{object: object, runnable: object}
	client.javaRun.threads[object] = record
	timer.thread, timer.threadRecord = object, record
	return record, nil
}

// javaTimerRunning reports whether a timer is in the middle of a run.
func (client *Client) javaTimerRunning(timer *javaTimer) bool {
	record := client.javaTimerThreadRecord(timer)
	return record != nil && record.worker != nil && !record.worker.done
}

// javaTimerHasWork reports whether a timer has a run going or one to come.
func (client *Client) javaTimerHasWork(timer *javaTimer) bool {
	return len(timer.queue) != 0 || client.javaTimerRunning(timer)
}

// javaTimerRunBy answers the timer whose runs a thread makes, or nil for a
// thread that is not a timer's.
func (client *Client) javaTimerRunBy(thread *javaThread) *javaTimer {
	if client.javaRun == nil || thread == nil {
		return nil
	}
	for _, timer := range client.javaRun.timers {
		if timer != nil && timer.threadRecord == thread && client.javaTimerThreadRecord(timer) == thread {
			return timer
		}
	}
	return nil
}

// queuedJavaTimerTasks counts the tasks with a run still to come.
func (client *Client) queuedJavaTimerTasks() int {
	count := 0
	for _, timer := range client.javaRuntimeState().timers {
		if timer != nil {
			count += len(timer.queue)
		}
	}
	return count
}

// javaTimerTaskHeld reports whether a task is on a timer's queue or in the
// middle of its run: the two places a task is held by the platform.
func (client *Client) javaTimerTaskHeld(object uint32) bool {
	for _, timer := range client.javaRuntimeState().timers {
		if timer == nil {
			continue
		}
		if slices.Contains(timer.queue, object) {
			return true
		}
		if record := client.javaTimerThreadRecord(timer); client.javaTimerRunning(timer) && record.runnable == object {
			return true
		}
	}
	return false
}

// runJavaTimerTask is one run, on the thread it was started on: the task's own
// `run`, entered the way a thread's is. An exception the task does not catch
// ends the run and the timer with it, and is reported the way one that ends a
// thread is.
func (client *Client) runJavaTimerTask(
	ctx context.Context, worker *javaWorker, timer *javaTimer, thread *javaThread,
) error {
	task := thread.runnable
	class, known := client.javaClassOfObject(task)
	if !known {
		return fmt.Errorf("the timer task at %#x is not an object this platform issued", task)
	}
	body, owner := client.javaTimerTaskBody(class)
	if body == 0 {
		return fmt.Errorf("%s declares no %s", class.Name, javaThreadRunMethod)
	}
	// The thread keeps naming the task it ran until its next run: a run
	// restored from a checkpoint ends through the remainder that resumed it
	// (checkpoint_worker.go), which leaves it so, and both have to hold the
	// same objects alive.
	_, err := client.callOn(ctx, worker.armThread, body, []uint32{task})
	if err == nil {
		return nil
	}
	var thrown *javaUncaughtThrow
	if errors.As(err, &thrown) {
		timer.cancelled, timer.queue = true, nil
	}
	return client.absorbUncaughtCallback(javaThreadRunMethod,
		fmt.Errorf("run %s.%s at %#x: %w", owner, javaThreadRunMethod, body, err))
}

// javaTimerTaskBody answers a task's own `run`: by name, or through the
// Runnable entry of its record, the way a thread's Runnable is found, and
// otherwise at TimerTask's run slot in the task's own dispatch table, where the
// compiler put its override.
func (client *Client) javaTimerTaskBody(class *javaRuntimeClass) (uint32, string) {
	if body, owner := client.javaRunnableBody(class); body != 0 {
		return body, owner
	}
	if class.VTable == 0 || javaTimerTaskSlotRun >= class.Slots {
		return 0, class.Name
	}
	entry, err := client.readWord(class.VTable + 4 + javaTimerTaskSlotRun*4)
	if err != nil || entry == 0 {
		return 0, class.Name
	}
	if _, stub := client.javaStubSlot(entry); stub {
		return 0, class.Name
	}
	return entry, class.Name
}

// forgetStaleJavaTimers drops what this platform kept for timers the collector
// has since taken: an idle timer whose object is no longer a Timer. The
// collector frees objects without telling this file, so a constructor — the
// one place a new timer can arrive at an old one's address — is where it is
// looked at. A timer with work is never taken: the timers' block holds it
// (javaTimerRootContent).
func (client *Client) forgetStaleJavaTimers() {
	runtime := client.javaRun
	for object, timer := range runtime.timers {
		if timer != nil && client.javaTimerHasWork(timer) {
			continue
		}
		if !client.javaObjectLive(object, javaTimerClass) {
			delete(runtime.timers, object)
		}
	}
}

// forgetStaleJavaTimerTasks is the same for tasks: one that is neither queued
// nor running, whose object is no longer a TimerTask.
func (client *Client) forgetStaleJavaTimerTasks() {
	runtime := client.javaRun
	for object := range runtime.timerTasks {
		if client.javaTimerTaskHeld(object) {
			continue
		}
		if !client.javaObjectLive(object, javaTimerTaskClass) {
			delete(runtime.timerTasks, object)
		}
	}
}

// javaObjectLive reports whether an address is still an object the collector
// tracks, of the named class or below it.
func (client *Client) javaObjectLive(object uint32, class string) bool {
	if _, tracked := client.javaRun.objects[object]; !tracked {
		return false
	}
	return client.javaObjectIs(object, class)
}

// ensureJavaTimerRoots allocates the block the timers' references are kept
// in, at the first schedule. It is the platform's own memory and is never
// given back: it is one allocation for the life of the title.
func (client *Client) ensureJavaTimerRoots() error {
	runtime := client.javaRun
	if runtime.timerRoots != 0 {
		return nil
	}
	block, err := client.allocateWords(make([]uint32, javaTimerRootWords))
	if err != nil {
		return fmt.Errorf("allocate the LGT java timer roots: %w", err)
	}
	runtime.timerRoots, runtime.timerRootWords = block, []uint32{}
	return nil
}

// javaTimerRootContent is what the block has to hold now: for each timer with
// work, in the order of the objects, the timer, its thread, the task it is
// running and the tasks it has queued. No word of it is zero.
func (client *Client) javaTimerRootContent() []uint32 {
	runtime := client.javaRun
	words := []uint32{}
	for _, object := range slices.Sorted(maps.Keys(runtime.timers)) {
		timer := runtime.timers[object]
		if timer == nil || !client.javaTimerHasWork(timer) {
			continue
		}
		words = append(words, object)
		if record := client.javaTimerThreadRecord(timer); record != nil {
			words = append(words, record.object)
			if client.javaTimerRunning(timer) && record.runnable != 0 && record.runnable != record.object {
				words = append(words, record.runnable)
			}
		}
		for _, task := range timer.queue {
			if task != 0 {
				words = append(words, task)
			}
		}
	}
	return words
}

// syncJavaTimerRoots writes the block when what it has to hold has changed.
// It writes the new words and clears the rest of what it last wrote, so the
// block is always its words and then zeros; that is also what lets a load
// read back what was last written (readJavaTimerRoots), so a restored session
// writes exactly what the session it came from writes.
func (client *Client) syncJavaTimerRoots() error {
	runtime := client.javaRun
	if runtime == nil || runtime.timerRoots == 0 {
		return nil
	}
	words := client.javaTimerRootContent()
	if runtime.timerRootWords != nil && slices.Equal(words, runtime.timerRootWords) {
		return nil
	}
	if len(words) > javaTimerRootWords {
		return fmt.Errorf("the LGT java timers hold %d references, past the %d their block keeps", len(words), javaTimerRootWords)
	}
	span := max(len(words), len(runtime.timerRootWords))
	if runtime.timerRootWords == nil {
		span = javaTimerRootWords
	}
	data := make([]byte, span*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	if err := client.core.Memory().Write(runtime.timerRoots, data); err != nil {
		return fmt.Errorf("write the LGT java timer roots: %w", err)
	}
	runtime.timerRootWords = words
	return nil
}

// readJavaTimerRoots reads back what the block holds: the words before its
// first zero, which is what was last written into it.
func (client *Client) readJavaTimerRoots(block uint32) ([]uint32, error) {
	data := make([]byte, javaTimerRootWords*4)
	if err := client.core.Memory().Read(block, data); err != nil {
		return nil, fmt.Errorf("read the LGT java timer roots: %w", err)
	}
	words := []uint32{}
	for offset := 0; offset < len(data); offset += 4 {
		word := binary.LittleEndian.Uint32(data[offset:])
		if word == 0 {
			break
		}
		words = append(words, word)
	}
	return words, nil
}
