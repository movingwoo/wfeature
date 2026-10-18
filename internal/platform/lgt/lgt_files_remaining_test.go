package lgt

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The limits the 2026-10-16 file fixes left behind: the database index in the
// guest's namespace, a refused close nobody was told about, objects the
// collector reclaims while they hold a write, path lists that named what has no
// save, a session with no store that forgot its files, a save that differs by
// case from a packaged file, and a stream that kept a copy of a closed File.

// writeFile stores a guest path, leaving a refusal to the logger: how a test
// seeds a save the way a title's own write would have made it.
func (client *Client) writeFile(name string, data []byte) {
	_ = client.storeFile(name, data)
}

// thrownClass answers the class of the exception a platform call threw with
// nothing of the title's to catch it, and "" for any other outcome.
func thrownClass(err error) string {
	var throw *javaUncaughtThrow
	if errors.As(err, &throw) {
		return throw.Class
	}
	return ""
}

// closeJavaSink is OutputStream.close() on a stream, through the slot a title
// calls it by.
func closeJavaSink(client *Client, sink uint32) error {
	_, err := javaBakedVirtualSlots[javaByteSinkClass][14].Method.Implementat(client, context.Background(), client.thread, []uint32{sink})
	return err
}

// The index of database names is the platform's: a title's file of the same
// name, under any spelling or case, or with anything beneath it, does not
// reach it, a database cannot take its name, and a listing does not show it,
// including from a created list an earlier build wrote with it in.
func TestTheDatabaseIndexIsNotAGuestFile(t *testing.T) {
	client, store, files := reviewFiles(t, nil)
	openFixtureDatabase(t, client, "save", 16)
	indexKey := fileSaveScope + databaseIndexName
	if got := store.held(indexKey); got != "save" {
		t.Fatalf("the index holds %q after the database was made", got)
	}
	for _, name := range []string{"__databases__.db", "/./__DATABASES__.DB", "__databases__.db/inside"} {
		handle := files.open(name, fileOpenReadWrite)
		files.write(handle, "bogus\nnames")
		files.close(handle)
		if got := store.held(indexKey); got != "save" {
			t.Fatalf("a guest file %q wrote the index: %q", name, got)
		}
	}
	if names := client.databaseNames(); !reflect.DeepEqual(names, []string{"save"}) {
		t.Fatalf("listDataBases answers %q", names)
	}
	if listed := client.listDirectory("/"); containsExact(listed, databaseIndexName) {
		t.Fatalf("the root lists the index: %q", listed)
	}
	_, err := javaOpenDataBase(client, nil, nil, []uint32{newTestString(t, client, "__Databases__"), 16, 1})
	if thrownClass(err) != javaDataBaseExceptionClass {
		t.Fatalf("a database named after the index opened: %v", err)
	}
	if got := store.held(indexKey); got != "save" {
		t.Fatalf("a database named after the index wrote it: %q", got)
	}
	for _, list := range []string{"__databases__.db\nsave.db", fileListMarker + "\n__databases__.db\nsave.db"} {
		store.behind(t, fileCreatedKey, list)
		next := fixtureClient(t)
		next.saveStore = store
		if listed := next.listDirectory("/"); containsExact(listed, databaseIndexName) || !containsExact(listed, "save.db") {
			t.Fatalf("over the created list %q the root lists %q", list, listed)
		}
	}
}

// File.close declares IOException for a file it cannot close, and a stream's
// flush and close declare it too: a store that refuses is thrown at the title,
// while the write waits in the File, which stays open with its streams bound.
// Closing a File that is already closed is nothing, as the specification says.
func TestARefusedFileCloseIsThrownAndTheFileStaysOpen(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "save.dat", 4)
	sink := openJavaOutputStream(t, client, object)
	writeJavaSink(t, client, sink, "progress")
	store.refuse = true
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("a close the store refused answered %v", err)
	}
	if _, open, err := client.javaFileHandle(object); err != nil || !open.dirty || string(open.data) != "progress" {
		t.Fatalf("a refused close did not keep the File open with its write: %v", err)
	}
	writeJavaSink(t, client, sink, "+more")
	if size := javaFileSizeOf(t, client, object); size != uint32(len("progress+more")) {
		t.Fatalf("after a refused close the stream wrote a File of %d bytes", size)
	}
	if _, err := javaByteSinkFlush(client, ctx, client.thread, []uint32{sink}); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("a flush the store refused answered %v", err)
	}
	if err := closeJavaSink(client, sink); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("a stream close the store refused answered %v", err)
	}
	store.refuse = false
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/save.dat"); got != "progress+more" {
		t.Fatalf("the close the store took stored %q", got)
	}
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatalf("closing a closed File answered %v", err)
	}
}

// An object the collector reclaims while it holds a write the store refused
// loses nothing. A File's handle stays open with its buffer, and a DataBase's
// records stay waiting, whether the title closed the object or not: the
// ending, a quick step and the next open of the database have them.
func TestACollectedObjectKeepsAWriteTheStoreRefused(t *testing.T) {
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
	for _, closed := range []bool{false, true} {
		name := "left open"
		if closed {
			name = "closed by the title"
		}
		t.Run("a database "+name, func(t *testing.T) {
			client, store, _ := reviewFiles(t, nil)
			object := openFixtureDatabase(t, client, "save", 16)
			store.refuse = true
			insertFixtureRecord(t, client, object, []byte("kept"))
			if closed {
				if _, err := javaCloseDataBase(client, ctx, client.thread, []uint32{object}); err != nil {
					t.Fatal(err)
				}
			}
			client.releaseJavaPins(0)
			collect(t, client)
			collect(t, client)
			if _, alive := client.javaRun.databases[object]; alive {
				t.Fatal("the fixture's database object was not collected")
			}
			reopened := openFixtureDatabase(t, client, "save", 16)
			if count, err := javaDataBaseRecordCount(client, ctx, client.thread, []uint32{reopened}); err != nil || count != 1 {
				t.Fatalf("the database opened again counts %d records, %v", count, err)
			}
			if _, err := javaCloseDataBase(client, ctx, client.thread, []uint32{reopened}); err != nil {
				t.Fatal(err)
			}
			client.releaseJavaPins(0)
			collect(t, client)
			collect(t, client)
			store.refuse = false
			client.flushOpenFiles()
			if got := stored(t, store); !reflect.DeepEqual(got, []string{"kept"}) {
				t.Fatalf("the ending stored %q", got)
			}
		})
	}
	t.Run("a file", func(t *testing.T) {
		client, store, _ := reviewFiles(t, nil)
		runtime := client.javaRuntimeState()
		handle := uint32(client.openFile("orphan.sav", fileOpenReadWrite))
		file := newCollectableObject(t, client, javaFileClass)
		runtime.files[file] = handle
		client.files[handle].data, client.files[handle].dirty = []byte("progress"), true
		store.refuse = true
		collect(t, client)
		collect(t, client)
		if _, alive := runtime.files[file]; alive {
			t.Fatal("the fixture's File object was not collected")
		}
		if open := client.files[handle]; open == nil || !open.dirty {
			t.Fatal("a collected File whose store was refused dropped its write")
		}
		store.refuse = false
		client.flushOpenFiles()
		if got := store.held("fs/orphan.sav"); got != "progress" {
			t.Fatalf("the ending stored %q", got)
		}
	})
}

// The two path lists name only paths that have a save: a name the store has
// no key for leaves them as they are, and an entry of that kind a list holds
// is read as nothing.
func TestPathListsHoldOnlyNamesWithASave(t *testing.T) {
	client, store, files := reviewFiles(t, nil)
	for _, name := range []string{"/.created", "../outside.dat", "/.removed/inside"} {
		handle := files.open(name, fileOpenReadWrite)
		files.write(handle, "x")
		files.close(handle)
		callSlot(t, client, slotFsRemove, files.name(name))
	}
	if len(client.createdFiles()) != 0 || len(client.removedFiles()) != 0 {
		t.Fatalf("the lists hold %v and %v", client.created, client.removed)
	}
	for _, key := range []string{fileCreatedKey, fileRemovedKey} {
		if got := store.held(key); got != "<absent>" && got != "" {
			t.Fatalf("%s holds %q", key, got)
		}
	}
	store.behind(t, fileCreatedKey, ".created\n../x\n"+fileListMarker+"\n.Removed/y\nkept.dat")
	next := fixtureClient(t)
	next.saveStore = store
	if listed := next.listDirectory("/"); !reflect.DeepEqual(listed, []string{"data", "kept.dat"}) {
		t.Fatalf("the root lists %q", listed)
	}
}

// A session with no store keeps what its title wrote for as long as it runs:
// a file closed is still there to open, stat and list, a removal still hides
// it, and a database closed is still there to open again and to list. Nothing
// leaves the session. An authentication adapter with no store beneath it is
// the same session.
func TestASessionWithoutAStoreKeepsWhatItWrote(t *testing.T) {
	t.Run("no store", func(t *testing.T) {
		storelessSessionKeepsWhatItWrote(t, fixtureClient(t))
	})
	t.Run("an adapter over no store", func(t *testing.T) {
		client := fixtureClient(t)
		client.saveStore = newAuthenticationOptionStore(nil, client.archive)
		storelessSessionKeepsWhatItWrote(t, client)
	})
}

func storelessSessionKeepsWhatItWrote(t *testing.T, client *Client) {
	ctx := context.Background()
	files := newGuestFiles(t, client)
	handle := files.open("save.dat", fileOpenReadWrite)
	files.write(handle, "progress")
	files.close(handle)
	if size, found := files.stat("save.dat"); !found || size != uint32(len("progress")) {
		t.Fatalf("the closed file stats as %d bytes, found %t", size, found)
	}
	reader := files.open("save.dat", fileOpenReadOnly)
	if got := files.read(reader, 16); got != "progress" {
		t.Fatalf("the closed file reads %q", got)
	}
	files.close(reader)
	if listed := client.listDirectory("/"); !containsExact(listed, "save.dat") {
		t.Fatalf("the root lists %q", listed)
	}
	object := openFixtureDatabase(t, client, "slots", 16)
	insertFixtureRecord(t, client, object, []byte("one"))
	if _, err := javaCloseDataBase(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	again, err := javaOpenDataBase(client, nil, nil, []uint32{newTestString(t, client, "slots"), 16, 0})
	if err != nil {
		t.Fatalf("the closed database would not open again: %v", err)
	}
	if count, err := javaDataBaseRecordCount(client, ctx, client.thread, []uint32{again}); err != nil || count != 1 {
		t.Fatalf("the database opened again counts %d records, %v", count, err)
	}
	if names := client.databaseNames(); !reflect.DeepEqual(names, []string{"slots"}) {
		t.Fatalf("listDataBases answers %q", names)
	}
	callSlot(t, client, slotFsRemove, files.name("save.dat"))
	if files.exists("save.dat") {
		t.Fatal("a removed file is still there")
	}
}

// A save whose name differs only by case from a packaged file is a file of its
// own, and the listing shows it beside the packaged one, in this session and
// the next. The packaged file rewritten under its own spelling is listed once.
func TestASaveThatDiffersByCaseFromAPackagedFileIsListed(t *testing.T) {
	client, store, files := reviewFiles(t, nil)
	for _, name := range []string{"data/HELLO.TXT", "data/hello.txt"} {
		handle := files.open(name, fileOpenWriteTruncate)
		files.write(handle, "mine")
		files.close(handle)
	}
	want := []string{"HELLO.TXT", "hello.txt", "other.txt"}
	if listed := client.listDirectory("data"); !reflect.DeepEqual(listed, want) {
		t.Fatalf("data lists %q", listed)
	}
	if strings.Contains(store.held(fileCreatedKey), "data/hello.txt") {
		t.Fatalf("the created list records the packaged file: %q", store.held(fileCreatedKey))
	}
	next := fixtureClient(t)
	next.saveStore = store
	if listed := next.listDirectory("data"); !reflect.DeepEqual(listed, want) {
		t.Fatalf("the next session's data lists %q", listed)
	}
}

// A stream opened on a File holds the file open after the File closes, as a
// connection's streams do: an input stream reads the file as it is now, an
// output stream writes it and its flush stores it, and the stream's own close
// lets the file go. A quick save and a load keep the stream on the file.
func TestAStreamHoldsItsFileOpenAfterTheFileCloses(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123456789"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "save.dat", 4)
	stream, err := javaFileOpenInputStream(client, ctx, client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	if got := readJavaStreamText(t, client, stream, 2); got != "01" {
		t.Fatalf("the stream starts with %q", got)
	}
	sink := openJavaOutputStream(t, client, object)
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	files := newGuestFiles(t, client)
	writer := files.open("save.dat", fileOpenReadWrite)
	files.seek(writer, 4)
	files.write(writer, "AB")
	if got := readJavaStreamText(t, client, stream, 4); got != "23AB" {
		t.Fatalf("after its File closed the stream reads %q", got)
	}
	writeJavaSink(t, client, sink, "XY")
	if size, found := files.stat("save.dat"); !found || size != 10 {
		t.Fatalf("after the stream's write the path stats as %d bytes", size)
	}
	reader := files.open("save.dat", fileOpenReadOnly)
	if got := files.read(reader, 16); got != "01XYAB6789" {
		t.Fatalf("after its File closed the stream wrote %q", got)
	}
	files.close(reader)
	if _, err := javaByteSinkFlush(client, ctx, client.thread, []uint32{sink}); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/save.dat"); got != "01XYAB6789" {
		t.Fatalf("the stream's flush stored %q", got)
	}
	files.close(writer)

	fixture.settle()
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	restored := mustCommitFixture(t, fixture.archive, checkpoint, fixture.session, store)
	copied := restored.client
	copiedFiles := newGuestFiles(t, copied)
	again := copiedFiles.open("save.dat", fileOpenReadWrite)
	copiedFiles.seek(again, 6)
	copiedFiles.write(again, "Z")
	if got := readJavaStreamText(t, copied, stream, 16); got != "Z789" {
		t.Fatalf("after a load the stream reads %q", got)
	}
	if _, err := javaStreamClose(copied, ctx, copied.thread, []uint32{stream}); err != nil {
		t.Fatal(err)
	}
	if err := closeJavaSink(copied, sink); err != nil {
		t.Fatal(err)
	}
	if len(copied.files) != 1 || len(copied.javaRun.files) != 0 {
		t.Fatalf("the streams' close left %d handles and %d bindings", len(copied.files), len(copied.javaRun.files))
	}
	if got := store.held("fs/save.dat"); got != "01XYABZ789" {
		t.Fatalf("the last stream's close stored %q", got)
	}
}

// A record an earlier build took can hold a stream whose File was closed with
// nothing holding the file open: that build left the stream a copy. It still
// loads, the window taken from the file as the store has it, and it reads that
// window, as the load gave it.
func TestAStreamAnEarlierBuildLeftWithoutItsFileStillLoads(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123456789"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "save.dat", 4)
	stream, err := javaFileOpenInputStream(client, ctx, client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	if got := readJavaStreamText(t, client, stream, 2); got != "01" {
		t.Fatalf("the stream starts with %q", got)
	}
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	// What an earlier build left: the stream bound to nothing and the file
	// closed under it.
	runtime := client.javaRun
	handle := runtime.files[stream]
	delete(runtime.files, stream)
	delete(runtime.streamFiles, stream)
	delete(client.files, handle)
	fixture.settle()
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	store.behind(t, "fs/save.dat", "abcdefghij")
	restored := mustCommitFixture(t, fixture.archive, checkpoint, fixture.session, store)
	copied := restored.client
	if len(copied.files) != 0 || len(copied.javaRun.files) != 0 {
		t.Fatalf("the load opened %d handles and %d bindings", len(copied.files), len(copied.javaRun.files))
	}
	if got := readJavaStreamText(t, copied, stream, 3); got != "cde" {
		t.Fatalf("after the load the stream reads %q", got)
	}
}
