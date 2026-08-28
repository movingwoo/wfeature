package ktf

// guestArena hands out the guest memory that platform structures and Java
// objects live in. New space comes from a bump cursor; space the collector
// releases is reused. Without the reuse a session's memory use tracks every
// object the game has ever allocated instead of the ones it still holds.
type guestArena struct {
	base   uint32
	limit  uint64
	cursor uint64
	// free is kept sorted by address and coalesced, so neighbouring releases
	// merge back into one block rather than fragmenting the arena into pieces
	// too small to satisfy the next allocation.
	free  []arenaBlock
	freed uint64
	// highWater is the furthest the cursor has ever reached. Everything below
	// it has been handed out at least once, which is what separates a cursor
	// allocation that reuses released space from one that claims space nothing
	// has ever held.
	highWater uint64
	// recordReleased and checkReused are the two halves of the use-after-free
	// detector in arena_shadow.go: the first remembers a block that has just
	// gone back, the second reports one whose bytes did not survive being left
	// alone. They belong to the arena rather than to either caller because
	// both the guest's free and the object collector release here, and a copy
	// taken on one path only would read every collected object as a fault.
	recordReleased func(address uint32, size uint64)
	checkReused    func(address uint32, size uint64)
}

type arenaBlock struct {
	start uint64
	end   uint64
}

const arenaAlignment = 4

func newGuestArena(base uint32, size uint64) *guestArena {
	return &guestArena{base: base, limit: uint64(base) + size, cursor: uint64(base), highWater: uint64(base)}
}

func alignArenaSize(size uint64) uint64 {
	if size == 0 {
		size = arenaAlignment
	}
	return (size + arenaAlignment - 1) &^ uint64(arenaAlignment-1)
}

// allocate reserves size bytes, reusing a released block when one fits and
// bumping the cursor otherwise. The second result reports whether the arena
// had room.
func (arena *guestArena) allocate(size uint64) (uint32, bool) {
	size = alignArenaSize(size)
	// First fit keeps the scan short in the common case: the collector's
	// releases coalesce into a few large blocks near the front.
	for index := range arena.free {
		block := &arena.free[index]
		if block.end-block.start < size {
			continue
		}
		address := block.start
		block.start += size
		if block.start == block.end {
			arena.free = append(arena.free[:index], arena.free[index+1:]...)
		}
		arena.freed -= size
		if arena.checkReused != nil {
			arena.checkReused(uint32(address), size)
		}
		return uint32(address), true
	}
	start := (arena.cursor + arenaAlignment - 1) &^ uint64(arenaAlignment-1)
	end := start + size
	if end < start || end > arena.limit {
		return 0, false
	}
	arena.cursor = end
	// A release at the top of the arena goes back to the cursor rather than to
	// the list, so space below the high-water mark is reuse just as a listed
	// block is, and is checked the same way.
	if arena.checkReused != nil && start < arena.highWater {
		arena.checkReused(uint32(start), min(end, arena.highWater)-start)
	}
	if end > arena.highWater {
		arena.highWater = end
	}
	return uint32(start), true
}

// release returns a block to the arena, merging it with any neighbour.
func (arena *guestArena) release(address uint32, size uint64) {
	size = alignArenaSize(size)
	start := uint64(address)
	end := start + size
	if start < uint64(arena.base) || end > arena.cursor {
		return
	}
	if arena.recordReleased != nil {
		arena.recordReleased(address, size)
	}
	// A block at the very top of the arena is better given back to the cursor
	// than kept on the list: it keeps the high-water mark honest, which is what
	// the guest's own free-memory query reports.
	if end == arena.cursor {
		arena.cursor = start
		arena.trimTail()
		return
	}
	position := 0
	for position < len(arena.free) && arena.free[position].start < start {
		position++
	}
	arena.free = append(arena.free, arenaBlock{})
	copy(arena.free[position+1:], arena.free[position:])
	arena.free[position] = arenaBlock{start: start, end: end}
	arena.freed += size
	arena.coalesceAt(position)
}

// coalesceAt merges the block at position with its neighbours.
func (arena *guestArena) coalesceAt(position int) {
	if position+1 < len(arena.free) && arena.free[position].end == arena.free[position+1].start {
		arena.free[position].end = arena.free[position+1].end
		arena.free = append(arena.free[:position+1], arena.free[position+2:]...)
	}
	if position > 0 && arena.free[position-1].end == arena.free[position].start {
		arena.free[position-1].end = arena.free[position].end
		arena.free = append(arena.free[:position], arena.free[position+1:]...)
	}
}

// trimTail pulls free blocks that now sit at the top of the arena back into
// the cursor, so releasing a run of adjacent objects lowers the high-water
// mark instead of leaving it permanently raised.
func (arena *guestArena) trimTail() {
	for len(arena.free) > 0 {
		last := arena.free[len(arena.free)-1]
		if last.end != arena.cursor {
			return
		}
		arena.cursor = last.start
		arena.freed -= last.end - last.start
		arena.free = arena.free[:len(arena.free)-1]
	}
}

// used reports the bytes currently handed out.
func (arena *guestArena) used() uint64 {
	return arena.cursor - uint64(arena.base) - arena.freed
}

// available reports what a further allocation could still claim.
func (arena *guestArena) available() uint64 {
	return arena.limit - arena.cursor + arena.freed
}

// reportedHeapCeiling bounds the heap size a title is told about. The arena is
// 64MiB, which no handset these archives shipped for ever had, and a title
// that does its own arithmetic on the figure does it in 32-bit ints: one of
// them multiplies the free bytes by a hundred to get a percentage, which
// overflows above about 21MiB and turns into a negative number. It printed
// "-28% FREE" and waited for the figure to recover for as long as it was left
// running. The ceiling is set below that overflow with room to spare, and
// above any heap a WIPI handset had, so nothing that sizes a cache from it
// gets less than the handset would have given.
const reportedHeapCeiling uint64 = 16 << 20

// reportedTotal and reportedFree are the heap a title sees, through
// java/lang/Runtime and through MC_knlGetTotalMemory and MC_knlGetFreeMemory.
// They are one view rather than two: a title that decides what it can afford
// from one and then frees against the other would work from two different
// heaps. Free is the ceiling less what is out, so it falls as the title
// allocates rather than sitting at the ceiling for ever.
func (arena *guestArena) reportedTotal() uint64 {
	total := arena.limit - uint64(arena.base)
	if total > reportedHeapCeiling {
		return reportedHeapCeiling
	}
	return total
}

func (arena *guestArena) reportedFree() uint64 {
	total := arena.reportedTotal()
	used := arena.used()
	if used >= total {
		return 0
	}
	free := total - used
	if available := arena.available(); free > available {
		return available
	}
	return free
}
