package skt

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/movingwoo/wfeature/internal/backend"
)

// storeSave remembers exactly the writes already issued by the guest. A failed
// write may outlive its file or RMS handle (including deletion and index writes),
// so the retry belongs to the runtime, rather than to its current open handles.
func (runtime *Runtime) storeSave(key string, data []byte) error {
	_, err := runtime.storeSaveVersion(key, data)
	return err
}

func (runtime *Runtime) storeSaveVersion(key string, data []byte) (uint64, error) {
	runtime.savePendingMu.Lock()
	defer runtime.savePendingMu.Unlock()
	if runtime.saveVersions == nil {
		runtime.saveVersions = make(map[string]uint64)
	}
	runtime.saveVersions[key]++
	version := runtime.saveVersions[key]
	store := runtime.saveStoreBoundary()
	if store == nil {
		return version, nil
	}
	err := store.StoreSave(key, data)
	if err == nil {
		delete(runtime.pendingSaves, key)
		return version, nil
	}
	if runtime.pendingSaves == nil {
		runtime.pendingSaves = make(map[string][]byte)
	}
	runtime.pendingSaves[key] = slices.Clone(data)
	return version, err
}

// The guest barrier holds every file still. Refuse all ambiguous writes before
// flushing any: a checkpoint must not choose between independently dirty
// handles or overwrite a newer write/deletion from another handle.
func (runtime *Runtime) checkCheckpointFiles(files []*xFileData) error {
	runtime.savePendingMu.Lock()
	defer runtime.savePendingMu.Unlock()
	dirty := make(map[string]*xFileData)
	for _, file := range files {
		if !file.dirty {
			continue
		}
		key, err := xFileKey(file.name)
		if err != nil {
			return fmt.Errorf("%w: SKT file path: %v", backend.ErrCheckpointSaveWrite, err)
		}
		if file.synced != runtime.saveVersions[key] || dirty[key] != nil && dirty[key] != file {
			return fmt.Errorf("%w: SKT file %q has conflicting pending writes", backend.ErrCheckpointSaveWrite, file.name)
		}
		dirty[key] = file
	}
	return nil
}

// flushCheckpointSaves requires the guest barrier. Captured file payloads carry
// only provenance/cursor, so their dirty buffers must reach the live store first.
func (runtime *Runtime) flushCheckpointSaves(ctx context.Context, files []*xFileData) (bool, error) {
	if err := runtime.checkCheckpointFiles(files); err != nil {
		return false, err
	}
	wrote := false
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return wrote, err
		}
		if file.dirty {
			wrote = true
			runtime.persistXFile(file)
		}
	}
	runtime.savePendingMu.Lock()
	defer runtime.savePendingMu.Unlock()
	keys := make([]string, 0, len(runtime.pendingSaves))
	for key := range runtime.pendingSaves {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return wrote, err
		}
		wrote = true
		if err := runtime.saveStoreBoundary().StoreSave(key, runtime.pendingSaves[key]); err != nil {
			return wrote, fmt.Errorf("%w: SKT pending write: %v", backend.ErrCheckpointSaveWrite, err)
		}
		delete(runtime.pendingSaves, key)
	}
	return wrote, nil
}

// rebuildCheckpointSaves fills host-owned caches from the current ordinary
// saves. It performs reads only, including for closed-but-reachable handles.
func (heap *checkpointHeap) rebuildCheckpointSaves(live backend.SaveStore) error {
	return heap.rebuildCheckpointSavesWithin(live, 128<<20)
}

func (heap *checkpointHeap) rebuildCheckpointSavesWithin(live backend.SaveStore, limit int64) error {
	runtime := heap.runtime
	reader := backend.NewRebuildReader(live, limit)
	runtime.AttachSaveStore(reader)
	defer runtime.AttachSaveStore(nil)
	fail := func(err error) error {
		return fmt.Errorf("%w: SKT save cache: %v", backend.ErrCheckpointSaveRead, err)
	}
	remaining := limit
	charge := func(size int64) error {
		if size < 0 || size > remaining {
			return fail(fmt.Errorf("reconstructed save caches exceed %d bytes", limit))
		}
		remaining -= size
		return nil
	}
	archives := make(map[string]map[string][]byte)
	for _, file := range heap.files {
		file.data, file.dirty, file.synced = nil, false, 0
		if file.name == "" {
			continue
		}
		name := file.name
		if file.archiveName != "" {
			name = file.archiveName
		}
		data, found, err := runtime.xFileContents(name)
		if err != nil {
			return fail(err)
		}
		if file.archiveName != "" && found {
			key, _ := xFileKey(name)
			entries, ok := archives[key]
			if !ok {
				limits := defaultArchiveLimits
				limits.entry = min(limits.entry, uint64(remaining))
				limits.total = uint64(remaining)
				entries, err = readJARWithin(data, limits)
				if err != nil {
					return fail(err)
				}
				for _, entry := range entries {
					if err = charge(int64(len(entry)) + 64); err != nil {
						return err
					}
				}
				archives[key] = entries
			}
			data, found = entries[file.archiveEntry]
		}
		// Removal leaves the execution handle open over an empty current file;
		// restoring execution never recreates the older bytes from a slot.
		if found {
			if err = charge(int64(len(data))); err != nil {
				return err
			}
			file.data = slices.Clone(data)
		}
	}
	if state := runtime.rmsState; state != nil {
		if heap.rmsCaches == nil {
			heap.rmsCaches = maps.Clone(state.stores)
		}
		state.stores = maps.Clone(heap.rmsCaches)
		state.names, state.packaged, state.unwritten, state.loaded = nil, nil, nil, false
		if err := runtime.loadIndex(state); err != nil {
			return fail(err)
		}
		for _, store := range heap.stores {
			store.records = nil
			key, err := recordStoreKey(store.name)
			if err != nil {
				return fail(err)
			}
			data, found, err := reader.ReadSave(key)
			if err != nil {
				return fail(err)
			}
			if found {
				if len(data) < 4 {
					return fail(fmt.Errorf("record cache header is truncated"))
				}
				count := binary.LittleEndian.Uint32(data)
				if count > backend.MaxSaveRecords {
					return fail(fmt.Errorf("record cache count exceeds limit"))
				}
				if err = charge(int64(len(data)) + int64(count)*24); err != nil {
					return err
				}
				records, err := backend.DecodeSaveRecords(data)
				if err != nil {
					return fail(err)
				}
				store.records = records
			} else if carried, ok := state.packaged[store.name]; ok {
				if err = charge(int64(len(carried.records)) * 24); err != nil {
					return err
				}
				for _, record := range carried.records {
					if err = charge(int64(len(record))); err != nil {
						return err
					}
					store.records = append(store.records, slices.Clone(record))
				}
			}
			// A cache must not shadow a store removed since the checkpoint. A
			// still-open guest object remains valid, but reopening uses the index.
			if !state.contains(store.name) {
				delete(state.stores, store.name)
			}
		}
	}
	if err := reader.Err(); err != nil {
		return fail(err)
	}
	return nil
}
