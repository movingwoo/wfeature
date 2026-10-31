package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// `org/kwis/msp/io/File`, which is the same filesystem a Clet writes its save
// into: one store and one set of open handles. A Java title
// and a Clet of the same game would see each other's files, which is what makes
// this the right layer to put it on rather than a second store beside it.
//
// The module lists the class's virtual methods in its own table, so these are
// reached by name rather than by a baked slot: `sizeOf()I`, `read([B)I`,
// `write([B)I`, `write([BII)I`, `write(I)I` and `close()V`, with the
// constructor arriving as a static entry on the object the module allocated.

const javaFileClass = "org/kwis/msp/io/File"

// javaFileOpen is `File(String, int mode, int access)`. The access argument is
// the sharing level, and this platform has one application and one private
// store, so there is nothing for it to select. The mode is the specification's
// enum: 1 read-only, 2 append, 3 truncate, 4 read-write. Translate it to the
// C bit flags before opening; treating Java mode 4 as C mode 4 discards the
// contents of an existing save before the caller can read it.
//
// **A file that will not open is an IOException**, which is what the
// specification says the constructor throws, and what the call sites are
// wrapped in a try region for. The name is read as every file call reads one
// (javaFileSystemName), and the title's own directory is no name a file can
// be opened under.
func javaFileOpen(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	object, name, mode := arguments[0], arguments[1], arguments[2]
	path, root, err := client.javaFileSystemName(thread, name)
	if err != nil {
		return 0, err
	}
	var flag uint32
	switch mode {
	case 1:
		flag = fileOpenReadOnly
	case 2:
		flag = fileOpenWriteOnly
	case 3:
		flag = fileOpenWriteTruncate
	case 4:
		flag = fileOpenReadWrite
	default:
		return 0, client.throwJavaPlatform(thread, "java/lang/IllegalArgumentException",
			fmt.Sprintf(": invalid file mode %d", mode))
	}
	handle := wipiBadFileName
	if !root {
		handle = client.openFile(path, flag)
	}
	if handle < 0 {
		if client.logger != nil {
			client.logger.Debug("LGT java file will not open", "name", path, "mode", mode)
		}
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass,
			fmt.Sprintf(": %s in mode %d", path, mode))
	}
	client.javaRuntimeState().files[object] = uint32(handle)
	if client.logger != nil {
		client.logger.Debug("LGT java file opened", "name", path, "mode", mode, "file", object)
	}
	return 0, nil
}

const javaIOExceptionClass = "java/io/IOException"

// `org/kwis/msp/io/FileSystem`, which asks about a path rather than an open
// file. It reads and writes the same store the File constructor opens from,
// so a Java title and a Clet agree about what a title has saved, and **each
// member answers what the C file slot for the same question answers**, in the
// language's form: a C failure code is the IOException the member declares.
// The common contract (docs/wipi-storage.md) gives both. The access flag of
// the two-argument forms chooses nothing: a title here has one directory, its
// own, and nothing answers SecurityException.
const javaFileSystemClass = "org/kwis/msp/io/FileSystem"

// javaFileSystemName reads a name argument the way every file call reads one,
// and reports whether it names the title's own directory: null is a
// NullPointerException, and a name longer than getMaxFilenameLength, or one
// no file can have here, is an IOException — the M_E_LONGNAME and
// M_E_BADFILENAME the C slots answer (fileNameCode).
func (client *Client) javaFileSystemName(thread *armcore.Thread, object uint32) (string, bool, error) {
	if object == 0 {
		return "", false, client.throwJavaPlatform(thread, javaThrowNullClass, ": a null file name")
	}
	name, ok := client.javaText(object)
	if !ok {
		return "", false, fmt.Errorf("the name at %#x is not a string this platform built", object)
	}
	root, code := fileNameCode(name)
	switch code {
	case wipiLongName:
		return "", false, client.throwJavaPlatform(thread, javaIOExceptionClass,
			fmt.Sprintf(": a name of %d bytes is longer than %d", len(name), maxFileNameBytes))
	case wipiBadFileName:
		return "", false, client.throwJavaPlatform(thread, javaIOExceptionClass, fmt.Sprintf(": %q is not a file name", name))
	}
	return name, root, nil
}

// javaFileSystemCode throws the IOException a C file code stands for, and is
// nothing for success.
func (client *Client) javaFileSystemCode(thread *armcore.Thread, code int32, what string) error {
	if code == wipiSuccess {
		return nil
	}
	return client.throwJavaPlatform(thread, javaIOExceptionClass, fmt.Sprintf(": %s answered %d", what, code))
}

func javaBoolean(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

// javaFileExists is `exists`: a file or a directory, as MC_fsIsExist answers.
func javaFileExists(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, root, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	return javaBoolean(root || client.fileExists(name) || client.holdsDirectory(name)), nil
}

// javaFileIsFile is `isFile`: a file of that name, and not a directory.
func javaFileIsFile(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, root, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	return javaBoolean(!root && client.fileExists(name)), nil
}

// javaFileIsDirectory is `isDirectory`: what directoryExists answers.
func javaFileIsDirectory(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, root, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	return javaBoolean(root || client.directoryExists(name)), nil
}

// javaFileRemove is `remove(String)`, which is MC_fsRemove: a name that is not
// a file is the IOException the specification gives a file that does not
// exist.
func javaFileRemove(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, root, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	if root || !client.fileExists(name) {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, fmt.Sprintf(": no file %s", name))
	}
	return 0, client.javaRemoveFile(thread, name)
}

// javaFileRemoveFlagged is `remove(String, int)`, which the specification's
// v2.0 text says throws nothing for a missing file: it removes a file that is
// there and returns for a name that is not one. A removal the store refuses
// is still the IOException both forms declare.
func javaFileRemoveFlagged(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, root, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	if root || !client.fileExists(name) {
		return 0, nil
	}
	return 0, client.javaRemoveFile(thread, name)
}

// javaRemoveFile removes a file for either form of remove, and throws the
// IOException a removal the store refuses is; the file is then still there
// (see removeFile).
func (client *Client) javaRemoveFile(thread *armcore.Thread, name string) error {
	if err := client.removeFile(name); err != nil {
		return client.throwJavaPlatform(thread, javaIOExceptionClass, fmt.Sprintf(": %s could not be removed: %v", name, err))
	}
	return nil
}

// javaFileList is `list`, which answers a Vector of the names a directory
// holds, files and directories alike: MC_fsList's listing, each name a
// String. A name that is not a directory is the IOException the specification
// gives one that does not exist; MC_fsList answers the same name an empty
// list, because the C call has no code for it.
func javaFileList(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, root, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	if !root && !client.directoryExists(name) {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, fmt.Sprintf(": no directory %s", name))
	}
	names := client.listDirectory(name)
	class, err := client.preparePlatformJavaClass(javaVectorClass)
	if err != nil {
		return 0, err
	}
	vector, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	// Everything a platform call allocates is a root until it returns (see
	// javaPinMark), so a collection one of these allocations raises keeps the
	// Vector and the Strings built so far.
	held := make([]uint32, 0, len(names))
	for _, entry := range names {
		text, err := client.newJavaString(entry)
		if err != nil {
			return 0, err
		}
		held = append(held, text)
	}
	client.javaRuntimeState().vectors[vector] = held
	return vector, nil
}

// javaFileMakeDirectory is `mkdir` and javaFileRemoveDirectory `rmdir`, which
// are MC_fsMkDir and MC_fsRmDir (makeDirectory, removeDirectory): every code
// the C call answers other than success is an IOException.
func javaFileMakeDirectory(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, root, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	return 0, client.javaFileSystemCode(thread, client.makeDirectory(name, root, wipiSuccess), "mkdir "+name)
}

func javaFileRemoveDirectory(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, root, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	return 0, client.javaFileSystemCode(thread, client.removeDirectory(name, root, wipiSuccess), "rmdir "+name)
}

// javaFileRename is `rename`, which is MC_fsRename: the same refusals (an
// absent file, a taken name, a name no file can have, a store that refuses),
// each one the IOException the specification gives "an error while
// renaming". The specification asks both names to be in one directory, and
// the C slot does not ask it, so neither does this.
func javaFileRename(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	oldName, oldRoot, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	newName, newRoot, err := client.javaFileSystemName(thread, arguments[1])
	if err != nil {
		return 0, err
	}
	code := wipiNoEntry
	switch {
	case oldRoot:
	case newRoot:
		code = wipiBadFileName
	default:
		code = client.renameFile(oldName, newName)
	}
	return 0, client.javaFileSystemCode(thread, code, "rename "+oldName+" to "+newName)
}

// javaFileCreationTime is `getCreationTime`, in seconds, with -1 for a
// directory as the specification gives it. **No creation time is kept**: a
// file is a save key and its bytes, and neither a save nor a packaged file
// carries one, so a file answers 0 — the creation time MC_fsFileAttribute
// writes for every file. A name that is neither is the IOException a C
// stat's M_E_NOENT is.
func javaFileCreationTime(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, root, err := client.javaFileSystemName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	switch {
	case root:
		return ^uint32(0), nil
	case client.fileExists(name):
		return 0, nil
	case client.directoryExists(name):
		return ^uint32(0), nil
	}
	return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, fmt.Sprintf(": no file %s", name))
}

// javaFileMaxNameLength is `getMaxFilenameLength`: maxFileNameBytes.
func javaFileMaxNameLength(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return uint32(maxFileNameBytes), nil
}

// javaFileCString is `toCString`: the text in the handset's encoding (KSC5601
// for Korean, which the specification names, and ISO 8859 for the rest) with
// the terminating NUL a C string ends in, the encoding `String.getBytes`
// answers in.
func javaFileCString(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[0] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": toCString of null")
	}
	text, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the string at %#x is not one this platform built", arguments[0])
	}
	return client.newJavaByteArray(append(backend.EncodeEUCKR(text), 0))
}

// javaFileSystemTotal is `totalSpace`, MC_fsTotalSpace's storageQuotaBytes,
// and javaFileSystemAvailable is `available`, MC_fsAvailable's
// storageAvailable.
func javaFileSystemTotal(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return uint32(storageQuotaBytes), nil
}

func javaFileSystemAvailable(client *Client, _ context.Context, _ *armcore.Thread, _ []uint32) (uint32, error) {
	return uint32(client.storageAvailable()), nil
}

// javaFileSeek is `File.seek(int)`, which is MC_fsSeek from the start of the
// file: the position is absolute, as the specification says. A position the C
// call refuses (before the start, or past the size a file may grow to) is the
// IOException the specification gives an error while moving the pointer, and
// so is a File that is not open. A position past the end is a seek a later
// write fills up to with zeros, as it is for MC_fsSeek.
func javaFileSeek(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	handle, _, err := client.javaFileHandle(arguments[0])
	if err != nil {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, ": the file is not open")
	}
	if position := client.seekFile(handle, int32(arguments[1]), 0); position < 0 {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass,
			fmt.Sprintf(": seek to %d", int32(arguments[1])))
	}
	return 0, nil
}

// javaFileHandle answers the open file an object stands for.
func (client *Client) javaFileHandle(object uint32) (uint32, *openFile, error) {
	if client.javaRun == nil {
		return 0, nil, fmt.Errorf("no file has been opened")
	}
	handle, ok := client.javaRun.files[object]
	if !ok {
		return 0, nil, fmt.Errorf("the object at %#x is not a file this platform opened", object)
	}
	file := client.files[handle]
	if file == nil {
		return 0, nil, fmt.Errorf("the file at %#x is closed", object)
	}
	return handle, file, nil
}

// javaFileSize is `sizeOf()`, which is the whole file rather than what is left
// of it: the specification calls it the size.
func javaFileSize(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	_, file, err := client.javaFileHandle(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(len(file.data)), nil
}

// javaFileRead is `read([B)` and `read([BII)`: the bytes go into the array the
// caller passes, and the answer is how many, or -1 at the end.
func javaFileRead(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	handle, _, err := client.javaFileHandle(arguments[0])
	if err != nil {
		return 0, err
	}
	block, offset, count, err := client.javaFileWindow(arguments)
	if err != nil {
		return 0, err
	}
	answer := client.transferFile(slotFsRead, handle, block+offset, count)
	if answer == 0 && count > 0 {
		// The C call answers zero at the end of a file and Java answers -1, and
		// a caller that loops on the Java one never stops on a zero.
		return ^uint32(0), nil
	}
	return uint32(answer), nil
}

// javaFileWrite is `write([B)` and `write([BII)`.
//
// A write the file cannot take — past the size a file may grow to, or into a
// File opened to read — is the IOException the specification gives a write
// that cannot be made, where MC_fsWrite answers a code.
func javaFileWrite(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	handle, _, err := client.javaFileHandle(arguments[0])
	if err != nil {
		return 0, err
	}
	block, offset, count, err := client.javaFileWindow(arguments)
	if err != nil {
		return 0, err
	}
	written := client.transferFile(slotFsWrite, handle, block+offset, count)
	if written < 0 {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass,
			fmt.Sprintf(": a write of %d bytes answered %d", count, written))
	}
	return uint32(written), nil
}

// javaFileWindow resolves the array argument the read and write pair take, with
// the offset and count when the four-argument form passes them.
func (client *Client) javaFileWindow(arguments []uint32) (uint32, uint32, uint32, error) {
	block, length, err := client.javaArrayBlock(arguments[1])
	if err != nil {
		return 0, 0, 0, err
	}
	offset, count := uint32(0), length
	if len(arguments) > 3 {
		offset, count = arguments[2], arguments[3]
	}
	if uint64(offset)+uint64(count) > uint64(length) {
		return 0, 0, 0, fmt.Errorf("%d bytes at %d of an array of %d", count, offset, length)
	}
	return block + javaArrayLengthWords*4, offset, count, nil
}

// javaFileWriteByte is `write(int)`: one byte, the low eight bits of it.
func javaFileWriteByte(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	_, file, err := client.javaFileHandle(arguments[0])
	if err != nil {
		return 0, err
	}
	if !file.writable {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, fmt.Sprintf(": %s was opened read-only", file.name))
	}
	value := byte(arguments[1])
	// A cursor past the end fills the gap with zeros, as the array forms of
	// this call do. A seek, or a file that is shorter after a quick load than
	// it was before one, moves the cursor there. A byte past maxFileBytes is
	// the IOException the array forms throw there.
	if !file.extend(file.cursor + 1) {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass,
			fmt.Sprintf(": %s cannot grow past %d bytes", file.name, maxFileBytes))
	}
	file.data[file.cursor] = value
	file.cursor++
	file.dirty = true
	return 1, nil
}

// javaFileOpenOutputStream is `File.openOutputStream()`, vtable slot 12: an
// `OutputStream` that writes into the file this File has open. A title reaches
// for it when it has a block to write rather than an array — the specification
// says as much, offering the four stream openers as the faster way to move
// bytes than `read`/`write` on the File itself.
//
// **What comes back is a byte sink bound to the file.** A `ByteArrayOutputStream`
// is an `OutputStream`, so the type is right and every write, flush and close
// slot already works; the binding is what makes the bytes land somewhere other
// than in memory. Every write goes into the File at its cursor as it is made,
// because a write to the stream is a write to the file: the File's size, a
// read of the path and every other open see it at once, and it waits in the
// File's buffer like a write through the File itself. The stream's flush and
// close store the File, as the File's own close does.
//
// The specification's one failure is a file that is not open or that already
// has a stream out, and both are an IOException.
func javaFileOpenOutputStream(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.openJavaFileOutputStream(thread, arguments[0], javaByteSinkClass)
}

// javaFileOpenDataOutputStream is `File.openDataOutputStream()`: the same
// binding with DataOutputStream's vtable, so the object it answers also takes
// the big-endian writes `writeInt` and the rest, each one reaching the file at
// its cursor as a write through the File would. The specification counts it
// with `openOutputStream` — one output stream on a File, a DataOutputStream
// included — so a second of either kind is the IOException a second
// `openOutputStream` is, as `openDataInputStream` shares its count with
// `openInputStream`.
func javaFileOpenDataOutputStream(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.openJavaFileOutputStream(thread, arguments[0], javaDataOutputStreamClass)
}

func (client *Client) openJavaFileOutputStream(thread *armcore.Thread, file uint32, className string) (uint32, error) {
	if _, _, err := client.javaFileHandle(file); err != nil {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, ": the file is not open")
	}
	runtime := client.javaRuntimeState()
	for sink, owner := range runtime.sinkFiles {
		if owner == file {
			_ = sink
			return 0, client.throwJavaPlatform(thread, javaIOExceptionClass,
				": an output stream is already open on this file")
		}
	}
	class, err := client.preparePlatformJavaClass(className)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	runtime.sinks[object] = []byte{}
	runtime.sinkFiles[object] = file
	if client.logger != nil {
		client.logger.Debug("LGT java file output stream opened", "file", file, "stream", object, "class", className)
	}
	return object, nil
}

// javaFileOpenInputStream is `File.openInputStream()`, vtable slot 16: the read
// half of the pair above. It answers an `InputStream` over what is left of the
// file from its current position, which is the same stream object a resource
// read hands back, so every read, skip and close slot already implemented works
// on it unchanged.
//
// **The file's own position follows what the stream consumed**, so a title that
// reads part of a file through the stream and the rest through `File.read` sees
// one file rather than two views of it. The stream reads the file's buffer from
// that position on, the buffer every handle on the path writes, so a write made
// after the stream was opened is what it reads; see followJavaFileStream.
//
// The specification's failures are a file that is not open and a second stream
// on one file, both `IOException` — the same pair `openOutputStream` refuses.
func javaFileOpenInputStream(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.openJavaFileInputStream(thread, arguments[0], javaInputStreamClass)
}

// The typed opener uses the same file binding, with DataInputStream's vtable
// so the returned object also serves big-endian primitive reads.
func javaFileOpenDataInputStream(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.openJavaFileInputStream(thread, arguments[0], javaDataInputStreamClass)
}

func (client *Client) openJavaFileInputStream(thread *armcore.Thread, file uint32, className string) (uint32, error) {
	_, open, err := client.javaFileHandle(file)
	if err != nil {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, ": the file is not open")
	}
	runtime := client.javaRuntimeState()
	for _, owner := range runtime.streamFiles {
		if owner == file {
			return 0, client.throwJavaPlatform(thread, javaIOExceptionClass,
				": an input stream is already open on this file")
		}
	}
	class, err := client.preparePlatformJavaClass(className)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	rest := open.data[min(open.cursor, len(open.data)):len(open.data):len(open.data)]
	runtime.streams[object] = &javaStream{Name: open.name, Data: rest, File: true, Offset: open.cursor}
	runtime.streamFiles[object] = file
	if client.logger != nil {
		client.logger.Debug("LGT java file input stream opened",
			"file", file, "stream", object, "bytes", len(rest))
	}
	return object, nil
}

// followJavaFileStream points a stream opened on a File at what the file holds
// now, from where the stream's window starts. A write or a truncation through
// any handle on the path is then what the stream reads next, as it is on the
// handset, where the stream reads the file. What was read stays read, and a
// file that is shorter now ends the window where it ends. A stream whose File
// has been closed holds the file open itself and follows it the same way; see
// javaFileClose. One a record an earlier build took restores without its File
// and keeps the window it was given.
func (client *Client) followJavaFileStream(object uint32, stream *javaStream) {
	runtime := client.javaRun
	file, bound := runtime.streamFiles[object]
	if !bound {
		return
	}
	handle, open := runtime.files[file]
	if !open || client.files[handle] == nil {
		return
	}
	data := client.files[handle].data
	stream.Data = data[min(stream.Offset, len(data)):len(data):len(data)]
	stream.Read, stream.Mark = min(stream.Read, len(stream.Data)), min(stream.Mark, len(stream.Data))
}

// syncJavaStreamToFile moves the file's position on by what a bound read stream
// has consumed, so the File and the stream opened on it agree about where they
// are. It runs where the stream is done with — a close, and the File's own
// close — rather than on every read, because a read is the hot path and the
// two only have to agree at the points a title can observe both.
func (client *Client) syncJavaStreamToFile(stream uint32) error {
	runtime := client.javaRuntimeState()
	file, bound := runtime.streamFiles[stream]
	if !bound {
		return nil
	}
	held, ok := runtime.streams[stream]
	if !ok {
		return nil
	}
	_, open, err := client.javaFileHandle(file)
	if err != nil {
		return err
	}
	open.cursor = min(open.cursor+held.Read, len(open.data))
	return nil
}

// putJavaSink keeps what a sink holds after a write. A sink opened on a File
// hands it to the File at once: see javaFileOpenOutputStream. A write the File
// cannot take — past the size a file may grow to, or into a File opened to
// read — fails as a write through the File does, with the IOException the
// stream's write declares, and the sink keeps nothing of it.
func (client *Client) putJavaSink(thread *armcore.Thread, sink uint32, held []byte) error {
	runtime := client.javaRuntimeState()
	before := runtime.sinks[sink]
	runtime.sinks[sink] = held
	if _, bound := runtime.sinkFiles[sink]; !bound {
		return nil
	}
	if err := client.drainJavaSinkToFile(sink); err != nil {
		runtime.sinks[sink] = before
		return client.throwJavaPlatform(thread, javaIOExceptionClass, ": "+err.Error())
	}
	return nil
}

// storeJavaSinkFile is a flush or a close of a stream opened on a File: the
// File is given to the store, with what was written through the stream in
// it. A store that refuses keeps the write waiting in the File, as a refused
// File.close does, for the next flush, the File's close, the ending and a
// quick step to give again, and the refusal is answered for the caller to
// throw: OutputStream's flush and close declare IOException.
func (client *Client) storeJavaSinkFile(sink uint32) error {
	file, bound := client.javaRuntimeState().sinkFiles[sink]
	if !bound {
		return nil
	}
	_, open, err := client.javaFileHandle(file)
	if err != nil || !open.dirty {
		return nil
	}
	if err := client.storeContent(open.name, open.fileContent); err != nil {
		if client.logger != nil {
			client.logger.Debug("LGT java file stream flush kept a write the store refused", "name", open.name, "stream", sink)
		}
		return fmt.Errorf("%s could not be stored", open.name)
	}
	return nil
}

// drainJavaSinkToFile moves what a bound sink holds into the file behind it.
// It is what makes a stream opened on a File write to the File rather than to
// memory, and it runs at every write. The flush, the close, the File's own
// close and the ending run it as well, where it finds nothing left to move.
func (client *Client) drainJavaSinkToFile(sink uint32) error {
	runtime := client.javaRuntimeState()
	file, bound := runtime.sinkFiles[sink]
	if !bound {
		return nil
	}
	held := runtime.sinks[sink]
	if len(held) == 0 {
		return nil
	}
	handle, open, err := client.javaFileHandle(file)
	if err != nil {
		return err
	}
	if !open.writable {
		return fmt.Errorf("%s was opened read-only", open.name)
	}
	// The bytes go in at the file's own cursor, which is what a write through
	// the File would have done, and the sink is emptied so a flush followed by
	// a close does not write the same block twice.
	end := open.cursor + len(held)
	if !open.extend(end) {
		return fmt.Errorf("%d bytes at %d of %s is past the %d bytes a file may grow to",
			len(held), open.cursor, open.name, maxFileBytes)
	}
	copy(open.data[open.cursor:end], held)
	open.cursor = end
	open.dirty = true
	runtime.sinks[sink] = []byte{}
	_ = handle
	return nil
}

// javaFileClose is `close()`, which is where a write reaches the store: the C
// path writes a dirty file back on its own close, and this is the same close.
//
// **A write the store refuses is thrown at the title, and the File stays
// open with it.** The specification declares IOException for a file that
// cannot be closed properly, and the language makes every caller handle it,
// so the title is told what a refused C close tells a Clet (see
// closeFileHandle), and nothing of the File changes: its streams stay bound,
// and the next close, the ending and a quick step give the store the write
// again. Closing a File that is already closed is nothing, which the
// specification also says.
//
// **A stream opened on the File holds the file open after the File closes.**
// File is a StreamConnection, and closing a connection whose streams are open
// leaves them usable and the connection open under them until they close. So
// each open stream takes the File's handle as its own: an input stream goes on
// reading the file as it is now and an output stream writing it, and the
// stream's close, or the collector reclaiming it, closes the handle as the
// File's close would have. The File itself is closed. A stream left with a
// copy of what it had read stale bytes, and an output stream left with no
// file wrote what the title saved through it into nothing.
func javaFileClose(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	object := arguments[0]
	handle, file, err := client.javaFileHandle(object)
	if err != nil {
		if client.javaFileClosed(object) {
			return 0, nil
		}
		return 0, err
	}
	// A stream opened on this file has nothing left to drain, since every
	// write reached the file as it was made; the drain stays as the last
	// place a byte left in a stream could go before the close.
	runtime := client.javaRuntimeState()
	for sink, owner := range runtime.sinkFiles {
		if owner != object {
			continue
		}
		if err := client.drainJavaSinkToFile(sink); err != nil {
			return 0, err
		}
	}
	// A File opened only to read has nothing of its own to store: a write
	// waiting on the path is another handle's, and its close or the ending
	// stores it.
	if file.writable && file.dirty && client.storeContent(file.name, file.fileContent) != nil {
		if client.logger != nil {
			client.logger.Debug("LGT java file close kept a write the store refused", "name", file.name, "file", object)
		}
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, fmt.Sprintf(": %s could not be stored", file.name))
	}
	// The rewrite the open began is stored now, so the handle the streams
	// keep is no longer one: a quick load gives it the file as it is then,
	// as it gives any other handle. Left marked, a quick save taken while a
	// stream held it cut a longer save written after it back to this one.
	file.truncated = false
	for sink, owner := range runtime.sinkFiles {
		if owner == object {
			runtime.sinkFiles[sink], runtime.files[sink] = sink, handle
		}
	}
	for stream, owner := range runtime.streamFiles {
		if owner != object {
			continue
		}
		if err := client.syncJavaStreamToFile(stream); err != nil {
			return 0, err
		}
		held := runtime.streams[stream]
		if held == nil || held.Closed {
			delete(runtime.streamFiles, stream)
			continue
		}
		// The File's position has taken what the stream read, so the
		// stream's window starts there now: its own close then hands the
		// handle only what it reads from here on.
		held.Offset += held.Read
		held.Data, held.Read, held.Mark = held.Data[held.Read:], 0, max(held.Mark-held.Read, 0)
		runtime.streamFiles[stream], runtime.files[stream] = stream, handle
	}
	delete(runtime.files, object)
	if !client.javaFileHeld(handle) {
		delete(client.files, handle)
	}
	return 0, nil
}

// javaFileClosed reports whether an object is a File that is not open: one
// the title has closed. The table forgets a File at its close, and the object
// is still one by its class.
func (client *Client) javaFileClosed(object uint32) bool {
	if _, open := client.javaRuntimeState().files[object]; open {
		return false
	}
	class, known := client.javaClassOfObject(object)
	for ; known && class != nil; class = class.Super {
		if class.Name == javaFileClass {
			return true
		}
	}
	return false
}

// releaseJavaFileStream closes the handle a stream holds open after its File
// closed (see javaFileClose), at the stream's own close; the collector closes
// it the same way when it reclaims the stream. What waits in the handle is
// stored as a File's close stores it; a store that refuses leaves the handle
// open, with nothing holding it, for the ending and a quick step.
func (client *Client) releaseJavaFileStream(stream uint32) {
	runtime := client.javaRuntimeState()
	handle, held := runtime.files[stream]
	if !held {
		return
	}
	delete(runtime.files, stream)
	if !client.javaFileHeld(handle) {
		client.closeFileHandle(handle)
	}
}
