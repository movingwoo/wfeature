package ktf

import (
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

	// A caller that gives a count is taken at its word, and one that gives an
	// impossible one is refused rather than read.
	address := nativeTextAt(t, platform, []byte("WAIT..\x00"))
	if raw, err := platform.readText(address, 4); err != nil || string(raw) != "WAIT" {
		t.Errorf("a counted run read %q (%v), want %q", raw, err, "WAIT")
	}
	if _, err := platform.readText(address, maxNativeTextLength+1); err == nil {
		t.Error("a run longer than the limit was read")
	}
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
