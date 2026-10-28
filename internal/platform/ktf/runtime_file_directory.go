package ktf

import (
	"fmt"
	"slices"
	"strings"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The directory half of org.kwis.msp.io.FileSystem, answered the way every
// platform answers it (docs/wipi-storage.md): a directory is the root, a name
// mkdir made, or a name a file lies below; a name that is a file is not one.
// The store keeps files under their full names, so the directories mkdir
// makes are a list beside them (guestDirectoryListKey), which is what keeps an
// empty one after a restart.

// fileSystemName reads a FileSystem name argument: null is the
// NullPointerException the specification gives every one of these calls, and
// a name longer than getMaxFilenameLength is its IOException.
func fileSystemName(value jvm.Value) (string, error) {
	object, err := value.Reference()
	if err != nil {
		return "", err
	}
	if object == nil {
		message := "a null file name"
		return "", &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/NullPointerException", Native: message},
			Message: message,
		}
	}
	name, ok := jvm.StringText(object)
	if !ok {
		return "", fmt.Errorf("FileSystem name is not a string")
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if nameTooLong(name) {
		return "", newGuestIOException(fmt.Sprintf("a name of %d bytes is longer than %d", len(name), maxWIPIFileName))
	}
	// A name the store cannot keep a file under — one that climbs out of
	// its directory, or one of the lists the platform keeps — is refused by
	// every call, as the C table refuses it with M_E_BADFILENAME.
	if slices.Contains(strings.Split(name, "/"), "..") || reservedStorageName(guestFileScope, listingPath(name)) {
		return "", newGuestIOException(fmt.Sprintf("%q is not a file name", name))
	}
	return name, nil
}

// fileSystemNameArgument is fileSystemName for the first argument.
func fileSystemNameArgument(arguments []jvm.Value, call string) (string, error) {
	if len(arguments) < 1 {
		return "", fmt.Errorf("FileSystem.%s expected a name", call)
	}
	return fileSystemName(arguments[0])
}

func javaBooleanValue(value bool) jvm.Value {
	if value {
		return jvm.IntValue(1)
	}
	return jvm.IntValue(0)
}

// runtimeFileSystemExists is `exists`: a file or a directory.
func runtimeFileSystemExists(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := fileSystemNameArgument(arguments, "exists")
	if err != nil {
		return jvm.VoidValue(), err
	}
	exists := runtime.guestIsFile(name)
	if !exists {
		if exists, err = runtime.guestIsDirectory(name); err != nil {
			return jvm.VoidValue(), newGuestIOException(err.Error())
		}
	}
	// The name and the answer both belong in the trace. A start-up gate is
	// often a file test, and a test that appears in the trace without its
	// path says only that the title asked something.
	runtime.countDiagnostic(fmt.Sprintf("exists %s found=%t", name, exists))
	return javaBooleanValue(exists), nil
}

// runtimeFileSystemIsFile is `isFile`: a file, and not a directory.
func runtimeFileSystemIsFile(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := fileSystemNameArgument(arguments, "isFile")
	if err != nil {
		return jvm.VoidValue(), err
	}
	return javaBooleanValue(runtime.guestIsFile(name)), nil
}

// runtimeFileSystemIsDirectory is `isDirectory`.
func runtimeFileSystemIsDirectory(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := fileSystemNameArgument(arguments, "isDirectory")
	if err != nil {
		return jvm.VoidValue(), err
	}
	directory, err := runtime.guestIsDirectory(name)
	if err != nil {
		return jvm.VoidValue(), newGuestIOException(err.Error())
	}
	return javaBooleanValue(directory), nil
}

// runtimeFileSystemMakeDirectory is `mkdir`: the name goes on the list, and a
// name that is already a file or a directory is the IOException the
// specification gives "the directory already exists".
func runtimeFileSystemMakeDirectory(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := fileSystemNameArgument(arguments, "mkdir")
	if err != nil {
		return jvm.VoidValue(), err
	}
	path := listingPath(name)
	if path == "" || runtime.guestIsFile(name) {
		return jvm.VoidValue(), newGuestIOException("already exists: " + name)
	}
	if !storableName(guestFileScope, path) {
		return jvm.VoidValue(), newGuestIOException("cannot make a directory named " + name)
	}
	directory, err := runtime.guestIsDirectory(name)
	if err != nil {
		return jvm.VoidValue(), newGuestIOException(err.Error())
	}
	if directory {
		return jvm.VoidValue(), newGuestIOException("already exists: " + name)
	}
	if err := runtime.markGuestDirectory(path, true); err != nil {
		return jvm.VoidValue(), newGuestIOException(err.Error())
	}
	runtime.countDiagnostic(fmt.Sprintf("fs java mkdir %s", path))
	return jvm.VoidValue(), nil
}

// runtimeFileSystemRemoveDirectory is `rmdir`: only an empty directory goes,
// and the root, a name with something below it, and a name that is not a
// directory are each the specification's IOException.
func runtimeFileSystemRemoveDirectory(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := fileSystemNameArgument(arguments, "rmdir")
	if err != nil {
		return jvm.VoidValue(), err
	}
	path := listingPath(name)
	if path == "" {
		return jvm.VoidValue(), newGuestIOException("cannot remove the program's own directory")
	}
	entries, err := runtime.guestDirectoryEntries(path)
	if err != nil {
		return jvm.VoidValue(), newGuestIOException(err.Error())
	}
	if len(entries) > 0 {
		return jvm.VoidValue(), newGuestIOException("directory is not empty: " + name)
	}
	if runtime.guestIsFile(name) || !runtime.guestDirectories()[path] {
		return jvm.VoidValue(), newGuestIOException("no such directory: " + name)
	}
	if err := runtime.markGuestDirectory(path, false); err != nil {
		return jvm.VoidValue(), newGuestIOException(err.Error())
	}
	return jvm.VoidValue(), nil
}

// runtimeFileSystemList is `list`: a Vector of the names directly below a
// directory, files and directories alike, without the directory's path and in
// name order. A name that is not a directory is the IOException the
// specification gives a directory that does not exist.
//
// It used to answer every file below the name with its whole path, from the
// archive and this session's writes only: a title that opened what it was
// handed opened the path twice over, and one that listed its save folder
// after a restart was told it had no saves.
func runtimeFileSystemList(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := fileSystemNameArgument(arguments, "list")
	if err != nil {
		return jvm.VoidValue(), err
	}
	directory, err := runtime.guestIsDirectory(name)
	if err != nil {
		return jvm.VoidValue(), newGuestIOException(err.Error())
	}
	if !directory {
		return jvm.VoidValue(), newGuestIOException("no such directory: " + name)
	}
	entries, err := runtime.guestDirectoryEntries(name)
	if err != nil {
		return jvm.VoidValue(), newGuestIOException(err.Error())
	}
	vector, err := vm.NewObject("java/util/Vector", "()V")
	if err != nil {
		return jvm.VoidValue(), err
	}
	for _, entry := range entries {
		if _, err := vm.InvokeVirtual(vector, "addElement", "(Ljava/lang/Object;)V", jvm.ReferenceValue(vm.NewString(entry))); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.ReferenceValue(vector), nil
}

// runtimeFileSystemCreationTime is `getCreationTime`. No creation time is
// kept, so a file answers 0 — 1970, the time MC_fsFileAttribute writes too —
// and a directory -1, as the specification gives it; a name that is neither
// is an IOException.
func runtimeFileSystemCreationTime(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := fileSystemNameArgument(arguments, "getCreationTime")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if runtime.guestIsFile(name) {
		return jvm.IntValue(0), nil
	}
	directory, err := runtime.guestIsDirectory(name)
	if err != nil {
		return jvm.VoidValue(), newGuestIOException(err.Error())
	}
	if directory {
		return jvm.IntValue(-1), nil
	}
	return jvm.VoidValue(), newGuestIOException("no such file: " + name)
}

// runtimeFileSystemRemove is `remove(name)`, whose specification throws for a
// file that is not there; the flagged form, runtimeFileSystemRemoveAny, says
// in as many words that it does not.
func runtimeFileSystemRemove(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := fileSystemNameArgument(arguments, "remove")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !runtime.guestIsFile(name) {
		return jvm.VoidValue(), newGuestIOException("no such file: " + name)
	}
	return runtimeFileSystemUnlink(runtime, vm, arguments)
}

// runtimeFileSystemRemoveAny is `remove(name, flag)`.
func runtimeFileSystemRemoveAny(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := fileSystemNameArgument(arguments, "remove")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !runtime.guestIsFile(name) {
		return jvm.VoidValue(), nil
	}
	return runtimeFileSystemUnlink(runtime, vm, arguments)
}

// runtimeFileSystemAvailable is `available`, and runtimeFileSystemTotalSpace
// `totalSpace`: the space every title has and what is left of it, the figures
// the WIPI C table answers too.
func runtimeFileSystemAvailable(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(int32(runtime.storageAvailable())), nil
}

func runtimeFileSystemTotalSpace(*initializationRuntime, *jvm.VM, []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(storageBudget), nil
}

func runtimeFileSystemNameLength(*initializationRuntime, *jvm.VM, []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(maxWIPIFileName), nil
}
