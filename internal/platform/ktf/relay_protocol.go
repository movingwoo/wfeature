package ktf

import (
	"encoding/binary"
	"fmt"
	"io"
)

// The observed relay envelope is independent of its application's commands.
// It starts with zero, a big-endian total length including the seven-byte
// prefix, and a big-endian extension length. Extensions and payload stay
// opaque here; decoding an envelope does not authorize a service response.
const relayPrefixSize = 7

// This is an emulator resource limit, not a recovered wire-format limit.
const maxRelayFrameSize = 1 << 20

type relayFrame struct {
	extensions []byte
	payload    []byte
}

// decodeRelayFrame borrows slices from data and consumes exactly one frame.
// Incomplete input is distinguishable from a malformed or oversized frame.
func decodeRelayFrame(data []byte) (relayFrame, int, error) {
	if len(data) == 0 {
		return relayFrame{}, 0, io.ErrUnexpectedEOF
	}
	if data[0] != 0 {
		return relayFrame{}, 0, fmt.Errorf("unsupported relay envelope marker %d", data[0])
	}
	if len(data) < 5 {
		return relayFrame{}, 0, io.ErrUnexpectedEOF
	}
	size := binary.BigEndian.Uint32(data[1:5])
	if size < relayPrefixSize || size > maxRelayFrameSize {
		return relayFrame{}, 0, fmt.Errorf("invalid relay frame length %d", size)
	}
	if len(data) < relayPrefixSize {
		return relayFrame{}, 0, io.ErrUnexpectedEOF
	}
	headerSize := uint32(binary.BigEndian.Uint16(data[5:7]))
	if headerSize > size-relayPrefixSize {
		return relayFrame{}, 0, fmt.Errorf("relay extension length %d exceeds frame length %d", headerSize, size)
	}
	if uint64(len(data)) < uint64(size) {
		return relayFrame{}, 0, io.ErrUnexpectedEOF
	}
	start := relayPrefixSize + headerSize
	return relayFrame{extensions: data[relayPrefixSize:start], payload: data[start:size]}, int(size), nil
}

func encodeRelayFrame(frame relayFrame) ([]byte, error) {
	if len(frame.extensions) > 65535 || len(frame.payload) > maxRelayFrameSize-relayPrefixSize-len(frame.extensions) {
		return nil, fmt.Errorf("relay frame exceeds encoding limits")
	}
	size := relayPrefixSize + len(frame.extensions) + len(frame.payload)
	data := make([]byte, size)
	binary.BigEndian.PutUint32(data[1:5], uint32(size))
	binary.BigEndian.PutUint16(data[5:7], uint16(len(frame.extensions)))
	copy(data[relayPrefixSize:], frame.extensions)
	copy(data[relayPrefixSize+len(frame.extensions):], frame.payload)
	return data, nil
}
