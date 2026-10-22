package ktf

import (
	"encoding/binary"
	"fmt"
	"image/color"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/glyph"
)

// The screen draws text, and until now this platform did not.
//
// The 2005 title draws its own glyphs as runs of 1x1 rectangles and needs no
// font from anyone, which is why the slot it asks this of was read as a status
// line the handset shows somewhere of its own and recorded rather than drawn.
// It is not that: it is the display's own text call, and two modules of two
// generations agree on its shape.
//
//	[sp+4] = y ; [sp] = x ; [sp+8] = 0 ; [sp+0xc] = flags
//	r0 = display ; r1 = font ; r2 = text ; r3 = -1
//	bl veneer
//
// So it is `DrawText(display, font, text, count, x, y, background, flags)`,
// which is the shape of the specification's own call. What each argument is
// comes off the two call sites rather than off the name:
//
//   - **count is -1 at both**, which is what a run of text with a terminator
//     rather than a length is. The 2005 module has not been seen to pass any
//     other count, and one it passed is taken as a number of bytes. A later
//     module passes 14 for "ne button game" and the run is fourteen halfwords
//     long — the specification's count of wide characters, each holding one
//     EUC-KR character as below — so that generation's count is halfwords.
//   - **the text is EUC-KR bytes**, not the specification's wide characters:
//     one module builds its lines by copying two bytes at a time out of a
//     string in its own image, and those bytes decode as Korean and read as
//     sentences.
//   - **y is the top of the line, not a baseline.** One module lays three lines
//     out with `(screen height - 3 * line height) / 2` and steps by the line
//     height, which only centres a block if the y it passes is the top of it.
//   - **the background rectangle is what the text is aligned in**, and it is
//     clipped to it too. It is null at most call sites, which is the screen;
//     one later title passes the screen itself.
//   - **the flags are BREW's IDF_ bits, and they align the text.** `0x8020` is
//     IDF_TEXT_TRANSPARENT with IDF_ALIGN_CENTER, and `0x8220` adds
//     IDF_ALIGN_MIDDLE. An aligned call has its x or y ignored: the call that
//     passes the middle of the screen for both is the one asking to be centred
//     both ways, and a later title passes 0x8020 with x zero for the choices
//     it centres under its message box, and IDF_ALIGN_RIGHT, 0x40, for its
//     loading line. Every call seen is transparent, so text never clears the
//     ground behind it, and none sets a rectangle flag, so nothing is filled.
//
// See docs/history/ktf.md, "The screen draws text".
const (
	// nativeScreenFontMetrics answers a font's line height, and fills in an
	// ascent and a descent through two pointers when they are not null. A
	// module calls it with a font and two zeroes and uses the answer as the
	// height of one line.
	nativeScreenFontMetrics = 0x08
	// nativeScreenMeasure answers how wide a run of text is drawn:
	// MeasureText(display, font, text, count, most, fits), the call between
	// the metrics and the drawing in the specification's table. A later
	// module passes it the halfword runs it draws, with -1 for the count and
	// for the most and null for fits, and divides by what comes back when it
	// lays its help pages out; another of its call sites centres a line with
	// it and draws at six pixels a character when it answers zero.
	nativeScreenMeasure = 0x0c
)

// The colour items a module sets before it draws, and which of them the ink is.
//
// **The pairing is a reading**, and this is the one where both local titles are
// legible. One module sets item 1 to black and item 2 to white and then clears
// its screen, which comes out white — so item 2 is the ground it clears to and
// item 1 is the ink on it. The other sets neither and clears the same way, so
// its screen is white too and its ink has to be the dark one.
//
// What would say this is wrong is a title whose text comes out the colour of
// what it is drawn on. That is what "invisible" looked like the first time
// round: white ink on the white a clear leaves behind.
const nativeTextColourItem = 1

// BREW's text alignments, from the flag word: one horizontal and one vertical,
// each against the background rectangle. IDF_ALIGN_FILL and IDF_ALIGN_SPREAD,
// which the specification says its SDK does not support, leave the point as it
// was given.
const (
	nativeAlignHorizontal = 0x0f0
	nativeAlignLeft       = 0x010
	nativeAlignCenter     = 0x020
	nativeAlignRight      = 0x040
	nativeAlignVertical   = 0xf00
	nativeAlignTop        = 0x100
	nativeAlignMiddle     = 0x200
	nativeAlignBottom     = 0x400
)

// nativeDefaultTextColour is what a title that sets no colour gets. It is dark
// because the ground a clear leaves is light.
var nativeDefaultTextColour = color.RGBA{A: 0xff}

// maxNativeTextLength bounds a run of text read out of guest memory when the
// caller gives a count rather than a terminator.
const maxNativeTextLength = 4 << 10

// installText registers the display's text calls.
func (platform *NativePlatform) installText() {
	surface := nativeInterfaceSurface(nativeInterfaceApplication)
	platform.client.Serve(surface, nativeScreenText, platform.drawText)
	platform.client.Serve(surface, nativeScreenFontMetrics, platform.fontMetrics)
	platform.client.Serve(surface, nativeScreenMeasure, platform.measureText)
}

// textFace is the face this platform draws a native package's text with. It is
// the same one the descriptor package's runtime uses, for the same reason: the
// screen a title declares turns out not to predict the font it expects, so
// there is one face rather than one per size.
func (platform *NativePlatform) textFace() *glyph.Face { return glyph.Handset() }

// fontMetrics answers the line height, and the ascent and descent behind it.
func (platform *NativePlatform) fontMetrics(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	face := platform.textFace()
	memory := platform.client.core.Memory()
	for _, out := range []struct {
		address uint32
		value   uint32
	}{
		{address: arguments[2], value: uint32(face.Ascent)},
		{address: arguments[3], value: uint32(face.Descent)},
	} {
		if out.address == 0 {
			continue
		}
		word := []byte{byte(out.value), byte(out.value >> 8), byte(out.value >> 16), byte(out.value >> 24)}
		if err := memory.Write(out.address, word); err != nil {
			return 0, fmt.Errorf("write a KTF native font metric to %#x: %w", out.address, err)
		}
	}
	return uint32(face.Height()), nil
}

// measureText answers how wide a run is drawn, and through fits, when it is not
// null, how many of its characters are drawn within most pixels. A most that
// is negative is no limit, and then the answer is the whole run's width; with a
// limit it is the width of the characters that fit. Each character is as wide
// as drawText advances past it, so a line laid out with this is the line drawn.
func (platform *NativePlatform) measureText(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	address, count := arguments[2], int32(arguments[3])
	stacked := platform.varargs(thread, 4)
	most, err := stacked(1)
	if err != nil {
		return 0, err
	}
	fits, err := stacked(1)
	if err != nil {
		return 0, err
	}
	width, fitted := 0, 0
	if address != 0 {
		// A run that cannot be read measures as nothing, as it draws as
		// nothing.
		if raw, err := platform.readText(address, count); err == nil {
			face := platform.textFace()
			for _, character := range decodeEUCKR(raw) {
				advance := face.Render(character).Advance
				if int32(most) >= 0 && width+advance > int(int32(most)) {
					break
				}
				width += advance
				fitted++
			}
		}
	}
	if uint32(fits) != 0 {
		word := binary.LittleEndian.AppendUint32(nil, uint32(fitted))
		if err := platform.client.core.Memory().Write(uint32(fits), word); err != nil {
			return 0, fmt.Errorf("write a KTF native text fit to %#x: %w", uint32(fits), err)
		}
	}
	return uint32(width), nil
}

// drawText draws one run of text on the screen.
func (platform *NativePlatform) drawText(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	address, count := arguments[2], int32(arguments[3])
	stacked := platform.varargs(thread, 4)
	next := func() (int32, error) {
		value, err := stacked(1)
		return int32(uint32(value)), err
	}
	x, err := next()
	if err != nil {
		return 0, err
	}
	y, err := next()
	if err != nil {
		return 0, err
	}
	background, err := stacked(1)
	if err != nil {
		return 0, err
	}
	flags, err := stacked(1)
	if err != nil {
		return 0, err
	}
	if address == 0 {
		return 1, nil
	}
	frame := platform.screen.frame
	area, bounds := frame.Bounds(), frame.Bounds()
	if background != 0 {
		// A rectangle that cannot be read is a run not drawn, for the reason
		// a run that cannot be read is below.
		record := make([]byte, 8)
		if err := platform.client.core.Memory().Read(uint32(background), record); err != nil {
			return 1, nil
		}
		area = nativeRectangle(record)
		bounds = bounds.Intersect(area)
	}

	raw, err := platform.readText(address, count)
	if err != nil {
		// A run of text that cannot be read is not worth ending a run over:
		// the title has no way to be told, and a screen missing a word is
		// better than a session missing its end.
		return 1, nil
	}
	text := decodeEUCKR(raw)
	// The messages a run reports are what they always were. They are the same
	// text, and a report that lists what the title said is worth keeping now
	// that the screen shows it too.
	platform.messages = keepFirst(platform.messages, text)

	ink := nativeDefaultTextColour
	if packed, ok := platform.colours[nativeTextColourItem]; ok {
		ink = nativeColour(packed)
	}
	face := platform.textFace()
	width := 0
	for _, character := range text {
		width += face.Render(character).Advance
	}
	switch uint32(flags) & nativeAlignHorizontal {
	case nativeAlignLeft:
		x = int32(area.Min.X)
	case nativeAlignCenter:
		x = int32(area.Min.X + (area.Dx()-width)/2)
	case nativeAlignRight:
		x = int32(area.Max.X - width)
	}
	switch uint32(flags) & nativeAlignVertical {
	case nativeAlignTop:
		y = int32(area.Min.Y)
	case nativeAlignMiddle:
		y = int32(area.Min.Y + (area.Dy()-face.Height())/2)
	case nativeAlignBottom:
		y = int32(area.Max.Y - face.Height())
	}
	cursor := x
	for _, character := range text {
		bitmap := face.Render(character)
		if character != ' ' && character != '\t' {
			for row := range bitmap.Rows {
				for column := 0; column < bitmap.Width; column++ {
					coverage := bitmap.Coverage(row, column)
					if coverage == 0 {
						continue
					}
					// y is the top of the line and the glyph sits on the
					// face's baseline inside it.
					atX := int(cursor) + column
					atY := int(y) + face.Ascent - bitmap.Ascent + row
					if atX < bounds.Min.X || atY < bounds.Min.Y || atX >= bounds.Max.X || atY >= bounds.Max.Y {
						continue
					}
					frame.SetRGBA(atX, atY, nativeBlendInk(frame.RGBAAt(atX, atY), ink, coverage))
				}
			}
		}
		cursor += int32(bitmap.Advance)
	}
	platform.screen.draws++
	return 1, nil
}

// readText reads the run the caller named. A count of -1 is a terminator,
// which is what both local call sites pass. Any other count is bytes for the
// 2005 module and halfwords for a later one, whose halfwords are read by the
// same rule as a terminated run's and end early at a zero one.
//
// **A terminator here is a zero halfword, not a zero byte**, and reading it as
// a byte cost three quarters of a sentence. One module builds its lines a
// halfword at a time — a Korean character is two bytes and fills one, and a
// space is one byte and fills one with a zero after it — so its line reads
//
//	c0ce c1f5 2000 bfe4 c3bb ... 2e00 0000
//	인   증   ' '  요   청       '.'  end
//
// and a reader that stops at the first zero byte stops at the space. The other
// module hands over plain bytes with a zero after the last one. Both are read
// by the same rule, which is what the shape of the two says rather than a
// choice between them: **a zero ends the run when it is on a halfword boundary
// or when the byte after it is zero too; anywhere else it is the pad of a
// single-byte character.** The pads are then dropped, which is safe because no
// byte of an EUC-KR character is zero.
func (platform *NativePlatform) readText(address uint32, count int32) ([]byte, error) {
	if count < 0 {
		raw, err := platform.readString(address)
		if err != nil {
			return nil, err
		}
		return platform.readPaddedText(address, raw)
	}
	if count == 0 {
		return nil, nil
	}
	if count > maxNativeTextLength {
		return nil, fmt.Errorf("KTF native text of %d bytes at %#x, limit %d", count, address, maxNativeTextLength)
	}
	length, halfwords := int(count), platform.archive.AsksForInterfaceVersion()
	if halfwords {
		length *= 2
	}
	raw := make([]byte, length)
	if err := platform.client.core.Memory().Read(address, raw); err != nil {
		return nil, fmt.Errorf("read %d bytes of KTF native text at %#x: %w", length, address, err)
	}
	if halfwords {
		return unpadHalfwords(raw), nil
	}
	return raw, nil
}

// readPaddedText carries on past a zero byte that is the pad of a single-byte
// character. `first` is what stopping at the first zero already gave.
func (platform *NativePlatform) readPaddedText(address uint32, first []byte) ([]byte, error) {
	// A zero on a halfword boundary is the terminator, and so is one at the end
	// of what could be read at all.
	if len(first)%2 == 0 {
		return first, nil
	}
	raw := make([]byte, maxNativeTextLength)
	if err := platform.client.core.Memory().Read(address, raw); err != nil {
		// The buffer does not reach that far, which says nothing is past the
		// zero: what was read up to it is the run.
		return first, nil
	}
	return unpadHalfwords(raw), nil
}

// unpadHalfwords reads a run a halfword at a time up to a zero one, dropping
// the zero byte that pads a single-byte character.
func unpadHalfwords(raw []byte) []byte {
	run := make([]byte, 0, len(raw))
	for offset := 0; offset+1 < len(raw); offset += 2 {
		high, low := raw[offset], raw[offset+1]
		if high == 0 && low == 0 {
			break
		}
		if high != 0 {
			run = append(run, high)
		}
		if low != 0 {
			run = append(run, low)
		}
	}
	return run
}

// nativeBlendInk mixes ink into what is under it. The face does not cover a
// pixel by halves at its own size, but it answers coverage rather than a bit
// so that a size its design cannot hit still reads as text rather than as a
// heavier version of itself.
func nativeBlendInk(under, ink color.RGBA, coverage uint8) color.RGBA {
	if coverage == 0xff {
		return ink
	}
	mix := func(over, under uint8) uint8 {
		return uint8((uint32(over)*uint32(coverage) + uint32(under)*uint32(0xff-coverage)) / 0xff)
	}
	return color.RGBA{
		R: mix(ink.R, under.R),
		G: mix(ink.G, under.G),
		B: mix(ink.B, under.B),
		A: 0xff,
	}
}
