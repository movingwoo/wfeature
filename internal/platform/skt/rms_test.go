package skt

import (
	"bytes"
	_ "embed"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/recordstore.jar
var recordStoreJAR []byte

// recordStoreChecks is every bit RecordStoreMIDlet.run sets when the whole
// surface behaves. Comparing against the exact value rather than a threshold
// means a newly broken check fails even if another starts passing.
const recordStoreChecks = int32(1)<<28 - 1

func TestRecordStoreSurfaceAndPersistence(t *testing.T) {
	root := t.TempDir()
	store := backend.NewDirectorySaveStore(filepath.Join(root, "RecordStore Fixture"))

	runtime := startRecordStoreFixture(t, store)
	flags, err := runtime.VM.InvokeStatic("RecordStoreMIDlet", "run", "()I")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	value, err := flags.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != recordStoreChecks {
		t.Fatalf("run() = %#x, want %#x (missing %#x): %s",
			value, recordStoreChecks, recordStoreChecks&^value, fixtureFailure(t, runtime))
	}
	if order := fixtureString(t, runtime, "RecordStoreMIDlet", "enumerationOrder"); order != "3," {
		t.Fatalf("enumerationOrder() = %q, want \"3,\"", order)
	}

	// A second runtime over the same save directory is what a later launch
	// of the game sees; nothing is carried over in memory.
	next := startRecordStoreFixture(t, backend.NewDirectorySaveStore(filepath.Join(root, "RecordStore Fixture")))
	count := invokeFixtureInt(t, next, "RecordStoreMIDlet", "reopen")
	if count != 3 {
		t.Fatalf("reopen() = %d, want 3 surviving records: %s", count, fixtureFailure(t, next))
	}
	if !invokeFixtureBoolean(t, next, "RecordStoreMIDlet", "deleteStore") {
		t.Fatalf("deleteStore() = false: %s", fixtureFailure(t, next))
	}

	// Deletion has to outlive the session too, or a game that clears its save
	// finds it again on the next launch.
	third := startRecordStoreFixture(t, backend.NewDirectorySaveStore(filepath.Join(root, "RecordStore Fixture")))
	if reopened := invokeFixtureInt(t, third, "RecordStoreMIDlet", "reopen"); reopened != -1 {
		t.Fatalf("reopen() after delete = %d, want -1", reopened)
	}
}

func TestRecordStoreWithoutSaveStoreStaysInMemory(t *testing.T) {
	runtime := startRecordStoreFixture(t, nil)
	flags, err := runtime.VM.InvokeStatic("RecordStoreMIDlet", "run", "()I")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	value, err := flags.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != recordStoreChecks {
		t.Fatalf("run() without a save store = %#x, want %#x: %s",
			value, recordStoreChecks, fixtureFailure(t, runtime))
	}
	// Without a Host store a new session starts empty rather than failing.
	next := startRecordStoreFixture(t, nil)
	if reopened := invokeFixtureInt(t, next, "RecordStoreMIDlet", "reopen"); reopened != -1 {
		t.Fatalf("reopen() with no save store = %d, want -1", reopened)
	}
}

func TestRecordStoreNameNormalizationRejectsTraversal(t *testing.T) {
	if _, err := recordStoreKey("../escape"); err == nil {
		t.Fatal("recordStoreKey(\"../escape\") = nil error, want rejection")
	}
	if validRecordStoreName("a/b") {
		t.Fatal("validRecordStoreName(\"a/b\") = true, want false")
	}
	if validRecordStoreName("") {
		t.Fatal("validRecordStoreName(\"\") = true, want false")
	}
	if !validRecordStoreName("scores") {
		t.Fatal("validRecordStoreName(\"scores\") = false, want true")
	}
	// The store list lives at rmsIndexKey, which is the scope joined to this
	// name: a store of that name and the list would address one key, and one
	// written over the other leaves every store unreachable.
	// The store list is names joined by newlines, so a name carrying one would
	// come back as two phantoms with the real store gone. An archive supplies
	// a name here as well as the guest.
	for _, name := range []string{"a\nb", "a\rb"} {
		if validRecordStoreName(name) {
			t.Fatalf("validRecordStoreName(%q) = true, want a name the store list cannot carry refused", name)
		}
	}
	// A name with a space around it is one a title may already have a store
	// under, and the list carries it as it is.
	if !validRecordStoreName("save ") {
		t.Fatal("validRecordStoreName(\"save \") = false, orphaning any store under it")
	}
	if validRecordStoreName(rmsReservedName) {
		t.Fatalf("validRecordStoreName(%q) = true, want the list's own name reserved", rmsReservedName)
	}
}

func TestSaveOwnerFallsBackToMainClass(t *testing.T) {
	if owner := SaveOwner(Descriptor{Name: "Sky Force", MainClass: "sky/Main"}); owner != "Sky Force" {
		t.Fatalf("SaveOwner() = %q, want the MIDlet name", owner)
	}
	if owner := SaveOwner(Descriptor{MainClass: "sky/Main"}); owner != "sky.Main" {
		t.Fatalf("SaveOwner() without a name = %q, want the main class", owner)
	}
	if owner := SaveOwner(Descriptor{Name: "a/b"}); owner != "a_b" {
		t.Fatalf("SaveOwner() = %q, want separators replaced", owner)
	}
}

func startRecordStoreFixture(t *testing.T, store backend.SaveStore) *Runtime {
	t.Helper()
	archive, err := Open(recordStoreJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	options := testRuntimeOptions(t)
	options.SaveStore = store
	runtime, err := Start(archive, options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return runtime
}

func fixtureFailure(t *testing.T, runtime *Runtime) string {
	t.Helper()
	return fixtureString(t, runtime, "RecordStoreMIDlet", "failure")
}

func fixtureString(t *testing.T, runtime *Runtime, className, method string) string {
	t.Helper()
	result, err := runtime.VM.InvokeStatic(className, method, "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("%s.%s() error = %v", className, method, err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if object == nil {
		return ""
	}
	value, ok := object.Native.(string)
	if !ok {
		t.Fatalf("%s.%s() did not return a String", className, method)
	}
	_ = jvm.StringClass
	return value
}

// TestTheSharedFileScopeReservesTheListTheOtherPlatformKeeps covers a name
// this platform had no reason to reserve on its own. The scope is shared with
// the KTF guest filesystem — one owner directory holds a title's files
// whichever platform wrote them — and that filesystem keeps its list of
// deleted paths there, so writing it from this side is the same corruption
// reached through the other door.
func TestTheSharedFileScopeReservesTheListTheOtherPlatformKeeps(t *testing.T) {
	for _, name := range []string{".removed", "/.removed", "./.removed"} {
		if _, err := xFileKey(name); err == nil {
			t.Fatalf("xFileKey(%q) = nil error, want the shared list's name refused", name)
		}
	}
	// And a path that normalizes to the scope itself, which would make a file
	// where the directory belongs.
	for _, name := range []string{".", "/", "./"} {
		if _, err := xFileKey(name); err == nil {
			t.Fatalf("xFileKey(%q) = nil error, want a path that is not a name refused", name)
		}
	}
	if _, err := xFileKey("/save/slot.dat"); err != nil {
		t.Fatalf("xFileKey on an ordinary path = %v", err)
	}
}

// A record of no bytes, which MIDP allows and answers with a null getRecord,
// is a record in a later session as well: the count includes it and setRecord
// can fill it. The save holds a length of zero for it, and decoding used to
// read that as a deleted record, so a title that reserved its slots with empty
// records found them gone on its next launch and could not write them.
func TestAnEmptyRecordOutlivesTheSession(t *testing.T) {
	root := t.TempDir()
	directory := func() backend.SaveStore {
		return backend.NewDirectorySaveStore(filepath.Join(root, "RecordStore Fixture"))
	}
	call := func(runtime *Runtime, method func(*jvm.VM, []jvm.Value) (jvm.Value, error), arguments ...jvm.Value) jvm.Value {
		t.Helper()
		value, err := method(runtime.VM, arguments)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	open := func(runtime *Runtime) jvm.Value {
		t.Helper()
		return call(runtime, runtime.rmsOpenRecordStore, jvm.ReferenceValue(runtime.VM.NewString("slots")), jvm.IntValue(1))
	}
	holds := func(runtime *Runtime, store jvm.Value, when string, sizes ...int32) {
		t.Helper()
		if count, _ := call(runtime, runtime.rmsGetNumRecords, store).Int32(); int(count) != len(sizes) {
			t.Fatalf("%s: getNumRecords = %d, want %d", when, count, len(sizes))
		}
		for index, want := range sizes {
			id := jvm.IntValue(int32(index + 1))
			if size, _ := call(runtime, runtime.rmsGetRecordSize, store, id).Int32(); size != want {
				t.Fatalf("%s: getRecordSize(%d) = %d, want %d", when, index+1, size, want)
			}
			record, _ := call(runtime, runtime.rmsGetRecordBytes, store, id).Reference()
			if (record == nil) != (want == 0) {
				t.Fatalf("%s: getRecord(%d) = %v, want null exactly for an empty record", when, index+1, record)
			}
		}
	}

	first := startRecordStoreFixture(t, directory())
	store := open(first)
	null := jvm.ReferenceValue(nil)
	for range 2 {
		call(first, first.rmsAddRecord, store, null, jvm.IntValue(0), jvm.IntValue(0))
	}
	holds(first, store, "after the adds", 0, 0)
	call(first, first.rmsCloseRecordStore, store)

	second := startRecordStoreFixture(t, directory())
	store = open(second)
	holds(second, store, "in a later session", 0, 0)
	filled, err := newByteArray(second.VM, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	call(second, second.rmsSetRecord, store, jvm.IntValue(2), jvm.ReferenceValue(filled), jvm.IntValue(0), jvm.IntValue(1))
	holds(second, store, "after filling one", 0, 1)
	call(second, second.rmsCloseRecordStore, store)

	third := startRecordStoreFixture(t, directory())
	holds(third, open(third), "in the session after that", 0, 1)
}

const storeEnumerate = "(Ljavax/microedition/rms/RecordFilter;Ljavax/microedition/rms/RecordComparator;Z)Ljavax/microedition/rms/RecordEnumeration;"

func storeEnumeration(t *testing.T, records int, keepUpdated bool) (*Runtime, *jvm.Object, *jvm.Object) {
	t.Helper()
	runtime := startRecordStoreFixture(t, nil)
	store := storeOpen(t, runtime, "walk", true)
	for index := range records {
		storeInvoke(t, runtime, store, "addRecord", "([BII)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{byte(index)})), jvm.IntValue(0), jvm.IntValue(1))
	}
	flag := int32(0)
	if keepUpdated {
		flag = 1
	}
	value := storeInvoke(t, runtime, store, "enumerateRecords", storeEnumerate, jvm.ReferenceValue(nil), jvm.ReferenceValue(nil), jvm.IntValue(flag))
	enumeration, err := value.Reference()
	if err != nil || enumeration == nil {
		t.Fatalf("enumerateRecords = %v", err)
	}
	return runtime, store, enumeration
}

func storeInvokeBool(t *testing.T, value jvm.Value) bool {
	t.Helper()
	return storeInvokeInt(t, value) != 0
}

// A kept-updated enumeration stays where it was when the records change under
// it, so a loop that rewrites each record it visits reaches the end.
func TestKeptUpdatedEnumerationKeepsItsPlaceThroughChanges(t *testing.T) {
	runtime, store, enumeration := storeEnumeration(t, 3, true)
	var visited []int32
	for storeInvokeBool(t, storeInvoke(t, runtime, enumeration, "hasNextElement", "()Z")) && len(visited) < 10 {
		id := storeInvoke(t, runtime, enumeration, "nextRecordId", "()I")
		visited = append(visited, storeInvokeInt(t, id))
		storeInvoke(t, runtime, store, "setRecord", "(I[BII)V", id, jvm.ReferenceValue(jvm.NewByteArray([]byte{9})), jvm.IntValue(0), jvm.IntValue(1))
	}
	if len(visited) != 3 || visited[0] != 1 || visited[1] != 2 || visited[2] != 3 {
		t.Fatalf("rewriting loop visited %v, want [1 2 3]", visited)
	}

	// Deleting the record just visited leaves the next one next.
	runtime, store, enumeration = storeEnumeration(t, 3, true)
	visited = nil
	for storeInvokeBool(t, storeInvoke(t, runtime, enumeration, "hasNextElement", "()Z")) && len(visited) < 10 {
		id := storeInvoke(t, runtime, enumeration, "nextRecordId", "()I")
		visited = append(visited, storeInvokeInt(t, id))
		storeInvoke(t, runtime, store, "deleteRecord", "(I)V", id)
	}
	if len(visited) != 3 || visited[0] != 1 || visited[1] != 2 || visited[2] != 3 {
		t.Fatalf("deleting loop visited %v, want [1 2 3]", visited)
	}
	if count := storeInvokeInt(t, storeInvoke(t, runtime, enumeration, "numRecords", "()I")); count != 0 {
		t.Fatalf("enumeration still holds %d records", count)
	}

	// A record added behind the cursor is still ahead of it.
	runtime, store, enumeration = storeEnumeration(t, 2, true)
	storeInvoke(t, runtime, enumeration, "nextRecordId", "()I")
	storeInvoke(t, runtime, store, "addRecord", "([BII)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{7})), jvm.IntValue(0), jvm.IntValue(1))
	for _, want := range []int32{2, 3} {
		if id := storeInvokeInt(t, storeInvoke(t, runtime, enumeration, "nextRecordId", "()I")); id != want {
			t.Fatalf("after an add the enumeration answered %d, want %d", id, want)
		}
	}
}

// MIDP's cursor is on the record the last step returned: a first
// previousRecord answers the last record, and stepping back after two forward
// steps answers the first one rather than the second again.
func TestEnumerationPreviousFollowsTheMIDPContract(t *testing.T) {
	runtime, _, enumeration := storeEnumeration(t, 3, false)
	call := func(method, descriptor string) jvm.Value {
		return storeInvoke(t, runtime, enumeration, method, descriptor)
	}
	if !storeInvokeBool(t, call("hasPreviousElement", "()Z")) {
		t.Fatal("a fresh enumeration has no previous element")
	}
	if id := storeInvokeInt(t, call("previousRecordId", "()I")); id != 3 {
		t.Fatalf("first previousRecordId = %d, want the last record", id)
	}
	if storeInvokeBool(t, call("hasNextElement", "()Z")) {
		t.Fatal("the last record has a next element")
	}
	call("reset", "()V")
	for _, want := range []int32{1, 2} {
		if id := storeInvokeInt(t, call("nextRecordId", "()I")); id != want {
			t.Fatalf("nextRecordId = %d, want %d", id, want)
		}
	}
	if id := storeInvokeInt(t, call("previousRecordId", "()I")); id != 1 {
		t.Fatalf("previousRecordId after 1, 2 = %d, want 1", id)
	}
	if storeInvokeBool(t, call("hasPreviousElement", "()Z")) {
		t.Fatal("the first record has a previous element")
	}
	if _, err := runtime.VM.InvokeVirtual(enumeration, "previousRecordId", "()I"); !runtime.VM.IsGuestException(err, midp.InvalidRecordIDExceptionClass) {
		t.Fatalf("previousRecordId before the first record error = %v", err)
	}
	if id := storeInvokeInt(t, call("nextRecordId", "()I")); id != 2 {
		t.Fatalf("nextRecordId after stepping back = %d, want 2", id)
	}

	empty := storeOpen(t, runtime, "empty", true)
	value := storeInvoke(t, runtime, empty, "enumerateRecords", storeEnumerate, jvm.ReferenceValue(nil), jvm.ReferenceValue(nil), jvm.IntValue(0))
	none, _ := value.Reference()
	if storeInvokeBool(t, storeInvoke(t, runtime, none, "hasPreviousElement", "()Z")) {
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
		return storeInvoke(t, runtime, enumeration, method, descriptor)
	}

	// On the first record, which is then deleted: nothing is behind it.
	runtime, store, enumeration := storeEnumeration(t, 3, true)
	call(runtime, enumeration, "nextRecordId", "()I")
	storeInvoke(t, runtime, store, "deleteRecord", "(I)V", jvm.IntValue(1))
	if storeInvokeBool(t, call(runtime, enumeration, "hasPreviousElement", "()Z")) {
		t.Fatal("deleting the first record left a previous element")
	}
	if _, err := runtime.VM.InvokeVirtual(enumeration, "previousRecordId", "()I"); !runtime.VM.IsGuestException(err, midp.InvalidRecordIDExceptionClass) {
		t.Fatalf("previousRecordId after deleting the first record error = %v", err)
	}
	if id := storeInvokeInt(t, call(runtime, enumeration, "nextRecordId", "()I")); id != 2 {
		t.Fatalf("nextRecordId after deleting the first record = %d, want 2", id)
	}

	// On the last record, which is then deleted: the one before it is next
	// back, and nothing is ahead.
	runtime, store, enumeration = storeEnumeration(t, 3, true)
	for range 3 {
		call(runtime, enumeration, "nextRecordId", "()I")
	}
	storeInvoke(t, runtime, store, "deleteRecord", "(I)V", jvm.IntValue(3))
	if storeInvokeBool(t, call(runtime, enumeration, "hasNextElement", "()Z")) {
		t.Fatal("deleting the last record left a next element")
	}
	if id := storeInvokeInt(t, call(runtime, enumeration, "previousRecordId", "()I")); id != 2 {
		t.Fatalf("previousRecordId after deleting the last record = %d, want 2", id)
	}

	// On a middle record: each side keeps its neighbour, and a second change
	// while between records keeps the cursor in the same gap.
	runtime, store, enumeration = storeEnumeration(t, 3, true)
	call(runtime, enumeration, "nextRecordId", "()I")
	call(runtime, enumeration, "nextRecordId", "()I")
	storeInvoke(t, runtime, store, "deleteRecord", "(I)V", jvm.IntValue(2))
	storeInvoke(t, runtime, store, "setRecord", "(I[BII)V", jvm.IntValue(3), jvm.ReferenceValue(jvm.NewByteArray([]byte{5})), jvm.IntValue(0), jvm.IntValue(1))
	if id := storeInvokeInt(t, call(runtime, enumeration, "previousRecordId", "()I")); id != 1 {
		t.Fatalf("previousRecordId after deleting the middle record = %d, want 1", id)
	}
	if id := storeInvokeInt(t, call(runtime, enumeration, "nextRecordId", "()I")); id != 3 {
		t.Fatalf("nextRecordId after stepping back = %d, want 3", id)
	}
}

func storeOpen(t *testing.T, runtime *Runtime, name string, create bool) *jvm.Object {
	t.Helper()
	flag := int32(0)
	if create {
		flag = 1
	}
	value, err := runtime.VM.InvokeStatic(midp.RecordStoreClass, "openRecordStore", "(Ljava/lang/String;Z)Ljavax/microedition/rms/RecordStore;",
		jvm.ReferenceValue(runtime.VM.NewString(name)), jvm.IntValue(flag))
	if err != nil {
		t.Fatalf("openRecordStore(%q, %v): %v", name, create, err)
	}
	object, err := value.Reference()
	if err != nil || object == nil {
		t.Fatalf("openRecordStore(%q) returned %v", name, err)
	}
	return object
}

func storeInvoke(t *testing.T, runtime *Runtime, object *jvm.Object, method, descriptor string, arguments ...jvm.Value) jvm.Value {
	t.Helper()
	value, err := runtime.VM.InvokeVirtual(object, method, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s%s: %v", method, descriptor, err)
	}
	return value
}

// The index can miss a store whose records were written, when the index write
// failed and nothing rewrote it. Creating the store then takes the records
// that are there instead of writing an empty list over the player's save; a
// deleted store, which leaves an empty list, still comes back empty.
func TestCreatingAnUnlistedStoreKeepsItsRecords(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	records := backend.EncodeSaveRecords([][]byte{{1, 2, 3}, nil, {4}})
	if err := store.StoreSave("rms/save", records); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave("rms/cleared", backend.EncodeSaveRecords(nil)); err != nil {
		t.Fatal(err)
	}
	runtime := startRecordStoreFixture(t, store)
	object := storeOpen(t, runtime, "save", true)
	if count := storeInvokeInt(t, storeInvoke(t, runtime, object, "getNumRecords", "()I")); count != 2 {
		t.Fatalf("created store has %d records, want the 2 already saved", count)
	}
	if next := storeInvokeInt(t, storeInvoke(t, runtime, object, "getNextRecordID", "()I")); next != 4 {
		t.Fatalf("next record id = %d, want 4", next)
	}
	if data, _ := store.LoadSave("rms/save"); !bytes.Equal(data, records) {
		t.Fatal("creating the store overwrote its saved records")
	}
	if index, _ := store.LoadSave("rms/.index"); !strings.Contains(string(index), "save") {
		t.Fatalf("index = %q, want the recovered store listed", index)
	}
	cleared := storeOpen(t, runtime, "cleared", true)
	if count := storeInvokeInt(t, storeInvoke(t, runtime, cleared, "getNumRecords", "()I")); count != 0 {
		t.Fatalf("a deleted store came back with %d records", count)
	}
}

// A store write takes its encoding inside the write's own turn, so an
// encoding made before a newer write cannot land after it.
func TestRecordStoreWriteCannotLandBehindANewerOne(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	runtime := startRecordStoreFixture(t, store)
	object := storeOpen(t, runtime, "race", true)
	storeInvoke(t, runtime, object, "addRecord", "([BII)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{0})), jvm.IntValue(0), jvm.IntValue(1))
	records := object.Native.(*recordStore)

	runtime.savePendingMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := runtime.rmsSetRecord(runtime.VM, []jvm.Value{jvm.ReferenceValue(object), jvm.IntValue(1),
			jvm.ReferenceValue(jvm.NewByteArray([]byte{1})), jvm.IntValue(0), jvm.IntValue(1)})
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		records.mu.Lock()
		changed := records.records[0][0] == 1
		records.mu.Unlock()
		if changed {
			break
		}
		if time.Now().After(deadline) {
			runtime.savePendingMu.Unlock()
			t.Fatal("setRecord never changed the record")
		}
		time.Sleep(time.Millisecond)
	}
	// The first writer is now waiting for its turn. A second thread's change
	// is written in that gap.
	time.Sleep(50 * time.Millisecond)
	records.mu.Lock()
	records.records[0] = []byte{2}
	records.mu.Unlock()
	newer := backend.EncodeSaveRecords([][]byte{{2}})
	if err := store.StoreSave("rms/race", newer); err != nil {
		runtime.savePendingMu.Unlock()
		t.Fatal(err)
	}
	runtime.savePendingMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if data, _ := store.LoadSave("rms/race"); !bytes.Equal(data, newer) {
		t.Fatalf("store holds %v, want the newer records %v", data, newer)
	}
}

// Ids are never reused, so a store's slot count grows with every add even
// when records are deleted again. Past the count a saved store can hold it
// would no longer load, so the add is refused as a full store instead.
func TestRecordStoreRefusesMoreSlotsThanASaveCanHold(t *testing.T) {
	runtime := startRecordStoreFixture(t, nil)
	object := storeOpen(t, runtime, "log", true)
	records := object.Native.(*recordStore)
	records.mu.Lock()
	records.records = make([][]byte, backend.MaxSaveRecords)
	records.mu.Unlock()
	_, err := runtime.VM.InvokeVirtual(object, "addRecord", "([BII)I", jvm.ReferenceValue(nil), jvm.IntValue(0), jvm.IntValue(0))
	if !runtime.VM.IsGuestException(err, midp.RecordStoreFullExceptionClass) {
		t.Fatalf("addRecord past the slot limit error = %v, want RecordStoreFullException", err)
	}
	records.mu.Lock()
	defer records.mu.Unlock()
	if len(records.records) != backend.MaxSaveRecords {
		t.Fatalf("store grew to %d slots", len(records.records))
	}
}
