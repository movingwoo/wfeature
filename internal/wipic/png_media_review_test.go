package wipic

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"testing"
)

// allocationBudget is far above what decoding these small inputs needs and far
// below what their headers would make an unbounded decoder allocate.
const allocationBudget = 16 << 20

func pngChunkBytes(tag string, data []byte) []byte {
	chunk := make([]byte, 8, 12+len(data))
	binary.BigEndian.PutUint32(chunk, uint32(len(data)))
	copy(chunk[4:], tag)
	chunk = append(chunk, data...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}

func pngHeaderBytes(width, height uint32, depth, colorType, interlace byte) []byte {
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header, width)
	binary.BigEndian.PutUint32(header[4:], height)
	header[8], header[9], header[12] = depth, colorType, interlace
	return header
}

func zlibBytes(t *testing.T, write func(*zlib.Writer)) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	write(writer)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func assemblePNG(header, idat []byte) []byte {
	data := append([]byte(nil), pngSignature[:]...)
	data = append(data, pngChunkBytes("IHDR", header)...)
	data = append(data, pngChunkBytes("IDAT", idat)...)
	return append(data, pngChunkBytes("IEND", nil)...)
}

// allocatedBy answers how many bytes the call allocated on the heap.
func allocatedBy(call func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	call()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// A header is checked against the framebuffer pixel limit before the decoder
// allocates for it, so a few bytes naming an enormous picture cost a decode
// error rather than the picture's whole buffer.
func TestDecodeStandardRefusesAnOversizedHeaderBeforeAllocating(t *testing.T) {
	stream := zlibBytes(t, func(writer *zlib.Writer) { writer.Write([]byte{0}) })
	data := assemblePNG(pngHeaderBytes(8000, 8000, 8, 6, 0), stream)
	var err error
	allocated := allocatedBy(func() { _, _, err = DecodeStandard(data) })
	if err == nil {
		t.Fatal("an 8000x8000 header with no pixels decoded")
	}
	if allocated > allocationBudget {
		t.Fatalf("refusing a %d-byte PNG allocated %d bytes", len(data), allocated)
	}
}

func TestDecodeBitmapRefusesAnOversizedHeaderBeforeAllocating(t *testing.T) {
	data := make([]byte, 54)
	copy(data, "BM")
	binary.LittleEndian.PutUint32(data[2:], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[10:], 54)
	binary.LittleEndian.PutUint32(data[14:], 40)
	binary.LittleEndian.PutUint32(data[18:], 8000)
	binary.LittleEndian.PutUint32(data[22:], 8000)
	binary.LittleEndian.PutUint16(data[26:], 1)
	binary.LittleEndian.PutUint16(data[28:], 32)
	var err error
	allocated := allocatedBy(func() { _, err = DecodeBitmap(data) })
	if err == nil {
		t.Fatal("an 8000x8000 header with no pixels decoded")
	}
	if allocated > allocationBudget {
		t.Fatalf("refusing a %d-byte BMP allocated %d bytes", len(data), allocated)
	}
}

// The header check does not get in the way of the repairs: a recoloured
// palette, whose header the standard reader refuses over its checksum, still
// decodes, and so does an ordinary picture and an ordinary bitmap.
func TestBoundedDecodeKeepsOrdinaryPicturesAndRepairs(t *testing.T) {
	encoded := palettePNG(t)
	if _, _, err := DecodeStandard(encoded); err != nil {
		t.Fatalf("an ordinary PNG: %v", err)
	}
	data, _ := findChunk(t, encoded, "PLTE")
	edited := append([]byte(nil), encoded...)
	edited[data], edited[data+1], edited[data+2] = 0x11, 0x22, 0x33
	decoded, _, err := DecodeStandard(edited)
	if err != nil {
		t.Fatalf("a recoloured palette: %v", err)
	}
	if got := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA); got.R != 0x11 || got.G != 0x22 || got.B != 0x33 {
		t.Fatalf("the recoloured entry decoded as %+v", got)
	}
	bitmap, err := EncodeBitmap(image.NewRGBA(image.Rect(0, 0, 3, 2)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeBitmap(bitmap); err != nil {
		t.Fatalf("an ordinary bitmap: %v", err)
	}
}

// A pixel stream that inflates far past what its header describes is not one
// whose trailer is the only thing wrong, and the repair stops reading it
// rather than inflating all of it.
func TestPixelChecksumRepairStopsAtTheHeaderSize(t *testing.T) {
	zeros := make([]byte, 1<<20)
	stream := zlibBytes(t, func(writer *zlib.Writer) {
		for range 32 {
			writer.Write(zeros)
		}
	})
	data := assemblePNG(pngHeaderBytes(1, 1, 8, 0, 0), stream)
	var err error
	allocated := allocatedBy(func() { _, _, err = DecodeStandard(data) })
	if err == nil {
		t.Fatal("a 1x1 picture with 32 MiB of pixel data decoded")
	}
	if allocated > allocationBudget {
		t.Fatalf("refusing a %d-byte PNG allocated %d bytes", len(data), allocated)
	}
	if _, mended := RepairPNGPixelChecksum(data); mended {
		t.Fatal("a stream longer than its picture was reported as repaired")
	}
}

// adam7Stream is the filtered pixel data of an interlaced picture whose rows
// are all zero, which is what the trailer repair has to size exactly.
func adam7Stream(width, height, bitsPerPixel int) []byte {
	passes := [7][4]int{{0, 0, 8, 8}, {4, 0, 8, 8}, {0, 4, 4, 8}, {2, 0, 4, 4}, {0, 2, 2, 4}, {1, 0, 2, 2}, {0, 1, 1, 2}}
	var raw []byte
	for _, pass := range passes {
		columns := (width - pass[0] + pass[2] - 1) / pass[2]
		rows := (height - pass[1] + pass[3] - 1) / pass[3]
		if columns <= 0 || rows <= 0 {
			continue
		}
		raw = append(raw, make([]byte, rows*(1+(columns*bitsPerPixel+7)/8))...)
	}
	return raw
}

// The repair still mends a wrong trailer on every shape of pixel stream: the
// size it stops at is the size the header describes, not one byte less.
func TestPixelChecksumRepairKeepsEveryPixelLayout(t *testing.T) {
	encodedWith := func(picture image.Image) []byte {
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, picture); err != nil {
			t.Fatal(err)
		}
		return encoded.Bytes()
	}
	paletted := image.NewPaletted(image.Rect(0, 0, 13, 5), color.Palette{color.Black, color.White})
	cases := map[string][]byte{
		"two-colour palette": encodedWith(paletted),
		"gray":               encodedWith(image.NewGray(image.Rect(0, 0, 7, 3))),
		"gray16":             encodedWith(image.NewGray16(image.Rect(0, 0, 7, 3))),
		"rgba":               encodedWith(image.NewNRGBA(image.Rect(0, 0, 9, 4))),
		"rgba64":             encodedWith(image.NewNRGBA64(image.Rect(0, 0, 9, 4))),
		"interlaced rgb": assemblePNG(pngHeaderBytes(11, 13, 8, 2, 1), zlibBytes(t, func(writer *zlib.Writer) {
			writer.Write(adam7Stream(11, 13, 24))
		})),
		"interlaced one-bit gray": assemblePNG(pngHeaderBytes(3, 2, 1, 0, 1), zlibBytes(t, func(writer *zlib.Writer) {
			writer.Write(adam7Stream(3, 2, 1))
		})),
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := png.Decode(bytes.NewReader(encoded)); err != nil {
				t.Fatalf("the fixture itself does not decode: %v", err)
			}
			_, checksum := findChunk(t, encoded, "IDAT")
			edited := append([]byte(nil), encoded...)
			edited[checksum-4] ^= 0xff
			edited, _ = RepairPNGChecksums(edited)
			if _, err := png.Decode(bytes.NewReader(edited)); err == nil {
				t.Fatal("a wrong trailer decoded, so this proves nothing")
			}
			if _, _, err := DecodeStandard(edited); err != nil {
				t.Fatalf("the trailer was not repaired: %v", err)
			}
		})
	}
}
