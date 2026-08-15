package skt

import (
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// SKVM file modes from com.xce.io.XFile.
const (
	xFileRead  int32 = 1
	xFileWrite int32 = 2
)

// XFile save keys live under "fs/", the same scope KTF's guest filesystem
// uses, so one owner directory holds a title's files whichever platform wrote
// them. A file the game never wrote falls back to the JAR resource of the
// same name, which is how a game reads data it shipped.
const xFileSaveScope = "fs/"

// xFileCapacity is what fsavail reports against. There is no partition here,
// so a fixed budget is the honest answer, and it is the same one RMS uses.
const xFileCapacity = rmsCapacity

func (runtime *Runtime) xFileRegistrations() []nativeRegistration {
	const text = "Ljava/lang/String;"
	return []nativeRegistration{
		{skvm.XFileClass, "initHandle", "(I)V", runtime.initXFileHandle},
		{skvm.XFileClass, "initName", "(" + text + "I)V", runtime.initXFileName},
		{skvm.XFileClass, "available", "()I", runtime.xFileAvailable},
		{skvm.XFileClass, "close", "()V", runtime.xFileClose},
		{skvm.XFileClass, "flush", "()V", runtime.xFileFlush},
		{skvm.XFileClass, "read", "([BII)I", runtime.xFileRead},
		{skvm.XFileClass, "write", "([BII)I", runtime.xFileWrite},
		{skvm.XFileClass, "seek", "(II)I", runtime.xFileSeek},
		{skvm.XFileClass, "readdir", "()" + text, nullReference},
		{skvm.XFileClass, "exists", "(" + text + ")Z", runtime.xFileExists},
		{skvm.XFileClass, "filesize", "(" + text + ")I", runtime.xFileSize},
		{skvm.XFileClass, "fsavail", "()I", runtime.xFileAvail},
		{skvm.XFileClass, "fsused", "()I", runtime.xFileUsed},
		{skvm.XFileClass, "mkdir", "(" + text + ")V", runtime.ignoreVoid},
		{skvm.XFileClass, "rmdir", "(" + text + ")V", runtime.ignoreVoid},
		{skvm.XFileClass, "rmrdir", "(" + text + ")V", runtime.ignoreVoid},
		{skvm.XFileClass, "unlink", "(" + text + ")I", runtime.xFileUnlink},
	}
}

// xFileKey normalizes a guest path into the save key it lives under.
func xFileKey(name string) (string, error) {
	key, err := backend.NormalizeSaveKey(xFileSaveScope + strings.TrimPrefix(name, "/"))
	if err != nil {
		return "", newGuestException("java/io/IOException", err.Error())
	}
	return key, nil
}

// xFileResourceName is the JAR entry a path falls back to when no save exists.
func xFileResourceName(name string) string {
	return strings.TrimPrefix(name, "/")
}

func (runtime *Runtime) initXFileHandle(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	// A raw handle names a file this runtime never opened; there is no
	// handle table behind SKVM here, so the file starts empty and closed
	// rather than pretending to address someone else's descriptor.
	receiver.Native = &xFileData{}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) initXFileName(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	mode, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	key, err := xFileKey(name)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file := &xFileData{name: name, mode: mode, open: true, writable: mode&xFileWrite != 0}

	found := false
	if store := runtime.saveStoreBoundary(); store != nil {
		if data, ok := store.LoadSave(key); ok {
			file.data = data
			found = true
		}
	}
	if !found {
		if data, ok := runtime.Archive.Resource(xFileResourceName(name)); ok {
			file.data = append([]byte(nil), data...)
			found = true
		}
	}
	// Opening for writing creates the file. There is no separate create bit in
	// this API — the original runtime opens a writable file the way
	// RandomAccessFile's "rw" does, and a save is the case that proves it: one
	// title writes its three slots by opening `/k`, `/s` and `/w` for writing
	// and nothing ever creates them first, so a missing file refused here comes
	// back as the title's own `실패하였습니다. 저장 공간이 부족합니다.` — the
	// message its catch block shows for an IOException out of the save. A
	// read-only open of a file that is not there still fails, because that is a
	// title asking for something and being told it does not exist.
	if !found && mode&xFileWrite == 0 {
		return jvm.VoidValue(), newGuestException("java/io/IOException", "no such file: "+name)
	}
	receiver.Native = file
	return jvm.VoidValue(), nil
}

func xFileArgument(arguments []jvm.Value) (*xFileData, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "XFile is null")
	}
	file, ok := receiver.Native.(*xFileData)
	if !ok || file == nil {
		return nil, fmt.Errorf("receiver is not an XFile")
	}
	return file, nil
}

func (runtime *Runtime) xFileAvailable(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file.mu.Lock()
	defer file.mu.Unlock()
	return jvm.IntValue(int32(max(len(file.data)-file.cursor, 0))), nil
}

func (runtime *Runtime) xFileRead(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	array, values, err := primitiveArrayArgument(arguments, 1, jvm.TypeByte)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	length, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(values)) {
		return jvm.VoidValue(), newGuestException("java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("read range %d..%d of %d", offset, offset+length, len(values)))
	}
	file.mu.Lock()
	if !file.open || file.mode&xFileRead == 0 {
		file.mu.Unlock()
		return jvm.IntValue(-1), nil
	}
	count := min(int(length), len(file.data)-file.cursor)
	if count <= 0 {
		file.mu.Unlock()
		return jvm.IntValue(0), nil
	}
	chunk := append([]byte(nil), file.data[file.cursor:file.cursor+count]...)
	file.cursor += count
	file.mu.Unlock()

	copied := make([]jvm.Value, count)
	for index, symbol := range chunk {
		copied[index] = jvm.IntValue(int32(int8(symbol)))
	}
	if err := jvm.SetArrayRange(array, int(offset), copied); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(count)), nil
}

func (runtime *Runtime) xFileWrite(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data, err := recordBytesArgument(arguments, 1, 2, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file.mu.Lock()
	if !file.open || !file.writable {
		file.mu.Unlock()
		return jvm.IntValue(-1), nil
	}
	if file.cursor > len(file.data) {
		file.data = append(file.data, make([]byte, file.cursor-len(file.data))...)
	}
	end := file.cursor + len(data)
	if end > len(file.data) {
		file.data = append(file.data, make([]byte, end-len(file.data))...)
	}
	copy(file.data[file.cursor:end], data)
	file.cursor = end
	file.dirty = true
	file.mu.Unlock()
	return jvm.IntValue(int32(len(data))), nil
}

func (runtime *Runtime) xFileSeek(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	whence, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file.mu.Lock()
	defer file.mu.Unlock()
	base := 0
	switch whence {
	case 0:
		base = 0
	case 1:
		base = file.cursor
	case 2:
		base = len(file.data)
	default:
		return jvm.IntValue(-1), nil
	}
	position := base + int(offset)
	if position < 0 {
		return jvm.IntValue(-1), nil
	}
	file.cursor = position
	return jvm.IntValue(int32(position)), nil
}

func (runtime *Runtime) xFileFlush(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.persistXFile(file)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) xFileClose(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.persistXFile(file)
	file.mu.Lock()
	file.open = false
	file.mu.Unlock()
	return jvm.VoidValue(), nil
}

// persistXFile writes a modified file through the Host save boundary.
func (runtime *Runtime) persistXFile(file *xFileData) {
	file.mu.Lock()
	if !file.dirty || file.name == "" {
		file.mu.Unlock()
		return
	}
	data := append([]byte(nil), file.data...)
	name := file.name
	file.dirty = false
	file.mu.Unlock()

	store := runtime.saveStoreBoundary()
	if store == nil {
		return
	}
	key, err := xFileKey(name)
	if err != nil {
		return
	}
	if err := store.StoreSave(key, data); err != nil && runtime.logger != nil {
		runtime.logger.Debug("XFile store failed", "name", name, "error", err)
	}
}

// xFileContents resolves a path to its bytes without opening it: the save
// first, then the packaged resource.
func (runtime *Runtime) xFileContents(name string) ([]byte, bool) {
	if key, err := xFileKey(name); err == nil {
		if store := runtime.saveStoreBoundary(); store != nil {
			if data, ok := store.LoadSave(key); ok {
				return data, true
			}
		}
	}
	return runtime.Archive.Resource(xFileResourceName(name))
}

func (runtime *Runtime) xFileExists(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, ok := runtime.xFileContents(name); ok {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) xFileSize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data, ok := runtime.xFileContents(name)
	if !ok {
		return jvm.IntValue(-1), nil
	}
	return jvm.IntValue(int32(len(data))), nil
}

func (runtime *Runtime) xFileAvail(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(xFileCapacity), nil
}

func (runtime *Runtime) xFileUsed(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(0), nil
}

// xFileUnlink empties a file rather than removing it: the save boundary has
// no delete, and an empty file is what a later open then finds.
func (runtime *Runtime) xFileUnlink(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	key, err := xFileKey(name)
	if err != nil {
		return jvm.IntValue(-1), nil
	}
	store := runtime.saveStoreBoundary()
	if store == nil {
		return jvm.IntValue(-1), nil
	}
	if err := store.StoreSave(key, nil); err != nil {
		return jvm.IntValue(-1), nil
	}
	return jvm.IntValue(0), nil
}
