package lgt

import (
	"encoding/binary"
	"testing"
)

// A class the compiler laid out itself declares no members: its record carries
// a pool pointer to its own dispatch table and an interface table saying where
// each interface's methods sit in it. The fixture builds one of those, because
// it is the only way a title's thread class is reached at all.
const (
	fixtureInterfaceTable uint32 = fixtureDataBase + 0x900
	fixtureInterfaceEntry uint32 = fixtureDataBase + 0x920
	fixtureVTable         uint32 = fixtureDataBase + 0x940
	fixtureRunnableName   uint32 = fixtureDataBase + 0x9f0
	fixtureRunBody        uint32 = 0x51000
	fixtureRunnableSlot   uint32 = 10
)

// writeJavaInterfaceFixture turns the record the class fixture plants into a
// laid-out one: a pool pointer, an interface table naming Runnable, and a
// dispatch table with a body at the slot the table gives.
func writeJavaInterfaceFixture(t *testing.T, client *Client) {
	t.Helper()
	write := func(at uint32, words ...uint32) {
		data := make([]byte, len(words)*4)
		for index, word := range words {
			binary.LittleEndian.PutUint32(data[index*4:], word)
		}
		if err := client.core.Memory().Write(at, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.core.Memory().Write(fixtureRunnableName,
		append([]byte(javaRunnableInterface), 0)); err != nil {
		t.Fatal(err)
	}
	// The record: a pool pointer where a record with member runs carries a
	// zero, no member runs at all, and the interface table the header names.
	write(fixtureClassHeader+javaClassPool, fixtureVTable)
	write(fixtureClassHeader+javaClassMethodRun, 0)
	write(fixtureClassHeader+javaClassFieldRun, 0)
	write(fixtureClassHeader+javaClassInterfaces, fixtureInterfaceTable)
	write(fixtureInterfaceTable, 1, fixtureInterfaceEntry)
	write(fixtureInterfaceEntry, fixtureRunnableName, fixtureRunnableSlot)
	// The dispatch table: the identity word, then the slots, with the class's
	// own `run` where the interface entry says.
	write(fixtureVTable, fixtureClassHandle)
	write(fixtureVTable+4+fixtureRunnableSlot*4, fixtureRunBody)
}

func TestJavaClassRecordCarriesItsInterfaceTable(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)
	writeJavaInterfaceFixture(t, client)

	record, err := client.readJavaClass(fixtureClassHandle, nil)
	if err != nil {
		t.Fatalf("readJavaClass() error = %v", err)
	}
	if record.Inline {
		t.Fatal("a laid-out record with no member runs was read as carrying members")
	}
	if record.VTable != fixtureVTable {
		t.Errorf("the record names %#x as its dispatch table, want %#x", record.VTable, fixtureVTable)
	}
	if len(record.Interfaces) != 1 {
		t.Fatalf("read %d interfaces, want 1", len(record.Interfaces))
	}
	if got := record.Interfaces[0]; got.Name != javaRunnableInterface || got.Slot != fixtureRunnableSlot {
		t.Errorf("interface = %+v", got)
	}
}

// The point of the table: a class with no member records still answers "where
// is your `run`", and the answer is a slot in its own dispatch table.
func TestJavaInterfaceMethodAnswersOutOfTheDispatchTable(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)
	writeJavaInterfaceFixture(t, client)

	record, err := client.readJavaClass(fixtureClassHandle, nil)
	if err != nil {
		t.Fatal(err)
	}
	class := &javaRuntimeClass{
		Name: record.Name, Handle: fixtureClassHandle, Record: record,
		VTable: record.VTable, Slots: uint32(record.VTableSize),
	}
	body, ok := client.javaInterfaceMethod(class, javaRunnableInterface, 0)
	if !ok || body != fixtureRunBody {
		t.Fatalf("javaInterfaceMethod() = %#x, %v, want %#x, true", body, ok, fixtureRunBody)
	}
	// An interface the class does not implement has no slot to answer with, and
	// neither has a method past the end of the table.
	if _, ok := client.javaInterfaceMethod(class, "java/lang/Comparable", 0); ok {
		t.Error("an interface the record does not name was answered anyway")
	}
	if _, ok := client.javaInterfaceMethod(class, javaRunnableInterface, class.Slots); ok {
		t.Error("a slot past the end of the dispatch table was answered anyway")
	}
}
