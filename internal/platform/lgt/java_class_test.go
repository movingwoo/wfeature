package lgt

import (
	"context"
	"encoding/binary"
	"testing"
)

// The fixture builds one application class record the way a Java title's module
// lays one out, so the walk that finds the end of the member list is exercised
// against a record whose end is known rather than only against a real title.
const (
	fixtureClassPool   uint32 = fixtureDataBase + 0x400
	fixtureClassHeader uint32 = fixtureDataBase + 0x500
	fixtureClassHandle uint32 = fixtureClassHeader + javaClassHeader
	fixtureClassTable  uint32 = fixtureDataBase + 0x700
)

// writeJavaClassFixture plants a class record with two fields and one method,
// and returns the address one past its last member record.
func writeJavaClassFixture(t *testing.T, client *Client) uint32 {
	t.Helper()
	strings := map[string]uint32{}
	next := fixtureClassPool
	plant := func(text string) uint32 {
		if at, ok := strings[text]; ok {
			return at
		}
		at := next
		if err := client.core.Memory().Write(at, append([]byte(text), 0)); err != nil {
			t.Fatal(err)
		}
		next += uint32(len(text)+1+3) &^ 3
		strings[text] = at
		return at
	}
	name := plant("o")
	super := plant("org/kwis/msp/lcdui/Card")

	header := make([]uint32, javaClassHeader/4)
	header[0] = 0x21        // access flags
	header[2] = name        // char *name
	header[3] = 0           // the record pool pointer: zero means inline
	header[4] = super       // a platform class, so a name rather than a handle
	header[6] = 24          // instanceSize, in words
	header[9] = 26 << 16    // vtableSize in the high half
	header[17] = 0xfffffffe // the sentinel every record carries
	header[18] = 6          // the words of static storage the class object holds
	write := func(at uint32, words []uint32) {
		data := make([]byte, len(words)*4)
		for index, word := range words {
			binary.LittleEndian.PutUint32(data[index*4:], word)
		}
		if err := client.core.Memory().Write(at, data); err != nil {
			t.Fatal(err)
		}
	}
	write(fixtureClassHeader, header)

	handle := fixtureClassHandle
	body := []uint32{
		0, 0, fixtureClassHeader, // the back-pointer that pairs handle and header
		2,                                     // field count
		handle, plant("as"), plant("I"), 1, 0, // field records, five words each
		handle, plant("at"), plant("Z"), 1, 1, //
		1, // method count
		handle, plant("paint"), plant("(Lorg/kwis/msp/lcdui/Graphics;)V"), 2, 0, 0, 0,
	}
	write(handle, body)
	return handle + uint32(len(body))*4
}

// The counts are what say where a class's member list stops. Reading them wrong
// walks off the end of one class and into the next, which is why the walk is
// checked against the address the record was built to end at.
func TestJavaClassRecordEndsWhereItsCountsSay(t *testing.T) {
	client := fixtureClient(t)
	end := writeJavaClassFixture(t, client)

	class, err := client.readJavaClass(fixtureClassHandle, map[uint32]int{fixtureClassHandle: 0})
	if err != nil {
		t.Fatalf("readJavaClass() error = %v", err)
	}
	if !class.Inline {
		t.Fatal("the record was not read as carrying its members inline")
	}
	if class.End != end {
		t.Errorf("the member list ends at %#x, want %#x", class.End, end)
	}
	if class.Name != "o" || class.Super != "org/kwis/msp/lcdui/Card" || class.SuperHandle != 0 {
		t.Errorf("name %q super %q handle %#x", class.Name, class.Super, class.SuperHandle)
	}
	if len(class.Fields) != 2 || len(class.Methods) != 1 {
		t.Fatalf("read %d fields and %d methods, want 2 and 1", len(class.Fields), len(class.Methods))
	}
	if class.Fields[1].Name != "at" || class.Fields[1].Descriptor != "Z" || class.Fields[1].Index != 1 {
		t.Errorf("second field = %+v", class.Fields[1])
	}
	if class.Methods[0].Name != "paint" || class.Methods[0].Descriptor != "(Lorg/kwis/msp/lcdui/Graphics;)V" {
		t.Errorf("method = %+v", class.Methods[0])
	}
}

// A record whose header carries a vtable carries no member list at all: the
// compiler laid that class out itself. Reading it as a counted record would
// walk a vtable entry as a count, so the reader stops at the name instead.
func TestJavaClassWithAPoolPointerIsNotReadPastItsName(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, fixtureClassHandle+0x10)
	if err := client.core.Memory().Write(fixtureClassHeader+javaClassPool, word); err != nil {
		t.Fatal(err)
	}

	class, err := client.readJavaClass(fixtureClassHandle, nil)
	if err != nil {
		t.Fatalf("readJavaClass() error = %v", err)
	}
	if class.Inline {
		t.Fatal("a record with a pool pointer was read as inline")
	}
	if class.Name != "o" {
		t.Errorf("name %q, want the name to still be read", class.Name)
	}
	if len(class.Fields) != 0 || len(class.Methods) != 0 {
		t.Errorf("read %d fields and %d methods from a shape that is not understood", len(class.Fields), len(class.Methods))
	}
}

// A handle whose back-pointer does not name its header is not a class handle,
// and answering for it anyway would describe whatever the bytes happened to be.
func TestJavaClassRefusesAHandleThatDoesNotPointBack(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, fixtureClassHeader+4)
	if err := client.core.Memory().Write(fixtureClassHandle+8, word); err != nil {
		t.Fatal(err)
	}
	if _, err := client.readJavaClass(fixtureClassHandle, nil); err == nil {
		t.Fatal("a handle with a wrong back-pointer was accepted")
	}
}

// The class list is a count, a zero word, and that many handles.
func TestJavaClassListReadsEveryHandle(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)
	table := make([]byte, 12)
	binary.LittleEndian.PutUint32(table[0:], 1)
	binary.LittleEndian.PutUint32(table[8:], fixtureClassHandle)
	if err := client.core.Memory().Write(fixtureClassTable, table); err != nil {
		t.Fatal(err)
	}
	classes, err := client.readJavaClassList(fixtureClassTable)
	if err != nil {
		t.Fatalf("readJavaClassList() error = %v", err)
	}
	if len(classes) != 1 || classes[0].Handle != fixtureClassHandle {
		t.Fatalf("read %d classes: %+v", len(classes), classes)
	}
	if lines := describeJavaClasses(classes); len(lines) != 1 {
		t.Fatalf("described %d classes", len(lines))
	}
}

// The launcher class is found by walking the image for records rather than by
// looking in the class list, because the list does not hold it. A record is
// recognised by two words agreeing — the handle names its header, the header
// ends with the sentinel — and a name pointer that happens to sit at the same
// address is not one.
func TestJavaClassRecordIsFoundByName(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)

	handle, found := client.findJavaClassRecord("o")
	if !found {
		t.Fatal("the planted record was not found by name")
	}
	if handle != fixtureClassHandle {
		t.Errorf("found the record at %#x, want %#x", handle, fixtureClassHandle)
	}
	if _, found := client.findJavaClassRecord("org/kwis/msp/lcdui/Card"); found {
		t.Error("a superclass name that is only a string was read as a record")
	}
}

// A class's static fields live in its class object's data block, after the
// words the class object itself uses, and the module's own record says how
// many. A block sized to the class object's own use is a block the guest writes
// past — which is what overwrote a class object in two local titles.
func TestJavaClassObjectHoldsTheStaticsTheRecordDeclares(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)

	record, err := client.readJavaClass(fixtureClassHandle, nil)
	if err != nil {
		t.Fatalf("readJavaClass() error = %v", err)
	}
	if record.StaticWords != 6 {
		t.Fatalf("the record declares %d static words, want 6", record.StaticWords)
	}
	class, err := client.prepareJavaClass(context.Background(), nil, fixtureClassHandle)
	if err != nil {
		t.Fatalf("prepareJavaClass() error = %v", err)
	}
	data, err := client.readWord(class.Object + 8)
	if err != nil {
		t.Fatal(err)
	}
	// The last static the compiler can address is at word 4 + count, and a
	// store there must not land on whatever the arena handed out next.
	last := data + (javaClassDataWords+record.StaticWords-1)*4
	if err := client.writeWord(last, 0x5a5a5a5a); err != nil {
		t.Fatalf("the last static is not writable: %v", err)
	}
	if err := client.checkJavaClassObject(class); err != nil {
		t.Errorf("a store into the last static was read as a class object being overwritten: %v", err)
	}
}
