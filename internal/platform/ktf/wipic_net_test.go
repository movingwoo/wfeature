package ktf

import (
	"context"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func netCall(t *testing.T, runtime *initializationRuntime, function uint32, arguments ...uint32) uint32 {
	t.Helper()
	callContext := armcore.NewContext()
	for index, value := range arguments {
		callContext.Registers[index] = value
	}
	result, err := runtime.handleWIPICTableCall(armcore.NewThread(callContext), wipicTableNet, function)
	if err != nil {
		t.Fatalf("net function %d: %v", function, err)
	}
	return result
}

// A caller that registers a callback is told the connection attempt started and
// is failed through the callback, because that is where a title listening for
// the answer is listening. Refusing at the call leaves one local title on its
// "waiting for the server" screen with nothing to react to.
func TestNetConnectFailsThroughTheCallbackItWasGiven(t *testing.T) {
	_, runtime := newTestRuntime(t)

	if code := netCall(t, runtime, wipicNetConnect, ImageBase|1, 0x1234); code != 0 {
		t.Fatalf("MC_netConnect with a callback answered %#x, want 0", code)
	}
	if len(runtime.pendingNetCallbacks) != 1 {
		t.Fatalf("%d callbacks queued, want 1", len(runtime.pendingNetCallbacks))
	}
	if got := runtime.pendingNetCallbacks[0]; got.callback != ImageBase|1 || got.param != 0x1234 {
		t.Fatalf("queued %+v, want the callback and parameter the game passed", got)
	}
}

// A caller that passes no callback keeps the old answer: there is nowhere to
// deliver a failure, so the synchronous error is the only "no" available. This
// is what keeps titles that never register one from noticing this code.
func TestNetConnectWithoutACallbackStillRefuses(t *testing.T) {
	_, runtime := newTestRuntime(t)

	if code := netCall(t, runtime, wipicNetConnect, 0, 0); code != wipiErrorCode {
		t.Fatalf("MC_netConnect without a callback answered %#x, want %#x", code, wipiErrorCode)
	}
	if len(runtime.pendingNetCallbacks) != 0 {
		t.Fatal("a call with no callback queued one anyway")
	}
}

// The specification has MC_netClose drop every callback the net API registered,
// which also keeps a title that gives up first from being called back after it
// has torn its own state down.
func TestNetCloseDropsThePendingCallbacks(t *testing.T) {
	_, runtime := newTestRuntime(t)

	netCall(t, runtime, wipicNetConnect, ImageBase|1, 0x1234)
	if code := netCall(t, runtime, wipicNetClose); code != 0 {
		t.Fatalf("MC_netClose answered %#x, want 0", code)
	}
	if len(runtime.pendingNetCallbacks) != 0 {
		t.Fatal("close left a callback queued")
	}
}

// The queue is bounded: the callback address is a word out of guest memory and
// a title looping on connect would otherwise grow it without end. Past the
// bound the call refuses, which is the answer that needs no queue.
func TestNetConnectQueueIsBounded(t *testing.T) {
	_, runtime := newTestRuntime(t)

	for round := 0; round < maxPendingNetCallbacks; round++ {
		if code := netCall(t, runtime, wipicNetConnect, ImageBase|1, uint32(round)); code != 0 {
			t.Fatalf("round %d answered %#x, want 0", round, code)
		}
	}
	if code := netCall(t, runtime, wipicNetConnect, ImageBase|1, 0); code != wipiErrorCode {
		t.Fatalf("past the bound MC_netConnect answered %#x, want a refusal", code)
	}
	if len(runtime.pendingNetCallbacks) != maxPendingNetCallbacks {
		t.Fatalf("queue holds %d, want the bound %d",
			len(runtime.pendingNetCallbacks), maxPendingNetCallbacks)
	}
}

// The rest of the table still refuses: without a connection there is nothing a
// socket call can answer, and a game's own error path expects the refusal.
func TestTheRestOfTheNetTableStillRefuses(t *testing.T) {
	_, runtime := newTestRuntime(t)

	for _, function := range []uint32{2, 3, 4, 9} {
		if code := netCall(t, runtime, function); code != wipiErrorCode {
			t.Fatalf("net function %d answered %#x, want %#x", function, code, wipiErrorCode)
		}
	}
}

// The failure is delivered where timer callbacks are delivered, and the guest
// receives M_E_ERROR and the parameter it registered.
func TestServiceDeliversTheNetworkFailureToTheGuest(t *testing.T) {
	client, runtime := newTestRuntime(t)

	// A callback that stores its two arguments where the test can read them.
	slot, err := runtime.allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	// The literal sits at (pc & ~3) + 8, twelve bytes into a word-aligned entry.
	body := []uint16{
		0x4a02,         // ldr r2, [pc, #8]
		0x6010,         // str r0, [r2, #0]
		0x6051,         // str r1, [r2, #4]
		0x4770,         // bx lr
		0x0000, 0x0000, // padding, so the literal lands twelve bytes in
		uint16(slot), uint16(slot >> 16),
	}
	data := make([]byte, len(body)*2)
	for index, instruction := range body {
		data[index*2] = byte(instruction)
		data[index*2+1] = byte(instruction >> 8)
	}
	const code = ImageBase + 0x40
	if err := client.core.Memory().Write(code, data); err != nil {
		t.Fatal(err)
	}

	netCall(t, runtime, wipicNetConnect, code|1, 0x5678)
	delivered, err := client.serviceNetCallbacks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if delivered != 1 {
		t.Fatalf("delivered %d callbacks, want 1", delivered)
	}
	words, err := runtime.readAOTWords(slot, 2, "net callback arguments")
	if err != nil {
		t.Fatal(err)
	}
	if words[0] != wipiErrorCode {
		t.Fatalf("callback received error %#x, want M_E_ERROR %#x", words[0], wipiErrorCode)
	}
	if words[1] != 0x5678 {
		t.Fatalf("callback received param %#x, want the registered %#x", words[1], 0x5678)
	}
	if len(runtime.pendingNetCallbacks) != 0 {
		t.Fatal("a delivered callback stayed queued")
	}
}
