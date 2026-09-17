package jvm

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestThreadRootsRetireAndCannotRestart(t *testing.T) {
	for _, failed := range []bool{false, true} {
		vm := New(nil, Options{GuestThreadStarter: func(*Object) error {
			if failed {
				return errors.New("start failed")
			}
			return nil
		}})
		thread := &Object{ClassName: ThreadClass}
		vm.threadState(thread)
		if len(vm.ThreadObjects()) != 0 {
			t.Fatal("unstarted thread rooted")
		}
		_, err := vm.InvokeVirtual(thread, "start", "()V")
		if (err != nil) != failed {
			t.Fatalf("start = %v", err)
		}
		if !failed {
			if len(vm.ThreadObjects()) != 1 {
				t.Fatal("live thread not rooted")
			}
			vm.EndGuestThread(thread)
		}
		vm.EndGuestThread(thread)
		if len(vm.ThreadObjects()) != 0 {
			t.Fatal("ended thread rooted")
		}
		_, err = vm.InvokeVirtual(thread, "start", "()V")
		if !vm.IsGuestException(err, "java/lang/IllegalThreadStateException") {
			t.Fatalf("restart = %v", err)
		}
	}
}

func TestJoinMultipleWaitersInterruptAndClose(t *testing.T) {
	for _, action := range []string{"finish", "interrupt", "close"} {
		t.Run(action, func(t *testing.T) {
			vm := New(nil, Options{})
			t.Cleanup(vm.Close)
			target := &Object{ClassName: ThreadClass}
			state := vm.threadState(target)
			state.started, state.alive = true, true
			results := make(chan error, 2)
			waiters := []*Object{{ClassName: ThreadClass}, {ClassName: ThreadClass}}
			for _, waiter := range waiters {
				execution := vm.newExecution()
				execution.thread = waiter
				go func() { _, err := threadJoin(vm, execution, []Value{ReferenceValue(target)}); results <- err }()
			}
			switch action {
			case "finish":
				vm.EndGuestThread(target)
			case "close":
				vm.Close()
			case "interrupt":
				for _, waiter := range waiters {
					if _, err := vm.InvokeVirtual(waiter, "interrupt", "()V"); err != nil {
						t.Fatal(err)
					}
				}
			}
			for range waiters {
				select {
				case err := <-results:
					if action == "finish" && err != nil {
						t.Fatal(err)
					}
					if action == "close" && !errors.Is(err, ErrClosed) {
						t.Fatalf("close = %v", err)
					}
					if action == "interrupt" && !vm.IsGuestException(err, "java/lang/InterruptedException") {
						t.Fatalf("interrupt = %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("join did not finish")
				}
			}
			for _, waiter := range waiters {
				if vm.threadState(waiter).interrupted {
					t.Fatal("interrupt not cleared")
				}
			}
		})
	}
}

func TestCooperativeJoinRefusesLiveThread(t *testing.T) {
	vm := New(nil, Options{GuestThreadStarter: func(*Object) error { return nil }})
	thread := &Object{ClassName: ThreadClass}
	if _, err := vm.InvokeVirtual(thread, "join", "()V"); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(thread, "start", "()V"); err != nil {
		t.Fatal(err)
	}
	_, err := vm.InvokeVirtual(thread, "join", "()V")
	if err == nil || !strings.Contains(err.Error(), "cooperative scheduler") {
		t.Fatalf("join = %v", err)
	}
	vm.EndGuestThread(thread)
}
