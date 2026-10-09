package skt

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type audioOrderProgressSink struct {
	backend.AudioSink
	owner   backend.AudioHandle
	reached chan struct{}
	once    sync.Once
}

func (sink *audioOrderProgressSink) AudioEvent(owner backend.AudioHandle, event smaf.Event) {
	if owner == sink.owner && event.Type == smaf.EventNoteOn && event.Velocity != 0 {
		sink.once.Do(func() { close(sink.reached) })
	}
}

func (*audioOrderProgressSink) StopSound(backend.AudioHandle) {}

func newAudioOrderPlayer(t *testing.T, guest *Runtime) *jvm.Object {
	t.Helper()
	value, err := guest.newPlayer(guest.VM, audioStopSound(), "application/vnd.smaf")
	if err != nil {
		t.Fatal(err)
	}
	player, err := value.Reference()
	if err != nil || player == nil {
		t.Fatalf("new Player = %v, %v", player, err)
	}
	if _, err := guest.setPlayerLoopCount(nil, []jvm.Value{value, jvm.IntValue(-1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := guest.playerStart(nil, []jvm.Value{value}); err != nil {
		t.Fatal(err)
	}
	return player
}

func newAudioOrderClip(t *testing.T, guest *Runtime) *jvm.Object {
	t.Helper()
	clip, err := guest.VM.NewObject(skvm.RuntimeAudioClipClass, "(Ljava/lang/String;)V", jvm.ReferenceValue(guest.VM.NewString("mmf")))
	if err != nil {
		t.Fatal(err)
	}
	sound := audioStopSound()
	wipiStreamCall(t, guest, clip, "open", "([BII)V", jvm.ReferenceValue(jvm.NewByteArray(sound)), jvm.IntValue(0), jvm.IntValue(int32(len(sound))))
	wipiStreamCall(t, guest, clip, "loop", "()V")
	return clip
}

func waitAudioOrderCall(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("audio transition did not finish")
		return nil
	}
}

func requireAudioOrderPlayer(t *testing.T, object *jvm.Object, saved backend.AudioState, paused bool, position time.Duration) {
	t.Helper()
	player := object.Native.(*playerData)
	player.mu.Lock()
	defer player.mu.Unlock()
	for _, sound := range saved.Sounds {
		if sound.Handle != player.handle {
			continue
		}
		wantState := playerStarted
		if paused {
			wantState = playerPrefetched
		}
		if !sound.Playing || sound.Paused != paused || sound.Position != position || player.state != wantState ||
			player.completed != sound.Completed || player.mediaTime != position.Microseconds() {
			t.Fatalf("Player/backend progress differs: state=%d completed=%d time=%d; sound playing=%v paused=%v completed=%d position=%v",
				player.state, player.completed, player.mediaTime, sound.Playing, sound.Paused, sound.Completed, sound.Position)
		}
		return
	}
	t.Fatalf("Player handle %d is missing from captured audio", player.handle)
}

func TestAudioTimelineSerializesOtherOwnersDuringPlayerStop(t *testing.T) {
	for _, competitor := range []string{"Player query", "raw clip pause"} {
		t.Run(competitor, func(t *testing.T) {
			fixture := newMediaLifecycleFixture(t, -1)
			fixture.tick(0)
			guest, first := fixture.runtime, fixture.player()
			second := newAudioOrderPlayer(t, guest)
			clip := newAudioOrderClip(t, guest)
			guest.AdvanceAudio()
			sink := &audioOrderProgressSink{
				AudioSink: backend.NewRecordingSink(nil), owner: first.Native.(*playerData).handle, reached: make(chan struct{}),
			}
			guest.AttachAudioSink(sink)

			// Hold the notification queue after global progress, before stop can
			// pause its owner. The backend lock is then free, but the Player's
			// query-and-pause transaction must still exclude other owners.
			guest.mediaMu.Lock()
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(guest.mediaMu.Unlock) }
			defer release()
			fixture.clock.advance(450 * time.Millisecond)
			stopped := make(chan error, 1)
			go func() {
				_, err := guest.playerStop(nil, []jvm.Value{jvm.ReferenceValue(first)})
				stopped <- err
			}()
			select {
			case <-sink.reached:
			case <-time.After(5 * time.Second):
				t.Fatal("stop never advanced the first repeated pass")
			}
			// Capture waits for the global backend pass to release its mutex.
			// A competing operation can now reach it unless the transaction is held.
			before, err := guest.audioTimeline().CaptureState()
			if err != nil || len(before.Sounds) != 3 || before.Sounds[0].Completed != 1 || before.Sounds[0].Position != 50*time.Millisecond {
				t.Fatalf("stop did not reach the queue barrier: %+v, %v", before.Sounds, err)
			}
			// Cross another loop origin: an unguarded peer would move the
			// first owner's start to 800 ms before its pause at 450 ms.
			fixture.clock.advance(400 * time.Millisecond)
			competing := make(chan error, 1)
			entered := make(chan struct{})
			go func() {
				close(entered)
				var err error
				if competitor == "Player query" {
					_, err = guest.playerMediaTime(nil, []jvm.Value{jvm.ReferenceValue(second)})
				} else {
					_, err = guest.audioClipAction("pause")(nil, []jvm.Value{jvm.ReferenceValue(clip)})
				}
				competing <- err
			}()
			<-entered
			var crossed bool
			var competingErr error
			select {
			case competingErr = <-competing:
				crossed = true
			case <-time.After(100 * time.Millisecond):
			}
			release()
			stopErr := waitAudioOrderCall(t, stopped)
			if !crossed {
				competingErr = waitAudioOrderCall(t, competing)
			}
			if crossed || stopErr != nil || competingErr != nil {
				t.Fatalf("owner crossed unfinished stop=%v; stop=%v competitor=%v", crossed, stopErr, competingErr)
			}
			guest.AdvanceAudio()
			if guest.State() != StateActive {
				t.Fatalf("Host advancement changed lifecycle state to %s", guest.State())
			}
			saved, err := guest.audioTimeline().CaptureState()
			if err != nil {
				t.Fatal(err)
			}
			requireAudioOrderPlayer(t, first, saved, true, 50*time.Millisecond)
			requireAudioOrderPlayer(t, second, saved, false, 50*time.Millisecond)
			if paused := guest.audioTimeline().Paused(clip.Native.(*audioClipData).handle); paused != (competitor == "raw clip pause") {
				t.Fatalf("raw clip pause state = %v", paused)
			}
		})
	}
}

func TestAudioTimelineConcurrentPlayersRawClipAndHost(t *testing.T) {
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	guest := wipiStreamRuntime(t, store)
	clock := installMediaPauseClock(guest, newMediaPauseProbe())
	guest.AttachAudioSink(nil)
	players := []*jvm.Object{newAudioOrderPlayer(t, guest), newAudioOrderPlayer(t, guest)}
	clip := newAudioOrderClip(t, guest)
	guest.AdvanceAudio()
	const rounds = 512
	start := make(chan struct{})
	errors := make(chan error, 4)
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for i := 0; i < rounds; i++ {
			clock.advance(7 * time.Millisecond)
			guest.AdvanceAudio()
			if guest.State() != StateActive {
				errors <- fmt.Errorf("Host advancement changed lifecycle state to %s", guest.State())
				return
			}
			runtime.Gosched()
		}
	})
	for index, player := range players {
		workers.Go(func() {
			<-start
			for round := 0; round < rounds; round++ {
				for _, method := range []jvm.NativeMethod{guest.playerMediaTime, guest.playerStop, guest.playerStart, guest.setPlayerMediaTime} {
					if _, err := method(nil, []jvm.Value{jvm.ReferenceValue(player), jvm.LongValue(0)}); err != nil {
						errors <- fmt.Errorf("Player %d transition at round %d: %w", index, round, err)
						return
					}
					runtime.Gosched()
				}
			}
		})
	}
	workers.Go(func() {
		<-start
		for round := 0; round < rounds; round++ {
			for _, method := range []string{"pause", "resume", "stop", "loop"} {
				if _, err := guest.VM.InvokeVirtual(clip, method, "()V"); err != nil {
					errors <- fmt.Errorf("raw clip %s at round %d: %w", method, round, err)
					return
				}
				runtime.Gosched()
			}
			if round%16 == 0 {
				if _, err := guest.playTone(nil, []jvm.Value{jvm.IntValue(72), jvm.IntValue(1), jvm.IntValue(50)}); err != nil {
					errors <- fmt.Errorf("tone at round %d: %w", round, err)
					return
				}
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
	// Finish with different, independently checkable owner states. Rewind
	// each at the same frozen clock, then stop only the first after 40 ms.
	for _, player := range players {
		for _, method := range []jvm.NativeMethod{guest.playerStop, guest.setPlayerMediaTime, guest.playerStart} {
			if _, err := method(nil, []jvm.Value{jvm.ReferenceValue(player), jvm.LongValue(0)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	wipiStreamCall(t, guest, clip, "stop", "()V")
	clock.advance(40 * time.Millisecond)
	guest.AdvanceAudio()
	if _, err := guest.playerStop(nil, []jvm.Value{jvm.ReferenceValue(players[0])}); err != nil {
		t.Fatal(err)
	}
	clock.advance(60 * time.Millisecond)
	guest.AdvanceAudio()
	saved, err := guest.audioTimeline().CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	requireAudioOrderPlayer(t, players[0], saved, true, 40*time.Millisecond)
	requireAudioOrderPlayer(t, players[1], saved, false, 100*time.Millisecond)
	if guest.audioTimeline().Playing(clip.Native.(*audioClipData).handle) || guest.audioTimeline().Paused(clip.Native.(*audioClipData).handle) {
		t.Fatal("stopped raw clip retained playback")
	}
	checkpoint, err := guest.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatalf("concurrent audio state could not be captured: %v", err)
	}
	prepared, err := PrepareJavaCheckpoint(canvasJAR, checkpoint, Options{})
	if err != nil {
		t.Fatalf("concurrent audio checkpoint could not be prepared: %v", err)
	}
	prepared.Discard()
}
