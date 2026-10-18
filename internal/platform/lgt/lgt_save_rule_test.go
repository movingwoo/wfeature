package lgt

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
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
	client, store, files := reviewFiles(t, map[string]string{"fs/slot.dat": "OLD SAVE"})
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
			client, store, files := reviewFiles(t, nil)
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
		client, store, files := reviewFiles(t, nil)
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
	_, store, files := reviewFiles(t, map[string]string{"fs/save.dat": "0123456789"})
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
// again: the removal list as well as a file, and a database's records.
func TestTheEndingGivesTheStoreWhatItRefusedEarlier(t *testing.T) {
	store := newFaultStore(t, map[string]string{"fs/save.dat": "stored"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	database := openFixtureDatabase(t, client, "rank", 4)
	store.refuse = true
	callSlot(t, client, slotFsRemove, files.name("save.dat"))
	insertFixtureRecord(t, client, database, []byte{1, 2, 3, 4})
	store.refuse = false
	client.flushOpenFiles()
	if !strings.Contains(store.held(fileRemovedKey), "save.dat") || client.removedUnsaved {
		t.Fatalf("after the ending the removal list is %q", store.held(fileRemovedKey))
	}
	if held := client.javaRun.databases[database]; held.unsaved || store.held("fs/rank.db") != string(held.encode()) {
		t.Fatal("the ending did not store the record the store had refused")
	}
	next := fixtureClient(t)
	next.saveStore = store
	if newGuestFiles(t, next).exists("save.dat") {
		t.Fatal("the next session finds the removed file")
	}
}

// Rule 4. A removal takes with it what was waiting in a handle open on the
// file: the ending does not bring the file back. A write through that handle
// after the removal is a write of the path, which is back at once.
func TestARemovalTakesTheWritesWaitingOnTheFile(t *testing.T) {
	client, store, files := reviewFiles(t, map[string]string{"fs/save.dat": "stored"})
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
	client, store, files := reviewFiles(t, map[string]string{"fs/save.dat": "the removed save"})
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
	if err := want.decode([]byte(store.held("fs/rank.db"))); err != nil {
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
