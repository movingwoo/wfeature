package lgt

import (
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
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
