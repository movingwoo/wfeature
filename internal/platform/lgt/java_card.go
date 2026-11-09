package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A Card with a position: the box on the screen the specification lets a card
// stand in instead of the whole of it.
//
// **The display still holds one card.** Pushing a card replaces the one shown,
// as it always has (java_frame.go); a box changes only where on the screen the
// card paints. Its paint is handed a Graphics whose origin is the card's
// top-left corner and which cannot draw outside the card, and the rest of the
// screen keeps what it held — under a card that covers part of the screen,
// that is the last frame of whatever was shown before it.
//
// **A card with a box paints into a surface of its own size, which is then put
// on the screen at the box.** A Graphics here has no origin it cannot move and
// no bound it cannot widen: a title's own translate, setClip and reset, and the
// pixel calls, all reach the whole of the surface the Graphics draws on. A
// surface the size of the card gives every one of them the card's answer — a
// translation of zero when a paint starts, a clip that is the card, a reset
// that stays inside it — without the drawing code knowing what a card is. The
// surface is made from what the screen holds under the box when the card first
// paints, or first paints at a new size, so whatever the card does not paint
// over shows what was there, as it would had the card drawn on the screen.
// After that it keeps what the card drew, so drawing between paints through the
// Graphics a title kept is shown at the next paint, as it is on the screen.
//
// A card built without a position is the whole screen and paints straight onto
// it, exactly as every card did before boxes existed; so does a card whose box
// is exactly the screen, which is a box only in name.
//
// **The answers are KTF's where KTF and SKT differ**, and the specification's
// where KTF has none:
//
//   - The positioned constructors record the four numbers, and the others make
//     a card the size of the screen, on both. Neither checks the size, where
//     the specification throws IllegalArgumentException for a width or height
//     of zero or less in those constructors and in resize; KTF keeps the
//     number, and so does this — nothing of such a card reaches the screen.
//     (SKT replaces a negative size given to a constructor with the screen's,
//     which is the marker its own constructors without a position pass.)
//   - getX and getY answer the box on both. getWidth and getHeight answer the
//     card's size on KTF, which is the specification's "the width of the
//     card"; SKT's answer the display whatever the card's size. This takes
//     KTF's.
//   - move and resize set two of the numbers and nothing else on both: no
//     repaint is asked for, so the change shows at the next paint.
//   - isShown answers whether the card is the one the display shows: KTF's top
//     of its stack, SKT's current displayable, this platform's one card.
//   - The display a constructor names is ignored on both, as it is here: there
//     is one display.
//   - The transparency flag is kept by KTF and dropped by SKT. It changes
//     nothing drawn here — with one card there is nothing beneath it to show
//     through but what the screen already holds — so it is only published, as
//     KTF publishes it, into the field the specification declares.
//
// **The specification declares x, y, w, h and bTrans protected**, so a title's
// card can read them off itself rather than calling the getters, and one local
// title asks for x and y that way. A module that asks for a field's number is
// answered with a word of the card's block (layoutPlatformFields); a box is
// published into those words whenever it changes, as KTF publishes its own,
// and the flag when a constructor is given one. A card built without a
// position publishes no box, which leaves those words at zero as they have
// always been here, where KTF's and SKT's hold the screen's size. A title that
// writes one of the words is not followed: the box is what move and resize
// set, as on KTF.

const (
	javaCardFieldX           = "x"
	javaCardFieldY           = "y"
	javaCardFieldWidth       = "w"
	javaCardFieldHeight      = "h"
	javaCardFieldTransparent = "bTrans"
)

// javaCardBox is a card's box on the screen: its top-left corner and its size,
// as the title gave them. A size of zero or less is kept as given.
type javaCardBox struct {
	x, y, width, height int32
}

// javaScreenBox is the box a card without one stands in: the whole screen.
func (client *Client) javaScreenBox() javaCardBox {
	return javaCardBox{width: int32(client.screen.width), height: int32(client.screen.height)}
}

// javaCardBoxOf answers the box a card stands in: its own, or the whole screen.
func (client *Client) javaCardBoxOf(card uint32) javaCardBox {
	if box, positioned := client.javaRuntimeState().cardBounds[card]; positioned && card != 0 {
		return box
	}
	return client.javaScreenBox()
}

// setJavaCardBox gives a card a box and publishes it into the card's protected
// fields. A box that is exactly the screen is kept as none, so such a card
// paints the way a card built without a position does.
func (client *Client) setJavaCardBox(card uint32, box javaCardBox) error {
	runtime := client.javaRuntimeState()
	runtime.forgetCollectedCards()
	if box == client.javaScreenBox() {
		delete(runtime.cardBounds, card)
	} else {
		if runtime.cardBounds == nil {
			runtime.cardBounds = map[uint32]javaCardBox{}
		}
		runtime.cardBounds[card] = box
	}
	for _, field := range []struct {
		name  string
		value int32
	}{
		{javaCardFieldX, box.x}, {javaCardFieldY, box.y},
		{javaCardFieldWidth, box.width}, {javaCardFieldHeight, box.height},
	} {
		if err := client.publishJavaCardField(card, field.name, "I", uint32(field.value)); err != nil {
			return err
		}
	}
	return nil
}

// forgetCollectedCards drops the box of every card the collector has
// reclaimed. The collector gives an object's memory back without knowing what
// this file kept for it, and an address handed out again is a new object; a
// box left under it would be taken for the new object's.
func (runtime *javaRuntime) forgetCollectedCards() {
	for card := range runtime.cardBounds {
		if _, live := runtime.objects[card]; !live {
			delete(runtime.cardBounds, card)
		}
	}
}

// forgetJavaCard is what every constructor does first. The card being built is
// a new object, and a box or a surface this file kept for an earlier object at
// the same address is not its: the box goes, and the surface is made again
// from the screen the next time a card with a box paints.
func (client *Client) forgetJavaCard(card uint32) {
	runtime := client.javaRuntimeState()
	delete(runtime.cardBounds, card)
	if runtime.cardSurfaceOwner == card {
		runtime.cardSurfaceOwner = 0
	}
}

// publishJavaCardField writes one of a card's protected fields into the word
// the module was told it sits in. A module that never asked for the field
// never reads it, and nothing is written (see layoutPlatformFields).
func (client *Client) publishJavaCardField(card uint32, name, descriptor string, value uint32) error {
	if client.javaLink == nil || client.javaLink.layout == nil {
		return nil
	}
	word, ok := client.javaLink.layout.platformFieldWord(javaCardClass, name, descriptor)
	if !ok {
		return nil
	}
	record, tracked := client.javaRuntimeState().objects[card]
	if !tracked || uint64(word)*4+4 > uint64(record.blockSize) {
		return fmt.Errorf("%s.%s is word %d, past the card at %#x", javaCardClass, name, word, card)
	}
	return client.writeWord(record.block+word*4, value)
}

// javaCardConstructor is `Card()` and `Card(Display)`: a card that is the whole
// screen, which is what every card was before a box could be given.
func javaCardConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.forgetJavaCard(arguments[0])
	return 0, nil
}

// javaCardConstructorTransparent is `Card(boolean)`: a card that is the whole
// screen, with the transparency flag published where the module reads it.
func javaCardConstructorTransparent(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.forgetJavaCard(arguments[0])
	return 0, client.publishJavaCardField(arguments[0], javaCardFieldTransparent, "Z", javaBoolean(arguments[1] != 0))
}

// javaCardConstructorAt is the three positioned constructors: `Card(x, y, w,
// h)`, and the two that name a display first, the second of which ends with
// the transparency flag. first is where x is among the argument words.
func javaCardConstructorAt(first int, transparency bool) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		card := arguments[0]
		client.forgetJavaCard(card)
		box := javaCardBox{
			x: int32(arguments[first]), y: int32(arguments[first+1]),
			width: int32(arguments[first+2]), height: int32(arguments[first+3]),
		}
		if err := client.setJavaCardBox(card, box); err != nil {
			return 0, err
		}
		if !transparency {
			return 0, nil
		}
		return 0, client.publishJavaCardField(card, javaCardFieldTransparent, "Z", javaBoolean(arguments[first+4] != 0))
	}
}

// javaCardX, javaCardY, javaCardWidth and javaCardHeight are getX, getY,
// getWidth and getHeight: the four numbers of the card's box. A card without a
// box answers the screen's, so getWidth and getHeight answer what they always
// have for one.
func javaCardX(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
	return uint32(client.javaCardBoxOf(arguments[0]).x), nil
}

func javaCardY(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
	return uint32(client.javaCardBoxOf(arguments[0]).y), nil
}

func javaCardWidth(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
	return uint32(client.javaCardBoxOf(arguments[0]).width), nil
}

func javaCardHeight(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
	return uint32(client.javaCardBoxOf(arguments[0]).height), nil
}

// javaCardMove is `move(x, y)`: the card's corner, for the next paint. A card
// without a box gets one the size of the screen.
func javaCardMove(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[0] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": Card.move")
	}
	box := client.javaCardBoxOf(arguments[0])
	box.x, box.y = int32(arguments[1]), int32(arguments[2])
	return 0, client.setJavaCardBox(arguments[0], box)
}

// javaCardResize is `resize(w, h)`: the card's size, for the next paint. A
// card without a box gets one at the screen's corner.
func javaCardResize(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[0] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": Card.resize")
	}
	box := client.javaCardBoxOf(arguments[0])
	box.width, box.height = int32(arguments[1]), int32(arguments[2])
	return 0, client.setJavaCardBox(arguments[0], box)
}

// javaCardIsShown is `isShown()`: whether the card is the one the display
// shows.
func javaCardIsShown(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return javaBoolean(arguments[0] != 0 && arguments[0] == client.javaRuntimeState().card), nil
}

// javaCardFrame is what one paint of the shown card draws through: the screen's
// Graphics, the surface it targets for this paint, and, for a card with a box,
// the box that surface goes on the screen at once the paint returns. The box is
// the one the paint started with, which is what makes a move or a resize take
// effect at the next paint rather than halfway through this one.
type javaCardFrame struct {
	graphics   uint32
	surface    *framebuffer
	box        javaCardBox
	positioned bool
}

// javaPaintTarget answers the surface the shown card paints into: the screen
// for a card without a box, and for one with a box the surface its paints keep,
// made again from the screen when it belongs to another card or is the wrong
// size.
func (client *Client) javaPaintTarget() (*framebuffer, javaCardBox, bool, error) {
	screen, err := client.screenSurface()
	if err != nil {
		return nil, javaCardBox{}, false, err
	}
	runtime := client.javaRuntimeState()
	box, positioned := runtime.cardBounds[runtime.card]
	if !positioned || runtime.card == 0 {
		client.dropJavaCardSurface()
		return screen, javaCardBox{}, false, nil
	}
	width, height := javaCardSurfaceSize(box, screen)
	surface := client.framebuffer(runtime.cardSurface)
	if surface == nil || surface.screen || runtime.cardSurfaceOwner != runtime.card ||
		surface.width != width || surface.height != height {
		fresh, err := client.newFramebuffer(width, height, false)
		if err != nil {
			return nil, javaCardBox{}, false, err
		}
		// The runtime's copy is the only one anything reads, as it is for every
		// surface a Java title draws on; see javaDraw.
		fresh.drawnHere = true
		javaCardBackdrop(fresh, screen, box)
		client.dropJavaCardSurface()
		runtime.cardSurface, runtime.cardSurfaceOwner = fresh.handle, runtime.card
		surface = fresh
		if client.logger != nil {
			client.logger.Debug("LGT java card surface made", "card", runtime.card,
				"x", box.x, "y", box.y, "width", box.width, "height", box.height)
		}
	}
	return surface, box, true, nil
}

// dropJavaCardSurface gives back the surface a card with a box painted into.
// It is called only where the screen's Graphics is pointed somewhere else next,
// because that Graphics is the one thing that draws on it.
func (client *Client) dropJavaCardSurface() {
	runtime := client.javaRuntimeState()
	if surface := client.framebuffer(runtime.cardSurface); surface != nil && !surface.screen {
		client.releaseSurface(surface)
	}
	runtime.cardSurface, runtime.cardSurfaceOwner = 0, 0
}

// javaCardSurfaceSize is how large a card's surface is: the card's size, at
// least one pixel each way and at most the screen's. A card larger than the
// screen keeps a part the size of the screen from its corner, which is all of
// it the screen can show while that corner is on the screen.
func javaCardSurfaceSize(box javaCardBox, screen *framebuffer) (int, int) {
	return min(max(int(box.width), 1), screen.width), min(max(int(box.height), 1), screen.height)
}

// javaCardBackdrop fills a card's new surface with what the screen holds under
// the box. What lies off the screen stays black.
func javaCardBackdrop(surface, screen *framebuffer, box javaCardBox) {
	javaCardRows(surface, screen, box, surface.width, surface.height,
		func(surfaceRow, screenRow []uint16) { copy(surfaceRow, screenRow) })
}

// composeJavaCard puts what a card with a box painted onto the screen at the
// box: the part of its surface inside the card's size, clipped to the screen.
// Nothing else of the screen changes.
func (client *Client) composeJavaCard(surface *framebuffer, box javaCardBox) {
	screen := client.screen
	javaCardRows(surface, screen, box, min(int(box.width), surface.width), min(int(box.height), surface.height),
		func(surfaceRow, screenRow []uint16) { copy(screenRow, surfaceRow) })
	// The screen's own copy is now the correct one, as it is after a Java draw;
	// the frame is published to guest memory at the end of the paint.
	screen.drawnHere = true
}

// javaCardRows walks the rows a card's surface and the screen share: the first
// width by height pixels of the surface, placed at the box's corner, clipped
// to the screen. Each call hands over one row of each, the same length. The
// corner is the title's, anywhere in 32 bits, so the sums are taken in 64.
func javaCardRows(
	surface, screen *framebuffer, box javaCardBox, width, height int, each func(surfaceRow, screenRow []uint16),
) {
	left, top := int64(box.x), int64(box.y)
	first := max(0, -left)
	last := min(int64(width), int64(screen.width)-left)
	if first >= last {
		return
	}
	for row := max(0, -top); row < int64(height) && top+row < int64(screen.height); row++ {
		at, start := (top+row)*int64(screen.width)+left, row*int64(surface.width)
		each(surface.pixels[start+first:start+last], screen.pixels[at+first:at+last])
	}
}
