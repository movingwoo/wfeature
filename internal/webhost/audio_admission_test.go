package webhost

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

const admissionRefusedSample int16 = -30000

func admissionWaveSamples(index int) []int16 {
	samples := make([]int16, 8)
	for frame := range samples {
		samples[frame] = int16(1000 + index*16 + frame)
	}
	return samples
}

func admissionRunner(t *testing.T, protocol int, pcm bool) *sessionRunner {
	t.Helper()
	runner := stalledRunner(t, 1)
	runner.protocol, runner.soundOwnership, runner.soundResume = protocol, true, true
	runner.soundPCM, runner.soundTiming = pcm, true
	runner.audio = &audioCollector{pcmChannels: pcm}
	return runner
}

func requireAdmissionWaves(t *testing.T, runner *sessionRunner, reader *pcmWireReader, owner backend.AudioHandle, frames int, reset bool) {
	t.Helper()
	if len(runner.outText) != 1 {
		t.Fatalf("admission queued %d messages, want one", len(runner.outText))
	}
	message := <-runner.outText
	if runner.protocol == protocolPictures {
		// The retained charge belongs to checkpoint state, never the wire.
		var document struct {
			Audio []map[string]json.RawMessage `json:"audio"`
		}
		if err := json.Unmarshal([]byte(message.text), &document); err != nil {
			t.Fatal(err)
		}
		for _, event := range document.Audio {
			if string(event["kind"]) != `"playWave"` {
				continue
			}
			for field := range event {
				switch field {
				case "kind", "sound", "at", "pcmChannel", "channels", "rate", "samples":
				default:
					t.Fatalf("wave added an unexpected wire field %q", field)
				}
			}
		}
	}
	// This independent reader accepts only the existing JSON/WFA2 operations.
	events := reader.read(t, message)
	if reset && (len(events) == 0 || events[0].Kind != audioAllOff) {
		t.Fatal("admission reconstruction did not clear old output first")
	}
	count := 0
	for _, event := range events {
		if event.Kind != audioPlayWave {
			continue
		}
		for offset := 0; offset+1 < len(event.pcm); offset += 2 {
			if int16(binary.LittleEndian.Uint16(event.pcm[offset:])) == admissionRefusedSample {
				t.Fatal("refused PCM marker reached the wire")
			}
		}
		if count >= 256 {
			t.Fatal("more PCM reached the wire than the backend can retain")
		}
		channels, group := uint8(1), uint16(257)
		var want []byte
		for _, sample := range admissionWaveSamples(count)[frames:] {
			want = binary.LittleEndian.AppendUint16(want, uint16(sample))
			if !runner.soundPCM {
				// Authored unity controls and pan zero give exact left-only PCM.
				want = binary.LittleEndian.AppendUint16(want, 0)
			}
		}
		if !runner.soundPCM {
			channels, group = 2, 0
		}
		if event.Sound != uint32(owner) || event.PCMChannel != group || event.Channels != channels || event.Rate != 4 || !bytes.Equal(event.pcm, want) {
			t.Fatalf("admitted wave %d changed owner, routing or sample tail: %+v", count, event)
		}
		count++
	}
	if count != 256 {
		t.Fatalf("wire contains %d admitted waves, want 256", count)
	}
}

func captureAdmissionState(t *testing.T, audio *backend.Audio, owner backend.AudioHandle, frames int) backend.AudioState {
	t.Helper()
	saved, err := audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 9 || len(saved.Output.Waves) != 256 {
		t.Fatalf("admission capture: version=%d, waves=%d", saved.Version, len(saved.Output.Waves))
	}
	for index, wave := range saved.Output.Waves {
		if wave.Sound != owner || wave.PCMChannel != 257 || wave.Channels != 1 || wave.Rate != 4 || !slices.Equal(wave.Samples, admissionWaveSamples(index)[frames:]) {
			t.Fatalf("capture changed admitted raw wave %d: %+v", index, wave)
		}
	}
	encoded, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var charges struct {
		Output struct {
			Waves []struct{ BudgetBytes uint32 }
		}
	}
	if err := json.Unmarshal(encoded, &charges); err != nil {
		t.Fatal(err)
	}
	for index, wave := range charges.Output.Waves {
		if wave.BudgetBytes != 16 {
			t.Fatalf("trimmed wave %d saved charge %d, want its original 16 bytes", index, wave.BudgetBytes)
		}
	}
	var decoded backend.AudioState
	if err := backend.DecodeCheckpointRecord(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, saved) {
		t.Fatal("checkpoint round trip changed admitted output")
	}
	return decoded
}

func sendAdmissionReplay(t *testing.T, runner *sessionRunner, audio *backend.Audio) {
	t.Helper()
	replay, overflow := runner.audio.collectReplay(audio.ResumeOutput)
	if overflow || !runner.sendAudio(append([]audioEvent{{Kind: audioAllOff}}, replay...), true) {
		t.Fatal("admitted PCM reconstruction exceeded the collector or queue")
	}
}

func TestAudioAdmissionKeepsLiveRecoveryAndReconnectWavesConsistent(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, pcm := range []bool{false, true} {
			t.Run(fmt.Sprintf("protocol_%d/pcm_%t", protocol, pcm), func(t *testing.T) {
				runner := admissionRunner(t, protocol, pcm)
				now := time.Unix(1700000000, 0)
				audio := backend.NewAudioWithClock(runner.audio, func() time.Time { return now })
				events := []smaf.Event{
					{Type: smaf.EventPCMControl, PCMChannel: 257, Control: 7, Value: 127},
					{Type: smaf.EventPCMControl, PCMChannel: 257, Control: 11, Value: 127},
					{Type: smaf.EventPCMControl, PCMChannel: 257, Control: 10, Value: 0},
				}
				for index := range 512 {
					samples := admissionWaveSamples(index)
					if index >= 256 {
						for frame := range samples {
							samples[frame] = admissionRefusedSample
						}
					}
					events = append(events, smaf.Event{Type: smaf.EventWave, PCMChannel: 257, WaveChannels: 1, SamplingRate: 4, Wave: samples})
				}
				events = append(events,
					smaf.Event{Time: 250, Type: smaf.EventNoteOn, Note: 60, Velocity: 80},
					smaf.Event{Time: 250, Type: smaf.EventWave, PCMChannel: 257, WaveChannels: 1, SamplingRate: 4, Wave: []int16{admissionRefusedSample}},
					smaf.Event{Time: 3000, Type: smaf.EventEnd},
				)
				owner, err := audio.LoadEvents(events)
				if err != nil {
					t.Fatal(err)
				}
				if err := audio.Play(owner, 0, false); err != nil {
					t.Fatal(err)
				}
				audio.Advance(0)
				runner.flushAudio()
				var reader pcmWireReader
				requireAdmissionWaves(t, runner, &reader, owner, 0, false)
				if runner.audioNeedsReset {
					t.Fatal("ordinary admission unnecessarily requested output recovery")
				}
				captureAdmissionState(t, audio, owner, 0)

				runner.outText <- outboundMessage{}
				now = now.Add(250 * time.Millisecond)
				audio.Advance(250 * time.Millisecond)
				requireAudioFlushReturns(t, runner)
				if !runner.audioNeedsReset || runner.shed.Load() != 1 {
					t.Fatal("the dropped batch did not request recovery")
				}
				<-runner.outText
				// Use the recovery collector/send sequence with an exact backend
				// clock; the session fixture separately covers its automatic retry.
				sendAdmissionReplay(t, runner, audio)
				requireAdmissionWaves(t, runner, &reader, owner, 1, true)
				captureAdmissionState(t, audio, owner, 1)

				now = now.Add(250 * time.Millisecond)
				// A new connection has no definitions and can change capabilities.
				reconnected := admissionRunner(t, protocol, !pcm)
				audio.SetSink(reconnected.audio)
				sendAdmissionReplay(t, reconnected, audio)
				var reconnectReader pcmWireReader
				requireAdmissionWaves(t, reconnected, &reconnectReader, owner, 2, true)
				saved := captureAdmissionState(t, audio, owner, 2)

				restored := admissionRunner(t, protocol, pcm)
				now = time.Unix(1900000000, 0)
				fresh, err := backend.NewAudioFromStateWithClock(saved, restored.audio, func() time.Time { return now })
				if err != nil {
					t.Fatal(err)
				}
				if err := fresh.RebasePlaybackClock(500*time.Millisecond, 1); err != nil {
					t.Fatal(err)
				}
				now = now.Add(time.Minute)
				fresh.ActivateOutputClock()
				sendAdmissionReplay(t, restored, fresh)
				var restoreReader pcmWireReader
				requireAdmissionWaves(t, restored, &restoreReader, owner, 2, true)
				captureAdmissionState(t, fresh, owner, 2)
			})
		}
	}
}
