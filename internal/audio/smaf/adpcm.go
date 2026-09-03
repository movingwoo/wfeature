package smaf

// Yamaha ADPCM-B decoding, the only wave encoding SMAF files in the wild
// actually use. Adapted from superctr's ymb_codec.c (Unlicense), the same
// source the reference implementation used.

// adpcmStepTable scales the step size by how large the last delta was: small
// deltas shrink the step, large ones grow it, which is what lets four bits
// track a sixteen-bit waveform. The entries are 256ths, so the update is a
// multiply and a shift of eight.
//
// **The precision is the whole of it.** These were once carried as 64ths —
// 57, 77, 102, 128, 153 — which is the same five ratios rounded to a quarter
// of the resolution, and four of the five round the wrong way. A ratio that is
// 0.4% small is nothing in one sample and everything in nine thousand: the
// step size this drives is the size of the *next* difference, so an error in
// it is an error in every sample after it, and the predictor walks away from
// the signal instead of tracking it. Every sampled sound in the local KTF set
// decoded to a waveform that ended thousands of counts away from where it
// started and spent part of its length pinned to a rail. See docs/audio.md,
// "A predictor that walked away from the signal".
var adpcmStepTable = [8]uint32{230, 230, 230, 230, 307, 409, 512, 614}

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

	next := (adpcmStepTable[delta] * state.stepSize) >> 8
	if next < 127 {
		next = 127
	} else if next > 24576 {
		next = 24576
	}
	state.stepSize = next
	state.history = value
	return int16(value)
}

// DecodeADPCM expands Yamaha ADPCM-B into signed 16-bit samples.
//
// **The low nibble of a byte is the earlier sample.** Reading them the other
// way round decodes a stream that is not the one that was written, and because
// each sample is a step from the one before it there is nothing later to put it
// right: the error is kept. Read high-first, the local set's sounds drift; read
// low-first they stay centred. Same evidence as the table above.
func DecodeADPCM(data []byte) []int16 {
	samples := make([]int16, 0, len(data)*2)
	state := adpcmState{stepSize: 127}
	for _, encoded := range data {
		samples = append(samples, state.step(encoded&0x0f), state.step(encoded>>4))
	}
	return samples
}
