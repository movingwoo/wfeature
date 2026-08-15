package skt

import (
	"fmt"
	"math"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type paintRect struct {
	minX int
	minY int
	maxX int
	maxY int
}

func (rect paintRect) empty() bool {
	return rect.maxX <= rect.minX || rect.maxY <= rect.minY
}

func (rect paintRect) union(other paintRect) paintRect {
	if rect.empty() {
		return other
	}
	if other.empty() {
		return rect
	}
	return paintRect{
		minX: min(rect.minX, other.minX),
		minY: min(rect.minY, other.minY),
		maxX: max(rect.maxX, other.maxX),
		maxY: max(rect.maxY, other.maxY),
	}
}

func (rect paintRect) intersect(other paintRect) paintRect {
	result := paintRect{
		minX: max(rect.minX, other.minX),
		minY: max(rect.minY, other.minY),
		maxX: min(rect.maxX, other.maxX),
		maxY: min(rect.maxY, other.maxY),
	}
	if result.empty() {
		return paintRect{}
	}
	return result
}

type graphicsContext struct {
	pixels      []byte
	width       int
	height      int
	destination *imageData
	deviceClip  paintRect
	clip        paintRect
	translateX  int32
	translateY  int32
	color       uint32
	font        *jvm.Object
	active      bool
}

func (runtime *Runtime) getDisplayableWidth(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := runtime.displayableReceiver(vm, arguments); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(runtime.frameWidth)), nil
}

func (runtime *Runtime) getDisplayableHeight(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := runtime.displayableReceiver(vm, arguments); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(runtime.frameHeight)), nil
}

func (runtime *Runtime) repaintNewCurrentCanvas(displayable *jvm.Object) error {
	if displayable == nil {
		return nil
	}
	isCanvas, err := runtime.VM.IsSubclassOf(displayable.ClassName, midp.CanvasClass)
	if err != nil {
		return fmt.Errorf("validate current Canvas: %w", err)
	}
	if !isCanvas {
		// A Screen is drawn by the runtime, so becoming current is what
		// schedules its first paint — there is no application paint() to call.
		if runtime.isScreen(displayable) {
			return runtime.queueScreenPaint(displayable)
		}
		return nil
	}
	return runtime.queueCanvasPaint(displayable, paintRect{maxX: runtime.frameWidth, maxY: runtime.frameHeight})
}

func (runtime *Runtime) repaintCanvas(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	canvas, err := runtime.canvasReceiver(vm, arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	y, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	width, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	height, err := intArgument(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	rect := clippedPaintRect(x, y, width, height, runtime.frameWidth, runtime.frameHeight)
	if rect.empty() {
		return jvm.VoidValue(), nil
	}
	if err := runtime.queueCanvasPaint(canvas, rect); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) serviceCanvasRepaints(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	canvas, err := runtime.canvasReceiver(vm, arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.displayMu.RLock()
	current := runtime.currentDisplayable
	painting := runtime.painting
	runtime.displayMu.RUnlock()
	if current != canvas || painting {
		return jvm.VoidValue(), nil
	}
	if err := runtime.paintPendingCanvas(); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) setCanvasFullScreenMode(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	canvas, err := runtime.canvasReceiver(vm, arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	fullScreen, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}

	runtime.displayMu.Lock()
	next := fullScreen != 0
	if runtime.fullScreen[canvas] == next {
		runtime.displayMu.Unlock()
		return jvm.VoidValue(), nil
	}
	runtime.fullScreen[canvas] = next
	current := runtime.currentDisplayable == canvas
	runtime.displayMu.Unlock()

	if current {
		if err := runtime.queueCanvasPaint(canvas, paintRect{maxX: runtime.frameWidth, maxY: runtime.frameHeight}); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) queueCanvasPaint(canvas *jvm.Object, rect paintRect) error {
	runtime.displayMu.Lock()
	if runtime.currentDisplayable != canvas {
		runtime.displayMu.Unlock()
		return nil
	}
	if runtime.paintCanvas != canvas {
		runtime.pendingPaint = rect
		runtime.paintCanvas = canvas
	} else {
		runtime.pendingPaint = runtime.pendingPaint.union(rect)
	}
	if runtime.paintQueued {
		runtime.displayMu.Unlock()
		return nil
	}
	if err := runtime.events.Post("Canvas.repaint", runtime.paintPendingCanvas); err != nil {
		runtime.pendingPaint = paintRect{}
		runtime.paintCanvas = nil
		runtime.displayMu.Unlock()
		return fmt.Errorf("queue Canvas repaint: %w", err)
	}
	runtime.paintQueued = true
	runtime.displayMu.Unlock()
	return nil
}

func (runtime *Runtime) paintPendingCanvas() error {
	runtime.displayMu.Lock()
	if runtime.painting || !runtime.paintQueued || runtime.pendingPaint.empty() {
		runtime.displayMu.Unlock()
		return nil
	}
	canvas := runtime.paintCanvas
	clip := runtime.pendingPaint
	runtime.pendingPaint = paintRect{}
	runtime.paintCanvas = nil
	runtime.paintQueued = false
	if runtime.currentDisplayable != canvas || runtime.State() != StateActive {
		runtime.displayMu.Unlock()
		return nil
	}
	runtime.painting = true
	runtime.displayMu.Unlock()

	graphics := runtime.screenGraphics(clip)

	runtime.renderMu.Lock()
	_, paintErr := runtime.VM.InvokeVirtual(canvas, "paint", "(Ljavax/microedition/lcdui/Graphics;)V", jvm.ReferenceValue(graphics))
	frame := backend.Frame{
		Width:  runtime.frameWidth,
		Height: runtime.frameHeight,
		RGBA:   append([]byte(nil), runtime.frameRGBA...),
	}
	runtime.renderMu.Unlock()

	runtime.displayMu.Lock()
	runtime.painting = false
	runtime.displayMu.Unlock()
	if paintErr != nil {
		return fmt.Errorf("paint Canvas %s: %w", canvas.ClassName, paintErr)
	}
	if err := runtime.framebuffer.Present(frame); err != nil {
		return fmt.Errorf("present Canvas %s: %w", canvas.ClassName, err)
	}
	return nil
}

// screenGraphics answers the Graphics a Canvas paints the screen through. It is
// one object for the life of the runtime, reset to the clip of the repaint
// about to run, and it is never retired.
//
// MIDP says a Graphics is only valid inside the paint call it arrived in, and
// on this vendor that is not what a title observed. The handset runtime handed
// out one screen Graphics, so the common shape here is a Canvas whose paint
// does nothing but store the Graphics in a static field; the game then draws
// from its own thread whenever it likes and pushes the result with
// XDisplay.refresh. A title written that way paints exactly once and then draws
// forever through an object this runtime used to declare dead — which showed up
// as a game that started its thread, drew its first logo, and died inside it.
//
// What a caller does not get is a second thread's worth of safety: two guest
// threads drawing at once still race, as they always did.
func (runtime *Runtime) screenGraphics(clip paintRect) *jvm.Object {
	if runtime.screenGraphicsObject == nil {
		context := &graphicsContext{
			pixels: runtime.frameRGBA,
			width:  runtime.frameWidth,
			height: runtime.frameHeight,
			font:   runtime.fontObject(fontSystem, fontPlain, fontMedium),
			active: true,
		}
		runtime.screenGraphicsContext = context
		runtime.screenGraphicsObject = &jvm.Object{
			ClassName: midp.GraphicsClass,
			Fields:    make(map[string]jvm.Value),
			Native:    context,
		}
	}
	// A paint starts from the state MIDP promises it: the repaint's clip, no
	// translation, and the default font.
	context := runtime.screenGraphicsContext
	context.deviceClip = clip
	context.clip = clip
	context.translateX = 0
	context.translateY = 0
	context.font = runtime.fontObject(fontSystem, fontPlain, fontMedium)
	return runtime.screenGraphicsObject
}

func (runtime *Runtime) setGraphicsColor(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	color, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	context.color = uint32(color) & 0x00ffffff
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) getGraphicsColor(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(context.color)), nil
}

func (runtime *Runtime) setGraphicsColorRGB(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	red, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	green, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	blue, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if red < 0 || red > 255 || green < 0 || green > 255 || blue < 0 || blue > 255 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "Graphics color component is outside 0..255")
	}
	context.color = uint32(red)<<16 | uint32(green)<<8 | uint32(blue)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) setGraphicsClip(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	context.clip = context.translatedRect(x, y, width, height).intersect(context.deviceClip)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) clipGraphicsRect(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	context.clip = context.clip.intersect(context.translatedRect(x, y, width, height))
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) getGraphicsClipX(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(int64(context.clip.minX) - int64(context.translateX))), nil
}

func (runtime *Runtime) getGraphicsClipY(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(int64(context.clip.minY) - int64(context.translateY))), nil
}

func (runtime *Runtime) getGraphicsClipWidth(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(max(0, context.clip.maxX-context.clip.minX))), nil
}

func (runtime *Runtime) getGraphicsClipHeight(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(max(0, context.clip.maxY-context.clip.minY))), nil
}

func (runtime *Runtime) translateGraphics(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	y, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	context.translateX += x
	context.translateY += y
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) getGraphicsTranslateX(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(context.translateX), nil
}

func (runtime *Runtime) getGraphicsTranslateY(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(context.translateY), nil
}

func (runtime *Runtime) fillGraphicsRect(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	rect := context.translatedRect(x, y, width, height).intersect(context.clip)
	if rect.empty() {
		return jvm.VoidValue(), nil
	}
	context.withDestinationWrite(func() {
		context.fillRect(rect)
	})
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) drawGraphicsLine(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x1, y1, x2, y2, err := rectArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	context.withDestinationWrite(func() {
		context.drawLine(
			int64(x1)+int64(context.translateX),
			int64(y1)+int64(context.translateY),
			int64(x2)+int64(context.translateX),
			int64(y2)+int64(context.translateY),
		)
	})
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) drawGraphicsRect(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if width < 0 || height < 0 {
		return jvm.VoidValue(), nil
	}
	minX := int64(x) + int64(context.translateX)
	minY := int64(y) + int64(context.translateY)
	maxX := minX + int64(width)
	maxY := minY + int64(height)
	context.withDestinationWrite(func() {
		context.drawLine(minX, minY, maxX, minY)
		if height != 0 {
			context.drawLine(minX, maxY, maxX, maxY)
			context.drawLine(minX, minY, minX, maxY)
			if width != 0 {
				context.drawLine(maxX, minY, maxX, maxY)
			}
		}
	})
	return jvm.VoidValue(), nil
}

func (context *graphicsContext) translatedRect(x, y, width, height int32) paintRect {
	if width <= 0 || height <= 0 {
		return paintRect{}
	}
	minX := max(int64(x)+int64(context.translateX), 0)
	minY := max(int64(y)+int64(context.translateY), 0)
	maxX := min(int64(x)+int64(context.translateX)+int64(width), int64(context.width))
	maxY := min(int64(y)+int64(context.translateY)+int64(height), int64(context.height))
	if maxX <= minX || maxY <= minY {
		return paintRect{}
	}
	return paintRect{minX: int(minX), minY: int(minY), maxX: int(maxX), maxY: int(maxY)}
}

// fillClipped fills a rectangle after clipping it, which is what a caller
// that composes its own rectangles — the screen renderer — needs. fillRect
// itself trusts its argument because the Graphics natives clip before calling
// it, and a rectangle past the framebuffer would index out of range.
func (context *graphicsContext) fillClipped(rect paintRect) {
	clipped := rect.intersect(context.clip)
	if clipped.empty() {
		return
	}
	context.fillRect(clipped)
}

func (context *graphicsContext) fillRect(rect paintRect) {
	r, g, b := context.colorBytes()
	for row := rect.minY; row < rect.maxY; row++ {
		index := (row*context.width + rect.minX) * 4
		for column := rect.minX; column < rect.maxX; column++ {
			context.pixels[index] = r
			context.pixels[index+1] = g
			context.pixels[index+2] = b
			context.pixels[index+3] = 0xff
			index += 4
		}
	}
}

func (context *graphicsContext) drawLine(x1, y1, x2, y2 int64) {
	x1Clipped, y1Clipped, x2Clipped, y2Clipped, ok := clipLine(x1, y1, x2, y2, context.clip)
	if !ok {
		return
	}
	deltaX := abs(x2Clipped - x1Clipped)
	stepX := -1
	if x1Clipped < x2Clipped {
		stepX = 1
	}
	deltaY := -abs(y2Clipped - y1Clipped)
	stepY := -1
	if y1Clipped < y2Clipped {
		stepY = 1
	}
	err := deltaX + deltaY
	for {
		context.putColorPixel(int64(x1Clipped), int64(y1Clipped))
		if x1Clipped == x2Clipped && y1Clipped == y2Clipped {
			return
		}
		twiceError := 2 * err
		if twiceError >= deltaY {
			err += deltaY
			x1Clipped += stepX
		}
		if twiceError <= deltaX {
			err += deltaX
			y1Clipped += stepY
		}
	}
}

func (context *graphicsContext) putColorPixel(x, y int64) {
	if x < int64(context.clip.minX) || x >= int64(context.clip.maxX) || y < int64(context.clip.minY) || y >= int64(context.clip.maxY) {
		return
	}
	index := (int(y)*context.width + int(x)) * 4
	r, g, b := context.colorBytes()
	context.pixels[index] = r
	context.pixels[index+1] = g
	context.pixels[index+2] = b
	context.pixels[index+3] = 0xff
}

func (context *graphicsContext) withDestinationWrite(draw func()) {
	if context.destination != nil {
		context.destination.mu.Lock()
		defer context.destination.mu.Unlock()
	}
	draw()
}

func (context *graphicsContext) colorBytes() (byte, byte, byte) {
	return byte(context.color >> 16), byte(context.color >> 8), byte(context.color)
}

func clipLine(x1, y1, x2, y2 int64, clip paintRect) (int, int, int, int, bool) {
	if clip.empty() {
		return 0, 0, 0, 0, false
	}
	deltaX := float64(x2 - x1)
	deltaY := float64(y2 - y1)
	enter, leave := 0.0, 1.0
	boundaries := [][2]float64{
		{-deltaX, float64(x1 - int64(clip.minX))},
		{deltaX, float64(int64(clip.maxX-1) - x1)},
		{-deltaY, float64(y1 - int64(clip.minY))},
		{deltaY, float64(int64(clip.maxY-1) - y1)},
	}
	for _, boundary := range boundaries {
		p, q := boundary[0], boundary[1]
		if p == 0 {
			if q < 0 {
				return 0, 0, 0, 0, false
			}
			continue
		}
		ratio := q / p
		if p < 0 {
			enter = max(enter, ratio)
		} else {
			leave = min(leave, ratio)
		}
		if enter > leave {
			return 0, 0, 0, 0, false
		}
	}
	clippedX1 := clamp(int(math.Round(float64(x1)+enter*deltaX)), clip.minX, clip.maxX-1)
	clippedY1 := clamp(int(math.Round(float64(y1)+enter*deltaY)), clip.minY, clip.maxY-1)
	clippedX2 := clamp(int(math.Round(float64(x1)+leave*deltaX)), clip.minX, clip.maxX-1)
	clippedY2 := clamp(int(math.Round(float64(y1)+leave*deltaY)), clip.minY, clip.maxY-1)
	return clippedX1, clippedY1, clippedX2, clippedY2, true
}

func rectArguments(arguments []jvm.Value, start int) (int32, int32, int32, int32, error) {
	values := [4]int32{}
	for index := range values {
		value, err := intArgument(arguments, start+index)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		values[index] = value
	}
	return values[0], values[1], values[2], values[3], nil
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func clamp(value, minimum, maximum int) int {
	return min(max(value, minimum), maximum)
}

func (runtime *Runtime) displayableReceiver(vm *jvm.VM, arguments []jvm.Value) (*jvm.Object, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "Displayable receiver is null")
	}
	isDisplayable, err := vm.IsSubclassOf(receiver.ClassName, midp.DisplayableClass)
	if err != nil {
		return nil, fmt.Errorf("validate Displayable receiver: %w", err)
	}
	if !isDisplayable {
		return nil, fmt.Errorf("receiver %s is not a Displayable", receiver.ClassName)
	}
	return receiver, nil
}

func (runtime *Runtime) canvasReceiver(vm *jvm.VM, arguments []jvm.Value) (*jvm.Object, error) {
	receiver, err := runtime.displayableReceiver(vm, arguments)
	if err != nil {
		return nil, err
	}
	isCanvas, err := vm.IsSubclassOf(receiver.ClassName, midp.CanvasClass)
	if err != nil {
		return nil, fmt.Errorf("validate Canvas receiver: %w", err)
	}
	if !isCanvas {
		return nil, fmt.Errorf("receiver %s is not a Canvas", receiver.ClassName)
	}
	return receiver, nil
}

func graphicsReceiver(arguments []jvm.Value) (*graphicsContext, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "Graphics receiver is null")
	}
	context, ok := receiver.Native.(*graphicsContext)
	if receiver.ClassName != midp.GraphicsClass || !ok || context == nil || !context.active {
		return nil, newGuestException("java/lang/IllegalStateException", "Graphics is only valid during paint")
	}
	return context, nil
}

func intArgument(arguments []jvm.Value, index int) (int32, error) {
	if index < 0 || index >= len(arguments) {
		return 0, fmt.Errorf("argument %d is missing", index)
	}
	value, err := arguments[index].Int32()
	if err != nil {
		return 0, fmt.Errorf("argument %d: %w", index, err)
	}
	return value, nil
}

func clippedPaintRect(x, y, width, height int32, frameWidth, frameHeight int) paintRect {
	if width <= 0 || height <= 0 {
		return paintRect{}
	}
	minX := max(int64(x), 0)
	minY := max(int64(y), 0)
	maxX := min(int64(x)+int64(width), int64(frameWidth))
	maxY := min(int64(y)+int64(height), int64(frameHeight))
	if maxX <= minX || maxY <= minY {
		return paintRect{}
	}
	return paintRect{minX: int(minX), minY: int(minY), maxX: int(maxX), maxY: int(maxY)}
}
