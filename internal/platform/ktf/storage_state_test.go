package ktf

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestRuntimeStorageRestoresSharedHandlesAndRemovalCaches(t *testing.T) {
	_, source := newTestRuntime(t)
	file := &runtimeCFile{name: "stream", data: []byte("abcde"), packaged: 5}
	source.cFiles = map[string]*runtimeCFile{"stream": file}
	source.cFileHandles = map[uint32]*runtimeCFileHandle{0x1001: {store: file, position: 1}, 0x1002: {store: file, position: 4}}
	source.nextCDatabaseHandle = 7
	records := &runtimeRecordDatabase{name: "records", records: [][]byte{nil, {}, []byte("last")}, recordSize: 12}
	source.recordDatabases = map[string]*runtimeRecordDatabase{"records": records}
	source.recordDatabaseHandles = map[uint32]*runtimeRecordDatabaseHandle{0x2001: {store: records}, 0x2003: {store: records}}
	source.nextRecordDatabaseHandle = 3
	source.guestFiles = map[string][]byte{"/guest": []byte("retained"), "empty": {}}
	source.removedFiles = map[string]bool{"deleted": true, "live": false}
	source.removedCDatabases = map[string]bool{}
	source.removedDatabaseLists = map[string]map[string]bool{javaDatabaseRemovedKey: {"hidden": true}, recordDatabaseRemovedKey: {}}
	// madeDirectories stays nil: that cache has not read its save yet.
	saved, err := source.captureStorageState()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded runtimeStorageState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	client, fresh := newTestRuntime(t)
	storage, err := restoreStorageState(decoded)
	if err != nil {
		t.Fatal(err)
	}
	storage.adopt(fresh)
	if fresh.cFileHandles[0x1001].store != fresh.cFiles["stream"] || fresh.cFileHandles[0x1002].store != fresh.cFiles["stream"] || fresh.cFiles["stream"] == file || fresh.cFiles["stream"].packaged != 5 || fresh.nextCDatabaseHandle != 7 {
		t.Fatal("file ownership, packaged-byte count, or handle sequence changed")
	}
	if fresh.recordDatabaseHandles[0x2001].store != fresh.recordDatabases["records"] || fresh.recordDatabaseHandles[0x2003].store != fresh.recordDatabases["records"] || fresh.nextRecordDatabaseHandle != 3 || !reflect.DeepEqual(fresh.recordDatabases["records"].records, records.records) || fresh.recordDatabases["records"].recordSize != 12 {
		t.Fatal("record database ownership, deleted slots, or handle sequence changed")
	}
	if !reflect.DeepEqual(fresh.guestFiles, source.guestFiles) || !reflect.DeepEqual(fresh.removedFiles, source.removedFiles) || !reflect.DeepEqual(fresh.removedDatabaseLists, source.removedDatabaseLists) || fresh.removedCDatabases == nil || fresh.madeDirectories != nil {
		t.Fatal("storage table data or lazy-cache state changed")
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
	if string(fresh.cFileHandles[0x1001].store.data) != "abcdZ" || string(file.data) != "abcde" || fresh.cFileHandles[0x1001].position != 3 || source.cFileHandles[0x1001].position != 1 {
		t.Fatal("file write lost store sharing or changed the source")
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
	if result, err := fresh.wipicRecordDatabaseSelect(thread); err != nil || result != 0 || string(readTestBytes(t, client, buffer, 4)) != "last" {
		t.Fatalf("empty record read=%#x, %v", result, err)
	}
	if err := client.core.Memory().Write(buffer, []byte("records\x00")); err != nil {
		t.Fatal(err)
	}
	set(buffer, 12, 0)
	if handle, err := fresh.wipicRecordDatabaseOpen(thread); err != nil || handle != 0x2004 {
		t.Fatalf("next record handle=%#x, %v", handle, err)
	}
	if _, err := fresh.captureStorageState(); err != nil {
		t.Fatalf("storage recapture: %v", err)
	}
}

func TestRuntimeStorageRetainsCursorPastTruncatedContents(t *testing.T) {
	_, source := newTestRuntime(t)
	file := &runtimeCFile{name: "truncated", packaged: 17}
	// A second open can truncate a shared store without moving an older handle.
	source.cFiles = map[string]*runtimeCFile{"truncated": file}
	source.cFileHandles = map[uint32]*runtimeCFileHandle{0x1001: {store: file, position: 5}}
	source.nextCDatabaseHandle = 1
	saved, err := source.captureStorageState()
	if err != nil {
		t.Fatal(err)
	}
	state, err := restoreStorageState(saved)
	if err != nil {
		t.Fatal(err)
	}
	_, fresh := newTestRuntime(t)
	state.adopt(fresh)
	thread := armcore.NewThread(armcore.NewContext())
	for i, argument := range []uint32{0x1001, 0, 1} {
		if err := thread.SetRegister(i, argument); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := fresh.wipicFileStream(thread, false); err != nil || count != 0 || fresh.cFileHandles[0x1001].position != 5 || fresh.cFiles["truncated"].packaged != 17 {
		t.Fatalf("read after another handle truncated=%d, %v", count, err)
	}
}

func TestRuntimeStorageRejectsMalformedHandlesAndPaths(t *testing.T) {
	_, source := newTestRuntime(t)
	store := &runtimeCFile{name: "stream", data: []byte("abc")}
	source.cFiles = map[string]*runtimeCFile{"stream": store}
	source.cFileHandles = map[uint32]*runtimeCFileHandle{0x1001: {store: store}}
	source.nextCDatabaseHandle = 1
	saved, err := source.captureStorageState()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*runtimeStorageState){
		func(s *runtimeStorageState) { s.Files[0].Name = []byte("../outside") },
		func(s *runtimeStorageState) { s.Files[0].Packaged = -1 },
		func(s *runtimeStorageState) { s.FileHandles[0].Handle = 0x2001 },
		func(s *runtimeStorageState) { s.FileHandles[0].Store = 0 },
		func(s *runtimeStorageState) { s.FileHandles[0].Position = -1 },
		func(s *runtimeStorageState) { s.NextFileHandle = 0 },
		func(s *runtimeStorageState) { s.FileHandles = append(s.FileHandles, s.FileHandles[0]) },
	} {
		var bad runtimeStorageState
		if err := json.Unmarshal(encoded, &bad); err != nil {
			t.Fatal(err)
		}
		change(&bad)
		if _, err := restoreStorageState(bad); err == nil {
			t.Fatal("malformed storage state accepted")
		}
	}
}
