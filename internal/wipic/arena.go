package wipic

// ArenaBlock is a span of a guest arena, [Start, End).
type ArenaBlock struct {
	Start uint64
	End   uint64
}

// The arenas a platform hands guest memory out of keep what has been given
// back in a free list sorted by address and coalesced, so neighbouring
// releases merge back into one block rather than fragmenting the arena into
// pieces too small to satisfy the next allocation. Every platform here reuses
// that list the same way — the first block that holds a request gives up its
// start — so where a block lands is decided here, once, and each arena keeps
// only its own alignment, its own cursor and its own bookkeeping around it.

// TakeFirstFit takes size bytes from the start of the first free block that
// holds them, which keeps the scan short in the common case: releases coalesce
// into a few large blocks near the front. It answers the list without them and
// the address taken, and false when no block is large enough.
func TakeFirstFit(free []ArenaBlock, size uint64) ([]ArenaBlock, uint64, bool) {
	for index := range free {
		block := &free[index]
		if block.End-block.Start < size {
			continue
		}
		address := block.Start
		block.Start += size
		if block.Start == block.End {
			free = append(free[:index], free[index+1:]...)
		}
		return free, address, true
	}
	return free, 0, false
}

// InsertFreeBlock puts a released block into a sorted, coalesced free list and
// merges it with whichever neighbours it touches, which is all a list that was
// coalesced before needs to stay so.
func InsertFreeBlock(free []ArenaBlock, block ArenaBlock) []ArenaBlock {
	position := 0
	for position < len(free) && free[position].Start < block.Start {
		position++
	}
	free = append(free, ArenaBlock{})
	copy(free[position+1:], free[position:])
	free[position] = block
	if position+1 < len(free) && free[position].End == free[position+1].Start {
		free[position].End = free[position+1].End
		free = append(free[:position+1], free[position+2:]...)
	}
	if position > 0 && free[position-1].End == free[position].Start {
		free[position-1].End = free[position].End
		free = append(free[:position], free[position+1:]...)
	}
	return free
}
