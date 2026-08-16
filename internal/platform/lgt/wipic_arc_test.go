package lgt

import "testing"

// The two arc slots sit in a gap between two confirmed neighbours, so what has
// to be proved here is that the platform answers them at all and reads their
// eight arguments in the documented order. An unmapped slot in this block is
// fatal, so before this a title drawing a curve simply ended.
func TestFillArcAnswersItsSlotAndFillsTheQuadrant(t *testing.T) {
	client := fixtureClient(t)

	const colour = uint16(0x07e0)
	context, pointer := polygonFixtureContext(t, client, colour)

	// A quarter turn from three o'clock over the whole 8x8 surface. Zero
	// degrees is three o'clock and positive angles run counter-clockwise, so
	// this is the top-right quadrant of the ellipse and nothing else.
	callDrawSlot(t, client, slotFillArc, context.target.handle, 0, 0, 8, 8, 0, 90, pointer)

	target := context.target
	if got := target.pixels[2*target.width+5]; got != colour {
		t.Fatalf("the first quadrant was not filled: pixel = %#x, want %#x", got, colour)
	}
	if got := target.pixels[2*target.width+2]; got != 0 {
		t.Fatalf("the second quadrant was filled: pixel = %#x, want 0", got)
	}
	if got := target.pixels[5*target.width+5]; got != 0 {
		t.Fatalf("the fourth quadrant was filled, so the angles are not counter-clockwise: pixel = %#x", got)
	}
}

// The outline form shares the argument shape and draws the curve alone, which
// is the difference worth pinning: both slots would otherwise look identical
// from the guest's side.
func TestDrawArcLeavesThePieInsideAlone(t *testing.T) {
	client := fixtureClient(t)

	const colour = uint16(0xf800)
	context, pointer := polygonFixtureContext(t, client, colour)

	callDrawSlot(t, client, slotDrawArc, context.target.handle, 0, 0, 8, 8, 0, 360, pointer)

	target := context.target
	if got := target.pixels[4*target.width+4]; got != 0 {
		t.Fatalf("the middle of the circle was drawn, so this is a fill: pixel = %#x", got)
	}
	drawn := 0
	for _, pixel := range target.pixels {
		if pixel == colour {
			drawn++
		}
	}
	if drawn == 0 {
		t.Fatal("the outline drew nothing at all")
	}
}
