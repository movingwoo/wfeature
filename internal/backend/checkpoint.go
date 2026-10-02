package backend

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	checkpointMagic        = "WFSTATE\x00"
	checkpointVersion      = 1
	checkpointHeaderSize   = 88
	checkpointSessionLimit = 64 << 10
	checkpointRuntimeLimit = 128 << 20
	// CheckpointLimit bounds an entire execution checkpoint before a Host reads
	// or accepts an uploaded file. Ordinary save backups have their own format.
	CheckpointLimit = checkpointHeaderSize + checkpointSessionLimit + checkpointRuntimeLimit + savePackHeaderSize + savePackLimit

	CheckpointKTFJava   uint16 = 1
	CheckpointKTFModule uint16 = 2
	CheckpointKTFNative uint16 = 3
)

var (
	ErrNotCheckpoint      = errors.New("backend: this is not an execution checkpoint")
	ErrCheckpointVersion  = errors.New("backend: this checkpoint version or execution variant is unsupported")
	ErrCheckpointDamaged  = errors.New("backend: this checkpoint is damaged or exceeds its limits")
	ErrCheckpointIdentity = errors.New("backend: this checkpoint belongs to a different archive")
)

// Checkpoint joins a shared session record, a platform execution record and a
// complete writable save generation. State codecs validate their own schemas;
// this envelope validates lengths, archive identity and transfer integrity.
// A checksum is not authentication. Every inner record remains untrusted.
type Checkpoint struct {
	Identity [32]byte
	Variant  uint16
	Session  []byte
	Runtime  []byte
	Saves    []SaveEntry
}

func checkpointVariantSupported(variant uint16) bool {
	return variant >= CheckpointKTFJava && variant <= CheckpointKTFNative
}

// EncodeCheckpoint writes a profile-independent, little-endian version 1
// envelope: magic(8), version(2), variant(2), archive SHA-256(32), three section
// lengths(4 each), SHA-256(32), then session/runtime/save-pack sections. The
// digest covers the first 56 header bytes and all section bytes.
func EncodeCheckpoint(saved Checkpoint) ([]byte, error) {
	if !checkpointVariantSupported(saved.Variant) {
		return nil, ErrCheckpointVersion
	}
	if len(saved.Session) > checkpointSessionLimit || len(saved.Runtime) > checkpointRuntimeLimit {
		return nil, ErrCheckpointDamaged
	}
	entries, err := validateSnapshotSaves(saved.Saves)
	if err != nil {
		return nil, err
	}
	pack, err := EncodeSavePack(SavePack{Identity: saved.Identity, Entries: entries})
	if err != nil {
		return nil, err
	}
	data := make([]byte, checkpointHeaderSize, checkpointHeaderSize+len(saved.Session)+len(saved.Runtime)+len(pack))
	copy(data, checkpointMagic)
	binary.LittleEndian.PutUint16(data[8:10], checkpointVersion)
	binary.LittleEndian.PutUint16(data[10:12], saved.Variant)
	copy(data[12:44], saved.Identity[:])
	binary.LittleEndian.PutUint32(data[44:48], uint32(len(saved.Session)))
	binary.LittleEndian.PutUint32(data[48:52], uint32(len(saved.Runtime)))
	binary.LittleEndian.PutUint32(data[52:56], uint32(len(pack)))
	data = append(data, saved.Session...)
	data = append(data, saved.Runtime...)
	data = append(data, pack...)
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
// copies. Returned sections and save files do not alias the input buffer.
func DecodeCheckpoint(data []byte, identity [32]byte) (Checkpoint, error) {
	if len(data) < 8 || string(data[:8]) != checkpointMagic {
		return Checkpoint{}, ErrNotCheckpoint
	}
	if len(data) < checkpointHeaderSize || len(data) > CheckpointLimit {
		return Checkpoint{}, ErrCheckpointDamaged
	}
	variant := binary.LittleEndian.Uint16(data[10:12])
	if binary.LittleEndian.Uint16(data[8:10]) != checkpointVersion || !checkpointVariantSupported(variant) {
		return Checkpoint{}, ErrCheckpointVersion
	}
	sessionSize := uint64(binary.LittleEndian.Uint32(data[44:48]))
	runtimeSize := uint64(binary.LittleEndian.Uint32(data[48:52]))
	saveSize := uint64(binary.LittleEndian.Uint32(data[52:56]))
	if sessionSize > checkpointSessionLimit || runtimeSize > checkpointRuntimeLimit || saveSize > savePackHeaderSize+savePackLimit || sessionSize+runtimeSize+saveSize != uint64(len(data)-checkpointHeaderSize) || !bytes.Equal(data[56:88], checkpointDigest(data)) {
		return Checkpoint{}, ErrCheckpointDamaged
	}
	if !bytes.Equal(data[12:44], identity[:]) {
		return Checkpoint{}, ErrCheckpointIdentity
	}
	runtimeStart := checkpointHeaderSize + int(sessionSize)
	saveStart := runtimeStart + int(runtimeSize)
	packData := data[saveStart:]
	// The ordinary save-pack decoder validates keys and checksums. Bound its
	// entry cardinality before it allocates records, including empty files.
	if err := checkpointSaveCount(packData); err != nil {
		return Checkpoint{}, err
	}
	pack, err := DecodeSavePack(packData)
	if err != nil || pack.Identity != identity {
		return Checkpoint{}, ErrCheckpointDamaged
	}
	entries, err := validateSnapshotSaves(pack.Entries)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("%w: %v", ErrCheckpointDamaged, err)
	}
	return Checkpoint{Identity: identity, Variant: variant, Session: bytes.Clone(data[checkpointHeaderSize:runtimeStart]), Runtime: bytes.Clone(data[runtimeStart:saveStart]), Saves: entries}, nil
}

func checkpointSaveCount(data []byte) error {
	if len(data) < savePackHeaderSize {
		return ErrCheckpointDamaged
	}
	for offset, count := savePackHeaderSize, 0; offset < len(data); count++ {
		if count >= maxSnapshotSaveEntries || len(data)-offset < 2 {
			return ErrCheckpointDamaged
		}
		keySize := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if keySize > len(data)-offset || len(data)-offset-keySize < 4 {
			return ErrCheckpointDamaged
		}
		offset += keySize
		fileSize := uint64(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4
		if fileSize > uint64(len(data)-offset) {
			return ErrCheckpointDamaged
		}
		offset += int(fileSize)
	}
	return nil
}
