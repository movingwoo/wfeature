package sgsvm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"golang.org/x/text/encoding/korean"
)

const MaxWideSize = 4 << 20

// WideProgram contains the 32-bit symbol banks and eleven event vectors of a
// version-four image. Code addresses count bytes from the start of its header.
type WideProgram struct {
	Entries   [11]int
	Variables [][]int32
}

func parseWide(data []byte) (*Program, error) {
	const header = 128
	if len(data) < header || len(data) > MaxWideSize || data[5] != 4 {
		return nil, fmt.Errorf("SGS: invalid version 4 header")
	}
	u32 := func(off int) int { return int(binary.LittleEndian.Uint32(data[off:])) }
	if u32(120) != len(data) {
		return nil, fmt.Errorf("SGS: version 4 module length mismatch")
	}
	vd, vi, rd, ri := u32(72), u32(76), u32(80), u32(84)
	if vd < header || vd%2 != 0 || vi < vd || rd < vi || ri < rd || ri > len(data) ||
		(vi-vd)%8 != 0 || (ri-rd)%12 != 0 || (vi-vd)/8 > 65536 || (ri-rd)/12 > 65536 {
		return nil, fmt.Errorf("SGS: invalid version 4 section boundaries")
	}
	wide := &WideProgram{}
	for i := range wide.Entries {
		entry := u32(28 + 4*i)
		if entry == 0 && i != 0 {
			continue
		}
		if entry < header || entry >= vd || entry%2 != 0 {
			return nil, fmt.Errorf("SGS: invalid version 4 callback %d at %#x", i, entry)
		}
		wide.Entries[i] = entry
	}
	title, _, _ := bytes.Cut(data[10:26], []byte{0})
	decoded, err := korean.EUCKR.NewDecoder().Bytes(title)
	if err != nil {
		return nil, fmt.Errorf("SGS: invalid title: %w", err)
	}
	p := &Program{Wide: wide, Name: strings.TrimSpace(string(decoded)), Data: bytes.Clone(data), CodeStart: header, CodeEnd: vd, Width: 240, Height: 320}
	cursor := vi
	for off := vd; off < vi; off += 8 {
		flags := binary.LittleEndian.Uint16(data[off:])
		count := int(binary.LittleEndian.Uint16(data[off+2:]))
		if flags&^0x101 != 0 || u32(off+4) != 0 || count > (rd-cursor)/4 {
			return nil, fmt.Errorf("SGS: invalid version 4 symbol at %#x", off)
		}
		values := make([]int32, count)
		for i := range values {
			values[i] = int32(binary.LittleEndian.Uint32(data[cursor:]))
			cursor += 4
		}
		wide.Variables = append(wide.Variables, values)
	}
	if cursor != rd {
		return nil, fmt.Errorf("SGS: version 4 symbol sizes do not match data")
	}
	cursor = ri
	for off := rd; off < ri; off += 12 {
		flags, size := u32(off), u32(off+4)
		if flags&^0x301 != 0 || flags>>8 > 2 || size < 0 || size > len(data)-cursor || u32(off+8) != 0 {
			return nil, fmt.Errorf("SGS: invalid version 4 resource at %#x", off)
		}
		end := cursor + size
		p.Resources = append(p.Resources, Resource{Mutable: flags&1 != 0, Kind: byte(flags >> 8), Data: p.Data[cursor:end:end]})
		cursor = end
	}
	if cursor != len(data) {
		return nil, fmt.Errorf("SGS: version 4 resource sizes do not match data")
	}
	return p, nil
}
