package ktf

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// callocThroughSVC and freeThroughSVC drive MC_knlCalloc and MC_knlFree the way
// a game does — through the supervisor call, with the argument in r0 — because
// the defect these tests pin was in the dispatch and not in the arena.
func callocThroughSVC(t *testing.T, runtime *initializationRuntime, size uint32) uint32 {
	t.Helper()
	callContext := armcore.NewContext()
	callContext.Registers[0] = size
	address, err := runtime.handleWIPICCall(armcore.NewThread(callContext), wipicKernelCalloc)
	if err != nil {
		t.Fatalf("MC_knlCalloc(%d) error = %v", size, err)
	}
	return address
}

func freeThroughSVC(t *testing.T, runtime *initializationRuntime, id uint32) {
	t.Helper()
	callContext := armcore.NewContext()
	callContext.Registers[0] = id
	if _, err := runtime.handleWIPICCall(armcore.NewThread(callContext), wipicKernelFree); err != nil {
		t.Fatalf("MC_knlFree(%#x) error = %v", id, err)
	}
}

// The drawing loop of one local title allocates a working buffer, copies into
// it and frees it, sixteen times a tick. While MC_knlFree did nothing that loop
// walked the whole 64 MiB arena and the run died on a screen it was only
// sitting on, so what this pins is that the loop is flat: repeating it does not
// raise what the arena has handed out.
func TestWIPICFreeReturnsTheBlockToTheArena(t *testing.T) {
	_, runtime := newTestRuntime(t)

	first := callocThroughSVC(t, runtime, 2048)
	freeThroughSVC(t, runtime, first)
	settled := runtime.arena.used()

	for round := 0; round < 4096; round++ {
		address := callocThroughSVC(t, runtime, 2048)
		if address != first {
			t.Fatalf("round %d allocated %#x, want the released block %#x", round, address, first)
		}
		freeThroughSVC(t, runtime, address)
		if used := runtime.arena.used(); used != settled {
			t.Fatalf("round %d left %d bytes handed out, want %d", round, used, settled)
		}
	}
	if live := len(runtime.wipicAllocations); live != 0 {
		t.Fatalf("%d allocations are still recorded as live", live)
	}
}

// MC_knlCalloc promises zero-filled memory. That was free while the arena only
// ever bumped a cursor, and stops being free the moment a released block is
// handed out again.
func TestWIPICCallocClearsAReusedBlock(t *testing.T) {
	client, runtime := newTestRuntime(t)

	const size = 64
	first := callocThroughSVC(t, runtime, size)
	dirty := bytes.Repeat([]byte{0xa5}, size)
	if err := client.core.Memory().Write(first+wipicAllocationOverhead, dirty); err != nil {
		t.Fatal(err)
	}
	freeThroughSVC(t, runtime, first)

	again := callocThroughSVC(t, runtime, size)
	if again != first {
		t.Fatalf("second allocation at %#x, want the released block %#x", again, first)
	}
	payload := readTestBytes(t, client, again+wipicAllocationOverhead, size)
	for index, value := range payload {
		if value != 0 {
			t.Fatalf("reused payload byte %d = %#x, want a zero fill", index, value)
		}
	}
}

// Freeing something this platform never handed out is the program's own error.
// A double free arrives here as an identifier that has already been dropped,
// and releasing that block a second time would hand one address to two owners.
func TestWIPICFreeIgnoresAnIdentifierItDidNotIssue(t *testing.T) {
	_, runtime := newTestRuntime(t)

	live := callocThroughSVC(t, runtime, 256)
	before := runtime.arena.used()

	freeThroughSVC(t, runtime, 0)
	freeThroughSVC(t, runtime, live+4)
	freeThroughSVC(t, runtime, platformDataBase+uint32(platformDataSize)-4)
	if used := runtime.arena.used(); used != before {
		t.Fatalf("used() = %d after refused frees, want %d", used, before)
	}
	if _, ok := runtime.wipicAllocations[live]; !ok {
		t.Fatal("a refused free dropped the live allocation")
	}

	freeThroughSVC(t, runtime, live)
	released := runtime.arena.used()
	freeThroughSVC(t, runtime, live)
	if used := runtime.arena.used(); used != released {
		t.Fatalf("a double free released the block twice: used() = %d, want %d", used, released)
	}
}

// Transparency is recorded against the guest address of the framebuffer or
// image that lives in a block. A released address is handed out again, so its
// mask has to go with the block rather than wait for a new tenant to inherit
// it and lose the pixels the new tenant's own encoding drew.
func TestWIPICFreeDropsTheBlocksTransparency(t *testing.T) {
	_, runtime := newTestRuntime(t)

	handle := callocThroughSVC(t, runtime, 128)
	runtime.setFramebufferOpacity(handle, &imageOpacity{width: 1, height: 1, opaque: []bool{false}})
	freeThroughSVC(t, runtime, handle)
	if opacity := runtime.framebufferOpacityOf(handle); opacity != nil {
		t.Fatal("a released block kept its transparency mask")
	}

	again := callocThroughSVC(t, runtime, 128)
	if again != handle {
		t.Fatalf("second allocation at %#x, want the released block %#x", again, handle)
	}
	if opacity := runtime.framebufferOpacityOf(again); opacity != nil {
		t.Fatal("a reused block inherited the previous tenant's transparency")
	}
}

// MC_knlGetFreeMemory is the number a game sizes its own caches from. It reads
// the arena, so a free that did not release left the game watching its memory
// drain with nothing holding it.
func TestWIPICFreeShowsInTheGuestsFreeMemory(t *testing.T) {
	_, runtime := newTestRuntime(t)

	callContext := armcore.NewContext()
	before, err := runtime.handleWIPICCall(armcore.NewThread(callContext), wipicKernelGetFreeMemory)
	if err != nil {
		t.Fatal(err)
	}
	address := callocThroughSVC(t, runtime, 4096)
	during, err := runtime.handleWIPICCall(armcore.NewThread(armcore.NewContext()), wipicKernelGetFreeMemory)
	if err != nil {
		t.Fatal(err)
	}
	if during >= before {
		t.Fatalf("free memory %d did not fall after a 4 KiB allocation from %d", during, before)
	}
	freeThroughSVC(t, runtime, address)
	after, err := runtime.handleWIPICCall(armcore.NewThread(armcore.NewContext()), wipicKernelGetFreeMemory)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("free memory %d after the release, want the %d it started at", after, before)
	}
}
