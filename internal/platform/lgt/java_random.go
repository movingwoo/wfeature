package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `Random.nextInt(int)`: a number from zero up to a bound, which CLDC 1.1 adds
// to the class and the WIPI 1.2.1 page does not list.
//
// It takes slot 13 by the declaration-order rule — setSeed, next, nextInt,
// nextInt(int) — one past the nextInt anchor at 12, and it is CLDC 1.1's
// algorithm run on the same generator nextInt() draws from (random.go): a
// power-of-two bound takes the top bits of one draw, and any other bound draws
// again while the draw falls in the last, incomplete run of the bound, so every
// answer is equally likely. Where the class library's `next(bits)` answers the
// top bits of its state, this generator's draws are 63 bits wide and
// `next(31)` is their top 31 — the same reading nextInt() makes of them, whose
// answer is their top 32 (rand.Rand.Uint32).
//
// The generator is this platform's (random.go) rather than the class library's
// linear congruential one, for this method as for nextInt(): a title gets the
// distribution it asked for and a seeded sequence that repeats, not the exact
// numbers a handset would draw.

// maxJavaRandomDraws bounds how many draws one bounded nextInt may take. A draw
// is rejected less than half the time, so this is never reached; it is here so
// that no loop the platform runs on the guest's behalf is unbounded.
const maxJavaRandomDraws = 1024

// javaRandomNextBounded is `nextInt(int)`, slot 13. A bound that is not
// positive is the IllegalArgumentException the class library throws.
func javaRandomNextBounded(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	bound := int32(arguments[1])
	if bound <= 0 {
		return 0, client.throwJavaPlatform(thread, javaIllegalArgumentClass, ": n must be positive")
	}
	source, ok := client.javaRuntimeState().random[arguments[0]]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a generator this platform built", arguments[0])
	}
	next31 := func() int32 { return int32(uint64(source.Int63()) >> 32) }
	if bound&-bound == bound {
		return uint32(int32(int64(bound) * int64(next31()) >> 31)), nil
	}
	for range maxJavaRandomDraws {
		bits := next31()
		value := bits % bound
		// The class library's own test, in its own 32-bit arithmetic: a draw
		// from the last run of the bound overflows here and is drawn again.
		if bits-value+(bound-1) >= 0 {
			return uint32(value), nil
		}
	}
	return 0, fmt.Errorf("nextInt(%d) drew %d times without an answer", bound, maxJavaRandomDraws)
}
