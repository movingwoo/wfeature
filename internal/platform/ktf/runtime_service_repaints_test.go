package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// `Card.serviceRepaints` enters `paint` itself, which is what the
// specification says — but only for the card the display is showing. One local
// title loads its resources in stages inside `startApp` and calls `repaint` and
// `serviceRepaints` between them to move a progress bar, on a card it pushes
// only once the load is done: entering `paint` there ran its drawing code
// against a state it had not built yet and stopped it in its own null check.
// The frame loop already refuses a card that is not shown; this is the one call
// that did not.
func TestServiceRepaintsOnlyPaintsTheCardTheDisplayShows(t *testing.T) {
	client, runtime := newTestRuntime(t)
	painted := 0
	if err := client.JVM().RegisterNative("test/Card", "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V",
		func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
			painted++
			return jvm.VoidValue(), nil
		}); err != nil {
		t.Fatal(err)
	}
	card := &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)}

	if _, err := runtimeCardRepaint(runtime, client.JVM(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeCardServiceRepaints(runtime, client.JVM(),
		[]jvm.Value{jvm.ReferenceValue(card)}); err != nil {
		t.Fatalf("serviceRepaints() on a card that was never pushed: %v", err)
	}
	if painted != 0 {
		t.Fatalf("paints of a card the display is not showing = %d, want 0", painted)
	}
	// The request is still owed: refusing to paint must not swallow it, or the
	// frame that does come never repaints either.
	if !runtime.repaintPending {
		t.Error("the pending repaint was consumed by a card that is not shown")
	}

	runtime.displayCards = append(runtime.displayCards, card)
	if _, err := runtimeCardServiceRepaints(runtime, client.JVM(),
		[]jvm.Value{jvm.ReferenceValue(card)}); err != nil {
		t.Fatalf("serviceRepaints() on the card being shown: %v", err)
	}
	if painted != 1 {
		t.Fatalf("paints of the card the display is showing = %d, want 1", painted)
	}

	// A card below the top is not the one being shown either, which is what
	// `Card.isShown` has always answered.
	top := &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)}
	runtime.displayCards = append(runtime.displayCards, top)
	runtime.repaintPending = true
	if _, err := runtimeCardServiceRepaints(runtime, client.JVM(),
		[]jvm.Value{jvm.ReferenceValue(card)}); err != nil {
		t.Fatal(err)
	}
	if painted != 1 {
		t.Fatalf("paints of a covered card = %d, want none", painted-1)
	}
}

// A title that paints its own frames gets the frames it asked for and no
// others. The Host paints the top card every round for the titles that never
// ask; once a title has serviced a repaint of its own, that round paint is a
// second frame it did not ask for — and a title whose frame loop lives inside
// `paint` advances its world every time one is entered. One local title
// scrolls there, and the extra entries laid the same column of terrain down at
// three offsets: its slopes came out as a sawtooth and its ground left the
// screen.
func TestHostStopsPaintingOnceTheGuestServicesItsOwnRepaints(t *testing.T) {
	client, runtime := newTestRuntime(t)
	painted := 0
	if err := client.JVM().RegisterNative("test/Card", "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V",
		func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
			painted++
			return jvm.VoidValue(), nil
		}); err != nil {
		t.Fatal(err)
	}
	card := &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)}
	runtime.displayCards = append(runtime.displayCards, card)

	// Before the title has asked for anything, the round paint is the only
	// thing putting a frame on the screen and it goes ahead.
	if _, err := runtime.paintTopCard(); err != nil {
		t.Fatal(err)
	}
	if painted != 1 {
		t.Fatalf("round paints before the title asked = %d, want 1", painted)
	}

	// The title asks, and services the request itself.
	if _, err := runtimeCardRepaint(runtime, client.JVM(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeCardServiceRepaints(runtime, client.JVM(),
		[]jvm.Value{jvm.ReferenceValue(card)}); err != nil {
		t.Fatal(err)
	}
	if painted != 2 {
		t.Fatalf("paints after the title serviced its own repaint = %d, want 2", painted)
	}

	// From here the round paint stands down, for longer than the gap a title
	// driving its own screen leaves between frames.
	for round := 0; round < guestPaintOwnershipRounds; round++ {
		if _, err := runtime.paintTopCard(); err != nil {
			t.Fatal(err)
		}
	}
	if painted != 2 {
		t.Fatalf("round paints while the title was still painting = %d, want none", painted-2)
	}

	// **And it comes back when the title stops.** A title that services a
	// repaint two or three times during a load screen and then draws the rest
	// of its run from a round paint it never asks for would otherwise show one
	// frame for ever; three local titles do exactly that.
	if _, err := runtime.paintTopCard(); err != nil {
		t.Fatal(err)
	}
	if painted != 3 {
		t.Fatalf("round paints once the title stopped = %d, want 1", painted-2)
	}

	// A frame the title does ask for always arrives, whichever side enters
	// paint: an outstanding request is never what this stands down for.
	if _, err := runtimeCardServiceRepaints(runtime, client.JVM(),
		[]jvm.Value{jvm.ReferenceValue(card)}); err == nil && painted != 3 {
		t.Fatal("servicing with nothing pending painted anyway")
	}
	if _, err := runtimeCardRepaint(runtime, client.JVM(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.paintTopCard(); err != nil {
		t.Fatal(err)
	}
	if painted != 4 {
		t.Fatalf("paints of a frame the title asked for = %d, want 4", painted)
	}
}
