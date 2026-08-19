package armcore

// Reaching a validated span's bytes where they are.
//
// A stand-in has already done, once, everything a guest access does per byte:
// it proved the whole span readable or writable before anything moved. Going
// on to call the sized accessors per pixel then pays that proof again — a
// thread-local test, a page lookup, a permission compare and a bounds check —
// for a span whose every byte is already known to be reachable. That is why
// 28.6% of a title's instructions became about 17% of its time rather than
// disappearing (`docs/armcore.md`, "Standing in for the loops a rasteriser is
// made of").
//
// rawSpan is the way out of that: it answers the page's own bytes for as much
// of the span as sits in one page, so the loop above it indexes a slice. What
// keeps it honest is that it answers nil for every case the sized accessors
// handle and this cannot:
//
//   - a watched address, where a store has to be recorded per store. The test
//     is the whole memory's watch count, not the span, because a stand-in that
//     silently skipped a watch would be the one bug watchpoints exist to find.
//   - a thread-local word, whose value is per guest thread and does not live in
//     the page at all.
//   - a page with no storage committed. A read of one answers zero and a write
//     to one has to commit it, which is the general path's business; a caller
//     that gets nil falls back to it.
//
// Both callers keep the checked path for whatever this refuses, so a refusal
// costs a slower stand-in and never a wrong one.

// rawSpan answers the bytes of the page holding address, from address to the
// end of the page or of length, whichever comes first, or nil where the direct
// route does not apply. The caller has already validated the whole span for
// the access it is making; write says the span is being written, which commits
// the page's storage and retires its decode cache.
func (memory *Memory) rawSpan(address, length uint32, write bool) []byte {
	if length == 0 || memory.watchCount > 0 {
		return nil
	}
	if memory.activeThreadLocal != nil && memory.threadLocalCandidate(address, int(length)) {
		return nil
	}
	offset := address & memoryPageMask
	run := uint32(memoryPageSize) - offset
	if run > length {
		run = length
	}
	page := memory.pageAt(address)
	if page == nil || page.data == nil {
		return nil
	}
	if write {
		// Once per page rather than once per store: the guest may be writing
		// over code, and what a decode cache cannot survive is bytes changing
		// underneath it, not how many times they changed.
		page.discardDecoded()
	}
	return page.data[offset : offset+run]
}
