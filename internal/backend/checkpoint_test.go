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
// earlier format kept the length of a save generation. The sections follow the
// header deflated, and they are all that follows it.
func TestCheckpointEnvelopeCarriesNoSaves(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	saved := Checkpoint{Identity: identity, Variant: CheckpointKTFJava, Session: []byte("session"), Runtime: []byte("runtime")}
	data, err := EncodeCheckpoint(saved)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(data[8:10]) != 4 || binary.LittleEndian.Uint32(data[52:56]) != 0 {
		t.Fatalf("version %d with reserved word %#x, want version 4 and zero",
			binary.LittleEndian.Uint16(data[8:10]), binary.LittleEndian.Uint32(data[52:56]))
	}
	if session, runtime := binary.LittleEndian.Uint32(data[44:48]), binary.LittleEndian.Uint32(data[48:52]); session != 7 || runtime != 7 {
		t.Fatalf("section lengths are %d and %d, want the sections' own 7 and 7", session, runtime)
	}
	if sections, err := inflateCheckpoint(data[checkpointHeaderSize:], 14); err != nil || string(sections) != "sessionruntime" {
		t.Fatalf("the deflated body is %q, %v; want the two sections and nothing else", sections, err)
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

// An envelope of an earlier format is refused for its version, whatever its
// size. Version 1 embedded a save generation and could be larger than this
// one's limit; version 2, which 0.5.2 wrote, has this layout but records this
// build cannot read. Its owner has to be told "an earlier format", not
// "damaged". A buffer too short to hold a version is damaged and is not read
// past its end.
func TestCheckpointRefusesAnEarlierFormatByVersion(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	earlier := func(version uint16, size int) []byte {
		data := make([]byte, size)
		copy(data, checkpointMagic)
		binary.LittleEndian.PutUint16(data[8:10], version)
		binary.LittleEndian.PutUint16(data[10:12], CheckpointKTFJava)
		copy(data[12:44], identity[:])
		return data
	}
	for _, version := range []uint16{1, 2} {
		for _, size := range []int{checkpointHeaderSize, checkpointHeaderSize + 4096, CheckpointLimit + 1} {
			if _, err := DecodeCheckpoint(earlier(version, size), identity); !errors.Is(err, ErrCheckpointVersion) {
				t.Fatalf("a version %d envelope of %d bytes was refused with %v, want the version", version, size, err)
			}
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

// A quick save an earlier build wrote is version 3: the same header with the
// sections stored as they are. This build reads it, so upgrading keeps every
// slot that was already there, and it judges its damage the same way.
func TestCheckpointReadsTheVersion3AnEarlierBuildWrote(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	saved := Checkpoint{Identity: identity, Variant: CheckpointLGTJava, Session: []byte("{}"), Runtime: bytes.Repeat([]byte("guest memory "), 64)}
	earlier := append(checkpointHeader(checkpointVersion, saved), saved.Session...)
	earlier = append(earlier, saved.Runtime...)
	got, err := DecodeCheckpoint(earlier, identity)
	if err != nil {
		t.Fatalf("a version 3 slot was refused: %v", err)
	}
	if !reflect.DeepEqual(got, saved) {
		t.Fatalf("a version 3 slot decoded as %+v", got)
	}
	for name, damaged := range map[string][]byte{
		"truncated": earlier[:len(earlier)-1],
		"trailing":  append(bytes.Clone(earlier), 0),
		"payload":   append(bytes.Clone(earlier[:len(earlier)-1]), earlier[len(earlier)-1]+1),
	} {
		if _, err := DecodeCheckpoint(damaged, identity); !errors.Is(err, ErrCheckpointDamaged) {
			t.Fatalf("a %s version 3 slot was refused with %v, want damaged", name, err)
		}
	}
	// Writing it again stores the deflated form.
	again, err := EncodeCheckpoint(got)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(again[8:10]) != checkpointDeflatedVersion || len(again) >= len(earlier) {
		t.Fatalf("rewritten slot is version %d and %d bytes, want version 4 below %d bytes",
			binary.LittleEndian.Uint16(again[8:10]), len(again), len(earlier))
	}
}

// The header says how long the sections are, and the deflated stream must
// produce exactly that: a stream that runs short or long is damage, however
// small the file. A file that only claims large sections allocates what its
// stream really produces.
func TestCheckpointRefusesDeflatedSectionsThatDisagreeWithTheirLengths(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	saved := Checkpoint{Identity: identity, Variant: CheckpointSKTJava, Session: []byte("{}"), Runtime: []byte("runtime")}
	data, err := EncodeCheckpoint(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, runtime := range []uint32{6, 8, checkpointRuntimeLimit} {
		edited := bytes.Clone(data)
		binary.LittleEndian.PutUint32(edited[48:52], runtime)
		if _, err := DecodeCheckpoint(edited, identity); !errors.Is(err, ErrCheckpointDamaged) {
			t.Fatalf("a stream of 9 bytes declared as 2 and %d was refused with %v, want damaged", runtime, err)
		}
	}
	// A stream that is not deflate at all is damage too.
	garbage := append(bytes.Clone(data[:checkpointHeaderSize]), 0xff, 0xff, 0xff, 0xff)
	if _, err := DecodeCheckpoint(garbage, identity); !errors.Is(err, ErrCheckpointDamaged) {
		t.Fatalf("an undeflatable body was refused with %v, want damaged", err)
	}
}

// Guest memory is mostly runs and repeats, which is why the sections are
// deflated: a megabyte of it is stored in a small fraction of that.
func TestCheckpointDeflatesItsSections(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	runtime := make([]byte, 1<<20)
	for index := range runtime {
		if index%4096 < 64 {
			runtime[index] = byte(index)
		}
	}
	data, err := EncodeCheckpoint(Checkpoint{Identity: identity, Variant: CheckpointKTFNative, Session: []byte("{}"), Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > len(runtime)/10 {
		t.Fatalf("a megabyte of sparse memory was stored in %d bytes", len(data))
	}
	got, err := DecodeCheckpoint(data, identity)
	if err != nil || !bytes.Equal(got.Runtime, runtime) {
		t.Fatalf("the deflated runtime did not come back whole: %v", err)
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
			copy(b[56:88], checkpointDigest(b, []byte("{}"), []byte("{}")))
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
