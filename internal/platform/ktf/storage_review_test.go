package ktf

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// These hold the fixes a review of the storage contract made here
// (docs/history/ktf.md#ktf-storage-review-2026-10-22).

// A title that keeps a file its archive ships as a database record was
// charged the whole file: the packaged copy was looked for only in a
// database's own packaged form, which a plain file is not. One title keeps
// 1.4 MB so, read its space as gone, and refused to start. The archive's file
// of the same name is the packaged copy, for the Java table and the C record
// table alike, and what a title adds beyond it is still counted.
func TestSpaceDoesNotChargeAShippedFileKeptAsADatabase(t *testing.T) {
	shipped := bytes.Repeat([]byte{7}, 600<<10)
	client, runtime := newTestRuntime(t)
	store := NewDirectorySaveStore(t.TempDir())
	runtime.client.saveStore = store
	client.AttachFilesystem(map[string][]byte{"pack.dat": shipped, "level": shipped})
	// Saved by an earlier session, as the title left them.
	for key, records := range map[string][][]byte{
		"jdb/pack.dat": {shipped},
		"rdb/level":    {shipped, make([]byte, 1000)},
	} {
		if err := store.StoreSave(key, encodeSaveRecords(records)); err != nil {
			t.Fatal(err)
		}
	}
	want := uint32(storageBudget - 1000)
	for name, got := range map[string]uint32{
		"MC_fsAvailable":            databaseCall(t, runtime, wipicFileAvailable, ""),
		"FileSystem.available":      uint32(mustInt(must(javaCall(t, runtime, client, runtimeFileSystemAvailable)))),
		"DataBase.getSizeAvailable": uint32(mustInt(must(javaCall(t, runtime, client, runtimeDataBaseSizeAvailable)))),
	} {
		if got != want {
			t.Errorf("%s = %d, want %d: only what the title added to the shipped bytes is used", name, got, want)
		}
	}
}

// Writing down when a database was opened or written read the clock the
// guest reads, and every read there counts as the guest waiting on the time,
// which changes how the step budget charges the call it came from. A title
// that opens and writes a database at start-up ran on a different schedule
// than before, and the same keys led it somewhere else.
func TestDatabaseBookkeepingDoesNotReadTheGuestClock(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(t.TempDir())
	before := client.guestClockReads
	database, err := openJavaDatabase(t, runtime, client, "scores", 8, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaCall(t, runtime, client, runtimeDataBaseInsert, database, jvm.ReferenceValue(jvm.NewByteArray([]byte("progress")))); err != nil {
		t.Fatal(err)
	}
	modified := mustLong(must(javaCall(t, runtime, client, runtimeDataBaseLastModified, database)))
	if modified == 0 {
		t.Error("getLastModified of a database just written is 0")
	}
	if reads := client.guestClockReads - before; reads != 0 {
		t.Fatalf("opening and writing a database counted %d guest clock reads, want none", reads)
	}
}

// toCString converts text, not a file name. Applying the name rules to it
// turned a backslash into a slash and threw IOException, which toCString does
// not declare, for 256 bytes of text or a name the store keeps for itself.
// LGT, SKT and earlier builds here convert the text as it is.
func TestToCStringConvertsTextAsItIs(t *testing.T) {
	client, runtime := newTestRuntime(t)
	for _, text := range []string{`C:\dir\x`, strings.Repeat("n", 300), ".dirs", "a/../b"} {
		value, err := javaCall(t, runtime, client, runtimeFileSystemCString, javaName(client, text))
		if err != nil {
			t.Errorf("toCString(%.20q) = %v", text, err)
			continue
		}
		if got := mustBytes(t, value); string(got) != text+"\x00" {
			t.Errorf("toCString(%.20q) = %.24q, want the text and a NUL", text, got)
		}
	}
	if _, err := javaCall(t, runtime, client, runtimeFileSystemCString, jvm.ReferenceValue(nil)); !isGuestException(err, "java/lang/NullPointerException") {
		t.Errorf("toCString(null) = %v, want NullPointerException", err)
	}
}

// Opening a database writes its record size down, and a store that refused
// that write failed the open: the save was there, and the title was told it
// was not. The open stands, and the object keeps the size.
func TestOpeningADatabaseSurvivesARefusedSizeTable(t *testing.T) {
	store := &refusableSaveStore{saves: map[string][]byte{"jdb/scores": encodeSaveRecords([][]byte{[]byte("progress")})}}
	client, runtime := newTestRuntime(t)
	runtime.client.saveStore = store
	store.refuse = errors.New("the store is read-only")
	database, err := openJavaDatabase(t, runtime, client, "scores", 8, false)
	if err != nil {
		t.Fatalf("open of a saved database over a store that refuses writes = %v", err)
	}
	if size := mustInt(must(javaCall(t, runtime, client, runtimeDataBaseRecordSize, database))); size != 8 {
		t.Errorf("getRecordSize = %d, want the open's 8", size)
	}
	if record := must(javaCall(t, runtime, client, runtimeDataBaseSelect, database, jvm.IntValue(0))); string(mustBytes(t, record)) != "progress" {
		t.Errorf("selectRecord(0) = %q, want the saved record", mustBytes(t, record))
	}
}

// The record table read a name only to 512 bytes and stopped the run past
// that. Every C storage call reads a name to 4096 bytes (docs/wipi-storage.md),
// and a name that long is no database's, so it is refused as one.
func TestRecordTableReadsNamesAsFarAsTheFileTable(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.saveStore = NewDirectorySaveStore(t.TempDir())
	long := strings.Repeat("n", 600)
	if got := openRecordDatabase(t, runtime, long, 4, 1); got != wipicErrorInvalid {
		t.Errorf("MC_dbOpenDataBase(600 bytes) = %#x, want M_E_INVALID", got)
	}
	const address = platformDataBase + 0xa000
	if err := runtime.client.core.Memory().Write(address, append([]byte(long), 0)); err != nil {
		t.Fatal(err)
	}
	for slot, want := range map[uint32]uint32{
		wipicRecordDatabaseDelete:     wipicErrorInvalid,
		wipicRecordDatabaseAccessMode: wipicErrorNotFound,
	} {
		thread := armcore.NewThread(armcore.Context{})
		if err := thread.SetRegister(0, address); err != nil {
			t.Fatal(err)
		}
		got, err := runtime.handleWIPICRecordDatabaseCall(thread, slot)
		if err != nil {
			t.Errorf("record slot %d with a 600-byte name stopped the run: %v", slot, err)
			continue
		}
		if got != want {
			t.Errorf("record slot %d with a 600-byte name = %#x, want %#x", slot, got, want)
		}
	}
}
