package smaf

import (
	"slices"
	"testing"
)

// **A wave struck again is not expanded again.** Every strike of one wave in
// one decode carries the same read-only samples, so a score that strikes a
// drum wave thousands of times costs one expansion, not thousands. Different
// waves keep their own samples.
func TestRepeatedStreamWaveStrikesShareSamples(t *testing.T) {
	adpcm := WaveData{Channels: Mono, Format: YamahaADPCM, BaseBit: Bit4, SamplingFreq: 8000, Data: []byte{0x17, 0x71, 0x3c, 0xc3}}
	unsigned := WaveData{Channels: Mono, Format: OffsetBinaryPCM, BaseBit: Bit8, SamplingFreq: 8000, Data: []byte{0x80, 0xc0, 0x40}}
	waves := map[uint8]WaveData{1: adpcm, 2: unsigned, 5: adpcm}
	tones := newToneMap()
	tones.initTrack(MobileStandardNoCompress, nil, 0)
	strike := func(duration uint32, channel, note uint8) SequenceEvent {
		return SequenceEvent{Kind: SeqNote, Duration: duration, Channel: channel, Note: note, Velocity: 100, HasVelocity: true, GateTime: 1}
	}
	events, _ := sequenceEvents([]SequenceEvent{
		strike(0, 0, 0),               // wave 1 through note zero
		strike(1, 1, 0),               // wave 2
		strike(1, 0, 0),               // wave 1 again
		strike(1, waveNoteChannel, 4), // wave 5: the same bytes under another number
		strike(1, 0, 0),               // wave 1 a third time
	}, 1, 1, 0, false, waves, tones, newDecodeBudget())

	var strikes [][]int16
	for _, event := range events {
		if event.Type == EventWave {
			strikes = append(strikes, event.Wave)
		}
	}
	if len(strikes) != 5 {
		t.Fatalf("got %d wave events, want 5: %+v", len(strikes), events)
	}
	if want := DecodeADPCM(adpcm.Data); !slices.Equal(strikes[0], want) {
		t.Fatalf("wave 1 samples = %v, want %v", strikes[0], want)
	}
	for _, index := range []int{2, 3, 4} {
		if &strikes[index][0] != &strikes[0][0] {
			t.Errorf("strike %d expanded wave 1's bytes again instead of sharing its samples", index)
		}
	}
	if &strikes[1][0] == &strikes[0][0] || !slices.Equal(strikes[1], []int16{0, 0x4000, -0x4000}) {
		t.Errorf("wave 2 = %v, want its own samples", strikes[1])
	}
}

func TestRepeatedPCMTrackWaveStrikesShareSamples(t *testing.T) {
	events := pcmTrackEvents(&PCMAudioTrack{
		Format: PCMAdpcm, Channels: Mono, SamplingFreq: 8000, TimebaseD: 1, TimebaseG: 1,
		Waves: map[uint8][]byte{1: {0x17, 0x71}, 2: {0x3c, 0xc3}},
		Sequence: []PCMEvent{
			{Kind: PCMEventWave, WaveNumber: 1},
			{Kind: PCMEventWave, Duration: 1, WaveNumber: 2},
			{Kind: PCMEventWave, Duration: 1, WaveNumber: 1},
		},
	}, 1, newDecodeBudget())
	var strikes [][]int16
	for _, event := range events {
		if event.Type == EventWave {
			strikes = append(strikes, event.Wave)
		}
	}
	if len(strikes) != 3 {
		t.Fatalf("got %d wave events, want 3: %+v", len(strikes), events)
	}
	if &strikes[2][0] != &strikes[0][0] {
		t.Error("the second strike of wave 1 expanded it again")
	}
	if &strikes[1][0] == &strikes[0][0] || !slices.Equal(strikes[1], DecodeADPCM([]byte{0x3c, 0xc3})) {
		t.Errorf("wave 2 = %v, want its own samples", strikes[1])
	}
}
