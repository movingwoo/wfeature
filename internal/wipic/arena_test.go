package wipic

import (
	"math/rand"
	"slices"
	"testing"
)

// Where a block lands is guest-visible — it is an address a title keeps — so
// the shared free list has to place every block exactly where each arena's own
// copy of it did. These are those copies as they were, run side by side with
// the shared one over random allocation and release sequences.

// referenceFirstFit is the loop both arenas ran.
func referenceFirstFit(free []ArenaBlock, size uint64) ([]ArenaBlock, uint64, bool) {
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

// referenceInsertJoined is the insertion that coalesced its two neighbours.
func referenceInsertJoined(free []ArenaBlock, block ArenaBlock) []ArenaBlock {
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

// referenceInsertSwept is the insertion that swept the whole list after it.
func referenceInsertSwept(free []ArenaBlock, block ArenaBlock) []ArenaBlock {
	index := 0
	for index < len(free) && free[index].Start < block.Start {
		index++
	}
	free = append(free, ArenaBlock{})
	copy(free[index+1:], free[index:])
	free[index] = block
	merged := free[:0]
	for _, current := range free {
		if len(merged) > 0 && merged[len(merged)-1].End == current.Start {
			merged[len(merged)-1].End = current.End
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func TestTheSharedFreeListPlacesBlocksAsEachArenaDid(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	for _, alignment := range []uint64{4, 8} {
		for round := range 300 {
			const base, limit = 0x1000, 0x1000 + 1<<14
			cursor := uint64(base)
			var shared, joined, swept []ArenaBlock
			live := map[uint64]uint64{}
			for step := range 400 {
				if len(live) == 0 || random.Intn(3) != 0 {
					size := (uint64(random.Intn(96)) + alignment) &^ (alignment - 1)
					var address, joinedAddress, sweptAddress uint64
					var ok, joinedOK, sweptOK bool
					shared, address, ok = TakeFirstFit(shared, size)
					joined, joinedAddress, joinedOK = referenceFirstFit(joined, size)
					swept, sweptAddress, sweptOK = referenceFirstFit(swept, size)
					if ok != joinedOK || ok != sweptOK || address != joinedAddress || address != sweptAddress {
						t.Fatalf("alignment %d round %d step %d: first fit of %d took %#x %v, the arenas took %#x %v and %#x %v",
							alignment, round, step, size, address, ok, joinedAddress, joinedOK, sweptAddress, sweptOK)
					}
					if !ok {
						if cursor+size > limit {
							continue
						}
						address, cursor = cursor, cursor+size
					}
					live[address] = size
				} else {
					addresses := make([]uint64, 0, len(live))
					for address := range live {
						addresses = append(addresses, address)
					}
					slices.Sort(addresses)
					address := addresses[random.Intn(len(addresses))]
					block := ArenaBlock{Start: address, End: address + live[address]}
					delete(live, address)
					shared = InsertFreeBlock(shared, block)
					joined = referenceInsertJoined(joined, block)
					swept = referenceInsertSwept(swept, block)
				}
				if !slices.Equal(shared, joined) || !slices.Equal(shared, swept) {
					t.Fatalf("alignment %d round %d step %d: free list %v, the arenas kept %v and %v",
						alignment, round, step, shared, joined, swept)
				}
			}
		}
	}
}
