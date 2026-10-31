# Maintenance investigation history

This file preserves implementation investigations and dated validation records.
Statements about current behavior, missing APIs, corpus size and planned work
apply to their original revision; later records may supersede them. Start with
[maintenance.md](../maintenance.md) for the maintained overview. This
consolidation adds no execution or acceptance evidence.

## Contents

- [Implementation and validation record](#implementation-maintenance-review-2026-09-10)
- [Work already present](#implementation-work-already-present)
- [Execution and acceptance follow-up](#implementation-execution-and-acceptance-follow-up)
- [Performance and optional features](#implementation-performance-and-optional-features)
- [Documentation and provenance](#implementation-documentation-and-provenance)
- [Watches and decisions that do not authorize implementation](#implementation-watches-and-decisions-that-do-not-authorize-implementation)
- [Validation of this review](#implementation-validation-of-this-review)
- [State and authentication research (2026-09-11; quick saves later paused)](#state-quick-saves-and-offline-authentication-compatibility)
- [Quick save and quick load](#state-quick-save-and-quick-load)
- [Common authentication bypass](#state-common-authentication-bypass)
- [Research validation](#state-research-validation)
- [Quick save implementation measurements (2026-10-01 to 2026-10-02)](#quick-save-implementation-measurements)
- [Earlier snapshot feasibility record](#snapshot-feasibility)
- [Quick-save and quick-load review — 2026-10-07](#quicksave-review-2026-10-07)
- [What the three carriers share — 2026-10-31](#maintenance-shared-core-2026-10-31)

<a id="implementation-maintenance-review-2026-09-10"></a>

## Implementation and validation record

This review reconciles the local work plan with source at `6d62c35`; it is not
a new game-corpus acceptance run. High priority means an observed execution
blocker or a release decision that needs evidence before closure; medium covers
acceptance quality, incomplete user-facing validation and bounded maintenance;
low covers optional features, performance experiments and compatibility gaps
with no demonstrated failing caller. User checks and evidence-triggered watches
are not implementation instructions at any priority.

On 2026-09-11 the user removed the watch list from the local TODO; its notes
below are historical evidence, not a second implementation queue. The user also
reopened quick save/load and common authentication bypass as active projects,
and [state and authentication research](#state-quick-saves-and-offline-authentication-compatibility)
supersedes the earlier deferral decisions for those two features. Other watch
proposals are not reactivated.

<a id="implementation-work-already-present"></a>

### Work already present

- `internal/webhost/resume.go` and `startapproval.go` limit active and parked
  games together to four. Capacity and shared-save replacement require
  confirmation; active games cannot be evicted. `startapproval_test.go` covers
  cancellation, stale approval, concurrent admission, and the all-active limit.
  Both `cmd/server/main.go` and `internal/appserver/appserver.go` set a
  ten-minute request read timeout. The old resource-limit task is closed.
- `web/keypad-layout.js`, `web/app.js`, and `web/index.html` provide cell editing,
  four shapes including the empty Type4, per-shape persistence, and reset.
  The layout and keypad Node suites cover these paths. Sharing a layout as an
  imported/exported JSON preset is still absent; local JSON storage is not that
  feature. Physical layout validation remains separate from implementation.
- `internal/jvm/builtins.go` routes both object text forms through `objectText`,
  which invokes an override of `toString`. `TestObjectFormsAskForTheObjectsOwnText`
  covers the former missing behavior. The old watch item is closed.
- `internal/platform/lgt/collect.go` reclaims Java objects and their surfaces,
  preserves aliases and platform roots, and closes unreachable files.
  `collect_test.go` covers those edges. The old claim that this platform has no
  collector is obsolete. See [lgt.md](lgt.md#implementation-the-lgt-platform), "Two triggers, and why the second
  one is needed twice over".
- The KTF Graphics table now registers grayscale, stroke-style, and RGB
  component accessors. These names must not remain on a list of missing
  declarations. The former facade task is narrowed to the still-absent
  `dumpRGB565`, `getPixel16`, `setPixel16`, `getImage`, and image constructor
  forms, pending an affected caller and a fresh `apiscan` result. Presence is
  not a claim of complete rendering semantics.
- Completed SKT resource, library, exception, repaint, clipping, Jlet, sound,
  save-probe, display-size, and JVM allocation work is recorded in
  [skvm.md](skt.md#implementation-skvm-and-the-skt-platform), [jvm.md](../jvm.md), and [audio.md](../audio.md). The implemented
  3D surface remains a partial renderer; completion of that slice does not
  establish complete 3D emulation.
- CI action pinning, dependency update configuration, archive validation, and
  dated acceptance reporting exist in `.github/`, `internal/tools/distcheck`,
  and `internal/tools/acceptance`. Their completed checklist entries are removed.

<a id="implementation-execution-and-acceptance-follow-up"></a>

### Execution and acceptance follow-up

The BREW loading-screen observation remains open. The earlier investigation
found no remaining trap and no writes to either source picture over 300 frames;
the state gates at `+0x1748` and `+0x3380` were also untouched. The next question
is how the eighteen loaded resources should populate the picture. It is not a
request to invent another platform slot. See [ktf.md](ktf.md#implementation-ktf-platform-implementation-status), "What writes
this, for a title nothing is unanswered for". The companion title's input and
text rendering work is complete. Text alignment flag `0x200` and the distinction
between font IDs `0x8000` and `0x8001` remain evidence-triggered questions.

The former local paths `var/games/test/errorFile/README.md` and
`var/games/request/` did not exist at this review, which does not prove that the
archives were moved into the acceptance corpus. Recover current paths and match
archive bytes before rerunning the BREW observation or deciding which previously
requested archives to include; do not move game files during a plan review. The
generated records under `var/acceptance/` give dated membership.

`internal/ladder` now uses the full 64-sample observation horizon for both
still and cycling screens. A regression reproduces the former 38-tick animation
false positive. A three-corpus comparison retained `SettleRounds = 2` because
four rounds changed no outcome. The remaining final-round watch difference and
finite-observation limits are recorded in
[settling validation](testing.md#settling-settling-and-key-response-validation). A non-settling animation is an
unanswerable grade, not an emulator defect.

[testing.md](testing.md#implementation-testing), "Browser session retention and control" and
"Destructive starts and pause observations", records local Chromium and WebKit
checks on 2026-09-10. The old statement that no real browser has been exercised
is obsolete. The repository still has no browser automation dependency or CI
browser job. Installation, service-worker replacement, audible recovery, touch,
and save restoration need an explicit browser acceptance path. Existing Node
tests and the recorded session checks cover only part of it.
Superseded: see [PWA acceptance](../pwa-acceptance.md#automated-route).

The same pause observations leave a concrete LGT `resumeClet` unmapped read at
`0x1` unresolved. Ticking continues, but that does not establish correct in-game
recovery. Real-phone suspension, long pauses, active music recovery, and guest
thread progress remain unverified. KTF and SKT clocks advance during the sampled
pauses; LGT's does not. Do not globally freeze guest clocks on this evidence.

<a id="implementation-performance-and-optional-features"></a>

### Performance and optional features

The following are candidates, not speed claims or approved large rewrites:

- Remeasure the KTF session and ARM supervisor-call allocation changes on an
  idle machine. The original run had load average 127, so only its allocation
  and guest-instruction counts were usable. Use the load probe's `ns_per_step`,
  `BenchmarkCoreFastSupervisorLoop`, and fixed-work JVM call benchmarks.
  See [armcore.md](armcore.md#implementation-arm-core-implementation-status), "The allocation every crossing made", and
  [ktf.md](ktf.md#implementation-ktf-platform-implementation-status), "The two allocations a busy session made".
- KTF/LGT frame getters and the SKT memory framebuffer still return copied
  pixels. A 240 by 320 RGBA frame is 307,200 bytes; at twenty copies per second
  that is 6,144,000 bytes per copy site before encoding. The
  [2026-09-23 remeasurement](session.md#cpu-saturation-investigation-2026-09-23-frame-copy-remeasurement)
  found high allocation share but low copy time and GC CPU in one KTF server
  scene. Retain ownership unless a relevant workload establishes material CPU
  cost. `internal/backend/framebuffer.go` explicitly requires retained
  presentation bytes to be copied.
- `walkGuestStack` and `encodeProfileKey` in `internal/armcore/profile.go` still
  allocate a stack and encode it as a string key. The old 2,000-tick debug
  observation attributed roughly 1.1 million objects to this path. That is
  historical profiling evidence, not a measurement of this checkout.
- `guestStringLength` in `internal/platform/ktf/binary_hooks.go` still falls
  back from a failed chunk read to individual bytes near a mapping boundary.
  The old 2,000-tick observation counted about 98,000 `AccessError` objects,
  or 4.5 MB. Recheck cost before replacing the boundary signal.
- The old 21 MB SKT allocation profile attributed 34% to `valueStorage.LoadRange`.
  Char/int array reads are a candidate only after a slow scene is reproduced.
  Byte-array direct reads and borrowed invocation arguments already exist.
- Per-call-site resolution caching was estimated to reach only 5–8% of call
  cost. It also requires receiver guards, native-registration invalidation,
  and atomic publication. The design and rejection threshold remain in
  [jvm.md](../jvm.md), "The three lookups a call did not need, and its last allocation".
- `internal/armcore/backend.go` registers only the interpreter. A native-code
  backend is still an experiment: one target, Thumb first, interpreter fallback,
  no cgo in the core, conformance before use, and a measured improvement beyond
  the old 3.20 ns/instruction no-dispatch result in [armcore.md](armcore.md#implementation-arm-core-implementation-status).
  That number belongs to its original machine and needs a paired local control.
- The unhandled blended-blit mode and two lower-ranked palette-loop shapes
  remain interpreter fallbacks. Reprofile an affected scene before adding a
  recognizer; see [armcore.md](armcore.md#implementation-arm-core-implementation-status), "The eighth shape", and the loop
  census method in [testing.md](testing.md#implementation-testing).
- The external-access add-on is absent. The server's access key is already
  implemented; it does not provide router mapping, a tunnel, or a test of
  external reachability. A separate add-on first needs a decision about that
  last claim. See [running.md](../running.md), "A key, for a server something
  outside can reach".
  Superseded: the server has no access key; see
  [maintenance](../maintenance.md#performance-and-optional-features).

<a id="implementation-documentation-and-provenance"></a>

### Documentation and provenance

The root README, `web/README.md` and the keypad section of `docs/session.md`
now describe four types, per-type cell editing and the shared sizing editor.
These documentation corrections do not add another keypad implementation.

The removed SKT corpus report is now marked historical in [testing.md](testing.md#implementation-testing).
The same document records current HTTP deadlines and the four-game retention cap.
KTF's incomplete list no longer lists implemented cheat watches and table
persistence. Historical archive counts still require dated corpus evidence;
these documentation corrections are not new whole-corpus acceptance runs.

The Host allocation bound remains a separate watch: `internal/webhost/session.go`
reads an archive with `os.ReadFile` before handing it to a bounded loader.
Loader expansion limits do not bound this initial allocation. See
[architecture.md](../architecture.md), "What an archive may declare". This review
does not establish an oversized local archive or an observed memory failure.

The SMAF provenance question is still unresolved: `smaf_test.go` and
[audio.md](../audio.md) describe carried-over test cases, while the embedded
third-party notices describe the SMAF/MIDI code as project-authored. This is
not proof that the shipped decoder was copied. Confirm the origin and scope
before changing attribution or importing a license. The user's separate choice
about the wording of hqx attribution comments also remains open; do not remove
existing copyright notices. Korean changelog publication proofreading remains
the author's task, including any outstanding historical 0.3.1 correction.

<a id="implementation-watches-and-decisions-that-do-not-authorize-implementation"></a>

### Watches and decisions that do not authorize implementation

Existing compatibility watches retain their trigger rather than implying a
new failing game: KTF Jlet slots 19–25, database enumeration, packaged record
width mismatch, startup paint exceptions, and the eight-round paint suppression
window; SKT Jlet soft-key constants disagreeing with delivered codes; LGT's
six-pair action mapping and one build's 24-row origin convention; missing
Graphics facade members and widget painting. The owning source and platform
documents remain the evidence. A WIPI contract must be checked against the
specification before assigning an undocumented slot or code.

The old non-`-play` KTF pixel-count discrepancy has no identified current
reproducer. A fixed manual-clock origin already removed an earlier first-frame
noise source; see [testing.md](testing.md#implementation-testing), "The KTF floor is now zero". Treat
the older 38,507 versus 40,089 counts as unverified historical evidence until
the same archive, fresh saves, and one build reproduce them. Audio event counts
also vary between runs and alone cannot establish an audible regression.

`importsaves` intentionally searches deeper than Host discovery. A narrower scan
could prevent legitimate external saves from matching; change it only after an
unwanted import is demonstrated. JVM semantic gaps and KTF graphics-state and
shutdown-callback limitations remain bounded by their documented callers,
not by a goal of completing every API.

The following earlier decisions remain closed: partial quick saves are not
shipped; image dimensions did not predict the missing SKT screen choice; the
encrypted DRM payloads are recognized but not decrypted; subscriber-bound
license failures are not emulator defects; the observed carrier-authentication
case did not justify a replacement server.
Their evidence is in [architecture.md](../architecture.md), [skvm.md](skt.md#implementation-skvm-and-the-skt-platform),
and [lgt.md](lgt.md#implementation-the-lgt-platform). They are not pending implementation entries.

Per-title exceptions and deterministic replay remain conditional proposals.
Before introducing exceptions, count affected titles and seek a platform rule;
any justified exceptions belong in ignored runtime data, declare assumptions
that are checked, and count their activations. Replay needs an actual diagnostic
blocked by nondeterministic `-play`; recording a network response alone does not
justify recording clocks, input, responses, and scheduler choices together.

The proposed common runtime extraction is also superseded: audio,
framebuffer, saves and glyphs are already shared. The caller contracts for the
common origin and rate of audio `Play` and `Advance` timestamps, borrowed
buffers and callback reentrancy are recorded in
[shared runtime contracts](../architecture.md#services-shared-runtime-service-contracts);
no additional layer is required for that documentation task.

Unqualified local reports about phone speed, PC keypad presentation, and sound
remain requests for reproduction details. The old device-error report likewise
needs exact error text, APK versus browser access, and stored screen dimensions.
No fixed decoder bug or new keypad implementation proves those reports resolved.

<a id="implementation-validation-of-this-review"></a>

### Validation of this review

`make test` passed on this checkout, including all 192 Node tests. This includes
the source-backed closure tests named above; local game probes remain opt-in.
No new full-corpus run, physical-device check, or release archive build was
performed. The review changes documentation and the ignored local plan only.

The previous GNEX exclusion was superseded by the user on 2026-09-11. SGS
work completed before the user paused development is in
[PR #136](https://github.com/movingwoo/wfeature/pull/136); full compatibility
remains incomplete. Further SGS implementation is paused until the user resumes it.


<a id="state-quick-saves-and-offline-authentication-compatibility"></a>

## State and authentication research (2026-09-11; quick saves later paused)

Superseded for quick saves: quick save/load is implemented; see
[architecture](../architecture.md#quick-load-and-ordinary-saves).

Research date: 2026-09-11. Quick saves remain an implementation project. The
authentication implementation now supports two KTF certificate formats, an embedded
subscriber fallback, recognized SKT license checks, an LGT cached-result layout,
an LGT encrypted 58-byte certificate, and a bounded LGT local notification/empty-save
protocol. [authentication.md](../authentication.md)
describes the current behavior, including automatic defaults and the empty-file
regression fix. The first-slice opt-in design below is historical.
This request reopens the previous quick-save deferral and the cost-based
rejection of a common authentication feature.
The local plan names the deliverables; this document preserves their evidence,
implementation sequence, and acceptance conditions.

This review read current execution, session, save, network, and provisioning
code, compared prior investigations, and fetched the WIPI network specification.
This initial design review performed no game-corpus sweep or snapshot-restoration
experiment. The subsequent [authentication case inventory](authentication.md#cases-authentication-case-inventory)
records a fresh corpus census and controlled authentication experiments separately.
Earlier measurements below remain historical observations unless that inventory
explicitly rechecks them.

<a id="state-quick-save-and-quick-load"></a>

### Quick save and quick load

Priority: Medium. Save running progress and restore it, including after a server
restart. Browser retention only keeps live Go objects and does not satisfy this.

<a id="state-current-obstacles"></a>

#### Current obstacles

| Boundary | Current source and consequence |
|---|---|
| Shared session | `internal/session/session.go` has pause/resume, not state export/import. Pause invokes guest callbacks; it does not freeze every thread without changing guest state. |
| SKT JVM | `internal/jvm/interpreter.go` recursively executes callees. `execution` in `vm.go` keeps a depth and pools of finished frames, not a restorable active frame chain. `frame.go` has locals, operand stack, and PC. |
| KTF mixed execution | `internal/armcore/core.go` creates temporary derived threads for nested calls. `internal/platform/ktf/workers.go` parks through channels while Go callers remain on the stack. Restoring outer registers alone loses nested returns, argument writeback, exception state, and monitor cleanup. |
| LGT execution | `internal/platform/lgt/java_thread.go` retains goroutines and channels too. Prior sampled sleep continuations were simpler than KTF's; they are still not serialized. |
| Memory and services | `Memory.CommittedRegions` inventories spans, not an atomic image of mappings, permissions, and contexts. Audio, images, handles, callbacks, clocks, and saves also live outside guest memory. Frame snapshots and `.wfs` files are not emulator state. |

[architecture.md](../architecture.md#where-a-stopped-game-keeps-its-state) records
the previous experiments: sampled KTF stacks alternated JVM/ARM work across
27 Go frames; an SKT prototype resumed a captured Java chain; LGT's sampled
sleep path needed a result returned through a register. These are feasibility
evidence, not a current complete restorer or park-site inventory. Historical
committed memory was 0.79–2.83 MB across six titles, so correctness should
precede compression work.

<a id="state-implementation-sequence"></a>

#### Implementation sequence

1. **Prove the difficult continuation first.** Inventory safe points for all
   supported execution paths: KTF AOT and earlier modules/BREW, LGT Clet and
   Java, SKT MIDlet and Jlet. Give nested ARM calls stable identities and parent
   relationships. Represent suspended work as explicit continuation records,
   including what executes after a native call returns. An authored KTF fixture
   must park inside nested JVM/ARM calls and resume in a fresh runtime. Resuming
   the original blocked goroutine is not proof.
2. **Establish a common checkpoint barrier.** Stop all guest execution at a
   bounded safe point; account for workers, pending callbacks, monitors and
   waiters, timers, and native calls. Implement active JVM frame capture and
   re-entry and the LGT continuation path. Do not invoke lifecycle callbacks
   merely to take a checkpoint: they can change what is being saved. A missing
   continuation handler must refuse capture explicitly, not create a silently
   incorrect restore.
3. **Define platform state codecs behind the shared session boundary.** Capture
   mappings, permissions and committed bytes, CPU contexts, JVM object identities
   and static fields, class initialization, exception state, scheduler queues,
   clocks/deadlines, random state, file/resource cursors, graphics surfaces,
   input, and audio progression. Rebuild locks, channels, goroutines, closures,
   and decode caches from data; never encode Go pointers or host descriptors.
   Preserve guest dates and remaining waits without charging the save-to-load
   interval as gameplay.
4. **Restore saves coherently with memory.** `backend.SaveStore` only provides
   load/store. Add a state transaction or overlay that captures modified and
   deleted saves and buffered writes. Define rollback of in-game save data
   together with memory; old memory over newer files can corrupt progress.
   Store quick saves in a reserved Host namespace under the configured save
   root, normally `var/savedata`, isolated from guest keys and ordinary `.wfs`
   operations. Use atomic replacement; a failed write or restore must preserve
   the original session and save data.
5. **Add format and Host controls.** Use one bounded, versioned format for debug
   and release, with archive digest, platform/execution variant, compatibility
   version, screen/speed settings, and authentication policy. Validate references,
   sizes, decompression, and identities before allocation or replacement. CLI
   and server use the same session path; the browser sends commands and displays
   results while snapshot bytes stay on the server. Reattach graphics/audio,
   reconcile held input, and preserve save ownership and session admission rules.

<a id="state-acceptance"></a>

#### Acceptance

Restore into a **fresh runtime**, then repeat after a **server restart**. Cover
every supported execution variant above with authored fixtures and local routes;
do not ship an arbitrary subset as the completed feature. Compare uninterrupted
and restored runs for guest-visible memory, frame sequence, input, timers,
sound progression, and save results. Include nested calls, sleeping and
monitor-waiting threads, queued callbacks, and pending writes. A matching static
screenshot is insufficient.

Test repeated save/load, incompatible archives/versions, malformed and oversized
state, interrupted writes, failed restoration, and debug/release compatibility.
Exercise Go handlers and the page client together, then real-browser recovery.
Measure capture/restore latency, size, steady-state overhead, and resource leaks.
Internal prototypes can land in slices; completion requires the full path.
The main risk remains KTF continuation maintenance, not file size.

<a id="state-common-authentication-bypass"></a>

### Common authentication bypass

Priority: High: the target is otherwise playable games blocked by authentication.
Provide an explicitly enabled common offline feature for local archives. Share
policy, selection, diagnostics, and Host integration; adapt each verified scheme
at its responsible platform boundary.

<a id="state-first-implemented-slice"></a>

#### First implemented slice

The shared session accepts `Options.Authentication`, default false. The PWA
remembers the choice per archive under `wfeature:authentication:<path>` and sends
`authentication: true` with a fresh start. The CLI exposes `runktf ... -auth`.
Both reach KTF's `StartSession`; other platform paths report `unsupported` and
keep their existing behavior. `started.authentication` reports `off`,
`unsupported`, or `ktf-certificate-23`, including after a retained session resumes.
A changed preference only affects the next fresh start. It is unrelated to the
server access key, and an applied result is not a claim of playable progress.

Recognition requires a packaged 23-byte `cert.c2s`, its ciphertext checksum,
an eight-character AID, and the existing cipher table plus NUL-terminated
`cert.c2s` and `PHONENUMBER` names in the executable. The archive's file name,
digest, title and instruction addresses do not participate. This is deliberately
stricter than the explicit provisioning command. The other certificate lengths,
missing or damaged data, and executable near matches do not activate it.

KTF snapshots the Host's subscriber number when its client is created. Its C,
HandsetProperty, CLDC System and DMInfo identity APIs use that same snapshot.
Authentication never changes the process-wide defaults. Per-game number overrides
and the short-number branch remain future work.

The certificate save wrapper keeps `db/cert.c2s` and its deletion state in memory.
It exposes the generated certificate even if the backing store has an older
certificate or a persisted deletion. Guest writes and deletions affect only this
run's certificate. Other database removals still persist, while the original
certificate's deletion bit is preserved; ordinary progress uses the existing
save store. With no backing store, the certificate remains usable for that run.
Switching the feature off restores the original certificate behavior. No archive
is rewritten, and the explicit `provision` command retains its persistent behavior.

The 52-byte and 28-byte formats, SKT guest license checks, number-dependent
branches, LGT dial/callback adaptations and application responders remain
unsupported. Network behavior has not changed. Missing content and encrypted
executables are not supplied by this adapter.

<a id="state-first-slice-validation-and-remaining-acceptance"></a>

#### First-slice validation and remaining acceptance

`make test`, `make test-debug`, `go test -race ./internal/...`, and `go vet ./...`
passed. The final browser module suite contains 195 passing tests. Tests cover
executable/certificate near matches, invalid identities, separate concurrent KTF
identity APIs, certificate deletion and write isolation, unrelated save persistence,
protocol result reporting, and retained-session policy. Debug and release CLI and
server binaries build successfully.

A local 23-byte case stops at error `3001` with the option off and reaches the
title and scenario selection with it on, in both build profiles. The extended
debug route also reaches the new/continue menu.
A run seeded from the off run's saved certificate deletion also reaches those menus
with the option on while preserving the original deletion ledger and certificate
bytes. The current 52-byte case and a working 28-byte control report unsupported.
The three source archives retain their recorded SHA-256 values. Run specifications,
frames and diagnostics are under the research directory's `cases/*/feature-*` paths.

Chromium and WebKit drove the real PWA and Go server: per-archive persistence,
recognized application, preference changes during play, reload/resume retaining
the original policy, a fresh start with the option off, and an unsupported SKT
control. Both had no page errors. Evidence is in
`var/acceptance/auth/2026-09-11/implementation-browser/`.

The initial extended new-game route reached the new/continue menu, then exceeded
its 45-second Host timeout before the next capture. The follow-up below establishes
playable input and save/restart separately from that batch timeout.

The initial full-suite invocation also found the earlier investigation's loose
Go overlay files under `build/`. A local nested module excluded those diagnostic
fragments from `go test ./...` without changing production source or the overlay
bytes, and all four checks then passed.

<a id="state-gameplay-and-save-cycle-follow-up"></a>

#### Gameplay and save-cycle follow-up

The follow-up used the current worktree, including the separately implemented
KTF worker paint ownership and LGT key-action corrections. It changed no production
source. Builds and probes used a separate ignored directory so another session's
debug/release binaries were not overwritten. The pre-existing modified files were
fingerprinted before work and checked again at handoff.

A fresh save root reached character creation, field movement, combat with visible
damage, and the next quest notice in 1,550 ticks. Small batches around interactive
menus made the route observable. Both the authentication adapter and explicit
certificate provisioning had exceeded the earlier 45-second batch deadline; that
deadline alone does not establish an authentication failure. Replaying the original
command sequence on this build with a 180-second limit completed all ten captures
and exited normally in 103.947 seconds. The current route exceeds the old probe
budget; it does not require an authentication or runtime patch to finish.

The prologue's save command deliberately says that the current map cannot be saved.
That behavior remains intact. The normal-map save cycle therefore used a private
copy of an existing save. Debug loaded it, opened the save menu, received the guest's
save-complete notice, and wrote a changed 1,250-byte `db/save0.dat`. A fresh release
process loaded those persisted bytes and accepted movement. Release then moved the
character north and saved again. Another fresh debug process loaded that file and
restored the changed character position. Both profile directions used the same
subscriber number and authentication setting.

The original user save was never passed as the probe's writable store. SHA-256
checks cover every source save file and each seed copy. The saved empty certificate
and its deletion ledger stayed byte-identical through both writes and both loads;
the adapter's certificate remained session-local. No save-store error or uncaught
guest exception was reported on these completed routes.

`make test`, `make test-debug`, `go test -race ./internal/...`, and `go vet ./...`
passed again with the other session's fixes present. The preceding Chromium/WebKit
checks remain the browser evidence; this follow-up exercised CLI gameplay and
persisted reloads. Commands, replies, screenshots, diagnostics, seed fingerprints,
and replay scripts are in `var/acceptance/auth/2026-09-11/cases/` under the
23-byte case's `followup-*` directories. The local plan now advances to the other
authentication schemes; it does not mark the common feature complete.

<a id="state-mechanisms-to-distinguish"></a>

#### Mechanisms to distinguish

Read [authentication case history](authentication.md#cases-authentication-case-inventory) before implementing an
adapter. It separates failure-dependent routes, mandatory identity checks,
different certificate formats, and authentication-looking storage/content issues.

| Mechanism | Evidence and work |
|---|---|
| Number-dependent offline branch | `internal/wipic/properties.go` has process-wide `PHONENUMBER`/`MIN`. Earlier KTF cases require incompatible short/full-length numbers. KTF now snapshots its identity; per-session overrides and recognized short-number selection remain open. |
| Packaged certificate | `internal/platform/ktf/provision.go` recognizes one format and produces replacement data; the session adapter now reuses it behind stricter executable recognition and a reversible certificate save wrapper. This is not a universal certificate format. |
| Shared guest license library | [skvm.md](skt.md#implementation-five-titles-check-their-licence-against-the-handsets-number) records five archives with one number/service-ID/key check under different, sometimes obfuscated class names. Reconfirm its shape and implement a narrowly recognized adapter or transformed guest check. Never change general string equality, hashing, or all boolean returns. |
| Refusal hides an offline route | KTF/LGT C calls report failure through callbacks; Java `Network.connect` fails and SKT `Connector` raises a guest exception. Earlier LGT experiments reached an offline route by accepting a dial and refusing later operations. Reproduce those routes and cover the complete follow-on path before enabling the scheme. |
| Application response or content required | There is no generic responder. Record bounded requests and the response fields consumed by the caller, then implement an in-process responder for recognized protocols. Authentication success cannot supply missing game data. |
| Encrypted executable payload | `internal/platform/detect/drm.go` recognizes DCF containers without decrypting them. Count separately and establish format/key prerequisites: a runtime adapter cannot execute still-encrypted code. Report this limitation explicitly. |

Preserve failure-dependent paths as controls: an earlier LGT certificate prompt
reaches its menu after the player accepts the attempt and dismisses the failed
connection. Other LGT paths need the initial dial to succeed before a later
failure exposes their offline route. These outcomes cannot share an unconditional
success response. See [network.md](../network.md#a-certificate-is-the-same-gate-under-another-name-and-it-is-not-a-wall)
and its following investigation.

Before classifying a full archive as an authentication gate, check the resources
the guest actually requests, including indirect resource tables. Archive size
alone is not proof of completeness. Compare fresh and existing saves as well:
packaged database loading and stale saved records have previously produced
download prompts without requiring a network authentication fix.

**One old prerequisite is already done.** The experiment in
[network.md](../network.md#the-sweep-that-measured-it-and-what-it-found) failed at
LGT slot `0x7d0`. Current `wipic.go` identifies it as `MC_netSocket` and refuses
it normally; `TestSocketIsRefusedLikeTheRestOfTheNetworkBlock` checks resolution
and refusal. Repeat the experiment against current code instead of adding that
slot again. KTF's older synchronous-only C refusal is also obsolete:
`wipic_net.go` already queues failure callbacks with bounds and cancellation.

The [WIPI C network contract](https://mirusu400.github.io/wipi-wiki/c-api/network.md)
separates accepting an attempt from its callback result and scopes network access
and close to an application. The
[Java Network contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msf/io/Network.md)
returns 0 for existing access, 1 for new access, and -1 for failure. These are
transport contracts, not a standard publisher-license API. Both pages were
fetched directly from the specification site on the research date.

<a id="state-implementation-sequence-1"></a>

#### Implementation sequence

1. **Build a current cause inventory.** Drive fresh and existing saves past
   dialogs with routes; distinguish real gates from prompts, missing APIs,
   and ordinary exits. Record archive digest, execution variant, scheme,
   stopping call, and the scene beyond the gate. Reuse acceptance records,
   diagnostics, and certificate code. Per-game records stay in ignored data;
   historical counts are not today's acceptance target.
2. **Introduce common policy with session-local state.** Configure the shared
   session path and adapt platform identity, certificate reads, guest checks,
   and network calls below the Host. Preserve ordinary behavior when disabled.
   Recognize formats/protocols/code shapes, not title names. Test near-matching
   non-authentication code. Count activations and failed assumptions through
   backend diagnostics; unknown schemes must not silently use another scheme.
   No separate environment, external service, or core cgo is needed.
3. **Implement recognized schemes in order.** Start with existing certificate
   and identity mechanisms, then the shared SKT check and LGT offline-route
   cases. Retest the current socket fallback before adding an application
   responder. Cover callback order, cancellation, partial reads, end-of-stream,
   timeouts, and bounded queues as applicable. Keep archives intact and save
   changes reversible. Opposing identities and policies must coexist in
   concurrent sessions without interference.
4. **Expose one feature in both Hosts.** Add a browser game setting and CLI
   option using the same core path, with applied/unsupported results. Include
   policy identity in future quick saves so loading restores the same guest
   environment. Game authentication is separate from the server's access key.

<a id="state-acceptance-1"></a>

#### Acceptance

A dismissed authentication screen is not completion. Every recognized scheme
needs authored positive and negative cases and local routes reaching playable
input, saving, and restart from that save. Compare the affected set with the
feature off/on and controls among working archives on all platforms. Test
concurrent opposing identities, cancellation, malformed inputs, bounded waits,
and restoration of original saves. Exercise the Go protocol and browser setting
together and verify both build profiles. State supported mechanisms explicitly;
do not claim arbitrary unknown authentication or encrypted content works.

<a id="state-research-validation"></a>

### Research validation

Focused existing Go tests passed for KTF/LGT network callbacks and cancellation,
LGT socket refusal, SKT Connector refusal and class resolution, committed memory
enumeration, and ARM suspension/derived contexts. These prove current building
blocks, not bypass or snapshot restoration. The initial design review changed
no runtime behavior, game archive, save data, or network policy. The subsequent
case investigation uses isolated research saves and a diagnostic build overlay;
its scope and results are recorded in [authentication case history](authentication.md#cases-authentication-case-inventory).

## Compatibility work scope (2026-09-12)

The work started at `2b56bad`. It covered KTF native loading, supported Host
keyboard/IME entry, LGT dispatch-aware scanning, KTF/SKT API call evidence,
SKT saves and sound, settling behavior, WebKit decoding, screen selection,
and shared service contracts. Quick saves, general PWA expansion, mobile
distribution, optional networking, performance projects, and paused SGS work
were outside that compatibility effort.

Acceptance required authored regressions, shared Host integration where relevant,
ordinary and debug tests, internal race tests, and vet. Startup did not establish
gameplay, saves, or audible playback. Missing archives and human observations
remained unresolved. The platform and testing histories preserve the outcomes.
Temporary worker assignments and publication sequencing are retired.

<a id="quick-save-implementation-measurements"></a>

## Quick save implementation measurements (2026-10-01 to 2026-10-02)

The KTF checkpoint was built in component steps against source `49bacd0`, and
LGT followed on 2026-10-02. [Testing](../testing.md) keeps the route-level
acceptance and the final loading-route cost of the whole feature (0.00%, 0.22%
and 1.77% over three routes). The component measurements below were taken on
the way and are kept here because their raw samples lived in temporary
directories that no longer exist.

### Design constraints that held

- Capture never goes through `Session.Pause`. That path runs guest lifecycle
  callbacks — MIDP lets `pauseApp` release resources, and a paused application
  still receives asynchronous events — so it changes the state being captured.
  Capture waits for a boundary between whole rounds instead and runs no guest
  code.
- Records are explicit codecs, never an automatic encoding of the runtime's
  structs. `encoding/gob` drops function and channel fields and does not keep a
  cyclic object graph, and Go locks, channels, goroutines and Host bindings are
  rebuilt from records rather than stored. A continuation or payload kind
  without a codec refuses the capture by name.
- A load prepares a detached runtime and validates it before the running
  session is touched, and the displaced session is not closed, because closing
  runs guest exit code that may write a save.

### Component costs

Each figure is the median of six serial A/B pairs that alternated order, at one
Go CPU, with identical benchmark source on both sides and no other build or
test running. They measure single operations, not frame rates.

| Fixed work | Before | After | Change |
| --- | ---: | ---: | --- |
| Two short nested ARM calls | 550.55 ns | 558.25 ns | +1.40%; 456 B to 520 B per pair |
| Supervisor crossing, four filler instructions | 212.60 ns | 213.15 ns | +0.26% |
| Supervisor crossing, 32 filler instructions | 348.95 ns | 348.65 ns | -0.09% |
| Native return-scope bookkeeping alone | 152.90 ns | 179.55 ns | +17.43% (+26.65 ns); 24 B to 32 B, one allocation |
| JVM bytecode static call | 336.30 ns | 341.00 ns | +1.40% |
| JVM static native call | 280.50 ns | 291.15 ns | +3.80% |
| JVM instance native call | 339.65 ns | 345.10 ns | +1.60% |
| Java `Math.abs` crossing | 357.00 ns | 361.65 ns | +1.30%; 172 B and four allocations on both sides |

The JVM rows are per call, 1,000 guest calls per operation, and measure the
native-depth counter; their allocation counts and bytes did not change. The
nested-call pair's extra 64 bytes is a real allocation cost even though its
time barely moved.

Real archives with continuation bookkeeping enabled, against unchanged
`49bacd0`, with saves in memory. Instruction counts and guest time were
identical within each archive:

| Archive SHA-256 prefix | Ticks | Guest instructions | Before, ns/step | After, ns/step | Change |
| --- | ---: | ---: | ---: | ---: | ---: |
| `23919eb33365` | 300 | 1,258,355,092 | 7.650 | 7.710 | +0.78% |
| `902466ac7e9e` | 300 | 7,153,206 | 75.910 | 76.755 | +1.11% |
| `e40b5b8196d2` | 300 | 89,485,668 | 4.420 | 4.445 | +0.57% |
| `cc2c96d671cd` | 2,000 | 38,697,351 | 33.200 | 33.175 | -0.08% |
| `aa3fcba4598b` | 2,000 | 6,929,494 | 41.680 | 41.695 | +0.04% |

The first three are loading routes with no parked worker at the sampled points;
the last two contain parked workers in the continuation inventory. None of them
is browser frame rate or restored gameplay, and none showed a sustained
regression.

### Review findings left open

The 2026-10-02 review of the LGT restore path left one finding as accepted:
handles inside the Java tables — a Graphics' surface, a widget's parent, a
thread's runnable — are not cross-checked against the objects they name. Every
consumer was read and answers a wrong handle with an error or a contained
panic, so it is a refusal question rather than a hang or an allocation. The
same review held the LGT display a record may carry to 1,024 a side and noted
that the KTF native record still accepted 4,096 without checking whether that
size reached a Host buffer. It did: a restored native session takes the
record's screen as its own size. The pre-release check of 2026-10-11 held it
to 1,024 as well; see [KTF](../ktf.md#quick-save-and-quick-load).

The quick load of that period also replaced the whole save folder from a copy
in the slot. That writer, the staged re-read added to it after review, and the
timings taken of both were removed once a load stopped putting saves back;
[testing](../testing.md#checkpoint-adoption-blocker) records the defect that
the replacement exposed and what of its locking remains.


<a id="snapshot-feasibility"></a>

## Earlier snapshot feasibility record

This record predates the latest pause and is retained for its measurements.
Quick save/load was built afterwards for KTF, LGT and SKT; the maintained
contract is in [architecture](../architecture.md#quick-load-and-ordinary-saves),
and this is not an implementation plan.

### Where a stopped game keeps its state

A quick save writes a running session to bytes and later starts an identical
one from them. At this point it had been measured across all three platforms and
**deliberately not built**. The evidence concerns where a stopped game's state
lives rather than the container format. On 2026-09-11 the user reopened it as an
implementation project; [state and authentication
research](#state-quick-save-and-quick-load) records that source review,
implementation sequence and acceptance path.

State that lives in **data** can be written out. Guest memory is data: a page
table of 4 KiB pages, and `Memory.CommittedRegions` reports exactly the pages
that have storage. An `armcore.Thread`'s `Context` — registers, CPSR, entry
stack — is data, and so is a JVM object graph. State on the **Go call stack**
cannot be written out, and that is where the platforms diverge: `frame` is a
local of `vm.execute` and the invoke path recurses into Go, so a Java call
chain's depth is Go stack depth, and `execution` keeps a counter and a free list
rather than a list of active frames. The question for each platform is
therefore what is on the Go stack when a guest thread comes to rest.

- **LGT is the cheap case, because its Java is compiled ahead of time and its
  Java state is therefore in guest memory.** A Java worker parks inside
  `javaThreadSleep`, which ends in `return 0, worker.park()`; the twelve Go
  frames above it are stateless dispatch, and the only work left in the whole
  chain after the park is `thread.SetRegister(0, result)` at the tail of
  `callJavaMethod`. Those frames can be thrown away: recreate the goroutine from
  the saved context, write the result register, resume. Titles with no guest
  thread are safe at every tick boundary; titles with one were safe at **none**
  of 300 measured boundaries until the park itself is declared a safe point.
- **SKT is possible but needs machinery.** Its guest threads are free-running
  goroutines, and over 66,558 observations across five titles the chain between
  a thread's entry and where it rested was **purely `vm.execute` frames every
  time**, at most seven deep. A prototype captured such a stack and resumed it in
  a VM that had executed nothing, matching an uninterrupted run — the parent
  frame is already in a resumable shape, because the interpreter reads an
  invoke's operands and pops its arguments before it calls, so resuming a caller
  is "push the result and continue". Making frames walkable costs a pointer
  write per call: allocation counts do not move and the time signal is +0.4% to
  +2.0% against a noise floor of ±1.2%. Stopping a thread needs no new check —
  the step-ceiling test at the top of the interpreter loop is already at an
  instruction boundary with the operand stack settled.
- **KTF is the expensive case.** Its stack alternates engines: JVM native
  dispatch, an ARM run, more dispatch, another ARM run, 27 frames deep at a
  `Thread.sleep`. Those Go layers are not stateless the way LGT's are — after
  the inner call returns, one writes eight bytes to a guest address, another
  converts registers to a typed value by descriptor and repairs an exception
  handler head, another leaves a monitor. The layer alphabet is closed and small
  (12,941 samples produced six distinct sequences over 26 frame kinds; the park
  sites are five and nesting is bounded at 64), and bytecode frames cannot appear
  at all because the platform builds its VM with no class source. So the stack
  could be recorded as data and re-entered layer by layer. Two things stand in
  the way. A derived `armcore.Thread` is registered nowhere and has no parent
  link, so **a parked worker's guest registers are reachable only from the
  sleeping goroutine's own Go frames** — measured at rest, `currentThread` is
  nil and every worker's ARM thread still reads `pc=0`. That is additive to fix.
  The second is not: roughly eight of the 26 frame kinds would need a resume
  entry point, and every future parkable supervisor call would have to add one,
  where forgetting is a silently wrong restore rather than a failure.

**Snapshot size was not the problem.** Committed memory is 1–3% of what is
mapped (0.79–2.83 MB against ~92 MB across six titles), `compress/flate` reduces
it another 2.2–3.7x, and the committed page set is fixed after boot: ten times
the ticks added no pages and changed the compressed size by five bytes.
Dirty-page tracking is therefore worthless — filtering out committed pages that
are still all zero wins 0.9% or loses, because the compressor already handles
them, so it would add cost to every guest store for nothing — and the per-page
decode caches are never carried, because they rebuild from the page bytes and
are sometimes larger than the whole snapshot.

The snapshot stays on the server, because the page does not emulate: the server
runs the game, so a snapshot never crosses the socket. The inbound message limit
is 1 MiB, and a compressed snapshot had already been measured above it.

**Why it was not built then.** Every platform could be made to work, but KTF's
part left a permanent maintenance surface with a silent failure mode, and the
project would not ship a quick save that works on some titles and not others.
The record named two reasons to reopen it: an execution-model change that made
recording the layer stack incidental, or the feature becoming worth that hazard;
the measurements above were to be the starting point.

<a id="quicksave-review-2026-10-07"></a>

## Quick-save and quick-load review — 2026-10-07

Reviewed revision: `2ca242e5c56c8e4b025efc858d03dc6ef8f09d71`.
Scope: KTF Java/AOT, older KTF modules, native KTF packages, LGT Clet/Java,
SKT Java and SGS, their shared storage boundary, CLI, server and page.

The acceptance rule is [ordinary saves come first](../architecture.md#quick-load-and-ordinary-saves).
A load restores execution, retains current saves and deletions, and rebuilds
host storage objects from current data. A quick step may persist writes the
game has already issued, but must not overwrite a newer save with a stale host
buffer. A refused step must preserve the running game and its pending writes.
The older design, in which a quick load replaced the whole save folder, is
historical; see [testing](../testing.md#checkpoint-adoption-blocker) and
[the implementation record](#quick-save-implementation-measurements).

**All eight findings were repaired and verified in the follow-up change.** The
review reproduced one P1, six P2 and one P3 on a revision that passed the normal
gates and bounded local routes. The findings describe that revision;
[the repair record](#quicksave-review-2026-10-07-repairs-and-verification) describes the resulting behavior
and regression coverage. Save and checkpoint formats did not change.

<a id="quicksave-review-2026-10-07-repairs-and-verification"></a>

### Repairs and verification

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

<a id="quicksave-review-2026-10-07-findings"></a>

### Findings

<a id="quicksave-review-2026-10-07-q1--p1-skt-quick-steps-overwrite-a-newer-file-with-an-older-buffer"></a>

#### Q1 — P1: SKT quick steps overwrite a newer file with an older buffer

Location: [checkpoint_storage.go](../../internal/platform/skt/checkpoint_storage.go),
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

<a id="quicksave-review-2026-10-07-q2--p2-lgt-misses-a-pending-filedatabase-collision"></a>

#### Q2 — P2: LGT misses a pending File/DataBase collision

Location: [checkpoint_storage.go](../../internal/platform/lgt/checkpoint_storage.go),
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

<a id="quicksave-review-2026-10-07-q3--p2-lgt-read-failures-lose-pending-writes-or-disable-the-live-game"></a>

#### Q3 — P2: LGT read failures lose pending writes or disable the live game

Locations: [checkpoint_storage.go](../../internal/platform/lgt/checkpoint_storage.go),
`storeIssuedWrites`, lines 191–194 and 237–240;
[wipic_file.go](../../internal/platform/lgt/wipic_file.go), `storeFile`, lines 629–634.

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

<a id="quicksave-review-2026-10-07-q4--p2-closing-a-restored-skt-rms-handle-reverses-a-later-deletion"></a>

#### Q4 — P2: closing a restored SKT RMS handle reverses a later deletion

Locations: [rms.go](../../internal/platform/skt/rms.go), `persistStore`, lines
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

<a id="quicksave-review-2026-10-07-q5--p2-linked-owner-folders-bypass-save-directory-exclusion"></a>

#### Q5 — P2: linked owner folders bypass save-directory exclusion

Locations: [save_lock.go](../../internal/backend/save_lock.go), `acquireSaveLock`,
lines 46–58; [save_replace.go](../../internal/backend/save_replace.go),
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

<a id="quicksave-review-2026-10-07-q6--p2-constructed-skt-audio-state-hangs-execution-after-adoption"></a>

#### Q6 — P2: constructed SKT audio state hangs execution after adoption

Locations: [checkpoint.go](../../internal/platform/skt/checkpoint.go),
`javaCheckpointState.validate`, lines 93–97;
[script_checkpoint.go](../../internal/platform/skt/script_checkpoint.go),
`scriptCheckpointState.validate`, lines 127–131.

Both validators accept a repeating one-millisecond sound starting at zero
with the guest clock one hundred years later. Preparing and committing this
record succeeds. The next Java audio advance or SGS tick tries to replay
3,153,600,000,000 cycles in `backend.Audio.advanceSound`.

Separate child tests for Java and SGS time out after three seconds with their
stacks inside that loop, after successful commit. The existing clock-origin
check does not bound the work required to catch up. KTF and LGT have a separate
pending-audio-work check that SKT lacks.

This requires a deliberately constructed, internally inconsistent state;
random file corruption is normally caught by the envelope checksum. The
component tests construct the record directly, and such bytes can also be
wrapped in an envelope with a recomputed checksum, because the checksum is an
integrity check, not authentication. No ordinary captured local game produced
this state, and the timeout proof is at the platform boundary, not in a browser.

Reproductions: `TestReviewSKTCheckpointRejectsUnboundedAudioCatchup`,
`TestReviewSKTCheckpointAudioNextTickJava`, and
`TestReviewSKTCheckpointAudioNextTickSGS`.

Correction: reject excessive pending audio cycles/events before adoption,
using the restored guest clock and actual audio progress.

<a id="quicksave-review-2026-10-07-q7--p2-a-restored-picture-can-overtake-its-browser-reset"></a>

#### Q7 — P2: a restored picture can overtake its browser reset

Location: [session.go](../../internal/webhost/session.go), `writeMessages`,
lines 1504–1508. Browser consequence: [app.js](../../web/app.js), lines 589–595.

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

<a id="quicksave-review-2026-10-07-q8--p3-reconnecting-after-quick-load-restores-stale-speed-metadata"></a>

#### Q8 — P3: reconnecting after quick load restores stale speed metadata

Locations: [session.go](../../internal/webhost/session.go), `clientSpeed`, lines
662–664, and `resumeGame`; [app.js](../../web/app.js), `sessionStarted`, line 691.

Load a checkpoint at speed 1, change speed to 2, then resume the session. The
runtime remains at 2 but its response reports `restored=true, speed=1`.
`r.started.Speed` was set at quick load and was never updated by the speed
command. The page trusts restored metadata and saves the stale value as the
user's preference, so the display/preference diverges from execution.

Reproduction: `TestReviewHostCheckpointResumeReportsCurrentSpeed` observes
`resume reports restored=true speed=1 while live speed=2`.

Correction: refresh speed metadata from the live session before resume, or
maintain it when speed changes. No save-data loss was observed in this case.

<a id="quicksave-review-2026-10-07-validation-on-the-reviewed-revision"></a>

### Validation on the reviewed revision

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

<a id="quicksave-review-2026-10-07-reproduction-artifacts"></a>

### Reproduction artifacts

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

The overlays contain absolute paths for that checkout and are diagnostic
artifacts, not portable fixtures; the trigger sequences and results above are
the durable record if the ignored artifacts are removed. The repairs turned
these cases into maintained regression tests at the responsible layers.

<a id="maintenance-shared-core-2026-10-31"></a>

## What the three carriers share — 2026-10-31

The WIPI-C engine (`docs/history/skt.md`, "One WIPI-C engine for KTF and SKT")
removed the one place two carriers ran copies of the same code. A scope review
the day before had sorted the rest into what the three carriers could share
without any title seeing a difference and what would change one of them. This
step is the first kind only: one interface over the session's six platform
sessions, and the pieces below the call boundary that each carrier had written
for itself. Nothing a title reads, draws or stores changed; the two exceptions
are listed where they occur.

<a id="maintenance-shared-core-2026-10-31-session"></a>

### The session asks one interface

`internal/session.Session` held six platform pointers — KTF's descriptor
package, its earlier native package, an SKT Clet, LGT, an SKT MIDlet and an SKT
script — and answered every Host call through a switch over them: 100 branches
in `session.go`, `checkpoint.go` and `textinput.go`. A branch left out compiles,
and three had been: a Clet's ending was a failure on every path but its tick,
its authentication read as unsupported (both fixed with the engine), and it had
no cheat engine at all, because `Session.Cheat` had no Clet branch.

Each kind of platform session now has an adapter in `engine.go`, and every
question the session asks is a method of one `engine` interface, so a question
added for one platform does not compile until all six answer it. A platform
with nothing to say answers so in its own adapter, with the reason beside it:
the earlier native package has no pause entry and no vibrator, a Clet and LGT
have no repeat event, the MIDP runtimes own their surface's size. What stays
per platform stays per platform: the holds a restored checkpoint may not carry,
LGT's press-and-release keys, the MIDP runtime's own pad. Restoring a
checkpoint prepares and commits one platform session per variant
(`restoreEngine`), and the session it displaces has to be the same kind
(`displaced`), as before.

`Session.UncaughtCallbacks` is the one new question: the debug report used to
reach the count of callbacks an uncaught exception ended through `KTF()` and
`LGT()`, so an SKT MIDlet's was never shown. It is now shown for every platform
that runs Java.

<a id="maintenance-shared-core-2026-10-31-shared"></a>

### Shared below the call boundary

| What | Now | Was |
| --- | --- | --- |
| The cheat engine's adapter for a guest on the ARM core | `internal/platform/armcheat` | Three copies (KTF, the earlier KTF package, LGT); a Clet had none |
| The save-owner collision scan behind `checkgames` | `gameroot.OwnerCollisions` | Three copies; what tells two titles apart is the AID on KTF and LGT and the main class on SKT |
| The count of callbacks an uncaught exception ended | `backend.UncaughtCallbacks` | Three copies; SKT's callbacks run on goroutines and it keeps its own lock around the record |
| Lenient EUC-KR text, the handset's default charset | `backend.DecodeEUCKR`, `backend.EncodeEUCKR` | Four copies: KTF, LGT, SKT Java and the WIPI-C engine |
| Display's game-action table | `wipi.GameAction`, `wipi.GameActionKey` | KTF's table and LGT's two switches, the same twelve pairs; an SKT Jlet keeps the vendor's codes and its own table |
| An arena's free list | `wipic.TakeFirstFit`, `wipic.InsertFreeBlock` | The engine's arena and LGT's ran the same first-fit loop and coalesced the same way |
| A C library's `struct tm` | `wipic.TimeFields`, `wipic.TimeRecord` | The Clet library's localtime, gmtime and mktime and LGT's localtime each built it |
| LGT's polygon fill | `curve.FillPolygonWithin` | A copy of the rule the KTF runtime fills with |

Each arena keeps what is its own around the shared list: 4-byte alignment and
a release at the top that lowers the cursor on the engine's, 8-byte alignment,
the sizes a release is looked up by and a batch release on LGT's. No record a
checkpoint writes changed shape. `TestTheSharedFreeListPlacesBlocksAsEachArenaDid`
runs the two copies as they were beside the shared list over random allocation
and release sequences at both alignments and requires the same addresses and
the same list after every step.

The one place a title could see a difference:

- **LGT's polygon fill is the shared one.** Its copy worked a crossing out in
  machine integers, which overflow when a guest puts vertices at both ends of
  int32 on both axes; the shared rule works that case in a `big.Int`. Every
  other polygon covers the same pixels, which the randomized clipping test
  (`TestClippedPolygonDrawsWhatTheFullWalkDrew`) still checks against its own
  full walk.

LGT's stub region is still not marked as code a search keeps out of, as the
engine's is. That is a difference in what a search sweeps and was left as it
was.

<a id="maintenance-shared-core-2026-10-31-apart"></a>

### Left apart, and why

These do the same job on more than one carrier and were read for sharing.
Each differs in something a title can see, so making them one means choosing
which carrier is right, which this step does not do.

- **The C library's functions.** LGT's (`lgt/stdlib.go`) and the Clet's
  (`skt/clet/clet_clib.go`) implement the same ANSI functions under different
  slot tables and different limits: LGT ends a counted read where a mapping
  ends and refuses a length over 16 MiB, the Clet's faults on the read and
  bounds strings at the engine's string limit. A title can tell them apart in
  `atoi`, whose leading space is space, tab, CR and LF on LGT and also VT and FF
  on the Clet, and which answers 0 on LGT for digits past int64 where the Clet
  saturates; in `localtime(NULL)`, which reads the clock on LGT and answers
  NULL on the Clet; and in `strncmp` across an unmapped page. Only `struct tm`
  is shared.
- **Guest string readers and the printf glue.** The formatter is already
  shared (`internal/wipic`); the readers around it are not the same reader.
  LGT's reads a null pointer as an empty string and bounds a name at 4,096
  bytes, and its counted reader stops at an unreadable byte; the engine's
  faults on both and bounds a string at its own limit.
- **File names and the lists behind them.** The rules are one contract
  ([WIPI storage](../wipi-storage.md)), but the lists are kept in each
  platform's own format — `db/.removed` and `db/.dirs` on the engine,
  `fs/.removed`, `.created` and `.directories` with earlier lower-case entries
  on LGT — and only LGT reads a backslash as a separator in every list key.
  Moving one onto another's is a save migration.
- **WIPI Java `DataBase`.** Three on-disk layouts (KTF's `jdb/.sizes`, SKT's
  strict `jdb/.index`, LGT's versioned `.db` and `.index`), and KTF sorts
  records with an insertion sort that calls a title's comparator a different
  number of times than the merge sort LGT and SKT use.
- **`Clip` and `Player`.** The state is already the shared `backend.Audio`'s;
  how a clip is owned and how its events reach a listener differ by design
  ([audio](../audio.md)).

The graphics primitives, the LCD flush, timers, the network refusal, archive
limits and the Java differences the scope review listed (`showNotify`,
`removeAllCards`, the BASELINE anchor, an unknown handset property,
`URL.find`) change what a title sees by construction and were not touched.
