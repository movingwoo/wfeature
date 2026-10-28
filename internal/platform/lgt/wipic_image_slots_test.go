package lgt

import (
	"context"
	"image"
	"image/color"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The image calls follow the v2.x specification's order from MC_grpCreateImage
// to MC_grpEncodeImage. Both ends and the destroy between them are confirmed
// by calls; the numbers are the finding, so they are pinned here.
func TestTheImageSlotsFollowTheSpecificationOrder(t *testing.T) {
	for _, slot := range []struct {
		name      string
		got, want uint32
	}{
		{"MC_grpCreateImage", slotCreateImage, 0xe9},
		{"MC_grpCreateSubImage", slotCreateSubImage, 0xea},
		{"MC_grpDestroyImage", slotDestroyImage, 0xeb},
		{"MC_grpDecodeNextImage", slotDecodeNextImage, 0xec},
		{"MC_grpEncodeImage", slotEncodeImage, 0xed},
	} {
		if slot.got != slot.want {
			t.Errorf("%s is slot %#x, want %#x", slot.name, slot.got, slot.want)
		}
		if !knownWIPICSlot(slot.got) || wipicSlotNames[slot.got] == "" {
			t.Errorf("%s at %#x is not served under a name", slot.name, slot.got)
		}
	}
}

// Twenty-seven local titles call 0xeb, mostly once for each image they
// create, from release code. While 0xeb answered "no further frame" nothing
// was freed, and a Clet has no collector to free it instead: every picture a
// title loaded, drew and let go stayed in the 16 MiB surface region. Six
// hundred 128×128 images are more than the region holds at once, so this runs
// out unless each destroy gives its surface and its record back.
func TestDestroyImageAtSlot0xebGivesTheSurfaceBack(t *testing.T) {
	client := fixtureClient(t)
	encoded := encodedTestImage(t, 128, 128)
	data := writeGuest(t, client, encoded)
	out := writeGuest(t, client, make([]byte, 4))
	surfaces, records := client.surfaces.used(), client.arena.used()
	for round := 0; round < 600; round++ {
		if status := int32(callSlot(t, client, 0xe9, out, data, 0, uint32(len(encoded)))); status != imageDone {
			t.Fatalf("round %d: createImage = %d", round, status)
		}
		handle, err := client.readWord(out)
		if err != nil {
			t.Fatal(err)
		}
		callSlot(t, client, 0xeb, handle)
		if client.framebuffer(handle) != nil {
			t.Fatalf("round %d: the image survived 0xeb", round)
		}
	}
	if used := client.surfaces.used(); used != surfaces {
		t.Fatalf("the surface region holds %d bytes after every image was destroyed, want %d", used, surfaces)
	}
	if used := client.arena.used(); used != records {
		t.Fatalf("the platform arena holds %d bytes after every image was destroyed, want %d", used, records)
	}
}

// callCreateSubImage runs MC_grpCreateSubImage(src, x, y, w, h) with the
// height on the stack, where the procedure call standard puts a fifth argument.
// The stack is the caller's own block, so a test that counts what the slot
// allocates is not counting the stack as well.
func callCreateSubImage(t *testing.T, client *Client, stack, source uint32, x, y, width, height int32) uint32 {
	t.Helper()
	if err := client.writeWord(stack, uint32(height)); err != nil {
		t.Fatal(err)
	}
	state := armcore.NewContext()
	state.Registers[0] = source
	state.Registers[1] = uint32(x)
	state.Registers[2] = uint32(y)
	state.Registers[3] = uint32(width)
	state.Registers[armcore.RegisterSP] = stack
	thread := armcore.NewThread(state)
	if err := client.handleWIPICSVC(context.Background(), thread, slotCreateSubImage); err != nil {
		t.Fatalf("createSubImage(%d, %d, %d, %d): %v", x, y, width, height, err)
	}
	result, err := thread.Register(0)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// guestPixels reads a surface's pixels the way a Clet does: through the
// pointer its framebuffer hands out, rather than out of the runtime's copy.
func guestPixels(t *testing.T, client *Client, handle uint32) []uint16 {
	t.Helper()
	buffer := client.framebuffer(handle)
	if buffer == nil {
		t.Fatalf("%#x is not a surface", handle)
	}
	pointer := callSlot(t, client, slotFramebufferPointer, handle)
	pixels := make([]uint16, buffer.width*buffer.height)
	for index := range pixels {
		value, err := client.readHalfword(pointer + uint32(index)*2)
		if err != nil {
			t.Fatal(err)
		}
		pixels[index] = value
	}
	return pixels
}

// A sub image is the source's own pixels from (x, y), with the part of the
// mask under them. This source's transparent pixels are two different colours,
// so its mask is positional rather than a colour key, and the sub image's mask
// has to be the same positions.
func TestCreateSubImageCopiesThePixelsAndTheMaskUnderIt(t *testing.T) {
	client := fixtureClient(t)
	decoded := image.NewNRGBA(image.Rect(0, 0, 6, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 6; x++ {
			decoded.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 40), G: uint8(y * 60), B: 0x80, A: 0xff})
		}
	}
	decoded.SetNRGBA(3, 1, color.NRGBA{R: 0xff})
	decoded.SetNRGBA(4, 2, color.NRGBA{B: 0xff})
	source, err := client.framebufferFromImage(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if source.colorKeyed || source.opaque == nil {
		t.Fatal("the fixture was meant to carry a positional mask and no colour key")
	}

	stack := writeGuest(t, client, make([]byte, 16))
	handle := callCreateSubImage(t, client, stack, source.handle, 2, 1, 3, 2)
	if handle == 0 {
		t.Fatal("createSubImage of a rectangle inside the source answered NULL")
	}
	sub := client.framebuffer(handle)
	if sub == nil || sub.width != 3 || sub.height != 2 {
		t.Fatalf("the sub image is %+v, want a 3x2 surface", sub)
	}
	if got := callSlot(t, client, slotGetImageProperty, handle, imagePropertyWidth); got != 3 {
		t.Fatalf("the sub image's width property is %d, want 3", got)
	}
	if got := callSlot(t, client, slotGetImageProperty, handle, imagePropertyHeight); got != 2 {
		t.Fatalf("the sub image's height property is %d, want 2", got)
	}
	inGuest := guestPixels(t, client, handle)
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			want := source.pixels[(1+y)*6+2+x]
			if got := sub.pixels[y*3+x]; got != want {
				t.Fatalf("sub pixel (%d, %d) = %#x, want the source's %#x", x, y, got, want)
			}
			if got := inGuest[y*3+x]; got != want {
				t.Fatalf("sub pixel (%d, %d) reads %#x through the framebuffer pointer, want %#x", x, y, got, want)
			}
			wantOpaque := source.opaque[(1+y)*6+2+x]
			if sub.opaque == nil || sub.opaque[y*3+x] != wantOpaque {
				t.Fatalf("sub mask at (%d, %d) is not the source's %v", x, y, wantOpaque)
			}
		}
	}
	// (3, 1) and (4, 2) of the source are (1, 0) and (2, 1) of the sub image.
	if sub.opaque[1] || sub.opaque[5] || !sub.opaque[0] || sub.colorKeyed {
		t.Fatalf("the sub mask is %v (keyed %v)", sub.opaque, sub.colorKeyed)
	}

	// The sub image is an image of its own: the source can go and it stays.
	callSlot(t, client, slotDestroyImage, source.handle)
	if client.framebuffer(handle) == nil {
		t.Fatal("destroying the source took the sub image with it")
	}
	for index, pixel := range guestPixels(t, client, handle) {
		if pixel != inGuest[index] {
			t.Fatalf("after the source was destroyed sub pixel %d reads %#x, want %#x", index, pixel, inGuest[index])
		}
	}
	callSlot(t, client, slotDestroyImage, handle)
	if client.framebuffer(handle) != nil {
		t.Fatal("the sub image survived its own destroy")
	}
}

// A source whose transparency is a colour key keeps the key in its sub image,
// and the sub image's mask is read off the pixels it holds: a guest that
// reflects a sprite by moving words through the framebuffer pointer moves its
// transparency with them, and the sub image cut afterwards has to agree with
// what drawing the source would show.
func TestCreateSubImageOfAKeyedSourceFollowsTheKey(t *testing.T) {
	client := fixtureClient(t)
	handle, status := createTestImage(t, client, magentaKeyedImage(t, 8, 4))
	if status != imageDone {
		t.Fatalf("createImage = %d", status)
	}
	source := client.framebuffer(handle)
	if !source.colorKeyed {
		t.Fatal("the magenta fixture was meant to be colour keyed")
	}
	const magenta, green = uint16(0xf81f), uint16(0x0400)
	// The guest swaps two words of row 0: an opaque pixel takes the key and a
	// transparent one a colour.
	if err := client.core.Memory().WriteHalfwords(source.address+2, []uint16{magenta}); err != nil {
		t.Fatal(err)
	}
	if err := client.core.Memory().WriteHalfwords(source.address+12, []uint16{green}); err != nil {
		t.Fatal(err)
	}
	stack := writeGuest(t, client, make([]byte, 16))
	sub := client.framebuffer(callCreateSubImage(t, client, stack, handle, 0, 0, 8, 1))
	if sub == nil {
		t.Fatal("createSubImage of the first row answered NULL")
	}
	if !sub.colorKeyed || sub.transparentKey != magenta {
		t.Fatalf("the sub image is keyed %v with %#x, want the source's key %#x", sub.colorKeyed, sub.transparentKey, magenta)
	}
	for x, want := range []bool{true, false, true, true, false, false, true, false} {
		if sub.opaque[x] != want {
			t.Fatalf("sub mask at %d is %v, want %v (pixels %#x)", x, sub.opaque[x], want, sub.pixels)
		}
	}
}

// A surface with no declared transparency gives a sub image with none, and
// draws the same way: the mask colour is the only transparency either has.
func TestCreateSubImageOfAPlainSurfaceHasNoMask(t *testing.T) {
	client := fixtureClient(t)
	offscreen := callSlot(t, client, slotCreateOffscreen, 4, 4)
	surface := client.framebuffer(offscreen)
	for index := range surface.pixels {
		surface.pixels[index] = uint16(index)
	}
	if err := client.syncToGuest(surface); err != nil {
		t.Fatal(err)
	}
	stack := writeGuest(t, client, make([]byte, 16))
	sub := client.framebuffer(callCreateSubImage(t, client, stack, offscreen, 1, 1, 2, 3))
	if sub == nil {
		t.Fatal("createSubImage of an offscreen surface answered NULL")
	}
	if sub.opaque != nil || sub.colorKeyed {
		t.Fatalf("a sub image of a plain surface carries a mask (%v, keyed %v)", sub.opaque, sub.colorKeyed)
	}
	if sub.pixels[0] != 5 || sub.pixels[5] != 14 {
		t.Fatalf("the sub image holds %v", sub.pixels)
	}
}

// NULL is the specification's one answer for an image that cannot be made.
// A rectangle that leaves the source, or has no pixels, is one: clipping it
// would answer an image of a size the caller did not ask for, and a caller
// that walks it with the width it asked for would walk past its end. Nothing
// is allocated for a refusal.
func TestCreateSubImageRefusesARegionTheSourceDoesNotHold(t *testing.T) {
	client := fixtureClient(t)
	handle, _ := createTestImage(t, client, encodedTestImage(t, 8, 4))
	stack := writeGuest(t, client, make([]byte, 16))
	surfaces, records := client.surfaces.used(), client.arena.used()
	for _, region := range [][4]int32{
		{0, 0, 0, 4},  // no width
		{0, 0, 8, 0},  // no height
		{0, 0, -1, 4}, // a negative width
		{-1, 0, 2, 2}, // left of the source
		{0, -1, 2, 2}, // above it
		{7, 0, 2, 1},  // past its right edge
		{0, 3, 1, 2},  // past its bottom edge
		{0, 0, 9, 4},  // wider than the source
		{0x7fffffff, 0, 2, 1},
		{2, 0, 0x7fffffff, 1},
	} {
		if got := callCreateSubImage(t, client, stack, handle, region[0], region[1], region[2], region[3]); got != 0 {
			t.Fatalf("createSubImage%v answered %#x, want NULL", region, got)
		}
	}
	if got := callCreateSubImage(t, client, stack, handle+0x1000, 0, 0, 1, 1); got != 0 {
		t.Fatalf("createSubImage of an unknown image answered %#x, want NULL", got)
	}
	if client.surfaces.used() != surfaces || client.arena.used() != records {
		t.Fatal("a refused sub image took memory")
	}
	// The whole source is a region it holds.
	if got := callCreateSubImage(t, client, stack, handle, 0, 0, 8, 4); got == 0 {
		t.Fatal("createSubImage of the whole source answered NULL")
	}
}
