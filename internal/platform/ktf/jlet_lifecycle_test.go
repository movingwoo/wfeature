package ktf

import (
	"context"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// A Host parks a game whose page has gone away rather than closing it, and a
// handset did the same when a call took the screen. The Jlet's own callbacks
// are how the application finds out — and this is the test that would have
// failed for as long as nothing here ran them.
//
// It runs real guest code: the fixture's pauseApp and resumeApp are Thumb
// bodies in the class's method table, and what they write into the receiver is
// read back out of guest memory.
func TestParkingAJletRunsItsOwnCallbacks(t *testing.T) {
	ctx := context.Background()
	client := syntheticLifecycleClient(t)
	object, _, err := client.NewObject(ctx, "game/Lifecycle", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	client.runtime.activeJlet = object

	if got := lifecycleMarker(t, client, object); got != 0 {
		t.Fatalf("the fixture starts at %d, want 0", got)
	}
	if err := client.PauseApp(ctx); err != nil {
		t.Fatalf("PauseApp() error = %v", err)
	}
	if got := lifecycleMarker(t, client, object); got != 2 {
		t.Fatalf("the Jlet is %d after a pause, want 2 — its pauseApp did not run", got)
	}
	if err := client.ResumeApp(ctx); err != nil {
		t.Fatalf("ResumeApp() error = %v", err)
	}
	if got := lifecycleMarker(t, client, object); got != 3 {
		t.Fatalf("the Jlet is %d after a resume, want 3 — its resumeApp did not run", got)
	}
	// The whole point of parking is that the game is still there afterwards.
	// A method that worked before the pair has to work after it, on the same
	// client thread the callbacks just used.
	result, err := client.InvokeVirtual(ctx, object, "add", "(I)I", jvm.IntValue(9))
	if err != nil {
		t.Fatalf("InvokeVirtual(add) after a park and a resume: %v", err)
	}
	if value, err := result.Result.Int32(); err != nil || value != 14 {
		t.Fatalf("InvokeVirtual(add) = %d/%v, want 14", value, err)
	}
}

// A title that declares no lifecycle callback has nothing to call, and half the
// local ones compile theirs to a prologue and a return, which says the same
// thing more expensively. Neither is an error: a Host that refused to park a
// game over it would be refusing to park a game that is perfectly well.
func TestAJletWithNoLifecycleCallbackIsNothingToCall(t *testing.T) {
	client, runtime := newTestRuntime(t)
	class := writeGuestClassWithMethod(t, runtime, "game/Quiet", 0, "startApp", "([Ljava/lang/String;)V")
	if _, err := runtime.resolveAOTClass(class); err != nil {
		t.Fatal(err)
	}
	runtime.activeJlet = &jvm.Object{ClassName: "game/Quiet"}

	if runtime.hasAOTMethod("game/Quiet", jletPauseApp, "()V") {
		t.Fatal("a class that declares only startApp reports a pauseApp")
	}
	if err := client.PauseApp(context.Background()); err != nil {
		t.Fatalf("PauseApp() over a Jlet with no pauseApp = %v", err)
	}
	if err := client.ResumeApp(context.Background()); err != nil {
		t.Fatalf("ResumeApp() over a Jlet with no resumeApp = %v", err)
	}
}

// The callback is looked up the same two ways an invocation looks it up, so one
// inherited from a base class in the same image is found — which is the case
// that used to report a title's own startApp missing.
func TestAnInheritedLifecycleCallbackIsFound(t *testing.T) {
	_, runtime := newTestRuntime(t)
	base := writeGuestClassWithMethod(t, runtime, "game/Base", 0, jletPauseApp, "()V")
	leaf := writeGuestClass(t, runtime, "game/Leaf", base, nil, 0x21)
	if _, err := runtime.resolveAOTClass(leaf); err != nil {
		t.Fatal(err)
	}
	if !runtime.hasAOTMethod("game/Leaf", jletPauseApp, "()V") {
		t.Fatal("a pauseApp inherited from a base class in the same image was not found")
	}
	if runtime.hasAOTMethod("game/Leaf", jletResumeApp, "()V") {
		t.Fatal("a resumeApp nothing declares was found")
	}
	if runtime.hasAOTMethod("game/Missing", jletPauseApp, "()V") {
		t.Fatal("a class nothing registered answered for a method")
	}
}

// Nothing constructed a Jlet, so there is no application with a lifecycle. A
// Host still parks the session — a game is its guest memory, not its Jlet.
func TestParkingBeforeThereIsAJletIsNotAFailure(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.activeJlet = nil
	if err := client.PauseApp(context.Background()); err != nil {
		t.Fatalf("PauseApp() before a Jlet exists = %v", err)
	}
	if err := client.ResumeApp(context.Background()); err != nil {
		t.Fatalf("ResumeApp() before a Jlet exists = %v", err)
	}
}

// A session being torn down runs no callbacks. Whatever they were going to tell
// the application, the application is about to stop existing — and a stopped
// worker is the one place where entering guest code would be entering it on top
// of a stack that is already unwinding. See "Guest workers unwound together".
func TestParkingASessionWhoseWorkersHaveStoppedRunsNothing(t *testing.T) {
	ctx := context.Background()
	client := syntheticLifecycleClient(t)
	object, _, err := client.NewObject(ctx, "game/Lifecycle", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	client.runtime.activeJlet = object
	client.workersStopped = true

	if err := client.PauseApp(ctx); err != nil {
		t.Fatalf("PauseApp() on a stopped session = %v", err)
	}
	if got := lifecycleMarker(t, client, object); got != 0 {
		t.Fatalf("the Jlet is %d, want 0 — a stopped session ran guest code", got)
	}
	if err := client.ResumeApp(ctx); err != nil {
		t.Fatalf("ResumeApp() on a stopped session = %v", err)
	}
	if got := lifecycleMarker(t, client, object); got != 0 {
		t.Fatalf("the Jlet is %d, want 0 — a stopped session ran guest code", got)
	}
}
