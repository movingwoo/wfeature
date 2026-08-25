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
//
// **Not every store to guest memory is a guest instruction**, and a watch that
// saw only the guest ones answered the question wrongly rather than partially.
// This platform writes into the guest's own address space constantly: a Java
// object's fields are guest words the runtime keeps in step, a supervisor call
// that services memcpy writes the destination itself, and an image blit lands
// through the same door. An address the host rewrites every tick would report
// no writers at all — which reads as "nothing touches this", the one answer
// that ends an investigation. So both kinds are recorded and each hit says
// which it was.
type WriteOrigin uint8

const (
	// OriginGuest is a store by an emulated instruction. PC names it exactly.
	OriginGuest WriteOrigin = iota
	// OriginHost is a store by this platform through Memory.Write.
	//
	// Its PC is the last guest instruction that ran, which is worth two
	// different amounts depending on how the write was reached. A write inside
	// a supervisor call names the guest instruction that made the call, which
	// is exactly the caller wanted. A write from a host path the guest did not
	// enter — a field synchronised between ticks, a frame published at a tick
	// boundary — names wherever the guest happened to stop, which means
	// nothing. A reader has to be told which, so the origin travels with the
	// hit rather than being folded into the PC.
	OriginHost
)

func (origin WriteOrigin) String() string {
	if origin == OriginHost {
		return "host"
	}
	return "guest"
}

// WatchHit is one writer's stores to one watched address.
type WatchHit struct {
	// Address is the watched address, PC the instruction that wrote it — or,
	// for a host write, the last guest instruction to have run. Origin says
	// which, and a guest and a host writer at the same PC stay separate hits
	// because they are separate facts.
	Address uint32
	PC      uint32
	Origin  WriteOrigin
	// Value is the most recent value written, Size its width in bytes.
	Value uint32
	Size  uint8
	// Count is how many times this writer wrote this address, which is what
	// separates the one store that matters from a memset passing through.
	Count uint64
	// First and Last are the ordinals of this writer's first and last store,
	// counted across every watched address. Two writers of one word — a host
	// write that clears a block and the guest store that fills it — are the
	// same two facts in either order, and only these say which order it was.
	First, Last uint64
}

// maxWatchHits bounds distinct (address, PC) pairs. A game that writes one
// address from thousands of sites is not being narrowed down by more of them.
const maxWatchHits = 4096

type watchKey struct {
	address uint32
	pc      uint32
	origin  WriteOrigin
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
	memory.watchStores = 0
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
		if hits[left].PC != hits[right].PC {
			return hits[left].PC < hits[right].PC
		}
		return hits[left].Origin < hits[right].Origin
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
		memory.recordWrite(address, value, size, OriginGuest)
	}
}

// noteHostWrite is what a platform write through Memory.Write calls. It walks
// the watch list rather than the written run, because the two differ by orders
// of magnitude in the case that matters: a frame published to guest memory is
// a hundred kilobytes, and the addresses being watched are a handful. The
// caller holds the memory lock, as Memory.write does.
func (memory *Memory) noteHostWrite(address uint32, data []byte) {
	if memory.watchCount == 0 || len(data) == 0 {
		return
	}
	end := uint64(address) + uint64(len(data))
	if uint64(address) > uint64(memory.watchHigh) || end <= uint64(memory.watchLow) {
		return
	}
	for target := range memory.watches {
		if uint64(target) < uint64(address) || uint64(target) >= end {
			continue
		}
		// A host write has no width of its own — it is a run of bytes, not a
		// store — so the value reported is the word the watcher would read at
		// its own address once the write lands, narrowed to what the run
		// actually reaches. A watched four-byte field written by a field
		// synchronisation therefore reads back as the word it was given.
		offset := target - address
		size := min(len(data)-int(offset), 4)
		var value uint32
		for index := range size {
			value |= uint32(data[int(offset)+index]) << (8 * index)
		}
		memory.recordWriteAt(target, value, uint8(size), OriginHost)
	}
}

// recordWrite records a store the span test admitted. The caller holds the
// memory lock, as every guest accessor does.
func (memory *Memory) recordWrite(address uint32, value uint32, size uint8, origin WriteOrigin) {
	// The span test is inclusive of the whole access, so re-check each byte
	// against the actual watch list.
	for offset := uint32(0); offset < uint32(size); offset++ {
		target := address + offset
		if _, ok := memory.watches[target]; !ok {
			continue
		}
		memory.recordWriteAt(target, value, size, origin)
	}
}

// recordWriteAt files one hit against an address already known to be watched.
// The caller holds the memory lock.
func (memory *Memory) recordWriteAt(target uint32, value uint32, size uint8, origin WriteOrigin) {
	key := watchKey{address: target, pc: memory.executingPC, origin: origin}
	memory.watchStores++
	if hit, ok := memory.watchHits[key]; ok {
		hit.Count++
		hit.Value, hit.Size = value, size
		hit.Last = memory.watchStores
		return
	}
	if len(memory.watchHits) >= maxWatchHits {
		memory.watchOverflowed = true
		return
	}
	memory.watchHits[key] = &WatchHit{
		Address: target, PC: memory.executingPC, Origin: origin, Value: value, Size: size, Count: 1,
		First: memory.watchStores, Last: memory.watchStores,
	}
}
