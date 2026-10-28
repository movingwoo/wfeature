package skt

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The tests here hold SKT's WIPI DataBase to the database rules of
// docs/wipi-storage.md, one or more for each.

const wipiDatabaseDescriptor = "(Ljava/lang/String;IZ)Lorg/kwis/msp/db/DataBase;"

// wipiNullName stands for a null name where a test passes names as strings.
const wipiNullName = "\x00null"

func wipiNameArgument(runtime *Runtime, name string) jvm.Value {
	if name == wipiNullName {
		return jvm.ReferenceValue(nil)
	}
	return ruleText(runtime, name)
}

func wipiOpen(runtime *Runtime, name string, recordSize int32, create bool, flag ...int32) (*jvm.Object, error) {
	descriptor := wipiDatabaseDescriptor
	arguments := []jvm.Value{wipiNameArgument(runtime, name), jvm.IntValue(recordSize), booleanValue(create)}
	if len(flag) > 0 {
		descriptor = "(Ljava/lang/String;IZI)Lorg/kwis/msp/db/DataBase;"
		arguments = append(arguments, jvm.IntValue(flag[0]))
	}
	value, err := runtime.VM.InvokeStatic(wipi.DataBaseClass, "openDataBase", descriptor, arguments...)
	if err != nil {
		return nil, err
	}
	object, err := value.Reference()
	if err == nil && (object == nil || object.ClassName != wipi.DataBaseClass) {
		err = errors.New("openDataBase answered no DataBase")
	}
	return object, err
}

func wipiMustOpen(t *testing.T, runtime *Runtime, name string, recordSize int32, create bool) *jvm.Object {
	t.Helper()
	db, err := wipiOpen(runtime, name, recordSize, create)
	if err != nil {
		t.Fatalf("openDataBase(%q, %d, %v): %v", name, recordSize, create, err)
	}
	return db
}

// wipiInt asks a DataBase one of its ()I questions.
func wipiInt(t *testing.T, runtime *Runtime, db *jvm.Object, method string) int32 {
	t.Helper()
	return storeReviewInt(t, wipiStreamCall(t, runtime, db, method, "()I"))
}

// wipiInsert inserts a whole array and answers the record id.
func wipiInsert(t *testing.T, runtime *Runtime, db *jvm.Object, record ...byte) int32 {
	t.Helper()
	return storeReviewInt(t, wipiStreamCall(t, runtime, db, "insertRecord", "([B)I", ruleBytes(record...)))
}

func wipiSelect(t *testing.T, runtime *Runtime, db *jvm.Object, id int32) []byte {
	t.Helper()
	array, _ := wipiStreamCall(t, runtime, db, "selectRecord", "(I)[B", jvm.IntValue(id)).Reference()
	data, err := jvm.ByteArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func wipiNames(t *testing.T, runtime *Runtime) []string {
	t.Helper()
	value, err := runtime.VM.InvokeStatic(wipi.DataBaseClass, "listDataBases", "()[Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	array, _ := value.Reference()
	if array == nil {
		return nil
	}
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(values))
	for _, value := range values {
		object, _ := value.Reference()
		name, _ := jvm.StringText(object)
		names = append(names, name)
	}
	if len(names) == 0 {
		t.Fatal("listDataBases answered an empty array; the contract says null")
	}
	return names
}

func wipiAccessMode(runtime *Runtime, name string) (int32, error) {
	value, err := runtime.VM.InvokeStatic(wipi.DataBaseClass, "getAccessMode", "(Ljava/lang/String;)I", wipiNameArgument(runtime, name))
	if err != nil {
		return 0, err
	}
	return value.Int32()
}

// A database outlives the session: a fresh runtime over the same directory
// finds it listed, its records under the ids the first session gave them, the
// deleted one still deleted, and the record size it was made with whatever
// the reopen asks. The layout is the documented one: the records under
// jdb/<name> in the shared record form, and jdb/.index naming the database
// with its record size.
func TestWIPIDataBasePersistsAcrossSessions(t *testing.T) {
	root := t.TempDir()
	first := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	db := wipiMustOpen(t, first, "save", 4, true)
	// Made at once, empty and listed.
	if count := wipiInt(t, first, db, "getNumberOfRecords"); count != 0 || !slices.Equal(wipiNames(t, first), []string{"save"}) {
		t.Fatalf("a made database holds %d records and lists as %q", count, wipiNames(t, first))
	}
	for index, record := range [][]byte{{1, 2, 3, 4}, {5}, {6, 7}} {
		if id := wipiInsert(t, first, db, record...); id != int32(index) {
			t.Fatalf("insertRecord #%d = %d; ids start at zero", index, id)
		}
	}
	wipiStreamCall(t, first, db, "deleteRecord", "(I)V", jvm.IntValue(1))
	wipiStreamCall(t, first, db, "updateRecord", "(I[BII)V", jvm.IntValue(2), ruleBytes(0, 8, 9, 0), jvm.IntValue(1), jvm.IntValue(2))
	if mode, err := wipiAccessMode(first, "save"); err != nil || mode != wipi.PrivateAccess {
		t.Fatalf("getAccessMode = %d, %v; want PRIVATE_ACCESS", mode, err)
	}
	// A close releases nothing a title can see: closing twice is no error,
	// and the object stays usable.
	wipiStreamCall(t, first, db, "closeDataBase", "()V")
	wipiStreamCall(t, first, db, "closeDataBase", "()V")
	if count := wipiInt(t, first, db, "getNumberOfRecords"); count != 2 {
		t.Fatalf("a closed object reads %d records", count)
	}
	if err := first.Destroy(true); err != nil {
		t.Fatal(err)
	}

	store := backend.NewDirectorySaveStore(root)
	if index, _ := store.LoadSave(wipiDatabaseIndexKey); string(index) != "4 save" {
		t.Fatalf("jdb/.index = %q", index)
	}
	data, _ := store.LoadSave("jdb/save")
	if records, err := backend.DecodeSaveRecords(data); err != nil || len(records) != 3 || records[1] != nil || !bytes.Equal(records[2], []byte{8, 9}) {
		t.Fatalf("jdb/save = %v, %v", records, err)
	}

	second := wipiStreamRuntime(t, store)
	if got := wipiNames(t, second); !slices.Equal(got, []string{"save"}) {
		t.Fatalf("listDataBases() in a later session = %q", got)
	}
	db = wipiMustOpen(t, second, "save", 99, false)
	if size := wipiInt(t, second, db, "getRecordSize"); size != 4 {
		t.Fatalf("getRecordSize = %d after a reopen asking for 99, want the 4 it was made with", size)
	}
	if count := wipiInt(t, second, db, "getNumberOfRecords"); count != 2 {
		t.Fatalf("getNumberOfRecords = %d", count)
	}
	if got := wipiSelect(t, second, db, 2); !bytes.Equal(got, []byte{8, 9}) {
		t.Fatalf("selectRecord(2) = %v, want the bytes stored", got)
	}
	// The id a delete freed is the one the next insert takes, then the next.
	if id := wipiInsert(t, second, db, 7); id != 1 {
		t.Fatalf("insertRecord after a delete = %d, want the freed id 1", id)
	}
	if id := wipiInsert(t, second, db, 8); id != 3 {
		t.Fatalf("insertRecord with no id free = %d, want 3", id)
	}
	if name, _ := wipiStreamCall(t, second, db, "getDataBaseName", "()Ljava/lang/String;").Reference(); name == nil {
		t.Fatal("getDataBaseName answered null")
	} else if text, _ := jvm.StringText(name); text != "save" {
		t.Fatalf("getDataBaseName = %q", text)
	}
}

// openDataBase: a name the store cannot keep is DataBaseException; create
// with a record size of 0 or less is IllegalArgumentException, whether or not
// the database is there; a database that is not there is DataBaseException
// without create. A name is a key's path, so the spellings of one key are one
// database, and a path below the scope is a name like any other.
func TestWIPIDataBaseOpenAndNames(t *testing.T) {
	root := t.TempDir()
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	isDataBase := func(err error) bool { return runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) }
	isArgument := func(err error) bool { return runtime.VM.IsGuestException(err, "java/lang/IllegalArgumentException") }
	if got := wipiNames(t, runtime); got != nil {
		t.Fatalf("listDataBases() with none = %q, want null", got)
	}
	if _, err := wipiOpen(runtime, "first", 4, false); !isDataBase(err) {
		t.Fatalf("opening a missing database without create: %v", err)
	}
	for _, size := range []int32{0, -1} {
		if _, err := wipiOpen(runtime, "first", size, true); !isArgument(err) {
			t.Fatalf("creating with record size %d: %v", size, err)
		}
	}
	if got := wipiNames(t, runtime); got != nil {
		t.Fatalf("refused opens made databases %q", got)
	}
	for _, name := range []string{wipiNullName, "", "/", ".", "..", "a/../..", "nul\x00name", ".index", ".index/x", "line\nbreak"} {
		if _, err := wipiOpen(runtime, name, 4, true); !isDataBase(err) {
			t.Fatalf("openDataBase(%q): %v", name, err)
		}
	}
	db := wipiMustOpen(t, runtime, "first", 4, true)
	if _, err := wipiOpen(runtime, "first", 0, true, wipi.SharedAccess); !isArgument(err) {
		t.Fatalf("create with record size 0 on a database that is there: %v", err)
	}
	again, err := wipiOpen(runtime, "/first", 0, false, wipi.SystemAccess)
	if err != nil || again.Native != db.Native {
		t.Fatalf("reopening without create: %v; the objects share one database: %v", err, err == nil && again.Native == db.Native)
	}
	nested := wipiMustOpen(t, runtime, "dir/save", 2, true)
	wipiInsert(t, runtime, nested, 5)
	if alias, err := wipiOpen(runtime, "./dir//save", 0, false); err != nil || alias.Native != nested.Native {
		t.Fatalf("another spelling of dir/save: %v", err)
	}
	if got := wipiNames(t, runtime); !slices.Equal(got, []string{"dir/save", "first"}) {
		t.Fatalf("listDataBases() = %q", got)
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	store := backend.NewDirectorySaveStore(root)
	if data, ok := store.LoadSave("jdb/dir/save"); !ok || len(data) == 0 {
		t.Fatal("a database named with a path is not under its key")
	}
	later := wipiStreamRuntime(t, store)
	if db := wipiMustOpen(t, later, "dir/save", 0, false); !bytes.Equal(wipiSelect(t, later, db, 0), []byte{5}) {
		t.Fatal("a database named with a path did not outlive the session")
	}
}

// The record rules: the record size is not enforced and a longer record is
// stored whole; selectRecord answers the bytes stored, and the buffer form
// copies them at the offset or throws IllegalArgumentException; an id with no
// record is DataBaseRecordException to select, update and delete, which is
// not a DataBaseException; getNumberOfRecords counts the records and
// getDataBaseSize sums their bytes.
func TestWIPIDataBaseRecordRules(t *testing.T) {
	root := t.TempDir()
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	db := wipiMustOpen(t, runtime, "records", 2, true)
	isRecord := func(err error) bool {
		return runtime.VM.IsGuestException(err, wipi.DataBaseRecordExceptionClass) && !runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass)
	}
	isArgument := func(err error) bool { return runtime.VM.IsGuestException(err, "java/lang/IllegalArgumentException") }
	call := func(method, descriptor string, arguments ...jvm.Value) error {
		_, err := runtime.VM.InvokeVirtual(db, method, descriptor, arguments...)
		return err
	}
	if id := wipiInsert(t, runtime, db, 1, 2, 3, 4, 5); id != 0 {
		t.Fatalf("a record longer than the record size = %d", id)
	}
	wipiInsert(t, runtime, db, 6)
	if got := wipiSelect(t, runtime, db, 0); !bytes.Equal(got, []byte{1, 2, 3, 4, 5}) {
		t.Fatalf("selectRecord(0) = %v, want the record whole", got)
	}
	if got := wipiSelect(t, runtime, db, 1); !bytes.Equal(got, []byte{6}) {
		t.Fatalf("selectRecord(1) = %v, want the one byte stored", got)
	}
	wipiStreamCall(t, runtime, db, "updateRecord", "(I[B)V", jvm.IntValue(1), ruleBytes(7, 7, 7))
	if size, count := wipiInt(t, runtime, db, "getDataBaseSize"), wipiInt(t, runtime, db, "getNumberOfRecords"); size != 8 || count != 2 {
		t.Fatalf("getDataBaseSize %d and getNumberOfRecords %d, want 8 and 2", size, count)
	}
	buffer := jvm.NewByteArray([]byte{9, 9, 9, 9, 9, 9})
	if err := call("selectRecord", "(I[BI)V", jvm.IntValue(1), jvm.ReferenceValue(buffer), jvm.IntValue(2)); err != nil {
		t.Fatal(err)
	}
	if got, _ := jvm.ByteArraySnapshot(buffer); !bytes.Equal(got, []byte{9, 9, 7, 7, 7, 9}) {
		t.Fatalf("selectRecord into a buffer = %v, want the record at offset 2", got)
	}
	for _, refused := range []struct {
		err      error
		want     func(error) bool
		describe string
	}{
		{call("selectRecord", "(I[BI)V", jvm.IntValue(0), ruleBytes(0, 0, 0, 0), jvm.IntValue(0)), isArgument, "a buffer the record does not fit"},
		{call("selectRecord", "(I[BI)V", jvm.IntValue(1), ruleBytes(0, 0, 0, 0), jvm.IntValue(2)), isArgument, "a record past the buffer's end"},
		{call("selectRecord", "(I[BI)V", jvm.IntValue(1), ruleBytes(0, 0, 0, 0), jvm.IntValue(-1)), isArgument, "a negative offset"},
		{call("insertRecord", "([BII)I", ruleBytes(1, 2), jvm.IntValue(1), jvm.IntValue(2)), isArgument, "a window past the array"},
		{call("selectRecord", "(I)[B", jvm.IntValue(2)), isRecord, "selecting an id past the end"},
		{call("selectRecord", "(I)[B", jvm.IntValue(-1)), isRecord, "selecting a negative id"},
		{call("selectRecord", "(I[BI)V", jvm.IntValue(5), ruleBytes(0, 0, 0, 0, 0, 0), jvm.IntValue(0)), isRecord, "selecting into a buffer from an id with no record"},
		{call("updateRecord", "(I[B)V", jvm.IntValue(5), ruleBytes(1)), isRecord, "updating an id with no record"},
		{call("deleteRecord", "(I)V", jvm.IntValue(3)), isRecord, "deleting an id with no record"},
	} {
		if !refused.want(refused.err) {
			t.Fatalf("%s: %v", refused.describe, refused.err)
		}
	}
	if err := call("insertRecord", "([B)I", jvm.ReferenceValue(nil)); !runtime.VM.IsGuestException(err, "java/lang/NullPointerException") {
		t.Fatalf("insertRecord(null): %v", err)
	}
	wipiStreamCall(t, runtime, db, "deleteRecord", "(I)V", jvm.IntValue(0))
	if err := call("deleteRecord", "(I)V", jvm.IntValue(0)); !isRecord(err) {
		t.Fatalf("deleting a deleted record: %v", err)
	}
	if size := wipiInt(t, runtime, db, "getDataBaseSize"); size != 3 {
		t.Fatalf("getDataBaseSize after a delete = %d", size)
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	later := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	reopened := wipiMustOpen(t, later, "records", 2, false)
	if got := wipiSelect(t, later, reopened, 1); !bytes.Equal(got, []byte{7, 7, 7}) {
		t.Fatalf("a record longer than the record size read back %v in a later session", got)
	}
}

// One database's records together hold at most 4 MiB: a write past that is
// DataBaseException, one that stays within it goes on, and a record list past
// it in the store is refused rather than read.
func TestWIPIDataBaseRecordsAreBoundedAt4MiB(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSaves(map[string][]byte{
		wipiDatabaseIndexKey: []byte("1 full\n1 over"),
		"jdb/full":           backend.EncodeSaveRecords([][]byte{make([]byte, wipiDatabaseMaxBytes)}),
		"jdb/over":           backend.EncodeSaveRecords([][]byte{make([]byte, wipiDatabaseMaxBytes), {1}}),
	}); err != nil {
		t.Fatal(err)
	}
	runtime := wipiStreamRuntime(t, store)
	db := wipiMustOpen(t, runtime, "full", 1, false)
	isDataBase := func(err error) bool { return runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) }
	if _, err := runtime.VM.InvokeVirtual(db, "insertRecord", "([B)I", ruleBytes(1)); !isDataBase(err) {
		t.Fatalf("an insert past 4 MiB: %v", err)
	}
	if count := wipiInt(t, runtime, db, "getNumberOfRecords"); count != 1 {
		t.Fatalf("a refused insert left %d records", count)
	}
	wipiStreamCall(t, runtime, db, "updateRecord", "(I[B)V", jvm.IntValue(0), ruleBytes(1, 2))
	if id := wipiInsert(t, runtime, db, 3); id != 1 {
		t.Fatalf("an insert within the bound = %d", id)
	}
	if _, err := wipiOpen(runtime, "over", 1, false); !isDataBase(err) {
		t.Fatalf("a record list past 4 MiB opened: %v", err)
	}
}

// Every object on one name is one database, which a delete empties and takes
// off the index. An object kept across the delete reads it empty, and a write
// through it makes the database again with its record size, in this session
// and the next. Deleting a database that is not there is DataBaseException.
func TestWIPIDataBaseObjectsShareRecordsAcrossADelete(t *testing.T) {
	root := t.TempDir()
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	writer := wipiMustOpen(t, runtime, "shared", 3, true)
	reader := wipiMustOpen(t, runtime, "shared", 3, false)
	wipiInsert(t, runtime, writer, 1, 2, 3)
	if got := wipiSelect(t, runtime, reader, 0); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("the second object reads %v", got)
	}
	if _, err := runtime.VM.InvokeStatic(wipi.DataBaseClass, "deleteDataBase", "(Ljava/lang/String;I)V", ruleText(runtime, "shared"), jvm.IntValue(wipi.PrivateAccess)); err != nil {
		t.Fatal(err)
	}
	if got := wipiNames(t, runtime); got != nil {
		t.Fatalf("listDataBases() after the delete = %q", got)
	}
	for _, name := range []string{"shared", "never", wipiNullName} {
		if _, err := runtime.VM.InvokeStatic(wipi.DataBaseClass, "deleteDataBase", "(Ljava/lang/String;)V", wipiNameArgument(runtime, name)); !runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) {
			t.Fatalf("deleting %q, which is not there: %v", name, err)
		}
		if _, err := wipiAccessMode(runtime, name); !runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) {
			t.Fatalf("getAccessMode(%q), which is not there: %v", name, err)
		}
	}
	if _, err := wipiOpen(runtime, "shared", 3, false); !runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) {
		t.Fatalf("opening a deleted database: %v", err)
	}
	if count := wipiInt(t, runtime, reader, "getNumberOfRecords"); count != 0 {
		t.Fatalf("an object kept across the delete reads %d records", count)
	}
	if id := wipiInsert(t, runtime, writer, 4); id != 0 {
		t.Fatalf("an insert into the deleted database = %d", id)
	}
	if got := wipiNames(t, runtime); !slices.Equal(got, []string{"shared"}) {
		t.Fatalf("a write did not make the database again: %q", got)
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	later := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	db := wipiMustOpen(t, later, "shared", 9, false)
	if size, count := wipiInt(t, later, db, "getRecordSize"), wipiInt(t, later, db, "getNumberOfRecords"); size != 3 || count != 1 {
		t.Fatalf("the database made again holds %d records of %d bytes", count, size)
	}
}

// A change the store refuses is not made: the call throws DataBaseException,
// the session's records stay as the store has them, and nothing waits to be
// written later — a title that tries again does not get the record twice.
func TestWIPIDataBaseRefusedStoreChangesNothing(t *testing.T) {
	store := &refusingBatchStore{DirectorySaveStore: backend.NewDirectorySaveStore(t.TempDir())}
	runtime := wipiStreamRuntime(t, store)
	store.refuse = true
	if _, err := wipiOpen(runtime, "refused", 2, true); !runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) {
		t.Fatalf("a refused creation: %v", err)
	}
	if _, err := wipiOpen(runtime, "refused", 2, false); !runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) {
		t.Fatalf("a refused creation made the database: %v", err)
	}
	store.refuse = false
	db := wipiMustOpen(t, runtime, "refused", 2, true)
	wipiInsert(t, runtime, db, 1)
	store.refuse = true
	isDataBase := func(err error) bool { return runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) }
	for _, refused := range []struct {
		method, descriptor string
		arguments          []jvm.Value
	}{
		{"insertRecord", "([B)I", []jvm.Value{ruleBytes(2)}},
		{"updateRecord", "(I[B)V", []jvm.Value{jvm.IntValue(0), ruleBytes(3)}},
		{"deleteRecord", "(I)V", []jvm.Value{jvm.IntValue(0)}},
	} {
		if _, err := runtime.VM.InvokeVirtual(db, refused.method, refused.descriptor, refused.arguments...); !isDataBase(err) {
			t.Fatalf("a refused %s: %v", refused.method, err)
		}
	}
	if _, err := runtime.VM.InvokeStatic(wipi.DataBaseClass, "deleteDataBase", "(Ljava/lang/String;)V", ruleText(runtime, "refused")); !isDataBase(err) {
		t.Fatalf("a refused deleteDataBase: %v", err)
	}
	if count := wipiInt(t, runtime, db, "getNumberOfRecords"); count != 1 {
		t.Fatalf("refused writes left %d records", count)
	}
	if got := wipiSelect(t, runtime, db, 0); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("a refused update changed the record: %v", got)
	}
	store.refuse = false
	writes := store.writes
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	if store.writes != writes {
		t.Fatal("a refused database write was stored at the session's end")
	}
	data, _ := store.LoadSave("jdb/refused")
	if records, err := backend.DecodeSaveRecords(data); err != nil || len(records) != 1 || !bytes.Equal(records[0], []byte{1}) {
		t.Fatalf("jdb/refused = %v, %v", records, err)
	}
}

// A save this runtime cannot read is refused rather than read in part, and is
// never written over: a title told its save is empty would write over it.
func TestWIPIDataBaseRefusesAnUnreadableSave(t *testing.T) {
	for _, corrupt := range []map[string][]byte{
		{wipiDatabaseIndexKey: []byte("x save")},
		{wipiDatabaseIndexKey: []byte("2 save\n2 save")},
		{wipiDatabaseIndexKey: []byte("2 ./save")},
		{wipiDatabaseIndexKey: []byte("2 save"), "jdb/save": {1, 2}},
		{"jdb/save": {9, 9, 9, 9, 9}},
	} {
		store := backend.NewDirectorySaveStore(t.TempDir())
		if err := store.StoreSaves(corrupt); err != nil {
			t.Fatal(err)
		}
		runtime := wipiStreamRuntime(t, store)
		if _, err := wipiOpen(runtime, "save", 2, true); !runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) {
			t.Fatalf("an unreadable save %q opened: %v", corrupt, err)
		}
		for key, data := range corrupt {
			if stored, _ := store.LoadSave(key); !bytes.Equal(stored, data) {
				t.Fatalf("an unreadable %s was written over: %q", key, stored)
			}
		}
	}
}

// A database whose stored form carries no record size — a record list the
// index does not name — is the title's database: it is listed, answers
// getAccessMode, takes the record size of the open that finds it and keeps it
// from then on, in the index and so in later sessions. An emptied list, which
// a deletion leaves, is no database.
func TestWIPIDataBaseWithoutAnIndexLine(t *testing.T) {
	root := t.TempDir()
	store := backend.NewDirectorySaveStore(root)
	if err := store.StoreSaves(map[string][]byte{
		"jdb/found":   backend.EncodeSaveRecords([][]byte{{1, 2}, nil, {3}}),
		"jdb/emptied": backend.EncodeSaveRecords(nil),
	}); err != nil {
		t.Fatal(err)
	}
	runtime := wipiStreamRuntime(t, store)
	if got := wipiNames(t, runtime); !slices.Equal(got, []string{"found"}) {
		t.Fatalf("listDataBases() = %q, want the record list the index does not name", got)
	}
	if mode, err := wipiAccessMode(runtime, "found"); err != nil || mode != wipi.PrivateAccess {
		t.Fatalf("getAccessMode(found) = %d, %v", mode, err)
	}
	if _, err := wipiOpen(runtime, "emptied", 4, false); !runtime.VM.IsGuestException(err, wipi.DataBaseExceptionClass) {
		t.Fatalf("an emptied record list opened: %v", err)
	}
	db := wipiMustOpen(t, runtime, "found", 7, false)
	if size, count := wipiInt(t, runtime, db, "getRecordSize"), wipiInt(t, runtime, db, "getNumberOfRecords"); size != 7 || count != 2 {
		t.Fatalf("getRecordSize %d, getNumberOfRecords %d; want the opening 7 and the 2 stored", size, count)
	}
	if again := wipiMustOpen(t, runtime, "found", 9, false); wipiInt(t, runtime, again, "getRecordSize") != 7 {
		t.Fatal("a second open changed the record size the first gave")
	}
	if index, _ := store.LoadSave(wipiDatabaseIndexKey); string(index) != "7 found" {
		t.Fatalf("jdb/.index = %q, want the record size kept", index)
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	later := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	if size := wipiInt(t, later, wipiMustOpen(t, later, "found", 3, false), "getRecordSize"); size != 7 {
		t.Fatalf("a later session's record size = %d, want 7", size)
	}
	if _, err := later.VM.InvokeStatic(wipi.DataBaseClass, "deleteDataBase", "(Ljava/lang/String;)V", ruleText(later, "found")); err != nil {
		t.Fatal(err)
	}
	if got := wipiNames(t, later); got != nil {
		t.Fatalf("listDataBases() after the delete = %q", got)
	}
}

// getLastModified is the guest time of the last write in this session, or of
// the session's first open when nothing has been written since.
func TestWIPIDataBaseLastModified(t *testing.T) {
	root := t.TempDir()
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	at := func(milliseconds int64) { runtime.now = func() int64 { return milliseconds } }
	modified := func(db *jvm.Object) int64 {
		value, err := wipiStreamCall(t, runtime, db, "getLastModified", "()J").Int64()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	at(1000)
	db := wipiMustOpen(t, runtime, "timed", 2, true)
	if got := modified(db); got != 1000 {
		t.Fatalf("after the creating open: %d", got)
	}
	at(2000)
	wipiInsert(t, runtime, db, 1)
	at(3000)
	again := wipiMustOpen(t, runtime, "timed", 2, false)
	if got, other := modified(db), modified(again); got != 2000 || other != 2000 {
		t.Fatalf("after a write at 2000 and an open at 3000: %d and %d", got, other)
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	later := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	// Asking about the database reads it, and is no open.
	later.now = func() int64 { return 4000 }
	if _, err := wipiAccessMode(later, "timed"); err != nil {
		t.Fatal(err)
	}
	later.now = func() int64 { return 5000 }
	reopened := wipiMustOpen(t, later, "timed", 2, false)
	later.now = func() int64 { return 6000 }
	if got, err := wipiStreamCall(t, later, reopened, "getLastModified", "()J").Int64(); err != nil || got != 5000 {
		t.Fatalf("a later session's first open at 5000 answers %d, %v", got, err)
	}
}

// wipiDefineCallbacks installs a filter that keeps records whose first byte
// is odd and a comparator ordering by the first byte — the comparator answers
// three times the difference, not the constants, which this sort reads by
// sign. They stand in for a title's own classes.
func wipiDefineCallbacks(t *testing.T, runtime *Runtime) (filter, comparator *jvm.Object) {
	t.Helper()
	construct := func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		object, _ := arguments[0].Reference()
		_, err := call.InvokeSpecial(object, "java/lang/Object", "<init>", "()V")
		return jvm.VoidValue(), err
	}
	first := func(value jvm.Value) int32 {
		array, _ := value.Reference()
		data, _ := jvm.ByteArraySnapshot(array)
		if len(data) != 1 {
			t.Errorf("a callback was handed %d bytes, want the one stored", len(data))
			return 0
		}
		return int32(data[0])
	}
	for _, definition := range []jvm.ClassDefinition{
		{Name: "test/OddFilter", SuperName: "java/lang/Object", Interfaces: []string{wipi.DataFilterClass}, Access: jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: construct},
				{Name: "filter", Descriptor: "([B)Z", Access: jvm.AccessPublic, Body: func(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
					return booleanValue(first(arguments[1])%2 == 1), nil
				}},
			}},
		{Name: "test/ByFirstByte", SuperName: "java/lang/Object", Interfaces: []string{wipi.DataComparatorClass}, Access: jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: construct},
				{Name: "compare", Descriptor: "([B[B)I", Access: jvm.AccessPublic, Body: func(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
					return jvm.IntValue(3 * (first(arguments[1]) - first(arguments[2]))), nil
				}},
			}},
	} {
		if err := runtime.VM.DefineClass(definition); err != nil {
			t.Fatal(err)
		}
	}
	return ruleNew(t, runtime, "test/OddFilter", "()V"), ruleNew(t, runtime, "test/ByFirstByte", "()V")
}

func wipiSort(t *testing.T, runtime *Runtime, db *jvm.Object, filter, comparator *jvm.Object) []int32 {
	t.Helper()
	value := wipiStreamCall(t, runtime, db, "sortRecord", "(Lorg/kwis/msp/db/DataFilter;Lorg/kwis/msp/db/DataComparator;)[I",
		jvm.ReferenceValue(filter), jvm.ReferenceValue(comparator))
	array, _ := value.Reference()
	if array == nil {
		return nil
	}
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int32, len(values))
	for index, value := range values {
		ids[index], _ = value.Int32()
	}
	if len(ids) == 0 {
		t.Fatal("sortRecord answered an empty array; the contract says null")
	}
	return ids
}

// sortRecord: the records the filter accepts (all with a null one), in id
// order with a null comparator and otherwise by it, a positive answer placing
// its first record after the second and equal records keeping their id order;
// no record answers null. Each callback gets the bytes stored.
func TestWIPIDataBaseSortRecord(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	db := wipiMustOpen(t, runtime, "sorted", 4, true)
	if ids := wipiSort(t, runtime, db, nil, nil); ids != nil {
		t.Fatalf("an empty database sorts to %v, want null", ids)
	}
	for _, first := range []byte{5, 1, 4, 2, 3, 1, 8} {
		wipiInsert(t, runtime, db, first)
	}
	wipiStreamCall(t, runtime, db, "deleteRecord", "(I)V", jvm.IntValue(6))
	filter, comparator := wipiDefineCallbacks(t, runtime)
	for _, check := range []struct {
		filter, comparator *jvm.Object
		want               []int32
	}{
		{nil, nil, []int32{0, 1, 2, 3, 4, 5}},
		{filter, nil, []int32{0, 1, 4, 5}},
		{nil, comparator, []int32{1, 5, 3, 4, 2, 0}},
		{filter, comparator, []int32{1, 5, 4, 0}},
	} {
		if ids := wipiSort(t, runtime, db, check.filter, check.comparator); !slices.Equal(ids, check.want) {
			t.Fatalf("sortRecord(%v, %v) = %v, want %v", check.filter != nil, check.comparator != nil, ids, check.want)
		}
	}
	for _, id := range []int32{0, 1, 4, 5} {
		wipiStreamCall(t, runtime, db, "deleteRecord", "(I)V", jvm.IntValue(id))
	}
	if ids := wipiSort(t, runtime, db, filter, comparator); ids != nil {
		t.Fatalf("a filter that keeps nothing sorts to %v, want null", ids)
	}
}

// A quick save keeps a database's name, record size and modified time, and a
// load reads its records from the saves as they are when it commits — a
// record written after the quick save is there — so the objects a restored
// title holds, and the next open of the name, share them.
func TestWIPIDataBaseCheckpointRestoresOverCurrentSaves(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	source := wipiStreamRuntime(t, store)
	db := wipiMustOpen(t, source, "progress", 2, true)
	wipiInsert(t, source, db, 1)
	captured := newCheckpointHeap(source, time.Now())
	payload, err := captured.captureNative(db.Native)
	if err != nil {
		t.Fatal(err)
	}
	modified := db.Native.(*wipiDatabase).modified
	wipiInsert(t, source, db, 2)

	target := wipiStreamRuntime(t, store)
	heap := newCheckpointHeap(target, time.Now())
	native, err := heap.restoreNative(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := heap.rebuildCheckpointSaves(store); err != nil {
		t.Fatal(err)
	}
	target.AttachSaveStore(store)
	restored := &jvm.Object{ClassName: wipi.DataBaseClass, Fields: make(map[string]jvm.Value), Native: native}
	if count := wipiInt(t, target, restored, "getNumberOfRecords"); count != 2 {
		t.Fatalf("a restored database holds %d records, want the 2 the saves hold", count)
	}
	if last, err := wipiStreamCall(t, target, restored, "getLastModified", "()J").Int64(); err != nil || last != modified {
		t.Fatalf("a restored database was modified at %d, want the quick save's %d", last, modified)
	}
	reopened := wipiMustOpen(t, target, "progress", 2, false)
	if reopened.Native != native {
		t.Fatal("an open after the load is not the restored database")
	}

	// Two databases of one name, a name no open could have made, or a time
	// before the epoch are no checkpoint this runtime wrote.
	if _, err := heap.restoreNative(payload); err == nil {
		t.Fatal("a second database of one name was restored")
	}
	for _, invalid := range []checkpointWIPIDatabase{{"", 2, 0}, {"./fine", 2, 0}, {".index", 2, 0}, {"fine", 2, -1}} {
		forged, err := nativeCheckpoint("wipi-database", invalid)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := newCheckpointHeap(target, time.Now()).restoreNative(forged); err == nil {
			t.Fatalf("restored the forged database %+v", invalid)
		}
	}
}
