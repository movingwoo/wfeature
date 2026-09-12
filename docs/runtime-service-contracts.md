# Shared runtime service contracts

The existing backend boundaries share services across the platforms. These
contracts describe caller responsibilities without adding a runtime layer.

## Audio time

Each `backend.Audio` instance has one guest-time domain. `Play` and `Advance`
receive durations with the same origin and rate. SMAF event times are millisecond
offsets from `Play`'s timestamp; they are not wall-clock timestamps. Host speed
changes affect the progression of guest time. Audio does not apply speed again.
A new session clock needs a new timeline; a backwards timestamp does not rewind
already emitted events. Paused guest time must not advance just because the
browser disconnects or wall time passes.

KTF uses `guestElapsed()` for both playback and advancement, LGT uses
`client.clock.now()`, and SKT uses `GuestElapsed()` through `audioNow()` and
`AdvanceAudio()`. The script runtime uses its own `clock` for both operations.
Keeping those pairs together prevents a sound from being scheduled in a different
clock domain and remaining indefinitely in the future.

## Data ownership and callbacks

`Framebuffer.Present` borrows `Frame.RGBA` for the call. A framebuffer retaining
pixels must copy them before returning. A consumer must not infer ownership from
whether a particular framebuffer currently copies.

Audio sink PCM and SysEx slices are also borrowed and read-only during the call.
A sink retaining or asynchronously transmitting them must copy them. This rule
also applies at reduced volume: the implementation may allocate scaled samples,
but allocation is not an ownership transfer.

`Audio.LoadEvents` copies the outer event slice. Nested sample and SysEx slices
remain shared with the caller and must remain immutable for the loaded handle's
lifetime. Events must already be ordered by nonnegative millisecond offsets.
Closing the handle releases the timeline's reference. These are explicit caller
preconditions, not validations added by this documentation change.

Audio serializes sink callbacks under its own mutex. Stop, close, restart and
volume changes can emit callbacks as well as `Advance`. A sink must not reenter
the same Audio instance, because its mutex remains held. Serialized callbacks do
not imply that every caller is the Host's frame-loop goroutine.
