package lgt

import (
	"context"
	"testing"
)

func TestDrawOffsetSynchronizesTranslatedRows(t *testing.T) {
	client := fixtureClient(t)
	pointer, target := pixelOpFixture(t, client, 0, 0x1234)
	if err := client.writeWord(pointer+grpContextOffset, 1<<16); err != nil {
		t.Fatal(err)
	}
	callDrawSlot(t, client, slotPutPixel, target.handle, 0, 0, pointer)
	pixel, err := client.readHalfword(target.address + uint32(target.bytesPerLine()))
	if err != nil || pixel != 0x1234 {
		t.Fatalf("translated pixel = %#x, %v", pixel, err)
	}
	if pixel, _ := client.readHalfword(target.address); pixel == 0x1234 {
		t.Fatal("untranslated row overwritten")
	}
}

func TestOverlappingFramebufferBlitUsesOriginalPixels(t *testing.T) {
	client := fixtureClient(t)
	pointer, target := pixelOpFixture(t, client, 0, 0)
	for i := range target.pixels {
		target.pixels[i] = uint16(i + 1)
	}
	if err := client.syncToGuest(target); err != nil {
		t.Fatal(err)
	}
	context, err := client.contextFor(context.Background(), client.thread, target, pointer)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.copyFramebuffer(context, []int32{1, 0, 3, 1, int32(target.handle), 0, 0}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint16{1, 1, 2, 3} {
		if target.pixels[i] != want {
			t.Fatalf("pixel %d = %d, want %d", i, target.pixels[i], want)
		}
	}
}

func TestPixelCallbackMayEnterFramebufferImport(t *testing.T) {
	client := fixtureClient(t)
	function := installThumb(t, client, 0xb510, 0x1c0c, 0x1c10, 0x4b02, 0x469c, 0xdf02, 0x1c20, 0xbd10,
		uint16(slotFramebufferWidth), uint16(slotFramebufferWidth>>16))
	pointer, target := pixelOpFixture(t, client, function, 0x1234)
	if err := client.writeWord(pointer+grpContextParam1, target.handle); err != nil {
		t.Fatal(err)
	}
	callDrawSlot(t, client, slotPutPixel, target.handle, 0, 0, pointer)
	if target.pixels[0] != 0x1234 {
		t.Fatalf("callback result = %#x", target.pixels[0])
	}
}

func TestPixelCallbackFailureReacquiresClientLock(t *testing.T) {
	client := fixtureClient(t)
	function := installThumb(t, client, 0xde00)
	client.mu.Lock()
	_, err := client.applyPixelOp(t.Context(), client.thread, pixelOp{function: function}, 0, 1, true)
	if client.mu.TryLock() {
		client.mu.Unlock()
		t.Fatal("failed callback did not reacquire the client lock")
	}
	client.mu.Unlock()
	if err == nil {
		t.Fatal("undefined callback instruction succeeded")
	}
}
