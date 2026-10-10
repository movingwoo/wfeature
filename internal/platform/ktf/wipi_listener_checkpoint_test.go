package ktf

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func restoreKTFWIPIListenerCheckpoint(t *testing.T, fixture *ktfWIPIListenerFixture, saved backend.Checkpoint) {
	t.Helper()
	var addresses [3]uint32
	for index, object := range []*jvm.Object{fixture.clip, fixture.listeners[0], fixture.listeners[1]} {
		if object != nil {
			var ok bool
			addresses[index], ok = fixture.client.JVM().AOTObjectAddress(object)
			if !ok {
				t.Fatal("fixture object has no AOT binding before restoration")
			}
		}
	}
	options := fixture.session.options
	clock := NewManualClock(time.Unix(1900000000, 0))
	sink := &audioPauseProbe{}
	options.Clock, options.AudioSink = clock, sink
	prepared, err := PrepareSessionCheckpoint(fixture.archive, saved, options)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if count, first := prepared.PreparationStoreCalls(); count != 0 {
		t.Fatalf("detached validation made %d save calls, starting with %s", count, first)
	}
	// Time spent inspecting a detached replacement must not consume a note or
	// turn a pending notification into a new playback transition.
	clock.Advance(4 * time.Hour)
	restored, err := prepared.Commit(t.Context(), fixture.session, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	fixture.session, fixture.client, fixture.runtime = restored, restored.Client, restored.Client.runtime
	fixture.clock, fixture.sink = clock, sink
	for index, destination := range []**jvm.Object{&fixture.clip, &fixture.listeners[0], &fixture.listeners[1]} {
		*destination = nil
		if addresses[index] != 0 {
			var ok bool
			*destination, ok = fixture.client.JVM().AOTObjectAt(addresses[index])
			if !ok {
				t.Fatalf("restoration lost fixture object at %#x", addresses[index])
			}
		}
	}
	if count := binary.LittleEndian.Uint32(readTestBytes(t, fixture.client, testfixture.KTFListenerStartupCounter, 4)); count != 1 {
		t.Fatalf("checkpoint restoration reran startup: count=%d", count)
	}
	fixture.session.ResumeCheckpointOutput()
}

func TestKTFWIPIListenerCheckpointRetainsRecipientsAndPausedGate(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	start := fixture.event(0, wipi.PlayEventStart)
	// The authored score starts its note at 20 ms and ends at 40 ms.
	fixture.advance(20 * time.Millisecond)
	fixture.advance(5 * time.Millisecond)
	fixture.setListener(fixture.listeners[1])
	fixture.call("pause", true)
	pause := fixture.event(1, wipi.PlayEventPause)
	fixture.setListener(nil)
	fixture.wantHistory()
	handle := fixture.runtime.clip(fixture.clip).handle
	elapsed := fixture.runtime.guestElapsed()
	saved, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	restoreKTFWIPIListenerCheckpoint(t, fixture, saved)
	listener, err := clipListener(fixture.clip)
	if err != nil || listener != nil || !fixture.client.audio.Paused(handle) || fixture.runtime.guestElapsed() != elapsed {
		t.Fatalf("paused checkpoint changed listener, owner, or guest time: listener=%p elapsed=%v err=%v", listener, fixture.runtime.guestElapsed(), err)
	}
	if len(fixture.sink.resumed) != 0 || len(fixture.sink.ofType(smaf.EventNoteOn)) != 0 || len(fixture.sink.ofType(smaf.EventWave)) != 0 {
		t.Fatal("restoration replayed paused audio")
	}
	fixture.advance(10 * time.Second)
	if got := fixture.drain(); got != 2 {
		t.Fatalf("restored pending snapshot delivered %d callbacks, want 2", got)
	}
	fixture.wantHistory(start, pause)
	fixture.call("resume", true)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("cleared listener received %d resume callbacks", got)
	}
	if len(fixture.sink.resumed) != 1 || fixture.sink.resumed[0].age != 5*time.Millisecond {
		t.Fatalf("restored held note resumed at the wrong age: %+v", fixture.sink.resumed)
	}
	fixture.setListener(fixture.listeners[1])
	fixture.advance(14 * time.Millisecond)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("paused interval consumed the remaining gate: %d callbacks", got)
	}
	fixture.advance(time.Millisecond)
	if got := fixture.drain(); got != 1 {
		t.Fatalf("natural completion delivered %d callbacks, want 1", got)
	}
	end := fixture.event(1, wipi.PlayEventEndOfData)
	fixture.wantHistory(start, pause, end)
	completed, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	restoreKTFWIPIListenerCheckpoint(t, fixture, completed)
	fixture.advance(time.Second)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("restoring a delivered completion duplicated %d callbacks", got)
	}
	fixture.wantHistory(start, pause, end)
}

func TestKTFWIPIListenerCheckpointRetainsPendingNaturalCompletion(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	fixture.drain()
	start := fixture.event(0, wipi.PlayEventStart)
	fixture.advance(100 * time.Millisecond)
	end := fixture.event(0, wipi.PlayEventEndOfData)
	fixture.setListener(fixture.listeners[1])
	fixture.wantHistory(start)
	saved, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	restoreKTFWIPIListenerCheckpoint(t, fixture, saved)
	fixture.advance(time.Second)
	if got := fixture.drain(); got != 1 {
		t.Fatalf("restored completed Clip delivered %d callbacks, want 1", got)
	}
	fixture.wantHistory(start, end)
	fixture.advance(time.Second)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("completed Clip repeated %d callbacks", got)
	}
}

func TestKTFWIPIListenerLegacyCheckpointStartsAtSavedCompletion(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	// Preserve the original runtime's empty interface record. Restoring a
	// checkpoint must not require adding slots to an already allocated class.
	address, err := fixture.runtime.createRuntimeJavaClass(runtimeInterfaceClass(runtimePlayListenerClass))
	if err != nil {
		t.Fatal(err)
	}
	if method, found, err := fixture.client.JVM().FindAOTMethod(address, "playUpdate", clipUpdateSignature); err != nil || found {
		t.Fatalf("legacy fixture already declares playUpdate: %+v, %v", method, err)
	}
	fixture.call("play", true, true)
	duration := fixture.duration()
	fixture.advance(3 * duration)
	fixture.wantHistory()
	checkpoint, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var saved sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		t.Fatal(err)
	}
	heap := &saved.Client.Heap
	var payloadID uint32
	for index, binding := range heap.Roots.Objects {
		if string(binding.Name) == heapMediaRoot {
			id := heap.JVM.Roots[binding.Root-1]
			payloadID = heap.JVM.Objects[id-1].Native
			heap.JVM.Objects[id-1].Native = 0
			heap.JVM.Roots[binding.Root-1] = 0
			heap.Roots.Objects = append(heap.Roots.Objects[:index], heap.Roots.Objects[index+1:]...)
			break
		}
	}
	if payloadID == 0 {
		t.Fatal("fixture did not capture a media carrier")
	}
	heap.JVM.Payloads = append(heap.JVM.Payloads[:payloadID-1], heap.JVM.Payloads[payloadID:]...)
	for index := range heap.JVM.Objects {
		if heap.JVM.Objects[index].Native > payloadID {
			heap.JVM.Objects[index].Native--
		}
	}
	checkpoint.Runtime, err = backend.EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	restoreKTFWIPIListenerCheckpoint(t, fixture, checkpoint)
	if method, found, err := fixture.client.JVM().FindAOTMethod(address, "playUpdate", clipUpdateSignature); err != nil || found {
		t.Fatalf("restoration rewrote the legacy interface: %+v, %v", method, err)
	}
	fixture.setListener(fixture.listeners[0])
	if got := fixture.drain(); got != 0 {
		t.Fatalf("legacy restoration invented %d historical callbacks", got)
	}
	fixture.advance(duration - time.Millisecond)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("legacy restoration emitted %d premature completions", got)
	}
	fixture.advance(time.Millisecond)
	if got := fixture.drain(); got != 1 {
		t.Fatalf("legacy Clip delivered %d new completions, want 1", got)
	}
	end := fixture.event(0, wipi.PlayEventEndOfData)
	fixture.wantHistory(end)
	upgraded, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	restoreKTFWIPIListenerCheckpoint(t, fixture, upgraded)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("recaptured legacy Clip repeated %d delivered completions", got)
	}
	fixture.advance(duration)
	fixture.drain()
	fixture.wantHistory(end, end)
}

func newKTFWIPIBaseClip(t *testing.T, fixture *ktfWIPIListenerFixture) *jvm.Object {
	t.Helper()
	class, err := fixture.runtime.ensureJavaClass("org/kwis/msp/media/BaseClip")
	if err != nil {
		t.Fatal(err)
	}
	_, object, err := fixture.runtime.allocateAOTInstance(class)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runtime.clip(object)
	return object
}

func TestKTFWIPIListenerCheckpointPreservesBaseClipPlayback(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.clip = newKTFWIPIBaseClip(t, fixture)
	data := oneNoteSMAF()
	result, err := fixture.client.JVM().InvokeVirtual(fixture.clip, "setBuffer", "([BI)Z",
		jvm.ReferenceValue(newByteArray(t, fixture.client, data)), jvm.IntValue(int32(len(data))))
	if ok, valueErr := result.Int32(); err != nil || valueErr != nil || ok != 1 {
		t.Fatalf("BaseClip.setBuffer = %v, %v", result, err)
	}
	javaAudioPauseCall(t, fixture.client, fixture.clip, "BaseClip", "play", 1, 0)
	fixture.advance(20 * time.Millisecond)
	fixture.advance(5 * time.Millisecond)
	javaAudioPauseCall(t, fixture.client, fixture.clip, "BaseClip", "pause", 1)
	saved, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	restoreKTFWIPIListenerCheckpoint(t, fixture, saved)
	javaAudioPauseCall(t, fixture.client, fixture.clip, "BaseClip", "resume", 1)
	if len(fixture.sink.resumed) != 1 || fixture.sink.resumed[0].age != 5*time.Millisecond {
		t.Fatalf("restored BaseClip lost its note position: %+v", fixture.sink.resumed)
	}
	fixture.advance(15 * time.Millisecond)
	if fixture.client.audio.Playing(fixture.runtime.clip(fixture.clip).handle) || fixture.drain() != 0 {
		t.Fatal("BaseClip completion changed playback or invented a listener callback")
	}
}

func TestKTFWIPIListenerCheckpointRejectsBaseClipCallbackArgument(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	base := newKTFWIPIBaseClip(t, fixture)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	checkpoint, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var saved sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		t.Fatal(err)
	}
	payload, _ := ktfListenerMediaRecord(t, &saved)
	for index, object := range saved.Client.Heap.JVM.Objects {
		if object.Class == base.ClassName {
			payload.References[1] = uint32(index + 1)
			break
		}
	}
	checkpoint.Runtime, err = backend.EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSessionCheckpoint(fixture.archive, checkpoint, fixture.session.options)
	if prepared != nil {
		prepared.Discard()
	}
	if err == nil {
		t.Fatal("checkpoint passed a BaseClip to a callback that requires a Clip")
	}
	fixture.drain()
	fixture.wantHistory(fixture.event(0, wipi.PlayEventStart))
}

func TestKTFWIPIListenerDerivedGuestInterfaceSurvivesCheckpoint(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	word := func(address uint32) uint32 {
		return binary.LittleEndian.Uint32(readTestBytes(t, fixture.client, address, 4))
	}
	concrete, _ := fixture.client.JVM().AOTClass(testfixture.KTFListenerClass)
	descriptor := word(concrete.Address + 8)
	interfaces := word(descriptor + 16)
	base := word(interfaces)
	derived, err := fixture.runtime.createRuntimeJavaClass(runtimeInterfaceClass("fixture/DerivedListener"))
	if err != nil {
		t.Fatal(err)
	}
	// The guest graph has a derived interface and a cycle. Cached JVM AOT
	// metadata carries neither; validation must walk the actual guest table.
	parents, err := fixture.runtime.allocateWords([]uint32{derived, base, 0})
	if err != nil {
		t.Fatal(err)
	}
	writeTestWords(t, fixture.client, word(derived+8)+16, []uint32{parents})
	if err := fixture.client.core.Memory().Write(word(derived+8)+30, []byte{2, 0}); err != nil {
		t.Fatal(err)
	}
	writeTestWords(t, fixture.client, interfaces, []uint32{derived})
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	start := fixture.event(0, wipi.PlayEventStart)
	checkpoint, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	restoreKTFWIPIListenerCheckpoint(t, fixture, checkpoint)
	if got := fixture.drain(); got != 1 {
		t.Fatalf("restored derived listener delivered %d callbacks, want 1", got)
	}
	fixture.wantHistory(start)
	fixture.advance(fixture.duration())
	fixture.drain()
	fixture.wantHistory(start, fixture.event(0, wipi.PlayEventEndOfData))
}
