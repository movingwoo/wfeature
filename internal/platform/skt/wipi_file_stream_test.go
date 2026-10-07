package skt

import (
	"bytes"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func wipiStreamRuntime(t *testing.T, store backend.SaveStore) *Runtime {
	t.Helper()
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	return runtime
}

func wipiStreamFile(t *testing.T, runtime *Runtime, name string, mode int32) *jvm.Object {
	t.Helper()
	file, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", jvm.ReferenceValue(runtime.VM.NewString(name)), jvm.IntValue(mode))
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func wipiStreamCall(t *testing.T, runtime *Runtime, object *jvm.Object, method, descriptor string, arguments ...jvm.Value) jvm.Value {
	t.Helper()
	value, err := runtime.VM.InvokeVirtual(object, method, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s.%s%s: %v", object.ClassName, method, descriptor, err)
	}
	return value
}

func wipiOpenStream(t *testing.T, runtime *Runtime, file *jvm.Object, method, class string) *jvm.Object {
	t.Helper()
	value := wipiStreamCall(t, runtime, file, method, "()L"+class+";")
	stream, err := value.Reference()
	if err != nil || stream == nil {
		t.Fatalf("%s did not return a stream: %v", method, err)
	}
	return stream
}

func TestWIPIFileOutputStreamsPersistAndShareFileCursor(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	runtime := wipiStreamRuntime(t, store)
	file := wipiStreamFile(t, runtime, "progress", xFileRead|xFileWrite)
	output := wipiOpenStream(t, runtime, file, "openDataOutputStream", "java/io/DataOutputStream")
	wipiStreamCall(t, runtime, output, "write", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(nil)))
	if file.Native.(*xFileData).dirty {
		t.Fatal("empty stream write marked the file dirty")
	}
	wipiStreamCall(t, runtime, output, "writeInt", "(I)V", jvm.IntValue(0x12345678))
	if cursor, _ := wipiStreamCall(t, runtime, file, "tell", "()I").Int32(); cursor != 4 {
		t.Fatalf("stream did not advance File cursor: %d", cursor)
	}
	if _, exists := store.LoadSave("fs/progress"); exists {
		t.Fatal("unflushed stream write reached persistent storage")
	}
	wipiStreamCall(t, runtime, output, "flush", "()V")
	if data, ok := store.LoadSave("fs/progress"); !ok || !bytes.Equal(data, []byte{0x12, 0x34, 0x56, 0x78}) {
		t.Fatalf("flushed data=%x, exists=%v", data, ok)
	}
	wipiStreamCall(t, runtime, output, "write", "(I)V", jvm.IntValue(0x19a))
	wipiStreamCall(t, runtime, output, "close", "()V")
	if data, ok := store.LoadSave("fs/progress"); !ok || !bytes.Equal(data, []byte{0x12, 0x34, 0x56, 0x78, 0x9a}) {
		t.Fatalf("closed data=%x, exists=%v", data, ok)
	}
	if len(runtime.wipiFileStreams) != 0 {
		t.Fatal("closed output stream still occupies its file")
	}
	input := wipiOpenStream(t, runtime, file, "openDataInputStream", "java/io/DataInputStream")
	if value, _ := wipiStreamCall(t, runtime, input, "readInt", "()I").Int32(); value != 0x12345678 {
		t.Fatalf("file data stream read=%#x", value)
	}
	if cursor, _ := wipiStreamCall(t, runtime, file, "tell", "()I").Int32(); cursor != 5 {
		t.Fatalf("input stream changed the File cursor: %d", cursor)
	}
}

func TestWIPIFileInputStreamRetainsIndependentPositionAndMark(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("fs/input", []byte{0x80, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	runtime := wipiStreamRuntime(t, store)
	file := wipiStreamFile(t, runtime, "input", xFileRead)
	wipiStreamCall(t, runtime, file, "seek", "(I)V", jvm.IntValue(2))
	input := wipiOpenStream(t, runtime, file, "openInputStream", jvm.InputStreamClass)
	if value, _ := wipiStreamCall(t, runtime, input, "read", "()I").Int32(); value != 0x80 {
		t.Fatalf("initial input byte=%d, want unsigned first byte", value)
	}
	wipiStreamCall(t, runtime, input, "mark", "(I)V", jvm.IntValue(10))
	if value, _ := wipiStreamCall(t, runtime, input, "skip", "(J)J", jvm.LongValue(1<<62)).Int64(); value != 3 {
		t.Fatalf("skip beyond end=%d, want 3", value)
	}
	buffer := jvm.NewByteArray(make([]byte, 2))
	if value, _ := wipiStreamCall(t, runtime, input, "read", "([B)I", jvm.ReferenceValue(buffer)).Int32(); value != -1 {
		t.Fatalf("EOF=%d, want -1", value)
	}
	if value, _ := wipiStreamCall(t, runtime, input, "read", "([BII)I", jvm.ReferenceValue(buffer), jvm.IntValue(2), jvm.IntValue(0)).Int32(); value != 0 {
		t.Fatalf("empty EOF read=%d, want 0", value)
	}
	wipiStreamCall(t, runtime, input, "reset", "()V")
	if value, _ := wipiStreamCall(t, runtime, input, "available", "()I").Int32(); value != 3 {
		t.Fatalf("reset available=%d, want 3", value)
	}
	wipiStreamCall(t, runtime, input, "read", "([B)I", jvm.ReferenceValue(buffer))
	if data, _ := jvm.ByteArraySnapshot(buffer); !bytes.Equal(data, []byte{2, 3}) {
		t.Fatalf("reset data=%v", data)
	}
	stream := input.Native.(*wipiFileStreamData)
	if stream.file == file.Native.(*xFileData) || stream.file.name != "input" || stream.file.cursor != 3 || stream.mark != 1 {
		t.Fatal("input stream lost storage provenance or independent position")
	}
	if cursor, _ := wipiStreamCall(t, runtime, file, "tell", "()I").Int32(); cursor != 2 {
		t.Fatalf("input changed File cursor to %d", cursor)
	}
}

func TestWIPIFileStreamChecksOpenModesAndDuplicateDirections(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	file := wipiStreamFile(t, runtime, "modes", xFileRead|xFileWrite)
	input := wipiOpenStream(t, runtime, file, "openInputStream", jvm.InputStreamClass)
	output := wipiOpenStream(t, runtime, file, "openOutputStream", jvm.OutputStreamClass)
	for _, call := range []struct{ method, descriptor string }{
		{"openInputStream", "()Ljava/io/InputStream;"},
		{"openDataInputStream", "()Ljava/io/DataInputStream;"},
		{"openOutputStream", "()Ljava/io/OutputStream;"},
		{"openDataOutputStream", "()Ljava/io/DataOutputStream;"},
	} {
		if _, err := runtime.VM.InvokeVirtual(file, call.method, call.descriptor); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
			t.Errorf("duplicate %s error=%v, want IOException", call.method, err)
		}
	}
	for _, stream := range []*jvm.Object{input, output} {
		wipiStreamCall(t, runtime, stream, "close", "()V")
		wipiStreamCall(t, runtime, stream, "close", "()V")
	}
	if _, err := runtime.VM.InvokeVirtual(input, "read", "()I"); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("closed stream read error=%v", err)
	}
	output = wipiOpenStream(t, runtime, file, "openOutputStream", jvm.OutputStreamClass)
	if _, err := runtime.VM.InvokeVirtual(output, "write", "([BII)V", jvm.ReferenceValue(nil), jvm.IntValue(0), jvm.IntValue(0)); !runtime.VM.IsGuestException(err, "java/lang/NullPointerException") {
		t.Fatalf("null output array error=%v", err)
	}
	wipiStreamCall(t, runtime, file, "close", "()V")
	if _, err := runtime.VM.InvokeVirtual(file, "openInputStream", "()Ljava/io/InputStream;"); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("closed File stream error=%v", err)
	}
	if _, err := runtime.VM.InvokeVirtual(output, "write", "(I)V", jvm.IntValue(1)); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("closed File output error=%v", err)
	}
	writeOnly := wipiStreamFile(t, runtime, "output-only", xFileWrite)
	if _, err := runtime.VM.InvokeVirtual(writeOnly, "openInputStream", "()Ljava/io/InputStream;"); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("write-only File input error=%v", err)
	}
}

type refusingWIPIStreamStore struct {
	backend.SaveStore
	refuse bool
}

func (store *refusingWIPIStreamStore) StoreSave(name string, data []byte) error {
	if store.refuse {
		return errors.New("save write refused")
	}
	return store.SaveStore.StoreSave(name, data)
}

func TestWIPIFileStreamRetriesFailedFlush(t *testing.T) {
	store := &refusingWIPIStreamStore{SaveStore: backend.NewDirectorySaveStore(t.TempDir()), refuse: true}
	runtime := wipiStreamRuntime(t, store)
	file := wipiStreamFile(t, runtime, "retry", xFileWrite)
	output := wipiOpenStream(t, runtime, file, "openOutputStream", jvm.OutputStreamClass)
	wipiStreamCall(t, runtime, output, "write", "([B)V", jvm.ReferenceValue(jvm.NewByteArray([]byte{3, 5, 7})))
	for _, method := range []string{"flush", "close"} {
		if _, err := runtime.VM.InvokeVirtual(output, method, "()V"); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
			t.Fatalf("refused %s error=%v", method, err)
		}
		if !file.Native.(*xFileData).dirty || output.Native.(*wipiFileStreamData).closed {
			t.Fatalf("refused %s lost dirty data or closed the stream", method)
		}
	}
	store.refuse = false
	wipiStreamCall(t, runtime, output, "close", "()V")
	if data, exists := store.LoadSave("fs/retry"); !exists || !bytes.Equal(data, []byte{3, 5, 7}) {
		t.Fatalf("retry data=%v, exists=%v", data, exists)
	}
	if file.Native.(*xFileData).dirty || len(runtime.pendingSaves) != 0 {
		t.Fatal("successful stream retry retained an issued write")
	}
}

func TestWIPIFileInputStreamRetainsArchiveEntryProvenance(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	runtime.Archive.Entries["data.jar"] = makeJAR(t, map[string][]byte{"entry": {17, 19}})
	file, err := runtime.VM.NewObject(skvm.XFileClass, "(Ljava/lang/String;Ljava/lang/String;)V", jvm.ReferenceValue(runtime.VM.NewString("data.jar")), jvm.ReferenceValue(runtime.VM.NewString("entry")))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.wipiFileInputStream(runtime.VM, []jvm.Value{jvm.ReferenceValue(file)})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := value.Reference()
	stream := input.Native.(*wipiFileStreamData)
	if stream.file.archiveName != "data.jar" || stream.file.archiveEntry != "entry" || !bytes.Equal(stream.file.data, []byte{17, 19}) {
		t.Fatal("input stream lost its archive entry provenance")
	}
}
