package smaf

// Yamaha ADPCM-B decoding, the only wave encoding SMAF files in the wild
// actually use. Adapted from superctr's ymb_codec.c (Unlicense), the same
// source the reference implementation used.

// stepTable scales the step size by how large the last delta was: small
// deltas shrink the step, large ones grow it, which is what lets four bits
// track a sixteen-bit waveform.
var adpcmStepTable = [8]uint32{57, 57, 57, 57, 77, 102, 128, 153}

type adpcmState struct {
	history  int32
	stepSize uint32
}

func (state *adpcmState) step(nibble uint8) int16 {
	sign := nibble & 8
	delta := uint32(nibble & 7)
	difference := ((1 + delta<<1) * state.stepSize) >> 3

	value := state.history
	if sign != 0 {
		value -= int32(difference)
	} else {
		value += int32(difference)
	}
	if value < -32768 {
		value = -32768
	} else if value > 32767 {
		value = 32767
	}

	next := (adpcmStepTable[delta] * state.stepSize) >> 6
	if next < 127 {
		next = 127
	} else if next > 24576 {
		next = 24576
	}
	state.stepSize = next
	state.history = value
	return int16(value)
}

// DecodeADPCM expands Yamaha ADPCM-B into signed 16-bit samples, high nibble
// first.
func DecodeADPCM(data []byte) []int16 {
	samples := make([]int16, 0, len(data)*2)
	state := adpcmState{stepSize: 127}
	for _, encoded := range data {
		samples = append(samples, state.step(encoded>>4), state.step(encoded&0x0f))
	}
	return samples
}
