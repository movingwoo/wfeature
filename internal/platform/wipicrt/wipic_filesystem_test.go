package wipicrt_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

// TestDeletedDatabaseStaysDeleted is the whole reason the delete needs a
// removal list. The save store has no delete of its own, so a name whose
// persisted copy is still readable comes straight back on the next question —
// and a title that deleted a database to start over is handed what it just
// threw away.
func TestDeletedDatabaseStaysDeleted(t *testing.T) {
	const notFound = int32(-12) // M_E_NOENT, which wipicErrorNotFound spells unsigned.
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())

	const name = "cert.dat"
	// Open for creation, write through the stream, and persist it.
	handle := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, name, 4)
	if int32(handle) < 0 {
		t.Fatalf("open for creation = %d", int32(handle))
	}
	store := runtime.CFiles[name]
	if store == nil {
		t.Fatal("the open created no store")
	}
	store.Data = []byte("certificate")
	store.Persist(runtime)

	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, name)); got != 0 {
		t.Fatalf("exists before the delete = %d, want 0", got)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileDelete, name, 1)); got != 0 {
		t.Fatalf("delete = %d, want 0", got)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, name)); got != notFound {
		t.Fatalf("exists after the delete = %d, want the not-found code", got)
	}
	// Opening for reading has to fail too: it is the other question a title
	// asks, and it seeds from the same persisted copy.
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, name, 1)); got != notFound {
		t.Fatalf("read-open after the delete = %d, want the not-found code", got)
	}
	// Deleting what is not there is reported rather than accepted.
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileDelete, name, 1)); got != notFound {
		t.Fatalf("deleting twice = %d, want the not-found code", got)
	}
}

// TestCreatingADeletedDatabaseBringsItBack pins the other half: a title that
// deletes a database and then writes a new one must be able to read the new
// one. A removal list that outlived the create would hide every later write.
func TestCreatingADeletedDatabaseBringsItBack(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())

	const name = "slot.dat"
	wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, name, 4)
	runtime.CFiles[name].Data = []byte("old")
	runtime.CFiles[name].Persist(runtime)
	wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileDelete, name, 1)

	if handle := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, name, 4)); handle < 0 {
		t.Fatalf("create after a delete = %d", handle)
	}
	if got := len(runtime.CFiles[name].Data); got != 0 {
		t.Fatalf("the recreated database holds %d bytes of the deleted one", got)
	}
	runtime.CFiles[name].Data = []byte("new")
	runtime.CFiles[name].Persist(runtime)
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, name)); got != 0 {
		t.Fatalf("exists after recreating = %d, want 0", got)
	}
	if data, ok := runtime.DatabaseSeed(name); !ok || string(data) != "new" {
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
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(saves)

	const name = "ga"
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileMakeDirectory, name, 1)); got != 0 {
		t.Fatalf("the first mkdir = %d, want 0", got)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileMakeDirectory, name, 1)); got != alreadyThere {
		t.Fatalf("the second mkdir = %d, want %d", got, alreadyThere)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, name)); got != 0 {
		t.Fatalf("a made directory answers exists = %d, want 0", got)
	}

	// A second session over the same save tree is where the list earns its
	// keep: nothing is in memory and the store is the only record.
	_, second := wipicrttest.NewRuntime(t)
	second.Client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	if got := int32(wipicrttest.DatabaseCall(t, second, wipicrt.WIPICFileMakeDirectory, name, 1)); got != alreadyThere {
		t.Fatalf("mkdir in a second session = %d, want %d", got, alreadyThere)
	}
	if got := int32(wipicrttest.DatabaseCall(t, second, wipicrt.WIPICFileMakeDirectory, "other", 1)); got != 0 {
		t.Fatalf("mkdir of a name nothing made = %d, want 0", got)
	}
}

// TestAMadeDirectoryIsThereToBothQuestions is one title's whole start-up. It
// makes its own directory, asks the platform about the name it has just made,
// and calls MC_knlExit if the answer is "no such file" — so `MC_fsIsExist` and
// `MC_fsGetFileStat` disagreeing about one name was the difference between a
// title that plays and one that quits inside `startApp`.
func TestAMadeDirectoryIsThereToBothQuestions(t *testing.T) {
	const noSuchName = int32(-12) // M_E_NOENT, which wipicErrorNotFound spells unsigned.
	const record = wipicrt.PlatformDataBase + 0xc000
	saves := t.TempDir()
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(saves)

	const name = "shared"
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileStatByName, name, record)); got != noSuchName {
		t.Fatalf("stat of a name nothing made = %d, want %d", got, noSuchName)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileMakeDirectory, name, 1)); got != 0 {
		t.Fatalf("mkdir = %d, want 0", got)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, name)); got != 0 {
		t.Fatalf("a made directory answers exists = %d, want 0", got)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileStatByName, name, record)); got != 0 {
		t.Fatalf("stat of the directory just made = %d, want 0", got)
	}
	// MH_FileInfo is { attrib, creationTime, size }. A directory holds no
	// bytes and keeps no time, and its attribute word carries MC_FILE_IS_DIR,
	// 0x01 in the specification's HAL page.
	info := make([]byte, 12)
	if err := runtime.Client.Core().Memory().Read(record, info); err != nil {
		t.Fatal(err)
	}
	for index, field := range []string{"attributes", "creation time", "size"} {
		want := uint32(0)
		if index == 0 {
			want = 0x01
		}
		if got := binary.LittleEndian.Uint32(info[index*4:]); got != want {
			t.Fatalf("the %s of a made directory = %d, want %d", field, got, want)
		}
	}

	// A second session over the same save tree is where the list of made
	// names earns its keep: nothing is in memory and the store is the only
	// record, and both questions still have to answer the same way.
	_, second := wipicrttest.NewRuntime(t)
	second.Client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	if got := int32(wipicrttest.DatabaseCall(t, second, wipicrt.WIPICFileStatByName, name, record)); got != 0 {
		t.Fatalf("stat in a second session = %d, want 0", got)
	}
	if got := int32(wipicrttest.DatabaseCall(t, second, wipicrt.WIPICFileStatByName, "other", record)); got != noSuchName {
		t.Fatalf("stat of a name nothing made, in a second session = %d, want %d", got, noSuchName)
	}
}

// renameCall drives MC_fsRename(oldName, newName, aMode), which is the one slot
// in this table that takes two names.
func renameCall(t *testing.T, runtime *wipicrt.Runtime, oldName, newName string) int32 {
	t.Helper()
	const oldAddress = wipicrt.PlatformDataBase + 0xb000
	const newAddress = wipicrt.PlatformDataBase + 0xb400
	if err := runtime.Client.Core().Memory().Write(oldAddress, append([]byte(oldName), 0)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Client.Core().Memory().Write(newAddress, append([]byte(newName), 0)); err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.Context{})
	for index, value := range []uint32{oldAddress, newAddress, 1} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runtime.HandleWIPICFileCall(thread, wipicrt.WIPICFileRename)
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
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(saves)

	const scratch, final = "save.z_", "save.dsk"
	wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, scratch, 4)
	runtime.CFiles[scratch].Data = []byte("a season")
	runtime.CFiles[scratch].Persist(runtime)

	if got := renameCall(t, runtime, scratch, final); got != 0 {
		t.Fatalf("rename = %d, want 0", got)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, final)); got != 0 {
		t.Fatalf("exists(%q) = %d, want 0", final, got)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, scratch)); got != notFound {
		t.Fatalf("exists(%q) after the rename = %d, want the not-found code", scratch, got)
	}

	// A second session over the same save tree has nothing in memory, which is
	// where a rename that moved only the live store would show.
	_, second := wipicrttest.NewRuntime(t)
	second.Client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	if data, ok := second.DatabaseSeed(final); !ok || string(data) != "a season" {
		t.Fatalf("the renamed file in a second session = %q ok=%t", data, ok)
	}
	if _, ok := second.DatabaseSeed(scratch); ok {
		t.Fatal("the scratch name came back in a second session")
	}
}

// The one refusal the specification spells out: a name that is taken cannot be
// renamed onto, and the file that was there stays there.
func TestRenameRefusesANameThatIsTaken(t *testing.T) {
	const alreadyThere = int32(-5) // M_E_EXIST
	const failed = int32(-1)       // M_E_ERROR
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())

	wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, "one", 4)
	runtime.CFiles["one"].Data = []byte("first")
	wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, "two", 4)
	runtime.CFiles["two"].Data = []byte("second")

	if got := renameCall(t, runtime, "one", "two"); got != alreadyThere {
		t.Fatalf("rename onto a taken name = %d, want %d", got, alreadyThere)
	}
	if got := string(runtime.CFiles["two"].Data); got != "second" {
		t.Fatalf("the refused rename wrote %q over the file that was there", got)
	}
	if got := string(runtime.CFiles["one"].Data); got != "first" {
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
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())

	wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, "lo.z_", 4)
	runtime.CFiles["lo.z_"].Data = []byte("installed")
	if got := renameCall(t, runtime, "lo.z_", "/lo.dsk"); got != 0 {
		t.Fatalf("rename to an absolute name = %d, want 0", got)
	}
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, "lo.dsk")); got != 0 {
		t.Fatalf("exists(%q) = %d, want 0", "lo.dsk", got)
	}
	if handle := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, "/lo.dsk", 1)); handle < 0 {
		t.Fatalf("opening the absolute spelling = %d", handle)
	}
	// What follows the leading separator is part of the name: a title that
	// keeps its files under a directory of its own is naming different files.
	if got := int32(wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, "/res/lo.dsk")); got == 0 {
		t.Fatal("a name under a directory resolved to the one at the root")
	}
}

// closeDatabaseHandle drives MC_fsClose.
func closeDatabaseHandle(t *testing.T, runtime *wipicrt.Runtime, handle uint32) {
	t.Helper()
	thread := armcore.NewThread(armcore.Context{})
	if err := thread.SetRegister(0, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.HandleWIPICFileCall(thread, wipicrt.WIPICFileClose); err != nil {
		t.Fatal(err)
	}
}

// TestFileNamesThatAreNotKeysOfTheirOwnAreRefused covers the names the WIPI C
// file table took without asking whether they could be stored. "/" trims to
// nothing and "." and "./" normalize away, so each was persisted under the
// table's own scope: a regular file named "db" where the directory belongs,
// after which every save of the title failed. A name with ".." failed its key
// on the first read and left the session unable to save anything; a name with
// a line break wrote a removal list that hid two unrelated files. The root is
// a directory to every question about one (docs/wipi-storage.md) and still no
// file to open. Every refusal of a name the store cannot keep is
// M_E_BADFILENAME, as LGT answers it; these were M_E_INVALID and, for
// MC_fsIsExist, M_E_NOENT.
func TestFileNamesThatAreNotKeysOfTheirOwnAreRefused(t *testing.T) {
	root := t.TempDir()
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(root)

	for _, name := range []string{"/", ".", "./"} {
		if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, name, 4); got != wipicrt.WIPICErrorBadFileName {
			t.Errorf("open(%q) = %#x, want M_E_BADFILENAME", name, got)
		}
		if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, name); got != 0 {
			t.Errorf("exists(%q) = %#x, want 0 for the root", name, got)
		}
		if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileStatByName, name, 0); got != 0 {
			t.Errorf("stat(%q) = %#x, want 0 for the root", name, got)
		}
		if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileMakeDirectory, name, 1); got != wipicrt.WIPICErrorExists {
			t.Errorf("mkdir(%q) = %#x, want M_E_EXIST for the root", name, got)
		}
	}
	for _, name := range []string{"a/../b", ".."} {
		if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, name, 4); got != wipicrt.WIPICErrorBadFileName {
			t.Errorf("open(%q) = %#x, want M_E_BADFILENAME", name, got)
		}
		if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, name); got != wipicrt.WIPICErrorBadFileName {
			t.Errorf("exists(%q) = %#x, want M_E_BADFILENAME", name, got)
		}
		if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileStatByName, name, 0); got != wipicrt.WIPICErrorBadFileName {
			t.Errorf("stat(%q) = %#x, want M_E_BADFILENAME", name, got)
		}
		if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileMakeDirectory, name, 1); got != wipicrt.WIPICErrorBadFileName {
			t.Errorf("mkdir(%q) = %#x, want M_E_BADFILENAME", name, got)
		}
	}
	if runtime.SaveReadError != nil {
		t.Fatalf("a refused name left the session unable to save: %v", runtime.SaveReadError)
	}
	if info, err := os.Stat(filepath.Join(root, "db")); err == nil && !info.IsDir() {
		t.Fatal("a refused name was stored over the table's own directory")
	}

	// A save written afterwards lands where it always did.
	handle := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 4)
	if int32(handle) < 0 {
		t.Fatalf("open(save.dat) = %d", int32(handle))
	}
	runtime.CFiles["save.dat"].Data = []byte("progress")
	if err := runtime.CFiles["save.dat"].Persist(runtime); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "db", "save.dat")); err != nil || string(data) != "progress" {
		t.Fatalf("save after the refusals = %q, %v", data, err)
	}

	// Deleting or renaming a name the removal list cannot carry is refused
	// rather than written into the list.
	runtime.CFiles["a\nsave.dat"] = &wipicrt.RuntimeCFile{Name: "a\nsave.dat", Data: []byte("x")}
	if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileDelete, "a\nsave.dat", 1); got != wipicrt.WIPICErrorBadFileName {
		t.Errorf("delete(a\\nsave.dat) = %#x, want M_E_BADFILENAME", got)
	}
	if got := renameCall(t, runtime, "a\nsave.dat", "other.dat"); uint32(got) != wipicrt.WIPICErrorBadFileName {
		t.Errorf("rename(a\\nsave.dat) = %d, want M_E_BADFILENAME", got)
	}
	if got := renameCall(t, runtime, "save.dat", "."); uint32(got) != wipicrt.WIPICErrorBadFileName {
		t.Errorf("rename(save.dat, .) = %d, want M_E_BADFILENAME", got)
	}
	if runtime.RemovedDatabases()["save.dat"] || runtime.RemovedDatabases()["a"] {
		t.Fatal("a refused delete or rename reached the removal list")
	}
	if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, "save.dat"); got != 0 {
		t.Fatalf("exists(save.dat) after the refusals = %#x", got)
	}
	closeDatabaseHandle(t, runtime, handle)
}

// TestFileHandlesAreRecycledAfterTheCounterRunsOut covers a session that opens
// and closes more files than there are handle numbers. The counter only went
// up, so the open after the 4095th failed whatever had been closed — the open
// a title makes to write its save among them.
func TestFileHandlesAreRecycledAfterTheCounterRunsOut(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	// Mode 2 stores only the open that creates the file, so the loop is not
	// four thousand store writes.
	for open := 0; open < wipicrt.MaxCFileHandles+16; open++ {
		handle := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 2)
		if handle&0xfffff000 != wipicrt.CFileHandleBit || handle&0x0fff == 0 {
			t.Fatalf("open %d answered %#x, want a file handle", open+1, handle)
		}
		closeDatabaseHandle(t, runtime, handle)
	}
	// A handle still open is never handed out twice.
	held := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 2)
	other := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 2)
	if held == other || held&0xfffff000 != wipicrt.CFileHandleBit || other&0xfffff000 != wipicrt.CFileHandleBit {
		t.Fatalf("two open handles = %#x and %#x", held, other)
	}
}

// TestStatAfterRemoveReportsNoFile covers MC_fsGetFileStat after MC_fsRemove.
// The remove leaves an empty entry under the old key, because the save store
// cannot delete, and the stat read that entry as a file of zero bytes while
// MC_fsIsExist answered that there was none.
func TestStatAfterRemoveReportsNoFile(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.Client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	handle := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 4)
	runtime.CFiles["save.dat"].Data = []byte("progress")
	if err := runtime.CFiles["save.dat"].Persist(runtime); err != nil {
		t.Fatal(err)
	}
	closeDatabaseHandle(t, runtime, handle)
	if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileDelete, "save.dat", 1); got != 0 {
		t.Fatalf("delete = %#x", got)
	}
	if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileExists, "save.dat"); got != wipicrt.WIPICErrorNotFound {
		t.Fatalf("exists after the delete = %#x, want M_E_NOENT", got)
	}
	if got := wipicrttest.DatabaseCall(t, runtime, wipicrt.WIPICFileStatByName, "save.dat", 0); got != wipicrt.WIPICErrorNotFound {
		t.Fatalf("stat after the delete = %#x, want M_E_NOENT", got)
	}
}

// TestFreeSpaceCountsTheSavesOfEarlierSessions holds MC_fsAvailable to
// docs/wipi-storage.md: the budget less what the title has stored, whichever
// session stored it, each file less the archive's copy of the same name. A
// platform with no count of its own, the SKT Clet, counted only the files this
// session had opened, so a program that asked before opening its save was told
// the save took no room.
func TestFreeSpaceCountsTheSavesOfEarlierSessions(t *testing.T) {
	saves := t.TempDir()
	packaged := func(client *wipicrt.Client) {
		client.Files = map[string][]byte{"level.dat": make([]byte, 400)}
	}
	_, first := wipicrttest.NewRuntimeWith(t, armcore.CoreOptions{}, packaged)
	first.Client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	for name, size := range map[string]int{"save.dat": 1000, "level.dat": 1000, "gone.dat": 300} {
		if handle := int32(wipicrttest.DatabaseCall(t, first, wipicrt.WIPICFileOpen, name, 4)); handle < 0 {
			t.Fatalf("open %s for creation = %d", name, handle)
		}
		first.CFiles[name].Data = make([]byte, size)
		if err := first.CFiles[name].Persist(first); err != nil {
			t.Fatal(err)
		}
	}
	if got := wipicrttest.DatabaseCall(t, first, wipicrt.WIPICFileDelete, "gone.dat", 1); got != 0 {
		t.Fatalf("delete = %#x", got)
	}

	_, second := wipicrttest.NewRuntimeWith(t, armcore.CoreOptions{}, packaged)
	second.Client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	// save.dat whole, level.dat over the 400 bytes the archive ships, and
	// nothing of the deleted gone.dat.
	const want = wipicrt.WIPICFileStorageLimit - 1000 - 600
	if got := wipicrttest.DatabaseCall(t, second, wipicrt.WIPICFileAvailable, ""); got != want {
		t.Fatalf("free space before the saves are opened = %d, want %d", got, want)
	}
	for _, name := range []string{"save.dat", "level.dat"} {
		if handle := int32(wipicrttest.DatabaseCall(t, second, wipicrt.WIPICFileOpen, name, 1)); handle < 0 {
			t.Fatalf("%s does not open: %d", name, handle)
		}
		if got := wipicrttest.DatabaseCall(t, second, wipicrt.WIPICFileAvailable, ""); got != want {
			t.Fatalf("free space once %s is open = %d, want %d", name, got, want)
		}
	}
}
