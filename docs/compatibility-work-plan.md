# Managed platform compatibility work

Started 2026-09-12 from `2b56bad`. The user authorized a manager and three
platform workers, with each worker using `gpt-5.6-sol` at `xhigh`. The manager
owns integration, final tests, commits, pushes and pull requests. Merging is
left to the user. Workers do not publish changes or create additional workers.

## Scope

This goal covers the platform compatibility follow-up in the local TODO,
including shared input and validation needed to establish compatibility.
Quick save/load, general PWA expansion, mobile distribution, optional networking
add-ons and performance projects remain separate work. SGS remains paused.
New unrelated features do not silently expand the goal; regressions caused by
this work must be repaired before a change is submitted.

The fixed work list is:

- KTF/BREW loading stall: locate the current archive, reproduce and investigate
  the resource-to-surface path; record an external-data blocker if absent.
- Host-native text entry: use the phone or PC keyboard/IME for composition,
  commit text through the session protocol into supported guest editors, and
  retain existing keypad input. The user steered this away from reproducing
  historical keypad composition on 2026-09-12.
- LGT Java scanner false positives, with actual dispatch and coverage boundaries
  represented accurately.
- KTF/SKT missing API references versus actual menu/save/exit calls; implement
  evidenced contract failures with regression proof.
- SKT/Jlet save/progression and audio validation beyond first-frame probes.
- Slow-animation input misclassification and whether shared settling rounds
  need adjustment, based on measured platform behavior.
- The recorded WebKit frame-decoding regression: reproduce and isolate the
  failing stage, fixing it if current evidence supports a mechanism.
- Landscape 320-by-240 support: verify archive evidence before changing runtime
  screen selection.
- Missing shared service contracts concerning audio time and data ownership.
- Correct affected stale compatibility and validation documentation.

## Ownership and acceptance

Each worker uses an isolated worktree. The game library is shared read-only;
diagnostics and probe saves are isolated. Shared code has one assigned writer
at a time. Initially SKT owns JVM stream-interface changes, LGT owns its platform
dispatch/coverage code, KTF owns its native investigation, and the manager owns
shared text input and integration.

A worker submits a bounded change with cause, changed files, regression proof,
validation and remaining limitations. The manager reviews the actual diff and
checks the affected behavior before accepting it. A passing startup probe does
not prove gameplay, saves or audible playback. Missing data, specifications or
human observations remain explicit unresolved items rather than completed boxes.

Integration gates are `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...`. Protocol changes exercise
Go and browser ends; profile changes verify debug and release. Real-title and
browser checks are selected for the changed behavior. Independent changes are
published as separately reviewable commits/PRs, with dependencies stated and
CI results checked.

The local ignored TODO remains the running queue. This document preserves the
agreed scope and acceptance contract; individual follow-up documents preserve
evidence and decisions. No whole-platform completion is inferred from a finite
test corpus.
