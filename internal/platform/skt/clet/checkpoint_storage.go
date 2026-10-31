package clet

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// Quick load and storage
//
// A checkpoint is the program's execution state and nothing else. The slot
// carries no save and a load replaces none: what the program saved after the
// checkpoint is still there after a load, and the restored program reads and
// writes its files as the store has them then.
//
// The file table keeps a host copy of each file the program has open, and
// every write stores that copy whole before the call that made it returns.
// So nothing is waiting for the store when a checkpoint is taken, and a copy
// brought back from one would answer the restored program with what the file
// held then — and its next write would put that over the file as it is now.
//
// A checkpoint therefore brings back names: which files were open, under which
// handles, at which cursor. Committing the load fills each of them from the
// store through the lookup a first open of that name makes, and from there
// every host copy equals the store again. The load itself writes nothing. The
// rules are the KTF file table's, which this table is:
//
//   - A cursor stays where the record has it, even past the end of a file
//     that is shorter now. A read there answers end of file and a write fills
//     the gap with zeros.
//   - A name the store has nothing for leaves its file empty, and nothing is
//     made. A handle that holds it stays valid, so the program's next write
//     through it makes the name again; the empty file is kept by name, so an
//     open, a rename or a write of the name takes it and the name never has
//     two (detachedCFiles).
//   - The removal and directory lists are read again, so a store that cannot
//     be read refuses the load rather than the program's first call after it.
//   - What is read is bounded: the names come from a slot, which is untrusted.

// storageState is what a checkpoint keeps of the file table.
type storageState struct {
	Files          []fileSnapshot
	FileHandles    []fileHandleSnapshot
	NextFileHandle uint32
}

// fileSnapshot is one file by name. Catalog says the table lists it; a file
// that is only held by a handle is one a load left without a file.
type fileSnapshot struct {
	Name    []byte
	Catalog bool
}

type fileHandleSnapshot struct {
	Handle, Store uint32
	Position      int32
}

func (runtime *initializationRuntime) captureStorageState() (storageState, error) {
	saved := storageState{NextFileHandle: runtime.NextCDatabaseHandle}
	if len(runtime.CFiles) > 1<<16 || len(runtime.CFileHandles) > wipicrt.MaxCFileHandles {
		return storageState{}, fmt.Errorf("SKT Clet file table exceeds its limits")
	}
	var bytesHeld uint64
	ids := make(map[*wipicrt.RuntimeCFile]uint32)
	id := func(file *wipicrt.RuntimeCFile) (uint32, error) {
		if file == nil {
			return 0, fmt.Errorf("SKT Clet file is nil")
		}
		if known := ids[file]; known != 0 {
			return known, nil
		}
		if len(file.Name) > wipicrt.MaxFileNameRead || len(saved.Files) >= 1<<16 {
			return 0, fmt.Errorf("SKT Clet file exceeds the checkpoint limits")
		}
		// A load reads back what these files hold, so what they hold now is
		// charged now, and a checkpoint no load would accept is not taken.
		bytesHeld += uint64(len(file.Data))
		if bytesHeld > maxCheckpointStorageBytes {
			return 0, fmt.Errorf("the files the program has open hold more than the %d bytes a load restores", maxCheckpointStorageBytes)
		}
		saved.Files = append(saved.Files, fileSnapshot{Name: []byte(file.Name)})
		ids[file] = uint32(len(saved.Files))
		return ids[file], nil
	}
	// Map order would make two captures of one session differ.
	for _, name := range sortedKeys(runtime.CFiles) {
		file := runtime.CFiles[name]
		if file == nil || name != file.Name {
			return storageState{}, fmt.Errorf("SKT Clet file table differs from its file names")
		}
		known, err := id(file)
		if err != nil {
			return storageState{}, err
		}
		saved.Files[known-1].Catalog = true
	}
	for _, handle := range sortedKeys(runtime.CFileHandles) {
		open := runtime.CFileHandles[handle]
		if open == nil || open.Position < 0 || open.Position > wipicrt.MaxCFileBytes {
			return storageState{}, fmt.Errorf("SKT Clet open file cursor is invalid")
		}
		known, err := id(open.Store)
		if err != nil {
			return storageState{}, err
		}
		saved.FileHandles = append(saved.FileHandles, fileHandleSnapshot{Handle: handle, Store: known, Position: int32(open.Position)})
	}
	if err := saved.validate(); err != nil {
		return storageState{}, err
	}
	return saved, nil
}

func (saved storageState) validate() error {
	if len(saved.Files) > 1<<16 || len(saved.FileHandles) > wipicrt.MaxCFileHandles || saved.NextFileHandle > wipicrt.MaxCFileHandles {
		return fmt.Errorf("SKT Clet file table record exceeds its limits")
	}
	catalog := make(map[string]bool)
	for _, file := range saved.Files {
		if len(file.Name) > wipicrt.MaxFileNameRead || !wipicrt.CFileNameKeyed(string(file.Name)) || wipicrt.ReservedStorageName(wipicrt.CFileScope, string(file.Name)) ||
			file.Catalog && catalog[string(file.Name)] {
			return fmt.Errorf("SKT Clet file table record names an invalid file")
		}
		if file.Catalog {
			catalog[string(file.Name)] = true
		}
	}
	used := make(map[uint32]bool)
	held := make([]bool, len(saved.Files)+1)
	for _, handle := range saved.FileHandles {
		if handle.Handle <= wipicrt.CFileHandleBit || handle.Handle > wipicrt.CFileHandleBit|saved.NextFileHandle || handle.Handle&^uint32(wipicrt.MaxCFileHandles) != wipicrt.CFileHandleBit ||
			used[handle.Handle] || handle.Store == 0 || uint64(handle.Store) > uint64(len(saved.Files)) || handle.Position < 0 || handle.Position > wipicrt.MaxCFileBytes {
			return fmt.Errorf("SKT Clet file handle record is invalid")
		}
		used[handle.Handle], held[handle.Store] = true, true
	}
	// A file is in a record because the table or a handle holds it, and every
	// file is a read when the load commits.
	for index, file := range saved.Files {
		if !file.Catalog && !held[index+1] {
			return fmt.Errorf("SKT Clet file record is held by nothing")
		}
	}
	return nil
}

// restoredStorage is every file a checkpoint named, once, with nothing in it
// yet: what a store fills when the load commits.
type restoredStorage struct {
	files []*wipicrt.RuntimeCFile
}

// restoreStorageState builds the file table a record names, every file empty
// under its handles. It reaches no store.
func (runtime *initializationRuntime) restoreStorageState(saved storageState) (*restoredStorage, error) {
	if err := saved.validate(); err != nil {
		return nil, err
	}
	restored := &restoredStorage{}
	// One name is one file: a record can name one twice, in the table and
	// under a handle that outlived it, and both are the same file once read.
	named := make(map[string]*wipicrt.RuntimeCFile, len(saved.Files))
	files := make([]*wipicrt.RuntimeCFile, len(saved.Files)+1)
	for index, record := range saved.Files {
		file := named[string(record.Name)]
		if file == nil {
			file = &wipicrt.RuntimeCFile{Name: string(record.Name)}
			named[file.Name] = file
			restored.files = append(restored.files, file)
		}
		files[index+1] = file
		if record.Catalog {
			runtime.CFiles[file.Name] = file
		}
	}
	for _, handle := range saved.FileHandles {
		runtime.CFileHandles[handle.Handle] = &wipicrt.RuntimeCFileHandle{Store: files[handle.Store], Position: int(handle.Position)}
	}
	runtime.NextCDatabaseHandle = saved.NextFileHandle
	return restored, nil
}

// bindRestoredStorage fills the files a checkpoint named from the store the
// restored program will run over, and attaches the program to it. Only reads
// are made, each key once and on a budget. On a failure the client is left on
// the store it had, still to be bound, and the error says the saves could not
// be read; nothing of the store or of a running session has changed.
func (client *Client) bindRestoredStorage(restored *restoredStorage, live wipicrt.SaveStore) error {
	reader := backend.NewRebuildReader(live, maxCheckpointStorageBytes)
	unbound := client.SaveStore
	client.SaveStore = reader
	err := client.runtime.rebindRestoredStorage(restored)
	if err == nil {
		err = reader.Err()
	}
	if err != nil {
		client.SaveStore = unbound
		return fmt.Errorf("%w: %v", backend.ErrCheckpointSaveRead, err)
	}
	client.SaveStore = live
	return nil
}

// rebindRestoredStorage starts from the names every time, so a second attempt
// after a failed one is the first attempt again.
func (runtime *initializationRuntime) rebindRestoredStorage(restored *restoredStorage) error {
	runtime.SaveReadError = nil
	runtime.RemovedFiles, runtime.RemovedCDatabases, runtime.MadeDirectories = nil, nil, nil
	runtime.CFiles, runtime.DetachedCFiles = make(map[string]*wipicrt.RuntimeCFile), make(map[string]*wipicrt.RuntimeCFile)
	failed := func(err error) error {
		// A failed read must not outlive the attempt: the runtime keeps one
		// for the life of a session and refuses every later write.
		runtime.SaveReadError = nil
		return err
	}
	if restored == nil {
		return nil
	}
	kept := uint64(0)
	// Every list is read here, the ones no lookup below asks for among them:
	// a read that fails stops every storage call after it, which has to
	// refuse the load and not meet the program at its first call.
	runtime.RemovedGuestFiles()
	runtime.RemovedDatabases()
	runtime.CreatedDirectories()
	if runtime.SaveReadError != nil {
		return failed(runtime.SaveReadError)
	}
	held := make(map[*wipicrt.RuntimeCFile]bool, len(runtime.CFileHandles))
	for _, open := range runtime.CFileHandles {
		held[open.Store] = true
	}
	for _, file := range restored.files {
		seed, found := runtime.DatabaseSeed(file.Name)
		file.Data, file.Packaged = nil, 0
		if packaged, shipped := runtime.PackagedDatabase(file.Name); shipped {
			file.Packaged = len(packaged)
		}
		if runtime.SaveReadError != nil {
			return failed(runtime.SaveReadError)
		}
		if !found {
			if held[file] {
				runtime.DetachedCFiles[file.Name] = file
			}
			continue
		}
		if uint64(len(seed)) > maxCheckpointStorageBytes-kept {
			return failed(fmt.Errorf("the files the checkpoint has open hold more than the %d bytes a load restores", maxCheckpointStorageBytes))
		}
		kept += uint64(len(seed))
		file.Data = bytes.Clone(seed)
		runtime.CFiles[file.Name] = file
	}
	return nil
}
