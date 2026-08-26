package lgt

import (
	"encoding/binary"
	"testing"
)

// The toolchain writes a module's entry down twice — in the ELF header and in
// its own `.raptor` record — and across the local library the two agree
// everywhere but one module, where the header is the one that is wrong. So the
// record is what the loader reads, with the header as the fallback.
//
// raptorModule builds the smallest ELF that carries both, so a test can make
// them disagree on purpose.
func raptorModule(t *testing.T, headerEntry uint32, raptor []byte) []byte {
	t.Helper()
	const (
		headerSize  = 52
		entrySize   = 40
		sectionsNum = 4 // null, .text, .shstrtab, .raptor
		codeBase    = 0x1000
		codeSize    = 0x40
	)
	names := []byte("\x00.text\x00.shstrtab\x00.raptor\x00")
	codeOffset := uint32(headerSize)
	namesOffset := codeOffset + codeSize
	raptorOffset := namesOffset + uint32(len(names))
	sectionOffset := raptorOffset + uint32(len(raptor))

	image := make([]byte, sectionOffset+sectionsNum*entrySize)
	copy(image, "\x7fELF")
	image[4], image[5], image[6] = 1, 1, 1
	binary.LittleEndian.PutUint16(image[16:], 2)  // ET_EXEC
	binary.LittleEndian.PutUint16(image[18:], 40) // EM_ARM
	binary.LittleEndian.PutUint32(image[20:], 1)
	binary.LittleEndian.PutUint32(image[24:], headerEntry)
	binary.LittleEndian.PutUint32(image[32:], sectionOffset)
	binary.LittleEndian.PutUint16(image[40:], headerSize)
	binary.LittleEndian.PutUint16(image[46:], entrySize)
	binary.LittleEndian.PutUint16(image[48:], sectionsNum)
	binary.LittleEndian.PutUint16(image[50:], 2) // .shstrtab index
	copy(image[namesOffset:], names)
	copy(image[raptorOffset:], raptor)

	section := func(index int, nameOffset, kind, flags, address, offset, size uint32) {
		base := sectionOffset + uint32(index)*entrySize
		binary.LittleEndian.PutUint32(image[base:], nameOffset)
		binary.LittleEndian.PutUint32(image[base+4:], kind)
		binary.LittleEndian.PutUint32(image[base+8:], flags)
		binary.LittleEndian.PutUint32(image[base+12:], address)
		binary.LittleEndian.PutUint32(image[base+16:], offset)
		binary.LittleEndian.PutUint32(image[base+20:], size)
	}
	section(0, 0, 0, 0, 0, 0, 0)
	section(1, 1, 1, sectionFlagAlloc|sectionFlagExec, codeBase, codeOffset, codeSize)
	section(2, 7, 3, 0, 0, namesOffset, uint32(len(names)))
	// The record is not loaded — its address is zero — which is why nothing
	// else in the loader had ever looked at it.
	section(3, 17, 0, 0, 0, raptorOffset, uint32(len(raptor)))
	return image
}

// raptorRecord is the record's fixed part: magic, version, its own length, and
// the entry as an offset from the image base with the Thumb bit set.
func raptorRecord(relative uint32) []byte {
	record := make([]byte, 0x20)
	copy(record, raptorMagic)
	binary.LittleEndian.PutUint32(record[4:], 0x20050512)
	binary.LittleEndian.PutUint32(record[8:], uint32(len(record)))
	binary.LittleEndian.PutUint32(record[raptorEntryOffset:], relative|1)
	return record
}

func TestModuleEntryComesFromTheRaptorRecord(t *testing.T) {
	// The disagreement this exists for: the header names the module's first
	// byte, the record names the routine 0x20 in.
	module, err := ParseModule(raptorModule(t, 0x1000, raptorRecord(0x20)))
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}
	if module.Entry != 0x1020 {
		t.Fatalf("entry = %#x, want the record's %#x", module.Entry, 0x1020)
	}
}

// Nothing is forced. A record that is missing, unrecognisable, or that names an
// address outside an executable section leaves the header's entry alone —
// which is what keeps a misread record from turning a readable failure into a
// wild jump.
func TestModuleEntryFallsBackToTheHeader(t *testing.T) {
	unrecognisable := raptorRecord(0x20)
	copy(unrecognisable, "XXXX")
	outside := raptorRecord(0x8000)
	short := raptorRecord(0x20)[:8]

	for name, record := range map[string][]byte{
		"no record":     nil,
		"wrong magic":   unrecognisable,
		"past the code": outside,
		"short record":  short,
		"zero-length":   {},
		// The ordinary case, which is every module but one: the record and the
		// header name the same address, so which one is read cannot be told
		// apart from the outside — and must not be.
		"the two agree": raptorRecord(0x10),
	} {
		module, err := ParseModule(raptorModule(t, 0x1010, record))
		if err != nil {
			t.Fatalf("%s: ParseModule() error = %v", name, err)
		}
		if module.Entry != 0x1010 {
			t.Fatalf("%s: entry = %#x, want %#x", name, module.Entry, 0x1010)
		}
	}
}
