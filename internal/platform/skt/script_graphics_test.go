package skt

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptGraphicsPresentAndRestore(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(3, 2)
	g, err := newScriptGraphics(fb)
	if err != nil {
		t.Fatal(err)
	}
	vm := sgsvm.New(&sgsvm.Program{}, nil)
	call := func(op byte, args ...int16) {
		t.Helper()
		for _, a := range args {
			vm.Push(a)
		}
		handled, err := g.call(op, vm)
		if err != nil || !handled {
			t.Fatalf("opcode %02x: handled=%v err=%v", op, handled, err)
		}
	}
	call(0x55)
	call(0x76)
	call(0x5e, 3)
	call(0x63, -20, -20, 1, 0)
	if !bytes.Equal(g.pixels, []byte{0, 0, 255, 255, 255, 255}) {
		t.Fatalf("clipped inclusive rectangle: %v", g.pixels)
	}
	_, n := fb.Snapshot()
	if n != 0 {
		t.Fatal("drawing presented before flush")
	}
	call(0x78)
	frame, n := fb.Snapshot()
	if n != 1 || frame.RGBA[8] != 255 || frame.RGBA[3] != 255 {
		t.Fatalf("bad presented frame %v", frame)
	}
	call(0x77)
	for _, p := range g.pixels {
		if p != 255 {
			t.Fatal("backup restore failed")
		}
	}
	if handled, err := g.call(0xf2, vm); handled || err != nil {
		t.Fatal("unimplemented operations must remain unhandled")
	}
}

func TestScriptBitmapPackingOriginAndTransparency(t *testing.T) {
	for _, kind := range []byte{2, 3, 4, 5, 6, 7, 8} {
		t.Run(string(rune('0'+kind)), func(t *testing.T) {
			fb, _ := backend.NewMemoryFramebuffer(4, 3)
			g, _ := newScriptGraphics(fb)
			for i := range g.pixels {
				g.pixels[i] = 17
			}
			// A newly authored 3x2 image with white, black, white / black, white, black.
			data := []byte{kind, 3, 2, 1, 1}
			switch kind {
			case 2:
				data = append(data, 0x03, 0x54)
			case 3:
				data = append(data, 0x03, 0x40, 0x11, 0x10)
			case 4:
				data = append(data, 0x03, 0x03, 0x03)
			case 5:
				data = append(data, 0, 3, 0x54)
			case 6:
				data = append(data, 0, 3, 4, 0, 0x11, 0x10)
			case 7:
				for i := 0; i < 16; i++ {
					data = append(data, byte(i))
				}
				data = append(data, 0x03, 0x03, 0x03)
			case 8:
				data = append(data, 0, 3, 0, 3, 0, 3)
			}
			if err := g.bitmap(data, 2, 2); err != nil {
				t.Fatal(err)
			}
			want := []byte{17, 17, 17, 17, 17, 255, 0, 255, 17, 0, 255, 0}
			if !bytes.Equal(g.pixels, want) {
				t.Fatalf("pixels=%v want%v", g.pixels, want)
			}
		})
	}
	fb, _ := backend.NewMemoryFramebuffer(2, 1)
	g, _ := newScriptGraphics(fb)
	g.pixels[0] = 17
	if err := g.bitmap([]byte{8, 2, 1, 0, 0, 4, 0}, 0, 0); err != nil {
		t.Fatal(err)
	}
	if g.pixels[0] != 17 || g.pixels[1] != 255 {
		t.Fatalf("transparency %v", g.pixels)
	}
}

func TestScriptBitmapRejectsMalformedWithoutDrawing(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(2, 1)
	g, _ := newScriptGraphics(fb)
	for _, data := range [][]byte{{}, {8, 2, 1, 0, 0, 0}, {1, 2, 1, 0, 0}, {8, 2, 1, 0, 0, 0, 182}} {
		if err := g.bitmap(data, 0, 0); err == nil {
			t.Fatalf("accepted malformed %v", data)
		}
		if g.pixels[0] != 0 || g.pixels[1] != 0 {
			t.Fatal("invalid image partially changed pixels")
		}
	}
}

func TestScriptPaletteBanks(t *testing.T) {
	// Independently selected RGB332 primaries and endpoints exercise each
	// channel's saturation and rounding, including the two darkening steps.
	for _, tc := range []struct {
		c    byte
		bank int
		want byte
	}{
		{0, 0, 183}, {0, 1, 146}, {0, 2, 73}, {0, 3, 0}, {255, 4, 73}, {255, 5, 36}, {255, 6, 0},
		{8, 0, 187}, {20, 1, 158}, {12, 2, 81}, {20, 4, 8}, {12, 5, 4},
	} {
		if got := scriptPaletteBank(tc.c, tc.bank); got != tc.want {
			t.Fatalf("color %d bank %d: got %d want %d", tc.c, tc.bank, got, tc.want)
		}
	}
	fb, _ := backend.NewMemoryFramebuffer(2, 1)
	g, _ := newScriptGraphics(fb)
	vm := sgsvm.New(&sgsvm.Program{}, nil)
	g.pixels[0] = 255
	vm.Push(6)
	if _, err := g.call(0x59, vm); err != nil {
		t.Fatal(err)
	}
	if g.pixels[0] != 255 {
		t.Fatal("palette switch recolored existing image")
	}
	vm.Push(1)
	vm.Push(0)
	vm.Push(0)
	if _, err := g.call(0x58, vm); err != nil {
		t.Fatal(err)
	}
	if g.pixels[1] != 0 {
		t.Fatal("drawing did not use selected bank")
	}
}

func TestScriptBitmapMirroredAnchor(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(5, 1)
	g, _ := newScriptGraphics(fb)
	// Original anchor minus width plus origin places the flipped left edge at2.
	if err := g.bitmapMirrored([]byte{8, 3, 1, 1, 0, 0, 3, 4}, 4, 0, true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g.pixels, []byte{0, 0, 0, 0, 255}) {
		t.Fatalf("mirrored pixels: %v", g.pixels)
	}
}

func TestScriptPackPaletteKindsAndBytePreservation(t *testing.T) {
	for _, tc := range []struct {
		kind   int16
		colors []int16
		want   []int16
	}{
		{2, []int16{0x21, 0x32}, []int16{0x5512}},
		{3, []int16{0, 3, 4, 5}, []int16{0x4503}},
		{4, []int16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, []int16{0x2301, 0x6745, -21623, -4147}},
		{5, []int16{0x123, 0x104}, []int16{0x0423}},
		{6, []int16{1, 2, 3, 4}, []int16{0x0201, 0x0403}},
		{7, []int16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, []int16{0x0100, 0x0302, 0x0504, 0x0706, 0x0908, 0x0b0a, 0x0d0c, 0x0f0e}},
	} {
		t.Run(string(rune('0'+tc.kind)), func(t *testing.T) {
			vm := sgsvm.New(&sgsvm.Program{
				Constants: append([]int16{tc.kind}, tc.colors...),
				Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{0x5555, 0x5555, 0x5555, 0x5555, 0x5555, 0x5555, 0x5555, 0x5555}}},
			}, nil)
			if err := scriptPackPalette(vm, 0, 0x4000); err != nil {
				t.Fatal(err)
			}
			for i, want := range tc.want {
				if got := vm.AddressRead(int16(i)); got != want {
					t.Fatalf("word %d: got %04x, want %04x", i, uint16(got), uint16(want))
				}
			}
		})
	}
}

func TestScriptPackPaletteBoundsAreAtomic(t *testing.T) {
	for _, tc := range []struct {
		name        string
		constants   []int16
		destination int16
		offset      int
	}{
		{"source", []int16{6, 1, 2, 3}, 0, 0},
		{"destination", []int16{6, 1, 2, 3, 4}, 0, 0},
		{"bank boundary", []int16{6, 1, 2, 3, 4}, 0x3fff, 0x3fff},
		{"negative destination", []int16{2, 1, 2}, -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := sgsvm.New(&sgsvm.Program{Constants: tc.constants, Variables: []sgsvm.Variable{{Mutable: true, Offset: tc.offset, Values: []int16{0x5555}}}}, nil)
			if err := scriptPackPalette(vm, tc.destination, 0x4000); err == nil {
				t.Fatal("accepted invalid palette span")
			}
			if vm.Variables[0].Values[0] != 0x5555 {
				t.Fatal("invalid palette partially changed destination")
			}
		})
	}
}

func TestScriptBitmapOverridePaletteAndMirror(t *testing.T) {
	for _, tc := range []struct {
		kind    byte
		palette []int16
		payload []byte
	}{
		{2, []int16{0x43}, []byte{0x00, 0x40}},
		{3, []int16{0x0043}, []byte{0, 0, 0x10}},
		{5, []int16{0x0304}, []byte{0, 0, 0x40}},
		{6, []int16{0x0304, 0}, []byte{0, 0, 0, 0, 0x10}},
		{7, []int16{0x0304, 0, 0, 0, 0, 0, 0, 0}, append(make([]byte, 16), 0x01)},
	} {
		for _, op := range []byte{0x71, 0x72} {
			fb, _ := backend.NewMemoryFramebuffer(2, 1)
			g, _ := newScriptGraphics(fb)
			g.pixels[0], g.pixels[1] = 255, 255
			vm := sgsvm.New(&sgsvm.Program{Constants: tc.palette, Resources: []sgsvm.Resource{{Data: append([]byte{tc.kind, 2, 1, 0, 0}, tc.payload...)}}}, nil)
			x := int16(0)
			if op == 0x72 {
				x = 2
			}
			vm.Push(x)
			vm.Push(0)
			vm.Push(0)
			if op == 0x72 {
				vm.Push(1)
			}
			vm.Push(0x4000)
			if handled, err := g.call(op, vm); !handled || err != nil {
				t.Fatalf("kind %d opcode %02x: handled=%v err=%v", tc.kind, op, handled, err)
			}
			want := []byte{255, 0}
			if op == 0x72 {
				want = []byte{0, 255}
			}
			if !bytes.Equal(g.pixels, want) {
				t.Fatalf("kind %d opcode %02x: pixels %v want %v", tc.kind, op, g.pixels, want)
			}
		}
	}
}

func TestScriptBitmapOverrideRejectsInvalidBeforeDrawing(t *testing.T) {
	for _, palette := range [][]int16{{}, {0x0300}} {
		fb, _ := backend.NewMemoryFramebuffer(2, 1)
		g, _ := newScriptGraphics(fb)
		g.pixels[0], g.pixels[1] = 17, 17
		vm := sgsvm.New(&sgsvm.Program{Constants: palette, Resources: []sgsvm.Resource{{Data: []byte{6, 2, 1, 0, 0, 0, 0, 0, 0, 0x10}}}}, nil)
		for _, a := range []int16{0, 0, 0, 0x4000} {
			vm.Push(a)
		}
		if _, err := g.call(0x71, vm); err == nil {
			t.Fatal("accepted invalid palette")
		}
		if !bytes.Equal(g.pixels, []byte{17, 17}) {
			t.Fatal("invalid palette changed pixels")
		}
	}
}

func TestScriptPaletteStackUnderflowHasNoSideEffects(t *testing.T) {
	for _, op := range []byte{0x6e, 0x71, 0x72} {
		fb, _ := backend.NewMemoryFramebuffer(1, 1)
		g, _ := newScriptGraphics(fb)
		g.pixels[0] = 17
		vm := sgsvm.New(&sgsvm.Program{Constants: []int16{2, 0, 3}, Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{0x5555}}}}, nil)
		vm.Push(0)
		if _, err := g.call(op, vm); err == nil {
			t.Fatalf("opcode %02x accepted stack underflow", op)
		}
		if g.pixels[0] != 17 || vm.Variables[0].Values[0] != 0x5555 {
			t.Fatalf("opcode %02x changed state on stack underflow", op)
		}
	}
}

func TestScriptEmptyBitmapDoesNotRequireRemainingHeader(t *testing.T) {
	for kind := byte(2); kind <= 8; kind++ {
		for _, data := range [][]byte{{kind, 0}, {kind, 3, 0}, {kind, 0, 255, 127, 128}} {
			for _, op := range []byte{0x6f, 0x70, 0x71, 0x72} {
				fb, _ := backend.NewMemoryFramebuffer(2, 1)
				g, _ := newScriptGraphics(fb)
				g.pixels[0], g.pixels[1] = 17, 17
				vm := sgsvm.New(&sgsvm.Program{Constants: []int16{0}, Resources: []sgsvm.Resource{{Data: data}}}, nil)
				for _, arg := range []int16{-32768, 32767, 0} {
					vm.Push(arg)
				}
				if op == 0x70 || op == 0x72 {
					vm.Push(1)
				}
				if op == 0x71 || op == 0x72 {
					vm.Push(0x4000)
				}
				if handled, err := g.call(op, vm); !handled || err != nil {
					t.Fatalf("opcode %02x data %x: handled=%v error=%v", op, data, handled, err)
				}
				if !bytes.Equal(g.pixels, []byte{17, 17}) {
					t.Fatalf("opcode %02x data %x changed pixels", op, data)
				}
				if _, count := fb.Snapshot(); count != 0 {
					t.Fatal("empty bitmap presented a frame")
				}
			}
		}
	}
}

func TestScriptShortBitmapRequiresKnownZeroDimension(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(1, 1)
	g, _ := newScriptGraphics(fb)
	for _, data := range [][]byte{{}, {5}, {5, 1}, {5, 1, 1}, {1, 0}, {9, 0}} {
		if err := g.bitmap(data, 0, 0); err == nil {
			t.Fatalf("accepted malformed nonempty bitmap %x", data)
		}
	}
}

func TestScriptPixelFrameFitsEventWorkBudget(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(128, 160)
	g, _ := newScriptGraphics(fb)
	// A compact authored callback visits every pixel, deriving coordinates
	// from a word counter and presenting after all 20,480 writes.
	code := []byte{0,
		4, 0, 6, 0, 128, 0x16, // x = counter % width
		4, 0, 6, 0, 128, 0x15, // y = counter / width
		5, 0, 0x58,
		4, 0, 0x0d, 0x0a, 0, // counter++
		4, 0, 6, 0x50, 0, 0x1e, 0x42, 0, 1,
		0x78, 0xff,
	}
	vm := sgsvm.New(&sgsvm.Program{Data: code, Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{0}}}}, &ScriptSession{graphics: g})
	if err := vm.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if got := vm.Value(0, 0); got != 128*160 {
		t.Fatalf("pixel count %d", got)
	}
	for i, p := range g.pixels {
		if p != 255 {
			t.Fatalf("pixel %d was not drawn", i)
		}
	}
	if _, count := fb.Snapshot(); count != 1 {
		t.Fatalf("flush count %d", count)
	}
}

func TestScriptPixelLoopStillStopsAtWorkLimit(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(1, 1)
	g, _ := newScriptGraphics(fb)
	code := []byte{0, 5, 0, 5, 0, 5, 0, 0x58, 0x41, 0, 1}
	vm := sgsvm.New(&sgsvm.Program{Data: code}, &ScriptSession{graphics: g})
	if err := vm.Run(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "work limit") {
		t.Fatalf("unbounded pixel callback: %v", err)
	}
}

func TestScriptGraphicsChargesLargeWorkBeforeDrawing(t *testing.T) {
	for _, tc := range []struct {
		op   byte
		args []int16
	}{
		{0x55, nil}, {0x57, []int16{0}},
		{0x5f, []int16{-32768, 0, 32767, 0}},
		{0x60, []int16{-32768, 32767, 0}},
		{0x61, []int16{0, -32768, 32767}},
		{0x62, []int16{-32768, -32768, 32767, 32767}},
		{0x63, []int16{-32768, -32768, 32767, 32767}},
		{0x64, []int16{0, 0, -32768, 0}},
		{0x65, []int16{0, 0, 32767, 32767}},
	} {
		fb, _ := backend.NewMemoryFramebuffer(128, 160)
		g, _ := newScriptGraphics(fb)
		g.pixels[0] = 17
		vm := sgsvm.New(&sgsvm.Program{}, nil)
		if err := vm.ChargeWork(sgsvm.MaxSteps - 2); err != nil {
			t.Fatal(err)
		}
		for _, arg := range tc.args {
			vm.Push(arg)
		}
		if _, err := g.call(tc.op, vm); err == nil {
			t.Fatalf("opcode %02x exceeded work budget", tc.op)
		}
		if g.pixels[0] != 17 {
			t.Fatalf("opcode %02x partially drew after exhaustion", tc.op)
		}
	}
}

func TestScriptBytePaletteUsesBoundedRendererScratch(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(3, 1)
	g, _ := newScriptGraphics(fb)
	g.pixels[0], g.pixels[1], g.pixels[2] = 17, 17, 17
	// A guest can patch a byte palette beyond its 182 ordinary colors. The
	// original lookup aliases adjacent queue state, initially all zero.
	if err := g.bitmap([]byte{6, 3, 1, 0, 0, 182, 198, 255, 4, 0x18}, 0, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g.pixels, []byte{0, 0, 0}) {
		t.Fatalf("initial scratch colors %v", g.pixels)
	}
	// Scratch bytes are already framebuffer indices and must not be remapped
	// by a palette bank. This also distinguishes an alias from a color clamp.
	g.bitmapQueueScratch[198-182] = 0xe0
	g.bank = 6
	if err := g.bitmap([]byte{5, 2, 1, 0, 0, 198, 4, 0x40}, 0, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g.pixels, []byte{0xe0, 0, 0}) {
		t.Fatalf("scratch alias or transparency changed: %v", g.pixels)
	}
	if err := g.bitmap([]byte{8, 1, 1, 0, 0, 198}, 0, 0); err == nil {
		t.Fatal("direct-color bitmap incorrectly used byte-palette scratch")
	}
	if _, err := g.mappedColor(198); err == nil {
		t.Fatal("ordinary drawing color incorrectly used bitmap scratch")
	}
}

func callScriptQueue(g *scriptGraphics, vm *sgsvm.VM, op byte, args ...int16) error {
	for _, arg := range args {
		vm.Push(arg)
	}
	_, err := g.call(op, vm)
	return err
}

func TestScriptBitmapQueueReservedResultAndCapacity(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(2, 1)
	g, _ := newScriptGraphics(fb)
	// Reserve a return slot, save its depth, call with four semantic arguments,
	// then restore the stack to expose the value written into the reserved slot.
	code := []byte{0, 5, 0, 0x0b, 5, 0, 5, 0, 5, 0, 5, 0, 0x74, 0x0c, 0x0a, 0, 0xff}
	vm := sgsvm.New(&sgsvm.Program{Data: code, Variables: []sgsvm.Variable{{Mutable: true, Values: []int16{0}}}, Resources: []sgsvm.Resource{{Data: []byte{8, 1, 1, 0, 0, 0}}}}, &ScriptSession{graphics: g})
	for i := 0; i < 21; i++ {
		if err := vm.Run(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		want := int16(1)
		if i == 20 {
			want = 0
		}
		if vm.Value(0, 0) != want {
			t.Fatalf("enqueue %d result %d", i, vm.Value(0, 0))
		}
	}
	if len(g.bitmapQueue) != 20 || g.bitmapQueueScratch[0] != 20 {
		t.Fatal("queue capacity exceeded")
	}
	if g.pixels[0] != 0 {
		t.Fatal("enqueue drew pixels")
	}
	if err := callScriptQueue(g, vm, 0x73); err != nil {
		t.Fatal(err)
	}
	if len(g.bitmapQueue) != 0 || g.bitmapQueueScratch[0] != 0 {
		t.Fatal("queue reset failed")
	}
	if !g.bitmapQueueOpaque[6] {
		t.Fatal("reset incorrectly erased retained queue records")
	}
}

func TestScriptBitmapQueueStableOrderingAndRetention(t *testing.T) {
	for _, tc := range []struct {
		mode int16
		want byte
	}{{0, 0}, {1, 255}, {2, 255}, {3, 0}, {4, 0}, {-1, 0}} {
		fb, _ := backend.NewMemoryFramebuffer(3, 3)
		g, _ := newScriptGraphics(fb)
		white := []byte{8, 2, 2, 0, 0, 0, 0, 0, 0}
		black := []byte{8, 2, 2, 0, 0, 3, 3, 3, 3}
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: white}, {Data: black}}}, nil)
		if err := callScriptQueue(g, vm, 0x74, 0, 0, 1, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := callScriptQueue(g, vm, 0x74, 0, 1, 0, 1, 0); err != nil {
			t.Fatal(err)
		}
		scratch := g.bitmapQueueScratch
		for repeat := 0; repeat < 2; repeat++ {
			if err := callScriptQueue(g, vm, 0x75, tc.mode); err != nil {
				t.Fatal(err)
			}
			if g.pixels[4] != tc.want {
				t.Fatalf("mode %d overlap=%d want%d", tc.mode, g.pixels[4], tc.want)
			}
			if len(g.bitmapQueue) != 2 || g.bitmapQueueScratch != scratch {
				t.Fatal("draw reordered or cleared retained queue state")
			}
		}
		if _, count := fb.Snapshot(); count != 0 {
			t.Fatal("queued drawing presented without flush")
		}
	}
	// Equal sort coordinates retain insertion order.
	fb, _ := backend.NewMemoryFramebuffer(1, 1)
	g, _ := newScriptGraphics(fb)
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: []byte{8, 1, 1, 0, 0, 0}}, {Data: []byte{8, 1, 1, 0, 0, 3}}}}, nil)
	callScriptQueue(g, vm, 0x74, 0, 0, 0, 0, 0)
	callScriptQueue(g, vm, 0x74, 0, 0, 0, 1, 0)
	if err := callScriptQueue(g, vm, 0x75, 1); err != nil || g.pixels[0] != 0 {
		t.Fatalf("unstable equal-coordinate sort: %v", err)
	}
}

func TestScriptBitmapQueueMirrorAndScratchCoordinates(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(2, 1)
	g, _ := newScriptGraphics(fb)
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: []byte{8, 2, 1, 0, 0, 0, 3}}}}, nil)
	if err := callScriptQueue(g, vm, 0x74, 0, 2, 0, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := callScriptQueue(g, vm, 0x75, 4); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g.pixels, []byte{0, 255}) {
		t.Fatalf("mirrored queue pixels %v", g.pixels)
	}
	callScriptQueue(g, vm, 0x73)
	if err := callScriptQueue(g, vm, 0x74, 0, 0x1234, -2, 0, -1); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g.bitmapQueueScratch[10:22], []byte{0x34, 0x12, 0, 0, 0xfe, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("scratch coordinates %x", g.bitmapQueueScratch[10:22])
	}
	if err := g.bitmap([]byte{5, 1, 1, 0, 0, 198, 4, 0}, 0, 0); err != nil || g.pixels[0] != 255 {
		t.Fatalf("signed coordinate alias: %v", err)
	}
	before := append([]byte(nil), g.pixels...)
	if err := g.bitmap([]byte{5, 1, 1, 0, 0, 188, 4, 0}, 0, 0); err == nil || !strings.Contains(err.Error(), "queue pointer") {
		t.Fatalf("native pointer alias accepted: %v", err)
	}
	if !bytes.Equal(before, g.pixels) {
		t.Fatal("unknown pointer alias partially drew")
	}
}

func TestScriptBitmapQueueInvalidCallsAreAtomic(t *testing.T) {
	for _, args := range [][]int16{{0, 0, 0, 0}, {0, 0, 0, -1, 0}, {0, 0, 0, 1, 0}} {
		fb, _ := backend.NewMemoryFramebuffer(1, 1)
		g, _ := newScriptGraphics(fb)
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: []byte{8, 1, 1, 0, 0, 0}}}}, nil)
		if err := callScriptQueue(g, vm, 0x74, args...); err == nil {
			t.Fatalf("accepted invalid enqueue %v", args)
		}
		if len(g.bitmapQueue) != 0 || g.bitmapQueueScratch[0] != 0 {
			t.Fatal("invalid enqueue changed queue")
		}
	}
	for _, budget := range []bool{false, true} {
		fb, _ := backend.NewMemoryFramebuffer(1, 1)
		g, _ := newScriptGraphics(fb)
		g.pixels[0] = 17
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: []byte{8, 1, 1, 0, 0, 0}}, {Data: []byte{8, 1}}}}, nil)
		callScriptQueue(g, vm, 0x74, 0, 0, 0, 0, 0)
		callScriptQueue(g, vm, 0x74, 0, 0, 0, 1, 0)
		if budget {
			vm.ChargeWork(sgsvm.MaxSteps - 3)
		}
		if err := callScriptQueue(g, vm, 0x75, 4); err == nil {
			t.Fatal("invalid batch accepted")
		}
		if g.pixels[0] != 17 {
			t.Fatal("invalid batch partially drew")
		}
	}
}

func TestScriptBitmapQueueRetainsOwnedStorage(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(1, 1)
	g, _ := newScriptGraphics(fb)
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: []byte{8, 1, 1, 0, 0, 0}}}}, nil)
	if err := callScriptQueue(g, vm, 0x74, 0, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	vm.Resources[0].Data[5] = 3
	g.pixels[0] = 255
	if err := callScriptQueue(g, vm, 0x75, 4); err != nil || g.pixels[0] != 0 {
		t.Fatalf("queued data missed in-place edit: %v", err)
	}
	vm.Resources[0].Data = []byte{8, 1, 1, 0, 0, 0}
	g.pixels[0] = 255
	if err := callScriptQueue(g, vm, 0x75, 4); err != nil || g.pixels[0] != 0 {
		t.Fatalf("resource replacement invalidated retained queue storage: %v", err)
	}
}
