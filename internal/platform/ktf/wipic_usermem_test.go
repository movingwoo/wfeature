package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// userMemoryPoolAddress is a scratch region of the platform data area used as
// the guest buffer a caller hands to the library.
const (
	userMemoryPoolAddress = platformDataBase + 0x9000
	userMemoryPoolSize    = 0x1000
	userMemoryNameAddress = platformDataBase + 0x8800
)

func callUserMemory(t *testing.T, runtime *initializationRuntime, function, pool, argument uint32) uint32 {
	t.Helper()
	thread := armcore.NewThread(armcore.Context{})
	for register, value := range map[int]uint32{0: pool, 1: argument} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runtime.handleUserMemoryCall(thread, function)
	if err != nil {
		t.Fatalf("user memory function %d error = %v", function, err)
	}
	return result
}

func getLibraryInterface(t *testing.T, runtime *initializationRuntime, name string) uint32 {
	t.Helper()
	if err := runtime.client.core.Memory().Write(userMemoryNameAddress, append([]byte(name), 0)); err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.Context{})
	for register, value := range map[int]uint32{0: userMemoryNameAddress, 1: 0xffffffff, 2: 0xffffffff, 3: 0} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	// The stacked rtnMinor argument is read from the stack pointer, so it has
	// to address something readable.
	if err := thread.SetRegister(armcore.RegisterSP, ThreadStackBase+uint32(ThreadStackSize)-4); err != nil {
		t.Fatal(err)
	}
	if err := runtime.writeGuestWord(ThreadStackBase+uint32(ThreadStackSize)-4, 0); err != nil {
		t.Fatal(err)
	}
	address, err := runtime.wipicGetDLLInterface(thread)
	if err != nil {
		t.Fatalf("MC_knlGetDLLInterface(%q) error = %v", name, err)
	}
	return address
}

// TestUnknownLibraryIsRefused pins the answer for a library this handset does
// not carry. Null is what the caller's own error path reads; a table of stubs
// would be a surface with no contract behind it.
func TestUnknownLibraryIsRefused(t *testing.T) {
	_, runtime := newTestRuntime(t)
	if address := getLibraryInterface(t, runtime, "MXSomethingElse"); address != 0 {
		t.Fatalf("unknown library answered %#x, want 0", address)
	}
}

// TestUserMemoryLibraryIsOneTable checks that repeated lookups hand back the
// same table. A caller that asks twice and compares pointers would otherwise
// see two different libraries.
func TestUserMemoryLibraryIsOneTable(t *testing.T) {
	_, runtime := newTestRuntime(t)
	first := getLibraryInterface(t, runtime, userMemoryLibrary)
	if first == 0 {
		t.Fatal("user memory library answered 0")
	}
	if second := getLibraryInterface(t, runtime, userMemoryLibrary); second != first {
		t.Fatalf("second lookup = %#x, first = %#x", second, first)
	}
}

// TestUserMemoryCutsBlocksFromTheCallersBuffer is the contract that matters:
// every block sits inside the buffer the caller supplied, blocks do not
// overlap, and they are aligned.
func TestUserMemoryCutsBlocksFromTheCallersBuffer(t *testing.T) {
	_, runtime := newTestRuntime(t)
	if result := callUserMemory(t, runtime, userMemoryCreate, userMemoryPoolAddress, userMemoryPoolSize); result != 0 {
		t.Fatalf("create = %#x, want 0", result)
	}
	sizes := []uint32{1, 100, 7, 256}
	var end uint32
	for _, size := range sizes {
		block := callUserMemory(t, runtime, userMemoryAlloc, userMemoryPoolAddress, size)
		if block == 0 {
			t.Fatalf("alloc(%d) = 0 in a pool with room", size)
		}
		if block%8 != 0 {
			t.Fatalf("alloc(%d) = %#x, want an eight-byte aligned block", size, block)
		}
		if block < userMemoryPoolAddress+end {
			t.Fatalf("alloc(%d) = %#x overlaps the block ending at %#x", size, block, userMemoryPoolAddress+end)
		}
		if block+size > userMemoryPoolAddress+userMemoryPoolSize {
			t.Fatalf("alloc(%d) = %#x runs past the end of the pool", size, block)
		}
		// The block has to be writable guest memory, not just an address.
		if err := runtime.writeGuestWord(block&^3, 0x5a5a5a5a); err != nil {
			t.Fatalf("block at %#x is not writable: %v", block, err)
		}
		end = block + size - userMemoryPoolAddress
	}
}

// TestUserMemoryRefusesWhatTheBufferCannotHold pins the full-pool answer.
// Handing back an address past the caller's buffer would corrupt whatever the
// game keeps after it, and the game's own error path reads null.
func TestUserMemoryRefusesWhatTheBufferCannotHold(t *testing.T) {
	_, runtime := newTestRuntime(t)
	callUserMemory(t, runtime, userMemoryCreate, userMemoryPoolAddress, userMemoryPoolSize)
	if block := callUserMemory(t, runtime, userMemoryAlloc, userMemoryPoolAddress, userMemoryPoolSize+1); block != 0 {
		t.Fatalf("oversized alloc = %#x, want 0", block)
	}
	if block := callUserMemory(t, runtime, userMemoryAlloc, userMemoryPoolAddress, userMemoryPoolSize); block == 0 {
		t.Fatal("an allocation of exactly the pool size was refused")
	}
	if block := callUserMemory(t, runtime, userMemoryAlloc, userMemoryPoolAddress, 1); block != 0 {
		t.Fatalf("alloc from an exhausted pool = %#x, want 0", block)
	}
}

// TestUserMemoryAllocWithoutAPoolAnswersNull covers the ordering error: a
// caller that allocates before it creates gets the same answer as a full pool
// rather than an address into memory nobody reserved.
func TestUserMemoryAllocWithoutAPoolAnswersNull(t *testing.T) {
	_, runtime := newTestRuntime(t)
	if block := callUserMemory(t, runtime, userMemoryAlloc, userMemoryPoolAddress, 16); block != 0 {
		t.Fatalf("alloc without create = %#x, want 0", block)
	}
}

// TestUserMemoryRejectsAnUnmappedPool pins where a bad buffer is reported.
// Checking it at create names the call that supplied it; checking it lazily
// would blame whichever allocation first crossed the end.
func TestUserMemoryRejectsAnUnmappedPool(t *testing.T) {
	_, runtime := newTestRuntime(t)
	thread := armcore.NewThread(armcore.Context{})
	for register, value := range map[int]uint32{0: 0xf0000000, 1: 0x1000} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.handleUserMemoryCall(thread, userMemoryCreate); err == nil {
		t.Fatal("create accepted a pool that is not guest memory")
	}
}
