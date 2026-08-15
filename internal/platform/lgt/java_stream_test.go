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
