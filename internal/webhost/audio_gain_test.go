package webhost

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

// gainWireReader retains binary definitions across messages, as a page does.
type gainWireReader struct {
	definitions map[uint32][]byte
}

func (reader *gainWireReader) read(t *testing.T, message outboundMessage) []audioEvent {
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
	var events []audioEvent
	var sound uint32
	for _, operation := range readAudio(t, message.binary) {
		switch operation.op {
		case audioOpSelectSound:
			sound = operation.id
		case audioOpSoundGain:
			events = append(events, audioEvent{Kind: audioSoundGain, Sound: sound, Value: binary.BigEndian.Uint16(operation.operands)})
		case audioOpNoteOn, audioOpNoteOff:
			kind := audioNoteOn
			if operation.op == audioOpNoteOff {
				kind = audioNoteOff
			}
			events = append(events, audioEvent{Kind: kind, Sound: sound, Channel: operation.operands[0], Note: operation.operands[1], Velocity: operation.operands[2]})
		case audioOpAllOff:
			events = append(events, audioEvent{Kind: audioAllOff})
		case audioOpStopSound:
			events = append(events, audioEvent{Kind: audioStopSound, Sound: sound})
		case audioOpDefine:
			if reader.definitions == nil {
				reader.definitions = make(map[uint32][]byte)
			}
			reader.definitions[operation.id] = bytes.Clone(operation.data)
		case audioOpForget:
			clear(reader.definitions)
		case audioOpPlayWave:
			pcm, exists := reader.definitions[operation.id]
			if !exists {
				t.Fatalf("wave refers to an undefined sample %d", operation.id)
			}
			events = append(events, audioEvent{Kind: audioPlayWave, Sound: sound, Channels: operation.operands[0], Rate: binary.BigEndian.Uint32(operation.operands[1:]), pcm: pcm})
		default:
			t.Fatalf("unexpected gain-test operation %#x", operation.op)
		}
	}
	return events
}

func TestAudioGainCollectorCarriesZeroAndFractionalOwnedLevels(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			runner := stalledRunner(t, 2)
			runner.protocol, runner.soundOwnership = protocol, true
			runner.audio = &audioCollector{}
			runner.audio.SoundGain(0x89abcdef, 0)
			runner.audio.SoundGain(17, 2500)
			runner.flushAudio()
			if len(runner.outText) != 1 {
				t.Fatal("collector did not send both gain events")
			}
			message := <-runner.outText
			if protocol == protocolStream {
				want := []byte{'W', 'F', 'A', '2', 0x07, 0x89, 0xab, 0xcd, 0xef, 0x09, 0, 0, 0x07, 0, 0, 0, 17, 0x09, 0x09, 0xc4}
				if !bytes.Equal(message.binary, want) {
					t.Fatalf("gain wire = % x, want % x", message.binary, want)
				}
			}
			var reader gainWireReader
			want := []audioEvent{{Kind: audioSoundGain, Sound: 0x89abcdef}, {Kind: audioSoundGain, Sound: 17, Value: 2500}}
			if got := reader.read(t, message); !reflect.DeepEqual(got, want) {
				t.Fatalf("owned gain events = %+v, want %+v", got, want)
			}
			samples := []int16{-32768, 4000}
			for _, owner := range []uint32{0x89abcdef, 17} {
				runner.audio.AudioEvent(backend.AudioHandle(owner), smaf.Event{Type: smaf.EventNoteOn, Note: 60, Velocity: 100})
				runner.audio.AudioEvent(backend.AudioHandle(owner), smaf.Event{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8000, Wave: samples})
			}
			clear(samples)
			runner.flushAudio()
			if len(runner.outText) != 1 {
				t.Fatal("collector did not send the owned sources")
			}
			want = nil
			for _, owner := range []uint32{0x89abcdef, 17} {
				want = append(want, audioEvent{Kind: audioNoteOn, Sound: owner, Note: 60, Velocity: 100},
					audioEvent{Kind: audioPlayWave, Sound: owner, Channels: 1, Rate: 8000, pcm: []byte{0, 0x80, 0xa0, 0x0f}})
			}
			if got := reader.read(t, <-runner.outText); !reflect.DeepEqual(got, want) {
				t.Fatalf("owned sources were scaled or retained borrowed samples: %+v", got)
			}
		})
	}
}

func TestLegacyAudioGainScalesFutureOutputWithoutChangingOwnedInput(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			runner := stalledRunner(t, 2)
			runner.protocol = protocol
			var reader gainWireReader
			pcm := []byte{0, 0x80, 0x60, 0xf0, 0xa0, 0x0f, 0xff, 0x7f}
			quarter := []byte{0, 0xe0, 0x18, 0xfc, 0xe8, 3, 0xff, 0x1f}
			wave := func(sound uint32, samples []byte) audioEvent {
				return audioEvent{Kind: audioPlayWave, Sound: sound, Channels: 1, Rate: 8000, pcm: samples}
			}
			check := func(events, want []audioEvent) {
				t.Helper()
				original := append([]audioEvent(nil), events...)
				for index := range original {
					original[index].pcm = bytes.Clone(original[index].pcm)
				}
				if !runner.sendAudio(events, true) {
					t.Fatal("audio batch unexpectedly dropped")
				}
				if !reflect.DeepEqual(events, original) {
					t.Fatalf("legacy conversion changed its owned input: got %+v, want %+v", events, original)
				}
				if len(runner.outText) != 1 {
					t.Fatal("legacy output did not reach the page")
				}
				message := <-runner.outText
				if strings.Contains(message.text, `"sound"`) || strings.Contains(message.text, `"soundGain"`) || strings.Contains(message.text, `"stopSound"`) {
					t.Fatalf("legacy JSON contains unsupported ownership: %s", message.text)
				}
				if message.binary != nil {
					for _, operation := range readAudio(t, message.binary) {
						if operation.op == audioOpSelectSound || operation.op == audioOpSoundGain || operation.op == audioOpStopSound {
							t.Fatalf("legacy binary contains unsupported operation %#x", operation.op)
						}
					}
				}
				if got := reader.read(t, message); !reflect.DeepEqual(got, want) {
					t.Fatalf("legacy output = %+v, want %+v", got, want)
				}
			}
			check([]audioEvent{
				{Kind: audioSoundGain, Sound: 17, Value: 2500},
				{Kind: audioNoteOn, Sound: 17, Channel: 2, Note: 60, Velocity: 100}, wave(17, pcm),
				{Kind: audioSoundGain, Sound: 18, Value: 10000},
				{Kind: audioNoteOn, Sound: 18, Channel: 2, Note: 61, Velocity: 80}, wave(18, pcm),
			}, []audioEvent{
				{Kind: audioNoteOn, Channel: 2, Note: 60, Velocity: 25}, wave(0, quarter),
				{Kind: audioNoteOn, Channel: 2, Note: 61, Velocity: 80}, wave(0, pcm),
			})
			check([]audioEvent{
				{Kind: audioSoundGain, Sound: 17},
				{Kind: audioNoteOn, Sound: 17, Channel: 2, Note: 62, Velocity: 100}, wave(17, pcm),
			}, []audioEvent{
				{Kind: audioNoteOff, Channel: 2, Note: 60},
				{Kind: audioNoteOn, Channel: 2, Note: 62}, wave(0, make([]byte, len(pcm))),
			})
			check([]audioEvent{
				{Kind: audioSoundGain, Sound: 17, Value: 2500},
				{Kind: audioNoteOn, Sound: 17, Channel: 2, Note: 63, Velocity: 100}, wave(17, pcm),
			}, []audioEvent{{Kind: audioNoteOn, Channel: 2, Note: 63, Velocity: 25}, wave(0, quarter)})
			check([]audioEvent{
				{Kind: audioAllOff}, {Kind: audioNoteOn, Sound: 17, Channel: 2, Note: 64, Velocity: 100}, wave(17, pcm),
			}, []audioEvent{{Kind: audioAllOff}, {Kind: audioNoteOn, Channel: 2, Note: 64, Velocity: 100}, wave(0, pcm)})
			if !runner.sendAudio([]audioEvent{{Kind: audioSoundGain, Sound: 18}}, true) || len(runner.outText) != 0 {
				t.Fatal("global reset retained a held note from another owner")
			}
		})
	}
}

func TestDroppedAudioGainRetriesGlobalResetOnEmptyTick(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			runner := stalledRunner(t, 1)
			runner.protocol, runner.soundOwnership = protocol, true
			runner.audio = &audioCollector{}
			runner.audio.AudioEvent(17, smaf.Event{Type: smaf.EventNoteOn, Note: 60, Velocity: 100})
			runner.flushAudio()
			runner.audio.SoundGain(17, 0)
			requireAudioFlushReturns(t, runner)
			if !runner.audioNeedsReset || runner.shed.Load() != 1 {
				t.Fatal("a lost mute did not retain an output recovery request")
			}
			requireAudioFlushReturns(t, runner)
			if !runner.audioNeedsReset {
				t.Fatal("an empty tick forgot a lost mute while the queue stayed full")
			}
			<-runner.outText
			requireAudioFlushReturns(t, runner)
			var reader gainWireReader
			if len(runner.outText) != 1 {
				t.Fatal("lost-mute recovery did not reach the page")
			}
			if got := reader.read(t, <-runner.outText); !reflect.DeepEqual(got, []audioEvent{{Kind: audioAllOff}}) || runner.audioNeedsReset {
				t.Fatalf("lost-mute recovery = %+v, pending=%t", got, runner.audioNeedsReset)
			}
		})
	}
}
