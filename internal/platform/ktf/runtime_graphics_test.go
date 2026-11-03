package ktf

import (
	"math"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

func gfxGraphics(t *testing.T, rt *initializationRuntime, width, height int32) (*runtimeGraphicsState, jvm.Value) {
	t.Helper()
	imageValue, err := runtimeImageCreateSized(rt, rt.client.vm, []jvm.Value{jvm.IntValue(width), jvm.IntValue(height)})
	if err != nil {
		t.Fatal(err)
	}
	graphics, err := runtimeImageGetGraphics(rt, rt.client.vm, []jvm.Value{imageValue})
	if err != nil {
		t.Fatal(err)
	}
	object, err := graphics.Reference()
	if err != nil {
		t.Fatal(err)
	}
	state, ok := object.Native.(*runtimeGraphicsState)
	if !ok {
		t.Fatal("image Graphics has no drawing state")
	}
	state.color = 0x1234
	return state, graphics
}

func randomGraphicsWindow(random *rand.Rand, state *runtimeGraphicsState) {
	state.translateX, state.translateY = int32(random.Intn(30)-15), int32(random.Intn(30)-15)
	state.clipX, state.clipY = 0, 0
	state.clipWidth, state.clipHeight = int32(state.target.Width), int32(state.target.Height)
	if random.Intn(2) == 0 {
		state.clipX, state.clipY = int32(random.Intn(40)-10), int32(random.Intn(30)-10)
		state.clipWidth, state.clipHeight = int32(random.Intn(40)), int32(random.Intn(30))
	}
}

// Graphics.drawLine keeps its pixels under every translation and clip a title
// can set with ordinary coordinates.
func TestGraphicsDrawLineKeepsOrdinaryPixels(t *testing.T) {
	_, rt := newTestRuntime(t)
	const width, height = 40, 30
	state, graphics := gfxGraphics(t, rt, width, height)
	white := slices.Repeat([]uint16{0xffff}, width*height)
	random := rand.New(rand.NewSource(3))
	for trial := 0; trial < 1500; trial++ {
		randomGraphicsWindow(random, state)
		x0, y0 := int32(random.Intn(160)-60), int32(random.Intn(160)-60)
		x1, y1 := int32(random.Intn(160)-60), int32(random.Intn(160)-60)
		wipicrttest.WriteSurfacePixels(t, rt.Runtime, state.target, white)
		if _, err := runtimeGraphicsDrawLine(rt, rt.client.vm, []jvm.Value{graphics, jvm.IntValue(x0), jvm.IntValue(y0), jvm.IntValue(x1), jvm.IntValue(y1)}); err != nil {
			t.Fatal(err)
		}
		want := slices.Clone(white)
		left, top := max(int64(state.clipX), 0), max(int64(state.clipY), 0)
		right := min(int64(state.clipX)+int64(state.clipWidth), width)
		bottom := min(int64(state.clipY)+int64(state.clipHeight), height)
		wipicrttest.ReferenceLine(int64(x0), int64(y0), int64(x1), int64(y1), func(x, y int64) {
			x += int64(state.translateX)
			y += int64(state.translateY)
			if x >= left && x < right && y >= top && y < bottom {
				want[y*width+x] = 0x1234
			}
		})
		if got := wipicrttest.ReadSurfacePixels(t, rt.Runtime, state.target); !slices.Equal(got, want) {
			t.Fatalf("line (%d,%d)-(%d,%d) translate (%d,%d) drew different pixels", x0, y0, x1, y1, state.translateX, state.translateY)
		}
	}
}

// drawLine and drawPolygon once looped forever on Integer.MIN_VALUE, where
// negating the span gives the span back and the doubled error wraps to zero.
func TestGraphicsDrawLineExtremeCoordinatesFinish(t *testing.T) {
	_, rt := newTestRuntime(t)
	const width, height = 40, 30
	state, graphics := gfxGraphics(t, rt, width, height)
	for _, line := range [][4]int32{
		{0, 0, math.MinInt32, 0},
		{0, 0, 0, math.MinInt32},
		{math.MinInt32, math.MinInt32, math.MaxInt32, math.MaxInt32},
		{0, 0, 1500000000, 7},
		{3, math.MaxInt32, 3, math.MinInt32},
	} {
		wipicrttest.FinishesWithin(t, 5*time.Second, func() error {
			_, err := runtimeGraphicsDrawLine(rt, rt.client.vm, []jvm.Value{graphics, jvm.IntValue(line[0]), jvm.IntValue(line[1]), jvm.IntValue(line[2]), jvm.IntValue(line[3])})
			return err
		})
	}
	// The vertical line through x=3 crosses every row of the surface.
	for y := uint32(0); y < height; y++ {
		if pixel := readScreenPixel(t, rt, state, 3, y); pixel != 0x1234 {
			t.Fatalf("far vertical line missed row %d: %#x", y, pixel)
		}
	}
	xs, ys := gfxIntArray(t, rt, 0, 0, math.MinInt32), gfxIntArray(t, rt, 0, 10, 0)
	wipicrttest.FinishesWithin(t, 5*time.Second, func() error {
		_, err := runtimeGraphicsDrawPolygon(false)(rt, rt.client.vm, []jvm.Value{graphics, xs, ys})
		return err
	})
}

func gfxIntArray(t *testing.T, rt *initializationRuntime, values ...int32) jvm.Value {
	t.Helper()
	array, err := rt.client.vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, int32(len(values)))
	if err != nil {
		t.Fatal(err)
	}
	elements := make([]jvm.Value, len(values))
	for index, value := range values {
		elements[index] = jvm.IntValue(value)
	}
	if err := jvm.SetArrayRange(array, 0, elements); err != nil {
		t.Fatal(err)
	}
	return jvm.ReferenceValue(array)
}

// referenceFillPolygon is the scanline fill as it ran before its rows were
// clipped to the surface.
func referenceFillPolygon(rt *initializationRuntime, state *runtimeGraphicsState, xs, ys []int32) error {
	count := min(len(xs), len(ys))
	top, bottom := ys[0], ys[0]
	for _, y := range ys[:count] {
		top, bottom = min(top, y), max(bottom, y)
	}
	for y := top; y <= bottom; y++ {
		var crossings []int32
		for index := 0; index < count; index++ {
			next := (index + 1) % count
			y0, y1 := ys[index], ys[next]
			if y0 == y1 || y < min(y0, y1) || y >= max(y0, y1) {
				continue
			}
			x0, x1 := xs[index], xs[next]
			crossings = append(crossings, x0+(y-y0)*(x1-x0)/(y1-y0))
		}
		slices.Sort(crossings)
		for index := 0; index+1 < len(crossings); index += 2 {
			if err := rt.graphicsFillRect(state, crossings[index], y, crossings[index+1]-crossings[index]+1, 1, state.color); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestGraphicsFillPolygonKeepsOrdinaryPixels(t *testing.T) {
	_, rt := newTestRuntime(t)
	const width, height = 40, 30
	state, graphics := gfxGraphics(t, rt, width, height)
	referenceState, _ := gfxGraphics(t, rt, width, height)
	white := slices.Repeat([]uint16{0xffff}, width*height)
	random := rand.New(rand.NewSource(4))
	fill := runtimeGraphicsDrawPolygon(true)
	for trial := 0; trial < 1500; trial++ {
		randomGraphicsWindow(random, state)
		target := referenceState.target
		*referenceState = *state
		referenceState.target = target
		count := 3 + random.Intn(4)
		xs, ys := make([]int32, count), make([]int32, count)
		for index := range xs {
			xs[index], ys[index] = int32(random.Intn(100)-30), int32(random.Intn(100)-30)
		}
		wipicrttest.WriteSurfacePixels(t, rt.Runtime, state.target, white)
		wipicrttest.WriteSurfacePixels(t, rt.Runtime, referenceState.target, white)
		if _, err := fill(rt, rt.client.vm, []jvm.Value{graphics, gfxIntArray(t, rt, xs...), gfxIntArray(t, rt, ys...)}); err != nil {
			t.Fatal(err)
		}
		if err := referenceFillPolygon(rt, referenceState, xs, ys); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(wipicrttest.ReadSurfacePixels(t, rt.Runtime, state.target), wipicrttest.ReadSurfacePixels(t, rt.Runtime, referenceState.target)) {
			t.Fatalf("polygon %v %v translate (%d,%d) filled different pixels", xs, ys, state.translateX, state.translateY)
		}
	}
}

// A vertex at Integer.MAX_VALUE made the row counter wrap before it could pass
// the bottom, and a span from MIN to MAX walked four thousand million rows.
func TestGraphicsFillPolygonExtremeRowsFinish(t *testing.T) {
	_, rt := newTestRuntime(t)
	state, graphics := gfxGraphics(t, rt, 40, 30)
	fill := runtimeGraphicsDrawPolygon(true)
	for _, polygon := range [][2][]int32{
		{{0, 10, 5}, {0, 0, math.MaxInt32}},
		{{0, 10, 5}, {math.MinInt32, math.MinInt32, math.MaxInt32}},
		{{0, 39, 39, 0}, {math.MinInt32, math.MinInt32, 29, 29}},
	} {
		xs, ys := gfxIntArray(t, rt, polygon[0]...), gfxIntArray(t, rt, polygon[1]...)
		wipicrttest.FinishesWithin(t, 5*time.Second, func() error {
			_, err := fill(rt, rt.client.vm, []jvm.Value{graphics, xs, ys})
			return err
		})
	}
	// The last one is a rectangle reaching up from the bottom row past the
	// top of the surface, so every visible row is filled.
	for y := uint32(0); y < 29; y++ {
		if pixel := readScreenPixel(t, rt, state, 20, y); pixel != 0x1234 {
			t.Fatalf("tall polygon left row %d unfilled: %#x", y, pixel)
		}
	}
}
