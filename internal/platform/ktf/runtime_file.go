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
	// seen is the count of writes under this path that data already holds;
	// see fileState. A restored File starts at zero and so keeps what the
	// load gave it until something is written after the load.
	seen uint64
	// truncated says the open itself asked for an empty file: mode 3, which
	// opens a name to rewrite it. Everything in data was written through this
	// object, which is what a quick load needs to know about it: it gives such
	// an object at most what it had written, so that the file the title is
	// rewriting is never followed by the tail of a newer one.
	//
	// An object whose open found nothing under the name is not one of these.
	// The title asked for the file; had there been one it would have kept it.
	// A load gives such an object the file as it is, like any other, so that
	// what was saved through it after the quick save is not cut off by the
	// restored title's next write.
	truncated bool
}

const maxGuestFileBytes = 4 << 20

// guestFileBatch is one guest DataOutputStream call's File writes, held until
// the call returns.
//
// A DataOutputStream writes every fixed-width value a byte at a time through
// the stream under it, and every File write stores the whole file: a locked,
// synced and renamed write per byte, so a save of a few hundred ints made a
// thousand of them and stalled the title for seconds. The writes one guest
// call makes are held in memory instead, where every reader already finds
// them, and stored together as the call returns — still before the guest's
// call returns, which is the point every write here is stored at. A store the
// call cannot make throws the IOException a refused write has always thrown,
// and leaves each file as it was before the call.
//
// The batch belongs to the guest thread whose call opened it. Any other
// platform call that thread makes before the batch closes — a title's own
// stream under the DataOutputStream can make one, and one can park the thread
// — stores what is held first, and so does the end of the thread's slice and a
// checkpoint capture (storeHeldGuestFiles), so nothing held outlives the
// guest's own call into the platform or is held at a boundary another thread
// runs at or a checkpoint is taken at.
type guestFileBatch struct {
	owner   *guestWorker
	pending []heldGuestFile
	failure error
}

// heldGuestFile is a file a batch holds writes for, with what it was before
// the first of them so a refused store can put it back.
type heldGuestFile struct {
	state        *runtimeGuestFile
	data         []byte
	position     int
	shared       []byte
	sharedExists bool
}

// enterGuestFileCall runs as a guest platform call enters. A runtime Java
// call into DataOutputStream opens a batch, and the function it answers closes
// it, storing what the call wrote and reporting a store that failed; any
// other call answers a function that does nothing.
func (runtime *initializationRuntime) enterGuestFileCall(category, id uint32) func() error {
	if batch := runtime.fileBatch; batch != nil {
		if batch.owner == runtime.client.activeWorker {
			runtime.storeGuestFileBatch(batch)
		}
		return func() error { return nil }
	}
	if category != svcCategoryRuntimeJava {
		return func() error { return nil }
	}
	if invocation, ok := runtime.nativeMethods[id]; !ok || invocation.method.class != "java/io/DataOutputStream" {
		return func() error { return nil }
	}
	batch := &guestFileBatch{owner: runtime.client.activeWorker}
	runtime.fileBatch = batch
	return func() error {
		runtime.storeGuestFileBatch(batch)
		runtime.fileBatch = nil
		return batch.failure
	}
}

// holdGuestFileWrite reports whether a write to this file is held by the
// batch the current guest thread has open, and starts holding the file if it
// is the first write to it.
func (runtime *initializationRuntime) holdGuestFileWrite(state *runtimeGuestFile) bool {
	batch := runtime.fileBatch
	if batch == nil || batch.owner != runtime.client.activeWorker {
		return false
	}
	for _, held := range batch.pending {
		if held.state == state {
			return true
		}
	}
	shared, exists := runtime.guestFiles[guestFileKey(state.name)]
	batch.pending = append(batch.pending, heldGuestFile{state: state, data: state.data, position: state.position, shared: shared, sharedExists: exists})
	return true
}

// storeGuestFileBatch stores every file the batch holds writes for. After a
// store fails, that file and every one after it is put back as it was before
// the batch's writes, which is what a refused write leaves. Putting a path
// back counts as a write to it, so another File that read the refused bytes
// reads the path again (fileState).
func (runtime *initializationRuntime) storeGuestFileBatch(batch *guestFileBatch) {
	for _, held := range batch.pending {
		if batch.failure == nil {
			if err := runtime.storeGuestFile(held.state.name, held.state.data); err != nil {
				batch.failure = err
			}
		}
		if batch.failure == nil {
			continue
		}
		key := guestFileKey(held.state.name)
		held.state.data, held.state.position = held.data, held.position
		if held.sharedExists {
			runtime.guestFiles[key] = held.shared
		} else {
			delete(runtime.guestFiles, key)
		}
		if runtime.guestFileWrites == nil {
			runtime.guestFileWrites = make(map[string]uint64)
		}
		runtime.guestFileWrites[key]++
		held.state.seen = runtime.guestFileWrites[key]
	}
	batch.pending = batch.pending[:0]
}

// storeHeldGuestFiles stores what an open batch holds, whichever thread
// opened it. A guest thread can stop inside a DataOutputStream call without
// making another platform call — its own stream's AOT write running out of
// its slice — and other threads run, and a checkpoint can be taken, while it
// is stopped; none of them may find bytes a File holds missing from the store.
// The batch stays open: the call goes on holding its writes when the thread
// resumes, and reports a store that failed here when it returns.
func (runtime *initializationRuntime) storeHeldGuestFiles() {
	if runtime == nil {
		return
	}
	if batch := runtime.fileBatch; batch != nil && len(batch.pending) > 0 {
		runtime.storeGuestFileBatch(batch)
	}
}

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
		for _, line := range splitRemovalList(data) {
			runtime.removedFiles[line] = true
		}
	}
	return runtime.removedFiles
}

// markGuestFileRemoved records or clears one path and writes the list back.
func (runtime *initializationRuntime) markGuestFileRemoved(name string, removed bool) error {
	return runtime.saveChanges(nil, guestFileRemovedKey, runtime.removedGuestFiles(), map[string]bool{strings.TrimPrefix(name, "/"): removed})
}

// storeGuestFile persists one guest file. Writing a path brings it back, so
// this is the one way a guest file reaches the store: a write that left the
// path on the removal list would be stored and then be unreadable.
func (runtime *initializationRuntime) storeGuestFile(name string, data []byte) error {
	trimmed := strings.TrimPrefix(name, "/")
	if reservedStorageName(guestFileScope, trimmed) {
		// The name this table keeps its own list under. Writing it would put
		// the file's bytes where the list belongs, and reading the list back
		// would mark whatever those bytes spell as deleted. Every caller
		// refuses the name before reaching here; this is the backstop, and it
		// is counted so a caller that does not is visible.
		runtime.countDiagnostic("fs reserved name " + trimmed)
		return fmt.Errorf("reserved file name %q", trimmed)
	}
	return runtime.saveChanges(map[string][]byte{"fs/" + trimmed: data}, guestFileRemovedKey, runtime.removedGuestFiles(), map[string]bool{trimmed: false})
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
		if reservedStorageName(guestFileScope, strings.TrimPrefix(name, "/")) {
			// The last guest-filesystem entry point that takes a name.
			// Unlinking this one writes the list's own name into the list, and
			// the claim that every way a name reaches a key is covered has to
			// be true rather than nearly true.
			return jvm.VoidValue(), newGuestIOException("cannot unlink a reserved name: " + name)
		}
		if err := runtime.markGuestFileRemoved(name, true); err != nil {
			return jvm.VoidValue(), newGuestIOException(err.Error())
		}
		delete(runtime.guestFiles, name)
		delete(runtime.guestFiles, strings.TrimPrefix(name, "/"))
	}
	return jvm.VoidValue(), nil
}

// fileState answers an open File's state brought up to what the session last
// wrote under its name, so a write through another File on the same path is
// what every later read through this one sees (the common save rule's first
// point). A File keeps its own copy only of a path nothing has written since
// it was opened, and of a path removed since: an open handle keeps reading
// what it had.
func (runtime *initializationRuntime) fileState(arguments []jvm.Value) (*jvm.Object, *runtimeGuestFile, error) {
	receiver, state, err := runtimeFileState(arguments)
	if err == nil && state != nil {
		key := guestFileKey(state.name)
		if writes := runtime.guestFileWrites[key]; writes > state.seen {
			if data, ok := runtime.guestFiles[key]; ok {
				state.data = data
			}
			state.seen = writes
		}
	}
	return receiver, state, err
}

// guestFileKey is the form guestFiles keys a path in: without its leading
// slash, which is how the store and the removal list name it too, so "/a" and
// "a" are one file here as they are there.
func guestFileKey(name string) string {
	return strings.TrimPrefix(name, "/")
}

// setGuestFile records what the session last wrote under a path. The slice is
// never written in place afterwards, so an open File may share it.
func (runtime *initializationRuntime) setGuestFile(name string, data []byte) {
	runtime.restoreGuestFile(name, data)
	if runtime.guestFileWrites == nil {
		runtime.guestFileWrites = make(map[string]uint64)
	}
	runtime.guestFileWrites[guestFileKey(name)]++
}

// restoreGuestFile records a path's content without counting it as a write,
// which is what a rebind from the store is: the Files a load restored keep
// what the load gave them.
func (runtime *initializationRuntime) restoreGuestFile(name string, data []byte) {
	if runtime.guestFiles == nil {
		runtime.guestFiles = make(map[string][]byte)
	}
	delete(runtime.guestFiles, name)
	runtime.guestFiles[guestFileKey(name)] = data
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
		// A title that caught the constructor's own IOException still has the
		// object it was constructing, and one local title goes on to call
		// through it. A handset answers that with the exception the method
		// declares rather than with a runtime error, and a title that catches
		// one catches the other.
		return nil, nil, newGuestIOException("File is not open")
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
	if reservedStorageName(guestFileScope, strings.TrimPrefix(name, "/")) {
		// The name this table keeps its removal list under. Refused where the
		// file is named rather than where it is written, so a title cannot
		// open it, write, and be told the write worked while nothing is
		// stored — the WIPI C table next door refuses the same collision.
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
	if mode == 3 || !exists {
		// A truncating open empties the file at the open, in the store as
		// well as for every later read, which is the common save rule (see
		// docs/architecture.md, "One save rule"). A title that opens to
		// rewrite and closes without writing therefore has an empty file, as
		// on a handset, and a store that refuses the emptying fails the open.
		// Opening a missing file for writing creates it the same way: an
		// empty file, there for the next open whatever the title writes.
		if err := runtime.storeGuestFile(name, []byte{}); err != nil {
			return jvm.VoidValue(), newGuestIOException(err.Error())
		}
		runtime.setGuestFile(name, []byte{})
	}
	state.truncated = mode == 3
	state.seen = runtime.guestFileWrites[guestFileKey(name)]
	receiver.Native = state
	return jvm.VoidValue(), nil
}

func newGuestIOException(message string) error {
	return &jvm.GuestException{
		Object:  &jvm.Object{ClassName: "java/io/IOException", Native: message},
		Message: message,
	}
}

func runtimeFileRead(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtime.fileState(arguments)
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
	if length == 0 {
		return jvm.IntValue(0), nil
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
	_, state, err := runtime.fileState(arguments)
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
	staged := make([]byte, max(end, len(state.data)))
	copy(staged, state.data)
	copy(staged[state.position:], data)
	// Inside a guest DataOutputStream call the store waits for the call to
	// return; see guestFileBatch.
	if !runtime.holdGuestFileWrite(state) {
		if err := runtime.storeGuestFile(state.name, staged); err != nil {
			return jvm.VoidValue(), newGuestIOException(err.Error())
		}
	}
	state.data, state.position = staged, end
	runtime.setGuestFile(state.name, staged)
	state.seen = runtime.guestFileWrites[guestFileKey(state.name)]
	return jvm.IntValue(int32(len(data))), nil
}

func runtimeFileSeek(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtime.fileState(arguments)
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

func runtimeFileTell(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtime.fileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(state.position)), nil
}

func runtimeFileClose(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, _, err := runtime.fileState(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), nil
}

func runtimeFileSize(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtime.fileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(len(state.data))), nil
}

func runtimeFileOpenStream(_ string) runtimeJavaImplementation {
	return func(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, state, err := runtime.fileState(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.ReferenceValue(&jvm.Object{ClassName: runtimeFileInputStreamClass, Native: state, Fields: make(map[string]jvm.Value)}), nil
	}
}

const runtimeFileInputStreamClass = "org/kwis/msp/io/FileInputStream"

func runtimeFileInputStreamClassDefinition() runtimeJavaClass {
	const class = runtimeFileInputStreamClass
	return runtimeJavaClass{name: class, superName: "java/io/InputStream", accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "read", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFileReadByte},
			{class: class, name: "read", descriptor: "([B)I", accessFlags: 0x0001, implementation: runtimeFileRead},
			{class: class, name: "read", descriptor: "([BII)I", accessFlags: 0x0001, implementation: runtimeFileRead},
			{class: class, name: "available", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFileStreamAvailable},
			{class: class, name: "close", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
		}}
}

func runtimeFileStreamAvailable(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtime.fileState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(max(0, len(state.data)-state.position))), nil
}

// Both stream directions share the File's bytes and cursor.
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

func runtimeFileOpenOutputStream(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtime.fileState(arguments)
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
func runtimeFileReadByte(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, state, err := runtime.fileState(arguments)
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
	if reservedStorageName(guestFileScope, strings.TrimPrefix(from, "/")) {
		// The source as well as the destination. Reading one of these names
		// resolves the platform's own list, so renaming it away would copy
		// the list's bytes into a file the title can then open — around the
		// refusal a File of that name already gets.
		return jvm.VoidValue(), newGuestIOException("cannot rename a reserved name: " + from)
	}
	if reservedStorageName(guestFileScope, strings.TrimPrefix(to, "/")) {
		// The name this table keeps its removal list under. The write would be
		// dropped and the source deleted, so the rename has to be refused
		// before either — a rename that destroys its source and stores nothing
		// is worse than one that fails.
		return jvm.VoidValue(), newGuestIOException("cannot rename onto a reserved name: " + to)
	}
	data, exists := runtime.guestFile(from)
	if runtime.saveReadError != nil {
		return jvm.VoidValue(), runtime.saveReadError
	}
	if !exists {
		return jvm.VoidValue(), newGuestIOException("file not found: " + from)
	}
	if strings.TrimPrefix(from, "/") == strings.TrimPrefix(to, "/") {
		return jvm.VoidValue(), nil
	}
	if err := runtime.saveChanges(map[string][]byte{"fs/" + strings.TrimPrefix(to, "/"): data}, guestFileRemovedKey, runtime.removedGuestFiles(), map[string]bool{strings.TrimPrefix(from, "/"): true, strings.TrimPrefix(to, "/"): false}); err != nil {
		return jvm.VoidValue(), newGuestIOException(err.Error())
	}
	runtime.setGuestFile(to, append([]byte(nil), data...))
	// The old name is deleted, which is the same problem unlink has: the save
	// underneath it outlives the rename unless the removal is written down.
	delete(runtime.guestFiles, from)
	delete(runtime.guestFiles, strings.TrimPrefix(from, "/"))
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
