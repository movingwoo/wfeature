package ktf

import (
	"encoding/binary"
	"testing"
)

// syntheticRelocatableClient builds a module in the older layout: the header,
// a relocation table naming one pointer and the entry, the terminator, and a
// segment whose nine-word header carries the entry between its two markers.
func syntheticRelocatableClient(bssSize uint32) []byte {
	segment := make([]byte, 0x40)
	binary.LittleEndian.PutUint32(segment[0x08:], 0x30)                   // a pointer to rebase
	binary.LittleEndian.PutUint32(segment[0x18:], relocatableMarker)      // marker
	binary.LittleEndian.PutUint32(segment[relocatableEntryOffset:], 0x25) // Thumb entry
	binary.LittleEndian.PutUint32(segment[0x20:], relocatableMarker)      // marker
	relocations := []uint32{0x08, relocatableEntryOffset}
	image := make([]byte, 0, relocatableHeaderWords*4+len(relocations)*4+4+len(segment))
	header := make([]byte, relocatableHeaderWords*4)
	binary.LittleEndian.PutUint32(header[0:], bssSize)
	binary.LittleEndian.PutUint32(header[4:], uint32(len(relocations)))
	image = append(image, header...)
	for _, offset := range relocations {
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], offset)
		image = append(image, word[:]...)
	}
	image = append(image, 0, 0, 0, 0)
	return append(image, segment...)
}

func TestRelocatableClientRebasesItsSegmentAndNamesItsEntry(t *testing.T) {
	data := syntheticRelocatableClient(36)
	module, ok := parseRelocatableClient(data)
	if !ok {
		t.Fatal("a module in the older layout was not recognized")
	}
	relocated, err := module.relocate(data, ImageBase)
	if err != nil {
		t.Fatal(err)
	}
	base := ImageBase + uint32(module.segmentStart)
	segment := relocated[module.segmentStart:]
	if value := binary.LittleEndian.Uint32(segment[0x08:]); value != base+0x30 {
		t.Fatalf("pointer rebased to %#x, want %#x", value, base+0x30)
	}
	if entry := module.entryAddress(relocated); entry != base+0x25 {
		t.Fatalf("entry is %#x, want %#x", entry, base+0x25)
	}
	// The header stays in front, because the entry is handed a pointer to it.
	if value := binary.LittleEndian.Uint32(relocated[0:]); value != 36 {
		t.Fatalf("the header's first word is %d, want the BSS size 36", value)
	}
}

// The current generation of client images begins with its Thumb entry, and
// reading that as a header has to fail rather than nearly work.
func TestACurrentClientImageIsNotReadAsARelocatableModule(t *testing.T) {
	if _, ok := parseRelocatableClient(syntheticInitializableClient()); ok {
		t.Fatal("a current client image was read as an older module")
	}
	for _, name := range []string{"empty", "short", "zeroed"} {
		var data []byte
		switch name {
		case "short":
			data = []byte{1, 2, 3}
		case "zeroed":
			data = make([]byte, 256)
		}
		if _, ok := parseRelocatableClient(data); ok {
			t.Fatalf("%s data was read as an older module", name)
		}
	}
}

// A relocation naming a word past the end of the segment means the header was
// read wrongly, so it is refused rather than skipped.
func TestARelocationOutsideTheSegmentIsRefused(t *testing.T) {
	data := syntheticRelocatableClient(36)
	module, ok := parseRelocatableClient(data)
	if !ok {
		t.Fatal("a module in the older layout was not recognized")
	}
	module.relocations = append(module.relocations, 0x1000)
	if _, err := module.relocate(data, ImageBase); err == nil {
		t.Fatal("a relocation outside the segment was applied")
	}
}
