package lgt

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// What the review of the 2026-10-17 file fixes found: a rewrite a stream
// outlived recorded as one in progress, a list read on the live session for a
// packaged file, a packaged Korean name not known under its raw spelling, a
// read-only close refused for another handle's write, a rename onto a name with
// no save, a backslash path the removal list dropped, a wrapper's close that
// left its stream's file open, an adapter over no store that kept every file
// twice, and a database name a refused index lost.

// A File opened to rewrite its save and closed while a stream is still open on
// it has finished the rewrite: a quick save taken then records an ordinary
// handle, and a load keeps a longer save written after the quick save whole
// instead of cutting it back to what the File held, and the stream's close
// stores it whole.
func TestARewriteClosedBeforeAQuickSaveKeepsALaterSaveWhole(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "an older save"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "save.dat", 3)
	sink := openJavaOutputStream(t, client, object)
	writeJavaSink(t, client, sink, "short")
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/save.dat"); got != "short" {
		t.Fatalf("the close stored %q", got)
	}
	fixture.settle()
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	const later = "a longer save written after the quick save"
	store.behind(t, "fs/save.dat", later)
	restored := mustCommitFixture(t, fixture.archive, checkpoint, fixture.session, store)
	files := newGuestFiles(t, restored.client)
	reader := files.open("save.dat", fileOpenReadOnly)
	if got := files.read(reader, 64); got != later {
		t.Fatalf("after the load the save reads %q", got)
	}
	files.close(reader)
	if err := closeJavaSink(restored.client, sink); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/save.dat"); got != later {
		t.Fatalf("the stream's close stored %q", got)
	}
}

// A quick save that stores packaged files the title rewrote reads nothing on
// the live session for them: a packaged file never goes on the created list,
// so the list is not asked. Read first, a list the store could not read
// refused the quick save and every write of the session after it.
func TestAQuickSaveOfRewrittenPackagedFilesReadsNoList(t *testing.T) {
	store := newFaultStore(t, nil)
	session := checkpointFixtureSession(t, fixtureArchive(t), store)
	tickSession(t, session, 2)
	files := newGuestFiles(t, session.client)
	first := files.open("data/hello.txt", fileOpenReadWrite)
	files.write(first, "mine")
	second := files.open("data/other.txt", fileOpenReadWrite)
	files.write(second, "also")
	store.unreadable = fileCreatedKey
	if _, err := session.CaptureCheckpoint(context.Background()); err != nil {
		t.Fatalf("the quick save: %v", err)
	}
	if err := session.client.saveReadError; err != nil {
		t.Fatalf("the quick save left the session unable to write: %v", err)
	}
	for key, prefix := range map[string]string{"fs/data/hello.txt": "mine", "fs/data/other.txt": "also"} {
		if got := store.held(key); !strings.HasPrefix(got, prefix) {
			t.Fatalf("the quick save left %q under %s", got, key)
		}
	}
	files.close(first)
	files.close(second)
}

// A C Clet that names a packaged Korean file in its own EUC-KR bytes names the
// packaged file, as the archive lookup reads it: rewriting it keeps it off the
// created list, and the directory lists it once.
func TestARawSpellingOfAPackagedKoreanNameIsThePackagedFile(t *testing.T) {
	client, store, files := reviewFiles(t, nil)
	client.archive.Resources["data/세이브.dat"] = []byte("packaged")
	raw := "data/" + string([]byte{0xbc, 0xbc, 0xc0, 0xcc, 0xba, 0xea}) + ".dat"
	handle := files.open(raw, fileOpenWriteTruncate)
	files.write(handle, "mine")
	files.close(handle)
	if list := store.held(fileCreatedKey); list != "<absent>" && list != "" {
		t.Fatalf("the created list holds %q", list)
	}
	listed := client.listDirectory("data")
	if count := len(listed) - len(removeAll(listed, "세이브.dat")); count != 1 || len(listed) != 3 {
		t.Fatalf("data lists %q", listed)
	}
}

func removeAll(names []string, name string) []string {
	var kept []string
	for _, entry := range names {
		if entry != name {
			kept = append(kept, entry)
		}
	}
	return kept
}

// A handle opened only to read has nothing of its own to store, so its close
// succeeds while another handle's write waits on a store that refuses it, in
// C and in Java alike. The write is stored by its own handle's close.
func TestAReadOnlyCloseIgnoresAnotherHandlesRefusedWrite(t *testing.T) {
	ctx := context.Background()
	client, store, files := reviewFiles(t, map[string]string{"fs/save.dat": "stored"})
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
	client, store, files := reviewFiles(t, map[string]string{"fs/save.dat": "progress"})
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
	client, store, files := reviewFiles(t, nil)
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

// A DataInputStream is the stream it wraps, so closing the wrapper lets go of
// the file the stream under it holds open after its File closed, at once
// rather than at a collection.
func TestClosingAWrapperLetsTheFileUnderItGo(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "save.dat", 1)
	stream, err := javaFileOpenInputStream(client, ctx, client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := newTestObject(t, client, javaDataInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapStream(client, ctx, client.thread, []uint32{wrapper, stream}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaStreamClose(client, ctx, client.thread, []uint32{wrapper}); err != nil {
		t.Fatal(err)
	}
	if len(client.files) != 0 || len(client.javaRun.files) != 0 {
		t.Fatalf("closing the wrapper left %d handles and %d bindings", len(client.files), len(client.javaRun.files))
	}
	if _, err := javaStreamClose(client, ctx, client.thread, []uint32{stream}); err != nil {
		t.Fatalf("closing the stream after its wrapper answered %v", err)
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

// A database made while the store refused the index stays a database the
// listing names: the index waits in the session, the next create starts from
// it rather than from the store's, and the store has both names once it takes
// the index again.
func TestADatabaseMadeWhileTheIndexWasRefusedIsListed(t *testing.T) {
	client, store, _ := reviewFiles(t, nil)
	store.refuse = true
	openFixtureDatabase(t, client, "first", 16)
	store.refuse = false
	if names := client.databaseNames(); !reflect.DeepEqual(names, []string{"first"}) {
		t.Fatalf("while the index waits listDataBases answers %q", names)
	}
	openFixtureDatabase(t, client, "second", 16)
	if names := client.databaseNames(); !reflect.DeepEqual(names, []string{"first", "second"}) {
		t.Fatalf("listDataBases answers %q", names)
	}
	if got := store.held(databaseIndexKey); got != "first\nsecond" {
		t.Fatalf("the index holds %q", got)
	}
}
