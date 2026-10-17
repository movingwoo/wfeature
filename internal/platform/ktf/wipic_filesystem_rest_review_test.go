package ktf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// closeDatabaseHandle drives MC_fsClose.
func closeDatabaseHandle(t *testing.T, runtime *initializationRuntime, handle uint32) {
	t.Helper()
	thread := armcore.NewThread(armcore.Context{})
	if err := thread.SetRegister(0, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.handleWIPICFileCall(thread, wipicFileClose); err != nil {
		t.Fatal(err)
	}
}

// TestFileNamesThatAreNotKeysOfTheirOwnAreRefused covers the names the WIPI C
// file table took without asking whether they could be stored. "/" trims to
// nothing and "." and "./" normalize away, so each was persisted under the
// table's own scope: a regular file named "db" where the directory belongs,
// after which every save of the title failed. A name with ".." failed its key
// on the first read and left the session unable to save anything; a name with
// a line break wrote a removal list that hid two unrelated files.
func TestFileNamesThatAreNotKeysOfTheirOwnAreRefused(t *testing.T) {
	root := t.TempDir()
	_, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(root)

	for _, name := range []string{"/", ".", "./", "a/../b", ".."} {
		if got := databaseCall(t, runtime, wipicFileOpen, name, 4); got != wipicErrorInvalid {
			t.Errorf("open(%q) = %#x, want M_E_INVALID", name, got)
		}
		if got := databaseCall(t, runtime, wipicFileExists, name); got != wipicErrorNotFound {
			t.Errorf("exists(%q) = %#x, want M_E_NOENT", name, got)
		}
		if got := databaseCall(t, runtime, wipicFileStatByName, name, 0); got != wipicErrorBadParam {
			t.Errorf("stat(%q) = %#x, want M_E_BADRECID", name, got)
		}
		if got := databaseCall(t, runtime, wipicFileMakeDirectory, name, 1); got != wipicErrorBadParam {
			t.Errorf("mkdir(%q) = %#x, want M_E_BADRECID", name, got)
		}
	}
	if runtime.saveReadError != nil {
		t.Fatalf("a refused name left the session unable to save: %v", runtime.saveReadError)
	}
	if info, err := os.Stat(filepath.Join(root, "db")); err == nil && !info.IsDir() {
		t.Fatal("a refused name was stored over the table's own directory")
	}

	// A save written afterwards lands where it always did.
	handle := databaseCall(t, runtime, wipicFileOpen, "save.dat", 4)
	if int32(handle) < 0 {
		t.Fatalf("open(save.dat) = %d", int32(handle))
	}
	runtime.cFiles["save.dat"].data = []byte("progress")
	if err := runtime.cFiles["save.dat"].persist(runtime); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "db", "save.dat")); err != nil || string(data) != "progress" {
		t.Fatalf("save after the refusals = %q, %v", data, err)
	}

	// Deleting or renaming a name the removal list cannot carry is refused
	// rather than written into the list.
	runtime.cFiles["a\nsave.dat"] = &runtimeCFile{name: "a\nsave.dat", data: []byte("x")}
	if got := databaseCall(t, runtime, wipicFileDelete, "a\nsave.dat", 1); got != wipicErrorInvalid {
		t.Errorf("delete(a\\nsave.dat) = %#x, want M_E_INVALID", got)
	}
	if got := renameCall(t, runtime, "a\nsave.dat", "other.dat"); uint32(got) != wipicErrorInvalid {
		t.Errorf("rename(a\\nsave.dat) = %d, want M_E_INVALID", got)
	}
	if got := renameCall(t, runtime, "save.dat", "."); uint32(got) != wipicErrorInvalid {
		t.Errorf("rename(save.dat, .) = %d, want M_E_INVALID", got)
	}
	if runtime.removedDatabases()["save.dat"] || runtime.removedDatabases()["a"] {
		t.Fatal("a refused delete or rename reached the removal list")
	}
	if got := databaseCall(t, runtime, wipicFileExists, "save.dat"); got != 0 {
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
	runtime.client.saveStore = NewDirectorySaveStore(t.TempDir())
	// Mode 2 stores only the open that creates the file, so the loop is not
	// four thousand store writes.
	for open := 0; open < maxCFileHandles+16; open++ {
		handle := databaseCall(t, runtime, wipicFileOpen, "save.dat", 2)
		if handle&0xfffff000 != cFileHandleBit || handle&0x0fff == 0 {
			t.Fatalf("open %d answered %#x, want a file handle", open+1, handle)
		}
		closeDatabaseHandle(t, runtime, handle)
	}
	// A handle still open is never handed out twice.
	held := databaseCall(t, runtime, wipicFileOpen, "save.dat", 2)
	other := databaseCall(t, runtime, wipicFileOpen, "save.dat", 2)
	if held == other || held&0xfffff000 != cFileHandleBit || other&0xfffff000 != cFileHandleBit {
		t.Fatalf("two open handles = %#x and %#x", held, other)
	}
}

// TestRecordDatabaseHandlesAreRecycledAfterTheCounterRunsOut is the same rule
// for the record database table, which issued its handles the same way.
func TestRecordDatabaseHandlesAreRecycledAfterTheCounterRunsOut(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(t.TempDir())
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
	runtime.client.saveStore = NewDirectorySaveStore(t.TempDir())
	handle := databaseCall(t, runtime, wipicFileOpen, "save.dat", 4)
	runtime.cFiles["save.dat"].data = []byte("progress")
	if err := runtime.cFiles["save.dat"].persist(runtime); err != nil {
		t.Fatal(err)
	}
	closeDatabaseHandle(t, runtime, handle)
	if got := databaseCall(t, runtime, wipicFileDelete, "save.dat", 1); got != 0 {
		t.Fatalf("delete = %#x", got)
	}
	if got := databaseCall(t, runtime, wipicFileExists, "save.dat"); got != wipicErrorNotFound {
		t.Fatalf("exists after the delete = %#x, want M_E_NOENT", got)
	}
	if got := databaseCall(t, runtime, wipicFileStatByName, "save.dat", 0); got != wipicErrorBadParam {
		t.Fatalf("stat after the delete = %#x, want the missing-name answer", got)
	}
}
