package skt

import (
	"sort"
	"sync"

	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Watching writes on this platform.
//
// The two ARM platforms answer "what wrote this address" from the core, which
// traps a store because the store is an instruction against a real address
// space. Here neither half is true: a MIDlet's state is Go objects, the address
// is one heapmap.go invented for it, and a store is the interpreter assigning
// into a map. So the trap is the interpreter itself — `putfield`, `putstatic`
// and the array stores tell the VM's store observer what they wrote — and this
// is what turns one of those into the address a search found.
//
// Two things follow from where it sits.
//
// It costs nothing when nobody is watching. The observer is installed on the
// first watch and taken away with the last, so a title being played rather than
// investigated pays one nil check per store. While one is installed, a store
// costs a map lookup on the object's identity, and only a store to an object
// the search has actually mapped costs more than that.
//
// And the writer is named rather than addressed. `pc 0x40183a2c` means
// something on a platform whose code is in the address space; here the code is
// class files, so a hit carries `com/example/Game.tick+42` instead. That is
// what cheat.WatchHit.Site is for, and the ARM platforms leave it empty.
const (
	// maxWatchSites bounds the distinct writers kept, the way the ARM core
	// bounds its own. A game that writes one field from a hundred places is
	// telling you something different from one that writes it from two, and
	// past this the list stops being readable anyway.
	maxWatchSites = 64
	// maxWatchAddresses bounds how many addresses may be watched at once. Each
	// one costs a comparison per store to a mapped object.
	maxWatchAddresses = 32
)

// watchKey is one writer of one address.
type watchKey struct {
	address uint32
	site    string
}

// watchState is the bookkeeping behind the WatchTarget methods.
type watchState struct {
	mu sync.Mutex
	// addresses is what is being watched, in the order they were added so the
	// panel lists them the way they were asked for.
	addresses []uint32
	hits      map[watchKey]*cheat.WatchHit
	// overflowed says a writer was dropped because the site limit was reached,
	// which the panel reports rather than hiding.
	overflowed bool
}

func (state *watchState) watching() bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return len(state.addresses) > 0
}

// record adds one write to the tally, or extends the one already there.
func (state *watchState) record(address uint32, site string, value uint32, size uint8) {
	state.mu.Lock()
	defer state.mu.Unlock()
	key := watchKey{address: address, site: site}
	if hit, ok := state.hits[key]; ok {
		hit.Count++
		hit.Value = value
		hit.Size = size
		return
	}
	if len(state.hits) >= maxWatchSites {
		state.overflowed = true
		return
	}
	if state.hits == nil {
		state.hits = map[watchKey]*cheat.WatchHit{}
	}
	state.hits[key] = &cheat.WatchHit{
		Address: address, Site: site, Origin: cheat.OriginGuest,
		Value: value, Size: size, Count: 1,
	}
}

// watched reports whether any byte of the span at address is being watched. A
// search finds an address and the field it names may be wider than one byte,
// so a store to the field is a hit on any address inside it.
func (state *watchState) watched(base, width uint32) (uint32, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, address := range state.addresses {
		if address >= base && address < base+width {
			return address, true
		}
	}
	return 0, false
}

// observe turns one store into a hit, if the address it landed on is watched.
// It runs on whatever guest thread performed the store.
func (target cheatTarget) observe(event jvm.StoreEvent) {
	state := &target.runtime.watches
	if !state.watching() {
		return
	}
	address, width, ok := target.runtime.addressOfStore(event)
	if !ok {
		return
	}
	hit, ok := state.watched(address, width)
	if !ok {
		return
	}
	value, size := storeValue(event.Value, width)
	state.record(hit, event.Site(), value, size)
}

// storeValue renders a stored value the way a watch hit reports it: the low
// bytes of what was written, and how wide the slot it went into is. A
// reference has no numeric value a person could use, so it reports zero and
// the site is the whole of the answer.
func storeValue(value jvm.Value, width uint32) (uint32, uint8) {
	size := uint8(width)
	if size == 0 || size > 8 {
		size = 4
	}
	switch value.Kind() {
	case jvm.ValueInt:
		integer, _ := value.Int32()
		return uint32(integer), size
	case jvm.ValueLong:
		long, _ := value.Int64()
		return uint32(long), size
	}
	return 0, size
}

// addressOfStore answers where in the synthetic space a store landed, and how
// wide the slot it landed in is.
//
// It is the reverse of what heapread.go does, and it is deliberately a lookup
// rather than a walk: the map is keyed by the VM's object identity, so a store
// to something the search has never mapped costs one failed map lookup and
// stops there. That is the common case while a watch is installed — a game
// writes to far more objects than a search has narrowed to.
func (runtime *Runtime) addressOfStore(event jvm.StoreEvent) (address, width uint32, ok bool) {
	if runtime == nil || runtime.VM == nil {
		return 0, 0, false
	}
	runtime.heapMu.Lock()
	defer runtime.heapMu.Unlock()
	heap := runtime.heap
	if heap == nil {
		return 0, 0, false
	}

	// A static belongs to its class rather than to an object, and the class is
	// mapped under an identity derived from its name.
	identity := staticsIdentity(event.Class)
	if event.Object != nil {
		identity = runtime.VM.Identity(event.Object)
	}
	entry := heap.byIdentity[identity]
	if entry == nil {
		return 0, 0, false
	}

	if entry.shape.kind == shapeArray {
		if event.Index < 0 {
			return 0, 0, false
		}
		offset := uint32(event.Index) * entry.shape.elementWidth
		if offset >= entry.size {
			return 0, 0, false
		}
		return entry.base + offset, entry.shape.elementWidth, true
	}

	for _, slot := range entry.shape.slots {
		if slot.key == event.Key || (slot.key == "" && slot.field.Key() == event.Key) {
			return entry.base + slot.offset, slot.width, true
		}
	}
	return 0, 0, false
}

// Watch, Unwatch, ClearWatches, Watches, WatchHits and WatchHitsOverflowed are
// the cheat.WatchTarget contract. Installing the observer is what the first
// watch does and removing it is what the last one does, so the interpreter
// carries no observer at all while nobody is investigating.

func (target cheatTarget) Watch(address uint32) {
	state := &target.runtime.watches
	state.mu.Lock()
	for _, existing := range state.addresses {
		if existing == address {
			state.mu.Unlock()
			return
		}
	}
	if len(state.addresses) >= maxWatchAddresses {
		state.mu.Unlock()
		return
	}
	state.addresses = append(state.addresses, address)
	first := len(state.addresses) == 1
	state.mu.Unlock()
	if first {
		target.runtime.VM.SetStoreObserver(target.observe)
	}
}

func (target cheatTarget) Unwatch(address uint32) {
	state := &target.runtime.watches
	state.mu.Lock()
	kept := state.addresses[:0]
	for _, existing := range state.addresses {
		if existing != address {
			kept = append(kept, existing)
		}
	}
	state.addresses = kept
	// The writes recorded for an address nobody is watching are not an answer
	// to any question the panel can still ask.
	for key := range state.hits {
		if key.address == address {
			delete(state.hits, key)
		}
	}
	empty := len(state.addresses) == 0
	state.mu.Unlock()
	if empty {
		target.runtime.VM.SetStoreObserver(nil)
	}
}

func (target cheatTarget) ClearWatches() {
	state := &target.runtime.watches
	state.mu.Lock()
	state.addresses = nil
	state.hits = nil
	state.overflowed = false
	state.mu.Unlock()
	target.runtime.VM.SetStoreObserver(nil)
}

func (target cheatTarget) Watches() []uint32 {
	state := &target.runtime.watches
	state.mu.Lock()
	defer state.mu.Unlock()
	return append([]uint32(nil), state.addresses...)
}

func (target cheatTarget) WatchHits() []cheat.WatchHit {
	state := &target.runtime.watches
	state.mu.Lock()
	defer state.mu.Unlock()
	hits := make([]cheat.WatchHit, 0, len(state.hits))
	for _, hit := range state.hits {
		hits = append(hits, *hit)
	}
	// Most-written first, then by address and site so two runs of the same
	// game list the same writers in the same order.
	sort.Slice(hits, func(a, b int) bool {
		if hits[a].Count != hits[b].Count {
			return hits[a].Count > hits[b].Count
		}
		if hits[a].Address != hits[b].Address {
			return hits[a].Address < hits[b].Address
		}
		return hits[a].Site < hits[b].Site
	})
	return hits
}

func (target cheatTarget) WatchHitsOverflowed() bool {
	state := &target.runtime.watches
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.overflowed
}
