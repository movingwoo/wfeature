package ktf

import (
	"bytes"
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
//
// **The platform keeps the bitmap, not the title.** The title builds one, hands
// it to the factory and frees it on the next call — so an object left pointing
// at what it was handed points into the arena's next tenant, and the picture a
// later blit reads is whatever that tenant wrote. `data` is this platform's own
// copy, and it is the copy the object's first word names, because the title
// reads that word and indexes the bitmap through it: what it draws afterwards
// lands in the copy and a blit sees it.
type nativeImage struct {
	// data is the platform's copy of the bitmap, in the guest's own address
	// space so the title can write into it.
	data uint32
	// length is how much of it there is.
	length uint32
	// bytes is the copy the decode below was made from, and what says whether
	// the title has drawn into the bitmap since.
	bytes []byte
	frame *image.RGBA
}

// nativeBitmapLength reports how long a bitmap the title built is, from its
// own header. The file header names it, and the pixels bound it: a length that
// covers neither is not one this platform will copy.
func nativeBitmapLength(header []byte) (uint32, bool) {
	if len(header) < bitmapFileHeaderSize+0x28 || header[0] != 'B' || header[1] != 'M' {
		return 0, false
	}
	named := binary.LittleEndian.Uint32(header[2:])
	pixels := binary.LittleEndian.Uint32(header[bitmapPixelOffsetField:])
	width := int(int32(binary.LittleEndian.Uint32(header[bitmapWidthField:])))
	height := int(int32(binary.LittleEndian.Uint32(header[bitmapHeightField:])))
	depth := int(binary.LittleEndian.Uint16(header[bitmapBitsPerPixelField:]))
	if height < 0 {
		height = -height
	}
	if width <= 0 || height <= 0 || depth <= 0 || width > maxNativeBitmap || height > maxNativeBitmap {
		return 0, false
	}
	stride := (width*depth + 31) / 32 * 4
	needed := uint64(pixels) + uint64(stride)*uint64(height)
	if uint64(named) > needed {
		needed = uint64(named)
	}
	if needed == 0 || needed > maxNativeBitmap {
		return 0, false
	}
	return uint32(needed), true
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
	memory := platform.client.core.Memory()
	header := make([]byte, bitmapFileHeaderSize+0x28)
	if err := memory.Read(data, header); err != nil {
		return 0, fmt.Errorf("read KTF native bitmap header at %#x: %w", data, err)
	}
	length, ok := nativeBitmapLength(header)
	if !ok {
		return 0, fmt.Errorf("KTF native image at %#x is not a bitmap this platform can keep", data)
	}
	kept, err := platform.client.Allocate(length)
	if err != nil {
		return 0, err
	}
	bytes := make([]byte, length)
	if err := memory.Read(data, bytes); err != nil {
		return 0, fmt.Errorf("read the %d bytes of the KTF native bitmap at %#x: %w", length, data, err)
	}
	if err := memory.Write(kept, bytes); err != nil {
		return 0, fmt.Errorf("keep the KTF native bitmap at %#x: %w", kept, err)
	}
	decoded, err := platform.decodeBitmap(kept)
	if err != nil {
		return 0, err
	}
	object, err := platform.client.Allocate(8)
	if err != nil {
		return 0, err
	}
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, kept)
	if err := memory.Write(object, word); err != nil {
		return 0, fmt.Errorf("write KTF native image object at %#x: %w", object, err)
	}
	platform.images[object] = &nativeImage{data: kept, length: length, bytes: bytes, frame: decoded}
	return object, nil
}

// refresh re-reads an image whose bitmap the title has drawn into since it was
// decoded, and reports whether it could be read at all.
//
// The comparison is the whole bitmap rather than a mark the title sets, because
// there is no mark: the title writes pixels through the word this platform put
// in its object and tells nobody. Reading and comparing is what a decode would
// have to do anyway, and it is the cheap half of it.
func (platform *NativePlatform) refresh(source *nativeImage) error {
	if source.length == 0 {
		return nil
	}
	current := make([]byte, source.length)
	if err := platform.client.core.Memory().Read(source.data, current); err != nil {
		return fmt.Errorf("read the KTF native bitmap at %#x: %w", source.data, err)
	}
	if bytes.Equal(current, source.bytes) {
		return nil
	}
	decoded, err := platform.decodeBitmap(source.data)
	if err != nil {
		return err
	}
	source.bytes, source.frame = current, decoded
	return nil
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
