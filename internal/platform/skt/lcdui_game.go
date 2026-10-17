package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// A GameCanvas draws into a buffer it owns and pushes it when the game says
// so, which inverts the ordinary Canvas contract: the runtime never asks it to
// paint, and the game never waits for a repaint callback.

func (runtime *Runtime) initGameCanvasBuffer(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	suppress, err := booleanArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	buffer, err := runtime.newMIDPImage(runtime.frameWidth, runtime.frameHeight, true, nil)
	if err != nil {
		return jvm.VoidValue(), err
	}
	image, err := midpImageOf(buffer)
	if err != nil || image == nil {
		return jvm.VoidValue(), fmt.Errorf("GameCanvas buffer is not an Image")
	}
	clip := paintRect{maxX: image.width, maxY: image.height}
	context := &graphicsContext{
		pixels:      image.rgba,
		width:       image.width,
		height:      image.height,
		destination: image,
		deviceClip:  clip,
		clip:        clip,
		font:        runtime.fontObject(fontSystem, fontPlain, fontMedium),
		active:      true,
	}
	data := runtime.displayableState(receiver)
	state := runtime.lcdui()
	state.mu.Lock()
	data.game = &gameCanvasData{
		buffer:       image,
		graphics:     &jvm.Object{ClassName: runtime.graphicsClassName(), Fields: make(map[string]jvm.Value), Native: context},
		suppressKeys: suppress,
	}
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) gameCanvasState(arguments []jvm.Value) (*jvm.Object, *gameCanvasData, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, nil, err
	}
	if receiver == nil {
		return nil, nil, newGuestException("java/lang/NullPointerException", "GameCanvas is null")
	}
	data := runtime.displayableState(receiver)
	state := runtime.lcdui()
	state.mu.Lock()
	game := data.game
	state.mu.Unlock()
	if game == nil {
		return nil, nil, fmt.Errorf("GameCanvas has no buffer")
	}
	return receiver, game, nil
}

func (runtime *Runtime) gameCanvasGraphics(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, game, err := runtime.gameCanvasState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(game.graphics), nil
}

func (runtime *Runtime) gameCanvasKeyStates(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, game, err := runtime.gameCanvasState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	// A key counts if it is down now or was pressed since the previous call,
	// so a tap that comes and goes between two polls still reaches the title;
	// reading the latch clears it.
	state := runtime.lcdui()
	state.mu.Lock()
	states := game.keyStates | game.latchedKeys
	game.latchedKeys = 0
	state.mu.Unlock()
	return jvm.IntValue(states), nil
}

func (runtime *Runtime) gameCanvasFlush(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, game, err := runtime.gameCanvasState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	rect := paintRect{maxX: game.buffer.width, maxY: game.buffer.height}
	if len(arguments) == 5 {
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
		rect = clippedPaintRect(x, y, width, height, game.buffer.width, game.buffer.height)
	}
	return jvm.VoidValue(), runtime.presentGameBuffer(receiver, game, rect)
}

// presentGameBuffer copies the buffer region into the Host frame and presents
// it. Only the current Displayable may present, so a background GameCanvas
// that keeps drawing does not overwrite what is on screen.
func (runtime *Runtime) presentGameBuffer(canvas *jvm.Object, game *gameCanvasData, rect paintRect) error {
	runtime.displayMu.RLock()
	current := runtime.currentDisplayable
	runtime.displayMu.RUnlock()
	if current != canvas || runtime.State() != StateActive {
		return nil
	}
	if rect.empty() {
		return nil
	}
	pixels := game.buffer.snapshot()

	runtime.renderMu.Lock()
	for y := rect.minY; y < rect.maxY && y < runtime.frameHeight; y++ {
		for x := rect.minX; x < rect.maxX && x < runtime.frameWidth; x++ {
			source := (y*game.buffer.width + x) * 4
			destination := (y*runtime.frameWidth + x) * 4
			if source+4 > len(pixels) || destination+4 > len(runtime.frameRGBA) {
				continue
			}
			copy(runtime.frameRGBA[destination:destination+4], pixels[source:source+4])
		}
	}
	frame := backend.Frame{
		Width:  runtime.frameWidth,
		Height: runtime.frameHeight,
		RGBA:   append([]byte(nil), runtime.frameRGBA...),
	}
	runtime.renderMu.Unlock()

	if err := runtime.framebuffer.Present(frame); err != nil {
		return fmt.Errorf("present GameCanvas %s: %w", canvas.ClassName, err)
	}
	return nil
}

// gameCanvasDrawBuffer implements the inherited paint(Graphics) by blitting
// the buffer, so a GameCanvas that is also repainted the ordinary way shows
// the same pixels. MIDP subjects that paint to the Graphics' clip and origin,
// and like every other drawing call it writes under the destination's lock:
// the title's own thread may be flushing the same screen meanwhile.
func (runtime *Runtime) gameCanvasDrawBuffer(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, game, err := runtime.gameCanvasState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	context, err := graphicsReceiver(arguments[1:])
	if err != nil {
		return jvm.VoidValue(), err
	}
	pixels := game.buffer.snapshot()
	context.withDestinationWrite(func() {
		context.drawRGBA(pixels, game.buffer.width, game.buffer.height,
			int64(context.translateX), int64(context.translateY))
	})
	return jvm.VoidValue(), nil
}

// gameCanvasKeyRecord is the one int32 a checkpoint keeps a GameCanvas's key
// state in: the held bits low, where they always were, and the latch above
// them. Every getKeyStates bit is below bit 16, so a record written before
// there was a latch reads back as the same held keys and no latch.
func gameCanvasKeyRecord(held, latched int32) int32 {
	return held&0xffff | latched&0xffff<<16
}

func splitGameCanvasKeyRecord(record int32) (held, latched int32) {
	return record & 0xffff, int32(uint32(record) >> 16)
}

// recordGameCanvasKey maintains the getKeyStates bits for a GameCanvas and
// reports whether the ordinary key callback should still be delivered.
func (runtime *Runtime) recordGameCanvasKey(canvas *jvm.Object, eventType KeyEventType, keyCode int32) bool {
	data := runtime.displayableState(canvas)
	state := runtime.lcdui()
	state.mu.Lock()
	defer state.mu.Unlock()
	if data.game == nil {
		return true
	}
	bit := canvasKeyState(keyCode)
	switch eventType {
	case KeyPressed, KeyRepeated:
		data.game.keyStates |= bit
		data.game.latchedKeys |= bit
	case KeyReleased:
		data.game.keyStates &^= bit
	}
	return !data.game.suppressKeys
}
