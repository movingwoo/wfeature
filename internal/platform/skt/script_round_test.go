package skt

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func callScriptRound(g *scriptGraphics, vm *sgsvm.VM, op byte, args ...int16) error {
	for _, arg := range args {
		vm.Push(arg)
	}
	return g.roundCall(op, vm)
}

func TestScriptRoundedRectangleMidpointPixels(t *testing.T) {
	for _, tc := range []struct {
		op   byte
		want string
	}{
		{0xcb, ".####.\n#....#\n#....#\n#....#\n#....#\n.####."},
		{0xcc, ".####.\n######\n######\n######\n######\n.####."},
	} {
		for _, args := range [][]int16{{0, 0, 5, 5, 2}, {5, 5, 0, 0, -2}, {0, 0, 5, 5, 32767}, {0, 0, 5, 5, -32768}} {
			fb, _ := backend.NewMemoryFramebuffer(6, 6)
			g, _ := newScriptGraphics(fb)
			for i := range g.pixels {
				g.pixels[i] = 255
			}
			vm := sgsvm.New(&sgsvm.Program{}, nil)
			if err := callScriptRound(g, vm, tc.op, args...); err != nil {
				t.Fatal(err)
			}
			want := strings.ReplaceAll(tc.want, "\n", "")
			for i, c := range []byte(want) {
				p := byte(255)
				if c == '#' {
					p = 0
				}
				if g.pixels[i] != p {
					t.Fatalf("opcode %02x args%v pixel%d got%d want%d", tc.op, args, i, g.pixels[i], p)
				}
			}
			if _, count := fb.Snapshot(); count != 0 {
				t.Fatal("rounded drawing flushed")
			}
		}
	}
}

func TestScriptFilledRoundedRectangleNormalizesMiddle(t *testing.T) {
	for _, tc := range []struct {
		args []int16
		rows []int
	}{
		{[]int16{1, 2, 5, 2, 0}, []int{1, 2, 3}},
		{[]int16{1, 1, 5, 3, 1}, []int{1, 2, 3}},
		{[]int16{3, 1, 3, 3, 9}, []int{1, 2, 3}},
	} {
		fb, _ := backend.NewMemoryFramebuffer(7, 5)
		g, _ := newScriptGraphics(fb)
		for i := range g.pixels {
			g.pixels[i] = 255
		}
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		if err := callScriptRound(g, vm, 0xcc, tc.args...); err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 5; y++ {
			for x := 0; x < 7; x++ {
				want := byte(255)
				for _, row := range tc.rows {
					if y == row && x >= int(tc.args[0]) && x <= int(tc.args[2]) {
						want = 0
					}
				}
				if g.pixels[y*7+x] != want {
					t.Fatalf("args%v pixel(%d,%d) got%d want%d", tc.args, x, y, g.pixels[y*7+x], want)
				}
			}
		}
	}
}

func TestScriptRoundedRectangleClipsAndUsesPalette(t *testing.T) {
	for _, op := range []byte{0xcb, 0xcc} {
		fb, _ := backend.NewMemoryFramebuffer(6, 6)
		g, _ := newScriptGraphics(fb)
		for i := range g.pixels {
			g.pixels[i] = 17
		}
		g.clip = image.Rect(1, 0, 5, 2)
		g.bank = 0
		g.color = 3
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		if err := callScriptRound(g, vm, op, 0, 0, 5, 5, 2); err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 6; y++ {
			for x := 0; x < 6; x++ {
				if !image.Pt(x, y).In(g.clip) && g.pixels[y*6+x] != 17 {
					t.Fatal("drawing escaped clip")
				}
			}
		}
		if g.pixels[1] != 183 {
			t.Fatalf("palette bank ignored: %d", g.pixels[1])
		}
		before := bytes.Clone(g.pixels)
		g.color = 4
		if err := callScriptRound(g, vm, op, -32768, -32768, 32767, 32767, -32768); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, g.pixels) {
			t.Fatal("transparent rounded rectangle changed pixels")
		}
	}
}

func TestScriptRoundedRectangleRejectsBeforeDrawing(t *testing.T) {
	for _, op := range []byte{0xcb, 0xcc} {
		for _, underflow := range []bool{false, true} {
			fb, _ := backend.NewMemoryFramebuffer(8, 8)
			g, _ := newScriptGraphics(fb)
			for i := range g.pixels {
				g.pixels[i] = 17
			}
			vm := sgsvm.New(&sgsvm.Program{}, nil)
			args := []int16{-32768, -32768, 32767, 32767, 32767}
			if underflow {
				args = args[:4]
			} else if err := vm.ChargeWork(sgsvm.MaxSteps - 1); err != nil {
				t.Fatal(err)
			}
			if err := callScriptRound(g, vm, op, args...); err == nil {
				t.Fatalf("opcode%02x accepted malformed/exhausted draw", op)
			}
			for _, p := range g.pixels {
				if p != 17 {
					t.Fatal("failed draw partially changed pixels")
				}
			}
		}
	}
}

func TestScriptClippedRoundStillChargesMidpointIterations(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(1, 1)
	g, _ := newScriptGraphics(fb)
	g.clip = image.Rectangle{}
	vm := sgsvm.New(&sgsvm.Program{}, nil)
	if err := vm.ChargeWork(sgsvm.MaxSteps - 1000); err != nil {
		t.Fatal(err)
	}
	if err := callScriptRound(g, vm, 0xcc, -32768, -32768, 32767, 32767, 32767); err == nil {
		t.Fatal("fully clipped midpoint loop escaped work accounting")
	}
}
