package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// A Runnable handed to callSerially runs on the client thread, so a wait the
// guest declared there holds it exactly as it holds a paint. A title whose
// frame loop is such a Runnable sleeps inside it; reporting the Runnable as
// due before the sleep ended left a Host on a manual clock with work it would
// not run and a clock it therefore never advanced.
func TestSerialRunnableIsNotDueBeforeTheClientWaitEnds(t *testing.T) {
	client, runtime := newTestRuntime(t)
	clock := NewManualClock(time.Time{})
	client.clock = clock

	runtime.pendingSerial = []*jvm.Object{{ClassName: "Frame"}}
	runtime.serialDueAt = client.now()
	client.deferClientWait(50 * time.Millisecond)

	deadline, parked := client.NextDeadline()
	if !parked {
		t.Fatal("NextDeadline reported nothing parked with a Runnable queued")
	}
	if want := client.clientWakeAt; !deadline.Equal(want) {
		t.Fatalf("NextDeadline() = %v, want the client wait's end %v", deadline, want)
	}

	clock.Advance(50 * time.Millisecond)
	if !client.clientThreadDue() {
		t.Fatal("the client thread is still not due after its own wait elapsed")
	}
}

// A repaint the guest asked for is dispatched before the next Runnable,
// because on the original event loop the two share one queue and the repaint
// was posted first. Without the ordering the Runnable takes the round the
// paint was finally due in, and the title draws nothing at all.
func TestRepaintQueuedHoldsTheSerialQueue(t *testing.T) {
	_, runtime := newTestRuntime(t)
	if runtime.repaintQueued() {
		t.Fatal("a repaint is queued before anything asked for one")
	}
	runtime.repaintPending = true
	if runtime.repaintQueued() {
		t.Fatal("a repaint with no card to paint holds the queue, which never ends")
	}
	runtime.displayCards = []*jvm.Object{{ClassName: "Card"}}
	if !runtime.repaintQueued() {
		t.Fatal("a repaint with a card to paint does not hold the queue")
	}
}

// Running off the end of a guest stack is an access to unmapped memory a few
// bytes below a mapping, which reads like a wild pointer. The two are
// investigated in opposite directions, so the report says which it is.
func TestGuestStackOverflowIsNamed(t *testing.T) {
	client, _ := newTestRuntime(t)
	if note := client.describeStackOverflow(ThreadStackBase-8, ThreadStackBase-8); note == "" {
		t.Fatal("an access below the client stack is not reported as an overflow")
	}
	// A push that has already moved the stack pointer past the end faults on
	// the store rather than on the pointer, so the stack pointer is checked
	// as well as the address.
	if note := client.describeStackOverflow(ThreadStackBase+16, ThreadStackBase-4); note == "" {
		t.Fatal("a store from a stack pointer below the client stack is not reported as an overflow")
	}
	if note := client.describeStackOverflow(ImageBase, ThreadStackBase+64); note != "" {
		t.Fatalf("an ordinary image address is reported as an overflow: %s", note)
	}
}
