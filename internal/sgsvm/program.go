// Package sgsvm loads and executes the SGS script format.
package sgsvm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"golang.org/x/text/encoding/korean"
)

// Program contains validated script data and the initial variable and resource banks.
// Entries are initialization, termination, timer, key, two message callbacks,
// system notification, and an additional system callback, in that order.
type Program struct {
	CodeStart, CodeEnd int
	Data               []byte
	Name               string
	Variables          []Variable
	Constants          []int16
	Resources          []Resource
	Entries            [8]uint16
	Width, Height      int
}

// Variable is a bank of signed 16-bit script values.
type Variable struct {
	Mutable bool
	Offset  int // Word offset in the constant or mutable variable bank.
	Values  []int16
}

// Resource is a typed byte bank; immutable banks hold embedded assets or strings.
type Resource struct {
	Mutable bool
	Kind    byte
	Data    []byte
}

// Parse validates a script before exposing its code or allocating runtime banks.
func Parse(data []byte) (*Program, error) {
	if len(data) > 128*1024+32 {
		return nil, fmt.Errorf("SGS: script exceeds 128 KiB plus wrapper")
	}
	// Some downloads prepend a 32-byte, zero-filled wrapper whose first word
	// identifies its length. Internal addresses remain relative to the script.
	if len(data) >= 32 && binary.LittleEndian.Uint32(data) == 32 && bytes.Equal(data[4:32], make([]byte, 28)) {
		data = data[32:]
	}
	if len(data) < 52 || len(data) > 128*1024 {
		return nil, fmt.Errorf("SGS: invalid script size %d", len(data))
	}
	header := 52
	switch data[0] {
	case 1:
	case 2:
		header = 100
	default:
		return nil, fmt.Errorf("SGS: unsupported header version %d", data[0])
	}
	if len(data) < header {
		return nil, fmt.Errorf("SGS: truncated version %d header", data[0])
	}
	u16 := func(off int) int { return int(binary.LittleEndian.Uint16(data[off:])) }
	vd, vi, rd, ri := u16(44), u16(46), u16(48), u16(50)
	if vd < header || vi < vd || rd < vi || ri < rd || ri > len(data) || (vi-vd)%4 != 0 || (ri-rd)%4 != 0 {
		return nil, fmt.Errorf("SGS: invalid variable or resource regions")
	}
	p := &Program{Data: bytes.Clone(data), Width: 128, Height: 160, CodeStart: header, CodeEnd: vd}
	// The LCD mask describes size classes, not an exact handset resolution.
	// A script excluding the usual class 4 but accepting class 8 needs at
	// least 176 pixels on both axes. Hosts may still select another size.
	if data[1]&4 == 0 && data[1]&8 != 0 {
		p.Width, p.Height = 176, 176
	}
	for i := range p.Entries {
		p.Entries[i] = uint16(u16(28 + i*2))
		if e := int(p.Entries[i]); e != 0 && (e < header || e >= vd) {
			return nil, fmt.Errorf("SGS: callback %d outside code region", i)
		}
	}
	title := data[10:26]
	if n := bytes.IndexByte(title, 0); n >= 0 {
		title = title[:n]
	}
	decoded, err := korean.EUCKR.NewDecoder().Bytes(title)
	if err != nil {
		return nil, fmt.Errorf("SGS: invalid title: %w", err)
	}
	p.Name = strings.TrimSpace(string(decoded))
	nv, nr := (vi-vd)/4, (ri-rd)/4
	budget := nv*6 + nr*8
	if budget > 16*1024 {
		return nil, fmt.Errorf("SGS: runtime descriptors exceed 16 KiB")
	}
	p.Variables = make([]Variable, nv)
	cursor := vi
	mutableWords := 0
	for i := range p.Variables {
		d := data[vd+i*4 : vd+i*4+4]
		count := int(d[1])
		mutable := d[0] != 0
		if mutable {
			budget += count * 2
		}
		if budget > 16*1024 {
			return nil, fmt.Errorf("SGS: variables exceed runtime memory")
		}
		v := Variable{Mutable: mutable, Values: make([]int16, count)}
		v.Offset = (cursor - vi) / 2
		if mutable {
			v.Offset = mutableWords
			mutableWords += count
		}
		if !mutable || d[2] != 0 {
			if count*2 > rd-cursor {
				return nil, fmt.Errorf("SGS: truncated variable %d initializer", i)
			}
			for j := range v.Values {
				v.Values[j] = int16(binary.LittleEndian.Uint16(data[cursor+j*2:]))
			}
			cursor += count * 2
		}
		p.Variables[i] = v
	}
	p.Constants = make([]int16, (cursor-vi)/2)
	for i := range p.Constants {
		p.Constants[i] = int16(binary.LittleEndian.Uint16(data[vi+i*2:]))
	}
	p.Resources = make([]Resource, nr)
	cursor = ri
	for i := range p.Resources {
		d := data[rd+i*4 : rd+i*4+4]
		size := int(binary.LittleEndian.Uint16(d[2:]))
		mutable := d[0] != 0
		if mutable {
			budget += size
		}
		if budget > 16*1024 {
			return nil, fmt.Errorf("SGS: resources exceed runtime memory")
		}
		if size > len(data)-cursor {
			return nil, fmt.Errorf("SGS: truncated resource %d", i)
		}
		p.Resources[i] = Resource{Mutable: mutable, Kind: d[1], Data: bytes.Clone(data[cursor : cursor+size])}
		cursor += size
	}
	return p, nil
}
