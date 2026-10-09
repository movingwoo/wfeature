package backend

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type controlResumeProbe struct {
	gainAudioProbe
	channels map[AudioHandle][16]AudioChannelState
	voices   []AudioVoiceState
}

func (sink *controlResumeProbe) AudioEvent(sound AudioHandle, event smaf.Event) {
	sink.gainAudioProbe.AudioEvent(sound, event)
	if sink.channels == nil {
		sink.channels = make(map[AudioHandle][16]AudioChannelState)
	}
	channels, ok := sink.channels[sound]
	if !ok {
		channels = defaultAudioChannels()
	}
	state := &channels[event.Channel]
	switch event.Type {
	case smaf.EventProgramChange:
		state.Program = event.Program
	case smaf.EventPitchBend:
		state.Bend = event.Bend
	case smaf.EventControlChange:
		state.controlChange(event.Control, event.Value)
	}
	sink.channels[sound] = channels
}

func (sink *controlResumeProbe) ResumeNote(sound AudioHandle, channel, note, velocity uint8, age time.Duration) {
	sink.voices = append(sink.voices, AudioVoiceState{Sound: sound, Channel: channel, Note: note, Velocity: velocity,
		Age: age, StartedWith: sink.channels[sound][channel]})
}

func TestAudioReconstructionUsesLiveControlsAndOriginalPrograms(t *testing.T) {
	for _, mode := range []string{"reconnect", "encoded checkpoint", "paused resume"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Unix(1000, 0)
			audio := NewAudioWithClock(nil, func() time.Time { return now })
			first := loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventProgramChange, Program: 27},
				{Type: smaf.EventNoteOn, Note: 60, Velocity: 80},
				{Time: 10, Type: smaf.EventProgramChange, Program: 50},
				{Time: 10, Type: smaf.EventNoteOn, Note: 64, Velocity: 100},
				{Time: 20, Type: smaf.EventControlChange, Control: 7, Value: 0},
				{Time: 20, Type: smaf.EventControlChange, Control: 11, Value: 63},
				{Time: 20, Type: smaf.EventControlChange, Control: 10, Value: 127},
				{Time: 20, Type: smaf.EventPitchBend, Bend: 9000},
				{Time: 1000, Type: smaf.EventEnd},
			})
			second := loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventProgramChange, Program: 72},
				{Type: smaf.EventNoteOn, Note: 60, Velocity: 90},
				{Time: 1000, Type: smaf.EventEnd},
			})
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
			sink := &controlResumeProbe{}
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
			want := []AudioVoiceState{
				{Sound: first, Note: 60, Velocity: 80, Age: 100 * time.Millisecond,
					StartedWith: defaultPitchState(AudioChannelState{Program: 27, Volume: 0, Expression: 63, Pan: 127, Bend: 9000})},
				{Sound: first, Note: 64, Velocity: 100, Age: 90 * time.Millisecond,
					StartedWith: defaultPitchState(AudioChannelState{Program: 50, Volume: 0, Expression: 63, Pan: 127, Bend: 9000})},
			}
			if mode != "paused resume" {
				want = append(want, AudioVoiceState{Sound: second, Note: 60, Velocity: 90, Age: 100 * time.Millisecond,
					StartedWith: defaultPitchState(AudioChannelState{Program: 72, Volume: 100, Expression: 127, Pan: 64, Bend: 8192})})
			}
			slices.SortFunc(sink.voices, func(a, b AudioVoiceState) int {
				if a.Sound != b.Sound {
					return int(a.Sound) - int(b.Sound)
				}
				return int(a.Note) - int(b.Note)
			})
			if !reflect.DeepEqual(sink.voices, want) {
				t.Fatalf("restored voices = %+v, want %+v", sink.voices, want)
			}
			if mode != "paused resume" && !reflect.DeepEqual(captureOwnedAudio(t, audio), saved) {
				t.Fatal("output reconstruction changed the portable record")
			}
		})
	}
}
