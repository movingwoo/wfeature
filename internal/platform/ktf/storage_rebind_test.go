package ktf

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// These cover what a load does with the storage tables a title cannot be made
// to fill by a key press in the authored archives: the WIPI C file table, the
// record databases and the Java databases. The tables are planted in one
// runtime, taken through the heap record, and filled in a second runtime from
// a store that has moved on.

// reboundStorage is a runtime restored from a record and filled from a store.
type reboundStorage struct {
	t       *testing.T
	client  *Client
	runtime *initializationRuntime
	store   *backend.MemorySaveStore
	probe   *saveStoreProbe
	roots   []*jvm.Object
	buffer  uint32
	thread  *armcore.Thread
}

// restoreStorage takes planted tables through their record into a fresh
// runtime over the given saves. The storage is not filled yet: bind does that.
func restoreStorage(t *testing.T, plant func(*initializationRuntime) []*jvm.Object, saves map[string][]byte) *reboundStorage {
	t.Helper()
	_, source := newTestRuntime(t)
	objects := plant(source)
	saved, err := source.captureHeapState(objects)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded runtimeHeapState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	store := testSaveStore(t, saves)
	probe := &saveStoreProbe{MemorySaveStore: store}
	client, fresh := newTestRuntime(t)
	client.AttachSaveStore(probe)
	roots, err := fresh.restoreHeapState(decoded)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	buffer, err := fresh.Allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	return &reboundStorage{t: t, client: client, runtime: fresh, store: store, probe: probe, roots: roots, buffer: buffer,
		thread: armcore.NewThread(armcore.NewContext())}
}

func (storage *reboundStorage) bind() error {
	storage.t.Helper()
	if err := storage.client.bindRestoredStorage(saveAdapterState{Version: saveAdapterVersion}, storage.probe); err != nil {
		return err
	}
	storage.runtime.restoredStorage = nil
	return nil
}

func (storage *reboundStorage) mustBind() *reboundStorage {
	storage.t.Helper()
	if err := storage.bind(); err != nil {
		storage.t.Fatalf("bind: %v", err)
	}
	return storage
}

func (storage *reboundStorage) set(arguments ...uint32) {
	storage.t.Helper()
	for index, argument := range arguments {
		if err := storage.thread.SetRegister(index, argument); err != nil {
			storage.t.Fatal(err)
		}
	}
}

// text puts a terminated string in guest memory, past the scratch buffer.
func (storage *reboundStorage) text(text string) uint32 {
	storage.t.Helper()
	address, err := storage.runtime.Allocate(uint64(len(text) + 1))
	if err != nil {
		storage.t.Fatal(err)
	}
	if err := storage.client.Core().Memory().Write(address, append([]byte(text), 0)); err != nil {
		storage.t.Fatal(err)
	}
	return address
}

func (storage *reboundStorage) stored(key string) string {
	storage.t.Helper()
	data, found := storage.store.LoadSave(key)
	if !found {
		return "<absent>"
	}
	return string(data)
}

// writeFile writes text through an open C file handle, at its cursor.
func (storage *reboundStorage) writeFile(handle uint32, text string) {
	storage.t.Helper()
	if err := storage.client.Core().Memory().Write(storage.buffer, []byte(text)); err != nil {
		storage.t.Fatal(err)
	}
	storage.set(handle, storage.buffer, uint32(len(text)))
	if count, err := storage.runtime.WIPICFileStream(storage.thread, true); err != nil || count != uint32(len(text)) {
		storage.t.Fatalf("write through %#x = %d, %v", handle, count, err)
	}
}

func (storage *reboundStorage) openFile(name string, mode uint32) uint32 {
	storage.t.Helper()
	storage.set(storage.text(name), mode)
	handle, err := storage.runtime.WIPICFileOpen(storage.thread)
	if err != nil {
		storage.t.Fatalf("open %q: %v", name, err)
	}
	return handle
}

func (storage *reboundStorage) fileExists(name string) bool {
	storage.t.Helper()
	storage.set(storage.text(name))
	result, err := storage.runtime.HandleWIPICFileCall(storage.thread, wipicrt.WIPICFileExists)
	if err != nil {
		storage.t.Fatal(err)
	}
	return result == 0
}

// A name that is gone from the store leaves its handle on an empty store that
// the catalog no longer binds, so questions about the name are answered from
// the store: it is missing. The name still has one host store. Whichever comes
// first — a write through the kept handle, or an open of the name — the other
// then shares it, and a rename onto the name takes the kept handles with it.
func TestStorageRebindKeepsOneStoreForACFileThatIsGone(t *testing.T) {
	plant := func(source *initializationRuntime) []*jvm.Object {
		gone := &wipicrt.RuntimeCFile{Name: "save", Data: []byte("captured")}
		other := &wipicrt.RuntimeCFile{Name: "other", Data: []byte("captured other")}
		source.CFiles = map[string]*wipicrt.RuntimeCFile{"save": gone, "other": other}
		source.CFileHandles = map[uint32]*wipicrt.RuntimeCFileHandle{0x1001: {Store: gone, Position: 1}}
		source.NextCDatabaseHandle = 1
		return nil
	}
	saves := map[string][]byte{"db/other": []byte("bbb")}
	detached := func(t *testing.T) *reboundStorage {
		storage := restoreStorage(t, plant, saves).mustBind()
		kept := storage.runtime.CFileHandles[0x1001]
		if kept == nil || len(kept.Store.Data) != 0 || kept.Position != 1 || storage.runtime.CFiles["save"] != nil || storage.runtime.DetachedCFiles["save"] != kept.Store {
			t.Fatalf("the handle on a file that is gone came back as %+v", kept)
		}
		if storage.fileExists("save") || storage.stored("db/save") != "<absent>" || storage.probe.writes != 0 {
			t.Fatal("the load made the file that was gone, or says it is there")
		}
		return storage
	}
	t.Run("a write through the kept handle, then an open", func(t *testing.T) {
		storage := detached(t)
		storage.writeFile(0x1001, "XY")
		kept := storage.runtime.CFileHandles[0x1001].Store
		if storage.stored("db/save") != "\x00XY" || storage.runtime.CFiles["save"] != kept || len(storage.runtime.DetachedCFiles) != 0 {
			t.Fatalf("the write stored %q and left the catalog at %v", storage.stored("db/save"), storage.runtime.CFiles["save"])
		}
		opened := storage.openFile("save", 2)
		if storage.runtime.CFileHandles[opened].Store != kept {
			t.Fatal("an open after the write made a second store for the name")
		}
	})
	t.Run("an open, then a write through the kept handle", func(t *testing.T) {
		storage := detached(t)
		kept := storage.runtime.CFileHandles[0x1001].Store
		opened := storage.openFile("save", 2)
		if storage.runtime.CFileHandles[opened].Store != kept || storage.runtime.CFiles["save"] != kept || len(storage.runtime.DetachedCFiles) != 0 {
			t.Fatal("the open did not take the store the kept handle is on")
		}
		storage.writeFile(opened, "ab")
		storage.writeFile(0x1001, "Z")
		if storage.stored("db/save") != "aZ" {
			t.Fatalf("the two handles wrote %q, want one file", storage.stored("db/save"))
		}
	})
	t.Run("a rename onto the name", func(t *testing.T) {
		storage := detached(t)
		storage.set(storage.text("other"), storage.text("save"))
		if result, err := storage.runtime.WIPICFileRename(storage.thread); err != nil || result != 0 {
			t.Fatalf("rename = %#x, %v", result, err)
		}
		renamed := storage.runtime.CFiles["save"]
		if renamed == nil || storage.runtime.CFileHandles[0x1001].Store != renamed || len(storage.runtime.DetachedCFiles) != 0 {
			t.Fatal("the kept handle did not follow the file that has the name now")
		}
		storage.writeFile(0x1001, "Z")
		if storage.stored("db/save") != "bZb" {
			t.Fatalf("a write through the kept handle stored %q", storage.stored("db/save"))
		}
	})
	t.Run("a remove of the name", func(t *testing.T) {
		storage := detached(t)
		storage.set(storage.text("save"))
		if result, err := storage.runtime.WIPICFileDelete(storage.thread); err != nil || result != wipicrt.WIPICErrorNotFound {
			t.Fatalf("remove of a name that is gone = %#x, %v", result, err)
		}
		if storage.runtime.CFileHandles[0x1001] == nil || storage.probe.writes != 0 {
			t.Fatal("removing a name that is gone closed its handle or wrote")
		}
	})
}

// The record databases follow the same rule, and a record that is gone with
// its database answers the table's own error, with nothing stored.
func TestStorageRebindKeepsOneStoreForARecordDatabaseThatIsGone(t *testing.T) {
	plant := func(source *initializationRuntime) []*jvm.Object {
		gone := &runtimeRecordDatabase{name: "slots", records: [][]byte{[]byte("captured")}, recordSize: 8}
		source.recordDatabases = map[string]*runtimeRecordDatabase{"slots": gone}
		source.recordDatabaseHandles = map[uint32]*runtimeRecordDatabaseHandle{0x2001: {store: gone}}
		source.nextRecordDatabaseHandle = 1
		return nil
	}
	detached := func(t *testing.T) *reboundStorage {
		storage := restoreStorage(t, plant, nil).mustBind()
		kept := storage.runtime.recordDatabaseHandles[0x2001]
		if kept == nil || len(kept.store.records) != 0 || kept.store.recordSize != 8 || storage.runtime.recordDatabases["slots"] != nil || storage.runtime.detachedRecordDatabases["slots"] != kept.store {
			t.Fatalf("the handle on a database that is gone came back as %+v", kept)
		}
		return storage
	}
	insert := func(storage *reboundStorage, handle uint32, text string) {
		t.Helper()
		if err := storage.client.Core().Memory().Write(storage.buffer, []byte(text)); err != nil {
			t.Fatal(err)
		}
		storage.set(handle, storage.buffer, uint32(len(text)))
		if id, err := storage.runtime.wipicRecordDatabaseInsert(storage.thread); err != nil || id == wipicrt.WIPICErrorInvalid {
			t.Fatalf("insert through %#x = %#x, %v", handle, id, err)
		}
	}
	open := func(storage *reboundStorage) uint32 {
		t.Helper()
		storage.set(storage.text("slots"), 8, 1)
		handle, err := storage.runtime.wipicRecordDatabaseOpen(storage.thread)
		if err != nil {
			t.Fatal(err)
		}
		return handle
	}
	t.Run("an update of a record that is gone", func(t *testing.T) {
		storage := detached(t)
		storage.set(0x2001, 1, storage.buffer, 4)
		if result, err := storage.runtime.wipicRecordDatabaseUpdate(storage.thread); err != nil || result != wipicrt.WIPICErrorInvalid {
			t.Fatalf("update = %#x, %v", result, err)
		}
		if storage.probe.writes != 0 || storage.stored("rdb/slots") != "<absent>" {
			t.Fatal("an update of a record that is gone wrote to the store")
		}
	})
	t.Run("an insert through the kept handle, then an open", func(t *testing.T) {
		storage := detached(t)
		insert(storage, 0x2001, "new")
		kept := storage.runtime.recordDatabaseHandles[0x2001].store
		if storage.runtime.recordDatabases["slots"] != kept || len(storage.runtime.detachedRecordDatabases) != 0 || storage.stored("rdb/slots") != string(wipicrt.EncodeSaveRecords([][]byte{[]byte("new")})) {
			t.Fatal("the insert did not make the database again under the kept store")
		}
		if storage.runtime.recordDatabaseHandles[open(storage)].store != kept {
			t.Fatal("an open after the insert made a second store for the name")
		}
	})
	t.Run("an open, then an insert through the kept handle", func(t *testing.T) {
		storage := detached(t)
		kept := storage.runtime.recordDatabaseHandles[0x2001].store
		opened := open(storage)
		if storage.runtime.recordDatabaseHandles[opened].store != kept || storage.runtime.recordDatabases["slots"] != kept || len(storage.runtime.detachedRecordDatabases) != 0 {
			t.Fatal("the open did not take the store the kept handle is on")
		}
		insert(storage, opened, "one")
		insert(storage, 0x2001, "two")
		if storage.stored("rdb/slots") != string(wipicrt.EncodeSaveRecords([][]byte{[]byte("one"), []byte("two")})) {
			t.Fatal("the two handles did not write one database")
		}
	})
}

// The Java databases follow it too. An object the title kept is on the store
// of its name whatever the catalog did since, so an open of the name takes
// that store, and a write through the object puts the name back.
func TestStorageRebindKeepsOneStoreForAJavaDatabaseThatIsGone(t *testing.T) {
	plant := func(source *initializationRuntime) []*jvm.Object {
		gone := &runtimeDataBaseStore{name: "save", records: [][]byte{[]byte("captured")}}
		source.databases = map[string]*runtimeDataBaseStore{"save": gone}
		return []*jvm.Object{{ClassName: "org/kwis/msp/db/DataBase", Native: gone}}
	}
	// The title deleted its database after the checkpoint: the emptied save
	// and the name on the list are what a delete leaves.
	saves := map[string][]byte{"jdb/save": wipicrt.EncodeSaveRecords(nil), javaDatabaseRemovedKey: wipicrt.JoinRemovalList([]string{"save"})}
	detached := func(t *testing.T) (*reboundStorage, *runtimeDataBaseStore) {
		storage := restoreStorage(t, plant, saves).mustBind()
		kept := storage.roots[0].Native.(*runtimeDataBaseStore)
		if len(kept.records) != 0 || storage.runtime.databases["save"] != nil || storage.runtime.detachedDatabases["save"] != kept {
			t.Fatalf("the object on a database that is gone came back as %+v", kept)
		}
		return storage, kept
	}
	insert := func(storage *reboundStorage, object *jvm.Object, text string) {
		t.Helper()
		arguments := []jvm.Value{jvm.ReferenceValue(object), jvm.ReferenceValue(jvm.NewByteArray([]byte(text)))}
		if _, err := runtimeDataBaseInsert(storage.runtime, storage.client.vm, arguments); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	open := func(storage *reboundStorage) *jvm.Object {
		t.Helper()
		name := jvm.ReferenceValue(storage.client.vm.NewString("save"))
		value, err := runtimeOpenDataBase(storage.runtime, storage.client.vm, []jvm.Value{name, jvm.IntValue(8), jvm.IntValue(1)})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		object, _ := value.Reference()
		return object
	}
	t.Run("an update of a record that is gone", func(t *testing.T) {
		storage, _ := detached(t)
		arguments := []jvm.Value{jvm.ReferenceValue(storage.roots[0]), jvm.IntValue(0), jvm.ReferenceValue(jvm.NewByteArray([]byte("x")))}
		_, err := runtimeDataBaseUpdate(storage.runtime, storage.client.vm, arguments)
		var thrown *jvm.GuestException
		if !errors.As(err, &thrown) || storage.probe.writes != 0 {
			t.Fatalf("an update of a record that is gone = %v after %d writes, want the table's exception and none", err, storage.probe.writes)
		}
	})
	t.Run("an insert through the kept object, then an open", func(t *testing.T) {
		storage, kept := detached(t)
		insert(storage, storage.roots[0], "new")
		if storage.runtime.databases["save"] != kept || len(storage.runtime.detachedDatabases) != 0 || storage.stored("jdb/save") != string(wipicrt.EncodeSaveRecords([][]byte{[]byte("new")})) {
			t.Fatal("the insert did not make the database again under the kept store")
		}
		if strings.Contains(storage.stored(javaDatabaseRemovedKey), "save") {
			t.Fatal("the database the title wrote is still on the removal list")
		}
		if open(storage).Native != any(kept) {
			t.Fatal("an open after the insert made a second store for the name")
		}
	})
	t.Run("an open, then an insert through the kept object", func(t *testing.T) {
		storage, kept := detached(t)
		opened := open(storage)
		if opened.Native != any(kept) || storage.runtime.databases["save"] != kept || len(storage.runtime.detachedDatabases) != 0 {
			t.Fatal("the open did not take the store the kept object is on")
		}
		insert(storage, opened, "one")
		insert(storage, storage.roots[0], "two")
		if storage.stored("jdb/save") != string(wipicrt.EncodeSaveRecords([][]byte{[]byte("one"), []byte("two")})) {
			t.Fatal("the two objects did not write one database")
		}
	})
}

// A database the title deleted while an object held it is kept for the next
// open of its name in the running session, and a checkpoint taken then comes
// back the same way: the object is on the one store of the name, and an open
// after the load takes it.
func TestStorageRebindKeepsADatabaseDeletedWhileHeld(t *testing.T) {
	saves := map[string][]byte{"jdb/save": wipicrt.EncodeSaveRecords(nil), javaDatabaseRemovedKey: wipicrt.JoinRemovalList([]string{"save"})}
	storage := restoreStorage(t, func(source *initializationRuntime) []*jvm.Object {
		source.client.SaveStore = testSaveStore(t, nil)
		vm := source.client.JVM()
		name := jvm.ReferenceValue(vm.NewString("save"))
		value, err := runtimeOpenDataBase(source, vm, []jvm.Value{name, jvm.IntValue(8), jvm.IntValue(1)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeDataBaseDeleteStore(source, vm, []jvm.Value{name}); err != nil {
			t.Fatal(err)
		}
		object, _ := value.Reference()
		if source.detachedDatabases["save"] != object.Native {
			t.Fatal("the delete did not keep the store the object holds")
		}
		return []*jvm.Object{object}
	}, saves).mustBind()
	kept := storage.roots[0].Native.(*runtimeDataBaseStore)
	if storage.runtime.databases["save"] != nil || storage.runtime.detachedDatabases["save"] != kept {
		t.Fatal("the held store did not come back as the name's one store")
	}
	value, err := runtimeOpenDataBase(storage.runtime, storage.client.vm, []jvm.Value{
		jvm.ReferenceValue(storage.client.vm.NewString("save")), jvm.IntValue(8), jvm.IntValue(1)})
	if err != nil {
		t.Fatal(err)
	}
	opened, _ := value.Reference()
	if opened.Native != any(kept) {
		t.Fatal("an open after the load made a second store for the name")
	}
}

// What a title is shown when it lists its saves after a load is the names the
// restored run knew that are still there. A name it knew that is gone is not
// listed, and neither is one that was first written after the checkpoint,
// which can still be opened by name: a listing here never reads the store.
func TestStorageRebindListsTheNamesTheRestoredRunKnew(t *testing.T) {
	storage := restoreStorage(t, func(source *initializationRuntime) []*jvm.Object {
		source.GuestFiles = map[string][]byte{"kept.sav": []byte("captured"), "gone.sav": []byte("captured")}
		source.databases = map[string]*runtimeDataBaseStore{"kept": {name: "kept"}, "gone": {name: "gone"}}
		return nil
	}, map[string][]byte{
		"fs/kept.sav":  []byte("now"),
		"fs/later.sav": []byte("written after the checkpoint"),
		"jdb/kept":     wipicrt.EncodeSaveRecords([][]byte{[]byte("now")}),
		"jdb/later":    wipicrt.EncodeSaveRecords([][]byte{[]byte("after")}),
	}).mustBind()
	files := make([]string, 0, len(storage.runtime.GuestFiles))
	for name := range storage.runtime.GuestFiles {
		files = append(files, name)
	}
	databases := make([]string, 0, len(storage.runtime.databases))
	for name := range storage.runtime.databases {
		databases = append(databases, name)
	}
	sort.Strings(files)
	sort.Strings(databases)
	if !reflect.DeepEqual(files, []string{"kept.sav"}) || !reflect.DeepEqual(databases, []string{"kept"}) {
		t.Fatalf("the listings name files %q and databases %q", files, databases)
	}
	if data, found := storage.runtime.GuestFile("later.sav"); !found || string(data) != "written after the checkpoint" {
		t.Fatal("a file written after the checkpoint cannot be opened by name")
	}
}

// A save a restored object needs that cannot be read, or does not decode,
// refuses the bind. Nothing is written, the read failure is not kept, and the
// same record binds once the store can be read.
func TestStorageRebindRefusesUnreadableAndCorruptSaves(t *testing.T) {
	plant := func(source *initializationRuntime) []*jvm.Object {
		file := &wipicrt.RuntimeCFile{Name: "stream", Data: []byte("c")}
		source.CFiles = map[string]*wipicrt.RuntimeCFile{"stream": file}
		source.CFileHandles = map[uint32]*wipicrt.RuntimeCFileHandle{0x1001: {Store: file}}
		source.NextCDatabaseHandle = 1
		records := &runtimeRecordDatabase{name: "records", recordSize: 4}
		source.recordDatabases = map[string]*runtimeRecordDatabase{"records": records}
		database := &runtimeDataBaseStore{name: "slot"}
		source.databases = map[string]*runtimeDataBaseStore{"slot": database}
		source.GuestFiles = map[string][]byte{"written": []byte("w")}
		return []*jvm.Object{{ClassName: "org/kwis/msp/io/File", Native: &runtimeGuestFile{name: "open.bin"}}}
	}
	healthy := map[string][]byte{
		"db/stream":   []byte("stream"),
		"rdb/records": wipicrt.EncodeSaveRecords([][]byte{[]byte("r")}),
		"jdb/slot":    wipicrt.EncodeSaveRecords([][]byte{[]byte("j")}),
		"fs/written":  []byte("written"),
		"fs/open.bin": []byte("open"),
	}
	refused := func(t *testing.T, storage *reboundStorage) {
		t.Helper()
		before, _ := storage.store.SnapshotSaves()
		err := storage.bind()
		if !errors.Is(err, backend.ErrCheckpointSaveRead) {
			t.Fatalf("bind = %v", err)
		}
		after, _ := storage.store.SnapshotSaves()
		if storage.runtime.SaveReadError != nil || storage.runtime.restoredStorage == nil || storage.client.SaveStore != wipicrt.SaveStore(storage.probe) || storage.probe.writes != 0 || !reflect.DeepEqual(before, after) {
			t.Fatalf("the refused bind left a read failure, a bound runtime or a write behind: %v", storage.runtime.SaveReadError)
		}
	}
	for _, key := range []string{guestFileRemovedKey, wipicrt.DatabaseRemovedKey, wipicrt.DirectoryListKey, recordDatabaseRemovedKey, javaDatabaseRemovedKey,
		"db/stream", "rdb/records", "jdb/slot", "fs/written", "fs/open.bin"} {
		t.Run("unreadable "+key, func(t *testing.T) {
			storage := restoreStorage(t, plant, healthy)
			storage.probe.unreadable = key
			refused(t, storage)
			storage.probe.unreadable = ""
			storage.mustBind()
			if string(storage.runtime.CFiles["stream"].Data) != "stream" || string(storage.runtime.databases["slot"].records[0]) != "j" ||
				string(storage.roots[0].Native.(*runtimeGuestFile).data) != "open" {
				t.Fatal("the bind after the refused one did not fill the storage")
			}
		})
	}
	// A record database with no save of its own is looked for in what the
	// archive ships, and those probes read the store as well.
	t.Run("unreadable packaged-copy probe", func(t *testing.T) {
		only := func(source *initializationRuntime) []*jvm.Object {
			store := &runtimeRecordDatabase{name: "records", recordSize: 4}
			source.recordDatabases = map[string]*runtimeRecordDatabase{"records": store}
			source.recordDatabaseHandles = map[uint32]*runtimeRecordDatabaseHandle{0x2001: {store: store}}
			source.nextRecordDatabaseHandle = 1
			return nil
		}
		storage := restoreStorage(t, only, map[string][]byte{})
		storage.probe.unreadable = "fs/records.idx"
		refused(t, storage)
	})
	for _, key := range []string{"rdb/records", "jdb/slot"} {
		t.Run("corrupt "+key, func(t *testing.T) {
			saves := map[string][]byte{}
			for name, data := range healthy {
				saves[name] = data
			}
			saves[key] = []byte("not a record list")
			refused(t, restoreStorage(t, plant, saves))
		})
	}
}

// The names in a record are untrusted, so what a bind reads and keeps is
// bounded: every object's copy counts, and each key is read once however many
// objects name it.
func TestStorageRebindIsBoundedAndReadsEachSaveOnce(t *testing.T) {
	plant := func(count int) func(*initializationRuntime) []*jvm.Object {
		return func(*initializationRuntime) []*jvm.Object {
			objects := make([]*jvm.Object, count)
			for index := range objects {
				objects[index] = &jvm.Object{ClassName: "org/kwis/msp/io/File", Native: &runtimeGuestFile{name: "big.bin"}}
			}
			return objects
		}
	}
	saves := map[string][]byte{"fs/big.bin": bytes.Repeat([]byte{'x'}, 16)}
	fits := restoreStorage(t, plant(4), saves)
	fits.runtime.restoredStorage.budget = 64
	fits.mustBind()
	reads := 0
	for index, root := range fits.roots {
		state := root.Native.(*runtimeGuestFile)
		if len(state.data) != 16 || index > 0 && &state.data[0] == &fits.roots[0].Native.(*runtimeGuestFile).data[0] {
			t.Fatal("the objects of one name do not each hold their own copy of it")
		}
	}
	// One read of the file, whatever the lists cost.
	reads = fits.probe.reads
	again := restoreStorage(t, plant(1), saves).mustBind()
	if reads != again.probe.reads {
		t.Fatalf("four objects of one name made %d store reads and one made %d", reads, again.probe.reads)
	}

	over := restoreStorage(t, plant(5), saves)
	over.runtime.restoredStorage.budget = 64
	if err := over.bind(); !errors.Is(err, backend.ErrCheckpointSaveRead) || over.probe.writes != 0 {
		t.Fatalf("five copies of sixteen bytes on a budget of sixty-four = %v", err)
	}
	// A single save over what is left of the budget is refused where it is
	// read, before it is copied anywhere.
	large := restoreStorage(t, plant(1), saves)
	large.runtime.restoredStorage.budget = 15
	if err := large.bind(); !errors.Is(err, backend.ErrCheckpointSaveRead) {
		t.Fatalf("a save larger than the budget = %v", err)
	}
}

// Every field of the runtime that names storage is accounted for here, so
// that a table added later cannot be restored from a checkpoint by accident:
// it has to be given one of these answers, and a test that shows it.
func TestStorageFieldsAreAccountedFor(t *testing.T) {
	const (
		identity = "identity in the slot"
		rebound  = "read from the store when the load commits"
		absent   = "not captured"
	)
	accounted := map[string]string{
		// Which names are open under which handles, at which cursor, and the
		// sequences the next handle is taken from.
		"CFileHandles":             identity,
		"NextCDatabaseHandle":      identity,
		"recordDatabaseHandles":    identity,
		"nextRecordDatabaseHandle": identity,
		// The catalogs and the written-file table travel as names; what they
		// hold, and which of the names still exist, is the store's answer.
		"CFiles":          rebound,
		"recordDatabases": rebound,
		"databases":       rebound,
		"GuestFiles":      rebound,
		// The lists are never in a slot: each is read from the store.
		"RemovedFiles":         rebound,
		"RemovedCDatabases":    rebound,
		"MadeDirectories":      rebound,
		"removedDatabaseLists": rebound,
		"madeGuestDirectories": rebound,
		"databaseSizes":        rebound,
		// What the store had nothing for, which is decided at the bind.
		"DetachedCFiles":          rebound,
		"detachedRecordDatabases": rebound,
		"detachedDatabases":       rebound,
		// A session with a failed read is not captured, and a bind that meets
		// one refuses the load; the names still to be filled exist only
		// between a restore and its bind.
		"SaveReadError":   absent,
		"restoredStorage": absent,
		// Writes held for one guest DataOutputStream call, stored before that
		// call returns; a checkpoint is never taken inside one.
		"fileBatch": absent,
		// Counts of this session's writes per path, which a restored File
		// starts from nothing; a rebind is not a write.
		"GuestFileWrites": absent,
	}
	words := []string{"file", "database", "removed", "director", "save", "storage", "record", "detached"}
	// The engine's fields are the runtime's too, through the embedded
	// engine runtime.
	var names []string
	for _, fields := range []reflect.Type{reflect.TypeOf(initializationRuntime{}), reflect.TypeOf(wipicrt.Runtime{})} {
		for index := 0; index < fields.NumField(); index++ {
			names = append(names, fields.Field(index).Name)
		}
	}
	seen := map[string]bool{}
	for _, name := range names {
		lowered := strings.ToLower(name)
		for _, word := range words {
			if strings.Contains(lowered, word) {
				seen[name] = true
				if accounted[name] == "" {
					t.Errorf("runtime field %s names storage and is not accounted for: say whether a checkpoint carries it, a bind reads it, or neither", name)
				}
				break
			}
		}
	}
	for name := range accounted {
		if !seen[name] {
			t.Errorf("accounted field %s is not a storage field of the runtime any more", name)
		}
	}

	// And the answers are true. Every table is planted with a marker, the
	// whole client record is encoded, and no marker is in it; the restored
	// tables then hold what the store has.
	options := continuationFixtureOptions{Runnable: true, Storage: true}
	source := newContinuationFixture(t, 1700000000, options)
	source.start(t)
	source.runtime.GuestFiles = map[string][]byte{"fixture.bin": []byte("MARKER-written")}
	source.runtime.CFiles["fixture-c"].Data = []byte("MARKER-c-file")
	source.runtime.recordDatabases["fixture-r"].records = [][]byte{[]byte("MARKER-record")}
	source.runtime.databases["fixture"].records = [][]byte{[]byte("MARKER-database")}
	source.runtime.RemovedFiles = map[string]bool{"MARKER-removed-file": true}
	source.runtime.RemovedCDatabases = map[string]bool{"MARKER-removed-c": true}
	source.runtime.MadeDirectories = map[string]bool{"MARKER-directory": true}
	source.runtime.removedDatabaseLists = map[string]map[string]bool{javaDatabaseRemovedKey: {"MARKER-removed-java": true}, recordDatabaseRemovedKey: {"MARKER-removed-record": true}}
	file, _ := source.object.Fields["snapshotFile"].Reference()
	file.Native.(*runtimeGuestFile).data = []byte("MARKER-file-object")
	saved := captureClientForTest(t, source.client)
	encoded, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"MARKER-written", "MARKER-c-file", "MARKER-record", "MARKER-database", "MARKER-removed-file", "MARKER-removed-c",
		"MARKER-directory", "MARKER-removed-java", "MARKER-removed-record", "MARKER-file-object"} {
		if bytes.Contains(encoded, []byte(marker)) || bytes.Contains(encoded, []byte(jsonBytes(t, marker))) {
			t.Errorf("the client record carries %q", marker)
		}
	}
	source.client.StopThreads()
	fresh := restoreClientForTest(t, roundTripClientState(t, saved), 1800000000, options)
	fresh.checkStorage(t)
	if !reflect.DeepEqual(fresh.runtime.GuestFiles, map[string][]byte{"fixture.bin": []byte("cursor")}) || !reflect.DeepEqual(fresh.runtime.RemovedFiles, map[string]bool{"deleted": true}) ||
		fresh.runtime.RemovedCDatabases == nil || fresh.runtime.MadeDirectories == nil || len(fresh.runtime.removedDatabaseLists) != 2 || fresh.runtime.restoredStorage != nil || fresh.runtime.SaveReadError != nil {
		t.Fatal("a restored table is not the store's answer")
	}
}

// A checkpoint is refused over the storage a load would refuse to read back:
// what the tables and the File objects hold is charged when it is taken,
// although none of it is recorded.
func TestStorageCaptureIsRefusedOverTheBudgetALoadHas(t *testing.T) {
	half := make([]byte, maxHeapStorageBytes/2)
	for name, plant := range map[string]func(*initializationRuntime) []*jvm.Object{
		"two C files": func(source *initializationRuntime) []*jvm.Object {
			source.CFiles = map[string]*wipicrt.RuntimeCFile{"a": {Name: "a", Data: half}, "b": {Name: "b", Data: half}}
			return nil
		},
		"a C file and a File object": func(source *initializationRuntime) []*jvm.Object {
			source.CFiles = map[string]*wipicrt.RuntimeCFile{"a": {Name: "a", Data: half}}
			return []*jvm.Object{{ClassName: "org/kwis/msp/io/File", Native: &runtimeGuestFile{name: "b", data: half}}}
		},
		"a written file and a database": func(source *initializationRuntime) []*jvm.Object {
			source.GuestFiles = map[string][]byte{"a": half}
			source.databases = map[string]*runtimeDataBaseStore{"b": {name: "b", records: [][]byte{half}}}
			return nil
		},
		"a record database and a C file": func(source *initializationRuntime) []*jvm.Object {
			source.recordDatabases = map[string]*runtimeRecordDatabase{"a": {name: "a", records: [][]byte{half}}}
			source.CFiles = map[string]*wipicrt.RuntimeCFile{"b": {Name: "b", Data: half}}
			return nil
		},
	} {
		_, source := newTestRuntime(t)
		objects := plant(source)
		if _, err := source.captureHeapState(objects); err == nil || !strings.Contains(err.Error(), "storage") {
			t.Errorf("%s holding the whole budget and more were captured: %v", name, err)
		}
	}
	// Half of it is a checkpoint like any other.
	_, source := newTestRuntime(t)
	source.CFiles = map[string]*wipicrt.RuntimeCFile{"a": {Name: "a", Data: half}}
	if _, err := source.captureHeapState(nil); err != nil {
		t.Fatalf("a capture inside the budget: %v", err)
	}
}
