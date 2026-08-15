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
