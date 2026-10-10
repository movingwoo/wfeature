package smaf

import "testing"

func TestHuffmanRefusesOversizedDeclaredExpansion(t *testing.T) {
	// A one-leaf tree needs no encoded bits for any of its output bytes.
	// Keep this regression safe even without the allocation guard.
	decoded, err := huffmanDecode((8<<20)+1, []byte{0x7f, 0x80})
	if err == nil || decoded != nil {
		t.Fatalf("oversized expansion produced %d bytes, error %v", len(decoded), err)
	}
}

func TestSetupVariableLengthRejectsIntegerOverflow(t *testing.T) {
	for _, data := range [][]byte{
		{0x90, 0x80, 0x80, 0x80, 0x00},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f},
	} {
		if value, _, ok := readMIDIVariableLength(data, 0); ok {
			t.Fatalf("overflowing variable length accepted as %d", value)
		}
	}
}

func TestTranslatedEventTimesDoNotWrap(t *testing.T) {
	for _, probe := range []struct {
		name       string
		sequence   []SequenceEvent
		base       uint32
		atmosphere bool
	}{
		{"duration product", []SequenceEvent{{Duration: ^uint32(0), Kind: SeqNop}}, 2, false},
		{"track sum", []SequenceEvent{{Duration: ^uint32(0)}, {Duration: 1}}, 1, false},
		{"gate product", []SequenceEvent{{Kind: SeqNote, Note: 60, GateTime: ^uint32(0)}}, 2, false},
		{"note end", []SequenceEvent{{Kind: SeqNote, Note: 60, Duration: ^uint32(0), GateTime: 1}}, 1, false},
		{"atmosphere gate", []SequenceEvent{{Kind: SeqNote, Note: 60, GateTime: ^uint32(0) - 100}}, 1, true},
		{"atmosphere layer", []SequenceEvent{{Kind: SeqNote, Note: 60, Duration: ^uint32(0) - 200}}, 1, true},
	} {
		t.Run(probe.name, func(t *testing.T) {
			budget, tones := newDecodeBudget(), newToneMap()
			tones.initTrack(MobileStandardNoCompress, nil, 0)
			tones.atmosphere[0] = probe.atmosphere
			tones.atmosLayerSet[0][0] = probe.atmosphere
			tones.atmosLayers[0][0].gateExtensionMS = 240
			sequenceEvents(probe.sequence, probe.base, probe.base, 0, false, nil, tones, budget)
			if budget.err == nil {
				t.Fatal("out-of-range event time was accepted")
			}
		})
	}
	budget := newDecodeBudget()
	pcmTrackEvents(&PCMAudioTrack{Format: PCMAdpcm, Channels: Mono, TimebaseD: 2,
		Sequence: []PCMEvent{{Duration: ^uint32(0)}}}, 1, budget)
	if budget.err == nil {
		t.Fatal("out-of-range PCM time was accepted")
	}
}

func TestTranslatedEventTimeAcceptsUint32Boundary(t *testing.T) {
	for _, atmosphere := range []bool{false, true} {
		budget, tones := newDecodeBudget(), newToneMap()
		tones.initTrack(MobileStandardNoCompress, nil, 0)
		tones.atmosphere[0], tones.atmosLayerSet[0][0] = atmosphere, atmosphere
		tones.atmosLayers[0][0].gateExtensionMS = 240
		start := ^uint32(0) - 5
		if atmosphere {
			start -= 360 // The primary and layer's existing extra tails.
		}
		events, _ := sequenceEvents([]SequenceEvent{{Kind: SeqNote, Note: 60, Duration: start, GateTime: 5}},
			1, 1, 0, false, nil, tones, budget)
		if budget.err != nil || !hasPlayedFunc(events, func(event Event) bool {
			return event.Type == EventNoteOff && event.Time == ^uint32(0)
		}) {
			t.Fatalf("maximum note-off time refused (atmosphere=%t): %v", atmosphere, budget.err)
		}
	}
	budget := newDecodeBudget()
	events := pcmTrackEvents(&PCMAudioTrack{Format: PCMAdpcm, Channels: Mono, TimebaseD: 1,
		Sequence: []PCMEvent{{Duration: ^uint32(0)}}}, 1, budget)
	if budget.err != nil || len(events) != 1 || events[0].Time != ^uint32(0) {
		t.Fatalf("maximum PCM track time refused: %v", budget.err)
	}
}
