package webhost

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"github.com/movingwoo/wfeature/internal/filter/hqx"
)

// The session protocols a page can speak. A page asks for the newer one with
// `/api/session?protocol=2`; anything else, including no query at all, is the
// first protocol, which an older page still speaks.
const (
	// protocolPictures sends every changed picture as a complete PNG,
	// magnified on the server, and sound as JSON.
	protocolPictures = 1
	// protocolStream sends pictures at the guest's own size as updates to the
	// one the page already holds, leaves magnification to the page, and sends
	// sound as binary with each sample carried once. See docs/session.md.
	protocolStream = 2
)

// negotiatedProtocol reads the version a page asked for. An unknown value is
// answered with the first protocol rather than refused, so a page newer than
// this server still gets pictures it can draw.
func negotiatedProtocol(query string) int {
	if query == "2" {
		return protocolStream
	}
	return protocolPictures
}

// A protocol 2 picture message starts with this header, big-endian throughout:
//
//	0  "WFP2"
//	4  operation: complete, replace or masked
//	5  presentation scale the page magnifies by, 1 to 4
//	6  horizontal shift of the held picture before drawing, signed (masked only)
//	8  vertical shift, signed
//	10 x of the rectangle
//	12 y of the rectangle
//	14 reserved, zero
//	16 PNG
//
// A masked update whose shift already produced every pixel carries no PNG.
const pictureHeaderSize = 16

var pictureMagic = []byte("WFP2")

// Picture operations.
const (
	// pictureComplete replaces the held picture, and its size, entirely.
	pictureComplete = 0
	// pictureReplace replaces a rectangle, transparency included.
	pictureReplace = 1
	// pictureMasked draws a rectangle over the held picture: a transparent
	// pixel keeps what was there. Every other pixel in it is opaque.
	pictureMasked = 2
)

// maxScrollSearch is how far the encoder looks for a picture that moved. The
// games scroll their fields and backgrounds by a pixel or two a frame; sixteen
// covers the fastest measured and keeps the search a small part of a frame.
const maxScrollSearch = 16

// scrollSearchShare gates the search: it runs when at least 1/scrollSearchShare
// of the picture changed. A side-scrolling title moves a band of its screen, a
// few percent of the pixels; an eighth let 59 of its 2,895 frames scroll and a
// thirty-second 1,046, for 18% less traffic. Lower gates found little more.
const scrollSearchShare = 32

// pictureEncoder turns one connection's pictures into messages. It is owned by
// the encoder goroutine.
type pictureEncoder struct {
	protocol int
	// The first protocol's pictures are complete, so their compression is
	// paid on every one; BestSpeed is what it always used.
	legacy png.Encoder
	scaler hqx.Scaler
	// scaleFailure is the last magnification that failed, for the caller to
	// report; the picture went out at its own size.
	scaleFailure error
	// The second protocol's pictures are mostly small updates, so the better
	// compression costs little and is paid on few bytes.
	stream png.Encoder
	buffer bytes.Buffer

	// base is the picture the page holds once the last message is accepted,
	// packed one pixel per word in RGBA byte order. next is the picture being
	// encoded; accept swaps them.
	base, next             []uint32
	width, height, scale   int
	baseOpaque, nextOpaque bool
	nextWidth, nextHeight  int
	nextScale              int
	// lastShift is the most recent scroll the page was sent; a game scrolls
	// by the same amount frame after frame, so it is tried before searching.
	lastShift, nextShift image.Point
	prediction           []uint32
	palette              map[uint32]uint8
	paletteColors        color.Palette
	paletteIndex         []uint8
}

func newPictureEncoder(protocol int) *pictureEncoder {
	return &pictureEncoder{
		protocol: protocol,
		legacy:   png.Encoder{CompressionLevel: png.BestSpeed, BufferPool: pngBuffers},
		stream:   png.Encoder{CompressionLevel: png.DefaultCompression, BufferPool: pngBuffers},
		palette:  make(map[uint32]uint8, 256),
	}
}

// encode answers the message for one picture, or nil when the page already
// holds exactly this picture. The answer is its own bytes.
func (e *pictureEncoder) encode(frame pendingFrame) ([]byte, error) {
	if frame.Width <= 0 || frame.Height <= 0 || len(frame.RGBA) < frame.Width*frame.Height*4 {
		return nil, fmt.Errorf("a %dx%d picture holds %d bytes", frame.Width, frame.Height, len(frame.RGBA))
	}
	if e.protocol == protocolStream {
		return e.encodeStream(frame)
	}
	return e.encodeComplete(frame)
}

// accept records that the last message went to the page. An update is only
// meaningful against the picture the page actually has, so the base moves
// here rather than when a message is merely composed.
func (e *pictureEncoder) accept() {
	if e.protocol != protocolStream {
		return
	}
	e.base, e.next = e.next, e.base
	e.baseOpaque = e.nextOpaque
	e.width, e.height, e.scale = e.nextWidth, e.nextHeight, e.nextScale
	if e.nextShift != (image.Point{}) {
		e.lastShift = e.nextShift
	}
	e.nextShift = image.Point{}
}

// encodeComplete is the first protocol: magnify here, send it all.
func (e *pictureEncoder) encodeComplete(frame pendingFrame) ([]byte, error) {
	if frame.Scale > 1 {
		pixels, width, height, err := e.scaler.ScaleRGBA(frame.RGBA, frame.Width, frame.Height, frame.Scale)
		if err != nil {
			// A picture at its own size beats no picture; the caller logs why.
			e.scaleFailure = err
		} else {
			frame.RGBA, frame.Width, frame.Height = pixels, width, height
		}
	}
	picture := &image.RGBA{Pix: frame.RGBA, Stride: frame.Width * 4, Rect: image.Rect(0, 0, frame.Width, frame.Height)}
	e.buffer.Reset()
	if err := e.legacy.Encode(&e.buffer, picture); err != nil {
		return nil, err
	}
	return bytes.Clone(e.buffer.Bytes()), nil
}

func (e *pictureEncoder) encodeStream(frame pendingFrame) ([]byte, error) {
	width, height := frame.Width, frame.Height
	scale := min(max(frame.Scale, 1), 4)
	e.next = packPixels(e.next, frame.RGBA[:width*height*4])
	e.nextOpaque = opaquePixels(e.next)
	e.nextWidth, e.nextHeight, e.nextScale = width, height, scale

	header := make([]byte, pictureHeaderSize)
	copy(header, pictureMagic)
	header[5] = byte(scale)
	e.buffer.Reset()
	whole := image.Rect(0, 0, width, height)
	if frame.Force || e.base == nil || width != e.width || height != e.height || scale != e.scale {
		header[4] = pictureComplete
		e.buffer.Write(header)
		if err := e.encodeRectangle(e.next, width, whole, nil); err != nil {
			return nil, err
		}
		return bytes.Clone(e.buffer.Bytes()), nil
	}

	bounds, changed := changedPixels(e.next, e.base, width, height)
	if changed == 0 {
		return nil, nil
	}
	operation, prediction, dx, dy := pictureMasked, e.base, 0, 0
	// A scroll is looked for only where a share of the picture changed: a
	// sprite that moved is already a small rectangle, and the search is the
	// most expensive thing here. The held picture must be opaque, because the
	// page draws it over itself and a translucent pixel would blend.
	if e.baseOpaque && changed*scrollSearchShare >= width*height {
		if shiftX, shiftY, found := findScroll(e.next, e.base, width, height, bounds, e.lastShift); found {
			e.prediction = shiftedPixels(e.prediction, e.base, width, height, shiftX, shiftY)
			if shiftedBounds, remaining := changedPixels(e.next, e.prediction, width, height); remaining*5 < changed*4 {
				prediction, bounds, changed, dx, dy = e.prediction, shiftedBounds, remaining, shiftX, shiftY
			}
		}
	}
	if !opaqueWhereChanged(e.next, prediction, width, bounds) {
		// A changed pixel that is not opaque would blend where it has to
		// replace, so this update replaces its rectangle instead.
		operation, prediction, dx, dy = pictureReplace, nil, 0, 0
		bounds, changed = changedPixels(e.next, e.base, width, height)
	}
	e.nextShift = image.Pt(dx, dy)
	header[4] = byte(operation)
	binary.BigEndian.PutUint16(header[6:8], uint16(int16(dx)))
	binary.BigEndian.PutUint16(header[8:10], uint16(int16(dy)))
	binary.BigEndian.PutUint16(header[10:12], uint16(bounds.Min.X))
	binary.BigEndian.PutUint16(header[12:14], uint16(bounds.Min.Y))
	e.buffer.Write(header)
	if changed > 0 {
		if err := e.encodeRectangle(e.next, width, bounds, prediction); err != nil {
			return nil, err
		}
	}
	return bytes.Clone(e.buffer.Bytes()), nil
}

// encodeRectangle appends one rectangle of pixels as a PNG. With a prediction,
// a pixel equal to it is written transparent: the page keeps what it has
// there, and a run of identical transparent pixels is what compresses best.
// Where the rectangle has at most 256 distinct values it is written with a
// palette, one byte a pixel before compression instead of four.
func (e *pictureEncoder) encodeRectangle(pixels []uint32, width int, bounds image.Rectangle, prediction []uint32) error {
	rectangleWidth, rectangleHeight := bounds.Dx(), bounds.Dy()
	count := rectangleWidth * rectangleHeight
	clear(e.palette)
	e.paletteColors = e.paletteColors[:0]
	if cap(e.paletteIndex) < count {
		e.paletteIndex = make([]uint8, count)
	}
	indices := e.paletteIndex[:count]
	paletted := true
	last, lastIndex, haveLast := uint32(0), uint8(0), false
	for y := bounds.Min.Y; y < bounds.Max.Y && paletted; y++ {
		row := y * width
		out := (y - bounds.Min.Y) * rectangleWidth
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := pixels[row+x]
			if prediction != nil && value == prediction[row+x] {
				value = 0
			}
			if haveLast && value == last {
				indices[out+x-bounds.Min.X] = lastIndex
				continue
			}
			index, known := e.palette[value]
			if !known {
				if len(e.paletteColors) == 256 {
					paletted = false
					break
				}
				index = uint8(len(e.paletteColors))
				e.palette[value] = index
				e.paletteColors = append(e.paletteColors, color.RGBA{R: uint8(value), G: uint8(value >> 8), B: uint8(value >> 16), A: uint8(value >> 24)})
			}
			indices[out+x-bounds.Min.X] = index
			last, lastIndex, haveLast = value, index, true
		}
	}
	if paletted {
		picture := &image.Paletted{Pix: indices, Stride: rectangleWidth,
			Rect: image.Rect(0, 0, rectangleWidth, rectangleHeight), Palette: e.paletteColors}
		return e.stream.Encode(&e.buffer, picture)
	}
	picture := image.NewRGBA(image.Rect(0, 0, rectangleWidth, rectangleHeight))
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := y * width
		out := (y - bounds.Min.Y) * picture.Stride
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := pixels[row+x]
			if prediction != nil && value == prediction[row+x] {
				value = 0
			}
			binary.LittleEndian.PutUint32(picture.Pix[out+(x-bounds.Min.X)*4:], value)
		}
	}
	return e.stream.Encode(&e.buffer, picture)
}

// packPixels reads RGBA bytes as one word a pixel. Equality is all anything
// here asks of a pixel, and a word compares in one step.
func packPixels(into []uint32, rgba []byte) []uint32 {
	count := len(rgba) / 4
	if cap(into) < count {
		into = make([]uint32, count)
	}
	into = into[:count]
	for index := range into {
		into[index] = binary.LittleEndian.Uint32(rgba[index*4:])
	}
	return into
}

func opaquePixels(pixels []uint32) bool {
	for _, value := range pixels {
		if value>>24 != 0xff {
			return false
		}
	}
	return true
}

// changedPixels answers the rectangle bounding every pixel that differs from
// the prediction, and how many do.
func changedPixels(current, prediction []uint32, width, height int) (image.Rectangle, int) {
	left, top, right, bottom := width, height, 0, 0
	changed := 0
	for y := 0; y < height; y++ {
		row := y * width
		for x := 0; x < width; x++ {
			if current[row+x] != prediction[row+x] {
				changed++
				left, right = min(left, x), max(right, x+1)
				top, bottom = min(top, y), y+1
			}
		}
	}
	if changed == 0 {
		return image.Rectangle{}, 0
	}
	return image.Rect(left, top, right, bottom), changed
}

// opaqueWhereChanged reports whether every pixel that differs from the
// prediction inside bounds is opaque, which is what drawing a masked update
// over the held picture needs.
func opaqueWhereChanged(current, prediction []uint32, width int, bounds image.Rectangle) bool {
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := y * width
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if value := current[row+x]; value != prediction[row+x] && value>>24 != 0xff {
				return false
			}
		}
	}
	return true
}

// findScroll looks for the shift of the held picture that best predicts the
// new one, along either axis. A field that scrolled by a pixel differs from
// the last picture almost everywhere and from the last picture moved by that
// pixel almost nowhere. Candidates are scored on every fourth row of the
// changed area, which is enough to rank them; the caller measures the winner
// on every pixel before using it.
func findScroll(current, base []uint32, width, height int, bounds image.Rectangle, hint image.Point) (int, int, bool) {
	samples := (bounds.Dy() + 3) / 4 * bounds.Dx()
	score := func(dx, dy int) int {
		matches := 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y += 4 {
			sourceY := y - dy
			row := y * width
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				sourceX := x - dx
				predicted := base[row+x]
				if sourceX >= 0 && sourceX < width && sourceY >= 0 && sourceY < height {
					predicted = base[sourceY*width+sourceX]
				}
				if current[row+x] == predicted {
					matches++
				}
			}
		}
		return matches
	}
	unshifted := score(0, 0)
	// The last scroll, when it still predicts nearly every sampled pixel, is
	// taken without looking further: a field scrolls by the same amount frame
	// after frame, and the search is most of what a scrolling frame costs.
	if hint != (image.Point{}) && max(hint.X, -hint.X) < width && max(hint.Y, -hint.Y) < height {
		if matches := score(hint.X, hint.Y); matches > unshifted && matches*8 >= samples*7 {
			return hint.X, hint.Y, true
		}
	}
	best, bestX, bestY := unshifted, 0, 0
	for distance := 1; distance <= maxScrollSearch; distance++ {
		for _, shift := range [4][2]int{{distance, 0}, {-distance, 0}, {0, distance}, {0, -distance}} {
			if shift[0] >= width || -shift[0] >= width || shift[1] >= height || -shift[1] >= height {
				continue
			}
			if matches := score(shift[0], shift[1]); matches > best {
				best, bestX, bestY = matches, shift[0], shift[1]
			}
		}
	}
	return bestX, bestY, best > unshifted
}

// shiftedPixels is the picture the page has after drawing its held picture at
// (dx, dy) over itself: the covered area moves and the strip it uncovers keeps
// what was there.
func shiftedPixels(into, base []uint32, width, height, dx, dy int) []uint32 {
	into = append(into[:0], base...)
	span := width - max(dx, -dx)
	for y := max(0, dy); y < min(height, height+dy); y++ {
		target := y*width + max(0, dx)
		source := (y-dy)*width + max(0, -dx)
		copy(into[target:target+span], base[source:source+span])
	}
	return into
}
