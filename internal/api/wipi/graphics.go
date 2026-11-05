package wipi

import (
	"fmt"
	"math"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The Graphics members WIPI has and MIDP does not
//
// They are composed here from drawing calls the platform already registers —
// MIDP's fills, lines, text, blits and reset, its Image factory and
// `Image.getRGB`, and the WIPI pair that reads and writes a rectangle of the
// surface — rather than registered as natives of their own. The surface keeps one owner that way, and
// every member answers through the clip, the translation and the lock the
// platform's own calls already apply. A member here is several of those calls,
// so a second guest thread drawing on the same surface at the same moment can
// land between two of them, as two threads' calls always could interleave.
//
// **What each one answers is what the KTF platform answers for the same call**,
// which is this repository's reference for WIPI Graphics, and where the
// specification is explicit it is followed. Where a member here differs from
// KTF, its comment says so and why.

const (
	// xorModeField and strokeStyleField hold the two drawing states a WIPI
	// Graphics carries beyond MIDP's. They are entries in the object's own
	// field map under names no class declares, so a quick save keeps them with
	// the object, a guest's getfield never meets them, and an object restored
	// from a quick save written before they existed has neither — which reads
	// as the state a new Graphics starts in: XOR off and the solid stroke.
	xorModeField     = "host:graphics-xor-mode"
	strokeStyleField = "host:graphics-stroke-style"
)

// The stroke styles the specification names, with the numbers MIDP gives them.
const (
	StrokeSolid  int32 = 0
	StrokeDotted int32 = 1
)

// anchorTopLeft is the MIDP anchor of a picture drawn from its corner.
const anchorTopLeft int32 = 16 | 4

// isXORMode and setXORMode keep the mode in the object, as KTF keeps it in the
// Graphics it hands out; reading it is what every drawing call below does.
func isXORMode(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(boolInt(graphicsXORMode(graphics))), nil
}

func setXORMode(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	values, err := intArguments(arguments, 1, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	graphics.SetFieldValue(xorModeField, jvm.IntValue(boolInt(values[0] != 0)))
	return jvm.VoidValue(), nil
}

func graphicsXORMode(graphics *jvm.Object) bool {
	value, ok := graphics.FieldValue(xorModeField)
	if !ok {
		return false
	}
	on, err := value.Int32()
	return err == nil && on != 0
}

// getStrokeStyle and setStrokeStyle keep the style and draw with neither.
//
// **The style is kept and not applied**, which is KTF's answer: it stores
// whatever it is handed and draws every line solid. The specification has
// DOTTED draw a point and skip the next along lines and arcs, and neither
// platform here that serves the call draws it so. A title whose dotted outline
// comes out solid is the evidence that would add it to both at once.
func getStrokeStyle(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, ok := graphics.FieldValue(strokeStyleField)
	if !ok {
		return jvm.IntValue(StrokeSolid), nil
	}
	style, err := value.Int32()
	if err != nil {
		return jvm.IntValue(StrokeSolid), nil
	}
	return jvm.IntValue(style), nil
}

func setStrokeStyle(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	values, err := intArguments(arguments, 1, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	graphics.SetFieldValue(strokeStyleField, jvm.IntValue(values[0]))
	return jvm.VoidValue(), nil
}

// resetGraphics is reset: the platform's, which puts back the clip, the
// translation and the font, and then the two states this layer keeps — XOR
// off and the solid stroke — which KTF's reset puts back as well. A state the
// Graphics never had is left unwritten, which reads the same.
func resetGraphics(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeSpecial(graphics, midp.GraphicsClass, "reset", "()V"); err != nil {
		return jvm.VoidValue(), err
	}
	StartPaint(graphics)
	return jvm.VoidValue(), nil
}

// StartPaint puts a screen Graphics the platform reuses back to the two states
// a new one has, XOR mode off and the solid stroke, so a paint does not
// inherit them from the one before. KTF hands every paint a new Graphics; SKT
// keeps one for the session and puts its clip, translation and font back at
// the start of each paint, and calls this beside them. A state the Graphics
// never had is left unwritten, which reads the same.
func StartPaint(graphics *jvm.Object) {
	if graphics == nil {
		return
	}
	for _, state := range []struct {
		field   string
		initial int32
	}{{xorModeField, 0}, {strokeStyleField, StrokeSolid}} {
		if _, ok := graphics.FieldValue(state.field); ok {
			graphics.SetFieldValue(state.field, jvm.IntValue(state.initial))
		}
	}
}

// colorComponent answers one eight-bit part of the colour as it was set: 16
// for red, 8 for green and 0 for blue, out of the same word getColor answers.
func colorComponent(shift uint) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		graphics, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		color, err := invokeInt(call, graphics, midp.GraphicsClass, "getColor")
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(int32(uint32(color) >> shift & 0xff)), nil
	}
}

// getPixel reads one pixel as 0x00RRGGBB, through the translation and not the
// clip, and answers 0 for a point off the surface — KTF's answer, and the one
// the WIPI read beside it gives every pixel it hands out.
func getPixel(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	point, err := intArguments(arguments, 1, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y := int64(point[0]), int64(point[1])
	pixels, err := readArea(call, graphics, area{x, y, x + 1, y + 1})
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(pixels[0])), nil
}

// setPixel is a one-pixel fill in the current colour: the clip, the
// translation and the XOR mode apply to it as they apply to a fill, which is
// what KTF draws it as.
func setPixel(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	point, err := intArguments(arguments, 1, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if graphicsXORMode(graphics) {
		return jvm.VoidValue(), xorFill(call, graphics, int64(point[0]), int64(point[1]), 1, 1)
	}
	_, err = call.InvokeSpecial(graphics, midp.GraphicsClass, "fillRect", "(IIII)V",
		jvm.IntValue(point[0]), jvm.IntValue(point[1]), jvm.IntValue(1), jvm.IntValue(1))
	return jvm.VoidValue(), err
}

// copyArea copies a rectangle inside the surface the Graphics draws on.
//
// **The destination comes first: `copyArea(dx, dy, sx, sy, w, h)`**, which is
// the specification's order and KTF's, and not MIDP's, which names the source
// first and ends with an anchor. The destination is clipped and translated,
// and the source moves with it, so a whole-surface shift that starts off the
// edge still copies the right column into the visible one. The source is read
// whole before anything is written, so a copy that overlaps itself reads what
// the surface held before it began; in XOR mode it draws the difference.
//
// A source pixel left of or above the surface is not copied, and the pixel it
// would have filled keeps what it held, as on KTF. One right of or below the
// surface reads as black: the read this is built on answers 0 for a point off
// the surface, which a layer above it cannot tell from a black pixel.
func copyArea(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	values, err := intArguments(arguments, 1, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	destinationX, destinationY := int64(values[0]), int64(values[1])
	sourceX, sourceY := int64(values[2]), int64(values[3])
	width, height := int64(values[4]), int64(values[5])
	if width <= 0 || height <= 0 {
		return jvm.VoidValue(), nil
	}
	view, err := surfaceOf(call, graphics)
	if err != nil {
		return jvm.VoidValue(), err
	}
	shiftX, shiftY := sourceX-destinationX, sourceY-destinationY
	destination := areaOf(destinationX, destinationY, width, height).intersect(view.clip)
	// Every source pixel at or past the surface's corner: a source x is the
	// destination x plus the shift, and it is on the surface from where it plus
	// the translation is zero.
	destination = destination.intersect(area{
		left: -int64(view.translateX) - shiftX, top: -int64(view.translateY) - shiftY,
		right: math.MaxInt64, bottom: math.MaxInt64,
	})
	source := destination.shift(shiftX, shiftY).intersect(addressable)
	destination = source.shift(-shiftX, -shiftY)
	if destination.empty() {
		return jvm.VoidValue(), nil
	}
	pixels, err := readArea(call, graphics, source)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if graphicsXORMode(graphics) {
		current, err := readArea(call, graphics, destination)
		if err != nil {
			return jvm.VoidValue(), err
		}
		for index := range pixels {
			pixels[index] ^= current[index]
		}
	}
	return jvm.VoidValue(), writeArea(call, graphics, destination, pixels)
}

// drawPolygon outlines a closed polygon: a line from each vertex to the next
// and from the last back to the first, over the vertices both arrays hold, as
// KTF draws it. Fewer than two vertices draw nothing. A null array is
// NullPointerException, where KTF answers with a platform error no guest
// handler can catch.
//
// In XOR mode the outline is one shape, so a vertex two edges share is
// inverted once. KTF inverts each edge on its own, which inverts a shared
// vertex twice and leaves it as it was.
func drawPolygon(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 3 {
		return jvm.VoidValue(), fmt.Errorf("Graphics.drawPolygon expected two coordinate arrays, got %d arguments", len(arguments)-1)
	}
	xArray, xLength, err := intArrayArgument(arguments[1], "x")
	if err != nil {
		return jvm.VoidValue(), err
	}
	yArray, yLength, err := intArrayArgument(arguments[2], "y")
	if err != nil {
		return jvm.VoidValue(), err
	}
	count := min(xLength, yLength)
	if count < 2 {
		return jvm.VoidValue(), nil
	}
	xs, err := intArrayValues(xArray, count)
	if err != nil {
		return jvm.VoidValue(), err
	}
	ys, err := intArrayValues(yArray, count)
	if err != nil {
		return jvm.VoidValue(), err
	}
	outline := func(target *jvm.Object) error {
		for index := 0; index < count; index++ {
			next := (index + 1) % count
			if _, err := call.InvokeSpecial(target, midp.GraphicsClass, "drawLine", "(IIII)V",
				jvm.IntValue(xs[index]), jvm.IntValue(ys[index]), jvm.IntValue(xs[next]), jvm.IntValue(ys[next])); err != nil {
				return err
			}
		}
		return nil
	}
	if !graphicsXORMode(graphics) {
		return jvm.VoidValue(), outline(graphics)
	}
	bounds := area{left: math.MaxInt64, top: math.MaxInt64, right: math.MinInt64, bottom: math.MinInt64}
	for index := 0; index < count; index++ {
		x, y := int64(xs[index]), int64(ys[index])
		bounds.left, bounds.right = min(bounds.left, x), max(bounds.right, x+1)
		bounds.top, bounds.bottom = min(bounds.top, y), max(bounds.bottom, y+1)
	}
	return jvm.VoidValue(), xorShape(call, graphics, bounds, inkPasses, outline)
}

// intArrayArgument checks one of drawPolygon's coordinate arrays and answers
// its length.
func intArrayArgument(value jvm.Value, name string) (*jvm.Object, int, error) {
	array, err := value.Reference()
	if err != nil {
		return nil, 0, err
	}
	if array == nil {
		return nil, 0, jvm.Throw("java/lang/NullPointerException", "Graphics.drawPolygon "+name+" array is null")
	}
	component, length, ok := jvm.ArrayComponent(array)
	if !ok || component.Kind != jvm.TypeInt {
		return nil, 0, fmt.Errorf("Graphics.drawPolygon %s argument is not an int array", name)
	}
	return array, length, nil
}

// intArrayValues reads the first count elements of an int array.
func intArrayValues(array *jvm.Object, count int) ([]int32, error) {
	values, err := jvm.ArrayRange(array, 0, count)
	if err != nil {
		return nil, err
	}
	result := make([]int32, len(values))
	for index, element := range values {
		if result[index], err = element.Int32(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// imageCopy is the WIPI `Image.createImage(Image)`: a new image the size of
// the source, with the source drawn into it.
//
// **The copy is mutable, and what the source left undrawn is white in it**,
// which is what the specification says of this call in as many words: an image
// is copied to be changed, only the copy can be drawn on through getGraphics,
// and a masked image's mask turns white in the copy, which keeps no mask. So
// the copy starts as the white surface a new mutable image is and the source
// is drawn over it. KTF's copy is mutable too and keeps the source's
// transparency, which the specification's own words do not leave room for.
// MIDP's call of the same name, which this one is not, makes an immutable
// image.
func imageCopy(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage expected a source image, got %d arguments", len(arguments))
	}
	source, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if source == nil {
		return jvm.VoidValue(), jvm.Throw("java/lang/NullPointerException", "Image.createImage source is null")
	}
	width, err := invokeInt(call, source, midp.ImageClass, "getWidth")
	if err != nil {
		return jvm.VoidValue(), err
	}
	height, err := invokeInt(call, source, midp.ImageClass, "getHeight")
	if err != nil {
		return jvm.VoidValue(), err
	}
	created, err := call.InvokeStatic(ImageClass, "createImage", "(II)Lorg/kwis/msp/lcdui/Image;", jvm.IntValue(width), jvm.IntValue(height))
	if err != nil {
		return jvm.VoidValue(), err
	}
	copied, err := created.Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	drawing, err := call.InvokeSpecial(copied, midp.ImageClass, "getGraphics", "()Ljavax/microedition/lcdui/Graphics;")
	if err != nil {
		return jvm.VoidValue(), err
	}
	graphics, err := drawing.Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeSpecial(graphics, midp.GraphicsClass, "drawImage", "(Ljavax/microedition/lcdui/Image;III)V",
		jvm.ReferenceValue(source), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(anchorTopLeft)); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(copied), nil
}

// XOR mode
//
// **XOR is a mode of every drawing call** — the specification's words are that
// what is drawn is XORed with what the screen holds, and KTF applies it to
// every fill, line, outline, curve, text run, picture and copy. A WIPI call
// that MIDP also has is declared again on this class for that reason: it
// forwards to the MIDP one as it is, and in XOR mode it draws through
// xorShape instead.
//
// The shape is drawn by the platform's own call onto a scratch image the size
// of the part of the surface it can reach, over a ground it cannot be confused
// with, and every pixel it covered becomes the XOR of the surface and what was
// drawn there. That keeps the platform's own geometry, glyphs, anchors and
// argument checks, and nothing half-drawn is ever on the surface a frame is
// taken from.
//
// What the scratch shows is which pixels a call covered, not how often it
// wrote each one, so a pixel two strokes of one call share is inverted once.
// KTF inverts per write, which leaves the corners of an XOR rectangle, the
// shared vertices of a polygon and the overlaps of a curve as they were; the
// specification describes the drawn content being XORed, which is what this
// is.

// The two kinds of drawing call: one that writes only the current colour, and
// one that draws a picture, whose pixels and transparency have to be read back
// over two grounds.
const (
	inkPasses     = 1
	picturePasses = 2
)

// xorAware is a WIPI drawing call over the MIDP one of the same name, given
// by its MIDP descriptor: outside XOR mode it is that call as it is, and in
// XOR mode the same call drawn through xorShape, inside the box bounds gives.
func xorAware(name, descriptor string, passes int, bounds func(*jvm.Invocation, *jvm.Object, []jvm.Value) (area, error)) jvm.ContextMethod {
	forward := forwardSpecial(midp.GraphicsClass, name, descriptor)
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		graphics, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if !graphicsXORMode(graphics) {
			return forward(call, arguments)
		}
		reach, err := bounds(call, graphics, arguments[1:])
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), xorShape(call, graphics, reach, passes, func(scratch *jvm.Object) error {
			_, err := call.InvokeSpecial(scratch, midp.GraphicsClass, name, descriptor, arguments[1:]...)
			return err
		})
	}
}

// xorFillRect is fillRect, which in XOR mode needs no scratch: what it covers
// is its rectangle.
func xorFillRect(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !graphicsXORMode(graphics) {
		return call.InvokeSpecial(graphics, midp.GraphicsClass, "fillRect", "(IIII)V", arguments[1:]...)
	}
	rect, err := intArguments(arguments, 1, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), xorFill(call, graphics, int64(rect[0]), int64(rect[1]), int64(rect[2]), int64(rect[3]))
}

// drawImageRegion is WIPI's `drawImage(image, dx, dy, sx, sy, w, h, anchor)`:
// the rectangle of the image at (sx, sy) drawn at (dx, dy). It is MIDP's
// drawRegion with the two halves swapped and no transform, which is how this
// platform has always answered it, and the anchor is not read — the picture
// is drawn from its corner, as it always was.
//
// In XOR mode it is a picture like any other, drawn through xorShape. The
// call is first made on the Graphics itself at a point where nothing it draws
// can land, because one check a scratch cannot make is the platform's refusal
// of a picture drawn onto itself.
func drawImageRegion(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	graphics, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 9 {
		return jvm.VoidValue(), fmt.Errorf("WIPI drawImage takes an image, a destination, a source rectangle and an anchor, got %d arguments", len(arguments)-1)
	}
	values, err := intArguments(arguments, 2, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	draw := func(target *jvm.Object, x, y int32) error {
		_, err := call.InvokeSpecial(target, midp.GraphicsClass, "drawRegion", "(Ljavax/microedition/lcdui/Image;IIIIIIII)V",
			arguments[1], arguments[4], arguments[5], arguments[6], arguments[7], jvm.IntValue(0),
			jvm.IntValue(x), jvm.IntValue(y), jvm.IntValue(0))
		return err
	}
	if !graphicsXORMode(graphics) {
		return jvm.VoidValue(), draw(graphics, values[0], values[1])
	}
	view, err := surfaceOf(call, graphics)
	if err != nil {
		return jvm.VoidValue(), err
	}
	// Left of the surface by the whole width: a corner the translation puts
	// that far left draws nothing whatever it is handed.
	if aside := -int64(view.translateX) - max(int64(values[4]), 0) - 1; aside >= math.MinInt32 {
		if err := draw(graphics, int32(aside), values[1]); err != nil {
			return jvm.VoidValue(), err
		}
	}
	bounds := areaOf(int64(values[0]), int64(values[1]), int64(values[4]), int64(values[5]))
	return jvm.VoidValue(), xorShape(call, graphics, bounds, picturePasses, func(target *jvm.Object) error {
		return draw(target, values[0], values[1])
	})
}

func xorFill(call *jvm.Invocation, graphics *jvm.Object, x, y, width, height int64) error {
	view, err := surfaceOf(call, graphics)
	if err != nil {
		return err
	}
	target := areaOf(x, y, width, height).intersect(view.clip)
	if target.empty() {
		return nil
	}
	color, err := invokeInt(call, graphics, midp.GraphicsClass, "getColor")
	if err != nil {
		return err
	}
	pixels, err := readArea(call, graphics, target)
	if err != nil {
		return err
	}
	for index := range pixels {
		pixels[index] ^= uint32(color) & 0xffffff
	}
	return writeArea(call, graphics, target, pixels)
}

// xorShape draws one call in XOR mode: draw is the call, made on a scratch
// Graphics whose coordinates are the caller's, and bounds is a box in the
// caller's coordinates that holds every pixel it can draw.
//
// A call whose box misses the clip draws nothing anywhere, so it is made on
// the Graphics itself: it changes no pixel, and the platform still checks its
// arguments — an anchor it refuses is refused in XOR mode as it is outside it.
func xorShape(call *jvm.Invocation, graphics *jvm.Object, bounds area, passes int, draw func(*jvm.Object) error) error {
	view, err := surfaceOf(call, graphics)
	if err != nil {
		return err
	}
	region := bounds.intersect(view.clip)
	if region.empty() {
		return draw(graphics)
	}
	color, err := invokeInt(call, graphics, midp.GraphicsClass, "getColor")
	if err != nil {
		return err
	}
	ink := uint32(color) & 0xffffff
	font, err := call.InvokeSpecial(graphics, midp.GraphicsClass, "getFont", "()Ljavax/microedition/lcdui/Font;")
	if err != nil {
		return err
	}
	picture, scratch, err := newScratch(call, region)
	if err != nil {
		return err
	}
	if _, err := call.InvokeSpecial(scratch, midp.GraphicsClass, "setFont", "(Ljavax/microedition/lcdui/Font;)V", font); err != nil {
		return err
	}
	// An ink call is read over the colour's complement, which it cannot
	// draw; a picture over black and over white, which between them give
	// each pixel's colour and how much of it was drawn.
	grounds := []uint32{ink ^ 0xffffff}
	if passes == picturePasses {
		grounds = []uint32{0x000000, 0xffffff}
	}
	width, height := region.right-region.left, region.bottom-region.top
	drawn := make([][]uint32, len(grounds))
	for index, ground := range grounds {
		if err := setColor(call, scratch, ground); err != nil {
			return err
		}
		if _, err := call.InvokeSpecial(scratch, midp.GraphicsClass, "fillRect", "(IIII)V",
			jvm.IntValue(int32(region.left)), jvm.IntValue(int32(region.top)), jvm.IntValue(int32(width)), jvm.IntValue(int32(height))); err != nil {
			return err
		}
		if err := setColor(call, scratch, ink); err != nil {
			return err
		}
		if err := draw(scratch); err != nil {
			return err
		}
		if drawn[index], err = readImage(call, picture, width, height); err != nil {
			return err
		}
	}
	pixels, err := readArea(call, graphics, region)
	if err != nil {
		return err
	}
	changed := false
	for index := range pixels {
		source, alpha := ink, uint32(0xff)
		if passes == picturePasses {
			var covered bool
			if source, alpha, covered = coverage(drawn[0][index], drawn[1][index]); !covered {
				continue
			}
		} else if drawn[0][index] != ink {
			continue
		}
		pixels[index] = xorBlend(pixels[index], source, alpha)
		changed = true
	}
	if !changed {
		return nil
	}
	return writeArea(call, graphics, region, pixels)
}

// newScratch makes the image an XOR call is drawn on first, and its Graphics,
// translated so that the caller's coordinates land on it where they land on
// the surface less the region's corner.
func newScratch(call *jvm.Invocation, region area) (*jvm.Object, *jvm.Object, error) {
	created, err := call.InvokeStatic(midp.ImageClass, "createImage", "(II)Ljavax/microedition/lcdui/Image;",
		jvm.IntValue(int32(region.right-region.left)), jvm.IntValue(int32(region.bottom-region.top)))
	if err != nil {
		return nil, nil, err
	}
	picture, err := created.Reference()
	if err != nil {
		return nil, nil, err
	}
	value, err := call.InvokeSpecial(picture, midp.ImageClass, "getGraphics", "()Ljavax/microedition/lcdui/Graphics;")
	if err != nil {
		return nil, nil, err
	}
	scratch, err := value.Reference()
	if err != nil {
		return nil, nil, err
	}
	if _, err := call.InvokeSpecial(scratch, midp.GraphicsClass, "translate", "(II)V",
		jvm.IntValue(int32(-region.left)), jvm.IntValue(int32(-region.top))); err != nil {
		return nil, nil, err
	}
	return picture, scratch, nil
}

// coverage reads one pixel of a picture drawn over black and over white. Where
// the two agree the picture covered it outright; where they differ by the
// same amount in every channel it was blended in at the strength that amount
// leaves, and its own colour is what the black ground kept of it; where they
// differ by the whole range nothing was drawn.
func coverage(black, white uint32) (uint32, uint32, bool) {
	if black == white {
		return black, 0xff, true
	}
	gap := uint32(0)
	for shift := uint(0); shift <= 16; shift += 8 {
		low, high := black>>shift&0xff, white>>shift&0xff
		if high < low {
			return 0, 0, false
		}
		gap = max(gap, high-low)
	}
	if gap >= 0xff {
		return 0, 0, false
	}
	alpha := 0xff - gap
	source := uint32(0)
	for shift := uint(0); shift <= 16; shift += 8 {
		level := min(0xff, ((black>>shift&0xff)*0xff+alpha/2)/alpha)
		source |= level << shift
	}
	return source, alpha, true
}

// xorBlend is what a covered pixel becomes: the XOR of the surface and the
// colour drawn there, mixed into the surface by how much of the pixel was
// drawn — all of it for everything but the edge of a blended picture. KTF
// mixes a glyph's edge the same way.
func xorBlend(destination, source, alpha uint32) uint32 {
	inverted := (destination ^ source) & 0xffffff
	if alpha >= 0xff {
		return inverted
	}
	result := uint32(0)
	for shift := uint(0); shift <= 16; shift += 8 {
		under, over := destination>>shift&0xff, inverted>>shift&0xff
		result |= ((over*alpha + under*(0xff-alpha) + 127) / 0xff) << shift
	}
	return result
}

// The boxes each XOR call can draw in, in the caller's coordinates. They hold
// every pixel the call can reach and may hold more, which costs only the
// scratch's size.

func lineBounds(_ *jvm.Invocation, _ *jvm.Object, arguments []jvm.Value) (area, error) {
	values, err := intArguments(arguments, 0, 4)
	if err != nil {
		return area{}, err
	}
	x1, y1, x2, y2 := int64(values[0]), int64(values[1]), int64(values[2]), int64(values[3])
	return area{min(x1, x2), min(y1, y2), max(x1, x2) + 1, max(y1, y2) + 1}, nil
}

// rectBounds is drawRect's: MIDP's outline covers one pixel more than its
// width and height, and a negative size draws nothing.
func rectBounds(_ *jvm.Invocation, _ *jvm.Object, arguments []jvm.Value) (area, error) {
	values, err := intArguments(arguments, 0, 4)
	if err != nil {
		return area{}, err
	}
	if values[2] < 0 || values[3] < 0 {
		return area{}, nil
	}
	x, y := int64(values[0]), int64(values[1])
	return area{x, y, x + int64(values[2]) + 1, y + int64(values[3]) + 1}, nil
}

// curveBounds is the box of the arc and rounded-rectangle calls with a pixel
// to spare on every side: a curve is centred in its box and rounds to a pixel
// at its right and bottom edge.
func curveBounds(_ *jvm.Invocation, _ *jvm.Object, arguments []jvm.Value) (area, error) {
	values, err := intArguments(arguments, 0, 6)
	if err != nil {
		return area{}, err
	}
	if values[2] <= 0 || values[3] <= 0 {
		return area{}, nil
	}
	x, y := int64(values[0]), int64(values[1])
	return area{x - 1, y - 1, x + int64(values[2]) + 2, y + int64(values[3]) + 2}, nil
}

// textBounds is the box of a text call, from the width the font measures it
// at. Every anchor puts the text within its width of x and within its height
// of y, and one more height on every side takes in what a glyph draws past
// its advance: a slant, a bold column, an underline. The width is measured by
// the font call that checks the same arguments the drawing call does, so a
// bad range is the same exception either way.
func textBounds(measure func(*jvm.Invocation, *jvm.Object, []jvm.Value) (int32, error), position int) func(*jvm.Invocation, *jvm.Object, []jvm.Value) (area, error) {
	return func(call *jvm.Invocation, graphics *jvm.Object, arguments []jvm.Value) (area, error) {
		value, err := call.InvokeSpecial(graphics, midp.GraphicsClass, "getFont", "()Ljavax/microedition/lcdui/Font;")
		if err != nil {
			return area{}, err
		}
		font, err := value.Reference()
		if err != nil {
			return area{}, err
		}
		width, err := measure(call, font, arguments)
		if err != nil {
			return area{}, err
		}
		height, err := invokeInt(call, font, midp.FontClass, "getHeight")
		if err != nil {
			return area{}, err
		}
		point, err := intArguments(arguments, position, 2)
		if err != nil {
			return area{}, err
		}
		x, y := int64(point[0]), int64(point[1])
		reach, margin := int64(max(width, 0)), int64(max(height, 0))+1
		return area{x - reach - margin, y - 2*margin, x + reach + margin, y + 2*margin}, nil
	}
}

func measureString(call *jvm.Invocation, font *jvm.Object, arguments []jvm.Value) (int32, error) {
	return invokeIntWith(call, font, midp.FontClass, "stringWidth", "(Ljava/lang/String;)I", arguments[:1]...)
}

func measureSubstring(call *jvm.Invocation, font *jvm.Object, arguments []jvm.Value) (int32, error) {
	return invokeIntWith(call, font, midp.FontClass, "substringWidth", "(Ljava/lang/String;II)I", arguments[:3]...)
}

func measureChars(call *jvm.Invocation, font *jvm.Object, arguments []jvm.Value) (int32, error) {
	return invokeIntWith(call, font, midp.FontClass, "charsWidth", "([CII)I", arguments[:3]...)
}

func measureChar(call *jvm.Invocation, font *jvm.Object, arguments []jvm.Value) (int32, error) {
	return invokeIntWith(call, font, midp.FontClass, "charWidth", "(C)I", arguments[:1]...)
}

// imageBounds is a picture's box under every anchor: within its width of x
// and its height of y.
func imageBounds(call *jvm.Invocation, _ *jvm.Object, arguments []jvm.Value) (area, error) {
	if len(arguments) < 3 {
		return area{}, fmt.Errorf("Graphics.drawImage expected an image and a position")
	}
	image, err := arguments[0].Reference()
	if err != nil {
		return area{}, err
	}
	if image == nil {
		return area{}, jvm.Throw("java/lang/NullPointerException", "Graphics.drawImage image is null")
	}
	width, err := invokeInt(call, image, midp.ImageClass, "getWidth")
	if err != nil {
		return area{}, err
	}
	height, err := invokeInt(call, image, midp.ImageClass, "getHeight")
	if err != nil {
		return area{}, err
	}
	point, err := intArguments(arguments, 1, 2)
	if err != nil {
		return area{}, err
	}
	x, y, w, h := int64(point[0]), int64(point[1]), int64(max(width, 0)), int64(max(height, 0))
	return area{x - w, y - h, x + w, y + h}, nil
}

// The surface, as the composed members see it

// area is a half-open rectangle in a Graphics' own translated coordinates,
// wide enough that no sum of a guest's coordinates and sizes wraps.
type area struct {
	left, top, right, bottom int64
}

// addressable is where a corner a platform call is handed can lie: inside an
// int, and one short of its least value, so a corner's negation is an int too.
// Every region the members here read or write is cut to it before a call
// names it, and to the clip, which keeps its size to the surface's.
var addressable = area{math.MinInt32 + 1, math.MinInt32 + 1, math.MaxInt32, math.MaxInt32}

func areaOf(x, y, width, height int64) area {
	if width <= 0 || height <= 0 {
		return area{}
	}
	return area{x, y, x + width, y + height}
}

func (a area) empty() bool {
	return a.right <= a.left || a.bottom <= a.top
}

func (a area) intersect(other area) area {
	result := area{max(a.left, other.left), max(a.top, other.top), min(a.right, other.right), min(a.bottom, other.bottom)}
	if result.empty() {
		return area{}
	}
	return result
}

func (a area) shift(x, y int64) area {
	if a.empty() {
		return area{}
	}
	return area{a.left + x, a.top + y, a.right + x, a.bottom + y}
}

// surface is what the composed members read off a Graphics before they touch
// its pixels: its clip, in its own coordinates and cut to what a call can
// name, and its translation.
type surface struct {
	clip                   area
	translateX, translateY int32
}

// surfaceOf reads a Graphics' clip and translation.
//
// The clip getters answer in the caller's coordinates as an int, which an
// extreme translation carries past an int's range and wraps. The corner they
// stand for on the surface is small, so adding the translation back with the
// same wrap recovers it exactly, and the clip is placed from there.
func surfaceOf(call *jvm.Invocation, graphics *jvm.Object) (surface, error) {
	var values [6]int32
	for index, name := range []string{"getTranslateX", "getTranslateY", "getClipX", "getClipY", "getClipWidth", "getClipHeight"} {
		value, err := invokeInt(call, graphics, midp.GraphicsClass, name)
		if err != nil {
			return surface{}, err
		}
		values[index] = value
	}
	translateX, translateY := values[0], values[1]
	left := int64(int32(uint32(values[2])+uint32(translateX))) - int64(translateX)
	top := int64(int32(uint32(values[3])+uint32(translateY))) - int64(translateY)
	clip := areaOf(left, top, int64(values[4]), int64(values[5])).intersect(addressable)
	return surface{clip: clip, translateX: translateX, translateY: translateY}, nil
}

// readArea reads a region of the surface as 0x00RRGGBB pixels through the WIPI
// read, which takes the translation and not the clip. A part of the region a
// call cannot name is off every surface and reads as 0, as the read answers
// for a point off the surface.
func readArea(call *jvm.Invocation, graphics *jvm.Object, region area) ([]uint32, error) {
	width, height := region.right-region.left, region.bottom-region.top
	if region.empty() || width*height > math.MaxInt32 {
		return nil, fmt.Errorf("Graphics region %+v cannot be read", region)
	}
	pixels := make([]uint32, width*height)
	readable := region.intersect(addressable)
	if readable.empty() {
		return pixels, nil
	}
	readWidth, readHeight := readable.right-readable.left, readable.bottom-readable.top
	array, err := call.VM().NewArray(jvm.Type{Kind: jvm.TypeInt}, int32(readWidth*readHeight))
	if err != nil {
		return nil, err
	}
	if _, err := call.InvokeSpecial(graphics, GraphicsClass, "getRGBPixels", "(IIII[III)V",
		jvm.IntValue(int32(readable.left)), jvm.IntValue(int32(readable.top)),
		jvm.IntValue(int32(readWidth)), jvm.IntValue(int32(readHeight)),
		jvm.ReferenceValue(array), jvm.IntValue(0), jvm.IntValue(int32(readWidth))); err != nil {
		return nil, err
	}
	values, err := jvm.ArrayRange(array, 0, int(readWidth*readHeight))
	if err != nil {
		return nil, err
	}
	for row := int64(0); row < readHeight; row++ {
		for column := int64(0); column < readWidth; column++ {
			pixel, err := values[row*readWidth+column].Int32()
			if err != nil {
				return nil, err
			}
			pixels[(readable.top-region.top+row)*width+readable.left-region.left+column] = uint32(pixel) & 0xffffff
		}
	}
	return pixels, nil
}

// writeArea writes a region of 0x00RRGGBB pixels through the WIPI write, which
// takes the translation and the clip. Every region written here lies inside
// the clip already.
func writeArea(call *jvm.Invocation, graphics *jvm.Object, region area, pixels []uint32) error {
	width, height := region.right-region.left, region.bottom-region.top
	if region.empty() || int64(len(pixels)) != width*height {
		return fmt.Errorf("Graphics region %+v does not hold %d pixels", region, len(pixels))
	}
	array, err := call.VM().NewArray(jvm.Type{Kind: jvm.TypeInt}, int32(len(pixels)))
	if err != nil {
		return err
	}
	values := make([]jvm.Value, len(pixels))
	for index, pixel := range pixels {
		values[index] = jvm.IntValue(int32(pixel))
	}
	if err := jvm.SetArrayRange(array, 0, values); err != nil {
		return err
	}
	_, err = call.InvokeSpecial(graphics, GraphicsClass, "setRGBPixels", "(IIII[III)V",
		jvm.IntValue(int32(region.left)), jvm.IntValue(int32(region.top)),
		jvm.IntValue(int32(width)), jvm.IntValue(int32(height)),
		jvm.ReferenceValue(array), jvm.IntValue(0), jvm.IntValue(int32(width)))
	return err
}

// readImage reads a whole image as 0x00RRGGBB pixels through MIDP's getRGB.
func readImage(call *jvm.Invocation, picture *jvm.Object, width, height int64) ([]uint32, error) {
	array, err := call.VM().NewArray(jvm.Type{Kind: jvm.TypeInt}, int32(width*height))
	if err != nil {
		return nil, err
	}
	if _, err := call.InvokeSpecial(picture, midp.ImageClass, "getRGB", "([IIIIIII)V",
		jvm.ReferenceValue(array), jvm.IntValue(0), jvm.IntValue(int32(width)),
		jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(int32(width)), jvm.IntValue(int32(height))); err != nil {
		return nil, err
	}
	values, err := jvm.ArrayRange(array, 0, int(width*height))
	if err != nil {
		return nil, err
	}
	pixels := make([]uint32, len(values))
	for index, value := range values {
		pixel, err := value.Int32()
		if err != nil {
			return nil, err
		}
		pixels[index] = uint32(pixel) & 0xffffff
	}
	return pixels, nil
}

func setColor(call *jvm.Invocation, graphics *jvm.Object, rgb uint32) error {
	_, err := call.InvokeSpecial(graphics, midp.GraphicsClass, "setColor", "(I)V", jvm.IntValue(int32(rgb)))
	return err
}

func invokeInt(call *jvm.Invocation, object *jvm.Object, className, name string) (int32, error) {
	return invokeIntWith(call, object, className, name, "()I")
}

func invokeIntWith(call *jvm.Invocation, object *jvm.Object, className, name, descriptor string, arguments ...jvm.Value) (int32, error) {
	value, err := call.InvokeSpecial(object, className, name, descriptor, arguments...)
	if err != nil {
		return 0, err
	}
	return value.Int32()
}

func intArguments(arguments []jvm.Value, first, count int) ([]int32, error) {
	if first < 0 || len(arguments) < first+count {
		return nil, fmt.Errorf("expected %d integer arguments, got %d", count, max(len(arguments)-first, 0))
	}
	values := make([]int32, count)
	for index := range values {
		value, err := arguments[first+index].Int32()
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func boolInt(value bool) int32 {
	if value {
		return 1
	}
	return 0
}
