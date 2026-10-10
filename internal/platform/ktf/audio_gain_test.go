package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type audioGainProbe struct {
	countingSink
	gains map[backend.AudioHandle]uint16
	notes map[backend.AudioHandle][]uint8
	stops map[backend.AudioHandle]int
}

var _ backend.AudioGainSink = (*audioGainProbe)(nil)

func (sink *audioGainProbe) AudioEvent(sound backend.AudioHandle, event smaf.Event) {
	if event.Type == smaf.EventNoteOn {
		sink.notes[sound] = append(sink.notes[sound], event.Velocity)
	}
}

func (sink *audioGainProbe) StopSound(sound backend.AudioHandle) {
	sink.stops[sound]++
	delete(sink.gains, sound)
}

func (sink *audioGainProbe) SoundGain(sound backend.AudioHandle, gain uint16) {
	sink.gains[sound] = gain
}

func TestJavaAudioGainFollowsClipAndSharedDeviceControls(t *testing.T) {
	client, runtime := newTestRuntime(t)
	clock := NewManualClock(runtime.clockBase)
	client.clock = clock
	sink := &audioGainProbe{
		gains: make(map[backend.AudioHandle]uint16),
		notes: make(map[backend.AudioHandle][]uint8),
		stops: make(map[backend.AudioHandle]int),
	}
	client.audio = backend.NewAudio(sink)
	vm := client.JVM()
	const clipClass = "org/kwis/msp/media/Clip"
	const playerClass = "org/kwis/msp/media/Player"
	const volumeClass = "org/kwis/msp/media/Volume"
	for _, class := range []string{clipClass, playerClass, volumeClass} {
		if _, err := runtime.ensureJavaClass(class); err != nil {
			t.Fatal(err)
		}
	}
	invoke := func(object *jvm.Object, method, descriptor string, arguments ...jvm.Value) jvm.Value {
		t.Helper()
		value, err := vm.InvokeVirtual(object, method, descriptor, arguments...)
		if err != nil {
			t.Fatalf("Clip.%s: %v", method, err)
		}
		return value
	}
	static := func(class, method, descriptor string, arguments ...jvm.Value) jvm.Value {
		t.Helper()
		value, err := vm.InvokeStatic(class, method, descriptor, arguments...)
		if err != nil {
			t.Fatalf("%s.%s: %v", class, method, err)
		}
		return value
	}
	wantInt := func(value jvm.Value, want int32) {
		t.Helper()
		if got, err := value.Int32(); err != nil || got != want {
			t.Fatalf("guest result = %d, %v; want %d", got, err, want)
		}
	}
	sound := oneNoteSMAF()
	buffer := newByteArray(t, client, sound)
	newClip := func() *jvm.Object {
		t.Helper()
		class, err := runtime.ensureJavaClass(clipClass)
		if err != nil {
			t.Fatal(err)
		}
		_, clip, err := runtime.allocateAOTInstance(class)
		if err != nil {
			t.Fatal(err)
		}
		invoke(clip, "<init>", "(Ljava/lang/String;[B)V",
			jvm.ReferenceValue(vm.NewString("audio/mmf")), jvm.ReferenceValue(buffer))
		wantInt(invoke(clip, "getVolume", "()I"), 100)
		return clip
	}
	first, second := newClip(), newClip()
	setClip := func(clip *jvm.Object, level, want int32) {
		t.Helper()
		wantInt(invoke(clip, "setVolume", "(I)Z", jvm.IntValue(level)), 1)
		wantInt(invoke(clip, "getVolume", "()I"), want)
	}
	play := func(clip *jvm.Object) backend.AudioHandle {
		t.Helper()
		wantInt(static(playerClass, "play", "(Lorg/kwis/msp/media/Clip;Z)Z", jvm.ReferenceValue(clip), jvm.IntValue(1)), 1)
		return runtime.clip(clip).handle
	}
	setClip(first, -1, 0)
	setClip(first, 150, 100)
	setClip(first, 25, 25)
	static(volumeClass, "set", "(I)V", jvm.IntValue(80))
	wantInt(static(volumeClass, "get", "()I"), 80)
	firstSound, secondSound := play(first), play(second)
	if firstSound == 0 || firstSound == secondSound {
		t.Fatal("clips did not acquire distinct sound owners")
	}
	wantGains := func(firstGain, secondGain uint16) {
		t.Helper()
		for sound, want := range map[backend.AudioHandle]uint16{firstSound: firstGain, secondSound: secondGain} {
			if got, ok := sink.gains[sound]; !ok || got != want {
				t.Fatalf("sound %d gain = %d, present=%v; want %d", sound, got, ok, want)
			}
		}
	}
	clock.Advance(25 * time.Millisecond)
	client.serviceAudio()
	wantGains(2000, 8000)
	for _, sound := range []backend.AudioHandle{firstSound, secondSound} {
		if got := sink.notes[sound]; len(got) != 1 || got[0] != 100 {
			t.Fatalf("sound %d note velocities = %v, want one unscaled note", sound, got)
		}
	}
	firstStops, secondStops := sink.stops[firstSound], sink.stops[secondSound]
	setClip(first, 50, 50)
	wantGains(4000, 8000)
	setClip(first, 0, 0)
	wantGains(0, 8000)
	setClip(first, 50, 50)
	wantGains(4000, 8000)
	mediaCall(t, runtime, wipicMediaSetVolume, 40)
	wantInt(static(volumeClass, "get", "()I"), 40)
	wantGains(2000, 4000)
	static(volumeClass, "set", "(I)V", jvm.IntValue(-1))
	wantInt(static(volumeClass, "get", "()I"), 0)
	wantGains(0, 0)
	static(volumeClass, "set", "(I)V", jvm.IntValue(80))
	wantGains(4000, 8000)
	if sink.stops[firstSound] != firstStops || sink.stops[secondSound] != secondStops ||
		len(sink.notes[firstSound]) != 1 || len(sink.notes[secondSound]) != 1 ||
		!client.audio.Playing(firstSound) || !client.audio.Playing(secondSound) {
		t.Fatal("a volume change stopped or retriggered playback")
	}

	// Both guest refill routes must carry the saved level into a fresh decode.
	for _, refill := range []string{"setBuffer", "putData"} {
		oldSound := firstSound
		if refill == "setBuffer" {
			invoke(first, refill, "([BI)V", jvm.ReferenceValue(buffer), jvm.IntValue(int32(len(sound))))
		} else {
			value, err := vm.InvokeSpecial(first, "org/kwis/msp/media/BaseClip", refill, "([BII)I",
				jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(int32(len(sound))))
			if err != nil {
				t.Fatal(err)
			}
			wantInt(value, int32(len(sound)))
		}
		firstSound = play(first)
		if firstSound == oldSound || firstSound == secondSound {
			t.Fatalf("%s did not acquire a fresh independent decode", refill)
		}
		wantInt(invoke(first, "getVolume", "()I"), 50)
		clock.Advance(25 * time.Millisecond)
		client.serviceAudio()
		wantGains(4000, 8000)
		if got := sink.notes[firstSound]; len(got) != 1 || got[0] != 100 {
			t.Fatalf("%s reloaded note velocities = %v", refill, got)
		}
		if sink.stops[secondSound] != secondStops || !client.audio.Playing(secondSound) {
			t.Fatalf("%s interrupted the other clip", refill)
		}
	}
}
