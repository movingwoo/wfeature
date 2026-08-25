package ktf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// TestDrawRectOutlinesTheRectangleFillWouldCover pins MC_grpDrawRect against
// MC_grpFillRect: both describe the same w by h box, so the outline has to sit
// on that box's own border with the interior left alone.
func TestDrawRectOutlinesTheRectangleFillWouldCover(t *testing.T) {
	_, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	framebuffer := graphics.Native.(*runtimeGraphicsState).target

	const pixel = 0xf81f
	if err := runtime.outlineFramebufferRect(framebuffer, wipicClip{}, wipicPixelOp{}, pixel, 4, 6, 5, 4); err != nil {
		t.Fatalf("outlineFramebufferRect() error = %v", err)
	}
	for y := int32(5); y <= 10; y++ {
		for x := int32(3); x <= 9; x++ {
			onBorder := (x >= 4 && x <= 8 && (y == 6 || y == 9)) || (y >= 6 && y <= 9 && (x == 4 || x == 8))
			got := readFramebufferPixel(t, runtime, framebuffer, x, y)
			if onBorder && got != pixel {
				t.Fatalf("border pixel (%d, %d) = %#x, want %#x", x, y, got, pixel)
			}
			if !onBorder && got != 0 {
				t.Fatalf("pixel (%d, %d) = %#x, want it untouched", x, y, got)
			}
		}
	}
}

// TestDrawRectFillsRectanglesWithoutAnInterior covers the thin cases: a
// rectangle one or two pixels across has no interior to preserve, and drawing
// four overlapping edges over it would plot the same pixels twice.
func TestDrawRectFillsRectanglesWithoutAnInterior(t *testing.T) {
	_, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	framebuffer := graphics.Native.(*runtimeGraphicsState).target

	const pixel = 0x07e0
	if err := runtime.outlineFramebufferRect(framebuffer, wipicClip{}, wipicPixelOp{}, pixel, 2, 2, 2, 3); err != nil {
		t.Fatalf("outlineFramebufferRect() error = %v", err)
	}
	for y := int32(2); y < 5; y++ {
		for x := int32(2); x < 4; x++ {
			if got := readFramebufferPixel(t, runtime, framebuffer, x, y); got != pixel {
				t.Fatalf("pixel (%d, %d) = %#x, want the whole thin rectangle filled", x, y, got)
			}
		}
	}
	// An empty rectangle draws nothing rather than a single stray pixel.
	if err := runtime.outlineFramebufferRect(framebuffer, wipicClip{}, wipicPixelOp{}, pixel, 20, 20, 0, 5); err != nil {
		t.Fatalf("outlineFramebufferRect() error = %v", err)
	}
	if got := readFramebufferPixel(t, runtime, framebuffer, 20, 20); got != 0 {
		t.Fatalf("empty rectangle drew pixel %#x", got)
	}
}

func readFramebufferPixel(t *testing.T, runtime *initializationRuntime, framebuffer wipicFramebuffer, x, y int32) uint16 {
	t.Helper()
	var data [2]byte
	address := framebuffer.pixels + uint32(y)*framebuffer.bpl + uint32(x)*2
	if err := runtime.client.core.Memory().Read(address, data[:]); err != nil {
		t.Fatal(err)
	}
	return uint16(data[0]) | uint16(data[1])<<8
}

// TestDatabaseSlot15AcceptsAnOpenStream covers the KTF custom slot a title
// calls while loading a save: it takes the open handle alone, and an unknown
// handle is refused the way the other handle-taking slots refuse one.
func TestDatabaseSlot15AcceptsAnOpenStream(t *testing.T) {
	_, runtime := newTestRuntime(t)
	store := &runtimeCFile{name: "OptionSave", data: []byte("options")}
	runtime.cFiles = map[string]*runtimeCFile{store.name: store}
	runtime.cFileHandles = map[uint32]*runtimeCFileHandle{
		cFileHandleBit | 1: {store: store, position: 3},
	}
	thread := armcore.NewThread(armcore.Context{})

	if err := thread.SetRegister(0, cFileHandleBit|1); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.handleWIPICFileCall(thread, wipicFileTouchStream)
	if err != nil {
		t.Fatalf("slot 15 error = %v", err)
	}
	if result != 0 {
		t.Fatalf("slot 15 = %#x, want 0", result)
	}
	if handle := runtime.cFileHandles[cFileHandleBit|1]; handle.position != 3 || string(handle.store.data) != "options" {
		t.Fatalf("slot 15 disturbed the stream: position %d data %q", handle.position, handle.store.data)
	}

	if err := thread.SetRegister(0, cFileHandleBit|9); err != nil {
		t.Fatal(err)
	}
	result, err = runtime.handleWIPICFileCall(thread, wipicFileTouchStream)
	if err != nil {
		t.Fatalf("slot 15 with an unknown handle error = %v", err)
	}
	if result != wipicErrorInvalid {
		t.Fatalf("slot 15 with an unknown handle = %#x, want %#x", result, wipicErrorInvalid)
	}
}

// TestHostServiceRenewsWindowsUntilItsAllowance covers the step policy a Host
// service call runs under: exhausting one window continues the call, spending
// the whole allowance fails it, and a cancelled Host context stops it wherever
// it is.
func TestHostServiceRenewsWindowsUntilItsAllowance(t *testing.T) {
	client, _ := newTestRuntime(t)
	window := client.core.MaxSteps()
	client.serviceSteps = window * 3

	ctx, cancel := context.WithCancel(context.Background())
	end := client.beginHostService(ctx)
	// Two renewals fit inside a three-window allowance; the third would leave
	// nothing to grant, so it reports the allowance instead of continuing.
	for renewal := 0; renewal < 2; renewal++ {
		if err := client.continueHostService(ctx, client.serviceSteps); err != nil {
			t.Fatalf("renewal %d error = %v", renewal, err)
		}
	}
	err := client.continueHostService(ctx, client.serviceSteps)
	if !errors.Is(err, ErrServiceStepLimit) {
		t.Fatalf("exhausted allowance error = %v, want ErrServiceStepLimit", err)
	}
	end()

	// A fresh call gets a fresh allowance, and the Host's cancellation beats
	// any budget that is left.
	end = client.beginHostService(ctx)
	defer end()
	cancel()
	if err := client.continueHostService(ctx, client.serviceSteps); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled service error = %v, want context.Canceled", err)
	}
}

// TestKeyDeliveryRunsInsideAServiceScope covers the wiring rather than the
// policy: the guest code a key reaches has to be running under the renewable
// budget, because that is the call a title spends a whole save load inside.
func TestKeyDeliveryRunsInsideAServiceScope(t *testing.T) {
	client, runtime := newTestRuntime(t)
	depth := -1
	if err := client.JVM().RegisterNative("test/Card", "keyNotify", "(II)Z", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		depth = client.serviceDepth
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = append(runtime.displayCards, &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)})

	if err := client.SendKey(t.Context(), KeyPressed, KeyFire); err != nil {
		t.Fatalf("SendKey() error = %v", err)
	}
	if depth != 1 {
		t.Fatalf("service depth during key delivery = %d, want 1", depth)
	}
	if client.serviceDepth != 0 {
		t.Fatalf("service depth after key delivery = %d, want 0", client.serviceDepth)
	}
}

// TestNestedHostServiceKeepsTheOuterAllowance guards the nesting rule: an
// inner scope must not hand the call a second allowance, which would make the
// ceiling unreachable.
func TestNestedHostServiceKeepsTheOuterAllowance(t *testing.T) {
	client, _ := newTestRuntime(t)
	ctx := context.Background()
	client.serviceSteps = client.core.MaxSteps() * 2

	outer := client.beginHostService(ctx)
	if err := client.continueHostService(ctx, client.serviceSteps); err != nil {
		t.Fatal(err)
	}
	inner := client.beginHostService(ctx)
	if client.serviceRemaining != client.core.MaxSteps() {
		t.Fatalf("nested scope reset the allowance to %d", client.serviceRemaining)
	}
	inner()
	if client.serviceDepth != 1 {
		t.Fatalf("service depth after closing the inner scope = %d, want 1", client.serviceDepth)
	}
	// The outer scope is still open, so the ceiling is still reachable from
	// where the inner scope left it.
	if err := client.continueHostService(ctx, client.serviceSteps); !errors.Is(err, ErrServiceStepLimit) {
		t.Fatalf("renewal after the inner scope = %v, want ErrServiceStepLimit", err)
	}
	outer()
	if client.serviceDepth != 0 {
		t.Fatalf("service depth after closing the outer scope = %d, want 0", client.serviceDepth)
	}
}

// TestSerialRunnablesDispatchOnePerIdlePass pins the pacing that separates a
// Display.callSerially Runnable from a started thread. A Runnable that
// re-queues itself is a whole title's frame loop, and dispatching it as fast as
// the Host turns rounds means the guest is never idle and the Host never
// sleeps. A backlog is the other case: more than one queued means the title is
// queueing faster than a frame drains, and holding those to one a frame only
// ends at the queue's limit.
func TestSerialRunnablesDispatchOnePerIdlePass(t *testing.T) {
	client, runtime := newTestRuntime(t)
	clock := NewManualClock(time.Unix(0, 0))
	client.clock = clock

	// The Runnables run on the client thread, so they need a body to run.
	// Counting the calls is also how the order they ran in is checked.
	var ran []string
	for _, class := range []string{"first", "second"} {
		name := class
		if err := client.JVM().RegisterNative(name, "run", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
			ran = append(ran, name)
			return jvm.VoidValue(), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, second := &jvm.Object{ClassName: "first"}, &jvm.Object{ClassName: "second"}
	runtime.pendingSerial = []*jvm.Object{first, second}
	// A limit of zero adopts without running any guest thread: what is under
	// test is which Runnables come off the queue and when.
	const adoptOnly = 0

	// The first pass takes one and only one, however many are queued.
	if _, err := client.ServiceThreads(context.Background(), adoptOnly); err != nil {
		t.Fatal(err)
	}
	if len(runtime.pendingSerial) != 1 {
		t.Fatalf("pendingSerial = %d after one pass, want the queue drained by one", len(runtime.pendingSerial))
	}
	if len(ran) != 1 || ran[0] != "first" {
		t.Fatalf("the first pass ran %v, want the Runnable at the head of the queue", ran)
	}

	// One left is the self-requeuing frame loop, and its pass is a frame away.
	if _, err := client.ServiceThreads(context.Background(), adoptOnly); err != nil {
		t.Fatal(err)
	}
	if len(runtime.pendingSerial) != 1 {
		t.Fatal("a second Runnable was dispatched in the same idle pass")
	}

	// And a queued Runnable is due at that pass rather than now, which is what
	// lets the Host sleep instead of spinning on it.
	deadline, parked := client.NextDeadline()
	if !parked {
		t.Fatal("NextDeadline reported nothing parked with a Runnable queued")
	}
	if wait := deadline.Sub(clock.Now()); wait <= 0 || wait > serialDispatchInterval {
		t.Fatalf("a queued Runnable is due in %v, want the next idle pass", wait)
	}

	clock.Advance(serialDispatchInterval)
	if _, err := client.ServiceThreads(context.Background(), adoptOnly); err != nil {
		t.Fatal(err)
	}
	if len(runtime.pendingSerial) != 0 {
		t.Fatal("the pass after the interval dispatched nothing")
	}
	if len(ran) != 2 || ran[1] != "second" {
		t.Fatalf("the Runnables ran as %v, want them in the order they were queued", ran)
	}

	// A backlog does not wait for the frame: a title that queues one from
	// inside its paint puts them on faster than that, and every round drains
	// one until the queue is back to the self-requeuing case.
	runtime.pendingSerial = []*jvm.Object{first, second}
	if _, err := client.ServiceThreads(context.Background(), adoptOnly); err != nil {
		t.Fatal(err)
	}
	if len(runtime.pendingSerial) != 1 {
		t.Fatalf("pendingSerial = %d, want the backlog drained by one", len(runtime.pendingSerial))
	}
	if _, err := client.ServiceThreads(context.Background(), adoptOnly); err != nil {
		t.Fatal(err)
	}
	if len(runtime.pendingSerial) != 1 {
		t.Fatal("the last Runnable went without waiting for its frame")
	}
}

// TestStartedThreadsAreDueImmediately covers the other half: a thread the guest
// started is runnable now, and a Host told otherwise sleeps through it. One
// title started a thread from its paint and then sat out the hundred seconds
// its only other thread had asked to sleep.
func TestStartedThreadsAreDueImmediately(t *testing.T) {
	client, runtime := newTestRuntime(t)
	clock := NewManualClock(time.Unix(0, 0))
	client.clock = clock
	client.workers = append(client.workers, &guestWorker{wakeAt: clock.Now().Add(100 * time.Second)})

	if deadline, parked := client.NextDeadline(); !parked || !deadline.After(clock.Now()) {
		t.Fatalf("with only a sleeping worker the deadline is %v, want the sleep", deadline)
	}
	runtime.pendingThreads = []*jvm.Object{{ClassName: "started"}}
	deadline, parked := client.NextDeadline()
	if !parked {
		t.Fatal("NextDeadline reported nothing parked with a thread waiting to start")
	}
	if deadline.After(clock.Now()) {
		t.Fatalf("a started thread is due at %v, want it due already", deadline)
	}
}

// TestAnEmptyRoundPaintsEvenWhenTheHostIsBehind pins the limit on frame
// skipping. Skipping trades the picture for the logic in the same round, so a
// round with no logic in it has nothing to trade — and for a title whose frame
// loop runs from its card's paint, the skip drops the only work there was. It
// also leaves that title's client thread perpetually due, because the thread
// sets its own next wake-up inside the paint, which the Host reads as work
// waiting and spins on.
func TestAnEmptyRoundPaintsEvenWhenTheHostIsBehind(t *testing.T) {
	client, _ := newTestRuntime(t)
	clock := NewManualClock(time.Unix(0, 0))
	client.clock = clock
	session := &Session{Client: client}

	// A saturated host with a recent paint: every condition for a skip.
	client.paintLoad = 4
	client.lastPaint = clock.Now()
	if !session.behindOnPaint() {
		t.Fatal("behindOnPaint() is false with a saturated host and a recent paint")
	}

	// There are no timers and no threads here, so the round has no logic in it.
	client.skipPaint = true
	if _, err := session.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.skipPaint {
		t.Fatal("a round with nothing else to run gave up its paint")
	}
}
