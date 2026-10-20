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
// takes no removal either, until it does.
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
		return false, nil
	}
	paths, err := store.saveSpellings(key, names)
	if err != nil || len(paths) == 0 {
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
// locateSave looks for it. In an encoded folder that is, at each segment, its
// encoded name and the names an earlier build gave it (earlierSaveSpellings),
// each of which reads back as that segment and as no other. A folder without
// the record is read at the whole key encoded and at the whole key spelled
// exactly, and not at a mix of the two, which would name another key there.
// On Windows a name Win32 stored for an earlier build is one too.
func (store *DirectorySaveStore) saveSpellings(key string, names saveNames) ([]string, error) {
	segments := strings.Split(key, "/")
	var paths []string
	add := func(path string) {
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	var failure error
	switch names.naming {
	case namingEncoded:
		var search func(directory string, depth int)
		search = func(directory string, depth int) {
			segment := segments[depth]
			spellings := append([]string{encodeSaveName(segment)}, earlierSaveSpellings(segment)...)
			for index, spelling := range spellings {
				if saveNamesReserved(spelling) || saveNameLength(spelling) > maxSaveNameLength {
					continue
				}
				path := filepath.Join(directory, spelling)
				present, err := saveNameExists(path)
				if err == nil && present && index != 0 {
					present, err = spelledExactly(directory, spelling)
				}
				if err != nil {
					failure = errors.Join(failure, err)
					continue
				}
				if !present {
					continue
				}
				info, err := os.Lstat(path)
				if err != nil {
					failure = errors.Join(failure, err)
					continue
				}
				if depth == len(segments)-1 {
					if !info.IsDir() {
						add(path)
					}
				} else if info.IsDir() {
					search(path, depth+1)
				}
			}
		}
		search(store.root, 0)
	default:
		encoded := filepath.Join(store.root, filepath.FromSlash(encodeSaveKey(key)))
		if present, err := saveNameExists(encoded); err != nil {
			failure = errors.Join(failure, err)
		} else if present && !keyIsDirectory(encoded) {
			add(encoded)
		}
		exact, found, err := exactSavePath(store.root, segments)
		if err != nil {
			failure = errors.Join(failure, err)
		} else if found && !keyIsDirectory(exact) {
			add(exact)
		}
	}
	if saveNamesWin32 {
		if at, found := store.win32Save(segments, names); found {
			add(at)
		}
	}
	return paths, failure
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
