package skt

import (
	_ "embed"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/media-lifecycle.jar
var mediaLifecycleJAR []byte

type mediaLifecycleEvent struct {
	code int32
	time int64
}

type mediaLifecycleFixture struct {
	t       *testing.T
	runtime *Runtime
	clock   *mediaPauseClock
	sink    *mediaPauseProbe
	store   backend.SaveStore
}

func newMediaLifecycleFixture(t *testing.T, loops int32) *mediaLifecycleFixture {
	t.Helper()
	archive, err := Open(mediaLifecycleJAR)
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &mediaLifecycleFixture{t: t, runtime: runtime, sink: newMediaPauseProbe(), store: store}
	t.Cleanup(func() { _ = fixture.runtime.Destroy(true) })
	fixture.clock = installMediaPauseClock(runtime, fixture.sink)
	fixture.call("begin", "([BI)V", jvm.ReferenceValue(jvm.NewByteArray(audioStopSound())), jvm.IntValue(loops))
	if runtime.State() != StateActive {
		t.Fatalf("fixture state = %s, want active", runtime.State())
	}
	return fixture
}

func (fixture *mediaLifecycleFixture) call(method, descriptor string, arguments ...jvm.Value) jvm.Value {
	fixture.t.Helper()
	value, err := fixture.runtime.VM.InvokeStatic("MediaLifecycleMIDlet", method, descriptor, arguments...)
	if err != nil {
		fixture.t.Fatalf("MediaLifecycleMIDlet.%s%s: %v", method, descriptor, err)
	}
	return value
}

func (fixture *mediaLifecycleFixture) integer(method string) int32 {
	fixture.t.Helper()
	value, err := fixture.call(method, "()I").Int32()
	if err != nil {
		fixture.t.Fatal(err)
	}
	return value
}

func (fixture *mediaLifecycleFixture) long(method string) int64 {
	fixture.t.Helper()
	value, err := fixture.call(method, "()J").Int64()
	if err != nil {
		fixture.t.Fatal(err)
	}
	return value
}

func (fixture *mediaLifecycleFixture) player() *jvm.Object {
	fixture.t.Helper()
	player, err := fixture.call("getPlayer", "()Ljavax/microedition/media/Player;").Reference()
	if err != nil || player == nil {
		fixture.t.Fatalf("fixture player = %v, %v", player, err)
	}
	return player
}

func (fixture *mediaLifecycleFixture) history() []mediaLifecycleEvent {
	fixture.t.Helper()
	var events []mediaLifecycleEvent
	for index := int32(0); index < fixture.integer("eventCount"); index++ {
		code, _ := fixture.call("eventCode", "(I)I", jvm.IntValue(index)).Int32()
		at, _ := fixture.call("eventTime", "(I)J", jvm.IntValue(index)).Int64()
		events = append(events, mediaLifecycleEvent{code, at})
	}
	return events
}

func (fixture *mediaLifecycleFixture) wantHistory(want ...mediaLifecycleEvent) {
	fixture.t.Helper()
	if value, err := fixture.call("eventNameIdentity", "()Z").Int32(); err != nil || value != 1 {
		fixture.t.Fatalf("retained event string lost its constant identity: %d, %v", value, err)
	}
	if got := fixture.history(); !slices.Equal(got, want) {
		fixture.t.Fatalf("guest event history = %v, want %v", got, want)
	}
	value, err := fixture.runtime.VM.StaticField("MediaLifecycleMIDlet", "synchronousCallbacks", "I")
	if err != nil {
		fixture.t.Fatal(err)
	}
	if count, err := value.Int32(); err != nil || count != 0 {
		fixture.t.Fatalf("callbacks entered before a native wrapper returned = %d, %v", count, err)
	}
}

func (fixture *mediaLifecycleFixture) drain() {
	fixture.t.Helper()
	if err := fixture.runtime.RunPending(); err != nil {
		fixture.t.Fatal(err)
	}
}

func (fixture *mediaLifecycleFixture) tick(delta time.Duration) {
	fixture.t.Helper()
	fixture.clock.advance(delta)
	fixture.runtime.AdvanceAudio()
	fixture.drain()
}

func TestMIDPLifecycleFiniteLoopsCompleteAndRestart(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 2)
	handle := fixture.player().Native.(*playerData).handle
	if got := fixture.long("duration"); got != 400000 {
		t.Fatalf("authored duration = %d, want 400000 microseconds", got)
	}
	fixture.wantHistory()
	fixture.runtime.AdvanceAudio()
	fixture.wantHistory()
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{1, 0})
	fixture.tick(1300 * time.Millisecond)
	if fixture.sink.on[handle] != 2 || fixture.sink.off[handle] != 2 || fixture.runtime.audio.Playing(handle) {
		t.Fatalf("two loops emitted attacks=%d releases=%d playing=%t", fixture.sink.on[handle], fixture.sink.off[handle], fixture.runtime.audio.Playing(handle))
	}
	if state, position := fixture.integer("state"), fixture.long("mediaTime"); state != playerPrefetched || position != 400000 {
		t.Fatalf("completed player state=%d mediaTime=%d, want PREFETCHED at 400000", state, position)
	}
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{3, 400000}, mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{3, 400000})
	fixture.call("start", "()V")
	if fixture.integer("eventCount") != 4 {
		t.Fatal("restart delivered its listener inside the native call")
	}
	fixture.tick(0)
	if fixture.sink.on[handle] != 3 || fixture.integer("state") != playerStarted || fixture.long("mediaTime") != 0 {
		t.Fatal("start after natural completion did not replay from zero")
	}
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{3, 400000}, mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{3, 400000}, mediaLifecycleEvent{1, 0})
}

func TestMIDPLifecycleStopFreezesMediaTimeAndRemainingGate(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 1)
	handle := fixture.player().Native.(*playerData).handle
	fixture.tick(0)
	fixture.tick(125 * time.Millisecond)
	if position := fixture.long("mediaTime"); position != 125000 {
		t.Fatalf("progressed media time = %d, want 125000", position)
	}
	fixture.call("stop", "()V")
	fixture.call("stop", "()V")
	fixture.wantHistory(mediaLifecycleEvent{1, 0})
	fixture.tick(10 * time.Second)
	if fixture.long("mediaTime") != 125000 || fixture.integer("state") != playerPrefetched || !fixture.runtime.audio.Paused(handle) {
		t.Fatal("stopped player lost its frozen cursor")
	}
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{2, 125000})
	fixture.call("start", "()V")
	fixture.call("start", "()V")
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{2, 125000}, mediaLifecycleEvent{1, 125000})
	if ages := fixture.sink.resumed[handle]; len(ages) != 1 || ages[0] != 125*time.Millisecond || fixture.sink.on[handle] != 1 {
		t.Fatalf("resumed voice ages=%v attacks=%d, want the existing 125ms voice", ages, fixture.sink.on[handle])
	}
	fixture.tick(274 * time.Millisecond)
	if fixture.long("mediaTime") != 399000 || fixture.sink.off[handle] != 0 {
		t.Fatal("stop shortened the remaining note gate")
	}
	fixture.tick(time.Millisecond)
	if fixture.long("mediaTime") != 400000 || fixture.integer("state") != playerPrefetched || fixture.sink.off[handle] != 1 {
		t.Fatal("resume restarted the note gate instead of finishing its remainder")
	}
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{2, 125000}, mediaLifecycleEvent{1, 125000}, mediaLifecycleEvent{3, 400000})
}

func TestMIDPLifecycleEndCallbackRestartWaitsForNextDeliveryPass(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 1)
	handle := fixture.player().Native.(*playerData).handle
	fixture.tick(0)
	if err := fixture.runtime.VM.SetStaticField("MediaLifecycleMIDlet", "restartOnceOnEnd", "Z", jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	fixture.tick(400 * time.Millisecond)
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{3, 400000})
	if !fixture.runtime.audio.Playing(handle) || fixture.sink.on[handle] != 1 {
		t.Fatal("end callback did not restart the player for the next audio pass")
	}
	fixture.tick(0)
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{3, 400000}, mediaLifecycleEvent{1, 0})
	if fixture.sink.on[handle] != 2 || fixture.integer("state") != playerStarted {
		t.Fatal("callback restart did not produce one fresh attack")
	}
	fixture.tick(400 * time.Millisecond)
	fixture.tick(400 * time.Millisecond)
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{3, 400000}, mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{3, 400000})
	if fixture.sink.on[handle] != 2 || fixture.integer("state") != playerPrefetched {
		t.Fatal("one-shot callback restart continued repeating")
	}
}

func TestMIDPLifecycleVolumeCallbackMutationUsesNextDeliveryPass(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, -1)
	handle := fixture.player().Native.(*playerData).handle
	fixture.tick(0)
	fixture.call("resetHistory", "()V")
	fixture.call("checkControl", "()V")
	if err := fixture.runtime.VM.SetStaticField("MediaLifecycleMIDlet", "muteOnceOnVolumeChanged", "Z", jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	fixture.call("setLevel", "(I)I", jvm.IntValue(40))
	fixture.wantHistory()
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{5, -1})
	level, _ := fixture.call("eventLevel", "(I)I", jvm.IntValue(0)).Int32()
	muted, _ := fixture.call("eventMuted", "(I)Z", jvm.IntValue(0)).Int32()
	if level != 40 || muted != 0 || fixture.sink.gains[handle] != 0 {
		t.Fatalf("first volume callback level=%d muted=%d resultingGain=%d", level, muted, fixture.sink.gains[handle])
	}
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{5, -1}, mediaLifecycleEvent{5, -1})
	level, _ = fixture.call("eventLevel", "(I)I", jvm.IntValue(1)).Int32()
	muted, _ = fixture.call("eventMuted", "(I)Z", jvm.IntValue(1)).Int32()
	if level != 40 || muted != 1 {
		t.Fatalf("deferred callback level=%d muted=%d, want 40 and true", level, muted)
	}
	fixture.call("setLevel", "(I)I", jvm.IntValue(40))
	fixture.call("setMute", "(Z)V", jvm.IntValue(1))
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{5, -1}, mediaLifecycleEvent{5, -1})
}

func TestMIDPLifecyclePendingEventsRestoreOnceWithTheirOwners(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 2)
	originalPlayer := fixture.player()
	handle := originalPlayer.Native.(*playerData).handle
	fixture.runtime.AdvanceAudio()
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{1, 0})
	fixture.clock.advance(500 * time.Millisecond)
	fixture.runtime.AdvanceAudio()
	fixture.call("stop", "()V")
	fixture.call("setLevel", "(I)I", jvm.IntValue(35))
	fixture.wantHistory(mediaLifecycleEvent{1, 0})
	saved, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareJavaCheckpoint(mediaLifecycleJAR, saved, Options{})
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
	restored.pace, restored.paceStart = backend.NewSpeedClock(fixture.clock.now), fixture.clock.now().Add(-elapsed)
	if fixture.player() == originalPlayer {
		t.Fatal("checkpoint reused the old player object")
	}
	fixture.call("checkControl", "()V")
	restored.ResumeCheckpointOutput()
	fixture.wantHistory(mediaLifecycleEvent{1, 0})
	fixture.drain()
	want := []mediaLifecycleEvent{{1, 0}, {3, 400000}, {1, 0}, {2, 100000}, {5, -1}}
	fixture.wantHistory(want...)
	level, _ := fixture.call("eventLevel", "(I)I", jvm.IntValue(4)).Int32()
	if level != 35 || len(sink.resumed[handle]) != 0 || sink.on[handle] != 0 {
		t.Fatal("restoration lost the queued control or sounded a paused owner")
	}
	fixture.drain()
	fixture.wantHistory(want...)
	fixture.call("start", "()V")
	fixture.tick(0)
	want = append(want, mediaLifecycleEvent{1, 100000})
	fixture.wantHistory(want...)
	if len(sink.resumed[handle]) != 1 || sink.on[handle] != 0 {
		t.Fatal("restored player resumed a different owner or replayed its attack")
	}
	fixture.tick(300 * time.Millisecond)
	fixture.wantHistory(append(want, mediaLifecycleEvent{3, 400000})...)
	if fixture.integer("state") != playerPrefetched || fixture.runtime.audio.Playing(handle) {
		t.Fatal("checkpoint lost the final remaining repetition")
	}
}

func TestMIDPLifecycleCheckpointRejectsMalformedPendingEvents(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 1)
	fixture.wantHistory()
	original, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing owner", "wrong owner", "missing payload", "wrong payload", "wrong listener", "duplicate listener", "unknown event", "missing registry", "event limit", "listener limit"} {
		t.Run(name, func(t *testing.T) {
			var state javaCheckpointState
			if err := backend.DecodeCheckpointRecord(original.Runtime, &state); err != nil {
				t.Fatal(err)
			}
			if len(state.Platform.MediaEvents) != 1 {
				t.Fatalf("saved pending event count = %d, want 1", len(state.Platform.MediaEvents))
			}
			event := &state.Platform.MediaEvents[0]
			switch name {
			case "missing owner":
				event.Player = 0
			case "wrong owner":
				event.Player = state.Platform.MIDlet
			case "missing payload":
				event.Data = 0
			case "wrong payload":
				event.Data = state.Platform.MIDlet
			case "wrong listener":
				event.Listeners = []int{event.Player}
			case "duplicate listener":
				event.Listeners = append(event.Listeners, event.Listeners[0])
			case "unknown event":
				event.Name = "unsupportedEvent"
			case "missing registry":
				state.Platform.Players = nil
			case "event limit":
				for len(state.Platform.MediaEvents) <= maxPlayerEvents {
					state.Platform.MediaEvents = append(state.Platform.MediaEvents, *event)
				}
			case "listener limit":
				event.Listeners = make([]int, maxPlayerListeners+1)
			}
			broken := original
			broken.Runtime, err = backend.EncodeCheckpointRecord(state)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareJavaCheckpoint(mediaLifecycleJAR, broken, Options{})
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil {
				t.Fatal("malformed pending event was accepted")
			}
		})
	}
	fixture.wantHistory()
	fixture.tick(0)
	fixture.wantHistory(mediaLifecycleEvent{1, 0})
}

func TestMIDPLifecycleRejectsInvalidLoopsWithoutChangingPausedPlayback(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 2)
	handle := fixture.player().Native.(*playerData).handle
	fixture.tick(0)
	fixture.tick(100 * time.Millisecond)
	fixture.call("stop", "()V")
	fixture.drain()
	for _, loops := range []int32{0, -2, -1 << 31} {
		_, err := fixture.runtime.VM.InvokeStatic("MediaLifecycleMIDlet", "setLoops", "(I)V", jvm.IntValue(loops))
		if !fixture.runtime.VM.IsGuestException(err, "java/lang/IllegalArgumentException") {
			t.Fatalf("setLoops(%d) = %v, want IllegalArgumentException", loops, err)
		}
	}
	fixture.tick(time.Second)
	if fixture.long("mediaTime") != 100000 || fixture.integer("state") != playerPrefetched {
		t.Fatal("refused loop count changed the paused cursor")
	}
	fixture.call("start", "()V")
	fixture.tick(700 * time.Millisecond)
	if fixture.sink.on[handle] != 2 || fixture.integer("state") != playerPrefetched || fixture.long("mediaTime") != 400000 {
		t.Fatal("refused loop count changed the remaining repetitions")
	}
}

func TestMIDPLifecycleClosedGuardsAndDuplicateTransitions(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 1)
	fixture.call("start", "()V")
	fixture.tick(0)
	fixture.wantHistory(mediaLifecycleEvent{1, 0})
	fixture.call("stop", "()V")
	fixture.call("stop", "()V")
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{2, 0})
	fixture.call("close", "()V")
	fixture.call("close", "()V")
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{2, 0}, mediaLifecycleEvent{4, -1})
	if fixture.integer("state") != playerClosed {
		t.Fatal("closed player has a live state")
	}
	for _, call := range []struct {
		method, descriptor string
		arguments          []jvm.Value
	}{
		{"start", "()V", nil}, {"stop", "()V", nil}, {"deallocate", "()V", nil},
		{"setLoops", "(I)V", []jvm.Value{jvm.IntValue(1)}}, {"rewind", "()J", nil},
		{"mediaTime", "()J", nil}, {"duration", "()J", nil},
		{"getControl", "()Ljavax/microedition/media/control/VolumeControl;", nil},
		{"level", "()I", nil}, {"isMuted", "()Z", nil},
		{"setLevel", "(I)I", []jvm.Value{jvm.IntValue(50)}}, {"setMute", "(Z)V", []jvm.Value{jvm.IntValue(1)}},
	} {
		_, err := fixture.runtime.VM.InvokeStatic("MediaLifecycleMIDlet", call.method, call.descriptor, call.arguments...)
		if !fixture.runtime.VM.IsGuestException(err, "java/lang/IllegalStateException") {
			t.Errorf("closed %s = %v, want IllegalStateException", call.method, err)
		}
	}
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{1, 0}, mediaLifecycleEvent{2, 0}, mediaLifecycleEvent{4, -1})
}
