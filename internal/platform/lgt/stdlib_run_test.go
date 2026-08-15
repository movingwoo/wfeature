package lgt

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The two addresses the fixture's runnable function uses. They sit past the end
// of the fixture's data section, inside the same page-rounded module mapping,
// which is executable the way a module's own code is.
const (
	fixtureRunFunction uint32 = fixtureDataBase + 0x200
	fixtureRunMarker   uint32 = fixtureDataBase + 0x300
)

// writeRunnableFunction plants a function that stores one word and returns, so
// a test can tell whether the platform ran it.
func writeRunnableFunction(t *testing.T, client *Client, value uint32) {
	t.Helper()
	words := []uint32{
		armLdrPC(0, 8),  // r0 = the marker address
		armLdrPC(1, 8),  // r1 = the value
		armStr(1, 0, 0), // *marker = value
		armBX(armcore.RegisterLR),
		fixtureRunMarker,
		value,
	}
	code := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(code[index*4:], word)
	}
	if err := client.core.Memory().Write(fixtureRunFunction, code); err != nil {
		t.Fatal(err)
	}
}

// The C library table's `0x424` is handed one function pointer into the module
// and runs it. Answering without running it looks harmless — the caller reads
// no result — and is not: in a Java title the function it points at is the only
// thing that hands the platform back the handle it answered the class-list call
// with, so skipping it would enter the application with the platform never told
// its classes were ready.
func TestRunFunctionRunsWhatItIsHanded(t *testing.T) {
	client := fixtureClient(t)
	const marker uint32 = 0xa5a50001
	writeRunnableFunction(t, client, marker)

	if result := callStdlib(t, client, stdlibRunFunction, fixtureRunFunction); result != 0 {
		t.Errorf("the slot answered %#x, want 0", result)
	}
	stored, err := client.readWord(fixtureRunMarker)
	if err != nil {
		t.Fatal(err)
	}
	if stored != marker {
		t.Fatalf("the function it was handed did not run: the marker holds %#x, want %#x", stored, marker)
	}
}

// A null pointer is nothing to run rather than a failure. The caller reads no
// result, so a refusal has no way to reach it and would only stop a title at a
// call it was never told anything about.
func TestRunFunctionAcceptsANullPointer(t *testing.T) {
	client := fixtureClient(t)

	thread := armcore.NewThread(armcore.NewContext())
	if err := thread.SetRegister(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := client.handleStdlibSVC(context.Background(), thread, stdlibRunFunction); err != nil {
		t.Fatalf("a null pointer was refused: %v", err)
	}
}
