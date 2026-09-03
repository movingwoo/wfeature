package ktf

import (
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
//     rather than a length is. A count that is not -1 has not been seen, and is
//     taken as a number of bytes.
//   - **the text is EUC-KR bytes**, not the specification's wide characters:
//     one module builds its lines by copying two bytes at a time out of a
//     string in its own image, and those bytes decode as Korean and read as
//     sentences.
//   - **y is the top of the line, not a baseline.** One module lays three lines
//     out with `(screen height - 3 * line height) / 2` and steps by the line
//     height, which only centres a block if the y it passes is the top of it.
//   - **the background rectangle is null at both**, so nothing is filled.
//   - **the flags are carried and not acted on.** They are `0x8020` at one call
//     site and `0x8220` at the other, and the one extra bit belongs to the call
//     that passes the middle of the screen for x and y — which reads as an
//     alignment. That is a reading of one bit from two samples, so it is
//     written down rather than acted on: text lands at the point it was given.
//
// See docs/ktf.md, "The screen draws text".
const (
	// nativeScreenFontMetrics answers a font's line height, and fills in an
	// ascent and a descent through two pointers when they are not null. A
	// module calls it with a font and two zeroes and uses the answer as the
	// height of one line.
	nativeScreenFontMetrics = 0x08
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
	if address == 0 {
		return 1, nil
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
	platform.messages = append(platform.messages, text)

	ink := nativeDefaultTextColour
	if packed, ok := platform.colours[nativeTextColourItem]; ok {
		ink = nativeColour(packed)
	}
	face := platform.textFace()
	frame := platform.screen.frame
	bounds := frame.Bounds()
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
// which is what both local call sites pass.
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
	raw := make([]byte, count)
	if err := platform.client.core.Memory().Read(address, raw); err != nil {
		return nil, fmt.Errorf("read %d bytes of KTF native text at %#x: %w", count, address, err)
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
	return run, nil
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
