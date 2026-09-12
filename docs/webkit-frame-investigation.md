# WebKit frame decoding investigation

## Recorded failure

The September 11 extended browser route passed its authentication and reconnect
assertions but recorded twelve Blob access-control errors and corresponding
`createImageBitmap` warnings. The preserved evidence is identified in
[authentication-cases.md](authentication-cases.md#existing-empty-certificate-and-automatic-defaults).
The error occurred while reading the Blob argument; that message does not prove
that PNG parsing failed or that the server emitted corrupt bytes.

An upstream [WebKit report](https://bugs.webkit.org/show_bug.cgi?id=258443)
describes similar Blob access-control errors in opaque-origin workers. This page
decodes on its normal document origin, so that report is not a diagnosis of this
project's failure. No browser workaround is justified by matching error text alone.

## Current phase-instrumented checks

On September 12, the manager rebuilt the debug server and instrumented the actual
page's `createImageBitmap` calls. Each observation records the lifecycle phase,
Blob size, successful bitmap dimensions, or failure. WebKit used the installed
`webkit-2336` engine with a 1000 by 850 viewport. The archive was the same
`caf9d76ffd13` corpus identity from the recorded failure.

- The first run decoded 642 frames without a decode or page error. A test-script
  visibility mistake prevented its restart action; it is partial evidence only.
- A corrected fresh-save route decoded 519 frames through initial input,
  reconnection and restart with no recorded errors.
- A held-key route decoded 518 frames across those phases with no recorded errors.
- A route seeded from a copy of the existing release save decoded 479 frames
  with no recorded errors. Its final screenshot shows active gameplay. The seed,
  game archive and original save directory were not modified; all writes went to
  an isolated manager-worktree save root.

Disposable observations, server logs and screenshots are under
`var/diagnostics/webkit-frame-20260912*` in the manager worktree. The seeded route
uses the suffix `-seeded`. These checks exercise the real WebSocket, page decoder
and game, rather than a mocked bitmap or a unit-test replacement.

## Decision and remaining limit

The earlier error did not recur in these bounded current checks. Keep the decoder
unchanged: neither replacing Blob transport nor retrying every decode has a
reproduced mechanism supporting it. This does not prove that the intermittent
failure is fixed. Its original cause and precise trigger remain unresolved.

A future recurrence should retain phase-tagged decode failures, browser version,
Blob size and type, page origin, navigation events and the matching server frame
record before changing transport or decoding behavior. The current evidence
narrows the investigation but is not a whole-browser acceptance claim.
