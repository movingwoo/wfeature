package ktf

import (
	"math"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type ktfAudioTimingProbe struct {
	audioPauseProbe
	at, stopAt float64
	noteAt     []float64
}

func (sink *ktfAudioTimingProbe) AudioTime(at float64) { sink.at = at }

func (sink *ktfAudioTimingProbe) AudioEvent(sound backend.AudioHandle, event smaf.Event) {
	sink.audioPauseProbe.AudioEvent(sound, event)
	if event.Type == smaf.EventNoteOn {
		sink.noteAt = append(sink.noteAt, sink.at)
	}
}

func (sink *ktfAudioTimingProbe) StopSound(sound backend.AudioHandle) {
	sink.audioPauseProbe.StopSound(sound)
	sink.stopAt = sink.at
}

func makeTimingOrphanPCM(t *testing.T, rt *initializationRuntime) {
	t.Helper()
	object := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}
	state := rt.clip(object)
	handle, err := rt.client.audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 10, Wave: make([]int16, 100)},
		{Time: 100, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	state.handle, state.loaded = handle, true
	if err := rt.client.audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
}

func TestKTFAudioTimingCollectorCancelsAtCollectionTime(t *testing.T) {
	client, rt := newCollectorRuntime(t)
	allocateTestArray(t, rt, 1)
	clock := NewManualClock(time.Unix(1000, 0))
	client.clock, rt.clockBase = clock, clock.Now()
	sink := &ktfAudioTimingProbe{}
	client.audio = backend.NewAudioWithClock(sink, clock.Now)
	makeTimingOrphanPCM(t, rt)
	clock.Advance(100 * time.Millisecond)
	client.audio.Advance(100 * time.Millisecond)
	runtime.GC()
	// The score has ended, but its PCM tail remains. Collection can also run
	// during a guest allocation, without the ordinary Host audio service.
	clock.Advance(200 * time.Millisecond)
	stops := len(sink.stopped)
	if _, err := rt.collectGuestObjects(nil); err != nil {
		t.Fatal(err)
	}
	if len(rt.clips) != 0 || len(sink.stopped) != stops+1 {
		t.Fatal("collection did not release the orphan's retained PCM owner")
	}
	if math.Abs(sink.stopAt-0.3) > 1e-12 {
		t.Fatalf("collector stop time = %g; want 0.3", sink.stopAt)
	}
}

func TestKTFAudioTimingSpeedBeforeRuntimeReachesOutput(t *testing.T) {
	clock := NewManualClock(time.Unix(1000, 0))
	sink := &ktfAudioTimingProbe{}
	audio := backend.NewAudioWithClock(sink, clock.Now)
	client := &Client{clock: clock, audio: audio}
	client.SetSpeed(2)
	handle, err := audio.LoadEvents([]smaf.Event{
		{Time: 200, Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 1000, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(200 * time.Millisecond)
	if client.Speed() != 2 || len(sink.noteAt) != 1 || math.Abs(sink.noteAt[0]-0.1) > 1e-12 {
		t.Fatalf("pre-runtime speed = %g; note times = %v; want 2 and [0.1]", client.Speed(), sink.noteAt)
	}
}

func TestKTFAudioTimingSpeedChangeReconcilesCompletionBeforeCheckpoint(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	duration := fixture.duration()
	start, end := fixture.event(0, wipi.PlayEventStart), fixture.event(0, wipi.PlayEventEndOfData)
	if count := fixture.drain(); count != 1 {
		t.Fatalf("initial callbacks = %d; want 1", count)
	}
	fixture.clock.Advance(duration)
	elapsed := fixture.runtime.guestElapsed()
	fixture.client.SetSpeed(2)
	if fixture.client.Speed() != 2 || fixture.runtime.guestElapsed() != elapsed {
		t.Fatal("speed change moved the guest clock or failed to select the rate")
	}
	wantQueued := []clipEvent{{clip: fixture.clip, listener: fixture.listeners[0], code: wipi.PlayEventEndOfData}}
	if !slices.Equal(fixture.runtime.mediaEvents, wantQueued) {
		t.Fatalf("speed change queued %+v; want the original recipient's END", fixture.runtime.mediaEvents)
	}
	if state := fixture.runtime.clip(fixture.clip); state.completed != 1 || state.owner != nil {
		t.Fatalf("completed clip retained stale accounting/root: completed=%d owner=%p", state.completed, state.owner)
	}
	fixture.wantHistory(start)
	saved, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatalf("checkpoint after speed change: %v", err)
	}
	restoreKTFWIPIListenerCheckpoint(t, fixture, saved)
	if fixture.client.Speed() != 2 {
		t.Fatal("restoration lost the changed playback rate")
	}
	fixture.setListener(fixture.listeners[1])
	if state := fixture.runtime.clip(fixture.clip); state.completed != 1 || state.owner != nil {
		t.Fatal("restoration revived the completed clip's active root")
	}
	if count := fixture.drain(); count != 1 {
		t.Fatalf("restored pending callbacks = %d; want 1", count)
	}
	fixture.wantHistory(start, end)
	fixture.advance(time.Second)
	if count := fixture.drain(); count != 0 {
		t.Fatalf("completed clip emitted %d duplicate callbacks", count)
	}
	fixture.wantHistory(start, end)
}
