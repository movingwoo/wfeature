package backend

import (
	"bytes"
	"testing"
)

func TestMemoryFramebufferPresentsDefensiveSnapshot(t *testing.T) {
	framebuffer, err := NewMemoryFramebuffer(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	pixels := []byte{1, 2, 3, 255, 4, 5, 6, 255}
	if err := framebuffer.Present(Frame{Width: 2, Height: 1, RGBA: pixels}); err != nil {
		t.Fatal(err)
	}
	pixels[0] = 99

	frame, presents := framebuffer.Snapshot()
	if presents != 1 {
		t.Fatalf("presents = %d, want 1", presents)
	}
	if want := []byte{1, 2, 3, 255, 4, 5, 6, 255}; !bytes.Equal(frame.RGBA, want) {
		t.Fatalf("RGBA = %v, want %v", frame.RGBA, want)
	}
	frame.RGBA[1] = 99
	second, _ := framebuffer.Snapshot()
	if second.RGBA[1] != 2 {
		t.Fatalf("snapshot mutation changed framebuffer: %v", second.RGBA)
	}
}

func TestFramebufferRejectsInvalidDimensionsAndFrames(t *testing.T) {
	for _, dimensions := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {MaxFramebufferPixels, 2}} {
		if _, err := NewMemoryFramebuffer(dimensions[0], dimensions[1]); err == nil {
			t.Fatalf("NewMemoryFramebuffer(%d, %d) succeeded", dimensions[0], dimensions[1])
		}
	}

	framebuffer, err := NewMemoryFramebuffer(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := framebuffer.Present(Frame{Width: 1, Height: 2, RGBA: make([]byte, 8)}); err == nil {
		t.Fatal("Present accepted mismatched dimensions")
	}
	if err := framebuffer.Present(Frame{Width: 2, Height: 2, RGBA: make([]byte, 15)}); err == nil {
		t.Fatal("Present accepted a truncated frame")
	}
}
