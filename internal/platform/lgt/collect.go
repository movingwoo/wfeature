package lgt

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Guest object collection for the Java path.
//
// Nothing else reclaims a Java object here. An AOT title's objects come out of
// this platform's own arena, the module carries no collector of its own in the
// image, and the language's is on the handset rather than in the archive — so
// without this a session holds every object the title has ever allocated.
// java_stream.go says what that cost in practice: "nothing here ever reclaims,
// because the Java path has no collector", written against a title that
// reloads its sprite sheets from inside `paint` and filled the surface region.
//
// The design is the one in internal/platform/ktf/collect.go, which is already
// carrying real titles, and the reasoning there holds here unchanged:
//
//   - The guest side is conservative. Guest code keeps references in registers,
//     on its stacks, and in memory this platform cannot describe field by
//     field, so any word that looks like the address of a tracked object is
//     treated as one. Over-retention is the safe direction; freeing a live
//     object corrupts the guest instead.
//   - Roots are the thread registers and thread-local words, every committed
//     read-write span outside a tracked object, and the objects this platform
//     holds by name — the display, the application object, the card, a started
//     thread, a held monitor. Tracing from there reaches every live object, and
//     a group of dead objects that only reference each other is reached by
//     nothing, which is how a cycle gets collected.
//   - An object is never freed in the cycle that first finds it unreachable.
//     That grace cycle is also what makes a collection safe to run from inside
//     a platform call: an object allocated since the last cycle can be
//     condemned but not released, so a Go frame that has just built one and not
//     yet handed it over cannot lose it underneath itself.
//
// Two things differ from the KTF path, both because the object model does.
//
// An object here is two allocations — the three-word object and the block its
// fields live in (docs/lgt.md, "The three thunks every class carries") — and
// the guest holds the block's address as often as the object's, because that
// is what a field access loads first. So both spans are indexed, both name the
// same object, and freeing one frees the other.
//
// And an object's references are not all in its payload. What a stream, an
// image, a file, a vector, a widget or a wrapper stands for lives in the maps
// in javaRuntime, keyed by the object that holds it, and three kinds of edge
// exist there that no word of guest memory expresses:
//
//  1. the surface an image or a Graphics draws into, which lives in a
//     different arena and has to be released with the object that held it;
//  2. the open file a File object stands for, which has to be closed — with
//     its buffered writes flushed — rather than merely forgotten;
//  3. aliases, where two guest objects name one native thing: two Images over
//     one decoded surface, a DataOutputStream over another object's sink, a
//     stream bound to the File it was opened on. A native payload is released
//     only once nothing live names it.
//
// walkJavaPayload covers the reference-carrying half of that and
// releaseJavaPayloads the owning half.

// javaObjectRecord is one tracked Java object: the block its fields live in,
// and whether a previous cycle already found it unreachable.
type javaObjectRecord struct {
	block     uint32
	blockSize uint32
	// condemned records that a cycle has already found this object
	// unreachable. Nothing is freed on that first finding; see the grace cycle
	// above.
	condemned bool
}

// CollectionStats reports what one collection cycle did.
type CollectionStats struct {
	Tracked   int
	Marked    int
	Condemned int
	Freed     int
	Bytes     uint64
	// Surfaces and Files count the native payloads the freed objects were the
	// last holder of.
	Surfaces int
	Files    int
}

const (
	// javaCollectionFloor is the arena growth that triggers the first cycle and
	// the smallest growth that triggers a later one.
	javaCollectionFloor uint64 = 256 << 10
	// javaScanChunk bounds how much guest memory is read at once while
	// scanning.
	javaScanChunk = 64 << 10
	// javaObjectBytes is the object itself: vtable, spare, data block.
	javaObjectBytes = javaObjectWords * 4
	// javaSweepInterval is how many service rounds apart the cycles that come
	// back for a condemned set are. See javaCollectionDue.
	javaSweepInterval = 8
)

// trackJavaObject records an object allocation so the collector can find, scan
// and eventually reclaim it, and pins it for the platform call that is
// building it. See releaseJavaPins.
func (client *Client) trackJavaObject(object, block, blockSize uint32) {
	runtime := client.javaRuntimeState()
	if runtime.objects == nil {
		runtime.objects = map[uint32]javaObjectRecord{}
	}
	runtime.objects[object] = javaObjectRecord{block: block, blockSize: blockSize}
	runtime.pins = append(runtime.pins, object)
}

// javaPinMark and releaseJavaPins bracket one platform call. Everything the
// call allocates is a root until it returns, which is what makes a collection
// raised by an allocation failure safe to run from inside one: the objects a
// half-finished call holds in Go locals are not yet named by any guest word,
// and the scan cannot see a Go frame.
func (client *Client) javaPinMark() int {
	if client.javaRun == nil {
		return 0
	}
	return len(client.javaRun.pins)
}

func (client *Client) releaseJavaPins(mark int) {
	if client.javaRun == nil || mark > len(client.javaRun.pins) {
		return
	}
	client.javaRun.pins = client.javaRun.pins[:mark]
}

// javaCollectionDue reports whether another cycle is worth running.
//
// Growth is the ordinary trigger. The second clause is what makes the grace
// cycle finish: a condemned object's bytes are still outstanding, so a title
// that fills the arena with garbage and then stops allocating never grows the
// arena again and never reaches the cycle that would have freed it. Measured on
// a local title, the first cycle condemned 980 objects of 2168 and the arena
// then sat still for the rest of the run — 296 KiB that growth alone was never
// going to give back.
//
// The interval is what keeps that from becoming a cycle every tick. A title
// that allocates steadily condemns something every time it is asked, so the
// pending set is never empty and the follow-up is always due; one local title
// ran 400 cycles in 400 ticks at about a millisecond each, which is a tenth of
// a frame spent on a handful of objects. Coming back every eighth tick instead
// costs an eighth of that and delays a release by at most eight ticks, which
// nothing observes.
func (client *Client) javaCollectionDue() bool {
	runtime := client.javaRun
	if runtime == nil {
		return false
	}
	if client.arena.used() >= runtime.collectAt {
		return true
	}
	return runtime.condemned > 0 && runtime.sinceCollection >= javaSweepInterval
}

// scheduleNextJavaCollection arms the next cycle a fixed amount of growth
// ahead of what survived this one. Growing the trigger with the arena instead
// would let the arena keep climbing, because an object is only freed a cycle
// after it stops being reachable and a doubling trigger outruns that lag.
func (client *Client) scheduleNextJavaCollection() {
	if client.javaRun != nil {
		client.javaRun.collectAt = client.arena.used() + javaCollectionFloor
	}
}

// javaExtentIndex is the spans tracked objects occupy, in address order, each
// naming the object it belongs to. There are two per object — the object and
// its field block — because the guest holds either address.
type javaExtentIndex struct {
	starts []uint32
	ends   []uint32
	owners []uint32
	// low and high bound every tracked span. A collection reads every word of
	// committed guest memory and asks this question of each one, and almost
	// none of them are object pointers — they are code addresses, small
	// integers, packed bytes, zero. Two compares reject those before the
	// search runs, which is what keeps a collection proportional to the
	// pointers a heap holds rather than to the words it spans.
	low  uint32
	high uint32
}

type javaExtent struct {
	start uint32
	end   uint32
	owner uint32
}

func (client *Client) buildJavaExtentIndex() javaExtentIndex {
	runtime := client.javaRun
	extents := make([]javaExtent, 0, len(runtime.objects)*2)
	for object, record := range runtime.objects {
		extents = append(extents, javaExtent{start: object, end: object + javaObjectBytes, owner: object})
		if record.blockSize > 0 {
			extents = append(extents, javaExtent{
				start: record.block, end: record.block + record.blockSize, owner: object})
		}
	}
	slices.SortFunc(extents, func(a, b javaExtent) int {
		switch {
		case a.start < b.start:
			return -1
		case a.start > b.start:
			return 1
		}
		return 0
	})
	index := javaExtentIndex{
		starts: make([]uint32, len(extents)),
		ends:   make([]uint32, len(extents)),
		owners: make([]uint32, len(extents)),
	}
	for position, extent := range extents {
		index.starts[position] = extent.start
		index.ends[position] = extent.end
		index.owners[position] = extent.owner
		index.high = max(index.high, extent.end)
	}
	if len(extents) > 0 {
		index.low = extents[0].start
	}
	return index
}

// lookup resolves a guest word to the object containing it, so an interior
// pointer — the guest holds a field's address as often as an object's head —
// still keeps its object alive.
func (index javaExtentIndex) lookup(word uint32) (uint32, bool) {
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
	return index.owners[position], true
}

// CollectJavaObjects runs one collection cycle if the arena has grown enough
// since the last one, and reports what it reclaimed. A Host calls it at the end
// of a service round: no platform call is in flight on this goroutine, every
// guest thread is parked with its ARM stack intact, and both are what the scan
// depends on.
//
// A worker parked inside a guest call is fine. Its guest state is in the
// registers and stack the scan covers.
func (client *Client) CollectJavaObjects(extraRoots []uint32) (CollectionStats, error) {
	if client == nil || client.collectorOff || client.javaRun == nil {
		return CollectionStats{}, nil
	}
	// Nothing is part-built here: this is the round's safepoint, so the pins a
	// platform call would have held are all released. It happens on every round
	// rather than on every cycle, because a platform call is not the only thing
	// that allocates — a paint builds a Graphics outside one — and a pin
	// nothing drops would grow for the life of the session.
	client.javaRun.pins = client.javaRun.pins[:0]
	client.javaRun.sinceCollection++
	if !client.javaCollectionDue() {
		return CollectionStats{}, nil
	}
	client.javaRun.sinceCollection = 0
	return client.collectJavaObjects(extraRoots)
}

// collectJavaObjects runs one mark-and-sweep cycle unconditionally.
func (client *Client) collectJavaObjects(extraRoots []uint32) (CollectionStats, error) {
	runtime := client.javaRun
	stats := CollectionStats{Tracked: len(runtime.objects)}
	if len(runtime.objects) == 0 {
		client.scheduleNextJavaCollection()
		return stats, nil
	}
	started := time.Now()
	index := client.buildJavaExtentIndex()
	marked := make(map[uint32]bool, len(runtime.objects))
	pending := make([]uint32, 0, 64)

	mark := func(word uint32) {
		owner, ok := index.lookup(word)
		if !ok || marked[owner] {
			return
		}
		marked[owner] = true
		pending = append(pending, owner)
	}

	// Addresses the Host itself will write to after the cycle, whether or not
	// any guest word names them. A frozen cheat address is the case that
	// matters: the freeze rewrites it every tick.
	for _, root := range extraRoots {
		mark(root)
	}
	client.markJavaPlatformRoots(mark)

	// Registers and thread-local words of every thread that can resume.
	for _, thread := range client.collectionThreads() {
		context := thread.Context()
		for _, register := range context.Registers {
			mark(register)
		}
		for _, word := range client.core.ThreadLocalWords(thread) {
			mark(word)
		}
	}

	// Every committed read-write span that is not itself a tracked object:
	// the guest stacks, the module's statics, the guest heap, and the platform
	// structures this arena holds alongside the objects.
	if err := client.scanJavaRootMemory(index, mark); err != nil {
		return stats, err
	}

	// Trace: a live object's own words reach the objects it references, and
	// its native payload reaches the ones only this platform knows about.
	buffer := make([]byte, 0, 1024)
	for len(pending) > 0 {
		object := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if err := client.scanJavaObjectWords(object, &buffer, mark); err != nil {
			return stats, err
		}
		client.walkJavaPayload(object, mark)
	}
	stats.Marked = len(marked)

	if err := client.sweepJavaObjects(marked, &stats); err != nil {
		return stats, err
	}
	client.scheduleNextJavaCollection()
	client.collectNanos += time.Since(started).Nanoseconds()
	runtime.collected.Tracked = stats.Tracked
	runtime.collected.Marked = stats.Marked
	runtime.collected.Condemned += stats.Condemned
	runtime.collected.Freed += stats.Freed
	runtime.collected.Bytes += stats.Bytes
	runtime.collected.Surfaces += stats.Surfaces
	runtime.collected.Files += stats.Files
	runtime.collections++
	if client.logger != nil && (stats.Freed > 0 || stats.Condemned > 0) {
		client.logger.Debug("LGT collected java objects",
			"tracked", stats.Tracked, "marked", stats.Marked,
			"condemned", stats.Condemned, "freed", stats.Freed,
			"bytes", stats.Bytes, "surfaces", stats.Surfaces, "files", stats.Files,
			"arena", client.arena.used(), "micros", time.Since(started).Microseconds())
	}
	return stats, nil
}

// markJavaPlatformRoots marks the objects this platform holds by name. Every
// one of them can be handed back to the guest without any guest word naming it
// first, which is what makes it a root rather than an edge.
func (client *Client) markJavaPlatformRoots(mark func(uint32)) {
	runtime := client.javaRun
	// Objects a platform call has built and not yet handed over.
	for _, pinned := range runtime.pins {
		mark(pinned)
	}
	// The application object, the card on the display, the Graphics a paint is
	// handed, and the platform's own thread object.
	mark(runtime.jlet)
	mark(runtime.card)
	mark(runtime.screenGraphics)
	mark(runtime.mainThread)
	// One object for the life of the title, answered by name.
	for _, object := range runtime.singletons {
		mark(object)
	}
	// Work `Display.callSerially` has been handed and not yet run.
	for _, object := range runtime.serial {
		mark(object)
	}
	// A started thread runs whether or not the title kept a reference to it,
	// and so does whatever it was given to run.
	for object, thread := range runtime.threads {
		if thread == nil {
			continue
		}
		if thread.worker != nil && !thread.worker.done {
			mark(object)
			mark(thread.runnable)
		}
	}
	// A held lock names the object it is on: a thread is inside its
	// synchronized body, and the guest resumes there.
	for object, monitor := range runtime.monitors {
		if monitor != nil && monitor.count > 0 {
			mark(object)
		}
	}
}

// scanJavaObjectWords reports every word of one object: the object itself and
// the block its fields live in.
//
// The block is scanned whatever the object is, including the primitive arrays
// a title spends most of its heap on. Their bytes are data, and a word of them
// that happens to land inside a tracked span retains an object that is dead —
// which is the conservative direction this collector is written in, and the
// alternative is trusting a type the platform did not lay out.
func (client *Client) scanJavaObjectWords(object uint32, buffer *[]byte, mark func(uint32)) error {
	record, tracked := client.javaRun.objects[object]
	if !tracked {
		return nil
	}
	if err := client.readJavaSpan(object, javaObjectBytes, buffer, mark); err != nil {
		return err
	}
	if record.blockSize == 0 {
		return nil
	}
	return client.readJavaSpan(record.block, record.blockSize, buffer, mark)
}

func (client *Client) readJavaSpan(address, size uint32, buffer *[]byte, mark func(uint32)) error {
	if int(size) > cap(*buffer) {
		*buffer = make([]byte, size)
	}
	body := (*buffer)[:size]
	if err := client.core.Memory().Read(address, body); err != nil {
		return fmt.Errorf("scan LGT java object at %#x: %w", address, err)
	}
	scanJavaWords(body, mark)
	return nil
}

// walkJavaPayload follows the edges an object's guest words do not carry: what
// this platform holds on the object's behalf, and the other objects that state
// names. Missing one of these is how a live object gets freed, because nothing
// in guest memory would have named it.
func (client *Client) walkJavaPayload(object uint32, mark func(uint32)) {
	runtime := client.javaRun
	// A vector holds its elements here rather than in a guest array.
	for _, element := range runtime.vectors[object] {
		mark(element)
	}
	// A container holds its children, and an input-method handler the listener
	// it was told to hand characters to.
	if widget := runtime.widgets[object]; widget != nil {
		for _, child := range widget.children {
			mark(child)
		}
		mark(widget.listener)
	}
	// A wrapper stands for another object's sink rather than a second one.
	mark(runtime.wrapped[object])
	// A stream or a sink bound to the File it drains into or was opened on.
	mark(runtime.sinkFiles[object])
	mark(runtime.streamFiles[object])
	// A thread object names what it was given to run.
	if thread := runtime.threads[object]; thread != nil {
		mark(thread.runnable)
	}
}

// sweepJavaObjects condemns what this cycle did not reach and frees what the
// previous one condemned.
func (client *Client) sweepJavaObjects(marked map[uint32]bool, stats *CollectionStats) error {
	runtime := client.javaRun
	dead := make([]uint32, 0, 16)
	for object, record := range runtime.objects {
		if marked[object] {
			if record.condemned {
				record.condemned = false
				runtime.objects[object] = record
			}
			continue
		}
		if !record.condemned {
			record.condemned = true
			runtime.objects[object] = record
			stats.Condemned++
			continue
		}
		dead = append(dead, object)
	}
	// Address order, so a run of neighbouring objects coalesces back into one
	// arena block rather than depending on map iteration order for it.
	slices.Sort(dead)
	for _, object := range dead {
		record := runtime.objects[object]
		if err := client.releaseJavaObjectMemory(object, record); err != nil {
			return err
		}
		delete(runtime.objects, object)
		stats.Freed++
		stats.Bytes += uint64(javaObjectBytes) + uint64(record.blockSize)
	}
	// The payloads go after the objects, so the aliasing question — is anything
	// still holding this surface, this file — is asked once, against what
	// survived, rather than once per object against a set still being emptied.
	client.releaseJavaPayloads(dead, stats)
	// What this cycle condemned is what the next one has to come back for. See
	// javaCollectionDue.
	runtime.condemned = 0
	for _, record := range runtime.objects {
		if record.condemned {
			runtime.condemned++
		}
	}
	return nil
}

// releaseJavaObjectMemory zeroes a dead object's bytes and returns both of its
// blocks to the arena. The zeroing matters: a stale word left behind would look
// like a live reference to the next cycle's scan and keep another dead object
// alive.
func (client *Client) releaseJavaObjectMemory(object uint32, record javaObjectRecord) error {
	memory := client.core.Memory()
	if record.blockSize > 0 {
		if err := memory.Write(record.block, make([]byte, record.blockSize)); err != nil {
			return fmt.Errorf("clear LGT java object block at %#x: %w", record.block, err)
		}
		client.arena.release(record.block)
	}
	if err := memory.Write(object, make([]byte, javaObjectBytes)); err != nil {
		return fmt.Errorf("clear LGT java object at %#x: %w", object, err)
	}
	client.arena.release(object)
	return nil
}

// releaseJavaPayloads drops what this platform held on behalf of the objects
// that have just been freed, and releases the native things nothing live names
// any more.
func (client *Client) releaseJavaPayloads(dead []uint32, stats *CollectionStats) {
	runtime := client.javaRun
	surfaces := make([]uint32, 0, 4)
	files := make([]uint32, 0, 4)
	for _, object := range dead {
		if handle, ok := runtime.images[object]; ok {
			surfaces = append(surfaces, handle)
		}
		if graphics := runtime.graphics[object]; graphics != nil && graphics.surface != 0 {
			surfaces = append(surfaces, graphics.surface)
		}
		if handle, ok := runtime.files[object]; ok {
			files = append(files, handle)
		}
		delete(runtime.strings, object)
		delete(runtime.random, object)
		delete(runtime.streams, object)
		delete(runtime.images, object)
		delete(runtime.files, object)
		delete(runtime.graphics, object)
		delete(runtime.threads, object)
		delete(runtime.monitors, object)
		delete(runtime.vectors, object)
		delete(runtime.calendars, object)
		delete(runtime.sinks, object)
		delete(runtime.wrapped, object)
		delete(runtime.sinkFiles, object)
		delete(runtime.streamFiles, object)
		delete(runtime.dates, object)
		delete(runtime.databases, object)
		delete(runtime.widgets, object)
	}
	for _, handle := range surfaces {
		if client.javaSurfaceHeld(handle) {
			continue
		}
		buffer := client.framebuffer(handle)
		if buffer == nil || buffer.screen {
			continue
		}
		client.releaseSurface(buffer)
		stats.Surfaces++
	}
	for _, handle := range files {
		if client.javaFileHeld(handle) {
			continue
		}
		if client.closeFileHandle(handle) {
			stats.Files++
		}
	}
}

// javaSurfaceHeld reports whether anything still names a surface. Two Images
// over one decoded picture are the case that matters — see newSharedJavaImage,
// where sharing the pixels is the whole point — and the decode cache itself is
// a holder, because the next Image built from that picture is handed the same
// surface.
func (client *Client) javaSurfaceHeld(handle uint32) bool {
	runtime := client.javaRun
	if client.screen != nil && client.screen.handle == handle {
		return true
	}
	for _, held := range runtime.images {
		if held == handle {
			return true
		}
	}
	for _, graphics := range runtime.graphics {
		if graphics != nil && graphics.surface == handle {
			return true
		}
	}
	for _, cached := range runtime.decodedImages {
		if cached == handle {
			return true
		}
	}
	return false
}

// javaFileHeld reports whether another File object still stands for the same
// open file.
func (client *Client) javaFileHeld(handle uint32) bool {
	for _, held := range client.javaRun.files {
		if held == handle {
			return true
		}
	}
	return false
}

// collectionThreads lists the ARM threads whose state can still name an
// object: this platform's own thread and every guest worker.
func (client *Client) collectionThreads() []*armcore.Thread {
	threads := make([]*armcore.Thread, 0, 4)
	if client.thread != nil {
		threads = append(threads, client.thread)
	}
	if client.javaRun != nil {
		for _, worker := range client.javaRun.workers {
			if worker != nil && worker.armThread != nil {
				threads = append(threads, worker.armThread)
			}
		}
	}
	return threads
}

// scanJavaRootMemory walks the committed read-write memory that is not part of
// a tracked object and reports every word in it.
//
// The surface region is not walked. Nothing there is a word a title wrote: a
// Java title never receives a surface's address and never writes a pixel
// through guest memory — see the note on framebuffer.drawnHere — so the region
// holds pixels and nothing else, and reading sixteen megabytes of them per
// cycle would buy only false retention.
func (client *Client) scanJavaRootMemory(index javaExtentIndex, mark func(uint32)) error {
	memory := client.core.Memory()
	buffer := make([]byte, javaScanChunk)
	for _, region := range memory.CommittedRegions(armcore.PermissionReadWrite) {
		start := uint64(region.Base)
		end := start + region.Size
		if start >= uint64(surfaceBase) && start < uint64(surfaceBase)+surfaceSize {
			continue
		}
		// Walk the region, skipping the spans tracked objects occupy.
		position := sort.Search(len(index.starts), func(candidate int) bool {
			return uint64(index.ends[candidate]) > start
		})
		for start < end {
			gapEnd := end
			if position < len(index.starts) {
				spanStart := uint64(index.starts[position])
				if spanStart <= start {
					// Inside a tracked span: skip past it.
					start = uint64(index.ends[position])
					position++
					continue
				}
				if spanStart < gapEnd {
					gapEnd = spanStart
				}
			}
			if err := scanJavaRange(memory, start, gapEnd, buffer, mark); err != nil {
				return err
			}
			start = gapEnd
		}
	}
	return nil
}

// scanJavaRange reports every aligned word in one span of guest memory.
func scanJavaRange(memory *armcore.Memory, start, end uint64, buffer []byte, mark func(uint32)) error {
	start = (start + 3) &^ 3
	end &^= 3
	for start < end {
		length := end - start
		if length > uint64(len(buffer)) {
			length = uint64(len(buffer))
		}
		chunk := buffer[:length]
		if err := memory.Read(uint32(start), chunk); err != nil {
			return fmt.Errorf("scan LGT memory at %#x: %w", start, err)
		}
		scanJavaWords(chunk, mark)
		start += length
	}
	return nil
}

// scanJavaWords reports each aligned little-endian word in a buffer.
func scanJavaWords(data []byte, mark func(uint32)) {
	for offset := 0; offset+4 <= len(data); offset += 4 {
		mark(uint32(data[offset]) | uint32(data[offset+1])<<8 |
			uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24)
	}
}

// collectForAllocation is the second trigger. Growth alone does not save a
// title that reaches the end of a region between two cycles: the arena is
// asked for a block it cannot give while the objects that would have made room
// are already unreachable and merely waiting for the next round. So a refused
// allocation runs a cycle of its own and the caller asks once more.
//
// Running here means running from inside a platform call, which is why the
// grace cycle and the call pins exist: an object this call has built and not
// yet handed over is a root, and an object allocated since the last cycle can
// be condemned but never freed. It reports whether anything was released, so a
// caller only retries when the arena actually changed.
func (client *Client) collectForAllocation() bool {
	if client == nil || client.collectorOff || client.javaRun == nil || client.collecting {
		return false
	}
	client.collecting = true
	defer func() { client.collecting = false }()
	stats, err := client.collectJavaObjects(nil)
	if err != nil {
		// A failed collection is not the allocation's failure to report. The
		// allocation reports its own; this one is worth a line of its own
		// because a scan that cannot read guest memory is a fault in itself.
		if client.logger != nil {
			client.logger.Debug("LGT collection on allocation failure failed", "error", err)
		}
		return false
	}
	if client.logger != nil {
		client.logger.Debug("LGT collected on allocation failure",
			"tracked", stats.Tracked, "freed", stats.Freed, "bytes", stats.Bytes,
			"surfaces", stats.Surfaces)
	}
	return stats.Freed > 0
}
