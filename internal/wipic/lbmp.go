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
// written down here comes from the files themselves. The first evidence was
// five files in one KTF title, and they settled the deep formats: every one is
// exactly 24 + width * height * (type / 8) bytes long, which fixes `size` as
// the payload length and `type` as the bit depth in one arithmetic.
//
// The colour reading is settled by the pixel values rather than assumed. In
// the darkest of the five, eleven distinct bytes cover the whole image, and
// read as RGB332 they are a single clean ramp — 0x00, 0x20, 0x24, 0x44, 0x48,
// 0x68, 0x8c, 0xb0, 0xb1, 0xd5, 0xf9, which is red a little above green with
// blue at zero throughout. That is a gold-on-black gradient, and the image is
// a gold dragon on black; the file beside it decodes as green foliage. A
// palette-indexed reading would have to explain why the indices are scattered
// across the whole byte range instead of counting from zero, and it cannot.
//
// The SKT archives added two more halves of the format, on a corpus of about
// nine hundred files rather than five:
//
//   - **The sixth header word is a mask flag**, and where it is 1 a one-bit
//     transparency mask follows the pixels. Its length is width * ceil(height/8)
//     in every file that has one, and that arithmetic is what identified it:
//     across the corpus the payload is exactly the pixels plus that, and a
//     sprite decoded without it is a rectangle of background with the sprite
//     in the middle. **A set bit is transparent**, which the files say twice
//     over: every pixel a set bit covers is the same magenta, and read the
//     other way a sprite is that magenta with its artwork punched out of it.
//   - **A depth below 8 is stored as bit planes**, not as packed pixels, and
//     each plane is the LCD's own page layout: byte (y/8)*width + x, one row of
//     pixels per bit, the top row in bit 0. `size` is then one plane rather
//     than the whole payload, which is what says the planes are the unit. A
//     two-bit image is two of them and its mask is a third.
type lbmpHeader struct {
	depth  uint32
	width  uint32
	height uint32
	// size is the length of one plane: the whole pixel payload for a packed
	// depth, and one bit plane for a planar one.
	size uint32
	// mask is the sixth word: 1 when a transparency plane follows the pixels.
	mask uint32
}

const (
	lbmpHeaderSize = 24
	// lbmpMagic is the first four bytes, which are also what tells this format
	// from every other encoding a title ships.
	lbmpMagic = "LBMP"

	// The packed depths, as bits per pixel.
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
// A masked pixel comes back fully transparent rather than as a colour key, so
// a caller that already blends alpha needs nothing new to draw one.
func DecodeLBMP(encoded []byte) (stdimage.Image, error) {
	if !IsLBMP(encoded) {
		return nil, fmt.Errorf("not an LBMP image")
	}
	header := lbmpHeader{
		depth:  binary.LittleEndian.Uint32(encoded[4:]),
		width:  binary.LittleEndian.Uint32(encoded[8:]),
		height: binary.LittleEndian.Uint32(encoded[12:]),
		size:   binary.LittleEndian.Uint32(encoded[16:]),
		mask:   binary.LittleEndian.Uint32(encoded[20:]),
	}
	if header.width == 0 || header.height == 0 ||
		uint64(header.width)*uint64(header.height) > maxLBMPPixels {
		return nil, fmt.Errorf("LBMP dimensions %dx%d are out of range", header.width, header.height)
	}

	planeBytes := uint64(header.width) * uint64((header.height+7)/8)
	planar := header.depth < lbmpDepthRGB332
	var pixelPlane, planes uint64
	switch {
	case planar && (header.depth == 1 || header.depth == 2 || header.depth == 4):
		pixelPlane, planes = planeBytes, uint64(header.depth)
	case header.depth == lbmpDepthRGB332:
		pixelPlane, planes = uint64(header.width)*uint64(header.height), 1
	case header.depth == lbmpDepthRGB565:
		pixelPlane, planes = uint64(header.width)*uint64(header.height)*2, 1
	default:
		return nil, fmt.Errorf("LBMP depth %d is not supported", header.depth)
	}
	if uint64(header.size) != pixelPlane {
		return nil, fmt.Errorf("LBMP declares a %d byte plane for %dx%d at %d bpp, want %d",
			header.size, header.width, header.height, header.depth, pixelPlane)
	}

	body := encoded[lbmpHeaderSize:]
	pixels := pixelPlane * planes
	if uint64(len(body)) < pixels {
		return nil, fmt.Errorf("LBMP holds %d pixel bytes, want %d", len(body), pixels)
	}
	// A file may carry more than it declares — a sprite sheet's later frames
	// sit past the first image in a good half of the local corpus — so the
	// mask is only there when the flag says so *and* the bytes are there.
	var maskPlane []byte
	if header.mask != 0 && uint64(len(body)) >= pixels+planeBytes {
		maskPlane = body[pixels : pixels+planeBytes]
	}

	image := stdimage.NewNRGBA(stdimage.Rect(0, 0, int(header.width), int(header.height)))
	for y := 0; y < int(header.height); y++ {
		for x := 0; x < int(header.width); x++ {
			var pixel color.NRGBA
			switch {
			case planar:
				pixel = lbmpPlanarPixel(body, header, planeBytes, x, y)
			case header.depth == lbmpDepthRGB332:
				pixel = rgb332(body[y*int(header.width)+x])
			default:
				pixel = rgb565(binary.LittleEndian.Uint16(body[(y*int(header.width)+x)*2:]))
			}
			if maskPlane != nil && lbmpPlaneBit(maskPlane, int(header.width), x, y) {
				pixel.A = 0
			}
			image.SetNRGBA(x, y, pixel)
		}
	}
	return image, nil
}

// lbmpPlanarPixel reads one pixel out of the bit planes as a grey level. **The
// ramp runs the other way from the packed depths**: zero is white and the
// widest value is black.
//
// One title settles that, because it ships the same glyph sheets twice. Its
// white font and its black font are the same shapes with the same mask, and
// the only difference is the value under the mask — zero in the white one and
// three in the black one. The white font also has one sheet at eight bits a
// pixel rather than two, and there the pixels are 0xfe and 0xff: white. So
// zero is what the white font is made of, and reading the value as ink rather
// than as light is what makes a black-on-black screen into text.
//
// What the levels between mean is not settled: every planar file in the corpus
// uses the two ends of its range and nothing in between. The plane order is
// undetermined for the same reason — the planes of every local two-bit image
// are identical.
func lbmpPlanarPixel(body []byte, header lbmpHeader, planeBytes uint64, x, y int) color.NRGBA {
	value := 0
	for plane := 0; plane < int(header.depth); plane++ {
		start := uint64(plane) * planeBytes
		if lbmpPlaneBit(body[start:start+planeBytes], int(header.width), x, y) {
			value |= 1 << plane
		}
	}
	levels := (1 << header.depth) - 1
	grey := uint8(255 - value*255/levels)
	return color.NRGBA{R: grey, G: grey, B: grey, A: 0xff}
}

// lbmpPlaneBit reads one bit out of a plane in the LCD's page layout: the
// bytes run left to right along a band of eight rows, and within a byte the
// topmost row of the band is bit 0.
func lbmpPlaneBit(plane []byte, width, x, y int) bool {
	index := (y/8)*width + x
	if index < 0 || index >= len(plane) {
		return false
	}
	return plane[index]>>(y%8)&1 == 1
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
