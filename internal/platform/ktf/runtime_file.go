package ktf

import (
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// runtimeGuestFile is the state behind one org/kwis/msp/io/File object. Reads
// see archive resources first and writes live in the client's in-memory file
// table; durable persistence joins with the Host save path.
type runtimeGuestFile struct {
	name     string
	data     []byte
	position int
}

const maxGuestFileBytes = 4 << 20

func runtimeFileClassDefinition() runtimeJavaClass {
	const class = "org/kwis/msp/io/File"
	return runtimeJavaClass{
		name:        class,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(Ljava/lang/String;I)V", accessFlags: 0x0001, implementation: runtimeFileConstructor},
			{class: class, name: "<init>", descriptor: "(Ljava/lang/String;II)V", accessFlags: 0x0001, implementation: runtimeFileConstructor},
			{class: class, name: "write", descriptor: "([B)I", accessFlags: 0x0001, implementation: runtimeFileWrite},
			{class: class, name: "write", descriptor: "([BII)I", accessFlags: 0x0001, implementation: runtimeFileWrite},
			{class: class, name: "read", descriptor: "([B)I", accessFlags: 0x0001, implementation: runtimeFileRead},
			{class: class, name: "read", descriptor: "([BII)I", accessFlags: 0x0001, implementation: runtimeFileRead},
			{class: class, name: "seek", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeFileSeek},
			{class: class, name: "tell", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFileTell},
			{class: class, name: "close", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeFileClose},
			{class: class, name: "sizeOf", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFileSize},
			{class: class, name: "openInputStream", descriptor: "()Ljava/io/InputStream;", accessFlags: 0x0001, implementation: runtimeFileOpenStream("java/io/ByteArrayInputStream")},
			{class: class, name: "openDataInputStream", descriptor: "()Ljava/io/DataInputStream;", accessFlags: 0x0001, implementation: runtimeFileOpenDataStream},
			{class: class, name: "openOutputStream", descriptor: "()Ljava/io/OutputStream;", accessFlags: 0x0001, implementation: runtimeFileOpenOutputStream},
			{class: class, name: "openDataOutputStream", descriptor: "()Ljava/io/DataOutputStream;", accessFlags: 0x0001, implementation: runtimeFileOpenDataOutputStream},
			{class: class, name: "read", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFileReadByte},
			{class: class, name: "write", descriptor: "(I)I", accessFlags: 0x0001, implementation: runtimeFileWriteByte},
		},
	}
}

func runtimeFileSystemClassDefinition() runtimeJavaClass {
	const class = "org/kwis/msp/io/FileSystem"
	return runtimeJavaClass{
		name:        class,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "exists", descriptor: "(Ljava/lang/String;)Z", accessFlags: 0x0009, implementation: runtimeFileSystemExists},
			{class: class, name: "exists", descriptor: "(Ljava/lang/String;I)Z", accessFlags: 0x0009, implementation: runtimeFileSystemExists},
			{class: class, name: "isFile", descriptor: "(Ljava/lang/String;)Z", accessFlags: 0x0009, implementation: runtimeFileSystemExists},
			{class: class, name: "isDirectory", descriptor: "(Ljava/lang/String;I)Z", accessFlags: 0x0009, implementation: runtimeComponentZero},
			{class: class, name: "mkdir", descriptor: "(Ljava/lang/String;I)V", accessFlags: 0x0009, implementation: runtimeComponentNoop},
			{class: class, name: "available", descriptor: "()I", accessFlags: 0x0009, implementation: runtimeFileSystemAvailable},
			{class: class, name: "getMaxFilenameLength", descriptor: "()I", accessFlags: 0x0009, implementation: runtimeFileSystemNameLength},
			{class: class, name: "unlink", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0009, implementation: runtimeFileSystemUnlink},
			{class: class, name: "isFile", descriptor: "(Ljava/lang/String;I)Z", accessFlags: 0x0009, implementation: runtimeFileSystemExists},
			// The guest filesystem is a flat name table, so nothing in it is a
			// directory and creating or removing one is accepted and ignored.
			{class: class, name: "isDirectory", descriptor: "(Ljava/lang/String;)Z", accessFlags: 0x0009, implementation: runtimeComponentZero},
			{class: class, name: "mkdir", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0009, implementation: runtimeComponentNoop},
			{class: class, name: "rmdir", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0009, implementation: runtimeComponentNoop},
			{class: class, name: "rmdir", descriptor: "(Ljava/lang/String;I)V", accessFlags: 0x0009, implementation: runtimeComponentNoop},
			{class: class, name: "remove", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0009, implementation: runtimeFileSystemUnlink},
			{class: class, name: "remove", descriptor: "(Ljava/lang/String;I)V", accessFlags: 0x0009, implementation: runtimeFileSystemUnlink},
			{class: class, name: "rename", descriptor: "(Ljava/lang/String;Ljava/lang/String;)V", accessFlags: 0x0009, implementation: runtimeFileSystemRename},
			{class: class, name: "rename", descriptor: "(Ljava/lang/String;Ljava/lang/String;I)V", accessFlags: 0x0009, implementation: runtimeFileSystemRename},
			{class: class, name: "list", descriptor: "(Ljava/lang/String;)Ljava/util/Vector;", accessFlags: 0x0009, implementation: runtimeFileSystemList},
			{class: class, name: "list", descriptor: "(Ljava/lang/String;I)Ljava/util/Vector;", accessFlags: 0x0009, implementation: runtimeFileSystemList},
			{class: class, name: "toCString", descriptor: "(Ljava/lang/String;)[B", accessFlags: 0x0009, implementation: runtimeFileSystemCString},
			// Creation times are not kept: the guest filesystem is composed
			// from archive entries and in-memory writes, neither of which
			// carries one.
			{class: class, name: "getCreationTime", descriptor: "(Ljava/lang/String;)I", accessFlags: 0x0009, implementation: runtimeComponentZero},
			{class: class, name: "getCreationTime", descriptor: "(Ljava/lang/String;I)I", accessFlags: 0x0009, implementation: runtimeComponentZero},
		},
	}
}

// guestFileRemovedKey lists the paths unlink has deleted.
//
// Deleting has to be written down, because the two layers under the writable
// table cannot be deleted from: the save boundary has no delete, and the
// mounted archive is the game's own package. Dropping the in-memory entry and
// nothing else leaves the file resolving from the save underneath it, so a
// title that deletes a file and then asks whether it is there is told yes.
//
// The sibling platform lost two titles' entire opening sequences to exactly
// that: each deleted its save slot to start a new game, asked whether a save
// was there, was told yes, and skipped everything a new game begins with. No
// title here has been seen to delete and re-ask, so this is a correction made
// from the other platform's evidence rather than from a failure of its own.
const guestFileRemovedKey = "fs/.removed"

// guestFile resolves a File path against in-memory writes first, persisted
// save writes second, and the mounted outer-archive filesystem last. Guest
// names may carry a leading slash that the mounted names do not. A removed
// path resolves to none of the three.
func (runtime *initializationRuntime) guestFile(name string) ([]byte, bool) {
	if runtime.removedGuestFiles()[strings.TrimPrefix(name, "/")] {
		return nil, false
	}
	for _, candidate := range []string{name, strings.TrimPrefix(name, "/")} {
		if data, exists := runtime.guestFiles[candidate]; exists {
			return data, true
		}
	}
	if data, exists := runtime.loadSave("fs/" + strings.TrimPrefix(name, "/")); exists {
		return data, true
	}
	for _, candidate := range []string{name, strings.TrimPrefix(name, "/")} {
		if data, exists := runtime.client.files[candidate]; exists {
			return data, true
		}
	}
	return nil, false
}

// removedGuestFiles is the set of deleted paths, read from the store once per
// session and kept in memory after that. Names are kept without their leading
// slash, which is the form the resolution above compares.
func (runtime *initializationRuntime) removedGuestFiles() map[string]bool {
	if runtime.removedFiles != nil {
		return runtime.removedFiles
	}
	runtime.removedFiles = make(map[string]bool)
	if data, exists := runtime.loadSave(guestFileRemovedKey); exists {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				runtime.removedFiles[line] = true
			}
		}
	}
	return runtime.removedFiles
}

// markGuestFileRemoved records or clears one path and writes the list back.
func (runtime *initializationRuntime) markGuestFileRemoved(name string, removed bool) {
	set := runtime.removedGuestFiles()
	key := strings.TrimPrefix(name, "/")
	if set[key] == removed {
		return
	}
	if removed {
		set[key] = true
	} else {
		delete(set, key)
	}
	names := make([]string, 0, len(set))
	for entry := range set {
		names = append(names, entry)
	}
	sort.Strings(names)
	runtime.storeSave(guestFileRemovedKey, []byte(strings.Join(names, "\n")))
}

// storeGuestFile persists one guest file. Writing a path brings it back, so
// this is the one way a guest file reaches the store: a write that left the
// path on the removal list would be stored and then be unreadable.
func (runtime *initializationRuntime) storeGuestFile(name string, data []byte) {
	trimmed := strings.TrimPrefix(name, "/")
	runtime.markGuestFileRemoved(trimmed, false)
	runtime.storeSave("fs/"+trimmed, data)
}

func runtimeFileSystemExists(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 1 {
		return jvm.VoidValue(), fmt.Errorf("FileSystem.exists expected name")
	}
	nameObject, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, ok := jvm.StringText(nameObject)
	if !ok {
		return jvm.IntValue(0), nil
	}
	_, exists := runtime.guestFile(name)
	// The name and the answer both belong in the trace. A start-up gate is
	// often a file test, and a test that appears in the trace without its
	// path says only that the title asked something.
	runtime.countDiagnostic(fmt.Sprintf("exists %s found=%t", name, exists))
	if exists {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func runtimeFileSystemAvailable(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(maxGuestFileBytes), nil
}

func runtimeFileSystemNameLength(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(255), nil
}

func runtimeFileSystemUnlink(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 1 {
		return jvm.VoidValue(), fmt.Errorf("FileSystem.unlink expected name")
	}
	nameObject, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if name, ok := jvm.StringText(nameObject); ok {
		delete(runtime.guestFiles, name)
		delete(runtime.guestFiles, strings.TrimPrefix(name, "/"))
		runtime.markGuestFileRemoved(name, true)
	}
	return jvm.VoidValue(), nil
}

func runtimeFileState(arguments []jvm.Value) (*jvm.Object, *runtimeGuestFile, error) {
	if len(arguments) < 1 {
		return nil, nil, fmt.Errorf("File method expected receiver")
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return nil, nil, err
	}
	if receiver == nil {
		return nil, nil, fmt.Errorf("File receiver is null")
	}
	state, ok := receiver.Native.(*runtimeGuestFile)
	if !ok {
		return nil, nil, fmt.Errorf("File receiver is not open")
	}
	return receiver, state, nil
}

func runtimeFileConstructor(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 3 {
		return jvm.VoidValue(), fmt.Errorf("File constructor expected receiver, name, and mode")
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("File constructor receiver is null")
	}
	nameObject, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, ok := jvm.StringText(nameObject)
	if !ok {
		return jvm.VoidValue(), fmt.Errorf("File name is not a string")
	}
	mode, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.countDiagnostic(fmt.Sprintf("file %s mode %d", name, mode))
	if name == "" || mode < 1 || mode > 4 {
		return jvm.VoidValue(), newGuestIOException("Invalid file open")
	}
	state := &runtimeGuestFile{name: name}
	data, exists := runtime.guestFile(name)
	// Mode 1 is read-only: a missing file is an open error, matching the
	// original runtime, so first-boot save loads take their fallback path.
	if !exists && mode == 1 {
		return jvm.VoidValue(), newGuestIOException("File not found: " + name)
	}
	if exists && mode != 3 {
		state.data = append([]byte(nil), data...)
	}
	receiver.Native = state
	return jvm.VoidValue(), nil
}

func newGuestIOException(message string) error {
	return &jvm.GuestException{
		Object:  &jvm.Object{ClassName: "java/io/IOException", Native: message},
		Message: message,
	}
}

func runtimeFileRead(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtimeFileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("File.read expected buffer")
	}
	buffer, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if buffer == nil {
		return jvm.VoidValue(), fmt.Errorf("File.read buffer is null")
	}
	component, values, err := jvm.ArraySnapshot(buffer)
	if err != nil || component.Kind != jvm.TypeByte {
		return jvm.VoidValue(), fmt.Errorf("File.read buffer is not byte[]: %v", err)
	}
	offset, length := 0, len(values)
	if len(arguments) >= 4 {
		offsetValue, offsetErr := arguments[2].Int32()
		if offsetErr != nil {
			return jvm.VoidValue(), offsetErr
		}
		lengthValue, lengthErr := arguments[3].Int32()
		if lengthErr != nil {
			return jvm.VoidValue(), lengthErr
		}
		offset, length = int(offsetValue), int(lengthValue)
	}
	if offset < 0 || length < 0 || offset+length > len(values) {
		return jvm.VoidValue(), fmt.Errorf("File.read range [%d, %d) is out of bounds", offset, offset+length)
	}
	remaining := len(state.data) - state.position
	if remaining <= 0 {
		return jvm.IntValue(-1), nil
	}
	if length > remaining {
		length = remaining
	}
	chunk := make([]jvm.Value, length)
	for index := 0; index < length; index++ {
		chunk[index] = jvm.IntValue(int32(int8(state.data[state.position+index])))
	}
	if err := jvm.SetArrayRange(buffer, offset, chunk); err != nil {
		return jvm.VoidValue(), err
	}
	state.position += length
	return jvm.IntValue(int32(length)), nil
}

func runtimeFileWrite(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtimeFileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("File.write expected buffer")
	}
	buffer, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if buffer == nil {
		return jvm.VoidValue(), fmt.Errorf("File.write buffer is null")
	}
	data, err := jvm.ByteArraySnapshot(buffer)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) >= 4 {
		offsetValue, offsetErr := arguments[2].Int32()
		if offsetErr != nil {
			return jvm.VoidValue(), offsetErr
		}
		lengthValue, lengthErr := arguments[3].Int32()
		if lengthErr != nil {
			return jvm.VoidValue(), lengthErr
		}
		offset, length := int(offsetValue), int(lengthValue)
		if offset < 0 || length < 0 || offset+length > len(data) {
			return jvm.VoidValue(), fmt.Errorf("File.write range [%d, %d) is out of bounds", offset, offset+length)
		}
		data = data[offset : offset+length]
	}
	end := state.position + len(data)
	if end > maxGuestFileBytes {
		return jvm.VoidValue(), fmt.Errorf("File %q exceeds size limit", state.name)
	}
	if end > len(state.data) {
		grown := make([]byte, end)
		copy(grown, state.data)
		state.data = grown
	}
	copy(state.data[state.position:], data)
	state.position = end
	if runtime.guestFiles == nil {
		runtime.guestFiles = make(map[string][]byte)
	}
	runtime.guestFiles[state.name] = append([]byte(nil), state.data...)
	runtime.storeGuestFile(state.name, state.data)
	return jvm.IntValue(int32(len(data))), nil
}

func runtimeFileSeek(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtimeFileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("File.seek expected position")
	}
	position, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if position < 0 {
		position = 0
	}
	if int(position) > len(state.data) {
		position = int32(len(state.data))
	}
	state.position = int(position)
	return jvm.VoidValue(), nil
}

func runtimeFileTell(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtimeFileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(state.position)), nil
}

func runtimeFileClose(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, _, err := runtimeFileState(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), nil
}

func runtimeFileSize(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtimeFileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(len(state.data))), nil
}

func runtimeFileOpenStream(class string) runtimeJavaImplementation {
	return func(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, state, err := runtimeFileState(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		array := jvm.NewByteArray(append([]byte(nil), state.data...))
		stream, err := vm.NewObject(class, "([B)V", jvm.ReferenceValue(array))
		if err != nil {
			return jvm.VoidValue(), fmt.Errorf("open File stream for %q: %w", state.name, err)
		}
		return jvm.ReferenceValue(stream), nil
	}
}

// runtimeFileOutputStreamClass is the write side of a File, handed out as the
// java/io/OutputStream a title asks for.
//
// The read side can be a snapshot — a ByteArrayInputStream over the bytes as
// they are now — because reading cannot change them. Writing cannot: a stream
// that collected bytes of its own would only reach the file if something
// copied them back, and a title that closes the stream rather than the file
// would have written its save into nothing. So the stream is the file: it
// carries the same open-file state and every write goes straight through it,
// persisted the same way File.write is.
const runtimeFileOutputStreamClass = "org/kwis/msp/io/FileOutputStream"

func runtimeFileOutputStreamClassDefinition() runtimeJavaClass {
	const class = runtimeFileOutputStreamClass
	return runtimeJavaClass{
		name:        class,
		superName:   "java/io/OutputStream",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "write", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeFileStreamWrite(runtimeFileWriteByte)},
			{class: class, name: "write", descriptor: "([B)V", accessFlags: 0x0001, implementation: runtimeFileStreamWrite(runtimeFileWrite)},
			{class: class, name: "write", descriptor: "([BII)V", accessFlags: 0x0001, implementation: runtimeFileStreamWrite(runtimeFileWrite)},
			// Every write is already through to the file, so there is nothing
			// held back for a flush or a close to release.
			{class: class, name: "flush", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "close", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
		},
	}
}

// runtimeFileStreamWrite adapts a File write, which answers how many bytes it
// took, to the stream method of the same name, which answers nothing.
func runtimeFileStreamWrite(write runtimeJavaImplementation) runtimeJavaImplementation {
	return func(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		if _, err := write(runtime, vm, arguments); err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), nil
	}
}

func runtimeFileOpenOutputStream(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtimeFileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(&jvm.Object{ClassName: runtimeFileOutputStreamClass, Native: state, Fields: make(map[string]jvm.Value)}), nil
}

func runtimeFileOpenDataOutputStream(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	inner, err := runtimeFileOpenOutputStream(runtime, vm, arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream, err := vm.NewObject("java/io/DataOutputStream", "(Ljava/io/OutputStream;)V", inner)
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("open File data output stream: %w", err)
	}
	return jvm.ReferenceValue(stream), nil
}

func runtimeFileOpenDataStream(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	inner, err := runtimeFileOpenStream("java/io/ByteArrayInputStream")(runtime, vm, arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream, err := vm.NewObject("java/io/DataInputStream", "(Ljava/io/InputStream;)V", inner)
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("open File data stream: %w", err)
	}
	return jvm.ReferenceValue(stream), nil
}

// runtimeFileReadByte reads one byte at the file position, answering -1 at the
// end of the file like the stream API it mirrors.
func runtimeFileReadByte(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtimeFileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if state.position < 0 || state.position >= len(state.data) {
		return jvm.IntValue(-1), nil
	}
	value := state.data[state.position]
	state.position++
	return jvm.IntValue(int32(value)), nil
}

// runtimeFileWriteByte appends or overwrites one byte at the file position and
// reports how many bytes it wrote.
func runtimeFileWriteByte(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("File.write expected receiver and byte, got %d arguments", len(arguments))
	}
	value, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	array := jvm.NewByteArray([]byte{byte(value)})
	return runtimeFileWrite(runtime, vm, []jvm.Value{arguments[0], jvm.ReferenceValue(array)})
}

// runtimeFileSystemRename moves in-memory content and its persisted save from
// one name to the other. Archive entries are read-only, so a rename of one
// copies it into the writable table under the new name.
func runtimeFileSystemRename(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("FileSystem.rename expected two names")
	}
	from, err := runtimeFileSystemName(arguments[0])
	if err != nil {
		return jvm.VoidValue(), err
	}
	to, err := runtimeFileSystemName(arguments[1])
	if err != nil {
		return jvm.VoidValue(), err
	}
	data, exists := runtime.guestFile(from)
	if !exists {
		return jvm.VoidValue(), newGuestIOException("file not found: " + from)
	}
	if runtime.guestFiles == nil {
		runtime.guestFiles = make(map[string][]byte)
	}
	runtime.guestFiles[strings.TrimPrefix(to, "/")] = append([]byte(nil), data...)
	runtime.storeGuestFile(to, data)
	// The old name is deleted, which is the same problem unlink has: the save
	// underneath it outlives the rename unless the removal is written down.
	delete(runtime.guestFiles, from)
	delete(runtime.guestFiles, strings.TrimPrefix(from, "/"))
	runtime.markGuestFileRemoved(from, true)
	return jvm.VoidValue(), nil
}

// runtimeFileSystemList answers the files under a directory prefix as a
// Vector of names, which is the container the original API returns.
func runtimeFileSystemList(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 1 {
		return jvm.VoidValue(), fmt.Errorf("FileSystem.list expected a directory name")
	}
	prefix, err := runtimeFileSystemName(arguments[0])
	if err != nil {
		return jvm.VoidValue(), err
	}
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	names := make(map[string]bool)
	removed := runtime.removedGuestFiles()
	for _, table := range []map[string][]byte{runtime.client.files, runtime.guestFiles} {
		for name := range table {
			if !strings.HasPrefix(name, prefix) || removed[strings.TrimPrefix(name, "/")] {
				continue
			}
			names[name] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	vector, err := vm.NewObject("java/util/Vector", "()V")
	if err != nil {
		return jvm.VoidValue(), err
	}
	for _, name := range sorted {
		if _, err := vm.InvokeVirtual(vector, "addElement", "(Ljava/lang/Object;)V", jvm.ReferenceValue(vm.NewString(name))); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.ReferenceValue(vector), nil
}

// runtimeFileSystemCString encodes a name in the platform charset with the
// terminator native code expects.
func runtimeFileSystemCString(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 1 {
		return jvm.VoidValue(), fmt.Errorf("FileSystem.toCString expected a name")
	}
	name, err := runtimeFileSystemName(arguments[0])
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(jvm.NewByteArray(append(encodeEUCKR(name), 0))), nil
}

func runtimeFileSystemName(value jvm.Value) (string, error) {
	object, err := value.Reference()
	if err != nil {
		return "", err
	}
	name, ok := jvm.StringText(object)
	if !ok {
		return "", fmt.Errorf("FileSystem name is not a string")
	}
	return name, nil
}
