package backend

import (
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func defaultPitchState(state AudioChannelState) AudioChannelState {
	state.BendSemitones = 2
	state.RPNMSB, state.RPNLSB, state.NRPNMSB, state.NRPNLSB = 127, 127, 127, 127
	return state
}

func TestAudioPitchSensitivitySelectionAndReset(t *testing.T) {
	audio := NewAudio(nil)
	output := audio.sink
	for _, step := range []struct {
		name             string
		controls         [][2]uint8
		semitones, cents uint8
	}{
		{"default ignores data entry", [][2]uint8{{6, 12}}, 2, 0},
		{"partial selector stays unsupported", [][2]uint8{{101, 0}, {6, 9}}, 2, 0},
		{"selected RPN accepts coarse and fine", [][2]uint8{{100, 0}, {6, 12}, {38, 25}}, 12, 25},
		{"cents clamp", [][2]uint8{{38, 127}}, 12, 99},
		{"increment carries cents and ignores data byte", [][2]uint8{{96, 100}}, 13, 0},
		{"decrement borrows cents", [][2]uint8{{97, 0}}, 12, 99},
		{"MSB resets cents", [][2]uint8{{6, 9}}, 9, 0},
		{"unsupported RPN ignores data", [][2]uint8{{100, 1}, {6, 23}, {38, 60}, {96, 0}}, 9, 0},
		{"NRPN prevents stale RPN writes", [][2]uint8{{99, 0}, {98, 0}, {6, 80}, {97, 0}}, 9, 0},
		{"selector MSB preserves its LSB", [][2]uint8{{101, 0}, {6, 30}}, 9, 0},
		{"one-byte reselect uses retained MSB", [][2]uint8{{100, 0}, {38, 50}}, 9, 50},
		{"reset preserves parameter value", [][2]uint8{{121, 0}, {6, 5}, {38, 5}}, 9, 50},
		{"upper limit", [][2]uint8{{101, 0}, {100, 0}, {6, 127}, {38, 99}, {96, 0}}, 127, 99},
		{"lower limit", [][2]uint8{{6, 0}, {97, 127}}, 0, 0},
		{"null selection", [][2]uint8{{101, 127}, {100, 127}, {6, 12}}, 0, 0},
	} {
		for _, control := range step.controls {
			output.MIDIControlChange(0, control[0], control[1])
		}
		state := captureOwnedAudio(t, audio).Output.Channels[0]
		if state.BendSemitones != step.semitones || state.BendCents != step.cents {
			t.Fatalf("%s: range=%d.%02d, want %d.%02d", step.name, state.BendSemitones, state.BendCents, step.semitones, step.cents)
		}
		if peer := captureOwnedAudio(t, audio).Output.Channels[1]; peer.BendSemitones != 2 || peer.BendCents != 0 {
			t.Fatalf("%s changed another channel", step.name)
		}
	}
}

type midiStateResumeProbe struct {
	controlResumeProbe
	sequence []ownedAudioEvent
}

func (sink *midiStateResumeProbe) AudioEvent(sound AudioHandle, event smaf.Event) {
	sink.controlResumeProbe.AudioEvent(sound, event)
	sink.sequence = append(sink.sequence, ownedAudioEvent{sound, event})
}

func (sink *midiStateResumeProbe) ResumeNote(sound AudioHandle, channel, note, velocity uint8, age time.Duration) {
	sink.controlResumeProbe.ResumeNote(sound, channel, note, velocity, age)
	sink.sequence = append(sink.sequence, ownedAudioEvent{sound, smaf.Event{Type: smaf.EventNoteOn, Channel: channel, Note: note, Velocity: velocity}})
}

func TestAudioSustainAndParametersSurviveReconstruction(t *testing.T) {
	for _, mode := range []string{"reconnect", "encoded checkpoint", "paused resume"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Unix(1000, 0)
			audio := NewAudioWithClock(nil, func() time.Time { return now })
			first := loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventControlChange, Control: 101, Value: 0},
				{Type: smaf.EventControlChange, Control: 100, Value: 0},
				{Type: smaf.EventControlChange, Control: 6, Value: 12},
				{Type: smaf.EventControlChange, Control: 38, Value: 25},
				{Type: smaf.EventControlChange, Control: 64, Value: 127},
				{Type: smaf.EventPitchBend, Bend: 12000},
				{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
				{Time: 10, Type: smaf.EventNoteOff, Note: 60},
				{Time: 10, Type: smaf.EventNoteOn, Note: 64, Velocity: 90},
				{Time: 10, Type: smaf.EventControlChange, Control: 100, Value: 1},
				{Time: 10, Type: smaf.EventControlChange, Control: 99, Value: 4},
				{Time: 10, Type: smaf.EventControlChange, Control: 98, Value: 5},
				{Time: 200, Type: smaf.EventControlChange, Control: 64, Value: 0},
				{Time: 1000, Type: smaf.EventEnd},
			})
			peer := loadOwnedAudio(t, audio, []smaf.Event{{Type: smaf.EventNoteOn, Note: 60, Velocity: 80}, {Time: 1000, Type: smaf.EventEnd}})
			audio.Advance(0)
			now = now.Add(10 * time.Millisecond)
			audio.Advance(10 * time.Millisecond)
			now = now.Add(90 * time.Millisecond)
			audio.Advance(100 * time.Millisecond)
			if mode == "paused resume" {
				if err := audio.Pause(first, 100*time.Millisecond); err != nil {
					t.Fatal(err)
				}
			}
			saved := captureOwnedAudio(t, audio)
			if len(saved.Output.Notes) != 3 {
				t.Fatal("fixture lost held or pedal-only notes")
			}
			sink := &midiStateResumeProbe{}
			if mode == "encoded checkpoint" {
				encoded, err := EncodeCheckpointRecord(saved)
				if err != nil {
					t.Fatal(err)
				}
				var decoded AudioState
				if err := DecodeCheckpointRecord(encoded, &decoded); err != nil {
					t.Fatal(err)
				}
				audio, err = NewAudioFromStateWithClock(decoded, sink, func() time.Time { return now })
				if err != nil {
					t.Fatal(err)
				}
				audio.ActivateOutputClock()
			} else {
				audio.SetSink(sink)
			}
			if mode == "paused resume" {
				now = now.Add(time.Second)
				if err := audio.Resume(first, 1100*time.Millisecond); err != nil {
					t.Fatal(err)
				}
			} else {
				audio.ResumeOutput()
			}
			for _, voice := range sink.voices {
				if voice.Sound == peer {
					if mode == "paused resume" || voice.StartedWith.Sustain != 0 || voice.StartedWith.BendSemitones != 2 {
						t.Fatal("replay changed the peer")
					}
					continue
				}
				state := voice.StartedWith
				if state.Sustain != 127 || state.Bend != 12000 || state.BendSemitones != 12 || state.BendCents != 25 || !state.NRPN || state.NRPNMSB != 4 || state.NRPNLSB != 5 || state.RPNMSB != 0 || state.RPNLSB != 1 {
					t.Fatalf("resume used incomplete live state: %+v", state)
				}
				wantAge := 100 * time.Millisecond
				if voice.Note == 64 {
					wantAge = 90 * time.Millisecond
				}
				if voice.Age != wantAge {
					t.Fatalf("resumed age=%s, want %s", voice.Age, wantAge)
				}
			}
			starts, releases := 0, 0
			for i, record := range sink.sequence {
				if record.sound != first {
					continue
				}
				if starts > 0 && record.event.Type == smaf.EventControlChange && (record.event.Control == 6 || record.event.Control == 38) {
					t.Fatal("reconstruction rewrote live sensitivity after notes had resumed")
				}
				if record.event.Type == smaf.EventNoteOff {
					releases++
				}
				if record.event.Type != smaf.EventNoteOn {
					continue
				}
				starts++
				if record.event.Note == 60 && (i+1 >= len(sink.sequence) || sink.sequence[i+1].sound != first || sink.sequence[i+1].event.Type != smaf.EventNoteOff || sink.sequence[i+1].event.Note != 60) {
					t.Fatal("pedal-only resume did not restore its released key")
				}
			}
			if starts != 2 || releases != 1 {
				t.Fatalf("reconstructed starts=%d offs=%d", starts, releases)
			}
			if mode != "paused resume" && !reflect.DeepEqual(captureOwnedAudio(t, audio), saved) {
				t.Fatal("reconstruction mutated playback state")
			}
			now = now.Add(100 * time.Millisecond)
			at := 200 * time.Millisecond
			if mode == "paused resume" {
				at += time.Second
			}
			audio.Advance(at)
			for _, note := range captureOwnedAudio(t, audio).Output.Notes {
				if note.Sound == first && (note.Note == 60 || note.Released) {
					t.Fatal("restored pedal-only note survived pedal release")
				}
			}
		})
	}
}

func TestAudioSustainDefersNoteOffAndAllNotesOff(t *testing.T) {
	for _, off := range []string{"note off", "zero velocity", "all notes off"} {
		t.Run(off, func(t *testing.T) {
			audio := NewAudio(nil)
			output := audio.sink
			output.MIDIControlChange(0, 64, 64)
			output.MIDINoteOn(0, 60, 100)
			output.MIDINoteOn(0, 64, 80)
			output.MIDINoteOn(1, 60, 90)
			switch off {
			case "note off":
				output.MIDINoteOff(0, 60, 0)
			case "zero velocity":
				output.MIDINoteOn(0, 60, 0)
			case "all notes off":
				output.MIDIControlChange(0, 123, 0)
			}
			if got := captureOwnedAudio(t, audio).Output.Notes; len(got) != 3 {
				t.Fatalf("pedal failed to retain notes after %s: %+v", off, got)
			}
			output.MIDIControlChange(0, 64, 63)
			got := captureOwnedAudio(t, audio).Output.Notes
			want := 2
			if off == "all notes off" {
				want = 1
			}
			if len(got) != want || got[len(got)-1].Channel != 1 {
				t.Fatalf("pedal release changed held keys or another channel: %+v", got)
			}
		})
	}
}

func TestAudioResetControllersReleasesOnlyPedalNotes(t *testing.T) {
	audio := NewAudio(nil)
	output := audio.sink
	output.MIDIProgramChange(0, 27)
	output.MIDIControlChange(0, 7, 37)
	output.MIDIControlChange(0, 10, 99)
	output.MIDIControlChange(0, 11, 12)
	output.MIDIPitchBend(0, 12000)
	output.MIDIControlChange(0, 64, 127)
	output.MIDINoteOn(0, 60, 100)
	output.MIDINoteOn(0, 64, 90)
	output.MIDINoteOff(0, 60, 0)
	output.MIDIControlChange(0, 121, 0)
	saved := captureOwnedAudio(t, audio).Output
	state := saved.Channels[0]
	if state.Expression != 127 || state.Sustain != 0 || state.Bend != 8192 || state.Program != 27 || state.Volume != 37 || state.Pan != 99 {
		t.Fatalf("controller reset changed the wrong state: %+v", state)
	}
	if len(saved.Notes) != 1 || saved.Notes[0].Note != 64 {
		t.Fatalf("controller reset failed to preserve the held key: %+v", saved.Notes)
	}
}

func TestAudioAllSoundOffCancelsSustainWithoutTouchingPeerOrPCM(t *testing.T) {
	now := time.Unix(1000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	first := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventControlChange, Control: 64, Value: 127},
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventNoteOff, Note: 60},
		{Type: smaf.EventNoteOn, Channel: 1, Note: 60, Velocity: 90},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{1, 2, 3, 4}},
		{Time: 10, Type: smaf.EventControlChange, Control: 120, Value: 0},
		{Time: 1000, Type: smaf.EventEnd},
	})
	peer := loadOwnedAudio(t, audio, []smaf.Event{{Type: smaf.EventNoteOn, Note: 60, Velocity: 80}, {Time: 1000, Type: smaf.EventEnd}})
	audio.Advance(0)
	if got := captureOwnedAudio(t, audio).Output.Notes; len(got) != 3 {
		t.Fatal("fixture lost its sustained note")
	}
	now = now.Add(10 * time.Millisecond)
	audio.Advance(10 * time.Millisecond)
	saved := captureOwnedAudio(t, audio).Output
	if len(saved.Notes) != 2 || len(saved.Waves) != 1 || saved.Waves[0].Sound != first {
		t.Fatal("all sound off changed PCM or peer notes")
	}
	for _, note := range saved.Notes {
		if note.Sound != peer && (note.Sound != first || note.Channel != 1) {
			t.Fatal("all sound off retained its own channel")
		}
	}
	if saved.Sounds[0].Channels[0].Sustain != 127 {
		t.Fatal("all sound off changed controller positions")
	}
}

func TestAudioMIDIStateRejectsMalformedCheckpoint(t *testing.T) {
	now := time.Unix(1000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	audio.sink.MIDIControlChange(0, 64, 127)
	audio.sink.MIDINoteOn(0, 60, 100)
	audio.sink.MIDINoteOff(0, 60, 0)
	saved := captureOwnedAudio(t, audio)
	for _, test := range []struct {
		name   string
		mutate func(*AudioState)
	}{
		{"old version", func(state *AudioState) { state.Version = 5 }},
		{"semitones", func(state *AudioState) { state.Output.Channels[0].BendSemitones = 128 }},
		{"cents", func(state *AudioState) { state.Output.Channels[0].BendCents = 100 }},
		{"RPN MSB", func(state *AudioState) { state.Output.Channels[0].RPNMSB = 128 }},
		{"RPN LSB", func(state *AudioState) { state.Output.Channels[0].RPNLSB = 128 }},
		{"NRPN MSB", func(state *AudioState) { state.Output.Channels[0].NRPNMSB = 128 }},
		{"NRPN LSB", func(state *AudioState) { state.Output.Channels[0].NRPNLSB = 128 }},
		{"released without sustain", func(state *AudioState) { state.Output.Channels[0].Sustain = 63 }},
		{"released drum", func(state *AudioState) {
			state.Output.Notes[0].Channel = 9
			state.Output.Notes[0].Age = 100 * time.Millisecond
			state.Output.Notes[0].DrumLeft = 100 * time.Millisecond
			state.Output.Channels[9].Sustain = 127
		}},
		{"invalid note metadata", func(state *AudioState) { state.Output.Notes[0].StartedWith.BendCents = 100 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := EncodeCheckpointRecord(saved)
			if err != nil {
				t.Fatal(err)
			}
			var altered AudioState
			if err := DecodeCheckpointRecord(data, &altered); err != nil {
				t.Fatal(err)
			}
			test.mutate(&altered)
			sink := &gainAudioProbe{}
			if _, err := NewAudioFromStateWithClock(altered, sink, func() time.Time { return now }); err == nil {
				t.Fatal("malformed MIDI state was accepted")
			}
			if len(sink.events) != 0 || len(sink.stops) != 0 || len(sink.gains) != 0 {
				t.Fatal("refused MIDI state reached the output")
			}
		})
	}
}

func TestAudioDrumNoteOffKeepsNaturalTailButChannelOffCancels(t *testing.T) {
	now := time.Unix(1000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	output := audio.sink
	output.MIDIControlChange(9, 64, 127)
	output.MIDINoteOn(9, 40, 100)
	output.MIDINoteOff(9, 40, 0)
	now = now.Add(50 * time.Millisecond)
	if got := captureOwnedAudio(t, audio).Output.Notes; len(got) != 1 || got[0].DrumLeft != 150*time.Millisecond {
		t.Fatalf("ordinary drum note-off forgot its sounding one-shot: %+v", got)
	}
	output.MIDIControlChange(9, 123, 0)
	if got := captureOwnedAudio(t, audio).Output.Notes; len(got) != 0 {
		t.Fatalf("all notes off deferred a drum to sustain: %+v", got)
	}
}

func TestAudioReplayRestoresPitchSensitivityBeforeNotes(t *testing.T) {
	audio := NewAudio(nil)
	output := audio.sink
	output.MIDIControlChange(0, 101, 0)
	output.MIDIControlChange(0, 100, 0)
	output.MIDIControlChange(0, 6, 12)
	output.MIDIControlChange(0, 38, 25)
	output.MIDIPitchBend(0, 12000)
	output.MIDINoteOn(0, 60, 100)
	sink := &ownedAudioProbe{}
	audio.SetSink(sink)
	audio.ResumeOutput()
	semitones, cents := uint8(0), uint8(0)
	for _, record := range sink.events {
		event := record.event
		if event.Channel != 0 {
			continue
		}
		if event.Type == smaf.EventControlChange {
			if event.Control == 6 {
				semitones = event.Value
			}
			if event.Control == 38 {
				cents = event.Value
			}
		}
		if event.Type == smaf.EventNoteOn {
			if semitones != 12 || cents != 25 {
				t.Fatalf("replayed note used sensitivity %d.%02d, want 12.25", semitones, cents)
			}
			return
		}
	}
	t.Fatal("reconstruction emitted no note")
}

func TestAudioLegacyReplayUsesEachNotesOwnerSettings(t *testing.T) {
	audio := NewAudio(nil)
	loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventControlChange, Control: 101, Value: 0},
		{Type: smaf.EventControlChange, Control: 100, Value: 0},
		{Type: smaf.EventControlChange, Control: 6, Value: 12},
		{Type: smaf.EventControlChange, Control: 38, Value: 25},
		{Type: smaf.EventControlChange, Control: 64, Value: 127},
		{Type: smaf.EventPitchBend, Bend: 12000},
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventNoteOff, Note: 60},
		{Time: 1000, Type: smaf.EventEnd},
	})
	loadOwnedAudio(t, audio, []smaf.Event{{Type: smaf.EventNoteOn, Note: 64, Velocity: 90}, {Time: 1000, Type: smaf.EventEnd}})
	audio.Advance(0)
	sink := &outputProbe{}
	audio.SetSink(sink)
	audio.ResumeOutput()
	if len(sink.voices) != 2 {
		t.Fatal("legacy replay lost a note")
	}
	for _, voice := range sink.voices {
		state := voice.StartedWith
		if voice.Note == 60 && (state.Sustain != 127 || state.Bend != 12000 || state.BendSemitones != 12 || state.BendCents != 25) {
			t.Fatalf("legacy first owner inherited another owner's state: %+v", state)
		}
		if voice.Note == 64 && (state.Sustain != 0 || state.Bend != 8192 || state.BendSemitones != 2 || state.BendCents != 0) {
			t.Fatalf("legacy second owner inherited another owner's state: %+v", state)
		}
	}
}
