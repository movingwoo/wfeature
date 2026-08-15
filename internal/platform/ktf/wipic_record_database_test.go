package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// packedRecordDatabase builds the packaged format: the header, then one slot
// per record with a live flag in front of each.
func packedRecordDatabase(recordSize int, records ...[]byte) []byte {
	file := make([]byte, recordDatabaseFileHeader)
	copy(file, recordDatabaseFileMagic)
	binary.LittleEndian.PutUint32(file[8:12], uint32(recordSize))
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
	binary.LittleEndian.PutUint32(zeroed[8:12], 0)
	if _, ok := parseRecordDatabaseFile(zeroed); ok {
		t.Fatal("parsed a file claiming a zero record size")
	}
}
