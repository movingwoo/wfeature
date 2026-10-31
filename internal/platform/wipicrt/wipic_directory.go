package wipicrt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// The directory slots of WIPI C table 7: MC_fsRmDir at 9, MC_fsList at 10,
// MC_fsSetMode at 13 and MC_fsGetCounts at 14, each where the specification's
// `MC_fs*` order prints it (see wipic_filesystem.go).
//
// The store behind this table is one flat map from a name to its bytes, so a
// directory is a prefix the names share, plus the names MC_fsMkDir wrote
// down. What a directory holds is therefore every name the table can open,
// cut at the directory: the stores this session has open, the saves earlier
// sessions wrote, the files the archive ships and the Java side's files, all
// less what was removed. The saves are the part that needs the save store to
// list, because nothing else in a fresh session names them — and a title that
// lists its save folder to find its slots, told the folder is empty, would
// start a new game over them.

// cFileNames answers every name this table can open, by the key it is kept
// under, and the directories made through it.
func (runtime *Runtime) cFileNames() (map[string]bool, error) {
	names := make(map[string]bool)
	// The lists hold names as the title spelled them and the store gives
	// keys back without "." segments, so both are compared as paths.
	removed := ListingPaths(runtime.RemovedDatabases())
	for name := range runtime.CFiles {
		names[name] = true
	}
	saved, err := runtime.ListSaves(CFileScope)
	if err != nil {
		return nil, err
	}
	for _, name := range saved {
		if !removed[ListingPath(name)] && !ReservedStorageName(CFileScope, name) {
			names[name] = true
		}
	}
	// A packaged file, or one the Java side wrote, opens here too: an open
	// that finds no save of its own seeds the store from it (databaseSeed).
	// What either removal list hides is not there for that open, so it is
	// not listed either.
	guestRemoved := ListingPaths(runtime.RemovedGuestFiles())
	packaged, err := runtime.ListSaves(GuestFileScope)
	if err != nil {
		return nil, err
	}
	for _, table := range []map[string][]byte{runtime.Client.Files, runtime.GuestFiles} {
		for name := range table {
			packaged = append(packaged, name)
		}
	}
	for _, name := range packaged {
		name = WIPICFileKey(name)
		path := ListingPath(name)
		if path == "" || removed[path] || guestRemoved[path] || ReservedStorageName(GuestFileScope, name) {
			continue
		}
		names[name] = true
	}
	for name := range runtime.CreatedDirectories() {
		names[name] = true
	}
	return names, nil
}

// ListSaves answers the names the save store holds under one scope, without
// the scope. A store that cannot list answers none, and the names this
// session knows are the listing.
func (runtime *Runtime) ListSaves(scope string) ([]string, error) {
	keys, _, err := backend.ListSaves(runtime.Client.SaveStore, scope)
	if err != nil {
		return nil, fmt.Errorf("list %s saves under %s: %w", runtime.platformName(), scope, err)
	}
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		if name, under := strings.CutPrefix(key, scope+"/"); under && name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// ListingPath is a name as a path of segments: no empty or "." segment, so
// "./save.dat", "//save.dat" and "save.dat" are the one file the save store
// keeps them as.
func ListingPath(name string) string {
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(name, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/")
}

// ListingPaths is a removal list's names as paths.
func ListingPaths(names map[string]bool) map[string]bool {
	paths := make(map[string]bool, len(names))
	for name, listed := range names {
		if listed {
			paths[ListingPath(name)] = true
		}
	}
	return paths
}

// cFileDirectory answers the entries directly under a directory, files and
// directories alike, in name order. "" is the program's own directory.
func (runtime *Runtime) cFileDirectory(directory string) ([]string, error) {
	names, err := runtime.cFileNames()
	if err != nil {
		return nil, err
	}
	prefix := ListingPath(directory)
	if prefix != "" {
		prefix += "/"
	}
	entries := make(map[string]bool)
	for name := range names {
		rest, under := strings.CutPrefix(ListingPath(name), prefix)
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

// readDirectoryName reads the directory argument of a listing slot, and the
// code a name it cannot take answers. The program's own directory is "" or
// "/", which is not a key of its own but is a directory to list; anything else
// has to be one.
func (runtime *Runtime) readDirectoryName(address uint32) (string, uint32, error) {
	name, err := runtime.readFileName(address)
	if err != nil {
		return "", 0, fmt.Errorf("read %s directory name: %w", runtime.platformName(), err)
	}
	switch {
	case NameTooLong(name):
		return "", WIPICErrorLongName, nil
	case ListingPath(name) == "":
		return "", 0, nil
	case !CFileNameKeyed(name):
		return "", WIPICErrorBadFileName, nil
	}
	return name, 0, nil
}

// wipicFileList serves MC_fsList(name, buf, bufSize, aMode): the names of
// the files and directories in a directory, each NUL-terminated one after
// another, the list ending in a second NUL ("file1\0file2\0\0"). An empty
// directory is the two NULs alone. A buffer the list does not fit answers
// M_E_SHORTBUF and is left as it was.
func (runtime *Runtime) wipicFileList(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	size, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	directory, code, err := runtime.readDirectoryName(nameAddress)
	if err != nil || code != 0 {
		return code, err
	}
	entries, err := runtime.cFileDirectory(directory)
	if err != nil {
		runtime.CountDiagnostic(fmt.Sprintf("fs list error: %v", err))
		return WIPICErrorGeneric, nil
	}
	packed := make([]byte, 0, 64)
	for _, entry := range entries {
		packed = append(packed, entry...)
		packed = append(packed, 0)
	}
	packed = append(packed, 0)
	if len(entries) == 0 {
		packed = append(packed, 0)
	}
	runtime.CountDiagnostic(fmt.Sprintf("fs list %s -> %d", directory, len(entries)))
	if buffer == 0 {
		return WIPICErrorGeneric, nil
	}
	if int32(size) < 0 || uint32(len(packed)) > size {
		return WIPICErrorShortBuf, nil
	}
	if err := runtime.Client.core.Memory().Write(buffer, packed); err != nil {
		return 0, fmt.Errorf("write %s directory listing: %w", runtime.platformName(), err)
	}
	return 0, nil
}

// wipicFileCount serves MC_fsGetCounts(dirName, aMode): how many entries the
// directory holds, sub-directories included — the entries MC_fsList names.
func (runtime *Runtime) wipicFileCount(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	directory, code, err := runtime.readDirectoryName(nameAddress)
	if err != nil || code != 0 {
		return code, err
	}
	entries, err := runtime.cFileDirectory(directory)
	if err != nil {
		runtime.CountDiagnostic(fmt.Sprintf("fs count error: %v", err))
		return WIPICErrorGeneric, nil
	}
	return uint32(len(entries)), nil
}

// wipicRemoveDirectory serves MC_fsRmDir(dirName, aMode). A directory is a
// name MC_fsMkDir wrote down, so removing one takes the name off that list,
// and only when nothing is under it: the specification refuses a directory
// that still holds a file or a directory with M_E_NOTEMPTY, and one that is
// not there with M_E_NOENT. The contents are asked first, so a directory that
// holds files without ever having been made answers that it is not empty
// rather than that it does not exist.
func (runtime *Runtime) wipicRemoveDirectory(thread *armcore.Thread) (uint32, error) {
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
	if !CFileNameKeyed(name) || !StorableName(CFileScope, name) {
		// The root among them: it is a directory, and not one a title can
		// take away.
		return WIPICErrorBadFileName, nil
	}
	entries, err := runtime.cFileDirectory(name)
	if err != nil {
		runtime.CountDiagnostic(fmt.Sprintf("fs rmdir error: %v", err))
		return WIPICErrorGeneric, nil
	}
	if len(entries) > 0 {
		runtime.CountDiagnostic(fmt.Sprintf("fs rmdir %s -> not empty", name))
		return WIPICErrorNotEmpty, nil
	}
	if !runtime.CreatedDirectories()[name] {
		runtime.CountDiagnostic(fmt.Sprintf("fs rmdir %s -> missing", name))
		return WIPICErrorNotFound, nil
	}
	if err := runtime.SaveChanges(nil, DirectoryListKey, runtime.CreatedDirectories(), map[string]bool{name: false}); err != nil {
		return 0, err
	}
	runtime.CountDiagnostic(fmt.Sprintf("fs rmdir %s", name))
	return 0, nil
}

// wipicFileSetMode serves MC_fsSetMode(fileName, fmode, aMode), which sets a
// file read-only, write-only or both. The specification spells the three
// modes two ways — 1 to 3 on this call's own page, `0x10`, `0x20` and `0x30` in
// its HAL page — and both are taken. The store keeps no mode, and nothing
// here refuses a write by one, so a mode is checked and accepted for a file or
// a directory that exists, and nothing else. A title that marks its save
// read-only and then writes it is not stopped, which on a handset it would
// be; no local title calls this slot at all.
func (runtime *Runtime) wipicFileSetMode(thread *armcore.Thread) (uint32, error) {
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
		return 0, fmt.Errorf("read %s file name: %w", runtime.platformName(), err)
	}
	if NameTooLong(name) {
		return WIPICErrorLongName, nil
	}
	if ListingPath(name) != "" && !CFileNameKeyed(name) {
		return WIPICErrorBadFileName, nil
	}
	switch mode {
	case 1, 2, 3, 0x10, 0x20, 0x30:
	default:
		return WIPICErrorInvalid, nil
	}
	exists, err := runtime.cFileExists(name)
	if err != nil {
		runtime.CountDiagnostic(fmt.Sprintf("fs setmode error: %v", err))
		return WIPICErrorGeneric, nil
	}
	if !exists {
		return WIPICErrorNotFound, nil
	}
	runtime.CountDiagnostic(fmt.Sprintf("fs setmode %s %d", name, mode))
	return 0, nil
}

// cFileIsFile answers whether a file of this name is there: a live store, a
// save, or the archive's copy.
func (runtime *Runtime) cFileIsFile(name string) bool {
	if _, live := runtime.CFiles[name]; live {
		return true
	}
	_, seeded := runtime.DatabaseSeed(name)
	return seeded
}

// cFileIsDirectory answers whether a name is a directory: the root, one
// MC_fsMkDir recorded, or one something lies below. A name that is a file is
// not a directory.
func (runtime *Runtime) cFileIsDirectory(name string) (bool, error) {
	path := ListingPath(name)
	if path == "" {
		return true, nil
	}
	if runtime.cFileIsFile(name) {
		return false, nil
	}
	if made := runtime.CreatedDirectories(); made[name] || made[path] {
		return true, nil
	}
	entries, err := runtime.cFileDirectory(name)
	return len(entries) > 0, err
}

// cFileExists is MC_fsIsExist's question: a file or a directory.
func (runtime *Runtime) cFileExists(name string) (bool, error) {
	if runtime.cFileIsFile(name) {
		return true, nil
	}
	return runtime.cFileIsDirectory(name)
}
