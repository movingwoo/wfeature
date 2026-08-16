package lgt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/curve"
)

// javaCurveFixture answers a surface large enough to hold a curve and a white
// Graphics drawing into it. The fixture screen is too small for a shape whose
// quadrants have to be told apart, and an off-screen surface is what a title
// draws a panel into anyway.
func javaCurveFixture(t *testing.T, width, height int) (*framebuffer, *Client, uint32) {
	t.Helper()
	client := fixtureClient(t)
	target, err := client.newFramebuffer(width, height, false)
	if err != nil {
		t.Fatal(err)
	}
	object, err := client.newJavaGraphics(target.handle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaSetColorPacked(client, nil, nil, []uint32{object, 0xffffff}); err != nil {
		t.Fatal(err)
	}
	return target, client, object
}

// A rounded rectangle whose corner diameters are its own width and height is
// an ellipse. This call used to be answered with the plain rectangle, so every
// panel and frame a title rounded came out square.
func TestJavaFillRoundRectRoundsItsCorners(t *testing.T) {
	target, client, object := javaCurveFixture(t, 24, 8)

	fill := javaCurve(curve.FillRoundRect)
	if _, err := fill(client, nil, nil, []uint32{object, 0, 0, 24, 8, 24, 8}); err != nil {
		t.Fatal(err)
	}

	at := func(x, y int) uint16 { return target.pixels[y*target.width+x] }
	if at(12, 4) == 0 || at(12, 0) == 0 {
		t.Fatal("the ellipse's middle or top was not filled")
	}
	if at(0, 0) != 0 || at(23, 7) != 0 {
		t.Fatal("a corner was filled, so the shape is still a rectangle")
	}
}

// The arc pair had no implementation at all, so a title asking for one got
// nothing drawn. Zero degrees is three o'clock and positive angles run
// counter-clockwise.
func TestJavaFillArcCutsThePieItNames(t *testing.T) {
	target, client, object := javaCurveFixture(t, 20, 20)

	fill := javaCurve(curve.FillArc)
	if _, err := fill(client, nil, nil, []uint32{object, 0, 0, 20, 20, 0, 90}); err != nil {
		t.Fatal(err)
	}

	at := func(x, y int) uint16 { return target.pixels[y*target.width+x] }
	if at(14, 5) == 0 {
		t.Fatal("the first quadrant of the pie was not filled")
	}
	if at(5, 5) != 0 {
		t.Fatal("the second quadrant was filled, so the sweep is wrong")
	}
	if at(14, 15) != 0 {
		t.Fatal("the fourth quadrant was filled, so the angles are not counter-clockwise")
	}
}

// A curve is drawn through the same state the rectangle calls use, so the
// translation the title set applies to it too.
func TestJavaCurveHonoursTheTranslation(t *testing.T) {
	target, client, object := javaCurveFixture(t, 24, 24)

	if _, err := javaTranslate(client, nil, nil, []uint32{object, 10, 10}); err != nil {
		t.Fatal(err)
	}
	fill := javaCurve(curve.FillArc)
	if _, err := fill(client, nil, nil, []uint32{object, 0, 0, 8, 8, 0, 360}); err != nil {
		t.Fatal(err)
	}

	at := func(x, y int) uint16 { return target.pixels[y*target.width+x] }
	if at(14, 14) == 0 {
		t.Fatal("the circle was not drawn at the translated origin")
	}
	if at(4, 4) != 0 {
		t.Fatal("the circle was drawn at the untranslated origin")
	}
}
