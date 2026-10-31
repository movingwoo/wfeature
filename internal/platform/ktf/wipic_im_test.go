package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

func inputMethodCall(t *testing.T, runtime *initializationRuntime, function uint32) uint32 {
	t.Helper()
	result, err := runtime.HandleWIPICTableCall(armcore.NewThread(armcore.NewContext()), wipicrt.WIPICTableInputMethod, function)
	if err != nil {
		t.Fatalf("input method function %d: %v", function, err)
	}
	return result
}

// The two entries the title reaches have to agree with each other: the count is
// the length of the array, and the array holds the codes the title looks itself
// up in. A count that did not match the array would send the caller off the end
// of it.
func TestInputMethodAnswersItsModesAndTheirCount(t *testing.T) {
	client, runtime := newTestRuntime(t)

	count := inputMethodCall(t, runtime, wipicrt.WIPICIMGetSupportedModeCount)
	if count != uint32(len(wipicrt.InputModes)) {
		t.Fatalf("mode count = %d, want %d", count, len(wipicrt.InputModes))
	}
	table := inputMethodCall(t, runtime, wipicrt.WIPICIMGetSupportedModes)
	if table == 0 {
		t.Fatal("the mode table was answered as a null pointer")
	}

	memory := client.Core().Memory()
	pointers := make([]byte, int(count)*4)
	if err := memory.Read(table, pointers); err != nil {
		t.Fatal(err)
	}
	for index, want := range wipicrt.InputModes {
		address := binary.LittleEndian.Uint32(pointers[index*4:])
		text := make([]byte, len(want)+1)
		if err := memory.Read(address, text); err != nil {
			t.Fatal(err)
		}
		if string(text[:len(want)]) != want || text[len(want)] != 0 {
			t.Fatalf("mode %d reads %q, want %q terminated", index, text, want)
		}
	}

	// The caller keeps the pointer rather than owning it, so asking twice has
	// to answer the same table instead of allocating another one.
	if again := inputMethodCall(t, runtime, wipicrt.WIPICIMGetSupportedModes); again != table {
		t.Fatalf("the mode table moved from %#x to %#x", table, again)
	}
}

// Unknown extensions remain counted stubs.
func TestInputMethodLeavesUnknownExtensionsStubbed(t *testing.T) {
	_, runtime := newTestRuntime(t)

	for _, function := range []uint32{5, 6} {
		if result := inputMethodCall(t, runtime, function); result != 0 {
			t.Fatalf("function %d answered %#x, want 0", function, result)
		}
	}
	// The name carries the call site, which is the whole point of counting a
	// stub at all: a table number appears nowhere in the guest's own code.
	counts := runtime.DiagnosticCounts()
	if counts["wipic stub table 4 function 5 @0x0"] == 0 {
		t.Fatalf("an unidentified entry was not counted with its call site: %v", counts)
	}
}
