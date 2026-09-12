# Maintenance review, 2026-09-10

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
projects; [state-and-auth.md](state-and-auth.md) supersedes the earlier deferral
decisions for those two features. Other watch proposals are not reactivated.

## Work already present

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
  collector is obsolete. See [lgt.md](lgt.md), "Two triggers, and why the second
  one is needed twice over".
- The KTF Graphics table now registers grayscale, stroke-style, and RGB
  component accessors. These names must not remain on a list of missing
  declarations. The former facade task is narrowed to the still-absent
  `dumpRGB565`, `getPixel16`, `setPixel16`, `getImage`, and image constructor
  forms, pending an affected caller and a fresh `apiscan` result. Presence is
  not a claim of complete rendering semantics.
- Completed SKT resource, library, exception, repaint, clipping, Jlet, sound,
  save-probe, display-size, and JVM allocation work is recorded in
  [skvm.md](skvm.md), [jvm.md](jvm.md), and [audio.md](audio.md). The implemented
  3D surface remains a partial renderer; completion of that slice does not
  establish complete 3D emulation.
- CI action pinning, dependency update configuration, archive validation, and
  dated acceptance reporting exist in `.github/`, `internal/tools/distcheck`,
  and `internal/tools/acceptance`. Their completed checklist entries are removed.

## Execution and acceptance follow-up

The BREW loading-screen observation remains open. The earlier investigation
found no remaining trap and no writes to either source picture over 300 frames;
the state gates at `+0x1748` and `+0x3380` were also untouched. The next question
is how the eighteen loaded resources should populate the picture. It is not a
request to invent another platform slot. See [ktf.md](ktf.md), "What writes
this, for a title nothing is unanswered for". The companion title's input and
text rendering work is complete. Text alignment flag `0x200` and the distinction
between font IDs `0x8000` and `0x8001` remain evidence-triggered questions.

The former local paths `var/games/test/errorFile/README.md` and
`var/games/request/` do not exist at this review. Their absence does not prove
that the archives were moved into the acceptance corpus. Recover current paths
and match archive bytes before rerunning the BREW observation or deciding which
previously requested archives to include. Do not move game files during a plan
review. Use the generated records under `var/acceptance/` for dated membership.

`internal/ladder` still uses `StillRuns = 8` and `SettleRounds = 2`. A four-frame
animation with a period around 38 ticks can satisfy the still test, allowing an
animation change to be credited to a key. The KTF rung watches after the last
round; the LGT and SKT rungs leave that round earlier. Increasing the common
round count therefore needs controls on all three platforms. See [ktf.md](ktf.md),
"A key cannot be credited on a screen that drifts" and "Two things worth knowing
about these openings". Truly non-settling animation is an unanswerable grade,
not an emulator defect.

[testing.md](testing.md), "Browser session retention and control" and
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

## Performance and optional features

The following are candidates, not speed claims or approved large rewrites:

- Remeasure the KTF session and ARM supervisor-call allocation changes on an
  idle machine. The original run had load average 127, so only its allocation
  and guest-instruction counts were usable. Use the load probe's `ns_per_step`,
  `BenchmarkCoreFastSupervisorLoop`, and fixed-work JVM call benchmarks.
  See [armcore.md](armcore.md), "The allocation every crossing made", and
  [ktf.md](ktf.md), "The two allocations a busy session made".
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
  [jvm.md](jvm.md), "The three lookups a call did not need, and its last allocation".
- `internal/armcore/backend.go` registers only the interpreter. A native-code
  backend is still an experiment: one target, Thumb first, interpreter fallback,
  no cgo in the core, conformance before use, and a measured improvement beyond
  the old 3.20 ns/instruction no-dispatch result in [armcore.md](armcore.md).
  That number belongs to its original machine and needs a paired local control.
- The unhandled blended-blit mode and two lower-ranked palette-loop shapes
  remain interpreter fallbacks. Reprofile an affected scene before adding a
  recognizer; see [armcore.md](armcore.md), "The eighth shape", and the loop
  census method in [testing.md](testing.md).
- The external-access add-on is absent. The server's access key is already
  implemented; it does not provide router mapping, a tunnel, or a test of
  external reachability. A separate add-on first needs a decision about that
  last claim. See [running.md](running.md), "A key, for a server something
  outside can reach".

## Documentation and provenance

The root README still describes three keypad types and the former size panel,
while the page ships four types and cell editing. `web/README.md` and early
sections of `docs/session.md` also describe the former layout. These are
documentation gaps, not missing keypad implementation.

Historical sections in [testing.md](testing.md) still name the removed
`var/games/test_skt/NOT-WORKING.md` report and state that there is no session
cap or request deadline. Later sections and current source supersede these
claims. Some deliberately-incomplete lists also retain completed features,
including KTF cheat watches and table persistence. The next documentation pass
should distinguish historical observations from current limitations and remove
individual title names already present in older tracked prose. This review
introduces no new title names.

The Host allocation bound remains a separate watch: `internal/webhost/session.go`
reads an archive with `os.ReadFile` before handing it to a bounded loader.
Loader expansion limits do not bound this initial allocation. See
[architecture.md](architecture.md), "What an archive may declare". This review
does not establish an oversized local archive or an observed memory failure.

The SMAF provenance question is still unresolved: `smaf_test.go` and
[audio.md](audio.md) describe carried-over test cases, while the embedded
third-party notices describe the SMAF/MIDI code as project-authored. This is
not proof that the shipped decoder was copied. Confirm the origin and scope
before changing attribution or importing a license. The user's separate choice
about the wording of hqx attribution comments also remains open; do not remove
existing copyright notices. Korean changelog publication proofreading remains
the author's task, including any outstanding historical 0.3.1 correction.

## Watches and decisions that do not authorize implementation

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
noise source; see [testing.md](testing.md), "The KTF floor is now zero". Treat
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
Their evidence is in [architecture.md](architecture.md), [skvm.md](skvm.md),
and [lgt.md](lgt.md). They are not pending implementation entries.

Per-title exceptions and deterministic replay remain conditional proposals.
Before introducing exceptions, count affected titles and seek a platform rule;
any justified exceptions belong in ignored runtime data, declare assumptions
that are checked, and count their activations. Replay needs an actual diagnostic
blocked by nondeterministic `-play`; recording a network response alone does not
justify recording clocks, input, responses, and scheduler choices together.

The proposed common runtime extraction is also superseded: audio, framebuffer,
saves, and glyphs are shared already. The caller contracts for the common origin and rate of audio `Play` and
`Advance` timestamps, borrowed buffers and callback reentrancy are now recorded
in [runtime-service-contracts.md](runtime-service-contracts.md). No additional
layer is required for that documentation task.

Unqualified local reports about phone speed, PC keypad presentation, and sound
remain requests for reproduction details. The old device-error report likewise
needs exact error text, APK versus browser access, and stored screen dimensions.
No fixed decoder bug or new keypad implementation proves those reports resolved.

## Validation of this review

`make test` passed on this checkout, including all 192 Node tests. This includes
the source-backed closure tests named above; local game probes remain opt-in.
No new full-corpus run, physical-device check, or release archive build was
performed. The review changes documentation and the ignored local plan only.

The previous GNEX exclusion was superseded by the user on 2026-09-11. SGS
work completed before the user paused development is in
[PR #136](https://github.com/movingwoo/wfeature/pull/136); full compatibility
remains incomplete. Further SGS implementation is paused until the user resumes it.
