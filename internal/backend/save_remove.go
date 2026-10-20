package backend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// EmptySaveRemover is a store that can take away an entry that holds nothing.
//
// It is the one removal a store makes. A guest's own removals are a platform's
// removal list, which keeps them in an entry of its own; this is for an entry
// that a platform has found holds nothing anyone reads, such as the empty files
// an earlier build left wherever a title opened a missing name for writing.
// Nothing with a byte in it is ever removed this way, and neither is an empty
// entry that stands in front of an older one of the same key.
type EmptySaveRemover interface {
	RemoveEmptySave(name string) (bool, error)
}

// ErrSaveRemovalDeferred is what a removal answers when the store takes no
// write for now — a folder whose earlier names could not be converted, or whose
// conversion is under way. Asked again once the folder takes writes, the
// removal goes ahead.
var ErrSaveRemovalDeferred = errors.New("backend: the save folder takes no removal until its names are settled")

// RemoveEmptySave removes name's entry when it is there and holds nothing, and
// reports whether it did. A store that cannot remove one, and an entry that
// holds anything or is not there, are left as they are, and answer false
// without an error; a store that cannot remove it yet answers
// ErrSaveRemovalDeferred.
func RemoveEmptySave(store SaveStore, name string) (bool, error) {
	if remover, ok := store.(EmptySaveRemover); ok {
		return remover.RemoveEmptySave(name)
	}
	return false, nil
}

// RemoveEmptySave removes an empty entry's files.
//
// A key may be on disk under more than its encoded name: under a name an
// earlier build or Win32 gave it, which a read finds when the encoded one is
// not there (see locateSave). So every name the key could be read from is
// looked at first, and the entry is removed only when each of them that is
// there is a file with nothing in it; then all of them go, and nothing is left
// to answer for the key. One that holds anything is an older entry the empty
// one is shadowing, and taking the empty one away would hand a read that older
// save, so then nothing is removed. A folder that takes no write — one whose
// earlier names could not be converted, or whose conversion is under way —
// takes no removal either, until it does, and nothing is removed under a name
// Win32 stored for an earlier build, which other keys may read too.
func (store *DirectorySaveStore) RemoveEmptySave(name string) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("save store has no root")
	}
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return false, err
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
		return false, ErrSaveRemovalDeferred
	}
	paths, removable, err := store.saveSpellings(key, names)
	if err != nil || !removable || len(paths) == 0 {
		return false, err
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() || info.Size() != 0 {
			return false, nil
		}
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return false, err
		}
		syncDirectory(filepath.Dir(path))
	}
	return true, nil
}

// saveSpellings lists every file one key's entry could be read from, as
// locateSave looks for it: in an encoded folder each name walkEarlierSaves
// visits, which findEarlierSave chooses among, and in a folder without the
// record the whole key encoded and the whole key spelled exactly, and not a
// mix of the two, which would name another key there. It answers false for a
// key that a name Win32 stored for an earlier build may answer: that file is
// shared by every key Win32 spelled the same and is never moved, so it is
// never taken away for one of them.
func (store *DirectorySaveStore) saveSpellings(key string, names saveNames) ([]string, bool, error) {
	segments := strings.Split(key, "/")
	if saveNamesWin32 {
		if _, found := store.win32Save(segments, names); found {
			return nil, false, nil
		}
	}
	var paths []string
	add := func(path string) {
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	if names.naming == namingEncoded {
		walkEarlierSaves(store.root, segments, func(path string, _ []string, _ bool, _ os.FileInfo) {
			add(path)
		})
		return paths, true, nil
	}
	encoded := filepath.Join(store.root, filepath.FromSlash(encodeSaveKey(key)))
	if present, err := saveNameExists(encoded); err != nil {
		return nil, false, err
	} else if present && !keyIsDirectory(encoded) {
		add(encoded)
	}
	exact, found, err := exactSavePath(store.root, segments)
	if err != nil {
		return nil, false, err
	}
	if found && !keyIsDirectory(exact) {
		add(exact)
	}
	return paths, true, nil
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
