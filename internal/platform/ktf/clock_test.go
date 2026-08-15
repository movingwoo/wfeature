package ktf

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// newPacedTestRuntime builds a runtime whose guest waits are measured against
// clock, which is what lets a test state a wait in milliseconds and then
// decide, itself, whether that wait has passed.
func newPacedTestRuntime(t *testing.T, clock Clock, speed float64) (*Client, *initializationRuntime) {
	t.Helper()
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	client.clock = clock
	client.SetSpeed(speed)
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	client.runtime = runtime
	return client, runtime
}

// returningStub emits a guest function that does nothing but return, so a test
// can register a timer whose callback is real code without bringing a game
// along to supply one.
func returningStub(t *testing.T, runtime *initializationRuntime) uint32 {
	t.Helper()
	address := uint32(runtime.codeCursor)
	runtime.codeCursor += 4
	if err := runtime.client.core.Memory().Load(address, []byte{0x70, 0x47}); err != nil {
		t.Fatalf("load returning stub: %v", err)
	}
	return address | 1
}

// setTimer registers a WIPI C timer the way guest code does, through the
// kernel call, so the test covers the delay the guest actually declared.
func setTimer(t *testing.T, client *Client, runtime *initializationRuntime, delay uint64) {
	t.Helper()
	record, err := runtime.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	callback := returningStub(t, runtime)
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, callback)
	if err := runtime.client.core.Memory().Write(record, word); err != nil {
		t.Fatal(err)
	}
	for register, value := range map[int]uint32{
		0: record,
		1: uint32(delay),
		2: uint32(delay >> 32),
		3: 0,
	} {
		if err := client.thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.wipicSetTimer(client.thread); err != nil {
		t.Fatalf("MC_knlSetTimer() error = %v", err)
	}
}

// TestTimerWaitsOutItsDelay is the pacing rule for the half of these games
// whose frame loop is a repeating timer: a delay is a wait, not a label, so a
// Host that ticks faster than the game asked for still gets one callback per
// delay.
func TestTimerWaitsOutItsDelay(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	setTimer(t, client, runtime, 50)

	// Ticking without letting time pass must not run the callback, however
	// many times the Host asks.
	for round := 0; round < 8; round++ {
		serviced, err := client.ServiceTimers(context.Background(), 4)
		if err != nil {
			t.Fatalf("ServiceTimers() error = %v", err)
		}
		if serviced != 0 {
			t.Fatalf("round %d serviced %d timers before the delay elapsed", round, serviced)
		}
	}

	// One millisecond short is still short.
	clock.Advance(49 * time.Millisecond)
	serviced, err := client.ServiceTimers(context.Background(), 4)
	if err != nil {
		t.Fatalf("ServiceTimers() error = %v", err)
	}
	if serviced != 0 {
		t.Fatalf("serviced %d timers at 49ms of a 50ms delay", serviced)
	}

	clock.Advance(time.Millisecond)
	serviced, err = client.ServiceTimers(context.Background(), 4)
	if err != nil {
		t.Fatalf("ServiceTimers() error = %v", err)
	}
	if serviced != 1 {
		t.Fatalf("serviced %d timers once the delay elapsed, want 1", serviced)
	}
}

// TestSpeedScalesTheWaitAndTheGuestClockTogether is what makes the multiplier
// mean "faster" rather than "out of step": the wait shrinks by the same factor
// the guest's own clock grows by, so a game that times its animation itself
// speeds up instead of compensating for the change.
func TestSpeedScalesTheWaitAndTheGuestClockTogether(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 2)
	setTimer(t, client, runtime, 50)
	before := runtime.guestMillis()

	// Half the delay in real time is the whole delay at 2x.
	clock.Advance(25 * time.Millisecond)
	serviced, err := client.ServiceTimers(context.Background(), 4)
	if err != nil {
		t.Fatalf("ServiceTimers() error = %v", err)
	}
	if serviced != 1 {
		t.Fatalf("serviced %d timers after half a 50ms delay at 2x speed, want 1", serviced)
	}
	if elapsed := runtime.guestMillis() - before; elapsed != 50 {
		t.Fatalf("guest clock advanced %dms over 25ms at 2x speed, want 50ms", elapsed)
	}
}

// TestGuestClockTracksTheSessionClock covers the case a virtual clock could
// not: a guest that polls the time without ever sleeping still sees it move,
// which is what lets a busy-wait loop finish.
func TestGuestClockTracksTheSessionClock(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	_, runtime := newPacedTestRuntime(t, clock, 1)
	before := runtime.guestMillis()
	clock.Advance(time.Second)
	if elapsed := runtime.guestMillis() - before; elapsed != 1000 {
		t.Fatalf("guest clock advanced %dms over a second, want 1000ms", elapsed)
	}
}

// TestSleepingWorkerIsNotGrantedASlice is the pacing rule for the other half
// of these games, whose frame loop is a guest thread that sleeps. The worker
// here has no goroutine behind it, so a Host that granted it a slice anyway
// would block on the handoff and hang this test rather than fail it.
func TestSleepingWorkerIsNotGrantedASlice(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, _ := newPacedTestRuntime(t, clock, 1)
	wakeAt := clock.Now().Add(50 * time.Millisecond)
	client.workers = append(client.workers, &guestWorker{
		armThread: armcore.NewThread(armcore.NewContext()),
		grant:     make(chan struct{}),
		events:    make(chan workerEvent, 1),
		wakeAt:    wakeAt,
	})

	serviced, err := client.ServiceThreads(context.Background(), 1)
	if err != nil {
		t.Fatalf("ServiceThreads() error = %v", err)
	}
	if serviced != 0 {
		t.Fatalf("serviced %d sleeping workers, want 0", serviced)
	}

	deadline, pending := client.NextDeadline()
	if !pending || !deadline.Equal(wakeAt) {
		t.Fatalf("NextDeadline() = %v, %v, want %v, true", deadline, pending, wakeAt)
	}
	if !client.SkipToNextDeadline() {
		t.Fatal("SkipToNextDeadline() did not move a manual clock to a parked wait")
	}
	if now := clock.Now(); !now.Equal(wakeAt) {
		t.Fatalf("clock after skipping = %v, want %v", now, wakeAt)
	}
	// Nothing is parked in the future any more, so there is nothing to skip
	// to; a Host that kept skipping would spin.
	if client.SkipToNextDeadline() {
		t.Fatal("SkipToNextDeadline() moved the clock past a wait that is already due")
	}
}

// TestClientThreadWaitHoldsOffPaint is the case that made a title run at
// several times its own speed: the game's whole frame loop is its card's
// paint, and it paces itself with a Thread.sleep there. There is no worker to
// park — the Host goroutine is inside the paint call — so the wait has to be
// deferred to the client thread and honoured by holding the next frame.
func TestClientThreadWaitHoldsOffPaint(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	painted := 0
	if err := client.JVM().RegisterNative("test/Card", "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		painted++
		// The guest's frame pacing, declared from inside paint.
		if _, err := runtimeThreadSleep(runtime, client.JVM(), []jvm.Value{jvm.LongValue(50)}); err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = append(runtime.displayCards, &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)})

	if _, err := client.ServicePaint(context.Background()); err != nil {
		t.Fatalf("ServicePaint() error = %v", err)
	}
	if painted != 1 {
		t.Fatalf("paints on the first service = %d, want 1", painted)
	}

	// However often the Host asks, the frame the guest paced does not repeat
	// until its wait has elapsed.
	for round := 0; round < 8; round++ {
		if _, err := client.ServicePaint(context.Background()); err != nil {
			t.Fatalf("ServicePaint() error = %v", err)
		}
	}
	if painted != 1 {
		t.Fatalf("paints while the declared wait was still running = %d, want 1", painted)
	}
	// A timer round shares the client thread, so it waits too.
	if serviced, err := client.ServiceTimers(context.Background(), 4); err != nil || serviced != 0 {
		t.Fatalf("ServiceTimers() = %d, %v while the client thread was waiting", serviced, err)
	}

	clock.Advance(50 * time.Millisecond)
	if _, err := client.ServicePaint(context.Background()); err != nil {
		t.Fatalf("ServicePaint() error = %v", err)
	}
	if painted != 2 {
		t.Fatalf("paints after the wait elapsed = %d, want 2", painted)
	}
}

// TestWallClockSessionsCannotSkip guards the line between the two kinds of
// Host: a session showing the game to a person must wait its waits out, so the
// skip a batch Host relies on has to be unavailable to it.
func TestWallClockSessionsCannotSkip(t *testing.T) {
	client, _ := newPacedTestRuntime(t, wallClock{}, 1)
	client.workers = append(client.workers, &guestWorker{
		armThread: armcore.NewThread(armcore.NewContext()),
		grant:     make(chan struct{}),
		events:    make(chan workerEvent, 1),
		wakeAt:    time.Now().Add(time.Hour),
	})
	if client.SkipToNextDeadline() {
		t.Fatal("SkipToNextDeadline() skipped a wait on the wall clock")
	}
}

func TestSpeedClampsToItsRange(t *testing.T) {
	client, _ := newPacedTestRuntime(t, NewManualClock(time.Time{}), 1)
	for _, testCase := range []struct {
		set  float64
		want float64
	}{
		{set: 0, want: 1},
		{set: -3, want: 1},
		{set: 0.01, want: speedFloor},
		{set: 1000, want: speedCeiling},
		{set: 2.5, want: 2.5},
	} {
		client.SetSpeed(testCase.set)
		if got := client.Speed(); got != testCase.want {
			t.Fatalf("SetSpeed(%v) then Speed() = %v, want %v", testCase.set, got, testCase.want)
		}
	}
}

func TestManualClockNeverRunsBackwards(t *testing.T) {
	start := time.Unix(1700000000, 0)
	clock := NewManualClock(start)
	clock.Advance(-time.Second)
	clock.Set(start.Add(-time.Hour))
	if now := clock.Now(); !now.Equal(start) {
		t.Fatalf("clock after backward moves = %v, want %v", now, start)
	}
}

// TestElapsedClientWaitIsNotADeadline is what a title whose first scene is a
// scheduled Timer task needs. The client thread's wait is recorded and never
// cleared, so once it has elapsed it answered "work is due, in the past"
// forever — which pins a manual clock, because there is nothing later to skip
// to. The task's own deadline is a second away on a clock that stopped moving,
// so the scene never starts and the screen holds whatever the first paint drew.
func TestElapsedClientWaitIsNotADeadline(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)

	client.deferClientWait(50 * time.Millisecond)
	deadline, pending := client.NextDeadline()
	if !pending || !deadline.Equal(clock.Now().Add(50*time.Millisecond)) {
		t.Fatalf("NextDeadline() while the wait runs = %v, %v", deadline, pending)
	}

	// The wait elapses, and with nothing else parked there is no deadline at
	// all: the client thread is runnable now, which a Host answers by ticking
	// rather than by waiting.
	clock.Advance(50 * time.Millisecond)
	if _, pending := client.NextDeadline(); pending {
		t.Fatal("an elapsed client wait is still reported as a deadline")
	}

	// A task scheduled a second out is now the next deadline, and a manual
	// clock can reach it.
	runtime.pendingTimers = append(runtime.pendingTimers, wipicTimer{
		task: &jvm.Object{ClassName: "test/Task"},
		due:  client.waitDeadline(time.Second),
	})
	if !client.SkipToNextDeadline() {
		t.Fatal("SkipToNextDeadline() could not reach a task the elapsed wait was hiding")
	}
	if !client.clientThreadDue() {
		t.Fatal("the client thread is not due after its wait elapsed")
	}
}
