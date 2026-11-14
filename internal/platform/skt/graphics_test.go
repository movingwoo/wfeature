package skt

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestTransformImageRegionCoversMIDPSpriteTransforms(t *testing.T) {
	source := make([]byte, 2*3*4)
	for index := 0; index < 6; index++ {
		source[index*4] = byte(index + 1)
		source[index*4+3] = 0xff
	}
	tests := []struct {
		transform int32
		width     int
		height    int
		want      []byte
	}{
		{transformNone, 2, 3, []byte{1, 2, 3, 4, 5, 6}},
		{transformMirrorRot180, 2, 3, []byte{5, 6, 3, 4, 1, 2}},
		{transformMirror, 2, 3, []byte{2, 1, 4, 3, 6, 5}},
		{transformRot180, 2, 3, []byte{6, 5, 4, 3, 2, 1}},
		{transformMirrorRot270, 3, 2, []byte{1, 3, 5, 2, 4, 6}},
		{transformRot90, 3, 2, []byte{5, 3, 1, 6, 4, 2}},
		{transformRot270, 3, 2, []byte{2, 4, 6, 1, 3, 5}},
		{transformMirrorRot90, 3, 2, []byte{6, 4, 2, 5, 3, 1}},
	}
	for _, test := range tests {
		pixels, width, height := transformImageRegion(source, 2, 0, 0, 2, 3, test.transform)
		if width != test.width || height != test.height {
			t.Errorf("transform %d dimensions = %dx%d, want %dx%d", test.transform, width, height, test.width, test.height)
		}
		got := make([]byte, len(test.want))
		for index := range got {
			got[index] = pixels[index*4]
		}
		if !bytes.Equal(got, test.want) {
			t.Errorf("transform %d red pixels = %v, want %v", test.transform, got, test.want)
		}
	}
}

func TestDecodeMIDPImagePreservesStraightAlpha(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	source.SetNRGBA(1, 0, color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0x20})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	object, err := (&Runtime{}).decodeMIDPImage(encoded.Bytes())
	if err != nil {
		t.Fatalf("decodeMIDPImage() error = %v", err)
	}
	decoded := object.Native.(*imageData)
	if got, want := decoded.snapshot(), []byte{0x12, 0x34, 0x56, 0xff, 0xaa, 0xbb, 0xcc, 0x20}; !bytes.Equal(got, want) {
		t.Fatalf("decoded RGBA = %v, want %v", got, want)
	}
}

func TestGraphicsAlphaBlendLeavesOpaqueDestination(t *testing.T) {
	context := &graphicsContext{
		pixels: []byte{0x00, 0x00, 0xff, 0xff},
		width:  1,
		height: 1,
		clip:   paintRect{maxX: 1, maxY: 1},
	}
	context.blendPixel(0, 0, 0xff, 0x00, 0x00, 0x80)
	if want := []byte{0x80, 0x00, 0x7f, 0xff}; !bytes.Equal(context.pixels, want) {
		t.Fatalf("blended RGBA = %v, want %v", context.pixels, want)
	}
}

// A game lays its menus out in the metrics this platform reports, so a glyph
// has to fit the line those metrics describe. Hangul comes from a face rather
// than from the authored 5x7 table, and drawing it on the 16-dot grid put a
// syllable eleven rows above a baseline the platform had placed eight rows
// into a ten-row line — three rows of it in the line above, which is what a
// local title's main menu showed.
func TestHangulFitsTheLineTheFontReports(t *testing.T) {
	font := newFontData(fontKey{size: fontMedium})

	const width, height = 16, 32
	// The baseline sits far enough down that anything drawn above it still
	// lands on the surface, so overflow shows as ink outside the line rather
	// than as ink clipped away.
	const top = 10
	pixels := make([]byte, width*height*4)
	context := &graphicsContext{pixels: pixels, width: width, height: height,
		clip: paintRect{maxX: width, maxY: height}, color: 0xffffff}
	font.render(context, []rune("한"), 0, top)

	highest, lowest := height, -1
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if pixels[(y*width+x)*4+3] == 0 {
				continue
			}
			if y < highest {
				highest = y
			}
			if y > lowest {
				lowest = y
			}
			break
		}
	}
	if lowest < 0 {
		t.Fatal("the syllable drew nothing")
	}
	if highest < top || lowest >= top+font.height {
		t.Fatalf("the syllable covers rows %d..%d, outside the %d-row line at %d",
			highest, lowest, font.height, top)
	}
}

// A font's metrics are the face's, so SMALL reports what MEDIUM does: the two
// draw the same glyphs from the one Korean face this platform has, and the
// only thing that ever separated them was the numbers they claimed.
func TestBitmapFontMetricsMatchRenderedAdvance(t *testing.T) {
	font := newFontData(fontKey{face: fontMonospace, style: fontBold | fontUnderlined, size: fontSmall})
	if font.height != pixelFace().Height() || font.baseline != pixelFace().Ascent {
		t.Fatalf("font metrics = height %d baseline %d, want the face's %d and %d",
			font.height, font.baseline, pixelFace().Height(), pixelFace().Ascent)
	}
	if font.charAdvance('A') != 7 || font.textWidth([]rune("AB")) != 14 {
		t.Fatalf("font advances = char %d text %d", font.charAdvance('A'), font.textWidth([]rune("AB")))
	}
	const width, height = 14, 16
	pixels := make([]byte, width*height*4)
	context := &graphicsContext{pixels: pixels, width: width, height: height, clip: paintRect{maxX: width, maxY: height}, color: 0xffffff}
	font.render(context, []rune("AB"), 0, 0)
	for x := 0; x < width; x++ {
		if pixels[(font.baseline*width+x)*4+3] != 0xff {
			t.Fatalf("underline alpha at x=%d = %d, want 255", x, pixels[(font.baseline*width+x)*4+3])
		}
	}
}

// Two lines stepped by the height the font reports must not touch. A menu that
// steps by Font.getHeight is the common layout in this corpus, and the line
// box has to hold the ink with room left over — a box that is exactly its own
// ink puts the next line's syllables against this one's.
func TestConsecutiveLinesAtTheReportedHeightDoNotTouch(t *testing.T) {
	font := newFontData(fontKey{size: fontMedium})

	const width, height = 16, 48
	pixels := make([]byte, width*height*4)
	context := &graphicsContext{pixels: pixels, width: width, height: height,
		clip: paintRect{maxX: width, maxY: height}, color: 0xffffff}
	const top = 8
	font.render(context, []rune("한"), 0, top)
	font.render(context, []rune("글"), 0, top+int64(font.height))

	lit := func(y int) bool {
		for x := 0; x < width; x++ {
			if pixels[(y*width+x)*4+3] != 0 {
				return true
			}
		}
		return false
	}
	blank := 0
	for y := top; y < top+2*font.height; y++ {
		if !lit(y) {
			blank++
		}
	}
	if blank == 0 {
		t.Fatal("the two lines run together with no blank row between them")
	}
}

// The screen Graphics is the one a title keeps and draws through from its own
// thread, and the Host copies the same bytes out to present a frame. Nothing
// held a lock across that pair — a context drawing into an Image locked the
// Image, and a context drawing on the screen had no destination to lock — so
// the two ran into each other on a real archive, which is what the race
// detector reported.
//
// This is that pair in miniature: it fails under `go test -race` when the
// screen context takes no lock, and passes when it takes the one the copier
// takes. Without the detector it only asserts that neither side crashes, which
// is why the race build is part of the standard checks.
func TestTheScreenContextLocksAgainstTheFrameCopy(t *testing.T) {
	const width, height = 8, 8
	var screen sync.Mutex
	pixels := make([]byte, width*height*4)
	context := &graphicsContext{
		pixels: pixels,
		width:  width,
		height: height,
		clip:   paintRect{maxX: width, maxY: height},
		color:  0x00ff00,
		active: true,
		screen: &screen,
	}

	var group sync.WaitGroup
	group.Add(2)
	// The guest thread's side: a drawing call, through the one seam every
	// drawing call goes through.
	go func() {
		defer group.Done()
		for round := 0; round < 200; round++ {
			context.withDestinationWrite(func() {
				context.fillClipped(paintRect{maxX: width, maxY: height})
			})
		}
	}()
	// The Host's side: the copy that becomes a frame.
	go func() {
		defer group.Done()
		for round := 0; round < 200; round++ {
			screen.Lock()
			_ = append([]byte(nil), pixels...)
			screen.Unlock()
		}
	}()
	group.Wait()
}

// **The screen Graphics' state is one caller's at a time.** A title keeps the
// screen Graphics and draws through it from its own thread, while a paint the
// runtime runs — the one a resume queues — resets the same object's clip and
// translation first. The race detector reported the two on every park of two
// local titles. This is that pair in miniature: a Graphics native from one
// goroutine and the reset from another. It fails under `go test -race` without
// screenStateMu.
func TestTheScreenGraphicsStateIsOneCallersAtATime(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 8, 8)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	whole := paintRect{maxX: 8, maxY: 8}
	graphics := runtime.screenGraphics(whole)

	var group sync.WaitGroup
	group.Add(2)
	failures := make(chan error, 2)
	// The title's thread: state set through the natives it calls.
	go func() {
		defer group.Done()
		for round := int32(0); round < 200; round++ {
			if _, err := runtime.VM.InvokeVirtual(graphics, "setClip", "(IIII)V",
				jvm.IntValue(round%4), jvm.IntValue(1), jvm.IntValue(4), jvm.IntValue(4)); err != nil {
				failures <- err
				return
			}
			if _, err := runtime.VM.InvokeVirtual(graphics, "translate", "(II)V", jvm.IntValue(1), jvm.IntValue(0)); err != nil {
				failures <- err
				return
			}
		}
	}()
	// The runtime's paint: the reset it starts from.
	go func() {
		defer group.Done()
		for round := 0; round < 200; round++ {
			runtime.screenGraphics(whole)
		}
	}()
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
}

// A thread parked on a sound is released when the program ends. A loop has no
// length of its own, so without this a title whose music nothing ever stops
// leaves a thread waiting on a program that is gone.
func TestEndingTheProgramReleasesThreadsWaitingOnASound(t *testing.T) {
	runtime := &Runtime{}
	clip := &audioClipData{}

	clip.mu.Lock()
	waiting := runtime.startPlaying(clip)
	clip.mu.Unlock()
	select {
	case <-waiting:
		t.Fatal("the wait ended while the clip was still sounding")
	default:
	}

	runtime.transition("test", StateDestroyed)
	select {
	case <-waiting:
	default:
		t.Fatal("a thread waiting on a sound was left waiting by the end of the program")
	}

	// The clip's own stop after that is not a second close of the same
	// channel, which would be a panic rather than a leak.
	clip.mu.Lock()
	runtime.endPlaying(clip)
	clip.mu.Unlock()
}

// getRGBPixels reads into the int[] argument, not into the height before it.
func TestGetRGBPixelsFillsTheCallersArray(t *testing.T) {
	vm := jvm.New(nil, jvm.Options{})
	context := graphicsTestContext(4, 4)
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
	context := graphicsTestContext(8, 8)
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
	// The reads above may all come before the first drawing; this one follows
	// the last.
	if _, err := session.getGraphicsRGBPixels(vm, rgbPixelArguments(context, 0, 0, 8, 8, buffer, 0, 8)); err != nil {
		t.Fatal(err)
	}
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
		"scan 1":  rgbPixelArguments(graphicsTestContext(4, 4), 0, 0, length/2, length/2+1, buffer, 0, 1),
		"scan -1": rgbPixelArguments(graphicsTestContext(4, 4), 0, 0, length/2, length/2+1, buffer, length/2, -1),
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
		context := clippedGraphicsTestContext(random)
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
	context := graphicsTestContext(4, 4)
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
			context := graphicsTestContext(8, 8)
			source := graphicsTestImage(t, session, 4, 4, 1)
			_, err := session.graphics2DDrawImage(vm, []jvm.Value{jvm.ReferenceValue(graphics2DObject(context)),
				jvm.IntValue(0), jvm.IntValue(0), jvm.ReferenceValue(source), jvm.IntValue(0), jvm.IntValue(0), huge, huge, jvm.IntValue(0)})
			return err
		},
		"drawImageEx": func() error {
			context := graphicsTestContext(8, 8)
			source := graphicsTestImage(t, session, 4, 4, 1)
			_, err := session.xDisplayDrawImageEx(vm, []jvm.Value{jvm.ReferenceValue(graphicsObject(context)), jvm.ReferenceValue(nil),
				jvm.IntValue(0), jvm.IntValue(0), jvm.ReferenceValue(source), jvm.IntValue(0), jvm.IntValue(0), huge, huge, jvm.IntValue(0)})
			return err
		},
		"setRGBPixels": func() error {
			context := graphicsTestContext(8, 8)
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
		source := graphicsTestImage(t, session, 9, 7, int64(round))
		image := source.Native.(*imageData)

		got, want := clippedGraphicsTestContext(random), (*graphicsContext)(nil)
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

		got = clippedGraphicsTestContext(random)
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
		got = clippedGraphicsTestContext(random)
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

func graphicsTestContext(width, height int) *graphicsContext {
	full := paintRect{maxX: width, maxY: height}
	return &graphicsContext{
		pixels: make([]byte, width*height*4), width: width, height: height,
		deviceClip: full, clip: full, active: true,
	}
}

func clippedGraphicsTestContext(random *rand.Rand) *graphicsContext {
	context := graphicsTestContext(16, 12)
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

func graphicsTestImage(t *testing.T, session *Runtime, width, height int, seed int64) *jvm.Object {
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
