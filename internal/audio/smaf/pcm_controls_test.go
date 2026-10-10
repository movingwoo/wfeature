package smaf

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// Newly authored ATR bytes use mono ADPCM at 8 kHz unless a test changes the
// wave-type word. A duplicate track tag must still get independent channels.
func pcmControlTrack(id byte, sequence, wave []byte) []byte {
	children := [][]byte{chunk("Atsq", sequence)}
	if wave != nil {
		children = append(children, chunk("Awa\x01", wave))
	}
	data := limitsPCM(children...)
	data[3] = id
	return data
}

func decodePCMControls(t *testing.T, chunks ...[]byte) []Event {
	t.Helper()
	data := buildSMAF(chunks...)
	original := bytes.Clone(data)
	events, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("PCM control decoding modified input bytes")
	}
	return events
}

func TestDecodePCMControlsSeparateTrackChannelsAndScoreWaves(t *testing.T) {
	sequence := func(volume byte) []byte {
		var result []byte
		for channel := byte(0); channel < 4; channel++ {
			// Author the wave before its controller at the same instant.
			result = append(result, 0, channel<<6|1, 1, 0, 0, channel<<6|0x37, volume+channel)
		}
		return result
	}
	raw := []byte{0x12, 0x34}
	events := decodePCMControls(t,
		pcmControlTrack(7, sequence(10), raw),
		limitsScore(MobileStandardNoCompress,
			chunk("Mtsp", chunk("Mwa\x01", []byte{0x11, 0x1f, 0x40, 0, 128, 255})),
			chunk("Mtsq", []byte{0, 0xb0, 7, 99, 0, 0x90, 0, 100, 1})),
		pcmControlTrack(7, sequence(20), raw),
	)
	var controls []Event
	var groups []uint16
	waveSeen, midiControl := false, false
	for _, event := range events {
		switch event.Type {
		case EventPCMControl, EventControlChange:
			if waveSeen {
				t.Fatal("same-time controller followed a wave")
			}
			if event.Type == EventPCMControl {
				controls = append(controls, event)
			} else if event.Control == 7 && event.Value == 99 {
				midiControl = true
			}
		case EventWave:
			waveSeen = true
			groups = append(groups, event.PCMChannel)
			want := DecodeADPCM(raw)
			if event.PCMChannel == 0 {
				want = []int16{-32768, 0, 32512}
			}
			if event.WaveChannels != 1 || event.SamplingRate != 8000 || !reflect.DeepEqual(event.Wave, want) {
				t.Fatalf("group %d changed raw PCM or its physical format", event.PCMChannel)
			}
		}
	}
	var want []Event
	for group := uint16(1); group <= 8; group++ {
		value := uint8(10 + (group-1)%4 + (group-1)/4*10)
		want = append(want, Event{Type: EventPCMControl, PCMChannel: group, Control: 7, Value: value})
	}
	if !reflect.DeepEqual(controls, want) || !reflect.DeepEqual(groups, []uint16{1, 2, 3, 4, 0, 5, 6, 7, 8}) || !midiControl {
		t.Fatalf("track isolation or ordering differs: controls=%v groups=%v MIDI=%v", controls, groups, midiControl)
	}
}

func TestDecodePCMControlsKeepTimelineAndRawWave(t *testing.T) {
	raw := bytes.Repeat([]byte{0x12, 0x34}, 200) // 100 ms of decoded mono PCM.
	track := pcmControlTrack(0, []byte{
		0, 1, 1,
		0, 0, 0x37, 0,
		5, 0, 0x3b, 127,
		5, 0, 0x3a, 0,
		5, 0, 0x36, 96,
		5, 0, 0x05,
		5, 0, 0x37, 127,
		5, 0, 0x3a, 127,
		5, 0, 0x34, 64, // Pitch bend remains unsupported but spends duration.
		5, 0, 0,
	}, raw)
	track[12] = 1 // Two milliseconds per duration unit.
	events := decodePCMControls(t, track)
	want := []Event{
		{Type: EventPCMControl, PCMChannel: 1, Control: 7, Value: 0},
		{Type: EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 8000, Wave: DecodeADPCM(raw)},
		{Time: 10, Type: EventPCMControl, PCMChannel: 1, Control: 11, Value: 127},
		{Time: 20, Type: EventPCMControl, PCMChannel: 1, Control: 10, Value: 0},
		{Time: 30, Type: EventPCMControl, PCMChannel: 1, Control: 11, Value: 96},
		{Time: 40, Type: EventPCMControl, PCMChannel: 1, Control: 11, Value: 55},
		{Time: 50, Type: EventPCMControl, PCMChannel: 1, Control: 7, Value: 127},
		{Time: 60, Type: EventPCMControl, PCMChannel: 1, Control: 10, Value: 127},
		{Time: 80, Type: EventEnd},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("PCM control timeline or raw wave differs: got %d events, want %d", len(events), len(want))
	}
}

func TestDecodePCMControlsIgnoreMalformedValuesWithoutLosingTime(t *testing.T) {
	events := decodePCMControls(t, pcmControlTrack(0, []byte{
		2, 0, 0x37, 128,
		3, 0, 0x3b, 255,
		5, 0, 0x3a, 128,
		7, 0, 0x36, 255,
		11, 0, 0x3b, 0,
		13, 0, 0,
	}, nil))
	want := []Event{
		{Time: 28, Type: EventPCMControl, PCMChannel: 1, Control: 11, Value: 0},
		{Time: 41, Type: EventEnd},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("malformed controls changed the later timeline: got %v, want %v", events, want)
	}
}

func TestDecodePCMControlsKeepUnsupportedTracksSilent(t *testing.T) {
	for _, waveType := range []uint16{0x0100, 0x9100, 0x2100} {
		t.Run(fmt.Sprintf("format-%04x", waveType), func(t *testing.T) {
			sequence := []byte{0, 0, 0x37, 127, 0, 1, 1}
			unsupported := pcmControlTrack(0, sequence, []byte{0x12})
			unsupported[10], unsupported[11] = byte(waveType>>8), byte(waveType)
			events := decodePCMControls(t, unsupported, pcmControlTrack(0, sequence, []byte{0x12}))
			want := []Event{
				{Type: EventPCMControl, PCMChannel: 5, Control: 7, Value: 127},
				{Type: EventWave, PCMChannel: 5, WaveChannels: 1, SamplingRate: 8000, Wave: DecodeADPCM([]byte{0x12})},
				{Type: EventEnd},
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("unsupported track emitted output or changed later group numbering: %v", events)
			}
		})
	}
}

func TestDecodePCMControlsSpendOutputEventBudget(t *testing.T) {
	data := buildSMAF(pcmControlTrack(0, []byte{0, 0, 0x37, 64, 1, 0, 0x3b, 127, 1, 0, 0x3a, 64}, nil))
	events := limitsPlayBoundary(t, data, 4, func(limits *decodeLimits, allowed int) { limits.events = allowed })
	if len(events) != 4 || events[0].Type != EventPCMControl || events[2].Time != 2 || events[3].Type != EventEnd {
		t.Fatalf("controller-only track emitted unexpected events: %v", events)
	}
}

func TestDecodePCMControlsRefuseGroupCounterOverflow(t *testing.T) {
	maxTracks := int(^uint16(0)) / 4
	for _, count := range []int{maxTracks, maxTracks + 1} {
		t.Run(fmt.Sprintf("tracks-%d", count), func(t *testing.T) {
			tracks := make([][]byte, count)
			for index := range tracks {
				tracks[index] = limitsPCM()
			}
			tracks[count-1] = pcmControlTrack(0, []byte{0, 0, 0xf7, 127, 0, 0xc1, 1}, []byte{0x12})
			budget := newDecodeBudget()
			budget.limits.chunks = count + 2 // Exercise beyond the public chunk ceiling.
			events := playWithBudget(buildSMAF(tracks...), budget)
			if count > maxTracks {
				if events != nil || !errors.Is(budget.err, ErrResourceLimit) {
					t.Fatalf("group overflow retained %d events: %v", len(events), budget.err)
				}
				return
			}
			if budget.err != nil || len(events) != count+2 || events[0].Type != EventPCMControl || events[0].PCMChannel != 65532 || events[1].Type != EventWave || events[1].PCMChannel != 65532 {
				t.Fatalf("last complete channel group was not preserved: events=%d error=%v", len(events), budget.err)
			}
		})
	}
}
