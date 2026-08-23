package backend

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// SaveStore persists guest save data across sessions. Keys are
// slash-separated printable names scoped by the platform that owns them —
// KTF uses "db/<name>", "jdb/<name>" and "fs/<path>", MIDP RMS uses
// "rms/<name>" — so one owner directory holds every kind of save a title
// writes without the platforms colliding.
type SaveStore interface {
	LoadSave(name string) ([]byte, bool)
	StoreSave(name string, data []byte) error
}

// DirectorySaveStore persists save entries as files under one root
// directory, one file per key with key slashes as subdirectories. It backs
// the native CLI Host; the browser Host supplies its own store over the same
// layout so both address the same entries.
type DirectorySaveStore struct {
	root string
}

// NewDirectorySaveStore roots a directory-backed save store. The directory
// is created on the first successful store.
func NewDirectorySaveStore(root string) *DirectorySaveStore {
	return &DirectorySaveStore{root: root}
}

// NormalizeSaveKey validates a save key and reduces it to the canonical form
// every Host stores under. Keys are slash-separated printable names. Empty and
// "." components are dropped rather than rejected — guests name databases the
// way they name files, and a guest can open "./OptionSave", which would
// otherwise never persist — while traversal components stay an error. Both
// Hosts normalize on the way in so a browser session and the CLI address the
// same entry, and so does the server save API.
func NormalizeSaveKey(name string) (string, error) {
	if name == "" || len(name) > 512 {
		return "", fmt.Errorf("save key %q length is invalid", name)
	}
	parts := make([]string, 0, strings.Count(name, "/")+1)
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", fmt.Errorf("save key %q has an invalid component", name)
		}
		if strings.ContainsAny(part, "\\\x00") || strings.ContainsRune(part, os.PathSeparator) {
			return "", fmt.Errorf("save key %q has an invalid character", name)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("save key %q has no name", name)
	}
	return strings.Join(parts, "/"), nil
}

// savePath resolves a save key inside the store root.
func (store *DirectorySaveStore) savePath(name string) (string, error) {
	if store == nil || store.root == "" {
		return "", fmt.Errorf("save store has no root")
	}
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{store.root}, strings.Split(key, "/")...)...), nil
}

// LoadSave reads one persisted entry; a missing or unreadable file reports
// absence.
func (store *DirectorySaveStore) LoadSave(name string) ([]byte, bool) {
	path, err := store.savePath(name)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// StoreSave writes one entry, creating the key's directories as needed.
//
// The write replaces the file rather than overwriting it: the bytes go to a
// temporary file beside the target, are flushed to the disk, and then take the
// name in one step. A save here is always written whole — the guest hands over
// the entire database every time it commits — so an overwrite that stops
// halfway, whether the machine lost power or the disk filled, leaves a file
// that is the front of the new save and the back of the old one. Nothing reads
// that as damaged: it is the game's own format, so the game loads it and the
// player finds their progress replaced by something that never existed. The
// rename is the one operation a file system will not do halfway, so what
// survives a failure is either the previous save or the new one.
func (store *DirectorySaveStore) StoreSave(name string, data []byte) error {
	path, err := store.savePath(name)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	// The temporary file is in the target's own directory, because a rename
	// is only atomic within one file system and a temporary directory
	// elsewhere may be on another one. The name is dotted so a listing of the
	// save tree passes over it, and carries the entry's own name so a leftover
	// from a killed process says which save it was.
	temporary, err := os.CreateTemp(directory, "."+temporaryPrefix(filepath.Base(path))+".*")
	if err != nil {
		return err
	}
	written := temporary.Name()
	// Every failure past this point removes the temporary file. The deferred
	// close is the same: closing twice reports an error that is not one.
	committed := false
	defer func() {
		if !committed {
			temporary.Close()
			os.Remove(written)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	// The flush is what makes the rename mean anything: without it the name
	// can arrive on the disk before the bytes it points at, which is the
	// truncated save this is here to prevent.
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// CreateTemp makes the file readable by its owner alone; a save is the
	// user's data beside the rest of the tree, so it keeps the tree's mode.
	if err := os.Chmod(written, 0o644); err != nil {
		return err
	}
	if err := os.Rename(written, path); err != nil {
		return err
	}
	committed = true
	syncDirectory(directory)
	return nil
}

// temporaryPrefix shortens an entry's name for the temporary file that will
// become it. A key may be longer than a file system's name limit on its own,
// and the decoration would then be what pushed a save that used to write over
// it. The cut is on a rune boundary: a name is bytes to the file system, but
// macOS refuses one that is not valid UTF-8.
func temporaryPrefix(name string) string {
	const limit = 64
	if len(name) <= limit {
		return name
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(name[cut]) {
		cut--
	}
	return name[:cut]
}

// syncDirectory flushes the directory entry the rename created, which is what
// makes the new name itself survive a power loss rather than only the bytes
// behind it. It is advisory: Windows has no directory handle to sync and
// answers with an error, and a file system that refuses says nothing about
// whether the save is safe, so a failure here is not the caller's to handle.
func syncDirectory(path string) {
	directory, err := os.Open(path)
	if err != nil {
		return
	}
	defer directory.Close()
	_ = directory.Sync()
}

// SaveRecordTombstone marks a deleted slot in an encoded record list. Record
// ids are handed to the guest and must not shift when a record is removed, so
// a deleted slot keeps its place instead of being compacted away.
const SaveRecordTombstone = 0xffffffff

// MaxSaveRecords bounds a decoded record list so a corrupt or hostile save
// cannot make the runtime allocate without limit.
const MaxSaveRecords = 1 << 20

// EncodeSaveRecords serializes a record list as a count followed by
// length-prefixed entries; nil records keep their slot with a tombstone.
func EncodeSaveRecords(records [][]byte) []byte {
	size := 4
	for _, record := range records {
		size += 4 + len(record)
	}
	encoded := make([]byte, 4, size)
	binary.LittleEndian.PutUint32(encoded, uint32(len(records)))
	for _, record := range records {
		var length [4]byte
		if record == nil {
			binary.LittleEndian.PutUint32(length[:], SaveRecordTombstone)
			encoded = append(encoded, length[:]...)
			continue
		}
		binary.LittleEndian.PutUint32(length[:], uint32(len(record)))
		encoded = append(encoded, length[:]...)
		encoded = append(encoded, record...)
	}
	return encoded
}

// DecodeSaveRecords reverses EncodeSaveRecords, rejecting truncated input.
func DecodeSaveRecords(encoded []byte) ([][]byte, error) {
	if len(encoded) < 4 {
		return nil, fmt.Errorf("save records header is truncated")
	}
	count := binary.LittleEndian.Uint32(encoded)
	if count > MaxSaveRecords {
		return nil, fmt.Errorf("save records count %d exceeds limit", count)
	}
	records := make([][]byte, 0, count)
	offset := 4
	for index := uint32(0); index < count; index++ {
		if offset+4 > len(encoded) {
			return nil, fmt.Errorf("save record %d length is truncated", index)
		}
		length := binary.LittleEndian.Uint32(encoded[offset:])
		offset += 4
		if length == SaveRecordTombstone {
			records = append(records, nil)
			continue
		}
		if uint64(offset)+uint64(length) > uint64(len(encoded)) {
			return nil, fmt.Errorf("save record %d data is truncated", index)
		}
		records = append(records, append([]byte(nil), encoded[offset:offset+int(length)]...))
		offset += int(length)
	}
	return records, nil
}
