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
	result, err := runtime.handleWIPICFileCall(thread, function)
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
	handle := databaseCall(t, runtime, wipicFileOpen, name, 4)
	if int32(handle) < 0 {
		t.Fatalf("open for creation = %d", int32(handle))
	}
	store := runtime.cFiles[name]
	if store == nil {
		t.Fatal("the open created no store")
	}
	store.data = []byte("certificate")
	store.persist(runtime)

	if got := int32(databaseCall(t, runtime, wipicFileExists, name)); got != 0 {
		t.Fatalf("exists before the delete = %d, want 0", got)
	}
	if got := int32(databaseCall(t, runtime, wipicFileDelete, name, 1)); got != 0 {
		t.Fatalf("delete = %d, want 0", got)
	}
	if got := int32(databaseCall(t, runtime, wipicFileExists, name)); got != notFound {
		t.Fatalf("exists after the delete = %d, want the not-found code", got)
	}
	// Opening for reading has to fail too: it is the other question a title
	// asks, and it seeds from the same persisted copy.
	if got := int32(databaseCall(t, runtime, wipicFileOpen, name, 1)); got != notFound {
		t.Fatalf("read-open after the delete = %d, want the not-found code", got)
	}
	// Deleting what is not there is reported rather than accepted.
	if got := int32(databaseCall(t, runtime, wipicFileDelete, name, 1)); got != notFound {
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
	databaseCall(t, runtime, wipicFileOpen, name, 4)
	runtime.cFiles[name].data = []byte("old")
	runtime.cFiles[name].persist(runtime)
	databaseCall(t, runtime, wipicFileDelete, name, 1)

	if handle := int32(databaseCall(t, runtime, wipicFileOpen, name, 4)); handle < 0 {
		t.Fatalf("create after a delete = %d", handle)
	}
	if got := len(runtime.cFiles[name].data); got != 0 {
		t.Fatalf("the recreated database holds %d bytes of the deleted one", got)
	}
	runtime.cFiles[name].data = []byte("new")
	runtime.cFiles[name].persist(runtime)
	if got := int32(databaseCall(t, runtime, wipicFileExists, name)); got != 0 {
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
	if got := int32(databaseCall(t, runtime, wipicFileMakeDirectory, name, 1)); got != 0 {
		t.Fatalf("the first mkdir = %d, want 0", got)
	}
	if got := int32(databaseCall(t, runtime, wipicFileMakeDirectory, name, 1)); got != alreadyThere {
		t.Fatalf("the second mkdir = %d, want %d", got, alreadyThere)
	}
	if got := int32(databaseCall(t, runtime, wipicFileExists, name)); got != 0 {
		t.Fatalf("a made directory answers exists = %d, want 0", got)
	}

	// A second session over the same save tree is where the list earns its
	// keep: nothing is in memory and the store is the only record.
	_, second := newTestRuntime(t)
	second.client.saveStore = NewDirectorySaveStore(saves)
	if got := int32(databaseCall(t, second, wipicFileMakeDirectory, name, 1)); got != alreadyThere {
		t.Fatalf("mkdir in a second session = %d, want %d", got, alreadyThere)
	}
	if got := int32(databaseCall(t, second, wipicFileMakeDirectory, "other", 1)); got != 0 {
		t.Fatalf("mkdir of a name nothing made = %d, want 0", got)
	}
}

// renameCall drives MC_fsRename(oldName, newName, aMode), which is the one slot
// in this table that takes two names.
func renameCall(t *testing.T, runtime *initializationRuntime, oldName, newName string) int32 {
	t.Helper()
	const oldAddress = platformDataBase + 0xb000
	const newAddress = platformDataBase + 0xb400
	if err := runtime.client.core.Memory().Write(oldAddress, append([]byte(oldName), 0)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.client.core.Memory().Write(newAddress, append([]byte(newName), 0)); err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.Context{})
	for index, value := range []uint32{oldAddress, newAddress, 1} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runtime.handleWIPICFileCall(thread, wipicFileRename)
	if err != nil {
		t.Fatalf("MC_fsRename(%q, %q): %v", oldName, newName, err)
	}
	return int32(result)
}

// The rename is how a title commits a save it wrote to a scratch name, so what
// it has to get right is that the bytes arrive under the new name and that the
// old one is gone — after a restart as well, where the persisted copy is the
// only record and the save store has no delete of its own.
func TestRenameMovesAFileAndItsPersistedCopy(t *testing.T) {
	const notFound = int32(-12) // M_E_NOENT
	saves := t.TempDir()
	_, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(saves)

	const scratch, final = "save.z_", "save.dsk"
	databaseCall(t, runtime, wipicFileOpen, scratch, 4)
	runtime.cFiles[scratch].data = []byte("a season")
	runtime.cFiles[scratch].persist(runtime)

	if got := renameCall(t, runtime, scratch, final); got != 0 {
		t.Fatalf("rename = %d, want 0", got)
	}
	if got := int32(databaseCall(t, runtime, wipicFileExists, final)); got != 0 {
		t.Fatalf("exists(%q) = %d, want 0", final, got)
	}
	if got := int32(databaseCall(t, runtime, wipicFileExists, scratch)); got != notFound {
		t.Fatalf("exists(%q) after the rename = %d, want the not-found code", scratch, got)
	}

	// A second session over the same save tree has nothing in memory, which is
	// where a rename that moved only the live store would show.
	_, second := newTestRuntime(t)
	second.client.saveStore = NewDirectorySaveStore(saves)
	if data, ok := second.databaseSeed(final); !ok || string(data) != "a season" {
		t.Fatalf("the renamed file in a second session = %q ok=%t", data, ok)
	}
	if _, ok := second.databaseSeed(scratch); ok {
		t.Fatal("the scratch name came back in a second session")
	}
}

// The one refusal the specification spells out: a name that is taken cannot be
// renamed onto, and the file that was there stays there.
func TestRenameRefusesANameThatIsTaken(t *testing.T) {
	const alreadyThere = int32(-5) // M_E_EXIST
	const failed = int32(-1)       // M_E_ERROR
	_, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(t.TempDir())

	databaseCall(t, runtime, wipicFileOpen, "one", 4)
	runtime.cFiles["one"].data = []byte("first")
	databaseCall(t, runtime, wipicFileOpen, "two", 4)
	runtime.cFiles["two"].data = []byte("second")

	if got := renameCall(t, runtime, "one", "two"); got != alreadyThere {
		t.Fatalf("rename onto a taken name = %d, want %d", got, alreadyThere)
	}
	if got := string(runtime.cFiles["two"].data); got != "second" {
		t.Fatalf("the refused rename wrote %q over the file that was there", got)
	}
	if got := string(runtime.cFiles["one"].data); got != "first" {
		t.Fatalf("the refused rename lost the source: %q", got)
	}
	// A source that is not there is the catch-all failure; the specification's
	// list for this call has no "no such file".
	if got := renameCall(t, runtime, "missing", "elsewhere"); got != failed {
		t.Fatalf("rename of a missing file = %d, want %d", got, failed)
	}
	// Renaming a file to the name it already has has nothing to move, and
	// refusing it because the destination is occupied by the source would
	// refuse a rename that has already happened.
	if got := renameCall(t, runtime, "one", "one"); got != 0 {
		t.Fatalf("rename onto itself = %d, want 0", got)
	}
}

// Every name in this table is an absolute path resolved inside the area aMode
// names, so a program's own `/save.dat` and its `save.dat` are one file. A
// title is free to use both spellings for the same file, and one local title
// does: it writes its save under a bare name and opens it back with a leading
// separator. Two keys would mean it never found what it wrote.
func TestFileNamesIgnoreTheLeadingSeparator(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(t.TempDir())

	databaseCall(t, runtime, wipicFileOpen, "lo.z_", 4)
	runtime.cFiles["lo.z_"].data = []byte("installed")
	if got := renameCall(t, runtime, "lo.z_", "/lo.dsk"); got != 0 {
		t.Fatalf("rename to an absolute name = %d, want 0", got)
	}
	if got := int32(databaseCall(t, runtime, wipicFileExists, "lo.dsk")); got != 0 {
		t.Fatalf("exists(%q) = %d, want 0", "lo.dsk", got)
	}
	if handle := int32(databaseCall(t, runtime, wipicFileOpen, "/lo.dsk", 1)); handle < 0 {
		t.Fatalf("opening the absolute spelling = %d", handle)
	}
	// What follows the leading separator is part of the name: a title that
	// keeps its files under a directory of its own is naming different files.
	if got := int32(databaseCall(t, runtime, wipicFileExists, "/res/lo.dsk")); got == 0 {
		t.Fatal("a name under a directory resolved to the one at the root")
	}
}
