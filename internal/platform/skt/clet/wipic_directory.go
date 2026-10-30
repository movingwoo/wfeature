package clet

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
func (runtime *initializationRuntime) cFileNames() (map[string]bool, error) {
	names := make(map[string]bool)
	// The lists hold names as the title spelled them and the store gives
	// keys back without "." segments, so both are compared as paths.
	removed := listingPaths(runtime.removedDatabases())
	for name := range runtime.cFiles {
		names[name] = true
	}
	saved, err := runtime.listSaves(cFileScope)
	if err != nil {
		return nil, err
	}
	for _, name := range saved {
		if !removed[listingPath(name)] && !reservedStorageName(cFileScope, name) {
			names[name] = true
		}
	}
	// A packaged file, or one the Java side wrote, opens here too: an open
	// that finds no save of its own seeds the store from it (databaseSeed).
	// What either removal list hides is not there for that open, so it is
	// not listed either.
	guestRemoved := listingPaths(runtime.removedGuestFiles())
	packaged, err := runtime.listSaves(guestFileScope)
	if err != nil {
		return nil, err
	}
	for _, table := range []map[string][]byte{runtime.client.files, runtime.guestFiles} {
		for name := range table {
			packaged = append(packaged, name)
		}
	}
	for _, name := range packaged {
		name = wipicFileKey(name)
		path := listingPath(name)
		if path == "" || removed[path] || guestRemoved[path] || reservedStorageName(guestFileScope, name) {
			continue
		}
		names[name] = true
	}
	for name := range runtime.createdDirectories() {
		names[name] = true
	}
	return names, nil
}

// listSaves answers the names the save store holds under one scope, without
// the scope. A store that cannot list answers none, and the names this
// session knows are the listing.
func (runtime *initializationRuntime) listSaves(scope string) ([]string, error) {
	keys, _, err := backend.ListSaves(runtime.client.saveStore, scope)
	if err != nil {
		return nil, fmt.Errorf("list KTF saves under %s: %w", scope, err)
	}
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		if name, under := strings.CutPrefix(key, scope+"/"); under && name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// listingPath is a name as a path of segments: no empty or "." segment, so
// "./save.dat", "//save.dat" and "save.dat" are the one file the save store
// keeps them as.
func listingPath(name string) string {
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(name, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/")
}

// listingPaths is a removal list's names as paths.
func listingPaths(names map[string]bool) map[string]bool {
	paths := make(map[string]bool, len(names))
	for name, listed := range names {
		if listed {
			paths[listingPath(name)] = true
		}
	}
	return paths
}

// cFileDirectory answers the entries directly under a directory, files and
// directories alike, in name order. "" is the program's own directory.
func (runtime *initializationRuntime) cFileDirectory(directory string) ([]string, error) {
	names, err := runtime.cFileNames()
	if err != nil {
		return nil, err
	}
	prefix := listingPath(directory)
	if prefix != "" {
		prefix += "/"
	}
	entries := make(map[string]bool)
	for name := range names {
		rest, under := strings.CutPrefix(listingPath(name), prefix)
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
func (runtime *initializationRuntime) readDirectoryName(address uint32) (string, uint32, error) {
	name, err := runtime.readFileName(address)
	if err != nil {
		return "", 0, fmt.Errorf("read KTF directory name: %w", err)
	}
	switch {
	case nameTooLong(name):
		return "", wipicErrorLongName, nil
	case listingPath(name) == "":
		return "", 0, nil
	case !cFileNameKeyed(name):
		return "", wipicErrorBadFileName, nil
	}
	return name, 0, nil
}

// wipicFileList serves MC_fsList(name, buf, bufSize, aMode): the names of
// the files and directories in a directory, each NUL-terminated one after
// another, the list ending in a second NUL ("file1\0file2\0\0"). An empty
// directory is the two NULs alone. A buffer the list does not fit answers
// M_E_SHORTBUF and is left as it was.
func (runtime *initializationRuntime) wipicFileList(thread *armcore.Thread) (uint32, error) {
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
		runtime.countDiagnostic(fmt.Sprintf("fs list error: %v", err))
		return wipicErrorGeneric, nil
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
	runtime.countDiagnostic(fmt.Sprintf("fs list %s -> %d", directory, len(entries)))
	if buffer == 0 {
		return wipicErrorGeneric, nil
	}
	if int32(size) < 0 || uint32(len(packed)) > size {
		return wipicErrorShortBuf, nil
	}
	if err := runtime.client.core.Memory().Write(buffer, packed); err != nil {
		return 0, fmt.Errorf("write KTF directory listing: %w", err)
	}
	return 0, nil
}

// wipicFileCount serves MC_fsGetCounts(dirName, aMode): how many entries the
// directory holds, sub-directories included — the entries MC_fsList names.
func (runtime *initializationRuntime) wipicFileCount(thread *armcore.Thread) (uint32, error) {
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
		runtime.countDiagnostic(fmt.Sprintf("fs count error: %v", err))
		return wipicErrorGeneric, nil
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
func (runtime *initializationRuntime) wipicRemoveDirectory(thread *armcore.Thread) (uint32, error) {
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
	if !cFileNameKeyed(name) || !storableName(cFileScope, name) {
		// The root among them: it is a directory, and not one a title can
		// take away.
		return wipicErrorBadFileName, nil
	}
	entries, err := runtime.cFileDirectory(name)
	if err != nil {
		runtime.countDiagnostic(fmt.Sprintf("fs rmdir error: %v", err))
		return wipicErrorGeneric, nil
	}
	if len(entries) > 0 {
		runtime.countDiagnostic(fmt.Sprintf("fs rmdir %s -> not empty", name))
		return wipicErrorNotEmpty, nil
	}
	if !runtime.createdDirectories()[name] {
		runtime.countDiagnostic(fmt.Sprintf("fs rmdir %s -> missing", name))
		return wipicErrorNotFound, nil
	}
	if err := runtime.saveChanges(nil, directoryListKey, runtime.createdDirectories(), map[string]bool{name: false}); err != nil {
		return 0, err
	}
	runtime.countDiagnostic(fmt.Sprintf("fs rmdir %s", name))
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
func (runtime *initializationRuntime) wipicFileSetMode(thread *armcore.Thread) (uint32, error) {
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
		return 0, fmt.Errorf("read KTF file name: %w", err)
	}
	if nameTooLong(name) {
		return wipicErrorLongName, nil
	}
	if listingPath(name) != "" && !cFileNameKeyed(name) {
		return wipicErrorBadFileName, nil
	}
	switch mode {
	case 1, 2, 3, 0x10, 0x20, 0x30:
	default:
		return wipicErrorInvalid, nil
	}
	exists, err := runtime.cFileExists(name)
	if err != nil {
		runtime.countDiagnostic(fmt.Sprintf("fs setmode error: %v", err))
		return wipicErrorGeneric, nil
	}
	if !exists {
		return wipicErrorNotFound, nil
	}
	runtime.countDiagnostic(fmt.Sprintf("fs setmode %s %d", name, mode))
	return 0, nil
}

// cFileIsFile answers whether a file of this name is there: a live store, a
// save, or the archive's copy.
func (runtime *initializationRuntime) cFileIsFile(name string) bool {
	if _, live := runtime.cFiles[name]; live {
		return true
	}
	_, seeded := runtime.databaseSeed(name)
	return seeded
}

// cFileIsDirectory answers whether a name is a directory: the root, one
// MC_fsMkDir recorded, or one something lies below. A name that is a file is
// not a directory.
func (runtime *initializationRuntime) cFileIsDirectory(name string) (bool, error) {
	path := listingPath(name)
	if path == "" {
		return true, nil
	}
	if runtime.cFileIsFile(name) {
		return false, nil
	}
	if made := runtime.createdDirectories(); made[name] || made[path] {
		return true, nil
	}
	entries, err := runtime.cFileDirectory(name)
	return len(entries) > 0, err
}

// cFileExists is MC_fsIsExist's question: a file or a directory.
func (runtime *initializationRuntime) cFileExists(name string) (bool, error) {
	if runtime.cFileIsFile(name) {
		return true, nil
	}
	return runtime.cFileIsDirectory(name)
}
