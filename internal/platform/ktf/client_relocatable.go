package ktf

import (
	"encoding/binary"
	"fmt"
)

// The older generation of this platform's client images are not code at their
// first byte. Three archives of a local set carry one, and each died a few
// instructions into what the loader took for the entry: the entry ran the
// header as Thumb, wrote through a word that was a relocation offset, and
// faulted.
//
// What they carry instead is a relocatable module:
//
//	u32 bssSize            what the file name's decimal suffix also names
//	u32 relocationCount
//	u32 relocations[]      segment offsets of words to rebase, first one zero
//	...segment...
//
// and the segment opens with eleven words of its own before any code:
//
//	seg[0]     the module descriptor: its class table, class count, table size
//	seg[1]     zero in all three
//	seg[2]     the end of the writable tail
//	seg[3..4]  the read-only data, ending at the module's own name table
//	seg[5]     a thunk
//	seg[6]     the static base, where the module's own pointer table starts
//	seg[7]     the end of the bytes the file carries
//	seg[8]     0x13580001
//	seg[9]     the module entry, Thumb, so odd before and after rebasing
//	seg[10]    0x13580001
//	seg[11..]  code, beginning with the same helper in all three files
//
// Every relocated word gets the segment's load address added. The header stays
// mapped in front of the segment because the segment's own offsets are
// relative to it, and the file names the BSS size in two places — the header's
// first word and the image name's decimal suffix — which is what makes the
// header recognizable. The entry is not handed it; see ExecuteModuleEntry.
//
// **The two words that decide where the segment begins are the markers, and
// they were read one word too late.** An earlier reading took the relocation
// table to start at offset 12 behind a zero word and to end at a zero
// terminator, which puts the segment four bytes further on and finds the same
// two markers around a different word — because the entry's neighbourhood is
// pointers either way. That reading's entry lands eight bytes inside the real
// entry, past `push {r4, r5, lr}` and past the first half of the
// position-independent prologue, so the module ran with a static base built out
// of whatever `r3` happened to hold. The correct reading is the one where all
// three files' entries land exactly on that push, which is what this parses:
// the relocations begin at offset 8 and their first entry is the zero that the
// other reading mistook for a header word.
//
// Three files agree on the header, the first relocation, both markers and the
// shared helper, and all of that is required here — because the alternative
// reading, a Thumb entry at the first byte, is what every other archive
// carries. Reading a newer image's first words as this header gives a
// relocation count far larger than the file, which is what keeps the two apart
// even before the markers.
//
// **The relocation list stops in front of a table the segment header bounds,
// and that table is relocated too.** The last offset the list names sits a few
// words below `seg[6]`, and `seg[6]` is exactly the static base the entry's own
// position-independent prologue computes from `pc` in all three modules;
// `seg[7]` is exactly the length of the segment the file carries. Between them
// is the module's pointer table, every word of it a segment offset — 334, 331
// and 546 words across the three, with no word outside the segment and its
// writable tail. Nothing else could make the entry's own first call
// well-formed: it reads a word out of that table and hands it on as a string
// pointer, and only the rebased word points at a string.
//
// **What the entry is handed is the platform's own callback table.** It runs
// its prologue, reads a string out of the rebased pointer table, and calls the
// first word of its argument with it — the same `getInterface(name, major,
// minor)` the current generation of images reaches through its initialization
// parameters. The name is the same in all three modules and the version asked
// for is `(-1, -1)`; the module keeps the answer in a static and returns
// success only when it is not null. So the entry runs under the platform
// rather than in front of it: see ExecuteModuleEntry, module_interface.go for
// the interface it asks for, and module_link.go for the classes it publishes
// and the context its whole runtime is reached through.
type relocatableClient struct {
	// segmentStart is where the segment begins in the file, and relocations
	// are the segment offsets of the words to rebase.
	segmentStart uint64
	relocations  []uint32
}

const (
	// relocatableHeaderWords is the fixed part of the header: the BSS size the
	// image name's suffix also carries, and the relocation count. The
	// relocation table follows immediately.
	relocatableHeaderWords = 2
	// relocatableEntryOffset is where the segment's own eleven-word header
	// keeps the module entry, flanked by the same constant on both sides in
	// every module seen. It is a Thumb address, so the word is odd before it is
	// rebased and after.
	relocatableEntryOffset = 0x24
	// relocatableMarker sits at the two words either side of the entry. It is
	// what makes the reading of a header certain rather than plausible.
	relocatableMarker = 0x13580001
	// relocatableStaticBaseOffset and relocatableSegmentEndOffset are the two
	// segment-header words that bound the module's own pointer table: the
	// static base its position-independent code addresses everything through,
	// and the end of the bytes the file carries. See "the table the relocation
	// list stops in front of" below.
	relocatableStaticBaseOffset = 0x18
	relocatableSegmentEndOffset = 0x1c
)

// parseRelocatableClient reports whether an image is one of the older
// relocatable modules and, if so, where its segment and relocations are.
// Anything that does not fit exactly is not one, because the alternative
// reading — a Thumb entry at the first byte — is what every other archive here
// carries.
func parseRelocatableClient(data []byte) (relocatableClient, bool) {
	// The header plus one relocation is the least this can be, and the first
	// relocation is read below, so anything shorter is not one of these.
	if len(data) < (relocatableHeaderWords+1)*4 {
		return relocatableClient{}, false
	}
	count := binary.LittleEndian.Uint32(data[4:])
	// The first relocation rebases the segment's own first word, so it is zero
	// in every module seen. It is checked because it is the one word of the
	// table whose value the format fixes, and reading the table one word out is
	// exactly the mistake this layout invites.
	if binary.LittleEndian.Uint32(data[8:]) != 0 {
		return relocatableClient{}, false
	}
	if count == 0 || uint64(count) > uint64(len(data))/4 {
		return relocatableClient{}, false
	}
	segmentStart := uint64(relocatableHeaderWords*4) + uint64(count)*4
	if segmentStart > uint64(len(data)) {
		return relocatableClient{}, false
	}
	segment := data[segmentStart:]
	if len(segment) < relocatableEntryOffset+8 {
		return relocatableClient{}, false
	}
	if binary.LittleEndian.Uint32(segment[relocatableEntryOffset-4:]) != relocatableMarker {
		return relocatableClient{}, false
	}
	if binary.LittleEndian.Uint32(segment[relocatableEntryOffset+4:]) != relocatableMarker {
		return relocatableClient{}, false
	}
	relocations := make([]uint32, count)
	for index := range relocations {
		relocations[index] = binary.LittleEndian.Uint32(data[relocatableHeaderWords*4+index*4:])
	}
	return relocatableClient{segmentStart: segmentStart, relocations: relocations}, true
}

// relocate rebases a copy of the whole file for a load at base. The header
// stays in front of the segment so that the segment lands where its own
// offsets say, so what moves is only what the relocation table names — and
// those offsets are the segment's, not the file's.
//
// A relocation that names a word outside the segment is refused rather than
// skipped: the table is the module's own account of itself, and one entry that
// does not fit means the reading of the header is wrong rather than that one
// word is.
func (module relocatableClient) relocate(data []byte, base uint32) ([]byte, error) {
	image := make([]byte, len(data))
	copy(image, data)
	segment := image[module.segmentStart:]
	addend := base + uint32(module.segmentStart)
	for index, offset := range module.relocations {
		if uint64(offset)+4 > uint64(len(segment)) {
			return nil, fmt.Errorf("KTF client relocation %d names offset %#x outside the %d-byte segment", index, offset, len(segment))
		}
		value := binary.LittleEndian.Uint32(segment[offset:])
		binary.LittleEndian.PutUint32(segment[offset:], value+addend)
	}
	// Both bounds are read from the file rather than from the copy, because
	// the relocation list names them: by now the copy holds their rebased
	// values, and the table's own offsets are still the segment's.
	original := data[module.segmentStart:]
	staticBase := uint64(binary.LittleEndian.Uint32(original[relocatableStaticBaseOffset:]))
	segmentEnd := uint64(binary.LittleEndian.Uint32(original[relocatableSegmentEndOffset:]))
	if staticBase%4 != 0 || segmentEnd%4 != 0 || staticBase > segmentEnd || segmentEnd > uint64(len(segment)) {
		return nil, fmt.Errorf("KTF client static base %#x and segment end %#x do not bound a table in a %d-byte segment",
			staticBase, segmentEnd, len(segment))
	}
	for offset := staticBase; offset+4 <= segmentEnd; offset += 4 {
		binary.LittleEndian.PutUint32(segment[offset:], binary.LittleEndian.Uint32(segment[offset:])+addend)
	}
	return image, nil
}

// entryAddress reads the module entry out of a relocated image.
func (module relocatableClient) entryAddress(image []byte) uint32 {
	return binary.LittleEndian.Uint32(image[module.segmentStart+relocatableEntryOffset:])
}
