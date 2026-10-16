package smaf

import (
	"reflect"
	"testing"
)

// handsetTrackPlayingAllChannels is one handset score track with a note on
// each of its four channels, every channel declared melodic.
func handsetTrackPlayingAllChannels() []byte {
	var sequence []byte
	for channel := byte(0); channel < 4; channel++ {
		// Duration, then a note packed as channel, octave one, voice one,
		// then its gate.
		sequence = append(sequence, 0x00, channel<<6|1<<4|1, 0x01)
	}
	return limitsScore(HandyPhoneStandard, chunk("Mtsq", sequence))
}

// assertPlayableMIDI checks every event against what a MIDI channel message
// can carry: sixteen channels and seven-bit data bytes.
func assertPlayableMIDI(t *testing.T, events []Event) {
	t.Helper()
	for _, event := range events {
		switch event.Type {
		case EventNoteOn, EventNoteOff, EventProgramChange, EventControlChange, EventPitchBend:
		default:
			continue
		}
		if event.Channel >= 16 || event.Note >= 128 || event.Velocity >= 128 ||
			event.Program >= 128 || event.Control >= 128 || event.Value >= 128 || event.Bend >= 1<<14 {
			t.Fatalf("decoded an event MIDI cannot carry: %+v", event)
		}
	}
}

func withoutEnds(events []Event) []Event {
	var kept []Event
	for _, event := range events {
		if event.Type != EventEnd {
			kept = append(kept, event)
		}
	}
	return kept
}

// Real handset files carry at most four score tracks, sixteen channels. A
// fifth track's channels find no MIDI channel left; they are dropped instead
// of being numbered past fifteen, which used to panic the next allocation.
func TestHandsetChannelsPastSixteenAreDroppedNotNumberedPastFifteen(t *testing.T) {
	four := make([][]byte, 4)
	for index := range four {
		four[index] = handsetTrackPlayingAllChannels()
	}
	want, err := Decode(buildSMAF(four...))
	if err != nil {
		t.Fatal(err)
	}
	assertPlayableMIDI(t, want)

	for _, tracks := range []int{5, 8} {
		chunks := make([][]byte, tracks)
		for index := range chunks {
			chunks[index] = handsetTrackPlayingAllChannels()
		}
		var got []Event
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("%d handset tracks panicked: %v", tracks, recovered)
				}
			}()
			got, err = Decode(buildSMAF(chunks...))
		}()
		if err != nil {
			t.Fatalf("%d handset tracks: %v", tracks, err)
		}
		assertPlayableMIDI(t, got)
		if !reflect.DeepEqual(withoutEnds(got), withoutEnds(want)) {
			t.Fatalf("%d handset tracks changed what the first four play:\ngot  %+v\nwant %+v", tracks, withoutEnds(got), withoutEnds(want))
		}
	}
}

// A data byte with its top bit set is not MIDI. Like the PCM track's controls,
// a score control carrying one is ignored and the timeline after it is kept.
func TestScoreControlsWithEightBitDataAreIgnored(t *testing.T) {
	cases := map[string][]byte{
		"mobile controller number": buildSMAF(limitsScore(MobileStandardNoCompress, chunk("Mtsq", []byte{
			0x00, 0xb0, 0x87, 0x10,
			0x02, 0xb0, 0x07, 0x40,
		}))),
		"mobile controller value": buildSMAF(limitsScore(MobileStandardNoCompress, chunk("Mtsq", []byte{
			0x00, 0xb0, 0x07, 0x90,
			0x02, 0xb0, 0x07, 0x40,
		}))),
		"handset pan": buildSMAF(limitsScore(HandyPhoneStandard, chunk("Mtsq", []byte{
			0x00, 0x00, 0x3a, 0x90,
			0x02, 0x00, 0x37, 0x40,
		}))),
		"handset modulation": buildSMAF(limitsScore(HandyPhoneStandard, chunk("Mtsq", []byte{
			0x00, 0x00, 0x33, 0xc8,
			0x02, 0x00, 0x37, 0x40,
		}))),
		"handset volume in a mobile track": buildSMAF(limitsScore(MobileStandardNoCompress, chunk("SEQU", []byte{
			0x00, 0x00, 0x37, 0x90,
			0x02, 0x00, 0x37, 0x40,
		}))),
		"handset expression in a mobile track": buildSMAF(limitsScore(MobileStandardNoCompress, chunk("SEQU", []byte{
			0x00, 0x00, 0x36, 0x90,
			0x02, 0x00, 0x37, 0x40,
		}))),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			events, err := Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			assertPlayableMIDI(t, events)
			// The well-formed volume after the malformed control still lands
			// two one-millisecond units later.
			if !hasPlayedFunc(events, func(event Event) bool {
				return event.Type == EventControlChange && event.Control == 7 && event.Time == 2
			}) {
				t.Fatalf("the control after the malformed one moved or vanished: %+v", events)
			}
		})
	}
}
