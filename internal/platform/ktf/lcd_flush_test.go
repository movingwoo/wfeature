package ktf

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

func TestPartialLCDFlushRetainsOutsideAndDoesNotMutateSource(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	screen := graphics.Native.(*runtimeGraphicsState).target
	if err := runtime.FillFramebufferRect(screen, wipicrt.WIPICClip{}, wipicrt.WIPICPixelOp{}, 0xf800, 0, 0, 4, 4); err != nil {
		t.Fatal(err)
	}
	if err := runtime.PresentScreen(); err != nil {
		t.Fatal(err)
	}
	before, width, _, _ := client.Frame()
	if err := runtime.FillFramebufferRect(screen, wipicrt.WIPICClip{}, wipicrt.WIPICPixelOp{}, 0x07e0, 0, 0, 4, 4); err != nil {
		t.Fatal(err)
	}
	source := screen
	if err := runtime.FlushLCDRegion(source, 1, 1, 2, 2); err != nil {
		t.Fatal(err)
	}
	after, _, _, _ := client.Frame()
	if !bytes.Equal(before[:4], after[:4]) {
		t.Fatal("outside LCD pixel overwritten")
	}
	i := (width + 1) * 4
	if !bytes.Equal(after[i:i+4], []byte{0, 252, 0, 255}) {
		t.Fatalf("flushed pixel = %v", after[i:i+4])
	}
	if got := readFramebufferPixel(t, runtime, screen, 0, 0); got != 0x07e0 {
		t.Fatalf("source mutated: %#x", got)
	}
	if err := client.vm.RegisterNative("test/EmptyCard", "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = []*jvm.Object{{ClassName: "test/EmptyCard"}}
	for i := 0; i < 3; i++ {
		runtime.RepaintPending = true
		if _, err := runtime.paintTopCard(); err != nil {
			t.Fatal(err)
		}
		got, _, _, _ := client.Frame()
		if !bytes.Equal(got, after) {
			t.Fatal("empty paint replaced retained LCD pixels")
		}
	}
}

// A title that draws from a timer, flushes the whole screen itself for a
// blocking screen and has a paint that draws nothing still gets the frame it
// asks for with MC_grpRepaint. A paint nobody in the title asked for keeps the
// flush, and so does every paint after a whole-screen flush of another
// framebuffer, which shows what the screen does not hold.
func TestWholeScreenFlushPresentsTheRepaintTheTitleAsksFor(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	screen := graphics.Native.(*runtimeGraphicsState).target
	width, height := int32(screen.Width), int32(screen.Height)
	fill := func(target wipicrt.WIPICFramebuffer, pixel uint32) {
		t.Helper()
		if err := runtime.FillFramebufferRect(target, wipicrt.WIPICClip{}, wipicrt.WIPICPixelOp{}, pixel, 0, 0, width, height); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.vm.RegisterNative("test/EmptyCard", "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = []*jvm.Object{{ClassName: "test/EmptyCard"}}
	paint := func(asked bool) []byte {
		t.Helper()
		runtime.RepaintPending = true
		if asked {
			runtime.Client.TitleRepaint = true
		}
		if _, err := runtime.paintTopCard(); err != nil {
			t.Fatal(err)
		}
		frame, _, _, _ := client.Frame()
		return frame[:4]
	}
	red, green, blue := []byte{248, 0, 0, 255}, []byte{0, 252, 0, 255}, []byte{0, 0, 248, 255}

	fill(screen, 0xf800)
	if err := runtime.FlushLCDRegion(screen, 0, 0, width, height); err != nil {
		t.Fatal(err)
	}
	fill(screen, 0x07e0)
	if got := paint(false); !bytes.Equal(got, red) {
		t.Fatalf("a paint the title did not ask for replaced its flush with %v", got)
	}
	if got := paint(true); !bytes.Equal(got, green) {
		t.Fatalf("the repaint the title asked for shows %v, want %v", got, green)
	}

	handle, err := runtime.NewWIPICFramebufferRecord(uint32(width), uint32(height))
	if err != nil {
		t.Fatal(err)
	}
	other, err := runtime.ReadWIPICFramebuffer(handle)
	if err != nil {
		t.Fatal(err)
	}
	fill(other, 0x001f)
	if err := runtime.FlushLCDRegion(other, 0, 0, width, height); err != nil {
		t.Fatal(err)
	}
	if got := paint(true); !bytes.Equal(got, blue) {
		t.Fatalf("a paint replaced another framebuffer's flush with %v", got)
	}
}

func TestNULPaddingHasNoAdvance(t *testing.T) {
	_, runtime := newTestRuntime(t)
	if got, want := runtime.GraphicsTextWidth([]rune("A\x00B\x00")), runtime.GraphicsTextWidth([]rune("AB")); got != want {
		t.Fatalf("padded width = %d, want %d", got, want)
	}
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	state := graphics.Native.(*runtimeGraphicsState)
	var frames [][]byte
	for _, text := range []string{"AB", "A\x00B\x00"} {
		if err := runtime.FillFramebufferRect(state.target, wipicrt.WIPICClip{}, wipicrt.WIPICPixelOp{}, 0xffff, 0, 0, int32(state.target.Width), int32(state.target.Height)); err != nil {
			t.Fatal(err)
		}
		if err := runtime.graphicsDrawText(state, []rune(text), 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		frame := make([]byte, int(state.target.Bpl)*int(state.target.Height))
		if err := runtime.client.Core().Memory().Read(state.target.Pixels, frame); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
	if !bytes.Equal(frames[0], frames[1]) {
		t.Fatal("NUL padding changes drawn pixels")
	}
}
