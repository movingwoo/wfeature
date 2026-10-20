package ktf

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/curve"
)

// One local title queries a second drawing interface beside the screen, and
// draws through it everything that is not a bitmap or text: the stars it
// scatters over its title screen, the sparks and trails of every firework, and
// a ring around the point it aims at. Until it was answered every call on it
// was a trap that answered zero, and the title drew none of those.
//
// What each method takes comes off its call sites:
//
//	+0x10 (r, g, b), 255 on the stack   before every point, line and ring; a
//	                                    grey when the title twinkles a star
//	+0x28 (1) or (2)                    before the stars and before the sparks
//	+0x30 (record, 0)                   a byte 1, then four halfwords from
//	                                    offset 4: x, y, width and height. The
//	                                    whole screen at the top of every frame
//	                                    and a box elsewhere, and the caller
//	                                    that draws in a box draws only when the
//	                                    answer is not zero
//	+0x50 (record)                      two halfwords, x and y
//	+0x54 (record)                      four halfwords: from x and y, to x and y
//	+0x5c (record)                      three halfwords: x and y, and a 4
//
// So they are a pen colour, a point size, a rectangle to clip to, and a point,
// a line and a circle drawn in that colour inside it, onto the screen the
// display draws on. The stack word beside the colour is 255 at every call site
// and is not acted on: a pen draws opaque.
const (
	nativeInterfaceShapes = 0x1002001

	nativeShapesColour    = 0x10
	nativeShapesPointSize = 0x28
	nativeShapesClip      = 0x30
	nativeShapesPoint     = 0x50
	nativeShapesLine      = 0x54
	nativeShapesCircle    = 0x5c
)

// nativeClipRectangle is the clip record's first byte for a rectangle, the
// only shape a title has been seen to clip to. Another shape is refused
// rather than read as a rectangle, and the clip stays as it was.
const nativeClipRectangle = 1

// maxNativePointSize bounds a point, which is a square that size centred on
// where it is drawn, on any screen a session runs at. On a screen it is bounded
// by that screen's longer side (nativePointSizeOn): a point that size already
// covers what the screen can show of it, and the part of the square inside the
// screen, filled a row at a time, is all a point costs — what a rectangle of
// that size costs the display's own fill.
const maxNativePointSize = maxNativeStateScreen

// nativePointSizeOn is the largest point a screen of the given size is drawn
// with.
func nativePointSizeOn(width, height int) int32 {
	return int32(min(max(width, height), maxNativePointSize))
}

// nativeShapes is the interface's state: what it draws with and where.
type nativeShapes struct {
	pen       uint32
	pointSize int32
	// clip is where drawing lands, in screen coordinates, when clipped says
	// the title set one.
	clip    image.Rectangle
	clipped bool
}

// installShapes registers the second drawing interface.
func (platform *NativePlatform) installShapes() {
	surface := nativeInterfaceSurface(nativeInterfaceShapes)
	platform.client.Serve(surface, nativeShapesColour, platform.shapesColour)
	platform.client.Serve(surface, nativeShapesPointSize, platform.shapesPointSize)
	platform.client.Serve(surface, nativeShapesClip, platform.shapesClip)
	platform.client.Serve(surface, nativeShapesPoint, platform.shapesPoint)
	platform.client.Serve(surface, nativeShapesLine, platform.shapesLine)
	platform.client.Serve(surface, nativeShapesCircle, platform.shapesCircle)
}

// shapesColour sets the pen. It is kept packed the way the screen's own
// colours are, so one decoder reads both. Its answer is not read. A title sets
// it before every shape, so the channels are read straight out of r1 to r3.
func (platform *NativePlatform) shapesColour(thread *armcore.Thread) (uint32, error) {
	pen := uint32(0)
	for register := 1; register <= 3; register++ {
		channel, err := thread.Register(register)
		if err != nil {
			return 0, err
		}
		pen |= uint32(uint8(channel)) << (32 - 8*register)
	}
	platform.shapes.pen = pen
	return 0, nil
}

// shapesPointSize sets how large a point is. Its answer is not read.
func (platform *NativePlatform) shapesPointSize(thread *armcore.Thread) (uint32, error) {
	size, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	screen := platform.screen.frame.Bounds()
	platform.shapes.pointSize = min(max(int32(size), 1), nativePointSizeOn(screen.Dx(), screen.Dy()))
	return 0, nil
}

// nativeClipRecordSize covers the clip record: the shape byte, padding to a
// halfword boundary the title leaves alone, and the rectangle.
const nativeClipRecordSize = 12

// shapesClip sets where drawing lands. A null record lifts the clip.
func (platform *NativePlatform) shapesClip(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if address == 0 {
		platform.shapes.clipped = false
		return 1, nil
	}
	var record [nativeClipRecordSize]byte
	if err := platform.client.core.Memory().Read(address, record[:]); err != nil {
		return 0, fmt.Errorf("read KTF native clip at %#x: %w", address, err)
	}
	if record[0] != nativeClipRectangle {
		return 0, nil
	}
	x, y, width, height := nativeHalfwords(record[4:], 4)
	platform.shapes.clip = image.Rect(x, y, x+max(width, 0), y+max(height, 0))
	platform.shapes.clipped = true
	return 1, nil
}

// nativeHalfwords reads the signed halfwords a shape record is made of. It
// answers four, and the ones past count are zero.
func nativeHalfwords(record []byte, count int) (int, int, int, int) {
	values := [4]int{}
	for index := range count {
		values[index] = int(int16(binary.LittleEndian.Uint16(record[index*2:])))
	}
	return values[0], values[1], values[2], values[3]
}

// readShape reads count halfwords, at most four, of the record a drawing call
// names. The record is read onto the stack: a title draws thousands of these
// a frame.
func (platform *NativePlatform) readShape(thread *armcore.Thread, count int) (int, int, int, int, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	var record [8]byte
	if err := platform.client.core.Memory().Read(address, record[:count*2]); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("read KTF native shape at %#x: %w", address, err)
	}
	a, b, c, d := nativeHalfwords(record[:], count)
	return a, b, c, d, nil
}

// shapesBounds is where a drawing call may land: the screen, inside the clip
// when there is one.
func (platform *NativePlatform) shapesBounds() image.Rectangle {
	bounds := platform.screen.frame.Bounds()
	if platform.shapes.clipped {
		bounds = bounds.Intersect(platform.shapes.clip)
	}
	return bounds
}

// plot draws one pixel in the pen colour if it lands inside bounds.
func (platform *NativePlatform) plot(bounds image.Rectangle, x, y int, pen color.RGBA) {
	if (image.Point{X: x, Y: y}).In(bounds) {
		platform.screen.frame.SetRGBA(x, y, pen)
	}
}

// shapesPoint draws a point: a square of the point size, centred on where it
// is drawn, with the extra pixel of an even size below and to the right.
// Answering zero is what the interface's other calls answer for one that
// worked, and no caller reads it.
func (platform *NativePlatform) shapesPoint(thread *armcore.Thread) (uint32, error) {
	x, y, _, _, err := platform.readShape(thread, 2)
	if err != nil {
		return 0, err
	}
	size := int(max(platform.shapes.pointSize, 1))
	square := image.Rect(x, y, x+size, y+size).Sub(image.Pt((size-1)/2, (size-1)/2))
	fillNativeRectangle(platform.screen.frame, square.Intersect(platform.shapesBounds()), nativeColour(platform.shapes.pen))
	platform.screen.draws++
	return 0, nil
}

// shapesLine draws a line one pixel wide, both ends included. It is the line
// the WIPI C table and Java's Graphics draw, walked only across the stretch of
// it inside the clip: the ends are untrusted halfwords, and a line that spans
// them is tens of thousands of steps that land nowhere.
func (platform *NativePlatform) shapesLine(thread *armcore.Thread) (uint32, error) {
	x, y, toX, toY, err := platform.readShape(thread, 4)
	if err != nil {
		return 0, err
	}
	bounds, pen := platform.shapesBounds(), nativeColour(platform.shapes.pen)
	// The plot never fails, so neither does the walk.
	_ = clippedLine(int32(x), int32(y), int32(toX), int32(toY),
		int64(bounds.Min.X), int64(bounds.Min.Y), int64(bounds.Max.X), int64(bounds.Max.Y),
		func(x, y int32) error {
			platform.plot(bounds, int(x), int(y), pen)
			return nil
		})
	platform.screen.draws++
	return 0, nil
}

// shapesCircle draws the outline of a circle: the arc the WIPI C table and
// Java's Graphics draw, all the way round the square the radius makes about
// the centre, and only the stretches of it that land inside the clip. A circle
// of no radius is its centre. Nothing a title has been seen to call asks for a
// filled one.
func (platform *NativePlatform) shapesCircle(thread *armcore.Thread) (uint32, error) {
	centreX, centreY, radius, _, err := platform.readShape(thread, 3)
	if err != nil {
		return 0, err
	}
	if radius < 0 {
		return 0, nil
	}
	bounds, pen := platform.shapesBounds(), nativeColour(platform.shapes.pen)
	if radius == 0 {
		platform.plot(bounds, centreX, centreY, pen)
	} else {
		within := curve.Bounds{Left: int64(bounds.Min.X), Top: int64(bounds.Min.Y), Right: int64(bounds.Max.X), Bottom: int64(bounds.Max.Y)}
		// The emit never fails, so neither does the walk.
		_ = curve.DrawArcWithin(within, int32(centreX-radius), int32(centreY-radius), int32(2*radius), int32(2*radius), 0, 360,
			func(span curve.Span) error {
				fillNativeRectangle(platform.screen.frame, image.Rect(int(span.X), int(span.Y), int(span.X+span.Width), int(span.Y)+1), pen)
				return nil
			})
	}
	platform.screen.draws++
	return 0, nil
}
