package skt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/glyph"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func textGraphicsTest(t *testing.T, data []byte) (*scriptGraphics, *sgsvm.VM) {
	t.Helper()
	fb, _ := backend.NewMemoryFramebuffer(64, 64)
	g, err := newScriptGraphics(fb)
	if err != nil {
		t.Fatal(err)
	}
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: data}}}, nil)
	return g, vm
}

func drawTextTest(t *testing.T, g *scriptGraphics, vm *sgsvm.VM, op byte, x, y int16) {
	t.Helper()
	vm.Push(x)
	vm.Push(y)
	vm.Push(0)
	if _, err := g.textCall(op, vm); err != nil {
		t.Fatal(err)
	}
}

func TestScriptTextPreservesNativeGridAndBaseline(t *testing.T) {
	// EUC-KR first syllable, then ASCII with an ascender and a descender.
	g, vm := textGraphicsTest(t, []byte{0xb0, 0xa1, 'A', 'g', 0})
	g.textFG = 5
	drawTextTest(t, g, vm, 0x6a, 2, 3)
	fg, _ := g.mappedColor(5)
	want := make([]byte, len(g.pixels))
	x := 2
	for _, r := range "가Ag" {
		bitmap := glyph.Handset().Render(r)
		w := 6
		if r > 127 {
			w = 12
		}
		for row := range bitmap.Rows {
			y := 3 + glyph.Handset().Ascent - bitmap.Ascent + row
			for col := 0; col < min(bitmap.Width, w); col++ {
				if bitmap.Coverage(row, col) >= 128 {
					want[y*g.width+x+col] = fg
				}
			}
		}
		x += w
	}
	if !bytes.Equal(g.pixels, want) {
		t.Fatal("glyph grid, fixed advances or shared baseline changed")
	}
}

func TestScriptTextLargeStyleDoublesOrdinaryPixels(t *testing.T) {
	small, svm := textGraphicsTest(t, []byte{0xb0, 0xa1, 'A', 0})
	large, lvm := textGraphicsTest(t, []byte{0xb0, 0xa1, 'A', 0})
	small.textFG = 5
	large.textFG = 5
	large.textStyle = 3
	drawTextTest(t, small, svm, 0x6a, 0, 0)
	drawTextTest(t, large, lvm, 0x6a, 0, 0)
	for y := 0; y < 12; y++ {
		for x := 0; x < 18; x++ {
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					if large.pixels[(1+y*2+dy)*large.width+1+x*2+dx] != small.pixels[y*small.width+x] {
						t.Fatalf("large style changed pixel at %d,%d", x, y)
					}
				}
			}
		}
	}
}

func TestScriptTextAlignmentTerminatorAndBackground(t *testing.T) {
	g, vm := textGraphicsTest(t, []byte{'A', 0, 'B'})
	g.textFG = 4
	g.textBG = 5
	g.textAlign = 2
	drawTextTest(t, g, vm, 0x6b, 20, 4)
	fg, _ := g.mappedColor(5)
	for y := 0; y < g.height; y++ {
		for x := 0; x < g.width; x++ {
			want := byte(0)
			if x >= 13 && x < 20 && y >= 3 && y < 16 {
				want = fg
			}
			if g.pixels[y*g.width+x] != want {
				t.Fatalf("background at %d,%d", x, y)
			}
		}
	}
}

func TestScriptTextRejectsInvalidResourceBeforeDrawing(t *testing.T) {
	g, vm := textGraphicsTest(t, []byte{'A', 0})
	vm.Push(0)
	vm.Push(0)
	vm.Push(1)
	if _, err := g.textCall(0x6a, vm); err == nil {
		t.Fatal("invalid text resource accepted")
	}
	if !bytes.Equal(g.pixels, make([]byte, len(g.pixels))) {
		t.Fatal("invalid text mutated framebuffer")
	}
}

func TestScriptEmptyTextLeavesBackgroundUntouched(t *testing.T) {
	g, vm := textGraphicsTest(t, []byte{0})
	g.textBG = 5
	drawTextTest(t, g, vm, 0x6b, 5, 5)
	if !bytes.Equal(g.pixels, make([]byte, len(g.pixels))) {
		t.Fatal("empty text painted background")
	}
}

func TestScriptSmallTextPreservesCompleteHUDStrokes(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
		rows  [5]uint16
	}{
		{"GOLD", 16, [5]uint16{0x648c, 0x8a8a, 0xaa8a, 0xaa8a, 0x64ec}},
		{"EXP", 12, [5]uint16{0xeac, 0x8aa, 0xc4c, 0x8a8, 0xea8}},
		{"0/0", 12, [5]uint16{0xe2e, 0xa2a, 0xa4a, 0xa8a, 0xe8e}},
	} {
		g, vm := textGraphicsTest(t, append([]byte(tc.text), 0))
		g.textStyle = 0
		g.textFG = 5
		drawTextTest(t, g, vm, 0x6a, 2, 3)
		fg, _ := g.mappedColor(5)
		for y := 0; y < 6; y++ {
			for x := 0; x < tc.width; x++ {
				want := byte(0)
				if y < 5 && tc.rows[y]&(1<<uint(tc.width-1-x)) != 0 {
					want = fg
				}
				if got := g.pixels[(3+y)*g.width+2+x]; got != want {
					t.Fatalf("%q pixel %d,%d=%d, want %d", tc.text, x, y, got, want)
				}
			}
		}
	}
}

func TestScriptSmallTextCoversPrintableASCIIOnThreeColumns(t *testing.T) {
	for r := byte(' '); r <= '~'; r++ {
		key := r
		if key >= 'a' && key <= 'z' {
			key -= 'a' - 'A'
		}
		rows, ok := scriptLatin3x5[key]
		if !ok {
			t.Fatalf("missing printable ASCII 0x%02x", r)
		}
		ink := byte(0)
		for _, row := range rows {
			if row > 7 {
				t.Fatalf("ASCII 0x%02x escapes three columns", r)
			}
			ink |= row
		}
		if r != ' ' && ink == 0 {
			t.Fatalf("ASCII 0x%02x has no strokes", r)
		}
	}
}

func drawExtendedTextTest(g *scriptGraphics, vm *sgsvm.VM, op byte, x, y, flags int16) error {
	vm.Push(x)
	vm.Push(y)
	vm.Push(0)
	vm.Push(flags)
	_, err := g.textCall(op, vm)
	return err
}

func TestScriptExtendedTextEffectsKeepAlignmentAndAdvances(t *testing.T) {
	for style := 0; style < 3; style++ {
		for flags := 0; flags < 8; flags++ {
			base, bvm := textGraphicsTest(t, []byte("AA\x00"))
			got, vm := textGraphicsTest(t, []byte("AA\x00"))
			base.textStyle = style
			got.textStyle = style
			base.textAlign = 2
			got.textAlign = 2
			base.textFG = 5
			got.textFG = 5
			drawTextTest(t, base, bvm, 0x6a, 30, 7)
			if err := drawExtendedTextTest(got, vm, 0xcf, 30, 7, int16(flags)); err != nil {
				t.Fatal(err)
			}
			want := make([]byte, len(got.pixels))
			fg, _ := got.mappedColor(5)
			h := []int{6, 8, 12}[style]
			w := []int{4, 6, 6}[style]
			left := 30 - w*2
			for row := 0; row < h; row++ {
				shift := 0
				if flags&2 != 0 {
					shift = max(0, h/4-1-(row+1)/4)
				}
				for x := left; x < 30; x++ {
					if base.pixels[(7+row)*base.width+x] != 0 {
						for dx := 0; dx <= flags&1; dx++ {
							want[(7+row)*got.width+x+shift+dx] = fg
						}
					}
				}
			}
			if flags&4 != 0 {
				for x := left - 1; x <= 30+(flags&1)-1; x++ {
					want[(7+h)*got.width+x] = fg
				}
			}
			if !bytes.Equal(got.pixels, want) {
				t.Fatalf("style%d flags%d changed effects/layout", style, flags)
			}
		}
	}
}

func TestScriptExtendedTextBackgroundAndNegativeFlags(t *testing.T) {
	g, vm := textGraphicsTest(t, []byte("A\x00"))
	g.textStyle = 2
	g.textFG = 4
	g.textBG = 5
	if err := drawExtendedTextTest(g, vm, 0xd0, 10, 10, -1); err != nil {
		t.Fatal(err)
	}
	fg, _ := g.mappedColor(5)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			want := byte(0)
			if x >= 9 && x < 19 && y >= 9 && y < 22 {
				want = fg
			}
			if g.pixels[y*64+x] != want {
				t.Fatalf("background pixel%d,%d", x, y)
			}
		}
	}
	positive, pvm := textGraphicsTest(t, []byte("A\x00"))
	negative, nvm := textGraphicsTest(t, []byte("A\x00"))
	positive.textFG = 5
	negative.textFG = 5
	if err := drawExtendedTextTest(positive, pvm, 0xcf, 10, 10, 7); err != nil {
		t.Fatal(err)
	}
	if err := drawExtendedTextTest(negative, nvm, 0xcf, 10, 10, -1); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(positive.pixels, negative.pixels) {
		t.Fatal("negative flag mask changed effects")
	}
}

func TestScriptExtendedLargeTextUsesObservedContinuousSource(t *testing.T) {
	base, bvm := textGraphicsTest(t, []byte("A\x00"))
	base.textFG = 5
	drawTextTest(t, base, bvm, 0x6a, 0, 0)
	for flags := 0; flags < 4; flags++ {
		g, vm := textGraphicsTest(t, []byte("A\x00"))
		g.textStyle = 3
		g.textFG = 5
		if err := drawExtendedTextTest(g, vm, 0xcf, -1, 10, int16(flags)); err != nil {
			t.Fatal(err)
		}
		want := make([]byte, len(g.pixels))
		fg, _ := g.mappedColor(5)
		for row := 0; row < 11; row++ {
			for col := 0; col < 3; col++ {
				bit := row*3 + col
				if base.pixels[(bit/5)*base.width+bit%5] == 0 {
					continue
				}
				shift := 0
				if flags&2 != 0 {
					shift = max(0, 4-row/2)
				}
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2+2*(flags&1); dx++ {
						want[(11+row*2+dy)*g.width+col*2+shift+dx] = fg
					}
				}
			}
		}
		if !bytes.Equal(g.pixels, want) {
			t.Fatalf("large source traversal flags%d", flags)
		}
	}
	for _, x := range []int16{4, 10, 30} {
		g, vm := textGraphicsTest(t, []byte("A\x00"))
		g.textStyle = 3
		g.textFG = 5
		if err := drawExtendedTextTest(g, vm, 0xcf, x, 10, 0); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(g.pixels, make([]byte, len(g.pixels))) {
			t.Fatalf("large glyph ignored native endpoint at x%d", x)
		}
	}
}

func TestScriptExtendedTextRejectsBoundsAndWorkBeforePainting(t *testing.T) {
	for _, exhaustWork := range []bool{false, true} {
		g, vm := textGraphicsTest(t, []byte("A\x00"))
		g.textStyle = 3
		g.textFG = 5
		g.textBG = 5
		x := int16(-7)
		if exhaustWork {
			x = 0
			if err := vm.ChargeWork(sgsvm.MaxSteps); err != nil {
				t.Fatal(err)
			}
		}
		if err := drawExtendedTextTest(g, vm, 0xd0, x, 10, 7); err == nil {
			t.Fatal("invalid extended call accepted")
		}
		if !bytes.Equal(g.pixels, make([]byte, len(g.pixels))) {
			t.Fatal("failed extended call painted pixels")
		}
	}
}
