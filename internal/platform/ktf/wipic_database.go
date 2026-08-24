package ktf

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// runtimeCDatabase keeps one named WIPI C database record in process memory.
// KTF clients use the stream extension: one record per database, read and
// written through a per-handle byte cursor. Durable persistence joins with
// the Host save path.
type runtimeCDatabase struct {
	name string
	data []byte
	// packaged is how many bytes of this store the archive shipped. The
	// archive's own data is not storage the player consumed — on a handset it
	// arrives with the program — so it does not count against the save budget
	// MC_dbListDataBase reports. Without this a title that packages its maps
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
	// cDatabaseHandleBit tags a stream database handle. It has to stay inside
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
	// MC_dbDeleteDataBase takes the database's name and its mode, not a
	// handle: the caller closes the database first and then names it. One
	// title deletes the certificate it could not renew and expects the next
	// question about it to answer "no such database".
	wipicDatabaseDelete      = 6
	wipicDatabaseNumRecords  = 10
	wipicDatabaseRecordSize  = 11
	wipicDatabaseList        = 12
	wipicDatabaseTouchStream = 15
	wipicDatabaseExists      = 16

	// wipicDatabaseStorageLimit mirrors the reference KTF per-game storage budget;
	// MC_dbListDataBase reports the space still available.
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
		// MC_dbListDataBase reports remaining storage: the budget minus every
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
		// MC_dbExists answers 0 on success and M_E_NOENT when missing;
		// titles branch to their fresh-init path on the error code.
		nameAddress, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		name, err := runtime.readCString(nameAddress, 512)
		if err != nil {
			return 0, fmt.Errorf("read KTF database name: %w", err)
		}
		_, exists := runtime.cDatabases[name]
		_, seeded := runtime.databaseSeed(name)
		runtime.countDiagnostic(fmt.Sprintf("cdb exists %s -> %t", name, exists || seeded))
		if exists || seeded {
			return 0, nil
		}
		return wipicErrorNotFound, nil
	case wipicDatabaseTouchStream:
		return runtime.wipicDatabaseTouch(thread)
	default:
		// The call site is named for the same reason the unimplemented-table
		// error names one: the function number is an index into an array of
		// pointers, so it never appears in the guest's own code, and an
		// address is the only thing about a missing call that can be
		// disassembled.
		return 0, fmt.Errorf("KTF WIPI C database function %d is not implemented%s",
			function, runtime.callerSite(thread))
	}
}

// wipicDatabaseDelete serves MC_dbDeleteDataBase(name, mode). A store may be
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

// databaseRemovedKey holds the names MC_dbDeleteDataBase has removed. See
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
