package ktf

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Quick load and storage
//
// A checkpoint is the title's execution state and nothing else. The slot
// carries no save and a load replaces none: what the title saved after the
// checkpoint is still there after a load, and the restored title reads and
// writes the saves as the store has them then.
//
// This runtime keeps host copies of what a title has open: the WIPI C file
// table and its catalog, the record databases, the Java databases, the bytes
// behind every File object, the table of names the session wrote, and the
// removal and directory lists. Each of them answers before the store is asked,
// and each write stores the host copy whole. So a copy brought back from a
// checkpoint would answer the restored title with what the file held when the
// checkpoint was taken, and the next write through it would put that over the
// file as it is now.
//
// A checkpoint therefore brings back names: which names were open, under which
// handles and objects, at which cursor. Committing the load fills every one of
// them from the store, through the lookup a first open of that name makes, and
// then the runtime is in its ordinary state: every host copy equals the store,
// and stays so because every write goes through to it. The load itself writes
// nothing.
//
// The rules, which are the same for every table:
//
//   - A cursor stays where the record has it, even past the end of a file
//     that is shorter now. A read there answers end of file and a write fills
//     the gap with zeros.
//   - A name the store has nothing for leaves its object empty, and the host
//     makes nothing. The object and its handle stay valid, so the title's next
//     write through it makes the name again; until then a question about the
//     name is answered from the store. The empty store is kept by name (the
//     detached tables), so that an open, a rename or a write of the name takes
//     that store and the name never has two.
//   - A File object whose open asked for an empty file takes at most what it
//     had written, from the front of the file as it is now. A File whose open
//     found nothing under its name is given the whole file, like any other.
//     See runtimeGuestFile.truncated.
//   - The WIPI C file table has no such object. A truncating open there
//     empties the file in the store at the open itself, so nothing of it is
//     still to come when a checkpoint is taken, and a handle is a cursor on
//     the one store its name has: after a load, on the file as it is.
//   - Every list this runtime keeps beside the files is read, so a store that
//     cannot be read refuses the load rather than the title's first call after
//     it.
//   - What is read is bounded: the names come from a slot, which is untrusted.
//
// What is not rebuilt, because it was never on disk: a record database's
// record size, and the certificate an authentication adapter issued for this
// run (save_adapter_state.go).

// restoredStorage is what a checkpoint brought back by name and a store has
// not filled yet: every store once, whatever holds it, and every File object
// of the heap.
type restoredStorage struct {
	cFiles     []*runtimeCFile
	records    []*runtimeRecordDatabase
	databases  []*runtimeDataBaseStore
	guestFiles []string
	files      []restoredGuestFile
	// budget is how many bytes these may take from the store between them,
	// and how many may be read to fill them. Zero is maxHeapStorageBytes.
	budget uint64
}

func (restored *restoredStorage) limit() uint64 {
	if restored == nil || restored.budget == 0 {
		return maxHeapStorageBytes
	}
	return restored.budget
}

// restoredGuestFile is one File object and, for one whose open asked for an
// empty file, how many bytes it held when the checkpoint was taken.
type restoredGuestFile struct {
	state  *runtimeGuestFile
	length int
}

// bindRestoredStorage connects a restored client to the store it will run
// over: the authentication adapters are built over it, and every restored
// storage object is filled from it. Only reads are made. On a failure the
// client is left on the store it had, still to be bound, and the error says
// the saves could not be read; nothing of the store or of a running session
// has changed.
func (client *Client) bindRestoredStorage(adapters saveAdapterState, live SaveStore) error {
	// Every read goes through one reader, under the adapters: each key is
	// read once, so every object of one name is given the same bytes, and on
	// a budget, because the names come from a slot.
	reader := backend.NewRebuildReader(live, int64(client.runtime.restoredStorage.limit()))
	store, err := bindSaveAdapters(adapters, reader)
	if err != nil {
		return fmt.Errorf("%w: %v", backend.ErrCheckpointSaveRead, err)
	}
	unbound := client.saveStore
	client.saveStore = store
	err = client.runtime.rebindRestoredStorage()
	if err == nil {
		err = reader.Err()
	}
	if err != nil {
		client.saveStore = unbound
		return fmt.Errorf("%w: %v", backend.ErrCheckpointSaveRead, err)
	}
	// The adapters now stand on the store itself, as in a session that was
	// started over it.
	client.saveStore = rebaseSaveAdapters(store, live)
	return nil
}

// rebindRestoredStorage fills what a checkpoint brought back from the store
// the client reads. It starts from the names every time, so a second attempt
// after a failed one is the first attempt again; the caller clears
// restoredStorage once the result is adopted.
func (runtime *initializationRuntime) rebindRestoredStorage() error {
	restored := runtime.restoredStorage
	if restored == nil {
		return nil
	}
	runtime.saveReadError = nil
	runtime.removedFiles, runtime.removedCDatabases, runtime.madeDirectories, runtime.removedDatabaseLists = nil, nil, nil, nil
	runtime.guestFiles = nil
	runtime.cFiles, runtime.detachedCFiles = make(map[string]*runtimeCFile), nil
	runtime.recordDatabases, runtime.detachedRecordDatabases = make(map[string]*runtimeRecordDatabase), nil
	runtime.databases, runtime.detachedDatabases = make(map[string]*runtimeDataBaseStore), nil
	failed := func(err error) error {
		// A failed read must not outlive the attempt: the runtime keeps one
		// for the life of a session and refuses every later write.
		runtime.saveReadError = nil
		return err
	}
	kept, limit := uint64(0), restored.limit()
	keep := func(size uint64) error {
		if size > limit || kept > limit-size {
			return fmt.Errorf("the saves the checkpoint has open hold more than the %d bytes a load restores", limit)
		}
		kept += size
		return nil
	}

	// Every list is read here, the ones no lookup below asks for among them.
	// A list is read once and kept for the session, and a read that fails
	// stops every storage call after it: that has to refuse the load, not
	// meet the title at its first call.
	runtime.removedGuestFiles()
	runtime.removedDatabases()
	runtime.createdDirectories()
	runtime.recordDatabaseRemovals(recordDatabaseRemovedKey)
	runtime.recordDatabaseRemovals(javaDatabaseRemovedKey)
	if runtime.saveReadError != nil {
		return failed(runtime.saveReadError)
	}

	// The names the session wrote come first, because every other lookup
	// resolves through this table before it asks the store.
	for _, name := range restored.guestFiles {
		trimmed := strings.TrimPrefix(name, "/")
		if runtime.removedGuestFiles()[trimmed] {
			continue
		}
		data, present := runtime.loadSave("fs/" + trimmed)
		if runtime.saveReadError != nil {
			return failed(runtime.saveReadError)
		}
		if !present {
			continue
		}
		if err := keep(uint64(len(data))); err != nil {
			return failed(err)
		}
		if runtime.guestFiles == nil {
			runtime.guestFiles = make(map[string][]byte)
		}
		runtime.guestFiles[name] = bytes.Clone(data)
	}

	held := make(map[*runtimeCFile]bool, len(runtime.cFileHandles))
	for _, open := range runtime.cFileHandles {
		held[open.store] = true
	}
	for _, store := range restored.cFiles {
		seed, found := runtime.databaseSeed(store.name)
		store.data, store.packaged = nil, 0
		if packaged, shipped := runtime.packagedDatabase(store.name); shipped {
			store.packaged = len(packaged)
		}
		if runtime.saveReadError != nil {
			return failed(runtime.saveReadError)
		}
		if !found {
			if held[store] {
				if runtime.detachedCFiles == nil {
					runtime.detachedCFiles = make(map[string]*runtimeCFile)
				}
				runtime.detachedCFiles[store.name] = store
			}
			continue
		}
		if err := keep(uint64(len(seed))); err != nil {
			return failed(err)
		}
		store.data = append([]byte(nil), seed...)
		runtime.cFiles[store.name] = store
	}

	heldRecords := make(map[*runtimeRecordDatabase]bool, len(runtime.recordDatabaseHandles))
	for _, open := range runtime.recordDatabaseHandles {
		heldRecords[open.store] = true
	}
	for _, store := range restored.records {
		records, source, err := runtime.resolveRecordDatabase(store.name, store.recordSize)
		if err != nil {
			return failed(err)
		}
		// The probes for a packaged copy read the store too, and a read that
		// failed there is not among the errors the lookup answers.
		if runtime.saveReadError != nil {
			return failed(runtime.saveReadError)
		}
		store.records = nil
		if source == storageAbsent {
			if heldRecords[store] {
				if runtime.detachedRecordDatabases == nil {
					runtime.detachedRecordDatabases = make(map[string]*runtimeRecordDatabase)
				}
				runtime.detachedRecordDatabases[store.name] = store
			}
			continue
		}
		if err := keep(recordListBytes(records)); err != nil {
			return failed(err)
		}
		store.records = records
		runtime.recordDatabases[store.name] = store
	}

	for _, store := range restored.databases {
		// The record size only words a diagnostic for a packaged database
		// whose index disagrees with it, and it is not kept: zero asks for
		// none.
		records, source, err := runtime.resolveJavaDatabase(store.name, 0)
		if err != nil {
			return failed(err)
		}
		if runtime.saveReadError != nil {
			return failed(runtime.saveReadError)
		}
		store.records = nil
		if source == storageAbsent {
			// An object may still hold it; the heap is not walked to find out.
			if runtime.detachedDatabases == nil {
				runtime.detachedDatabases = make(map[string]*runtimeDataBaseStore)
			}
			runtime.detachedDatabases[store.name] = store
			continue
		}
		if err := keep(recordListBytes(records)); err != nil {
			return failed(err)
		}
		store.records = records
		runtime.databases[store.name] = store
	}

	for _, file := range restored.files {
		data, _ := runtime.guestFile(file.state.name)
		if runtime.saveReadError != nil {
			return failed(runtime.saveReadError)
		}
		if file.state.truncated && len(data) > file.length {
			data = data[:file.length]
		}
		if err := keep(uint64(len(data))); err != nil {
			return failed(err)
		}
		file.state.data = bytes.Clone(data)
	}
	return nil
}
