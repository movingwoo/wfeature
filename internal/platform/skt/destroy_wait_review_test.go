package skt

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// A destroyApp that never returns — one that loops, and one that waits for a
// notification nobody sends — holds a closing session for destroyWait and no
// longer: the call is ended there, the runtime is finished, and the close goes
// on to store what the title wrote. Before the bound it held the close for
// ever.
func TestAnUnconditionalDestroyThatNeverReturnsIsEnded(t *testing.T) {
	wait := destroyWait
	destroyWait = 200 * time.Millisecond
	t.Cleanup(func() { destroyWait = wait })
	for _, mode := range []string{"loop", "wait"} {
		runtime := startLifecycleFixture(t)
		if _, err := runtime.VM.InvokeStatic("LifecycleMIDlet", "hangNextDestroy", "(Ljava/lang/String;)V", jvm.ReferenceValue(runtime.VM.NewString(mode))); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- runtime.Destroy(true) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("a destroyApp that %ss still holds Destroy(true)", mode)
		}
		if state := runtime.State(); state != StateDestroyed && state != StateError {
			t.Fatalf("after a destroyApp that %ss the runtime is %s", mode, state)
		}
		if err := runtime.renewGuestSteps(); err == nil {
			t.Fatalf("after a destroyApp that %ss a step window is still renewed", mode)
		}
	}
}

// A destroyApp that returns in time is not cut short by the bound, and the
// bound does not outlive it: the runtime is destroyed as before.
func TestAnUnconditionalDestroyInTimeIsNotEnded(t *testing.T) {
	wait := destroyWait
	destroyWait = 500 * time.Millisecond
	t.Cleanup(func() { destroyWait = wait })
	runtime := startLifecycleFixture(t)
	started := time.Now()
	if err := runtime.Destroy(true); err != nil {
		t.Fatalf("Destroy(true) error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > destroyWait/2 {
		t.Skipf("destroyApp took %s here, too near the bound to tell", elapsed)
	}
	if runtime.State() != StateDestroyed {
		t.Fatalf("runtime state = %s, want destroyed", runtime.State())
	}
	time.Sleep(2 * destroyWait)
	if runtime.destroyExpired.Load() {
		t.Fatal("the bound fired after destroyApp had returned")
	}
}
