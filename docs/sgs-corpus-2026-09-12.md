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
