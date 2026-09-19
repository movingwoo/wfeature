package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// A self-requeueing UI callback must not consume the worker's only grant.
func TestSerialRunnableDoesNotStarveStartedWorker(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	serial := &jvm.Object{ClassName: "test/BusySerial"}
	serialRuns, workerRuns := 0, 0
	if err := client.vm.RegisterNative(serial.ClassName, "run", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		serialRuns++
		runtime.pendingSerial = append(runtime.pendingSerial, serial)
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.vm.RegisterNative("test/ReceiverWorker", "run", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		for i := 0; i < 3; i++ {
			workerRuns++
			if err := runtime.yieldCurrentWorker(); err != nil {
				return jvm.VoidValue(), err
			}
		}
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.pendingSerial = []*jvm.Object{serial}
	runtime.pendingThreads = []*jvm.Object{{ClassName: "test/ReceiverWorker"}}
	t.Cleanup(client.StopThreads)
	for i := 1; i <= 3; i++ {
		if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		if serialRuns != i || workerRuns != i {
			t.Fatalf("round %d: serial=%d worker=%d", i, serialRuns, workerRuns)
		}
		clock.Advance(time.Second)
	}
	if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if len(client.workers) != 0 {
		t.Fatal("completed worker did not retire")
	}
}
