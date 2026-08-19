package lgt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// `java/lang/Thread`, which an AOT Java title's game loop runs on.
//
// A Clet is called by the platform and returns; a Java title starts a thread in
// `startApp` and never comes back out of it. **So a thread here cannot be a
// call**: its guest stack has to survive across frames, and the platform has to
// be able to stop it in the middle and resume it later. That is a goroutine
// with a private ARM stack, exactly the arrangement the KTF platform reached
// for the same reason: the Host grants one step slice and blocks until the
// thread parks — its budget spent, or a `sleep` it asked for — so guest state
// is never touched by two goroutines at once, and a loop that never returns
// still advances a slice per tick.
//
// The thread object is the module's; what stands behind it is here, keyed by
// that object, like every other platform class's state.

const (
	javaThreadClass = "java/lang/Thread"
	// javaThreadStackBase is where a guest thread's stack is mapped, above the
	// Clet stack and below the sentinel return address.
	javaThreadStackBase uint32 = 0x41000000
	javaThreadStackSize uint64 = 1 << 20
	// maxJavaThreads bounds how many a title may start. A title here starts
	// one; a title that starts hundreds is looping, not threading.
	maxJavaThreads = 16
	// javaThreadSliceSteps is how far one thread runs in a tick before it is
	// parked. A loop that sleeps parks sooner; one that never sleeps still
	// yields, so a runaway thread cannot hold the frame.
	javaThreadSliceSteps = 4_000_000
	// javaThreadRunMethod is what a thread runs, on the Runnable it was built
	// with or on itself.
	javaThreadRunMethod = "run"
)

var errJavaThreadsStopped = errors.New("LGT guest threads are stopped")

// javaThread is one guest thread: the object the module allocated, what it
// runs, and the goroutine it runs on once started.
type javaThread struct {
	object   uint32
	runnable uint32
	worker   *javaWorker
}

type javaWorker struct {
	armThread *armcore.Thread
	stackBase uint32
	grant     chan context.Context
	events    chan javaWorkerEvent
	// wakeAt is when this thread is eligible for another slice, **on the
	// guest's own clock**: zero for one that spent its budget, and the
	// deadline of the wait for one that parked inside `sleep`. The guest clock
	// is the only one a title's pace is set against here — the wall clock
	// belongs to the Host, which decides how fast the guest clock runs.
	wakeAt time.Duration
	done   bool
	// yields counts the yields this slice has made without parking. See
	// javaYieldBurst.
	yields int
	// monitors is how many locks this thread holds, and renewals how many
	// windows it has been granted while holding one. A thread inside a
	// synchronized body is not parked, because nothing else may run there.
	monitors int
	renewals int
	// waitSite is the address the thread last slept from, waits how many
	// sleeps in a row came from it, and waitReported whether that wait has
	// been reported. See noteJavaWait.
	waitSite     uint32
	waits        int
	waitReported bool
}

type javaWorkerEvent struct {
	done bool
	err  error
}

// javaThreadConstructor is `Thread(Runnable)`. Nothing starts here: the
// specification's constructor only records what the thread will run.
func javaThreadConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	if len(runtime.threads) >= maxJavaThreads {
		return 0, fmt.Errorf("a title started more than %d threads", maxJavaThreads)
	}
	runnable := arguments[0]
	if len(arguments) > 1 && arguments[1] != 0 {
		runnable = arguments[1]
	}
	runtime.threads[arguments[0]] = &javaThread{object: arguments[0], runnable: runnable}
	if client.logger != nil {
		client.logger.Debug("LGT java thread built", "thread", arguments[0], "runnable", runnable)
	}
	return 0, nil
}

// javaThreadStart is `Thread.start()`: the thread becomes live and its `run`
// is entered on the next slice the session grants.
func javaThreadStart(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	thread, known := runtime.threads[arguments[0]]
	if !known {
		// A Thread subclass the module built without the constructor this
		// platform sees runs itself, which is what `Thread.run` does.
		thread = &javaThread{object: arguments[0], runnable: arguments[0]}
		runtime.threads[arguments[0]] = thread
	}
	if thread.worker != nil {
		return 0, fmt.Errorf("the thread at %#x is already started", arguments[0])
	}
	worker, err := client.startJavaWorker(thread)
	if err != nil {
		return 0, err
	}
	thread.worker = worker
	return 0, nil
}

// javaThreadSleep is `Thread.sleep(long)`. **The wait is the guest's own pace**
// — a game loop is written as work, repaint, sleep — so it parks the thread
// until the deadline rather than returning at once, which is what makes a title
// run at the speed it was written for instead of as fast as the host will go.
//
// A sleep declared outside a guest thread has nothing to park: the platform's
// own thread is the one inside the call, and blocking it would stop the Host
// rather than the guest. That one advances the guest clock instead, which is
// what the wait was for.
func javaThreadSleep(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	// A long is two words, low first, and a sleep is a small number of
	// milliseconds — the high word is carried so a title that passes a real
	// 64-bit value is not slept for a fraction of it.
	milliseconds := uint64(arguments[0]) | uint64(arguments[1])<<32
	if milliseconds > uint64(time.Hour/time.Millisecond) {
		return 0, fmt.Errorf("a sleep of %d milliseconds", milliseconds)
	}
	wait := time.Duration(milliseconds) * time.Millisecond
	worker := client.activeJavaWorker
	if worker == nil {
		_ = client.clock.advance(wait)
		return 0, nil
	}
	worker.wakeAt = client.clock.now() + wait
	worker.yields = 0
	client.noteJavaWait(worker, thread)
	return 0, worker.park()
}

// javaStallSleeps is how many sleeps at one call site make a wait worth
// reporting. A game loop sleeps at the same site every frame and is not
// stalled, so the count is high enough to be seconds of guest time rather than
// a busy loop's worth.
const javaStallSleeps = 500

// noteJavaWait reports a thread that has been sleeping in the same place for a
// long time, once, with the guest call stack of the wait.
//
// **A title that waits for something that never happens looks exactly like a
// title that has stopped**, and the trace of it is three lines repeating: the
// sleep says nothing about which of the title's own methods is waiting or what
// it is waiting for. The stack does, and the wait is the only moment when
// taking one costs nothing — the thread is parked either way.
func (client *Client) noteJavaWait(worker *javaWorker, thread *armcore.Thread) {
	if client.logger == nil || thread == nil {
		return
	}
	site, err := thread.Register(armcore.RegisterLR)
	if err != nil {
		return
	}
	if site != worker.waitSite {
		worker.waitSite, worker.waits, worker.waitReported = site, 0, false
		return
	}
	worker.waits++
	if worker.waits != javaStallSleeps || worker.waitReported {
		return
	}
	worker.waitReported = true
	client.logger.Debug("LGT java thread is waiting in one place",
		"sleeps", worker.waits, "stack", client.javaBacktraceLine(thread))
}

// javaThreadYield is `Thread.yield()`: a hint that something else may run.
//
// **It only parks when there is something else to run.** Parking ends the
// thread's slice, and a slice is granted once a tick — so a yield with nobody
// to hand to costs a whole tick of guest time for a call that on a handset
// returns immediately. One local title's frame loop is `work; repaint;
// yield; sleep(100)`, one yield and one sleep per frame, and it was being
// charged 150ms of guest time for the 100 it asked for: **a third of that
// title's frame rate was going into a hint with no other thread to give the
// turn to.** It reads as "the game is just slow", and it moves with the
// session tick rather than with anything the game does.
//
// A title that really has two threads still hands over, which is what the
// call is for. The step budget, not this, is what stops a runaway loop from
// holding the frame.
func javaThreadYield(
	client *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	worker := client.activeJavaWorker
	if worker == nil {
		return 0, nil
	}
	if !client.otherJavaWorkerReady(worker) && worker.yields < javaYieldBurst {
		worker.yields++
		return 0, nil
	}
	worker.yields = 0
	worker.wakeAt = 0
	return 0, worker.park()
}

// javaYieldBurst is how many times a thread may yield inside one slice before
// it is parked anyway. **A loop that yields is not always a frame loop.** One
// local title spins on `yield` while it waits for something, and letting that
// through unparked spends the whole slice budget every tick — measured, four
// million instructions a tick against the twenty thousand the title was doing
// before, and the run took minutes instead of seconds. The bound keeps the
// frame loop's single yield free while a spin still parks almost at once, and
// the counter is per slice: it is cleared wherever the thread parks.
const javaYieldBurst = 8

// otherJavaWorkerReady reports whether some other guest thread could run now,
// which is what decides whether a yield has anywhere to go.
func (client *Client) otherJavaWorkerReady(yielding *javaWorker) bool {
	if client.javaRun == nil {
		return false
	}
	now := client.clock.now()
	for _, worker := range client.javaRun.workers {
		if worker == yielding || worker.done {
			continue
		}
		if worker.wakeAt == 0 || worker.wakeAt <= now {
			return true
		}
	}
	return false
}

// javaCurrentThread is `Thread.currentThread()`. It answers the object the
// running thread was built from, and for the platform's own thread — the one a
// frame is painted on — a Thread of its own, because a title that asks there
// still has to be given one it can call methods on.
func javaCurrentThread(
	client *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	if worker := client.activeJavaWorker; worker != nil {
		for _, thread := range runtime.threads {
			if thread.worker == worker {
				return thread.object, nil
			}
		}
	}
	if runtime.mainThread == 0 {
		class, err := client.preparePlatformJavaClass(javaThreadClass)
		if err != nil {
			return 0, err
		}
		object, err := client.allocateJavaObject(class)
		if err != nil {
			return 0, err
		}
		runtime.mainThread = object
		runtime.threads[object] = &javaThread{object: object, runnable: object}
	}
	return runtime.mainThread, nil
}

// startJavaWorker maps a private stack, derives the logical ARM thread, and
// starts the goroutine that will enter `run` on its first slice.
func (client *Client) startJavaWorker(thread *javaThread) (*javaWorker, error) {
	runtime := client.javaRuntimeState()
	base := javaThreadStackBase + uint32(runtime.threadStacks)*uint32(javaThreadStackSize)
	if err := client.core.Memory().Map(base, javaThreadStackSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map an LGT guest thread stack: %w", err)
	}
	runtime.threadStacks++
	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = base + uint32(javaThreadStackSize)
	worker := &javaWorker{
		armThread: armcore.NewThread(initial),
		stackBase: base,
		grant:     make(chan context.Context),
		events:    make(chan javaWorkerEvent, 1),
	}
	worker.armThread.SetStepBudget(javaThreadSliceSteps)
	worker.armThread.SetLimitHook(func(context.Context) error {
		// A thread holding a lock is granted another window rather than
		// parked, which is what keeps a synchronized body indivisible under a
		// scheduler that only switches at a park. The renewal count bounds it
		// so a loop inside one cannot hold the frame for ever.
		if worker.monitors > 0 && worker.renewals < maxJavaSliceRenewals {
			worker.renewals++
			return nil
		}
		worker.renewals = 0
		return worker.park()
	})
	runtime.workers = append(runtime.workers, worker)
	go client.runJavaWorker(thread, worker)
	return worker, nil
}

// runJavaWorker is the worker goroutine: wait for the first slice, then enter
// the Runnable's own `run` and stay inside it for as long as the title does.
func (client *Client) runJavaWorker(thread *javaThread, worker *javaWorker) {
	ctx, ok := <-worker.grant
	if !ok {
		return
	}
	// The monitors this thread holds have to be released whatever ended it, so
	// the run is wrapped rather than deferred over the function: a panic that
	// skipped the release would leave every other thread waiting on a lock
	// nobody owns. See backend.GuestPanic for why a panic becomes this
	// worker's error rather than the process's exit.
	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = backend.GuestPanic(client.logger, "LGT guest thread", recovered)
			}
		}()
		err = client.enterJavaRunnable(ctx, thread, worker)
	}()
	client.releaseJavaMonitors(worker)
	worker.events <- javaWorkerEvent{done: true, err: err}
}

func (client *Client) enterJavaRunnable(
	ctx context.Context, thread *javaThread, worker *javaWorker,
) error {
	class, known := client.javaClassOfObject(thread.runnable)
	if !known {
		return fmt.Errorf("the runnable at %#x is not an object this platform issued", thread.runnable)
	}
	body, owner := uint32(0), class.Name
	if method, named, ok := client.findJavaMethod(class.Record, javaThreadRunMethod); ok {
		body, owner = method.Body, named
	} else if slot, ok := client.javaInterfaceMethod(class, javaRunnableInterface, 0); ok {
		// A class the compiler laid out itself declares no members at all, and
		// its record says where `java/lang/Runnable` sits in its own dispatch
		// table instead. See readJavaInterfaces.
		body = slot
	}
	if body == 0 {
		return fmt.Errorf("%s declares no %s", class.Name, javaThreadRunMethod)
	}
	if _, err := client.callOn(ctx, worker.armThread, body, []uint32{thread.runnable}); err != nil {
		return fmt.Errorf("run %s.%s at %#x: %w", owner, javaThreadRunMethod, body, err)
	}
	return nil
}

// javaRunnableInterface is the one interface whose contract names a method by
// position: it declares `run()V` and nothing else, so the slot its entry gives
// is that method.
const javaRunnableInterface = "java/lang/Runnable"

// javaInterfaceMethod answers the address in a class's own dispatch table for
// one method of an interface the class implements, by the slot the record's
// interface table gives. The index is the method's position within the
// interface, which only a one-method interface makes obvious — see
// readJavaInterfaces for why nothing else is resolved this way.
func (client *Client) javaInterfaceMethod(
	class *javaRuntimeClass, name string, index uint32,
) (uint32, bool) {
	for _, implemented := range class.Record.Interfaces {
		if implemented.Name != name {
			continue
		}
		slot := implemented.Slot + index
		if class.VTable == 0 || slot >= class.Slots {
			return 0, false
		}
		body, err := client.readWord(class.VTable + 4 + slot*4)
		if err != nil || body == 0 {
			return 0, false
		}
		if client.logger != nil {
			client.logger.Debug("LGT java interface method resolved",
				"class", class.Name, "interface", name, "slot", slot, "body", body)
		}
		return body, true
	}
	return 0, false
}

// park reports the thread as still running and blocks until the session grants
// the next slice. A closed grant channel aborts the run.
func (worker *javaWorker) park() error {
	worker.events <- javaWorkerEvent{}
	if _, ok := <-worker.grant; !ok {
		return errJavaThreadsStopped
	}
	return nil
}

// ServiceJavaThreads grants one slice to every live guest thread whose wait has
// passed, and reports how many ran. It is the other half of a Java title's
// tick: `PaintJava` draws what the threads have done.
func (client *Client) ServiceJavaThreads(ctx context.Context) (int, error) {
	if client.javaRun == nil || len(client.javaRun.workers) == 0 {
		return 0, nil
	}
	runtime := client.javaRun
	now := client.clock.now()
	serviced := 0
	live := runtime.workers[:0]
	for _, worker := range runtime.workers {
		if worker.done {
			continue
		}
		if now < worker.wakeAt {
			live = append(live, worker)
			continue
		}
		event, err := client.grantJavaSlice(ctx, worker)
		if err != nil {
			runtime.workers = append(live, worker)
			return serviced, err
		}
		serviced++
		if !event.done {
			live = append(live, worker)
			continue
		}
		worker.done = true
		if event.err != nil && !errors.Is(event.err, ErrGuestExited) {
			runtime.workers = live
			return serviced, event.err
		}
	}
	runtime.workers = live
	return serviced, nil
}

// nextJavaThreadDue is the wait until the earliest sleeping guest thread is
// eligible again, on the guest's own clock. It is what a Java title has
// instead of a timer, and the tick span needs it for the same reason it needs
// `nextTimerDue`: **a title's frame loop here is a thread that sleeps at the
// end of every frame**, so the interval it sleeps for is the rate it wants,
// and a tick that always stands for the session tick rounds that interval up
// to a multiple of itself.
//
// The rounding is not the whole of the interval, which is what made it hard to
// see. One local title sleeps 100ms against a 50ms tick, which divides
// exactly — but it asks for the sleep *after* doing a frame's work, and the
// work clock has already carried `now` a few milliseconds past the tick
// boundary. The deadline lands at 103ms, the tick after next is only at 100,
// and the frame waits a whole further tick: **150ms of guest time for a frame
// it wanted at 103.** The heavier the frame, the further past the boundary it
// starts, which is why the symptom is "it lags when I move" — moving is the
// frame with the most work in it, and nothing in the host's own cost changes
// at all.
//
// A thread that is already eligible is not a deadline and is skipped: zero
// means "spent its budget", and letting that shorten the tick to nothing would
// stop the guest clock for a thread that never sleeps.
func (client *Client) nextJavaThreadDue() (time.Duration, bool) {
	if client.javaRun == nil {
		return 0, false
	}
	now := client.clock.now()
	earliest, found := time.Duration(0), false
	for _, worker := range client.javaRun.workers {
		// Zero is "spent its budget", not a deadline. It is the case that has
		// to be skipped rather than the one that has already come due: a
		// thread whose deadline has passed **is** the reason to make this tick
		// stand for no time — otherwise the tick advances the clock a whole
		// session tick before handing over the slice, and every wait comes out
		// one tick long. That is a 100ms sleep taking 150ms, every frame.
		if worker.done || worker.wakeAt == 0 {
			continue
		}
		wait := worker.wakeAt - now
		if wait < 0 {
			wait = 0
		}
		if !found || wait < earliest {
			earliest, found = wait, true
		}
	}
	return earliest, found
}

// grantJavaSlice hands one thread its slice and waits out the whole of it. The
// session goroutine is blocked here for as long as the guest runs, which is
// what keeps one guest at a time inside the runtime.
func (client *Client) grantJavaSlice(
	ctx context.Context, worker *javaWorker,
) (javaWorkerEvent, error) {
	if client.javaThreadsStopped {
		return javaWorkerEvent{}, errJavaThreadsStopped
	}
	previous := client.activeJavaWorker
	client.activeJavaWorker = worker
	defer func() { client.activeJavaWorker = previous }()
	// The deadline is spent the moment the slice is granted. Leaving it set
	// would make `nextJavaThreadDue` keep reporting a wait that has already
	// been served, and a tick would then stand for no time at all for as long
	// as the thread went without sleeping again — the guest clock would stop.
	// A thread sets it again from inside the slice if it sleeps.
	worker.wakeAt = 0
	worker.grant <- ctx
	return <-worker.events, nil
}

// StopJavaThreads aborts every guest thread; parked ones wake, fail their run
// and exit. A Host calls it when it tears the session down.
func (client *Client) StopJavaThreads() {
	if client == nil || client.javaRun == nil || client.javaThreadsStopped {
		return
	}
	client.javaThreadsStopped = true
	for _, worker := range client.javaRun.workers {
		if !worker.done {
			close(worker.grant)
		}
	}
	client.javaRun.workers = nil
}

// Monitors: `synchronized`, which the compiler turns into a pair of calls
// around the body of a method and a third on the path that rethrows.
//
// A synchronized method compiles to `setjmp; if (exception) { exit; rethrow }
// enter; body; leave the try region; exit`, so **the two slots are told apart
// by where they sit**: one runs where the protected body begins and the other
// where it ends and again where an exception leaves it. That is monitorenter
// and monitorexit, and it is the only reading under which the exception path
// makes sense — a handler that runs the *entry* on its way out would leave the
// lock held forever.
//
// Threads here run one at a time, so a lock is only ever contended across a
// park: a thread that spends its slice inside a synchronized body, or sleeps
// there. The first case is taken away — a thread holding a monitor is granted
// another window instead of being parked — and what is left is handled by
// letting the owner run until it lets go.
const (
	// maxJavaMonitorWaits bounds how long a contended enter waits before it is
	// reported. A wait this long is a deadlock, not a delay.
	maxJavaMonitorWaits = 4096
	// maxJavaSliceRenewals bounds how many windows a thread holding a monitor
	// may take before it is parked anyway, so a loop inside a synchronized
	// body cannot hold the frame for ever.
	maxJavaSliceRenewals = 64
)

// javaMonitor is one object's lock: who holds it and how many times, because
// the language lets a thread re-enter one it already owns.
type javaMonitor struct {
	owner *javaWorker
	count int
	// held marks that the platform's own thread is the owner, which is not a
	// worker and so cannot be told apart by the pointer alone.
	platform bool
}

// javaMonitorEnter takes an object's lock for whoever is running.
func (client *Client) javaMonitorEnter(ctx context.Context, object uint32) error {
	runtime := client.javaRuntimeState()
	for wait := 0; wait < maxJavaMonitorWaits; wait++ {
		monitor := runtime.monitors[object]
		worker := client.activeJavaWorker
		switch {
		case monitor == nil || monitor.count == 0:
			runtime.monitors[object] = &javaMonitor{owner: worker, count: 1, platform: worker == nil}
			client.holdJavaMonitor(worker, 1)
			return nil
		case monitor.owner == worker && monitor.platform == (worker == nil):
			monitor.count++
			client.holdJavaMonitor(worker, 1)
			return nil
		case worker != nil:
			// Another thread has it. Park and look again: the owner runs on
			// some later slice and lets go there.
			worker.wakeAt = 0
			if err := worker.park(); err != nil {
				return err
			}
		case monitor.owner != nil:
			// The platform's own thread wants a lock a guest thread holds,
			// which means that thread parked inside the body. Nothing else can
			// run it, so it is run from here until it lets go — the wait a
			// handset's own scheduler would have made.
			if _, err := client.grantJavaSlice(ctx, monitor.owner); err != nil {
				return err
			}
		default:
			return fmt.Errorf("the monitor of %#x is held by nothing", object)
		}
	}
	return fmt.Errorf("the monitor of %#x could not be taken", object)
}

// javaMonitorExit gives one level of an object's lock back.
func (client *Client) javaMonitorExit(object uint32) error {
	runtime := client.javaRuntimeState()
	monitor := runtime.monitors[object]
	if monitor == nil || monitor.count == 0 {
		return fmt.Errorf("the monitor of %#x was left without being taken", object)
	}
	monitor.count--
	client.holdJavaMonitor(monitor.owner, -1)
	if monitor.count == 0 {
		delete(runtime.monitors, object)
	}
	return nil
}

// holdJavaMonitor counts the locks one runner holds, which is what says whether
// it may be parked at the end of its window.
func (client *Client) holdJavaMonitor(worker *javaWorker, delta int) {
	if worker == nil {
		return
	}
	worker.monitors += delta
}

// releaseJavaMonitors gives back every lock a finished thread still held. A
// thread that failed inside a synchronized body would otherwise leave it shut.
func (client *Client) releaseJavaMonitors(worker *javaWorker) {
	if client.javaRun == nil {
		return
	}
	for object, monitor := range client.javaRun.monitors {
		if monitor.owner == worker && !monitor.platform {
			delete(client.javaRun.monitors, object)
		}
	}
	worker.monitors = 0
}
