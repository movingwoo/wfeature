package webhost

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

// readMIDIReplay preserves every MIDI operation and its position in the batch.
func readMIDIReplay(t *testing.T, message outboundMessage) []audioEvent {
	t.Helper()
	if !message.audio {
		t.Fatal("expected a MIDI replay message")
	}
	if message.binary == nil {
		var decoded serverMessage
		if err := json.Unmarshal([]byte(message.text), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Kind != serverAudio {
			t.Fatalf("replay message kind = %q", decoded.Kind)
		}
		return decoded.Audio
	}
	var events []audioEvent
	var sound uint32
	for _, operation := range readAudio(t, message.binary) {
		event := audioEvent{Sound: sound}
		switch operation.op {
		case audioOpSelectSound:
			sound = operation.id
			continue
		case audioOpSoundGain:
			event.Kind, event.Value = audioSoundGain, binary.BigEndian.Uint16(operation.operands)
		case audioOpProgramChange:
			event.Kind, event.Channel, event.Program = audioProgramChange, operation.operands[0], operation.operands[1]
		case audioOpControlChange:
			event.Kind, event.Channel, event.Control, event.Value = audioControlChange, operation.operands[0], operation.operands[1], uint16(operation.operands[2])
		case audioOpPitchBend:
			event.Kind, event.Channel, event.Value = audioPitchBend, operation.operands[0], binary.BigEndian.Uint16(operation.operands[1:])
		case audioOpNoteResume:
			event.Kind, event.Channel, event.Note, event.Velocity = audioNoteResume, operation.operands[0], operation.operands[1], operation.operands[2]
			event.Age = binary.BigEndian.Uint32(operation.operands[3:])
		case audioOpNoteOn, audioOpNoteOff:
			event.Kind = audioNoteOn
			if operation.op == audioOpNoteOff {
				event.Kind = audioNoteOff
			}
			event.Channel, event.Note, event.Velocity = operation.operands[0], operation.operands[1], operation.operands[2]
		default:
			t.Fatalf("unexpected MIDI replay operation %#x", operation.op)
		}
		events = append(events, event)
	}
	return events
}

func TestAudioMIDIReplayRestoresSustainedNotesAcrossBothProtocols(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, mode := range []string{"reconnect", "checkpoint"} {
			t.Run(fmt.Sprintf("protocol_%d/%s", protocol, mode), func(t *testing.T) {
				runner := stalledRunner(t, 1)
				runner.protocol, runner.soundOwnership, runner.soundResume = protocol, true, true
				runner.audio = &audioCollector{}
				now := time.Unix(1000, 0)
				audio := backend.NewAudioWithClock(nil, func() time.Time { return now })
				load := func(events []smaf.Event) backend.AudioHandle {
					t.Helper()
					handle, err := audio.LoadEvents(events)
					if err != nil {
						t.Fatal(err)
					}
					if err := audio.Play(handle, 0, false); err != nil {
						t.Fatal(err)
					}
					return handle
				}
				const channel = 3
				first := load([]smaf.Event{
					{Type: smaf.EventProgramChange, Channel: channel, Program: 27},
					{Type: smaf.EventNoteOn, Channel: channel, Note: 60, Velocity: 100},
					{Time: 10, Type: smaf.EventProgramChange, Channel: channel, Program: 50},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 7, Value: 37},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 11, Value: 91},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 10, Value: 100},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 64, Value: 127},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 101, Value: 0},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 100, Value: 0},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 6, Value: 12},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 38, Value: 25},
					{Time: 10, Type: smaf.EventPitchBend, Channel: channel, Bend: 12288},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 100, Value: 1},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 99, Value: 4},
					{Time: 10, Type: smaf.EventControlChange, Channel: channel, Control: 98, Value: 5},
					{Time: 20, Type: smaf.EventNoteOff, Channel: channel, Note: 60},
					{Time: 20, Type: smaf.EventNoteOn, Channel: channel, Note: 64, Velocity: 80},
					{Time: 200, Type: smaf.EventControlChange, Channel: channel, Control: 64, Value: 0},
					{Time: 1000, Type: smaf.EventEnd},
				})
				peer := load([]smaf.Event{
					{Type: smaf.EventProgramChange, Channel: channel, Program: 72},
					{Type: smaf.EventControlChange, Channel: channel, Control: 7, Value: 83},
					{Type: smaf.EventControlChange, Channel: channel, Control: 11, Value: 77},
					{Type: smaf.EventControlChange, Channel: channel, Control: 10, Value: 32},
					{Type: smaf.EventControlChange, Channel: channel, Control: 101, Value: 0},
					{Type: smaf.EventControlChange, Channel: channel, Control: 100, Value: 0},
					{Type: smaf.EventControlChange, Channel: channel, Control: 6, Value: 3},
					{Type: smaf.EventControlChange, Channel: channel, Control: 38, Value: 75},
					{Type: smaf.EventPitchBend, Channel: channel, Bend: 4096},
					{Type: smaf.EventControlChange, Channel: channel, Control: 101, Value: 127},
					{Type: smaf.EventControlChange, Channel: channel, Control: 100, Value: 127},
					{Type: smaf.EventNoteOn, Channel: channel, Note: 60, Velocity: 90},
					{Time: 1000, Type: smaf.EventEnd},
				})
				if err := audio.SetSoundVolume(first, 50); err != nil {
					t.Fatal(err)
				}
				for _, milliseconds := range []int{0, 10, 20, 100} {
					now = time.Unix(1000, 0).Add(time.Duration(milliseconds) * time.Millisecond)
					audio.Advance(time.Duration(milliseconds) * time.Millisecond)
				}
				saved, err := audio.CaptureState()
				if err != nil {
					t.Fatal(err)
				}
				if len(saved.Output.Notes) != 3 {
					t.Fatalf("fixture retained %d voices, want a sustained key and two held keys", len(saved.Output.Notes))
				}
				if mode == "checkpoint" {
					encoded, err := backend.EncodeCheckpointRecord(saved)
					if err != nil {
						t.Fatal(err)
					}
					var decoded backend.AudioState
					if err := backend.DecodeCheckpointRecord(encoded, &decoded); err != nil {
						t.Fatal(err)
					}
					now = time.Unix(2000, 0)
					audio, err = backend.NewAudioFromStateWithClock(decoded, runner.audio, func() time.Time { return now })
					if err != nil {
						t.Fatal(err)
					}
					audio.ActivateOutputClock()
				} else {
					audio.SetSink(runner.audio)
				}
				replay, overflow := runner.audio.collectReplay(audio.ResumeOutput)
				if overflow || !runner.sendAudio(replay, true) || len(runner.outText) != 1 {
					t.Fatal("MIDI reconstruction did not reach the transport intact")
				}
				message := <-runner.outText
				if (message.binary != nil) != (protocol == protocolStream) {
					t.Fatal("reconstruction used the wrong transport")
				}
				events := readMIDIReplay(t, message)
				channels := make(map[uint32]*[16]backend.AudioChannelState)
				gains := make(map[uint32]uint16)
				starts := make(map[[2]uint32]int)
				releases := 0
				for index, event := range events {
					states := channels[event.Sound]
					if states == nil {
						states = newAudioReplayChannels()
						channels[event.Sound] = states
					}
					applyAudioReplayChannel(&states[event.Channel], event)
					switch event.Kind {
					case audioSoundGain:
						gains[event.Sound] = event.Value
					case audioNoteResume:
						wantState := backend.AudioChannelState{Program: 27, Volume: 37, Expression: 91, Pan: 100, Sustain: 127, Bend: 12288,
							BendSemitones: 12, BendCents: 25, RPNMSB: 0, RPNLSB: 1, NRPNMSB: 4, NRPNLSB: 5, NRPN: true}
						wantAge, wantVelocity, wantGain := uint32(100), uint8(100), uint16(5000)
						switch {
						case event.Sound == uint32(first) && event.Note == 60:
							wantOff := audioEvent{Kind: audioNoteOff, Sound: uint32(first), Channel: channel, Note: 60}
							if index+1 >= len(events) || !reflect.DeepEqual(events[index+1], wantOff) {
								t.Fatal("sustained note was not immediately followed by its owned key release")
							}
						case event.Sound == uint32(first) && event.Note == 64:
							wantState.Program, wantAge, wantVelocity = 50, 80, 80
						case event.Sound == uint32(peer) && event.Note == 60:
							wantState = backend.AudioChannelState{Program: 72, Volume: 83, Expression: 77, Pan: 32, Bend: 4096,
								BendSemitones: 3, BendCents: 75, RPNMSB: 127, RPNLSB: 127, NRPNMSB: 127, NRPNLSB: 127}
							wantVelocity, wantGain = 90, backend.AudioGainUnity
						default:
							t.Fatalf("reconstruction started an unexpected voice: %+v", event)
						}
						if event.Channel != channel || event.Age != wantAge || event.Velocity != wantVelocity || gains[event.Sound] != wantGain {
							t.Fatalf("reconstructed voice lost its age, velocity, gain or channel: %+v, gain=%d", event, gains[event.Sound])
						}
						if states[channel] != wantState {
							t.Fatalf("owner %d note %d started with incomplete live controls:\n got %+v\nwant %+v", event.Sound, event.Note, states[channel], wantState)
						}
						starts[[2]uint32{event.Sound, uint32(event.Note)}]++
					case audioNoteOff:
						releases++
						if event.Sound != uint32(first) || event.Channel != channel || event.Note != 60 {
							t.Fatalf("reconstruction released a held or peer-owned key: %+v", event)
						}
					case audioNoteOn:
						t.Fatal("reconstruction replaced a resume with a fresh attack")
					}
				}
				wantStarts := map[[2]uint32]int{{uint32(first), 60}: 1, {uint32(first), 64}: 1, {uint32(peer), 60}: 1}
				if !reflect.DeepEqual(starts, wantStarts) || releases != 1 {
					t.Fatalf("reconstructed starts=%v releases=%d", starts, releases)
				}
				for _, sound := range saved.Output.Sounds {
					if got := channels[uint32(sound.Sound)]; got == nil || *got != sound.Channels {
						t.Fatalf("reconstruction left owner %d with another voice's controller state", sound.Sound)
					}
				}
				now = now.Add(100 * time.Millisecond)
				audio.Advance(200 * time.Millisecond)
				runner.flushAudio()
				if len(runner.outText) != 1 {
					t.Fatal("pedal release did not produce one live batch")
				}
				wantRelease := []audioEvent{{Kind: audioControlChange, Sound: uint32(first), Channel: channel, Control: 64}}
				if got := readMIDIReplay(t, <-runner.outText); !reflect.DeepEqual(got, wantRelease) {
					t.Fatalf("pedal release changed another owner or retriggered notes: %+v", got)
				}
			})
		}
	}
}

func TestLegacyAudioMIDIReplayRestoresEachNotesControlsBeforeFlattening(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			runner := stalledRunner(t, 1)
			runner.protocol = protocol
			runner.audio = &audioCollector{legacyReplay: true}
			now := time.Unix(1000, 0)
			audio := backend.NewAudioWithClock(nil, func() time.Time { return now })
			const channel = 3
			voices := []struct {
				note     uint8
				volume   int
				controls backend.AudioChannelState
			}{
				{60, 50, backend.AudioChannelState{Program: 27, Volume: 37, Expression: 91, Pan: 100, Sustain: 127, Bend: 12288,
					BendSemitones: 12, BendCents: 25, RPNMSB: 0, RPNLSB: 1, NRPNMSB: 4, NRPNLSB: 5, NRPN: true}},
				{64, 100, backend.AudioChannelState{Program: 72, Volume: 83, Expression: 77, Pan: 32, Bend: 4096,
					BendSemitones: 3, BendCents: 75, RPNMSB: 127, RPNLSB: 127, NRPNMSB: 127, NRPNLSB: 127}},
			}
			for _, voice := range voices {
				state := voice.controls
				events := []smaf.Event{{Type: smaf.EventProgramChange, Channel: channel, Program: state.Program}}
				for _, control := range [][2]uint8{
					{7, state.Volume}, {11, state.Expression}, {10, state.Pan}, {64, state.Sustain},
					{101, 0}, {100, 0}, {6, state.BendSemitones}, {38, state.BendCents},
					{99, state.NRPNMSB}, {98, state.NRPNLSB}, {101, state.RPNMSB}, {100, state.RPNLSB},
				} {
					events = append(events, smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: control[0], Value: control[1]})
				}
				if state.NRPN {
					events = append(events, smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 98, Value: state.NRPNLSB})
				}
				events = append(events,
					smaf.Event{Type: smaf.EventPitchBend, Channel: channel, Bend: state.Bend},
					smaf.Event{Type: smaf.EventNoteOn, Channel: channel, Note: voice.note, Velocity: 100},
					smaf.Event{Time: 1000, Type: smaf.EventEnd},
				)
				handle, err := audio.LoadEvents(events)
				if err != nil {
					t.Fatal(err)
				}
				if err := audio.SetSoundVolume(handle, voice.volume); err != nil {
					t.Fatal(err)
				}
				if err := audio.Play(handle, 0, false); err != nil {
					t.Fatal(err)
				}
			}
			audio.Advance(0)
			now = now.Add(75 * time.Millisecond)
			audio.SetSink(runner.audio)
			replay, overflow := runner.audio.collectReplay(audio.ResumeOutput)
			if overflow || !runner.sendAudio(replay, true) || len(runner.outText) != 1 {
				t.Fatal("legacy MIDI replay did not reach the transport intact")
			}
			message := <-runner.outText
			if (message.binary != nil) != (protocol == protocolStream) {
				t.Fatal("legacy replay used the wrong transport")
			}
			if message.binary != nil {
				for _, operation := range readAudio(t, message.binary) {
					switch operation.op {
					case audioOpSelectSound, audioOpSoundGain, audioOpStopSound, audioOpNoteResume:
						t.Fatalf("legacy replay contains unsupported operation %#x", operation.op)
					}
				}
			} else if strings.Contains(message.text, `"sound"`) || strings.Contains(message.text, `"age"`) {
				t.Fatalf("legacy replay contains ownership or envelope-age fields: %s", message.text)
			}
			channels := newAudioReplayChannels()
			starts := make(map[uint8]int)
			for _, event := range readMIDIReplay(t, message) {
				if event.Sound != 0 || event.Kind == audioNoteResume || event.Kind == audioSoundGain || event.Kind == audioStopSound {
					t.Fatalf("legacy replay contains an unsupported event: %+v", event)
				}
				applyAudioReplayChannel(&channels[event.Channel], event)
				if event.Kind != audioNoteOn {
					continue
				}
				index := -1
				for i, voice := range voices {
					if voice.note == event.Note {
						index = i
					}
				}
				if index == -1 || event.Channel != channel {
					t.Fatalf("legacy replay started an unexpected key: %+v", event)
				}
				want := voices[index]
				if channels[channel] != want.controls {
					t.Fatalf("legacy note %d inherited another owner's controls:\n got %+v\nwant %+v", event.Note, channels[channel], want.controls)
				}
				if event.Age != 0 || event.Velocity != uint8(want.volume) {
					t.Fatalf("legacy note did not retain its scaled attack: %+v", event)
				}
				starts[event.Note]++
			}
			if !reflect.DeepEqual(starts, map[uint8]int{60: 1, 64: 1}) {
				t.Fatalf("legacy starts = %v, want both independent owners once", starts)
			}
		})
	}
}
