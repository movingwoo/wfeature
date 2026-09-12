package skt

// scriptRandom keeps the original 32-bit recurrence local to a session.
// Its 15-bit result is intentionally not a uniformly sampled host integer.
type scriptRandom uint32

func (r *scriptRandom) next() int {
	*r = scriptRandom(uint32(*r)*214013 + 2531011)
	return int(uint32(*r)>>16) & 32767
}
