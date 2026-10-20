package ktf

import (
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

// nativeNoColour is the value a colour argument carries when the title names
// no colour of its own. Every rectangle a title has been seen to draw passes
// it for the frame, beside a mode that draws no frame. For the fill it is the
// screen's background item: the call sites that pass it there pass no
// rectangle either, which is a clear, and a clear is what a fill in the
// background is.
const nativeNoColour = 0xffffffff

// A clear fills with the screen's background item, which is what a fill that
// names no colour of its own takes. A later title is what says so: at the top
// of every frame it sets item 2 to black and fills the whole screen with
// nativeNoColour, and then scatters grey stars over the result. Filling with
// white instead left the stars on a white sky. The earlier titles set no item
// and clear the same way to white, which is what the item holds until a title
// sets it. The rectangle says where the fill lands and the colour what it is,
// so a smaller rectangle filled with nativeNoColour is the same fill over less
// of the screen.
const nativeBackgroundColourItem = 2

// nativeRectangleFill is the bit of a rectangle's mode that asks for its inside
// to be filled. Every rectangle a title has been seen to draw passes a mode of
// 2 and is a fill, the clear among them. A mode without the bit asks for no
// fill — an outline, which this platform does not draw — and the inside is
// left as it is rather than painted over.
const nativeRectangleFill = 2

var nativeDefaultBackground = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

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

// FrameDigest fingerprints the current screen without copying it out.
//
// It is a fingerprint and not an identity: two different screens can collide.
// For deciding whether a title is still animating that is harmless, and it
// keeps the check to one pass over the pixels.
func (platform *NativePlatform) FrameDigest() uint64 {
	if platform == nil || platform.screen == nil || platform.screen.frame == nil {
		return 0
	}
	// FNV-1a over the frame bytes, which is what the descriptor package's
	// Client.FrameDigest answers with too — one route runner reads both.
	const offset64, prime64 = uint64(14695981039346656037), uint64(1099511628211)
	digest := offset64
	for _, value := range platform.screen.frame.Pix {
		digest = (digest ^ uint64(value)) * prime64
	}
	return digest
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
// before it has drawn anything. The mode is the argument on the stack.
func (platform *NativePlatform) drawRectangle(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	mode, err := platform.varargs(thread, 4)(1)
	if err != nil {
		return 0, err
	}
	bounds := platform.screen.frame.Bounds()
	if arguments[1] != 0 {
		var record [8]byte
		if err := platform.client.core.Memory().Read(arguments[1], record[:]); err != nil {
			return 0, fmt.Errorf("read KTF native rectangle at %#x: %w", arguments[1], err)
		}
		x, y, width, height := nativeHalfwords(record[:], 4)
		bounds = image.Rect(x, y, x+width, y+height).Intersect(bounds)
	}
	platform.screen.draws++
	if uint32(mode)&nativeRectangleFill == 0 {
		return 1, nil
	}
	fill := nativeColour(arguments[3])
	if arguments[3] == nativeNoColour {
		fill = nativeDefaultBackground
		if packed, ok := platform.colours[nativeBackgroundColourItem]; ok {
			fill = nativeColour(packed)
		}
	}
	fillNativeRectangle(platform.screen.frame, bounds, fill)
	return 1, nil
}

// fillNativeRectangle fills bounds, which lie inside the frame, with one
// colour: one row is written a pixel at a time and the rest are copies of it,
// so a fill costs what moving its bytes costs. It is the fill a rectangle and a
// point both are, done without the allocation image/draw makes of a colour.
func fillNativeRectangle(frame *image.RGBA, bounds image.Rectangle, fill color.RGBA) {
	if bounds.Empty() {
		return
	}
	start := frame.PixOffset(bounds.Min.X, bounds.Min.Y)
	row := frame.Pix[start : start+bounds.Dx()*4]
	for offset := 0; offset < len(row); offset += 4 {
		row[offset], row[offset+1], row[offset+2], row[offset+3] = fill.R, fill.G, fill.B, fill.A
	}
	for y := bounds.Min.Y + 1; y < bounds.Max.Y; y++ {
		offset := frame.PixOffset(bounds.Min.X, y)
		copy(frame.Pix[offset:offset+len(row)], row)
	}
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
	// The title draws into the bitmap this platform kept for it and says
	// nothing, so what it has drawn is read here rather than when it was made.
	if err := platform.refresh(source); err != nil {
		return 0, err
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
	// range that is thousands of millions of turns doing nothing. The same
	// holds at the near end: positions far to the left or above both the image
	// and the frame start the loop where the first pixel can land.
	rows := blit.Height
	columns := blit.Width
	rows = min(rows, min(bounds.Max.Y-blit.SourceY, frame.Bounds().Max.Y-blit.Y))
	columns = min(columns, min(bounds.Max.X-blit.SourceX, frame.Bounds().Max.X-blit.X))
	firstRow := max(0, bounds.Min.Y-blit.SourceY, -blit.Y)
	firstColumn := max(0, bounds.Min.X-blit.SourceX, -blit.X)
	for row := firstRow; row < rows; row++ {
		sourceY := blit.SourceY + row
		targetY := blit.Y + row
		if sourceY < bounds.Min.Y || targetY < 0 {
			continue
		}
		for column := firstColumn; column < columns; column++ {
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
