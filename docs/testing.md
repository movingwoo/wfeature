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

The earlier WebKit Blob failure did not recur in bounded instrumented routes;
its cause remains unresolved. Preserve lifecycle phase, browser version, Blob
properties, origin/navigation, and server frame evidence on recurrence. See
[WebKit observations](history/testing.md#webkit-webkit-frame-decoding-investigation).

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
Patch tests additionally reconstruct consecutive updates pixel for pixel at
original scale and hq2x/hq3x/hq4x, verify legacy connections and forced complete
pictures through the WebSocket handler, and bound browser decoding and recovery.
`TestFramePatchBandwidth` and `BenchmarkFramePatchBandwidth` compare complete
frames with patches over an authored moving sprite; this is not real-game traffic.

The opt-in browser route uses repository-authored SKT and LGT fixtures:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
  node web/acceptance/frame-delivery.mjs build/release/wfeature-server chromium
```

Use `webkit` for the second engine. `WFEATURE_FRAME_ARCHIVE` may name a local
archive under `var/games` for an additional six-second original-scale probe.
The runner uses isolated game and save directories and records results under
`var/acceptance`. It exercises static presentation, restart, park/resume,
reconnection, changed input frames, lossless patch composition against a forced
complete server PNG, and WIPI scale changes. A static picture
without new PNGs is expected; the runtime must keep ticking. Measured scope and
results are in [the CPU investigation](cpu-saturation-investigation-2026-09-23.md).

## Reading old validation

[Testing history](history/testing.md) keeps detailed experiments and release
checks. [Coverage audits](history/coverage.md) retain the 2026-09-12 source
snapshot; their missing-method tables are historical, not a current task list.
Current platform overviews identify supported behavior and remaining limits.
Documentation consolidation itself is not a new runtime acceptance run.
