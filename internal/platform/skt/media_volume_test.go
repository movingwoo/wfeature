package skt

import (
	_ "embed"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/media-volume.jar
var mediaVolumeJAR []byte

func TestMIDPVolumeControlLookupRequiresRealizedPlayer(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	value, err := runtime.newPlayer(runtime.VM, audioStopSound(), "")
	if err != nil {
		t.Fatal(err)
	}
	player, _ := value.Reference()
	_, err = runtime.VM.InvokeVirtual(player, "getControl", "(Ljava/lang/String;)Ljavax/microedition/media/Control;",
		jvm.ReferenceValue(runtime.VM.NewString("VolumeControl")))
	if !runtime.VM.IsGuestException(err, "java/lang/IllegalStateException") {
		t.Fatalf("unrealized control lookup = %v, want IllegalStateException", err)
	}
	_, err = runtime.VM.InvokeVirtual(player, "getControls", "()[Ljavax/microedition/media/Control;")
	if !runtime.VM.IsGuestException(err, "java/lang/IllegalStateException") {
		t.Fatalf("unrealized control list = %v, want IllegalStateException", err)
	}
	wipiStreamCall(t, runtime, player, "realize", "()V")
	_, err = runtime.VM.InvokeVirtual(player, "getControl", "(Ljava/lang/String;)Ljavax/microedition/media/Control;", jvm.ReferenceValue(nil))
	if !runtime.VM.IsGuestException(err, "java/lang/IllegalArgumentException") {
		t.Fatalf("null control name = %v, want IllegalArgumentException", err)
	}
	control, _ := wipiStreamCall(t, runtime, player, "getControl", "(Ljava/lang/String;)Ljavax/microedition/media/Control;",
		jvm.ReferenceValue(runtime.VM.NewString("VolumeControl"))).Reference()
	if level, _ := wipiStreamCall(t, runtime, control, "getLevel", "()I").Int32(); level != 100 {
		t.Fatalf("default level = %d, want 100", level)
	}
	if muted, _ := wipiStreamCall(t, runtime, control, "isMuted", "()Z").Int32(); muted != 0 {
		t.Fatal("new control is muted")
	}
	wipiStreamCall(t, runtime, player, "close", "()V")
	for _, call := range []struct {
		receiver           *jvm.Object
		method, descriptor string
		arguments          []jvm.Value
	}{
		{player, "getControls", "()[Ljavax/microedition/media/Control;", nil},
		{player, "getControl", "(Ljava/lang/String;)Ljavax/microedition/media/Control;", []jvm.Value{jvm.ReferenceValue(runtime.VM.NewString("VolumeControl"))}},
		{control, "getLevel", "()I", nil},
		{control, "isMuted", "()Z", nil},
		{control, "setLevel", "(I)I", []jvm.Value{jvm.IntValue(50)}},
		{control, "setMute", "(Z)V", []jvm.Value{jvm.IntValue(1)}},
	} {
		_, err := runtime.VM.InvokeVirtual(call.receiver, call.method, call.descriptor, call.arguments...)
		if !runtime.VM.IsGuestException(err, "java/lang/IllegalStateException") {
			t.Errorf("closed %s = %v, want IllegalStateException", call.method, err)
		}
	}
}

type mediaVolumeSink struct {
	sktGainSink
	waves int
}

func (sink *mediaVolumeSink) AudioEvent(sound backend.AudioHandle, event smaf.Event) {
	sink.sktGainSink.AudioEvent(sound, event)
	if event.Type == smaf.EventWave {
		sink.waves++
	}
}

// Add an authored ten-second mono ADPCM tail to the existing MIDI fixture.
func mediaVolumeSound() []byte {
	chunk := func(tag string, data []byte) []byte {
		result := append([]byte(tag), 0, 0, 0, 0)
		binary.BigEndian.PutUint32(result[4:], uint32(len(data)))
		return append(result, data...)
	}
	track := []byte{0, 0, 0x10, 0, 2, 2}
	track = append(track, chunk("Atsq", []byte{0, 1, 100, 0, 0, 0, 0})...)
	track = append(track, chunk("Awa\x01", make([]byte, 20000))...)
	sound := audioStopSound()
	body := append(sound[8:len(sound)-2:len(sound)-2], chunk("ATR\x00", track)...)
	return chunk("MMMD", append(body, 0, 0))
}

func TestJavaMIDPVolumeControlLiveOutputAndCheckpoint(t *testing.T) {
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
	now := time.Unix(1000, 0)
	runtime.pace, runtime.paceStart = backend.NewSpeedClock(func() time.Time { return now }), now
	sink := &mediaVolumeSink{}
	runtime.AttachAudioSink(sink)
	call := func(method, descriptor string, arguments ...jvm.Value) jvm.Value {
		t.Helper()
		value, err := runtime.VM.InvokeStatic("MediaVolumeMIDlet", method, descriptor, arguments...)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return value
	}
	integer := func(field, descriptor string) int32 {
		t.Helper()
		value, err := runtime.VM.StaticField("MediaVolumeMIDlet", field, descriptor)
		if err != nil {
			t.Fatal(err)
		}
		result, _ := value.Int32()
		return result
	}
	player := func(field string) *jvm.Object {
		t.Helper()
		value, err := runtime.VM.StaticField("MediaVolumeMIDlet", field, "Ljavax/microedition/media/Player;")
		if err != nil {
			t.Fatal(err)
		}
		object, _ := value.Reference()
		return object
	}
	runPending := func() {
		t.Helper()
		if err := runtime.RunPending(); err != nil {
			t.Fatal(err)
		}
	}
	call("begin", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(mediaVolumeSound())))
	runtime.AdvanceAudio()
	runPending()
	first, second := player("first").Native.(*playerData).handle, player("second").Native.(*playerData).handle
	if sink.notes != 2 || sink.waves != 2 || sink.gains[first] != 10000 || sink.gains[second] != 10000 {
		t.Fatalf("guest playback setup = %+v", sink)
	}
	for _, step := range []struct {
		method, descriptor          string
		value, level, muted, events int32
		gain                        uint16
	}{
		{"setLevel", "(I)I", 25, 25, 0, 1, 2500},
		{"setMute", "(Z)V", 1, 25, 1, 2, 0},
		{"setMute", "(Z)V", 1, 25, 1, 2, 0},
		{"setLevel", "(I)I", 40, 40, 1, 3, 0},
		{"setMute", "(Z)V", 0, 40, 0, 4, 4000},
		{"setLevel", "(I)I", -1, 0, 0, 5, 0},
		{"setLevel", "(I)I", -20, 0, 0, 5, 0},
		{"setLevel", "(I)I", 101, 100, 0, 6, 10000},
		{"setLevel", "(I)I", 200, 100, 0, 6, 10000},
		{"setLevel", "(I)I", 35, 35, 0, 7, 3500},
		{"setMute", "(Z)V", 1, 35, 1, 8, 0},
	} {
		before := integer("events", "I")
		result := call(step.method, step.descriptor, jvm.IntValue(step.value))
		if step.method == "setLevel" {
			if got, _ := result.Int32(); got != step.level {
				t.Fatalf("setLevel(%d) = %d, want %d", step.value, got, step.level)
			}
		}
		level, _ := call("level", "()I").Int32()
		muted, _ := call("muted", "()Z").Int32()
		if integer("events", "I") != before {
			t.Fatalf("%s(%d) delivered a callback before RunPending", step.method, step.value)
		}
		runPending()
		if level != step.level || muted != step.muted || integer("events", "I") != step.events || integer("eventLevel", "I") != step.level || integer("eventMuted", "Z") != step.muted || sink.gains[first] != step.gain || sink.gains[second] != 10000 {
			t.Fatalf("%s(%d): level=%d muted=%d events=%d gains=%v", step.method, step.value, level, muted, integer("events", "I"), sink.gains)
		}
		if sink.notes != 2 || sink.waves != 2 || !runtime.audioTimeline().Playing(first) || !runtime.audioTimeline().Playing(second) {
			t.Fatal("volume change restarted or stopped sounding MIDI/PCM")
		}
	}
	saved, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkMalformedVolumeControls(t, saved)
	prepared, err := PrepareJavaCheckpoint(mediaVolumeJAR, saved, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	freshSink := &mediaVolumeSink{}
	restored, err := prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), freshSink)
	if err != nil {
		t.Fatal(err)
	}
	runtime, sink = restored, freshSink
	runtime.ResumeCheckpointOutput()
	call("checkIdentity", "()V")
	if sink.notes != 2 || sink.waves != 2 || sink.gains[first] != 0 || sink.gains[second] != 10000 {
		t.Fatalf("restored mixed output = %+v", sink)
	}
	call("setMute", "(Z)V", jvm.IntValue(0))
	if integer("events", "I") != 8 || sink.gains[first] != 3500 {
		t.Fatal("restored volume change did not update gain before its deferred callback")
	}
	runPending()
	if sink.gains[first] != 3500 || integer("events", "I") != 9 || integer("eventLevel", "I") != 35 || integer("eventMuted", "Z") != 0 {
		t.Fatal("restored control lost level, owner or callback identity")
	}
	// A listener may inspect and change the same control without deadlocking.
	if err := runtime.VM.SetStaticField("MediaVolumeMIDlet", "muteFromCallback", "Z", jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	call("setLevel", "(I)I", jvm.IntValue(20))
	if integer("events", "I") != 9 || sink.gains[first] != 2000 {
		t.Fatal("reentrant volume callback ran before RunPending")
	}
	runPending()
	if integer("events", "I") != 10 || integer("eventLevel", "I") != 20 || integer("eventMuted", "Z") != 0 || sink.gains[first] != 0 || sink.gains[second] != 10000 {
		t.Fatal("reentrant volume change was not deferred to the following pass")
	}
	runPending()
	if integer("events", "I") != 11 || integer("eventLevel", "I") != 20 || integer("eventMuted", "Z") != 1 || sink.gains[first] != 0 || sink.gains[second] != 10000 {
		t.Fatal("reentrant volume callback lost an event or muted another player")
	}
	call("closeFirst", "()V")
	if _, found := sink.gains[first]; found || sink.gains[second] != 10000 {
		t.Fatal("closing a controlled player cancelled another owner")
	}
	closed, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	closedPrepared, err := PrepareJavaCheckpoint(mediaVolumeJAR, closed, Options{})
	if err != nil {
		t.Fatalf("retained control of a closed Player was rejected: %v", err)
	}
	defer closedPrepared.Discard()
	_, err = closedPrepared.runtime.VM.InvokeStatic("MediaVolumeMIDlet", "level", "()I")
	if !closedPrepared.runtime.VM.IsGuestException(err, "java/lang/IllegalStateException") {
		t.Fatalf("restored closed control getter = %v", err)
	}
}

func checkMalformedVolumeControls(t *testing.T, original backend.Checkpoint) {
	t.Helper()
	for _, name := range []string{"missing owner", "different owner", "missing control", "wrong control class", "missing reference slot"} {
		t.Run(name, func(t *testing.T) {
			var state javaCheckpointState
			if err := backend.DecodeCheckpointRecord(original.Runtime, &state); err != nil {
				t.Fatal(err)
			}
			heap := &state.Threads.Heap
			var player, control *jvm.HeapPayloadState
			for i := range heap.Payloads {
				payload := &heap.Payloads[i]
				if payload.ExternalKind == "skt.volume-control" {
					control = payload
				}
				if payload.ExternalKind == "skt.player" && payload.References[0] != 0 {
					player = payload
				}
			}
			if player == nil || control == nil {
				t.Fatal("checkpoint lacks the tested control graph")
			}
			switch name {
			case "missing owner":
				control.References[0] = 0
			case "different owner":
				for i, object := range heap.Objects {
					if object.Class == "javax/microedition/media/Player" && uint32(i+1) != control.References[0] {
						control.References[0] = uint32(i + 1)
						break
					}
				}
			case "missing control":
				player.References[0] = 0
			case "wrong control class":
				heap.Objects[player.References[0]-1].Class = jvm.ObjectClass
			case "missing reference slot":
				control.References = nil
			}
			broken := original
			var err error
			broken.Runtime, err = backend.EncodeCheckpointRecord(state)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareJavaCheckpoint(mediaVolumeJAR, broken, Options{})
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil {
				t.Fatal("malformed control graph was accepted")
			}
		})
	}
}

func TestConcurrentMIDPVolumeControlsPreserveBackendLevel(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	value, err := runtime.newPlayer(runtime.VM, audioStopSound(), "")
	if err != nil {
		t.Fatal(err)
	}
	player, _ := value.Reference()
	wipiStreamCall(t, runtime, player, "realize", "()V")
	control, _ := wipiStreamCall(t, runtime, player, "getControl", "(Ljava/lang/String;)Ljavax/microedition/media/Control;",
		jvm.ReferenceValue(runtime.VM.NewString("VolumeControl"))).Reference()
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Go(func() {
			for level := int32(0); level < 100; level++ {
				if _, err := runtime.setPlayerVolumeLevel(nil, []jvm.Value{jvm.ReferenceValue(control), jvm.IntValue(level)}); err != nil {
					t.Error(err)
				}
				if _, err := runtime.setPlayerVolumeMute(nil, []jvm.Value{jvm.ReferenceValue(control), jvm.IntValue(level % 2)}); err != nil {
					t.Error(err)
				}
				if _, err := runtime.playerVolumeLevel(nil, []jvm.Value{jvm.ReferenceValue(control)}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	saved, err := runtime.audioTimeline().CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	level, _ := wipiStreamCall(t, runtime, control, "getLevel", "()I").Int32()
	muted, _ := wipiStreamCall(t, runtime, control, "isMuted", "()Z").Int32()
	if len(saved.Sounds) != 1 || int(level) != saved.Sounds[0].Volume || (muted != 0) != saved.Sounds[0].Muted {
		t.Fatal("guest and backend volume state diverged")
	}
}
