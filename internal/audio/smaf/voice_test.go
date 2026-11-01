package smaf

import (
	"bytes"
	"testing"
)

// handsetVoiceMessage is an authored `43 03` definition for bank and program:
// LFO 1, feedback 4, serial algorithm; a half-pitch abs-sine modulator with a
// decaying envelope over a percussive sine carrier.
func handsetVoiceMessage(index, bank, program uint8) []byte {
	return []byte{0x43, 0x03, index, bank, program, 0x60, 0x01,
		0x01, 0x78, 0xb7, 0x30, 0xa2,
		0x15, 0x70, 0xf0, 0x00, 0xa0,
		0xf7}
}

// handsetSetup frames messages as an HPS setup chunk does: FF F0, a one-byte
// size counting the final F7, then the message.
func handsetSetup(messages ...[]byte) []byte {
	var data []byte
	for _, message := range messages {
		data = append(data, 0xff, 0xf0, byte(len(message)))
		data = append(data, message...)
	}
	return chunk("Mtsu", data)
}

func handsetScore(track byte, status [2]byte, children ...[]byte) []byte {
	header := []byte{byte(HandyPhoneStandard), 0, 0, 0, status[0], status[1]}
	for _, child := range children {
		header = append(header, child...)
	}
	return chunk("MTR"+string([]byte{track}), header)
}

func smafFile(chunks ...[]byte) []byte {
	var body []byte
	for _, part := range chunks {
		body = append(body, part...)
	}
	return chunk("MMMD", body)
}

// mobileVoiceBody is an authored two-operator mobile body: three global bytes
// and seven per operator.
var mobileVoiceBody = []byte{
	0x00, 0x79, 0x41, // centre pan, normal octave; LFO 1, pan off, parallel algorithm
	0x23, 0x34, 0xf2, 0x42, 0x40, 0x10, 0x2b,
	0x13, 0x62, 0xf3, 0x02, 0x41, 0x30, 0x80,
}

// packSevenBit is the inverse of unpackSevenBit, for authoring test messages.
func packSevenBit(data []byte) []byte {
	var packed []byte
	for start := 0; start < len(data); start += 7 {
		group := data[start:min(start+7, len(data))]
		var high byte
		for index, value := range group {
			if value&0x80 != 0 {
				high |= 0x40 >> index
			}
		}
		packed = append(packed, high)
		for _, value := range group {
			packed = append(packed, value&0x7f)
		}
	}
	return packed
}

func mobileVoiceMessage(form, bankMSB, bankLSB, program, drumNote byte, body []byte) []byte {
	if form == 0x06 {
		body = packSevenBit(body)
	}
	message := []byte{0x43, 0x79, form, 0x7f, 0x01, bankMSB, bankLSB, program, drumNote, 0x00}
	message = append(message, body...)
	return append(message, 0xf7)
}

func TestHandsetVoiceExclusiveDecodesOperatorFields(t *testing.T) {
	key, voice, ok := parseVoiceExclusive(handsetVoiceMessage(2, 0x80, 36))
	if !ok {
		t.Fatal("handset voice was not read")
	}
	if key != (voiceKey{handset: true, bankMSB: 0x80, program: 36}) {
		t.Fatalf("key = %+v", key)
	}
	if voice.Algorithm != 0 || voice.LFO != 1 || voice.BasicOctave != 1 || voice.OperatorCount() != 2 {
		t.Fatalf("globals = %+v", voice)
	}
	modulator := Operator{Multiple: 0, KeyScaleRate: true, Release: 7, Decay: 8, Attack: 11, SustainLevel: 7,
		TotalLevel: 12, VibratoDepth: 2, TremoloDepth: 2, Wave: 2, Feedback: 4, SustainRate: 7}
	if voice.Operators[0] != modulator {
		t.Fatalf("modulator = %+v, want %+v", voice.Operators[0], modulator)
	}
	// 0x15 sets the sustained envelope type, so the carrier holds its level.
	carrier := Operator{Multiple: 1, KeyScaleRate: true, Release: 7, Attack: 15, VibratoDepth: 2, TremoloDepth: 2}
	if voice.Operators[1] != carrier {
		t.Fatalf("carrier = %+v, want %+v", voice.Operators[1], carrier)
	}
}

func TestMobileVoicePackedAndDirectFormsAgree(t *testing.T) {
	_, packed, ok := parseVoiceExclusive(mobileVoiceMessage(0x06, 0x7c, 0x01, 11, 0, mobileVoiceBody))
	if !ok {
		t.Fatal("packed mobile voice was not read")
	}
	key, direct, ok := parseVoiceExclusive(mobileVoiceMessage(0x07, 0x7c, 0x01, 11, 0, mobileVoiceBody))
	if !ok {
		t.Fatal("direct mobile voice was not read")
	}
	if packed != direct {
		t.Fatalf("packed %+v differs from direct %+v", packed, direct)
	}
	if key != (voiceKey{bankMSB: 0x7c, bankLSB: 0x01, program: 11}) {
		t.Fatalf("key = %+v", key)
	}
	if direct.Algorithm != 1 || direct.LFO != 1 || direct.BasicOctave != 1 {
		t.Fatalf("algorithm = %d", direct.Algorithm)
	}
	want := Operator{SustainRate: 2, KeyScaleRate: true, Release: 3, Decay: 4, Attack: 15, SustainLevel: 2,
		TotalLevel: 16, KeyScaleLevel: 2, TremoloDepth: 2, Multiple: 1, Wave: 5, Feedback: 3}
	if direct.Operators[0] != want {
		t.Fatalf("first operator = %+v, want %+v", direct.Operators[0], want)
	}
	// The second operator's wave byte has its top bit set, which the packed
	// form carries in its group's leading byte.
	if direct.Operators[1].Wave != 16 || direct.Operators[1].Multiple != 3 || !direct.Operators[1].Vibrato || direct.Operators[1].Feedback != 0 {
		t.Fatalf("second operator = %+v", direct.Operators[1])
	}
}

func TestMobileFourOperatorVoiceKeepsFeedbackOnlyWhereTheAlgorithmHasIt(t *testing.T) {
	body := append([]byte{0x00, 0x79, 0x05}, bytes.Repeat([]byte{0x00, 0x00, 0xf0, 0x00, 0x00, 0x10, 0x07}, 4)...)
	_, voice, ok := parseVoiceExclusive(mobileVoiceMessage(0x06, 0x7c, 0x01, 3, 0, body))
	if !ok || voice.OperatorCount() != 4 {
		t.Fatalf("four-operator voice = %+v, %v", voice, ok)
	}
	for index, want := range []uint8{7, 0, 7, 0} {
		if voice.Operators[index].Feedback != want {
			t.Fatalf("operator %d feedback = %d, want %d", index, voice.Operators[index].Feedback, want)
		}
	}
}

func TestVoiceExclusiveRefusesWhatIsNotAnFMVoice(t *testing.T) {
	sampled := mobileVoiceMessage(0x07, 0x7c, 0x01, 11, 0, mobileVoiceBody)
	sampled[9] = 1
	for name, message := range map[string][]byte{
		"device command":   {0x43, 0x79, 0x06, 0x7f, 0x7f, 0xf7},
		"other maker":      {0x41, 0x03, 0, 0, 0},
		"sampled voice":    sampled,
		"short handset":    handsetVoiceMessage(0, 0, 1)[:12],
		"short mobile":     mobileVoiceMessage(0x07, 0x7c, 0x01, 11, 0, mobileVoiceBody[:10]),
		"handset 4-op":     append([]byte{0x43, 0x03, 0, 0, 1, 0x02, 0x01}, make([]byte, 10)...),
		"empty":            nil,
		"framing only":     {0xf0, 0xf7},
		"mobile too short": {0x43, 0x79, 0x07, 0x7f, 0x01},
	} {
		if _, _, ok := parseVoiceExclusive(message); ok {
			t.Errorf("%s was read as a voice", name)
		}
	}
}

func TestVoiceEncodingRoundTripsAndRefusesOutOfRangeBytes(t *testing.T) {
	_, voice, ok := parseVoiceExclusive(handsetVoiceMessage(0, 0, 5))
	if !ok {
		t.Fatal("voice was not read")
	}
	encoded := voice.Encode()
	if len(encoded) != voiceHeaderBytes+2*voiceOperatorBytes {
		t.Fatalf("two-operator encoding is %d bytes", len(encoded))
	}
	decoded, ok := DecodeVoice(encoded)
	if !ok || decoded != voice {
		t.Fatalf("round trip = %+v, %v; want %+v", decoded, ok, voice)
	}
	for position := range encoded {
		bad := bytes.Clone(encoded)
		bad[position] = 0xff
		if _, ok := DecodeVoice(bad); ok {
			t.Errorf("byte %d out of range was accepted", position)
		}
	}
	if _, ok := DecodeVoice(encoded[:len(encoded)-1]); ok {
		t.Error("a truncated encoding was accepted")
	}
	if _, ok := DecodeVoice(append(bytes.Clone(encoded), 0)); ok {
		t.Error("an overlong encoding was accepted")
	}
}

func voiceNotes(events []Event) []Event {
	var notes []Event
	for _, event := range events {
		if event.Type == EventNoteOn && event.Velocity != 0 {
			notes = append(notes, event)
		}
	}
	return notes
}

func voiceDefinitions(events []Event) map[uint8][]byte {
	definitions := map[uint8][]byte{}
	for _, event := range events {
		if event.Type == EventVoice {
			definitions[event.Program] = event.SysEx
		}
	}
	return definitions
}

func TestHandsetRhythmNoteSoundsItsDrumVoiceAtTheWrittenPitch(t *testing.T) {
	// Channel 0 is rhythm. Bank 0x80 and program 36 select the defined drum
	// voice; the note's written pitch is octave 0, D#.
	sequence := chunk("Mtsq", []byte{
		0x00, 0x00, 0x31, 0x80,
		0x00, 0x00, 0x30, 36,
		0x00, 0x03, 0x01,
		0x00, 0x00, 0x00, 0x00,
	})
	data := smafFile(handsetScore(1, [2]byte{0x33, 0x33}, handsetSetup(handsetVoiceMessage(0, 0x80, 36)), sequence))
	events, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	definitions := voiceDefinitions(events)
	if len(definitions) != 1 || definitions[1] == nil {
		t.Fatalf("definitions = %v", definitions)
	}
	if voice, ok := DecodeVoice(definitions[1]); !ok || voice.Operators[0].Feedback != 4 {
		t.Fatalf("definition does not decode: %+v %v", voice, ok)
	}
	notes := voiceNotes(events)
	if len(notes) != 1 {
		t.Fatalf("notes = %+v", notes)
	}
	// Note stays the General MIDI drum key for synthesizers without the
	// voice; the voice sounds at the written D#2.
	if note := notes[0]; note.Channel != midiDrumChannel || note.Note != 36 || note.Program != 1 || note.Value != 39 {
		t.Fatalf("note = %+v, want key 36 on the drum channel, slot 1 at pitch 39", note)
	}
	if events[0].Type != EventVoice {
		t.Fatalf("the definition does not lead its instant: %+v", events[0])
	}
}

func TestHandsetVoiceSetUpByOneTrackPlaysOnAnother(t *testing.T) {
	first := handsetScore(1, [2]byte{0x11, 0x11}, handsetSetup(handsetVoiceMessage(0, 0x00, 96)),
		chunk("Mtsq", []byte{0x00, 0x00, 0x00, 0x00}))
	second := handsetScore(2, [2]byte{0x11, 0x11}, chunk("Mtsq", []byte{
		0x00, 0x00, 0x30, 96,
		0x00, 0x29, 0x01, // octave 2, A: MIDI 69
		0x00, 0x00, 0x30, 95,
		0x00, 0x29, 0x01,
		0x00, 0x00, 0x00, 0x00,
	}))
	notes := voiceNotes(Play(smafFile(first, second)))
	if len(notes) != 2 {
		t.Fatalf("notes = %+v", notes)
	}
	if notes[0].Program != 1 || notes[0].Value != 69 || notes[0].Note != 69 {
		t.Fatalf("defined program played %+v", notes[0])
	}
	if notes[1].Program != 0 || notes[1].Value != 0 {
		t.Fatalf("undefined program carried a voice: %+v", notes[1])
	}
}

func mobileScore(children ...[]byte) []byte {
	header := []byte{byte(MobileStandardNoCompress), 0, 0, 0}
	status := make([]byte, 16)
	header = append(header, status...)
	for _, child := range children {
		header = append(header, child...)
	}
	return chunk("MTR\x05", header)
}

func mobileSetup(messages ...[]byte) []byte {
	var data []byte
	for _, message := range messages {
		data = append(data, 0xf0, byte(len(message)))
		data = append(data, message...)
	}
	return chunk("Mtsu", data)
}

func TestMobileDrumKeyVoiceWinsOverTheKitsVoice(t *testing.T) {
	kit := mobileVoiceMessage(0x07, 0x7d, 0x00, 0x00, 0, mobileVoiceBody)
	snare := mobileVoiceMessage(0x07, 0x7d, 0x00, 0x00, 38, mobileVoiceBody)
	sequence := chunk("Mtsq", []byte{
		0x00, 0xb0, 0x00, 0x7d, // bank MSB 0x7d: a drum kit
		0x00, 0xc0, 0x00,
		0x00, 0x90, 38, 0x40, 0x01,
		0x00, 0x90, 36, 0x40, 0x01,
		0x00, 0xff, 0x2f, 0x00,
	})
	notes := voiceNotes(Play(smafFile(mobileScore(mobileSetup(kit, snare), sequence))))
	if len(notes) != 2 {
		t.Fatalf("notes = %+v", notes)
	}
	if notes[0].Channel != midiDrumChannel || notes[0].Program != 2 || notes[0].Value != 38 {
		t.Fatalf("snare = %+v, want its own voice in slot 2", notes[0])
	}
	if notes[1].Program != 1 || notes[1].Value != 36 {
		t.Fatalf("kick = %+v, want the kit voice in slot 1", notes[1])
	}
}

func TestInlineVoiceRedefinitionAppliesFromItsTime(t *testing.T) {
	first := mobileVoiceMessage(0x07, 0x7c, 0x01, 11, 0, mobileVoiceBody)
	changed := bytes.Clone(mobileVoiceBody)
	changed[6] = 0x30 // a louder first operator
	second := mobileVoiceMessage(0x07, 0x7c, 0x01, 11, 0, changed)
	stream := []byte{
		0x00, 0xb0, 0x00, 0x7c,
		0x00, 0xb0, 0x20, 0x01,
		0x00, 0xc0, 11,
		0x00, 0x90, 60, 0x40, 0x01,
		0x0a, 0xf0, byte(len(second)),
	}
	stream = append(stream, second...)
	stream = append(stream, 0x00, 0x90, 62, 0x40, 0x01, 0x00, 0xff, 0x2f, 0x00)
	events := Play(smafFile(mobileScore(mobileSetup(first), chunk("Mtsq", stream))))
	definitions := voiceDefinitions(events)
	if len(definitions) != 2 {
		t.Fatalf("definitions = %v", definitions)
	}
	notes := voiceNotes(events)
	if len(notes) != 2 || notes[0].Program != 1 || notes[1].Program != 2 {
		t.Fatalf("notes = %+v, want slot 1 before the redefinition and 2 after", notes)
	}
	for _, event := range events {
		if event.Type == EventVoice && event.Program == 2 && event.Time != notes[1].Time {
			t.Fatalf("redefinition at %d ms, second note at %d ms", event.Time, notes[1].Time)
		}
	}
}

func TestDefinedAmbienceVoiceNeedsNoStandInLayers(t *testing.T) {
	ambience := mobileVoiceMessage(0x07, 0x7c, 0x01, 0x62, 0, mobileVoiceBody)
	sequence := chunk("Mtsq", []byte{
		0x00, 0xb0, 0x00, 0x7c,
		0x00, 0xb0, 0x20, 0x01,
		0x00, 0xc0, 0x62,
		0x00, 0x90, 60, 0x40, 0x01,
		0x00, 0xff, 0x2f, 0x00,
	})
	notes := voiceNotes(Play(smafFile(mobileScore(mobileSetup(ambience), sequence))))
	if len(notes) != 1 || notes[0].Program != 1 {
		t.Fatalf("notes = %+v, want the defined voice alone", notes)
	}
	withoutVoice := voiceNotes(Play(smafFile(mobileScore(sequence))))
	if len(withoutVoice) != 3 {
		t.Fatalf("undefined ambience voice played %d notes, want the note and two layers", len(withoutVoice))
	}
}

func TestVoiceSlotsStopAtTheirBound(t *testing.T) {
	var messages [][]byte
	for program := range maxVoiceSlots + 3 {
		messages = append(messages, handsetVoiceMessage(0, byte(program>>7), byte(program&0x7f)))
	}
	events := Play(smafFile(handsetScore(1, [2]byte{0x11, 0x11}, handsetSetup(messages...), chunk("Mtsq", []byte{0x00, 0x00, 0x00, 0x00}))))
	definitions := voiceDefinitions(events)
	if len(definitions) != maxVoiceSlots {
		t.Fatalf("%d definitions kept, want %d", len(definitions), maxVoiceSlots)
	}
	if _, ok := definitions[maxVoiceSlots]; !ok {
		t.Fatal("the last slot is missing")
	}
}
