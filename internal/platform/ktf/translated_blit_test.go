package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The clip answers in device coordinates and a drawing call's arguments are in
// the caller's, so an image blit has to move the corner it measures its source
// from before it subtracts the two. It did not, and a translated context read
// the source from the translation's own offset: one local title draws a
// 132x123 dialog through a Graphics translated by 28,28, so it took the
// dialog's pixels from (28,28) of itself — every drawn pixel came from the
// wrong place, and the last 28 rows came from past the end of the pixel buffer
// as a band of noise across the screen.
//
// The test draws a mutable image with a marked first pixel through a
// translated context. What lands at the translated corner is the source's
// corner or it is the bug.
func TestATranslatedBlitReadsItsSourceFromTheCorner(t *testing.T) {
	client, runtime := newTestRuntime(t)
	screen, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	state := screen.Native.(*runtimeGraphicsState)

	// A small mutable image, white by creation, with one corner pixel marked.
	created, err := runtimeImageCreateSized(runtime, client.JVM(), []jvm.Value{jvm.IntValue(8), jvm.IntValue(8)})
	if err != nil {
		t.Fatal(err)
	}
	image, err := created.Reference()
	if err != nil {
		t.Fatal(err)
	}
	imageGraphics, err := runtimeImageGetGraphics(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(image)})
	if err != nil {
		t.Fatal(err)
	}
	target, err := imageGraphics.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(target), jvm.IntValue(0xff0000)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsFillRect(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(target), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(1), jvm.IntValue(1),
	}); err != nil {
		t.Fatal(err)
	}
	mark := readImagePixel(t, runtime, target, 0, 0)
	if mark == readImagePixel(t, runtime, target, 4, 4) {
		t.Fatal("the mark is the same colour as the rest of the image")
	}

	// Translate, then draw at the origin: the image lands at the translation.
	if _, err := runtimeGraphicsTranslate(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(screen), jvm.IntValue(5), jvm.IntValue(3),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsDrawImage(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(screen), jvm.ReferenceValue(image), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(20),
	}); err != nil {
		t.Fatal(err)
	}
	if pixel := readScreenPixel(t, runtime, state, 5, 3); pixel != mark {
		t.Fatalf("the translated corner holds %#04x, want the source corner %#04x", pixel, mark)
	}
}

// readImagePixel reads one pixel of a mutable image's own framebuffer.
func readImagePixel(t *testing.T, runtime *initializationRuntime, graphics *jvm.Object, x, y uint32) uint16 {
	t.Helper()
	state, ok := graphics.Native.(*runtimeGraphicsState)
	if !ok {
		t.Fatal("the Graphics carries no drawing state")
	}
	return readScreenPixel(t, runtime, state, x, y)
}
