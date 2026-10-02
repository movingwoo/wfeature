package backend

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

// A slot holds execution state and nothing a load could put back over the
// game's saves: the envelope has two sections and a reserved word where the
// earlier format kept the length of a save generation.
func TestCheckpointEnvelopeCarriesNoSaves(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	saved := Checkpoint{Identity: identity, Variant: CheckpointKTFJava, Session: []byte("session"), Runtime: []byte("runtime")}
	data, err := EncodeCheckpoint(saved)
	if err != nil {
		t.Fatal(err)
	}
	if want := checkpointHeaderSize + len("session") + len("runtime"); len(data) != want {
		t.Fatalf("the envelope is %d bytes, want the header and two sections: %d", len(data), want)
	}
	if binary.LittleEndian.Uint16(data[8:10]) != 2 || binary.LittleEndian.Uint32(data[52:56]) != 0 {
		t.Fatalf("version %d with reserved word %#x, want version 2 and zero",
			binary.LittleEndian.Uint16(data[8:10]), binary.LittleEndian.Uint32(data[52:56]))
	}
	first := bytes.Clone(data)
	got, err := DecodeCheckpoint(data, identity)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, saved) {
		t.Fatalf("decoded checkpoint differs: %+v", got)
	}
	again, err := EncodeCheckpoint(got)
	if err != nil || !bytes.Equal(first, again) {
		t.Fatalf("encoding is not stable: %v", err)
	}
	clear(data)
	if string(got.Session) != "session" || string(got.Runtime) != "runtime" {
		t.Fatal("decoded checkpoint shares the input buffer")
	}
}

// An envelope of the earlier format is refused for its version, whatever its
// size. That format embedded a save generation and could be larger than this
// one's limit, and its owner has to be told "an earlier format", not "damaged".
// A buffer too short to hold a version is damaged and is not read past its end.
func TestCheckpointRefusesAnEarlierFormatByVersion(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	earlier := func(size int) []byte {
		data := make([]byte, size)
		copy(data, checkpointMagic)
		binary.LittleEndian.PutUint16(data[8:10], 1)
		binary.LittleEndian.PutUint16(data[10:12], CheckpointKTFJava)
		copy(data[12:44], identity[:])
		return data
	}
	for _, size := range []int{checkpointHeaderSize, checkpointHeaderSize + 4096, CheckpointLimit + 1} {
		if _, err := DecodeCheckpoint(earlier(size), identity); !errors.Is(err, ErrCheckpointVersion) {
			t.Fatalf("an earlier envelope of %d bytes was refused with %v, want the version", size, err)
		}
	}
	current, err := EncodeCheckpoint(Checkpoint{Identity: identity, Variant: CheckpointKTFJava})
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{8, 9, 10, 11, 12, checkpointHeaderSize - 1} {
		if _, err := DecodeCheckpoint(current[:size], identity); !errors.Is(err, ErrCheckpointDamaged) {
			t.Fatalf("a %d-byte buffer was refused with %v, want damaged", size, err)
		}
	}
	if _, err := DecodeCheckpoint(current[:7], identity); !errors.Is(err, ErrNotCheckpoint) {
		t.Fatalf("a buffer shorter than the magic was refused with %v", err)
	}
	// The current version over its own limit is damage, not a version.
	large := make([]byte, CheckpointLimit+1)
	copy(large, current[:checkpointHeaderSize])
	if _, err := DecodeCheckpoint(large, identity); !errors.Is(err, ErrCheckpointDamaged) {
		t.Fatalf("an oversized current envelope was refused with %v, want damaged", err)
	}
}

func TestCheckpointContainerRejectsDamageIdentityAndUnsupportedVariants(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	data, err := EncodeCheckpoint(Checkpoint{Identity: identity, Variant: CheckpointKTFJava, Session: []byte("{}"), Runtime: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func([]byte) []byte
		want error
	}{
		{"magic", func(b []byte) []byte { b[0]++; return b }, ErrNotCheckpoint},
		{"version", func(b []byte) []byte { b[8]++; return b }, ErrCheckpointVersion},
		{"variant", func(b []byte) []byte { b[10] = 255; return b }, ErrCheckpointVersion},
		{"length", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[48:52], ^uint32(0)); return b }, ErrCheckpointDamaged},
		{"reserved", func(b []byte) []byte {
			// The digest is made right again, so the reserved word is the one
			// thing wrong with this envelope.
			binary.LittleEndian.PutUint32(b[52:56], 1)
			copy(b[56:88], checkpointDigest(b))
			return b
		}, ErrCheckpointDamaged},
		{"digest", func(b []byte) []byte { b[checkpointHeaderSize-1]++; return b }, ErrCheckpointDamaged},
		{"payload", func(b []byte) []byte { b[len(b)-1]++; return b }, ErrCheckpointDamaged},
		{"truncated", func(b []byte) []byte { return b[:len(b)-1] }, ErrCheckpointDamaged},
		{"trailing", func(b []byte) []byte { return append(b, 0) }, ErrCheckpointDamaged},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeCheckpoint(test.edit(bytes.Clone(data)), identity); !errors.Is(err, test.want) {
				t.Fatalf("refusal = %v, want %v", err, test.want)
			}
		})
	}
	if _, err := DecodeCheckpoint(data, SaveIdentity([]byte("different archive"))); !errors.Is(err, ErrCheckpointIdentity) {
		t.Fatalf("different archive refusal = %v", err)
	}
	if _, err := DecodeSavePack(data); !errors.Is(err, ErrNotSavePack) {
		t.Fatalf("checkpoint mistaken for ordinary saves: %v", err)
	}
}
