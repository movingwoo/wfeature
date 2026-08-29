package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// postEventThroughSVC drives MC_grpPostEvent(id, type, param1, param2) the way
// a game does: through the graphics table's supervisor call, with the four
// arguments in r0 to r3.
func postEventThroughSVC(t *testing.T, runtime *initializationRuntime, program, kind, first, second uint32) uint32 {
	t.Helper()
	callContext := armcore.NewContext()
	callContext.Registers[0] = program
	callContext.Registers[1] = kind
	callContext.Registers[2] = first
	callContext.Registers[3] = second
	answer, err := runtime.handleWIPICCall(armcore.NewThread(callContext), wipicTableGraphics<<16|36)
	if err != nil {
		t.Fatalf("MC_grpPostEvent error = %v", err)
	}
	return answer
}

func currentProgramIDThroughSVC(t *testing.T, runtime *initializationRuntime) uint32 {
	t.Helper()
	identity, err := runtime.handleWIPICCall(armcore.NewThread(armcore.NewContext()), wipicKernelGetCurProgramID)
	if err != nil {
		t.Fatalf("MC_knlGetCurProgramID error = %v", err)
	}
	return identity
}

// A title asks for its own identifier once and hands it back to every call that
// names a program, so the only property the answer needs is that it does not
// change under it — and that it is not the zero a caller reads as "no program".
func TestCurrentProgramIDIsStableAndNotZero(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.SetProgramName("0102DD43")

	identity := currentProgramIDThroughSVC(t, runtime)
	if identity == 0 {
		t.Fatal("MC_knlGetCurProgramID answered zero")
	}
	if again := currentProgramIDThroughSVC(t, runtime); again != identity {
		t.Fatalf("MC_knlGetCurProgramID answered %#x then %#x", identity, again)
	}

	client.SetProgramName("010100D5")
	if other := currentProgramIDThroughSVC(t, runtime); other == identity {
		t.Fatalf("two archives share the identifier %#x", identity)
	}
}

// The pairing the local titles use: a middleware wrapper posts its own message
// codes to itself with the identifier the kernel just gave it, and the event
// has to come back through the listener the Jlet registered.
func TestPostedEventReachesTheJletListener(t *testing.T) {
	client, runtime := newTestRuntime(t)
	var seen []int32
	if err := client.JVM().RegisterNative("test/Listener", "notifyEvent", "(III)V", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		for _, argument := range arguments[1:] {
			value, err := argument.Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			seen = append(seen, value)
		}
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.jletListeners = append(runtime.jletListeners, &jvm.Object{ClassName: "test/Listener", Fields: make(map[string]jvm.Value)})

	if answer := postEventThroughSVC(t, runtime, currentProgramIDThroughSVC(t, runtime), 0xa600, 7, 9); answer != wipicPostEventQueued {
		t.Fatalf("MC_grpPostEvent answered %d, want %d", answer, wipicPostEventQueued)
	}
	// Nothing is delivered inside the call: a title posts from inside its own
	// paint, and delivering there would re-enter the card while it is drawing.
	if len(seen) != 0 {
		t.Fatalf("the event was delivered inside the call: %v", seen)
	}

	delivered, err := client.ServiceEvents(t.Context())
	if err != nil {
		t.Fatalf("ServiceEvents() error = %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered %d events, want 1", delivered)
	}
	if len(seen) != 3 || seen[0] != 0xa600 || seen[1] != 7 || seen[2] != 9 {
		t.Fatalf("listener received %v, want [%d 7 9]", seen, 0xa600)
	}
	if len(runtime.events) != 0 {
		t.Fatalf("%d events are still queued", len(runtime.events))
	}
}

// A game that runs its own getNextEvent loop is the reader of its own queue.
// Draining it from the Host as well would deliver every event twice.
func TestPostedEventStaysQueuedForAGuestEventLoop(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.guestEventLoop = true

	postEventThroughSVC(t, runtime, currentProgramIDThroughSVC(t, runtime), 0xa002, 0, 0)
	delivered, err := client.ServiceEvents(t.Context())
	if err != nil {
		t.Fatalf("ServiceEvents() error = %v", err)
	}
	if delivered != 0 {
		t.Fatalf("delivered %d events, want none", delivered)
	}
	if len(runtime.events) != 1 {
		t.Fatalf("%d events are queued, want the posted one", len(runtime.events))
	}
}

// A handler that answers an event with another event is queuing for the next
// round. Delivering what it posts in the same round would let two of them hold
// the round for ever.
func TestServiceEventsDeliversOnlyWhatWasWaiting(t *testing.T) {
	client, runtime := newTestRuntime(t)
	posts := 0
	if err := client.JVM().RegisterNative("test/Listener", "notifyEvent", "(III)V", func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
		posts++
		runtime.postGuestEvent(guestEvent{kind: eventKindNotify, param1: 1})
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.jletListeners = append(runtime.jletListeners, &jvm.Object{ClassName: "test/Listener", Fields: make(map[string]jvm.Value)})

	postEventThroughSVC(t, runtime, currentProgramIDThroughSVC(t, runtime), 0xa003, 0, 0)
	delivered, err := client.ServiceEvents(t.Context())
	if err != nil {
		t.Fatalf("ServiceEvents() error = %v", err)
	}
	if delivered != 1 || posts != 1 {
		t.Fatalf("delivered %d events with %d handler runs, want 1 and 1", delivered, posts)
	}
	if len(runtime.events) != 1 {
		t.Fatalf("%d events are queued, want the one the handler posted", len(runtime.events))
	}
}
