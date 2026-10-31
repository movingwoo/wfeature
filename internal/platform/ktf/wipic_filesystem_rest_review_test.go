package ktf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// closeDatabaseHandle drives MC_fsClose.
func closeDatabaseHandle(t *testing.T, runtime *initializationRuntime, handle uint32) {
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
	_, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(root)

	for _, name := range []string{"/", ".", "./"} {
		if got := databaseCall(t, runtime, wipicrt.WIPICFileOpen, name, 4); got != wipicrt.WIPICErrorBadFileName {
			t.Errorf("open(%q) = %#x, want M_E_BADFILENAME", name, got)
		}
		if got := databaseCall(t, runtime, wipicrt.WIPICFileExists, name); got != 0 {
			t.Errorf("exists(%q) = %#x, want 0 for the root", name, got)
		}
		if got := databaseCall(t, runtime, wipicrt.WIPICFileStatByName, name, 0); got != 0 {
			t.Errorf("stat(%q) = %#x, want 0 for the root", name, got)
		}
		if got := databaseCall(t, runtime, wipicrt.WIPICFileMakeDirectory, name, 1); got != wipicrt.WIPICErrorExists {
			t.Errorf("mkdir(%q) = %#x, want M_E_EXIST for the root", name, got)
		}
	}
	for _, name := range []string{"a/../b", ".."} {
		if got := databaseCall(t, runtime, wipicrt.WIPICFileOpen, name, 4); got != wipicrt.WIPICErrorBadFileName {
			t.Errorf("open(%q) = %#x, want M_E_BADFILENAME", name, got)
		}
		if got := databaseCall(t, runtime, wipicrt.WIPICFileExists, name); got != wipicrt.WIPICErrorBadFileName {
			t.Errorf("exists(%q) = %#x, want M_E_BADFILENAME", name, got)
		}
		if got := databaseCall(t, runtime, wipicrt.WIPICFileStatByName, name, 0); got != wipicrt.WIPICErrorBadFileName {
			t.Errorf("stat(%q) = %#x, want M_E_BADFILENAME", name, got)
		}
		if got := databaseCall(t, runtime, wipicrt.WIPICFileMakeDirectory, name, 1); got != wipicrt.WIPICErrorBadFileName {
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
	handle := databaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 4)
	if int32(handle) < 0 {
		t.Fatalf("open(save.dat) = %d", int32(handle))
	}
	runtime.CFiles["save.dat"].Data = []byte("progress")
	if err := runtime.CFiles["save.dat"].Persist(runtime.Runtime); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "db", "save.dat")); err != nil || string(data) != "progress" {
		t.Fatalf("save after the refusals = %q, %v", data, err)
	}

	// Deleting or renaming a name the removal list cannot carry is refused
	// rather than written into the list.
	runtime.CFiles["a\nsave.dat"] = &wipicrt.RuntimeCFile{Name: "a\nsave.dat", Data: []byte("x")}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileDelete, "a\nsave.dat", 1); got != wipicrt.WIPICErrorBadFileName {
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
	if got := databaseCall(t, runtime, wipicrt.WIPICFileExists, "save.dat"); got != 0 {
		t.Fatalf("exists(save.dat) after the refusals = %#x", got)
	}
	closeDatabaseHandle(t, runtime, handle)
}

// TestFileHandlesAreRecycledAfterTheCounterRunsOut covers a session that opens
// and closes more files than there are handle numbers. The counter only went
// up, so the open after the 4095th failed whatever had been closed — the open
// a title makes to write its save among them.
func TestFileHandlesAreRecycledAfterTheCounterRunsOut(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	// Mode 2 stores only the open that creates the file, so the loop is not
	// four thousand store writes.
	for open := 0; open < wipicrt.MaxCFileHandles+16; open++ {
		handle := databaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 2)
		if handle&0xfffff000 != wipicrt.CFileHandleBit || handle&0x0fff == 0 {
			t.Fatalf("open %d answered %#x, want a file handle", open+1, handle)
		}
		closeDatabaseHandle(t, runtime, handle)
	}
	// A handle still open is never handed out twice.
	held := databaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 2)
	other := databaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 2)
	if held == other || held&0xfffff000 != wipicrt.CFileHandleBit || other&0xfffff000 != wipicrt.CFileHandleBit {
		t.Fatalf("two open handles = %#x and %#x", held, other)
	}
}

// TestRecordDatabaseHandlesAreRecycledAfterTheCounterRunsOut is the same rule
// for the record database table, which issued its handles the same way.
func TestRecordDatabaseHandlesAreRecycledAfterTheCounterRunsOut(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	for open := 0; open < maxRecordDatabaseHandles+16; open++ {
		handle := openRecordDatabase(t, runtime, "scores", 8, 1)
		if handle&0xfffff000 != recordDatabaseHandleBit || handle&0x0fff == 0 {
			t.Fatalf("open %d answered %#x, want a record database handle", open+1, handle)
		}
		thread := armcore.NewThread(armcore.Context{})
		if err := thread.SetRegister(0, handle); err != nil {
			t.Fatal(err)
		}
		if result, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseClose); err != nil || result != 0 {
			t.Fatalf("close %#x = %#x, %v", handle, result, err)
		}
	}
}

// TestStatAfterRemoveReportsNoFile covers MC_fsGetFileStat after MC_fsRemove.
// The remove leaves an empty entry under the old key, because the save store
// cannot delete, and the stat read that entry as a file of zero bytes while
// MC_fsIsExist answered that there was none.
func TestStatAfterRemoveReportsNoFile(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	handle := databaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 4)
	runtime.CFiles["save.dat"].Data = []byte("progress")
	if err := runtime.CFiles["save.dat"].Persist(runtime.Runtime); err != nil {
		t.Fatal(err)
	}
	closeDatabaseHandle(t, runtime, handle)
	if got := databaseCall(t, runtime, wipicrt.WIPICFileDelete, "save.dat", 1); got != 0 {
		t.Fatalf("delete = %#x", got)
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileExists, "save.dat"); got != wipicrt.WIPICErrorNotFound {
		t.Fatalf("exists after the delete = %#x, want M_E_NOENT", got)
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileStatByName, "save.dat", 0); got != wipicrt.WIPICErrorNotFound {
		t.Fatalf("stat after the delete = %#x, want M_E_NOENT", got)
	}
}
