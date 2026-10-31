package ktf

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"math/rand"
	goruntime "runtime"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// finishesWithin runs a drawing call that extreme guest arguments once turned
// into a host loop with no practical end, and fails instead of hanging the
// test binary when it still does.
func finishesWithin(t *testing.T, limit time.Duration, call func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(limit):
		t.Fatalf("call still running after %v", limit)
	}
}

// gfxReviewThread lays WIPI C arguments out the way the guest passes them:
// the first four in registers, the rest on the stack.
func gfxReviewThread(t *testing.T, rt *initializationRuntime, arguments ...uint32) *armcore.Thread {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for index := 0; index < min(len(arguments), 4); index++ {
		if err := thread.SetRegister(index, arguments[index]); err != nil {
			t.Fatal(err)
		}
	}
	if len(arguments) > 4 {
		stack := make([]byte, (len(arguments)-4)*4)
		for index, value := range arguments[4:] {
			binary.LittleEndian.PutUint32(stack[index*4:], value)
		}
		address, err := rt.AllocateBytes(stack)
		if err != nil {
			t.Fatal(err)
		}
		if err := thread.SetRegister(armcore.RegisterSP, address); err != nil {
			t.Fatal(err)
		}
	}
	return thread
}

// gfxReviewContext builds a graphics context with a foreground pixel and the
// three clip words the context starts with.
func gfxReviewContext(t *testing.T, rt *initializationRuntime, pixel uint32, clip [3]uint32) uint32 {
	t.Helper()
	record := make([]byte, wipicrt.WIPICGraphicsContextSize)
	for index, word := range clip {
		binary.LittleEndian.PutUint32(record[index*4:], word)
	}
	binary.LittleEndian.PutUint32(record[12:], pixel)
	address, err := rt.AllocateBytes(record)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func gfxReviewSurface(t *testing.T, rt *initializationRuntime, width, height uint32) (uint32, wipicrt.WIPICFramebuffer) {
	t.Helper()
	handle, err := rt.NewWIPICFramebufferRecord(width, height)
	if err != nil {
		t.Fatal(err)
	}
	framebuffer, err := rt.ReadWIPICFramebuffer(handle)
	if err != nil {
		t.Fatal(err)
	}
	return handle, framebuffer
}

func readSurfacePixels(t *testing.T, rt *initializationRuntime, framebuffer wipicrt.WIPICFramebuffer) []uint16 {
	t.Helper()
	data := make([]byte, framebuffer.Bpl*framebuffer.Height)
	if err := rt.client.Core().Memory().Read(framebuffer.Pixels, data); err != nil {
		t.Fatal(err)
	}
	pixels := make([]uint16, framebuffer.Width*framebuffer.Height)
	for y := uint32(0); y < framebuffer.Height; y++ {
		for x := uint32(0); x < framebuffer.Width; x++ {
			pixels[y*framebuffer.Width+x] = binary.LittleEndian.Uint16(data[y*framebuffer.Bpl+x*2:])
		}
	}
	return pixels
}

func writeSurfacePixels(t *testing.T, rt *initializationRuntime, framebuffer wipicrt.WIPICFramebuffer, pixels []uint16) {
	t.Helper()
	data := make([]byte, framebuffer.Bpl*framebuffer.Height)
	for y := uint32(0); y < framebuffer.Height; y++ {
		for x := uint32(0); x < framebuffer.Width; x++ {
			binary.LittleEndian.PutUint16(data[y*framebuffer.Bpl+x*2:], pixels[y*framebuffer.Width+x])
		}
	}
	if err := rt.client.Core().Memory().Write(framebuffer.Pixels, data); err != nil {
		t.Fatal(err)
	}
}

func randomSurfacePixels(random *rand.Rand, count uint32) []uint16 {
	pixels := make([]uint16, count)
	for index := range pixels {
		pixels[index] = uint16(random.Intn(0x10000))
	}
	return pixels
}

// referenceLine is the Bresenham loop both line calls ran before they were
// clipped, in a width that ordinary coordinates never overflow.
func referenceLine(x0, y0, x1, y1 int64, plot func(x, y int64)) {
	deltaX, deltaY := x1-x0, y1-y0
	if deltaX < 0 {
		deltaX = -deltaX
	}
	if deltaY < 0 {
		deltaY = -deltaY
	}
	stepX, stepY := int64(1), int64(1)
	if x0 > x1 {
		stepX = -1
	}
	if y0 > y1 {
		stepY = -1
	}
	difference := deltaX - deltaY
	for {
		plot(x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		doubled := 2 * difference
		if doubled > -deltaY {
			difference -= deltaY
			x0 += stepX
		}
		if doubled < deltaX {
			difference += deltaX
			y0 += stepY
		}
	}
}

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
		referenceLine(int64(x0), int64(y0), int64(x1), int64(y1), func(x, y int64) {
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
	_, rt := newTestRuntime(t)
	const width, height = 40, 30
	handle, framebuffer := gfxReviewSurface(t, rt, width, height)
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
		context := gfxReviewContext(t, rt, 0x1234, words)
		x0, y0 := int32(random.Intn(160)-60), int32(random.Intn(160)-60)
		x1, y1 := int32(random.Intn(160)-60), int32(random.Intn(160)-60)
		writeSurfacePixels(t, rt, framebuffer, blank)
		thread := gfxReviewThread(t, rt, handle, uint32(x0), uint32(y0), uint32(x1), uint32(y1), context)
		if _, err := rt.WIPICDrawLine(thread); err != nil {
			t.Fatal(err)
		}
		want := make([]uint16, width*height)
		referenceLine(int64(x0), int64(y0), int64(x1), int64(y1), func(x, y int64) {
			if x >= 0 && y >= 0 && x < width && y < height && clip.Contains(int32(x), int32(y)) {
				want[y*width+x] = 0x1234
			}
		})
		if got := readSurfacePixels(t, rt, framebuffer); !slices.Equal(got, want) {
			t.Fatalf("line (%d,%d)-(%d,%d) clip %+v drew different pixels", x0, y0, x1, y1, clip)
		}
	}
}

// An endpoint a long way off the surface — an unshifted fixed-point value, a
// projected point behind the camera — once spun the host forever, because the
// doubled error term overflowed and the loop's only exit was reaching the far
// end exactly.
func TestWIPICDrawLineFarEndpointsFinish(t *testing.T) {
	_, rt := newTestRuntime(t)
	const width, height = 40, 30
	handle, framebuffer := gfxReviewSurface(t, rt, width, height)
	context := gfxReviewContext(t, rt, 0xffff, [3]uint32{})
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
		writeSurfacePixels(t, rt, framebuffer, make([]uint16, width*height))
		thread := gfxReviewThread(t, rt, handle, uint32(line[0]), uint32(line[1]), uint32(line[2]), uint32(line[3]), context)
		finishesWithin(t, 5*time.Second, func() error {
			_, err := rt.WIPICDrawLine(thread)
			return err
		})
		pixels := readSurfacePixels(t, rt, framebuffer)
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

func gfxReviewGraphics(t *testing.T, rt *initializationRuntime, width, height int32) (*runtimeGraphicsState, jvm.Value) {
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
	state, graphics := gfxReviewGraphics(t, rt, width, height)
	white := slices.Repeat([]uint16{0xffff}, width*height)
	random := rand.New(rand.NewSource(3))
	for trial := 0; trial < 1500; trial++ {
		randomGraphicsWindow(random, state)
		x0, y0 := int32(random.Intn(160)-60), int32(random.Intn(160)-60)
		x1, y1 := int32(random.Intn(160)-60), int32(random.Intn(160)-60)
		writeSurfacePixels(t, rt, state.target, white)
		if _, err := runtimeGraphicsDrawLine(rt, rt.client.vm, []jvm.Value{graphics, jvm.IntValue(x0), jvm.IntValue(y0), jvm.IntValue(x1), jvm.IntValue(y1)}); err != nil {
			t.Fatal(err)
		}
		want := slices.Clone(white)
		left, top := max(int64(state.clipX), 0), max(int64(state.clipY), 0)
		right := min(int64(state.clipX)+int64(state.clipWidth), width)
		bottom := min(int64(state.clipY)+int64(state.clipHeight), height)
		referenceLine(int64(x0), int64(y0), int64(x1), int64(y1), func(x, y int64) {
			x += int64(state.translateX)
			y += int64(state.translateY)
			if x >= left && x < right && y >= top && y < bottom {
				want[y*width+x] = 0x1234
			}
		})
		if got := readSurfacePixels(t, rt, state.target); !slices.Equal(got, want) {
			t.Fatalf("line (%d,%d)-(%d,%d) translate (%d,%d) drew different pixels", x0, y0, x1, y1, state.translateX, state.translateY)
		}
	}
}

// drawLine and drawPolygon once looped forever on Integer.MIN_VALUE, where
// negating the span gives the span back and the doubled error wraps to zero.
func TestGraphicsDrawLineExtremeCoordinatesFinish(t *testing.T) {
	_, rt := newTestRuntime(t)
	const width, height = 40, 30
	state, graphics := gfxReviewGraphics(t, rt, width, height)
	for _, line := range [][4]int32{
		{0, 0, math.MinInt32, 0},
		{0, 0, 0, math.MinInt32},
		{math.MinInt32, math.MinInt32, math.MaxInt32, math.MaxInt32},
		{0, 0, 1500000000, 7},
		{3, math.MaxInt32, 3, math.MinInt32},
	} {
		finishesWithin(t, 5*time.Second, func() error {
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
	xs, ys := gfxReviewIntArray(t, rt, 0, 0, math.MinInt32), gfxReviewIntArray(t, rt, 0, 10, 0)
	finishesWithin(t, 5*time.Second, func() error {
		_, err := runtimeGraphicsDrawPolygon(false)(rt, rt.client.vm, []jvm.Value{graphics, xs, ys})
		return err
	})
}

func gfxReviewIntArray(t *testing.T, rt *initializationRuntime, values ...int32) jvm.Value {
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
	state, graphics := gfxReviewGraphics(t, rt, width, height)
	referenceState, _ := gfxReviewGraphics(t, rt, width, height)
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
		writeSurfacePixels(t, rt, state.target, white)
		writeSurfacePixels(t, rt, referenceState.target, white)
		if _, err := fill(rt, rt.client.vm, []jvm.Value{graphics, gfxReviewIntArray(t, rt, xs...), gfxReviewIntArray(t, rt, ys...)}); err != nil {
			t.Fatal(err)
		}
		if err := referenceFillPolygon(rt, referenceState, xs, ys); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(readSurfacePixels(t, rt, state.target), readSurfacePixels(t, rt, referenceState.target)) {
			t.Fatalf("polygon %v %v translate (%d,%d) filled different pixels", xs, ys, state.translateX, state.translateY)
		}
	}
}

// A vertex at Integer.MAX_VALUE made the row counter wrap before it could pass
// the bottom, and a span from MIN to MAX walked four thousand million rows.
func TestGraphicsFillPolygonExtremeRowsFinish(t *testing.T) {
	_, rt := newTestRuntime(t)
	state, graphics := gfxReviewGraphics(t, rt, 40, 30)
	fill := runtimeGraphicsDrawPolygon(true)
	for _, polygon := range [][2][]int32{
		{{0, 10, 5}, {0, 0, math.MaxInt32}},
		{{0, 10, 5}, {math.MinInt32, math.MinInt32, math.MaxInt32}},
		{{0, 39, 39, 0}, {math.MinInt32, math.MinInt32, 29, 29}},
	} {
		xs, ys := gfxReviewIntArray(t, rt, polygon[0]...), gfxReviewIntArray(t, rt, polygon[1]...)
		finishesWithin(t, 5*time.Second, func() error {
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

// modelCopy is what a copy between surfaces should leave behind: each pixel
// that lands inside both surfaces and the clip takes the source pixel, read
// either before the copy began (a blit, which snapshots an overlapping
// source) or as the copy reaches it (MC_grpCopyFrameBuffer, which does not).
func modelCopy(destination, source []uint16, destinationWidth, destinationHeight, sourceWidth, sourceHeight int64, clip wipicrt.WIPICClip, destinationX, destinationY, width, height, sourceX, sourceY int64, live bool) []uint16 {
	result := slices.Clone(destination)
	read := slices.Clone(source)
	if live {
		read = result // one surface, read as the copy writes it
	}
	for line := int64(0); line < height; line++ {
		for column := int64(0); column < width; column++ {
			fromX, fromY := sourceX+column, sourceY+line
			toX, toY := destinationX+column, destinationY+line
			if fromX < 0 || fromY < 0 || fromX >= sourceWidth || fromY >= sourceHeight ||
				toX < 0 || toY < 0 || toX >= destinationWidth || toY >= destinationHeight {
				continue
			}
			if !clip.Contains(int32(toX), int32(toY)) {
				continue
			}
			result[toY*destinationWidth+toX] = read[fromY*sourceWidth+fromX]
		}
	}
	return result
}

// Blits and frame buffer copies draw the same pixels they always have for
// rectangles that stick out of either surface, inside one surface or between
// two, with and without a clip.
func TestWIPICCopiesKeepOrdinaryPixels(t *testing.T) {
	_, rt := newTestRuntime(t)
	sourceHandle, source := gfxReviewSurface(t, rt, 20, 15)
	destinationHandle, destination := gfxReviewSurface(t, rt, 25, 18)
	random := rand.New(rand.NewSource(5))
	for trial := 0; trial < 1200; trial++ {
		var words [3]uint32
		var clip wipicrt.WIPICClip
		if trial%3 == 0 {
			left, top := random.Intn(20), random.Intn(15)
			right, bottom := left+random.Intn(20), top+random.Intn(15)
			words = [3]uint32{1, uint32(left) | uint32(top)<<16, uint32(right) | uint32(bottom)<<16}
			clip = wipicrt.WIPICClip{Enabled: true, Left: int32(left), Top: int32(top), Right: int32(right), Bottom: int32(bottom)}
		}
		context := gfxReviewContext(t, rt, 0, words)
		from, fromHandle := source, sourceHandle
		to, toHandle := destination, destinationHandle
		if trial%2 == 0 {
			from, fromHandle = destination, destinationHandle
		}
		mode := trial % 3
		if mode == 2 {
			// MC_grpCopyArea copies within one surface.
			from, fromHandle = to, toHandle
		}
		fromPixels := randomSurfacePixels(random, from.Width*from.Height)
		writeSurfacePixels(t, rt, from, fromPixels)
		toPixels := fromPixels
		if toHandle != fromHandle {
			toPixels = randomSurfacePixels(random, to.Width*to.Height)
			writeSurfacePixels(t, rt, to, toPixels)
		}
		destinationX, destinationY := int32(random.Intn(60)-20), int32(random.Intn(50)-20)
		sourceX, sourceY := int32(random.Intn(60)-20), int32(random.Intn(50)-20)
		width, height := int32(random.Intn(60)-5), int32(random.Intn(50)-5)
		var err error
		switch mode {
		case 0:
			err = rt.WIPICBlitClipped(to, clip, wipicrt.WIPICPixelOp{}, destinationX, destinationY, width, height, from, sourceX, sourceY, wipicrt.BlitOpacity{})
		case 1:
			thread := gfxReviewThread(t, rt, toHandle, uint32(destinationX), uint32(destinationY), uint32(width), uint32(height), fromHandle, uint32(sourceX), uint32(sourceY), context)
			_, err = rt.WIPICCopyFramebuffer(thread)
		case 2:
			thread := gfxReviewThread(t, rt, toHandle, uint32(destinationX), uint32(destinationY), uint32(width), uint32(height), uint32(sourceX), uint32(sourceY), context)
			_, err = rt.WIPICCopyArea(thread)
		}
		if err != nil {
			t.Fatal(err)
		}
		want := modelCopy(toPixels, fromPixels, int64(to.Width), int64(to.Height), int64(from.Width), int64(from.Height), clip,
			int64(destinationX), int64(destinationY), int64(width), int64(height), int64(sourceX), int64(sourceY),
			mode == 1 && toHandle == fromHandle)
		if got := readSurfacePixels(t, rt, to); !slices.Equal(got, want) {
			t.Fatalf("mode %d copy %dx%d from (%d,%d) to (%d,%d) clip %+v drew different pixels", mode, width, height, sourceX, sourceY, destinationX, destinationY, clip)
		}
	}
}

// A copy the guest sized from garbage walked every one of its requested
// columns doing nothing, and an overlapping one inside a single surface
// allocated a host snapshot of the requested size before anything was clipped.
func TestWIPICCopiesWithGarbageSizesFinish(t *testing.T) {
	_, rt := newTestRuntime(t)
	const width, height = 40, 30
	sourceHandle, source := gfxReviewSurface(t, rt, width, height)
	destinationHandle, destination := gfxReviewSurface(t, rt, width, height)
	random := rand.New(rand.NewSource(6))
	sourcePixels := randomSurfacePixels(random, width*height)
	writeSurfacePixels(t, rt, source, sourcePixels)
	context := gfxReviewContext(t, rt, 0, [3]uint32{})

	finishesWithin(t, 5*time.Second, func() error {
		return rt.WIPICBlitClipped(destination, wipicrt.WIPICClip{}, wipicrt.WIPICPixelOp{}, 0, 0, math.MaxInt32, height, source, 0, 0, wipicrt.BlitOpacity{})
	})
	if got := readSurfacePixels(t, rt, destination); !slices.Equal(got, sourcePixels) {
		t.Fatal("a blit wider than both surfaces did not copy the whole surface")
	}
	finishesWithin(t, 5*time.Second, func() error {
		return rt.WIPICBlitClipped(destination, wipicrt.WIPICClip{}, wipicrt.WIPICPixelOp{}, math.MinInt32, 0, math.MaxInt32, height, source, math.MinInt32, 0, wipicrt.BlitOpacity{})
	})
	thread := gfxReviewThread(t, rt, destinationHandle, 0, 0, math.MaxInt32, math.MaxInt32, sourceHandle, 0, 0, context)
	finishesWithin(t, 5*time.Second, func() error {
		_, err := rt.WIPICCopyFramebuffer(thread)
		return err
	})

	shifted := randomSurfacePixels(random, width*height)
	writeSurfacePixels(t, rt, destination, shifted)
	var before, after goruntime.MemStats
	goruntime.ReadMemStats(&before)
	thread = gfxReviewThread(t, rt, destinationHandle, 0, 0, 0x10000, 0x4000, 1, 0, context)
	finishesWithin(t, 5*time.Second, func() error {
		_, err := rt.WIPICCopyArea(thread)
		return err
	})
	goruntime.ReadMemStats(&after)
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 16<<20 {
		t.Fatalf("an oversized copy area allocated %d bytes", grown)
	}
	want := slices.Clone(shifted)
	for y := 0; y < height; y++ {
		copy(want[y*width:y*width+width-1], shifted[y*width+1:y*width+width])
	}
	if got := readSurfacePixels(t, rt, destination); !slices.Equal(got, want) {
		t.Fatal("an oversized copy area did not shift the surface by one column")
	}
}

// referenceNativeDraw is the native blit as it ran before its loops started at
// the first row and column that can land.
func referenceNativeDraw(frame *image.RGBA, source *nativeImage, blit NativeBlit) {
	bounds := source.frame.Bounds()
	rows := min(blit.Height, min(bounds.Max.Y-blit.SourceY, frame.Bounds().Max.Y-blit.Y))
	columns := min(blit.Width, min(bounds.Max.X-blit.SourceX, frame.Bounds().Max.X-blit.X))
	for row := 0; row < rows; row++ {
		sourceY, targetY := blit.SourceY+row, blit.Y+row
		if sourceY < bounds.Min.Y || targetY < 0 {
			continue
		}
		for column := 0; column < columns; column++ {
			sourceX, targetX := blit.SourceX+column, blit.X+column
			if sourceX < bounds.Min.X || targetX < 0 {
				continue
			}
			pixel := source.frame.RGBAAt(sourceX, sourceY)
			if pixel == nativeTransparent {
				continue
			}
			frame.SetRGBA(targetX, targetY, pixel)
		}
	}
}

func randomNativeFrame(random *rand.Rand, bounds image.Rectangle) *image.RGBA {
	frame := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.RGBA{R: uint8(random.Intn(256)), G: uint8(random.Intn(256)), B: uint8(random.Intn(256)), A: 0xff}
			if random.Intn(4) == 0 {
				pixel = nativeTransparent
			}
			frame.SetRGBA(x, y, pixel)
		}
	}
	return frame
}

func TestNativeBlitKeepsOrdinaryPixels(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	for trial := 0; trial < 3000; trial++ {
		sourceBounds := image.Rect(0, 0, 16, 12)
		if trial%2 == 0 {
			sourceBounds = image.Rect(2, 3, 18, 15)
		}
		source := &nativeImage{frame: randomNativeFrame(random, sourceBounds)}
		start := randomNativeFrame(random, image.Rect(0, 0, 30, 20))
		platform := &NativePlatform{screen: &nativeScreen{frame: image.NewRGBA(start.Rect)}}
		copy(platform.screen.frame.Pix, start.Pix)
		blit := NativeBlit{
			X: random.Intn(60) - 25, Y: random.Intn(40) - 15,
			Width: random.Intn(50) - 5, Height: random.Intn(40) - 5,
			SourceX: random.Intn(40) - 15, SourceY: random.Intn(30) - 10,
		}
		platform.drawImage(source, blit)
		referenceNativeDraw(start, source, blit)
		if !slices.Equal(platform.screen.frame.Pix, start.Pix) {
			t.Fatalf("native blit %+v drew different pixels", blit)
		}
	}
}

// A blit whose target and source positions are both far negative was bounded
// by nothing but its own width, and skipped the off-screen part one turn at a
// time.
func TestNativeBlitFarNegativePositionsFinish(t *testing.T) {
	random := rand.New(rand.NewSource(8))
	source := &nativeImage{frame: randomNativeFrame(random, image.Rect(0, 0, 16, 16))}
	platform := &NativePlatform{screen: &nativeScreen{frame: image.NewRGBA(image.Rect(0, 0, 40, 30))}}
	for _, blit := range []NativeBlit{
		{X: math.MinInt32, SourceX: math.MinInt32, Width: math.MaxInt32, Height: 50},
		{Y: math.MinInt32, SourceY: math.MinInt32, Width: 50, Height: math.MaxInt32},
		{X: -math.MaxInt32, Y: -math.MaxInt32, SourceX: -math.MaxInt32, SourceY: -math.MaxInt32, Width: math.MaxInt32, Height: math.MaxInt32},
	} {
		finishesWithin(t, 5*time.Second, func() error {
			platform.drawImage(source, blit)
			return nil
		})
	}
}

// A frame buffer record is read back from guest memory, and a width of 2^31 or
// more once passed the bytes-per-line check because width*2 wrapped.
func TestWIPICSurfaceRejectsAWidthThatWrapsTheLineCheck(t *testing.T) {
	_, rt := newTestRuntime(t)
	handle, _ := gfxReviewSurface(t, rt, 4, 1)
	fields := handle + wipicrt.WIPICAllocationOverhead
	writeGuestWords(t, rt, fields, 0x80000001, 1, 2)
	if framebuffer, err := rt.ReadWIPICFramebuffer(handle); err == nil {
		t.Fatalf("accepted a %d-wide frame buffer with %d bytes per line", framebuffer.Width, framebuffer.Bpl)
	}
	// The same record with a width its line holds still reads.
	writeGuestWords(t, rt, fields, 1, 1, 2)
	if _, err := rt.ReadWIPICFramebuffer(handle); err != nil {
		t.Fatal(err)
	}
}

// A draw-string length is a guest argument, and the host buffer it sized was
// allocated before the range was known to exist.
func TestWIPICDrawStringLengthIsCheckedBeforeAllocating(t *testing.T) {
	_, rt := newTestRuntime(t)
	pointer, err := rt.AllocateBytes([]byte{'a', 0, 'b', 0})
	if err != nil {
		t.Fatal(err)
	}
	var before, after goruntime.MemStats
	goruntime.ReadMemStats(&before)
	_, err = rt.WIPICReadString(pointer, math.MaxInt32, true)
	goruntime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("read a string running past mapped memory")
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 16<<20 {
		t.Fatalf("allocated %d bytes before checking the range", grown)
	}
	text, err := rt.WIPICReadString(pointer, 2, true)
	if err != nil || string(text) != "ab" {
		t.Fatalf("ordinary string = %q, %v", string(text), err)
	}
}

func orphanDecodedImageSurface(t *testing.T, rt *initializationRuntime) uint32 {
	t.Helper()
	value, err := runtimeImageFromEncoded(rt, encodePalettedPNG(t, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	handle, err := rt.imageFramebufferHandle(object)
	if err != nil {
		t.Fatal(err)
	}
	return handle
}

func orphanCopiedImageSurface(t *testing.T, rt *initializationRuntime, source jvm.Value, sub bool) uint32 {
	t.Helper()
	var value jvm.Value
	var err error
	if sub {
		value, err = runtimeImageCreateSubImage(rt, rt.client.vm, []jvm.Value{source, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(1), jvm.IntValue(1), jvm.IntValue(0)})
	} else {
		value, err = runtimeImageCreateCopy(rt, rt.client.vm, []jvm.Value{source})
	}
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	handle, err := object.Fields["guestFramebuffer:I"].Int32()
	if err != nil {
		t.Fatal(err)
	}
	return uint32(handle)
}

// The frame buffer a decoded image is rasterized into on first use, and the
// one createImage(Image) and createSubImage copy into, go back to the arena
// when the image dies, as createImage(int, int) always did.
func TestImageSurfacesMadeLazilyOrByCopyAreReclaimed(t *testing.T) {
	_, rt := newTestRuntime(t)
	for i := 0; i < 4; i++ {
		handle := orphanDecodedImageSurface(t, rt)
		goruntime.GC()
		rt.collectHostObjects()
		if _, exists := rt.WIPICAllocations[handle]; exists {
			t.Fatal("a dead decoded image kept the frame buffer it was rasterized into")
		}
	}
	source, err := runtimeImageFromEncoded(rt, encodePalettedPNG(t, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	sourceObject, _ := source.Reference()
	sourceHandle, err := rt.imageFramebufferHandle(sourceObject)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []bool{false, true} {
		for i := 0; i < 4; i++ {
			handle := orphanCopiedImageSurface(t, rt, source, sub)
			goruntime.GC()
			rt.collectHostObjects()
			if _, exists := rt.WIPICAllocations[handle]; exists {
				t.Fatalf("a dead copied image (sub-image %v) kept its frame buffer", sub)
			}
			if _, exists := rt.WIPICAllocations[sourceHandle]; !exists {
				t.Fatal("a live source image lost its frame buffer")
			}
		}
	}
	goruntime.KeepAlive(sourceObject)
}
