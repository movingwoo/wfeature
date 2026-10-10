package skt

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestMIDPConcurrentQueueProducersAndDelivery(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 1)
	fixture.tick(0)
	runtime, player := fixture.runtime, fixture.player()
	data := player.Native.(*playerData)
	wipiStreamCall(t, runtime, player, "removePlayerListener", "(Ljavax/microedition/media/PlayerListener;)V", jvm.ReferenceValue(data.listeners[0]))
	const listenerClass = "ConcurrentMediaListener"
	const update = "(Ljavax/microedition/media/Player;Ljava/lang/String;Ljava/lang/Object;)V"
	if err := runtime.VM.DefineClass(jvm.ClassDefinition{Name: listenerClass, SuperName: jvm.ObjectClass,
		Interfaces: []string{midp.PlayerListenerClass}, Access: jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{{Name: "playerUpdate", Descriptor: update, Access: jvm.AccessPublic | jvm.AccessNative}}}); err != nil {
		t.Fatal(err)
	}
	var delivered atomic.Int32
	if err := runtime.VM.RegisterNative(listenerClass, "playerUpdate", update, func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		owner, _ := arguments[1].Reference()
		control, _ := arguments[3].Reference()
		if owner != player || control != data.volumeControl {
			return jvm.VoidValue(), fmt.Errorf("concurrent media event lost its owner")
		}
		if _, err := runtime.playerVolumeLevel(nil, []jvm.Value{jvm.ReferenceValue(control)}); err != nil {
			return jvm.VoidValue(), err
		}
		delivered.Add(1)
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	listener := &jvm.Object{ClassName: listenerClass, Fields: make(map[string]jvm.Value)}
	wipiStreamCall(t, runtime, player, "addPlayerListener", "(Ljavax/microedition/media/PlayerListener;)V", jvm.ReferenceValue(listener))
	var workers sync.WaitGroup
	for worker := int32(0); worker < 4; worker++ {
		workers.Go(func() {
			// Every write is distinct, regardless of the interleaving.
			for level := worker*16 + 1; level <= (worker+1)*16; level++ {
				if _, err := runtime.setPlayerVolumeLevel(nil, []jvm.Value{jvm.ReferenceValue(data.volumeControl), jvm.IntValue(level)}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Go(func() {
		for i := 0; i < 32; i++ {
			if err := runtime.RunPending(); err != nil {
				t.Error(err)
				return
			}
		}
	})
	workers.Wait()
	fixture.drain()
	if got := delivered.Load(); got != 64 {
		t.Fatalf("concurrent delivery count = %d, want 64", got)
	}
	fixture.drain()
	if delivered.Load() != 64 {
		t.Fatal("a drained media event was delivered twice")
	}
}

func TestMIDPRemovedListenerRetainsAlreadyQueuedEvents(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 1)
	player := fixture.player()
	listener := player.Native.(*playerData).listeners[0]
	wipiStreamCall(t, fixture.runtime, player, "removePlayerListener", "(Ljavax/microedition/media/PlayerListener;)V", jvm.ReferenceValue(listener))
	fixture.call("close", "()V")
	fixture.wantHistory()
	fixture.drain()
	fixture.wantHistory(mediaLifecycleEvent{1, 0})
	for _, method := range []string{"addPlayerListener", "removePlayerListener"} {
		_, err := fixture.runtime.VM.InvokeVirtual(player, method, "(Ljavax/microedition/media/PlayerListener;)V", jvm.ReferenceValue(nil))
		if !fixture.runtime.VM.IsGuestException(err, "java/lang/IllegalStateException") {
			t.Fatalf("closed %s(null) = %v", method, err)
		}
	}
}

func TestMIDPConcurrentHostAdvanceAndPlayerTransitions(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 2)
	fixture.tick(0)
	player := fixture.player()
	data := player.Native.(*playerData)
	wipiStreamCall(t, fixture.runtime, player, "removePlayerListener", "(Ljavax/microedition/media/PlayerListener;)V", jvm.ReferenceValue(data.listeners[0]))
	fixture.runtime.AttachAudioSink(nil)

	// Keep the guest clock moving while ordinary native calls capture their
	// timestamps. A Player query and its following transition must remain
	// atomic with the Host's global advancement, including raw clips.
	start := make(chan struct{})
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	const rounds = 4096
	workers.Go(func() {
		<-start
		for i := 0; i < rounds; i++ {
			fixture.clock.advance(2 * time.Millisecond)
			fixture.runtime.AdvanceAudio()
			if fixture.runtime.State() != StateActive {
				errors <- fmt.Errorf("Host advancement changed lifecycle state to %s", fixture.runtime.State())
				return
			}
			runtime.Gosched()
		}
	})
	workers.Go(func() {
		<-start
		for i := 0; i < rounds; i++ {
			for _, call := range []struct {
				name      string
				method    jvm.NativeMethod
				arguments []jvm.Value
			}{
				{"media time", fixture.runtime.playerMediaTime, []jvm.Value{jvm.ReferenceValue(player)}},
				{"stop", fixture.runtime.playerStop, []jvm.Value{jvm.ReferenceValue(player)}},
				{"start", fixture.runtime.playerStart, []jvm.Value{jvm.ReferenceValue(player)}},
				{"rewind", fixture.runtime.setPlayerMediaTime, []jvm.Value{jvm.ReferenceValue(player), jvm.LongValue(0)}},
			} {
				if _, err := call.method(nil, call.arguments); err != nil {
					errors <- fmt.Errorf("concurrent %s at round %d: %w", call.name, i, err)
					return
				}
				runtime.Gosched()
			}
		}
	})
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	if _, err := fixture.runtime.playerStart(nil, []jvm.Value{jvm.ReferenceValue(player)}); err != nil {
		t.Fatalf("start after concurrent transitions: %v", err)
	}
	if got := fixture.integer("state"); got != playerStarted {
		t.Fatalf("final Player state = %d, want started", got)
	}
	if _, err := fixture.runtime.audioTimeline().CaptureState(); err != nil {
		t.Fatalf("concurrent transitions left invalid audio state: %v", err)
	}
}

func TestSKVMConcurrentHostAdvanceAndClipTransitions(t *testing.T) {
	guest := wipiStreamRuntime(t, nil)
	clock := installMediaPauseClock(guest, newMediaPauseProbe())
	guest.AttachAudioSink(nil)
	object, err := guest.VM.NewObject(skvm.RuntimeAudioClipClass, "(Ljava/lang/String;)V", jvm.ReferenceValue(guest.VM.NewString("mmf")))
	if err != nil {
		t.Fatal(err)
	}
	sound := audioStopSound()
	wipiStreamCall(t, guest, object, "open", "([BII)V", jvm.ReferenceValue(jvm.NewByteArray(sound)), jvm.IntValue(0), jvm.IntValue(int32(len(sound))))
	// Host invocations return from the SKVM playback wait, leaving the score
	// looping while another Host pass and native transitions run concurrently.
	wipiStreamCall(t, guest, object, "loop", "()V")
	start := make(chan struct{})
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	const rounds = 4096
	workers.Go(func() {
		<-start
		for i := 0; i < rounds; i++ {
			clock.advance(400 * time.Millisecond)
			guest.AdvanceAudio()
			if guest.State() != StateActive {
				errors <- fmt.Errorf("SKVM Host advancement changed lifecycle state to %s", guest.State())
				return
			}
			runtime.Gosched()
		}
	})
	workers.Go(func() {
		<-start
		for i := 0; i < rounds; i++ {
			for _, method := range []string{"pause", "resume", "stop", "loop"} {
				if _, err := guest.VM.InvokeVirtual(object, method, "()V"); err != nil {
					errors <- fmt.Errorf("concurrent SKVM %s at round %d: %w", method, i, err)
					return
				}
				runtime.Gosched()
			}
		}
	})
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	wipiStreamCall(t, guest, object, "stop", "()V")
	handle := object.Native.(*audioClipData).handle
	if guest.audioTimeline().Playing(handle) || guest.audioTimeline().Paused(handle) {
		t.Fatal("SKVM stop after concurrent transitions retained playback")
	}
	if _, err := guest.audioTimeline().CaptureState(); err != nil {
		t.Fatalf("concurrent SKVM transitions left invalid audio state: %v", err)
	}
}
