package lgt

import (
	"testing"
)

// A draw writes back the rows it drew and no others.
//
// **What it protects is the guest's own pixels.** A Clet writes into a surface
// through the framebuffer pointer it was handed, and the runtime's copy of the
// rows it wrote is stale until something reads them back. A draw that syncs the
// whole surface either way is correct but pays two screens of copying to set
// one pixel; a draw that syncs a band and writes back the whole surface would
// hand the guest back the stale rows, which is this test.
func TestADrawWritesBackTheRowsItDrewAndNoOthers(t *testing.T) {
	const (
		background = uint16(0x1111)
		guestOnly  = uint16(0x2222)
		drawn      = uint16(0xf81f)
	)
	client := fixtureClient(t)
	pointer, target := pixelOpFixture(t, client, 0, drawn)

	for index := range target.pixels {
		target.pixels[index] = background
	}
	if err := client.syncToGuest(target); err != nil {
		t.Fatal(err)
	}
	// The guest writes its own row, the way a Clet does: straight into the
	// surface's memory, with no MC_grp call to tell the runtime about it.
	stride := target.width
	const guestRow = 3
	guestPixels := make([]uint16, stride)
	for index := range guestPixels {
		guestPixels[index] = guestOnly
	}
	memory := client.core.Memory()
	if err := memory.WriteHalfwords(
		target.address+uint32(guestRow*stride*2), guestPixels); err != nil {
		t.Fatal(err)
	}

	// A fill of the top row only.
	callDrawSlot(t, client, slotFillRect, target.handle, 0, 0, uint32(stride), 1, pointer)

	readBack := make([]uint16, stride*target.height)
	if err := memory.ReadHalfwords(target.address, readBack); err != nil {
		t.Fatal(err)
	}
	if got := readBack[0]; got != drawn {
		t.Fatalf("the drawn row holds %#x, want %#x", got, drawn)
	}
	if got := readBack[guestRow*stride]; got != guestOnly {
		t.Fatalf("the row the guest wrote holds %#x, want the guest's own %#x", got, guestOnly)
	}
}

// The band a slot names is its own rectangle inside the clip, and a slot the
// switch does not name gets the clip — which is always correct, because every
// one of these operations draws through a put that refuses a pixel outside it.
func TestTheBandADrawSyncsIsItsRectangleInsideTheClip(t *testing.T) {
	context := &graphicsContext{clipY: 2, clipHeight: 6}
	// MC_grpPutPixel(dst, x, y, pgc): one row.
	if band := drawBand(slotPutPixel, context, []int32{0, 1, 4}); band != (rowBand{top: 4, bottom: 5}) {
		t.Fatalf("put pixel band = %+v", band)
	}
	// A pixel above the clip is a band of nothing.
	if band := drawBand(slotPutPixel, context, []int32{0, 1, 0}); !band.empty() {
		t.Fatalf("a pixel outside the clip named %+v", band)
	}
	// MC_grpFillRect(dst, x, y, w, h, pgc), clipped at both ends.
	if band := drawBand(slotFillRect, context, []int32{0, 0, 0, 4, 100}); band != (rowBand{top: 2, bottom: 8}) {
		t.Fatalf("fill band = %+v", band)
	}
	// A line's two endpoints, in either order, both included.
	if band := drawBand(slotDrawLine, context, []int32{0, 0, 6, 9, 3}); band != (rowBand{top: 3, bottom: 7}) {
		t.Fatalf("line band = %+v", band)
	}
	// MC_grpCopyArea(dst, x, y, w, h, sx, sy, pgc) reads rows the clip does
	// not cover, so the band covers them too.
	if band := drawBand(slotCopyArea, context, []int32{0, 0, 4, 4, 2, 0, 0}); band != (rowBand{top: 0, bottom: 6}) {
		t.Fatalf("copy area band = %+v", band)
	}
	// A slot with no rectangle of its own falls back to the clip.
	if band := drawBand(slotDrawString, context, []int32{0, 0, 0, 0, 0}); band != (rowBand{top: 2, bottom: 8}) {
		t.Fatalf("string band = %+v", band)
	}
}
