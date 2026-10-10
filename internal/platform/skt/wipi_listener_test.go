package skt

import (
	_ "embed"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/wipi-listener.jar
var wipiListenerJAR []byte

type wipiListenerEvent struct{ code, listener int32 }

type wipiListenerFixture struct {
	t       *testing.T
	runtime *Runtime
	clock   *mediaPauseClock
	sink    *mediaPauseProbe
	store   *backend.MemorySaveStore
}

func newWIPIListenerFixture(t *testing.T) *wipiListenerFixture {
	t.Helper()
	archive, err := Open(wipiListenerJAR)
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
	fixture := &wipiListenerFixture{t: t, runtime: runtime, sink: newMediaPauseProbe(), store: store}
	t.Cleanup(func() { _ = fixture.runtime.Destroy(true) })
	fixture.clock = installMediaPauseClock(runtime, fixture.sink)
	return fixture
}

func (fixture *wipiListenerFixture) call(method, descriptor string, arguments ...jvm.Value) jvm.Value {
	fixture.t.Helper()
	value, err := fixture.runtime.VM.InvokeStatic("WIPIListenerMIDlet", method, descriptor, arguments...)
	if err != nil {
		fixture.t.Fatalf("WIPIListenerMIDlet.%s%s: %v", method, descriptor, err)
	}
	return value
}

func (fixture *wipiListenerFixture) boolean(method, descriptor string, want bool, arguments ...jvm.Value) {
	fixture.t.Helper()
	value, err := fixture.call(method, descriptor, arguments...).Int32()
	if err != nil || (value != 0) != want {
		fixture.t.Fatalf("%s returned %d, %v; want %t", method, value, err, want)
	}
}

func (fixture *wipiListenerFixture) begin(repeat bool) {
	fixture.t.Helper()
	loop := int32(0)
	if repeat {
		loop = 1
	}
	fixture.boolean("begin", "([BZ)Z", true, jvm.ReferenceValue(jvm.NewByteArray(audioStopSound())), jvm.IntValue(loop))
}

func (fixture *wipiListenerFixture) clip() *jvm.Object {
	fixture.t.Helper()
	object, err := fixture.call("getClip", "()Lorg/kwis/msp/media/Clip;").Reference()
	if err != nil {
		fixture.t.Fatal(err)
	}
	return object
}

func (fixture *wipiListenerFixture) history() []wipiListenerEvent {
	fixture.t.Helper()
	count, err := fixture.call("eventCount", "()I").Int32()
	if err != nil {
		fixture.t.Fatal(err)
	}
	var events []wipiListenerEvent
	for index := int32(0); index < count; index++ {
		code, _ := fixture.call("eventCode", "(I)I", jvm.IntValue(index)).Int32()
		listener, _ := fixture.call("eventListener", "(I)I", jvm.IntValue(index)).Int32()
		parameter, _ := fixture.call("eventParameter", "(I)I", jvm.IntValue(index)).Int32()
		owner, err := fixture.call("eventOwner", "(I)Lorg/kwis/msp/media/Clip;", jvm.IntValue(index)).Reference()
		if err != nil || owner == nil || !fixture.runtime.VM.IsInstance(owner, wipi.ClipClass) || parameter != 0 {
			fixture.t.Fatalf("callback %d lost its Clip or zero parameter: %v", index, err)
		}
		events = append(events, wipiListenerEvent{code, listener})
	}
	return events
}

func (fixture *wipiListenerFixture) wantHistory(want ...wipiListenerEvent) {
	fixture.t.Helper()
	if got := fixture.history(); !slices.Equal(got, want) {
		fixture.t.Fatalf("WIPI listener history = %v, want %v", got, want)
	}
	value, err := fixture.runtime.VM.StaticField("WIPIListenerMIDlet", "synchronousCallbacks", "I")
	if err != nil {
		fixture.t.Fatal(err)
	}
	if count, err := value.Int32(); err != nil || count != 0 {
		fixture.t.Fatalf("callbacks entered during a native wrapper: %d, %v", count, err)
	}
}

func (fixture *wipiListenerFixture) drain() {
	fixture.t.Helper()
	if err := fixture.runtime.RunPending(); err != nil {
		fixture.t.Fatal(err)
	}
}

func (fixture *wipiListenerFixture) tick(delta time.Duration) {
	fixture.t.Helper()
	fixture.clock.advance(delta)
	fixture.runtime.AdvanceAudio()
	fixture.drain()
}

func TestWIPIPlayListenerStartsThroughDeferredQueue(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	fixture.wantHistory()
	fixture.runtime.AdvanceAudio()
	fixture.wantHistory()
	fixture.drain()
	fixture.wantHistory(wipiListenerEvent{2, 1})
	fixture.drain()
	fixture.wantHistory(wipiListenerEvent{2, 1})
}

func TestWIPIPlayListenerTransitionsAndNoops(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.call("create", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(audioStopSound())))
	fixture.boolean("pause", "()Z", false)
	fixture.boolean("resume", "()Z", false)
	fixture.call("stop", "()Z")
	fixture.drain()
	fixture.wantHistory()
	fixture.boolean("play", "(Z)Z", true, jvm.IntValue(0))
	fixture.boolean("play", "(Z)Z", true, jvm.IntValue(0))
	fixture.boolean("pause", "()Z", true)
	fixture.boolean("pause", "()Z", false)
	fixture.boolean("resume", "()Z", true)
	fixture.boolean("resume", "()Z", false)
	fixture.boolean("pause", "()Z", true)
	fixture.boolean("stop", "()Z", true)
	fixture.call("stop", "()Z")
	fixture.boolean("resume", "()Z", false)
	fixture.boolean("pause", "()Z", false)
	fixture.boolean("play", "(Z)Z", true, jvm.IntValue(0))
	fixture.boolean("stop", "()Z", true)
	fixture.wantHistory()
	fixture.drain()
	fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{2, 1}, wipiListenerEvent{4, 1},
		wipiListenerEvent{5, 1}, wipiListenerEvent{4, 1}, wipiListenerEvent{3, 1}, wipiListenerEvent{2, 1}, wipiListenerEvent{3, 1})

	// An unsupported/empty clip has no successful playback transition.
	fixture.call("create", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(nil)))
	fixture.boolean("play", "(Z)Z", false, jvm.IntValue(0))
	fixture.boolean("pause", "()Z", false)
	fixture.boolean("resume", "()Z", false)
	fixture.boolean("stop", "()Z", false)
	fixture.drain()
	fixture.wantHistory()
}

func TestWIPIPlayListenerReportsEachNaturalPassWithoutSyntheticStart(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		t.Run(map[bool]string{false: "one-shot", true: "repeat"}[repeat], func(t *testing.T) {
			fixture := newWIPIListenerFixture(t)
			fixture.begin(repeat)
			handle := fixture.clip().Native.(*wipiClipData).player.Native.(*playerData).handle
			fixture.drain()
			fixture.clock.advance(1300 * time.Millisecond)
			fixture.runtime.AdvanceAudio()
			fixture.wantHistory(wipiListenerEvent{2, 1})
			fixture.drain()
			if repeat {
				fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{1, 1}, wipiListenerEvent{1, 1}, wipiListenerEvent{1, 1})
				if fixture.sink.on[handle] != 4 || !fixture.runtime.audio.Playing(handle) {
					t.Fatal("listener delivery changed the repeating score")
				}
			} else {
				fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{1, 1})
				fixture.call("stop", "()Z")
				fixture.tick(time.Second)
				fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{1, 1})
				if fixture.sink.on[handle] != 1 || fixture.runtime.audio.Playing(handle) {
					t.Fatal("completed one-shot restarted or emitted duplicate output")
				}
			}
		})
	}
}

func TestWIPIPlayListenerReplacementSnapshotsAndNullRemoval(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	fixture.call("setListener", "(I)V", jvm.IntValue(2))
	fixture.boolean("pause", "()Z", true)
	fixture.call("setListener", "(I)V", jvm.IntValue(0))
	fixture.boolean("resume", "()Z", true)
	fixture.boolean("stop", "()Z", true)
	fixture.wantHistory()
	fixture.drain()
	fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{4, 2})
	fixture.call("setListener", "(I)V", jvm.IntValue(2))
	fixture.boolean("play", "(Z)Z", true, jvm.IntValue(0))
	fixture.call("setListener", "(I)V", jvm.IntValue(1))
	fixture.boolean("stop", "()Z", true)
	fixture.drain()
	want := []wipiListenerEvent{{2, 1}, {4, 2}, {2, 2}, {3, 1}}
	fixture.wantHistory(want...)
	fixture.call("setListener", "(I)V", jvm.IntValue(0))
	fixture.boolean("play", "(Z)Z", true, jvm.IntValue(1))
	fixture.tick(1300 * time.Millisecond)
	fixture.wantHistory(want...)
}

func TestWIPIPlayListenerReentrantRestartWaitsForNextDrain(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	if err := fixture.runtime.VM.SetStaticField("WIPIListenerMIDlet", "restartOnceOnEnd", "Z", jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	fixture.tick(0)
	fixture.tick(400 * time.Millisecond)
	fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{1, 1})
	fixture.drain()
	fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{1, 1}, wipiListenerEvent{2, 1})
	fixture.tick(400 * time.Millisecond)
	fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{1, 1}, wipiListenerEvent{2, 1}, wipiListenerEvent{1, 1})
	fixture.tick(time.Second)
	fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{1, 1}, wipiListenerEvent{2, 1}, wipiListenerEvent{1, 1})
}

func TestWIPIPlayListenerRetainsOwnerAfterGuestDropsClip(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	clip := fixture.clip()
	fixture.call("dropClip", "()V")
	fixture.drain()
	fixture.wantHistory(wipiListenerEvent{2, 1})
	owner, _ := fixture.call("eventOwner", "(I)Lorg/kwis/msp/media/Clip;", jvm.IntValue(0)).Reference()
	if owner != clip || fixture.clip() != nil {
		t.Fatal("queued START lost the clip whose static reference was dropped")
	}
	fixture.call("resetHistory", "()V")
	fixture.tick(400 * time.Millisecond)
	fixture.wantHistory(wipiListenerEvent{1, 1})
	owner, _ = fixture.call("eventOwner", "(I)Lorg/kwis/msp/media/Clip;", jvm.IntValue(0)).Reference()
	if owner != clip || fixture.clip() != nil {
		t.Fatal("natural END_OF_DATA lost the original clip owner")
	}
}

func TestWIPIPlayListenerRejectsMalformedReplacement(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	clip := fixture.clip()
	previous := clip.Native.(*wipiClipData).listener
	_, err := fixture.runtime.VM.InvokeVirtual(clip, "setListener", "(Lorg/kwis/msp/media/PlayListener;)V",
		jvm.ReferenceValue(&jvm.Object{ClassName: jvm.ObjectClass}))
	if err == nil || clip.Native.(*wipiClipData).listener != previous {
		t.Fatalf("invalid replacement changed the listener or was accepted: %v", err)
	}
	fixture.boolean("pause", "()Z", true)
	fixture.drain()
	fixture.wantHistory(wipiListenerEvent{2, 1}, wipiListenerEvent{4, 1})
}

func TestWIPIPlayListenerQueueHasFiniteCapacity(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.begin(false)
	fixture.runtime.AttachAudioSink(nil)
	arguments := []jvm.Value{jvm.ReferenceValue(fixture.clip()), jvm.IntValue(0)}
	for i := 1; i < maxPlayerEvents; i++ {
		value, err := fixture.runtime.wipiPlayerPlay(fixture.runtime.VM, arguments)
		ok, _ := value.Int32()
		if err != nil || ok != 1 {
			t.Fatalf("valid transition %d refused before the queue bound: %v", i, err)
		}
	}
	if len(fixture.runtime.mediaEvents) != maxPlayerEvents {
		t.Fatalf("pending transitions = %d, want %d", len(fixture.runtime.mediaEvents), maxPlayerEvents)
	}
	if _, err := fixture.runtime.wipiPlayerPlay(fixture.runtime.VM, arguments); err == nil || len(fixture.runtime.mediaEvents) != maxPlayerEvents {
		t.Fatalf("overflow did not refuse at the fixed queue bound: %v", err)
	}
	fixture.wantHistory()
}

func TestWIPIPlayListenerConcurrentReplacementPlaybackAndDelivery(t *testing.T) {
	fixture := newWIPIListenerFixture(t)
	fixture.call("create", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(audioStopSound())))
	fixture.runtime.AttachAudioSink(nil)
	guest, clip := fixture.runtime, fixture.clip()
	const listenerClass = "ConcurrentWIPIListener"
	const update = "(Lorg/kwis/msp/media/Clip;II)V"
	if err := guest.VM.DefineClass(jvm.ClassDefinition{Name: listenerClass, SuperName: jvm.ObjectClass,
		Interfaces: []string{wipi.PlayListenerClass}, Access: jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{{Name: "playUpdate", Descriptor: update, Access: jvm.AccessPublic | jvm.AccessNative}}}); err != nil {
		t.Fatal(err)
	}
	var delivered atomic.Int32
	if err := guest.VM.RegisterNative(listenerClass, "playUpdate", update, func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		owner, _ := arguments[1].Reference()
		code, _ := arguments[2].Int32()
		parameter, _ := arguments[3].Int32()
		if owner != clip || code != 2 || parameter != 0 {
			return jvm.VoidValue(), fmt.Errorf("concurrent WIPI callback lost its owner or event")
		}
		if _, err := guest.wipiClipVolume(vm, []jvm.Value{jvm.ReferenceValue(owner)}); err != nil {
			return jvm.VoidValue(), err
		}
		delivered.Add(1)
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	listener := &jvm.Object{ClassName: listenerClass}
	set := []jvm.Value{jvm.ReferenceValue(clip), jvm.ReferenceValue(listener)}
	if _, err := guest.wipiClipSetListener(guest.VM, set); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			for range 32 {
				if _, err := guest.wipiPlayerPlay(guest.VM, []jvm.Value{jvm.ReferenceValue(clip), jvm.IntValue(0)}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Go(func() {
		for range 64 {
			if _, err := guest.wipiClipSetListener(guest.VM, set); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Go(func() {
		for range 64 {
			if err := guest.RunPending(); err != nil {
				t.Error(err)
				return
			}
		}
	})
	workers.Wait()
	fixture.drain()
	if got := delivered.Load(); got != 64 {
		t.Fatalf("concurrent transitions delivered %d callbacks, want 64", got)
	}
	fixture.drain()
	if delivered.Load() != 64 {
		t.Fatal("draining an empty queue repeated callbacks")
	}
}

func TestWIPIListenerReplacementAfterPeerQueryKeepsCompletionRecipient(t *testing.T) {
	for _, change := range []struct {
		name     string
		previous int32
		next     int32
	}{
		{"replace", 1, 2}, {"remove", 1, 0}, {"late registration", 0, 2},
	} {
		t.Run(change.name, func(t *testing.T) {
			fixture := newWIPIListenerFixture(t)
			fixture.begin(false)
			fixture.drain()
			if change.previous == 0 {
				fixture.call("setListener", "(I)V", jvm.IntValue(0))
			}
			value, err := fixture.runtime.newPlayer(nil, audioStopSound(), "audio/mmf")
			if err != nil {
				t.Fatal(err)
			}
			peer, _ := value.Reference()
			wipiStreamCall(t, fixture.runtime, peer, "start", "()V")
			fixture.clock.advance(400 * time.Millisecond)
			wipiStreamCall(t, fixture.runtime, peer, "getState", "()I")
			player := fixture.clip().Native.(*wipiClipData).player.Native.(*playerData)
			progress, err := fixture.runtime.audio.PlaybackState(player.handle)
			if err != nil || progress.Completed != 1 || player.completed != 0 {
				t.Fatalf("peer query did not leave an unobserved completion: progress=%+v cached=%d err=%v", progress, player.completed, err)
			}
			fixture.call("setListener", "(I)V", jvm.IntValue(change.next))
			fixture.drain()
			want := []wipiListenerEvent{{2, 1}}
			if change.previous != 0 {
				want = append(want, wipiListenerEvent{1, change.previous})
			}
			fixture.wantHistory(want...)
			fixture.tick(0)
			fixture.wantHistory(want...)
		})
	}
}
