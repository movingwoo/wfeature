package ktf

import (
	"encoding/binary"
	"testing"
)

// syntheticRelocatableClient builds a module in the older layout: the header,
// a relocation table whose first entry rebases the segment's own first word,
// and a segment whose eleven-word header carries the entry between its two
// markers.
func syntheticRelocatableClient(bssSize uint32) []byte {
	segment := make([]byte, 0x60)
	binary.LittleEndian.PutUint32(segment[0x08:], 0x30) // a pointer to rebase
	// The static base and the end of the file's own bytes bound the module's
	// pointer table, which the relocation list does not name and the load
	// rebases wholesale.
	binary.LittleEndian.PutUint32(segment[relocatableStaticBaseOffset:], 0x50)
	binary.LittleEndian.PutUint32(segment[relocatableSegmentEndOffset:], 0x60)
	binary.LittleEndian.PutUint32(segment[0x50:], 0x30)
	binary.LittleEndian.PutUint32(segment[0x54:], 0x44)
	binary.LittleEndian.PutUint32(segment[relocatableEntryOffset-4:], relocatableMarker)
	binary.LittleEndian.PutUint32(segment[relocatableEntryOffset:], 0x45) // Thumb entry
	binary.LittleEndian.PutUint32(segment[relocatableEntryOffset+4:], relocatableMarker)
	relocations := []uint32{0x00, 0x08, relocatableStaticBaseOffset, relocatableSegmentEndOffset, relocatableEntryOffset}
	image := make([]byte, 0, relocatableHeaderWords*4+len(relocations)*4+len(segment))
	header := make([]byte, relocatableHeaderWords*4)
	binary.LittleEndian.PutUint32(header[0:], bssSize)
	binary.LittleEndian.PutUint32(header[4:], uint32(len(relocations)))
	image = append(image, header...)
	for _, offset := range relocations {
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], offset)
		image = append(image, word[:]...)
	}
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
	if entry := module.entryAddress(relocated); entry != base+0x45 {
		t.Fatalf("entry is %#x, want %#x", entry, base+0x45)
	}
	// The header stays in front, because the entry is handed a pointer to it.
	if value := binary.LittleEndian.Uint32(relocated[0:]); value != 36 {
		t.Fatalf("the header's first word is %d, want the BSS size 36", value)
	}
}

// The relocation list stops in front of the module's pointer table and the
// segment header bounds it instead. The entry reads a word out of that table
// and hands it on as an address, so a table left holding segment offsets makes
// the module's own first call malformed.
func TestRelocatableClientRebasesThePointerTableTheListDoesNotName(t *testing.T) {
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
	for offset, want := range map[int]uint32{0x50: base + 0x30, 0x54: base + 0x44} {
		if value := binary.LittleEndian.Uint32(segment[offset:]); value != want {
			t.Fatalf("pointer table word %#x is %#x, want %#x", offset, value, want)
		}
	}
}

// Bounds that do not describe a table inside the segment mean the header was
// read wrongly, and rebasing whatever they happen to name would corrupt the
// module silently.
func TestPointerTableBoundsOutsideTheSegmentAreRefused(t *testing.T) {
	for _, bounds := range []struct {
		name            string
		staticBase, end uint32
	}{
		{"end past the segment", 0x50, 0x400},
		{"base above the end", 0x58, 0x50},
		{"unaligned base", 0x51, 0x60},
	} {
		data := syntheticRelocatableClient(36)
		module, ok := parseRelocatableClient(data)
		if !ok {
			t.Fatal("a module in the older layout was not recognized")
		}
		segment := data[module.segmentStart:]
		binary.LittleEndian.PutUint32(segment[relocatableStaticBaseOffset:], bounds.staticBase)
		binary.LittleEndian.PutUint32(segment[relocatableSegmentEndOffset:], bounds.end)
		module.relocations = []uint32{0x00}
		if _, err := module.relocate(data, ImageBase); err == nil {
			t.Fatalf("%s was accepted", bounds.name)
		}
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
