package wipicrt

import "github.com/movingwoo/wfeature/internal/wipic"

// GuestArena hands out the guest memory that platform structures and Java
// objects live in. New space comes from a bump cursor; space the collector
// releases is reused. Without the reuse a session's memory use tracks every
// object the game has ever allocated instead of the ones it still holds.
type GuestArena struct {
	Base   uint32
	Limit  uint64
	Cursor uint64
	// Free is kept sorted by address and coalesced, so neighbouring releases
	// merge back into one block rather than fragmenting the arena into pieces
	// too small to satisfy the next allocation.
	Free  []ArenaBlock
	Freed uint64
	// HighWater is the furthest the cursor has ever reached. Everything below
	// it has been handed out at least once, which is what separates a cursor
	// allocation that reuses released space from one that claims space nothing
	// has ever held.
	HighWater uint64
	// RecordReleased and checkReused are the two halves of the use-after-free
	// detector in arena_shadow.go: the first remembers a block that has just
	// gone back, the second reports one whose bytes did not survive being left
	// alone. They belong to the arena rather than to either caller because
	// both the guest's free and the object collector release here, and a copy
	// taken on one path only would read every collected object as a fault.
	RecordReleased func(address uint32, size uint64)
	CheckReused    func(address uint32, size uint64)
}

type ArenaBlock = wipic.ArenaBlock

const ArenaAlignment = 4

func NewGuestArena(base uint32, size uint64) *GuestArena {
	return &GuestArena{Base: base, Limit: uint64(base) + size, Cursor: uint64(base), HighWater: uint64(base)}
}

func AlignArenaSize(size uint64) uint64 {
	if size == 0 {
		size = ArenaAlignment
	}
	return (size + ArenaAlignment - 1) &^ uint64(ArenaAlignment-1)
}

// Allocate reserves size bytes, reusing a released block when one fits and
// bumping the cursor otherwise. The second result reports whether the arena
// had room.
func (arena *GuestArena) Allocate(size uint64) (uint32, bool) {
	size = AlignArenaSize(size)
	// First fit, as every arena here reuses its list (wipic.TakeFirstFit).
	if free, address, ok := wipic.TakeFirstFit(arena.Free, size); ok {
		arena.Free = free
		arena.Freed -= size
		if arena.CheckReused != nil {
			arena.CheckReused(uint32(address), size)
		}
		return uint32(address), true
	}
	start := (arena.Cursor + ArenaAlignment - 1) &^ uint64(ArenaAlignment-1)
	end := start + size
	if end < start || end > arena.Limit {
		return 0, false
	}
	arena.Cursor = end
	// A release at the top of the arena goes back to the cursor rather than to
	// the list, so space below the high-water mark is reuse just as a listed
	// block is, and is checked the same way.
	if arena.CheckReused != nil && start < arena.HighWater {
		arena.CheckReused(uint32(start), min(end, arena.HighWater)-start)
	}
	if end > arena.HighWater {
		arena.HighWater = end
	}
	return uint32(start), true
}

// Release returns a block to the arena, merging it with any neighbour.
func (arena *GuestArena) Release(address uint32, size uint64) {
	size = AlignArenaSize(size)
	start := uint64(address)
	end := start + size
	if start < uint64(arena.Base) || end > arena.Cursor {
		return
	}
	if arena.RecordReleased != nil {
		arena.RecordReleased(address, size)
	}
	// A block at the very top of the arena is better given back to the cursor
	// than kept on the list: it keeps the high-water mark honest, which is what
	// the guest's own free-memory query reports.
	if end == arena.Cursor {
		arena.Cursor = start
		arena.trimTail()
		return
	}
	arena.Free = wipic.InsertFreeBlock(arena.Free, ArenaBlock{Start: start, End: end})
	arena.Freed += size
}

// trimTail pulls free blocks that now sit at the top of the arena back into
// the cursor, so releasing a run of adjacent objects lowers the high-water
// mark instead of leaving it permanently raised.
func (arena *GuestArena) trimTail() {
	for len(arena.Free) > 0 {
		last := arena.Free[len(arena.Free)-1]
		if last.End != arena.Cursor {
			return
		}
		arena.Cursor = last.Start
		arena.Freed -= last.End - last.Start
		arena.Free = arena.Free[:len(arena.Free)-1]
	}
}

// Used reports the bytes currently handed out.
func (arena *GuestArena) Used() uint64 {
	return arena.Cursor - uint64(arena.Base) - arena.Freed
}

// Available reports what a further allocation could still claim.
func (arena *GuestArena) Available() uint64 {
	return arena.Limit - arena.Cursor + arena.Freed
}

// ReportedHeapCeiling bounds the heap size a title is told about. The arena is
// 64MiB, which no handset these archives shipped for ever had, and a title
// that does its own arithmetic on the figure does it in 32-bit ints: one of
// them multiplies the free bytes by a hundred to get a percentage, which
// overflows above about 21MiB and turns into a negative number. It printed
// "-28% FREE" and waited for the figure to recover for as long as it was left
// running. The ceiling is set below that overflow with room to spare, and
// above any heap a WIPI handset had, so nothing that sizes a cache from it
// gets less than the handset would have given.
const ReportedHeapCeiling uint64 = 16 << 20

// ReportedTotal and reportedFree are the heap a title sees, through
// java/lang/Runtime and through MC_knlGetTotalMemory and MC_knlGetFreeMemory.
// They are one view rather than two: a title that decides what it can afford
// from one and then frees against the other would work from two different
// heaps. Free is the ceiling less what is out, so it falls as the title
// allocates rather than sitting at the ceiling for ever.
func (arena *GuestArena) ReportedTotal() uint64 {
	total := arena.Limit - uint64(arena.Base)
	if total > ReportedHeapCeiling {
		return ReportedHeapCeiling
	}
	return total
}

func (arena *GuestArena) ReportedFree() uint64 {
	total := arena.ReportedTotal()
	used := arena.Used()
	if used >= total {
		return 0
	}
	free := total - used
	if available := arena.Available(); free > available {
		return available
	}
	return free
}
