package ktf

import (
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
)

func TestKTFWIPIClipTransitionReconcilesPeerCompletionBeforeCheckpoint(t *testing.T) {
	for _, method := range []string{"stop", "pause"} {
		t.Run(method, func(t *testing.T) {
			peer := newKTFWIPIListenerFixture(t)
			other := *peer
			other.clip = newJavaAudioPauseClip(t, peer.client, peer.runtime)
			peer.setListener(peer.listeners[0])
			other.setListener(other.listeners[1])
			peer.call("play", true, false)
			duration := peer.duration()
			peer.clock.Advance(duration / 2)
			other.call("play", true, false)
			starts := []ktfWIPIListenerEvent{peer.event(0, wipi.PlayEventStart), other.event(1, wipi.PlayEventStart)}
			if count := peer.drain(); count != 2 {
				t.Fatalf("initial callbacks = %d, want 2", count)
			}
			peer.wantHistory(starts...)

			// No Host audio service runs here. Querying the newer Clip advances
			// both scores, so its native transition must also reconcile the peer.
			peer.clock.Advance(duration - duration/2)
			other.call(method, true)
			transition := int32(wipi.PlayEventStop)
			if method == "pause" {
				transition = wipi.PlayEventPause
			}
			wantQueued := []clipEvent{
				{clip: peer.clip, listener: peer.listeners[0], code: wipi.PlayEventEndOfData},
				{clip: other.clip, listener: other.listeners[1], code: transition},
			}
			if !slices.Equal(peer.runtime.mediaEvents, wantQueued) {
				t.Errorf("native transition queued %+v, want peer END followed by %s", peer.runtime.mediaEvents, method)
			}
			if state := peer.runtime.clip(peer.clip); state.completed != 1 || state.owner != nil {
				t.Errorf("completed peer retains stale progress or active owner: completed=%d owner=%p", state.completed, state.owner)
			}
			peer.wantHistory(starts...)
			end := peer.event(0, wipi.PlayEventEndOfData)
			changed := other.event(1, transition)
			saved, err := peer.session.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatalf("checkpoint after another Clip's %s: %v", method, err)
			}
			restoreKTFWIPIListenerCheckpoint(t, peer, saved)
			peer.setListener(peer.listeners[1])
			if state := peer.runtime.clip(peer.clip); state.completed != 1 || state.owner != nil {
				t.Fatal("restoration revived the completed peer's active owner")
			}
			if count := peer.drain(); count != 2 {
				t.Fatalf("restored pending callbacks = %d, want 2", count)
			}
			want := append(starts, end, changed)
			peer.wantHistory(want...)
			peer.advance(time.Second)
			if count := peer.drain(); count != 0 {
				t.Fatalf("completed peer or %s Clip emitted %d duplicate callbacks", method, count)
			}
			peer.wantHistory(want...)
		})
	}
}
