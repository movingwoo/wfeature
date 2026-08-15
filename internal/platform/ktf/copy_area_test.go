package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// copyArea on this platform takes the destination first — `copyArea(dx, dy,
// sx, sy, w, h)` — and not the MIDP order that ends with the destination and
// an anchor. A scrolling title shifts its whole background surface with it
// once a frame, so reading the arguments in MIDP order copies a rectangle of
// no size and the surface never moves: the parts of the scene that repeat look
// unaffected and everything that does not repeat stops arriving.
func TestCopyAreaTakesDestinationFirst(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)

	fill := func(rgb, x, y, width, height int32) {
		t.Helper()
		if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(rgb)}); err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeGraphicsFillRect(runtime, client.JVM(), []jvm.Value{
			receiver, jvm.IntValue(x), jvm.IntValue(y), jvm.IntValue(width), jvm.IntValue(height),
		}); err != nil {
			t.Fatal(err)
		}
	}
	copyArea := func(values ...int32) {
		t.Helper()
		arguments := []jvm.Value{receiver}
		for _, value := range values {
			arguments = append(arguments, jvm.IntValue(value))
		}
		if _, err := runtimeGraphicsCopyArea(runtime, client.JVM(), arguments); err != nil {
			t.Fatal(err)
		}
	}

	fill(0x000000, 0, 0, 16, 4)
	fill(0xffffff, 4, 0, 4, 4)
	white := readScreenPixel(t, runtime, state, 4, 1)
	if white == 0 {
		t.Fatal("the fill drew nothing")
	}

	// Move the marked column two pixels left, carrying the surface with it.
	copyArea(0, 0, 2, 0, 16, 4)
	if pixel := readScreenPixel(t, runtime, state, 2, 1); pixel != white {
		t.Fatalf("after the shift the mark is not at x=2: %#04x, want %#04x", pixel, white)
	}
	if pixel := readScreenPixel(t, runtime, state, 4, 1); pixel != white {
		t.Fatalf("the mark is four wide, so x=4 should still hold it: %#04x", pixel)
	}
	if pixel := readScreenPixel(t, runtime, state, 6, 1); pixel != 0 {
		t.Fatalf("the mark moved left, so x=6 should be black: %#04x", pixel)
	}
}

// A copy inside one surface has to read what the surface held before the copy
// began. Copying to the right one pixel at a time reads pixels the same copy
// has already written unless the source is taken first, which smears the
// leading column across the whole rectangle.
func TestCopyAreaOverlappingItselfDoesNotSmear(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)

	fill := func(rgb, x, y, width, height int32) {
		t.Helper()
		if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(rgb)}); err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeGraphicsFillRect(runtime, client.JVM(), []jvm.Value{
			receiver, jvm.IntValue(x), jvm.IntValue(y), jvm.IntValue(width), jvm.IntValue(height),
		}); err != nil {
			t.Fatal(err)
		}
	}

	fill(0x000000, 0, 0, 8, 2)
	fill(0xffffff, 0, 0, 1, 2)
	white := readScreenPixel(t, runtime, state, 0, 0)

	// Shift a four-pixel run one pixel right: the single white column must end
	// up one column wide, not painted over everything it passes.
	if _, err := runtimeGraphicsCopyArea(runtime, client.JVM(), []jvm.Value{
		receiver, jvm.IntValue(1), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(4), jvm.IntValue(2),
	}); err != nil {
		t.Fatal(err)
	}
	if pixel := readScreenPixel(t, runtime, state, 1, 0); pixel != white {
		t.Fatalf("the moved column is not at x=1: %#04x, want %#04x", pixel, white)
	}
	for x := uint32(2); x < 5; x++ {
		if pixel := readScreenPixel(t, runtime, state, x, 0); pixel != 0 {
			t.Fatalf("the copy smeared the column to x=%d: %#04x, want black", x, pixel)
		}
	}
}

// The clip moves the destination corner, and the source corner has to move
// with it. A title that shifts its whole surface left passes a destination
// that starts off the edge, and holding the source still there copies the
// wrong column into the visible one — the surface stops moving.
func TestCopyAreaClippedDestinationMovesItsSource(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)

	fill := func(rgb, x, y, width, height int32) {
		t.Helper()
		if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(rgb)}); err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeGraphicsFillRect(runtime, client.JVM(), []jvm.Value{
			receiver, jvm.IntValue(x), jvm.IntValue(y), jvm.IntValue(width), jvm.IntValue(height),
		}); err != nil {
			t.Fatal(err)
		}
	}

	width := int32(state.target.width)
	fill(0x000000, 0, 0, width, 2)
	fill(0xffffff, 4, 0, 1, 2)
	white := readScreenPixel(t, runtime, state, 4, 0)

	// The whole surface two pixels left, the way a scrolling background moves.
	if _, err := runtimeGraphicsCopyArea(runtime, client.JVM(), []jvm.Value{
		receiver, jvm.IntValue(-2), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(0),
		jvm.IntValue(width), jvm.IntValue(2),
	}); err != nil {
		t.Fatal(err)
	}
	if pixel := readScreenPixel(t, runtime, state, 2, 0); pixel != white {
		t.Fatalf("the mark did not move to x=2: %#04x, want %#04x", pixel, white)
	}
	if pixel := readScreenPixel(t, runtime, state, 4, 0); pixel != 0 {
		t.Fatalf("the mark is still at x=4, so the surface did not move: %#04x", pixel)
	}
}
