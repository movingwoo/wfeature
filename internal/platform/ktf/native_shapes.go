package ktf

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"

	"github.com/movingwoo/wfeature/internal/armcore"
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
// where it is drawn. A point the size of the largest screen a session runs at
// already covers it.
const maxNativePointSize = maxNativeStateScreen

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
// colours are, so one decoder reads both. Its answer is not read.
func (platform *NativePlatform) shapesColour(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	platform.shapes.pen = uint32(uint8(arguments[1]))<<24 | uint32(uint8(arguments[2]))<<16 | uint32(uint8(arguments[3]))<<8
	return 0, nil
}

// shapesPointSize sets how large a point is. Its answer is not read.
func (platform *NativePlatform) shapesPointSize(thread *armcore.Thread) (uint32, error) {
	size, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	platform.shapes.pointSize = min(max(int32(size), 1), maxNativePointSize)
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
	record := make([]byte, nativeClipRecordSize)
	if err := platform.client.core.Memory().Read(address, record); err != nil {
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

// readShape reads count halfwords of the record a drawing call names.
func (platform *NativePlatform) readShape(thread *armcore.Thread, count int) (int, int, int, int, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	record := make([]byte, count*2)
	if err := platform.client.core.Memory().Read(address, record); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("read KTF native shape at %#x: %w", address, err)
	}
	a, b, c, d := nativeHalfwords(record, count)
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
	square = square.Intersect(platform.shapesBounds())
	pen := nativeColour(platform.shapes.pen)
	for row := square.Min.Y; row < square.Max.Y; row++ {
		for column := square.Min.X; column < square.Max.X; column++ {
			platform.screen.frame.SetRGBA(column, row, pen)
		}
	}
	platform.screen.draws++
	return 0, nil
}

// shapesLine draws a line one pixel wide, both ends included.
func (platform *NativePlatform) shapesLine(thread *armcore.Thread) (uint32, error) {
	x, y, toX, toY, err := platform.readShape(thread, 4)
	if err != nil {
		return 0, err
	}
	bounds, pen := platform.shapesBounds(), nativeColour(platform.shapes.pen)
	// Bresenham's walk, which visits every pixel the line crosses once and
	// steps diagonally where it crosses both ways. The ends are halfwords, so
	// a walk is at most 65,536 steps.
	deltaX, deltaY := distance(x, toX), -distance(y, toY)
	stepX, stepY := towards(x, toX), towards(y, toY)
	balance := deltaX + deltaY
	for {
		platform.plot(bounds, x, y, pen)
		if x == toX && y == toY {
			break
		}
		twice := 2 * balance
		if twice >= deltaY {
			balance += deltaY
			x += stepX
		}
		if twice <= deltaX {
			balance += deltaX
			y += stepY
		}
	}
	platform.screen.draws++
	return 0, nil
}

// shapesCircle draws the outline of a circle. Nothing a title has been seen
// to call asks for a filled one.
func (platform *NativePlatform) shapesCircle(thread *armcore.Thread) (uint32, error) {
	centreX, centreY, radius, _, err := platform.readShape(thread, 3)
	if err != nil {
		return 0, err
	}
	if radius < 0 {
		return 0, nil
	}
	bounds, pen := platform.shapesBounds(), nativeColour(platform.shapes.pen)
	// The midpoint walk over one eighth of the circle, mirrored into the rest.
	x, y, balance := radius, 0, 1-radius
	for x >= y {
		for _, point := range [8][2]int{{x, y}, {y, x}, {-y, x}, {-x, y}, {-x, -y}, {-y, -x}, {y, -x}, {x, -y}} {
			platform.plot(bounds, centreX+point[0], centreY+point[1], pen)
		}
		y++
		if balance < 0 {
			balance += 2*y + 1
		} else {
			x--
			balance += 2*(y-x) + 1
		}
	}
	platform.screen.draws++
	return 0, nil
}

// distance is how far apart two coordinates are, and towards is the step that
// takes the first to the second.
func distance(from, to int) int {
	if to < from {
		return from - to
	}
	return to - from
}

func towards(from, to int) int {
	switch {
	case to < from:
		return -1
	case to > from:
		return 1
	}
	return 0
}
