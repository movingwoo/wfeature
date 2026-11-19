package skt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestWideIndexedBitmap(t *testing.T) {
	// Two RGB palette colors, transparent index zero, three continuous pixels 1,0,1.
	data := []byte{9, 3, 1, 1, 0, 1, 2, 1, 2, 3, 12, 34, 56, 0xa0}
	img, err := readWideBitmap(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := img.pixel(0); !ok || c != 0x0c2238 {
		t.Fatalf("first pixel = %#x, %v", c, ok)
	}
	if _, ok := img.pixel(1); ok {
		t.Fatal("transparent pixel is opaque")
	}
	if _, err := readWideBitmap(data[:len(data)-1], nil); err == nil {
		t.Fatal("accepted truncated raster")
	}
}

func TestWidePackedHandsetPalette(t *testing.T) {
	img, err := readWideBitmap([]byte{2, 2, 1, 0, 0, 0x43, 0x40, 0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img.pixel(0); ok {
		t.Fatal("first pixel should be transparent")
	}
	if c, ok := img.pixel(1); !ok || c != 0 {
		t.Fatalf("black pixel = %#x, %v", c, ok)
	}
}

func TestWideScreenPresentation(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(4, 3)
	screen, err := newWideScreen(fb)
	if err != nil {
		t.Fatal(err)
	}
	img, err := readWideBitmap([]byte{9, 2, 1, 1, 0, 0, 2, 255, 0, 0, 0, 255, 0, 0x40}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.blit(img, 1, 1, 2, 1, 0, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, n := fb.Snapshot(); n != 0 {
		t.Fatal("early presentation")
	}
	if err := screen.present(screen.full()); err != nil {
		t.Fatal(err)
	}
	f, _ := fb.Snapshot()
	if f.RGBA[(1*4+1)*4+1] != 255 || f.RGBA[(1*4+2)*4] != 255 {
		t.Fatalf("mirrored pixels = %v", f.RGBA[20:28])
	}
}

func TestWideScreenBoundsNativeWork(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(64, 64)
	s, err := newWideScreen(fb)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []struct {
		id   uint16
		args []int32
	}{
		{0x3a, nil}, {0x46, []int32{-1 << 30, -1 << 30, 1 << 30, 1 << 30}},
		{0x47, []int32{0, 0, 63, 63, 2}}, {0x51, nil}, {0x52, nil}, {0x5f, nil},
		{0x60, []int32{0, 0, 0, 0}}, {0x42, []int32{-1 << 30, -1 << 30, 1 << 30, 1 << 30}},
		{0x4c, []int32{-5000, -5000, 5000, 5000}},
	} {
		vm := new(sgsvm.WideVM)
		if err := vm.Charge(sgsvm.MaxSteps); err != nil {
			t.Fatal(err)
		}
		if err := s.draw(op.id, op.args, vm); err == nil {
			t.Errorf("operation %#x escaped budget", op.id)
		}
	}
	if _, n := fb.Snapshot(); n != 0 {
		t.Fatal("failed flush reached host")
	}
}

func TestWideScreenHandsetPaletteAndBlend(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(3, 1)
	s, err := newWideScreen(fb)
	if err != nil {
		t.Fatal(err)
	}
	vm := new(sgsvm.WideVM)
	if err := s.draw(0x3a, nil, vm); err != nil {
		t.Fatal(err)
	}
	if err := s.draw(0x3c, []int32{0, 0, 1}, vm); err != nil {
		t.Fatal(err)
	}
	if err := s.draw(0x3c, []int32{1, 0, 2}, vm); err != nil {
		t.Fatal(err)
	}
	if err := s.draw(0x47, []int32{2, 0, 2, 0, 3}, vm); err != nil {
		t.Fatal(err)
	}
	if err := s.present(s.full()); err != nil {
		t.Fatal(err)
	}
	f, _ := fb.Snapshot()
	want := []byte{197, 194, 197, 255, 131, 129, 131, 255, 180, 186, 180, 255}
	if !bytes.Equal(f.RGBA, want) {
		t.Fatalf("palette and RGB565 blend: %v", f.RGBA)
	}
}

func TestWideDrawTextRetainsAlignment(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(64, 24)
	s, err := newWideScreen(fb)
	if err != nil {
		t.Fatal(err)
	}
	vm := &sgsvm.WideVM{Resources: []sgsvm.Resource{{Data: []byte("ABCD\x00")}}}
	for _, op := range []struct {
		id uint16
		a  []int32
	}{{0x3a, nil}, {0x28, []int32{0, 3, 0, 1}}, {0x57, []int32{32, 4, 0}}, {0x5f, nil}} {
		if err := s.draw(op.id, op.a, vm); err != nil {
			t.Fatal(err)
		}
	}
	f, _ := fb.Snapshot()
	left, right := 64, -1
	for y := 0; y < 24; y++ {
		for x := 0; x < 64; x++ {
			if f.RGBA[4*(64*y+x)] == 0 {
				left = min(left, x)
				right = max(right, x)
			}
		}
	}
	if left < 20 || left >= 26 || right < 38 || right >= 44 {
		t.Fatalf("centered large text occupies x=%d..%d", left, right)
	}
}

func TestWideGammaAppliesToImagesAndPalette(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(3, 1)
	s, err := newWideScreen(fb)
	if err != nil {
		t.Fatal(err)
	}
	vm := &sgsvm.WideVM{Resources: []sgsvm.Resource{{Data: []byte{9, 1, 1, 0, 0, 0, 1, 193, 61, 95, 0, 0}}}}
	for _, entry := range []struct {
		id   uint16
		args []int32
	}{
		{0x25, []int32{2}}, {0xf094, []int32{0, 0, 0}}, {0x3c, []int32{1, 0, 1}}, {0x3e, []int32{2, 0, 193, 61, 95}}, {0x5f, nil},
	} {
		if err := s.draw(entry.id, entry.args, vm); err != nil {
			t.Fatal(err)
		}
	}
	f, _ := fb.Snapshot()
	if want := []byte{230, 157, 172, 255, 230, 226, 230, 255, 197, 60, 90, 255}; !bytes.Equal(f.RGBA, want) {
		t.Fatalf("gamma frame %v", f.RGBA)
	}
}
