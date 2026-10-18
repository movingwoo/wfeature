package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
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
// wrapped in a try region for.
func javaFileOpen(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	object, name, mode := arguments[0], arguments[1], arguments[2]
	path, ok := client.javaText(name)
	if !ok {
		return 0, fmt.Errorf("the name at %#x is not a string this platform built", name)
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
	handle := client.openFile(path, flag)
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

// javaFileExists and javaFileRemove are `org/kwis/msp/io/FileSystem`, which
// asks about a path rather than an open file. They read and write the same
// store the constructor above opens from, so a Java title and a Clet agree
// about what a title has saved.
func javaFileExists(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	path, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the name at %#x is not a string this platform built", arguments[0])
	}
	if _, found := client.readFile(path); found {
		return 1, nil
	}
	return 0, nil
}

func javaFileRemove(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	path, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the name at %#x is not a string this platform built", arguments[0])
	}
	client.removeFile(path)
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
func javaFileWrite(
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
	return uint32(client.transferFile(slotFsWrite, handle, block+offset, count)), nil
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
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	handle, file, err := client.javaFileHandle(arguments[0])
	if err != nil {
		return 0, err
	}
	_ = handle
	if !file.writable {
		return 0, fmt.Errorf("%s was opened read-only", file.name)
	}
	value := byte(arguments[1])
	// A cursor past the end fills the gap with zeros, as the array forms of
	// this call do. Nothing a Java title does moves the cursor there; a file
	// that is shorter after a quick load than it was before one does. A cursor
	// past maxFileBytes is one only a record can hold, and the write answers
	// -1 there, as the array forms do.
	if !file.extend(file.cursor + 1) {
		return ^uint32(0), nil
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
	file := arguments[0]
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
	class, err := client.preparePlatformJavaClass(javaByteSinkClass)
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
		client.logger.Debug("LGT java file output stream opened", "file", file, "stream", object)
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
// hands it to the File at once: see javaFileOpenOutputStream. A write the
// File cannot take, past the size a file may grow to or into a File opened to
// read, stops the run, as a write through such a File itself would fail.
func (client *Client) putJavaSink(sink uint32, held []byte) error {
	runtime := client.javaRuntimeState()
	runtime.sinks[sink] = held
	if _, bound := runtime.sinkFiles[sink]; !bound {
		return nil
	}
	return client.drainJavaSinkToFile(sink)
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
	if file.dirty && client.storeContent(file.name, file.fileContent) != nil {
		if client.logger != nil {
			client.logger.Debug("LGT java file close kept a write the store refused", "name", file.name, "file", object)
		}
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, fmt.Sprintf(": %s could not be stored", file.name))
	}
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
