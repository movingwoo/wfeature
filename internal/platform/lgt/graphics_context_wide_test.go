package lgt

import (
	"context"
	"testing"
)

func TestWideContextKeepsSignedOffsetsAndDirectCallbackSeparate(t *testing.T) {
	client := fixtureClient(t)
	client.wideGraphicsContexts = true
	pointer := writeGuest(t, client, make([]byte, wideContextSize+4))
	if err := client.writeWord(pointer+wideContextSize, 0xdeadbeef); err != nil {
		t.Fatal(err)
	}
	if got := client.initContext(pointer); got != wipiSuccess {
		t.Fatal(got)
	}
	guard, _ := client.readWord(pointer + wideContextSize)
	if guard != 0xdeadbeef {
		t.Fatal("initialization overwrote the guard")
	}
	// A mutable global supplies the answer, independently of the pixel pair.
	global := writeGuest(t, client, []byte{0x34, 0x12, 0, 0})
	function := installThumb(t, client, 0x4b01, 0x6818, 0x4770, 0x46c0, uint16(global), uint16(global>>16))
	if err := client.writeWord(pointer+28, function); err != nil {
		t.Fatal(err)
	}
	offsets := writeGuest(t, client, make([]byte, 8))
	client.writeWord(offsets, 0xfffffffe)
	client.writeWord(offsets+4, 3)
	if got := client.transferContextFieldFor(t, pointer, grpFieldOffset, offsets); got != wipiSuccess {
		t.Fatal(got)
	}
	for _, want := range []uint32{0x1234, 0x5678} {
		client.writeWord(global, want)
		gc, err := client.contextFor(context.Background(), client.thread, client.screen, pointer)
		if err != nil {
			t.Fatal(err)
		}
		if gc.offsetX != -2 || gc.offsetY != 3 || gc.op.function != function {
			t.Fatalf("context = %+v", gc)
		}
		got, err := client.applyPixelOp(context.Background(), client.thread, gc.op, 0, 0xffff)
		if err != nil || got != uint16(want) {
			t.Fatalf("callback = %#x, %v; want %#x", got, err, want)
		}
	}
}

func TestWideContextClipRoundTripAndEmptyRegion(t *testing.T) {
	client := fixtureClient(t)
	client.wideGraphicsContexts = true
	pointer := writeGuest(t, client, make([]byte, wideContextSize))
	client.initContext(pointer)
	clip := writeGuest(t, client, make([]byte, 16))
	for i, v := range []uint32{0xfffffffe, 1, 7, 6} {
		client.writeWord(clip+uint32(i)*4, v)
	}
	if got := client.transferContextFieldFor(t, pointer, grpFieldClip, clip); got != wipiSuccess {
		t.Fatal(got)
	}
	gc, err := client.contextFor(context.Background(), client.thread, client.screen, pointer)
	if err != nil {
		t.Fatal(err)
	}
	if gc.clipX != -2 || gc.clipY != 1 || gc.clipWidth != 9 || gc.clipHeight != 5 {
		t.Fatalf("clip = %+v", gc)
	}
	output := writeGuest(t, client, make([]byte, 16))
	if got := callSlot(t, client, slotGetContext, pointer, grpFieldClip, output); int32(got) != wipiSuccess {
		t.Fatal(got)
	}
	for i, v := range []uint32{0xfffffffe, 1, 7, 6} {
		got, _ := client.readWord(output + uint32(i)*4)
		if got != v {
			t.Fatalf("clip[%d] = %#x", i, got)
		}
	}
	client.writeWord(clip+8, 0xfffffffe)
	client.transferContextFieldFor(t, pointer, grpFieldClip, clip)
	gc, err = client.contextFor(context.Background(), client.thread, client.screen, pointer)
	if err != nil {
		t.Fatal(err)
	}
	if gc.clipWidth != 0 || !gc.clipped(0, 2) {
		t.Fatal("empty clip draws pixels")
	}
}

func TestWideContextDoesNotSelectUnregisteredModules(t *testing.T) {
	if hasWideGraphicsContexts([]byte("authored module")) {
		t.Fatal("unknown module selected wide ABI")
	}
	client := fixtureClient(t)
	if client.wideGraphicsContexts {
		t.Fatal("fixture lost compact ABI")
	}
}

func TestWideExclusiveClipKeepsNativeBoundsAndColorKey(t *testing.T) {
	client := fixtureClient(t)
	client.wideGraphicsContexts, client.wideExclusiveClip = true, true
	pointer := writeGuest(t, client, make([]byte, wideContextSize))
	client.initContext(pointer)
	for i, want := range []uint32{0, 0, 16, 8} {
		got, err := client.readWord(pointer + uint32(i)*4)
		if err != nil || got != want {
			t.Fatalf("initial clip[%d]=%d, %v", i, got, err)
		}
	}
	// Direct native key and offset writes share a context with platform fields.
	client.writeWord(pointer+32, 0xf81f)
	client.writeWord(pointer+48, 0xffffffff)
	client.writeWord(pointer+52, 2)
	clip := writeGuest(t, client, make([]byte, 16))
	for i, v := range []uint32{1, 2, 5, 7} {
		client.writeWord(clip+uint32(i)*4, v)
	}
	callSlot(t, client, slotSetContext, pointer, grpFieldClip, clip)
	callSlot(t, client, slotSetContext, pointer, grpFieldForeground, 0x1234)
	gc, err := client.contextFor(context.Background(), client.thread, client.screen, pointer)
	if err != nil {
		t.Fatal(err)
	}
	if gc.clipWidth != 4 || gc.clipHeight != 5 || gc.foreground != 0x1234 || gc.offsetX != -1 || gc.offsetY != 2 {
		t.Fatalf("context = %+v", gc)
	}
	for offset, want := range map[uint32]uint32{8: 5, 12: 7, 32: 0xf81f} {
		got, err := client.readWord(pointer + offset)
		if err != nil || got != want {
			t.Fatalf("native field %d=%#x, %v", offset, got, err)
		}
	}
}
