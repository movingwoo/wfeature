package skt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/curve"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// curveGraphics answers a Graphics over a surface of its own, which is all a
// drawing call needs: the receiver carries the whole drawing state.
func curveGraphics(width, height int) (*graphicsContext, jvm.Value) {
	context := &graphicsContext{
		pixels:     make([]byte, width*height*4),
		width:      width,
		height:     height,
		deviceClip: paintRect{maxX: width, maxY: height},
		clip:       paintRect{maxX: width, maxY: height},
		color:      0xffffff,
		active:     true,
	}
	object := &jvm.Object{
		ClassName: midp.GraphicsClass,
		Fields:    make(map[string]jvm.Value),
		Native:    context,
	}
	return context, jvm.ReferenceValue(object)
}

func (context *graphicsContext) drawn(x, y int) bool {
	index := (y*context.width + x) * 4
	return context.pixels[index] != 0 || context.pixels[index+1] != 0 || context.pixels[index+2] != 0
}

// None of the four curve calls existed on this platform, so a MIDlet reaching
// one failed to resolve the method and the title ended there. Zero degrees is
// three o'clock and positive angles run counter-clockwise.
func TestGraphicsFillArcCutsThePieItNames(t *testing.T) {
	runtime := &Runtime{}
	context, receiver := curveGraphics(20, 20)

	fill := runtime.graphicsCurve(curve.FillArc)
	if _, err := fill(nil, []jvm.Value{
		receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(20), jvm.IntValue(20),
		jvm.IntValue(0), jvm.IntValue(90),
	}); err != nil {
		t.Fatal(err)
	}

	if !context.drawn(14, 5) {
		t.Fatal("the first quadrant of the pie was not filled")
	}
	if context.drawn(5, 5) {
		t.Fatal("the second quadrant was filled, so the sweep is wrong")
	}
	if context.drawn(14, 15) {
		t.Fatal("the fourth quadrant was filled, so the angles are not counter-clockwise")
	}
}

// A rounded rectangle takes corner diameters, so a diameter equal to the side
// makes the shape an ellipse rather than overflowing it.
func TestGraphicsFillRoundRectRoundsItsCorners(t *testing.T) {
	runtime := &Runtime{}
	context, receiver := curveGraphics(24, 8)

	fill := runtime.graphicsCurve(curve.FillRoundRect)
	if _, err := fill(nil, []jvm.Value{
		receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(24), jvm.IntValue(8),
		jvm.IntValue(24), jvm.IntValue(8),
	}); err != nil {
		t.Fatal(err)
	}

	if !context.drawn(12, 4) || !context.drawn(12, 0) {
		t.Fatal("the ellipse's middle or top was not filled")
	}
	if context.drawn(0, 0) || context.drawn(23, 7) {
		t.Fatal("a corner was filled, so the shape is still a rectangle")
	}
}

// A curve goes through the same translated, clipped fill the rectangle calls
// use, so the clip the title set cuts it.
func TestGraphicsCurveIsClipped(t *testing.T) {
	runtime := &Runtime{}
	context, receiver := curveGraphics(20, 20)
	context.clip = paintRect{maxX: 20, maxY: 10}

	fill := runtime.graphicsCurve(curve.FillArc)
	if _, err := fill(nil, []jvm.Value{
		receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(20), jvm.IntValue(20),
		jvm.IntValue(0), jvm.IntValue(360),
	}); err != nil {
		t.Fatal(err)
	}

	if !context.drawn(10, 5) {
		t.Fatal("the circle was not filled inside the clip")
	}
	if context.drawn(10, 15) {
		t.Fatal("the circle was filled outside the clip")
	}
}

// A Graphics that is not one of this runtime's is refused rather than drawn
// into, the same way every other drawing call refuses it.
func TestGraphicsCurveRefusesAForeignReceiver(t *testing.T) {
	runtime := &Runtime{}
	fill := runtime.graphicsCurve(curve.FillArc)
	if _, err := fill(nil, []jvm.Value{
		jvm.ReferenceValue(nil), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(4), jvm.IntValue(4),
		jvm.IntValue(0), jvm.IntValue(90),
	}); err == nil {
		t.Fatal("a null Graphics was accepted")
	}
}
