package ktf

import (
	"context"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/curve"
)

// An arc's box comes from the guest. One as wide as an int used to be walked
// whole, every span clipped away one at a time, and the call never returned.
func TestWIPICArcsWithBoxesFarLargerThanTheSurfaceFinish(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.currentThread, runtime.currentContext = client.thread, context.Background()
	handle, _, contextAddress := arcFixture(t, runtime, 0xffff)

	for _, function := range []uint32{15, 16} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			callArcFunction(t, runtime, function, handle, 0, 0, 0x7fffffff, 0x7fffffff, 0, 360, contextAddress)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("graphics function %d still running after 5s", function)
		}
	}
}

// Walking only the reachable part must draw exactly what the whole walk drew,
// with and without a clip, for arcs inside, across and beyond the surface.
func TestWIPICArcsDrawWhatTheWholeWalkDrew(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.currentThread, runtime.currentContext = client.thread, context.Background()
	_, buffer, _ := arcFixture(t, runtime, 0)

	clips := []wipicClip{{}, {left: 2, top: 1, right: 7, bottom: 6, enabled: true}}
	boxes := [][4]int32{{0, 0, 8, 8}, {-3, -2, 12, 9}, {5, 5, 9, 9}, {-20, 3, 40, 3}, {2, -6, 3, 20}}
	angles := [][2]int32{{0, 360}, {30, 100}, {-45, 270}, {200, -90}}
	for _, clip := range clips {
		for _, box := range boxes {
			for _, angle := range angles {
				for _, fill := range []bool{false, true} {
					want := arcPixels(t, runtime, buffer, func(emit curve.Emit) error {
						if fill {
							return curve.FillArc(box[0], box[1], box[2], box[3], angle[0], angle[1], emit)
						}
						return curve.DrawArc(box[0], box[1], box[2], box[3], angle[0], angle[1], emit)
					}, clip)
					got := arcPixels(t, runtime, buffer, func(emit curve.Emit) error {
						if fill {
							return curve.FillArcWithin(buffer.curveBounds(clip), box[0], box[1], box[2], box[3], angle[0], angle[1], emit)
						}
						return curve.DrawArcWithin(buffer.curveBounds(clip), box[0], box[1], box[2], box[3], angle[0], angle[1], emit)
					}, clip)
					if got != want {
						t.Fatalf("clip %+v box %v angles %v fill %v: bounded walk drew\n%s\nwant\n%s", clip, box, angle, fill, got, want)
					}
				}
			}
		}
	}
}

// arcPixels clears the surface, draws through the framebuffer's own clipped
// fill, and answers the result as text.
func arcPixels(t *testing.T, runtime *initializationRuntime, buffer wipicFramebuffer, draw func(curve.Emit) error, clip wipicClip) string {
	t.Helper()
	if err := runtime.fillFramebufferRect(buffer, wipicClip{}, wipicPixelOp{}, 0, 0, 0, int32(buffer.width), int32(buffer.height)); err != nil {
		t.Fatal(err)
	}
	if err := draw(runtime.framebufferCurveEmit(buffer, clip, wipicPixelOp{}, 1)); err != nil {
		t.Fatal(err)
	}
	text := make([]byte, 0, (buffer.width+1)*buffer.height)
	for y := uint32(0); y < uint32(buffer.height); y++ {
		for x := uint32(0); x < uint32(buffer.width); x++ {
			if arcPixel(t, runtime, buffer, x, y) != 0 {
				text = append(text, '#')
			} else {
				text = append(text, '.')
			}
		}
		text = append(text, '\n')
	}
	return string(text)
}
