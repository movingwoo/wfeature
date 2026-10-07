package lgt

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

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/storageinventory"
)

// The storage inventory
//
// A quick save and a quick load are taken between two ticks. This probe looks
// at what the platform is holding for a title's storage at exactly those
// moments, in every local archive, and counts it. It checks nothing and
// changes nothing: what it produces is the size of each case a rule about
// storage at a boundary has to have an answer for, before the rule is written.
//
// Every counter is read from the client's own tables:
//
//	handles             open MC_fsOpen handles, a Java File's among them
//	writable            the ones opened with write intent
//	dirty               the ones written to and not stored since: a write the
//	                    title issued that only the handle's buffer holds
//	stale               the ones whose buffer is not what an open of the same
//	                    name would read now
//	stale_clean         the stale ones that are not dirty, so that nothing the
//	                    title wrote through the handle explains the difference
//	empty_over_content  writable, clean and empty over a name that holds bytes:
//	                    an open that truncated and has not written yet, or a
//	                    file that gained its content behind the handle
//	no_save_key         the ones whose name the save store has no key for
//	dirty_no_save_key   the dirty ones among those, which nothing can store
//	removed_open        the ones whose path is on the removal list
//	shared_keys         keys two or more handles are open on
//	shared_dirty_keys   keys two or more dirty handles are open on
//	databases           open Java DataBase objects
//	stale_databases     the ones whose records are not the container the store
//	                    holds
//	shared_databases    names two or more open DataBase objects stand for
//	sink_files          output streams opened on a File that hold bytes the
//	                    file has not been handed
//	file_streams        input streams opened on a File and not closed
//	ledgers_loaded      how many of the removal list and the created list the
//	                    session has read
//	stale_ledgers       the read ones that differ from the list the store holds
//	unreadable          lookups the store answered with an error
//	read_error          1 while the session holds a read failure of its own
//
// A write is pending at a boundary when a handle is dirty or a stream bound to
// a File holds bytes. The first is what ending the game stores and the second
// is what it does not, so they are counted apart; they are one group because
// both are bytes a title has issued and the store has not received.
//
// **What the store holds is asked of a second client, not of the session.** A
// file's content on this platform is the removal list, then the save, then the
// packaged copy, and readFile is the one place that order is written down. On
// the running client it answers the removal list from the copy the session
// read once, and it keeps a failed read for the rest of the session — so
// asking it would compare a handle with the session's own memory, and could
// leave the session changed by having been measured. A client that shares the
// archive and the store and has read nothing answers what a first open would
// be answered, and what it retains is discarded with it.
const (
	storageHandles = iota
	storageWritable
	storageDirty
	storageStale
	storageStaleClean
	storageEmptyOverContent
	storageNoSaveKey
	storageDirtyNoSaveKey
	storageRemovedOpen
	storageSharedKeys
	storageSharedDirtyKeys
	storageDatabases
	storageStaleDatabases
	storageSharedDatabases
	storageSinkFiles
	storageFileStreams
	storageLedgersLoaded
	storageStaleLedgers
	storageUnreadable
	storageReadError
	storageColumns
)

// storageInventory is one boundary: a count per column.
type storageInventory [storageColumns]int

var storageInventoryNames = [storageColumns]string{
	storageHandles:          "handles",
	storageWritable:         "writable",
	storageDirty:            "dirty",
	storageStale:            "stale",
	storageStaleClean:       "stale_clean",
	storageEmptyOverContent: "empty_over_content",
	storageNoSaveKey:        "no_save_key",
	storageDirtyNoSaveKey:   "dirty_no_save_key",
	storageRemovedOpen:      "removed_open",
	storageSharedKeys:       "shared_keys",
	storageSharedDirtyKeys:  "shared_dirty_keys",
	storageDatabases:        "databases",
	storageStaleDatabases:   "stale_databases",
	storageSharedDatabases:  "shared_databases",
	storageSinkFiles:        "sink_files",
	storageFileStreams:      "file_streams",
	storageLedgersLoaded:    "ledgers_loaded",
	storageStaleLedgers:     "stale_ledgers",
	storageUnreadable:       "unreadable",
	storageReadError:        "read_error",
}

// storageInventoryHandleLimit is how many open handles get a line of their own
// in the summary. A checkpoint record holds up to maxStateRecords of them
// today; a load that rebuilt each one from the store would have to bound them
// far lower, and this is the bound to ask the library about before choosing it.
const storageInventoryHandleLimit = 4096

var storageInventoryLayout = storageinventory.Layout{
	Platform: "lgt",
	Counters: storageInventoryNames[:],
	Groups: []storageinventory.Group{
		{Name: "pending", Counters: []string{"dirty", "sink_files"}},
		// What a table rebuilt from the store would hold differently although
		// the title has nothing pending there.
		{Name: "stale", Counters: []string{"stale_clean", "stale_databases", "stale_ledgers"}},
	},
	Limits: []storageinventory.Limit{{Counter: "handles", Above: storageInventoryHandleLimit}},
}

// storageInventoryKey is what two handles are compared by to say they are open
// on one file: the save key, folded the way the removal list folds a name, so
// that two spellings a case-folding filesystem keeps as one file count as one.
// A name with no save key is compared as the removal list compares it.
func storageInventoryKey(name string) string {
	if key, err := fileSaveKey(name); err == nil {
		return "key:" + strings.ToLower(key)
	}
	return "name:" + canonicalFileName(name)
}

// sameNameSet reports whether two ledgers list the same names.
func sameNameSet(left, right map[string]bool) bool {
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

// inventoryStorage counts one boundary. note, when it is not nil, is told the
// name each counted condition was seen on.
func inventoryStorage(client *Client, note func(column int, name string)) storageInventory {
	var counts storageInventory
	mark := func(column int, name string) {
		counts[column]++
		if note != nil {
			note(column, name)
		}
	}
	view := &Client{archive: client.archive, saveStore: client.saveStore}
	// A lookup the store refused says nothing about the handle it was for, so
	// it is counted as that and the comparison is left out.
	refused := func(name string) bool {
		if view.saveReadError == nil {
			return false
		}
		view.saveReadError = nil
		mark(storageUnreadable, name)
		return true
	}

	removed := view.removedFiles()
	removedRefused := refused(fileRemovedKey)
	type sharing struct{ handles, dirty int }
	keys := make(map[string]sharing, len(client.files))
	for _, file := range client.files {
		if file == nil {
			continue
		}
		counts[storageHandles]++
		if file.writable {
			counts[storageWritable]++
		}
		if file.dirty {
			mark(storageDirty, file.name)
		}
		if _, err := fileSaveKey(file.name); err != nil {
			mark(storageNoSaveKey, file.name)
			if file.dirty {
				mark(storageDirtyNoSaveKey, file.name)
			}
		}
		if removed[canonicalFileName(file.name)] {
			mark(storageRemovedOpen, file.name)
		}
		key := storageInventoryKey(file.name)
		shared := keys[key]
		shared.handles++
		if file.dirty {
			shared.dirty++
		}
		keys[key] = shared

		// An absent name reads as no bytes, which is what a handle rebuilt over
		// it would hold.
		stored, _ := view.readFile(file.name)
		if refused(file.name) {
			continue
		}
		if !bytes.Equal(file.data, stored) {
			counts[storageStale]++
			if !file.dirty {
				mark(storageStaleClean, file.name)
			}
		}
		if file.writable && !file.dirty && len(file.data) == 0 && len(stored) != 0 {
			mark(storageEmptyOverContent, file.name)
		}
	}
	for key, shared := range keys {
		if shared.handles > 1 {
			mark(storageSharedKeys, key)
		}
		if shared.dirty > 1 {
			mark(storageSharedDirtyKeys, key)
		}
	}

	if run := client.javaRun; run != nil {
		names := make(map[string]int, len(run.databases))
		for _, database := range run.databases {
			if database == nil || database.closed {
				continue
			}
			counts[storageDatabases]++
			names[database.name]++
			stored, exists := view.readFile(databaseFileName(database.name))
			if refused(database.name) {
				continue
			}
			// A container that is not there is rebuilt as a database with no
			// record in it, so only records make the object differ from it.
			if exists && !bytes.Equal(database.encode(), stored) || !exists && len(database.records) != 0 {
				mark(storageStaleDatabases, database.name)
			}
		}
		for name, objects := range names {
			if objects > 1 {
				mark(storageSharedDatabases, name)
			}
		}
		for sink, file := range run.sinkFiles {
			if len(run.sinks[sink]) == 0 {
				continue
			}
			name := "a closed file"
			if open := client.files[run.files[file]]; open != nil {
				name = open.name
			}
			mark(storageSinkFiles, name)
		}
		for stream := range run.streamFiles {
			if held := run.streams[stream]; held != nil && !held.Closed {
				mark(storageFileStreams, held.Name)
			}
		}
	}

	// Unread is the state a session starts in, and a list nobody has read is
	// not a copy of anything.
	if client.removed != nil {
		counts[storageLedgersLoaded]++
		if !removedRefused && !sameNameSet(client.removed, removed) {
			mark(storageStaleLedgers, fileRemovedKey)
		}
	}
	if client.created != nil {
		counts[storageLedgersLoaded]++
		created := view.createdFiles()
		if !refused(fileCreatedKey) && !sameNameSet(client.created, created) {
			mark(storageStaleLedgers, fileCreatedKey)
		}
	}
	if client.saveReadError != nil {
		counts[storageReadError] = 1
	}
	return counts
}

// storageInventoryDirectories is where the local archives are: the two LGT
// directories of the ignored library, or the one the checkpoint probe's own
// variable names. An absolute path is taken as it is, which is how the probe
// is pointed at a directory outside the library.
func storageInventoryDirectories(t *testing.T) []string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the probe source")
	}
	root := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games")
	groups := []string{"lgt", "LGT WIPI 2.X"}
	if directory := os.Getenv("WFEATURE_LGT_CHECKPOINT_DIR"); directory != "" {
		if filepath.IsAbs(directory) {
			return []string{directory}
		}
		groups = []string{directory}
	}
	directories := make([]string, 0, len(groups))
	for _, group := range groups {
		directories = append(directories, filepath.Join(root, group))
	}
	return directories
}

// TestLocalLGTStorageInventory runs every local archive the way the checkpoint
// probe does and counts its storage tables before each tick. It is opt-in for
// the reason every local probe is.
//
//	WFEATURE_LGT_STORAGE_INVENTORY=1 go test -run TestLocalLGTStorageInventory -v ./internal/platform/lgt
//
// The checkpoint probe's own variables apply: WFEATURE_LGT_CHECKPOINT_WARM is
// how many ticks a title runs before its first boundary is counted, _ROUNDS
// how many boundaries are counted, _MATCH keeps the archives whose name
// contains it, and _DIR names another directory under var/games, or any
// directory when it is absolute. WFEATURE_STORAGE_INVENTORY_OUT names a
// directory outside the repository for one record per archive.
//
// An archive is never a failure here. One that does not start, or stops, is a
// line saying so beside the boundaries it reached.
func TestLocalLGTStorageInventory(t *testing.T) {
	if os.Getenv("WFEATURE_LGT_STORAGE_INVENTORY") != "1" {
		t.Skip("set WFEATURE_LGT_STORAGE_INVENTORY=1 to count the storage tables of ignored local LGT archives")
	}
	count := func(name string, fallback int) int {
		value, err := storageinventory.Count(name, fallback)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	warm, rounds := count("WFEATURE_LGT_CHECKPOINT_WARM", 200), count("WFEATURE_LGT_CHECKPOINT_ROUNDS", 150)
	output, err := storageinventory.OutputDirectory()
	if err != nil {
		t.Fatal(err)
	}
	directories := storageInventoryDirectories(t)
	files := storageinventory.Archives(directories, os.Getenv("WFEATURE_LGT_CHECKPOINT_MATCH"))
	if len(files) == 0 {
		t.Logf("no local LGT archives in %s", strings.Join(directories, " or "))
	}
	var mutex sync.Mutex
	var records []storageinventory.ArchiveRecord
	t.Run("archives", func(t *testing.T) {
		for _, file := range files {
			name := filepath.Base(filepath.Dir(file)) + "/" + filepath.Base(file)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				record := storageInventoryOfArchive(t, file, name, warm, rounds)
				mutex.Lock()
				records = append(records, record)
				mutex.Unlock()
			})
		}
	})
	sort.Slice(records, func(i, j int) bool { return records[i].Archive < records[j].Archive })
	for _, record := range records {
		t.Log(storageInventoryLayout.Line(record))
	}
	summary := storageInventoryLayout.Summarize(records)
	for _, line := range storageInventoryLayout.Table(summary) {
		t.Log(line)
	}
	if output == "" {
		return
	}
	path, err := storageInventoryLayout.Write(output, storageInventoryLayout.Run(t.Name(), warm, rounds), records, summary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("records written to %s", path)
}

// storageInventoryOfArchive runs one archive and answers its record.
func storageInventoryOfArchive(t *testing.T, file, name string, warm, rounds int) (record storageinventory.ArchiveRecord) {
	tally := storageInventoryLayout.NewTally()
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

	ctx := context.Background()
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		return tally.Record(name, digest, variant, "skipped: store: "+storageinventory.Reason(err))
	}
	keys := []uint32{'5', 0xFFFFFFFE, '5', 0xFFFFFFFC, '5', 0xFFFFFFFF, '5', 0xFFFFFFFD}
	press := func(session *Session, tick int) {
		// A key every so often moves a title off the screen it would otherwise
		// sit on for the whole run, and a title that never leaves its first
		// screen never reaches the code that saves.
		if tick%25 == 10 {
			session.SendKey(true, keys[tick/25%len(keys)])
		}
		if tick%25 == 13 {
			session.SendKey(false, keys[tick/25%len(keys)])
		}
	}
	// A title whose first run installs itself and asks to be restarted is
	// started again over the saves that run left, the way a person would.
	var session *Session
	for run := 1; session == nil; run++ {
		started, err := StartSession(ctx, data, SessionOptions{SaveStore: store})
		if err != nil {
			if errors.Is(err, ErrGuestExited) && run < 3 {
				continue
			}
			return tally.Record(name, digest, variant, "skipped: start: "+storageinventory.Reason(err))
		}
		variant = "clet"
		if started.client.javaApplication {
			variant = "java"
		}
		var failure error
		for tick := 0; tick < warm && failure == nil; tick++ {
			press(started, tick)
			failure = started.Tick(ctx)
		}
		if failure == nil {
			session = started
			break
		}
		_ = started.Close(ctx)
		if !errors.Is(failure, ErrGuestExited) {
			return tally.Record(name, digest, variant, "skipped: tick: "+storageinventory.Reason(failure))
		}
		if run == 3 {
			return tally.Record(name, digest, variant, "skipped: exited before the first boundary on three runs")
		}
	}
	defer session.Close(ctx)

	note := func(column int, text string) { tally.Example(storageInventoryNames[column], text) }
	result := "ok"
	for round := 0; round < rounds; round++ {
		counts := inventoryStorage(session.client, note)
		tally.Observe(counts[:])
		press(session, warm+round)
		if err := session.Tick(ctx); err != nil {
			if errors.Is(err, ErrGuestExited) {
				result = fmt.Sprintf("ended: the title exited at round %d", round)
			} else {
				result = fmt.Sprintf("stopped: round %d: %s", round, storageinventory.Reason(err))
			}
			break
		}
	}
	return tally.Record(name, digest, variant, result)
}
