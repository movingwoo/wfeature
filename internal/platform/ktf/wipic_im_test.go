package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func inputMethodCall(t *testing.T, runtime *initializationRuntime, function uint32) uint32 {
	t.Helper()
	result, err := runtime.handleWIPICTableCall(armcore.NewThread(armcore.NewContext()), wipicTableInputMethod, function)
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

	count := inputMethodCall(t, runtime, wipicIMGetSupportedModeCount)
	if count != uint32(len(inputModes)) {
		t.Fatalf("mode count = %d, want %d", count, len(inputModes))
	}
	table := inputMethodCall(t, runtime, wipicIMGetSupportedModes)
	if table == 0 {
		t.Fatal("the mode table was answered as a null pointer")
	}

	memory := client.core.Memory()
	pointers := make([]byte, int(count)*4)
	if err := memory.Read(table, pointers); err != nil {
		t.Fatal(err)
	}
	for index, want := range inputModes {
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
	if again := inputMethodCall(t, runtime, wipicIMGetSupportedModes); again != table {
		t.Fatalf("the mode table moved from %#x to %#x", table, again)
	}
}

// The three entries this table has not identified answer zero and keep being
// counted with their call site, which is how the next one gets named.
func TestInputMethodLeavesItsUnidentifiedEntriesStubbed(t *testing.T) {
	_, runtime := newTestRuntime(t)

	for _, function := range []uint32{0, 1, 2} {
		if result := inputMethodCall(t, runtime, function); result != 0 {
			t.Fatalf("function %d answered %#x, want 0", function, result)
		}
	}
	// The name carries the call site, which is the whole point of counting a
	// stub at all: a table number appears nowhere in the guest's own code.
	counts := runtime.diagnosticCounts()
	if counts["wipic stub table 4 function 0 @0x0"] == 0 {
		t.Fatalf("an unidentified entry was not counted with its call site: %v", counts)
	}
}
