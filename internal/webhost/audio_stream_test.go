package webhost

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
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
		case audioOpProgramChange:
			operation.operands = take(2)
		case audioOpAllOff, audioOpForget:
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
	if len(next) != 3 || next[0].op != audioOpForget || next[1].op != audioOpDefine || next[2].op != audioOpPlayWave {
		t.Fatalf("after a drop %+v, want forget, define, play", next)
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
