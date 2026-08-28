package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A block a title frees and keeps writing into was harmless while free did
// nothing, because the address was never handed out twice. Now that free
// really returns space, that stale write lands in whatever is allocated there
// next — a Java object, another buffer — and surfaces far from its cause, as a
// wrong sprite or a fault at an address the game never computed, on no
// particular screen. A sweep is the wrong instrument for that: it can only
// find the fault if the fault happens to become visible during the sweep.
//
// So a released block is remembered — a copy of its bytes is kept on the Host
// side — and the copy is compared against guest memory where the arena hands
// those bytes out again. Identical means nothing wrote there while the block
// was free; a difference is a write that happened after the release, and it
// names the block, the offset and what was written — enough to put a
// watchpoint on the address and catch the writer itself on the next run.
//
// **The detector must not change what the guest reads.** It used to work the
// other way round: the released block was filled with a byte no guest value
// looks like, and the fill was checked on reuse. That found the same writes
// and cost less memory, and it also destroyed the block's contents — which a
// handset's allocator does not do, and neither does this arena in a release
// build. A title that frees a structure and keeps reading it therefore ran in
// release and faulted in debug, at an address made of the fill (`0xdfdfdfdf`
// and offsets from it) that named nothing a reader could act on. Two local
// titles do exactly that, and the server the archives are played on is a debug
// build, so the instrument was manufacturing the failures it was there to
// observe. A copy answers the same question and leaves the guest's view of a
// freed block exactly as a release build leaves it.
//
// Two details decide whether this works, and both are why it lives here rather
// than in MC_knlFree. The check has to know a block came from the free list or
// from below the high-water mark rather than from untouched space, which only
// the arena knows; and the object collector releases into the same arena, so a
// copy taken on the guest's frees alone would read every collected object as a
// use after free.
//
// Both halves walk the bytes of every block released and every block reused,
// and the heaviest local title frees 21,147 times in 400 ticks, so they are
// installed in debug builds alone.

// installArenaShadow attaches the detector to the arena. A release build has
// no detector, and its arena behaves exactly as before.
func (runtime *initializationRuntime) installArenaShadow() {
	if !backend.DebugBuild() {
		return
	}
	runtime.arenaShadow = newArenaShadow(runtime.arena.base, runtime.arena.limit-uint64(runtime.arena.base))
	runtime.arena.recordReleased = runtime.recordReleasedBlock
	runtime.arena.checkReused = runtime.checkReusedBlock
}

// arenaShadow is the Host-side copy of every arena byte that is currently
// free. It is one run of bytes covering the arena from its base rather than a
// map of blocks, because the ranges do not line up: releases coalesce, and an
// allocation may take part of a block or run across several of them, so what a
// check is handed is a range rather than a block that was released under that
// name.
//
// It grows to what the arena has actually handed out rather than to what the
// arena could hand out. The region is 64MB and a session's whole working set
// is a fraction of that, so reserving the span up front would be the
// detector's largest cost by far — on a Host running several debug sessions,
// larger than everything else they hold together.
type arenaShadow struct {
	base  uint32
	limit uint64
	bytes []byte
}

func newArenaShadow(base uint32, size uint64) *arenaShadow {
	return &arenaShadow{base: base, limit: size}
}

// window answers the shadow bytes for one guest range, growing the copy to
// reach it, and reports false for a range that falls outside the arena.
func (shadow *arenaShadow) window(address uint32, size uint64) ([]byte, bool) {
	if shadow == nil || address < shadow.base {
		return nil, false
	}
	start := uint64(address - shadow.base)
	end := start + size
	if end < start || end > shadow.limit {
		return nil, false
	}
	if end > uint64(len(shadow.bytes)) {
		grown := make([]byte, min(max(end, uint64(len(shadow.bytes))*2), shadow.limit))
		copy(grown, shadow.bytes)
		shadow.bytes = grown
	}
	return shadow.bytes[start:end], true
}

// recordReleasedBlock copies a block that has just gone back to the arena.
func (runtime *initializationRuntime) recordReleasedBlock(address uint32, size uint64) {
	runtime.shadowedBlocks++
	window, ok := runtime.arenaShadow.window(address, size)
	if !ok {
		runtime.countDiagnostic("arena shadow error")
		return
	}
	if err := runtime.client.core.Memory().Read(address, window); err != nil {
		runtime.countDiagnostic("arena shadow error")
	}
}

// checkReusedBlock reports the first byte of a block the arena is about to hand
// out again that no longer holds what was released. Reporting the first one
// alone is deliberate: one write is enough to name the address to watch, and a
// corrupted block would otherwise produce thousands of events describing the
// same fault.
func (runtime *initializationRuntime) checkReusedBlock(address uint32, size uint64) {
	runtime.checkedBlocks++
	window, ok := runtime.arenaShadow.window(address, size)
	if !ok {
		runtime.countDiagnostic("arena shadow error")
		return
	}
	if cap(runtime.shadowWindow) < len(window) {
		runtime.shadowWindow = make([]byte, len(window))
	}
	current := runtime.shadowWindow[:len(window)]
	if err := runtime.client.core.Memory().Read(address, current); err != nil {
		runtime.countDiagnostic("arena shadow error")
		return
	}
	offset, written := firstDifference(window, current)
	if written == nil {
		return
	}
	runtime.reportArenaUseAfterFree(address, size, offset, written)
}

// firstDifference answers where two readings of the same range stop agreeing,
// along with what the second one holds there. A nil answer means they agree.
func firstDifference(released, current []byte) (uint64, []byte) {
	for index := range released {
		if released[index] == current[index] {
			continue
		}
		return uint64(index), current[index:]
	}
	return 0, nil
}

// reportArenaUseAfterFree names one surviving write. The bytes are carried
// because what was written is often what identifies the writer: a small
// integer, a pointer into the image, a run of text. Everything that varies is
// written after the address, so the collapsed form of the name — which is what
// a run past the name budget keeps — stays one countable event.
func (runtime *initializationRuntime) reportArenaUseAfterFree(address uint32, size, offset uint64, written []byte) {
	if len(written) > 8 {
		written = written[:8]
	}
	runtime.countDiagnostic(fmt.Sprintf("arena use after free @%#x in %#x+%d: % x",
		address+uint32(offset), address, size, written))
}
