package lgt

import (
	"context"
	"testing"
	"time"
)

// guestThumbStub writes Thumb code into the platform's own executable region —
// the guest arena is mapped without execute — and answers the address to call
// it at, with the Thumb bit set.
func guestThumbStub(t *testing.T, client *Client, words ...uint32) uint32 {
	t.Helper()
	address := client.codeCurse
	client.codeCurse += uint32(len(words)) * 4
	for index, word := range words {
		if err := client.writeWord(address+uint32(index)*4, word); err != nil {
			t.Fatal(err)
		}
	}
	return address | 1
}

// reportingCallback is `str r0, [r1]` followed by `bx lr`: it writes its first
// argument — the error the platform reports — to the address its second
// argument names, which is the parameter the dial registered.
const reportingCallback = uint32(0x47706008)

// A dial is accepted and then fails through its callback, because the
// specification makes the callback the only way an accepted MC_netConnect
// reports anything. Answering the error synchronously and never calling back
// leaves a title that waits on the callback waiting forever.
func TestNetConnectFailsThroughItsCallback(t *testing.T) {
	client := fixtureClient(t)

	slot, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	callback := guestThumbStub(t, client, reportingCallback)

	if result := int32(callSlot(t, client, slotNetConnect, callback, slot)); result != wipiSuccess {
		t.Fatalf("MC_netConnect = %d, want %d: an accepted dial is the only kind that calls back",
			result, wipiSuccess)
	}

	// Nothing is reported before the dial has had time to fail. A callback
	// that fires while the caller is still entering its connecting state is
	// delivered to a state machine that is not listening yet.
	if err := client.serviceNetConnects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.netConnects) != 1 {
		t.Fatalf("pending dials = %d, want the dial to still be waiting", len(client.netConnects))
	}
	if written, err := client.readWord(slot); err != nil || written != 0 {
		t.Fatalf("the callback reported %#x before its time (err %v)", written, err)
	}

	client.clock.advance(netConnectFailureDelay + time.Millisecond)
	if err := client.serviceNetConnects(context.Background()); err != nil {
		t.Fatalf("serviceNetConnects: %v", err)
	}
	written, err := client.readWord(slot)
	if err != nil {
		t.Fatal(err)
	}
	if int32(written) != wipiError {
		t.Fatalf("callback error = %#x, want M_E_ERROR (%d)", written, wipiError)
	}
	if len(client.netConnects) != 0 {
		t.Fatalf("pending dials = %d, want the reported dial to be dropped", len(client.netConnects))
	}

	// Reporting happens once. A second service call must not run the callback
	// again, or a game counts two failures for one attempt.
	if err := client.writeWord(slot, 0); err != nil {
		t.Fatal(err)
	}
	if err := client.serviceNetConnects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if again, err := client.readWord(slot); err != nil || again != 0 {
		t.Fatalf("the callback ran a second time (%#x, err %v)", again, err)
	}
}

// A dial with nowhere to report to is refused where the rest of the block is.
func TestNetConnectWithoutCallbackFailsImmediately(t *testing.T) {
	client := fixtureClient(t)

	if result := int32(callSlot(t, client, slotNetConnect, 0, 0)); result != wipiError {
		t.Fatalf("MC_netConnect(nil) = %d, want %d", result, wipiError)
	}
	if len(client.netConnects) != 0 {
		t.Fatalf("pending dials = %d, want none", len(client.netConnects))
	}
}

// MC_netClose ends the application's internet access, so a refusal that has not
// been reported yet is answering a question the game has withdrawn.
func TestNetCloseDropsUnreportedDials(t *testing.T) {
	client := fixtureClient(t)

	slot, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	callSlot(t, client, slotNetConnect, guestThumbStub(t, client, reportingCallback), slot)
	if len(client.netConnects) != 1 {
		t.Fatalf("pending dials = %d, want the dial to be waiting", len(client.netConnects))
	}

	callSlot(t, client, slotNetClose)
	if len(client.netConnects) != 0 {
		t.Fatalf("pending dials = %d, want MC_netClose to have dropped it", len(client.netConnects))
	}

	client.clock.advance(netConnectFailureDelay + time.Millisecond)
	if err := client.serviceNetConnects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if written, err := client.readWord(slot); err != nil || written != 0 {
		t.Fatalf("a dropped dial still reported %#x (err %v)", written, err)
	}
}

// The rest of the block still answers with its return value: those calls have
// no callback to report through, and a title refused a connection tears down
// what it started, so every one of them has to answer rather than stop the run.
func TestRestOfNetBlockStillReportsFailure(t *testing.T) {
	client := fixtureClient(t)

	for _, slot := range []uint32{
		slotNetSocketConnect, slotNetSocketWrite, slotNetSocketRead,
		slotNetSocketClose, slotNetSetReadCB, slotNetSetWriteCB,
	} {
		if result := int32(callSlot(t, client, slot)); result != wipiError {
			t.Errorf("slot %#x = %d, want %d", slot, result, wipiError)
		}
	}
	if result := int32(callSlot(t, client, slotNetClose)); result != wipiSuccess {
		t.Errorf("MC_netClose = %d, want %d because it returns void", result, wipiSuccess)
	}
}
