package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
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
// Marking is what turns it into something a run decides rather than notices. A
// released block is filled with a byte no guest value looks like, and the fill
// is read back where the arena hands those bytes out again. Intact means
// nothing wrote there while the block was free; a difference is a write that
// happened after the release, and it names the block, the offset and what was
// written — enough to put a watchpoint on the address and catch the writer
// itself on the next run.
//
// Two details decide whether this works, and both are why it lives here rather
// than in MC_knlFree. The check has to know a block came from the free list or
// from below the high-water mark rather than from untouched space, which only
// the arena knows; and the object collector releases into the same arena, so a
// fill applied to the guest's frees alone would read every collected object as
// a use after free.
//
// Both halves walk the bytes of every block released and every block reused,
// and the heaviest local title frees 21,147 times in 400 ticks, so they are
// installed in debug builds alone.
const (
	// arenaPoisonByte repeats into 0xdfdfdfdf, which is above the arena and so
	// is rejected by the collector's conservative scan before its search runs.
	// A pattern that looked like an address inside the arena would keep dead
	// objects alive for as long as the poisoned block stayed free.
	arenaPoisonByte = 0xdf
	// arenaPoisonWindow bounds the buffer either half works through, so the
	// cost of marking a block is a fixed window rather than a copy of it.
	arenaPoisonWindow = 4096
)

var arenaPoisonPattern = repeatedPoison()

func repeatedPoison() [arenaPoisonWindow]byte {
	var pattern [arenaPoisonWindow]byte
	for index := range pattern {
		pattern[index] = arenaPoisonByte
	}
	return pattern
}

// installArenaPoison attaches the detector to the arena. A release build has
// no detector, and its arena behaves exactly as before.
func (runtime *initializationRuntime) installArenaPoison() {
	if !backend.DebugBuild() {
		return
	}
	runtime.arena.poisonReleased = runtime.poisonReleasedBlock
	runtime.arena.checkReused = runtime.checkReusedBlock
}

// poisonReleasedBlock fills a block that has just gone back to the arena.
func (runtime *initializationRuntime) poisonReleasedBlock(address uint32, size uint64) {
	runtime.poisonedBlocks++
	if err := poisonGuestBlock(runtime.client.core.Memory(), address, size); err != nil {
		runtime.countDiagnostic("arena poison error")
	}
}

// checkReusedBlock reports the first byte of a block the arena is about to hand
// out again that no longer holds the fill. Reporting the first one alone is
// deliberate: one write is enough to name the address to watch, and a corrupted
// block would otherwise produce thousands of events describing the same fault.
func (runtime *initializationRuntime) checkReusedBlock(address uint32, size uint64) {
	runtime.checkedBlocks++
	if cap(runtime.poisonWindow) < arenaPoisonWindow {
		runtime.poisonWindow = make([]byte, arenaPoisonWindow)
	}
	offset, written, err := checkGuestPoison(runtime.client.core.Memory(), runtime.poisonWindow, address, size)
	if err != nil {
		runtime.countDiagnostic("arena poison error")
		return
	}
	if written == nil {
		return
	}
	runtime.reportArenaUseAfterFree(address, size, offset, written)
}

// poisonGuestBlock writes the pattern over one guest range in bounded windows,
// so marking a block costs a fixed buffer rather than a copy of the block.
func poisonGuestBlock(memory *armcore.Memory, address uint32, size uint64) error {
	for offset := uint64(0); offset < size; {
		span := min(size-offset, uint64(len(arenaPoisonPattern)))
		if err := memory.Write(address+uint32(offset), arenaPoisonPattern[:span]); err != nil {
			return err
		}
		offset += span
	}
	return nil
}

// checkGuestPoison reads a marked range back and answers where the mark first
// stopped holding, along with the bytes found there. A nil answer means the
// whole range survived untouched.
func checkGuestPoison(memory *armcore.Memory, window []byte, address uint32, size uint64) (uint64, []byte, error) {
	for offset := uint64(0); offset < size; {
		span := min(size-offset, uint64(len(window)))
		read := window[:span]
		if err := memory.Read(address+uint32(offset), read); err != nil {
			return 0, nil, err
		}
		for index, value := range read {
			if value == arenaPoisonByte {
				continue
			}
			return offset + uint64(index), read[index:], nil
		}
		offset += span
	}
	return 0, nil, nil
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
