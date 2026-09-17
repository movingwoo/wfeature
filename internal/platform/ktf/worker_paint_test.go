package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestForcedSlicePaintRequiresARequestAndYieldRestoresFallback(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	paints := 0
	if err := client.vm.RegisterNative("test/SliceCard", "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		paints++
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.vm.RegisterNative("test/SliceWorker", "run", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		if err := client.activeWorker.parkSlice(true); err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), runtime.yieldCurrentWorker()
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = []*jvm.Object{{ClassName: "test/SliceCard"}}
	runtime.pendingThreads = []*jvm.Object{{ClassName: "test/SliceWorker"}}
	t.Cleanup(client.StopThreads)
	if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ServicePaint(t.Context()); err != nil {
		t.Fatal(err)
	}
	if paints != 0 {
		t.Fatal("forced slice triggered an unrequested paint")
	}
	runtime.repaintPending = true
	if _, err := client.ServicePaint(t.Context()); err != nil {
		t.Fatal(err)
	}
	if paints != 1 {
		t.Fatal("forced slice swallowed an explicit repaint")
	}
	clock.Advance(time.Second)
	if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ServicePaint(t.Context()); err != nil {
		t.Fatal(err)
	}
	if paints != 2 {
		t.Fatal("explicit yield starved automatic painting")
	}
	clock.Advance(time.Second)
	if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ServicePaint(t.Context()); err != nil {
		t.Fatal(err)
	}
	if paints != 3 {
		t.Fatal("idle boundary starved automatic painting")
	}
}

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
