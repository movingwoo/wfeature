package ktf

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestAsyncWorkerRepaintDoesNotAdvanceWorldBetweenRequests(t *testing.T) {
	client, runtime := newTestRuntime(t)
	steps := 0
	if err := client.JVM().RegisterNative("test/AsyncCard", "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) { steps++; return jvm.VoidValue(), nil }); err != nil {
		t.Fatal(err)
	}
	card := &jvm.Object{ClassName: "test/AsyncCard"}
	runtime.displayCards = []*jvm.Object{card}
	worker := &guestWorker{}
	client.workers = []*guestWorker{worker}
	client.activeWorker = worker
	if _, err := runtimeCardRepaint(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(card)}); err != nil {
		t.Fatal(err)
	}
	client.activeWorker = nil
	for i := 0; i < 100; i++ {
		if _, err := runtime.paintTopCard(); err != nil {
			t.Fatal(err)
		}
	}
	if steps != 1 {
		t.Fatalf("one async repaint advanced world %d times", steps)
	}
	runtime.repaintPending = true
	if _, err := runtime.paintTopCard(); err != nil {
		t.Fatal(err)
	}
	if steps != 2 {
		t.Fatal("next requested frame was lost")
	}
	client.workers = nil
	if _, err := runtime.paintTopCard(); err != nil {
		t.Fatal(err)
	}
	if steps != 3 {
		t.Fatal("worker exit did not restore fallback")
	}
}

func TestCRepaintWithoutGuestEventLoopQueuesAPaint(t *testing.T) {
	_, runtime := newTestRuntime(t)
	thread := armcore.NewThread(armcore.Context{})
	if _, err := runtime.handleWIPICTableCall(thread, wipicTableGraphics, 25); err != nil {
		t.Fatal(err)
	}
	if !runtime.repaintPending {
		t.Fatal("MC_grpRepaint dropped without a guest event loop")
	}
}

// The callback requests paint and rearms itself through real Thumb calls to
// the platform table. Unrelated timers must not retain ownership after cancel.
func TestCRepaintTimerOwnsCadenceUntilCancelled(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	steps := 0
	if err := client.JVM().RegisterNative("test/TimerCard", "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) { steps++; return jvm.VoidValue(), nil }); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = []*jvm.Object{{ClassName: "test/TimerCard"}}
	repaint, err := runtime.stub(svcCategoryWIPIC, wipicTableGraphics<<16|25)
	if err != nil {
		t.Fatal(err)
	}
	rearm, err := runtime.stub(svcCategoryWIPIC, wipicKernelSetTimer)
	if err != nil {
		t.Fatal(err)
	}
	address := uint32((runtime.codeCursor + 3) &^ 3)
	runtime.codeCursor = uint64(address) + 32
	body := make([]byte, 32)
	// push {r4,r5,lr}; retain timer; repaint(0); setTimer(timer,120,0,0); return.
	for i, word := range []uint16{0xb530, 0x4604, 0x2000, 0x4d04, 0x47a8, 0x4620, 0x2178, 0x2200, 0x2300, 0x4d02, 0x47a8, 0xbd30} {
		binary.LittleEndian.PutUint16(body[i*2:], word)
	}
	binary.LittleEndian.PutUint32(body[24:], repaint)
	binary.LittleEndian.PutUint32(body[28:], rearm)
	if err := client.core.Memory().Load(address, body); err != nil {
		t.Fatal(err)
	}
	record, err := runtime.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	if err := armTimerRecord(t, client, runtime, record, address|1, 0, 120); err != nil {
		t.Fatal(err)
	}
	for frame := 1; frame <= 3; frame++ {
		clock.Advance(runtime.pendingTimers[0].due.Sub(client.now()))
		if count, err := client.ServiceTimers(t.Context(), 4); err != nil || count != 1 {
			t.Fatalf("frame=%d timer count=%d error=%v", frame, count, err)
		}
		for round := 0; round < 100; round++ {
			if _, err := runtime.paintTopCard(); err != nil {
				t.Fatal(err)
			}
		}
		if steps != frame {
			t.Fatalf("after %d requests: %d world steps", frame, steps)
		}
	}
	setTimer(t, client, runtime, 10000)
	if err := client.thread.SetRegister(0, record); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.wipicUnsetTimer(client.thread); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.paintTopCard(); err != nil {
		t.Fatal(err)
	}
	if steps != 4 {
		t.Fatalf("cancelled repaint timer blocked fallback: %d paints", steps)
	}
}

func TestRepeatingJavaTimerKeepsPaintCadenceAfterTaskReturns(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	steps := 0
	card := &jvm.Object{ClassName: "test/TimerPaintCard"}
	owner := &jvm.Object{ClassName: "java/util/Timer"}
	task := &jvm.Object{ClassName: "test/RepeatingPaintTask"}
	if err := client.JVM().RegisterNative(card.ClassName, "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) { steps++; return jvm.VoidValue(), nil }); err != nil {
		t.Fatal(err)
	}
	if err := client.JVM().RegisterNative(task.ClassName, "run", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		return runtimeCardRepaint(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(card)})
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = []*jvm.Object{card}
	runtime.pendingTimers = []wipicTimer{{owner: owner, task: task, period: 100 * time.Millisecond, due: client.now()}}
	t.Cleanup(client.StopThreads)
	for frame := 1; frame <= 3; frame++ {
		if _, err := client.ServiceTimers(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		if len(client.workers) != 0 {
			t.Fatal("timer task did not return")
		}
		for idle := 0; idle < 20; idle++ {
			if _, err := runtime.paintTopCard(); err != nil {
				t.Fatal(err)
			}
		}
		if steps != frame {
			t.Fatalf("%d timer requests caused %d world steps", frame, steps)
		}
		clock.Advance(100 * time.Millisecond)
	}
	// An unrelated task on the same Timer does not retain this card's cadence.
	runtime.pendingTimers = []wipicTimer{{owner: owner, task: &jvm.Object{ClassName: "test/OtherTask"}, due: client.now().Add(time.Second)}}
	if _, err := runtime.paintTopCard(); err != nil {
		t.Fatal(err)
	}
	if steps != 4 {
		t.Fatal("removed task retained paint ownership")
	}
}

func TestSerialRepaintKeepsCadenceWhileRunnablePolls(t *testing.T) {
	for _, synchronous := range []bool{false, true} {
		name := "async"
		if synchronous {
			name = "sync"
		}
		t.Run(name, func(t *testing.T) {
			clock := NewManualClock(time.Unix(1700000000, 0))
			client, runtime := newPacedTestRuntime(t, clock, 1)
			card := &jvm.Object{ClassName: "test/SerialPaintCard"}
			runnable := &jvm.Object{ClassName: "test/SerialPaintLoop"}
			other := &jvm.Object{ClassName: runnable.ClassName}
			paints, calls := 0, 0
			requeue := true
			if err := client.JVM().RegisterNative(card.ClassName, "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
				paints++
				return jvm.VoidValue(), nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := client.JVM().RegisterNative(runnable.ClassName, "run", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
				if calls%3 == 0 {
					args := []jvm.Value{jvm.ReferenceValue(card)}
					if _, err := runtimeCardRepaint(runtime, client.JVM(), args); err != nil {
						return jvm.VoidValue(), err
					}
					if synchronous {
						if _, err := runtimeCardServiceRepaints(runtime, client.JVM(), args); err != nil {
							return jvm.VoidValue(), err
						}
					}
				}
				calls++
				if requeue {
					return runtimeDisplayCallSerially(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(nil), jvm.ReferenceValue(runnable)})
				}
				return runtimeDisplayCallSerially(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(nil), jvm.ReferenceValue(other)})
			}); err != nil {
				t.Fatal(err)
			}
			runtime.displayCards = []*jvm.Object{card}
			runtime.pendingSerial = []*jvm.Object{runnable}
			for round := 0; round < 6; round++ {
				if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
					t.Fatal(err)
				}
				for idle := 0; idle < 20; idle++ {
					if _, err := runtime.paintTopCard(); err != nil {
						t.Fatal(err)
					}
				}
				if want := round/3 + 1; paints != want {
					t.Fatalf("%d requests caused %d paints", want, paints)
				}
				clock.Advance(serialDispatchInterval)
			}
			// The last invocation stops queueing itself. Its final requested
			// frame still runs, then automatic painting becomes eligible again.
			// Another instance of the same Runnable class inherits nothing.
			requeue = false
			if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
				t.Fatal(err)
			}
			for idle := 0; idle < 20; idle++ {
				if _, err := runtime.paintTopCard(); err != nil {
					t.Fatal(err)
				}
			}
			if paints <= 3 {
				t.Fatal("finished serial loop retained paint ownership")
			}
		})
	}
}
