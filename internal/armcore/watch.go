package armcore

import "sort"

// Write watching answers the question a memory scanner cannot: not "where does
// this value live" but "what writes it". Finding the address of a health bar
// is a scan; finding the instruction that decrements it is a watch, and that
// instruction is what a cheat has to be built against — the address alone
// changes every run.
//
// The cost is one compare per guest store against a span that is empty unless
// something is being watched, plus one compare per instruction to record where
// execution is. Nothing is allocated and no map is consulted while the watch
// list is empty.

// WatchHit is one instruction's writes to one watched address.
type WatchHit struct {
	// Address is the watched address, PC the instruction that wrote it.
	Address uint32
	PC      uint32
	// Value is the most recent value written, Size its width in bytes.
	Value uint32
	Size  uint8
	// Count is how many times this instruction wrote this address, which is
	// what separates the one store that matters from a memset passing through.
	Count uint64
}

// maxWatchHits bounds distinct (address, PC) pairs. A game that writes one
// address from thousands of sites is not being narrowed down by more of them.
const maxWatchHits = 4096

type watchKey struct {
	address uint32
	pc      uint32
}

// Watch records stores to address. Watching an address already watched is not
// an error: it is what a Host does when re-arming after a reset.
func (core *Core) Watch(address uint32) {
	memory := core.Memory()
	memory.mu.Lock()
	defer memory.mu.Unlock()
	if memory.watches == nil {
		memory.watches = map[uint32]struct{}{}
		memory.watchHits = map[watchKey]*WatchHit{}
	}
	memory.watches[address] = struct{}{}
	memory.refreshWatchSpanLocked()
}

// Unwatch stops recording stores to address and drops its hits.
func (core *Core) Unwatch(address uint32) {
	memory := core.Memory()
	memory.mu.Lock()
	defer memory.mu.Unlock()
	delete(memory.watches, address)
	for key := range memory.watchHits {
		if key.address == address {
			delete(memory.watchHits, key)
		}
	}
	memory.refreshWatchSpanLocked()
}

// ClearWatches stops watching everything and forgets every hit.
func (core *Core) ClearWatches() {
	memory := core.Memory()
	memory.mu.Lock()
	defer memory.mu.Unlock()
	memory.watches = nil
	memory.watchHits = nil
	memory.refreshWatchSpanLocked()
}

// Watches lists the watched addresses in order.
func (core *Core) Watches() []uint32 {
	memory := core.Memory()
	memory.mu.RLock()
	defer memory.mu.RUnlock()
	addresses := make([]uint32, 0, len(memory.watches))
	for address := range memory.watches {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(left, right int) bool { return addresses[left] < addresses[right] })
	return addresses
}

// WatchHits reports what has written the watched addresses, most frequent
// first — the instruction in a loop comes before the one that ran once.
func (core *Core) WatchHits() []WatchHit {
	memory := core.Memory()
	memory.mu.RLock()
	defer memory.mu.RUnlock()
	hits := make([]WatchHit, 0, len(memory.watchHits))
	for _, hit := range memory.watchHits {
		hits = append(hits, *hit)
	}
	sort.Slice(hits, func(left, right int) bool {
		if hits[left].Count != hits[right].Count {
			return hits[left].Count > hits[right].Count
		}
		if hits[left].Address != hits[right].Address {
			return hits[left].Address < hits[right].Address
		}
		return hits[left].PC < hits[right].PC
	})
	return hits
}

// WatchHitsOverflowed reports that the distinct-site limit was reached, so the
// list is missing sites rather than complete.
func (core *Core) WatchHitsOverflowed() bool {
	memory := core.Memory()
	memory.mu.RLock()
	defer memory.mu.RUnlock()
	return memory.watchOverflowed
}

// refreshWatchSpanLocked recomputes the bounds an ordinary store is tested
// against. An empty list leaves a span nothing can fall inside.
func (memory *Memory) refreshWatchSpanLocked() {
	if len(memory.watches) == 0 {
		memory.watchCount, memory.watchLow, memory.watchHigh = 0, ^uint32(0), 0
		return
	}
	low, high := ^uint32(0), uint32(0)
	for address := range memory.watches {
		if address < low {
			low = address
		}
		if address > high {
			high = address
		}
	}
	memory.watchCount, memory.watchLow, memory.watchHigh = len(memory.watches), low, high
}

// noteStore is what every successful guest store calls. It is small enough to
// inline, so an unwatched store pays one compare against a span that nothing
// can fall inside while the watch list is empty — putting it behind another
// call cost more than the compare saved.
func (memory *Memory) noteStore(address, value uint32, size uint8) {
	if memory.watchCount > 0 && address <= memory.watchHigh && address+uint32(size) > memory.watchLow {
		memory.recordWrite(address, value, size)
	}
}

// recordWrite records a store the span test admitted. The caller holds the
// memory lock, as every guest accessor does.
func (memory *Memory) recordWrite(address uint32, value uint32, size uint8) {
	// The span test is inclusive of the whole access, so re-check each byte
	// against the actual watch list.
	for offset := uint32(0); offset < uint32(size); offset++ {
		target := address + offset
		if _, ok := memory.watches[target]; !ok {
			continue
		}
		key := watchKey{address: target, pc: memory.executingPC}
		if hit, ok := memory.watchHits[key]; ok {
			hit.Count++
			hit.Value, hit.Size = value, size
			continue
		}
		if len(memory.watchHits) >= maxWatchHits {
			memory.watchOverflowed = true
			continue
		}
		memory.watchHits[key] = &WatchHit{Address: target, PC: memory.executingPC, Value: value, Size: size, Count: 1}
	}
}
