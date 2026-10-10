package smaf

import (
	"errors"
	"fmt"
)

// ErrResourceLimit reports a file whose expansion exceeds the decoder budget.
// Unlike a malformed trailing chunk, this rejects the entire file.
var ErrResourceLimit = errors.New("SMAF resource limit exceeded")

const (
	maxDecodedSequenceBytes = 8 << 20
	maxDecodedPCMBytes      = 64 << 20
)

type decodeLimits struct {
	inputBytes, sequenceBytes, sequenceEvents, chunks int
	events, pcmBytes, sysExBytes                      int
}

func defaultDecodeLimits() decodeLimits {
	return decodeLimits{
		inputBytes: 64 << 20, sequenceBytes: maxDecodedSequenceBytes,
		sequenceEvents: 1 << 18, chunks: 4096, events: 1 << 20,
		pcmBytes: maxDecodedPCMBytes, sysExBytes: 8 << 20,
	}
}

// A budget belongs to one Parse/Play call and is shared by every nested track.
// Sticky errors keep tolerant chunk parsing from accepting a truncated prefix
// after a hard resource refusal. Counters include discarded parsing work.
type decodeBudget struct {
	limits decodeLimits
	used   decodeLimits
	err    error
}

func newDecodeBudget() *decodeBudget { return &decodeBudget{limits: defaultDecodeLimits()} }

func (b *decodeBudget) charge(used *int, count, limit int, resource string) error {
	if b.err != nil {
		return b.err
	}
	if count < 0 || *used > limit || count > limit-*used {
		b.err = fmt.Errorf("%w: %s exceeds %d", ErrResourceLimit, resource, limit)
		return b.err
	}
	*used += count
	return nil
}

func (b *decodeBudget) chunk() error {
	return b.charge(&b.used.chunks, 1, b.limits.chunks, "chunks")
}

func (b *decodeBudget) sequence(size uint64) error {
	// Compare the untrusted 32-bit Huffman length before conversion to int.
	if b.used.sequenceBytes > b.limits.sequenceBytes || size > uint64(b.limits.sequenceBytes-b.used.sequenceBytes) {
		return b.charge(&b.used.sequenceBytes, -1, b.limits.sequenceBytes, "decoded sequence bytes")
	}
	return b.charge(&b.used.sequenceBytes, int(size), b.limits.sequenceBytes, "decoded sequence bytes")
}

func (b *decodeBudget) sequenceEvent() error {
	return b.charge(&b.used.sequenceEvents, 1, b.limits.sequenceEvents, "sequence records")
}

func (b *decodeBudget) emit(events []Event, added ...Event) []Event {
	if b.charge(&b.used.events, len(added), b.limits.events, "output events") != nil {
		return events
	}
	return append(events, added...)
}

func (b *decodeBudget) pcm(encodedBytes, expansion int) bool {
	if b.used.pcmBytes > b.limits.pcmBytes || encodedBytes > (b.limits.pcmBytes-b.used.pcmBytes)/expansion {
		b.charge(&b.used.pcmBytes, -1, b.limits.pcmBytes, "decoded PCM bytes")
		return false
	}
	return b.charge(&b.used.pcmBytes, encodedBytes*expansion, b.limits.pcmBytes, "decoded PCM bytes") == nil
}

func (b *decodeBudget) sysEx(data []byte) []byte {
	size := len(data)
	if size == 0 || data[0] != 0xf0 {
		size++
	}
	if len(data) == 0 || data[len(data)-1] != 0xf7 {
		size++
	}
	if b.charge(&b.used.sysExBytes, size, b.limits.sysExBytes, "SysEx bytes") != nil {
		return nil
	}
	return sysExMessage(data)
}

func (b *decodeBudget) milliseconds(value uint64) uint32 {
	if value > uint64(^uint32(0)) {
		if b.err == nil {
			b.err = fmt.Errorf("%w: event time exceeds %d milliseconds", ErrResourceLimit, uint64(^uint32(0)))
		}
		return 0
	}
	return uint32(value)
}
