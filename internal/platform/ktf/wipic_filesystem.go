package ktf

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// **This file is WIPI C table 7, which is the filesystem.** Every slot in it
// sits where the specification's `MC_fs*` section prints one — open, read,
// write, close and seek at 0 to 4, the file attributes at 5, remove at 6,
// mkdir at 8, the free space at 12, `MC_fsTell` at 15 and `MC_fsIsExist` at 16
// — and a title's own log names the call it makes here as `MC_fsOpen`. See
// docs/ktf.md, "The two storage tables".
//
// **Two things deliberately kept the older name.** The save keys begin `db/`
// and are a player's own files, so renaming them would orphan every save
// already written; and the diagnostic counter names are what a stored report
// is read against. Both are records rather than code, and the code is what
// moved.
//
// runtimeCFile keeps one named file in process memory: read and written
// through a per-handle byte cursor. Durable persistence joins with the Host
// save path.
type runtimeCFile struct {
	name string
	data []byte
	// packaged is how many bytes of this store the archive shipped. The
	// archive's own data is not storage the player consumed — on a handset it
	// arrives with the program — so it does not count against the save budget
	// MC_fsAvailable reports. Without this a title that packages its maps
	// and its text as databases reads its own content as a full disk: one
	// here ships 2 MB that way and could not start a new game, because the
	// budget was gone before the player had saved anything.
	packaged int
}

// persist writes the record through the Host save store when one is attached.
func (store *runtimeCFile) persist(runtime *initializationRuntime) {
	runtime.storeSave("db/"+store.name, store.data)
}

type runtimeCFileHandle struct {
	store    *runtimeCFile
	position int
}

const (
	maxCFileBytes = 4 << 20
	// cFileHandleBit tags a file handle. It has to stay inside
	// a signed 16-bit value: a handset's handle is small, and a title that
	// keeps one in a `short` sign-extends what the open returned before it
	// hands it back. One local title does exactly that — `lsl #16; asr #16` on
	// the result, then a "negative means it failed" test — so a handle with a
	// high bit set came back as a number this platform had never issued. The
	// reads on it were refused, silently, leaving the header buffer untouched;
	// the title then sized an allocation from the difference of two words that
	// were never written and asked for four gigabytes.
	cFileHandleBit  = 0x1000
	maxCFileHandles = 0x0fff
)

const (
	wipicFileOpen         = 0
	wipicFileStreamRead   = 1
	wipicFileStreamWrite  = 2
	wipicFileClose        = 3
	wipicFileSelectRecord = 4
	wipicFileStatByName   = 5
	// MC_fsRemove takes the file's name and its area, not a handle: the caller
	// closes the file first and then names it. One title deletes the
	// certificate it could not renew and expects the next question about it to
	// answer "no such file".
	wipicFileDelete = 6
	// MC_fsMkDir takes a name and an access area — 1 is the program's own
	// directory, which is the only area this platform has. **Nothing here
	// holds a directory**: a name is a key in one flat store, so making one is
	// the whole of what a directory is, and a name that has been made is what
	// says it exists.
	wipicFileMakeDirectory = 8
	wipicFileNumRecords    = 10
	wipicFileRecordSize    = 11
	wipicFileList          = 12
	wipicFileTouchStream   = 15
	wipicFileExists        = 16

	// wipicFileStorageLimit mirrors the reference KTF per-game storage budget;
	// MC_fsAvailable reports the space still available.
	wipicFileStorageLimit = 1024 * 1024
)

func (runtime *initializationRuntime) handleWIPICFileCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case wipicFileOpen:
		return runtime.wipicFileOpen(thread)
	case wipicFileStreamRead:
		return runtime.wipicFileStream(thread, false)
	case wipicFileStreamWrite:
		return runtime.wipicFileStream(thread, true)
	case wipicFileClose:
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		delete(runtime.cFileHandles, handle)
		return 0, nil
	case wipicFileSelectRecord:
		return runtime.wipicFileSeek(thread)
	case wipicFileDelete:
		return runtime.wipicFileDelete(thread)
	case wipicFileStatByName:
		return runtime.wipicFileStatByName(thread)
	case wipicFileNumRecords:
		state, err := runtime.wipicFileHandle(thread)
		if err != nil {
			return wipicErrorInvalid, nil
		}
		if len(state.store.data) == 0 {
			return 0, nil
		}
		return 1, nil
	case wipicFileRecordSize:
		state, err := runtime.wipicFileHandle(thread)
		if err != nil {
			return wipicErrorInvalid, nil
		}
		return uint32(len(state.store.data)), nil
	case wipicFileList:
		// MC_fsAvailable reports remaining storage: the budget minus every
		// open store's bytes, floored at zero like the reference repository usage.
		used := 0
		for _, store := range runtime.cFiles {
			if grown := len(store.data) - store.packaged; grown > 0 {
				used += grown
			}
		}
		if used >= wipicFileStorageLimit {
			return 0, nil
		}
		return uint32(wipicFileStorageLimit - used), nil
	case wipicFileExists:
		// MC_fsIsExist answers 0 on success and M_E_NOENT when missing;
		// titles branch to their fresh-init path on the error code. A
		// directory this platform was asked to make counts as something that
		// exists, because the alternative is telling a title that the
		// directory it just made is not there.
		nameAddress, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		name, err := runtime.readCString(nameAddress, 512)
		if err != nil {
			return 0, fmt.Errorf("read KTF file name: %w", err)
		}
		_, exists := runtime.cFiles[name]
		_, seeded := runtime.databaseSeed(name)
		made := runtime.createdDirectories()[name]
		runtime.countDiagnostic(fmt.Sprintf("cdb exists %s -> %t", name, exists || seeded || made))
		if exists || seeded || made {
			return 0, nil
		}
		return wipicErrorNotFound, nil
	case wipicFileMakeDirectory:
		return runtime.wipicMakeDirectory(thread)
	case wipicFileTouchStream:
		return runtime.wipicFileTouch(thread)
	default:
		// The call site is named for the same reason the unimplemented-table
		// error names one: the function number is an index into an array of
		// pointers, so it never appears in the guest's own code, and an
		// address is the only thing about a missing call that can be
		// disassembled.
		return 0, fmt.Errorf("KTF WIPI C filesystem function %d is not implemented%s",
			function, runtime.callerSite(thread))
	}
}

// wipicFileDelete serves MC_fsRemove(name, mode). A store may be
// in memory, persisted, packaged with the archive, or all three, so the delete
// has to cover every place the open would look. The persisted copy cannot be
// unlinked — the save store has no delete — so the name is recorded on a
// removal list the way the guest filesystem records one, and every lookup
// consults it. Without that the database comes back on the next question, and
// a title that deleted it to start again is handed what it just threw away.
func (runtime *initializationRuntime) wipicFileDelete(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF database name: %w", err)
	}
	_, live := runtime.cFiles[name]
	_, seeded := runtime.databaseSeed(name)
	runtime.countDiagnostic(fmt.Sprintf("cdb delete %s -> %t", name, live || seeded))
	if !live && !seeded {
		return wipicErrorNotFound, nil
	}
	delete(runtime.cFiles, name)
	// Handles onto the store go with it; a caller holding one after the delete
	// would otherwise keep reading a database nothing else can see.
	for handle, open := range runtime.cFileHandles {
		if open.store != nil && open.store.name == name {
			delete(runtime.cFileHandles, handle)
		}
	}
	runtime.markDatabaseRemoved(name, true)
	runtime.storeSave("db/"+name, nil)
	return 0, nil
}

// databaseRemovedKey holds the names MC_fsRemove has removed. See
// guestFileRemovedKey for why a delete needs a list of its own.
const databaseRemovedKey = "db/.removed"

// removedDatabases is the set of deleted names, read from the store once per
// session and kept in memory after that.
func (runtime *initializationRuntime) removedDatabases() map[string]bool {
	if runtime.removedCDatabases != nil {
		return runtime.removedCDatabases
	}
	runtime.removedCDatabases = make(map[string]bool)
	if data, exists := runtime.loadSave(databaseRemovedKey); exists {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				runtime.removedCDatabases[line] = true
			}
		}
	}
	return runtime.removedCDatabases
}

// markDatabaseRemoved records or clears one name and writes the list back.
// Opening a database for creation clears it, because a store that was written
// and then still read as deleted is worse than one that was never deleted.
func (runtime *initializationRuntime) markDatabaseRemoved(name string, removed bool) {
	set := runtime.removedDatabases()
	if set[name] == removed {
		return
	}
	if removed {
		set[name] = true
	} else {
		delete(set, name)
	}
	names := make([]string, 0, len(set))
	for entry := range set {
		names = append(names, entry)
	}
	sort.Strings(names)
	runtime.storeSave(databaseRemovedKey, []byte(strings.Join(names, "\n")))
}

// wipicMakeDirectory serves MC_fsMkDir(dirName, aMode).
//
// A directory has no representation here — the store this table reads and
// writes is one flat map from a name to its bytes — so making one has nothing
// to create. What it does have is an answer to give, and the specification
// gives it two: zero for a directory that was made, `M_E_EXIST` for one that
// was already there. Answering zero every time would tell a title that checks
// the result that every run is its first, so the names that have been made are
// written down beside the names that have been deleted, in the same store and
// for the same reason: a question about storage has to be answered the same way
// after a restart as before one.
//
// The two local titles that reach this call make their own directory at
// start-up and drop the answer, so neither is what the list is for; a title
// that reads it is.
func (runtime *initializationRuntime) wipicMakeDirectory(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF directory name: %w", err)
	}
	if name == "" {
		return wipicErrorBadParam, nil
	}
	if runtime.createdDirectories()[name] {
		runtime.countDiagnostic(fmt.Sprintf("fs mkdir %s -> exists", name))
		return wipicErrorExists, nil
	}
	runtime.markDirectoryCreated(name)
	runtime.countDiagnostic(fmt.Sprintf("fs mkdir %s", name))
	return 0, nil
}

// directoryListKey holds the directory names MC_fsMkDir has made. See
// databaseRemovedKey for why a flat store needs a list of its own.
const directoryListKey = "db/.dirs"

// createdDirectories is the set of made names, read from the store once per
// session and kept in memory after that.
func (runtime *initializationRuntime) createdDirectories() map[string]bool {
	if runtime.madeDirectories != nil {
		return runtime.madeDirectories
	}
	runtime.madeDirectories = make(map[string]bool)
	if data, exists := runtime.loadSave(directoryListKey); exists {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				runtime.madeDirectories[line] = true
			}
		}
	}
	return runtime.madeDirectories
}

// markDirectoryCreated records one name and writes the list back.
func (runtime *initializationRuntime) markDirectoryCreated(name string) {
	set := runtime.createdDirectories()
	if set[name] {
		return
	}
	set[name] = true
	names := make([]string, 0, len(set))
	for entry := range set {
		names = append(names, entry)
	}
	sort.Strings(names)
	runtime.storeSave(directoryListKey, []byte(strings.Join(names, "\n")))
}

// databaseSeed is what an open would start a store from: the persisted copy
// first, the archive's packaged copy second, and neither once the name has
// been deleted.
func (runtime *initializationRuntime) databaseSeed(name string) ([]byte, bool) {
	if runtime.removedDatabases()[name] {
		return nil, false
	}
	if saved, exists := runtime.loadSave("db/" + name); exists {
		// A persisted save takes precedence over packaged initial data.
		return saved, true
	}
	return runtime.packagedDatabase(name)
}

// packagedDatabase is the archive's own initial content for a database, which
// archives ship as a data file with the database's name. It ignores the
// removal list: create mode keeps packaged content, and that has to stay true
// of a name the game deleted and reopened.
func (runtime *initializationRuntime) packagedDatabase(name string) ([]byte, bool) {
	return runtime.guestFile(name)
}

func (runtime *initializationRuntime) wipicFileOpen(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	mode, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF database name: %w", err)
	}
	runtime.countDiagnostic(fmt.Sprintf("cdb open %s mode %d", name, int32(mode)))
	store, exists := runtime.cFiles[name]
	seed, hasSeed := runtime.databaseSeed(name)
	_, hasPackaged := runtime.packagedDatabase(name)
	if !exists {
		if int32(mode) == 1 && !hasSeed {
			return wipicErrorNotFound, nil
		}
		store = &runtimeCFile{name: name}
		store.data = append([]byte(nil), seed...)
		if packaged, ok := runtime.packagedDatabase(name); ok {
			store.packaged = len(packaged)
		}
		if runtime.cFiles == nil {
			runtime.cFiles = make(map[string]*runtimeCFile)
		}
		runtime.cFiles[name] = store
	}
	// Create mode wipes prior contents unless the database is backed by
	// packaged archive data.
	if int32(mode) == 4 && !hasPackaged {
		store.data = nil
	}
	// Opening a deleted name brings it back: the store is live again from
	// here, and a removal list that still held it would hide the writes the
	// caller is about to make.
	runtime.markDatabaseRemoved(name, false)
	if runtime.cFileHandles == nil {
		runtime.cFileHandles = make(map[uint32]*runtimeCFileHandle)
	}
	if runtime.nextCDatabaseHandle >= maxCFileHandles {
		return wipicErrorInvalid, nil
	}
	runtime.nextCDatabaseHandle++
	handle := cFileHandleBit | runtime.nextCDatabaseHandle
	runtime.cFileHandles[handle] = &runtimeCFileHandle{store: store}
	return handle, nil
}

func (runtime *initializationRuntime) wipicFileHandle(thread *armcore.Thread) (*runtimeCFileHandle, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return nil, err
	}
	state, ok := runtime.cFileHandles[handle]
	if !ok {
		return nil, fmt.Errorf("KTF database handle %#x is not open", handle)
	}
	return state, nil
}

func (runtime *initializationRuntime) wipicFileStream(thread *armcore.Thread, write bool) (uint32, error) {
	state, err := runtime.wipicFileHandle(thread)
	if err != nil {
		return wipicErrorInvalid, nil
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	length, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	if int32(length) < 0 || length > maxCFileBytes {
		return wipicErrorInvalid, nil
	}
	if write {
		data := make([]byte, length)
		if err := runtime.client.core.Memory().Read(buffer, data); err != nil {
			return 0, fmt.Errorf("read KTF database write buffer: %w", err)
		}
		end := state.position + int(length)
		if end > maxCFileBytes {
			return wipicErrorInvalid, nil
		}
		if end > len(state.store.data) {
			grown := make([]byte, end)
			copy(grown, state.store.data)
			state.store.data = grown
		}
		copy(state.store.data[state.position:], data)
		state.position = end
		state.store.persist(runtime)
		return length, nil
	}
	remaining := len(state.store.data) - state.position
	if remaining <= 0 {
		return 0, nil
	}
	count := int(length)
	if count > remaining {
		count = remaining
	}
	if err := runtime.client.core.Memory().Write(buffer, state.store.data[state.position:state.position+count]); err != nil {
		return 0, fmt.Errorf("write KTF database read buffer: %w", err)
	}
	state.position += count
	return uint32(count), nil
}

// wipicFileStatByName implements the KTF custom slot 5, db_stat_by_name:
// (name, out int32[3], mode). Titles treat a zero result with out[2] above a
// size threshold as a valid save. Missing databases answer M_E_BADRECID.
func (runtime *initializationRuntime) wipicFileStatByName(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	outAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF database name: %w", err)
	}
	size := uint32(0)
	if store, ok := runtime.cFiles[name]; ok {
		size = uint32(len(store.data))
	} else if saved, ok := runtime.loadSave("db/" + name); ok {
		size = uint32(len(saved))
	} else if packaged, ok := runtime.guestFile(name); ok {
		size = uint32(len(packaged))
	} else {
		runtime.countDiagnostic(fmt.Sprintf("cdb stat %s -> missing", name))
		return wipicErrorBadParam, nil
	}
	runtime.countDiagnostic(fmt.Sprintf("cdb stat %s -> %d", name, size))
	if outAddress != 0 {
		record := make([]byte, 12)
		binary.LittleEndian.PutUint32(record[8:], size)
		if err := runtime.client.core.Memory().Write(outAddress, record); err != nil {
			return 0, fmt.Errorf("write KTF database stat: %w", err)
		}
	}
	return 0, nil
}

// wipicFileTouch implements the KTF custom slot 15, which the original
// runtime's own emulator never named either. One local title calls it, from
// its save loader:
//
//	handle = open(name, read, 1)
//	size   = seek(handle, 0, SEEK_END)
//	if size > 0 { slot15(handle); seek(handle, 0, SEEK_SET) }
//
// The call takes the handle alone — the disassembly reaches it through the
// `bx r2` trampoline with only r0 set — and the caller drops the result into a
// scratch local that the following seek overwrites before anything reads it.
// The cursor is reset immediately afterwards too, so any positioning this
// performs is unobservable. That leaves an open stream to validate and nothing
// to do, which is what this answers; an unknown handle is rejected the way the
// other handle-taking slots reject one.
func (runtime *initializationRuntime) wipicFileTouch(thread *armcore.Thread) (uint32, error) {
	state, err := runtime.wipicFileHandle(thread)
	if err != nil {
		return wipicErrorInvalid, nil
	}
	// A second title calling this with a different shape would need the real
	// semantics, so record that it happened rather than passing silently.
	runtime.countDiagnostic(fmt.Sprintf("cdb slot15 %s", state.store.name))
	return 0, nil
}

// wipicFileSeek implements the KTF select-record extension, a seek over
// the open record's byte cursor.
func (runtime *initializationRuntime) wipicFileSeek(thread *armcore.Thread) (uint32, error) {
	state, err := runtime.wipicFileHandle(thread)
	if err != nil {
		return wipicErrorInvalid, nil
	}
	offset, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	origin, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	base := 0
	switch origin {
	case 0:
		base = 0
	case 1:
		base = state.position
	case 2:
		base = len(state.store.data)
	default:
		return wipicErrorInvalid, nil
	}
	position := base + int(int32(offset))
	if position < 0 {
		position = 0
	}
	if position > len(state.store.data) {
		position = len(state.store.data)
	}
	state.position = position
	return uint32(position), nil
}
