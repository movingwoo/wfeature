package ktf

import (
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

type ktfWIPIListenerWeakReferences struct {
	clip, listener               weak.Pointer[jvm.Object]
	clipAddress, listenerAddress uint32
	handle                       backend.AudioHandle
}

func clearKTFWIPIListenerHistory(fixture *ktfWIPIListenerFixture) {
	fixture.t.Helper()
	writeTestWords(fixture.t, fixture.client, testfixture.KTFListenerHistoryCount, []uint32{0})
	// The guest collector conservatively treats the recorded addresses as
	// roots, even after the callback history count has been cleared.
	writeTestWords(fixture.t, fixture.client, testfixture.KTFListenerHistory, make([]uint32, testfixture.KTFListenerHistoryCapacity*4))
}

// Return only weak references so the test's own stack cannot retain the
// listener-to-Clip cycle while it asks the collectors to reclaim it.
//
//go:noinline
func detachKTFWIPIListenerReferences(fixture *ktfWIPIListenerFixture) ktfWIPIListenerWeakReferences {
	fixture.t.Helper()
	listener, err := clipListener(fixture.clip)
	if err != nil || listener == nil {
		fixture.t.Fatalf("fixture has no listener: %v", err)
	}
	listener.SetFieldValue("clip", jvm.ReferenceValue(fixture.clip))
	clipAddress, clipBound := fixture.client.JVM().AOTObjectAddress(fixture.clip)
	listenerAddress, listenerBound := fixture.client.JVM().AOTObjectAddress(listener)
	if !clipBound || !listenerBound {
		fixture.t.Fatal("retention fixture lacks guest object bindings")
	}
	references := ktfWIPIListenerWeakReferences{
		clip: weak.Make(fixture.clip), listener: weak.Make(listener),
		clipAddress: clipAddress, listenerAddress: listenerAddress,
		handle: fixture.runtime.clip(fixture.clip).handle,
	}
	clearKTFWIPIListenerHistory(fixture)
	fixture.clip, fixture.listeners = nil, [2]*jvm.Object{}
	return references
}

//go:noinline
func restoredKTFWIPIListenerReferences(fixture *ktfWIPIListenerFixture, prior ktfWIPIListenerWeakReferences) ktfWIPIListenerWeakReferences {
	fixture.t.Helper()
	clip, ok := fixture.client.JVM().AOTObjectAt(prior.clipAddress)
	if !ok {
		fixture.t.Fatal("restoration lost the retained Clip")
	}
	listener, err := clipListener(clip)
	if err != nil || listener == nil {
		fixture.t.Fatalf("restoration lost the retained listener: %v", err)
	}
	if address, ok := fixture.client.JVM().AOTObjectAddress(listener); !ok || address != prior.listenerAddress {
		fixture.t.Fatal("restoration changed the listener's guest identity")
	}
	owner, err := listener.Fields["clip"].Reference()
	if err != nil || owner != clip {
		fixture.t.Fatalf("restoration broke the listener-to-Clip cycle: %v", err)
	}
	return ktfWIPIListenerWeakReferences{weak.Make(clip), weak.Make(listener), prior.clipAddress, prior.listenerAddress, prior.handle}
}

//go:noinline
func ktfWIPIListenerReferencesLive(references ktfWIPIListenerWeakReferences) (bool, bool) {
	return references.clip.Value() != nil, references.listener.Value() != nil
}

func requireKTFWIPIListenerRetention(fixture *ktfWIPIListenerFixture, references ktfWIPIListenerWeakReferences, retained bool) {
	fixture.t.Helper()
	defer runtime.KeepAlive(fixture.runtime)
	for attempt := 0; attempt < 32; attempt++ {
		fixture.runtime.collectAt = 0
		if _, err := fixture.runtime.collectGuestObjects(nil); err != nil {
			fixture.t.Fatal(err)
		}
		runtime.GC()
		runtime.Gosched()
		clip, listener := ktfWIPIListenerReferencesLive(references)
		if retained {
			if !clip || !listener {
				fixture.t.Fatalf("active or queued cycle was collected: Clip=%t listener=%t", clip, listener)
			}
			if attempt == 3 {
				return
			}
		} else if !clip && !listener {
			// Reclaim the now-dead guest allocations and the weak-key native
			// resources after Go has released both objects.
			if _, err := fixture.runtime.collectGuestObjects(nil); err != nil {
				fixture.t.Fatal(err)
			}
			for _, address := range []uint32{references.clipAddress, references.listenerAddress} {
				if _, exists := fixture.runtime.objects[address]; exists {
					fixture.t.Fatalf("collected object at %#x retained its guest allocation", address)
				}
			}
			if references.handle != 0 {
				if _, loaded := fixture.client.audio.Length(references.handle); loaded {
					fixture.t.Fatal("collected Clip retained its backend audio handle")
				}
			}
			return
		}
	}
	clip, listener := ktfWIPIListenerReferencesLive(references)
	fixture.t.Fatalf("idle cycle survived 32 guest and Go collections: Clip=%t listener=%t", clip, listener)
}

//go:noinline
func resumeUnreferencedKTFWIPIClip(fixture *ktfWIPIListenerFixture, references ktfWIPIListenerWeakReferences) {
	fixture.t.Helper()
	fixture.clip = references.clip.Value()
	if fixture.clip == nil {
		fixture.t.Fatal("paused Clip was collected before resume")
	}
	fixture.call("resume", true)
	fixture.clip = nil
}

func TestKTFWIPIListenerUnreferencedActiveCycleSurvivesCheckpoint(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "playing", true: "paused"}[paused], func(t *testing.T) {
			fixture := newKTFWIPIListenerFixture(t)
			fixture.setListener(fixture.listeners[0])
			fixture.call("play", true, false)
			fixture.advance(25 * time.Millisecond)
			fixture.drain()
			if paused {
				fixture.call("pause", true)
				fixture.drain()
			}
			references := detachKTFWIPIListenerReferences(fixture)
			requireKTFWIPIListenerRetention(fixture, references, true)
			saved, err := fixture.session.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			restoreKTFWIPIListenerCheckpoint(t, fixture, saved)
			restored := restoredKTFWIPIListenerReferences(fixture, references)
			if restored.clip == references.clip || restored.listener == references.listener {
				t.Fatal("restoration reused the original Host objects")
			}
			references = restored
			requireKTFWIPIListenerRetention(fixture, references, true)
			if paused {
				fixture.advance(time.Hour)
				if got := fixture.drain(); got != 0 || !fixture.client.audio.Paused(references.handle) {
					t.Fatalf("restored paused cycle progressed: callbacks=%d", got)
				}
				resumeUnreferencedKTFWIPIClip(fixture, references)
				fixture.drain()
				fixture.wantHistory(ktfWIPIListenerEvent{references.listenerAddress, references.clipAddress, wipi.PlayEventResume, 0})
				clearKTFWIPIListenerHistory(fixture)
			}
			fixture.advance(75 * time.Millisecond)
			fixture.wantHistory()
			requireKTFWIPIListenerRetention(fixture, references, true)
			if got := fixture.drain(); got != 1 {
				t.Fatalf("unreferenced Clip completion delivered %d callbacks, want 1", got)
			}
			fixture.wantHistory(ktfWIPIListenerEvent{references.listenerAddress, references.clipAddress, wipi.PlayEventEndOfData, 0})
			clearKTFWIPIListenerHistory(fixture)
			requireKTFWIPIListenerRetention(fixture, references, false)
		})
	}
}

func TestKTFWIPIListenerIdleCycleCanBeCollected(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "created", true: "restored"}[restore], func(t *testing.T) {
			fixture := newKTFWIPIListenerFixture(t)
			fixture.setListener(fixture.listeners[0])
			if restore {
				saved, err := fixture.session.CaptureCheckpoint(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				restoreKTFWIPIListenerCheckpoint(t, fixture, saved)
			}
			references := detachKTFWIPIListenerReferences(fixture)
			requireKTFWIPIListenerRetention(fixture, references, false)
			fixture.wantHistory()
			if _, err := fixture.session.CaptureCheckpoint(t.Context()); err != nil {
				t.Fatalf("collected cycle left invalid checkpoint ownership: %v", err)
			}
		})
	}
}

func TestKTFWIPIListenerPendingInactiveCycleReleasedAfterDelivery(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "stopped"}[stopped], func(t *testing.T) {
			fixture := newKTFWIPIListenerFixture(t)
			fixture.setListener(fixture.listeners[0])
			fixture.call("play", true, false)
			fixture.drain()
			code := int32(wipi.PlayEventEndOfData)
			if stopped {
				fixture.call("stop", true)
				code = wipi.PlayEventStop
			} else {
				fixture.advance(100 * time.Millisecond)
			}
			references := detachKTFWIPIListenerReferences(fixture)
			requireKTFWIPIListenerRetention(fixture, references, true)
			saved, err := fixture.session.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			restoreKTFWIPIListenerCheckpoint(t, fixture, saved)
			references = restoredKTFWIPIListenerReferences(fixture, references)
			requireKTFWIPIListenerRetention(fixture, references, true)
			if got := fixture.drain(); got != 1 {
				t.Fatalf("restored pending callback count=%d, want 1", got)
			}
			fixture.wantHistory(ktfWIPIListenerEvent{references.listenerAddress, references.clipAddress, code, 0})
			clearKTFWIPIListenerHistory(fixture)
			requireKTFWIPIListenerRetention(fixture, references, false)
		})
	}
}
