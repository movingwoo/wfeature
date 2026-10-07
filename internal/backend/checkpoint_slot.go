package backend

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Checkpoint slots are Host files beside the owner directory, never SaveStore
// keys. One archive has one slot, and a guest save export does not include it.
// Hosts retain their directory claim while capturing and writing a slot or
// while loading and adopting one.
//
// The file name carries the envelope version. A slot written in an earlier
// format keeps its earlier name and is never written, renamed or removed here:
// it is reported as present, refused when it is loaded, and left for its owner.
// A new quick save is written beside it under the current name, so no build
// overwrites another format's slot.
//
// The owner directory may be a link to saves kept elsewhere. A slot is a file
// in the reserved directory beside the link, named after it, and a quick save
// or a quick load reaches the saves themselves only through ordinary reads and
// writes, which follow the link; nothing here renames or stages the owner
// directory. So a linked root is followed like any other. The reserved
// directories themselves must still be real directories.
func (store *DirectorySaveStore) checkpointDirectory(create bool) (string, bool, error) {
	paths, err := replacementPaths(store.root)
	if err != nil {
		return "", false, err
	}
	if _, err := ownerSaveDirectory(paths.root); err != nil {
		return "", false, err
	}
	if create {
		if err := prepareSaveDirectory(paths); err != nil {
			return "", false, err
		}
	}
	for _, directory := range []string{filepath.Dir(paths.directory), paths.directory} {
		if create {
			if err := os.Mkdir(directory, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
				return "", false, err
			}
		}
		exists, err := existingSaveDirectory(directory)
		if err != nil || !exists {
			return "", false, err
		}
	}
	return paths.directory, true, nil
}

func checkpointSlotName(identity [32]byte) string { return fmt.Sprintf("%x.v2.wfq", identity) }

// legacyCheckpointSlotName is the name envelope version 1 was stored under.
func legacyCheckpointSlotName(identity [32]byte) string { return fmt.Sprintf("%x.wfq", identity) }

func checkpointSlotInfo(directory *os.Root, identity [32]byte) (bool, error) {
	info, err := directory.Lstat(checkpointSlotName(identity))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() < checkpointHeaderSize || info.Size() > CheckpointLimit {
		return false, fmt.Errorf("checkpoint slot is not a regular file within size limits")
	}
	return true, nil
}

// legacyCheckpointSlotInfo reports a regular file under the earlier name. Its
// size is not judged and its bytes are not read: an earlier format had its own
// limits, and nothing here will decode it.
func legacyCheckpointSlotInfo(directory *os.Root, identity [32]byte) (bool, error) {
	info, err := directory.Lstat(legacyCheckpointSlotName(identity))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

// openCheckpointDirectory takes the tree lock, settles an interrupted
// replacement and opens the reserved directory if it exists. The caller closes
// the root and releases the lock through the returned function.
func (store *DirectorySaveStore) openCheckpointDirectory() (*os.Root, func(), error) {
	if store == nil {
		return nil, nil, fmt.Errorf("checkpoint store has no root")
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return nil, nil, err
	}
	if err := store.recoverSaveReplacement(); err != nil {
		unlock()
		return nil, nil, err
	}
	path, exists, err := store.checkpointDirectory(false)
	if err != nil || !exists {
		unlock()
		return nil, nil, err
	}
	directory, err := os.OpenRoot(path)
	if err != nil {
		unlock()
		return nil, nil, err
	}
	return directory, func() {
		_ = directory.Close()
		unlock()
	}, nil
}

// HasCheckpoint checks slot presence and file bounds without reading a large
// checkpoint. Load and detached restoration still validate the actual bytes.
// A slot in an earlier format counts as present, so that a Host offers the
// load and the person who asks for it is told why it is refused.
func (store *DirectorySaveStore) HasCheckpoint(identity [32]byte) (bool, error) {
	directory, done, err := store.openCheckpointDirectory()
	if err != nil || directory == nil {
		return false, err
	}
	defer done()
	if exists, err := checkpointSlotInfo(directory, identity); err != nil || exists {
		return exists, err
	}
	return legacyCheckpointSlotInfo(directory, identity)
}

// LegacyCheckpoint reports whether a slot in an earlier format is still beside
// the saves. It is only asked whether the file exists.
func (store *DirectorySaveStore) LegacyCheckpoint(identity [32]byte) (bool, error) {
	directory, done, err := store.openCheckpointDirectory()
	if err != nil || directory == nil {
		return false, err
	}
	defer done()
	return legacyCheckpointSlotInfo(directory, identity)
}

// LoadCheckpoint bounds the read even if a file grows after Stat. The shared
// session checks identity, integrity and all state records before adoption.
// With only an earlier-format slot it answers found with ErrCheckpointLegacy
// and reads nothing: the file is left exactly as it is.
func (store *DirectorySaveStore) LoadCheckpoint(identity [32]byte) ([]byte, bool, error) {
	directory, done, err := store.openCheckpointDirectory()
	if err != nil || directory == nil {
		return nil, false, err
	}
	defer done()
	exists, err := checkpointSlotInfo(directory, identity)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		legacy, err := legacyCheckpointSlotInfo(directory, identity)
		if err != nil || !legacy {
			return nil, false, err
		}
		return nil, true, fmt.Errorf("%w: %s was left in place", ErrCheckpointLegacy, legacyCheckpointSlotName(identity))
	}
	file, err := directory.Open(checkpointSlotName(identity))
	if err != nil {
		return nil, false, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, CheckpointLimit+1))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, false, err
	}
	if len(data) > CheckpointLimit {
		return nil, false, ErrCheckpointDamaged
	}
	return data, true, nil
}

// StoreCheckpoint checks the envelope before replacing the previous slot using
// the ordinary synced temporary-file write. It never writes guest save keys,
// and it never touches a slot stored under an earlier format's name.
func (store *DirectorySaveStore) StoreCheckpoint(identity [32]byte, data []byte) error {
	if store == nil {
		return fmt.Errorf("checkpoint store has no root")
	}
	if _, err := DecodeCheckpoint(data, identity); err != nil {
		return err
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return err
	}
	defer unlock()
	if err := store.recoverSaveReplacement(); err != nil {
		return err
	}
	path, _, err := store.checkpointDirectory(true)
	if err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Join(path, checkpointSlotName(identity)))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("checkpoint slot is not a regular file")
	}
	private := &DirectorySaveStore{root: path}
	if err := private.storeSave(checkpointSlotName(identity), data); err != nil {
		return err
	}
	syncDirectory(filepath.Dir(path))
	syncDirectory(filepath.Dir(filepath.Dir(path)))
	return nil
}
