# SGS corpus acceptance on 2026-09-12

This record covers directed play routes and controlled save/reopen checks in
the twelve-ID local SGS corpus. It identifies the archive only by the stable ID and SHA-256 recorded in
the ignored manifest. Archives, saves, screenshots, and the manifest remain
outside Git.

## ID 09 result

| Acceptance dimension | Result | Evidence |
| --- | --- | --- |
| Main play route | Pass | The restored session accepts directed input through the stage transition and reaches an active playfield with a HUD. |
| In-game save/load | Pass | A new 64-byte payload is written, survives a close/reopen boundary, is loaded before any write in the reopened session, and causes a visible route outcome distinct from an immutable-seed control. |
| Audio | Pending | No physical playback was heard. Message or sample counters would establish service activity only. |
| Exit/restart | Pass | Both sessions close without an error, and the second session starts from the same isolated save store. |

The manifest archive SHA-256 is
`e1b2a614fb1e2d6b850b101fd0d26478e7d7f58cba004cfe3955995a87393792`.
The file still exists at the ignored manifest path and its current bytes match
that digest.

## Directed route

The first session starts from a read-only copy of the existing 64-byte seed
whose SHA-256 is
`ebbd43295ef805d23ea963906787fd5f86e5a561298ec9759fcd50564083df5b`.
It replays the following presses at guest elapsed milliseconds; every press is
released 51-166 ms later, matching the recorded user input.

| At (ms) | Press |
| ---: | --- |
| 965, 1470, 2074, 2408, 3308 | Fire |
| 4604 | Up |
| 5599, 6700 | Fire |
| 7300 | Down |
| 7888, 9187, 10095 | Fire |
| 10567 | `#` |

The game writes a distinct 64-byte payload at 9187 ms and writes the same
payload again at 10615 ms. Its SHA-256 is
`cb2e47c14ce322675c16a317d120aeeb24fa726cf07074e26fed6967990da1c4`.
The session stops at 11000 ms, before the later input in the original recording
that changes the save again, and closes successfully.

The second session uses the same isolated store. Startup loads the distinct
payload at 0 ms, with no earlier write in that session. Five Fire presses use
the opening screens at 965, 1470, 2074, 2408, and 3308 ms. Fire at 4604 ms
selects the existing progress. After the map settles, the following continuation
reaches the main playfield:

| At (ms) | Press |
| ---: | --- |
| 10000, 11000 | Fire |
| 11600 | `5` |
| 12200, 12800, 13400 | Fire |
| 14000 | `#` |

The session reaches the active playfield and HUD by 16120 ms and closes without
an execution error. The final `#` advances beyond the restore checkpoint and
causes another save transition, so it is evidence for continued play after
restore rather than part of the persistence comparison.

## Visible-state control

The map displayed immediately after Continue is not evidence of restoration by
itself. Repeating the second-session route with the immutable seed produces the
same map bytes:
`2e9bcc7992a2b9a9dac574a13d9b2227a6d4963a62a2134446041771a14809d5`.

The next identical input is the first causal distinction. With the newly
written payload, the framebuffer SHA-256 is
`d073ed60c8b414de07c5264be68ae2ba818f41fae585e4679f7d75df8b6cb639`
and shows the stage introduction. With the immutable seed it is
`7b838617372c5b3b474d56bf17314c276f8bde1be5c7d8954b1f287049ec352a`
and shows the active field and HUD. At the end of the same input schedule, the
new-payload session shows the active playfield and HUD with framebuffer digest
`be6377be34ff49205eaca3a1914e4501b80400b333c2e652c079405125c6fc27`;
the seed control instead shows a skill menu with digest
`7f347aae8ab54f9c0dfd59be6f8a93799562150b4103a0d2b52cd1ad483c1bd9`.
The seed control performs no save write. This establishes that the payload
loaded across the session boundary changes user-visible progress under the
same inputs.

The random-number compatibility implementation preserves this causal
checkpoint byte for byte: the new-payload and immutable-seed captures at
11000 ms retain the two digests above. It also preserves the write sequence,
the written payload, and every capture through the final press at 14000 ms.
The continued-play capture at 16120 ms changes to
`7b838617372c5b3b474d56bf17314c276f8bde1be5c7d8954b1f287049ec352a`
and still shows the active playfield and HUD. The immutable-seed arm starts to
differ from its pre-random frames at 11600 ms, as expected from the corrected
random sequence, and ends on the same skill-menu digest recorded above. Both
compatibility runs close without an execution error.

The exact post-random evidence directories are:

- `sgs-progress-09-short-randomcompat-mode2-20260912` for the focused
  new-payload reopen;
- `sgs-progress-09-seed-control-randomcompat-mode2-20260912` for its
  immutable-seed control;
- `sgs-progress-09-mainplay-randomcompat2-mode2-20260912` for the thirteen-press
  continued-play route; and
- `sgs-progress-09-mainplay-seed-control-randomcompat2-mode2-20260912` for the
  matching thirteen-press immutable-seed route.

These are ignored local artifacts under `var/acceptance`. Two directories with
`mainplay-randomcompat-mode2` in their names are excluded: they were diagnostic
runs made with the harness default of twelve reopened presses and therefore
omit the final `#` press. They are not acceptance evidence.

## Reproduction boundary

The run uses the ignored twelve-ID manifest at
`var/acceptance/sgs-audit-2026-09-11-next/archives.json`, the recorded input at
`build/sgs-user-routes.json`, and an ignored diagnostic that copies the seed
into an isolated store before starting the first session. The original archive
and original saves are read-only inputs.

The route was first recorded against commit `2b56bad`. Before the random-number
compatibility change, repeating it with the standalone SGS runtime mode set to
2 produced byte-identical artifacts: all 27 files in the focused save/reopen
run and all 35 files in the continued-play run matched, including summaries,
PNG captures, and isolated save bytes. Both runs completed without start,
advance, close, or reopen errors. These exact comparisons describe that
pre-random implementation only; the compatibility implementation is validated
separately because it intentionally changes the random sequence. The tested
compatibility files have SHA-256 digests
`e3862ad7e07a6991b97fb1e00f0161da30ed26191fbf0acd691736f0341c783e`
and `15b7e36c52e318dd9b2b67d44da9a8e16e6b44b6a94a5bbaaf95c6f19cd53646`.

This result completes the main-play, save/load, and exit/restart cells for ID
09 only. It does not establish audible output, fidelity, every saved field, or
any acceptance dimension for the other eleven IDs.

## ID 01-05 directed routes

These routes resolve archives through the same ignored manifest and verify each
archive against its recorded SHA-256 before opening it. Each session uses a new
empty save directory. The session advances for 19200 ms before the first press,
then advances for 2400 ms after each press. Thus press zero occurs at 19200 ms,
press one at 21600 ms, and press number *n* at `19200 + 2400n` ms. The cited
capture is taken at `21600 + 2400n` ms. These historical diagnostics use press
events without paired release events.

| ID | Press sequence | Active-play capture |
| --- | --- | --- |
| 01 | Fire, Fire, `1`, Fire, `5`, Fire, Fire, Fire | `sgs-route-reconstruct-01-inferred-randomcompat-mode2-20260912/key7.png` at 38400 ms |
| 02 | Fire x10, `#`, Fire, `1`, `5`, Right, Left | `sgs-route-reconstruct-02-randomcompat-mode2-20260912/key15.png` at 57600 ms |
| 03 | Fire, `1`, Fire, `5`, `5`, Right, `5`, Fire x4, Left, `5` | `sgs-route-reconstruct-03-randomcompat-mode2-20260912/key12.png` at 50400 ms |
| 04 | Fire x10, `#`, Fire, `1`, `5`, Right, Left | `sgs-route-reconstruct-04-randomcompat-mode2-20260912/key15.png` at 57600 ms |
| 05 | Fire, `1`, Fire, `5`, Fire x3, `#`, Fire, Right, Left, Up, Down, `2`, `4`, `6`, `8`, `5`, `1`, Fire | `sgs-route-reconstruct-05-generic-randomcompat-mode2-20260912/key6.png` at 36000 ms |

All five current sessions reach a visibly interactive playfield and close
without a start, advance, input, or close error. For ID 05, the first four
presses are sufficient to enter the playfield; later directional and action
inputs visibly change its state.

The ID 01 route reconstructs a previously unrecorded input sequence from its
archived frame series. Its captures match the archived PNG bytes through
press four, including the opening, story, stage, and ready transitions. The
first difference is press five, where the corrected random sequence selects a
different scene outcome. The reconstructed route still reaches the active
playfield by press seven. ID 02-04 use the exact input arrays preserved in the
older diagnostics, now resolved by stable manifest ID instead of filesystem
walk order.

These artifacts remain ignored under `var/acceptance`; no archive or screenshot
is added to Git. This evidence establishes directed main play for IDs 01-05.
It does not yet establish user-visible save/load behavior, exit/restart from a
saved checkpoint, or audible output for those IDs.

## ID 01-05 save and lifecycle boundary

Save-boundary tracing and controlled reopen runs distinguish initialization
from progress:

| ID | Observed save behavior | Controlled visible result |
| --- | --- | --- |
| 01 | The empty store is read at startup, a 64-byte default with digest `bbffe0aa61cc73c4b716e2f1f8be1b4e2d1bdbc7a9d591609b3cee2433d15edb` is written at 0 ms, and a distinct payload with digest `a6719ee8a2ca241a39b24fd67bf62ab4dc2638df0dc4b3819ad44c1ddf8c17d5` is written at 29616 ms after press four. | Reopen loads the distinct payload before any new input. Repeating the complete route against that payload and the startup default produces the same thirteen captures, so the tested route does not expose a causal visible progress distinction. |
| 02 | The complete directed route performs no save load or write. | Close and reopen succeed from an empty store. No save or continue action is visible in the traversed main menu. |
| 03 | Startup writes a 64-byte default with digest `8edccb6154be81c585c0de1615f7b9741e87e362281583322c3491169dfc4bb3`; the directed route reads it repeatedly but never changes it. | A seeded-default arm and an empty-store arm produce the same eighteen captures. The empty arm creates the same default at startup, so this is initialization rather than demonstrated progress. |
| 04 | Startup writes a 64-byte default with digest `c65a363f975f4cfa7facb1ae53b5bf02a4d05312798320530b04ce4cb43b43ff`; the directed route reads it repeatedly but never changes it. | A seeded-default arm and an empty-store arm produce the same twenty-one captures. The empty arm creates the same default at startup, so this is initialization rather than demonstrated progress. |
| 05 | The empty store is read at startup and a 64-byte payload with digest `5c0bfe68eb0efb627b6288421f7671c6768f80cf399c71b124351d936e9e1084` is written at 24064 ms after press two. | Reopen loads the payload. Repeating the complete route against that payload and an empty-store control produces the same twenty-five captures, so the tested route does not expose a causal visible progress distinction. |

Every first session and reopened session in this comparison starts, advances,
accepts input, and closes without an error. None reports a guest-initiated exit;
the lifecycle evidence here is an orderly Host close followed by a new session
over the same isolated store. The bounded absence findings apply to the menus
and directed routes above, not to unvisited end-of-round, settings, or score
flows.

The same directed routes emitted the following activity through a recording
audio sink:

| ID | MIDI messages | Wave samples |
| --- | ---: | ---: |
| 01 | 1230 | 4164 |
| 02 | 921 | 6584 |
| 03 | 0 | 16570 |
| 04 | 0 | 57360 |
| 05 | 597 | 75740 |

These counts identify applicable audio paths and do not establish that a person
heard output. Audible acceptance remains pending for all five IDs.

## ID 06-08 and 10-12 directed acceptance

The current random-compatible runtime replays the exact timed recordings where
they reach a useful endpoint, with focused directed routes for IDs 07 and 11.
Each archive is resolved through the stable manifest and verified before
opening. The following table records the concrete visible endpoint and the
boundary that remains:

| ID | Route and visible endpoint | Current boundary |
| --- | --- | --- |
| 06 | 100 events, including 50 presses, from 677 through 20756 ms. `sgs-directed-06-randomcompat-mode2-20260912/first-key025.png` and `first-key050.png` show interactive management screens over the playfield after resource-changing input. | This establishes the recorded management path. A distinct action-field route has not been identified. |
| 07 | The first six recorded Fire presses reach the main menu; a seventh Fire press at 9000 ms selects the start flow. `sgs-directed-07-start-randomcompat-mode2-20260912/first-final.png` at 14120 ms shows the existing-save path in its interactive management/HUD screen. | The original 75-press recording navigates settings, help, and ranking screens without selecting Start, so it is not the directed route. |
| 08 | 44 events, including 22 presses, from 938 through 17614 ms. `sgs-directed-08-randomcompat-mode2-20260912/first-final.png` at 22614 ms shows an active battle field after directed input. | No save write occurs on this route. |
| 10 | 40 events, including 20 presses, from 619 through 6487 ms. `sgs-directed-10-randomcompat-mode2-20260912/first-key005.png` shows the field and HUD; later captures show field dialogue after continued input. | No save write occurs on this route. |
| 11 | The title has six vertical selections. `Down, Fire`, then five more Fire presses advance the local story into an active room; later Down, Left, and Up presses move the visible player. `sgs-directed-11-local-mainplay-reopen-randomcompat-mode2-20260912/first-key011.png` through `first-key013.png` record that movement. | No save call occurs on this local main-play route. The external handoff on title selection zero remains unsupported but does not block local main play. |
| 12 | 18 events, including nine presses, from 497 through 5974 ms. `sgs-directed-12-randomcompat-mode2-20260912/first-key009.png` and `first-final.png` show the active field after dialogue and movement input. | No save write occurs on this route. |

The ID 11 route waits 19200 ms, then presses `Down, Fire, Fire, Fire, Fire,
Fire, Fire, Right, Right, Down, Left, Up, 5, Fire` at 2400 ms intervals; each
key is released 100 ms after its press. The first and reopened sessions use
the same empty isolated store. Both finish without an execution or close error,
make no save call, and remain guest-active. The first session records 2365 MIDI
messages and no wave samples; the reopened session records 2218 MIDI messages
and no wave samples.

All directed main-play sessions start, advance, accept input, and close without
an error. Their reopened sessions do the same. None reports a guest-initiated
exit; this establishes Host close/reopen lifecycle behavior. For ID 11, all 16
captures shared by the first and reopened sessions are byte-identical.

The earlier ID 11 failure is an alternate title-menu path. Initialization calls
the menu reset routine at `0x058c`, which stores zero in the selection element
at `0x061c`. The key routine at `0x10f8` decrements that element at `0x117e`
for Up and increments it at `0x1231` for Down; observed title input moves it
from zero through five and clamps there. Fire on selection zero first enters
state `[1, 0]`; a second Fire reaches external-URL service `0xc4` at `0x13cd`.
The service argument is an absolute HTTP URL in script resource 65; the full
value stays in ignored local evidence and no network request was made. State
`[1, 5]` takes the local branch at the same gate and returns toward the title.
Selection one instead enters the local story and main-play route described
above. After `0xc4`, the script unconditionally jumps to the invocation
terminator, so the alternate handoff does not expect a script callback.

## ID 06-08 and 10-12 save controls

ID 06 starts from the 64-byte seed
`caf4e3c15d6f27cdf179cd215eec5a9d83420b3d7949c6e03fbef021a96597bd`.
It writes
`d1e86415605a626fa05e9eb878b0a8d0a7e22ab73ea7071069ba2267e044d3d9`
at startup, then writes the seed bytes again at 11168 and 14060 ms. An
empty-store control produces the same write sequence. All 54 first-session and
15 reopened-session captures are identical between the seeded and empty arms.
The route therefore exercises the save boundary but does not establish visible
saved progress.

ID 07 starts from the existing 64-byte seed
`1b1c5cc8c32c9b5122befbde3cee7639ff24a759a20910e2144223d30ef4bc6e`.
The seven-press start route loads and retains that payload. Its empty-store
control writes a different payload with digest
`2d67960ff306191afdd74818324b1afa172eb53b2c917c01818f86b5a89a3f5f`.
Under the same inputs, the arms first differ at press five at 7089 ms. At 14120
ms the seeded arm shows the interactive management/HUD screen with framebuffer
digest
`4c0540acd03f8a7186aa0c84ed2d53dbbf7c1c6ddabc6d6c78ff2b9d970623e6`;
the empty arm shows the new-game story with digest
`df74839356bbb53c66390830469fafeeee06e9ab7e5866cf7f142e55bf89f16a`.
The same distinction repeats after close/reopen. This proves that an existing
save causally changes visible state. It does not prove creation and restoration
of new progress during this run.

IDs 08, 10, and 12 each make one missing-save read in the first session and one
in the reopened session, with no write. ID 11 makes no save-boundary call. These
are bounded results for the recorded paths; end-of-round or unvisited menu
flows remain unknown.

The directed first sessions emit the following audio activity:

| ID | MIDI messages | Wave samples |
| --- | ---: | ---: |
| 06 | 1708 | 0 |
| 07 | 363 | 4262 |
| 08 | 496 | 980 |
| 10 | 208 | 0 |
| 11 | 2365 | 0 |
| 12 | 800 | 0 |

These counters demonstrate service activity only. Audible output remains a
human acceptance item for every ID.

## Current corpus matrix

| ID | Reusable directed route | Visible save result | Lifecycle result | First remaining test or blocker |
| --- | --- | --- | --- | --- |
| 01 | Active playfield | Distinct payload reloads, but default control is visually identical | Host close/reopen passes | Reach an end-of-round or menu flow that exposes a saved field. |
| 02 | Active playfield | No save call on the route | Host close/reopen passes | Check end-of-round and the remaining menu entries for save applicability. |
| 03 | Active fight | Startup default only; no progress change | Host close/reopen passes | Check end-of-round behavior for a changed payload. |
| 04 | Active fight | Startup default only; no progress change | Host close/reopen passes | Check end-of-round behavior for a changed payload. |
| 05 | Active playfield | Payload reloads, but empty control is visually identical | Host close/reopen passes | Exercise score, settings, or round completion and compare a changed visible field. |
| 06 | Interactive management path | Seeded and empty controls are visually identical | Host close/reopen passes | Identify a distinct progress action or an action-field transition; repeating this management route adds no evidence. |
| 07 | Existing-save path reaches interactive management/HUD | Existing seed causally changes visible state; new progress is not proven | Host close/reopen passes | Produce a new changed payload through a directed progress action and compare it with the immutable seed. |
| 08 | Active battle field | No save write on the route | Host close/reopen passes | Check end-of-round or menu flows for save applicability. |
| 09 | Active playfield and HUD | New payload causally restores visible progress | Host close/reopen passes | Audible output remains a human check. |
| 10 | Field and HUD with continued dialogue | No save write on the route | Host close/reopen passes | Check end-of-round or menu flows for save applicability. |
| 11 | Active room after local story and dialogue; movement changes the visible field | No save call on the route | Host close/reopen passes; all 16 shared captures match | Check later progress or menu flows for save applicability; the alternate title-menu URL handoff remains unsupported. |
| 12 | Active field after dialogue and movement | No save write on the route | Host close/reopen passes | Check end-of-round or menu flows for save applicability. |

Audio remains pending for all twelve IDs because no physical playback was
heard during these diagnostics.

## Combined implementation verification

The integrated revision `9c716ee` combines native text input, the original
random sequence, SIS metadata comparisons, literal frame extraction, and exact
object references. General and debug tests, internal race tests, vet, and both
CLI and server builds in debug and release passed. A Chromium run at 390 by
844 pixels exercised the authored native-input archive: Korean submission,
callback-opened dialogs, explicit cancellation, and park/resume without guest
cancellation all passed with no page or server errors. This is a desktop
Chromium viewport check, not physical-phone keyboard acceptance.

A fresh manifest-verified startup check opened all twelve archives without a
persistent save store and advanced each for 1,200 ticks of 16 milliseconds.
All remained running and closed without error. Frame counts by ID were 93,
74, 73, 73, 171, 93, 120, 75, 92, 600, 92, and 21. This confirms the bounded
startup path on the combined implementation; it does not repeat or strengthen
the directed play and save evidence above. The corpus matrix remains open.
