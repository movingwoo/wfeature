package ktf

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The heap record keeps which objects share which database and which File
// state, the catalog's bindings and each File's cursor. What the databases and
// the files held is not in it: a restore reads that from the store, and one
// name is one store whatever held it when the record was taken.
func TestHeapStorageKeepsSharingAndReadsContentFromTheStore(t *testing.T) {
	_, source := newTestRuntime(t)
	old := &runtimeDataBaseStore{name: "slot", records: [][]byte{[]byte("captured old"), nil, {}}}
	current := &runtimeDataBaseStore{name: "slot", records: [][]byte{[]byte("captured current")}}
	closed := &runtimeDataBaseStore{name: "closed", records: [][]byte{[]byte("captured closed")}}
	source.databases = map[string]*runtimeDataBaseStore{"slot": current, "closed": closed}
	file := &runtimeGuestFile{name: "/slot.bin", data: []byte("captured file"), position: 1}
	emptied := &runtimeGuestFile{name: "rewritten.bin", data: []byte("xy"), position: 2, truncated: true}
	objects := []*jvm.Object{
		{ClassName: "org/kwis/msp/db/DataBase", Native: old},
		{ClassName: "org/kwis/msp/db/DataBase", Native: old},
		{ClassName: "org/kwis/msp/db/DataBase", Native: current},
		{ClassName: "org/kwis/msp/io/File", Native: file},
		{ClassName: runtimeFileInputStreamClass, Native: file},
		{ClassName: runtimeFileOutputStreamClass, Native: file},
		{ClassName: "org/kwis/msp/io/File", Native: emptied},
	}
	saved, err := source.captureHeapState(objects)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"captured old", "captured current", "captured closed", "captured file"} {
		if bytes.Contains(encoded, []byte(marker)) || bytes.Contains(encoded, []byte(jsonBytes(t, marker))) {
			t.Fatalf("the heap record carries %q", marker)
		}
	}
	var decoded runtimeHeapState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	store := testSaveStore(t, map[string][]byte{
		"jdb/slot":         encodeSaveRecords([][]byte{[]byte("live"), nil, []byte("third")}),
		"jdb/closed":       encodeSaveRecords([][]byte{[]byte("kept by catalog")}),
		"fs/slot.bin":      []byte("abcd"),
		"fs/rewritten.bin": []byte("0123456789"),
	})
	client, fresh := newTestRuntime(t)
	client.AttachSaveStore(store)
	roots, err := fresh.restoreHeapState(decoded)
	if err != nil {
		t.Fatal(err)
	}
	restoredOld := roots[0].Native.(*runtimeDataBaseStore)
	restoredCurrent := roots[2].Native.(*runtimeDataBaseStore)
	restoredFile := roots[3].Native.(*runtimeGuestFile)
	if len(restoredOld.records) != 0 || len(restoredFile.data) != 0 || len(fresh.restoredStorage.databases) != 2 || len(fresh.restoredStorage.files) != 2 {
		t.Fatal("a restore without a store brought content with it, or lost an object to fill")
	}
	bindTestStorage(t, fresh)
	// Both generations of the name are one store now: the database as the
	// store has it.
	if restoredOld != roots[1].Native || restoredOld == old || restoredOld != restoredCurrent || fresh.databases["slot"] != restoredCurrent {
		t.Fatal("database object sharing or the catalog binding changed")
	}
	if !reflect.DeepEqual(restoredOld.records, [][]byte{[]byte("live"), nil, []byte("third")}) || !reflect.DeepEqual(fresh.databases["closed"].records, [][]byte{[]byte("kept by catalog")}) {
		t.Fatalf("the databases hold %q and %q, want the store's lists", restoredOld.records, fresh.databases["closed"].records)
	}
	if roots[4].Native != restoredFile || roots[5].Native != restoredFile || restoredFile == file || restoredFile.position != 1 || string(restoredFile.data) != "abcd" {
		t.Fatalf("File and stream sharing, the cursor or the bytes changed: %+v", restoredFile)
	}
	// An object opened on an empty file takes at most what it had written.
	if rewritten := roots[6].Native.(*runtimeGuestFile); !rewritten.truncated || string(rewritten.data) != "01" || rewritten.position != 2 {
		t.Fatalf("the emptied File came back as %+v", rewritten)
	}
	value, err := runtimeFileReadByte(fresh, fresh.client.vm, []jvm.Value{jvm.ReferenceValue(roots[4])})
	byteValue, _ := value.Int32()
	if err != nil || byteValue != 'b' || restoredFile.position != 2 || file.position != 1 {
		t.Fatalf("restored stream read=%d, %v; position=%d", byteValue, err, restoredFile.position)
	}
	_, err = runtimeFileWriteByte(fresh, fresh.client.vm, []jvm.Value{jvm.ReferenceValue(roots[5]), jvm.IntValue('Z')})
	if err != nil || string(restoredFile.data) != "abZd" || restoredFile.position != 3 {
		t.Fatalf("restored stream write=%q, %v", restoredFile.data, err)
	}
	if stored, _ := store.LoadSave("fs/slot.bin"); string(stored) != "abZd" {
		t.Fatalf("the store holds %q after the write", stored)
	}
	if _, err := fresh.captureHeapState(roots); err != nil {
		t.Fatalf("storage recapture: %v", err)
	}
}

func TestHeapStorageRefusesInvalidStateBeforeAdoption(t *testing.T) {
	_, source := newTestRuntime(t)
	store := &runtimeDataBaseStore{name: "slot", records: [][]byte{[]byte("data")}}
	source.databases = map[string]*runtimeDataBaseStore{"slot": store}
	saved, err := source.captureHeapState([]*jvm.Object{
		{ClassName: "org/kwis/msp/db/DataBase", Native: store},
		{ClassName: "org/kwis/msp/io/File", Native: &runtimeGuestFile{name: "file", data: []byte("x")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	filePayload := func(s *runtimeHeapState, change func(data []byte) []byte) {
		found := false
		for i := range s.JVM.Payloads {
			if s.JVM.Payloads[i].ExternalKind == heapFilePayloadKind {
				s.JVM.Payloads[i].Data = change(s.JVM.Payloads[i].Data)
				found = true
			}
		}
		if !found {
			t.Fatal("the record has no File payload to damage")
		}
	}
	word := func(offset int, value uint32) func([]byte) []byte {
		return func(data []byte) []byte {
			binary.LittleEndian.PutUint32(data[offset:], value)
			return data
		}
	}
	for name, change := range map[string]func(*runtimeHeapState){
		"a database name that leaves the save directory": func(s *runtimeHeapState) { s.Databases[0].Name = []byte("../escape") },
		"a database named after the table's own list":    func(s *runtimeHeapState) { s.Databases[0].Name = []byte(".removed") },
		"a binding to no database":                       func(s *runtimeHeapState) { s.DatabaseBindings[0].Database = 0 },
		"a binding under another name":                   func(s *runtimeHeapState) { s.DatabaseBindings[0].Name = []byte("different") },
		"a binding twice":                                func(s *runtimeHeapState) { s.DatabaseBindings = append(s.DatabaseBindings, s.DatabaseBindings[0]) },
		"a database nothing holds": func(s *runtimeHeapState) {
			s.Databases = append(s.Databases, heapDatabaseState{Name: []byte("loose")})
		},
		"a File flag this build does not know": func(s *runtimeHeapState) { filePayload(s, word(heapFileFlags, 2)) },
		"a File cursor past the bound":         func(s *runtimeHeapState) { filePayload(s, word(heapFilePosition, maxHeapStorageBytes+1)) },
		"a File length with no emptying open":  func(s *runtimeHeapState) { filePayload(s, word(heapFileLength, 1)) },
		"a File name shorter than it says":     func(s *runtimeHeapState) { filePayload(s, word(heapFileNameLength, 9)) },
		"a File payload cut short":             func(s *runtimeHeapState) { filePayload(s, func(data []byte) []byte { return data[:heapFileHeader-1] }) },
		"a File payload of the earlier layout": func(s *runtimeHeapState) {
			for i := range s.JVM.Payloads {
				if s.JVM.Payloads[i].ExternalKind == heapFilePayloadKind {
					s.JVM.Payloads[i].ExternalKind = "ktf-file-v1"
				}
			}
		},
	} {
		var bad runtimeHeapState
		if err := json.Unmarshal(encoded, &bad); err != nil {
			t.Fatal(err)
		}
		change(&bad)
		_, fresh := newTestRuntime(t)
		marker := &runtimeDataBaseStore{name: "marker"}
		fresh.databases = map[string]*runtimeDataBaseStore{"marker": marker}
		if _, err := fresh.restoreHeapState(bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
		if len(fresh.databases) != 1 || fresh.databases["marker"] != marker {
			t.Errorf("%s: failed restoration changed the target catalog", name)
		}
	}
}
