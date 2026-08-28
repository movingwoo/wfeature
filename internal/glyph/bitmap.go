package glyph

import (
	"image"
	"sync"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/movingwoo/wfeature/fonts"
)

// Bitmap is a rendered glyph positioned relative to the text baseline: row r
// draws at baselineY-Ascent+r, and column c lands at glyphLeft+c.
//
// It carries the glyph twice. Rows is the one-bit view, where bit
// (Width-1-column) of a row lights that pixel, and it is what the renderers
// that plot a pixel or nothing use. Alpha is how much of each pixel the outline
// actually covers, row-major and Width wide, which is what a renderer needs to
// draw a glyph that does not land on whole pixels — see Face for when that
// happens and why the difference is visible.
type Bitmap struct {
	Rows    []uint32
	Alpha   []uint8
	Width   int
	Ascent  int
	Advance int
}

// Coverage answers how much of one pixel the glyph outline covers, 0 for none
// and 255 for all of it. Out-of-range positions are uncovered.
func (bitmap Bitmap) Coverage(row, column int) uint8 {
	if row < 0 || column < 0 || column >= bitmap.Width {
		return 0
	}
	offset := row*bitmap.Width + column
	if offset >= len(bitmap.Alpha) {
		return 0
	}
	return bitmap.Alpha[offset]
}

// A Face is one font on one pixel grid. Two exist because the screens did: a
// 240x320 phone was written against a 16-pixel Korean face and a 176x220 one
// against a 12-pixel face. The size is not decoration — a game lays its menus
// out in the metrics the font it was written against reports, so drawing a
// 176x220 game's text at 16 overflows boxes the game sized itself.
//
// Each face uses the font that is exact at its size rather than one font
// scaled to both. A pixel font is only itself on its own grid: NeoDGM is drawn
// on a 16-unit em, and asking it for 12 pixels puts every two-unit stroke on a
// pixel and a half, which a renderer either rounds up — strokes a pixel too
// heavy in some places and not others, counters filled in, the whole face
// reading bolder and muddier than it is — or blends, which only softens it.
// Galmuri9 is exact at 10 pixels, where its ink stands as tall as NeoDGM's does
// scaled to 12 — the size those titles lay out for. See the fonts package for
// what Galmuri is and, more to the point, what it is not.
type Face struct {
	data        []byte
	pixelsPerEm int
	// Ascent and Descent are the face's shared text metrics: rows above the
	// baseline and rows below it, summing to the line height.
	Ascent  int
	Descent int
	// authoredLatin renders ASCII from the 5x7 table rather than from the font.
	// The large face does, because those shapes are what the platform tests
	// pin down; the handset face does not, because a 7-pixel Latin letter
	// beside a 9-pixel Korean one is two sizes of text on one line.
	authoredLatin bool
	// baselineDrop moves this font's glyphs down relative to a baseline,
	// because fonts disagree about where the baseline sits inside a Korean
	// syllable and the games do not ask. A native title positions text by
	// handing MC_grpDrawString a baseline it worked out for the face it was
	// written against, and that face put part of the syllable below the line —
	// NeoDGM still does, by two rows, at every size. Galmuri9 puts none of it
	// below. Drawn as-is its text rides two rows high in every box a game
	// centres, which is measurable: a menu bar 13 rows tall gets nine rows of
	// text against its top edge and four rows of space underneath.
	baselineDrop int

	once  sync.Once
	value font.Face

	cacheMu sync.Mutex
	cache   map[rune]Bitmap
}

// The two faces, each using the font that is exact at its size: NeoDGM at 16
// pixels, Galmuri9 at 10. Neither covers a pixel by halves, so neither needs
// the coverage blending Bitmap carries — that is there for the case where a
// face is asked for a size its design cannot hit.
//
// What makes one face smaller than the other is the ink, not the em: Korean
// glyphs stand 11 pixels above the baseline on the large face and 9 on the
// handset face.
//
// The handset face reports 11, and its ascent puts the nine rows of a Korean
// syllable one row below the top of that box and one above the bottom. Latin
// descends three rows past the baseline once the drop below is applied, which
// is exactly the descent, so nothing overflows either edge.
var (
	faceLarge   = &Face{data: fonts.NeoDGM, pixelsPerEm: 16, Ascent: 12, Descent: 4, authoredLatin: true}
	faceHandset = &Face{data: fonts.Galmuri9, pixelsPerEm: 10, Ascent: 8, Descent: 3, baselineDrop: 2}
)

// PixelAscent and PixelDescent are the 16-dot face's metrics, the shape of
// text on the 240x320 screens most of the library was written for.
const (
	PixelAscent  = 12
	PixelDescent = 4
)

// Default is the 16-dot face, which is what a platform with nothing to say
// about the screen it was built for gets.
func Default() *Face { return faceLarge }

// Handset is the face the handset games' own layouts were written against: a
// line 11 pixels tall with 9 of Korean ink above the baseline. It is one face
// for every screen size, because the screen a game declares turns out not to
// predict the font it expects — see the KTF runtime's fontFace for the title
// that settles it.
func Handset() *Face { return faceHandset }

// Height is the face's line height, ascent plus descent.
func (face *Face) Height() int { return face.Ascent + face.Descent }

// Covered reports whether a character has a runtime-authored 5x7 pattern
// (after ASCII case folding), the shapes the platform tests pin down.
func Covered(character rune) bool {
	if unicode.IsLower(character) && character <= unicode.MaxASCII {
		character = unicode.ToUpper(character)
	}
	_, ok := patterns[character]
	return ok
}

// Render returns the glyph for any character from the 16-dot face, for the
// platforms that have no screen size to choose one with.
func Render(character rune) Bitmap { return faceLarge.Render(character) }

// Render returns the glyph for any character. On a face that keeps the
// authored Latin, ASCII comes from the 5x7 table with its exact shape, since
// those are the shapes the platform tests pin down; everything else rasterizes
// from this face's font on this face's grid, and characters neither source
// covers keep the deterministic codepoint-marked box from Pattern.
func (face *Face) Render(character rune) Bitmap {
	// A NUL is padding, not a character. A title of this era keeps its
	// dialogue in fixed-length buffers and hands the whole buffer to
	// drawString, so the text it draws ends in as many NULs as the line was
	// short — and the codepoint-marked box below is exactly the wrong answer
	// for them: a run of boxes after every line, and a stringWidth wide enough
	// to centre the line off the screen. One face rendered them and one did
	// not, because the 16-dot font happens to carry a blank glyph at zero and
	// the handset font does not, which is not a difference a title should be
	// able to see. It reads as nothing, and takes no width, on both.
	if character == 0 {
		return Bitmap{}
	}
	if character == ' ' && face.authoredLatin {
		return Bitmap{Advance: 3}
	}
	if face.authoredLatin && Covered(character) {
		return patternBitmap(Pattern(character))
	}
	face.cacheMu.Lock()
	defer face.cacheMu.Unlock()
	if bitmap, ok := face.cache[character]; ok {
		return bitmap
	}
	bitmap, ok := face.renderTTF(character)
	if !ok {
		bitmap = patternBitmap(Pattern(character))
	}
	if face.cache == nil {
		face.cache = make(map[rune]Bitmap)
	}
	face.cache[character] = bitmap
	return bitmap
}

// patternBitmap converts a 5x7 pattern into the trimmed Bitmap form the
// legacy renderers produce: origin at the first used column, one empty
// column of advance padding, and the shared 7-row ascent.
func patternBitmap(pattern [7]byte) Bitmap {
	first, last := Columns(pattern)
	width := last - first + 1
	rows := make([]uint32, len(pattern))
	// An authored pattern is drawn pixel by pixel rather than scaled from an
	// outline, so every pixel it names is a whole one.
	coverage := make([]uint8, len(pattern)*width)
	for row, bits := range pattern {
		rows[row] = uint32(bits>>uint(5-1-last)) & (1<<uint(width) - 1)
		for column := 0; column < width; column++ {
			if rows[row]&(1<<uint(width-1-column)) != 0 {
				coverage[row*width+column] = 0xff
			}
		}
	}
	return Bitmap{Rows: rows, Alpha: coverage, Width: width, Ascent: 7, Advance: width + 1}
}

func (face *Face) ttfFace() font.Face {
	face.once.Do(func() {
		parsed, err := opentype.Parse(face.data)
		if err != nil {
			return
		}
		rendered, err := opentype.NewFace(parsed, &opentype.FaceOptions{
			Size:    float64(face.pixelsPerEm),
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err != nil {
			return
		}
		face.value = rendered
	})
	return face.value
}

// renderTTF rasterizes one character from the embedded face and thresholds
// the coverage mask to one bit per pixel.
func (face *Face) renderTTF(character rune) (Bitmap, bool) {
	rendered := face.ttfFace()
	if rendered == nil {
		return Bitmap{}, false
	}
	rectangle, mask, maskPoint, advance, ok := rendered.Glyph(fixed.Point26_6{}, character)
	if !ok {
		return Bitmap{}, false
	}
	width := rectangle.Max.X
	if width < rectangle.Dx() {
		width = rectangle.Dx()
	}
	if width <= 0 || width > 32 || rectangle.Dy() <= 0 {
		return Bitmap{Advance: advance.Round()}, true
	}
	alpha, ok := mask.(*image.Alpha)
	if !ok {
		return Bitmap{}, false
	}
	rows := make([]uint32, rectangle.Dy())
	coverage := make([]uint8, rectangle.Dy()*width)
	for y := 0; y < rectangle.Dy(); y++ {
		for x := 0; x < rectangle.Dx(); x++ {
			sample := alpha.AlphaAt(maskPoint.X+x, maskPoint.Y+y).A
			column := rectangle.Min.X + x
			if column < 0 || column >= width {
				continue
			}
			coverage[y*width+column] = sample
			if sample >= 0x80 {
				rows[y] |= 1 << uint(width-1-column)
			}
		}
	}
	return Bitmap{
		Rows:    rows,
		Alpha:   coverage,
		Width:   width,
		Ascent:  -rectangle.Min.Y - face.baselineDrop,
		Advance: advance.Round(),
	}, true
}
