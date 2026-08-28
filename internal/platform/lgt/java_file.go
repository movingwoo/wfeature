package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `org/kwis/msp/io/File`, which is the same filesystem a Clet writes its save
// into: one store, one set of open handles, one set of mode flags. A Java title
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
// own flag set — the same numbers `MC_fsOpen` takes, which is why the C path's
// own reading of them is reused rather than repeated.
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
	handle := client.openFile(path, mode)
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
	if file.cursor == len(file.data) {
		file.data = append(file.data, value)
	} else {
		file.data[file.cursor] = value
	}
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
// than in memory. They are drained into the file on flush and on close, which
// is where a title expects its writes to have happened, and the file reaches
// the store on its own close the way every other write here does.
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
// one file rather than two views of it. The bytes are taken at open because a
// file here is wholly in memory and nothing can change it underneath: the store
// is written on close, not during.
//
// The specification's failures are a file that is not open and a second stream
// on one file, both `IOException` — the same pair `openOutputStream` refuses.
func javaFileOpenInputStream(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	file := arguments[0]
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
	class, err := client.preparePlatformJavaClass(javaInputStreamClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	rest := append([]byte(nil), open.data[min(open.cursor, len(open.data)):]...)
	runtime.streams[object] = &javaStream{Name: open.name, Data: rest}
	runtime.streamFiles[object] = file
	if client.logger != nil {
		client.logger.Debug("LGT java file input stream opened",
			"file", file, "stream", object, "bytes", len(rest))
	}
	return object, nil
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

// drainJavaSinkToFile moves what a bound sink holds into the file behind it.
// It is what makes a stream opened on a File write to the File rather than to
// memory, and it runs on flush, on close, and before the File itself closes —
// a title that writes through a stream and closes only the File must still find
// its bytes there.
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
	if end > len(open.data) {
		open.data = append(open.data, make([]byte, end-len(open.data))...)
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
func javaFileClose(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	handle, file, err := client.javaFileHandle(arguments[0])
	if err != nil {
		return 0, err
	}
	// A stream opened on this file is drained before the file goes: a title
	// that writes through the stream and closes only the File would otherwise
	// lose everything it wrote.
	runtime := client.javaRuntimeState()
	for sink, owner := range runtime.sinkFiles {
		if owner != arguments[0] {
			continue
		}
		if err := client.drainJavaSinkToFile(sink); err != nil {
			return 0, err
		}
		delete(runtime.sinkFiles, sink)
	}
	for stream, owner := range runtime.streamFiles {
		if owner != arguments[0] {
			continue
		}
		if err := client.syncJavaStreamToFile(stream); err != nil {
			return 0, err
		}
		delete(runtime.streamFiles, stream)
	}
	if file.dirty {
		client.writeFile(file.name, file.data)
	}
	delete(client.files, handle)
	delete(runtime.files, arguments[0])
	return 0, nil
}
