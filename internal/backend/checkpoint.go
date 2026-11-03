package backend

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

const (
	checkpointMagic = "WFSTATE\x00"
	// checkpointVersion names the records inside the envelope as well as its
	// layout. Version 3 keeps version 2's layout; it is the first whose
	// session and runtime records carry this build's audio, JVM heap and media
	// listener state, which a version 2 record lacks. A slot file is named
	// after it, so an earlier build's slot is refused for what it is.
	checkpointVersion = 3
	// checkpointDeflatedVersion is version 3's records in version 3's header,
	// with the two sections deflated together after it. It is what this build
	// writes: a record is mostly guest memory, and a quick save of several
	// megabytes deflates to a tenth or a sixth of that. The records are
	// version 3's, so a slot keeps the version 3 name and a version 3 file an
	// earlier build wrote is still read; an earlier build refuses this one as
	// a version it does not support and leaves it where it is.
	checkpointDeflatedVersion = 4
	checkpointHeaderSize      = 88
	checkpointSessionLimit    = 64 << 10
	checkpointRuntimeLimit    = 128 << 20
	// CheckpointLimit bounds an entire execution checkpoint before a Host reads
	// or accepts an uploaded file. A checkpoint carries no ordinary save, and
	// save backups have their own format.
	CheckpointLimit = checkpointHeaderSize + checkpointSessionLimit + checkpointRuntimeLimit

	CheckpointKTFJava   uint16 = 1
	CheckpointKTFModule uint16 = 2
	CheckpointKTFNative uint16 = 3
	// The LGT variants share one runtime; the number says which continuation
	// the record carries. A Clet is called and returns, so its record has no
	// parked guest call. An AOT Java title keeps guest threads parked inside
	// platform calls, and its record carries what each one still owes.
	CheckpointLGTClet   uint16 = 4
	CheckpointLGTJava   uint16 = 5
	CheckpointSKTJava   uint16 = 6
	CheckpointSKTScript uint16 = 7
	// CheckpointSKTClet is an SKT native WIPI-C program. Like an LGT Clet it
	// is called and returns, so its record has no parked guest call.
	CheckpointSKTClet uint16 = 8
)

var (
	ErrNotCheckpoint      = errors.New("backend: this is not an execution checkpoint")
	ErrCheckpointVersion  = errors.New("backend: this checkpoint version or execution variant is unsupported")
	ErrCheckpointDamaged  = errors.New("backend: this checkpoint is damaged or exceeds its limits")
	ErrCheckpointIdentity = errors.New("backend: this checkpoint belongs to a different archive")
)

// Checkpoint joins a shared session record and a platform execution record.
// It carries no ordinary save: a load restores execution state and leaves the
// game's saves where they are, so a slot holds nothing a load could put back.
// State codecs validate their own schemas; this envelope validates lengths,
// archive identity and transfer integrity. A checksum is not authentication.
// Every inner record remains untrusted.
type Checkpoint struct {
	Identity [32]byte
	Variant  uint16
	Session  []byte
	Runtime  []byte
}

func checkpointVariantSupported(variant uint16) bool {
	return variant >= CheckpointKTFJava && variant <= CheckpointSKTClet
}

// EncodeCheckpoint writes a profile-independent, little-endian version 4
// envelope: magic(8), version(2), variant(2), archive SHA-256(32), two section
// lengths(4 each), a reserved word that is zero(4), SHA-256(32), then the
// session and runtime sections deflated as one stream. The lengths are the
// sections' own, and the digest covers the first 56 header bytes and both
// sections as they are before deflating. The reserved word is where version 1
// kept the length of an embedded save generation; versions 2 and 3 had this
// same layout with the sections stored as they are.
func EncodeCheckpoint(saved Checkpoint) ([]byte, error) {
	if !checkpointVariantSupported(saved.Variant) {
		return nil, ErrCheckpointVersion
	}
	if len(saved.Session) > checkpointSessionLimit || len(saved.Runtime) > checkpointRuntimeLimit {
		return nil, ErrCheckpointDamaged
	}
	var data bytes.Buffer
	data.Write(checkpointHeader(checkpointDeflatedVersion, saved))
	deflater, err := flate.NewWriter(&data, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	for _, section := range [][]byte{saved.Session, saved.Runtime} {
		if _, err := deflater.Write(section); err != nil {
			return nil, err
		}
	}
	if err := deflater.Close(); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

// checkpointHeader lays out the 88 header bytes for a version and fills in the
// digest of the sections as they are.
func checkpointHeader(version uint16, saved Checkpoint) []byte {
	header := make([]byte, checkpointHeaderSize)
	copy(header, checkpointMagic)
	binary.LittleEndian.PutUint16(header[8:10], version)
	binary.LittleEndian.PutUint16(header[10:12], saved.Variant)
	copy(header[12:44], saved.Identity[:])
	binary.LittleEndian.PutUint32(header[44:48], uint32(len(saved.Session)))
	binary.LittleEndian.PutUint32(header[48:52], uint32(len(saved.Runtime)))
	copy(header[56:88], checkpointDigest(header, saved.Session, saved.Runtime))
	return header
}

func checkpointDigest(header []byte, sections ...[]byte) []byte {
	hash := sha256.New()
	_, _ = hash.Write(header[:56])
	for _, section := range sections {
		_, _ = hash.Write(section)
	}
	return hash.Sum(nil)
}

// inflateCheckpoint inflates exactly size bytes and requires the stream, and
// the file, to end there. The output grows with what the stream really
// produces, so a short file that claims large sections costs no more than its
// own output, which the section limits already bound.
func inflateCheckpoint(deflated []byte, size uint64) ([]byte, error) {
	source := bytes.NewReader(deflated)
	inflater := flate.NewReader(source)
	defer inflater.Close()
	var sections bytes.Buffer
	if written, err := io.CopyN(&sections, inflater, int64(size)); err != nil || uint64(written) != size {
		return nil, ErrCheckpointDamaged
	}
	var extra [1]byte
	if count, err := io.ReadFull(inflater, extra[:]); count != 0 || err != io.EOF || source.Len() != 0 {
		return nil, ErrCheckpointDamaged
	}
	return sections.Bytes(), nil
}

// DecodeCheckpoint reads version 3, which earlier builds wrote, and version 4,
// which this one writes. It checks the expected archive before copying the
// sections out; a version 4 file is inflated first, because its digest is of
// the inflated sections. Returned sections do not alias the input buffer.
//
// The version is read before the size is bounded, so an envelope of an earlier
// format answers ErrCheckpointVersion whatever its size: an earlier format
// could be larger than this one's limit, and "damaged" would be the wrong
// thing to tell its owner.
func DecodeCheckpoint(data []byte, identity [32]byte) (Checkpoint, error) {
	if len(data) < 8 || string(data[:8]) != checkpointMagic {
		return Checkpoint{}, ErrNotCheckpoint
	}
	if len(data) < checkpointHeaderSize {
		return Checkpoint{}, ErrCheckpointDamaged
	}
	version := binary.LittleEndian.Uint16(data[8:10])
	variant := binary.LittleEndian.Uint16(data[10:12])
	if version != checkpointVersion && version != checkpointDeflatedVersion || !checkpointVariantSupported(variant) {
		return Checkpoint{}, ErrCheckpointVersion
	}
	if len(data) > CheckpointLimit {
		return Checkpoint{}, ErrCheckpointDamaged
	}
	sessionSize := uint64(binary.LittleEndian.Uint32(data[44:48]))
	runtimeSize := uint64(binary.LittleEndian.Uint32(data[48:52]))
	if binary.LittleEndian.Uint32(data[52:56]) != 0 || sessionSize > checkpointSessionLimit || runtimeSize > checkpointRuntimeLimit {
		return Checkpoint{}, ErrCheckpointDamaged
	}
	sections := data[checkpointHeaderSize:]
	if version == checkpointDeflatedVersion {
		inflated, err := inflateCheckpoint(sections, sessionSize+runtimeSize)
		if err != nil {
			return Checkpoint{}, err
		}
		sections = inflated
	}
	if sessionSize+runtimeSize != uint64(len(sections)) || !bytes.Equal(data[56:88], checkpointDigest(data, sections)) {
		return Checkpoint{}, ErrCheckpointDamaged
	}
	if !bytes.Equal(data[12:44], identity[:]) {
		return Checkpoint{}, ErrCheckpointIdentity
	}
	return Checkpoint{Identity: identity, Variant: variant, Session: bytes.Clone(sections[:sessionSize]), Runtime: bytes.Clone(sections[sessionSize:])}, nil
}
