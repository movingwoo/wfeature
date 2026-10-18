package lgt

import (
	"context"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

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
