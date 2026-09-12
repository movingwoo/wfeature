# SGS completion acceptance

## Resumed managed work

The user resumed SGS work on 2026-09-12 using the managed workflow established
for the carrier platforms. PR #136 is merged; the restart baseline is main at
`2b56bad`. The pause below is historical and no longer prohibits SGS work.

The five existing open areas remain in scope: systematic SIS metadata comparison,
the twelve-title gameplay/save/audio/lifecycle matrix, Host UI/communication and
identity services, extended SIS/SAF decoding and composition, and the remaining
compatibility differences. A working subset does not complete this scope.

Three `gpt-5.6-sol` workers at `xhigh` use isolated worktrees. Initial ownership
is image contracts and metadata comparison, Host service contracts, and corpus
acceptance. The manager owns shared integration, final review, testing and
separate commits/PRs. Workers do not publish or merge. Runtime game archives and
original saves remain read-only; each probe uses isolated save data.

Implementation follows executable slices with evidence and regression tests.
Unclear contracts remain explicit until compared with the original runtime or
a demonstrated caller. The existing normal/debug/race/vet and profile-build
gates still apply, and Host changes require session/browser verification.
Physical audio and OS IME observations are reported separately from automated
checks. No whole-platform completion follows from green unit tests alone.

The systematic SIS metadata comparison is now complete in PR #154. The manager
reran all 102 authored native results against the Go service in normal, debug
and race builds; all return values and 510 output words matched. This closes
only the metadata comparison item; the other four areas remain open.

The standalone runtime-mode correction in PR #155 initializes the guest with mode 2 and
implements `0xd2` without confusing that state with subsequent event parameters.
The manager integration passed the normal/debug suites, internal race tests,
vet, and both CLI/server build profiles. Matched, SHA-verified runs of all twelve
archives produced identical frames and flush counts before and after the change.
A real-server headless Chromium check rendered IDs 06 and 07, exercised key
input, and returned to the launcher to start another session without page or
protocol errors. This is startup/lifecycle evidence, not completion of their
gameplay, save restoration or audible playback acceptance paths.

PR #156, stacked on #155, corrects the random services to the observed 32-bit
recurrence and 15-bit draw semantics. All 196 authored native service results
and final states match the Go regression. The manager reran the full test,
debug, race, vet and CLI/server profile-build gates and a twelve-archive startup
probe. The directed ID 09 restore/control distinction also survives the change;
later randomized play frames are not expected to match the former substitute
generator. See [random evidence](sgs-random.md).

The restart corpus manifest was checked against all twelve current archive
SHA-256 values; every entry matched. A separate read-only descriptor inventory
found one paired descriptor per archive: ten MOD files and two INF files.
A subsequent descriptor audit found six distinct payloads and no `UserID` key.
An optional connection-ID member is not proven to map to the installed UserID.
The original PC Host instead reads external configuration with an empty
default. Service `0x53` now supports that default and an internal, bounded
session option; no release Host exposes a UserID setting. No identity is inferred
from a filename, subscriber phone number or executable save-owner digest. See
[the identity source evidence](sgs-host-state.md).

## Combined extraction and Host checkpoint

Combined revision `d250de9` includes type-1 SIS literal/coded tiles, reference
replacements, ordered composition, geometric transforms, canvas inversion and
extraction-inert header flags. It also includes native text input, intentional
external-link handoff, invocation yielding, and the local request-status path.
All eight native SIS comparison groups passed together, as did general/debug
checks, internal race tests, vet and debug/release CLI and server builds.
These results establish the documented contracts, not complete SIS/SAF support.

At preceding revision `fc8e8ea`, a manifest-verified startup check opened every
archive twice without a persistent save store. Each run advanced 1,200 ticks of
16 ms and pressed/released Right at ticks 900/906. All 24 runs remained active
and closed without error. Each pair had identical final framebuffer hashes and
presentation counts. Counts by ID were 93, 74, 73, 73, 171, 93, 120, 75, 92,
600, 92 and 48. This check covers bounded startup, input and Host restart; it
does not establish directed play, restored progress, guest exit or audible output.

Type-1 extraction returns packed resource bytes. A successful extraction is
not proof of the complete extended-image drawing path through the browser.
Type 2, SAF and remaining Host contracts still require their own executable
acceptance paths. The corpus gate below remains open.

## Paused at user request

Development of further compatibility features stopped on 2026-09-11 at the
user's request. The user subsequently authorized a PR containing all SGS work
completed so far. That PR is a partial-support delivery; it does not depend on
finishing the full-compatibility requirements below. At that checkpoint, further implementation
remained paused; the explicit resumption above supersedes that instruction.
Unrelated workspace changes are excluded from the PR.

The local SIS metadata query for `0xe8` is included, with focused header,
instruction, address-bank, malformed-input and work-limit tests. Systematic
comparison of the native SIS metadata probe with Go was follow-up work at that
checkpoint and is completed by the resumed comparison above.
SAF metadata and the host image branch remain explicitly unsupported.

The SAF zlib object-body component is tested but remains disconnected from
image service execution. Full file parsing, additional codecs and composition
remain open. See [extended images](sgs-images.md), [SIS evidence](sgs-sis.md),
and [the opcode inventory](sgs-opcodes.md). Missing entries have not all been
shown to block the twelve local titles. The corpus matrix remains incomplete,
including user-visible save progress and audible playback checks.

## Partial-support PR validation

The isolated SGS branch passed `make test` (Go and browser-client unit tests),
`make test-debug`, `go test -race ./internal/...`, `go vet ./...`,
`make server server-release debug release`, and `git diff --check` on
2026-09-11. This includes the latest SIS metadata query. The standalone branch
uses the existing handset menu value directly instead of depending on an
unrelated, uncommitted Java input change.

These checks validate the delivered implementation and authored fixtures. They
do not complete the twelve-title gameplay/save/audio matrix, add missing host
services, or establish full SIS/SAF rendering support. Earlier corpus and browser
observations remain historical evidence, not new runs of this PR checkout.

## Previous full-compatibility objective

The following gates describe full compatibility, which remains unfinished.
They are follow-up requirements, not blockers for the user-authorized partial
support PR. Earlier user acceptance closes the reported defects, not the full
service inventory.

## Implementation gate

- Inventory every original dispatch entry, distinguishing implemented services,
  reserved no-ops, host operations and unresolved contracts.
- Implement missing instructions and services with authored regression fixtures,
  bounded guest memory, cancellation and shared CLI/server execution.
- Exercise host-dependent behavior through the actual session and browser client.
  External operations need a host handoff; silently ignoring them is not support.
- Record divergences from unsafe original behavior and any contract still unknown.
  Unresolved required contracts keep this gate open.

## Corpus gate

Use the twelve IDs in the ignored local archive manifest. Each title needs a
recorded route to its main playfield with actual input, plus its applicable
save/load, audio, termination and restart route. A generic key sequence, a
successful parser or reopening an empty save directory is insufficient.
For titles without a user-visible save or audio feature, record evidence of that
absence rather than inventing a passing test. Keep archives and captures ignored.

| ID | Main play route | In-game save/load | Audio | Exit/restart |
| --- | --- | --- | --- | --- |
| 01 | Directed play and settings routes verified | Changed audio setting survives restart and differs from immutable-seed control; gameplay progress remains outside this proof | Activity observed; audible check pending | Directed Host close/reopen verified |
| 02 | Directed play route verified | No save activity in traversed route; wider applicability unresolved | Activity observed; audible check pending | Empty-store Host close/reopen verified |
| 03 | Directed fight route verified | Startup default only; visible progress pending | Wave activity observed; audible check pending | Directed Host close/reopen verified |
| 04 | Directed fight route verified | Startup default only; visible progress pending | Wave activity observed; audible check pending | Directed Host close/reopen verified |
| 05 | Directed play entry and input verified | Payload reloads; visible-progress control still indistinguishable | Activity observed; audible check pending | Directed Host close/reopen verified |
| 06 | Interactive management route verified | Seeded and empty controls are visually identical; progress pending | MIDI activity; audible check pending | Directed Host close/reopen verified |
| 07 | Directed purchase and restored management/HUD verified | New payload restores changed currency and supply quantity versus immutable seed | Activity observed; audible check pending | Directed Host close/reopen verified |
| 08 | Directed battle field verified | No save write on route; applicability pending | Activity observed; audible check pending | Directed Host close/reopen verified |
| 09 | Directed restore-to-playfield route verified | New 64-byte save survives reopen and changes visible state versus immutable-seed control | Audible check pending | Directed close/reopen verified |
| 10 | Directed field/HUD and dialogue verified | No save write on route; applicability pending | MIDI activity; audible check pending | Directed Host close/reopen verified |
| 11 | Local title selection reaches room and player movement | No save call on route; applicability pending | MIDI activity; audible check pending | Directed Host close/reopen verified |
| 12 | Directed field and movement verified | No save write on route; applicability pending | MIDI activity; audible check pending | Directed Host close/reopen verified |

Directed routes, save controls, and the ID 11 alternate-menu correction are recorded in the
[dated corpus evidence](sgs-corpus-2026-09-12.md). For ID 09, the shared map alone did not
prove progress restoration; the same-input seed control was necessary.

## Final gates

Run `make test`, `make test-debug`, `go test -race ./internal/...`, `go vet ./...`
and debug/release builds after implementation. Review the final diff, preserve
unrelated workspace changes and exclude local archives, saves and diagnostics.
Then create a branch and PR for the user to review and merge. Do not merge it.

## Integer and calendar checkpoint

The next slice adds signed signum (`0xa4`), widened means of two/three words
(`0xab`, `0xac`), minimum of three (`0xb0`), first maximum index (`0xb1`) and
nearest-value index (`0xb3`). Means truncate toward zero. Searches retain the
first tie and charge work per guest word. The nearest search uses widened
absolute differences and preserves its later-exact-match early return.
Original handlers: `0x41b240`, `0x41b5b0`, `0x41b5e0`, `0x41b710`, `0x41b770`,
`0x41b850`. All are independently implemented; no lookup assets are copied.

Date (`0xb8`, `0x41c200` calling `0x40c900`) returns local year, month, day and
Sunday-zero weekday. Time (`0xb9`) retains local hour, minute, second and
millisecond fields. Both validate the complete result span before any write.
Authored tests include leap day, word extremes, truncation, ties, span failures,
stack underflow and work exhaustion. This is an implementation checkpoint, not
completion of the corpus or full service gate.

The same checkpoint adds six trigonometric services (`0xa5`–`0xaa`) using
mathematical formulas validated against all 576 relevant original table entries.
Ratios use scale 100; angles use integer degrees. Tests span every signed-word
input, periodicity, symmetry, domain errors, endpoints and stack preservation.

Bitmap queue services (`0x73`–`0x75`) now support twenty entries, stable coordinate
sorting, mirroring, retained queue state and bounded atomic batch drawing.
Insertion consumes a reserved result slot and four arguments; a stack-save/
restore fixture proves result recovery. Queue scratch count/coordinate bytes
remain coherent. Native pointer-byte aliases are explicitly rejected, and
reallocated resource storage is retained safely instead of becoming a dangling
native pointer. These differences remain recorded compatibility boundaries.

The original empty reserved entries and device no-ops now retain their verified
stack effects. Capability probe `0xe1` returns zero for selector one, otherwise
minus one. No unrelated host/UI operation is treated as a reserved no-op.

An authored SGS audio test passes through the real SMAF decoder and backend
sink, verifying guest-time note onset, pause/resume, stop, replay and silence on
session close. This establishes the service pipeline, not audible acceptance
for the twelve real archives.

Checkpoint validation passed: `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and debug/release server and CLI
builds. The full implementation and corpus gates above remain open.

## Clipping and pixel read checkpoint

Clipping (`0xc8`, original `0x41cb90`/`0x40e760`) now sorts inclusive endpoints
and intersects them with the screen. A wholly external rectangle is empty;
`0xc9` restores the full screen. Pixel read (`0xca`, `0x41cbe0`/`0x40ee00`)
returns the unsigned raw RGB332 byte, independently of clipping and palette
bank, or -1 outside the screen. Authored tests cover reversed and extreme
coordinates, a single pixel, empty intersections, actual clipped writes,
reset, unsigned byte 255, invalid reads and atomic stack-underflow rejection.

Five-argument formatting (`0x8e`, original `0x41ac70`) now uses the existing
bounded formatter, consuming destination, format resource and five signed
words. Original wrapper inspection confirms the scalar order and count passed
to `0x41a500`. Tests exercise mixed conversions and signed extremes, preservation
of the caller stack and rejection before mutation when the seventh operand is
missing. The opcode inventory also corrects vector rows to existing partial
execution paths; those services were not absent merely because their handlers
live in the VM instead of platform dispatch.

Clipping checkpoint passed the full test/debug/race/vet checks. The subsequent
five-argument formatter passed focused normal tests, SKT debug/race tests and
vet, followed by debug/release CLI and server builds. The inventory currently
contains 107 entries with dispatch paths, 13 partial contracts and 42 missing
services; these counts are not a compatibility or corpus-pass score.

## Resource transfer checkpoint

`0x87` exports bytes from word memory into a resized resource; `0x88` imports
resource bytes into word memory. Both consume `(word address, byte offset,
resource ID, byte count)` and return no stack value. Original bodies at
`0x419fb0` and `0x419f20` establish the direction, offset unit and copy count.
Words are little endian; unaligned transfers preserve neighboring bytes.
Resource reads use the existing bounded guest resource-region model.

The implementation validates and snapshots both spans before destination
mutation, unlike the original unchecked native copies. Invalid addresses,
negative offsets/counts, bank crossings, missing source bytes and exhausted
work budgets fail without partial writes. Tests cover both directions and both
word banks, byte order, odd offsets, empty transfers and caller-stack retention.

Resource transfer checkpoint passed `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, both server/CLI profiles and
`git diff --check`. Full service and corpus acceptance remain open.

## Raw color inversion checkpoint

`0xcd` inverts the raw RGB332 bytes within a sorted inclusive rectangle and the
current drawing clip. Original wrapper `0x41ccc0` calls `0x40ee40`, whose pixel
operation is bytewise NOT. No palette-bank conversion or automatic presentation
is involved. Work is charged for the intersected area before any write.
Authored tests cover all 256 raw colors, restoration by applying the operation
twice, clipping, reversed bounds, caller-stack preservation and atomic work-limit
failure. Inspection also corrects the prior tentative classification of
`0xcb`/`0xcc`: their helpers build rounded rectangles, not scrolling regions.

The same rendering follow-up implements `0xce` whole-buffer white-fill/wrapping
scroll, including selection of the backup buffer and the original oversized
white-fill quirk. [The scroll contract](sgs-scroll.md) records the evidence.
Authored fixtures cover both buffers, displacement signs, diagonals, zero and
full-dimension shifts, large wrapped shifts, invalid selectors and work failure.

Inversion and scroll checkpoint passed `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, debug/release server and CLI
builds and `git diff --check`. These authored service checks do not replace
the still-open twelve-title main-play/save/audio acceptance routes.

## Recorded-route audio and persistence observations

The ignored `build/sgs_activity_replay.go` augments the actual user-input replay
with the shared recording audio sink and an observed save-store wrapper. It
copies existing saves into isolated directories; original user saves are never
modified. MIDI message counts include control/program messages and are not a
claim of audible notes or fidelity. Wave counts measure decoded sample values.

| ID | MIDI messages | Wave samples | Save writes before reopen |
| --- | ---: | ---: | ---: |
| 06 | 1708 | 0 | 3 |
| 07 | 1073 | 12786 | 0 |
| 08 | 496 | 980 | 0 |
| 09 | 822 | 0 | 3 |
| 10 | 235 | 0 | 0 |
| 11 | 457 | 0 | 0 |
| 12 | 830 | 0 | 0 |

All seven recorded routes complete and close without errors. Dedicated repeats
for 06 and 09 reopen the archive using the same isolated store and observe the
exact last-written 64-byte payload returned by a load. Both reopened sessions
advance and close successfully. This proves persistence across those session
boundaries, but not which visible progress/settings fields the games restored.
No write on the other recorded routes does not establish that those titles lack
saving. The main-play and audible acceptance gates therefore remain open.

Evidence: `build/sgs-activity-replay.log`, `build/sgs-activity-reload-6.log`,
`build/sgs-activity-reload-9.log`; frame captures are under the ignored
`var/acceptance/sgs-audio-save-replay-2026-09-11-saved/` directory. ID 10 is rerun
at its selected 176×176 default in `build/sgs-activity-native-10.log`; the other
six use their selected 128×160 defaults.

## Rounded rectangle checkpoint

`0xcb`/`0xcc` now draw outlined/filled rounded rectangles using sorted inclusive
bounds and an absolute radius clamped to half each endpoint span. Original
helpers `0x40e810` and `0x40ea10` establish the integer midpoint recurrence,
straight edges and horizontal fill spans. The filled helper normalizes an
inverted middle interval through `0x40e640`; consequently a zero-height filled
rectangle can cover three rows. Authored regression grids preserve this quirk
rather than silently replacing it with conventional rounded-rectangle geometry.

Work preflight charges midpoint iteration bounds even when clipping eliminates
all writes, as well as raster work. Tests cover signed radius extremes, clipping,
both drawing modes, palette/transparency, no implicit flush and budget failure.
Extended text investigation is recorded separately in
[sgs-text-effects.md](sgs-text-effects.md); its unresolved large-style coordinate
contract keeps that service gate open.

Rounded-rectangle validation passed `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, debug/release server and CLI
builds and `git diff --check`. The newly observed audio/save activity is
additional corpus evidence, not completion of the full acceptance matrix.

## Extended text and trace checkpoint

`0xcf`/`0xd0` implement bold, italic and underline with transparent/filled
backgrounds. Alignment and advances are unchanged. Large-style coordinate
behavior was confirmed by executing the original helper with an authored
all-ink glyph in an isolated x86 interpreter; its shortened horizontal loop and
continuous bit cursor are preserved. Source-bound violations at negative x fail
before drawing. Total extended-text work is preflighted before mutation, while
ordinary text retains its former raster and cost. The existing bundled fonts
remain substitutes. See [the effect evidence](sgs-text-effects.md).

`0x89` substitutes a single resource string for repeated `%s` conversions,
validating resource IDs, terminators, aliasing and work before resizing the
output. The result is capped at 255 bytes plus NUL. Width, padding and bounded
precision are covered by authored fixtures; precision deliberately avoids the
original non-advancing parser. See [resource formatting](sgs-resource-format.md).

`0xf0` formats three signed words and `0xf1` formats one resource string into
host diagnostic trace output. They do not draw or present a frame. The shared
logger receives a debug record with ASCII-escaped guest bytes, preserving the
English shell-output constraint and the configured log threshold. Tests cover
scalar ordering, resource errors, stack preservation, unchanged pixels/resources
and suppression at an info-level threshold. No diagnostic interpreter or its
packages are bundled with the Go runtime.

Extended text/string/trace checkpoint passed `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, debug/release server and CLI
builds and `git diff --check`. Host UI/communication services and remaining
real-title acceptance routes still prevent full-goal completion.

## Device result checkpoint

Device information (`0x51`) and the five-word device result (`0x54`) now
validate the whole destination and charge work before writing. Truncated
destinations, negative addresses, bank crossings and exhausted budgets leave
guest memory unchanged. Authored tests cover both word banks, retained
neighboring words, caller stack preservation and LCD class thresholds.

The empty phone identifier returned by `0x52` uses the shared resource allocator,
preserving its capacity and retained-tail semantics instead of replacing the
bank with a one-byte allocation. No real phone identity is supplied yet.

This checkpoint passed `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, debug/release server and CLI
builds and `git diff --check`. It does not complete the host communication
services or the remaining real-title acceptance matrix.

## Vector and device-wrapper checkpoint

All twelve operators of `0xb4`/`0xb5`/`0xb6` were compared with original helper
execution over 5,760 authored cases, including forward/reverse overlap, signed
extremes, shift boundaries and arithmetic faults. The comparison exposed and
verified a fix for scalar division/remainder by zero with nonpositive counts.
Vector loops now reserve their full execution work before mutation, while
preserving earlier elements when a later arithmetic fault occurs. Tracked
instruction fixtures cover aliases of either source and both word banks.
See [vector evidence](sgs-vectors.md).

`0xe0` consumes six operands with no result. Its conditional device helper is
empty in the inspected original runtime. The host now accepts that exact stack
contract, with underflow rejection, rather than reporting an unknown service.

The remaining extended-image services were traced to proprietary SIS/SAF
streams, not PNG. Their selector and metadata contracts are recorded in
[extended images](sgs-images.md); compressed payload decoding remains open.
[Host-state evidence](sgs-host-state.md) also distinguishes device MIN, script
UserID and outgoing/incoming role; those findings do not complete communication
integration.

This checkpoint passed `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, debug/release server and CLI
builds and `git diff --check`. Real-title acceptance and remaining services
still prevent full-goal completion. The later partial-support PR authorization
is recorded at the top of this document.

## Compressed image investigation checkpoint

SIS type 1 and type 2 header layouts, object boundaries and composition
dependencies are recorded in [SIS evidence](sgs-sis.md). Literal tiles,
variable-length runs and adaptive object coding are distinct formats; the
remaining composition and coding contracts still need verification.

The SAF object-body decoder now handles compression 1 with the standard Go
zlib implementation. Input lengths, dimensions, output size, checksum and work
are bounded before returning owned pixels. Six authored headers matched the
original parser; twelve authored compressed streams matched the original
decompression wrapper. Tests cover packed depths, maximum byte dimensions,
malformed data, checksum errors, excessive output and work exhaustion.
See [the exact integration boundary](sgs-images.md#object-compression-1-checkpoint).

This is a tested decoder component, not an implemented extended-image service:
file parsing, decoder state, frame composition and host dispatch are still open.
No additional dependency, original decompressor code or third-party asset was
added. Full-suite normal/debug tests, internal race tests, vet and both profile
builds passed; the final maximum-size fixture also passed focused normal/debug
and race runs. The full SGS goal remains open.
