# SGS pseudo-random services

The original runtime uses a 32-bit state recurrence:
`state = (214013 * state + 2531011) mod 2^32`. Each draw returns
`(state >> 16) & 32767`. The Go implementation expresses this arithmetic
independently and stores the state per session; no runtime code is bundled.

Service `0xa0` sign-extends its signed 16-bit seed into the 32-bit state.
Service `0xa1` sorts its two signed bounds and uses the draw modulo their
positive difference, then adds the lower bound. Equal bounds return that bound
without drawing. Consequently an interval wider than 32768 does not cover its
entire upper portion. Replacing this with a uniform host sampler changes the
observed contract. Service `0xa2` always draws, including for percentages outside
0..100, and compares the draw modulo 100 with the signed percentage.

The original handlers are at `0x41b180`, `0x41b1a0` and `0x41b1f0`; the state
update is at `0x44cb52`. The thread-state initializer at `0x4507c0` sets its
initial state to 1. Each emulator session starts with that state so another
session cannot perturb its sequence. This intentionally does not share the
original process/thread storage across independent browser games.

On 2026-09-12 an isolated x86 interpreter executed the original service handlers
for seven signed seeds and 28 mixed operations per seed. A thread-state lookup
hook supplied private storage; the seed and draw routines themselves ran from
the original runtime. All 196 results and final states matched the arithmetic
above, reached the return sentinel within 1000 instructions per call, and
preserved the caller stack. The authored regression repeats these sequences
through Go service dispatch, covering negative seeds, full-word bounds, reversed
bounds, equal-bound non-consumption, and unconditional percentage draws.

These vectors establish the explicitly seeded service contract. They do not
establish that the standalone player's other host actions never draw from or
reseed its thread state before launching a script. Real-title routes must be
checked separately when the generator changes; matching a framebuffer from the
previous substitute sequence is not the acceptance criterion.
