package ktf

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// nativeStackedCall runs a handler with up to four arguments in registers and
// the rest on the guest stack, where the calling standard puts everything past
// the fourth.
func nativeStackedCall(t *testing.T, platform *NativePlatform, handler NativeSlotHandler, registers []uint32, stacked ...uint32) uint32 {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range registers {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	stack := ThreadStackBase + uint32(ThreadStackSize) - 0x200
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	spilled := make([]byte, 4*max(1, len(stacked)))
	for index, value := range stacked {
		binary.LittleEndian.PutUint32(spilled[4*index:], value)
	}
	if err := platform.client.core.Memory().Write(stack, spilled); err != nil {
		t.Fatal(err)
	}
	result, err := handler(thread)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return result
}

// nativeHalfwords writes signed halfwords into guest memory and answers where.
func nativeHalfwords(t *testing.T, platform *NativePlatform, values ...int16) uint32 {
	t.Helper()
	raw := make([]byte, 2*len(values))
	for index, value := range values {
		binary.LittleEndian.PutUint16(raw[2*index:], uint16(value))
	}
	return nativeTextAt(t, platform, raw)
}

// nativePadded is a run the way a later module builds it: one character a
// halfword, a single-byte one padded with a zero, and a zero halfword after.
func nativePadded(text string) []byte {
	raw := make([]byte, 0, 2*len(text)+2)
	for index := 0; index < len(text); index++ {
		raw = append(raw, text[index], 0)
	}
	return append(raw, 0, 0)
}

// TestNativeShapesDrawInThePenTheySet covers the shape interface a later module
// draws through: a colour, a point size, points, lines and circle outlines.
func TestNativeShapesDrawInThePenTheySet(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	frame := platform.screen.frame
	red := color.RGBA{R: 0xc0, G: 0x10, B: 0x20, A: 0xff}
	if previous := nativeStackedCall(t, platform, platform.shapeColour, []uint32{0, 0xc0, 0x10, 0x20}, 0xff); previous != 0 {
		t.Errorf("the first colour replaced %#x, want the black a title starts with", previous)
	}

	nativeCall(t, platform.drawPoint, 0, nativeHalfwords(t, platform, 10, 20))
	if got := frame.RGBAAt(10, 20); got != red {
		t.Errorf("the point is %v, want %v", got, red)
	}
	if got := frame.RGBAAt(11, 20); got == red {
		t.Error("a point of size 1 covered its neighbour")
	}
	if previous := nativeCall(t, platform.shapePointSize, 0, 2); previous != 1 {
		t.Errorf("the point size replaced %d, want 1", previous)
	}
	nativeCall(t, platform.drawPoint, 0, nativeHalfwords(t, platform, 30, 40))
	for _, at := range [][2]int{{30, 40}, {31, 40}, {30, 41}, {31, 41}} {
		if got := frame.RGBAAt(at[0], at[1]); got != red {
			t.Errorf("a point of size 2 left %v uncovered", at)
		}
	}

	// A line is a pixel wide whatever the point size.
	nativeCall(t, platform.drawLine, 0, nativeHalfwords(t, platform, 50, 60, 54, 60))
	for x := 50; x <= 54; x++ {
		if got := frame.RGBAAt(x, 60); got != red {
			t.Errorf("the line left (%d, 60) undrawn", x)
		}
	}
	if got := frame.RGBAAt(52, 61); got == red {
		t.Error("the line was drawn two pixels wide")
	}

	// A circle is its outline, radius pixels either side of its centre — to
	// within the pixel the arc walk rounds its last step to, as every
	// platform's arc does.
	nativeCall(t, platform.drawCircle, 0, nativeHalfwords(t, platform, 120, 160, 4))
	for _, at := range [][4]int{{116, 160, 1, 0}, {124, 160, -1, 0}, {120, 156, 0, 1}, {120, 164, 0, -1}} {
		if frame.RGBAAt(at[0], at[1]) != red && frame.RGBAAt(at[0]+at[2], at[1]+at[3]) != red {
			t.Errorf("the circle left %v undrawn", at[:2])
		}
	}
	if got := frame.RGBAAt(120, 160); got == red {
		t.Error("the circle was filled")
	}
	if got := platform.Draws(); got != 4 {
		t.Errorf("draws = %d, want 4", got)
	}
	// The pen a colour replaces is packed as the screen's colours are, BREW's
	// 0xBBGGRR00.
	if previous := nativeStackedCall(t, platform, platform.shapeColour, []uint32{0, 0, 0, 0}, 0xff); previous != 0x2010c000 {
		t.Errorf("the second colour replaced %#x, want 0x2010c000", previous)
	}
}

// TestNativeShapesStayInsideTheClip covers the clip the module sets once a
// frame. A rectangle clips every shape, a null record clears it, and a kind it
// cannot take is refused and leaves the clip as it was.
func TestNativeShapesStayInsideTheClip(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	frame := platform.screen.frame
	nativeStackedCall(t, platform, platform.shapeColour, []uint32{0, 0xff, 0, 0}, 0xff)
	pen := color.RGBA{R: 0xff, A: 0xff}

	clip := nativeHalfwords(t, platform, 1, 0, 0, 0, 10, 10)
	if got := nativeCall(t, platform.shapeClip, 0, clip, 0); got != 0 {
		t.Fatalf("a rectangle clip answered %d, want SUCCESS", got)
	}
	nativeCall(t, platform.drawLine, 0, nativeHalfwords(t, platform, 0, 5, 30, 5))
	if frame.RGBAAt(9, 5) != pen || frame.RGBAAt(10, 5) == pen {
		t.Error("the line was not cut at the clip's edge")
	}
	unknown := nativeHalfwords(t, platform, 7, 0, 0, 0, 1, 1)
	if got := nativeCall(t, platform.shapeClip, 0, unknown, 0); got != nativeShapesFailed {
		t.Errorf("a clip kind it cannot take answered %d, want EFAILED", got)
	}
	nativeCall(t, platform.drawPoint, 0, nativeHalfwords(t, platform, 20, 20))
	if frame.RGBAAt(20, 20) == pen {
		t.Error("a refused clip cleared the one in force")
	}
	if got := nativeCall(t, platform.shapeClip, 0, 0, 0); got != 0 {
		t.Errorf("clearing the clip answered %d, want SUCCESS", got)
	}
	nativeCall(t, platform.drawPoint, 0, nativeHalfwords(t, platform, 20, 20))
	if frame.RGBAAt(20, 20) != pen {
		t.Error("the cleared clip still held the point back")
	}
}

// TestNativeClearFillsWithTheGroundColour covers a fill in no colour, which is
// BREW's clear: it takes the ground the title set, and white when it set none.
func TestNativeClearFillsWithTheGroundColour(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	frame := platform.screen.frame
	clear := func() { nativeCall(t, platform.drawRectangle, 0, 0, nativeNoColour, nativeNoColour) }

	clear()
	if got := frame.RGBAAt(5, 5); got != nativeDefaultGround {
		t.Errorf("a clear with no ground set is %v, want white", got)
	}
	nativeCall(t, platform.screenColour, 0, nativeGroundColourItem, 0)
	clear()
	if got := frame.RGBAAt(5, 5); got != (color.RGBA{A: 0xff}) {
		t.Errorf("a clear over a black ground is %v", got)
	}
	// A real colour is the colour, ground or not, and BREW packs it as
	// 0xBBGGRR00.
	for _, fill := range []struct {
		packed uint32
		want   color.RGBA
	}{
		{0x0000ff00, color.RGBA{R: 0xff, A: 0xff}},
		{0x00ff0000, color.RGBA{G: 0xff, A: 0xff}},
		{0xff000000, color.RGBA{B: 0xff, A: 0xff}},
		{0xd9ae2700, color.RGBA{R: 0x27, G: 0xae, B: 0xd9, A: 0xff}},
	} {
		nativeCall(t, platform.drawRectangle, 0, 0, nativeNoColour, fill.packed)
		if got := frame.RGBAAt(5, 5); got != fill.want {
			t.Errorf("a fill in %#x is %v, want %v", fill.packed, got, fill.want)
		}
	}
}

// TestNativeTextCountIsTheGenerationsUnit covers a counted run. A later module
// passes 14 for "ne button game", a run of fourteen halfwords; the 2005 module
// has passed no count, and one it passed is still bytes.
func TestNativeTextCountIsTheGenerationsUnit(t *testing.T) {
	text := "ne button game"
	later := newTestNativePlatform(t, nil)
	later.archive.Module = nativeGateModule()
	address := nativeTextAt(t, later, nativePadded(text))
	nativeTextCall(t, later, 0x8000, address, int32(len(text)), 0, 0)
	if got := later.Messages(); len(got) != 1 || got[0] != text {
		t.Errorf("a later module's counted run drew %q, want %q", got, text)
	}

	earlier := newTestNativePlatform(t, nil)
	raw, err := earlier.readText(nativeTextAt(t, earlier, nativePadded(text)), 4)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{'n', 0, 'e', 0}; string(raw) != string(want) {
		t.Errorf("the 2005 module's count read %q, want four bytes", raw)
	}
}

// TestNativeMeasureTextIsWhatIsDrawn covers the measure a later module lays its
// help pages out with and divides by. A run is as wide as drawText advances
// past it, a limit stops at the last character that fits, and fits says how
// many did.
func TestNativeMeasureTextIsWhatIsDrawn(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	platform.archive.Module = nativeGateModule()
	text := "TIP#1"
	address := nativeTextAt(t, platform, nativePadded(text))
	face := platform.textFace()
	want := 0
	for _, character := range text {
		want += face.Render(character).Advance
	}
	minusOne := uint32(0xffffffff)
	if got := nativeStackedCall(t, platform, platform.measureText, []uint32{0, 0x8000, address, minusOne}, minusOne, 0); int(got) != want || want == 0 {
		t.Errorf("measure = %d, want %d", got, want)
	}

	fits, err := platform.client.Allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	two := face.Render('T').Advance + face.Render('I').Advance
	got := nativeStackedCall(t, platform, platform.measureText, []uint32{0, 0x8000, address, minusOne}, uint32(two), fits)
	counted, err := platform.client.ReadWord(fits)
	if err != nil {
		t.Fatal(err)
	}
	if int(got) != two || counted != 2 {
		t.Errorf("within %d pixels the measure is %d with %d fitting, want %d with 2", two, got, counted, two)
	}
	if got := nativeStackedCall(t, platform, platform.measureText, []uint32{0, 0x8000, 0, minusOne}, minusOne, 0); got != 0 {
		t.Errorf("a null run measured %d", got)
	}
}

// TestNativeShapesSurviveAQuickLoad covers the record the shape interface rides
// in, and the record every earlier build wrote, which still loads with the
// interface a title starts with.
func TestNativeShapesSurviveAQuickLoad(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore(nil)
	source, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: NewManualClock(time.Time{}), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	set := nativeShapesState{Colour: 0x11223344, PointSize: 3, Clipped: true, ClipX: 1, ClipY: 2, ClipWidth: 30, ClipHeight: 40}
	source.platform.shapes = set.restore()
	// Every clip a record can set is one a checkpoint can carry, including one
	// whose negative size turned it round.
	widest := source.platform
	widest.shapes.clip = nativeRectangle([]byte{0x00, 0x80, 0x00, 0x80, 0x00, 0x80, 0x00, 0x80})
	if err := widest.shapes.capture().validate(); err != nil {
		t.Errorf("the widest clip a record sets is refused: %v (%v)", err, widest.shapes.clip)
	}
	source.platform.shapes = set.restore()
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareNativeSessionCheckpoint(archive, saved, NativeSessionOptions{Clock: NewManualClock(time.Time{})})
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared.session.platform.shapes.capture(); got != set {
		t.Errorf("the restored shapes are %+v, want %+v", got, set)
	}
	prepared.Discard()

	var record nativeShapesRecord
	if err := backend.DecodeCheckpointRecord(saved.Runtime, &record); err != nil {
		t.Fatal(err)
	}
	earlier := saved
	if earlier.Runtime, err = backend.EncodeCheckpointRecord(record.State); err != nil {
		t.Fatal(err)
	}
	prepared, err = PrepareNativeSessionCheckpoint(archive, earlier, NativeSessionOptions{Clock: NewManualClock(time.Time{})})
	if err != nil {
		t.Fatalf("a record without the shapes was refused: %v", err)
	}
	if got, want := prepared.session.platform.shapes.capture(), newNativeShapes().capture(); got != want {
		t.Errorf("a record without the shapes restored %+v, want %+v", got, want)
	}
	prepared.Discard()

	for _, bad := range []struct {
		name    string
		mutate  func(*nativeShapesRecord)
		version bool
	}{
		{name: "a later version", mutate: func(r *nativeShapesRecord) { r.Version++ }, version: true},
		{name: "a point size past a byte", mutate: func(r *nativeShapesRecord) { r.Shapes.PointSize = 256 }},
		{name: "a clip past a halfword", mutate: func(r *nativeShapesRecord) { r.Shapes.ClipWidth = 1 << 16 }},
	} {
		changed := record
		bad.mutate(&changed)
		damaged := saved
		if damaged.Runtime, err = backend.EncodeCheckpointRecord(changed); err != nil {
			t.Fatal(err)
		}
		candidate, err := PrepareNativeSessionCheckpoint(archive, damaged, NativeSessionOptions{Clock: NewManualClock(time.Time{})})
		if candidate != nil {
			candidate.Discard()
		}
		if err == nil || errors.Is(err, backend.ErrCheckpointVersion) != bad.version {
			t.Errorf("%s was answered with %v", bad.name, err)
		}
	}
}

// TestNativeShapesFillRectangles covers the fill a later title draws its bars
// with: a rectangle's outline is in the pen, and with the fill mode on its
// inside is in the fill colour. The mode turns on for one and off for anything
// else, as the specification has it, and answers the mode it set.
func TestNativeShapesFillRectangles(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	frame := platform.screen.frame
	under := color.RGBA{R: 1, G: 2, B: 3, A: 0xff}
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			frame.SetRGBA(x, y, under)
		}
	}
	pen, fill := color.RGBA{R: 0xc0, A: 0xff}, color.RGBA{B: 0xc0, A: 0xff}
	nativeStackedCall(t, platform, platform.shapeColour, []uint32{0, 0xc0, 0, 0}, 0xff)
	if previous := nativeStackedCall(t, platform, platform.shapeFillColour, []uint32{0, 0, 0, 0xc0}, 0xff); previous != 0 {
		t.Errorf("the first fill colour replaced %#x, want the black a title starts with", previous)
	}

	// An outline: the inside is left as it was.
	nativeCall(t, platform.drawShapeRectangle, 0, nativeHalfwords(t, platform, 10, 10, 5, 4))
	for _, pixel := range []struct {
		x, y int
		want color.RGBA
	}{
		{10, 10, pen}, {14, 10, pen}, {10, 13, pen}, {14, 13, pen}, {12, 10, pen}, {10, 12, pen},
		{12, 12, under}, {15, 10, under}, {10, 14, under},
	} {
		if got := frame.RGBAAt(pixel.x, pixel.y); got != pixel.want {
			t.Errorf("an outline drew %v at %d,%d, want %v", got, pixel.x, pixel.y, pixel.want)
		}
	}

	if got := nativeCall(t, platform.shapeFillMode, 0, 1); got != 1 {
		t.Errorf("turning the fill on answered %d, want 1", got)
	}
	nativeCall(t, platform.drawShapeRectangle, 0, nativeHalfwords(t, platform, 20, 20, 5, 4))
	if got := frame.RGBAAt(22, 22); got != fill {
		t.Errorf("a filled rectangle's inside is %v, want %v", got, fill)
	}
	if got := frame.RGBAAt(20, 20); got != pen {
		t.Errorf("a filled rectangle's outline is %v, want the pen %v", got, pen)
	}

	// A value that is neither true nor false turns the fill off.
	if got := nativeCall(t, platform.shapeFillMode, 0, 2); got != 0 {
		t.Errorf("a fill mode of 2 answered %d, want 0", got)
	}
	nativeCall(t, platform.drawShapeRectangle, 0, nativeHalfwords(t, platform, 30, 30, 5, 4))
	if got := frame.RGBAAt(32, 32); got != under {
		t.Errorf("after the fill was turned off the inside is %v", got)
	}
	// A rectangle with no width draws nothing, and one that runs off the
	// clip is cut at it.
	nativeCall(t, platform.drawShapeRectangle, 0, nativeHalfwords(t, platform, 40, 40, 0, 4))
	if got := frame.RGBAAt(40, 40); got != under {
		t.Errorf("a rectangle with no width drew %v", got)
	}
	nativeCall(t, platform.shapeClip, 0, nativeHalfwords(t, platform, 1, 0, 0, 0, 50, 50), 0)
	nativeCall(t, platform.shapeFillMode, 0, 1)
	nativeCall(t, platform.drawShapeRectangle, 0, nativeHalfwords(t, platform, 45, 45, 10, 10))
	if frame.RGBAAt(49, 47) != fill || frame.RGBAAt(50, 47) != under {
		t.Error("a filled rectangle was not cut at the clip's edge")
	}
}

// TestNativeShapesEarlierRecordLoadsWithoutAFill covers the record a build
// that drew no filled shapes wrote: it loads with its pen and clip, and with
// the fill a title starts with.
func TestNativeShapesEarlierRecordLoadsWithoutAFill(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore(nil)
	source, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: NewManualClock(time.Time{}), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	set := nativeShapesState{Colour: 0x11223344, PointSize: 3, Clipped: true, ClipX: 1, ClipY: 2, ClipWidth: 30, ClipHeight: 40, Filled: true, FillColour: 0x55667788}
	source.platform.shapes = set.restore()
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var record nativeShapesRecord
	if err := backend.DecodeCheckpointRecord(saved.Runtime, &record); err != nil {
		t.Fatal(err)
	}
	outline := nativeOutlineShapesRecord{Version: nativeOutlineShapesRecordVersion, State: record.State, Shapes: nativeOutlineShapesState{
		Colour: set.Colour, PointSize: set.PointSize, Clipped: set.Clipped,
		ClipX: set.ClipX, ClipY: set.ClipY, ClipWidth: set.ClipWidth, ClipHeight: set.ClipHeight,
	}}
	earlier := saved
	if earlier.Runtime, err = backend.EncodeCheckpointRecord(outline); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareNativeSessionCheckpoint(archive, earlier, NativeSessionOptions{Clock: NewManualClock(time.Time{})})
	if err != nil {
		t.Fatalf("a record without the fill was refused: %v", err)
	}
	want := set
	starting := newNativeShapes().capture()
	want.Filled, want.FillColour = starting.Filled, starting.FillColour
	if got := prepared.session.platform.shapes.capture(); got != want {
		t.Errorf("a record without the fill restored %+v, want %+v", got, want)
	}
	prepared.Discard()

	// The earlier record at a version it never had is refused as a version.
	outline.Version++
	if earlier.Runtime, err = backend.EncodeCheckpointRecord(outline); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareNativeSessionCheckpoint(archive, earlier, NativeSessionOptions{Clock: NewManualClock(time.Time{})})
	if candidate != nil {
		candidate.Discard()
	}
	if !errors.Is(err, backend.ErrCheckpointVersion) {
		t.Errorf("an earlier record at another version was answered with %v", err)
	}
}

// TestNativeTextFollowsItsAlignment covers BREW's alignment flags: an aligned
// call has its x or y ignored and lands against the background rectangle, the
// screen when there is none, and the text is clipped to that rectangle. A
// later title centres the choices under its message box this way with x zero.
func TestNativeTextFollowsItsAlignment(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	frame := platform.screen.frame
	face := platform.textFace()
	nativeCall(t, platform.screenColour, 0, nativeTextColourItem, 0)
	ink := color.RGBA{A: 0xff}
	address := nativeTextAt(t, platform, []byte("MMMM\x00"))
	width := 4 * face.Render('M').Advance
	inked := func(area image.Rectangle) (image.Rectangle, bool) {
		var found image.Rectangle
		any := false
		for y := area.Min.Y; y < area.Max.Y; y++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				if frame.RGBAAt(x, y) == ink {
					point := image.Rect(x, y, x+1, y+1)
					if !any {
						found, any = point, true
					}
					found = found.Union(point)
				}
			}
		}
		return found, any
	}
	clear := func() { nativeCall(t, platform.drawRectangle, 0, 0, nativeNoColour, 0xffffff00) }
	draw := func(x, y int32, background, flags uint32) {
		nativeStackedCall(t, platform, platform.drawText, []uint32{0, 0, address, 0xffffffff}, uint32(x), uint32(y), background, flags)
	}
	screen := frame.Bounds()

	for _, test := range []struct {
		name  string
		flags uint32
		left  int
	}{
		{name: "centred", flags: 0x8020, left: (screen.Dx() - width) / 2},
		{name: "right", flags: 0x8040, left: screen.Dx() - width},
		{name: "left", flags: 0x8010, left: 0},
	} {
		clear()
		draw(7, 100, 0, test.flags)
		found, ok := inked(screen)
		if !ok {
			t.Fatalf("%s text left no ink", test.name)
		}
		// The glyph's own side bearing is all that separates its ink from
		// where the run starts.
		if found.Min.X < test.left || found.Min.X > test.left+face.Render('M').Advance/2 {
			t.Errorf("%s text starts at %d, want the run to start at %d", test.name, found.Min.X, test.left)
		}
	}

	// Centred both ways in a rectangle of the title's own, and kept inside it.
	box := nativeHalfwords(t, platform, 40, 60, int16(width+20), int16(face.Height()+40))
	clear()
	draw(0, 0, box, 0x8220)
	found, ok := inked(screen)
	if !ok {
		t.Fatal("text centred in a rectangle left no ink")
	}
	top := 60 + (face.Height()+40-face.Height())/2
	if found.Min.Y < top || found.Max.Y > top+face.Height() || found.Min.X < 50 {
		t.Errorf("text centred in the rectangle covers %v, want inside the line at %d,%d", found, 50, top)
	}
	small := nativeHalfwords(t, platform, 0, 0, 5, 200)
	clear()
	draw(0, 100, small, 0x8000)
	if found, ok := inked(screen); ok && found.Max.X > 5 {
		t.Errorf("text drawn in a rectangle 5 wide reached x %d", found.Max.X)
	}
}
