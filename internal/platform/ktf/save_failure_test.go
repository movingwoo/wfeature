package ktf

import (
	"bytes"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

type failingSaveStore struct {
	memorySaveStore
	readError error
}

func (store failingSaveStore) StoreSave(string, []byte) error {
	return errors.New("injected write failure")
}
func (store failingSaveStore) StoreSaves(map[string][]byte) error {
	return errors.New("injected batch failure")
}
func (store failingSaveStore) ReadSave(name string) ([]byte, bool, error) {
	if store.readError != nil {
		return nil, false, store.readError
	}
	data, present := store.LoadSave(name)
	return data, present, nil
}

func TestFailedDatabaseDeletePreservesRecordsHandlesAndTombstones(t *testing.T) {
	client, runtime := newTestRuntime(t)
	store := &runtimeRecordDatabase{name: "save", records: [][]byte{[]byte("old")}}
	handle := uint32(recordDatabaseHandleBit | 1)
	runtime.recordDatabases = map[string]*runtimeRecordDatabase{"save": store}
	runtime.recordDatabaseHandles = map[uint32]*runtimeRecordDatabaseHandle{handle: {store: store}}
	old := wipicrt.EncodeSaveRecords(store.records)
	client.SaveStore = failingSaveStore{memorySaveStore: memorySaveStore{"rdb/save": old}}
	name, err := runtime.AllocateBytes([]byte("save\x00"))
	if err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	if err := thread.SetRegister(0, name); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.wipicRecordDatabaseDelete(thread); err == nil {
		t.Fatal("failed delete succeeded")
	}
	if runtime.recordDatabases["save"] != store || runtime.recordDatabaseHandles[handle].store != store || runtime.recordDatabaseRemovals(recordDatabaseRemovedKey)["save"] {
		t.Fatal("failed delete changed live database state")
	}
	if !bytes.Equal(wipicrt.EncodeSaveRecords(store.records), old) {
		t.Fatal("failed delete changed records")
	}
}

func TestFailedFileRecreationAndRenamePreserveRemovalLedger(t *testing.T) {
	client, runtime := newTestRuntime(t)
	disk := memorySaveStore{"fs/source": []byte("old"), guestFileRemovedKey: []byte("target\n")}
	client.SaveStore = failingSaveStore{memorySaveStore: disk}
	runtime.GuestFiles = map[string][]byte{"source": []byte("old")}
	if err := runtime.storeGuestFile("target", []byte("new")); err == nil {
		t.Fatal("failed recreation succeeded")
	}
	if !runtime.RemovedGuestFiles()["target"] {
		t.Fatal("failed recreation removed the tombstone")
	}
	_, err := runtimeFileSystemRename(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(client.vm.NewString("source")), jvm.ReferenceValue(client.vm.NewString("target"))})
	if err == nil {
		t.Fatal("failed rename succeeded")
	}
	if string(runtime.GuestFiles["source"]) != "old" || runtime.GuestFiles["target"] != nil || runtime.RemovedGuestFiles()["source"] || !runtime.RemovedGuestFiles()["target"] {
		t.Fatal("failed rename changed live paths")
	}
	if string(disk["fs/source"]) != "old" || disk["fs/target"] != nil {
		t.Fatal("failed rename changed disk entries")
	}
}

func TestFailedCRecordChangesPreserveSharedStore(t *testing.T) {
	for _, operation := range []string{"insert", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			store := &runtimeRecordDatabase{name: "save", records: [][]byte{[]byte("old")}}
			handle := uint32(recordDatabaseHandleBit | 1)
			runtime.recordDatabaseHandles = map[uint32]*runtimeRecordDatabaseHandle{handle: {store: store}}
			old := wipicrt.EncodeSaveRecords(store.records)
			client.SaveStore = failingSaveStore{memorySaveStore: memorySaveStore{"rdb/save": old}}
			buffer, err := runtime.AllocateBytes([]byte("new"))
			if err != nil {
				t.Fatal(err)
			}
			context := armcore.NewContext()
			context.Registers[0] = handle
			context.Registers[1], context.Registers[2], context.Registers[3] = 1, buffer, 3
			if operation == "insert" {
				context.Registers[1], context.Registers[2] = buffer, 3
			}
			thread := armcore.NewThread(context)
			switch operation {
			case "insert":
				_, err = runtime.wipicRecordDatabaseInsert(thread)
			case "update":
				_, err = runtime.wipicRecordDatabaseUpdate(thread)
			case "delete":
				_, err = runtime.wipicRecordDatabaseDeleteRecord(thread)
			}
			if err == nil || !bytes.Equal(wipicrt.EncodeSaveRecords(store.records), old) {
				t.Fatal("failed record change published live records")
			}
		})
	}
}

func TestFailedCFileChangesPreserveCursorAndOpenHandle(t *testing.T) {
	for _, operation := range []string{"write", "delete", "rename"} {
		t.Run(operation, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			store := &wipicrt.RuntimeCFile{Name: "save", Data: []byte("old")}
			handle := uint32(wipicrt.CFileHandleBit | 1)
			state := &wipicrt.RuntimeCFileHandle{Store: store, Position: 1}
			runtime.CFiles = map[string]*wipicrt.RuntimeCFile{"save": store}
			runtime.CFileHandles = map[uint32]*wipicrt.RuntimeCFileHandle{handle: state}
			client.SaveStore = failingSaveStore{memorySaveStore: memorySaveStore{"db/save": []byte("old")}}
			first, err := runtime.AllocateBytes([]byte("save\x00"))
			if err != nil {
				t.Fatal(err)
			}
			second, err := runtime.AllocateBytes([]byte("new\x00"))
			if err != nil {
				t.Fatal(err)
			}
			context := armcore.NewContext()
			context.Registers[0], context.Registers[1] = first, second
			if operation == "write" {
				context.Registers[0], context.Registers[2] = handle, 3
			}
			thread := armcore.NewThread(context)
			switch operation {
			case "write":
				_, err = runtime.WIPICFileStream(thread, true)
			case "delete":
				_, err = runtime.WIPICFileDelete(thread)
			case "rename":
				_, err = runtime.WIPICFileRename(thread)
			}
			if err == nil {
				t.Fatal("failed operation succeeded")
			}
			if state.Position != 1 || store.Name != "save" || string(store.Data) != "old" || runtime.CFiles["save"] != store || runtime.CFileHandles[handle] != state || runtime.RemovedDatabases()["save"] {
				t.Fatal("failed operation changed the file or its handle")
			}
		})
	}
}

func TestUnreadableLedgerStopsTheJavaStorageBoundary(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.SaveStore = failingSaveStore{readError: errors.New("ledger read failure")}
	_, err := client.vm.InvokeStatic("org/kwis/msp/io/FileSystem", "exists", "(Ljava/lang/String;)Z", jvm.ReferenceValue(client.vm.NewString("save")))
	if err == nil || runtime.SaveReadError == nil {
		t.Fatal("unreadable ledger reported a missing file")
	}
	if err := runtime.storeGuestFile("save", []byte("replacement")); err == nil {
		t.Fatal("read failure permitted a later overwrite")
	}
}

func TestCertificateBatchFailureDoesNotPublishPrivateLedger(t *testing.T) {
	base := failingSaveStore{memorySaveStore: memorySaveStore{wipicrt.DatabaseRemovedKey: []byte("old\n")}}
	store := newCertificateSaveStore(base, []byte("certificate"))
	before, _ := store.LoadSave(wipicrt.DatabaseRemovedKey)
	if err := store.StoreSaves(map[string][]byte{wipicrt.DatabaseRemovedKey: []byte("new\n"), "db/progress": []byte("new")}); err == nil {
		t.Fatal("failed batch succeeded")
	}
	after, _ := store.LoadSave(wipicrt.DatabaseRemovedKey)
	if !bytes.Equal(before, after) {
		t.Fatal("failed batch published the authentication ledger")
	}
}

func TestFailedFileWritePreservesBytesAndCursor(t *testing.T) {
	client, runtime := newTestRuntime(t)
	old := []byte("old")
	client.SaveStore = failingSaveStore{memorySaveStore: memorySaveStore{"fs/save": bytes.Clone(old)}}
	state := &runtimeGuestFile{name: "save", data: bytes.Clone(old), position: 1}
	file := &jvm.Object{ClassName: "org/kwis/msp/io/File", Native: state}
	_, err := runtimeFileWrite(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(file), jvm.ReferenceValue(jvm.NewByteArray([]byte("new")))})
	if err == nil {
		t.Fatal("failed write succeeded")
	}
	if state.position != 1 || !bytes.Equal(state.data, old) {
		t.Fatal("failed write changed file state")
	}
	if saved, _ := client.SaveStore.LoadSave("fs/save"); !bytes.Equal(saved, old) {
		t.Fatal("failed write changed stored bytes")
	}
}

func TestFailedJavaRecordMutationPreservesOpenStore(t *testing.T) {
	for _, operation := range []string{"insert", "update", "delete", "range"} {
		t.Run(operation, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			store := &runtimeDataBaseStore{name: "save", records: [][]byte{[]byte("old")}}
			old := wipicrt.EncodeSaveRecords(store.records)
			client.SaveStore = failingSaveStore{memorySaveStore: memorySaveStore{"jdb/save": bytes.Clone(old)}}
			object := &jvm.Object{ClassName: "org/kwis/msp/db/DataBase", Native: store}
			receiver, buffer := jvm.ReferenceValue(object), jvm.ReferenceValue(jvm.NewByteArray([]byte("new")))
			var err error
			switch operation {
			case "insert":
				_, err = runtimeDataBaseInsert(runtime, client.vm, []jvm.Value{receiver, buffer})
			case "update":
				_, err = runtimeDataBaseUpdate(runtime, client.vm, []jvm.Value{receiver, jvm.IntValue(0), buffer})
			case "delete":
				_, err = runtimeDataBaseDelete(runtime, client.vm, []jvm.Value{receiver, jvm.IntValue(0)})
			case "range":
				_, err = runtimeDataBaseUpdateRange(runtime, client.vm, []jvm.Value{receiver, jvm.IntValue(0), buffer, jvm.IntValue(0), jvm.IntValue(2)})
			}
			if err == nil {
				t.Fatal("failed mutation succeeded")
			}
			if !bytes.Equal(wipicrt.EncodeSaveRecords(store.records), old) {
				t.Fatal("failed mutation changed open records")
			}
			if saved, _ := client.SaveStore.LoadSave("jdb/save"); !bytes.Equal(saved, old) {
				t.Fatal("failed mutation changed stored records")
			}
		})
	}
}

func TestJavaDatabaseRejectsUnreadableCorruptAndFailedCreation(t *testing.T) {
	for _, mode := range []string{"read", "corrupt", "create"} {
		t.Run(mode, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			store := failingSaveStore{memorySaveStore: make(memorySaveStore)}
			if mode == "read" {
				store.readError = errors.New("injected read failure")
			}
			if mode == "corrupt" {
				store.memorySaveStore["jdb/save"] = []byte{1, 2}
			}
			client.SaveStore = store
			_, err := runtimeOpenDataBase(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(client.vm.NewString("save")), jvm.IntValue(0), jvm.IntValue(1)})
			if err == nil || runtime.databases["save"] != nil {
				t.Fatal("failed open published an empty database")
			}
		})
	}
}
