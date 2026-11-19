package wipicrt_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

// tableCall calls a graphics function with its arguments where a guest
// would pass them: the first four in registers and the rest on the stack.
func tableCall(t *testing.T, runtime *wipicrt.Runtime, function uint32, arguments ...uint32) uint32 {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range arguments[:min(4, len(arguments))] {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	if len(arguments) > 4 {
		stack, err := runtime.AllocateWords(arguments[4:])
		if err != nil {
			t.Fatal(err)
		}
		if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runtime.HandleWIPICTableCall(thread, wipicrt.WIPICTableGraphics, function)
	if err != nil {
		t.Fatalf("graphics function %d: %v", function, err)
	}
	return result
}

// surface makes a framebuffer holding pixels, one row of them.
func surface(t *testing.T, runtime *wipicrt.Runtime, pixels ...uint16) (uint32, wipicrt.WIPICFramebuffer) {
	t.Helper()
	handle, err := runtime.NewWIPICFramebufferRecord(uint32(len(pixels)), 1)
	if err != nil {
		t.Fatal(err)
	}
	framebuffer, err := runtime.ReadWIPICFramebuffer(handle)
	if err != nil {
		t.Fatal(err)
	}
	wipicrttest.WriteSurfacePixels(t, runtime, framebuffer, pixels)
	return handle, framebuffer
}

// keyedPNG is a two-pixel palette image whose first pixel is transparent with
// the colour one local title's art keeps there, (0, 32, 8), and whose second
// is opaque red.
func keyedPNG(t *testing.T) []byte {
	t.Helper()
	source := image.NewPaletted(image.Rect(0, 0, 2, 1), color.Palette{
		color.NRGBA{R: 0, G: 32, B: 8, A: 0},
		color.NRGBA{R: 0xff, A: 0xff},
	})
	source.SetColorIndex(1, 0, 1)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// createdImagePixels decodes encoded through MC_grpCreateImage and answers
// the pixels of the image's framebuffer.
func createdImagePixels(t *testing.T, runtime *wipicrt.Runtime, encoded []byte) []uint16 {
	t.Helper()
	result, err := runtime.AllocateWIPIC(4)
	if err != nil {
		t.Fatal(err)
	}
	data, err := runtime.AllocateWIPIC(uint32(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Client.Core().Memory().Write(data+wipicrt.WIPICAllocationOverhead, encoded); err != nil {
		t.Fatal(err)
	}
	if code := tableCall(t, runtime, 32, result+wipicrt.WIPICAllocationOverhead, data, 0, uint32(len(encoded))); code != 1 {
		t.Fatalf("MC_grpCreateImage = %#x", code)
	}
	handles, err := runtime.ReadAOTWords(result+wipicrt.WIPICAllocationOverhead, 1, "image")
	if err != nil {
		t.Fatal(err)
	}
	framebuffer, err := runtime.ReadWIPICFramebuffer(handles[0])
	if err != nil {
		t.Fatal(err)
	}
	return wipicrttest.ReadSurfacePixels(t, runtime, framebuffer)
}

// TestCreateImageWritesThePlatformsTransparentPixel pins what a decoded
// image's transparent pixels hold: the colour the encoding kept there, which
// KTF and LGT titles key on, or the pixel a platform writes in its place —
// 0xFFF7 on SKT, which one title skips in its own blitters whatever colour a
// picture declared transparent.
func TestCreateImageWritesThePlatformsTransparentPixel(t *testing.T) {
	encoded := keyedPNG(t)
	_, kept := wipicrttest.NewRuntime(t)
	if got, want := createdImagePixels(t, kept, encoded), []uint16{0x0101, 0xf800}; !slices.Equal(got, want) {
		t.Errorf("pixels = %#x, want the kept colour %#x", got, want)
	}
	_, marked := wipicrttest.NewRuntimeWith(t, armcore.CoreOptions{}, func(client *wipicrt.Client) {
		client.Platform.TransparentImagePixel = 0xfff7
	})
	if got, want := createdImagePixels(t, marked, encoded), []uint16{0xfff7, 0xf800}; !slices.Equal(got, want) {
		t.Errorf("pixels = %#x, want the platform's %#x", got, want)
	}
}

// TestDrawImageLeavesOutTheContextsTransparentPixel pins MC_grpDrawImage's
// transparent pixel on a platform that honours it: an SKT title sets it to
// magenta before every image and has no other transparency. A context
// MC_grpInitContext cleared has none, and a platform that does not honour it
// draws every pixel.
func TestDrawImageLeavesOutTheContextsTransparentPixel(t *testing.T) {
	const key, red, blue = 0xf81f, 0xf800, 0x001f
	draw := func(honour bool, transparent uint32) []uint16 {
		_, runtime := wipicrttest.NewRuntimeWith(t, armcore.CoreOptions{}, func(client *wipicrt.Client) {
			client.Platform.ContextTransparentPixel = honour
		})
		source, _ := surface(t, runtime, key, red)
		destination, framebuffer := surface(t, runtime, blue, blue)
		context, err := runtime.AllocateBytes(make([]byte, wipicrt.WIPICGraphicsContextSize))
		if err != nil {
			t.Fatal(err)
		}
		contextCall(t, runtime, true, context, 3, transparent)
		tableCall(t, runtime, 13, destination, 0, 0, 2, 1, source, 0, 0, context)
		return wipicrttest.ReadSurfacePixels(t, runtime, framebuffer)
	}
	if got, want := draw(true, key), []uint16{blue, red}; !slices.Equal(got, want) {
		t.Errorf("keyed draw = %#x, want %#x", got, want)
	}
	if got, want := draw(true, 0), []uint16{key, red}; !slices.Equal(got, want) {
		t.Errorf("draw with no transparent pixel = %#x, want %#x", got, want)
	}
	if got, want := draw(false, key), []uint16{key, red}; !slices.Equal(got, want) {
		t.Errorf("draw on a platform without it = %#x, want %#x", got, want)
	}
}
