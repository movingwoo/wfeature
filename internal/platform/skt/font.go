package skt

import (
	"fmt"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/glyph"

	"github.com/movingwoo/wfeature/internal/jvm"
)

const (
	anchorHCenter  int32 = 1
	anchorVCenter  int32 = 2
	anchorLeft     int32 = 4
	anchorRight    int32 = 8
	anchorTop      int32 = 16
	anchorBottom   int32 = 32
	anchorBaseline int32 = 64

	fontSystem       int32 = 0
	fontMonospace    int32 = 32
	fontProportional int32 = 64
	fontPlain        int32 = 0
	fontBold         int32 = 1
	fontItalic       int32 = 2
	fontUnderlined   int32 = 4
	fontMedium       int32 = 0
	fontSmall        int32 = 8
	fontLarge        int32 = 16
)

// pixelFace is the face every SKVM glyph outside the authored 5x7 table —
// Hangul, and everything else non-ASCII — is drawn and measured with.
//
// It is the handset face, the same one KTF settled on, and not the 16-dot one
// this platform used to reach for through the package-level glyph.Render.
// These titles lay their menus out in the metrics the font they were written
// against reports, and those metrics are the small ones: a MIDP font this
// platform calls MEDIUM reports a height of 10, so a Korean syllable drawn on
// the 16-dot grid stands eleven rows above a baseline meant to carry eight.
// A local title's main menu shows exactly that — seven Korean entries whose
// glyphs are half again the height of the Latin numerals beside them, running
// into each other line to line while the numerals sit small and correct.
//
// The 5x7 table still answers for the characters it covers, so the Latin
// shapes the platform tests pin down are unchanged.
func pixelFace() *glyph.Face { return glyph.Handset() }

type fontKey struct {
	face  int32
	style int32
	size  int32
}

type fontData struct {
	fontKey
	scale    int
	height   int
	baseline int
}

func (runtime *Runtime) fontObject(face, style, size int32) *jvm.Object {
	key := fontKey{face: face, style: style, size: size}
	runtime.fontMu.Lock()
	defer runtime.fontMu.Unlock()
	if font := runtime.fonts[key]; font != nil {
		return font
	}
	font := &jvm.Object{
		ClassName: runtime.fontClassName(),
		Fields:    make(map[string]jvm.Value),
		Native:    newFontData(key),
	}
	runtime.fonts[key] = font
	return font
}

func newFontData(key fontKey) *fontData {
	font := &fontData{fontKey: key, scale: 1, height: 10, baseline: 8}
	switch key.size {
	case fontSmall:
		font.height = 8
		font.baseline = 7
	case fontLarge:
		font.scale = 2
		font.height = 16
		font.baseline = 14
	}
	return font
}

func (runtime *Runtime) getDefaultFont(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.ReferenceValue(runtime.fontObject(fontSystem, fontPlain, fontMedium)), nil
}

func (runtime *Runtime) getFontBySpecifier(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	specifier, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if specifier != 0 && specifier != 1 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "invalid font specifier")
	}
	return jvm.ReferenceValue(runtime.fontObject(fontSystem, fontPlain, fontMedium)), nil
}

func (runtime *Runtime) getFontByAttributes(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	face, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	style, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	size, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !validFontAttributes(face, style, size) {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "invalid font face, style, or size")
	}
	return jvm.ReferenceValue(runtime.fontObject(face, style, size)), nil
}

func (runtime *Runtime) getGraphicsFont(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(context.font), nil
}

func (runtime *Runtime) setGraphicsFont(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	fontObject, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if fontObject == nil {
		fontObject = runtime.fontObject(fontSystem, fontPlain, fontMedium)
	} else if _, err := fontReceiver(fontObject); err != nil {
		return jvm.VoidValue(), err
	}
	context.font = fontObject
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) getFontFace(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(font.face), nil
}

func (runtime *Runtime) getFontStyle(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(font.style), nil
}

func (runtime *Runtime) getFontSize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(font.size), nil
}

func (runtime *Runtime) isFontPlain(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return fontStyleFlag(arguments, 0)
}

func (runtime *Runtime) isFontBold(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return fontStyleFlag(arguments, fontBold)
}

func (runtime *Runtime) isFontItalic(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return fontStyleFlag(arguments, fontItalic)
}

func (runtime *Runtime) isFontUnderlined(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return fontStyleFlag(arguments, fontUnderlined)
}

func (runtime *Runtime) getFontHeight(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(font.height)), nil
}

func (runtime *Runtime) getFontBaseline(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(font.baseline)), nil
}

func (runtime *Runtime) getFontCharWidth(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	character, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(font.charAdvance(rune(uint16(character))))), nil
}

func (runtime *Runtime) getFontCharsWidth(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	units, err := characterArraySlice(arguments, 1, 2, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(font.textWidth(utf16.Decode(units)))), nil
}

func (runtime *Runtime) getFontStringWidth(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(font.textWidth([]rune(text)))), nil
}

func (runtime *Runtime) getFontSubstringWidth(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	units, err := stringUnitSlice(text, arguments, 2, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(font.textWidth(utf16.Decode(units)))), nil
}

func (runtime *Runtime) drawGraphicsChar(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	character, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.drawText(arguments, []rune{rune(uint16(character))}, 2)
}

func (runtime *Runtime) drawGraphicsChars(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	units, err := characterArraySlice(arguments, 1, 2, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.drawText(arguments, utf16.Decode(units), 4)
}

func (runtime *Runtime) drawGraphicsString(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	text, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.drawText(arguments, []rune(text), 2)
}

func (runtime *Runtime) drawGraphicsSubstring(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	text, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	units, err := stringUnitSlice(text, arguments, 2, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.drawText(arguments, utf16.Decode(units), 4)
}

func (runtime *Runtime) drawText(arguments []jvm.Value, text []rune, coordinateIndex int) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, err := intArgument(arguments, coordinateIndex)
	if err != nil {
		return jvm.VoidValue(), err
	}
	y, err := intArgument(arguments, coordinateIndex+1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	anchor, err := intArgument(arguments, coordinateIndex+2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	horizontal, vertical, err := normalizeAnchor(anchor, false)
	if err != nil {
		return jvm.VoidValue(), err
	}
	font, err := fontReceiver(context.font)
	if err != nil {
		return jvm.VoidValue(), err
	}
	left := int64(x) + int64(context.translateX)
	top := int64(y) + int64(context.translateY)
	textWidth := int64(font.textWidth(text))
	switch horizontal {
	case anchorHCenter:
		left -= textWidth / 2
	case anchorRight:
		left -= textWidth
	}
	switch vertical {
	case anchorBaseline:
		top -= int64(font.baseline)
	case anchorBottom:
		top -= int64(font.height)
	}
	context.withDestinationWrite(func() {
		font.render(context, text, left, top)
	})
	return jvm.VoidValue(), nil
}

func (font *fontData) textWidth(text []rune) int {
	width := 0
	for _, character := range text {
		width += font.charAdvance(character)
	}
	return width
}

func (font *fontData) charAdvance(character rune) int {
	if character == '\t' {
		return 4 * font.charAdvance(' ')
	}
	boldExtra := 0
	if font.style&fontBold != 0 {
		boldExtra = font.scale
	}
	if character == ' ' {
		if font.face == fontMonospace {
			return 6*font.scale + boldExtra
		}
		return 3*font.scale + boldExtra
	}
	if !glyph.Covered(character) {
		// Characters outside the authored 5x7 table (Hangul and other
		// non-ASCII text) measure by the embedded pixel face's advance.
		return pixelFace().Render(character).Advance*font.scale + boldExtra
	}
	if font.face == fontMonospace {
		return 6*font.scale + boldExtra
	}
	pattern := glyph.Pattern(character)
	first, last := glyph.Columns(pattern)
	return (last-first+2)*font.scale + boldExtra
}

func (font *fontData) render(context *graphicsContext, text []rune, left, top int64) {
	cursor := left
	bodyTop := top + int64(font.baseline-7*font.scale)
	for _, character := range text {
		advance := font.charAdvance(character)
		if character != ' ' && character != '\t' && !glyph.Covered(character) {
			// Pixel-face glyphs (Hangul and other non-ASCII) draw relative
			// to the same baseline the 5x7 body sits on, scaled like it.
			bitmap := pixelFace().Render(character)
			baseline := top + int64(font.baseline)
			for row, bits := range bitmap.Rows {
				for column := 0; column < bitmap.Width; column++ {
					if bits&(1<<uint(bitmap.Width-1-column)) == 0 {
						continue
					}
					pixelX := cursor + int64(column*font.scale)
					pixelY := baseline + int64((row-bitmap.Ascent)*font.scale)
					font.drawScaledPixel(context, pixelX, pixelY)
				}
			}
			if font.style&fontUnderlined != 0 {
				underlineY := top + int64(font.baseline)
				for x := int64(0); x < int64(advance); x++ {
					context.putColorPixel(cursor+x, underlineY)
				}
			}
			cursor += int64(advance)
			continue
		}
		if character != ' ' && character != '\t' {
			pattern := glyph.Pattern(character)
			first, _ := glyph.Columns(pattern)
			columnOrigin := 0
			if font.face != fontMonospace {
				columnOrigin = first
			}
			for row, bits := range pattern {
				italicShift := 0
				if font.style&fontItalic != 0 {
					italicShift = (6 - row) * font.scale / 3
				}
				for column := 0; column < 5; column++ {
					if bits&(1<<uint(4-column)) == 0 {
						continue
					}
					pixelX := cursor + int64((column-columnOrigin)*font.scale+italicShift)
					pixelY := bodyTop + int64(row*font.scale)
					font.drawScaledPixel(context, pixelX, pixelY)
				}
			}
		}
		if font.style&fontUnderlined != 0 {
			underlineY := top + int64(font.baseline)
			for x := int64(0); x < int64(advance); x++ {
				context.putColorPixel(cursor+x, underlineY)
			}
		}
		cursor += int64(advance)
	}
}

func (font *fontData) drawScaledPixel(context *graphicsContext, x, y int64) {
	boldWidth := font.scale
	if font.style&fontBold != 0 {
		boldWidth += font.scale
	}
	for row := 0; row < font.scale; row++ {
		for column := 0; column < boldWidth; column++ {
			context.putColorPixel(x+int64(column), y+int64(row))
		}
	}
}

func validFontAttributes(face, style, size int32) bool {
	validFace := face == fontSystem || face == fontMonospace || face == fontProportional
	validStyle := style >= 0 && style&^(fontBold|fontItalic|fontUnderlined) == 0
	validSize := size == fontMedium || size == fontSmall || size == fontLarge
	return validFace && validStyle && validSize
}

func fontStyleFlag(arguments []jvm.Value, flag int32) (jvm.Value, error) {
	font, err := fontArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if (flag == 0 && font.style == 0) || (flag != 0 && font.style&flag != 0) {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func fontArgument(arguments []jvm.Value, index int) (*fontData, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, newGuestException("java/lang/NullPointerException", "Font is null")
	}
	return fontReceiver(object)
}

func fontReceiver(object *jvm.Object) (*fontData, error) {
	font, ok := object.Native.(*fontData)
	if !isFontClass(object.ClassName) || !ok || font == nil {
		return nil, fmt.Errorf("object is not a MIDP Font")
	}
	return font, nil
}

func characterArraySlice(arguments []jvm.Value, arrayIndex, offsetIndex, lengthIndex int) ([]uint16, error) {
	_, values, err := primitiveArrayArgument(arguments, arrayIndex, jvm.TypeChar)
	if err != nil {
		return nil, err
	}
	offset, err := intArgument(arguments, offsetIndex)
	if err != nil {
		return nil, err
	}
	length, err := intArgument(arguments, lengthIndex)
	if err != nil {
		return nil, err
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(values)) {
		return nil, newGuestException("java/lang/ArrayIndexOutOfBoundsException", "invalid character array range")
	}
	units := make([]uint16, int(length))
	for index, value := range values[int(offset):int(offset+length)] {
		character, valueErr := value.Int32()
		if valueErr != nil {
			return nil, fmt.Errorf("character %d: %w", index, valueErr)
		}
		units[index] = uint16(character)
	}
	return units, nil
}

func stringUnitSlice(text string, arguments []jvm.Value, offsetIndex, lengthIndex int) ([]uint16, error) {
	offset, err := intArgument(arguments, offsetIndex)
	if err != nil {
		return nil, err
	}
	length, err := intArgument(arguments, lengthIndex)
	if err != nil {
		return nil, err
	}
	units := utf16.Encode([]rune(text))
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(units)) {
		return nil, newGuestException("java/lang/StringIndexOutOfBoundsException", "invalid String range")
	}
	return units[int(offset):int(offset+length)], nil
}

// normalizeAnchor reads the two halves of an anchor. **A half a title leaves
// out is its default**, which is the reading the zero anchor already had and
// the one the handset gave every other value: a WIPI title draws its title
// screen with `LEFT` alone and nothing vertical, and MIDP's own rule — exactly
// one bit from each group — would end that title on its first image. Naming
// two bits from one group is still refused, because that is a title asking for
// two different places at once rather than leaving one unsaid.
func normalizeAnchor(anchor int32, image bool) (int32, int32, error) {
	horizontal := anchor & (anchorLeft | anchorHCenter | anchorRight)
	verticalMask := anchorTop | anchorBottom | anchorBaseline
	if image {
		verticalMask = anchorTop | anchorVCenter | anchorBottom
	}
	vertical := anchor & verticalMask
	allowed := int32(anchorLeft | anchorHCenter | anchorRight | verticalMask)
	if anchor&^allowed != 0 || !atMostOneAnchor(horizontal) || !atMostOneAnchor(vertical) {
		return 0, 0, newGuestException("java/lang/IllegalArgumentException", "invalid Graphics anchor")
	}
	if horizontal == 0 {
		horizontal = anchorLeft
	}
	if vertical == 0 {
		vertical = anchorTop
	}
	return horizontal, vertical, nil
}

func atMostOneAnchor(value int32) bool {
	return value&(value-1) == 0
}

func singleAnchor(value int32) bool {
	return value != 0 && value&(value-1) == 0
}
