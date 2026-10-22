package lgt

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The Java database half of the common storage contract, docs/wipi-storage.md:
// org.kwis.msp.db.DataBase, bullet by bullet.

const (
	dbOpen       = javaDataBaseClass + ".openDataBase(Ljava/lang/String;IZ)Lorg/kwis/msp/db/DataBase;"
	dbOpenIn     = javaDataBaseClass + ".openDataBase(Ljava/lang/String;IZI)Lorg/kwis/msp/db/DataBase;"
	dbDelete     = javaDataBaseClass + ".deleteDataBase(Ljava/lang/String;)V"
	dbAccessMode = javaDataBaseClass + ".getAccessMode(Ljava/lang/String;)I"
	dbList       = javaDataBaseClass + ".listDataBases()[Ljava/lang/String;"
	dbSort       = javaDataBaseClass + ".sortRecord(Lorg/kwis/msp/db/DataFilter;Lorg/kwis/msp/db/DataComparator;)[I"
	dbModified   = javaDataBaseClass + ".getLastModified()J"
)

// databaseFixture drives one client's databases.
type databaseFixture struct {
	t      *testing.T
	client *Client
}

func (fixture databaseFixture) text(value string) uint32 {
	return newTestString(fixture.t, fixture.client, value)
}

func (fixture databaseFixture) open(name string, recordSize int32, create bool) (uint32, error) {
	fixture.t.Helper()
	flag := uint32(0)
	if create {
		flag = 1
	}
	return javaCall(fixture.t, fixture.client, dbOpen, fixture.text(name), uint32(recordSize), flag)
}

func (fixture databaseFixture) mustOpen(name string, recordSize int32, create bool) uint32 {
	fixture.t.Helper()
	object, err := fixture.open(name, recordSize, create)
	if err != nil {
		fixture.t.Fatalf("openDataBase(%q, %d, %v): %v", name, recordSize, create, err)
	}
	return object
}

func (fixture databaseFixture) insert(object uint32, data string) uint32 {
	fixture.t.Helper()
	identifier, err := javaInsertRecord(fixture.client, nil, fixture.client.thread, []uint32{object, mustByteArray(fixture.t, fixture.client, data)})
	if err != nil {
		fixture.t.Fatalf("insertRecord(%q): %v", data, err)
	}
	return identifier
}

func (fixture databaseFixture) names() []string {
	fixture.t.Helper()
	array := mustJavaCall(fixture.t, fixture.client, dbList)
	if array == 0 {
		return nil
	}
	block, length, err := fixture.client.javaArrayBlock(array)
	if err != nil {
		fixture.t.Fatal(err)
	}
	names := make([]string, length)
	for index := range names {
		element, err := fixture.client.readWord(block + javaArrayLengthWords*4 + uint32(index)*4)
		if err != nil {
			fixture.t.Fatal(err)
		}
		names[index], _ = fixture.client.javaText(element)
	}
	return names
}

func newDatabaseFixture(t *testing.T, store backend.SaveStore) databaseFixture {
	t.Helper()
	client := fixtureClient(t)
	client.saveStore = store
	return databaseFixture{t: t, client: client}
}

// DataBaseException and DataBaseRecordException both extend Exception, and
// neither is the other's subclass, so a handler for one never takes the other.
func TestStorageContractDatabaseExceptions(t *testing.T) {
	for _, name := range []string{javaDataBaseExceptionClass, javaDataBaseRecordClass} {
		if super := javaPlatformSuper(name); super != "java/lang/Exception" {
			t.Errorf("%s extends %s, want java/lang/Exception", name, super)
		}
	}
	fixture := newDatabaseFixture(t, newMemorySaveStore())
	object := fixture.mustOpen("save", 8, true)
	// A missing record is the record exception and nothing else.
	if _, err := javaSelectRecord(fixture.client, nil, fixture.client.thread, []uint32{object, 0}); thrownClass(err) != javaDataBaseRecordClass {
		t.Fatalf("a missing record answered %v", err)
	}
}

// openDataBase: a name no database can have is DataBaseException, and so is
// null, as it is to every DataBase call; create with a record size of 0 or
// less is IllegalArgumentException, whether or not the database exists; a
// missing database is DataBaseException without create and is made at once
// with it, empty and listed.
func TestStorageContractDatabaseOpen(t *testing.T) {
	fixture := newDatabaseFixture(t, newMemorySaveStore())
	for _, name := range []string{
		"", "/", ".", "./", "..", "a/../b", "line\nbreak", strings.Repeat("d", 256), "./" + strings.Repeat("d", 256),
		"__databases__", "__DATABASES__", "/.created/x",
	} {
		if _, err := fixture.open(name, 8, true); thrownClass(err) != javaDataBaseExceptionClass {
			t.Errorf("openDataBase(%.12q) answered %v, want DataBaseException", name, err)
		}
	}
	if _, err := javaCall(t, fixture.client, dbOpen, 0, 8, 1); thrownClass(err) != javaDataBaseExceptionClass {
		t.Errorf("openDataBase(null) answered %v", err)
	}
	if _, err := javaCall(t, fixture.client, dbOpenIn, 0, 8, 1, 1); thrownClass(err) != javaDataBaseExceptionClass {
		t.Errorf("openDataBase(null, flag) answered %v", err)
	}
	for _, size := range []int32{0, -1} {
		if _, err := fixture.open("save", size, true); thrownClass(err) != javaIllegalArgumentClass {
			t.Errorf("openDataBase(create, %d) answered %v, want IllegalArgumentException", size, err)
		}
	}
	if _, err := fixture.open("save", 8, false); thrownClass(err) != javaDataBaseExceptionClass {
		t.Fatalf("opening a missing database without create answered %v", err)
	}
	if fixture.names() != nil {
		t.Fatal("a refused open listed a database")
	}
	object := fixture.mustOpen("save", 8, true)
	if count := mustJavaCall(t, fixture.client, javaDataBaseClass+".getNumberOfRecords()I", object); count != 0 {
		t.Fatalf("a new database holds %d records", count)
	}
	if names := fixture.names(); !reflect.DeepEqual(names, []string{"save"}) {
		t.Fatalf("a new database lists as %q", names)
	}
	// An existing database still refuses create with no record size.
	if _, err := fixture.open("save", 0, true); thrownClass(err) != javaIllegalArgumentClass {
		t.Errorf("openDataBase(existing, create, 0) answered %v", err)
	}
	if _, err := javaCall(t, fixture.client, dbOpenIn, fixture.text("save"), 8, 0, 3); err != nil {
		t.Errorf("the access flag form answered %v", err)
	}
	// A name of 255 bytes is one, counted on its path: the separators a
	// spelling adds are not part of it.
	if _, err := fixture.open(strings.Repeat("d", 255), 8, true); err != nil {
		t.Errorf("a 255-byte database name answered %v", err)
	}
	if _, err := fixture.open("//./"+strings.Repeat("d", 255), 8, false); err != nil {
		t.Errorf("another spelling of the 255-byte name answered %v", err)
	}
}

// A database name is a name like a file's, read by the same rule: a backslash
// separates segments as a slash does, leading separators, empty and "."
// segments are dropped, so every spelling of one path is one database, and a
// "/" inside a name is a directory. The path is the name the database answers
// to and is listed under.
func TestStorageContractDatabaseNames(t *testing.T) {
	store := newMemorySaveStore()
	fixture := newDatabaseFixture(t, store)
	client := fixture.client
	saved := fixture.mustOpen("/save", 8, true)
	fixture.insert(saved, "one")
	for _, spelling := range []string{"save", "./save", "//save", "\\save", "./\\./save"} {
		object, err := fixture.open(spelling, 8, false)
		if err != nil {
			t.Fatalf("openDataBase(%q) answered %v", spelling, err)
		}
		if got := string(databaseRecord(t, client, object, 0)); got != "one" {
			t.Fatalf("openDataBase(%q) reads %q, not the database /save made", spelling, got)
		}
	}
	name := mustJavaCall(t, client, javaDataBaseClass+".getDataBaseName()Ljava/lang/String;", saved)
	if text, _ := client.javaText(name); text != "save" {
		t.Fatalf("getDataBaseName = %q, want the path", text)
	}
	nested := fixture.mustOpen("dir\\scores", 8, true)
	fixture.insert(nested, "two")
	fixture.mustOpen(" spaced ", 8, true)
	if names := fixture.names(); !reflect.DeepEqual(names, []string{" spaced ", "dir/scores", "save"}) {
		t.Fatalf("listDataBases = %q", names)
	}
	if mode := mustJavaCall(t, client, dbAccessMode, fixture.text("./dir//scores")); mode != 1 {
		t.Fatalf("getAccessMode of another spelling = %d", mode)
	}
	// A database is not a file, so the one in "dir" does not make "dir" a
	// directory to the file calls (TestStorageContractContainersAreNoFiles).
	if exists := mustJavaCall(t, client, fsIsDirectory, newTestString(t, client, "dir")); exists != 0 {
		t.Fatal("a database made the directory it is in one to the file calls")
	}
	mustJavaCall(t, client, dbDelete, fixture.text("/dir/scores"))
	if names := fixture.names(); !reflect.DeepEqual(names, []string{" spaced ", "save"}) {
		t.Fatalf("after deleting another spelling listDataBases = %q", names)
	}
	later := newDatabaseFixture(t, store)
	if names := later.names(); !reflect.DeepEqual(names, []string{" spaced ", "save"}) {
		t.Fatalf("a later session lists %q", names)
	}
	if got := string(databaseRecord(t, later.client, later.mustOpen("\\save", 8, false), 0)); got != "one" {
		t.Fatalf("a later session reads %q", got)
	}
}

// A null name is DataBaseException to every DataBase call that takes a name.
func TestStorageContractDatabaseNullNames(t *testing.T) {
	fixture := newDatabaseFixture(t, newMemorySaveStore())
	for key, arguments := range map[string][]uint32{
		dbOpen: {0, 8, 1}, dbOpenIn: {0, 8, 1, 1}, dbDelete: {0},
		javaDataBaseClass + ".deleteDataBase(Ljava/lang/String;I)V": {0, 1},
		dbAccessMode: {0},
	} {
		if _, err := javaCall(t, fixture.client, key, arguments...); thrownClass(err) != javaDataBaseExceptionClass {
			t.Errorf("%s with a null name answered %v", key, err)
		}
	}
}

// The record size is the one the database was created with, and a container
// an earlier build wrote without one takes the size of the open that finds it
// and keeps it from then on, in a later session as well.
func TestStorageContractDatabaseRecordSize(t *testing.T) {
	store := newMemorySaveStore()
	fixture := newDatabaseFixture(t, store)
	object := fixture.mustOpen("save", 16, true)
	again := fixture.mustOpen("save", 32, true)
	for _, handle := range []uint32{object, again} {
		if size := mustJavaCall(t, fixture.client, javaDataBaseClass+".getRecordSize()I", handle); size != 16 {
			t.Fatalf("getRecordSize = %d, want the 16 it was created with", size)
		}
	}
	unsized := &databaseContent{records: [][]byte{[]byte("old")}, deleted: []bool{false}}
	store.entries[databaseSaveKey("legacy")] = unsized.encode()
	store.entries[databaseIndexKey] = []byte("legacy\nsave")
	legacy := fixture.mustOpen("legacy", 24, false)
	if size := mustJavaCall(t, fixture.client, javaDataBaseClass+".getRecordSize()I", legacy); size != 24 {
		t.Fatalf("a container with no size took %d, want the open's 24", size)
	}
	later := newDatabaseFixture(t, store)
	if size := mustJavaCall(t, later.client, javaDataBaseClass+".getRecordSize()I", later.mustOpen("legacy", 40, false)); size != 24 {
		t.Fatalf("in a later session the container's size is %d, want the 24 it kept", size)
	}
}

// Records: ids start at 0 and an insert takes the lowest id a delete freed;
// the record size is not enforced; selectRecord answers the bytes stored, and
// the form that fills a buffer copies them at the offset or is an
// IllegalArgumentException for a buffer they do not fit; an id that holds no
// record is DataBaseRecordException to select, update and delete;
// getNumberOfRecords counts the records and getDataBaseSize sums their bytes.
func TestStorageContractDatabaseRecords(t *testing.T) {
	fixture := newDatabaseFixture(t, newMemorySaveStore())
	client := fixture.client
	object := fixture.mustOpen("save", 4, true)
	for index, data := range []string{"zero", "one", "two", "three, longer than four"} {
		if id := fixture.insert(object, data); id != uint32(index) {
			t.Fatalf("insert %d answered id %d", index, id)
		}
	}
	for _, id := range []uint32{2, 1} {
		if _, err := javaDeleteRecord(client, nil, client.thread, []uint32{object, id}); err != nil {
			t.Fatal(err)
		}
	}
	if id := fixture.insert(object, "again"); id != 1 {
		t.Fatalf("an insert after deleting 2 and 1 took id %d, want 1", id)
	}
	if id := fixture.insert(object, "and"); id != 2 {
		t.Fatalf("the next insert took id %d, want 2", id)
	}
	if id := fixture.insert(object, "new"); id != 4 {
		t.Fatalf("an insert with no freed id took %d, want 4", id)
	}
	if got := string(databaseRecord(t, client, object, 3)); got != "three, longer than four" {
		t.Fatalf("a long record reads %q", got)
	}
	if count := mustJavaCall(t, client, javaDataBaseClass+".getNumberOfRecords()I", object); count != 5 {
		t.Fatalf("getNumberOfRecords = %d, want 5", count)
	}
	size := len("zero") + len("again") + len("and") + len("three, longer than four") + len("new")
	if got := mustJavaCall(t, client, javaDataBaseClass+".getDataBaseSize()I", object); got != uint32(size) {
		t.Fatalf("getDataBaseSize = %d, want %d", got, size)
	}
	buffer := mustByteArray(t, client, "........")
	if _, err := javaSelectRecordInto(client, nil, client.thread, []uint32{object, 2, buffer, 5}); err != nil {
		t.Fatal(err)
	}
	if data, _ := client.readJavaArrayBytes(buffer); string(data) != ".....and" {
		t.Fatalf("selectRecord into a buffer left %q", data)
	}
	for _, offset := range []uint32{6, 0xffffffff} {
		if _, err := javaSelectRecordInto(client, nil, client.thread, []uint32{object, 2, buffer, offset}); thrownClass(err) != javaIllegalArgumentClass {
			t.Errorf("selectRecord into a buffer at %d answered %v", int32(offset), err)
		}
	}
	missing := map[string]func() error{
		"selectRecord": func() error {
			_, err := javaSelectRecord(client, nil, client.thread, []uint32{object, 9})
			return err
		},
		"selectRecord into": func() error {
			_, err := javaSelectRecordInto(client, nil, client.thread, []uint32{object, 9, buffer, 0})
			return err
		},
		"updateRecord": func() error {
			_, err := javaUpdateRecord(client, nil, client.thread, []uint32{object, 9, mustByteArray(t, client, "x")})
			return err
		},
		"deleteRecord": func() error {
			_, err := javaDeleteRecord(client, nil, client.thread, []uint32{object, 9})
			return err
		},
	}
	for call, run := range missing {
		if err := run(); thrownClass(err) != javaDataBaseRecordClass {
			t.Errorf("%s of a missing id answered %v", call, err)
		}
	}
}

// getLastModified is the guest time of the last write in this session, or of
// the session's first open when nothing has been written since.
func TestStorageContractDatabaseLastModified(t *testing.T) {
	store := newMemorySaveStore()
	fixture := newDatabaseFixture(t, store)
	client := fixture.client
	modified := func(object uint32) int64 {
		low := mustJavaCall(t, client, dbModified, object)
		high, err := client.thread.Register(1)
		if err != nil {
			t.Fatal(err)
		}
		return int64(uint64(high)<<32 | uint64(low))
	}
	opened := client.clock.unixMillis()
	object := fixture.mustOpen("save", 8, true)
	if got := modified(object); got != opened {
		t.Fatalf("after the first open getLastModified = %d, want %d", got, opened)
	}
	client.clock.advance(5 * time.Second)
	if got := modified(fixture.mustOpen("save", 8, false)); got != opened {
		t.Fatalf("a later open moved getLastModified to %d", got)
	}
	fixture.insert(object, "x")
	written := client.clock.unixMillis()
	if written == opened {
		t.Fatal("the clock did not move")
	}
	client.clock.advance(5 * time.Second)
	if got := modified(fixture.mustOpen("save", 8, false)); got != written {
		t.Fatalf("after a write getLastModified = %d, want %d", got, written)
	}
	// A later session starts from its own first open.
	later := newDatabaseFixture(t, store)
	later.client.clock.advance(time.Hour)
	reopened := later.client.clock.unixMillis()
	reopenedObject := later.mustOpen("save", 8, false)
	low := mustJavaCall(t, later.client, dbModified, reopenedObject)
	high, _ := later.client.thread.Register(1)
	if got := int64(uint64(high)<<32 | uint64(low)); got != reopened {
		t.Fatalf("a later session's getLastModified = %d, want its open at %d", got, reopened)
	}
}

// getAccessMode is PRIVATE_ACCESS for a database that exists and
// DataBaseException for one that does not; listDataBases names every database
// in name order and is null when there is none; deleteDataBase of a missing
// database is DataBaseException, and an object opened before a delete and
// written through afterwards makes the database again.
func TestStorageContractDatabaseListDeleteAccess(t *testing.T) {
	store := newMemorySaveStore()
	fixture := newDatabaseFixture(t, store)
	client := fixture.client
	if fixture.names() != nil {
		t.Fatal("listDataBases with no database is not null")
	}
	if _, err := javaCall(t, client, dbAccessMode, fixture.text("save")); thrownClass(err) != javaDataBaseExceptionClass {
		t.Fatalf("getAccessMode of a missing database answered %v", err)
	}
	held := fixture.mustOpen("save", 8, true)
	fixture.mustOpen("alpha", 8, true)
	if mode := mustJavaCall(t, client, dbAccessMode, fixture.text("save")); mode != 1 {
		t.Fatalf("getAccessMode = %d, want PRIVATE_ACCESS", mode)
	}
	if names := fixture.names(); !reflect.DeepEqual(names, []string{"alpha", "save"}) {
		t.Fatalf("listDataBases = %q", names)
	}
	mustJavaCall(t, client, dbDelete, fixture.text("save"))
	if names := fixture.names(); !reflect.DeepEqual(names, []string{"alpha"}) {
		t.Fatalf("after a delete listDataBases = %q", names)
	}
	if _, err := javaCall(t, client, dbDelete, fixture.text("save")); thrownClass(err) != javaDataBaseExceptionClass {
		t.Fatalf("deleting a missing database answered %v", err)
	}
	if count := mustJavaCall(t, client, javaDataBaseClass+".getNumberOfRecords()I", held); count != 0 {
		t.Fatalf("an object on a deleted database holds %d records", count)
	}
	fixture.insert(held, "back")
	if mode := mustJavaCall(t, client, dbAccessMode, fixture.text("save")); mode != 1 {
		t.Fatalf("after a write through the held object getAccessMode = %d", mode)
	}
	if names := fixture.names(); !reflect.DeepEqual(names, []string{"alpha", "save"}) {
		t.Fatalf("after a write through the held object listDataBases = %q", names)
	}
	later := newDatabaseFixture(t, store)
	if names := later.names(); !reflect.DeepEqual(names, []string{"alpha", "save"}) {
		t.Fatalf("a later session lists %q", names)
	}
	if got := string(databaseRecord(t, later.client, later.mustOpen("save", 8, false), 0)); got != "back" {
		t.Fatalf("a later session reads %q", got)
	}
}

// A deletion the store refuses does not happen: deleteDataBase throws
// DataBaseException, and the database is still there, listed and whole.
func TestStorageContractDatabaseDeleteRefused(t *testing.T) {
	store := newFaultStore(t, nil)
	fixture := newDatabaseFixture(t, store)
	client := fixture.client
	object := fixture.mustOpen("save", 8, true)
	fixture.insert(object, "kept")
	store.refuse = true
	if _, err := javaCall(t, client, dbDelete, fixture.text("save")); thrownClass(err) != javaDataBaseExceptionClass {
		t.Fatalf("a deletion the store refused answered %v", err)
	}
	store.refuse = false
	if mode := mustJavaCall(t, client, dbAccessMode, fixture.text("save")); mode != 1 {
		t.Fatalf("after a refused deletion getAccessMode = %d", mode)
	}
	if names := fixture.names(); !reflect.DeepEqual(names, []string{"save"}) {
		t.Fatalf("after a refused deletion listDataBases = %q", names)
	}
	if got := string(databaseRecord(t, client, object, 0)); got != "kept" {
		t.Fatalf("after a refused deletion the record reads %q", got)
	}
}

// closeDataBase releases nothing a title can see: the object stays usable,
// and closing it twice is not an error.
func TestStorageContractDatabaseClose(t *testing.T) {
	fixture := newDatabaseFixture(t, newMemorySaveStore())
	client := fixture.client
	object := fixture.mustOpen("save", 8, true)
	for range 2 {
		if _, err := javaCloseDataBase(client, nil, client.thread, []uint32{object}); err != nil {
			t.Fatal(err)
		}
	}
	if id := fixture.insert(object, "after close"); id != 0 {
		t.Fatalf("an insert after close took id %d", id)
	}
	other := fixture.mustOpen("save", 8, false)
	if got := string(databaseRecord(t, client, other, 0)); got != "after close" {
		t.Fatalf("another object reads %q", got)
	}
}

// Space: getSizeAvailable is the title's whole room, which FileSystem.available
// and MC_fsAvailable answer too, and a database counts as the bytes of its
// records. One database's records hold at most 4 MiB, and a write past that
// is a DataBaseException.
func TestStorageContractDatabaseSpaceAndBound(t *testing.T) {
	fixture := newDatabaseFixture(t, newMemorySaveStore())
	client := fixture.client
	object := fixture.mustOpen("save", 8, true)
	fixture.insert(object, strings.Repeat("r", 3000))
	client.writeFile("file.dat", []byte(strings.Repeat("f", 1000)))
	want := uint32(1<<20 - 4000)
	if got := mustJavaCall(t, client, javaDataBaseClass+".getSizeAvailable()I", object); got != want {
		t.Fatalf("getSizeAvailable = %d, want %d", got, want)
	}
	if got := mustJavaCall(t, client, fsAvailable); got != want || callSlot(t, client, slotFsAvailable) != want {
		t.Fatalf("FileSystem.available = %d and MC_fsAvailable = %d, want %d", got, callSlot(t, client, slotFsAvailable), want)
	}
	big := mustByteArray(t, client, strings.Repeat("b", maxDatabaseBytes-3000))
	if _, err := javaInsertRecord(client, nil, client.thread, []uint32{object, big}); err != nil {
		t.Fatalf("records up to the bound were refused: %v", err)
	}
	if _, err := javaInsertRecord(client, nil, client.thread, []uint32{object, mustByteArray(t, client, "x")}); thrownClass(err) != javaDataBaseExceptionClass {
		t.Fatalf("an insert past the bound answered %v", err)
	}
	if _, err := javaUpdateRecord(client, nil, client.thread, []uint32{object, 0, mustByteArray(t, client, strings.Repeat("r", 3001))}); thrownClass(err) != javaDataBaseExceptionClass {
		t.Fatalf("an update past the bound answered %v", err)
	}
	if _, err := javaUpdateRecord(client, nil, client.thread, []uint32{object, 0, mustByteArray(t, client, "short")}); err != nil {
		t.Fatalf("an update that shrinks a record answered %v", err)
	}
	if got := mustJavaCall(t, client, javaDataBaseClass+".getSizeAvailable()I", object); got != 0 {
		t.Fatalf("past the room getSizeAvailable = %d, want 0", got)
	}
}

// installDatabaseCallback builds a title's object implementing one of
// sortRecord's interfaces with the Thumb routine given: by a member its class
// record names, or through the slot its interface table gives the interface,
// the way a class the compiler laid out itself carries it.
func installDatabaseCallback(t *testing.T, client *Client, name, iface, method string, offset uint32, byTable bool, code ...uint16) uint32 {
	t.Helper()
	body := installThumbAt(t, client, offset, code...)
	object, err := client.preparePlatformJavaClass("java/lang/Object")
	if err != nil {
		t.Fatal(err)
	}
	class := &javaRuntimeClass{Name: name, Handle: 1, Slots: object.Slots, Instance: 4, Super: object,
		Record: javaClass{Name: name}}
	const slot = 10
	if byTable {
		class.Record.Interfaces = []javaInterface{{Name: iface, Slot: slot}}
	} else {
		class.Record.Methods = []javaMember{{Name: method, Body: body}}
	}
	classObject, err := client.allocateJavaClassObject(class)
	if err != nil {
		t.Fatal(err)
	}
	class.Object = classObject
	class.dataBlock, _ = client.readWord(classObject + 8)
	runtime := client.javaRuntimeState()
	runtime.byObject[classObject] = class
	runtime.byName[name] = class
	if err := client.buildJavaVTable(class); err != nil {
		t.Fatal(err)
	}
	if byTable {
		if err := client.writeWord(class.VTable+4+slot*4, body); err != nil {
			t.Fatal(err)
		}
	}
	instance, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

// sortRecord: the records the filter accepts (all when it is null), in id
// order when the comparator is null and otherwise ordered by it, a positive
// answer placing its first record after the second, records it calls equal
// keeping their id order; no record answers null. The filter and the
// comparator are the title's own objects, called back into.
func TestStorageContractDatabaseSortRecord(t *testing.T) {
	fixture := newDatabaseFixture(t, newMemorySaveStore())
	client := fixture.client
	object := fixture.mustOpen("save", 4, true)
	for _, data := range []string{"\x03a", "\x01b", "\x02c", "\x01d", "\x00e", "\x03f"} {
		fixture.insert(object, data)
	}
	// filter: ldr r2,[r1,#8]; ldrb r0,[r2,#4]; bx lr — keeps a record whose
	// first byte is not zero.
	filter := installDatabaseCallback(t, client, "test/Filter", javaDataFilterInterface, "filter", 0, false,
		0x688a, 0x7910, 0x4770)
	// compare: the first byte of data1 less that of data2, ascending; the
	// second answers the opposite, through an interface table.
	ascending := installDatabaseCallback(t, client, "test/Ascending", javaDataComparatorInterface, "compare", 16, false,
		0x688b, 0x7918, 0x6893, 0x7919, 0x1a40, 0x4770)
	descending := installDatabaseCallback(t, client, "test/Descending", javaDataComparatorInterface, "compare", 32, true,
		0x688b, 0x7918, 0x6893, 0x7919, 0x1a08, 0x4770)
	sorted := func(filter, comparator uint32) []uint32 {
		t.Helper()
		array := mustJavaCall(t, client, dbSort, object, filter, comparator)
		if array == 0 {
			return nil
		}
		block, length, err := client.javaArrayBlock(array)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]uint32, length)
		for index := range ids {
			if ids[index], err = client.readWord(block + javaArrayLengthWords*4 + uint32(index)*4); err != nil {
				t.Fatal(err)
			}
		}
		return ids
	}
	for name, check := range map[string]struct {
		filter, comparator uint32
		want               []uint32
	}{
		"neither":                {0, 0, []uint32{0, 1, 2, 3, 4, 5}},
		"a filter":               {filter, 0, []uint32{0, 1, 2, 3, 5}},
		"a comparator":           {0, ascending, []uint32{4, 1, 3, 2, 0, 5}},
		"both, descending":       {filter, descending, []uint32{0, 5, 2, 1, 3}},
		"descending, unfiltered": {0, descending, []uint32{0, 5, 2, 1, 3, 4}},
	} {
		if got := sorted(check.filter, check.comparator); !reflect.DeepEqual(got, check.want) {
			t.Errorf("sortRecord with %s = %v, want %v", name, got, check.want)
		}
	}
	if _, err := javaDeleteRecord(client, nil, client.thread, []uint32{object, 2}); err != nil {
		t.Fatal(err)
	}
	if got := sorted(0, 0); !reflect.DeepEqual(got, []uint32{0, 1, 3, 4, 5}) {
		t.Errorf("after a delete sortRecord = %v", got)
	}
	empty := fixture.mustOpen("empty", 4, true)
	if array := mustJavaCall(t, client, dbSort, empty, 0, 0); array != 0 {
		t.Error("sortRecord of an empty database is not null")
	}
	none := fixture.mustOpen("zeros", 4, true)
	fixture.insert(none, "\x00")
	if array := mustJavaCall(t, client, dbSort, none, filter, ascending); array != 0 {
		t.Error("sortRecord with every record filtered out is not null")
	}
	// An object that implements neither interface cannot be called back into.
	stranger, err := newTestObject(t, client, javaVectorClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaCall(t, client, dbSort, object, stranger, 0); thrownClass(err) != javaDataBaseExceptionClass {
		t.Errorf("sortRecord with an object that is no filter answered %v", err)
	}
}

// What a database holds is there for a later session, which reads it from the
// same directory store.
func TestStorageContractDatabasePersistsAcrossSessions(t *testing.T) {
	root := t.TempDir()
	first := newDatabaseFixture(t, backend.NewDirectorySaveStore(root))
	object := first.mustOpen("save", 8, true)
	first.insert(object, "kept")
	first.insert(object, "gone")
	if _, err := javaDeleteRecord(first.client, nil, first.client.thread, []uint32{object, 1}); err != nil {
		t.Fatal(err)
	}
	second := newDatabaseFixture(t, backend.NewDirectorySaveStore(root))
	reopened := second.mustOpen("save", 16, false)
	if got := string(databaseRecord(t, second.client, reopened, 0)); got != "kept" {
		t.Fatalf("a later session reads %q", got)
	}
	if size := mustJavaCall(t, second.client, javaDataBaseClass+".getRecordSize()I", reopened); size != 8 {
		t.Fatalf("a later session's record size is %d", size)
	}
	if id := second.insert(reopened, "reused"); id != 1 {
		t.Fatalf("a later session's insert took id %d, want the freed 1", id)
	}
	if names := second.names(); !reflect.DeepEqual(names, []string{"save"}) {
		t.Fatalf("a later session lists %q", names)
	}
	if mode := mustJavaCall(t, second.client, dbAccessMode, second.text("save")); mode != 1 {
		t.Fatalf("a later session's access mode is %d", mode)
	}
}

// rootEntries answers the names MC_fsList gives for the root.
func rootEntries(files *contractFiles) (int32, []string) {
	code, held := files.list("", 256)
	end := bytes.Index(held, []byte{0, 0})
	if end < 0 {
		return code, nil
	}
	return code, strings.Split(string(held[:end]), "\x00")
}

// A database is not a file (docs/wipi-storage.md): no file call, C or Java,
// finds, lists or opens where a database is kept, a database "dir/scores" does
// not make "dir" a directory, and a title's own file "save.db" sits beside the
// database "save" without touching it.
func TestStorageContractDatabasesAreNoFiles(t *testing.T) {
	client, store, _ := reviewFiles(t, nil)
	fixture := databaseFixture{t: t, client: client}
	fixture.insert(fixture.mustOpen("save", 8, true), "kept")
	fixture.insert(fixture.mustOpen("dir/scores", 8, true), "deep")

	unseen := func(t *testing.T, client *Client) {
		t.Helper()
		files := newContractFiles(t, client)
		for _, name := range []string{"save.db", "save", "dir/scores.db", "dir/scores", "dir"} {
			if code := files.code(slotFsIsExist, name, 1); code != wipiNoEntry {
				t.Errorf("MC_fsIsExist(%q) = %d, want M_E_NOENT", name, code)
			}
			if got, err := javaCall(t, client, fsExists, newTestString(t, client, name)); err != nil || got != 0 {
				t.Errorf("exists(%q) = %d, %v", name, got, err)
			}
		}
		if code, listed := rootEntries(files); code != wipiSuccess || slices.Contains(listed, "save.db") || slices.Contains(listed, "dir") {
			t.Errorf("MC_fsList of the root = %d, %q, with a database's storage in it", code, listed)
		}
		if got := callSlot(t, client, slotFsAvailable); got != 1<<20-uint32(len("kept")+len("deep")) {
			t.Errorf("MC_fsAvailable = %d, want the two databases' records counted once", got)
		}
	}
	t.Run("this session", func(t *testing.T) { unseen(t, client) })
	t.Run("a later session", func(t *testing.T) {
		next := fixtureClient(t)
		next.saveStore = store
		unseen(t, next)
	})

	// A title's own file of the same name as a container is a file of its
	// own, and the database is as it was.
	files := newContractFiles(t, client)
	files.save("save.db", "file")
	if code := files.code(slotFsIsExist, "save.db", 1); code != wipiSuccess {
		t.Fatalf("MC_fsIsExist(save.db) after the title wrote it = %d", code)
	}
	if code, listed := rootEntries(files); code != wipiSuccess || !slices.Contains(listed, "save.db") || slices.Contains(listed, "dir") {
		t.Fatalf("MC_fsList of the root = %d, %q, want the title's save.db among the files", code, listed)
	}
	if got := string(databaseRecord(t, client, fixture.mustOpen("save", 8, false), 0)); got != "kept" {
		t.Fatalf("after the title's file of that name the database reads %q", got)
	}
	if code := files.code(slotFsRemove, "save.db", 1); code != wipiSuccess {
		t.Fatalf("MC_fsRemove(save.db) = %d", code)
	}
	next := fixtureClient(t)
	next.saveStore = store
	later := databaseFixture{t: t, client: next}
	for name, want := range map[string]string{"save": "kept", "dir/scores": "deep"} {
		if got := string(databaseRecord(t, next, later.mustOpen(name, 8, false), 0)); got != want {
			t.Errorf("in a later session database %q reads %q", name, got)
		}
	}
}
