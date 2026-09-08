package ktf

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// packagedDatabaseHeader builds the header the two packaged shapes share.
func packagedDatabaseHeader(magic []byte, recordSize, count int) []byte {
	header := make([]byte, recordDatabaseFileHeader)
	copy(header, magic)
	binary.BigEndian.PutUint32(header[recordDatabaseSizeOffset:], uint32(recordSize))
	binary.BigEndian.PutUint32(header[recordDatabaseCountOffset:], uint32(count))
	return header
}

// packedRecordDatabase builds the one-file shape: the header, then one slot
// per record with a live flag in front of each.
func packedRecordDatabase(recordSize int, records ...[]byte) []byte {
	file := packagedDatabaseHeader(recordDatabaseFileMagic, recordSize, len(records))
	for _, record := range records {
		slot := make([]byte, recordSize+1)
		if record != nil {
			slot[0] = 1
			copy(slot[1:], record)
		}
		file = append(file, slot...)
	}
	return file
}

// splitRecordDatabase builds the two-file shape: an index carrying the header
// alone, and the records end to end with nothing between them.
func splitRecordDatabase(recordSize int, records ...[]byte) (index, data []byte) {
	index = packagedDatabaseHeader(recordDatabaseIndexMagic, recordSize, len(records))
	for _, record := range records {
		slot := make([]byte, recordSize)
		copy(slot, record)
		data = append(data, slot...)
	}
	return index, data
}

func openRecordDatabase(t *testing.T, runtime *initializationRuntime, name string, recordSize, create uint32) uint32 {
	t.Helper()
	const nameAddress = platformDataBase + 0x8000
	if err := runtime.client.core.Memory().Write(nameAddress, append([]byte(name), 0)); err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.Context{})
	for register, value := range map[int]uint32{0: nameAddress, 1: recordSize, 2: create} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	handle, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseOpen)
	if err != nil {
		t.Fatalf("open error = %v", err)
	}
	return handle
}

// TestRecordDatabaseSeedsFromThePackagedFileNamedWithDB pins the lookup that
// made one title unplayable: a game opens the database by a bare name and the
// archive ships it as that name with a .db suffix. Missing it does not fail
// anywhere visible — the game reads its own zeroed buffer and carries on with
// zeros for every value it expected to find.
func TestRecordDatabaseSeedsFromThePackagedFileNamedWithDB(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.guestFiles = map[string][]byte{
		"KEYS.db": packedRecordDatabase(8, []byte("YVQZZQEX"), []byte("EIHNTSAZ")),
	}

	// create = 0: the game is not asking for a new database, so only the
	// packaged copy can answer, and answering with the suffix stripped is the
	// whole point.
	handle := openRecordDatabase(t, runtime, "KEYS", 8, 0)
	if handle&recordDatabaseHandleBit == 0 {
		t.Fatalf("open = %#x, want a record database handle", handle)
	}

	const buffer = platformDataBase + 0x9000
	thread := armcore.NewThread(armcore.Context{})
	for register, value := range map[int]uint32{0: handle, 1: 1, 2: buffer, 3: 8} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseSelect)
	if err != nil || result != 0 {
		t.Fatalf("select record 1 = %#x, err = %v", result, err)
	}
	got := make([]byte, 8)
	if err := runtime.client.core.Memory().Read(buffer, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "YVQZZQEX" {
		t.Fatalf("record 1 = %q, want the packaged record", got)
	}
}

// TestRecordDatabaseRefusesToOpenWhatIsNotThere covers the other half: a game
// that asks for a database nobody shipped has to be told so, because its own
// fresh-start path is what runs on that answer.
func TestRecordDatabaseRefusesToOpenWhatIsNotThere(t *testing.T) {
	_, runtime := newTestRuntime(t)
	if handle := openRecordDatabase(t, runtime, "ABSENT", 8, 0); handle != wipicErrorNotFound {
		t.Fatalf("open of an absent database = %#x, want %#x", handle, wipicErrorNotFound)
	}
	// With create set, the same call opens an empty database instead.
	if handle := openRecordDatabase(t, runtime, "ABSENT", 8, 1); handle&recordDatabaseHandleBit == 0 {
		t.Fatalf("open with create = %#x, want a handle", handle)
	}
}

// TestRecordDatabaseKeepsIdsAcrossADelete pins the numbering: ids are handed
// out by position, so a deleted record leaves its slot behind rather than
// renumbering the records a game already stored ids for.
func TestRecordDatabaseKeepsIdsAcrossADelete(t *testing.T) {
	_, runtime := newTestRuntime(t)
	handle := openRecordDatabase(t, runtime, "SAVES", 4, 1)

	const buffer = platformDataBase + 0x9000
	insert := func(payload string) uint32 {
		t.Helper()
		if err := runtime.client.core.Memory().Write(buffer, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		thread := armcore.NewThread(armcore.Context{})
		for register, value := range map[int]uint32{0: handle, 1: buffer, 2: uint32(len(payload))} {
			if err := thread.SetRegister(register, value); err != nil {
				t.Fatal(err)
			}
		}
		id, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseInsert)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	if id := insert("aaaa"); id != 1 {
		t.Fatalf("first insert = %d, want 1", id)
	}
	if id := insert("bbbb"); id != 2 {
		t.Fatalf("second insert = %d, want 2", id)
	}

	thread := armcore.NewThread(armcore.Context{})
	for register, value := range map[int]uint32{0: handle, 1: 1} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseDeleteRec); err != nil || result != 0 {
		t.Fatalf("delete record 1 = %#x, err = %v", result, err)
	}
	if id := insert("cccc"); id != 3 {
		t.Fatalf("insert after a delete = %d, want 3 rather than the freed id", id)
	}
	// Record 2 is still record 2.
	for register, value := range map[int]uint32{0: handle, 1: 2, 2: buffer, 3: 4} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseSelect); err != nil || result != 0 {
		t.Fatalf("select record 2 = %#x, err = %v", result, err)
	}
	got := make([]byte, 4)
	if err := runtime.client.core.Memory().Read(buffer, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "bbbb" {
		t.Fatalf("record 2 = %q, want it undisturbed by the delete", got)
	}
}

// TestRecordDatabaseSlot6TellsTheTwoCallShapesApart covers the overloaded
// slot: the same signature carries delete-record and delete-database, and only
// a handle this platform issued distinguishes them.
func TestRecordDatabaseSlot6TellsTheTwoCallShapesApart(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.guestFiles = map[string][]byte{
		"KEYS.db": packedRecordDatabase(8, []byte("YVQZZQEX")),
	}
	handle := openRecordDatabase(t, runtime, "KEYS", 8, 0)

	// A name pointer rather than a handle selects the database form.
	const nameAddress = platformDataBase + 0x8000
	thread := armcore.NewThread(armcore.Context{})
	for register, value := range map[int]uint32{0: nameAddress, 1: 0} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseDeleteRec); err != nil || result != 0 {
		t.Fatalf("delete database = %#x, err = %v", result, err)
	}
	if _, still := runtime.recordDatabases["KEYS"]; still {
		t.Fatal("the database survived a delete addressed by name")
	}
	// The handle form still reaches records.
	for register, value := range map[int]uint32{0: handle, 1: 1} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseDeleteRec); err != nil || result != 0 {
		t.Fatalf("delete record through the handle = %#x, err = %v", result, err)
	}
}

// TestPackagedRecordDatabaseRejectsWhatIsNotOne keeps the parser from reading
// an unrelated packaged file as records.
func TestPackagedRecordDatabaseRejectsWhatIsNotOne(t *testing.T) {
	if _, ok := parseRecordDatabaseFile([]byte("not a database")); ok {
		t.Fatal("parsed a file with the wrong magic")
	}
	short := packedRecordDatabase(8, []byte("YVQZZQEX"))
	if _, ok := parseRecordDatabaseFile(short[:len(short)-1]); ok {
		t.Fatal("parsed a file whose slots do not divide evenly")
	}
	zeroed := packedRecordDatabase(8)
	binary.BigEndian.PutUint32(zeroed[recordDatabaseSizeOffset:], 0)
	if _, ok := parseRecordDatabaseFile(zeroed); ok {
		t.Fatal("parsed a file claiming a zero record size")
	}
}

// TestASplitPackagedDatabaseIsOpenedFromItsIndex covers the shape most of the
// local archives package: the header is a NAME.idx of its own and NAME.db is
// the records end to end, with no live flag in front of them. Reading only the
// one-file shape left every one of those databases missing, and a title that
// ships its own save that way opens on an offer to download it again.
func TestASplitPackagedDatabaseIsOpenedFromItsIndex(t *testing.T) {
	_, runtime := newTestRuntime(t)
	index, data := splitRecordDatabase(4, []byte("aaaa"), []byte("bbbb"))
	runtime.guestFiles = map[string][]byte{"SAVE.idx": index, "SAVE.db": data}

	records, ok := runtime.packagedRecordDatabase("SAVE", 4)
	if !ok {
		t.Fatal("the split shape was not recognized as a database")
	}
	if len(records) != 2 || string(records[0]) != "aaaa" || string(records[1]) != "bbbb" {
		t.Fatalf("packaged records = %q, want the two the data file holds", records)
	}
	// The whole point is that a game opening it without asking for it to be
	// created finds it rather than being told it is not there.
	if handle := openRecordDatabase(t, runtime, "SAVE", 4, 0); handle == 0 {
		t.Fatal("opening the packaged database without create failed")
	}
}

// TestAPackagedIndexDeclaringNoRecordIsStillADatabase pins the difference
// between empty and missing. An index whose count is zero has no data file in
// the archive at all, and answering "no such database" for it would send a
// title down its first-run path every time.
func TestAPackagedIndexDeclaringNoRecordIsStillADatabase(t *testing.T) {
	_, runtime := newTestRuntime(t)
	index, _ := splitRecordDatabase(100)
	runtime.guestFiles = map[string][]byte{"EMPTY.idx": index}

	records, ok := runtime.packagedRecordDatabase("EMPTY", 100)
	if !ok {
		t.Fatal("an index with no records was read as no database")
	}
	if len(records) != 0 {
		t.Fatalf("records = %q, want none", records)
	}
}

// TestTheDataFileSettlesARecordSizeItsIndexDisagreesWith covers one packaged
// index in the local set whose record size has bytes clobbered. Its data file
// still divides evenly by the record count, and that division is the honest
// answer; refusing the database instead loses a title's saved game.
func TestTheDataFileSettlesARecordSizeItsIndexDisagreesWith(t *testing.T) {
	_, runtime := newTestRuntime(t)
	index, data := splitRecordDatabase(4, []byte("aaaa"), []byte("bbbb"))
	// The real file has the last byte of the magic and the first byte of the
	// record size overwritten together, which is why neither is trusted.
	index[recordDatabaseMagicMatch] = 0xff
	binary.BigEndian.PutUint32(index[recordDatabaseSizeOffset:], 0xff000004)
	runtime.guestFiles = map[string][]byte{"BENT.idx": index, "BENT.db": data}

	records, ok := runtime.packagedRecordDatabase("BENT", 4)
	if !ok {
		t.Fatal("a database whose index disagrees with its own data was refused")
	}
	if len(records) != 2 || string(records[1]) != "bbbb" {
		t.Fatalf("records = %q, want the two the data file holds", records)
	}

	// A data file the count cannot divide is a disagreement nothing settles.
	runtime.guestFiles["BENT.db"] = data[:len(data)-1]
	if _, ok := runtime.packagedRecordDatabase("BENT", 4); ok {
		t.Fatal("parsed a data file the record count does not divide")
	}
}

// TestTheOneFileShapeReadsItsRecordSizeBigEndian pins the header's byte order.
// It was read as a little-endian word one field further on, which answers the
// same number for a record shorter than 256 bytes and a different one for
// everything above that.
func TestTheOneFileShapeReadsItsRecordSizeBigEndian(t *testing.T) {
	record := make([]byte, 328)
	copy(record, "wide")
	records, ok := parseRecordDatabaseFile(packedRecordDatabase(len(record), record))
	if !ok {
		t.Fatal("a database whose records are wider than 255 bytes was refused")
	}
	if len(records) != 1 || len(records[0]) != len(record) {
		t.Fatalf("records = %d of %d bytes, want one of %d", len(records), len(records[0]), len(record))
	}
}

// TestRecordDatabaseListCountsIdentifiersRatherThanBytes pins the unit of the
// list call's third argument. Reading it as a byte length quartered the answer,
// and a title that asked for twelve identifiers received three: the entries it
// went on to index held whatever its own stack had left there, which it used as
// a resource name and then dereferenced the failed lookup.
func TestRecordDatabaseListCountsIdentifiersRatherThanBytes(t *testing.T) {
	_, runtime := newTestRuntime(t)
	records := make([][]byte, 0, 12)
	for index := range 12 {
		records = append(records, []byte{byte(index), 0, 0, 0, 0, 0, 0, 0})
	}
	runtime.guestFiles = map[string][]byte{"SLOTS.db": packedRecordDatabase(8, records...)}
	handle := openRecordDatabase(t, runtime, "SLOTS", 8, 0)

	const buffer = platformDataBase + 0x9000
	list := func(capacity uint32) uint32 {
		t.Helper()
		if err := runtime.client.core.Memory().Write(buffer, make([]byte, 4*len(records)+16)); err != nil {
			t.Fatal(err)
		}
		thread := armcore.NewThread(armcore.Context{})
		for register, value := range map[int]uint32{0: handle, 1: buffer, 2: capacity} {
			if err := thread.SetRegister(register, value); err != nil {
				t.Fatal(err)
			}
		}
		written, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseList)
		if err != nil {
			t.Fatalf("list error = %v", err)
		}
		return written
	}

	if written := list(uint32(len(records))); written != uint32(len(records)) {
		t.Fatalf("list of %d records into a %d-entry array wrote %d", len(records), len(records), written)
	}
	for index := range records {
		var word [4]byte
		if err := runtime.client.core.Memory().Read(buffer+uint32(index)*4, word[:]); err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(word[:]); got != uint32(index+1) {
			t.Fatalf("identifier %d = %d, want %d", index, got, index+1)
		}
	}

	// An array smaller than the database still stops at what it can hold, so
	// the reading that was wrong is not an overrun for a title that held it.
	if written := list(4); written != 4 {
		t.Fatalf("list into a four-entry array wrote %d, want 4", written)
	}
	var past [4]byte
	if err := runtime.client.core.Memory().Read(buffer+16, past[:]); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(past[:]) != 0 {
		t.Fatal("the list wrote past the entries it was given")
	}

	// A buffer that is not there, or an array with no room in it, is the one
	// case the specification names as invalid rather than empty.
	for _, arguments := range []struct{ buffer, capacity uint32 }{{0, 4}, {buffer, 0}} {
		thread := armcore.NewThread(armcore.Context{})
		for register, value := range map[int]uint32{0: handle, 1: arguments.buffer, 2: arguments.capacity} {
			if err := thread.SetRegister(register, value); err != nil {
				t.Fatal(err)
			}
		}
		result, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseList)
		if err != nil || result != wipicErrorInvalid {
			t.Fatalf("list(buffer=%#x, capacity=%d) = %#x, err = %v", arguments.buffer, arguments.capacity, result, err)
		}
	}
}

// TestTheNamesTheTablesKeepTheirOwnListsUnderAreReserved closes a collision
// that runs both ways: a save key is a table's scope joined to the name a game
// chose, so a game naming an entry ".removed" addresses the list of what it
// deleted. Whichever was written last used to win — the game's records read
// back as a list of deleted names, which then hides databases nobody deleted.
func TestTheNamesTheTablesKeepTheirOwnListsUnderAreReserved(t *testing.T) {
	for _, name := range []string{".removed", "./.removed", ".removed/"} {
		if !reservedStorageName(javaDatabaseScope, name) {
			t.Fatalf("%q reaches a key a table keeps its own list under", name)
		}
	}
	if !reservedStorageName(cFileScope, ".dirs") {
		t.Fatal("the file table's directory list is not reserved on its own scope")
	}
	// A name is reserved only on the scope that keeps a list of it: the guest
	// filesystem has no directory list, so a guest file called ".dirs"
	// collides with nothing.
	for _, name := range []string{".dirs", ".index", "sub/.removed", "removed", ".removedx", "scores"} {
		if reservedStorageName(guestFileScope, name) {
			t.Fatalf("%q collides with nothing under fs/ and was refused", name)
		}
	}
	// The list is names joined by newlines, trimmed on the way back, so a name
	// carrying either would not come back as itself.
	for _, name := range []string{"", "A\nB", "save ", " save", ".removed"} {
		if storableName(javaDatabaseScope, name) {
			t.Fatalf("%q cannot survive the removal list and was accepted", name)
		}
	}
	// The length cap is the WIPI C record database's own and does not reach
	// the Java class: eleven Korean characters are thirty-three bytes, and
	// that class has always accepted them.
	long := strings.Repeat("가", 11)
	if !storableName(javaDatabaseScope, long) {
		t.Fatalf("a %d-byte name the Java class has always taken was refused", len(long))
	}
	if !storableName(recordDatabaseScope, "save") {
		t.Fatal("an ordinary name was refused")
	}

	_, runtime := newTestRuntime(t)
	if handle := openRecordDatabase(t, runtime, ".removed", 8, 1); handle != wipicErrorInvalid {
		t.Fatalf("the record table opened its own list as a database: %#x", handle)
	}
}

// TestAnIntactIndexIsNotSettledByWhateverFileSitsBesideIt narrows the recovery
// the split parser makes. It exists for one packaged index whose magic is cut
// short, and with a single record the divide-evenly test can never reject — so
// an intact header that disagrees with the file beside it would have read that
// whole file as the database's one record.
func TestAnIntactIndexIsNotSettledByWhateverFileSitsBesideIt(t *testing.T) {
	_, runtime := newTestRuntime(t)
	index, _ := splitRecordDatabase(5, []byte("aaaaa"))
	unrelated := make([]byte, 1400)
	runtime.guestFiles = map[string][]byte{"CFG.idx": index, "CFG.db": unrelated}
	if records, ok := runtime.packagedRecordDatabase("CFG", 5); ok {
		t.Fatalf("an intact index took an unrelated file as its data: %d records of %d bytes", len(records), len(records[0]))
	}
	// The bent one it was written for still recovers.
	bent, data := splitRecordDatabase(4, []byte("aaaa"), []byte("bbbb"))
	bent[recordDatabaseMagicMatch] = 0xff
	binary.BigEndian.PutUint32(bent[recordDatabaseSizeOffset:], 0xff000004)
	runtime.guestFiles = map[string][]byte{"BENT.idx": bent, "BENT.db": data}
	if _, ok := runtime.packagedRecordDatabase("BENT", 4); !ok {
		t.Fatal("the bent index this recovery exists for was refused")
	}
}

// TestAnIndexDeclaringNoRecordIsADatabaseWhateverSitsBesideIt keeps the
// empty-versus-missing rule from turning on a stale data file: answering "no
// such database" sends a title down its first-run path on every launch.
func TestAnIndexDeclaringNoRecordIsADatabaseWhateverSitsBesideIt(t *testing.T) {
	_, runtime := newTestRuntime(t)
	index, _ := splitRecordDatabase(100)
	runtime.guestFiles = map[string][]byte{"E.idx": index, "E.db": []byte("stale")}
	records, ok := runtime.packagedRecordDatabase("E", 100)
	if !ok {
		t.Fatal("an index declaring no record was read as no database")
	}
	if len(records) != 0 {
		t.Fatalf("records = %q, want none", records)
	}
}

// TestWritingADatabaseTakesItBackOffTheDeletionList covers a handle a title
// keeps across its own delete. The records go to the key the list hides, so
// without the unmark they would be written and then unreadable for ever — the
// guest file table pairs every write with the same unmark.
func TestWritingADatabaseTakesItBackOffTheDeletionList(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.saveStore = NewDirectorySaveStore(t.TempDir())
	runtime.markRecordDatabaseRemoved(recordDatabaseRemovedKey, "SAVE", true)
	store := &runtimeRecordDatabase{name: "SAVE", records: [][]byte{[]byte("written")}}
	runtime.persistRecordDatabase(store)
	if runtime.recordDatabaseRemovals(recordDatabaseRemovedKey)["SAVE"] {
		t.Fatal("a database that was written is still on the deletion list")
	}
	runtime.recordDatabases = nil
	if handle := openRecordDatabase(t, runtime, "SAVE", 8, 0); handle == wipicErrorNotFound {
		t.Fatal("the record written through a surviving handle is unreachable")
	}
}

// TestDeletingThroughOneTableHidesTheArchiveFromTheOther pins what the two
// tables share. They keep separate stores and separate lists, but one archive:
// a packaged copy hidden by one table's delete has to be hidden by both, or
// the record a title cleared comes straight back through the other door.
func TestDeletingThroughOneTableHidesTheArchiveFromTheOther(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.saveStore = NewDirectorySaveStore(t.TempDir())
	index, data := splitRecordDatabase(4, []byte("aaaa"))
	runtime.guestFiles = map[string][]byte{"save.idx": index, "save.db": data}
	runtime.markRecordDatabaseRemoved(javaDatabaseRemovedKey, "save", true)
	if handle := openRecordDatabase(t, runtime, "save", 4, 0); handle != wipicErrorNotFound {
		t.Fatalf("the C table served the archive's copy of a database the Java table deleted: %#x", handle)
	}
}

// TestRemovingAReservedNameCannotWipeTheListItNames covers the way into the
// file table's own lists that the open and the rename do not. Removing writes
// nil over the key, and the deletion list answers as a seed as soon as
// anything has been deleted — so the removal went through and wiped it, and
// every database the title had deleted came back on the next open.
func TestRemovingAReservedNameCannotWipeTheListItNames(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.saveStore = NewDirectorySaveStore(t.TempDir())
	runtime.markDatabaseRemoved("SAVE", true)
	before, ok := runtime.loadSave(databaseRemovedKey)
	if !ok || string(before) != "SAVE" {
		t.Fatalf("removal list = %q, want the name that was deleted", before)
	}
	const nameAddress = platformDataBase + 0x8000
	if err := runtime.client.core.Memory().Write(nameAddress, append([]byte(".removed"), 0)); err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.Context{})
	if err := thread.SetRegister(0, nameAddress); err != nil {
		t.Fatal(err)
	}
	if result, err := runtime.handleWIPICFileCall(thread, wipicFileDelete); err != nil || result != wipicErrorInvalid {
		t.Fatalf("removing the list = %#x, err = %v, want it refused", result, err)
	}
	after, ok := runtime.loadSave(databaseRemovedKey)
	if !ok || string(after) != string(before) {
		t.Fatalf("removal list = %q, want it untouched", after)
	}
}
