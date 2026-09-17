# Maintenance investigation history

This file preserves implementation investigations and dated validation records.
Statements about current behavior, missing APIs, corpus size, and planned work
apply to their original revision; later records may supersede them.
Start with [maintenance.md](../maintenance.md) for the maintained overview.
This consolidation adds no execution or acceptance evidence.


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
- [Earlier snapshot feasibility record](#snapshot-feasibility)

<a id="implementation-maintenance-review-2026-09-10"></a>

## Implementation and validation record

This review reconciles the local work plan with source at `6d62c35`. It is not
a new game-corpus acceptance run. High means an observed execution blocker or
a release decision that needs evidence before closure. Medium covers acceptance
quality, incomplete user-facing validation, and bounded maintenance work. Low
covers optional features, performance experiments, and compatibility gaps with
no demonstrated failing caller. User checks and evidence-triggered watches are
not implementation instructions, regardless of priority.

On 2026-09-11 the user removed the watch list from the local TODO. Its notes
below remain historical evidence, not a second implementation queue. The user
also reopened quick save/load and common authentication bypass as active
projects; [state and authentication research](#state-quick-saves-and-offline-authentication-compatibility) supersedes the earlier deferral
decisions for those two features. Other watch proposals are not reactivated.

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
`var/games/request/` do not exist at this review. Their absence does not prove
that the archives were moved into the acceptance corpus. Recover current paths
and match archive bytes before rerunning the BREW observation or deciding which
previously requested archives to include. Do not move game files during a plan
review. Use the generated records under `var/acceptance/` for dated membership.

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
  that is 6,144,000 bytes per session before encoding. Establish current cost
  before changing ownership. `internal/backend/framebuffer.go` explicitly
  requires retained presentation bytes to be copied.
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
encrypted DRM payloads are recognized
but not decrypted; subscriber-bound license failures are not emulator defects;
the observed carrier-authentication case did not justify a replacement server.
Their evidence is in [architecture.md](../architecture.md), [skvm.md](skt.md#implementation-skvm-and-the-skt-platform),
and [lgt.md](lgt.md#implementation-the-lgt-platform). They are not pending implementation entries.

Per-title exceptions and deterministic replay remain conditional proposals.
Before introducing exceptions, count affected titles and seek a platform rule;
any justified exceptions belong in ignored runtime data, declare assumptions
that are checked, and count their activations. Replay needs an actual diagnostic
blocked by nondeterministic `-play`; recording a network response alone does not
justify recording clocks, input, responses, and scheduler choices together.

The proposed common runtime extraction is also superseded: audio, framebuffer,
saves, and glyphs are shared already. The caller contracts for the common origin and rate of audio `Play` and
`Advance` timestamps, borrowed buffers and callback reentrancy are now recorded
in [shared runtime contracts](../architecture.md#services-shared-runtime-service-contracts). No additional
layer is required for that documentation task.

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
Go overlay files under `build/`. A local nested module excludes those diagnostic
fragments from `go test ./...`; production source and the overlay bytes are
unchanged by that isolation. All four checks then passed.

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


<a id="snapshot-feasibility"></a>

## Earlier snapshot feasibility record

This record predates the latest pause and is retained for its measurements.
Quick save/load remains paused; this is not an active implementation plan.

### Where a stopped game keeps its state

A quick save writes a running session to bytes and starts an identical one from
them later. It was measured across all three platforms and **deliberately not
built**; what follows is the evidence, because the interesting half is not the
container but where a stopped game's state actually lives.

On 2026-09-11 the user reopened this as an implementation project. The feature
is still absent; the earlier decision below is historical, and its risks remain
applicable. [state and authentication research](#state-quick-save-and-quick-load)
records the current source review, implementation sequence, and acceptance path.

State that lives in **data** can be written out. Guest memory is data: a page
table of 4 KiB pages, and `Memory.CommittedRegions` already reports exactly the
pages that have storage. An `armcore.Thread`'s `Context` — registers, CPSR,
entry stack — is data. A JVM object graph is data. State that lives on the **Go
call stack** cannot be written out, and that is where the answer diverges per
platform: `frame` is a local of `vm.execute` and the invoke path recurses into
Go, so a Java call chain's depth is Go stack depth and `execution` keeps a
counter and a free list rather than a list of active frames.

So the question for each platform is what is on the Go stack at the moment a
guest thread comes to rest.

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

**The size of a snapshot was never the problem.** Committed memory is 1-3% of
what is mapped (0.79-2.83 MB against ~92 MB across six titles), `compress/flate`
takes another 2.2-3.7x, and the committed page set is fixed after boot — ten
times the ticks added no pages and changed the compressed size by five bytes.
Two things follow. Dirty-page tracking is worth nothing: filtering out the
committed pages that are still all zero wins 0.9% or loses, because the
compressor already handles them, so it would add cost to every guest store to
buy nothing. And the per-page decode caches are never carried — they rebuild
from the page bytes and are sometimes larger than the whole snapshot.

Where the bytes would live is settled too, and by a fact rather than a
preference: the page does not emulate. The server runs the game, so a snapshot
never crosses the socket — which is as well, since the inbound message limit is
1 MiB and a compressed snapshot has already been measured above it.

**Why it was not built.** Every platform can be made to work, but KTF's third of
it leaves a permanent maintenance surface with a silent failure mode, and the
project would not ship a quick save that works on some titles and not others.
It is reopenable: if the execution model changes for another reason and
recording the layer stack becomes incidental, or if the feature becomes worth
that hazard, the measurements above are the starting point rather than the work.
