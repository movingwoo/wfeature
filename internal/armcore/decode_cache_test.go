package armcore

import (
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"
)

// runThumb executes count instructions from address and answers the context.
func runThumb(t *testing.T, memory *Memory, address uint32, count uint32) Context {
	t.Helper()
	context := NewContext()
	if err := context.SetPC(address | 1); err != nil {
		t.Fatal(err)
	}
	if _, err := (Engine{}).Run(&context, memory, 0xffffffff, count); err != nil {
		t.Fatal(err)
	}
	return context
}

// TestDecodeCacheFollowsRewrittenCode is the hazard the per-address decode
// cache introduces: the KTF loader relocates itself and patches SVC stubs over
// pages it has already executed, so a cache that outlived a write would run
// the instruction that used to be there.
func TestDecodeCacheFollowsRewrittenCode(t *testing.T) {
	const base = uint32(0x10000)
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize*2, PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	instruction := make([]byte, 2)
	binary.LittleEndian.PutUint16(instruction, 0x3005) // adds r0, #5
	if err := memory.Load(base, instruction); err != nil {
		t.Fatal(err)
	}
	if got := runThumb(t, memory, base, 1).Registers[0]; got != 5 {
		t.Fatalf("r0 after the original instruction = %d, want 5", got)
	}

	// A Host write replaces it.
	binary.LittleEndian.PutUint16(instruction, 0x3009) // adds r0, #9
	if err := memory.Write(base, instruction); err != nil {
		t.Fatal(err)
	}
	if got := runThumb(t, memory, base, 1).Registers[0]; got != 9 {
		t.Fatalf("r0 after the Host rewrote the instruction = %d, want 9", got)
	}

	// And a guest store does too: the guest writes `adds r0, #7` over itself
	// through a store instruction, then the rewritten instruction runs.
	binary.LittleEndian.PutUint16(instruction, 0x3007)
	memory.beginQuantum()
	if err := memory.writeGuest(base, instruction); err != nil {
		memory.endQuantum()
		t.Fatal(err)
	}
	memory.endQuantum()
	if got := runThumb(t, memory, base, 1).Registers[0]; got != 7 {
		t.Fatalf("r0 after a guest store rewrote the instruction = %d, want 7", got)
	}
}

// TestDecodeCacheRefusesPagesItCannotCoverWholly guards the shortcut the cache
// takes: a cached page answers later addresses without re-checking permission,
// so a page a mapping only partly covers must not be cached, or its
// unmapped tail would execute as code.
func TestDecodeCacheRefusesPagesItCannotCoverWholly(t *testing.T) {
	const base = uint32(0x20000)
	memory := NewMemory()
	// Execute permission covers only the first half of the page.
	if err := memory.Map(base, memoryPageSize/2, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	instruction := make([]byte, 2)
	binary.LittleEndian.PutUint16(instruction, 0x3005)
	if err := memory.Load(base, instruction); err != nil {
		t.Fatal(err)
	}
	if got := runThumb(t, memory, base, 1).Registers[0]; got != 5 {
		t.Fatalf("r0 inside the mapped half = %d, want 5", got)
	}
	if page := memory.pageFor(base); page.decoded != nil {
		t.Fatal("a page the mapping only half covers was cached")
	}

	// The unmapped tail of the same page must still fault.
	memory.beginQuantum()
	_, err := memory.decodeThumb(base + uint32(memoryPageSize)/2)
	memory.endQuantum()
	if !errors.Is(err, ErrUnmapped) && !errors.Is(err, ErrPermission) {
		t.Fatalf("decoding the unmapped tail of a partly covered page = %v, want a fault", err)
	}
}

// TestDecodeCacheRetiresOnRemap covers the other way a cached answer can go
// stale: the page is unchanged but what it is allowed to do is not.
func TestDecodeCacheRetiresOnRemap(t *testing.T) {
	const base = uint32(0x30000)
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize, PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	instruction := make([]byte, 2)
	binary.LittleEndian.PutUint16(instruction, 0x3005)
	if err := memory.Load(base, instruction); err != nil {
		t.Fatal(err)
	}
	runThumb(t, memory, base, 1)
	if page := memory.pageFor(base); page.decoded == nil {
		t.Fatal("a wholly executable page was not cached")
	}
	if err := memory.Map(base+uint32(memoryPageSize), memoryPageSize, PermissionRead); err != nil {
		t.Fatal(err)
	}
	if page := memory.pageFor(base); page.decoded != nil {
		t.Fatal("a remap left decoded entries behind")
	}
}

// TestDecodeCacheCanBeTurnedOff covers the diagnostic switch a Host uses to
// ask whether the cache is helping. With it off the interpreter must still run
// the same code and fault the same way — an answer measured against a broken
// interpreter would be worthless — and no page may be cached.
func TestDecodeCacheCanBeTurnedOff(t *testing.T) {
	if !DecodeCacheEnabled() {
		t.Fatal("the decode cache is off by default")
	}
	SetDecodeCacheEnabled(false)
	t.Cleanup(func() { SetDecodeCacheEnabled(true) })
	if DecodeCacheEnabled() {
		t.Fatal("the switch did not take")
	}

	const base = uint32(0x50000)
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize, PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	instruction := make([]byte, 2)
	binary.LittleEndian.PutUint16(instruction, 0x3005) // adds r0, #5
	if err := memory.Load(base, instruction); err != nil {
		t.Fatal(err)
	}
	if got := runThumb(t, memory, base, 1).Registers[0]; got != 5 {
		t.Fatalf("r0 with the cache off = %d, want 5", got)
	}
	if page := memory.pageFor(base); page.decoded != nil {
		t.Fatal("a page was cached with the cache off")
	}

	// Execute permission is checked on the uncached path too, or turning the
	// cache off would turn the check off with it.
	const unreadable = uint32(0x60000)
	if err := memory.Map(unreadable, memoryPageSize, PermissionRead); err != nil {
		t.Fatal(err)
	}
	memory.beginQuantum()
	_, err := memory.decodeThumb(unreadable)
	memory.endQuantum()
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("decoding a non-executable page with the cache off = %v, want ErrPermission", err)
	}
}

// TestAccessCachesDoNotAuthorizeOtherRegions guards the two lookup caches the
// guest access path keeps — the last mapping that satisfied a validation and
// the last page touched. Both are keyed by address, so the risk is one region
// answering for another: an access just outside a cached mapping must still
// fault, and a read from a second page must not return the first page's bytes.
func TestAccessCachesDoNotAuthorizeOtherRegions(t *testing.T) {
	const first = uint32(0x40000)
	const second = uint32(0x80000)
	memory := NewMemory()
	if err := memory.Map(first, memoryPageSize, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(second, memoryPageSize, PermissionRead); err != nil {
		t.Fatal(err)
	}
	if err := memory.Write(first, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}

	memory.beginQuantum()
	defer memory.endQuantum()

	// Warm both caches on the first region.
	value, err := memory.read32(first)
	if err != nil || value != 0x04030201 {
		t.Fatalf("read32(first) = %#x, %v", value, err)
	}
	if err := memory.write32(first, 0x11223344); err != nil {
		t.Fatalf("write32(first) error = %v", err)
	}

	// An address past the end of that mapping is not covered by it.
	if _, err := memory.read32(first + uint32(memoryPageSize)); !errors.Is(err, ErrUnmapped) {
		t.Fatalf("read past the cached mapping = %v, want ErrUnmapped", err)
	}
	// The second region is readable but not writable, and the cached mapping
	// from the first must not grant it.
	if err := memory.write32(second, 1); !errors.Is(err, ErrPermission) {
		t.Fatalf("write into a read-only region = %v, want ErrPermission", err)
	}
	// Its bytes are its own, not the cached page's.
	value, err = memory.read32(second)
	if err != nil || value != 0 {
		t.Fatalf("read32(second) = %#x, %v, want 0", value, err)
	}
	// And the first region still reads what was written to it.
	value, err = memory.read32(first)
	if err != nil || value != 0x11223344 {
		t.Fatalf("read32(first) after touching the second = %#x, %v", value, err)
	}
}

func TestDecodedEntryStaysFourBytes(t *testing.T) {
	if size := unsafe.Sizeof(decodedThumb{}); size != 4 {
		t.Fatalf("decodedThumb is %d bytes, want 4", size)
	}
}
