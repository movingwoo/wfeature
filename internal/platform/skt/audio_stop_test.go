package skt

import (
	_ "embed"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/audio-stop.jar
var audioStopJAR []byte

func TestStoppedAudioDoesNotReportNaturalCompletion(t *testing.T) {
	for _, loop := range []bool{false, true} {
		mode := "play"
		if loop {
			mode = "loop"
		}
		for _, action := range []string{"stop", "close", "pause", "play", "finish"} {
			if loop && action == "finish" {
				continue
			}
			t.Run(mode+"/"+action, func(t *testing.T) {
				archive, err := Open(audioStopJAR)
				if err != nil {
					t.Fatal(err)
				}
				runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
				if err != nil {
					t.Fatal(err)
				}
				defer runtime.Destroy(true)
				value, err := runtime.VM.InvokeStatic(skvm.AudioSystemClass, "getAudioClip", "(Ljava/lang/String;)Lcom/skt/m/AudioClip;", jvm.ReferenceValue(runtime.VM.NewString("mmf")))
				if err != nil {
					t.Fatal(err)
				}
				clip, err := value.Reference()
				if err != nil {
					t.Fatal(err)
				}
				// An authored SMAF note with a 400 ms duration leaves time
				// to observe the registered wait before ending playback.
				sound := audioStopSound()
				if _, err := runtime.VM.InvokeVirtual(clip, "open", "([BII)V", jvm.ReferenceValue(jvm.NewByteArray(sound)), jvm.IntValue(0), jvm.IntValue(int32(len(sound)))); err != nil {
					t.Fatal(err)
				}
				if err := runtime.VM.SetStaticField("AudioStopMIDlet", "clip", "Lcom/skt/m/AudioClip;", value); err != nil {
					t.Fatal(err)
				}
				repeat := int32(0)
				if loop {
					repeat = 1
				}
				if err := runtime.VM.SetStaticField("AudioStopMIDlet", "loop", "Z", jvm.IntValue(repeat)); err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.VM.InvokeStatic("AudioStopMIDlet", "begin", "()V"); err != nil {
					t.Fatal(err)
				}
				data := clip.Native.(*audioClipData)
				deadline := time.Now().Add(time.Second)
				for {
					data.mu.Lock()
					waiting := data.playing != nil
					data.mu.Unlock()
					if waiting {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("audio worker never waited")
					}
					time.Sleep(time.Millisecond)
				}
				if got := invokeFixtureInt(t, runtime, "AudioStopMIDlet", "result"); got != 0 {
					t.Fatalf("result before stop = %d", got)
				}
				want := int32(2)
				if action == "finish" {
					want = 1
				} else if _, err := runtime.VM.InvokeVirtual(clip, action, "()V"); err != nil {
					t.Fatal(err)
				}
				deadline = time.Now().Add(time.Second)
				for {
					got := invokeFixtureInt(t, runtime, "AudioStopMIDlet", "result")
					if got != 0 {
						if got != want {
							t.Fatalf("result = %d, want %d (1 completed, 2 stopped, 3 other exception)", got, want)
						}
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("audio worker did not finish")
					}
					time.Sleep(time.Millisecond)
				}
			})
		}
	}
}

func audioStopSound() []byte {
	chunk := func(tag string, body []byte) []byte {
		n := uint32(len(body))
		result := append([]byte(tag), byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		return append(result, body...)
	}
	sequence := []byte{0, 0x90, 60, 100, 100, 0, 0xff, 0x2f, 0}
	track := append([]byte{2, 0, 2, 2}, make([]byte, 16)...)
	track = append(track, chunk("Mtsq", sequence)...)
	body := append(chunk("MTR\x00", track), 0, 0)
	return chunk("MMMD", body)
}
