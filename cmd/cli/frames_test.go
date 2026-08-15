package main

import (
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"testing"
)

// writeFrame lays down one tickNNNN.png of a solid colour with one pixel of
// its own, which is what a difference has to be able to find.
func writeFrame(t *testing.T, directory string, tick int, shade uint8, mark image.Point) string {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			frame.Set(x, y, color.RGBA{shade, shade, shade, 0xff})
		}
	}
	if mark.X >= 0 {
		frame.Set(mark.X, mark.Y, color.RGBA{0xff, 0, 0, 0xff})
	}
	path := filepath.Join(directory, "tick"+pad(tick)+".png")
	if err := encodePNG(path, frame); err != nil {
		t.Fatal(err)
	}
	return path
}

func pad(tick int) string {
	text := ""
	for _, digit := range []int{1000, 100, 10, 1} {
		text += string(rune('0' + (tick/digit)%10))
	}
	return text
}

// A framedir is read in tick order rather than in name order, because a run
// past ten thousand ticks sorts wrong as text and a contact sheet in the wrong
// order is worse than none.
func TestFrameFilesReadInTickOrder(t *testing.T) {
	directory := t.TempDir()
	for _, tick := range []int{120, 9, 1000, 87} {
		writeFrame(t, directory, tick, 0x40, image.Pt(-1, 0))
	}
	frames, err := frameFiles(directory)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{9, 87, 120, 1000}
	if len(frames) != len(want) {
		t.Fatalf("read %d frames, want %d", len(frames), len(want))
	}
	for index, tick := range want {
		if frames[index].tick != tick {
			t.Fatalf("frame %d is tick %d, want %d", index, frames[index].tick, tick)
		}
	}
}

// The sheet holds one tile per selected frame at the size the shrink asks for,
// so a caller can tell from the dimensions that it got the run it wanted.
func TestContactSheetTilesTheSelectedFrames(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	for tick := 0; tick < 40; tick++ {
		writeFrame(t, source, tick, uint8(tick), image.Pt(-1, 0))
	}
	out := filepath.Join(destination, "sheet.png")

	var stdout, stderr strings.Builder
	if code := contactSheet([]string{source, out, "-every", "10", "-columns", "2", "-shrink", "2"}, &stdout, &stderr); code != 0 {
		t.Fatalf("contactsheet exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "4 of 40 frames") {
		t.Fatalf("contactsheet reported %q", stdout.String())
	}
	sheet, err := readPNG(out)
	if err != nil {
		t.Fatal(err)
	}
	// Four frames at 4x3 each, two columns of (4+4) by two rows of (3+4+10).
	if got, want := sheet.Bounds().Size(), image.Pt(16, 34); got != want {
		t.Fatalf("sheet is %v, want %v", got, want)
	}
}

// -from and -to are what turn a whole run into the stretch worth reading frame
// by frame.
func TestContactSheetHonoursTheTickRange(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	for tick := 0; tick < 40; tick++ {
		writeFrame(t, source, tick, uint8(tick), image.Pt(-1, 0))
	}
	var stdout, stderr strings.Builder
	code := contactSheet([]string{source, filepath.Join(destination, "sheet.png"),
		"-every", "1", "-from", "10", "-to", "14"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("contactsheet exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "5 of 40 frames") {
		t.Fatalf("contactsheet reported %q", stdout.String())
	}
}

// The bounding box is the point of the diff: it says whether a change touched
// the dialogue box or the whole screen. Frames the two runs share and agree on
// are counted but not named.
func TestFrameDiffNamesTheDifferingFramesAndWhere(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	for tick := 0; tick < 5; tick++ {
		writeFrame(t, before, tick, 0x40, image.Pt(-1, 0))
		mark := image.Pt(-1, 0)
		if tick == 3 {
			mark = image.Pt(5, 2)
		}
		writeFrame(t, after, tick, 0x40, mark)
	}
	// A frame only one run has is skipped rather than counted as a difference.
	writeFrame(t, after, 9, 0x40, image.Pt(-1, 0))

	var stdout, stderr strings.Builder
	if code := frameDiff([]string{after, before}, &stdout, &stderr); code != 0 {
		t.Fatalf("framediff exited %d: %s", code, stderr.String())
	}
	report := stdout.String()
	if !strings.Contains(report, "1 of 5 frames present in both runs differ") {
		t.Fatalf("framediff reported %q", report)
	}
	if !strings.Contains(report, "tick0003  (5,2)-(6,3)") {
		t.Fatalf("framediff did not name the differing pixel: %q", report)
	}
}

// Two runs that share no frame names is a mistake worth reporting rather than
// a clean "nothing differs" — the usual cause is a directory that was never
// written.
func TestFrameDiffRefusesRunsWithNothingInCommon(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	writeFrame(t, before, 1, 0x40, image.Pt(-1, 0))
	writeFrame(t, after, 2, 0x40, image.Pt(-1, 0))

	var stdout, stderr strings.Builder
	if code := frameDiff([]string{after, before}, &stdout, &stderr); code == 0 {
		t.Fatalf("framediff accepted two unrelated runs: %s", stdout.String())
	}
}
