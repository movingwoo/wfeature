package clet

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
type runtimeCFile struct {
	name string
	data []byte
	// packaged is how many bytes of this store the archive shipped. The
	// archive's own data is not storage the player consumed — on a handset it
	// arrives with the program — so it does not count against the save budget
	// MC_fsAvailable reports. Without this a title that packages its maps
	// and its text as databases reads its own content as a full disk: one
	// here ships 2 MB that way and could not start a new game, because the
	// budget was gone before the player had saved anything.
	packaged int
}

// persist writes the record through the Host save store when one is attached.
func (store *runtimeCFile) persist(runtime *initializationRuntime) error {
	return runtime.saveChanges(map[string][]byte{"db/" + store.name: store.data}, databaseRemovedKey, runtime.removedDatabases(), map[string]bool{store.name: false})
}

type runtimeCFileHandle struct {
	store    *runtimeCFile
	position int
}

const (
	maxCFileBytes = 4 << 20
	// cFileHandleBit tags a file handle. It has to stay inside
	// a signed 16-bit value: a handset's handle is small, and a title that
	// keeps one in a `short` sign-extends what the open returned before it
	// hands it back. One local title does exactly that — `lsl #16; asr #16` on
	// the result, then a "negative means it failed" test — so a handle with a
	// high bit set came back as a number this platform had never issued. The
	// reads on it were refused, silently, leaving the header buffer untouched;
	// the title then sized an allocation from the difference of two words that
	// were never written and asked for four gigabytes.
	cFileHandleBit  = 0x1000
	maxCFileHandles = 0x0fff
)

const (
	wipicFileOpen         = 0
	wipicFileStreamRead   = 1
	wipicFileStreamWrite  = 2
	wipicFileClose        = 3
	wipicFileSelectRecord = 4
	wipicFileStatByName   = 5
	// MC_fsRemove takes the file's name and its area, not a handle: the caller
	// closes the file first and then names it. One title deletes the
	// certificate it could not renew and expects the next question about it to
	// answer "no such file".
	wipicFileDelete = 6
	// MC_fsRename takes two names and an access area, and is how a title that
	// writes a save to a scratch name commits it: close the scratch file, then
	// rename it over the real one. One local title does exactly that, and
	// while the slot was missing its save was written and then never arrived.
	wipicFileRename = 7
	// MC_fsMkDir takes a name and an access area — 1 is the program's own
	// directory, which is the only area this platform has. **Nothing here
	// holds a directory**: a name is a key in one flat store, so making one is
	// the whole of what a directory is, and a name that has been made is what
	// says it exists.
	wipicFileMakeDirectory   = 8
	wipicFileRemoveDirectory = 9
	// Slots 10 and 11 were the record count and the record size of an
	// earlier reading of this table, which took the first register for a
	// handle. They are `MC_fsList` and `MC_fsTotalSpace`: the specification's
	// order says so, and a title that asks 11 for the total space with no
	// argument at all was told M_E_INVALID, reported that the handset had no
	// room to save, and never left its title screen.
	wipicFileList       = 10
	wipicFileTotalSpace = 11
	wipicFileAvailable  = 12
	wipicFileSetMode    = 13
	wipicFileGetCounts  = 14
	wipicFileTell       = 15
	wipicFileExists     = 16

	// wipicFileStorageLimit mirrors the reference KTF per-game storage budget;
	// MC_fsAvailable reports the space still available.
	wipicFileStorageLimit = 1024 * 1024
)

func (runtime *initializationRuntime) handleWIPICFileCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case wipicFileOpen:
		return runtime.wipicFileOpen(thread)
	case wipicFileStreamRead:
		return runtime.wipicFileStream(thread, false)
	case wipicFileStreamWrite:
		return runtime.wipicFileStream(thread, true)
	case wipicFileClose:
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		delete(runtime.cFileHandles, handle)
		return 0, nil
	case wipicFileSelectRecord:
		return runtime.wipicFileSeek(thread)
	case wipicFileDelete:
		return runtime.wipicFileDelete(thread)
	case wipicFileRename:
		return runtime.wipicFileRename(thread)
	case wipicFileStatByName:
		return runtime.wipicFileStatByName(thread)
	case wipicFileRemoveDirectory:
		return runtime.wipicRemoveDirectory(thread)
	case wipicFileList:
		return runtime.wipicFileList(thread)
	case wipicFileTotalSpace:
		// MC_fsTotalSpace takes no argument and answers the size of the whole
		// file system, which here is the per-game budget MC_fsAvailable
		// counts down from, so the free space is never more than the total.
		return wipicFileStorageLimit, nil
	case wipicFileAvailable:
		// MC_fsAvailable reports remaining storage: the budget less what the
		// title has stored, as every platform counts it (storageAvailable).
		return runtime.storageAvailable(), nil
	case wipicFileExists:
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
			return 0, fmt.Errorf("read KTF file name: %w", err)
		}
		if nameTooLong(name) {
			return wipicErrorLongName, nil
		}
		if listingPath(name) == "" {
			// The program's own directory is always there.
			return 0, nil
		}
		if !cFileNameKeyed(name) {
			// Nothing can be stored under it: a name the store cannot keep
			// is M_E_BADFILENAME, as MC_fsFileAttribute answers it.
			return wipicErrorBadFileName, nil
		}
		exists, err := runtime.cFileExists(name)
		if err != nil {
			runtime.countDiagnostic(fmt.Sprintf("cdb exists error: %v", err))
			return wipicErrorGeneric, nil
		}
		runtime.countDiagnostic(fmt.Sprintf("cdb exists %s -> %t", name, exists))
		if exists {
			return 0, nil
		}
		return wipicErrorNotFound, nil
	case wipicFileMakeDirectory:
		return runtime.wipicMakeDirectory(thread)
	case wipicFileSetMode:
		return runtime.wipicFileSetMode(thread)
	case wipicFileGetCounts:
		return runtime.wipicFileCount(thread)
	case wipicFileTell:
		return runtime.wipicFileTell(thread)
	default:
		// The call site is named for the same reason the unimplemented-table
		// error names one: the function number is an index into an array of
		// pointers, so it never appears in the guest's own code, and an
		// address is the only thing about a missing call that can be
		// disassembled.
		return 0, fmt.Errorf("KTF WIPI C filesystem function %d is not implemented%s",
			function, runtime.callerSite(thread))
	}
}

// wipicFileDelete serves MC_fsRemove(name, mode). A store may be
// in memory, persisted, packaged with the archive, or all three, so the delete
// has to cover every place the open would look. The persisted copy cannot be
// unlinked — the save store has no delete — so the name is recorded on a
// removal list the way the guest filesystem records one, and every lookup
// consults it. Without that the database comes back on the next question, and
// a title that deleted it to start again is handed what it just threw away.
func (runtime *initializationRuntime) wipicFileDelete(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readFileName(nameAddress)
	if err != nil {
		return 0, fmt.Errorf("read KTF database name: %w", err)
	}
	if nameTooLong(name) {
		return wipicErrorLongName, nil
	}
	if reservedStorageName(cFileScope, name) {
		// Removing writes nil over the key, and these keys are this table's
		// own lists: the deletion list answers as a seed as soon as anything
		// has been deleted, so the removal would go through and wipe it, and
		// every database the title had deleted would be back on the next open.
		return wipicErrorBadFileName, nil
	}
	if !storableName(cFileScope, name) {
		// The removal list could not carry it: "a\nb" would come back as the
		// two names a and b, and hide both.
		return wipicErrorBadFileName, nil
	}
	_, live := runtime.cFiles[name]
	_, seeded := runtime.databaseSeed(name)
	runtime.countDiagnostic(fmt.Sprintf("cdb delete %s -> %t", name, live || seeded))
	if !live && !seeded {
		return wipicErrorNotFound, nil
	}
	if err := runtime.saveChanges(map[string][]byte{"db/" + name: nil}, databaseRemovedKey, runtime.removedDatabases(), map[string]bool{name: true}); err != nil {
		return 0, err
	}
	delete(runtime.cFiles, name)
	// Handles onto the store go with it; a caller holding one after the delete
	// would otherwise keep reading a database nothing else can see.
	for handle, open := range runtime.cFileHandles {
		if open.store != nil && open.store.name == name {
			delete(runtime.cFileHandles, handle)
		}
	}

	return 0, nil
}

// wipicFileKey is the name a file is stored under here, which is the name the
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
func wipicFileKey(name string) string {
	return strings.TrimLeft(strings.ReplaceAll(name, "\\", "/"), "/")
}

// readFileName reads a name argument of this table and answers the key it
// stands for. Every slot that takes a name goes through it, because two slots
// that disagree about which file a name means are worse than either rule.
func (runtime *initializationRuntime) readFileName(address uint32) (string, error) {
	// Read further than any name may be, so a long one is the
	// specification's M_E_LONGNAME rather than a string that never ended:
	// the same bound every platform reads a name to.
	name, err := runtime.readCString(address, maxFileNameRead)
	if err != nil {
		return "", err
	}
	return wipicFileKey(name), nil
}

// wipicFileRename serves MC_fsRename(oldName, newName, aMode): "파일이름을
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
func (runtime *initializationRuntime) wipicFileRename(thread *armcore.Thread) (uint32, error) {
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
		return 0, fmt.Errorf("read KTF file name: %w", err)
	}
	newName, err := runtime.readFileName(newAddress)
	if err != nil {
		return 0, fmt.Errorf("read KTF file name: %w", err)
	}
	if nameTooLong(oldName) || nameTooLong(newName) {
		return wipicErrorLongName, nil
	}
	if oldName == "" || newName == "" {
		return wipicErrorGeneric, nil
	}
	if !storableName(cFileScope, oldName) || !storableName(cFileScope, newName) {
		// A name that is not a key of its own, or that the removal list could
		// not carry. Refused before either name is looked up: a name that
		// fails its key fails the read, and that read failure stays.
		return wipicErrorBadFileName, nil
	}
	if oldName == newName {
		// The same file under the same name: there is nothing to move, and
		// answering M_E_EXIST because the destination is occupied by the
		// source would refuse a rename that has already happened.
		runtime.countDiagnostic(fmt.Sprintf("fs rename %s -> itself", oldName))
		return 0, nil
	}
	_, taken := runtime.cFiles[newName]
	if _, seeded := runtime.databaseSeed(newName); seeded {
		taken = true
	}
	if runtime.createdDirectories()[newName] {
		taken = true
	}
	if taken {
		runtime.countDiagnostic(fmt.Sprintf("fs rename %s -> %s exists", oldName, newName))
		return wipicErrorExists, nil
	}
	if reservedStorageName(cFileScope, oldName) || reservedStorageName(cFileScope, newName) {
		// Either end. Renaming *onto* a list this table keeps would replace it
		// with the file's bytes, and the next session would read those bytes
		// as the list; renaming one *away* is worse, because the source is
		// emptied afterwards — the list answers as a seed as soon as anything
		// has been deleted, so the rename would carry it off under another
		// name and leave nothing behind, bringing back every database the
		// title had deleted. Refused before anything moves: returning after
		// the source had already left the live map left a handle pointing at
		// a store nothing else could reach.
		return wipicErrorInvalid, nil
	}
	store, live := runtime.cFiles[oldName]
	if !live {
		seed, exists := runtime.databaseSeed(oldName)
		if !exists {
			// The specification's failure list for this call has no "no such
			// file", so the source that is not there is the catch-all
			// M_E_ERROR rather than the M_E_NOENT the questions about a name
			// answer with.
			runtime.countDiagnostic(fmt.Sprintf("fs rename %s -> missing", oldName))
			return wipicErrorGeneric, nil
		}
		store = &runtimeCFile{name: oldName, data: append([]byte(nil), seed...)}
		if packaged, ok := runtime.packagedDatabase(oldName); ok {
			store.packaged = len(packaged)
		}
	}
	if err := runtime.saveChanges(map[string][]byte{"db/" + newName: store.data, "db/" + oldName: nil}, databaseRemovedKey, runtime.removedDatabases(), map[string]bool{oldName: true, newName: false}); err != nil {
		return 0, err
	}
	delete(runtime.cFiles, oldName)
	store.name = newName
	if runtime.cFiles == nil {
		runtime.cFiles = make(map[string]*runtimeCFile)
	}
	runtime.cFiles[newName] = store
	// A store a quick load left without a file under the new name is that
	// name's store no longer: the handles that kept it follow the file that
	// has the name now, so the name still has one host store.
	if detached := runtime.detachedCFiles[newName]; detached != nil {
		for _, open := range runtime.cFileHandles {
			if open.store == detached {
				open.store = store
			}
		}
		delete(runtime.detachedCFiles, newName)
	}

	runtime.countDiagnostic(fmt.Sprintf("fs rename %s -> %s", oldName, newName))
	return 0, nil
}

// databaseRemovedKey holds the names MC_fsRemove has removed. See
// guestFileRemovedKey for why a delete needs a list of its own.
const databaseRemovedKey = "db/.removed"

// removedDatabases is the set of deleted names, read from the store once per
// session and kept in memory after that.
func (runtime *initializationRuntime) removedDatabases() map[string]bool {
	if runtime.removedCDatabases != nil {
		return runtime.removedCDatabases
	}
	runtime.removedCDatabases = make(map[string]bool)
	if data, exists := runtime.loadSave(databaseRemovedKey); exists {
		for _, line := range splitRemovalList(data) {
			runtime.removedCDatabases[line] = true
		}
	}
	return runtime.removedCDatabases
}

// markDatabaseRemoved records or clears one name and writes the list back.
// Opening a database for creation clears it, because a store that was written
// and then still read as deleted is worse than one that was never deleted.
func (runtime *initializationRuntime) markDatabaseRemoved(name string, removed bool) error {
	return runtime.saveChanges(nil, databaseRemovedKey, runtime.removedDatabases(), map[string]bool{name: removed})
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
func (runtime *initializationRuntime) wipicMakeDirectory(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readFileName(nameAddress)
	if err != nil {
		return 0, fmt.Errorf("read KTF directory name: %w", err)
	}
	if nameTooLong(name) {
		return wipicErrorLongName, nil
	}
	if listingPath(name) == "" {
		// The program's own directory is there already.
		return wipicErrorExists, nil
	}
	if !storableName(cFileScope, name) {
		// The list of made names is read back a line at a time.
		return wipicErrorBadFileName, nil
	}
	// A name that is a file, or a directory by what lies below it, is taken
	// as much as one this call made.
	exists, err := runtime.cFileExists(name)
	if err != nil {
		runtime.countDiagnostic(fmt.Sprintf("fs mkdir error: %v", err))
		return wipicErrorGeneric, nil
	}
	if exists || runtime.createdDirectories()[name] {
		runtime.countDiagnostic(fmt.Sprintf("fs mkdir %s -> exists", name))
		return wipicErrorExists, nil
	}
	if err := runtime.markDirectoryCreated(name); err != nil {
		return 0, err
	}
	runtime.countDiagnostic(fmt.Sprintf("fs mkdir %s", name))
	return 0, nil
}

// directoryListKey holds the directory names MC_fsMkDir has made. See
// databaseRemovedKey for why a flat store needs a list of its own.
const directoryListKey = "db/.dirs"

// createdDirectories is the set of made names, read from the store once per
// session and kept in memory after that.
func (runtime *initializationRuntime) createdDirectories() map[string]bool {
	if runtime.madeDirectories != nil {
		return runtime.madeDirectories
	}
	runtime.madeDirectories = make(map[string]bool)
	if data, exists := runtime.loadSave(directoryListKey); exists {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				runtime.madeDirectories[line] = true
			}
		}
	}
	return runtime.madeDirectories
}

// markDirectoryCreated records one name and writes the list back.
func (runtime *initializationRuntime) markDirectoryCreated(name string) error {
	return runtime.saveChanges(nil, directoryListKey, runtime.createdDirectories(), map[string]bool{name: true})
}

// bindDetachedCFile puts a store a quick load left without a file back under
// its name once a write through it has been stored: the file exists again, and
// this store is the one that holds it. For any other store it does nothing.
func (runtime *initializationRuntime) bindDetachedCFile(store *runtimeCFile) {
	if runtime.detachedCFiles[store.name] != store {
		return
	}
	delete(runtime.detachedCFiles, store.name)
	if runtime.cFiles == nil {
		runtime.cFiles = make(map[string]*runtimeCFile)
	}
	if runtime.cFiles[store.name] == nil {
		runtime.cFiles[store.name] = store
	}
}

func (runtime *initializationRuntime) databaseSeed(name string) ([]byte, bool) {
	if runtime.removedDatabases()[name] {
		return nil, false
	}
	if saved, exists := runtime.loadSave("db/" + name); exists {
		// A persisted save takes precedence over packaged initial data.
		return saved, true
	}
	return runtime.packagedDatabase(name)
}

// packagedDatabase is the archive's own initial content for a database, which
// archives ship as a data file with the database's name. It ignores the
// removal list: create mode keeps packaged content, and that has to stay true
// of a name the game deleted and reopened.
func (runtime *initializationRuntime) packagedDatabase(name string) ([]byte, bool) {
	return runtime.guestFile(name)
}

func (runtime *initializationRuntime) wipicFileOpen(thread *armcore.Thread) (uint32, error) {
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
		return 0, fmt.Errorf("read KTF database name: %w", err)
	}
	runtime.countDiagnostic(fmt.Sprintf("cdb open %s mode %d", name, int32(mode)))
	if nameTooLong(name) {
		return wipicErrorLongName, nil
	}
	if reservedStorageName(cFileScope, name) || !cFileNameKeyed(name) {
		return wipicErrorBadFileName, nil
	}
	store, exists := runtime.cFiles[name]
	seed, hasSeed := runtime.databaseSeed(name)
	_, hasPackaged := runtime.packagedDatabase(name)
	if !exists {
		if int32(mode) == 1 && !hasSeed {
			return wipicErrorNotFound, nil
		}
		store = &runtimeCFile{name: name}
		store.data = append([]byte(nil), seed...)
		if packaged, ok := runtime.packagedDatabase(name); ok {
			store.packaged = len(packaged)
		}
		if runtime.cFiles == nil {
			runtime.cFiles = make(map[string]*runtimeCFile)
		}
	}
	// Create mode wipes prior contents unless the database is backed by
	// packaged archive data.
	staged := *store
	if int32(mode) == 4 && !hasPackaged {
		staged.data = nil
	}
	if !runtime.cFileHandleFree() {
		return wipicErrorInvalid, nil
	}
	if (!exists && !hasSeed) || (int32(mode) == 4 && !hasPackaged) || runtime.removedDatabases()[name] {
		if err := staged.persist(runtime); err != nil {
			return 0, err
		}
	}
	store.data = staged.data
	// A store a quick load left without a file is the store of this name, so
	// the open takes it rather than keeping a second one: the handles that
	// kept it and this one share what is written from here.
	if detached := runtime.detachedCFiles[name]; !exists && detached != nil {
		detached.data, detached.packaged = store.data, store.packaged
		store = detached
		delete(runtime.detachedCFiles, name)
	}
	runtime.cFiles[name] = store
	// Opening a deleted name brings it back: the store is live again from
	// here, and a removal list that still held it would hide the writes the
	// caller is about to make.

	if runtime.cFileHandles == nil {
		runtime.cFileHandles = make(map[uint32]*runtimeCFileHandle)
	}
	handle, ok := nextTableHandle(&runtime.nextCDatabaseHandle, maxCFileHandles, cFileHandleBit, func(handle uint32) bool {
		_, open := runtime.cFileHandles[handle]
		return open
	})
	if !ok {
		return wipicErrorInvalid, nil
	}
	runtime.cFileHandles[handle] = &runtimeCFileHandle{store: store}
	return handle, nil
}

// cFileHandleFree reports whether an open could be given a handle, which is
// asked before the open stores anything.
func (runtime *initializationRuntime) cFileHandleFree() bool {
	return runtime.nextCDatabaseHandle < maxCFileHandles || len(runtime.cFileHandles) < maxCFileHandles
}

// nextTableHandle issues the next handle of a table whose handles are a tag
// and a number. Numbers come from the counter while it lasts, which is every
// handle a session has ever been given until now; once it reaches the limit a
// closed number is reused, lowest first, the way a handset hands a closed
// descriptor out again. The counter used to be the whole rule, so a session's
// open after its 4095th failed however many had been closed — the open a
// title makes to write its save among them.
func nextTableHandle(counter *uint32, limit, tag uint32, open func(uint32) bool) (uint32, bool) {
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

// cFileNameKeyed reports whether a name is a save key of its own under this
// table's scope. readFileName drops only leading separators, so "/" arrives
// as nothing and "." and "./" survive it; NormalizeSaveKey then collapses all
// three onto the scope itself, and the store wrote a regular file named "db"
// where the directory belongs, after which every save of the title failed. A
// name with ".." fails its key, and that failure on a read stays for the rest
// of the session. Line breaks are left alone here, unlike storableName: they
// cannot corrupt a key, a save may already be stored under such a name, and
// only the slots that write a name into a list refuse them.
func cFileNameKeyed(name string) bool {
	key, err := backend.NormalizeSaveKey(cFileScope + "/" + name)
	if err != nil {
		return false
	}
	rest, under := strings.CutPrefix(key, cFileScope+"/")
	return under && rest != ""
}

func (runtime *initializationRuntime) wipicFileHandle(thread *armcore.Thread) (*runtimeCFileHandle, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return nil, err
	}
	state, ok := runtime.cFileHandles[handle]
	if !ok {
		return nil, fmt.Errorf("KTF database handle %#x is not open", handle)
	}
	return state, nil
}

func (runtime *initializationRuntime) wipicFileStream(thread *armcore.Thread, write bool) (uint32, error) {
	state, err := runtime.wipicFileHandle(thread)
	if err != nil {
		return wipicErrorInvalid, nil
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
		return wipicErrorInvalid, nil
	}
	if length > maxCFileBytes {
		if write {
			return wipicErrorNoSpace, nil
		}
		return wipicErrorInvalid, nil
	}
	if write {
		data := make([]byte, length)
		if err := runtime.client.core.Memory().Read(buffer, data); err != nil {
			return 0, fmt.Errorf("read KTF database write buffer: %w", err)
		}
		end := state.position + int(length)
		if end > maxCFileBytes {
			// Past the bound one file may hold, which a write fails as a
			// file system out of room does.
			return wipicErrorNoSpace, nil
		}
		staged := make([]byte, max(end, len(state.store.data)))
		copy(staged, state.store.data)
		copy(staged[state.position:], data)
		if err := runtime.saveChanges(map[string][]byte{"db/" + state.store.name: staged}, databaseRemovedKey, runtime.removedDatabases(), map[string]bool{state.store.name: false}); err != nil {
			return 0, err
		}
		state.store.data, state.position = staged, end
		runtime.bindDetachedCFile(state.store)
		return length, nil
	}
	remaining := len(state.store.data) - state.position
	if remaining <= 0 {
		return 0, nil
	}
	count := int(length)
	if count > remaining {
		count = remaining
	}
	if err := runtime.client.core.Memory().Write(buffer, state.store.data[state.position:state.position+count]); err != nil {
		return 0, fmt.Errorf("write KTF database read buffer: %w", err)
	}
	state.position += count
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
func (runtime *initializationRuntime) wipicFileStatByName(thread *armcore.Thread) (uint32, error) {
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
		return 0, fmt.Errorf("read KTF database name: %w", err)
	}
	if nameTooLong(name) {
		return wipicErrorLongName, nil
	}
	if listingPath(name) != "" && !cFileNameKeyed(name) {
		runtime.countDiagnostic(fmt.Sprintf("cdb stat %s -> bad name", name))
		return wipicErrorBadFileName, nil
	}
	if store, ok := runtime.cFiles[name]; ok {
		runtime.countDiagnostic(fmt.Sprintf("cdb stat %s -> %d", name, len(store.data)))
		return runtime.writeFileStat(outAddress, uint32(len(store.data)), 0)
	}
	if seed, ok := runtime.databaseSeed(name); ok {
		// The save, then the packaged copy, as before — through the removal
		// list, which MC_fsIsExist and an open consult as well. A removed
		// name leaves an empty entry under its key, because the store cannot
		// delete, and read directly that was a file of zero bytes.
		runtime.countDiagnostic(fmt.Sprintf("cdb stat %s -> %d", name, len(seed)))
		return runtime.writeFileStat(outAddress, uint32(len(seed)), 0)
	}
	directory, err := runtime.cFileIsDirectory(name)
	if err != nil {
		runtime.countDiagnostic(fmt.Sprintf("cdb stat error: %v", err))
		return wipicErrorGeneric, nil
	}
	if directory {
		runtime.countDiagnostic(fmt.Sprintf("cdb stat %s -> directory", name))
		return runtime.writeFileStat(outAddress, 0, isDirectory)
	}
	runtime.countDiagnostic(fmt.Sprintf("cdb stat %s -> missing", name))
	return wipicErrorNotFound, nil
}

// writeFileStat fills the MH_FileInfo the stat was handed and answers success.
// A caller that passed no record still gets the answer.
func (runtime *initializationRuntime) writeFileStat(outAddress, size, attributes uint32) (uint32, error) {
	if outAddress == 0 {
		return 0, nil
	}
	record := make([]byte, 12)
	binary.LittleEndian.PutUint32(record[0:], attributes)
	binary.LittleEndian.PutUint32(record[8:], size)
	if err := runtime.client.core.Memory().Write(outAddress, record); err != nil {
		return 0, fmt.Errorf("write KTF database stat: %w", err)
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
func (runtime *initializationRuntime) wipicFileTell(thread *armcore.Thread) (uint32, error) {
	state, err := runtime.wipicFileHandle(thread)
	if err != nil {
		return wipicErrorBadFD, nil
	}
	return uint32(state.position), nil
}

// wipicFileSeek implements the KTF select-record extension, a seek over
// the open record's byte cursor.
func (runtime *initializationRuntime) wipicFileSeek(thread *armcore.Thread) (uint32, error) {
	state, err := runtime.wipicFileHandle(thread)
	if err != nil {
		return wipicErrorInvalid, nil
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
		base = state.position
	case 2:
		base = len(state.store.data)
	default:
		return wipicErrorInvalid, nil
	}
	position := base + int(int32(offset))
	if position < 0 {
		position = 0
	}
	if position > len(state.store.data) {
		position = len(state.store.data)
	}
	state.position = position
	return uint32(position), nil
}
