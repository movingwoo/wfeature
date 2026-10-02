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
// keys. One archive has one slot. Neither a guest save export nor replacing a
// save generation includes or removes it. Hosts retain their directory claim
// while capturing and writing a slot or while loading and adopting one.
func (store *DirectorySaveStore) checkpointDirectory(create bool) (string, bool, error) {
	paths, err := replacementPaths(store.root)
	if err != nil {
		return "", false, err
	}
	if _, err := existingSaveDirectory(paths.root); err != nil {
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

func checkpointSlotName(identity [32]byte) string { return fmt.Sprintf("%x.wfq", identity) }

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

// HasCheckpoint checks slot presence and file bounds without reading a large
// checkpoint. Load and detached restoration still validate the actual bytes.
func (store *DirectorySaveStore) HasCheckpoint(identity [32]byte) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("checkpoint store has no root")
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return false, err
	}
	defer unlock()
	if err := store.recoverSaveReplacement(); err != nil {
		return false, err
	}
	path, exists, err := store.checkpointDirectory(false)
	if err != nil || !exists {
		return false, err
	}
	directory, err := os.OpenRoot(path)
	if err != nil {
		return false, err
	}
	defer directory.Close()
	return checkpointSlotInfo(directory, identity)
}

// LoadCheckpoint bounds the read even if a file grows after Stat. The shared
// session checks identity, integrity and all state records before adoption.
func (store *DirectorySaveStore) LoadCheckpoint(identity [32]byte) ([]byte, bool, error) {
	if store == nil {
		return nil, false, fmt.Errorf("checkpoint store has no root")
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return nil, false, err
	}
	defer unlock()
	if err := store.recoverSaveReplacement(); err != nil {
		return nil, false, err
	}
	path, exists, err := store.checkpointDirectory(false)
	if err != nil || !exists {
		return nil, false, err
	}
	directory, err := os.OpenRoot(path)
	if err != nil {
		return nil, false, err
	}
	defer directory.Close()
	if exists, err := checkpointSlotInfo(directory, identity); err != nil || !exists {
		return nil, false, err
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
// the ordinary synced temporary-file write. It never writes guest save keys.
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
