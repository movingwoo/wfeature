# KTF C input and bitmap investigation

## Observations (2026-09-19)

A reported KTF archive shows a green rectangle around its startup logo and
does not expose its name editor to Host text input. Archive SHA-256:
`5f6037b8852b74254d5e66f8066c251c3a437bb0526f190233f38864b93fdef1`.
These were separate runtime limitations, not evidence of a speed-setting fault.
The implementation and validation below supersede the initial findings.

## Bitmap evidence

Before correction, the startup BMPs decoded successfully, but their `biClrImportant` fields are
`0x126` and `0x100` with 256-entry palettes and `bfReserved1 == 1`.
`bitmapTransparentIndex` treated these as out-of-range palette indices and
left the images opaque. Their low bytes select palette entries 38 and 0,
respectively; both contain RGB `#209020`. Direct decoding through the original
`wipic.DecodeBitmap` produced 2,463 and 30,536 fully opaque green pixels.
This explains why the encoded backdrop remains visible.

The decoder now accepts the `0x100` flag only for an 8-bit palette with the
existing transparency marker. The low-byte index must still fit the actual
palette. Other high bits remain invalid. Authored fixtures cover the extended
variant, ordinary transparency, and out-of-range indices.

There is also a separate image-load failure. One UI BMP declares 256 used
palette entries but its pixel offset leaves room for only 43. Its important
colour value is `0x113`, whose low byte selects the same green. The original
decoder returned `bmp: unsupported BMP image`. For this marked, uncompressed
8-bit variant with a 40-byte info header, a copied header now derives the palette
count from the checked pixel offset. The underlying decoder still validates
pixel indices and row lengths. Unmarked files do not receive this repair.

## Input evidence

The session report records input-method table 4 calls to entries 0, 1, and 2
as stubs. At the time of the report, the Host adapter in
`runtime_lwc_host_input.go` supported Java LWC/KFC objects, not this game-owned
WIPI-C editor.

Disassembly of the actual caller identifies the previously unnamed entries:

- At `0x124b08`, entry 1 is called with mode 3, after obtaining the mode count
  and list. This is `MC_imSetCurrentMode`.
- At `0x124b10`, entry 2 takes no arguments and its return replaces the stored
  mode. This is `MC_imGetCurrentMode`.
- At `0x124ffe`, entry 0 receives `0x9d` (the flush character), a type, two
  buffers and two size pointers. This is `MC_imHandleInput`.

The [WIPI graphics/input-method specification](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
defines these signatures; table ordering is established by the caller.
The original implementation returned zero for all three entries. In particular,
setting numeric mode failed and reading the current mode returned zero. All
three entries now implement mode state or completed/composing output buffers.

## Implemented input boundary

The Host adapter appends completed EUC-KR text through the guest card's existing
numeric-key callback. The C widget continues to own its text, cursor, display,
confirmation and field policy. No game-memory offsets or archive-specific
patches are used. The existing Java Shell and KFC adapters retain precedence.

The observed C input callback uses event type 2 for a pressed key, distinct
from the Java card event constants. Its first four arguments are in registers;
the composing buffer and size pointer are on the stack. The caller initializes
capacities of 6 and 8 bytes and then reads the size words as produced byte
lengths. Both output strings are terminated and both lengths are written.
A flush invalidates old snapshots but does not by itself close the editor.
CLR that flushes composition retains input availability. Numeric keypad input
is supported; other
handset keypad composition remains outside this change because the Host IME
provides completed text.

The keypad mode selects an automaton, not a field constraint. The observed name
editor explicitly selects mode 3 yet labels the field for Korean text. The
Host therefore validates controls, encoding and length without treating that
keypad mode as a digits-only field policy. It does not reorder the published
mode list to suit one caller.

The completion buffer holds an automaton result, not the whole field. A Host
submission is validated for encoding and controls before any guest callback,
then delivered one complete EUC-KR character at a time. Each character and its
terminator must fit the completion buffer. Up to 64 characters may be submitted;
the guest continues to apply its own field limit on each callback. The reported
five-character field accepts five Korean characters in one Host submission.

A character that cannot fit is rejected without splitting its encoding. A
changed target or interrupted callback stops further delivery and invalidates
a snapshot that already inserted a prefix. Previously accepted characters remain
owned by the guest; this C interface cannot promise whole-field rollback after
a mid-submission guest failure. It must not replay the prefix through the same
edit token. No game-specific field size or guest-memory offset is hard-coded.

Key activity, mode changes, flushing, card replacement, a newly focused Java
editor, shutdown and successful submission invalidate existing snapshots. A
callback that no longer routes to the C input method rejects the edit. Queued
guest event loops remain unsupported: commits require consumption before
returning. The later [timer-delivery correction](ktf-graphics-input-qa-2026-09-23.md)
also supports cards that defer keys and CLR flushing to their owning C timer.

## Validation

Authored regressions cover extended transparency, compact palettes, source-byte
preservation, malformed palette offsets and pixel indices, truncated rows, mode
state, buffer lengths and termination, pressed-key handling, Korean Host input,
single-character capacity rejection and retry, unsupported characters, stale
targets, cancellation and stopping a composition when the guest changes mode. Existing Java editor tests remain in the affected package gate.

The initial isolated-save replay proved only two-character insertion. User
feedback exposed two missed requirements: the field accepts five characters,
and CLR deletion must not disable input. Those cases now have authored
regressions and a fresh real-archive replay.

The final replay submitted five Korean characters at once, deleted one with
CLR, appended a replacement, cleared all five with CLR, and submitted five again.
Captured frames and retained EUC-KR guest strings establish the resulting field
value. The guest's existing confirmation buttons then continued into the story.
Old snapshots were rejected after deletion and dismissal. Both startup logos
rendered without the green backdrop, with no image-decode errors on that route.
This does not establish whole-game rendering or saved-name restoration.

Artifacts and the opt-in replay probe are retained under the ignored
`var/diagnostics/ktf-c-input-logo-20260919/`. Physical iPhone IME interaction was
not repeated; the replay calls the same platform text provider used by the
existing browser transport. No session protocol or page code changes are needed.

The final code passed `make test` (including Node tests), `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and `make server`.
