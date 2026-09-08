package skt

import (
	"encoding/binary"
	"sort"
	"strings"
)

// A handset kept a MIDlet's record stores in the container beside its JAR, and
// an archive taken off one carries them still: an `rs` directory holding two
// files per store. `NAME.sb` is the store — its name, its version, its record
// table — and `NAME.db` is the bytes those entries point into. The file name
// is not the store name (an upper-case letter in it is written `#X`), so the
// name is read out of the `.sb` rather than decoded from the path.
//
// The directory itself is gone by the time this runs: a container's files are
// mounted by their bare names, because that is how a handset unpacked them and
// how a title opens them. So a pair is found by its suffixes, and what says a
// pair is a record store is that the header adds up against its data file.
//
// Nothing here looked at them, so a title whose save came with it opened on an
// empty world. It is the same defect the KTF platform had with the databases
// its own archives ship, and it has the same shape: the data was present, in a
// format that could be read, behind an API that never asked.
//
// The `.sb` layout, every field big-endian:
//
//	u32       the id the next record will take
//	u16 + n   the store's name
//	u32       the store's version, what getVersion answers
//	u32       how many records follow
//	u32       how many bytes the data file holds
//	u64       when it was last modified, in milliseconds
//	per record: u32 id, u32 offset into the data file, u32 length
//
// What says that reading is right rather than plausible is arithmetic that
// holds for every one of the twelve packaged stores in the local set: the
// header's declared length is the data file's exact size, the entries tile it
// end to end with no gap, and the fixed part plus twelve bytes per record is
// the `.sb` file's own length.
const (
	packagedStoreIndex = ".sb"
	packagedStoreData  = ".db"
	// packagedStoreHeaderBytes is the header without its name or its record
	// table: the two counts either side of the name, the size, and the time.
	packagedStoreHeaderBytes = 4 + 2 + 4 + 4 + 4 + 8
	packagedStoreEntryBytes  = 12
	// packagedStoreMaxRecords bounds what a crafted header may make one store
	// allocate, and packagedStoreMaxSlots what a whole archive may. A
	// handset's store held tens of records; five hundred crafted indexes of
	// twenty-seven bytes each asked for thirty-two million slots.
	packagedStoreMaxRecords = 1 << 16
	packagedStoreMaxSlots   = 1 << 17
)

// packagedRecordStores reads every record store the container carried. A store
// that cannot be read is left out rather than reported: the archive is
// untrusted input, and a title with no save is a title on its first run.
func (a *Archive) packagedRecordStores() map[string]packagedStore {
	if a == nil || len(a.Entries) == 0 {
		return nil
	}
	// The entries are walked in name order rather than in map order. Two
	// things depend on it: what this seeds is written out as a list, so map
	// order made the store index different bytes from identical input on every
	// launch — every save-tree comparison then reported a difference that was
	// not one; and when two files decode to one store name, which of them wins
	// has to be the same on every launch rather than whichever the map offered
	// first.
	indexes := make([]string, 0, len(a.Entries))
	for name := range a.Entries {
		if strings.HasSuffix(name, packagedStoreIndex) {
			indexes = append(indexes, name)
		}
	}
	sort.Strings(indexes)
	stores := make(map[string]packagedStore)
	// One archive may hold many of these and each asks for its slots before a
	// record is read, so what the whole of them may ask for is bounded as well
	// as what each one may.
	budget := packagedStoreMaxSlots
	for _, name := range indexes {
		header := a.Entries[name]
		data := a.Entries[strings.TrimSuffix(name, packagedStoreIndex)+packagedStoreData]
		store, carried, ok := parsePackagedRecordStore(header, data)
		if !ok || !validRecordStoreName(store) {
			continue
		}
		// A name that is not a save key is not a store this runtime can hold,
		// and seeding it would put a name in listRecordStores that nothing can
		// open and nothing can delete. The name comes out of an archive, so it
		// is checked the way a name from the guest is.
		if _, err := recordStoreKey(store); err != nil {
			continue
		}
		if _, taken := stores[store]; taken {
			continue
		}
		// Skipped rather than ending the walk: one oversized store early in
		// name order would otherwise suppress every smaller one after it, and
		// which stores load would depend on alphabetical position.
		if len(carried.records) > budget {
			continue
		}
		budget -= len(carried.records)
		stores[store] = carried
	}
	if len(stores) == 0 {
		return nil
	}
	return stores
}

// packagedStore is one store as the container held it: its records indexed the
// way this runtime holds them — by id, with a nil for an id the store no longer
// has — and the two numbers a title can ask the store about itself.
type packagedStore struct {
	records  [][]byte
	version  int32
	modified int64
}

// parsePackagedRecordStore decodes one `.sb` against its data file.
func parsePackagedRecordStore(header, data []byte) (string, packagedStore, bool) {
	if len(header) < packagedStoreHeaderBytes {
		return "", packagedStore{}, false
	}
	next := binary.BigEndian.Uint32(header[0:4])
	nameLength := int(binary.BigEndian.Uint16(header[4:6]))
	cursor := 6 + nameLength
	if nameLength == 0 || len(header) < cursor+packagedStoreHeaderBytes-6 {
		return "", packagedStore{}, false
	}
	name := string(header[6:cursor])
	// The version and the modification time are what getVersion and
	// getLastModified answer for a store the container carried. Decoding them
	// past and letting the store report zero and "now" would make a title that
	// stamps a version into its own data and compares it read its own save as
	// version zero.
	version := binary.BigEndian.Uint32(header[cursor : cursor+4])
	count := binary.BigEndian.Uint32(header[cursor+4 : cursor+8])
	declared := binary.BigEndian.Uint32(header[cursor+8 : cursor+12])
	modified := binary.BigEndian.Uint64(header[cursor+12 : cursor+20])
	// Past the name: the version, the count, the size, and the eight bytes of
	// the modification time.
	cursor += 4 + 4 + 4 + 8
	if count > packagedStoreMaxRecords || uint64(declared) != uint64(len(data)) {
		return "", packagedStore{}, false
	}
	if len(header) < cursor+int(count)*packagedStoreEntryBytes {
		return "", packagedStore{}, false
	}
	// The store is as long as the next id says, so getNextRecordID answers
	// what the handset would have. A record the store no longer holds is a
	// hole in it rather than a shift of every id after it.
	//
	// That word is the one field here that sizes an allocation, and it comes
	// out of an archive, so it is bounded exactly as the record count is: a
	// `.sb` is a file anybody can craft, and four bytes naming four billion
	// records is a hundred gigabytes asked for before a single record has
	// been read. It also has to be consistent with the count — the next id
	// comes after every id the table holds — which is what the twelve
	// packaged stores in the local set all say.
	if next < rmsFirstRecordID || uint64(next)-rmsFirstRecordID > packagedStoreMaxRecords {
		return "", packagedStore{}, false
	}
	if uint64(count) > uint64(next)-rmsFirstRecordID {
		return "", packagedStore{}, false
	}
	length := int(next) - rmsFirstRecordID
	records := make([][]byte, max(length, 0))
	// The entries may point anywhere inside the data file, including all at
	// the same offset, so the count and the slot bound say nothing about how
	// many bytes they ask for between them: two thousand entries each naming a
	// megabyte of a one-megabyte file asked for two gigabytes. A real store
	// tiles its data file end to end, so the file's own length is the honest
	// ceiling for the whole of them.
	carried := 0
	for index := range int(count) {
		entry := header[cursor+index*packagedStoreEntryBytes:]
		id := binary.BigEndian.Uint32(entry[0:4])
		offset := binary.BigEndian.Uint32(entry[4:8])
		size := binary.BigEndian.Uint32(entry[8:12])
		// An id the store holds is below the id it will hand out next. Without
		// that the id is a second way to size the slice: a one-record table
		// naming id 16384 grows the store to 16384 slots and makes
		// getNextRecordID answer past every id the title ever reserved.
		if id < rmsFirstRecordID || id >= next || size > rmsMaxRecordBytes {
			return "", packagedStore{}, false
		}
		if uint64(offset)+uint64(size) > uint64(len(data)) {
			return "", packagedStore{}, false
		}
		carried += int(size)
		if carried > len(data) {
			return "", packagedStore{}, false
		}
		for int(id) > len(records) {
			records = append(records, nil)
		}
		// A record of no bytes is a record: MIDP writes one for
		// addRecord(null, 0, 0). append([]byte(nil)) answers nil, which is
		// this runtime's tombstone for an id the store no longer has.
		record := make([]byte, size)
		copy(record, data[offset:offset+size])
		records[id-rmsFirstRecordID] = record
	}
	return name, packagedStore{records: records, version: int32(version), modified: int64(modified)}, true
}
