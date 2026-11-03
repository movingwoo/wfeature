package skt

import (
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// A GameCanvas's inherited paint blits its buffer at the Graphics origin and
// under the lock the destination's other drawing calls take.
func TestGameCanvasPaintBlitsAtTheOriginUnderTheDestinationLock(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	invokeFixtureVoid(t, runtime, "UIMIDlet", "drawBuffer")
	canvas := fixtureStatic(t, runtime, "buffer", "LUIMIDlet$Buffer;")

	target := graphicsTestContext(40, 30)
	var screen sync.Mutex
	target.screen = &screen
	target.translateX, target.translateY = 5, 3
	screen.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := runtime.gameCanvasDrawBuffer(runtime.VM, []jvm.Value{jvm.ReferenceValue(canvas), jvm.ReferenceValue(graphicsObject(target))})
		done <- err
	}()
	select {
	case <-done:
		screen.Unlock()
		t.Fatal("drawBuffer wrote while the destination's lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	screen.Unlock()
	if err := <-done; err != nil {
		t.Fatalf("drawBuffer() error = %v", err)
	}
	pixelAt := func(x, y int) []byte { index := (y*40 + x) * 4; return target.pixels[index : index+4] }
	if got := pixelAt(5, 3); got[0] != 0xff || got[1] != 0 || got[2] != 0 {
		t.Fatalf("pixel at the origin = %v, want the buffer's red", got)
	}
	for _, point := range [][2]int{{0, 0}, {4, 3}, {5, 2}} {
		if got := pixelAt(point[0], point[1]); got[0] != 0 || got[3] != 0 {
			t.Fatalf("pixel %v outside the translated buffer = %v, want it untouched", point, got)
		}
	}
}

// getKeyStates reports a key pressed since the previous call even when it was
// released before the title polled, and only once.
func TestGameCanvasReportsATapBetweenTwoPolls(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	if err := runtime.SendKey(KeyPressed, KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SendKey(KeyReleased, KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	if states := invokeFixtureInt(t, runtime, "UIMIDlet", "bufferKeyStates"); states != 1<<8 {
		t.Fatalf("bufferKeyStates() after a tap = %d, want FIRE_PRESSED", states)
	}
	if states := invokeFixtureInt(t, runtime, "UIMIDlet", "bufferKeyStates"); states != 0 {
		t.Fatalf("bufferKeyStates() on the next poll = %d, want 0", states)
	}
}

// The latch travels in the checkpoint's KeyStates beside the held bits, and a
// checkpoint written before there was a latch restores with none.
func TestGameCanvasKeyLatchCheckpointForm(t *testing.T) {
	held, latched := int32(1<<8|1<<1), int32(1<<12|1<<6)
	gotHeld, gotLatched := splitGameCanvasKeyRecord(gameCanvasKeyRecord(held, latched))
	if gotHeld != held || gotLatched != latched {
		t.Fatalf("round trip = %#x, %#x, want %#x, %#x", gotHeld, gotLatched, held, latched)
	}
	gotHeld, gotLatched = splitGameCanvasKeyRecord(1 << 8)
	if gotHeld != 1<<8 || gotLatched != 0 {
		t.Fatalf("older record = %#x, %#x, want the held bits and no latch", gotHeld, gotLatched)
	}
}
