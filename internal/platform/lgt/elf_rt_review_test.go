package lgt

import (
	"encoding/binary"
	"strings"
	"testing"
)

// elfSection is one section header for elfWithSections.
type elfSection struct{ kind, flags, address, offset, size uint32 }

// elfWithSections builds an ELF whose file holds dataSize bytes after the
// header and the given sections after a null one. The bytes are a counter, so
// two sections that alias the same file bytes read the same thing.
func elfWithSections(dataSize uint32, sections []elfSection) []byte {
	const headerSize, entrySize = 52, 40
	sectionOffset := headerSize + dataSize
	count := uint32(len(sections) + 1)
	image := make([]byte, sectionOffset+count*entrySize)
	copy(image, "\x7fELF")
	image[4], image[5], image[6] = 1, 1, 1
	binary.LittleEndian.PutUint16(image[16:], 2)  // ET_EXEC
	binary.LittleEndian.PutUint16(image[18:], 40) // EM_ARM
	binary.LittleEndian.PutUint32(image[20:], 1)
	binary.LittleEndian.PutUint32(image[24:], 0x1001)
	binary.LittleEndian.PutUint32(image[32:], sectionOffset)
	binary.LittleEndian.PutUint16(image[40:], headerSize)
	binary.LittleEndian.PutUint16(image[46:], entrySize)
	binary.LittleEndian.PutUint16(image[48:], uint16(count))
	for index := range dataSize {
		image[headerSize+index] = byte(index)
	}
	for index, section := range sections {
		base := sectionOffset + uint32(index+1)*entrySize
		binary.LittleEndian.PutUint32(image[base+4:], section.kind)
		binary.LittleEndian.PutUint32(image[base+8:], section.flags)
		binary.LittleEndian.PutUint32(image[base+12:], section.address)
		binary.LittleEndian.PutUint32(image[base+16:], section.offset)
		binary.LittleEndian.PutUint32(image[base+20:], section.size)
	}
	return image
}

const (
	elfProgBits = 1
	elfCode     = sectionFlagAlloc | sectionFlagExec
)

// A module's sections are pieces of one image, so no two of them hold the same
// bytes of it or claim the same addresses. Headers that do are how a small
// file becomes tens of thousands of copies of one blob — loaded once each and
// scanned once each at every session start.
func TestModuleSectionsThatAliasEachOtherAreRefused(t *testing.T) {
	for name, sections := range map[string][]elfSection{
		"two headers for one section": {
			{elfProgBits, elfCode, 0x1000, 52, 0x100},
			{elfProgBits, elfCode, 0x1000, 52, 0x100},
		},
		"overlapping addresses": {
			{elfProgBits, elfCode, 0x1000, 52, 0x100},
			{elfProgBits, sectionFlagAlloc, 0x10f0, 52 + 0x100, 0x40},
		},
		"zero-filled bytes over a section's addresses": {
			{elfProgBits, elfCode, 0x1000, 52, 0x100},
			{sectionTypeNoBits, sectionFlagAlloc, 0x1080, 0, 0x100},
		},
		"one file range at two addresses": {
			{elfProgBits, elfCode, 0x1000, 52, 0x100},
			{elfProgBits, elfCode, 0x2000, 52 + 0x80, 0x100},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseModule(elfWithSections(0x200, sections)); err == nil {
				t.Fatal("the module was accepted")
			} else if !strings.Contains(err.Error(), "overlaps") {
				t.Fatalf("the module was refused for another reason: %v", err)
			}
		})
	}

	// The shape this exists for: one blob behind every header the format
	// allows.
	many := make([]elfSection, 0xfffe)
	for index := range many {
		many[index] = elfSection{elfProgBits, elfCode, 0x8000, 52, 0x40}
	}
	if _, err := ParseModule(elfWithSections(0x40, many)); err == nil {
		t.Fatal("a module of 65534 aliased sections was accepted")
	}
}

// The count of loaded sections is bounded too, because every recognition scan
// walks the section list once per address it probes. A module here has three
// — code, data and zero-filled data — so the bound is far past any of them.
func TestModuleSectionCountIsBounded(t *testing.T) {
	sections := func(count int) []elfSection {
		list := make([]elfSection, count)
		for index := range list {
			list[index] = elfSection{elfProgBits, elfCode, 0x1000 + uint32(index)*0x10, 52 + uint32(index)*0x10, 0x10}
		}
		return list
	}
	if _, err := ParseModule(elfWithSections(0x10*maxModuleSections, sections(maxModuleSections))); err != nil {
		t.Fatalf("%d sections were refused: %v", maxModuleSections, err)
	}
	if _, err := ParseModule(elfWithSections(0x10*(maxModuleSections+1), sections(maxModuleSections+1))); err == nil {
		t.Fatalf("%d sections were accepted", maxModuleSections+1)
	}
}

// What a linker does write is still taken: sections end to end in both the
// file and the address space, zero-filled data whose file offset is where the
// next bytes would have been, and empty sections that share an address with
// a neighbour.
func TestOrdinaryModuleLayoutsAreStillAccepted(t *testing.T) {
	module, err := ParseModule(elfWithSections(0x200, []elfSection{
		{elfProgBits, elfCode, 0x1000, 52, 0x100},
		{elfProgBits, sectionFlagAlloc, 0x1100, 52 + 0x100, 0x40},
		{sectionTypeNoBits, sectionFlagAlloc | 1, 0x1140, 52 + 0x140, 0x400},
		{elfProgBits, sectionFlagAlloc, 0x1000, 52, 0},
		{elfProgBits, sectionFlagAlloc, 0x1140, 52 + 0x140, 0},
		{sectionTypeNoBits, sectionFlagAlloc, 0x1540, 52 + 0x140, 0},
	}))
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}
	if len(module.Sections) != 6 {
		t.Fatalf("%d sections were loaded, want 6", len(module.Sections))
	}
	if low, high := module.Span(); low != 0x1000 || high != 0x1540 {
		t.Fatalf("span = %#x..%#x", low, high)
	}
}
