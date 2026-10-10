package smaf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

func limitsScore(format FormatType, children ...[]byte) []byte {
	header := []byte{byte(format), 0, 0, 0}
	if format == HandyPhoneStandard {
		header = append(header, 0, 0)
	} else {
		header = append(header, make([]byte, 16)...)
	}
	for _, child := range children {
		header = append(header, child...)
	}
	return chunk("MTR\x00", header)
}

func limitsPCM(children ...[]byte) []byte {
	// Mono ADPCM, 8 kHz, one-millisecond duration and gate units.
	data := []byte{0, 0, 0x11, 0, 0, 0}
	for _, child := range children {
		data = append(data, child...)
	}
	return chunk("ATR\x00", data)
}

func limitsCompressedZeros(length int) []byte {
	var tree bitWriter
	tree.write(0, 1)
	tree.write(0, 8)
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(length))
	return append(data, tree.bytes()...)
}

func limitsParseBoundary(t *testing.T, data []byte, limit int, set func(*decodeLimits, int)) {
	t.Helper()
	original := bytes.Clone(data)
	for _, allowed := range []int{limit, limit - 1} {
		budget := newDecodeBudget()
		set(&budget.limits, allowed)
		file, err := parseWithBudget(data, budget)
		if allowed == limit {
			if err != nil || budget.err != nil || file == nil {
				t.Fatalf("exact limit %d rejected: file=%v, err=%v, budget=%v", allowed, file, err, budget.err)
			}
		} else if file != nil || !errors.Is(err, ErrResourceLimit) || !errors.Is(budget.err, ErrResourceLimit) {
			t.Fatalf("limit %d retained a partial file: file=%v, err=%v, budget=%v", allowed, file, err, budget.err)
		}
		budget = newDecodeBudget()
		set(&budget.limits, allowed)
		events := playWithBudget(data, budget)
		if allowed == limit {
			if len(events) == 0 || budget.err != nil {
				t.Fatalf("exact limit %d lost playback: events=%d, err=%v", allowed, len(events), budget.err)
			}
		} else if events != nil || !errors.Is(budget.err, ErrResourceLimit) {
			t.Fatalf("limit %d retained %d output events: %v", allowed, len(events), budget.err)
		}
	}
	if !bytes.Equal(data, original) {
		t.Fatal("bounded decoding modified its input")
	}
}

func limitsPlayBoundary(t *testing.T, data []byte, limit int, set func(*decodeLimits, int)) []Event {
	t.Helper()
	budget := newDecodeBudget()
	set(&budget.limits, limit)
	want := playWithBudget(data, budget)
	if len(want) == 0 || budget.err != nil {
		t.Fatalf("exact output limit %d rejected: events=%d, err=%v", limit, len(want), budget.err)
	}
	budget = newDecodeBudget()
	set(&budget.limits, limit-1)
	if got := playWithBudget(data, budget); got != nil || !errors.Is(budget.err, ErrResourceLimit) {
		t.Fatalf("output limit %d retained %d events: %v", limit-1, len(got), budget.err)
	}
	if got := Play(data); !reflect.DeepEqual(got, want) {
		t.Fatal("a rejected decode affected a later decode with its own budget")
	}
	return want
}

func TestDecodeInputByteBoundary(t *testing.T) {
	data := buildSMAF(chunk("CNTI", nil), limitsScore(MobileStandardNoCompress,
		chunk("Mtsq", []byte{0, 0x90, 60, 100, 1})))
	limitsParseBoundary(t, data, len(data), func(limits *decodeLimits, value int) { limits.inputBytes = value })
}

func TestDecodePublicLimitRejectsWholeFileAfterValidTrack(t *testing.T) {
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, (8<<20)+1)
	// No Huffman tree is needed: reject the oversized declaration before
	// decoding, and do not mistake it for a tolerated malformed trailing track.
	data := buildSMAF(
		limitsScore(MobileStandardNoCompress, chunk("Mtsq", []byte{0, 0x90, 60, 100, 1})),
		limitsScore(MobileStandardCompress, chunk("Mtsq", length)),
	)
	if file, err := Parse(data); file != nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("public Parse retained a prefix after an excessive declaration: file=%v, err=%v", file, err)
	}
	if events := Play(data); events != nil {
		t.Fatalf("public Play retained %d events after an excessive declaration", len(events))
	}
}

func TestDecodeChunkBudgetIncludesNestedAndUnknownChunks(t *testing.T) {
	// Eleven chunks: CNTI; MTR and its four descendants; ATR and its two
	// descendants; top-level SEQU; an unknown top-level chunk.
	data := buildSMAF(
		chunk("CNTI", nil),
		limitsScore(MobileStandardNoCompress,
			chunk("Mtsq", []byte{0, 0x90, 60, 100, 1}),
			chunk("Mtsq", []byte{0, 0xc0, 1}),
			chunk("Mtsp", chunk("Mwa\x01", []byte{0x20, 0x1f, 0x40, 0x12}))),
		limitsPCM(chunk("Atsq", []byte{0, 1, 1}), chunk("Awa\x01", []byte{0x12})),
		chunk("SEQU", []byte{0, 0x11, 1}),
		chunk("????", nil),
	)
	limitsParseBoundary(t, data, 11, func(limits *decodeLimits, value int) { limits.chunks = value })
}

func TestDecodeSequenceBudgetsAggregateAllDialectsAndRepeatedChunks(t *testing.T) {
	// Twenty-six sequence bytes and nine records. The two Atsq chunks both
	// consume work even though the second replaces the first retained sequence.
	data := buildSMAF(
		chunk("CNTI", nil),
		limitsScore(MobileStandardNoCompress,
			chunk("Mtsq", []byte{0, 0x90, 60, 100, 1}),
			chunk("Mtsq", []byte{0, 0xc0, 1})),
		limitsPCM(chunk("Atsq", []byte{0, 1, 1}), chunk("Atsq", []byte{1, 1, 1}), chunk("Awa\x01", []byte{0x12})),
		limitsScore(HandyPhoneStandard, chunk("Mtsq", []byte{0, 0x11, 1})),
		limitsScore(MobileStandardCompress, chunk("Mtsq", limitsCompressedZeros(6))),
		chunk("SEQU", []byte{0, 0x11, 1}),
	)
	t.Run("decoded bytes", func(t *testing.T) {
		limitsParseBoundary(t, data, 26, func(limits *decodeLimits, value int) { limits.sequenceBytes = value })
	})
	t.Run("records", func(t *testing.T) {
		limitsParseBoundary(t, data, 9, func(limits *decodeLimits, value int) { limits.sequenceEvents = value })
	})
}

func TestDecodeSequenceBudgetCountsIgnoredWork(t *testing.T) {
	// Reserved mobile statuses and ignored handset controls still require work.
	data := buildSMAF(
		limitsScore(MobileStandardNoCompress, chunk("Mtsq", []byte{0, 0xa0, 1, 2, 0, 0x90, 60, 100, 1})),
		chunk("SEQU", []byte{0, 0, 0x3f, 0, 0x11, 1}),
	)
	limitsParseBoundary(t, data, 4, func(limits *decodeLimits, value int) { limits.sequenceEvents = value })
}

func TestDecodeOutputEventBudgetCountsExpansionAndEveryTrack(t *testing.T) {
	t.Run("mixed tracks", func(t *testing.T) {
		data := buildSMAF(
			limitsScore(MobileStandardNoCompress, chunk("Mtsq", []byte{0, 0x90, 60, 100, 1})),
			limitsPCM(chunk("Atsq", []byte{0, 1, 1}), chunk("Awa\x01", []byte{0x12})),
			chunk("SEQU", []byte{0, 0x11, 1}),
		)
		events := limitsPlayBoundary(t, data, 8, func(limits *decodeLimits, value int) { limits.events = value })
		if len(events) != 8 {
			t.Fatalf("mixed tracks emitted %d events, want two note pairs, one wave and three ends", len(events))
		}
	})
	t.Run("layered notes", func(t *testing.T) {
		data := buildSMAF(limitsScore(MobileStandardNoCompress, chunk("Mtsq", []byte{
			0, 0xb0, 0, 0x7c, 0, 0xb0, 32, 1,
			0, 0xc0, 0x62, 0, 0x90, 60, 100, 1,
		})))
		// Four source records produce two bank changes, twenty program/setup
		// operations, three note pairs and an end event.
		events := limitsPlayBoundary(t, data, 29, func(limits *decodeLimits, value int) {
			limits.sequenceEvents, limits.events = 4, value
		})
		attacks := 0
		for _, event := range events {
			if event.Type == EventNoteOn {
				attacks++
			}
		}
		if len(events) != 29 || attacks != 3 {
			t.Fatalf("layered fixture emitted %d operations and %d attacks", len(events), attacks)
		}
	})
}

func TestDecodePCMByteBudgetCountsEveryRepeatedTrigger(t *testing.T) {
	data := buildSMAF(
		limitsScore(MobileStandardNoCompress,
			chunk("Mtsp", chunk("Mwa\x01", []byte{0x20, 0x1f, 0x40, 0x12, 0x34})),
			chunk("Mtsq", []byte{0, 0x90, 0, 100, 1, 1, 0x90, 0, 100, 1})),
		limitsPCM(chunk("Atsq", []byte{0, 1, 1, 1, 1, 1}), chunk("Awa\x01", []byte{0x12, 0x34, 0x56})),
	)
	// Each ADPCM byte becomes two int16 samples: 2*2*4 + 2*3*4 bytes.
	events := limitsPlayBoundary(t, data, 40, func(limits *decodeLimits, value int) { limits.pcmBytes = value })
	var lengths []int
	for _, event := range events {
		if event.Type == EventWave {
			lengths = append(lengths, len(event.Wave))
		}
	}
	if !reflect.DeepEqual(lengths, []int{4, 6, 4, 6}) {
		t.Fatalf("repeated wave triggers decoded sample lengths %v", lengths)
	}
}

func TestDecodePCMByteBudgetIncludesUncompressedSamples(t *testing.T) {
	var tracks [][]byte
	for _, format := range []byte{0x01, 0x11} {
		tracks = append(tracks, limitsScore(MobileStandardNoCompress,
			chunk("Mtsp", chunk("Mwa\x01", []byte{format, 0x1f, 0x40, 0, 128, 255})),
			chunk("Mtsq", []byte{0, 0x90, 0, 100, 1})))
	}
	events := limitsPlayBoundary(t, buildSMAF(tracks...), 12, func(limits *decodeLimits, value int) { limits.pcmBytes = value })
	var samples [][]int16
	for _, event := range events {
		if event.Type == EventWave {
			samples = append(samples, event.Wave)
		}
	}
	if !reflect.DeepEqual(samples, [][]int16{{0, -32768, -256}, {-32768, 0, 32512}}) {
		t.Fatalf("bounded PCM conversion changed samples: %v", samples)
	}
}

func TestDecodeSysExBudgetAggregatesSetupAndSequenceFraming(t *testing.T) {
	data := buildSMAF(
		limitsScore(MobileStandardNoCompress,
			chunk("Mtsu", []byte{0xf0, 2, 0x41, 0x42}),
			chunk("Mtsu", []byte{0xf0, 3, 0xf0, 0x43, 0xf7}),
			chunk("Mtsq", []byte{0, 0xf0, 2, 0x44, 0x45, 0, 0xf0, 0})),
		chunk("SEQU", []byte{0, 0xff, 0xf0, 3, 0xf0, 0x46, 0xf7}),
	)
	events := limitsPlayBoundary(t, data, 16, func(limits *decodeLimits, value int) { limits.sysExBytes = value })
	var messages [][]byte
	for _, event := range events {
		if event.Type == EventSysEx {
			messages = append(messages, event.SysEx)
		}
	}
	want := [][]byte{{0xf0, 0x41, 0x42, 0xf7}, {0xf0, 0x43, 0xf7}, {0xf0, 0x44, 0x45, 0xf7}, {0xf0, 0xf7}, {0xf0, 0x46, 0xf7}}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("bounded SysEx decoding changed messages: %x", messages)
	}
}

func TestDecodeLimitsKeepTolerantMalformedTails(t *testing.T) {
	note := []byte{0, 0x90, 60, 100, 1}
	track := limitsScore(MobileStandardNoCompress, chunk("Mtsq", note))
	want := Play(buildSMAF(track))
	brokenChunk := []byte{'B', 'A', 'D', '!', 0, 0, 0, 100}
	for name, data := range map[string][]byte{
		"top-level chunk":            buildSMAF(track, brokenChunk),
		"nested chunk":               buildSMAF(limitsScore(MobileStandardNoCompress, chunk("Mtsq", note), brokenChunk)),
		"short sequence":             buildSMAF(limitsScore(MobileStandardNoCompress, chunk("Mtsq", append(bytes.Clone(note), 0, 0x90, 62)))),
		"invalid later track":        buildSMAF(track, chunk("MTR\x01", nil)),
		"truncated compressed track": buildSMAF(track, limitsScore(MobileStandardCompress, chunk("Mtsq", []byte{0, 0, 0, 6}))),
	} {
		t.Run(name, func(t *testing.T) {
			budget := newDecodeBudget()
			if got := playWithBudget(data, budget); budget.err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("malformed tail lost the valid prefix: got=%+v, err=%v", got, budget.err)
			}
			if file, err := Parse(data); err != nil || file == nil {
				t.Fatalf("malformed tail no longer parses tolerantly: file=%v, err=%v", file, err)
			}
		})
	}
}

func TestMobileVariableNumberRejectsUint32Overflow(t *testing.T) {
	maximum := &reader{data: []byte{0x8f, 0xff, 0xff, 0xff, 0x7f}}
	if value, ok := maximum.variableNumber(); !ok || value != ^uint32(0) {
		t.Fatalf("maximum uint32 VLQ = %d, %t", value, ok)
	}
	overflow := []byte{0x90, 0x80, 0x80, 0x80, 0}
	if value, ok := (&reader{data: overflow}).variableNumber(); ok {
		t.Fatalf("oversized VLQ wrapped to %d", value)
	}
	note := []byte{0, 0x90, 60, 100, 1}
	want := Play(buildSMAF(limitsScore(MobileStandardNoCompress, chunk("Mtsq", note))))
	for name, tail := range map[string][]byte{
		"duration":         append(bytes.Clone(overflow), 0xc0, 1),
		"gate":             append([]byte{0, 0x90, 62, 100}, overflow...),
		"exclusive length": append([]byte{0, 0xf0}, overflow...),
	} {
		t.Run(name, func(t *testing.T) {
			data := buildSMAF(limitsScore(MobileStandardNoCompress, chunk("Mtsq", append(bytes.Clone(note), tail...))))
			if got := Play(data); !reflect.DeepEqual(got, want) {
				t.Fatalf("overflowing %s became a playable event: %+v", name, got)
			}
		})
	}
}

func FuzzBoundedSMAF(f *testing.F) {
	sequence := []byte{0, 0x90, 60, 100, 1}
	mobile := limitsScore(MobileStandardNoCompress, chunk("Mtsq", sequence))
	for _, seed := range [][]byte{
		buildSMAF(mobile),
		buildSMAF(limitsScore(HandyPhoneStandard, chunk("Mtsq", []byte{0, 0x11, 1}))),
		buildSMAF(limitsPCM(chunk("Atsq", []byte{0, 1, 1}), chunk("Awa\x01", []byte{0x12, 0x34}))),
		buildSMAF(limitsScore(MobileStandardCompress, chunk("Mtsq", limitsCompressedZeros(6)))),
		buildSMAF(chunk("SEQU", []byte{0, 0xff, 0xf0, 2, 0x41, 0x42, 0, 0x11, 1})),
		buildSMAF(limitsScore(MobileStandardNoCompress,
			chunk("Mtsu", []byte{0xf0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}),
			chunk("Mtsq", sequence))),
		buildSMAF(mobile, limitsScore(MobileStandardCompress, chunk("Mtsq", limitsCompressedZeros(513)))),
		buildSMAF(limitsScore(MobileStandardNoCompress, chunk("Mtsq", bytes.Repeat(sequence, 65)))),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		newBudget := func() *decodeBudget {
			budget := newDecodeBudget()
			budget.limits = decodeLimits{
				inputBytes: 4096, sequenceBytes: 512, sequenceEvents: 64, chunks: 32,
				events: 128, pcmBytes: 2048, sysExBytes: 512,
			}
			return budget
		}
		original := bytes.Clone(data)
		budget := newBudget()
		file, err := parseWithBudget(data, budget)
		if errors.Is(budget.err, ErrResourceLimit) || errors.Is(err, ErrResourceLimit) {
			if file != nil || !errors.Is(err, ErrResourceLimit) {
				t.Fatalf("hard parse limit retained a file: err=%v, budget=%v", err, budget.err)
			}
		}
		if !bytes.Equal(data, original) {
			t.Fatal("Parse modified the input")
		}
		budget = newBudget()
		events := playWithBudget(data, budget)
		if errors.Is(budget.err, ErrResourceLimit) && events != nil {
			t.Fatalf("hard playback limit retained %d events", len(events))
		}
		pcmBytes, sysExBytes := 0, 0
		for _, event := range events {
			pcmBytes += 2 * len(event.Wave)
			sysExBytes += len(event.SysEx)
		}
		if len(events) > budget.limits.events || pcmBytes > budget.limits.pcmBytes || sysExBytes > budget.limits.sysExBytes {
			t.Fatalf("output exceeds its budget: events=%d, PCM=%d, SysEx=%d", len(events), pcmBytes, sysExBytes)
		}
		if !bytes.Equal(data, original) {
			t.Fatal("Play modified the input")
		}
	})
}
