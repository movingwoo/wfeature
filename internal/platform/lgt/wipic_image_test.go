package lgt

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// encodedTestImage builds a small PNG whose left half is opaque red and whose
// right half is fully transparent, so both the colour conversion and the
// transparency the encoding declared can be checked from one image.
func encodedTestImage(t *testing.T, width, height int) []byte {
	t.Helper()
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < width/2 {
				source.Set(x, y, color.NRGBA{R: 0xff, A: 0xff})
			} else {
				source.Set(x, y, color.NRGBA{})
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// createTestImage runs MC_grpCreateImage the way a game does and answers the
// handle it wrote through the out-parameter.
func createTestImage(t *testing.T, client *Client, encoded []byte) (uint32, int32) {
	t.Helper()
	data := writeGuest(t, client, encoded)
	out := writeGuest(t, client, make([]byte, 4))
	status := int32(callSlot(t, client, slotCreateImage, out, data, 0, uint32(len(encoded))))
	handle, err := client.readWord(out)
	if err != nil {
		t.Fatal(err)
	}
	return handle, status
}

// TestCreateImageWritesTheHandleThroughItsOutParameter is the argument order,
// and getting it backwards is silent: the caller reads whatever its own stack
// slot held and carries on with a number that was never a handle.
func TestCreateImageWritesTheHandleThroughItsOutParameter(t *testing.T) {
	client := fixtureClient(t)
	handle, status := createTestImage(t, client, encodedTestImage(t, 8, 4))
	if status != imageDone {
		t.Fatalf("createImage = %d, want the done status", status)
	}
	if handle == 0 {
		t.Fatal("createImage wrote a null handle")
	}
	if got := callSlot(t, client, slotGetImageProperty, handle, imagePropertyWidth); got != 8 {
		t.Fatalf("image width = %d, want 8", got)
	}
	if got := callSlot(t, client, slotGetImageProperty, handle, imagePropertyHeight); got != 4 {
		t.Fatalf("image height = %d, want 4", got)
	}
	// An image is a framebuffer, and a game asks for its pointer to read the
	// pixels directly.
	if got := callSlot(t, client, slotGetImageFramebuffer, handle); got != handle {
		t.Fatalf("getImageFramebuffer = %#x, want the image handle %#x", got, handle)
	}
	if pointer := callSlot(t, client, slotFramebufferPointer, handle); pointer == 0 || int32(pointer) == wipiError {
		t.Fatalf("the image framebuffer has no pointer (%#x)", pointer)
	}
}

// TestCreateImageRefusesWhatItCannotDecode pins the failure: a null image and
// the bad-format status, rather than a blank the game would draw as an
// invisible sprite.
func TestCreateImageRefusesWhatItCannotDecode(t *testing.T) {
	client := fixtureClient(t)
	handle, status := createTestImage(t, client, []byte("this is not an image"))
	if status != imageBadFormat {
		t.Fatalf("createImage of junk = %d, want the bad-format status", status)
	}
	if handle != 0 {
		t.Fatalf("a failed create left handle %#x behind", handle)
	}
}

// TestDrawImageSkipsWhatTheEncodingDeclaredTransparent is the difference
// between MC_grpDrawImage and MC_grpCopyFrameBuffer. RGB565 has no alpha, so
// without the mask a sprite arrives as a rectangle of whatever its transparent
// pixels happened to decode to.
func TestDrawImageSkipsWhatTheEncodingDeclaredTransparent(t *testing.T) {
	client := fixtureClient(t)
	handle, status := createTestImage(t, client, encodedTestImage(t, 8, 4))
	if status != imageDone {
		t.Fatalf("createImage = %d", status)
	}
	source := client.framebuffers[handle]
	if source == nil {
		t.Fatal("the image is not a framebuffer")
	}

	// The destination starts as a colour neither half of the image carries, so
	// a pixel that was drawn and one that was skipped are told apart.
	const background = 0x001f
	target := client.screen
	for index := range target.pixels {
		target.pixels[index] = background
	}
	context := &graphicsContext{
		target: target, clipWidth: target.width, clipHeight: target.height,
	}
	if err := client.drawImage(context, []int32{0, 0, 8, 4, int32(handle), 0, 0}); err != nil {
		t.Fatal(err)
	}
	const red = 0xf800
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			got := target.pixels[y*target.width+x]
			want := uint16(red)
			if x >= 4 {
				want = background
			}
			if got != want {
				t.Fatalf("pixel (%d, %d) = %#x, want %#x", x, y, got, want)
			}
		}
	}
}

// TestDecodeNextImageReportsNoFurtherFrames covers the call a title makes
// after it has finished with an image. Answering "one more frame" would have a
// loop ask forever.
func TestDecodeNextImageReportsNoFurtherFrames(t *testing.T) {
	client := fixtureClient(t)
	handle, _ := createTestImage(t, client, encodedTestImage(t, 4, 4))
	if got := int32(callSlot(t, client, slotDecodeNextImage, handle)); got != imageDone {
		t.Fatalf("decodeNextImage = %d, want the done status", got)
	}
	if got := int32(callSlot(t, client, slotDecodeNextImage, handle+0x1000)); got != imageBadFormat {
		t.Fatalf("decodeNextImage on an unknown image = %d, want the bad-format status", got)
	}
}

// TestDestroyImageReleasesIt checks that an image can be freed, and that the
// LCD cannot be destroyed through the same call.
func TestDestroyImageReleasesIt(t *testing.T) {
	client := fixtureClient(t)
	handle, _ := createTestImage(t, client, encodedTestImage(t, 4, 4))
	callSlot(t, client, slotDestroyImage, handle)
	if client.framebuffers[handle] != nil {
		t.Fatal("the image survived destroyImage")
	}
	// The LCD is registered the first time a Clet asks for it, and it must
	// survive a destroy aimed at it: an image and a surface share one handle
	// space here, so nothing but the screen flag keeps them apart.
	screen := callSlot(t, client, slotGetScreenFramebuffer, 0)
	callSlot(t, client, slotDestroyImage, screen)
	if client.framebuffers[screen] == nil {
		t.Fatal("destroyImage removed the LCD")
	}
}

// TestDisplayInfoDescribesTheLCD pins the argument order and the structure.
// The pointer is the second argument, and reading the first wrote nothing at
// all — the display index is zero — so a title read its own uninitialised
// structure and drew every screen at four bytes a pixel.
func TestDisplayInfoDescribesTheLCD(t *testing.T) {
	client := fixtureClient(t)
	out := writeGuest(t, client, make([]byte, 9*4))
	if got := callSlot(t, client, slotGetDisplayInfo, 0, out); got != 1 {
		t.Fatalf("getDisplayInfo = %d, want 1 for a display that exists", got)
	}
	fields := make([]uint32, 9)
	for index := range fields {
		value, err := client.readWord(out + uint32(index)*4)
		if err != nil {
			t.Fatal(err)
		}
		fields[index] = value
	}
	want := []uint32{
		16, 16,
		uint32(client.screen.width), uint32(client.screen.height),
		uint32(client.screen.bytesPerLine()),
		directColorType,
		0xf800, 0x001f, 0x07e0,
	}
	for index, value := range want {
		if fields[index] != value {
			t.Fatalf("display info field %d = %#x, want %#x", index, fields[index], value)
		}
	}
	if got := callSlot(t, client, slotGetDisplayInfo, 0, 0); got != 0 {
		t.Fatalf("getDisplayInfo with no structure = %d, want 0", got)
	}
}

// An image handle is an address, because one title dereferences it: it loads
// the first word of what a create wrote and stores into that structure. A
// handle that is not mapped memory is a fault one screen later, in the title's
// own code, with nothing in the trace pointing at the create.
func TestCreateImageAnswersAnAddressableRecord(t *testing.T) {
	client := fixtureClient(t)
	handle, status := createTestImage(t, client, encodedTestImage(t, 8, 4))
	if status != imageDone {
		t.Fatalf("createImage = %d, want the done status", status)
	}
	framebuffer, err := client.readWord(handle)
	if err != nil {
		t.Fatalf("the image handle %#x is not readable memory: %v", handle, err)
	}
	if framebuffer != handle {
		t.Fatalf("word zero of the image record = %#x, want the framebuffer %#x", framebuffer, handle)
	}
	if got := callSlot(t, client, slotGetImageFramebuffer, handle); got != handle {
		t.Fatalf("MC_grpGetImageFrameBuffer = %#x, want word zero %#x", got, handle)
	}
	// The record has room past the fields this platform chose, because a title
	// stores its own transparent colour into one of them.
	if err := client.writeWord(handle+0x24, 0x00ff00ff); err != nil {
		t.Fatalf("a title's store into the image record failed: %v", err)
	}
	// The dimensions the specification says a framebuffer holds are where the
	// record says they are, after the data pointer and the bytes per line.
	width, err := client.readWord(handle + surfaceRecordWidth)
	if err != nil {
		t.Fatal(err)
	}
	height, err := client.readWord(handle + surfaceRecordHeight)
	if err != nil {
		t.Fatal(err)
	}
	if width != 8 || height != 4 {
		t.Fatalf("the record's dimensions are %dx%d, want 8x4", width, height)
	}
}

// magentaKeyedImage is the shape these titles actually ship: a paletted PNG
// whose transparent entry is a colour the art never draws with, declared
// through tRNS. Fifty-two of one title's images use pure magenta.
func magentaKeyedImage(t *testing.T, width, height int) []byte {
	t.Helper()
	palette := color.Palette{
		color.NRGBA{R: 0xff, G: 0x00, B: 0xff, A: 0x00}, // the transparent key
		color.NRGBA{R: 0x00, G: 0x80, B: 0x00, A: 0xff},
	}
	source := image.NewPaletted(image.Rect(0, 0, width, height), palette)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := uint8(0)
			if x < width/2 {
				index = 1
			}
			source.SetColorIndex(x, y, index)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// A transparent pixel keeps the colour its encoding gave it, because the guest
// reads these pixels itself. One local title never asks the platform to blit a
// sprite: it takes MC_grpGetImageFrameBuffer, then the raw pointer, and runs
// its own blitter over the words, skipping the magenta its art declares
// transparent. Go returns colours premultiplied, so a plain RGBA() hands that
// blitter black, the key never matches, and every sprite paints its whole
// rectangle — a character on a solid box.
func TestATransparentPixelKeepsItsColourForAGuestThatBlitsItself(t *testing.T) {
	client := fixtureClient(t)
	handle, status := createTestImage(t, client, magentaKeyedImage(t, 8, 4))
	if status != imageDone {
		t.Fatalf("createImage = %d, want the done status", status)
	}
	buffer := client.framebuffer(handle)
	if buffer == nil {
		t.Fatal("createImage produced no surface")
	}
	const magenta565 = uint16(0xf81f)
	if got := buffer.pixels[0]; got == magenta565 {
		t.Fatalf("the opaque half is the key colour %04x", got)
	}
	for x := 4; x < 8; x++ {
		if got := buffer.pixels[x]; got != magenta565 {
			t.Fatalf("transparent pixel %d = %04x, want the declared key %04x",
				x, got, magenta565)
		}
	}
	// The mask is still recorded beside the pixels: the platform's own blit
	// reads it, and a guest reading raw pixels reads the colour. Both answers
	// have to be there at once.
	if buffer.opaque == nil {
		t.Fatal("the declared transparency was not recorded beside the pixels")
	}
	if !buffer.opaque[0] || buffer.opaque[4] {
		t.Fatal("the mask does not match the half the encoding declared")
	}
}
