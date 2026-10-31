package ktf

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// These tests hold KTF to docs/wipi-storage.md, the contract all three
// platforms keep, one rule at a time.

// javaName is a guest String argument.
func javaName(client *Client, name string) jvm.Value {
	return jvm.ReferenceValue(client.JVM().NewString(name))
}

// javaCall runs one FileSystem or DataBase native and answers its result.
func javaCall(t *testing.T, runtime *initializationRuntime, client *Client, native func(*initializationRuntime, *jvm.VM, []jvm.Value) (jvm.Value, error), arguments ...jvm.Value) (jvm.Value, error) {
	t.Helper()
	return native(runtime, client.JVM(), arguments)
}

// javaBool reads a boolean answer; an error is a failed test.
func javaBool(value jvm.Value, err error) bool {
	if err != nil {
		panic(err)
	}
	got, _ := value.Int32()
	return got != 0
}

func TestContractNamesLongerThan255AreRefused(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	long := strings.Repeat("n", wipicrt.MaxWIPIFileName+1)
	fits := strings.Repeat("n", wipicrt.MaxWIPIFileName)

	for _, slot := range []uint32{wipicrt.WIPICFileExists, wipicrt.WIPICFileMakeDirectory, wipicrt.WIPICFileRemoveDirectory, wipicrt.WIPICFileGetCounts, wipicrt.WIPICFileDelete} {
		if got := databaseCall(t, runtime, slot, long, 1); got != wipicrt.WIPICErrorLongName {
			t.Errorf("slot %d with a 256-byte name = %#x, want M_E_LONGNAME", slot, got)
		}
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileOpen, long, 4); got != wipicrt.WIPICErrorLongName {
		t.Errorf("open with a 256-byte name = %#x, want M_E_LONGNAME", got)
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileStatByName, long, 0); got != wipicrt.WIPICErrorLongName {
		t.Errorf("stat with a 256-byte name = %#x, want M_E_LONGNAME", got)
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileSetMode, long, 1, 1); got != wipicrt.WIPICErrorLongName {
		t.Errorf("setmode with a 256-byte name = %#x, want M_E_LONGNAME", got)
	}
	if got := int32(databaseCall(t, runtime, wipicrt.WIPICFileOpen, fits, 4)); got < 0 {
		t.Errorf("open with a 255-byte name = %d, want a handle", got)
	}

	if value, err := javaCall(t, runtime, client, runtimeFileSystemNameLength); err != nil || mustInt(value) != 255 {
		t.Fatalf("getMaxFilenameLength = %v, %v", value, err)
	}
	if _, err := javaCall(t, runtime, client, runtimeFileSystemExists, javaName(client, long)); !isGuestException(err, "java/io/IOException") {
		t.Errorf("exists of a 256-byte name = %v, want IOException", err)
	}
	if _, err := javaCall(t, runtime, client, runtimeFileSystemExists, jvm.ReferenceValue(nil)); !isGuestException(err, "java/lang/NullPointerException") {
		t.Errorf("exists(null) = %v, want NullPointerException", err)
	}
}

func mustInt(value jvm.Value) int32 {
	got, _ := value.Int32()
	return got
}

// A directory is the root, one mkdir recorded, or one a file lies below; a
// file is not one. Both halves answer alike.
func TestContractDirectoriesAnswerAlikeInCAndJava(t *testing.T) {
	saves := t.TempDir()
	client, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	databaseCall(t, runtime, wipicrt.WIPICFileOpen, "slots/one", 4)

	// C: the implied directory "slots" exists, is a directory, cannot be made
	// again and is not empty; the file cannot be made a directory.
	if got := databaseCall(t, runtime, wipicrt.WIPICFileExists, "slots"); got != 0 {
		t.Errorf("C exists(slots) = %#x, want 0", got)
	}
	const record = wipicrt.PlatformDataBase + 0xd000
	if got := databaseCall(t, runtime, wipicrt.WIPICFileStatByName, "slots", record); got != 0 {
		t.Errorf("C stat(slots) = %#x, want 0", got)
	}
	info := make([]byte, 12)
	if err := runtime.client.Core().Memory().Read(record, info); err != nil {
		t.Fatal(err)
	}
	if attributes := binary.LittleEndian.Uint32(info); attributes != 0x01 {
		t.Errorf("C stat(slots) attributes = %#x, want MC_FILE_IS_DIR", attributes)
	}
	for _, name := range []string{"slots", "slots/one", "/"} {
		if got := databaseCall(t, runtime, wipicrt.WIPICFileMakeDirectory, name, 1); got != wipicrt.WIPICErrorExists {
			t.Errorf("C mkdir(%q) = %#x, want M_E_EXIST", name, got)
		}
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileRemoveDirectory, "slots", 1); got != wipicrt.WIPICErrorNotEmpty {
		t.Errorf("C rmdir(slots) = %#x, want M_E_NOTEMPTY", got)
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileStatByName, "missing", record); got != wipicrt.WIPICErrorNotFound {
		t.Errorf("C stat(missing) = %#x, want M_E_NOENT", got)
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileDelete, "missing", 1); got != wipicrt.WIPICErrorNotFound {
		t.Errorf("C remove(missing) = %#x, want M_E_NOENT", got)
	}
	for _, mode := range []uint32{1, 3, 0x10, 0x30} {
		if got := databaseCall(t, runtime, wipicrt.WIPICFileSetMode, "slots", mode, 1); got != 0 {
			t.Errorf("C setmode(slots, %#x) = %#x, want 0", mode, got)
		}
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileSetMode, "slots/one", 0x40, 1); got != wipicrt.WIPICErrorInvalid {
		t.Errorf("C setmode with mode 0x40 = %#x, want M_E_INVALID", got)
	}

	// Java over its own files: the same rules.
	if err := runtime.storeGuestFile("saves/slot", []byte("progress")); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"saves": true, "saves/slot": true, "/": true, "missing": false} {
		if got := javaBool(javaCall(t, runtime, client, runtimeFileSystemExists, javaName(client, name))); got != want {
			t.Errorf("Java exists(%q) = %t, want %t", name, got, want)
		}
	}
	if javaBool(javaCall(t, runtime, client, runtimeFileSystemIsDirectory, javaName(client, "saves/slot"))) {
		t.Error("Java isDirectory of a file answered true")
	}
	if javaBool(javaCall(t, runtime, client, runtimeFileSystemIsFile, javaName(client, "saves"))) {
		t.Error("Java isFile of a directory answered true")
	}
	for _, name := range []string{"saves", "saves/slot", "/"} {
		if _, err := javaCall(t, runtime, client, runtimeFileSystemMakeDirectory, javaName(client, name)); !isGuestException(err, "java/io/IOException") {
			t.Errorf("Java mkdir(%q) = %v, want IOException", name, err)
		}
	}
	if _, err := javaCall(t, runtime, client, runtimeFileSystemMakeDirectory, javaName(client, "empty")); err != nil {
		t.Fatalf("Java mkdir(empty) = %v", err)
	}
	for _, name := range []string{"saves", "missing", "/", "saves/slot"} {
		if _, err := javaCall(t, runtime, client, runtimeFileSystemRemoveDirectory, javaName(client, name)); !isGuestException(err, "java/io/IOException") {
			t.Errorf("Java rmdir(%q) = %v, want IOException", name, err)
		}
	}
	if value, err := javaCall(t, runtime, client, runtimeFileSystemCreationTime, javaName(client, "saves")); err != nil || mustInt(value) != -1 {
		t.Errorf("Java getCreationTime(saves) = %v, %v, want -1", value, err)
	}
	if value, err := javaCall(t, runtime, client, runtimeFileSystemCreationTime, javaName(client, "saves/slot")); err != nil || mustInt(value) != 0 {
		t.Errorf("Java getCreationTime(saves/slot) = %v, %v, want 0", value, err)
	}
	if _, err := javaCall(t, runtime, client, runtimeFileSystemCreationTime, javaName(client, "missing")); !isGuestException(err, "java/io/IOException") {
		t.Errorf("Java getCreationTime(missing) = %v, want IOException", err)
	}
	if _, err := javaCall(t, runtime, client, runtimeFileSystemRemove, javaName(client, "missing")); !isGuestException(err, "java/io/IOException") {
		t.Errorf("Java remove(missing) = %v, want IOException", err)
	}
	if _, err := javaCall(t, runtime, client, runtimeFileSystemRemoveAny, javaName(client, "missing"), jvm.IntValue(1)); err != nil {
		t.Errorf("Java remove(missing, flag) = %v, want no exception", err)
	}

	// The made directory and the listing outlast the session.
	client2, second := newTestRuntime(t)
	second.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	if !javaBool(javaCall(t, second, client2, runtimeFileSystemIsDirectory, javaName(client2, "empty"))) {
		t.Error("a directory mkdir made and left empty is gone after a restart")
	}
	listed, err := javaCall(t, second, client2, runtimeFileSystemList, javaName(client2, "/"))
	if err != nil {
		t.Fatal(err)
	}
	vector, _ := listed.Reference()
	size, err := client2.JVM().InvokeVirtual(vector, "size", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if got := mustInt(size); got != 2 {
		t.Errorf("Java list(/) after a restart = %d entries, want empty and saves", got)
	}
	if _, err := javaCall(t, second, client2, runtimeFileSystemRemoveDirectory, javaName(client2, "empty")); err != nil {
		t.Errorf("Java rmdir(empty) = %v", err)
	}
}

// Every title has 1 MiB, less what it has stored; a packaged file it rewrote
// only uses what it added, and nothing is refused for space.
func TestContractSpace(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	client.AttachFilesystem(map[string][]byte{"level.dat": bytes.Repeat([]byte{1}, 500)})

	total := databaseCall(t, runtime, wipicrt.WIPICFileTotalSpace, "")
	if total != 1<<20 {
		t.Fatalf("MC_fsTotalSpace = %d, want 1 MiB", total)
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileAvailable, ""); got != total {
		t.Fatalf("MC_fsAvailable before any save = %d, want %d", got, total)
	}
	if err := runtime.storeGuestFile("save.dat", bytes.Repeat([]byte{2}, 1000)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.storeGuestFile("level.dat", bytes.Repeat([]byte{3}, 800)); err != nil {
		t.Fatal(err)
	}
	want := total - 1000 - 300
	for name, got := range map[string]uint32{
		"MC_fsAvailable":                  databaseCall(t, runtime, wipicrt.WIPICFileAvailable, ""),
		"the record table's slot 12":      databaseRecordCall(t, runtime, wipicRecordDatabaseListNames),
		"FileSystem.available":            uint32(mustInt(must(javaCall(t, runtime, client, runtimeFileSystemAvailable)))),
		"DataBase.getSizeAvailable":       uint32(mustInt(must(javaCall(t, runtime, client, runtimeDataBaseSizeAvailable)))),
		"FileSystem.totalSpace (as used)": uint32(mustInt(must(javaCall(t, runtime, client, runtimeFileSystemTotalSpace)))) - 1300,
	} {
		if got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
}

func must(value jvm.Value, err error) jvm.Value {
	if err != nil {
		panic(err)
	}
	return value
}

// databaseRecordCall drives a record-table slot with no arguments.
func databaseRecordCall(t *testing.T, runtime *initializationRuntime, function uint32) uint32 {
	t.Helper()
	thread := armcore.NewThread(armcore.Context{})
	result, err := runtime.handleWIPICRecordDatabaseCall(thread, function)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// A write past 4 MiB fails as a write does.
func TestContractBoundOnOneFile(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	handle := databaseCall(t, runtime, wipicrt.WIPICFileOpen, "big", 4)
	runtime.CFileHandles[handle].Position = maxStoredBytes - 1
	thread := armcore.NewThread(armcore.Context{})
	for index, value := range []uint32{handle, wipicrt.PlatformDataBase + 0xa000, 2} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	got, err := runtime.HandleWIPICFileCall(thread, wipicrt.WIPICFileStreamWrite)
	if err != nil {
		t.Fatal(err)
	}
	if got != wipicrt.WIPICErrorNoSpace {
		t.Fatalf("a write past 4 MiB = %#x, want M_E_NOSPACE", got)
	}
}

func openJavaDatabase(t *testing.T, runtime *initializationRuntime, client *Client, name string, size int32, create bool) (jvm.Value, error) {
	t.Helper()
	flag := int32(0)
	if create {
		flag = 1
	}
	return javaCall(t, runtime, client, runtimeOpenDataBase, javaName(client, name), jvm.IntValue(size), jvm.IntValue(flag))
}

func TestContractDatabases(t *testing.T) {
	saves := t.TempDir()
	client, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)

	if runtimeJavaClasses[runtimeDataBaseRecordClass].superName != "java/lang/Exception" {
		t.Errorf("DataBaseRecordException extends %s, want java/lang/Exception", runtimeJavaClasses[runtimeDataBaseRecordClass].superName)
	}
	if list := must(javaCall(t, runtime, client, runtimeDataBaseListNames)); !isNull(list) {
		t.Error("listDataBases with no database is not null")
	}
	if _, err := openJavaDatabase(t, runtime, client, "scores", 0, true); !isGuestException(err, "java/lang/IllegalArgumentException") {
		t.Errorf("open with create and size 0 = %v, want IllegalArgumentException", err)
	}
	if _, err := openJavaDatabase(t, runtime, client, "scores", 8, false); !isGuestException(err, runtimeDataBaseExceptionClass) {
		t.Errorf("open of a missing database without create = %v, want DataBaseException", err)
	}
	if _, err := javaCall(t, runtime, client, runtimeDataBaseAccessMode, javaName(client, "scores")); !isGuestException(err, runtimeDataBaseExceptionClass) {
		t.Errorf("getAccessMode of a missing database = %v, want DataBaseException", err)
	}
	database, err := openJavaDatabase(t, runtime, client, "scores", 8, true)
	if err != nil {
		t.Fatal(err)
	}
	modified := mustLong(must(javaCall(t, runtime, client, runtimeDataBaseLastModified, database)))
	if modified == 0 {
		t.Error("getLastModified of a database just opened is 0")
	}
	insert := func(data string) int32 {
		t.Helper()
		value, err := javaCall(t, runtime, client, runtimeDataBaseInsert, database, jvm.ReferenceValue(jvm.NewByteArray([]byte(data))))
		if err != nil {
			t.Fatal(err)
		}
		return mustInt(value)
	}
	if first, second, third := insert("aaaa"), insert("bbbb"), insert("a record longer than eight bytes"); first != 0 || second != 1 || third != 2 {
		t.Fatalf("ids = %d %d %d, want 0 1 2", first, second, third)
	}
	if record := must(javaCall(t, runtime, client, runtimeDataBaseSelect, database, jvm.IntValue(2))); string(mustBytes(t, record)) != "a record longer than eight bytes" {
		t.Error("a record longer than the record size was not stored whole")
	}
	if record := must(javaCall(t, runtime, client, runtimeDataBaseSelect, database, jvm.IntValue(0))); len(mustBytes(t, record)) != 4 {
		t.Error("selectRecord padded a short record")
	}
	small := jvm.ReferenceValue(jvm.NewByteArray(make([]byte, 2)))
	if _, err := javaCall(t, runtime, client, runtimeDataBaseSelectInto, database, jvm.IntValue(0), small, jvm.IntValue(0)); !isGuestException(err, "java/lang/IllegalArgumentException") {
		t.Errorf("selectRecord into a short buffer = %v, want IllegalArgumentException", err)
	}
	if _, err := javaCall(t, runtime, client, runtimeDataBaseDelete, database, jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := javaCall(t, runtime, client, runtimeDataBaseDelete, database, jvm.IntValue(1)); !isGuestException(err, runtimeDataBaseRecordClass) {
		t.Errorf("deleting a deleted record = %v, want DataBaseRecordException", err)
	}
	if reused := insert("cccc"); reused != 1 {
		t.Errorf("an insert after deleting id 1 took %d, want 1", reused)
	}
	if later := mustLong(must(javaCall(t, runtime, client, runtimeDataBaseLastModified, database))); later < modified {
		t.Errorf("getLastModified went back from %d to %d", modified, later)
	}
	if size := mustInt(must(javaCall(t, runtime, client, runtimeDataBaseRecordSize, database))); size != 8 {
		t.Errorf("getRecordSize = %d, want the creating size 8", size)
	}
	if mode := mustInt(must(javaCall(t, runtime, client, runtimeDataBaseAccessMode, javaName(client, "scores")))); mode != 1 {
		t.Errorf("getAccessMode = %d, want PRIVATE_ACCESS", mode)
	}
	if _, err := javaCall(t, runtime, client, runtimeComponentNoop, database); err != nil {
		t.Fatal(err)
	}
	if total := insert("dddd"); total != 3 {
		t.Errorf("an insert after closeDataBase took %d, want 3: the object stays usable", total)
	}

	// A second session finds the database, its record size and its name.
	client2, second := newTestRuntime(t)
	second.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	list := must(javaCall(t, second, client2, runtimeDataBaseListNames))
	if isNull(list) {
		t.Fatal("listDataBases in a new session is null")
	}
	reopened, err := openJavaDatabase(t, second, client2, "scores", 99, false)
	if err != nil {
		t.Fatal(err)
	}
	if size := mustInt(must(javaCall(t, second, client2, runtimeDataBaseRecordSize, reopened))); size != 8 {
		t.Errorf("getRecordSize in a new session = %d, want 8 from creation", size)
	}
	empty, err := openJavaDatabase(t, second, client2, "empty", 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if sorted := must(javaCall(t, second, client2, runtimeDataBaseSortRecord, empty, jvm.ReferenceValue(nil), jvm.ReferenceValue(nil))); !isNull(sorted) {
		t.Error("sortRecord of a database with no record is not null")
	}
	if _, err := javaCall(t, second, client2, runtimeDataBaseDeleteStore, javaName(client2, "never")); !isGuestException(err, runtimeDataBaseExceptionClass) {
		t.Errorf("deleteDataBase of a missing database = %v, want DataBaseException", err)
	}
}

func mustLong(value jvm.Value) int64 {
	got, _ := value.Int64()
	return got
}

func mustBytes(t *testing.T, value jvm.Value) []byte {
	t.Helper()
	object, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	data, err := jvm.ByteArraySnapshot(object)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func isNull(value jvm.Value) bool {
	object, err := value.Reference()
	return err == nil && object == nil
}

// A backslash is a separator, the 255 bytes are counted on the path, and a
// database name is read the way a file name is.
func TestContractNameReadings(t *testing.T) {
	saves := t.TempDir()
	client, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)

	databaseCall(t, runtime, wipicrt.WIPICFileOpen, `data\save.dat`, 4)
	if got := databaseCall(t, runtime, wipicrt.WIPICFileExists, "data/save.dat"); got != 0 {
		t.Errorf(`C exists(data/save.dat) after open(data\save.dat) = %#x, want 0`, got)
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileExists, "data"); got != 0 {
		t.Errorf("C exists(data), the directory a backslashed name implied = %#x, want 0", got)
	}
	// The Java half keeps its own files; a backslash names the same path there.
	if err := runtime.storeGuestFile("slots/one", []byte("progress")); err != nil {
		t.Fatal(err)
	}
	if !javaBool(javaCall(t, runtime, client, runtimeFileSystemExists, javaName(client, `slots\one`))) {
		t.Error(`Java exists(slots\one) is false for slots/one`)
	}
	if got := int32(databaseCall(t, runtime, wipicrt.WIPICFileOpen, "/"+strings.Repeat("n", wipicrt.MaxWIPIFileName), 4)); got < 0 {
		t.Errorf("a leading slash counted against the 255 bytes: open = %d", got)
	}
	if _, err := javaCall(t, runtime, client, runtimeFileSystemExists, javaName(client, `./`+strings.Repeat("n", wipicrt.MaxWIPIFileName))); err != nil {
		t.Errorf("a leading ./ counted against the 255 bytes: %v", err)
	}

	if _, err := javaCall(t, runtime, client, runtimeOpenDataBase, jvm.ReferenceValue(nil), jvm.IntValue(4), jvm.IntValue(1)); !isGuestException(err, runtimeDataBaseExceptionClass) {
		t.Errorf("openDataBase(null) = %v, want DataBaseException", err)
	}
	if _, err := javaCall(t, runtime, client, runtimeDataBaseAccessMode, jvm.ReferenceValue(nil)); !isGuestException(err, runtimeDataBaseExceptionClass) {
		t.Errorf("getAccessMode(null) = %v, want DataBaseException", err)
	}
	first, err := openJavaDatabase(t, runtime, client, "/scores", 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaCall(t, runtime, client, runtimeDataBaseInsert, first, jvm.ReferenceValue(jvm.NewByteArray([]byte("abcd")))); err != nil {
		t.Fatal(err)
	}
	second, err := openJavaDatabase(t, runtime, client, "scores", 4, false)
	if err != nil {
		t.Fatalf("scores after /scores was made: %v", err)
	}
	if count := mustInt(must(javaCall(t, runtime, client, runtimeDataBaseCount, second))); count != 1 {
		t.Errorf("scores and /scores hold %d records between them as one database, want 1", count)
	}
	if _, err := javaCall(t, runtime, client, runtimeDataBaseDeleteStore, javaName(client, "./scores")); err != nil {
		t.Fatal(err)
	}
	if _, err := openJavaDatabase(t, runtime, client, "scores", 4, false); !isGuestException(err, runtimeDataBaseExceptionClass) {
		t.Errorf("scores after ./scores was deleted = %v, want DataBaseException", err)
	}
}

// A removal an earlier build listed under another spelling still hides the
// database the name stands for.
func TestContractEarlierRemovalSpellingStillHides(t *testing.T) {
	saves := t.TempDir()
	store := wipicrt.NewDirectorySaveStore(saves)
	if err := store.StoreSave("jdb/scores", wipicrt.EncodeSaveRecords([][]byte{[]byte("old")})); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave(javaDatabaseRemovedKey, []byte("/scores")); err != nil {
		t.Fatal(err)
	}
	client, runtime := newTestRuntime(t)
	runtime.client.SaveStore = store
	if _, err := openJavaDatabase(t, runtime, client, "scores", 4, false); !isGuestException(err, runtimeDataBaseExceptionClass) {
		t.Errorf("a database listed as /scores came back as scores: %v", err)
	}
}

// A name the store cannot keep is refused by every Java call, and a C name
// longer than the bound but shorter than the read is M_E_LONGNAME, not a
// string that never ended.
func TestContractUnkeepableNames(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	for _, name := range []string{"..", "a/../b", `a\..\b`, ".removed", ".dirs"} {
		if _, err := javaCall(t, runtime, client, runtimeFileSystemExists, javaName(client, name)); !isGuestException(err, "java/io/IOException") {
			t.Errorf("exists(%q) = %v, want IOException", name, err)
		}
		if _, err := javaCall(t, runtime, client, runtimeFileSystemRemoveAny, javaName(client, name), jvm.IntValue(1)); !isGuestException(err, "java/io/IOException") {
			t.Errorf("remove(%q, flag) = %v, want IOException", name, err)
		}
	}
	if got := databaseCall(t, runtime, wipicrt.WIPICFileOpen, strings.Repeat("n", 600), 4); got != wipicrt.WIPICErrorLongName {
		t.Errorf("open with a 600-byte name = %#x, want M_E_LONGNAME", got)
	}
}
