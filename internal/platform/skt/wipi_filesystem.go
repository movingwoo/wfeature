package skt

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// WIPI's FileSystem over the store the XFile and File handles already share.
//
// Every answer here is the WIPI storage contract's, docs/wipi-storage.md,
// which KTF and LGT give too; this file is where SKT keeps what the contract
// asks for. A path is the save key the handles use, under "fs/", so what
// FileSystem.exists says is there is what an open finds. The access flag
// every path-naming member takes picks the one directory a title has, so the
// two forms of a member are one handler — remove alone answers differently
// with the flag, as the contract has it.
//
// **The directories mkdir made are a list beside the saves**, at
// wipiDirectoriesKey. The store keeps files and nothing else, so a directory
// is the root, a name on the list, or a name with a file below it, and the
// list is what keeps a directory made and left empty across a restart. A save
// tree from before the list has none, which is no directory made.
//
// com.xce.io.XFile keeps its own answers: nothing here reaches it.

const (
	// wipiStorageBytes is the space a title has: totalSpace, and what
	// available counts down from.
	wipiStorageBytes = 1 << 20
	// wipiFileMaxBytes bounds one file a WIPI File writes, the contract's
	// bound on untrusted input. XFile keeps its own, xFileMaxBytes.
	wipiFileMaxBytes = 4 << 20
	// wipiDirectoriesKey is the directory list. It is a platform key outside
	// every scope a title names — "fs/" for its files, "rms/" for its record
	// stores, "jdb/" for its databases — so no path, XFile name or database
	// name reaches it. Its form is the removal list's: the paths below "fs/",
	// one a line, in name order.
	wipiDirectoriesKey = "wipi/directories"
)

func (runtime *Runtime) wipiFileSystemRegistrations() []nativeRegistration {
	const text = "Ljava/lang/String;"
	registrations := []nativeRegistration{
		{wipi.FileClass, "write", "(I)I", runtime.wipiFileWriteByte},
		{wipi.FileSystemClass, "available", "()I", runtime.wipiFileSystemAvailable},
		{wipi.FileSystemClass, "totalSpace", "()I", wipiFileSystemTotalSpace},
		{wipi.FileSystemClass, "toCString", "(" + text + ")[B", wipiToCString},
		{wipi.FileSystemClass, "sizeOf", "(" + text + ")I", runtime.wipiFileSystemSizeOf},
		{wipi.FileSystemClass, "remove", "(" + text + ")V", runtime.wipiFileRemove(false)},
		{wipi.FileSystemClass, "remove", "(" + text + "I)V", runtime.wipiFileRemove(true)},
		{wipi.FileSystemClass, "rename", "(" + text + text + ")V", runtime.wipiFileSystemRename(false)},
		{wipi.FileSystemClass, "rename", "(" + text + text + "I)V", runtime.wipiFileSystemRename(true)},
	}
	for _, member := range []struct {
		name, result string
		method       jvm.NativeMethod
	}{
		{"exists", "Z", runtime.wipiFileSystemExists},
		{"isFile", "Z", runtime.wipiFileSystemIsFile},
		{"isDirectory", "Z", runtime.wipiFileSystemIsDirectory},
		{"getCreationTime", "I", runtime.wipiFileSystemCreationTime},
		{"list", "Ljava/util/Vector;", runtime.wipiFileSystemList},
		{"mkdir", "V", runtime.wipiFileSystemMkdir},
		{"rmdir", "V", runtime.wipiFileSystemRmdir},
	} {
		registrations = append(registrations,
			nativeRegistration{wipi.FileSystemClass, member.name, "(" + text + ")" + member.result, member.method},
			nativeRegistration{wipi.FileSystemClass, member.name, "(" + text + "I)" + member.result, member.method})
	}
	return registrations
}

func wipiFileSystemTotalSpace(*jvm.VM, []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(wipiStorageBytes), nil
}

func (runtime *Runtime) wipiFileSystemAvailable(*jvm.VM, []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(runtime.wipiAvailable()), nil
}

// wipiAvailable is FileSystem.available and DataBase.getSizeAvailable: the
// title's 1 MiB less what it has stored, never below zero. What it has stored
// is each file it wrote, less the archive's file of the same name, and each
// database's records; the removal list, the directory list and the database
// index are the platform's and are not counted. Nothing is refused against
// this figure. It takes fileMu and then wipiDatabaseMu, one after the other,
// so a caller holds neither.
func (runtime *Runtime) wipiAvailable() int32 {
	runtime.fileMu.Lock()
	files := runtime.wipiFileBytesLocked()
	runtime.fileMu.Unlock()
	runtime.wipiDatabaseMu.Lock()
	databases := runtime.wipiDatabaseBytesLocked()
	runtime.wipiDatabaseMu.Unlock()
	return int32(max(wipiStorageBytes-files-databases, 0))
}

// wipiFileBytesLocked answers what the title's files take: for each file it
// wrote, in this session or an earlier one, its size less the size of the
// archive's file of the same name, and nothing for one it never wrote or that
// is no longer than the archive's — a title does not get space back by
// shortening a file it shipped with. The figure declares no exception, so a
// save that cannot be read is left out. The caller holds fileMu.
func (runtime *Runtime) wipiFileBytesLocked() int {
	removed, err := runtime.xFileRemovalsLocked()
	if err != nil {
		runtime.logStorageFigure(err)
		return 0
	}
	keys, _, err := backend.ListSaves(runtime.saveStoreBoundary(), strings.TrimSuffix(xFileSaveScope, "/"))
	if err != nil {
		runtime.logStorageFigure(err)
	}
	candidates := make(map[string]bool, len(keys)+len(runtime.files))
	for _, key := range keys {
		candidates[key] = true
	}
	for key := range runtime.files {
		candidates[key] = true
	}
	used := 0
	for key := range candidates {
		name := strings.TrimPrefix(key, xFileSaveScope)
		if _, err := xFileKey(name); err != nil {
			continue
		}
		node := runtime.files[key]
		if node == nil {
			if removed[name] {
				continue
			}
			if node, err = runtime.xFilePeekLocked(key); err != nil {
				runtime.logStorageFigure(err)
				continue
			}
		}
		if !node.present || node.borrowed {
			continue
		}
		shipped := 0
		if packaged, ok := runtime.packagedFile(name); ok {
			shipped = len(packaged)
		}
		used += max(len(node.data)-shipped, 0)
	}
	return used
}

func (runtime *Runtime) logStorageFigure(err error) {
	if runtime.logger != nil {
		runtime.logger.Debug("WIPI storage figure left out a save it could not read", "error", err)
	}
}

// wipiToCString is FileSystem.toCString: the string in the handset's charset,
// EUC-KR, which is ASCII for English and KS C 5601 for Korean as the
// specification names them, with the NUL a C string ends in.
func wipiToCString(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	text, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	array, err := newByteArray(vm, append(backend.EncodeEUCKR(text), 0))
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

// wipiFileWriteWindow is File.write(byte[], int, int), and the whole-array
// form and the output streams go through it: the handle's own write, with a
// file bounded at wipiFileMaxBytes. A write past the bound is the contract's
// IOException; a handle that cannot write keeps the -1 XFile answers.
func (runtime *Runtime) wipiFileWriteWindow(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	written, outcome, err := runtime.writeXFile(arguments, wipiFileMaxBytes)
	if err != nil {
		return jvm.VoidValue(), err
	}
	switch outcome {
	case xFileTooLarge:
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, fmt.Sprintf("a file holds at most %d bytes", wipiFileMaxBytes))
	case xFileNotWritable:
		return jvm.IntValue(-1), nil
	}
	return jvm.IntValue(written), nil
}

// wipiFileWriteByte is File.write(int): one byte through the same write,
// answering the one byte written. A handle that cannot take it — closed,
// opened only for reading, or at the bound — is the specification's
// IOException.
func (runtime *Runtime) wipiFileWriteByte(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	value, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	one := jvm.ReferenceValue(jvm.NewByteArray([]byte{byte(value)}))
	written, err := runtime.wipiFileWriteWindow(vm, []jvm.Value{arguments[0], one, jvm.IntValue(0), jvm.IntValue(1)})
	if err != nil {
		return jvm.VoidValue(), err
	}
	if count, _ := written.Int32(); count != 1 {
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "file is not open for writing")
	}
	return jvm.IntValue(1), nil
}

// wipiName is a WIPI name with its backslashes read as the slashes they are:
// the contract has a backslash separate segments as a slash does, on every
// platform, before anything else is asked of the name. XFile names are not
// WIPI names and keep refusing one.
func wipiName(name string) string {
	return strings.ReplaceAll(name, "\\", "/")
}

// wipiFileSystemPath reads a path argument as the contract reads every name
// (docs/wipi-storage.md, "Names and areas"): null is NullPointerException, a
// backslash separates segments as a slash does, "", "/" and "." are the root,
// and any other name is its save key, with leading separators, empty segments
// and "." segments dropped. A name longer than 255 bytes as the key keeps it,
// and one the store cannot keep — a ".." segment, a NUL, the removal list's own
// name — are IOException.
func wipiFileSystemPath(arguments []jvm.Value, index int) (key string, root bool, err error) {
	name, err := stringArgument(arguments, index)
	if err != nil {
		return "", false, err
	}
	name = wipiName(name)
	if wipiRootPath(name) {
		return xFileSaveScope, true, nil
	}
	if key, err = xFileKey(name); err != nil {
		return "", false, err
	}
	if length := len(key) - len(xFileSaveScope); length > int(wipi.MaxFilenameLength) {
		return "", false, newGuestException(jvm.IOExceptionClass,
			fmt.Sprintf("a name of %d bytes is longer than %d", length, wipi.MaxFilenameLength))
	}
	return key, false, nil
}

func wipiRootPath(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part != "" && part != "." {
			return false
		}
	}
	return true
}

// wipiFilePresentLocked answers whether a path holds a file, as exists and an
// open see it. The caller holds fileMu.
func (runtime *Runtime) wipiFilePresentLocked(key string) (bool, error) {
	node, err := runtime.xFilePeekLocked(key)
	if err != nil {
		return false, err
	}
	return node.present, nil
}

// wipiDirectoriesLocked answers the directory list, read from the store the
// first time a session asks. A line that is not a path a title could make is
// left out, and the next mkdir or rmdir writes the list without it. The
// caller holds fileMu.
func (runtime *Runtime) wipiDirectoriesLocked() (map[string]bool, error) {
	if runtime.wipiDirectories != nil {
		return runtime.wipiDirectories, nil
	}
	made := make(map[string]bool)
	data, found, err := backend.ReadSave(runtime.saveStoreBoundary(), wipiDirectoriesKey)
	if err != nil {
		return nil, newGuestException(jvm.IOExceptionClass, "the directory list cannot be read: "+err.Error())
	}
	if found {
		for _, name := range splitRemovalList(data) {
			if key, err := xFileKey(name); err == nil && key == xFileSaveScope+name {
				made[key] = true
			}
		}
	}
	runtime.wipiDirectories = made
	return made, nil
}

// storeWIPIDirectoriesLocked writes the list as a mkdir or an rmdir leaves it,
// and changes the session only once the store has it: a directory the title
// was told it made is one a later session finds, and one the store refused is
// the IOException the specification gives a directory that could not be made
// or removed. The caller holds fileMu.
func (runtime *Runtime) storeWIPIDirectoriesLocked(made map[string]bool) error {
	if store := runtime.saveStoreBoundary(); store != nil {
		names := make(map[string]bool, len(made))
		for key := range made {
			names[strings.TrimPrefix(key, xFileSaveScope)] = true
		}
		// Every write through the save boundary takes this turn.
		runtime.savePendingMu.Lock()
		err := store.StoreSave(wipiDirectoriesKey, joinRemovalList(names))
		runtime.savePendingMu.Unlock()
		if err != nil {
			return newGuestException(jvm.IOExceptionClass, "the directory list cannot be stored: "+err.Error())
		}
	}
	runtime.wipiDirectories = made
	return nil
}

// wipiFilesBelowLocked answers the keys of the files below a directory, prefix
// being the directory's key and a slash, or the file scope for the root.
//
// A file is below it when exists would find it: a file of this session, a
// save of an earlier one that the removal list does not hide, or a file the
// archive packages. The saves come from listing the store, which is the one
// question asked of what is there rather than of a name; without listStore,
// or with a store that cannot list, only the names this session knows are
// answered for. The removal list's own key is no file. The caller holds
// fileMu.
func (runtime *Runtime) wipiFilesBelowLocked(prefix string, listStore bool) ([]string, error) {
	removed, err := runtime.xFileRemovalsLocked()
	if err != nil {
		return nil, err
	}
	stored := make(map[string]bool)
	if listStore {
		keys, _, err := backend.ListSaves(runtime.saveStoreBoundary(), strings.TrimSuffix(prefix, "/"))
		if err != nil {
			return nil, newGuestException(jvm.IOExceptionClass, err.Error())
		}
		for _, key := range keys {
			stored[key] = true
		}
	}
	candidates := maps.Clone(stored)
	for key := range runtime.files {
		candidates[key] = true
	}
	if runtime.Archive != nil {
		for name := range runtime.Archive.Entries {
			// An archive name that is not text is a Korean name in the
			// handset's charset, and the archive answers its decoded spelling
			// too; only that one can come back to the title as a String.
			if !utf8.ValidString(name) {
				continue
			}
			if key, err := xFileKey(name); err == nil {
				candidates[key] = true
			}
		}
	}
	files := make([]string, 0, len(candidates))
	for key := range candidates {
		if len(key) <= len(prefix) || !strings.HasPrefix(key, prefix) {
			continue
		}
		name := strings.TrimPrefix(key, xFileSaveScope)
		if _, err := xFileKey(name); err != nil {
			continue
		}
		present := false
		switch node := runtime.files[key]; {
		case node != nil:
			present = node.present
		case removed[name]:
		case stored[key]:
			present = true
		default:
			_, present = runtime.packagedFile(name)
		}
		if present {
			files = append(files, key)
		}
	}
	slices.Sort(files)
	return files, nil
}

// wipiDirectoriesBelowLocked answers the listed directories below a
// directory. The caller holds fileMu.
func (runtime *Runtime) wipiDirectoriesBelowLocked(prefix string) ([]string, error) {
	listed, err := runtime.wipiDirectoriesLocked()
	if err != nil {
		return nil, err
	}
	var made []string
	for key := range listed {
		if len(key) > len(prefix) && strings.HasPrefix(key, prefix) {
			made = append(made, key)
		}
	}
	slices.Sort(made)
	return made, nil
}

// wipiDirectoryLocked answers whether a path is a directory: on the list, or
// with a file or a listed directory below it. A file of the name makes it a
// file; the contract says a name that is a file is not a directory. The
// session's own names are looked at before the store is listed, so asking
// about a name the session knows costs no listing. The caller holds fileMu.
func (runtime *Runtime) wipiDirectoryLocked(key string) (bool, error) {
	if present, err := runtime.wipiFilePresentLocked(key); err != nil || present {
		return false, err
	}
	listed, err := runtime.wipiDirectoriesLocked()
	if err != nil {
		return false, err
	}
	prefix := key + "/"
	made, err := runtime.wipiDirectoriesBelowLocked(prefix)
	if err != nil || listed[key] || len(made) > 0 {
		return err == nil, err
	}
	for _, listStore := range []bool{false, true} {
		files, err := runtime.wipiFilesBelowLocked(prefix, listStore)
		if err != nil || len(files) > 0 {
			return len(files) > 0, err
		}
	}
	return false, nil
}

// wipiFileSystemExists is FileSystem.exists: a file or a directory of the
// name. XFile.exists, which shares the file half, still answers for files
// alone.
func (runtime *Runtime) wipiFileSystemExists(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	key, root, err := wipiFileSystemPath(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if root {
		return jvm.IntValue(1), nil
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	present, err := runtime.wipiFilePresentLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if present {
		return jvm.IntValue(1), nil
	}
	directory, err := runtime.wipiDirectoryLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return booleanValue(directory), nil
}

func (runtime *Runtime) wipiFileSystemIsFile(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	key, root, err := wipiFileSystemPath(arguments, 0)
	if err != nil || root {
		return jvm.IntValue(0), err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	present, err := runtime.wipiFilePresentLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return booleanValue(present), nil
}

func (runtime *Runtime) wipiFileSystemIsDirectory(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	key, root, err := wipiFileSystemPath(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if root {
		return jvm.IntValue(1), nil
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	directory, err := runtime.wipiDirectoryLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return booleanValue(directory), nil
}

// wipiFileSystemSizeOf is FileSystem.sizeOf: a file's length, and -1 for a
// name that is not a file, as XFile.filesize answers; the name is read as the
// contract reads it.
func (runtime *Runtime) wipiFileSystemSizeOf(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	key, root, err := wipiFileSystemPath(arguments, 0)
	if err != nil || root {
		return jvm.IntValue(-1), err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	node, err := runtime.xFilePeekLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !node.present {
		return jvm.IntValue(-1), nil
	}
	return jvm.IntValue(int32(len(node.data))), nil
}

// wipiFileSystemCreationTime is getCreationTime, in seconds. **The store keeps
// no creation time**, only bytes, so a file answers 0, the start of the epoch:
// a fixed number at least orders every file the same way in every session,
// where the time of the session that read it would make an old save look new.
// A directory answers the specification's -1, and a name that is neither is
// IOException.
func (runtime *Runtime) wipiFileSystemCreationTime(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	key, root, err := wipiFileSystemPath(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if root {
		return jvm.IntValue(-1), nil
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	present, err := runtime.wipiFilePresentLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if present {
		return jvm.IntValue(0), nil
	}
	directory, err := runtime.wipiDirectoryLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !directory {
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "no such file: "+strings.TrimPrefix(key, xFileSaveScope))
	}
	return jvm.IntValue(-1), nil
}

// wipiFileSystemList is FileSystem.list: the names directly below a directory,
// a file's and a directory's alike, each once and without the directory's
// path, in name order. The saves are listed from the store, so a session finds
// what earlier ones wrote, and the directories from the list; a store that
// cannot list leaves the names this session knows.
func (runtime *Runtime) wipiFileSystemList(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	key, root, err := wipiFileSystemPath(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.fileMu.Lock()
	names, err := runtime.wipiListLocked(key, root)
	runtime.fileMu.Unlock()
	if err != nil {
		return jvm.VoidValue(), err
	}
	vector, err := vm.NewObject(jvm.VectorClass, "()V")
	if err != nil {
		return jvm.VoidValue(), err
	}
	for _, name := range names {
		if _, err := vm.InvokeVirtual(vector, "addElement", "(Ljava/lang/Object;)V", jvm.ReferenceValue(vm.NewString(name))); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.ReferenceValue(vector), nil
}

// wipiListLocked answers a directory's names, or the IOException the contract
// gives a name that is not a directory. The caller holds fileMu.
func (runtime *Runtime) wipiListLocked(key string, root bool) ([]string, error) {
	prefix := xFileSaveScope
	if !root {
		directory, err := runtime.wipiDirectoryLocked(key)
		if err != nil {
			return nil, err
		}
		if !directory {
			return nil, newGuestException(jvm.IOExceptionClass, "not a directory: "+strings.TrimPrefix(key, xFileSaveScope))
		}
		prefix = key + "/"
	}
	files, err := runtime.wipiFilesBelowLocked(prefix, true)
	if err != nil {
		return nil, err
	}
	made, err := runtime.wipiDirectoriesBelowLocked(prefix)
	if err != nil {
		return nil, err
	}
	children := make(map[string]bool, len(files)+len(made))
	for _, below := range append(files, made...) {
		child, _, _ := strings.Cut(below[len(prefix):], "/")
		children[child] = true
	}
	return slices.Sorted(maps.Keys(children)), nil
}

// wipiFileSystemMkdir is FileSystem.mkdir: the name goes on the directory
// list, stored before the call returns. A name a file or a directory already
// has, the root among them, is IOException. A missing parent is not refused,
// because saves have always been written below directories nobody made.
func (runtime *Runtime) wipiFileSystemMkdir(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	key, root, err := wipiFileSystemPath(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if root {
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "the root directory exists")
	}
	name := strings.TrimPrefix(key, xFileSaveScope)
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	present, err := runtime.wipiFilePresentLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if present {
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "a file has that name: "+name)
	}
	directory, err := runtime.wipiDirectoryLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if directory {
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "directory exists: "+name)
	}
	if strings.ContainsAny(name, "\n\r") {
		// The list is a name per line.
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "cannot make a directory of "+name)
	}
	listed, err := runtime.wipiDirectoriesLocked()
	if err != nil {
		return jvm.VoidValue(), err
	}
	staged := maps.Clone(listed)
	staged[key] = true
	return jvm.VoidValue(), runtime.storeWIPIDirectoriesLocked(staged)
}

// wipiFileSystemRmdir is FileSystem.rmdir: it takes a listed directory with
// nothing below it off the list, stored before the call returns, and throws
// IOException for the contract's three cases: something below it, a name that
// is not a directory, and the root.
func (runtime *Runtime) wipiFileSystemRmdir(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	key, root, err := wipiFileSystemPath(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if root {
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "the root directory cannot be removed")
	}
	name := strings.TrimPrefix(key, xFileSaveScope)
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	present, err := runtime.wipiFilePresentLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if present {
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "not a directory: "+name)
	}
	prefix := key + "/"
	files, err := runtime.wipiFilesBelowLocked(prefix, true)
	if err != nil {
		return jvm.VoidValue(), err
	}
	made, err := runtime.wipiDirectoriesBelowLocked(prefix)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(files) > 0 || len(made) > 0 {
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "directory is not empty: "+name)
	}
	listed, err := runtime.wipiDirectoriesLocked()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !listed[key] {
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "not a directory: "+name)
	}
	staged := maps.Clone(listed)
	delete(staged, key)
	return jvm.VoidValue(), runtime.storeWIPIDirectoriesLocked(staged)
}

// wipiFileSystemRename is FileSystem.rename. The flagged form takes names in
// the root of the title's directory, since the specification lets neither
// name carry a directory there; the other takes two paths in one directory.
// Only a file is renamed, and never onto a name a file or a directory has —
// the C API's rename refuses an existing new name the same way.
//
// **The new path's bytes and the old path's removal are stored in one batch**
// (storeXFilesLocked), so a later session finds the file under one name and
// never under both or neither. A batch the store refuses undoes the rename and
// throws IOException: the specification's answer to a rename that failed, and
// the title still has its file under the old name. Handles left open on the
// old path read a removed file, as they do after FileSystem.remove.
func (runtime *Runtime) wipiFileSystemRename(flagged bool) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		oldKey, oldRoot, err := wipiFileSystemPath(arguments, 0)
		if err != nil {
			return jvm.VoidValue(), err
		}
		newKey, newRoot, err := wipiFileSystemPath(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if oldRoot || newRoot {
			return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "the root directory cannot be renamed")
		}
		oldName := strings.TrimPrefix(oldKey, xFileSaveScope)
		newName := strings.TrimPrefix(newKey, xFileSaveScope)
		switch {
		case strings.ContainsAny(oldName+newName, "\n\r"):
			// The removal list is a name per line.
			return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "cannot rename "+oldName)
		case flagged && (strings.Contains(oldName, "/") || strings.Contains(newName, "/")):
			return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "a name with a directory: "+oldName+", "+newName)
		case path.Dir(oldName) != path.Dir(newName):
			return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "not in one directory: "+oldName+", "+newName)
		}
		runtime.fileMu.Lock()
		defer runtime.fileMu.Unlock()
		oldNode, err := runtime.xFilePeekLocked(oldKey)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if !oldNode.present {
			return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "no such file: "+oldName)
		}
		if oldKey == newKey {
			return jvm.VoidValue(), nil
		}
		if taken, err := runtime.wipiFilePresentLocked(newKey); err != nil || taken {
			if err == nil {
				err = newGuestException(jvm.IOExceptionClass, "file exists: "+newName)
			}
			return jvm.VoidValue(), err
		}
		if directory, err := runtime.wipiDirectoryLocked(newKey); err != nil || directory {
			if err == nil {
				err = newGuestException(jvm.IOExceptionClass, "a directory has that name: "+newName)
			}
			return jvm.VoidValue(), err
		}
		removed, err := runtime.xFileRemovalsLocked()
		if err != nil {
			return jvm.VoidValue(), err
		}
		newNode, err := runtime.xFileNodeLocked(newKey)
		if err != nil {
			return jvm.VoidValue(), err
		}
		oldBefore, newBefore := *oldNode, *newNode
		newNode.data, newNode.borrowed, newNode.present, newNode.dirty = oldNode.data, oldNode.borrowed, true, true
		oldNode.data, oldNode.borrowed, oldNode.present = nil, false, false
		// As a removal does: only what a later session would otherwise find
		// under the old name is written down.
		oldNode.dirty = !removed[oldName] && (oldNode.stored || oldNode.packaged)
		if err := runtime.storeXFilesLocked(newNode, oldNode); err != nil {
			*oldNode, *newNode = oldBefore, newBefore
			return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "the rename could not be stored: "+err.Error())
		}
		return jvm.VoidValue(), nil
	}
}
