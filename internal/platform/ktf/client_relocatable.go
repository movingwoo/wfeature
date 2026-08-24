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
//	u32 zero
//	u32 relocations[]      segment offsets of words to rebase
//	u32 zero               terminator
//	...segment...
//
// and the segment opens with nine words of its own before any code:
//
//	seg[0..5]  addresses, one of them the end of the segment
//	seg[6]     0x13580001
//	seg[7]     the module entry, Thumb, so odd before and after rebasing
//	seg[8]     0x13580001
//	seg[9..]   code, beginning with the same helper in all three files
//
// Every relocated word gets the segment's load address added. The header stays
// mapped in front of the segment because the entry is handed a pointer to it,
// which is where the entry reads the BSS size back from.
//
// Three files agree on the header, the terminator, both markers and the shared
// helper, and all of that is required here — because the alternative reading, a
// Thumb entry at the first byte, is what every other archive carries. Reading a
// newer image's first words as this header gives a relocation count far larger
// than the file, which is what keeps the two apart even before the markers.
//
// **This gets all three to the same place and no further.** The entry runs its
// first twenty-odd instructions and stops on a call through the first word of
// what it was handed, so the argument is a table of functions rather than the
// header alone. What that table holds is not known; see docs/ktf.md.
type relocatableClient struct {
	// segmentStart is where the segment begins in the file, and relocations
	// are the segment offsets of the words to rebase.
	segmentStart uint64
	relocations  []uint32
}

const (
	// relocatableHeaderWords is the fixed part of the header: the BSS size the
	// entry reads back through its argument, the relocation count, and a zero.
	relocatableHeaderWords = 3
	// relocatableEntryOffset is where the segment's own nine-word header keeps
	// the module entry, flanked by the same constant on both sides in every
	// module seen. It is a Thumb address, so the word is odd before it is
	// rebased and after.
	relocatableEntryOffset = 0x1c
	// relocatableMarker sits at the two words either side of the entry. It is
	// what makes the reading of a header certain rather than plausible.
	relocatableMarker = 0x13580001
)

// parseRelocatableClient reports whether an image is one of the older
// relocatable modules and, if so, where its segment and relocations are.
// Anything that does not fit exactly is not one, because the alternative
// reading — a Thumb entry at the first byte — is what every other archive here
// carries.
func parseRelocatableClient(data []byte) (relocatableClient, bool) {
	if len(data) < relocatableHeaderWords*4 {
		return relocatableClient{}, false
	}
	count := binary.LittleEndian.Uint32(data[4:])
	if binary.LittleEndian.Uint32(data[8:]) != 0 {
		return relocatableClient{}, false
	}
	if count == 0 || uint64(count) > uint64(len(data))/4 {
		return relocatableClient{}, false
	}
	tableEnd := uint64(relocatableHeaderWords*4) + uint64(count)*4
	if tableEnd+4 > uint64(len(data)) {
		return relocatableClient{}, false
	}
	if binary.LittleEndian.Uint32(data[tableEnd:]) != 0 {
		return relocatableClient{}, false
	}
	segmentStart := tableEnd + 4
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
// stays in front of the segment because the entry is handed a pointer to it and
// reads the BSS size back out of its first word, so what moves is only what the
// relocation table names — and those offsets are the segment's, not the file's.
//
// A relocation that names a word outside the segment is refused rather than
// skipped: the table is the module's own account of itself, and one entry that
// does not fit means the reading of the header is wrong rather than that one
// word is.
func (module relocatableClient) relocate(data []byte, base uint32) ([]byte, error) {
	image := make([]byte, len(data))
	copy(image, data)
	segment := image[module.segmentStart:]
	for index, offset := range module.relocations {
		if uint64(offset)+4 > uint64(len(segment)) {
			return nil, fmt.Errorf("KTF client relocation %d names offset %#x outside the %d-byte segment", index, offset, len(segment))
		}
		value := binary.LittleEndian.Uint32(segment[offset:])
		binary.LittleEndian.PutUint32(segment[offset:], value+base+uint32(module.segmentStart))
	}
	return image, nil
}

// entryAddress reads the module entry out of a relocated image.
func (module relocatableClient) entryAddress(image []byte) uint32 {
	return binary.LittleEndian.Uint32(image[module.segmentStart+relocatableEntryOffset:])
}
