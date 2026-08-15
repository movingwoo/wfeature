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
