package lgt

import "testing"

// A title that starts a short-lived thread per scene starts thousands over a
// session. A finished thread's stack is taken by the next one, so the stacks
// mapped are as many as ran at once rather than as many as ever started —
// which is also what keeps a checkpoint, which allows maxJavaThreads stacks,
// loadable.
func TestAFinishedThreadsStackIsTakenByTheNextOne(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	defer client.StopJavaThreads()
	finish := func(worker *javaWorker) {
		// What a round does with a worker whose run ended: it is recorded
		// as done and dropped from the list. Its goroutine has not been
		// granted a slice here, so closing grant is what ends it.
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

	first, err := client.startJavaWorker(&javaThread{object: 0x100})
	if err != nil {
		t.Fatal(err)
	}
	// A stack the guest used, so a reuse that kept its words would show.
	if err := client.writeWord(first.stackBase+16, 0xdeadbeef); err != nil {
		t.Fatal(err)
	}
	finish(first)
	for index := range 5000 {
		next, err := client.startJavaWorker(&javaThread{object: 0x200})
		if err != nil {
			t.Fatalf("start %d: %v", index, err)
		}
		if next.stackBase != first.stackBase {
			t.Fatalf("start %d took the stack at %#x, not the finished one at %#x", index, next.stackBase, first.stackBase)
		}
		finish(next)
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

// The Thread constructor's bound is on threads that can still run. A finished
// thread nothing has collected yet is not one, so a title that builds a
// thread per scene is not stopped by the ones it has already finished with.
func TestTheThreadBoundCountsOnlyThreadsThatCanRun(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	for index := range maxJavaThreads {
		object := uint32(0x1000 + index*0x10)
		runtime.threads[object] = &javaThread{object: object, runnable: object, worker: &javaWorker{done: true}}
	}
	if _, err := javaThreadConstructor(client, nil, nil, []uint32{0x8000, 0x8000}); err != nil {
		t.Fatalf("a thread was refused for %d finished ones: %v", maxJavaThreads, err)
	}
	for index := range maxJavaThreads - 1 {
		object := uint32(0x9000 + index*0x10)
		if _, err := javaThreadConstructor(client, nil, nil, []uint32{object, object}); err != nil {
			t.Fatalf("thread %d: %v", index+2, err)
		}
	}
	if _, err := javaThreadConstructor(client, nil, nil, []uint32{0xa000, 0xa000}); err == nil {
		t.Fatalf("a thread past %d that can run was built", maxJavaThreads)
	}
}
