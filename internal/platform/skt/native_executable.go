package skt

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// The `.daf` executable, as both local titles lay it out (2026-10-30; see
// docs/skt-native.md):
//
//	0x00  0x20, the first block's length; 0x08 holds 16 bytes the descriptor
//	      repeats at its own offset 8
//	0x20  a second block, 0x50 bytes (its length is at 0x28); 0x3c..0x50 are
//	      five sizes: code with its read-only data, initialised data, BSS
//	      (not in the file), the relocation table and the resource archive
//	0x58  six Thumb entry offsets into the code
//	0x70  code, data, relocation table and resources, back to back
//
// Code is linked at zero and data at NativeDataLink; the first data word is
// the marker the loader overwrites with the platform's root table. Every
// pointer in both regions is listed in the relocation table, so the regions
// can be placed anywhere.
const (
	nativeFirstBlock  = 0x20
	nativeSecondBlock = 0x50
	nativeHeaderSize  = nativeFirstBlock + nativeSecondBlock
	nativeSizesOffset = 0x3c
	nativeEntryOffset = 0x58

	// NativeDataLink is where data and BSS are linked. Every data address
	// the code holds is this plus an offset into data or BSS.
	NativeDataLink uint32 = 0x01001000
	// nativeRootMarker fills the data word the platform's root table
	// pointer goes in. Its presence is part of what identifies the format.
	nativeRootMarker uint32 = 0xCAFECAFE

	// The executable is untrusted, so its regions are bounded. Code is linked
	// below NativeDataLink and cannot be larger than the space before it;
	// data and BSS together are bounded the way a KTF image and its BSS are,
	// well past the 2.1 MB the larger local title asks for.
	maxNativeCode     = uint64(NativeDataLink)
	maxNativeDataBSS  = 64 << 20
	maxNativeResource = 64 << 20
	// maxNativeResources bounds the entries of the resource archive; the
	// local titles carry 391 and 2,545.
	maxNativeResources = 1 << 16
)

// NativeEntries are the six entry points a `.daf` header lists, as offsets
// into the code with the Thumb bit kept. Their order is not the
// specification's startClet, pauseClet, resumeClet, destroyClet order:
// the second entry is the teardown, the third and fourth post the title's own
// pause and resume events, and the fifth is the paint entry, empty in both
// local titles.
type NativeEntries struct {
	Start, Destroy, Pause, Resume, Paint, Event uint32
}

// NativeExecutable is a parsed `.daf`.
type NativeExecutable struct {
	// Code is the code region with its read-only data, linked at zero.
	Code []byte
	// Data is the initialised data, linked at NativeDataLink.
	Data []byte
	// BSSSize is the zero-filled region after the data.
	BSSSize uint32
	Entries NativeEntries
	// Resources are the files of the embedded resource archive, by name.
	Resources map[string][]byte
	// Digest is the SHA-256 of the executable as shipped, which is what the
	// compatibility registry names a revision by.
	Digest [32]byte
	// relocations lists the words that hold addresses: offsets into Code and
	// into Data. Each holds an absolute link address that moves with the
	// image; see Load.
	codeRelocations []uint32
	dataRelocations []uint32
}

// ParseNativeExecutable validates a `.daf` and splits it into its regions.
// A layout other than the one described above is refused rather than read
// as far as it goes.
func ParseNativeExecutable(image []byte) (*NativeExecutable, error) {
	if len(image) < nativeHeaderSize {
		return nil, fmt.Errorf("executable is %d bytes, shorter than its %d-byte header", len(image), nativeHeaderSize)
	}
	word := func(offset int) uint32 { return binary.LittleEndian.Uint32(image[offset:]) }
	if word(0) != nativeFirstBlock || word(nativeFirstBlock+8) != nativeSecondBlock {
		return nil, fmt.Errorf("executable header blocks are %#x and %#x, want %#x and %#x", word(0), word(nativeFirstBlock+8), nativeFirstBlock, nativeSecondBlock)
	}
	codeSize := uint64(word(nativeSizesOffset))
	dataSize := uint64(word(nativeSizesOffset + 4))
	bssSize := uint64(word(nativeSizesOffset + 8))
	relocationSize := uint64(word(nativeSizesOffset + 12))
	resourceSize := uint64(word(nativeSizesOffset + 20))
	if codeSize == 0 || codeSize > maxNativeCode {
		return nil, fmt.Errorf("executable code is %d bytes, limit %d", codeSize, maxNativeCode)
	}
	if dataSize < 4 || dataSize+bssSize > maxNativeDataBSS {
		return nil, fmt.Errorf("executable data and BSS are %d and %d bytes, limit %d together", dataSize, bssSize, maxNativeDataBSS)
	}
	if resourceSize > maxNativeResource {
		return nil, fmt.Errorf("executable resources are %d bytes, limit %d", resourceSize, maxNativeResource)
	}
	if total := uint64(nativeHeaderSize) + codeSize + dataSize + relocationSize + resourceSize; total != uint64(len(image)) {
		return nil, fmt.Errorf("executable regions add up to %d bytes, the file is %d", total, len(image))
	}
	codeStart := uint64(nativeHeaderSize)
	dataStart := codeStart + codeSize
	relocationStart := dataStart + dataSize
	resourceStart := relocationStart + relocationSize
	executable := &NativeExecutable{
		Code:    bytes.Clone(image[codeStart:dataStart]),
		Data:    bytes.Clone(image[dataStart:relocationStart]),
		BSSSize: uint32(bssSize),
		Digest:  sha256.Sum256(image),
	}
	if marker := binary.LittleEndian.Uint32(executable.Data); marker != nativeRootMarker {
		return nil, fmt.Errorf("executable data starts with %#x rather than the root marker %#x", marker, nativeRootMarker)
	}
	var entries [6]uint32
	for index := range entries {
		entry := word(nativeEntryOffset + 4*index)
		// An absent entry is zero; a present one is a Thumb address inside
		// the code. The start is the one a title cannot do without.
		if entry != 0 && (entry&1 == 0 || uint64(entry) >= codeSize) {
			return nil, fmt.Errorf("executable entry %d is %#x, not a Thumb address inside %d bytes of code", index, entry, codeSize)
		}
		entries[index] = entry
	}
	if entries[0] == 0 {
		return nil, fmt.Errorf("executable has no start entry")
	}
	executable.Entries = NativeEntries{Start: entries[0], Destroy: entries[1], Pause: entries[2], Resume: entries[3], Paint: entries[4], Event: entries[5]}
	var err error
	executable.codeRelocations, executable.dataRelocations, err = parseNativeRelocations(image[relocationStart:resourceStart], codeSize, dataSize)
	if err != nil {
		return nil, err
	}
	executable.Resources, err = parseNativeResources(image[resourceStart:])
	if err != nil {
		return nil, err
	}
	return executable, nil
}

// parseNativeRelocations reads the relocation table: a byte count for the code
// list and one for the data list, then the two lists. An entry advances an
// offset from its region's start to the next word that holds an address:
//
//   - bit 7 set: the delta is the low nibble and the next three bytes, 28 bits;
//   - bit 5 set: the low nibble and the next byte, 12 bits, or with bit 4 also
//     set the low nibble and the next two, 20 bits;
//   - otherwise the low nibble, plus 16 when bit 4 is set.
//
// Bit 6 says whether the word holds a data or a code address and bit 7 that it
// holds a target less its own address. Load moves code and data together, so
// the first two are moved the same way and the third not at all; the bits are
// read only to find the next entry.
func parseNativeRelocations(table []byte, codeSize, dataSize uint64) ([]uint32, []uint32, error) {
	if len(table) < 8 {
		return nil, nil, fmt.Errorf("executable relocation table is %d bytes, shorter than its counts", len(table))
	}
	codeBytes := uint64(binary.LittleEndian.Uint32(table))
	dataBytes := uint64(binary.LittleEndian.Uint32(table[4:]))
	if 8+codeBytes+dataBytes > uint64(len(table)) {
		return nil, nil, fmt.Errorf("executable relocation lists of %d and %d bytes overrun a %d-byte table", codeBytes, dataBytes, len(table))
	}
	codeList := table[8 : 8+codeBytes]
	dataList := table[8+codeBytes : 8+codeBytes+dataBytes]
	code, err := decodeNativeRelocations(codeList, codeSize, "code")
	if err != nil {
		return nil, nil, err
	}
	data, err := decodeNativeRelocations(dataList, dataSize, "data")
	if err != nil {
		return nil, nil, err
	}
	return code, data, nil
}

func decodeNativeRelocations(list []byte, regionSize uint64, region string) ([]uint32, error) {
	var (
		offsets []uint32
		offset  uint64
	)
	for position := 0; position < len(list); {
		first := list[position]
		position++
		var delta uint64
		need := 0
		switch {
		case first&0x80 != 0:
			need = 3
		case first&0x20 != 0 && first&0x10 != 0:
			need = 2
		case first&0x20 != 0:
			need = 1
		}
		if position+need > len(list) {
			return nil, fmt.Errorf("executable %s relocation list ends inside an entry", region)
		}
		delta = uint64(first & 0x0f)
		for index := 0; index < need; index++ {
			delta = delta<<8 | uint64(list[position+index])
		}
		position += need
		if need == 0 && first&0x10 != 0 {
			delta += 16
		}
		offset += delta
		// An address is a whole aligned word inside its region. Both local
		// titles' entries all are; one that is not would move bytes that are
		// not an address.
		if offset%4 != 0 || offset+4 > regionSize {
			return nil, fmt.Errorf("executable %s relocation at %#x is outside its %d-byte region or unaligned", region, offset, regionSize)
		}
		if first&0x80 == 0 {
			offsets = append(offsets, uint32(offset))
		}
	}
	return offsets, nil
}

// parseNativeResources reads the embedded resource archive: a 28-byte header
// `{0x10, 1, 0x10, "KO", directory size, 0, count}`, then count pairs of
// offset and size, then count NUL-terminated names. The files follow the
// directory at 16 plus the directory size, and the offsets count from there.
// An executable without resources has an empty archive region.
func parseNativeResources(archive []byte) (map[string][]byte, error) {
	resources := make(map[string][]byte)
	if len(archive) == 0 {
		return resources, nil
	}
	const header = 28
	if len(archive) < header {
		return nil, fmt.Errorf("executable resource archive is %d bytes, shorter than its header", len(archive))
	}
	word := func(offset int) uint32 { return binary.LittleEndian.Uint32(archive[offset:]) }
	if word(0) != 0x10 || word(4) != 1 || word(8) != 0x10 || !bytes.Equal(archive[12:16], []byte{'K', 'O', 0, 0}) {
		return nil, fmt.Errorf("executable resource archive header % x is not the one this format uses", archive[:16])
	}
	directory := uint64(word(16))
	count := uint64(word(24))
	if count > maxNativeResources {
		return nil, fmt.Errorf("executable resource archive lists %d files, limit %d", count, maxNativeResources)
	}
	base := 16 + directory
	if header+8*count > uint64(len(archive)) || base > uint64(len(archive)) || header+8*count > base {
		return nil, fmt.Errorf("executable resource directory of %d files and %d bytes overruns a %d-byte archive", count, directory, len(archive))
	}
	names := archive[header+8*count : base]
	for index := uint64(0); index < count; index++ {
		end := bytes.IndexByte(names, 0)
		if end < 0 {
			return nil, fmt.Errorf("executable resource name %d is not terminated inside the directory", index)
		}
		name := string(names[:end])
		names = names[end+1:]
		offset := uint64(word(header + 8*int(index)))
		size := uint64(word(header + 8*int(index) + 4))
		if offset+size > uint64(len(archive))-base {
			return nil, fmt.Errorf("executable resource %q at %d of %d bytes overruns the archive", name, offset, size)
		}
		if name == "" {
			return nil, fmt.Errorf("executable resource %d has no name", index)
		}
		if _, duplicate := resources[name]; duplicate {
			return nil, fmt.Errorf("executable resource archive names %q twice", name)
		}
		resources[name] = archive[base+offset : base+offset+size : base+offset+size]
	}
	return resources, nil
}

// Load lays the executable out for a base address: the code at base and the
// data at base plus NativeDataLink, both moved by base, so every address the
// relocation table lists — a code address, a data address, or one relative to
// its own word — stays right by adding base to the first two and leaving the
// third alone. BSS is the caller's to zero-fill after the data.
//
// Moving both regions by one amount is a choice, not a requirement of the
// table: it keeps the root marker at base plus NativeDataLink and makes the
// self-relative words need nothing. A base that would wrap either region
// past the top of the address space is refused.
func (executable *NativeExecutable) Load(base uint32) (code, data []byte, err error) {
	if executable == nil {
		return nil, nil, fmt.Errorf("executable is not parsed")
	}
	if uint64(base)+uint64(NativeDataLink)+uint64(len(executable.Data))+uint64(executable.BSSSize) > 1<<32 {
		return nil, nil, fmt.Errorf("executable does not fit at base %#x", base)
	}
	code = bytes.Clone(executable.Code)
	data = bytes.Clone(executable.Data)
	for _, offset := range executable.codeRelocations {
		binary.LittleEndian.PutUint32(code[offset:], binary.LittleEndian.Uint32(code[offset:])+base)
	}
	for _, offset := range executable.dataRelocations {
		binary.LittleEndian.PutUint32(data[offset:], binary.LittleEndian.Uint32(data[offset:])+base)
	}
	return code, data, nil
}
