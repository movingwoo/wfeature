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
This comparison does not establish gameplay, server-restart restoration, what a
load does to a save written after the checkpoint, or audible continuity: the
restored client is given a copy of the store as it was at the boundary.
Authored older-module tests also cover its initialized client and class links
without re-entering the module.

KTF session-checkpoint tests restore the execution record over a store that
changed after the capture, and make a displaced worker write during its abort.
The later save must still be the save, and the worker's write must land only in
the old client's isolated memory store
(`TestSessionCheckpointLeavesLaterSavesAndIsolatesDiscardedWrites`). A save
that cannot be read, cancellation and a busy client leave the original usable
and the store as it was. A separate subprocess restores the envelope over the
saves on disk after the source workers have terminated; the same cross-profile
binary variable applies. Backend container tests reject wrong archives,
lengths, checksums, variants, an earlier envelope version and a non-zero
reserved word, and keep ordinary `.wfs` backups separate. Record tests
exercise typed-allocation amplification, depth/value limits, invalid UTF-8,
missing/duplicate/unknown fields, wrong scalar types and failed-decode isolation.

Shared-session tests use the newly authored archive generated by
`internal/testfixture/ktf.go`. It runs the ordinary descriptor/JAR/AOT startup
path and increments a guest startup counter. Capture/load restores that counter
without another startup, along with pause state, held keys and pointer, and
key-repeat phase, and leaves a save written after the capture as it is. A
child process reads the reserved slot after the source closes and restores
through the public shared API. Set
`WFEATURE_SHARED_CHECKPOINT_RESTORE_BINARY` to a compiled `internal/session` test
binary to exercise this path between debug and release. Malformed shared settings
and input are refused before changing the live session or any save.
Slot tests verify restart persistence, archive isolation, exclusion from loose
save export, survival across ordinary writes, and that a file under an earlier
format's name is reported, refused and never touched. Sparse oversized files,
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
replaying startup, over a save written after the source server stopped, which
must still be there. Set
`WFEATURE_WEB_CHECKPOINT_RESTORE_BINARY` to a compiled `internal/webhost` test
binary to test the two build profiles together. Both directions passed for
Java and native packages after the directory repair below. Paused-slot tests
check that live and startup browser restoration resume through the Host lifecycle.
Node tests
exercise delayed bitmap decoding across reset, audio definitions, stale request
cancellation, failed-load isolation, explicit disk restoration and button state.

CLI tests drive `run` through live JSON checkpoint commands and through a fresh
process with `-quickload`. They check saved input, no repeated guest startup,
saves left as they were, missing-slot refusal and save-claim release. Set
`WFEATURE_CLI_CHECKPOINT_RESTORE_BINARY` to a compiled `cmd/cli` test binary for
`TestCheckpointCLIProcess` and `TestCheckpointNativeCLIProcess`;
debug-to-release and release-to-debug both passed. A paused CLI restore reports
a stalled step and can resume through `park`, without retaining a false ending.
The authored native fixture in `internal/testfixture/ktf_native.go` runs its
entry, factory, event handler and scheduled frames through generated ARM code.
Its checkpoint tests compare subsequent frames, instruction counts, input,
elapsed time and ordinary saves without rerunning startup. Device tests preserve
a decoded image whose guest pixels changed, pending events, timers, resume
callbacks and sound, and reopen the files the record names from the store
(`TestNativeCheckpointPreservesDevicesAndReopensFiles`). Detached staging
advances by an hour before adoption to check rebasing; continued runtime
records and saves then match the source exactly. Malformed allocator, mapping,
pixel, callback, file and device records are refused before adoption. A store
that refuses a pending write or cannot be read, busy execution and cancellation
preserve the original runtime and files.
`TestCheckpointNativeRejectsMalformedAudioAndLeavesSaves` sends modified
native audio records through the public shared-session load API with a valid
envelope checksum. Repeating clips and negative playback origins must fail
before changing the live session, input or disk saves. The original session must
remain runnable, and a subsequent valid load must leave the saves as they are.
The existing device round trip above covers supported one-shot audio playback.

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

The page checks, the two gameplay routes and the cost measurements in this
section are dated 2026-10-01 and were taken when a load still put back the
saves a slot carried. The gameplay routes and the cost measurements were not
run again after that changed; what was run again, a browser check on authored
storage titles among it, is recorded under
[Quick load and the saves](#quick-load-and-the-saves).

The final ordinary/debug gates, `go test -race ./internal/...`, `go vet ./...`
and 278 Node tests passed on 2026-10-01. `CGO_ENABLED=0 make dist` produced all
five configured targets in an isolated output directory. File locks have runtime
evidence on macOS; those builds establish Windows/Linux compilation only.
The fallback for a location that cannot hold a lock (2026-10-03) has authored
tests for a linked owner directory, an injected failure of each fallback class
at both steps (making the reserved directory, locking the file), a read-only
root, contention that must not fall back, and a tampered reserved path. They
ran on macOS; `go vet` with `GOOS=windows` and `GOOS=linux` establishes that the
platform error lists compile, not that those systems answer with them.
Six alternating baseline/current measurements at one Go CPU, 2,000 rounds each,
gave median instruction costs of 33.27/33.27 ns (`cc2c96d671cd`), 41.60/41.69 ns
(`aa3fcba4598b`, +0.22%) and 419.50/426.94 ns (`1d5831e42a8a`, +1.77%). Instruction
counts and guest times match. The module sample lasts about 60 ms and is more
sensitive to fixed overhead; these are loading-route measurements, not browser
FPS or a bound on all gameplay costs.

### Checkpoint keypad controls

The 2026-10-02 UI revision removed picker/settings checkpoint buttons. Quick save
and quick load use keypad assignments; the Type1–3 defaults now include them in
top-row columns 5 and 6, while Type4 leaves them unassigned. Node tests cover
default row geometry, legacy preservation, reset, per-type persistence, duplicate
assignments, editing unavailable cells, immediate requests, pending-operation
exclusion, inline failure feedback and retry. The Node suite passes 270 tests.

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

<a id="lgt-checkpoints"></a>

### LGT checkpoints

LGT quick save/load reuses the envelope, slot and Host commands above; what is
new is the platform record and the guest-thread continuation, and those are
what the tests below are about.

**The property every continuation test asks for is the same one**: a session
restored from a checkpoint, over the saves as they were when it was taken,
agrees with the session it was taken from after every tick that follows — in
the frame it presents, its flush count, its retired instructions and its guest
clock — and in guest memory and ordinary saves at the end. A matching still
frame at the moment of the load establishes none of that. What a load does when
the saves have changed since is a separate property, tested in
[Quick load and the saves](#quick-load-and-the-saves) below.

Authored tests in `internal/platform/lgt`:

- `checkpoint_state_test.go` runs the authored Clet with a timer that counts,
  draws, presents and arms itself again. It covers restoration into a fresh
  client, a record that survives a round trip unchanged with every table
  populated, a live load that leaves the saves as they are and cuts the
  displaced session off from them without closing it, thirty-two malformed
  records each refused
  before a save is touched, capture away from a boundary, the two behaviors
  the feature changed — timer order and the writable-open copy — and the
  generator, whose sequence is checked against the standard library's for
  5,000 draws over six seeds and restored at seven points in it.
- `TestCheckpointAccountsForEveryRuntimeField` lists every field of the client,
  the Java runtime, a guest thread and the structures they hold, and says for
  each whether it is recorded, rebuilt, the Host's, fixed at a boundary or a
  diagnostic. **A field added without a line there fails the test**, which is
  the only thing that keeps "the checkpoint covers the runtime" true after the
  next table is added.
- `checkpoint_worker_test.go` gives the Clet a Java runtime and guest threads
  whose `run` is assembled in the test, so where a thread is parked is chosen:
  inside a sleep, behind a contended lock, in a two-level `wait`, at the end of
  its budget, inside a static initializer and a C-library function call that
  each outlast a slice, inside an armed try region, and before its first slice.
  A thread beneath a class declaration thunk is refused by name and restored
  once it has left it. Thirty-five Java records that do not add up are refused,
  each for the reason the case names. A restored session saved again before it
  runs writes the record it was restored from.
- Three checks have tables of their own, because what they refuse is what a
  panic boundary does not contain. `TestCheckpointRefusesAClassChainThatNeverEnds`
  closes a superclass chain through recorded links and through the name a
  platform class's superclass is looked up under.
  `TestCheckpointRefusesAnObjectTheAllocatorDidNotHandOut` gives the
  collector's table an address, a size and a shared block the allocator never
  handed out. `TestCheckpointRefusesALayoutThatCannotBeWalked` closes a layout
  chain through its own classes and through the specification's hierarchy, and
  states sizes no class has. Each also accepts the well-formed table next to
  the ones it refuses.
- `TestCheckpointSurvivesADamagedRecord` changes one or two values anywhere in
  a valid record — a number to a boundary, a flag, a list's length, an optional
  part — and requires the result to be refused or to run ten bounded ticks,
  never to panic or hang. A case that does not return is reported by its seed
  rather than as a timeout, and every failing seed is listed, not the first.
  It runs 250 seeded cases per variant as an ordinary gate;
  `WFEATURE_LGT_CHECKPOINT_DAMAGE=100000` ran 200,000 on 2026-10-02 with no
  failure, of which about 45% were accepted. **The same count has to be run
  again whenever the record changes shape**, because a seed then names a
  different change: a run after the generator's state was reworked found the
  one case earlier runs had not, a dial marked as accepted by a local service
  in a record that had no such service, which is now refused. It was run again
  on 2026-10-03, after the record stopped carrying file bytes and path lists:
  200,000 cases, no failure, 45,404 of the Clet's 100,000 and 46,003 of the
  Java title's accepted.
  The test changes values, not relationships, so a record in which two tables
  disagree is beyond it; those are what the three table tests above are for.
- `TestCheckpointRestoresJavaThreadsInAnotherProcess` restores the Java
  fixture's parked threads in a second process and compares what they counted,
  the instruction total, the guest clock and the memory image six ticks later.

Host tests use `testfixture.LGTCheckpointArchive`, a Clet authored in ARM words
that starts through the ordinary loader. `internal/session/checkpoint_lgt_test.go`
covers held input, pause, speed, the continuation and refusals through the
shared API, and a slot restored by another process. `internal/webhost` runs the
same WebSocket subprocess route the KTF fixtures do (`TestCheckpointLGTServerSubprocess`),
and `cmd/cli` runs live `quicksave`/`quickload` commands and a `-quickload`
restart (`TestCheckpointLGTCLIProcess`).

**Local archives.** The opt-in probe runs every archive in the two local LGT
directories, takes a checkpoint, restores it into a second session with saves
of its own and ticks both, sending the same keys to each:

```sh
WFEATURE_LGT_CHECKPOINT=1 go test -run TestLocalLGTCheckpointsContinueIdentically -v ./internal/platform/lgt
```

`WFEATURE_LGT_CHECKPOINT_WARM` and `_ROUNDS` set the tick the checkpoint is
taken at and how long the two are compared; `_MATCH` and `_DIR` narrow the set.
`WFEATURE_LGT_CHECKPOINT_SWEEP=1` instead takes a checkpoint after every round
and prepares a session from each, which is what finds a boundary that is
refused.

On 2026-10-02 the two directories held 130 archives: 105 Clets, 23 AOT Java
titles, and two files no LGT loader claims. A title whose first run only
installs itself is started again over the saves that run left.

| run | Clets | Java titles |
| --- | ---: | ---: |
| checkpoint at tick 150, compared for 100 | 105 of 105 identical | 21 identical, 2 ended together |
| checkpoint at tick 300, compared for 200 | 105 of 105 identical | 21 of 21 identical |
| checkpoint at tick 400, compared for 250 | 105 of 105 identical | 21 of 21 identical |
| a checkpoint at each of 260 boundaries from tick 20 | none refused | none refused while running |

The two Java archives missing from the later rows are one title packaged twice;
the probe's keys take it to its own exit before tick 300, and after that a
checkpoint is refused because the title has ended. No other boundary was
refused. An earlier inventory over 300 ticks found every guest thread at every
boundary one call deep: in `Thread.sleep` (19 titles, two of them at times
holding a lock), `Object.wait` (3 titles without a deadline and 1 with),
`Thread.yield` (5), or waiting for a first slice. Nothing in the library was seen parked at
its budget, inside an initializer or behind a contended lock at a boundary;
those are covered by the authored threads above and by nothing else.

**Another process.** For 128 of the 130 files, `run -ticks 500` and
`run -ticks 300 -quicksave` followed by `run -quickload -ticks 200` in a new
process end on the same screen digest. The other two are the files no loader
claims. Slots were 0.82 to 6.3 MB at tick 300, median 1.96 MB.

**Gameplay.** Two archives were driven by local routes into play — `a947f46eebb2`,
a Clet, 1,790 ticks to a field scene, and `735a579d82ac`, an AOT Java title,
5,915 ticks to an in-game dialogue. A fixed sequence of six key taps over 170
ticks then gives the same per-command screen digests and flush counts in an
uninterrupted run, in a fresh process started with `-quickload`, and after a
live `quickload` that follows other input. Thirteen and seven distinct pictures
occur in the sequence. Both pass with a debug binary saving and a release
binary loading, and the reverse. Slots were 3.7 MB and 10.7 MB.

**Browser.** Chromium and WebKit at 390 px with touch, against the release and
the debug server, used the page's own picker and keypad: quick save and quick
load were assigned to cells, the authored Clet's startup word returned to its
saved value after a live load while its frame counter kept running, a slot with
one byte changed was refused inline and loaded on retry, and the slot loaded
after a real server restart and ordinary startup. A real Clet (`3cc7a9b4cb15`)
and a real Java title (`a30bbe008b5e`) were then saved, loaded live and loaded
again after a restart, each answered with fresh frames. All four pairs of
engine and profile passed. Neither browser reported a page error from the page;
WebKit reported the game-list request that the script's own reload cut off, in
two of three release runs, and that is counted apart. Both real titles were on a
notice screen, so that part establishes the controls and the transport, not
gameplay; the route runs above are the gameplay evidence.

**Cost while running.** What the work adds to a tick is the order timers fire
in and a generator that keeps its own state; everything else runs at a save or
a load. `TestLGTLoadCostProbe` was built from `4ae66a7` and from this work and
run over the two gameplay routes above, 2,400 and 6,400 ticks, as six
alternating pairs on one Go CPU with nothing else running. Both binaries
retired the same number of instructions on each route, 3,420,023,111 and
487,333,258. The median of the six per-pair ratios, this work over `4ae66a7`,
was 0.996 for the Clet, with pairs from 0.994 to 1.006, and 1.008 for the Java
title, with pairs from 0.998 to 1.013. Two earlier runs on a less quiet machine
gave 0.995 and 0.998 for the Clet and 1.005 twice for the Java title.

The Clet is inside the noise. The Java route was above one each time, by under
one percent, so that was looked at rather than rounded away: in a CPU profile
of the route in both binaries, neither the generator nor the timer ordering is
among the functions that take half a percent of the time or more. Neither can
account for the difference. An effect of that size is what the placement of
code in a rebuilt binary produces, and this comparison cannot separate that
from a cost; it does bound whichever it is at about one percent.

**Gates.** On the final tree `make test` (including the 280 Node tests),
`make test-debug`, `go test -race ./internal/...`, `go vet ./...`, `gofmt` and
`git diff --check` pass, and the CLI and the server build without cgo for
`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` and
`windows/amd64`.

**After a load stopped replacing saves.** The measurements in this section
dated 2026-10-02 were taken when a load still put back the saves a slot
carried. The continuation probe, the sweep, the damage count and the process
comparison were run again on 2026-10-03 and are recorded under
[Quick load and the saves](#quick-load-and-the-saves), with a browser check on
authored storage titles. The gameplay routes, the browser check on real titles
and the cost comparison were not run again.

These are bounded routes over one local library on macOS. They are not a
physical phone, a listening test, or every title's every scene. Audio state is
restored through the shared output record and was not listened to.

### Checkpoint adoption blocker

A controlled concurrent read exposed a defect on 2026-10-01 in the
whole-folder replacement a quick load then made. After the replacement wrote
its intent but before its first rename, an independent reader treated the
active transaction as an abandoned one and removed staging and intent.
Replacement then moved the live directory to its backup and failed to install
the missing staging. Its rollback found no intent, so the original bytes
remained in the backup while live saves were missing. This failed three
ordinary reproductions and a debug run using authored temporary files. HTTP
save listing and export reached the same recovery path.

The repair gave every directory-store operation the same filesystem lock,
including independent objects, complete loose-tree walks, imports and slot
access. That lock is what every store operation still takes. The replacement
writer itself was removed once a load stopped replacing saves, and the tests
that held it at its rename boundaries went with it.

`TestSaveDirectoryAliasesShareTransaction` covers relative paths, linked parent
directories, and case/Unicode aliases when supported by the executing filesystem.
`TestSaveTransactionExcludesAnotherProcess` checks a child process waiting for an
active transaction.
`TestSaveClaimsExcludeSessionsButAllowReadsAndReleaseOnExit` checks the longer
Host claim separately: competing sessions are refused, reads remain possible,
and an abruptly exited child leaves no stale claim.
`TestSaveHTTPReadsNeverSplitABatch` runs the HTTP listing and export while
batches are written and requires each read to see both keys of one batch; a
recovery record that cannot be read is reported as an error rather than as an
empty save tree. Separate server instances cannot claim the same live save
directory.

Backend test binaries cross-compiled without cgo for the five distribution
targets when the lock was added. Those builds do not establish runtime
file-lock behavior on Windows or Linux.

### Save folder snapshot and recovery tests

Backend snapshot tests preserve empty and dotted keys, reject path conflicts,
oversized input and symlinked files, and check independent in-memory reads and
batches. `TestSaveRecoveryOfLeftoversFromEarlierBuilds` builds, from literal
bytes on disk, every state the removed replacement writer could leave when its
process stopped, and for each of them runs every store operation: a prepared
or half-swapped replacement rolls back, a completed one keeps the live folder
and its `previous`, a record that cannot be read refuses the operation, and
staging or a `previous` with no record is left where it is. These tests
exercise recovery on the executing host, not a physical power loss.

<a id="quick-load-and-the-saves"></a>

### Quick load and the saves

The rule under test is the one in
[architecture](architecture.md#quick-load-and-ordinary-saves): a checkpoint is
execution state, a load leaves every save where it is, the restored game reads
and writes the saves as they are then, and the one thing either step writes is
what the running game had issued and the host had not yet stored. The tests are
on the [storage fixtures](#storage-fixtures-and-the-boundary-inventory) below,
which act on a save when they are sent a key.

**Shared session.** `internal/session/quickload_saves_test.go` drives eight
titles through `Start`, `SendKey`, `Tick`, `CaptureCheckpoint`,
`LoadCheckpoint` and `RestoreCheckpoint`: KTF Java/AOT, the older KTF module,
the native package with and without a frame callback, the LGT Clet, and the LGT
Java title on each of its three surfaces. The store is the directory store a
Host uses, in a temporary folder, behind a wrapper that counts writes and fails
a write or the read of one key on request. Every claim about the disk compares
the whole save root before and after, file for file and byte for byte, the
reserved directory included.

| Test | What must hold on every title |
| --- | --- |
| `TestQuickLoadKeepsTheGamesSaves` | Quick save, a save in the game, load. The later save is byte-identical and the load made no store write; the guest's progress word is the quick save's; the game reads the later save; a patch lands on it and keeps its other part. Run over the running game, where the displaced runtime must be cut off, and into a session that never ran the title. |
| `TestQuickLoadTwiceLeavesTheSameSaves` | Two loads in a row write nothing and change nothing. |
| `TestQuickSaveStoresOnlyWhatTheGameIssued` | With a part written through a file still open, a quick save makes exactly one store write where that part was pending (LGT files, the native package) and none where the platform had stored it or it is still in an unflushed stream. A second quick save writes nothing. |
| `TestQuickLoadStoresTheRunningGamesIssuedWritesFirst` | The same pending part is on disk after a load, and the restored game reads it. |
| `TestRefusedQuickStepsKeepTheRunningGamesWrites` | A store that refuses writes refuses the quick save or the load with `ErrCheckpointSaveWrite`; nothing on disk changed; the same session is running; its pending write is finished by the game's next key; the step succeeds once the store takes writes. |
| `TestRefusedQuickLoadRevertsNothing` | A save the load has to read and cannot refuses it with `ErrCheckpointSaveRead`, with no store write; the running game saves again and the same load is then accepted. |
| `TestQuickLoadAfterTheGameDeletedItsSave` | A delete made after the quick save stays: the restored game finds no save, and saving makes one. |
| `TestQuickLoadReopensWhatTheGameHadOpenOnTheSaveAsItIsNow` | A file kept open across the quick save is open after the load on the save as it is: over a later save, over a shorter one (its position is kept, so the write lands behind what is there), and over a deleted one (the object is empty and the write fills the gap before its position with zeros). In each case the load itself wrote nothing. |
| `TestQuickLoadRefusesAnotherArchivesCheckpoint` | A checkpoint of another archive of the same variant is refused with either archive's bytes. |
| `TestQuickLoadValidatesWithoutTheSaves` | No load logs that validation asked the save store for anything. |

The native interface has no delete and a record store has no truncating open
or position, so those cases are absent where the surface has none. Copied onto
the tree before the change, with the store wrapper given the snapshot methods
that tree required, `TestQuickLoadKeepsTheGamesSaves` fails on every title for
the reason the change exists: the save file is back to the first save and a
`previous` directory has appeared.

**Platform packages.** Each platform's own rules are tested where they live:
`native_quickload_test.go` (pending and refused names, the single attempt at an
ordinary boundary, reopening from the store and the package, an emptied object,
one budget for every object and one read per key however many records name
it), `session_quickload_test.go` and `storage_rebind_test.go` in
`internal/platform/ktf` (one store per name after a load, listings, unreadable
and undecodable saves, the budget and one read per key, a field-accounting test
for the storage tables, the running client's lock given back when a store
panics), and `checkpoint_storage_test.go` in `internal/platform/lgt` (the
buffer stored at a quick step, the refusal of a buffer behind its save under
any spelling of its name, two objects of one database with refused stores,
path lists read again, file streams and `DataBase` objects filled from the
store, the adapter deriving its disk state again). `TestRebuildReader*` in
`internal/backend` cover the reader a load reads through: one read per key, a
shared budget, and a refused and remembered write.

One rule has a test on each platform because it is easy to get wrong in the
direction that loses a save. An object whose open asked for an empty file
takes at most what it had written; an object whose open only made a file that
was not there takes the whole file as it is.
`TestCheckpointGivesAHandleThatMadeItsFileTheFileAsItIsNow` (LGT),
`TestSessionQuickLoadGivesAFileThatMadeItsSaveTheSaveAsItIsNow` (KTF `File`)
and the "made by its open" case of
`TestNativeQuickLoadNeverHandsAnEmptiedObjectANewerTail` write a save through a
handle that created it, take a quick save, save more behind it, load, write the
front again and require what was saved behind it to still be there.

The rules were also removed one at a time to see that a test fails: 23 changes
in the native package, 37 in LGT, about 38 in the descriptor runtime and 19 in
the hosts. Every one failed a test except two. One LGT change removed a
statement that turned out to be redundant, and the statement was deleted. One
host change dropped the shared session's archive-identity check, which changes
nothing observable because each platform's commit makes the same check.

**Web host and CLI.** `internal/webhost/quickload_saves_test.go` runs a
write-through title and a title with pending writes through the runner's own
commands: the save folder is byte-identical across a quick load and across a
start from the slot; the three refusals a person can act on read as the page's
sentences while the cause, naming the file, is in the log
(`TestQuickStepRefusalsAreWordedForThePage`, where a directory put in place of
the save file makes the real store fail to write, and a save file without read
permission makes it fail to read); an
earlier-format slot is offered, refused with its sentence and never touched,
and a new slot is written beside it; an earlier build's `previous` directory is
reported in the log and left byte-identical; a start from the slot that is
certain to be refused leaves the running game running and its saves claimed;
saves laid out as the released 0.5.1 leaves them, with dotted keys, nested
directories, an empty file and a leftover temporary file, stay byte-identical
and only the reserved directory is added; and the scenario passes over a real
WebSocket connection with the runner ticking on its own clock.
`cmd/cli/checkpoint_saves_test.go` runs `run -serve` and `run -quickload`: a
save made after the quick save is still the save after a live load and after a
start in another command, and an earlier-format slot ends the command with
status 1 and a reason that names the file.

**Local archives, 2026-10-03.** The opt-in probes were run with the tree before
the change and with this one. The continuation probes give the restored session
a copy of the store as it was at the boundary, so what they establish is that
storage a record no longer carries is rebuilt to the state the source had, on
real titles; what a load does to a later save is established by the authored
tests above and by the real-title scenario below, not by these probes.

| Probe | Before the change | After it |
| --- | --- | --- |
| LGT `TestLocalLGTCheckpointsContinueIdentically`, 130 files | 126 identical, 2 ended together, 2 not LGT games | the same answer for every file |
| LGT sweep, a checkpoint at each of 150 boundaries | not run again | none refused in 126; the title packaged twice that ends itself is refused once it has ended, which the tree before the change answers the same way |
| KTF `TestLocalKTFClientCheckpoint`, three older modules | 3 | 3 |
| KTF, the 38 files of the `ktf` directory | 34; three differ in the saves they went on to write; one is not a descriptor archive | 37; the same one is not a descriptor archive |
| KTF, the 259 files of the 1.2 directory | 253 | 253, the same files, in a quiet run; 251 in a run beside other work |
| LGT, `run -ticks 500` against `run -ticks 300 -quicksave` and `run -quickload -ticks 200` in a new process, 130 files | 128 end on the same screen digest; 2 are not LGT games | the same |

The six files of the 1.2 directory that do not pass are two titles that end
themselves inside the probe, one whose boundary is refused because a clip's
owner still awaits collection, one that outruns the probe's five-minute limit,
and two files that are not KTF descriptor archives. A boundary refused for a
clip awaiting collection, or for a record over the allocation limit, depends on
when the collector ran: in other runs of each tree two or three further files
were refused once and passed the next time, and the run of this tree that
shared the machine with the other measurements passed 251.

Slots in the process comparison were 0.82 to 6.3 MB at tick 300, median
1.96 MB, as before: an LGT record of a title on its opening screens holds
little that was a file.

Without a quick save the two trees behave alike. A first run of 500 ticks and a
second run of 300 over the saves the first left, through the shared `run`
command, give the same exit status, screen digest and save bytes in both trees
for all 130 LGT files and for the 41 KTF files of the `ktf` directory and the
older modules. Those KTF runs repeat exactly when the same binary is run twice,
so the comparison has no noise floor to read through.

**Upgrade rehearsal, 2026-10-03.** The released 0.5.1 command-line binary was
built from its tag and made the saves: each archive run for 500 ticks with the
fire key pressed at seven fixed ticks, into a save root that did not exist
before. Each tree those runs left was then copied, and the copies were read by
0.5.1 again, by the tree before this change and by this one. No save a person
made was read.

- LGT, 130 files: 102 titles wrote saves, 26 wrote none in those ticks and 2
  are not LGT games. For all 102, a second run of 300 ticks by this tree ends
  on the same final frame and leaves the same save bytes as the same run by
  0.5.1 and by the tree before the change. Over the same saves a quick save at
  tick 300, a start from it in a new process and 200 more ticks end on the
  screen digest of an uninterrupted 500 ticks, and a start from the slot with
  no tick leaves every save file as it was.
- KTF, 300 files: 166 titles wrote saves, 129 wrote none, 4 are not archives
  the released command runs and 1 outran the time limit. Through the shared
  `run` command, which steps a manual clock and repeats exactly, the tree
  before the change and this one end a second run of 300 ticks on the same
  screen digest and leave the same save bytes for all 166; 291 of the 304
  files 0.5.1 had made were byte-identical afterwards and the titles rewrote
  the other 13. A quick save at tick 300 and a start from it in a new process
  ended on the digest of an uninterrupted run for all 162 titles whose quick
  save was accepted, and a start from the slot with no tick changed no save
  file.
- The released command line has no `run`, so 0.5.1 itself was compared through
  `runktf`, which is paced by the wall clock. There two runs of one binary over
  the same saves can end on different frames, and five titles write different
  bytes on every run of any of the three binaries, which was checked by
  running each binary more than once. With that set aside, 100 titles agree
  with 0.5.1 on the final frame and on every save byte, 57 differ in the final
  frame alone and none differs in a way one binary shows and another does not.
- Four KTF quick saves were refused. Two were at a boundary where a clip's
  owner still awaited collection. Two were on titles that the released 0.5.1
  cannot run a second time either: they keep a save inside a directory and then
  ask whether the directory exists, and the store answers a read of a
  directory with an error, which ends the session. That defect is in the
  released build and is not changed here.

**Records of no bytes, 2026-10-07.** `TestSaveRecordsKeepAnEmptyRecordApartFromADeletedOne`
pins the encoded bytes of a list holding empty, deleted and filled records and
requires decoding to give each back as it was. The KTF Java `DataBase`, the
KTF WIPI C record table and SKT RMS each write an empty record, read it in a
later session (counted, selectable, and on SKT a null `getRecord` and a
`setRecord` that fills it), and fail on the tree before the change. Saves made
from nothing by the released 0.5.1 over the local library — 300 KTF and 106
SKT files, 500 ticks with the fire key tapped seven times — hold 183 record
lists and not one record of no bytes, so no second run over them reads
differently. Run with this change, the 59 KTF titles that keep Java databases
write one such record: one title inserts an empty record 0 before its data,
which 0.5.1 stored as deleted. Two `runktf` runs of it over one save folder,
twice on each tree, all end normally on the same scene of play, the change's
saves holding the empty record and the earlier tree's the deleted one; the
final frames differ between any two runs of either tree, which `runktf`'s
wall-clock pacing does to this title.

**A Java database deleted while held, 2026-10-07.** In a running session the
next open of a deleted name takes the store a `DataBase` object kept; a test
writes through the old and the new object and reads one record list in both
and in a later session (it fails without the change), and a checkpoint taken
in that state comes back with the object on the name's one store. Twenty local
KTF archives name `deleteDataBase`. Through `run -serve`, forty rounds of
twenty ticks with the fire key tapped every third round, a first and a second
run over one save folder end on the same screen digest and leave the same save
bytes before and after the change for all twenty. A probe build that reported
the path showed two of them delete a database they held and open it again, and
take the kept store at that open.

**A linked save folder, 2026-10-07.** `TestCheckpointSlotBesideALinkedOwner`
stores and loads a slot over an owner directory that is a link, and requires
the slot to be in the reserved directory beside the link, the link to stay a
link and the directory it leads to to hold the saves alone.
`TestQuickSaveAndLoadOnALinkedSaveFolder` runs the scenario through the web
host's runner on both host titles: quick save, save in the game, quick load,
and the folder the link leads to is byte-identical across the load and is what
the restored game reads. On the tree before the change both fail: the quick
save is refused with "save replacement path is not a directory".

**Real titles through the scenario, 2026-10-03.** No input script takes a real
title to its save menu, so the save made in the game is one the title makes by
itself. `run -serve` ran each archive in rounds of twenty ticks, with the fire
key tapped in every third round, and watched the save folder for a change. A
second run then took a quick save one round before that change, ran until the
folder changed again after the quick save had returned, loaded the quick save
and read the folder. What the quick save itself stored, a pending write, does
not count as the title's later save.

| | LGT, 130 files | KTF, 300 files |
| --- | ---: | ---: |
| titles that wrote after the quick save | 84 | 104 |
| this tree: the write is still on disk after the load | 72 | 102 |
| the tree before the change: the folder is back as it was at the quick save | 72 | 99 |
| ended by itself during the rounds, on both trees | 12 | 2 |
| the quick save was refused at that boundary | 0 | none on this tree, 3 on the tree before |
| wrote only before a quick save could be taken, or only what the quick save stored | 24 | 68 |
| wrote nothing in forty rounds | 22 | 128 |

The refusals on KTF are a clip whose owner still awaits collection, or a record
over the allocation limit. They fall on different titles in the two trees and
on different runs of one tree: an earlier run refused two on each. After the
load every title that kept its write ran six more rounds without an error.

**Browser, 2026-10-03.** Chromium and WebKit at 390 px with touch, against the
release and the debug server, drove the page's own picker and keypad on three
authored storage titles and read the save folder from disk after every step.
Quick save and quick load were stored as two keypad cells the way the editor
stores them. On a KTF Java title and an LGT Clet: a save, quick save, a save
that leaves the file shorter, quick load — the status line said, in the
page's wording, that the game went back to the quick-save moment and that
saves were not reverted, and the save folder was byte-identical across the
load — then a key that patches the save in place, which landed in the shorter
file; the server was stopped and started,
the game started from the picker, and quick load again left the folder
byte-identical. On a native package with an earlier build's slot beside its
saves: quick load was offered, was refused in the error style with the
sentence for an earlier format, the old file was byte-identical afterwards, a
new quick save was written beside it and loaded. All four pairs of engine and
profile passed. The page logged the refused load's reason, as it does for any
refusal, and WebKit logged the socket the script's own server stop closed;
nothing else was reported. These are authored titles: the browser check
establishes the controls, the wording and the transport, not a real game's
save.

### Storage fixtures and the boundary inventory

The three checkpoint fixtures make no storage call, so a Host test could only
imitate a game's save with a store write of its own. Authored storage fixtures
now exist for every checkpoint variant. Each starts through the ordinary loader
and acts once per key press on one save of two four-byte parts: write both,
patch the first in place, read both back, delete, hold the save open after
writing the first part, open it with truncation without writing, and finish
through what was kept.

| Fixture | Variant | Surface | Where a held write waits |
| --- | --- | --- | --- |
| `KTFSaveArchive` | KTF Java/AOT | Java `File` | nowhere: stored at the call; the `File` keeps a private copy and its cursor |
| `KTFModuleSaveArchive` | KTF older module | Java `File` | the same |
| `KTFNativeSaveArchive` | KTF native, with a frame callback | file interface | in the written table until the frame ends |
| `KTFNativeSaveArchiveWithoutFrame` | KTF native, no frame callback | file interface | in the written table until a file or the session is closed |
| `LGTSaveArchive` | LGT Clet | WIPI-C files | in the handle buffer until the handle is closed |
| `LGTJavaSaveArchive` | LGT AOT Java | `File`, a stream on a `File`, or `DataBase`, chosen by a guest word | in the handle buffer; in the stream until a flush; nowhere for `DataBase` |

The native interface has no delete, and a record store has no truncating open;
those actions are absent where the surface has none. Each fixture has tests of
its own actions against a store, and is captured and restored once to show
that it is a valid checkpoint subject.

Four backend pieces a load is built from have tests of their own: the sentinels
`ErrCheckpointSaveWrite`, `ErrCheckpointSaveRead` and `ErrCheckpointLegacy`;
`NewDetachedSaveStore`, the placeholder a record is validated against, which
answers every read as absent, refuses writes and counts calls; `ReadSaveLimit`,
which checks a directory entry's size before reading it; and
`DisplacedGeneration`, which reports a `previous` directory without reading it.
The journal's recovery is also tested from literal on-disk states, without the
writer that produces them.

Opt-in probes count what the platform holds for a title's storage at tick
boundaries, in in-memory stores:

```sh
WFEATURE_LGT_STORAGE_INVENTORY=1 go test -run TestLocalLGTStorageInventory -v ./internal/platform/lgt
WFEATURE_KTF_STORAGE_INVENTORY=1 go test -run 'TestLocalKTFStorageInventory|TestKTFNativeStorageInventory' -v ./internal/platform/ktf
```

`WFEATURE_STORAGE_INVENTORY_OUT` names a directory outside the repository for
one record per archive. On 2026-10-03 the local libraries gave the following,
with no input sent, so these are boot and notice screens rather than play.

LGT, 200 ticks of warm-up and 150 boundaries each, 130 files: 105 Clets and 23
AOT Java titles ran; two files are not LGT games; two Java archives are one
title packaged twice and exit at round 13.

| At a boundary | Archives |
| --- | --- |
| an open file handle | 3 Clets, at every boundary |
| a writable handle | 2 |
| a dirty handle, a write only the buffer holds | 1 Clet, at every boundary |
| a handle whose buffer differs from the store | the same 1; none that is clean |
| an open `DataBase`, a file stream, a sink with bytes | 0 |
| two handles on one key, a pending truncation, a stale path list | 0 |

KTF descriptor runtime, 300 rounds of warm-up and 100 boundaries each, 38 files
in the local KTF directory: 36 ran, all of the Java/AOT variant.

| At a boundary | Archives |
| --- | --- |
| C file catalog entries | 17, up to 13 entries |
| live Java `File` payloads | 9, up to 29 |
| Java database catalog entries | 3 |
| a record database catalog entry | 1 |
| an open C file handle | 2, one of them at every boundary |
| files this session wrote | 2 |
| a `File` payload whose buffer differs from the store | 1, at every boundary |
| a stale catalog entry or ledger, a pending truncation | 0 |

The authored native package held nothing open or pending. No real native
package is in the local library.

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

An LGT module states what each of its classes extends, and can state a chain
that comes back to itself. `java_class_chain_test.go` plants such records in
guest memory — a class over itself, two over each other, and a class registered
under the root of the platform's hierarchy above a platform class — and
requires preparation to fail with the class named, the type check to return,
and every chain left behind to end. The layout's check by name has a table of
shapes it accepts and refuses, each then walked. Before the 2026-10-02 fix the
first two prepared without error and the type check did not return. One more
test requires the specification's hierarchy table to end, which several walks
rely on and nothing else checks.

A save key whose path is a directory reads as no entry (2026-10-07).
`TestSaveReadOfADirectoryReportsMissing` asks it of the directory and memory
stores, through the ordinary and the bounded read, before and after a save
below it, and requires a write to it to fail without touching that save. The
KTF `FileSystem.exists` and LGT `MC_fsIsExist` tests ask about a folder over
three and two runs of one save directory; all three fail on the tree before
the fix. Two local KTF titles that keep their saves in a folder ended their
second `runktf` with `read save fs/<folder>: ... is a directory` before the
fix and run to the end after it. The tests that used a directory as their
unreadable entry now use a file without permissions or an injected failure.
Two quick load tests that refused a load over a directory where the save file
had been now require the load to take it as a save that is not there: the
object over it is empty and the tree is unchanged.

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

## Grid keypad

The grid model and geometry suites cover preset topology, every legacy cell's
migration, action policies, connected shapes, holes, conflicts, settings
uniqueness, split/clear/undo, per-type reset, reload, corrupted records, unknown
versions and failed writes. The recorded old preset table remains independent
of the new defaults. Removed slider assertions are replaced by these behavior
checks and actual rendered-browser coverage.
`keypad-refinement.test.mjs` independently records the version-1 seven-column
schema and checks paired-column migration, assigned and cleared shapes, occupied
holes, activation policies, independent empty halves, split/reload, failed writes,
per-type recovery and a concurrent old tab writing its separate storage slot.
`keypad-editor.test.mjs` drives the actual editor event handlers with DOM stubs.
It reproduces the stale multiple-selection mode after merging, then checks
immediate assignment to the selected merged button, conflict recovery and
settings protection. These checks do not render a browser; the browser route
also includes the merge-then-assign interaction without a manual mode change.
Explicit font metrics additionally check that single-cell labels fit, including
two-character names, that resizing or merging restores available font size, and
that changing rapid-fire text refits without accumulating shrink. The browser
route checks actual range widths and no-wrap styles across its viewport sizes;
the metric stubs do not establish real font rendering.

Run the browser route with the existing Playwright installation:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
  node web/acceptance/keypad-grid.mjs build/release/wfeature-server chromium
```

Use `webkit` for the second engine and a debug server executable for the other
profile. The runner uses the embedded client and an authored SKT canvas archive,
with isolated runtime directories. It creates rectangle, L, T and ring buttons
through the editor, checks the hole's separate key and the native hit targets,
and observes actual session key messages. It covers mouse drag selection,
touch taps, keyboard/pointer shared ownership, duplicate pressed feedback,
focused activation, cancellation, blur, local-action exclusion, reload and full
storage. Chromium additionally supplies two actual browser touch contacts through
its automation protocol. These contacts are not physical-device evidence.

Geometry checks cover 240 by 320, 320 by 480, 320 by 568, 390 by 844, 390 by 700,
768 by 1024, 844 by 390, 1280 by 900 and 1280 by 390 px windows, plus explicit
inputs for all four safe-area insets. They wait for resized button positions
before checking full width, contact with the game wrapper, bottom alignment,
overflow, the last cell's hit target and unchanged saved coordinates. The route
also loads a version-1 grid and checks paired columns and retained source storage.
The route stops its server and
reloads the cached page to establish offline shell availability; it does not
claim offline game execution. WebKit's forced-offline API is avoided for the
[already recorded navigation limitation](pwa-acceptance.md).

The fourteen-column route passes with the embedded release server in Chromium
and WebKit and with the debug server in Chromium. All nine viewports and explicit
safe-area inputs pass. The shortest screen also checks that the first and last
start-menu controls remain reachable by scrolling inside the game area. Actual
range widths prove the single-cell labels fit; rapid-fire mode changes are
checked after the authored game starts.

The checkpoint route uses the new grid positions while retaining Java/SGS pixel,
input-epoch, ordinary-save and reconnect assertions. The two-version PWA route
edits a legacy cell or coarse grid, retains the old records, and verifies the
migrated key and stored source after the new shell replaces the old one. Release-facing
instructions are in the root README; the technical contract is in
[the web host reference](../web/README.md#grid-keypad).

The [2026-10-08 validation record](history/testing.md#keypad-grid-validation-2026-10-08)
separates completed browser checks from the pending physical-phone and real-game
play observations. It retains the initial Chromium update cache failure
and the subsequent fix. Service-worker regression tests reproduce a late legacy
navigation and overlapping future workers; both failed before the repair and pass
afterward. Both engines pass the version-34 and version-35 updates to version 41.
The route waits for cache retirement after navigation, because the worker's
fetch lifetime can extend beyond the completed response.

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

### SKT execution checkpoints

[The SKT checkpoint record](skt-checkpoints.md) describes the implementation and
its supported boundaries. Focused suites are `TestThreadCheckpoint`,
`TestCheckpointNativeWait`, `TestJavaCheckpoint`, `TestJavaPlatformCheckpoint`,
`TestScriptCheckpoint` and the shared `TestCheckpointSKTJava`/
`TestCheckpointScript`. Authored worker fixtures verify continuation and monitor
ownership; platform tests verify current ordinary-save reconstruction and
refused-load isolation. Subprocess tests accept
`WFEATURE_SHARED_CHECKPOINT_RESTORE_BINARY` to exercise both debug/release
directions using separately compiled test executables.

CLI and webhost checkpoint suites now include both SKT variants. The rendered
client route is `web/acceptance/checkpoint-skt.mjs`; it builds on the existing
Playwright acceptance setup. Chromium and WebKit exercise actual keypad
placement/save/load, restored pixels and input epochs, corrupt-slot feedback,
and unchanged ordinary save hashes. It also checks that the first restored
picture follows the reset and that a later speed change survives page reload,
including the resumed metadata, control and saved preference. Real-archive probes
remain opt-in through `TestLocalSKTCheckpoint`, with separate temporary saves and anonymous digest
labels. No local game data is bundled in these tests.

The [2026-10-07 review](quicksave-review-2026-10-07.md) records eight reproduced
defects and their follow-up fixes. Maintained regressions now cover:

- SKT stale or independently dirty file aliases, deletion, failed writes and
  opens before pending writes reach disk; refusal preserves every buffer and
  occurs before any checkpoint flush.
- LGT File/DataBase collisions and unreadable path lists; refusal preserves
  issued writes and the live Clet remains usable after a checkpoint read fault.
- SKT RMS close after a later deletion, actual subsequent record mutations and
  retrying failed writes on close.
- Linked and real owner paths contending for claims and transactions in one or
  separate processes, including process-only fallback and missing owner creation.
- Java/SGS audio catchup limits, late reset arrival at the writer boundary and
  current speed on both attached and parked session resumes.

### Independent audio ownership

The [2026-10-08 ownership repair](audio-ownership.md) adds backend ownership and
checkpoint regressions, JSON/binary capability and cancellation tests, and an
authored SGS Host route through dropped sound, quick load and reconnect.
`web/acceptance/audio-ownership.mjs` renders simultaneous clips in Chromium and
WebKit, cancels one and compares the remaining output with a separate reference.
The existing coarse-clock timing route also passes. These checks do not establish
physical-phone listening or the remaining source/controller contracts.

### Guest audio gain

`internal/backend/audio_gain_test.go` covers device and clip multiplication,
active mute without position loss, raw checkpoint output, malformed levels,
late sink attachment and legacy scaling. KTF VM, LGT ARM Java/C and the authored
SKT `audio-gain.jar` exercise their native adapters; the SKT fixture also restores
both clip levels through a complete checkpoint. Host tests verify gain before
reconstructed sources in both protocols, dropped mute recovery and older pages.

`web/audio-gain.test.mjs` checks the actual source routes and pending automation
after a clock reset. `web/acceptance/audio-gain.mjs` compares rendered MIDI and
nonperiodic PCM against uninterrupted references in Chromium and WebKit,
through zero, quarter and full gain, with sample tolerance 0.000001.

`internal/backend/audio_transient_test.go` checks overlapping transient sounds,
preserved reusable clips, cleanup after encoded restoration and refused loads.
`internal/platform/skt/media_tone_test.go` runs 512 sequential tones, checks
guest argument errors and capability filters, and restores an active Java tone
through the complete SKT checkpoint path before verifying its reclamation.

`internal/platform/skt/media_volume_test.go` and the authored `media-volume.jar`
exercise MIDP control interfaces from Java, stable identity and array isolation,
invalid-state queries, live MIDI/PCM level/mute changes, duplicate-event
suppression, reentrant callbacks and concurrent native setters/getters. A full
checkpoint preserves the muted control/listener graph and a retained closed
control; five malformed graph variants are refused before activation.

### Clip pause and envelope continuation

`internal/backend/audio_pause_test.go` checks cursor/repeat retention, frozen
note/drum ages and PCM frames through encoded checkpoints, replay isolation,
active and paused voice budgets, eviction order and sample-budget refusal.
Invalid clocks and malformed state are refused before output is cancelled.
Platform tests exercise KTF Java, LGT Java/C, SKT WIPI/MIDP and SKVM through
their existing authored fixtures. They check remaining gates, repeat changes,
explicit Stop versus Pause, rewind, paused quick load, deallocate reentrancy
and SKVM wait interruption. Platform checkpoint guards use paused guest time
even after long pauses; KTF checks include speed changes.

The web Host negotiates `sound=resume` through actual WebSocket handlers and
tests envelope ages in JSON and binary alongside older capabilities. The page
tests route both formats and reject truncated resume operations.
`web/acceptance/audio-resume.mjs` renders melodic, PCM and percussion resumes
against independent native Web Audio references in Chromium and WebKit. Both
engines match within 0.000001, remain silent during pause and preserve a peer
source. Melodic phase is deliberately fresh in the reference. The gain,
ownership and coarse-clock browser checks continue to pass; see
[pause verification](audio-ownership.md#verification).


### MIDP loops, progress and event delivery

`internal/backend/audio_playback_test.go` covers exact finite counts across
coarse ticks, replay after completion, frozen media time, finite rewind and
encoded progress restoration. Invalid remaining counts, positions and
completion counters are refused. KTF, LGT and SKT checkpoint guards cap finite
catch-up work at the remaining passes and use the frozen clock while paused.

The authored `media-lifecycle.jar` exercises the Player interface and receives
boxed Long event payloads from Java. The 400 ms score emits exactly two attacks
when count 2 is advanced by 1.3 seconds. Its event history is STARTED(0),
END_OF_MEDIA(400000), STARTED(0), END_OF_MEDIA(400000); state becomes PREFETCHED
and a subsequent start replays from zero. Tests verify stopped media time,
remaining gates, duplicate suppression, closed guards and callback-generated
work waiting for the next `RunPending` pass. Full checkpoint restoration after
one completed pass preserves the remaining pass, pending events, payloads and
listener/control/Player identities without duplicate delivery. Standard event
names are compared by `==` in the Java callback. A previously delivered name
also retains its literal identity immediately after restoration, before queued
callbacks run.

The JVM `StringIdentity` Java fixture and Go regressions cover repeated and
cross-class literal loads, class-file/native constants, distinct constructed
strings, `String.intern()` receiver adoption and encoded heap round trips.
Concurrent interning, count/text budgets and malformed pool records have
separate tests. A refused restore preserves the destination pool; a successful
restore replaces its bootstrap objects. Heap version 2 carries the canonical
roots, including empty strings and embedded NUL characters.

Malformed queued-event and Player graphs are refused before activation,
including an open Player omitted from the registry despite remaining reachable
from Java. Concurrent event producers and delivery are exercised under the
race detector; removing a listener preserves its already queued notifications.

Host/native concurrency regressions run media-time queries, stop/start/rewind
and SKVM pause/resume against advancing clocks. The earlier per-Player exclusion
workaround has been replaced by [whole audio transaction serialization](#global-audio-score-order)
so progress queries can merge peer scores without racing a transition. An
authored Java loop worker also verifies that successful SKVM media replacement
interrupts its old wait, while invalid replacement leaves the previous sound
playing.


### Live channel controls

`internal/backend/audio_controls_test.go` verifies current controller values
before resumed note output, original per-note programs, raw velocity and
remaining envelope age. It covers reconnect, encoded checkpoint restoration
and pause/resume with two owners and overlapping notes. The regression fails
under the former replay of stale per-note controllers.

`web/audio-controls.test.mjs` covers live volume/expression/pan, zero recovery,
unmodified envelopes, shared channel nodes, retained release/drum tails, owner
and channel isolation, PCM independence, activation, interrupted/same-time
ramps and clock-reanchor cancellation. Existing timing/resume tests now inspect
the velocity envelope before channel gain; their scheduling and age assertions
remain intact.

`web/acceptance/audio-controls.mjs` renders seven scenarios in Chromium and
WebKit and compares them against independent native graphs, with a maximum
sample error below 0.000001 and explicit RMS/stereo-energy assertions. Both
engines pass at 5.960464477539063e-8 maximum error. The resume, gain, ownership
and twelve-note 90 ms-clock acceptance scripts pass alongside it. The latest
`make test` (321 Node tests), `make test-debug`, race, vet and both cgo-free
server builds pass; local logs are under `build/sound-controls/`.

### MIDI sustain and parameters

`internal/backend/audio_midi_state_test.go` covers pedal-held released keys,
zero-velocity note-on, CC120/121/123, one-shot drum lifetime, RPN/NRPN partial
and null selection, cents carry, bounds and reset preservation. Reconnect,
encoded checkpoint and paused resume restore parameters before any resumed
note and reproduce its released-key flag under the held pedal. Invalid fields,
released drums, released keys without sustain and old component versions are
refused before output. A legacy sink regression observes each note's owner
controls at note-on; it fails when independent-owner replay ordering is used
for a flattened sink.

`internal/webhost/audio_midi_state_test.go` exercises JSON and binary output,
including reconstruction, checkpoint restore, peer isolation and legacy
flattening. The maximum-state replay fixture now varies sensitivity, both
selector pairs and selected family for every channel of 256 sound owners.
It verifies every state independently and returns to the ordinary collection
limit after reconstruction. Focused Host tests also pass under the race detector.

`web/audio-midi-state.test.mjs` has nine state/scheduling regressions.
`web/acceptance/audio-midi-state.mjs` separately renders nine authored scenarios
against native Web Audio graphs. Its expected actions identify source releases,
cuts and frequencies directly rather than interpreting MIDI controls. Both
graphs apply actions at the same suspended rendering boundaries; pre-scheduling
the reference's future frequency changes caused a comparison discrepancy before
the reset and was corrected without increasing the 0.000001 tolerance.
Chromium and WebKit pass with maximum sample error 5.960464477539063e-8.
The five existing acceptance scripts (controls, resume, gain, ownership and
timing) also pass in both engines.

All four required gates, both cgo-free server builds and 330 Node tests pass.
Six real archive scenes retain MIDI/PCM output and five successful restores;
the known SKT WIPI Display-owner refusal is unchanged. Local results and logs:
`build/sound-midi-state/`. These checks do not substitute for phone listening.

### SMAF decoder bounds

`internal/audio/smaf/limits_test.go` uses small per-call budgets to verify every
exact boundary and one-over refusal without large hostile allocations. It covers
all sequence dialects, overwritten sequences, nested/unknown chunks, ignored
records, atmosphere event expansion, repeated PCM and framed SysEx. Public
`Parse`/`Play` reject an excessive compressed declaration after a valid track,
while ordinary malformed tails retain the previous behavior. Disabling the
shared charging function makes nine regression groups fail.

The separate allocation/overflow regressions reproduce an oversized single-leaf
Huffman expansion, setup length overflow, track/gate products, accumulated time
and atmosphere tails. They pass after repair, including the exact uint32 time
boundary. `internal/backend/audio_decode_test.go` proves `Audio.Load` reports
the typed resource error without changing active output or consuming a handle.

`FuzzBoundedSMAF` seeds mobile, handset, PCM, compressed and Softbank data,
length overflow and aggregate failures. Fresh small budgets bound each call;
the target checks no partial result after a hard failure, output ceilings and
input immutability. A 30-second two-worker run passed 1,204,551 executions.
All four required gates and both cgo-free server profiles pass. The page did not
change in this decoder slice; the 330 Node tests remain green.

Before/after decoder binaries produced identical membership, complete event and
PCM/SysEx hashes and metrics for 11,175 unique local sounds. The comparison exits
successfully; the scanner separately exposes 1,014 unchanged ZIP-read exclusions.
See [the audit](history/audio.md#bounded-decoding) for measured maxima and coverage.
Local logs, frozen binaries and anonymous comparison: `build/sound-bounds/`.

### HPS exclusive framing

`internal/audio/smaf/hps_exclusive_test.go` builds complete authored SMAF files
around [the HPS exclusive contract](audio.md#hps-exclusive-messages). A sized
sequence message must keep its payload and final `F7`, drop its size byte, and
leave the following note's onset and deadline where the score puts them. One-byte
sizes of 12, 128 and 255 carry an interior `F7`, an `FF F0` pair, zero runs and
an `F7` just before the terminator, all of which must stay payload; decoding
must not modify the input. Seven malformed tails (short header, missing size,
zero size, truncated payload, missing terminator, terminator outside the size,
interior `F7` only) must keep the messages and notes before them, the next
setup chunk, the next sequence chunk and the next track. HPS setup chunks must
emit back-to-back messages at time zero ahead of the sequence, and their bytes
and events are charged to the shared budgets at the exact boundaries. Softbank
sizes 0, 128 and 255 and a 128-byte Mobile MIDI-length message pin the other two
dialects to their previous output. `TestExclusiveDelimitingDiffersByDialect`
now states the sized handset form instead of the lengthless one it assumed.

Run against the pre-repair `score.go` and `player.go` through a Go overlay,
five of the six new tests and the corrected dialect case fail; the other-dialect
test passes on both, as it should. All pass after the repair in normal, debug
and race modes, alongside the unchanged decoder-bound and fuzz seeds, and a
30-second `FuzzBoundedSMAF` run passed 5,572,442 executions.

The corpus comparison freezes the pre-repair decoder and rescans the same
11,175 carrier-local records. Notes, waves and per-trigger PCM sizes are equal
for every record; the event-count change equals the added setup messages
exactly; each of the 2,676 changed outputs has a recorded reason; and in all
7,763 HPS sequences the decoded duration, note, control and exclusive counts
and timeline hash match an independent size-aware cursor that ends at EOS. The
measured effects are in [the audit](history/audio.md#sounds-without-audible-events).
The scanner keeps the known 1,014 KTF member-read exclusions and exits 2. The
local KTF acceptance probe still plays all 895 SMAF files in 38 archives. The
same eight real scenes as [the fractional PCM check](#fractional-pcm-reconstruction)
start, change speed, capture, restore and tick again with no failure and empty
stderr; their PCM totals (12 routed waves, 23,268 samples) are unchanged. Logs,
the overlay and the anonymous comparison are under `build/sound-hps-exclusive/`
and `build/sound-empty-classification/`.

On 2026-10-09 the complete working tree also passes `make test` (427 Node
tests), `make test-debug`, `go test -race ./internal/...`, `go vet ./...` and
both `CGO_ENABLED=0` server profiles in an environment that permits binding
ports, so the server, appserver, launcher, webhost and wsproto socket tests run
too. Those were the tests the earlier sound sections record as blocked.

### Loaded audio admission

`internal/backend/audio_resources_test.go` covers combined entry/byte limits
through decoded load, authored load and transient playback. It verifies exact
boundaries, atomic refusal, no consumed handle, retained reusable stop/pause/end
capacity, all five transient removal routes, repeated payload accounting,
encoded restore, malformed active keys and concurrent admission. The full MIDI
key-space fixture admits 2,048 distinct keys at exactly 4,097 reserved entries
and 147,648 bytes, while output remains capped at 24 audible voices.

Before repair, 250 repeated passes of an unmatched key retained 501 active
entries; velocity-zero note-on added another held record, and mutating the
caller's nested buffers changed loaded/transient data. All three regressions
now pass. The new 11-test suite also passes under the race detector. All four
required gates, both cgo-free server builds and 330 Node tests pass. The decoder
also compiles for Linux/386 with cgo disabled.

Six real archive scenes retain MIDI/PCM output, five successful checkpoint
restores and the existing SKT WIPI Display-owner refusal, without execution or
input errors. Logs: `build/sound-bounds/loaded-*.log`; anonymous smoke results:
`build/sound-bounds/runtime-loaded.jsonl`. The page and session protocol did not
change in this admission slice.

### Checkpoint Display and audio catch-up

The authored `audio-gain.jar` covers both WIPI Display factories before MIDP
owner binding, shared factory identity, capture/prepare/commit, preserved clip
aliases and active gains. A later MIDP lookup binds the existing Display to
its first MIDlet; a second owner is refused. Invalid Display/owner roots are
refused at capture and typed restore without displacing the live session.

`TestJavaCheckpointAcceptsConsumedAudioPrefix` independently exercises capture
and detached preparation after 1,200 note-on/off pairs have already completed.
Both failed under the old whole-score note-work product, despite only a future
End remaining. The shared `AudioCatchup` tests cover exact/over-limit event,
payload and ordered key-work budgets, multiple owners, consumed/future events,
repeat boundaries, cheap same-key retriggers, finite passes, frozen pauses,
zero-length scores, maximum clocks and immutable malformed-state refusals.
Positive note searches and stable release compaction have separate adversarial
coverage; the Java/SGS refusal tests retain unchanged ordinary saves and a
running source. Existing KTF/LGT pause and finite-loop regressions pass too.

All four required gates, both cgo-free server builds and 330 Node tests pass.
All six previously sampled real scenes now restore and execute the following
tick, with MIDI/PCM output and no execution/input errors; stderr is empty.
Logs and anonymous results: `build/sound-catchup/`. Earlier Display-only
evidence is in `build/sound-wipi-display/`. No page or wire format changed.

### KTF speed-change continuity

Authored KTF clock/audio tests reproduce the old backward clock at 0.5x and
the skipped remaining gate/repeat at 2x. Seven regressions now cover elapsed
guest time/date continuity, subsequent rate, active/prepared aliases, one Host
clock sample, fractional rounding, clamping, atomic range refusal and paused
audio. A serialized client checkpoint restores on another epoch after a
three-hour detached activation delay; saved output age and remaining finite
passes continue correctly without changing the source.

Shared-session tests cover eight initial/setter rates, normalized stored
options, accepted guest/public/input-repeat agreement, the 600 ms held-key
delay and successful checkpoint capture after a refused rate. Before repair,
raw and nonfinite shared rates invalidated checkpoints or advanced input repeat
at a different rate from the guest. A separate backend regression reproduces
NaN corrupting `SpeedClock` and verifies its normalized future progression.

All four required gates, both cgo-free server builds and 330 Node tests pass.
Two real KTF scenes each run through 0.5x, 2x and 1x, then restore and execute
another tick; MIDI/PCM remains present and stderr is empty. The six measured
before/after clock deltas are forward and under 9 microseconds. Logs, baseline
failures and anonymous results: `build/sound-speed/`. No page, transport or
checkpoint schema changed; physical listening remains separate acceptance.

### SKT WIPI playback listeners

The authored `WIPIListenerMIDlet` JAR and 17 Go tests cover deferred integer
events, no-op and unsupported clips, each natural repeat, listener replacement
and null removal, reentrant restart, a full queue and concurrent transition,
replacement and delivery. The API test resolves the real installed interface
method and all eight constant fields; the original declaration and missing
START delivery each fail their baseline regression.

Review found that a listener implementing an interface derived from
`PlayListener` was incorrectly refused. The JAR now uses such an interface,
and both setter and checkpoint paths reproduce the original refusal. The
JVM `InterfaceIdentity` Java fixture covers transitive/diamond/inherited
interfaces through `instanceof`, casts and interface calls. Native metadata
tests cover cycles, distinct-type and edge work bounds, missing branches,
nulls and the existing array rules. Diagnostic baselines and subsequent gates
are under `build/sound-interface/`. All four gates, both cgo-free profiles,
330 Node tests and the six-scene restore/continuation probe pass again after
this JVM repair; stderr is empty.

Full checkpoints retain paused gates, pending recipients and unreferenced
playing owners. A record with the previous native Player kind and reference
layout restores and continues, then can be captured again. Eighteen malformed
ownership/event cases are refused without changing source audio, pending
events or ordinary saves. Forced Go collection plus JVM heap refresh verifies
that active/paused owners survive, idle owners are collectible, and completed
or stopped owners are retained only until their queued callback is delivered
and the guest releases its event history.

`make test` (330 Node tests), `make test-debug`, `go test -race ./internal/...`,
`go vet ./...` and both `CGO_ENABLED=0` server builds pass. The six real archive
scenes retain MIDI/PCM output, restore and execute their next tick, with no
start/input/execution errors and empty stderr. Logs and anonymous results:
`build/sound-wipi-listener/`. This verifies the SKT Java callback implementation;
physical-phone listening remains separate work. The
page and wire protocol did not change.

### KTF WIPI playback listeners

`internal/testfixture/ktf_listener.go` authors a complete descriptor/JAR/AOT
application with a real Thumb callback; it contains no copied runtime or game
bytes. Twenty-seven Go tests cover the callback ABI and installed constants,
deferred transitions, per-pass completion, replacement/null recipients,
no-op and unsupported clips, reentrant restart and bounded queues. The original
no-op setter fails the START regression. Host deadline tests cover a long guest
sleep, a guest-owned event loop, pause/resume, speed changes and a zero-length
one-shot. Derived guest interfaces with a cycle remain valid across restoration.

Full checkpoints retain pending recipients and a paused note's age and
remaining gate after a long detached activation delay. Forced Go/guest
collection verifies active/paused roots, idle cycle collection and release
after pending completion/stop delivery. A record without the media carrier and
with the old empty interface restores without rerunning startup or inventing
historical END events, and can be captured and restored again.

Twenty-four malformed cases cover missing/wrong owner and recipient references,
carrier identity, counts, progress, active flags, listener fields, executable
callbacks and disagreement between cached types and actual guest memory.
Refusal leaves the source audio, queue and ordinary save intact. Review also
reproduced two BaseClip regressions: overstrict validation rejected supported
playback checkpoints, while understrict event validation admitted a non-Clip
callback argument. Both are repaired and have separate baseline failures.
Further review reproduced new generic events being delivered during the media
callback's round and a recoverable generic exception discarding the waiting
media prefix. Empty, pending and full generic queues, both cross-queue posting
directions, and exception recovery now have regression coverage. Saved audio
and media completion counters must match; small, overflowing and stopped
unreconciled backlogs are refused during preparation.

The existing outbound sound/frame test also exposed a real-time sleep race
under concurrent validation load. It now uses the standard library's virtual
test clock, asserting no write before the delayed frame and one combined
write after it. One hundred race-enabled repetitions pass without changing
the production linger interval or wire format.

`make test` (330 Node tests), `make test-debug`, `go test -race ./internal/...`,
`go vet ./...` and both `CGO_ENABLED=0` server builds pass. The six real archive
scenes retain MIDI/PCM output, restore and execute their next tick, with no
start/input/execution errors and empty stderr. Logs and anonymous results:
`build/sound-ktf-listener/`. No page or wire format changed;
physical-device listening remains separate acceptance.

### LGT WIPI playback listeners

`internal/testfixture/lgt_listener.go` authors a complete descriptor/JAR/AOT
application, boots its Jlet and executes a real ARM playback callback. No
runtime or game bytes were imported. The fixture checks receiver, Clip, event
and parameter words, bounds its own history, and can restart a completed Clip
through the imported Player. The original missing delivery and lost pending
checkpoint events fail executable regressions in
`build/sound-lgt-listener/lifecycle-before.log` and `checkpoint-before.log`.

Tests cover deferred transitions, existing no-op boolean results, original
recipient snapshots after replacement/null, per-pass END without synthetic
START, reentrant restart, native/completion queue limits and guest-clock
deadlines. Zero-duration one-shots, paused time and uncaught callback exceptions
are included. The static-field import path verifies all eight event constants;
`constants-before.log` records their original zero values.

Forced guest collections cover active and paused owners, a reachable idle
Clip's listener edge, queued old recipients, release of idle cycles and native
sounds, and collection twice inside the first callback of a detached batch.
Native target tests refuse unissued copied vtables, wrong Clip/listener types,
wrong descriptors, invalid executable entries and malformed raw class graphs
without replacing the previous listener. Real archives exposed omitted nominal
interface metadata with an exact executable callback member; authored live and
restore tests now accept that observed AOT form. Inherited/derived interfaces
and both stripped direct-interface dispatch forms remain executable. Cached interface or
superclass metadata cannot override a different raw guest declaration.
Review also reproduced stripped child overrides being bypassed by a named
parent. The authored regression compares deferred delivery with an ordinary
guest interface call, including both dispatch forms and full restoration;
`review/override-before.log` records the original failure.

Full checkpoints preserve pending recipients, paused repeat progress, already
completed passes, cleared buffers and guest GC ownership without rerunning
startup or reading saves during preparation. The previous strict version-2
shape restores without historical callbacks and can be saved/restored again;
an older strict reader refuses the new version-3 envelope. Malformed cases cover
ownership, shared sound handles, exact completion counters, pause/repeat/gain,
C/Java state mixing, event limits, raw target records and both readers' strict
unknown/duplicate/missing-field checks. Refusal leaves source playback and its
pending queue usable.

`make test` (330 Node tests), `make test-debug`, `go test -race ./internal/...`,
`go vet ./...` and both `CGO_ENABLED=0` server profiles pass after the real AOT
compatibility repair. Formatting and diff checks pass; final read-only review
found no remaining issue. The six existing real scenes retain output, restore
and execute the next tick, with empty stderr. The LGT Clet was also rerun after
the override repair. Two additional real Java archives then ran for twelve
seconds with three confirm-key presses and restored their version-3 media
snapshots without startup/input/tick errors: `735a579d82ac53bb` retained one
Clip and emitted 74 note starts; `70d709c40e10541f` retained two Clips and emitted
nine note starts plus one PCM start. Both executed their next restored tick;
stderr is empty. These runs use in-memory saves and do not change real saves.

Final logs and anonymous results are under `build/sound-lgt-listener/`, including
`runtime-java-final.jsonl`. The `pre-corpus-*` gates preceded discovery of the
omitted-interface regression; the unprefixed gates include its repair. The
page and wire format did not change. Physical-device listening remains separate
acceptance.

### Global audio score order

`internal/backend/audio_order_test.go` checks explicit note sequences across
three owners, finite and unbounded repeats, equal deadlines, coarse 950 ms
advances, separate deadline passes and an intervening checkpoint. All produce
the same twenty note gates. A separate 32-note interleaving verifies which 24
melodic voices survive under a fixed output clock. Progress queries merge all
owners, resumed voices follow older due peer attacks, and `PlaybackState`
observes a serviced boundary without emitting or advancing anything. Existing
pause, transient, maximum-clock and malformed-state tests remain in force.
The original per-owner drain fails all four initial ordering regressions.

`internal/platform/skt/audio_order_test.go` holds a Player stop between progress
and pause while another Player query or raw SKVM pause attempts to advance the
clock from 450 to 850 ms. Removing the transaction guard fails both cases with
an invalid pause clock. A bounded concurrent test also runs two Players, raw
clip transitions, 32 tones and Host advancement, then verifies independent
paused/running states and full checkpoint capture/preparation. The WIPI
listener regression covers replacement, removal and late registration after
a peer query has completed the old owner's score; the end keeps its original
recipient exactly once, and late registration creates no historical callback.

The KTF and LGT `audio_order_checkpoint_test.go` fixtures enter their registered
Java/AOT APIs with two Clips and no intervening Host audio service. Stopping or
pausing one must reconcile the peer's natural end before the requested
transition. Tests preserve recipient snapshots, clear completed ownership,
capture/restore immediately and reject duplicate delivery. Before repair,
both strict checkpoint paths refused stale peer completion records. The LGT
mixed C/Java regression additionally resumes a C clip past a Java peer's end;
its Java notification and strict checkpoint must remain valid. Existing record
versions and exact completion validation remain unchanged.

Final validation on 2026-10-09 passes `make test` (330 Node tests),
`make test-debug`, `go test -race ./internal/...`, `go vet ./...` and both
`CGO_ENABLED=0` server profiles. Formatting and diff checks pass. Read-only
review found the LGT C-resume omission above; its regression and repair are
included in these final gates, and the follow-up review reports no issue.

Eight existing real scenes (two KTF, one LGT Clet, two LGT Java and three SKT)
run for twelve seconds with three confirm-key presses, emit audio, restore a
checkpoint and execute the next tick without startup/input/tick errors or
stderr. All three LGT scenes were repeated after the final C-resume repair.
The probes use in-memory saves. Final gate logs and anonymous scene summaries
are under `build/sound-event-order/`; the pre-review gate logs are not the final
acceptance result. Reproductions include `backend-order-before.log`,
`resume-before.log`, `skt-order-before.log`, `platform-checkpoint-before.log`,
`listener-recipient-before.log` and `c-resume-before.log`.

The page and wire format did not change. These checks establish backend score
order, concurrency and continuation; true event timestamps, browser playout,
Host-time aging under coarse batches and physical-device listening remain
separate work in the [audio audit](history/audio.md#guest-event-delivery).

### Audio presentation timestamps

`internal/backend/audio_timing_test.go` compares fine and coarse service across
owners and repeats, checks piecewise rate changes, pause/resume/gain/stop times,
negative overdue deadlines after restoration and transient end cancellation.
Retained-state comparisons include drum expiry and exact PCM suffixes on a
stationary Host clock. A slow-guest regression prevents expired output from
becoming young again. `SpeedClock` returns a rate transition from one source
sample, which lets platform audio share the exact boundary.

KTF timing tests exercise pre-runtime rate setup, GC cancellation and a real
listener completion across speed change, strict checkpoint validation and
restore. SKT tests invoke both WIPI volume signatures between Host ticks.
These complement the existing platform pause, rate and checkpoint tests.

`internal/webhost/audio_timing_test.go` covers collector deadlines and silent
frontiers, binary selection bytes, zero/signed timestamps, legacy JSON/binary
fallback and negotiated, epoch-bound recovery. The socket negotiation test
also includes supported and unsupported timing queries. Node tests exercise
both decoders, complete-batch refusal, source and automation times, repeated
retired-graph cleanup, short gates, expired-percussion admission, interruption,
malformed-first-packet recovery and quick load with recovery outstanding.

For native rendered comparison, use the existing Playwright installation:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-presentation.mjs chromium
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-presentation.mjs webkit
```

This harness serves local modules through browser request interception, without
binding a server port. It renders identical notes, PCM, sustain, bend, gain,
pan and stop/restart events in an `OfflineAudioContext`, comparing 5 ms and
70 ms packet grouping against a 90 ms render-clock model. The expected maximum
sample difference is below `1e-6`; nonzero energy prevents a silent false pass.
It models already mapped speed changes, not a native guest execution. Device
latency and real foreground interruption still require a live browser/device.

On 2026-10-09, all 359 Node tests pass. Backend and platform packages pass both
profiles and the race detector; 133 webhost tests with no transitive socket
binding pass in those modes too. `go vet ./...` and `CGO_ENABLED=0 make server
server-release` pass, and binary build metadata confirms debug/release and cgo
disabled. Full test/debug/race gates were attempted: only socket-binding tests
fail under the sandbox, in server/appserver/launcher/webhost/wsproto. Chromium
launch is denied too, so the rendered comparison is syntax checked but remains
unexecuted. The full gates are not recorded as passing. Both later passed on
2026-10-09: [gates](#hps-exclusive-framing), [native rendering](#native-audio-rendering-acceptance).

Logs live in `build/sound-timestamps/`, including the conservative transitive
socket-test exclusion expression used for the port-free webhost checks. No
tracked test is skipped or weakened to accommodate this environment. Browser
and device acceptance remains open in the audio audit and local work plan.

Eight real scenes (two KTF, one LGT Clet, two LGT Java and three SKT) also run
for twelve seconds with three confirm presses, initially at 1.5x, then 0.5x
after three seconds and 2x after seven seconds. All emit MIDI or PCM, restore
an in-memory checkpoint and run the next tick without errors or stderr.
Anonymous results are `runtime.jsonl` and `runtime-lgt.jsonl` in that log
directory. The probe additionally records output event timestamps.

One SKT scene initially showed a backward timestamp. Six diagnostic repeats
located it exclusively after restoration: a pending note-off was due
5.839489 ms before the saved epoch, after the reconstruction's zero timestamp.
It is an overdue saved event, not a backward running clock. The backend retains
that signed deadline; the page rejects the stale post-reconstruction batch and
requests current output. A dedicated page regression verifies atomic refusal
and successful subsequent reconstruction. The trace is in
`runtime-skt-timing-audit.jsonl`; ordinary playback in these probes has no
backward event timestamps. This is protocol/state evidence, not audible or
device-render validation.

### PCM buffer reuse and mobile velocity

`internal/audio/smaf/smaf_test.go` adds two complete authored SMAF fixture
families. A stream note must update omitted-velocity memory even when its wave
is missing or unsupported; explicit zero, initial 64 and another channel's
value remain distinct. Mobile CC121 resets only that channel's memory to 64.
Both zero and nonzero controller operands are covered, followed by another
explicit velocity. Bank/program mapping, emitted gain/pan events, wave mapping,
sample bytes and event times remain unchanged. The new cases fail before the
repair and pass afterward in normal, debug and race modes.

The anonymous corpus comparison covers 11,175 carrier-local unique records.
Exactly 57 records (21 KTF, 34 LGT and two SKT; 50 distinct sound hashes) change
667 note-on velocities. A separate sequence-memory replay explains every
change as an omitted velocity following an explicit stream note. Hashes masking
only note-on velocity are identical for all 11,175 records, including every
PCM/SysEx byte. Applying only the stream-note fix reproduces the combined result;
applying only the CC121 fix changes no scanned record. The independent oracle
checks 8,385 mobile records with no mismatches; 2,721 handset and 69 layered
records are excluded from that oracle, but remain in the complete hash comparison.
All scans retain the known 1,014 KTF archive-member read exclusions and exit 2;
this does not claim those members decoded. Existing checkpoints retain their
already decoded scores. No archive assets are copied or save data changed.

`web/audio-buffer.test.mjs` exercises the actual binary decoder and page
dispatcher. Nine regressions cover 512 repeated immutable samples, independent
owners/sources/gains/times, mono/stereo/rate variants, context replacement,
definition forget/replacement, unchanged mutable/JSON callers, 256-entry and
16 MiB least-recently-used eviction, oversized uncached playback and global
reset. The existing session tests also verify the optional cacheability argument
without changing the wire format. All 368 Node tests pass.

The port-free allocation probe uses one authored 2,000-frame mono definition at
8 kHz for 512 timed plays. Before reuse it requests 512 AudioBuffers and copies
4,096,000 float payload bytes; afterward it requests one and copies 8,000 bytes.
All 512 source creations/starts remain, sample comparisons report no mismatches,
and simulated completion releases every source. These are instrumented API calls
and sample writes, not measurements of native memory or CPU. The native
`web/acceptance/audio-presentation.mjs` comparison now checks cached coarse
packets against uncached fine packets, including exact buffer-request counts.
It is syntax checked but remains unexecuted under the browser-launch restriction.
It passed on 2026-10-09; see [native rendering](#native-audio-rendering-acceptance).

Normal/debug/race SMAF, backend and platform packages pass, as do vet, 133
port-free webhost tests in all three modes, and both cgo-free server builds.
Full `make test`, `make test-debug` and `go test -race ./internal/...` are attempted
again; their failures remain socket-binding denials, with no race report.
The builds emit the same nonfatal denied module stat-cache warning, but exit
successfully and retain the expected debug tag/release trimming and `CGO_ENABLED=0`.
No tracked tests are disabled. Evidence, pinned diagnostic overlays, allocation
before/after records and the independent velocity oracle are under
`build/sound-pcm-followup/`. Native rendering, live socket acceptance and device
listening remain open.

### PCM track control propagation

The 2026-10-09 extension carries ATR volume/expression/pan through decoding,
runtime output, checkpoints, negotiated transport and the page. Six complete
authored SMAF fixture tests cover all four local channels, duplicate track tags,
same-time control ordering, mid-wave deadlines, canonical/compatibility/short
opcodes, ignored invalid values, unchanged samples and exact event/group bounds.
The pre-change run fails for missing controls/routing and uncharged control
events; normal/debug/race SMAF runs pass after the repair.

Backend tests cover live controls without restarted samples, owner isolation,
neutral unset volume, capable/incapable reconnects, stereo onset fallback with
one application of clip gain, pause, natural/repeated tails, explicit restart,
control-only admission, stereo catch-up accounting and atomic invalid-state
refusal. v7 state round-trips and sends current controls before a trimmed raw
wave. A genuine authored v6 JSON record is read, adopted and recaptured as v7
without inventing channels. Six codec tests independently cover field ordering,
mixed-version wrappers, null/empty lists, malformed hybrids, duplicate/unknown
or unrelated missing fields, allocation limits and unchanged destinations after
refusal. Version-6 records omit every new PCM field; version-7 records require them.

Host tests exercise real collector/backend paths, golden JSON and WFA2 bytes,
both legacy formats, ownership and PCM negotiation, dropped output, full
checkpoint reconstruction and parked reconnection in both capability directions.
A maximum reconstruction includes 256 owners and all 2,048 PCM groups with 6,144
PCM controls and fits the existing 65,536-operation bound; normal collection
still refuses above 4,096 operations. Page tests cover exact speaker coefficients,
timed mute/unmute without another source or buffer, initial controls before
activation, independent MIDI/PCM/owners, shared-buffer routing, malformed and
over-budget batch atomicity, reset reconstruction, stop/restart generations,
natural source cleanup and failed allocation. All 380 Node tests pass. An
independent review found no backend or page defects in this extension.

The anonymous corpus comparison covers all 11,175 carrier-local unique sounds.
Exactly 5,010 records (4,473 distinct hashes) gain PCM controls or routing:
3,112 KTF, 1,089 LGT and 809 SKT. A separate translator over parsed ATR sequences
matches all 4,732 controls and 8,752 routed waves, including their group, value,
type and deadline. Removing only the new controls and routing yields identical
ordered hashes for every pre-existing event field and PCM/SysEx byte. All decoded
records pass shared runtime admission; the observed maximum is one PCM group
per sound. No parse/panic/admission error or new exclusion occurs. The known
1,014 KTF archive-member read exclusions remain, and the scan exits 2 to report
them. These checks establish propagation and preservation, not handset loudness.

Eight real archive runs use an in-memory save store, 12-second windows, injected
keys and speed changes 1.5 → 0.5 → 2. All start, capture, restore and advance after
restoration without error. Eleven PCM controls and eleven routed waves reach the
capable shared sink across KTF, LGT and SKT; all ordinary and post-restore event
times are monotonic in this run. The prior overdue-restore case remains documented
above and covered by its regression; a clean observation does not remove it.
Archive hashes, counters and stage results remain in ignored evidence. No game
assets are copied, and ordinary save files are not written by these probes.

Required `make test`, `make test-debug` and `go test -race ./internal/...` runs
fail only in socket-binding tests under the sandbox, with no race report.
SMAF, backend and all platform packages pass in these runs; all 139 port-free
webhost tests separately pass in normal/debug/race modes. `go vet ./...`,
formatting/diff checks and both `CGO_ENABLED=0` server builds pass. Build metadata
confirms the debug tag and release trimming. The builds retain the nonfatal
denied module stat-cache warning. No tracked test is disabled or weakened.

`web/acceptance/audio-pcm.mjs` prepares four port-free native-render comparisons:
gain/pan and mid-wave mute, overlapping groups/owners, future stop/restart and
trimmed restoration against a full-buffer offset. Independent native graphs
check samples within 1e-6, source starts/counts, shared-buffer reuse, silence
windows and graph retirement. Run with `PLAYWRIGHT_MODULE` pointing to
`playwright/index.mjs` and `node web/acceptance/audio-pcm.mjs chromium` (or
`webkit`). The harness is syntax checked only; browser launch remains blocked.
It passed in both engines on 2026-10-09; see [native rendering](#native-audio-rendering-acceptance).

Evidence and the pinned anonymous scanner are under `build/sound-pcm-controls/`,
including `corpus-comparison.json`, `runtime-summary.json` and gate logs. Native
WebAudio rendering, live socket acceptance and phone listening remain open;
Node graph checks and runtime event counters do not substitute for those checks.

### Receiver source pressure

The 2026-10-09 receiver repair adds whole-batch admission for 512 retained
sources and 128 MiB of PCM float payload per live reference. Twenty page
regressions cover exact/excess limits, immutable binary and JSON input,
same-key retriggers, melodic release/percussion tails, future owner stops,
stale end callbacks, direct calls, zero-velocity/expired resumes, allocation
and start failures, and preservation of controls/output after refusal.
The largest reconstruction fits both limits at once: 256 references to a
512 KiB float payload plus 24 resumed notes. Its canonical padded JSON shape
also passes preflight without decoding or allocating a sample buffer; one
extra frame is refused. Huge declared lengths use a failing allocation stub,
so boundary tests do not allocate a 128 MiB sample.

Review caught a legacy compatibility regression in the first conservative
stop policy: refusing a combined immediate stop/restart at capacity left the
old sound playing. Both source and byte boundaries reproduce it before the
fix. Untimed preflight now credits that owner's stopped sources, including
sources created earlier in the batch, without freeing peers or double-crediting
repeated stops. Timed stops stay charged. Invalid explicit owner identities
refuse the complete batch before controllers, stops or resets change output.

Three integration tests drive real `GameSession`, binary/JSON decoding and
`PageAudio`: pressure requests one epoch-bound recovery, immediately clears
old sources, ignores stale batches, accepts all 280 reconstructed sources and
reuses the remaining capacity. An untimed peer drops a refused batch and
accepts subsequent output without requesting an unsupported handshake. All
three fail against the frozen pre-limit page and pass after the repair.

The original authored 2,048-wave probe now refuses the batch with zero native
buffer/source requests; 512 smaller waves still fit. A 10-second wave at 100 Hz
reaches 419 retained references under the byte ceiling, then refuses further
starts until capacity is released. Simulated natural completion and reset
disconnect all sources. The measurements count mock API calls and graph
connections, not native CPU/RAM or audible output. Before/after results, the
frozen page, and an independent backend replay/retention probe remain under
`build/sound-source-pressure/`.

All 403 Node tests and `go vet ./...` pass. Required full normal/debug/race Go
gates fail only in socket-binding tests under the sandbox, with no race report.
The port-free webhost suites pass separately in normal/debug/race modes.
Both `CGO_ENABLED=0` server profiles build; the known denied module stat-cache
write is nonfatal. PWA version 51 carries the new receiver. Native browser
rendering, live sockets and phone listening remain unexecuted in this sandbox.
Rendering and the socket tests ran on 2026-10-09 ([native rendering](#native-audio-rendering-acceptance));
phone listening remains.

### PCM admission and checkpoint charges

The shared backend now refuses a PCM onset before emission when retaining it
would exceed 256 waves or 32 MiB of original raw samples. The authored 512-wave
Host reproduction previously emitted every wave while retaining only 256;
all four JSON/WFA2 and PCM-capable/fallback cases fail against that frozen
backend. The repaired path emits, captures and reconstructs the same admitted
waves, including dropped-batch recovery, reconnect with changed capabilities,
and checkpoint restoration. No checkpoint budget field enters either wire format.

`internal/backend/audio_admission_test.go` covers exact count and byte limits,
refusal without disturbing peers, MIDI, PCM controls or completion, frozen pause
reservations, expiry/stop/close capacity reuse, and identical admission under
fine and coarse advancement with equal-time owner ordering. A 32 MiB fixture
captures half-consumed waves, then tries an onset that fits only if restoration
incorrectly charges the shortened tails. Both original and restored runtimes
refuse it, release the original charges on expiry, and refill the capacity.
Frame-aligned capture isolates this accounting contract. At this stage,
whole-frame reconstruction still shifted expiry by less than one sample frame;
the [fractional PCM repair](#fractional-pcm-reconstruction) below closes that gap.

`audio_admission_state_test.go` rejects absent, undersized, misaligned and
excessive charges before runtime construction. It verifies typed legacy
adoption without mutating caller data and debug-only, logarithmically throttled
diagnostics through sink changes and logger detachment. The checkpoint codec
required exact version-6, version-7 or version-8 shapes at this stage, including when Version
follows its children or versions coexist in separate nested records. Null,
duplicate, mixed, missing and over-budget records leave the destination unchanged.
Version 8 preserves original charges; older records derive them from available
tails. An older binary requires its supported checkpoint or ordinary startup.

An anonymous corpus pass exercises public `LoadEvents`, `Play`, `Advance`,
`CaptureState`, live replay and restored replay for all 11,175 decoded sounds.
Independent ordered hashes match all 10,688 PCM calls and 80,954,352 raw emitted
bytes; independent suffix hashes and charges match all 2,912 retained tails.
This run's recaptures use version 8. Coverage, decoded events and exclusions match the
preceding PCM census: 1,014 known KTF member-read failures remain, and the scan
exits 2 solely to report them. No parse, panic, admission or replay failure occurs.

Single-pass overlap peaks at five references. A separate exact periodic analysis
uses each score's final event time as the repetition period. For sample duration
`D` and period `P`, `D / P` complete copies are always retained, while `D % P`
contributes one circular interval. Sweeping these intervals counts natural ends
before equal-time starts and charges full original buffers. An independent
finite enumeration validates 220 authored fixtures. The infinite-repeat peaks
below describe one sound at 1x; count and byte witnesses can differ.

| Platform | Peak retained references | Peak raw admission bytes |
| --- | ---: | ---: |
| KTF | 64 | 520,192 |
| LGT | 64 | 520,448 |
| SKT | 64 | 520,192 |

No individual sound exceeds either limit; no zero-period score occurs. The
analytic repeats do not prove live repeated playback, simultaneous game owners,
changed speed, browser source cost or audible quality. Method, reproducible
commands, anonymous hashes and before/after evidence are retained locally under
`build/sound-pcm-admission/`.

Eight real archive scenes also start, capture, restore and advance successfully
with in-memory saves and the existing 12-second key/speed script. This pass
records 12 PCM controls, 12 routed starts and 23,268 samples. The wall-clock
probe is not a deterministic event-count oracle: one additional SKT onset
occurs compared with the preceding pass. One first post-restore note-off has
the already permitted overdue timestamp, about -6.54 ms behind the replay
frontier. The corpus comparison above provides deterministic PCM preservation.
This scene sink lacks guest-gain support and a logger, so it does not establish
gain rendering or the absence of PCM refusals.

All backend tests pass in normal/debug/race modes, and all 403 Node tests pass.
The required `make test`, `make test-debug` and `go test -race ./internal/...`
gates fail only at sandbox-denied socket binding, with no race report. Port-free
webhost suites pass separately: 137 tests in normal/race and 139 in debug, with
three/one existing skips respectively. `go vet ./...`, formatting/diff checks,
and both `CGO_ENABLED=0` server builds pass. Metadata confirms the debug tag,
release trimming and identical embedded PWA 51. The module stat-cache warning
remains nonfatal. No tracked test is disabled. Browser rendering, live sockets
and phone listening remain unverified in this sandbox.

### Playout lateness diagnostics

`web/audio-lateness.test.mjs` exercises the public batch preparation and report
callback. The baseline fails because no timing aggregate exists. Ten authored
regressions cover exact histogram boundaries, accepted lateness within 5 ms,
every event after a batch's first window refusal, distinct late/future/range
refusals, invalid mapped times and silent clock-only refusals. First/reset
anchors stay separate from continuing anchors. Recovery and `stopAll` preserve
pending measurements; flushing and log clear discard them without changing
live sources or their anchor. Disabled/default diagnostics retain no aggregate,
and malformed, backward, resource-refused and unavailable-context inputs do
not become timing samples.

The two transport cases pass authored JSON and independently encoded WFA2
through `GameSession`, decoding, dispatch and `PageAudio`. A late packet makes
one existing recovery request, suppresses subsequent stale batches and retains
its diagnostic through `allOff` and reconstruction. All 427 Node tests pass;
the production diff also passes independent review. Evidence is retained under
`build/sound-lateness/`. These are controlled clock and callback measurements,
not native rendering, network transit, post-admission source allocation cost,
physical-device distributions or listening acceptance.

A later change stops refusing a late batch that holds only its frontier. "A
late frontier without events moves the anchor and keeps playing" replaces the
silent clock-only refusal case: the anchor moves to place the frontier one lead
ahead, the batch is counted as `reanchored`, a note that follows is scheduled
from the moved anchor, and a late batch that carries an event is still refused.
"A reset anchor after a refusal is counted apart from continuing batches" keeps
the reset accounting the old case covered, now through a refused event batch.
Both fail against the previous page; all 428 Node tests pass. The live sessions
that motivated it are in [sustained load](#sustained-sound-load-in-real-titles).

### Fractional PCM reconstruction

The authored baseline at 4 Hz captures a one-second wave at 125 ms. At another
125 ms after restoration, its first remaining sample differs from uninterrupted
output; at another 875 ms, it still retains a sample that should have expired.
The repaired version-9 state keeps a bounded integer fraction of the first
remaining frame. `audio_wave_phase_test.go` compares future suffixes and expiry
against the uninterrupted runtime and uses an independent arbitrary-precision
oracle over original samples through repeated checkpoint round trips. Rates
include 1, 3, 44,100, values around one billion, 1.5 billion and maximum uint32,
with mono, stereo and 32-channel cases. Tests preserve the existing floor-to-
nanoseconds expiry, including sub-nanosecond sample periods.

Further backend cases verify exact-deadline admission at 256 retained waves,
frozen pause/resume, delayed detached activation, original byte charges,
owned/group routing, guest gain and onset-rendered stereo fallback. Legacy
version-6/7/8 state adopts phase zero; out-of-range phase or phase in an older
typed record is refused before construction. The strict codec separately checks
all four encoded shapes, Version-last ordering, nested independent versions,
missing/duplicate/null/negative/overflow fields, allocation bounds and unchanged
destinations after refusal.

Host tests reproduce the missing extension against the frozen encoder and
compare independently authored bytes for `0x16`, JSON phase, and unchanged old
wave operations. Actual backend/collector replay covers capability combinations,
trimmed capture/restore, dropped replay and parked reconnect in both directions.
The page requests `phase=1`; new client tests drive `GameSession`, both decoders,
dispatch and `PageAudio` to the source's actual start arguments. Invalid phase
or truncated operands cannot deliver partial audio. Recovery waits for observed
timing support after a malformed first binary packet and requests only one reset.

Seven page tests also check independent offsets on a shared immutable buffer,
sample-rate and channel handling, atomic refusal before controls/reset/allocation,
failed-start cleanup and retiring graph cleanup. Payload reservations remain
unchanged until source cleanup. Missing or zero phase keeps the original
`start(when)` call; a nonzero phase adds an explicit offset without moving `when`.
The native `web/acceptance/audio-pcm.mjs` harness now includes a fifth scenario
comparing two fractional offsets with an independently authored native graph
using the same suffix. Syntax checking passes; native rendering remains
unexecuted under the existing sandbox restriction (it matched exactly on
2026-10-09; see [native rendering](#native-audio-rendering-acceptance)). These checks do not claim
to preserve an earlier renderer's hidden resampling history.

The complete 11,175-sound corpus also passes public backend playback, v9 capture
and live/restored phase-aware replay. Decoded event digests, ordered raw PCM
hashes, byte charges and coverage match the preceding run: 10,688 PCM calls,
80,954,352 emitted raw bytes and 2,912 retained endpoint tails. Of those tails,
32 have nonzero phase. An independent original-age oracle then performs 11,634
bounded future capture/restore steps across 2,905 sounds. It checks 8,751
surviving tails, including 5,889 nonzero phases, immediately before and at every
one of the 2,912 original tail deadlines. At most ten future points are needed
per sound. No waveform payload is written to the evidence directory.
The same 1,014 archive-read exclusions remain; exit 2 reports those exclusions,
not a phase failure. This checks individual sounds at 1x, not mixed game or
native browser playback. Anonymous results and commands are under
`build/sound-pcm-phase/`.

Eight real archive scenes also start, capture v9 state, restore and advance
successfully with the existing in-memory-save, 12-second key/speed probe. The
pass produces 12 routed PCM starts and 23,268 samples. Two already permitted
overdue restore deadlines appear: a KTF SysEx at about -7.24 ms and an SKT
note-off at about -9.50 ms. The probe deliberately retains its legacy sink
without phase, gain or logger support; it checks real platform adoption and
compatibility, not fractional rendering or admission diagnostics. Original
evidence remains unchanged and no ordinary saves are written.

All 417 Node tests pass. Backend and platform packages pass required normal,
debug and race runs; the full Go gates still fail only on sandbox socket
binding, with no race report. Port-free webhost suites separately pass 143
tests in normal/race and 145 in debug, with three/one existing skips respectively.
`go vet ./...`, formatting/diff checks and both cgo-free server builds pass.
Metadata confirms the debug tag, release trimming and current embedded PWA 52;
the module stat-cache warning remains nonfatal. Live sockets, native rendering
and physical-phone listening remain unexecuted here. Sockets and rendering ran
on 2026-10-09 (below); listening remains.

### Native audio rendering acceptance

On 2026-10-09 every audio harness under `web/acceptance/` ran in headless
Chromium 153 and WebKit 26.6 through Playwright 1.63; the earlier sound
sections could not launch a browser. `audio-presentation.mjs` and
`audio-pcm.mjs` serve the shell through request interception; the others take
an origin, here a loopback static server for `web/`. All sixteen runs pass:

| Harness | What it renders | Chromium | WebKit |
| --- | --- | --- | --- |
| `audio-presentation.mjs` | 5 ms and 70 ms packets, cached PCM, 90 ms render clock | error 0 | error 0 |
| `audio-pcm.mjs` | gain/pan laws, overlap, future stop/restart, trimmed and fractional restored PCM | ≤ 1.5e-8 | ≤ 1.5e-8 |
| `audio-controls.mjs` | seven live CC7/CC11/CC10 scenarios | ≤ 6.0e-8 | ≤ 6.0e-8 |
| `audio-midi-state.mjs` | nine sustain, RPN and reset scenarios | ≤ 3.0e-8 | ≤ 3.0e-8 |
| `audio-resume.mjs` | MIDI, PCM and percussion pause/resume with a peer | error 0 | error 0 |
| `audio-gain.mjs` | MIDI and PCM live mute, quarter gain, unmute | pass | pass |
| `audio-ownership.mjs` | one clip cancelled, peer RMS matches reference | pass | pass |
| `audio-timing.mjs` | twelve short notes on a 90 ms clock | pass | pass |

The fractional restored PCM scenario, the one the
[fractional reconstruction](#fractional-pcm-reconstruction) section left
pending, matches its independent native graph exactly in both engines. These
are offline renders of authored events: they settle native graph behavior, not
a live game session, device latency or listening. `build/sound-browser-acceptance/`
holds `run.sh` and the sixteen logs.

### Live audio session acceptance

`web/acceptance/audio-live.mjs` runs a debug server binary, the embedded page
and a running `AudioContext` in Chromium or WebKit. No authored fixture plays
sound unless a test calls it, so the harness takes a local archive through
`WFEATURE_AUDIO_ARCHIVE`, copies it into a scratch library, presses OK three
times like the real-scene probe, and removes the copy and its save tree
afterwards. It checks that the socket negotiates
`protocol=2&sound=resume&timing=1&pcm=1&phase=1`, that binary sound batches
reach a running context and that every source the page schedules starts ahead
of the render clock, and it folds the debug page's `audio playout timing`
reports into totals. A wrapper around the page's own window check records the
first batches and every refusal with page time, render time and the batch's
presentation times. It also reports the most sources alive at once and the
peak of the mix the page sends to the speakers. `WFEATURE_AUDIO_SECONDS`
lengthens the run and `WFEATURE_AUDIO_KEYS=mash` keeps pressing keys after the
three confirms; timing reports are copied as they are logged, because a long
run pushes the first ones out of the debug log view.

```sh
WFEATURE_AUDIO_ARCHIVE=/path/to/archive PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
  node web/acceptance/audio-live.mjs build/debug/wfeature-server chromium
```

On 2026-10-09 four local archives pass in both engines, each for about
twelve seconds:

| Archive prefix | Sound batches | Scheduled sources | Lead (s) | Window refusals |
| --- | ---: | ---: | --- | --- |
| SKT `8bf2f80c0723fd97` | 682–688 | 463 | 0.078–0.105 | none |
| SKT `14a62a8521a0d434` | 689–690 | 172–176 | 0.044–0.109 | WebKit: one late |
| KTF `aa3fcba4598b` | 666–678 | 113–116 | 0.078–0.116 | none |
| LGT `87b04639cdfef6cc` | 329 | 10 | 0.030–0.092 | one future, one late |

No source started late and no event was more than 100 ms late. The LGT
archive's first ticks take about 300 ms: the batch sent before the first stall
and the one after it arrive together, so the second looks 309 ms ahead, and the
batch after the reconstruction waits out a 363 ms stall and arrives late. Each
refusal requests current output once, and the rest of the run stays 30–100 ms
ahead without another. In WebKit the second SKT archive delivered one batch,
around a key press, about 55 ms later than its neighbours; its first event, 65
ms older than the batch's frontier, missed the 5 ms tolerance by 10 ms and the
page reconstructed once. These are the designed recoveries from a stalled
server, not lost timestamps. Whether an event a few milliseconds late should
play at once instead of reconstructing is a tuning question for device
lateness data. Per-run results without archive names are in
`build/sound-live-audio/`.

### Native cost of the largest sound loads

`web/acceptance/audio-load.mjs` renders the page's own `PageAudio` offline, four
seconds at 48 kHz, under loads up to the two admission ceilings, and samples the
browser's resident memory from outside while it runs. The time a render takes
against the audio it produces is the audio thread's share of one core:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-load.mjs chromium
```

Every wave is a distinct 8 kHz buffer, so nothing is shared through the buffer
cache. On 2026-10-09, on the development machine (Apple silicon, Chromium 153
and WebKit 26.6 through Playwright 1.63):

| Load | Native sources | Chromium (× real time) | WebKit (× real time) |
| --- | ---: | ---: | ---: |
| 24 notes | 24 | 40 | 98 |
| 24 notes + 4 waves | 28 | 24 | 80 |
| 24 notes + 16 waves | 40 | 8.8 | 71 |
| 24 notes + 32 waves | 56 | 4.5 | 59 |
| 24 notes + 64 waves | 88 | 2.3 | 42 |
| 24 notes + 128 waves | 152 | 1.4 | 25 |
| backend ceiling: 24 notes + 256 waves, 32 MiB of 16-bit charges | 280 | 0.8 | 15 |
| receiver ceiling: 512 sources, just under 128 MiB of floats | 512 | 0.5 | 8.3 |

Every load was admitted and rendered. A wave costs Chromium far more than a
note, and somewhere between 128 and 256 simultaneous waves its audio thread
stops keeping up on this machine; WebKit keeps up at both ceilings. Chromium's
renderer grew from 357 MiB to 488 MiB under the backend ceiling and to 608 MiB
under the receiver's; WebKit runs its audio outside the processes the harness
can see. A phone is slower than this machine, so these are upper bounds on
what an Android WebView sustains. The ceilings bound memory and recovery, not
real-time cost: the [sustained load in real titles](#sustained-sound-load-in-real-titles)
is what says how far below them play stays. Logs: `build/sound-load/`.

### Sustained sound load in real titles

A temporary probe recorded, for each run, the most PCM references and raw bytes
the backend held at once, the most retained notes, every wave started and every
wave refused. Each local archive ran 3,000 ticks with a key pressed every
twelve ticks, cycling OK, 5, the four directions, 2 and 8 (KTF under
`runktf -play -speed 4`, LGT under `runlgt`, SKT under `runskt`):

| Group | Runs | Runs with PCM | Most waves at once | Most bytes at once | Waves started | Refused |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| KTF | 300 (260 distinct finished) | 138 | 7 | 156,768 | 4,272 | 0 |
| LGT | 130 (90 distinct finished) | 44 | 2 | 69,860 | 1,020 | 0 |
| SKT | 106 (90 distinct finished) | 44 | 7 | 106,640 | 2,166 | 0 |

Against the backend's 256 references and 32 MiB, the busiest title holds 2.7%
of the first and 0.5% of the second; the most notes held is the 24-note voice
limit. A run that was cut at the 180-second probe timeout prints nothing, which
is what the distinct counts leave out. A second pass over the 71 titles that use
the KTF C media block, with functions 14, 25 and 26 bound, found at most two
waves at once. Three titles run with both builds side by side started 3 and 3,
0 and 0, and 60 and 42 waves. That third title is what `-play` does to a count:
eight runs of the earlier build started 27 to 90 waves and six of this one 11
to 63, and every run's peak was a single wave.

The two titles with the most PCM on each platform then ran for 90 seconds in
the real page, against a debug server, with keys pressed the same way:

```sh
WFEATURE_AUDIO_SECONDS=90 WFEATURE_AUDIO_KEYS=mash WFEATURE_AUDIO_ARCHIVE=/path/to/archive \
  PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
  node web/acceptance/audio-live.mjs build/debug/wfeature-server chromium
```

| Archive prefix | Most sources alive (Chromium / WebKit) | Mix peak at default sliders | Late refusals, frontier-only in brackets (Chromium / WebKit) |
| --- | --- | ---: | --- |
| KTF `ab601085` | 24 / 25 | 0.27 | 3 (2) / 0 |
| KTF `599c1325` | 25 / 25 | 0.11 | 0 / 0 |
| SKT `858d7a15` | 20 / 18 | 0.42 | 1 (1) / 0 |
| SKT `2f524600` | 7 / 7 | 0.18 | 3 (3) / 0 |
| LGT `ecb1bf8d` | 13 / 13 | 0.10 | 31 (25) / 18 (13) |
| LGT `c9b287e3` | 31 / 28 | 0.08 | 4 (0) / 2 (1) |

No source started late and no meter read crossed full scale. At most 31 sources
were alive against the 512 the page admits. The page's two sliders default to
half and its master gain is fixed, so full sliders double the mix: the loudest
peak, 0.42, would reach about 0.84, short of clipping, so this sample calls for
no limiter. The meter sums everything the page connects to the speakers, read
four times a second over overlapping windows.

Every late refusal asked the server for its current output, and 80 of the 121
over both passes were batches holding only their frontier, which lose nothing
by arriving late. Most came from one LGT title whose server ticks stall for up
to a quarter second under the debug build. The page now moves its anchor for
such a batch instead of reconstructing ([the contract](audio.md#presentation-timestamps-and-recovery)).
Run again on that title for the same 90 seconds, it asked for 6 reconstructions
in Chromium and 7 in WebKit where it had asked for 31 and 18, and 14 batches in
each moved the anchor instead; the remaining requests all follow batches that
carried events.

The first KTF title stopped in both engines partway through its run: its guest
thread called `new Character(char)`, which the KTF class table does not declare
although the JVM implements it. The CLI sweep found the same in one title and
`InputMethodHandler.notifyKeyInput(II)Z` missing in two. Neither is a sound
defect; both are tracked as follow-ups. Results and the probe's patch are
under `build/sound-load/`.

### Quick saves from 0.5.2

0.5.2 is the first release with quick saves, and this tree changed the records
inside a slot (audio component version 9, JVM heap version 2, the media
listener records) while the envelope stayed at version 2. An upgrade rehearsal
captured slots with the 0.5.2 tree from seven of the eight real scenes the
sound sections use (the eighth, an SKT title, cannot be quick-saved in 0.5.2
at all, which this tree repairs) and loaded them with this tree: all seven were
refused as damaged, `checkpoint record is missing fields`, which the page would
have shown as that English cause instead of its sentence for an earlier build.

The envelope is now version 3 and a slot is `<archive SHA-256>.v3.wfq`; a
`.v2.wfq` or `.wfq` file is an earlier build's, as
[the slot rules](architecture.md#leftovers-from-earlier-builds) describe.
`TestCheckpointSlotNeverTouchesAnEarlierBuildsSlot` runs both earlier names:
offered, refused with `ErrCheckpointLegacy` naming the file, never touched, and
a new quick save written beside it. `TestCheckpointRefusesAnEarlierFormatByVersion`
refuses version 1 and version 2 envelopes for their version at every size.
`TestQuickLoadRefusesAnEarlierBuildsSlotWithAReason` runs every host title
against both names: a start from the slot and a quick load both answer the
page's sentence for an earlier build, the running game and its saves are
unchanged, the log names the file left in place, and the new quick save beside
it loads. The rehearsal program is the ignored `build/_rehearsal`
(`rehearsal capture|load|store GAMES_DIR OUT_DIR PREFIX...`), built once in a
0.5.2 worktree and once here.

The same seven slots then went through this tree's slot store as the bytes
0.5.2 wrote. The `store` mode puts each one where a directory store keeps
slots, as `<archive SHA-256>.v2.wfq`, and asks for it the way a Host does. All
seven are offered, all seven are refused with `ErrCheckpointLegacy` naming the
file, none returns a byte, and every file is unchanged afterwards.

### Whole-corpus acceptance against 0.5.2

On 2026-10-09 the acceptance tool behind `make acceptance` ran the seven local
corpora twice with `-since '' -cache=false`: once with the 0.5.2 tree and once
with this one. They hold 546 archives across KTF, LGT and SKT. The default
60-minute stage timeout stopped the largest KTF corpus's interactive rung after
126 of its 257 archives on both sides, so that rung compares those 126. The
other rungs and corpora compare every archive. One run recorded absolute corpus
paths and the other relative ones, so the repository prefix was stripped before
`-compare`.

Two archives got worse and none got better:

- **An older relocatable KTF module stopped at `startApp` in `Clip.setListener`.**
  0.5.2 ignored listeners. The new check read the listener's implements-list
  entry `0xb` as a class record, but it is a reference cell naming
  `org/kwis/msp/media/PlayListener`
  ([the older modules](history/ktf.md#the-older-modules-run-under-the-platform-and-all-three-of-them-play)).
  The check now treats a record it cannot read as undecided, as the guest's
  type check does, and still requires a concrete, executable `playUpdate`.
  `TestKTFWIPIListenerAcceptsAnUnlinkedImplementsEntry` covers the entry and a
  quick save over it. All three older modules pass all eight rungs again, and
  the regressed one receives four `playUpdate` calls in 600 ticks. Both this
  check and the guest's type check later learned to read such a cell by its
  name ([below](#older-module-implements-cells)).
- **An SKT title passed the interactive rung in the 0.5.2 batch but skipped it
  in this one**, because its screen was still changing. Run alone, both trees
  skip it three times out of three for the same reason, so the batch pass was
  load timing.

Every other archive reached the same rung, with the same outcome at every
stage.

### Older module implements cells

An older relocatable module leaves each implements-list entry as a reference
cell: a name-table index with the unresolved bit. The guest's type check and the
media listener check both walk those lists. Before, neither could read a cell,
so the listener check left the answer open and the type check fell back to the
Host registry, which records superclass names only and answered no to every
interface target — including the one a class names outright. Both now read a
cell's name from the module's name table, compare it with the target and follow
the record it names; a name neither the module nor the registry knows stays
undecided. The registry fallback also stops answering no for an interface
target at all, since it cannot see one.

`TestCheckTypeReadsAModuleImplementsCellByName` covers a cell naming the target
(yes), one naming an interface that extends it (yes), one naming an unrelated
interface (a decided no) and an unknown name (permissive yes); the first three
answered no before. `TestCheckTypeRegistryFallbackLeavesAnInterfaceTargetOpen`
covers the fallback. `TestKTFWIPIListenerReadsAModuleImplementsCellByName`
refuses a listener whose cell names an unrelated interface — the previous check
accepted it as undecided — and accepts one naming `PlayListener` without an
undecided count. None of the three local older modules asks the type check
anything in a 600-tick run with keys, so no local title changes.

### KTF C media slots read from callers

The evidence for functions 9, 10, 14, 18, 25 and 26 of the KTF C media block is
in [the audio history](history/audio.md#source-mutes-and-the-ktf-c-media-slots-read-from-callers).
It came from a temporary probe, kept out of the tree, that counted each call
with its argument registers, link register and lifecycle phase, over all 300
local KTF archives:

```sh
wfeature runktf <archive> -play -speed 4 -ticks 900 \
  -key 200:fire -key 300:fire -key 400:fire -key 500:5 -park 650:300 -diag <report>
```

and over the LGT archives with `runlgt -ticks 2400 -trace-live` on the media
slot names. `TestWIPICMediaPauseAndResumeKeepTheClipsPlace` pauses a playing C
clip at 250 ms, holds it for ten seconds without output, resumes the held note
at age 250 ms and ends its gate at the remaining 250 ms; second calls, a stopped
clip and a null clip answer `M_E_ERROR`. `TestWIPICMediaClipVolumeIsTheTitlesOwn`
reads the device level, sets a clip level before load and after, and clamps.
`TestWIPICMediaMuteStateIsRememberedAndSilencesNothing` reads back what was set,
plays a clip under a muted source and bounds the sources a guest can name. The
control-state round trip carries mute state and clip volume through a quick
save, and two malformed records are refused.

### Host text commits a title can see

The behaviour is in [native text input](native-text-input.md#what-a-title-sees-after-a-commit).
The survey that found it drove real archives through `internal/session`, the
path the page uses, with a temporary probe kept out of the tree: tick with a
manual KTF clock, press a fixed cycle of keys every 12 ticks for 3,000 ticks
(restarting a title that exits, up to four runs), and whenever `TextInput`
opens, commit Korean text at the field's limit (four characters for append
targets), then record the frame before the commit, 30 ticks after it, and
after one more key, and read the field back where the route allows. It ran on
the 89 archives under `var/games/{ktf,lgt,skt}` and on the 72 archives whose
packages name a text-entry class (`TextFieldComponent`, `TextBoxComponent`,
`GTextField`, `InputMethodHandler`, `XTextField`, `TextComponentHandler`,
`lcdui/TextField`), plus the documented C-editor archives.

The sweeps reached 16 editors in 14 archives, and two manual routes added a
KTF C and an LGT C name editor. Eleven LGT archives also offered input on their
splash screen and refused the commit (open, see the page above). Two SKT
titles showed the committed text only after another key on the
`TextComponent` route (one field of one title, two of three fields of the
other), and one on `XTextField`. With the redraw key all four fields show the
text within 30 ticks and none gains a character. The LGT name widget reported
every commit as changed although the name reached it; with the delivery fix
the same commit succeeds. Three titles keep the text off screen whatever key
follows: two draw black `XTextField` text over a dark field (open), and a KTF
title's name box stays empty with the frame request in place (cause not
established). No KTF C editor reached flushes inside its key, so the
first-character stop is reproduced by
`TestCInputHostDeliveryToleratesAFlushInsideTheCarrier`, which delivers `b0a1`
(one character) and reports the field changed before the fix.

`TestTextInputCommitRequestsAFrameForTheShownCard` (KTF) and
`TestTextInputCommitAsksThePushedCardToPaint` (LGT) fail without the frame
request. `TestLGTDeliveryToleratesAFlushInsideTheCarrier` uses the C fixture's
optional flush after each key and still requires a player key between
snapshot and commit to stale the edit; the KTF test does the same.
`TestHostCommitRedrawsATitleTextComponent` and
`TestHostCommitRedrawsATitleXTextField` define a Canvas that redraws only after
passing a key to its field, and require exactly one redraw after the commit and
no extra character. Package tests, `-race` on the KTF, LGT, SKT, session and
webhost packages, and `go vet` pass.
