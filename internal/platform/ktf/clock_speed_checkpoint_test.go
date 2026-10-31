package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

func TestSpeedChangeCheckpointPreservesClockAndPausedAudio(t *testing.T) {
	for _, speed := range []float64{0.5, 2} {
		t.Run(map[float64]string{0.5: "slow then fast", 2: "fast then slow"}[speed], func(t *testing.T) {
			options := continuationFixtureOptions{}
			source := newContinuationFixture(t, 1700000000, options)
			source.clock.Advance(time.Second)
			source.client.SetSpeed(speed)
			source.client.Audio = backend.NewAudioWithClock(nil, source.clock.Now)
			if err := source.client.Audio.SetPlaybackRate(0, speed); err != nil {
				t.Fatal(err)
			}
			handle, err := source.client.Audio.LoadEvents([]smaf.Event{
				{Type: smaf.EventNoteOn, Note: 60, Velocity: 80},
				{Time: 200, Type: smaf.EventNoteOff, Note: 60},
				{Time: 300, Type: smaf.EventEnd},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := source.client.Audio.PlayCount(handle, source.runtime.GuestElapsed(), 2); err != nil {
				t.Fatal(err)
			}
			source.client.ServiceAudio()
			hostAge := time.Duration(float64(100*time.Millisecond) / speed)
			source.clock.Advance(hostAge)
			source.client.ServiceAudio()
			if err := source.client.Audio.Pause(handle, source.runtime.GuestElapsed()); err != nil {
				t.Fatal(err)
			}
			// Change rate while the score is paused, then capture after a long
			// Host absence. Only the clock moves; the retained gate does not.
			finalSpeed := 1 / speed
			source.client.SetSpeed(finalSpeed)
			source.clock.Advance(time.Hour)
			elapsed, date := source.runtime.GuestElapsed(), source.runtime.GuestMillis()
			saved := roundTripClientState(t, captureClientForTest(t, source.client))
			if saved.Speed != finalSpeed || time.Duration(float64(saved.Heap.Control.ClockAge)*saved.Speed) != elapsed {
				t.Fatal("existing ClockAge/speed representation lost the rebased guest time")
			}
			fresh := newContinuationRestoreFixture(t, 1900000000, options)
			fresh.client.Vibrator.SetClock(fresh.clock.Now)
			sink := &audioPauseProbe{}
			var activation clientActivation
			if err := fresh.client.restoreClientStateForActivation(saved, armcore.CoreOptions{}, sink, &activation); err != nil {
				t.Fatal(err)
			}
			// Detached construction and adoption may happen on another Host
			// epoch. Activation must exclude that wait after a rate change too.
			fresh.clock.Advance(3 * time.Hour)
			activation.activate()
			if fresh.runtime.GuestElapsed() != elapsed || fresh.runtime.GuestMillis() != date || fresh.client.Speed() != finalSpeed {
				t.Fatal("fresh-epoch activation changed the saved guest clock or rate")
			}
			if !fresh.client.Audio.Paused(handle) {
				t.Fatal("restoration resumed a paused clip")
			}
			if err := fresh.client.Audio.Resume(handle, fresh.runtime.GuestElapsed()); err != nil {
				t.Fatal(err)
			}
			if len(sink.resumed) != 1 || sink.resumed[0].age != hostAge {
				t.Fatalf("restored output lost its unscaled age: %+v", sink.resumed)
			}
			sink.events = nil
			advance := func(guest time.Duration) {
				fresh.clock.Advance(time.Duration(float64(guest) / finalSpeed))
				fresh.client.ServiceAudio()
			}
			advance(99 * time.Millisecond)
			if len(sink.ofType(smaf.EventNoteOff)) != 0 {
				t.Fatal("restored gate ended early")
			}
			advance(time.Millisecond)
			if len(sink.ofType(smaf.EventNoteOff)) != 1 {
				t.Fatal("restored gate did not end at the saved new rate")
			}
			advance(400 * time.Millisecond)
			progress, err := fresh.client.Audio.Playback(handle, fresh.runtime.GuestElapsed())
			if err != nil || progress.Playing || progress.Completed != 2 || len(sink.ofType(smaf.EventNoteOn)) != 1 {
				t.Fatalf("restore changed the remaining finite pass: %+v, %v", progress, err)
			}
			if source.runtime.GuestElapsed() != elapsed || !source.client.Audio.Paused(handle) {
				t.Fatal("restored playback changed the source")
			}
		})
	}
}
