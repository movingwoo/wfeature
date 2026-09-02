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
	state := runtime.lcdui()
	state.mu.Lock()
	states := game.keyStates
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
// the same pixels.
func (runtime *Runtime) gameCanvasDrawBuffer(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, game, err := runtime.gameCanvasState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	context, err := graphicsReceiver(arguments[1:])
	if err != nil {
		return jvm.VoidValue(), err
	}
	context.drawRGBA(game.buffer.snapshot(), game.buffer.width, game.buffer.height, 0, 0)
	return jvm.VoidValue(), nil
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
	case KeyReleased:
		data.game.keyStates &^= bit
	}
	return !data.game.suppressKeys
}
