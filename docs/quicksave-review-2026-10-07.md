# Quick-save and quick-load review — 2026-10-07

Reviewed revision: `2ca242e5c56c8e4b025efc858d03dc6ef8f09d71`.
Scope: KTF Java/AOT, older KTF modules, native KTF packages, LGT Clet/Java,
SKT Java and SGS, their shared storage boundary, CLI, server and page.

The acceptance rule is [ordinary saves come first](architecture.md#quick-load-and-ordinary-saves).
A load restores execution, retains current saves and deletions, and rebuilds
host storage objects from current data. A quick step may persist writes the
game has already issued, but must not overwrite a newer save with a stale host
buffer. A refused step must preserve the running game and its pending writes.
The older whole-save replacement design in local `QUICK.md` is historical.

**All eight findings are repaired and verified in the follow-up change.**
The original review reproduced one P1, six P2 and one P3 despite passing normal
gates and bounded local routes. The findings below describe that reviewed
revision; [the repair record](#repairs-and-verification) describes the resulting
behavior and new regression coverage. Save and checkpoint formats are unchanged.

## Repairs and verification

| Finding | Repair and maintained regression |
| --- | --- |
| Q1 | SKT tracks issued writes by normalized key and checks every dirty file before any checkpoint flush. Stale buffers, independently dirty aliases, later deletions and opens over failed newer writes refuse the step. The guest's explicit flush can resolve the conflict. `TestJavaCheckpointFlushPreservesLaterWrite` and `TestCheckpointFileConflictsRefuseBeforeAnyWrite` cover preservation and recovery. |
| Q2 | LGT includes File/DataBase collisions in the same preflight. `TestCheckpointRefusesPendingFileAndDatabaseOnOneKey` covers aliases, both quick steps and a later guest close. |
| Q3 | LGT reads required path lists into an isolated view before writing. Read refusal preserves dirty buffers and does not install a sticky error in the live session. A prior guest read error refuses load while writes remain pending. `TestCheckpointListReadFailureKeepsTheGameAndItsPendingWrite` and `TestCheckpointLoadAfterReadFailureKeepsIssuedWrites` cover continuation, refusal and later-save preservation. |
| Q4 | RMS close retries only already-issued writes. It neither rewrites a clean cache nor recreates a deleted name. A later real mutation may recreate the store. `TestJavaCheckpointCloseKeepsLaterRMSDeletion` and `TestRMSCloseRetriesOnlyIssuedWrites` cover both behaviors. |
| Q5 | Claims and transactions share the resolved owner's identity and kernel lock, retaining the original-spelling lock for older processes. Tests cover one/separate processes, fallback, missing owners, relative/absolute dangling links containing linked components and `..`, duplicate lock inodes and slot paths. Partial fallback retains every usable lock; permission errors keep their OS classification. |
| Q6 | Java/SGS validate aggregate catchup before adoption: at most 1,048,576 cycles/events, 128 MiB of repeated PCM/SysEx data, and 1,048,576 active-note visits. The last limit includes notes appended during catchup: event count alone still allowed quadratic note-off scans. `TestCheckpointRejectsUnboundedAudioCatchup`, `TestCheckpointAudioCatchupBudget` and `TestCheckpointRejectsQuadraticAudioCatchup` cover these limits and live-session preservation. |
| Q7 | The writer drains preceding lifecycle messages after selecting its picture. `TestCheckpointPictureWaitsForAResetQueuedAfterTheTextDrain` deterministically covers the previously failing schedule without a production hook. |
| Q8 | Attached and parked resume responses refresh speed from the live runtime. `TestCheckpointResumeKeepsLaterSpeed` covers both paths; the browser runner verifies a later speed of 2 survives page reload in metadata, controls and local preference. |

The final code passed uncached `make test`, `make test-debug`,
`go test -race -count=1 ./internal/...` and `go vet ./...`; the page suite has
281 passing tests. The initial storage/audio reproductions failed before
correction, and maintained tests now cover their triggers. Removing the Host
fixes also reproduces the ordering and speed failures.

Fresh-process shared tests passed in both debug-to-release and release-to-debug
directions for KTF Java, LGT, SKT Java (active and paused) and SGS. Both server
profiles built with `CGO_ENABLED=0`. Release cross-builds passed for
darwin/arm64, darwin/amd64, linux/amd64, linux/arm64 and windows/amd64.

`web/acceptance/checkpoint-skt.mjs` passed all 11 checks against each debug/release
server in Chromium 151.0.7922.34 and WebKit 26.5: 44 checks, zero page errors.
The authored Java/SGS routes verify current save hashes, corrupt-slot refusal,
reset-before-picture order, exact restored pixels, new input epochs and speed
after reconnect. The runner waits for the decoded frame's dimensions and full
pixel hash; a default black canvas cannot satisfy the SGS reconnect assertion.

Final logs, profile tests and builds are in the ignored
`build/quicksave-fixes-20261007/`. Browser result JSON and screenshots are in the
run directories named by its four `browser-*.log` files. Local archive sweeps
listed below were not repeated after these fixes. Physical phones, audible
continuity, complete game save-menu routes and native execution on Windows/Linux
remain outside this repair validation.

## Findings

### Q1 — P1: SKT quick steps overwrite a newer file with an older buffer

Location: [checkpoint_storage.go](../internal/platform/skt/checkpoint_storage.go),
`flushCheckpointSaves`, lines 38–47.

Open the same XFile through handles A and B. A starts with `[1, 2, 3]` and
writes byte zero as `4` without closing. B independently writes byte one as `9`
and closes, leaving `[1, 9, 3]` on disk. Both quick save and quick load then
return success but replace those bytes with A's `[4, 2, 3]`.

The checkpoint boundary calls `persistXFile` on every dirty buffer without
checking whether another handle has stored a newer version. This is a write
made by the quick step itself, before any restored guest instruction runs.
It requires neither a damaged checkpoint nor a storage failure.

Reproduction: `TestReviewSKTCheckpointFlushPreservesLaterWrite`, both `save`
and `load`. Both fail with `before=[1 9 3] after=[4 2 3] error=<nil>`.

Correction: retain a file generation or equivalent original-content check and
refuse conflicting buffers before any checkpoint flush. Check removals and
multiple dirty handles under the same canonical key as well.

### Q2 — P2: LGT misses a pending File/DataBase collision

Location: [checkpoint_storage.go](../internal/platform/lgt/checkpoint_storage.go),
`issuedWritesBlocked`, lines 150–173, followed by the two flush loops.

Create `DataBase("rank")` and persist one record. Open its `rank.db` container
as a Java `File` and write its first byte, leaving that handle dirty. Make the
disk write for a second database insert fail, then restore the store and take
a quick save. The step succeeds and performs two writes. The live database
contains two records but the persisted container contains only one.

The preflight checks duplicate dirty files and duplicate unsaved databases
separately. It never compares a database key against the dirty-file keys.
The flush stores the newer database and then replaces it with the older File
buffer. Both objects can be reached through ordinary Java API calls.

Reproduction: `TestReviewCheckpointPendingFileDatabaseCollision` in the LGT
API overlay. Observed `capture error=<nil>; writes=2; guest records=2; disk records=1`.

Correction: perform one conflict check across file and database buffers before
writing either. A refusal must retain both pending objects.

### Q3 — P2: LGT read failures lose pending writes or disable the live game

Locations: [checkpoint_storage.go](../internal/platform/lgt/checkpoint_storage.go),
`storeIssuedWrites`, lines 191–194 and 237–240;
[wipic_file.go](../internal/platform/lgt/wipic_file.go), `storeFile`, lines 629–634.

Write to an existing open file while its created-path list has not been read.
Fail the first `fs/.created` read, which the quick-save flush triggers through
`markFileCreated`. `storeFile` records `saveReadError` but returns nil without
storing the file. The checkpoint flush consequently clears `file.dirty`.
Capture is subsequently refused, but the issued data is absent from disk and
no longer marked pending. After the store recovers, the original game still
fails on its retained read error.

Two independent proofs cover the private flag and the public behavior:

- `TestReviewRefusedQuickSavePreservesPendingWriteOnListReadFailure` observes
  `pending dirty=false; stored="original"` after the refused capture.
- `TestReviewLGTQuickSavePoisonsLiveSessionOnListReadFailure` uses the shared
  session and an authored Clet. The next tick remains broken after the store
  recovers; the guest cannot finish its pending save.

There is another manifestation of the same false-success rule. If an earlier
guest read has already set `saveReadError`, `storeIssuedWrites` returns `(0,
nil)`. Shared `LoadCheckpoint` then accepts a replacement and discards an
issued pending write. `TestReviewLGTQuickLoadDropsPendingWriteAfterReadError`
observes a successful load with zero writes while `Running()` was true. That
case establishes shared-API reachability; a browser runner can end the session
on the earlier tick error, so it is not claimed as a browser reproduction.

Correction: a blocked write must not report persistence success or clear its
pending flag. Validate required storage metadata without permanently changing
the running session on a refused quick step, and reject adoption if issued
writes cannot be retained.

### Q4 — P2: closing a restored SKT RMS handle reverses a later deletion

Locations: [rms.go](../internal/platform/skt/rms.go), `persistStore`, lines
386–398, and `rmsCloseRecordStore`, lines 560–573.

Take a quick save with a RecordStore open. Close and delete that store, then
load the quick save. The load initially preserves the deletion. Close the
restored handle without adding, setting or deleting any record: `rms/.index`
changes from empty to `"checkpoint"`, recreating the deleted name.

The close unconditionally calls `persistStore`, whose new absent-name branch
registers the store again. This is distinct from a game deliberately saving
after quick load: no record mutation occurs after restoration. The
[MIDP RecordStore contract](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/rms/RecordStore.html#closeRecordStore())
describes close as balancing opens and releasing listeners/enumerations;
record mutations are separate operations. Restoring an open handle over a
deleted backing store is this project's compatibility policy, and its
documented promise is that the next actual write creates the missing save.

Reproduction: `TestReviewSKTCheckpointCloseKeepsLaterRMSDeletion` fails on
the index comparison after the close.

Correction: distinguish a clean restored handle with missing backing data from
an actual guest mutation. A plain close must retain the deletion; a later
record mutation must still use the ordinary persistence path.

### Q5 — P2: linked owner folders bypass save-directory exclusion

Locations: [save_lock.go](../internal/backend/save_lock.go), `acquireSaveLock`,
lines 46–58; [save_replace.go](../internal/backend/save_replace.go),
`replacementPaths`, lines 39–44.

Claim a real owner directory, then claim a symbolic link to that directory.
Both calls return success, including in one process on this macOS filesystem
with working kernel locks. The two paths select different in-process gates
and different sibling lock files despite accessing the same save files.

A session using the link can therefore coexist with a session or import using
the target path. The protection against concurrent whole-buffer writes is
lost. This is conditional on accessing one linked owner through different
paths; a single session using a single link still passes its existing tests.
The advertised linked-folder support does not document an exclusion exception.

Reproduction: `TestReviewLinkedOwnerClaim` receives nil instead of
`ErrSaveDirectoryBusy` on the second claim. This proves lock exclusion failure,
not an independently exercised HTTP import race.

Correction: derive lock identity from the same underlying owner for aliases.
Checkpoint slots may remain beside the link as documented; slot placement and
mutual exclusion need not have the same identity.

### Q6 — P2: constructed SKT audio state hangs execution after adoption

Locations: [checkpoint.go](../internal/platform/skt/checkpoint.go),
`javaCheckpointState.validate`, lines 93–97;
[script_checkpoint.go](../internal/platform/skt/script_checkpoint.go),
`scriptCheckpointState.validate`, lines 127–131.

Both validators accept a repeating one-millisecond sound starting at zero
with the guest clock one hundred years later. Preparing and committing this
record succeeds. The next Java audio advance or SGS tick tries to replay
3,153,600,000,000 cycles in `backend.Audio.advanceSound`.

Separate child tests for Java and SGS time out after three seconds with their
stacks inside that loop, after successful commit. The existing clock-origin
check does not bound the work required to catch up. KTF and LGT have a separate
pending-audio-work check that SKT lacks.

This requires a deliberately constructed internally inconsistent state. Random
file corruption is normally caught by the envelope checksum. The component
tests construct the record directly; such bytes can also be wrapped in an
envelope with a recomputed checksum, which is integrity checking rather than
authentication. No ordinary captured local game was observed producing this
state, and the timeout proof is at the platform boundary, not in a browser.

Reproductions: `TestReviewSKTCheckpointRejectsUnboundedAudioCatchup`,
`TestReviewSKTCheckpointAudioNextTickJava`, and
`TestReviewSKTCheckpointAudioNextTickSGS`.

Correction: reject excessive pending audio cycles/events before adoption,
using the restored guest clock and actual audio progress.

### Q7 — P2: a restored picture can overtake its browser reset

Location: [session.go](../internal/webhost/session.go), `writeMessages`,
lines 1504–1508. Browser consequence: [app.js](../web/app.js), lines 589–595.

The writer drains queued text, then separately checks for a picture. If the
runner queues `restored` and its new complete picture between those operations,
the writer selects the picture and sends it before the newly queued reset.
The page then invalidates its decoder and clears the canvas on `restored`.
A static title need not produce a second frame; subsequent patches can also
arrive without their expected complete base.

`TestReviewHostResetMustPrecedeItsNewFrame` observes wire order
`[picture restored]` instead of `[restored picture]`. Its overlay only adds
a scheduling hook immediately after the first text drain; it does not change
message ordering logic. It controls a legal goroutine interleaving. An
uncontrolled 50,000-iteration stress attempt did not reproduce it, so this
review establishes the possible ordering, not its frequency in normal play.

Correction: serialize each output reset before any picture in its epoch, or
drain the required preceding lifecycle messages after selecting the picture.

### Q8 — P3: reconnecting after quick load restores stale speed metadata

Locations: [session.go](../internal/webhost/session.go), `clientSpeed`, lines
662–664, and `resumeGame`; [app.js](../web/app.js), `sessionStarted`, line 691.

Load a checkpoint at speed 1, change speed to 2, then resume the session. The
runtime remains at 2 but its response reports `restored=true, speed=1`.
`r.started.Speed` was set at quick load and was never updated by the speed
command. The page trusts restored metadata and saves the stale value as the
user's preference, so the display/preference diverges from execution.

Reproduction: `TestReviewHostCheckpointResumeReportsCurrentSpeed` observes
`resume reports restored=true speed=1 while live speed=2`.

Correction: refresh speed metadata from the live session before resume, or
maintain it when speed changes. No save-data loss was observed in this case.

## Validation on the reviewed revision

All four normal gates were rerun with uncached Go tests where applicable:

```sh
GOFLAGS=-count=1 make test
GOFLAGS=-count=1 make test-debug
go test -race -count=1 ./internal/...
go vet ./...
```

All passed, including 281 page tests. A focused uncached checkpoint/storage
selection also passed in backend, ARM, JVM, all three platforms, shared
session, webhost and CLI before the wider gates.

| Additional check | Result and scope |
| --- | --- |
| KTF local continuation | Nine selected archives passed: six Java/AOT and three older modules. Capture after 300 manual-clock rounds, compare 100 rounds of frame bytes, instruction counts, guest time, workers and subsequent saves. Digests: `023c935aea8e`, `1d5831e42a8a`, `56d6865251cc`, `5d3ba49eccf7`, `6c96c5050b2b`, `98db83c3bda6`, `aa3fcba4598b`, `ab601085dc04`, `cc2c96d671cd`. |
| LGT local continuation | All 130 files attempted at tick 300, then 200 comparison rounds. 105 Clets and 21 Java archives passed. Two Java archives exited before capture; two other files were not accepted by the loader. No continuation divergence or unexpected capture refusal. |
| SKT local checkpoint route | All 15 Java archives in the primary directory and three selected SGS archives passed. Capture, later ordinary-save probe, live load, input, continued ticks and recapture. The later-save probe is a host-written test key, not a real game's save-menu route. |
| Fresh-process cross-profile session tests | KTF Java, LGT, SKT Java and SGS passed in both debug-to-release and release-to-debug directions. |
| Fresh-process cross-profile CLI/webhost tests | Existing KTF Java/native and LGT process tests passed in both directions. SKT CLI and real WebSocket routes ran in the ordinary/debug gates; separate SKT Host cross-profile process tests were not added. |
| Rendered SKT keypad route | Chromium 151.0.7922.34 and WebKit 26.5, each against new debug and release servers: all nine checks per combination passed, no page errors. Authored Java/SGS fixtures cover pixels, save hashes, damaged-slot feedback and new input epochs. |
| Builds | Debug and release servers built with `CGO_ENABLED=0`. Release servers also cross-compiled for darwin/arm64, darwin/amd64, linux/amd64, linux/arm64 and windows/amd64. |

Local archive probes used isolated in-memory or temporary stores. Browser runs
used their own ignored game/save roots. No installed game archive or existing
user save was modified. No additional KTF-specific defect was confirmed in the
reviewed capture, restoration and continuation paths. The shared Host and
linked-folder findings still apply when KTF uses those paths.

This review did not repeat every game's gameplay route, measure steady-state
performance, test native KTF packages from a real corpus, listen to restored
audio, or run on a physical phone. Cross-compilation is not Windows/Linux
runtime validation. Current rendered-browser checks use SKT fixtures; KTF/LGT
browser evidence in earlier documents remains historical.

## Reproduction artifacts

Ignored local evidence is retained under `build/quicksave-review-20261007/`:
gate logs, profile/build summaries, local-route logs and the authored Go
overlay tests. The overlays add tests outside the tracked tree. Only the
output-order reproduction overlays a production source file, and its sole
change is the scheduling hook described under Q7.

From the repository root, these commands reproduce expected failures on the
reviewed revision; they are not ordinary gate failures:

```sh
review=build/quicksave-review-20261007
go test -overlay "$review/skt.overlay.json" ./internal/platform/skt \
  -run '^TestReviewSKTCheckpoint(FlushPreservesLaterWrite|CloseKeepsLaterRMSDeletion|RejectsUnboundedAudioCatchup)$' -count=1 -v
go test -overlay "$review/lgt-api.overlay.json" ./internal/platform/lgt -run '^TestReview' -count=1 -v
go test -overlay "$review/lgt-shared.overlay.json" ./internal/session -run '^TestReview' -count=1 -v
go test -overlay "$review/backend.overlay.json" ./internal/backend -run '^TestReviewLinkedOwnerClaim$' -count=1 -v
go test -overlay "$review/host-order.overlay.json" -vet=off ./internal/webhost -run '^TestReviewHostResetMustPrecedeItsNewFrame$' -count=1 -v
go test -overlay "$review/host-speed.overlay.json" ./internal/webhost -run '^TestReviewHostCheckpointResumeReportsCurrentSpeed$' -count=1 -v
```

Run each audio execution proof in a separate test process with a short timeout:

```sh
go test -overlay "$review/skt.overlay.json" ./internal/platform/skt \
  -run '^TestReviewSKTCheckpointAudioNextTickJava$' -count=1 -timeout=3s -v
go test -overlay "$review/skt.overlay.json" ./internal/platform/skt \
  -run '^TestReviewSKTCheckpointAudioNextTickSGS$' -count=1 -timeout=3s -v
```

The overlays contain absolute paths for this checkout and are diagnostic
artifacts, not portable fixtures. The trigger sequences and results above
remain the durable record if ignored artifacts are removed. The repairs above
turn these cases into maintained regression tests at the responsible layers.
