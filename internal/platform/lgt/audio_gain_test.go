package lgt

import (
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

type audioGainProbe struct {
	recordingSink
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

func TestJavaAndCAudioGainShareDeviceAndKeepClipLevels(t *testing.T) {
	sink := &audioGainProbe{
		gains: make(map[backend.AudioHandle]uint16),
		notes: make(map[backend.AudioHandle][]uint8),
		stops: make(map[backend.AudioHandle]int),
	}
	client := mediaClient(t, sink)
	javaCall := func(member string, arguments ...uint32) uint32 {
		t.Helper()
		method, ok := javaPlatformMethods[member]
		if !ok || method.Words != len(arguments) {
			t.Fatalf("Java media method %s has no matching guest entry", member)
		}
		thread := armcore.NewThread(armcore.NewContext())
		for index, value := range arguments {
			if err := thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
		}
		class, called, _ := strings.Cut(member, ".")
		if err := client.callJavaMethod(t.Context(), thread, class, called, method); err != nil {
			t.Fatal(err)
		}
		answer, err := thread.Register(0)
		if err != nil {
			t.Fatal(err)
		}
		return answer
	}
	class, err := client.preparePlatformJavaClass(javaClipClass)
	if err != nil {
		t.Fatal(err)
	}
	javaClip, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	kind, err := client.newJavaString("audio/mmf")
	if err != nil {
		t.Fatal(err)
	}
	sound := oneNoteSound(t)
	arrayClass, err := client.javaArrayType(1, "B", 1)
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.allocateJavaArray(arrayClass.Object, uint32(len(sound)))
	if err != nil {
		t.Fatal(err)
	}
	block, _, err := client.javaArrayBlock(array)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.core.Memory().Write(block+javaArrayLengthWords*4, sound); err != nil {
		t.Fatal(err)
	}
	javaCall(javaClipClass+".<init>(Ljava/lang/String;[B)V", javaClip, kind, array)
	cClip := callSlot(t, client, slotClipCreate, 0, uint32(len(sound)), 0)
	cData := writeGuest(t, client, sound)
	callSlot(t, client, slotClipPutData, cClip, cData, uint32(len(sound)))
	for _, clip := range []uint32{javaClip, cClip} {
		if got := callSlot(t, client, slotClipGetVolume, clip); got != 100 {
			t.Fatalf("clip %#x default volume = %d, want 100", clip, got)
		}
	}
	setJavaClip := func(level uint32) {
		t.Helper()
		if got := javaCall(javaClipClass+".setVolume(I)Z", javaClip, level); got != javaTrue {
			t.Fatalf("Java clip setVolume = %d", got)
		}
		if got := callSlot(t, client, slotClipGetVolume, javaClip); got != level {
			t.Fatalf("Java clip volume read through C = %d, want %d", got, level)
		}
	}
	setCClip := func(level uint32) {
		t.Helper()
		if got := int32(callSlot(t, client, slotClipSetVolume, cClip, level)); got != wipiSuccess {
			t.Fatalf("C clip setVolume = %d", got)
		}
		if got := callSlot(t, client, slotClipGetVolume, cClip); got != level {
			t.Fatalf("C clip volume = %d, want %d", got, level)
		}
	}
	setJavaDevice := func(level uint32) {
		t.Helper()
		javaCall("org/kwis/msp/media/Volume.set(I)V", level)
		if got := callSlot(t, client, slotGetVolume); got != level {
			t.Fatalf("Java device volume read through C = %d, want %d", got, level)
		}
	}
	play := func() (backend.AudioHandle, backend.AudioHandle) {
		t.Helper()
		if got := javaCall("org/kwis/msp/media/Player.play(Lorg/kwis/msp/media/Clip;Z)Z", javaClip, 1); got != javaTrue {
			t.Fatalf("Java play = %d", got)
		}
		if got := int32(callSlot(t, client, slotClipPlay, cClip, 1)); got != wipiSuccess {
			t.Fatalf("C play = %d", got)
		}
		return client.clips[javaClip].handle, client.clips[cClip].handle
	}
	setJavaClip(40)
	setCClip(60)
	setJavaDevice(50)
	javaSound, cSound := play()
	if javaSound == 0 || javaSound == cSound {
		t.Fatal("Java and C clips did not acquire distinct sound owners")
	}
	wantGains := func(javaGain, cGain uint16) {
		t.Helper()
		for sound, want := range map[backend.AudioHandle]uint16{javaSound: javaGain, cSound: cGain} {
			if got, ok := sink.gains[sound]; !ok || got != want {
				t.Fatalf("sound %d gain = %d, present=%v; want %d", sound, got, ok, want)
			}
		}
	}
	client.clock.advance(25 * time.Millisecond)
	client.serviceAudio()
	wantGains(2000, 3000)
	for _, sound := range []backend.AudioHandle{javaSound, cSound} {
		if got := sink.notes[sound]; len(got) != 1 || got[0] != 100 {
			t.Fatalf("sound %d note velocities = %v, want one unscaled note", sound, got)
		}
	}
	javaStops, cStops := sink.stops[javaSound], sink.stops[cSound]
	setJavaClip(20)
	wantGains(1000, 3000)
	setCClip(80)
	wantGains(1000, 4000)
	callSlot(t, client, slotSetVolume, 25)
	if got := callSlot(t, client, slotGetVolume); got != 25 {
		t.Fatalf("C device volume = %d, want 25", got)
	}
	wantGains(500, 2000)
	setJavaDevice(100)
	wantGains(2000, 8000)
	callSlot(t, client, slotSetMuteState, 0x7fffffff, 1)
	callSlot(t, client, slotSetSourceVolume, 0x7fffffff, 0)
	wantGains(2000, 8000)
	if got := callSlot(t, client, slotGetVolume); got != 100 {
		t.Fatalf("unknown source changed device volume to %d", got)
	}
	if sink.stops[javaSound] != javaStops || sink.stops[cSound] != cStops ||
		len(sink.notes[javaSound]) != 1 || len(sink.notes[cSound]) != 1 ||
		!client.audio.Playing(javaSound) || !client.audio.Playing(cSound) {
		t.Fatal("a volume change stopped or retriggered Java or C playback")
	}

	// Clearing an active clip releases its decode before the guest refills it.
	oldJava, oldC := javaSound, cSound
	javaCall("org/kwis/msp/media/BaseClip.clearData()V", javaClip)
	if _, loaded := client.audio.Length(oldJava); loaded || sink.stops[oldJava] != javaStops+1 {
		t.Fatal("clearing the active Java clip left its old sound loaded or uncancelled")
	}
	if !client.audio.Playing(cSound) || sink.stops[cSound] != cStops || sink.gains[cSound] != 8000 {
		t.Fatal("clearing the Java clip interrupted the C clip")
	}
	if got := javaCall("org/kwis/msp/media/BaseClip.setBuffer([BI)Z", javaClip, array, uint32(len(sound))); got != javaTrue {
		t.Fatalf("Java refill = %d", got)
	}
	callSlot(t, client, slotClipClearData, cClip)
	if _, loaded := client.audio.Length(oldC); loaded || sink.stops[oldC] != cStops+1 {
		t.Fatal("clearing the active C clip left its old sound loaded or uncancelled")
	}
	callSlot(t, client, slotClipPutData, cClip, cData, uint32(len(sound)))
	javaSound, cSound = play()
	if javaSound == oldJava || cSound == oldC || javaSound == cSound {
		t.Fatal("refilled clips did not acquire fresh independent decodes")
	}
	client.clock.advance(25 * time.Millisecond)
	client.serviceAudio()
	wantGains(2000, 8000)
	for _, sound := range []backend.AudioHandle{javaSound, cSound} {
		if got := sink.notes[sound]; len(got) != 1 || got[0] != 100 {
			t.Fatalf("reloaded sound %d note velocities = %v", sound, got)
		}
	}
}
