package webhost

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

// pcmWireReader keeps definitions across messages and reads the public wire
// operands independently of the encoder, including the unchanged legacy wave.
type pcmWireReader struct {
	definitions map[uint32][]byte
}

func TestAudioPCMNegotiationRequiresExplicitOwnership(t *testing.T) {
	for _, test := range []struct {
		query string
		owned bool
		want  bool
	}{{"", true, false}, {"0", true, false}, {"1", false, false}, {"1", true, true}, {"2", true, false}, {"true", true, false}} {
		if got := negotiatedPCM(test.query, test.owned); got != test.want {
			t.Fatalf("PCM query %q, ownership %t: got %t, want %t", test.query, test.owned, got, test.want)
		}
	}
}

func (reader *pcmWireReader) read(t *testing.T, message outboundMessage) []audioEvent {
	t.Helper()
	if !message.audio {
		t.Fatal("expected an audio message")
	}
	if message.binary == nil {
		var decoded serverMessage
		if err := json.Unmarshal([]byte(message.text), &decoded); err != nil {
			t.Fatal(err)
		}
		for index := range decoded.Audio {
			event := &decoded.Audio[index]
			if event.Kind == audioPlayWave {
				var err error
				event.pcm, err = base64.StdEncoding.DecodeString(event.Samples)
				if err != nil {
					t.Fatal(err)
				}
				event.Samples = ""
			}
		}
		return decoded.Audio
	}
	if !bytes.HasPrefix(message.binary, []byte("WFA2")) {
		t.Fatal("expected a WFA2 message")
	}
	rest := message.binary[4:]
	take := func(size int) []byte {
		if len(rest) < size {
			t.Fatal("audio message ends inside an operation")
		}
		part := rest[:size]
		rest = rest[size:]
		return part
	}
	var events []audioEvent
	var sound uint32
	var at *float64
	emit := func(event audioEvent) {
		event.Sound, event.At = sound, at
		events = append(events, event)
	}
	for len(rest) > 0 {
		switch operation := take(1)[0]; operation {
		case 0x01, 0x02:
			data := take(3)
			kind := audioNoteOn
			if operation == 0x02 {
				kind = audioNoteOff
			}
			emit(audioEvent{Kind: kind, Channel: data[0], Note: data[1], Velocity: data[2]})
		case 0x03:
			data := take(2)
			emit(audioEvent{Kind: audioProgramChange, Channel: data[0], Program: data[1]})
		case 0x04:
			data := take(3)
			emit(audioEvent{Kind: audioControlChange, Channel: data[0], Control: data[1], Value: uint16(data[2])})
		case 0x05:
			data := take(3)
			emit(audioEvent{Kind: audioPitchBend, Channel: data[0], Value: binary.BigEndian.Uint16(data[1:])})
		case 0x06:
			events = append(events, audioEvent{Kind: audioAllOff})
			at = nil
		case 0x07:
			sound = binary.BigEndian.Uint32(take(4))
		case 0x08:
			emit(audioEvent{Kind: audioStopSound})
		case 0x09:
			emit(audioEvent{Kind: audioSoundGain, Value: binary.BigEndian.Uint16(take(2))})
		case 0x0a:
			data := take(7)
			emit(audioEvent{Kind: audioNoteResume, Channel: data[0], Note: data[1], Velocity: data[2], Age: binary.BigEndian.Uint32(data[3:])})
		case 0x0b:
			present := take(1)[0]
			at = nil
			if present == 1 {
				at = presentation(math.Float64frombits(binary.BigEndian.Uint64(take(8))))
			} else if present != 0 {
				t.Fatal("invalid audio time selector")
			}
		case 0x0c:
			events = append(events, audioEvent{Kind: audioClock, At: presentation(math.Float64frombits(binary.BigEndian.Uint64(take(8))))})
		case 0x10:
			id, length := binary.BigEndian.Uint32(take(4)), binary.BigEndian.Uint32(take(4))
			if reader.definitions == nil {
				reader.definitions = make(map[uint32][]byte)
			}
			reader.definitions[id] = bytes.Clone(take(int(length)))
		case 0x11, 0x14, 0x16:
			id := binary.BigEndian.Uint32(take(4))
			data := take(5)
			event := audioEvent{Kind: audioPlayWave, Channels: data[0], Rate: binary.BigEndian.Uint32(data[1:])}
			if operation == 0x14 || operation == 0x16 {
				event.PCMChannel = binary.BigEndian.Uint16(take(2))
			}
			if operation == 0x16 {
				event.FramePhase = binary.BigEndian.Uint32(take(4))
			}
			var exists bool
			if event.pcm, exists = reader.definitions[id]; !exists {
				t.Fatalf("wave refers to missing definition %d", id)
			}
			emit(event)
		case 0x13:
			clear(reader.definitions)
		case 0x15:
			data := take(4)
			emit(audioEvent{Kind: audioPCMControl, PCMChannel: binary.BigEndian.Uint16(data), Control: data[2], Value: uint16(data[3])})
		default:
			t.Fatalf("unexpected PCM-test operation %#x", operation)
		}
	}
	return events
}

func TestAudioPCMCollectorPreservesRoutingAndRawSamplesOnBothProtocols(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			runner := stalledRunner(t, 1)
			runner.protocol, runner.soundOwnership, runner.soundPCM = protocol, true, true
			runner.audio = &audioCollector{pcmChannels: true}
			var capable backend.AudioPCMSink = runner.audio
			if !capable.PCMChannels() {
				t.Fatal("collector lost its negotiated PCM capability")
			}
			for _, control := range []struct{ number, value uint8 }{{7, 0}, {11, 63}, {10, 127}} {
				runner.audio.AudioEvent(0x89abcdef, smaf.Event{Type: smaf.EventPCMControl, PCMChannel: 0x1234, Control: control.number, Value: control.value})
			}
			samples := []int16{-32768, 32767, 0x1234}
			for _, route := range []struct {
				owner backend.AudioHandle
				group uint16
			}{{0x89abcdef, 0x1234}, {17, 0x21}, {17, 0}} {
				runner.audio.AudioEvent(route.owner, smaf.Event{Type: smaf.EventWave, PCMChannel: route.group, WaveChannels: 1, SamplingRate: 8000, Wave: samples})
			}
			clear(samples)
			runner.flushAudio()
			message := <-runner.outText
			pcm := []byte{0, 0x80, 0xff, 0x7f, 0x34, 0x12}
			if protocol == protocolStream {
				want := []byte{'W', 'F', 'A', '2',
					0x07, 0x89, 0xab, 0xcd, 0xef,
					0x15, 0x12, 0x34, 7, 0, 0x15, 0x12, 0x34, 11, 63, 0x15, 0x12, 0x34, 10, 127,
					0x10, 0, 0, 0, 1, 0, 0, 0, 6, 0, 0x80, 0xff, 0x7f, 0x34, 0x12,
					0x14, 0, 0, 0, 1, 1, 0, 0, 0x1f, 0x40, 0x12, 0x34,
					0x07, 0, 0, 0, 17,
					0x14, 0, 0, 0, 1, 1, 0, 0, 0x1f, 0x40, 0, 0x21,
					0x11, 0, 0, 0, 1, 1, 0, 0, 0x1f, 0x40}
				if !bytes.Equal(message.binary, want) {
					t.Fatalf("PCM wire = % x, want % x", message.binary, want)
				}
			} else {
				var actual, want map[string]any
				if err := json.Unmarshal([]byte(message.text), &actual); err != nil {
					t.Fatal(err)
				}
				golden := `{"kind":"audio","audio":[{"kind":"pcmControl","sound":2309737967,"pcmChannel":4660,"control":7},{"kind":"pcmControl","sound":2309737967,"pcmChannel":4660,"control":11,"value":63},{"kind":"pcmControl","sound":2309737967,"pcmChannel":4660,"control":10,"value":127},{"kind":"playWave","sound":2309737967,"pcmChannel":4660,"channels":1,"rate":8000,"samples":"AID/fzQS"},{"kind":"playWave","sound":17,"pcmChannel":33,"channels":1,"rate":8000,"samples":"AID/fzQS"},{"kind":"playWave","sound":17,"channels":1,"rate":8000,"samples":"AID/fzQS"}]}`
				if err := json.Unmarshal([]byte(golden), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, want) {
					t.Fatalf("PCM JSON = %s, want %s", message.text, golden)
				}
			}
			var reader pcmWireReader
			want := []audioEvent{
				{Kind: audioPCMControl, Sound: 0x89abcdef, PCMChannel: 0x1234, Control: 7},
				{Kind: audioPCMControl, Sound: 0x89abcdef, PCMChannel: 0x1234, Control: 11, Value: 63},
				{Kind: audioPCMControl, Sound: 0x89abcdef, PCMChannel: 0x1234, Control: 10, Value: 127},
				{Kind: audioPlayWave, Sound: 0x89abcdef, PCMChannel: 0x1234, Channels: 1, Rate: 8000, pcm: pcm},
				{Kind: audioPlayWave, Sound: 17, PCMChannel: 0x21, Channels: 1, Rate: 8000, pcm: pcm},
				{Kind: audioPlayWave, Sound: 17, Channels: 1, Rate: 8000, pcm: pcm},
			}
			if got := reader.read(t, message); !reflect.DeepEqual(got, want) {
				t.Fatalf("PCM collector changed routing, controls or borrowed samples: %+v", got)
			}
			if !runner.sendAudio([]audioEvent{want[3]}, false) {
				t.Fatal("repeated wave was not queued")
			}
			if got := reader.read(t, <-runner.outText); !reflect.DeepEqual(got, want[3:4]) {
				t.Fatalf("repeated definition changed routing: %+v", got)
			}
		})
	}
}

func TestAudioPCMCapabilityKeepsLegacyAndOwnedWireCompatible(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, capability := range []string{"legacy", "owned", "resume"} {
			t.Run(fmt.Sprintf("%d/%s", protocol, capability), func(t *testing.T) {
				runner := stalledRunner(t, 1)
				runner.protocol, runner.soundOwnership, runner.soundResume = protocol, capability != "legacy", capability == "resume"
				collector := &audioCollector{}
				if collector.PCMChannels() {
					t.Fatal("PCM capability was enabled without negotiation")
				}
				pcm := []byte{0, 0x40, 0, 0xc0}
				events := []audioEvent{
					{Kind: audioPCMControl, Sound: 7, PCMChannel: 257, Control: 7, Value: 63},
					{Kind: audioPlayWave, Sound: 7, PCMChannel: 257, Channels: 1, Rate: 8000, pcm: pcm},
					{Kind: audioControlChange, Sound: 7, Channel: 1, Control: 7, Value: 91},
				}
				if !runner.sendAudio(events, false) {
					t.Fatal("compatible wave was not queued")
				}
				message := <-runner.outText
				var reader pcmWireReader
				sound := uint32(7)
				if !runner.soundOwnership {
					sound = 0
				}
				want := []audioEvent{
					{Kind: audioPlayWave, Sound: sound, Channels: 1, Rate: 8000, pcm: pcm},
					{Kind: audioControlChange, Sound: sound, Channel: 1, Control: 7, Value: 91},
				}
				if got := reader.read(t, message); !reflect.DeepEqual(got, want) {
					t.Fatalf("unsupported PCM metadata or controls reached the page: %+v", got)
				}
				if protocol == protocolStream {
					for _, operation := range readAudio(t, message.binary) {
						if operation.op == audioOpPlayWave && len(operation.operands) != 5 {
							t.Fatal("legacy wave operands changed")
						}
					}
				}
			})
		}
	}
}

func TestAudioPCMReconnectRefreshesCollectorCapability(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/initial_%t", protocol, enabled), func(t *testing.T) {
				root := t.TempDir()
				writeCheckpointGame(t, root, "skt", audioReplayArchive(t))
				server := checkpointServer(t, root)
				newRunner := func(pcm bool) *sessionRunner {
					return &sessionRunner{server: server, protocol: protocol, soundOwnership: true, soundResume: true, soundPCM: pcm,
						frames: make(chan pendingFrame, 1), outText: make(chan outboundMessage, 64), writerDone: make(chan struct{})}
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
				collector := runner.audio
				if collector.PCMChannels() != enabled {
					t.Fatal("startup did not apply the negotiated PCM capability")
				}
				token := runner.token
				runner.park()
				resumed.resumeGame(t.Context(), clientMessage{Kind: clientResume, Token: token, ID: 2})
				if resumed.game == nil || resumed.audio != collector || collector.PCMChannels() != !enabled {
					t.Fatal("reconnect did not update the retained collector for its new page")
				}
				for len(resumed.outText) > 0 {
					<-resumed.outText
				}
				collector.AudioEvent(7, smaf.Event{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 7, Value: 63})
				collector.AudioEvent(7, smaf.Event{Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 8000, Wave: []int16{1, 2}})
				resumed.flushAudio()
				var reader pcmWireReader
				events := reader.read(t, <-resumed.outText)
				wantCount, wantChannel := 2, uint16(1)
				if enabled {
					wantCount, wantChannel = 1, 0
				}
				if len(events) != wantCount || events[len(events)-1].PCMChannel != wantChannel {
					t.Fatalf("reconnected page received stale PCM capabilities: %+v", events)
				}
			})
		}
	}
}

func TestAudioPCMBackendFallbackUsesCurrentOnsetControls(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			runner := stalledRunner(t, 1)
			runner.protocol, runner.soundOwnership = protocol, true
			runner.audio = &audioCollector{}
			audio := backend.NewAudio(runner.audio)
			handle, err := audio.LoadEvents([]smaf.Event{
				{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 7, Value: 127},
				{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 11, Value: 127},
				{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 10},
				{Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 8000, Wave: []int16{10000, -10000}},
				{Time: 250, Type: smaf.EventPCMControl, PCMChannel: 1, Control: 7},
				{Time: 250, Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 8000, Wave: []int16{10000, -10000}},
				{Time: 1000, Type: smaf.EventEnd},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := audio.Play(handle, 0, false); err != nil {
				t.Fatal(err)
			}
			audio.Advance(250 * time.Millisecond)
			batch := runner.audio.take()
			for _, event := range batch {
				if event.Kind == audioPCMControl || event.PCMChannel != 0 {
					t.Fatalf("backend sent live PCM operations to an incapable collector: %+v", event)
				}
			}
			if !runner.sendAudio(batch, false) {
				t.Fatal("fallback was not queued")
			}
			var reader pcmWireReader
			var waves []audioEvent
			for _, event := range reader.read(t, <-runner.outText) {
				if event.Kind == audioPlayWave {
					waves = append(waves, event)
				}
			}
			want := []audioEvent{
				{Kind: audioPlayWave, Sound: uint32(handle), Channels: 2, Rate: 8000, pcm: []byte{0x10, 0x27, 0, 0, 0xf0, 0xd8, 0, 0}},
				{Kind: audioPlayWave, Sound: uint32(handle), Channels: 2, Rate: 8000, pcm: make([]byte, 8)},
			}
			if !reflect.DeepEqual(waves, want) {
				t.Fatalf("legacy onset gain/pan = %+v, want %+v", waves, want)
			}
		})
	}
}

func TestAudioPCMReplayRestoresLatestControlsAndTrimmedTailsAfterDrop(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, restore := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/restore_%t", protocol, restore), func(t *testing.T) {
				runner := stalledRunner(t, 1)
				runner.protocol, runner.soundOwnership, runner.soundResume, runner.soundPCM = protocol, true, true, true
				runner.audio = &audioCollector{pcmChannels: true}
				now := time.Unix(1700000000, 0)
				audio := backend.NewAudioWithClock(runner.audio, func() time.Time { return now })
				samples := []int16{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000, 1100, 1200}
				owners := make([]backend.AudioHandle, 2)
				for index := range owners {
					events := []smaf.Event{
						{Type: smaf.EventPCMControl, PCMChannel: 257, Control: 7, Value: 127},
						{Type: smaf.EventPCMControl, PCMChannel: 257, Control: 11, Value: uint8(127 - 32*index)},
						{Type: smaf.EventPCMControl, PCMChannel: 257, Control: 10, Value: uint8(64 - 64*index)},
						{Type: smaf.EventWave, PCMChannel: 257, WaveChannels: 1, SamplingRate: 4, Wave: samples},
					}
					if index == 0 {
						events = append(events,
							smaf.Event{Time: 250, Type: smaf.EventPCMControl, PCMChannel: 257, Control: 7, Value: 63},
							smaf.Event{Time: 500, Type: smaf.EventPCMControl, PCMChannel: 257, Control: 11, Value: 31},
							smaf.Event{Time: 750, Type: smaf.EventPCMControl, PCMChannel: 257, Control: 10, Value: 127},
						)
					}
					events = append(events, smaf.Event{Time: 4000, Type: smaf.EventEnd})
					var err error
					owners[index], err = audio.LoadEvents(events)
					if err != nil {
						t.Fatal(err)
					}
					if err := audio.Play(owners[index], 0, false); err != nil {
						t.Fatal(err)
					}
				}
				audio.Advance(0)
				runner.flushAudio()
				var reader pcmWireReader
				reader.read(t, <-runner.outText)
				runner.outText <- outboundMessage{}
				now = now.Add(750 * time.Millisecond)
				audio.Advance(750 * time.Millisecond)
				requireAudioFlushReturns(t, runner)
				if !runner.audioNeedsReset {
					t.Fatal("dropped PCM controls did not request output reconstruction")
				}
				<-runner.outText
				if restore {
					saved, err := audio.CaptureState()
					if err != nil {
						t.Fatal(err)
					}
					audio, err = backend.NewAudioFromStateWithClock(saved, runner.audio, func() time.Time { return now })
					if err != nil {
						t.Fatal(err)
					}
					if err := audio.RebasePlaybackClock(750*time.Millisecond, 1); err != nil {
						t.Fatal(err)
					}
					audio.ActivateOutputClock()
				}
				// The existing session integration covers the automatic retry. Exercise
				// its collector/replay/wire sequence with an exact backend clock here.
				replay, overflow := runner.audio.collectReplay(audio.ResumeOutput)
				if overflow || !runner.sendAudio(append([]audioEvent{{Kind: audioAllOff}}, replay...), true) {
					t.Fatal("PCM reconstruction did not fit the output queue")
				}
				got := reader.read(t, <-runner.outText)
				if len(got) == 0 || got[0].Kind != audioAllOff {
					t.Fatal("reconstruction did not clear old sources first")
				}
				controls := make(map[[3]uint32]uint16)
				waves := make(map[uint32]bool)
				wantControls := [2]map[uint8]uint16{{7: 63, 11: 31, 10: 127}, {7: 127, 11: 95, 10: 0}}
				var tail []byte
				for _, sample := range samples[3:] {
					tail = binary.LittleEndian.AppendUint16(tail, uint16(sample))
				}
				for _, event := range got {
					switch event.Kind {
					case audioPCMControl:
						controls[[3]uint32{event.Sound, uint32(event.PCMChannel), uint32(event.Control)}] = event.Value
					case audioPlayWave:
						index := slices.Index(owners, backend.AudioHandle(event.Sound))
						if index < 0 || waves[event.Sound] || event.PCMChannel != 257 || event.Channels != 1 || event.Rate != 4 || !bytes.Equal(event.pcm, tail) {
							t.Fatalf("replay changed ownership, routing or sample position: %+v", event)
						}
						for control, want := range wantControls[index] {
							key := [3]uint32{event.Sound, 257, uint32(control)}
							if value, exists := controls[key]; !exists || value != want {
								t.Fatalf("wave preceded its current PCM control %v: got %d, present=%t, want %d", key, value, exists, want)
							}
						}
						waves[event.Sound] = true
					}
				}
				if len(waves) != 2 || len(controls) != 6 {
					t.Fatalf("PCM reconstruction has %d owners and %d controls", len(waves), len(controls))
				}
			})
		}
	}
}
