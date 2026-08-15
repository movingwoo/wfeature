package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// A rounded rectangle whose corner diameters are its own width and height is
// an ellipse, and that is the call one title makes seventeen thousand times a
// run for a character's ground shadow. Filled as its bounding rectangle it is
// the hard square under the character that got this looked at.
func TestFillRoundRectRoundsItsCorners(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)

	if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0xffffff)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsFillRoundRect(runtime, client.JVM(), []jvm.Value{
		receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(24), jvm.IntValue(8),
		jvm.IntValue(24), jvm.IntValue(8),
	}); err != nil {
		t.Fatal(err)
	}

	if pixel := readScreenPixel(t, runtime, state, 12, 4); pixel == 0 {
		t.Fatal("the middle of the ellipse was not drawn")
	}
	if pixel := readScreenPixel(t, runtime, state, 12, 0); pixel == 0 {
		t.Fatal("the top of the ellipse was not drawn")
	}
	if pixel := readScreenPixel(t, runtime, state, 0, 0); pixel != 0 {
		t.Fatal("the corner was drawn, so the shape is still a rectangle")
	}
	if pixel := readScreenPixel(t, runtime, state, 23, 7); pixel != 0 {
		t.Fatal("the opposite corner was drawn, so the shape is still a rectangle")
	}
}

// Corner diameters of zero are a plain rectangle, which the same call has to
// keep drawing.
func TestFillRoundRectWithoutCornersIsARectangle(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)

	if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0xffffff)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsFillRoundRect(runtime, client.JVM(), []jvm.Value{
		receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(6), jvm.IntValue(6),
		jvm.IntValue(0), jvm.IntValue(0),
	}); err != nil {
		t.Fatal(err)
	}
	if pixel := readScreenPixel(t, runtime, state, 0, 0); pixel == 0 {
		t.Fatal("a corner diameter of zero rounded a corner anyway")
	}
}

// A full-turn arc is the whole ellipse; a quarter turn from three o'clock is
// the top-right of it and nothing else, which is what settles that the angles
// run counter-clockwise from there.
func TestFillArcCutsThePieItNames(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)

	if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0xffffff)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsFillArc(runtime, client.JVM(), []jvm.Value{
		receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(20), jvm.IntValue(20),
		jvm.IntValue(0), jvm.IntValue(90),
	}); err != nil {
		t.Fatal(err)
	}

	if pixel := readScreenPixel(t, runtime, state, 14, 5); pixel == 0 {
		t.Fatal("the first quadrant of the pie was not drawn")
	}
	if pixel := readScreenPixel(t, runtime, state, 5, 5); pixel != 0 {
		t.Fatal("the second quadrant was drawn, so the sweep is wrong")
	}
	if pixel := readScreenPixel(t, runtime, state, 14, 15); pixel != 0 {
		t.Fatal("the fourth quadrant was drawn, so the angles are not counter-clockwise")
	}
	if pixel := readScreenPixel(t, runtime, state, 19, 0); pixel != 0 {
		t.Fatal("a corner outside the ellipse was drawn")
	}
}
