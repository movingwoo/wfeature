package ktf

import (
	"fmt"
	"strings"
)

// runtimeStorageState is what a checkpoint keeps of the storage tables: which
// names are open, under which handles, at which cursor. It holds no file
// bytes, no record lists and none of the removal or directory lists. A save
// has one home, the save store, and a load fills these objects from it as it
// is then; see storage_rebind.go.
type runtimeStorageState struct {
	Files            []cFileSnapshot
	FileHandles      []cFileHandleSnapshot
	NextFileHandle   uint32
	Records          []cRecordSnapshot
	RecordHandles    []cRecordHandleSnapshot
	NextRecordHandle uint32
	// GuestFiles are the names this session wrote through the File class,
	// which is what its listing shows beside the archive's own names.
	GuestFiles [][]byte
}
type cFileSnapshot struct {
	Name    []byte
	Catalog bool
}
type cFileHandleSnapshot struct {
	Handle, Store uint32
	Position      int32
}

// RecordSize is the one field of a record database that is not on disk: the
// title supplied it at open and is only told it back.
type cRecordSnapshot struct {
	Name       []byte
	RecordSize uint32
	Catalog    bool
}
type cRecordHandleSnapshot struct{ Handle, Store uint32 }

// validStorageName is what a name in a record has to be to be looked up in a
// store: a name its table would have accepted from the guest.
func validStorageName(scope string, name []byte) bool {
	return len(name) <= 1<<20 && storableName(scope, strings.TrimPrefix(string(name), "/"))
}

// captureStorageState records the tables by name. charge is the checkpoint's
// storage budget: a load reads back what these objects hold, so what they hold
// now is charged now, and a checkpoint no load would accept is not taken.
func (runtime *initializationRuntime) captureStorageState(charge func(uint64) error) (runtimeStorageState, error) {
	saved := runtimeStorageState{NextFileHandle: runtime.nextCDatabaseHandle, NextRecordHandle: runtime.nextRecordDatabaseHandle}
	for _, count := range []int{len(runtime.cFiles), len(runtime.recordDatabases), len(runtime.guestFiles)} {
		if count > 1<<16 {
			return runtimeStorageState{}, fmt.Errorf("KTF storage table count exceeds limit")
		}
	}
	if len(runtime.cFileHandles) > maxCFileHandles || len(runtime.recordDatabaseHandles) > maxRecordDatabaseHandles {
		return runtimeStorageState{}, fmt.Errorf("KTF storage handle count exceeds limit")
	}
	fileIDs := make(map[*runtimeCFile]uint32)
	fileID := func(file *runtimeCFile) (uint32, error) {
		if file == nil {
			return 0, fmt.Errorf("KTF file store is nil")
		}
		if id := fileIDs[file]; id != 0 {
			return id, nil
		}
		if len(file.name) > 1<<20 || len(saved.Files) >= 1<<16 {
			return 0, fmt.Errorf("KTF file store exceeds snapshot limits")
		}
		if err := charge(uint64(len(file.name)) + uint64(len(file.data)) + 64); err != nil {
			return 0, err
		}
		id := uint32(len(saved.Files) + 1)
		fileIDs[file] = id
		saved.Files = append(saved.Files, cFileSnapshot{Name: []byte(file.name)})
		return id, nil
	}
	// Map order would make two captures of one session differ, and a record
	// that is compared or hashed has to be the same record.
	for _, name := range metadataKeys(runtime.cFiles) {
		file := runtime.cFiles[name]
		if file == nil || name != file.name {
			return runtimeStorageState{}, fmt.Errorf("KTF file catalog differs from its store name")
		}
		id, err := fileID(file)
		if err != nil {
			return runtimeStorageState{}, err
		}
		saved.Files[id-1].Catalog = true
	}
	for _, handle := range metadataKeys(runtime.cFileHandles) {
		open := runtime.cFileHandles[handle]
		if open == nil || open.position < 0 || open.position > maxHeapStorageBytes {
			return runtimeStorageState{}, fmt.Errorf("KTF open file cursor is invalid")
		}
		id, err := fileID(open.store)
		if err != nil {
			return runtimeStorageState{}, err
		}
		saved.FileHandles = append(saved.FileHandles, cFileHandleSnapshot{Handle: handle, Store: id, Position: int32(open.position)})
	}
	recordIDs := make(map[*runtimeRecordDatabase]uint32)
	recordID := func(store *runtimeRecordDatabase) (uint32, error) {
		if store == nil {
			return 0, fmt.Errorf("KTF record store is nil")
		}
		if id := recordIDs[store]; id != 0 {
			return id, nil
		}
		if len(store.name) > maxRecordDatabaseName || len(store.records) > maxDataBaseRecords || len(saved.Records) >= 1<<16 {
			return 0, fmt.Errorf("KTF record store exceeds snapshot limits")
		}
		if err := charge(uint64(len(store.name)) + recordListBytes(store.records) + 64); err != nil {
			return 0, err
		}
		id := uint32(len(saved.Records) + 1)
		recordIDs[store] = id
		saved.Records = append(saved.Records, cRecordSnapshot{Name: []byte(store.name), RecordSize: store.recordSize})
		return id, nil
	}
	for _, name := range metadataKeys(runtime.recordDatabases) {
		store := runtime.recordDatabases[name]
		if store == nil || name != store.name {
			return runtimeStorageState{}, fmt.Errorf("KTF record catalog differs from its store name")
		}
		id, err := recordID(store)
		if err != nil {
			return runtimeStorageState{}, err
		}
		saved.Records[id-1].Catalog = true
	}
	for _, handle := range metadataKeys(runtime.recordDatabaseHandles) {
		open := runtime.recordDatabaseHandles[handle]
		if open == nil {
			return runtimeStorageState{}, fmt.Errorf("KTF record handle is nil")
		}
		id, err := recordID(open.store)
		if err != nil {
			return runtimeStorageState{}, err
		}
		saved.RecordHandles = append(saved.RecordHandles, cRecordHandleSnapshot{Handle: handle, Store: id})
	}
	for _, name := range metadataKeys(runtime.guestFiles) {
		if len(name) > 1<<20 {
			return runtimeStorageState{}, fmt.Errorf("KTF guest filename exceeds snapshot limit")
		}
		if err := charge(uint64(len(name)) + uint64(len(runtime.guestFiles[name])) + 64); err != nil {
			return runtimeStorageState{}, err
		}
		saved.GuestFiles = append(saved.GuestFiles, []byte(name))
	}
	if err := saved.validate(); err != nil {
		return runtimeStorageState{}, err
	}
	return saved, nil
}

// recordListBytes is what a record list costs a storage budget: its records
// and a fixed amount for each slot, so that a long list of empty records is
// not free.
func recordListBytes(records [][]byte) uint64 {
	size := uint64(len(records)) * 24
	for _, record := range records {
		size += uint64(len(record))
	}
	return size
}

func (saved runtimeStorageState) validate() error {
	if len(saved.Files) > 1<<16 || len(saved.Records) > 1<<16 || len(saved.GuestFiles) > 1<<16 || len(saved.FileHandles) > maxCFileHandles || len(saved.RecordHandles) > maxRecordDatabaseHandles || saved.NextFileHandle > maxCFileHandles || saved.NextRecordHandle > maxRecordDatabaseHandles {
		return fmt.Errorf("KTF storage state counts or handle sequences exceed limits")
	}
	catalog := make(map[string]bool)
	for _, file := range saved.Files {
		if !validStorageName(cFileScope, file.Name) || file.Catalog && catalog[string(file.Name)] {
			return fmt.Errorf("KTF file catalog or name is invalid")
		}
		if file.Catalog {
			catalog[string(file.Name)] = true
		}
	}
	catalog = make(map[string]bool)
	for _, record := range saved.Records {
		if len(record.Name) > maxRecordDatabaseName || !validStorageName(recordDatabaseScope, record.Name) || record.Catalog && catalog[string(record.Name)] {
			return fmt.Errorf("KTF record catalog or name is invalid")
		}
		if record.Catalog {
			catalog[string(record.Name)] = true
		}
	}
	used := make(map[uint32]bool)
	validHandle := func(handle, tag, next uint32) bool {
		return handle > tag && handle <= (tag|next) && handle&0xfffff000 == tag
	}
	held := make([]bool, len(saved.Files)+1)
	for _, handle := range saved.FileHandles {
		if !validHandle(handle.Handle, cFileHandleBit, saved.NextFileHandle) || used[handle.Handle] || handle.Store == 0 || uint64(handle.Store) > uint64(len(saved.Files)) || handle.Position < 0 || handle.Position > maxHeapStorageBytes {
			return fmt.Errorf("KTF file handle state is invalid")
		}
		used[handle.Handle], held[handle.Store] = true, true
	}
	// A store is in a record because the catalog or a handle holds it. One
	// that nothing holds was put there, and every store is a read at load.
	for index, file := range saved.Files {
		if !file.Catalog && !held[index+1] {
			return fmt.Errorf("KTF file store is held by nothing")
		}
	}
	held = make([]bool, len(saved.Records)+1)
	for _, handle := range saved.RecordHandles {
		if !validHandle(handle.Handle, recordDatabaseHandleBit, saved.NextRecordHandle) || used[handle.Handle] || handle.Store == 0 || uint64(handle.Store) > uint64(len(saved.Records)) {
			return fmt.Errorf("KTF record handle state is invalid")
		}
		used[handle.Handle], held[handle.Store] = true, true
	}
	for index, record := range saved.Records {
		if !record.Catalog && !held[index+1] {
			return fmt.Errorf("KTF record store is held by nothing")
		}
	}
	names := make(map[string]bool)
	for _, name := range saved.GuestFiles {
		if names[string(name)] || !validStorageName(guestFileScope, name) {
			return fmt.Errorf("KTF guest file name is invalid or duplicated")
		}
		names[string(name)] = true
	}
	return nil
}

// restoredRuntimeStorage is the storage tables as a record names them: every
// store is there under its handles, with its name and nothing in it. Building
// it reaches no save store.
type restoredRuntimeStorage struct {
	files                map[string]*runtimeCFile
	fileHandles          map[uint32]*runtimeCFileHandle
	records              map[string]*runtimeRecordDatabase
	recordHandles        map[uint32]*runtimeRecordDatabaseHandle
	nextFile, nextRecord uint32
	restored             *restoredStorage
}

func restoreStorageState(saved runtimeStorageState) (restoredRuntimeStorage, error) {
	if err := saved.validate(); err != nil {
		return restoredRuntimeStorage{}, err
	}
	state := restoredRuntimeStorage{files: make(map[string]*runtimeCFile), fileHandles: make(map[uint32]*runtimeCFileHandle), records: make(map[string]*runtimeRecordDatabase), recordHandles: make(map[uint32]*runtimeRecordDatabaseHandle), nextFile: saved.NextFileHandle, nextRecord: saved.NextRecordHandle, restored: &restoredStorage{}}
	// One name is one store. A record can name a file twice — once in the
	// catalog and once under a handle that outlived it — and both are the
	// same file once they are read back from the store.
	named := make(map[string]*runtimeCFile, len(saved.Files))
	files := make([]*runtimeCFile, len(saved.Files)+1)
	for i, record := range saved.Files {
		file := named[string(record.Name)]
		if file == nil {
			file = &runtimeCFile{name: string(record.Name)}
			named[file.name] = file
			state.restored.cFiles = append(state.restored.cFiles, file)
		}
		files[i+1] = file
		if record.Catalog {
			state.files[file.name] = file
		}
	}
	for _, handle := range saved.FileHandles {
		state.fileHandles[handle.Handle] = &runtimeCFileHandle{store: files[handle.Store], position: int(handle.Position)}
	}
	namedRecords := make(map[string]*runtimeRecordDatabase, len(saved.Records))
	records := make([]*runtimeRecordDatabase, len(saved.Records)+1)
	for i, record := range saved.Records {
		store := namedRecords[string(record.Name)]
		if store == nil {
			store = &runtimeRecordDatabase{name: string(record.Name), recordSize: record.RecordSize}
			namedRecords[store.name] = store
			state.restored.records = append(state.restored.records, store)
		}
		records[i+1] = store
		if record.Catalog {
			// The catalog's store is the one a later open is handed, so its
			// record size is the one that stays.
			store.recordSize = record.RecordSize
			state.records[store.name] = store
		}
	}
	for _, handle := range saved.RecordHandles {
		state.recordHandles[handle.Handle] = &runtimeRecordDatabaseHandle{store: records[handle.Store]}
	}
	for _, name := range saved.GuestFiles {
		state.restored.guestFiles = append(state.restored.guestFiles, string(name))
	}
	return state, nil
}

// adopt hands the restored tables to a runtime. The removal and directory
// lists are left unread, which is the state every accessor of them already
// handles: the first use reads the list as the store has it. The written-file
// table is left empty until a store fills it.
func (state restoredRuntimeStorage) adopt(runtime *initializationRuntime) {
	runtime.cFiles, runtime.cFileHandles, runtime.nextCDatabaseHandle = state.files, state.fileHandles, state.nextFile
	runtime.recordDatabases, runtime.recordDatabaseHandles, runtime.nextRecordDatabaseHandle = state.records, state.recordHandles, state.nextRecord
	runtime.guestFiles = nil
	runtime.removedFiles, runtime.removedCDatabases, runtime.madeDirectories, runtime.removedDatabaseLists = nil, nil, nil, nil
	runtime.detachedCFiles, runtime.detachedRecordDatabases, runtime.detachedDatabases = nil, nil, nil
	runtime.restoredStorage = state.restored
}
