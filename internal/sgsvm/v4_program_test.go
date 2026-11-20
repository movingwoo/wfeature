package sgsvm

import (
	"encoding/binary"
	"testing"
)

// The fixture includes an entry above the legacy address limit and a padded,
// initialized symbol. All offsets are relative to the header after the prefix.
func wideProgramFixture() []byte {
	const entry = 0x10080
	const symbols = entry + 2
	const words = symbols + 8
	const media = words + 4
	const pixels = media + 12
	data := make([]byte, 32+pixels+4)
	data[0], data[8] = 32, 0xc1
	b := data[32:]
	b[0], b[5] = 4, 4
	copy(b[10:26], "Word fixture")
	put := func(off, n int) { binary.LittleEndian.PutUint32(b[off:], uint32(n)) }
	put(28, entry)
	put(72, symbols)
	put(76, words)
	put(80, media)
	put(84, pixels)
	put(120, len(b))
	b[entry] = 0xff
	put(symbols, 1<<16|0x101)
	put(words, 0xfedcba98)
	put(media, 0x101)
	put(media+4, 4)
	copy(b[pixels:], "data")
	return data
}

func TestParseWideDownload(t *testing.T) {
	data := wideProgramFixture()
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	clear(data)
	if p.Wide == nil || p.CodeEnd != 0x10082 || p.Wide.Entries[0] != 0x10080 || p.Name != "Word fixture" ||
		uint32(p.Wide.Variables[0][0]) != 0xfedcba98 || !p.Resources[0].Mutable || p.Resources[0].Kind != 1 ||
		string(p.Resources[0].Data) != "data" {
		t.Fatal("incorrect decoded wide program")
	}
}
func TestRejectMalformedWideProgram(t *testing.T) {
	for _, field := range []int{28, 72, 76, 80, 84, 120} {
		b := wideProgramFixture()
		binary.LittleEndian.PutUint32(b[32+field:], 0xfffffffe)
		if _, err := Parse(b); err == nil {
			t.Fatalf("accepted corrupt field at %d", field)
		}
	}
	b := wideProgramFixture()
	b[32+28] |= 1
	if _, err := Parse(b); err == nil {
		t.Fatal("accepted unaligned callback")
	}
	b = wideProgramFixture()
	b[32+0x10082+2]++
	if _, err := Parse(b); err == nil {
		t.Fatal("accepted overlapping symbol data")
	}
}
