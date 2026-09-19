package ktf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestRelayEnvelopeWireLayoutAndFragments(t *testing.T) {
	wire := []byte{0, 0, 0, 0, 13, 0, 3, 6, 1, 42, 0xaa, 0xbb, 0xcc}
	for n := 0; n < len(wire); n++ {
		if _, consumed, err := decodeRelayFrame(wire[:n]); !errors.Is(err, io.ErrUnexpectedEOF) || consumed != 0 {
			t.Fatalf("fragment %d consumed=%d error=%v", n, consumed, err)
		}
	}
	frame, consumed, err := decodeRelayFrame(append(bytes.Clone(wire), wire...))
	if err != nil || consumed != len(wire) || !bytes.Equal(frame.extensions, []byte{6, 1, 42}) || !bytes.Equal(frame.payload, []byte{0xaa, 0xbb, 0xcc}) {
		t.Fatalf("frame=%+v consumed=%d error=%v", frame, consumed, err)
	}
	encoded, err := encodeRelayFrame(frame)
	if err != nil || !bytes.Equal(encoded, wire) {
		t.Fatalf("encoded=%x error=%v", encoded, err)
	}
}

func TestRelayEnvelopeRejectsInvalidLengthsBeforeBody(t *testing.T) {
	for _, size := range []uint32{0, 6, maxRelayFrameSize + 1, 0xffffffff} {
		prefix := make([]byte, 5)
		binary.BigEndian.PutUint32(prefix[1:], size)
		if _, _, err := decodeRelayFrame(prefix); err == nil || errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("length %d was not rejected: %v", size, err)
		}
	}
	for _, wire := range [][]byte{{1}, {0, 0, 0, 0, 7, 0, 1}} {
		if _, _, err := decodeRelayFrame(wire); err == nil || errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("invalid prefix %x: %v", wire, err)
		}
	}
	for _, frame := range []relayFrame{{extensions: make([]byte, 65536)}, {payload: make([]byte, maxRelayFrameSize)}} {
		if _, err := encodeRelayFrame(frame); err == nil {
			t.Fatal("oversized frame accepted")
		}
	}
	frame := relayFrame{payload: make([]byte, maxRelayFrameSize-relayPrefixSize)}
	wire, err := encodeRelayFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, consumed, err := decodeRelayFrame(wire); err != nil || consumed != maxRelayFrameSize {
		t.Fatalf("maximum frame consumed=%d error=%v", consumed, err)
	}
}
