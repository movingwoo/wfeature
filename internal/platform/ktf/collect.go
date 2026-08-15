package ktf

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Guest object collection.
//
// Nothing else reclaims a Java object here. The guest's MC_knlFree is a no-op,
// its allocator is the Host's bump arena, and the original runtime's own
// collector is not part of the AOT image, so without this a session holds
// every object the game has ever allocated.
//
// The collector is conservative on the guest side and precise on the Go side.
// Guest code keeps references in registers, on its stacks and in memory the
// Host cannot describe field by field, so any word that looks like the address
// of a tracked object is treated as one: over-retention is the safe direction,
// and freeing a live object would corrupt the guest instead. The Go side needs
// no guessing — an object still referenced from Go frames is exactly what a
// weak binding reports, which covers the Go stacks of parked guest workers
// that nothing else can enumerate.
//
// Roots are the thread registers and thread-local words, every committed
// read-write span outside a tracked object, and the objects Go still holds.
// Tracing from there reaches every live object, and a group of dead objects
// that only reference each other is reached by nothing — which is how a cycle
// gets collected.
//
// Asking Go the question only works once Go can see the same graph the guest
// can, and it cannot: a bound object's references live in its guest payload,
// which to Go is opaque bytes. So every cycle mirrors that graph across with
// jvm.RetainAOTGraph before anything is released — for every tracked object,
// not only the ones being released, because a pinned object's payload names
// things just as a released one's does. Without the mirror an object reachable
// only from another object's payload looks unreferenced to Go the moment it is
// released, and Go frees it while its holder lives on, is handed back to the
// guest, and has the guest follow the reference into an address whose object
// no longer exists. The mirror is refreshed rather than accumulated, so it
// tracks what the guest actually holds, and because it is Go that answers,
// cycles are still collected.

// objectRecord is one tracked guest object allocation.
type objectRecord struct {
	size uint32
	// released records that the collector has dropped the Host's strong
	// reference and is waiting to see whether Go frees the object. An object
	// is never freed in the cycle that first finds it unreachable, so a
	// reference living only in a Go frame always gets a chance to show itself.
	released bool
}

// CollectionStats reports what one collection cycle did.
type CollectionStats struct {
	Tracked  int
	Marked   int
	Released int
	Freed    int
	Lost     int
	Bytes    uint64
}

const (
	// collectionFloor is the arena growth that triggers the first cycle and
	// the smallest growth that triggers a later one.
	collectionFloor uint64 = 256 << 10
	// scanChunk bounds how much guest memory is read at once while scanning.
	scanChunk = 64 << 10
)

// trackObject records an object allocation so the collector can find, scan and
// eventually reclaim it.
func (runtime *initializationRuntime) trackObject(address uint32, size uint64) {
	if runtime.objects == nil {
		runtime.objects = make(map[uint32]objectRecord)
	}
	runtime.objects[address] = objectRecord{size: uint32(size)}
}

// collectionDue reports whether the arena has grown enough since the last
// cycle to be worth another one.
func (runtime *initializationRuntime) collectionDue() bool {
	return runtime.arena.used() >= runtime.collectAt
}

// scheduleNextCollection arms the next cycle a fixed amount of growth ahead of
// what survived this one. Growing the trigger with the arena instead would let
// the arena keep climbing, because an object is only freed a cycle after it
// stops being reachable and a doubling trigger outruns that lag.
func (runtime *initializationRuntime) scheduleNextCollection() {
	runtime.collectAt = runtime.arena.used() + collectionFloor
}

// extentIndex is the tracked objects in address order, used to decide whether
// a guest word points into one.
type extentIndex struct {
	starts []uint32
	ends   []uint32
	// low and high bound every tracked object. A collection reads every word
	// of committed guest memory and asks this question of each one, and almost
	// none of them are object pointers — they are code addresses, small
	// integers, packed bytes, zero. Two compares reject those before the
	// search runs, which is what keeps a collection proportional to the
	// pointers a heap holds rather than to the words it spans.
	low  uint32
	high uint32
}

func (runtime *initializationRuntime) buildExtentIndex() extentIndex {
	starts := make([]uint32, 0, len(runtime.objects))
	for address := range runtime.objects {
		starts = append(starts, address)
	}
	slices.Sort(starts)
	ends := make([]uint32, len(starts))
	high := uint32(0)
	for index, address := range starts {
		ends[index] = address + runtime.objects[address].size
		high = max(high, ends[index])
	}
	low := uint32(0)
	if len(starts) > 0 {
		low = starts[0]
	}
	return extentIndex{starts: starts, ends: ends, low: low, high: high}
}

// lookup resolves a guest word to the object containing it, so an interior
// pointer — the guest holds an object's field base as often as its head —
// still keeps its object alive.
func (index extentIndex) lookup(word uint32) (uint32, bool) {
	if word < index.low || word >= index.high {
		return 0, false
	}
	// The search is written out rather than handed to sort.Search because that
	// takes a closure, and a call per comparison is most of the cost of a
	// comparison this small.
	low, high := 0, len(index.starts)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if index.starts[middle] > word {
			high = middle
		} else {
			low = middle + 1
		}
	}
	if low == 0 {
		return 0, false
	}
	position := low - 1
	if word >= index.ends[position] {
		return 0, false
	}
	return index.starts[position], true
}

// collectGuestObjects runs one mark-and-sweep cycle. It must be called at a
// safepoint: no AOT call in flight and every guest worker parked, so the only
// guest state holding references is the memory and registers it scans.
func (runtime *initializationRuntime) collectGuestObjects(extraRoots []uint32) (CollectionStats, error) {
	stats := CollectionStats{Tracked: len(runtime.objects)}
	if len(runtime.objects) == 0 {
		runtime.scheduleNextCollection()
		return stats, nil
	}
	index := runtime.buildExtentIndex()
	marked := make(map[uint32]bool, len(runtime.objects))
	pending := make([]uint32, 0, 64)

	mark := func(word uint32) {
		address, ok := index.lookup(word)
		if !ok || marked[address] {
			return
		}
		marked[address] = true
		pending = append(pending, address)
	}

	// Addresses the Host itself will write to after the cycle, whether or not
	// any guest word names them. A frozen cheat address is the case that
	// matters: the freeze rewrites it every tick.
	for _, root := range extraRoots {
		mark(root)
	}

	// Registers and thread-local words of every thread that can resume.
	for _, thread := range runtime.collectionThreads() {
		context := thread.Context()
		for _, register := range context.Registers {
			mark(register)
		}
		for _, word := range runtime.client.core.ThreadLocalWords(thread) {
			mark(word)
		}
	}

	// An object Go still holds may be handed back to the guest at any time, so
	// it is a root, and so is everything it references.
	for address, record := range runtime.objects {
		if record.released && runtime.client.vm.AOTObjectRetained(address) {
			mark(address)
		}
	}

	// Every committed read-write span that is not itself a tracked object:
	// guest stacks, the client image's statics, and the platform structures
	// the arena holds alongside the objects.
	if err := runtime.scanRootMemory(index, mark); err != nil {
		return stats, err
	}

	// Trace: a live object's own words reach the objects it references.
	buffer := make([]byte, 0, 1024)
	for len(pending) > 0 {
		address := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		size := runtime.objects[address].size
		if int(size) > cap(buffer) {
			buffer = make([]byte, size)
		}
		body := buffer[:size]
		if err := runtime.client.core.Memory().Read(address, body); err != nil {
			return stats, fmt.Errorf("scan KTF object at %#x: %w", address, err)
		}
		scanWords(body, mark)
	}
	stats.Marked = len(marked)

	// Mirror the guest's reference graph into Go before anything is released.
	// An object's references live in its guest payload, which Go cannot see, so
	// without this an object reachable only from another object's payload looks
	// unreferenced to Go the moment it is released — and Go frees it while the
	// holder lives on. Every tracked object is mirrored, not only the ones
	// being released: a pinned object's payload names things just as a released
	// one's does, and it is the pinned holders that were losing their contents.
	// See jvm.RetainAOTGraph.
	mirror := make([]byte, 0, 1024)
	for address, record := range runtime.objects {
		retain, err := runtime.referencedObjects(index, address, record.size, &mirror)
		if err != nil {
			return stats, err
		}
		runtime.client.vm.RetainAOTGraph(address, retain)
	}

	// Sweep. An unmarked object first loses its strong reference and only a
	// later cycle frees it, and only once Go has let go as well.
	for address, record := range runtime.objects {
		if marked[address] {
			if record.released {
				// Re-pin: the guest can reach it again, so Go must not drop it.
				// A binding that cannot be re-pinned is one Go collected while
				// the guest could still reach it. The mirror above is what makes
				// that impossible; if it ever happens the address is not usable
				// again, so it is counted, dropped from tracking, and its guest
				// bytes are left reserved rather than handed to something else.
				if _, ok := runtime.client.vm.AOTObject(address); !ok {
					stats.Lost++
					delete(runtime.objects, address)
					continue
				}
				record.released = false
				runtime.objects[address] = record
			}
			continue
		}
		// An address handed back to the guest since the last cycle has been
		// re-pinned by the boundary that handed it over, which leaves this
		// record a cycle out of date. Fold that in before deciding anything:
		// the scan has just shown the guest is not holding it, so it goes back
		// through the release below.
		if record.released && runtime.client.vm.AOTObjectPinned(address) {
			record.released = false
		}
		if !record.released {
			runtime.client.vm.ReleaseAOTObject(address)
			record.released = true
			runtime.objects[address] = record
			stats.Released++
			continue
		}
		if runtime.client.vm.AOTObjectRetained(address) {
			continue
		}
		if err := runtime.releaseObjectMemory(address, record.size); err != nil {
			return stats, err
		}
		runtime.client.vm.ForgetAOTObject(address)
		delete(runtime.objects, address)
		stats.Freed++
		stats.Bytes += uint64(record.size)
	}
	runtime.scheduleNextCollection()
	return stats, nil
}

// referencedObjects reads one object's guest payload and answers the tracked
// objects its words name — the guest-side edges of the graph, in the form Go
// can hold. The lookup does not pin anything: pinning here is exactly what the
// release that follows is avoiding.
//
// This runs for every tracked object on every cycle, and most objects name
// nothing at all — they are the byte and integer arrays a game spends its heap
// on. So the buffer is the caller's to reuse and neither the answer nor the
// duplicate set is allocated until a first reference actually turns up.
func (runtime *initializationRuntime) referencedObjects(index extentIndex, address uint32, size uint32, buffer *[]byte) ([]*jvm.Object, error) {
	if int(size) > cap(*buffer) {
		*buffer = make([]byte, size)
	}
	body := (*buffer)[:size]
	if err := runtime.client.core.Memory().Read(address, body); err != nil {
		return nil, fmt.Errorf("read KTF object at %#x: %w", address, err)
	}
	var retain []*jvm.Object
	var seen map[uint32]bool
	scanWords(body, func(word uint32) {
		target, ok := index.lookup(word)
		if !ok || target == address || seen[target] {
			return
		}
		if seen == nil {
			seen = make(map[uint32]bool, 4)
		}
		seen[target] = true
		if object, bound := runtime.client.vm.AOTObjectAt(target); bound {
			retain = append(retain, object)
		}
	})
	return retain, nil
}

// collectionThreads lists the ARM threads whose state can still name an
// object: the Host's own service thread and every parked guest worker.
func (runtime *initializationRuntime) collectionThreads() []*armcore.Thread {
	threads := make([]*armcore.Thread, 0, len(runtime.client.workers)+1)
	if runtime.client.thread != nil {
		threads = append(threads, runtime.client.thread)
	}
	for _, worker := range runtime.client.workers {
		if worker != nil && worker.armThread != nil {
			threads = append(threads, worker.armThread)
		}
	}
	return threads
}

// scanRootMemory walks the committed read-write memory that is not part of a
// tracked object and reports every word in it.
func (runtime *initializationRuntime) scanRootMemory(index extentIndex, mark func(uint32)) error {
	memory := runtime.client.core.Memory()
	buffer := make([]byte, scanChunk)
	for _, region := range memory.CommittedRegions(armcore.PermissionReadWrite) {
		start := uint64(region.Base)
		end := start + region.Size
		// Walk the region, skipping the spans tracked objects occupy.
		position := sort.Search(len(index.starts), func(candidate int) bool {
			return uint64(index.ends[candidate]) > start
		})
		for start < end {
			gapEnd := end
			if position < len(index.starts) {
				objectStart := uint64(index.starts[position])
				if objectStart <= start {
					// Inside an object: skip past it.
					start = uint64(index.ends[position])
					position++
					continue
				}
				if objectStart < gapEnd {
					gapEnd = objectStart
				}
			}
			if err := scanRange(memory, start, gapEnd, buffer, mark); err != nil {
				return err
			}
			start = gapEnd
		}
	}
	return nil
}

// scanRange reports every aligned word in one span of guest memory.
func scanRange(memory *armcore.Memory, start, end uint64, buffer []byte, mark func(uint32)) error {
	start = (start + 3) &^ 3
	end &^= 3
	for start < end {
		length := end - start
		if length > uint64(len(buffer)) {
			length = uint64(len(buffer))
		}
		chunk := buffer[:length]
		if err := memory.Read(uint32(start), chunk); err != nil {
			return fmt.Errorf("scan KTF memory at %#x: %w", start, err)
		}
		scanWords(chunk, mark)
		start += length
	}
	return nil
}

// scanWords reports each aligned little-endian word in a buffer.
func scanWords(data []byte, mark func(uint32)) {
	for offset := 0; offset+4 <= len(data); offset += 4 {
		mark(uint32(data[offset]) | uint32(data[offset+1])<<8 |
			uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24)
	}
}

// releaseObjectMemory zeroes a dead object's bytes and returns them to the
// arena. The zeroing matters: a stale word left behind would look like a live
// reference to the next cycle's scan and keep another dead object alive.
func (runtime *initializationRuntime) releaseObjectMemory(address uint32, size uint32) error {
	zero := make([]byte, size)
	if err := runtime.client.core.Memory().Write(address, zero); err != nil {
		return fmt.Errorf("clear KTF object at %#x: %w", address, err)
	}
	runtime.arena.release(address, uint64(size))
	return nil
}

// CollectGuestObjects runs one collection cycle if the arena has grown enough
// since the last one, and reports what it reclaimed. Hosts call it at the end
// of a service round: no guest code is executing on this goroutine, every
// worker is parked with its ARM stack intact, and both are what the scan
// depends on.
//
// A worker parked inside an AOT call is fine. Its guest state is in the
// registers and stack the scan covers, and whatever its suspended Go frames
// hold is exactly what the weak bindings report.
func (client *Client) CollectGuestObjects(extraRoots []uint32) (CollectionStats, error) {
	if client == nil {
		return CollectionStats{}, nil
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil || !client.runtime.collectionDue() {
		return CollectionStats{}, nil
	}
	started := time.Now()
	stats, err := client.runtime.collectGuestObjects(extraRoots)
	if err != nil {
		return stats, err
	}
	if stats.Freed > 0 || stats.Released > 0 || stats.Lost > 0 {
		client.log("KTF collected guest objects",
			"tracked", stats.Tracked, "marked", stats.Marked,
			"released", stats.Released, "freed", stats.Freed, "lost", stats.Lost,
			"bytes", stats.Bytes, "arena", client.runtime.arena.used(),
			"micros", time.Since(started).Microseconds())
	}
	return stats, nil
}
