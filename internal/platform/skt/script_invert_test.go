package skt

import (
	"bytes"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"image"
	"testing"
)

func TestScriptInvertAllRawColorsAndRestore(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	for i := range s.graphics.pixels {
		s.graphics.pixels[i] = byte(i)
	}
	before := bytes.Clone(s.graphics.pixels)
	for pass := 0; pass < 2; pass++ {
		s.vm.Push(123)
		for _, v := range []int16{15, 15, 0, 0} {
			s.vm.Push(v)
		}
		if err := s.Call(0xcd, s.vm); err != nil {
			t.Fatal(err)
		}
		for i, value := range s.graphics.pixels {
			want := byte(i)
			if pass == 0 {
				want = ^want
			}
			if value != want {
				t.Fatalf("pass %d color %d: got %d want %d", pass, i, value, want)
			}
		}
		if s.vm.Pop() != 123 {
			t.Fatal("invert changed caller stack")
		}
	}
	if !bytes.Equal(before, s.graphics.pixels) {
		t.Fatal("double inversion did not restore image")
	}
}

func TestScriptInvertClipsInclusiveRectangle(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.graphics.clip = image.Rect(2, 3, 5, 6)
	for _, v := range []int16{-32768, -32768, 3, 4} {
		s.vm.Push(v)
	}
	if err := s.Call(0xcd, s.vm); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			want := byte(0)
			if x >= 2 && x <= 3 && y >= 3 && y <= 4 {
				want = 255
			}
			if s.graphics.pixels[y*16+x] != want {
				t.Fatal("inversion escaped intersection")
			}
		}
	}
}

func TestScriptInvertWorkFailureIsAtomic(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.ChargeWork(sgsvm.MaxSteps - 1)
	for _, v := range []int16{0, 0, 15, 15} {
		s.vm.Push(v)
	}
	if err := s.Call(0xcd, s.vm); err == nil {
		t.Fatal("inversion ignored work limit")
	}
	if !bytes.Equal(s.graphics.pixels, make([]byte, 256)) {
		t.Fatal("failed inversion changed pixels")
	}
}
