package backend

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// voiceResumeProbe records what an FM-capable page receives, in order.
type voiceResumeProbe struct {
	ownedAudioProbe
	resumed []AudioVoiceState
}

func (sink *voiceResumeProbe) ResumeNote(sound AudioHandle, channel, note, velocity uint8, age time.Duration) {
	sink.resumed = append(sink.resumed, AudioVoiceState{Sound: sound, Channel: channel, Note: note, Velocity: velocity, Age: age})
	sink.events = append(sink.events, ownedAudioEvent{sound, smaf.Event{Type: smaf.EventNoteOn, Channel: channel, Note: note, Velocity: velocity}})
}

func (sink *voiceResumeProbe) ResumeVoiceNote(sound AudioHandle, channel, note, velocity, voice, pitch uint8, age time.Duration) {
	sink.resumed = append(sink.resumed, AudioVoiceState{Sound: sound, Channel: channel, Note: note, Velocity: velocity, Age: age, Voice: voice, Pitch: pitch})
	sink.events = append(sink.events, ownedAudioEvent{sound, smaf.Event{Type: smaf.EventNoteOn, Channel: channel, Note: note, Velocity: velocity, Program: voice, Value: pitch}})
}

func testVoiceBytes() []byte {
	voice := smaf.Voice{BasicOctave: 1}
	voice.Operators[0] = smaf.Operator{Multiple: 2, TotalLevel: 20, Attack: 15, Decay: 4, Release: 6, Feedback: 3}
	voice.Operators[1] = smaf.Operator{Multiple: 1, Attack: 15, Decay: 2, SustainLevel: 3, Release: 7}
	return voice.Encode()
}

func voicedScore() []smaf.Event {
	return []smaf.Event{
		{Type: smaf.EventVoice, Program: 1, SysEx: testVoiceBytes()},
		{Type: smaf.EventProgramChange, Channel: 2, Program: 11},
		{Type: smaf.EventNoteOn, Channel: 2, Note: 60, Velocity: 100, Program: 1, Value: 48},
		{Time: 500, Type: smaf.EventNoteOff, Channel: 2, Note: 60},
		{Time: 1000, Type: smaf.EventEnd},
	}
}

func TestAudioVoicesReachOwnedSinksAndLegacySinksHearOrdinaryNotes(t *testing.T) {
	owned := &ownedAudioProbe{}
	audio := NewAudio(owned)
	handle := loadOwnedAudio(t, audio, voicedScore())
	audio.Advance(0)
	definitions := owned.ofType(smaf.EventVoice)
	if len(definitions) != 1 || definitions[0].sound != handle || definitions[0].event.Program != 1 || !bytes.Equal(definitions[0].event.SysEx, testVoiceBytes()) {
		t.Fatalf("definitions = %+v", definitions)
	}
	notes := owned.ofType(smaf.EventNoteOn)
	if len(notes) != 1 || notes[0].event.Program != 1 || notes[0].event.Value != 48 || notes[0].event.Note != 60 {
		t.Fatalf("notes = %+v", notes)
	}
	if len(owned.calls) != 0 {
		t.Fatalf("an owned sink received legacy calls %v", owned.calls)
	}

	legacy := &recordingSink{}
	audio = NewAudio(legacy)
	loadOwnedAudio(t, audio, voicedScore())
	audio.Advance(0)
	if !slices.Contains(legacy.calls, "on 2 60 100") || legacy.count("sysex") != 0 {
		t.Fatalf("legacy calls = %v", legacy.calls)
	}
}

func TestAudioVoiceReconstructionDefinesBeforeNotes(t *testing.T) {
	for _, mode := range []string{"reconnect", "encoded checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Unix(1000, 0)
			clock := func() time.Time { return now }
			first := &voiceResumeProbe{}
			audio := NewAudioWithClock(first, clock)
			handle := loadOwnedAudio(t, audio, voicedScore())
			audio.Advance(0)
			now = now.Add(100 * time.Millisecond)
			audio.Advance(100 * time.Millisecond)
			sink := first
			if mode == "encoded checkpoint" {
				saved := captureOwnedAudio(t, audio)
				if len(saved.Output.Notes) != 1 || saved.Output.Notes[0].Voice != 1 || saved.Output.Notes[0].Pitch != 48 {
					t.Fatalf("captured notes = %+v", saved.Output.Notes)
				}
				encoded, err := EncodeCheckpointRecord(saved)
				if err != nil {
					t.Fatal(err)
				}
				var decoded AudioState
				if err := DecodeCheckpointRecord(encoded, &decoded); err != nil {
					t.Fatal(err)
				}
				sink = &voiceResumeProbe{}
				audio, err = NewAudioFromStateWithClock(decoded, sink, clock)
				if err != nil {
					t.Fatal(err)
				}
				audio.ActivateOutputClock()
			}
			sink.events, sink.resumed = nil, nil
			audio.ResumeOutput()
			definition := slices.IndexFunc(sink.events, func(event ownedAudioEvent) bool { return event.event.Type == smaf.EventVoice })
			note := slices.IndexFunc(sink.events, func(event ownedAudioEvent) bool { return event.event.Type == smaf.EventNoteOn })
			if definition < 0 || note < 0 || definition > note || sink.events[definition].sound != handle {
				t.Fatalf("reconstruction order = %+v", sink.events)
			}
			if len(sink.resumed) != 1 || sink.resumed[0].Voice != 1 || sink.resumed[0].Pitch != 48 || sink.resumed[0].Age != 100*time.Millisecond {
				t.Fatalf("resumed = %+v", sink.resumed)
			}
		})
	}
}

func TestAudioVoiceRestartDefinesAgainAndStopForgets(t *testing.T) {
	sink := &voiceResumeProbe{}
	audio := NewAudio(sink)
	handle := loadOwnedAudio(t, audio, voicedScore())
	audio.Advance(0)
	audio.Stop(handle)
	sink.events = nil
	audio.ResumeOutput()
	if len(sink.ofType(smaf.EventVoice)) != 0 {
		t.Fatal("a stopped sound's voices were defined again")
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	if len(sink.ofType(smaf.EventVoice)) != 1 {
		t.Fatalf("a restarted sound did not define its voice: %+v", sink.events)
	}
}

func TestAudioVoiceEventsAreValidated(t *testing.T) {
	for name, events := range map[string][]smaf.Event{
		"undecodable voice":  {{Type: smaf.EventVoice, Program: 1, SysEx: []byte{1, 0, 0}}, {Time: 1, Type: smaf.EventEnd}},
		"voice without slot": {{Type: smaf.EventVoice, SysEx: testVoiceBytes()}, {Time: 1, Type: smaf.EventEnd}},
		"pitch without slot": {{Type: smaf.EventNoteOn, Note: 60, Velocity: 1, Value: 60}, {Time: 1, Type: smaf.EventEnd}},
		"pitch past MIDI":    {{Type: smaf.EventNoteOn, Note: 60, Velocity: 1, Program: 1, Value: 128}, {Time: 1, Type: smaf.EventEnd}},
		"unknown event type": {{Type: smaf.EventVoice + 1}, {Time: 1, Type: smaf.EventEnd}},
	} {
		if _, err := NewAudio(nil).LoadEvents(events); err == nil {
			t.Errorf("%s was loaded", name)
		}
	}
}
