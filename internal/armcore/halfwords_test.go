package armcore

import "testing"

// The bulk halfword transfers are what a platform moves a whole surface
// through, so what they have to get right is the byte order and the page
// boundaries the run crosses — a 240x320 surface is thirty-eight pages.

func TestHalfwordTransferRoundTripsAcrossPages(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x10000, 0x4000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	// Long enough to span four pages, and started part-way into the first so
	// that neither end is page aligned.
	values := make([]uint16, 0x1800)
	for index := range values {
		values[index] = uint16(index*7 + 1)
	}
	const base = uint32(0x10000 + 0x40)
	if err := memory.WriteHalfwords(base, values); err != nil {
		t.Fatal(err)
	}

	// The guest reads its own halfwords one at a time, and has to see exactly
	// what was written.
	memory.beginQuantum()
	for index, want := range values {
		got, err := memory.read16(base + uint32(index)*2)
		if err != nil {
			memory.endQuantum()
			t.Fatalf("read16 at %d: %v", index, err)
		}
		if got != want {
			memory.endQuantum()
			t.Fatalf("halfword %d = %#x, want %#x", index, got, want)
		}
	}
	memory.endQuantum()

	read := make([]uint16, len(values))
	if err := memory.ReadHalfwords(base, read); err != nil {
		t.Fatal(err)
	}
	for index := range values {
		if read[index] != values[index] {
			t.Fatalf("read back halfword %d = %#x, want %#x", index, read[index], values[index])
		}
	}
}

func TestHalfwordTransferIsLittleEndian(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x2000, 0x1000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.WriteHalfwords(0x2000, []uint16{0x1234}); err != nil {
		t.Fatal(err)
	}
	var raw [2]byte
	if err := memory.Read(0x2000, raw[:]); err != nil {
		t.Fatal(err)
	}
	if raw != [2]byte{0x34, 0x12} {
		t.Fatalf("stored bytes = %v, want the low byte first", raw)
	}
	read := make([]uint16, 1)
	if err := memory.Write(0x2000, []byte{0x78, 0x56}); err != nil {
		t.Fatal(err)
	}
	if err := memory.ReadHalfwords(0x2000, read); err != nil {
		t.Fatal(err)
	}
	if read[0] != 0x5678 {
		t.Fatalf("read halfword = %#x, want 0x5678", read[0])
	}
}

func TestHalfwordTransferRefusesWhatBytesWouldRefuse(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x3000, 0x1000, PermissionRead); err != nil {
		t.Fatal(err)
	}
	if err := memory.WriteHalfwords(0x3000, make([]uint16, 4)); err == nil {
		t.Fatal("wrote halfwords into read-only memory")
	}
	if err := memory.ReadHalfwords(0x8000, make([]uint16, 4)); err == nil {
		t.Fatal("read halfwords from unmapped memory")
	}
	// An empty transfer touches nothing, so an address that is not mapped at
	// all is still not an error.
	if err := memory.ReadHalfwords(0x8000, nil); err != nil {
		t.Fatalf("empty read = %v", err)
	}
	if err := memory.WriteHalfwords(0x8000, nil); err != nil {
		t.Fatalf("empty write = %v", err)
	}
}

// The remembered data pages are a cache, so the thing to hold is that they
// never change an answer: a guest walking more distinct pages than there are
// ways, in an order that keeps evicting them, still reads back what it wrote.
func TestRememberedPagesSurviveMoreRegionsThanWays(t *testing.T) {
	memory := NewMemory()
	const pages = dataPageWays * 3
	if err := memory.Map(0x100000, uint64(memoryPageSize)*pages, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	address := func(page int) uint32 {
		return 0x100000 + uint32(page)*uint32(memoryPageSize) + 0x10
	}
	memory.beginQuantum()
	defer memory.endQuantum()
	for page := 0; page < pages; page++ {
		if err := memory.write32(address(page), uint32(page)+1); err != nil {
			t.Fatalf("write page %d: %v", page, err)
		}
	}
	// Walked forwards, every page has been evicted by the ones after it; the
	// backwards walk then asks for them in the order least likely to be held.
	for page := pages - 1; page >= 0; page-- {
		got, err := memory.read32(address(page))
		if err != nil {
			t.Fatalf("read page %d: %v", page, err)
		}
		if got != uint32(page)+1 {
			t.Fatalf("page %d = %d, want %d", page, got, page+1)
		}
	}
	// Interleaving two pages a whole way-count apart is the case the ways
	// exist for, and the one where a stale entry would show.
	for round := 0; round < 4; round++ {
		low, err := memory.read32(address(0))
		if err != nil {
			t.Fatal(err)
		}
		high, err := memory.read32(address(dataPageWays))
		if err != nil {
			t.Fatal(err)
		}
		if low != 1 || high != uint32(dataPageWays)+1 {
			t.Fatalf("round %d read %d and %d", round, low, high)
		}
	}
}

// Mapping changes what a page permits, and the remembered pages have to be
// dropped with it or an access keeps the answer the mappings gave before it.
func TestRememberedPagesAreDroppedWhenMappingChanges(t *testing.T) {
	memory := NewMemory()
	// Half a page: a page only partly covered remembers no permission at all,
	// so every access to it goes the long way round.
	half := uint32(memoryPageSize) / 2
	if err := memory.Map(0x200000, uint64(half), PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	memory.beginQuantum()
	if err := memory.write32(0x200000, 0x11223344); err != nil {
		memory.endQuantum()
		t.Fatal(err)
	}
	if err := memory.write32(0x200000+half, 0); err == nil {
		memory.endQuantum()
		t.Fatal("wrote into the unmapped half of the page")
	}
	memory.endQuantum()

	// Mapping the other half makes the page wholly covered, which is a
	// different answer to "what does this page permit" than the one the
	// accesses above established.
	if err := memory.Map(0x200000+half, uint64(half), PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	memory.beginQuantum()
	defer memory.endQuantum()
	if err := memory.write32(0x200000+half, 0x55667788); err != nil {
		t.Fatalf("write into the newly mapped half = %v", err)
	}
	if err := memory.write8(0x200010, 0x99); err != nil {
		t.Fatalf("byte write after remapping = %v", err)
	}
	if byteValue, err := memory.read8(0x200010); err != nil || byteValue != 0x99 {
		t.Fatalf("byte after remapping = %#x, %v", byteValue, err)
	}
	got, err := memory.read32(0x200000)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x11223344 {
		t.Fatalf("word the first mapping held = %#x", got)
	}
	got, err = memory.read32(0x200000 + half)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x55667788 {
		t.Fatalf("word in the newly mapped half = %#x", got)
	}
}
