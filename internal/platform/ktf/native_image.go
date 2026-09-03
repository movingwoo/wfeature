package ktf

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The title does not hand the screen pixels. It hands it an object, and that
// object comes from platform slot 25, which takes a number and builds
// something: `create(0x1004001, data, out, out)`. The number is from the same
// family as the interfaces the module queries by number, so the slot is a
// factory and 0x1004001 is a class.
//
// What `data` points at settles what the class is. Dumping it at the call:
//
//	42 4d 18 0f 00 00 ... 36 04 00 00 28 00 00 00 36 00 00 00 32 00 00 00
//	01 00 08 00 00 00 00 00 ...
//
// "BM", a file size, pixels at 0x436, a 40-byte information header, 54 by 50,
// one plane, 8 bits, no compression — a **Windows bitmap**, built in memory by
// the title out of its own archive. The 0x400 between the two headers is its
// 256-entry palette. So the title's own graphics format is decoded by the
// title, and what it asks the platform for is the one format every handset
// already had a decoder for.
const (
	// nativeSlotCreateObject builds an object of the class named in r0.
	nativeSlotCreateObject = 0x64
	// nativeClassImage is the only class the local title asks for.
	nativeClassImage = 0x1004001
)

// Bitmap header offsets, in the order the format defines them.
const (
	bitmapFileHeaderSize    = 14
	bitmapPixelOffsetField  = 0x0a
	bitmapInformationSize   = 0x0e
	bitmapWidthField        = 0x12
	bitmapHeightField       = 0x16
	bitmapBitsPerPixelField = 0x1c
	bitmapCompressionField  = 0x1e
	bitmapPaletteSizeField  = 0x2e
)

// maxNativeBitmap bounds a bitmap read out of guest memory. The largest the
// local title builds is a screen's worth at a byte a pixel; this leaves room
// for one at four.
const maxNativeBitmap = 4 << 20

// nativeImage is one image the title built and the platform decoded.
type nativeImage struct {
	// data is the guest bitmap the title built. The object's first word points
	// at it because the title reads that word itself on one path and indexes
	// the bitmap through it.
	data  uint32
	frame *image.RGBA
}

// createObject answers the platform table's factory.
func (platform *NativePlatform) createObject(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 2)
	if err != nil {
		return 0, err
	}
	class, data := arguments[0], arguments[1]
	if class != nativeClassImage {
		// A class this platform does not build is a refusal rather than a
		// failure: the title checks what it gets back, and a run that stops
		// here would report the factory instead of what wanted the object.
		return 0, nil
	}
	decoded, err := platform.decodeBitmap(data)
	if err != nil {
		return 0, err
	}
	object, err := platform.client.Allocate(8)
	if err != nil {
		return 0, err
	}
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, data)
	if err := platform.client.core.Memory().Write(object, word); err != nil {
		return 0, fmt.Errorf("write KTF native image object at %#x: %w", object, err)
	}
	platform.images[object] = &nativeImage{data: data, frame: decoded}
	return object, nil
}

// decodeBitmap reads one bitmap out of guest memory.
//
// Only what the title builds is decoded: an uncompressed bottom-up bitmap at
// eight bits with a palette, or at twenty-four. Anything else is refused by
// name rather than drawn wrongly, because a wrong decode reads as a corrupt
// screen and a refusal reads as itself.
func (platform *NativePlatform) decodeBitmap(address uint32) (*image.RGBA, error) {
	header := make([]byte, bitmapFileHeaderSize+0x28)
	memory := platform.client.core.Memory()
	if err := memory.Read(address, header); err != nil {
		return nil, fmt.Errorf("read KTF native bitmap header at %#x: %w", address, err)
	}
	if header[0] != 'B' || header[1] != 'M' {
		return nil, fmt.Errorf("KTF native image at %#x does not start with a bitmap header", address)
	}
	pixelOffset := binary.LittleEndian.Uint32(header[bitmapPixelOffsetField:])
	width := int(int32(binary.LittleEndian.Uint32(header[bitmapWidthField:])))
	height := int(int32(binary.LittleEndian.Uint32(header[bitmapHeightField:])))
	depth := int(binary.LittleEndian.Uint16(header[bitmapBitsPerPixelField:]))
	compression := binary.LittleEndian.Uint32(header[bitmapCompressionField:])
	if compression != 0 {
		return nil, fmt.Errorf("KTF native bitmap at %#x uses compression %d", address, compression)
	}
	if depth != 4 && depth != 8 && depth != 24 {
		return nil, fmt.Errorf("KTF native bitmap at %#x is %d bits per pixel", address, depth)
	}
	// A negative height is a top-down bitmap.
	topDown := height < 0
	if topDown {
		height = -height
	}
	if width <= 0 || height <= 0 || width > maxNativeBitmap || height > maxNativeBitmap {
		return nil, fmt.Errorf("KTF native bitmap at %#x is %dx%d", address, width, height)
	}
	// A row is padded to a whole number of words, and at four bits a pixel an
	// odd width still occupies a whole byte — so the padding is computed from
	// the bits rather than from a byte count that has already lost the odd
	// pixel.
	stride := (width*depth + 31) / 32 * 4
	if uint64(stride)*uint64(height) > maxNativeBitmap {
		return nil, fmt.Errorf("KTF native bitmap at %#x needs %d bytes", address, stride*height)
	}

	palette := make([]color.RGBA, 0, 256)
	if depth == 4 || depth == 8 {
		count := int(binary.LittleEndian.Uint32(header[bitmapPaletteSizeField:]))
		if count == 0 {
			count = 1 << depth
		}
		if count > 1<<depth {
			return nil, fmt.Errorf("KTF native bitmap at %#x names %d palette entries", address, count)
		}
		entries := make([]byte, count*4)
		if err := memory.Read(address+bitmapFileHeaderSize+binary.LittleEndian.Uint32(header[bitmapInformationSize:]), entries); err != nil {
			return nil, fmt.Errorf("read KTF native bitmap palette at %#x: %w", address, err)
		}
		// A palette entry is blue, green, red, and a byte the format does not
		// define — not an alpha, so it is dropped rather than believed.
		for index := 0; index < count; index++ {
			palette = append(palette, color.RGBA{
				B: entries[index*4],
				G: entries[index*4+1],
				R: entries[index*4+2],
				A: 0xff,
			})
		}
	}

	rows := make([]byte, stride*height)
	if err := memory.Read(address+pixelOffset, rows); err != nil {
		return nil, fmt.Errorf("read KTF native bitmap pixels at %#x: %w", address, err)
	}
	decoded := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		source := y
		if !topDown {
			source = height - 1 - y
		}
		row := rows[source*stride:]
		for x := 0; x < width; x++ {
			var pixel color.RGBA
			switch depth {
			case 4, 8:
				index := 0
				if depth == 8 {
					index = int(row[x])
				} else {
					// Two pixels to a byte, the left one in the high nibble.
					index = int(row[x/2] >> 4)
					if x%2 == 1 {
						index = int(row[x/2] & 0xf)
					}
				}
				if index >= len(palette) {
					return nil, fmt.Errorf("KTF native bitmap at %#x indexes palette entry %d", address, index)
				}
				pixel = palette[index]
			case 24:
				pixel = color.RGBA{B: row[x*3], G: row[x*3+1], R: row[x*3+2], A: 0xff}
			}
			decoded.SetRGBA(x, y, pixel)
		}
	}
	return decoded, nil
}
