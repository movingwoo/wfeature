package lgt

import (
	"math"
	"testing"
)

// Math.abs(long) answers in the register pair a long comes back in, and the
// one long with no positive twin stays itself.
func TestJavaMathAbsLong(t *testing.T) {
	client := fixtureClient(t)
	for _, probe := range []struct{ value, want int64 }{
		{-5, 5}, {5, 5}, {0, 0}, {-1 << 32, 1 << 32}, {-(1<<40 + 3), 1<<40 + 3},
		{math.MinInt64, math.MinInt64}, {math.MaxInt64, math.MaxInt64},
	} {
		low := mustJavaCall(t, client, "java/lang/Math.abs(J)J", uint32(uint64(probe.value)), uint32(uint64(probe.value)>>32))
		high, err := client.thread.Register(1)
		if err != nil {
			t.Fatal(err)
		}
		if got := int64(uint64(high)<<32 | uint64(low)); got != probe.want {
			t.Errorf("abs(%d) = %d, want %d", probe.value, got, probe.want)
		}
	}
}
