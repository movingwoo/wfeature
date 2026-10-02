package backend

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const (
	checkpointMagic        = "WFSTATE\x00"
	checkpointVersion      = 2
	checkpointHeaderSize   = 88
	checkpointSessionLimit = 64 << 10
	checkpointRuntimeLimit = 128 << 20
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
	CheckpointLGTClet uint16 = 4
	CheckpointLGTJava uint16 = 5
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
	return variant >= CheckpointKTFJava && variant <= CheckpointLGTJava
}

// EncodeCheckpoint writes a profile-independent, little-endian version 2
// envelope: magic(8), version(2), variant(2), archive SHA-256(32), two section
// lengths(4 each), a reserved word that is zero(4), SHA-256(32), then the
// session and runtime sections. The digest covers the first 56 header bytes
// and both sections. The reserved word is where version 1 kept the length of
// an embedded save generation.
func EncodeCheckpoint(saved Checkpoint) ([]byte, error) {
	if !checkpointVariantSupported(saved.Variant) {
		return nil, ErrCheckpointVersion
	}
	if len(saved.Session) > checkpointSessionLimit || len(saved.Runtime) > checkpointRuntimeLimit {
		return nil, ErrCheckpointDamaged
	}
	data := make([]byte, checkpointHeaderSize, checkpointHeaderSize+len(saved.Session)+len(saved.Runtime))
	copy(data, checkpointMagic)
	binary.LittleEndian.PutUint16(data[8:10], checkpointVersion)
	binary.LittleEndian.PutUint16(data[10:12], saved.Variant)
	copy(data[12:44], saved.Identity[:])
	binary.LittleEndian.PutUint32(data[44:48], uint32(len(saved.Session)))
	binary.LittleEndian.PutUint32(data[48:52], uint32(len(saved.Runtime)))
	data = append(data, saved.Session...)
	data = append(data, saved.Runtime...)
	copy(data[56:88], checkpointDigest(data))
	return data, nil
}

func checkpointDigest(data []byte) []byte {
	hash := sha256.New()
	_, _ = hash.Write(data[:56])
	_, _ = hash.Write(data[checkpointHeaderSize:])
	return hash.Sum(nil)
}

// DecodeCheckpoint checks the expected archive before allocating section
// copies. Returned sections do not alias the input buffer.
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
	variant := binary.LittleEndian.Uint16(data[10:12])
	if binary.LittleEndian.Uint16(data[8:10]) != checkpointVersion || !checkpointVariantSupported(variant) {
		return Checkpoint{}, ErrCheckpointVersion
	}
	if len(data) > CheckpointLimit {
		return Checkpoint{}, ErrCheckpointDamaged
	}
	sessionSize := uint64(binary.LittleEndian.Uint32(data[44:48]))
	runtimeSize := uint64(binary.LittleEndian.Uint32(data[48:52]))
	if binary.LittleEndian.Uint32(data[52:56]) != 0 || sessionSize > checkpointSessionLimit || runtimeSize > checkpointRuntimeLimit ||
		sessionSize+runtimeSize != uint64(len(data)-checkpointHeaderSize) || !bytes.Equal(data[56:88], checkpointDigest(data)) {
		return Checkpoint{}, ErrCheckpointDamaged
	}
	if !bytes.Equal(data[12:44], identity[:]) {
		return Checkpoint{}, ErrCheckpointIdentity
	}
	runtimeStart := checkpointHeaderSize + int(sessionSize)
	return Checkpoint{Identity: identity, Variant: variant, Session: bytes.Clone(data[checkpointHeaderSize:runtimeStart]), Runtime: bytes.Clone(data[runtimeStart:])}, nil
}
