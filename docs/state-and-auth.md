# Quick saves and offline authentication compatibility

Research date: 2026-09-11. Quick saves remain an implementation project. The
authentication implementation now supports two KTF certificate formats, an embedded
subscriber fallback, recognized SKT license checks, an LGT cached-result layout,
an LGT encrypted 58-byte certificate, and a bounded LGT local notification/empty-save
protocol. [authentication.md](authentication.md)
describes the current behavior, including automatic defaults and the empty-file
regression fix. The first-slice opt-in design below is historical.
This request reopens the previous quick-save deferral and the cost-based
rejection of a common authentication feature.
The local plan names the deliverables; this document preserves their evidence,
implementation sequence, and acceptance conditions.

This review read current execution, session, save, network, and provisioning
code, compared prior investigations, and fetched the WIPI network specification.
This initial design review performed no game-corpus sweep or snapshot-restoration
experiment. The subsequent [authentication case inventory](authentication-cases.md)
records a fresh corpus census and controlled authentication experiments separately.
Earlier measurements below remain historical observations unless that inventory
explicitly rechecks them.

## Quick save and quick load

Priority: Medium. Save running progress and restore it, including after a server
restart. Browser retention only keeps live Go objects and does not satisfy this.

### Current obstacles

| Boundary | Current source and consequence |
|---|---|
| Shared session | `internal/session/session.go` has pause/resume, not state export/import. Pause invokes guest callbacks; it does not freeze every thread without changing guest state. |
| SKT JVM | `internal/jvm/interpreter.go` recursively executes callees. `execution` in `vm.go` keeps a depth and pools of finished frames, not a restorable active frame chain. `frame.go` has locals, operand stack, and PC. |
| KTF mixed execution | `internal/armcore/core.go` creates temporary derived threads for nested calls. `internal/platform/ktf/workers.go` parks through channels while Go callers remain on the stack. Restoring outer registers alone loses nested returns, argument writeback, exception state, and monitor cleanup. |
| LGT execution | `internal/platform/lgt/java_thread.go` retains goroutines and channels too. Prior sampled sleep continuations were simpler than KTF's; they are still not serialized. |
| Memory and services | `Memory.CommittedRegions` inventories spans, not an atomic image of mappings, permissions, and contexts. Audio, images, handles, callbacks, clocks, and saves also live outside guest memory. Frame snapshots and `.wfs` files are not emulator state. |

[architecture.md](architecture.md#where-a-stopped-game-keeps-its-state) records
the previous experiments: sampled KTF stacks alternated JVM/ARM work across
27 Go frames; an SKT prototype resumed a captured Java chain; LGT's sampled
sleep path needed a result returned through a register. These are feasibility
evidence, not a current complete restorer or park-site inventory. Historical
committed memory was 0.79–2.83 MB across six titles, so correctness should
precede compression work.

### Implementation sequence

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

### Acceptance

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

## Common authentication bypass

Priority: High: the target is otherwise playable games blocked by authentication.
Provide an explicitly enabled common offline feature for local archives. Share
policy, selection, diagnostics, and Host integration; adapt each verified scheme
at its responsible platform boundary.

### First implemented slice

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

### First-slice validation and remaining acceptance

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

### Gameplay and save-cycle follow-up

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

### Mechanisms to distinguish

Read [authentication-cases.md](authentication-cases.md) before implementing an
adapter. It separates failure-dependent routes, mandatory identity checks,
different certificate formats, and authentication-looking storage/content issues.

| Mechanism | Evidence and work |
|---|---|
| Number-dependent offline branch | `internal/wipic/properties.go` has process-wide `PHONENUMBER`/`MIN`. Earlier KTF cases require incompatible short/full-length numbers. KTF now snapshots its identity; per-session overrides and recognized short-number selection remain open. |
| Packaged certificate | `internal/platform/ktf/provision.go` recognizes one format and produces replacement data; the session adapter now reuses it behind stricter executable recognition and a reversible certificate save wrapper. This is not a universal certificate format. |
| Shared guest license library | [skvm.md](skvm.md#five-titles-check-their-licence-against-the-handsets-number) records five archives with one number/service-ID/key check under different, sometimes obfuscated class names. Reconfirm its shape and implement a narrowly recognized adapter or transformed guest check. Never change general string equality, hashing, or all boolean returns. |
| Refusal hides an offline route | KTF/LGT C calls report failure through callbacks; Java `Network.connect` fails and SKT `Connector` raises a guest exception. Earlier LGT experiments reached an offline route by accepting a dial and refusing later operations. Reproduce those routes and cover the complete follow-on path before enabling the scheme. |
| Application response or content required | There is no generic responder. Record bounded requests and the response fields consumed by the caller, then implement an in-process responder for recognized protocols. Authentication success cannot supply missing game data. |
| Encrypted executable payload | `internal/platform/detect/drm.go` recognizes DCF containers without decrypting them. Count separately and establish format/key prerequisites: a runtime adapter cannot execute still-encrypted code. Report this limitation explicitly. |

Preserve failure-dependent paths as controls: an earlier LGT certificate prompt
reaches its menu after the player accepts the attempt and dismisses the failed
connection. Other LGT paths need the initial dial to succeed before a later
failure exposes their offline route. These outcomes cannot share an unconditional
success response. See [network.md](network.md#a-certificate-is-the-same-gate-under-another-name-and-it-is-not-a-wall)
and its following investigation.

Before classifying a full archive as an authentication gate, check the resources
the guest actually requests, including indirect resource tables. Archive size
alone is not proof of completeness. Compare fresh and existing saves as well:
packaged database loading and stale saved records have previously produced
download prompts without requiring a network authentication fix.

**One old prerequisite is already done.** The experiment in
[network.md](network.md#the-sweep-that-measured-it-and-what-it-found) failed at
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

### Implementation sequence

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

### Acceptance

A dismissed authentication screen is not completion. Every recognized scheme
needs authored positive and negative cases and local routes reaching playable
input, saving, and restart from that save. Compare the affected set with the
feature off/on and controls among working archives on all platforms. Test
concurrent opposing identities, cancellation, malformed inputs, bounded waits,
and restoration of original saves. Exercise the Go protocol and browser setting
together and verify both build profiles. State supported mechanisms explicitly;
do not claim arbitrary unknown authentication or encrypted content works.

## Research validation

Focused existing Go tests passed for KTF/LGT network callbacks and cancellation,
LGT socket refusal, SKT Connector refusal and class resolution, committed memory
enumeration, and ARM suspension/derived contexts. These prove current building
blocks, not bypass or snapshot restoration. The initial design review changed
no runtime behavior, game archive, save data, or network policy. The subsequent
case investigation uses isolated research saves and a diagnostic build overlay;
its scope and results are recorded in [authentication-cases.md](authentication-cases.md).
