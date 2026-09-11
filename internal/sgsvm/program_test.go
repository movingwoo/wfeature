package sgsvm

import (
	"encoding/binary"
	"testing"
)

// parserFixture is authored here: one terminating instruction, two variables,
// and two resources. No original script or asset bytes are used.
func parserFixture(version byte) []byte {
	header := 52
	if version == 2 {
		header = 100
	}
	vd := header + 1
	vi := vd + 8
	rd := vi + 4
	ri := rd + 8
	data := make([]byte, ri+5)
	data[0] = version
	copy(data[10:], "Fixture")
	put := func(off, n int) { binary.LittleEndian.PutUint16(data[off:], uint16(n)) }
	put(28, header)
	put(32, header)
	put(44, vd)
	put(46, vi)
	put(48, rd)
	put(50, ri)
	data[header] = 0xff
	copy(data[vd:], []byte{0, 2, 0, 0, 1, 3, 0, 0})
	put(vi, 65535)
	put(vi+2, 123)
	copy(data[rd:], []byte{0, 7, 3, 0, 1, 2, 2, 0})
	copy(data[ri:], []byte{10, 20, 30, 40, 50})
	return data
}

func TestParseBanksAndWrapper(t *testing.T) {
	for _, version := range []byte{1, 2} {
		for _, wrapped := range []bool{false, true} {
			data := parserFixture(version)
			if wrapped {
				prefix := make([]byte, 32)
				prefix[0] = 32
				data = append(prefix, data...)
			}
			p, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if p.Name != "Fixture" || p.Entries[0] != p.Entries[2] || len(p.Variables) != 2 || len(p.Resources) != 2 {
				t.Fatalf("unexpected program: %+v", p)
			}
			if p.Variables[0].Mutable || p.Variables[0].Values[0] != -1 || p.Variables[0].Values[1] != 123 {
				t.Fatal("immutable initializer lost")
			}
			if !p.Variables[1].Mutable || len(p.Variables[1].Values) != 3 || p.Variables[1].Values[0] != 0 {
				t.Fatal("zero variable initializer lost")
			}
			if p.Resources[0].Kind != 7 || p.Resources[0].Mutable || !p.Resources[1].Mutable || p.Resources[1].Data[1] != 50 {
				t.Fatal("resource descriptor lost")
			}
			for i := range data {
				data[i] = 0
			}
			if p.Data[0] != version || p.Resources[0].Data[0] != 10 {
				t.Fatal("program aliases caller input")
			}
		}
	}
}

func TestParseMutableInitializer(t *testing.T) {
	data := parserFixture(1)
	vd := int(binary.LittleEndian.Uint16(data[44:]))
	data[vd] = 1
	data[vd+2] = 1
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Variables[0].Mutable || p.Variables[0].Values[0] != -1 {
		t.Fatal("mutable initializer lost")
	}
}

func TestParseLCDClassMinimum(t *testing.T) {
	for _, version := range []byte{1, 2} {
		for _, mask := range []byte{0, 4, 7, 8, 9, 12, 255} {
			data := parserFixture(version)
			data[1] = mask
			p, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			w, h := 128, 160
			if mask == 8 || mask == 9 {
				w, h = 176, 176
			}
			if p.Width != w || p.Height != h {
				t.Fatalf("version %d mask %02x: screen %dx%d, want %dx%d", version, mask, p.Width, p.Height, w, h)
			}
		}
	}
}

func TestParseRejectsMalformedRegions(t *testing.T) {
	cases := map[string]func([]byte) []byte{
		"short header":                   func(b []byte) []byte { return b[:30] },
		"extended short header":          func(b []byte) []byte { b[0] = 2; return b },
		"unknown version":                func(b []byte) []byte { b[0] = 3; return b },
		"table in header":                func(b []byte) []byte { binary.LittleEndian.PutUint16(b[44:], 4); return b },
		"unaligned descriptor":           func(b []byte) []byte { b[46]++; return b },
		"reversed regions":               func(b []byte) []byte { binary.LittleEndian.PutUint16(b[48:], 1); return b },
		"resource table beyond file":     func(b []byte) []byte { binary.LittleEndian.PutUint16(b[50:], 65535); return b },
		"entry in data":                  func(b []byte) []byte { copy(b[28:30], b[44:46]); return b },
		"entry in header":                func(b []byte) []byte { binary.LittleEndian.PutUint16(b[28:], 1); return b },
		"variable initializer truncated": func(b []byte) []byte { b[54] = 255; return b },
		"resource truncated":             func(b []byte) []byte { return b[:len(b)-1] },
		"mutable bank oversized": func(b []byte) []byte {
			rd := int(binary.LittleEndian.Uint16(b[48:]))
			b[rd] = 1
			binary.LittleEndian.PutUint16(b[rd+2:], 16384)
			return append(b, make([]byte, 16384)...)
		},
		"oversized file": func(b []byte) []byte { return make([]byte, 128*1024+33) },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(edit(parserFixture(1))); err == nil {
				t.Fatal("malformed program accepted")
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	f.Add(parserFixture(1))
	f.Add(parserFixture(2))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = Parse(b) })
}
