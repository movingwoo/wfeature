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
