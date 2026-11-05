package skt

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// wipiGraphicsMembersJAR is a Jlet whose card draws with the WIPI Graphics
// members MIDP does not have and records what the getters answered; see the
// fixture README.
//
//go:embed testdata/wipi-graphics-members.jar
var wipiGraphicsMembersJAR []byte

const wipiGraphicsMembersClass = "WIPIGraphicsMembersJlet"

// The ground the fixture's card fills its screen with, and what XOR white
// makes of it.
const (
	membersGround   = 0x2040c0
	membersInverted = membersGround ^ 0xffffff
	membersWidth    = 48
	membersHeight   = 32
)

func startWIPIGraphicsMembers(t *testing.T, store backend.SaveStore) (*Runtime, *backend.MemoryFramebuffer) {
	t.Helper()
	archive, err := Open(wipiGraphicsMembersJAR)
	if err != nil {
		t.Fatal(err)
	}
	framebuffer := newTestFramebuffer(t, membersWidth, membersHeight)
	runtime, err := Start(archive, Options{Framebuffer: framebuffer, SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	if !runtime.jlet {
		t.Fatal("the fixture's main class was not taken for a Jlet")
	}
	if wipiGraphicsMembersResults(t, runtime)[0] == 0 {
		if err := runtime.RunPending(); err != nil {
			t.Fatal(err)
		}
	}
	if wipiGraphicsMembersResults(t, runtime)[0] == 0 {
		t.Fatal("the fixture's card never painted")
	}
	return runtime, framebuffer
}

func wipiGraphicsMembersResults(t *testing.T, runtime *Runtime) []int32 {
	t.Helper()
	value, err := runtime.VM.StaticField(wipiGraphicsMembersClass, "RESULTS", "[I")
	if err != nil {
		t.Fatal(err)
	}
	array, err := value.Reference()
	if err != nil || array == nil {
		t.Fatalf("RESULTS = %v, %v", array, err)
	}
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	results := make([]int32, len(values))
	for index, value := range values {
		if results[index], err = value.Int32(); err != nil {
			t.Fatal(err)
		}
	}
	return results
}

func frameRGB(frame backend.Frame, x, y int) uint32 {
	index := (y*frame.Width + x) * 4
	return uint32(frame.RGBA[index])<<16 | uint32(frame.RGBA[index+1])<<8 | uint32(frame.RGBA[index+2])
}

// A Jlet compiled against the WIPI signatures reaches every member through the
// class it was compiled against, and the frame its card draws is the one KTF
// would draw for the same calls — except where the comments in
// internal/api/wipi/graphics.go say otherwise, which this checks too: an XOR
// outline inverts its corners once, and a copy's mask is white. drawChar, the
// drawImage with a source rectangle and reset reach XOR mode and the stroke
// style through bytecode as the other calls do.
func TestAPackagedJletDrawsWithTheWIPIGraphicsMembers(t *testing.T) {
	runtime, framebuffer := startWIPIGraphicsMembers(t, nil)
	results := wipiGraphicsMembersResults(t, runtime)
	for _, want := range []struct {
		name  string
		index int
		value int32
	}{
		{"isXORMode on a new Graphics", 1, 0},
		{"isXORMode after setXORMode(true)", 2, 1},
		{"isXORMode after setXORMode(false)", 3, 0},
		{"getRedComponent of 0x123456", 4, 0x12},
		{"getGreenComponent of 0x123456", 5, 0x34},
		{"getBlueComponent of 0x123456", 6, 0x56},
		{"getStrokeStyle on a new Graphics", 7, wipi.StrokeSolid},
		{"getStrokeStyle after setStrokeStyle(DOTTED)", 8, wipi.StrokeDotted},
		{"getPixel of a pixel set green", 9, 0x00ff00},
		{"getPixel of a translated pixel set blue", 10, 0x0000ff},
		{"getPixel left of the surface", 11, 0},
		{"getPixel right of the surface", 12, 0},
		{"isMutable of an image's copy", 13, 1},
		{"getWidth of the copy", 14, 4},
		{"getHeight of the copy", 15, 4},
		{"drawPolygon of null throws NullPointerException", 16, 1},
		{"createImage of null throws NullPointerException", 17, 1},
		{"isXORMode after reset", 18, 0},
		{"getStrokeStyle after reset", 19, wipi.StrokeSolid},
		{"getTranslateX after reset", 20, 0},
	} {
		if got := results[want.index]; got != want.value {
			t.Errorf("%s = %d (%#x), want %d (%#x)", want.name, got, got, want.value, want.value)
		}
	}

	frame, _ := framebuffer.Snapshot()
	for _, want := range []struct {
		name string
		x, y int
		rgb  uint32
	}{
		{"an XOR fill in white", 1, 1, membersInverted},
		{"two XOR fills in white", 5, 1, membersGround},
		{"an XOR fill in black", 9, 1, membersGround},
		{"a dotted line's first pixel", 0, 5, 0xff0000},
		{"a dotted line's second pixel", 1, 5, 0xff0000},
		{"a dotted line's last pixel", 7, 5, 0xff0000},
		{"setPixel", 10, 5, 0x00ff00},
		{"a translated setPixel", 12, 4, 0x0000ff},
		{"the polygon's top edge", 3, 8, 0xffff00},
		{"the polygon's slanted edge", 3, 11, 0xffff00},
		{"the polygon's closing edge", 0, 11, 0xffff00},
		{"the polygon's last vertex", 0, 14, 0xffff00},
		{"inside the polygon", 2, 10, membersGround},
		{"the marker", 10, 8, 0xff00ff},
		{"the copied marker", 14, 8, 0xff00ff},
		{"the copied marker's far corner", 15, 9, 0xff00ff},
		{"beside the copied marker", 16, 8, membersGround},
		{"an overlapping copy's source column", 10, 11, 0xffffff},
		{"an overlapping copy's first column", 11, 11, 0xffffff},
		{"an overlapping copy's second column", 12, 11, 0x000000},
		{"an overlapping copy's last column", 14, 11, 0x000000},
		{"past an overlapping copy", 15, 11, membersGround},
		{"an XOR copy's source", 10, 13, 0xff0000},
		{"an XOR copy", 12, 13, 0xff00ff},
		{"the copy drawn on", 0, 16, 0xff0000},
		{"the copy", 1, 16, 0x00ffff},
		{"the copy's far corner", 3, 19, 0x00ffff},
		{"the copy in XOR mode where it was drawn on", 4, 16, membersGround ^ 0xff0000},
		{"the copy in XOR mode", 5, 16, membersGround ^ 0x00ffff},
		{"the copy in XOR mode, far corner", 7, 19, membersGround ^ 0x00ffff},
		{"an XOR outline's corner", 10, 16, membersInverted},
		{"an XOR outline's far corner", 13, 19, membersInverted},
		{"an XOR outline's edge", 11, 16, membersInverted},
		{"inside an XOR outline", 11, 17, membersGround},
		{"an XOR line's first pixel", 16, 16, membersInverted},
		{"an XOR line's last pixel", 19, 19, membersInverted},
		{"beside an XOR line", 17, 16, membersGround},
		{"a masked picture's copy where it is transparent", 24, 16, 0xffffff},
		{"a masked picture's copy where it is drawn", 25, 16, 0xff0000},
		{"a masked picture in XOR mode where it is transparent", 24, 18, membersGround},
		{"a masked picture in XOR mode where it is drawn", 25, 18, membersGround ^ 0xff0000},
		{"a source rectangle in XOR mode", 0, 26, membersGround ^ 0x00ffff},
		{"a source rectangle in XOR mode, from column 1 of the copy", 1, 27, membersGround ^ 0x00ffff},
		{"beside a source rectangle", 2, 26, membersGround},
		{"a source rectangle twice in XOR mode", 4, 26, membersGround},
		{"a source rectangle twice in XOR mode, far corner", 5, 27, membersGround},
	} {
		if got := frameRGB(frame, want.x, want.y); got != want.rgb {
			t.Errorf("%s at (%d, %d) = %#06x, want %#06x", want.name, want.x, want.y, got, want.rgb)
		}
	}

	// A character drawn twice in XOR mode is gone, and one drawn once is
	// inverted wherever the face draws it.
	inverted := 0
	for y := 0; y < membersHeight; y++ {
		for x := 34; x < membersWidth; x++ {
			got := frameRGB(frame, x, y)
			switch {
			case y < 16 && got != membersGround:
				t.Errorf("a character drawn twice in XOR mode left (%d, %d) at %#06x", x, y, got)
			case y >= 16 && got == membersInverted:
				inverted++
			case y >= 16 && got != membersGround:
				t.Errorf("a character drawn once in XOR mode left (%d, %d) at %#06x, neither the ground nor its inverse", x, y, got)
			}
		}
	}
	if inverted < 10 {
		t.Errorf("a character drawn once in XOR mode inverted %d pixels, want its glyph", inverted)
	}
}

// membersGraphics answers a new image of the session and its Graphics, filled
// with the ground, for a test that draws on the real surface itself.
func membersGraphics(t *testing.T, runtime *Runtime, width, height int32) (*jvm.Object, *jvm.Object) {
	t.Helper()
	value, err := runtime.VM.InvokeStatic(wipi.ImageClass, "createImage", "(II)Lorg/kwis/msp/lcdui/Image;", jvm.IntValue(width), jvm.IntValue(height))
	if err != nil {
		t.Fatal(err)
	}
	picture, _ := value.Reference()
	value, err = runtime.VM.InvokeVirtual(picture, "getGraphics", "()Lorg/kwis/msp/lcdui/Graphics;")
	if err != nil {
		t.Fatal(err)
	}
	graphics, _ := value.Reference()
	if graphics == nil || graphics.ClassName != wipi.GraphicsClass {
		t.Fatalf("a Jlet session's image Graphics is %v", graphics)
	}
	membersCall(t, runtime, graphics, "setColor", "(I)V", jvm.IntValue(membersGround))
	membersCall(t, runtime, graphics, "fillRect", "(IIII)V", jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(width), jvm.IntValue(height))
	return picture, graphics
}

func membersCall(t *testing.T, runtime *Runtime, receiver *jvm.Object, name, descriptor string, arguments ...jvm.Value) jvm.Value {
	t.Helper()
	value, err := runtime.VM.InvokeVirtual(receiver, name, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s%s: %v", name, descriptor, err)
	}
	return value
}

func membersPixels(t *testing.T, picture *jvm.Object) []uint32 {
	t.Helper()
	data := picture.Native.(*imageData)
	pixels := make([]uint32, data.width*data.height)
	rgba := data.snapshot()
	for index := range pixels {
		pixels[index] = uint32(rgba[index*4])<<16 | uint32(rgba[index*4+1])<<8 | uint32(rgba[index*4+2])
	}
	return pixels
}

// Text in XOR mode — a string or one character — inverts exactly the pixels
// the same call draws outside it: the glyphs come from this platform's own
// face, through its anchor, and a second pass puts the ground back. The text
// is translated and partly outside the clip, so both reach the composition
// too.
func TestWIPIXORTextInvertsTheGlyphsTheFaceDraws(t *testing.T) {
	runtime, _ := startWIPIGraphicsMembers(t, nil)
	const ink = 0xffd000
	for _, text := range []struct {
		name, descriptor string
		subject          func() jvm.Value
	}{
		{"drawString", "(Ljava/lang/String;III)V", func() jvm.Value { return jvm.ReferenceValue(runtime.VM.NewString("Ag한")) }},
		{"drawChar", "(CIII)V", func() jvm.Value { return jvm.IntValue('한') }},
	} {
		t.Run(text.name, func(t *testing.T) {
			draw := func(graphics *jvm.Object) {
				membersCall(t, runtime, graphics, "translate", "(II)V", jvm.IntValue(3), jvm.IntValue(2))
				membersCall(t, runtime, graphics, "setClip", "(IIII)V", jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(30), jvm.IntValue(9))
				membersCall(t, runtime, graphics, "setColor", "(I)V", jvm.IntValue(ink))
				membersCall(t, runtime, graphics, text.name, text.descriptor,
					text.subject(), jvm.IntValue(2), jvm.IntValue(1), jvm.IntValue(16|4))
				membersCall(t, runtime, graphics, "translate", "(II)V", jvm.IntValue(-3), jvm.IntValue(-2))
			}
			plainPicture, plain := membersGraphics(t, runtime, 40, 24)
			draw(plain)
			xorPicture, inverted := membersGraphics(t, runtime, 40, 24)
			membersCall(t, runtime, inverted, "setXORMode", "(Z)V", jvm.IntValue(1))
			draw(inverted)

			reference, got := membersPixels(t, plainPicture), membersPixels(t, xorPicture)
			glyphs := 0
			for index := range reference {
				want := uint32(membersGround)
				if reference[index] == ink {
					want = membersGround ^ ink
					glyphs++
				}
				if got[index] != want {
					t.Fatalf("pixel (%d, %d) = %#06x, want %#06x", index%40, index/40, got[index], want)
				}
			}
			if glyphs < 10 {
				t.Fatalf("the text drew %d pixels, too few to say anything", glyphs)
			}
			draw(inverted)
			for index, pixel := range membersPixels(t, xorPicture) {
				if pixel != membersGround {
					t.Fatalf("a second XOR pass left (%d, %d) at %#06x, want the ground", index%40, index/40, pixel)
				}
			}
			// An anchor the platform refuses is refused in XOR mode too, even
			// where the text could not have reached the clip.
			if _, err := runtime.VM.InvokeVirtual(inverted, text.name, text.descriptor,
				text.subject(), jvm.IntValue(-1000), jvm.IntValue(-1000), jvm.IntValue(2)); !runtime.VM.IsGuestException(err, "java/lang/IllegalArgumentException") {
				t.Fatalf("an XOR %s with a vertical-centre anchor = %v, want IllegalArgumentException", text.name, err)
			}
		})
	}
}

// The WIPI blit with a source rectangle draws what MIDP's drawRegion draws
// with the two halves swapped and no transform, its anchor unread — the call
// this platform has always answered it with. In XOR mode it inverts what it
// draws, and a picture drawn onto itself is refused in both modes.
func TestWIPIDrawImageWithASourceRectangle(t *testing.T) {
	runtime, _ := startWIPIGraphicsMembers(t, nil)
	sourcePicture, source := membersGraphics(t, runtime, 5, 4)
	for y := int32(0); y < 4; y++ {
		for x := int32(0); x < 5; x++ {
			membersCall(t, runtime, source, "setColor", "(I)V", jvm.IntValue(0x300000*x+0x003000*y+0x60))
			membersCall(t, runtime, source, "setPixel", "(II)V", jvm.IntValue(x), jvm.IntValue(y))
		}
	}
	region := func(graphics *jvm.Object, wipiCall bool) {
		if wipiCall {
			// HCENTER | VCENTER, which a drawRegion would read and this does not.
			membersCall(t, runtime, graphics, "drawImage", "(Lorg/kwis/msp/lcdui/Image;IIIIIII)V", jvm.ReferenceValue(sourcePicture),
				jvm.IntValue(2), jvm.IntValue(1), jvm.IntValue(1), jvm.IntValue(2), jvm.IntValue(3), jvm.IntValue(2), jvm.IntValue(1|2))
			return
		}
		if _, err := runtime.VM.InvokeSpecial(graphics, "javax/microedition/lcdui/Graphics", "drawRegion", "(Ljavax/microedition/lcdui/Image;IIIIIIII)V",
			jvm.ReferenceValue(sourcePicture), jvm.IntValue(1), jvm.IntValue(2), jvm.IntValue(3), jvm.IntValue(2),
			jvm.IntValue(0), jvm.IntValue(2), jvm.IntValue(1), jvm.IntValue(0)); err != nil {
			t.Fatal(err)
		}
	}
	directPicture, direct := membersGraphics(t, runtime, 8, 5)
	region(direct, false)
	layeredPicture, layered := membersGraphics(t, runtime, 8, 5)
	region(layered, true)
	xorPicture, inverted := membersGraphics(t, runtime, 8, 5)
	membersCall(t, runtime, inverted, "setXORMode", "(Z)V", jvm.IntValue(1))
	region(inverted, true)
	want, plainGot, xorGot := membersPixels(t, directPicture), membersPixels(t, layeredPicture), membersPixels(t, xorPicture)
	drawn := 0
	for index := range want {
		if plainGot[index] != want[index] {
			t.Fatalf("pixel (%d, %d) = %#06x, drawRegion draws %#06x", index%8, index/8, plainGot[index], want[index])
		}
		inverse := uint32(membersGround)
		if want[index] != membersGround {
			inverse, drawn = membersGround^want[index], drawn+1
		}
		if xorGot[index] != inverse {
			t.Fatalf("XOR pixel (%d, %d) = %#06x, want %#06x", index%8, index/8, xorGot[index], inverse)
		}
	}
	if drawn != 6 {
		t.Fatalf("the rectangle drew %d pixels, want its 3x2", drawn)
	}
	for _, xor := range []int32{0, 1} {
		membersCall(t, runtime, source, "setXORMode", "(Z)V", jvm.IntValue(xor))
		before := membersPixels(t, sourcePicture)
		_, err := runtime.VM.InvokeVirtual(source, "drawImage", "(Lorg/kwis/msp/lcdui/Image;IIIIIII)V", jvm.ReferenceValue(sourcePicture),
			jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(2), jvm.IntValue(2), jvm.IntValue(0))
		if !runtime.VM.IsGuestException(err, "java/lang/IllegalArgumentException") {
			t.Fatalf("a picture drawn onto itself (XOR mode %d) = %v, want IllegalArgumentException", xor, err)
		}
		for index, pixel := range membersPixels(t, sourcePicture) {
			if pixel != before[index] {
				t.Fatalf("a refused blit onto itself drew (XOR mode %d)", xor)
			}
		}
	}
}

// reset on this platform puts back the clip, the translation and the font, and
// now XOR mode and the stroke style as well, as KTF's reset does.
func TestWIPIResetPutsBackXORModeAndStrokeStyle(t *testing.T) {
	runtime, _ := startWIPIGraphicsMembers(t, nil)
	picture, graphics := membersGraphics(t, runtime, 6, 4)
	membersCall(t, runtime, graphics, "translate", "(II)V", jvm.IntValue(2), jvm.IntValue(1))
	membersCall(t, runtime, graphics, "setClip", "(IIII)V", jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(1), jvm.IntValue(1))
	membersCall(t, runtime, graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
	membersCall(t, runtime, graphics, "setStrokeStyle", "(I)V", jvm.IntValue(wipi.StrokeDotted))
	membersCall(t, runtime, graphics, "reset", "()V")
	for _, want := range []struct {
		name, descriptor string
		value            int32
	}{
		{"isXORMode", "()Z", 0}, {"getStrokeStyle", "()I", wipi.StrokeSolid},
		{"getTranslateX", "()I", 0}, {"getTranslateY", "()I", 0},
		{"getClipWidth", "()I", 6}, {"getClipHeight", "()I", 4},
	} {
		if got, _ := membersCall(t, runtime, graphics, want.name, want.descriptor).Int32(); got != want.value {
			t.Errorf("%s after reset = %d, want %d", want.name, got, want.value)
		}
	}
	membersCall(t, runtime, graphics, "setColor", "(I)V", jvm.IntValue(0xffffff))
	membersCall(t, runtime, graphics, "fillRect", "(IIII)V", jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(6), jvm.IntValue(4))
	for index, pixel := range membersPixels(t, picture) {
		if pixel != 0xffffff {
			t.Fatalf("a fill after reset left (%d, %d) at %#06x, want white", index%6, index/6, pixel)
		}
	}
}

// The screen Graphics is one object for the session, and a paint starts it
// with XOR mode off and the solid stroke, as a new Graphics would be, rather
// than with what the paint before left set.
func TestWIPIPaintStartsWithoutXORModeOrAStrokeStyle(t *testing.T) {
	runtime, _ := startWIPIGraphicsMembers(t, nil)
	first := runtime.screenGraphics(paintRect{maxX: 4, maxY: 4})
	membersCall(t, runtime, first, "setXORMode", "(Z)V", jvm.IntValue(1))
	membersCall(t, runtime, first, "setStrokeStyle", "(I)V", jvm.IntValue(5))
	second := runtime.screenGraphics(paintRect{maxX: 4, maxY: 4})
	if second != first {
		t.Fatal("the screen Graphics is a new object; this test is about the reused one")
	}
	if got, _ := membersCall(t, runtime, second, "isXORMode", "()Z").Int32(); got != 0 {
		t.Errorf("isXORMode at the next paint = %d, want 0", got)
	}
	if got, _ := membersCall(t, runtime, second, "getStrokeStyle", "()I").Int32(); got != wipi.StrokeSolid {
		t.Errorf("getStrokeStyle at the next paint = %d, want SOLID", got)
	}
}

// An XOR arc, a filled round rectangle and a line each invert what the same
// call draws outside XOR mode, through the platform's own curve walk.
func TestWIPIXORCurvesInvertWhatTheyDraw(t *testing.T) {
	runtime, _ := startWIPIGraphicsMembers(t, nil)
	for _, shape := range []struct {
		name, descriptor string
		arguments        []int32
	}{
		{"drawArc", "(IIIIII)V", []int32{2, 3, 20, 14, 30, 250}},
		{"fillArc", "(IIIIII)V", []int32{-4, -2, 20, 14, 0, 300}},
		{"drawRoundRect", "(IIIIII)V", []int32{1, 1, 22, 15, 8, 6}},
		{"fillRoundRect", "(IIIIII)V", []int32{5, 4, 30, 30, 10, 10}},
		{"drawLine", "(IIII)V", []int32{-5, 20, 35, 2}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			arguments := make([]jvm.Value, len(shape.arguments))
			for index, value := range shape.arguments {
				arguments[index] = jvm.IntValue(value)
			}
			const ink = 0x13579b
			plainPicture, plain := membersGraphics(t, runtime, 24, 18)
			membersCall(t, runtime, plain, "setColor", "(I)V", jvm.IntValue(ink))
			membersCall(t, runtime, plain, shape.name, shape.descriptor, arguments...)
			xorPicture, inverted := membersGraphics(t, runtime, 24, 18)
			membersCall(t, runtime, inverted, "setXORMode", "(Z)V", jvm.IntValue(1))
			membersCall(t, runtime, inverted, "setColor", "(I)V", jvm.IntValue(ink))
			membersCall(t, runtime, inverted, shape.name, shape.descriptor, arguments...)
			reference, got := membersPixels(t, plainPicture), membersPixels(t, xorPicture)
			drawn := 0
			for index := range reference {
				want := uint32(membersGround)
				if reference[index] == ink {
					want, drawn = membersGround^ink, drawn+1
				}
				if got[index] != want {
					t.Fatalf("pixel (%d, %d) = %#06x, want %#06x", index%24, index/24, got[index], want)
				}
			}
			if drawn == 0 {
				t.Fatal("the shape drew nothing")
			}
		})
	}
}

// copyArea reads a source pixel left of or above the surface as nothing, and
// leaves the pixel it would have filled as it was, as KTF does; the rest of
// the copy lands. The clip moves the destination and the source with it.
func TestWIPICopyAreaSkipsASourceLeftOfTheSurface(t *testing.T) {
	runtime, _ := startWIPIGraphicsMembers(t, nil)
	picture, graphics := membersGraphics(t, runtime, 8, 2)
	for x := int32(0); x < 8; x++ {
		membersCall(t, runtime, graphics, "setColor", "(I)V", jvm.IntValue(0x100000*x+0x10))
		membersCall(t, runtime, graphics, "setPixel", "(II)V", jvm.IntValue(x), jvm.IntValue(0))
	}
	before := membersPixels(t, picture)
	// The destination [2, 6) is copied from [-2, 2): the first two source
	// columns are off the surface.
	membersCall(t, runtime, graphics, "copyArea", "(IIIIII)V",
		jvm.IntValue(2), jvm.IntValue(0), jvm.IntValue(-2), jvm.IntValue(0), jvm.IntValue(4), jvm.IntValue(1))
	after := membersPixels(t, picture)
	for x, want := range []uint32{before[0], before[1], before[2], before[3], before[0], before[1], before[6], before[7]} {
		if after[x] != want {
			t.Errorf("column %d = %#06x, want %#06x", x, after[x], want)
		}
	}
	// A clip that starts at column 5 moves both corners: column 5 takes the
	// pixel three to its left and nothing before the clip changes.
	membersCall(t, runtime, graphics, "setClip", "(IIII)V", jvm.IntValue(5), jvm.IntValue(0), jvm.IntValue(3), jvm.IntValue(2))
	membersCall(t, runtime, graphics, "copyArea", "(IIIIII)V",
		jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(-3), jvm.IntValue(0), jvm.IntValue(8), jvm.IntValue(1))
	clipped := membersPixels(t, picture)
	for x, want := range []uint32{after[0], after[1], after[2], after[3], after[4], after[2], after[3], after[4]} {
		if clipped[x] != want {
			t.Errorf("after the clipped copy column %d = %#06x, want %#06x", x, clipped[x], want)
		}
	}
}

// encodeMembersPNG builds a picture whose left column is fully transparent,
// whose middle column is half drawn and whose right column is opaque.
func encodeMembersPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		picture.SetNRGBA(1, y, color.NRGBA{R: 0x40, G: 0x80, B: 0xff, A: 0x80})
		picture.SetNRGBA(2, y, color.NRGBA{R: 0xff, G: 0x20, A: 0xff})
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// A picture in XOR mode inverts what it draws: an opaque pixel by its colour,
// a half-drawn one by half, and a transparent one not at all, the blend the
// platform's own draw uses at the edge of a picture.
func TestWIPIXORPictureInvertsByWhatItDraws(t *testing.T) {
	runtime, _ := startWIPIGraphicsMembers(t, nil)
	encoded := encodeMembersPNG(t)
	sourceArray := jvm.NewByteArray(encoded)
	value, err := runtime.VM.InvokeStatic(wipi.ImageClass, "createImage", "([BII)Lorg/kwis/msp/lcdui/Image;",
		jvm.ReferenceValue(sourceArray), jvm.IntValue(0), jvm.IntValue(int32(len(encoded))))
	if err != nil {
		t.Fatal(err)
	}
	source, _ := value.Reference()
	picture, graphics := membersGraphics(t, runtime, 3, 2)
	membersCall(t, runtime, graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
	membersCall(t, runtime, graphics, "drawImage", "(Lorg/kwis/msp/lcdui/Image;III)V",
		jvm.ReferenceValue(source), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(16|4))
	got := membersPixels(t, picture)
	half := uint32(0)
	inverted := uint32(membersGround ^ 0x4080ff)
	for shift := uint(0); shift <= 16; shift += 8 {
		under, over := uint32(membersGround)>>shift&0xff, inverted>>shift&0xff
		half |= ((over*0x80 + under*0x7f + 127) / 0xff) << shift
	}
	for x, want := range []uint32{membersGround, half, membersGround ^ 0xff2000} {
		for y := 0; y < 2; y++ {
			if pixel := got[y*3+x]; pixel != want && (x != 1 || !withinOne(pixel, want)) {
				t.Errorf("column %d row %d = %#06x, want %#06x", x, y, pixel, want)
			}
		}
	}
}

func withinOne(a, b uint32) bool {
	for shift := uint(0); shift <= 16; shift += 8 {
		x, y := int(a>>shift&0xff), int(b>>shift&0xff)
		if x-y > 1 || y-x > 1 {
			return false
		}
	}
	return true
}

// The XOR mode and the stroke style live in the Graphics object, so a quick
// save keeps them with it and a load gives them back on the same object.
func TestWIPIGraphicsStateSurvivesACheckpoint(t *testing.T) {
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := startWIPIGraphicsMembers(t, store)
	screen := runtime.screenGraphicsObject
	membersCall(t, runtime, screen, "setXORMode", "(Z)V", jvm.IntValue(1))
	membersCall(t, runtime, screen, "setStrokeStyle", "(I)V", jvm.IntValue(wipi.StrokeDotted))
	slot, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoreWIPIGraphicsMembers(t, runtime, store, slot)
	screen = restored.screenGraphicsObject
	if xor, _ := membersCall(t, restored, screen, "isXORMode", "()Z").Int32(); xor != 1 {
		t.Error("a load lost the XOR mode")
	}
	if style, _ := membersCall(t, restored, screen, "getStrokeStyle", "()I").Int32(); style != wipi.StrokeDotted {
		t.Errorf("a load gave back stroke style %d, want DOTTED", style)
	}
	// And the restored mode draws: a white XOR fill inverts the ground the
	// card left there.
	membersCall(t, restored, screen, "setColor", "(I)V", jvm.IntValue(0xffffff))
	membersCall(t, restored, screen, "fillRect", "(IIII)V", jvm.IntValue(30), jvm.IntValue(20), jvm.IntValue(1), jvm.IntValue(1))
	if pixel := restoredScreenPixel(restored, 30, 20); pixel != membersInverted {
		t.Errorf("an XOR fill after the load gave %#06x, want %#06x", pixel, membersInverted)
	}
}

// A quick save written before these members existed holds a Graphics with
// neither state in it. It loads, and the Graphics answers what a new one
// does: XOR off and the solid stroke, and a fill paints its colour.
func TestWIPIGraphicsFromAnOlderCheckpointAnswersTheDefaults(t *testing.T) {
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := startWIPIGraphicsMembers(t, store)
	screen := runtime.screenGraphicsObject
	membersCall(t, runtime, screen, "setXORMode", "(Z)V", jvm.IntValue(1))
	membersCall(t, runtime, screen, "setStrokeStyle", "(I)V", jvm.IntValue(wipi.StrokeDotted))
	slot, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var saved javaCheckpointState
	if err := backend.DecodeCheckpointRecord(slot.Runtime, &saved); err != nil {
		t.Fatal(err)
	}
	removed := 0
	for index := range saved.Threads.Heap.Objects {
		object := &saved.Threads.Heap.Objects[index]
		kept := object.Fields[:0]
		for _, field := range object.Fields {
			if field.Name == "host:graphics-xor-mode" || field.Name == "host:graphics-stroke-style" {
				removed++
				continue
			}
			kept = append(kept, field)
		}
		object.Fields = kept
	}
	if removed != 2 {
		t.Fatalf("the quick save held %d of the new fields, want the screen Graphics' two", removed)
	}
	if slot.Runtime, err = backend.EncodeCheckpointRecord(saved); err != nil {
		t.Fatal(err)
	}
	restored := restoreWIPIGraphicsMembers(t, runtime, store, slot)
	screen = restored.screenGraphicsObject
	if xor, _ := membersCall(t, restored, screen, "isXORMode", "()Z").Int32(); xor != 0 {
		t.Error("a Graphics restored without the XOR mode answers it on")
	}
	if style, _ := membersCall(t, restored, screen, "getStrokeStyle", "()I").Int32(); style != wipi.StrokeSolid {
		t.Errorf("a Graphics restored without a stroke style answers %d, want SOLID", style)
	}
	membersCall(t, restored, screen, "setColor", "(I)V", jvm.IntValue(0xffffff))
	membersCall(t, restored, screen, "fillRect", "(IIII)V", jvm.IntValue(30), jvm.IntValue(20), jvm.IntValue(1), jvm.IntValue(1))
	if pixel := restoredScreenPixel(restored, 30, 20); pixel != 0xffffff {
		t.Errorf("a fill after the load gave %#06x, want white", pixel)
	}
}

func restoreWIPIGraphicsMembers(t *testing.T, runtime *Runtime, store backend.SaveStore, slot backend.Checkpoint) *Runtime {
	t.Helper()
	prepared, err := PrepareJavaCheckpoint(wipiGraphicsMembersJAR, slot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(prepared.Discard)
	restored, err := prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, membersWidth, membersHeight), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Destroy(true) })
	if restored.screenGraphicsObject == nil || restored.screenGraphicsObject.ClassName != wipi.GraphicsClass {
		t.Fatalf("the restored screen Graphics is %v", restored.screenGraphicsObject)
	}
	return restored
}

func restoredScreenPixel(runtime *Runtime, x, y int) uint32 {
	runtime.renderMu.Lock()
	defer runtime.renderMu.Unlock()
	index := (y*runtime.frameWidth + x) * 4
	return uint32(runtime.frameRGBA[index])<<16 | uint32(runtime.frameRGBA[index+1])<<8 | uint32(runtime.frameRGBA[index+2])
}
