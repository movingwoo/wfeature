package ktf

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/curve"
)

// nativeInterfaceGraphics is the shape interface a later module asks the
// factory for beside the screen: BREW's IGraphics, which draws points and lines
// in a colour it keeps, inside a clip it keeps. The screen's own table draws
// rectangles, images and text; anything round or thin comes through here.
const nativeInterfaceGraphics = 0x1002001

// The shape interface's methods, by byte offset. Each came off its call sites
// in the one local module that asks for the interface, and each shape is the
// one BREW's header gives the method at that place in its table.
//
//	+0x10  r1, r2 and r3 are the same byte and the stacked word is 0xff,
//	       ahead of every point and line: SetColor(r, g, b, alpha).
//	+0x28  1 ahead of a run of points, 2 ahead of the point that heads a
//	       trail: SetPointSize(size).
//	+0x30  a record whose first byte is 1 and whose second halfword pair
//	       is 240x320, once a frame: SetClip(clip, flags), with clip type 1
//	       a rectangle. Its answer is what the module's wrapper returns, and
//	       it is BREW's SUCCESS, zero, as the draw calls' answers are.
//	+0x50  a pointer to two halfwords filled from a particle's position:
//	       DrawPoint(point).
//	+0x54  a pointer to four halfwords, a trail segment's two ends:
//	       DrawLine(line).
//	+0x5c  a pointer to three halfwords, the middle of the screen and then
//	       4 or 12, around the cursor of a game in play: DrawCircle(circle),
//	       a centre and a radius. The module never sets a fill mode, so a
//	       circle is its outline, which is what the specification starts in.
//
// No caller reads what the colour, the size or a shape answers; they answer
// what the specification says they answer.
const (
	nativeShapesColour    = 0x10
	nativeShapesPointSize = 0x28
	nativeShapesClip      = 0x30
	nativeShapesPoint     = 0x50
	nativeShapesLine      = 0x54
	nativeShapesCircle    = 0x5c
)

// The clip record's kinds. A rectangle is the only one the local module sets;
// none clears the clip.
const (
	nativeClipNone      = 0
	nativeClipRectangle = 1
	// nativeClipRecordSize is the kind's word and the rectangle behind it.
	nativeClipRecordSize = 12
)

// nativeShapesFailed is BREW's EFAILED, what a call answers that did not do
// what it was asked.
const nativeShapesFailed = 1

// nativeShapes is what the shape interface keeps between calls.
type nativeShapes struct {
	// colour is the pen. Its alpha is carried and not acted on: every colour
	// the local module sets is opaque, and what a partly transparent pen
	// should do to what is under it is not something it says.
	colour    color.RGBA
	pointSize uint8
	// clip is the rectangle shapes are drawn inside, when clipped is set.
	clipped bool
	clip    image.Rectangle
}

// newNativeShapes is the interface as a title finds it: a black pen one pixel
// wide, and no clip.
func newNativeShapes() *nativeShapes {
	return &nativeShapes{colour: color.RGBA{A: 0xff}, pointSize: 1}
}

// installShapes registers the shape interface.
func (platform *NativePlatform) installShapes() {
	platform.shapes = newNativeShapes()
	surface := nativeInterfaceSurface(nativeInterfaceGraphics)
	platform.client.Serve(surface, nativeShapesColour, platform.shapeColour)
	platform.client.Serve(surface, nativeShapesPointSize, platform.shapePointSize)
	platform.client.Serve(surface, nativeShapesClip, platform.shapeClip)
	platform.client.Serve(surface, nativeShapesPoint, platform.drawPoint)
	platform.client.Serve(surface, nativeShapesLine, platform.drawLine)
	platform.client.Serve(surface, nativeShapesCircle, platform.drawCircle)
}

// shapeColour sets the pen and answers the one it replaces, packed the way the
// screen's colours are: BREW's 0xBBGGRR00.
func (platform *NativePlatform) shapeColour(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	alpha, err := platform.varargs(thread, 4)(1)
	if err != nil {
		return 0, err
	}
	previous := platform.shapes.colour
	platform.shapes.colour = color.RGBA{R: uint8(arguments[1]), G: uint8(arguments[2]), B: uint8(arguments[3]), A: uint8(alpha)}
	return uint32(previous.B)<<24 | uint32(previous.G)<<16 | uint32(previous.R)<<8, nil
}

// shapePointSize sets how wide a point is and answers the size it replaces.
func (platform *NativePlatform) shapePointSize(thread *armcore.Thread) (uint32, error) {
	size, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	previous := platform.shapes.pointSize
	platform.shapes.pointSize = uint8(size)
	return uint32(previous), nil
}

// shapeClip sets the clip from a record, or clears it. It answers BREW's
// SUCCESS, zero, when it took the record, and EFAILED for a kind it cannot
// take, which leaves the clip as it was.
func (platform *NativePlatform) shapeClip(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if address == 0 {
		platform.shapes.clipped = false
		return 0, nil
	}
	record := make([]byte, nativeClipRecordSize)
	if err := platform.client.core.Memory().Read(address, record); err != nil {
		return 0, fmt.Errorf("read KTF native clip at %#x: %w", address, err)
	}
	switch record[0] {
	case nativeClipNone:
		platform.shapes.clipped = false
	case nativeClipRectangle:
		platform.shapes.clipped, platform.shapes.clip = true, nativeRectangle(record[4:])
	default:
		return nativeShapesFailed, nil
	}
	return 0, nil
}

// nativeRectangle reads a rectangle record: four signed halfwords, a corner and
// a size.
func nativeRectangle(record []byte) image.Rectangle {
	x := int(int16(binary.LittleEndian.Uint16(record[0:])))
	y := int(int16(binary.LittleEndian.Uint16(record[2:])))
	width := int(int16(binary.LittleEndian.Uint16(record[4:])))
	height := int(int16(binary.LittleEndian.Uint16(record[6:])))
	return image.Rect(x, y, x+width, y+height)
}

// shapeBounds is where a shape may land: the screen, and inside the clip when
// there is one.
func (platform *NativePlatform) shapeBounds() image.Rectangle {
	bounds := platform.screen.frame.Bounds()
	if platform.shapes.clipped {
		bounds = bounds.Intersect(platform.shapes.clip)
	}
	return bounds
}

// nativeMaxShapeHalfwords is the longest shape record: a line's two ends.
const nativeMaxShapeHalfwords = 4

// readHalfwords fills values with signed halfwords from guest memory. A title
// draws thousands of points a frame, so the record is read into a buffer the
// call keeps on its stack rather than one it allocates.
func (platform *NativePlatform) readHalfwords(address uint32, values []int32, what string) error {
	var buffer [2 * nativeMaxShapeHalfwords]byte
	record := buffer[:2*len(values)]
	if err := platform.client.core.Memory().Read(address, record); err != nil {
		return fmt.Errorf("read KTF native %s at %#x: %w", what, address, err)
	}
	for index := range values {
		values[index] = int32(int16(binary.LittleEndian.Uint16(record[2*index:])))
	}
	return nil
}

// drawPoint draws one point, a square as wide as the point size with the point
// at its centre — at its top left for an even size, whose centre is between
// pixels.
func (platform *NativePlatform) drawPoint(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	var point [2]int32
	if err := platform.readHalfwords(address, point[:], "point"); err != nil {
		return 0, err
	}
	size := max(1, int(platform.shapes.pointSize))
	corner := image.Pt(int(point[0])-(size-1)/2, int(point[1])-(size-1)/2)
	area := image.Rectangle{Min: corner, Max: corner.Add(image.Pt(size, size))}.Intersect(platform.shapeBounds())
	frame, pen := platform.screen.frame, platform.shapes.pen()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			frame.SetRGBA(x, y, pen)
		}
	}
	platform.screen.draws++
	return 0, nil
}

// drawLine draws one line a pixel wide. The point size is the size of a point
// and not of the pen a line is drawn with: the specification gives a line's
// width a call of its own, and the local module never makes it.
func (platform *NativePlatform) drawLine(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	var line [4]int32
	if err := platform.readHalfwords(address, line[:], "line"); err != nil {
		return 0, err
	}
	bounds := platform.shapeBounds()
	frame, pen := platform.screen.frame, platform.shapes.pen()
	err = clippedLine(line[0], line[1], line[2], line[3],
		int64(bounds.Min.X), int64(bounds.Min.Y), int64(bounds.Max.X), int64(bounds.Max.Y),
		func(x, y int32) error {
			if image.Pt(int(x), int(y)).In(bounds) {
				frame.SetRGBA(int(x), int(y), pen)
			}
			return nil
		})
	if err != nil {
		return 0, err
	}
	platform.screen.draws++
	return 0, nil
}

// drawCircle draws one circle's outline a pixel wide. A radius of zero is its
// centre, and a negative one draws nothing.
func (platform *NativePlatform) drawCircle(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	var circle [3]int32
	if err := platform.readHalfwords(address, circle[:], "circle"); err != nil {
		return 0, err
	}
	centreX, centreY, radius := circle[0], circle[1], circle[2]
	bounds := platform.shapeBounds()
	frame, pen := platform.screen.frame, platform.shapes.pen()
	switch {
	case radius == 0:
		if image.Pt(int(centreX), int(centreY)).In(bounds) {
			frame.SetRGBA(int(centreX), int(centreY), pen)
		}
	case radius > 0:
		// The arc's box is as wide as the diameter, which centres the curve
		// on the centre pixel and puts its ends radius pixels either side.
		within := curve.Bounds{Left: int64(bounds.Min.X), Top: int64(bounds.Min.Y), Right: int64(bounds.Max.X), Bottom: int64(bounds.Max.Y)}
		err := curve.DrawArcWithin(within, centreX-radius, centreY-radius, 2*radius, 2*radius, 0, 360, func(span curve.Span) error {
			for x := span.X; x < span.X+span.Width; x++ {
				frame.SetRGBA(int(x), int(span.Y), pen)
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	platform.screen.draws++
	return 0, nil
}

// pen is the colour a shape is drawn in.
func (shapes *nativeShapes) pen() color.RGBA {
	return color.RGBA{R: shapes.colour.R, G: shapes.colour.G, B: shapes.colour.B, A: 0xff}
}

// nativeShapesRecord is a checkpoint that carries the shape interface beside the
// record every earlier build wrote. The record inside is that one unchanged, so
// a checkpoint taken before the interface existed still loads — its title finds
// the interface as a title first does — and a build that predates this record
// refuses the new shape rather than dropping the pen. The two shapes share
// only Version, so what tells them apart is the codec: it takes no record with
// a field missing or one it does not know, and a record therefore decodes as
// one of them or neither.
type nativeShapesRecord struct {
	Version uint32
	State   nativeState
	Shapes  nativeShapesState
}

// nativeShapesRecordVersion is the layout of nativeShapesRecord.
const nativeShapesRecordVersion = 3

type nativeShapesState struct {
	// Colour is the pen as 0xRRGGBBAA.
	Colour                              uint32
	PointSize                           uint32
	Clipped                             bool
	ClipX, ClipY, ClipWidth, ClipHeight int32
}

func (shapes *nativeShapes) capture() nativeShapesState {
	colour := shapes.colour
	return nativeShapesState{
		Colour:    uint32(colour.R)<<24 | uint32(colour.G)<<16 | uint32(colour.B)<<8 | uint32(colour.A),
		PointSize: uint32(shapes.pointSize),
		Clipped:   shapes.clipped,
		ClipX:     int32(shapes.clip.Min.X), ClipY: int32(shapes.clip.Min.Y),
		ClipWidth: int32(shapes.clip.Dx()), ClipHeight: int32(shapes.clip.Dy()),
	}
}

// validate holds a record to what the interface can have set: a point size is
// a byte, and a clip is what a rectangle record of four signed halfwords comes
// to once a negative size has turned it round — a corner as far as two
// halfwords below zero, and a size up to one halfword's span.
func (saved nativeShapesState) validate() error {
	corner := func(value int32) bool { return value >= 2*math.MinInt16 && value <= math.MaxInt16 }
	size := func(value int32) bool { return value >= 0 && value <= -math.MinInt16 }
	if saved.PointSize > math.MaxUint8 || !corner(saved.ClipX) || !corner(saved.ClipY) ||
		!size(saved.ClipWidth) || !size(saved.ClipHeight) {
		return fmt.Errorf("KTF native checkpoint has an invalid shape state")
	}
	return nil
}

func (saved nativeShapesState) restore() *nativeShapes {
	x, y := int(saved.ClipX), int(saved.ClipY)
	return &nativeShapes{
		colour:    color.RGBA{R: uint8(saved.Colour >> 24), G: uint8(saved.Colour >> 16), B: uint8(saved.Colour >> 8), A: uint8(saved.Colour)},
		pointSize: uint8(saved.PointSize),
		clipped:   saved.Clipped,
		clip:      image.Rect(x, y, x+int(saved.ClipWidth), y+int(saved.ClipHeight)),
	}
}

// decodeNativeRecord reads a checkpoint's runtime record in either shape. One
// without the shape interface's state answers the interface a title starts
// with.
func decodeNativeRecord(data []byte) (nativeState, nativeShapesState, error) {
	var extended nativeShapesRecord
	if err := backend.DecodeCheckpointRecord(data, &extended); err == nil {
		if extended.Version != nativeShapesRecordVersion {
			return nativeState{}, nativeShapesState{}, fmt.Errorf("KTF native shapes checkpoint version %d: %w", extended.Version, backend.ErrCheckpointVersion)
		}
		if err := extended.Shapes.validate(); err != nil {
			return nativeState{}, nativeShapesState{}, err
		}
		return extended.State, extended.Shapes, nil
	}
	// The codec checks the whole schema before it allocates, so trying the
	// two shapes in turn cannot half restore a record or accept a field
	// neither has.
	var saved nativeState
	if err := backend.DecodeCheckpointRecord(data, &saved); err != nil {
		return nativeState{}, nativeShapesState{}, err
	}
	return saved, newNativeShapes().capture(), nil
}
