package skt

import (
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/curve"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type boundedCurveWalk = func(curve.Bounds, int32, int32, int32, int32, int32, int32, curve.Emit) error
type wholeCurveWalk = func(int32, int32, int32, int32, int32, int32, curve.Emit) error

// A curve's box comes from the guest. One as large as an int used to be
// walked whole, every span clipped away one at a time, and the call never
// returned.
func TestCurvesWithBoxesFarLargerThanTheSurfaceReturn(t *testing.T) {
	runtime := &Runtime{}
	_, receiver := curveGraphics(20, 20)
	for index, walk := range []boundedCurveWalk{curve.FillRoundRectWithin, curve.DrawRoundRectWithin, curve.FillArcWithin, curve.DrawArcWithin} {
		call := runtime.graphicsCurve(walk)
		done := make(chan error, 1)
		go func() {
			_, err := call(nil, []jvm.Value{receiver, jvm.IntValue(0), jvm.IntValue(0),
				jvm.IntValue(0x7fffffff), jvm.IntValue(0x7fffffff), jvm.IntValue(360), jvm.IntValue(360)})
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("curve %d: %v", index, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("curve %d still running after 5s", index)
		}
	}
}

// Walking only what the fill can reach must draw exactly what the whole walk
// drew, under a clip and a translation, for shapes inside, across and beyond
// the surface.
func TestBoundedCurvesDrawWhatTheWholeWalkDrew(t *testing.T) {
	runtime := &Runtime{}
	context, receiver := curveGraphics(24, 20)
	context.translateX, context.translateY = 3, -2
	context.clip = paintRect{minX: 1, minY: 4, maxX: 18, maxY: 16}
	pairs := []struct {
		bounded boundedCurveWalk
		whole   wholeCurveWalk
	}{
		{curve.FillRoundRectWithin, curve.FillRoundRect}, {curve.DrawRoundRectWithin, curve.DrawRoundRect},
		{curve.FillArcWithin, curve.FillArc}, {curve.DrawArcWithin, curve.DrawArc},
	}
	boxes := [][6]int32{{0, 0, 20, 16, 30, 120}, {-9, -7, 40, 33, 200, -90}, {12, 10, 30, 30, 8, 8}, {-50, 5, 90, 4, 0, 360}}
	for index, pair := range pairs {
		for _, box := range boxes {
			arguments := []jvm.Value{receiver}
			for _, value := range box {
				arguments = append(arguments, jvm.IntValue(value))
			}
			clear(context.pixels)
			if _, err := runtime.graphicsCurve(pair.bounded)(nil, arguments); err != nil {
				t.Fatal(err)
			}
			got := slices.Clone(context.pixels)
			clear(context.pixels)
			// The fill as graphicsCurve did it before its walk was bounded.
			if err := pair.whole(box[0], box[1], box[2], box[3], box[4], box[5], func(span curve.Span) error {
				rect := context.translatedRect(span.X, span.Y, span.Width, 1).intersect(context.clip)
				if !rect.empty() {
					context.fillRect(rect)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, context.pixels) {
				t.Fatalf("curve %d box %v: the bounded walk drew different pixels", index, box)
			}
		}
	}
}
