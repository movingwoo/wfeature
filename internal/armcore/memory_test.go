package armcore

import (
	"errors"
	"sync"
	"testing"
)

func TestMemoryUsesSparseZeroFilledMappings(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x1ff0, 0x30, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 0x30)
	if err := memory.Read(0x1ff0, data); err != nil {
		t.Fatal(err)
	}
	for index, value := range data {
		if value != 0 {
			t.Fatalf("zero-filled byte %d = %d", index, value)
		}
	}
	if err := memory.Write(0x1ffe, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	var got [4]byte
	if err := memory.Read(0x1ffe, got[:]); err != nil {
		t.Fatal(err)
	}
	if got != [4]byte{1, 2, 3, 4} {
		t.Fatalf("cross-page read = %v", got)
	}
}

func TestMemoryValidatesRangesPermissionsAndAlignment(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x1000, 0x1000, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Load(0x1000, []byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("loader write to RX memory = %v", err)
	}
	if err := memory.Write(0x1000, []byte{0}); !errors.Is(err, ErrPermission) {
		t.Fatalf("guest write error = %v, want ErrPermission", err)
	}
	if err := memory.Read(0x2000, make([]byte, 1)); !errors.Is(err, ErrUnmapped) {
		t.Fatalf("unmapped read error = %v, want ErrUnmapped", err)
	}
	if _, err := memory.read32(0x1001); !errors.Is(err, ErrUnaligned) {
		t.Fatalf("unaligned read error = %v, want ErrUnaligned", err)
	}
	if err := memory.Map(0xfffff000, 0x1001, PermissionRead); !errors.Is(err, ErrAddressOverflow) {
		t.Fatalf("overflowing map error = %v, want ErrAddressOverflow", err)
	}
}

func TestContextSelectsAndValidatesInstructionState(t *testing.T) {
	context := NewContext()
	if err := context.SetPC(0x1001); err != nil {
		t.Fatal(err)
	}
	if !context.Thumb() || context.PC() != 0x1000 {
		t.Fatalf("Thumb context = thumb:%t pc:%#x", context.Thumb(), context.PC())
	}
	if err := context.SetPC(0x1002); err == nil {
		t.Fatal("misaligned ARM PC was accepted")
	}
	if err := context.SetPC(0x1004); err != nil {
		t.Fatal(err)
	}
	if context.Thumb() || context.PC() != 0x1004 {
		t.Fatalf("ARM context = thumb:%t pc:%#x", context.Thumb(), context.PC())
	}
}

func TestCommittedRegionsCoalescesWritablePages(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x100000, 0x10000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(0x200000, 0x10000, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if regions := memory.CommittedRegions(PermissionWrite); len(regions) != 0 {
		t.Fatalf("uncommitted regions = %+v, want none", regions)
	}
	// Two adjacent pages and one distant page in the writable mapping, plus a
	// committed page in the read/execute mapping that write scans must skip.
	for _, address := range []uint32{0x100000, 0x101000, 0x104000} {
		if err := memory.Write(address, []byte{1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := memory.Load(0x200000, []byte{1}); err != nil {
		t.Fatal(err)
	}
	regions := memory.CommittedRegions(PermissionWrite)
	want := []CommittedRegion{{Base: 0x100000, Size: 0x2000}, {Base: 0x104000, Size: 0x1000}}
	if len(regions) != len(want) || regions[0] != want[0] || regions[1] != want[1] {
		t.Fatalf("regions = %+v, want %+v", regions, want)
	}
	if executable := memory.CommittedRegions(PermissionExecute); len(executable) != 1 || executable[0].Base != 0x200000 {
		t.Fatalf("executable regions = %+v", executable)
	}

	// A mapping ending mid-page commits a whole page; only the mapped span
	// may be reported, and an overlapping writable mapping extends coverage
	// across a differently-permissioned one.
	if err := memory.Map(0x300000, 0x180, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.Write(0x300000, []byte{1}); err != nil {
		t.Fatal(err)
	}
	partial := memory.CommittedRegions(PermissionWrite)
	if len(partial) != 3 || (partial[2] != CommittedRegion{Base: 0x300000, Size: 0x180}) {
		t.Fatalf("partial-page regions = %+v", partial)
	}
}

// Host reads share the lock: they read the last-mapping cache the guest's
// accesses keep and write no cache at all. Two of them at once must not race;
// run with -race. The two mappings lie a whole number of page-cache ways
// apart, so their pages share ways, and a read that went through the page
// cache, which refreshes a way on every miss, would race here.
func TestConcurrentHostReadsDoNotRace(t *testing.T) {
	memory := NewMemory()
	bases := []uint32{0x100000, 0x100000 + dataPageWays<<memoryPageShift}
	for _, base := range bases {
		if dataPageWay(base>>memoryPageShift) != dataPageWay(bases[0]>>memoryPageShift) {
			t.Fatalf("mapping %#x does not share the first mapping's page-cache way", base)
		}
		if err := memory.Map(base, 0x2000, PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := memory.Write(base, []byte{1, 2, 3, 4}); err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	for _, base := range bases {
		group.Add(1)
		go func(base uint32) {
			defer group.Done()
			buffer := make([]byte, 4)
			for range 2000 {
				if err := memory.Read(base, buffer); err != nil {
					t.Error(err)
					return
				}
				if err := memory.Read(base+0x1000, buffer); err != nil {
					t.Error(err)
					return
				}
				if !memory.Holds([]Span{{Address: base, Data: []byte{1, 2, 3, 4}}, {Address: base + 0x1000, Data: buffer}}) {
					t.Error("guest memory does not hold what was written and read")
					return
				}
			}
		}(base)
	}
	group.Wait()
}
