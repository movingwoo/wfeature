package webhost

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

// audioOperation is one operation read back from a protocol 2 sound message.
type audioOperation struct {
	op       byte
	operands []byte
	id       uint32
	data     []byte
}

// readAudio splits a message the way the page does.
func readAudio(t *testing.T, message []byte) []audioOperation {
	t.Helper()
	if !bytes.HasPrefix(message, audioMagic) {
		t.Fatalf("not a sound message: % x", message[:min(len(message), 8)])
	}
	var operations []audioOperation
	rest := message[len(audioMagic):]
	take := func(count int) []byte {
		if len(rest) < count {
			t.Fatalf("message ends inside an operation: % x", message)
		}
		taken := rest[:count]
		rest = rest[count:]
		return taken
	}
	for len(rest) > 0 {
		operation := audioOperation{op: take(1)[0]}
		switch operation.op {
		case audioOpNoteOn, audioOpNoteOff, audioOpControlChange, audioOpPitchBend:
			operation.operands = take(3)
		case audioOpProgramChange, audioOpSoundGain:
			operation.operands = take(2)
		case audioOpNoteResume:
			operation.operands = take(7)
		case audioOpAllOff, audioOpForget, audioOpStopSound:
		case audioOpSelectSound:
			operation.id = binary.BigEndian.Uint32(take(4))
		case audioOpDefine:
			operation.id = binary.BigEndian.Uint32(take(4))
			operation.data = take(int(binary.BigEndian.Uint32(take(4))))
		case audioOpPlayWave:
			operation.id = binary.BigEndian.Uint32(take(4))
			operation.operands = take(5)
		case audioOpSysEx:
			operation.id = binary.BigEndian.Uint32(take(4))
		default:
			t.Fatalf("unknown operation %#x", operation.op)
		}
		operations = append(operations, operation)
	}
	return operations
}

func TestStreamAudioEncodesEachCall(t *testing.T) {
	var definitions audioDefinitions
	got := definitions.encode([]audioEvent{
		{Kind: audioNoteOn, Channel: 1, Note: 60, Velocity: 100},
		{Kind: audioNoteOff, Channel: 1, Note: 60, Velocity: 64},
		{Kind: audioProgramChange, Channel: 2, Program: 5},
		{Kind: audioControlChange, Channel: 3, Control: 7, Value: 90},
		{Kind: audioPitchBend, Channel: 4, Value: 0x2001},
		{Kind: audioAllOff},
	})
	want := append(bytes.Clone(audioMagic),
		audioOpNoteOn, 1, 60, 100,
		audioOpNoteOff, 1, 60, 64,
		audioOpProgramChange, 2, 5,
		audioOpControlChange, 3, 7, 90,
		audioOpPitchBend, 4, 0x20, 0x01,
		audioOpAllOff)
	if !bytes.Equal(got, want) {
		t.Fatalf("encoded % x\nwant    % x", got, want)
	}
}

func TestStreamAudioCarriesEachSampleOnce(t *testing.T) {
	var definitions audioDefinitions
	effect := []byte{1, 0, 2, 0, 3, 0}
	other := []byte{9, 0}
	first := readAudio(t, definitions.encode([]audioEvent{{Kind: audioPlayWave, Channels: 1, Rate: 8000, pcm: effect}}))
	if len(first) != 2 || first[0].op != audioOpDefine || !bytes.Equal(first[0].data, effect) || first[1].op != audioOpPlayWave || first[1].id != first[0].id {
		t.Fatalf("a first play is %+v, want a definition and a play of it", first)
	}
	if rate := binary.BigEndian.Uint32(first[1].operands[1:]); first[1].operands[0] != 1 || rate != 8000 {
		t.Fatalf("play operands % x", first[1].operands)
	}
	again := readAudio(t, definitions.encode([]audioEvent{
		{Kind: audioPlayWave, Channels: 1, Rate: 8000, pcm: bytes.Clone(effect)},
		{Kind: audioPlayWave, Channels: 1, Rate: 11025, pcm: other},
		{Kind: audioSysEx, raw: []byte{0xf0, 0x7e, 0xf7}},
		{Kind: audioSysEx, raw: []byte{0xf0, 0x7e, 0xf7}},
	}))
	// The repeated effect is named; the new sound and the SysEx are defined
	// once each, under ids that were never used before.
	ops := []byte{}
	for _, operation := range again {
		ops = append(ops, operation.op)
	}
	if !bytes.Equal(ops, []byte{audioOpPlayWave, audioOpDefine, audioOpPlayWave, audioOpDefine, audioOpSysEx, audioOpSysEx}) {
		t.Fatalf("operations % x", ops)
	}
	if again[0].id != first[0].id || again[1].id == first[0].id || again[3].id == again[1].id || again[4].id != again[3].id || again[5].id != again[3].id {
		t.Fatalf("ids %+v", again)
	}
}

func TestStreamAudioStartsOverPastTheBudget(t *testing.T) {
	var definitions audioDefinitions
	large := bytes.Repeat([]byte{1}, audioDefinitionBudget/2+1)
	larger := bytes.Repeat([]byte{2}, audioDefinitionBudget/2+1)
	definitions.encode([]audioEvent{{Kind: audioPlayWave, Channels: 1, Rate: 8000, pcm: large}})
	operations := readAudio(t, definitions.encode([]audioEvent{{Kind: audioPlayWave, Channels: 1, Rate: 8000, pcm: larger}}))
	if len(operations) != 3 || operations[0].op != audioOpForget || operations[1].op != audioOpDefine || operations[1].id != 2 {
		t.Fatalf("operations past the budget %+v, want forget, then define id 2", operations)
	}
	// The first sound is no longer held, so it is defined again, under a new id.
	operations = readAudio(t, definitions.encode([]audioEvent{{Kind: audioPlayWave, Channels: 1, Rate: 8000, pcm: large}}))
	if operations[0].op != audioOpForget || operations[1].op != audioOpDefine || operations[1].id != 3 {
		t.Fatalf("operations %+v", operations)
	}
}

func TestStreamAudioStartsOverAfterADroppedMessage(t *testing.T) {
	runner := stalledRunner(t, 1)
	runner.protocol = protocolStream
	runner.audio = &audioCollector{}
	effect := []int16{100, -100}
	for round := 0; round < 2; round++ {
		runner.audio.PlayWave(1, 8000, effect)
		runner.flushAudio()
	}
	if shed := runner.shed.Load(); shed != 1 {
		t.Fatalf("shed = %d, want the second message dropped", shed)
	}
	delivered := <-runner.outText
	if operations := readAudio(t, delivered.binary); operations[0].op != audioOpDefine {
		t.Fatalf("first message %+v", operations)
	}
	// The dropped message only named the sound, but the page's store is not
	// known any more, so the next message clears it and defines again.
	runner.audio.PlayWave(1, 8000, effect)
	runner.flushAudio()
	next := readAudio(t, (<-runner.outText).binary)
	if len(next) != 4 || next[0].op != audioOpForget || next[1].op != audioOpAllOff || next[2].op != audioOpDefine || next[3].op != audioOpPlayWave {
		t.Fatalf("after a drop %+v, want forget, all off, define, play", next)
	}
}

func TestFirstProtocolAudioIsTheJSONItAlwaysWas(t *testing.T) {
	runner := stalledRunner(t, 2)
	runner.audio = &audioCollector{}
	runner.audio.PlayWave(2, 22050, []int16{1, -1})
	runner.audio.MIDISysEx([]byte{0xf0, 0x01, 0xf7})
	runner.flushAudio()
	message := <-runner.outText
	if message.binary != nil || !message.audio {
		t.Fatal("the first protocol's sound is not a text message")
	}
	var decoded struct {
		Kind  string           `json:"kind"`
		Audio []map[string]any `json:"audio"`
	}
	if err := json.Unmarshal([]byte(message.text), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != serverAudio || len(decoded.Audio) != 2 ||
		decoded.Audio[0]["samples"] != "AQD//w==" || decoded.Audio[0]["channels"] != 2.0 || decoded.Audio[0]["rate"] != 22050.0 ||
		decoded.Audio[1]["data"] != "8AH3" {
		t.Fatalf("audio JSON %s", message.text)
	}
}

func TestAStoppedGameSilencesThePageInItsProtocol(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		runner := stalledRunner(t, 2)
		runner.protocol = protocol
		runner.audio = &audioCollector{}
		runner.stopGame()
		message := <-runner.outText
		if protocol == protocolStream {
			if operations := readAudio(t, message.binary); len(operations) != 1 || operations[0].op != audioOpAllOff {
				t.Fatalf("stop sent %+v", operations)
			}
		} else if message.text != `{"kind":"audio","audio":[{"kind":"allOff"}]}` {
			t.Fatalf("stop sent %s", message.text)
		}
	}
}

func TestStreamAudioOwnershipIsLocalToEachMessage(t *testing.T) {
	var definitions audioDefinitions
	const sound uint32 = 0x89abcdef
	message := definitions.encode([]audioEvent{
		{Kind: audioNoteOn, Sound: sound, Channel: 1, Note: 60, Velocity: 100},
		{Kind: audioStopSound, Sound: sound},
		{Kind: audioAllOff},
		{Kind: audioNoteOff, Channel: 1, Note: 60},
	})
	want := append(bytes.Clone(audioMagic),
		audioOpSelectSound, 0x89, 0xab, 0xcd, 0xef,
		audioOpNoteOn, 1, 60, 100,
		audioOpStopSound, audioOpAllOff,
		audioOpSelectSound, 0, 0, 0, 0,
		audioOpNoteOff, 1, 60, 0)
	if !bytes.Equal(message, want) {
		t.Fatalf("owned message % x, want % x", message, want)
	}
	for round := 0; round < 2; round++ {
		operations := readAudio(t, definitions.encode([]audioEvent{{Kind: audioStopSound, Sound: sound}}))
		if len(operations) != 2 || operations[0].op != audioOpSelectSound || operations[0].id != sound || operations[1].op != audioOpStopSound {
			t.Fatalf("message %d inherited another message's selection: %+v", round, operations)
		}
	}
	zero := readAudio(t, definitions.encode([]audioEvent{{Kind: audioStopSound}}))
	if len(zero) != 1 || zero[0].op != audioOpStopSound {
		t.Fatalf("default sound should need no selection: %+v", zero)
	}
}

func TestStreamAudioSharesDefinitionsAcrossSoundOwners(t *testing.T) {
	var definitions audioDefinitions
	pcm := []byte{0, 0x40, 0, 0xc0}
	message := definitions.encode([]audioEvent{
		{Kind: audioPlayWave, Sound: 7, Channels: 1, Rate: 8000, pcm: pcm},
		{Kind: audioStopSound, Sound: 7},
		{Kind: audioPlayWave, Sound: 9, Channels: 1, Rate: 8000, pcm: bytes.Clone(pcm)},
	})
	operations := readAudio(t, message)
	wantOps := []byte{audioOpSelectSound, audioOpDefine, audioOpPlayWave, audioOpStopSound, audioOpSelectSound, audioOpPlayWave}
	var gotOps []byte
	for _, operation := range operations {
		gotOps = append(gotOps, operation.op)
	}
	if !bytes.Equal(gotOps, wantOps) || operations[0].id != 7 || operations[4].id != 9 ||
		operations[1].id != operations[2].id || operations[2].id != operations[5].id {
		t.Fatalf("owners did not share the same definition: %+v", operations)
	}
	firstID := operations[1].id
	definitions.dropped()
	operations = readAudio(t, definitions.encode([]audioEvent{
		{Kind: audioPlayWave, Sound: 9, Channels: 1, Rate: 8000, pcm: pcm},
		{Kind: audioStopSound, Sound: 9},
	}))
	if len(operations) != 5 || operations[0].op != audioOpForget || operations[1].op != audioOpSelectSound || operations[1].id != 9 ||
		operations[2].op != audioOpDefine || operations[2].id == firstID || operations[3].id != operations[2].id || operations[4].op != audioOpStopSound {
		t.Fatalf("dropped message did not reset definitions independently of ownership: %+v", operations)
	}
}

func TestSessionNegotiatesSoundOwnershipForBothProtocols(t *testing.T) {
	for _, test := range []struct {
		query  string
		owned  bool
		stream bool
		resume bool
		timing bool
	}{
		{"", false, false, false, false},
		{"sound=owned", true, false, false, false},
		{"sound=resume", true, false, true, false},
		{"protocol=2", false, true, false, false},
		{"protocol=2&sound=owned", true, true, false, false},
		{"protocol=2&sound=resume", true, true, true, false},
		{"protocol=2&sound=unknown", false, true, false, false},
		{"sound=resume&timing=1", true, false, true, true},
		{"protocol=2&sound=resume&timing=1", true, true, true, true},
		{"protocol=2&sound=owned&timing=1", true, true, false, false},
		{"protocol=2&sound=resume&timing=unknown", true, true, true, false},
	} {
		t.Run(test.query, func(t *testing.T) {
			server, url := resumeFixture(t)
			connection := dialSession(t, url+"?"+test.query)
			if err := connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			expectMessage(t, connection, serverReady)
			send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
			started := expectMessage(t, connection, serverStarted)
			if started.Started == nil {
				t.Fatal("session did not start")
			}
			server.parkedMu.Lock()
			attached := server.attached[started.Started.Token]
			var owned, resume, timing bool
			var protocol int
			if attached != nil {
				owned, protocol = attached.soundOwnership, attached.protocol
				resume = attached.soundResume
				timing = attached.soundTiming
			}
			server.parkedMu.Unlock()
			if attached == nil || owned != test.owned || resume != test.resume || timing != test.timing || (protocol == protocolStream) != test.stream {
				t.Fatalf("negotiated ownership=%t protocol=%d for %q", owned, protocol, test.query)
			}

			// Use the handler's negotiated capabilities with an isolated runner;
			// touching a live guest's collector would race its frame loop.
			runner := stalledRunner(t, 1)
			runner.protocol, runner.soundOwnership = protocol, owned
			runner.soundResume = resume
			runner.audio = &audioCollector{}
			const sound backend.AudioHandle = 0x89abcdef
			runner.audio.AudioEvent(sound, smaf.Event{Type: smaf.EventNoteOn, Channel: 1, Note: 60, Velocity: 100})
			runner.audio.AudioEvent(9, smaf.Event{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8000, Wave: []int16{16384, -16384}})
			runner.audio.StopSound(sound)
			runner.flushAudio()
			message := <-runner.outText
			if !message.audio || (message.binary != nil) != test.stream {
				t.Fatalf("wrong audio message shape: %+v", message)
			}
			if test.stream {
				operations := readAudio(t, message.binary)
				var selected uint32
				var notes, waves, stops int
				for _, operation := range operations {
					if !owned && (operation.op == audioOpSelectSound || operation.op == audioOpStopSound) {
						t.Fatalf("legacy client received unsupported operation %#x", operation.op)
					}
					switch operation.op {
					case audioOpSelectSound:
						selected = operation.id
					case audioOpNoteOn:
						notes++
						if owned && selected != uint32(sound) || !bytes.Equal(operation.operands, []byte{1, 60, 100}) {
							t.Fatalf("note lost its owner or data: owner=%d operation=%+v", selected, operation)
						}
					case audioOpPlayWave:
						waves++
						if owned && selected != 9 {
							t.Fatalf("PCM owner=%d, want 9", selected)
						}
					case audioOpStopSound:
						stops++
						if selected != uint32(sound) {
							t.Fatalf("stopped owner=%d, want %d", selected, sound)
						}
					}
				}
				if notes != 1 || waves != 1 || owned && stops != 1 || !owned && stops != 0 {
					t.Fatalf("note/wave/stop counts=%d/%d/%d", notes, waves, stops)
				}
			} else {
				var decoded struct {
					Kind  string           `json:"kind"`
					Audio []map[string]any `json:"audio"`
				}
				if err := json.Unmarshal([]byte(message.text), &decoded); err != nil {
					t.Fatal(err)
				}
				wantCount := 2
				if owned {
					wantCount++
				}
				if decoded.Kind != serverAudio || len(decoded.Audio) != wantCount || decoded.Audio[0]["kind"] != audioNoteOn ||
					decoded.Audio[1]["kind"] != audioPlayWave || decoded.Audio[1]["samples"] != "AEAAwA==" {
					t.Fatalf("unexpected collector JSON: %s", message.text)
				}
				if owned {
					if decoded.Audio[0]["sound"] != float64(sound) || decoded.Audio[1]["sound"] != 9.0 ||
						decoded.Audio[2]["kind"] != audioStopSound || decoded.Audio[2]["sound"] != float64(sound) {
						t.Fatalf("JSON lost sound ownership: %s", message.text)
					}
				} else {
					for _, event := range decoded.Audio {
						if _, present := event["sound"]; present {
							t.Fatalf("legacy JSON contains sound ownership: %s", message.text)
						}
					}
				}
			}
		})
	}
}
