package lgt

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
)

// A quick load brings back the title and leaves its saves alone. These cover
// what that takes on this platform: the writes a title has issued are stored
// before a quick save and before a quick load, and what a restored title has
// open is read again from the store when the load commits.

// faultStore is the store a test's session runs over. It counts what reaches
// it, and a test can make it refuse writes, the way a full disk does, or fail
// the read of one key, the way a damaged one does.
type faultStore struct {
	*backend.MemorySaveStore
	reads, writes, attempts int
	refuse                  bool
	unreadable              string
}

func newFaultStore(t *testing.T, entries map[string]string) *faultStore {
	t.Helper()
	list := make([]backend.SaveEntry, 0, len(entries))
	for key, data := range entries {
		list = append(list, backend.SaveEntry{Key: key, Data: []byte(data)})
	}
	memory, err := backend.NewMemorySaveStore(list)
	if err != nil {
		t.Fatal(err)
	}
	return &faultStore{MemorySaveStore: memory}
}

func (store *faultStore) LoadSave(name string) ([]byte, bool) {
	data, present, _ := store.ReadSave(name)
	return data, present
}

func (store *faultStore) ReadSave(name string) ([]byte, bool, error) {
	store.reads++
	if store.unreadable != "" && name == store.unreadable {
		return nil, false, errors.New("the fixture store cannot read the entry")
	}
	return store.MemorySaveStore.ReadSave(name)
}

func (store *faultStore) StoreSave(name string, data []byte) error {
	store.attempts++
	if store.refuse {
		return errors.New("the fixture store refuses the write")
	}
	store.writes++
	return store.MemorySaveStore.StoreSave(name, data)
}

// held is what the store has under a key, and a marker when it has nothing.
func (store *faultStore) held(key string) string {
	data, found := store.MemorySaveStore.LoadSave(key)
	if !found {
		return "<absent>"
	}
	return string(data)
}

// behind changes the store without the session knowing, the way a save made
// after the quick save, or an import, changes it.
func (store *faultStore) behind(t *testing.T, key, data string) {
	t.Helper()
	if err := store.MemorySaveStore.StoreSave(key, []byte(data)); err != nil {
		t.Fatal(err)
	}
}

func (store *faultStore) snapshot(t *testing.T) []backend.SaveEntry {
	t.Helper()
	entries, err := store.MemorySaveStore.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// guestFiles drives one client's file slots the way the guest does.
type guestFiles struct {
	t      *testing.T
	client *Client
	buffer uint32
}

func newGuestFiles(t *testing.T, client *Client) *guestFiles {
	t.Helper()
	buffer, err := client.allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	return &guestFiles{t: t, client: client, buffer: buffer}
}

func (files *guestFiles) name(text string) uint32 {
	files.t.Helper()
	address, err := files.client.allocateBytes(append([]byte(text), 0))
	if err != nil {
		files.t.Fatal(err)
	}
	return address
}

func (files *guestFiles) open(name string, flag uint32) uint32 {
	files.t.Helper()
	handle := callSlot(files.t, files.client, slotFsOpen, files.name(name), flag)
	if int32(handle) < 0 {
		files.t.Fatalf("open %q answered %d", name, int32(handle))
	}
	return handle
}

func (files *guestFiles) write(handle uint32, text string) {
	files.t.Helper()
	if err := files.client.core.Memory().Write(files.buffer, []byte(text)); err != nil {
		files.t.Fatal(err)
	}
	if moved := callSlot(files.t, files.client, slotFsWrite, handle, files.buffer, uint32(len(text))); moved != uint32(len(text)) {
		files.t.Fatalf("write of %q moved %d bytes", text, int32(moved))
	}
}

func (files *guestFiles) read(handle uint32, length int) string {
	files.t.Helper()
	moved := callSlot(files.t, files.client, slotFsRead, handle, files.buffer, uint32(length))
	if int32(moved) < 0 {
		files.t.Fatalf("read answered %d", int32(moved))
	}
	data := make([]byte, moved)
	if err := files.client.core.Memory().Read(files.buffer, data); err != nil {
		files.t.Fatal(err)
	}
	return string(data)
}

func (files *guestFiles) close(handle uint32) {
	files.t.Helper()
	if result := callSlot(files.t, files.client, slotFsClose, handle); result != 0 {
		files.t.Fatalf("close answered %d", int32(result))
	}
}

func (files *guestFiles) exists(name string) bool {
	files.t.Helper()
	return callSlot(files.t, files.client, slotFsIsExist, files.name(name)) == 0
}

// commitFixture loads a checkpoint over a running session on its own store.
func commitFixture(t *testing.T, archive []byte, checkpoint backend.Checkpoint, running *Session, store backend.SaveStore) (*Session, error) {
	t.Helper()
	prepared, err := PrepareSessionCheckpoint(archive, checkpoint, SessionOptions{})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer prepared.Discard()
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		t.Fatalf("checking the load made %d save store calls, the first being %s", calls, first)
	}
	restored, err := prepared.Commit(context.Background(), running, store)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = restored.Close(context.Background()) })
	return restored, nil
}

func mustCommitFixture(t *testing.T, archive []byte, checkpoint backend.Checkpoint, running *Session, store backend.SaveStore) *Session {
	t.Helper()
	restored, err := commitFixture(t, archive, checkpoint, running, store)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return restored
}

// A quick save gives the store what the title has written to a file it has not
// closed, so its record carries no write: an open file is a path, a cursor and
// its flags. A store that will not take the write refuses the quick save, and
// the write is still where it was.
func TestCheckpointStoresUnclosedWritesWhenItIsTaken(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, nil)
	session := checkpointFixtureSession(t, archive, store)
	tickSession(t, session, 2)
	files := newGuestFiles(t, session.client)
	handle := files.open("open.dat", fileOpenReadWrite)
	files.write(handle, "MARKER-unclosed")
	if store.held("fs/open.dat") != "" || !session.client.files[handle].dirty {
		t.Fatalf("the write reached the store before any boundary: %q", store.held("fs/open.dat"))
	}

	store.refuse = true
	attempts := store.attempts
	if _, err := session.CaptureCheckpoint(context.Background()); !errors.Is(err, backend.ErrCheckpointSaveWrite) || !strings.Contains(err.Error(), "open.dat") {
		t.Fatalf("a quick save over a store that refuses = %v", err)
	}
	if store.attempts != attempts+1 || !session.client.files[handle].dirty || store.held("fs/open.dat") != "" {
		t.Fatal("the refused quick save lost the write or asked the store more than once")
	}
	tickSession(t, session, 2)
	store.refuse = false

	checkpoint := roundTripCheckpoint(t, archive, session)
	if store.held("fs/open.dat") != "MARKER-unclosed" {
		t.Fatalf("the quick save left the store holding %q", store.held("fs/open.dat"))
	}
	open := session.client.files[handle]
	if open == nil || open.dirty || open.cursor != 15 || string(open.data) != "MARKER-unclosed" {
		t.Fatalf("the quick save closed the file or moved it: %+v", open)
	}
	saved := decodeRuntime(t, checkpoint)
	// The open made the file, which is not an open that asked for an empty
	// one: the handle is recorded as any other.
	want := []fileState{{Handle: handle, Name: []byte("open.dat"), Cursor: 15, Writable: true}}
	if !reflect.DeepEqual(saved.Client.Files, want) {
		t.Fatalf("the record holds the files %+v, want %+v", saved.Client.Files, want)
	}
	if bytes.Contains(checkpoint.Runtime, []byte("MARKER-unclosed")) {
		t.Fatal("the record carries the file's bytes")
	}
	// A second quick save has nothing to store.
	attempts = store.attempts
	roundTripCheckpoint(t, archive, session)
	if store.attempts != attempts {
		t.Fatal("a quick save with nothing unstored wrote to the store")
	}
}

// What can be refused without asking the store is refused before the store is
// given anything: a quick save that is not going to happen stores nothing.
func TestCheckpointRefusalsStoreNothing(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, nil)
	session := checkpointFixtureSession(t, archive, store)
	tickSession(t, session, 2)
	client := session.client
	files := newGuestFiles(t, client)
	handle := files.open("open.dat", fileOpenReadWrite)
	files.write(handle, "waiting")
	attempts := store.attempts
	refused := func(why string) {
		t.Helper()
		if _, err := session.CaptureCheckpoint(context.Background()); err == nil {
			t.Fatalf("%s: the quick save was accepted", why)
		}
		if store.attempts != attempts || !client.files[handle].dirty {
			t.Fatalf("%s: the refused quick save wrote to the store", why)
		}
	}
	client.javaCallDepth = 1
	refused("inside a guest call")
	client.javaCallDepth = 0
	session.Cheat().Freezes().Insert(cheat.FreezeEntry{Address: checkpointCounter, Value: 1})
	refused("a frozen value")
	session.Cheat().Freezes().Clear()
	// An open file holding more than a load would read back.
	large := files.open("large.dat", fileOpenReadWrite)
	attempts = store.attempts
	kept := client.files[large].data
	client.files[large].data = make([]byte, maxStateBytes+1)
	refused("open files past the limit")
	client.files[large].data = kept
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := session.CaptureCheckpoint(cancelled); !errors.Is(err, context.Canceled) || store.attempts != attempts {
		t.Fatalf("a cancelled quick save = %v, after %d writes", err, store.attempts-attempts)
	}
	if _, err := session.CaptureCheckpoint(context.Background()); err != nil || store.held("fs/open.dat") != "waiting" {
		t.Fatalf("the quick save after the refusals = %v, with %q stored", err, store.held("fs/open.dat"))
	}
}

// A buffer with unstored writes that is behind its file is not stored by the
// host: it holds older bytes than the store for everything the title did not
// write through it. The quick save is refused, naming the file, with nothing
// written, until the title has closed the file.
func TestCheckpointRefusesAnOpenFileBehindItsSave(t *testing.T) {
	archive := fixtureArchive(t)
	for name, arrange := range map[string]func(files *guestFiles) (close func()){
		"another handle stored the file since": func(files *guestFiles) func() {
			first := files.open("save.dat", fileOpenReadWrite)
			second := files.open("save.dat", fileOpenReadWrite)
			files.write(second, "newer")
			files.close(second)
			files.write(first, "ol")
			return func() { files.close(first) }
		},
		"the file was removed since": func(files *guestFiles) func() {
			first := files.open("save.dat", fileOpenReadWrite)
			files.write(first, "ol")
			callSlot(files.t, files.client, slotFsRemove, files.name("save.dat"))
			return func() { files.close(first) }
		},
		"another file was renamed onto it since": func(files *guestFiles) func() {
			first := files.open("save.dat", fileOpenReadWrite)
			files.write(first, "ol")
			other := files.open("other.dat", fileOpenReadWrite)
			files.write(other, "moved")
			files.close(other)
			callSlot(files.t, files.client, slotFsRemove, files.name("save.dat"))
			callSlot(files.t, files.client, slotFsRename, files.name("other.dat"), files.name("save.dat"))
			return func() { files.close(first) }
		},
		"two handles on it have unstored writes": func(files *guestFiles) func() {
			first := files.open("save.dat", fileOpenReadWrite)
			second := files.open("save.dat", fileOpenReadWrite)
			files.write(first, "one")
			files.write(second, "two")
			return func() { files.close(first); files.close(second) }
		},
		// A title can spell one file more than one way, and the spellings are
		// one save.
		"another spelling of its name stored the file since": func(files *guestFiles) func() {
			first := files.open("save.dat", fileOpenReadWrite)
			files.write(first, "ol")
			second := files.open("./save.dat", fileOpenWriteTruncate)
			files.write(second, "newer")
			files.close(second)
			return func() { files.close(first) }
		},
		"two spellings of its name have unstored writes": func(files *guestFiles) func() {
			first := files.open("save.dat", fileOpenReadWrite)
			second := files.open("//SAVE.dat", fileOpenReadWrite)
			files.write(first, "one")
			files.write(second, "two")
			return func() { files.close(first); files.close(second) }
		},
		"its name is not one a save can have": func(files *guestFiles) func() {
			first := files.open("dir\\save.dat", fileOpenReadWrite)
			files.write(first, "lost")
			return func() { files.close(first) }
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := newFaultStore(t, map[string]string{"fs/save.dat": "stored"})
			session := checkpointFixtureSession(t, archive, store)
			tickSession(t, session, 2)
			closeFiles := arrange(newGuestFiles(t, session.client))
			before, attempts := store.snapshot(t), store.attempts
			_, err := session.CaptureCheckpoint(context.Background())
			if !errors.Is(err, backend.ErrCheckpointSaveWrite) || !strings.Contains(strings.ToLower(err.Error()), "save.dat") {
				t.Fatalf("the quick save = %v, want a refusal that names the file", err)
			}
			if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
				t.Fatalf("the refused quick save wrote to the store: %+v", after)
			}
			// The title's own close stores what it holds, as it always has,
			// and then a quick save is taken.
			closeFiles()
			if _, err := session.CaptureCheckpoint(context.Background()); err != nil {
				t.Fatalf("the quick save after the title closed its file: %v", err)
			}
		})
	}
}

// A list or a database the store refused at the title's own call is given to
// the store again by the next quick save, and a database whose container has
// changed since is not: that would put its older records over newer ones.
func TestCheckpointRetriesWhatTheStoreRefused(t *testing.T) {
	archive := fixtureArchive(t)
	t.Run("the removal list", func(t *testing.T) {
		store := newFaultStore(t, map[string]string{"fs/save.dat": "stored"})
		session := checkpointFixtureSession(t, archive, store)
		files := newGuestFiles(t, session.client)
		store.refuse = true
		callSlot(t, session.client, slotFsRemove, files.name("save.dat"))
		if !session.client.removedUnsaved || store.held("fs/.removed") != "<absent>" {
			t.Fatal("a removal the store refused was not remembered")
		}
		if _, err := session.CaptureCheckpoint(context.Background()); !errors.Is(err, backend.ErrCheckpointSaveWrite) {
			t.Fatalf("a quick save over a list the store still refuses = %v", err)
		}
		store.refuse = false
		roundTripCheckpoint(t, archive, session)
		if store.held("fs/.removed") != "save.dat" || session.client.removedUnsaved {
			t.Fatalf("the quick save left the removal list at %q", store.held("fs/.removed"))
		}
	})
	t.Run("a database", func(t *testing.T) {
		store := newFaultStore(t, nil)
		fixture := newJavaThreadFixture(t, store)
		database := openFixtureDatabase(t, fixture.client, "rank", 4)
		store.refuse = true
		insertFixtureRecord(t, fixture.client, database, []byte{1, 2, 3, 4})
		held := fixture.client.javaRun.databases[database]
		if !held.unsaved {
			t.Fatal("an insert the store refused was not remembered")
		}
		fixture.settle()
		if _, err := fixture.session.CaptureCheckpoint(context.Background()); !errors.Is(err, backend.ErrCheckpointSaveWrite) {
			t.Fatalf("a quick save over a database the store still refuses = %v", err)
		}
		store.refuse = false
		roundTripCheckpoint(t, fixture.archive, fixture.session)
		if held.unsaved || !bytes.Equal([]byte(store.held("fs/rank.db")), held.encode()) {
			t.Fatal("the quick save did not store the database the store had refused")
		}
	})
	t.Run("two objects of one database", func(t *testing.T) {
		store := newFaultStore(t, nil)
		fixture := newJavaThreadFixture(t, store)
		first := openFixtureDatabase(t, fixture.client, "rank", 4)
		second := openFixtureDatabase(t, fixture.client, "rank", 4)
		store.refuse = true
		insertFixtureRecord(t, fixture.client, second, []byte{2, 2, 2, 2})
		insertFixtureRecord(t, fixture.client, first, []byte{1, 1, 1, 1})
		store.refuse = false
		fixture.settle()
		// Storing both would leave whichever was stored last, and the host
		// would be the one choosing which of the title's inserts is lost.
		before, attempts := store.snapshot(t), store.attempts
		_, err := fixture.session.CaptureCheckpoint(context.Background())
		if !errors.Is(err, backend.ErrCheckpointSaveWrite) || !strings.Contains(err.Error(), "rank") {
			t.Fatalf("a quick save with two objects of one database waiting = %v", err)
		}
		if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
			t.Fatal("the refused quick save wrote to the store")
		}
		if !fixture.client.javaRun.databases[first].unsaved || !fixture.client.javaRun.databases[second].unsaved {
			t.Fatal("the refused quick save forgot a write that is still waiting")
		}
	})
	t.Run("a closed database whose container changed since", func(t *testing.T) {
		store := newFaultStore(t, nil)
		fixture := newJavaThreadFixture(t, store)
		first := openFixtureDatabase(t, fixture.client, "rank", 4)
		store.refuse = true
		insertFixtureRecord(t, fixture.client, first, []byte{1, 2, 3, 4})
		if _, err := javaCloseDataBase(fixture.client, context.Background(), fixture.client.thread, []uint32{first}); err != nil {
			t.Fatal(err)
		}
		store.refuse = false
		second := openFixtureDatabase(t, fixture.client, "rank", 4)
		insertFixtureRecord(t, fixture.client, second, []byte{9, 9, 9, 9})
		newer := store.held("fs/rank.db")
		fixture.settle()
		// No call of the title's reaches the closed object again, and its
		// records are older than the container: the quick save neither stores
		// them nor waits for them for the rest of the session.
		roundTripCheckpoint(t, fixture.archive, fixture.session)
		if store.held("fs/rank.db") != newer || fixture.client.javaRun.databases[first].unsaved {
			t.Fatalf("the quick save stored the closed database or kept it waiting: container changed %t", store.held("fs/rank.db") != newer)
		}
	})
	t.Run("a database whose container changed since", func(t *testing.T) {
		store := newFaultStore(t, nil)
		fixture := newJavaThreadFixture(t, store)
		first := openFixtureDatabase(t, fixture.client, "rank", 4)
		store.refuse = true
		insertFixtureRecord(t, fixture.client, first, []byte{1, 2, 3, 4})
		store.refuse = false
		second := openFixtureDatabase(t, fixture.client, "rank", 4)
		insertFixtureRecord(t, fixture.client, second, []byte{9, 9, 9, 9})
		newer := store.held("fs/rank.db")
		fixture.settle()
		_, err := fixture.session.CaptureCheckpoint(context.Background())
		if !errors.Is(err, backend.ErrCheckpointSaveWrite) || !strings.Contains(err.Error(), "rank") || store.held("fs/rank.db") != newer {
			t.Fatalf("a quick save with an older database waiting = %v, container changed: %t", err, store.held("fs/rank.db") != newer)
		}
	})
}

// A quick load displaces the running session, so what that session's title
// has written to a file it has not closed is stored first, and it is what the
// restored title then reads. A store that refuses refuses the load, and the
// running session is the one that still runs, with its write still waiting.
func TestCheckpointLoadStoresTheDisplacedSessionsWrites(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, map[string]string{"fs/progress": "saved", "fs/shared.dat": "stored!"})
	source := checkpointFixtureSession(t, archive, store)
	tickSession(t, source, 4)
	files := newGuestFiles(t, source.client)
	// A file the title has open across the checkpoint, and writes to after it.
	shared := files.open("shared.dat", fileOpenReadWrite)
	checkpoint := roundTripCheckpoint(t, archive, source)
	counted := guestWord(t, source, checkpointCounter)

	tickSession(t, source, 5)
	store.behind(t, "fs/progress", "later")
	files.write(shared, "NEW")
	handle := files.open("unclosed.dat", fileOpenReadWrite)
	files.write(handle, "issued")
	displaced := source.client

	store.refuse = true
	before := store.snapshot(t)
	if _, err := commitFixture(t, archive, checkpoint, source, store); !errors.Is(err, backend.ErrCheckpointSaveWrite) {
		t.Fatalf("a load over a store that refuses = %v", err)
	}
	if source.client != displaced || !displaced.files[handle].dirty || !reflect.DeepEqual(store.snapshot(t), before) {
		t.Fatal("the refused load displaced the session, lost its write or changed the saves")
	}
	tickSession(t, source, 2)
	store.refuse = false

	restored := mustCommitFixture(t, archive, checkpoint, source, store)
	if guestWord(t, restored, checkpointCounter) != counted || source.client != nil {
		t.Fatal("the load did not bring the title back to the checkpoint")
	}
	if restored.client.restoredStorage != nil || restored.client.saveReadError != nil || restored.client.saveStore != backend.SaveStore(store) {
		t.Fatal("the adopted session is still marked as waiting for a store, or is not on it")
	}
	if store.held("fs/progress") != "later" || store.held("fs/unclosed.dat") != "issued" || store.held("fs/shared.dat") != "NEWred!" {
		t.Fatalf("after the load the store holds %q, %q and %q, want the later save and the displaced session's writes",
			store.held("fs/progress"), store.held("fs/unclosed.dat"), store.held("fs/shared.dat"))
	}
	// The file the restored title has open is the file with those writes in
	// it: the store was read again after they were stored.
	if open := restored.client.files[shared]; open == nil || string(open.data) != "NEWred!" || open.cursor != 0 || open.dirty {
		t.Fatalf("the restored title's open file came back as %+v, want what the displaced session stored", open)
	}
	// The restored title reads both as they are now.
	restoredFiles := newGuestFiles(t, restored.client)
	for name, want := range map[string]string{"progress": "later", "unclosed.dat": "issued"} {
		handle := restoredFiles.open(name, fileOpenReadOnly)
		if got := restoredFiles.read(handle, 32); got != want {
			t.Fatalf("the restored title reads %q from %s, want %q", got, name, want)
		}
		restoredFiles.close(handle)
	}
	// What is left of the displaced session cannot reach the store.
	writes := store.attempts
	late := newGuestFiles(t, displaced)
	lateHandle := late.open("after.dat", fileOpenReadWrite)
	late.write(lateHandle, "late")
	displaced.flushOpenFiles()
	if store.attempts != writes || store.held("fs/after.dat") != "<absent>" {
		t.Fatal("the displaced session wrote into the saves after the load")
	}
}

// An open file is the file as the store has it when the load commits, found
// the way an open finds it, with the cursor where the record has it.
func TestCheckpointRebuildsAnOpenFileFromTheStore(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, map[string]string{
		"fs/grown.dat": "0123456789", "fs/shrunk.dat": "0123456789", "fs/removed.dat": "0123456789",
		"fs/gone.dat": "0123456789", "fs/twice.dat": "0123456789",
	})
	source := checkpointFixtureSession(t, archive, store)
	tickSession(t, source, 2)
	files := newGuestFiles(t, source.client)
	handles := map[string]uint32{}
	for _, name := range []string{"grown.dat", "shrunk.dat", "removed.dat", "gone.dat"} {
		handles[name] = files.open(name, fileOpenReadWrite)
		callSlot(t, source.client, slotFsSeek, handles[name], 6, 0)
	}
	packaged := files.open("data/hello.txt", fileOpenReadOnly)
	shipped := files.read(packaged, 2)
	writer, reader := files.open("twice.dat", fileOpenReadWrite), files.open("twice.dat", fileOpenReadOnly)
	checkpoint := roundTripCheckpoint(t, archive, source)

	store.behind(t, "fs/grown.dat", "0123456789ABCDEF")
	store.behind(t, "fs/shrunk.dat", "0123")
	store.behind(t, "fs/.removed", "removed.dat")
	store.behind(t, "fs/twice.dat", "abcdefgh")
	entries := store.snapshot(t)
	for index, entry := range entries {
		if entry.Key == "fs/gone.dat" {
			entries = append(entries[:index], entries[index+1:]...)
			break
		}
	}
	if err := store.ReplaceSaves(entries); err != nil {
		t.Fatal(err)
	}
	before, attempts := store.snapshot(t), store.attempts

	restored := mustCommitFixture(t, archive, checkpoint, source, store)
	if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
		t.Fatalf("the load wrote to the store or made a file: %+v", after)
	}
	client := restored.client
	guest := newGuestFiles(t, client)
	// A file that grew: the cursor is where it was and reading goes on into
	// what the file holds now.
	if got := guest.read(handles["grown.dat"], 8); got != "6789ABCD" {
		t.Fatalf("the grown file reads %q from the recorded cursor", got)
	}
	// A file that shrank below the cursor: the cursor stays, a read there
	// answers nothing, and a write fills the gap with zeros.
	if open := client.files[handles["shrunk.dat"]]; open.cursor != 6 || string(open.data) != "0123" || guest.read(handles["shrunk.dat"], 4) != "" {
		t.Fatalf("the shrunk file came back as %+v", open)
	}
	guest.write(handles["shrunk.dat"], "XY")
	guest.close(handles["shrunk.dat"])
	if store.held("fs/shrunk.dat") != "0123\x00\x00XY" {
		t.Fatalf("a write past the end of the shrunk file stored %q", store.held("fs/shrunk.dat"))
	}
	// A file the title removed and one that is simply gone: an empty handle
	// at its cursor, and nothing made until the title writes.
	for _, name := range []string{"removed.dat", "gone.dat"} {
		if open := client.files[handles[name]]; open.cursor != 6 || len(open.data) != 0 || open.dirty || guest.exists(name) {
			t.Fatalf("%s came back as %+v, or the load says it exists", name, open)
		}
		guest.close(handles[name])
	}
	if store.held("fs/gone.dat") != "<absent>" || store.held("fs/.removed") != "removed.dat" {
		t.Fatal("closing a handle on a file that is gone made the file")
	}
	// A resource the package ships: the same bytes, read on from the cursor.
	if got, _ := client.archive.Resource("data/hello.txt"); guest.read(packaged, 2) != string(got[2:4]) || shipped != string(got[:2]) {
		t.Fatal("the packaged file was not read on from its cursor")
	}
	// Two handles on one file: each has the file as it is now, in a buffer of
	// its own, and a write through one leaves the other alone.
	if &client.files[writer].data[0] == &client.files[reader].data[0] {
		t.Fatal("two handles on one file share a buffer")
	}
	guest.write(writer, "ZZ")
	if got := guest.read(reader, 8); got != "abcdefgh" {
		t.Fatalf("a write through one handle reached the other: %q", got)
	}
	guest.close(writer)
	if store.held("fs/twice.dat") != "ZZcdefgh" {
		t.Fatalf("the write stored %q, want only its own bytes changed", store.held("fs/twice.dat"))
	}
}

// A handle opened on an empty file holds only what the title wrote through it.
// A load gives it back at most that much, so what the restored title writes is
// never followed by the tail of a file it did not write; and neither a quick
// save nor a load carries out an emptying the title has not written through.
func TestCheckpointNeverHandsAnEmptiedHandleANewerTail(t *testing.T) {
	archive := fixtureArchive(t)
	t.Run("emptied and part written", func(t *testing.T) {
		store := newFaultStore(t, map[string]string{"fs/save.dat": "the earlier save"})
		source := checkpointFixtureSession(t, archive, store)
		files := newGuestFiles(t, source.client)
		handle := files.open("save.dat", fileOpenWriteTruncate)
		files.write(handle, "HEAD")
		checkpoint := roundTripCheckpoint(t, archive, source)
		if store.held("fs/save.dat") != "HEAD" {
			t.Fatalf("the quick save stored %q, want the part the title had written", store.held("fs/save.dat"))
		}
		want := []fileState{{Handle: handle, Name: []byte("save.dat"), Cursor: 4, Writable: true, Truncated: true, Length: 4}}
		if saved := decodeRuntime(t, checkpoint); !reflect.DeepEqual(saved.Client.Files, want) {
			t.Fatalf("the record holds the files %+v, want %+v", saved.Client.Files, want)
		}
		// The running title finishes its save, and a longer one follows it.
		files.write(handle, "-finished")
		files.close(handle)
		store.behind(t, "fs/save.dat", "LATE-a much longer later save")

		restored := mustCommitFixture(t, archive, checkpoint, source, store)
		open := restored.client.files[handle]
		if open == nil || !open.truncated || string(open.data) != "LATE" || open.cursor != 4 {
			t.Fatalf("the emptied handle came back as %+v, want the four bytes it had written, as the file has them now", open)
		}
		guest := newGuestFiles(t, restored.client)
		guest.write(handle, "-body")
		guest.close(handle)
		if store.held("fs/save.dat") != "LATE-body" {
			t.Fatalf("the restored title's save is %q, want no tail of the later one", store.held("fs/save.dat"))
		}
	})
	t.Run("emptied and not yet written", func(t *testing.T) {
		store := newFaultStore(t, map[string]string{"fs/save.dat": "the earlier save"})
		source := checkpointFixtureSession(t, archive, store)
		files := newGuestFiles(t, source.client)
		handle := files.open("save.dat", fileOpenWriteTruncate)
		attempts := store.attempts
		checkpoint := roundTripCheckpoint(t, archive, source)
		if store.attempts != attempts || store.held("fs/save.dat") != "the earlier save" {
			t.Fatal("the quick save emptied a file the title had only opened")
		}
		restored := mustCommitFixture(t, archive, checkpoint, source, store)
		open := restored.client.files[handle]
		if open == nil || !open.truncated || len(open.data) != 0 || store.held("fs/save.dat") != "the earlier save" {
			t.Fatalf("the load emptied the file or filled the handle: %+v", open)
		}
		// Closed without a write, the file is what it was; that is what this
		// platform's own close does with such a handle.
		newGuestFiles(t, restored.client).close(handle)
		if store.held("fs/save.dat") != "the earlier save" {
			t.Fatalf("closing the unwritten handle left %q", store.held("fs/save.dat"))
		}
	})
}

// A handle whose open made the file is an ordinary handle. The title did not
// ask for an empty file: it asked for the file, and there was none. After a
// load it holds the file as it is now, so what the running title appended
// after the checkpoint is still there behind what the restored title rewrites.
// A title that keeps its save open for a whole session is in exactly this
// position on its first run.
func TestCheckpointGivesAHandleThatMadeItsFileTheFileAsItIsNow(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, nil)
	source := checkpointFixtureSession(t, archive, store)
	files := newGuestFiles(t, source.client)
	handle := files.open("save.dat", fileOpenReadWrite)
	files.write(handle, "SLOT1aaa")
	checkpoint := roundTripCheckpoint(t, archive, source)
	if store.held("fs/save.dat") != "SLOT1aaa" {
		t.Fatalf("the quick save stored %q", store.held("fs/save.dat"))
	}
	// The running title saves a second slot behind the first, through the
	// handle it has kept. The load stores that write before anything else.
	files.write(handle, "SLOT2bbb")

	restored := mustCommitFixture(t, archive, checkpoint, source, store)
	if store.held("fs/save.dat") != "SLOT1aaaSLOT2bbb" {
		t.Fatalf("after the load the store holds %q", store.held("fs/save.dat"))
	}
	open := restored.client.files[handle]
	if open == nil || open.truncated || string(open.data) != "SLOT1aaaSLOT2bbb" || open.cursor != 8 {
		t.Fatalf("the handle came back as %+v, want the whole file as it is now and the cursor it had", open)
	}
	// The restored title rewrites its first slot in place and closes.
	guest := newGuestFiles(t, restored.client)
	if moved := int32(callSlot(t, restored.client, slotFsSeek, handle, 0, 0)); moved < 0 {
		t.Fatalf("seek answered %d", moved)
	}
	guest.write(handle, "SLOT1ccc")
	guest.close(handle)
	if store.held("fs/save.dat") != "SLOT1cccSLOT2bbb" {
		t.Fatalf("the save is %q: the slot saved after the quick save is gone", store.held("fs/save.dat"))
	}
}

// The two path lists are the store's own after a load: a path removed and one
// created after the checkpoint are seen, and the next change writes the list
// as it is now with that one change.
func TestCheckpointRereadsThePathLists(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, map[string]string{"fs/kept.dat": "kept", "fs/later-removed.dat": "x", "fs/.created": "kept.dat\nlater-removed.dat"})
	source := checkpointFixtureSession(t, archive, store)
	files := newGuestFiles(t, source.client)
	if !files.exists("later-removed.dat") || len(source.client.removedFiles()) != 0 {
		t.Fatal("the fixture's lists are not loaded as the store has them")
	}
	checkpoint := roundTripCheckpoint(t, archive, source)
	if saved := decodeRuntime(t, checkpoint); bytes.Contains(checkpoint.Runtime, []byte("later-removed")) || len(saved.Client.Files) != 0 {
		t.Fatal("the record carries a path list")
	}
	store.behind(t, "fs/.removed", "later-removed.dat")
	store.behind(t, "fs/.created", "kept.dat\nlater-created.dat\nlater-removed.dat")
	store.behind(t, "fs/later-created.dat", "new")

	restored := mustCommitFixture(t, archive, checkpoint, source, store)
	guest := newGuestFiles(t, restored.client)
	if guest.exists("later-removed.dat") || !guest.exists("later-created.dat") || !guest.exists("kept.dat") {
		t.Fatal("the restored title's view of its files is not the store's")
	}
	if listed := restored.client.listDirectory(""); !reflect.DeepEqual(listed, append(restored.client.listDirectory("")[:0:0], listed...)) || !containsName(listed, "later-created.dat") || containsName(listed, "later-removed.dat") {
		t.Fatalf("the restored title lists %q", listed)
	}
	callSlot(t, restored.client, slotFsRemove, guest.name("kept.dat"))
	if store.held("fs/.removed") != "kept.dat\nlater-removed.dat" {
		t.Fatalf("the next removal wrote the list %q, want the list as it was with one name more", store.held("fs/.removed"))
	}
}

func containsName(names []string, name string) bool {
	for _, entry := range names {
		if strings.EqualFold(entry, name) {
			return true
		}
	}
	return false
}

// A save that cannot be read refuses the load before anything is stored, the
// displaced session's own unstored write included: the saves are read first.
// The running session is the one that still runs, and the same load is
// accepted once the save can be read.
func TestCheckpointLoadIsRefusedWhenASaveCannotBeRead(t *testing.T) {
	archive := fixtureArchive(t)
	for _, key := range []string{"fs/open.dat", fileRemovedKey, fileCreatedKey} {
		t.Run(key, func(t *testing.T) {
			store := newFaultStore(t, map[string]string{"fs/open.dat": "stored"})
			source := checkpointFixtureSession(t, archive, store)
			tickSession(t, source, 2)
			files := newGuestFiles(t, source.client)
			handle := files.open("open.dat", fileOpenReadWrite)
			checkpoint := roundTripCheckpoint(t, archive, source)
			pending := files.open("pending.dat", fileOpenReadWrite)
			files.write(pending, "issued")
			client, before, attempts := source.client, store.snapshot(t), store.attempts

			store.unreadable = key
			_, err := commitFixture(t, archive, checkpoint, source, store)
			store.unreadable = ""
			if !errors.Is(err, backend.ErrCheckpointSaveRead) {
				t.Fatalf("a load over an unreadable %s = %v", key, err)
			}
			if source.client != client || !client.files[pending].dirty || client.saveReadError != nil || !reflect.DeepEqual(store.snapshot(t), before) || store.attempts != attempts {
				t.Fatal("the refused load displaced the session, stored its write or left it unable to save")
			}
			tickSession(t, source, 2)
			restored := mustCommitFixture(t, archive, checkpoint, source, store)
			if open := restored.client.files[handle]; open == nil || string(open.data) != "stored" || restored.client.saveReadError != nil {
				t.Fatalf("the accepted load did not read the open file: %+v", open)
			}
			if store.held("fs/pending.dat") != "issued" {
				t.Fatal("the accepted load did not store the displaced session's write")
			}
		})
	}
}

// On a directory store a file can have become a directory behind a slot. The
// load is refused for the read and the tree is what it was.
func TestCheckpointLoadRefusesADirectoryWhereAFileWas(t *testing.T) {
	archive := fixtureArchive(t)
	root := filepath.Join(t.TempDir(), "owner")
	store := backend.NewDirectorySaveStore(root)
	source := checkpointFixtureSession(t, archive, store)
	files := newGuestFiles(t, source.client)
	handle := files.open("save.dat", fileOpenReadWrite)
	files.write(handle, "saved")
	checkpoint := roundTripCheckpoint(t, archive, source)
	file := filepath.Join(root, "fs", "save.dat")
	if data, err := os.ReadFile(file); err != nil || string(data) != "saved" {
		t.Fatalf("the quick save left %q in the save file, %v", data, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(file, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(file, "below"), []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := commitFixture(t, archive, checkpoint, source, store); !errors.Is(err, backend.ErrCheckpointSaveRead) {
		t.Fatalf("a load over a directory where the save was = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(file, "below")); err != nil || string(data) != "kept" || source.client == nil {
		t.Fatalf("the refused load changed the tree or displaced the session: %q, %v", data, err)
	}
}

// lateCancel is a context that is cancelled from its second question on, which
// is where Commit asks again: after the saves were read and the displaced
// session's writes stored, and before anything is adopted.
type lateCancel struct {
	context.Context
	asked int
}

func (ctx *lateCancel) Err() error {
	ctx.asked++
	if ctx.asked > 1 {
		return context.Canceled
	}
	return nil
}

// A load cancelled after its saves were read adopts nothing and leaves the
// restored client off the store, so what is discarded cannot reach a save. The
// same prepared load can still be committed.
func TestCheckpointLoadCancelledAfterTheSavesWereRead(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, map[string]string{"fs/open.dat": "stored"})
	source := checkpointFixtureSession(t, archive, store)
	files := newGuestFiles(t, source.client)
	handle := files.open("open.dat", fileOpenReadWrite)
	checkpoint := roundTripCheckpoint(t, archive, source)
	prepared, err := PrepareSessionCheckpoint(archive, checkpoint, SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restoredClient, running := prepared.session.client, source.client
	if _, err := prepared.Commit(&lateCancel{Context: context.Background()}, source, store); !errors.Is(err, context.Canceled) {
		t.Fatalf("a load cancelled late = %v", err)
	}
	if restoredClient.saveStore != backend.SaveStore(prepared.placeholder) || restoredClient.restoredStorage == nil {
		t.Fatal("the cancelled load left the restored client on the store")
	}
	if source.client != running || running.saveStore != backend.SaveStore(store) {
		t.Fatal("the cancelled load changed the running session")
	}
	store.behind(t, "fs/open.dat", "changed since")
	restored, err := prepared.Commit(context.Background(), source, store)
	if err != nil {
		t.Fatalf("the load could not be committed after it was cancelled once: %v", err)
	}
	defer restored.Close(context.Background())
	if open := restored.client.files[handle]; open == nil || string(open.data) != "changed since" {
		t.Fatalf("the second commit did not read the file again: %+v", open)
	}
}

// What a load reads is bounded: the names come from a slot, and every object
// counts towards one limit, in a buffer of its own, with one read of the file.
func TestCheckpointRebuildIsBoundedAndReadsEachSaveOnce(t *testing.T) {
	archive := fixtureArchive(t)
	store := newFaultStore(t, map[string]string{"fs/big.dat": strings.Repeat("x", 16)})
	source := checkpointFixtureSession(t, archive, store)
	files := newGuestFiles(t, source.client)
	for range 4 {
		files.open("big.dat", fileOpenReadOnly)
	}
	checkpoint := roundTripCheckpoint(t, archive, source)
	commit := func(budget uint64) (*Session, int, error) {
		prepared, err := PrepareSessionCheckpoint(archive, checkpoint, SessionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Discard()
		prepared.session.client.restoredStorage.budget = budget
		reads := store.reads
		restored, err := prepared.Commit(context.Background(), nil, store)
		if restored != nil {
			t.Cleanup(func() { _ = restored.Close(context.Background()) })
		}
		return restored, store.reads - reads, err
	}
	restored, reads, err := commit(64)
	if err != nil {
		t.Fatalf("four handles of sixteen bytes on a budget of sixty-four: %v", err)
	}
	// The file once and the two lists: three reads for four handles.
	if reads != 3 || len(restored.client.files) != 4 {
		t.Fatalf("four handles on one file made %d store reads", reads)
	}
	if _, _, err := commit(63); !errors.Is(err, backend.ErrCheckpointSaveRead) {
		t.Fatalf("four handles of sixteen bytes on a budget of sixty-three = %v", err)
	}
	if _, _, err := commit(15); !errors.Is(err, backend.ErrCheckpointSaveRead) {
		t.Fatalf("a file larger than the budget = %v", err)
	}
}

// A store that only answers one key at a time is enough for a quick save and
// for a quick load, and a record of another version or setting is told apart
// from a damaged one.
func TestCheckpointNeedsOnlyAPlainStoreAndRefusesOtherVersions(t *testing.T) {
	archive := fixtureArchive(t)
	store := newMemorySaveStore()
	source := checkpointFixtureSession(t, archive, store)
	files := newGuestFiles(t, source.client)
	handle := files.open("save.dat", fileOpenReadWrite)
	files.write(handle, "saved")
	checkpoint := roundTripCheckpoint(t, archive, source)
	if string(store.entries["fs/save.dat"]) != "saved" {
		t.Fatal("the quick save over a plain store did not store the unclosed write")
	}
	restored := mustCommitFixture(t, archive, checkpoint, source, store)
	if open := restored.client.files[handle]; open == nil || string(open.data) != "saved" {
		t.Fatalf("the load over a plain store came back with %+v", open)
	}

	saved := decodeRuntime(t, checkpoint)
	for name, test := range map[string]struct {
		mutate  func(*sessionCheckpointState)
		options SessionOptions
		reason  string
	}{
		"an earlier session record": {mutate: func(s *sessionCheckpointState) { s.Version = 1 }, reason: "version 1"},
		"a later session record":    {mutate: func(s *sessionCheckpointState) { s.Version = 3 }, reason: "version 3"},
		"another tick":              {mutate: func(*sessionCheckpointState) {}, options: SessionOptions{Tick: 7}, reason: "another tick"},
	} {
		changed := saved
		test.mutate(&changed)
		bad := checkpoint
		var err error
		if bad.Runtime, err = backend.EncodeCheckpointRecord(changed); err != nil {
			t.Fatal(err)
		}
		prepared, err := PrepareSessionCheckpoint(archive, bad, test.options)
		if prepared != nil {
			prepared.Discard()
		}
		if !errors.Is(err, backend.ErrCheckpointVersion) || !strings.Contains(err.Error(), test.reason) {
			t.Errorf("%s was refused with %v", name, err)
		}
	}
	for name, mutate := range map[string]func(*sessionCheckpointState){
		"an earlier client record":  func(s *sessionCheckpointState) { s.Client.Version = 1 },
		"an earlier adapter record": func(s *sessionCheckpointState) { s.Adapters.Version = 1 },
		"an emptied read-only handle": func(s *sessionCheckpointState) {
			s.Client.Files[0].Truncated, s.Client.Files[0].Writable = true, false
		},
		"a length with no emptying open": func(s *sessionCheckpointState) { s.Client.Files[0].Length = 5 },
		"a length below zero": func(s *sessionCheckpointState) {
			s.Client.Files[0].Truncated, s.Client.Files[0].Length = true, -1
		},
		"a length past the bound": func(s *sessionCheckpointState) {
			s.Client.Files[0].Truncated, s.Client.Files[0].Length = true, maxStateBytes+1
		},
	} {
		changed := saved
		changed.Client.Files = append([]fileState(nil), saved.Client.Files...)
		mutate(&changed)
		bad := checkpoint
		var err error
		if bad.Runtime, err = backend.EncodeCheckpointRecord(changed); err != nil {
			t.Fatal(err)
		}
		prepared, err := PrepareSessionCheckpoint(archive, bad, SessionOptions{})
		if prepared != nil {
			prepared.Discard()
		}
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// File.write(int) at a cursor past the end fills the gap with zeros, as the
// array forms of the call do. Only a file that is shorter after a load than it
// was before one puts a Java title's cursor there.
func TestJavaFileWriteByteFillsAGapWithZeros(t *testing.T) {
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123456789"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object, err := newTestObject(t, client, javaFileClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaFileOpen(client, context.Background(), client.thread, []uint32{object, newTestString(t, client, "save.dat"), 4, 0}); err != nil {
		t.Fatal(err)
	}
	_, open, err := client.javaFileHandle(object)
	if err != nil {
		t.Fatal(err)
	}
	open.data, open.cursor = []byte("01"), 5
	if written, err := javaFileWriteByte(client, context.Background(), client.thread, []uint32{object, 'Z'}); err != nil || written != 1 {
		t.Fatalf("write(int) past the end = %d, %v", written, err)
	}
	if string(open.data) != "01\x00\x00\x00Z" || open.cursor != 6 || !open.dirty {
		t.Fatalf("write(int) past the end left %q at %d", open.data, open.cursor)
	}
	// At the end and inside the file it does what it always did.
	if _, err := javaFileWriteByte(client, context.Background(), client.thread, []uint32{object, 'A'}); err != nil || string(open.data) != "01\x00\x00\x00ZA" {
		t.Fatalf("write(int) at the end left %q, %v", open.data, err)
	}
	open.cursor = 0
	if _, err := javaFileWriteByte(client, context.Background(), client.thread, []uint32{object, 'B'}); err != nil || string(open.data) != "B1\x00\x00\x00ZA" {
		t.Fatalf("write(int) inside the file left %q, %v", open.data, err)
	}
}

// A stream opened on a File is a window on that file, and a load takes the
// window again from the file as the store has it. What the title had written
// into an output stream and not flushed is the title's own and comes back with
// it: it lands at the File's cursor, in the file as it is now, when the
// restored title flushes.
func TestCheckpointRebuildsFileStreamsFromTheStore(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123456789", "fs/kept.dat": "0123456789", "fs/short.dat": "0123456789"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	openFile := func(name string) uint32 {
		t.Helper()
		object, err := newTestObject(t, client, javaFileClass)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := javaFileOpen(client, ctx, client.thread, []uint32{object, newTestString(t, client, name), 4, 0}); err != nil {
			t.Fatal(err)
		}
		return object
	}
	openStream := func(file uint32, cursor, read int) uint32 {
		t.Helper()
		_, open, err := client.javaFileHandle(file)
		if err != nil {
			t.Fatal(err)
		}
		open.cursor = cursor
		stream, err := javaFileOpenInputStream(client, ctx, client.thread, []uint32{file})
		if err != nil {
			t.Fatal(err)
		}
		for range read {
			if _, err := javaStreamRead(client, ctx, client.thread, []uint32{stream}); err != nil {
				t.Fatal(err)
			}
		}
		return stream
	}
	// A File with a stream in and a stream out, and bytes waiting in the
	// stream out.
	file := openFile("save.dat")
	stream := openStream(file, 2, 3)
	sink, err := javaFileOpenOutputStream(client, ctx, client.thread, []uint32{file})
	if err != nil {
		t.Fatal(err)
	}
	client.javaRun.sinks[sink] = []byte("MARKER-pending")
	// A stream whose File the title closed first, and one over a file that
	// will be shorter than the stream has read.
	closedFile := openFile("kept.dat")
	orphan := openStream(closedFile, 4, 1)
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{closedFile}); err != nil {
		t.Fatal(err)
	}
	short := openStream(openFile("short.dat"), 1, 6)
	fixture.settle()

	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	saved := decodeRuntime(t, checkpoint)
	for _, record := range saved.Client.Java.Streams {
		if record.File && (record.Data != nil || record.Archive != 0) {
			t.Fatalf("a file stream was recorded with bytes: %+v", record)
		}
	}
	if !bytes.Contains(checkpoint.Runtime, []byte(jsonBytesOf(t, "MARKER-pending"))) || bytes.Contains(checkpoint.Runtime, []byte(jsonBytesOf(t, "23456789"))) {
		t.Fatal("the record lost the bytes waiting in the stream, or carries a file stream's window")
	}
	store.behind(t, "fs/save.dat", "ABCDEFGHIJKLMNOP")
	store.behind(t, "fs/kept.dat", "abcdefghij")
	store.behind(t, "fs/short.dat", "xyz")
	before, attempts := store.snapshot(t), store.attempts

	restored := mustCommitFixture(t, fixture.archive, checkpoint, fixture.session, store)
	if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
		t.Fatalf("the load wrote to the store: %+v", after)
	}
	copied := restored.client
	if held := copied.javaRun.streams[stream]; string(held.Data) != "CDEFGHIJKLMNOP" || held.Read != 3 || !held.File || held.Offset != 2 {
		t.Fatalf("the stream on the open File came back as %+v", held)
	}
	if value, err := javaStreamRead(copied, ctx, copied.thread, []uint32{stream}); err != nil || value != 'F' {
		t.Fatalf("the stream reads %q on from where it was, %v", value, err)
	}
	if held := copied.javaRun.streams[orphan]; string(held.Data) != "efghij" || held.Read != 1 {
		t.Fatalf("the stream whose File was closed came back as %+v", held)
	}
	// What was read of a longer window is all of a shorter one.
	if held := copied.javaRun.streams[short]; string(held.Data) != "yz" || held.Read != 2 {
		t.Fatalf("the stream on the shorter file came back as %+v", held)
	}
	if value, err := javaStreamRead(copied, ctx, copied.thread, []uint32{short}); err != nil || value != ^uint32(0) {
		t.Fatalf("a read at the end of the shorter window = %d, %v", int32(value), err)
	}
	// The waiting bytes go in at the File's cursor, in the file as it is now.
	if string(copied.javaRun.sinks[sink]) != "MARKER-pending" {
		t.Fatalf("the stream out came back holding %q", copied.javaRun.sinks[sink])
	}
	if _, err := javaByteSinkFlush(copied, ctx, copied.thread, []uint32{sink}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaFileClose(copied, ctx, copied.thread, []uint32{file}); err != nil {
		t.Fatal(err)
	}
	if store.held("fs/save.dat") != "ABMARKER-pending" {
		t.Fatalf("the flushed stream stored %q", store.held("fs/save.dat"))
	}
}

// jsonBytesOf is a string the way a checkpoint record writes a byte slice
// holding it.
func jsonBytesOf(t *testing.T, text string) string {
	t.Helper()
	encoded, err := backend.EncodeCheckpointRecord(struct{ Data []byte }{[]byte(text)})
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.IndexByte(encoded, ':') + 2
	return string(encoded[start:bytes.LastIndexByte(encoded, '"')])
}

// A DataBase object the title has open is the database as the store has it
// after a load: its records are the container's. A close that changed nothing
// writes nothing, so it cannot bring back a container the title deleted, and a
// container this runtime does not read refuses the load.
func TestCheckpointRebuildsAnOpenDataBaseFromTheStore(t *testing.T) {
	ctx := context.Background()
	start := func(t *testing.T) (*faultStore, *javaThreadFixture, uint32, backend.Checkpoint) {
		store := newFaultStore(t, nil)
		fixture := newJavaThreadFixture(t, store)
		database := openFixtureDatabase(t, fixture.client, "save", 16)
		insertFixtureRecord(t, fixture.client, database, []byte("MARKER-first"))
		fixture.settle()
		checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
		if bytes.Contains(checkpoint.Runtime, []byte(jsonBytesOf(t, "MARKER-first"))) || bytes.Contains(checkpoint.Runtime, []byte("MARKER-first")) {
			t.Fatal("the record carries the database's records")
		}
		return store, fixture, database, checkpoint
	}
	update := func(t *testing.T, client *Client, database, identifier uint32, record string) error {
		t.Helper()
		array, err := client.newJavaByteArray([]byte(record))
		if err != nil {
			t.Fatal(err)
		}
		_, err = javaUpdateRecord(client, ctx, client.thread, []uint32{database, identifier, array})
		return err
	}
	t.Run("the container changed", func(t *testing.T) {
		store, fixture, database, checkpoint := start(t)
		// The running title saves through a second object on the database.
		second := openFixtureDatabase(t, fixture.client, "save", 16)
		if err := update(t, fixture.client, second, 0, "second"); err != nil {
			t.Fatal(err)
		}
		insertFixtureRecord(t, fixture.client, second, []byte("third"))
		// It also has a file open with a write the store does not have, so
		// the load stores that and then reads the saves a second time.
		files := newGuestFiles(t, fixture.client)
		files.write(files.open("unclosed.dat", fileOpenReadWrite), "issued")
		newer, attempts := store.held("fs/save.db"), store.attempts

		restored := mustCommitFixture(t, fixture.archive, checkpoint, fixture.session, store)
		client := restored.client
		if store.held("fs/save.db") != newer || store.attempts != attempts+1 || store.held("fs/unclosed.dat") != "issued" {
			t.Fatal("the load changed the container, or stored something other than the displaced session's write")
		}
		if got := databaseRecord(t, client, database, 0); string(got) != "second" {
			t.Fatalf("record 0 after the load is %q, want the one saved after the checkpoint", got)
		}
		if count, _ := javaDataBaseRecordCount(client, ctx, client.thread, []uint32{database}); count != 2 {
			t.Fatalf("the restored database counts %d records", count)
		}
		// An update changes the record it names, in the container as it is.
		if err := update(t, client, database, 0, "fourth"); err != nil {
			t.Fatal(err)
		}
		want := &javaDatabase{name: "save", recordSize: 16, records: [][]byte{[]byte("fourth"), []byte("third")}, deleted: []bool{false, false}}
		if store.held("fs/save.db") != string(want.encode()) {
			t.Fatal("the update did not land on the container as it was")
		}
	})
	t.Run("a close that changed nothing", func(t *testing.T) {
		store, fixture, database, checkpoint := start(t)
		restored := mustCommitFixture(t, fixture.archive, checkpoint, fixture.session, store)
		attempts := store.attempts
		if _, err := javaCloseDataBase(restored.client, ctx, restored.client.thread, []uint32{database}); err != nil {
			t.Fatal(err)
		}
		if store.attempts != attempts {
			t.Fatal("closing a database the load read, and nothing changed, wrote to the store")
		}
	})
	t.Run("the title deleted it since", func(t *testing.T) {
		store, fixture, database, checkpoint := start(t)
		name := newTestString(t, fixture.client, "save")
		if _, err := javaDeleteDataBase(fixture.client, ctx, fixture.client.thread, []uint32{name}); err != nil {
			t.Fatal(err)
		}
		before := store.snapshot(t)
		restored := mustCommitFixture(t, fixture.archive, checkpoint, fixture.session, store)
		client := restored.client
		if count, _ := javaDataBaseRecordCount(client, ctx, client.thread, []uint32{database}); count != 0 {
			t.Fatalf("the object on a deleted database counts %d records", count)
		}
		// A record it remembers is gone, and the answer is the exception the
		// specification gives for one.
		if _, err := javaSelectRecord(client, ctx, client.thread, []uint32{database, 0}); err == nil || !strings.Contains(err.Error(), "DataBaseRecordException") {
			t.Fatalf("a record of a deleted database = %v", err)
		}
		if err := update(t, client, database, 0, "again"); err == nil {
			t.Fatal("an update of a record that is gone was accepted")
		}
		if _, err := javaCloseDataBase(client, ctx, client.thread, []uint32{database}); err != nil {
			t.Fatal(err)
		}
		if after := store.snapshot(t); !reflect.DeepEqual(after, before) {
			t.Fatalf("the load, the refused update or the close brought the deleted database back: %+v", after)
		}
	})
	t.Run("a container this runtime does not read", func(t *testing.T) {
		store, fixture, _, checkpoint := start(t)
		store.behind(t, "fs/save.db", "not a container")
		before, client := store.snapshot(t), fixture.session.client
		if _, err := commitFixture(t, fixture.archive, checkpoint, fixture.session, store); !errors.Is(err, backend.ErrCheckpointSaveRead) || !strings.Contains(err.Error(), "save") {
			t.Fatalf("a load over an undecodable container = %v", err)
		}
		if fixture.session.client != client || !reflect.DeepEqual(store.snapshot(t), before) {
			t.Fatal("the refused load displaced the session or changed the saves")
		}
	})
}

// A database that no load has rebuilt is stored at its close, as it always
// was: only a rebuilt one that has not changed since is left alone.
func TestJavaDataBaseCloseStoresUnlessALoadFoundItUnchanged(t *testing.T) {
	store := newFaultStore(t, nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	database := openFixtureDatabase(t, client, "save", 8)
	attempts := store.attempts
	if _, err := javaCloseDataBase(client, context.Background(), client.thread, []uint32{database}); err != nil {
		t.Fatal(err)
	}
	if store.attempts != attempts+1 {
		t.Fatalf("an ordinary close asked the store %d times, want once", store.attempts-attempts)
	}
	// A second close is ignored, as the specification says.
	if _, err := javaCloseDataBase(client, context.Background(), client.thread, []uint32{database}); err != nil || store.attempts != attempts+1 {
		t.Fatalf("a second close = %v after %d writes", err, store.attempts-attempts)
	}
}

// What an authentication adapter read from its store it reads again from the
// store a restored session runs over, and what it made for the run comes with
// the record.
func TestCheckpointAdaptersDeriveDiskStateAgain(t *testing.T) {
	record := func(t *testing.T, client *Client) adapterState {
		t.Helper()
		saved, _, err := client.captureAdapters()
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := backend.EncodeCheckpointRecord(saved)
		if err != nil {
			t.Fatal(err)
		}
		var decoded adapterState
		if err := backend.DecodeCheckpointRecord(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	t.Run("the options adapter's hidden word", func(t *testing.T) {
		options := func(word uint32) string {
			data := make([]byte, 56)
			binary.LittleEndian.PutUint32(data[40:], word)
			return string(data)
		}
		store := newFaultStore(t, map[string]string{authenticationOptionsKey: options(7)})
		source := fixtureClient(t)
		source.saveStore = newAuthenticationOptionStore(store, source.archive)
		saved := record(t, source)
		if saved.Store != adapterStoreOptions || saved.Certificate != nil {
			t.Fatalf("the options adapter was recorded as %+v", saved)
		}
		store.behind(t, authenticationOptionsKey, options(9))
		restored := fixtureClient(t)
		reads := store.reads
		if err := restored.attachAdapters(restored.archive, saved, store); err != nil || store.reads != reads+1 || store.attempts != 0 {
			t.Fatalf("attach = %v after %d reads and %d writes", err, store.reads-reads, store.attempts)
		}
		view, _ := restored.saveStore.LoadSave(authenticationOptionsKey)
		if binary.LittleEndian.Uint32(view[40:]) != 1 {
			t.Fatal("the restored adapter does not hide the word")
		}
		view[0] = 5
		if err := restored.saveStore.StoreSave(authenticationOptionsKey, view); err != nil {
			t.Fatal(err)
		}
		// The word it puts back is the one the file has now.
		if held := []byte(store.held(authenticationOptionsKey)); held[0] != 5 || binary.LittleEndian.Uint32(held[40:]) != 9 {
			t.Fatalf("the title's write stored the word %d", binary.LittleEndian.Uint32(held[40:]))
		}
		store.unreadable = authenticationOptionsKey
		if err := fixtureClient(t).attachAdapters(restored.archive, saved, store); err == nil {
			t.Fatal("an options file that cannot be read was accepted")
		}
	})
	// The 58-byte adapter keeps whatever the title last stored under the
	// certificate's name, at whatever length: empty when an open made the
	// file, short while a write is half done. A record takes it as it is.
	t.Run("the 58-byte adapter's certificate at any length", func(t *testing.T) {
		for _, private := range [][]byte{{}, []byte("half"), bytes.Repeat([]byte{0x5a}, 58), bytes.Repeat([]byte{0x33}, 90)} {
			store := newFaultStore(t, nil)
			adapter, ok := newAuthenticationCertificate58Store(store, nil, "12")
			if !ok {
				t.Fatal("the fixture adapter was not built")
			}
			if err := adapter.StoreSave(authenticationCertificate58Key, private); err != nil {
				t.Fatal(err)
			}
			source := fixtureClient(t)
			source.saveStore = adapter
			saved := record(t, source)
			if saved.Store != adapterStoreCertificate58 || !bytes.Equal(saved.Certificate, private) {
				t.Fatalf("a certificate of %d bytes was recorded as %+v", len(private), saved)
			}
			restored := fixtureClient(t)
			if err := restored.attachAdapters(restored.archive, saved, store); err != nil {
				t.Fatalf("a certificate of %d bytes was refused: %v", len(private), err)
			}
			if view, found := restored.saveStore.LoadSave(authenticationCertificate58Key); !found || !bytes.Equal(view, private) {
				t.Fatalf("the restored adapter answers %x for a certificate of %d bytes", view, len(private))
			}
		}
	})
	t.Run("the 58-byte adapter's path lists", func(t *testing.T) {
		store := newFaultStore(t, map[string]string{fileCreatedKey: "other.dat", fileRemovedKey: ""})
		adapter, ok := newAuthenticationCertificate58Store(store, nil, "12")
		if !ok {
			t.Fatal("the fixture adapter was not built")
		}
		// The title rewrites its certificate and then removes it.
		private := bytes.Repeat([]byte{0x5a}, 58)
		if err := adapter.StoreSave(authenticationCertificate58Key, private); err != nil {
			t.Fatal(err)
		}
		if err := adapter.StoreSave(fileRemovedKey, []byte(authenticationCertificate58Name)); err != nil {
			t.Fatal(err)
		}
		source := fixtureClient(t)
		source.saveStore = adapter
		saved := record(t, source)
		if saved.Store != adapterStoreCertificate58 || !saved.CertificateRemoved || !saved.CertificateCreated || !bytes.Equal(saved.Certificate, private) {
			t.Fatalf("the 58-byte adapter was recorded as %+v", saved)
		}
		store.behind(t, fileCreatedKey, "later.dat\nother.dat")
		store.behind(t, fileRemovedKey, "gone.dat")
		restored := fixtureClient(t)
		reads, attempts := store.reads, store.attempts
		if err := restored.attachAdapters(restored.archive, saved, store); err != nil || store.reads != reads+2 || store.attempts != attempts {
			t.Fatalf("attach = %v after %d reads and %d writes", err, store.reads-reads, store.attempts-attempts)
		}
		for key, want := range map[string]string{
			fileRemovedKey: authenticationCertificate58Name + "\ngone.dat",
			fileCreatedKey: authenticationCertificate58Name + "\nlater.dat\nother.dat",
		} {
			if view, _ := restored.saveStore.LoadSave(key); string(view) != want {
				t.Errorf("the restored view of %s is %q, want %q", key, view, want)
			}
		}
		if certificate, _ := restored.saveStore.LoadSave(authenticationCertificate58Key); !bytes.Equal(certificate, private) {
			t.Fatal("the certificate the title rewrote did not come back")
		}
		// A later write of a list never puts the certificate's name on disk.
		if err := restored.saveStore.StoreSave(fileRemovedKey, []byte(authenticationCertificate58Name+"\ngone.dat\nmore.dat")); err != nil {
			t.Fatal(err)
		}
		if store.held(fileRemovedKey) != "gone.dat\nmore.dat" || store.held(authenticationCertificate58Key) != "<absent>" {
			t.Fatalf("the store holds the list %q and the certificate %q", store.held(fileRemovedKey), store.held(authenticationCertificate58Key))
		}
		store.unreadable = fileCreatedKey
		if err := fixtureClient(t).attachAdapters(restored.archive, saved, store); err == nil {
			t.Fatal("a path list that cannot be read was accepted")
		}
	})
	t.Run("the 100-byte adapter's originals", func(t *testing.T) {
		module, _, _ := certificate100Fixture(t, 0)
		contract := authenticationCertificate100(module)
		if contract == nil {
			t.Fatal("missing fixture")
		}
		store := newFaultStore(t, nil)
		adapter := newAuthenticationCertificate100Store(store, contract)
		file := func(flag, fill byte) []byte {
			data, header := make([]byte, 820), make([]byte, 100)
			header[0] = 40
			for index := 100; index < 200; index++ {
				data[index] = fill
			}
			adapter.putHeader(data, header, flag)
			return data
		}
		store.behind(t, adapter.key, string(file(0, 0xa1)))
		adapter.active, adapter.originalFlag = true, 0
		adapter.originalCertificate, adapter.certificate = bytes.Repeat([]byte{0xa1}, 100), bytes.Repeat([]byte{0x42}, 100)
		source := fixtureClient(t)
		source.module, source.saveStore = module, adapter
		saved := record(t, source)
		if saved.Store != adapterStoreCertificate100 || !saved.Active || saved.OriginalFlag != 0 || !bytes.Equal(saved.OriginalCertificate, adapter.originalCertificate) {
			t.Fatalf("the 100-byte adapter was recorded as %+v", saved)
		}
		attach := func(t *testing.T) *authenticationCertificate100Store {
			t.Helper()
			restored := fixtureClient(t)
			restored.module = module
			if err := restored.attachAdapters(restored.archive, saved, store); err != nil {
				t.Fatal(err)
			}
			bound := restored.saveStore.(*authenticationCertificate100Store)
			if !bound.active || !bytes.Equal(bound.certificate, adapter.certificate) || bound.publishHeader == nil {
				t.Fatal("the run's own certificate did not come back")
			}
			return bound
		}
		// The file has another flag and another certificate now: those are
		// what the adapter has to keep in it.
		store.behind(t, adapter.key, string(file(1, 0xb2)))
		if bound := attach(t); bound.originalFlag != 1 || !bytes.Equal(bound.originalCertificate, bytes.Repeat([]byte{0xb2}, 100)) {
			t.Fatalf("the originals are %d and %x, want the file's own", bound.originalFlag, bound.originalCertificate[:4])
		}
		// A file with no header the adapter reads, and no file at all: the
		// record's originals are all there is.
		store.behind(t, adapter.key, "short")
		if bound := attach(t); bound.originalFlag != 0 || !bytes.Equal(bound.originalCertificate, bytes.Repeat([]byte{0xa1}, 100)) {
			t.Fatal("the record's originals were not used for a file without a header")
		}
		if err := store.ReplaceSaves(nil); err != nil {
			t.Fatal(err)
		}
		if bound := attach(t); bound.originalFlag != 0 || !bytes.Equal(bound.originalCertificate, bytes.Repeat([]byte{0xa1}, 100)) {
			t.Fatal("the record's originals were not used for a file that is gone")
		}
		if store.attempts != 0 {
			t.Fatal("attaching the adapter wrote to the store")
		}
		store.unreadable = adapter.key
		restored := fixtureClient(t)
		restored.module = module
		if err := restored.attachAdapters(restored.archive, saved, store); err == nil {
			t.Fatal("a file that cannot be read was accepted")
		}
	})
}
