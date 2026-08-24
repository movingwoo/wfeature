package wipic

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
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
