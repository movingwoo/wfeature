package lgt

import (
	"encoding/binary"
	"testing"
)

// The fixture lays the six member tables out the way a module does — one
// contiguous run, in the argument's own order — because that adjacency is what
// gives each table its length. A fixture that spaced them out would pass a
// reader that measured them any other way.
const (
	fixtureSurfaceStrings uint32 = fixtureDataBase + 0x800
	fixtureSurfaceTables  uint32 = fixtureDataBase + 0xa00
	fixtureSurfaceClasses uint32 = fixtureDataBase + 0xb00
	fixtureSurfaceOut     uint32 = fixtureDataBase + 0xb80
)

type fixtureSurface struct {
	arguments []uint32
	// where each output array landed, in the call's own order
	out [5]uint32
}

// writeJavaSurfaceFixture plants two platform classes: one with a virtual
// method and a constructor, one with a static field and a static method. The
// virtual table carries two entries past the platform's own runs, which is
// what a real title's application methods look like.
func writeJavaSurfaceFixture(t *testing.T, client *Client) fixtureSurface {
	t.Helper()
	pool := fixtureSurfaceStrings
	plant := func(text string) uint32 {
		at := pool
		if err := client.core.Memory().Write(at, append([]byte(text), 0)); err != nil {
			t.Fatal(err)
		}
		pool += uint32(len(text)+1+3) &^ 3
		return at
	}
	write := func(at uint32, words []uint32) {
		data := make([]byte, len(words)*4)
		for index, word := range words {
			binary.LittleEndian.PutUint32(data[index*4:], word)
		}
		if err := client.core.Memory().Write(at, data); err != nil {
			t.Fatal(err)
		}
	}

	// The member tables, back to back: static methods, methods, virtual
	// methods, static fields, fields.
	staticMethods := []uint32{
		0, 0, 0, 0, // the two unnamed entries every class's run opens with
		plant("<init>"), plant("()V"),
		0, 0, 0, 0,
		plant("abs"), plant("(I)I"),
	}
	methods := []uint32{plant("close"), plant("()V")}
	virtualMethods := []uint32{
		plant("getHeight"), plant("()I"),
		plant("a"), plant("(I)V"), // an application method, past the runs
	}
	staticFields := []uint32{plant("out"), plant("Ljava/io/PrintStream;")}
	fields := []uint32{plant("as"), plant("I")}

	at := fixtureSurfaceTables
	place := func(words []uint32) uint32 {
		address := at
		write(address, words)
		at += uint32(len(words)) * 4
		return address
	}
	staticMethodsAt := place(staticMethods)
	methodsAt := place(methods)
	virtualAt := place(virtualMethods)
	staticFieldsAt := place(staticFields)
	fieldsAt := place(fields)
	// Padding between the last table and whatever follows it, so the reader
	// has to stop at the end of the names rather than at the next address.
	place([]uint32{0xdeadbeef, 0xdeadbeef})

	write(fixtureSurfaceClasses, []uint32{
		2,
		plant("org/kwis/msp/lcdui/Card"), 0,
		0, 1 << 16, 1 << 16, 3 << 16,
		plant("java/lang/System"), 0,
		1 << 16, 0, 0, 3<<16 | 3,
	})

	surface := fixtureSurface{out: [5]uint32{
		fixtureSurfaceOut, fixtureSurfaceOut + 0x10, fixtureSurfaceOut + 0x20,
		fixtureSurfaceOut + 0x30, fixtureSurfaceOut + 0x40,
	}}
	surface.arguments = []uint32{
		fixtureSurfaceClasses, fieldsAt, staticFieldsAt, virtualAt, methodsAt, staticMethodsAt,
		surface.out[0], surface.out[1], surface.out[2], surface.out[3], surface.out[4],
	}
	return surface
}

func TestJavaSurfaceTablesAreMeasuredByTheirNeighbours(t *testing.T) {
	client := fixtureClient(t)
	fixture := writeJavaSurfaceFixture(t, client)

	surface, err := client.readJavaSurface(fixture.arguments)
	if err != nil {
		t.Fatalf("readJavaSurface() error = %v", err)
	}
	for _, want := range []struct {
		label string
		got   int
		count int
	}{
		{"fields", len(surface.Fields), 1},
		{"static fields", len(surface.StaticFields), 1},
		{"virtual methods", len(surface.VirtualMethods), 2},
		{"methods", len(surface.Methods), 1},
		{"static methods", len(surface.StaticMethods), 6},
	} {
		if want.got != want.count {
			t.Errorf("%s: read %d entries, want %d", want.label, want.got, want.count)
		}
	}
	if len(surface.Classes) != 2 {
		t.Fatalf("read %d classes", len(surface.Classes))
	}
	if surface.Classes[0].Name != "org/kwis/msp/lcdui/Card" {
		t.Errorf("first class %q", surface.Classes[0].Name)
	}
	if run := surface.Classes[1].StaticMethods; run.Start != 3 || run.Count != 3 {
		t.Errorf("the second class's static methods are %+v, want 3..5", run)
	}
	// The entry past both classes' runs is the application's own, and nothing
	// here owns it.
	if owner, known := surface.ownerOf(
		func(class javaAPIClass) javaRun { return class.VirtualMethods }, 1); known {
		t.Errorf("virtual entry 1 was claimed by %q", owner)
	}
}

// A run that does not fit the table it indexes means the tables were misread,
// and carrying on would answer indices for members that are not there.
func TestJavaSurfaceRefusesARunThatDoesNotFit(t *testing.T) {
	client := fixtureClient(t)
	fixture := writeJavaSurfaceFixture(t, client)
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, 9<<16)
	if err := client.core.Memory().Write(fixtureSurfaceClasses+4+12, word); err != nil {
		t.Fatal(err)
	}
	if _, err := client.readJavaSurface(fixture.arguments); err == nil {
		t.Fatal("a class claiming nine virtual methods of a table of two was accepted")
	}
}

// A platform class's virtual methods are answered with slots of its own — a
// class rooted at java/lang/Object numbers from 11 — and the static-method
// array is answered with addresses, one stub each, which names the method it
// stands for when the module calls it. An entry no platform class claims is
// left alone: it belongs to an application class, which is laid out later,
// against its own record.
func TestJavaSurfaceIsAnsweredWithSlotsAndAddresses(t *testing.T) {
	client := fixtureClient(t)
	fixture := writeJavaSurfaceFixture(t, client)
	surface, err := client.readJavaSurface(fixture.arguments)
	if err != nil {
		t.Fatalf("readJavaSurface() error = %v", err)
	}
	if err := client.linkJavaSurface(surface, newJavaLayout()); err != nil {
		t.Fatalf("linkJavaSurface() error = %v", err)
	}

	for _, want := range []struct {
		index uint32
		slot  uint16
	}{
		{0, javaObjectVTableSize}, // the platform's own getHeight()I
		{1, 0},                    // an application method, not this call's to answer
	} {
		slot, err := client.readHalfword(surface.VirtualMethodsOut + want.index*2)
		if err != nil {
			t.Fatal(err)
		}
		if slot != want.slot {
			t.Errorf("virtual entry %d was answered with slot %d, want %d", want.index, slot, want.slot)
		}
	}

	addresses := map[uint32]bool{}
	for index := uint32(0); index < uint32(len(surface.StaticMethods)); index++ {
		address, err := client.readWord(surface.StaticMethodsOut + index*4)
		if err != nil {
			t.Fatal(err)
		}
		if address == 0 {
			t.Fatalf("static method entry %d was answered with a null address", index)
		}
		if addresses[address] {
			t.Fatalf("static method entry %d shares an address with an earlier entry", index)
		}
		addresses[address] = true
	}

	link := &javaLink{surface: surface}
	if described := link.describeJavaStaticMethod(5); described !=
		"java/lang/System.abs(I)I (static method entry 5)" {
		t.Errorf("entry 5 is described as %q", described)
	}
	if described := link.describeJavaStaticMethod(0); described !=
		"org/kwis/msp/lcdui/Card.the unnamed entry (static method entry 0)" {
		t.Errorf("entry 0 is described as %q", described)
	}
}

// The string constants follow the class handles, and the list ends where the
// output array begins — the only thing that says how many there are.
func TestJavaClassListReadsTheStringConstants(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)

	table := fixtureClassTable
	out := table + 8 + 4 + 2*4
	header := make([]byte, 12)
	binary.LittleEndian.PutUint32(header[0:], 1)
	binary.LittleEndian.PutUint32(header[8:], fixtureClassHandle)
	if err := client.core.Memory().Write(table, header); err != nil {
		t.Fatal(err)
	}
	constants := []string{"ok", ""}
	pointers := make([]byte, len(constants)*4)
	at := fixtureSurfaceStrings + 0x180
	for index, text := range constants {
		binary.LittleEndian.PutUint32(pointers[index*4:], at)
		encoded := make([]byte, 2+len(text)*2)
		binary.LittleEndian.PutUint16(encoded, uint16(len(text)))
		for unit, symbol := range text {
			binary.LittleEndian.PutUint16(encoded[2+unit*2:], uint16(symbol))
		}
		if err := client.core.Memory().Write(at, encoded); err != nil {
			t.Fatal(err)
		}
		at += uint32(len(encoded)+3) &^ 3
	}
	if err := client.core.Memory().Write(table+8+4, pointers); err != nil {
		t.Fatal(err)
	}

	if err := client.takeJavaClassList(table, out); err != nil {
		t.Fatalf("takeJavaClassList() error = %v", err)
	}
	list := client.javaClasses
	if list == nil || len(list.Classes) != 1 {
		t.Fatalf("the class list was not kept: %+v", list)
	}
	if len(list.Strings) != 2 || list.Strings[0] != "ok" || list.Strings[1] != "" {
		t.Fatalf("string constants = %q", list.Strings)
	}
}
