package wipic

import (
	"bytes"
	"encoding/binary"
	stdimage "image"
	"image/color"

	"golang.org/x/image/bmp"
)

// A BMP these titles ship can declare one of its palette entries transparent,
// and the declaration is in two fields the format itself does not use that
// way: `bfReserved1` in the file header is 1, and `biClrImportant` in the info
// header is the index. BMP has no alpha channel and no transparency of its
// own, so without this a sprite paints its backdrop as a solid rectangle —
// which is what one title's characters looked like: cut-outs on a green box.
//
// The reading is not a guess. In the title that ships 1,301 of these, every
// one of the 1,259 images with `bfReserved1 == 1` names an index whose colour
// is that backdrop green, with no exceptions, and the colour covers about half
// of a typical sprite. The 42 images with `bfReserved1 == 0` are the
// backgrounds, which have no backdrop to drop. Two other titles carry one such
// BMP each and use a different colour, so **the colour belongs to the title
// while the mechanism belongs to the format** — which is why no transparent
// colour is written down here.
const (
	bitmapPixelOffset      = 10
	bitmapReservedOffset   = 6
	bitmapInfoHeaderOffset = 14
	bitmapBitCountOffset   = 28
	bitmapClrImportant     = 50
	bitmapPaletteEntrySize = 4

	// bitmapDeclaresTransparency is the value `bfReserved1` carries when the
	// important-colour field is an index rather than the count BMP means by
	// it. Zero is what every other writer leaves there.
	bitmapDeclaresTransparency = 1
)

// DecodeBitmap decodes a BMP, applying the transparency its header declared.
// The result is a paletted image whose transparent entry has an alpha of zero,
// so it carries its transparency the same way an encoding with an alpha
// channel does and every caller reads it the same way.
func DecodeBitmap(encoded []byte) (stdimage.Image, error) {
	decoded, err := bmp.Decode(bytes.NewReader(bitmapPaletteHeader(encoded)))
	if err != nil {
		return nil, err
	}
	index, declared := bitmapTransparentIndex(encoded)
	if !declared {
		return decoded, nil
	}
	paletted, ok := decoded.(*stdimage.Paletted)
	if !ok || index >= len(paletted.Palette) {
		return decoded, nil
	}
	// The entry's alpha is cleared and its colour kept. Clearing the whole
	// entry would lose the backdrop colour, and the colour is not decoration:
	// a surface handed to a guest is RGB565 with no alpha, so the colour is
	// the only transparency such a guest can read. See lgt.md, "A transparent
	// pixel still has a colour".
	//
	// The entry is cleared rather than the pixels rewritten: an index is
	// exactly the set of pixels the title meant, while matching on colour
	// would also take out a second entry that happens to hold the same value.
	masked := *paletted
	masked.Palette = make(color.Palette, len(paletted.Palette))
	copy(masked.Palette, paletted.Palette)
	red, green, blue, _ := paletted.Palette[index].RGBA()
	masked.Palette[index] = color.NRGBA{R: uint8(red >> 8), G: uint8(green >> 8), B: uint8(blue >> 8)}
	return &masked, nil
}

// bitmapTransparentIndex answers the palette index a BMP declared transparent,
// and whether it declared one at all.
func bitmapTransparentIndex(encoded []byte) (int, bool) {
	if len(encoded) < bitmapClrImportant+4 {
		return 0, false
	}
	if encoded[0] != 'B' || encoded[1] != 'M' {
		return 0, false
	}
	if binary.LittleEndian.Uint16(encoded[bitmapReservedOffset:]) != bitmapDeclaresTransparency {
		return 0, false
	}
	// Only a palette carries indices. A 16- or 24-bit BMP has none, and the
	// field would then be the count of colours the format intends by it.
	if binary.LittleEndian.Uint16(encoded[bitmapBitCountOffset:]) > 8 {
		return 0, false
	}
	// The info header's size gives where the palette starts and the pixel
	// offset gives where it ends, which bounds the index without trusting it.
	// The extended 8-bit variant carries a 0x100 flag above the index.
	// Other high bits remain invalid, and the actual palette still bounds it.
	infoSize := binary.LittleEndian.Uint32(encoded[bitmapInfoHeaderOffset:])
	pixelOffset := binary.LittleEndian.Uint32(encoded[bitmapPixelOffset:])
	paletteStart := uint64(bitmapInfoHeaderOffset) + uint64(infoSize)
	if uint64(pixelOffset) <= paletteStart {
		return 0, false
	}
	entries := (uint64(pixelOffset) - paletteStart) / bitmapPaletteEntrySize
	index := binary.LittleEndian.Uint32(encoded[bitmapClrImportant:])
	if binary.LittleEndian.Uint16(encoded[bitmapBitCountOffset:]) == 8 && index&^uint32(0xff) == 0x100 {
		index &= 0xff
	}
	if uint64(index) >= entries {
		return 0, false
	}
	return int(index), true
}

// EncodeBitmap writes an image as an uncompressed BMP, which is the format the
// specification names for MC_grpEncodeImage and Graphics.encodeImage: a title
// encodes a region of a framebuffer, keeps the bytes, and hands them back to
// image creation later, so what is written here has to be what DecodeBitmap
// reads. No transparency is declared, because a framebuffer has none to
// declare — its pixels are already composited.
func EncodeBitmap(image stdimage.Image) ([]byte, error) {
	var encoded bytes.Buffer
	if err := bmp.Encode(&encoded, image); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// bitmapPaletteHeader normalizes the observed extended 8-bit BMP variant:
// biClrUsed remains 256 even when the encoder writes a shorter palette. Only
// its explicit transparency marker admits this repair; standard BMPs retain
// the decoder's normal validation. Pixel indices and row lengths are still
// checked by bmp.Decode, and the source bytes are never changed.
func bitmapPaletteHeader(encoded []byte) []byte {
	if len(encoded) < 54 || string(encoded[:2]) != "BM" ||
		binary.LittleEndian.Uint16(encoded[6:]) != 1 ||
		binary.LittleEndian.Uint32(encoded[14:]) != 40 ||
		binary.LittleEndian.Uint16(encoded[28:]) != 8 ||
		binary.LittleEndian.Uint32(encoded[30:]) != 0 ||
		binary.LittleEndian.Uint32(encoded[46:]) != 256 ||
		binary.LittleEndian.Uint32(encoded[50:])&^uint32(0xff) != 0x100 {
		return encoded
	}
	offset := uint64(binary.LittleEndian.Uint32(encoded[10:]))
	if offset <= 54 || offset > uint64(len(encoded)) || (offset-54)%4 != 0 {
		return encoded
	}
	entries := (offset - 54) / 4
	if entries >= 256 {
		return encoded
	}
	// Do not turn a rejected header into a large allocation before the pixel
	// payload is checked. An 8-bit row is padded to four bytes.
	width := int64(int32(binary.LittleEndian.Uint32(encoded[18:])))
	height := int64(int32(binary.LittleEndian.Uint32(encoded[22:])))
	if height < 0 {
		height = -height
	}
	if width <= 0 || height == 0 || offset+uint64((width+3)&^3)*uint64(height) > uint64(len(encoded)) {
		return encoded
	}
	normalized := append([]byte(nil), encoded...)
	binary.LittleEndian.PutUint32(normalized[46:], uint32(entries))
	return normalized
}
