package skt

import (
	"bytes"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"image"
	"testing"
)

func TestScriptScrollDirectionsBuffersAndModes(t *testing.T) {
	for _, buffer := range []int16{0, 1} {
		for _, tc := range []struct {
			dx, dy, mode int16
			want         []byte
		}{
			{1, 0, 0, []byte{255, 1, 2, 255, 4, 5}}, {-1, 0, 0, []byte{2, 3, 255, 5, 6, 255}},
			{1, 1, 0, []byte{255, 255, 255, 255, 1, 2}}, {0, -1, 0, []byte{4, 5, 6, 255, 255, 255}},
			{1, 1, 1, []byte{6, 4, 5, 3, 1, 2}}, {-1, -1, 1, []byte{5, 6, 4, 2, 3, 1}},
			{3, 2, 1, []byte{1, 2, 3, 4, 5, 6}}, {0, 0, 0, []byte{1, 2, 3, 4, 5, 6}},
			{3, 0, 0, []byte{255, 255, 255, 255, 255, 255}},
			{-3, 0, 0, []byte{255, 255, 255, 255, 255, 255}},
			{32767, 32767, 1, []byte{6, 4, 5, 3, 1, 2}},
		} {
			fb, _ := backend.NewMemoryFramebuffer(3, 2)
			g, _ := newScriptGraphics(fb)
			g.pixels = []byte{1, 2, 3, 4, 5, 6}
			g.backup = bytes.Clone(g.pixels)
			g.clip = image.Rectangle{}
			g.bank = 6
			vm := sgsvm.New(&sgsvm.Program{}, nil)
			vm.Push(123)
			for _, v := range []int16{buffer, tc.dx, tc.dy, tc.mode} {
				vm.Push(v)
			}
			if err := (&ScriptSession{graphics: g}).Call(0xce, vm); err != nil {
				t.Fatal(err)
			}
			got, other := g.pixels, g.backup
			if buffer == 1 {
				got, other = other, got
			}
			if !bytes.Equal(got, tc.want) || !bytes.Equal(other, []byte{1, 2, 3, 4, 5, 6}) {
				t.Fatalf("buffer %d shift %d,%d mode %d got %v other %v", buffer, tc.dx, tc.dy, tc.mode, got, other)
			}
			if vm.Pop() != 123 {
				t.Fatal("scroll changed caller stack")
			}
		}
	}
}

func TestScriptScrollOversizeQuirkAndInvalidSelectors(t *testing.T) {
	for _, a := range [][4]int16{{1, 17, 0, 0}, {1, 0, -17, 0}, {0, -32768, 0, 0}, {2, 1, 1, 0}, {1, 1, 1, 2}, {-1, 0, 0, 0}} {
		s := newScriptTest(t, []byte{0xff}, nil)
		for i := range s.graphics.backup {
			s.graphics.backup[i] = 42
		}
		for _, v := range a {
			s.vm.Push(v)
		}
		if err := s.Call(0xce, s.vm); err != nil {
			t.Fatal(err)
		}
		want := byte(0)
		if a[0] >= 0 && a[0] <= 1 && a[3] == 0 {
			want = 255
		}
		for i, v := range s.graphics.pixels {
			if v != want || s.graphics.backup[i] != 42 {
				t.Fatal("oversized or invalid scroll changed wrong buffer")
			}
		}
	}
}

func TestScriptScrollBudgetFailureIsAtomic(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.ChargeWork(sgsvm.MaxSteps)
	for _, v := range []int16{0, 1, 1, 0} {
		s.vm.Push(v)
	}
	if err := s.Call(0xce, s.vm); err == nil {
		t.Fatal("scroll ignored work limit")
	}
	if !bytes.Equal(s.graphics.pixels, make([]byte, 256)) {
		t.Fatal("failed scroll partially wrote")
	}
}
