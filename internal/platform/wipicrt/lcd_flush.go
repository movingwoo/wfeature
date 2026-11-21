package wipicrt

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// flushLCD copies the requested rectangle into the retained LCD image. The
// source remains untouched, including when it is the screen framebuffer.
func (runtime *Runtime) flushLCD(thread *armcore.Thread) error {
	var args [6]uint32
	for i := range args {
		value, err := runtime.WIPICArgument(thread, i)
		if err != nil {
			return err
		}
		args[i] = value
	}
	if args[0] != 0 {
		return fmt.Errorf("unsupported %s LCD index %d", runtime.platformName(), args[0])
	}
	source, err := runtime.ReadWIPICFramebuffer(args[1])
	if err != nil {
		return err
	}
	return runtime.FlushLCDRegion(source, int32(args[2]), int32(args[3]), int32(args[4]), int32(args[5]))
}

func (runtime *Runtime) FlushLCDRegion(source WIPICFramebuffer, x, y, w, h int32) error {
	if w <= 0 || h <= 0 {
		return nil
	}
	handle, err := runtime.WIPICGetScreenFramebuffer()
	if err != nil {
		return err
	}
	screen, err := runtime.ReadWIPICFramebuffer(handle)
	if err != nil {
		return err
	}
	width, height := int(screen.Width), int(screen.Height)
	if runtime.Client.FramePending {
		if err := runtime.ConvertScreen(); err != nil {
			return err
		}
	}
	frame := append([]byte(nil), runtime.Client.FrameRGBA...)
	if len(frame) != width*height*4 {
		frame = make([]byte, width*height*4)
		for i := 3; i < len(frame); i += 4 {
			frame[i] = 255
		}
	}
	x0, y0 := max(int64(x), 0), max(int64(y), 0)
	x1 := min(int64(x)+int64(w), int64(width), int64(source.Width))
	y1 := min(int64(y)+int64(h), int64(height), int64(source.Height))
	if x1 <= x0 || y1 <= y0 {
		return nil
	}
	row := make([]byte, int(x1-x0)*2)
	for yy := y0; yy < y1; yy++ {
		if err := runtime.Client.core.Memory().Read(source.Pixels+uint32(yy)*source.Bpl+uint32(x0)*2, row); err != nil {
			return err
		}
		for xx := x0; xx < x1; xx++ {
			pixel := binary.LittleEndian.Uint16(row[(xx-x0)*2:])
			i := (int(yy)*width + int(xx)) * 4
			frame[i], frame[i+1], frame[i+2], frame[i+3] = byte((pixel>>11&31)<<3), byte((pixel>>5&63)<<2), byte((pixel&31)<<3), 255
		}
	}
	runtime.recordScreenFlush()
	runtime.Client.FrameRGBA, runtime.Client.FrameWidth, runtime.Client.FrameHeight = frame, width, height
	runtime.Client.FramePending = false
	runtime.Client.PartialFrame = true
	runtime.Client.WholeFrameFlush = source.Pixels == screen.Pixels && x0 == 0 && y0 == 0 && x1 == int64(width) && y1 == int64(height)
	runtime.Client.TitleRepaint = false
	return runtime.publishIntermediateFrame()
}

// BeginCardPaint preserves an explicit flush and avoids replacing a partial
// LCD image when paint leaves the screen buffer unchanged. Snapshot only while
// a partial frame is retained; ordinary full-frame painting keeps its old cost.
//
// One flush is not retained against the paint the title asks for after it: a
// flush of the whole screen out of the screen framebuffer, followed by the
// title's own MC_grpRepaint or Card.repaint. Such a flush keeps nothing of an
// earlier image, and a title that draws each frame straight into the screen
// from a timer, asks for a repaint and has a paint entry that draws nothing
// needs that paint to show the screen; retained, its last whole-screen flush
// stayed on the Host for the rest of the run. A paint nobody in the title
// asked for, the Host's own round paint or the first paint of a card that was
// shown, still keeps the flush: what the title left in the screen after it is
// not a frame it published.
func (runtime *Runtime) BeginCardPaint() (func() error, error) {
	flushes := runtime.Client.FlushCount
	var before []byte
	var framebuffer WIPICFramebuffer
	if runtime.Client.PartialFrame && !(runtime.Client.WholeFrameFlush && runtime.Client.TitleRepaint) {
		var err error
		framebuffer, err = runtime.ReadWIPICFramebuffer(runtime.ScreenFramebuffer)
		if err != nil {
			return nil, err
		}
		before = make([]byte, int(framebuffer.Bpl)*int(framebuffer.Height))
		if err := runtime.Client.core.Memory().Read(framebuffer.Pixels, before); err != nil {
			return nil, err
		}
	}
	return func() error {
		if runtime.Client.FlushCount != flushes {
			return nil
		}
		if before != nil {
			after := make([]byte, len(before))
			if err := runtime.Client.core.Memory().Read(framebuffer.Pixels, after); err != nil {
				return err
			}
			if bytes.Equal(before, after) {
				return nil
			}
		}
		return runtime.PresentScreen()
	}, nil
}
