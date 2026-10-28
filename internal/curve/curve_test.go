package curve

import (
	"math"
	"testing"
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
