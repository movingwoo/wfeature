package lgt

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func encodeTestRegion(t *testing.T, client *Client, handle uint32, x, y, w, h int32, length uint32) (uint32, error) {
	t.Helper()
	stack := writeGuest(t, client, make([]byte, 8))
	_ = client.writeWord(stack, uint32(h))
	_ = client.writeWord(stack+4, length)
	state := armcore.NewContext()
	state.Registers[0] = handle
	state.Registers[1] = uint32(x)
	state.Registers[2] = uint32(y)
	state.Registers[3] = uint32(w)
	state.Registers[armcore.RegisterSP] = stack
	thread := armcore.NewThread(state)
	err := client.handleWIPICSVC(context.Background(), thread, 0xed)
	result, _ := thread.Register(0)
	return result, err
}

func TestEncodeImageRoundTripsGuestPixelsAndReleasesBuffer(t *testing.T) {
	client := fixtureClient(t)
	fb, err := client.newFramebuffer(3, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	pixels := []uint16{0xf800, 0x07e0, 0x001f, 0xffff, 0x0000, 0xf800}
	data := make([]byte, len(pixels)*2)
	for i, p := range pixels {
		binary.LittleEndian.PutUint16(data[i*2:], p)
	}
	if err := client.core.Memory().Write(fb.address, data); err != nil {
		t.Fatal(err)
	}
	out := writeGuest(t, client, make([]byte, 4))
	used := client.heap.used()
	handle, err := encodeTestRegion(t, client, fb.handle, 1, 0, 2, 2, out)
	if err != nil {
		t.Fatal(err)
	}
	if handle == 0 {
		t.Fatal("encoding returned null")
	}
	size, err := client.readWord(out)
	if err != nil {
		t.Fatal(err)
	}
	encoded := make([]byte, size)
	if err := client.core.Memory().Read(handle, encoded); err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 2 || string(encoded[:2]) != "BM" {
		t.Fatal("not a BMP")
	}
	image, status := createTestImage(t, client, encoded)
	if status != imageDone {
		t.Fatalf("decode status %d", status)
	}
	decoded := client.framebuffer(image)
	if decoded.width != 2 || decoded.height != 2 {
		t.Fatalf("decoded size %dx%d", decoded.width, decoded.height)
	}
	for i, want := range []uint16{0x07e0, 0x001f, 0x0000, 0xf800} {
		if decoded.pixels[i] != want {
			t.Fatalf("pixel %d=%x, want %x", i, decoded.pixels[i], want)
		}
	}
	callSlot(t, client, slotFree, handle)
	if client.heap.used() != used {
		t.Fatal("encoded buffer is not caller-owned heap memory")
	}
}

func TestEncodeImageClipsAndRejectsInvalidRegions(t *testing.T) {
	client := fixtureClient(t)
	fb, err := client.newFramebuffer(3, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	out := writeGuest(t, client, make([]byte, 4))
	for _, test := range []struct {
		x, y, w, h int32
		valid      bool
	}{{-1, -1, 3, 3, true}, {2, 1, 2147483647, 2147483647, true}, {0, 0, 0, 1, false}, {0, 0, -1, 1, false}, {-2147483648, 0, 2147483647, 1, false}, {2147483647, 0, 1, 1, false}} {
		_ = client.writeWord(out, 0x1234)
		result, err := encodeTestRegion(t, client, fb.handle, test.x, test.y, test.w, test.h, out)
		if err != nil {
			t.Fatal(err)
		}
		size, _ := client.readWord(out)
		if test.valid {
			if result == 0 || size == 0 {
				t.Fatalf("valid region refused: %+v", test)
			}
			callSlot(t, client, slotFree, result)
		} else if result != 0 || size != 0 {
			t.Fatalf("invalid region accepted: %+v", test)
		}
	}
	if result, err := encodeTestRegion(t, client, 0xffffffff, 0, 0, 1, 1, out); err != nil || result != 0 {
		t.Fatalf("invalid framebuffer = %x/%v", result, err)
	}
	used := client.heap.used()
	if _, err := encodeTestRegion(t, client, fb.handle, 0, 0, 1, 1, 0xfffffff0); err == nil {
		t.Fatal("invalid length pointer accepted")
	}
	if client.heap.used() != used {
		t.Fatal("failed encoding leaked a buffer")
	}
	if result, err := encodeTestRegion(t, client, fb.handle, 0, 0, 1, 1, 0); err != nil || result == 0 {
		t.Fatalf("optional length pointer = %x/%v", result, err)
	} else {
		callSlot(t, client, slotFree, result)
	}
}
