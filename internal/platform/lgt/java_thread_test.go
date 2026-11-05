package lgt

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
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

// `Thread.isAlive` is baked slot 13, and it answers the worker's real state: a
// thread nothing started is not alive, a started one is, and one whose worker
// has finished is not. A title's paint asks this every frame while a loader
// thread builds what the paint is about to draw, so the wrong answer either
// leaves it waiting on a load that finished or sends it into objects that do
// not exist yet.
func TestThreadIsAliveAnswersTheWorkersState(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	object := uint32(0x2000)

	alive, err := javaThreadIsAlive(client, nil, nil, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	if alive != javaFalse {
		t.Error("a thread this platform never saw answered alive")
	}

	runtime.threads[object] = &javaThread{object: object, runnable: object}
	if alive, err = javaThreadIsAlive(client, nil, nil, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if alive != javaFalse {
		t.Error("a thread that was built and not started answered alive")
	}

	worker := &javaWorker{}
	runtime.threads[object].worker = worker
	if alive, err = javaThreadIsAlive(client, nil, nil, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if alive != javaTrue {
		t.Error("a started thread answered not alive")
	}

	worker.done = true
	if alive, err = javaThreadIsAlive(client, nil, nil, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if alive != javaFalse {
		t.Error("a finished thread answered alive")
	}
}

// dropFinishedJavaWorker is what a round does with a worker whose run ended:
// it is recorded as done and dropped from the list. Its goroutine has not been
// granted a slice here, so closing grant is what ends it.
func dropFinishedJavaWorker(runtime *javaRuntime, worker *javaWorker) {
	worker.done = true
	close(worker.grant)
	live := runtime.workers[:0]
	for _, other := range runtime.workers {
		if other != worker {
			live = append(live, other)
		}
	}
	runtime.workers = live
}

// A title that starts a short-lived thread per scene starts thousands over a
// session. A finished thread's stack is taken by the next one, so the stacks
// mapped are as many as ran at once rather than as many as ever started —
// which is also what keeps a checkpoint, which allows maxJavaThreads stacks,
// loadable.
func TestAFinishedThreadsStackIsTakenByTheNextOne(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	defer client.StopJavaThreads()

	first, err := client.startJavaWorker(&javaThread{object: 0x100})
	if err != nil {
		t.Fatal(err)
	}
	// A stack the guest used, so a reuse that kept its words would show.
	if err := client.writeWord(first.stackBase+16, 0xdeadbeef); err != nil {
		t.Fatal(err)
	}
	dropFinishedJavaWorker(runtime, first)
	for index := range 5000 {
		next, err := client.startJavaWorker(&javaThread{object: 0x200})
		if err != nil {
			t.Fatalf("start %d: %v", index, err)
		}
		if next.stackBase != first.stackBase {
			t.Fatalf("start %d took the stack at %#x, not the finished one at %#x", index, next.stackBase, first.stackBase)
		}
		dropFinishedJavaWorker(runtime, next)
	}
	if runtime.threadStacks != 1 {
		t.Fatalf("%d stacks are mapped for threads that never ran at once", runtime.threadStacks)
	}
	if word, err := client.readWord(first.stackBase + 16); err != nil || word != 0 {
		t.Fatalf("a reused stack starts with %#x (%v), not the zeros a fresh one has", word, err)
	}

	// Threads that do run at once each have a stack of their own.
	live, err := client.startJavaWorker(&javaThread{object: 0x300})
	if err != nil {
		t.Fatal(err)
	}
	other, err := client.startJavaWorker(&javaThread{object: 0x400})
	if err != nil {
		t.Fatal(err)
	}
	if live.stackBase == other.stackBase || runtime.threadStacks != 2 {
		t.Fatalf("two live threads share a stack (%#x, %#x) with %d mapped", live.stackBase, other.stackBase, runtime.threadStacks)
	}
}

// Building a thread takes no stack, so the constructor counts nothing: a title
// may hold more Thread objects than can run at once, and the bound is met only
// by the start that would run one too many.
func TestBuildingAThreadIsNotStartingOne(t *testing.T) {
	client := fixtureClient(t)
	for index := range 2 * maxJavaThreads {
		object := uint32(0x1000 + index*0x10)
		if _, err := javaThreadConstructor(client, nil, nil, []uint32{object, object}); err != nil {
			t.Fatalf("thread %d was refused when it was built: %v", index+1, err)
		}
	}
}

// The bound on threads running at once is met at the start, whichever way the
// thread was built — through the constructor or as a subclass the start sees
// first — so the stacks never outnumber what a checkpoint restores. The start
// past it throws an OutOfMemoryError the title can catch rather than ending
// the session, and leaves the thread unstarted, so it starts once a running
// one has finished.
func TestAStartPastTheThreadBoundThrowsOutOfMemoryError(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	defer client.StopJavaThreads()
	caller := armcore.NewThread(armcore.NewContext())
	const resume = 0x2000
	if err := caller.SetRegister(armcore.RegisterLR, resume); err != nil {
		t.Fatal(err)
	}
	for index := range maxJavaThreads {
		object := uint32(0x1000 + index*0x10)
		if index%2 == 0 {
			if _, err := javaThreadConstructor(client, nil, nil, []uint32{object, object}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := javaThreadStart(client, nil, caller, []uint32{object}); err != nil {
			t.Fatalf("start %d: %v", index+1, err)
		}
	}
	const extra = 0x8000

	var throw *javaUncaughtThrow
	if _, err := javaThreadStart(client, nil, caller, []uint32{extra}); !errors.As(err, &throw) || throw.Class != javaOutOfMemoryClass {
		t.Fatalf("an uncaught start past %d running threads: %v, want %s", maxJavaThreads, err, javaOutOfMemoryClass)
	}

	buffer, err := client.enterJavaTry()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.armJavaTry(caller, buffer); err != nil {
		t.Fatal(err)
	}
	if _, err := javaThreadStart(client, nil, caller, []uint32{extra}); !errors.Is(err, errJavaThrowHandled) {
		t.Fatalf("a caught start past %d running threads: %v, want it handled", maxJavaThreads, err)
	}
	if resumed := caller.Context(); resumed.PC() != resume {
		t.Errorf("the guest resumes at %#x, want its handler at %#x", resumed.PC(), resume)
	}
	object, _ := caller.Register(0)
	if class, ok := client.javaClassOfObject(object); !ok || class.Name != javaOutOfMemoryClass {
		t.Errorf("the handler was given %v, want a %s", class, javaOutOfMemoryClass)
	}
	if runtime.threadStacks != maxJavaThreads {
		t.Fatalf("%d stacks are mapped for %d threads", runtime.threadStacks, maxJavaThreads)
	}
	if thread := runtime.threads[extra]; thread != nil && thread.worker != nil {
		t.Fatal("the refused thread was started")
	}

	dropFinishedJavaWorker(runtime, runtime.workers[0])
	if _, err := javaThreadStart(client, nil, caller, []uint32{extra}); err != nil {
		t.Fatalf("a start once a thread has finished: %v", err)
	}
	if thread := runtime.threads[extra]; thread == nil || thread.worker == nil || runtime.threadStacks != maxJavaThreads {
		t.Fatalf("the thread did not start on the finished one's stack (%d mapped)", runtime.threadStacks)
	}
}

// A startApp that loads its resources one thread at a time starts a loader,
// waits on a lock until the loader is done, and goes on to the next. Each
// loader ends inside a call of the platform's own thread — the wait that ran
// it, or the lock the platform took back from it — where no round is under way
// to drop it from the list. A thread that has ended is nothing a checkpoint
// restores, so the next start takes its stack: three times the bound, started
// that way, run on one stack, and the session saves and loads. Sixteen threads
// running at once are still the most there can be.
func TestThreadsThatEndInsideAPlatformCallGiveTheirStackBack(t *testing.T) {
	for _, ending := range []struct {
		name string
		// sleep has the loader park while it holds the lock, so it ends in
		// the platform's lock rather than in the wait.
		sleep bool
	}{
		{"inside the wait", false},
		{"inside the lock", true},
	} {
		t.Run(ending.name, func(t *testing.T) {
			store, _ := backend.NewMemorySaveStore(nil)
			fixture := newJavaThreadFixture(t, store)
			client := fixture.client
			runtime := client.javaRuntimeState()
			ctx := context.Background()
			lock, err := newTestObject(t, client, "java/lang/Object")
			if err != nil {
				t.Fatal(err)
			}
			start, err := client.stub(svcCategoryJava, javaVirtualSlot(javaThreadClass, 10))
			if err != nil {
				t.Fatal(err)
			}
			// run() { synchronized (lock) { loaded++; } }
			loader := fixture.install(func(a *assembler) {
				a.emit(armPushLR)
				fixture.call(a, "enter", lock)
				if ending.sleep {
					fixture.call(a, "sleep", 10, 0)
				}
				fixture.increment(a, 1)
				fixture.call(a, "exit", lock)
				a.emit(armPopPC)
			})
			_, runnable := fixture.newClass("Loader", loader)
			const loads = 3 * maxJavaThreads
			threads := make([]uint32, loads)
			for index := range threads {
				if threads[index], err = newTestObject(t, client, javaThreadClass); err != nil {
					t.Fatal(err)
				}
				if _, err := javaThreadConstructor(client, ctx, nil, []uint32{threads[index], runnable}); err != nil {
					t.Fatal(err)
				}
			}
			list, err := client.allocateWords(threads)
			if err != nil {
				t.Fatal(err)
			}
			// for (Thread loader : loaders) {
			//     loader.start();
			//     synchronized (lock) { lock.wait(); }
			//     started++;
			// }
			startApp := fixture.install(func(a *assembler) {
				a.emit(armPushLR)
				a.literal(6, list)
				a.literal(7, loads)
				top := a.here()
				a.emit(armLdrPost(0, 6, 4))
				a.literal(5, start)
				a.call(5)
				fixture.call(a, "enter", lock)
				fixture.call(a, "wait", lock)
				fixture.call(a, "exit", lock)
				fixture.increment(a, 0)
				a.emit(0xe2577001) // subs r7, r7, #1
				back := int32(int64(top)-int64(a.here()+8)) / 4
				a.emit(0x1a000000 | uint32(back)&0xffffff) // bne top
				a.emit(armPopPC)
			})
			if _, err := client.call(ctx, startApp, nil); err != nil {
				t.Fatalf("a startApp that starts %d loaders one at a time: %v", loads, err)
			}
			if words := fixture.words(fixture.session); words[0] != loads || words[1] != loads {
				t.Fatalf("startApp counted %d starts and the loaders %d loads, want %d of each", words[0], words[1], loads)
			}
			if runtime.threadStacks != 1 || len(runtime.workers) > 1 {
				t.Fatalf("%d loaders run one at a time left %d stacks mapped and %d threads listed",
					loads, runtime.threadStacks, len(runtime.workers))
			}
			fixture.settle()
			checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
			if java := decodeRuntime(t, checkpoint).Client.Java; java.ThreadStacks != 1 || len(java.Workers) > 1 {
				t.Fatalf("the record holds %d threads on %d stacks", len(java.Workers), java.ThreadStacks)
			}
			fixture.compare(fixture.restore(checkpoint), 2)

			// Threads that run at once still take a stack each, up to the bound.
			sleeper := fixture.install(func(a *assembler) {
				top := a.here()
				fixture.increment(a, 2)
				fixture.call(a, "sleep", 30, 0)
				fixture.loop(a, top)
			})
			for index := range maxJavaThreads {
				fixture.start("Sleeper"+string(rune('A'+index)), sleeper)
			}
			_, extra := fixture.newClass("Extra", sleeper)
			var throw *javaUncaughtThrow
			if _, err := javaThreadStart(client, ctx, client.thread, []uint32{extra}); !errors.As(err, &throw) ||
				throw.Class != javaOutOfMemoryClass {
				t.Fatalf("a start past %d running threads: %v, want %s", maxJavaThreads, err, javaOutOfMemoryClass)
			}
			if runtime.threadStacks != maxJavaThreads {
				t.Fatalf("%d stacks are mapped for %d running threads", runtime.threadStacks, maxJavaThreads)
			}
		})
	}
}

// The specification puts OutOfMemoryError beneath VirtualMachineError, so the
// start past the bound reaches a handler for either, for Error and for
// Throwable, and no handler for Exception. The test is the one the title's
// handler makes: the class read through the thrown object's vtable, against
// the name its catch clause wrote.
func TestAStartPastTheThreadBoundIsAVirtualMachineError(t *testing.T) {
	client := fixtureClient(t)
	defer client.StopJavaThreads()
	caller := armcore.NewThread(armcore.NewContext())
	for index := range maxJavaThreads {
		if _, err := javaThreadStart(client, nil, caller, []uint32{uint32(0x1000 + index*0x10)}); err != nil {
			t.Fatalf("start %d: %v", index+1, err)
		}
	}
	buffer, err := client.enterJavaTry()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.armJavaTry(caller, buffer); err != nil {
		t.Fatal(err)
	}
	if _, err := javaThreadStart(client, nil, caller, []uint32{0x8000}); !errors.Is(err, errJavaThrowHandled) {
		t.Fatalf("a caught start past %d running threads: %v, want it handled", maxJavaThreads, err)
	}
	thrown, _ := caller.Register(0)
	vtable, err := client.readWord(thrown)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.readWord(vtable)
	if err != nil {
		t.Fatal(err)
	}
	for _, clause := range []struct {
		class  string
		caught bool
	}{
		{javaOutOfMemoryClass, true},
		{"java/lang/VirtualMachineError", true},
		{"java/lang/Error", true},
		{"java/lang/Throwable", true},
		{"java/lang/Exception", false},
		{"java/lang/RuntimeException", false},
	} {
		name, err := client.allocateBytes([]byte(clause.class + "\x00"))
		if err != nil {
			t.Fatal(err)
		}
		answer, err := client.javaTypeCheck(identity, name)
		if err != nil || (answer == 1) != clause.caught {
			t.Errorf("catch (%s) answered %d (%v), want caught %v", clause.class, answer, err, clause.caught)
		}
	}
}

// newTestRunnable is a Runnable the title wrote whose run counts its calls in
// its own object (recordingWrite), declared in its record the way a class
// with member records declares one.
func newTestRunnable(t *testing.T, client *Client, name string, offset uint32) uint32 {
	t.Helper()
	runnable := installGuestSubclass(t, client, name, javaObjectClass, 64, 0)
	class, _ := client.javaClassOfObject(runnable)
	class.Record.Inline = true
	class.Record.Methods = []javaMember{{Name: "run", Descriptor: "()V",
		Body: installThumbAt(t, client, offset, recordingWrite...)}}
	return runnable
}

// Thread.run called as a method runs the Runnable the thread was built with,
// on the calling thread, once per call; a thread built with none runs nothing,
// and a Thread handed over as the Runnable runs its own in turn.
func TestJavaThreadRunRunsItsRunnable(t *testing.T) {
	client := fixtureClient(t)
	task := newTestRunnable(t, client, "game/Task", 0)
	thread, err := newTestObject(t, client, javaThreadClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Thread.<init>(Ljava/lang/Runnable;)V", thread, task)
	mustSlot(t, client, javaThreadClass, javaThreadSlotRun, thread)
	if got := len(recorded(t, client, task)); got != 1 {
		t.Fatalf("run() ran the Runnable %d times, want once", got)
	}

	bare, err := newTestObject(t, client, javaThreadClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Thread.<init>()V", bare)
	mustSlot(t, client, javaThreadClass, javaThreadSlotRun, bare)

	outer, err := newTestObject(t, client, javaThreadClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Thread.<init>(Ljava/lang/Runnable;)V", outer, thread)
	mustSlot(t, client, javaThreadClass, javaThreadSlotRun, outer)
	if got := len(recorded(t, client, task)); got != 2 {
		t.Errorf("run() through a Thread handed over as the Runnable ran it %d times in all, want twice", got)
	}
	if runtime := client.javaRuntimeState(); runtime.threads[thread].worker != nil || runtime.threads[outer].worker != nil {
		t.Error("run() started a thread")
	}
}

// getPriority answers what setPriority kept, and NORM_PRIORITY before.
func TestJavaThreadGetPriority(t *testing.T) {
	client := fixtureClient(t)
	thread, err := javaCurrentThread(client, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustSlot(t, client, javaThreadClass, 15, thread); got != 5 {
		t.Errorf("getPriority() before any setPriority = %d, want 5", got)
	}
	mustSlot(t, client, javaThreadClass, 14, thread, 10)
	if got := mustSlot(t, client, javaThreadClass, 15, thread); got != 10 {
		t.Errorf("getPriority() after setPriority(10) = %d, want 10", got)
	}
}

// activeCount counts the threads running, and the platform's own once a title
// has its Thread object, and is never less than one.
func TestJavaThreadActiveCount(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	count := func() uint32 {
		t.Helper()
		return mustJavaCall(t, client, "java/lang/Thread.activeCount()I")
	}
	if got := count(); got != 1 {
		t.Errorf("activeCount() with nothing started = %d, want 1", got)
	}
	runtime.threads[0x2000] = &javaThread{object: 0x2000, runnable: 0x2000}
	runtime.threads[0x3000] = &javaThread{object: 0x3000, runnable: 0x3000, worker: &javaWorker{}}
	runtime.threads[0x4000] = &javaThread{object: 0x4000, runnable: 0x4000, worker: &javaWorker{}}
	runtime.threads[0x5000] = &javaThread{object: 0x5000, runnable: 0x5000, worker: &javaWorker{done: true}}
	if got := count(); got != 2 {
		t.Errorf("activeCount() with two running = %d, want 2", got)
	}
	if _, err := javaCurrentThread(client, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 3 {
		t.Errorf("activeCount() with two running and the platform's own = %d, want 3", got)
	}
}
