package webhost

import (
	"bytes"
	"context"
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

func TestAudioWavePhaseNegotiationRequiresExplicitOwnership(t *testing.T) {
	for _, test := range []struct {
		query string
		owned bool
		want  bool
	}{{"", true, false}, {"0", true, false}, {"1", false, false}, {"1", true, true}, {"2", true, false}, {"true", true, false}} {
		if got := negotiatedWavePhase(test.query, test.owned); got != test.want {
			t.Fatalf("phase query %q, ownership %t: got %t, want %t", test.query, test.owned, got, test.want)
		}
	}
}

func requireWavePhaseJSON(t *testing.T, message outboundMessage, golden string) {
	t.Helper()
	var got, want map[string]any
	if err := json.Unmarshal([]byte(message.text), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(golden), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wave JSON = %s, want %s", message.text, golden)
	}
}

func TestAudioWavePhaseCollectorWireAndDefinitionIdentity(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			runner := stalledRunner(t, 1)
			runner.protocol, runner.soundOwnership, runner.soundPCM, runner.soundWavePhase = protocol, true, true, true
			runner.audio = &audioCollector{pcmChannels: true}
			var sink backend.AudioWaveResumeSink = runner.audio
			samples := []int16{-32768, 32767, 0x1234, -0x1234}
			sink.ResumeWave(0x89abcdef, smaf.Event{Type: smaf.EventWave, PCMChannel: 0x1234, WaveChannels: 1, SamplingRate: 8000, Wave: samples}, 0x01020304)
			sink.ResumeWave(17, smaf.Event{Type: smaf.EventWave, WaveChannels: 2, SamplingRate: 8000, Wave: samples}, 0x03040506)
			runner.audio.AudioEvent(17, smaf.Event{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8000, Wave: samples})
			runner.audio.AudioEvent(17, smaf.Event{Type: smaf.EventWave, PCMChannel: 0x21, WaveChannels: 1, SamplingRate: 8000, Wave: samples})
			clear(samples)
			runner.flushAudio()
			message := <-runner.outText
			if protocol == protocolStream {
				want := []byte{'W', 'F', 'A', '2',
					0x07, 0x89, 0xab, 0xcd, 0xef,
					0x10, 0, 0, 0, 1, 0, 0, 0, 8, 0, 0x80, 0xff, 0x7f, 0x34, 0x12, 0xcc, 0xed,
					0x16, 0, 0, 0, 1, 1, 0, 0, 0x1f, 0x40, 0x12, 0x34, 1, 2, 3, 4,
					0x07, 0, 0, 0, 17,
					0x16, 0, 0, 0, 1, 2, 0, 0, 0x1f, 0x40, 0, 0, 3, 4, 5, 6,
					0x11, 0, 0, 0, 1, 1, 0, 0, 0x1f, 0x40,
					0x14, 0, 0, 0, 1, 1, 0, 0, 0x1f, 0x40, 0, 0x21}
				if !bytes.Equal(message.binary, want) {
					t.Fatalf("phase wire = % x, want % x", message.binary, want)
				}
			} else {
				requireWavePhaseJSON(t, message, `{"kind":"audio","audio":[{"kind":"playWave","sound":2309737967,"pcmChannel":4660,"channels":1,"rate":8000,"framePhase":16909060,"samples":"AID/fzQSzO0="},{"kind":"playWave","sound":17,"channels":2,"rate":8000,"framePhase":50595078,"samples":"AID/fzQSzO0="},{"kind":"playWave","sound":17,"channels":1,"rate":8000,"samples":"AID/fzQSzO0="},{"kind":"playWave","sound":17,"pcmChannel":33,"channels":1,"rate":8000,"samples":"AID/fzQSzO0="}]}`)
			}
			pcm := []byte{0, 0x80, 0xff, 0x7f, 0x34, 0x12, 0xcc, 0xed}
			want := []audioEvent{
				{Kind: audioPlayWave, Sound: 0x89abcdef, PCMChannel: 0x1234, Channels: 1, Rate: 8000, FramePhase: 0x01020304, pcm: pcm},
				{Kind: audioPlayWave, Sound: 17, Channels: 2, Rate: 8000, FramePhase: 0x03040506, pcm: pcm},
				{Kind: audioPlayWave, Sound: 17, Channels: 1, Rate: 8000, pcm: pcm},
				{Kind: audioPlayWave, Sound: 17, PCMChannel: 0x21, Channels: 1, Rate: 8000, pcm: pcm},
			}
			var reader pcmWireReader
			if got := reader.read(t, message); !reflect.DeepEqual(got, want) {
				t.Fatalf("collector changed borrowed samples or phase metadata: %+v", got)
			}
			repeated := want[1]
			repeated.FramePhase = 500000000
			if !runner.sendAudio([]audioEvent{repeated}, false) {
				t.Fatal("repeated wave was not queued")
			}
			message = <-runner.outText
			if protocol == protocolStream {
				want := []byte{'W', 'F', 'A', '2', 0x07, 0, 0, 0, 17,
					0x16, 0, 0, 0, 1, 2, 0, 0, 0x1f, 0x40, 0, 0, 0x1d, 0xcd, 0x65, 0}
				if !bytes.Equal(message.binary, want) {
					t.Fatalf("phase change redefined immutable PCM: % x", message.binary)
				}
			}
			if got := reader.read(t, message); !reflect.DeepEqual(got, []audioEvent{repeated}) {
				t.Fatalf("reused PCM definition changed the fractional offset: %+v", got)
			}
		})
	}
}

func TestAudioWavePhaseCapabilityKeepsLegacyWireExact(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, capability := range []string{"legacy", "owned", "resume", "pcm"} {
			t.Run(fmt.Sprintf("%d/%s", protocol, capability), func(t *testing.T) {
				runner := stalledRunner(t, 1)
				runner.protocol, runner.soundOwnership, runner.soundResume, runner.soundPCM = protocol, capability != "legacy", capability == "resume", capability == "pcm"
				runner.audio = &audioCollector{pcmChannels: runner.soundPCM}
				runner.audio.ResumeWave(7, smaf.Event{Type: smaf.EventWave, PCMChannel: 257, WaveChannels: 1, SamplingRate: 8000, Wave: []int16{16384, -16384}}, 500000000)
				runner.flushAudio()
				message := <-runner.outText
				owner, group := uint32(0), uint16(0)
				ownerJSON, groupJSON := "", ""
				want := []byte{'W', 'F', 'A', '2'}
				if runner.soundOwnership {
					owner, ownerJSON = 7, `,"sound":7`
					want = append(want, 0x07, 0, 0, 0, 7)
				}
				want = append(want, 0x10, 0, 0, 0, 1, 0, 0, 0, 4, 0, 0x40, 0, 0xc0)
				op := byte(0x11)
				if runner.soundPCM {
					op, group, groupJSON = 0x14, 257, `,"pcmChannel":257`
				}
				want = append(want, op, 0, 0, 0, 1, 1, 0, 0, 0x1f, 0x40)
				if group != 0 {
					want = append(want, 1, 1)
				}
				if protocol == protocolStream {
					if !bytes.Equal(message.binary, want) {
						t.Fatalf("legacy wave operands changed: % x, want % x", message.binary, want)
					}
				} else {
					requireWavePhaseJSON(t, message, `{"kind":"audio","audio":[{"kind":"playWave"`+ownerJSON+groupJSON+`,"channels":1,"rate":8000,"samples":"AEAAwA=="}]}`)
				}
				var reader pcmWireReader
				expected := audioEvent{Kind: audioPlayWave, Sound: owner, PCMChannel: group, Channels: 1, Rate: 8000, pcm: []byte{0, 0x40, 0, 0xc0}}
				if got := reader.read(t, message); !reflect.DeepEqual(got, []audioEvent{expected}) {
					t.Fatalf("unnegotiated phase reached the page: %+v", got)
				}
			})
		}
	}
}

func wavePhaseBackend(t *testing.T, collector *audioCollector, now *time.Time) *backend.Audio {
	t.Helper()
	audio := backend.NewAudioWithClock(collector, func() time.Time { return *now })
	handle, err := audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventPCMControl, PCMChannel: 257, Control: 7, Value: 127},
		{Type: smaf.EventPCMControl, PCMChannel: 257, Control: 11, Value: 127},
		{Type: smaf.EventPCMControl, PCMChannel: 257, Control: 10},
		{Type: smaf.EventWave, PCMChannel: 257, WaveChannels: 1, SamplingRate: 4, Wave: []int16{100, -200, 300, -400}},
		{Time: 2000, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	return audio
}

func onlyPhaseWave(t *testing.T, events []audioEvent) audioEvent {
	t.Helper()
	var waves []audioEvent
	for _, event := range events {
		if event.Kind == audioPlayWave {
			waves = append(waves, event)
		}
	}
	if len(waves) != 1 {
		t.Fatalf("expected one PCM tail, got %d", len(waves))
	}
	return waves[0]
}

func expectedPhaseWave(samples []int16, pcm bool, phase uint32, at float64) audioEvent {
	event := audioEvent{Kind: audioPlayWave, Sound: 1, PCMChannel: 257, Channels: 1, Rate: 4, FramePhase: phase, At: presentation(at)}
	if !pcm {
		event.PCMChannel, event.Channels = 0, 2
	}
	for _, sample := range samples {
		event.pcm = binary.LittleEndian.AppendUint16(event.pcm, uint16(sample))
		if !pcm {
			event.pcm = append(event.pcm, 0, 0)
		}
	}
	return event
}

func TestAudioWavePhaseBackendReplayAndCheckpoint(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, pcm := range []bool{false, true} {
			for _, phase := range []bool{false, true} {
				for _, restore := range []bool{false, true} {
					t.Run(fmt.Sprintf("%d/pcm_%t/phase_%t/restore_%t", protocol, pcm, phase, restore), func(t *testing.T) {
						runner := stalledRunner(t, 1)
						runner.protocol, runner.soundOwnership, runner.soundResume, runner.soundTiming = protocol, true, true, true
						runner.soundPCM, runner.soundWavePhase = pcm, phase
						runner.audio = &audioCollector{pcmChannels: pcm}
						now := time.Unix(1000, 0)
						audio := wavePhaseBackend(t, runner.audio, &now)
						runner.audio.take()
						now = now.Add(375 * time.Millisecond)
						audio.Advance(375 * time.Millisecond)
						saved, err := audio.CaptureState()
						if err != nil {
							t.Fatal(err)
						}
						if saved.Version != 9 || len(saved.Output.Waves) != 1 || saved.Output.Waves[0].FramePhase != 500000000 || saved.Output.Waves[0].BudgetBytes != 8 || !slices.Equal(saved.Output.Waves[0].Samples, []int16{-200, 300, -400}) {
							t.Fatalf("capture lost fractional frame or raw suffix: %+v", saved.Output.Waves)
						}
						at := 0.375
						if restore {
							encoded, err := backend.EncodeCheckpointRecord(saved)
							if err != nil {
								t.Fatal(err)
							}
							var decoded backend.AudioState
							if err := backend.DecodeCheckpointRecord(encoded, &decoded); err != nil {
								t.Fatal(err)
							}
							now = now.Add(10 * time.Second)
							audio, err = backend.NewAudioFromStateWithClock(decoded, runner.audio, func() time.Time { return now })
							if err != nil {
								t.Fatal(err)
							}
							if err := audio.RebasePlaybackClock(375*time.Millisecond, 1); err != nil {
								t.Fatal(err)
							}
							audio.ActivateOutputClock()
							at = 0
						}
						var reader pcmWireReader
						for round := range 2 {
							fraction, samples := uint32(500000000), []int16{-200, 300, -400}
							if round == 1 {
								now = now.Add(125 * time.Millisecond)
								audio.Advance(500 * time.Millisecond)
								at, fraction, samples = at+0.125, 0, []int16{300, -400}
							}
							events, overflow := runner.audio.collectReplay(audio.ResumeOutput)
							if overflow || onlyPhaseWave(t, events).FramePhase != fraction {
								t.Fatal("collector lost the backend's current fractional frame")
							}
							if !runner.sendAudio(events, false) {
								t.Fatal("PCM replay was not queued")
							}
							if !phase {
								fraction = 0
							}
							got := onlyPhaseWave(t, reader.read(t, <-runner.outText))
							want := expectedPhaseWave(samples, pcm, fraction, at)
							if !reflect.DeepEqual(got, want) {
								t.Fatalf("replay suffix, phase or onset fallback differs: got %+v, want %+v", got, want)
							}
						}
					})
				}
			}
		}
	}
}

func TestAudioWavePhaseDropRecoveryUsesCurrentOffset(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			runner := stalledRunner(t, 1)
			runner.protocol, runner.soundOwnership, runner.soundResume, runner.soundTiming = protocol, true, true, true
			runner.soundPCM, runner.soundWavePhase = true, true
			runner.audio = &audioCollector{pcmChannels: true}
			now := time.Unix(1000, 0)
			audio := wavePhaseBackend(t, runner.audio, &now)
			runner.flushAudio()
			now = now.Add(375 * time.Millisecond)
			audio.Advance(375 * time.Millisecond)
			audio.ResumeOutput()
			requireAudioFlushReturns(t, runner)
			if !runner.audioNeedsReset || runner.shed.Load() != 1 {
				t.Fatal("dropped fractional replay did not request reconstruction")
			}
			var reader pcmWireReader
			reader.read(t, <-runner.outText)
			now = now.Add(325 * time.Millisecond)
			audio.Advance(700 * time.Millisecond)
			// Session retry is covered by the integration tests; this exercises
			// its reconstruction sequence with an exact fractional sample clock.
			events, overflow := runner.audio.collectReplay(audio.ResumeOutput)
			if overflow || !runner.sendAudio(append([]audioEvent{{Kind: audioAllOff}}, events...), true) {
				t.Fatal("fractional reconstruction did not fit the output queue")
			}
			message := <-runner.outText
			if protocol == protocolStream && !bytes.HasPrefix(message.binary, []byte{'W', 'F', 'A', '2', 0x13}) {
				t.Fatal("dropped PCM definitions were not invalidated")
			}
			got := reader.read(t, message)
			if got[0].Kind != audioAllOff {
				t.Fatal("recovery did not cancel previous sources first")
			}
			want := expectedPhaseWave([]int16{300, -400}, true, 800000000, 0.7)
			if wave := onlyPhaseWave(t, got); !reflect.DeepEqual(wave, want) {
				t.Fatalf("recovery replayed stale samples or phase: got %+v, want %+v", wave, want)
			}
		})
	}
}

func TestAudioWavePhaseReconnectUsesNewPageCapability(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/initial_%t", protocol, enabled), func(t *testing.T) {
				root := t.TempDir()
				writeCheckpointGame(t, root, "skt", audioReplayArchive(t))
				server := checkpointServer(t, root)
				newRunner := func(phase bool) *sessionRunner {
					return &sessionRunner{server: server, protocol: protocol, soundOwnership: true, soundResume: true,
						soundPCM: true, soundWavePhase: phase, frames: make(chan pendingFrame, 1),
						outText: make(chan outboundMessage, 64), writerDone: make(chan struct{})}
				}
				runner, resumed := newRunner(enabled), newRunner(!enabled)
				t.Cleanup(func() {
					runner.closeGame()
					resumed.closeGame()
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					server.CloseSessions(ctx)
				})
				runner.startGame(t.Context(), clientMessage{Kind: clientStart, Game: "games/skt/checkpoint.zip", ID: 1})
				if runner.game == nil || runner.audio == nil {
					t.Fatalf("audio fixture failed to start: %+v", readCheckpointReplies(t, runner))
				}
				collector, token := runner.audio, runner.token
				runner.park()
				resumed.resumeGame(t.Context(), clientMessage{Kind: clientResume, Token: token, ID: 2})
				if resumed.game == nil || resumed.audio != collector {
					t.Fatal("reconnect did not retain the running audio collector")
				}
				for len(resumed.outText) > 0 {
					<-resumed.outText
				}
				collector.ResumeWave(7, smaf.Event{Type: smaf.EventWave, PCMChannel: 257, WaveChannels: 1, SamplingRate: 4, Wave: []int16{100, -200}}, 500000000)
				resumed.flushAudio()
				var reader pcmWireReader
				wave := onlyPhaseWave(t, reader.read(t, <-resumed.outText))
				want := uint32(500000000)
				if enabled {
					want = 0
				}
				if wave.FramePhase != want || wave.PCMChannel != 257 || !bytes.Equal(wave.pcm, []byte{100, 0, 0x38, 0xff}) {
					t.Fatalf("reconnect used the previous page's phase capability: %+v", wave)
				}
			})
		}
	}
}
