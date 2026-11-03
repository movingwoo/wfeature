package curve

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// raster collects emitted spans into a grid, which is the only thing worth
// asserting about geometry that does not draw.
type raster struct {
	width, height int32
	pixels        []bool
}

func newRaster(width, height int32) *raster {
	return &raster{width: width, height: height, pixels: make([]bool, width*height)}
}

func (grid *raster) emit(span Span) error {
	for offset := int32(0); offset < span.Width; offset++ {
		x, y := span.X+offset, span.Y
		if x < 0 || y < 0 || x >= grid.width || y >= grid.height {
			continue
		}
		grid.pixels[y*grid.width+x] = true
	}
	return nil
}

func (grid *raster) at(x, y int32) bool {
	if x < 0 || y < 0 || x >= grid.width || y >= grid.height {
		return false
	}
	return grid.pixels[y*grid.width+x]
}

// A rounded rectangle whose corner diameters are its own width and height is
// an ellipse. Filled as its bounding rectangle it is a hard square, which is
// what this shape looked like before it was drawn as a curve.
func TestFillRoundRectRoundsItsCorners(t *testing.T) {
	grid := newRaster(24, 8)
	if err := FillRoundRect(0, 0, 24, 8, 24, 8, grid.emit); err != nil {
		t.Fatal(err)
	}
	if !grid.at(12, 4) || !grid.at(12, 0) {
		t.Fatal("the ellipse's middle or top was not filled")
	}
	if grid.at(0, 0) || grid.at(23, 7) {
		t.Fatal("a corner was filled, so the shape is still a rectangle")
	}
}

// Corner diameters of zero are a plain rectangle, and the same call has to
// keep drawing one.
func TestFillRoundRectWithoutCornersIsARectangle(t *testing.T) {
	grid := newRaster(6, 6)
	if err := FillRoundRect(0, 0, 6, 6, 0, 0, grid.emit); err != nil {
		t.Fatal(err)
	}
	for _, corner := range [][2]int32{{0, 0}, {5, 0}, {0, 5}, {5, 5}} {
		if !grid.at(corner[0], corner[1]) {
			t.Fatalf("corner %v was rounded despite a diameter of zero", corner)
		}
	}
}

// The outline is the shape's edge and not its inside, which is the whole
// difference between the two round-rectangle calls.
func TestDrawRoundRectLeavesTheInsideAlone(t *testing.T) {
	grid := newRaster(20, 12)
	if err := DrawRoundRect(0, 0, 20, 12, 8, 8, grid.emit); err != nil {
		t.Fatal(err)
	}
	if !grid.at(10, 0) || !grid.at(10, 11) || !grid.at(0, 6) || !grid.at(19, 6) {
		t.Fatal("an edge of the outline is missing")
	}
	if grid.at(10, 6) {
		t.Fatal("the middle was drawn, so the outline is a fill")
	}
	if grid.at(0, 0) {
		t.Fatal("a corner was drawn square")
	}
}

// Zero degrees is three o'clock and positive angles run counter-clockwise, so
// a quarter turn from zero is the top-right quadrant and nothing else. Both
// specifications state this and a title that draws a gauge depends on it.
func TestFillArcCutsThePieItNames(t *testing.T) {
	grid := newRaster(20, 20)
	if err := FillArc(0, 0, 20, 20, 0, 90, grid.emit); err != nil {
		t.Fatal(err)
	}
	if !grid.at(14, 5) {
		t.Fatal("the first quadrant of the pie was not filled")
	}
	if grid.at(5, 5) {
		t.Fatal("the second quadrant was filled, so the sweep is wrong")
	}
	if grid.at(14, 15) {
		t.Fatal("the fourth quadrant was filled, so the angles are not counter-clockwise")
	}
	if grid.at(19, 0) {
		t.Fatal("a corner outside the ellipse was filled")
	}
}

// A negative extent sweeps clockwise from the start rather than drawing
// nothing, which is the half of the contract a caller passing -90 relies on.
func TestFillArcSweepsClockwiseForANegativeExtent(t *testing.T) {
	grid := newRaster(20, 20)
	if err := FillArc(0, 0, 20, 20, 0, -90, grid.emit); err != nil {
		t.Fatal(err)
	}
	if !grid.at(14, 15) {
		t.Fatal("a negative extent did not sweep into the fourth quadrant")
	}
	if grid.at(14, 5) {
		t.Fatal("a negative extent swept counter-clockwise")
	}
}

// Drawing an arc is the curve alone. The pie's two straight edges belong to
// the fill, so a quarter arc must leave its own centre untouched.
func TestDrawArcIsTheCurveAndNotThePie(t *testing.T) {
	grid := newRaster(20, 20)
	if err := DrawArc(0, 0, 20, 20, 0, 90, grid.emit); err != nil {
		t.Fatal(err)
	}
	if !grid.at(19, 9) && !grid.at(19, 10) {
		t.Fatal("the arc does not reach three o'clock")
	}
	if grid.at(10, 10) || grid.at(12, 12) {
		t.Fatal("the pie's inside was drawn, so this is a fill")
	}
}

// A full turn is the whole ellipse however it is spelled, and an extent of
// zero is nothing at all.
func TestArcExtentEdges(t *testing.T) {
	full := newRaster(20, 20)
	if err := FillArc(0, 0, 20, 20, 0, 360, full.emit); err != nil {
		t.Fatal(err)
	}
	for _, point := range [][2]int32{{14, 5}, {5, 5}, {5, 14}, {14, 14}} {
		if !full.at(point[0], point[1]) {
			t.Fatalf("a full turn left %v unfilled", point)
		}
	}

	empty := newRaster(20, 20)
	if err := FillArc(0, 0, 20, 20, 0, 0, empty.emit); err != nil {
		t.Fatal(err)
	}
	for _, filled := range empty.pixels {
		if filled {
			t.Fatal("an extent of zero drew something")
		}
	}
}

// A width or height that is not positive draws nothing rather than running the
// geometry on a degenerate ellipse. Both specifications say so outright.
func TestEmptyBoundsDrawNothing(t *testing.T) {
	for _, size := range [][2]int32{{0, 10}, {10, 0}, {-4, 10}, {10, -4}} {
		grid := newRaster(20, 20)
		if err := FillArc(0, 0, size[0], size[1], 0, 90, grid.emit); err != nil {
			t.Fatal(err)
		}
		if err := DrawArc(0, 0, size[0], size[1], 0, 90, grid.emit); err != nil {
			t.Fatal(err)
		}
		if err := FillRoundRect(0, 0, size[0], size[1], 4, 4, grid.emit); err != nil {
			t.Fatal(err)
		}
		if err := DrawRoundRect(0, 0, size[0], size[1], 4, 4, grid.emit); err != nil {
			t.Fatal(err)
		}
		for _, filled := range grid.pixels {
			if filled {
				t.Fatalf("bounds %v drew something", size)
			}
		}
	}
}

// The shape is placed by its bounding rectangle, so an offset origin moves it
// rather than growing it.
func TestArcIsPlacedByItsBoundingRectangle(t *testing.T) {
	grid := newRaster(40, 40)
	if err := FillArc(20, 10, 10, 10, 0, 360, grid.emit); err != nil {
		t.Fatal(err)
	}
	if !grid.at(25, 15) {
		t.Fatal("the ellipse is not centred in its bounding rectangle")
	}
	if grid.at(5, 5) {
		t.Fatal("the ellipse was drawn at the origin instead of at x, y")
	}
}

// A caller whose fill can fail stops at the first failure instead of drawing
// on past it.
func TestEmitErrorAbandonsTheShape(t *testing.T) {
	failure := errStub{}
	count := 0
	err := FillArc(0, 0, 20, 20, 0, 360, func(Span) error {
		count++
		return failure
	})
	if err != failure {
		t.Fatalf("FillArc returned %v, want the emit failure", err)
	}
	if count != 1 {
		t.Fatalf("the shape kept emitting after a failure: %d spans", count)
	}
}

type errStub struct{}

func (errStub) Error() string { return "emit failed" }

// A polygon's fill is the scanline the platforms draw: a right triangle
// covers x 0 to 3-y on rows 0 to 2, and the row of its bottom vertex has no
// crossings.
func TestFillPolygonWithinIsTheScanline(t *testing.T) {
	covered := map[[2]int32]bool{}
	err := FillPolygonWithin(Bounds{Left: -10, Top: -10, Right: 10, Bottom: 10},
		[]int32{0, 3, 0}, []int32{0, 0, 3}, func(span Span) error {
			for x := span.X; x < span.X+span.Width; x++ {
				covered[[2]int32{x, span.Y}] = true
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	for y := int32(-1); y <= 4; y++ {
		for x := int32(-1); x <= 4; x++ {
			want := y >= 0 && y < 3 && x >= 0 && x <= 3-y
			if covered[[2]int32{x, y}] != want {
				t.Fatalf("pixel (%d, %d) covered = %v, want %v", x, y, !want, want)
			}
		}
	}
}

// Vertices at the ends of int32 walk only the rows inside the bounds, hand
// out spans inside them, and get the crossing right where the product of the
// differences passes int64.
func TestFillPolygonWithinTakesVerticesAnywhere(t *testing.T) {
	bounds := Bounds{Left: 0, Top: 0, Right: 8, Bottom: 8}
	spans := 0
	err := FillPolygonWithin(bounds,
		[]int32{math.MinInt32, math.MaxInt32, 0}, []int32{math.MinInt32, math.MinInt32, math.MaxInt32},
		func(span Span) error {
			spans++
			if span.X < 0 || span.X+span.Width > 8 || span.Y < 0 || span.Y >= 8 {
				t.Fatalf("span %+v is outside the bounds", span)
			}
			if span.X != 0 || span.Width != 8 {
				t.Fatalf("span %+v does not cover the row the triangle covers whole", span)
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if spans != 8 {
		t.Fatalf("%d spans, want one for each of the 8 rows", spans)
	}
	if got := crossing(math.MinInt32, math.MinInt32, 0, math.MaxInt32, 0); got != -1<<30 {
		t.Fatalf("crossing at row 0 = %d, want %d", got, -1<<30)
	}
}

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
