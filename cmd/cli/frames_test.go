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

// The two holes in a sweep's automatic judgment are a screen filled with one
// colour and a screen with nothing on it, and this is what makes both visible:
// a run whose every frame is one colour is reported as such and exits nonzero,
// so a script can ask without a person looking.
func TestFrameStatsFindsAScreenWithNothingOnIt(t *testing.T) {
	directory := t.TempDir()
	for _, tick := range []int{1, 2} {
		writeFrame(t, directory, tick, 0xff, image.Pt(-1, 0))
	}
	output, errors := &strings.Builder{}, &strings.Builder{}
	if code := frameStats([]string{directory}, output, errors); code == 0 {
		t.Fatalf("a run of single-colour frames exited zero: %s", output)
	}
	if !strings.Contains(output.String(), "2 frames, 2 of one colour") {
		t.Fatalf("stats do not name the solid frames: %s", output)
	}
	// A white screen is every pixel lit, which is exactly the frame the KTF
	// half of the judgment passes.
	if !strings.Contains(output.String(), "lit=48 of 48") {
		t.Fatalf("stats do not count a filled screen as lit: %s", output)
	}
}

// A frame with something drawn on it exits zero, and one frame among solid
// ones is enough: a boot that starts black and then draws is a working boot.
func TestFrameStatsAcceptsARunThatDrawsSomething(t *testing.T) {
	directory := t.TempDir()
	writeFrame(t, directory, 1, 0x00, image.Pt(-1, 0))
	writeFrame(t, directory, 2, 0x00, image.Pt(3, 2))
	output, errors := &strings.Builder{}, &strings.Builder{}
	if code := frameStats([]string{directory}, output, errors); code != 0 {
		t.Fatalf("a run that drew something exited %d: %s", code, errors)
	}
	if !strings.Contains(output.String(), "2 frames, 1 of one colour, 1 with nothing lit") {
		t.Fatalf("stats do not separate the drawn frame from the blank one: %s", output)
	}
}

// One PNG is a frame too: `-frame out.png` is how a sweep captures its screen.
func TestFrameStatsReadsASingleFrame(t *testing.T) {
	directory := t.TempDir()
	path := writeFrame(t, directory, 7, 0x20, image.Pt(1, 1))
	output, errors := &strings.Builder{}, &strings.Builder{}
	if code := frameStats([]string{path}, output, errors); code != 0 {
		t.Fatalf("one drawn frame exited %d: %s", code, errors)
	}
	if !strings.Contains(output.String(), "colours=2") {
		t.Fatalf("stats do not count the frame's colours: %s", output)
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

// A zoom keeps every pixel of the box it was asked for, repeated scale times
// in each direction. Nearest-neighbour is the whole point: the marked pixel
// has to come back as a solid block of its own colour, because a sprite's
// facing is read off exactly such a block.
func TestZoomCropsAndScalesWithoutBlending(t *testing.T) {
	directory := t.TempDir()
	source := writeFrame(t, directory, 1, 0x40, image.Pt(5, 2))
	out := filepath.Join(directory, "zoom.png")

	var stdout, stderr strings.Builder
	code := zoomFrame([]string{source, out,
		"-x", "4", "-y", "1", "-width", "3", "-height", "3", "-scale", "4"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("zoom exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "(4,1)-(7,4) at 4x") {
		t.Fatalf("zoom reported %q", stdout.String())
	}
	zoomed, err := readPNG(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := zoomed.Bounds().Size(); got != (image.Point{X: 12, Y: 12}) {
		t.Fatalf("zoom wrote %v, want 12x12", got)
	}
	// The marked pixel is frame (5,2), so box (1,1), so the block at (4..7,
	// 4..7) of the zoom — every pixel of it, with nothing blended at the edge.
	for y := 4; y < 8; y++ {
		for x := 4; x < 8; x++ {
			red, green, blue, _ := zoomed.At(x, y).RGBA()
			if red>>8 != 0xff || green != 0 || blue != 0 {
				t.Fatalf("zoom (%d,%d) = %v, want the marked colour", x, y, zoomed.At(x, y))
			}
		}
	}
	if red, _, _, _ := zoomed.At(3, 4).RGBA(); red>>8 == 0xff {
		t.Fatalf("zoom bled the mark into the pixel beside it")
	}
}

// A box that runs off the edge is clipped rather than refused, because the
// coordinates come from guessing where a sprite is. A box entirely outside the
// frame is the mistake worth reporting.
func TestZoomClipsToTheFrameAndRefusesABoxOutsideIt(t *testing.T) {
	directory := t.TempDir()
	source := writeFrame(t, directory, 1, 0x40, image.Pt(-1, 0))
	out := filepath.Join(directory, "zoom.png")

	var stdout, stderr strings.Builder
	if code := zoomFrame([]string{source, out,
		"-x", "6", "-width", "40", "-height", "40", "-scale", "2"}, &stdout, &stderr); code != 0 {
		t.Fatalf("zoom exited %d: %s", code, stderr.String())
	}
	zoomed, err := readPNG(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := zoomed.Bounds().Size(); got != (image.Point{X: 4, Y: 12}) {
		t.Fatalf("clipped zoom is %v, want 4x12", got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := zoomFrame([]string{source, out, "-x", "80", "-y", "80"}, &stdout, &stderr); code == 0 {
		t.Fatalf("zoom accepted a box outside the frame: %s", stdout.String())
	}
}
