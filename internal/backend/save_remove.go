package backend

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// EmptySaveRemover is a store that can take away an entry that holds nothing.
//
// It is the one removal a store makes. A guest's own removals are a platform's
// removal list, which keeps them in an entry of its own; this is for an entry
// that a platform has found holds nothing anyone reads, such as the empty files
// an earlier build left wherever a title opened a missing name for writing.
// Nothing with a byte in it is ever removed this way.
type EmptySaveRemover interface {
	RemoveEmptySave(name string) (bool, error)
}

// RemoveEmptySave removes name's entry when it is there and holds nothing, and
// reports whether it did. A store that cannot remove one, and an entry that
// holds anything or is not there, are left as they are, and answer false
// without an error.
func RemoveEmptySave(store SaveStore, name string) (bool, error) {
	if remover, ok := store.(EmptySaveRemover); ok {
		return remover.RemoveEmptySave(name)
	}
	return false, nil
}

// RemoveEmptySave removes an empty entry's file.
//
// Only a key with one name on disk is removed: one that the encoding leaves as
// it is, in a directory whose names are settled. Such a key is read from that
// one path and from nowhere else (see locateSave), so taking the file away
// leaves the key with no entry, rather than handing a read to another spelling
// of it that an earlier build left. A key the encoding changes, and a directory
// with earlier names still in it or a conversion under way, are left alone.
func (store *DirectorySaveStore) RemoveEmptySave(name string) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("save store has no root")
	}
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return false, err
	}
	if encodeSaveKey(key) != key || !saveNamesFit(key) {
		return false, nil
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return false, err
	}
	defer unlock()
	if err := store.recoverSaveReplacement(); err != nil {
		return false, err
	}
	names, err := store.settleSaveNames()
	if err != nil {
		return false, err
	}
	if names.refusesWrites() {
		return false, nil
	}
	path := filepath.Join(store.root, filepath.FromSlash(key))
	if !savePathFits(path) {
		return false, nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	syncDirectory(filepath.Dir(path))
	return true, nil
}

// RemoveEmptySave removes an empty entry.
func (store *MemorySaveStore) RemoveEmptySave(name string) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory save store is nil")
	}
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if data, present := store.entries[key]; !present || len(data) != 0 {
		return false, nil
	}
	delete(store.entries, key)
	return true, nil
}
