package wipicrt_test

import (
	"math"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

// The clipped walk must land on exactly the pixels the unclipped loop did for
// every line, whichever part of it the window keeps.
func TestClippedLineMatchesTheUnclippedLoop(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	for trial := 0; trial < 20500; trial++ {
		x0, y0 := int32(random.Intn(200)-100), int32(random.Intn(200)-100)
		x1, y1 := int32(random.Intn(200)-100), int32(random.Intn(200)-100)
		left, top := int64(random.Intn(120)-60), int64(random.Intn(120)-60)
		right, bottom := left+int64(random.Intn(80)), top+int64(random.Intn(80))
		if trial >= 20000 {
			// Long lines whose window sits somewhere along them, which is
			// where the walk starts part-way with a carried error term.
			x0, y0 = int32(random.Intn(80000)-40000), int32(random.Intn(80000)-40000)
			x1, y1 = int32(random.Intn(80000)-40000), int32(random.Intn(80000)-40000)
			left, top = int64(random.Intn(80000)-40000), int64(random.Intn(80000)-40000)
			right, bottom = left+int64(random.Intn(30000)), top+int64(random.Intn(30000))
		}
		var want, got [][2]int64
		wipicrttest.ReferenceLine(int64(x0), int64(y0), int64(x1), int64(y1), func(x, y int64) {
			if x >= left && x < right && y >= top && y < bottom {
				want = append(want, [2]int64{x, y})
			}
		})
		err := wipicrt.ClippedLine(x0, y0, x1, y1, left, top, right, bottom, func(x, y int32) error {
			if int64(x) >= left && int64(x) < right && int64(y) >= top && int64(y) < bottom {
				got = append(got, [2]int64{int64(x), int64(y)})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(want, got) {
			t.Fatalf("line (%d,%d)-(%d,%d) in [%d,%d)x[%d,%d): got %v, want %v", x0, y0, x1, y1, left, right, top, bottom, got, want)
		}
	}
}

// MC_grpDrawLine draws the same pixels it always has for coordinates a title
// really passes, with and without a context clip.
func TestWIPICDrawLineKeepsOrdinaryPixels(t *testing.T) {
	_, rt := wipicrttest.NewRuntime(t)
	const width, height = 40, 30
	handle, framebuffer := gfxSurface(t, rt, width, height)
	blank := make([]uint16, width*height)
	random := rand.New(rand.NewSource(2))
	for trial := 0; trial < 1500; trial++ {
		var words [3]uint32
		var clip wipicrt.WIPICClip
		if trial%3 == 0 {
			left, top := random.Intn(30), random.Intn(20)
			right, bottom := left+random.Intn(30), top+random.Intn(25)
			words = [3]uint32{1, uint32(left) | uint32(top)<<16, uint32(right) | uint32(bottom)<<16}
			clip = wipicrt.WIPICClip{Enabled: true, Left: int32(left), Top: int32(top), Right: int32(right), Bottom: int32(bottom)}
		}
		context := gfxContext(t, rt, 0x1234, words)
		x0, y0 := int32(random.Intn(160)-60), int32(random.Intn(160)-60)
		x1, y1 := int32(random.Intn(160)-60), int32(random.Intn(160)-60)
		wipicrttest.WriteSurfacePixels(t, rt, framebuffer, blank)
		thread := gfxThread(t, rt, handle, uint32(x0), uint32(y0), uint32(x1), uint32(y1), context)
		if _, err := rt.WIPICDrawLine(thread); err != nil {
			t.Fatal(err)
		}
		want := make([]uint16, width*height)
		wipicrttest.ReferenceLine(int64(x0), int64(y0), int64(x1), int64(y1), func(x, y int64) {
			if x >= 0 && y >= 0 && x < width && y < height && clip.Contains(int32(x), int32(y)) {
				want[y*width+x] = 0x1234
			}
		})
		if got := wipicrttest.ReadSurfacePixels(t, rt, framebuffer); !slices.Equal(got, want) {
			t.Fatalf("line (%d,%d)-(%d,%d) clip %+v drew different pixels", x0, y0, x1, y1, clip)
		}
	}
}

// An endpoint a long way off the surface — an unshifted fixed-point value, a
// projected point behind the camera — once spun the host forever, because the
// doubled error term overflowed and the loop's only exit was reaching the far
// end exactly.
func TestWIPICDrawLineFarEndpointsFinish(t *testing.T) {
	_, rt := wipicrttest.NewRuntime(t)
	const width, height = 40, 30
	handle, framebuffer := gfxSurface(t, rt, width, height)
	context := gfxContext(t, rt, 0xffff, [3]uint32{})
	lines := [][4]int32{
		{0, 0, 1500000000, 7},
		{0, 0, 1000000000, 7},
		{math.MinInt32, 0, 100, 10},
		{math.MinInt32, 0, math.MaxInt32, 1},
		{0, 0, math.MinInt32, 0},
		{5, math.MaxInt32, 5, math.MinInt32},
		{math.MaxInt32, math.MinInt32, math.MinInt32, math.MaxInt32},
	}
	for _, line := range lines {
		wipicrttest.WriteSurfacePixels(t, rt, framebuffer, make([]uint16, width*height))
		thread := gfxThread(t, rt, handle, uint32(line[0]), uint32(line[1]), uint32(line[2]), uint32(line[3]), context)
		wipicrttest.FinishesWithin(t, 5*time.Second, func() error {
			_, err := rt.WIPICDrawLine(thread)
			return err
		})
		pixels := wipicrttest.ReadSurfacePixels(t, rt, framebuffer)
		switch line {
		case lines[0]:
			// Seven rows over a billion and a half columns: the first forty
			// columns are all on the first row.
			for x := 0; x < width; x++ {
				if pixels[x] != 0xffff || pixels[width+x] != 0 {
					t.Fatalf("far shallow line column %d: row 0 %#x, row 1 %#x", x, pixels[x], pixels[width+x])
				}
			}
		case lines[5]:
			for y := 0; y < height; y++ {
				if pixels[y*width+5] != 0xffff {
					t.Fatalf("far vertical line missed row %d", y)
				}
			}
		}
	}
}
