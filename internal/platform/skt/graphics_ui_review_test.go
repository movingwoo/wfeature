package skt

import (
	"bytes"
	"errors"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Card.move and Card.resize land in the fields a Card subclass reads with
// getfield, under the interpreter's key and lock, and getX/getY agree.
func TestCardMoveAndResizeReachTheFieldsASubclassReads(t *testing.T) {
	session := startUIFixture(t)
	card := &jvm.Object{ClassName: wipi.CardClass}
	if _, err := session.wipiCardMove(session.VM, []jvm.Value{jvm.ReferenceValue(card), jvm.IntValue(3), jvm.IntValue(4)}); err != nil {
		t.Fatalf("move() error = %v", err)
	}
	if _, err := session.wipiCardResize(session.VM, []jvm.Value{jvm.ReferenceValue(card), jvm.IntValue(30), jvm.IntValue(40)}); err != nil {
		t.Fatalf("resize() error = %v", err)
	}
	for name, want := range map[string]int32{"x": 3, "y": 4, "w": 30, "h": 40} {
		value, err := session.VM.Field(card, wipi.CardClass, name, "I")
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := value.Int32(); got != want {
			t.Fatalf("getfield Card.%s = %d, want %d", name, got, want)
		}
	}
	value, err := session.VM.InvokeVirtual(card, "getX", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int32(); got != 3 {
		t.Fatalf("getX() = %d, want 3", got)
	}
}

// getRGBPixels reads into the int[] argument, not into the height before it.
func TestGetRGBPixelsFillsTheCallersArray(t *testing.T) {
	vm := jvm.New(nil, jvm.Options{})
	context := reviewContext(4, 4)
	for index := range context.pixels {
		context.pixels[index] = byte(index)
	}
	buffer, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, 4)
	if err != nil {
		t.Fatal(err)
	}
	session := &Runtime{}
	if _, err := session.getGraphicsRGBPixels(vm, rgbPixelArguments(context, 1, 1, 2, 2, buffer, 0, 2)); err != nil {
		t.Fatalf("getRGBPixels() error = %v", err)
	}
	_, values, err := jvm.ArraySnapshot(buffer)
	if err != nil {
		t.Fatal(err)
	}
	for index, point := range [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}} {
		pixel := (point[1]*4 + point[0]) * 4
		// 0x00RRGGBB, as the specification gives it: no alpha.
		want := int32(uint32(context.pixels[pixel])<<16 | uint32(context.pixels[pixel+1])<<8 | uint32(context.pixels[pixel+2]))
		if got, _ := values[index].Int32(); got != want {
			t.Fatalf("pixel %d = %#x, want %#x", index, got, want)
		}
	}
}

// getRGBPixels reads the screen under the lock a title's drawing takes, so a
// read on one thread while another draws is no data race. Run with -race.
func TestGetRGBPixelsReadsTheScreenUnderItsLock(t *testing.T) {
	vm := jvm.New(nil, jvm.Options{})
	context := reviewContext(8, 8)
	context.screen = &sync.Mutex{}
	context.color = 0x123456
	buffer, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, 64)
	if err != nil {
		t.Fatal(err)
	}
	session := &Runtime{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			context.withDestinationWrite(func() {
				for y := range int64(8) {
					for x := range int64(8) {
						context.putColorPixel(x, y)
					}
				}
			})
		}
	}()
	for range 200 {
		if _, err := session.getGraphicsRGBPixels(vm, rgbPixelArguments(context, 0, 0, 8, 8, buffer, 0, 8)); err != nil {
			t.Fatal(err)
		}
	}
	<-done
	_, values, err := jvm.ArraySnapshot(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := values[63].Int32(); got != 0x123456 {
		t.Fatalf("a drawn pixel reads %#x, want 0x123456 with no alpha", got)
	}
}

// A scan length shorter than the width overlaps the rows in the caller's
// array, so a tall rectangle names far more pixels than the array holds. The
// work is bounded by the array: a 2^16-wide, 2^16+1-tall read into a 2^17-int
// array writes each element once instead of 2^32 times.
func TestGetRGBPixelsWithOverlappingRowsIsBoundedByTheArray(t *testing.T) {
	vm := jvm.New(nil, jvm.Options{})
	session := &Runtime{}
	const length = 1 << 17
	buffer, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, length)
	if err != nil {
		t.Fatal(err)
	}
	for name, arguments := range map[string][]jvm.Value{
		"scan 1":  rgbPixelArguments(reviewContext(4, 4), 0, 0, length/2, length/2+1, buffer, 0, 1),
		"scan -1": rgbPixelArguments(reviewContext(4, 4), 0, 0, length/2, length/2+1, buffer, length/2, -1),
	} {
		done := make(chan error, 1)
		go func() {
			_, err := session.getGraphicsRGBPixels(vm, arguments)
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: getRGBPixels() error = %v", name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: getRGBPixels with overlapping rows did not return", name)
		}
	}
}

// Bounding the overlapping walk changes how often an element is written, not
// what it ends up holding: for every scan length, overlapping, gapped and
// negative, the array matches the row-by-row walk in which a later row
// overwrites an earlier one, and elements outside every row keep their value.
func TestGetRGBPixelsMatchesTheRowByRowWalk(t *testing.T) {
	vm := jvm.New(nil, jvm.Options{})
	session := &Runtime{}
	random := rand.New(rand.NewSource(11))
	overlapping, negative := 0, 0
	for round := 0; round < 2000; round++ {
		context := reviewClippedContext(random)
		x, y := int32(random.Intn(30)-10), int32(random.Intn(24)-8)
		width, height := int32(random.Intn(12)), int32(random.Intn(12))
		scan, offset := int32(random.Intn(31)-15), int32(random.Intn(80))
		length := int32(random.Intn(120))
		array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, length)
		if err != nil {
			t.Fatal(err)
		}
		fill := make([]jvm.Value, length)
		for index := range fill {
			fill[index] = jvm.IntValue(int32(random.Uint32()))
		}
		if err := jvm.SetArrayRange(array, 0, fill); err != nil {
			t.Fatal(err)
		}
		want := append([]jvm.Value(nil), fill...)
		_, err = session.getGraphicsRGBPixels(vm, rgbPixelArguments(context, x, y, width, height, array, offset, scan))
		if err != nil {
			assertGuestExceptionClass(t, err, "java/lang/ArrayIndexOutOfBoundsException")
		} else {
			if scan == 0 {
				scan = width
			}
			rowByRowGetRGBPixels(context, want, x, y, width, height, offset, scan)
			if height > 1 && scan > -width && scan < width {
				overlapping++
				if scan < 0 {
					negative++
				}
			}
		}
		_, got, snapshotErr := jvm.ArraySnapshot(array)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("round %d (x=%d y=%d w=%d h=%d offset=%d scan=%d length=%d): element %d = %v, want %v",
					round, x, y, width, height, offset, scan, length, index, got[index], want[index])
			}
		}
	}
	if overlapping < 100 || negative < 30 {
		t.Fatalf("only %d accepted calls had overlapping rows, %d of them scanning upwards", overlapping, negative)
	}
}

// rowByRowGetRGBPixels is getRGBPixels as every row in order, each writing
// its whole width; a refused call writes nothing, so the caller skips it.
func rowByRowGetRGBPixels(context *graphicsContext, values []jvm.Value, x, y, width, height, offset, scanLength int32) {
	for row := int32(0); row < height; row++ {
		for column := int32(0); column < width; column++ {
			pixel := jvm.IntValue(0)
			if index, ok := contextPixelIndexUnclipped(context, x+column, y+row); ok {
				pixel = jvm.IntValue(int32(uint32(context.pixels[index])<<16 |
					uint32(context.pixels[index+1])<<8 | uint32(context.pixels[index+2])))
			}
			values[int(offset)+int(row)*int(scanLength)+int(column)] = pixel
		}
	}
}

// A rectangle far larger than the caller's array is refused before anything
// the size of the rectangle is allocated.
func TestGetRGBPixelsRefusesASmallArrayBeforeAllocating(t *testing.T) {
	vm := jvm.New(nil, jvm.Options{})
	context := reviewContext(4, 4)
	buffer, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, 1)
	if err != nil {
		t.Fatal(err)
	}
	session := &Runtime{}
	allocated := allocatedDuring(func() {
		_, err = session.getGraphicsRGBPixels(vm, rgbPixelArguments(context, 0, 0, 3000, 3000, buffer, 0, 0))
	})
	assertGuestExceptionClass(t, err, "java/lang/ArrayIndexOutOfBoundsException")
	if allocated > 16<<20 {
		t.Fatalf("getRGBPixels allocated %d bytes before refusing", allocated)
	}
}

// captureLCD refuses a region over the image limit before allocating it.
func TestCaptureLCDRefusesAnOversizedRegionBeforeAllocating(t *testing.T) {
	session := startUIFixture(t)
	var err error
	allocated := allocatedDuring(func() {
		_, err = session.graphics2DCaptureLCD(session.VM, []jvm.Value{jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(5000), jvm.IntValue(5000)})
	})
	assertGuestExceptionClass(t, err, "java/lang/IllegalArgumentException")
	if allocated > 16<<20 {
		t.Fatalf("captureLCD allocated %d bytes before refusing", allocated)
	}
}

// A guest's huge width and height cost no more than the pixels that can
// actually be drawn, so the screen lock is not held for 2^62 iterations.
func TestBlitsWithHugeExtentsReturn(t *testing.T) {
	vm := jvm.New(nil, jvm.Options{})
	session := &Runtime{}
	huge := jvm.IntValue(0x7fffffff)
	values, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, 16)
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"Graphics2D.drawImage": func() error {
			context := reviewContext(8, 8)
			source := reviewImage(t, session, 4, 4, 1)
			_, err := session.graphics2DDrawImage(vm, []jvm.Value{jvm.ReferenceValue(graphics2DObject(context)),
				jvm.IntValue(0), jvm.IntValue(0), jvm.ReferenceValue(source), jvm.IntValue(0), jvm.IntValue(0), huge, huge, jvm.IntValue(0)})
			return err
		},
		"drawImageEx": func() error {
			context := reviewContext(8, 8)
			source := reviewImage(t, session, 4, 4, 1)
			_, err := session.xDisplayDrawImageEx(vm, []jvm.Value{jvm.ReferenceValue(graphicsObject(context)), jvm.ReferenceValue(nil),
				jvm.IntValue(0), jvm.IntValue(0), jvm.ReferenceValue(source), jvm.IntValue(0), jvm.IntValue(0), huge, huge, jvm.IntValue(0)})
			return err
		},
		"setRGBPixels": func() error {
			context := reviewContext(8, 8)
			_, err := session.setGraphicsRGBPixels(vm, rgbPixelArguments(context, 0, 0, 0x7fffffff, 0x7fffffff, values, 0, 0))
			return err
		},
	} {
		done := make(chan error, 1)
		go func() { done <- call() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s error = %v", name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s with a 2^31 extent did not return", name)
		}
	}
}

// Clamping the loops changes how many pixels are visited, not which ones are
// drawn: for ordinary coordinates, including partly outside the image, the
// clip and the surface, every pixel matches the unclamped walk.
func TestClampedBlitsDrawTheSamePixels(t *testing.T) {
	vm := jvm.New(nil, jvm.Options{})
	session := &Runtime{}
	random := rand.New(rand.NewSource(7))
	for round := 0; round < 300; round++ {
		x, y := int32(random.Intn(40)-20), int32(random.Intn(40)-20)
		sourceX, sourceY := int32(random.Intn(20)-10), int32(random.Intn(20)-10)
		width, height := int32(random.Intn(34)-2), int32(random.Intn(34)-2)
		mode := int32(random.Intn(4))
		source := reviewImage(t, session, 9, 7, int64(round))
		image := source.Native.(*imageData)

		got, want := reviewClippedContext(random), (*graphicsContext)(nil)
		want = cloneContext(got)
		if _, err := session.graphics2DDrawImage(vm, []jvm.Value{jvm.ReferenceValue(graphics2DObject(got)),
			jvm.IntValue(x), jvm.IntValue(y), jvm.ReferenceValue(source), jvm.IntValue(sourceX), jvm.IntValue(sourceY),
			jvm.IntValue(width), jvm.IntValue(height), jvm.IntValue(mode)}); err != nil {
			t.Fatal(err)
		}
		unclampedDrawImage2D(want, image, x, y, sourceX, sourceY, width, height, mode)
		if !bytes.Equal(got.pixels, want.pixels) {
			t.Fatalf("round %d: Graphics2D.drawImage pixels differ from the unclamped walk", round)
		}

		got = reviewClippedContext(random)
		want = cloneContext(got)
		if _, err := session.xDisplayDrawImageEx(vm, []jvm.Value{jvm.ReferenceValue(graphicsObject(got)), jvm.ReferenceValue(nil),
			jvm.IntValue(x), jvm.IntValue(y), jvm.ReferenceValue(source), jvm.IntValue(sourceX), jvm.IntValue(sourceY),
			jvm.IntValue(width), jvm.IntValue(height), jvm.IntValue(0)}); err != nil {
			t.Fatal(err)
		}
		unclampedDrawImageEx(want, image, x, y, sourceX, sourceY, width, height)
		if !bytes.Equal(got.pixels, want.pixels) {
			t.Fatalf("round %d: drawImageEx pixels differ from the unclamped walk", round)
		}

		length := int32(random.Intn(200))
		array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, length)
		if err != nil {
			t.Fatal(err)
		}
		fill := make([]jvm.Value, length)
		for index := range fill {
			fill[index] = jvm.IntValue(int32(random.Uint32()))
		}
		if err := jvm.SetArrayRange(array, 0, fill); err != nil {
			t.Fatal(err)
		}
		offset, scan := int32(random.Intn(60)-20), int32(random.Intn(30)-10)
		rgbWidth, rgbHeight := int32(random.Intn(30)), int32(random.Intn(30))
		got = reviewClippedContext(random)
		want = cloneContext(got)
		if _, err := session.setGraphicsRGBPixels(vm, rgbPixelArguments(got, x, y, rgbWidth, rgbHeight, array, offset, scan)); err != nil {
			t.Fatal(err)
		}
		if scan == 0 {
			scan = rgbWidth
		}
		unclampedSetRGBPixels(want, fill, x, y, rgbWidth, rgbHeight, offset, scan)
		if !bytes.Equal(got.pixels, want.pixels) {
			t.Fatalf("round %d: setRGBPixels pixels differ from the unclamped walk", round)
		}
	}
}

func unclampedDrawImage2D(context *graphicsContext, image *imageData, x, y, sourceX, sourceY, width, height, mode int32) {
	pixels := image.snapshot()
	for row := int32(0); row < height; row++ {
		for column := int32(0); column < width; column++ {
			sx, sy := sourceX+column, sourceY+row
			if sx < 0 || sy < 0 || int(sx) >= image.width || int(sy) >= image.height {
				continue
			}
			index, ok := contextPixelIndex(context, x+column, y+row)
			if !ok {
				continue
			}
			sourceIndex := (int(sy)*image.width + int(sx)) * 4
			if pixels[sourceIndex+3] == 0 && mode == skvmSourceCopy {
				continue
			}
			for channel := 0; channel < 3; channel++ {
				value := pixels[sourceIndex+channel]
				switch mode {
				case skvmSourceAnd:
					value &= context.pixels[index+channel]
				case skvmSourceOr:
					value |= context.pixels[index+channel]
				case skvmSourceXor:
					value ^= context.pixels[index+channel]
				}
				context.pixels[index+channel] = value
			}
			context.pixels[index+3] = 0xff
		}
	}
}

func unclampedDrawImageEx(context *graphicsContext, source *imageData, x, y, sourceX, sourceY, width, height int32) {
	pixels := source.snapshot()
	for row := int32(0); row < height; row++ {
		for column := int32(0); column < width; column++ {
			sx, sy := sourceX+column, sourceY+row
			if sx < 0 || sy < 0 || int(sx) >= source.width || int(sy) >= source.height {
				continue
			}
			index := (int(sy)*source.width + int(sx)) * 4
			context.blendPixel(int64(x+column), int64(y+row),
				pixels[index], pixels[index+1], pixels[index+2], pixels[index+3])
		}
	}
}

func unclampedSetRGBPixels(context *graphicsContext, values []jvm.Value, x, y, width, height, offset, scanLength int32) {
	for row := int32(0); row < height; row++ {
		for column := int32(0); column < width; column++ {
			source := int(offset) + int(row)*int(scanLength) + int(column)
			if source < 0 || source >= len(values) {
				continue
			}
			index, ok := contextPixelIndex(context, x+column, y+row)
			if !ok {
				continue
			}
			argb, valueErr := values[source].Int32()
			if valueErr != nil {
				continue
			}
			context.pixels[index] = byte(uint32(argb) >> 16)
			context.pixels[index+1] = byte(uint32(argb) >> 8)
			context.pixels[index+2] = byte(uint32(argb))
			context.pixels[index+3] = 0xff
		}
	}
}

func reviewContext(width, height int) *graphicsContext {
	full := paintRect{maxX: width, maxY: height}
	return &graphicsContext{
		pixels: make([]byte, width*height*4), width: width, height: height,
		deviceClip: full, clip: full, active: true,
	}
}

func reviewClippedContext(random *rand.Rand) *graphicsContext {
	context := reviewContext(16, 12)
	random.Read(context.pixels)
	context.clip = paintRect{minX: random.Intn(5), minY: random.Intn(4), maxX: 10 + random.Intn(7), maxY: 8 + random.Intn(5)}
	context.translateX, context.translateY = int32(random.Intn(7)-3), int32(random.Intn(7)-3)
	return context
}

func cloneContext(context *graphicsContext) *graphicsContext {
	clone := *context
	clone.pixels = append([]byte(nil), context.pixels...)
	return &clone
}

func reviewImage(t *testing.T, session *Runtime, width, height int, seed int64) *jvm.Object {
	t.Helper()
	pixels := make([]byte, width*height*4)
	rand.New(rand.NewSource(seed)).Read(pixels)
	for index := 3; index < len(pixels); index += 16 {
		pixels[index] = 0
	}
	image, err := session.newMIDPImage(width, height, false, pixels)
	if err != nil {
		t.Fatal(err)
	}
	return image
}

func graphicsObject(context *graphicsContext) *jvm.Object {
	return &jvm.Object{ClassName: midp.GraphicsClass, Native: context}
}

func graphics2DObject(context *graphicsContext) *jvm.Object {
	return &jvm.Object{ClassName: "com/skt/m/Graphics2D", Native: &graphics2DData{graphics: graphicsObject(context)}}
}

func rgbPixelArguments(context *graphicsContext, x, y, width, height int32, array *jvm.Object, offset, scan int32) []jvm.Value {
	return []jvm.Value{jvm.ReferenceValue(graphicsObject(context)), jvm.IntValue(x), jvm.IntValue(y),
		jvm.IntValue(width), jvm.IntValue(height), jvm.ReferenceValue(array), jvm.IntValue(offset), jvm.IntValue(scan)}
}

func allocatedDuring(call func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	call()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func assertGuestExceptionClass(t *testing.T, err error, class string) {
	t.Helper()
	var guest *jvm.GuestException
	if !errors.As(err, &guest) || guest.Object == nil || guest.Object.ClassName != class {
		t.Fatalf("error = %v, want %s", err, class)
	}
}
