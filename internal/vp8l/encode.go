// Package vp8l writes pictures as lossless WebP, the VP8L bitstream of RFC
// 9649 in its RIFF container, for the pictures a session sends its page.
//
// It is written for those pictures rather than for photographs: a handset
// game's frame, or the part of one that changed, is mostly a few hundred
// colours at most, with transparent pixels where nothing changed. Such a
// picture is written through a colour table, packing up to eight pixels in
// one where the colours are few, and everything else with green subtracted
// from red and blue; the predictor transform is kept for pictures that stay
// large without it, like photographs. Both then go through backward
// references, which copy from the rows above as readily as from the left,
// chosen for the bits they save, and a colour cache sized by estimate. A
// picture has a single group of prefix codes.
//
// Nothing here is taken from another implementation: the tables are the
// format's own, from the RFC.
package vp8l

import (
	"encoding/binary"
	"errors"
	"slices"
)

// MaxDimension is the largest width or height the format can describe.
const MaxDimension = 1 << 14

// settings are the choices that trade encoding time for size.
type settings struct {
	// chains are the hash chains backward references are searched with.
	chains     chains
	colorCache bool
	// predictAbove is the size, in bits a pixel, over which a picture with
	// too many colours for a table is also tried through the predictor.
	predictAbove int
	// predictorBits is the log2 of a predictor block's side.
	predictorBits int
}

// The chains were chosen on real play: pairs of pixels find the short copies
// a busy full-colour scene is made of, and runs of six reach the repeated
// tiles of a field that lie a screen or more back.
var defaultSettings = settings{
	chains:        chains{{span: 2, depth: 8}, {span: 6, depth: 16}},
	colorCache:    true,
	predictAbove:  6,
	predictorBits: 4,
}

// Encoder writes pictures. It keeps its working memory between pictures, so
// one encoder serves one goroutine, and that memory grows to what the largest
// picture it has written needed.
type Encoder struct {
	image     imageWriter
	palette   palette
	predictor predictor
	// options replaces the default settings; tests set it.
	options *settings
	work    []uint32
	modes   []uint32
	// bits is the stream being written, and predicted the one written
	// through the predictor when that is tried as well.
	bits, predicted bitWriter
}

// Encode appends to dst a lossless WebP file holding the picture: width by
// height pixels, row by row, each 0xAARRGGBB. A fully transparent pixel's
// colour is not kept, because nothing that draws the picture can show it;
// it is chosen to cost as little as possible instead.
func (e *Encoder) Encode(dst []byte, pixels []uint32, width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 || width > MaxDimension || height > MaxDimension {
		return dst, errors.New("vp8l: picture dimensions out of range")
	}
	if len(pixels) < width*height {
		return dst, errors.New("vp8l: too few pixels for the dimensions")
	}
	pixels = pixels[:width*height]
	e.image.settings = defaultSettings
	if e.options != nil {
		e.image.settings = *e.options
	}
	// The colours are collected in the same pass that finds whether any
	// pixel is less than opaque, which the header has to say first.
	indexed, translucent := e.palette.collect(pixels)
	alpha := uint32(0)
	if translucent {
		alpha = 1
	}
	w := e.begin(&e.bits, width, height, alpha)
	switch {
	case indexed:
		e.writeIndexed(w, width, height)
	default:
		// The games draw with few colours in flat areas and repeated tiles,
		// which copies from above and to the left capture far better than
		// the differences a predictor leaves; a picture that is still large
		// that way, like a photograph, is tried through the predictor too.
		e.writeDirect(w, pixels, width)
		if size := len(w.finish()); size*8 > len(pixels)*e.image.settings.predictAbove {
			predicted := e.begin(&e.predicted, width, height, alpha)
			e.writePredicted(predicted, pixels, width, height)
			if len(predicted.finish()) < size {
				w = predicted
			}
		}
	}
	stream := w.finish()
	size := len(stream)
	padded := size + size&1
	start := len(dst)
	dst = slices.Grow(dst, 20+padded)[:start+20]
	copy(dst[start:], "RIFF")
	binary.LittleEndian.PutUint32(dst[start+4:], uint32(12+padded))
	copy(dst[start+8:], "WEBPVP8L")
	binary.LittleEndian.PutUint32(dst[start+16:], uint32(size))
	dst = append(dst, stream...)
	if size&1 != 0 {
		dst = append(dst, 0)
	}
	return dst, nil
}

// begin starts a stream with the image header. The header is forty bits, so
// what follows it starts on a byte.
func (e *Encoder) begin(w *bitWriter, width, height int, alpha uint32) *bitWriter {
	w.out, w.value, w.count = w.out[:0], 0, 0
	w.write(0x2f, 8)
	w.write(uint32(width-1), 14)
	w.write(uint32(height-1), 14)
	w.write(alpha, 1)
	w.write(0, 3)
	return w
}

// writeIndexed writes the picture through its colour table, which the
// palette already holds together with every pixel's place in it.
func (e *Encoder) writeIndexed(w *bitWriter, width, height int) {
	colors := e.palette.colors
	w.write(1, 1)
	w.write(3, 2)
	w.write(uint32(len(colors)-1), 8)
	// The table is written as the differences between neighbouring entries,
	// which the order of the table keeps small.
	e.work = resize(e.work, len(colors))
	previous := uint32(0)
	for index, color := range colors {
		e.work[index] = subtractPixels(color, previous)
		previous = color
	}
	e.image.write(w, e.work, len(colors), false)

	// Up to eight indexes share a pixel's green, as few bits each as the
	// table needs.
	packing := 0
	switch {
	case len(colors) <= 2:
		packing = 3
	case len(colors) <= 4:
		packing = 2
	case len(colors) <= 16:
		packing = 1
	}
	packedWidth := (width + 1<<packing - 1) >> packing
	e.work = resize(e.work, packedWidth*height)
	e.palette.packIndexes(e.work, width, height, packing)
	w.write(0, 1)
	e.image.write(w, e.work, packedWidth, true)
}

// subtractGreen copies pixels into e.work with green subtracted from red and
// blue, which leaves less to code where the channels move together, and
// writes the transform.
func (e *Encoder) subtractGreen(w *bitWriter, pixels []uint32) {
	e.work = resize(e.work, len(pixels))
	for index, pixel := range pixels {
		pixel = visible(pixel)
		green := pixel >> 8 & 0xff
		red, blue := (pixel>>16-green)&0xff, (pixel-green)&0xff
		e.work[index] = pixel&0xff00ff00 | red<<16 | blue
	}
	w.write(1, 1)
	w.write(2, 2)
}

// writeDirect writes a picture with too many colours for a table as its
// pixels, green subtracted.
func (e *Encoder) writeDirect(w *bitWriter, pixels []uint32, width int) {
	e.subtractGreen(w, pixels)
	w.write(0, 1)
	e.image.write(w, e.work, width, true)
}

// writePredicted writes a picture with too many colours for a table with
// green subtracted and each block of pixels as its difference from the
// prediction that suits the block best.
func (e *Encoder) writePredicted(w *bitWriter, pixels []uint32, width, height int) {
	e.subtractGreen(w, pixels)
	blockBits := e.image.settings.predictorBits
	blocksWide := (width + 1<<blockBits - 1) >> blockBits
	blocksHigh := (height + 1<<blockBits - 1) >> blockBits
	e.modes = resize(e.modes, blocksWide*blocksHigh)
	residuals := e.predictor.predict(e.work, width, height, blockBits, e.modes)
	w.write(1, 1)
	w.write(0, 2)
	w.write(uint32(blockBits-2), 3)
	e.image.write(w, e.modes, blocksWide, false)
	w.write(0, 1)
	e.image.write(w, residuals, width, true)
}

// visible answers the pixel with the colour of a fully transparent one
// dropped, so that every such pixel is one colour.
func visible(pixel uint32) uint32 {
	if pixel>>24 == 0 {
		return 0
	}
	return pixel
}

// subtractPixels subtracts b from a in each of the four channels separately.
func subtractPixels(a, b uint32) uint32 {
	alphaGreen := 0x00ff00ff + a&0xff00ff00 - b&0xff00ff00
	redBlue := 0xff00ff00 + a&0x00ff00ff - b&0x00ff00ff
	return alphaGreen&0xff00ff00 | redBlue&0x00ff00ff
}

// palette finds a picture's colours when it has at most 256.
type palette struct {
	colors []uint32
	// found holds every pixel's colour by the order it was found in, and
	// ranks turns that into the colour's place in the sorted table, so that
	// the pixels are looked up in slots only once.
	found []uint8
	ranks [256]uint8
	// sorting holds each colour with its number, to sort them together.
	sorting []uint64
	// slots is an open addressing table of the colours seen, each a colour
	// and its number plus one in the upper word; zero marks an empty slot.
	slots [1024]uint64
}

func paletteSlot(color uint32) uint32 {
	return (color * 0x9e3779b1) >> 22
}

// collect reports whether pixels have at most 256 colours and, when they
// do, holds them sorted, which keeps the differences the table is written
// as small, and every pixel's place among them. It also reports whether any
// pixel is less than opaque, which it finds on the way.
func (p *palette) collect(pixels []uint32) (fits, translucent bool) {
	p.colors = p.colors[:0]
	clear(p.slots[:])
	p.found = resize(p.found, len(pixels))
	found := p.found
	// opaque keeps the bits every pixel has; its alpha stays 0xff only if
	// every pixel is opaque. A transparent pixel counts as black, which
	// leaves that answer alone.
	opaque := uint32(0xffffffff)
	last, number := ^visible(pixels[0]), uint8(0)
	for position, pixel := range pixels {
		pixel = visible(pixel)
		if pixel != last {
			opaque &= pixel
			slot := paletteSlot(pixel)
			for {
				entry := p.slots[slot]
				if entry == 0 {
					if len(p.colors) == 256 {
						return false, translucentAfter(pixels[position:], opaque)
					}
					number = uint8(len(p.colors))
					p.colors = append(p.colors, pixel)
					p.slots[slot] = uint64(pixel) | (uint64(number)+1)<<32
					break
				}
				if uint32(entry) == pixel {
					number = uint8(entry>>32 - 1)
					break
				}
				slot = (slot + 1) & (uint32(len(p.slots)) - 1)
			}
			last = pixel
		}
		found[position] = number
	}
	p.sorting = resize(p.sorting, len(p.colors))
	for number, color := range p.colors {
		p.sorting[number] = uint64(color)<<8 | uint64(number)
	}
	slices.Sort(p.sorting)
	for index, entry := range p.sorting {
		p.colors[index] = uint32(entry >> 8)
		p.ranks[uint8(entry)] = uint8(index)
	}
	return true, opaque>>24 != 0xff
}

// translucentAfter finishes what collect found out about opacity when it
// stopped counting colours: opaque holds the bits of every pixel before
// pixels.
func translucentAfter(pixels []uint32, opaque uint32) bool {
	if opaque>>24 != 0xff {
		return true
	}
	for _, pixel := range pixels {
		if pixel>>24 != 0xff {
			return true
		}
	}
	return false
}

// packIndexes writes the collected pixels' places in the table into out,
// width pixels to a row: 1<<packing places share a pixel's green, the first
// in the lowest bits.
func (p *palette) packIndexes(out []uint32, width, height, packing int) {
	if packing == 0 {
		var greens [256]uint32
		for number, index := range p.ranks {
			greens[number] = 0xff000000 | uint32(index)<<8
		}
		for position, number := range p.found[:width*height] {
			out[position] = greens[number]
		}
		return
	}
	perPixel := 1 << packing
	indexBits := uint(8 >> packing)
	packedWidth := (width + perPixel - 1) >> packing
	for y := range height {
		row := p.found[y*width : (y+1)*width]
		packed := out[y*packedWidth : (y+1)*packedWidth]
		for x := range packed {
			group := row[x<<packing : min((x+1)<<packing, width)]
			value := uint32(0)
			for place, number := range group {
				value |= uint32(p.ranks[number]) << (uint(place) * indexBits)
			}
			packed[x] = 0xff000000 | value<<8
		}
	}
}
