package armcore

import (
	"errors"
	"testing"
)

// A page remembers what the mappings permit across the whole of it, so that an
// access does not have to find that out from the mapping list. A page a mapping
// only partly covers has no such answer: the bytes inside the mapping are
// readable and the bytes past it are not, and remembering the mapping's
// permission for the page would make the tail of it readable too.
func TestPartlyMappedPageDoesNotInheritThePermission(t *testing.T) {
	memory := NewMemory()
	// Half a page, so the page holding it is covered from 0x1000 to 0x1800.
	if err := memory.Map(0x1000, 0x800, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.Write(0x1000, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	// Touching the covered half is what fills a page's remembered permission
	// in, so the unmapped half is asked about with that already done.
	if _, err := memory.read32(0x1000); err != nil {
		t.Fatalf("mapped read = %v", err)
	}
	if _, err := memory.read32(0x1800); !errors.Is(err, ErrUnmapped) {
		t.Fatalf("read past the mapping = %v, want ErrUnmapped", err)
	}
	if err := memory.write32(0x1ffc, 1); !errors.Is(err, ErrUnmapped) {
		t.Fatalf("write past the mapping = %v, want ErrUnmapped", err)
	}
}

// The same thing one step on: a page whose permission has been remembered has
// to forget it when the mappings change. Mapping is additive here — there is no
// unmap and no protect, and a second mapping over the same range only ever
// grants more — so what a stale answer costs is a refusal of an access the new
// mapping allows.
func TestMappingAgainForgetsAPageRememberedPermission(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x2000, 0x1000, PermissionRead); err != nil {
		t.Fatal(err)
	}
	// Reading is what fills the page's remembered permission in.
	if _, err := memory.read32(0x2000); err != nil {
		t.Fatal(err)
	}
	if err := memory.write32(0x2000, 1); !errors.Is(err, ErrPermission) {
		t.Fatalf("write to read-only memory = %v, want ErrPermission", err)
	}
	if err := memory.Map(0x2000, 0x1000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.write32(0x2000, 0x11223344); err != nil {
		t.Fatalf("write after mapping the range writable = %v", err)
	}
	value, err := memory.read32(0x2000)
	if err != nil {
		t.Fatal(err)
	}
	if value != 0x11223344 {
		t.Fatalf("read back = %#x, want what was written", value)
	}
}

// Execute permission is asked for by the fetch path and never by a load, so a
// page that carries only it must not answer a read from the remembered value.
func TestRememberedPermissionKeepsTheKindsApart(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x3000, 0x1000, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Load(0x3000, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.read32(0x3000); err != nil {
		t.Fatalf("read of read-execute memory = %v", err)
	}
	if err := memory.write32(0x3000, 0); !errors.Is(err, ErrPermission) {
		t.Fatalf("write to read-execute memory = %v, want ErrPermission", err)
	}
}

// A read-execute stub region mapped inside a read-write arena is the layout
// mistake that costs the most to find: every write the arena's owner makes is
// permitted, because the arena's own mapping is still in force over the
// overlap, and the stubs it lands on only fail once one of them is called
// again. The map refuses the pair instead.
func TestMappingRefusesAnOverlapNeitherPermissionCovers(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x10000, 0x4000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	err := memory.Map(0x11000, 0x1000, PermissionReadExecute)
	if !errors.Is(err, ErrOverlappingMapping) {
		t.Fatalf("read-execute inside read-write = %v, want ErrOverlappingMapping", err)
	}
	// The refusal is about the overlap, not about the two permissions: the
	// same stub region maps where nothing else is.
	if err := memory.Map(0x20000, 0x1000, PermissionReadExecute); err != nil {
		t.Fatalf("map beside the arena = %v", err)
	}
	// An additive remap of a range already mapped is not an overlap of two
	// owners, and stays legal.
	if err := memory.Map(0x10000, 0x4000, PermissionReadWriteExecute); err != nil {
		t.Fatalf("remap with more permission = %v", err)
	}
}
