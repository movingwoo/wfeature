package lgt

import (
	"context"
	"testing"
)

// newTestDate is a Date at an instant, built through Date(long).
func newTestDate(t *testing.T, client *Client, millis int64) uint32 {
	t.Helper()
	date, err := newTestObject(t, client, javaDateClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/util/Date.<init>(J)V", date, uint32(uint64(millis)), uint32(uint64(millis)>>32))
	return date
}

// setTime moves a Date, getTime reads the move back, and equals and hashCode
// are the instant's: another Date at the same millisecond is equal, and the
// hash folds the two halves of it together.
func TestJavaDateSetTimeEqualsAndHashCode(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	date := newTestDate(t, client, 5000)
	moved := int64(7)<<32 | 9
	mustSlot(t, client, javaDateClass, 11, date, uint32(uint64(moved)), uint32(uint64(moved)>>32))
	low := mustSlot(t, client, javaDateClass, 10, date)
	high, _ := client.thread.Register(1)
	if got := int64(uint64(high)<<32 | uint64(low)); got != moved {
		t.Fatalf("getTime() after setTime(%d) = %d", moved, got)
	}
	if hash, err := client.javaHashCodeOf(ctx, client.thread, date); err != nil || hash != 9^7 {
		t.Errorf("hashCode() = %d, %v, want 14", hash, err)
	}
	if hash, err := client.javaHashCodeOf(ctx, client.thread, newTestDate(t, client, -1)); err != nil || hash != 0 {
		t.Errorf("hashCode() of -1 = %d, %v, want 0", hash, err)
	}
	long, err := newTestObject(t, client, javaLongClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Long.<init>(J)V", long, uint32(uint64(moved)), uint32(uint64(moved)>>32))
	for _, probe := range []struct {
		name  string
		other uint32
		want  bool
	}{
		{"a Date at the same instant", newTestDate(t, client, moved), true},
		{"a Date a millisecond later", newTestDate(t, client, moved+1), false},
		{"a Long of the same number", long, false},
		{"null", 0, false},
	} {
		if equal, err := client.javaEqualsOf(ctx, client.thread, date, probe.other); err != nil || equal != probe.want {
			t.Errorf("equals(%s) = %v, %v, want %v", probe.name, equal, err, probe.want)
		}
	}
	if _, err := slotCall(t, client, javaDateClass, 11, 0xdeadbeef, 0, 0); err == nil {
		t.Error("setTime on something that is not a date was accepted")
	}
}
