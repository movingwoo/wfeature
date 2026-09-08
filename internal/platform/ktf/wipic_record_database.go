package ktf

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// The record database is the other half of the WIPI C storage interface. Where
// the stream database in wipic_filesystem.go is one blob read and written through
// a cursor, this one is a numbered set of records, and a game reaches for it
// when it wants to address entries rather than offsets.
//
// It has to be here rather than stubbed because a stub is not neutral. A game
// that cannot open a record database does not stop: it carries on with whatever
// the read left in its buffer, and a buffer it allocated is zeros. One title
// keeps its cipher keys in a packaged record database and decrypts its settings
// with them, so a stub turned every setting into a zero — and its frame
// interval, chosen from a table by that zero, into the slowest entry. The game
// then ran at two frames a second with nothing failing anywhere.

// runtimeRecordDatabase is one named record database. Records are addressed by
// a one-based id, and a deleted record keeps its slot: ids are handed out by
// position and reusing one would rename another game's record.
type runtimeRecordDatabase struct {
	name    string
	records [][]byte
	// recordSize is what the database was created with. It is reported back
	// and nothing else: the local titles write fixed-size records and never
	// ask this platform to enforce it.
	recordSize uint32
}

type runtimeRecordDatabaseHandle struct {
	store *runtimeRecordDatabase
}

const (
	// recordDatabaseHandleBit tags a record database handle. It is a different
	// tag from the stream database's so that a handle passed to the wrong
	// table is rejected rather than silently addressing another store, and
	// both stay inside a signed 16-bit value because a title may keep what an
	// open returned in a `short`. See cFileHandleBit for what a handle
	// that does not survive that costs.
	recordDatabaseHandleBit  = 0x2000
	maxRecordDatabaseHandles = 0x0fff
	maxRecordDatabaseBytes   = 4 << 20
	maxRecordDatabaseName    = 31
)

const (
	wipicRecordDatabaseOpen       = 0
	wipicRecordDatabaseClose      = 1
	wipicRecordDatabaseDelete     = 2
	wipicRecordDatabaseInsert     = 3
	wipicRecordDatabaseSelect     = 4
	wipicRecordDatabaseUpdate     = 5
	wipicRecordDatabaseDeleteRec  = 6
	wipicRecordDatabaseList       = 7
	wipicRecordDatabaseNumRecords = 10
	wipicRecordDatabaseRecordSize = 11
	wipicRecordDatabaseListNames  = 12
)

func (runtime *initializationRuntime) handleWIPICRecordDatabaseCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case wipicRecordDatabaseOpen:
		return runtime.wipicRecordDatabaseOpen(thread)
	case wipicRecordDatabaseClose:
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		if _, ok := runtime.recordDatabaseHandles[handle]; !ok {
			return wipicErrorInvalid, nil
		}
		delete(runtime.recordDatabaseHandles, handle)
		return 0, nil
	case wipicRecordDatabaseDelete:
		return runtime.wipicRecordDatabaseDelete(thread)
	case wipicRecordDatabaseInsert:
		return runtime.wipicRecordDatabaseInsert(thread)
	case wipicRecordDatabaseSelect:
		return runtime.wipicRecordDatabaseSelect(thread)
	case wipicRecordDatabaseUpdate:
		return runtime.wipicRecordDatabaseUpdate(thread)
	case wipicRecordDatabaseDeleteRec:
		// This slot carries two call shapes with the same signature: the
		// standard delete_record(handle, id), and a name-keyed database
		// deletion. A handle this platform issued is the only way to tell
		// them apart, so anything else is read as the name form.
		first, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		if _, ok := runtime.recordDatabaseHandles[first]; ok {
			return runtime.wipicRecordDatabaseDeleteRecord(thread)
		}
		return runtime.wipicRecordDatabaseDelete(thread)
	case wipicRecordDatabaseList:
		return runtime.wipicRecordDatabaseList(thread)
	case wipicRecordDatabaseNumRecords:
		state, ok := runtime.recordDatabaseHandle(thread)
		if !ok {
			return wipicErrorInvalid, nil
		}
		count := 0
		for _, record := range state.store.records {
			if record != nil {
				count++
			}
		}
		return uint32(count), nil
	case wipicRecordDatabaseRecordSize:
		state, ok := runtime.recordDatabaseHandle(thread)
		if !ok {
			return wipicErrorInvalid, nil
		}
		return state.store.recordSize, nil
	case wipicRecordDatabaseListNames:
		// Titles read this slot's return value as the storage still available
		// rather than as a list, and refuse to save below a threshold of their
		// own. The budget is the stream database's, because it is the same
		// per-game storage.
		used := 0
		for _, store := range runtime.cFiles {
			used += len(store.data)
		}
		for _, store := range runtime.recordDatabases {
			for _, record := range store.records {
				used += len(record)
			}
		}
		if used >= wipicFileStorageLimit {
			return 0, nil
		}
		return uint32(wipicFileStorageLimit - used), nil
	default:
		runtime.countDiagnostic(fmt.Sprintf("wipic record database function %d", function))
		return wipicErrorInvalid, nil
	}
}

func (runtime *initializationRuntime) wipicRecordDatabaseOpen(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	recordSize, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	create, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF record database name: %w", err)
	}
	runtime.countDiagnostic(fmt.Sprintf("rdb open %s size %d create %d", name, recordSize, int32(create)))
	if !storableName(recordDatabaseScope, name) || len(name) > maxRecordDatabaseName {
		return wipicErrorInvalid, nil
	}
	store, exists := runtime.recordDatabases[name]
	if !exists {
		// A database this title deleted is gone rather than empty: the list
		// hides both the emptied save and the archive's packaged copy until
		// something creates the name again.
		deleted := runtime.recordDatabaseRemovals(recordDatabaseRemovedKey)[name]
		saved, hasSaved := runtime.loadSave("rdb/" + name)
		var records [][]byte
		hasPackaged := false
		// A save wins, empty or not — see the Java table for why an empty one
		// is not treated as absent.
		if !hasSaved && !runtime.databaseDeleted(name) {
			records, hasPackaged = runtime.packagedRecordDatabase(name, recordSize)
		}
		if deleted {
			hasSaved = false
		}
		if !hasPackaged && !hasSaved && int32(create) == 0 {
			return wipicErrorNotFound, nil
		}
		if deleted {
			runtime.markRecordDatabaseRemoved(recordDatabaseRemovedKey, name, false)
		}
		store = &runtimeRecordDatabase{name: name, recordSize: recordSize}
		switch {
		case hasSaved:
			// A save wins over the packaged copy: the packaged records are the
			// initial content, and a game that has written since owns them.
			decoded, err := decodeSaveRecords(saved)
			if err != nil {
				runtime.countDiagnostic(fmt.Sprintf("rdb save decode failed %s: %v", name, err))
			} else {
				store.records = decoded
			}
		case hasPackaged:
			store.records = records
		}
		if runtime.recordDatabases == nil {
			runtime.recordDatabases = make(map[string]*runtimeRecordDatabase)
		}
		runtime.recordDatabases[name] = store
		// A database opened for creation exists from that moment, with no
		// record in it yet, so the next session finds it rather than
		// answering M_E_NOENT. The Java table next door has always done this;
		// this one did not, and after a delete it left a name the removal
		// list and the save disagreed about.
		if !hasSaved && !hasPackaged {
			runtime.persistRecordDatabase(store)
		}
	}
	if runtime.recordDatabaseHandles == nil {
		runtime.recordDatabaseHandles = make(map[uint32]*runtimeRecordDatabaseHandle)
	}
	if runtime.nextRecordDatabaseHandle >= maxRecordDatabaseHandles {
		return wipicErrorInvalid, nil
	}
	runtime.nextRecordDatabaseHandle++
	handle := recordDatabaseHandleBit | runtime.nextRecordDatabaseHandle
	runtime.recordDatabaseHandles[handle] = &runtimeRecordDatabaseHandle{store: store}
	return handle, nil
}

func (runtime *initializationRuntime) recordDatabaseHandle(thread *armcore.Thread) (*runtimeRecordDatabaseHandle, bool) {
	handle, err := thread.Register(0)
	if err != nil {
		return nil, false
	}
	state, ok := runtime.recordDatabaseHandles[handle]
	return state, ok
}

func (runtime *initializationRuntime) wipicRecordDatabaseDelete(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF record database name: %w", err)
	}
	runtime.countDiagnostic(fmt.Sprintf("rdb delete %s", name))
	// The name is written into the removal list, so it has to survive that
	// list for the same reason an open has to: a name carrying a newline would
	// come back as two, and hide two databases nobody deleted.
	if !storableName(recordDatabaseScope, name) || len(name) > maxRecordDatabaseName {
		return wipicErrorInvalid, nil
	}
	_, exists := runtime.recordDatabases[name]
	deleted := runtime.recordDatabaseRemovals(recordDatabaseRemovedKey)[name]
	_, hasSaved := runtime.loadSave("rdb/" + name)
	hasPackaged := false
	if !runtime.databaseDeleted(name) {
		// The archive is what the two tables share, so the same answer the
		// open gives: a packaged copy hidden by either list is not there for
		// this call either, or a delete would succeed on a name the open next
		// to it reports as missing.
		_, hasPackaged = runtime.packagedRecordDatabase(name, 0)
	}
	if deleted {
		hasSaved = false
	}
	if !exists && !hasPackaged && !hasSaved {
		return wipicErrorNotFound, nil
	}
	delete(runtime.recordDatabases, name)
	// Handles onto the store go with it, exactly as the file table's remove
	// does it. A caller that kept one would otherwise still be holding the
	// records, and writing through it persists them again — and the write
	// takes the name back off the removal list, so the database the title had
	// just deleted comes back whole.
	for handle, open := range runtime.recordDatabaseHandles {
		if open.store != nil && open.store.name == name {
			delete(runtime.recordDatabaseHandles, handle)
		}
	}
	// The save is emptied rather than removed: a packaged database would
	// otherwise come back on the next open, which is not what a game that
	// deleted it asked for. The name is written down as well, because an
	// emptied save still answers "the database exists" — see
	// recordDatabaseRemovals.
	runtime.storeSave("rdb/"+name, encodeSaveRecords(nil))
	runtime.markRecordDatabaseRemoved(recordDatabaseRemovedKey, name, true)
	return 0, nil
}

func (runtime *initializationRuntime) wipicRecordDatabaseInsert(thread *armcore.Thread) (uint32, error) {
	state, ok := runtime.recordDatabaseHandle(thread)
	if !ok {
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
	if int32(length) < 0 || length > maxRecordDatabaseBytes {
		return wipicErrorInvalid, nil
	}
	data := make([]byte, length)
	if length > 0 {
		if err := runtime.client.core.Memory().Read(buffer, data); err != nil {
			return 0, fmt.Errorf("read KTF record database insert buffer: %w", err)
		}
	}
	state.store.records = append(state.store.records, data)
	runtime.persistRecordDatabase(state.store)
	return uint32(len(state.store.records)), nil
}

func (runtime *initializationRuntime) wipicRecordDatabaseSelect(thread *armcore.Thread) (uint32, error) {
	state, ok := runtime.recordDatabaseHandle(thread)
	if !ok {
		return wipicErrorInvalid, nil
	}
	recordID, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	buffer, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	length, err := thread.Register(3)
	if err != nil {
		return 0, err
	}
	data, ok := state.store.record(recordID)
	if !ok {
		return wipicErrorInvalid, nil
	}
	if length < uint32(len(data)) {
		return wipicErrorShortBuf, nil
	}
	if len(data) > 0 {
		if err := runtime.client.core.Memory().Write(buffer, data); err != nil {
			return 0, fmt.Errorf("write KTF record database select buffer: %w", err)
		}
	}
	return 0, nil
}

func (runtime *initializationRuntime) wipicRecordDatabaseUpdate(thread *armcore.Thread) (uint32, error) {
	state, ok := runtime.recordDatabaseHandle(thread)
	if !ok {
		return wipicErrorInvalid, nil
	}
	recordID, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	buffer, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	length, err := thread.Register(3)
	if err != nil {
		return 0, err
	}
	if int32(length) < 0 || length > maxRecordDatabaseBytes {
		return wipicErrorInvalid, nil
	}
	if _, ok := state.store.record(recordID); !ok {
		return wipicErrorInvalid, nil
	}
	data := make([]byte, length)
	if length > 0 {
		if err := runtime.client.core.Memory().Read(buffer, data); err != nil {
			return 0, fmt.Errorf("read KTF record database update buffer: %w", err)
		}
	}
	state.store.records[recordID-1] = data
	runtime.persistRecordDatabase(state.store)
	return 0, nil
}

func (runtime *initializationRuntime) wipicRecordDatabaseDeleteRecord(thread *armcore.Thread) (uint32, error) {
	state, ok := runtime.recordDatabaseHandle(thread)
	if !ok {
		return wipicErrorInvalid, nil
	}
	recordID, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if _, ok := state.store.record(recordID); !ok {
		return wipicErrorInvalid, nil
	}
	// The slot stays, holding nothing. Compacting would renumber every record
	// after it, and the ids are what the game stored.
	state.store.records[recordID-1] = nil
	runtime.persistRecordDatabase(state.store)
	return 0, nil
}

// wipicRecordDatabaseList fills a caller's array with the id of every record
// the database holds.
//
// **Its third argument counts identifiers, not bytes.** The specification calls
// it "the size of the buffer" over an `M_Int32*`, which reads either way, and
// this platform read it as bytes — so a title asking for twelve ids received
// three and read its own uninitialized stack for the rest. A title settles it:
// one reserves 0x30 bytes of frame for the array, hands the call 12, and then
// indexes the fourth entry. Twelve four-byte ids in forty-eight bytes is the
// count reading, and only the count reading fills the entry it goes on to use.
//
// The reading is a contract rather than a compromise, and it has to be: a
// caller that meant bytes passes four times the number a caller that meant
// entries does, so serving the count reading writes past an array that meant
// the other one as soon as the database holds more than a quarter of that
// number. Nothing here reads it as bytes — the specification's own type is
// `M_Int32 *`, and the one title that reaches this call sizes its frame for
// the count — and the guard that remains is that a database is never asked to
// produce ids it does not have.
func (runtime *initializationRuntime) wipicRecordDatabaseList(thread *armcore.Thread) (uint32, error) {
	state, ok := runtime.recordDatabaseHandle(thread)
	if !ok {
		return wipicErrorInvalid, nil
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	capacity, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	if buffer == 0 || int32(capacity) <= 0 {
		return wipicErrorInvalid, nil
	}
	written := uint32(0)
	var word [4]byte
	for index, record := range state.store.records {
		if record == nil {
			continue
		}
		if written >= capacity {
			break
		}
		binary.LittleEndian.PutUint32(word[:], uint32(index+1))
		if err := runtime.client.core.Memory().Write(buffer+written*4, word[:]); err != nil {
			return 0, fmt.Errorf("write KTF record database id list: %w", err)
		}
		written++
	}
	return written, nil
}

// record answers a one-based record id, reporting whether it holds anything.
func (store *runtimeRecordDatabase) record(id uint32) ([]byte, bool) {
	if id == 0 || id > uint32(len(store.records)) {
		return nil, false
	}
	data := store.records[id-1]
	if data == nil {
		return nil, false
	}
	return data, true
}

func (runtime *initializationRuntime) persistRecordDatabase(store *runtimeRecordDatabase) {
	// Writing brings it back, as it does on the guest file table: a handle
	// held across the title's own delete would otherwise write to a key the
	// deletion list hides for ever.
	runtime.markRecordDatabaseRemoved(recordDatabaseRemovedKey, store.name, false)
	runtime.storeSave("rdb/"+store.name, encodeSaveRecords(store.records))
}

// packagedRecordDatabase reads a record database an archive ships with it. The
// database's own name carries no suffix, so the file names are composed from
// it, and the local set packages one in two shapes:
//
//   - one file, NAME.db, a header followed by one slot per record, each a live
//     flag then the record's bytes;
//   - two files, NAME.idx holding the header alone and NAME.db holding the
//     records end to end with nothing between them.
//
// The split shape is by far the commoner one here, and a database in it can
// have no data file at all: an index declaring no record is a database that
// exists and is empty, which is a different answer from one that is missing.
//
// recordSize is what the caller asked to open the database with. It is only
// consulted when the index disagrees with the size of its own data file.
func (runtime *initializationRuntime) packagedRecordDatabase(name string, recordSize uint32) ([][]byte, bool) {
	if index, exists := runtime.guestFile(name + recordDatabaseIndexSuffix); exists {
		data, _ := runtime.guestFile(name + recordDatabaseDataSuffix)
		if records, ok := runtime.parseRecordDatabaseIndex(name, index, data, recordSize); ok {
			return records, true
		}
	}
	for _, candidate := range []string{name, name + recordDatabaseDataSuffix} {
		data, exists := runtime.guestFile(candidate)
		if !exists {
			continue
		}
		if records, ok := parseRecordDatabaseFile(data); ok {
			return records, true
		}
	}
	return nil, false
}

const (
	// recordDatabaseFileHeader is the fixed header both packaged shapes carry.
	recordDatabaseFileHeader = 45
	// The record size and the record count are big-endian words inside it.
	recordDatabaseSizeOffset  = 5
	recordDatabaseCountOffset = 9

	recordDatabaseDataSuffix  = ".db"
	recordDatabaseIndexSuffix = ".idx"
)

var (
	// recordDatabaseFileMagic opens the one-file shape.
	recordDatabaseFileMagic = []byte("qtcdb")
	// recordDatabaseIndexMagic opens the index of the split shape.
	recordDatabaseIndexMagic = []byte("qtpdb")
)

// recordDatabaseMagicMatch is how much of a magic has to match. Both are five
// bytes and only their third differs, so four is still the whole of what tells
// the two shapes apart. The last byte is left out because one packaged index
// in the local set has it and the byte after it overwritten with 0xff, and
// everything else about that file — its length, its record count, and a data
// file that divides evenly by it — says it is an index. What such a file
// cannot be trusted about is its record size, and the parse below takes that
// from the data file instead.
const recordDatabaseMagicMatch = 4

// parseRecordDatabaseHeader reads the header the two shapes share. It answers
// the record size and the record count; what the words past them mean is not
// known, and nothing here needs them.
// intact says whether the whole magic matched. Only a header whose last magic
// byte is gone is one whose record size cannot be believed, and that is the
// one file the data-file fallback is for.
func parseRecordDatabaseHeader(data, magic []byte) (recordSize, count uint32, intact, ok bool) {
	if len(data) < recordDatabaseFileHeader || string(data[:recordDatabaseMagicMatch]) != string(magic[:recordDatabaseMagicMatch]) {
		return 0, 0, false, false
	}
	recordSize = binary.BigEndian.Uint32(data[recordDatabaseSizeOffset : recordDatabaseSizeOffset+4])
	count = binary.BigEndian.Uint32(data[recordDatabaseCountOffset : recordDatabaseCountOffset+4])
	return recordSize, count, string(data[:len(magic)]) == string(magic), true
}

// parseRecordDatabaseIndex decodes the split shape. The index says how many
// records there are and how long one is; the data file is those records and
// nothing else, so the two have to agree about its length. When they do not,
// the data file wins: one packaged index in the local set has the two bytes
// before its record size clobbered, and its data file still divides evenly by
// the count the index declares.
func (runtime *initializationRuntime) parseRecordDatabaseIndex(name string, index, data []byte, requested uint32) ([][]byte, bool) {
	recordSize, count, intact, ok := parseRecordDatabaseHeader(index, recordDatabaseIndexMagic)
	if !ok || count > maxDataBaseRecords {
		return nil, false
	}
	if count == 0 {
		// A database with no record still exists, whatever sits beside it. Its
		// data file is usually not in the archive at all; one that is there
		// and not empty is a stale file the index does not describe, and
		// refusing the database over it would send the title down its
		// first-run path on every launch — the answer this rule exists to
		// avoid.
		return nil, true
	}
	if uint64(recordSize)*uint64(count) != uint64(len(data)) {
		// The data file settles it only for the damage this was written for:
		// the one packaged index in the local set whose magic is cut short and
		// whose record size therefore cannot be believed. An intact header
		// that disagrees with the file beside it is a stale file rather than a
		// bent number — and with one record the divisibility test can never
		// reject, so any file at all would otherwise be read as the database's
		// single record.
		if intact || len(data) == 0 || len(data)%int(count) != 0 {
			return nil, false
		}
		recordSize = uint32(len(data) / int(count))
		runtime.countDiagnostic(fmt.Sprintf("rdb index size disagrees %s: %d records over %d bytes", name, count, len(data)))
	}
	if recordSize == 0 || recordSize > maxRecordDatabaseBytes {
		return nil, false
	}
	if requested != 0 && requested != recordSize {
		runtime.countDiagnostic(fmt.Sprintf("rdb packaged size %d opened as %d %s", recordSize, requested, name))
	}
	records := make([][]byte, 0, count)
	for offset := 0; offset+int(recordSize) <= len(data); offset += int(recordSize) {
		records = append(records, append([]byte(nil), data[offset:offset+int(recordSize)]...))
	}
	return records, true
}

// parseRecordDatabaseFile decodes the one-file shape: the shared header, then
// one slot per record, each a live flag followed by the record's bytes. A slot
// whose flag is clear is an id that was never used. The header's record count
// is not read here — the slots are counted from the file's own length, which
// is the same answer and holds for a file that was appended to.
func parseRecordDatabaseFile(data []byte) ([][]byte, bool) {
	recordSize, _, _, ok := parseRecordDatabaseHeader(data, recordDatabaseFileMagic)
	if !ok || recordSize == 0 || recordSize > maxRecordDatabaseBytes {
		return nil, false
	}
	slotSize := int(recordSize) + 1
	body := data[recordDatabaseFileHeader:]
	if len(body)%slotSize != 0 {
		return nil, false
	}
	records := make([][]byte, 0, len(body)/slotSize)
	for offset := 0; offset+slotSize <= len(body); offset += slotSize {
		slot := body[offset : offset+slotSize]
		if slot[0] == 0 {
			records = append(records, nil)
			continue
		}
		records = append(records, append([]byte(nil), slot[1:]...))
	}
	return records, true
}

// A deleted database has to be written down, for the reason the guest
// filesystem's removal list is written down (`guestFileRemovedKey`): nothing
// under either storage table can actually be deleted from. The save boundary
// has no delete, and the archive's packaged copy is the game's own package.
//
// Emptying the save is what a delete used to do, and it is not enough. The
// next open finds a save, answers "the database exists and holds nothing", and
// a title that deletes its slot to start a new game is told its save is still
// there — which is exactly how the sibling platform lost two titles' opening
// sequences (see `guestFileRemovedKey`). It stays emptied, because a save tree
// written before this list existed has to keep hiding its packaged copy; what
// the list adds is the difference between empty and gone.
//
// The two tables keep separate lists because they keep separate stores.
const (
	recordDatabaseRemovedKey = "rdb/.removed"
	javaDatabaseRemovedKey   = "jdb/.removed"
)

// databaseDeleted answers whether a name is on either table's deletion list.
// The two tables keep separate stores, and each one's list hides its own save;
// what they share is the archive, so a packaged copy has to be hidden by both.
// Without that, deleting through one table leaves the archive's copy fully
// readable through the other, and a title using both gets back the record it
// just cleared.
func (runtime *initializationRuntime) databaseDeleted(name string) bool {
	return runtime.recordDatabaseRemovals(recordDatabaseRemovedKey)[name] ||
		runtime.recordDatabaseRemovals(javaDatabaseRemovedKey)[name]
}

// databaseRemovals reads one deletion list, once per session per list.
func (runtime *initializationRuntime) recordDatabaseRemovals(key string) map[string]bool {
	if runtime.removedDatabaseLists == nil {
		runtime.removedDatabaseLists = make(map[string]map[string]bool, 2)
	}
	if names, loaded := runtime.removedDatabaseLists[key]; loaded {
		return names
	}
	names := make(map[string]bool)
	if data, exists := runtime.loadSave(key); exists {
		// Lines are taken as they are. Trimming them would map "save " onto
		// "save", so deleting a name with a space around it would hide the
		// name without one — and refusing such a name instead would orphan
		// the save a title with one already has.
		for _, line := range strings.Split(string(data), "\n") {
			if line != "" {
				names[line] = true
			}
		}
	}
	runtime.removedDatabaseLists[key] = names
	return names
}

// markDatabaseRemoved records or clears one name and writes the list back.
// Creating a database again takes its name off, or a title that deleted a save
// and started a new game would never see the new one.
func (runtime *initializationRuntime) markRecordDatabaseRemoved(key, name string, removed bool) {
	names := runtime.recordDatabaseRemovals(key)
	if names[name] == removed {
		return
	}
	if removed {
		names[name] = true
	} else {
		delete(names, name)
	}
	list := make([]string, 0, len(names))
	for existing := range names {
		list = append(list, existing)
	}
	sort.Strings(list)
	runtime.storeSave(key, []byte(strings.Join(list, "\n")))
}

// reservedStorageNames are the names the storage tables keep their own
// bookkeeping under, beside the entries a game names. A save key is a table's
// scope and the game's name joined, so a game naming an entry after one of
// these addresses the table's own record — the list of what was deleted, the
// list of directories that exist. Whichever was written last would win, and
// both readings are wrong: the game's save read back as a list of deleted
// names, or a list read back as the game's save. Worse, a list overwritten by
// records reads as a set of deleted names on the next run, which hides
// databases nobody deleted.
//
// Moving the bookkeeping somewhere a name cannot reach would orphan every list
// already written, which is the same reason these save keys still spell "db".
// So the names are reserved instead. No local title asks for one.
// The set is per scope, because a name is only reserved where a list of that
// name actually lives: the guest filesystem keeps a removal list, the WIPI C
// file table keeps a removal list and a directory list, and each database
// table keeps a removal list. A guest file called ".dirs" collides with
// nothing under "fs/" and is left alone there.
var reservedStorageNames = map[string]map[string]bool{
	guestFileScope:      {".removed": true},
	cFileScope:          {".removed": true, ".dirs": true},
	recordDatabaseScope: {".removed": true},
	javaDatabaseScope:   {".removed": true},
}

const (
	guestFileScope      = "fs"
	cFileScope          = "db"
	recordDatabaseScope = "rdb"
	javaDatabaseScope   = "jdb"
)

// reservedStorageName answers whether a guest-chosen name addresses one of the
// lists its own table keeps. The test is against the key the name normalizes
// to rather than against the name: NormalizeSaveKey drops empty and "."
// components, so "./.removed" and ".removed/" reach the same file.
func reservedStorageName(scope, name string) bool {
	key, err := backend.NormalizeSaveKey(scope + "/" + name)
	if err != nil {
		return false
	}
	rest, found := strings.CutPrefix(key, scope+"/")
	return found && reservedStorageNames[scope][rest]
}

// storableName is what both tables accept from a guest. A name has to be a
// save key on its own, has to survive the removal list's own encoding, and
// must not address the list itself.
//
// The list is names joined by newlines and read back a line at a time with the
// surrounding space trimmed, so a name carrying either would not come back as
// itself: deleting "A\nB" would hide the unrelated databases A and B, and
// deleting "save " would hide "save", which nobody deleted. Neither table
// bounded those characters, and both accept whatever string the guest built.
// It does not bound the length. That bound is the WIPI C record database's
// own, from its specification, and applying it to the Java class would refuse
// names that class has always accepted — eleven Korean characters are
// thirty-three bytes, so a title using one, and any save already written under
// it, would stop working.
func storableName(scope, name string) bool {
	if name == "" {
		return false
	}
	// Only the line separators. A name with a space around it is a name a
	// title may already have a save under, and the list carries it as it is.
	if strings.ContainsAny(name, "\n\r") {
		return false
	}
	// And it has to be a key of its own under the scope. NormalizeSaveKey
	// refuses "..", and it *collapses* ".", "/" and "./" away — so those
	// passed, and the store then wrote the scope itself: a regular file named
	// "jdb" where the directory belongs, after which every jdb write for that
	// title fails with "not a directory" and stays failing across sessions,
	// the removal list among them. What survives normalization has to still
	// be the scope and a name under it.
	key, err := backend.NormalizeSaveKey(scope + "/" + name)
	if err != nil {
		return false
	}
	if rest, under := strings.CutPrefix(key, scope+"/"); !under || rest == "" {
		return false
	}
	return !reservedStorageName(scope, name)
}
