package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/curve"
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

// javaDraw runs one drawing call: read the receiver's state and draw into the
// surface the runtime holds.
//
// **It does not synchronise the surface with guest memory, and that is the
// whole difference between a Java title's drawing and a Clet's.** A Clet is
// handed the framebuffer's address and writes pixels into it behind the
// platform's back, so a draw there has to re-read before it blends and write
// back after — the pair `syncFromGuest`/`syncToGuest` exists for exactly that.
// A Java title has no such address: it draws only through `Graphics`, and the
// runtime's copy is the only one anything writes.
//
// Doing it here anyway cost more than everything else the platform did. Each
// call read the whole surface out of guest memory, converted it a byte at a
// time, drew, converted it back and wrote it — 150 KiB each way for a call
// that might set one pixel. Two local Java titles were measured over a real
// session: **1,711,725 and 82,017 of those round trips, and not one of them
// found a single pixel the guest had changed.** The same instrumentation on
// two Clets found 600 changed frames out of 1,201 calls, which is the case
// worth paying for and the only one.
//
// The invariant is restored once per frame instead: `paintJavaCard` publishes
// the finished frame to guest memory, so guest and runtime agree at every
// frame boundary. What it assumes is that nothing on the Clet side of the
// platform touches a Java title's surfaces between frames, which is true
// because a Java title registers no Clet and calls no `MC_grp` slot — and if
// it ever stops being true, the acceptance probe and a frame diff say so.
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
	// From here the runtime's copy of this surface is the only correct one;
	// syncFromGuest has why that has to be recorded.
	context.target.drawnHere = true
	return draw(context, state)
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
	// One pixel in the current colour, which is the drawing call with nothing
	// between it and the surface. It is the write side of the `getPixel` the
	// specification lists, and it goes through the same clip, translation and
	// alpha every other call here does.
	"org/kwis/msp/lcdui/Graphics.setPixel(II)V": {Words: 3, Implementat: javaSetPixel},
	// The rounded pair take the corner **diameters** as their last two
	// arguments, and the arc pair take a start angle and an extent with zero
	// degrees at three o'clock. Both used to be answered with the plain
	// rectangle, which drew a title's panels with square corners and had no
	// answer at all for a curve.
	"org/kwis/msp/lcdui/Graphics.fillRoundRect(IIIIII)V": {Words: 7, Implementat: javaCurve(curve.FillRoundRect)},
	"org/kwis/msp/lcdui/Graphics.drawRoundRect(IIIIII)V": {Words: 7, Implementat: javaCurve(curve.DrawRoundRect)},
	"org/kwis/msp/lcdui/Graphics.fillArc(IIIIII)V":       {Words: 7, Implementat: javaCurve(curve.FillArc)},
	"org/kwis/msp/lcdui/Graphics.drawArc(IIIIII)V":       {Words: 7, Implementat: javaCurve(curve.DrawArc)},

	"org/kwis/msp/lcdui/Graphics.translate(II)V": {Words: 3, Implementat: javaTranslate},
	// The origin a title reads back is the one its own `translate` calls moved
	// it to. A title that draws a panel in its own coordinates and then has to
	// hand a screen coordinate to something else asks for it rather than
	// keeping a second copy — and one that never asks is not wrong either,
	// which is why these went unnoticed until a title's thread stopped on
	// them.
	"org/kwis/msp/lcdui/Graphics.getTranslateX()I": {Words: 1, Implementat: javaGetTranslateX},
	"org/kwis/msp/lcdui/Graphics.getTranslateY()I": {Words: 1, Implementat: javaGetTranslateY},
	"org/kwis/msp/lcdui/Graphics.setClip(IIII)V":   {Words: 5, Implementat: javaSetClip},
	// The specification's own wording for `clipRect` is that the clip becomes
	// what it and the rectangle have in common, so it can only ever narrow.
	"org/kwis/msp/lcdui/Graphics.clipRect(IIII)V": {Words: 5, Implementat: javaClipRect},
	"org/kwis/msp/lcdui/Graphics.setAlpha(I)V":    {Words: 2, Implementat: javaSetAlpha},
	"org/kwis/msp/lcdui/Graphics.setXORMode(Z)V":  {Words: 2, Implementat: javaSetXORMode},
	"org/kwis/msp/lcdui/Graphics.getColor()I":     {Words: 1, Implementat: javaGetColor},
	// The clip a title reads back is the one it set, in its own coordinates.
	// A Graphics that has had none set is clipped to the whole surface, which
	// is what these answer then.
	"org/kwis/msp/lcdui/Graphics.getClipX()I":      {Words: 1, Implementat: javaClipValue(0)},
	"org/kwis/msp/lcdui/Graphics.getClipY()I":      {Words: 1, Implementat: javaClipValue(1)},
	"org/kwis/msp/lcdui/Graphics.getClipWidth()I":  {Words: 1, Implementat: javaClipValue(2)},
	"org/kwis/msp/lcdui/Graphics.getClipHeight()I": {Words: 1, Implementat: javaClipValue(3)},

	// The font this Graphics draws text with. There is one face here — the
	// same one `getDefaultFont` answers and the same one the C drawing calls
	// render — so a title that reads the font off a Graphics to measure a
	// string measures the glyphs it is about to draw.
	"org/kwis/msp/lcdui/Graphics.getFont()Lorg/kwis/msp/lcdui/Font;": {
		Words: 1, Implementat: javaPlatformSingleton(javaFontClass)},
	// Choosing the face is accepted and changes nothing, for the same reason
	// `Font.getFont` answers the default one: there is a single face here, so
	// the font a title sets is the font it would have drawn with anyway. What
	// it must not be is a refusal — a title sets the font before it draws its
	// own text, and stopping there costs the screen the text was on.
	"org/kwis/msp/lcdui/Graphics.setFont(Lorg/kwis/msp/lcdui/Font;)V": {
		Words: 2, Implementat: javaNoResult},
	// Putting a Graphics back to the state a fresh one has. It is not in the
	// specification's own listing, and it does not have to be: the same call
	// is what titles on this project's two other platforms make of the one
	// Graphics they keep for the life of the application, and it means the
	// same thing here — no translation, the whole surface visible, opaque
	// black, plain drawing mode.
	"org/kwis/msp/lcdui/Graphics.reset()V": {Words: 1, Implementat: javaGraphicsReset},

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
	"org/kwis/msp/lcdui/Graphics.drawSubstring(Ljava/lang/String;IIIII)V": {
		Words: 7, Implementat: javaDrawSubstring},

	"org/kwis/msp/lcdui/Graphics.drawImage(Lorg/kwis/msp/lcdui/Image;III)V": {
		Words: 5, Implementat: javaDrawImage},

	// A surface copying part of itself, which is how a title scrolls without
	// keeping a second copy of the screen.
	"org/kwis/msp/lcdui/Graphics.copyArea(IIIIII)V": {Words: 7, Implementat: javaCopyArea},

	// The font a title measures its text with. One face, one height: the same
	// glyphs the C drawing calls render.
	"org/kwis/msp/lcdui/Font.getDefaultFont()Lorg/kwis/msp/lcdui/Font;": {
		Implementat: javaPlatformSingleton(javaFontClass)},
	// A named face, style and size answer the same font, because there is one
	// face here. It is the default font rather than a refusal: a title that
	// asks for a bold face and is refused stops, and a title that asks for one
	// and gets the plain face draws the text it meant to.
	"org/kwis/msp/lcdui/Font.getFont(III)Lorg/kwis/msp/lcdui/Font;": {
		Words: 3, Implementat: javaPlatformSingleton(javaFontClass)},
	"org/kwis/msp/lcdui/Font.getHeight()I":  {Words: 1, Implementat: javaFontHeight},
	"org/kwis/msp/lcdui/Font.charWidth(C)I": {Words: 2, Implementat: javaCharWidth},
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

	// Work a title asks the platform to do for it once the events in hand
	// have been dealt with; see java_frame.go.
	"org/kwis/msp/lcdui/Display.callSerially(Ljava/lang/Runnable;)V": {
		Words: 2, Implementat: javaCallSerially},

	// What a key means rather than which key it was. A title that handles the
	// pad through actions asks for this in its own `keyNotify` and branches on
	// the answer, so an unimplemented one stops it on its first key press.
	"org/kwis/msp/lcdui/Display.getGameAction(I)I": {Words: 1, Implementat: javaGameAction},
	"org/kwis/msp/lcdui/Display.getKeyCode(I)I":    {Words: 1, Implementat: javaKeyCode},
}

// The game actions the platform's own `Display` answers with. They are the
// table the other two platforms here already carry, and the numbers are the
// ones a title compares against rather than anything this platform chose.
const (
	javaGameActionUp    = 1
	javaGameActionLeft  = 2
	javaGameActionRight = 5
	javaGameActionDown  = 6
	javaGameActionFire  = 8
	javaGameActionClear = 99
)

// The key codes those actions stand for. A handset reports these to
// `keyNotify`, and `getKeyCode` turns an action back into one.
const (
	javaKeyUp    int32 = -1
	javaKeyDown  int32 = -2
	javaKeyLeft  int32 = -3
	javaKeyRight int32 = -4
	javaKeyFire  int32 = -5
	javaKeyClear int32 = -16
)

// javaGameAction is `Display.getGameAction(keyCode)`. A key with no action
// reports itself, which is what leaves a title's digit and soft keys reaching
// its own branches.
func javaGameAction(_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
	switch int32(arguments[0]) {
	case javaKeyUp:
		return javaGameActionUp, nil
	case javaKeyDown:
		return javaGameActionDown, nil
	case javaKeyLeft:
		return javaGameActionLeft, nil
	case javaKeyRight:
		return javaGameActionRight, nil
	case javaKeyFire:
		return javaGameActionFire, nil
	case javaKeyClear:
		return javaGameActionClear, nil
	}
	return arguments[0], nil
}

// javaKeyCode is `Display.getKeyCode(action)`, the reverse. An action with no
// key reports zero.
func javaKeyCode(_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
	var key int32
	switch int32(arguments[0]) {
	case javaGameActionUp:
		key = javaKeyUp
	case javaGameActionDown:
		key = javaKeyDown
	case javaGameActionLeft:
		key = javaKeyLeft
	case javaGameActionRight:
		key = javaKeyRight
	case javaGameActionFire:
		key = javaKeyFire
	case javaGameActionClear:
		key = javaKeyClear
	}
	return uint32(key), nil
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
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	surface, err := client.javaImageArgument(thread, arguments[0], "getGraphics")
	if err != nil {
		return 0, err
	}
	// **Asking to draw into a picture ends its sharing.** A picture loaded from
	// a resource is immutable and two Images of one name share its pixels, but
	// the specification's own rule is a rule about *copies* — and nothing here
	// checks that the copy is what a title is drawing into. A title that draws
	// into a named picture gets its own surface from here on, so a second
	// holder of that name keeps the picture it loaded. See javaCreateImageNamed.
	if surface, err = client.unshareDecodedSurface(arguments[0], surface); err != nil {
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

// javaCurve is the shape of the four curve calls: they differ only in which
// geometry they walk and in the two trailing arguments it takes. Every span
// goes through the same clipped fill the rectangle calls use, so the clip, the
// translation, the alpha and the XOR mode apply to a curve as they do to a
// rectangle.
func javaCurve(walk func(x, y, width, height, a, b int32, emit curve.Emit) error) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		return 0, client.javaDraw(arguments[0], func(drawContext *graphicsContext, state *javaGraphics) error {
			emit := func(span curve.Span) error {
				drawContext.fill(int(span.X), int(span.Y), int(span.Width), 1, state.color)
				return nil
			}
			return walk(
				int32(arguments[1])+int32(state.translateX), int32(arguments[2])+int32(state.translateY),
				int32(arguments[3]), int32(arguments[4]), int32(arguments[5]), int32(arguments[6]), emit)
		})
	}
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

// javaGetTranslateX and javaGetTranslateY answer where the origin now is.
func javaGetTranslateX(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(int32(state.translateX)), nil
}

func javaGetTranslateY(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(int32(state.translateY)), nil
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

// javaDrawSubstring is `drawSubstring(text, offset, length, x, y, anchor)`. It
// is the run-of-a-String companion to drawChars, and a title that lays out one
// buffer of text a line at a time draws through it rather than through
// drawString.
func javaDrawSubstring(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, ok := client.javaText(arguments[1])
	if !ok && arguments[1] != 0 {
		return 0, fmt.Errorf("the object at %#x is not a string this platform built", arguments[1])
	}
	units := utf16Units(text)
	offset, length := arguments[2], arguments[3]
	if uint64(offset)+uint64(length) > uint64(len(units)) {
		return 0, fmt.Errorf("%d characters from %d is past the end of a %d-character string",
			length, offset, len(units))
	}
	return 0, client.javaDrawText(arguments[0], javaTextOfUnits(units[offset:offset+length]),
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
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	source, err := client.javaImageArgument(thread, arguments[1], "drawImage")
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

// javaCopyArea is `copyArea(dx, dy, sx, sy, w, h)`, a rectangle copied inside
// the surface the Graphics draws on.
//
// **This is not the MIDP call of the same name.** The WIPI specification gives
// the destination first, then the source, then the size; MIDP names the source
// first and ends with an anchor. The C call beside it in this platform takes a
// third order again — destination, size, source — so the three cannot share an
// argument list and the order is written out here. Reading it in either of the
// other two makes a scrolling title copy nothing, because what it passed as the
// size lands where the size is not.
func javaCopyArea(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaDraw(arguments[0], func(context *graphicsContext, state *javaGraphics) error {
		// The C copy takes its arguments in its own order, and the translation
		// applies to both corners: a title that shifted its origin means both
		// the rectangle it reads and the one it writes.
		client.copyArea(context, []int32{
			int32(arguments[1]) + int32(state.translateX), int32(arguments[2]) + int32(state.translateY),
			int32(arguments[5]), int32(arguments[6]),
			int32(arguments[3]) + int32(state.translateX), int32(arguments[4]) + int32(state.translateY),
		})
		return nil
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

// javaCharWidth is `charWidth(char)`: one character, measured with the same
// glyphs the drawing calls render.
func javaCharWidth(
	_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return uint32(textWidth(string(rune(uint16(arguments[1]))))), nil
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

// javaGraphicsReset puts a Graphics back to the state a freshly built one has.
// The surface it draws on is not part of that: a title resets the Graphics it
// was handed, and one that came back pointing at nothing would stop the next
// call rather than draw.
func javaGraphicsReset(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	*state = *newJavaGraphicsState(state.surface)
	return 0, nil
}

// javaSetPixel is `setPixel(x, y)`: the current colour at one point.
func javaSetPixel(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaDraw(arguments[0], func(context *graphicsContext, state *javaGraphics) error {
		context.put(
			int(int32(arguments[1]))+state.translateX,
			int(int32(arguments[2]))+state.translateY, state.color)
		return nil
	})
}

// javaClipRect is `clipRect(x, y, width, height)`: the clip becomes the
// intersection of what it was and the rectangle given, in the Graphics' own
// coordinates. A Graphics with no clip of its own is clipped to its surface, so
// the intersection starts from the surface's own bounds — and an intersection
// that comes out empty is kept as empty rather than as a negative size, which
// the drawing code would read as no clip at all.
func javaClipRect(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, err := client.javaGraphicsState(arguments[0])
	if err != nil {
		return 0, err
	}
	left, top := 0, 0
	right, bottom := 0, 0
	if target := client.framebuffer(state.surface); target != nil {
		// The surface's own bounds are in surface coordinates and the clip is
		// in the title's, so the origin moves with the translation.
		left, top = -state.translateX, -state.translateY
		right, bottom = left+target.width, top+target.height
	}
	if state.clipSet {
		left, top = state.clipX, state.clipY
		right, bottom = left+state.clipWidth, top+state.clipHeight
	}
	x, y := int(int32(arguments[1])), int(int32(arguments[2]))
	width, height := int(int32(arguments[3])), int(int32(arguments[4]))
	if x > left {
		left = x
	}
	if y > top {
		top = y
	}
	if x+width < right {
		right = x + width
	}
	if y+height < bottom {
		bottom = y + height
	}
	if right < left {
		right = left
	}
	if bottom < top {
		bottom = top
	}
	state.clipX, state.clipY = left, top
	state.clipWidth, state.clipHeight = right-left, bottom-top
	state.clipSet = true
	return 0, nil
}
