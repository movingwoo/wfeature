package ktf

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// TestAnUncaughtGuestExceptionEndsTheCallbackAndNotTheSession pins the rule the
// browser sweep's exception family came down to. A title whose frame loop is a
// thread catches its own throw on that thread; the paint this Host starts runs
// on the client thread, whose handler chain is empty, so the same throw used to
// end the session. It ends the callback now, and it is still reported.
func TestAnUncaughtGuestExceptionEndsTheCallbackAndNotTheSession(t *testing.T) {
	client, _ := newTestRuntime(t)
	guest := &jvm.GuestException{
		Object:  &jvm.Object{ClassName: "java/lang/ArrayIndexOutOfBoundsException"},
		Message: "thrown by guest code at 0x7f000000",
	}

	// Wrapped the way a service call wraps it on the way out.
	wrapped := fmt.Errorf("paint KTF card SomeCard: %w", guest)
	if err := client.absorbUncaughtCallback("paint", wrapped); err != nil {
		t.Fatalf("an uncaught guest exception ended the tick: %v", err)
	}
	count, first := client.UncaughtCallbacks()
	if count != 1 {
		t.Fatalf("uncaught callbacks = %d, want 1", count)
	}
	if !strings.HasPrefix(first, "paint: ") || !strings.Contains(first, "ArrayIndexOutOfBounds") {
		t.Fatalf("first uncaught = %q", first)
	}

	// A second one counts and leaves the first message standing: the first is
	// what says where a title started failing.
	if err := client.absorbUncaughtCallback("thread", fmt.Errorf("run KTF guest thread: %w", guest)); err != nil {
		t.Fatal(err)
	}
	if count, again := client.UncaughtCallbacks(); count != 2 || again != first {
		t.Fatalf("after a second absorb: count = %d, first = %q", count, again)
	}
}

// Only an exception is absorbed. A guest fault, a limit a Host imposes and a
// platform error are all still the end of the run — absorbing one of those
// would turn a broken emulator into a title that quietly draws nothing.
func TestOnlyAGuestExceptionIsAbsorbed(t *testing.T) {
	client, _ := newTestRuntime(t)
	for _, err := range []error{
		fmt.Errorf("paint KTF card SomeCard: %w", &armcore.InstructionError{PC: 0x1000, Cause: errors.New("guest memory is not mapped")}),
		fmt.Errorf("KTF Host service call exceeded its step allowance"),
		ErrGuestExited,
	} {
		if got := client.absorbUncaughtCallback("paint", err); !errors.Is(got, err) {
			t.Fatalf("absorbed %v", err)
		}
	}
	if count, _ := client.UncaughtCallbacks(); count != 0 {
		t.Fatalf("uncaught callbacks = %d, want 0", count)
	}
	if client.absorbUncaughtCallback("paint", nil) != nil {
		t.Fatal("a nil error came back as an error")
	}
}
