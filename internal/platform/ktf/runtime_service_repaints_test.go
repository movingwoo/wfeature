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
