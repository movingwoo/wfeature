package lgt

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The save rule every platform's file layer keeps, from the side of the store:
//
//  1. A write or a truncation is what every later read of the path in the
//     session sees, through any handle, a new open, an existence check, a
//     size and a stream.
//  2. What a title wrote reaches the store no later than the title's flush or
//     close of that handle, and in any case when the session ends.
//  3. A truncating open empties the file in the store at the open.
//  4. A removed file is absent to every later read, and stays absent in later
//     sessions through the removal list, until the path is written again.
//  5. Names that differ only by case are different files.
//  6. Nothing else writes saves.

// stat answers the size MC_fsFileAttribute reports for a path, and whether it
// found one.
func (files *guestFiles) stat(name string) (uint32, bool) {
	files.t.Helper()
	info, err := files.client.allocate(12)
	if err != nil {
		files.t.Fatal(err)
	}
	if callSlot(files.t, files.client, slotFsFileAttribute, files.name(name), info) != 0 {
		return 0, false
	}
	size, err := files.client.readWord(info + 8)
	if err != nil {
		files.t.Fatal(err)
	}
	return size, true
}

func (files *guestFiles) seek(handle, offset uint32) {
	files.t.Helper()
	if moved := int32(callSlot(files.t, files.client, slotFsSeek, handle, offset, 0)); moved != int32(offset) {
		files.t.Fatalf("seek to %d answered %d", offset, moved)
	}
}

// javaFileSizeOf is File.sizeOf() on an object.
func javaFileSizeOf(t *testing.T, client *Client, object uint32) uint32 {
	t.Helper()
	size, err := javaFileSize(client, context.Background(), client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	return size
}

// openJavaOutputStream is File.openOutputStream() on an object.
func openJavaOutputStream(t *testing.T, client *Client, object uint32) uint32 {
	t.Helper()
	sink, err := javaFileOpenOutputStream(client, context.Background(), client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	return sink
}

// writeJavaSink is OutputStream.write(byte[]) on a stream.
func writeJavaSink(t *testing.T, client *Client, sink uint32, text string) {
	t.Helper()
	if _, err := javaByteSinkWriteAll(client, context.Background(), client.thread, []uint32{sink, mustByteArray(t, client, text)}); err != nil {
		t.Fatal(err)
	}
}

// Rule 3. The open itself empties the stored file: a session cut short after
// a truncating open, or a title that opens a slot that way to clear it, finds
// the file empty in the store, through the C table and through Java's mode 3.
func TestATruncatingOpenEmptiesTheStoreAtTheOpen(t *testing.T) {
	store := newFaultStore(t, map[string]string{"fs/slot.dat": "OLD SAVE", "fs/java.dat": "OLD SAVE"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	handle := files.open("slot.dat", fileOpenWriteTruncate)
	if got := store.held("fs/slot.dat"); got != "" {
		t.Fatalf("after MC_fsOpen with the truncating flag the store holds %q", got)
	}
	if size, found := files.stat("slot.dat"); !found || size != 0 {
		t.Fatalf("the emptied file stats as %d bytes, found %t", size, found)
	}
	files.close(handle)
	if got := store.held("fs/slot.dat"); got != "" {
		t.Fatalf("after the close the store holds %q", got)
	}
	openJavaFile(t, client, "java.dat", 3)
	if got := store.held("fs/java.dat"); got != "" {
		t.Fatalf("after new File(name, 3) the store holds %q", got)
	}
}

// Rule 3, with a store that refuses: the emptied file is a write waiting in
// the session, which every read of the path sees, and which the ending gives
// the store again.
func TestATruncationTheStoreRefusedIsAWriteThatWaits(t *testing.T) {
	client, store, files := fileFixture(t, map[string]string{"fs/slot.dat": "OLD SAVE"})
	store.refuse = true
	handle := files.open("slot.dat", fileOpenWriteTruncate)
	store.refuse = false
	if got := store.held("fs/slot.dat"); got != "OLD SAVE" {
		t.Fatalf("a refused truncation left %q in the store", got)
	}
	if size, found := files.stat("slot.dat"); !found || size != 0 {
		t.Fatalf("the file being emptied stats as %d bytes, found %t", size, found)
	}
	client.flushOpenFiles()
	if got := store.held("fs/slot.dat"); got != "" {
		t.Fatalf("the ending left %q in the store for a truncation the store had refused", got)
	}
	files.close(handle)
}

// Rules 1 and 3. A truncation is what a handle opened before it reads next,
// and a File opened before it reports as its size.
func TestATruncationIsWhatAnOlderHandleReads(t *testing.T) {
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123456789"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	older := files.open("save.dat", fileOpenReadWrite)
	object := openJavaFile(t, client, "save.dat", 4)
	files.close(files.open("save.dat", fileOpenWriteTruncate))
	if got := files.read(older, 10); got != "" {
		t.Fatalf("a handle opened before the truncation reads %q", got)
	}
	if size := javaFileSizeOf(t, client, object); size != 0 {
		t.Fatalf("a File opened before the truncation is %d bytes", size)
	}
}

// Rule 3, across a quick save and a quick load. The record still says the
// handle was opened to be rewritten and how much it had written, and a load
// gives it at most that much of the file as the store has it then. A record
// an earlier build took while the store still held the old bytes loads too:
// the load writes nothing, and the handle holds nothing.
func TestAQuickStepOverATruncatingOpenFindsTheFileEmptied(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, map[string]string{"fs/save.dat": "the earlier save"})
	source := checkpointFixtureSession(t, archive, store)
	files := newGuestFiles(t, source.client)
	handle := files.open("save.dat", fileOpenWriteTruncate)
	checkpoint := roundTripCheckpoint(t, archive, source)
	if got := store.held("fs/save.dat"); got != "" {
		t.Fatalf("at the quick save the store holds %q", got)
	}
	want := []fileState{{Handle: handle, Name: []byte("save.dat"), Writable: true, Truncated: true}}
	if saved := decodeRuntime(t, checkpoint); !reflect.DeepEqual(saved.Client.Files, want) {
		t.Fatalf("the record holds the files %+v, want %+v", saved.Client.Files, want)
	}
	restored := mustCommitFixture(t, archive, checkpoint, source, store)
	if open := restored.client.files[handle]; open == nil || !open.truncated || len(open.data) != 0 || open.dirty {
		t.Fatalf("the handle came back as %+v", open)
	}
	newGuestFiles(t, restored.client).close(handle)
	if got := store.held("fs/save.dat"); got != "" {
		t.Fatalf("closing the restored handle left %q", got)
	}

	// The same record over a store that still has the old bytes, as one an
	// earlier build took before the title wrote through the handle.
	store.behind(t, "fs/save.dat", "the earlier save")
	before, attempts := store.snapshot(t), store.attempts
	again := mustCommitFixture(t, archive, checkpoint, restored, store)
	if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
		t.Fatalf("the load wrote to the store: %+v", after)
	}
	if open := again.client.files[handle]; open == nil || len(open.data) != 0 || open.dirty {
		t.Fatalf("the handle an earlier build recorded came back as %+v", open)
	}
}

// Rule 5. Two names that differ only by case are two files: a removal of one
// leaves the other, a write waiting on one is not what an open of the other
// reads, a listing shows both, and a quick save stores both.
func TestNamesThatDifferOnlyByCaseAreTwoFiles(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, nil)
	session := checkpointFixtureSession(t, archive, store)
	client := session.client
	files := newGuestFiles(t, client)
	for name, text := range map[string]string{"save.dat": "lower", "SAVE.DAT": "UPPER", "Saves/Slot1": "slot"} {
		handle := files.open(name, fileOpenReadWrite)
		files.write(handle, text)
		files.close(handle)
	}
	if listed := client.listDirectory("/"); !containsExact(listed, "save.dat") || !containsExact(listed, "SAVE.DAT") {
		t.Fatalf("the root lists %q", listed)
	}
	if listed := client.listDirectory("Saves"); !reflect.DeepEqual(listed, []string{"Slot1"}) {
		t.Fatalf("Saves lists %q, want the name as the title wrote it", listed)
	}
	if listed := client.listDirectory("saves"); len(listed) != 0 {
		t.Fatalf("saves lists %q, want nothing: it is another directory", listed)
	}
	upper := files.open("SAVE.DAT", fileOpenReadWrite)
	files.write(upper, "WAITING")
	lower := files.open("save.dat", fileOpenReadOnly)
	if got := files.read(lower, 16); got != "lower" {
		t.Fatalf("save.dat reads %q while a write waits on SAVE.DAT", got)
	}
	files.close(lower)
	other := files.open("save.dat", fileOpenReadWrite)
	files.write(other, "LOWER")
	if _, err := session.CaptureCheckpoint(context.Background()); err != nil {
		t.Fatalf("a quick save with a write waiting on each of two files: %v", err)
	}
	if store.held("fs/save.dat") != "LOWER" || store.held("fs/SAVE.DAT") != "WAITING" {
		t.Fatalf("the quick save stored %q and %q", store.held("fs/save.dat"), store.held("fs/SAVE.DAT"))
	}
	files.close(upper)
	files.close(other)
	callSlot(t, client, slotFsRemove, files.name("save.dat"))
	if files.exists("save.dat") || !files.exists("SAVE.DAT") {
		t.Fatal("removing save.dat did not remove it alone")
	}
	// The next session reads the list the same way.
	next := fixtureClient(t)
	next.saveStore = store
	nextFiles := newGuestFiles(t, next)
	if nextFiles.exists("save.dat") || !nextFiles.exists("SAVE.DAT") {
		t.Fatalf("the next session reads the removal list %q as something else", store.held(fileRemovedKey))
	}
}

func containsExact(names []string, name string) bool {
	for _, entry := range names {
		if entry == name {
			return true
		}
	}
	return false
}

// Rule 5 against the lists a released build wrote. Those builds folded every
// entry to lower case, so an entry there hid the file under any case, and a
// write of any case brought it back; a listing showed the folded name. They
// keep meaning that. What this build adds to such a list keeps its case.
func TestPathListsAReleasedBuildWroteKeepTheirMeaning(t *testing.T) {
	store := newFaultStore(t, map[string]string{
		"fs/Save.DAT":     "removed under a released build",
		"fs/Gone.DAT":     "removed under a released build",
		"fs/Saves/Slot1":  "written under a released build",
		"fs/Kept.DAT":     "kept",
		fileRemovedKey:    "gone.dat\nsave.dat",
		fileCreatedKey:    "gone.dat\nkept.dat\nsave.dat\nsaves/slot1",
		"fs/Removed.DAT":  "removed under this build",
		"fs/removed.dat2": "unrelated",
	})
	client := fixtureClient(t)
	client.saveStore = store
	files := newGuestFiles(t, client)
	for _, name := range []string{"Save.DAT", "save.dat", "SAVE.DAT", "./Gone.DAT", "gone.dat"} {
		if files.exists(name) {
			t.Fatalf("%s exists under a released removal list", name)
		}
	}
	if listed := client.listDirectory("Saves"); !reflect.DeepEqual(listed, []string{"slot1"}) {
		t.Fatalf("a released created list lists %q", listed)
	}
	handle := files.open("Save.DAT", fileOpenReadWrite)
	files.write(handle, "again")
	files.close(handle)
	if !files.exists("Save.DAT") || strings.Contains(store.held(fileRemovedKey), "save.dat") {
		t.Fatalf("a write left the released entry behind: %q", store.held(fileRemovedKey))
	}
	callSlot(t, client, slotFsRemove, files.name("Removed.DAT"))
	if stored := store.held(fileRemovedKey); !strings.HasPrefix(stored, "gone.dat\n") || !strings.HasSuffix(stored, "\nRemoved.DAT") {
		t.Fatalf("the list holds %q, want the released entry kept as it was and the new one as the title spelled it", stored)
	}
	next := fixtureClient(t)
	next.saveStore = store
	nextFiles := newGuestFiles(t, next)
	if nextFiles.exists("Removed.DAT") || nextFiles.exists("gone.dat") || nextFiles.exists("GONE.DAT") || !nextFiles.exists("Save.DAT") {
		t.Fatalf("the next session reads %q as another list", store.held(fileRemovedKey))
	}
	if !nextFiles.exists("removed.dat2") {
		t.Fatal("an entry of this build hides another name")
	}
}

// Rule 1. A write through one handle is what a handle opened before it reads,
// what a File opened before it reports as its size, and what a stream opened
// before it reads next.
func TestAWriteIsWhatEveryHandleOnThePathReads(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123456789"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	older := files.open("save.dat", fileOpenReadOnly)
	object := openJavaFile(t, client, "save.dat", 4)
	stream, err := javaFileOpenInputStream(client, ctx, client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	writer := files.open("save.dat", fileOpenReadWrite)
	files.write(writer, "AB")
	files.seek(writer, 10)
	files.write(writer, "XYZ")
	if got := files.read(older, 16); got != "AB23456789XYZ" {
		t.Fatalf("a handle opened before the write reads %q", got)
	}
	if size := javaFileSizeOf(t, client, object); size != 13 {
		t.Fatalf("a File opened before the write is %d bytes", size)
	}
	if got := readJavaStreamText(t, client, stream, 16); got != "AB23456789XYZ" {
		t.Fatalf("a stream opened before the write reads %q", got)
	}
}

// readJavaStreamText reads up to count bytes through InputStream.read(byte[]).
func readJavaStreamText(t *testing.T, client *Client, stream uint32, count int) string {
	t.Helper()
	array, err := client.newJavaByteArray(make([]byte, count))
	if err != nil {
		t.Fatal(err)
	}
	read, err := javaStreamReadArray(client, context.Background(), client.thread, []uint32{stream, array})
	if err != nil {
		t.Fatal(err)
	}
	if int32(read) <= 0 {
		return ""
	}
	data, err := client.readJavaArrayBytes(array)
	if err != nil {
		t.Fatal(err)
	}
	return string(data[:read])
}

// Rules 1 and 2 for streams. A byte written into a stream opened on a File,
// directly or through a DataOutputStream over it, is a write to the file at
// once: the File's size, a stat and another open of the path see it. It waits
// in the File's buffer like a write through the File itself, so a quick save
// stores it as it stores any waiting write, and so does the stream's flush.
func TestAStreamWriteIsAWriteToTheFile(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "old"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "save.dat", 3)
	sink := openJavaOutputStream(t, client, object)
	writeJavaSink(t, client, sink, "streamed")
	wrapper, err := newTestObject(t, client, javaDataOutputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapSink(client, ctx, client.thread, []uint32{wrapper, sink}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaSinkAppend(4)(client, ctx, client.thread, []uint32{wrapper, 0x41424344}); err != nil {
		t.Fatal(err)
	}
	if size := javaFileSizeOf(t, client, object); size != 12 {
		t.Fatalf("after the stream's writes the File is %d bytes", size)
	}
	files := newGuestFiles(t, client)
	if size, found := files.stat("save.dat"); !found || size != 12 {
		t.Fatalf("after the stream's writes the path stats as %d bytes, found %t", size, found)
	}
	reader := files.open("save.dat", fileOpenReadOnly)
	if got := files.read(reader, 32); got != "streamedABCD" {
		t.Fatalf("another open reads %q", got)
	}
	if got := store.held("fs/save.dat"); got != "" {
		t.Fatalf("the stream's writes reached the store before a flush, a close or a quick save: %q", got)
	}
	fixture.settle()
	roundTripCheckpoint(t, fixture.archive, fixture.session)
	if got := store.held("fs/save.dat"); got != "streamedABCD" {
		t.Fatalf("the quick save stored %q", got)
	}
	// The close of any handle on the path stores the buffer the handles share,
	// and there is nothing left to store here.
	files.close(reader)
	writeJavaSink(t, client, sink, "+more")
	if _, err := javaByteSinkFlush(client, ctx, client.thread, []uint32{sink}); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/save.dat"); got != "streamedABCD+more" {
		t.Fatalf("after the stream's flush the store holds %q", got)
	}
	// A flush the store refuses throws the IOException it declares and keeps
	// the write waiting in the File, for the File's close to store.
	writeJavaSink(t, client, sink, "!")
	store.refuse = true
	if _, err := javaByteSinkFlush(client, ctx, client.thread, []uint32{sink}); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("a flush the store refused answered %v", err)
	}
	store.refuse = false
	if size, _ := files.stat("save.dat"); size != 18 || store.held("fs/save.dat") != "streamedABCD+more" {
		t.Fatalf("a refused flush left the path at %d bytes and %q stored", size, store.held("fs/save.dat"))
	}
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/save.dat"); got != "streamedABCD+more!" {
		t.Fatalf("the File's close stored %q", got)
	}
}

// A record an earlier build took can hold bytes a stream on a File had not
// handed the File yet: that build kept them in the stream until a flush. They
// load into the File at its cursor, as the write that put them in the stream
// would have, and wait there for the flush, the File's close, the ending or a
// quick save. The record is as that build wrote it; the load writes nothing to
// the file; and a load that binds the saves twice puts the bytes in once.
func TestStreamBytesAnEarlierBuildRecordedLoadIntoTheFile(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/stream.dat": "0123"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "stream.dat", 4)
	_, open, err := client.javaFileHandle(object)
	if err != nil {
		t.Fatal(err)
	}
	open.cursor = 2
	sink := openJavaOutputStream(t, client, object)
	// What an earlier build left: the bytes in the stream, the File as it was.
	client.javaRun.sinks[sink] = []byte("XYZ")
	fixture.settle()
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	if saved := decodeRuntime(t, checkpoint); saved.Client.Java == nil || !slices.ContainsFunc(saved.Client.Java.Sinks, func(entry javaBytesState) bool {
		return entry.Object == sink && string(entry.Data) == "XYZ"
	}) {
		t.Fatal("the record does not hold the stream's bytes the way an earlier build wrote them")
	}
	// The running session has a write of its own waiting, which the load
	// stores first and then binds the restored session's saves again.
	files := newGuestFiles(t, client)
	files.write(files.open("other.dat", fileOpenReadWrite), "pending")
	restored := mustCommitFixture(t, fixture.archive, checkpoint, fixture.session, store)
	if store.held("fs/other.dat") != "pending" || store.held("fs/stream.dat") != "0123" {
		t.Fatalf("the load left %q and %q in the store", store.held("fs/other.dat"), store.held("fs/stream.dat"))
	}
	rclient := restored.client
	_, file, err := rclient.javaFileHandle(object)
	if err != nil {
		t.Fatal(err)
	}
	if string(file.data) != "01XYZ" || file.cursor != 5 || !file.dirty || len(rclient.javaRun.sinks[sink]) != 0 {
		t.Fatalf("the File came back holding %q at %d (waiting %t), the stream %q", file.data, file.cursor, file.dirty, rclient.javaRun.sinks[sink])
	}
	if size, found := newGuestFiles(t, rclient).stat("stream.dat"); !found || size != 5 {
		t.Fatalf("the restored path stats as %d bytes, found %t", size, found)
	}
	if _, err := javaByteSinkFlush(rclient, ctx, rclient.thread, []uint32{sink}); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/stream.dat"); got != "01XYZ" {
		t.Fatalf("the restored stream's flush stored %q", got)
	}
}

// Opening a missing file for writing creates it: an empty file that every
// later read sees at once, in the store from the open on, through every C
// flag that writes and every Java mode that does. Opening it only to read
// fails and makes nothing. A create the store refuses waits in the buffer,
// where every read sees it, until a close or the ending stores it.
func TestOpeningAMissingFileForWritingCreatesIt(t *testing.T) {
	made := func(t *testing.T, client *Client, store *faultStore, files *guestFiles) {
		t.Helper()
		if got := store.held("fs/made.dat"); got != "" {
			t.Fatalf("after the open the store holds %q", got)
		}
		if size, found := files.stat("made.dat"); !found || size != 0 || !files.exists("made.dat") {
			t.Fatalf("the made file stats as %d bytes, found %t", size, found)
		}
		reader := files.open("made.dat", fileOpenReadOnly)
		if got := files.read(reader, 8); got != "" {
			t.Fatalf("the made file reads %q", got)
		}
		files.close(reader)
		if !containsExact(client.listDirectory("/"), "made.dat") {
			t.Fatalf("the root lists %q", client.listDirectory("/"))
		}
	}
	for _, flag := range []uint32{fileOpenWriteOnly, fileOpenWriteTruncate, fileOpenReadWrite} {
		t.Run(fmt.Sprintf("MC_fsOpen flag %d", flag), func(t *testing.T) {
			client, store, files := fileFixture(t, nil)
			handle := files.open("made.dat", flag)
			made(t, client, store, files)
			files.close(handle)
			if got := store.held("fs/made.dat"); got != "" {
				t.Fatalf("after the close the store holds %q", got)
			}
		})
	}
	for _, mode := range []uint32{2, 3, 4} {
		t.Run(fmt.Sprintf("File mode %d", mode), func(t *testing.T) {
			store := newFaultStore(t, nil)
			fixture := newJavaThreadFixture(t, store)
			client := fixture.client
			object := openJavaFile(t, client, "made.dat", mode)
			made(t, client, store, newGuestFiles(t, client))
			if size := javaFileSizeOf(t, client, object); size != 0 {
				t.Fatalf("the made File is %d bytes", size)
			}
		})
	}
	t.Run("only to read", func(t *testing.T) {
		store := newFaultStore(t, nil)
		fixture := newJavaThreadFixture(t, store)
		client := fixture.client
		files := newGuestFiles(t, client)
		if got := int32(callSlot(t, client, slotFsOpen, files.name("made.dat"), fileOpenReadOnly)); got != wipiNoEntry {
			t.Fatalf("MC_fsOpen read-only of a missing file answered %d", got)
		}
		object, err := newTestObject(t, client, javaFileClass)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := javaFileOpen(client, context.Background(), client.thread, []uint32{object, newTestString(t, client, "made.dat"), 1, 0}); err == nil || !strings.Contains(err.Error(), javaIOExceptionClass) {
			t.Fatalf("new File(name, 1) of a missing file = %v, want an IOException", err)
		}
		if store.held("fs/made.dat") != "<absent>" || files.exists("made.dat") || containsExact(client.listDirectory("/"), "made.dat") || store.attempts != 0 {
			t.Fatal("a read-only open of a missing file made it")
		}
	})
	t.Run("a create the store refuses", func(t *testing.T) {
		client, store, files := fileFixture(t, nil)
		store.refuse = true
		handle := files.open("made.dat", fileOpenReadWrite)
		if size, found := files.stat("made.dat"); !found || size != 0 || store.held("fs/made.dat") != "<absent>" {
			t.Fatalf("a refused create stats as %d bytes, found %t, stored %q", size, found, store.held("fs/made.dat"))
		}
		if got := int32(callSlot(t, client, slotFsClose, handle)); got != wipiError {
			t.Fatalf("a close the store refused answered %d", got)
		}
		store.refuse = false
		client.flushOpenFiles()
		if got := store.held("fs/made.dat"); got != "" {
			t.Fatalf("the ending stored %q for the made file", got)
		}
	})
}

// Rule 2. A handle closed last does not put the file as it was when that
// handle was opened over a write another handle made in the meantime.
func TestTheLastCloseKeepsAnotherHandlesWrite(t *testing.T) {
	_, store, files := fileFixture(t, map[string]string{"fs/save.dat": "0123456789"})
	first := files.open("save.dat", fileOpenReadWrite)
	second := files.open("save.dat", fileOpenReadWrite)
	files.write(second, "XY")
	files.close(second)
	files.seek(first, 9)
	files.write(first, "Z")
	files.close(first)
	if got := store.held("fs/save.dat"); got != "XY2345678Z" {
		t.Fatalf("after both closes the store holds %q", got)
	}
}

// Rule 2. The ending is where what the store refused earlier is given to it
// again: the removal list as well as a file, and a database's records. The
// list waits when a rename has already stored the file under its new name
// and the store refuses the list that hides the old one; a removal the store
// refuses outright does not happen, and leaves nothing to wait
// (TestStorageContractCRemove).
func TestTheEndingGivesTheStoreWhatItRefusedEarlier(t *testing.T) {
	store := newFaultStore(t, map[string]string{"fs/save.dat": "stored"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	database := openFixtureDatabase(t, client, "rank", 4)
	client.saveStore = keyRefusingWrites{faultStore: store, key: fileRemovedKey}
	if got := int32(callSlot(t, client, slotFsRename, files.name("save.dat"), files.name("moved.dat"), 1)); got != wipiSuccess {
		t.Fatalf("a rename whose removal list the store refused answered %d", got)
	}
	if !client.removedUnsaved || files.exists("save.dat") {
		t.Fatal("the refused removal list is not waiting, or the old name is still there")
	}
	client.saveStore = store
	store.refuse = true
	insertFixtureRecord(t, client, database, []byte{1, 2, 3, 4})
	store.refuse = false
	client.flushOpenFiles()
	if !strings.Contains(store.held(fileRemovedKey), "save.dat") || client.removedUnsaved {
		t.Fatalf("after the ending the removal list is %q", store.held(fileRemovedKey))
	}
	if held := client.javaRun.databases[database]; held.unsaved || store.held("jdb/rank.db") != string(held.encode()) {
		t.Fatal("the ending did not store the record the store had refused")
	}
	next := fixtureClient(t)
	next.saveStore = store
	if newGuestFiles(t, next).exists("save.dat") {
		t.Fatal("the next session finds the file under its old name")
	}
}

// Rule 4. A removal takes with it what was waiting in a handle open on the
// file: the ending does not bring the file back. A write through that handle
// after the removal is a write of the path, which is back at once.
func TestARemovalTakesTheWritesWaitingOnTheFile(t *testing.T) {
	client, store, files := fileFixture(t, map[string]string{"fs/save.dat": "stored"})
	handle := files.open("save.dat", fileOpenReadWrite)
	files.write(handle, "pending")
	callSlot(t, client, slotFsRemove, files.name("save.dat"))
	client.flushOpenFiles()
	if files.exists("save.dat") || store.held("fs/save.dat") != "stored" {
		t.Fatalf("the ending brought the removed file back: %q", store.held("fs/save.dat"))
	}
	files.write(handle, "again")
	if !files.exists("save.dat") {
		t.Fatal("a write through the handle after the removal is not seen")
	}
	files.close(handle)
	// The buffer was empty after the removal and the handle was at 7, so the
	// file that comes back is seven zeros and the new write.
	if !files.exists("save.dat") || store.held("fs/save.dat") != "\x00\x00\x00\x00\x00\x00\x00again" ||
		strings.Contains(store.held(fileRemovedKey), "save.dat") {
		t.Fatalf("the close stored %q and left the list at %q", store.held("fs/save.dat"), store.held(fileRemovedKey))
	}
}

// keyRefusingWrites refuses every write of one key, the way a file the Host
// cannot replace does, and takes every other write.
type keyRefusingWrites struct {
	*faultStore
	key string
}

func (store keyRefusingWrites) StoreSave(name string, data []byte) error {
	if name == store.key {
		return errors.New("the fixture store refuses this key")
	}
	return store.faultStore.StoreSave(name, data)
}

// Rule 4. A write the store refuses does not take the removal off the list,
// even when the store takes the list: the next session must not find the
// bytes the removal hid. The session itself sees the write, which waits in
// its handle.
func TestARefusedWriteLeavesTheRemovalOnTheList(t *testing.T) {
	client, store, files := fileFixture(t, map[string]string{"fs/save.dat": "the removed save"})
	callSlot(t, client, slotFsRemove, files.name("save.dat"))
	client.saveStore = keyRefusingWrites{faultStore: store, key: "fs/save.dat"}
	// The open makes the file again, which is a write of the path too, and
	// the store refuses it as it refuses the write that follows.
	handle := files.open("./save.dat", fileOpenReadWrite)
	files.write(handle, "new")
	if got := int32(callSlot(t, client, slotFsClose, handle)); got != wipiError {
		t.Fatalf("a close the store refused answered %d", got)
	}
	if !files.exists("save.dat") {
		t.Fatal("the session does not see its own write")
	}
	if !strings.Contains(store.held(fileRemovedKey), "save.dat") {
		t.Fatalf("a refused write took the removal off the stored list: %q", store.held(fileRemovedKey))
	}
	next := fixtureClient(t)
	next.saveStore = store
	if data, found := next.readFile("save.dat"); found {
		t.Fatalf("the next session reads %q under a removed name", data)
	}
	// Once the store takes the file, the removal comes off the list.
	client.saveStore = store
	files.close(handle)
	if store.held("fs/save.dat") != "new" || strings.Contains(store.held(fileRemovedKey), "save.dat") {
		t.Fatalf("the close stored %q and left the list at %q", store.held("fs/save.dat"), store.held(fileRemovedKey))
	}
}

// Rule 6. Reading, opening a file that is there for writing and closing it
// unwritten, a stat, an existence check, the ending and a quick save with
// nothing waiting store nothing.
func TestOnlyTheTitlesWritesReachTheStore(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, map[string]string{"fs/save.dat": "stored"})
	session := checkpointFixtureSession(t, archive, store)
	tickSession(t, session, 2)
	files := newGuestFiles(t, session.client)
	reader := files.open("save.dat", fileOpenReadOnly)
	files.read(reader, 8)
	writer := files.open("save.dat", fileOpenReadWrite)
	files.read(writer, 3)
	files.stat("save.dat")
	files.exists("missing.dat")
	files.close(writer)
	roundTripCheckpoint(t, archive, session)
	tickSession(t, session, 2)
	session.client.flushOpenFiles()
	if store.attempts != 0 {
		t.Fatalf("the store was written %d times", store.attempts)
	}
}

// Rules 1 and 2 for DataBase objects. Every object on a database is one set
// of records, so a write the store refused is what another object reads, and
// the next store carries both objects' writes: an older copy never goes over
// a newer one. A deletion takes a refused write with it.
func TestDataBaseObjectsOnOneDatabaseAreOneSetOfRecords(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	first := openFixtureDatabase(t, client, "rank", 4)
	store.refuse = true
	insertFixtureRecord(t, client, first, []byte("AAAA"))
	store.refuse = false
	second := openFixtureDatabase(t, client, "rank", 4)
	if got := databaseRecord(t, client, second, 0); string(got) != "AAAA" {
		t.Fatalf("a second object reads %q", got)
	}
	insertFixtureRecord(t, client, second, []byte("BBBB"))
	if _, err := javaCloseDataBase(client, ctx, client.thread, []uint32{first}); err != nil {
		t.Fatal(err)
	}
	want := &databaseContent{}
	if err := want.decode([]byte(store.held("jdb/rank.db"))); err != nil {
		t.Fatal(err)
	}
	if len(want.records) != 2 || string(want.records[0]) != "AAAA" || string(want.records[1]) != "BBBB" {
		t.Fatalf("after the first object's close the store holds %q", want.records)
	}

	// A refused write and then a deletion: nothing brings the database back.
	store.refuse = true
	insertFixtureRecord(t, client, second, []byte("CCCC"))
	store.refuse = false
	if _, err := javaDeleteDataBase(client, ctx, client.thread, []uint32{newTestString(t, client, "rank")}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaCloseDataBase(client, ctx, client.thread, []uint32{second}); err != nil {
		t.Fatal(err)
	}
	client.flushOpenFiles()
	if _, exists := client.readFile(databaseFileName("rank")); exists {
		t.Fatal("a write the store refused before the deletion brought the database back")
	}
	fixture.settle()
	attempts := store.attempts
	if _, err := fixture.session.CaptureCheckpoint(ctx); err != nil || store.attempts != attempts {
		t.Fatalf("a quick save after the deletion = %v, after %d store writes", err, store.attempts-attempts)
	}
}

// What the review of the 2026-10-17 file fixes found: a rewrite a stream
// outlived recorded as one in progress, a list read on the live session for a
// packaged file, a packaged Korean name not known under its raw spelling, a
// read-only close refused for another handle's write, a rename onto a name with
// no save, a backslash path the removal list dropped, a wrapper's close that
// left its stream's file open, an adapter over no store that kept every file
// twice, and a database name a refused index lost.

// A handle opened only to read has nothing of its own to store, so its close
// succeeds while another handle's write waits on a store that refuses it, in
// C and in Java alike. The write is stored by its own handle's close.
func TestAReadOnlyCloseIgnoresAnotherHandlesRefusedWrite(t *testing.T) {
	ctx := context.Background()
	client, store, files := fileFixture(t, map[string]string{"fs/save.dat": "stored"})
	writer := files.open("save.dat", fileOpenReadWrite)
	files.write(writer, "new")
	store.refuse = true
	reader := files.open("save.dat", fileOpenReadOnly)
	if result := int32(callSlot(t, client, slotFsClose, reader)); result != 0 {
		t.Fatalf("a read-only close answered %d", result)
	}
	object := openJavaFile(t, client, "save.dat", 1)
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatalf("a read-only File's close answered %v", err)
	}
	store.refuse = false
	files.close(writer)
	if got := store.held("fs/save.dat"); got != "newred" {
		t.Fatalf("the writer's close stored %q", got)
	}
}

// A rename onto a name the store cannot hold is refused with the
// specification's bad-name code, and the file stays where it was.
func TestARenameOntoANameWithNoSaveIsRefused(t *testing.T) {
	client, store, files := fileFixture(t, map[string]string{"fs/save.dat": "progress"})
	for _, target := range []string{"__databases__.db", ".created", ".removed/x"} {
		if got := int32(callSlot(t, client, slotFsRename, files.name("save.dat"), files.name(target))); got != wipiBadFileName {
			t.Fatalf("a rename onto %q answered %d, want %d", target, got, wipiBadFileName)
		}
		if !files.exists("save.dat") {
			t.Fatalf("the rename onto %q took the file away", target)
		}
	}
	if got := store.held("fs/save.dat"); got != "progress" {
		t.Fatalf("the save holds %q", got)
	}
}

// A backslash separates a path as a slash does, which is how the archive
// lookup reads it: a removal of the backslash spelling hides the packaged
// file, in this session and the next, and a write of one is stored under the
// slash path. An entry an earlier build wrote with one is one of that build's
// other spellings: it still hides that spelling, and a rewrite of the list
// keeps it.
func TestABackslashPathIsTheSlashPath(t *testing.T) {
	client, store, files := fileFixture(t, nil)
	callSlot(t, client, slotFsRemove, files.name(`data\hello.txt`))
	if files.exists("data/hello.txt") {
		t.Fatal("the packaged file is still there after its removal")
	}
	handle := files.open(`data\saved.dat`, fileOpenReadWrite)
	files.write(handle, "kept")
	files.close(handle)
	if got := store.held("fs/data/saved.dat"); got != "kept" {
		t.Fatalf("a write of a backslash path stored %q", got)
	}
	next := fixtureClient(t)
	next.saveStore = store
	nextFiles := newGuestFiles(t, next)
	if nextFiles.exists("data/hello.txt") {
		t.Fatal("the removed packaged file came back in the next session")
	}
	store.behind(t, fileRemovedKey, `data\other.txt`)
	earlier := fixtureClient(t)
	earlier.saveStore = store
	earlierFiles := newGuestFiles(t, earlier)
	if earlierFiles.exists(`data\other.txt`) {
		t.Fatal("an earlier build's backslash entry no longer hides its spelling")
	}
	callSlot(t, earlier, slotFsRemove, earlierFiles.name("data/hello.txt"))
	if list := store.held(fileRemovedKey); !strings.Contains(list, `data\other.txt`) {
		t.Fatalf("a rewrite of the list dropped the earlier build's entry: %q", list)
	}
}

// An authentication adapter over no store keeps the file it rewrites and
// nothing else, and the session keeps the title's other files itself, within
// its bound: once, not twice.
func TestAnAdapterOverNoStoreKeepsOnlyItsOwnFile(t *testing.T) {
	client := fixtureClient(t)
	contract := &certificate100Contract{name: "state.dat", application: client.archive.Descriptor.AID, token: "Example"}
	adapter := newAuthenticationCertificate100Store(nil, contract)
	client.saveStore = adapter
	files := newGuestFiles(t, client)
	handle := files.open("save.dat", fileOpenReadWrite)
	files.write(handle, "progress")
	files.close(handle)
	if _, held := adapter.volatile["fs/save.dat"]; held {
		t.Fatal("the adapter kept the title's save beside the session")
	}
	if got := string(client.sessionFiles["fs/save.dat"]); got != "progress" {
		t.Fatalf("the session keeps %q", got)
	}
	reader := files.open("save.dat", fileOpenReadOnly)
	if got := files.read(reader, 16); got != "progress" {
		t.Fatalf("the save reads %q", got)
	}
	files.close(reader)
}

// The file and record store boundary, from the side a title cannot see: what
// reaches the save store, and what one view of a file tells another.

// fileFixture is a client over a store of its own, driven through the file
// slots the way a Clet drives them.
func fileFixture(t *testing.T, entries map[string]string) (*Client, *faultStore, *guestFiles) {
	t.Helper()
	store := newFaultStore(t, entries)
	client := fixtureClient(t)
	client.saveStore = store
	return client, store, newGuestFiles(t, client)
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
	client, store, files := fileFixture(t, map[string]string{"fs/save.dat": "old"})
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

	// A Java File closed over a refusing store stays open with its write, and
	// the title is thrown the IOException the close declares.
	fixture := newJavaThreadFixture(t, store)
	java := fixture.client
	object := openJavaFile(t, java, "java.dat", 4)
	if _, err := javaFileWriteByte(java, context.Background(), java.thread, []uint32{object, 'J'}); err != nil {
		t.Fatal(err)
	}
	store.refuse = true
	if _, err := javaFileClose(java, context.Background(), java.thread, []uint32{object}); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("a File close the store refused answered %v", err)
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

func mustByteArray(t *testing.T, client *Client, text string) uint32 {
	t.Helper()
	array, err := client.newJavaByteArray([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return array
}

// A platform list and the scope itself are not guest files: a title cannot
// open a file under one of those names — the file calls refuse it as a name
// no file can have — so it cannot change what the platform reads back as its
// list, or put a file where the scope's directory is.
func TestAGuestFileCannotTakeThePlatformsKeys(t *testing.T) {
	t.Run("the path lists", func(t *testing.T) {
		client, store, files := fileFixture(t, nil)
		kept := files.open("real.dat", fileOpenReadWrite)
		files.write(kept, "keep")
		files.close(kept)
		for _, name := range []string{"/.removed", ".CREATED", "/.created/inside", "/.Directories"} {
			if handle := int32(callSlot(t, client, slotFsOpen, files.name(name), fileOpenReadWrite)); handle != wipiBadFileName {
				t.Fatalf("open %q answered %d, want M_E_BADFILENAME", name, handle)
			}
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
		if handle := int32(callSlot(t, client, slotFsOpen, files.name("/"), fileOpenReadWrite)); handle != wipiBadFileName {
			t.Fatalf("open \"/\" answered %d, want M_E_BADFILENAME", handle)
		}
		handle := files.open("real.dat", fileOpenReadWrite)
		files.write(handle, "keep")
		files.close(handle)
		data, found, err := backend.ReadSave(backend.NewDirectorySaveStore(root), "fs/real.dat")
		if err != nil || !found || string(data) != "keep" {
			t.Fatalf("after a write of \"/\" the save store holds %q for real.dat (found=%t, err=%v)", data, found, err)
		}
	})
}

// A removal hides the file under every spelling the store gives one key to,
// and only that file: a name that differs only by case is another file.
func TestARemovalHidesTheFileUnderEverySpelling(t *testing.T) {
	client, _, files := fileFixture(t, map[string]string{"fs/save.dat": "progress", "fs/SAVE.DAT": "another file"})
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
	_, store, files := fileFixture(t, map[string]string{
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
	client, store, files := fileFixture(t, map[string]string{"fs/save.dat": "old"})
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
