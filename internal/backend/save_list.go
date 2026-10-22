package backend

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrSavesNotListed is what a store standing in front of another answers when
// the store behind it cannot list. It is the caller's cue to answer from the
// names it already knows, not a failed save.
var ErrSavesNotListed = errors.New("backend: the save store cannot list its entries")

// SaveLister is a store that can say which keys it holds.
//
// Every other question a title asks of its storage names a key. "What is in
// this directory" is the one answered from what is there, and after a restart
// the answer is entries nothing in the session has named yet: a title that
// lists its save folder to find its slots would otherwise be told the folder
// is empty, and start a new game over the player's saves.
type SaveLister interface {
	// ListSaves answers the keys below directory, or every key for "", in
	// key order.
	ListSaves(directory string) ([]string, error)
}

// ListSaves asks a store for the keys below directory, and reports whether
// the store could list at all. A store that cannot is not an error.
func ListSaves(store SaveStore, directory string) ([]string, bool, error) {
	lister, ok := store.(SaveLister)
	if !ok {
		return nil, false, nil
	}
	keys, err := lister.ListSaves(directory)
	if errors.Is(err, ErrSavesNotListed) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	return keys, true, nil
}

// SaveListPrefix is the prefix every key listed for directory begins with: ""
// for the whole store, which "", "/" and "./" all name, and the normalized
// directory and a slash otherwise. A store in front of another lists the keys
// it answers itself with it.
func SaveListPrefix(directory string) (string, error) {
	root := true
	for _, part := range strings.Split(directory, "/") {
		if part != "" && part != "." {
			root = false
			break
		}
	}
	if root {
		return "", nil
	}
	key, err := NormalizeSaveKey(directory)
	if err != nil {
		return "", err
	}
	return key + "/", nil
}

// ListSaves reads the keys back from the files under the root, the way a
// backup does, so it answers what this build and every earlier one stored.
func (store *DirectorySaveStore) ListSaves(directory string) ([]string, error) {
	if store == nil {
		return nil, fmt.Errorf("save store has no root")
	}
	prefix, err := SaveListPrefix(directory)
	if err != nil {
		return nil, err
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := store.recoverSaveReplacement(); err != nil {
		return nil, err
	}
	names, err := store.settleSaveNames()
	if err != nil {
		return nil, err
	}
	if names.naming == namingConverting {
		return nil, refuseSaveNamesListing(store.root, names)
	}
	choice, err := collectSaveTree(store.root, names)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(choice))
	for _, key := range choice.keys() {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// ListSaves answers the keys of the entries held in memory.
func (store *MemorySaveStore) ListSaves(directory string) ([]string, error) {
	if store == nil {
		return nil, fmt.Errorf("memory save store is nil")
	}
	prefix, err := SaveListPrefix(directory)
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	keys := make([]string, 0, len(store.entries))
	for key := range store.entries {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// ListSaves answers that nothing is stored and counts the call, as a read
// does.
func (store *DetachedSaveStore) ListSaves(directory string) ([]string, error) {
	store.note("list " + detachedName(directory))
	return nil, nil
}
