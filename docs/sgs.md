# SKT SGS execution

SKT archives can contain GNEX/GVM SGS bytecode instead of Java classes. These
run on a separate, pure Go execution path and keep the `skt` platform
identifier in the library, CLI and session protocol. The user's SGS support
request on 2026-09-11 superseded the earlier decision to exclude this runtime.

## Current acceptance status

The initial SGS execution path and the reported QA fixes passed their
checkpoint. Further compatibility work is paused, and the user authorized a PR
of the implementation as it stood: a partial-support delivery.
[The remaining acceptance gates](sgs-completion.md) record unfinished
full-compatibility work. After the final fixes, the user confirmed that the
previously questioned archive runs, the reported text corruption is gone, and
their testing shows no further problems. This closes the reported QA issues; it
does not certify every SGS service or gameplay route. The audit checkpoints
below keep earlier failures as diagnostic history, not as the latest user
acceptance result.

External URL launch (`0xc4`) remains unsupported. The later full-implementation
work added bitmap queue services; see the [opcode inventory](sgs-opcodes.md).
Directed save/audio acceptance and unexercised gameplay paths are follow-up
work. An archive running successfully does not imply that its optional
external-download route is implemented.

## Composition and packaging

`internal/platform/detect` recognizes a matching `.SGS` and `.mod`/`.inf`
pair. The MOD descriptor declares `application/x-gnex-sgs` and `SGS` as
length-prefixed strings without terminators. INF descriptors identify `GVM` in
their bounded final fields. Names and extensions are case-insensitive,
including wrapped directories; an extension alone is insufficient. Descriptor
reads are bounded to 4 KiB and sixteen candidates. Existing carrier and Java
markers keep precedence.

`internal/platform/skt` opens the archive and composes `internal/sgsvm` with
the existing framebuffer, audio, save and logging boundaries. The server's
shared session and the CLI `runskt` command both call `skt.StartScript`.
`inspect` reports SGS metadata without requiring a Java main class. JVM
invocation and cheats remain Java-specific. Quick save and quick load cover SGS
sessions; see [SKT checkpoints](skt-checkpoints.md#sgs-state).

The parser accepts version 1 (52-byte header), version 2 (100-byte header), and
the observed 32-byte download prefix. Internal offsets stay relative to the SGS
header. It validates callbacks, variable and resource tables, initializer
spans, descriptor counts and the original 16 KiB initial allocation budget.
Input is bounded to 128 KiB plus the optional prefix. No game archives or
original runtime binaries are distributed with this implementation.

## Execution and events

Values are signed 16-bit integers. Bytecode words and branch targets are big
endian; header fields, initializer words and saved words are little endian.
The VM implements indexed/scalar loads and stores, arithmetic and comparisons,
assignment shortcuts, branches, calls, encoded word references and vector
arithmetic. The absolute-value service (`0xa3`, original handler `0x41b220`)
keeps signed-word overflow: the absolute value of -32768 remains -32768.
Constants and mutable values belong to each VM instance, including address
references into initializer data. The operand stack holds at most 65 values
and the return stack at most 17 addresses.

Each callback has a bounded work budget, including vector and raster
operations, and checks context cancellation. Unknown operations fail with
their opcode and instruction offset instead of silently succeeding. Guest code
cannot create host threads or run an unlimited loop.

The eight header callbacks are initialization, termination, timer, key, two
message callbacks and two system callbacks. The host drives initialization,
termination, timers and key activation. Variables 0–15 include the event
parameter, screen dimensions, timer counters and runtime version. Three timers
share a virtual clock. Pausing freezes this clock; resuming resets the
wall-clock origin. The CLI advances the same clock in 16 ms diagnostic steps.
Timers do not replay an unlimited backlog after a delayed host tick.

SGS receives one activation per key press, with no second event on release.
Numeric keys, star, hash, directions, fire and function keys map to the script
runtime's key codes. Pointer events and cheats remain unavailable.

## Graphics, sound and storage

The framebuffer is indexed RGB332 and is presented only when the script
flushes it. Implemented drawing includes pixel/line/rectangle/ellipse
operations, bitmap types 2–8, horizontal mirroring, packed palette construction
and palette overrides (`0x6e`, `0x71`, `0x72`), transparency, backup/restore,
text and seven palette banks. Packed bitmap rows form a continuous MSB-first
bitstream. Palette cubes, accent ramps and bank transforms are generated
mathematically; no palette table was copied from a runtime. Ellipse
rasterization follows the geometric contract and can differ by an edge pixel
from the original midpoint rasterizer.

Text uses the bundled fonts, with fixed byte-cell layout and EUC-KR decoding.
The ordinary 12-pixel style preserves the handset face's native strokes and
shared baseline, and the large style doubles that grid exactly; glyph ink
bounds are not stretched to fill the advance cell. The small styles are
Latin-only, matching the original runtime; the smallest 3×5 ink grid uses
independently authored compact glyphs to preserve whole strokes. Background
drawing includes the original one-pixel top/left border, and empty strings draw
nothing. These faces remain substitutes; no additional third-party font is
bundled.

The default screen is 128×160. When the header excludes class 4 but accepts
class 8, it is 176×176, the minimum satisfying the larger class. Explicit host
dimensions take precedence. The header's LCD class mask does not establish
exact dimensions, so these defaults are compatibility choices, not decoded
handset sizes.

Embedded audio with the observed two-byte wrapper goes through the existing
SMAF decoder and audio timeline. Sound stop and device-effect state use the
shared backend services. Unsupported audio content returns a decoding error.
The original runtime's no-op device hooks consume their documented arguments.
The mobile identifier query returns an empty value, matching the original
runtime's unconfigured default; it does not invent a subscriber identity.

Saves live under the regular SKT save tree with an `sgs-<digest>` owner and
`nv/data` key. The executable digest separates archives with reused carrier
descriptors. Both profiles use the same layout. Save words are little endian;
missing data reads as zeros. Short writes have a deterministic 64-byte minimum.
A handset package includes a 66-byte save and requests 33 words, so larger
bounded writes are preserved; this deliberately differs from the PC player's
unsafe fixed 64-byte write buffer. Tests cover the 33rd word and a fresh
session loading a previous session's save.

## Evidence and current limits

The loader, opcode and service contracts were established by inspecting the
original runtime bundled with local archives, then independently implemented.
No original implementation code, binary, font or palette asset was copied. The
interpreter dispatch is at original address `0x41e3b0` with table `0x494a68`;
the previously reported 279-pointer table was three adjacent vector tables
followed by the opcode table. Header/data initialization is at `0x41dfd0`. The
WIPI standard does not cover this private SGS instruction set.

All twelve SGS archives in the local corpus pass structural parsing and are
recognized as SKT packages. The two archives the user reported reach their
title screens and accept input; one reaches difficulty selection and in-game
help, and the other reaches character selection and a live fight. These checks
are evidence for those routes, not a claim that every SGS API or game is
complete.

The follow-up on 2026-09-11 implements the bounded formatting family
`0x8a`–`0x8e` (one through five signed-word arguments). It preserves EUC-KR
bytes, decimal, hexadecimal and character conversions, flags, width and a
255-byte output limit plus NUL. Invalid argument counts fail before changing
the target. The original handlers start at `0x41a430`, `0x41a9c0`, `0x41aaa0`
and `0x41ab80`, using the formatter at `0x41a500`. Hexadecimal uses a
sign-extended 32-bit value. Precision uses bounded conventional
integer-format semantics: the original precision parser appears not to advance
past its dot, so reproducing that apparent loop would be unsafe. Formatting
also accepts bounded input without a NUL, treating the resource boundary as its
end; string length/copy operations require a terminator.

Resource resizing (`0x7a`), zeroing allocation (`0x7b`), string length
(`0x7c`) and copying (`0x7d`) share the allocator behavior observed at
`0x4197c0`: growth has an even delta, and shrinking retains up to sixteen spare
bytes. Formatting uses the same allocation path. Existing bytes survive
resizing; newly exposed bytes are deterministically zeroed. Allocation failure
returns zero to the status-bearing operation, and unterminated length/copy
strings or invalid lengths fail before mutation. Host bounds prevent the
original 16-bit size overflow.

The previously blocked introductory route now reaches its live playfield after
skipping the tutorial. A second fighting archive reaches a live fight after the
palette construction/override operations were added. Its local failure was
`0x6e` at `0x093e`; the reported `0x6a` failure was not reproduced. Text
operation `0x6a` keeps its `(x, y, resource)` contract, with invalid-resource
tests. Original text layout is at `0x40cce0`, and palette handlers at
`0x419310`, `0x419550` and `0x419600`.

Network/message callbacks, original overlay behavior, pointer input, full
handset font fidelity and end-to-end completion of every archive remain
unverified or unimplemented. Passing these opening and input routes does not
establish complete GNEX compatibility.

After this follow-up, ten of twelve local SGS archives completed the
1,200-tick opening probe (19.2 seconds of manual guest time). Two stopped: one
at the substring service `0x7e` (`0x0292`), the other with a truncated bitmap
passed to `0x70` (`0x067b`). The latter is an unresolved resource-construction
path, not evidence that an empty bitmap should silently draw nothing. Longer
gameplay, audible sound and save/load routes across the corpus remain to be
verified.

## Validation

Fixtures are newly authored bytecode, variable tables, bitmap data and ZIP
containers assembled in tests. They cover parser bounds, instance ownership,
operand and return stacks, control flow, cancellation and work limits, vector
arithmetic, bitmap packing, clipping and palette banks, text/ellipse drawing,
key/timer lifecycle and save round trips. Real archives remain only under the
ignored game tree; captures and diagnostic scripts remain under `build/`.

Both debug and release CLI builds execute a local SGS archive for 1,200 manual
ticks and accept the existing `-serve` step/key/quit protocol. The browser
checks use an isolated library and save directory, exercise the real WebSocket
client, and confirm that an SGS session can stop and launch another SGS archive
or an authored Java control archive on the same server. These checks do not
establish audible sound fidelity or long-term gameplay/save compatibility for
every archive.

The follow-up passes `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...`, and builds both profiles.
Both reported routes also pass debug/release CLI `-serve` step/key/shot/quit
checks. Chromium reaches the live cleaning playfield and the second fighting
archive's fight through the PWA's keyboard/WebSocket path, then stops and
launches the authored Java control archive without restarting the server.

## Extended corpus audit (2026-09-11)

The later audit extends the opening probe with twenty key activations, 150
manual ticks between keys, 7,500 further ticks, termination and a new session
using the same isolated save directory. A successful first session runs 11,700
ticks (187.2 seconds); the reopened session runs another 1,200 ticks. The keys
are a diagnostic sequence, not an authored completion route for each archive.
Existing user saves and the user's running server are not touched.

Five archives complete that sequence without a runtime, close or reopen error:
IDs 01, 02, 03, 04 and 08. Their final screen is not necessarily gameplay: 03
returns to a title menu and 08 reaches options. Separate directed menu routes
reach live playfields for 01 and 08; the previously reported paths for 02–04
have separate gameplay evidence above. Reopening a save directory does not
prove that a game wrote progress or restored a particular in-game state.
Audible sound, completion and longer play are outside this check.

Seven archives stop. IDs refer to the ignored local report, and digest prefixes
identify the exact archive independently of its filename.

| ID | SHA-256 prefix | Phase | First error |
| --- | --- | --- | --- |
| 05 | `192cbe77da2a` | fourth key / following ticks | `0x81` at `0x00a9`: resource byte offset out of bounds |
| 06 | `8d6f2315f6b4` | opening tick 221 | `0x7e` at `0x0292`: unsupported service |
| 07 | `aa8b15a034ee` | fifth key / following ticks | `0x7e` at `0x3b79`: unsupported service |
| 09 | `e1b2a614fb1e` | opening tick 156 | `0x70` at `0x067b`: truncated bitmap |
| 10 | `3200793549ca` | third key | `0x81` at `0x5a4b`: resource byte offset out of bounds |
| 11 | `b7c4cf261fd1` | third key | `0xc4` at `0x13cd`: unsupported service |
| 12 | `701358f8c4f0` | first key | `0xad` at `0x083d`: unsupported service |

The original `0x81` handler at `0x419d00` confirms the implemented
resource/index argument order and signed-byte result. Traces narrow both
boundary errors: 05 reads offset 2 of immutable resource 160 (length 2), and 10
reads offset 1042 of immutable resource 798 (length 1042). Both resources keep
their initial sizes; no resize corruption was observed. The original loader at
`0x41e195`–`0x41e1a0` places immutable banks contiguously, so those reads reach
the first byte of the following bank (`0x05` and `0x40`, respectively).
Independent Go slices do not preserve that guest layout. Compatibility requires
bounded access to the guest's shared resource region, not removal of host
bounds.

For 09, bitmap resource 343 contains only `05 00`, declaring type 5 and zero
width. The original mirror handler reads across that short header. Zero-width
rendering may be a no-op, but the renderer's exact early-exit behavior still
needs confirmation before changing validation. Read-only diagnostic evidence
is in `build/sgs-resource-diagnosis.log`.

Local evidence is under `var/acceptance/sgs-audit-2026-09-11/`:
`results.json`, `archives.json`, and opening/input/sustained/targeted screen
captures. The probe source is `build/sgs_audit.go`; directed menu probes are
also under `build/`. This audit adds evidence and follow-up work; it does not
change the emulator binary under user test.

## Resource and service follow-up (2026-09-11)

The next implementation checkpoint addresses the audit's concrete execution
blockers without changing the session protocol or save format:

- `0x81` reads within instance-owned guest resource regions. Original
  immutable resources share one copied module region; mutable resources form a
  packed region in descriptor order, reflecting their current allocation sizes.
  Cross-descriptor reads remain bounded by the appropriate guest region.
  Mutable scans charge work per descriptor. Detached immutable allocations keep
  their own bounds, and zero-length immutable descriptors conservatively reject
  cross-descriptor byte reads until their provenance is represented.
- `0x7e` copies a bounded substring and pads after an early NUL; `0x80`
  compares strings as unsigned bytes; `0x83` converts a decimal prefix to a
  signed word. Inputs are validated before destination mutation, including
  self-copy.
- `0x86` writes a byte through a word address, preserving the neighboring byte.
  It shares bank and offset validation with the existing unsigned `0x85` read.
- `0xad` returns the signed maximum of two words; `0xb2` finds the earliest
  minimum element's index within a bounded word span.
- Valid bitmap kinds with zero width or height draw nothing, including short
  zero-width descriptors. Nonempty truncated images still fail validation.
- Graphics work accounting distinguishes a pixel from a whole image.
  Pixel-by-pixel full-screen drawing no longer consumes the budget as if each
  pixel copied an entire frame. Infinite loops, excessive work and cancellation
  remain bounded and tested.

The original evidence includes handlers `0x419ac0` (substring), `0x419c60`
(comparison), `0x419db0` (decimal conversion), `0x419ea0`/`0x419ee0` (byte
memory), `0x41b630` (maximum), `0x41b7e0` (minimum index), and the zero-width
clipping exits at `0x40f60f` and `0x40fc58`. Resource-region ownership is
tested across separate VM instances, immutable/mutable boundaries and resizing.

Repeating the extended audit now passes eleven of twelve archives through
11,700 ticks, twenty activations, close and 1,200 reopened ticks. This is an
execution checkpoint, not game completion or proof of game-specific saves. The
remaining route invokes `0xc4`, confirmed at `0x41c750` as an external URL
request that transfers control back to the host. The captured screen offers a
download, and its resource contains an HTTP download URL. The host reports that
external URL launch is unsupported; it neither opens the address nor ignores
the request and continues the callback.

Current results and captures are in
`var/acceptance/sgs-audit-2026-09-11-next/`. Baseline counters remain in the
first report's `results.json` and `build/sgs-audit.log`; use the newer
directory for current screen captures. Remaining work includes directed
play/save/audio routes, later unexercised services, and the external-download
path.

This checkpoint passes `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and both profile builds. The
six newly unblocked archives (05, 06, 07, 09, 10, 12) also pass the same
extended key sequence through debug and release CLI `-serve`, including the
sustained 7,500-tick phase. The tests preserve existing saves by using isolated
save roots. Real user progress and audible output still require separate
acceptance routes.

Chromium also verifies the previously blocked pixel-drawing archive's in-game
menu and the string-service archive's introductory dialogue through page
input. Both keep delivering frames without protocol errors, and the same server
then launches the authored Java control archive. These browser checks do not
claim completion of either game's introductory or play routes.

## User-input QA follow-up (2026-09-11)

The reports captured at 12:46–12:49 UTC expose paths the earlier generic key
sequence did not reach; eleven generic probes passing does not mean eleven
complete, playable titles. This round extracts each session's key
press/release sequence and relative timestamps from the page log and replays it
with isolated saves, both empty and copied from the user's save tree. A replay
never modifies original saves or the running user server.

The reproducible missing services are `0x84` at `0x1429`, `0xb7` at `0x33df`,
`0xae` at `0x347b`, and `0x7f` at `0x0dee`. They are now implemented as signed
integer-to-string conversion, strict center/radius proximity, maximum of three
signed words, and NUL-terminated concatenation. Original handlers are
`0x419e00`, `0x41c1a0`, `0x41b670` and `0x419b70`. Concatenation snapshots
aliased inputs before allocation; proximity uses widened arithmetic and
excludes both endpoints. Authored tests cover overflow, sign, ties, aliasing
and invalid input. The recorded routes for IDs 06–09 reproduce their former
missing-service errors before the dispatch changes and pass afterward with both
save setups.

The color-198 report is timing-sensitive. ID 10's immediate replay passes, but
an extra 500 or 1,000 ms of guest time before its first recorded key
reproduces `0x6f` at `0x54c7`. Resource 735 originally contains palette index
166; the guest explicitly writes literal 198 at byte 17 through `0x82` at
`0x5f67`. This is neither a corrupt ZIP nor a decimal conversion failure. The
original decoder reads beyond its 182-entry color map into bitmap-queue
scratch state for this index. Byte-palette bitmap kinds 5–7 now model indices
182–255 with a bounded, instance-owned 74-byte scratch tail. These bytes are
already RGB332 and bypass palette-bank conversion. The original queue count is
at `0x526a8a` and its entries begin at `0x526a90`. At this QA checkpoint the
queue services were unsupported, so scratch remained zero initially. The later
queue implementation updates count and coordinate bytes; reads aliasing native
pointer bytes fail explicitly because host addresses are not guest data.
Direct-color kind 8 and ordinary drawing colors keep their existing bounds.
Authored tests cover scratch indexing, transparency, palette-bank independence
and those bounds. The copied-save replay with a 500 ms lead now passes the
former failure and the five-second observation after its last recorded key.

No replacement-character or private-use decoding was observed in text calls
along the replayed routes, and the page log contains no image-decoding
exception. Chromium nevertheless captured fragmented strokes beside the intact
Korean HUD name in ID 10 (`build/sgs-user-browser-before.png`), also present
in the copied-save replay's final framebuffer. The text tracer identifies the
affected HUD labels as small Latin text. This visual evidence is independent of
decoder errors and does not establish the cause of every reported Korean
defect. Authored compact Latin glyphs now replace the destructive 5×7-to-3×5
sampling; stroke tests and the same captured route verify readable labels.

ID 10 also declares LCD mask `0x08` and draws its HUD through row 174, which
the old 128×160 default clipped. Original service `0x51` at `0x418980` defines
class 8 as width and height both at least 176. A surviving header validator
tests the class-1 bit at `0x409c6d`; higher-class validation was removed from
the available runtime. Matching the mask to that class contract informs the
176×176 default rather than claiming an exact resolution field. Parser and
session tests cover selection, framebuffer dimensions and explicit overrides.
The final Chromium replay starts at 176×176, shows the previously clipped
level/health HUD and readable compact labels, and then starts the Java control
without page or session errors (`build/sgs-user-browser-1.png`).

Chromium replays the recorded inputs for IDs 10 and 09, keeps receiving frames
without session or page errors, and launches the authored Java control on the
same server afterward. This verifies input and session reuse, not visual
fidelity or complete gameplay.

Local inputs are `build/sgs-user-routes.json`; the replay and text/resource
tracer are `build/sgs_user_replay.go`. Results/captures are under
`var/acceptance/sgs-user-replay-2026-09-11*`, with console evidence in
`build/sgs-user-replay*.log` and the palette timing/mutation logs under
`build/`.

Validation passes `make test`, `make test-debug`, `go test -race ./internal/...`
and `go vet ./...`, plus debug/release server and CLI builds. Passing a
recorded route establishes that route only; saving, audio, external downloads
and further gameplay still need separate acceptance evidence.
