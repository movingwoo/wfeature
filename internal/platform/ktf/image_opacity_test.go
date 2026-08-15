package ktf

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// encodePalettedPNG builds the shape the games' sprite assets have: a palette
// image whose first entry is fully transparent, which PNG carries as a tRNS
// chunk rather than as pixel data. Column 0 uses that entry and column 1 a
// solid colour.
func encodePalettedPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	palette := color.Palette{
		color.NRGBA{R: 0, G: 0, B: 0, A: 0},
		color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	}
	source := image.NewPaletted(image.Rect(0, 0, width, height), palette)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x == 0 {
				source.SetColorIndex(x, y, 0)
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

// A sprite whose encoding marks pixels transparent must leave the target alone
// there. Dropping that transparency paints the sprite's unused border as solid
// colour, which is the black box that used to surround every drawn character.
func TestDrawImageKeepsTargetWhereTheEncodingIsTransparent(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)

	// A red background makes both outcomes visible: the transparent column has
	// to keep it and the opaque column has to replace it.
	if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0xff0000)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsFillRect(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(4), jvm.IntValue(4)}); err != nil {
		t.Fatal(err)
	}
	background := readScreenPixel(t, runtime, state, 0, 0)
	if background == 0 {
		t.Fatal("background fill did not reach the screen")
	}

	imageValue, err := runtimeImageFromEncoded(runtime, encodePalettedPNG(t, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsDrawImage(runtime, client.JVM(), []jvm.Value{
		receiver, imageValue, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(0),
	}); err != nil {
		t.Fatalf("Graphics.drawImage() error = %v", err)
	}

	if pixel := readScreenPixel(t, runtime, state, 0, 0); pixel != background {
		t.Errorf("transparent source pixel drew %#x over the background %#x", pixel, background)
	}
	if pixel := readScreenPixel(t, runtime, state, 1, 0); pixel != 0xffff {
		t.Errorf("opaque source pixel drew %#x, want white", pixel)
	}
}

// A copy of such an image inherits the transparency, so a game that composes
// sprites into a scratch surface before drawing it gets the same result.
func TestCreateImageCopyInheritsEncodedTransparency(t *testing.T) {
	client, runtime := newTestRuntime(t)
	source, err := runtimeImageFromEncoded(runtime, encodePalettedPNG(t, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	copied, err := runtimeImageCreateCopy(runtime, client.JVM(), []jvm.Value{source})
	if err != nil {
		t.Fatalf("Image.createImage(Image) error = %v", err)
	}
	copiedObject, err := copied.Reference()
	if err != nil {
		t.Fatal(err)
	}
	handle, err := runtime.imageFramebufferHandle(copiedObject)
	if err != nil {
		t.Fatal(err)
	}
	opacity := runtime.framebufferOpacityOf(handle)
	if opacity == nil {
		t.Fatal("the copy records no transparency")
	}
	if opacity.opaqueAt(0, 0) {
		t.Error("the copy reports its transparent column as drawn")
	}
	if !opacity.opaqueAt(1, 0) {
		t.Error("the copy reports its opaque column as absent")
	}
}

// MC_grpCreateImage answers an MC_GrpImage handle, and that is the handle
// every C-side draw is given — MC_grpGetImageFrameBuffer hands the same one
// back, because the record inlines its framebuffer fields. So the transparency
// the encoding declared has to answer to the image handle and not only to the
// framebuffer record built inside it. Recording it on the inner handle alone
// left MC_grpDrawImage with no transparency at all, which drew one title's map
// objects each inside a black rectangle.
func TestCreateImageRecordsTransparencyUnderTheImageHandle(t *testing.T) {
	_, runtime := newTestRuntime(t)
	encoded := encodePalettedPNG(t, 2, 2)

	data, err := runtime.allocateWIPIC(uint32(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.client.core.Memory().Write(data+wipicAllocationOverhead, encoded); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.allocateWIPIC(4)
	if err != nil {
		t.Fatal(err)
	}

	thread := armcore.NewThread(armcore.Context{})
	for register, value := range []uint32{result + wipicAllocationOverhead, data, 0, uint32(len(encoded))} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	if code, err := runtime.wipicCreateImage(thread); err != nil || code != 1 {
		t.Fatalf("MC_grpCreateImage() = %#x, %v", code, err)
	}

	handles, err := runtime.readAOTWords(result+wipicAllocationOverhead, 1, "created image handle")
	if err != nil {
		t.Fatal(err)
	}
	image := handles[0]
	if _, err := runtime.readWIPICFramebuffer(image); err != nil {
		t.Fatalf("the image handle does not read as a framebuffer: %v", err)
	}
	opacity := runtime.framebufferOpacityOf(image)
	if opacity == nil {
		t.Fatal("the image handle records no transparency")
	}
	if opacity.opaqueAt(0, 0) {
		t.Error("the transparent column reports as drawn")
	}
	if !opacity.opaqueAt(1, 0) {
		t.Error("the opaque column reports as absent")
	}
}

// A fully opaque image carries no mask at all, so the common case keeps the
// plain copy path.
func TestFullyOpaqueImageCarriesNoOpacityMask(t *testing.T) {
	_, runtime := newTestRuntime(t)
	opaque := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for index := range opaque.Pix {
		opaque.Pix[index] = 0xff
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, opaque); err != nil {
		t.Fatal(err)
	}
	value, err := runtimeImageFromEncoded(runtime, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	object, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := object.Native.(image.Image)
	if decoded == nil {
		t.Fatal("decoded image is missing")
	}
	if opacity := imageOpacityOf(decoded); opacity != nil {
		t.Errorf("an opaque image carries an opacity mask: %+v", opacity)
	}
}
