package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The frame loop of an AOT Java title.
//
// A Clet is driven by the platform calling the entry points it registered. A
// Java title registers nothing: it builds a `Card`, pushes it onto the display,
// and the platform is what calls the card's `paint` from then on. So the loop
// is here — one call per tick into a method of the application's own class,
// with a `Graphics` over the screen.
//
// **A card only paints when it has asked to.** `repaint` is the request, and a
// title that does its work in a thread and repaints at the end of it would
// otherwise be painted mid-update. The first paint is the one the push itself
// asks for, which is what the specification says a pushed card gets.

// javaCardPaintMethod and javaCardKeyMethod are the application's own methods
// the platform enters. Their names are the specification's; the descriptors are
// what the local titles declare.
const (
	javaCardPaintMethod = "paint"
	javaCardKeyMethod   = "keyNotify"
)

// javaPushCard is `Display.pushCard(Card)`. The card becomes the screen, and
// the platform starts painting it.
func javaPushCard(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	if arguments[1] == 0 {
		return 0, fmt.Errorf("a null card was pushed")
	}
	runtime.card = arguments[1]
	runtime.cardDirty = true
	if client.logger != nil {
		if class, known := client.javaClassOfObject(arguments[1]); known {
			client.logger.Debug("LGT java card pushed", "class", class.Name)
		}
	}
	return 0, nil
}

// javaCardRepaint is `Card.repaint`, in both the whole-card and the rectangle
// form. **The rectangle is not honoured**: this platform repaints the whole
// card, which is what a title that paints its own background every frame does
// anyway, and drawing more than was asked for is visible only as work.
func javaCardRepaint(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	if runtime.card == 0 {
		runtime.card = arguments[0]
	}
	runtime.cardDirty = true
	return 0, nil
}

// javaRemoveAllCards is `Display.removeAllCards()`. Nothing is shown after it
// until something is pushed again, and the platform has no card to paint.
func javaRemoveAllCards(
	client *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	runtime.card = 0
	runtime.cardDirty = false
	return 0, nil
}

// javaServiceRepaints is `Card.serviceRepaints()`: paint now rather than at the
// next frame, which is what the specification defines it as — the call returns
// once the repaint it was waiting for has been served. **It runs on whichever
// thread called it**, because that thread's stack is the live one; a title that
// calls this from its own game thread paints from inside it.
func javaServiceRepaints(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	if runtime.card == 0 {
		runtime.card = arguments[0]
	}
	if !runtime.cardDirty {
		return 0, nil
	}
	return 0, client.paintJavaCard(ctx, thread)
}

// PaintJava paints the pushed card, if the application has asked for one, and
// presents the result. It is what `PaintClet` is for a Clet, and a session
// calls both: a title is one or the other, and the one it is not does nothing.
func (client *Client) PaintJava(ctx context.Context) error {
	if client.javaRun == nil || client.javaRun.card == 0 || !client.javaRun.cardDirty {
		return nil
	}
	return client.paintJavaCard(ctx, client.thread)
}

// paintJavaCard runs one frame: hand the card a Graphics over the screen, let
// it paint, and present what it drew.
func (client *Client) paintJavaCard(ctx context.Context, thread *armcore.Thread) error {
	client.javaRun.cardDirty = false
	graphics, err := client.javaScreenGraphics()
	if err != nil {
		return err
	}
	if err := client.callJavaCardMethod(ctx, thread, javaCardPaintMethod, graphics); err != nil {
		return err
	}
	// The frame goes the other way here than it does on the Clet path. A Clet
	// wrote its pixels into guest memory itself, so a flush reads them out; a
	// Java title drew into the runtime's copy through `Graphics` and never
	// touched guest memory, so a flush **publishes**. Reading here instead
	// would overwrite the frame that was just painted with whatever guest
	// memory still held. See javaDraw for why the two syncs live here rather
	// than around every drawing call.
	if err := client.syncToGuest(client.screen); err != nil {
		return err
	}
	client.mu.Lock()
	client.framePending = true
	client.flushes++
	client.mu.Unlock()
	return nil
}

// javaScreenGraphics answers the Graphics a card paints through, building it
// once and resetting the state a paint starts with. **The object is the same
// every frame**: a title that keeps the Graphics it was handed — and the local
// ones do, in a field — has to keep one that still works.
func (client *Client) javaScreenGraphics() (uint32, error) {
	runtime := client.javaRuntimeState()
	screen, err := client.screenSurface()
	if err != nil {
		return 0, err
	}
	if runtime.screenGraphics == 0 {
		object, err := client.newJavaGraphics(screen.handle)
		if err != nil {
			return 0, err
		}
		runtime.screenGraphics = object
	}
	state, err := client.javaGraphicsState(runtime.screenGraphics)
	if err != nil {
		return 0, err
	}
	*state = *newJavaGraphicsState(screen.handle)
	return runtime.screenGraphics, nil
}

// callJavaCardMethod enters one of the application card's own methods. The
// method is found by name on the card's class or on what it extends, the way
// the launcher finds `startApp`, and it is entered on the platform's own thread
// because nothing of the guest's is running when a frame is painted.
func (client *Client) callJavaCardMethod(
	ctx context.Context, thread *armcore.Thread, name string, arguments ...uint32,
) error {
	runtime := client.javaRuntimeState()
	class, known := client.javaClassOfObject(runtime.card)
	if !known {
		return fmt.Errorf("the card at %#x is not an object this platform issued", runtime.card)
	}
	method, owner, ok := client.findJavaMethod(class.Record, name)
	if !ok {
		// A card that declares no `keyNotify` is not a failure — it is a card
		// that does not take keys. One that declares no `paint` is not a card.
		if name == javaCardPaintMethod {
			return fmt.Errorf("%s declares no %s", class.Name, name)
		}
		return nil
	}
	call := append([]uint32{runtime.card}, arguments...)
	if _, err := client.callOn(ctx, thread, method.Body, call); err != nil {
		return fmt.Errorf("run %s.%s%s at %#x: %w", owner, name, method.Descriptor, method.Body, err)
	}
	return nil
}

// deliverJavaKey hands one key press or release to the pushed card, as the
// `keyNotify(int type, int key)` the specification defines: the type is press
// or release, and the key is the platform's own code — the same code a Clet's
// event carries, so one keypad serves both.
func (client *Client) deliverJavaKey(ctx context.Context, pressed bool, key uint32) error {
	if client.javaRun == nil || client.javaRun.card == 0 {
		return nil
	}
	kind := uint32(javaKeyReleased)
	if pressed {
		kind = javaKeyPressed
	}
	if client.logger != nil {
		// Which card was handed the key, by name. A title that ignores a key
		// and a title that never sees it look the same on the screen, and the
		// card's own class is what tells them apart — the handler is found on
		// it or on what it extends, so the name is also the answer to "whose
		// keyNotify ran".
		class := "the card"
		if named, known := client.javaClassOfObject(client.javaRun.card); known {
			class = named.Name
		}
		client.logger.Debug("LGT java key delivered",
			"card", class, "type", kind, "key", int32(key))
	}
	if err := client.callJavaCardMethod(ctx, client.thread, javaCardKeyMethod, kind, key); err != nil {
		return err
	}
	// A key is a reason to draw the next frame: a card that repaints itself
	// from its own handler has already said so, and one that does not would
	// otherwise show the state before the key until something else asked.
	client.javaRun.cardDirty = true
	return nil
}

// The event types `keyNotify` takes. They are the specification's own
// `Card.KEY_PRESSED` and `KEY_RELEASED`.
const (
	javaKeyPressed  = 1
	javaKeyReleased = 2
)
