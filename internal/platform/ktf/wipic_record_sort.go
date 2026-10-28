package ktf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// recordDatabaseExists answers whether a record database of this name is
// there for the title: open in this session, saved by an earlier one, or
// shipped in the archive. A database the title deleted is not, whichever of
// those still holds bytes for it.
func (runtime *initializationRuntime) recordDatabaseExists(name string) bool {
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
	return exists || hasPackaged || hasSaved
}

// wipicRecordDatabaseAccessMode serves MC_dbGetAccessMode(dataBaseName): the
// directory a database lives in. A title has the one private directory here
// (see wipicMakeDirectory), so a database that exists answers
// MC_DIR_PRIVATE_ACCESS, one that does not answers M_E_NOENT, and a null name
// M_E_INVALID, which are the specification's three answers.
func (runtime *initializationRuntime) wipicRecordDatabaseAccessMode(thread *armcore.Thread) (uint32, error) {
	const privateAccess = 1 // MC_DIR_PRIVATE_ACCESS
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	if nameAddress == 0 {
		return wipicErrorInvalid, nil
	}
	name, err := runtime.readCString(nameAddress, maxFileNameRead)
	if err != nil {
		return 0, fmt.Errorf("read KTF record database name: %w", err)
	}
	if !storableName(recordDatabaseScope, name) || len(name) > maxRecordDatabaseName || !runtime.recordDatabaseExists(name) {
		return wipicErrorNotFound, nil
	}
	return privateAccess, nil
}

// wipicRecordDatabaseSort serves MC_dbSortRecords(dbID, buf, len, compare,
// filter): the ids of the records, in order, written to buf, and how many
// there are. The stored database does not change.
//
// Without a compare the order is the records' bytes, compared byte by byte
// ascending, which is the specification's own example. With one, compare is a
// guest function given two records, and the specification's convention is
// the reverse of qsort's: a positive answer puts the first record *before*
// the second. A filter is a guest function given one record; a positive answer
// keeps it, zero leaves it out. Both are called with pointers to copies of the
// records in a block of guest memory that is released afterwards, which is
// the only way a function in the title can read them. Records that compare
// equal keep the order of their ids.
//
// The ids are this table's one-based ones (see runtimeRecordDatabase). A
// buffer the ids do not fit answers M_E_SHORTBUF and holds nothing, and a null
// buffer or a length that is not positive answers M_E_INVALID; len counts ids,
// as MC_dbListRecords' does here.
func (runtime *initializationRuntime) wipicRecordDatabaseSort(thread *armcore.Thread) (uint32, error) {
	state, ok := runtime.recordDatabaseHandle(thread)
	if !ok {
		return wipicErrorBadFD, nil
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	capacity, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	compare, err := thread.Register(3)
	if err != nil {
		return 0, err
	}
	filter, err := runtime.wipicArgument(thread, 4)
	if err != nil {
		return 0, err
	}
	if buffer == 0 || int32(capacity) <= 0 {
		return wipicErrorInvalid, nil
	}
	ids := make([]uint32, 0, len(state.store.records))
	for index, record := range state.store.records {
		if record != nil {
			ids = append(ids, uint32(index+1))
		}
	}
	record := func(id uint32) []byte { return state.store.records[id-1] }

	if compare != 0 || filter != 0 {
		copies, release, err := runtime.recordCopies(state.store, ids)
		if err != nil {
			return 0, err
		}
		defer release()
		if filter != 0 {
			kept := ids[:0]
			for _, id := range ids {
				answer, err := runtime.callRecordFunction(thread, filter, copies[id])
				if err != nil {
					return 0, fmt.Errorf("run KTF record filter at %#x: %w", filter, err)
				}
				if answer > 0 {
					kept = append(kept, id)
				}
			}
			ids = kept
		}
		if compare != 0 {
			var failed error
			sort.SliceStable(ids, func(i, j int) bool {
				if failed != nil {
					return false
				}
				answer, err := runtime.callRecordFunction(thread, compare, copies[ids[i]], copies[ids[j]])
				if err != nil {
					failed = err
					return false
				}
				return answer > 0
			})
			if failed != nil {
				return 0, fmt.Errorf("run KTF record comparison at %#x: %w", compare, failed)
			}
		}
	}
	if compare == 0 {
		sort.SliceStable(ids, func(i, j int) bool {
			return bytes.Compare(record(ids[i]), record(ids[j])) < 0
		})
	}
	if uint32(len(ids)) > capacity {
		return wipicErrorShortBuf, nil
	}
	encoded := make([]byte, 4*len(ids))
	for index, id := range ids {
		binary.LittleEndian.PutUint32(encoded[index*4:], id)
	}
	if err := runtime.client.core.Memory().Write(buffer, encoded); err != nil {
		return 0, fmt.Errorf("write KTF sorted record ids: %w", err)
	}
	return uint32(len(ids)), nil
}

// recordCopies writes the records into one block of guest memory and answers
// where each one starts, and how to give the block back.
func (runtime *initializationRuntime) recordCopies(store *runtimeRecordDatabase, ids []uint32) (map[uint32]uint32, func(), error) {
	total := 0
	for _, id := range ids {
		total += len(store.records[id-1])
	}
	block, err := runtime.allocateWIPIC(uint32(max(total, 1)))
	if err != nil {
		return nil, nil, fmt.Errorf("allocate KTF record copies: %w", err)
	}
	release := func() { runtime.freeWIPIC(block) }
	copies := make(map[uint32]uint32, len(ids))
	address := block + wipicAllocationOverhead
	for _, id := range ids {
		data := store.records[id-1]
		if err := runtime.client.core.Memory().Write(address, data); err != nil {
			release()
			return nil, nil, fmt.Errorf("write KTF record copy: %w", err)
		}
		copies[id] = address
		address += uint32(len(data))
	}
	return copies, release, nil
}

// callRecordFunction runs one of the title's record callbacks on the thread
// that made the call, the way a pixel operation runs (see applyPixelOp), and
// answers its result as the signed integer the specification makes it.
func (runtime *initializationRuntime) callRecordFunction(thread *armcore.Thread, function uint32, arguments ...uint32) (int32, error) {
	if err := runtime.enterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.leaveAOTCall(runtime.aotCallOwner())
	summary, err := runtime.client.core.Call(runtime.currentContext, thread, function, ReturnAddress, arguments, runtime.handleSupervisorCall)
	if err != nil {
		return 0, err
	}
	runtime.countDiagnostic("record database guest call")
	return int32(summary.Context.Registers[0]), nil
}
