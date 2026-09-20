# Gameplay QA, 2026-09-20

This investigation starts from revision `5b00ae17cdb867e86e2bfc6aac8093d872c4217d`
and the locally retained September 19 server/page reports. Five reported paths
remain distinct: SKT unpaced gameplay, SKT character-start failure, SKT tutorial
failure, LGT initial network agreement, and an LGT slowdown after selecting
the medium in-game speed. Passing one path does not establish the others.

## SKT separately installed resource archives

The character-start report ends in `System.arraycopy` with a null source.
The caller loads map data from a separate JAR when `getResourceAsStream`
does not find it in the primary JAR. The outer package contains that data.
Two runtime defects prevent access: the container loader drops every `.jar`
beside the primary payload, and the two-string `XFile` constructor treats the
entry name as a textual file mode. The latter interpretation was inferred by
the old implementation; the observed caller explicitly passes an archive name
and its internal resource path.

The loader now retains secondary JARs as installed files without adding their
classes to the primary class path. `XFile(String, String)` opens an entry in
the named archive, read-only. Integer-mode constructors keep their existing
save behavior. Archive reads reuse the bounded JAR parser, including entry
count, input/expanded size, duplicate and path checks. The entry may have a
single leading slash; missing, damaged and unsafe resources fail with
`IOException`. Original archive bytes are preserved.

An authored outer container packages the existing Canvas MIDlet fixture with
a newly generated resource JAR. Tests cover both path spellings, the entry
named `r` (which must not become a file mode), actual guest reads, rejected
writes, missing resources and unsafe paths. API tests check forwarding of both
constructor arguments.

For local archive SHA-256 prefix `1f0d7e81336a`, the page's final session inputs
were converted to press/release events at 16 ms ticks. Separate empty save roots
were used. The unchanged CLI fails at tick 368 with the reported null-array
stack. The corrected CLI completes the same route plus 300 observation ticks:
674 ticks, active state, no error, 62,371 non-black pixels, and a visible opening
scene with characters and dialogue. Local artifacts are
`build/qa-case1.route`, `build/qa-case1-{baseline,fixed}.log`, and
`build/qa-case1-{before,after}.png`. This proves entry past the reported
failure, not campaign completion or progress restoration.

## SKT refresh-driven speed

The reported fast loop repeatedly calls `Thread.yield` and `XDisplay.refresh`
without a gameplay sleep. Scaling the guest clock and `Thread.sleep` therefore
does not limit its iterations. Slowing Host ticks only reduces delivered frames;
the independent guest thread continues running at interpreter throughput.

`XDisplay.refresh` now paces its producer to at most 60 refreshes per guest
second. This is an emulator policy, not a measured handset refresh contract.
The existing speed clock scales that bound. Work already spent drawing counts
toward the interval, late frames do not accumulate credit, and waits observe
live speed changes. Rendering and presentation locks are not held while waiting.
Terminal states release waits without another Host tick. Frame snapshots and
coalesced Host presentation retain their existing ownership.

The regression test previously completed two 0.25x refresh intervals in
4.25 microseconds. It now requires at least 120 ms and also exercises changing
the live speed; a separate test covers shutdown without Host ticks. The existing
clock tests retain deterministic coverage of each speed multiplier.

A local replay of the reported archive's first 30 seconds of input then measures
three consecutive intervals in the same live runtime:

| Selected speed | Refresh calls | Wall interval | State |
| --- | ---: | ---: | --- |
| 1x | 175 | 3.011 s | active |
| 0.25x | 45 | 3.006 s | active |
| 2x | 311 | 3.002 s | active |

The counter measures guest calls, not browser frame delivery. Higher speeds
remain limited by execution cost. The local probe and results are
`build/qa_speed_probe.go`, `build/qa-speed-events.json`, and
`build/qa-speed-result.log`. This establishes that the selected speed reaches
the producer; perceived handset fidelity and real-phone rendering are separate.

## SKT tutorial reproduction

Archive SHA-256 prefix `d3e3b16cefd0` reproduces the reported exception with
292 press/release events from the retained page session. The isolated run
fails at tick 4,652, after approximately 75.6 seconds, at the same drawing
instruction: `b.try(Graphics)` bytecode offset 102 reads index 9 from a row
of length 5. Local artifacts are `build/qa-case2-events.json`,
`build/qa_tutorial_probe.go`, and `build/qa-tutorial-probe.log`.

A VM store observer tracks the shared `h.w` name table and menu state. The
ability-name loader installs one row of 24 entries. At 67.673 seconds the
item loader replaces it with 28 rows of five entries. At 75.594 seconds an
equipment operation returns to menu state 6; the subsequent tutorial overlay
reads the retained item table as ability names. Its lazy loader checks only
whether the shared table is null. The table is non-null, so the required
24-entry resource is not loaded.

Inspection of the main loop shows key handlers
queue input, the guest worker consumes it before updating/drawing, and `paint`
only supplies the graphics object. A concurrent Host paint replacing the name
table therefore does not explain this trace. Do not suppress the bounds
exception or fabricate a larger array.

An independent OpenJDK 8 replay restores the failing object graph and invokes
the original drawing method. It throws the same index-9 exception on its first
frame, through the same guest drawing methods. Clearing only the name table
allows 1,000 frames with repeated confirmation inputs and leaves a one-row,
24-entry ability table. This validates the drawing/data-flow diagnosis; the
graph comes from the emulator and does not prove a complete independent
handset input route. The desktop shim preserves image dimensions but does not
render pixels. Local artifacts are `build/qa-case2-failure-openjdk.log` and
`build/qa-case2-failure-reset-openjdk.log`.

The tutorial's phase-17 block clears image resources and changes to phase 5,
but does not clear the item-name cache. Ordinary menu exit paths do clear it.
A diagnostic observer clearing it at that transition completes the original
292-event route and 5,500 ticks, active and without an exception. The production
compatibility adapter now makes this same correction for the exact class-set
digest `d70497e3b4fae79a2b552c0cdad418494e01faa282a29a2eb8b7c4a702e45347`.
It replaces the redundant phase-17 assignment with a null name-table store;
the block still sets phase 5 before its next method call. Branch offsets,
exception regions, stack limits and unrelated methods remain unchanged.
Four appended constant-pool entries provide the field reference. Original
archive bytes remain unchanged, and any class-set change disables selection.
This is an observed revision's guest-cache workaround, not a new JVM rule.

The production runtime without the diagnostic mutation completes 5,000 ticks
of the same route, remains active, and shows the next combat tutorial dialogue
and its world scene. Its observer records the null store followed by the
ordinary ability loader installing 24 names. Artifacts are
`build/qa-tutorial-production.log` and `build/qa-case2-probe.png`.
This passes the reported crash point, not the whole tutorial or campaign.
The authored `TutorialCacheMIDlet.java` JAR reproduces the stale-table bounds
exception, then proves lazy reload after the adapter and preservation of the
final phase. Tests reject truncated classes, wrong fields and offsets, and
verify unrelated method bytecode and original archive ownership.

## LGT agreement reproduction

The retained log directory has no matching original report, so this path uses
fresh isolated runs of archive `ecb1bf8d2079`. Both local package variants
contain the same module, SHA-256 prefix `41ed310172ee`. The completed route
selects the default explicit No at the customer-information agreement,
shows network progress, and eventually shows a server-communication error.
`build/qa-case3-network.route` completes 7,018 ticks; its log records
`netConnect` from guest address `0x1355c` with callback `0x13070`, followed
by two `netClose` calls. The baseline status is `unsupported`.

None of the eight existing Thumb notification patterns matches this ARM
implementation. Static inspection finds a different stream framing: the
builder formats `SMSAGREE %s %s %c`, and its sender includes one terminating
NUL rather than padding the command to 100 bytes. The agreement response
handler accepts codes 2 and 3; the stream decoder reads a signed command byte
and a payload-length byte. The socket callback is `0x131ac`. These are local
investigation addresses, not proposed recognition keys. A diagnostic overlay
injects only this module's routing contract into the existing in-process socket
service. The same 7,018-tick route then writes 32 bytes, reads a two-byte
response, and closes the socket normally. The final frame is an empty
informational dialog with an OK button, rather than the baseline's communication
error. A second
fresh-save replay adds explicit confirmation and 300 observation ticks. It
completes 7,324 ticks and reaches the title screen without the network error;
artifacts are `build/qa-case3-null-complete.{route,log,png}`. This confirms the
No path with the diagnostic adapter. Those diagnostic runs did not establish
explicit Yes, restart behavior or production module recognition, and left the
informational dialog blank. The final implementation and acceptance follow.

### Implemented ARM agreement support

The production recognizer now joins the agreement initializer, writer and reply
handler through their virtual table, follows the constructed network object's
virtual table to the stream decoder, and validates dial/socket callback state
and transport links. It extracts the application and endpoint from the matched
initializer. Archive names, module hashes and fixed image addresses do not select
the adapter. Authored fixtures cover relocated code/data, mismatched links,
changed instruction windows and strings, ambiguous writers, truncated data and
address overflow. Unsupported compiler layouts retain the ordinary failure path.

The separate stream parser accepts only the current session's identity,
application and explicit N/Y choice, with a terminating NUL and a 64-byte bound.
It rejects oversized, trailing, unrelated and repeated commands. The response
uses code 2 for No or 3 for Yes, followed by the length and bytes of
`Handled locally. Nothing sent.` This replaces the diagnostic adapter's blank
dialog without claiming contact with the original service. Tests exercise every
split position, final-terminator handling, guest socket writes and short reads.

A release CLI built from the actual source, without diagnostic overlays, passes
both fresh-save choices. The final No route completes 7,324 ticks and the final
Yes route 7,330 ticks. Both reach the title screen after confirmation. The Yes
replay also captures the visibly selected Yes button and the readable local
notice. Traces show a 32-byte request, a two-byte response header and a separate
30-byte text read, then ordinary socket and network closure. Artifacts are
`build/qa-case3-arm-message.{log,png}` and
`build/qa-case3-arm-{yes-selected,message-dialog,final-yes}.png`, with the latter
route's log at `build/qa-case3-arm-final-yes.log`. Only isolated QA save roots
were used. A fresh runtime using the No route's saved data then completes
2,212 ticks and reaches the title without a dial or socket write. Both saved
files retain their SHA-256 hashes. Restart evidence is
`build/qa-case3-arm-restart.{route,log,png}`. This proves the reported initial
agreement and its immediate restart path, not campaign completion or any
external service functionality.

## Outstanding investigation

- The LGT medium-speed page report contains temporary drops, including 4.4 fps,
  57.1 ms per tick and 0.67x measured guest speed, followed by recovery toward
  0.96–0.98x. The server report is at 1.00x after recovery. These measurements
  do not identify the cause or establish a fix.

The final LGT session in the retained page report starts playing at
`2026-09-19T14:14:06.095Z`. Its archive SHA-256 is
`344e1285110a77089625b6d353c9766ecfd900763d7283ba0fd5eeeded07dcab`.
The page records two transient drops: at 14:14:16.425Z, 7.9 fps with 27.2 ms
ticks and measured speed 0.58x; at 14:14:56.997Z, 4.4 fps with 57.1 ms ticks
and 0.67x. Each recovers. This identifies the package and time windows to
replay; it does not yet distinguish guest loading work from a timer defect.

A full reading of the same page report finds a much more severe later window:
at 14:15:54.384Z the average tick is 228.6 ms and measured speed is 0.17x;
at 14:15:56.583Z it is 358.3 ms and 0.11x; at 14:15:59.143Z it is 635.6 ms
and 0.06x. It recovers to 0.97x by 14:16:05.207Z, with further intermittent
drops afterward. These later measurements, rather than only the early loading
dips, are the remaining investigation's target.

The module hash is
`a1467fcb4a07f8ed763b43c74fda41c9ce44566539cc21f51e1bbcaae296b696`.
A debug runtime replay with a fresh isolated save root delivers all 1,326
recorded key edges over 160 seconds without uncaught callbacks. It does not
reproduce the severe window. Screen captures show the fixed timestamps reach
a main-menu confirmation instead of the intended speed option; this is not a
successful reproduction of the setting change. The run's logs, captures and
guest profiles are `build/qa-case4-full-*`, with input events at
`build/qa-case4-events.json`. A separate manual probe reaches the speed option.
The first manual navigation leaves the system menu open: the first CLR removes
its selection, and another CLR returns to play. Shorter holds alone do not
change that behavior. Subsequent navigation verifies each screen and uses two
ticks held, two released, then 30 observation ticks. Menu-only timing results
do not establish gameplay behavior after selecting medium.

The manual probe subsequently returns to the visible combat tutorial after
selecting high. The small selected glyph was initially misread as medium;
inspection of its pixel strokes identifies it as high. A 300-tick real-time
measurement completes in 7.685 seconds,
with 7.339 seconds of guest time, 6.663 seconds busy, and a 57.684 ms maximum
tick. It retires 796,673,754 instructions. This is not the reported severe
slowdown or a medium-setting acceptance. Captures are
`build/qa-case4-combat-high-option.png` and
`build/qa-case4-combat-high.png`. The probe is `build/qa_lgt_manual.go` and
uses only `var/savedata/qa-20260920/medium-manual/`, copied from the preceding
QA replay's save root. An earlier adjacent-setting measurement is not labeled
high: its exact displayed value was not established. Subsequent navigation
passes the tutorial and enters the first combat stage. The full menu has
Status, Skill, Item, Record and System tabs, unlike the tutorial's system-only
menu. First-use Item and Record explanations must be confirmed before tab
navigation proceeds. Medium is then explicitly selected and verified in
`build/qa-case4-battle-medium-option.png`. After confirmation and leaving the
menu, the battlefield remains visible with its minimap, enemies and weather.
A 300-tick measurement takes 11.914 seconds wall time and advances 11.968
seconds guest time (approximately 1.00x), with 7.667 seconds busy, a 53.876 ms
maximum tick, and 912,392,079 guest instructions. The resulting screen is
`build/qa-case4-battle-medium.png`; console measurements are transcribed in
`build/qa-case4-manual-evidence.txt`. This verifies that specific setting change
in the first combat stage without the reported transient. It does not explain
or resolve the severe original episode. After closing the probe, its ordinary
save files are copied to the isolated `medium-battle-checkpoint` QA save root,
with hashes in `build/qa-case4-battle-save-hashes.json`, for subsequent runs.

The macOS CPU profile again attributes most samples to
`runtime.pthread_cond_signal`; the previously measured profiler limitation in
[ARM execution history](history/armcore.md#implementation-where-a-clets-time-goes-and-the-quantum-that-is-not-one-number)
applies. Do not infer a scheduler optimization from this profile alone. Guest
instruction samples and measured tick times remain the useful evidence.

### LGT WebSocket comparison

An isolated debug `webhost.Server` and real WebSocket connection repeat the
setting change using a copy of the first-stage QA save. This exercises the
shared session adapter, audio collector, PNG encoder, socket writer, key
dispatch and statistics used by the page. The existing user server is not
restarted or changed. The probe has its own ephemeral listener, game directory
under `var/games/.qa-web-case4/`, and saves under
`var/savedata/qa-20260920/medium-web/`.

The captured option screens explicitly establish high, then medium. Each
setting has a 12-second observation interval in the same first battle, with
enemies, weather and damage visible. Five complete two-second statistics
windows after the initial mixed window give:

| In-game setting | Measured guest speed | Mean tick cost | Delivered frame rate |
| --- | --- | --- | --- |
| High | 0.969–0.984x | 20.46–21.33 ms | 16.27–16.96 fps |
| Medium | 0.994–1.001x | 26.25–31.63 ms | 12.39–12.52 fps |

The transition window itself reports 1.013x and 21.943 ms per tick. No severe
drop appears immediately after selection or during the following interval.
Medium's lower presentation rate here accompanies an on-time guest clock;
it is distinct from the original report's 0.06x clock shortfall. The tick-cost
measurement brackets `session.Tick`; frame conversion, PNG encoding and socket
writing are outside that timed call. They can still compete for host resources,
so this timing boundary alone does not exclude host contention.

Artifacts are `build/qa_lgt_web.go`, `build/qa-case4-web.log`,
`build/qa-case4-web-comparison.json`, and the `high-selected`, `high-end`,
`medium-selected`, and `medium-end` PNGs under `build/qa-case4-web-*`.
The probe is stopped after observation. These results establish a second
unsuccessful reproduction through the actual host path; they do not identify
or fix the historical transient. The original report lacks a code revision
and a slow-tick execution profile. The currently running user service reports
only version `dev` and started after that report, so it cannot establish the
historical executable either. Further diagnosis needs a reproduced slow state
or additional evidence from that episode rather than a speculative timing change.

## Validation

After both SKT changes, `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` pass. The Node gate reports
219 passing tests. `git diff --check` passes. No server restart, publication,
or real-phone check is part of this run. Original archives and user saves are
unchanged; probes use `var/savedata/qa-20260920/`.

After connecting the ARM recognizer and adding the informational response,
`make test`, `make test-debug`, `go test -race ./internal/...`, and `go vet ./...`
all pass again. The Node gate retains 219 passing tests. Logs are
`build/qa-arm-{test,test-debug,race,vet}.log`. `git diff --check` passes.

After the tutorial adapter, all four gates pass again: `make test`,
`make test-debug`, `go test -race ./internal/...`, and `go vet ./...`.
Logs are `build/qa-tutorial-{test,test-debug,race,vet}.log`.
The optional original-archive fingerprint test also passes with an absolute
`WFEATURE_SKT_TUTORIAL_ARCHIVE` path. `git diff --check` passes.
