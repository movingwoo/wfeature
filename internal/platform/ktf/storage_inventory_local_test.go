package ktf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/storageinventory"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// The storage inventory
//
// A quick save and a quick load are taken between two service rounds. These
// probes look at what the platform is holding for a title's storage at exactly
// those moments and count it. They check nothing and change nothing: what they
// produce is the size of each case a rule about storage at a boundary has to
// have an answer for, before the rule is written.
//
// # The descriptor runtime
//
// Every write here is through to the store before the call that made it
// returns, so there is no pending write to count. What there is instead is a
// copy: each table keeps the whole of a key in memory from the first open to
// the end of the session, and answers from it without asking the store again.
// The counters are those copies and whether they still say what the store
// says:
//
//	java_databases               names in the DataBase catalog
//	c_files                      names in the WIPI C file catalog
//	record_databases             names in the WIPI C record database catalog
//	c_file_handles               open MC_fsOpen handles
//	record_handles               open record database handles
//	handle_only_stores           stores an open handle holds that the catalog
//	                             no longer names
//	written_files                names in the session-written file table
//	file_payloads                live File states, each shared by a File and
//	                             the streams opened on it
//	object_only_databases        record lists a DataBase object holds that the
//	                             catalog no longer names
//	ledger_fs_removed            1 while the session has read fs/.removed
//	ledger_db_removed            1 while it has read db/.removed
//	ledger_db_dirs               1 while it has read db/.dirs
//	ledger_rdb_removed           1 while it has read rdb/.removed
//	ledger_jdb_removed           1 while it has read jdb/.removed
//	stale_java_databases         cataloged DataBase stores that are not what an
//	                             open of the name would read now
//	stale_c_files                the same for cataloged C files
//	stale_record_databases       the same for cataloged record databases
//	stale_written_files          written-file entries that are not the bytes
//	                             the store holds under the name
//	stale_file_payloads          File states that are not what a File of the
//	                             name would be built from now
//	stale_object_only_databases  the object-only record lists that differ
//	stale_ledgers                read ledgers that differ from the store's list
//	empty_over_content           empty File states over a name that holds
//	                             bytes: an open that truncates and has not
//	                             written yet, or a file written behind it
//	unresolved                   cataloged or written names nothing answers for
//	unreadable                   lookups the store answered with an error
//	heap_refused                 1 when the heap could not be walked, which is
//	                             when the three counts taken from it are zero
//	                             because they are unknown
//	busy                         1 when the client's run lock was held, which
//	                             leaves every other count unknown
//	read_error                   1 while the session holds a read failure
//
// **What the store holds is asked of a second runtime, not of the session.**
// What an open reads is a removal list, then a save, then the packaged copy,
// and the functions that walk that order fill the session's ledger caches and
// keep a failed read for the rest of the session. Asked on the running
// runtime they would answer from the session's own memory and could change
// it. A runtime that shares the client — its store and its mounted files —
// and has read nothing is what a first open, and a table rebuilt after a
// load, would be answered; what it retains is discarded with it.
//
// The File states come from the heap capture a checkpoint takes, because
// that is what says which objects are live. A boundary at which the capture
// is refused is counted as that.
const (
	descriptorJavaDatabases = iota
	descriptorCFiles
	descriptorRecordDatabases
	descriptorCFileHandles
	descriptorRecordHandles
	descriptorHandleOnlyStores
	descriptorWrittenFiles
	descriptorFilePayloads
	descriptorObjectOnlyDatabases
	descriptorLedgerFSRemoved
	descriptorLedgerDBRemoved
	descriptorLedgerDBDirs
	descriptorLedgerRDBRemoved
	descriptorLedgerJDBRemoved
	descriptorStaleJavaDatabases
	descriptorStaleCFiles
	descriptorStaleRecordDatabases
	descriptorStaleWrittenFiles
	descriptorStaleFilePayloads
	descriptorStaleObjectOnlyDatabases
	descriptorStaleLedgers
	descriptorEmptyOverContent
	descriptorUnresolved
	descriptorUnreadable
	descriptorHeapRefused
	descriptorBusy
	descriptorReadError
	descriptorColumns
)

// descriptorInventory is one boundary: a count per column.
type descriptorInventory [descriptorColumns]int

var descriptorInventoryNames = [descriptorColumns]string{
	descriptorJavaDatabases:            "java_databases",
	descriptorCFiles:                   "c_files",
	descriptorRecordDatabases:          "record_databases",
	descriptorCFileHandles:             "c_file_handles",
	descriptorRecordHandles:            "record_handles",
	descriptorHandleOnlyStores:         "handle_only_stores",
	descriptorWrittenFiles:             "written_files",
	descriptorFilePayloads:             "file_payloads",
	descriptorObjectOnlyDatabases:      "object_only_databases",
	descriptorLedgerFSRemoved:          "ledger_fs_removed",
	descriptorLedgerDBRemoved:          "ledger_db_removed",
	descriptorLedgerDBDirs:             "ledger_db_dirs",
	descriptorLedgerRDBRemoved:         "ledger_rdb_removed",
	descriptorLedgerJDBRemoved:         "ledger_jdb_removed",
	descriptorStaleJavaDatabases:       "stale_java_databases",
	descriptorStaleCFiles:              "stale_c_files",
	descriptorStaleRecordDatabases:     "stale_record_databases",
	descriptorStaleWrittenFiles:        "stale_written_files",
	descriptorStaleFilePayloads:        "stale_file_payloads",
	descriptorStaleObjectOnlyDatabases: "stale_object_only_databases",
	descriptorStaleLedgers:             "stale_ledgers",
	descriptorEmptyOverContent:         "empty_over_content",
	descriptorUnresolved:               "unresolved",
	descriptorUnreadable:               "unreadable",
	descriptorHeapRefused:              "heap_refused",
	descriptorBusy:                     "busy",
	descriptorReadError:                "read_error",
}

var descriptorInventoryLayout = storageinventory.Layout{
	Platform: "ktf",
	Counters: descriptorInventoryNames[:],
	Groups: []storageinventory.Group{
		// What a table rebuilt from the store would hold differently.
		{Name: "stale", Counters: []string{
			"stale_java_databases", "stale_c_files", "stale_record_databases", "stale_written_files",
			"stale_file_payloads", "stale_object_only_databases", "stale_ledgers"}},
	},
}

// descriptorFilePayloadKind is the name the heap capture files a File state
// under. It is spelled here as well as where the capture writes it, and the
// authored test is what fails if the two stop agreeing.
const descriptorFilePayloadKind = "ktf-file-v1"

// descriptorJavaDatabaseNow is what DataBase.openDataBase would find for a
// name the catalog did not hold: the save unless the name is on this table's
// removal list, and otherwise the packaged copy unless either list hides it.
// It is that function's own lookup, asked of a runtime that has read nothing.
func descriptorJavaDatabaseNow(view *initializationRuntime, name string) (records [][]byte, found bool, err error) {
	deleted := view.recordDatabaseRemovals(javaDatabaseRemovedKey)[name]
	saved, present, err := backend.ReadSave(view.client.saveStore, "jdb/"+name)
	if err != nil {
		return nil, false, err
	}
	if present && !deleted {
		records, err := decodeSaveRecords(saved)
		return records, true, err
	}
	if view.databaseDeleted(name) {
		return nil, false, nil
	}
	records, found = view.packagedRecordDatabase(name, 0)
	return records, found, nil
}

// descriptorRecordDatabaseNow is the same question for the WIPI C record
// table, in the order its own open asks it: a save, deleted or not, keeps the
// packaged copy from being looked at.
func descriptorRecordDatabaseNow(view *initializationRuntime, name string, recordSize uint32) (records [][]byte, found bool, err error) {
	deleted := view.recordDatabaseRemovals(recordDatabaseRemovedKey)[name]
	saved, hasSaved := view.loadSave("rdb/" + name)
	if !hasSaved {
		if view.databaseDeleted(name) {
			return nil, false, nil
		}
		records, found = view.packagedRecordDatabase(name, recordSize)
		return records, found, nil
	}
	if deleted {
		return nil, false, nil
	}
	records, err = decodeSaveRecords(saved)
	return records, true, err
}

// sameRecords compares two record lists the way the store would see them, so
// that an empty record and a deleted slot stay different and no list and an
// empty list do not.
func sameRecords(left, right [][]byte) bool {
	return bytes.Equal(encodeSaveRecords(left), encodeSaveRecords(right))
}

// sameLedger reports whether two ledgers list the same names.
func sameLedger(left, right map[string]bool) bool {
	listed := func(set map[string]bool) int {
		count := 0
		for _, present := range set {
			if present {
				count++
			}
		}
		return count
	}
	if listed(left) != listed(right) {
		return false
	}
	for name, present := range left {
		if present && !right[name] {
			return false
		}
	}
	return true
}

// inventoryDescriptorStorage counts one boundary. roots are heap roots beside
// the workers' own, which only an authored caller has. note, when it is not
// nil, is told the name each counted condition was seen on.
func inventoryDescriptorStorage(client *Client, roots []*jvm.Object, note func(column int, name string)) descriptorInventory {
	var counts descriptorInventory
	mark := func(column int, name string) {
		counts[column]++
		if note != nil {
			note(column, name)
		}
	}
	// A free lock is what says no worker is inside a service call.
	if !client.run.TryLock() {
		counts[descriptorBusy] = 1
		return counts
	}
	defer client.run.Unlock()
	state := client.runtime
	if state == nil {
		return counts
	}
	view := &initializationRuntime{client: client}
	// A lookup the store refused says nothing about the copy it was for, so it
	// is counted as that and the comparison is left out.
	refused := func(name string, err error) bool {
		if err == nil {
			err = view.saveReadError
		}
		view.saveReadError = nil
		if err == nil {
			return false
		}
		mark(descriptorUnreadable, name)
		return true
	}

	for name, store := range state.databases {
		counts[descriptorJavaDatabases]++
		records, found, err := descriptorJavaDatabaseNow(view, name)
		if refused("jdb/"+name, err) {
			continue
		}
		if !found {
			mark(descriptorUnresolved, "jdb/"+name)
		}
		if !sameRecords(store.records, records) {
			mark(descriptorStaleJavaDatabases, name)
		}
	}

	for name, store := range state.cFiles {
		counts[descriptorCFiles]++
		seed, found := view.databaseSeed(name)
		if refused("db/"+name, nil) {
			continue
		}
		if !found {
			mark(descriptorUnresolved, "db/"+name)
		}
		if !bytes.Equal(store.data, seed) {
			mark(descriptorStaleCFiles, name)
		}
	}
	detachedFiles := map[*runtimeCFile]bool{}
	for _, open := range state.cFileHandles {
		if open == nil || open.store == nil {
			continue
		}
		counts[descriptorCFileHandles]++
		if state.cFiles[open.store.name] != open.store && !detachedFiles[open.store] {
			detachedFiles[open.store] = true
			mark(descriptorHandleOnlyStores, "db/"+open.store.name)
		}
	}

	for name, store := range state.recordDatabases {
		counts[descriptorRecordDatabases]++
		records, found, err := descriptorRecordDatabaseNow(view, name, store.recordSize)
		if refused("rdb/"+name, err) {
			continue
		}
		if !found {
			mark(descriptorUnresolved, "rdb/"+name)
		}
		if !sameRecords(store.records, records) {
			mark(descriptorStaleRecordDatabases, name)
		}
	}
	detachedRecords := map[*runtimeRecordDatabase]bool{}
	for _, open := range state.recordDatabaseHandles {
		if open == nil || open.store == nil {
			continue
		}
		counts[descriptorRecordHandles]++
		if state.recordDatabases[open.store.name] != open.store && !detachedRecords[open.store] {
			detachedRecords[open.store] = true
			mark(descriptorHandleOnlyStores, "rdb/"+open.store.name)
		}
	}

	// The written-file table shadows the store for every name it holds. A
	// rebuilt one would hold the store's bytes for a name that is still there
	// and would not hold the name at all otherwise.
	removedFiles := view.removedGuestFiles()
	removedRefused := refused(guestFileRemovedKey, nil)
	for name, data := range state.guestFiles {
		counts[descriptorWrittenFiles]++
		trimmed := strings.TrimPrefix(name, "/")
		if removedRefused {
			continue
		}
		var stored []byte
		found := false
		if !removedFiles[trimmed] {
			stored, found = view.loadSave("fs/" + trimmed)
		}
		if refused("fs/"+trimmed, nil) {
			continue
		}
		if !found {
			mark(descriptorUnresolved, "fs/"+trimmed)
		}
		if !found || !bytes.Equal(data, stored) {
			mark(descriptorStaleWrittenFiles, name)
		}
	}

	ledgers := []struct {
		column int
		key    string
		held   map[string]bool
		stored func() map[string]bool
	}{
		{descriptorLedgerFSRemoved, guestFileRemovedKey, state.removedFiles, func() map[string]bool { return removedFiles }},
		{descriptorLedgerDBRemoved, databaseRemovedKey, state.removedCDatabases, view.removedDatabases},
		{descriptorLedgerDBDirs, directoryListKey, state.madeDirectories, view.createdDirectories},
		{descriptorLedgerRDBRemoved, recordDatabaseRemovedKey, state.removedDatabaseLists[recordDatabaseRemovedKey],
			func() map[string]bool { return view.recordDatabaseRemovals(recordDatabaseRemovedKey) }},
		{descriptorLedgerJDBRemoved, javaDatabaseRemovedKey, state.removedDatabaseLists[javaDatabaseRemovedKey],
			func() map[string]bool { return view.recordDatabaseRemovals(javaDatabaseRemovedKey) }},
	}
	for _, ledger := range ledgers {
		// Unread is the state a session starts in, and a list nobody has read
		// is not a copy of anything.
		if ledger.held == nil {
			continue
		}
		counts[ledger.column] = 1
		stored := ledger.stored()
		if refused(ledger.key, nil) || ledger.key == guestFileRemovedKey && removedRefused {
			continue
		}
		if !sameLedger(ledger.held, stored) {
			mark(descriptorStaleLedgers, ledger.key)
		}
	}

	walked := append([]*jvm.Object(nil), roots...)
	for _, worker := range client.workers {
		if worker != nil {
			walked = append(walked, worker.javaThread, worker.timerOwner, worker.paintedCard)
		}
	}
	saved, err := state.captureHeapState(walked)
	if err != nil {
		mark(descriptorHeapRefused, storageinventory.Reason(err))
	} else {
		for _, payload := range saved.JVM.Payloads {
			if payload.ExternalKind != descriptorFilePayloadKind {
				continue
			}
			file, err := restoreHeapFile(payload.Data)
			if err != nil {
				mark(descriptorHeapRefused, storageinventory.Reason(err))
				continue
			}
			counts[descriptorFilePayloads]++
			stored, _ := view.guestFile(file.name)
			if refused("fs/"+strings.TrimPrefix(file.name, "/"), nil) {
				continue
			}
			if !bytes.Equal(file.data, stored) {
				mark(descriptorStaleFilePayloads, file.name)
			}
			if len(file.data) == 0 && len(stored) != 0 {
				mark(descriptorEmptyOverContent, file.name)
			}
		}
		// The capture numbers every record list it meets, the catalog's first.
		// What is left is a list only an object reaches.
		cataloged := make(map[uint32]bool, len(saved.DatabaseBindings))
		for _, binding := range saved.DatabaseBindings {
			cataloged[binding.Database] = true
		}
		for index, database := range saved.Databases {
			if cataloged[uint32(index+1)] {
				continue
			}
			name := string(database.Name)
			counts[descriptorObjectOnlyDatabases]++
			records, _, err := descriptorJavaDatabaseNow(view, name)
			if refused("jdb/"+name, err) {
				continue
			}
			if !sameRecords(database.Records, records) {
				mark(descriptorStaleObjectOnlyDatabases, name)
			}
		}
	}
	if state.saveReadError != nil {
		counts[descriptorReadError] = 1
	}
	return counts
}

// descriptorInventoryDirectories is where the local archives are: the KTF
// directory of the ignored library every other local probe reads, or the one
// WFEATURE_KTF_STORAGE_INVENTORY_DIR names. An absolute path is taken as it
// is, which is how the probe is pointed at a directory outside the library.
func descriptorInventoryDirectories(t *testing.T) []string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the probe source")
	}
	directory := os.Getenv("WFEATURE_KTF_STORAGE_INVENTORY_DIR")
	if filepath.IsAbs(directory) {
		return []string{directory}
	}
	if directory == "" {
		directory = "ktf"
	}
	return []string{filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", directory)}
}

// storageInventoryRounds reads how long the two KTF probes run a title before
// the first boundary they count, and how many boundaries they count.
func storageInventoryRounds(t *testing.T) (warm, rounds int) {
	t.Helper()
	count := func(name string, fallback int) int {
		value, err := storageinventory.Count(name, fallback)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	// The local continuation probe's own distances: three hundred rounds to
	// get past a title's loading, and a hundred compared after it.
	return count("WFEATURE_KTF_STORAGE_INVENTORY_WARM", 300), count("WFEATURE_KTF_STORAGE_INVENTORY_ROUNDS", 100)
}

// TestLocalKTFStorageInventory runs every local descriptor archive on a manual
// clock and a store of its own, and counts its storage tables before each
// round. It is opt-in for the reason every local probe is.
//
//	WFEATURE_KTF_STORAGE_INVENTORY=1 go test -run TestLocalKTFStorageInventory -v ./internal/platform/ktf
//
// WFEATURE_KTF_STORAGE_INVENTORY_WARM is how many rounds a title runs before
// its first boundary is counted and _ROUNDS how many boundaries are counted;
// _DIR names another directory under var/games, or any directory when it is
// absolute. WFEATURE_KTF_ONLY keeps the archives whose name contains it and
// WFEATURE_KTF_MAX_STEPS widens the instruction ceiling, as they do for the
// other local probes. WFEATURE_STORAGE_INVENTORY_OUT names a directory outside
// the repository for one record per archive.
//
// An archive is never a failure here. One that does not start, or stops, is a
// line saying so beside the boundaries it reached.
func TestLocalKTFStorageInventory(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_STORAGE_INVENTORY") != "1" {
		t.Skip("set WFEATURE_KTF_STORAGE_INVENTORY=1 to count the storage tables of ignored local KTF archives")
	}
	warm, rounds := storageInventoryRounds(t)
	output, err := storageinventory.OutputDirectory()
	if err != nil {
		t.Fatal(err)
	}
	runDescriptorInventory(t, descriptorInventoryDirectories(t), os.Getenv("WFEATURE_KTF_ONLY"), warm, rounds, localAcceptanceMaxSteps(t), output)
}

// runDescriptorInventory is the probe with its inputs named, so that the
// authored archive can be put through all of it.
func runDescriptorInventory(t *testing.T, directories []string, only string, warm, rounds int, maxSteps uint64, output string) []storageinventory.ArchiveRecord {
	t.Helper()
	files := storageinventory.Archives(directories, only)
	if len(files) == 0 {
		t.Logf("no local KTF archives in %s", strings.Join(directories, " or "))
	}
	var mutex sync.Mutex
	var records []storageinventory.ArchiveRecord
	t.Run("archives", func(t *testing.T) {
		for _, file := range files {
			name := filepath.Base(file)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				record := descriptorInventoryOfArchive(t, file, name, warm, rounds, maxSteps)
				mutex.Lock()
				records = append(records, record)
				mutex.Unlock()
			})
		}
	})
	sort.Slice(records, func(i, j int) bool { return records[i].Archive < records[j].Archive })
	logStorageInventory(t, descriptorInventoryLayout, records, warm, rounds, output)
	return records
}

// logStorageInventory prints a probe's archives and its library summary, and
// writes the records when a directory was named for them.
func logStorageInventory(t *testing.T, layout storageinventory.Layout, records []storageinventory.ArchiveRecord, warm, rounds int, output string) {
	t.Helper()
	for _, record := range records {
		t.Log(layout.Line(record))
	}
	summary := layout.Summarize(records)
	for _, line := range layout.Table(summary) {
		t.Log(line)
	}
	if output == "" {
		return
	}
	path, err := layout.Write(output, layout.Run(t.Name(), warm, rounds), records, summary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("records written to %s", path)
}

// storageInventoryKeys are pressed in turn, each held long enough for a title
// that reads the pad on its own schedule to see it. A title that never leaves
// its first screen never reaches the code that saves.
const (
	storageInventoryKeyPeriod = 40
	storageInventoryKeyPress  = 10
)

// descriptorInventoryOfArchive runs one archive and answers its record.
func descriptorInventoryOfArchive(t *testing.T, file, name string, warm, rounds int, maxSteps uint64) (record storageinventory.ArchiveRecord) {
	tally := descriptorInventoryLayout.NewTally()
	digest, variant := "", ""
	// A panic in one title is that title's result and a failed subtest. It is
	// not the end of the library, which would cost every archive after it.
	defer func() {
		if failure := recover(); failure != nil {
			record = tally.Record(name, digest, variant, fmt.Sprintf("panicked: %v", failure))
			t.Error(record.Result)
		}
	}()
	data, err := os.ReadFile(file)
	if err != nil {
		return tally.Record(name, digest, variant, "skipped: read: "+storageinventory.Reason(err))
	}
	sum := sha256.Sum256(data)
	digest = hex.EncodeToString(sum[:])
	if isNativePackageArchive(data) {
		return tally.Record(name, digest, "native", "skipped: the earlier native package, which this probe does not drive")
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		return tally.Record(name, digest, variant, "skipped: store: "+storageinventory.Reason(err))
	}
	keys := make([]int32, 0, len(localLadderKeys))
	for _, key := range localLadderKeys {
		if code, known := KeyCodeByName(key); known {
			keys = append(keys, code)
		}
	}
	ctx := context.Background()
	round := func(session *Session, tick int) error {
		key := keys[tick/storageInventoryKeyPeriod%len(keys)]
		switch tick % storageInventoryKeyPeriod {
		case storageInventoryKeyPress:
			if err := session.SendKey(ctx, KeyPressed, key); err != nil {
				return err
			}
		case storageInventoryKeyPress + localLadderHoldTicks:
			if err := session.SendKey(ctx, KeyReleased, key); err != nil {
				return err
			}
		}
		if _, err := session.Tick(ctx); err != nil {
			return err
		}
		session.SkipToNextDeadline()
		return nil
	}
	// A title that ends its first launch on the handset's restart notice is
	// launched again over the saves that launch left, the way a person would.
	var session *Session
	for launch := 1; session == nil; launch++ {
		started, err := StartSession(ctx, data, SessionOptions{
			MaxSteps: maxSteps, Clock: NewManualClock(time.Unix(1700000000, 0)), SaveStore: store,
		})
		if err != nil {
			if errors.Is(err, ErrGuestExited) && launch < localLadderLaunches {
				continue
			}
			return tally.Record(name, digest, variant, "skipped: start: "+storageinventory.Reason(err))
		}
		variant = "java"
		if started.Client.IsModule() {
			variant = "module"
		}
		var failure error
		for tick := 0; tick < warm && failure == nil; tick++ {
			failure = round(started, tick)
		}
		if failure == nil {
			session = started
			break
		}
		started.Close()
		if !errors.Is(failure, ErrGuestExited) {
			return tally.Record(name, digest, variant, "skipped: round: "+storageinventory.Reason(failure))
		}
		if launch == localLadderLaunches {
			return tally.Record(name, digest, variant, "skipped: exited before the first boundary on every launch")
		}
	}
	defer session.Close()

	note := func(column int, text string) { tally.Example(descriptorInventoryNames[column], text) }
	result := "ok"
	for boundary := 0; boundary < rounds; boundary++ {
		counts := inventoryDescriptorStorage(session.Client, nil, note)
		tally.Observe(counts[:])
		if err := round(session, warm+boundary); err != nil {
			if errors.Is(err, ErrGuestExited) {
				result = fmt.Sprintf("ended: the title exited at round %d", boundary)
			} else {
				result = fmt.Sprintf("stopped: round %d: %s", boundary, storageinventory.Reason(err))
			}
			break
		}
	}
	return tally.Record(name, digest, variant, result)
}

// # The native package
//
// A native title's write goes to the session's own copy of the file and is
// marked for the store; the store receives it when the file is closed, at the
// end of a frame, or when the session ends. Between those a key is unsaved,
// and a module that registered no frame callback keeps it that way across
// rounds. The counters:
//
//	open_files           file objects the module holds
//	writable_files       the ones opened in a mode that may write
//	written_entries      names in the session's written table
//	unsaved_keys         the ones the store has not been given
//	stale_open_files     open files whose buffer is not what an open of the
//	                     same name would read from the store now
//	stale_open_saved     the stale ones whose key is not unsaved, so nothing
//	                     pending explains the difference
//	stale_written        written entries that are not the store's bytes
//	stale_written_saved  the stale ones that are not unsaved: a write the
//	                     store refused, or a key changed behind the session
//	shared_keys          keys two or more files are open on
//	store_failures       store writes refused so far
//	unreadable           lookups the store answered with an error
//
// The store is asked through a second platform over the same archive and the
// same store with an empty written table, for the reason given above.
const (
	nativeOpenFiles = iota
	nativeWritableFiles
	nativeWrittenEntries
	nativeUnsavedKeys
	nativeStaleOpenFiles
	nativeStaleOpenSaved
	nativeStaleWritten
	nativeStaleWrittenSaved
	nativeSharedKeys
	nativeStoreFailures
	nativeUnreadable
	nativeColumns
)

// nativeInventory is one boundary: a count per column.
type nativeInventory [nativeColumns]int

var nativeInventoryNames = [nativeColumns]string{
	nativeOpenFiles:         "open_files",
	nativeWritableFiles:     "writable_files",
	nativeWrittenEntries:    "written_entries",
	nativeUnsavedKeys:       "unsaved_keys",
	nativeStaleOpenFiles:    "stale_open_files",
	nativeStaleOpenSaved:    "stale_open_saved",
	nativeStaleWritten:      "stale_written",
	nativeStaleWrittenSaved: "stale_written_saved",
	nativeSharedKeys:        "shared_keys",
	nativeStoreFailures:     "store_failures",
	nativeUnreadable:        "unreadable",
}

var nativeInventoryLayout = storageinventory.Layout{
	Platform: "ktf-native",
	Counters: nativeInventoryNames[:],
	Groups: []storageinventory.Group{
		{Name: "pending", Counters: []string{"unsaved_keys"}},
		{Name: "stale", Counters: []string{"stale_open_saved", "stale_written_saved"}},
	},
}

// inventoryNativeStorage counts one boundary of a native platform.
func inventoryNativeStorage(platform *NativePlatform, note func(column int, name string)) nativeInventory {
	var counts nativeInventory
	mark := func(column int, name string) {
		counts[column]++
		if note != nil {
			note(column, name)
		}
	}
	view := &NativePlatform{archive: platform.archive, saves: platform.saves}
	keys := make(map[string]int, len(platform.files))
	for _, file := range platform.files {
		if file == nil {
			continue
		}
		counts[nativeOpenFiles]++
		if file.writable {
			counts[nativeWritableFiles]++
		}
		keys[file.key]++
		stored, _, err := view.contents(file.name)
		if err != nil {
			mark(nativeUnreadable, file.name)
			continue
		}
		if !bytes.Equal(file.data, stored) {
			mark(nativeStaleOpenFiles, file.name)
			if !platform.unsaved[file.key] {
				mark(nativeStaleOpenSaved, file.name)
			}
		}
	}
	for key, files := range keys {
		if files > 1 {
			mark(nativeSharedKeys, key)
		}
	}
	for key, data := range platform.written {
		counts[nativeWrittenEntries]++
		if platform.unsaved[key] {
			mark(nativeUnsavedKeys, key)
		}
		// With no store there is nothing for an entry to differ from: the
		// session's copy is the only one there is.
		if platform.saves == nil {
			continue
		}
		stored, present, err := backend.ReadSave(platform.saves, nativeSaveKey(key))
		if err != nil {
			mark(nativeUnreadable, key)
			continue
		}
		if !present || !bytes.Equal(data, stored) {
			mark(nativeStaleWritten, key)
			if !platform.unsaved[key] {
				mark(nativeStaleWrittenSaved, key)
			}
		}
	}
	counts[nativeStoreFailures] = platform.storeFailures
	return counts
}

// nativeInventoryFixtures are the authored native packages the probe runs. No
// native package is in the local library, so what this counts is whatever an
// authored one does with its files; a fixture that writes one is a line here.
var nativeInventoryFixtures = []struct {
	name    string
	archive func() ([]byte, error)
}{
	{"authored/native-checkpoint", testfixture.KTFNativeCheckpointArchive},
}

// TestKTFNativeStorageInventory runs the authored native packages the way the
// descriptor probe runs a local archive, and counts the native platform's
// file tables before each round. It reads nothing from the local library and
// is opt-in only so that the two KTF inventories are one command.
//
//	WFEATURE_KTF_STORAGE_INVENTORY=1 go test -run TestKTFNativeStorageInventory -v ./internal/platform/ktf
func TestKTFNativeStorageInventory(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_STORAGE_INVENTORY") != "1" {
		t.Skip("set WFEATURE_KTF_STORAGE_INVENTORY=1 to count the file tables of the authored KTF native packages")
	}
	warm, rounds := storageInventoryRounds(t)
	output, err := storageinventory.OutputDirectory()
	if err != nil {
		t.Fatal(err)
	}
	var records []storageinventory.ArchiveRecord
	for _, fixture := range nativeInventoryFixtures {
		archive, err := fixture.archive()
		if err != nil {
			t.Fatalf("%s: %v", fixture.name, err)
		}
		records = append(records, nativeInventoryOfArchive(t, archive, fixture.name, warm, rounds))
	}
	logStorageInventory(t, nativeInventoryLayout, records, warm, rounds, output)
}

// nativeInventoryOfArchive runs one native package and answers its record.
func nativeInventoryOfArchive(t *testing.T, archive []byte, name string, warm, rounds int) (record storageinventory.ArchiveRecord) {
	tally := nativeInventoryLayout.NewTally()
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	const variant = "native"
	defer func() {
		if failure := recover(); failure != nil {
			record = tally.Record(name, digest, variant, fmt.Sprintf("panicked: %v", failure))
			t.Error(record.Result)
		}
	}()
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		return tally.Record(name, digest, variant, "skipped: store: "+storageinventory.Reason(err))
	}
	ctx := context.Background()
	clock := NewManualClock(time.Unix(1700000000, 0))
	session, err := StartNativeSession(ctx, archive, NativeSessionOptions{Clock: clock, SaveStore: store})
	if err != nil {
		return tally.Record(name, digest, variant, "skipped: start: "+storageinventory.Reason(err))
	}
	defer session.Close()
	round := func(tick int) error {
		// Every digit in turn: an authored package acts on the keys its author
		// chose, and the probe does not know which.
		key := KeyNum0 + int32(tick/storageInventoryKeyPeriod%10)
		switch tick % storageInventoryKeyPeriod {
		case storageInventoryKeyPress:
			if err := session.SendKey(ctx, KeyPressed, key); err != nil {
				return err
			}
		case storageInventoryKeyPress + localLadderHoldTicks:
			if err := session.SendKey(ctx, KeyReleased, key); err != nil {
				return err
			}
		}
		// A module with nothing scheduled still gets its rounds a frame apart.
		if !session.SkipToNextDeadline() {
			clock.Advance(16 * time.Millisecond)
		}
		_, err := session.Tick(ctx)
		return err
	}
	for tick := 0; tick < warm; tick++ {
		if err := round(tick); err != nil {
			return tally.Record(name, digest, variant, "skipped: round: "+storageinventory.Reason(err))
		}
	}
	note := func(column int, text string) { tally.Example(nativeInventoryNames[column], text) }
	result := "ok"
	for boundary := 0; boundary < rounds; boundary++ {
		counts := inventoryNativeStorage(session.platform, note)
		tally.Observe(counts[:])
		if err := round(warm + boundary); err != nil {
			result = fmt.Sprintf("stopped: round %d: %s", boundary, storageinventory.Reason(err))
			break
		}
	}
	return tally.Record(name, digest, variant, result)
}
