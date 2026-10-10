package lgt

import (
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
)

func TestLGTWIPIClipTransitionReconcilesPeerCompletionBeforeCheckpoint(t *testing.T) {
	for _, method := range []string{"stop", "pause"} {
		t.Run(method, func(t *testing.T) {
			peer := newLGTWIPIListenerFixture(t)
			other := *peer
			other.clip = peer.newClip()
			peer.setListener(peer.listeners[0])
			other.setListener(other.listeners[1])
			peer.call("play", true, false)
			duration := peer.duration()
			peer.client.clock.advance(duration / 2)
			other.call("play", true, false)
			starts := []lgtWIPIListenerEvent{peer.event(0, wipi.PlayEventStart), other.event(1, wipi.PlayEventStart)}
			if count := peer.drain(); count != 2 {
				t.Fatalf("initial callbacks = %d, want 2", count)
			}
			peer.wantHistory(starts...)

			// Enter the real Java ABI without a Host audio service at the first
			// Clip's deadline. The second Clip still has half its score to play.
			peer.client.clock.advance(duration - duration/2)
			other.call(method, true)
			transition := int32(wipi.PlayEventStop)
			if method == "pause" {
				transition = wipi.PlayEventPause
			}
			wantQueued := []javaMediaEvent{
				{clip: peer.clip, listener: peer.listeners[0], code: wipi.PlayEventEndOfData},
				{clip: other.clip, listener: other.listeners[1], code: transition},
			}
			if !slices.Equal(peer.client.javaMediaEvents, wantQueued) {
				t.Errorf("native transition queued %+v, want peer END followed by %s", peer.client.javaMediaEvents, method)
			}
			if clip := peer.client.clips[peer.clip]; clip.completed != 1 || peer.client.audio.Playing(clip.handle) || peer.client.audio.Paused(clip.handle) {
				t.Errorf("completed peer retains stale progress or playback: completed=%d", clip.completed)
			}
			peer.wantHistory(starts...)
			end := peer.event(0, wipi.PlayEventEndOfData)
			changed := other.event(1, transition)
			restored := peer.restore(peer.checkpoint())
			for _, current := range []*lgtWIPIListenerFixture{peer, restored} {
				current.setListener(current.listeners[1])
				if count := current.drain(); count != 2 {
					t.Fatalf("pending callbacks after checkpoint = %d, want 2", count)
				}
				want := append(slices.Clone(starts), end, changed)
				current.wantHistory(want...)
				current.client.releaseJavaPins(0)
				roots := make(map[uint32]bool)
				current.client.markJavaPlatformRoots(func(object uint32) { roots[object] = true })
				if roots[current.clip] || roots[other.clip] != (method == "pause") {
					t.Fatalf("media roots after %s and delivery: peer=%t other=%t", method, roots[current.clip], roots[other.clip])
				}
				current.advance(time.Second)
				if count := current.drain(); count != 0 {
					t.Fatalf("completed peer or %s Clip emitted %d duplicate callbacks", method, count)
				}
				current.wantHistory(want...)
			}
		})
	}
}

func TestLGTWIPICResumeReconcilesJavaPeerBeforeCheckpoint(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	client := fixture.client
	sound := oneNoteSound(t)
	clip := callSlot(t, client, slotClipCreate, 0, uint32(len(sound)), 0)
	callSlot(t, client, slotClipPutData, clip, writeGuest(t, client, sound), uint32(len(sound)))
	if got := int32(callSlot(t, client, slotClipPlay, clip, 0)); got != wipiSuccess {
		t.Fatalf("C play = %d", got)
	}
	if got := int32(callSlot(t, client, slotClipPause, clip)); got != wipiSuccess {
		t.Fatalf("C pause = %d", got)
	}
	fixture.drain()
	client.clock.advance(fixture.duration() + time.Millisecond)
	if got := int32(callSlot(t, client, slotClipResume, clip)); got != wipiSuccess {
		t.Fatalf("C resume = %d", got)
	}
	javaClip := client.clips[fixture.clip]
	progress, err := client.audio.PlaybackState(javaClip.handle)
	if err != nil || progress.Completed != 1 || javaClip.completed != 1 {
		t.Errorf("C resume left stale Java progress: backend=%+v observed=%d err=%v", progress, javaClip.completed, err)
	}
	want := []javaMediaEvent{{clip: fixture.clip, listener: fixture.listeners[0], code: wipi.PlayEventEndOfData}}
	if !slices.Equal(client.javaMediaEvents, want) {
		t.Errorf("C resume queued %+v, want Java peer END", client.javaMediaEvents)
	}
	restored := fixture.restore(fixture.checkpoint())
	restored.drain()
	restored.wantHistory(restored.event(0, wipi.PlayEventStart), restored.event(0, wipi.PlayEventEndOfData))
	restored.advance(time.Second)
	if count := restored.drain(); count != 0 {
		t.Fatalf("restored peer generated %d duplicate callbacks", count)
	}
}
