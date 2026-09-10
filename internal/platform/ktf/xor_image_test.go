package ktf

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// encodeInkGlyphPNG builds the shape a title's own font sheet has: a two-entry
// palette whose first colour is fully transparent and whose second is black
// ink, which PNG carries as a tRNS chunk. Column 0 is the transparent one and
// column 1 the ink, so one image carries both cases a composite has to get
// right.
func encodeInkGlyphPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	source := image.NewPaletted(image.Rect(0, 0, width, height), color.Palette{
		color.NRGBA{},
		color.NRGBA{A: 0xff},
	})
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x == 0 {
				continue
			}
			source.SetColorIndex(x, y, 1)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// XOR is a mode of every drawing operation, and drawing a picture is one of
// them.
//
// This is how one title draws Korean at all. Its font is not a font: it is
// eight strips of black-on-transparent jamo, and it has no way to ask for them
// in a colour. So it fills a small image with the ink colour, XORs that image
// over the cell, draws the jamo strip normally, and XORs the same image again.
// The first pass inverts the ground by the ink, the strip writes zero where its
// ink lands, and the second pass turns those zeroes into the ink colour and
// returns everything else to what it was.
//
// A drawImage that ignores the mode makes the third step paint over the second,
// so every syllable comes out a solid block of the ink colour — which is what
// the whole of that title's dialogue was.
func TestAnXORImagePairComposesInkOverTheGround(t *testing.T) {
	client, runtime := newTestRuntime(t)
	screen, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(screen)
	state := screen.Native.(*runtimeGraphicsState)

	const inkRGB = 0xff8000
	setColor := func(target jvm.Value, rgb int32) {
		t.Helper()
		if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{target, jvm.IntValue(rgb)}); err != nil {
			t.Fatal(err)
		}
	}
	fillRect := func(target jvm.Value, width, height int32) {
		t.Helper()
		if _, err := runtimeGraphicsFillRect(runtime, client.JVM(), []jvm.Value{
			target, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(width), jvm.IntValue(height),
		}); err != nil {
			t.Fatal(err)
		}
	}
	setXOR := func(on int32) {
		t.Helper()
		if _, err := runtimeGraphicsSetXORMode(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(on)}); err != nil {
			t.Fatal(err)
		}
	}
	drawImage := func(value jvm.Value) {
		t.Helper()
		if _, err := runtimeGraphicsDrawImage(runtime, client.JVM(), []jvm.Value{
			receiver, value, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(0),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// A ground that is neither the ink nor black, so a pass that forgets to put
	// it back is visible.
	setColor(receiver, 0x2040c0)
	fillRect(receiver, 4, 4)
	ground := readScreenPixel(t, runtime, state, 0, 0)
	if ground == 0 {
		t.Fatal("the ground fill did not reach the screen")
	}

	// The ink carrier: a mutable image the size of one cell, filled with the
	// colour the text is meant to come out in.
	created, err := runtimeImageCreateSized(runtime, client.JVM(), []jvm.Value{jvm.IntValue(2), jvm.IntValue(2)})
	if err != nil {
		t.Fatal(err)
	}
	inkImage, err := created.Reference()
	if err != nil {
		t.Fatal(err)
	}
	inkGraphics, err := runtimeImageGetGraphics(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(inkImage)})
	if err != nil {
		t.Fatal(err)
	}
	setColor(inkGraphics, inkRGB)
	fillRect(inkGraphics, 2, 2)
	inkTarget, err := inkGraphics.Reference()
	if err != nil {
		t.Fatal(err)
	}
	ink := readImagePixel(t, runtime, inkTarget, 0, 0)
	if ink == 0 || ink == ground {
		t.Fatalf("the ink carrier holds %#04x, which is not a colour this test can tell apart", ink)
	}

	glyph, err := runtimeImageFromEncoded(runtime, encodeInkGlyphPNG(t, 2, 2))
	if err != nil {
		t.Fatal(err)
	}

	setXOR(1)
	drawImage(jvm.ReferenceValue(inkImage))
	setXOR(0)
	drawImage(glyph)
	setXOR(1)
	drawImage(jvm.ReferenceValue(inkImage))
	setXOR(0)

	if pixel := readScreenPixel(t, runtime, state, 1, 0); pixel != ink {
		t.Errorf("the glyph's ink came out %#04x, want the ink colour %#04x", pixel, ink)
	}
	if pixel := readScreenPixel(t, runtime, state, 0, 0); pixel != ground {
		t.Errorf("the glyph's transparent column came out %#04x, want the ground %#04x", pixel, ground)
	}
}

// copyArea is a drawing operation too, and the specification says the mode
// applies to all of them. Nothing local copies in XOR mode; the test is here
// because the call now reads the mode, and a path that reads a flag without a
// test is one the next change can quietly stop reading.
func TestCopyAreaDrawsTheDifferenceInXORMode(t *testing.T) {
	client, runtime := newTestRuntime(t)
	screen, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(screen)
	state := screen.Native.(*runtimeGraphicsState)

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
	copyArea := func(destinationX, destinationY, sourceX, sourceY, width, height int32) {
		t.Helper()
		if _, err := runtimeGraphicsCopyArea(runtime, client.JVM(), []jvm.Value{
			receiver, jvm.IntValue(destinationX), jvm.IntValue(destinationY),
			jvm.IntValue(sourceX), jvm.IntValue(sourceY), jvm.IntValue(width), jvm.IntValue(height),
		}); err != nil {
			t.Fatal(err)
		}
	}

	fill(0xff0000, 0, 0, 2, 1) // the source
	fill(0x0000ff, 2, 0, 2, 1) // where it is copied to
	source := readScreenPixel(t, runtime, state, 0, 0)
	destination := readScreenPixel(t, runtime, state, 2, 0)

	if _, err := runtimeGraphicsSetXORMode(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(1)}); err != nil {
		t.Fatal(err)
	}
	copyArea(2, 0, 0, 0, 2, 1)
	if pixel := readScreenPixel(t, runtime, state, 2, 0); pixel != source^destination {
		t.Fatalf("an XOR copy gave %#04x, want the difference %#04x", pixel, source^destination)
	}

	// Leaving the mode copies the pixels themselves again.
	if _, err := runtimeGraphicsSetXORMode(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0)}); err != nil {
		t.Fatal(err)
	}
	copyArea(2, 0, 0, 0, 2, 1)
	if pixel := readScreenPixel(t, runtime, state, 2, 0); pixel != source {
		t.Fatalf("an ordinary copy gave %#04x, want the source %#04x", pixel, source)
	}
}
