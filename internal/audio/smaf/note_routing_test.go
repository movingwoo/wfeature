package smaf

import (
	"reflect"
	"sort"
	"testing"
)

// mobileWaveFixture is a mobile score track carrying the given waves, each a
// three-sample offset-binary sound whose first sample says which wave it is.
func mobileWaveFixture(sequence []byte, waves ...byte) []byte {
	children := [][]byte{chunk("Mtsq", sequence)}
	var attached []byte
	for _, number := range waves {
		attached = append(attached, chunk("Mwa"+string([]byte{number}), []byte{0x11, 0x1f, 0x40, 0x80 + number, 0x80, 0x80})...)
	}
	children = append(children, chunk("Mtsp", attached))
	return buildSMAF(limitsScore(MobileStandardNoCompress, children...))
}

// waveNumbers answers which fixture wave each wave event played, by the first
// sample mobileWaveFixture gave it.
func waveNumbers(events []Event) []int {
	var numbers []int
	for _, event := range events {
		if event.Type == EventWave {
			numbers = append(numbers, int(event.Wave[0]>>8))
		}
	}
	return numbers
}

// **Channel 9's notes name waves.** Its note n plays wave n+1 when the track
// carries one, so note zero there plays wave 1 rather than the wave one past
// the channel, which no local file carries. A channel 9 note without such a
// wave stays a rhythm note; other channels keep note zero's wave-past-channel
// rule.
func TestChannelNineNoteNPlaysWaveNPlusOne(t *testing.T) {
	sequence := []byte{
		0, 0x99, 0, 100, 1, // channel 9 note 0: wave 1
		1, 0x99, 1, 100, 1, // channel 9 note 1: wave 2
		1, 0x99, 42, 100, 1, // channel 9 note 42: no wave 43, a rhythm note
		1, 0x92, 0, 100, 1, // channel 2 note 0: wave 3
		1, 0x9f, 0, 100, 1, // channel 15 note 0: wave 16 is not there
		1, 0x92, 1, 100, 1, // channel 2 note 1 is a pitch, not wave 2
		1, 0xff, 0x2f, 0,
	}
	events, err := Decode(mobileWaveFixture(sequence, 1, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := waveNumbers(events), []int{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waves played = %v, want %v", got, want)
	}
	var notes []uint8
	for _, event := range events {
		if event.Type == EventNoteOn {
			notes = append(notes, event.Note)
		}
	}
	if len(notes) != 2 || notes[0] != 42 {
		t.Fatalf("note-ons = %v, want the channel 9 rhythm note 42 and channel 2's note 1", notes)
	}
}

// Note zero on channel 9 without a wave 1 still names no pitch.
func TestChannelNineNoteZeroWithoutWaveOneIsSilent(t *testing.T) {
	events, err := Decode(mobileWaveFixture([]byte{0, 0x99, 0, 100, 1, 1, 0xff, 0x2f, 0}, 2))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == EventWave || event.Type == EventNoteOn {
			t.Fatalf("channel 9 note 0 with no wave 1 played %+v", event)
		}
	}
}

type noteEdge struct {
	Time    uint32
	Type    EventType
	Channel uint8
	Note    uint8
}

func noteEdges(events []Event) []noteEdge {
	sort.SliceStable(events, func(left, right int) bool {
		if events[left].Time != events[right].Time {
			return events[left].Time < events[right].Time
		}
		return eventOrder(events[left]) < eventOrder(events[right])
	})
	var edges []noteEdge
	for _, event := range events {
		if event.Type == EventNoteOn || event.Type == EventNoteOff {
			edges = append(edges, noteEdge{event.Time, event.Type, event.Channel, event.Note})
		}
	}
	return edges
}

// **A handset channel sounds one note at a time.** A note-on keys off the
// note still sounding on its channel, melody and rhythm alike and whatever the
// key; the earlier note-off moves rather than a second being added, so it
// cannot cut a retriggered key. A note replaced at its own start is dropped.
func TestHandsetChannelSoundsOneNoteAtATime(t *testing.T) {
	tones := newToneMap()
	tones.initTrack(HandyPhoneStandard, []ChannelStatus{{Kind: ChannelMelody}, {Kind: ChannelRhythm}}, 0)
	events, _ := sequenceEvents([]SequenceEvent{
		{Kind: SeqProgramChange, Channel: 1, Program: 38},
		{Kind: SeqNote, Channel: 0, Note: 24, GateTime: 10},             // 0: melody 60 until 10
		{Kind: SeqNote, Channel: 1, Note: 24, GateTime: 10},             // 0: drum 38 until 10
		{Kind: SeqNote, Duration: 4, Channel: 0, Note: 26, GateTime: 2}, // 4: cuts 60, plays 62 until 6
		{Kind: SeqNote, Channel: 1, Note: 24, GateTime: 10},             // 4: retriggers 38 until 14
		{Kind: SeqNote, Duration: 4, Channel: 0, Note: 28, GateTime: 5}, // 8: 62 already ended
		{Kind: SeqNote, Channel: 0, Note: 29, GateTime: 5},              // 8: replaces 64 at its start
	}, 1, 1, 0, true, nil, tones, newDecodeBudget())

	want := []noteEdge{
		{0, EventNoteOn, 0, 60},
		{0, EventNoteOn, midiDrumChannel, 38},
		{4, EventNoteOff, 0, 60},
		{4, EventNoteOff, midiDrumChannel, 38},
		{4, EventNoteOn, 0, 62},
		{4, EventNoteOn, midiDrumChannel, 38},
		{6, EventNoteOff, 0, 62},
		{8, EventNoteOn, 0, 65},
		{13, EventNoteOff, 0, 65},
		{14, EventNoteOff, midiDrumChannel, 38},
	}
	if got := noteEdges(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("handset notes:\ngot  %+v\nwant %+v", got, want)
	}
}

// Mobile channels stay polyphonic: overlapping notes keep their own gates.
func TestMobileChannelKeepsOverlappingNotes(t *testing.T) {
	events, err := Decode(mobileWaveFixture([]byte{
		0, 0x90, 60, 100, 10,
		4, 0x90, 64, 100, 2,
		1, 0xff, 0x2f, 0,
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := []noteEdge{
		{0, EventNoteOn, 0, 60},
		{4, EventNoteOn, 0, 64},
		{6, EventNoteOff, 0, 64},
		{10, EventNoteOff, 0, 60},
	}
	if got := noteEdges(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("mobile notes:\ngot  %+v\nwant %+v", got, want)
	}
}
