package wipic

import (
	"encoding/binary"
	"fmt"
	stdimage "image"
	"image/color"
)

// LBMP is a handset bitmap: a 24-byte header and then the pixels the LCD would
// hold, with no compression, no palette and no header the standard library
// knows. A title ships it beside its PNGs for the images it wants handed
// straight to the screen.
//
// It is not in the WIPI specification — it is a vendor's format — so what is
// written down here comes from the files themselves. One local title carries
// five of them, and they settle the layout completely: every one is exactly
// 24 + width * height * (type / 8) bytes long, which fixes `size` as the
// payload length and `type` as the bit depth in one arithmetic.
//
// The colour reading is settled by the pixel values rather than assumed. In
// the darkest of the five, eleven distinct bytes cover the whole image, and
// read as RGB332 they are a single clean ramp — 0x00, 0x20, 0x24, 0x44, 0x48,
// 0x68, 0x8c, 0xb0, 0xb1, 0xd5, 0xf9, which is red a little above green with
// blue at zero throughout. That is a gold-on-black gradient, and the image is
// a gold dragon on black; the file beside it decodes as green foliage. A
// palette-indexed reading would have to explain why the indices are scattered
// across the whole byte range instead of counting from zero, and it cannot.
const (
	lbmpHeaderSize = 24
	// lbmpMagic is the first four bytes, which are also what tells this format
	// from every other encoding a title ships.
	lbmpMagic = "LBMP"

	// The two depths that appear, as bits per pixel.
	lbmpDepthRGB332 = 8
	lbmpDepthRGB565 = 16
)

// maxLBMPPixels bounds what a header may claim. The dimensions come out of an
// archive this build treats as untrusted, and a header naming a billion pixels
// has to cost a refused decode rather than an allocation.
const maxLBMPPixels = 1 << 24

// IsLBMP reports whether these bytes carry the handset bitmap header. Callers
// use it to route a decode, so it answers on the magic alone and leaves every
// other complaint to DecodeLBMP — a truncated LBMP should be reported as a bad
// LBMP rather than fall through to a decoder that will call it a bad PNG.
func IsLBMP(encoded []byte) bool {
	return len(encoded) >= lbmpHeaderSize && string(encoded[:4]) == lbmpMagic
}

// DecodeLBMP decodes a handset bitmap into an NRGBA image, which is how every
// other decode here answers and therefore what the callers already handle.
func DecodeLBMP(encoded []byte) (stdimage.Image, error) {
	if !IsLBMP(encoded) {
		return nil, fmt.Errorf("not an LBMP image")
	}
	depth := binary.LittleEndian.Uint32(encoded[4:])
	width := binary.LittleEndian.Uint32(encoded[8:])
	height := binary.LittleEndian.Uint32(encoded[12:])
	size := binary.LittleEndian.Uint32(encoded[16:])
	// The sixth word is zero in every file seen. Nothing here reads it: a
	// guess at a colour key would put holes in an image on no evidence, and
	// the format's own transparency, if it has one, has never been exercised.

	if width == 0 || height == 0 || uint64(width)*uint64(height) > maxLBMPPixels {
		return nil, fmt.Errorf("LBMP dimensions %dx%d are out of range", width, height)
	}
	var bytesPerPixel uint64
	switch depth {
	case lbmpDepthRGB332:
		bytesPerPixel = 1
	case lbmpDepthRGB565:
		bytesPerPixel = 2
	default:
		// Two and three are the vendor's grayscale depths and nothing local
		// ships one. Reporting the depth is what a later archive needs, and it
		// is better than decoding it as something it is not.
		return nil, fmt.Errorf("LBMP depth %d is not supported", depth)
	}
	expected := uint64(width) * uint64(height) * bytesPerPixel
	if uint64(size) != expected {
		return nil, fmt.Errorf("LBMP declares %d bytes for %dx%d at %d bpp, want %d", size, width, height, depth, expected)
	}
	body := encoded[lbmpHeaderSize:]
	if uint64(len(body)) < expected {
		return nil, fmt.Errorf("LBMP holds %d pixel bytes, want %d", len(body), expected)
	}

	image := stdimage.NewNRGBA(stdimage.Rect(0, 0, int(width), int(height)))
	for y := 0; y < int(height); y++ {
		for x := 0; x < int(width); x++ {
			index := (y*int(width) + x) * int(bytesPerPixel)
			var pixel color.NRGBA
			if bytesPerPixel == 1 {
				pixel = rgb332(body[index])
			} else {
				pixel = rgb565(binary.LittleEndian.Uint16(body[index:]))
			}
			image.SetNRGBA(x, y, pixel)
		}
	}
	return image, nil
}

// rgb332 expands one byte of rrrgggbb. Each channel is scaled so its widest
// value reaches 255 rather than being shifted into place and left short — a
// shift alone makes white come out as 0xe0e0c0 and tints every image.
func rgb332(value byte) color.NRGBA {
	red := uint32(value >> 5)
	green := uint32(value >> 2 & 0x7)
	blue := uint32(value & 0x3)
	return color.NRGBA{
		R: uint8(red * 255 / 7),
		G: uint8(green * 255 / 7),
		B: uint8(blue * 255 / 3),
		A: 0xff,
	}
}

// rgb565 expands one halfword of rrrrrggggggbbbbb, the LCD's own pixel.
func rgb565(value uint16) color.NRGBA {
	red := uint32(value >> 11)
	green := uint32(value >> 5 & 0x3f)
	blue := uint32(value & 0x1f)
	return color.NRGBA{
		R: uint8(red * 255 / 31),
		G: uint8(green * 255 / 63),
		B: uint8(blue * 255 / 31),
		A: 0xff,
	}
}
