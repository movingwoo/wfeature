package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The screen, as an AOT Java title reaches it: `Card`, `Display`, `Graphics`
// and `Font`.
//
// **These are the platform's own classes and the module numbers none of them
// itself** — every one of them arrives by name, out of the class table the
// module hands over, so there is no slot to guess at here. What they draw with
// is what a Clet draws with: one framebuffer table, one clip and colour state,
// one glyph face. A Java title and a Clet see the same LCD.
//
// The state a `Graphics` carries is this platform's, keyed by the object the
// module allocated, for the same reason a String's characters are: the module
// never reads a Graphics' own words, it only calls methods on it.

const (
	javaGraphicsClass = "org/kwis/msp/lcdui/Graphics"
	javaCardClass     = "org/kwis/msp/lcdui/Card"
	javaDisplayClass  = "org/kwis/msp/lcdui/Display"
	javaFontClass     = "org/kwis/msp/lcdui/Font"
)

// javaGraphics is the drawing state one Graphics object holds. It is the same
// state `MC_GrpContext` holds for a Clet, in the fields the Java calls set.
type javaGraphics struct {
	// surface is the framebuffer handle this Graphics draws into. The screen's
	// handle is zero, which is what a Graphics handed to `paint` targets.
	surface uint32
	color   uint16
	// translate is the origin every coordinate is taken from, which
	// `Graphics.translate` moves and nothing else resets.
	translateX int
	translateY int
	// clip is in the same translated coordinates the drawing calls use. A
	// Graphics starts with the whole surface inside it.
	clipX      int
	clipY      int
	clipWidth  int
	clipHeight int
	clipSet    bool
	// alpha is the specification's own three-way transparency: zero draws
	// nothing, 255 draws normally, and **every other value counts as 255**,
	// which is what it says in as many words. So there is no blend to do here.
	alpha int
	// packed is the colour as the title set it, which `getColor` has to answer
	// with: the surface is 16-bit, so what is drawn cannot be read back.
	packed uint32
	// xor is the specification's XOR mode, which draws the difference between
	// the colour and what is already there. Setting an alpha turns it off, in
	// as many words.
	xor bool
}

// The anchor constants, which the specification fixes: a call names one
// horizontal and one vertical position for the point it was given.
const (
	javaAnchorHCenter  = 1
	javaAnchorVCenter  = 2
	javaAnchorLeft     = 4
	javaAnchorRight    = 8
	javaAnchorTop      = 16
	javaAnchorBottom   = 32
	javaAnchorBaseline = 64
)

// javaAnchorOrigin moves the point a draw was given to the top-left corner the
// drawing code takes, for a box of this width and height.
func javaAnchorOrigin(x, y, width, height, anchor int) (int, int) {
	switch {
	case anchor&javaAnchorHCenter != 0:
		x -= width / 2
	case anchor&javaAnchorRight != 0:
		x -= width
	}
	switch {
	case anchor&javaAnchorVCenter != 0:
		y -= height / 2
	case anchor&javaAnchorBottom != 0:
		y -= height
	case anchor&javaAnchorBaseline != 0:
		y -= textFace().Ascent
	}
	return x, y
}

// javaGraphicsState answers the state behind a Graphics object.
func (client *Client) javaGraphicsState(object uint32) (*javaGraphics, error) {
	runtime := client.javaRuntimeState()
	state, ok := runtime.graphics[object]
	if !ok {
		return nil, fmt.Errorf("the object at %#x is not a Graphics this platform built", object)
	}
	return state, nil
}

// newJavaGraphics builds a Graphics object over a surface, with the state a
// fresh one starts in.
func (client *Client) newJavaGraphics(surface uint32) (uint32, error) {
	class, err := client.preparePlatformJavaClass(javaGraphicsClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().graphics[object] = newJavaGraphicsState(surface)
	return object, nil
}

// newJavaGraphicsState is the state a Graphics starts in. **The transparency
// starts opaque**, which is not the zero value: a Graphics that has never been
// given an alpha draws normally, and taking the zero to mean "draw nothing"
// makes a title that never calls `setAlpha` paint a black screen.
func newJavaGraphicsState(surface uint32) *javaGraphics {
	return &javaGraphics{surface: surface, alpha: javaAlphaOpaque}
}

// javaAlphaOpaque is the value the specification calls fully drawn, and the
// one every other value but zero counts as.
const javaAlphaOpaque = 255

// context turns a Graphics' state into the context the drawing code shares with
// the Clet path. The clip is intersected with the surface rather than trusted:
// a title that sets one past the edge is not asking to draw off it.
func (client *Client) javaDrawContext(state *javaGraphics) (*graphicsContext, error) {
	target := client.framebuffer(state.surface)
	if target == nil {
		return nil, fmt.Errorf("the surface %#x is not one this platform holds", state.surface)
	}
	context := &graphicsContext{
		target:     target,
		clipX:      0,
		clipY:      0,
		clipWidth:  target.width,
		clipHeight: target.height,
		foreground: state.color,
		fontHeight: defaultFontHeight(),
		xor:        state.xor,
	}
	if state.alpha == 0 {
		// Nothing is drawn at all, which is the specification's own reading of
		// alpha zero. An empty clip is how that is said to the drawing code.
		context.clipWidth, context.clipHeight = 0, 0
		return context, nil
	}
	if state.clipSet {
		context.clipX = state.clipX + state.translateX
		context.clipY = state.clipY + state.translateY
		context.clipWidth = state.clipWidth
		context.clipHeight = state.clipHeight
	}
	return context, nil
}

// javaDraw runs one drawing call: read the receiver's state, take the surface's
// pixels as the guest last left them, draw, and write them back. The
// synchronisation is the same pair a Clet's draw makes, because a Java title's
// pictures are Clet surfaces and a title may hold a framebuffer pointer of its
// own.
func (client *Client) javaDraw(
	object uint32, draw func(*graphicsContext, *javaGraphics) error,
) error {
	state, err := client.javaGraphicsState(object)
	if err != nil {
		return err
	}
	context, err := client.javaDrawContext(state)
	if err != nil {
		return err
	}
	if err := client.syncFromGuest(context.target); err != nil {
		return err
	}
	if err := draw(context, state); err != nil {
		return err
	}
	return client.syncToGuest(context.target)
}

// javaGraphicsMethods is the drawing surface, keyed the way every platform
// method is: the class, the name and the descriptor the module hands over.
var javaGraphicsMethods = map[string]javaPlatformMethod{
	// The colour every later call draws in. The three-argument form is the
	// specification's `setColor(int r, int g, int b)`; the one-argument form
	// takes the same 24-bit value packed, which is what the platform's own
	// `MC_grpGetPixelFromRGB` packs.
	"org/kwis/msp/lcdui/Graphics.setColor(III)V": {Words: 4, Implementat: javaSetColorTriple},
	"org/kwis/msp/lcdui/Graphics.setColor(I)V":   {Words: 2, Implementat: javaSetColorPacked},

	"org/kwis/msp/lcdui/Graphics.fillRect(IIII)V": {Words: 5, Implementat: javaFillRect},
	"org/kwis/msp/lcdui/Graphics.drawRect(IIII)V": {Words: 5, Implementat: javaDrawRect},
	"org/kwis/msp/lcdui/Graphics.drawLine(IIII)V": {Words: 5, Implementat: javaDrawLine},
	// The rounded pair take the corner diameters as their last two arguments.
	// Nothing here rounds a corner, and a rectangle is what the difference
	// costs: the alternative is a title's frames and panels not being drawn at
	// all.
	"org/kwis/msp/lcdui/Graphics.fillRoundRect(IIIIII)V": {Words: 7, Implementat: javaFillRect},
	"org/kwis/msp/lcdui/Graphics.drawRoundRect(IIIIII)V": {Words: 7, Implementat: javaDrawRect},

	"org/kwis/msp/lcdui/Graphics.translate(II)V": {Words: 3, Implementat: javaTranslate},
	"org/kwis/msp/lcdui/Graphics.setClip(IIII)V": {Words: 5, Implementat: javaSetClip},
	"org/kwis/msp/lcdui/Graphics.setAlpha(I)V":   {Words: 2, Implementat: javaSetAlpha},
	"org/kwis/msp/lcdui/Graphics.setXORMode(Z)V": {Words: 2, Implementat: javaSetXORMode},
	"org/kwis/msp/lcdui/Graphics.getColor()I":    {Words: 1, Implementat: javaGetColor},
	// The clip a title reads back is the one it set, in its own coordinates.
	// A Graphics that has had none set is clipped to the whole surface, which
	// is what these answer then.
	"org/kwis/msp/lcdui/Graphics.getClipX()I":      {Words: 1, Implementat: javaClipValue(0)},
	"org/kwis/msp/lcdui/Graphics.getClipY()I":      {Words: 1, Implementat: javaClipValue(1)},
	"org/kwis/msp/lcdui/Graphics.getClipWidth()I":  {Words: 1, Implementat: javaClipValue(2)},
	"org/kwis/msp/lcdui/Graphics.getClipHeight()I": {Words: 1, Implementat: javaClipValue(3)},

	// A picture a title draws into itself, and the Graphics that draws there.
	"org/kwis/msp/lcdui/Image.createImage(II)Lorg/kwis/msp/lcdui/Image;": {
		Words: 2, Implementat: javaCreateBlankImage},
	"org/kwis/msp/lcdui/Image.getGraphics()Lorg/kwis/msp/lcdui/Graphics;": {
		Words: 1, Implementat: javaImageGraphics},

	// Taking the card off the display, and painting one that has asked to be
	// painted without waiting for the next frame; see java_frame.go.
	"org/kwis/msp/lcdui/Display.removeAllCards()V": {Words: 1, Implementat: javaRemoveAllCards},
	"org/kwis/msp/lcdui/Card.serviceRepaints()V":   {Words: 1, Implementat: javaServiceRepaints},

	"org/kwis/msp/lcdui/Graphics.drawString(Ljava/lang/String;III)V": {
		Words: 5, Implementat: javaDrawString},
	"org/kwis/msp/lcdui/Graphics.drawChar(CIII)V":     {Words: 5, Implementat: javaDrawChar},
	"org/kwis/msp/lcdui/Graphics.drawChars([CIIIII)V": {Words: 7, Implementat: javaDrawChars},

	"org/kwis/msp/lcdui/Graphics.drawImage(Lorg/kwis/msp/lcdui/Image;III)V": {
		Words: 5, Implementat: javaDrawImage},

	// The font a title measures its text with. One face, one height: the same
	// glyphs the C drawing calls render.
	"org/kwis/msp/lcdui/Font.getDefaultFont()Lorg/kwis/msp/lcdui/Font;": {
		Implementat: javaPlatformSingleton(javaFontClass)},
	"org/kwis/msp/lcdui/Font.getHeight()I": {Words: 1, Implementat: javaFontHeight},
	"org/kwis/msp/lcdui/Font.stringWidth(Ljava/lang/String;)I": {
		Words: 2, Implementat: javaStringWidth},
	"org/kwis/msp/lcdui/Font.substringWidth(Ljava/lang/String;II)I": {
		Words: 4, Implementat: javaSubstringWidth},
	"org/kwis/msp/lcdui/Font.charsWidth([CII)I": {Words: 4, Implementat: javaCharsWidth},

	// The card the application shows, and the display it shows it on. A Card
	// is the whole screen here, and pushing one is what makes its `paint` the
	// frame; see java_frame.go.
	"org/kwis/msp/lcdui/Display.pushCard(Lorg/kwis/msp/lcdui/Card;)V": {
		Words: 2, Implementat: javaPushCard},
	"org/kwis/msp/lcdui/Card.repaint(IIII)V": {Words: 5, Implementat: javaCardRepaint},
	"org/kwis/msp/lcdui/Card.repaint()V":     {Words: 1, Implementat: javaCardRepaint},
}

// javaSetColorTriple is `setColor(r, g, b)`.
func javaSetColorTriple(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	state.color = rgb565(arguments[1], arguments[2], arguments[3])
	state.packed = arguments[1]&0xff<<16 | arguments[2]&0xff<<8 | arguments[3]&0xff
	return 0, nil
}

// javaSetColorPacked is `setColor(0xRRGGBB)`.
func javaSetColorPacked(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	packed := arguments[1]
	state.color = rgb565(packed>>16&0xff, packed>>8&0xff, packed&0xff)
	state.packed = packed & 0xffffff
	return 0, nil
}

// javaSetAlpha is the specification's three-way transparency: zero draws
// nothing and anything else draws normally.
func javaSetAlpha(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	state.alpha = int(int32(arguments[1]))
	return 0, nil
}

// javaSetXORMode turns the difference-drawing mode on and off.
func javaSetXORMode(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	state.xor = arguments[1] != 0
	return 0, nil
}

func javaGetColor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	return state.packed, nil
}

// javaClipValue answers one of the four numbers the clip is read back as.
func javaClipValue(which int) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(
		client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
	) (uint32, error) {
		state, err := client.javaGraphicsState(arguments[0])
		if err != nil {
			return 0, err
		}
		target := client.framebuffer(state.surface)
		values := []int{0, 0, 0, 0}
		if target != nil {
			values = []int{-state.translateX, -state.translateY, target.width, target.height}
		}
		if state.clipSet {
			values = []int{state.clipX, state.clipY, state.clipWidth, state.clipHeight}
		}
		return uint32(int32(values[which])), nil
	}
}

// javaCreateBlankImage is `Image.createImage(w, h)`: a surface of the title's
// own to draw into, which it then draws to the screen.
func javaCreateBlankImage(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	width, height := int(int32(arguments[0])), int(int32(arguments[1]))
	surface, err := client.newFramebuffer(width, height, false)
	if err != nil {
		return 0, err
	}
	class, err := client.preparePlatformJavaClass(javaImageClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().images[object] = surface.handle
	return object, nil
}

// javaImageGraphics answers a Graphics over a picture's own surface. It is one
// object per picture: a title that asks twice draws through the same state,
// which is what the specification's own wording — the image's graphics —
// means.
func javaImageGraphics(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	surface, err := client.javaImageSurface(arguments[0])
	if err != nil {
		return 0, err
	}
	runtime := client.javaRuntimeState()
	for object, state := range runtime.graphics {
		if state.surface == surface.handle {
			return object, nil
		}
	}
	return client.newJavaGraphics(surface.handle)
}

func javaFillRect(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaDraw(arguments[0], func(context *graphicsContext, state *javaGraphics) error {
		context.fill(
			int(int32(arguments[1]))+state.translateX, int(int32(arguments[2]))+state.translateY,
			int(int32(arguments[3])), int(int32(arguments[4])), state.color)
		return nil
	})
}

func javaDrawRect(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaDraw(arguments[0], func(context *graphicsContext, state *javaGraphics) error {
		context.rect(
			int(int32(arguments[1]))+state.translateX, int(int32(arguments[2]))+state.translateY,
			int(int32(arguments[3])), int(int32(arguments[4])), state.color)
		return nil
	})
}

func javaDrawLine(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaDraw(arguments[0], func(context *graphicsContext, state *javaGraphics) error {
		context.line(
			int(int32(arguments[1]))+state.translateX, int(int32(arguments[2]))+state.translateY,
			int(int32(arguments[3]))+state.translateX, int(int32(arguments[4]))+state.translateY,
			state.color)
		return nil
	})
}

// javaTranslate moves the origin, and the specification's own definition is
// that it moves it by the amount given rather than to it.
func javaTranslate(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	state.translateX += int(int32(arguments[1]))
	state.translateY += int(int32(arguments[2]))
	return 0, nil
}

func javaSetClip(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	state.clipX, state.clipY = int(int32(arguments[1])), int(int32(arguments[2]))
	state.clipWidth, state.clipHeight = int(int32(arguments[3])), int(int32(arguments[4]))
	state.clipSet = true
	return 0, nil
}

// javaDrawString is `drawString(text, x, y, anchor)`. The anchor is the
// specification's own placement flag; what a title uses it for is where the
// coordinates are measured from, and the two the local titles pass are the
// top-left pair — so the text is drawn from the point given, which is also what
// the C `MC_grpDrawString` does.
func javaDrawString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, ok := client.javaText(arguments[1])
	if !ok && arguments[1] != 0 {
		return 0, fmt.Errorf("the object at %#x is not a string this platform built", arguments[1])
	}
	return 0, client.javaDrawText(arguments[0], text,
		int(int32(arguments[2])), int(int32(arguments[3])), int(int32(arguments[4])))
}

// javaDrawText places a run of text by its anchor and draws it.
func (client *Client) javaDrawText(object uint32, text string, x, y, anchor int) error {
	return client.javaDraw(object, func(context *graphicsContext, state *javaGraphics) error {
		left, top := javaAnchorOrigin(x, y, textWidth(text), textFace().Height(), anchor)
		context.text(left+state.translateX, top+state.translateY, text, state.color)
		return nil
	})
}

// javaDrawChar is `drawChar(c, x, y, anchor)`.
func javaDrawChar(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaDrawText(arguments[0], string(rune(uint16(arguments[1]))),
		int(int32(arguments[2])), int(int32(arguments[3])), int(int32(arguments[4])))
}

// javaDrawChars is `drawChars(chars, offset, length, x, y, anchor)`.
func javaDrawChars(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, err := client.javaCharsText(arguments[1], arguments[2], arguments[3])
	if err != nil {
		return 0, err
	}
	return 0, client.javaDrawText(arguments[0], text,
		int(int32(arguments[4])), int(int32(arguments[5])), int(int32(arguments[6])))
}

// javaCharsText reads a run of a `char[]` as text.
func (client *Client) javaCharsText(array, offset, length uint32) (string, error) {
	units, err := client.readJavaArrayChars(array)
	if err != nil {
		return "", err
	}
	if uint64(offset)+uint64(length) > uint64(len(units)) {
		return "", fmt.Errorf("%d characters from %d is past the end of %d", length, offset, len(units))
	}
	return javaTextOfUnits(units[offset : offset+length]), nil
}

// javaDrawImage is `drawImage(image, x, y, anchor)`. It goes through the same
// blit a Clet's `MC_grpDrawImage` does, so a picture's declared transparency is
// honoured on both paths.
func javaDrawImage(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	source, err := client.javaImageSurface(arguments[1])
	if err != nil {
		return 0, err
	}
	return 0, client.javaDraw(arguments[0], func(context *graphicsContext, state *javaGraphics) error {
		// The C blit takes the whole picture from its own origin, which is what
		// `drawImage(image, x, y, anchor)` draws.
		left, top := javaAnchorOrigin(
			int(int32(arguments[2])), int(int32(arguments[3])),
			source.width, source.height, int(int32(arguments[4])))
		return client.drawImage(context, []int32{
			int32(left + state.translateX), int32(top + state.translateY),
			int32(source.width), int32(source.height), int32(source.handle), 0, 0})
	})
}

func javaFontHeight(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return uint32(textFace().Height()), nil
}

func javaStringWidth(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, ok := client.javaText(arguments[1])
	if !ok && arguments[1] != 0 {
		return 0, fmt.Errorf("the object at %#x is not a string this platform built", arguments[1])
	}
	return uint32(textWidth(text)), nil
}

// javaSubstringWidth is `substringWidth(text, offset, length)`, measured in
// characters the way the string itself is indexed.
func javaSubstringWidth(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, ok := client.javaText(arguments[1])
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a string this platform built", arguments[1])
	}
	symbols := []rune(text)
	offset, length := arguments[2], arguments[3]
	if uint64(offset)+uint64(length) > uint64(len(symbols)) {
		return 0, fmt.Errorf("%d characters from %d is past the end of %d", length, offset, len(symbols))
	}
	return uint32(textWidth(string(symbols[offset : offset+length]))), nil
}

func javaCharsWidth(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, err := client.javaCharsText(arguments[1], arguments[2], arguments[3])
	if err != nil {
		return 0, err
	}
	return uint32(textWidth(text)), nil
}
