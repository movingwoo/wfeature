package smaf

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
)

// These cover the parser's three sequence dialects and the tone map's
// conversions, and are the reference implementation's own cases carried over:
// a port that only proves "does not crash on real files" would not have caught
// a dialect decoded plausibly but wrongly.

func TestHandyVariableNumber(t *testing.T) {
	for _, testCase := range []struct {
		name string
		data []byte
		want uint32
	}{
		{"single byte", []byte{0x42}, 0x42},
		// The handset form is not the MIDI one: the second byte contributes
		// all eight bits and the first is biased by one.
		{"two bytes", []byte{0x82, 0x34}, (0x02+1)<<7 | 0x34},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reader := &reader{data: testCase.data}
			value, ok := reader.handyVariableNumber()
			if !ok || value != testCase.want {
				t.Fatalf("handyVariableNumber(%x) = %d, %v; want %d", testCase.data, value, ok, testCase.want)
			}
		})
	}
}

func TestMobileVariableNumber(t *testing.T) {
	for _, testCase := range []struct {
		name string
		data []byte
		want uint32
	}{
		{"single byte", []byte{0x42}, 0x42},
		{"continuation", []byte{0x81, 0x00}, 128},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reader := &reader{data: testCase.data}
			value, ok := reader.variableNumber()
			if !ok || value != testCase.want {
				t.Fatalf("variableNumber(%x) = %d, %v; want %d", testCase.data, value, ok, testCase.want)
			}
		})
	}
}

func TestMobileReservedStatusBytesBecomeNops(t *testing.T) {
	// duration 0, reserved status 0xa5 with two data bytes, then end of stream.
	events, err := parseSequenceMobile([]byte{0x00, 0xa5, 0x12, 0x34, 0x00, 0xff, 0x2f, 0x00}, newDecodeBudget())
	if err != nil {
		t.Fatal(err)
	}
	for index, event := range events {
		if event.Kind != SeqNop {
			t.Fatalf("event %d is kind %d, want a nop", index, event.Kind)
		}
	}
}

func TestHandySequenceEndingShortDoesNotOverrun(t *testing.T) {
	// A stream whose tail is shorter than the four-byte terminator must stop,
	// not read past it.
	if _, err := parseSequenceHandyLike([]byte{0x00, 0x01, 0x00}, false, newDecodeBudget()); err != nil {
		t.Fatal(err)
	}
}

func TestHandyShortFormControlEvents(t *testing.T) {
	t.Run("pitch bend", func(t *testing.T) {
		events, err := parseSequenceHandyLike([]byte{0x00, 0x00, 0x13, 0x00, 0x00, 0x00, 0x00, 0x00}, false, newDecodeBudget())
		if err != nil {
			t.Fatal(err)
		}
		want := uint16((0x13 - 0x10) * 16384 / 16)
		if !hasEvent(events, func(event SequenceEvent) bool {
			return event.Kind == SeqPitchBend && event.Channel == 0 && event.BendValue == want
		}) {
			t.Fatalf("no pitch bend to %d in %+v", want, events)
		}
	})
	t.Run("expression", func(t *testing.T) {
		events, err := parseSequenceHandyLike([]byte{0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00}, false, newDecodeBudget())
		if err != nil {
			t.Fatal(err)
		}
		if !hasEvent(events, func(event SequenceEvent) bool {
			return event.Kind == SeqExpression && event.Channel == 0 && event.Value == 0x37
		}) {
			t.Fatalf("no short-form expression in %+v", events)
		}
	})
}

func TestHandyNoteCarriesNoVelocity(t *testing.T) {
	// Status 0x49 is channel 1, octave 0, voice 9.
	events, err := parseSequenceHandyLike([]byte{0x00, 0x49, 0x00, 0x00, 0x00, 0x00, 0x00}, false, newDecodeBudget())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, func(event SequenceEvent) bool {
		return event.Kind == SeqNote && event.Channel == 1 && event.Note == 9 && !event.HasVelocity
	}) {
		t.Fatalf("no velocity-free note in %+v", events)
	}
}

func TestExclusiveDelimitingDiffersByDialect(t *testing.T) {
	t.Run("softbank length prefix", func(t *testing.T) {
		events, err := parseSequenceHandyLike([]byte{0x00, 0xff, 0xf0, 0x03, 0x41, 0x42, 0x43, 0x00, 0x00, 0x00, 0x00}, true, newDecodeBudget())
		if err != nil {
			t.Fatal(err)
		}
		if !hasEvent(events, func(event SequenceEvent) bool {
			return event.Kind == SeqExclusive && string(event.Exclusive) == "ABC"
		}) {
			t.Fatalf("no length-prefixed exclusive in %+v", events)
		}
	})
	t.Run("handset length includes terminator", func(t *testing.T) {
		events, err := parseSequenceHandyLike([]byte{0x00, 0xff, 0xf0, 0x03, 0x41, 0x42, 0xf7, 0x00, 0x00, 0x00, 0x00, 0x00}, false, newDecodeBudget())
		if err != nil {
			t.Fatal(err)
		}
		if !hasEvent(events, func(event SequenceEvent) bool {
			return event.Kind == SeqExclusive && string(event.Exclusive) == "AB\xf7"
		}) {
			t.Fatalf("no sized, 0xf7-terminated exclusive in %+v", events)
		}
	})
}

func TestPCMShortFormControlEvents(t *testing.T) {
	t.Run("expression", func(t *testing.T) {
		events, err := parsePCMSequence([]byte{0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x00}, newDecodeBudget())
		if err != nil {
			t.Fatal(err)
		}
		if !hasPCMEvent(events, func(event PCMEvent) bool {
			return event.Kind == PCMEventExpression && event.Channel == 0 && event.Value == 0x37
		}) {
			t.Fatalf("no short-form expression in %+v", events)
		}
	})
	t.Run("pitch bend", func(t *testing.T) {
		events, err := parsePCMSequence([]byte{0x00, 0x00, 0x13, 0x00, 0x00, 0x00, 0x00}, newDecodeBudget())
		if err != nil {
			t.Fatal(err)
		}
		if !hasPCMEvent(events, func(event PCMEvent) bool {
			return event.Kind == PCMEventPitchBend && event.Channel == 0 && event.Value == 0x18
		}) {
			t.Fatalf("no short-form pitch bend in %+v", events)
		}
	})
}

func TestUnknownTopLevelChunkIsKept(t *testing.T) {
	file, err := Parse(buildSMAF(
		chunk("CNTI", []byte{0, 0, 0, 0, 0}),
		chunk("XXXX", []byte{0, 0, 0, 0}),
	))
	if err != nil {
		t.Fatal(err)
	}
	var sawContents, sawUnknown bool
	for _, current := range file.Chunks {
		if current.Kind == ChunkContentsInfo {
			sawContents = true
		}
		if current.Kind == ChunkUnknown && string(current.Tag[:]) == "XXXX" {
			sawUnknown = true
		}
	}
	if !sawContents || !sawUnknown {
		t.Fatalf("contents=%v unknown=%v; want both kept", sawContents, sawUnknown)
	}
}

func TestParseRejectsNonSMAF(t *testing.T) {
	if _, err := Parse([]byte("not a smaf file at all")); err != ErrNotSMAF {
		t.Fatalf("Parse of foreign data = %v, want ErrNotSMAF", err)
	}
	if events := Play([]byte("not a smaf file at all")); events != nil {
		t.Fatalf("Play of foreign data produced %d events, want silence", len(events))
	}
}

func TestMapsYamahaMAProgramToGeneralMIDIFallback(t *testing.T) {
	tones := newToneMap()
	tones.updateControl(1, 0, 0x7c)
	tones.updateControl(1, 32, 0x01)

	channel, program := tones.setProgram(1, 0x22)
	if channel != 0 || program != 81 {
		t.Fatalf("setProgram = (%d, %d), want (0, 81)", channel, program)
	}
}

func TestMapsYamahaRhythmBankToDrumChannel(t *testing.T) {
	tones := newToneMap()
	tones.updateControl(9, 0, 0x7d)
	tones.updateControl(9, 32, 0x00)

	channel, program := tones.setProgram(9, 0x02)
	if channel != midiDrumChannel || program != 0 {
		t.Fatalf("setProgram = (%d, %d), want (%d, 0)", channel, program, midiDrumChannel)
	}
	if note := tones.mapNote(9, 0x1a); note != 41 {
		t.Fatalf("mapNote(9, 0x1a) = %d, want 41", note)
	}
}

func TestMelodyChannelsCompactAroundDrumChannel(t *testing.T) {
	tones := newToneMap()
	for _, testCase := range []struct{ channel, want uint8 }{{1, 0}, {2, 1}, {9, 2}} {
		if real := tones.realChannel(testCase.channel); real != testCase.want {
			t.Fatalf("realChannel(%d) = %d, want %d", testCase.channel, real, testCase.want)
		}
	}
	// A rhythm channel takes the drum channel without consuming an allocation,
	// so the next melodic channel continues the sequence.
	tones.updateControl(10, 0, 0x7d)
	if real := tones.realChannel(10); real != midiDrumChannel {
		t.Fatalf("realChannel(10) = %d, want %d", real, midiDrumChannel)
	}
	if real := tones.realChannel(11); real != 3 {
		t.Fatalf("realChannel(11) = %d, want 3", real)
	}
}

func TestAtmosphereLayersForYamahaAmbienceVoice(t *testing.T) {
	tones := newToneMap()
	tones.updateControl(7, 0, 0x7c)
	tones.updateControl(7, 32, 0x01)
	tones.updateControl(7, 7, 80)

	setup := tones.atmosphereSetup(0, 7, 0x62)
	for _, want := range []Event{
		{Type: EventProgramChange, Channel: 15, Program: 99},
		{Type: EventControlChange, Channel: 0, Control: 91, Value: 92},
		{Type: EventControlChange, Channel: 15, Control: 7, Value: 56},
		{Type: EventControlChange, Channel: 15, Control: 72, Value: 22},
	} {
		if !hasPlayed(setup, want) {
			t.Fatalf("atmosphere setup is missing %+v", want)
		}
	}

	notes := tones.atmosphereNotes(100, 200, 7, 84, 64)
	if !hasPlayedFunc(notes, func(event Event) bool {
		return event.Type == EventNoteOn && event.Channel == 15 && event.Note == 84
	}) {
		t.Fatalf("atmosphere notes are missing the unshifted layer: %+v", notes)
	}
}

func TestMobileNoteWithoutVelocityReusesThePrevious(t *testing.T) {
	tones := newToneMap()
	tones.initTrack(MobileStandardNoCompress, nil, 0)
	events, _ := sequenceEvents([]SequenceEvent{
		{Kind: SeqNote, Channel: 0, Note: 60, Velocity: 96, HasVelocity: true, GateTime: 10},
		{Kind: SeqNote, Channel: 0, Note: 62, GateTime: 10},
	}, 1, 1, 0, false, nil, tones, newDecodeBudget())

	if !hasPlayedFunc(events, func(event Event) bool {
		return event.Type == EventNoteOn && event.Note == 62 && event.Velocity == 96
	}) {
		t.Fatalf("second note did not reuse velocity 96: %+v", events)
	}
}

func TestMobileStreamNotePreservesVelocityMemory(t *testing.T) {
	for _, probe := range []struct {
		name      string
		wave      []byte
		wantWaves int
	}{
		{"mapped", []byte{0x11, 0x1f, 0x40, 0, 128, 255}, 3},
		{"absent", nil, 0},
		{"unsupported-width", []byte{0x13, 0x1f, 0x40, 0, 0, 0, 0}, 0},
		{"unsupported-stereo", []byte{0x91, 0x1f, 0x40, 0, 128, 255, 128}, 0},
	} {
		t.Run(probe.name, func(t *testing.T) {
			sequence := []byte{
				0, 0x80, 60, 1, // Initial omitted velocity is 64 on either channel.
				1, 0x81, 61, 1,
				1, 0x91, 62, 77, 1,
				1, 0x90, 0, 96, 1, // A stream note still stores its explicit velocity.
				1, 0x80, 63, 1,
				1, 0x81, 64, 1, // Another channel keeps its own previous value.
				1, 0x90, 0, 0, 1,
				1, 0x80, 65, 1, // Explicit zero must remain zero when omitted.
				1, 0x90, 66, 88, 1,
				1, 0x80, 0, 1, // An omitted stream velocity preserves the stored value.
				1, 0x80, 67, 1,
				1, 0xff, 0x2f, 0,
			}
			events := decodeMobileNoteFixture(t, sequence, probe.wave)
			assertMobileNoteFixture(t, events, []Event{
				{Type: EventNoteOn, Note: 60, Velocity: 64},
				{Time: 1, Type: EventNoteOn, Channel: 1, Note: 61, Velocity: 64},
				{Time: 2, Type: EventNoteOn, Channel: 1, Note: 62, Velocity: 77},
				{Time: 4, Type: EventNoteOn, Note: 63, Velocity: 96},
				{Time: 5, Type: EventNoteOn, Channel: 1, Note: 64, Velocity: 77},
				{Time: 7, Type: EventNoteOn, Note: 65, Velocity: 0},
				{Time: 8, Type: EventNoteOn, Note: 66, Velocity: 88},
				{Time: 10, Type: EventNoteOn, Note: 67, Velocity: 88},
			})
			var waves []Event
			for _, event := range events {
				if event.Type == EventWave {
					waves = append(waves, event)
				}
			}
			if len(waves) != probe.wantWaves {
				t.Fatalf("stream notes emitted %d waves, want %d", len(waves), probe.wantWaves)
			}
			for index, wave := range waves {
				if wave.Time != []uint32{3, 6, 9}[index] || wave.WaveChannels != 1 || wave.SamplingRate != 8000 ||
					!reflect.DeepEqual(wave.Wave, []int16{-32768, 0, 32512}) {
					t.Fatalf("velocity memory changed stream mapping, timing or raw samples: %+v", wave)
				}
			}
		})
	}
}

func TestMobileResetControllersRestoresVelocityMemory(t *testing.T) {
	for _, seed := range []struct {
		name string
		note byte
	}{{"ordinary", 60}, {"stream", 0}} {
		for _, operand := range []byte{0, 37} {
			t.Run(fmt.Sprintf("%s/operand-%d", seed.name, operand), func(t *testing.T) {
				sequence := []byte{
					0, 0xb0, 0, 0x7c,
					0, 0xb0, 32, 1,
					0, 0xb0, 7, 73,
					0, 0xb0, 10, 23,
					0, 0xc0, 0x22,
					0, 0x91, 70, 77, 1,
					1, 0x90, seed.note, 96, 1,
					1, 0x80, 61, 1,
					1, 0xb0, 121, operand,
					1, 0x80, 0, 1,
					1, 0x80, 62, 1,
					1, 0x81, 71, 1,
					1, 0xc0, 0x22, // The bank selection still maps this program to 81.
					1, 0x90, 0, 101, 1,
					1, 0x80, 63, 1,
					1, 0xff, 0x2f, 0,
				}
				events := decodeMobileNoteFixture(t, sequence, []byte{0x11, 0x1f, 0x40, 0, 128, 255})
				want := []Event{{Type: EventNoteOn, Channel: 1, Note: 70, Velocity: 77}}
				if seed.note != 0 {
					want = append(want, Event{Time: 1, Type: EventNoteOn, Note: seed.note, Velocity: 96})
				}
				want = append(want,
					Event{Time: 2, Type: EventNoteOn, Note: 61, Velocity: 96},
					Event{Time: 5, Type: EventNoteOn, Note: 62, Velocity: 64},
					Event{Time: 6, Type: EventNoteOn, Channel: 1, Note: 71, Velocity: 77},
					Event{Time: 9, Type: EventNoteOn, Note: 63, Velocity: 101})
				assertMobileNoteFixture(t, events, want)
				var controls []Event
				for _, event := range events {
					if event.Type == EventControlChange || event.Type == EventProgramChange {
						controls = append(controls, event)
					}
				}
				if want := []Event{
					{Type: EventControlChange, Control: 0, Value: 0x7c},
					{Type: EventControlChange, Control: 32, Value: 1},
					{Type: EventControlChange, Control: 7, Value: 73},
					{Type: EventControlChange, Control: 10, Value: 23},
					{Type: EventProgramChange, Program: 81},
					{Time: 3, Type: EventControlChange, Control: 121, Value: operand},
					{Time: 7, Type: EventProgramChange, Program: 81},
				}; !reflect.DeepEqual(controls, want) {
					t.Fatalf("velocity reset changed bank, program, gain or pan events:\ngot  %+v\nwant %+v", controls, want)
				}
			})
		}
	}
}

func decodeMobileNoteFixture(t *testing.T, sequence, wave []byte) []Event {
	t.Helper()
	children := [][]byte{chunk("Mtsq", sequence)}
	if wave != nil {
		children = append(children, chunk("Mtsp", chunk("Mwa\x01", wave)))
	}
	events, err := Decode(buildSMAF(limitsScore(MobileStandardNoCompress, children...)))
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func assertMobileNoteFixture(t *testing.T, events, want []Event) {
	t.Helper()
	var notes []Event
	for _, event := range events {
		if event.Type == EventNoteOn {
			notes = append(notes, event)
		}
	}
	if !reflect.DeepEqual(notes, want) {
		t.Fatalf("decoded mobile velocity memory:\ngot  %+v\nwant %+v", notes, want)
	}
}

func TestDurationAppliesBeforeItsEvent(t *testing.T) {
	tones := newToneMap()
	tones.initTrack(MobileStandardNoCompress, nil, 0)
	events, _ := sequenceEvents([]SequenceEvent{
		{Kind: SeqNote, Channel: 0, Note: 60, Velocity: 64, HasVelocity: true, Duration: 5, GateTime: 2},
	}, 4, 4, 0, false, nil, tones, newDecodeBudget())

	// The duration precedes its event, so the note starts at 5*4 and lasts 2*4.
	if !hasPlayedFunc(events, func(event Event) bool {
		return event.Type == EventNoteOn && event.Note == 60 && event.Time == 20
	}) {
		t.Fatalf("note on is not at 20ms: %+v", events)
	}
	if !hasPlayedFunc(events, func(event Event) bool {
		return event.Type == EventNoteOff && event.Note == 60 && event.Time == 28
	}) {
		t.Fatalf("note off is not at 28ms: %+v", events)
	}
}

func TestPCMTrackDurationAppliesBeforeItsEvent(t *testing.T) {
	events := pcmTrackEvents(&PCMAudioTrack{
		Format: PCMAdpcm, Channels: Mono, SamplingFreq: 8000, TimebaseD: 4, TimebaseG: 4,
		Sequence: []PCMEvent{{Kind: PCMEventNop, Duration: 5}},
	}, 1, newDecodeBudget())
	if !hasPlayedFunc(events, func(event Event) bool { return event.Type == EventEnd && event.Time == 20 }) {
		t.Fatalf("track does not end at 20ms: %+v", events)
	}
}

func TestHandsetTracksKeepIndependentChannelAllocations(t *testing.T) {
	tones := newToneMap()
	tones.initTrack(HandyPhoneStandard, []ChannelStatus{{Kind: ChannelMelody}}, 0)
	events, _ := sequenceEvents([]SequenceEvent{{Kind: SeqProgramChange, Channel: 0, Program: 40}}, 1, 1, 0, true, nil, tones, newDecodeBudget())
	if !hasPlayed(events, Event{Type: EventProgramChange, Channel: 0, Program: 40}) {
		t.Fatalf("first track did not take MIDI channel 0: %+v", events)
	}

	// The second track continues the allocation rather than restarting it,
	// which is why the handset tone map is shared across tracks.
	tones.initTrack(HandyPhoneStandard, []ChannelStatus{{Kind: ChannelMelody}}, 4)
	events, _ = sequenceEvents([]SequenceEvent{{Kind: SeqProgramChange, Channel: 0, Program: 41}}, 1, 1, 4, true, nil, tones, newDecodeBudget())
	if !hasPlayed(events, Event{Type: EventProgramChange, Channel: 1, Program: 41}) {
		t.Fatalf("second track did not take MIDI channel 1: %+v", events)
	}
}

func TestHandsetRhythmUsesProgramAsDrumKey(t *testing.T) {
	tones := newToneMap()
	tones.initTrack(HandyPhoneStandard, []ChannelStatus{{Kind: ChannelRhythm}}, 0)
	events, _ := sequenceEvents([]SequenceEvent{
		{Kind: SeqProgramChange, Channel: 0, Program: 35},
		{Kind: SeqExpression, Channel: 0, Value: 92},
		{Kind: SeqNote, Channel: 0, Note: 1, GateTime: 10},
	}, 1, 1, 0, true, nil, tones, newDecodeBudget())

	if !hasPlayed(events, Event{Type: EventProgramChange, Channel: midiDrumChannel}) {
		t.Fatalf("rhythm program change did not go to the drum channel: %+v", events)
	}
	// The program is the drum key and the expression is the hit's loudness.
	if !hasPlayed(events, Event{Type: EventNoteOn, Channel: midiDrumChannel, Note: 35, Velocity: 92}) {
		t.Fatalf("rhythm note is not key 35 at velocity 92: %+v", events)
	}
	if hasPlayedFunc(events, func(event Event) bool {
		return event.Type == EventControlChange && event.Channel == midiDrumChannel && event.Control == 11
	}) {
		t.Fatalf("rhythm emitted an expression controller: %+v", events)
	}
}

func TestHandsetMelodyExpressionFoldsIntoVolume(t *testing.T) {
	tones := newToneMap()
	tones.initTrack(HandyPhoneStandard, []ChannelStatus{{Kind: ChannelMelody}}, 0)
	events, _ := sequenceEvents([]SequenceEvent{
		{Kind: SeqVolume, Channel: 0, Value: 100},
		{Kind: SeqExpression, Channel: 0, Value: 92},
	}, 1, 1, 0, true, nil, tones, newDecodeBudget())

	// 100 * 92 / 127 = 72, sent as channel volume rather than expression.
	if !hasPlayed(events, Event{Type: EventControlChange, Channel: 0, Control: 7, Value: 72}) {
		t.Fatalf("expression was not folded into volume: %+v", events)
	}
	if hasPlayedFunc(events, func(event Event) bool {
		return event.Type == EventControlChange && event.Control == 11
	}) {
		t.Fatalf("handset melody emitted an expression controller: %+v", events)
	}
}

func TestHandsetMelodyAddsBaseOctaveButRhythmDoesNot(t *testing.T) {
	melody := newToneMap()
	melody.initTrack(HandyPhoneStandard, []ChannelStatus{{Kind: ChannelMelody}}, 0)
	if note := melody.mapNote(0, 24); note != 60 {
		t.Fatalf("melody mapNote(24) = %d, want 60", note)
	}

	rhythm := newToneMap()
	rhythm.initTrack(HandyPhoneStandard, []ChannelStatus{{Kind: ChannelRhythm}}, 0)
	if channel, program := rhythm.setProgram(0, 38); channel != midiDrumChannel || program != 0 {
		t.Fatalf("rhythm setProgram = (%d, %d), want (%d, 0)", channel, program, midiDrumChannel)
	}
	if note := rhythm.mapNote(0, 24); note != 38 {
		t.Fatalf("rhythm mapNote(24) = %d, want the drum key 38", note)
	}
}

func TestHuffmanRoundTripsATreeItBuilt(t *testing.T) {
	// Encode "AAB" with the tree (A, B): 1 0 'A' 0 'B', then codes 0, 0, 1.
	var bits bitWriter
	bits.write(1, 1)
	bits.write(0, 1)
	bits.write('A', 8)
	bits.write(0, 1)
	bits.write('B', 8)
	bits.write(0, 1)
	bits.write(0, 1)
	bits.write(1, 1)

	decoded, err := huffmanDecode(3, bits.bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "AAB" {
		t.Fatalf("huffmanDecode = %q, want %q", decoded, "AAB")
	}
}

func TestADPCMDecodesTwoSamplesPerByte(t *testing.T) {
	samples := DecodeADPCM([]byte{0x12, 0x34})
	if len(samples) != 4 {
		t.Fatalf("DecodeADPCM produced %d samples, want 4", len(samples))
	}
	if samples[0] == 0 {
		t.Fatal("first sample is silent; the decoder never left its initial state")
	}
}

// The low nibble of a byte is the earlier sample. A byte whose low nibble is a
// positive step and whose high nibble is the negative one says which way round
// the two are read, and nothing later in the stream can correct a decoder that
// reads them the other way: every sample is a step from the one before it, so
// the error is kept for the rest of the sound.
func TestADPCMReadsTheLowNibbleFirst(t *testing.T) {
	samples := DecodeADPCM([]byte{0x80})
	if len(samples) != 2 {
		t.Fatalf("DecodeADPCM produced %d samples, want 2", len(samples))
	}
	if samples[0] <= 0 {
		t.Errorf("first sample = %d, want a positive step: the low nibble is read first", samples[0])
	}
	if samples[1] >= samples[0] {
		t.Errorf("second sample = %d, want it below the first: the high nibble is the negative step", samples[1])
	}
}

// **The step table is carried in 256ths and the four uneven entries have to be
// exact.** Rounded to 64ths they become 57, 77, 102, 128 and 153, which is the
// same five ratios at a quarter of the resolution and four of them rounded the
// wrong way. The step size decides the size of the next difference, so an error
// there is an error in every sample after it — small enough to look like
// nothing over ten samples and enough to walk the predictor off the signal over
// nine thousand. This locks the numbers; the reason is in adpcm.go.
func TestADPCMStepTableIsCarriedIn256ths(t *testing.T) {
	want := [8]uint32{230, 230, 230, 230, 307, 409, 512, 614}
	if adpcmStepTable != want {
		t.Errorf("adpcmStepTable = %v, want %v", adpcmStepTable, want)
	}
	// The same check from the other side: one maximal delta from the floor
	// step has to grow it to 304, which is 614/256 of 127 and not 612/256.
	state := adpcmState{stepSize: 127}
	state.step(7)
	if state.stepSize != 304 {
		t.Errorf("step size after one maximal delta = %d, want 304", state.stepSize)
	}
}

// The two uncompressed wave forms a score track can hang off a channel. One
// local title's four songs carry nothing else, and the gate that let only
// ADPCM through played none of them. Offset binary puts silence at 0x80;
// two's complement puts it at 0.
func TestStreamWaveDecodesTheUncompressedForms(t *testing.T) {
	for _, probe := range []struct {
		name     string
		wave     WaveData
		want     []int16
		playable bool
	}{
		{
			name: "offset binary, eight bit",
			wave: WaveData{Format: OffsetBinaryPCM, BaseBit: Bit8, Channels: Mono,
				Data: []byte{0x80, 0xff, 0x00, 0xc0}},
			want:     []int16{0, 32512, -32768, 16384},
			playable: true,
		},
		{
			name: "two's complement, eight bit",
			wave: WaveData{Format: TwosComplementPCM, BaseBit: Bit8, Channels: Mono,
				Data: []byte{0x00, 0x7f, 0x80, 0x40}},
			want:     []int16{0, 32512, -32768, 16384},
			playable: true,
		},
		{
			// Nothing local carries one, so it stays unplayed rather than
			// guessed at: a wave played wrong is worse than a wave not played.
			name:     "sixteen bit is not read",
			wave:     WaveData{Format: OffsetBinaryPCM, BaseBit: Bit16, Channels: Mono, Data: []byte{0, 0, 0, 0}},
			playable: false,
		},
		{
			name:     "stereo is not read",
			wave:     WaveData{Format: OffsetBinaryPCM, BaseBit: Bit8, Channels: Stereo, Data: []byte{0x80, 0x80}},
			playable: false,
		},
	} {
		samples, playable := decodeStreamWave(probe.wave)
		if playable != probe.playable {
			t.Errorf("%s: playable = %v, want %v", probe.name, playable, probe.playable)
			continue
		}
		if !playable {
			continue
		}
		if len(samples) != len(probe.want) {
			t.Errorf("%s: %d samples, want %d", probe.name, len(samples), len(probe.want))
			continue
		}
		for index, want := range probe.want {
			if samples[index] != want {
				t.Errorf("%s: sample %d = %d, want %d", probe.name, index, samples[index], want)
			}
		}
	}
}

// Helpers.

type bitWriter struct {
	data []byte
	bits uint8
}

func (writer *bitWriter) write(value int, count uint8) {
	for index := int(count) - 1; index >= 0; index-- {
		if writer.bits%8 == 0 {
			writer.data = append(writer.data, 0)
		}
		if value>>index&1 == 1 {
			writer.data[len(writer.data)-1] |= 1 << (7 - writer.bits%8)
		}
		writer.bits++
	}
}

func (writer *bitWriter) bytes() []byte { return writer.data }

func chunk(tag string, payload []byte) []byte {
	header := make([]byte, 8)
	copy(header, tag)
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

func buildSMAF(chunks ...[]byte) []byte {
	var body []byte
	for _, current := range chunks {
		body = append(body, current...)
	}
	file := make([]byte, 8)
	copy(file, "MMMD")
	binary.BigEndian.PutUint32(file[4:], uint32(len(body)+2))
	file = append(file, body...)
	return append(file, 0, 0)
}

func hasEvent(events []SequenceEvent, match func(SequenceEvent) bool) bool {
	for _, event := range events {
		if match(event) {
			return true
		}
	}
	return false
}

func hasPCMEvent(events []PCMEvent, match func(PCMEvent) bool) bool {
	for _, event := range events {
		if match(event) {
			return true
		}
	}
	return false
}

// hasPlayed matches on the fields want sets, ignoring time and the zero rest,
// which is what makes these assertions read like the events they describe.
func hasPlayed(events []Event, want Event) bool {
	return hasPlayedFunc(events, func(event Event) bool {
		return event.Type == want.Type && event.Channel == want.Channel &&
			event.Note == want.Note && event.Velocity == want.Velocity &&
			event.Program == want.Program && event.Control == want.Control && event.Value == want.Value
	})
}

func hasPlayedFunc(events []Event, match func(Event) bool) bool {
	for _, event := range events {
		if match(event) {
			return true
		}
	}
	return false
}
