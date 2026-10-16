package lgt

import (
	"context"
	"testing"
	"time"
)

// Two timers that come due in one tick fire in turn, and the first one's
// callback may cancel or re-arm the second. The second then fires the way its
// latest arming says — not at all, or at its new deadline with its new
// parameter — rather than once more for the arming that was replaced.
func TestATimerChangedByAnEarlierCallbackInTheSameTickFollowsTheChange(t *testing.T) {
	setup := func(t *testing.T) (client *Client, first, second, cancel, rearm uint32) {
		t.Helper()
		client = fixtureClient(t)
		var err error
		if first, err = client.allocate(16); err != nil {
			t.Fatal(err)
		}
		if second, err = client.allocate(16); err != nil {
			t.Fatal(err)
		}
		unset, err := client.stub(svcCategoryWIPIC, slotUnsetTimer)
		if err != nil {
			t.Fatal(err)
		}
		set, err := client.stub(svcCategoryWIPIC, slotSetTimer)
		if err != nil {
			t.Fatal(err)
		}
		a := &assembler{base: checkpointCodeBase}
		// callback(structure, param): count the call at +8 and keep the
		// parameter at +12.
		record := a.here()
		a.emit(armLdr(2, 0, 8), armAddImm(2, 2, 1), armStr(2, 0, 8), armStr(1, 0, 12), armBX(14))
		// callback(structure, other): MC_knlUnsetTimer(other).
		cancel = a.here()
		a.emit(armMovReg(0, 1))
		a.literal(3, unset)
		a.emit(armBX(3))
		// callback(structure, other): MC_knlSetTimer(other, 200, 0x77).
		rearm = a.here()
		a.emit(armMovReg(0, 1), armMovImm(1, 200), armMovImm(2, 0), armMovImm(3, 0x77))
		a.literal(12, set)
		a.emit(armBX(12))
		if err := client.core.Memory().Write(checkpointCodeBase, a.finish()); err != nil {
			t.Fatal(err)
		}
		callSlot(t, client, slotDefTimer, second, record)
		return client, first, second, cancel, rearm
	}
	fired := func(t *testing.T, client *Client, structure uint32) (count, param uint32) {
		t.Helper()
		count, err := client.readWord(structure + 8)
		if err != nil {
			t.Fatal(err)
		}
		param, err = client.readWord(structure + 12)
		if err != nil {
			t.Fatal(err)
		}
		return count, param
	}
	service := func(t *testing.T, client *Client, advance time.Duration) {
		t.Helper()
		client.clock.advance(advance)
		if err := client.serviceTimers(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("cancelled", func(t *testing.T) {
		client, first, second, cancel, _ := setup(t)
		callSlot(t, client, slotDefTimer, first, cancel)
		callSlot(t, client, slotSetTimer, first, 20, 0, second)
		callSlot(t, client, slotSetTimer, second, 40, 0, 0x55)
		service(t, client, 100*time.Millisecond)
		if count, _ := fired(t, client, second); count != 0 {
			t.Fatalf("a timer cancelled by an earlier callback fired %d times", count)
		}
		if client.timers[second].armed {
			t.Fatal("a cancelled timer is armed")
		}
	})

	t.Run("re-armed", func(t *testing.T) {
		client, first, second, _, rearm := setup(t)
		callSlot(t, client, slotDefTimer, first, rearm)
		callSlot(t, client, slotSetTimer, first, 20, 0, second)
		callSlot(t, client, slotSetTimer, second, 40, 0, 0x55)
		service(t, client, 100*time.Millisecond)
		if count, _ := fired(t, client, second); count != 0 {
			t.Fatalf("a timer re-armed for later fired %d times in the tick it was re-armed", count)
		}
		if entry := client.timers[second]; !entry.armed || entry.param != 0x77 {
			t.Fatalf("the re-armed timer is armed=%t with parameter %#x", entry.armed, entry.param)
		}
		service(t, client, 250*time.Millisecond)
		if count, param := fired(t, client, second); count != 1 || param != 0x77 {
			t.Fatalf("the re-armed timer fired %d times with %#x, want once with 0x77", count, param)
		}
		service(t, client, time.Second)
		if count, _ := fired(t, client, second); count != 1 {
			t.Fatalf("the re-armed timer fired %d times, want once", count)
		}
	})
}
