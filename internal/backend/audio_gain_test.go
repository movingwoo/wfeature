package backend

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type gainAudioProbe struct {
	ownedAudioProbe
	gains      map[AudioHandle]uint16
	eventGains []observedAudioGain
}

type observedAudioGain struct {
	value uint16
	known bool
}

func (sink *gainAudioProbe) AudioEvent(sound AudioHandle, event smaf.Event) {
	sink.ownedAudioProbe.AudioEvent(sound, event)
	gain, known := sink.gains[sound]
	sink.eventGains = append(sink.eventGains, observedAudioGain{gain, known})
}

func (sink *gainAudioProbe) StopSound(sound AudioHandle) {
	sink.ownedAudioProbe.StopSound(sound)
	delete(sink.gains, sound)
}

func (sink *gainAudioProbe) SoundGain(sound AudioHandle, gain uint16) {
	if sink.gains == nil {
		sink.gains = make(map[AudioHandle]uint16)
	}
	sink.gains[sound] = gain
}

func TestAudioDeviceGainUpdatesHeldNotesAndPCMWithoutRestart(t *testing.T) {
	sink := &gainAudioProbe{}
	now := time.Unix(1, 0)
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	handle := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{100, -100, 50, -50}},
		{Time: 1000, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	before := len(sink.events)
	for _, level := range []int{50, 0, 25, 100} {
		audio.SetVolume(level)
		if gain, ok := sink.gains[handle]; !ok || gain != uint16(level*100) {
			t.Fatalf("live gain = %d, present %v, want %d", gain, ok, level*100)
		}
		if len(sink.events) != before {
			t.Fatal("volume change restarted or released the held output")
		}
		saved := captureOwnedAudio(t, audio)
		if len(saved.Output.Notes) != 1 || len(saved.Output.Waves) != 1 || !audio.Playing(handle) {
			t.Fatal("volume change discarded playback needed after unmute")
		}
	}
}

func TestAudioGainReachesALateAttachedSinkBeforeFutureEvents(t *testing.T) {
	audio := NewAudio(nil)
	handle := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 20, Type: smaf.EventNoteOn, Note: 67, Velocity: 100},
		{Time: 1000, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	if err := audio.SetSoundVolume(handle, 25); err != nil {
		t.Fatal(err)
	}
	audio.SetVolume(50)
	sink := &gainAudioProbe{}
	audio.SetSink(sink)
	if sink.gains[handle] != 1250 || len(sink.events) != 0 {
		t.Fatal("late attachment lost gain or replayed unrequested output")
	}
	audio.Advance(20 * time.Millisecond)
	if len(sink.eventGains) != 1 || sink.eventGains[0] != (observedAudioGain{1250, true}) {
		t.Fatalf("future event did not inherit gain: %v", sink.eventGains)
	}
}

func TestAudioClipGainKeepsConcurrentHeldOutput(t *testing.T) {
	sink := &gainAudioProbe{}
	now := time.Unix(1, 0)
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	audio.SetVolume(50)
	var handles []AudioHandle
	for _, level := range []int{25, 80} {
		handle, err := audio.LoadEvents([]smaf.Event{
			{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
			{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{10000, -10000, 5000, -5000}},
			{Time: 1000, Type: smaf.EventNoteOff, Note: 60},
			{Time: 2000, Type: smaf.EventEnd},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := audio.SetSoundVolume(handle, level); err != nil {
			t.Fatal(err)
		}
		handles = append(handles, handle)
	}
	if len(sink.gains) != 0 || len(sink.events) != 0 {
		t.Fatal("configuring an idle clip created output")
	}
	for _, handle := range handles {
		if err := audio.Play(handle, 0, false); err != nil {
			t.Fatal(err)
		}
	}
	audio.Advance(0)
	first, second := handles[0], handles[1]
	if sink.gains[first] != 1250 || sink.gains[second] != 4000 {
		t.Fatalf("clip and device levels did not multiply independently: %v", sink.gains)
	}
	before := captureOwnedAudio(t, audio)
	events, stops := len(sink.events), len(sink.stops)
	for _, step := range []struct {
		name          string
		change        func() error
		first, second uint16
		level         int
		muted         bool
	}{
		{"clip level", func() error { return audio.SetSoundVolume(first, 50) }, 2500, 4000, 50, false},
		{"mute", func() error { return audio.SetSoundMuted(first, true) }, 0, 4000, 50, true},
		{"level while muted", func() error { return audio.SetSoundVolume(first, 60) }, 0, 4000, 60, true},
		{"device while muted", func() error { audio.SetVolume(20); return nil }, 0, 1600, 60, true},
		{"unmute", func() error { return audio.SetSoundMuted(first, false) }, 1200, 1600, 60, false},
		{"negative clip level", func() error { return audio.SetSoundVolume(first, -1) }, 0, 1600, 0, false},
		{"high clip level", func() error { return audio.SetSoundVolume(first, 101) }, 2000, 1600, 100, false},
		{"negative device level", func() error { audio.SetVolume(-1); return nil }, 0, 0, 100, false},
		{"high device level", func() error { audio.SetVolume(101); return nil }, 10000, 8000, 100, false},
	} {
		t.Run(step.name, func(t *testing.T) {
			if err := step.change(); err != nil {
				t.Fatal(err)
			}
			if sink.gains[first] != step.first || sink.gains[second] != step.second {
				t.Fatalf("live gains = %v, want %d and %d", sink.gains, step.first, step.second)
			}
			if len(sink.events) != events || len(sink.stops) != stops {
				t.Fatal("gain change restarted or stopped output")
			}
			saved := captureOwnedAudio(t, audio)
			if saved.Sounds[0].Volume != step.level || saved.Sounds[0].Muted != step.muted ||
				!reflect.DeepEqual(saved.Output.Notes, before.Output.Notes) || !reflect.DeepEqual(saved.Output.Waves, before.Output.Waves) ||
				!audio.Playing(first) || !audio.Playing(second) {
				t.Fatal("gain change altered raw held output, clip state or playback")
			}
		})
	}
	for _, event := range sink.events {
		if event.event.Type == smaf.EventNoteOn && event.event.Velocity != 100 ||
			event.event.Type == smaf.EventWave && !slices.Equal(event.event.Wave, []int16{10000, -10000, 5000, -5000}) {
			t.Fatalf("gain-capable sink received pre-scaled output: %+v", event)
		}
	}
}

func TestAudioGainUpdatesPCMWhenScoreAlreadyEnded(t *testing.T) {
	sink := &gainAudioProbe{}
	now := time.Unix(1, 0)
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	ended := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 1, Wave: []int16{100, 200, 300, 400}},
		{Time: 10, Type: smaf.EventEnd},
	})
	active := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 1, Wave: []int16{400, 300, 200, 100}},
		{Time: 1000, Type: smaf.EventEnd},
	})
	audio.Advance(10 * time.Millisecond)
	now = now.Add(250 * time.Millisecond)
	before := captureOwnedAudio(t, audio)
	if audio.Playing(ended) || !audio.Playing(active) || len(before.Output.Notes) != 1 || len(before.Output.Waves) != 2 {
		t.Fatal("fixture did not retain an ended score's PCM beside another playing clip")
	}
	events, stops := len(sink.events), len(sink.stops)
	if err := audio.SetSoundVolume(ended, 40); err != nil {
		t.Fatal(err)
	}
	if sink.gains[ended] != 4000 || sink.gains[active] != AudioGainUnity {
		t.Fatalf("ended score's level changed the wrong output: %v", sink.gains)
	}
	if err := audio.SetSoundMuted(ended, true); err != nil {
		t.Fatal(err)
	}
	audio.SetVolume(50)
	if sink.gains[ended] != 0 || sink.gains[active] != 5000 {
		t.Fatalf("muted PCM tail or other clip has the wrong level: %v", sink.gains)
	}
	if err := audio.SetSoundMuted(ended, false); err != nil {
		t.Fatal(err)
	}
	after := captureOwnedAudio(t, audio)
	if sink.gains[ended] != 2000 || len(sink.events) != events || len(sink.stops) != stops || audio.Playing(ended) || !audio.Playing(active) ||
		!reflect.DeepEqual(after.Output.Notes, before.Output.Notes) || !reflect.DeepEqual(after.Output.Waves, before.Output.Waves) {
		t.Fatal("unmuting an ended score lost its PCM tail or restarted playback")
	}
}

func TestAudioRestartPreservesClipLevelAndMute(t *testing.T) {
	for _, muted := range []bool{false, true} {
		name := "audible"
		if muted {
			name = "muted"
		}
		t.Run(name, func(t *testing.T) {
			sink := &gainAudioProbe{}
			audio := NewAudio(sink)
			audio.SetVolume(80)
			handle := loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
				{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 1, Wave: []int16{100, -100}},
				{Time: 1000, Type: smaf.EventEnd},
			})
			audio.Advance(0)
			if err := audio.SetSoundVolume(handle, 37); err != nil {
				t.Fatal(err)
			}
			if err := audio.SetSoundMuted(handle, muted); err != nil {
				t.Fatal(err)
			}
			for _, stopFirst := range []bool{false, true} {
				if stopFirst {
					audio.Stop(handle)
				}
				if err := audio.Play(handle, 100*time.Millisecond, false); err != nil {
					t.Fatal(err)
				}
				start := len(sink.events)
				audio.Advance(100 * time.Millisecond)
				gain := uint16(2960)
				if muted {
					gain = 0
				}
				for i := start; i < len(sink.events); i++ {
					if !sink.eventGains[i].known || sink.eventGains[i].value != gain {
						t.Fatalf("restart delivered output before its gain: %+v", sink.eventGains[i])
					}
				}
				state := captureOwnedAudio(t, audio)
				if len(sink.events)-start != 2 || state.Sounds[0].Volume != 37 || state.Sounds[0].Muted != muted ||
					len(state.Output.Notes) != 1 || len(state.Output.Waves) != 1 || state.Output.Sounds[0].Gain != gain {
					t.Fatal("restart discarded clip settings or duplicated output")
				}
			}
		})
	}
}

func TestAudioGainCheckpointRestoresBeforeRawOutput(t *testing.T) {
	now := time.Unix(1, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	audio.SetVolume(50)
	var handles []AudioHandle
	for _, level := range []int{40, 70} {
		handle := loadOwnedAudio(t, audio, []smaf.Event{
			{Type: smaf.EventNoteOn, Channel: 2, Note: 64, Velocity: 96},
			{Type: smaf.EventWave, WaveChannels: 2, SamplingRate: 4, Wave: []int16{100, -100, 200, -200, 300, -300, 400, -400}},
			{Time: 1000, Type: smaf.EventNoteOff, Channel: 2, Note: 64},
			{Time: 2000, Type: smaf.EventEnd},
		})
		if err := audio.SetSoundVolume(handle, level); err != nil {
			t.Fatal(err)
		}
		handles = append(handles, handle)
	}
	if err := audio.SetSoundMuted(handles[1], true); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	now = now.Add(500 * time.Millisecond)
	saved := captureOwnedAudio(t, audio)
	data, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioState
	if err := DecodeCheckpointRecord(data, &decoded); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(1900000000, 0)
	sink := &gainAudioProbe{}
	restored, err := NewAudioFromStateWithClock(decoded, sink, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 0 || len(sink.gains) != 0 || len(sink.stops) != 0 {
		t.Fatal("detached restoration emitted audio or changed host levels")
	}
	now = now.Add(15 * time.Second)
	restored.ActivateOutputClock()
	restored.ActivateOutputClock()
	restored.ResumeOutput()
	if sink.gains[handles[0]] != 2000 || sink.gains[handles[1]] != 0 {
		t.Fatalf("restored levels or mute changed: %v", sink.gains)
	}
	notes, waves := 0, 0
	for i, event := range sink.events {
		if event.event.Type != smaf.EventNoteOn && event.event.Type != smaf.EventWave {
			continue
		}
		if !sink.eventGains[i].known || sink.eventGains[i].value != sink.gains[event.sound] {
			t.Fatalf("replay delivered output before its saved gain: %+v", event)
		}
		if event.event.Type == smaf.EventNoteOn {
			notes++
			if event.event.Velocity != 96 {
				t.Fatalf("restored note was attenuated before the host gain: %+v", event)
			}
		} else {
			waves++
			if !slices.Equal(event.event.Wave, []int16{300, -300, 400, -400}) {
				t.Fatalf("restored raw PCM did not resume at the captured frame: %+v", event)
			}
		}
	}
	if notes != 2 || waves != 2 || !reflect.DeepEqual(captureOwnedAudio(t, restored), saved) {
		t.Fatal("encoded restoration lost raw output or clip settings")
	}
	before := len(sink.events)
	restored.SetVolume(25)
	if err := restored.SetSoundMuted(handles[1], false); err != nil {
		t.Fatal(err)
	}
	if sink.gains[handles[0]] != 1000 || sink.gains[handles[1]] != 1750 || len(sink.events) != before ||
		!restored.Playing(handles[0]) || !restored.Playing(handles[1]) {
		t.Fatal("changing restored levels restarted output or lost the muted clip's saved level")
	}
	restored.Advance(time.Second)
	if len(sink.ofType(smaf.EventNoteOff)) != 2 {
		t.Fatal("restored notes did not keep their future release positions")
	}
}

func TestAudioGainRejectsMalformedCheckpoint(t *testing.T) {
	audio := NewAudio(nil)
	audio.SetVolume(80)
	handle := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 1000, Type: smaf.EventEnd},
	})
	if err := audio.SetSoundVolume(handle, 50); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	saved := captureOwnedAudio(t, audio)
	for _, test := range []struct {
		name   string
		damage func(*AudioState)
	}{
		{"old component", func(s *AudioState) { s.Version = 2 }},
		{"negative device volume", func(s *AudioState) { s.Volume = -1 }},
		{"high device volume", func(s *AudioState) { s.Volume = 101 }},
		{"negative clip volume", func(s *AudioState) { s.Sounds[0].Volume = -1 }},
		{"high clip volume", func(s *AudioState) { s.Sounds[0].Volume = 101 }},
		{"high output gain", func(s *AudioState) { s.Output.Sounds[0].Gain = AudioGainUnity + 1 }},
		{"inconsistent output gain", func(s *AudioState) { s.Output.Sounds[0].Gain-- }},
		{"audible muted clip", func(s *AudioState) { s.Sounds[0].Muted = true }},
		{"audible zero clip volume", func(s *AudioState) { s.Sounds[0].Volume = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := saved
			bad.Sounds = slices.Clone(saved.Sounds)
			bad.Output.Sounds = slices.Clone(saved.Output.Sounds)
			test.damage(&bad)
			sink := &gainAudioProbe{}
			if restored, err := NewAudioFromState(bad, sink); err == nil || restored != nil || len(sink.events) != 0 || len(sink.gains) != 0 {
				t.Fatalf("invalid gain state reached output or constructed audio: %v", err)
			}
		})
	}
}

func TestAudioGainLegacyScalingKeepsRawInputImmutable(t *testing.T) {
	now := time.Unix(1, 0)
	sink := &outputProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	audio.SetVolume(33)
	samples := []int16{32767, -32768, 12345, -12345, 1, -1}
	events := []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 127},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: samples},
		{Time: 250, Type: smaf.EventNoteOn, Note: 64, Velocity: 127},
		{Time: 250, Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: samples},
		{Time: 2000, Type: smaf.EventEnd},
	}
	original := cloneAudioEvents(events)
	handle := loadOwnedAudio(t, audio, events)
	if err := audio.SetSoundVolume(handle, 87); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	if len(sink.voices) != 1 || sink.voices[0].Velocity != 36 ||
		!reflect.DeepEqual(sink.waves, [][]int16{{9407, -9407, 3544, -3544, 0, 0}}) {
		t.Fatalf("legacy output did not receive the multiplied level: notes=%+v waves=%v", sink.voices, sink.waves)
	}
	if err := audio.SetSoundVolume(handle, 50); err != nil {
		t.Fatal(err)
	}
	audio.Advance(250 * time.Millisecond)
	if len(sink.voices) != 2 || sink.voices[1].Velocity != 20 || len(sink.waves) != 2 ||
		!slices.Equal(sink.waves[1], []int16{5406, -5406, 2036, -2036, 0, 0}) {
		t.Fatalf("future legacy output ignored a level change: notes=%+v waves=%v", sink.voices, sink.waves)
	}
	saved := captureOwnedAudio(t, audio)
	if !reflect.DeepEqual(events, original) || !reflect.DeepEqual(saved.Sounds[0].Events, original) || len(saved.Output.Notes) != 2 || len(saved.Output.Waves) != 2 {
		t.Fatal("legacy attenuation mutated borrowed input or discarded raw checkpoint output")
	}
	for _, note := range saved.Output.Notes {
		if note.Velocity != 127 {
			t.Fatalf("checkpoint retained an attenuated legacy note: %+v", note)
		}
	}
	for index, wave := range saved.Output.Waves {
		want := samples
		if index == 0 {
			want = samples[1:]
		} // The first wave is already one frame old.
		if !slices.Equal(wave.Samples, want) {
			t.Fatalf("checkpoint retained attenuated legacy PCM: %+v", wave)
		}
	}
}
