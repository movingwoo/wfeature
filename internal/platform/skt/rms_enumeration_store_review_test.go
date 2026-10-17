package skt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

const storeReviewEnumerate = "(Ljavax/microedition/rms/RecordFilter;Ljavax/microedition/rms/RecordComparator;Z)Ljavax/microedition/rms/RecordEnumeration;"

func storeReviewEnumeration(t *testing.T, records int, keepUpdated bool) (*Runtime, *jvm.Object, *jvm.Object) {
	t.Helper()
	runtime := startRecordStoreFixture(t, nil)
	store := storeReviewOpen(t, runtime, "walk", true)
	for index := range records {
		storeReviewCall(t, runtime, store, "addRecord", "([BII)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{byte(index)})), jvm.IntValue(0), jvm.IntValue(1))
	}
	flag := int32(0)
	if keepUpdated {
		flag = 1
	}
	value := storeReviewCall(t, runtime, store, "enumerateRecords", storeReviewEnumerate, jvm.ReferenceValue(nil), jvm.ReferenceValue(nil), jvm.IntValue(flag))
	enumeration, err := value.Reference()
	if err != nil || enumeration == nil {
		t.Fatalf("enumerateRecords = %v", err)
	}
	return runtime, store, enumeration
}

func storeReviewBool(t *testing.T, value jvm.Value) bool {
	t.Helper()
	return storeReviewInt(t, value) != 0
}

// A kept-updated enumeration stays where it was when the records change under
// it, so a loop that rewrites each record it visits reaches the end.
func TestKeptUpdatedEnumerationKeepsItsPlaceThroughChanges(t *testing.T) {
	runtime, store, enumeration := storeReviewEnumeration(t, 3, true)
	var visited []int32
	for storeReviewBool(t, storeReviewCall(t, runtime, enumeration, "hasNextElement", "()Z")) && len(visited) < 10 {
		id := storeReviewCall(t, runtime, enumeration, "nextRecordId", "()I")
		visited = append(visited, storeReviewInt(t, id))
		storeReviewCall(t, runtime, store, "setRecord", "(I[BII)V", id, jvm.ReferenceValue(jvm.NewByteArray([]byte{9})), jvm.IntValue(0), jvm.IntValue(1))
	}
	if len(visited) != 3 || visited[0] != 1 || visited[1] != 2 || visited[2] != 3 {
		t.Fatalf("rewriting loop visited %v, want [1 2 3]", visited)
	}

	// Deleting the record just visited leaves the next one next.
	runtime, store, enumeration = storeReviewEnumeration(t, 3, true)
	visited = nil
	for storeReviewBool(t, storeReviewCall(t, runtime, enumeration, "hasNextElement", "()Z")) && len(visited) < 10 {
		id := storeReviewCall(t, runtime, enumeration, "nextRecordId", "()I")
		visited = append(visited, storeReviewInt(t, id))
		storeReviewCall(t, runtime, store, "deleteRecord", "(I)V", id)
	}
	if len(visited) != 3 || visited[0] != 1 || visited[1] != 2 || visited[2] != 3 {
		t.Fatalf("deleting loop visited %v, want [1 2 3]", visited)
	}
	if count := storeReviewInt(t, storeReviewCall(t, runtime, enumeration, "numRecords", "()I")); count != 0 {
		t.Fatalf("enumeration still holds %d records", count)
	}

	// A record added behind the cursor is still ahead of it.
	runtime, store, enumeration = storeReviewEnumeration(t, 2, true)
	storeReviewCall(t, runtime, enumeration, "nextRecordId", "()I")
	storeReviewCall(t, runtime, store, "addRecord", "([BII)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{7})), jvm.IntValue(0), jvm.IntValue(1))
	for _, want := range []int32{2, 3} {
		if id := storeReviewInt(t, storeReviewCall(t, runtime, enumeration, "nextRecordId", "()I")); id != want {
			t.Fatalf("after an add the enumeration answered %d, want %d", id, want)
		}
	}
}

// MIDP's cursor is on the record the last step returned: a first
// previousRecord answers the last record, and stepping back after two forward
// steps answers the first one rather than the second again.
func TestEnumerationPreviousFollowsTheMIDPContract(t *testing.T) {
	runtime, _, enumeration := storeReviewEnumeration(t, 3, false)
	call := func(method, descriptor string) jvm.Value {
		return storeReviewCall(t, runtime, enumeration, method, descriptor)
	}
	if !storeReviewBool(t, call("hasPreviousElement", "()Z")) {
		t.Fatal("a fresh enumeration has no previous element")
	}
	if id := storeReviewInt(t, call("previousRecordId", "()I")); id != 3 {
		t.Fatalf("first previousRecordId = %d, want the last record", id)
	}
	if storeReviewBool(t, call("hasNextElement", "()Z")) {
		t.Fatal("the last record has a next element")
	}
	call("reset", "()V")
	for _, want := range []int32{1, 2} {
		if id := storeReviewInt(t, call("nextRecordId", "()I")); id != want {
			t.Fatalf("nextRecordId = %d, want %d", id, want)
		}
	}
	if id := storeReviewInt(t, call("previousRecordId", "()I")); id != 1 {
		t.Fatalf("previousRecordId after 1, 2 = %d, want 1", id)
	}
	if storeReviewBool(t, call("hasPreviousElement", "()Z")) {
		t.Fatal("the first record has a previous element")
	}
	if _, err := runtime.VM.InvokeVirtual(enumeration, "previousRecordId", "()I"); !runtime.VM.IsGuestException(err, midp.InvalidRecordIDExceptionClass) {
		t.Fatalf("previousRecordId before the first record error = %v", err)
	}
	if id := storeReviewInt(t, call("nextRecordId", "()I")); id != 2 {
		t.Fatalf("nextRecordId after stepping back = %d, want 2", id)
	}

	empty := storeReviewOpen(t, runtime, "empty", true)
	value := storeReviewCall(t, runtime, empty, "enumerateRecords", storeReviewEnumerate, jvm.ReferenceValue(nil), jvm.ReferenceValue(nil), jvm.IntValue(0))
	none, _ := value.Reference()
	if storeReviewBool(t, storeReviewCall(t, runtime, none, "hasPreviousElement", "()Z")) {
		t.Fatal("an empty enumeration has a previous element")
	}
	if _, err := runtime.VM.InvokeVirtual(none, "previousRecordId", "()I"); !runtime.VM.IsGuestException(err, midp.InvalidRecordIDExceptionClass) {
		t.Fatalf("previousRecordId on an empty enumeration error = %v", err)
	}
}

// Deleting the record a kept-updated enumeration is on leaves its cursor
// between the records around it: the previous step answers the one before
// the deleted record, not the last record or the one before that.
func TestKeptUpdatedEnumerationStepsBackAcrossADeletedRecord(t *testing.T) {
	call := func(runtime *Runtime, enumeration *jvm.Object, method, descriptor string) jvm.Value {
		return storeReviewCall(t, runtime, enumeration, method, descriptor)
	}

	// On the first record, which is then deleted: nothing is behind it.
	runtime, store, enumeration := storeReviewEnumeration(t, 3, true)
	call(runtime, enumeration, "nextRecordId", "()I")
	storeReviewCall(t, runtime, store, "deleteRecord", "(I)V", jvm.IntValue(1))
	if storeReviewBool(t, call(runtime, enumeration, "hasPreviousElement", "()Z")) {
		t.Fatal("deleting the first record left a previous element")
	}
	if _, err := runtime.VM.InvokeVirtual(enumeration, "previousRecordId", "()I"); !runtime.VM.IsGuestException(err, midp.InvalidRecordIDExceptionClass) {
		t.Fatalf("previousRecordId after deleting the first record error = %v", err)
	}
	if id := storeReviewInt(t, call(runtime, enumeration, "nextRecordId", "()I")); id != 2 {
		t.Fatalf("nextRecordId after deleting the first record = %d, want 2", id)
	}

	// On the last record, which is then deleted: the one before it is next
	// back, and nothing is ahead.
	runtime, store, enumeration = storeReviewEnumeration(t, 3, true)
	for range 3 {
		call(runtime, enumeration, "nextRecordId", "()I")
	}
	storeReviewCall(t, runtime, store, "deleteRecord", "(I)V", jvm.IntValue(3))
	if storeReviewBool(t, call(runtime, enumeration, "hasNextElement", "()Z")) {
		t.Fatal("deleting the last record left a next element")
	}
	if id := storeReviewInt(t, call(runtime, enumeration, "previousRecordId", "()I")); id != 2 {
		t.Fatalf("previousRecordId after deleting the last record = %d, want 2", id)
	}

	// On a middle record: each side keeps its neighbour, and a second change
	// while between records keeps the cursor in the same gap.
	runtime, store, enumeration = storeReviewEnumeration(t, 3, true)
	call(runtime, enumeration, "nextRecordId", "()I")
	call(runtime, enumeration, "nextRecordId", "()I")
	storeReviewCall(t, runtime, store, "deleteRecord", "(I)V", jvm.IntValue(2))
	storeReviewCall(t, runtime, store, "setRecord", "(I[BII)V", jvm.IntValue(3), jvm.ReferenceValue(jvm.NewByteArray([]byte{5})), jvm.IntValue(0), jvm.IntValue(1))
	if id := storeReviewInt(t, call(runtime, enumeration, "previousRecordId", "()I")); id != 1 {
		t.Fatalf("previousRecordId after deleting the middle record = %d, want 1", id)
	}
	if id := storeReviewInt(t, call(runtime, enumeration, "nextRecordId", "()I")); id != 3 {
		t.Fatalf("nextRecordId after stepping back = %d, want 3", id)
	}
}
