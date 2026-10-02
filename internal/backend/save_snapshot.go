package backend

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
)

const maxSnapshotSaveEntries = 65536

// SaveSnapshotStore exposes a complete writable generation. Callers must hold
// their session's admission barrier while capturing or replacing that generation.
type SaveSnapshotStore interface {
	SaveStore
	SnapshotSaves() ([]SaveEntry, error)
	ReplaceSaves([]SaveEntry) error
}

func validateSnapshotSaves(entries []SaveEntry) ([]SaveEntry, error) {
	if len(entries) > maxSnapshotSaveEntries {
		return nil, fmt.Errorf("save snapshot entry count exceeds limit")
	}
	ordered := slices.Clone(entries)
	slices.SortFunc(ordered, func(a, b SaveEntry) int { return strings.Compare(a.Key, b.Key) })
	var size uint64
	files := make(map[string]bool, len(entries))
	for _, entry := range ordered {
		key, err := NormalizeSaveKey(entry.Key)
		if err != nil || key != entry.Key || files[key] {
			return nil, fmt.Errorf("save snapshot has invalid, duplicate or conflicting keys")
		}
		for index := 0; index < len(key); index++ {
			if key[index] == '/' && files[key[:index]] {
				return nil, fmt.Errorf("save snapshot key conflicts with a parent file")
			}
		}
		files[key] = true
		size += uint64(len(key)) + uint64(len(entry.Data)) + 6
		if size > savePackLimit {
			return nil, fmt.Errorf("save snapshot data exceeds %d bytes", savePackLimit)
		}
	}
	return ordered, nil
}

// MemorySaveStore is an isolated generation used while validating a restored
// session. Reads, writes, batches and snapshots own their bytes. It uses the same
// key and file/directory collision rules as a directory store.
type MemorySaveStore struct {
	mu      sync.Mutex
	entries map[string][]byte
}

func NewMemorySaveStore(entries []SaveEntry) (*MemorySaveStore, error) {
	store := &MemorySaveStore{}
	if err := store.ReplaceSaves(entries); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *MemorySaveStore) LoadSave(name string) ([]byte, bool) {
	data, present, _ := store.ReadSave(name)
	return data, present
}

func (store *MemorySaveStore) ReadSave(name string) ([]byte, bool, error) {
	if store == nil {
		return nil, false, fmt.Errorf("memory save store is nil")
	}
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return nil, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	data, present := store.entries[key]
	if !present {
		for child := range store.entries {
			if strings.HasPrefix(child, key+"/") {
				return nil, false, fmt.Errorf("save key names a directory")
			}
		}
	}
	return bytes.Clone(data), present, nil
}

func (store *MemorySaveStore) StoreSave(name string, data []byte) error {
	return store.StoreSaves(map[string][]byte{name: data})
}

func (store *MemorySaveStore) StoreSaves(updates map[string][]byte) error {
	if store == nil {
		return fmt.Errorf("memory save store is nil")
	}
	if len(updates) > maxSnapshotSaveEntries {
		return fmt.Errorf("save snapshot update count exceeds limit")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entries := make([]SaveEntry, 0, len(store.entries)+len(updates))
	normalized := make(map[string][]byte, len(updates))
	for name, data := range updates {
		key, err := NormalizeSaveKey(name)
		if err != nil {
			return err
		}
		if _, exists := normalized[key]; exists {
			return fmt.Errorf("duplicate canonical save key")
		}
		normalized[key] = data
	}
	for key, data := range store.entries {
		if _, replaced := normalized[key]; !replaced {
			entries = append(entries, SaveEntry{Key: key, Data: data})
		}
	}
	for key, data := range normalized {
		entries = append(entries, SaveEntry{Key: key, Data: data})
	}
	ordered, err := validateSnapshotSaves(entries)
	if err != nil {
		return err
	}
	store.entries = cloneSaveEntries(ordered)
	return nil
}

func cloneSaveEntries(entries []SaveEntry) map[string][]byte {
	result := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		result[entry.Key] = bytes.Clone(entry.Data)
	}
	return result
}

func (store *MemorySaveStore) ReplaceSaves(entries []SaveEntry) error {
	if store == nil {
		return fmt.Errorf("memory save store is nil")
	}
	ordered, err := validateSnapshotSaves(entries)
	if err != nil {
		return err
	}
	replacement := cloneSaveEntries(ordered)
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries = replacement
	return nil
}

func (store *MemorySaveStore) SnapshotSaves() ([]SaveEntry, error) {
	if store == nil {
		return nil, fmt.Errorf("memory save store is nil")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entries := make([]SaveEntry, 0, len(store.entries))
	for key, data := range store.entries {
		entries = append(entries, SaveEntry{Key: key, Data: bytes.Clone(data)})
	}
	slices.SortFunc(entries, func(a, b SaveEntry) int { return strings.Compare(a.Key, b.Key) })
	return entries, nil
}

// readSnapshotSaves refuses unreadable, linked or noncanonical entries instead
// of silently omitting them from a whole-generation replacement. Reads are
// bounded before allocation; a growing file cannot evade the size limit.
func readSnapshotSaves(root string) ([]SaveEntry, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("save snapshot root is not a directory")
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries := []SaveEntry{}
	remaining := int64(savePackLimit)
	visited := 0
	err = fs.WalkDir(directory.FS(), ".", func(key string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > 2*maxSnapshotSaveEntries {
			return fmt.Errorf("save snapshot tree exceeds entry limit")
		}
		if key == "." {
			return nil
		}
		canonical, err := NormalizeSaveKey(key)
		if err != nil || key != canonical {
			return fmt.Errorf("save snapshot contains a noncanonical key")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() || len(entries) == maxSnapshotSaveEntries {
			return fmt.Errorf("save snapshot contains a nonregular file or too many files")
		}
		remaining -= int64(len(key) + 6)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() < 0 || info.Size() > remaining {
			return fmt.Errorf("save snapshot data exceeds limit")
		}
		file, err := directory.Open(key)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, remaining+1))
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		remaining -= int64(len(data))
		if remaining < 0 {
			return fmt.Errorf("save snapshot data exceeds limit")
		}
		entries = append(entries, SaveEntry{Key: key, Data: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return validateSnapshotSaves(entries)
}
