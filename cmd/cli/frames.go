package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Reviewing a `-framedir` run. A scripted run answers what a title does with
// what it was told, but only if somebody looks at the frames, and a run is a
// few thousand of them. These are the two ways of looking that have found
// things: read the whole run at a glance, and ask what one change did to it.
//
// They are subcommands rather than a script beside the repository because the
// project takes no new dependencies, and rendering a grid of PNGs and
// comparing two of them is entirely within the standard library. A reviewing
// step that needs an install is a reviewing step that gets skipped.

// contactSheet tiles every Nth frame of a directory into one image, labelled
// with the tick each came from. A title's whole boot, menu, character creation
// and first minutes read as a page — which is what makes a scene that never
// plays visible at all, since its absence is not something a single frame
// shows.
func contactSheet(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: wfeature contactsheet <framedir> <out.png> [-every N] [-columns N] [-shrink N] [-from tick] [-to tick]")
		return 2
	}
	source, destination := args[0], args[1]
	every, columns, shrink := 20, 10, 2
	from, to := 0, -1
	for index := 2; index < len(args); index++ {
		if index+1 >= len(args) {
			fmt.Fprintf(stderr, "%s expects a value\n", args[index])
			return 2
		}
		value, err := strconv.Atoi(args[index+1])
		if err != nil {
			fmt.Fprintf(stderr, "invalid %s %q\n", args[index], args[index+1])
			return 2
		}
		switch args[index] {
		case "-every":
			every = value
		case "-columns":
			columns = value
		case "-shrink":
			shrink = value
		case "-from":
			from = value
		case "-to":
			to = value
		default:
			fmt.Fprintf(stderr, "unknown contactsheet option %q\n", args[index])
			return 2
		}
		index++
	}
	if every <= 0 || columns <= 0 || shrink <= 0 {
		fmt.Fprintln(stderr, "-every, -columns and -shrink must be positive")
		return 2
	}

	frames, err := frameFiles(source)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	chosen := make([]frameFile, 0, len(frames)/every+1)
	for _, frame := range frames {
		if frame.tick < from || (to >= 0 && frame.tick > to) {
			continue
		}
		if len(chosen) == 0 || frame.tick >= chosen[len(chosen)-1].tick+every {
			chosen = append(chosen, frame)
		}
	}
	if len(chosen) == 0 {
		fmt.Fprintf(stderr, "no frames in %s within the range asked for\n", source)
		return 1
	}

	first, err := readPNG(chosen[0].path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cell := first.Bounds().Size().Div(shrink)
	const (
		gap   = 4
		label = 10
	)
	rows := (len(chosen) + columns - 1) / columns
	sheet := image.NewRGBA(image.Rect(0, 0,
		columns*(cell.X+gap), rows*(cell.Y+gap+label)))
	draw.Draw(sheet, sheet.Bounds(), &image.Uniform{color.RGBA{0x28, 0x28, 0x30, 0xff}}, image.Point{}, draw.Src)
	for index, frame := range chosen {
		tile, err := readPNG(frame.path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		x := (index % columns) * (cell.X + gap)
		y := (index / columns) * (cell.Y + gap + label)
		drawTinyNumber(sheet, x+1, y+2, frame.tick)
		drawShrunk(sheet, image.Pt(x, y+label), tile, cell, shrink)
	}
	if err := encodePNG(destination, sheet); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "%d of %d frames -> %s (%dx%d)\n",
		len(chosen), len(frames), destination, sheet.Bounds().Dx(), sheet.Bounds().Dy())
	return 0
}

// zoomFrame crops a box out of one frame and scales it up, pixel for pixel.
//
// It is the third way of looking, and it exists because of a question a
// contact sheet cannot answer: **which way is the character facing.** A
// handset screen is 240 wide and a sprite on it is twenty pixels; a report
// that says "the attack goes the wrong way" is checked by reading a sprite
// that small, and at 1:1 it cannot be read at all. At five times it is
// obvious. Nearest-neighbour for the same reason `drawShrunk` drops pixels
// rather than averaging them — a smoothed sprite is a sprite whose facing has
// been invented by the filter.
func zoomFrame(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr,
			"usage: wfeature zoom <frame.png> <out.png> [-x N] [-y N] [-width N] [-height N] [-scale N]")
		return 2
	}
	source, destination := args[0], args[1]
	x, y, width, height, scale := 0, 0, 0, 0, 4
	for index := 2; index < len(args); index++ {
		if index+1 >= len(args) {
			fmt.Fprintf(stderr, "%s expects a value\n", args[index])
			return 2
		}
		value, err := strconv.Atoi(args[index+1])
		if err != nil {
			fmt.Fprintf(stderr, "invalid %s %q\n", args[index], args[index+1])
			return 2
		}
		switch args[index] {
		case "-x":
			x = value
		case "-y":
			y = value
		case "-width":
			width = value
		case "-height":
			height = value
		case "-scale":
			scale = value
		default:
			fmt.Fprintf(stderr, "unknown zoom option %q\n", args[index])
			return 2
		}
		index++
	}
	if scale <= 0 {
		fmt.Fprintln(stderr, "-scale must be positive")
		return 2
	}

	frame, err := readPNG(source)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// The box is clipped to the frame rather than refused, so a guess at where
	// a sprite is can be off the edge without costing a run.
	bounds := frame.Bounds()
	box := image.Rect(bounds.Min.X+x, bounds.Min.Y+y, bounds.Max.X, bounds.Max.Y)
	if width > 0 {
		box.Max.X = min(box.Max.X, box.Min.X+width)
	}
	if height > 0 {
		box.Max.Y = min(box.Max.Y, box.Min.Y+height)
	}
	box = box.Intersect(bounds)
	if box.Empty() {
		fmt.Fprintf(stderr, "the box asked for is outside %s (%dx%d)\n",
			source, bounds.Dx(), bounds.Dy())
		return 1
	}

	zoomed := image.NewRGBA(image.Rect(0, 0, box.Dx()*scale, box.Dy()*scale))
	for row := 0; row < zoomed.Bounds().Dy(); row++ {
		for column := 0; column < zoomed.Bounds().Dx(); column++ {
			zoomed.Set(column, row, frame.At(box.Min.X+column/scale, box.Min.Y+row/scale))
		}
	}
	if err := encodePNG(destination, zoomed); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "(%d,%d)-(%d,%d) at %dx -> %s (%dx%d)\n",
		box.Min.X, box.Min.Y, box.Max.X, box.Max.Y, scale, destination,
		zoomed.Bounds().Dx(), zoomed.Bounds().Dy())
	return 0
}

// drawShrunk copies a frame at 1/shrink by dropping pixels. Nearest-neighbour
// rather than an average, because a contact sheet is read for what changed
// between frames and a blur is exactly what hides that; the same reason the
// canvas asks for pixelated scaling.
func drawShrunk(target *image.RGBA, at image.Point, source image.Image, size image.Point, shrink int) {
	origin := source.Bounds().Min
	for y := 0; y < size.Y; y++ {
		for x := 0; x < size.X; x++ {
			target.Set(at.X+x, at.Y+y, source.At(origin.X+x*shrink, origin.Y+y*shrink))
		}
	}
}

// frameDiff names the frames where two runs of the same key script disagree.
//
// This is how a change is held to what it actually did. Run the same script
// against the build before it and the build after it, and the first differing
// tick names the moment and its bounding box names the place. A screen that
// was reported as broken and comes back byte-identical is a screen the change
// did not touch, which is worth knowing before believing a fix.
func frameDiff(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: wfeature framediff <dirA> <dirB> [-limit N]")
		return 2
	}
	limit := 40
	for index := 2; index < len(args); index++ {
		if args[index] != "-limit" || index+1 >= len(args) {
			fmt.Fprintf(stderr, "unknown framediff option %q\n", args[index])
			return 2
		}
		value, err := strconv.Atoi(args[index+1])
		if err != nil || value <= 0 {
			fmt.Fprintf(stderr, "invalid -limit %q\n", args[index+1])
			return 2
		}
		limit = value
		index++
	}

	left, err := frameFiles(args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	differing, compared, reported := 0, 0, 0
	for _, frame := range left {
		other := filepath.Join(args[1], filepath.Base(frame.path))
		if _, err := os.Stat(other); err != nil {
			continue
		}
		compared++
		mine, err := readPNG(frame.path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		theirs, err := readPNG(other)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		box, differs := differenceBounds(mine, theirs)
		if !differs {
			continue
		}
		differing++
		if reported < limit {
			reported++
			fmt.Fprintf(stdout, "  tick%04d  (%d,%d)-(%d,%d)\n",
				frame.tick, box.Min.X, box.Min.Y, box.Max.X, box.Max.Y)
		}
	}
	fmt.Fprintf(stdout, "%d of %d frames present in both runs differ\n", differing, compared)
	if compared == 0 {
		fmt.Fprintln(stderr, "the two directories share no frame names")
		return 1
	}
	return 0
}

// frameStats reports what is actually on each frame: how many distinct colours
// it holds and how many pixels are lit. It exists because the two automatic
// judgments a whole-set sweep makes both have a hole a frame would close.
//
// **A sweep judges a run by its tick count and its lit-pixel count, and both
// pass a screen with nothing on it.** A KTF run is counted as working when any
// pixel is lit — so a title that fills the screen white passes with all 76,800
// of them — and an LGT run is counted as working when it finishes its ticks,
// so one that draws nothing at all passes too. Neither is visible without
// looking at a frame, and both were found by looking at one by hand. This is
// that look, as a number a script can read: one colour is a screen with
// nothing drawn on it, and no lit pixel is a black one.
//
// The exit status is the point: it is nonzero when **every** frame given is a
// single colour, so a sweep can ask this about a run's frames and get an
// answer without a person in the loop. A run whose frames are mixed exits
// zero, because a boot that starts black and then draws is a working boot.
func frameStats(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: wfeature framestats <framedir|frame.png> [-limit N]")
		return 2
	}
	limit := 40
	for index := 1; index < len(args); index++ {
		if args[index] != "-limit" || index+1 >= len(args) {
			fmt.Fprintf(stderr, "unknown framestats option %q\n", args[index])
			return 2
		}
		value, err := strconv.Atoi(args[index+1])
		if err != nil || value < 0 {
			fmt.Fprintf(stderr, "invalid -limit %q\n", args[index+1])
			return 2
		}
		limit = value
		index++
	}
	frames, err := statFrameFiles(args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	solid, blank, reported := 0, 0, 0
	for _, frame := range frames {
		picture, err := readPNG(frame.path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		colours, lit, total := frameColours(picture)
		if colours <= 1 {
			solid++
		}
		if lit == 0 {
			blank++
		}
		if limit == 0 || reported < limit {
			reported++
			fmt.Fprintf(stdout, "  %-16s colours=%-6d lit=%d of %d\n",
				filepath.Base(frame.path), colours, lit, total)
		}
	}
	fmt.Fprintf(stdout, "%d frames, %d of one colour, %d with nothing lit\n",
		len(frames), solid, blank)
	if solid == len(frames) {
		fmt.Fprintln(stderr, "every frame is a single colour")
		return 1
	}
	return 0
}

// statFrameFiles takes either a directory of `tickNNNN.png` frames or one PNG,
// because the two ways a run captures frames are `-framedir` and `-frame` and
// a question about what is on the screen is the same question for both.
func statFrameFiles(path string) ([]frameFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return frameFiles(path)
	}
	return []frameFile{{path: path}}, nil
}

// frameColours counts a frame's distinct colours and its lit pixels. Alpha is
// left out of both: every frame this writes is opaque, and a colour that
// differs only in an alpha nothing varies would count a screen as two-coloured
// when it is one.
func frameColours(picture image.Image) (int, int, int) {
	bounds := picture.Bounds()
	colours := make(map[uint32]struct{}, 256)
	lit := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			red, green, blue, _ := picture.At(x, y).RGBA()
			packed := red>>8<<16 | green>>8<<8 | blue>>8
			colours[packed] = struct{}{}
			if packed != 0 {
				lit++
			}
		}
	}
	return len(colours), lit, bounds.Dx() * bounds.Dy()
}

// differenceBounds is the smallest rectangle holding every differing pixel,
// which is what says whether a change touched the dialogue box or the whole
// screen. Two frames of different sizes differ everywhere.
func differenceBounds(left, right image.Image) (image.Rectangle, bool) {
	if left.Bounds() != right.Bounds() {
		return left.Bounds(), true
	}
	bounds := left.Bounds()
	box := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			leftRed, leftGreen, leftBlue, _ := left.At(x, y).RGBA()
			rightRed, rightGreen, rightBlue, _ := right.At(x, y).RGBA()
			if leftRed == rightRed && leftGreen == rightGreen && leftBlue == rightBlue {
				continue
			}
			found = true
			box.Min.X = min(box.Min.X, x)
			box.Min.Y = min(box.Min.Y, y)
			box.Max.X = max(box.Max.X, x+1)
			box.Max.Y = max(box.Max.Y, y+1)
		}
	}
	return box, found
}

// frameFile is one `tick0042.png` and the tick it came from.
type frameFile struct {
	path string
	tick int
}

// frameFiles lists a framedir in tick order. Sorting by the number rather than
// by the name is what keeps a run of more than ten thousand ticks in order.
func frameFiles(directory string) ([]frameFile, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	frames := make([]frameFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".png") {
			continue
		}
		tick := 0
		if digits := strings.TrimSuffix(strings.TrimPrefix(name, "tick"), ".png"); digits != name {
			parsed, err := strconv.Atoi(digits)
			if err != nil {
				continue
			}
			tick = parsed
		}
		frames = append(frames, frameFile{path: filepath.Join(directory, name), tick: tick})
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("%s holds no tickNNNN.png frames", directory)
	}
	sort.Slice(frames, func(one, two int) bool { return frames[one].tick < frames[two].tick })
	return frames, nil
}

func readPNG(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoded, err := png.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return decoded, nil
}

func encodePNG(path string, source image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := png.Encode(file, source); err != nil {
		return err
	}
	return file.Close()
}

// tinyDigits is a 3x5 stroke font, one bitmap row per scanline. A contact
// sheet needs the tick under each frame and nothing else, so it carries its
// own digits rather than a font: the label has to survive being the only text
// on an image made of game frames.
var tinyDigits = [10][5]uint8{
	{0b111, 0b101, 0b101, 0b101, 0b111},
	{0b010, 0b110, 0b010, 0b010, 0b111},
	{0b111, 0b001, 0b111, 0b100, 0b111},
	{0b111, 0b001, 0b111, 0b001, 0b111},
	{0b101, 0b101, 0b111, 0b001, 0b001},
	{0b111, 0b100, 0b111, 0b001, 0b111},
	{0b111, 0b100, 0b111, 0b101, 0b111},
	{0b111, 0b001, 0b010, 0b010, 0b010},
	{0b111, 0b101, 0b111, 0b101, 0b111},
	{0b111, 0b101, 0b111, 0b001, 0b111},
}

func drawTinyNumber(target *image.RGBA, x, y, value int) {
	text := strconv.Itoa(value)
	ink := color.RGBA{0xdc, 0xdc, 0x78, 0xff}
	for index, symbol := range text {
		if symbol < '0' || symbol > '9' {
			continue
		}
		glyph := tinyDigits[symbol-'0']
		for row := 0; row < 5; row++ {
			for column := 0; column < 3; column++ {
				if glyph[row]&(1<<(2-column)) == 0 {
					continue
				}
				target.Set(x+index*4+column, y+row, ink)
			}
		}
	}
}
