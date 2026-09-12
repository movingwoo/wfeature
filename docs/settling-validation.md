# Settling and key-response validation

## Reproduced false positive

A four-frame cycle with holds of 10, 9, 10 and 9 ticks reproduces the documented
38-tick animation. The former `StillRuns = 8` shortcut settled on frame one at
tick eight. `Changed(2)` then returned true without any key being pressed. The
new regression test first failed with that exact sequence.

Both still and cycling decisions now observe 64 frame samples: the initial frame
and 63 ticks, the same horizon as two existing 32-sample cycle windows. A cycle
must show the same frame set in both halves. Requiring equality also prevents a
slow crawl from being accepted merely because its most recent half is a subset
of the earlier half. The regression distinguishes all four unpressed animation
phases from a genuinely new fifth frame. Existing still, blink, crawl and
six-frame animation tests pass.

This is an acceptance-test judgment. It does not change runtime speed, input
latency or the game's frame loop. A finite observation window still cannot prove
causality for every possible animation or delayed transition.

## Three-corpus comparison

The September 12 comparison used the same ignored canonical archive directories
and fresh per-test save directories. Each row is an archive subtest, not a whole
platform claim. The original shortcut, full-window candidate and four-round
experiment were separate compiled test runs.

| Platform | Original pass / skip / fail | Full window, 2 rounds | Full window, 4 rounds |
| --- | --- | --- | --- |
| KTF | 31 / 4 / 1 | 21 / 14 / 1 | 21 / 14 / 1 |
| LGT | 31 / 0 / 2 | 31 / 0 / 2 | 31 / 0 / 2 |
| SKT | 14 / 1 / 0 | 9 / 6 / 0 | 9 / 6 / 0 |

Ten KTF and five SKT passes became skips because their screens kept changing
within the longer baseline. They are now unanswered input questions, not runtime
failures. The final KTF skips comprise eleven changing screens and three openings
still moving; the six SKT skips are changing screens. The one KTF failure is the
previously identified differently packaged input without `__adf__`; the two LGT
failures lack `app_info`. Those same three input failures occur in every variant.

The full-window tests completed in approximately 83 seconds for KTF, 28 for LGT
and 13 for SKT. These runs overlapped other local development and are execution
records, not controlled performance benchmarks. Raw `go test -json` records are
under `var/diagnostics/settle-20260912/{baseline,confirmed,four-rounds}.json` in the
manager worktree. All archive outcomes were compared by subtest identity.

## Round-count decision

Keep `SettleRounds = 2`. Doubling it produced no outcome change in any of the
three corpora. KTF already observes spontaneous movement after its final round;
LGT and SKT currently leave before that final watch. No current result changed
because of that difference in this comparison. This records the difference
without claiming it can never matter, and does not add unmeasured rounds merely
to increase a coverage number.
