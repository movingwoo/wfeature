package jvm

import (
	"strings"
	"testing"
	"time"
)

// A platform that installs GuestThreadStarter takes over Thread.start, and
// with it the end of the thread: the goroutine that would otherwise have
// cleared the flag never ran. Nothing else can know when a run the platform's
// own scheduler drove has finished, so the platform reports it — and a title
// whose loading screen is `while (loader.isAlive()) sleep()` waits for the
// whole session if it does not.
func TestGuestThreadEndsWhenThePlatformSaysSo(t *testing.T) {
	started := 0
	vm := New(nil, Options{GuestThreadStarter: func(*Object) error {
		started++
		return nil
	}})
	thread := &Object{ClassName: ThreadClass, Fields: map[string]Value{}}

	alive := func() bool {
		t.Helper()
		result, err := vm.InvokeVirtual(thread, "isAlive", "()Z")
		if err != nil {
			t.Fatalf("isAlive() error = %v", err)
		}
		value, err := result.Int32()
		if err != nil {
			t.Fatal(err)
		}
		return value != 0
	}

	if alive() {
		t.Fatal("a thread that was never started is alive")
	}
	if _, err := vm.InvokeVirtual(thread, "start", "()V"); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("the platform was asked to start %d threads, want 1", started)
	}
	if !alive() {
		t.Fatal("a started thread is not alive")
	}
	vm.EndGuestThread(thread)
	if alive() {
		t.Fatal("a thread whose run() returned is still alive")
	}
}

// A guest thread runs on a goroutine of its own, and a panic on a goroutine
// takes the process with it unless that goroutine catches it. That process is
// the one holding the session's post-mortem report, its boundary trace and
// every other session — so an unsupported title's thread has to end as an
// error the Host can report, not as an exit.
//
// This test crashes the whole test binary if the containment is removed, which
// is exactly what it is for.
func TestGuestThreadPanicBecomesAnError(t *testing.T) {
	failures := make(chan error, 1)
	vm := New(nil, Options{AsyncError: func(err error) { failures <- err }})
	vm.builtin(ThreadClass, "run", "()V", func(*VM, []Value) (Value, error) {
		panic("a guest thread went off the rails")
	})
	thread := &Object{ClassName: ThreadClass, Fields: map[string]Value{}}

	if _, err := vm.InvokeVirtual(thread, "start", "()V"); err != nil {
		t.Fatalf("start() error = %v", err)
	}

	select {
	case err := <-failures:
		if !strings.Contains(err.Error(), "panicked") {
			t.Errorf("error = %q, want it to say the thread panicked", err)
		}
		if !strings.Contains(err.Error(), "went off the rails") {
			t.Errorf("error = %q, want it to carry the panic", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a panicking guest thread reported nothing")
	}

	// The thread is over however it ended. A title whose loading screen is
	// `while (loader.isAlive()) sleep()` waits for the session otherwise.
	state := vm.threadState(thread)
	state.mu.Lock()
	alive := state.alive
	state.mu.Unlock()
	if alive {
		t.Error("a thread that panicked is still alive")
	}
}

// A platform call that models something the handset did not return from until
// it finished — a sound played to its end — waits on the thread the game
// started and does not wait on the Host's own pass. Blocking the pass stops the
// screen, the input and the timers together, so a caller there is told it
// cannot wait and answers without one.
func TestWaitAsGuestThreadWaitsOnlyOnAGuestThread(t *testing.T) {
	const wait = 60 * time.Millisecond
	waited := make(chan bool, 2)
	elapsed := make(chan time.Duration, 2)
	body := func(call *Invocation, _ []Value) (Value, error) {
		started := time.Now()
		ok := call.WaitAsGuestThread(wait, nil)
		elapsed <- time.Since(started)
		waited <- ok
		return VoidValue(), nil
	}

	vm := New(nil, Options{})
	// The waiting method is the thread's own run, because that is the shape
	// this models: the platform call is entered from the game's thread.
	if err := vm.DefineClass(ClassDefinition{
		Name:      "net/wfeature/Waiter",
		SuperName: ThreadClass,
		Access:    AccessPublic,
		Methods: []MethodDefinition{
			{Name: "run", Descriptor: "()V", Access: AccessPublic, Body: body},
			{Name: "now", Descriptor: "()V", Access: AccessPublic | AccessStatic, Body: body},
		},
	}); err != nil {
		t.Fatalf("DefineClass() error = %v", err)
	}

	// The Host's own pass: no thread, no wait.
	if _, err := vm.InvokeStatic("net/wfeature/Waiter", "now", "()V"); err != nil {
		t.Fatalf("now() error = %v", err)
	}
	if ok := <-waited; ok {
		t.Error("a call off a guest thread reported that it waited")
	}
	if took := <-elapsed; took >= wait {
		t.Errorf("a call off a guest thread took %v, want it to return at once", took)
	}

	// A thread the game started: the wait is that thread's to take.
	thread := &Object{ClassName: "net/wfeature/Waiter", Fields: map[string]Value{}}
	if _, err := vm.InvokeVirtual(thread, "start", "()V"); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	select {
	case ok := <-waited:
		if !ok {
			t.Error("a call on a guest thread reported that it did not wait")
		}
		if took := <-elapsed; took < wait {
			t.Errorf("the wait took %v, want at least %v", took, wait)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the guest thread never returned from the wait")
	}
}

// The wait ends early when what it is waiting for is over — a title that stops
// its own music is not held for the rest of the piece.
func TestWaitAsGuestThreadEndsWhenTheReasonEnds(t *testing.T) {
	done := make(chan time.Duration, 1)
	over := make(chan struct{})

	vm := New(nil, Options{})
	if err := vm.DefineClass(ClassDefinition{
		Name:      "net/wfeature/Waiter",
		SuperName: ThreadClass,
		Access:    AccessPublic,
		Methods: []MethodDefinition{{
			Name:       "run",
			Descriptor: "()V",
			Access:     AccessPublic,
			Body: func(call *Invocation, _ []Value) (Value, error) {
				started := time.Now()
				if !call.WaitAsGuestThread(time.Minute, over) {
					t.Error("the thread's own run did not wait")
				}
				done <- time.Since(started)
				return VoidValue(), nil
			},
		}},
	}); err != nil {
		t.Fatalf("DefineClass() error = %v", err)
	}

	thread := &Object{ClassName: "net/wfeature/Waiter", Fields: map[string]Value{}}
	if _, err := vm.InvokeVirtual(thread, "start", "()V"); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	close(over)
	select {
	case took := <-done:
		if took >= time.Minute {
			t.Errorf("the wait took %v, want it cut short", took)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the wait was not cut short")
	}
}
