package lgt

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The file and record store boundary, from the side a title cannot see: what
// reaches the save store, and what one view of a file tells another.

// reviewFiles is a client over a store of its own, driven through the file
// slots the way a Clet drives them.
func reviewFiles(t *testing.T, entries map[string]string) (*Client, *faultStore, *guestFiles) {
	t.Helper()
	store := newFaultStore(t, entries)
	client := fixtureClient(t)
	client.saveStore = store
	return client, store, newGuestFiles(t, client)
}

// openJavaFile opens a File the way a title's constructor call does.
func openJavaFile(t *testing.T, client *Client, name string, mode uint32) uint32 {
	t.Helper()
	object, err := newTestObject(t, client, javaFileClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaFileOpen(client, context.Background(), client.thread, []uint32{object, newTestString(t, client, name), mode, 0}); err != nil {
		t.Fatal(err)
	}
	return object
}

// A seek or a write that would take a file past the cap is refused the way any
// other bad file call is, before anything is allocated for it: a title's
// garbage offset or length must not size a host allocation.
func TestFileSeekAndWriteStopAtTheFileSizeCap(t *testing.T) {
	client, _, files := reviewFiles(t, nil)
	handle := files.open("big.dat", fileOpenReadWrite)
	seek := func(offset, whence uint32) int32 {
		return int32(callSlot(t, client, slotFsSeek, handle, offset, whence))
	}
	if got := seek(maxFileBytes+1, 0); got != wipiError {
		t.Fatalf("a seek past the cap answered %d", got)
	}
	if got := seek(0x7fffffff, 0); got != wipiError {
		t.Fatalf("a seek to 0x7fffffff answered %d", got)
	}
	if got := seek(maxFileBytes, 0); got != maxFileBytes {
		t.Fatalf("a seek to the cap itself answered %d", got)
	}
	if got := seek(1, 1); got != wipiError || client.files[handle].cursor != maxFileBytes {
		t.Fatalf("a relative seek past the cap answered %d and left the cursor at %d", got, client.files[handle].cursor)
	}
	if got := int32(callSlot(t, client, slotFsWrite, handle, files.buffer, 1)); got != wipiError || len(client.files[handle].data) != 0 {
		t.Fatalf("a write past the cap answered %d and left %d bytes", got, len(client.files[handle].data))
	}
	seek(0, 0)
	if got := int32(callSlot(t, client, slotFsWrite, handle, files.buffer, 0xffffffff)); got != wipiError {
		t.Fatalf("a write of 0xffffffff bytes answered %d", got)
	}
	// Inside the cap a write past the end fills the gap with zeros, as before.
	seek(4, 0)
	files.write(handle, "ab")
	if open := client.files[handle]; string(open.data) != "\x00\x00\x00\x00ab" || open.cursor != 6 {
		t.Fatalf("a write inside the cap left %q at %d", open.data, open.cursor)
	}
}

// A cursor a checkpoint record restored far past the end of a file is kept, as
// the record has it, and the first write there is refused rather than sizing a
// host allocation from the record.
func TestARestoredCursorPastTheCapCannotGrowAJavaFile(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "save.dat", 4)
	_, open, err := client.javaFileHandle(object)
	if err != nil {
		t.Fatal(err)
	}
	open.cursor = maxFileBytes
	if written, err := javaFileWriteByte(client, ctx, client.thread, []uint32{object, 'Z'}); err != nil || int32(written) != wipiError {
		t.Fatalf("write(int) past the cap = %d, %v", int32(written), err)
	}
	sink, err := javaFileOpenOutputStream(client, ctx, client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	client.javaRun.sinks[sink] = []byte("MARKER")
	if _, err := javaByteSinkFlush(client, ctx, client.thread, []uint32{sink}); err == nil {
		t.Fatal("a stream drained past the cap was accepted")
	}
	if string(open.data) != "0123" || open.dirty {
		t.Fatalf("the refused writes left %d bytes, dirty=%t", len(open.data), open.dirty)
	}
	open.cursor = 6
	if written, err := javaFileWriteByte(client, ctx, client.thread, []uint32{object, 'Z'}); err != nil || written != 1 || string(open.data) != "0123\x00\x00Z" {
		t.Fatalf("write(int) inside the cap = %d, %v, leaving %q", written, err, open.data)
	}
}

// A file opened to be rewritten is empty from the open on, in the store as
// well, and stays empty when the title closes it without writing: that is how
// a title clears a save slot.
func TestATruncatingOpenEmptiesTheSaveWithoutAWrite(t *testing.T) {
	store := newFaultStore(t, map[string]string{"fs/slot.dat": "OLD SAVE", "fs/java.dat": "OLD SAVE"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	handle := files.open("slot.dat", fileOpenWriteTruncate)
	info, err := client.allocate(12)
	if err != nil {
		t.Fatal(err)
	}
	if callSlot(t, client, slotFsFileAttribute, files.name("slot.dat"), info) != 0 {
		t.Fatal("the file being rewritten is not there")
	}
	if size, _ := client.readWord(info + 8); size != 0 {
		t.Fatalf("the file being rewritten reports %d bytes", size)
	}
	// The open emptied the store already, so a session the Host ends here
	// leaves the slot cleared.
	if got := store.held("fs/slot.dat"); got != "" {
		t.Fatalf("after the truncating open the store holds %q", got)
	}
	client.flushOpenFiles()
	files.close(handle)
	if got := store.held("fs/slot.dat"); got != "" {
		t.Fatalf("after a truncating open and a close the store holds %q", got)
	}
	// Java's truncating mode is the same open.
	object := openJavaFile(t, client, "java.dat", 3)
	if _, err := javaFileClose(client, context.Background(), client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/java.dat"); got != "" {
		t.Fatalf("after File(name, 3).close() the store holds %q", got)
	}
}

// A close the store refuses keeps the write: the handle stays open with it, the
// title is told the close failed, and the ending and a later close try again.
func TestAWriteTheStoreRefusesAtCloseIsKept(t *testing.T) {
	client, store, files := reviewFiles(t, map[string]string{"fs/save.dat": "old"})
	handle := files.open("save.dat", fileOpenReadWrite)
	files.write(handle, "new")
	store.refuse = true
	if got := int32(callSlot(t, client, slotFsClose, handle)); got != wipiError {
		t.Fatalf("a close the store refused answered %d", got)
	}
	if open := client.files[handle]; open == nil || !open.dirty || string(open.data) != "new" {
		t.Fatalf("a close the store refused dropped the write: %+v", open)
	}
	client.flushOpenFiles()
	if open := client.files[handle]; !open.dirty {
		t.Fatal("an ending the store refused marked the write stored")
	}
	store.refuse = false
	client.flushOpenFiles()
	if store.held("fs/save.dat") != "new" || client.files[handle].dirty {
		t.Fatalf("the ending stored %q", store.held("fs/save.dat"))
	}
	files.close(handle)
	if client.files[handle] != nil {
		t.Fatal("a close the store took left the handle open")
	}
	// A rename whose new name the store refuses leaves the old name in place.
	store.refuse = true
	if got := int32(callSlot(t, client, slotFsRename, files.name("save.dat"), files.name("moved.dat"))); got != wipiError {
		t.Fatalf("a rename the store refused answered %d", got)
	}
	store.refuse = false
	if !files.exists("save.dat") || files.exists("moved.dat") {
		t.Fatal("a rename the store refused removed the old name or made the new one")
	}

	// A Java File closed over a refusing store stays open with its write.
	fixture := newJavaThreadFixture(t, store)
	java := fixture.client
	object := openJavaFile(t, java, "java.dat", 4)
	if _, err := javaFileWriteByte(java, context.Background(), java.thread, []uint32{object, 'J'}); err != nil {
		t.Fatal(err)
	}
	store.refuse = true
	if _, err := javaFileClose(java, context.Background(), java.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if _, open, err := java.javaFileHandle(object); err != nil || !open.dirty {
		t.Fatalf("a File close the store refused dropped the write: %v", err)
	}
	store.refuse = false
	if _, err := javaFileClose(java, context.Background(), java.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if store.held("fs/java.dat") != "J" {
		t.Fatalf("the second close stored %q", store.held("fs/java.dat"))
	}
	if _, _, err := java.javaFileHandle(object); err == nil {
		t.Fatal("a close the store took left the File open")
	}
}

// What a title writes in its own destroy entry, into a file it never closes,
// reaches the store when the session closes.
func TestClosingASessionStoresWhatDestroyWrote(t *testing.T) {
	store := newFaultStore(t, nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	handle := files.open("final.dat", fileOpenReadWrite)
	if err := client.core.Memory().Write(files.buffer, []byte("FINAL")); err != nil {
		t.Fatal(err)
	}
	write, err := client.stub(svcCategoryWIPIC, slotFsWrite)
	if err != nil {
		t.Fatal(err)
	}
	client.clet.Destroy = fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		a.literal(0, handle)
		a.literal(1, files.buffer)
		a.literal(2, 5)
		a.literal(4, write)
		a.call(4)
		a.emit(armPopPC)
	})
	if err := fixture.session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/final.dat"); got != "FINAL" {
		t.Fatalf("after the close the store holds %q, want what destroyClet wrote", got)
	}
}

// Bytes a title wrote through a stream opened on a File, and never flushed,
// reach the store when the session ends.
func TestTheEndingDrainsAFileOutputStream(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "stream.dat", 4)
	sink, err := javaFileOpenOutputStream(client, ctx, client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.newJavaByteArray([]byte("MARKER-stream"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaByteSinkWriteAll(client, ctx, client.thread, []uint32{sink, array}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/stream.dat"); got != "MARKER-stream" {
		t.Fatalf("after the close the store holds %q, want what the stream was given", got)
	}
}

// Two DataBase objects on one database are one store of records: what one
// saves, the other reads and keeps, and neither puts an older copy back.
func TestTwoDataBaseObjectsShareTheirRecords(t *testing.T) {
	ctx := context.Background()
	stored := func(t *testing.T, store *faultStore) []string {
		t.Helper()
		data, ok := store.MemorySaveStore.LoadSave("fs/save.db")
		if !ok {
			return nil
		}
		database := &databaseContent{}
		if err := database.decode(data); err != nil {
			t.Fatal(err)
		}
		var records []string
		for index, record := range database.records {
			if !database.deleted[index] {
				records = append(records, string(record))
			}
		}
		return records
	}
	closeDatabase := func(t *testing.T, client *Client, object uint32) {
		t.Helper()
		if _, err := javaCloseDataBase(client, ctx, client.thread, []uint32{object}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("a close after another object saved", func(t *testing.T) {
		client, store, _ := reviewFiles(t, nil)
		settings := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, settings, []byte("settings"))
		progress := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, progress, []byte("progress"))
		closeDatabase(t, client, progress)
		closeDatabase(t, client, settings)
		if got := stored(t, store); !reflect.DeepEqual(got, []string{"settings", "progress"}) {
			t.Fatalf("the store holds %q", got)
		}
	})
	t.Run("an update after another object saved", func(t *testing.T) {
		client, store, _ := reviewFiles(t, nil)
		first := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, first, []byte("one"))
		second := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, second, []byte("two"))
		closeDatabase(t, client, second)
		if count, _ := javaDataBaseRecordCount(client, ctx, client.thread, []uint32{first}); count != 2 {
			t.Fatalf("the first object counts %d records", count)
		}
		array, err := client.newJavaByteArray([]byte("uno"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := javaUpdateRecord(client, ctx, client.thread, []uint32{first, 0, array}); err != nil {
			t.Fatal(err)
		}
		if got := stored(t, store); !reflect.DeepEqual(got, []string{"uno", "two"}) {
			t.Fatalf("the store holds %q", got)
		}
	})
	t.Run("a close after the database was deleted", func(t *testing.T) {
		client, store, _ := reviewFiles(t, nil)
		object := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, object, []byte("old"))
		if _, err := javaDeleteDataBase(client, ctx, client.thread, []uint32{newTestString(t, client, "save")}); err != nil {
			t.Fatal(err)
		}
		attempts := store.attempts
		closeDatabase(t, client, object)
		if _, exists := client.readFile(databaseFileName("save")); exists || store.attempts != attempts {
			t.Fatal("closing an object the title changed nothing through brought a deleted database back")
		}
	})
	t.Run("a close retries a refused store", func(t *testing.T) {
		client, store, _ := reviewFiles(t, nil)
		object := openFixtureDatabase(t, client, "save", 16)
		store.refuse = true
		if _, err := javaInsertRecord(client, ctx, client.thread, []uint32{object, mustByteArray(t, client, "kept")}); err != nil {
			t.Fatal(err)
		}
		store.refuse = false
		closeDatabase(t, client, object)
		if got := stored(t, store); !reflect.DeepEqual(got, []string{"kept"}) {
			t.Fatalf("the close stored %q", got)
		}
	})
}

func mustByteArray(t *testing.T, client *Client, text string) uint32 {
	t.Helper()
	array, err := client.newJavaByteArray([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return array
}

// A start that fails after the title started a thread ends that thread: the
// session is never returned, so nothing would close it.
func TestAFailedStartStopsTheThreadsItStarted(t *testing.T) {
	fixture := newJavaThreadFixture(t, nil)
	run := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.call(a, "yield")
		fixture.loop(a, top)
	})
	fixture.start("Runner", run)
	client := fixture.client
	if len(client.javaRun.workers) != 1 {
		t.Fatalf("the fixture started %d threads", len(client.javaRun.workers))
	}
	worker := client.javaRun.workers[0]
	cause := errors.New("the title stopped")
	failure := startFailure(client, cause)
	if !errors.Is(failure, cause) {
		t.Fatalf("the failure reports %v", failure)
	}
	if !worker.done || !client.javaThreadsStopped || len(client.javaRun.workers) != 0 {
		t.Fatal("a failed start left the title's thread waiting for a slice")
	}
}

// A platform list and the scope itself are not guest files: a title that
// writes a file under one of those names cannot change what the platform
// reads back as its list, or put a file where the scope's directory is.
func TestAGuestFileCannotTakeThePlatformsKeys(t *testing.T) {
	t.Run("the path lists", func(t *testing.T) {
		_, store, files := reviewFiles(t, nil)
		kept := files.open("real.dat", fileOpenReadWrite)
		files.write(kept, "keep")
		files.close(kept)
		for _, name := range []string{"/.removed", ".CREATED", "/.created/inside"} {
			handle := files.open(name, fileOpenReadWrite)
			files.write(handle, "real.dat\nbogus")
			files.close(handle)
		}
		if strings.Contains(store.held("fs/.removed"), "real.dat") || strings.Contains(store.held("fs/.created"), "bogus") {
			t.Fatalf("the guest wrote over the platform's lists: %q, %q", store.held("fs/.removed"), store.held("fs/.created"))
		}
		next := fixtureClient(t)
		next.saveStore = store
		if _, exists := next.readFile("real.dat"); !exists {
			t.Fatal("the next session reads a guest file as its removal list")
		}
		if containsName(next.listDirectory("/"), "bogus") {
			t.Fatal("the next session lists a guest file's contents")
		}
	})
	// A directory store keeps the scope as a directory, and a file put where
	// it has to be leaves every later save with nowhere to go.
	t.Run("the scope itself", func(t *testing.T) {
		root := t.TempDir()
		client := fixtureClient(t)
		client.saveStore = backend.NewDirectorySaveStore(root)
		files := newGuestFiles(t, client)
		for _, name := range []string{"/", "real.dat"} {
			handle := files.open(name, fileOpenReadWrite)
			files.write(handle, "keep")
			files.close(handle)
		}
		data, found, err := backend.ReadSave(backend.NewDirectorySaveStore(root), "fs/real.dat")
		if err != nil || !found || string(data) != "keep" {
			t.Fatalf("after a write of \"/\" the save store holds %q for real.dat (found=%t, err=%v)", data, found, err)
		}
	})
}

// A removal hides the file under every spelling the store gives one key to,
// and only that file: a name that differs only by case is another file.
func TestARemovalHidesTheFileUnderEverySpelling(t *testing.T) {
	client, _, files := reviewFiles(t, map[string]string{"fs/save.dat": "progress", "fs/SAVE.DAT": "another file"})
	if callSlot(t, client, slotFsRemove, files.name("./save.dat")) != 0 {
		t.Fatal("the remove was refused")
	}
	spellings := []string{"save.dat", "/save.dat", "//save.dat", "./save.dat"}
	for _, spelling := range spellings {
		if files.exists(spelling) {
			t.Fatalf("%q exists after the file was removed as ./save.dat", spelling)
		}
	}
	if !files.exists("SAVE.DAT") {
		t.Fatal("removing ./save.dat removed SAVE.DAT")
	}
	handle := files.open("//save.dat", fileOpenReadWrite)
	files.write(handle, "new game")
	files.close(handle)
	for _, spelling := range spellings {
		if !files.exists(spelling) {
			t.Fatalf("%q is missing after the file was written as //save.dat", spelling)
		}
	}
}

// A removal list an earlier build wrote keeps meaning what it meant. An entry
// under a spelling other than the plain one hid that spelling only, and the
// file may have been written since under the plain one, which the earlier
// build did not clear; so such an entry still hides that spelling only. A write
// under any spelling clears it.
func TestARemovalListFromAnEarlierBuildKeepsItsMeaning(t *testing.T) {
	_, store, files := reviewFiles(t, map[string]string{
		"fs/save.dat": "written after the removal",
		"fs/gone.dat": "removed",
		"fs/.removed": "./save.dat\ngone.dat",
	})
	if files.exists("gone.dat") || files.exists("./gone.dat") {
		t.Fatal("a plain entry no longer hides its file")
	}
	if !files.exists("save.dat") || files.exists("./save.dat") {
		t.Fatal("an entry under another spelling changed its meaning")
	}
	handle := files.open("save.dat", fileOpenReadWrite)
	files.write(handle, "again")
	files.close(handle)
	if !files.exists("./save.dat") || strings.Contains(store.held("fs/.removed"), "save.dat") {
		t.Fatalf("a write left the earlier entry behind: %q", store.held("fs/.removed"))
	}
}

// A write waiting in an open handle is what a second open, a stat and a rename
// of the same path see, and a rename takes the handle with the file.
func TestAnUnclosedWriteIsWhatTheNextOpenSees(t *testing.T) {
	client, store, files := reviewFiles(t, map[string]string{"fs/save.dat": "old"})
	writer := files.open("save.dat", fileOpenReadWrite)
	files.write(writer, "newer progress")
	reader := files.open("./save.dat", fileOpenReadOnly)
	if got := files.read(reader, 32); got != "newer progress" {
		t.Fatalf("a second open reads %q", got)
	}
	files.close(reader)
	info, err := client.allocate(12)
	if err != nil {
		t.Fatal(err)
	}
	if callSlot(t, client, slotFsFileAttribute, files.name("save.dat"), info) != 0 {
		t.Fatal("the stat was refused")
	}
	if size, _ := client.readWord(info + 8); size != uint32(len("newer progress")) {
		t.Fatalf("the stat reports %d bytes", size)
	}
	if callSlot(t, client, slotFsRename, files.name("save.dat"), files.name("moved.dat")) != 0 {
		t.Fatal("the rename was refused")
	}
	files.write(writer, "!")
	files.close(writer)
	if store.held("fs/moved.dat") != "newer progress!" || files.exists("save.dat") {
		t.Fatalf("after the rename and the close the store holds %q, and the old name exists: %t",
			store.held("fs/moved.dat"), files.exists("save.dat"))
	}
}
