package lgt

import (
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
// the bytes are there by the time the flush returns. This is the difference
// between `openOutputStream` and the `ByteArrayOutputStream` it is built on: a
// title that writes a save through the stream and then reads it back has to
// find it, and one that only closes the File must not lose what it wrote.
func TestFileOutputStreamWritesReachTheFile(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()

	name, err := client.newJavaString("save.dat")
	if err != nil {
		t.Fatal(err)
	}
	file := uint32(0x1000)
	if _, err := javaFileOpen(client, nil, nil, []uint32{file, name, fileOpenReadWrite, 1}); err != nil {
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
	if held := client.files[handle]; len(held.data) != 0 {
		t.Errorf("the file holds %d bytes before the flush, want 0", len(held.data))
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
	if _, err := javaFileOpen(client, nil, nil, []uint32{file, name, fileOpenReadWrite, 1}); err != nil {
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
