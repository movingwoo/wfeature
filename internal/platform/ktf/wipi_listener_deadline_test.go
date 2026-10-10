package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func TestKTFWIPIListenerDeadlineBypassesLongGuestWait(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.runtime.guestEventLoop = true
	fixture.client.clientWakeAt = fixture.clock.Now().Add(time.Hour)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	if deadline, ok := fixture.session.NextDeadline(); !ok || !deadline.Equal(fixture.clock.Now()) {
		t.Fatalf("pending callback deadline = %v, %t; want now", deadline, ok)
	}
	if _, _, err := fixture.session.TickFor(t.Context(), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	start := fixture.event(0, wipi.PlayEventStart)
	fixture.wantHistory(start)
	if deadline, ok := fixture.session.NextDeadline(); !ok || deadline.Sub(fixture.clock.Now()) != 40*time.Millisecond {
		t.Fatalf("score deadline = %v, %t; want 40 ms", deadline.Sub(fixture.clock.Now()), ok)
	}
	fixture.clock.Advance(40 * time.Millisecond)
	if _, _, err := fixture.session.TickFor(t.Context(), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	fixture.wantHistory(start, fixture.event(0, wipi.PlayEventEndOfData))
	if deadline, ok := fixture.session.NextDeadline(); !ok || !deadline.Equal(fixture.client.clientWakeAt) {
		t.Fatalf("completed clip retained a due deadline: %v, %t", deadline, ok)
	}
}

func TestKTFWIPIListenerDeadlineTracksPauseAndRate(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.client.clientWakeAt = fixture.clock.Now().Add(time.Hour)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, true)
	fixture.advance(25 * time.Millisecond)
	fixture.call("pause", true)
	fixture.drain()
	if deadline, ok := fixture.session.NextDeadline(); !ok || !deadline.Equal(fixture.client.clientWakeAt) {
		t.Fatalf("paused score retained a deadline: %v, %t", deadline, ok)
	}
	fixture.clock.Advance(time.Second)
	fixture.client.SetSpeed(2)
	fixture.call("resume", true)
	fixture.drain()
	if deadline, ok := fixture.session.NextDeadline(); !ok || deadline.Sub(fixture.clock.Now()) != 7500*time.Microsecond {
		t.Fatalf("resumed 2x deadline = %v, %t; want 7.5 ms", deadline.Sub(fixture.clock.Now()), ok)
	}
	fixture.clock.Advance(7500 * time.Microsecond)
	if _, _, err := fixture.session.TickFor(t.Context(), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	history := fixture.history()
	if len(history) != 4 || history[3].event != wipi.PlayEventEndOfData {
		t.Fatalf("resumed pass did not complete once: %v", history)
	}
	if deadline, ok := fixture.session.NextDeadline(); !ok || deadline.Sub(fixture.clock.Now()) != 20*time.Millisecond {
		t.Fatalf("next repeated pass deadline = %v, %t", deadline.Sub(fixture.clock.Now()), ok)
	}
}

func TestKTFWIPIListenerZeroLengthCompletionHasDeadline(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.client.clientWakeAt = fixture.clock.Now().Add(time.Hour)
	handle, err := fixture.client.audio.LoadEvents([]smaf.Event{{Type: smaf.EventEnd}})
	if err != nil {
		t.Fatal(err)
	}
	state := fixture.runtime.clip(fixture.clip)
	state.handle, state.loaded = handle, true
	fixture.call("play", true, false)
	// Installing after Play leaves no START queued, so only the zero-length
	// score's completion can wake the Host before its unrelated long wait.
	fixture.setListener(fixture.listeners[0])
	if deadline, ok := fixture.session.NextDeadline(); !ok || !deadline.Equal(fixture.clock.Now()) {
		t.Fatalf("zero-length completion deadline = %v, %t", deadline, ok)
	}
	if _, _, err := fixture.session.TickFor(t.Context(), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	fixture.wantHistory(fixture.event(0, wipi.PlayEventEndOfData))
}
