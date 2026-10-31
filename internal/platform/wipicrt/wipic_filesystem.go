package wipicrt

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// **This file is WIPI C table 7, which is the filesystem.** Every slot in it
// sits where the specification's `MC_fs*` section prints one — open, read,
// write, close and seek at 0 to 4, the file attributes at 5, remove at 6,
// rename at 7, mkdir and rmdir at 8 and 9, the listing at 10, the total and
// the free space at 11 and 12, the mode at 13, the entry count at 14,
// `MC_fsTell` at 15 and `MC_fsIsExist` at 16 — and a title's own log names the
// call it makes here as `MC_fsOpen`. The directory slots are in
// wipic_directory.go. See docs/history/ktf.md, "The two storage tables".
//
// **Two things deliberately kept the older name.** The save keys begin `db/`
// and are a player's own files, so renaming them would orphan every save
// already written; and the diagnostic counter names are what a stored report
// is read against. Both are records rather than code, and the code is what
// moved.
//
// runtimeCFile keeps one named file in process memory: read and written
// through a per-handle byte cursor. Durable persistence joins with the Host
// save path.
type RuntimeCFile struct {
	Name string
	Data []byte
	// Packaged is how many bytes of this store the archive shipped. The
	// archive's own data is not storage the player consumed — on a handset it
	// arrives with the program — so it does not count against the save budget
	// MC_fsAvailable reports. Without this a title that packages its maps
	// and its text as databases reads its own content as a full disk: one
	// here ships 2 MB that way and could not start a new game, because the
	// budget was gone before the player had saved anything.
	Packaged int
}

// Persist writes the record through the Host save store when one is attached.
func (store *RuntimeCFile) Persist(runtime *Runtime) error {
	return runtime.SaveChanges(map[string][]byte{"db/" + store.Name: store.Data}, DatabaseRemovedKey, runtime.RemovedDatabases(), map[string]bool{store.Name: false})
}

type RuntimeCFileHandle struct {
	Store    *RuntimeCFile
	Position int
}

const (
	MaxCFileBytes = 4 << 20
	// CFileHandleBit tags a file handle. It has to stay inside
	// a signed 16-bit value: a handset's handle is small, and a title that
	// keeps one in a `short` sign-extends what the open returned before it
	// hands it back. One local title does exactly that — `lsl #16; asr #16` on
	// the result, then a "negative means it failed" test — so a handle with a
	// high bit set came back as a number this platform had never issued. The
	// reads on it were refused, silently, leaving the header buffer untouched;
	// the title then sized an allocation from the difference of two words that
	// were never written and asked for four gigabytes.
	CFileHandleBit  = 0x1000
	MaxCFileHandles = 0x0fff
)

const (
	WIPICFileOpen         = 0
	wipicFileStreamRead   = 1
	WIPICFileStreamWrite  = 2
	WIPICFileClose        = 3
	wipicFileSelectRecord = 4
	WIPICFileStatByName   = 5
	// MC_fsRemove takes the file's name and its area, not a handle: the caller
	// closes the file first and then names it. One title deletes the
	// certificate it could not renew and expects the next question about it to
	// answer "no such file".
	WIPICFileDelete = 6
	// MC_fsRename takes two names and an access area, and is how a title that
	// writes a save to a scratch name commits it: close the scratch file, then
	// rename it over the real one. One local title does exactly that, and
	// while the slot was missing its save was written and then never arrived.
	WIPICFileRename = 7
	// MC_fsMkDir takes a name and an access area — 1 is the program's own
	// directory, which is the only area this platform has. **Nothing here
	// holds a directory**: a name is a key in one flat store, so making one is
	// the whole of what a directory is, and a name that has been made is what
	// says it exists.
	WIPICFileMakeDirectory   = 8
	WIPICFileRemoveDirectory = 9
	// Slots 10 and 11 were the record count and the record size of an
	// earlier reading of this table, which took the first register for a
	// handle. They are `MC_fsList` and `MC_fsTotalSpace`: the specification's
	// order says so, and a title that asks 11 for the total space with no
	// argument at all was told M_E_INVALID, reported that the handset had no
	// room to save, and never left its title screen.
	WIPICFileList       = 10
	WIPICFileTotalSpace = 11
	WIPICFileAvailable  = 12
	WIPICFileSetMode    = 13
	WIPICFileGetCounts  = 14
	WIPICFileTell       = 15
	WIPICFileExists     = 16

	// WIPICFileStorageLimit mirrors the reference KTF per-game storage budget;
	// MC_fsAvailable reports the space still available.
	WIPICFileStorageLimit = 1024 * 1024
)

func (runtime *Runtime) HandleWIPICFileCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case WIPICFileOpen:
		return runtime.WIPICFileOpen(thread)
	case wipicFileStreamRead:
		return runtime.WIPICFileStream(thread, false)
	case WIPICFileStreamWrite:
		return runtime.WIPICFileStream(thread, true)
	case WIPICFileClose:
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		delete(runtime.CFileHandles, handle)
		return 0, nil
	case wipicFileSelectRecord:
		return runtime.wipicFileSeek(thread)
	case WIPICFileDelete:
		return runtime.WIPICFileDelete(thread)
	case WIPICFileRename:
		return runtime.WIPICFileRename(thread)
	case WIPICFileStatByName:
		return runtime.wipicFileStatByName(thread)
	case WIPICFileRemoveDirectory:
		return runtime.wipicRemoveDirectory(thread)
	case WIPICFileList:
		return runtime.wipicFileList(thread)
	case WIPICFileTotalSpace:
		// MC_fsTotalSpace takes no argument and answers the size of the whole
		// file system, which here is the per-game budget MC_fsAvailable
		// counts down from, so the free space is never more than the total.
		return WIPICFileStorageLimit, nil
	case WIPICFileAvailable:
		// MC_fsAvailable reports remaining storage: the budget less what the
		// title has stored, as every platform counts it (storageAvailable).
		return runtime.StorageAvailable(), nil
	case WIPICFileExists:
		// MC_fsIsExist answers 0 on success and M_E_NOENT when missing;
		// titles branch to their fresh-init path on the error code. A
		// directory this platform was asked to make counts as something that
		// exists, because the alternative is telling a title that the
		// directory it just made is not there.
		nameAddress, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		name, err := runtime.readFileName(nameAddress)
		if err != nil {
			return 0, fmt.Errorf("read %s file name: %w", runtime.platformName(), err)
		}
		if NameTooLong(name) {
			return WIPICErrorLongName, nil
		}
		if ListingPath(name) == "" {
			// The program's own directory is always there.
			return 0, nil
		}
		if !CFileNameKeyed(name) {
			// Nothing can be stored under it: a name the store cannot keep
			// is M_E_BADFILENAME, as MC_fsFileAttribute answers it.
			return WIPICErrorBadFileName, nil
		}
		exists, err := runtime.cFileExists(name)
		if err != nil {
			runtime.CountDiagnostic(fmt.Sprintf("cdb exists error: %v", err))
			return WIPICErrorGeneric, nil
		}
		runtime.CountDiagnostic(fmt.Sprintf("cdb exists %s -> %t", name, exists))
		if exists {
			return 0, nil
		}
		return WIPICErrorNotFound, nil
	case WIPICFileMakeDirectory:
		return runtime.wipicMakeDirectory(thread)
	case WIPICFileSetMode:
		return runtime.wipicFileSetMode(thread)
	case WIPICFileGetCounts:
		return runtime.wipicFileCount(thread)
	case WIPICFileTell:
		return runtime.wipicFileTell(thread)
	default:
		// The call site is named for the same reason the unimplemented-table
		// error names one: the function number is an index into an array of
		// pointers, so it never appears in the guest's own code, and an
		// address is the only thing about a missing call that can be
		// disassembled.
		return 0, fmt.Errorf("%s filesystem function %d is not implemented%s", runtime.platformCName(),
			function, runtime.CallerSite(thread))
	}
}

// WIPICFileDelete serves MC_fsRemove(name, mode). A store may be
// in memory, persisted, packaged with the archive, or all three, so the delete
// has to cover every place the open would look. The persisted copy cannot be
// unlinked — the save store has no delete — so the name is recorded on a
// removal list the way the guest filesystem records one, and every lookup
// consults it. Without that the database comes back on the next question, and
// a title that deleted it to start again is handed what it just threw away.
func (runtime *Runtime) WIPICFileDelete(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readFileName(nameAddress)
	if err != nil {
		return 0, fmt.Errorf("read %s database name: %w", runtime.platformName(), err)
	}
	if NameTooLong(name) {
		return WIPICErrorLongName, nil
	}
	if ReservedStorageName(CFileScope, name) {
		// Removing writes nil over the key, and these keys are this table's
		// own lists: the deletion list answers as a seed as soon as anything
		// has been deleted, so the removal would go through and wipe it, and
		// every database the title had deleted would be back on the next open.
		return WIPICErrorBadFileName, nil
	}
	if !StorableName(CFileScope, name) {
		// The removal list could not carry it: "a\nb" would come back as the
		// two names a and b, and hide both.
		return WIPICErrorBadFileName, nil
	}
	_, live := runtime.CFiles[name]
	_, seeded := runtime.DatabaseSeed(name)
	runtime.CountDiagnostic(fmt.Sprintf("cdb delete %s -> %t", name, live || seeded))
	if !live && !seeded {
		return WIPICErrorNotFound, nil
	}
	if err := runtime.SaveChanges(map[string][]byte{"db/" + name: nil}, DatabaseRemovedKey, runtime.RemovedDatabases(), map[string]bool{name: true}); err != nil {
		return 0, err
	}
	delete(runtime.CFiles, name)
	// Handles onto the store go with it; a caller holding one after the delete
	// would otherwise keep reading a database nothing else can see.
	for handle, open := range runtime.CFileHandles {
		if open.Store != nil && open.Store.Name == name {
			delete(runtime.CFileHandles, handle)
		}
	}

	return 0, nil
}

// WIPICFileKey is the name a file is stored under here, which is the name the
// guest passed with its leading separators removed.
//
// The specification says every file name in this section is an absolute path
// and that a name is resolved inside the area `aMode` names, so a program's own
// `/save.dat` and its `save.dat` are one file on a handset — the leading
// separator is the root of the program's own directory, not a different place.
// The store behind this table is flat, so the two spellings would otherwise be
// two files, and a title is free to use both: one local title creates its save
// under a bare name, renames it, and then opens it back with a leading slash.
// It wrote its save and could not find it afterwards.
//
// Only the leading separators go. What follows one is part of the name, because
// a title that keeps its files under a directory of its own is naming different
// files with it. A backslash is a separator like a slash, as every platform
// reads one (docs/wipi-storage.md); the store refuses one in a key, so no save
// was ever written under a name that had one.
func WIPICFileKey(name string) string {
	return strings.TrimLeft(strings.ReplaceAll(name, "\\", "/"), "/")
}

// readFileName reads a name argument of this table and answers the key it
// stands for. Every slot that takes a name goes through it, because two slots
// that disagree about which file a name means are worse than either rule.
func (runtime *Runtime) readFileName(address uint32) (string, error) {
	// Read further than any name may be, so a long one is the
	// specification's M_E_LONGNAME rather than a string that never ended:
	// the same bound every platform reads a name to.
	name, err := runtime.ReadCString(address, MaxFileNameRead)
	if err != nil {
		return "", err
	}
	return WIPICFileKey(name), nil
}

// WIPICFileRename serves MC_fsRename(oldName, newName, aMode): "파일이름을
// 변경한다", with the one refusal the specification spells out — a name that is
// already taken cannot be renamed onto, which answers `M_E_EXIST`.
//
// A rename is a move in one flat store, so it is the same three places an open
// looks at, in the same order: the live store, then the archive's packaged copy
// of the old name. Handles follow the file rather than break, because they hold
// the store itself and only its name changes; a title that renames a file it
// still has open keeps reading the bytes it opened.
//
// The persisted side needs both halves. The bytes are written under the new key
// and the old name goes on the removal list `MC_fsRemove` keeps, because the
// save store has no delete: without that the next open of the old name would be
// handed back the copy the rename was supposed to move away.
func (runtime *Runtime) WIPICFileRename(thread *armcore.Thread) (uint32, error) {
	oldAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	newAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	oldName, err := runtime.readFileName(oldAddress)
	if err != nil {
		return 0, fmt.Errorf("read %s file name: %w", runtime.platformName(), err)
	}
	newName, err := runtime.readFileName(newAddress)
	if err != nil {
		return 0, fmt.Errorf("read %s file name: %w", runtime.platformName(), err)
	}
	if NameTooLong(oldName) || NameTooLong(newName) {
		return WIPICErrorLongName, nil
	}
	if oldName == "" || newName == "" {
		return WIPICErrorGeneric, nil
	}
	if !StorableName(CFileScope, oldName) || !StorableName(CFileScope, newName) {
		// A name that is not a key of its own, or that the removal list could
		// not carry. Refused before either name is looked up: a name that
		// fails its key fails the read, and that read failure stays.
		return WIPICErrorBadFileName, nil
	}
	if oldName == newName {
		// The same file under the same name: there is nothing to move, and
		// answering M_E_EXIST because the destination is occupied by the
		// source would refuse a rename that has already happened.
		runtime.CountDiagnostic(fmt.Sprintf("fs rename %s -> itself", oldName))
		return 0, nil
	}
	_, taken := runtime.CFiles[newName]
	if _, seeded := runtime.DatabaseSeed(newName); seeded {
		taken = true
	}
	if runtime.CreatedDirectories()[newName] {
		taken = true
	}
	if taken {
		runtime.CountDiagnostic(fmt.Sprintf("fs rename %s -> %s exists", oldName, newName))
		return WIPICErrorExists, nil
	}
	if ReservedStorageName(CFileScope, oldName) || ReservedStorageName(CFileScope, newName) {
		// Either end. Renaming *onto* a list this table keeps would replace it
		// with the file's bytes, and the next session would read those bytes
		// as the list; renaming one *away* is worse, because the source is
		// emptied afterwards — the list answers as a seed as soon as anything
		// has been deleted, so the rename would carry it off under another
		// name and leave nothing behind, bringing back every database the
		// title had deleted. Refused before anything moves: returning after
		// the source had already left the live map left a handle pointing at
		// a store nothing else could reach.
		return WIPICErrorInvalid, nil
	}
	store, live := runtime.CFiles[oldName]
	if !live {
		seed, exists := runtime.DatabaseSeed(oldName)
		if !exists {
			// The specification's failure list for this call has no "no such
			// file", so the source that is not there is the catch-all
			// M_E_ERROR rather than the M_E_NOENT the questions about a name
			// answer with.
			runtime.CountDiagnostic(fmt.Sprintf("fs rename %s -> missing", oldName))
			return WIPICErrorGeneric, nil
		}
		store = &RuntimeCFile{Name: oldName, Data: append([]byte(nil), seed...)}
		if packaged, ok := runtime.PackagedDatabase(oldName); ok {
			store.Packaged = len(packaged)
		}
	}
	if err := runtime.SaveChanges(map[string][]byte{"db/" + newName: store.Data, "db/" + oldName: nil}, DatabaseRemovedKey, runtime.RemovedDatabases(), map[string]bool{oldName: true, newName: false}); err != nil {
		return 0, err
	}
	delete(runtime.CFiles, oldName)
	store.Name = newName
	if runtime.CFiles == nil {
		runtime.CFiles = make(map[string]*RuntimeCFile)
	}
	runtime.CFiles[newName] = store
	// A store a quick load left without a file under the new name is that
	// name's store no longer: the handles that kept it follow the file that
	// has the name now, so the name still has one host store.
	if detached := runtime.DetachedCFiles[newName]; detached != nil {
		for _, open := range runtime.CFileHandles {
			if open.Store == detached {
				open.Store = store
			}
		}
		delete(runtime.DetachedCFiles, newName)
	}

	runtime.CountDiagnostic(fmt.Sprintf("fs rename %s -> %s", oldName, newName))
	return 0, nil
}

// DatabaseRemovedKey holds the names MC_fsRemove has removed. See
// guestFileRemovedKey for why a delete needs a list of its own.
const DatabaseRemovedKey = "db/.removed"

// RemovedDatabases is the set of deleted names, read from the store once per
// session and kept in memory after that.
func (runtime *Runtime) RemovedDatabases() map[string]bool {
	if runtime.RemovedCDatabases != nil {
		return runtime.RemovedCDatabases
	}
	runtime.RemovedCDatabases = make(map[string]bool)
	if data, exists := runtime.LoadSave(DatabaseRemovedKey); exists {
		for _, line := range SplitRemovalList(data) {
			runtime.RemovedCDatabases[line] = true
		}
	}
	return runtime.RemovedCDatabases
}

// MarkDatabaseRemoved records or clears one name and writes the list back.
// Opening a database for creation clears it, because a store that was written
// and then still read as deleted is worse than one that was never deleted.
func (runtime *Runtime) MarkDatabaseRemoved(name string, removed bool) error {
	return runtime.SaveChanges(nil, DatabaseRemovedKey, runtime.RemovedDatabases(), map[string]bool{name: removed})
}

// wipicMakeDirectory serves MC_fsMkDir(dirName, aMode).
//
// A directory has no representation here — the store this table reads and
// writes is one flat map from a name to its bytes — so making one has nothing
// to create. What it does have is an answer to give, and the specification
// gives it two: zero for a directory that was made, `M_E_EXIST` for one that
// was already there. Answering zero every time would tell a title that checks
// the result that every run is its first, so the names that have been made are
// written down beside the names that have been deleted, in the same store and
// for the same reason: a question about storage has to be answered the same way
// after a restart as before one.
//
// The two local titles that reach this call make their own directory at
// start-up and drop the answer, so neither is what the list is for; a title
// that reads it is.
func (runtime *Runtime) wipicMakeDirectory(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readFileName(nameAddress)
	if err != nil {
		return 0, fmt.Errorf("read %s directory name: %w", runtime.platformName(), err)
	}
	if NameTooLong(name) {
		return WIPICErrorLongName, nil
	}
	if ListingPath(name) == "" {
		// The program's own directory is there already.
		return WIPICErrorExists, nil
	}
	if !StorableName(CFileScope, name) {
		// The list of made names is read back a line at a time.
		return WIPICErrorBadFileName, nil
	}
	// A name that is a file, or a directory by what lies below it, is taken
	// as much as one this call made.
	exists, err := runtime.cFileExists(name)
	if err != nil {
		runtime.CountDiagnostic(fmt.Sprintf("fs mkdir error: %v", err))
		return WIPICErrorGeneric, nil
	}
	if exists || runtime.CreatedDirectories()[name] {
		runtime.CountDiagnostic(fmt.Sprintf("fs mkdir %s -> exists", name))
		return WIPICErrorExists, nil
	}
	if err := runtime.markDirectoryCreated(name); err != nil {
		return 0, err
	}
	runtime.CountDiagnostic(fmt.Sprintf("fs mkdir %s", name))
	return 0, nil
}

// DirectoryListKey holds the directory names MC_fsMkDir has made. See
// databaseRemovedKey for why a flat store needs a list of its own.
const DirectoryListKey = "db/.dirs"

// CreatedDirectories is the set of made names, read from the store once per
// session and kept in memory after that.
func (runtime *Runtime) CreatedDirectories() map[string]bool {
	if runtime.MadeDirectories != nil {
		return runtime.MadeDirectories
	}
	runtime.MadeDirectories = make(map[string]bool)
	if data, exists := runtime.LoadSave(DirectoryListKey); exists {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				runtime.MadeDirectories[line] = true
			}
		}
	}
	return runtime.MadeDirectories
}

// markDirectoryCreated records one name and writes the list back.
func (runtime *Runtime) markDirectoryCreated(name string) error {
	return runtime.SaveChanges(nil, DirectoryListKey, runtime.CreatedDirectories(), map[string]bool{name: true})
}

// bindDetachedCFile puts a store a quick load left without a file back under
// its name once a write through it has been stored: the file exists again, and
// this store is the one that holds it. For any other store it does nothing.
func (runtime *Runtime) bindDetachedCFile(store *RuntimeCFile) {
	if runtime.DetachedCFiles[store.Name] != store {
		return
	}
	delete(runtime.DetachedCFiles, store.Name)
	if runtime.CFiles == nil {
		runtime.CFiles = make(map[string]*RuntimeCFile)
	}
	if runtime.CFiles[store.Name] == nil {
		runtime.CFiles[store.Name] = store
	}
}

func (runtime *Runtime) DatabaseSeed(name string) ([]byte, bool) {
	if runtime.RemovedDatabases()[name] {
		return nil, false
	}
	if saved, exists := runtime.LoadSave("db/" + name); exists {
		// A persisted save takes precedence over packaged initial data.
		return saved, true
	}
	return runtime.PackagedDatabase(name)
}

// PackagedDatabase is the archive's own initial content for a database, which
// archives ship as a data file with the database's name. It ignores the
// removal list: create mode keeps packaged content, and that has to stay true
// of a name the game deleted and reopened.
func (runtime *Runtime) PackagedDatabase(name string) ([]byte, bool) {
	return runtime.GuestFile(name)
}

func (runtime *Runtime) WIPICFileOpen(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	mode, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readFileName(nameAddress)
	if err != nil {
		return 0, fmt.Errorf("read %s database name: %w", runtime.platformName(), err)
	}
	runtime.CountDiagnostic(fmt.Sprintf("cdb open %s mode %d", name, int32(mode)))
	if NameTooLong(name) {
		return WIPICErrorLongName, nil
	}
	if ReservedStorageName(CFileScope, name) || !CFileNameKeyed(name) {
		return WIPICErrorBadFileName, nil
	}
	store, exists := runtime.CFiles[name]
	seed, hasSeed := runtime.DatabaseSeed(name)
	_, hasPackaged := runtime.PackagedDatabase(name)
	if !exists {
		if int32(mode) == 1 && !hasSeed {
			return WIPICErrorNotFound, nil
		}
		store = &RuntimeCFile{Name: name}
		store.Data = append([]byte(nil), seed...)
		if packaged, ok := runtime.PackagedDatabase(name); ok {
			store.Packaged = len(packaged)
		}
		if runtime.CFiles == nil {
			runtime.CFiles = make(map[string]*RuntimeCFile)
		}
	}
	// Create mode wipes prior contents unless the database is backed by
	// packaged archive data.
	staged := *store
	if int32(mode) == 4 && !hasPackaged {
		staged.Data = nil
	}
	if !runtime.cFileHandleFree() {
		return WIPICErrorInvalid, nil
	}
	if (!exists && !hasSeed) || (int32(mode) == 4 && !hasPackaged) || runtime.RemovedDatabases()[name] {
		if err := staged.Persist(runtime); err != nil {
			return 0, err
		}
	}
	store.Data = staged.Data
	// A store a quick load left without a file is the store of this name, so
	// the open takes it rather than keeping a second one: the handles that
	// kept it and this one share what is written from here.
	if detached := runtime.DetachedCFiles[name]; !exists && detached != nil {
		detached.Data, detached.Packaged = store.Data, store.Packaged
		store = detached
		delete(runtime.DetachedCFiles, name)
	}
	runtime.CFiles[name] = store
	// Opening a deleted name brings it back: the store is live again from
	// here, and a removal list that still held it would hide the writes the
	// caller is about to make.

	if runtime.CFileHandles == nil {
		runtime.CFileHandles = make(map[uint32]*RuntimeCFileHandle)
	}
	handle, ok := NextTableHandle(&runtime.NextCDatabaseHandle, MaxCFileHandles, CFileHandleBit, func(handle uint32) bool {
		_, open := runtime.CFileHandles[handle]
		return open
	})
	if !ok {
		return WIPICErrorInvalid, nil
	}
	runtime.CFileHandles[handle] = &RuntimeCFileHandle{Store: store}
	return handle, nil
}

// cFileHandleFree reports whether an open could be given a handle, which is
// asked before the open stores anything.
func (runtime *Runtime) cFileHandleFree() bool {
	return runtime.NextCDatabaseHandle < MaxCFileHandles || len(runtime.CFileHandles) < MaxCFileHandles
}

// NextTableHandle issues the next handle of a table whose handles are a tag
// and a number. Numbers come from the counter while it lasts, which is every
// handle a session has ever been given until now; once it reaches the limit a
// closed number is reused, lowest first, the way a handset hands a closed
// descriptor out again. The counter used to be the whole rule, so a session's
// open after its 4095th failed however many had been closed — the open a
// title makes to write its save among them.
func NextTableHandle(counter *uint32, limit, tag uint32, open func(uint32) bool) (uint32, bool) {
	if *counter < limit {
		*counter++
		return tag | *counter, true
	}
	for number := uint32(1); number <= limit; number++ {
		if !open(tag | number) {
			return tag | number, true
		}
	}
	return 0, false
}

// CFileNameKeyed reports whether a name is a save key of its own under this
// table's scope. readFileName drops only leading separators, so "/" arrives
// as nothing and "." and "./" survive it; NormalizeSaveKey then collapses all
// three onto the scope itself, and the store wrote a regular file named "db"
// where the directory belongs, after which every save of the title failed. A
// name with ".." fails its key, and that failure on a read stays for the rest
// of the session. Line breaks are left alone here, unlike storableName: they
// cannot corrupt a key, a save may already be stored under such a name, and
// only the slots that write a name into a list refuse them.
func CFileNameKeyed(name string) bool {
	key, err := backend.NormalizeSaveKey(CFileScope + "/" + name)
	if err != nil {
		return false
	}
	rest, under := strings.CutPrefix(key, CFileScope+"/")
	return under && rest != ""
}

func (runtime *Runtime) wipicFileHandle(thread *armcore.Thread) (*RuntimeCFileHandle, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return nil, err
	}
	state, ok := runtime.CFileHandles[handle]
	if !ok {
		return nil, fmt.Errorf("%s database handle %#x is not open", runtime.platformName(), handle)
	}
	return state, nil
}

func (runtime *Runtime) WIPICFileStream(thread *armcore.Thread, write bool) (uint32, error) {
	state, err := runtime.wipicFileHandle(thread)
	if err != nil {
		return WIPICErrorInvalid, nil
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	length, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	if int32(length) < 0 {
		return WIPICErrorInvalid, nil
	}
	if length > MaxCFileBytes {
		if write {
			return WIPICErrorNoSpace, nil
		}
		return WIPICErrorInvalid, nil
	}
	if write {
		data := make([]byte, length)
		if err := runtime.Client.core.Memory().Read(buffer, data); err != nil {
			return 0, fmt.Errorf("read %s database write buffer: %w", runtime.platformName(), err)
		}
		end := state.Position + int(length)
		if end > MaxCFileBytes {
			// Past the bound one file may hold, which a write fails as a
			// file system out of room does.
			return WIPICErrorNoSpace, nil
		}
		staged := make([]byte, max(end, len(state.Store.Data)))
		copy(staged, state.Store.Data)
		copy(staged[state.Position:], data)
		if err := runtime.SaveChanges(map[string][]byte{"db/" + state.Store.Name: staged}, DatabaseRemovedKey, runtime.RemovedDatabases(), map[string]bool{state.Store.Name: false}); err != nil {
			return 0, err
		}
		state.Store.Data, state.Position = staged, end
		runtime.bindDetachedCFile(state.Store)
		return length, nil
	}
	remaining := len(state.Store.Data) - state.Position
	if remaining <= 0 {
		return 0, nil
	}
	count := int(length)
	if count > remaining {
		count = remaining
	}
	if err := runtime.Client.core.Memory().Write(buffer, state.Store.Data[state.Position:state.Position+count]); err != nil {
		return 0, fmt.Errorf("write %s database read buffer: %w", runtime.platformName(), err)
	}
	state.Position += count
	return uint32(count), nil
}

// wipicFileStatByName serves MC_fsFileAttribute(name, MH_FileInfo* out, mode),
// the file attributes at slot 5: `{ attrib, creationTime, size }`, twelve
// bytes with the length at offset 8. Titles treat a zero result with out[2]
// above a size threshold as a valid save.
//
// **A directory is a name it has**, as it is to MC_fsIsExist: the root, a name
// `MC_fsMkDir` wrote down, or one a file lies below. One title makes its
// shared directory, stats the name it just made, and calls `MC_knlExit` inside
// `startApp` if it is told there is no such thing. A directory answers with
// `MC_FILE_IS_DIR` in the attribute word: the value is `MH_FILE_IS_DIR`, which
// the specification's HAL page gives as `0x01`. The word was zero while that
// value was unknown here; the one title that reads it tests bits 3 and 1 and
// files the entry the same way whatever it finds, so the bit changes nothing
// it does. A file's word stays zero, since the store keeps no attributes, and
// no creation time is kept for either.
//
// A name nothing has answers `M_E_NOENT`, as every platform's does
// (docs/wipi-storage.md); it used to be `M_E_BADRECID`, which was what a
// missing database answered here.
func (runtime *Runtime) wipicFileStatByName(thread *armcore.Thread) (uint32, error) {
	const isDirectory = 0x01 // MC_FILE_IS_DIR
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	outAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readFileName(nameAddress)
	if err != nil {
		return 0, fmt.Errorf("read %s database name: %w", runtime.platformName(), err)
	}
	if NameTooLong(name) {
		return WIPICErrorLongName, nil
	}
	if ListingPath(name) != "" && !CFileNameKeyed(name) {
		runtime.CountDiagnostic(fmt.Sprintf("cdb stat %s -> bad name", name))
		return WIPICErrorBadFileName, nil
	}
	if store, ok := runtime.CFiles[name]; ok {
		runtime.CountDiagnostic(fmt.Sprintf("cdb stat %s -> %d", name, len(store.Data)))
		return runtime.writeFileStat(outAddress, uint32(len(store.Data)), 0)
	}
	if seed, ok := runtime.DatabaseSeed(name); ok {
		// The save, then the packaged copy, as before — through the removal
		// list, which MC_fsIsExist and an open consult as well. A removed
		// name leaves an empty entry under its key, because the store cannot
		// delete, and read directly that was a file of zero bytes.
		runtime.CountDiagnostic(fmt.Sprintf("cdb stat %s -> %d", name, len(seed)))
		return runtime.writeFileStat(outAddress, uint32(len(seed)), 0)
	}
	directory, err := runtime.cFileIsDirectory(name)
	if err != nil {
		runtime.CountDiagnostic(fmt.Sprintf("cdb stat error: %v", err))
		return WIPICErrorGeneric, nil
	}
	if directory {
		runtime.CountDiagnostic(fmt.Sprintf("cdb stat %s -> directory", name))
		return runtime.writeFileStat(outAddress, 0, isDirectory)
	}
	runtime.CountDiagnostic(fmt.Sprintf("cdb stat %s -> missing", name))
	return WIPICErrorNotFound, nil
}

// writeFileStat fills the MH_FileInfo the stat was handed and answers success.
// A caller that passed no record still gets the answer.
func (runtime *Runtime) writeFileStat(outAddress, size, attributes uint32) (uint32, error) {
	if outAddress == 0 {
		return 0, nil
	}
	record := make([]byte, 12)
	binary.LittleEndian.PutUint32(record[0:], attributes)
	binary.LittleEndian.PutUint32(record[8:], size)
	if err := runtime.Client.core.Memory().Write(outAddress, record); err != nil {
		return 0, fmt.Errorf("write %s database stat: %w", runtime.platformName(), err)
	}
	return 0, nil
}

// wipicFileTell serves MC_fsTell(fd): the handle's cursor. Slot 15 is where
// the specification's order puts it, and the one local title that calls it
// does so from its save loader in exactly the shape a program without a size
// call uses to ask for one:
//
//	handle = open(name, read, 1)
//	size   = seek(handle, 0, SEEK_END)
//	if size > 0 { slot15(handle); seek(handle, 0, SEEK_SET) }
//
// The call takes the handle alone — the disassembly reaches it through the
// `bx r2` trampoline with only r0 set. That title drops the answer, which is
// why answering zero had passed; a title that keeps it is asking where the
// cursor is. An unknown handle answers M_E_BADFD, the specification's code for
// it.
func (runtime *Runtime) wipicFileTell(thread *armcore.Thread) (uint32, error) {
	state, err := runtime.wipicFileHandle(thread)
	if err != nil {
		return WIPICErrorBadFD, nil
	}
	return uint32(state.Position), nil
}

// wipicFileSeek implements the KTF select-record extension, a seek over
// the open record's byte cursor.
func (runtime *Runtime) wipicFileSeek(thread *armcore.Thread) (uint32, error) {
	state, err := runtime.wipicFileHandle(thread)
	if err != nil {
		return WIPICErrorInvalid, nil
	}
	offset, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	origin, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	base := 0
	switch origin {
	case 0:
		base = 0
	case 1:
		base = state.Position
	case 2:
		base = len(state.Store.Data)
	default:
		return WIPICErrorInvalid, nil
	}
	position := base + int(int32(offset))
	if position < 0 {
		position = 0
	}
	if position > len(state.Store.Data) {
		position = len(state.Store.Data)
	}
	state.Position = position
	return uint32(position), nil
}
