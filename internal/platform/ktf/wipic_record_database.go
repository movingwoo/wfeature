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
		records, hasPackaged := runtime.packagedRecordDatabase(name)
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
	_, hasPackaged := runtime.packagedRecordDatabase(name)
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

func (runtime *initializationRuntime) wipicRecordDatabaseList(thread *armcore.Thread) (uint32, error) {
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
	written := uint32(0)
	var word [4]byte
	for index, record := range state.store.records {
		if record == nil {
			continue
		}
		if (written+1)*4 > length {
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

// packagedRecordDatabase reads a record database an archive ships as a data
// file. The file carries the database's name with a .db suffix the name itself
// does not have, so both spellings are tried: a game opens "NAME" and the
// archive holds "NAME.db".
func (runtime *initializationRuntime) packagedRecordDatabase(name string) ([][]byte, bool) {
	for _, candidate := range []string{name, name + ".db"} {
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

// recordDatabaseFileHeader is the fixed header a packaged record database
// carries before its slots.
const recordDatabaseFileHeader = 45

// recordDatabaseFileMagic opens the packaged format.
var recordDatabaseFileMagic = []byte("qtcdb")

// parseRecordDatabaseFile decodes the packaged format: a header naming the
// record size, then one slot per record, each a live flag followed by the
// record's bytes. A slot whose flag is clear is an id that was never used.
func parseRecordDatabaseFile(data []byte) ([][]byte, bool) {
	if len(data) < recordDatabaseFileHeader || string(data[:len(recordDatabaseFileMagic)]) != string(recordDatabaseFileMagic) {
		return nil, false
	}
	recordSize := binary.LittleEndian.Uint32(data[8:12])
	if recordSize == 0 || recordSize > maxRecordDatabaseBytes {
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
