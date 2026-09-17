package ktf

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestPartialLCDFlushRetainsOutsideAndDoesNotMutateSource(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	screen := graphics.Native.(*runtimeGraphicsState).target
	if err := runtime.fillFramebufferRect(screen, wipicClip{}, wipicPixelOp{}, 0xf800, 0, 0, 4, 4); err != nil {
		t.Fatal(err)
	}
	if err := runtime.presentScreen(); err != nil {
		t.Fatal(err)
	}
	before, width, _, _ := client.Frame()
	if err := runtime.fillFramebufferRect(screen, wipicClip{}, wipicPixelOp{}, 0x07e0, 0, 0, 4, 4); err != nil {
		t.Fatal(err)
	}
	source := screen
	if err := runtime.flushLCDRegion(source, 1, 1, 2, 2); err != nil {
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
		runtime.repaintPending = true
		if _, err := runtime.paintTopCard(); err != nil {
			t.Fatal(err)
		}
		got, _, _, _ := client.Frame()
		if !bytes.Equal(got, after) {
			t.Fatal("empty paint replaced retained LCD pixels")
		}
	}
}

func TestNULPaddingHasNoAdvance(t *testing.T) {
	_, runtime := newTestRuntime(t)
	if got, want := runtime.graphicsTextWidth([]rune("A\x00B\x00")), runtime.graphicsTextWidth([]rune("AB")); got != want {
		t.Fatalf("padded width = %d, want %d", got, want)
	}
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	state := graphics.Native.(*runtimeGraphicsState)
	var frames [][]byte
	for _, text := range []string{"AB", "A\x00B\x00"} {
		if err := runtime.fillFramebufferRect(state.target, wipicClip{}, wipicPixelOp{}, 0xffff, 0, 0, int32(state.target.width), int32(state.target.height)); err != nil {
			t.Fatal(err)
		}
		if err := runtime.graphicsDrawText(state, []rune(text), 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		frame := make([]byte, int(state.target.bpl)*int(state.target.height))
		if err := runtime.client.core.Memory().Read(state.target.pixels, frame); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
	if !bytes.Equal(frames[0], frames[1]) {
		t.Fatal("NUL padding changes drawn pixels")
	}
}
