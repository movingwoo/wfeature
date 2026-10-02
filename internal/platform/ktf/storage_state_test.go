package ktf

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// testStorageBudget is a checkpoint storage budget for a test that captures a
// table by itself.
func testStorageBudget() func(uint64) error {
	return (&heapNativeContext{}).chargeStorage
}

// bindTestStorage fills what a restore brought back from the store the client
// has attached, the way committing a load does.
func bindTestStorage(t *testing.T, runtime *initializationRuntime) {
	t.Helper()
	if err := runtime.client.bindRestoredStorage(saveAdapterState{Version: saveAdapterVersion}, runtime.client.saveStore); err != nil {
		t.Fatalf("bind restored storage: %v", err)
	}
	runtime.restoredStorage = nil
}

// testSaveStore is a memory store holding the given entries.
func testSaveStore(t *testing.T, entries map[string][]byte) *backend.MemorySaveStore {
	t.Helper()
	list := make([]backend.SaveEntry, 0, len(entries))
	for key, data := range entries {
		list = append(list, backend.SaveEntry{Key: key, Data: data})
	}
	store, err := backend.NewMemorySaveStore(list)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// A storage record is names, handles and cursors. What the tables held when it
// was taken is not in it, and what they hold after a restore is what the store
// has then: here a store that has moved on since.
func TestRuntimeStorageRestoresNamesAndReadsContentFromTheStore(t *testing.T) {
	_, source := newTestRuntime(t)
	file := &runtimeCFile{name: "stream", data: []byte("captured stream bytes"), packaged: 5}
	source.cFiles = map[string]*runtimeCFile{"stream": file}
	source.cFileHandles = map[uint32]*runtimeCFileHandle{0x1001: {store: file, position: 1}, 0x1002: {store: file, position: 4}}
	source.nextCDatabaseHandle = 7
	records := &runtimeRecordDatabase{name: "records", records: [][]byte{nil, []byte("m"), []byte("captured record")}, recordSize: 12}
	source.recordDatabases = map[string]*runtimeRecordDatabase{"records": records}
	source.recordDatabaseHandles = map[uint32]*runtimeRecordDatabaseHandle{0x2001: {store: records}, 0x2003: {store: records}}
	source.nextRecordDatabaseHandle = 3
	source.guestFiles = map[string][]byte{"/guest": []byte("captured guest file"), "empty": {}, "gone": []byte("captured and gone")}
	source.removedFiles = map[string]bool{"captured removal": true}
	source.removedCDatabases = map[string]bool{}
	source.removedDatabaseLists = map[string]map[string]bool{javaDatabaseRemovedKey: {"captured hidden": true}, recordDatabaseRemovedKey: {}}
	saved, err := source.captureStorageState(testStorageBudget())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	// Names are in the record as bytes, which JSON writes as base64, so a
	// marker that appears in it is content.
	for _, marker := range []string{"captured stream bytes", "captured record", "captured guest file", "captured removal", "captured hidden"} {
		if bytes.Contains(encoded, []byte(marker)) || bytes.Contains(encoded, []byte(jsonBytes(t, marker))) {
			t.Fatalf("the storage record carries %q", marker)
		}
	}
	var decoded runtimeStorageState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.GuestFiles, [][]byte{[]byte("/guest"), []byte("empty"), []byte("gone")}) {
		t.Fatalf("the record names the written files %q", decoded.GuestFiles)
	}

	store := testSaveStore(t, map[string][]byte{
		"db/stream":         []byte("abcde"),
		"rdb/records":       encodeSaveRecords([][]byte{nil, []byte("mid!"), []byte("last")}),
		"fs/guest":          []byte("retained"),
		"fs/empty":          {},
		guestFileRemovedKey: joinRemovalList([]string{"deleted"}),
		directoryListKey:    joinRemovalList([]string{"made"}),
	})
	client, fresh := newTestRuntime(t)
	client.AttachSaveStore(store)
	storage, err := restoreStorageState(decoded)
	if err != nil {
		t.Fatal(err)
	}
	storage.adopt(fresh)
	if fresh.cFiles["stream"] == nil || len(fresh.cFiles["stream"].data) != 0 || fresh.guestFiles != nil || fresh.removedFiles != nil || fresh.removedDatabaseLists != nil {
		t.Fatal("a restore without a store brought content with it")
	}
	if err := fresh.saveChanges(map[string][]byte{"db/stream": nil}, databaseRemovedKey, map[string]bool{}, nil); err == nil {
		t.Fatal("restored storage wrote before a store had filled it")
	}
	bindTestStorage(t, fresh)

	if fresh.cFileHandles[0x1001].store != fresh.cFiles["stream"] || fresh.cFileHandles[0x1002].store != fresh.cFiles["stream"] || fresh.cFiles["stream"] == file || fresh.nextCDatabaseHandle != 7 {
		t.Fatal("file ownership or handle sequence changed")
	}
	if string(fresh.cFiles["stream"].data) != "abcde" || fresh.cFiles["stream"].packaged != 0 || fresh.cFileHandles[0x1001].position != 1 || fresh.cFileHandles[0x1002].position != 4 {
		t.Fatalf("the file store holds %q with %d packaged bytes, want the store's bytes and none packaged", fresh.cFiles["stream"].data, fresh.cFiles["stream"].packaged)
	}
	if fresh.recordDatabaseHandles[0x2001].store != fresh.recordDatabases["records"] || fresh.recordDatabaseHandles[0x2003].store != fresh.recordDatabases["records"] || fresh.nextRecordDatabaseHandle != 3 ||
		!reflect.DeepEqual(fresh.recordDatabases["records"].records, [][]byte{nil, []byte("mid!"), []byte("last")}) || fresh.recordDatabases["records"].recordSize != 12 {
		t.Fatal("record database ownership, deleted slots, record size or handle sequence changed")
	}
	// The written-file table holds the names that are still there, with the
	// store's bytes, and every list is the store's own.
	if !reflect.DeepEqual(fresh.guestFiles, map[string][]byte{"/guest": []byte("retained"), "empty": {}}) {
		t.Fatalf("the written-file table holds %q", fresh.guestFiles)
	}
	if !reflect.DeepEqual(fresh.removedFiles, map[string]bool{"deleted": true}) || !reflect.DeepEqual(fresh.madeDirectories, map[string]bool{"made": true}) ||
		fresh.removedCDatabases == nil || len(fresh.removedCDatabases) != 0 || len(fresh.removedDatabaseLists) != 2 || len(fresh.removedDatabaseLists[javaDatabaseRemovedKey]) != 0 {
		t.Fatal("the removal and directory lists are not the store's own, all of them read")
	}
	buffer, err := fresh.allocate(32)
	if err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	set := func(arguments ...uint32) {
		for i, argument := range arguments {
			if err := thread.SetRegister(i, argument); err != nil {
				t.Fatal(err)
			}
		}
	}
	set(0x1001, buffer, 2)
	if count, err := fresh.wipicFileStream(thread, false); err != nil || count != 2 || string(readTestBytes(t, client, buffer, 2)) != "bc" {
		t.Fatalf("restored file read=%d, %v", count, err)
	}
	if err := client.core.Memory().Write(buffer, []byte("Z")); err != nil {
		t.Fatal(err)
	}
	set(0x1002, buffer, 1)
	if count, err := fresh.wipicFileStream(thread, true); err != nil || count != 1 {
		t.Fatalf("restored file write=%d, %v", count, err)
	}
	if string(fresh.cFileHandles[0x1001].store.data) != "abcdZ" || fresh.cFileHandles[0x1001].position != 3 || source.cFileHandles[0x1001].position != 1 {
		t.Fatal("file write lost store sharing or changed the source")
	}
	// The write went through to the store, and it changed the byte it named.
	if stored, _ := store.LoadSave("db/stream"); string(stored) != "abcdZ" {
		t.Fatalf("the store holds %q after the write", stored)
	}
	set(0x2003, 3, buffer, 32)
	if result, err := fresh.wipicRecordDatabaseSelect(thread); err != nil || result != 0 || string(readTestBytes(t, client, buffer, 4)) != "last" {
		t.Fatalf("restored record read=%#x, %v", result, err)
	}
	set(0x2001, 1, buffer, 32)
	if result, err := fresh.wipicRecordDatabaseSelect(thread); err != nil || result != wipicErrorInvalid {
		t.Fatalf("deleted record read=%#x, %v", result, err)
	}
	set(0x2001, 2, buffer, 32)
	if result, err := fresh.wipicRecordDatabaseSelect(thread); err != nil || result != 0 || string(readTestBytes(t, client, buffer, 4)) != "mid!" {
		t.Fatalf("second record read=%#x, %v", result, err)
	}
	if err := client.core.Memory().Write(buffer, []byte("records\x00")); err != nil {
		t.Fatal(err)
	}
	set(buffer, 12, 0)
	if handle, err := fresh.wipicRecordDatabaseOpen(thread); err != nil || handle != 0x2004 {
		t.Fatalf("next record handle=%#x, %v", handle, err)
	}
	if _, err := fresh.captureStorageState(testStorageBudget()); err != nil {
		t.Fatalf("storage recapture: %v", err)
	}
}

// jsonBytes is a string the way JSON writes a byte slice holding it.
func jsonBytes(t *testing.T, text string) string {
	t.Helper()
	encoded, err := json.Marshal([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes.Trim(encoded, `"`))
}

// A cursor stays where the record has it when the file under it is shorter
// than it was: a read there answers nothing, a write fills the gap with zeros,
// and the session can be captured again in that state.
func TestRuntimeStorageKeepsACursorPastTheEndOfAShorterFile(t *testing.T) {
	_, source := newTestRuntime(t)
	file := &runtimeCFile{name: "shrunk", data: []byte("0123456789"), packaged: 17}
	source.cFiles = map[string]*runtimeCFile{"shrunk": file}
	source.cFileHandles = map[uint32]*runtimeCFileHandle{0x1001: {store: file, position: 7}}
	source.nextCDatabaseHandle = 1
	saved, err := source.captureStorageState(testStorageBudget())
	if err != nil {
		t.Fatal(err)
	}
	state, err := restoreStorageState(saved)
	if err != nil {
		t.Fatal(err)
	}
	store := testSaveStore(t, map[string][]byte{"db/shrunk": []byte("0123")})
	client, fresh := newTestRuntime(t)
	client.AttachSaveStore(store)
	state.adopt(fresh)
	bindTestStorage(t, fresh)
	buffer, err := fresh.allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	set := func(arguments ...uint32) {
		for i, argument := range arguments {
			if err := thread.SetRegister(i, argument); err != nil {
				t.Fatal(err)
			}
		}
	}
	set(0x1001, buffer, 1)
	// The packaged count is what the name's packaged copy holds now, which
	// is nothing: it is not carried.
	if count, err := fresh.wipicFileStream(thread, false); err != nil || count != 0 || fresh.cFileHandles[0x1001].position != 7 || fresh.cFiles["shrunk"].packaged != 0 {
		t.Fatalf("read past the end of the shorter file=%d, %v at %d", count, err, fresh.cFileHandles[0x1001].position)
	}
	if _, err := fresh.captureStorageState(testStorageBudget()); err != nil {
		t.Fatalf("a cursor past the end cannot be captured again: %v", err)
	}
	if err := client.core.Memory().Write(buffer, []byte("XY")); err != nil {
		t.Fatal(err)
	}
	set(0x1001, buffer, 2)
	if count, err := fresh.wipicFileStream(thread, true); err != nil || count != 2 {
		t.Fatalf("write past the end=%d, %v", count, err)
	}
	if stored, _ := store.LoadSave("db/shrunk"); string(stored) != "0123\x00\x00\x00XY" {
		t.Fatalf("the store holds %q after a write past the end", stored)
	}
}

func TestRuntimeStorageRejectsMalformedHandlesAndPaths(t *testing.T) {
	_, source := newTestRuntime(t)
	store := &runtimeCFile{name: "stream", data: []byte("abc")}
	source.cFiles = map[string]*runtimeCFile{"stream": store}
	source.cFileHandles = map[uint32]*runtimeCFileHandle{0x1001: {store: store}}
	source.nextCDatabaseHandle = 1
	records := &runtimeRecordDatabase{name: "records", recordSize: 4}
	source.recordDatabases = map[string]*runtimeRecordDatabase{"records": records}
	source.recordDatabaseHandles = map[uint32]*runtimeRecordDatabaseHandle{0x2001: {store: records}}
	source.nextRecordDatabaseHandle = 1
	source.guestFiles = map[string][]byte{"written": []byte("x")}
	saved, err := source.captureStorageState(testStorageBudget())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*runtimeStorageState){
		"a file name that leaves the save directory": func(s *runtimeStorageState) { s.Files[0].Name = []byte("../outside") },
		"a file name that is a list of the table":    func(s *runtimeStorageState) { s.Files[0].Name = []byte(".removed") },
		"a handle of the other table":                func(s *runtimeStorageState) { s.FileHandles[0].Handle = 0x2001 },
		"a handle on no store":                       func(s *runtimeStorageState) { s.FileHandles[0].Store = 0 },
		"a handle on a store past the list":          func(s *runtimeStorageState) { s.FileHandles[0].Store = 2 },
		"a cursor before the start":                  func(s *runtimeStorageState) { s.FileHandles[0].Position = -1 },
		"a handle sequence behind its handles":       func(s *runtimeStorageState) { s.NextFileHandle = 0 },
		"a handle twice":                             func(s *runtimeStorageState) { s.FileHandles = append(s.FileHandles, s.FileHandles[0]) },
		"a name twice in the catalog":                func(s *runtimeStorageState) { s.Files = append(s.Files, s.Files[0]) },
		"a file store nothing holds":                 func(s *runtimeStorageState) { s.Files = append(s.Files, cFileSnapshot{Name: []byte("loose")}) },
		"a record store nothing holds": func(s *runtimeStorageState) {
			s.Records = append(s.Records, cRecordSnapshot{Name: []byte("loose")})
		},
		"a record name past its limit":      func(s *runtimeStorageState) { s.Records[0].Name = bytes.Repeat([]byte{'n'}, maxRecordDatabaseName+1) },
		"a record handle on no store":       func(s *runtimeStorageState) { s.RecordHandles[0].Store = 3 },
		"a written file twice":              func(s *runtimeStorageState) { s.GuestFiles = append(s.GuestFiles, s.GuestFiles[0]) },
		"a written file that is the list":   func(s *runtimeStorageState) { s.GuestFiles[0] = []byte("/.removed") },
		"a written file with a line in it":  func(s *runtimeStorageState) { s.GuestFiles[0] = []byte("a\nb") },
		"a written file outside the folder": func(s *runtimeStorageState) { s.GuestFiles[0] = []byte("../outside") },
	} {
		var bad runtimeStorageState
		if err := json.Unmarshal(encoded, &bad); err != nil {
			t.Fatal(err)
		}
		change(&bad)
		if _, err := restoreStorageState(bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// One name under the catalog and under a handle that outlived it is one
	// store: the two are the same file once they are read back.
	var twice runtimeStorageState
	if err := json.Unmarshal(encoded, &twice); err != nil {
		t.Fatal(err)
	}
	twice.Files = append(twice.Files, cFileSnapshot{Name: []byte("stream")})
	twice.FileHandles = append(twice.FileHandles, cFileHandleSnapshot{Handle: 0x1002, Store: 2, Position: 3})
	twice.NextFileHandle = 2
	state, err := restoreStorageState(twice)
	if err != nil {
		t.Fatal(err)
	}
	if state.fileHandles[0x1001].store != state.fileHandles[0x1002].store || state.files["stream"] != state.fileHandles[0x1002].store || len(state.restored.cFiles) != 1 {
		t.Fatal("one name was restored as two stores")
	}
}
