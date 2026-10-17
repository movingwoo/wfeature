package skt

import (
	"fmt"
	"sync"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/jvm"
)

const (
	wipiFileInputStreamClass  = "net/wfeature/WIPIFileInputStream"
	wipiFileOutputStreamClass = "net/wfeature/WIPIFileOutputStream"
)

// A file stream retains its storage provenance rather than hiding a copy in a
// guest byte array. An input stream is a handle of its own on the File's file,
// with its own read position, so it reads what any handle writes; an output
// stream is the File's handle. A checkpoint can therefore reconnect both to
// the current save file.
type wipiFileStreamData struct {
	mu     sync.Mutex
	file   *xFileData
	mark   int
	closed bool
}

func (runtime *Runtime) registerWIPIFileStreams() error {
	for _, stream := range []struct {
		class, parent string
		methods       []nativeRegistration
	}{
		{wipiFileInputStreamClass, jvm.InputStreamClass, []nativeRegistration{
			{wipiFileInputStreamClass, "read", "()I", runtime.wipiStreamReadByte},
			{wipiFileInputStreamClass, "read", "([BII)I", runtime.wipiStreamRead},
			{wipiFileInputStreamClass, "available", "()I", runtime.wipiStreamAvailable},
			{wipiFileInputStreamClass, "skip", "(J)J", runtime.wipiStreamSkip},
			{wipiFileInputStreamClass, "mark", "(I)V", runtime.wipiStreamMark},
			{wipiFileInputStreamClass, "reset", "()V", runtime.wipiStreamReset},
			{wipiFileInputStreamClass, "markSupported", "()Z", func(*jvm.VM, []jvm.Value) (jvm.Value, error) { return jvm.IntValue(1), nil }},
			{wipiFileInputStreamClass, "close", "()V", runtime.wipiStreamClose},
		}},
		{wipiFileOutputStreamClass, jvm.OutputStreamClass, []nativeRegistration{
			{wipiFileOutputStreamClass, "write", "(I)V", runtime.wipiStreamWriteByte},
			{wipiFileOutputStreamClass, "write", "([BII)V", runtime.wipiStreamWrite},
			{wipiFileOutputStreamClass, "flush", "()V", runtime.wipiStreamFlush},
			{wipiFileOutputStreamClass, "close", "()V", runtime.wipiStreamClose},
		}},
	} {
		definition := jvm.ClassDefinition{Name: stream.class, SuperName: stream.parent, Access: jvm.AccessPublic | jvm.AccessFinal}
		for _, method := range stream.methods {
			definition.Methods = append(definition.Methods, jvm.MethodDefinition{Name: method.name, Descriptor: method.descriptor, Access: jvm.AccessPublic})
		}
		if err := runtime.VM.DefineClass(definition); err != nil {
			return err
		}
		for _, method := range stream.methods {
			if err := runtime.registerNative(method.class, method.name, method.descriptor, method.method); err != nil {
				return err
			}
		}
	}
	return nil
}

func (runtime *Runtime) newWIPIFileStream(arguments []jvm.Value, output bool) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	class := wipiFileInputStreamClass
	if output {
		class = wipiFileOutputStreamClass
	}
	runtime.wipiStreamMu.Lock()
	defer runtime.wipiStreamMu.Unlock()
	for object, owner := range runtime.wipiFileStreams {
		if owner != file || object.ClassName != class {
			continue
		}
		if stream, ok := object.Native.(*wipiFileStreamData); ok {
			stream.mu.Lock()
			closed := stream.closed
			stream.mu.Unlock()
			if !closed {
				return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "file already has a stream in this direction")
			}
		}
	}
	runtime.fileMu.Lock()
	if !file.open || output && !file.writable || !output && file.mode&xFileRead == 0 {
		runtime.fileMu.Unlock()
		return jvm.VoidValue(), newGuestException(jvm.IOExceptionClass, "file is not open for this stream direction")
	}
	data := &wipiFileStreamData{file: file}
	if !output {
		data.file = &xFileData{name: file.name, archiveName: file.archiveName, archiveEntry: file.archiveEntry,
			mode: xFileRead, open: true, node: file.node}
	}
	runtime.fileMu.Unlock()
	object := &jvm.Object{ClassName: class, Fields: make(map[string]jvm.Value), Native: data}
	if runtime.wipiFileStreams == nil {
		runtime.wipiFileStreams = make(map[*jvm.Object]*xFileData)
	}
	runtime.wipiFileStreams[object] = file
	return jvm.ReferenceValue(object), nil
}

func wipiStreamArgument(arguments []jvm.Value) (*jvm.Object, *wipiFileStreamData, error) {
	object, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, nil, err
	}
	if object == nil {
		return nil, nil, newGuestException("java/lang/NullPointerException", "file stream is null")
	}
	stream, ok := object.Native.(*wipiFileStreamData)
	if !ok || stream == nil || stream.file == nil {
		return nil, nil, fmt.Errorf("receiver is not a WIPI file stream")
	}
	return object, stream, nil
}

func (runtime *Runtime) wipiStreamOpen(stream *wipiFileStreamData) error {
	runtime.fileMu.Lock()
	open := stream.file.open
	runtime.fileMu.Unlock()
	if stream.closed || !open {
		return newGuestException(jvm.IOExceptionClass, "file stream is closed")
	}
	return nil
}

func wipiStreamFileArguments(arguments []jvm.Value, file *xFileData) []jvm.Value {
	forwarded := append([]jvm.Value(nil), arguments...)
	forwarded[0] = jvm.ReferenceValue(&jvm.Object{ClassName: wipi.FileClass, Native: file})
	return forwarded
}

func (runtime *Runtime) wipiStreamReadByte(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, stream, err := wipiStreamArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if err := runtime.wipiStreamOpen(stream); err != nil {
		return jvm.VoidValue(), err
	}
	file := stream.file
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	data := file.contents()
	if file.cursor >= len(data) {
		return jvm.IntValue(-1), nil
	}
	value := data[file.cursor]
	file.cursor++
	return jvm.IntValue(int32(value)), nil
}

func (runtime *Runtime) wipiStreamRead(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, stream, err := wipiStreamArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if err := runtime.wipiStreamOpen(stream); err != nil {
		return jvm.VoidValue(), err
	}
	value, err := runtime.xFileRead(vm, wipiStreamFileArguments(arguments, stream.file))
	if count, _ := value.Int32(); err == nil && count == 0 {
		if length, err := intArgument(arguments, 3); err == nil && length > 0 {
			return jvm.IntValue(-1), nil
		}
	}
	return value, err
}

func (runtime *Runtime) wipiStreamAvailable(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, stream, err := wipiStreamArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if err := runtime.wipiStreamOpen(stream); err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.xFileAvailable(vm, wipiStreamFileArguments(arguments, stream.file))
}

func (runtime *Runtime) wipiStreamSkip(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, stream, err := wipiStreamArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	count, err := longArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if err := runtime.wipiStreamOpen(stream); err != nil {
		return jvm.VoidValue(), err
	}
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	skipped := min(max(count, 0), int64(max(len(stream.file.contents())-stream.file.cursor, 0)))
	stream.file.cursor += int(skipped)
	return jvm.LongValue(skipped), nil
}

func (runtime *Runtime) wipiStreamMark(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, stream, err := wipiStreamArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if !stream.closed {
		runtime.fileMu.Lock()
		stream.mark = stream.file.cursor
		runtime.fileMu.Unlock()
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) wipiStreamReset(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, stream, err := wipiStreamArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if err := runtime.wipiStreamOpen(stream); err != nil {
		return jvm.VoidValue(), err
	}
	runtime.fileMu.Lock()
	stream.file.cursor = stream.mark
	runtime.fileMu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) wipiStreamWriteByte(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	value, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.wipiStreamWrite(vm, []jvm.Value{arguments[0], jvm.ReferenceValue(jvm.NewByteArray([]byte{byte(value)})), jvm.IntValue(0), jvm.IntValue(1)})
}

func (runtime *Runtime) wipiStreamWrite(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, stream, err := wipiStreamArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	array, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "stream output array is null")
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if err := runtime.wipiStreamOpen(stream); err != nil {
		return jvm.VoidValue(), err
	}
	if length, err := intArgument(arguments, 3); err == nil && length == 0 {
		_, err := recordBytesArgument(arguments, 1, 2, 3)
		return jvm.VoidValue(), err
	}
	value, err := runtime.xFileWrite(vm, wipiStreamFileArguments(arguments, stream.file))
	if count, _ := value.Int32(); err == nil && count < 0 {
		err = newGuestException(jvm.IOExceptionClass, "file stream is not writable")
	}
	return jvm.VoidValue(), err
}

func (runtime *Runtime) wipiStreamFlush(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, stream, err := wipiStreamArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if err := runtime.wipiStreamOpen(stream); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.persistWIPIFileStream(stream.file)
}

func (runtime *Runtime) wipiStreamClose(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, stream, err := wipiStreamArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream.mu.Lock()
	if !stream.closed && object.ClassName == wipiFileOutputStreamClass {
		if err := runtime.persistWIPIFileStream(stream.file); err != nil {
			stream.mu.Unlock()
			return jvm.VoidValue(), err
		}
	}
	stream.closed = true
	stream.mu.Unlock()
	runtime.wipiStreamMu.Lock()
	delete(runtime.wipiFileStreams, object)
	runtime.wipiStreamMu.Unlock()
	return jvm.VoidValue(), nil
}

// persistWIPIFileStream stores the file an output stream writes, as every
// handle on the path has left it. Unlike XFile's flush, the stream's flush and
// close declare IOException, and a refusal is reported through it; the file
// stays dirty, so a retry, a quick save or the end of the session cannot lose
// it.
func (runtime *Runtime) persistWIPIFileStream(file *xFileData) error {
	runtime.fileMu.Lock()
	defer runtime.fileMu.Unlock()
	if err := runtime.storeXFileLocked(file.node); err != nil {
		return newGuestException(jvm.IOExceptionClass, err.Error())
	}
	return nil
}
