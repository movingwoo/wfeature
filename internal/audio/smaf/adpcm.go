package smaf

// Yamaha ADPCM-B decoding, the only wave encoding SMAF files in the wild
// actually use.

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

// The difference is a sum of the step size's shifts, one per magnitude bit,
// each rounded down on its own: an eighth always, then a quarter, a half and
// the whole step for bits 0, 1 and 2. Rounding the sum once instead,
// ((1+2·delta)·step)>>3, is up to two counts larger per sample, and the error
// is kept for the rest of the sound like any other (docs/audio.md, "Rounding
// each bit of the difference").
func (state *adpcmState) step(nibble uint8) int16 {
	sign := nibble & 8
	delta := uint32(nibble & 7)
	difference := state.stepSize >> 3
	if delta&1 != 0 {
		difference += state.stepSize >> 2
	}
	if delta&2 != 0 {
		difference += state.stepSize >> 1
	}
	if delta&4 != 0 {
		difference += state.stepSize
	}

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

// DecodeADPCM expands Yamaha ADPCM-B into signed 16-bit samples. It returns nil
// when the output would exceed the per-file PCM byte limit.
//
// **The low nibble of a byte is the earlier sample.** Reading them the other
// way round decodes a stream that is not the one that was written, and because
// each sample is a step from the one before it there is nothing later to put it
// right: the error is kept. Read high-first, the local set's sounds drift; read
// low-first they stay centred. Same evidence as the table above.
func DecodeADPCM(data []byte) []int16 {
	if len(data) > maxDecodedPCMBytes/4 {
		return nil
	}
	samples := make([]int16, 0, len(data)*2)
	state := adpcmState{stepSize: 127}
	for _, encoded := range data {
		samples = append(samples, state.step(encoded&0x0f), state.step(encoded>>4))
	}
	return samples
}
