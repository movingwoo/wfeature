package skt

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// packagedStoreFiles builds the pair a container carries for one record store:
// the `.sb` holding the name and the record table, and the `.db` holding the
// bytes those entries point into.
func packagedStoreFiles(name string, records ...[]byte) (index, data []byte) {
	entries := make([]byte, 0, len(records)*packagedStoreEntryBytes)
	for position, record := range records {
		entry := make([]byte, packagedStoreEntryBytes)
		binary.BigEndian.PutUint32(entry[0:4], uint32(position+rmsFirstRecordID))
		binary.BigEndian.PutUint32(entry[4:8], uint32(len(data)))
		binary.BigEndian.PutUint32(entry[8:12], uint32(len(record)))
		entries = append(entries, entry...)
		data = append(data, record...)
	}
	index = binary.BigEndian.AppendUint32(nil, uint32(len(records)+rmsFirstRecordID))
	index = binary.BigEndian.AppendUint16(index, uint16(len(name)))
	index = append(index, name...)
	index = binary.BigEndian.AppendUint32(index, 1)
	index = binary.BigEndian.AppendUint32(index, uint32(len(records)))
	index = binary.BigEndian.AppendUint32(index, uint32(len(data)))
	index = binary.BigEndian.AppendUint64(index, 1157345872686)
	return append(index, entries...), data
}

func carriedFixture(t *testing.T, store backend.SaveStore, name string, records ...[]byte) *Runtime {
	t.Helper()
	archive, err := Open(recordStoreJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	index, data := packagedStoreFiles(name, records...)
	archive.Entries["#Carried"+packagedStoreIndex] = index
	archive.Entries["#Carried"+packagedStoreData] = data
	options := testRuntimeOptions(t)
	options.SaveStore = store
	runtime, err := Start(archive, options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return runtime
}

// TestARecordStoreTheContainerCarriedIsAStoreThatExists is the SKT half of a
// defect both platforms had: a title whose save came inside its own archive
// was told it had none. The store's name is read out of the index rather than
// decoded from the file name, which is why the file here is spelled with the
// `#` a handset escapes an upper-case letter with.
func TestARecordStoreTheContainerCarriedIsAStoreThatExists(t *testing.T) {
	root := t.TempDir()
	runtime := carriedFixture(t, backend.NewDirectorySaveStore(filepath.Join(root, "carried")),
		"Carried", []byte("first"), []byte("second"))

	store, err := runtime.openStore("Carried", false)
	if err != nil {
		t.Fatalf("openRecordStore(create=false) on a carried store = %v", err)
	}
	if len(store.records) != 2 || string(store.records[0]) != "first" || string(store.records[1]) != "second" {
		t.Fatalf("records = %q, want the two the container carried", store.records)
	}
	// listRecordStores has to name it too: a title asking what it has finds
	// the store the same way a title opening it by name does.
	state := runtime.rms()
	state.mu.Lock()
	names := append([]string(nil), state.names...)
	state.mu.Unlock()
	if len(names) != 1 || names[0] != "Carried" {
		t.Fatalf("listRecordStores() = %q, want the carried store", names)
	}
}

// TestDeletingACarriedRecordStoreKeepsItDeleted pins the rule that makes the
// archive's copy safe to seed from: the Host's answer wins wherever there is
// one, and deleting a store writes an empty record list under its key.
func TestDeletingACarriedRecordStoreKeepsItDeleted(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "carried")
	runtime := carriedFixture(t, backend.NewDirectorySaveStore(directory), "Carried", []byte("first"))
	state := runtime.rms()
	state.mu.Lock()
	runtime.loadIndex(state)
	state.mu.Unlock()
	name := jvm.ReferenceValue(runtime.VM.NewString("Carried"))
	if _, err := runtime.rmsDeleteRecordStore(runtime.VM, []jvm.Value{name}); err != nil {
		t.Fatalf("deleteRecordStore() = %v", err)
	}

	next := carriedFixture(t, backend.NewDirectorySaveStore(directory), "Carried", []byte("first"))
	if _, err := next.openStore("Carried", false); err == nil {
		t.Fatal("a deleted store came back from the archive on the next session")
	}
	// Creating it again is a store with nothing in it, not the archive's copy.
	created, err := next.openStore("Carried", true)
	if err != nil {
		t.Fatalf("openRecordStore(create=true) after a delete = %v", err)
	}
	if len(created.records) != 0 {
		t.Fatalf("records = %q, want an empty store", created.records)
	}
	// The same is true inside the session the delete happened in.
	sameSession, err := runtime.openStore("Carried", true)
	if err != nil {
		t.Fatalf("openRecordStore(create=true) in the deleting session = %v", err)
	}
	if len(sameSession.records) != 0 {
		t.Fatalf("records = %q, want an empty store", sameSession.records)
	}
}

// TestAWrittenRecordStoreWinsOverTheCarriedCopy is the other half of the same
// rule: what the title wrote is what it reads back.
func TestAWrittenRecordStoreWinsOverTheCarriedCopy(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "carried")
	runtime := carriedFixture(t, backend.NewDirectorySaveStore(directory), "Carried", []byte("first"))
	store, err := runtime.openStore("Carried", false)
	if err != nil {
		t.Fatal(err)
	}
	store.records = [][]byte{[]byte("written")}
	runtime.persistStore(store)

	next := carriedFixture(t, backend.NewDirectorySaveStore(directory), "Carried", []byte("first"))
	reopened, err := next.openStore("Carried", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.records) != 1 || string(reopened.records[0]) != "written" {
		t.Fatalf("records = %q, want what the title wrote", reopened.records)
	}
}

// TestACarriedStoreNobodyWroteLeavesNothingBehind is the other side of that:
// serving a store out of the archive must not put its name in the Host's
// index, or a session whose archive no longer carries it would find a store
// that exists and holds nothing — and a title reads that as a save rather than
// as a first run.
func TestACarriedStoreNobodyWroteLeavesNothingBehind(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "carried")
	first := carriedFixture(t, backend.NewDirectorySaveStore(directory), "Carried", []byte("first"))
	if _, err := first.openStore("Carried", false); err != nil {
		t.Fatalf("openRecordStore on a carried store = %v", err)
	}

	// The same save directory, an archive that carries nothing.
	archive, err := Open(recordStoreJAR)
	if err != nil {
		t.Fatal(err)
	}
	options := testRuntimeOptions(t)
	options.SaveStore = backend.NewDirectorySaveStore(directory)
	next, err := Start(archive, options)
	if err != nil {
		t.Fatal(err)
	}
	if store, err := next.openStore("Carried", false); err == nil {
		t.Fatalf("a store nothing wrote survived its archive with %d records", len(store.records))
	}
}

// TestWritingOneCarriedStoreDoesNotPublishTheOthers is the multi-store half of
// the rule above, and the single-store test could not see it: the index is
// written whole, so the first write to one carried store used to put every
// carried store's name in it — names with no bytes under them, which is the
// phantom the rule exists to prevent.
func TestWritingOneCarriedStoreDoesNotPublishTheOthers(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "carried")
	archive, err := Open(recordStoreJAR)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Alpha", "Beta"} {
		index, data := packagedStoreFiles(name, []byte("x"))
		archive.Entries[name+packagedStoreIndex] = index
		archive.Entries[name+packagedStoreData] = data
	}
	options := testRuntimeOptions(t)
	options.SaveStore = backend.NewDirectorySaveStore(directory)
	runtime, err := Start(archive, options)
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := runtime.openStore("Alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	alpha.records = [][]byte{[]byte("written")}
	runtime.persistStore(alpha)

	// A later session whose archive carries neither.
	plain, err := Open(recordStoreJAR)
	if err != nil {
		t.Fatal(err)
	}
	later := testRuntimeOptions(t)
	later.SaveStore = backend.NewDirectorySaveStore(directory)
	next, err := Start(plain, later)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.openStore("Alpha", false); err != nil {
		t.Fatalf("the store that was written did not survive: %v", err)
	}
	if store, err := next.openStore("Beta", false); err == nil {
		t.Fatalf("a store nothing wrote was published with %d records", len(store.records))
	}
}

// TestACarriedRecordStoreIsRefusedWhenItDoesNotAddUp keeps a crafted container
// from being read as a store. Every field the parse trusts is checked against
// the data file it describes.
func TestACarriedRecordStoreIsRefusedWhenItDoesNotAddUp(t *testing.T) {
	index, data := packagedStoreFiles("Carried", []byte("first"))
	if _, _, ok := parsePackagedRecordStore(index, data[:len(data)-1]); ok {
		t.Fatal("parsed an index whose declared size is not the data file's")
	}
	if _, _, ok := parsePackagedRecordStore(index[:len(index)-1], data); ok {
		t.Fatal("parsed an index whose record table is short")
	}
	reaching, data := packagedStoreFiles("Carried", []byte("first"))
	binary.BigEndian.PutUint32(reaching[len(reaching)-4:], uint32(len(data)+1))
	if _, _, ok := parsePackagedRecordStore(reaching, data); ok {
		t.Fatal("parsed a record reaching past the data file")
	}
	if _, _, ok := parsePackagedRecordStore(nil, nil); ok {
		t.Fatal("parsed an empty index")
	}

	// The next-id word sizes an allocation, so a crafted one must not be
	// believed: four bytes naming four billion records is a hundred gigabytes
	// asked for before a record has been read.
	huge, _ := packagedStoreFiles("Carried")
	binary.BigEndian.PutUint32(huge[0:4], 0xffffff00)
	if _, _, ok := parsePackagedRecordStore(huge, nil); ok {
		t.Fatal("parsed an index naming four billion records")
	}
	// And it has to come after every id the table holds.
	behind, data := packagedStoreFiles("Carried", []byte("first"), []byte("second"))
	binary.BigEndian.PutUint32(behind[0:4], 2)
	if _, _, ok := parsePackagedRecordStore(behind, data); ok {
		t.Fatal("parsed an index whose next id is behind its own records")
	}
}

// TestACarriedStoreDeletedAndCreatedAgainSurvivesTheSession is the flow this
// whole change set exists to protect — clearing a slot to start a new game.
// The delete left the store marked unwritten, so the create that followed put
// its name into an index that filtered it straight back out, and the next
// session could find neither the Host's copy nor the archive's.
func TestACarriedStoreDeletedAndCreatedAgainSurvivesTheSession(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "carried")
	runtime := carriedPair(t, backend.NewDirectorySaveStore(directory))
	name := jvm.ReferenceValue(runtime.VM.NewString("Alpha"))
	if _, err := runtime.rmsDeleteRecordStore(runtime.VM, []jvm.Value{name}); err != nil {
		t.Fatal(err)
	}
	created, err := runtime.openStore("Alpha", true)
	if err != nil {
		t.Fatalf("creating it again = %v", err)
	}
	if len(created.records) != 0 {
		t.Fatalf("records = %q, want the empty store the title asked for", created.records)
	}

	next := plainFixture(t, backend.NewDirectorySaveStore(directory))
	if _, err := next.openStore("Alpha", false); err != nil {
		t.Fatalf("the store the title created is gone on the next session: %v", err)
	}
}

// TestAPackagedRecordOfNoBytesIsARecord keeps empty and absent apart. A nil
// slot is this runtime's tombstone for an id the store no longer has, and MIDP
// writes a zero-length record for addRecord(null, 0, 0).
func TestAPackagedRecordOfNoBytesIsARecord(t *testing.T) {
	index, data := packagedStoreFiles("Carried", []byte("aaaaa"), []byte{}, []byte("bbbbb"))
	_, records, ok := parsePackagedRecordStore(index, data)
	if !ok {
		t.Fatal("a store holding an empty record was refused")
	}
	if len(records) != 3 || records[1] == nil || len(records[1]) != 0 {
		t.Fatalf("records = %q, want the middle one empty rather than absent", records)
	}
}

// TestTheStoreListTheArchiveSeedsIsTheSameEveryRun keeps map order out of a
// file. The index is a list, so iterating the archive's entries in map order
// wrote different bytes from identical input on every launch — every save-tree
// comparison then reported a difference that was not one, and a real index
// regression would hide inside that noise.
func TestTheStoreListTheArchiveSeedsIsTheSameEveryRun(t *testing.T) {
	archive, err := Open(recordStoreJAR)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon"} {
		index, data := packagedStoreFiles(name, []byte("x"))
		archive.Entries[name+packagedStoreIndex] = index
		archive.Entries[name+packagedStoreData] = data
	}
	var first []string
	for round := range 20 {
		options := testRuntimeOptions(t)
		options.SaveStore = backend.NewDirectorySaveStore(t.TempDir())
		runtime, err := Start(archive, options)
		if err != nil {
			t.Fatal(err)
		}
		state := runtime.rms()
		state.mu.Lock()
		runtime.loadIndex(state)
		names := append([]string(nil), state.names...)
		state.mu.Unlock()
		if round == 0 {
			first = names
			continue
		}
		if strings.Join(names, "\n") != strings.Join(first, "\n") {
			t.Fatalf("round %d seeded %q, want the same order as %q", round, names, first)
		}
	}
}

// TestACraftedIndexCannotAskForMoreThanItDescribes bounds the two words that
// size an allocation. Both come out of an archive: the next-record id, and an
// entry's own id, which used to be a second door to the same room.
func TestACraftedIndexCannotAskForMoreThanItDescribes(t *testing.T) {
	huge, _ := packagedStoreFiles("Carried")
	binary.BigEndian.PutUint32(huge[0:4], 0xffffff00)
	if _, _, ok := parsePackagedRecordStore(huge, nil); ok {
		t.Fatal("parsed an index naming four billion records")
	}
	reaching, data := packagedStoreFiles("Carried", []byte("first"))
	binary.BigEndian.PutUint32(reaching[len(reaching)-12:], 16384)
	if _, _, ok := parsePackagedRecordStore(reaching, data); ok {
		t.Fatal("parsed a record whose id is past the id the store hands out next")
	}

	// And what a whole archive may ask for is bounded as well as each store.
	archive, err := Open(recordStoreJAR)
	if err != nil {
		t.Fatal(err)
	}
	wide, wideData := packagedStoreFiles("Wide")
	binary.BigEndian.PutUint32(wide[0:4], packagedStoreMaxRecords)
	for index := range 500 {
		name := fmt.Sprintf("Wide%03d", index)
		copied := append([]byte(nil), wide...)
		binary.BigEndian.PutUint16(copied[4:6], uint16(len(name)))
		copied = append(copied[:6], append([]byte(name), copied[6+len("Wide"):]...)...)
		archive.Entries[name+packagedStoreIndex] = copied
		archive.Entries[name+packagedStoreData] = wideData
	}
	slots := 0
	for _, records := range archive.packagedRecordStores() {
		slots += len(records)
	}
	if slots > packagedStoreMaxSlots {
		t.Fatalf("one archive asked for %d slots, over the %d budget", slots, packagedStoreMaxSlots)
	}
}

func carriedPair(t *testing.T, store backend.SaveStore) *Runtime {
	t.Helper()
	archive, err := Open(recordStoreJAR)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Alpha", "Beta"} {
		index, data := packagedStoreFiles(name, []byte("x"))
		archive.Entries[name+packagedStoreIndex] = index
		archive.Entries[name+packagedStoreData] = data
	}
	options := testRuntimeOptions(t)
	options.SaveStore = store
	runtime, err := Start(archive, options)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func plainFixture(t *testing.T, store backend.SaveStore) *Runtime {
	t.Helper()
	archive, err := Open(recordStoreJAR)
	if err != nil {
		t.Fatal(err)
	}
	options := testRuntimeOptions(t)
	options.SaveStore = store
	runtime, err := Start(archive, options)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
