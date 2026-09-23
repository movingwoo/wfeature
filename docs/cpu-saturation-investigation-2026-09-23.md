# CPU saturation investigation

The baseline presentation path had two concrete optimization candidates: discard
unchanged pictures before scaling and encoding, and move ordinary-frame scaling
off the emulator goroutine. A bounded local experiment establishes their cost;
it does not reproduce the user's unspecified saturated gameplay scene.

The initial investigation changed no product code, running server, or existing save. Diagnostic
programs and measurements remain under ignored
`build/cpu-investigation-20260923/`, with fresh saves under
`var/savedata/cpu-investigation-20260923/`.

The subsequent implementation and verification are recorded at the end of this
document. The baseline measurements below describe the earlier checkout.

## Scope and method

- Source: `53cec34`, Apple M1, 8 logical CPUs, 16 GiB RAM, macOS,
  Go 1.27.1 darwin/arm64. Initial load averages were 3.11, 2.56 and 2.35;
  this was not an otherwise empty machine.
- The existing debug server was idle at the initial observation. The affected
  platform, machine, scene, speed and scale settings were not supplied.
- One KTF archive, SHA-256 prefix `23919eb33365`, was selected from the local
  library by large inner-JAR size. This is a reproducible workload, not a claim
  that archive size predicts gameplay cost. Another candidate started and ran
  but had too little work in this window for the main comparison.
- The standalone harness calls the existing KTF session API, advances a manual
  clock to each deadline, and runs 100 ticks from fresh saves. It links the
  committed CLI PGO profile. It omits browser transport, logging and audio sinks.
- CPU time is process user plus system time from `getrusage`; wall time and Go
  allocation counters cover the tick loop after startup and an explicit GC.
  The frame harness links the committed server PGO profile, uses the same hqx
  scaler, PNG `BestSpeed`, buffer pool and encoded-byte copy as the server.
- Modes run in interleaved orders, three repetitions each. Tables show medians.
  Small differences are not presented as statistically established improvements.
  The known macOS sampling-profile attribution issue described in
  [testing history](history/testing.md) is avoided for the timing conclusions.

## Diagnostic overhead is small in this workload

All twelve core runs completed 100 ticks, retired exactly 358,036,632 measured
guest instructions, reached 5.221374215 seconds of guest time, produced 100
flushes, and ended at frame digest `6571936240797073961`.

| Diagnostic mode | CPU seconds | Wall seconds | Allocated bytes | Allocations |
| --- | ---: | ---: | ---: | ---: |
| No trace or guest sampling | 2.752 | 2.539 | 25,831,536 | 7,923 |
| Ordered trace only | 2.762 | 2.544 | 32,267,704 | 7,936 |
| Trace plus sampling every 1,000 instructions | 2.780 | 2.567 | 34,088,760 | 371,900 |
| Trace plus sampling every 10,000 instructions | 2.785 | 2.560 | 32,499,112 | 44,349 |

The debug server enables the 1,000-instruction sampler in
`internal/webhost/session.go`. Its diagnostic configuration costs about 1.0%
more process CPU and 1.1% more wall time than the untraced configuration here.
This isolates those knobs in one binary; it is not a complete debug-versus-release
server comparison. Sampling produces many small allocations, but reducing the
frequency does not establish a CPU improvement in these runs. Disabling it is
not supported as a general cure for saturation.

A separate phase observation attributes nearly all tick time to timer callbacks,
about 25.35 ms per round. These callbacks execute guest code; the number is not
evidence that the timer queue itself is expensive. The base core workload takes
about 7.09 wall-clock nanoseconds per retired guest instruction, including its
platform calls. Because the manual-clock probe intentionally removes waits,
its busy process is not a reproduction of a paced server saturating a core.

## Scaling and encoding have measurable cost

The captured frame has 240 by 320 pixels and eight distinct RGBA values. Each
run warms ten frames, then processes 300 copies using the presentation pipeline.
This measures one simple image, not a representative gameplay-image corpus.

| Scale | Scaling ms/frame | PNG and output copy ms/frame | CPU ms/frame | Allocated bytes/frame |
| --- | ---: | ---: | ---: | ---: |
| Original | negligible | 0.609 | 0.609 | 6,365 |
| hq2x | 2.675 | 2.461 | 5.172 | 1,421,531 |
| hq4x | 7.001 | 9.197 | 16.304 | 5,072,116 |

At twenty frames per second the measured hq4x CPU cost projects to about 32.6%
of one core, versus 1.2% for original scale. That is arithmetic from a component
benchmark, not a measured whole-server utilization reduction. Network writes,
browser work and guest execution are outside this comparison. Selecting
original scale is an immediate way to avoid this cost when hqx is enabled.

At baseline, the ordinary server route was `sessionRunner.pushFrame` calling `Session.Frame`,
which calls `Session.magnify` for KTF and LGT. Scaling therefore runs on the
emulator goroutine, before the nonblocking queue send. Even a picture discarded
because that queue is full has already paid the scaling cost. PNG encoding runs
on the encoder goroutine.

KTF intermediate frames take a different route: `FrameUpdate` carries unscaled
pixels and the scale factor, and `writeFrames` scales them on its own goroutine.
This existing path establishes a suitable ownership boundary for moving ordinary
frame scaling too. Moving work changes which goroutine pays for it; it does not
by itself eliminate CPU work or parallelize guest instructions.

## Unchanged ordinary frames are a stronger CPU-saving candidate

A separate capture of the same 100 ticks compared complete RGBA slices with
`bytes.Equal`: 97 of the 100 pictures were identical to their predecessor. The
captured sequence was replayed through hq4x and PNG, three interleaved runs per
mode, with a scratch-only duplicate check before scaling and encoding.

| Replay mode | Pictures encoded | CPU seconds | Wall seconds |
| --- | ---: | ---: | ---: |
| Process every picture | 100 | 1.554 | 1.549 |
| Process only changed pictures | 3 | 0.048 | 0.048 |

The experiment reduces presentation CPU by about 96.9% for this mostly static
sequence. Guest execution continues unchanged and is not included in that saving.
Moving scenes with few repeated pictures will benefit much less. This is a
component prototype, not an implemented or browser-validated server fix.

The baseline `pushFrame`/`writeFrames` path had no pixel equality check.
KTF's separate intermediate-frame publisher already compares its own retained
pixels; the missing check concerns ordinary delivery and duplicates between
the two routes. A server-level comparison must include dimensions and scale,
retain independently owned pixels, and reset or force presentation on game
changes, reconnect/takeover, resume and display-setting changes. It must not
skip guest callbacks, timers or painting merely because the output is unchanged.

## Scope selected for implementation

Compare raw owned pictures at the encoder boundary, preserve explicit redraws,
and scale only accepted pictures there. This targets repeated presentation work
and the emulator goroutine's unnecessary scaling. Verify Go session/host tests
and page frame behavior, including changed frames, scale changes, reconnects,
intermediate/ordinary delivery and output pixels, before treating it as a fix.

If a saturated scene already uses original scale and changes every picture,
these measurements do not identify its cause. Capture that scene's platform,
profile, scale, speed, delivered frames and guest progress, then measure the
execution path. PGO, decode caches and several guest-loop fast paths already
exist. Adding sleeps or imposing a CPU ceiling trades guest progress for lower
utilization; it is not established as an optimization by this investigation.

## Local evidence

The ignored directory contains `main.go`, `frames/main.go`, `candidates.json`,
`measurements.ndjson`, `frames.ndjson`, `observation.json`, `dedupe.ndjson`, raw
frame captures and sampled allocation profiles. Archive paths appear only in the
ignored candidate mapping. There are twelve checked core comparisons, nine
single-frame pipeline measurements and six sequence replays. Core comparisons
agree on instruction counts, guest time, flush counts and final frame digest;
this does not establish frame-by-frame equivalence across entire game routes.

No product Go or JavaScript source changed during the initial investigation, so
that stage did not run full regression gates. The diagnostic programs built
successfully and all measured runs completed.

## Implementation and verification

The server now queues unscaled `Session.FrameUpdate` snapshots. Its encoder
compares owned raw pixels, dimensions and scale before hqx or PNG work. The
synchronous `Session.Frame` contract remains unchanged. The original-scale path
also benefits from duplicate detection; hqx relocation only affects enabled
magnification. No execution steps, guest callbacks, timer cadence, audio or save
formats were changed.

`FrameUpdate.Force` preserves start, resume and scale-change redraws. A full queue
does not consume the request: the emulator retries until it is accepted. The
writer sends the existing queued text prefix before an explicit redraw, ensuring
that a reconnecting page receives its lifecycle answer before its only static
picture. Ordinary and intermediate frames share the encoder comparison.

The new regression first failed on the baseline with five encoded pictures
instead of three changes. The production change passes the regression, scaled
PNG pixel checks, same-byte shape changes, forced redraws, queue-pressure tests,
ownership checks and lifecycle-message ordering. Existing session and retention
tests also pass.

`BenchmarkFrameEncoding` was compiled against baseline source via a Go overlay
and against the changed code, using the same committed server PGO profile and
benchmark. Each trial processes 100 authored 240 by 320 inputs, including initial
encoding; identical frames alternate independently allocated buffers. Three
interleaved baseline/candidate trials produced these median wall times per input:

| Path | Baseline | Changed | Difference |
| --- | ---: | ---: | ---: |
| Original, unchanged | 429.265 us | 9.125 us | -97.87% |
| Original, changing | 427.550 us | 429.336 us | +0.42% |
| hq4x, unchanged | 13.494 ms | 0.144 ms | -98.93% |
| hq4x, changing | 13.454 ms | 13.472 ms | +0.14% |

These are encoder/queue component costs, not whole-emulator CPU reductions.
They exclude snapshot capture, guest execution and network writes. The unchanged
case sends one picture, while the changing case changes a pixel every time.
Sub-percent differences in the changing cases do not establish a regression.
The raw results and baseline source overlay are retained under ignored
`build/cpu-reduction-20260923/`.

Both debug and release servers build. Required gates passed: `make test`
(including 227 Node tests), `make test-debug`, `go test -race ./internal/...`, and
`go vet ./...`. An initial gate invocation found mixed-package baseline source
snapshots in the ignored build directory; renaming those snapshots to `.go.txt`
excluded them from package discovery, after which the gates passed.

`web/acceptance/frame-delivery.mjs` passed all seven authored-fixture checks in
Chromium and WebKit: a static original-scale picture stays visible without new
PNGs, identical restart and resume pictures redraw, reconnection restores the
canvas, changed input paints arrive, MIDP scale requests keep native dimensions,
and WIPI frames switch among hq4x, original and hq2x. Browser artifacts are under:

- `var/acceptance/frame-delivery-chromium-1790123231365/`
- `var/acceptance/frame-delivery-webkit-1790123279605/`

An additional Chromium run used the same local KTF identity at original scale,
with fresh isolated saves. Over six seconds after first presentation it sent two
additional pictures. The last statistics window reported no new PNG, 23.51 guest
ticks per second and guest speed 1.00001. This proves that suppressing static
presentation did not stop that runtime's progress. The route and all seven
authored checks passed in
`var/acceptance/frame-delivery-chromium-1790123390353/`.

The user-reported saturated scene was not identified or reproduced. This change
removes demonstrated presentation work; it does not claim to solve a workload
whose CPU cost is dominated by guest instruction execution. Physical phones,
long gameplay routes and total process CPU savings remain outside this result.

## Frame copy remeasurement

Frame copies account for most allocated bytes in the measured scene, but very
little execution time. The evidence does not justify changing buffer ownership
to address CPU saturation. This follow-up measures the current implementation
with duplicate PNG suppression already applied; it changes no product code.

The same local KTF archive (`23919eb33365`) ran through the actual Go HTTP and
WebSocket session handler at original 240 by 320 resolution, with fresh saves.
A Go client drained messages in the same process. After three seconds of warmup,
each trial measured eight seconds. Debug and release each ran three times in
alternating order, on the same M1 and Go version recorded above, with the server
PGO profile. Initial load averages were 2.39, 1.86 and 1.84. The existing user
server was left running unchanged; this was not a completely idle machine.

An ignored Go source overlay times only allocation-and-copy expressions and
counts their calls and bytes. It preserves independent owned buffers. Process
CPU comes from `getrusage`, allocation totals from `runtime.MemStats`, and total
GC CPU from `/cpu/classes/gc/total:cpu-seconds`. Copy timings are elapsed time
inside those expressions, including possible scheduling and allocation assists;
they are not CPU-profile attribution. A separate allocation-profile run uses
a 4 KiB sampling interval and before/after profiles, outside the timing trials.
The known macOS CPU-profile attribution problem is therefore not used to rank
this cost.

Every measured interval contained 188 ordinary Host snapshots and 188 copies in
`flushLCDRegion`, which preserves the previous LCD image during a partial flush.
No changed intermediate frame was offered. Each copy moves 307,200 bytes and
allocates 311,296 heap bytes after allocator rounding. The two sites together
allocate 117,047,296 bytes per interval, about 14.6 MB/s. These are allocation
traffic totals, not retained memory or a leak. Medians follow:

| Quantity over eight seconds | Release | Debug |
| --- | ---: | ---: |
| Process CPU time | 6.749 s | 6.798 s |
| All allocated bytes | 117.372 MB | 121.613 MB |
| Host snapshot share of allocated bytes | 49.86% | 48.12% |
| Host plus partial-LCD copy share | 99.72% | 96.25% |
| Elapsed time inside Host copies | 2.114 ms | 2.001 ms |
| Elapsed time inside partial-LCD copies | 2.352 ms | 2.373 ms |
| Elapsed time inside both copy sites | 4.494 ms | 4.373 ms |
| Total GC CPU, including unrelated allocations | 8.013 ms | 7.754 ms |
| Total GC CPU / process CPU | 0.119% | 0.114% |

The separate allocation profile confirms both large allocation sites. Profile
serialization itself adds allocations, so the percentages in the table use the
timing runs' counters and allocation totals instead. The combined copy timing
and total GC CPU must not be added as a precise saving: some work can overlap,
and elapsed copy time and process CPU are different clocks. Both observations
nevertheless put the remaining cost far below the guest execution workload.

A separate fixed-work experiment makes 20,000 copies while retaining four
snapshots, with normal GC, repeated three times. At 240 by 320, allocation plus
copy costs a median 35.8 microseconds of process CPU per operation; copying into
an existing buffer costs 4.3 microseconds. At 320 by 480 these are 46.5 and 11.7
microseconds. The tight allocation loop triggers much more frequent GC than
the server and is not an end-to-end improvement measurement. At twenty copies
per second, the first number projects to approximately 0.072% of one core for
one copy site, not a cure for a saturated interpreter.

All server intervals kept guest speed near 1.00001 and approximately 23.5 ticks
per second. No new PNG was required after warmup: this was a stationary screen
whose guest still repeatedly flushed and executed code. The result covers this
KTF scene on this machine, not SKT/LGT gameplay, a high-resolution workload,
multiple simultaneous sessions, or the user's unspecified saturated scene.

The remeasurement is complete; retain the existing ownership contract. Revisit
only when a relevant workload shows substantial copy time or GC CPU, rather
than using the percentage of allocated bytes alone. Diagnostic overlays,
source hashes, seven final server runs, three primitive runs and raw results
remain in ignored `build/frame-copy-20260923/`; `results-v2.ndjson` and
`summary.json` contain the final server counters, while `results.ndjson`
contains the primitive results and the initial Host-only timing pass. Saves are
isolated under `var/savedata/frame-copy-*`.

Both diagnostic build profiles compiled and all seven final server probes
passed. With the overlay enabled, focused tests passed for defensive snapshots,
partial LCD preservation, raw snapshot ownership, duplicate encoding, forced
redraws and lifecycle ordering. `git diff --check` passed. The full regression
gates were not repeated for this measurement-only follow-up; their earlier
implementation results remain recorded above.

## Independent PR validation

Before publication, the presentation changes were applied independently to
`main` at `bb2294f`. `make test` (226 Node tests), `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` passed again. Both server
profiles built and reported their expected profile. The release server passed
all seven authored browser checks in WebKit and those checks plus the local
KTF original-resolution route in Chromium. The timing measurements above retain
their original checkout provenance; this final pass verifies the independent
PR's behavior without reinterpreting those timings as a new benchmark.
