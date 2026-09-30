# SKT retained-session resume (2026-09-30)

## Failure and evidence

The user reported an installed iPhone PWA, switching to another app and
returning after approximately three seconds. The automated browser checks
below exercise the page lifecycle with WebKit; they are not physical iPhone
or installed-PWA suspension tests.

The reported app switch retained the server session. The debug log records a
pause and park at 09:14:28.355 KST, followed by resume at 09:14:33.722: a 5.367
second absence, with no intervening session start. The lifecycle resume invoked
`startApp` again inside the existing JVM.

The reported revision's `startApp` unconditionally constructs a new Canvas,
stores it as the current game and starts another worker. Its `pauseApp` returns
without stopping the previous worker. A direct reproduction changed Canvas
identity and doubled the JVM's reported thread objects from two to four.
The old game still holds its retained Graphics and resources, while the new
game initializes and draws. This explains the return to the opening screen and
provides a mechanism for competing drawing after a new game is selected.

The failure was also reproduced in a different local game. Two other games
retained their Canvas and thread count under the same pause/resume sequence.
Labels refer to the corpus recorded in the [previous audit](skt-qa-2026-09-29.md#cross-archive-call-verification).

| Label | Canvas retained before correction | Thread objects before/after resume |
| --- | --- | --- |
| `archive-017` | Yes | 1 / 1 |
| `archive-024` | Yes | 0 / 0 |
| `archive-042` | No | 2 / 4 |
| `archive-075` | No | 2 / 4 |

## Scope and contract

The [WIPI specification](https://mirusu400.github.io/wipi-wiki/)'s MIDP `MIDlet`
entry defines reactivation through
`startApp`; a standard MIDlet must handle repeated calls. The server's retention
protocol did not lose or replace this session. Removing lifecycle callbacks
from every game would break applications that use them to stop and restart
their own work.

The compatibility registry therefore selects `skt.resume_without_start` for
the two verified class sets:

- `3be2da69a3a6d9f33a33c3c510684a7f29b469309f5f1366a51598e5276cc516`
- `e3a656d9d672fb5db522ab5a3752208cd333c43cf68c09368e4ed2bb56c315df`

After a successful initial start, these revisions return to the active state
without re-running their initialization. The current Canvas, JVM, worker and
game state remain owned by the retained session. A deferred initial start still
retries its real callback. Ordinary MIDlets and WIPI Jlets keep their existing
lifecycle behavior. Matching uses all class bytes, not filenames or profiles.

This correction prevents duplicate initialization. It does not add an execution
snapshot or freeze independently running Java threads and the guest clock;
those retain the existing behavior described in [SKT lifecycle](skvm.md#loading-and-lifecycle).

## Validation

The authored `RestartingMIDlet` fixture creates a new Canvas and worker on
each start. Before the correction, the retention regression failed because
resume replaced its Canvas. The regression checks three pause/resume cycles,
input progress, worker count and callback counts. Separate tests keep standard
MIDP reactivation and deferred initial starts working. An opt-in local test
rejects the correction after changing any class in either recognized archive.

After correction, all four original archives retained their Canvas. The two
affected revisions kept two reported thread objects instead of growing to four;
the other two retained their original counts of one and zero. In the reported
revision's tutorial, three 5.4-second pauses also preserved the current Canvas,
thread count and guest progress fields. Captured room backgrounds remained
intact; changing character-animation pixels are not lost background tiles.
After the third return, input reached the same Canvas: its last-key field
recorded `190` for Host Call, then `148` for confirmation, while the worker
continued updating its frame counter.

`make test`, `make test-debug`, `go test -race ./internal/...` and `go vet ./...`
passed. Debug and release CLI/server builds succeeded. The local fingerprint
test recognized both original class sets and rejected every class mutation.

The browser route uses the real page client and WebSocket, dispatching hidden
and visible lifecycle events around 5.4-second absences. In the final route,
both Chromium and WebKit retained one start across two returns, passed a
pixel comparison of the room background, and delivered confirmation and Call
press/release pairs after returning. Both final routes had no page errors.

An additional reload route in Chromium retained one session start across two
returns and an actual page reload, with three resumes
and no page errors. WebKit also retained one start and three resumes, but the
reload route recorded a Blob access-control error. This is a recurrence of the
[previously unresolved WebKit decoder observation](history/testing.md#webkit-webkit-frame-decoding-investigation),
not evidence of lost JVM state. That run is not a clean browser acceptance pass.

Original PNG captures and replay of the captured WebP messages preserve the
room background. Pixel comparisons across returns locate changes within the
animated character area; no additional graphics compatibility change was made.
These checks do not establish physical-device suspension behavior or recovery
after the server process itself exits.

Local evidence is under ignored `var/diagnostics/skt-resume-20260930` and saves
are isolated under `var/savedata/skt-resume-20260930`.
