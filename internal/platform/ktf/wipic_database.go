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
// **The names below say database because that is what the table was read as,
// and they have not been changed yet.** What they name is right either way: a
// file here is one blob per name addressed by a cursor, which is exactly the
// store this keeps. Two things would move if the identifiers did — the save
// keys, which begin `db/` and are a player's own files, and the diagnostic
// counter names, which a report is read against — so the rename is a change of
// its own rather than part of the one that found this out. It is in TODO.md.
//
// runtimeCDatabase keeps one named file in process memory: read and written
// through a per-handle byte cursor. Durable persistence joins with the Host
// save path.
type runtimeCDatabase struct {
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
func (store *runtimeCDatabase) persist(runtime *initializationRuntime) {
	runtime.storeSave("db/"+store.name, store.data)
}

type runtimeCDatabaseHandle struct {
	store    *runtimeCDatabase
	position int
}

const (
	maxCDatabaseBytes = 4 << 20
	// cDatabaseHandleBit tags a file handle. It has to stay inside
	// a signed 16-bit value: a handset's handle is small, and a title that
	// keeps one in a `short` sign-extends what the open returned before it
	// hands it back. One local title does exactly that — `lsl #16; asr #16` on
	// the result, then a "negative means it failed" test — so a handle with a
	// high bit set came back as a number this platform had never issued. The
	// reads on it were refused, silently, leaving the header buffer untouched;
	// the title then sized an allocation from the difference of two words that
	// were never written and asked for four gigabytes.
	cDatabaseHandleBit  = 0x1000
	maxCDatabaseHandles = 0x0fff
)

const (
	wipicDatabaseOpen         = 0
	wipicDatabaseStreamRead   = 1
	wipicDatabaseStreamWrite  = 2
	wipicDatabaseClose        = 3
	wipicDatabaseSelectRecord = 4
	wipicDatabaseStatByName   = 5
	// MC_fsRemove takes the file's name and its area, not a handle: the caller
	// closes the file first and then names it. One title deletes the
	// certificate it could not renew and expects the next question about it to
	// answer "no such file".
	wipicDatabaseDelete = 6
	// MC_fsMkDir takes a name and an access area — 1 is the program's own
	// directory, which is the only area this platform has. **Nothing here
	// holds a directory**: a name is a key in one flat store, so making one is
	// the whole of what a directory is, and a name that has been made is what
	// says it exists.
	wipicDatabaseMakeDirectory = 8
	wipicDatabaseNumRecords    = 10
	wipicDatabaseRecordSize    = 11
	wipicDatabaseList          = 12
	wipicDatabaseTouchStream   = 15
	wipicDatabaseExists        = 16

	// wipicDatabaseStorageLimit mirrors the reference KTF per-game storage budget;
	// MC_fsAvailable reports the space still available.
	wipicDatabaseStorageLimit = 1024 * 1024
)

func (runtime *initializationRuntime) handleWIPICDatabaseCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case wipicDatabaseOpen:
		return runtime.wipicDatabaseOpen(thread)
	case wipicDatabaseStreamRead:
		return runtime.wipicDatabaseStream(thread, false)
	case wipicDatabaseStreamWrite:
		return runtime.wipicDatabaseStream(thread, true)
	case wipicDatabaseClose:
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		delete(runtime.cDatabaseHandles, handle)
		return 0, nil
	case wipicDatabaseSelectRecord:
		return runtime.wipicDatabaseSeek(thread)
	case wipicDatabaseDelete:
		return runtime.wipicDatabaseDelete(thread)
	case wipicDatabaseStatByName:
		return runtime.wipicDatabaseStatByName(thread)
	case wipicDatabaseNumRecords:
		state, err := runtime.wipicDatabaseHandle(thread)
		if err != nil {
			return wipicErrorInvalid, nil
		}
		if len(state.store.data) == 0 {
			return 0, nil
		}
		return 1, nil
	case wipicDatabaseRecordSize:
		state, err := runtime.wipicDatabaseHandle(thread)
		if err != nil {
			return wipicErrorInvalid, nil
		}
		return uint32(len(state.store.data)), nil
	case wipicDatabaseList:
		// MC_fsAvailable reports remaining storage: the budget minus every
		// open store's bytes, floored at zero like the reference repository usage.
		used := 0
		for _, store := range runtime.cDatabases {
			if grown := len(store.data) - store.packaged; grown > 0 {
				used += grown
			}
		}
		if used >= wipicDatabaseStorageLimit {
			return 0, nil
		}
		return uint32(wipicDatabaseStorageLimit - used), nil
	case wipicDatabaseExists:
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
		_, exists := runtime.cDatabases[name]
		_, seeded := runtime.databaseSeed(name)
		made := runtime.createdDirectories()[name]
		runtime.countDiagnostic(fmt.Sprintf("cdb exists %s -> %t", name, exists || seeded || made))
		if exists || seeded || made {
			return 0, nil
		}
		return wipicErrorNotFound, nil
	case wipicDatabaseMakeDirectory:
		return runtime.wipicMakeDirectory(thread)
	case wipicDatabaseTouchStream:
		return runtime.wipicDatabaseTouch(thread)
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

// wipicDatabaseDelete serves MC_fsRemove(name, mode). A store may be
// in memory, persisted, packaged with the archive, or all three, so the delete
// has to cover every place the open would look. The persisted copy cannot be
// unlinked — the save store has no delete — so the name is recorded on a
// removal list the way the guest filesystem records one, and every lookup
// consults it. Without that the database comes back on the next question, and
// a title that deleted it to start again is handed what it just threw away.
func (runtime *initializationRuntime) wipicDatabaseDelete(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF database name: %w", err)
	}
	_, live := runtime.cDatabases[name]
	_, seeded := runtime.databaseSeed(name)
	runtime.countDiagnostic(fmt.Sprintf("cdb delete %s -> %t", name, live || seeded))
	if !live && !seeded {
		return wipicErrorNotFound, nil
	}
	delete(runtime.cDatabases, name)
	// Handles onto the store go with it; a caller holding one after the delete
	// would otherwise keep reading a database nothing else can see.
	for handle, open := range runtime.cDatabaseHandles {
		if open.store != nil && open.store.name == name {
			delete(runtime.cDatabaseHandles, handle)
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

func (runtime *initializationRuntime) wipicDatabaseOpen(thread *armcore.Thread) (uint32, error) {
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
	store, exists := runtime.cDatabases[name]
	seed, hasSeed := runtime.databaseSeed(name)
	_, hasPackaged := runtime.packagedDatabase(name)
	if !exists {
		if int32(mode) == 1 && !hasSeed {
			return wipicErrorNotFound, nil
		}
		store = &runtimeCDatabase{name: name}
		store.data = append([]byte(nil), seed...)
		if packaged, ok := runtime.packagedDatabase(name); ok {
			store.packaged = len(packaged)
		}
		if runtime.cDatabases == nil {
			runtime.cDatabases = make(map[string]*runtimeCDatabase)
		}
		runtime.cDatabases[name] = store
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
	if runtime.cDatabaseHandles == nil {
		runtime.cDatabaseHandles = make(map[uint32]*runtimeCDatabaseHandle)
	}
	if runtime.nextCDatabaseHandle >= maxCDatabaseHandles {
		return wipicErrorInvalid, nil
	}
	runtime.nextCDatabaseHandle++
	handle := cDatabaseHandleBit | runtime.nextCDatabaseHandle
	runtime.cDatabaseHandles[handle] = &runtimeCDatabaseHandle{store: store}
	return handle, nil
}

func (runtime *initializationRuntime) wipicDatabaseHandle(thread *armcore.Thread) (*runtimeCDatabaseHandle, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return nil, err
	}
	state, ok := runtime.cDatabaseHandles[handle]
	if !ok {
		return nil, fmt.Errorf("KTF database handle %#x is not open", handle)
	}
	return state, nil
}

func (runtime *initializationRuntime) wipicDatabaseStream(thread *armcore.Thread, write bool) (uint32, error) {
	state, err := runtime.wipicDatabaseHandle(thread)
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
	if int32(length) < 0 || length > maxCDatabaseBytes {
		return wipicErrorInvalid, nil
	}
	if write {
		data := make([]byte, length)
		if err := runtime.client.core.Memory().Read(buffer, data); err != nil {
			return 0, fmt.Errorf("read KTF database write buffer: %w", err)
		}
		end := state.position + int(length)
		if end > maxCDatabaseBytes {
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

// wipicDatabaseStatByName implements the KTF custom slot 5, db_stat_by_name:
// (name, out int32[3], mode). Titles treat a zero result with out[2] above a
// size threshold as a valid save. Missing databases answer M_E_BADRECID.
func (runtime *initializationRuntime) wipicDatabaseStatByName(thread *armcore.Thread) (uint32, error) {
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
	if store, ok := runtime.cDatabases[name]; ok {
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

// wipicDatabaseTouch implements the KTF custom slot 15, which the original
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
func (runtime *initializationRuntime) wipicDatabaseTouch(thread *armcore.Thread) (uint32, error) {
	state, err := runtime.wipicDatabaseHandle(thread)
	if err != nil {
		return wipicErrorInvalid, nil
	}
	// A second title calling this with a different shape would need the real
	// semantics, so record that it happened rather than passing silently.
	runtime.countDiagnostic(fmt.Sprintf("cdb slot15 %s", state.store.name))
	return 0, nil
}

// wipicDatabaseSeek implements the KTF select-record extension, a seek over
// the open record's byte cursor.
func (runtime *initializationRuntime) wipicDatabaseSeek(thread *armcore.Thread) (uint32, error) {
	state, err := runtime.wipicDatabaseHandle(thread)
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
