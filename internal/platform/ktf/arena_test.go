package ktf

import "testing"

func TestGuestArenaReusesReleasedBlocks(t *testing.T) {
	arena := newGuestArena(0x1000, 256)
	first, ok := arena.allocate(16)
	if !ok || first != 0x1000 {
		t.Fatalf("allocate() = %#x, %v", first, ok)
	}
	second, ok := arena.allocate(16)
	if !ok || second != 0x1010 {
		t.Fatalf("allocate() = %#x, %v", second, ok)
	}
	third, _ := arena.allocate(16)
	if used := arena.used(); used != 48 {
		t.Fatalf("used() = %d, want 48", used)
	}
	// Releasing the middle block leaves a hole the next allocation of the same
	// size takes, rather than growing the arena.
	arena.release(second, 16)
	if used := arena.used(); used != 32 {
		t.Fatalf("used() after release = %d, want 32", used)
	}
	again, ok := arena.allocate(16)
	if !ok || again != second {
		t.Fatalf("allocate() after release = %#x, %v, want %#x", again, ok, second)
	}
	if third != 0x1020 {
		t.Fatalf("third allocation moved to %#x", third)
	}
}

func TestGuestArenaCoalescesNeighbours(t *testing.T) {
	arena := newGuestArena(0x1000, 4096)
	blocks := make([]uint32, 4)
	for index := range blocks {
		blocks[index], _ = arena.allocate(16)
	}
	// Keep the last block allocated so the released run cannot simply be
	// folded back into the cursor.
	arena.release(blocks[0], 16)
	arena.release(blocks[1], 16)
	arena.release(blocks[2], 16)
	if len(arena.free) != 1 {
		t.Fatalf("free list holds %d blocks, want 1 coalesced block", len(arena.free))
	}
	wide, ok := arena.allocate(48)
	if !ok || wide != blocks[0] {
		t.Fatalf("allocate(48) = %#x, %v, want the coalesced block %#x", wide, ok, blocks[0])
	}
}

func TestGuestArenaReleasingTheTopLowersTheCursor(t *testing.T) {
	arena := newGuestArena(0x1000, 4096)
	first, _ := arena.allocate(16)
	second, _ := arena.allocate(16)
	arena.release(second, 16)
	if arena.cursor != uint64(second) {
		t.Fatalf("cursor = %#x, want %#x", arena.cursor, second)
	}
	arena.release(first, 16)
	if arena.used() != 0 || len(arena.free) != 0 {
		t.Fatalf("used() = %d with %d free blocks, want an empty arena", arena.used(), len(arena.free))
	}
}

func TestGuestArenaReportsExhaustion(t *testing.T) {
	arena := newGuestArena(0x1000, 64)
	if _, ok := arena.allocate(64); !ok {
		t.Fatal("allocate() refused the whole arena")
	}
	if _, ok := arena.allocate(4); ok {
		t.Fatal("allocate() handed out space past the arena limit")
	}
	if available := arena.available(); available != 0 {
		t.Fatalf("available() = %d, want 0", available)
	}
}

// The detector's halves are wired through the arena, so both the list path and
// the space a release gave back to the cursor have to reach them. A block
// handed out from untouched space must not, because nothing marked it.
func TestGuestArenaMarksEveryReuse(t *testing.T) {
	arena := newGuestArena(0x1000, 4096)
	marked := map[uint32]uint64{}
	checked := map[uint32]uint64{}
	arena.recordReleased = func(address uint32, size uint64) { marked[address] = size }
	arena.checkReused = func(address uint32, size uint64) { checked[address] = size }

	first, _ := arena.allocate(16)
	second, _ := arena.allocate(16)
	third, _ := arena.allocate(16)
	if len(checked) != 0 {
		t.Fatalf("untouched space was checked: %v", checked)
	}

	// A block released into the list is marked, and checked when it is reused.
	arena.release(second, 16)
	if marked[second] != 16 {
		t.Fatalf("release did not mark %#x: %v", second, marked)
	}
	if again, _ := arena.allocate(16); again != second {
		t.Fatalf("allocate() = %#x, want the released block %#x", again, second)
	}
	if checked[second] != 16 {
		t.Fatalf("reuse of %#x was not checked: %v", second, checked)
	}

	// Space released back to the cursor is checked as far as the high-water
	// mark and no further, because past it nothing was ever handed out.
	clear(checked)
	arena.release(third, 16)
	arena.release(second, 16)
	wide, _ := arena.allocate(64)
	if wide != second {
		t.Fatalf("allocate(64) = %#x, want %#x", wide, second)
	}
	if checked[second] != 32 {
		t.Fatalf("cursor reuse checked %v, want 32 bytes at %#x", checked, second)
	}
	if _, ok := checked[first]; ok {
		t.Fatalf("a live block was checked: %v", checked)
	}
}

func TestGuestArenaAlignsAllocations(t *testing.T) {
	arena := newGuestArena(0x1000, 4096)
	first, _ := arena.allocate(3)
	second, _ := arena.allocate(1)
	if first%arenaAlignment != 0 || second%arenaAlignment != 0 {
		t.Fatalf("allocations %#x and %#x are not aligned", first, second)
	}
	if second != first+arenaAlignment {
		t.Fatalf("second allocation at %#x, want %#x", second, first+arenaAlignment)
	}
}

// The heap a title is told about is not the arena's own size. A title does its
// own arithmetic on the figure in 32-bit ints — one multiplies the free bytes
// by a hundred for a percentage — and 64MiB overflows that: it printed a
// negative percentage and waited for the heap to recover for the rest of the
// run. The reported view is bounded, and free falls as the title allocates so
// the percentage means something.
func TestGuestArenaReportsABoundedHeapThatFallsAsItIsUsed(t *testing.T) {
	arena := newGuestArena(0x30000000, 64<<20)
	total := arena.reportedTotal()
	if total != reportedHeapCeiling {
		t.Fatalf("reported total = %d, want the ceiling %d", total, reportedHeapCeiling)
	}
	if total*100 > 1<<31-1 {
		t.Fatalf("reported total %d overflows a title's 32-bit percentage", total)
	}
	if free := arena.reportedFree(); free != total {
		t.Fatalf("free on an empty arena = %d, want the whole reported heap %d", free, total)
	}
	if _, ok := arena.allocate(1 << 20); !ok {
		t.Fatal("allocate() ran out of an empty arena")
	}
	if free := arena.reportedFree(); free != total-(1<<20) {
		t.Fatalf("free after a megabyte = %d, want %d", free, total-(1<<20))
	}

	// An arena smaller than the ceiling reports itself, and free never claims
	// more than a further allocation could actually take.
	small := newGuestArena(0x30000000, 1<<20)
	if got := small.reportedTotal(); got != 1<<20 {
		t.Fatalf("small arena reported total = %d, want its own size", got)
	}
	if _, ok := small.allocate(1 << 19); !ok {
		t.Fatal("allocate() ran out of a fresh arena")
	}
	if free, available := small.reportedFree(), small.available(); free > available {
		t.Fatalf("reported free %d is more than the %d left", free, available)
	}
}
