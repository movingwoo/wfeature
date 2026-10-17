package skt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func storeReviewInt(t *testing.T, value jvm.Value) int32 {
	t.Helper()
	number, err := value.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return number
}

// The WIPI File modes are an enumeration — 1 read-only, 2 append, 3 truncate,
// 4 read-write — and not the XFile bits the same handle uses underneath.
func TestWIPIFileModesFollowTheEnumeration(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("fs/save", []byte{1, 2, 3, 4, 5}); err != nil {
		t.Fatal(err)
	}
	runtime := wipiStreamRuntime(t, store)
	for name, want := range map[string]int32{"READ_ONLY": 1, "WRITE": 2, "WRITE_TRUNC": 3, "READ_WRITE": 4} {
		value, err := runtime.VM.StaticField(wipi.FileClass, name, "I")
		if err != nil || storeReviewInt(t, value) != want {
			t.Fatalf("File.%s = %v (%v), want %d", name, value, err, want)
		}
	}

	file := wipiStreamFile(t, runtime, "save", wipiFileReadWrite)
	buffer := jvm.NewByteArray(make([]byte, 2))
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, file, "read", "([B)I", jvm.ReferenceValue(buffer))); count != 2 {
		t.Fatalf("READ_WRITE read = %d, want the existing save", count)
	}
	wipiStreamCall(t, runtime, file, "write", "([B)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{9})))
	wipiStreamCall(t, runtime, file, "close", "()V")
	if data, _ := store.LoadSave("fs/save"); !bytes.Equal(data, []byte{1, 2, 9, 4, 5}) {
		t.Fatalf("READ_WRITE save = %v, want the third byte rewritten in place", data)
	}
	wipiStreamFile(t, runtime, "fresh", wipiFileReadWrite)

	file = wipiStreamFile(t, runtime, "save", wipiFileWriteTrunc)
	if size := storeReviewInt(t, wipiStreamCall(t, runtime, file, "sizeOf", "()I")); size != 0 {
		t.Fatalf("WRITE_TRUNC size = %d, want an emptied file", size)
	}
	wipiStreamCall(t, runtime, file, "write", "([B)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{7})))
	wipiStreamCall(t, runtime, file, "close", "()V")
	if data, _ := store.LoadSave("fs/save"); !bytes.Equal(data, []byte{7}) {
		t.Fatalf("WRITE_TRUNC save = %v, want only the new bytes", data)
	}

	file = wipiStreamFile(t, runtime, "save", wipiFileWrite)
	if position := storeReviewInt(t, wipiStreamCall(t, runtime, file, "tell", "()I")); position != 1 {
		t.Fatalf("WRITE position = %d, want the end of the file", position)
	}
	wipiStreamCall(t, runtime, file, "write", "([B)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{8})))
	wipiStreamCall(t, runtime, file, "close", "()V")
	if data, _ := store.LoadSave("fs/save"); !bytes.Equal(data, []byte{7, 8}) {
		t.Fatalf("WRITE save = %v, want the new bytes appended", data)
	}

	// Truncating is the open itself, so closing without a write still empties.
	file = wipiStreamFile(t, runtime, "save", wipiFileWriteTrunc)
	wipiStreamCall(t, runtime, file, "close", "()V")
	if data, ok := store.LoadSave("fs/save"); !ok || len(data) != 0 {
		t.Fatalf("WRITE_TRUNC close = %v (%v), want an empty file", data, ok)
	}

	for _, mode := range []int32{0, 5} {
		_, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", jvm.ReferenceValue(runtime.VM.NewString("save")), jvm.IntValue(mode))
		if !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
			t.Fatalf("mode %d error = %v, want IOException", mode, err)
		}
	}
	_, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", jvm.ReferenceValue(runtime.VM.NewString("missing")), jvm.IntValue(wipiFileReadOnly))
	if !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("READ_ONLY of a missing file error = %v, want IOException", err)
	}
}

// WIPI File.read answers -1 for an end reached before any byte, so a
// `!= -1` read loop ends. XFile keeps answering 0 there.
func TestWIPIFileReadAnswersMinusOneAtEnd(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("fs/eof", []byte{1}); err != nil {
		t.Fatal(err)
	}
	runtime := wipiStreamRuntime(t, store)
	file := wipiStreamFile(t, runtime, "eof", wipiFileReadOnly)
	buffer := jvm.NewByteArray(make([]byte, 4))
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, file, "read", "([B)I", jvm.ReferenceValue(buffer))); count != 1 {
		t.Fatalf("first read = %d, want 1", count)
	}
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, file, "read", "([B)I", jvm.ReferenceValue(buffer))); count != -1 {
		t.Fatalf("read([B) at end = %d, want -1", count)
	}
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, file, "read", "([BII)I", jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(4))); count != -1 {
		t.Fatalf("read([BII) at end = %d, want -1", count)
	}
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, file, "read", "([BII)I", jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(0))); count != 0 {
		t.Fatalf("zero-length read at end = %d, want 0", count)
	}

	xfile, err := runtime.VM.NewObject(skvm.XFileClass, "(Ljava/lang/String;I)V", jvm.ReferenceValue(runtime.VM.NewString("eof")), jvm.IntValue(xFileRead))
	if err != nil {
		t.Fatal(err)
	}
	wipiStreamCall(t, runtime, xfile, "read", "([BII)I", jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(4))
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, xfile, "read", "([BII)I", jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(4))); count != 0 {
		t.Fatalf("XFile read at end = %d, want 0", count)
	}
}

// A seek far past the end and a one-byte write must not grow the buffer to
// the seek position: the growth is refused like any other failed write.
func TestXFileWriteRefusesGrowthPastTheBound(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	// An existing empty file, so the open makes nothing that waits to be
	// stored and the refused write is the only thing that could.
	runtime.Archive.Entries["huge"] = nil
	file, err := runtime.VM.NewObject(skvm.XFileClass, "(Ljava/lang/String;I)V", jvm.ReferenceValue(runtime.VM.NewString("huge")), jvm.IntValue(xFileRead|xFileWrite))
	if err != nil {
		t.Fatal(err)
	}
	far := int32(xFileMaxBytes + 1)
	if position := storeReviewInt(t, wipiStreamCall(t, runtime, file, "seek", "(II)I", jvm.IntValue(far), jvm.IntValue(0))); position != far {
		t.Fatalf("seek = %d, want %d", position, far)
	}
	written := storeReviewInt(t, wipiStreamCall(t, runtime, file, "write", "([BII)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{1})), jvm.IntValue(0), jvm.IntValue(1)))
	if data := file.Native.(*xFileData).node; written != -1 || len(data.data) != 0 || data.dirty {
		t.Fatalf("write past the bound = %d with %d buffered bytes, want -1 and nothing", written, len(data.data))
	}
	// Growth up to the bound, and writes inside what is already there, work.
	wipiStreamCall(t, runtime, file, "seek", "(II)I", jvm.IntValue(xFileMaxBytes-1), jvm.IntValue(0))
	if written := storeReviewInt(t, wipiStreamCall(t, runtime, file, "write", "([BII)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{1})), jvm.IntValue(0), jvm.IntValue(1))); written != 1 {
		t.Fatalf("write up to the bound = %d, want 1", written)
	}
}

// A flush the store refused leaves the bytes dirty, so the close after it
// writes them again rather than reporting nothing to do.
func TestXFileFailedFlushIsRetriedByClose(t *testing.T) {
	store := &refusingWIPIStreamStore{SaveStore: backend.NewDirectorySaveStore(t.TempDir()), refuse: true}
	runtime := wipiStreamRuntime(t, store)
	file, err := runtime.VM.NewObject(skvm.XFileClass, "(Ljava/lang/String;I)V", jvm.ReferenceValue(runtime.VM.NewString("keep")), jvm.IntValue(xFileWrite))
	if err != nil {
		t.Fatal(err)
	}
	wipiStreamCall(t, runtime, file, "write", "([BII)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{4, 2})), jvm.IntValue(0), jvm.IntValue(2))
	wipiStreamCall(t, runtime, file, "flush", "()V")
	if !file.Native.(*xFileData).node.dirty {
		t.Fatal("refused flush marked the file clean")
	}
	store.refuse = false
	wipiStreamCall(t, runtime, file, "close", "()V")
	if data, ok := store.LoadSave("fs/keep"); !ok || !bytes.Equal(data, []byte{4, 2}) {
		t.Fatalf("close after a refused flush stored %v (%v)", data, ok)
	}
	if file.Native.(*xFileData).node.dirty || len(runtime.pendingSaves) != 0 {
		t.Fatal("successful retry left the write pending")
	}
}

// FileOutputStream(name) and (name, false) start the file over, as the
// java.io constructors they mirror do; (name, true) keeps it and appends.
func TestXCEFileOutputStreamTruncatesUnlessAppending(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	runtime := wipiStreamRuntime(t, store)
	write := func(descriptor string, arguments ...jvm.Value) {
		t.Helper()
		stream, err := runtime.VM.NewObject(skvm.FileOutputStreamClass, descriptor, arguments...)
		if err != nil {
			t.Fatal(err)
		}
		wipiStreamCall(t, runtime, stream, "write", "([BII)V", jvm.ReferenceValue(jvm.NewByteArray([]byte{6, 6})), jvm.IntValue(0), jvm.IntValue(2))
		wipiStreamCall(t, runtime, stream, "close", "()V")
	}
	name := jvm.ReferenceValue(runtime.VM.NewString("/s"))
	for _, form := range []struct {
		descriptor string
		arguments  []jvm.Value
	}{
		{"(Ljava/lang/String;)V", []jvm.Value{name}},
		{"(Ljava/lang/String;Z)V", []jvm.Value{name, jvm.IntValue(0)}},
	} {
		if err := store.StoreSave("fs/s", []byte{1, 2, 3, 4}); err != nil {
			t.Fatal(err)
		}
		write(form.descriptor, form.arguments...)
		if data, _ := store.LoadSave("fs/s"); !bytes.Equal(data, []byte{6, 6}) {
			t.Fatalf("FileOutputStream%s left %v, want only the new save", form.descriptor, data)
		}
	}
	write("(Ljava/lang/String;Z)V", name, jvm.IntValue(1))
	if data, _ := store.LoadSave("fs/s"); !bytes.Equal(data, []byte{6, 6, 6, 6}) {
		t.Fatalf("appending FileOutputStream left %v", data)
	}
}

// A zero-length read is 0 for java.io, not the end of the stream.
func TestXCEFileInputStreamZeroLengthReadIsZero(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("fs/in", []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	runtime := wipiStreamRuntime(t, store)
	stream, err := runtime.VM.NewObject(skvm.FileInputStreamClass, "(Ljava/lang/String;)V", jvm.ReferenceValue(runtime.VM.NewString("in")))
	if err != nil {
		t.Fatal(err)
	}
	empty := jvm.ReferenceValue(jvm.NewByteArray(nil))
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, stream, "read", "([B)I", empty)); count != 0 {
		t.Fatalf("read(new byte[0]) = %d, want 0", count)
	}
	buffer := jvm.ReferenceValue(jvm.NewByteArray(make([]byte, 4)))
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, stream, "read", "([BII)I", buffer, jvm.IntValue(1), jvm.IntValue(0))); count != 0 {
		t.Fatalf("read(b, 1, 0) = %d, want 0", count)
	}
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, stream, "read", "([BII)I", buffer, jvm.IntValue(0), jvm.IntValue(4))); count != 2 {
		t.Fatalf("read after the empty reads = %d, want the 2 stored bytes", count)
	}
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, stream, "read", "([BII)I", buffer, jvm.IntValue(0), jvm.IntValue(4))); count != -1 {
		t.Fatalf("read at end = %d, want -1", count)
	}
}
