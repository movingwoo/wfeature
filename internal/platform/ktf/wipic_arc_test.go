package ktf

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The two arc calls take eight arguments, so the last four arrive on the stack
// and the context is the eighth rather than the sixth. Reading it from the
// rectangle calls' position would take an angle for a context, so the argument
// order is the thing worth pinning here — that, and that the table answers
// these functions at all: an unimplemented graphics function is fatal, so
// before this a title drawing a curve ended there.
func callArcFunction(t *testing.T, runtime *initializationRuntime, function uint32, arguments ...uint32) {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	stack := platformDataBase + 0x8000
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	for index, value := range arguments {
		if index < 4 {
			if err := thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
			continue
		}
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], value)
		if err := runtime.client.core.Memory().Write(stack+uint32(index-4)*4, word[:]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.handleWIPICTableCall(thread, wipicTableGraphics, function); err != nil {
		t.Fatalf("graphics function %d: %v", function, err)
	}
}

// arcFixture answers a surface to draw on and a graphics context in guest
// memory carrying the foreground colour, since a drawing call reads its colour
// out of the game's own record rather than taking one.
func arcFixture(t *testing.T, runtime *initializationRuntime, colour uint16) (uint32, wipicFramebuffer, uint32) {
	t.Helper()
	handle, err := runtime.newWIPICFramebufferRecord(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := runtime.readWIPICFramebuffer(handle)
	if err != nil {
		t.Fatal(err)
	}
	contextAddress := platformDataBase + 0x9000
	record := make([]byte, 32)
	binary.LittleEndian.PutUint32(record[12:], uint32(colour))
	if err := runtime.client.core.Memory().Write(contextAddress, record); err != nil {
		t.Fatal(err)
	}
	return handle, buffer, contextAddress
}

func arcPixel(t *testing.T, runtime *initializationRuntime, buffer wipicFramebuffer, x, y uint32) uint16 {
	t.Helper()
	var pixel [2]byte
	address := buffer.pixels + y*buffer.bpl + x*2
	if err := runtime.client.core.Memory().Read(address, pixel[:]); err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint16(pixel[:])
}

// Zero degrees is three o'clock and positive angles run counter-clockwise, so
// a quarter turn from zero is the top-right quadrant and nothing else.
func TestWIPICFillArcFillsTheQuadrantItNames(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.currentThread, runtime.currentContext = client.thread, context.Background()

	const colour = uint16(0x07e0)
	handle, buffer, contextAddress := arcFixture(t, runtime, colour)

	callArcFunction(t, runtime, 16, handle, 0, 0, 8, 8, 0, 90, contextAddress)

	if got := arcPixel(t, runtime, buffer, 5, 2); got != colour {
		t.Fatalf("the first quadrant was not filled: pixel = %#x, want %#x", got, colour)
	}
	if got := arcPixel(t, runtime, buffer, 2, 2); got != 0 {
		t.Fatalf("the second quadrant was filled: pixel = %#x, want 0", got)
	}
	if got := arcPixel(t, runtime, buffer, 5, 5); got != 0 {
		t.Fatalf("the fourth quadrant was filled, so the angles are not counter-clockwise: pixel = %#x", got)
	}
}

// Drawing an arc is the curve alone, so a full turn leaves the middle of the
// circle untouched. It also proves the context was read from the eighth
// argument: taken from the sixth the colour would be the start angle, zero,
// and nothing would be visible at all.
func TestWIPICDrawArcIsTheCurveAndNotThePie(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.currentThread, runtime.currentContext = client.thread, context.Background()

	const colour = uint16(0xf800)
	handle, buffer, contextAddress := arcFixture(t, runtime, colour)

	callArcFunction(t, runtime, 15, handle, 0, 0, 8, 8, 0, 360, contextAddress)

	if got := arcPixel(t, runtime, buffer, 4, 4); got != 0 {
		t.Fatalf("the middle of the circle was drawn, so this is a fill: pixel = %#x", got)
	}
	drawn := 0
	for y := uint32(0); y < 8; y++ {
		for x := uint32(0); x < 8; x++ {
			if arcPixel(t, runtime, buffer, x, y) == colour {
				drawn++
			}
		}
	}
	if drawn == 0 {
		t.Fatal("the outline drew nothing, so the context was not read from the eighth argument")
	}
}
