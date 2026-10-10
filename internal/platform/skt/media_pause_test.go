package skt

import (
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type mediaPauseClock struct{ instant atomic.Int64 }

func (clock *mediaPauseClock) now() time.Time { return time.Unix(0, clock.instant.Load()) }
func (clock *mediaPauseClock) advance(delta time.Duration) {
	clock.instant.Add(int64(delta))
}

type mediaPauseProbe struct {
	sktGainSink
	on, off, stops map[backend.AudioHandle]int
	resumed        map[backend.AudioHandle][]time.Duration
	waves          map[backend.AudioHandle][]smaf.Event
}

var _ backend.AudioResumeSink = (*mediaPauseProbe)(nil)

func newMediaPauseProbe() *mediaPauseProbe {
	return &mediaPauseProbe{
		on: make(map[backend.AudioHandle]int), off: make(map[backend.AudioHandle]int),
		stops: make(map[backend.AudioHandle]int), resumed: make(map[backend.AudioHandle][]time.Duration),
		waves: make(map[backend.AudioHandle][]smaf.Event),
	}
}

func (sink *mediaPauseProbe) AudioEvent(sound backend.AudioHandle, event smaf.Event) {
	sink.sktGainSink.AudioEvent(sound, event)
	switch event.Type {
	case smaf.EventNoteOn:
		sink.on[sound]++
	case smaf.EventNoteOff:
		sink.off[sound]++
	case smaf.EventWave:
		event.Wave = slices.Clone(event.Wave)
		sink.waves[sound] = append(sink.waves[sound], event)
	}
}

func (sink *mediaPauseProbe) StopSound(sound backend.AudioHandle) {
	sink.sktGainSink.StopSound(sound)
	sink.stops[sound]++
}

func (sink *mediaPauseProbe) ResumeNote(sound backend.AudioHandle, _, _, _ uint8, age time.Duration) {
	sink.resumed[sound] = append(sink.resumed[sound], age)
}

func installMediaPauseClock(runtime *Runtime, sink *mediaPauseProbe) *mediaPauseClock {
	clock := &mediaPauseClock{}
	clock.instant.Store(time.Unix(1000, 0).UnixNano())
	runtime.pace, runtime.paceStart = backend.NewSpeedClock(clock.now), clock.now()
	runtime.audio = backend.NewAudioWithClock(sink, clock.now)
	return clock
}

func TestMIDPPausedPlayerCheckpointRetainsGatePCMAndRepeat(t *testing.T) {
	archive, err := Open(mediaVolumeJAR)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore(nil)
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Destroy(true) }()
	sink := newMediaPauseProbe()
	clock := installMediaPauseClock(runtime, sink)
	if _, err := runtime.VM.InvokeStatic("MediaVolumeMIDlet", "begin", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(mediaVolumeSound()))); err != nil {
		t.Fatal(err)
	}
	player := func(field string) *jvm.Object {
		t.Helper()
		value, err := runtime.VM.StaticField("MediaVolumeMIDlet", field, "L"+midp.PlayerClass+";")
		if err != nil {
			t.Fatal(err)
		}
		object, _ := value.Reference()
		return object
	}
	runtime.AdvanceAudio()
	first, second := player("first"), player("second")
	handle, other := first.Native.(*playerData).handle, second.Native.(*playerData).handle
	if sink.on[handle] != 1 || len(sink.waves[handle]) != 1 {
		t.Fatal("fixture did not start MIDI and PCM together")
	}
	original := sink.waves[handle][0]
	clock.advance(100 * time.Millisecond)
	runtime.AdvanceAudio()
	stops := sink.stops[handle]
	wipiStreamCall(t, runtime, first, "stop", "()V")
	wipiStreamCall(t, runtime, first, "stop", "()V")
	if state, _ := wipiStreamCall(t, runtime, first, "getState", "()I").Int32(); state != playerPrefetched || sink.stops[handle] != stops+1 {
		t.Fatal("MIDP stop did not pause once into PREFETCHED")
	}
	if !runtime.audio.Playing(other) || sink.gains[other] != backend.AudioGainUnity {
		t.Fatal("pausing the first player changed the second owner")
	}
	// Only the paused output remains when captured; its age must stay frozen.
	wipiStreamCall(t, runtime, second, "close", "()V")
	clock.advance(30 * time.Second)
	runtime.AdvanceAudio()
	if sink.on[handle] != 1 || sink.off[handle] != 0 || len(sink.waves[handle]) != 1 {
		t.Fatal("paused player advanced while the clocks moved")
	}
	saved, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareJavaCheckpoint(mediaVolumeJAR, saved, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	elapsed := prepared.saved.Elapsed
	sink = newMediaPauseProbe()
	restored, err := prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), sink)
	if err != nil {
		t.Fatal(err)
	}
	runtime = restored
	clock.instant.Store(time.Unix(2000, 0).UnixNano())
	runtime.pace, runtime.paceStart = backend.NewSpeedClock(clock.now), clock.now().Add(-elapsed)
	first = player("first")
	runtime.ResumeCheckpointOutput()
	if len(sink.resumed[handle]) != 0 || len(sink.waves[handle]) != 0 || sink.on[handle] != 0 {
		t.Fatal("checkpoint adoption sounded the paused player")
	}
	clock.advance(5 * time.Second)
	runtime.AdvanceAudio()
	wipiStreamCall(t, runtime, first, "start", "()V")
	wipiStreamCall(t, runtime, first, "start", "()V")
	if ages := sink.resumed[handle]; len(ages) != 1 || ages[0] != 100*time.Millisecond || sink.on[handle] != 0 {
		t.Fatalf("restored start restarted the held note: ages=%v, attacks=%d", ages, sink.on[handle])
	}
	skip := int(original.SamplingRate) * int(original.WaveChannels) / 10
	if waves := sink.waves[handle]; len(waves) != 1 || !slices.Equal(waves[0].Wave, original.Wave[skip:]) {
		t.Fatal("restored start replayed or skipped the wrong PCM prefix")
	}
	clock.advance(299 * time.Millisecond)
	runtime.AdvanceAudio()
	if sink.off[handle] != 0 {
		t.Fatal("restored pause shortened the remaining note gate")
	}
	clock.advance(time.Millisecond)
	runtime.AdvanceAudio()
	if sink.off[handle] != 1 || sink.on[handle] != 1 || len(sink.waves[handle]) != 2 || !runtime.audio.Playing(handle) {
		t.Fatal("restored start lost the remaining 300 ms gate or repeat boundary")
	}
	wipiStreamCall(t, runtime, first, "deallocate", "()V")
	if state, _ := wipiStreamCall(t, runtime, first, "getState", "()I").Int32(); state != playerRealized || !runtime.audio.Paused(handle) {
		t.Fatal("deallocate did not retain the cursor while entering REALIZED")
	}
	wipiStreamCall(t, runtime, first, "setMediaTime", "(J)J", jvm.LongValue(0))
	wipiStreamCall(t, runtime, first, "start", "()V")
	runtime.AdvanceAudio()
	if sink.on[handle] != 2 || len(sink.resumed[handle]) != 1 {
		t.Fatal("explicit rewind retained the paused cursor instead of starting over")
	}
}

func TestWIPIPauseResumesAndStopCancelsTheRetainedCursor(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	sink := newMediaPauseProbe()
	clock := installMediaPauseClock(runtime, sink)
	clip, err := runtime.VM.NewObject(wipi.ClipClass, "(Ljava/lang/String;[B)V",
		jvm.ReferenceValue(runtime.VM.NewString("mmf")), jvm.ReferenceValue(jvm.NewByteArray(audioStopSound())))
	if err != nil {
		t.Fatal(err)
	}
	call := func(method string, want int32, extra ...jvm.Value) {
		t.Helper()
		descriptor := "(Lorg/kwis/msp/media/Clip;)Z"
		if method == "play" {
			descriptor = "(Lorg/kwis/msp/media/Clip;Z)Z"
		}
		value, err := runtime.VM.InvokeStatic(wipi.PlayerClass, method, descriptor, append([]jvm.Value{jvm.ReferenceValue(clip)}, extra...)...)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := value.Int32(); err != nil || got != want {
			t.Fatalf("WIPI Player.%s = %d, %v; want %d", method, got, err, want)
		}
	}
	handle := clip.Native.(*wipiClipData).player.Native.(*playerData).handle
	call("pause", 0)
	call("resume", 0)
	call("play", 1, jvm.IntValue(1))
	runtime.AdvanceAudio()
	clock.advance(100 * time.Millisecond)
	runtime.AdvanceAudio()
	call("pause", 1)
	call("pause", 0)
	clock.advance(10 * time.Second)
	runtime.AdvanceAudio()
	if sink.on[handle] != 1 || sink.off[handle] != 0 {
		t.Fatal("WIPI pause advanced its retained score")
	}
	call("resume", 1)
	call("resume", 0)
	if ages := sink.resumed[handle]; len(ages) != 1 || ages[0] != 100*time.Millisecond {
		t.Fatalf("WIPI resume note ages = %v, want [100ms]", ages)
	}
	clock.advance(299 * time.Millisecond)
	runtime.AdvanceAudio()
	if sink.off[handle] != 0 {
		t.Fatal("WIPI resume shortened the gate")
	}
	clock.advance(time.Millisecond)
	runtime.AdvanceAudio()
	if sink.off[handle] != 1 || sink.on[handle] != 2 {
		t.Fatal("WIPI resume lost its remaining gate or repeat mode")
	}
	call("pause", 1)
	call("stop", 1)
	call("resume", 0)
	call("play", 1, jvm.IntValue(0))
	runtime.AdvanceAudio()
	if sink.on[handle] != 3 || len(sink.resumed[handle]) != 1 {
		t.Fatal("WIPI stop retained a resumable cursor")
	}
	clock.advance(400 * time.Millisecond)
	runtime.AdvanceAudio()
	if runtime.audio.Playing(handle) || sink.on[handle] != 3 {
		t.Fatal("new WIPI play retained the cancelled repeat mode")
	}
}

func TestSKVMPauseInterruptsLoopAndResumeKeepsItsPosition(t *testing.T) {
	archive, err := Open(audioStopJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Destroy(true) }()
	sink := newMediaPauseProbe()
	clock := installMediaPauseClock(runtime, sink)
	value, err := runtime.VM.InvokeStatic(skvm.AudioSystemClass, "getAudioClip", "(Ljava/lang/String;)Lcom/skt/m/AudioClip;", jvm.ReferenceValue(runtime.VM.NewString("mmf")))
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	sound := audioStopSound()
	wipiStreamCall(t, runtime, object, "open", "([BII)V", jvm.ReferenceValue(jvm.NewByteArray(sound)), jvm.IntValue(0), jvm.IntValue(int32(len(sound))))
	if err := runtime.VM.SetStaticField("AudioStopMIDlet", "clip", "Lcom/skt/m/AudioClip;", value); err != nil {
		t.Fatal(err)
	}
	if err := runtime.VM.SetStaticField("AudioStopMIDlet", "loop", "Z", jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.VM.InvokeStatic("AudioStopMIDlet", "begin", "()V"); err != nil {
		t.Fatal(err)
	}
	clip := object.Native.(*audioClipData)
	waitFor := func(ready func() bool, message string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for !ready() {
			if time.Now().After(deadline) {
				t.Fatal(message)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitFor(func() bool {
		clip.mu.Lock()
		defer clip.mu.Unlock()
		return clip.playing != nil
	}, "SKVM loop did not enter its guest wait")
	handle := clip.handle
	runtime.AdvanceAudio()
	clock.advance(100 * time.Millisecond)
	runtime.AdvanceAudio()
	wipiStreamCall(t, runtime, object, "pause", "()V")
	waitFor(func() bool { return invokeFixtureInt(t, runtime, "AudioStopMIDlet", "result") != 0 }, "pause did not release the loop worker")
	if result := invokeFixtureInt(t, runtime, "AudioStopMIDlet", "result"); result != 2 {
		t.Fatalf("paused loop result = %d, want UserStopException", result)
	}
	clock.advance(10 * time.Second)
	runtime.AdvanceAudio()
	wipiStreamCall(t, runtime, object, "resume", "()V")
	if ages := sink.resumed[handle]; len(ages) != 1 || ages[0] != 100*time.Millisecond || sink.on[handle] != 1 {
		t.Fatalf("SKVM resume restarted the score: ages=%v, attacks=%d", ages, sink.on[handle])
	}
	clip.mu.Lock()
	waiting := clip.playing != nil
	clip.mu.Unlock()
	if waiting {
		t.Fatal("resume installed another blocking playback wait")
	}
	clock.advance(299 * time.Millisecond)
	runtime.AdvanceAudio()
	if sink.off[handle] != 0 {
		t.Fatal("SKVM resume shortened the gate")
	}
	clock.advance(time.Millisecond)
	runtime.AdvanceAudio()
	if sink.off[handle] != 1 || sink.on[handle] != 2 || !runtime.audio.Playing(handle) {
		t.Fatal("SKVM resume lost the remaining gate or loop")
	}
	wipiStreamCall(t, runtime, object, "stop", "()V")
	wipiStreamCall(t, runtime, object, "resume", "()V")
	if runtime.audio.Playing(handle) || runtime.audio.Paused(handle) {
		t.Fatal("SKVM resume revived an explicitly stopped clip")
	}
}

func TestMIDPPausedLoopCountChangePreservesRemainingGate(t *testing.T) {
	for _, change := range []struct {
		name          string
		before, after int32
	}{
		{"repeat to once", -1, 1},
		{"once to repeat", 1, -1},
	} {
		t.Run(change.name, func(t *testing.T) {
			runtime := wipiStreamRuntime(t, nil)
			sink := newMediaPauseProbe()
			clock := installMediaPauseClock(runtime, sink)
			value, err := runtime.newPlayer(runtime.VM, audioStopSound(), "")
			if err != nil {
				t.Fatal(err)
			}
			player, _ := value.Reference()
			handle := player.Native.(*playerData).handle
			wipiStreamCall(t, runtime, player, "setLoopCount", "(I)V", jvm.IntValue(change.before))
			wipiStreamCall(t, runtime, player, "start", "()V")
			runtime.AdvanceAudio()
			clock.advance(100 * time.Millisecond)
			runtime.AdvanceAudio()
			wipiStreamCall(t, runtime, player, "stop", "()V")
			wipiStreamCall(t, runtime, player, "setLoopCount", "(I)V", jvm.IntValue(change.after))
			clock.advance(10 * time.Second)
			wipiStreamCall(t, runtime, player, "start", "()V")
			if ages := sink.resumed[handle]; len(ages) != 1 || ages[0] != 100*time.Millisecond || sink.on[handle] != 1 {
				t.Fatal("changing the paused loop count rewound the note")
			}
			clock.advance(299 * time.Millisecond)
			runtime.AdvanceAudio()
			if sink.off[handle] != 0 {
				t.Fatal("changing the paused loop count shortened the note")
			}
			clock.advance(time.Millisecond)
			runtime.AdvanceAudio()
			wantNotes := 1
			if change.after == -1 {
				wantNotes = 2
			}
			if sink.off[handle] != 1 || sink.on[handle] != wantNotes || runtime.audio.Playing(handle) != (change.after == -1) {
				t.Fatalf("setLoopCount(%d) while paused did not replace repeat: on=%d off=%d playing=%t", change.after, sink.on[handle], sink.off[handle], runtime.audio.Playing(handle))
			}
		})
	}
}

func TestSKVMReplacingSoundReleasesPreviousLoopWait(t *testing.T) {
	archive, err := Open(audioStopJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Destroy(true) }()
	sink := newMediaPauseProbe()
	installMediaPauseClock(runtime, sink)
	value, err := runtime.VM.InvokeStatic(skvm.AudioSystemClass, "getAudioClip", "(Ljava/lang/String;)Lcom/skt/m/AudioClip;", jvm.ReferenceValue(runtime.VM.NewString("mmf")))
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	sound := audioStopSound()
	open := func(data []byte) error {
		_, err := runtime.VM.InvokeVirtual(object, "open", "([BII)V", jvm.ReferenceValue(jvm.NewByteArray(data)), jvm.IntValue(0), jvm.IntValue(int32(len(data))))
		return err
	}
	if err := open(sound); err != nil {
		t.Fatal(err)
	}
	if err := runtime.VM.SetStaticField("AudioStopMIDlet", "clip", "Lcom/skt/m/AudioClip;", value); err != nil {
		t.Fatal(err)
	}
	if err := runtime.VM.SetStaticField("AudioStopMIDlet", "loop", "Z", jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.VM.InvokeStatic("AudioStopMIDlet", "begin", "()V"); err != nil {
		t.Fatal(err)
	}
	clip := object.Native.(*audioClipData)
	deadline := time.Now().Add(time.Second)
	for {
		clip.mu.Lock()
		waiting := clip.playing != nil
		clip.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SKVM loop did not enter its guest wait")
		}
		time.Sleep(time.Millisecond)
	}
	old := clip.handle
	runtime.AdvanceAudio()
	if err := open([]byte("invalid media")); !runtime.VM.IsGuestException(err, skvm.UnsupportedFormatExceptionClass) {
		t.Fatalf("invalid replacement = %v", err)
	}
	clip.mu.Lock()
	preserved := clip.handle == old && clip.loop && clip.playing != nil
	clip.mu.Unlock()
	if !preserved || !runtime.audio.Playing(old) {
		t.Fatal("failed replacement disturbed the previous playback")
	}
	if err := open(sound); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for invokeFixtureInt(t, runtime, "AudioStopMIDlet", "result") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("replacement left the old loop worker waiting")
		}
		time.Sleep(time.Millisecond)
	}
	clip.mu.Lock()
	replaced := clip.handle != old && !clip.loop && !clip.paused && clip.playing == nil
	clip.mu.Unlock()
	if _, loaded := runtime.audio.Length(old); loaded || !replaced || invokeFixtureInt(t, runtime, "AudioStopMIDlet", "result") != 2 {
		t.Fatal("replacement did not release the old sound and interrupt its wait")
	}
	wipiStreamCall(t, runtime, object, "resume", "()V")
	if runtime.audio.Playing(clip.handle) {
		t.Fatal("resume started newly opened media")
	}
	wipiStreamCall(t, runtime, object, "play", "()V")
	runtime.AdvanceAudio()
	if sink.on[clip.handle] != 1 {
		t.Fatal("newly opened media did not play")
	}
}

func TestWIPIPlayWhilePausedRestartsWithRequestedRepeat(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	sink := newMediaPauseProbe()
	clock := installMediaPauseClock(runtime, sink)
	clip, err := runtime.VM.NewObject(wipi.ClipClass, "(Ljava/lang/String;[B)V",
		jvm.ReferenceValue(runtime.VM.NewString("mmf")), jvm.ReferenceValue(jvm.NewByteArray(audioStopSound())))
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, descriptor string, arguments ...jvm.Value) {
		t.Helper()
		value, err := runtime.VM.InvokeStatic(wipi.PlayerClass, method, descriptor, append([]jvm.Value{jvm.ReferenceValue(clip)}, arguments...)...)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := value.Int32(); err != nil || got != 1 {
			t.Fatalf("WIPI %s = %d, %v; want true", method, got, err)
		}
	}
	handle := clip.Native.(*wipiClipData).player.Native.(*playerData).handle
	call("play", "(Lorg/kwis/msp/media/Clip;Z)Z", jvm.IntValue(1))
	runtime.AdvanceAudio()
	clock.advance(100 * time.Millisecond)
	runtime.AdvanceAudio()
	call("pause", "(Lorg/kwis/msp/media/Clip;)Z")
	clock.advance(2 * time.Second)
	call("play", "(Lorg/kwis/msp/media/Clip;Z)Z", jvm.IntValue(0))
	runtime.AdvanceAudio()
	if sink.on[handle] != 2 || len(sink.resumed[handle]) != 0 {
		t.Fatal("WIPI play resumed the paused note instead of starting a fresh attack")
	}
	offs := sink.off[handle]
	clock.advance(399 * time.Millisecond)
	runtime.AdvanceAudio()
	if sink.off[handle] != offs {
		t.Fatal("fresh WIPI play reused the paused gate")
	}
	clock.advance(time.Millisecond)
	runtime.AdvanceAudio()
	if sink.off[handle] != offs+1 || sink.on[handle] != 2 || runtime.audio.Playing(handle) {
		t.Fatal("fresh WIPI play ignored its one-shot repeat setting")
	}
}

func TestMIDPDeallocateListenerCanRestartOrClosePlayer(t *testing.T) {
	for _, action := range []string{"start", "close"} {
		t.Run(action, func(t *testing.T) {
			runtime := wipiStreamRuntime(t, nil)
			// This isolated runtime drives callbacks without a MIDlet lifecycle.
			runtime.stateMu.Lock()
			runtime.state = StateActive
			runtime.stateMu.Unlock()
			installMediaPauseClock(runtime, newMediaPauseProbe())
			value, err := runtime.newPlayer(runtime.VM, audioStopSound(), "")
			if err != nil {
				t.Fatal(err)
			}
			player, _ := value.Reference()
			handle := player.Native.(*playerData).handle
			const listenerClass = "MediaPauseListener"
			const update = "(Ljavax/microedition/media/Player;Ljava/lang/String;Ljava/lang/Object;)V"
			if err := runtime.VM.DefineClass(jvm.ClassDefinition{
				Name: listenerClass, SuperName: jvm.ObjectClass, Access: jvm.AccessPublic,
				Interfaces: []string{midp.PlayerListenerClass},
				Methods:    []jvm.MethodDefinition{{Name: "playerUpdate", Descriptor: update, Access: jvm.AccessPublic | jvm.AccessNative}},
			}); err != nil {
				t.Fatal(err)
			}
			stopped, callbackState := 0, int32(-1)
			var delivered []string
			if err := runtime.VM.RegisterNative(listenerClass, "playerUpdate", update, func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
				event, _ := arguments[2].Reference()
				name, _ := jvm.StringText(event)
				delivered = append(delivered, name)
				if name != midp.PlayerEventStopped {
					return jvm.VoidValue(), nil
				}
				stopped++
				state, err := vm.InvokeVirtual(player, "getState", "()I")
				if err != nil {
					return jvm.VoidValue(), err
				}
				callbackState, _ = state.Int32()
				return vm.InvokeVirtual(player, action, "()V")
			}); err != nil {
				t.Fatal(err)
			}
			listener := &jvm.Object{ClassName: listenerClass, Fields: make(map[string]jvm.Value)}
			wipiStreamCall(t, runtime, player, "addPlayerListener", "(Ljavax/microedition/media/PlayerListener;)V", jvm.ReferenceValue(listener))
			wipiStreamCall(t, runtime, player, "start", "()V")
			runtime.AdvanceAudio()
			if len(delivered) != 0 {
				t.Fatal("start delivered a callback before RunPending")
			}
			if err := runtime.RunPending(); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(delivered, []string{midp.PlayerEventStarted}) {
				t.Fatalf("initial callbacks = %v", delivered)
			}
			wipiStreamCall(t, runtime, player, "deallocate", "()V")
			state, _ := wipiStreamCall(t, runtime, player, "getState", "()I").Int32()
			if stopped != 0 || callbackState != -1 || state != playerRealized || runtime.audio.Playing(handle) || !runtime.audio.Paused(handle) {
				t.Fatal("deallocate did not finish before its deferred callback")
			}
			if err := runtime.RunPending(); err != nil {
				t.Fatal(err)
			}
			state, _ = wipiStreamCall(t, runtime, player, "getState", "()I").Int32()
			wantState := playerStarted
			wantEvent := midp.PlayerEventStarted
			if action == "close" {
				wantState = playerClosed
				wantEvent = midp.PlayerEventClosed
			}
			if stopped != 1 || callbackState != playerRealized || state != wantState || runtime.audio.Playing(handle) != (action == "start") || runtime.audio.Paused(handle) {
				t.Fatalf("deallocate callback %s: events=%d callbackState=%d state=%d playing=%t paused=%t", action, stopped, callbackState, state, runtime.audio.Playing(handle), runtime.audio.Paused(handle))
			}
			if !slices.Equal(delivered, []string{midp.PlayerEventStarted, midp.PlayerEventStopped}) {
				t.Fatalf("reentrant callback was delivered in the same pass: %v", delivered)
			}
			if err := runtime.RunPending(); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(delivered, []string{midp.PlayerEventStarted, midp.PlayerEventStopped, wantEvent}) || stopped != 1 {
				t.Fatalf("reentrant callback order = %v", delivered)
			}
		})
	}
}
