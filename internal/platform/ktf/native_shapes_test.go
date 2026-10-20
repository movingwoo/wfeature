package ktf

import (
	"encoding/binary"
	"image/color"
	"testing"

	"github.com/movingwoo/wfeature/internal/curve"
)

// nativeShapeRecord writes the halfwords of one shape record into guest memory
// and answers where.
func nativeShapeRecord(t *testing.T, platform *NativePlatform, values ...int16) uint32 {
	t.Helper()
	record := make([]byte, 0, len(values)*2)
	for _, value := range values {
		record = binary.LittleEndian.AppendUint16(record, uint16(value))
	}
	address, err := platform.client.Allocate(uint32(len(record)))
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.client.core.Memory().Write(address, record); err != nil {
		t.Fatal(err)
	}
	return address
}

// nativeClipRecord is the record the title clips with: a shape byte, then the
// rectangle from offset 4.
func nativeClipRecord(t *testing.T, platform *NativePlatform, shape byte, x, y, width, height int16) uint32 {
	t.Helper()
	return nativeShapeRecord(t, platform, int16(shape), 0, x, y, width, height)
}

// TestNativeShapesDrawInThePenInsideTheClip covers the second drawing
// interface the way a title uses it: a pen colour before each shape, a point
// size before its points, and a clip that every shape lands inside.
func TestNativeShapesDrawInThePenInsideTheClip(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	frame, _ := platform.Frame()
	pen := color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	nativeCall(t, platform.shapesColour, 0, 0x12, 0x34, 0x56)
	inked := func(x, y int) bool { return frame.RGBAAt(x, y) == pen }

	nativeCall(t, platform.shapesPoint, 0, nativeShapeRecord(t, platform, 5, 6))
	if !inked(5, 6) || inked(6, 6) || inked(5, 7) {
		t.Error("a point before any size was set is not one pixel at its place")
	}
	nativeCall(t, platform.shapesPointSize, 0, 2)
	nativeCall(t, platform.shapesPoint, 0, nativeShapeRecord(t, platform, 10, 10))
	for _, at := range [][2]int{{10, 10}, {11, 10}, {10, 11}, {11, 11}} {
		if !inked(at[0], at[1]) {
			t.Errorf("a point of size 2 left %v bare", at)
		}
	}
	if inked(9, 9) || inked(12, 12) {
		t.Error("a point of size 2 is larger than two pixels a side")
	}

	// A line visits every pixel it crosses once, both ends included, and steps
	// diagonally where it crosses both ways.
	nativeCall(t, platform.shapesLine, 0, nativeShapeRecord(t, platform, 20, 20, 25, 22))
	marked := 0
	for y := 18; y < 25; y++ {
		for x := 18; x < 28; x++ {
			if inked(x, y) {
				marked++
			}
		}
	}
	if !inked(20, 20) || !inked(25, 22) || marked != 6 {
		t.Errorf("the line from 20,20 to 25,22 marked %d pixels, want its two ends among 6", marked)
	}

	// A circle is the arc the WIPI C table and Java's Graphics draw, all the
	// way round the square its radius makes about the centre, and not filled.
	nativeCall(t, platform.shapesCircle, 0, nativeShapeRecord(t, platform, 50, 50, 4))
	ring := map[[2]int]bool{}
	if err := curve.DrawArc(46, 46, 8, 8, 0, 360, func(span curve.Span) error {
		for x := span.X; x < span.X+span.Width; x++ {
			ring[[2]int{int(x), int(span.Y)}] = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for y := 40; y < 61; y++ {
		for x := 40; x < 61; x++ {
			if inked(x, y) != ring[[2]int{x, y}] {
				t.Errorf("the circle inked %d,%d = %t, the arc %t", x, y, inked(x, y), ring[[2]int{x, y}])
			}
		}
	}
	if !inked(54, 50) || !inked(46, 50) || !inked(50, 46) || inked(50, 50) {
		t.Error("the circle is not a ring of radius 4 round its centre")
	}
	nativeCall(t, platform.shapesCircle, 0, nativeShapeRecord(t, platform, 70, 70, 0))
	if !inked(70, 70) || inked(71, 70) {
		t.Error("a circle of no radius is not its centre")
	}

	// The clip answers that it took, and nothing lands outside it.
	if got := nativeCall(t, platform.shapesClip, 0, nativeClipRecord(t, platform, nativeClipRectangle, 0, 0, 30, 30), 0); got == 0 {
		t.Fatal("a rectangle clip was refused")
	}
	nativeCall(t, platform.shapesLine, 0, nativeShapeRecord(t, platform, 25, 40, 35, 40))
	nativeCall(t, platform.shapesLine, 0, nativeShapeRecord(t, platform, 25, 28, 35, 28))
	if inked(30, 40) || !inked(29, 28) || inked(30, 28) {
		t.Error("a line drew past the clip")
	}
	// A shape this platform cannot clip to is refused, and the clip stays.
	if got := nativeCall(t, platform.shapesClip, 0, nativeClipRecord(t, platform, 2, 0, 0, 100, 100), 0); got != 0 {
		t.Error("a clip of another shape was taken as a rectangle")
	}
	nativeCall(t, platform.shapesPoint, 0, nativeShapeRecord(t, platform, 60, 60))
	if inked(60, 60) {
		t.Error("the refused clip replaced the one before it")
	}
	// A null record lifts the clip.
	if got := nativeCall(t, platform.shapesClip, 0, 0, 0); got == 0 {
		t.Error("lifting the clip was refused")
	}
	nativeCall(t, platform.shapesPoint, 0, nativeShapeRecord(t, platform, 60, 60))
	if !inked(60, 60) {
		t.Error("a point after the clip was lifted did not land")
	}
	// Off the screen is nowhere, not a failure.
	nativeCall(t, platform.shapesCircle, 0, nativeShapeRecord(t, platform, -100, -100, 30))
	nativeCall(t, platform.shapesLine, 0, nativeShapeRecord(t, platform, -5, -5, 300, 400))
	if got := platform.Draws(); got != 11 {
		t.Errorf("draws = %d, want one for every shape", got)
	}
}
