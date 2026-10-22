package skt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
)

// storeSave remembers exactly the writes already issued by the guest. A failed
// write may outlive its RMS handle (including deletion and index writes), so
// the retry belongs to the runtime, rather than to its current open handles.
// A file keeps its own retry: it stays dirty (skvm_file.go).
func (runtime *Runtime) storeSave(key string, data []byte) error {
	return runtime.storeSaveEncoded(key, func() []byte { return data })
}

// storeSaveEncoded is storeSave for bytes built from state other guest threads
// change. The encoding is taken inside the write's own turn, so the writes of
// one key reach the store in the order their contents were read: encoding
// first and writing after let an older encoding land behind a newer one, and
// the store kept it until the next change.
func (runtime *Runtime) storeSaveEncoded(key string, encode func() []byte) error {
	runtime.savePendingMu.Lock()
	defer runtime.savePendingMu.Unlock()
	store := runtime.saveStoreBoundary()
	if store == nil {
		return nil
	}
	data := encode()
	err := store.StoreSave(key, data)
	if err == nil {
		delete(runtime.pendingSaves, key)
		return nil
	}
	if runtime.pendingSaves == nil {
		runtime.pendingSaves = make(map[string][]byte)
	}
	runtime.pendingSaves[key] = slices.Clone(data)
	return err
}

// flushCheckpointSaves requires the guest barrier. A checkpoint holds no save
// bytes, so what the title has written and the store does not hold yet — a
// dirty file, a write a refused store left pending — is stored first. Every
// handle on a path shares the path's one file, so storing it stores the
// newest state of the path, and there is no older buffer to put over a newer
// write or removal. A refusal refuses the step and leaves the rest pending in
// the running session.
func (runtime *Runtime) flushCheckpointSaves(ctx context.Context) (bool, error) {
	wrote, err := runtime.flushDirtyFiles(ctx)
	if err != nil {
		return wrote, err
	}
	runtime.savePendingMu.Lock()
	defer runtime.savePendingMu.Unlock()
	for _, key := range slices.Sorted(maps.Keys(runtime.pendingSaves)) {
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

// flushDirtyFiles stores every file whose state the store does not hold, in
// key order, and stops at the first refusal.
func (runtime *Runtime) flushDirtyFiles(ctx context.Context) (bool, error) {
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	wrote := false
	for _, key := range runtime.dirtyFileKeysLocked() {
		if err := ctx.Err(); err != nil {
			return wrote, err
		}
		wrote = true
		if err := runtime.storeXFileLocked(runtime.files[key]); err != nil {
			return wrote, fmt.Errorf("%w: SKT file %q: %v", backend.ErrCheckpointSaveWrite, strings.TrimPrefix(key, xFileSaveScope), err)
		}
	}
	return wrote, nil
}

// storeAtSessionEnd is the last point a title's writes reach the store: when
// the session ends, however it ends — destroyed by the Host, the title's own
// exit, or a failure — every file not stored yet is, written and never
// flushed or closed, or refused by an earlier attempt, and so is every write a
// refused store left pending. Each is tried even when one before it is
// refused. A session retired by a quick load has its store detached first, and
// stores nothing here.
func (runtime *Runtime) storeAtSessionEnd() {
	if runtime.saveStoreBoundary() == nil {
		return
	}
	runtime.fileMu.Lock()
	for _, key := range runtime.dirtyFileKeysLocked() {
		if err := runtime.storeXFileLocked(runtime.files[key]); err != nil && runtime.logger != nil {
			runtime.logger.Debug("XFile store at session end failed", "key", key, "error", err)
		}
	}
	runtime.fileMu.Unlock()
	runtime.savePendingMu.Lock()
	defer runtime.savePendingMu.Unlock()
	store := runtime.saveStoreBoundary()
	for _, key := range slices.Sorted(maps.Keys(runtime.pendingSaves)) {
		if store == nil {
			return
		}
		if err := store.StoreSave(key, runtime.pendingSaves[key]); err != nil {
			if runtime.logger != nil {
				runtime.logger.Debug("pending save at session end failed", "key", key, "error", err)
			}
			continue
		}
		delete(runtime.pendingSaves, key)
	}
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
	if err := heap.rebuildCheckpointFiles(charge, &remaining, fail); err != nil {
		return err
	}
	if err := heap.rebuildCheckpointDatabases(charge, fail); err != nil {
		return err
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

// rebuildCheckpointFiles puts every restored handle back on a file read from
// the current saves. A record names a handle's path, mode and position and
// holds no bytes, and the handles on one path — whatever build wrote the
// record — get the path's one file again, read once through the lookup a
// first open makes: the removal list, the save, the packaged file. A removed
// path leaves its handles open over an absent, empty file; restoring
// execution never brings back older bytes from a slot. An entry of an
// installed archive is read out of the archive as it is now.
func (heap *checkpointHeap) rebuildCheckpointFiles(charge func(int64) error, remaining *int64, fail func(error) error) error {
	runtime := heap.runtime
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	// The directories FileSystem.mkdir made are read again from the current
	// saves at first use, as the removal list is.
	runtime.files, runtime.removedFiles, runtime.wipiDirectories = nil, nil, nil
	archives := make(map[string]map[string][]byte)
	for _, file := range heap.files {
		file.node = nil
		if file.name == "" {
			continue
		}
		name := file.name
		if file.archiveName != "" {
			name = file.archiveName
		}
		key, err := xFileKey(name)
		if err != nil {
			return fail(err)
		}
		fresh := runtime.files[key] == nil
		node, err := runtime.xFileNodeLocked(key)
		if err != nil {
			return fail(err)
		}
		if fresh {
			if err = charge(int64(len(node.data))); err != nil {
				return err
			}
		}
		if file.archiveName == "" {
			file.node = node
			continue
		}
		entries, parsed := archives[key]
		if !parsed && node.present {
			limits := defaultArchiveLimits
			limits.entry = min(limits.entry, uint64(*remaining))
			limits.total = uint64(*remaining)
			entries, err = readJARWithin(node.data, limits)
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
		data, found := entries[file.archiveEntry]
		file.node = &xFileNode{data: data, present: found}
	}
	return nil
}

// rebuildCheckpointDatabases makes the restored WIPI databases the session's
// own and reads each from the current saves, through the same budget as the
// files: the index, then the records, refused whole as an open refuses them.
// A database deleted since the quick save is restored deleted, with the record
// size its handles had. It runs again when the old session's writes changed
// the saves, so it starts each time from nothing.
func (heap *checkpointHeap) rebuildCheckpointDatabases(charge func(int64) error, fail func(error) error) error {
	runtime := heap.runtime
	runtime.wipiDatabaseMu.Lock()
	defer runtime.wipiDatabaseMu.Unlock()
	runtime.wipiDatabaseIndex, runtime.wipiDatabases = nil, nil
	for _, db := range heap.databases {
		db.loaded = false
		if err := runtime.loadWIPIDatabaseLocked(db, charge); err != nil {
			if errors.Is(err, backend.ErrCheckpointSaveRead) {
				return err
			}
			return fail(err)
		}
		if runtime.wipiDatabases == nil {
			runtime.wipiDatabases = make(map[string]*wipiDatabase, len(heap.databases))
		}
		runtime.wipiDatabases[db.name] = db
	}
	return nil
}
