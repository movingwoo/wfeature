package ktf

import (
	"bytes"
	"fmt"
	"strings"
)

type runtimeStorageState struct {
	Files            []cFileSnapshot
	FileHandles      []cFileHandleSnapshot
	NextFileHandle   uint32
	Records          []cRecordSnapshot
	RecordHandles    []cRecordHandleSnapshot
	NextRecordHandle uint32
	GuestFiles       []storageBytesSnapshot
	RemovedFiles     []storageFlagSnapshot
	RemovedCFiles    []storageFlagSnapshot
	Directories      []storageFlagSnapshot
	RemovedRecords   []storageFlagGroup
}
type cFileSnapshot struct {
	Name, Data []byte
	Packaged   int32
	Catalog    bool
}
type cFileHandleSnapshot struct {
	Handle, Store uint32
	Position      int32
}
type cRecordSnapshot struct {
	Name       []byte
	Records    [][]byte
	RecordSize uint32
	Catalog    bool
}
type cRecordHandleSnapshot struct{ Handle, Store uint32 }
type storageBytesSnapshot struct{ Name, Data []byte }
type storageFlagSnapshot struct {
	Name  []byte
	Value bool
}
type storageFlagGroup struct {
	Key   string
	Flags []storageFlagSnapshot
}

type storageStateBudget struct{ used uint64 }

func (budget *storageStateBudget) charge(size uint64) error {
	if size > maxHeapStorageBytes || budget.used > maxHeapStorageBytes-size {
		return fmt.Errorf("KTF storage state data exceeds limit")
	}
	budget.used += size
	return nil
}
func (budget *storageStateBudget) name(scope string, name []byte, extra uint64) error {
	if len(name) > 1<<20 || !storableName(scope, strings.TrimPrefix(string(name), "/")) {
		return fmt.Errorf("KTF storage state name is invalid")
	}
	return budget.charge(uint64(len(name)) + extra + 64)
}

func (runtime *initializationRuntime) captureStorageState() (runtimeStorageState, error) {
	saved := runtimeStorageState{NextFileHandle: runtime.nextCDatabaseHandle, NextRecordHandle: runtime.nextRecordDatabaseHandle}
	for _, count := range []int{len(runtime.cFiles), len(runtime.recordDatabases), len(runtime.guestFiles), len(runtime.removedFiles), len(runtime.removedCDatabases), len(runtime.madeDirectories)} {
		if count > 1<<16 {
			return runtimeStorageState{}, fmt.Errorf("KTF storage table count exceeds limit")
		}
	}
	if len(runtime.cFileHandles) > maxCFileHandles || len(runtime.recordDatabaseHandles) > maxRecordDatabaseHandles || len(runtime.removedDatabaseLists) > 2 {
		return runtimeStorageState{}, fmt.Errorf("KTF storage handle or removal-list count exceeds limit")
	}
	// Initially borrow data views while building and validating the bounded
	// record. Clone below only after every table and handle has passed.
	budget := storageStateBudget{}
	fileIDs := make(map[*runtimeCFile]uint32)
	fileID := func(file *runtimeCFile) (uint32, error) {
		if file == nil {
			return 0, fmt.Errorf("KTF file store is nil")
		}
		if id := fileIDs[file]; id != 0 {
			return id, nil
		}
		if len(file.name) > 1<<20 || file.packaged < 0 || file.packaged > maxHeapStorageBytes || len(saved.Files) >= 1<<16 {
			return 0, fmt.Errorf("KTF file store exceeds snapshot limits")
		}
		if err := budget.charge(uint64(len(file.name)) + uint64(len(file.data)) + 64); err != nil {
			return 0, err
		}
		id := uint32(len(saved.Files) + 1)
		fileIDs[file] = id
		saved.Files = append(saved.Files, cFileSnapshot{Name: []byte(file.name), Data: file.data, Packaged: int32(file.packaged)})
		return id, nil
	}
	for name, file := range runtime.cFiles {
		if file == nil || name != file.name {
			return runtimeStorageState{}, fmt.Errorf("KTF file catalog differs from its store name")
		}
		id, err := fileID(file)
		if err != nil {
			return runtimeStorageState{}, err
		}
		saved.Files[id-1].Catalog = true
	}
	for handle, open := range runtime.cFileHandles {
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
		if err := budget.charge(uint64(len(store.name)) + uint64(len(store.records))*24 + 64); err != nil {
			return 0, err
		}
		for _, data := range store.records {
			if err := budget.charge(uint64(len(data))); err != nil {
				return 0, err
			}
		}
		id := uint32(len(saved.Records) + 1)
		recordIDs[store] = id
		saved.Records = append(saved.Records, cRecordSnapshot{Name: []byte(store.name), Records: store.records, RecordSize: store.recordSize})
		return id, nil
	}
	for name, store := range runtime.recordDatabases {
		if store == nil || name != store.name {
			return runtimeStorageState{}, fmt.Errorf("KTF record catalog differs from its store name")
		}
		id, err := recordID(store)
		if err != nil {
			return runtimeStorageState{}, err
		}
		saved.Records[id-1].Catalog = true
	}
	for handle, open := range runtime.recordDatabaseHandles {
		if open == nil {
			return runtimeStorageState{}, fmt.Errorf("KTF record handle is nil")
		}
		id, err := recordID(open.store)
		if err != nil {
			return runtimeStorageState{}, err
		}
		saved.RecordHandles = append(saved.RecordHandles, cRecordHandleSnapshot{Handle: handle, Store: id})
	}
	if runtime.guestFiles != nil {
		saved.GuestFiles = make([]storageBytesSnapshot, 0, len(runtime.guestFiles))
	}
	for name, data := range runtime.guestFiles {
		if len(name) > 1<<20 {
			return runtimeStorageState{}, fmt.Errorf("KTF guest filename exceeds snapshot limit")
		}
		if err := budget.charge(uint64(len(name)) + uint64(len(data)) + 64); err != nil {
			return runtimeStorageState{}, err
		}
		saved.GuestFiles = append(saved.GuestFiles, storageBytesSnapshot{Name: []byte(name), Data: data})
	}
	flags := func(table map[string]bool) ([]storageFlagSnapshot, error) {
		if table == nil {
			return nil, nil
		}
		if len(table) > 1<<16 {
			return nil, fmt.Errorf("KTF storage removal cache exceeds limit")
		}
		result := make([]storageFlagSnapshot, 0, len(table))
		for name, value := range table {
			if len(name) > 1<<20 {
				return nil, fmt.Errorf("KTF storage removal name exceeds limit")
			}
			if err := budget.charge(uint64(len(name)) + 64); err != nil {
				return nil, err
			}
			result = append(result, storageFlagSnapshot{Name: []byte(name), Value: value})
		}
		return result, nil
	}
	var err error
	if saved.RemovedFiles, err = flags(runtime.removedFiles); err != nil {
		return runtimeStorageState{}, err
	}
	if saved.RemovedCFiles, err = flags(runtime.removedCDatabases); err != nil {
		return runtimeStorageState{}, err
	}
	if saved.Directories, err = flags(runtime.madeDirectories); err != nil {
		return runtimeStorageState{}, err
	}
	if runtime.removedDatabaseLists != nil {
		saved.RemovedRecords = make([]storageFlagGroup, 0, len(runtime.removedDatabaseLists))
	}
	for key, table := range runtime.removedDatabaseLists {
		records, err := flags(table)
		if err != nil {
			return runtimeStorageState{}, err
		}
		saved.RemovedRecords = append(saved.RemovedRecords, storageFlagGroup{Key: key, Flags: records})
	}
	if err := saved.validate(); err != nil {
		return runtimeStorageState{}, err
	}
	for i := range saved.Files {
		saved.Files[i].Data = bytes.Clone(saved.Files[i].Data)
	}
	for i := range saved.Records {
		saved.Records[i].Records = cloneHeapRecords(saved.Records[i].Records)
	}
	for i := range saved.GuestFiles {
		saved.GuestFiles[i].Data = bytes.Clone(saved.GuestFiles[i].Data)
	}
	return saved, nil
}

func (saved runtimeStorageState) validate() error {
	if len(saved.Files) > 1<<16 || len(saved.Records) > 1<<16 || len(saved.GuestFiles) > 1<<16 || len(saved.FileHandles) > maxCFileHandles || len(saved.RecordHandles) > maxRecordDatabaseHandles || saved.NextFileHandle > maxCFileHandles || saved.NextRecordHandle > maxRecordDatabaseHandles || len(saved.RemovedRecords) > 2 {
		return fmt.Errorf("KTF storage state counts or handle sequences exceed limits")
	}
	budget := storageStateBudget{}
	catalog := make(map[string]bool)
	for _, file := range saved.Files {
		if file.Packaged < 0 || file.Packaged > maxHeapStorageBytes || file.Catalog && catalog[string(file.Name)] {
			return fmt.Errorf("KTF file catalog or packaged-byte count is invalid")
		}
		if err := budget.name(cFileScope, file.Name, uint64(len(file.Data))); err != nil {
			return err
		}
		if file.Catalog {
			catalog[string(file.Name)] = true
		}
	}
	catalog = make(map[string]bool)
	for _, record := range saved.Records {
		if len(record.Name) > maxRecordDatabaseName || len(record.Records) > maxDataBaseRecords || record.Catalog && catalog[string(record.Name)] {
			return fmt.Errorf("KTF record catalog or shape is invalid")
		}
		if err := budget.name(recordDatabaseScope, record.Name, uint64(len(record.Records))*24); err != nil {
			return err
		}
		for _, data := range record.Records {
			if err := budget.charge(uint64(len(data))); err != nil {
				return err
			}
		}
		if record.Catalog {
			catalog[string(record.Name)] = true
		}
	}
	used := make(map[uint32]bool)
	validHandle := func(handle, tag, next uint32) bool {
		return handle > tag && handle <= (tag|next) && handle&0xfffff000 == tag
	}
	for _, handle := range saved.FileHandles {
		if !validHandle(handle.Handle, cFileHandleBit, saved.NextFileHandle) || used[handle.Handle] || handle.Store == 0 || uint64(handle.Store) > uint64(len(saved.Files)) || handle.Position < 0 || handle.Position > maxHeapStorageBytes {
			return fmt.Errorf("KTF file handle state is invalid")
		}
		used[handle.Handle] = true
	}
	for _, handle := range saved.RecordHandles {
		if !validHandle(handle.Handle, recordDatabaseHandleBit, saved.NextRecordHandle) || used[handle.Handle] || handle.Store == 0 || uint64(handle.Store) > uint64(len(saved.Records)) {
			return fmt.Errorf("KTF record handle state is invalid")
		}
		used[handle.Handle] = true
	}
	names := make(map[string]bool)
	for _, file := range saved.GuestFiles {
		if names[string(file.Name)] {
			return fmt.Errorf("KTF guest file name is duplicated")
		}
		names[string(file.Name)] = true
		if err := budget.name(guestFileScope, file.Name, uint64(len(file.Data))); err != nil {
			return err
		}
	}
	flags := func(scope string, entries []storageFlagSnapshot) error {
		if len(entries) > 1<<16 {
			return fmt.Errorf("KTF storage flag table exceeds limit")
		}
		names := make(map[string]bool)
		for _, flag := range entries {
			if names[string(flag.Name)] {
				return fmt.Errorf("KTF storage flag name is duplicated")
			}
			names[string(flag.Name)] = true
			if err := budget.name(scope, flag.Name, 0); err != nil {
				return err
			}
		}
		return nil
	}
	for _, table := range []struct {
		scope   string
		entries []storageFlagSnapshot
	}{{guestFileScope, saved.RemovedFiles}, {cFileScope, saved.RemovedCFiles}, {cFileScope, saved.Directories}} {
		if err := flags(table.scope, table.entries); err != nil {
			return err
		}
	}
	groups := make(map[string]bool)
	for _, group := range saved.RemovedRecords {
		scope := javaDatabaseScope
		if group.Key == recordDatabaseRemovedKey {
			scope = recordDatabaseScope
		} else if group.Key != javaDatabaseRemovedKey {
			return fmt.Errorf("KTF record removal group is unknown")
		}
		if groups[group.Key] {
			return fmt.Errorf("KTF record removal group is duplicated")
		}
		groups[group.Key] = true
		if err := flags(scope, group.Flags); err != nil {
			return err
		}
	}
	return nil
}

// A detached storage replacement. Constructing it does not access a Host save
// store; the eventual session transaction owns durable save replacement.
type restoredRuntimeStorage struct {
	files                                    map[string]*runtimeCFile
	fileHandles                              map[uint32]*runtimeCFileHandle
	records                                  map[string]*runtimeRecordDatabase
	recordHandles                            map[uint32]*runtimeRecordDatabaseHandle
	nextFile, nextRecord                     uint32
	guestFiles                               map[string][]byte
	removedFiles, removedCFiles, directories map[string]bool
	removedRecords                           map[string]map[string]bool
}

func restoreStorageState(saved runtimeStorageState) (restoredRuntimeStorage, error) {
	if err := saved.validate(); err != nil {
		return restoredRuntimeStorage{}, err
	}
	state := restoredRuntimeStorage{files: make(map[string]*runtimeCFile), fileHandles: make(map[uint32]*runtimeCFileHandle), records: make(map[string]*runtimeRecordDatabase), recordHandles: make(map[uint32]*runtimeRecordDatabaseHandle), nextFile: saved.NextFileHandle, nextRecord: saved.NextRecordHandle}
	files := make([]*runtimeCFile, len(saved.Files)+1)
	for i, record := range saved.Files {
		file := &runtimeCFile{name: string(record.Name), data: bytes.Clone(record.Data), packaged: int(record.Packaged)}
		files[i+1] = file
		if record.Catalog {
			state.files[file.name] = file
		}
	}
	for _, handle := range saved.FileHandles {
		state.fileHandles[handle.Handle] = &runtimeCFileHandle{store: files[handle.Store], position: int(handle.Position)}
	}
	records := make([]*runtimeRecordDatabase, len(saved.Records)+1)
	for i, record := range saved.Records {
		store := &runtimeRecordDatabase{name: string(record.Name), records: cloneHeapRecords(record.Records), recordSize: record.RecordSize}
		records[i+1] = store
		if record.Catalog {
			state.records[store.name] = store
		}
	}
	for _, handle := range saved.RecordHandles {
		state.recordHandles[handle.Handle] = &runtimeRecordDatabaseHandle{store: records[handle.Store]}
	}
	if saved.GuestFiles != nil {
		state.guestFiles = make(map[string][]byte, len(saved.GuestFiles))
	}
	for _, file := range saved.GuestFiles {
		state.guestFiles[string(file.Name)] = bytes.Clone(file.Data)
	}
	flags := func(entries []storageFlagSnapshot) map[string]bool {
		if entries == nil {
			return nil
		}
		result := make(map[string]bool, len(entries))
		for _, entry := range entries {
			result[string(entry.Name)] = entry.Value
		}
		return result
	}
	state.removedFiles, state.removedCFiles, state.directories = flags(saved.RemovedFiles), flags(saved.RemovedCFiles), flags(saved.Directories)
	if saved.RemovedRecords != nil {
		state.removedRecords = make(map[string]map[string]bool, len(saved.RemovedRecords))
	}
	for _, group := range saved.RemovedRecords {
		state.removedRecords[group.Key] = flags(group.Flags)
	}
	return state, nil
}

func (state restoredRuntimeStorage) adopt(runtime *initializationRuntime) {
	runtime.cFiles, runtime.cFileHandles, runtime.nextCDatabaseHandle = state.files, state.fileHandles, state.nextFile
	runtime.recordDatabases, runtime.recordDatabaseHandles, runtime.nextRecordDatabaseHandle = state.records, state.recordHandles, state.nextRecord
	runtime.guestFiles = state.guestFiles
	runtime.removedFiles, runtime.removedCDatabases, runtime.madeDirectories = state.removedFiles, state.removedCFiles, state.directories
	runtime.removedDatabaseLists = state.removedRecords
}
