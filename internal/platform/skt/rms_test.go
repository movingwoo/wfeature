package skt

import (
	_ "embed"
	"path/filepath"
	"testing"

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
