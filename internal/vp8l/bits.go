package vp8l

// bitWriter packs values least significant bit first, which is the order the
// VP8L bitstream is read in.
type bitWriter struct {
	out   []byte
	value uint64
	count uint
}

// write appends the low bits of value. At most 32 bits go in one call.
func (w *bitWriter) write(value uint32, bits uint) {
	w.value |= uint64(value&(1<<bits-1)) << w.count
	w.count += bits
	if w.count >= 32 {
		w.out = append(w.out, byte(w.value), byte(w.value>>8), byte(w.value>>16), byte(w.value>>24))
		w.value >>= 32
		w.count -= 32
	}
}

// finish pads the last byte with zeros and answers everything written.
func (w *bitWriter) finish() []byte {
	for w.count > 0 {
		w.out = append(w.out, byte(w.value))
		w.value >>= 8
		w.count = max(w.count, 8) - 8
	}
	return w.out
}
