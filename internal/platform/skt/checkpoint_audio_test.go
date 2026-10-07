package skt

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestJavaCheckpointResumesAudioWaitWithoutReplayingPlayback(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		for _, replaced := range []bool{false, true} {
			t.Run(map[bool]string{false: "play", true: "loop"}[repeat]+map[bool]string{false: "/stop", true: "/replaced"}[replaced], func(t *testing.T) {
				archive, err := Open(audioStopJAR)
				if err != nil {
					t.Fatal(err)
				}
				store, _ := backend.NewMemorySaveStore(nil)
				runtime, err := Start(archive, Options{SaveStore: store, Framebuffer: newTestFramebuffer(t, 4, 3), Speed: 0.1})
				if err != nil {
					t.Fatal(err)
				}
				defer runtime.transition("test cleanup", StateDestroyed)
				value, err := runtime.VM.InvokeStatic(skvm.AudioSystemClass, "getAudioClip", "(Ljava/lang/String;)Lcom/skt/m/AudioClip;", jvm.ReferenceValue(runtime.VM.NewString("mmf")))
				if err != nil {
					t.Fatal(err)
				}
				object, _ := value.Reference()
				sound := audioStopSound()
				if _, err = runtime.VM.InvokeVirtual(object, "open", "([BII)V", jvm.ReferenceValue(jvm.NewByteArray(sound)), jvm.IntValue(0), jvm.IntValue(int32(len(sound)))); err != nil {
					t.Fatal(err)
				}
				if err = runtime.VM.SetStaticField("AudioStopMIDlet", "clip", "Lcom/skt/m/AudioClip;", value); err != nil {
					t.Fatal(err)
				}
				loop := int32(0)
				if repeat {
					loop = 1
				}
				if err = runtime.VM.SetStaticField("AudioStopMIDlet", "loop", "Z", jvm.IntValue(loop)); err != nil {
					t.Fatal(err)
				}
				if _, err = runtime.VM.InvokeStatic("AudioStopMIDlet", "begin", "()V"); err != nil {
					t.Fatal(err)
				}
				clip := object.Native.(*audioClipData)
				deadline := time.Now().Add(time.Second)
				for {
					clip.mu.Lock()
					ready := clip.playing != nil
					clip.mu.Unlock()
					if ready {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("audio did not wait")
					}
					time.Sleep(time.Millisecond)
				}
				parked, release, err := runtime.parkCheckpoint(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				if replaced {
					clip.mu.Lock()
					runtime.startPlaying(clip)
					clip.mu.Unlock()
				}
				record, _, err := runtime.captureJavaState(parked)
				if err != nil {
					t.Fatal(err)
				}
				data, err := backend.EncodeCheckpointRecord(record)
				if err != nil {
					t.Fatal(err)
				}
				saved := backend.Checkpoint{Identity: archive.identity, Variant: backend.CheckpointSKTJava, Runtime: data}
				prepared, err := PrepareJavaCheckpoint(audioStopJAR, saved, Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.Discard()
				restored, err := prepared.Commit(t.Context(), nil, store, newTestFramebuffer(t, 4, 3), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer restored.transition("test cleanup", StateDestroyed)
				value, err = restored.VM.StaticField("AudioStopMIDlet", "clip", "Lcom/skt/m/AudioClip;")
				if err != nil {
					t.Fatal(err)
				}
				object, _ = value.Reference()
				fresh := object.Native.(*audioClipData)
				fresh.mu.Lock()
				generation := fresh.generation
				fresh.mu.Unlock()
				if generation != clip.generation {
					t.Fatal("restoration replayed playback")
				}
				if !replaced {
					if _, err = restored.VM.InvokeVirtual(object, "stop", "()V"); err != nil {
						t.Fatal(err)
					}
				}
				deadline = time.Now().Add(time.Second)
				for invokeFixtureInt(t, restored, "AudioStopMIDlet", "result") == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if got := invokeFixtureInt(t, restored, "AudioStopMIDlet", "result"); got != 2 {
					t.Fatalf("restored audio result=%d", got)
				}
				fresh.mu.Lock()
				stillPlaying := fresh.playing != nil
				fresh.mu.Unlock()
				if replaced && !stillPlaying {
					t.Fatal("old audio wait ended replacement playback")
				}
			})
		}
	}
}
