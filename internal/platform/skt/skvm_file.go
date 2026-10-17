package skt

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// SKVM file modes from com.xce.io.XFile.
const (
	xFileRead  int32 = 1
	xFileWrite int32 = 2
)

// WIPI file modes from org.kwis.msp.io.File. They are an enumeration rather
// than the XFile bits above: WRITE starts writing at the end of an existing
// file, WRITE_TRUNC empties it on open, and only READ_WRITE does both. A title
// compiles these numbers in, so they are what an open has to understand.
const (
	wipiFileReadOnly   int32 = 1
	wipiFileWrite      int32 = 2
	wipiFileWriteTrunc int32 = 3
	wipiFileReadWrite  int32 = 4
)

// xFileMaxBytes bounds how far a write may grow a file. xFileCapacity is what
// a title is told it has; this is the hard stop behind it, so a seek far past
// the end followed by a write cannot make the shared process allocate up to
// two gigabytes. A handset gave an application a few megabytes of flash at
// most, so no title wrote a file this large; a file already larger (a
// packaged resource) can still be rewritten in place.
const xFileMaxBytes = 16 << 20

// XFile save keys live under "fs/", the same scope KTF's guest filesystem
// uses, so one owner directory holds a title's files whichever platform wrote
// them. A file the game never wrote falls back to the JAR resource of the
// same name, which is how a game reads data it shipped.
const xFileSaveScope = "fs/"

// xFileRemovedKey lists the paths a title has removed, one per line.
//
// The save boundary has no delete, and a path nothing has written falls back
// to the archive's packaged file, so a removal has to be written down rather
// than represented by an empty file: an emptied file is a file that exists,
// and a title that deletes its slot to start a new game and then asks
// whether a save is there is told yes. The key and the form are the ones the
// KTF guest filesystem's list has — names joined by newlines and read back
// exactly as written — and a name here is the path of the save key below the
// scope, so every spelling of one path removes the one file. A removal also
// stores the emptied file, in the same batch as the list, so a reader that
// does not know the list finds an empty file rather than the old bytes.
const xFileRemovedKey = xFileSaveScope + ".removed"

// xFileCapacity is what fsavail reports against. There is no partition here,
// so a fixed budget is the honest answer, and it is the same one RMS uses.
const xFileCapacity = rmsCapacity

func (runtime *Runtime) xFileRegistrations() []nativeRegistration {
	const text = "Ljava/lang/String;"
	return []nativeRegistration{
		{skvm.XFileClass, "initHandle", "(I)V", runtime.initXFileHandle},
		{skvm.XFileClass, "initName", "(" + text + "I)V", runtime.initXFileName},
		{skvm.XFileClass, "initArchive", "(" + text + text + ")V", runtime.initXFileArchive},
		{skvm.XFileClass, "truncate", "()V", runtime.xFileTruncate},
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

// xFileReservedNames are the paths the platform keeps for itself: the removal
// list above. A title writing that path would put its own bytes where the list
// belongs, and the next session would read them back as a set of removed
// names; a directory of that name would leave nowhere to write the list. So
// the name is refused as a path and as a path's first component.
var xFileReservedNames = map[string]bool{".removed": true}

// xFileKey normalizes a guest path into the save key it lives under.
func xFileKey(name string) (string, error) {
	key, err := backend.NormalizeSaveKey(xFileSaveScope + strings.TrimPrefix(name, "/"))
	if err != nil {
		return "", newGuestException("java/io/IOException", err.Error())
	}
	rest, under := strings.CutPrefix(key, xFileSaveScope)
	first, _, _ := strings.Cut(rest, "/")
	if !under || rest == "" || xFileReservedNames[first] {
		return "", newGuestException("java/io/IOException", "reserved path "+name)
	}
	return key, nil
}

// xFileData is one open of a file: an XFile, a WIPI File or the input stream
// a File opened. What the handle owns is its position and the way it was
// opened; the bytes are the path's, in node, which every handle on that path
// shares. fileMu guards all of it.
type xFileData struct {
	name         string
	archiveName  string
	archiveEntry string
	mode         int32
	cursor       int
	open         bool
	writable     bool
	// node is the file the handle reads and writes: the path's shared one, or
	// for an entry of an installed archive a read-only one of its own. A raw
	// handle has none.
	node *xFileNode
}

// xFileNode is one path's file for the session. Every handle open on the path
// reads and writes these bytes, and exists, filesize and every later open
// answer from them, so a write or a truncation is seen at once by every read
// of the path. Storing them is a separate step: the title's flush or close,
// a truncating open, a removal, a quick save, and the end of the session —
// and nothing else. See docs/skvm.md, "Saves and sound".
type xFileNode struct {
	// key is the save key. An archive entry's node has none and is never
	// stored.
	key  string
	data []byte
	// borrowed says data is the archive's own packaged bytes, which a write
	// copies before changing them.
	borrowed bool
	// present says the file exists. A removed path does not, and neither
	// does one nothing has made: a writable open makes it, empty, and so does
	// a write through a handle left open on a path removed since.
	present bool
	// stored and packaged say what a later session would find without the
	// removal list: an entry under the key, or a packaged file of the name.
	stored, packaged bool
	// dirty says a later session would read something other than this: a
	// write not stored yet, or a removal not stored yet.
	dirty bool
}

// own makes data the node's own before a write changes it in place.
func (node *xFileNode) own() {
	if node.borrowed {
		node.data = slices.Clone(node.data)
		node.borrowed = false
	}
}

// xFileNodeLocked answers the one file a save key names in this session. The
// first time a key is named it is read: the removal list, then the save, then
// the archive's packaged file of the same name. The caller holds fileMu.
func (runtime *Runtime) xFileNodeLocked(key string) (*xFileNode, error) {
	return runtime.xFileNodeKeptLocked(key, true)
}

// xFilePeekLocked is xFileNodeLocked for a caller that only looks: a path that
// is not there is answered without being kept, so a title that probes
// generated names — "slot0" to "slotN", or a timestamp — does not grow the
// session's table by a node for every name it ever asked about. A path that
// is there is kept, as it would be by any open. The caller holds fileMu.
func (runtime *Runtime) xFilePeekLocked(key string) (*xFileNode, error) {
	return runtime.xFileNodeKeptLocked(key, false)
}

// xFileNodeKeptLocked reads a key's node, keeping it when keep is set or when
// the path is there.
func (runtime *Runtime) xFileNodeKeptLocked(key string, keep bool) (*xFileNode, error) {
	if node := runtime.files[key]; node != nil {
		return node, nil
	}
	removed, err := runtime.xFileRemovalsLocked()
	if err != nil {
		return nil, err
	}
	name := strings.TrimPrefix(key, xFileSaveScope)
	node := &xFileNode{key: key}
	packaged, isPackaged := runtime.packagedFile(name)
	node.packaged = isPackaged
	if !removed[name] {
		data, stored, err := backend.ReadSave(runtime.saveStoreBoundary(), key)
		if err != nil {
			return nil, newGuestException("java/io/IOException", err.Error())
		}
		node.stored = stored
		switch {
		case stored:
			node.data, node.present = slices.Clone(data), true
		case isPackaged:
			node.data, node.borrowed, node.present = packaged, true, true
		}
	}
	if !keep && !node.present {
		return node, nil
	}
	if runtime.files == nil {
		runtime.files = make(map[string]*xFileNode)
	}
	runtime.files[key] = node
	return node, nil
}

// packagedFile is the archive's own file of a path, which is how a title
// reads data it shipped. The path is the one the save key names, so every
// spelling of one path finds the same file.
func (runtime *Runtime) packagedFile(name string) ([]byte, bool) {
	if runtime.Archive == nil {
		return nil, false
	}
	return runtime.Archive.Resource(name)
}

// xFileRemovalsLocked is the removal list as the store holds it, read once
// per session with the first path the session names. The caller holds fileMu.
func (runtime *Runtime) xFileRemovalsLocked() (map[string]bool, error) {
	if runtime.removedFiles != nil {
		return runtime.removedFiles, nil
	}
	data, found, err := backend.ReadSave(runtime.saveStoreBoundary(), xFileRemovedKey)
	if err != nil {
		return nil, newGuestException("java/io/IOException", err.Error())
	}
	removed := make(map[string]bool)
	if found {
		for _, name := range splitRemovalList(data) {
			removed[name] = true
		}
	}
	runtime.removedFiles = removed
	return removed, nil
}

// joinRemovalList writes a removal list: the names sorted and joined by
// newlines, read back exactly as written. A name a newline would split is
// refused before it gets here (removeXFile).
func joinRemovalList(names map[string]bool) []byte {
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	return []byte(strings.Join(sorted, "\n"))
}

// splitRemovalList reads one back, dropping only the empty line an empty list
// leaves behind.
func splitRemovalList(data []byte) []string {
	lines := strings.Split(string(data), "\n")
	names := make([]string, 0, len(lines))
	for _, name := range lines {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// storeXFileLocked writes one path the way a later session has to find it:
// its bytes when it exists, or an emptied file and its name on the removal
// list when it was removed. The list changes in the same batch as the file it
// is about — a write takes the name off, a removal puts it on — so a later
// session never reads one without the other. A refused write leaves the path
// dirty, and the next flush, close, quick save or the end of the session
// writes it again. Without a store there is nothing to write to, and the file
// stays what the session holds. The caller holds fileMu.
func (runtime *Runtime) storeXFileLocked(node *xFileNode) error {
	if node == nil || !node.dirty || node.key == "" {
		return nil
	}
	store := runtime.saveStoreBoundary()
	if store == nil {
		return nil
	}
	removed, err := runtime.xFileRemovalsLocked()
	if err != nil {
		return err
	}
	name := strings.TrimPrefix(node.key, xFileSaveScope)
	entries := make(map[string][]byte, 2)
	var staged map[string]bool
	if node.present {
		entries[node.key] = slices.Clone(node.data)
		if removed[name] {
			staged = maps.Clone(removed)
			delete(staged, name)
		}
	} else {
		entries[node.key] = nil
		if !removed[name] {
			staged = maps.Clone(removed)
			staged[name] = true
		}
	}
	if staged != nil {
		entries[xFileRemovedKey] = joinRemovalList(staged)
	}
	// Every write through the save boundary takes this turn, so a file and a
	// record store never reach the Host at once.
	runtime.savePendingMu.Lock()
	err = backend.StoreSaves(store, entries)
	if err == nil {
		for key := range entries {
			delete(runtime.pendingSaves, key)
		}
	}
	runtime.savePendingMu.Unlock()
	if err != nil {
		return err
	}
	if staged != nil {
		runtime.removedFiles = staged
	}
	node.dirty, node.stored = false, true
	return nil
}

// truncateXFileLocked empties a file for a truncating open and stores the
// empty file at once: the open itself is the write. The file still exists,
// empty, rather than being removed and made again. The caller holds fileMu.
func (runtime *Runtime) truncateXFileLocked(node *xFileNode) {
	node.data, node.borrowed, node.present, node.dirty = nil, false, true, true
	if err := runtime.storeXFileLocked(node); err != nil && runtime.logger != nil {
		runtime.logger.Debug("XFile truncation store failed", "key", node.key, "error", err)
	}
}

// dirtyFileKeysLocked lists the paths whose state is not stored yet, in key
// order. The caller holds fileMu.
func (runtime *Runtime) dirtyFileKeysLocked() []string {
	var keys []string
	for key, node := range runtime.files {
		if node.dirty {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
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
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	node, err := runtime.xFileNodeLocked(key)
	if err != nil {
		return jvm.VoidValue(), err
	}
	// A writable open of a missing file creates it. There is no separate create
	// bit in this API — the original runtime opens a writable file the way
	// RandomAccessFile's "rw" does, and a save is the case that proves it: one
	// title writes its three slots by opening `/k`, `/s` and `/w` for writing
	// and nothing ever creates them first, so a missing file refused here comes
	// back as the title's own `실패하였습니다. 저장 공간이 부족합니다.` — the
	// message its catch block shows for an IOException out of the save. The
	// open makes an empty file that every later read sees at once, and the
	// handle's flush or close, or the end of the session, stores it; a removed
	// name made again this way comes off the removal list in the same batch.
	// A read-only open of a file that is not there still fails, because that
	// is a title asking for something and being told it does not exist.
	writable := mode&xFileWrite != 0
	if !node.present {
		if !writable {
			return jvm.VoidValue(), newGuestException("java/io/IOException", "no such file: "+name)
		}
		node.data, node.borrowed, node.present, node.dirty = nil, false, true, true
	}
	receiver.Native = &xFileData{name: name, mode: mode, open: true, writable: writable, node: node}
	return jvm.VoidValue(), nil
}

// initWIPIFileName is org.kwis.msp.io.File(String, int[, int]): the same
// handle as XFile once the mode is translated to its bits. The third argument
// is a sharing level, and one title has nobody to share a file with.
func (runtime *Runtime) initWIPIFileName(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	mode, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	var bits int32
	switch mode {
	case wipiFileReadOnly:
		bits = xFileRead
	case wipiFileWrite, wipiFileWriteTrunc:
		bits = xFileWrite
	case wipiFileReadWrite:
		bits = xFileRead | xFileWrite
	default:
		return jvm.VoidValue(), newGuestException("java/io/IOException", fmt.Sprintf("invalid file mode %d", mode))
	}
	translated := append([]jvm.Value(nil), arguments[:3]...)
	translated[2] = jvm.IntValue(bits)
	if _, err := runtime.initXFileName(vm, translated); err != nil {
		return jvm.VoidValue(), err
	}
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	switch mode {
	case wipiFileWrite:
		file.cursor = len(file.node.data)
	case wipiFileWriteTrunc:
		// The open is what empties the file, in the store as well, so a close
		// with nothing written — or no close at all — leaves it empty.
		runtime.truncateXFileLocked(file.node)
	}
	return jvm.VoidValue(), nil
}

// xFileTruncate is the private half of com.xce.io.FileOutputStream(String),
// which starts its file over the way java.io's constructor does. XFile's own
// modes never truncate, so the stream opens the file for writing and then
// empties it here, at the open; the file stays a file rather than being
// removed and made again.
func (runtime *Runtime) xFileTruncate(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	if !file.open || !file.writable || file.node == nil || file.node.key == "" {
		return jvm.VoidValue(), newGuestException("java/io/IOException", "file is not open for writing")
	}
	runtime.truncateXFileLocked(file.node)
	file.cursor = 0
	return jvm.VoidValue(), nil
}

// isWIPIFile reports a receiver that is an org.kwis.msp.io.File rather than an
// XFile; the two share natives but not every answer.
func isWIPIFile(vm *jvm.VM, arguments []jvm.Value) bool {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil || receiver == nil {
		return false
	}
	return receiver.ClassName == wipi.FileClass || vm != nil && vm.IsInstance(receiver, wipi.FileClass)
}

func (runtime *Runtime) initXFileArchive(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	entry, err := stringArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	entry, err = safeEntryName(strings.TrimPrefix(entry, "/"))
	if err != nil {
		return jvm.VoidValue(), newGuestException("java/io/IOException", err.Error())
	}
	data, found, err := runtime.xFileContents(name)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !found {
		return jvm.VoidValue(), newGuestException("java/io/IOException", "no such archive: "+name)
	}
	entries, err := readJAR(data)
	if err != nil {
		return jvm.VoidValue(), newGuestException("java/io/IOException", err.Error())
	}
	contents, found := entries[entry]
	if !found {
		return jvm.VoidValue(), newGuestException("java/io/IOException", "no such archive entry: "+entry)
	}
	receiver.Native = &xFileData{name: name + "!/" + entry, archiveName: name, archiveEntry: entry, mode: xFileRead, open: true,
		node: &xFileNode{data: contents, present: true}}
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

// contents is what a handle's file holds: nothing for a raw handle.
func (file *xFileData) contents() []byte {
	if file.node == nil {
		return nil
	}
	return file.node.data
}

func (runtime *Runtime) xFileAvailable(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	return jvm.IntValue(int32(max(len(file.contents())-file.cursor, 0))), nil
}

func (runtime *Runtime) xFileRead(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
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
	runtime.fileMu.Lock()
	if !file.open || file.mode&xFileRead == 0 {
		runtime.fileMu.Unlock()
		return jvm.IntValue(-1), nil
	}
	data := file.contents()
	count := min(int(length), len(data)-file.cursor)
	if count <= 0 {
		runtime.fileMu.Unlock()
		// WIPI File.read answers -1 for an end reached before any byte, and
		// a title's `!= -1` read loop depends on it; XFile answers 0 there.
		if length > 0 && isWIPIFile(vm, arguments) {
			return jvm.IntValue(-1), nil
		}
		return jvm.IntValue(0), nil
	}
	chunk := append([]byte(nil), data[file.cursor:file.cursor+count]...)
	file.cursor += count
	runtime.fileMu.Unlock()

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
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	node := file.node
	if !file.open || !file.writable || node == nil || node.key == "" {
		return jvm.IntValue(-1), nil
	}
	if end := file.cursor + len(data); end > len(node.data) && end > xFileMaxBytes {
		return jvm.IntValue(-1), nil
	}
	node.own()
	if file.cursor > len(node.data) {
		node.data = append(node.data, make([]byte, file.cursor-len(node.data))...)
	}
	end := file.cursor + len(data)
	if end > len(node.data) {
		node.data = append(node.data, make([]byte, end-len(node.data))...)
	}
	copy(node.data[file.cursor:end], data)
	file.cursor = end
	// Writing a path makes it exist, a removed one included: the next store
	// takes its name off the removal list.
	node.present, node.dirty = true, true
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
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	base := 0
	switch whence {
	case 0:
		base = 0
	case 1:
		base = file.cursor
	case 2:
		base = len(file.contents())
	default:
		return jvm.IntValue(-1), nil
	}
	position := int64(base) + int64(offset)
	if position < 0 || position > 1<<31-1 {
		return jvm.IntValue(-1), nil
	}
	file.cursor = int(position)
	return jvm.IntValue(int32(position)), nil
}

func (runtime *Runtime) xFileFlush(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	runtime.persistXFileLocked(file)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) xFileClose(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	runtime.persistXFileLocked(file)
	file.open = false
	return jvm.VoidValue(), nil
}

// persistXFileLocked stores the file a handle is open on, as every handle on
// the path has left it. A refusal is not the guest's to see — XFile's flush
// and close answer nothing — and the file stays dirty for the next attempt.
// The caller holds fileMu.
func (runtime *Runtime) persistXFileLocked(file *xFileData) {
	if err := runtime.storeXFileLocked(file.node); err != nil && runtime.logger != nil {
		runtime.logger.Debug("XFile store failed", "name", file.name, "error", err)
	}
}

// xFileContents is a path's bytes without opening it, for reading an
// installed archive. Bytes a later write could change in place are copied;
// packaged ones are not, because a write copies those first.
func (runtime *Runtime) xFileContents(name string) ([]byte, bool, error) {
	key, err := xFileKey(name)
	if err != nil {
		return nil, false, err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	node, err := runtime.xFilePeekLocked(key)
	if err != nil || !node.present {
		return nil, false, err
	}
	if node.borrowed {
		return node.data, true, nil
	}
	return slices.Clone(node.data), true, nil
}

// xFileState answers whether a path exists and how long it is, as every
// handle on it has left it.
func (runtime *Runtime) xFileState(name string) (bool, int, error) {
	key, err := xFileKey(name)
	if err != nil {
		return false, 0, err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	node, err := runtime.xFilePeekLocked(key)
	if err != nil {
		return false, 0, err
	}
	return node.present, len(node.data), nil
}

func (runtime *Runtime) xFileExists(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	present, _, err := runtime.xFileState(name)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if present {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) xFileSize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	present, size, err := runtime.xFileState(name)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !present {
		return jvm.IntValue(-1), nil
	}
	return jvm.IntValue(int32(size)), nil
}

func (runtime *Runtime) xFileAvail(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(xFileCapacity), nil
}

func (runtime *Runtime) xFileUsed(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(0), nil
}

// xFileUnlink removes a file. It answers -1 for a name that cannot be
// removed and 0 otherwise, including for a name that does not exist, which it
// always answered.
func (runtime *Runtime) xFileUnlink(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !runtime.removeXFile(name) {
		return jvm.IntValue(-1), nil
	}
	return jvm.IntValue(0), nil
}

// removeXFile removes a path. From here on it is absent to every read —
// exists, filesize, an open for reading, and the handles still open on it,
// which read an empty file — and the removal list keeps it absent in later
// sessions, its packaged file included, until a writable open or a write makes
// the path again. A path nothing holds — no write, no save, no packaged file — is
// already absent: removing it changes nothing and stores nothing. It answers
// false for a name it cannot remove: one that is not a save key, one the
// store could not be read for, and one the removal list cannot carry, since
// the list is a name per line and a name with a line break in it would come
// back as other names.
func (runtime *Runtime) removeXFile(name string) bool {
	key, err := xFileKey(name)
	if err != nil || strings.ContainsAny(key, "\n\r") {
		return false
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	node, err := runtime.xFilePeekLocked(key)
	if err != nil {
		return false
	}
	if !node.present {
		return true
	}
	removed, err := runtime.xFileRemovalsLocked()
	if err != nil {
		return false
	}
	node.data, node.borrowed, node.present = nil, false, false
	// Only what a later session would otherwise find needs writing down: a
	// file made or written this session and never stored leaves nothing
	// behind.
	node.dirty = !removed[strings.TrimPrefix(key, xFileSaveScope)] && (node.stored || node.packaged)
	if err := runtime.storeXFileLocked(node); err != nil && runtime.logger != nil {
		runtime.logger.Debug("XFile removal store failed", "name", name, "error", err)
	}
	return true
}
