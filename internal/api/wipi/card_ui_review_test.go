package wipi

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The specification gives SOFT1 and SOFT2 as 90 and 91, and those are the
// numbers the platform hands a Card for the two soft keys. A title that reads
// the field at run time rather than inlining it has to see the same numbers.
func TestEventQueueSoftKeysAreTheCodesACardReceives(t *testing.T) {
	machine := newMachine(t)
	for name, want := range map[string]int32{"SOFT1": 90, "SOFT2": 91} {
		value, err := machine.StaticField(EventQueueClass, name, "I")
		if err != nil {
			t.Fatalf("EventQueue.%s error = %v", name, err)
		}
		got, err := value.Int32()
		if err != nil || got != want {
			t.Fatalf("EventQueue.%s = %d (%v), want %d", name, got, err, want)
		}
	}
}

// A Card subclass reads its protected bounds with getfield, which names a
// field by its declaring class. What the constructor stores has to be under
// that same name, and getX/getY have to agree with it.
func TestCardBoundsAreTheFieldsASubclassReads(t *testing.T) {
	machine := newMachine(t)
	card, err := machine.NewObject(CardClass, "(IIII)V",
		jvm.IntValue(1), jvm.IntValue(2), jvm.IntValue(30), jvm.IntValue(40))
	if err != nil {
		t.Fatalf("new Card(1, 2, 30, 40) error = %v", err)
	}
	for name, want := range map[string]int32{"x": 1, "y": 2, "w": 30, "h": 40} {
		value, err := machine.Field(card, CardClass, name, "I")
		if err != nil {
			t.Fatalf("getfield Card.%s error = %v", name, err)
		}
		if got, _ := value.Int32(); got != want {
			t.Fatalf("getfield Card.%s = %d, want %d", name, got, want)
		}
	}

	// A title that moves its card with putfield is answered by getX/getY.
	if err := machine.SetField(card, CardClass, "x", "I", jvm.IntValue(11)); err != nil {
		t.Fatal(err)
	}
	if got := invokeCardInt(t, machine, card, "getX"); got != 11 {
		t.Fatalf("getX() after putfield = %d, want 11", got)
	}
	if got := invokeCardInt(t, machine, card, "getY"); got != 2 {
		t.Fatalf("getY() = %d, want 2", got)
	}
}

// A quicksave written before the bounds were keyed the way getfield keys
// them holds them under the bare field name. getX/getY keep answering those
// values rather than falling to zero.
func TestCardBoundsFromAnOlderCheckpointStillAnswer(t *testing.T) {
	machine := newMachine(t)
	card := &jvm.Object{ClassName: CardClass, Fields: map[string]jvm.Value{
		"x": jvm.IntValue(7), "y": jvm.IntValue(9), "w": jvm.IntValue(10), "h": jvm.IntValue(12),
	}}
	if got := invokeCardInt(t, machine, card, "getX"); got != 7 {
		t.Fatalf("getX() on the older form = %d, want 7", got)
	}
	if got := invokeCardInt(t, machine, card, "getY"); got != 9 {
		t.Fatalf("getY() on the older form = %d, want 9", got)
	}
}

func invokeCardInt(t *testing.T, machine *jvm.VM, card *jvm.Object, method string) int32 {
	t.Helper()
	value, err := machine.InvokeVirtual(card, method, "()I")
	if err != nil {
		t.Fatalf("Card.%s() error = %v", method, err)
	}
	got, err := value.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return got
}
