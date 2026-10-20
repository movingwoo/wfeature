package ktf

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// nativeTextCall runs the text handler with the arguments the display's own
// call takes: four in registers and the rest on the guest stack, which is
// where the calling standard puts everything past the fourth.
func nativeTextCall(t *testing.T, platform *NativePlatform, font, address uint32, count int32, x, y int32) uint32 {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	stack := ThreadStackBase + uint32(ThreadStackSize) - 0x200
	for index, value := range []uint32{1, font, address, uint32(count)} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	spilled := make([]byte, 16)
	binary.LittleEndian.PutUint32(spilled, uint32(x))
	binary.LittleEndian.PutUint32(spilled[4:], uint32(y))
	if err := platform.client.core.Memory().Write(stack, spilled); err != nil {
		t.Fatal(err)
	}
	result, err := platform.drawText(thread)
	if err != nil {
		t.Fatalf("draw text: %v", err)
	}
	return result
}

// nativeTextAt writes a run of text into guest memory and answers where.
func nativeTextAt(t *testing.T, platform *NativePlatform, raw []byte) uint32 {
	t.Helper()
	address, err := platform.client.Allocate(uint32(len(raw)) + 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.client.core.Memory().Write(address, raw); err != nil {
		t.Fatal(err)
	}
	return address
}

// TestNativeTextReadsBothShapesOfARun covers the terminator. One module builds
// its lines a halfword at a time, so a one-byte space carries a zero after it
// and a reader that stops at the first zero byte stops at the space — three
// quarters of a sentence lost. The other hands over plain bytes. One rule
// reads both.
func TestNativeTextReadsBothShapesOfARun(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	for _, testCase := range []struct {
		name string
		raw  []byte
		want string
	}{
		{
			name: "plain bytes with a zero after the last",
			raw:  []byte("WAIT..\x00\x00\x00\x00"),
			want: "WAIT..",
		},
		{
			name: "a halfword for every character",
			// 인 증 ' ' 요 청, and then the zero halfword that ends it.
			raw:  []byte{0xc0, 0xce, 0xc1, 0xf5, 0x20, 0x00, 0xbf, 0xe4, 0xc3, 0xbb, 0x00, 0x00, 0xb4, 0xd9},
			want: "인증 요청",
		},
		{
			name: "a halfword run ending on its boundary",
			raw:  []byte{0xc0, 0xce, 0x00, 0x00, 0xc3, 0xbb},
			want: "인",
		},
		{
			name: "nothing at all",
			raw:  []byte{0x00, 0x00, 0x00, 0x00},
			want: "",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			address := nativeTextAt(t, platform, testCase.raw)
			raw, err := platform.readText(address, -1)
			if err != nil {
				t.Fatalf("read text: %v", err)
			}
			if got := decodeEUCKR(raw); got != testCase.want {
				t.Errorf("read %q, want %q", got, testCase.want)
			}
		})
	}

}

// nativeLaterTextPlatform is a test platform whose module is of the later
// generation, the one whose runs are halfwords.
func nativeLaterTextPlatform(t *testing.T) *NativePlatform {
	t.Helper()
	platform := newTestNativePlatform(t, nil)
	platform.archive.Module = nativeGateModule()
	return platform
}

// TestNativeTextCountsInTheUnitsOfTheRun covers a run with a count. A later
// title passes 14 for "ne button game", fourteen halfwords; the 2005 module's
// runs are plain bytes, and a count of them is a count of bytes. Read the
// other way round, one draws half its line and the other twice its line and
// whatever follows it.
func TestNativeTextCountsInTheUnitsOfTheRun(t *testing.T) {
	later := nativeLaterTextPlatform(t)
	address := nativeTextAt(t, later, []byte("W\x00A\x00I\x00T\x00.\x00.\x00\x00\x00"))
	if raw, err := later.readText(address, 4); err != nil || string(raw) != "WAIT" {
		t.Errorf("a counted run of halfwords read %q (%v), want %q", raw, err, "WAIT")
	}
	mixed := nativeTextAt(t, later, []byte{0xc0, 0xce, 'A', 0x00, 0xc1, 0xf5, 0x00, 0x00})
	if raw, err := later.readText(mixed, 2); err != nil || decodeEUCKR(raw) != "인A" {
		t.Errorf("a counted run of Korean and ASCII read %q (%v), want %q", decodeEUCKR(raw), err, "인A")
	}

	earlier := newTestNativePlatform(t, nil)
	plain := nativeTextAt(t, earlier, []byte("WAIT..\x00"))
	if raw, err := earlier.readText(plain, 4); err != nil || string(raw) != "WAIT" {
		t.Errorf("a counted run of bytes read %q (%v), want %q", raw, err, "WAIT")
	}
	korean := nativeTextAt(t, earlier, []byte{0xc0, 0xce, 'A', 0xc1, 0xf5, 0x00})
	if raw, err := earlier.readText(korean, 3); err != nil || decodeEUCKR(raw) != "인A" {
		t.Errorf("a counted run of Korean and ASCII bytes read %q (%v), want %q", decodeEUCKR(raw), err, "인A")
	}
}

// TestNativeTextReadsWhatThereIsOfARun covers a run that goes on past what is
// read of it. One longer than the bound is the part up to the bound, and one
// that reaches the end of guest memory is what is there up to that end: the
// screen shows the front of a line either way, and measuring it answers how
// wide that front is rather than nothing. Only a run of which nothing can be
// read is an error.
func TestNativeTextReadsWhatThereIsOfARun(t *testing.T) {
	later := nativeLaterTextPlatform(t)
	long := nativeTextAt(t, later, bytes.Repeat([]byte{'A', 0}, maxNativeTextLength))
	if raw, err := later.readText(long, maxNativeTextLength); err != nil || len(raw) != maxNativeTextLength/2 {
		t.Errorf("a run past the bound read %d characters (%v), want the %d up to it", len(raw), err, maxNativeTextLength/2)
	}

	top := ThreadStackBase + uint32(ThreadStackSize)
	memory := later.client.core.Memory()
	if err := memory.Read(top, make([]byte, 1)); err == nil {
		t.Fatal("guest memory goes on past the stack")
	}
	end := top - 6
	if err := memory.Write(end, []byte("A\x00B\x00C\x00")); err != nil {
		t.Fatal(err)
	}
	for _, count := range []int32{-1, 8} {
		if raw, err := later.readText(end, count); err != nil || string(raw) != "ABC" {
			t.Errorf("a run of count %d at the end of memory read %q (%v), want %q", count, raw, err, "ABC")
		}
	}
	if _, err := later.readText(top, 4); err == nil {
		t.Error("a run of which nothing can be read was read")
	}
}

// TestNativeMeasureTextAnswersTheWidthItIsDrawnAt covers what a title lays its
// help text out with: it measures a run, divides by its length, and takes the
// answer as the width of a character. Zero is a division by zero the title's
// own runtime ends the run over.
func TestNativeMeasureTextAnswersTheWidthItIsDrawnAt(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	face := platform.textFace()
	a, b := face.Render('A').Advance, face.Render('B').Advance
	run := nativeTextAt(t, platform, []byte("A\x00B\x00\x00\x00"))
	if got := nativeMeasureCall(t, platform, run, -1, -1, 0); got != uint32(a+b) || got == 0 {
		t.Errorf("width = %d, want %d", got, a+b)
	}
	if got := nativeMeasureCall(t, platform, run, 1, -1, 0); got != uint32(a) {
		t.Errorf("width of a counted run of one = %d, want %d", got, a)
	}
	// A width to stop at stops before the character that would pass it, and
	// says how many fit.
	fits, err := platform.client.Allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeMeasureCall(t, platform, run, -1, int32(a+b-1), fits); got != uint32(a) {
		t.Errorf("width within %d = %d, want %d", a+b-1, got, a)
	}
	if got := binary.LittleEndian.Uint32(nativeRead(t, platform, fits, 4)); got != 1 {
		t.Errorf("%d characters fit, want 1", got)
	}
	if got := nativeMeasureCall(t, platform, 0, -1, -1, 0); got != 0 {
		t.Errorf("a null run measured %d", got)
	}
}

// TestNativeMeasureTextMeasuresWhatItDraws covers the runs the reading bounds:
// one past the bound measures the part up to it, and one that reaches the end
// of guest memory the part before that end, which is what drawing either
// draws. Answering nothing for them was the division by zero again.
func TestNativeMeasureTextMeasuresWhatItDraws(t *testing.T) {
	platform := nativeLaterTextPlatform(t)
	face := platform.textFace()
	a, b, c := face.Render('A').Advance, face.Render('B').Advance, face.Render('C').Advance
	long := nativeTextAt(t, platform, bytes.Repeat([]byte{'A', 0}, maxNativeTextLength))
	if got, want := nativeMeasureCall(t, platform, long, maxNativeTextLength, -1, 0), uint32(a*maxNativeTextLength/2); got != want {
		t.Errorf("a run past the bound measured %d, want %d", got, want)
	}
	end := ThreadStackBase + uint32(ThreadStackSize) - 6
	if err := platform.client.core.Memory().Write(end, []byte("A\x00B\x00C\x00")); err != nil {
		t.Fatal(err)
	}
	if got := nativeMeasureCall(t, platform, end, -1, -1, 0); got != uint32(a+b+c) {
		t.Errorf("a run at the end of memory measured %d, want %d", got, a+b+c)
	}
}

// TestNativeMeasureTextCountsWhatFitsInTheRunsUnits covers the count a caller
// is given of what fits: it is in the run's own units, which a caller steps
// through its run by. A Korean character is one halfword of a later title's
// run and two bytes of the 2005 module's.
func TestNativeMeasureTextCountsWhatFitsInTheRunsUnits(t *testing.T) {
	for _, generation := range []struct {
		name     string
		platform func(t *testing.T) *NativePlatform
		run      []byte
		want     uint32
	}{
		{name: "halfwords", platform: nativeLaterTextPlatform, run: []byte{0xc0, 0xce, 'A', 0x00, 0xc1, 0xf5, 0x00, 0x00}, want: 2},
		{name: "plain bytes", platform: func(t *testing.T) *NativePlatform { return newTestNativePlatform(t, nil) },
			run: []byte{0xc0, 0xce, 'A', 0xc1, 0xf5, 0x00, 0x00, 0x00}, want: 3},
	} {
		t.Run(generation.name, func(t *testing.T) {
			platform := generation.platform(t)
			face := platform.textFace()
			// The width of 인 and A, short of the third character.
			limit := face.Render('인').Advance + face.Render('A').Advance
			fits, err := platform.client.Allocate(4)
			if err != nil {
				t.Fatal(err)
			}
			run := nativeTextAt(t, platform, generation.run)
			if got := nativeMeasureCall(t, platform, run, -1, int32(limit), fits); got != uint32(limit) {
				t.Errorf("width within %d = %d", limit, got)
			}
			if got := binary.LittleEndian.Uint32(nativeRead(t, platform, fits, 4)); got != generation.want {
				t.Errorf("%d units fit, want %d", got, generation.want)
			}
		})
	}
}

// nativeMeasureCall runs the measuring handler with its last two arguments on
// the guest stack.
func nativeMeasureCall(t *testing.T, platform *NativePlatform, address uint32, count, limit int32, fits uint32) uint32 {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	stack := ThreadStackBase + uint32(ThreadStackSize) - 0x200
	for index, value := range []uint32{1, 0x8000, address, uint32(count)} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	spilled := binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, uint32(limit)), fits)
	if err := platform.client.core.Memory().Write(stack, spilled); err != nil {
		t.Fatal(err)
	}
	result, err := platform.measureText(thread)
	if err != nil {
		t.Fatalf("measure text: %v", err)
	}
	return result
}

// TestNativeDrawTextPutsInkOnTheScreen covers the drawing itself: the ink is
// the colour the title set, the run lands at the point it was given with y as
// the top of the line, and what a title says is still reported.
func TestNativeDrawTextPutsInkOnTheScreen(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	ink := color.RGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff}
	nativeCall(t, platform.screenColour, 0, nativeTextColourItem, 0x20406000)

	const x, y = 30, 40
	address := nativeTextAt(t, platform, []byte("A\x00"))
	if got := nativeTextCall(t, platform, 0x8000, address, -1, x, y); got == 0 {
		t.Fatal("drawing text answered nothing")
	}

	face := platform.textFace()
	frame := platform.screen.frame
	marked := 0
	for row := y; row < y+face.Height(); row++ {
		for column := x; column < x+16; column++ {
			if frame.RGBAAt(column, row) == ink {
				marked++
			}
		}
	}
	if marked == 0 {
		t.Error("the run left no ink inside the line it was given")
	}
	// Nothing outside the line: a y read as a baseline would put most of the
	// glyph above the point instead of below it.
	for row := 0; row < y; row++ {
		for column := 0; column < frame.Bounds().Max.X; column++ {
			if frame.RGBAAt(column, row) == ink {
				t.Fatalf("ink at %d,%d, above the top of the line", column, row)
			}
		}
	}
	if got := platform.Messages(); len(got) != 1 || got[0] != "A" {
		t.Errorf("messages = %q, want the run that was drawn", got)
	}

	// A null run is nothing to draw rather than a read of address zero.
	if got := nativeTextCall(t, platform, 0x8000, 0, -1, x, y); got == 0 {
		t.Error("a null run was reported as a failure")
	}
}

// TestNativeFontMetricsAnswerTheFace covers what a module lays its lines out
// with: it asks for a font's height and steps by it.
func TestNativeFontMetricsAnswerTheFace(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	face := platform.textFace()
	if got := nativeCall(t, platform.fontMetrics, 0, 0x8000, 0, 0); got != uint32(face.Height()) {
		t.Errorf("height = %d, want %d", got, face.Height())
	}
	// The two pointers are filled in when they are not null, which is the form
	// the specification's own call takes.
	out, err := platform.client.Allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	nativeCall(t, platform.fontMetrics, 0, 0x8000, out, out+4)
	ascent := binary.LittleEndian.Uint32(nativeRead(t, platform, out, 4))
	descent := binary.LittleEndian.Uint32(nativeRead(t, platform, out+4, 4))
	if int(ascent) != face.Ascent || int(descent) != face.Descent {
		t.Errorf("ascent %d descent %d, want %d and %d", ascent, descent, face.Ascent, face.Descent)
	}
}
