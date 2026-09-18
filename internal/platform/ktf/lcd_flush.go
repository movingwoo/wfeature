package ktf

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// flushLCD copies the requested rectangle into the retained LCD image. The
// source remains untouched, including when it is the screen framebuffer.
func (runtime *initializationRuntime) flushLCD(thread *armcore.Thread) error {
	var args [6]uint32
	for i := range args {
		value, err := runtime.wipicArgument(thread, i)
		if err != nil {
			return err
		}
		args[i] = value
	}
	if args[0] != 0 {
		return fmt.Errorf("unsupported KTF LCD index %d", args[0])
	}
	source, err := runtime.readWIPICFramebuffer(args[1])
	if err != nil {
		return err
	}
	return runtime.flushLCDRegion(source, int32(args[2]), int32(args[3]), int32(args[4]), int32(args[5]))
}

func (runtime *initializationRuntime) flushLCDRegion(source wipicFramebuffer, x, y, w, h int32) error {
	if w <= 0 || h <= 0 {
		return nil
	}
	handle, err := runtime.wipicGetScreenFramebuffer()
	if err != nil {
		return err
	}
	screen, err := runtime.readWIPICFramebuffer(handle)
	if err != nil {
		return err
	}
	width, height := int(screen.width), int(screen.height)
	if runtime.client.framePending {
		if err := runtime.convertScreen(); err != nil {
			return err
		}
	}
	frame := append([]byte(nil), runtime.client.frame...)
	if len(frame) != width*height*4 {
		frame = make([]byte, width*height*4)
		for i := 3; i < len(frame); i += 4 {
			frame[i] = 255
		}
	}
	x0, y0 := max(int64(x), 0), max(int64(y), 0)
	x1 := min(int64(x)+int64(w), int64(width), int64(source.width))
	y1 := min(int64(y)+int64(h), int64(height), int64(source.height))
	if x1 <= x0 || y1 <= y0 {
		return nil
	}
	row := make([]byte, int(x1-x0)*2)
	for yy := y0; yy < y1; yy++ {
		if err := runtime.client.core.Memory().Read(source.pixels+uint32(yy)*source.bpl+uint32(x0)*2, row); err != nil {
			return err
		}
		for xx := x0; xx < x1; xx++ {
			pixel := binary.LittleEndian.Uint16(row[(xx-x0)*2:])
			i := (int(yy)*width + int(xx)) * 4
			frame[i], frame[i+1], frame[i+2], frame[i+3] = byte((pixel>>11&31)<<3), byte((pixel>>5&63)<<2), byte((pixel&31)<<3), 255
		}
	}
	runtime.recordScreenFlush()
	runtime.client.frame, runtime.client.frameWidth, runtime.client.frameHeight = frame, width, height
	runtime.client.framePending = false
	runtime.client.partialFrame = true
	return runtime.publishIntermediateFrame()
}

// beginCardPaint preserves an explicit flush and avoids replacing a partial
// LCD image when paint leaves the screen buffer unchanged. Snapshot only while
// a partial frame is retained; ordinary full-frame painting keeps its old cost.
func (runtime *initializationRuntime) beginCardPaint() (func() error, error) {
	flushes := runtime.client.flushCount
	var before []byte
	var framebuffer wipicFramebuffer
	if runtime.client.partialFrame {
		var err error
		framebuffer, err = runtime.readWIPICFramebuffer(runtime.screenFramebuffer)
		if err != nil {
			return nil, err
		}
		before = make([]byte, int(framebuffer.bpl)*int(framebuffer.height))
		if err := runtime.client.core.Memory().Read(framebuffer.pixels, before); err != nil {
			return nil, err
		}
	}
	return func() error {
		if runtime.client.flushCount != flushes {
			return nil
		}
		if before != nil {
			after := make([]byte, len(before))
			if err := runtime.client.core.Memory().Read(framebuffer.pixels, after); err != nil {
				return err
			}
			if bytes.Equal(before, after) {
				return nil
			}
		}
		return runtime.presentScreen()
	}, nil
}
