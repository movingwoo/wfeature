package lgt

import (
	"testing"
)

// nextInt(int) is CLDC 1.1's algorithm on the generator nextInt() draws from:
// a seeded generator answers what the algorithm makes of the same draws, every
// answer is inside the bound, and nextInt() and nextInt(int) take turns on one
// stream of draws.
func TestJavaRandomNextIntBounded(t *testing.T) {
	client := fixtureClient(t)
	random, err := newTestObject(t, client, "java/util/Random")
	if err != nil {
		t.Fatal(err)
	}
	seed := int64(0x1234_5678_9abc)
	mustJavaCall(t, client, "java/util/Random.<init>(J)V", random, uint32(seed), uint32(seed>>32))
	reference := newGuestRandom(seed)
	next31 := func() int32 { return int32(uint64(reference.Int63()) >> 32) }
	bounded := func(bound int32) int32 {
		if bound&-bound == bound {
			return int32(int64(bound) * int64(next31()) >> 31)
		}
		for {
			bits := next31()
			if value := bits % bound; bits-value+(bound-1) >= 0 {
				return value
			}
		}
	}
	for round, bound := range []int32{16, 10, 1, 1 << 30, 7, 0x7fffffff, 2, 1000} {
		got := int32(mustSlot(t, client, "java/util/Random", 13, random, uint32(bound)))
		if want := bounded(bound); got != want {
			t.Errorf("round %d: nextInt(%d) = %d, want %d", round, bound, got, want)
		}
		if got < 0 || got >= bound {
			t.Errorf("nextInt(%d) = %d, outside the bound", bound, got)
		}
		// nextInt() is the top 32 bits of the next draw.
		if got, want := mustSlot(t, client, "java/util/Random", 12, random), uint32(reference.Int63()>>31); got != want {
			t.Errorf("round %d: nextInt() = %#x, want %#x", round, got, want)
		}
	}
	for _, bound := range []uint32{0, 0xffffffff, 0x80000000} {
		if _, err := slotCall(t, client, "java/util/Random", 13, random, bound); thrownClass(err) != javaIllegalArgumentClass {
			t.Errorf("nextInt(%d) = %v, want IllegalArgumentException", int32(bound), err)
		}
	}
}
