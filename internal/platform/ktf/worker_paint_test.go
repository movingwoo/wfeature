package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// A worker can prepare its next frame in stages. An unsolicited paint between
// those stages may clear the application's dirty flag before the next stage
// consumes it, leaving the scene waiting for input it cannot yet handle.
func TestWorkerPaintKeepsFrameStateUntilTheNextRequest(t *testing.T) {
	for _, handover := range []string{"worker exits", "another card is shown"} {
		t.Run(handover, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			painted, pendingStage := 0, false
			if err := client.JVM().RegisterNative("test/Card", "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V",
				func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
					painted++
					pendingStage = false
					return jvm.VoidValue(), nil
				}); err != nil {
				t.Fatal(err)
			}
			card := &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)}
			runtime.displayCards = append(runtime.displayCards, card)
			worker := &guestWorker{}
			client.workers = []*guestWorker{worker}
			client.activeWorker = worker
			runtime.repaintPending = true
			if _, err := runtimeCardServiceRepaints(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(card)}); err != nil {
				t.Fatal(err)
			}
			client.activeWorker = nil
			pendingStage = true
			for round := 0; round < 1000; round++ {
				if _, err := runtime.paintTopCard(); err != nil {
					t.Fatal(err)
				}
			}
			if painted != 1 || !pendingStage {
				t.Fatalf("unrequested paint consumed the next stage: paints=%d, pending=%v", painted, pendingStage)
			}

			// An explicit request still reaches the card immediately.
			runtime.repaintPending = true
			if _, err := runtime.paintTopCard(); err != nil {
				t.Fatal(err)
			}
			if painted != 2 || pendingStage {
				t.Fatalf("requested paint was withheld: paints=%d, pending=%v", painted, pendingStage)
			}

			// Ownership covers this card only while its worker remains live.
			if handover == "worker exits" {
				client.workers = nil
			} else {
				runtime.displayCards = append(runtime.displayCards, &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)})
			}
			for round := 0; round <= guestPaintOwnershipRounds; round++ {
				if _, err := runtime.paintTopCard(); err != nil {
					t.Fatal(err)
				}
			}
			if painted <= 2 {
				t.Fatal("automatic painting did not resume after handover")
			}
		})
	}
}
