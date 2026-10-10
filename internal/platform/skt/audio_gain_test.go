package skt

import (
	_ "embed"
	"sync"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/audio-gain.jar
var audioGainJAR []byte

type sktGainSink struct {
	scriptNoteSink
	gains map[backend.AudioHandle]uint16
	notes int
}

func TestConcurrentSKTGuestLevelsAgreeWithAudio(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	clip := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}
	if err := runtime.buildWIPIClip(runtime.VM, clip, "smaf", audioStopSound()); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Go(func() {
			for level := int32(0); level < 100; level++ {
				if _, err := runtime.setAudioVolume(nil, []jvm.Value{jvm.IntValue(level)}); err != nil {
					t.Error(err)
				}
				if _, err := runtime.wipiClipSetVolume(nil, []jvm.Value{jvm.ReferenceValue(clip), jvm.IntValue(level)}); err != nil {
					t.Error(err)
				}
				if _, err := runtime.wipiClipVolume(nil, []jvm.Value{jvm.ReferenceValue(clip)}); err != nil {
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
	device, _ := runtime.audioVolume(nil, nil)
	deviceLevel, _ := device.Int32()
	level, _ := runtime.wipiClipVolume(nil, []jvm.Value{jvm.ReferenceValue(clip)})
	clipLevel, _ := level.Int32()
	if saved.Volume != int(deviceLevel) || len(saved.Sounds) != 1 || saved.Sounds[0].Volume != int(clipLevel) {
		t.Fatalf("guest/audio levels diverged: device=%d clip=%d saved=%+v", deviceLevel, clipLevel, saved)
	}
}

func (sink *sktGainSink) AudioEvent(_ backend.AudioHandle, event smaf.Event) {
	if event.Type == smaf.EventNoteOn {
		sink.notes++
	}
}
func (sink *sktGainSink) StopSound(sound backend.AudioHandle) { delete(sink.gains, sound) }
func (sink *sktGainSink) SoundGain(sound backend.AudioHandle, value uint16) {
	if sink.gains == nil {
		sink.gains = make(map[backend.AudioHandle]uint16)
	}
	sink.gains[sound] = value
}

func TestJavaAudioGainFixtureAndCheckpoint(t *testing.T) {
	archive, err := Open(audioGainJAR)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore(nil)
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Destroy(true) }()
	sink := &sktGainSink{}
	runtime.AttachAudioSink(sink)
	volume, err := runtime.VM.InvokeStatic(skvm.AudioSystemClass, "getVolume", "()I")
	if got, _ := volume.Int32(); err != nil || got != 50 || runtime.audioTimeline().Volume() != 50 {
		t.Fatalf("default reported/output volume differ: %v, %v", volume, err)
	}
	call := func(method, descriptor string, arguments ...jvm.Value) jvm.Value {
		t.Helper()
		value, err := runtime.VM.InvokeStatic("AudioGainMIDlet", method, descriptor, arguments...)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return value
	}
	call("begin", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(audioStopSound())))
	runtime.AdvanceAudio()
	clip := func(field string) *jvm.Object {
		t.Helper()
		value, err := runtime.VM.StaticField("AudioGainMIDlet", field, "Lorg/kwis/msp/media/Clip;")
		if err != nil {
			t.Fatal(err)
		}
		object, err := value.Reference()
		if err != nil || object == nil {
			t.Fatalf("clip %s missing: %v", field, err)
		}
		return object
	}
	handle := func(object *jvm.Object) backend.AudioHandle {
		return object.Native.(*wipiClipData).player.Native.(*playerData).handle
	}
	first, second := handle(clip("first")), handle(clip("second"))
	if sink.gains[first] != 2500 || sink.gains[second] != 10000 || sink.notes != 2 {
		t.Fatalf("guest clip setup output = %v, notes=%d", sink.gains, sink.notes)
	}
	for _, step := range []struct {
		method        string
		level, result int32
		first, second uint16
	}{
		{"setClipLevel", -1, 0, 0, 10000},
		{"setDeviceLevel", 50, 50, 0, 5000},
		{"setClipLevel", 40, 40, 2000, 5000},
		{"setDeviceLevel", 500, 100, 4000, 10000},
		{"setClipLevel", 500, 100, 10000, 10000},
		{"setClipLevel", 25, 25, 2500, 10000},
	} {
		got, err := call(step.method, "(I)I", jvm.IntValue(step.level)).Int32()
		if err != nil || got != step.result || sink.gains[first] != step.first || sink.gains[second] != step.second {
			t.Fatalf("%s(%d) = %d, %v; gains=%v", step.method, step.level, got, err, sink.gains)
		}
		if sink.notes != 2 || !runtime.audioTimeline().Playing(first) || !runtime.audioTimeline().Playing(second) {
			t.Fatal("changing a level restarted or stopped the guest's clips")
		}
	}
	// The older void extension must behave like the standard boolean method.
	if _, err := runtime.VM.InvokeVirtual(clip("first"), "setVolume", "(I)V", jvm.IntValue(30)); err != nil || sink.gains[first] != 3000 {
		t.Fatalf("void volume extension = %v, gains=%v", err, sink.gains)
	}
	saved, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareJavaCheckpoint(audioGainJAR, saved, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	freshSink := &sktGainSink{}
	restored, err := prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), freshSink)
	if err != nil {
		t.Fatal(err)
	}
	runtime, sink = restored, freshSink
	runtime.ResumeCheckpointOutput()
	if got, _ := call("getClipLevel", "()I").Int32(); got != 30 || sink.gains[first] != 3000 || sink.gains[second] != 10000 || sink.notes != 2 {
		t.Fatalf("checkpoint changed guest level/output: level=%d, gains=%v, notes=%d", got, sink.gains, sink.notes)
	}
	call("setDeviceLevel", "(I)I", jvm.IntValue(0))
	if sink.gains[first] != 0 || sink.gains[second] != 0 {
		t.Fatal("restored device setter did not mute active clips")
	}
}
