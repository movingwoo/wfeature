package backend

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type outputProbe struct {
	recordingSink
	channels [16]AudioChannelState
	voices   []AudioVoiceState
	waves    [][]int16
}

func (sink *outputProbe) MIDINoteOn(channel, note, velocity uint8) {
	sink.recordingSink.MIDINoteOn(channel, note, velocity)
	sink.voices = append(sink.voices, AudioVoiceState{Channel: channel, Note: note, Velocity: velocity, StartedWith: sink.channels[channel]})
}
func (sink *outputProbe) MIDIProgramChange(channel, program uint8) {
	sink.recordingSink.MIDIProgramChange(channel, program)
	sink.channels[channel].Program = program
}
func (sink *outputProbe) MIDIControlChange(channel, control, value uint8) {
	sink.recordingSink.MIDIControlChange(channel, control, value)
	switch control {
	case 7:
		sink.channels[channel].Volume = value
	case 10:
		sink.channels[channel].Pan = value
	case 11:
		sink.channels[channel].Expression = value
	case 64:
		sink.channels[channel].Sustain = value
	}
}
func (sink *outputProbe) MIDIPitchBend(channel uint8, value uint16) {
	sink.recordingSink.MIDIPitchBend(channel, value)
	sink.channels[channel].Bend = value
}
func (sink *outputProbe) PlayWave(channels uint8, rate uint32, samples []int16) {
	sink.recordingSink.PlayWave(channels, rate, samples)
	sink.waves = append(sink.waves, slices.Clone(samples))
}

func TestAudioOutputRestoresEmittedLevelsAndSamplePosition(t *testing.T) {
	now := time.Unix(1700000000, 0)
	source := NewAudioWithClock(nil, func() time.Time { return now })
	handle, err := source.LoadEvents([]smaf.Event{
		{Type: smaf.EventProgramChange, Channel: 2, Program: 27},
		{Type: smaf.EventControlChange, Channel: 2, Control: 7, Value: 90},
		{Type: smaf.EventControlChange, Channel: 2, Control: 10, Value: 30},
		{Type: smaf.EventNoteOn, Channel: 2, Note: 64, Velocity: 80},
		{Time: 10, Type: smaf.EventWave, WaveChannels: 2, SamplingRate: 4, Wave: []int16{0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28, 30}},
		{Time: 20, Type: smaf.EventProgramChange, Channel: 2, Program: 50},
		{Time: 20, Type: smaf.EventControlChange, Channel: 2, Control: 7, Value: 40},
		{Time: 20, Type: smaf.EventPitchBend, Channel: 2, Bend: 9000},
		{Time: 80, Type: smaf.EventNoteOff, Channel: 2, Note: 64},
		{Time: 100, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	source.Advance(0)
	source.SetVolume(50)
	source.Advance(10 * time.Millisecond)
	source.SetVolume(25)
	source.Advance(20 * time.Millisecond)
	now = now.Add(500 * time.Millisecond)
	saved, err := source.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	data, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioState
	if err := DecodeCheckpointRecord(data, &decoded); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(1900000000, 0)
	sink := &outputProbe{}
	fresh, err := NewAudioFromStateWithClock(decoded, sink, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.calls) != 0 {
		t.Fatal("detached construction emitted audio")
	}
	decoded.Output.Waves[0].Samples[0] = 3000
	now = now.Add(15 * time.Second)
	fresh.ActivateOutputClock()
	fresh.ActivateOutputClock()
	fresh.ResumeOutput()
	wantVoice := AudioVoiceState{Channel: 2, Note: 64, Velocity: 80, StartedWith: AudioChannelState{Program: 27, Volume: 90, Expression: 127, Pan: 30, Bend: 8192}}
	if !reflect.DeepEqual(sink.voices, []AudioVoiceState{wantVoice}) || sink.channels[2] != saved.Output.Channels[2] {
		t.Fatalf("note-on settings or final channel settings changed: %+v, %+v", sink.voices, sink.channels[2])
	}
	if !reflect.DeepEqual(sink.waves, [][]int16{{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}}) {
		t.Fatalf("PCM tail or original emitted volume changed: %v", sink.waves)
	}
	again, err := fresh.CaptureState()
	if err != nil || !reflect.DeepEqual(again, saved) {
		t.Fatalf("reconstruction changed the execution/audio record: %v", err)
	}
	fresh.Advance(80 * time.Millisecond)
	if sink.count("off 2 64") != 1 {
		t.Fatal("restored voice did not receive its future note-off")
	}
	now = now.Add(2 * time.Second)
	again, err = fresh.CaptureState()
	if err != nil || len(again.Output.Waves) != 0 || len(again.Output.Notes) != 0 {
		t.Fatal("finished output was retained")
	}
}

func TestAudioOutputMatchesVoiceStealingAndExpiresPercussion(t *testing.T) {
	now := time.Unix(1700000000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	for note := uint8(40); note < 64; note++ {
		audio.sink.MIDINoteOn(0, note, 90)
	}
	// Retrigger at capacity steals the oldest key before replacing its own.
	audio.sink.MIDINoteOn(0, 63, 100)
	saved, err := audio.CaptureState()
	if err != nil || len(saved.Output.Notes) != 23 || saved.Output.Notes[0].Note != 41 || saved.Output.Notes[22].Velocity != 100 {
		t.Fatalf("voice stealing changed: %+v, %v", saved.Output.Notes, err)
	}
	audio.sink.MIDIControlChange(0, 123, 0)
	audio.sink.MIDINoteOn(9, 40, 80)
	now = now.Add(100 * time.Millisecond)
	saved, err = audio.CaptureState()
	if err != nil || len(saved.Output.Notes) != 1 || saved.Output.Notes[0].DrumLeft != 100*time.Millisecond {
		t.Fatalf("percussion position changed: %+v, %v", saved.Output.Notes, err)
	}
	now = now.Add(time.Second)
	saved, err = audio.CaptureState()
	if err != nil || len(saved.Output.Notes) != 0 {
		t.Fatal("expired percussion would restart")
	}
	audio.sink.MIDINoteOn(0, 60, 80)
	audio.sink.MIDINoteOn(0, 60, 0)
	saved, err = audio.CaptureState()
	if err != nil || len(saved.Output.Notes) != 0 {
		t.Fatal("zero-velocity note-on remained sounding")
	}
}

func TestAudioOutputRefusesUntrackedPCMWhileKeepingPlayback(t *testing.T) {
	now := time.Unix(1700000000, 0)
	sink := &recordingSink{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	for i := 0; i <= maxOutputWaves; i++ {
		audio.sink.PlayWave(1, 1, []int16{1})
	}
	if _, err := audio.CaptureState(); err == nil || sink.count("wave") != maxOutputWaves+1 {
		t.Fatal("overflow capture succeeded or ordinary output was dropped")
	}
	now = now.Add(2 * time.Second)
	if state, err := audio.CaptureState(); err != nil || len(state.Output.Waves) != 0 {
		t.Fatalf("expired output still refused capture: %v", err)
	}
}

func TestAudioOutputRejectsMalformedStateBeforeEmission(t *testing.T) {
	source := NewAudio(nil)
	source.sink.MIDINoteOn(0, 60, 90)
	saved, err := source.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	for _, damage := range []func(*AudioState){
		func(s *AudioState) { s.Output.Channels[0].Bend = 16384 },
		func(s *AudioState) { s.Output.Notes = append(s.Output.Notes, s.Output.Notes[0]) },
		func(s *AudioState) { s.Output.Notes[0].Channel = 16 },
		func(s *AudioState) { s.Output.Notes[0].StartedWith.Pan = 128 },
		func(s *AudioState) { s.Output.Notes[0].DrumLeft = time.Second },
		func(s *AudioState) { s.Output.Waves = []AudioWaveState{{Channels: 2, Rate: 8000, Samples: []int16{1}}} },
		func(s *AudioState) { s.Output.Waves = []AudioWaveState{{Channels: 0, Rate: 8000, Samples: []int16{1}}} },
	} {
		bad := saved
		bad.Output.Notes = slices.Clone(saved.Output.Notes)
		damage(&bad)
		sink := &recordingSink{}
		if audio, err := NewAudioFromState(bad, sink); err == nil || audio != nil || len(sink.calls) != 0 {
			t.Fatal("invalid output constructed a runtime or reached the sink")
		}
	}
}
