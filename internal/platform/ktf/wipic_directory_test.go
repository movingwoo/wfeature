package ktf

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// listCall drives MC_fsList with a buffer of the given size and answers the
// result and the buffer's contents.
func listCall(t *testing.T, runtime *initializationRuntime, directory string, size uint32) (uint32, []byte) {
	t.Helper()
	const buffer = wipicrt.PlatformDataBase + 0xb000
	fill := bytes.Repeat([]byte{0xee}, int(size)+8)
	if err := runtime.client.Core().Memory().Write(buffer, fill); err != nil {
		t.Fatal(err)
	}
	result := databaseCall(t, runtime, wipicrt.WIPICFileList, directory, buffer, size, 1)
	got := make([]byte, size+8)
	if err := runtime.client.Core().Memory().Read(buffer, got); err != nil {
		t.Fatal(err)
	}
	return result, got
}

// A title asks slot 11 for the size of the file system with no argument, and
// what r0 holds then is whatever the code before it left there. Read as a
// handle it answered M_E_INVALID, and the title said there was no room to save
// and stayed on its title screen.
func TestTotalSpaceTakesNoArgument(t *testing.T) {
	_, runtime := newTestRuntime(t)
	thread := armcore.NewThread(armcore.Context{})
	if err := thread.SetRegister(0, 0x35001d81); err != nil {
		t.Fatal(err)
	}
	total, err := runtime.HandleWIPICFileCall(thread, wipicrt.WIPICFileTotalSpace)
	if err != nil {
		t.Fatal(err)
	}
	if total != wipicrt.WIPICFileStorageLimit {
		t.Fatalf("MC_fsTotalSpace = %#x, want %#x", total, wipicrt.WIPICFileStorageLimit)
	}
	available, err := runtime.HandleWIPICFileCall(thread, wipicrt.WIPICFileAvailable)
	if err != nil {
		t.Fatal(err)
	}
	if available > total {
		t.Fatalf("MC_fsAvailable %d is more than MC_fsTotalSpace %d", available, total)
	}
}

// A listing names what the table can open, so a save an earlier session wrote
// has to be in it: that session's stores are gone, and nothing in this one
// has named the file yet.
func TestListNamesSavedFilesAfterARestart(t *testing.T) {
	saves := t.TempDir()
	_, first := newTestRuntime(t)
	first.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	for _, name := range []string{"slot1.sav", "slots/a", "slots/b", "gone.sav", "./dotted.sav"} {
		databaseCall(t, first, wipicrt.WIPICFileOpen, name, 4)
		first.CFiles[name].Data = []byte("progress " + name)
		if err := first.CFiles[name].Persist(first.Runtime); err != nil {
			t.Fatal(err)
		}
	}
	// The second is removed under the spelling it was made with, which the
	// store keeps without its "." segment.
	for _, name := range []string{"gone.sav", "./dotted.sav"} {
		if got := int32(databaseCall(t, first, wipicrt.WIPICFileDelete, name, 1)); got != 0 {
			t.Fatalf("delete(%q) = %d", name, got)
		}
	}
	if got := int32(databaseCall(t, first, wipicrt.WIPICFileMakeDirectory, "maps", 1)); got != 0 {
		t.Fatalf("mkdir = %d", got)
	}

	_, second := newTestRuntime(t)
	second.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	second.client.Files = map[string][]byte{"res/title.png": {1}, "intro.dat": {2}}
	databaseCall(t, second, wipicrt.WIPICFileOpen, "live.tmp", 4)

	result, got := listCall(t, second, "/", 64)
	if result != 0 {
		t.Fatalf("MC_fsList(/) = %d", int32(result))
	}
	want := []byte("intro.dat\x00live.tmp\x00maps\x00res\x00slot1.sav\x00slots\x00\x00")
	if !bytes.HasPrefix(got, want) {
		t.Fatalf("MC_fsList(/) wrote %q, want %q", got[:len(want)], want)
	}

	if result, got = listCall(t, second, "slots", 16); result != 0 || !bytes.HasPrefix(got, []byte("a\x00b\x00\x00")) {
		t.Fatalf("MC_fsList(slots) = %d, %q", result, got[:5])
	}
	if count := databaseCall(t, second, wipicrt.WIPICFileGetCounts, "/slots/", 1); count != 2 {
		t.Fatalf("MC_fsGetCounts(slots) = %d, want 2", count)
	}
	if count := databaseCall(t, second, wipicrt.WIPICFileGetCounts, "", 1); count != 6 {
		t.Fatalf("MC_fsGetCounts(root) = %d, want 6", count)
	}
}

func TestListAnswersTheSpecificationsFailures(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())

	// An empty directory is the terminating pair alone.
	if result, got := listCall(t, runtime, "empty", 8); result != 0 || !bytes.HasPrefix(got, []byte{0, 0, 0xee}) {
		t.Fatalf("MC_fsList(empty) = %d, %x", result, got[:3])
	}
	databaseCall(t, runtime, wipicrt.WIPICFileOpen, "a-long-file-name.sav", 4)
	// A buffer the list does not fit is refused and left as it was.
	result, got := listCall(t, runtime, "", 8)
	if result != wipicrt.WIPICErrorShortBuf || got[0] != 0xee {
		t.Fatalf("MC_fsList into 8 bytes = %d, %x", result, got[:2])
	}
	if result, _ := listCall(t, runtime, "../outside", 64); result != wipicrt.WIPICErrorBadFileName {
		t.Fatalf("MC_fsList(../outside) = %d, want M_E_BADFILENAME", result)
	}
}

// A directory is a name MC_fsMkDir wrote down; removing one takes it off that
// list, but only when it holds nothing, and the removal lasts a restart.
func TestRemoveDirectory(t *testing.T) {
	saves := t.TempDir()
	_, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)

	if got := databaseCall(t, runtime, wipicrt.WIPICFileRemoveDirectory, "never", 1); got != wipicrt.WIPICErrorNotFound {
		t.Fatalf("rmdir of a directory never made = %d, want M_E_NOENT", got)
	}
	databaseCall(t, runtime, wipicrt.WIPICFileMakeDirectory, "save", 1)
	databaseCall(t, runtime, wipicrt.WIPICFileOpen, "save/slot", 4)
	if got := databaseCall(t, runtime, wipicrt.WIPICFileRemoveDirectory, "save", 1); got != wipicrt.WIPICErrorNotEmpty {
		t.Fatalf("rmdir of a directory holding a file = %d, want M_E_NOTEMPTY", got)
	}
	if got := int32(databaseCall(t, runtime, wipicrt.WIPICFileDelete, "save/slot", 1)); got != 0 {
		t.Fatalf("delete = %d", got)
	}
	if got := int32(databaseCall(t, runtime, wipicrt.WIPICFileRemoveDirectory, "/save", 1)); got != 0 {
		t.Fatalf("rmdir of an empty directory = %d, want 0", got)
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileExists, "save"); got != wipicrt.WIPICErrorNotFound {
		t.Fatalf("exists after rmdir = %d, want M_E_NOENT", got)
	}

	_, second := newTestRuntime(t)
	second.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	if got := databaseCall(t, second, wipicrt.WIPICFileExists, "save"); got != wipicrt.WIPICErrorNotFound {
		t.Fatalf("the removed directory came back in a second session: exists = %d", got)
	}
}

func TestSetModeChecksTheModeAndTheFile(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	databaseCall(t, runtime, wipicrt.WIPICFileOpen, "save.dat", 4)

	for _, check := range []struct {
		name string
		mode uint32
		want uint32
	}{
		{"save.dat", 1, 0},
		{"save.dat", 3, 0},
		{"save.dat", 0, wipicrt.WIPICErrorInvalid},
		{"save.dat", 4, wipicrt.WIPICErrorInvalid},
		{"missing.dat", 1, wipicrt.WIPICErrorNotFound},
		{"../outside", 1, wipicrt.WIPICErrorBadFileName},
	} {
		if got := databaseCall(t, runtime, wipicrt.WIPICFileSetMode, check.name, check.mode, 1); got != check.want {
			t.Errorf("MC_fsSetMode(%q, %d) = %#x, want %#x", check.name, check.mode, got, check.want)
		}
	}
}
