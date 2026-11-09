package lgt

import (
	"context"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// What a `Font` reports about itself: its face, style and size, the style bits
// one at a time, and where its baseline is.
//
// **There is one face here and one Font object** (javaPlatformSingleton), the
// same object getDefaultFont, getFont and Graphics.getFont answer. KTF has the
// same single object and keeps the face, style and size the last
// `getFont(face, style, size)` asked for on it, which is what every Font method
// that reports one then answers; this platform keeps the same three numbers.
// So a title that asks for a bold face and then asks its font whether it is
// bold is told what it asked for, though the glyphs are the one face either
// way. Before any such request the three are zero, the specification's
// FACE_SYSTEM, STYLE_PLAIN and SIZE_MEDIUM. A checkpoint carries them in a
// record of its own (checkpoint_java_screen.go).

// javaFontRequest is the face, style and size the last Font.getFont asked for.
type javaFontRequest struct {
	face, style, size int32
}

// The style bits the specification fixes; STYLE_PLAIN is none of them.
const (
	javaFontStyleBold       = 1
	javaFontStyleItalic     = 2
	javaFontStyleUnderlined = 4
)

// javaFontGetFont is `Font.getFont(face, style, size)`: the one font, with
// the three numbers recorded as asked. KTF takes any three numbers without the
// IllegalArgumentException the specification describes for one that is not a
// face, style or size it names, and so does this.
func javaFontGetFont(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	font, err := client.javaPlatformObject(javaFontClass)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().fontRequest = javaFontRequest{
		face: int32(arguments[0]), style: int32(arguments[1]), size: int32(arguments[2]),
	}
	return font, nil
}

func javaFontFace(client *Client, _ context.Context, _ *armcore.Thread, _ []uint32) (uint32, error) {
	return uint32(client.javaRuntimeState().fontRequest.face), nil
}

func javaFontStyle(client *Client, _ context.Context, _ *armcore.Thread, _ []uint32) (uint32, error) {
	return uint32(client.javaRuntimeState().fontRequest.style), nil
}

func javaFontSize(client *Client, _ context.Context, _ *armcore.Thread, _ []uint32) (uint32, error) {
	return uint32(client.javaRuntimeState().fontRequest.size), nil
}

// javaFontStyleIs answers one of the style questions: whether a bit is set, or
// for a zero mask whether the style is plain, which is no bit set at all.
func javaFontStyleIs(mask int32) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, _ []uint32) (uint32, error) {
		style := client.javaRuntimeState().fontRequest.style
		if mask == 0 {
			return javaBoolean(style == 0), nil
		}
		return javaBoolean(style&mask != 0), nil
	}
}

// javaFontBaseline is `getBaselinePosition()`: how far below the top of a line
// its baseline lies, which is the face's ascent — where drawString puts the
// baseline, and what a BASELINE anchor measures (javaAnchorOrigin). KTF
// answers its face's ascent the same way.
func javaFontBaseline(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return uint32(textFace().Ascent), nil
}
