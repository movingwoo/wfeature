package skt

import (
	"encoding/binary"
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
	// packagedStoreMaxRecords bounds what a crafted header may make this
	// allocate. A handset's store held tens of records.
	packagedStoreMaxRecords = 1 << 16
)

// packagedRecordStores reads every record store the container carried. A store
// that cannot be read is left out rather than reported: the archive is
// untrusted input, and a title with no save is a title on its first run.
func (a *Archive) packagedRecordStores() map[string][][]byte {
	if a == nil || len(a.Entries) == 0 {
		return nil
	}
	stores := make(map[string][][]byte)
	for name, header := range a.Entries {
		if !strings.HasSuffix(name, packagedStoreIndex) {
			continue
		}
		data := a.Entries[strings.TrimSuffix(name, packagedStoreIndex)+packagedStoreData]
		store, records, ok := parsePackagedRecordStore(header, data)
		if !ok || !validRecordStoreName(store) {
			continue
		}
		stores[store] = records
	}
	if len(stores) == 0 {
		return nil
	}
	return stores
}

// parsePackagedRecordStore decodes one `.sb` against its data file. It answers
// the store's name and its records indexed the way this runtime holds them:
// by id, with a nil for an id the store no longer has.
func parsePackagedRecordStore(header, data []byte) (string, [][]byte, bool) {
	if len(header) < packagedStoreHeaderBytes {
		return "", nil, false
	}
	next := binary.BigEndian.Uint32(header[0:4])
	nameLength := int(binary.BigEndian.Uint16(header[4:6]))
	cursor := 6 + nameLength
	if nameLength == 0 || len(header) < cursor+packagedStoreHeaderBytes-6 {
		return "", nil, false
	}
	name := string(header[6:cursor])
	count := binary.BigEndian.Uint32(header[cursor+4 : cursor+8])
	declared := binary.BigEndian.Uint32(header[cursor+8 : cursor+12])
	// Past the name: the version, the count, the size, and the eight bytes of
	// the modification time.
	cursor += 4 + 4 + 4 + 8
	if count > packagedStoreMaxRecords || uint64(declared) != uint64(len(data)) {
		return "", nil, false
	}
	if len(header) < cursor+int(count)*packagedStoreEntryBytes {
		return "", nil, false
	}
	// The store is as long as the next id says, so getNextRecordID answers
	// what the handset would have. A record the store no longer holds is a
	// hole in it rather than a shift of every id after it.
	length := int(next) - rmsFirstRecordID
	records := make([][]byte, max(length, 0))
	for index := range int(count) {
		entry := header[cursor+index*packagedStoreEntryBytes:]
		id := binary.BigEndian.Uint32(entry[0:4])
		offset := binary.BigEndian.Uint32(entry[4:8])
		size := binary.BigEndian.Uint32(entry[8:12])
		if id < rmsFirstRecordID || id > packagedStoreMaxRecords || size > rmsMaxRecordBytes {
			return "", nil, false
		}
		if uint64(offset)+uint64(size) > uint64(len(data)) {
			return "", nil, false
		}
		for int(id) > len(records) {
			records = append(records, nil)
		}
		records[id-rmsFirstRecordID] = append([]byte(nil), data[offset:offset+size]...)
	}
	return name, records, true
}
