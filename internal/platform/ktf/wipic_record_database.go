package ktf

import (
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
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
	if name == "" || len(name) > maxRecordDatabaseName {
		return wipicErrorInvalid, nil
	}
	store, exists := runtime.recordDatabases[name]
	if !exists {
		records, hasPackaged := runtime.packagedRecordDatabase(name, recordSize)
		saved, hasSaved := runtime.loadSave("rdb/" + name)
		if !hasPackaged && !hasSaved && int32(create) == 0 {
			return wipicErrorNotFound, nil
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
	_, exists := runtime.recordDatabases[name]
	_, hasPackaged := runtime.packagedRecordDatabase(name, 0)
	_, hasSaved := runtime.loadSave("rdb/" + name)
	if !exists && !hasPackaged && !hasSaved {
		return wipicErrorNotFound, nil
	}
	delete(runtime.recordDatabases, name)
	// The save is emptied rather than removed: a packaged database would
	// otherwise come back on the next open, which is not what a game that
	// deleted it asked for.
	runtime.storeSave("rdb/"+name, encodeSaveRecords(nil))
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
func parseRecordDatabaseHeader(data, magic []byte) (recordSize, count uint32, ok bool) {
	if len(data) < recordDatabaseFileHeader || string(data[:recordDatabaseMagicMatch]) != string(magic[:recordDatabaseMagicMatch]) {
		return 0, 0, false
	}
	recordSize = binary.BigEndian.Uint32(data[recordDatabaseSizeOffset : recordDatabaseSizeOffset+4])
	count = binary.BigEndian.Uint32(data[recordDatabaseCountOffset : recordDatabaseCountOffset+4])
	return recordSize, count, true
}

// parseRecordDatabaseIndex decodes the split shape. The index says how many
// records there are and how long one is; the data file is those records and
// nothing else, so the two have to agree about its length. When they do not,
// the data file wins: one packaged index in the local set has the two bytes
// before its record size clobbered, and its data file still divides evenly by
// the count the index declares.
func (runtime *initializationRuntime) parseRecordDatabaseIndex(name string, index, data []byte, requested uint32) ([][]byte, bool) {
	recordSize, count, ok := parseRecordDatabaseHeader(index, recordDatabaseIndexMagic)
	if !ok || count > maxDataBaseRecords {
		return nil, false
	}
	if count == 0 {
		// A database with no record still exists. Its data file is usually not
		// in the archive at all, and an empty one says the same thing.
		return nil, len(data) == 0
	}
	if uint64(recordSize)*uint64(count) != uint64(len(data)) {
		if len(data) == 0 || len(data)%int(count) != 0 {
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
	recordSize, _, ok := parseRecordDatabaseHeader(data, recordDatabaseFileMagic)
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
