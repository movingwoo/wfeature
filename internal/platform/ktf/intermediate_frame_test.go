package ktf

import (
	"bytes"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

func TestIntermediateFramesLeaveTheCallbackBeforeItReturns(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	screen := graphics.Native.(*runtimeGraphicsState).target
	updates := make(chan backend.FrameUpdate, 1)
	now := time.Unix(1700000000, 0)
	client.frameSampleClock = func() time.Time { return now }
	client.SetFrameSink(backend.FrameSink{Output: updates})
	// A guest callback owns this lock until return. Frame() cannot provide
	// an intermediate image on that path, but the independent queue can.
	client.run.Lock()
	defer client.run.Unlock()
	for _, color := range []uint16{0xf800, 0x07e0} {
		if err := runtime.fillFramebufferRect(screen, wipicClip{}, wipicPixelOp{}, uint32(color), 0, 0, 1, 1); err != nil {
			t.Fatal(err)
		}
		if err := runtime.presentScreen(); err != nil {
			t.Fatal(err)
		}
		select {
		case frame := <-updates:
			if frame.Width != int(screen.width) || frame.Height != int(screen.height) {
				t.Fatal("incorrect frame dimensions")
			}
			if color == 0xf800 && !bytes.Equal(frame.RGBA[:4], []byte{248, 0, 0, 255}) {
				t.Fatal("first loading image missing")
			}
			if color == 0x07e0 && !bytes.Equal(frame.RGBA[:4], []byte{0, 252, 0, 255}) {
				t.Fatal("second loading image missing")
			}
			frame.RGBA[0] = 99
			if client.frame[0] == 99 || client.lastSample[0] == 99 {
				t.Fatal("Host frame aliases runtime storage")
			}
		default:
			t.Fatal("frame withheld until callback return")
		}
		now = now.Add(20 * time.Millisecond)
	}
	if err := runtime.presentScreen(); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 {
		t.Fatal("unchanged frame transmitted")
	}
}

func TestIntermediateFrameRateAndSlowConsumerAreBounded(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	screen := graphics.Native.(*runtimeGraphicsState).target
	updates := make(chan backend.FrameUpdate, 1)
	now := time.Unix(1700000000, 0)
	client.frameSampleClock = func() time.Time { return now }
	client.SetFrameSink(backend.FrameSink{Output: updates})
	count := 0
	for i := 0; i < 1000; i++ {
		if err := runtime.fillFramebufferRect(screen, wipicClip{}, wipicPixelOp{}, uint32(i+1), 0, 0, 1, 1); err != nil {
			t.Fatal(err)
		}
		if err := runtime.presentScreen(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-updates:
			count++
		default:
		}
		now = now.Add(time.Millisecond)
	}
	if count < 50 || count > 60 {
		t.Fatalf("sampled %d frames per second", count)
	}
	for i := 0; i < 100; i++ {
		now = now.Add(20 * time.Millisecond)
		if err := runtime.fillFramebufferRect(screen, wipicClip{}, wipicPixelOp{}, uint32(i+1), 0, 0, 1, 1); err != nil {
			t.Fatal(err)
		}
		if err := runtime.presentScreen(); err != nil {
			t.Fatal(err)
		}
	}
	if len(updates) != 1 {
		t.Fatal("slow consumer did not retain one bounded frame")
	}
	client.SetFrameSink(backend.FrameSink{})
	close(updates)
	if err := runtime.presentScreen(); err != nil {
		t.Fatal(err)
	}
	replacement := make(chan backend.FrameUpdate, 1)
	client.SetFrameSink(backend.FrameSink{Output: replacement, Scale: 3})
	if err := runtime.presentScreen(); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-replacement:
		if frame.Scale != 3 {
			t.Fatal("replacement sink lost its Host scale")
		}
	default:
		t.Fatal("replacement sink did not receive the retained frame")
	}
}
