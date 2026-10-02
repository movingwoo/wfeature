# Testing

## Required gates

Run the checks relevant to a change from the repository root:

```sh
make test
make test-debug
go test -race ./internal/...
go vet ./...
```

`make test` runs Go and Node tests. The debug gate checks the same Go packages
with detailed diagnostics enabled. Format Go changes with `gofmt`. Build both
profiles when changing profile behavior. `make dist-check` validates release
archives after `make dist`; it is not a substitute for runtime tests.

JVM changes need authored Java class fixtures, Go regressions, and a packaged
J2ME integration fixture when applicable. Protocol changes exercise both
`internal/webhost` and `web/`. Use isolated saves for local archive probes.
Do not commit real games, runtime saves, or generated corpus reports.

## Test boundaries

| Area | Main evidence |
| --- | --- |
| Loaders | Invalid paths, duplicates, lengths, expansion bounds, metadata, and malformed inputs. |
| JVM | Authored bytecode/JARs, initialization, exceptions, monitors, threads, streams, and heap roots. |
| ARM | Authored ARM/Thumb programs, memory checks, instruction charging, and backend conformance. |
| Platforms | Service fixtures, Java/ARM round trips, graphics pixels, file/record failure injection, and resource lifetime. |
| Sessions | Lifecycle, save ownership, admission, parking/resume, protocol handlers, and encoder output. |
| Page | Node tests for controls, transport, composition, frame handling, and lifecycle. |
| Release | Cross-platform server smoke checks, archive contents, metadata, and embedded notices. |

A fixture proves its tested contract. Startup does not establish gameplay;
a write at boot does not establish saved progress; generated audio does not
establish audible playback. Static API references and startup import scans are
lower bounds, not a complete inventory of executed methods.

The ARM call-checkpoint tests and KTF continuation tests cover parked call
records, including restoration in a subprocess and recapture after another wait.
Cases cover native return cleanup, caught exceptions, and `Thread.run` delegation
whose target field changes after entry. JVM tests and a packaged J2ME MIDlet
check that unsupported bytecode callers are refused without changing the source
execution. These checks do not enable checkpoints for J2ME sessions.
Older module cases exercise compiled and native invocation, direct module waits,
two result registers, argument-spill cleanup, exceptions and recapture after a
second wait. Invalid remainders, missing children and overflowing stack cleanup
are rejected before execution, and the original worker can still finish.
They serialize the ARM address space, JVM heap, allocator and runtime metadata.
The destination registers VM native implementations without preparing guest
tables or recreating the fixture's ARM code. Dynamic objects and guest-memory arrays exercise
aliasing and both directions of subsequent writes. The packaged Java heap case
continues execution without running class initialization or MIDlet startup again.
Graphics tests preserve transparent color keys and a shared image/framebuffer
mask while rejecting malformed and truncated image records. Storage payload tests
preserve database generations and shared File/stream cursors. Authored Calendar
rules cover both sides of past and post-2038 transitions, subsequent Java calls,
malformed-record refusal, and source independence. Platform root tests exercise
restored Timer.cancel, event consumption, shared key ownership, and weak resource
collection. Editor tests continue an unfinished multi-tap cycle at a new clock
epoch, including UTF-16 limits and focused-editor/native-payload sharing. C storage
tests continue reads/writes and handle allocation, including a cursor retained
past truncation through another handle, and keep lazy cache state distinct.
Metadata tests continue native dispatch, allocation, pixel-cache lookup and
collection, and resolve saved older-module class links without rerunning entry.
Relay tests finish a partly written request, read the remaining reply, preserve
socket/stream ownership and close flags, and retain a runtime-only socket without
allocating a guest identity. A continuation case carries relay state through a
fresh process and finishes its partial frame after the worker returns.
Control tests preserve fractional guest time at another Host epoch, deliver
queued C text after discarding a canceled carrier, execute a pending network
callback once, and decode/play an unloaded C clip after restoration.
Vibrator tests reanchor active, expired and indefinite requests to a different
Host epoch, preserve the next request number, and reject invalid records without
changing an existing motor request.
Audio timeline tests compare subsequent event output and repeat boundaries,
stop saved active notes, preserve the next handle, copy nested PCM/SysEx data,
and reject malformed records without emitting output. This does not establish
restoration of already sounding Host voices or in-flight wave samples.
Client tests restore the joined records without guest startup, including multiple
parked workers, initial Timer grants, stack reuse order, retained LCD pixels,
pacing deadlines at another clock epoch, and logical audio/vibration. A separate
activation test advances the detached clock by ten seconds and verifies that
the final adoption preserves wait, timer, editor and device offsets. Recapture
before the first restored grant preserves its saved continuation. Invalid worker
ownership, stacks, budgets, heap roots, image identity and audio catch-up bounds
are rejected before any goroutine starts; the original source remains usable.
Malformed component records must leave existing metadata and heap bindings untouched.
A failed detached client is discarded. These component tests
do not establish full-session quick save/load. Set
`WFEATURE_CALL_CHECKPOINT_RESTORE_BINARY` to another compiled KTF test binary to
check restoration across debug/release profiles. To inspect pending calls in a
local KTF loading route:

```sh
WFEATURE_KTF_CALL_ARCHIVE=/absolute/path.zip go test ./internal/platform/ktf -run '^TestLocalKTFCallCheckpointInventory$' -count=1 -v
```

The inventory uses in-memory saves and reports ARM call components and complete
worker continuations separately. Acceptance does not establish that the session's
heap, devices, or files can be restored.

`TestLocalKTFClientCheckpoint` uses the same archive variable and a separate
in-memory save store. It captures after 300 manual-clock rounds, restores the
session from a bounded checkpoint envelope at another clock epoch, then
compares 100 rounds of exact frame bytes, flushes, ARM instruction totals, guest
time, worker counts and resulting saves against the source. Six local loading
routes passed this comparison on 2026-10-01: SHA-256 prefixes `cc2c96d671cd`,
`aa3fcba4598b`, `023c935aea8e`, `5d3ba49eccf7`, `ab601085dc04`, and
`6c96c5050b2b`. Three older module routes also pass: `1d5831e42a8a`,
`56d6865251cc`, and `98db83c3bda6`. They first refused capture with an unrepresented
supervisor category 7; the invocation/wait remainder tests above cover its repair.
This comparison does not establish gameplay, server-restart restoration,
durable save replacement or audible continuity. Authored older-module tests also
cover its initialized client and class links without re-entering the module.

KTF session-checkpoint tests join the execution record and save generation,
replace a real temporary directory, and make a displaced worker write during
its abort. That write must land only in the old client's isolated memory store.
Injected replacement failure, cancellation and a busy client leave the original
usable. A separate subprocess restores the envelope and saved files after the
source workers have terminated; the same cross-profile binary variable applies.
Backend container tests reject wrong archives, lengths, checksums and variants,
preserve raw save keys, and keep ordinary `.wfs` backups separate. Record tests
exercise typed-allocation amplification, depth/value limits, invalid UTF-8,
missing/duplicate/unknown fields, wrong scalar types and failed-decode isolation.

Shared-session tests use the newly authored archive generated by
`internal/testfixture/ktf.go`. It runs the ordinary descriptor/JAR/AOT startup
path and increments a guest startup counter. Capture/load restores that counter
without another startup, along with ordinary saves, pause state, held keys and
pointer, and key-repeat phase. A child process reads the reserved slot after the
source closes and restores through the public shared API. Set
`WFEATURE_SHARED_CHECKPOINT_RESTORE_BINARY` to a compiled `internal/session` test
binary to exercise this path between debug and release. Malformed shared settings
and input are refused before changing the live session or save generation.
Slot tests verify restart persistence, archive isolation, exclusion from loose
save export and survival across save replacement. Sparse oversized files,
symlinks and directories are refused. The state-record decoder also has a fuzz
target, `FuzzCheckpointRecord`; a 15-second run on 2026-10-01 processed 70,281
test inputs without a failure. This is parser evidence, not Host UI acceptance.

Audio output tests retain emitted note volume and note-on settings across later
channel changes, resume owned PCM tails at another clock epoch, exclude detached
preparation time, preserve future note-offs, match the page's voice stealing and
refuse malformed/oversized output state. Page audio tests stop PCM, percussion
and melodic release tails when replacing a timeline.

Host checkpoint tests cover shared-session live replacement, wrong-archive and
damaged-slot refusal, stale-input rejection, a fresh complete frame after an
epoch change and writer ordering. `TestCheckpointServerSubprocess` starts a real
WebSocket server in a fresh process and restores a saved guest value without
replaying startup, together with the earlier ordinary save generation. Set
`WFEATURE_WEB_CHECKPOINT_RESTORE_BINARY` to a compiled `internal/webhost` test
binary to test the two build profiles together. Both directions passed for
Java and native packages after the directory repair below. Paused-slot tests
check that live and startup browser restoration resume through the Host lifecycle.
Node tests
exercise delayed bitmap decoding across reset, audio definitions, stale request
cancellation, failed-load isolation, explicit disk restoration and button state.

CLI tests drive `run` through live JSON checkpoint commands and through a fresh
process with `-quickload`. They check saved input, no repeated guest startup,
restored ordinary saves, missing-slot refusal and save-claim release. Set
`WFEATURE_CLI_CHECKPOINT_RESTORE_BINARY` to a compiled `cmd/cli` test binary for
`TestCheckpointCLIProcess` and `TestCheckpointNativeCLIProcess`;
debug-to-release and release-to-debug both passed. A paused CLI restore reports
a stalled step and can resume through `park`, without retaining a false ending.
The authored native fixture in `internal/testfixture/ktf_native.go` runs its
entry, factory, event handler and scheduled frames through generated ARM code.
Its checkpoint tests compare subsequent frames, instruction counts, input,
elapsed time and ordinary saves without rerunning startup. Device tests preserve
file/resource buffer aliases, cached parsed indexes, a decoded image whose guest
pixels changed, unflushed writes, pending events, timers, resume callbacks and
sound. Detached staging advances by an hour before adoption to check rebasing;
continued runtime records and saves then match the source exactly. Malformed
allocator, mapping, pixel, callback, buffer and device records are refused before
adoption. Failed save replacement, busy execution and cancellation preserve the
original runtime and files. The displaced native runtime cannot flush discarded
pending writes into the adopted generation.

The three current local archives in the older BREW-labelled group contain Java
descriptor modules. They do not contain the native `.mod`/`.mif` package shape.
No real native package was available in the current local corpus on 2026-10-01;
native evidence above uses authored fixtures. The opt-in
`TestLocalNativeCheckpointContinuation` uses the same library discovery boundary
as the Host and skips explicitly when there is no native package.

A Chromium page check on 2026-10-01 used the actual picker, save/load controls,
key input and disk-restore button for both authored archive formats. It checked
a changed guest word returning to its saved value, then restarted the real server
and restored the same word through the page. Fresh frames arrived after restart,
and neither page reported a JavaScript error. The nine local digests listed above
also pass live save/load and restoration after a real server restart from their
loading routes, with a fresh frame and no page JavaScript errors.

Two local gameplay routes, `aa3fcba4598b` (Java AOT) and `1d5831e42a8a` (older
module), reach interactive play through CLI input before saving. A sequence of
left/fire/right input and 168 ticks has identical per-command screen digests,
final RGBA hashes and present counts after live restoration and in a fresh CLI
process. The page then loads each CLI-produced slot, saves/loads during play,
and restores again after restarting the server. These are bounded routes, not
completion of either game or a physical-phone/audio-listening test.

The final ordinary/debug gates, `go test -race ./internal/...`, `go vet ./...`
and 278 Node tests passed on 2026-10-01. `CGO_ENABLED=0 make dist` produced all
five configured targets in an isolated output directory. File locks have runtime
evidence on macOS; those builds establish Windows/Linux compilation only.
Six alternating baseline/current measurements at one Go CPU, 2,000 rounds each,
gave median instruction costs of 33.27/33.27 ns (`cc2c96d671cd`), 41.60/41.69 ns
(`aa3fcba4598b`, +0.22%) and 419.50/426.94 ns (`1d5831e42a8a`, +1.77%). Instruction
counts and guest times match. The module sample lasts about 60 ms and is more
sensitive to fixed overhead; these are loading-route measurements, not browser
FPS or a bound on all gameplay costs.

### Checkpoint keypad controls

The 2026-10-02 UI revision removes picker/settings checkpoint buttons and offers
quick save/load only as optional keypad assignments. All four default layouts
remain unchanged. Node tests cover per-type persistence, duplicate assignments,
editing unavailable cells, immediate requests, pending-operation exclusion,
inline failure feedback and retry. `make test` passes, including 280 Node tests.

Chromium at 320 px and WebKit at 390 px, both with touch enabled, exercised the
embedded release page against the repository-authored native checkpoint fixture.
Both checked touch and focused Space/Enter activation, restored guest memory,
absence of handset input and modal popups, stable keypad positions, sliding past
action cells, corrupt-slot refusal and retry, layout persistence across reload,
and quick load after ordinary startup on a restarted server. Reset removed all
checkpoint assignments. Neither browser reported a page JavaScript error.
The touch route also caught and verified the repair of canceled local-button
pointerdown suppressing WebKit's generated click. These are browser checks, not
physical-phone or real-game acceptance for this UI revision.

### Checkpoint adoption blocker

A controlled concurrent read exposed a save-generation defect on 2026-10-01.
After the replacement wrote its intent but before its first rename, an
independent reader treated the active transaction as an abandoned one and
removed staging and intent. Replacement then moved the live directory to its
backup and failed to install the missing staging. Its rollback found no intent,
so the original bytes remained in the backup while live saves were missing.
This failed three ordinary reproductions and a debug run using authored
temporary files. HTTP save listing and export reached the same recovery path.

The repair gives every directory-store operation the same filesystem lock,
including independent objects, complete loose-tree walks, imports and slot
access. Active rollback also uses its completed moves rather than relying only
on the journal. `TestSaveReplacementExcludesIndependentReaders` holds the
replacement at both rename boundaries, starts each independent reader, releases
the writer, and checks the complete resulting generation. Both successful and
refused replacements are covered. `TestSaveReplacementRollbackSurvivesMissingJournal`
checks preservation when the prepared files disappear. A reader must never be
joined while the test is deliberately holding its writer; that would deadlock
correct exclusion.

`TestSaveDirectoryAliasesShareTransaction` covers relative paths, linked parent
directories, and case/Unicode aliases when supported by the executing filesystem.
`TestSaveTransactionExcludesAnotherProcess` checks a child process waiting for an
active transaction. Crash-recovery tests also reuse an already-open store, so a
cached recovery result cannot hide a later process interruption.
`TestSaveClaimsExcludeSessionsButAllowReadsAndReleaseOnExit` checks the longer
Host claim separately: competing sessions are refused, reads remain possible,
and an abruptly exited child leaves no stale claim.

Focused ordinary/debug/race tests passed on macOS after the repair. Backend test
binaries also cross-compiled without cgo for the five distribution targets.
Those builds do not establish runtime file-lock behavior on Windows or Linux.
HTTP listing/export also run during repeated replacements and return one complete
generation; corrupt recovery records are reported as errors. Separate server
instances cannot claim the same live save directory. Ordinary/debug, full internal
race and vet gates passed after the repaired controls, and all 278 Node tests
passed. Native state coverage and final browser/gameplay acceptance remain open.

### Save-generation failure tests

Backend save-generation tests preserve empty and dotted keys, reject path
conflicts, oversized input and symlinked files, and check independent in-memory
reads and batches. Replacement tests verify deletions, the previous-generation
backup and rollback on either directory-rename failure. Subprocesses exit without
running defers before the swap, between its renames and after commit. Ordinary
reads recover each case and see one complete generation; concurrent snapshots
through the same store likewise see one generation. These tests exercise process
interruption on the executing host, not a physical power loss.

## Local acceptance

```sh
make acceptance
make acceptance ARGS="-platform ktf"
make acceptance ARGS="-games 'var/games/one,var/games/two' -out var/acceptance/sweep"
```

The runner drives opt-in probes and writes dated Markdown and NDJSON under
`var/acceptance/`. Record corpus membership, archive identity, source revision,
profile, save setup, stages attempted, pass/skip/fail outcomes, and limits.
A successful report-generation command can contain failing archives.

Cached results retain their original `measured_run`; they are not new
measurements. Compare the same corpus and stages. Use a separate output directory
for a one-off sweep. Content detection determines platform identity, and files
no platform claims remain reportable outcomes rather than disappearing.

The full probe commands, environment variables, report schema, route examples,
and interpretation rules remain in [testing history](history/testing.md).
[CLI reference](cli.md) describes replay, frame comparison, API scans, guest
profiles, and save inspection. Historical counts belong to their recorded runs.

Settling observes 64 frame samples and retains two rounds. The dated comparison
found no outcome improvement from four rounds. A changing screen can leave an
input question unanswered; it is not automatically an emulator failure. See
[settling evidence](history/testing.md#settling-settling-and-key-response-validation).

## Runtime boundary regressions

The 2026-09-17 fixtures cover thread lifecycle, archive selection/names, UTF-16,
JVM/ARM reentry, graphics and media ownership, storage failure recovery, and
intermediate frame delivery. The ordinary, debug, internal race, and vet gates
passed after those runtime changes. The implementation record retains precise
[fixture and ABI evidence](history/testing.md#runtime-runtime-boundary-follow-up-2026-09-17).

The alternate LGT graphics context layout remains unconfirmed. The later
[KTF investigation](ktf-qa-2026-09-21.md#native-environment-return-and-the-slow-case)
establishes tag-2 word results in the environment; other tags and wide
environment returns remain unconfirmed. The earlier runtime-boundary change
itself did not include new real-game, physical-device, or browser acceptance.

Compile the authored `ReentryProbe.java` with Java 8 target settings into a
temporary directory and copy only `ReentryProbe.class` into KTF testdata.
Its bridge declaration is compile-time only; the test installs the ARM body.
The SKT async JAR uses the authored MIDlet stubs and packages
`AsyncFailureMIDlet.class` plus its two anonymous classes with `ASYNC_FAILURE.MF`.
Do not include runtime stub classes. General fixture commands remain in
[the fixture guide](history/testing.md#implementation-compiling-a-java-fixture).

The SKT `tutorial-cache.jar` packages only the authored
`TutorialCacheMIDlet.class`, compiled from `testdata/src/TutorialCacheMIDlet.java`
with Java 8 target settings and the authored MIDlet stub on the compilation
class path. Its manifest selects `TutorialCacheMIDlet` with MIDP-2.0 and
CLDC-1.1. Do not package the stub. This fixture tests a revision-scoped cache
adaptation, including the original bounds exception and the corrected reload.

## Browser and device acceptance

The checked-in browser and load acceptance runners remove their own temporary
`var/ext/<run>` trees after stopping their server. Reports, screenshots, and
isolated saves remain available for diagnosis. The installation's root
`var/ext/.adopted` marker and user uploads are outside those temporary trees.

Go handler tests and Node client tests are ordinary gates. Local Chromium and
WebKit records additionally cover selected retention/takeover, reconnect,
restart, native-text, and save-confirmation routes. Automated composition events
are not evidence of a physical phone keyboard or desktop IME behaving correctly.

The earlier WebKit Blob failure did not recur in the September 12 instrumented routes;
its cause remains unresolved. Preserve lifecycle phase, browser version, Blob
properties, origin/navigation, and server frame evidence on recurrence. See
[WebKit observations](history/testing.md#webkit-webkit-frame-decoding-investigation).
It recurred during the September 30 SKT retained-session reload route; see the
[resume investigation](skt-resume-2026-09-30.md#validation) for the retained-state
evidence and the browser acceptance limit.

The [PWA release acceptance route](pwa-acceptance.md) defines an opt-in two-version
Chromium/WebKit check and separate physical-device steps.
There is no complete automated PWA acceptance claim. Installation, service-worker
replacement, audible audio activation/recovery, touch, real-phone suspension,
and saved progression need explicit routes and observations. A mocked bitmap
decoder does not establish successful decoding in a real browser.

## Rapid-fire input

[Rapid-fire behavior and measurements](rapid-fire.md) records the deterministic
input tests, real WebSocket route, Chromium/WebKit checks, and the separate
server CPU probe. Its authored fixture does not establish all-game input cost.

## Frame delivery

`internal/webhost/frame_encoding_test.go` checks pixel changes, dimensions,
scaling, explicit redraws under queue pressure and lifecycle-message ordering.
Session tests preserve owned raw snapshots and the synchronous scaled-frame API.
`BenchmarkFrameEncoding` compares unchanged and changing pictures at original
scale and hq4x through the actual encoder goroutine; it excludes guest execution.
Protocol 2 tests reconstruct consecutive updates pixel for pixel the way the page
composes them — complete, masked, replacing and shifted — check the scale each
names, the palette, a scrolling field under a fixed bar, a shift with nothing
left to draw and the refusal to shift over a picture that is not opaque, and
verify protocol negotiation through the WebSocket handler.
`TestFramePatchBandwidth` and `BenchmarkFramePatchBandwidth` compare protocols 1
and 2 over an authored moving sprite and an authored scrolling field; neither is
real-game traffic. `audio_stream_test.go` covers the binary sound operations,
definitions carried once, the budget, a shed message and protocol 1's unchanged
JSON. `outbound_test.go` checks that queued messages leave in one write and that
sound waits for its tick's picture only while one is being encoded. Static
serving tests cover gzip, validators and 304 answers for the shell and the game
list. `internal/wsproto` checks that each message, and each batch, is one
transport write.

`internal/filter/hqx/javascript_test.go` regenerates `web/hqx-patterns.js` from
the Go tables and fails when they differ, and holds the digests of an authored
picture magnified at each scale; `web/hqx.test.mjs` magnifies the same picture
in JavaScript and must produce the same digests, and checks that redoing only
the blocks around a change matches magnifying everything. `magnify.test.mjs`,
`frame-stream.test.mjs` and `session.test.mjs` cover the page's side of the
protocol with mocked canvases and sockets.

`internal/vp8l` round-trips its pictures through the WebP decoder in
`golang.org/x/image`: colour counts either side of each colour-table size,
widths either side of the neighbourhood codes, authored pixel art with
transparent bands, gradients, and every path the defaults avoid (the predictor
on every picture, no colour cache). A fully transparent pixel may come back
with another colour, which is the one difference allowed. `FuzzEncode` does the
same for random small pictures, and the prefix, distance and Huffman helpers
are checked against the decoder's arithmetic. An Encoder keeps tables
between pictures, so one reused encoder, and copies of it used in turn, must
write exactly what a fresh one writes, including when its position base wraps;
the parse and the colour-cache trial, both written for speed, are checked
against plain versions of themselves, the parse by `FuzzReferences` as well. A browser decodes with its
own decoder rather than the Go one, so the encoder was also checked by hand
against `dwebp`, the WebP project's decoder, on the authored
pictures and on all 13,268 rectangles of recorded play; see
[the record](history/session.md#frame-bandwidth-webp-2026-09-25). The session
tests in `internal/webhost` compose every protocol 2 case from WebP as well as
PNG. Translucent-frame regressions compare both formats after composition,
including complete pictures, replacing and masked updates, explicit redraws,
and both paletted and full-colour pictures. WebP must use PNG's conversion from
premultiplied RGBA to straight alpha, including its rounding.

The opt-in browser route uses repository-authored SKT and LGT fixtures:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
  node web/acceptance/frame-delivery.mjs build/release/wfeature-server chromium
```

Use `webkit` for the second engine. `WFEATURE_FRAME_ARCHIVE` may name a local
archive under `var/games` for an additional six-second original-scale probe.
The runner uses isolated game and save directories and records results under
`var/acceptance`. It exercises static presentation, restart, park/resume,
reconnection, changed input frames, lossless update composition against a forced
complete server picture, WIPI scale changes, and hq2x of partial updates against
hq2x of a forced complete picture. A static picture without new pictures is
expected; the runtime must keep ticking. Both engines decode lossless WebP, so
the route also checks that the page asked for WebP pictures and drew them, and
every composition check above ran on WebP. Measured scope and results are in
[the CPU investigation](cpu-saturation-investigation-2026-09-23.md) and the
[bandwidth record](history/session.md#frame-bandwidth-2026-09-24).

## Reading old validation

[Testing history](history/testing.md) keeps detailed experiments and release
checks. [Coverage audits](history/coverage.md) retain the 2026-09-12 source
snapshot; their missing-method tables are historical, not a current task list.
Current platform overviews identify supported behavior and remaining limits.
Documentation consolidation itself is not a new runtime acceptance run.
