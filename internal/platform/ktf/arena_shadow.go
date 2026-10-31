package ktf

import ()

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
