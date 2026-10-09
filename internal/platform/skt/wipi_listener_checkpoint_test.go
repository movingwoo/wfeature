package skt

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func restoreWIPIListenerCheckpoint(t *testing.T, fixture *wipiListenerFixture, saved backend.Checkpoint) {
	t.Helper()
	prepared, err := PrepareJavaCheckpoint(wipiListenerJAR, saved, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	elapsed := prepared.saved.Elapsed
	sink := newMediaPauseProbe()
	restored, err := prepared.Commit(t.Context(), fixture.runtime, fixture.store, newTestFramebuffer(t, 32, 24), sink)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runtime, fixture.sink = restored, sink
	fixture.clock.instant.Store(time.Unix(2000, 0).UnixNano())
	restored.pace = backend.NewSpeedClock(fixture.clock.now)
	restored.paceStart = fixture.clock.now().Add(-elapsed)
	restored.ResumeCheckpointOutput()
}

func wipiListenerCheckpointPayload(t *testing.T, state *javaCheckpointState, object uint32) *jvm.HeapPayloadState {
	t.Helper()
	if object == 0 || int(object) > len(state.Threads.Heap.Objects) {
		t.Fatalf("invalid fixture heap object %d", object)
	}
	native := state.Threads.Heap.Objects[object-1].Native
	if native == 0 || int(native) > len(state.Threads.Heap.Payloads) {
		t.Fatalf("fixture object %d has no native payload", object)
	}
	return &state.Threads.Heap.Payloads[native-1]
}

func encodeWIPIListenerCheckpoint(t *testing.T, saved backend.Checkpoint, state javaCheckpointState) backend.Checkpoint {
	t.Helper()
	var err error
	saved.Runtime, err = backend.EncodeCheckpointRecord(state)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func omitWIPIListenerPlayerBackreference(t *testing.T, payload *jvm.HeapPayloadState) {
	t.Helper()
	var player checkpointPlayer
	if err := backend.DecodeCheckpointRecord(payload.Data, &player); err != nil {
		t.Fatal(err)
	}
	if payload.ExternalKind != "skt.wipi-player" || len(payload.References) != 2+player.Listeners {
		t.Fatal("fixture Player has no trailing WIPI Clip reference")
	}
	// The old native kind and reference layout keep exactly the same scalar
	// record. The forward Clip -> Player reference remains in its old slot.
	payload.ExternalKind = "skt.player"
	payload.References = payload.References[:len(payload.References)-1]
}

func TestWIPIPlayListenerCheckpointRetainsPendingRecipientsAndPausedGate(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	originalClip := fixture.clip()
	listener := originalClip.Native.(*wipiClipData).listener
	if !fixture.runtime.VM.IsInstance(listener, "WIPIListenerExtension") || !fixture.runtime.VM.IsInstance(listener, wipi.PlayListenerClass) {
		t.Fatal("authored listener does not satisfy its derived and standard interfaces")
	}
	handle := originalClip.Native.(*wipiClipData).player.Native.(*playerData).handle
	fixture.runtime.AdvanceAudio()
	fixture.clock.advance(125 * time.Millisecond)
	fixture.runtime.AdvanceAudio()
	fixture.call("setListener", "(I)V", jvm.IntValue(2))
	fixture.boolean("pause", "()Z", true)
	fixture.call("setListener", "(I)V", jvm.IntValue(0))
	fixture.wantHistory()
	saved, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var state javaCheckpointState
	if err := backend.DecodeCheckpointRecord(saved.Runtime, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Platform.MediaEvents) != 2 || state.Platform.MediaEvents[0].Name != "wipi.start" || state.Platform.MediaEvents[1].Name != "wipi.pause" {
		t.Fatalf("capture lost pending WIPI transitions: %+v", state.Platform.MediaEvents)
	}
	restoreWIPIListenerCheckpoint(t, fixture, saved)
	clip := fixture.clip()
	data := clip.Native.(*wipiClipData)
	player := data.player.Native.(*playerData)
	if clip == originalClip || data.object != clip || data.listener != nil || player.wipiClip != clip || fixture.runtime.mediaPlayers[handle] != data.player {
		t.Fatal("restoration lost Clip, Player, registry, or cleared-listener identity")
	}
	for _, event := range fixture.runtime.mediaEvents {
		for _, listener := range event.Listeners {
			if !fixture.runtime.VM.IsInstance(listener, "WIPIListenerExtension") || !fixture.runtime.VM.IsInstance(listener, wipi.PlayListenerClass) {
				t.Fatal("restoration lost the pending listener's derived interface")
			}
		}
	}
	fixture.wantHistory()
	fixture.drain()
	want := []wipiListenerEvent{{wipi.PlayEventStart, 1}, {wipi.PlayEventPause, 2}}
	fixture.wantHistory(want...)
	fixture.tick(10 * time.Second)
	fixture.wantHistory(want...)
	if !fixture.runtime.audio.Paused(handle) || len(fixture.sink.resumed[handle]) != 0 || fixture.sink.on[handle] != 0 {
		t.Fatal("restoration or pending-event delivery sounded the paused owner")
	}
	fixture.boolean("resume", "()Z", true)
	fixture.drain()
	fixture.wantHistory(want...)
	if ages := fixture.sink.resumed[handle]; len(ages) != 1 || ages[0] != 125*time.Millisecond || fixture.sink.on[handle] != 0 {
		t.Fatalf("restored resume lost the existing gate: ages=%v attacks=%d", ages, fixture.sink.on[handle])
	}
	fixture.call("setListener", "(I)V", jvm.IntValue(2))
	fixture.tick(274 * time.Millisecond)
	fixture.wantHistory(want...)
	fixture.tick(time.Millisecond)
	want = append(want, wipiListenerEvent{wipi.PlayEventEndOfData, 2})
	fixture.wantHistory(want...)
	fixture.tick(time.Second)
	fixture.wantHistory(want...)
	if fixture.runtime.audio.Playing(handle) || fixture.sink.off[handle] != 1 {
		t.Fatal("checkpoint continuation changed the final remaining gate")
	}
}

func TestWIPIPlayListenerCheckpointRootsUnreferencedActiveClip(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	original := fixture.clip()
	handle := original.Native.(*wipiClipData).player.Native.(*playerData).handle
	fixture.tick(0)
	fixture.call("resetHistory", "()V")
	fixture.call("dropClip", "()V")
	if fixture.clip() != nil {
		t.Fatal("fixture did not release its Java Clip reference")
	}
	saved, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	restoreWIPIListenerCheckpoint(t, fixture, saved)
	if fixture.clip() != nil {
		t.Fatal("restoration repopulated the released static Clip")
	}
	object := fixture.runtime.mediaPlayers[handle]
	if object == nil {
		t.Fatal("restoration lost the active Player registry root")
	}
	clip := object.Native.(*playerData).wipiClip
	if clip == nil || clip == original || clip.Native.(*wipiClipData).player != object || clip.Native.(*wipiClipData).object != clip {
		t.Fatal("registry root did not retain the unreferenced Clip and its owner")
	}
	fixture.tick(399 * time.Millisecond)
	fixture.wantHistory()
	fixture.tick(time.Millisecond)
	fixture.wantHistory(wipiListenerEvent{wipi.PlayEventEndOfData, 1})
	owner, err := fixture.call("eventOwner", "(I)Lorg/kwis/msp/media/Clip;", jvm.IntValue(0)).Reference()
	if err != nil || owner != clip || fixture.runtime.audio.Playing(handle) {
		t.Fatal("unreferenced Clip completion lost its restored identity")
	}
}

func TestWIPIPlayListenerCheckpointRootsPendingEndAfterOwnerRelease(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	handle := fixture.clip().Native.(*wipiClipData).player.Native.(*playerData).handle
	fixture.tick(0)
	fixture.call("resetHistory", "()V")
	fixture.call("dropClip", "()V")
	fixture.clock.advance(400 * time.Millisecond)
	fixture.runtime.AdvanceAudio()
	fixture.wantHistory()
	if fixture.runtime.mediaPlayers[handle].Native.(*playerData).wipiClip != nil {
		t.Fatal("completed Player still strongly retains its Clip")
	}
	saved, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	restoreWIPIListenerCheckpoint(t, fixture, saved)
	if fixture.clip() != nil || len(fixture.runtime.mediaEvents) != 1 {
		t.Fatal("restoration lost the independently rooted completion event")
	}
	player := fixture.runtime.mediaPlayers[handle]
	event := fixture.runtime.mediaEvents[0]
	if player.Native.(*playerData).wipiClip != nil || event.Data == nil || event.Data.Native.(*wipiClipData).player != player {
		t.Fatal("pending completion did not preserve its stopped owner without retaining it strongly")
	}
	fixture.drain()
	fixture.wantHistory(wipiListenerEvent{wipi.PlayEventEndOfData, 1})
	fixture.tick(time.Second)
	fixture.wantHistory(wipiListenerEvent{wipi.PlayEventEndOfData, 1})
}

func TestWIPIPlayListenerCheckpointReadsPreviousPlayerReferenceLayout(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	fixture.tick(100 * time.Millisecond)
	fixture.wantHistory(wipiListenerEvent{wipi.PlayEventStart, 1})
	saved, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var state javaCheckpointState
	if err := backend.DecodeCheckpointRecord(saved.Runtime, &state); err != nil {
		t.Fatal(err)
	}
	players := 0
	for i := range state.Threads.Heap.Payloads {
		payload := &state.Threads.Heap.Payloads[i]
		if payload.ExternalKind == "skt.wipi-player" {
			omitWIPIListenerPlayerBackreference(t, payload)
			players++
		}
	}
	if players != 1 {
		t.Fatalf("fixture Player count = %d, want 1", players)
	}
	restoreWIPIListenerCheckpoint(t, fixture, encodeWIPIListenerCheckpoint(t, saved, state))
	clip := fixture.clip()
	player := clip.Native.(*wipiClipData).player
	if player.Native.(*playerData).wipiClip != clip {
		t.Fatal("legacy forward Clip reference did not rebuild its Player owner")
	}
	fixture.tick(299 * time.Millisecond)
	fixture.wantHistory(wipiListenerEvent{wipi.PlayEventStart, 1})
	fixture.tick(time.Millisecond)
	fixture.wantHistory(wipiListenerEvent{wipi.PlayEventStart, 1}, wipiListenerEvent{wipi.PlayEventEndOfData, 1})
	if _, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
		t.Fatalf("upgraded legacy owner graph could not be captured again: %v", err)
	}
}

func TestWIPIPlayListenerCheckpointRejectsMalformedOwnershipAtomically(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	originalClip := fixture.clip()
	// A second valid Clip supplies a differently owned Player and Clip for
	// type-correct cross-owner mutations. It never queues a callback.
	peer, err := fixture.runtime.VM.NewObject(wipi.ClipClass, "(Ljava/lang/String;[B)V",
		jvm.ReferenceValue(fixture.runtime.VM.NewString("mmf")), jvm.ReferenceValue(jvm.NewByteArray(audioStopSound())))
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.runtime.VM.InvokeStatic(wipi.PlayerClass, "play", "(Lorg/kwis/msp/media/Clip;Z)Z", jvm.ReferenceValue(peer), jvm.IntValue(0))
	if played, valueErr := result.Int32(); err != nil || valueErr != nil || played != 1 {
		t.Fatalf("second Clip did not start without a listener: %v, %v", err, valueErr)
	}
	if err := fixture.store.StoreSave("progress", []byte("saved")); err != nil {
		t.Fatal(err)
	}
	original, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.StoreSave("progress", []byte("current")); err != nil {
		t.Fatal(err)
	}
	beforeAudio, err := fixture.runtime.audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"missing event Clip", "wrong event Clip type", "different event Clip", "different event Player",
		"mixed event name", "missing event name", "unsupported event code",
		"wrong event listener", "multiple event listeners", "null Player Clip", "missing Player Clip reference",
		"different Player Clip", "negative Player listeners", "wrong Clip self", "wrong Clip Player", "wrong Clip listener",
		"two legacy Clips claim one Player", "missing Player registry",
	} {
		t.Run(name, func(t *testing.T) {
			var state javaCheckpointState
			if err := backend.DecodeCheckpointRecord(original.Runtime, &state); err != nil {
				t.Fatal(err)
			}
			if len(state.Platform.MediaEvents) != 1 || len(state.Platform.Players) != 2 {
				t.Fatal("fixture did not capture one event and two owned Players")
			}
			event := &state.Platform.MediaEvents[0]
			playerID := state.Threads.Heap.Roots[event.Player-1]
			clipID := state.Threads.Heap.Roots[event.Data-1]
			playerPayload := wipiListenerCheckpointPayload(t, &state, playerID)
			clipPayload := wipiListenerCheckpointPayload(t, &state, clipID)
			peerRoot := state.Platform.Players[0]
			if peerRoot == event.Player {
				peerRoot = state.Platform.Players[1]
			}
			peerID := state.Threads.Heap.Roots[peerRoot-1]
			peerPlayer := wipiListenerCheckpointPayload(t, &state, peerID)
			peerClipID := peerPlayer.References[len(peerPlayer.References)-1]
			peerClip := wipiListenerCheckpointPayload(t, &state, peerClipID)
			var player checkpointPlayer
			if err := backend.DecodeCheckpointRecord(playerPayload.Data, &player); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "missing event Clip":
				event.Data = 0
			case "wrong event Clip type":
				event.Data = event.Player
			case "different event Clip":
				state.Threads.Heap.Roots = slices.Insert(state.Threads.Heap.Roots, state.Threads.PlatformRoots, peerClipID)
				state.Threads.PlatformRoots++
				for i := range state.Threads.Threads {
					state.Threads.Threads[i].RootStart++
				}
				state.Platform.RootCount++
				event.Data = state.Platform.RootCount
			case "different event Player":
				event.Player = peerRoot
			case "mixed event name":
				event.Name = "started"
			case "missing event name":
				event.Name = ""
			case "unsupported event code":
				event.Name = "wipi.record"
			case "wrong event listener":
				event.Listeners = []int{event.Player}
			case "multiple event listeners":
				event.Listeners = append(event.Listeners, event.Listeners[0])
			case "null Player Clip":
				playerPayload.References[len(playerPayload.References)-1] = 0
			case "missing Player Clip reference":
				playerPayload.References = playerPayload.References[:len(playerPayload.References)-1]
			case "different Player Clip":
				playerPayload.References[len(playerPayload.References)-1] = peerClipID
			case "negative Player listeners":
				player.Listeners = -1
				playerPayload.References = playerPayload.References[:1]
				playerPayload.Data, err = backend.EncodeCheckpointRecord(player)
				if err != nil {
					t.Fatal(err)
				}
			case "wrong Clip self":
				clipPayload.References[2] = peerClipID
			case "wrong Clip Player":
				clipPayload.References[0] = peerID
			case "wrong Clip listener":
				clipPayload.References[1] = playerID
			case "two legacy Clips claim one Player":
				omitWIPIListenerPlayerBackreference(t, playerPayload)
				omitWIPIListenerPlayerBackreference(t, peerPlayer)
				peerClip.References[0] = playerID
			case "missing Player registry":
				state.Platform.Players = []int{peerRoot}
			}
			prepared, err := PrepareJavaCheckpoint(wipiListenerJAR, encodeWIPIListenerCheckpoint(t, original, state), Options{})
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil {
				t.Fatal("malformed WIPI listener graph was accepted")
			}
			if fixture.runtime.State() != StateActive || fixture.clip() != originalClip || len(fixture.runtime.mediaEvents) != 1 {
				t.Fatal("refused preparation changed live ownership or pending callbacks")
			}
			afterAudio, err := fixture.runtime.audio.CaptureState()
			if err != nil || !reflect.DeepEqual(beforeAudio, afterAudio) {
				t.Fatalf("refused preparation changed live playback: %v", err)
			}
			if data, found, err := fixture.store.ReadSave("progress"); err != nil || !found || string(data) != "current" {
				t.Fatalf("refused preparation changed durable saves: %q, %t, %v", data, found, err)
			}
			fixture.wantHistory()
		})
	}
	fixture.tick(0)
	fixture.wantHistory(wipiListenerEvent{wipi.PlayEventStart, 1})
	fixture.tick(400 * time.Millisecond)
	fixture.wantHistory(wipiListenerEvent{wipi.PlayEventStart, 1}, wipiListenerEvent{wipi.PlayEventEndOfData, 1})
}
