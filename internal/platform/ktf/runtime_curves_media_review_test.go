package ktf

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/curve"
)

func screenBytes(t *testing.T, runtime *initializationRuntime, state *runtimeGraphicsState) []byte {
	t.Helper()
	data := make([]byte, int(state.target.Height*state.target.Bpl))
	if err := runtime.client.Core().Memory().Read(state.target.Pixels, data); err != nil {
		t.Fatal(err)
	}
	return data
}

func restoreScreen(t *testing.T, runtime *initializationRuntime, state *runtimeGraphicsState, data []byte) {
	t.Helper()
	if err := runtime.client.Core().Memory().Write(state.target.Pixels, data); err != nil {
		t.Fatal(err)
	}
}

// A guest box far larger than any surface used to be walked whole before the
// clip threw it away; one as wide as an int never finished. Each binding now
// walks only the part of the box the clip can reach.
func TestCurveBindingsReturnForBoxesFarLargerThanTheScreen(t *testing.T) {
	_, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	state := graphics.Native.(*runtimeGraphicsState)
	calls := []struct {
		name string
		call func() error
	}{
		{"fillArc as wide as an int", func() error { return runtime.graphicsFillArc(state, 0, 0, math.MaxInt32, 1, 0, 360) }},
		{"fillArc of ten billion pixels", func() error {
			return runtime.graphicsFillArc(state, -50000, -50000, 100000, 100000, 0, 360)
		}},
		{"drawArc as large as an int", func() error {
			return runtime.graphicsDrawArc(state, 0, 0, math.MaxInt32, math.MaxInt32, 0, 360)
		}},
		{"fillRoundRect as tall as an int", func() error {
			return runtime.graphicsFillRoundRect(state, 0, 0, 100, math.MaxInt32, 20, 20)
		}},
		{"drawRoundRect as tall as an int", func() error {
			return runtime.graphicsDrawRoundRect(state, 0, 0, 100, math.MaxInt32, 20, 20)
		}},
	}
	for _, current := range calls {
		done := make(chan error, 1)
		go func() { done <- current.call() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", current.name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s did not return", current.name)
		}
	}
}

// What lands on the screen is what the unbounded walk drew, under a
// translation, a clip, shapes that cross the screen's edge, and XOR, which
// would show a pixel drawn a different number of times.
func TestCurveBindingsDrawWhatTheUnboundedWalkDrew(t *testing.T) {
	_, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	state := graphics.Native.(*runtimeGraphicsState)
	emit := runtime.curveEmit(state)
	type shape struct{ x, y, width, height, a, b int32 }
	shapes := []shape{
		{20, 30, 80, 60, 30, 250},
		{5, 5, 37, 91, -45, -300},
		{-30, -20, 150, 400, 0, 360},
		{100, 150, 500, 90, 200, 120},
		{-400, 40, 900, 33, 10, 360},
		{60, -700, 40, 1500, 90, 180},
	}
	for _, xor := range []bool{false, true} {
		for _, clip := range [][4]int32{{0, 0, 1 << 20, 1 << 20}, {10, 12, 100, 90}, {-5, 200, 300, 50}} {
			state.xorMode = xor
			state.translateX, state.translateY = 7, -5
			state.clipX, state.clipY, state.clipWidth, state.clipHeight = clip[0], clip[1], clip[2], clip[3]
			state.color = 0x5aa5
			for _, current := range shapes {
				for _, pair := range []struct {
					name      string
					binding   func() error
					unbounded func() error
				}{
					{"fillArc",
						func() error {
							return runtime.graphicsFillArc(state, current.x, current.y, current.width, current.height, current.a, current.b)
						},
						func() error {
							return curve.FillArc(current.x, current.y, current.width, current.height, current.a, current.b, emit)
						}},
					{"drawArc",
						func() error {
							return runtime.graphicsDrawArc(state, current.x, current.y, current.width, current.height, current.a, current.b)
						},
						func() error {
							return curve.DrawArc(current.x, current.y, current.width, current.height, current.a, current.b, emit)
						}},
					// The round-rectangle bindings draw collapsed corners as
					// rectangles without walking a curve at all.
					{"fillRoundRect",
						func() error {
							return runtime.graphicsFillRoundRect(state, current.x, current.y, current.width, current.height, current.a, current.b)
						},
						func() error {
							if radiusX, radiusY := curve.RoundRectRadii(current.width, current.height, current.a, current.b); radiusX == 0 || radiusY == 0 {
								return runtime.graphicsFillRect(state, current.x, current.y, current.width, current.height, state.color)
							}
							return curve.FillRoundRect(current.x, current.y, current.width, current.height, current.a, current.b, emit)
						}},
					{"drawRoundRect",
						func() error {
							return runtime.graphicsDrawRoundRect(state, current.x, current.y, current.width, current.height, current.a, current.b)
						},
						func() error {
							if radiusX, radiusY := curve.RoundRectRadii(current.width, current.height, current.a, current.b); radiusX == 0 || radiusY == 0 {
								return runtime.graphicsDrawRectOutline(state, current.x, current.y, current.width, current.height)
							}
							return curve.DrawRoundRect(current.x, current.y, current.width, current.height, current.a, current.b, emit)
						}},
				} {
					before := screenBytes(t, runtime, state)
					if err := pair.unbounded(); err != nil {
						t.Fatal(err)
					}
					want := screenBytes(t, runtime, state)
					restoreScreen(t, runtime, state, before)
					if err := pair.binding(); err != nil {
						t.Fatal(err)
					}
					if got := screenBytes(t, runtime, state); !bytes.Equal(got, want) {
						t.Fatalf("%s %+v under clip %v (xor %v) drew differently from the unbounded walk", pair.name, current, clip, xor)
					}
				}
			}
		}
	}
}
