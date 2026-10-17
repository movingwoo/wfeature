package lgt

import (
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"
)

// The draw primitives take their extents straight from the guest, and a call
// with a width of two billion is one call. Each is clipped to what it can draw
// before it iterates. The pixels it draws inside the surface have to be the
// ones the unclipped walk drew, so every test here draws through both and
// compares; the reference walks below are the loops the primitives ran before
// they were clipped.

func referenceFill(context *graphicsContext, x, y, width, height int, color uint16) {
	for row := y; row < y+height; row++ {
		for column := x; column < x+width; column++ {
			context.put(column, row, color)
		}
	}
}

func referenceLine(context *graphicsContext, x1, y1, x2, y2 int, color uint16) {
	abs := func(value int) int {
		if value < 0 {
			return -value
		}
		return value
	}
	deltaX, deltaY := abs(x2-x1), -abs(y2-y1)
	stepX, stepY := 1, 1
	if x1 > x2 {
		stepX = -1
	}
	if y1 > y2 {
		stepY = -1
	}
	err := deltaX + deltaY
	for step := 0; step <= deltaX-deltaY+1; step++ {
		context.put(x1, y1, color)
		if x1 == x2 && y1 == y2 {
			return
		}
		doubled := 2 * err
		if doubled >= deltaY {
			err += deltaY
			x1 += stepX
		}
		if doubled <= deltaX {
			err += deltaX
			y1 += stepY
		}
	}
}

func referencePolygon(context *graphicsContext, xs, ys []int, color uint16) {
	count := min(len(xs), len(ys))
	if count < 2 {
		return
	}
	top, bottom := ys[0], ys[0]
	for _, y := range ys[:count] {
		top, bottom = min(top, y), max(bottom, y)
	}
	crossings := make([]int, 0, count)
	for y := top; y <= bottom; y++ {
		crossings = crossings[:0]
		for index := 0; index < count; index++ {
			next := (index + 1) % count
			y0, y1 := ys[index], ys[next]
			if y0 == y1 || y < min(y0, y1) || y >= max(y0, y1) {
				continue
			}
			x0, x1 := xs[index], xs[next]
			crossings = append(crossings, x0+(y-y0)*(x1-x0)/(y1-y0))
		}
		sort.Ints(crossings)
		for index := 0; index+1 < len(crossings); index += 2 {
			referenceFill(context, crossings[index], y, crossings[index+1]-crossings[index]+1, 1, color)
		}
	}
}

// referenceBlit is the loop copyArea, copyFramebuffer and drawImage shared: the
// guest's whole rectangle, with every pixel checked against the source.
func referenceBlit(context *graphicsContext, source *framebuffer, pixels []uint16, values []int32, pixel func(index int) (uint16, bool)) {
	destinationX, destinationY := int(values[0]), int(values[1])
	width, height := int(values[2]), int(values[3])
	sourceX, sourceY := int(values[5]), int(values[6])
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			sx, sy := sourceX+column, sourceY+row
			if sx < 0 || sy < 0 || sx >= source.width || sy >= source.height {
				continue
			}
			if value, draw := pixel(sy*source.width + sx); draw {
				context.put(destinationX+column, destinationY+row, value)
			}
		}
	}
}

// drawPair is two identical surfaces and two identical contexts on them, one
// for the primitive and one for its reference.
type drawPair struct {
	clipped, reference *graphicsContext
}

func newDrawPair(random *rand.Rand, width, height int) drawPair {
	pixels := make([]uint16, width*height)
	for index := range pixels {
		pixels[index] = uint16(random.Intn(1 << 16))
	}
	context := func() *graphicsContext {
		return &graphicsContext{
			target: &framebuffer{width: width, height: height, pixels: append([]uint16(nil), pixels...)},
		}
	}
	pair := drawPair{context(), context()}
	clipX, clipY := random.Intn(width+6)-3, random.Intn(height+6)-3
	clipWidth, clipHeight := random.Intn(width+6)-2, random.Intn(height+6)-2
	offsetX, offsetY := random.Intn(9)-4, random.Intn(9)-4
	xor := random.Intn(3) == 0
	for _, context := range []*graphicsContext{pair.clipped, pair.reference} {
		context.clipX, context.clipY, context.clipWidth, context.clipHeight = clipX, clipY, clipWidth, clipHeight
		context.offsetX, context.offsetY, context.xor = offsetX, offsetY, xor
	}
	return pair
}

func (pair drawPair) compare(t *testing.T, what string, arguments ...any) {
	t.Helper()
	for index, got := range pair.clipped.target.pixels {
		if want := pair.reference.target.pixels[index]; got != want {
			t.Fatalf("%s %v: pixel (%d, %d) = %#x, want %#x", what, arguments,
				index%pair.clipped.target.width, index/pair.clipped.target.width, got, want)
		}
	}
}

func TestClippedFillDrawsWhatTheFullWalkDrew(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	for range 5000 {
		pair := newDrawPair(random, 13, 9)
		x, y := random.Intn(40)-15, random.Intn(30)-10
		width, height := random.Intn(40)-5, random.Intn(30)-5
		color := uint16(random.Intn(1 << 16))
		pair.clipped.fill(x, y, width, height, color)
		referenceFill(pair.reference, x, y, width, height, color)
		pair.compare(t, "fill", x, y, width, height)
	}
}

func TestClippedLineDrawsWhatTheFullWalkDrew(t *testing.T) {
	// Every pair of endpoints in a small window, which covers all eight
	// octants and every tie the walk breaks, through one ordinary context.
	random := rand.New(rand.NewSource(2))
	pair := drawPair{}
	for x1 := -4; x1 <= 16; x1 += 2 {
		for y1 := -4; y1 <= 12; y1 += 2 {
			for x2 := -5; x2 <= 17; x2++ {
				for y2 := -5; y2 <= 13; y2++ {
					pair = newDrawPair(random, 13, 9)
					pair.clipped.line(x1, y1, x2, y2, 0xffff)
					referenceLine(pair.reference, x1, y1, x2, y2, 0xffff)
					pair.compare(t, "line", x1, y1, x2, y2)
				}
			}
		}
	}
	// Long lines whose endpoints are far outside the surface: the steps the
	// clipped line computes have to land where the walk would have reached.
	for range 3000 {
		pair = newDrawPair(random, 13, 9)
		x1, y1 := random.Intn(8001)-4000, random.Intn(8001)-4000
		x2, y2 := random.Intn(8001)-4000, random.Intn(8001)-4000
		if random.Intn(2) == 0 {
			// Through the surface, so the visible part is not always empty.
			x2, y2 = 2*random.Intn(13)-x1, 2*random.Intn(9)-y1
		}
		pair.clipped.line(x1, y1, x2, y2, 0xffff)
		referenceLine(pair.reference, x1, y1, x2, y2, 0xffff)
		pair.compare(t, "line", x1, y1, x2, y2)
	}
}

func TestClippedPolygonDrawsWhatTheFullWalkDrew(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	for range 3000 {
		pair := newDrawPair(random, 13, 9)
		count := 2 + random.Intn(5)
		xs, ys := make([]int, count), make([]int, count)
		for index := range xs {
			xs[index], ys[index] = random.Intn(60)-25, random.Intn(50)-20
		}
		pair.clipped.polygon(xs, ys, 0xf81f, true)
		referencePolygon(pair.reference, xs, ys, 0xf81f)
		pair.compare(t, "polygon", xs, ys)
	}
}

func TestClippedBlitsDrawWhatTheFullWalkDrew(t *testing.T) {
	random := rand.New(rand.NewSource(4))
	client := fixtureClient(t)
	for round := range 3000 {
		pair := newDrawPair(random, 13, 9)
		source := &framebuffer{handle: 0x7e000000, width: 1 + random.Intn(15), height: 1 + random.Intn(11)}
		source.pixels = make([]uint16, source.width*source.height)
		for index := range source.pixels {
			source.pixels[index] = uint16(random.Intn(1 << 16))
			if random.Intn(4) == 0 {
				source.pixels[index] = maskPixel
			}
		}
		client.framebuffers[source.handle] = source
		values := []int32{
			int32(random.Intn(30) - 10), int32(random.Intn(24) - 8),
			int32(random.Intn(30) - 5), int32(random.Intn(24) - 5),
			int32(source.handle),
			int32(random.Intn(30) - 10), int32(random.Intn(24) - 8),
		}
		switch round % 3 {
		case 0:
			// copyArea reads the target itself, through a snapshot.
			area := []int32{values[0], values[1], values[2], values[3], values[5], values[6]}
			snapshot := append([]uint16(nil), pair.reference.target.pixels...)
			client.copyArea(pair.clipped, area)
			referenceBlit(pair.reference, pair.reference.target, snapshot, values,
				func(index int) (uint16, bool) { return snapshot[index], true })
			pair.compare(t, "copyArea", area)
		case 1:
			if err := client.copyFramebuffer(pair.clipped, values); err != nil {
				t.Fatal(err)
			}
			referenceBlit(pair.reference, source, source.pixels, values,
				func(index int) (uint16, bool) { return source.pixels[index], source.pixels[index] != maskPixel })
			pair.compare(t, "copyFramebuffer", values)
		case 2:
			if err := client.drawImage(pair.clipped, values); err != nil {
				t.Fatal(err)
			}
			referenceBlit(pair.reference, source, source.pixels, values,
				func(index int) (uint16, bool) { return source.pixels[index], source.pixels[index] != maskPixel })
			pair.compare(t, "drawImage", values)
		}
	}
}

// withinDeadline fails the test when work does not come back in time. What it
// guards would take hours unbounded and takes milliseconds bounded, so the
// deadline only has to be generous enough for a loaded machine.
func withinDeadline(t *testing.T, what string, work func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not return in time", what)
	}
}

func TestDrawPrimitivesWithGuestSizedExtentsReturn(t *testing.T) {
	const (
		low  = math.MinInt32
		high = math.MaxInt32
	)
	surface := func() *graphicsContext {
		target := &framebuffer{width: 16, height: 8, pixels: make([]uint16, 16*8)}
		return &graphicsContext{target: target, clipWidth: target.width, clipHeight: target.height}
	}

	context := surface()
	withinDeadline(t, "fillRect(0, 0, MaxInt32, MaxInt32)", func() { context.fill(0, 0, high, high, 0x1234) })
	for index, pixel := range context.target.pixels {
		if pixel != 0x1234 {
			t.Fatalf("the fill missed pixel %d", index)
		}
	}
	withinDeadline(t, "fillRect from MinInt32", func() { surface().fill(low, low, high, high, 1) })

	// A line across the whole int32 range in both directions still lands on
	// the surface's diagonal pixels, which is where the walk reaches them.
	for _, line := range [][4]int{{low, low, high, high}, {high, high, low, low}} {
		context := surface()
		withinDeadline(t, "a line across the int32 range", func() {
			context.line(line[0], line[1], line[2], line[3], 0xffff)
		})
		for x := 0; x < 8; x++ {
			if context.target.pixels[x*16+x] != 0xffff {
				t.Fatalf("line %v missed (%d, %d)", line, x, x)
			}
		}
	}
	withinDeadline(t, "a line between opposite corners", func() { surface().line(low, high, high, low, 1) })
	withinDeadline(t, "a polygon as tall as the int32 range", func() {
		surface().polygon([]int{0, 8, 4}, []int{low, low, high}, 1, true)
	})

	client := fixtureClient(t)
	source := &framebuffer{handle: 0x7e000000, width: 4, height: 4, pixels: make([]uint16, 16)}
	client.framebuffers[source.handle] = source
	withinDeadline(t, "copyArea of MaxInt32 x MaxInt32", func() {
		client.copyArea(surface(), []int32{0, 0, high, high, 0, 0})
	})
	withinDeadline(t, "copyFramebuffer of MaxInt32 x MaxInt32", func() {
		if err := client.copyFramebuffer(surface(), []int32{0, 0, high, high, int32(source.handle), 0, 0}); err != nil {
			t.Error(err)
		}
	})
	withinDeadline(t, "drawImage of MaxInt32 x MaxInt32", func() {
		if err := client.drawImage(surface(), []int32{low, low, high, high, int32(source.handle), low, low}); err != nil {
			t.Error(err)
		}
	})
}
