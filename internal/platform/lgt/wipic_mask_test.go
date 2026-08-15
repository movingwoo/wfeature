package lgt

import "testing"

// A surface copy leaves the mask colour behind. This is the only transparency
// an engine that builds its own glyphs has: it clears a strip to the mask,
// draws into it through the framebuffer pointer, and copies the glyph's cell
// over a background it must not erase. An opaque copy puts a magenta box
// behind every character.
func TestCopyFramebufferLeavesTheMaskColourBehind(t *testing.T) {
	client := fixtureClient(t)

	source, err := client.newFramebuffer(4, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	const glyph = uint16(0xffff)
	for index := range source.pixels {
		source.pixels[index] = maskPixel
	}
	source.pixels[1] = glyph
	if err := client.syncToGuest(source); err != nil {
		t.Fatal(err)
	}

	const background = uint16(0x001f)
	target := client.screen
	for index := range target.pixels {
		target.pixels[index] = background
	}
	context := &graphicsContext{
		target: target, clipWidth: target.width, clipHeight: target.height,
	}
	if err := client.copyFramebuffer(context, []int32{0, 0, 4, 2, int32(source.handle), 0, 0}); err != nil {
		t.Fatal(err)
	}

	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			want := background
			if x == 1 && y == 0 {
				want = glyph
			}
			if got := target.pixels[y*target.width+x]; got != want {
				t.Fatalf("pixel (%d, %d) = %#x, want %#x", x, y, got, want)
			}
		}
	}
}

// The same colour is transparent to an image draw, for the images a title
// builds itself rather than decodes: those carry no encoded transparency, so
// the mask is the only thing that says which pixels not to draw.
func TestDrawImageLeavesTheMaskColourBehind(t *testing.T) {
	client := fixtureClient(t)

	source, err := client.newFramebuffer(2, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	const ink = uint16(0xf800)
	source.pixels[0] = ink
	source.pixels[1] = maskPixel
	if err := client.syncToGuest(source); err != nil {
		t.Fatal(err)
	}

	const background = uint16(0x001f)
	target := client.screen
	for index := range target.pixels {
		target.pixels[index] = background
	}
	context := &graphicsContext{
		target: target, clipWidth: target.width, clipHeight: target.height,
	}
	if err := client.drawImage(context, []int32{0, 0, 2, 1, int32(source.handle), 0, 0}); err != nil {
		t.Fatal(err)
	}
	if got := target.pixels[0]; got != ink {
		t.Fatalf("drawn pixel = %#x, want %#x", got, ink)
	}
	if got := target.pixels[1]; got != background {
		t.Fatalf("masked pixel = %#x, want the background %#x", got, background)
	}
}

// The application id is what an anti-piracy check reads. A title asks for its
// own program's id string and compares it with the value compiled into its
// module, which is the AID its archive declares; answering null or anything
// else sends the title to a notice screen instead of its first frame.
func TestProgramApplicationIDAnswersTheArchivesAID(t *testing.T) {
	client := fixtureClient(t)

	address := callSlot(t, client, slotProgramApplicationID, client.programID())
	if address == 0 {
		t.Fatal("the application id slot answered null for this platform's own program")
	}
	text, err := client.readCString(address)
	if err != nil {
		t.Fatal(err)
	}
	if want := client.archive.Descriptor.AID; text != want {
		t.Fatalf("application id = %q, want the archive's %q", text, want)
	}
	if again := callSlot(t, client, slotProgramApplicationID, client.programID()); again != address {
		t.Fatalf("a second call answered %#x, want the same string %#x", again, address)
	}

	// A program this platform is not hosting gets a null, which is the answer
	// the caller already tests for before it formats the string.
	if other := callSlot(t, client, slotProgramApplicationID, client.programID()+1); other != 0 {
		t.Fatalf("an unrelated program id answered %#x, want null", other)
	}
}
