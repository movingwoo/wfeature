package ktf

import (
	"context"
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// polygonPoints writes a coordinate array and answers where it went.
func polygonPoints(t *testing.T, runtime *initializationRuntime, address uint32, values ...uint32) uint32 {
	t.Helper()
	writeGuestWords(t, runtime, address, values...)
	return address
}

// MC_grpFillPolygon fills by the scanline the LGT runtime's C polygon and this
// runtime's Java Graphics draw: a right triangle (0,0), (3,0), (0,3) covers
// x 0 to 3-y on rows 0 to 2, and its bottom vertex row has no crossings.
func TestWIPICFillPolygonFillsTheTriangleItNames(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.currentThread, runtime.currentContext = client.thread, context.Background()
	const colour = uint16(0x07e0)
	handle, buffer, contextAddress := arcFixture(t, runtime, colour)
	xs := polygonPoints(t, runtime, platformDataBase+0xa000, 0, 3, 0)
	ys := polygonPoints(t, runtime, platformDataBase+0xa100, 0, 0, 3)

	callArcFunction(t, runtime, 38, handle, xs, ys, 3, contextAddress)

	for y := uint32(0); y < 4; y++ {
		for x := uint32(0); x < 5; x++ {
			want := uint16(0)
			if y < 3 && x <= 3-y {
				want = colour
			}
			if got := arcPixel(t, runtime, buffer, x, y); got != want {
				t.Fatalf("pixel (%d, %d) = %#x, want %#x", x, y, got, want)
			}
		}
	}
}

// MC_grpDrawPolygon draws the edges, the last back to the first, and leaves
// the inside alone; fewer than two vertices draw nothing.
func TestWIPICDrawPolygonDrawsTheOutline(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.currentThread, runtime.currentContext = client.thread, context.Background()
	const colour = uint16(0xf800)
	handle, buffer, contextAddress := arcFixture(t, runtime, colour)
	xs := polygonPoints(t, runtime, platformDataBase+0xa000, 1, 5, 5, 1)
	ys := polygonPoints(t, runtime, platformDataBase+0xa100, 1, 1, 5, 5)

	callArcFunction(t, runtime, 37, handle, xs, ys, 1, contextAddress)
	if got := arcPixel(t, runtime, buffer, 1, 1); got != 0 {
		t.Fatalf("one vertex drew pixel (1, 1) = %#x", got)
	}

	callArcFunction(t, runtime, 37, handle, xs, ys, 4, contextAddress)
	for _, point := range [][2]uint32{{1, 1}, {5, 1}, {5, 5}, {1, 5}, {3, 1}, {5, 3}, {3, 5}, {1, 3}} {
		if got := arcPixel(t, runtime, buffer, point[0], point[1]); got != colour {
			t.Fatalf("edge pixel (%d, %d) = %#x, want %#x", point[0], point[1], got, colour)
		}
	}
	if got := arcPixel(t, runtime, buffer, 3, 3); got != 0 {
		t.Fatalf("the inside pixel (3, 3) = %#x, want it untouched", got)
	}

	// A count past the bound is refused rather than read.
	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range []uint32{handle, xs, ys, maxWIPICPolygonPoints + 1} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := thread.SetRegister(armcore.RegisterSP, platformDataBase+0x8000); err != nil {
		t.Fatal(err)
	}
	writeGuestWords(t, runtime, platformDataBase+0x8000, contextAddress)
	if _, err := runtime.handleWIPICTableCall(thread, wipicTableGraphics, 38); err == nil {
		t.Fatal("a polygon of more points than the bound was read")
	}
}

// MC_grpGetUnicodeStringWidth measures UTF-16 text as entry 30 measures the
// same text in EUC-KR, counted or up to the terminating zero.
func TestWIPICGetUnicodeStringWidth(t *testing.T) {
	_, runtime := newTestRuntime(t)
	units := utf16.Encode([]rune("A가나\x00"))
	data := make([]byte, 2*len(units))
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[index*2:], unit)
	}
	address := platformDataBase + 0xa000
	if err := runtime.client.core.Memory().Write(address, data); err != nil {
		t.Fatal(err)
	}
	want := uint32(runtime.graphicsTextWidth([]rune("A가")))
	for _, length := range []uint32{2, 0xffffffff} {
		if length == 0xffffffff {
			want = uint32(runtime.graphicsTextWidth([]rune("A가나")))
		}
		thread := armcore.NewThread(armcore.NewContext())
		for index, value := range []uint32{0, address, length} {
			if err := thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
		}
		got, err := runtime.handleWIPICTableCall(thread, wipicTableGraphics, 31)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("width with length %d = %d, want %d", int32(length), got, want)
		}
	}
}

// MC_grpDecodeNextImage: an image here is decoded whole, so a surface has no
// further frame, and a handle that is no surface is a bad format.
func TestWIPICDecodeNextImage(t *testing.T) {
	_, runtime := newTestRuntime(t)
	handle, err := runtime.newWIPICFramebufferRecord(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		handle, want uint32
	}{{handle, wipicImageDone}, {0, wipicErrorBadFormat}} {
		thread := armcore.NewThread(armcore.NewContext())
		if err := thread.SetRegister(0, probe.handle); err != nil {
			t.Fatal(err)
		}
		got, err := runtime.handleWIPICTableCall(thread, wipicTableGraphics, 34)
		if err != nil {
			t.Fatal(err)
		}
		if got != probe.want {
			t.Errorf("decodeNextImage(%#x) = %#x, want %#x", probe.handle, got, probe.want)
		}
	}
}
