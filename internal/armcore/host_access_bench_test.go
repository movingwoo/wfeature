package armcore

import "testing"

// The Host draws a KTF title's sprites a pixel at a time through Memory.Read
// and Memory.Write (a 2-byte read and a 2-byte write per pixel, about 4,000 of
// each per tick on a light title), and reads a C string a byte at a time, so
// their fixed cost is a per-pixel and per-byte cost. These measure it on the
// shapes those make.

func hostAccessMemory(b *testing.B) (*Memory, uint32) {
	const base = uint32(0x40000)
	memory := NewMemory()
	if err := memory.Map(base, 1<<20, PermissionReadWrite); err != nil {
		b.Fatal(err)
	}
	if err := memory.Write(base, make([]byte, 1<<20)); err != nil {
		b.Fatal(err)
	}
	return memory, base
}

func BenchmarkHostRead2(b *testing.B) {
	memory, base := hostAccessMemory(b)
	var data [2]byte
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := memory.Read(base+uint32(index&0xffff)*2, data[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHostWrite2(b *testing.B) {
	memory, base := hostAccessMemory(b)
	var data [2]byte
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		data[0] = byte(index)
		if err := memory.Write(base+uint32(index&0xffff)*2, data[:]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHostBlitPixel is one blit pixel: a source read and a
// destination write, as wipicBlitClipped and blendPixel make them.
func BenchmarkHostBlitPixel(b *testing.B) {
	memory, base := hostAccessMemory(b)
	var data [2]byte
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		offset := uint32(index&0x7fff) * 2
		if err := memory.Read(base+offset, data[:]); err != nil {
			b.Fatal(err)
		}
		if err := memory.Write(base+0x80000+offset, data[:]); err != nil {
			b.Fatal(err)
		}
	}
}
