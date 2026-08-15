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

// javaFileClose is `close()`, which is where a write reaches the store: the C
// path writes a dirty file back on its own close, and this is the same close.
func javaFileClose(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	handle, file, err := client.javaFileHandle(arguments[0])
	if err != nil {
		return 0, err
	}
	if file.dirty {
		client.writeFile(file.name, file.data)
	}
	delete(client.files, handle)
	delete(client.javaRuntimeState().files, arguments[0])
	return 0, nil
}
