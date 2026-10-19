package ktf

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// aotClassFixture is where the parts of a class record written by
// writeAOTClassFixture are in guest memory.
type aotClassFixture struct {
	class, descriptor, vtable            uint32
	method, methodName, field, fieldName uint32
}

// newAOTFixtureRuntime answers a runtime over the synthetic client, ready to
// have class records written into its memory.
func newAOTFixtureRuntime(t *testing.T) (*initializationRuntime, *Client) {
	t.Helper()
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, client
}

// writeAOTClassFixture builds a class record named name in guest memory, with
// one method, (I)I run, one instance field, int value at offset 4, and a
// vtable of one word naming the method.
func writeAOTClassFixture(t *testing.T, runtime *initializationRuntime, client *Client, name string) aotClassFixture {
	t.Helper()
	allocate := func(address uint32, err error) uint32 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return address
	}
	var fixture aotClassFixture
	fixture.class = allocate(runtime.allocate(javaClassSize))
	className := allocate(runtime.allocateBytes([]byte(name + "\x00")))
	fixture.methodName = allocate(runtime.allocateBytes([]byte("\x00(I)I+run\x00")))
	fixture.fieldName = allocate(runtime.allocateBytes([]byte("\x00I+value\x00")))
	fixture.method = allocate(runtime.allocate(javaMethodSize))
	fixture.field = allocate(runtime.allocate(javaFieldSize))
	methodTable := allocate(runtime.allocateWords([]uint32{fixture.method, 0}))
	fieldTable := allocate(runtime.allocateWords([]uint32{fixture.field, 0}))
	fixture.vtable = allocate(runtime.allocateWords([]uint32{fixture.method}))
	fixture.descriptor = allocate(runtime.allocate(javaDescriptorSize))
	writeTestWords(t, client, fixture.method, []uint32{0x101001, fixture.class, 0, fixture.methodName, 0, 0x00010000, 0})
	writeTestWords(t, client, fixture.field, []uint32{0x0001, fixture.class, fixture.fieldName, 4})
	descriptorData := make([]byte, javaDescriptorSize)
	binary.LittleEndian.PutUint32(descriptorData[0:], className)
	binary.LittleEndian.PutUint32(descriptorData[12:], methodTable)
	binary.LittleEndian.PutUint32(descriptorData[20:], fieldTable)
	binary.LittleEndian.PutUint16(descriptorData[24:], 1)
	binary.LittleEndian.PutUint16(descriptorData[26:], 8)
	binary.LittleEndian.PutUint16(descriptorData[28:], 0x21)
	if err := client.Core().Memory().Write(fixture.descriptor, descriptorData); err != nil {
		t.Fatal(err)
	}
	classData := make([]byte, javaClassSize)
	binary.LittleEndian.PutUint32(classData[0:], fixture.class+4)
	binary.LittleEndian.PutUint32(classData[8:], fixture.descriptor)
	binary.LittleEndian.PutUint32(classData[12:], fixture.vtable)
	binary.LittleEndian.PutUint16(classData[16:], 1)
	if err := client.Core().Memory().Write(fixture.class, classData); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// registerAOTClassAt has the runtime register the class record at address as
// the guest's own registration call would.
func registerAOTClassAt(t *testing.T, runtime *initializationRuntime, address uint32) {
	t.Helper()
	aotRegistration(t, runtime, address)()
}

// aotRegistration answers a function that registers the class record at
// address, with the guest thread making the call built once, so that what the
// function allocates is what registering does.
func aotRegistration(t *testing.T, runtime *initializationRuntime, address uint32) func() {
	t.Helper()
	context_ := armcore.NewContext()
	context_.Registers[0] = address
	thread := armcore.NewThread(context_)
	return func() {
		t.Helper()
		if _, err := runtime.registerAOTClass(context.Background(), thread); err != nil {
			t.Fatalf("registerAOTClass(%#x) error = %v", address, err)
		}
	}
}

// A title registers the same class record again and again. When nothing it
// says has changed, registering it again compares the bytes it was read from
// and allocates nothing: no parsing, no copy, and no walk up to the ancestors
// registered the first time. When its vtable has changed, the class's vtable
// snapshot follows, as it did when every registration read the record whole.
func TestRegisteringAnUnchangedClassAgainReadsNoMoreThanItsBytes(t *testing.T) {
	runtime, client := newAOTFixtureRuntime(t)
	parent := writeAOTClassFixture(t, runtime, client, "game/Parent")
	fixture := writeAOTClassFixture(t, runtime, client, "game/Again")
	writeTestWords(t, client, fixture.descriptor+8, []uint32{parent.class})
	registerAOTClassAt(t, runtime, fixture.class)
	first, ok := client.JVM().AOTClass("game/Again")
	if !ok || first.Address != fixture.class || first.SuperName != "game/Parent" || len(first.VTable) != 1 {
		t.Fatalf("registered class = %+v (%t)", first, ok)
	}
	if !client.JVM().HasAOTClassAt(parent.class) {
		t.Fatal("the parent was not registered with the class")
	}
	register := aotRegistration(t, runtime, fixture.class)
	whole := testing.AllocsPerRun(20, func() {
		runtime.forgetAOTClassRead(fixture.class)
		register()
	})
	again := testing.AllocsPerRun(20, register)
	t.Logf("allocations: %.0f registering again, %.0f reading the record whole", again, whole)
	if again != 0 || whole == 0 {
		t.Fatalf("registering an unchanged class again allocated %.0f times, a whole read %.0f", again, whole)
	}
	if held, ok := client.JVM().AOTClass("game/Again"); !ok || !slices.Equal(held.VTable, first.VTable) {
		t.Fatalf("the class after registering it again = %+v", held)
	}

	writeTestWords(t, client, fixture.vtable, []uint32{0x123450})
	registerAOTClassAt(t, runtime, fixture.class)
	refreshed, ok := client.JVM().AOTClass("game/Again")
	if !ok || !slices.Equal(refreshed.VTable, []uint32{0x123450}) {
		t.Fatalf("the vtable snapshot after the title changed its vtable = %+v", refreshed.VTable)
	}
	// AllocsPerRun runs its function once more than it is asked to.
	if registered := runtime.callbacks.RegisteredClasses; registered != 1+21+21+1 {
		t.Fatalf("RegisteredClasses = %d, want every registration counted", registered)
	}
}

// A class is found unchanged only once its ancestors are all registered: a
// walk up the hierarchy that an unreadable record stopped is walked again with
// the next registration, which registers them when the record reads.
func TestAnAncestorThatWouldNotRegisterIsTriedAgain(t *testing.T) {
	runtime, client := newAOTFixtureRuntime(t)
	grand := writeAOTClassFixture(t, runtime, client, "game/Grand")
	parent := writeAOTClassFixture(t, runtime, client, "game/Middle")
	child := writeAOTClassFixture(t, runtime, client, "game/Child")
	writeTestWords(t, client, parent.descriptor+8, []uint32{grand.class})
	writeTestWords(t, client, child.descriptor+8, []uint32{parent.class})
	writeTestWords(t, client, grand.class, []uint32{0})
	registerAOTClassAt(t, runtime, child.class)
	if client.JVM().HasAOTClassAt(parent.class) || runtime.aotClassReads[child.class] != nil {
		t.Fatal("a class whose grandparent will not read registered its parent or was kept")
	}
	writeTestWords(t, client, grand.class, []uint32{grand.class + 4})
	registerAOTClassAt(t, runtime, child.class)
	if !client.JVM().HasAOTClassAt(parent.class) || !client.JVM().HasAOTClassAt(grand.class) || runtime.aotClassReads[child.class] == nil {
		t.Fatal("registering the class again did not register the ancestors that now read")
	}
}

// What decides where a name ends is the zero after it, so a name the title
// lengthens by writing over that zero alone is read again, whole.
func TestRegisteringAgainSeesANameLengthenedOverItsTerminator(t *testing.T) {
	runtime, client := newAOTFixtureRuntime(t)
	fixture := writeAOTClassFixture(t, runtime, client, "game/Renamed")
	registerAOTClassAt(t, runtime, fixture.class)
	terminator := fixture.methodName + uint32(len("\x00(I)I+run"))
	following := make([]byte, 1)
	if err := client.Core().Memory().Read(terminator+1, following); err != nil || following[0] != 0 {
		t.Fatalf("the byte after the name's terminator = %v (%v), want a zero to end the longer name", following, err)
	}
	if err := client.Core().Memory().Write(terminator, []byte("s")); err != nil {
		t.Fatal(err)
	}
	registerAOTClassAt(t, runtime, fixture.class)
	class, ok := client.JVM().AOTClass("game/Renamed")
	if !ok || len(class.Methods) != 1 || class.Methods[0].Name != "runs" {
		t.Fatalf("the class's methods after the name was lengthened = %+v", class.Methods)
	}
}

// A static field's last word is its value. A title storing to the static
// changes neither the class nor whether registering it again finds it
// unchanged; an instance field's offset changing still refreshes the class.
func TestStoringToAStaticLeavesItsClassUnchanged(t *testing.T) {
	runtime, client := newAOTFixtureRuntime(t)
	fixture := writeAOTClassFixture(t, runtime, client, "game/Statics")
	writeTestWords(t, client, fixture.field, []uint32{0x0008, fixture.class, fixture.fieldName, 0x11})
	registerAOTClassAt(t, runtime, fixture.class)
	class, ok := client.JVM().AOTClass("game/Statics")
	if !ok || len(class.Fields) != 1 || class.Fields[0].Offset != 0 {
		t.Fatalf("the static field as registered = %+v", class.Fields)
	}
	kept := runtime.aotClassReads[fixture.class]
	writeTestWords(t, client, fixture.field+12, []uint32{0x22})
	if allocations := testing.AllocsPerRun(5, aotRegistration(t, runtime, fixture.class)); allocations != 0 {
		t.Fatalf("registering the class again after a store to its static allocated %.0f times", allocations)
	}
	if runtime.aotClassReads[fixture.class] != kept {
		t.Fatal("a store to a static sent the class down a whole read")
	}

	writeTestWords(t, client, fixture.field, []uint32{0x0001, fixture.class, fixture.fieldName, 4})
	registerAOTClassAt(t, runtime, fixture.class)
	writeTestWords(t, client, fixture.field+12, []uint32{12})
	registerAOTClassAt(t, runtime, fixture.class)
	if class, _ := client.JVM().AOTClass("game/Statics"); len(class.Fields) != 1 || class.Fields[0].Offset != 12 {
		t.Fatalf("the instance field after its offset changed = %+v", class.Fields)
	}
}

// A kept read holds the bytes its spans cover in one buffer and nothing more,
// and what it costs is what the runtime charges.
func TestAKeptClassReadHoldsOnlyWhatItCovers(t *testing.T) {
	runtime, client := newAOTFixtureRuntime(t)
	for index := range 3 {
		fixture := writeAOTClassFixture(t, runtime, client, fmt.Sprintf("game/Kept%d", index))
		registerAOTClassAt(t, runtime, fixture.class)
	}
	charged := 0
	for address, read := range runtime.aotClassReads {
		if read.notes != nil || read.noted != nil || cap(read.spans) != len(read.spans) || len(read.spans) == 0 {
			t.Fatalf("the read kept for %#x holds %d notes, %d noted bytes and %d of %d spans", address, len(read.notes), len(read.noted), len(read.spans), cap(read.spans))
		}
		covered := 0
		for index, span := range read.spans {
			if cap(span.Data) != len(span.Data) {
				t.Fatalf("span %d of %#x can grow from %d to %d bytes", index, address, len(span.Data), cap(span.Data))
			}
			if index > 0 && uint64(read.spans[index-1].Address)+uint64(len(read.spans[index-1].Data)) >= uint64(span.Address) {
				t.Fatalf("spans %d and %d of %#x meet and were not joined", index-1, index, address)
			}
			covered += len(span.Data)
		}
		if want := covered + aotReadSpanCost*len(read.spans); read.retained != want {
			t.Fatalf("the read kept for %#x is charged %d, holds %d", address, read.retained, want)
		}
		charged += read.retained
	}
	if len(runtime.aotClassReads) != 3 || runtime.aotClassReadBytes != charged {
		t.Fatalf("%d reads kept, charged %d, holding %d", len(runtime.aotClassReads), runtime.aotClassReadBytes, charged)
	}
}

// The records a class record names are untrusted. One whose tables name a
// great many records it does not own, with long names, registers as the class
// it is, with none of them, and keeps nothing; reads that each fit but would
// together hold too much are kept only while they fit.
func TestUntrustedClassRecordsCannotMakeTheirReadsHoldUnboundedMemory(t *testing.T) {
	runtime, client := newAOTFixtureRuntime(t)
	owner := writeAOTClassFixture(t, runtime, client, "game/Owner")
	foreignName, err := runtime.allocateBytes([]byte("\x00()V+" + strings.Repeat("x", maxAOTNameBytes-6) + "\x00"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := runtime.allocateWords([]uint32{0x101001, owner.class, 0, foreignName, 0, 0x00010000, 0})
	if err != nil {
		t.Fatal(err)
	}
	table := slices.Repeat([]uint32{foreign}, 1<<11)
	tableAddress, err := runtime.allocateWords(append(table, 0))
	if err != nil {
		t.Fatal(err)
	}
	wide := writeAOTClassFixture(t, runtime, client, "game/Wide")
	writeTestWords(t, client, wide.descriptor+12, []uint32{tableAddress})
	registerAOTClassAt(t, runtime, wide.class)
	if class, ok := client.JVM().AOTClass("game/Wide"); !ok || len(class.Methods) != 0 {
		t.Fatalf("the class naming only another class's methods = %+v (%t)", class, ok)
	}
	if runtime.aotClassReads[wide.class] != nil || runtime.aotClassReadBytes != 0 {
		t.Fatalf("a read past its limit was kept: %d bytes charged", runtime.aotClassReadBytes)
	}

	// Distinct records, so that every class's read costs nearly its limit: a
	// record costs its 28 bytes, a word of the table, the name's tag and the
	// name with its terminator, each note aotReadSpanCost more.
	const recordCost = javaMethodSize + 4 + 1 + len("()V+") + 1000 + 1 + 4*aotReadSpanCost
	records := make([]uint32, 0, (maxAOTClassReadBytes-16<<10)/recordCost)
	for range cap(records) {
		name, err := runtime.allocateBytes([]byte("\x00()V+" + strings.Repeat("y", 1000) + "\x00"))
		if err != nil {
			t.Fatal(err)
		}
		record, err := runtime.allocateWords([]uint32{0x101001, owner.class, 0, name, 0, 0x00010000, 0})
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	sharedTable, err := runtime.allocateWords(append(records, 0))
	if err != nil {
		t.Fatal(err)
	}
	kept, notKept := 0, 0
	for index := range 2 * maxAOTClassReadsBytes / maxAOTClassReadBytes {
		class := writeAOTClassFixture(t, runtime, client, fmt.Sprintf("game/Shared%d", index))
		writeTestWords(t, client, class.descriptor+12, []uint32{sharedTable})
		registerAOTClassAt(t, runtime, class.class)
		if read := runtime.aotClassReads[class.class]; read != nil {
			kept++
			if read.retained > maxAOTClassReadBytes {
				t.Fatalf("a kept read holds %d bytes, past the %d one read may", read.retained, maxAOTClassReadBytes)
			}
		} else {
			notKept++
		}
		if _, ok := client.JVM().AOTClass(fmt.Sprintf("game/Shared%d", index)); !ok {
			t.Fatalf("class %d was not registered", index)
		}
	}
	t.Logf("%d reads kept, %d not, %d bytes charged", kept, notKept, runtime.aotClassReadBytes)
	if kept == 0 || notKept == 0 || runtime.aotClassReadBytes > maxAOTClassReadsBytes {
		t.Fatalf("%d reads kept and %d not, %d bytes charged against %d", kept, notKept, runtime.aotClassReadBytes, maxAOTClassReadsBytes)
	}
}
