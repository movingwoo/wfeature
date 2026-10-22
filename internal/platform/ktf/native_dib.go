package ktf

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
)

// A later module's image is BREW's IDIB, the bitmap its own pixel code works
// on: a table, a palette map, a pointer to the top row of pixels, a palette,
// the pixel value that is not drawn, the width and height, the pitch, the
// palette's size, the bits per pixel, the colour scheme and six reserved
// bytes.
//
// One later title works on it directly. At start-up it follows the pointer at
// +8 of an image the factory returned to the pixels in the same block, takes
// the bytes per pixel from the first one, and later divides by what it
// measured. Then it copies that header into blocks of its own, fills their
// pixels itself — a 256 by 256 picture its race is drawn from — and blits
// them, and it draws into the pixels of an image the factory built. So the
// image is laid out the way it reads it, and the blit reads any IDIB's pixels
// from guest memory. The 2005 module reads its image's first word as the
// bitmap instead, and keeps the object it had.
const (
	nativeDIBHeaderSize = 0x24
	// nativeClassDIB is BREW's AEECLSID_DIB. Its trapped table is the one
	// every image here points at, which is also how the blit tells an IDIB a
	// title built from anything else it is handed.
	nativeClassDIB = 0x1001045
	// The pixels are 5-6-5 — the handsets of the time had 65,536 colours — in
	// rows from the top, each the width times two bytes.
	nativeDIBDepth     = 16
	nativeDIBScheme565 = 16
	// nativeDIBTransparent is the magenta the blit leaves out, in 5-6-5.
	nativeDIBTransparent = 0xf81f
	// nativeDIBMaxSide bounds a side of an IDIB a title built, which is read
	// out of guest memory.
	nativeDIBMaxSide = 4096
)

// BREW's raster operations that leave the transparent pixel behind: its
// transparent copy, and the one version 1.0 used for the same.
const (
	nativeRasterOldMask     = 4
	nativeRasterTransparent = 7
)

// dibObject lays a decoded picture out as an IDIB, header and pixels in one
// block.
func (platform *NativePlatform) dibObject(decoded *image.RGBA) (uint32, error) {
	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	pitch := width * 2
	if pitch > math.MaxInt16 || height > math.MaxUint16 {
		return 0, fmt.Errorf("KTF native image of %dx%d is too large for a native bitmap", width, height)
	}
	size := uint64(nativeDIBHeaderSize) + uint64(pitch)*uint64(height)
	if size > maxPlatformAllocation {
		return 0, fmt.Errorf("KTF native image of %dx%d needs %d bytes, limit %d", width, height, size, maxPlatformAllocation)
	}
	object, err := platform.client.InterfaceObject(nativeClassDIB)
	if err != nil {
		return 0, err
	}
	table, err := platform.client.ReadWord(object)
	if err != nil {
		return 0, err
	}
	block, err := platform.client.Allocate(uint32(size))
	if err != nil {
		return 0, err
	}
	data := make([]byte, size)
	binary.LittleEndian.PutUint32(data[0x00:], table)
	binary.LittleEndian.PutUint32(data[0x08:], block+nativeDIBHeaderSize)
	binary.LittleEndian.PutUint32(data[0x10:], nativeDIBTransparent)
	binary.LittleEndian.PutUint16(data[0x14:], uint16(width))
	binary.LittleEndian.PutUint16(data[0x16:], uint16(height))
	binary.LittleEndian.PutUint16(data[0x18:], uint16(pitch))
	data[0x1c] = nativeDIBDepth
	data[0x1d] = nativeDIBScheme565
	for y := 0; y < height; y++ {
		row := data[nativeDIBHeaderSize+y*pitch:]
		for x := 0; x < width; x++ {
			binary.LittleEndian.PutUint16(row[x*2:], encode565(decoded.RGBAAt(x, y)))
		}
	}
	if err := platform.client.core.Memory().Write(block, data); err != nil {
		return 0, fmt.Errorf("write KTF native bitmap at %#x: %w", block, err)
	}
	return block, nil
}

// nativeDIBView is what the blit reads out of an IDIB's header.
type nativeDIBView struct {
	pixels        uint32
	width, height int
	pitch         int
	transparent   uint16
}

// dibAt reads an object as an IDIB, and reports false for anything that is not
// one this platform can draw: an object whose table is not the one its images
// carry, or a header that does not describe 5-6-5 pixels of a bounded size.
func (platform *NativePlatform) dibAt(object uint32) (nativeDIBView, bool) {
	holder, ok := platform.client.interfaces[nativeClassDIB]
	if !ok || object == 0 {
		return nativeDIBView{}, false
	}
	header := make([]byte, nativeDIBHeaderSize)
	if err := platform.client.core.Memory().Read(object, header); err != nil {
		return nativeDIBView{}, false
	}
	table, err := platform.client.ReadWord(holder)
	if err != nil || binary.LittleEndian.Uint32(header) != table {
		return nativeDIBView{}, false
	}
	view := nativeDIBView{
		pixels:      binary.LittleEndian.Uint32(header[0x08:]),
		width:       int(binary.LittleEndian.Uint16(header[0x14:])),
		height:      int(binary.LittleEndian.Uint16(header[0x16:])),
		pitch:       int(int16(binary.LittleEndian.Uint16(header[0x18:]))),
		transparent: uint16(binary.LittleEndian.Uint32(header[0x10:])),
	}
	palette, depth, scheme := binary.LittleEndian.Uint16(header[0x1a:]), header[0x1c], header[0x1d]
	if depth != nativeDIBDepth || palette != 0 || scheme != nativeDIBScheme565 && scheme != 0 ||
		view.width <= 0 || view.height <= 0 || view.width > nativeDIBMaxSide || view.height > nativeDIBMaxSide ||
		abs(view.pitch) < view.width*2 {
		return nativeDIBView{}, false
	}
	return view, true
}

// drawDIB copies one region of an IDIB's pixels onto the frame, clipped to
// both. own is the picture the factory decoded when the IDIB is one of its
// images: a pixel the title has not changed is drawn from that picture and
// left out when it is the transparent colour, exactly as the decoded image
// draws, so an image nobody draws into looks the same either way. A pixel the
// title wrote is read as 5-6-5 and left out only by a transparent copy, when it
// holds the IDIB's transparent value.
func (platform *NativePlatform) drawDIB(view nativeDIBView, blit NativeBlit, own *nativeImage) bool {
	frame := platform.screen.frame
	rows := min(blit.Height, min(view.height-blit.SourceY, frame.Bounds().Max.Y-blit.Y))
	columns := min(blit.Width, min(view.width-blit.SourceX, frame.Bounds().Max.X-blit.X))
	firstRow := max(0, -blit.SourceY, -blit.Y)
	firstColumn := max(0, -blit.SourceX, -blit.X)
	if rows <= firstRow || columns <= firstColumn {
		return true
	}
	if own != nil && own.frame.Bounds() != image.Rect(0, 0, view.width, view.height) {
		own = nil
	}
	transparentCopy := blit.Mode == nativeRasterTransparent || blit.Mode == nativeRasterOldMask
	span := make([]byte, (columns-firstColumn)*2)
	memory := platform.client.core.Memory()
	for row := firstRow; row < rows; row++ {
		sourceY := blit.SourceY + row
		address := int64(view.pixels) + int64(sourceY)*int64(view.pitch) + int64(blit.SourceX+firstColumn)*2
		if address < 0 || address+int64(len(span)) > math.MaxUint32 {
			return false
		}
		if err := memory.Read(uint32(address), span); err != nil {
			return false
		}
		for column := firstColumn; column < columns; column++ {
			value := binary.LittleEndian.Uint16(span[(column-firstColumn)*2:])
			sourceX := blit.SourceX + column
			if own != nil {
				if pixel := own.frame.RGBAAt(sourceX, sourceY); encode565(pixel) == value {
					if pixel != nativeTransparent {
						frame.SetRGBA(blit.X+column, blit.Y+row, pixel)
					}
					continue
				}
			}
			if transparentCopy && value == view.transparent {
				continue
			}
			frame.SetRGBA(blit.X+column, blit.Y+row, decode565(value))
		}
	}
	return true
}

func encode565(pixel color.RGBA) uint16 {
	return uint16(pixel.R>>3)<<11 | uint16(pixel.G>>2)<<5 | uint16(pixel.B>>3)
}

func decode565(value uint16) color.RGBA {
	red, green, blue := uint8(value>>11&0x1f), uint8(value>>5&0x3f), uint8(value&0x1f)
	return color.RGBA{R: red<<3 | red>>2, G: green<<2 | green>>4, B: blue<<3 | blue>>2, A: 0xff}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
