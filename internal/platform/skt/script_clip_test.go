package skt

import (
	"image"
	"testing"
)

func TestScriptClipInclusiveSortedAndReset(t *testing.T) {
	for _, tc := range []struct {
		args []int16
		want image.Rectangle
	}{
		{[]int16{3, 4, 1, 2}, image.Rect(1, 2, 4, 5)},
		{[]int16{-32768, -32768, 32767, 32767}, image.Rect(0, 0, 16, 16)},
		{[]int16{2, 3, 2, 3}, image.Rect(2, 3, 3, 4)},
		{[]int16{-4, -3, -1, -1}, image.Rectangle{}},
		{[]int16{20, 20, 30, 30}, image.Rectangle{}},
	} {
		s := newScriptTest(t, []byte{0xff}, nil)
		for _, v := range tc.args {
			s.vm.Push(v)
		}
		if err := s.Call(0xc8, s.vm); err != nil {
			t.Fatal(err)
		}
		if s.graphics.clip != tc.want {
			t.Fatalf("clip %v want %v", s.graphics.clip, tc.want)
		}
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				s.graphics.point(x, y, 255)
			}
		}
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				if (s.graphics.pixels[y*16+x] == 255) != image.Pt(x, y).In(tc.want) {
					t.Fatal("drawing escaped clip")
				}
			}
		}
		if err := s.Call(0xc9, s.vm); err != nil {
			t.Fatal(err)
		}
		if s.graphics.clip != image.Rect(0, 0, 16, 16) {
			t.Fatal("clip reset failed")
		}
	}
}

func TestScriptReadPixelIgnoresClipAndReturnsUnsignedByte(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.graphics.pixels[0] = 255
	s.graphics.clip = image.Rect(2, 2, 3, 3)
	for _, tc := range []struct{ x, y, want int16 }{{0, 0, 255}, {15, 15, 0}, {-1, 0, -1}, {0, -1, -1}, {16, 0, -1}, {0, 16, -1}, {32767, -32768, -1}} {
		s.vm.Push(123)
		s.vm.Push(tc.x)
		s.vm.Push(tc.y)
		if err := s.Call(0xca, s.vm); err != nil {
			t.Fatal(err)
		}
		if got := s.vm.Pop(); got != tc.want {
			t.Fatalf("pixel(%d,%d)=%d want %d", tc.x, tc.y, got, tc.want)
		}
		if s.vm.Pop() != 123 {
			t.Fatal("read changed caller stack")
		}
	}
}

func TestScriptClipUnderflowPreservesState(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	before := s.graphics.clip
	s.vm.Push(4)
	if err := s.Call(0xc8, s.vm); err == nil {
		t.Fatal("clip accepted missing arguments")
	}
	if s.graphics.clip != before || s.vm.Pop() != 4 {
		t.Fatal("failed clip changed state")
	}
}
