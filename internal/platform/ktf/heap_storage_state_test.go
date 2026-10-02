package ktf

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestHeapStoragePreservesOpenHandlesAndDatabaseGenerations(t *testing.T) {
	_, source := newTestRuntime(t)
	old := &runtimeDataBaseStore{name: "slot", records: [][]byte{[]byte("old"), nil, {}}}
	current := &runtimeDataBaseStore{name: "slot", records: [][]byte{[]byte("current")}}
	closed := &runtimeDataBaseStore{name: "closed", records: [][]byte{[]byte("kept by catalog")}}
	source.databases = map[string]*runtimeDataBaseStore{"slot": current, "closed": closed}
	file := &runtimeGuestFile{name: "/slot.bin", data: []byte("abcd"), position: 1}
	objects := []*jvm.Object{
		{ClassName: "org/kwis/msp/db/DataBase", Native: old},
		{ClassName: "org/kwis/msp/db/DataBase", Native: old},
		{ClassName: "org/kwis/msp/db/DataBase", Native: current},
		{ClassName: "org/kwis/msp/io/File", Native: file},
		{ClassName: runtimeFileInputStreamClass, Native: file},
		{ClassName: runtimeFileOutputStreamClass, Native: file},
	}
	saved, err := source.captureHeapState(objects)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded runtimeHeapState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	_, fresh := newTestRuntime(t)
	roots, err := fresh.restoreHeapState(decoded)
	if err != nil {
		t.Fatal(err)
	}
	restoredOld := roots[0].Native.(*runtimeDataBaseStore)
	restoredCurrent := roots[2].Native.(*runtimeDataBaseStore)
	if restoredOld != roots[1].Native || restoredOld == old || restoredOld == restoredCurrent || fresh.databases["slot"] != restoredCurrent {
		t.Fatal("database handle sharing or catalog generation changed")
	}
	if !reflect.DeepEqual(restoredOld.records, old.records) || !reflect.DeepEqual(fresh.databases["closed"].records, closed.records) {
		t.Fatal("record bytes, deleted slot, empty slot, or catalog-only store changed")
	}
	restoredOld.records[0][0] = 'O'
	if string(old.records[0]) != "old" || string(restoredCurrent.records[0]) != "current" {
		t.Fatal("restored database shares its source or a later generation")
	}
	restoredFile := roots[3].Native.(*runtimeGuestFile)
	if roots[4].Native != restoredFile || roots[5].Native != restoredFile || restoredFile == file {
		t.Fatal("File and stream cursor sharing changed")
	}
	value, err := runtimeFileReadByte(fresh, fresh.client.vm, []jvm.Value{jvm.ReferenceValue(roots[4])})
	byteValue, _ := value.Int32()
	if err != nil || byteValue != 'b' || restoredFile.position != 2 || file.position != 1 {
		t.Fatalf("restored stream read=%d, %v; position=%d", byteValue, err, restoredFile.position)
	}
	_, err = runtimeFileWriteByte(fresh, fresh.client.vm, []jvm.Value{jvm.ReferenceValue(roots[5]), jvm.IntValue('Z')})
	if err != nil || string(restoredFile.data) != "abZd" || string(file.data) != "abcd" || restoredFile.position != 3 {
		t.Fatalf("restored stream write=%q, %v", restoredFile.data, err)
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
	for _, change := range []func(*runtimeHeapState){
		func(s *runtimeHeapState) { s.Databases[0].Name = []byte("../escape") },
		func(s *runtimeHeapState) { s.Databases[0].Name = []byte(".removed") },
		func(s *runtimeHeapState) { s.Databases[0].Records = make([][]byte, maxDataBaseRecords+1) },
		func(s *runtimeHeapState) { s.DatabaseBindings[0].Database = 0 },
		func(s *runtimeHeapState) { s.DatabaseBindings[0].Name = []byte("different") },
		func(s *runtimeHeapState) { s.DatabaseBindings = append(s.DatabaseBindings, s.DatabaseBindings[0]) },
		func(s *runtimeHeapState) {
			for i := range s.JVM.Payloads {
				if s.JVM.Payloads[i].ExternalKind == "ktf-file-v1" {
					s.JVM.Payloads[i].Data[4] = 2 // Cursor beyond the one-byte buffer.
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
			t.Fatal("malformed storage accepted")
		}
		if len(fresh.databases) != 1 || fresh.databases["marker"] != marker {
			t.Fatal("failed restoration changed the target catalog")
		}
	}
}
