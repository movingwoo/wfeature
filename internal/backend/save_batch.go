package backend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// SaveBatchStore stages all entries before replacing any live save. A failed
// operation leaves the previous entries in place; this is not a power-loss
// transaction across multiple filesystem renames.
type SaveBatchStore interface{ StoreSaves(map[string][]byte) error }

// StoreSaves preserves the single-key contract for older Host stores. A Host
// without batch support must refuse multi-key changes before writing anything.
func StoreSaves(store SaveStore, entries map[string][]byte) error {
	if store == nil || len(entries) == 0 {
		return nil
	}
	if len(entries) == 1 {
		for name, data := range entries {
			return store.StoreSave(name, data)
		}
	}
	if batch, ok := store.(SaveBatchStore); ok {
		return batch.StoreSaves(entries)
	}
	return fmt.Errorf("save store does not support multi-entry writes")
}

func (store *DirectorySaveStore) StoreSaves(entries map[string][]byte) error {
	if store == nil || store.root == "" {
		return fmt.Errorf("save store has no root")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.storeSaves(entries, os.Rename)
}

func (store *DirectorySaveStore) storeSaves(entries map[string][]byte, rename func(string, string) error) error {
	if len(entries) == 0 {
		return nil
	}
	keys := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for name := range entries {
		key, err := NormalizeSaveKey(name)
		if err != nil {
			return err
		}
		if seen[key] {
			return fmt.Errorf("duplicate canonical save key %q", key)
		}
		seen[key] = true
		keys = append(keys, name)
	}
	sort.Strings(keys)
	if err := os.MkdirAll(store.root, 0755); err != nil {
		return err
	}
	directory, err := os.MkdirTemp(store.root, ".batch-")
	if err != nil {
		return err
	}
	keepRecovery := false
	defer func() {
		if !keepRecovery {
			_ = os.RemoveAll(directory)
		}
	}()
	staged := NewDirectorySaveStore(directory)
	targets := make([]string, len(keys))
	existed := make([]bool, len(keys))
	for i, name := range keys {
		target, err := store.savePath(name)
		if err != nil {
			return err
		}
		targets[i] = target
		old, present, err := store.readSave(name)
		if err != nil {
			return err
		}
		existed[i] = present
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := staged.StoreSave(fmt.Sprintf("new-%d", i), entries[name]); err != nil {
			return err
		}
		if present {
			if err := staged.StoreSave(fmt.Sprintf("old-%d", i), old); err != nil {
				return err
			}
		}
	}
	for i, target := range targets {
		if err := rename(filepath.Join(directory, fmt.Sprintf("new-%d", i)), target); err != nil {
			result := err
			for j := i - 1; j >= 0; j-- {
				var restore error
				if existed[j] {
					restore = rename(filepath.Join(directory, fmt.Sprintf("old-%d", j)), targets[j])
				} else {
					restore = os.Remove(targets[j])
				}
				if restore != nil {
					keepRecovery = true
					result = errors.Join(result, fmt.Errorf("restore %s: %w", keys[j], restore))
				}
			}
			if keepRecovery {
				result = errors.Join(result, fmt.Errorf("save recovery files retained in %s", directory))
			}
			return result
		}
	}
	for _, target := range targets {
		syncDirectory(filepath.Dir(target))
	}
	return nil
}
