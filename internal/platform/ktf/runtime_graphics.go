package ktf

import (
	"encoding/binary"
	"fmt"
	stdimage "image"
	"sort"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// runtimeGraphicsState is the drawing state behind one org/kwis/msp/lcdui/
// Graphics object targeting the WIPI C screen framebuffer.
type runtimeGraphicsState struct {
	target wipicFramebuffer
	color  uint16
	rgb    uint32
	// The clip is kept in device coordinates so a translated clip and a
	// translated draw meet in the same space; the clip getters subtract the
	// translation again, which is what MIDP-shaped callers expect.
	clipX, clipY           int32
	clipWidth, clipHeight  int32
	translateX, translateY int32
	strokeStyle            int32
	xorMode                bool
	alpha                  int32
}

// newScreenGraphics builds a Graphics object whose operations draw into the
// shared screen framebuffer.
func (runtime *initializationRuntime) newScreenGraphics() (*jvm.Object, error) {
	handle, err := runtime.wipicGetScreenFramebuffer()
	if err != nil {
		return nil, err
	}
	target, err := runtime.readWIPICFramebuffer(handle)
	if err != nil {
		return nil, err
	}
	state := &runtimeGraphicsState{
		target:     target,
		clipWidth:  int32(target.width),
		clipHeight: int32(target.height),
		alpha:      255,
	}
	graphics := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Graphics", Native: state, Fields: make(map[string]jvm.Value)}
	return graphics, nil
}

func runtimeGraphicsStateOf(arguments []jvm.Value) (*runtimeGraphicsState, error) {
	if len(arguments) < 1 {
		return nil, fmt.Errorf("Graphics method expected receiver")
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, fmt.Errorf("Graphics receiver is null")
	}
	state, _ := receiver.Native.(*runtimeGraphicsState)
	return state, nil
}

func runtimeGraphicsInts(arguments []jvm.Value, count int) ([]int32, error) {
	if len(arguments) < count+1 {
		return nil, fmt.Errorf("Graphics method expected %d arguments, got %d", count, len(arguments)-1)
	}
	values := make([]int32, count)
	for index := 0; index < count; index++ {
		value, err := arguments[index+1].Int32()
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

// clip converts a rectangle from the caller's translated coordinate space to
// device space and intersects it with the clip and the target. Every drawing
// operation funnels through it, which is what makes one translation
// assignment apply to all of them.
func (state *runtimeGraphicsState) clip(x, y, width, height int32) (int32, int32, int32, int32, bool) {
	x += state.translateX
	y += state.translateY
	left, top := x, y
	right := int64(x) + int64(width)
	bottom := int64(y) + int64(height)
	if left < state.clipX {
		left = state.clipX
	}
	if top < state.clipY {
		top = state.clipY
	}
	clipRight := int64(state.clipX) + int64(state.clipWidth)
	clipBottom := int64(state.clipY) + int64(state.clipHeight)
	if right > clipRight {
		right = clipRight
	}
	if bottom > clipBottom {
		bottom = clipBottom
	}
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	if right > int64(state.target.width) {
		right = int64(state.target.width)
	}
	if bottom > int64(state.target.height) {
		bottom = int64(state.target.height)
	}
	if int64(left) >= right || int64(top) >= bottom {
		return 0, 0, 0, 0, false
	}
	return left, top, int32(right - int64(left)), int32(bottom - int64(top)), true
}

func (runtime *initializationRuntime) graphicsFillRect(state *runtimeGraphicsState, x, y, width, height int32, pixel uint16) error {
	left, top, clippedWidth, clippedHeight, ok := state.clip(x, y, width, height)
	if !ok {
		return nil
	}
	row := make([]byte, int(clippedWidth)*2)
	for column := int32(0); column < clippedWidth; column++ {
		binary.LittleEndian.PutUint16(row[column*2:], pixel)
	}
	memory := runtime.client.core.Memory()
	// XOR mode draws the difference between the colour and what is already
	// there, so the destination has to be read back a row at a time.
	var target []byte
	if state.xorMode {
		target = make([]byte, len(row))
	}
	for line := int32(0); line < clippedHeight; line++ {
		address := state.target.pixels + uint32(top+line)*state.target.bpl + uint32(left)*2
		written := row
		if state.xorMode {
			if err := memory.Read(address, target); err != nil {
				return err
			}
			for column := 0; column < len(target); column += 2 {
				binary.LittleEndian.PutUint16(target[column:], binary.LittleEndian.Uint16(target[column:])^pixel)
			}
			written = target
		}
		if err := memory.Write(address, written); err != nil {
			return err
		}
	}
	return nil
}

// blend565 mixes a source pixel into a destination one by coverage, in the
// 5/6/5 space both already live in. Full coverage is the source untouched, so
// a glyph that lands on whole pixels — which every glyph on the 16-dot grid
// does — comes out of this exactly as it did when text was plotted rather than
// blended.
func blend565(destination, source uint16, coverage uint8) uint16 {
	if coverage == 0xff {
		return source
	}
	mix := func(from, to uint32) uint32 {
		return (from*uint32(0xff-coverage) + to*uint32(coverage) + 0x7f) / 0xff
	}
	red := mix(uint32(destination>>11&0x1f), uint32(source>>11&0x1f))
	green := mix(uint32(destination>>5&0x3f), uint32(source>>5&0x3f))
	blue := mix(uint32(destination&0x1f), uint32(source&0x1f))
	return uint16(red<<11 | green<<5 | blue)
}

// graphicsBlendPixel draws one glyph pixel, mixing the current colour into
// whatever is already there by how much of the pixel the outline covers. The
// clip and the translation are the same ones every other operation goes
// through; only the write differs.
func (runtime *initializationRuntime) graphicsBlendPixel(state *runtimeGraphicsState, x, y int32, coverage uint8) error {
	if coverage == 0 {
		return nil
	}
	if coverage == 0xff {
		return runtime.graphicsFillRect(state, x, y, 1, 1, state.color)
	}
	left, top, _, _, ok := state.clip(x, y, 1, 1)
	if !ok {
		return nil
	}
	memory := runtime.client.core.Memory()
	address := state.target.pixels + uint32(top)*state.target.bpl + uint32(left)*2
	var pixel [2]byte
	if err := memory.Read(address, pixel[:]); err != nil {
		return err
	}
	destination := binary.LittleEndian.Uint16(pixel[:])
	source := state.color
	if state.xorMode {
		source = destination ^ state.color
	}
	binary.LittleEndian.PutUint16(pixel[:], blend565(destination, source, coverage))
	return memory.Write(address, pixel[:])
}

func runtimeGraphicsSetColor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, len(arguments)-1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	var rgb uint32
	if len(values) == 1 {
		rgb = uint32(values[0])
	} else if len(values) == 3 {
		rgb = uint32(values[0]&0xff)<<16 | uint32(values[1]&0xff)<<8 | uint32(values[2]&0xff)
	}
	state.rgb = rgb & 0xffffff
	state.color = uint16(rgb>>16&0xff)>>3<<11 | uint16(rgb>>8&0xff)>>2<<5 | uint16(rgb&0xff)>>3
	return jvm.VoidValue(), nil
}

func runtimeGraphicsGetColor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.IntValue(0), err
	}
	return jvm.IntValue(int32(state.rgb)), nil
}

func runtimeGraphicsFillRect(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsFillRect(state, values[0], values[1], values[2], values[3], state.color)
}

func runtimeGraphicsDrawRect(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsDrawRectOutline(state, values[0], values[1], values[2], values[3])
}

// graphicsDrawRectOutline draws the four edges of a rectangle in the current
// color. Arc and round-rectangle outlines approximate with it, the outline
// counterpart of the filled approximation.
func (runtime *initializationRuntime) graphicsDrawRectOutline(state *runtimeGraphicsState, x, y, width, height int32) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	if err := runtime.graphicsFillRect(state, x, y, width, 1, state.color); err != nil {
		return err
	}
	if err := runtime.graphicsFillRect(state, x, y+height-1, width, 1, state.color); err != nil {
		return err
	}
	if err := runtime.graphicsFillRect(state, x, y, 1, height, state.color); err != nil {
		return err
	}
	return runtime.graphicsFillRect(state, x+width-1, y, 1, height, state.color)
}

// runtimeGraphicsDrawArc plots the arc's own curve, which is what MIDP's
// drawArc is: the pie's two straight edges are not part of it.
func runtimeGraphicsDrawArc(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsDrawArc(state, values[0], values[1], values[2], values[3], values[4], values[5])
}

// runtimeGraphicsDrawRoundRect outlines a rectangle with rounded corners.
func runtimeGraphicsDrawRoundRect(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsDrawRoundRect(state, values[0], values[1], values[2], values[3], values[4], values[5])
}

func runtimeGraphicsDrawLine(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsDrawSegment(state, values[0], values[1], values[2], values[3])
}

// graphicsTextWidth mirrors the runtime font advance used by Font metrics.
func (runtime *initializationRuntime) graphicsTextWidth(text []rune) int32 {
	face := runtime.fontFace()
	width := int32(0)
	for _, character := range text {
		width += int32(face.Render(character).Advance)
	}
	return width
}

func (runtime *initializationRuntime) graphicsCharAdvance(character rune) int32 {
	return int32(runtime.fontFace().Render(character).Advance)
}

// runtimeGraphicsDrawString renders text with the runtime-owned 5x7 bitmap
// glyphs in the current color. The anchor places the text box; baseline
// anchoring uses the shared 8-pixel baseline.
func runtimeGraphicsDrawString(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 5 {
		return jvm.VoidValue(), fmt.Errorf("Graphics.drawString expected text, position, and anchor")
	}
	textObject, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, _ := jvm.StringText(textObject)
	positions, err := runtimeGraphicsInts(arguments[1:], 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsDrawText(state, []rune(value), positions[0], positions[1], positions[2])
}

func (runtime *initializationRuntime) graphicsDrawText(state *runtimeGraphicsState, text []rune, x, y, anchor int32) error {
	if len(text) == 0 {
		return nil
	}
	face := runtime.fontFace()
	width := runtime.graphicsTextWidth(text)
	x, y = anchorOrigin(x, y, width, runtime.fontHeight(), anchor)
	if anchor&64 != 0 {
		y -= runtime.fontBaseline()
	}
	baseline := y + runtime.fontBaseline()
	cursor := x
	for _, character := range text {
		bitmap := face.Render(character)
		if character != ' ' && character != '\t' {
			for row := range bitmap.Rows {
				for column := 0; column < bitmap.Width; column++ {
					if err := runtime.graphicsBlendPixel(state,
						cursor+int32(column), baseline-int32(bitmap.Ascent)+int32(row),
						bitmap.Coverage(row, column)); err != nil {
						return err
					}
				}
			}
		}
		cursor += int32(bitmap.Advance)
	}
	return nil
}

// runtimeGraphicsFillArc fills the pie slice an arc cuts out of its bounding
// ellipse.
func runtimeGraphicsFillArc(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsFillArc(state, values[0], values[1], values[2], values[3], values[4], values[5])
}

// runtimeGraphicsFillRoundRect fills a rectangle with rounded corners. One
// title draws every character's ground shadow with it, at corner diameters
// that make the shape an ellipse.
func runtimeGraphicsFillRoundRect(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsFillRoundRect(state, values[0], values[1], values[2], values[3], values[4], values[5])
}

// runtimeGraphicsDrawChar renders one character with the runtime glyphs.
func runtimeGraphicsDrawChar(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsDrawText(state, []rune{rune(values[0])}, values[1], values[2], values[3])
}

// runtimeGraphicsSetGrayScale sets the color to an even gray level.
func runtimeGraphicsSetGrayScale(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	level := uint32(values[0] & 0xff)
	rgb := level<<16 | level<<8 | level
	state.rgb = rgb
	state.color = uint16(level)>>3<<11 | uint16(level)>>2<<5 | uint16(level)>>3
	return jvm.VoidValue(), nil
}

// runtimeGraphicsClipValue answers one of the clip getters: x, y, width, or
// height by index.
func runtimeGraphicsClipValue(which int) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		state, err := runtimeGraphicsStateOf(arguments)
		if err != nil || state == nil {
			return jvm.IntValue(0), err
		}
		// The clip is stored in device coordinates; a caller asks for it in
		// its own translated space.
		switch which {
		case 0:
			return jvm.IntValue(state.clipX - state.translateX), nil
		case 1:
			return jvm.IntValue(state.clipY - state.translateY), nil
		case 2:
			return jvm.IntValue(state.clipWidth), nil
		default:
			return jvm.IntValue(state.clipHeight), nil
		}
	}
}

func runtimeGraphicsSetClip(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state.clipX = values[0] + state.translateX
	state.clipY = values[1] + state.translateY
	state.clipWidth, state.clipHeight = values[2], values[3]
	return jvm.VoidValue(), nil
}

func runtimeGraphicsClipRect(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	left, top, width, height, ok := state.clip(values[0], values[1], values[2], values[3])
	if !ok {
		state.clipWidth, state.clipHeight = 0, 0
		return jvm.VoidValue(), nil
	}
	state.clipX, state.clipY, state.clipWidth, state.clipHeight = left, top, width, height
	return jvm.VoidValue(), nil
}

// Anchor bits follow the MIDP constants KTF shares: HCENTER 1, VCENTER 2,
// LEFT 4, RIGHT 8, TOP 16, BOTTOM 32.
func anchorOrigin(x, y, width, height, anchor int32) (int32, int32) {
	if anchor&1 != 0 {
		x -= width / 2
	} else if anchor&8 != 0 {
		x -= width
	}
	if anchor&2 != 0 {
		y -= height / 2
	} else if anchor&32 != 0 {
		y -= height
	}
	return x, y
}

// graphicsBlitFramebuffer copies a guest offscreen framebuffer into the
// graphics target with anchor and clip applied, row by row through guest
// memory.
func (runtime *initializationRuntime) graphicsBlitFramebuffer(state *runtimeGraphicsState, handle uint32, x, y, anchor int32, transparent *uint16) error {
	source, err := runtime.readWIPICFramebuffer(handle)
	if err != nil {
		return err
	}
	width, height := int32(source.width), int32(source.height)
	x, y = anchorOrigin(x, y, width, height, anchor)
	left, top, clippedWidth, clippedHeight, ok := state.clip(x, y, width, height)
	if !ok {
		return nil
	}
	// A blit is masked when the image declared a transparent colour or when its
	// encoding left pixels undrawn; either way the skipped pixels keep whatever
	// the target already holds.
	opacity := runtime.framebufferOpacityOf(handle)
	memory := runtime.client.core.Memory()
	row := make([]byte, int(clippedWidth)*2)
	target := make([]byte, int(clippedWidth)*2)
	for line := int32(0); line < clippedHeight; line++ {
		sourceY := top - y + line
		sourceAddress := source.pixels + uint32(sourceY)*source.bpl + uint32(left-x)*2
		if err := memory.Read(sourceAddress, row); err != nil {
			return fmt.Errorf("read KTF blit source row %d: %w", line, err)
		}
		targetAddress := state.target.pixels + uint32(top+line)*state.target.bpl + uint32(left)*2
		if transparent != nil || opacity != nil {
			if err := memory.Read(targetAddress, target); err != nil {
				return fmt.Errorf("read KTF blit target row %d: %w", line, err)
			}
			for column := 0; column < len(row); column += 2 {
				sourceX := left - x + int32(column/2)
				if (transparent != nil && binary.LittleEndian.Uint16(row[column:]) == *transparent) ||
					!opacity.opaqueAt(int(sourceX), int(sourceY)) {
					copy(row[column:column+2], target[column:column+2])
				}
			}
		}
		if err := memory.Write(targetAddress, row); err != nil {
			return fmt.Errorf("write KTF blit target row %d: %w", line, err)
		}
	}
	return nil
}

// imageTransparentPixel reports the target pixel an image declared
// transparent, or nil when it draws every pixel.
func imageTransparentPixel(image *jvm.Object) *uint16 {
	if image == nil {
		return nil
	}
	value, ok := image.Fields["transparentColor:I"]
	if !ok {
		return nil
	}
	rgb, err := value.Int32()
	if err != nil {
		return nil
	}
	pixel := pixel565FromRGB(uint32(rgb))
	return &pixel
}

// imageFramebufferHandle answers the guest framebuffer behind an image,
// rasterizing a decoded one into a new framebuffer on first use. Image
// composition works on those 16-bit pixels, so both kinds of image reach the
// same path.
func (runtime *initializationRuntime) imageFramebufferHandle(image *jvm.Object) (uint32, error) {
	if image == nil {
		return 0, fmt.Errorf("image is null")
	}
	if value, ok := image.Fields["guestFramebuffer:I"]; ok {
		handle, err := value.Int32()
		if err != nil {
			return 0, err
		}
		return uint32(handle), nil
	}
	decoded, _ := image.Native.(stdimage.Image)
	if decoded == nil {
		return 0, fmt.Errorf("image has no pixels")
	}
	bounds := decoded.Bounds()
	handle, err := runtime.newWIPICFramebufferRecord(uint32(bounds.Dx()), uint32(bounds.Dy()))
	if err != nil {
		return 0, err
	}
	framebuffer, err := runtime.readWIPICFramebuffer(handle)
	if err != nil {
		return 0, err
	}
	memory := runtime.client.core.Memory()
	row := make([]byte, bounds.Dx()*2)
	for line := 0; line < bounds.Dy(); line++ {
		for column := 0; column < bounds.Dx(); column++ {
			binary.LittleEndian.PutUint16(row[column*2:], imagePixel565(decoded, bounds.Min.X+column, bounds.Min.Y+line))
		}
		if err := memory.Write(framebuffer.pixels+uint32(line)*framebuffer.bpl, row); err != nil {
			return 0, err
		}
	}
	runtime.setFramebufferOpacity(handle, imageOpacityOf(decoded))
	image.Fields["guestFramebuffer:I"] = jvm.IntValue(int32(handle))
	return handle, nil
}

func runtimeGraphicsDrawImage(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 5 {
		return jvm.VoidValue(), fmt.Errorf("Graphics.drawImage expected image, position, and anchor")
	}
	imageObject, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if imageObject == nil {
		return jvm.VoidValue(), &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/NullPointerException", Native: "drawImage image"},
			Message: "drawImage image",
		}
	}
	positions, err := runtimeGraphicsInts(arguments[1:], 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if value, ok := imageObject.Fields["guestFramebuffer:I"]; ok {
		handle, err := value.Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), runtime.graphicsBlitFramebuffer(state, uint32(handle), positions[0], positions[1], positions[2], imageTransparentPixel(imageObject))
	}
	decoded, _ := imageObject.Native.(stdimage.Image)
	if decoded == nil {
		return jvm.VoidValue(), nil // sized mutable image without pixel data yet
	}
	transparent := imageTransparentPixel(imageObject)
	if transparent != nil || imageOpacityOf(decoded) != nil {
		// A masked draw needs the image's pixels in target format, so the
		// decoded image is rasterized once and drawn through the same blit
		// framebuffer-backed images use. An image whose encoding declared
		// transparent pixels is masked whether or not the game named a
		// transparent colour, which is what keeps a sprite from painting its
		// unused border over the scene.
		handle, err := runtime.imageFramebufferHandle(imageObject)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), runtime.graphicsBlitFramebuffer(state, handle, positions[0], positions[1], positions[2], transparent)
	}
	bounds := decoded.Bounds()
	width, height := int32(bounds.Dx()), int32(bounds.Dy())
	x, y := anchorOrigin(positions[0], positions[1], width, height, positions[2])
	left, top, clippedWidth, clippedHeight, ok := state.clip(x, y, width, height)
	if !ok {
		return jvm.VoidValue(), nil
	}
	memory := runtime.client.core.Memory()
	row := make([]byte, int(clippedWidth)*2)
	for line := int32(0); line < clippedHeight; line++ {
		sourceY := bounds.Min.Y + int(top-y+line)
		for column := int32(0); column < clippedWidth; column++ {
			sourceX := bounds.Min.X + int(left-x+column)
			red, green, blue, _ := decoded.At(sourceX, sourceY).RGBA()
			pixel := uint16(red>>11)<<11 | uint16(green>>10)<<5 | uint16(blue>>11)
			binary.LittleEndian.PutUint16(row[column*2:], pixel)
		}
		address := state.target.pixels + uint32(top+line)*state.target.bpl + uint32(left)*2
		if err := memory.Write(address, row); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), nil
}

// runtimeGraphicsTranslate moves the origin every later operation draws
// relative to, accumulating like the MIDP call it mirrors.
func runtimeGraphicsTranslate(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state.translateX += values[0]
	state.translateY += values[1]
	return jvm.VoidValue(), nil
}

// runtimeGraphicsTranslation answers one of the translation getters.
func runtimeGraphicsTranslation(vertical bool) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		state, err := runtimeGraphicsStateOf(arguments)
		if err != nil || state == nil {
			return jvm.IntValue(0), err
		}
		if vertical {
			return jvm.IntValue(state.translateY), nil
		}
		return jvm.IntValue(state.translateX), nil
	}
}

// runtimeGraphicsReset returns the context to its initial state: no
// translation, the whole target visible, opaque black, and no XOR mode.
func runtimeGraphicsReset(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	state.translateX, state.translateY = 0, 0
	state.clipX, state.clipY = 0, 0
	state.clipWidth, state.clipHeight = int32(state.target.width), int32(state.target.height)
	state.color, state.rgb = 0, 0
	state.strokeStyle = 0
	state.xorMode = false
	state.alpha = 255
	return jvm.VoidValue(), nil
}

// runtimeGraphicsColorComponent answers one 8-bit component of the current
// color: 16 for red, 8 for green, 0 for blue.
func runtimeGraphicsColorComponent(shift uint) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		state, err := runtimeGraphicsStateOf(arguments)
		if err != nil || state == nil {
			return jvm.IntValue(0), err
		}
		return jvm.IntValue(int32(state.rgb >> shift & 0xff)), nil
	}
}

func runtimeGraphicsGetAlpha(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.IntValue(255), err
	}
	return jvm.IntValue(state.alpha), nil
}

// runtimeGraphicsSetAlpha records the requested alpha. Blending is not
// implemented, so drawing stays opaque; the value is kept because a game that
// sets it reads it back.
func runtimeGraphicsSetAlpha(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state.alpha = values[0]
	return jvm.VoidValue(), nil
}

func runtimeGraphicsSetStrokeStyle(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state.strokeStyle = values[0]
	return jvm.VoidValue(), nil
}

func runtimeGraphicsGetStrokeStyle(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.IntValue(0), err
	}
	return jvm.IntValue(state.strokeStyle), nil
}

func runtimeGraphicsSetXORMode(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state.xorMode = values[0] != 0
	return jvm.VoidValue(), nil
}

func runtimeGraphicsIsXORMode(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.IntValue(0), err
	}
	if state.xorMode {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

// runtimeGraphicsDrawChars renders a range of a character array.
func runtimeGraphicsDrawChars(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 7 {
		return jvm.VoidValue(), fmt.Errorf("Graphics.drawChars expected characters, range, position, and anchor, got %d arguments", len(arguments)-1)
	}
	array, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil {
		return jvm.VoidValue(), fmt.Errorf("Graphics.drawChars characters are null")
	}
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		return jvm.VoidValue(), err
	}
	positions, err := runtimeGraphicsInts(arguments[1:], 5)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, length := positions[0], positions[1]
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(values)) {
		return jvm.VoidValue(), fmt.Errorf("Graphics.drawChars range [%d, %d) is out of bounds", offset, offset+length)
	}
	text := make([]rune, 0, length)
	for _, value := range values[offset : offset+length] {
		character, err := value.Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		text = append(text, rune(character))
	}
	return jvm.VoidValue(), runtime.graphicsDrawText(state, text, positions[2], positions[3], positions[4])
}

// runtimeGraphicsDrawSubstring renders part of a string.
func runtimeGraphicsDrawSubstring(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 7 {
		return jvm.VoidValue(), fmt.Errorf("Graphics.drawSubstring expected text, range, position, and anchor, got %d arguments", len(arguments)-1)
	}
	textObject, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, _ := jvm.StringText(textObject)
	text := []rune(value)
	positions, err := runtimeGraphicsInts(arguments[1:], 5)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, length := positions[0], positions[1]
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(text)) {
		return jvm.VoidValue(), fmt.Errorf("Graphics.drawSubstring range [%d, %d) is out of bounds", offset, offset+length)
	}
	return jvm.VoidValue(), runtime.graphicsDrawText(state, text[offset:offset+length], positions[2], positions[3], positions[4])
}

// runtimeGraphicsCopyArea copies a rectangle inside the target framebuffer.
//
// This platform's Java call is not the MIDP one: the specification gives it as
// `copyArea(int dx, int dy, int sx, int sy, int w, int h)` — the destination
// first, then the source, then the size — while MIDP names the source first
// and ends with an anchor. Reading it in MIDP order makes a scrolling title's
// whole-surface shift a copy of nothing, because what it passes as the size
// lands where the size is not.
func runtimeGraphicsCopyArea(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	width, height := values[4], values[5]
	if width <= 0 || height <= 0 {
		return jvm.VoidValue(), nil
	}
	destinationX, destinationY := values[0]+state.translateX, values[1]+state.translateY
	left, top, clippedWidth, clippedHeight, ok := state.clip(values[0], values[1], width, height)
	if !ok {
		return jvm.VoidValue(), nil
	}
	// The clip moved the destination corner, so the source corner moves with
	// it; otherwise a shift that starts off the left edge copies the wrong
	// column into the visible one.
	sourceX := values[2] + state.translateX + (left - destinationX)
	sourceY := values[3] + state.translateY + (top - destinationY)
	// The graphics target is a drawing surface, not a decoded image, so it
	// carries no transparency of its own to preserve here.
	return jvm.VoidValue(), runtime.wipicBlit(
		state.target, left, top, clippedWidth, clippedHeight,
		state.target, sourceX, sourceY,
		blitOpacity{},
	)
}

// runtimeGraphicsSetPixel draws one pixel in the current color.
func runtimeGraphicsSetPixel(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.VoidValue(), err
	}
	values, err := runtimeGraphicsInts(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.graphicsFillRect(state, values[0], values[1], 1, 1, state.color)
}

// runtimeGraphicsGetPixel reads one pixel as a 0x00RRGGBB word. Coordinates
// outside the target answer zero rather than failing, matching how the
// original runtime reports an unreadable pixel.
func runtimeGraphicsGetPixel(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.IntValue(0), err
	}
	values, err := runtimeGraphicsInts(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y := values[0]+state.translateX, values[1]+state.translateY
	if x < 0 || y < 0 || uint32(x) >= state.target.width || uint32(y) >= state.target.height {
		return jvm.IntValue(0), nil
	}
	var data [2]byte
	if err := runtime.client.core.Memory().Read(state.target.pixels+uint32(y)*state.target.bpl+uint32(x)*2, data[:]); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(rgbFromPixel565(binary.LittleEndian.Uint16(data[:])))), nil
}

// rgbFromPixel565 expands a 16-bit target pixel to a 0x00RRGGBB word, keeping
// the top bits in the low positions so white stays white.
func rgbFromPixel565(pixel uint16) uint32 {
	red := uint32(pixel>>11&0x1f) << 3
	green := uint32(pixel>>5&0x3f) << 2
	blue := uint32(pixel&0x1f) << 3
	return red<<16 | green<<8 | blue | red>>5<<16 | green>>6<<8 | blue>>5
}

func pixel565FromRGB(rgb uint32) uint16 {
	return uint16(rgb>>16&0xff)>>3<<11 | uint16(rgb>>8&0xff)>>2<<5 | uint16(rgb&0xff)>>3
}

// runtimeGraphicsTransferRGBPixels implements getRGBPixels and setRGBPixels:
// a rectangle of 0x00RRGGBB words in a caller-owned int array, with a
// caller-chosen row offset and pitch.
// runtimeGraphicsEncodeImage implements Graphics.encodeImage(x, y, w, h),
// which the specification defines as the BMP bytes of a region of the screen.
// A title uses it to keep what it drew — a portrait, a saved map — and hands
// the bytes back to Image creation later, so the same encoder serves it as
// serves MC_grpEncodeImage. The specification's answer to a region that cannot
// be encoded is null rather than an exception.
func runtimeGraphicsEncodeImage(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := runtimeGraphicsStateOf(arguments)
	if err != nil || state == nil {
		return jvm.ReferenceValue(nil), err
	}
	values, err := runtimeGraphicsInts(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	encoded, err := runtime.encodeFramebufferRegion(state.target,
		values[0]+state.translateX, values[1]+state.translateY, values[2], values[3])
	if err != nil {
		runtime.countDiagnostic("encode image refused")
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(jvm.NewByteArray(encoded)), nil
}

func runtimeGraphicsTransferRGBPixels(write bool) runtimeJavaImplementation {
	return func(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		state, err := runtimeGraphicsStateOf(arguments)
		if err != nil || state == nil {
			return jvm.VoidValue(), err
		}
		if len(arguments) != 8 {
			return jvm.VoidValue(), fmt.Errorf("Graphics RGB pixel transfer expected a rectangle, buffer, offset, and pitch, got %d arguments", len(arguments)-1)
		}
		bounds, err := runtimeGraphicsInts(arguments, 4)
		if err != nil {
			return jvm.VoidValue(), err
		}
		buffer, err := arguments[5].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if buffer == nil {
			// The specification names the throw, and a title that guards its
			// own call with `catch (NullPointerException)` gets nothing from a
			// platform error.
			return jvm.VoidValue(), &jvm.GuestException{
				Object:  &jvm.Object{ClassName: "java/lang/NullPointerException", Native: "Graphics RGB pixel buffer"},
				Message: "Graphics RGB pixel buffer is null",
			}
		}
		tail, err := runtimeGraphicsInts(arguments[5:], 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		offset, bytesPerLine := tail[0], tail[1]
		width, height := bounds[2], bounds[3]
		if width <= 0 || height <= 0 {
			return jvm.VoidValue(), nil
		}
		// **The last argument is bytes per line, not elements.** The
		// specification says so in as many words — "한 줄의 이미지가 저장되기
		// 위해서 필요한 바이트 수" — and the WIPI C call beside it is already
		// read that way here. Reading it as an element pitch multiplies the
		// span by four: one title hands over an 88x102 picture in an 8976
		// element array with a line of 352, which is exactly 88 pixels of four
		// bytes and exactly fills the array, and this platform made it a range
		// four times too long and ended the session on its first frame.
		pitch := width
		if bytesPerLine != 0 {
			pitch = bytesPerLine / 4
		}
		_, length, ok := jvm.ArrayComponent(buffer)
		if !ok {
			return jvm.VoidValue(), fmt.Errorf("Graphics RGB pixel buffer is not an array")
		}
		// A line too short to hold the row copies nothing, which is what the C
		// call's own specification says for the same argument.
		if pitch < width {
			return jvm.VoidValue(), nil
		}
		last := int64(offset) + int64(height-1)*int64(pitch) + int64(width)
		if offset < 0 || last > int64(length) {
			message := fmt.Sprintf("Graphics RGB pixel range exceeds the %d element buffer (rect=%v offset=%d bpl=%d last=%d)", length, bounds, offset, bytesPerLine, last)
			return jvm.VoidValue(), &jvm.GuestException{
				Object:  &jvm.Object{ClassName: "java/lang/ArrayIndexOutOfBoundsException", Native: message},
				Message: message,
			}
		}
		memory := runtime.client.core.Memory()
		var source []jvm.Value
		if write {
			if _, source, err = jvm.ArraySnapshot(buffer); err != nil {
				return jvm.VoidValue(), err
			}
		}
		row := make([]jvm.Value, width)
		for line := int32(0); line < height; line++ {
			y := bounds[1] + state.translateY + line
			base := int(offset + line*pitch)
			for column := int32(0); column < width; column++ {
				x := bounds[0] + state.translateX + column
				address := state.target.pixels + uint32(y)*state.target.bpl + uint32(x)*2
				inside := x >= 0 && y >= 0 && uint32(x) < state.target.width && uint32(y) < state.target.height
				if !write {
					row[column] = jvm.IntValue(0)
					if !inside {
						continue
					}
					var data [2]byte
					if err := memory.Read(address, data[:]); err != nil {
						return jvm.VoidValue(), err
					}
					row[column] = jvm.IntValue(int32(rgbFromPixel565(binary.LittleEndian.Uint16(data[:]))))
					continue
				}
				if !inside {
					continue
				}
				rgb, err := source[base+int(column)].Int32()
				if err != nil {
					return jvm.VoidValue(), err
				}
				var data [2]byte
				binary.LittleEndian.PutUint16(data[:], pixel565FromRGB(uint32(rgb)))
				if err := memory.Write(address, data[:]); err != nil {
					return jvm.VoidValue(), err
				}
			}
			if !write {
				if err := jvm.SetArrayRange(buffer, base, row); err != nil {
					return jvm.VoidValue(), err
				}
			}
		}
		return jvm.VoidValue(), nil
	}
}

// runtimeGraphicsTransferPixels implements getPixels and setPixels: the same
// rectangle the RGB pair moves, in the target's own pixel format rather than
// in 0x00RRGGBB words. The specification leaves that format to the handset and
// only ties it to setPixel; these are 16-bit screens, so a pixel is the two
// framebuffer bytes as they lie and `bpl` is a byte pitch, which is why the
// buffer is a byte array here and an int array there.
func runtimeGraphicsTransferPixels(write bool) runtimeJavaImplementation {
	return func(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		state, err := runtimeGraphicsStateOf(arguments)
		if err != nil || state == nil {
			return jvm.VoidValue(), err
		}
		if len(arguments) != 8 {
			return jvm.VoidValue(), fmt.Errorf("Graphics pixel transfer expected a rectangle, buffer, offset, and pitch, got %d arguments", len(arguments)-1)
		}
		bounds, err := runtimeGraphicsInts(arguments, 4)
		if err != nil {
			return jvm.VoidValue(), err
		}
		buffer, err := arguments[5].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if buffer == nil {
			return jvm.VoidValue(), fmt.Errorf("Graphics pixel buffer is null")
		}
		tail, err := runtimeGraphicsInts(arguments[5:], 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		offset, pitch := tail[0], tail[1]
		width, height := bounds[2], bounds[3]
		if width <= 0 || height <= 0 {
			return jvm.VoidValue(), nil
		}
		if pitch == 0 {
			pitch = width * 2
		}
		component, length, ok := jvm.ArrayComponent(buffer)
		if !ok || component.Kind != jvm.TypeByte {
			return jvm.VoidValue(), fmt.Errorf("Graphics pixel buffer is not a byte array")
		}
		last := int64(offset) + int64(height-1)*int64(pitch) + int64(width)*2
		if offset < 0 || pitch < 0 || last > int64(length) {
			return jvm.VoidValue(), fmt.Errorf("Graphics pixel range exceeds the %d byte buffer", length)
		}
		memory := runtime.client.core.Memory()
		var source []jvm.Value
		if write {
			if _, source, err = jvm.ArraySnapshot(buffer); err != nil {
				return jvm.VoidValue(), err
			}
		}
		row := make([]jvm.Value, width*2)
		for line := int32(0); line < height; line++ {
			y := bounds[1] + state.translateY + line
			base := int(offset + line*pitch)
			for column := int32(0); column < width; column++ {
				x := bounds[0] + state.translateX + column
				address := state.target.pixels + uint32(y)*state.target.bpl + uint32(x)*2
				inside := x >= 0 && y >= 0 && uint32(x) < state.target.width && uint32(y) < state.target.height
				var data [2]byte
				if !write {
					if inside {
						if err := memory.Read(address, data[:]); err != nil {
							return jvm.VoidValue(), err
						}
					}
					row[column*2] = jvm.IntValue(int32(int8(data[0])))
					row[column*2+1] = jvm.IntValue(int32(int8(data[1])))
					continue
				}
				if !inside {
					continue
				}
				for half := 0; half < 2; half++ {
					value, err := source[base+int(column)*2+half].Int32()
					if err != nil {
						return jvm.VoidValue(), err
					}
					data[half] = byte(value)
				}
				if err := memory.Write(address, data[:]); err != nil {
					return jvm.VoidValue(), err
				}
			}
			if !write {
				if err := jvm.SetArrayRange(buffer, base, row); err != nil {
					return jvm.VoidValue(), err
				}
			}
		}
		return jvm.VoidValue(), nil
	}
}

// runtimeGraphicsFillPolygon fills a closed polygon by scanline, and
// runtimeGraphicsDrawPolygon outlines one. Both take parallel x and y arrays
// the way the original API does.
func runtimeGraphicsDrawPolygon(fill bool) runtimeJavaImplementation {
	return func(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		state, err := runtimeGraphicsStateOf(arguments)
		if err != nil || state == nil {
			return jvm.VoidValue(), err
		}
		if len(arguments) != 3 {
			return jvm.VoidValue(), fmt.Errorf("Graphics polygon expected x and y arrays, got %d arguments", len(arguments)-1)
		}
		xs, err := runtimeIntArrayArgument(arguments[1])
		if err != nil {
			return jvm.VoidValue(), err
		}
		ys, err := runtimeIntArrayArgument(arguments[2])
		if err != nil {
			return jvm.VoidValue(), err
		}
		count := min(len(xs), len(ys))
		if count < 2 {
			return jvm.VoidValue(), nil
		}
		if !fill {
			for index := 0; index < count; index++ {
				next := (index + 1) % count
				if err := runtime.graphicsDrawSegment(state, xs[index], ys[index], xs[next], ys[next]); err != nil {
					return jvm.VoidValue(), err
				}
			}
			return jvm.VoidValue(), nil
		}
		top, bottom := ys[0], ys[0]
		for _, y := range ys[:count] {
			if y < top {
				top = y
			}
			if y > bottom {
				bottom = y
			}
		}
		for y := top; y <= bottom; y++ {
			var crossings []int32
			for index := 0; index < count; index++ {
				next := (index + 1) % count
				y0, y1 := ys[index], ys[next]
				if y0 == y1 || y < min(y0, y1) || y >= max(y0, y1) {
					continue
				}
				x0, x1 := xs[index], xs[next]
				crossings = append(crossings, x0+(y-y0)*(x1-x0)/(y1-y0))
			}
			sort.Slice(crossings, func(left, right int) bool { return crossings[left] < crossings[right] })
			for index := 0; index+1 < len(crossings); index += 2 {
				width := crossings[index+1] - crossings[index] + 1
				if err := runtime.graphicsFillRect(state, crossings[index], y, width, 1, state.color); err != nil {
					return jvm.VoidValue(), err
				}
			}
		}
		return jvm.VoidValue(), nil
	}
}

func runtimeIntArrayArgument(value jvm.Value) ([]int32, error) {
	array, err := value.Reference()
	if err != nil {
		return nil, err
	}
	if array == nil {
		return nil, fmt.Errorf("Graphics polygon coordinate array is null")
	}
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		return nil, err
	}
	result := make([]int32, len(values))
	for index, element := range values {
		result[index], err = element.Int32()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// graphicsDrawSegment draws one Bresenham line in the current color.
func (runtime *initializationRuntime) graphicsDrawSegment(state *runtimeGraphicsState, x0, y0, x1, y1 int32) error {
	deltaX, deltaY := x1-x0, y1-y0
	if deltaX < 0 {
		deltaX = -deltaX
	}
	if deltaY < 0 {
		deltaY = -deltaY
	}
	stepX, stepY := int32(1), int32(1)
	if x0 > x1 {
		stepX = -1
	}
	if y0 > y1 {
		stepY = -1
	}
	difference := deltaX - deltaY
	for {
		if err := runtime.graphicsFillRect(state, x0, y0, 1, 1, state.color); err != nil {
			return err
		}
		if x0 == x1 && y0 == y1 {
			return nil
		}
		doubled := 2 * difference
		if doubled > -deltaY {
			difference -= deltaY
			x0 += stepX
		}
		if doubled < deltaX {
			difference += deltaX
			y0 += stepY
		}
	}
}

// runtimeImageSetTransparentColor records the color the image draws as
// see-through. Every later blit of this image masks pixels matching it.
func runtimeImageSetTransparentColor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Image.setTransparentColor expected receiver and color, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("Image.setTransparentColor receiver is null")
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields["transparentColor:I"] = arguments[1]
	return jvm.VoidValue(), nil
}

// runtimeImageCreateCopy copies an image into a new mutable one, which is how
// a game turns a decoded resource into a surface it can draw on.
func runtimeImageCreateCopy(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage expected a source image, got %d arguments", len(arguments))
	}
	source, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if source == nil {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage source is null")
	}
	width, height, err := runtimeImageSize(source)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.copyImageRegion(vm, source, 0, 0, width, height)
}

// runtimeImageCreateSubImage copies a rectangle of an image into a new one.
// The trailing flag asks for a transparent copy, which the copy inherits by
// keeping the source's transparent color.
func runtimeImageCreateSubImage(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 6 {
		return jvm.VoidValue(), fmt.Errorf("Image.createSubImage expected receiver, bounds, and transparency, got %d arguments", len(arguments))
	}
	source, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if source == nil {
		return jvm.VoidValue(), fmt.Errorf("Image.createSubImage receiver is null")
	}
	bounds := make([]int32, 4)
	for index := range bounds {
		value, err := arguments[index+1].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		bounds[index] = value
	}
	return runtime.copyImageRegion(vm, source, bounds[0], bounds[1], bounds[2], bounds[3])
}

// copyImageRegion rasterizes a region of one image into a fresh offscreen
// image of that size.
func (runtime *initializationRuntime) copyImageRegion(_ *jvm.VM, source *jvm.Object, x, y, width, height int32) (jvm.Value, error) {
	if width <= 0 || height <= 0 || width > maxImageDimension || height > maxImageDimension {
		return jvm.VoidValue(), fmt.Errorf("image region %dx%d is out of range", width, height)
	}
	sourceHandle, err := runtime.imageFramebufferHandle(source)
	if err != nil {
		return jvm.VoidValue(), err
	}
	sourceFramebuffer, err := runtime.readWIPICFramebuffer(sourceHandle)
	if err != nil {
		return jvm.VoidValue(), err
	}
	handle, err := runtime.newWIPICFramebufferRecord(uint32(width), uint32(height))
	if err != nil {
		return jvm.VoidValue(), err
	}
	target, err := runtime.readWIPICFramebuffer(handle)
	if err != nil {
		return jvm.VoidValue(), err
	}
	// The copy replaces rather than composites, so it inherits the region's
	// transparency outright instead of merging into what the fresh
	// framebuffer holds.
	sourceOpacity := runtime.framebufferOpacityOf(sourceHandle)
	if err := runtime.wipicBlit(target, 0, 0, width, height, sourceFramebuffer, x, y, blitOpacity{source: sourceOpacity}); err != nil {
		return jvm.VoidValue(), err
	}
	runtime.setFramebufferOpacity(handle, sourceOpacity.region(int(x), int(y), int(width), int(height)))
	fields := map[string]jvm.Value{
		"width:I":            jvm.IntValue(width),
		"height:I":           jvm.IntValue(height),
		"mutable:Z":          jvm.IntValue(1),
		"guestFramebuffer:I": jvm.IntValue(int32(handle)),
	}
	if value, ok := source.Fields["transparentColor:I"]; ok {
		fields["transparentColor:I"] = value
	}
	return jvm.ReferenceValue(&jvm.Object{ClassName: "org/kwis/msp/lcdui/Image", Fields: fields}), nil
}

const maxImageDimension = 2048

// runtimeImageDrawImage composites a region of one image into the receiver
// image, the offscreen counterpart of Graphics.drawImage.
func runtimeImageDrawImage(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 10 {
		return jvm.VoidValue(), fmt.Errorf("Image.drawImage expected receiver, source, and geometry, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	source, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil || source == nil {
		return jvm.VoidValue(), fmt.Errorf("Image.drawImage receiver or source is null")
	}
	geometry := make([]int32, 8)
	for index := range geometry {
		value, err := arguments[index+2].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		geometry[index] = value
	}
	targetHandle, err := runtime.imageFramebufferHandle(receiver)
	if err != nil {
		return jvm.VoidValue(), err
	}
	sourceHandle, err := runtime.imageFramebufferHandle(source)
	if err != nil {
		return jvm.VoidValue(), err
	}
	target, err := runtime.readWIPICFramebuffer(targetHandle)
	if err != nil {
		return jvm.VoidValue(), err
	}
	sourceFramebuffer, err := runtime.readWIPICFramebuffer(sourceHandle)
	if err != nil {
		return jvm.VoidValue(), err
	}
	// The original signature is (destination x, y, width, height, source x, y,
	// width, height); scaling is not implemented, so the copied rectangle is
	// the smaller of the two.
	width := min(geometry[2], geometry[6])
	height := min(geometry[3], geometry[7])
	return jvm.VoidValue(), runtime.wipicBlit(
		target, geometry[0], geometry[1], width, height,
		sourceFramebuffer, geometry[4], geometry[5],
		blitOpacity{
			source:      runtime.framebufferOpacityOf(sourceHandle),
			destination: runtime.framebufferOpacityOf(targetHandle),
		},
	)
}

// runtimeImageSize reports an image's pixel size from its recorded fields.
func runtimeImageSize(image *jvm.Object) (int32, int32, error) {
	width, widthOK := image.Fields["width:I"]
	height, heightOK := image.Fields["height:I"]
	if !widthOK || !heightOK {
		return 0, 0, fmt.Errorf("image has no recorded size")
	}
	widthValue, err := width.Int32()
	if err != nil {
		return 0, 0, err
	}
	heightValue, err := height.Int32()
	if err != nil {
		return 0, 0, err
	}
	return widthValue, heightValue, nil
}
