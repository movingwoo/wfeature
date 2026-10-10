package ktf

import (
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type audioPauseProbe struct {
	countingSink
	events []struct {
		sound backend.AudioHandle
		event smaf.Event
	}
	resumed []struct {
		sound                   backend.AudioHandle
		channel, note, velocity uint8
		age                     time.Duration
	}
	stopped []backend.AudioHandle
}

var _ backend.AudioResumeSink = (*audioPauseProbe)(nil)

func (sink *audioPauseProbe) AudioEvent(sound backend.AudioHandle, event smaf.Event) {
	event.Wave = slices.Clone(event.Wave)
	sink.events = append(sink.events, struct {
		sound backend.AudioHandle
		event smaf.Event
	}{sound, event})
}

func (sink *audioPauseProbe) StopSound(sound backend.AudioHandle) {
	sink.stopped = append(sink.stopped, sound)
}

func (sink *audioPauseProbe) ResumeNote(sound backend.AudioHandle, channel, note, velocity uint8, age time.Duration) {
	sink.resumed = append(sink.resumed, struct {
		sound                   backend.AudioHandle
		channel, note, velocity uint8
		age                     time.Duration
	}{sound, channel, note, velocity, age})
}

func (sink *audioPauseProbe) ofType(kind smaf.EventType) []smaf.Event {
	var events []smaf.Event
	for _, call := range sink.events {
		if call.event.Type == kind {
			events = append(events, call.event)
		}
	}
	return events
}

func newJavaAudioPauseClip(t *testing.T, client *Client, runtime *initializationRuntime) *jvm.Object {
	t.Helper()
	const clipClass = "org/kwis/msp/media/Clip"
	if _, err := runtime.ensureJavaClass("org/kwis/msp/media/Player"); err != nil {
		t.Fatal(err)
	}
	class, err := runtime.ensureJavaClass(clipClass)
	if err != nil {
		t.Fatal(err)
	}
	_, clip, err := runtime.allocateAOTInstance(class)
	if err != nil {
		t.Fatal(err)
	}
	vm := client.JVM()
	if _, err := vm.InvokeVirtual(clip, "<init>", "(Ljava/lang/String;[B)V",
		jvm.ReferenceValue(vm.NewString("audio/mmf")), jvm.ReferenceValue(newByteArray(t, client, oneNoteSMAF()))); err != nil {
		t.Fatal(err)
	}
	return clip
}

func javaAudioPauseCall(t *testing.T, client *Client, clip *jvm.Object, parameter, method string, want int32, repeat ...int32) {
	t.Helper()
	descriptor := "(Lorg/kwis/msp/media/" + parameter + ";"
	arguments := []jvm.Value{jvm.ReferenceValue(clip)}
	if len(repeat) != 0 {
		descriptor += "Z"
		arguments = append(arguments, jvm.IntValue(repeat[0]))
	}
	value, err := client.JVM().InvokeStatic("org/kwis/msp/media/Player", method, descriptor+")Z", arguments...)
	if err != nil {
		t.Fatalf("Player.%s(%s): %v", method, parameter, err)
	}
	if got, err := value.Int32(); err != nil || got != want {
		t.Fatalf("Player.%s(%s) = %d, %v; want %d", method, parameter, got, err, want)
	}
}

func TestJavaAudioPauseResumeTransitions(t *testing.T) {
	for _, parameter := range []string{"Clip", "BaseClip"} {
		t.Run(parameter, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			clock := NewManualClock(runtime.clockBase)
			client.clock = clock
			sink := &audioPauseProbe{}
			client.audio = backend.NewAudioWithClock(sink, clock.Now)
			clip := newJavaAudioPauseClip(t, client, runtime)
			call := func(method string, want int32, repeat ...int32) {
				t.Helper()
				javaAudioPauseCall(t, client, clip, parameter, method, want, repeat...)
			}
			for _, method := range []string{"pause", "resume", "stop"} {
				javaAudioPauseCall(t, client, nil, parameter, method, 0)
				call(method, 0)
			}
			call("play", 1, 0)
			clock.Advance(25 * time.Millisecond)
			client.serviceAudio()
			call("resume", 0)
			call("pause", 1)
			call("pause", 0)
			handle := runtime.clip(clip).handle
			if !client.audio.Paused(handle) || client.audio.Playing(handle) {
				t.Fatal("pause did not retain a stopped cursor")
			}
			clock.Advance(time.Second)
			client.serviceAudio()
			call("resume", 1)
			call("resume", 0)
			call("pause", 1)
			call("stop", 1)
			call("resume", 0)
			call("pause", 0)
			if client.audio.Paused(handle) || client.audio.Playing(handle) {
				t.Fatal("stop retained paused or active playback")
			}
			sink.events, sink.resumed = nil, nil
			clock.Advance(time.Second)
			client.serviceAudio()
			if len(sink.events) != 0 || len(sink.resumed) != 0 {
				t.Fatal("cancelled pause emitted output")
			}
			call("play", 1, 0)
			clock.Advance(100 * time.Millisecond)
			client.serviceAudio()
			call("pause", 0)
			call("resume", 0)
		})
	}
}

func TestJavaAudioPauseRetainsCursorPCMAndRepeat(t *testing.T) {
	client, runtime := newTestRuntime(t)
	clock := NewManualClock(runtime.clockBase)
	client.clock = clock
	sink := &audioPauseProbe{}
	client.audio = backend.NewAudioWithClock(sink, clock.Now)
	clip := newJavaAudioPauseClip(t, client, runtime)
	// An authored decoded score isolates the native bridge from SMAF encoding.
	handle, err := client.audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventNoteOn, Channel: 2, Note: 60, Velocity: 100},
		{Type: smaf.EventWave, WaveChannels: 2, SamplingRate: 4, Wave: []int16{10, -10, 20, -20, 30, -30, 40, -40}},
		{Time: 500, Type: smaf.EventNoteOff, Channel: 2, Note: 60},
		{Time: 600, Type: smaf.EventNoteOn, Channel: 2, Note: 62, Velocity: 90},
		{Time: 800, Type: smaf.EventNoteOff, Channel: 2, Note: 62},
		{Time: 1000, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := runtime.clip(clip)
	state.handle, state.loaded = handle, true
	call := func(method string, want int32, repeat ...int32) {
		t.Helper()
		javaAudioPauseCall(t, client, clip, "Clip", method, want, repeat...)
	}
	call("play", 1, 1)
	client.serviceAudio()
	clock.Advance(250 * time.Millisecond)
	call("pause", 1)
	if len(sink.stopped) == 0 || sink.stopped[len(sink.stopped)-1] != handle {
		t.Fatal("pause did not stop its owned output")
	}
	sink.events = nil
	clock.Advance(10 * time.Second)
	client.serviceAudio()
	if len(sink.events) != 0 || len(sink.resumed) != 0 {
		t.Fatal("paused score emitted output")
	}
	call("resume", 1)
	if len(sink.resumed) != 1 {
		t.Fatalf("resumed notes = %v, want one held note", sink.resumed)
	}
	note := sink.resumed[0]
	if note.sound != handle || note.channel != 2 || note.note != 60 || note.velocity != 100 || note.age != 250*time.Millisecond {
		t.Fatalf("resumed note = %+v, want the held note at age 250ms", note)
	}
	waves := sink.ofType(smaf.EventWave)
	if len(waves) != 1 || waves[0].WaveChannels != 2 || waves[0].SamplingRate != 4 ||
		!slices.Equal(waves[0].Wave, []int16{20, -20, 30, -30, 40, -40}) {
		t.Fatalf("resumed PCM = %+v, want the remaining stereo frames", waves)
	}
	sink.events = nil
	clock.Advance(249 * time.Millisecond)
	client.serviceAudio()
	if len(sink.ofType(smaf.EventNoteOff)) != 0 {
		t.Fatal("remaining note gate ended early")
	}
	clock.Advance(time.Millisecond)
	client.serviceAudio()
	if offs := sink.ofType(smaf.EventNoteOff); len(offs) != 1 || offs[0].Note != 60 {
		t.Fatalf("remaining note gate = %+v, want one note-off at 500ms", offs)
	}
	clock.Advance(500 * time.Millisecond)
	client.serviceAudio()
	notes, waves := sink.ofType(smaf.EventNoteOn), sink.ofType(smaf.EventWave)
	if len(notes) != 2 || notes[0].Note != 62 || notes[1].Note != 60 || len(waves) != 1 ||
		!slices.Equal(waves[0].Wave, []int16{10, -10, 20, -20, 30, -30, 40, -40}) || !client.audio.Playing(handle) {
		t.Fatalf("repeat lost its original boundary: notes=%+v waves=%+v", notes, waves)
	}
	if runtime.clip(clip).handle != handle {
		t.Fatal("pause or resume replaced the loaded clip")
	}
}
