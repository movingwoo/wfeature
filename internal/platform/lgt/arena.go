package lgt

import "errors"

// ErrGuestExited reports that the game called MC_knlExit. It is not a failure:
// a Host stops the session on it the same way it would on a window close.
var ErrGuestExited = errors.New("LGT guest exited")

// ErrJavaAppUnsupported reports an LGT Java title stopping at something the
// AOT bridge does not implement yet — an import slot, a class-layout rule, an
// exception it cannot place. It named the whole category once, when no Java
// title ran at all: they are AOT-compiled to native ARM and hand the runtime
// their class metadata through an import table, and implementing that table is
// what turned this from "Java apps" into "this one, here". See docs/lgt.md.
var ErrJavaAppUnsupported = errors.New("LGT Java apps are not supported")

// arena hands out guest memory from a bump cursor and reuses what is freed,
// so a game that allocates and frees in a loop does not exhaust the region.
type arena struct {
	base   uint32
	limit  uint64
	cursor uint64
	free   []arenaBlock
	sizes  map[uint32]uint64
}

type arenaBlock struct {
	start uint64
	end   uint64
}

const arenaAlignment = 8

func newArena(base uint32, size uint64) *arena {
	return &arena{
		base:   base,
		limit:  uint64(base) + size,
		cursor: uint64(base),
		sizes:  make(map[uint32]uint64),
	}
}

func alignArenaSize(size uint64) uint64 {
	if size == 0 {
		size = arenaAlignment
	}
	return (size + arenaAlignment - 1) &^ uint64(arenaAlignment-1)
}

// allocate reserves size bytes, reusing a freed block when one fits.
func (a *arena) allocate(size uint64) (uint32, bool) {
	size = alignArenaSize(size)
	for index := range a.free {
		block := &a.free[index]
		if block.end-block.start < size {
			continue
		}
		address := uint32(block.start)
		block.start += size
		if block.start == block.end {
			a.free = append(a.free[:index], a.free[index+1:]...)
		}
		a.sizes[address] = size
		return address, true
	}
	start := (a.cursor + arenaAlignment - 1) &^ uint64(arenaAlignment-1)
	end := start + size
	if end < start || end > a.limit {
		return 0, false
	}
	a.cursor = end
	a.sizes[uint32(start)] = size
	return uint32(start), true
}

// release returns a block. A pointer the arena never handed out is ignored
// rather than trusted: a game that frees a stack address must not be able to
// make the allocator hand that address out again.
func (a *arena) release(address uint32) bool {
	size, ok := a.sizes[address]
	if !ok {
		return false
	}
	delete(a.sizes, address)
	a.insert(arenaBlock{start: uint64(address), end: uint64(address) + size})
	return true
}

// insert adds a block and coalesces it with its neighbours, so repeated
// allocate/free cycles do not fragment the arena into pieces too small to
// satisfy the next request.
func (a *arena) insert(block arenaBlock) {
	index := 0
	for index < len(a.free) && a.free[index].start < block.start {
		index++
	}
	a.free = append(a.free, arenaBlock{})
	copy(a.free[index+1:], a.free[index:])
	a.free[index] = block

	merged := a.free[:0]
	for _, current := range a.free {
		if len(merged) > 0 && merged[len(merged)-1].end == current.start {
			merged[len(merged)-1].end = current.end
			continue
		}
		merged = append(merged, current)
	}
	a.free = merged
}

// used reports how many bytes are outstanding, which the kernel's memory
// queries answer with.
func (a *arena) used() uint64 {
	total := uint64(0)
	for _, size := range a.sizes {
		total += size
	}
	return total
}

func (a *arena) capacity() uint64 { return a.limit - uint64(a.base) }
