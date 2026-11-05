package lgt

import (
	"context"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A resource stream reads the archive's own files, one byte or a block at a
// time, and answers -1 at the end rather than a short read that never ends.
func TestJavaResourceStreamReadsAndEnds(t *testing.T) {
	client := fixtureClient(t)
	client.archive = &Archive{Resources: map[string][]byte{"img/all.mbm": {1, 2, 3, 4}}}
	name, err := client.newJavaString("/img/all.mbm")
	if err != nil {
		t.Fatal(err)
	}

	stream, err := javaGetResourceAsStream(client, nil, nil, []uint32{0, name})
	if err != nil {
		t.Fatalf("getResourceAsStream() error = %v", err)
	}
	if stream == 0 {
		t.Fatal("a resource that is in the archive answered null")
	}
	for _, want := range []uint32{1, 2} {
		value, err := javaStreamRead(client, nil, nil, []uint32{stream})
		if err != nil {
			t.Fatal(err)
		}
		if value != want {
			t.Errorf("read() = %d, want %d", value, want)
		}
	}

	array, err := client.javaArrayType(1, "B", 1)
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := client.allocateJavaArray(array.Object, 4)
	if err != nil {
		t.Fatal(err)
	}
	count, err := javaStreamReadArray(client, nil, nil, []uint32{stream, buffer})
	if err != nil {
		t.Fatalf("read([B) error = %v", err)
	}
	if count != 2 {
		t.Errorf("read([B) = %d, want the 2 bytes that were left", count)
	}
	block, err := client.readWord(buffer + 8)
	if err != nil {
		t.Fatal(err)
	}
	read := make([]byte, 4)
	if err := client.core.Memory().Read(block+4, read); err != nil {
		t.Fatal(err)
	}
	if read[0] != 3 || read[1] != 4 {
		t.Errorf("the array holds %v, want the last two bytes first", read)
	}
	if value, err := javaStreamRead(client, nil, nil, []uint32{stream}); err != nil || value != ^uint32(0) {
		t.Errorf("read() at the end = %#x (%v), want -1", value, err)
	}
	if count, err := javaStreamReadArray(client, nil, nil, []uint32{stream, buffer}); err != nil ||
		count != ^uint32(0) {
		t.Errorf("read([B) at the end = %#x (%v), want -1", count, err)
	}

	// A resource the archive does not have is null, not a failure: every call
	// site null-checks the answer, and the title's own handling is what should
	// run.
	missing, err := client.newJavaString("/img/none.mbm")
	if err != nil {
		t.Fatal(err)
	}
	if object, err := javaGetResourceAsStream(client, nil, nil, []uint32{0, missing}); err != nil ||
		object != 0 {
		t.Errorf("a missing resource answered %#x (%v), want null", object, err)
	}

	// After a close, a read says the stream was closed rather than that the
	// object was never one.
	if _, err := javaStreamClose(client, nil, nil, []uint32{stream}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaStreamRead(client, nil, nil, []uint32{stream}); err == nil {
		t.Error("a read after a close is not reported")
	}
}

// A block move counts elements, and the platform has to turn that into bytes
// with the width the array itself carries.
func TestJavaArrayCopyUsesTheArraysOwnElementWidth(t *testing.T) {
	client := fixtureClient(t)
	ints, err := client.javaArrayType(1, "I", 4)
	if err != nil {
		t.Fatal(err)
	}
	source, err := client.allocateJavaArray(ints.Object, 4)
	if err != nil {
		t.Fatal(err)
	}
	target, err := client.allocateJavaArray(ints.Object, 4)
	if err != nil {
		t.Fatal(err)
	}
	block, err := client.readWord(source + 8)
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range []uint32{10, 20, 30, 40} {
		if err := client.writeWord(block+4+uint32(index)*4, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := javaArrayCopy(client, nil, nil, []uint32{source, 1, target, 0, 3}); err != nil {
		t.Fatalf("arraycopy() error = %v", err)
	}
	into, err := client.readWord(target + 8)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []uint32{20, 30, 40, 0} {
		value, err := client.readWord(into + 4 + uint32(index)*4)
		if err != nil {
			t.Fatal(err)
		}
		if value != want {
			t.Errorf("element %d = %d, want %d", index, value, want)
		}
	}
	if _, err := javaArrayCopy(client, nil, nil, []uint32{source, 2, target, 0, 3}); err == nil {
		t.Error("a copy that runs off the end of the source is not reported")
	}
	bytes, err := client.javaArrayType(1, "B", 1)
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := client.allocateJavaArray(bytes.Object, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaArrayCopy(client, nil, nil, []uint32{source, 0, narrow, 0, 1}); err == nil {
		t.Error("a copy between two element widths is not reported")
	}
	// A String[] is as wide as an int[]; only the types tell them apart.
	names, err := client.newJavaStringArray([]string{"a", "b", "c", "d"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]uint32{{source, names}, {names, target}} {
		if _, err := javaArrayCopy(client, nil, nil, []uint32{pair[0], 0, pair[1], 0, 1}); err == nil {
			t.Errorf("a copy from %#x into %#x between references and numbers is not reported", pair[0], pair[1])
		}
	}
	if _, err := javaArrayCopy(client, nil, nil, []uint32{names, 0, names, 1, 3}); err != nil {
		t.Errorf("a copy between references: %v", err)
	}
}

// Skipping moves the cursor without reading and answers how far it actually
// moved, which is less than asked for at the end of the data.
func TestJavaStreamSkipMovesTheCursor(t *testing.T) {
	client := fixtureClient(t)
	client.archive = &Archive{Resources: map[string][]byte{"txt/a.dat": {1, 2, 3, 4, 5}}}
	name, err := client.newJavaString("/txt/a.dat")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := javaGetResourceAsStream(client, nil, nil, []uint32{0, name})
	if err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	moved, err := javaStreamSkip(client, nil, thread, []uint32{stream, 3, 0})
	if err != nil {
		t.Fatalf("javaStreamSkip() error = %v", err)
	}
	if moved != 3 {
		t.Errorf("skip(3) moved %d", moved)
	}
	value, err := javaStreamRead(client, nil, nil, []uint32{stream})
	if err != nil {
		t.Fatal(err)
	}
	if value != 4 {
		t.Errorf("the byte after skipping three is %d, want 4", value)
	}
	// Past the end it moves what is left rather than failing.
	moved, err = javaStreamSkip(client, nil, thread, []uint32{stream, 100, 0})
	if err != nil {
		t.Fatal(err)
	}
	if moved != 1 {
		t.Errorf("skipping past the end moved %d, want the one byte left", moved)
	}
}

// A stream opened on a File writes into the file rather than into memory, and
// the bytes are there as soon as the write returns: a write to the stream is
// a write to the file. This is the difference between `openOutputStream` and
// the `ByteArrayOutputStream` it is built on: a title that writes a save
// through the stream and then reads it back has to find it, and one that only
// closes the File must not lose what it wrote.
func TestFileOutputStreamWritesReachTheFile(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()

	name, err := client.newJavaString("save.dat")
	if err != nil {
		t.Fatal(err)
	}
	file := uint32(0x1000)
	if _, err := javaFileOpen(client, nil, nil, []uint32{file, name, 4, 1}); err != nil {
		t.Fatalf("File() error = %v", err)
	}
	stream, err := javaFileOpenOutputStream(client, nil, nil, []uint32{file})
	if err != nil {
		t.Fatalf("openOutputStream() error = %v", err)
	}
	if stream == 0 {
		t.Fatal("openOutputStream answered null on an open file")
	}
	// A second stream on the same file is the specification's other failure,
	// and it must not quietly hand out a second sink writing over the first.
	if _, err := javaFileOpenOutputStream(client, nil, nil, []uint32{file}); err == nil {
		t.Error("a second output stream on one file was allowed")
	}

	for _, value := range []uint32{7, 8, 9} {
		if _, err := javaByteSinkWrite(client, nil, nil, []uint32{stream, value}); err != nil {
			t.Fatal(err)
		}
	}
	handle := client.javaRuntimeState().files[file]
	if held := client.files[handle]; string(held.data) != string([]byte{7, 8, 9}) {
		t.Errorf("the file holds %v before the flush, want [7 8 9]", held.data)
	}
	if _, err := javaByteSinkFlush(client, nil, nil, []uint32{stream}); err != nil {
		t.Fatalf("flush() error = %v", err)
	}
	held := client.files[handle]
	if string(held.data) != string([]byte{7, 8, 9}) {
		t.Errorf("the file holds %v after the flush, want [7 8 9]", held.data)
	}
	// A flush that drained everything must not write the same block again.
	if _, err := javaByteSinkFlush(client, nil, nil, []uint32{stream}); err != nil {
		t.Fatal(err)
	}
	if held := client.files[handle]; len(held.data) != 3 {
		t.Errorf("a second flush wrote again: %v", held.data)
	}

	if _, err := javaFileWriteByte(client, nil, nil, []uint32{file, 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaFileClose(client, nil, nil, []uint32{file}); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	stored, ok := client.readFile("save.dat")
	if !ok {
		t.Fatal("nothing was stored for a file that was written and closed")
	}
	if string(stored) != string([]byte{7, 8, 9, 10}) {
		t.Errorf("the store holds %v, want [7 8 9 10]", stored)
	}
}

// The read half of the same pair. A stream opened on a File reads what is left
// of it from the file's own position, and the file follows what the stream
// consumed — a title that reads a header through the stream and the rest
// through `File.read` has to see one file rather than two views of it.
func TestFileInputStreamReadsFromTheFilesPosition(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	client.writeFile("save.dat", []byte{1, 2, 3, 4, 5})

	name, err := client.newJavaString("save.dat")
	if err != nil {
		t.Fatal(err)
	}
	file := uint32(0x1100)
	if _, err := javaFileOpen(client, nil, nil, []uint32{file, name, 4, 1}); err != nil {
		t.Fatalf("File() error = %v", err)
	}
	// One byte read through the File first, so the stream has to start at 1
	// rather than at the beginning.
	handle := client.javaRuntimeState().files[file]
	client.files[handle].cursor = 1

	stream, err := javaFileOpenInputStream(client, nil, nil, []uint32{file})
	if err != nil {
		t.Fatalf("openInputStream() error = %v", err)
	}
	if _, err := javaFileOpenInputStream(client, nil, nil, []uint32{file}); err == nil {
		t.Error("a second input stream on one file was allowed")
	}
	for _, want := range []uint32{2, 3} {
		value, err := javaStreamRead(client, nil, nil, []uint32{stream})
		if err != nil {
			t.Fatal(err)
		}
		if value != want {
			t.Errorf("read() = %d, want %d", value, want)
		}
	}
	if _, err := javaStreamClose(client, nil, nil, []uint32{stream}); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	// One byte was consumed before the stream and two through it.
	if got := client.files[handle].cursor; got != 3 {
		t.Errorf("the file is at %d after the stream closed, want 3", got)
	}
}

func TestJavaByteSinkStopsAtTheLargestArrayItCouldBecome(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	const sink = 0x7ffff000
	if _, err := javaByteSinkConstructor(client, nil, nil, []uint32{sink}); err != nil {
		t.Fatal(err)
	}
	array, err := client.newJavaByteArray([]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	writes := map[string]func() (uint32, error){
		"write(I)":    func() (uint32, error) { return javaByteSinkWrite(client, ctx, nil, []uint32{sink, 7}) },
		"write([BII)": func() (uint32, error) { return javaByteSinkWriteRange(client, ctx, nil, []uint32{sink, array, 0, 4}) },
		"write([B)":   func() (uint32, error) { return javaByteSinkWriteAll(client, ctx, nil, []uint32{sink, array}) },
		"writeInt":    func() (uint32, error) { return javaSinkAppend(4)(client, ctx, nil, []uint32{sink, 7}) },
		"writeLong":   func() (uint32, error) { return javaSinkWriteLong(client, ctx, nil, []uint32{sink, 7, 0}) },
		"writeUTF": func() (uint32, error) {
			return javaSinkWriteUTF(client, ctx, nil, []uint32{sink, newTestString(t, client, "ab")})
		},
	}
	for name, write := range writes {
		// Room for exactly one byte: a write of one fits, a wider one does not.
		runtime.sinks[sink] = make([]byte, maxJavaSinkBytes-1)
		_, err := write()
		if name == "write(I)" {
			if err != nil {
				t.Errorf("%s into the last free byte: %v", name, err)
			}
			if len(runtime.sinks[sink]) != maxJavaSinkBytes {
				t.Errorf("%s left %d bytes, want %d", name, len(runtime.sinks[sink]), maxJavaSinkBytes)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s past the bound was taken", name)
		}
		if len(runtime.sinks[sink]) != maxJavaSinkBytes-1 {
			t.Errorf("%s past the bound changed the sink to %d bytes", name, len(runtime.sinks[sink]))
		}
	}
	runtime.sinks[sink] = make([]byte, maxJavaSinkBytes)
	if _, err := javaByteSinkWrite(client, ctx, nil, []uint32{sink, 7}); err == nil {
		t.Error("a byte past a full sink was taken")
	}
}

// A ByteArrayInputStream over a window of an array reads the window, from its
// offset to the end it was given or the array's own, whichever comes first;
// a reset goes back to the offset. What the window refuses is what the KTF and
// SKT runtimes' constructor refuses.
func TestJavaByteArrayInputStreamOverAWindow(t *testing.T) {
	client := fixtureClient(t)
	key := "java/io/ByteArrayInputStream.<init>([BII)V"
	array, err := client.newJavaByteArray([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	if err != nil {
		t.Fatal(err)
	}
	reads := func(stream uint32, count int) []int32 {
		t.Helper()
		values := make([]int32, count)
		for index := range values {
			values[index] = int32(mustSlot(t, client, javaInputStreamClass, 10, stream))
		}
		return values
	}
	newStream := func(offset, length int32) uint32 {
		t.Helper()
		stream, err := newTestObject(t, client, "java/io/ByteArrayInputStream")
		if err != nil {
			t.Fatal(err)
		}
		mustJavaCall(t, client, key, stream, array, uint32(offset), uint32(length))
		return stream
	}
	window := newStream(2, 3)
	if got := mustSlot(t, client, javaInputStreamClass, 14, window); got != 3 {
		t.Errorf("available() of a window of three = %d", got)
	}
	if got := reads(window, 4); got[0] != 2 || got[1] != 3 || got[2] != 4 || got[3] != -1 {
		t.Errorf("a window of 2..4 read %v", got)
	}
	mustSlot(t, client, javaInputStreamClass, 17, window)
	if got := reads(window, 1); got[0] != 2 {
		t.Errorf("after reset() the window reads %d, want its offset's 2", got[0])
	}
	if got := reads(newStream(8, 5), 3); got[0] != 8 || got[1] != 9 || got[2] != -1 {
		t.Errorf("a window past the array's end read %v, want it cut at the end", got)
	}
	if got := reads(newStream(10, 0), 1); got[0] != -1 {
		t.Errorf("a window at the array's end read %v", got)
	}
	if got := reads(newStream(9, 0x7fffffff), 2); got[0] != 9 || got[1] != -1 {
		t.Errorf("a window whose end overflows read %v", got)
	}
	for _, probe := range []struct {
		name           string
		offset, length int32
	}{{"an offset past the array", 11, 0}, {"a negative offset", -1, 2}, {"a negative length", 0, -1}} {
		stream, err := newTestObject(t, client, "java/io/ByteArrayInputStream")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := javaCall(t, client, key, stream, array, uint32(probe.offset), uint32(probe.length)); thrownClass(err) != javaIndexClass {
			t.Errorf("%s = %v, want IndexOutOfBoundsException", probe.name, err)
		}
	}
	stream, err := newTestObject(t, client, "java/io/ByteArrayInputStream")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaCall(t, client, key, stream, 0, 0, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("a null array = %v, want NullPointerException", err)
	}
}

// ByteArrayOutputStream's toString is what has been written, decoded the way
// String(byte[]) decodes it, and a new String each time.
func TestJavaByteArrayOutputStreamToString(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	sink := newTestSink(t, client)
	client.javaRuntimeState().sinks[sink] = []byte{0xb0, 0xa1, 'A'}
	first := mustSlot(t, client, javaByteSinkClass, javaToStringSlot, sink)
	if got := mustText(t, client, first); got != "가A" {
		t.Errorf("toString() = %q, want \"가A\"", got)
	}
	if again := mustSlot(t, client, javaByteSinkClass, javaToStringSlot, sink); again == first {
		t.Error("two toStrings answered the same object")
	}
	if text, err := client.javaToStringOf(ctx, client.thread, sink); err != nil || text != "가A" {
		t.Errorf("String.valueOf(the sink) = %q, %v", text, err)
	}
}

// writeChars writes each code unit as two bytes, high first, with no length.
func TestJavaDataOutputStreamWriteChars(t *testing.T) {
	client := fixtureClient(t)
	sink := newTestSink(t, client)
	stream, err := newTestObject(t, client, javaDataOutputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/io/DataOutputStream.<init>(Ljava/io/OutputStream;)V", stream, sink)
	mustSlot(t, client, javaDataOutputStreamClass, 23, stream, newTestString(t, client, "A가"))
	mustSlot(t, client, javaDataOutputStreamClass, 23, stream, newTestString(t, client, ""))
	if got, want := client.javaRuntimeState().sinks[sink], []byte{0x00, 0x41, 0xac, 0x00}; string(got) != string(want) {
		t.Errorf("writeChars wrote % x, want % x", got, want)
	}
	if _, err := slotCall(t, client, javaDataOutputStreamClass, 23, stream, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("writeChars(null) = %v, want NullPointerException", err)
	}
}

// Every DataInputStream read that needs more than is left throws EOFException
// at the end of its stream, as the class library's and the KTF and SKT
// runtimes' do, so a title reading records until its file runs out reaches its
// own handler. The bytes before the end are taken — readFully leaves them in
// the array — and the stream answers -1 after.
func TestJavaDataInputStreamReadsThrowEOFExceptionAtTheEnd(t *testing.T) {
	client := fixtureClient(t)
	for _, probe := range []struct {
		name      string
		data      []byte
		slot      uint32
		arguments func() []uint32
	}{
		{"readBoolean", nil, 22, nil},
		{"readByte", nil, 23, nil},
		{"readUnsignedByte", nil, 24, nil},
		{"readShort", []byte{1}, 25, nil},
		{"readUnsignedShort", []byte{1}, 26, nil},
		{"readChar", []byte{1}, 27, nil},
		{"readInt", []byte{1, 2, 3}, 28, nil},
		{"readLong", []byte{1, 2, 3, 4, 5}, 29, nil},
		{"readUTF inside its length", []byte{0}, 32, nil},
		{"readUTF inside its text", []byte{0, 3, 'a'}, 32, nil},
		{"readFully(byte[])", []byte{'x'}, 19, func() []uint32 { return []uint32{newTestBytes(t, client, 2)} }},
		{"readFully(byte[], int, int)", []byte{'x'}, 20, func() []uint32 { return []uint32{newTestBytes(t, client, 4), 1, 3} }},
	} {
		stream := newTestDataStream(t, client, probe.data)
		arguments := []uint32{stream}
		if probe.arguments != nil {
			arguments = append(arguments, probe.arguments()...)
		}
		if _, err := slotCall(t, client, javaDataInputStreamClass, probe.slot, arguments...); thrownClass(err) != javaEOFClass {
			t.Errorf("%s at the end = %v, want EOFException", probe.name, err)
			continue
		}
		if got := mustSlot(t, client, javaInputStreamClass, 10, stream); int32(got) != -1 {
			t.Errorf("after %s at the end, read() = %d, want -1: what was left is taken", probe.name, int32(got))
		}
		if probe.slot == 19 || probe.slot == 20 {
			want := []byte{'x', 0}
			if probe.slot == 20 {
				want = []byte{0, 'x', 0, 0}
			}
			if got, _ := client.readJavaArrayBytes(arguments[1]); string(got) != string(want) {
				t.Errorf("%s at the end left % x in the array, want % x", probe.name, got, want)
			}
		}
	}
	// skipBytes is no read that needs a count: it answers how far it got.
	stream := newTestDataStream(t, client, []byte{1})
	if got := mustSlot(t, client, javaDataInputStreamClass, 21, stream, 5); got != 1 {
		t.Errorf("skipBytes(5) over one byte = %d, want 1", got)
	}
	if got := mustSlot(t, client, javaDataInputStreamClass, 21, stream, 5); got != 0 {
		t.Errorf("skipBytes(5) at the end = %d, want 0", got)
	}
}

// newTestBytes is a zeroed byte array of a length.
func newTestBytes(t *testing.T, client *Client, length int) uint32 {
	t.Helper()
	array, err := client.newJavaByteArray(make([]byte, length))
	if err != nil {
		t.Fatal(err)
	}
	return array
}
