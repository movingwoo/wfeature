package glyph

import "testing"

// The 5x7 path must reproduce the legacy trimmed-pattern rendering exactly:
// platform tests assert those pixels.
func TestRenderKeepsAuthoredPatternShape(t *testing.T) {
	bitmap := Render('A')
	pattern := Pattern('A')
	first, last := Columns(pattern)
	if bitmap.Width != last-first+1 {
		t.Fatalf("width = %d, want %d", bitmap.Width, last-first+1)
	}
	if bitmap.Ascent != 7 || bitmap.Advance != bitmap.Width+1 {
		t.Fatalf("metrics = ascent %d advance %d, want 7 and %d", bitmap.Ascent, bitmap.Advance, bitmap.Width+1)
	}
	for row, bits := range pattern {
		for column := first; column <= last; column++ {
			source := bits&(1<<uint(4-column)) != 0
			target := bitmap.Rows[row]&(1<<uint(bitmap.Width-1-(column-first))) != 0
			if source != target {
				t.Fatalf("row %d column %d = %t, want %t", row, column, target, source)
			}
		}
	}
}

func TestRenderSpaceAdvance(t *testing.T) {
	bitmap := Render(' ')
	if bitmap.Width != 0 || bitmap.Advance != 3 {
		t.Fatalf("space = width %d advance %d, want 0 and 3", bitmap.Width, bitmap.Advance)
	}
}

// Hangul must come from the embedded pixel face, not the codepoint box: a
// real glyph on the 16-pixel grid with lit pixels and a full-width advance.
func TestRenderRasterizesHangul(t *testing.T) {
	for _, character := range []rune{'한', '글', '가', '힣'} {
		bitmap := Render(character)
		if bitmap.Advance < 10 || bitmap.Advance > 17 {
			t.Fatalf("%q advance = %d, want a full-width value", character, bitmap.Advance)
		}
		if bitmap.Ascent < 8 || bitmap.Ascent > 16 {
			t.Fatalf("%q ascent = %d, want the pixel-face ascent range", character, bitmap.Ascent)
		}
		lit := 0
		for _, row := range bitmap.Rows {
			for column := 0; column < bitmap.Width; column++ {
				if row&(1<<uint(bitmap.Width-1-column)) != 0 {
					lit++
				}
			}
		}
		if lit < 10 {
			t.Fatalf("%q has %d lit pixels, want a real glyph", character, lit)
		}
	}
}

// Repeated lookups must come from the cache with identical content.
func TestRenderCachesTTFGlyphs(t *testing.T) {
	first := Render('한')
	second := Render('한')
	if first.Width != second.Width || first.Ascent != second.Ascent || first.Advance != second.Advance {
		t.Fatalf("cached render differs: %+v vs %+v", first, second)
	}
	for index := range first.Rows {
		if first.Rows[index] != second.Rows[index] {
			t.Fatalf("cached row %d differs", index)
		}
	}
}

// The two faces and what each is for. The handset face is the one every
// handset game's text is drawn with; the large face is what a platform with
// nothing to say about its screen falls back to.
func TestFaceMetrics(t *testing.T) {
	if got := Default().Height(); got != 16 {
		t.Fatalf("large face height = %d, want 16", got)
	}
	if got := Handset().Height(); got != 11 {
		t.Fatalf("handset face height = %d, want 11", got)
	}
}

// The ink has to sit centred in the line box, and it is Korean ink that
// decides: these are Korean games, a menu line is Korean, and Korean has no
// descenders. Measuring the slack against the deepest glyph of any script
// hides the problem — Latin lowercase reaches a row the Korean never uses, so
// a box that looks balanced for "g" still draws 한글 a row high.
func TestHandsetKoreanInkIsCentredInItsLineBox(t *testing.T) {
	face := Handset()
	for _, character := range []rune{'한', '글', '가', '체', '복', '했'} {
		bitmap := face.Render(character)
		above := face.Ascent - bitmap.Ascent
		below := face.Height() - face.Ascent - (len(bitmap.Rows) - bitmap.Ascent)
		if above != below {
			t.Fatalf("%q sits %d rows below the top of its line box and %d above the bottom; it should be even",
				character, above, below)
		}
		if above < 0 || below < 0 {
			t.Fatalf("%q overflows its line box: %d above, %d below", character, above, below)
		}
	}
}

// The two faces have to agree about where the baseline sits inside a Korean
// syllable, because a native title positions text by handing the renderer a
// baseline it worked out for the face it was written against and never asks
// what that face was. Galmuri9 draws a syllable entirely above the baseline
// where NeoDGM puts two rows below it; the handset face corrects for that, and
// a menu bar the game centres its own text in is where the difference shows.
func TestBothFacesPutTheSameKoreanRowsBelowTheBaseline(t *testing.T) {
	for _, character := range []rune{'한', '글', '가', '체'} {
		large := Default().Render(character)
		handset := Handset().Render(character)
		largeBelow := len(large.Rows) - large.Ascent
		handsetBelow := len(handset.Rows) - handset.Ascent
		if largeBelow != handsetBelow {
			t.Fatalf("%q sits %d rows below the baseline on the large face and %d on the handset face",
				character, largeBelow, handsetBelow)
		}
	}
}

// Latin descends where Korean does not, and the box has to hold that too.
func TestHandsetDescendersStayInTheLineBox(t *testing.T) {
	face := Handset()
	for _, character := range []rune{'g', 'y', 'p', 'j'} {
		bitmap := face.Render(character)
		if below := face.Height() - face.Ascent - (len(bitmap.Rows) - bitmap.Ascent); below < 0 {
			t.Fatalf("%q descends %d rows past the bottom of its line box", character, -below)
		}
	}
}

// The two faces must differ where it shows: Hangul is drawn at each face's own
// size, so the smaller one takes fewer columns and fewer rows.
func TestFaceSizesRenderHangulOnTheirOwnGrid(t *testing.T) {
	small := Handset().Render('한')
	large := Default().Render('한')
	if small.Advance != 10 || large.Advance != 16 {
		t.Fatalf("advances = %d and %d, want 10 and 16", small.Advance, large.Advance)
	}
	// The ink is the part that reads as big or small. A face whose glyphs are
	// merely narrower is the trap Galmuri11 was: 11 rows of ink at a 12-pixel
	// em is exactly as tall as NeoDGM at 16, so the text still looks oversized.
	// Nine rows against thirteen is the size difference; where those rows sit
	// relative to the baseline is a separate question, pinned above.
	if len(small.Rows) >= len(large.Rows) {
		t.Fatalf("ink heights = %d and %d rows, want the handset face to be shorter",
			len(small.Rows), len(large.Rows))
	}
}

// Latin has to be the same size as the Korean beside it. The large face keeps
// the authored 5x7 shapes, which platform tests pin down and which are shorter
// than its Hangul; the small face takes its Latin from its own font, so a line
// mixing the two is one size of text rather than two.
func TestSmallFaceLatinMatchesItsHangul(t *testing.T) {
	small := Handset()
	latin, hangul := small.Render('A'), small.Render('한')
	if len(latin.Rows) != len(hangul.Rows) || latin.Ascent != hangul.Ascent {
		t.Fatalf("Latin is %d rows at ascent %d, Korean is %d at %d — one line, two sizes",
			len(latin.Rows), latin.Ascent, len(hangul.Rows), hangul.Ascent)
	}

	large := Default()
	authored := patternBitmap(Pattern('A'))
	if got := large.Render('A'); got.Width != authored.Width || got.Ascent != authored.Ascent {
		t.Fatalf("large-face Latin = %+v, want the authored %+v", got, authored)
	}
}

// Each face uses the font that is exact at its size, so neither ever covers a
// pixel by halves. This is the property that keeps text crisp, and the reason
// the wrong font at the wrong size was visible as a bolder, muddier face: a
// half-covered pixel has to be rounded up or blended, and both are worse than
// not having one. A failure here means a face is being asked for a size its
// design cannot hit.
func TestNeitherFaceCoversAPixelByHalves(t *testing.T) {
	for _, face := range []*Face{Default(), Handset()} {
		for _, character := range []rune{'한', '글', '가', '힣', '체', '복', '했', 'A', 'g', '0', '7'} {
			bitmap := face.Render(character)
			for row := range bitmap.Rows {
				for column := 0; column < bitmap.Width; column++ {
					coverage := bitmap.Coverage(row, column)
					if coverage != 0 && coverage != 0xff {
						t.Fatalf("%d-pixel face: %q at %d,%d covers %d, want 0 or 255",
							face.pixelsPerEm, character, row, column, coverage)
					}
				}
			}
		}
	}
}

// Coverage and the one-bit view have to agree, since renderers use both: a
// pixel is set in Rows exactly when the outline covers at least half of it.
func TestCoverageAgreesWithTheOneBitView(t *testing.T) {
	for _, face := range []*Face{Default(), Handset()} {
		for _, character := range []rune{'한', '가', 'A', '9'} {
			bitmap := face.Render(character)
			for row, bits := range bitmap.Rows {
				for column := 0; column < bitmap.Width; column++ {
					lit := bits&(1<<uint(bitmap.Width-1-column)) != 0
					if lit != (bitmap.Coverage(row, column) >= 0x80) {
						t.Fatalf("%q at %d,%d: bit %t, coverage %d", character, row, column, lit, bitmap.Coverage(row, column))
					}
				}
			}
		}
	}
}

// Reading outside a glyph is uncovered rather than a panic: the renderers walk
// a glyph's box, and a trimmed glyph is narrower than the box it sits in.
func TestCoverageOutsideTheGlyphIsEmpty(t *testing.T) {
	bitmap := Handset().Render('한')
	for _, position := range [][2]int{{-1, 0}, {0, -1}, {0, bitmap.Width}, {len(bitmap.Rows), 0}} {
		if coverage := bitmap.Coverage(position[0], position[1]); coverage != 0 {
			t.Fatalf("coverage at %v = %d, want 0", position, coverage)
		}
	}
}
