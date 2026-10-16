package curve

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// The walks as they were before they were bounded, kept here as the reference
// the bounded ones must agree with inside the bounds.

func referenceFillRoundRect(x, y, width, height, arcWidth, arcHeight int32, emit Emit) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	radiusX, radiusY := RoundRectRadii(width, height, arcWidth, arcHeight)
	for row := int32(0); row < height; row++ {
		inset := roundRectInset(row, height, radiusX, radiusY)
		if span := width - 2*inset; span > 0 {
			if err := emit(Span{X: x + inset, Y: y + row, Width: span}); err != nil {
				return err
			}
		}
	}
	return nil
}

func referenceDrawRoundRect(x, y, width, height, arcWidth, arcHeight int32, emit Emit) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	radiusX, radiusY := RoundRectRadii(width, height, arcWidth, arcHeight)
	previous := int32(-1)
	for row := int32(0); row < height; row++ {
		inset := roundRectInset(row, height, radiusX, radiusY)
		if row == 0 || row == height-1 {
			if span := width - 2*inset; span > 0 {
				if err := emit(Span{X: x + inset, Y: y + row, Width: span}); err != nil {
					return err
				}
			}
			previous = inset
			continue
		}
		thickness := int32(1)
		if previous >= 0 && previous > inset {
			thickness = previous - inset + 1
		}
		if err := emit(Span{X: x + inset, Y: y + row, Width: thickness}); err != nil {
			return err
		}
		if err := emit(Span{X: x + width - inset - thickness, Y: y + row, Width: thickness}); err != nil {
			return err
		}
		previous = inset
	}
	return nil
}

func referenceFillArc(x, y, width, height, start, extent int32, emit Emit) error {
	if width <= 0 || height <= 0 || extent == 0 {
		return nil
	}
	begin, sweep := arcSpan(start, extent)
	radiusX, radiusY := float64(width)/2, float64(height)/2
	centreX, centreY := float64(x)+radiusX, float64(y)+radiusY
	for row := int32(0); row < height; row++ {
		pixelY := float64(y+row) + 0.5
		offsetY := pixelY - centreY
		runStart := int32(-1)
		for column := int32(0); column <= width; column++ {
			inside := false
			if column < width {
				offsetX := float64(x+column) + 0.5 - centreX
				normalX, normalY := offsetX/radiusX, offsetY/radiusY
				inside = normalX*normalX+normalY*normalY <= 1 && arcContains(offsetX, offsetY, begin, sweep)
			}
			switch {
			case inside && runStart < 0:
				runStart = column
			case !inside && runStart >= 0:
				if err := emit(Span{X: x + runStart, Y: y + row, Width: column - runStart}); err != nil {
					return err
				}
				runStart = -1
			}
		}
	}
	return nil
}

func referenceDrawArc(x, y, width, height, start, extent int32, emit Emit) error {
	if width <= 0 || height <= 0 || extent == 0 {
		return nil
	}
	begin, sweep := arcSpan(start, extent)
	radiusX, radiusY := float64(width)/2, float64(height)/2
	centreX, centreY := float64(x)+radiusX, float64(y)+radiusY
	steps := int(math.Max(8, 2*math.Pi*math.Max(radiusX, radiusY)))
	previousX, previousY := int32(math.MinInt32), int32(math.MinInt32)
	for step := 0; step <= steps; step++ {
		angle := (begin + sweep*float64(step)/float64(steps)) * math.Pi / 180
		pixelX := int32(math.Round(centreX + radiusX*math.Cos(angle) - 0.5))
		pixelY := int32(math.Round(centreY - radiusY*math.Sin(angle) - 0.5))
		if pixelX == previousX && pixelY == previousY {
			continue
		}
		if err := emit(Span{X: pixelX, Y: pixelY, Width: 1}); err != nil {
			return err
		}
		previousX, previousY = pixelX, pixelY
	}
	return nil
}

type walk func(x, y, width, height, a, b int32, emit Emit) error
type boundedWalk func(bounds Bounds, x, y, width, height, a, b int32, emit Emit) error

var walks = []struct {
	name      string
	reference walk
	unbounded walk
	bounded   boundedWalk
}{
	{"FillRoundRect", referenceFillRoundRect, FillRoundRect, FillRoundRectWithin},
	{"DrawRoundRect", referenceDrawRoundRect, DrawRoundRect, DrawRoundRectWithin},
	{"FillArc", referenceFillArc, FillArc, FillArcWithin},
	{"DrawArc", referenceDrawArc, DrawArc, DrawArcWithin},
}

func collect(call func(Emit) error) []Span {
	var spans []Span
	if err := call(func(span Span) error {
		spans = append(spans, span)
		return nil
	}); err != nil {
		panic(err)
	}
	return spans
}

// coverage counts how many times each pixel inside the bounds is drawn, which
// is what a caller's clipped fill sees: an opaque fill the set, an XOR fill the
// count.
func coverage(spans []Span, bounds Bounds) map[[2]int64]int {
	counts := make(map[[2]int64]int)
	for _, span := range spans {
		row := int64(span.Y)
		if row < bounds.Top || row >= bounds.Bottom {
			continue
		}
		left := max(int64(span.X), bounds.Left)
		right := min(int64(span.X)+int64(span.Width), bounds.Right)
		for column := left; column < right; column++ {
			counts[[2]int64{column, row}]++
		}
	}
	return counts
}

// The unbounded entry points still hand out exactly the spans they did.
func TestUnboundedWalksAreUnchanged(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	for range 600 {
		x, y := int32(random.Intn(400)-200), int32(random.Intn(400)-200)
		width, height := int32(random.Intn(140)-4), int32(random.Intn(140)-4)
		a, b := int32(random.Intn(1000)-500), int32(random.Intn(1000)-500)
		for _, current := range walks {
			want := collect(func(emit Emit) error { return current.reference(x, y, width, height, a, b, emit) })
			got := collect(func(emit Emit) error { return current.unbounded(x, y, width, height, a, b, emit) })
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s(%d, %d, %d, %d, %d, %d) changed its spans", current.name, x, y, width, height, a, b)
			}
		}
	}
}

// Inside the bounds, a bounded walk draws every pixel exactly as many times as
// the unbounded one did, whatever part of the shape the bounds cut out.
func TestBoundedWalksDrawWhatTheUnboundedOnesDrewInsideTheBounds(t *testing.T) {
	random := rand.New(rand.NewSource(2))
	for range 1500 {
		x, y := int32(random.Intn(400)-200), int32(random.Intn(400)-200)
		width, height := int32(random.Intn(160)-4), int32(random.Intn(160)-4)
		a, b := int32(random.Intn(1000)-500), int32(random.Intn(1000)-500)
		left, top := int64(random.Intn(400)-200), int64(random.Intn(400)-200)
		bounds := Bounds{Left: left, Top: top, Right: left + int64(random.Intn(200)-10), Bottom: top + int64(random.Intn(200)-10)}
		for _, current := range walks {
			want := coverage(collect(func(emit Emit) error { return current.reference(x, y, width, height, a, b, emit) }), bounds)
			spans := collect(func(emit Emit) error { return current.bounded(bounds, x, y, width, height, a, b, emit) })
			if got := coverage(spans, bounds); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s(%d, %d, %d, %d, %d, %d) within %+v drew differently", current.name, x, y, width, height, a, b, bounds)
			}
			// Nothing is handed out past the bounds, so a caller whose fill
			// costs a span's width pays only for what it can draw.
			for _, span := range spans {
				if span.Width <= 0 || int64(span.X) < bounds.Left || int64(span.X)+int64(span.Width) > bounds.Right ||
					int64(span.Y) < bounds.Top || int64(span.Y) >= bounds.Bottom {
					t.Fatalf("%s handed out %+v outside %+v", current.name, span, bounds)
				}
			}
		}
	}
}

// A large arc's curve crosses small bounds in a few short stretches. Only
// those are walked, and what they draw is what the whole walk drew there,
// including at the far ends of very flat ellipses where the curve creeps.
func TestBoundedDrawArcSkipsTheCurveOutsideTheBounds(t *testing.T) {
	cases := []struct{ x, y, width, height, start, extent int32 }{
		{-200000, -200000, 400240, 400320, 0, 360},
		{-1000000, 100, 2000000, 3, 0, 360},
		{100, -1000000, 3, 2000000, 0, 360},
		{-1999990, -500, 2000000, 1000, 0, 360},
		{-300000, 10, 600000, 900000, 15, 160},
		{-300000, 10, 600000, 900000, 200, -330},
	}
	for _, shape := range cases {
		for _, bounds := range []Bounds{{0, 0, 240, 320}, {-50, -50, 20, 400}, {100, 0, 101, 1}} {
			want := coverage(collect(func(emit Emit) error {
				return referenceDrawArc(shape.x, shape.y, shape.width, shape.height, shape.start, shape.extent, emit)
			}), bounds)
			got := coverage(collect(func(emit Emit) error {
				return DrawArcWithin(bounds, shape.x, shape.y, shape.width, shape.height, shape.start, shape.extent, emit)
			}), bounds)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("DrawArc %+v within %+v drew differently", shape, bounds)
			}
		}
	}
}

// Boxes no surface could hold cost what their visible part costs. A width of
// the largest int used to wrap the column counter and never finish.
func TestBoundedWalksReturnForBoxesFarLargerThanAnySurface(t *testing.T) {
	screen := Bounds{Left: 0, Top: 0, Right: 240, Bottom: 320}
	calls := []struct {
		name string
		call func(Emit) error
	}{
		{"FillArc as wide as an int", func(emit Emit) error { return FillArcWithin(screen, 0, 0, math.MaxInt32, 1, 0, 360, emit) }},
		{"FillArc as large as an int", func(emit Emit) error {
			return FillArcWithin(screen, -math.MaxInt32/2, -math.MaxInt32/2, math.MaxInt32, math.MaxInt32, 0, 360, emit)
		}},
		{"DrawArc as large as an int", func(emit Emit) error {
			return DrawArcWithin(screen, 0, 0, math.MaxInt32, math.MaxInt32, 0, 360, emit)
		}},
		{"DrawArc as flat as can be", func(emit Emit) error {
			return DrawArcWithin(screen, -math.MaxInt32/2+100, 5, math.MaxInt32, 1, 0, 360, emit)
		}},
		{"FillRoundRect as tall as an int", func(emit Emit) error {
			return FillRoundRectWithin(screen, 0, math.MinInt32, 100, math.MaxInt32, 20, 20, emit)
		}},
		{"DrawRoundRect as tall as an int", func(emit Emit) error {
			return DrawRoundRectWithin(screen, 0, math.MinInt32, 100, math.MaxInt32, 20, 20, emit)
		}},
	}
	for _, current := range calls {
		done := make(chan int, 1)
		go func() {
			count := 0
			if err := current.call(func(Span) error { count++; return nil }); err != nil {
				panic(err)
			}
			done <- count
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s did not return", current.name)
		}
	}
}
