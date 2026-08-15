package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// XOR mode draws the difference between the colour and what is already there.
// One title family brackets every sprite with a pair of XOR fills in black,
// which as XOR is a pair of no-ops and as a solid fill is a black rectangle
// drawn over the sprite twice — its title logo, its icons and its characters.
func TestXORModeFillsWithTheDifference(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)

	fill := func(rgb int32) {
		t.Helper()
		if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(rgb)}); err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeGraphicsFillRect(runtime, client.JVM(),
			[]jvm.Value{receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(4), jvm.IntValue(4)}); err != nil {
			t.Fatal(err)
		}
	}
	setXOR := func(on int32) {
		t.Helper()
		if _, err := runtimeGraphicsSetXORMode(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(on)}); err != nil {
			t.Fatal(err)
		}
	}

	fill(0xffffff)
	white := readScreenPixel(t, runtime, state, 1, 1)
	if white == 0 {
		t.Fatal("the opaque fill drew nothing")
	}

	// Black is the colour the title family XORs with, and XOR with zero leaves
	// the destination exactly as it was.
	setXOR(1)
	fill(0x000000)
	if pixel := readScreenPixel(t, runtime, state, 1, 1); pixel != white {
		t.Fatalf("an XOR fill in black changed the pixel to %#04x, want %#04x", pixel, white)
	}

	// A non-zero colour is the difference, and applying it twice is the
	// identity — which is what the bracket around a sprite relies on.
	fill(0xffffff)
	if pixel := readScreenPixel(t, runtime, state, 1, 1); pixel != white^0xffff {
		t.Fatalf("an XOR fill in white gave %#04x, want %#04x", pixel, white^0xffff)
	}
	fill(0xffffff)
	if pixel := readScreenPixel(t, runtime, state, 1, 1); pixel != white {
		t.Fatalf("two XOR fills in white gave %#04x, want the original %#04x", pixel, white)
	}

	// Leaving the mode is what makes a fill opaque again.
	setXOR(0)
	fill(0x000000)
	if pixel := readScreenPixel(t, runtime, state, 1, 1); pixel != 0 {
		t.Fatalf("an opaque fill in black gave %#04x, want 0", pixel)
	}
}
