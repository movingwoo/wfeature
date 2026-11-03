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

// palettePNG builds the shape a title recolours: a small paletted picture,
// encoded the way one is shipped.
func palettePNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewPaletted(image.Rect(0, 0, 4, 2), color.Palette{
		color.NRGBA{R: 0xae, G: 0xae, B: 0xae, A: 0xff},
		color.NRGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff},
	})
	picture.SetColorIndex(1, 1, 1)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// findChunk answers where the named chunk's data and its checksum sit.
func findChunk(t *testing.T, data []byte, name string) (int, int) {
	t.Helper()
	for at := pngHeader; at+3*pngChunkTag <= len(data); {
		length := int(binary.BigEndian.Uint32(data[at:]))
		end := at + 2*pngChunkTag + length + pngChunkTag
		if string(data[at+pngChunkTag:at+2*pngChunkTag]) == name {
			return at + 2*pngChunkTag, end - pngChunkTag
		}
		at = end
	}
	t.Fatalf("no %s chunk", name)
	return 0, 0
}

// The case this exists for: a title writes its own colours over the palette and
// leaves the checksum the encoder wrote. The picture is whole and the four
// bytes at the end of the chunk are the only thing wrong with it.
func TestRepairedPaletteDecodes(t *testing.T) {
	encoded := palettePNG(t)
	data, _ := findChunk(t, encoded, "PLTE")
	edited := make([]byte, len(encoded))
	copy(edited, encoded)
	edited[data], edited[data+1], edited[data+2] = 0x11, 0x22, 0x33

	if _, err := png.Decode(bytes.NewReader(edited)); err == nil {
		t.Fatal("the edited picture decoded without a repair, so this proves nothing")
	}
	repaired, mended := RepairPNGChecksums(edited)
	if !mended {
		t.Fatal("RepairPNGChecksums found nothing to repair")
	}
	decoded, err := png.Decode(bytes.NewReader(repaired))
	if err != nil {
		t.Fatalf("the repaired picture does not decode: %v", err)
	}
	got := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	if got.R != 0x11 || got.G != 0x22 || got.B != 0x33 {
		t.Errorf("the first palette entry decoded as %+v, want the colour the title wrote", got)
	}
	if bytes.Equal(repaired, edited) {
		t.Error("the repair answered the same bytes it was given")
	}
}

// A picture whose checksums are all correct is left alone, and no copy of it is
// made: the repair is only ever reached after a decode has failed, and a file
// that fails for another reason must not be reported as mended.
func TestUneditedPictureIsNotReportedAsRepaired(t *testing.T) {
	encoded := palettePNG(t)
	if _, mended := RepairPNGChecksums(encoded); mended {
		t.Error("an untouched picture was reported as repaired")
	}
}

// Damage that a checksum is not the whole of stays damage. The chunk is
// repaired, the picture still does not decode, and the caller reports the
// decoder's own failure rather than a picture made out of nothing.
func TestRepairDoesNotRescueABrokenStream(t *testing.T) {
	encoded := palettePNG(t)
	data, checksum := findChunk(t, encoded, "IDAT")
	edited := make([]byte, len(encoded))
	copy(edited, encoded)
	for index := data; index < checksum; index++ {
		edited[index] = 0
	}
	repaired, mended := RepairPNGChecksums(edited)
	if !mended {
		t.Fatal("a chunk whose data was rewritten kept its checksum")
	}
	if _, err := png.Decode(bytes.NewReader(repaired)); err == nil {
		t.Error("a picture with no deflate stream in it decoded")
	}
}

// Anything that is not a walkable PNG answers "nothing to repair", so a caller
// can tell that apart from a picture whose only problem was four bytes.
func TestUnwalkableInputIsRefused(t *testing.T) {
	encoded := palettePNG(t)
	cases := map[string][]byte{
		"not a PNG at all":     []byte("BM not a png"),
		"the signature alone":  encoded[:pngHeader],
		"a truncated chunk":    encoded[:pngHeader+12],
		"an impossible length": impossibleLength(t, encoded),
	}
	for name, data := range cases {
		if _, mended := RepairPNGChecksums(data); mended {
			t.Errorf("%s was reported as repaired", name)
		}
	}
}

// impossibleLength rewrites the first chunk's length to one that runs past the
// end of the file, with a checksum that matches so the walk is what refuses it.
func impossibleLength(t *testing.T, encoded []byte) []byte {
	t.Helper()
	edited := make([]byte, len(encoded))
	copy(edited, encoded)
	binary.BigEndian.PutUint32(edited[pngHeader:], uint32(len(encoded)))
	return edited
}

// The other checksum a picture carries. The pixels inside IDAT are a zlib
// stream whose last four bytes hash the *uncompressed* data, and one local
// title ships a sprite sheet whose trailer disagrees: the stream inflates
// perfectly and only those four bytes are wrong. A handset's decoder never
// looks at them.
func TestAPixelStreamWithAWrongTrailerStillDecodes(t *testing.T) {
	encoded := palettePNG(t)
	_, checksum := findChunk(t, encoded, "IDAT")
	edited := make([]byte, len(encoded))
	copy(edited, encoded)
	// The trailer is the last four bytes of the IDAT payload, in front of the
	// chunk's own CRC.
	trailer := checksum - 4
	edited[trailer] ^= 0xff
	// The chunk CRC is corrected, so that what is left is only the Adler-32:
	// otherwise the repair above would be the one doing the work.
	repairedCRC, mended := RepairPNGChecksums(edited)
	if !mended {
		t.Fatal("editing the payload left the chunk CRC valid, so this proves nothing")
	}
	if _, err := png.Decode(bytes.NewReader(repairedCRC)); err == nil {
		t.Fatal("the edited picture decoded with a wrong trailer, so this proves nothing")
	}

	repaired, fixed := RepairPNGPixelChecksum(repairedCRC)
	if !fixed {
		t.Fatal("RepairPNGPixelChecksum found nothing to repair")
	}
	if _, err := png.Decode(bytes.NewReader(repaired)); err != nil {
		t.Fatalf("the repaired picture did not decode: %v", err)
	}
	// And the router gets there on its own, which is what every platform's
	// image path actually calls.
	if _, _, err := DecodeStandard(repairedCRC); err != nil {
		t.Fatalf("DecodeStandard did not repair the trailer: %v", err)
	}

	// A picture whose trailer is right is left alone, and one whose pixels are
	// genuinely damaged is not rescued by rewriting a checksum.
	if _, fixed := RepairPNGPixelChecksum(encoded); fixed {
		t.Error("an intact picture was reported as repaired")
	}
	data, _ := findChunk(t, encoded, "IDAT")
	broken := make([]byte, len(encoded))
	copy(broken, encoded)
	for index := data; index < data+4 && index < len(broken); index++ {
		broken[index] ^= 0xff
	}
	broken, _ = RepairPNGChecksums(broken)
	if _, fixed := RepairPNGPixelChecksum(broken); fixed {
		t.Error("a stream that does not inflate was reported as repaired")
	}
}

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

// A title hands the decoder the picture less its last five bytes, which a
// picture packaged without the trailer it expects loses from its IEND. The
// pixels are all there; the standard reader refuses the picture anyway, and a
// handset's decoder, which stops once the pixels have inflated, does not.
func TestAPictureCutShortInItsEndStillDecodes(t *testing.T) {
	encoded := palettePNG(t)
	for _, cut := range []int{1, 5, 8, 12} {
		short := encoded[:len(encoded)-cut]
		if _, _, err := decodeBounded(short); err == nil {
			t.Fatalf("cut %d: the short picture decoded without a repair, so this proves nothing", cut)
		}
		decoded, _, err := DecodeStandard(short)
		if err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		if got := color.NRGBAModel.Convert(decoded.At(1, 1)).(color.NRGBA); got.R != 0xff || got.G != 0 || got.B != 0xff {
			t.Errorf("cut %d: the marked pixel decoded as %+v", cut, got)
		}
	}
}

// Only the end is supplied. A picture that already has its IEND, one cut inside
// its pixel stream, and one that never reached its first IDAT are not pictures
// that lost their end, and none of them is reported as repaired into one that
// decodes.
func TestRepairPNGEndSuppliesOnlyTheEnd(t *testing.T) {
	encoded := palettePNG(t)
	if _, mended := RepairPNGEnd(encoded); mended {
		t.Error("a whole picture was reported as missing its end")
	}
	data, checksum := findChunk(t, encoded, "IDAT")
	inside := encoded[:data+(checksum-data)/2]
	if repaired, mended := RepairPNGEnd(inside); mended {
		if _, err := png.Decode(bytes.NewReader(repaired)); err == nil {
			t.Error("a picture cut inside its pixel stream decoded")
		}
	}
	if _, _, err := DecodeStandard(inside); err == nil {
		t.Error("DecodeStandard decoded a picture cut inside its pixel stream")
	}
	before, _ := findChunk(t, encoded, "IDAT")
	if _, mended := RepairPNGEnd(encoded[:before-2*pngChunkTag]); mended {
		t.Error("a picture with no pixel stream was given an end")
	}
	if _, mended := RepairPNGEnd([]byte("BM not a png")); mended {
		t.Error("a bitmap was given a PNG end")
	}
	short := encoded[:len(encoded)-5]
	repaired, mended := RepairPNGEnd(short)
	if !mended {
		t.Fatal("a picture missing part of its IEND was not repaired")
	}
	if !bytes.Equal(repaired, encoded) {
		t.Error("the repaired picture is not the picture before the cut")
	}
	if len(short) != len(encoded)-5 || &short[0] == &repaired[0] {
		t.Error("the repair wrote over the bytes it was given")
	}
}
