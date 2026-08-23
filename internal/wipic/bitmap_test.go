package wipic

import (
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

// paletteBitmap builds an 8-bit BMP the way the titles here ship one: a two
// entry palette, one row of pixels, and the two header fields that carry the
// transparency declaration.
func paletteBitmap(reserved uint16, important uint32, indices []byte) []byte {
	const (
		fileHeader = 14
		infoHeader = 40
		entries    = 2
	)
	stride := (len(indices) + 3) &^ 3
	offset := fileHeader + infoHeader + entries*4
	data := make([]byte, offset+stride)
	copy(data, "BM")
	binary.LittleEndian.PutUint32(data[2:], uint32(len(data)))
	binary.LittleEndian.PutUint16(data[6:], reserved)
	binary.LittleEndian.PutUint32(data[10:], uint32(offset))
	binary.LittleEndian.PutUint32(data[14:], infoHeader)
	binary.LittleEndian.PutUint32(data[18:], uint32(len(indices))) // width
	binary.LittleEndian.PutUint32(data[22:], 1)                    // height
	binary.LittleEndian.PutUint16(data[26:], 1)                    // planes
	binary.LittleEndian.PutUint16(data[28:], 8)                    // bits
	binary.LittleEndian.PutUint32(data[46:], entries)              // biClrUsed
	binary.LittleEndian.PutUint32(data[50:], important)            // biClrImportant
	// Palette entries are BGRA: index 0 is the backdrop green these titles
	// use, index 1 is red.
	copy(data[fileHeader+infoHeader:], []byte{0x20, 0x90, 0x20, 0, 0, 0, 0xff, 0})
	copy(data[offset:], indices)
	return data
}

func alphaAt(t *testing.T, decoded image.Image, x int) uint32 {
	t.Helper()
	_, _, _, alpha := decoded.At(x, 0).RGBA()
	return alpha
}

// A BMP carries no transparency of its own, so a title declares it in the two
// header fields the format leaves spare. Without this a sprite paints its
// backdrop as a solid rectangle, which is what one title's characters looked
// like: cut-outs on a green box.
func TestBitmapHeaderDeclaresATransparentPaletteEntry(t *testing.T) {
	decoded, err := DecodeBitmap(paletteBitmap(1, 0, []byte{0, 1, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if alpha := alphaAt(t, decoded, 0); alpha != 0 {
		t.Fatalf("declared entry alpha = %d, want it transparent", alpha)
	}
	if alpha := alphaAt(t, decoded, 1); alpha == 0 {
		t.Fatal("a pixel that is not the declared entry came back transparent")
	}
}

// The declaration is the whole of it: an image that does not make it is opaque,
// including one that happens to be painted in another image's backdrop colour.
func TestBitmapWithoutTheDeclarationStaysOpaque(t *testing.T) {
	decoded, err := DecodeBitmap(paletteBitmap(0, 0, []byte{0, 1, 0}))
	if err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 3; x++ {
		if alpha := alphaAt(t, decoded, x); alpha == 0 {
			t.Fatalf("pixel %d came back transparent without a declaration", x)
		}
	}
}

// One title's status bar declares an index of 304 against a 256-entry palette.
// Whatever that means, it is not an index, and an image that names no palette
// entry has to stay whole rather than lose whichever entry the number wrapped
// onto.
func TestBitmapIgnoresAnIndexOutsideThePalette(t *testing.T) {
	decoded, err := DecodeBitmap(paletteBitmap(1, 304, []byte{0, 1, 0}))
	if err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 3; x++ {
		if alpha := alphaAt(t, decoded, x); alpha == 0 {
			t.Fatalf("pixel %d came back transparent for an out-of-range index", x)
		}
	}
}

// A transparent pixel keeps the colour the palette gave it. The surfaces these
// decoders feed are 16-bit colour with no alpha, and a title that runs its own
// blitter over them — which is what the LGT titles do, through
// MC_grpGetImageFrameBuffer — has nothing but the colour to key on. Clearing
// the whole palette entry hands it black instead, the key never matches, and
// every sprite paints its backdrop as a solid box.
func TestATransparentPaletteEntryKeepsItsColour(t *testing.T) {
	decoded, err := DecodeBitmap(paletteBitmap(1, 0, []byte{0, 1, 0}))
	if err != nil {
		t.Fatal(err)
	}
	// paletteBitmap writes index 0 as the backdrop green, BGRA 0x20 0x90 0x20.
	red, green, blue, alpha := decoded.At(0, 0).RGBA()
	if alpha != 0 {
		t.Fatalf("declared entry alpha = %d, want it transparent", alpha)
	}
	// RGBA premultiplies, so the transparent entry reads as black through it.
	// The colour survives in the non-premultiplied form, which is what the
	// conversion to guest pixels reads.
	if red|green|blue != 0 {
		t.Fatalf("premultiplied colour = %d,%d,%d, want black", red, green, blue)
	}
	nrgba, ok := decoded.(*image.Paletted)
	if !ok {
		t.Fatalf("decoded is %T, want a paletted image", decoded)
	}
	entry := color.NRGBAModel.Convert(nrgba.Palette[0]).(color.NRGBA)
	if entry.R != 0x20 || entry.G != 0x90 || entry.B != 0x20 {
		t.Fatalf("transparent entry colour = %02x%02x%02x, want 209020",
			entry.R, entry.G, entry.B)
	}
	if entry.A != 0 {
		t.Fatalf("transparent entry alpha = %d, want 0", entry.A)
	}
}

// A game's BMP is untrusted input, and one whose pixels name a palette entry
// that is not there used to decode without complaint and then panic on the
// first read — `index out of range [5] with length 2`, raised inside the
// standard image interface rather than at the decode. A guest that ships such
// a file could therefore take down whatever was drawing it, which for the
// server Host is the process the other players' sessions are in as well
// (GO-2026-5031, fixed in golang.org/x/image v0.41.0). The decoder rejects the
// file now; what this pins is that the rejection reaches the caller as an
// error and never as a decoded image whose pixels cannot be read.
func TestBitmapWhosePixelsNameAMissingPaletteEntryIsRejected(t *testing.T) {
	// The palette holds two entries; the pixels name entry 5 and entry 200.
	decoded, err := DecodeBitmap(paletteBitmap(0, 0, []byte{0, 5, 200}))
	if err == nil {
		// Not a failure by itself — a decoder that clamped instead of
		// refusing would be fine too, as long as every pixel is readable.
		for x := 0; x < 3; x++ {
			decoded.At(x, 0)
		}
		return
	}
	if decoded != nil {
		t.Fatalf("a refused bitmap came back with an image as well: %T", decoded)
	}
}

// The same file with the transparency declaration on it, because that path
// reads the palette itself: a declared index is checked against the palette
// the decoder returned, so a decode that failed must not be carried past.
func TestBitmapWithAMissingPaletteEntryAndADeclarationIsRejected(t *testing.T) {
	decoded, err := DecodeBitmap(paletteBitmap(1, 1, []byte{0, 5, 200}))
	if err == nil {
		for x := 0; x < 3; x++ {
			decoded.At(x, 0)
		}
		return
	}
	if decoded != nil {
		t.Fatalf("a refused bitmap came back with an image as well: %T", decoded)
	}
}
