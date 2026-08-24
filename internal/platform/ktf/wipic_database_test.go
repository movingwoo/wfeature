package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// databaseCall drives one WIPI C database slot with a name in r0.
func databaseCall(t *testing.T, runtime *initializationRuntime, function uint32, name string, extra ...uint32) uint32 {
	t.Helper()
	const nameAddress = platformDataBase + 0xa000
	if err := runtime.client.core.Memory().Write(nameAddress, append([]byte(name), 0)); err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.Context{})
	if err := thread.SetRegister(0, nameAddress); err != nil {
		t.Fatal(err)
	}
	for index, value := range extra {
		if err := thread.SetRegister(index+1, value); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runtime.handleWIPICDatabaseCall(thread, function)
	if err != nil {
		t.Fatalf("database function %d: %v", function, err)
	}
	return result
}

// TestDeletedDatabaseStaysDeleted is the whole reason the delete needs a
// removal list. The save store has no delete of its own, so a name whose
// persisted copy is still readable comes straight back on the next question —
// and a title that deleted a database to start over is handed what it just
// threw away.
func TestDeletedDatabaseStaysDeleted(t *testing.T) {
	const notFound = int32(-12) // M_E_NOENT, which wipicErrorNotFound spells unsigned.
	_, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(t.TempDir())

	const name = "cert.dat"
	// Open for creation, write through the stream, and persist it.
	handle := databaseCall(t, runtime, wipicDatabaseOpen, name, 4)
	if int32(handle) < 0 {
		t.Fatalf("open for creation = %d", int32(handle))
	}
	store := runtime.cDatabases[name]
	if store == nil {
		t.Fatal("the open created no store")
	}
	store.data = []byte("certificate")
	store.persist(runtime)

	if got := int32(databaseCall(t, runtime, wipicDatabaseExists, name)); got != 0 {
		t.Fatalf("exists before the delete = %d, want 0", got)
	}
	if got := int32(databaseCall(t, runtime, wipicDatabaseDelete, name, 1)); got != 0 {
		t.Fatalf("delete = %d, want 0", got)
	}
	if got := int32(databaseCall(t, runtime, wipicDatabaseExists, name)); got != notFound {
		t.Fatalf("exists after the delete = %d, want the not-found code", got)
	}
	// Opening for reading has to fail too: it is the other question a title
	// asks, and it seeds from the same persisted copy.
	if got := int32(databaseCall(t, runtime, wipicDatabaseOpen, name, 1)); got != notFound {
		t.Fatalf("read-open after the delete = %d, want the not-found code", got)
	}
	// Deleting what is not there is reported rather than accepted.
	if got := int32(databaseCall(t, runtime, wipicDatabaseDelete, name, 1)); got != notFound {
		t.Fatalf("deleting twice = %d, want the not-found code", got)
	}
}

// TestCreatingADeletedDatabaseBringsItBack pins the other half: a title that
// deletes a database and then writes a new one must be able to read the new
// one. A removal list that outlived the create would hide every later write.
func TestCreatingADeletedDatabaseBringsItBack(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(t.TempDir())

	const name = "slot.dat"
	databaseCall(t, runtime, wipicDatabaseOpen, name, 4)
	runtime.cDatabases[name].data = []byte("old")
	runtime.cDatabases[name].persist(runtime)
	databaseCall(t, runtime, wipicDatabaseDelete, name, 1)

	if handle := int32(databaseCall(t, runtime, wipicDatabaseOpen, name, 4)); handle < 0 {
		t.Fatalf("create after a delete = %d", handle)
	}
	if got := len(runtime.cDatabases[name].data); got != 0 {
		t.Fatalf("the recreated database holds %d bytes of the deleted one", got)
	}
	runtime.cDatabases[name].data = []byte("new")
	runtime.cDatabases[name].persist(runtime)
	if got := int32(databaseCall(t, runtime, wipicDatabaseExists, name)); got != 0 {
		t.Fatalf("exists after recreating = %d, want 0", got)
	}
	if data, ok := runtime.databaseSeed(name); !ok || string(data) != "new" {
		t.Fatalf("the seed after recreating = %q ok=%t, want the new contents", data, ok)
	}
}

// A directory has nothing to create here, so what MC_fsMkDir has to get right
// is its answer: made once, already there afterwards, and the same after a
// restart — a title that reads the result to decide whether this is its first
// run would otherwise be told "first run" every time.
func TestMadeDirectoryIsRememberedAcrossSessions(t *testing.T) {
	const alreadyThere = int32(-5) // M_E_EXIST, which wipicErrorExists spells unsigned.
	saves := t.TempDir()
	_, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(saves)

	const name = "ga"
	if got := int32(databaseCall(t, runtime, wipicDatabaseMakeDirectory, name, 1)); got != 0 {
		t.Fatalf("the first mkdir = %d, want 0", got)
	}
	if got := int32(databaseCall(t, runtime, wipicDatabaseMakeDirectory, name, 1)); got != alreadyThere {
		t.Fatalf("the second mkdir = %d, want %d", got, alreadyThere)
	}
	if got := int32(databaseCall(t, runtime, wipicDatabaseExists, name)); got != 0 {
		t.Fatalf("a made directory answers exists = %d, want 0", got)
	}

	// A second session over the same save tree is where the list earns its
	// keep: nothing is in memory and the store is the only record.
	_, second := newTestRuntime(t)
	second.client.saveStore = NewDirectorySaveStore(saves)
	if got := int32(databaseCall(t, second, wipicDatabaseMakeDirectory, name, 1)); got != alreadyThere {
		t.Fatalf("mkdir in a second session = %d, want %d", got, alreadyThere)
	}
	if got := int32(databaseCall(t, second, wipicDatabaseMakeDirectory, "other", 1)); got != 0 {
		t.Fatalf("mkdir of a name nothing made = %d, want 0", got)
	}
}
