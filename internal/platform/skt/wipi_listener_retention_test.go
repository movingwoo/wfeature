package skt

import (
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type wipiListenerWeakReferences struct {
	clip, listener weak.Pointer[jvm.Object]
	handle         backend.AudioHandle
}

func trackWIPIListenerReferences(t *testing.T, clip *jvm.Object) wipiListenerWeakReferences {
	t.Helper()
	if clip == nil {
		t.Fatal("fixture has no Clip to track")
	}
	data := clip.Native.(*wipiClipData)
	if data.listener == nil || data.player == nil {
		t.Fatal("fixture has no listener or decoded Player to track")
	}
	return wipiListenerWeakReferences{
		clip: weak.Make(clip), listener: weak.Make(data.listener),
		handle: data.player.Native.(*playerData).handle,
	}
}

// Keep temporary strong references in a returned stack frame. The test itself
// retains only weak handles while forcing collection of the native cycle.
//
//go:noinline
func detachWIPIListenerReferences(fixture *wipiListenerFixture) wipiListenerWeakReferences {
	fixture.t.Helper()
	references := trackWIPIListenerReferences(fixture.t, fixture.clip())
	fixture.call("resetHistory", "()V")
	fixture.call("dropClip", "()V")
	return references
}

//go:noinline
func restoredWIPIListenerReferences(fixture *wipiListenerFixture, handle backend.AudioHandle, queued bool) wipiListenerWeakReferences {
	fixture.t.Helper()
	if queued {
		fixture.runtime.mediaMu.Lock()
		defer fixture.runtime.mediaMu.Unlock()
		if len(fixture.runtime.mediaEvents) != 1 {
			fixture.t.Fatal("restored pending Clip did not retain exactly one event")
		}
		return trackWIPIListenerReferences(fixture.t, fixture.runtime.mediaEvents[0].Data)
	}
	fixture.runtime.mediaMu.Lock()
	object := fixture.runtime.mediaPlayers[handle]
	fixture.runtime.mediaMu.Unlock()
	if object == nil {
		fixture.t.Fatal("restoration lost the active Player")
	}
	player := object.Native.(*playerData)
	player.mu.Lock()
	defer player.mu.Unlock()
	return trackWIPIListenerReferences(fixture.t, player.wipiClip)
}

// A Value result must not survive into the next GC cycle in the polling caller.
//
//go:noinline
func wipiListenerReferencesLive(references wipiListenerWeakReferences) (bool, bool) {
	return references.clip.Value() != nil, references.listener.Value() != nil
}

func requireWIPIListenerRetention(fixture *wipiListenerFixture, references wipiListenerWeakReferences, retained bool) {
	fixture.t.Helper()
	// Searching the synthetic heap must not pin an idle owner after its Java
	// references and pending callbacks disappear.
	if err := fixture.runtime.withHeap(func(heap *heapMap) error {
		heap.refresh()
		return nil
	}); err != nil {
		fixture.t.Fatal(err)
	}
	defer runtime.KeepAlive(fixture.runtime)
	for attempt := 0; attempt < 32; attempt++ {
		runtime.GC()
		runtime.Gosched()
		clip, listener := wipiListenerReferencesLive(references)
		if retained {
			if !clip || !listener {
				fixture.t.Fatalf("active or queued ownership was collected: Clip=%t listener=%t", clip, listener)
			}
			if attempt == 2 {
				return
			}
		} else if !clip && !listener {
			return
		}
	}
	clip, listener := wipiListenerReferencesLive(references)
	fixture.t.Fatalf("idle ownership remained after 32 collections: Clip=%t listener=%t", clip, listener)
}

//go:noinline
func resumeUnreferencedWIPIClip(fixture *wipiListenerFixture, references wipiListenerWeakReferences) {
	fixture.t.Helper()
	clip := references.clip.Value()
	if clip == nil {
		fixture.t.Fatal("paused Clip was collected before resume")
	}
	result, err := fixture.runtime.VM.InvokeStatic(wipi.PlayerClass, "resume", "(Lorg/kwis/msp/media/Clip;)Z", jvm.ReferenceValue(clip))
	ok, valueErr := result.Int32()
	if err != nil || valueErr != nil || ok != 1 {
		fixture.t.Fatalf("unreferenced Clip could not resume: %v, %v, %d", err, valueErr, ok)
	}
}

//go:noinline
func requireWIPIListenerEventOwner(fixture *wipiListenerFixture, references wipiListenerWeakReferences) {
	fixture.t.Helper()
	owner, err := fixture.call("eventOwner", "(I)Lorg/kwis/msp/media/Clip;", jvm.IntValue(0)).Reference()
	if err != nil || owner == nil || owner != references.clip.Value() {
		fixture.t.Fatalf("deferred event lost its retained Clip identity: %v", err)
	}
}

func TestWIPIPlayListenerUnreferencedActiveClipSurvivesGCAndCheckpoint(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "playing", true: "paused"}[paused], func(t *testing.T) {
			fixture := newWIPIListenerFixture(t)
			fixture.begin(false)
			fixture.tick(100 * time.Millisecond)
			if paused {
				fixture.boolean("pause", "()Z", true)
				fixture.drain()
			}
			references := detachWIPIListenerReferences(fixture)
			requireWIPIListenerRetention(fixture, references, true)
			fixture.wantHistory()
			saved, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			restoreWIPIListenerCheckpoint(t, fixture, saved)
			restored := restoredWIPIListenerReferences(fixture, references.handle, false)
			if restored.clip == references.clip || restored.listener == references.listener || fixture.clip() != nil {
				t.Fatal("restoration reused the prior graph or recreated the dropped Java root")
			}
			references = restored
			requireWIPIListenerRetention(fixture, references, true)
			if paused {
				fixture.tick(5 * time.Second)
				fixture.wantHistory()
				if !fixture.runtime.audio.Paused(references.handle) {
					t.Fatal("restoration did not retain the paused playback")
				}
				requireWIPIListenerRetention(fixture, references, true)
				resumeUnreferencedWIPIClip(fixture, references)
				fixture.drain()
				fixture.wantHistory(wipiListenerEvent{wipi.PlayEventResume, 1})
				fixture.call("resetHistory", "()V")
			}
			fixture.clock.advance(300 * time.Millisecond)
			fixture.runtime.AdvanceAudio()
			fixture.wantHistory()
			if fixture.runtime.audio.Playing(references.handle) {
				t.Fatal("retained Clip did not finish its remaining gate")
			}
			requireWIPIListenerRetention(fixture, references, true)
			fixture.drain()
			fixture.wantHistory(wipiListenerEvent{wipi.PlayEventEndOfData, 1})
			requireWIPIListenerEventOwner(fixture, references)
			fixture.call("resetHistory", "()V")
			requireWIPIListenerRetention(fixture, references, false)
		})
	}
}

func TestWIPIPlayListenerIdleClipCanBeCollected(t *testing.T) {
	for _, restored := range []bool{false, true} {
		t.Run(map[bool]string{false: "created", true: "restored"}[restored], func(t *testing.T) {
			fixture := newWIPIListenerFixture(t)
			fixture.call("create", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(audioStopSound())))
			if restored {
				saved, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
				if err != nil {
					t.Fatal(err)
				}
				restoreWIPIListenerCheckpoint(t, fixture, saved)
			}
			references := detachWIPIListenerReferences(fixture)
			requireWIPIListenerRetention(fixture, references, false)
			fixture.wantHistory()
			if _, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
				t.Fatalf("collected idle Clip left an invalid checkpoint graph: %v", err)
			}
		})
	}
}

func TestWIPIPlayListenerPendingInactiveClipReleasedAfterDelivery(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "stopped"}[stopped], func(t *testing.T) {
			fixture := newWIPIListenerFixture(t)
			fixture.begin(false)
			fixture.tick(0)
			if stopped {
				fixture.boolean("stop", "()Z", true)
			} else {
				fixture.clock.advance(400 * time.Millisecond)
				fixture.runtime.AdvanceAudio()
			}
			references := detachWIPIListenerReferences(fixture)
			fixture.wantHistory()
			requireWIPIListenerRetention(fixture, references, true)
			saved, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			restoreWIPIListenerCheckpoint(t, fixture, saved)
			references = restoredWIPIListenerReferences(fixture, references.handle, true)
			if fixture.clip() != nil || fixture.runtime.audio.Playing(references.handle) || fixture.runtime.audio.Paused(references.handle) {
				t.Fatal("restoration reactivated a Clip retained only for delivery")
			}
			requireWIPIListenerRetention(fixture, references, true)
			fixture.drain()
			event := int32(wipi.PlayEventEndOfData)
			if stopped {
				event = wipi.PlayEventStop
			}
			fixture.wantHistory(wipiListenerEvent{event, 1})
			requireWIPIListenerEventOwner(fixture, references)
			fixture.call("resetHistory", "()V")
			requireWIPIListenerRetention(fixture, references, false)
		})
	}
}
