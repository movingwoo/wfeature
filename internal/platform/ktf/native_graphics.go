package ktf

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The interface the class factory queries while it builds the title's object
// is the screen. Its methods came off their call sites and out of one table in
// the module:
//
//	m5(rect, -1, colour, 2)  drew a run of 1x1 rectangles whose colours came
//	                         from a 36-word table in the image, and that table
//	                         is what names the colour format: it holds
//	                         0xffffff00, 0x0000ff00, 0x00ff0000, 0xff000000,
//	                         0x00ffff00, 0xff00ff00, 0xffff0000 and zero —
//	                         white, blue, green, red, cyan, magenta, yellow
//	                         and black, packed as 0xRRGGBB00.
//	m7()                     is the only call the title makes in a frame that
//	                         draws nothing, which is what a present looks like.
//
// See docs/ktf.md.
const (
	// nativeScreenText takes a flag word, a string and more. The title draws
	// its loading message through it.
	nativeScreenText = 0x10
	// nativeScreenRectangle takes a rectangle, two colours and a mode.
	nativeScreenRectangle = 0x14
	// nativeScreenBlit takes a destination position, a size, a pointer to
	// pixels and a source position. Its call site builds those from a sprite
	// record and a clip, which is what names them.
	nativeScreenBlit = 0x18
	// nativeScreenPresent ends a frame.
	nativeScreenPresent = 0x1c
)

// nativeNoColour is the value both colour arguments carry when the title means
// "leave it alone". It is also what a fully saturated colour would be, so a
// call that passes it for the fill is ambiguous; the one call site that does
// passes no rectangle either, which reads as a clear.
const nativeNoColour = 0xffffffff

// nativeScreen is the frame the title draws into.
type nativeScreen struct {
	frame    *image.RGBA
	presents int
	draws    int
	blits    []NativeBlit
	// missed counts blits whose image this platform did not build, which is
	// what says a screen is empty because a factory refused rather than
	// because the title drew nothing.
	missed int
}

// installScreen registers the screen interface.
func (platform *NativePlatform) installScreen() {
	width, height := platform.screenSize()
	platform.screen = &nativeScreen{
		frame: image.NewRGBA(image.Rect(0, 0, width, height)),
	}
	surface := nativeInterfaceSurface(nativeInterfaceApplication)
	platform.client.Serve(surface, nativeScreenRectangle, platform.drawRectangle)
	platform.client.Serve(surface, nativeScreenBlit, platform.blit)
	platform.client.Serve(surface, nativeScreenPresent, platform.present)
}

// Frame reports what the title has drawn, and how many times it has ended a
// frame. A Host presents the image; a probe writes it out.
func (platform *NativePlatform) Frame() (*image.RGBA, int) {
	if platform.screen == nil {
		return nil, 0
	}
	return platform.screen.frame, platform.screen.presents
}

// Missed reports blits whose image the platform never built.
func (platform *NativePlatform) Missed() int {
	if platform.screen == nil {
		return 0
	}
	return platform.screen.missed
}

// Draws reports how many rectangles and images the title has drawn, which is what says a
// frame that looks empty was never drawn rather than drawn blank.
func (platform *NativePlatform) Draws() int {
	if platform.screen == nil {
		return 0
	}
	return platform.screen.draws
}

// nativeColour decodes the packed colour the title's own palette established.
func nativeColour(packed uint32) color.RGBA {
	return color.RGBA{
		R: uint8(packed >> 24),
		G: uint8(packed >> 16),
		B: uint8(packed >> 8),
		A: 0xff,
	}
}

// drawRectangle fills one rectangle. A null rectangle covers the screen: the
// one call site that passes null passes no size either, and the title makes it
// before it has drawn anything.
func (platform *NativePlatform) drawRectangle(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	bounds := platform.screen.frame.Bounds()
	if arguments[1] != 0 {
		record := make([]byte, 8)
		if err := platform.client.core.Memory().Read(arguments[1], record); err != nil {
			return 0, fmt.Errorf("read KTF native rectangle at %#x: %w", arguments[1], err)
		}
		x := int(int16(binary.LittleEndian.Uint16(record[0:])))
		y := int(int16(binary.LittleEndian.Uint16(record[2:])))
		width := int(int16(binary.LittleEndian.Uint16(record[4:])))
		height := int(int16(binary.LittleEndian.Uint16(record[6:])))
		bounds = image.Rect(x, y, x+width, y+height).Intersect(bounds)
	}
	fill := nativeColour(arguments[3])
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			platform.screen.frame.SetRGBA(x, y, fill)
		}
	}
	platform.screen.draws++
	return 1, nil
}

// NativeBlit is one blit the title asked for, as its arguments arrived.
type NativeBlit struct {
	X, Y, Width, Height int
	Pixels              uint32
	SourceX, SourceY    int
	Mode                uint32
}

// Blits reports the first blits of the run, which is how a probe checks the
// argument order against what the title is drawing.
func (platform *NativePlatform) Blits() []NativeBlit { return platform.screen.blits }

// blit draws one image. Its arguments came off two call sites that build them
// the same way — a destination, a size, the object the factory answered with,
// a source position and a mode — and the sizes and positions they carry are
// sprite-shaped against a 240x320 screen, which is what says the order is
// right.
//
// The mode is not decoded. Every blit the local title makes passes 7, and a
// mode that is always the same says nothing about what its bits mean.
func (platform *NativePlatform) blit(thread *armcore.Thread) (uint32, error) {
	stacked := platform.varargs(thread, 4)
	// An argument that cannot be read is kept rather than swallowed: a stacked
	// read only fails when the caller's stack is not where it should be, and a
	// blit assembled out of zeroes would draw rather than say so.
	var stackedErr error
	next := func() uint32 {
		value, err := stacked(1)
		if err != nil && stackedErr == nil {
			stackedErr = err
		}
		return uint32(value)
	}
	x, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	y, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	width, err := thread.Register(3)
	if err != nil {
		return 0, err
	}
	record := NativeBlit{
		X:      int(int32(x)),
		Y:      int(int32(y)),
		Width:  int(int32(width)),
		Height: int(int32(next())),
	}
	record.Pixels = next()
	record.SourceX = int(int32(next()))
	record.SourceY = int(int32(next()))
	record.Mode = next()
	if stackedErr != nil {
		return 0, fmt.Errorf("read KTF native blit arguments: %w", stackedErr)
	}
	if len(platform.screen.blits) < maxNativeRecordedBlits {
		platform.screen.blits = append(platform.screen.blits, record)
	}
	source, ok := platform.images[record.Pixels]
	if !ok {
		// An image the platform never built is not an error: the title asks
		// the factory for classes this platform does not make, keeps what it
		// gets, and draws with it anyway.
		platform.screen.missed++
		return 0, nil
	}
	platform.drawImage(source, record)
	platform.screen.draws++
	return 1, nil
}

// nativeTransparent is the colour the title's own artwork uses for the pixels
// it does not want drawn. Eleven of the fourteen images the local title builds
// have it in their top-left corner, and drawing it opaque puts a magenta box
// around every sprite — which is the whole evidence, because the blit carries
// no colour of its own and the mode it passes is always the same.
var nativeTransparent = color.RGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff}

// drawImage copies one region of a decoded image onto the frame, clipped to
// both, leaving the transparent colour behind.
func (platform *NativePlatform) drawImage(source *nativeImage, blit NativeBlit) {
	frame := platform.screen.frame
	bounds := source.frame.Bounds()
	// The size comes off the guest's own stack, so the loop is bounded by where
	// the copy can land rather than by what it was told. Skipping the rows and
	// columns one at a time is the same picture, but a width the title computed
	// wrongly would be counted to instead of drawn — and at the top of the
	// range that is thousands of millions of turns doing nothing.
	rows := blit.Height
	columns := blit.Width
	rows = min(rows, min(bounds.Max.Y-blit.SourceY, frame.Bounds().Max.Y-blit.Y))
	columns = min(columns, min(bounds.Max.X-blit.SourceX, frame.Bounds().Max.X-blit.X))
	for row := 0; row < rows; row++ {
		sourceY := blit.SourceY + row
		targetY := blit.Y + row
		if sourceY < bounds.Min.Y || targetY < 0 {
			continue
		}
		for column := 0; column < columns; column++ {
			sourceX := blit.SourceX + column
			targetX := blit.X + column
			if sourceX < bounds.Min.X || targetX < 0 {
				continue
			}
			pixel := source.frame.RGBAAt(sourceX, sourceY)
			if pixel == nativeTransparent {
				continue
			}
			frame.SetRGBA(targetX, targetY, pixel)
		}
	}
}

// maxNativeRecordedBlits bounds what a run keeps for a probe to read.
const maxNativeRecordedBlits = 64

// present ends a frame.
func (platform *NativePlatform) present(*armcore.Thread) (uint32, error) {
	platform.screen.presents++
	return 1, nil
}
