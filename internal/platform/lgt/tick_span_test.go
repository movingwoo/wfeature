package lgt

import (
	"context"
	"testing"
	"time"
)

// A title's frame loop here is a timer that re-arms itself at the end of every
// frame, so the interval it asks for is the rate it wants. A tick that always
// stands for the same span rounds that interval up to a multiple of itself,
// and the local titles ask for 1ms, 10ms, 20ms, 46ms, 60ms and 83ms — every
// one of which lands on 50 or 100 under a fixed 50ms tick. The tick now stands
// for the wait until the guest's next scheduled work instead.
func TestTickSpanFollowsTheGuestsNextTimer(t *testing.T) {
	client := fixtureClient(t)
	session := &Session{client: client, tick: 50 * time.Millisecond}

	structure, err := client.allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	callSlot(t, client, slotDefTimer, structure, 0x4771)

	// Nothing armed: a tick stands for the session tick, as it always did.
	if span := session.tickSpan(); span != 50*time.Millisecond {
		t.Fatalf("tickSpan() with no timer = %v, want the session tick", span)
	}

	// 46ms is what one title's frame loop asks for, and it is what the tick
	// now covers rather than rounding up to the next 50.
	callSlot(t, client, slotSetTimer, structure, 46, 0, 0)
	if span := session.tickSpan(); span != 46*time.Millisecond {
		t.Fatalf("tickSpan() with a 46ms timer = %v, want 46ms", span)
	}

	// A timer further out than the tick does not stretch it: the Host still
	// has to take frames and deliver keys while the guest waits.
	callSlot(t, client, slotSetTimer, structure, 279, 0, 0)
	if span := session.tickSpan(); span != 50*time.Millisecond {
		t.Fatalf("tickSpan() with a 279ms timer = %v, want the session tick", span)
	}

	// One already due carries no time of its own, so the callback and the
	// frame it draws land in the same instant rather than a tick apart.
	client.timers[structure].dueAt = 0
	if span := session.tickSpan(); span != 0 {
		t.Fatalf("tickSpan() with an overdue timer = %v, want 0", span)
	}
}

// A one millisecond timer is not a request for a thousand frames a second: it
// says "as soon as you can", and the specification says a timer is only
// accurate to the resolution of the OS underneath. Without a floor a title
// idling behind such a heartbeat would have the Host turning hundreds of
// rounds a second for frames nothing shows.
func TestATimerShorterThanTheDisplayIsGivenTheDisplaysPeriod(t *testing.T) {
	client := fixtureClient(t)
	structure, err := client.allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	callSlot(t, client, slotDefTimer, structure, 0x4771)

	before := time.Duration(client.clock.millis()) * time.Millisecond
	callSlot(t, client, slotSetTimer, structure, 1, 0, 0)
	if wait := client.timers[structure].dueAt - before; wait < minTimerPeriod {
		t.Fatalf("a 1ms timer came due in %v, want no sooner than %v", wait, minTimerPeriod)
	}

	// The floor is on the period, not on the tick: a longer request is
	// unaffected, which is what keeps a 46ms frame loop at 46 rather than
	// rounding it up the way the fixed tick did.
	before = time.Duration(client.clock.millis()) * time.Millisecond
	callSlot(t, client, slotSetTimer, structure, 46, 0, 0)
	if wait := client.timers[structure].dueAt - before; wait != 46*time.Millisecond {
		t.Fatalf("a 46ms timer came due in %v, want 46ms", wait)
	}
}

// TickFor waits for the span the tick stood for rather than a fixed one. That
// is what carries the fix out to a Host: the guest clock and the wall clock
// still advance together, but a frame now costs what the game asked for.
func TestTickForWaitsTheSpanTheTickStoodFor(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20, Tick: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	structure, err := session.client.allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	// The callback is the fixture's own event entry, so a timer that comes due
	// during the tick runs guest code that returns rather than faulting.
	callSlot(t, session.client, slotDefTimer, structure, session.client.clet.HandleEvent)

	// With nothing armed the wait is the session tick, as before.
	wait, err := session.TickFor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if wait <= 30*time.Millisecond || wait > 50*time.Millisecond {
		t.Fatalf("TickFor() wait with no timer = %v, want most of the 50ms tick", wait)
	}

	// A frame loop asking for 20ms is waited for 20, not 50.
	session.nextDue = time.Now()
	callSlot(t, session.client, slotSetTimer, structure, 20, 0, 0)
	wait, err = session.TickFor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if wait > 20*time.Millisecond {
		t.Fatalf("TickFor() wait with a 20ms timer = %v, want no more than 20ms", wait)
	}
}

// A tick reports the guest time it **actually** stood for, not the span it set
// out to stand for. `TickFor` charges the wall clock with that number, so a
// tick whose work overran its span has to say so; reporting the span instead
// hands the computation back for free and the guest clock outruns the wall.
//
// The test above and `TestTickForPacesGuestTimeAgainstTheWallClock` both passed
// while that was wrong, because **the fixture module does no work to overrun
// with** — its every slot returns at once. On a local title in a scene whose
// frames cost more than the span between them, the guest ran **2.27x** the wall
// and painted a frame every 44ms of real time that carried 100ms of its own.
// So the work is supplied here rather than waited for: the clock's step source
// is the one thing a fixture cannot make expensive.
func TestATickReportsTheGuestTimeItActuallyStoodFor(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20, Tick: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)

	// The clock counts instructions through this, so the tick can be made to
	// overrun by exactly as much as the case under test needs.
	retired := uint64(0)
	session.client.clock.mu.Lock()
	session.client.clock.steps = func() uint64 { return retired }
	session.client.clock.baseline = 0
	session.client.clock.mu.Unlock()
	// The committed clock, not `now`: a reading taken inside a tick already
	// carries the work the tick has not yet charged for, which is what keeps
	// time from going backwards at the boundary. What a tick *stood for* is
	// what it added.
	committed := func() time.Duration {
		session.client.clock.mu.Lock()
		defer session.client.clock.mu.Unlock()
		return session.client.clock.elapsed
	}

	// A tick the guest was idle for stands for its span, as before.
	before := committed()
	span, err := session.tickOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if moved := committed() - before; span != moved {
		t.Fatalf("an idle tick reported %v and moved the clock %v", span, moved)
	}

	// A tick whose work overran its span stands for the work. This is the one
	// the wall clock was not being charged for.
	retired += 500 * guestInstructionsPerMillisecond
	before = committed()
	span, err = session.tickOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	moved := committed() - before
	if span != moved {
		t.Fatalf("an overrunning tick reported %v and moved the clock %v", span, moved)
	}
	if span < 500*time.Millisecond {
		t.Fatalf("an overrunning tick stood for %v, want at least the 500ms of work", span)
	}
}

// The same defect from the end a player is at: guest time and wall time have to
// advance together over a run of overrunning ticks, which is what
// `TestTickForPacesGuestTimeAgainstTheWallClock` asks of a run of cheap ones.
func TestTickForPacesGuestTimeWhenTheWorkOverrunsTheSpan(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20, Tick: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)

	retired := uint64(0)
	session.client.clock.mu.Lock()
	session.client.clock.steps = func() uint64 { return retired }
	session.client.clock.baseline = 0
	session.client.clock.mu.Unlock()

	before := session.client.clock.now()
	started := time.Now()
	for range 10 {
		// Every tick costs half again what the session tick stands for, which
		// is the shape of a frame that takes longer than the interval its title
		// asked to be called back on.
		retired += 30 * guestInstructionsPerMillisecond
		wait, err := session.TickFor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(wait)
	}
	guest := session.client.clock.now() - before
	wall := time.Since(started)
	// Loose in the one direction a loaded test machine can move it: it can only
	// ever be slower than the pace, never faster.
	if speed := guest.Seconds() / wall.Seconds(); speed > 1.2 {
		t.Fatalf("guest ran at %.2fx wall clock (guest=%v wall=%v), want about 1x", speed, guest, wall)
	}
}
