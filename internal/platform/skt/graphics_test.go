package skt

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
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
