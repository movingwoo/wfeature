package ktf

import (
	"encoding/binary"
	"fmt"
	"path"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The title's own files sit beside its module in the package, and the module
// opens them by bare name through an interface it queries for. Unlike the
// descriptor package there is no private prefix: a name in the module is a
// name in the archive.
//
// The interface hands back an object rather than a number, so a file is a
// pointer to a table like everything else on this surface. One table serves
// every open file and a handler tells them apart by the object it was called
// on, which is what the module does too.
const (
	// nativeFileOpen takes a name and a mode and answers a file object, or
	// zero. The module retries a refused open with a different mode before
	// giving up, so a refusal has to be a refusal rather than an error.
	nativeFileOpen = 0x08
	// nativeFileExists and nativeFileCreate are the pair the module's own open
	// wrapper calls before it opens for writing: it asks whether the name is
	// there and creates it when it is not. The title's start-up gate is one of
	// those creates — it makes a scratch file to find out whether the handset
	// has room, and puts "not enough file system memory" on the screen when
	// the answer is no.
	nativeFileExists = 0x1c
	nativeFileCreate = 0x10
	// nativeFileInformation takes a name and a record to fill. The module
	// keeps the record and reads the file's length out of its third word.
	nativeFileInformation = 0x0c
)

// The file object's own table, by byte offset.
const (
	// nativeFileClose is called on the object the module is done with, and
	// the module clears its own pointer to it afterwards.
	nativeFileClose = 0x04
	// nativeFileRead and nativeFileWrite take a buffer and a length and answer
	// how many bytes moved. One loop in the module drives both and picks
	// between them on a flag its caller passes: the flag is set by the write
	// wrapper, which also measures a null-terminated buffer with the platform
	// table's own length slot when its caller passes no length.
	nativeFileRead  = 0x0c
	nativeFileWrite = 0x14
	// nativeFileSeek takes the same whence and offset the module's own
	// wrapper takes, which is what says the two agree on the codes: the
	// wrapper computes its own position from them and then passes them
	// straight through.
	nativeFileSeek = 0x1c
)

// The whence codes the module's own seek wrapper switches on.
const (
	nativeSeekStart   = 0
	nativeSeekEnd     = 1
	nativeSeekCurrent = 2
)

// nativeFileRecordSize covers the record the information call fills. Only its
// third word is read by this title — the length — so the rest is zeroed rather
// than invented.
const nativeFileRecordSize = 0x0c

// nativeFileLengthOffset is where in that record the length sits.
const nativeFileLengthOffset = 0x08

// nativeFileSurface is the shared table every open file's object points at.
const nativeFileSurface NativeSurface = "file"

// nativeMaxFileName bounds the name read out of guest memory.
const nativeMaxFileName = 256

// NativeFileOpen records one open the module asked for. A run that stops
// inside the title's own loading code says nothing about which file it was
// reading, and the module names its files only in its own code, so the list of
// opens is what turns "it stopped while loading" into a name.
type NativeFileOpen struct {
	Name  string
	Mode  uint32
	Found bool
}

// FileOpens reports every open the module asked for, in order.
func (platform *NativePlatform) FileOpens() []NativeFileOpen { return platform.opens }

// nativeOpenFile is one file the module has open.
type nativeOpenFile struct {
	name string
	// key is the lower-cased base name the session's own copy is kept under.
	key      string
	data     []byte
	position int64
	writable bool
}

// nativeModeRead is the only mode this title opens a file it only reads with.
// The module's own wrapper retries a refused open of mode 2 or 8 with mode 4,
// which is what says those are writes: a read that failed would have nothing
// to retry with. So anything but a plain read may create the file.
const nativeModeRead = 1

// installFiles registers the file interface and the table its objects share.
func (platform *NativePlatform) installFiles() error {
	table, err := platform.client.AddSurface(nativeFileSurface)
	if err != nil {
		return err
	}
	platform.fileTable = table
	platform.files = map[uint32]*nativeOpenFile{}
	if platform.written == nil {
		platform.written = map[string][]byte{}
	}

	files := nativeInterfaceSurface(nativeInterfaceFile)
	platform.client.Serve(files, nativeFileOpen, platform.openFile)
	platform.client.Serve(files, nativeFileInformation, platform.fileInformation)
	platform.client.Serve(files, nativeFileExists, platform.fileExists)
	platform.client.Serve(files, nativeFileCreate, platform.createFile)

	platform.client.Serve(nativeFileSurface, nativeFileClose, platform.closeFile)
	platform.client.Serve(nativeFileSurface, nativeFileRead, platform.readFile)
	platform.client.Serve(nativeFileSurface, nativeFileWrite, platform.writeFile)
	platform.client.Serve(nativeFileSurface, nativeFileSeek, platform.seekFile)
	return nil
}

// AttachSaves gives the platform somewhere to keep what the title writes.
// Without one a save lives for the session and no longer, which is what a
// probe wants and not what a player does.
func (platform *NativePlatform) AttachSaves(store SaveStore) {
	platform.saves = store
}

// nativeSaveKey scopes one of the title's files inside the save store. It is
// the same "fs/" space the descriptor package's guest filesystem uses, because
// it is the same kind of thing: a file the title names and writes.
func nativeSaveKey(name string) string {
	return "fs/" + name
}

// contents finds a file the package carries. Names are matched on the base
// name and without regard to case: the module names its files the way they
// were written into the archive, and the archive's entry names are the
// title's directory rather than a bare name.
func (platform *NativePlatform) contents(name string) ([]byte, bool) {
	if platform.archive == nil {
		return nil, false
	}
	wanted := strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/")))
	// What the title has written this session comes first: a save it wrote and
	// then reopened has to read back what it wrote. Behind that is what it
	// wrote in an earlier session, and only then what the package shipped — a
	// save has to win over the archive's copy of the same name, or a title
	// would start every session from its shipped settings.
	if data, ok := platform.written[wanted]; ok {
		return data, true
	}
	if platform.saves != nil {
		if data, ok := platform.saves.LoadSave(nativeSaveKey(wanted)); ok {
			return data, true
		}
	}
	if data, ok := platform.archive.Files[name]; ok {
		return data, true
	}
	for entry, data := range platform.archive.Files {
		if strings.ToLower(path.Base(entry)) == wanted {
			return data, true
		}
	}
	return nil, false
}

// readName reads a name argument out of guest memory.
func (platform *NativePlatform) readName(address uint32) (string, error) {
	if address == 0 {
		return "", fmt.Errorf("KTF native file call names no file")
	}
	data := make([]byte, 0, 32)
	buffer := make([]byte, 1)
	for offset := uint32(0); offset < nativeMaxFileName; offset++ {
		if err := platform.client.core.Memory().Read(address+offset, buffer); err != nil {
			return "", fmt.Errorf("read KTF native file name at %#x: %w", address, err)
		}
		if buffer[0] == 0 {
			return string(data), nil
		}
		data = append(data, buffer[0])
	}
	return "", fmt.Errorf("KTF native file name at %#x is not terminated within %d bytes", address, nativeMaxFileName)
}

// openFile answers the interface's open.
func (platform *NativePlatform) openFile(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := platform.readName(address)
	if err != nil {
		return 0, err
	}
	mode, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	writable := int32(mode) != nativeModeRead
	data, ok := platform.contents(name)
	platform.opens = append(platform.opens, NativeFileOpen{Name: name, Mode: mode, Found: ok})
	if !ok {
		if !writable {
			// A file the package does not carry is a refusal, not a failure.
			// The module opens names it may not have and answers for itself
			// what to do about it.
			return 0, nil
		}
		// Opening for writing creates the file. A title whose save has never
		// been written has no other way to make one, and the module treats a
		// refused create as the end of the run rather than as an empty save.
		platform.create(strings.ToLower(path.Base(name)))
		data = platform.written[strings.ToLower(path.Base(name))]
	}
	object, err := platform.client.Allocate(4)
	if err != nil {
		return 0, err
	}
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, platform.fileTable)
	if err := platform.client.core.Memory().Write(object, word); err != nil {
		return 0, fmt.Errorf("write KTF native file object for %q: %w", name, err)
	}
	platform.files[object] = &nativeOpenFile{
		name:     name,
		key:      strings.ToLower(path.Base(name)),
		data:     append([]byte(nil), data...),
		writable: writable,
	}
	return object, nil
}

// fileInformation answers the interface's information call.
func (platform *NativePlatform) fileInformation(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	out, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	name, err := platform.readName(address)
	if err != nil {
		return 0, err
	}
	record := make([]byte, nativeFileRecordSize)
	data, ok := platform.contents(name)
	if ok {
		binary.LittleEndian.PutUint32(record[nativeFileLengthOffset:], uint32(len(data)))
	}
	if err := platform.client.core.Memory().Write(out, record); err != nil {
		return 0, fmt.Errorf("write KTF native file record for %q at %#x: %w", name, out, err)
	}
	if !ok {
		return 0, nil
	}
	return 1, nil
}

// fileExists answers whether a name is there.
func (platform *NativePlatform) fileExists(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := platform.readName(address)
	if err != nil {
		return 0, err
	}
	if _, ok := platform.contents(name); ok {
		return 1, nil
	}
	return 0, nil
}

// createFile makes an empty file.
func (platform *NativePlatform) createFile(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := platform.readName(address)
	if err != nil {
		return 0, err
	}
	if _, ok := platform.contents(name); !ok {
		platform.create(strings.ToLower(path.Base(name)))
	}
	return 1, nil
}

// create makes an empty file, and keep records what the title wrote. Both go
// to the store as well as to the session, so a save survives the session that
// wrote it.
func (platform *NativePlatform) create(key string) { platform.keep(key, []byte{}) }

func (platform *NativePlatform) keep(key string, data []byte) {
	platform.written[key] = data
	if platform.saves == nil {
		return
	}
	if err := platform.saves.StoreSave(nativeSaveKey(key), data); err != nil {
		// A store that refuses is not the title's problem: it wrote what it
		// wrote, and the session still reads it back. The Host's log is where
		// a failing store belongs.
		platform.storeFailures++
	}
}

// StoreFailures reports how many writes the save store refused, which is what
// says a session that looks like it saved did not.
func (platform *NativePlatform) StoreFailures() int { return platform.storeFailures }

// openFileFor resolves the object a file call was made on.
func (platform *NativePlatform) openFileFor(thread *armcore.Thread) (*nativeOpenFile, error) {
	object, err := thread.Register(0)
	if err != nil {
		return nil, err
	}
	file, ok := platform.files[object]
	if !ok {
		return nil, fmt.Errorf("KTF native file call on %#x, which is not an open file", object)
	}
	return file, nil
}

// closeFile answers the file object's close.
func (platform *NativePlatform) closeFile(thread *armcore.Thread) (uint32, error) {
	object, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	delete(platform.files, object)
	platform.client.Free(object)
	return 0, nil
}

// readFile answers the file object's read.
func (platform *NativePlatform) readFile(thread *armcore.Thread) (uint32, error) {
	file, err := platform.openFileFor(thread)
	if err != nil {
		return 0, err
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	length, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	if file.position >= int64(len(file.data)) || length == 0 {
		return 0, nil
	}
	available := int64(len(file.data)) - file.position
	if int64(length) < available {
		available = int64(length)
	}
	chunk := file.data[file.position : file.position+available]
	if err := platform.client.core.Memory().Write(buffer, chunk); err != nil {
		return 0, fmt.Errorf("write %d bytes of %q to %#x: %w", len(chunk), file.name, buffer, err)
	}
	file.position += available
	return uint32(available), nil
}

// writeFile answers the file object's write. What a title writes is kept for
// the session and shadows the package's own copy of the same name, so a save
// it writes and then reads back is the save it wrote.
func (platform *NativePlatform) writeFile(thread *armcore.Thread) (uint32, error) {
	file, err := platform.openFileFor(thread)
	if err != nil {
		return 0, err
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	length, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	if !file.writable {
		// A write to a file opened for reading is refused the way a short
		// write is: the module adds what it is told and stops on a zero.
		return 0, nil
	}
	if length == 0 {
		return 0, nil
	}
	if uint64(length) > nativeMaxTransfer {
		return 0, fmt.Errorf("KTF native write of %d bytes to %q", length, file.name)
	}
	chunk := make([]byte, length)
	if err := platform.client.core.Memory().Read(buffer, chunk); err != nil {
		return 0, fmt.Errorf("read %d bytes at %#x: %w", length, buffer, err)
	}
	end := file.position + int64(length)
	if end > int64(len(file.data)) {
		grown := make([]byte, end)
		copy(grown, file.data)
		file.data = grown
	}
	copy(file.data[file.position:end], chunk)
	file.position = end
	platform.keep(file.key, file.data)
	return length, nil
}

// seekFile answers the file object's seek. The module's own wrapper computes
// the same position from the same two arguments and then passes them through,
// so this follows its arithmetic rather than a convention of its own — its
// end-relative case counts back from the last byte.
func (platform *NativePlatform) seekFile(thread *armcore.Thread) (uint32, error) {
	file, err := platform.openFileFor(thread)
	if err != nil {
		return 0, err
	}
	whence, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	offset, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	position := file.position
	switch whence {
	case nativeSeekStart:
		position = int64(int32(offset))
	case nativeSeekEnd:
		position = int64(len(file.data)) - 1 - int64(int32(offset))
	case nativeSeekCurrent:
		position += int64(int32(offset))
	default:
		return 1, nil
	}
	if position < 0 || position > int64(len(file.data)) {
		return 1, nil
	}
	file.position = position
	return 0, nil
}
