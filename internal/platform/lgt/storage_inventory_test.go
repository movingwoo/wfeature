package lgt

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/storageinventory"
)

// wantStorageInventory compares a whole boundary: every column that is not
// named has to be zero, because a count that appears where none was expected
// is as wrong as one that is missing.
func wantStorageInventory(t *testing.T, step string, got storageInventory, want map[int]int) {
	t.Helper()
	for column, name := range storageInventoryNames {
		if got[column] != want[column] {
			t.Errorf("%s: %s = %d, want %d", step, name, got[column], want[column])
		}
	}
}

// The layout the probe hands the tally is built from the same table the
// columns are indexed by, so a column added without a name is caught here
// rather than as an unnamed number in a library's worth of records.
func TestStorageInventoryNamesEveryColumn(t *testing.T) {
	for column, name := range storageInventoryNames {
		if name == "" {
			t.Errorf("column %d has no name", column)
		}
	}
	if err := storageInventoryLayout.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The probe's counting, on the authored Clet. The file slots are driven the
// way a module drives them, across real ticks, and every boundary is compared
// whole — the arithmetic the local probe reports is this arithmetic, and the
// library is not here to check it against.
func TestStorageInventoryCountsAnAuthoredSession(t *testing.T) {
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	session := checkpointFixtureSession(t, fixtureArchive(t), store)
	client := session.client
	text := func(value string) uint32 {
		t.Helper()
		address, err := client.allocateBytes(append([]byte(value), 0))
		if err != nil {
			t.Fatal(err)
		}
		return address
	}
	open := func(name, flag uint32) uint32 {
		t.Helper()
		handle := callSlot(t, client, slotFsOpen, name, flag)
		if int32(handle) < 0 {
			t.Fatalf("open answered %d", int32(handle))
		}
		return handle
	}
	tally := storageInventoryLayout.NewTally()
	notes := map[int][]string{}
	boundary := func(step string, want map[int]int) {
		t.Helper()
		got := inventoryStorage(client, func(column int, name string) {
			if !slices.Contains(notes[column], name) {
				notes[column] = append(notes[column], name)
			}
			tally.Example(storageInventoryNames[column], name)
		})
		wantStorageInventory(t, step, got, want)
		tally.Observe(got[:])
	}

	// Counting reads the store and nothing else: a session that has not read
	// its removal list yet has still not read it afterwards, and the store is
	// what it was.
	tickSession(t, session, 2)
	before, _ := store.SnapshotSaves()
	boundary("a session that has opened nothing", nil)
	if client.removed != nil || client.created != nil || client.saveReadError != nil {
		t.Fatal("counting a boundary read the session's own removal or created list")
	}
	if after, _ := store.SnapshotSaves(); !reflect.DeepEqual(before, after) {
		t.Fatal("counting a boundary wrote to the store")
	}

	save, written := text("save.dat"), text("written by the guest")
	handle := open(save, fileOpenReadWrite)
	callSlot(t, client, slotFsWrite, handle, written, 20)
	dirty := map[int]int{storageHandles: 1, storageWritable: 1, storageDirty: 1, storageStale: 1, storageLedgersLoaded: 2}
	boundary("a written file that is still open", dirty)
	tickSession(t, session, 2)
	boundary("the same file two ticks later", dirty)
	callSlot(t, client, slotFsClose, handle)
	boundary("the file closed", map[int]int{storageLedgersLoaded: 2})

	// A reader and a writer on one file. The reader is not stale while the
	// writer only holds its change, and is once the writer has stored it.
	reader, writer := open(save, fileOpenReadOnly), open(save, fileOpenReadWrite)
	boundary("two handles on one file", map[int]int{storageHandles: 2, storageWritable: 1, storageSharedKeys: 1, storageLedgersLoaded: 2})
	callSlot(t, client, slotFsWrite, writer, text("LATER"), 5)
	boundary("one of them written", map[int]int{
		storageHandles: 2, storageWritable: 1, storageDirty: 1, storageStale: 1, storageSharedKeys: 1, storageLedgersLoaded: 2})
	callSlot(t, client, slotFsClose, writer)
	boundary("the writer closed over the reader", map[int]int{
		storageHandles: 1, storageStale: 1, storageStaleClean: 1, storageLedgersLoaded: 2})

	// An open that truncates stores nothing until it is written to, so the
	// file under it still holds its bytes.
	truncating := open(save, fileOpenWriteTruncate)
	boundary("a truncating open that has not written", map[int]int{
		storageHandles: 2, storageWritable: 1, storageStale: 2, storageStaleClean: 2, storageEmptyOverContent: 1,
		storageSharedKeys: 1, storageLedgersLoaded: 2})

	// Removed while it is open: the path reads as nothing, which is what the
	// empty handle holds and not what the reader does.
	callSlot(t, client, slotFsRemove, save)
	boundary("the path removed under both", map[int]int{
		storageHandles: 2, storageWritable: 1, storageStale: 1, storageStaleClean: 1, storageRemovedOpen: 2,
		storageSharedKeys: 1, storageLedgersLoaded: 2})
	callSlot(t, client, slotFsClose, reader)
	callSlot(t, client, slotFsClose, truncating)
	boundary("both closed", map[int]int{storageLedgersLoaded: 2})

	// A name the save store has no key for is a file nothing can store.
	unkeyed := open(text(`dir\name.dat`), fileOpenReadWrite)
	callSlot(t, client, slotFsWrite, unkeyed, written, 3)
	boundary("a written file with no save key", map[int]int{
		storageHandles: 1, storageWritable: 1, storageDirty: 1, storageStale: 1, storageNoSaveKey: 1,
		storageDirtyNoSaveKey: 1, storageLedgersLoaded: 2})
	callSlot(t, client, slotFsClose, unkeyed)
	boundary("it closed", map[int]int{storageLedgersLoaded: 2})

	// Two spellings a case-folding filesystem keeps as one file are one key.
	lower, upper := open(text("twice.dat"), fileOpenReadWrite), open(text("TWICE.dat"), fileOpenReadWrite)
	callSlot(t, client, slotFsWrite, lower, written, 4)
	callSlot(t, client, slotFsWrite, upper, written, 6)
	boundary("two written handles on one key", map[int]int{
		storageHandles: 2, storageWritable: 2, storageDirty: 2, storageStale: 2, storageSharedKeys: 1,
		storageSharedDirtyKeys: 1, storageLedgersLoaded: 2})
	callSlot(t, client, slotFsClose, lower)
	callSlot(t, client, slotFsClose, upper)
	boundary("both of those closed", map[int]int{storageLedgersLoaded: 2})

	// The removal list changed in the store behind the list the session read.
	if err := store.StoreSave(fileRemovedKey, []byte("elsewhere.dat\nsave.dat")); err != nil {
		t.Fatal(err)
	}
	boundary("a removal list the store no longer agrees with", map[int]int{storageLedgersLoaded: 2, storageStaleLedgers: 1})

	for column, want := range map[int][]string{
		storageDirty:            {"save.dat", `dir\name.dat`, "twice.dat", "TWICE.dat"},
		storageStaleClean:       {"save.dat"},
		storageEmptyOverContent: {"save.dat"},
		storageNoSaveKey:        {`dir\name.dat`},
		storageDirtyNoSaveKey:   {`dir\name.dat`},
		storageRemovedOpen:      {"save.dat"},
		storageSharedKeys:       {"key:fs/save.dat", "key:fs/twice.dat"},
		storageSharedDirtyKeys:  {"key:fs/twice.dat"},
		storageStaleLedgers:     {fileRemovedKey},
	} {
		got := slices.Clone(notes[column])
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s was noted on %q, want %q", storageInventoryNames[column], got, want)
		}
	}

	// What the probe reports for an archive is this record. A write was
	// pending at five boundaries in four runs: the first file stayed open over
	// a tick, and the other three were each closed before the next look.
	record := tally.Record("authored", "", "clet", "ok")
	if record.Boundaries != 15 {
		t.Fatalf("the record covers %d boundaries, want 15", record.Boundaries)
	}
	if got, want := record.Groups["pending"], (storageinventory.GroupRecord{Boundaries: 5, Runs: 4, LongestRun: 2}); got != want {
		t.Errorf("pending = %+v, want %+v", got, want)
	}
	if got, want := record.Groups["stale"], (storageinventory.GroupRecord{Boundaries: 4, Runs: 2, LongestRun: 3}); got != want {
		t.Errorf("stale = %+v, want %+v", got, want)
	}
	if record.Max["handles"] != 2 || record.BoundariesWith["handles"] != 9 || record.Max["dirty"] != 2 || record.BoundariesWith["dirty"] != 5 {
		t.Errorf("handles = %d at %d boundaries and dirty = %d at %d, want 2 at 9 and 2 at 5",
			record.Max["handles"], record.BoundariesWith["handles"], record.Max["dirty"], record.BoundariesWith["dirty"])
	}
	if want := []string{"save.dat", `dir\name.dat`, "twice.dat", "TWICE.dat"}; !slices.Equal(sortedCopy(record.Examples["dirty"]), sortedCopy(want)) {
		t.Errorf("the record's dirty examples are %q, want %q", record.Examples["dirty"], want)
	}
}

func sortedCopy(values []string) []string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted
}

// The Java tables, driven through the natives a title's own code reaches them
// by. Nothing here ticks: the collector closes a File no guest object holds,
// and these objects are held by the test alone.
func TestStorageInventoryCountsJavaStorage(t *testing.T) {
	client := fixtureClient(t)
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.saveStore = store
	var notes [storageColumns][]string
	boundary := func(step string, want map[int]int) {
		t.Helper()
		got := inventoryStorage(client, func(column int, name string) { notes[column] = append(notes[column], name) })
		wantStorageInventory(t, step, got, want)
	}

	// One name opened twice is two objects with a record list each, and the
	// one that did not make a change no longer holds what the store does.
	first := openFixtureDatabase(t, client, "save", 64)
	insertFixtureRecord(t, client, first, []byte("alpha"))
	boundary("an open database", map[int]int{storageDatabases: 1, storageLedgersLoaded: 2})
	second := openFixtureDatabase(t, client, "save", 64)
	boundary("the same database opened twice", map[int]int{storageDatabases: 2, storageSharedDatabases: 1, storageLedgersLoaded: 2})
	insertFixtureRecord(t, client, second, []byte("beta"))
	boundary("a record inserted through the second", map[int]int{
		storageDatabases: 2, storageSharedDatabases: 1, storageStaleDatabases: 1, storageLedgersLoaded: 2})

	// Deleted by name while both are open: the container reads as nothing,
	// and both objects still hold records.
	name, err := client.newJavaString("save")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaDeleteDataBase(client, nil, nil, []uint32{name}); err != nil {
		t.Fatal(err)
	}
	boundary("the database deleted under both", map[int]int{
		storageDatabases: 2, storageSharedDatabases: 1, storageStaleDatabases: 2, storageLedgersLoaded: 2})
	for _, object := range []uint32{first, second} {
		if _, err := javaCloseDataBase(client, nil, nil, []uint32{object}); err != nil {
			t.Fatal(err)
		}
	}
	boundary("both closed", map[int]int{storageLedgersLoaded: 2})

	// A stream opened on a File holds what it is written until it is flushed,
	// and only then does the file hold a write of its own.
	path, err := client.newJavaString("stream.dat")
	if err != nil {
		t.Fatal(err)
	}
	const writing, reading = uint32(0x1000), uint32(0x1100)
	if _, err := javaFileOpen(client, nil, nil, []uint32{writing, path, 4, 1}); err != nil {
		t.Fatal(err)
	}
	sink, err := javaFileOpenOutputStream(client, nil, nil, []uint32{writing})
	if err != nil {
		t.Fatal(err)
	}
	boundary("an output stream nothing was written to", map[int]int{storageHandles: 1, storageWritable: 1, storageLedgersLoaded: 2})
	for _, value := range []uint32{7, 8, 9} {
		if _, err := javaByteSinkWrite(client, nil, nil, []uint32{sink, value}); err != nil {
			t.Fatal(err)
		}
	}
	boundary("bytes held by the stream", map[int]int{storageHandles: 1, storageWritable: 1, storageSinkFiles: 1, storageLedgersLoaded: 2})
	if _, err := javaByteSinkFlush(client, nil, nil, []uint32{sink}); err != nil {
		t.Fatal(err)
	}
	boundary("the stream flushed into the file", map[int]int{
		storageHandles: 1, storageWritable: 1, storageDirty: 1, storageStale: 1, storageLedgersLoaded: 2})

	if _, err := javaFileOpen(client, nil, nil, []uint32{reading, path, 1, 1}); err != nil {
		t.Fatal(err)
	}
	stream, err := javaFileOpenInputStream(client, nil, nil, []uint32{reading})
	if err != nil {
		t.Fatal(err)
	}
	boundary("an input stream on a second File", map[int]int{
		storageHandles: 2, storageWritable: 1, storageDirty: 1, storageStale: 1, storageSharedKeys: 1,
		storageFileStreams: 1, storageLedgersLoaded: 2})
	if _, err := javaStreamClose(client, nil, nil, []uint32{stream}); err != nil {
		t.Fatal(err)
	}
	for _, file := range []uint32{reading, writing} {
		if _, err := javaFileClose(client, nil, nil, []uint32{file}); err != nil {
			t.Fatal(err)
		}
	}
	boundary("every File closed", map[int]int{storageLedgersLoaded: 2})

	for column, want := range map[int]string{
		storageStaleDatabases: "save", storageSharedDatabases: "save", storageSinkFiles: "stream.dat", storageFileStreams: "stream.dat",
	} {
		if !slices.Contains(notes[column], want) {
			t.Errorf("%s was noted on %q, want %q among them", storageInventoryNames[column], notes[column], want)
		}
	}
}

// keyRefusingStore fails every read of one key and answers the rest.
type keyRefusingStore struct {
	*backend.MemorySaveStore
	key string
}

func (store keyRefusingStore) ReadSave(name string) ([]byte, bool, error) {
	if name == store.key {
		return nil, false, errors.New("injected read failure")
	}
	return store.MemorySaveStore.ReadSave(name)
}

// A lookup the store refuses is counted as that and compared with nothing, and
// the failure stays with the probe's own view: the session's retained error is
// reported, not caused.
func TestStorageInventoryCountsWhatTheStoreRefuses(t *testing.T) {
	memory, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := fixtureClient(t)
	client.saveStore = keyRefusingStore{MemorySaveStore: memory, key: "fs/saves"}
	if got := inventoryStorage(client, nil); got != (storageInventory{}) {
		t.Fatalf("a client that has opened nothing counted %v", got)
	}
	if client.saveReadError != nil {
		t.Fatal("counting a boundary left the session with a read failure")
	}

	// The same lookup made by the title is what the session retains.
	if handle := client.openFile("saves", fileOpenReadWrite); handle < 0 {
		t.Fatalf("open answered %d", handle)
	}
	if client.saveReadError == nil {
		t.Fatal("the store's refusal did not reach the session")
	}
	held := client.saveReadError
	var noted []string
	got := inventoryStorage(client, func(column int, name string) {
		if column == storageUnreadable {
			noted = append(noted, name)
		}
	})
	// The created list the session holds names the file its suppressed write
	// never stored, which is a list the store does not agree with.
	wantStorageInventory(t, "an open file the store cannot read", got, map[int]int{
		storageHandles: 1, storageWritable: 1, storageUnreadable: 1, storageReadError: 1,
		storageLedgersLoaded: 2, storageStaleLedgers: 1})
	if !slices.Equal(noted, []string{"saves"}) {
		t.Errorf("the refused lookup was noted on %q, want the file it was for", noted)
	}
	if client.saveReadError != held {
		t.Fatal("counting a boundary replaced the session's retained read failure")
	}
}
