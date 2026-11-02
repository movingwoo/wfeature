package lgt

import (
	"math/rand"
	"slices"
	"testing"
)

// A collection releases every block it frees in one batch. The arena has to
// come out of that exactly as releasing them one at a time would leave it —
// the same spans, joined the same way — because the next allocation's address
// depends on it, and a checkpoint records it.
func TestReleasingABatchLeavesTheArenaAsReleasingEachDoes(t *testing.T) {
	random := rand.New(rand.NewSource(5))
	for round := range 300 {
		one, batch := newArena(0x1000, 1<<20), newArena(0x1000, 1<<20)
		var held []uint32
		for range 200 {
			size := uint64(1 + random.Intn(64))
			if len(held) > 0 && random.Intn(3) == 0 {
				index := random.Intn(len(held))
				one.release(held[index])
				batch.release(held[index])
				held = slices.Delete(held, index, index+1)
				continue
			}
			first, ok := one.allocate(size)
			second, _ := batch.allocate(size)
			if !ok || first != second {
				t.Fatalf("round %d: the arenas diverged before the batch", round)
			}
			held = append(held, first)
		}
		var freed []uint32
		for _, address := range held {
			if random.Intn(2) == 0 {
				freed = append(freed, address)
			}
		}
		// Not handed out, and handed twice: both are ignored, as release does.
		freed = append(append(freed, 0x10), freed...)
		random.Shuffle(len(freed), func(i, j int) { freed[i], freed[j] = freed[j], freed[i] })
		for _, address := range freed {
			one.release(address)
		}
		batch.releaseAll(freed)
		if !slices.Equal(one.free, batch.free) || one.outstanding != batch.outstanding || len(one.sizes) != len(batch.sizes) {
			t.Fatalf("round %d: one at a time left %v (%d), the batch %v (%d)",
				round, one.free, one.outstanding, batch.free, batch.outstanding)
		}
		for range 50 {
			size := uint64(1 + random.Intn(128))
			first, firstOK := one.allocate(size)
			second, secondOK := batch.allocate(size)
			if first != second || firstOK != secondOK {
				t.Fatalf("round %d: the next allocation is %#x in one and %#x in the other", round, first, second)
			}
		}
	}
}

// A sweep that frees many objects into a fragmented arena costs one pass over
// the free list, not one per object freed.
func TestSweepingIntoAFragmentedArenaIsNotQuadratic(t *testing.T) {
	const count = 200000
	arena := newArena(0x1000, 64<<20)
	addresses := make([]uint32, count)
	for index := range addresses {
		address, ok := arena.allocate(8)
		if !ok {
			t.Fatal("the arena is full")
		}
		addresses[index] = address
	}
	// Every other block freed: as many separate spans as there are blocks
	// left, which is the shape interleaved lifetimes leave behind.
	var kept, freed []uint32
	for index, address := range addresses {
		if index%2 == 0 {
			freed = append(freed, address)
		} else {
			kept = append(kept, address)
		}
	}
	arena.releaseAll(freed)
	if len(arena.free) != count/2 {
		t.Fatalf("%d free spans, want %d", len(arena.free), count/2)
	}
	// In no particular order, as a collection finds them.
	rand.New(rand.NewSource(6)).Shuffle(len(kept), func(i, j int) { kept[i], kept[j] = kept[j], kept[i] })
	withinDeadline(t, "a sweep of 100000 blocks into 100000 spans", func() { arena.releaseAll(kept) })
	if len(arena.free) != 1 || arena.outstanding != 0 {
		t.Fatalf("the arena did not join back up: %d spans, %d bytes outstanding", len(arena.free), arena.outstanding)
	}
}
