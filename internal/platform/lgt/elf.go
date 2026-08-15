package lgt

import (
	"encoding/binary"
	"fmt"
)

// A minimal ELF32 little-endian ARM reader. The project takes no new
// dependencies, and what an LGT module needs is one header check and a walk
// of the section table — the parts of ELF that would justify a library are
// the ones this loader never touches.

const (
	elfMagic          = "\x7fELF"
	elfClass32        = 1
	elfDataLittle     = 1
	elfTypeExecutable = 2
	elfMachineARM     = 40

	elfHeaderSize     = 52
	sectionEntrySize  = 40
	sectionTypeNoBits = 8
	sectionFlagExec   = 0x4
	sectionFlagAlloc  = 0x2
)

// Section is one loadable piece of the module.
type Section struct {
	Name       string
	Address    uint32
	Size       uint32
	Data       []byte
	Executable bool
}

// Module is a parsed binary.mod.
type Module struct {
	Entry    uint32
	Sections []Section
}

// ParseModule reads an ARM ELF32 executable. Everything it rejects is
// something an LGT module cannot be, so a wrong file fails here rather than
// as a wild jump later.
func ParseModule(data []byte) (*Module, error) {
	if len(data) < elfHeaderSize {
		return nil, fmt.Errorf("LGT module is %d bytes, shorter than an ELF header", len(data))
	}
	if string(data[:4]) != elfMagic {
		return nil, fmt.Errorf("LGT module does not start with the ELF magic")
	}
	if data[4] != elfClass32 {
		return nil, fmt.Errorf("LGT module is not ELF32 (class %d)", data[4])
	}
	if data[5] != elfDataLittle {
		return nil, fmt.Errorf("LGT module is not little-endian (data %d)", data[5])
	}
	if kind := binary.LittleEndian.Uint16(data[16:]); kind != elfTypeExecutable {
		return nil, fmt.Errorf("LGT module ELF type is %d, want an executable", kind)
	}
	if machine := binary.LittleEndian.Uint16(data[18:]); machine != elfMachineARM {
		return nil, fmt.Errorf("LGT module machine is %d, want ARM", machine)
	}

	module := &Module{Entry: binary.LittleEndian.Uint32(data[24:])}
	sectionOffset := binary.LittleEndian.Uint32(data[32:])
	entrySize := binary.LittleEndian.Uint16(data[46:])
	count := binary.LittleEndian.Uint16(data[48:])
	nameIndex := binary.LittleEndian.Uint16(data[50:])
	if count == 0 {
		return nil, fmt.Errorf("LGT module has no section headers")
	}
	if entrySize < sectionEntrySize {
		return nil, fmt.Errorf("LGT module section entry size is %d", entrySize)
	}

	header := func(index int) ([]byte, error) {
		start := uint64(sectionOffset) + uint64(index)*uint64(entrySize)
		if start+sectionEntrySize > uint64(len(data)) {
			return nil, fmt.Errorf("LGT module section header %d is outside the file", index)
		}
		return data[start : start+sectionEntrySize], nil
	}

	// The section name table has to be read before the sections so each one
	// can be reported by name; an unreadable table is not fatal, because the
	// names are for diagnostics only.
	var names []byte
	if int(nameIndex) < int(count) {
		if entry, err := header(int(nameIndex)); err == nil {
			offset := binary.LittleEndian.Uint32(entry[16:])
			size := binary.LittleEndian.Uint32(entry[20:])
			if uint64(offset)+uint64(size) <= uint64(len(data)) {
				names = data[offset : uint64(offset)+uint64(size)]
			}
		}
	}

	for index := 0; index < int(count); index++ {
		entry, err := header(index)
		if err != nil {
			return nil, err
		}
		address := binary.LittleEndian.Uint32(entry[12:])
		if address == 0 {
			// A section with no address is not loaded: symbol tables,
			// debug info, the section name table itself.
			continue
		}
		kind := binary.LittleEndian.Uint32(entry[4:])
		flags := binary.LittleEndian.Uint32(entry[8:])
		offset := binary.LittleEndian.Uint32(entry[16:])
		size := binary.LittleEndian.Uint32(entry[20:])

		section := Section{
			Name:       sectionName(names, binary.LittleEndian.Uint32(entry[0:])),
			Address:    address,
			Size:       size,
			Executable: flags&sectionFlagExec != 0,
		}
		if kind != sectionTypeNoBits {
			// SHT_NOBITS is .bss: it occupies memory but has no file bytes,
			// which is why only the others are read.
			if uint64(offset)+uint64(size) > uint64(len(data)) {
				return nil, fmt.Errorf("LGT module section %q is outside the file", section.Name)
			}
			section.Data = data[offset : uint64(offset)+uint64(size)]
		}
		if uint64(address)+uint64(size) > 1<<32 {
			return nil, fmt.Errorf("LGT module section %q wraps the address space", section.Name)
		}
		module.Sections = append(module.Sections, section)
	}
	if len(module.Sections) == 0 {
		return nil, fmt.Errorf("LGT module has no loadable sections")
	}
	return module, nil
}

func sectionName(names []byte, offset uint32) string {
	if int(offset) >= len(names) {
		return ""
	}
	end := int(offset)
	for end < len(names) && names[end] != 0 {
		end++
	}
	return string(names[offset:end])
}

// Span reports the address range the module occupies once loaded.
func (module *Module) Span() (uint32, uint32) {
	low, high := ^uint32(0), uint32(0)
	for _, section := range module.Sections {
		if section.Address < low {
			low = section.Address
		}
		if end := section.Address + section.Size; end > high {
			high = end
		}
	}
	if high == 0 {
		return 0, 0
	}
	return low, high
}
