package lgt

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/curve"
)

type boundedWalk = func(curve.Bounds, int32, int32, int32, int32, int32, int32, curve.Emit) error
type wholeWalk = func(int32, int32, int32, int32, int32, int32, curve.Emit) error

func finishesWithin(t *testing.T, what string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		call()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s still running after 5s", what)
	}
}

// A curve's box comes from the guest. One as large as an int used to be
// walked whole, every span clipped away one at a time, and the call never
// returned.
func TestCurvesWithBoxesFarLargerThanTheSurfaceReturn(t *testing.T) {
	_, client, object := javaCurveFixture(t, 20, 20)
	for index, walk := range []boundedWalk{curve.FillRoundRectWithin, curve.DrawRoundRectWithin, curve.FillArcWithin, curve.DrawArcWithin} {
		call := javaCurve(walk)
		finishesWithin(t, "a Java curve", func() {
			if _, err := call(client, nil, nil, []uint32{object, 0, 0, 0x7fffffff, 0x7fffffff, 360, 360}); err != nil {
				t.Errorf("curve %d: %v", index, err)
			}
		})
	}
	drawContext, pointer := polygonFixtureContext(t, client, 0x07e0)
	for _, slot := range []uint32{slotDrawArc, slotFillArc} {
		finishesWithin(t, "a WIPI-C arc", func() {
			callDrawSlot(t, client, slot, drawContext.target.handle, 0, 0, 0x7fffffff, 0x7fffffff, 0, 360, pointer)
		})
	}
}

// Walking only what the fill can reach must draw exactly what the whole walk
// drew, under a clip and a translation, for shapes inside, across and beyond
// the surface.
func TestBoundedCurvesDrawWhatTheWholeWalkDrew(t *testing.T) {
	target, client, object := javaCurveFixture(t, 24, 20)
	if _, err := javaTranslate(client, nil, nil, []uint32{object, 3, uint32(0xfffffffe)}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaSetClip(client, nil, nil, []uint32{object, 1, 4, 17, 12}); err != nil {
		t.Fatal(err)
	}
	pairs := []struct {
		bounded boundedWalk
		whole   wholeWalk
	}{
		{curve.FillRoundRectWithin, curve.FillRoundRect}, {curve.DrawRoundRectWithin, curve.DrawRoundRect},
		{curve.FillArcWithin, curve.FillArc}, {curve.DrawArcWithin, curve.DrawArc},
	}
	boxes := [][6]int32{{0, 0, 20, 16, 30, 120}, {-9, -7, 40, 33, 200, -90}, {12, 10, 30, 30, 8, 8}, {-50, 5, 90, 4, 0, 360}}
	for index, pair := range pairs {
		for _, box := range boxes {
			arguments := []uint32{object}
			for _, value := range box {
				arguments = append(arguments, uint32(value))
			}
			clear(target.pixels)
			if _, err := javaCurve(pair.bounded)(client, nil, nil, arguments); err != nil {
				t.Fatal(err)
			}
			got := slices.Clone(target.pixels)
			clear(target.pixels)
			if _, err := wholeJavaCurve(pair.whole)(client, nil, nil, arguments); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, target.pixels) {
				t.Fatalf("curve %d box %v: the bounded walk drew different pixels", index, box)
			}
		}
	}
}

// wholeJavaCurve is javaCurve as it was before its walk was bounded.
func wholeJavaCurve(walk wholeWalk) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		return 0, client.javaDraw(arguments[0], func(drawContext *graphicsContext, state *javaGraphics) error {
			emit := func(span curve.Span) error {
				drawContext.fill(int(span.X), int(span.Y), int(span.Width), 1, state.color)
				return nil
			}
			return walk(
				int32(arguments[1])+int32(state.translateX), int32(arguments[2])+int32(state.translateY),
				int32(arguments[3]), int32(arguments[4]), int32(arguments[5]), int32(arguments[6]), emit)
		})
	}
}
