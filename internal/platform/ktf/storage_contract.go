package ktf

import (
	"fmt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"sort"
	"strconv"
	"strings"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The rules every platform's WIPI storage keeps alike are in
// docs/wipi-storage.md. What is here is the part of them this platform shares
// between its WIPI C and Java halves: how long a name may be, how much a title
// has stored, and what the Java half calls a directory.

// maxStoredBytes bounds one file, and one database's records together. It is a
// bound on what a title's numbers may make the Host hold, not a quota: no
// save comes near it.
const maxStoredBytes = 4 << 20

// storageUsed counts each file and database the title has written at its size
// less the size of the archive's file of the same name, never below zero. A
// title's own content that it shipped is not space it used, and a save that
// rewrites a packaged file only uses what it added. Databases count their
// record bytes. The lists the platform keeps beside them are not counted.
//
// What this session holds in memory wins over the store for the same name,
// because it is newer, and a session without a store has nothing else.
func (runtime *initializationRuntime) platformStorageUsed() (int, error) {
	used := 0
	add := func(size, packaged int) {
		if size > packaged {
			used += size - packaged
		}
	}

	seen := make(map[string]bool)
	for name, store := range runtime.CFiles {
		seen[name] = true
		add(len(store.Data), store.Packaged)
	}
	if err := runtime.addStoredFiles(wipicrt.CFileScope, seen, runtime.RemovedDatabases(), add); err != nil {
		return 0, err
	}
	seen = make(map[string]bool)
	for name, data := range runtime.GuestFiles {
		name = wipicrt.WIPICFileKey(name)
		seen[name] = true
		add(len(data), len(runtime.client.Files[name]))
	}
	if err := runtime.addStoredFiles(wipicrt.GuestFileScope, seen, runtime.RemovedGuestFiles(), add); err != nil {
		return 0, err
	}

	recordBytes := func(records [][]byte) int {
		total := 0
		for _, record := range records {
			total += len(record)
		}
		return total
	}
	// A database the archive ships in a database's own form counts what its
	// records hold. Otherwise the archive's file of the same name is the
	// packaged copy, as the contract words it: a title that keeps a shipped
	// file as a database record — one keeps 1.4 MB of its own data so — had
	// that whole file counted, was told it had no room left, and refused to
	// start (docs/history/ktf.md#ktf-storage-review-2026-10-22).
	packagedBytes := func(name string) int {
		if records, ok := runtime.packagedRecordDatabase(name, 0); ok {
			return recordBytes(records)
		}
		return len(runtime.client.Files[strings.TrimPrefix(name, "/")])
	}
	seen = make(map[string]bool)
	for name, store := range runtime.recordDatabases {
		seen[name] = true
		add(recordBytes(store.records), packagedBytes(name))
	}
	for name, store := range runtime.databases {
		seen["\x00"+name] = true
		add(recordBytes(store.records), packagedBytes(name))
	}
	for _, scope := range []struct {
		name, removedKey, prefix string
	}{{wipicrt.RecordDatabaseScope, recordDatabaseRemovedKey, ""}, {wipicrt.JavaDatabaseScope, javaDatabaseRemovedKey, "\x00"}} {
		names, err := runtime.ListSaves(scope.name)
		if err != nil {
			return 0, err
		}
		removed := runtime.recordDatabaseRemovals(scope.removedKey)
		for _, name := range names {
			if seen[scope.prefix+name] || removed[name] || wipicrt.ReservedStorageName(scope.name, name) {
				continue
			}
			data, ok := runtime.LoadSave(scope.name + "/" + name)
			if !ok {
				continue
			}
			records, err := wipicrt.DecodeSaveRecords(data)
			if err != nil {
				continue
			}
			add(recordBytes(records), packagedBytes(name))
		}
	}
	return used, nil
}

// addStoredFiles counts the saved files of one scope that this session holds
// no copy of.
func (runtime *initializationRuntime) addStoredFiles(scope string, seen, removed map[string]bool, add func(size, packaged int)) error {
	names, err := runtime.ListSaves(scope)
	if err != nil {
		return err
	}
	for _, name := range names {
		if seen[name] || removed[name] || wipicrt.ReservedStorageName(scope, name) {
			continue
		}
		if data, ok := runtime.LoadSave(scope + "/" + name); ok {
			add(len(data), len(runtime.client.Files[name]))
		}
	}
	return nil
}

// guestDirectoryListKey holds the directories FileSystem.mkdir made, which
// is what keeps one that holds nothing after a restart. See directoryListKey
// for the C table's.
const guestDirectoryListKey = "fs/.dirs"

// guestDirectories is the set of made directories by path, read from the
// store once per session.
func (runtime *initializationRuntime) guestDirectories() map[string]bool {
	if runtime.madeGuestDirectories != nil {
		return runtime.madeGuestDirectories
	}
	runtime.madeGuestDirectories = make(map[string]bool)
	if data, exists := runtime.LoadSave(guestDirectoryListKey); exists {
		for _, line := range wipicrt.SplitRemovalList(data) {
			runtime.madeGuestDirectories[line] = true
		}
	}
	return runtime.madeGuestDirectories
}

// markGuestDirectory records or forgets one made directory and writes the
// list back.
func (runtime *initializationRuntime) markGuestDirectory(path string, made bool) error {
	return runtime.SaveChanges(nil, guestDirectoryListKey, runtime.guestDirectories(), map[string]bool{path: made})
}

// guestFileNames answers every file the Java side can open, by path: the
// archive's files, this session's writes and the saves of every session, less
// what was removed and the platform's own lists.
func (runtime *initializationRuntime) guestFileNames() (map[string]bool, error) {
	removed := wipicrt.ListingPaths(runtime.RemovedGuestFiles())
	names := make(map[string]bool)
	add := func(name string) {
		path := wipicrt.ListingPath(name)
		if path != "" && !removed[path] && !wipicrt.ReservedStorageName(wipicrt.GuestFileScope, path) {
			names[path] = true
		}
	}
	for _, table := range []map[string][]byte{runtime.client.Files, runtime.GuestFiles} {
		for name := range table {
			add(name)
		}
	}
	saved, err := runtime.ListSaves(wipicrt.GuestFileScope)
	if err != nil {
		return nil, err
	}
	for _, name := range saved {
		add(name)
	}
	return names, nil
}

// guestDirectoryEntries answers the names directly below a directory, files
// and made directories alike, in name order.
func (runtime *initializationRuntime) guestDirectoryEntries(directory string) ([]string, error) {
	names, err := runtime.guestFileNames()
	if err != nil {
		return nil, err
	}
	for path := range runtime.guestDirectories() {
		names[path] = true
	}
	prefix := wipicrt.ListingPath(directory)
	if prefix != "" {
		prefix += "/"
	}
	entries := make(map[string]bool)
	for name := range names {
		rest, under := strings.CutPrefix(name, prefix)
		if !under || rest == "" {
			continue
		}
		if cut := strings.IndexByte(rest, '/'); cut >= 0 {
			rest = rest[:cut]
		}
		entries[rest] = true
	}
	sorted := make([]string, 0, len(entries))
	for entry := range entries {
		sorted = append(sorted, entry)
	}
	sort.Strings(sorted)
	return sorted, nil
}

// guestIsFile answers whether a file of this name is there for the Java side.
func (runtime *initializationRuntime) guestIsFile(name string) bool {
	if wipicrt.ListingPath(name) == "" {
		return false
	}
	_, exists := runtime.GuestFile(name)
	return exists
}

// guestIsDirectory answers whether a name is a directory: the root, one mkdir
// made, or one a file lies below. A name that is a file is not a directory.
func (runtime *initializationRuntime) guestIsDirectory(name string) (bool, error) {
	path := wipicrt.ListingPath(name)
	if path == "" {
		return true, nil
	}
	if runtime.guestIsFile(name) {
		return false, nil
	}
	if runtime.guestDirectories()[path] {
		return true, nil
	}
	entries, err := runtime.guestDirectoryEntries(path)
	return len(entries) > 0, err
}

// databaseName reads a DataBase name argument by the rule a file name is read
// by, so the spellings of one path are one database; a null name, and one the
// rule refuses, are the DataBaseException every DataBase call declares.
func databaseName(value jvm.Value) (string, error) {
	object, err := value.Reference()
	if err != nil {
		return "", err
	}
	if object == nil {
		return "", runtimeDataBaseException("a null database name")
	}
	name, ok := jvm.StringText(object)
	if !ok {
		return "", fmt.Errorf("DataBase name is not a string")
	}
	if wipicrt.NameTooLong(name) {
		return "", runtimeDataBaseException(fmt.Sprintf("a name of %d bytes is longer than %d", len(name), wipicrt.MaxWIPIFileName))
	}
	return wipicrt.ListingPath(strings.ReplaceAll(name, "\\", "/")), nil
}

// javaDatabaseSizeKey holds the record size each Java database was created
// with, one "<size> <name>" line each. The record list has no room for it,
// and the specification makes getRecordSize answer it.
const javaDatabaseSizeKey = "jdb/.sizes"

// javaDatabaseSizes is the size table, read from the store once per session.
func (runtime *initializationRuntime) javaDatabaseSizes() map[string]int32 {
	if runtime.databaseSizes != nil {
		return runtime.databaseSizes
	}
	runtime.databaseSizes = make(map[string]int32)
	if data, exists := runtime.LoadSave(javaDatabaseSizeKey); exists {
		for _, line := range wipicrt.SplitRemovalList(data) {
			sizeText, name, found := strings.Cut(line, " ")
			size, err := strconv.ParseInt(sizeText, 10, 32)
			if found && err == nil && size > 0 && name != "" {
				runtime.databaseSizes[name] = int32(size)
			}
		}
	}
	return runtime.databaseSizes
}

// setJavaDatabaseSize records a database's record size, or forgets it for a
// size of 0, and writes the table back.
func (runtime *initializationRuntime) setJavaDatabaseSize(name string, size int32) error {
	sizes := runtime.javaDatabaseSizes()
	if sizes[name] == size || size < 0 {
		return nil
	}
	staged := make(map[string]int32, len(sizes)+1)
	for key, value := range sizes {
		staged[key] = value
	}
	if size == 0 {
		delete(staged, name)
	} else {
		staged[name] = size
	}
	lines := make([]string, 0, len(staged))
	for key, value := range staged {
		lines = append(lines, strconv.Itoa(int(value))+" "+key)
	}
	if err := runtime.SaveChanges(map[string][]byte{javaDatabaseSizeKey: wipicrt.JoinRemovalList(lines)}, javaDatabaseRemovedKey, runtime.recordDatabaseRemovals(javaDatabaseRemovedKey), nil); err != nil {
		return err
	}
	runtime.databaseSizes = staged
	return nil
}
