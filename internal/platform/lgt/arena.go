package lgt

import (
	"cmp"
	"errors"
	"slices"

	"github.com/movingwoo/wfeature/internal/wipic"
)

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
// The free list is the one every arena here keeps (wipic.TakeFirstFit and
// wipic.InsertFreeBlock); what is this platform's own is the alignment, the
// sizes it looks a release up by, and that a release at the top of the region
// stays on the list rather than lowering the cursor.
type arena struct {
	base   uint32
	limit  uint64
	cursor uint64
	free   []arenaBlock
	sizes  map[uint32]uint64
	// outstanding is the total of sizes. It is kept rather than summed because
	// the collector asks for it once a tick to decide whether a cycle is due,
	// and summing a map of every live allocation to answer that would cost
	// more than the cycle it is deciding about. See collect.go.
	outstanding uint64
}

type arenaBlock = wipic.ArenaBlock

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
	if free, address, ok := wipic.TakeFirstFit(a.free, size); ok {
		a.free = free
		a.sizes[uint32(address)] = size
		a.outstanding += size
		return uint32(address), true
	}
	start := (a.cursor + arenaAlignment - 1) &^ uint64(arenaAlignment-1)
	end := start + size
	if end < start || end > a.limit {
		return 0, false
	}
	a.cursor = end
	a.sizes[uint32(start)] = size
	a.outstanding += size
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
	a.outstanding -= size
	a.free = wipic.InsertFreeBlock(a.free, arenaBlock{Start: uint64(address), End: uint64(address) + size})
	return true
}

// releaseAll returns many blocks at once, ignoring the ones release would. The
// free list comes out exactly as releasing them one at a time leaves it — the
// same spans, sorted and joined, whatever order they came in — but from one
// pass over it rather than one per block. A collection releases two blocks per
// dead object, and per-block insertion into a fragmented list cost the product
// of the two counts.
func (a *arena) releaseAll(addresses []uint32) {
	blocks := make([]arenaBlock, 0, len(addresses))
	for _, address := range addresses {
		size, ok := a.sizes[address]
		if !ok {
			continue
		}
		delete(a.sizes, address)
		a.outstanding -= size
		blocks = append(blocks, arenaBlock{Start: uint64(address), End: uint64(address) + size})
	}
	if len(blocks) == 0 {
		return
	}
	slices.SortFunc(blocks, func(x, y arenaBlock) int { return cmp.Compare(x.Start, y.Start) })
	merged := make([]arenaBlock, 0, len(a.free)+len(blocks))
	join := func(block arenaBlock) {
		if last := len(merged) - 1; last >= 0 && merged[last].End == block.Start {
			merged[last].End = block.End
			return
		}
		merged = append(merged, block)
	}
	free := a.free
	for len(free) > 0 || len(blocks) > 0 {
		if len(blocks) == 0 || len(free) > 0 && free[0].Start < blocks[0].Start {
			join(free[0])
			free = free[1:]
			continue
		}
		join(blocks[0])
		blocks = blocks[1:]
	}
	a.free = merged
}

// used reports how many bytes are outstanding, which the kernel's memory
// queries answer with.
func (a *arena) used() uint64 { return a.outstanding }

func (a *arena) capacity() uint64 { return a.limit - uint64(a.base) }
