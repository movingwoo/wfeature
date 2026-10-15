# KTF C input and bitmap investigation

## Observations (2026-09-19)

A reported KTF archive (SHA-256
`5f6037b8852b74254d5e66f8066c251c3a437bb0526f190233f38864b93fdef1`) showed a
green rectangle around its startup logo and did not expose its name editor to
Host text input. These were two separate runtime limitations, not a
speed-setting fault. The implementation and validation below supersede the
initial findings.

## Bitmap evidence

The startup BMPs decoded successfully, but their `biClrImportant` fields are
`0x126` and `0x100`, with 256-entry palettes and `bfReserved1 == 1`.
`bitmapTransparentIndex` treated these as out-of-range palette indices and
left the images opaque. Their low bytes select palette entries 38 and 0, both
RGB `#209020`; direct decoding through the original `wipic.DecodeBitmap`
produced 2,463 and 30,536 fully opaque green pixels, which is the visible
backdrop.

The decoder now accepts the `0x100` flag only for an 8-bit palette with the
existing transparency marker, and the low-byte index must still fit the actual
palette. Other high bits remain invalid. Authored fixtures cover the extended
variant, ordinary transparency, and out-of-range indices.

A separate image-load failure: one UI BMP declares 256 used palette entries
but its pixel offset leaves room for only 43. Its important-colour value
`0x113` selects the same green. The original decoder returned
`bmp: unsupported BMP image`. For this marked, uncompressed 8-bit variant with
a 40-byte info header, a copied header now derives the palette count from the
checked pixel offset; the underlying decoder still validates pixel indices and
row lengths. Unmarked files do not receive this repair.

## Input evidence

The session report records input-method table 4 calls to entries 0, 1, and 2
as stubs. The Host adapter in `runtime_lwc_host_input.go` then supported Java
LWC/KFC objects, not this game-owned WIPI-C editor. Disassembly of the caller
identifies the entries:

- At `0x124b08`, entry 1 is called with mode 3, after obtaining the mode count
  and list: `MC_imSetCurrentMode`.
- At `0x124b10`, entry 2 takes no arguments and its return replaces the stored
  mode: `MC_imGetCurrentMode`.
- At `0x124ffe`, entry 0 receives `0x9d` (the flush character), a type, two
  buffers and two size pointers: `MC_imHandleInput`.

The [WIPI graphics/input-method specification](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
defines these signatures; the caller establishes the table order. All three
entries returned zero, so setting numeric mode failed and reading the current
mode returned zero. They now implement mode state and completed/composing
output buffers.

## Implemented input boundary

The Host adapter appends completed EUC-KR text through the guest card's
existing numeric-key callback; the C widget keeps its text, cursor, display,
confirmation and field policy. No game-memory offsets or archive-specific
patches are used, and the Java Shell and KFC adapters retain precedence. The
current contract is in [native text input](native-text-input.md#supported-editors);
the findings specific to this caller were:

- The observed C input callback uses event type 2 for a pressed key, distinct
  from the Java card event constants. Its first four arguments are in
  registers; the composing buffer and size pointer are on the stack.
- The caller initializes capacities of 6 and 8 bytes and then reads the size
  words as produced byte lengths. Both output strings are terminated and both
  lengths written.
- The keypad mode selects an automaton, not a field constraint: the name editor
  selects mode 3 yet labels the field for Korean text. The Host therefore
  validates controls, encoding and length without treating that mode as a
  digits-only policy, and does not reorder the published mode list.
- The completion buffer holds an automaton result, not the whole field. A Host
  submission (up to 64 characters) is validated before any guest callback,
  then delivered one complete EUC-KR character at a time; each character and
  its terminator must fit the buffer, and a character that cannot fit is
  rejected without splitting its encoding. The guest applies its own field
  limit on each callback; the reported five-character field accepts five
  Korean characters in one submission.
- Numeric keypad input is supported; other handset keypad composition is out
  of scope because the Host IME supplies completed text.
- A flush invalidates old snapshots but does not close the editor; CLR that
  flushes composition retains availability. Key activity, mode changes,
  flushing, card replacement, a newly focused Java editor, shutdown and
  successful submission invalidate snapshots, and a callback that no longer
  routes to the C input method rejects the edit.
- A changed target or interrupted callback stops delivery and invalidates a
  snapshot that already inserted a prefix. Accepted characters remain the
  guest's; this C interface cannot promise whole-field rollback after a
  mid-submission guest failure, and the prefix is never replayed through the
  same edit token.

Queued guest event loops remained unsupported: commits require consumption
before returning. The later [timer-delivery correction](ktf-graphics-input-qa-2026-09-23.md)
added cards that defer keys and CLR flushing to their owning C timer.

## Validation

Authored regressions cover extended transparency, compact palettes,
source-byte preservation, malformed palette offsets and pixel indices,
truncated rows, mode state, buffer lengths and termination, pressed-key
handling, Korean Host input, single-character capacity rejection and retry,
unsupported characters, stale targets, cancellation and stopping a
composition when the guest changes mode. Existing Java editor tests remain in
the affected package gate.

The initial isolated-save replay proved only two-character insertion. User
feedback exposed two missed requirements (the field accepts five characters,
and CLR deletion must not disable input), which then gained authored
regressions and a fresh real-archive replay. That replay submitted five
Korean characters at once, deleted one with CLR, appended a replacement,
cleared all five with CLR, and submitted five again. Captured frames and
retained EUC-KR guest strings establish the field value, and the guest's
confirmation buttons continued into the story. Old snapshots were rejected
after deletion and dismissal. Both startup logos rendered without the green
backdrop, with no image-decode errors on that route. This does not establish
whole-game rendering or saved-name restoration.

Artifacts and the opt-in replay probe are under the ignored
`var/diagnostics/ktf-c-input-logo-20260919/`. Physical iPhone IME interaction
was not repeated; the replay calls the same platform text provider as the
browser transport, and no session protocol or page change was needed. The
final code passed `make test` (including Node tests), `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and `make server`.
