package smaf

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

func TestHPSExclusiveSequenceUsesSizedTerminatedPayload(t *testing.T) {
	data := buildSMAF(limitsScore(HandyPhoneStandard, chunk("Mtsq", []byte{
		3, 0xff, 0xf0, 4, 0x7d, 1, 2, 0xf7,
		5, 0x11, 2,
		0, 0, 0, 0,
	})))
	file, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []SequenceEvent{
		{Kind: SeqExclusive, Duration: 3, Exclusive: []byte{0x7d, 1, 2, 0xf7}},
		{Kind: SeqNote, Duration: 5, Note: 13, GateTime: 2},
	}
	if got := file.Chunks[0].ScoreTrack.Sequences[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("sequence after a sized HPS exclusive = %+v, want %+v", got, want)
	}
	events, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[0].Time != 3 || events[0].Type != EventSysEx ||
		!bytes.Equal(events[0].SysEx, []byte{0xf0, 0x7d, 1, 2, 0xf7}) ||
		events[1].Type != EventNoteOn || events[1].Time != 8 ||
		events[2].Type != EventEnd || events[2].Time != 8 ||
		events[3].Type != EventNoteOff || events[3].Time != 10 {
		t.Fatalf("exclusive framing or following note timing changed: %+v", events)
	}
}

func TestHPSExclusiveOneByteSizeKeepsPayloadOpaque(t *testing.T) {
	for _, size := range []int{12, 128, 255} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			payload := bytes.Repeat([]byte{0x21}, size)
			// Embedded terminators, headers and an EOS-shaped byte sequence are
			// payload, including the F7 immediately before the final terminator.
			copy(payload, []byte{0x7d, 0xf7, 0xff, 0xf0, 0, 0, 0, 0})
			payload[size-2], payload[size-1] = 0xf7, 0xf7
			message := append([]byte{0xff, 0xf0, byte(size)}, payload...)
			sequence := append([]byte{3}, message...)
			sequence = append(sequence, 5, 0x11, 2, 0, 0, 0, 0)
			data := buildSMAF(limitsScore(HandyPhoneStandard,
				chunk("Mtsu", message), chunk("Mtsq", sequence)))
			original := bytes.Clone(data)
			file, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			want := []SequenceEvent{
				{Kind: SeqExclusive, Duration: 3, Exclusive: payload},
				{Kind: SeqNote, Duration: 5, Note: 13, GateTime: 2},
			}
			if got := file.Chunks[0].ScoreTrack.Sequences[0]; !reflect.DeepEqual(got, want) {
				t.Fatalf("opaque %d-byte payload changed following records: %+v", size, got)
			}
			events, err := Decode(data)
			if err != nil || len(events) != 5 {
				t.Fatalf("opaque payload decode = %+v, %v", events, err)
			}
			wantMessage := append([]byte{0xf0}, payload...)
			for i, at := range []uint32{0, 3} {
				if events[i].Type != EventSysEx || events[i].Time != at || !bytes.Equal(events[i].SysEx, wantMessage) {
					t.Fatalf("message %d lost declared bytes or time: %+v", i, events[i])
				}
			}
			if !reflect.DeepEqual(hpsExclusiveEventTimes(events, EventNoteOn), []uint32{8}) ||
				!reflect.DeepEqual(hpsExclusiveEventTimes(events, EventNoteOff), []uint32{10}) ||
				!reflect.DeepEqual(hpsExclusiveEventTimes(events, EventEnd), []uint32{8}) {
				t.Fatalf("payload bytes changed following onset/deadline: %+v", events)
			}
			if !bytes.Equal(data, original) {
				t.Fatal("exclusive decoding modified the input")
			}
		})
	}
}

func TestHPSExclusiveMalformedTailPreservesOtherChunks(t *testing.T) {
	valid := []byte{0xff, 0xf0, 3, 0x7d, 1, 0xf7}
	neighbor := []byte{0xff, 0xf0, 3, 0x7d, 9, 0xf7}
	tails := []struct {
		name string
		data []byte
	}{
		{"short header", []byte{0xff}},
		{"missing size", []byte{0xff, 0xf0}},
		{"zero size", append([]byte{0xff, 0xf0, 0}, neighbor...)},
		{"truncated payload", []byte{0xff, 0xf0, 5, 0x7d, 0xf7}},
		{"missing terminator", append([]byte{0xff, 0xf0, 3, 0x7d, 2, 0x7f}, neighbor...)},
		{"terminator outside size", append([]byte{0xff, 0xf0, 2, 0x7d, 2, 0xf7}, neighbor...)},
		{"interior terminator only", append([]byte{0xff, 0xf0, 4, 0x7d, 0xf7, 2, 0x7f}, neighbor...)},
	}
	for _, tail := range tails {
		t.Run(tail.name, func(t *testing.T) {
			setup := append(bytes.Clone(valid), tail.data...)
			sequence := append([]byte{2, 0x11, 1, 1}, tail.data...)
			data := buildSMAF(
				limitsScore(HandyPhoneStandard,
					chunk("Mtsu", setup),
					chunk("Mtsu", []byte{0xff, 0xf0, 3, 0x7d, 8, 0xf7}),
					chunk("Mtsq", sequence), chunk("Mtsq", []byte{4, 0x12, 1})),
				limitsScore(HandyPhoneStandard, chunk("Mtsq", []byte{6, 0x13, 1})))
			file, err := Parse(data)
			if err != nil || len(file.Chunks) != 2 {
				t.Fatalf("malformed tail lost later track: file=%+v, err=%v", file, err)
			}
			if got := file.Chunks[0].ScoreTrack.Sequences[0]; !reflect.DeepEqual(got, []SequenceEvent{
				{Kind: SeqNote, Duration: 2, Note: 13, GateTime: 1},
			}) {
				t.Fatalf("malformed exclusive changed valid sequence prefix: %+v", got)
			}
			events, err := Decode(data)
			if err != nil || len(events) != 11 {
				t.Fatalf("malformed tail lost valid output or invented events: %+v, %v", events, err)
			}
			for i, marker := range []byte{1, 8} {
				if events[i].Type != EventSysEx || events[i].Time != 0 ||
					!bytes.Equal(events[i].SysEx, []byte{0xf0, 0x7d, marker, 0xf7}) {
					t.Fatalf("malformed setup lost preceding message or next chunk: %+v", events)
				}
			}
			if !reflect.DeepEqual(hpsExclusiveEventTimes(events, EventNoteOn), []uint32{2, 4, 6}) ||
				!reflect.DeepEqual(hpsExclusiveEventTimes(events, EventNoteOff), []uint32{3, 5, 7}) ||
				!reflect.DeepEqual(hpsExclusiveEventTimes(events, EventEnd), []uint32{2, 4, 6}) {
				t.Fatalf("malformed tail changed prefix or other sequence clocks: %+v", events)
			}
		})
	}
}

func TestHPSExclusiveFixPreservesOtherDialects(t *testing.T) {
	t.Run("softbank size only", func(t *testing.T) {
		for _, size := range []int{0, 128, 255} {
			t.Run(fmt.Sprint(size), func(t *testing.T) {
				payload := bytes.Repeat([]byte{0x21}, size)
				if size > 0 {
					copy(payload, []byte{0x7d, 0xf7, 0xff, 0xf0})
				}
				sequence := append([]byte{1, 0xff, 0xf0, byte(size)}, payload...)
				sequence = append(sequence, 2, 0x11, 1)
				data := buildSMAF(chunk("SEQU", sequence))
				file, err := Parse(data)
				if err != nil {
					t.Fatal(err)
				}
				got := file.Chunks[0].SoftbankSequence
				if len(got) != 2 || got[0].Kind != SeqExclusive || got[0].Duration != 1 ||
					!bytes.Equal(got[0].Exclusive, payload) || got[1].Kind != SeqNote || got[1].Duration != 2 {
					t.Fatalf("size-only SEQU changed: %+v", got)
				}
				events, err := Decode(data)
				wantMessage := append(append([]byte{0xf0}, payload...), 0xf7)
				if err != nil || len(events) != 4 || events[0].Type != EventSysEx || events[0].Time != 20 ||
					!bytes.Equal(events[0].SysEx, wantMessage) ||
					!reflect.DeepEqual(hpsExclusiveEventTimes(events, EventNoteOn), []uint32{60}) ||
					!reflect.DeepEqual(hpsExclusiveEventTimes(events, EventNoteOff), []uint32{80}) {
					t.Fatalf("size-only SEQU output changed: %+v, %v", events, err)
				}
			})
		}
	})
	t.Run("mobile MIDI variable size", func(t *testing.T) {
		payload := bytes.Repeat([]byte{0x21}, 128)
		copy(payload, []byte{0x7d, 0xf7, 0xff, 0xf0})
		message := append([]byte{0xf0, 0x81, 0}, payload...)
		sequence := append([]byte{3}, message...)
		sequence = append(sequence, 5, 0x90, 60, 100, 2)
		data := buildSMAF(limitsScore(MobileStandardNoCompress,
			chunk("Mtsu", message), chunk("Mtsq", sequence)))
		file, err := Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		got := file.Chunks[0].ScoreTrack.Sequences[0]
		if len(got) != 2 || got[0].Kind != SeqExclusive || got[0].Duration != 3 ||
			!bytes.Equal(got[0].Exclusive, payload) || got[1].Kind != SeqNote || got[1].Duration != 5 {
			t.Fatalf("Mobile VLQ sequence changed: %+v", got)
		}
		events, err := Decode(data)
		if err != nil || len(events) != 5 {
			t.Fatalf("Mobile VLQ decode = %+v, %v", events, err)
		}
		wantMessage := append(append([]byte{0xf0}, payload...), 0xf7)
		for i, at := range []uint32{0, 3} {
			if events[i].Type != EventSysEx || events[i].Time != at || !bytes.Equal(events[i].SysEx, wantMessage) {
				t.Fatalf("Mobile setup/sequence framing changed: %+v", events)
			}
		}
		if !reflect.DeepEqual(hpsExclusiveEventTimes(events, EventNoteOn), []uint32{8}) ||
			!reflect.DeepEqual(hpsExclusiveEventTimes(events, EventNoteOff), []uint32{10}) {
			t.Fatalf("Mobile note timing changed: %+v", events)
		}
	})
}

func TestHPSExclusiveOutputLimitsIncludeSetupMessages(t *testing.T) {
	data := buildSMAF(
		limitsScore(HandyPhoneStandard,
			chunk("Mtsu", []byte{0xff, 0xf0, 3, 0x7d, 1, 0xf7, 0xff, 0xf0, 4, 0x7d, 2, 3, 0xf7}),
			chunk("Mtsu", []byte{0xff, 0xf0, 2, 0x7d, 0xf7}),
			chunk("Mtsq", []byte{0, 0xff, 0xf0, 3, 0x7d, 4, 0xf7, 0, 0x11, 1})),
		limitsScore(HandyPhoneStandard,
			chunk("Mtsu", []byte{0xff, 0xf0, 3, 0x7d, 5, 0xf7}),
			chunk("Mtsq", []byte{0, 0x12, 1})),
		chunk("SEQU", []byte{0, 0xff, 0xf0, 2, 0x7d, 6}))
	events, err := Decode(data)
	if err != nil || len(events) != 13 {
		t.Fatalf("setup-aware aggregate event count = %d, err=%v", len(events), err)
	}
	var messages [][]byte
	for _, event := range events {
		if event.Type == EventSysEx {
			messages = append(messages, event.SysEx)
		}
	}
	want := [][]byte{
		{0xf0, 0x7d, 1, 0xf7}, {0xf0, 0x7d, 2, 3, 0xf7}, {0xf0, 0x7d, 0xf7},
		{0xf0, 0x7d, 4, 0xf7}, {0xf0, 0x7d, 5, 0xf7}, {0xf0, 0x7d, 6, 0xf7},
	}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("aggregate setup/sequence messages = %x, want %x", messages, want)
	}
	limitsPlayBoundary(t, data, 24, func(limits *decodeLimits, value int) { limits.sysExBytes = value })
	limitsPlayBoundary(t, data, 13, func(limits *decodeLimits, value int) { limits.events = value })
}

func hpsExclusiveEventTimes(events []Event, kind EventType) []uint32 {
	var times []uint32
	for _, event := range events {
		if event.Type == kind {
			times = append(times, event.Time)
		}
	}
	return times
}

func TestHPSExclusiveSetupConcatenatesWithoutDuration(t *testing.T) {
	setup := []byte{0xff, 0xf0, 3, 0x7d, 1, 0xf7, 0xff, 0xf0, 3, 0x7d, 2, 0xf7}
	data := buildSMAF(limitsScore(HandyPhoneStandard,
		chunk("Mtsu", setup),
		chunk("Mtsq", []byte{0, 0xff, 0xf0, 3, 0x7d, 3, 0xf7, 0, 0x11, 2})))
	file, err := Parse(data)
	if err != nil || !bytes.Equal(file.Chunks[0].ScoreTrack.SetupData[0], setup) {
		t.Fatalf("setup parse = %+v, %v", file, err)
	}
	events, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 {
		t.Fatalf("two setup messages, sequence message and following note = %+v", events)
	}
	for i, marker := range []byte{1, 2, 3} {
		if events[i].Type != EventSysEx || events[i].Time != 0 ||
			!bytes.Equal(events[i].SysEx, []byte{0xf0, 0x7d, marker, 0xf7}) {
			t.Fatalf("message %d lost setup/sequence order: %+v", i, events)
		}
	}
	if events[3].Type != EventNoteOn || events[3].Time != 0 ||
		events[4].Type != EventEnd || events[4].Time != 0 ||
		events[5].Type != EventNoteOff || events[5].Time != 2 {
		t.Fatalf("following note timing = %+v", events)
	}
}
