# The command line

`cmd/cli` runs a game without a browser. It is the same emulator the server
hosts — the same loaders, ARM core and runtime — driven by a terminal and a
tick count instead of a socket and a page. That makes it the debugging tool: a
run can be given an exact number of ticks, a scripted route back to a scene, a
profiler, a GDB stub or a cheat console, none of which a browser session asks
for.

## Running it

```sh
make run ARGS="runktf var/games/ktf/game.zip -play"          # debug profile
make run-release ARGS="runktf var/games/ktf/game.zip -play"  # release profile
go run ./cmd/cli inspect var/games/skt/game.jar              # without building
./build/debug/wfeature runktf var/games/ktf/game.zip -play   # a built binary
```

The profile is the binary, not a flag — see the build profiles in
[`architecture.md`](architecture.md). A debug build logs at `DEBUG` and collects
the ordered boundary trace; a release build keeps the counted totals.

One setting is read from the environment rather than from a flag, because it
belongs to the handset rather than to a command: `WFEATURE_PHONE_NUMBER` is the
subscriber number `PHONENUMBER` and `MIN` answer with. An invalid value stops
every command with exit code 2 before anything is loaded. It matters for two
titles, which want opposite things from it — see [`network.md`](network.md).

## Commands

```
wfeature inspect <game.jar>                     read the archive, print its summary as JSON
wfeature run <archive> [flags]                 use the shared Host session and checkpoints
wfeature runskt  <game.jar|game.zip> [flags]    run an SKT title
wfeature runlgt  <game.zip> [flags]             run an LGT title
wfeature runktf  <game.zip> [flags]             run a KTF title
wfeature invoke  <game.jar> <method> <descriptor> [arguments...]
wfeature importsaves <external savedata dir> [-save dir] [-games dir] [-dry-run]
wfeature checkgames [-games dir]                find titles that share a save directory
wfeature provision <game.zip> [flags]           write the certificate one title asks for
wfeature contactsheet <framedir> <out.png>      tile a run's frames into one page
wfeature framediff <dirA> <dirB> [-limit N]     name the frames two runs disagree on
wfeature framestats <framedir|frame.png> [-limit N]  count colours and lit pixels; nonzero when every frame is one colour
wfeature zoom <frame.png> <out.png> [flags]     magnify a corner of one frame
wfeature licenses                               print the project and third-party notices
```

`inspect` and `invoke` both answer JSON. `invoke` calls one static method on the
archive's main class and prints what it returned, which checks a class file's
behaviour without a game around it. Running the binary with no arguments, or
with an unknown command, prints the usage and exits 2.

`-screen WxH`, wherever a command takes it, accepts a width and a height
between 32 and 1024.

## What a run exits with

A run is read two ways: by a person, from the JSON summary, and by a batch over
hundreds of archives, from the exit code. `runktf` and `runlgt` once reported a
failed tick in the summary and still exited zero, so a sweep counted a title
that died on its second tick as one that ran. The codes are now:

| code | meaning |
|---|---|
| 0 | the run finished. No tick failed, or the guest ended itself |
| 1 | the run failed, or the tool did |
| 2 | the arguments were wrong |

The two kinds of 1 are told apart by whether a summary was printed. A run that
failed inside the guest prints its summary first, with `tick_error` saying what
stopped it; a run the tool could not finish stops before the summary and writes
the reason to stderr.

Five outcomes deliberately exit **zero**:

- **A guest that exited on its own.** The Jlet or Clet asked for it, and some
  runs exist to reach it. The run prints `the game exited at tick N: …` to
  stderr and carries `exited` and `exit_reason` (the platform call the guest was
  inside and the address it left from) in its summary, because a run that
  stops short of `-ticks` with no note is indistinguishable from a hang. See
  [`history/session.md`](history/session.md#implementation-an-ending-says-where-it-came-from).

  **A guest can end before the first tick, and that is the same ending.** A
  title whose first run installs itself quits inside `startApp` on its second
  run: it reads the flag it wrote and calls the platform's exit. That run prints
  `the game exited before its first tick: …` and carries `exited` and
  `exit_reason` in a summary of no ticks; it was formerly a start failure with
  exit code 1 and no summary. **A sweep that always starts from an empty save
  directory never sees this case**: it takes two runs over one save. See
  [`history/session.md`](history/session.md#implementation-a-start-can-be-refused-by-a-game-that-ended-not-a-game-that-broke).
- **An interrupted run.** Ctrl-C cancels the context rather than killing the
  process, so it reaches the exit code as the reason a tick stopped.
- **A route that did not arrive.** `route_stopped_at` and `route_reason` are in
  the summary and on stderr. A route's budget running out says the scene moved,
  not that the emulator broke.
- **A run that drew nothing.** Whether a frame is blank is a question about
  pixels, answered by [`framestats`](#contactsheet-framediff-framestats-and-zoom).
- **A callback that ended in an uncaught exception.** All three platforms end
  the callback rather than the session, as the language and the handset do.
  `uncaught` and `uncaught_first` in the summary record it on `runktf`, `runlgt`
  and `runskt` alike. **A sweep that reads only the exit code and `tick_error`
  counts a title that fails every paint as one that plays**, so a sweep must
  read them. See [`ktf.md`](ktf.md), [`lgt.md`](lgt.md) and [`skvm.md`](skvm.md).

`checkgames` and `framestats` are the other two commands whose exit code is an
answer rather than a status; each is documented with the answer it gives.

## One setting, three clocks

The browser's speed control offers 0.25 through 4; `backend.ClampSpeed` clamps
any multiplier to `[0.1, 16]`, and zero means the speed the game was written
for. A multiplier means the same on every platform: the guest's clock runs at
that rate, and the waits between its callbacks cost that much less. Both halves
are required, because a game that measures its own elapsed time and steps by it
would, called twice as often against an unchanged clock, step half as far and
not speed up.

What differs is what there is to scale.

| platform | the guest's clock | what a Host waits |
| --- | --- | --- |
| KTF, both packages | a wall clock scaled by `backend.SpeedClock` | the callback interval, divided |
| LGT | virtual: only a tick moves it, so there is no rate to change | what a tick of guest time is allowed to cost |
| SKT | the runtime's own clock, which `System.currentTimeMillis` and `RecordStore.getLastModified` both read | the VM's `Thread.sleep` and timed `Object.wait`, divided |

On SKT a title's frame loop is a thread that sleeps: scaling only the sleep
leaves it stepping less each time it wakes, and scaling only the clock leaves
it stepping further at the same rate. Scaling both makes it run faster.

## run: shared sessions and checkpoints

```sh
wfeature run var/games/ktf/game.zip -ticks 300 -quicksave
wfeature run var/games/ktf/game.zip -quickload -ticks 100 -frame build/restored.png
wfeature run var/games/lgt/game.zip -ticks 300 -quicksave
wfeature run var/games/ktf/game.zip -serve
```

`run` calls the same `internal/session` API as the server. Its checkpoint
support covers supported KTF Java, older module and native package states, LGT
Clets and AOT Java titles, and SKT Java/MIDP/WIPI and SGS titles. Unsupported
runtime state is refused explicitly. The platform commands `runktf`, `runlgt`
and `runskt` keep the profiling, routes and platform diagnostics.

| Flag | Behavior |
| --- | --- |
| `-ticks N` | Run up to N ticks, default 64; accepts 0 through 1,000,000. |
| `-save dir` | Platform save root; defaults to `var/savedata/<profile>/<platform>`. The archive's owner directory is appended. |
| `-quicksave` | Store one checkpoint of execution state after the final tick. It holds no ordinary save. |
| `-quickload` | Restore execution state from the archive's quick save before any tick, without rerunning guest startup. Ordinary saves are left as they are. A missing or invalid slot, or one written in an earlier format, fails startup with status 1 and a reason that names the file. |
| `-serve` | Read one JSON command per line; use `step` and `quicksave` commands instead of `-ticks` and `-quicksave`, which are refused beside it. Can combine with `-quickload`. |
| `-frame path` | Write the final screen to a PNG. |
| `-play` | Pace the game using wall time. Without it KTF uses a manual clock and advances to scheduled deadlines, and LGT, whose guest clock only a tick moves, runs its ticks back to back: the run is the one a paced Host would have had, without the waiting. SKT is paced either way. |
| `-speed N` | Guest speed, 0.1 through 16; default 1. This flag does not imply `-play` for this command. |
| `-screen WxH` | Handset dimensions for a newly started session. Restoration uses saved runtime settings. |

A non-serve run prints JSON with `platform`, `ticks`, `exited`, `restored`,
`quick_saved` and `digest` (a screen digest). A guest that exits during start
ends the command with status 0. A failed checkpoint reports its reason and
returns a nonzero status. In serve mode a refused load returns an error
response and the current session remains available:

```json
{"cmd":"step","ticks":100}
{"cmd":"quicksave"}
{"cmd":"step","ticks":50}
{"cmd":"quickload"}
{"cmd":"screen"}
{"cmd":"quit"}
```

`run -serve` supports stepping, keys, touch, park/resume, screen/pixel
inspection, PNG output and checkpoints. Its driver has no audio sink, route
runner or runtime diagnostic callback. A paused slot stays paused: `step`
reports a stall, and `{"cmd":"park","ms":0}` resumes it through the ordinary
lifecycle. Held input in a checkpoint is restored exactly, so scripts must
issue releases when needed; the browser separately releases restored input
while resetting its physical controls. The serve response's `total_ticks`
counts Host steps since the command started, not a restored guest counter. Key
names are the shared table below (`run` maps `clear` but not `clr` or `back`),
sent as the codes the page sends: `soft1` and `soft2` are `-6` and `-7` on
every platform. On an SKT title (a MIDlet or an SGS script) `soft3`, `ez` and
`hangup` are refused, as `runskt` refuses them: a MIDlet would be handed the
raw code, and an SGS script reads the hang-up code as up.

The slot is tied to the full archive digest. Loading brings the game back and
leaves its ordinary saves as they are: the restored game reads the saves on
disk and writes on top of them, and a write the running game had issued and the
host still held is stored before either a `quicksave` or a `quickload`. See
[Quick load and ordinary saves](architecture.md#quick-load-and-ordinary-saves).
Slots live in the reserved sibling `.wfeature-quicksave/owners/<owner>/`
directory, outside `.wfs` exports. Use the same explicit `-save` root to share
a slot between build profiles; the default roots include the profile name.
Restore format and runtime data are the same in both profiles.

The command holds the same filesystem session claim as the server for its
lifetime. `runktf`, `runskt` (an SGS title included), `runlgt`, `importsaves`
and `provision` also take this claim, on the directory their store writes:
the owner directory under the default tree, or the `-save` directory itself
for `runskt` and `runlgt`. Another cooperating writer is refused while that
owner is live, so a run on a title a browser session has open exits 1 with
`save directory is in use by another session or tool`. Save listing and export
remain available and wait for complete directory transactions. A save location
that cannot hold a file lock is reported on stderr.

## runktf

```
wfeature runktf <game.zip> [-ticks N] [-frame out.png] [-framedir dir] [-save dir]
                           [-play] [-speed N] [-key tick:name] [-hold N] [-route script]
                           [-touch tick:action:x,y] [-park tick[:ms]] [-serve]
                           [-cheat] [-patch table.json] [-gdb host:port] [-screen WxH]
                           [-diag report.json] [-audio out] [-scale N]
                           [-profile report.txt] [-profile-folded stacks.txt]
                           [-profile-from tick] [-no-auth]
```

| Flag | What it does |
|---|---|
| `-ticks N` | how many ticks to run, 64 by default. Without `-play` the run stops early at the first lit frame, which `-framedir` still writes |
| `-frame out.png` | write one PNG of what the screen ended up holding |
| `-framedir dir` | write `tick0042.png` into `dir` every time a new frame is painted |
| `-save dir` | the save tree. Defaults to `var/savedata/<profile>/ktf` |
| `-play` | run on the wall clock at the game's own pace instead of stepping ticks |
| `-speed N` | multiply the guest clock; must be positive; implies `-play` |
| `-key tick:name` | press a key at a tick, e.g. `-key 300:fire`. Repeatable; implies `-play` |
| `-touch tick:action:x,y` | one touch at a tick, e.g. `-touch 300:press:120,160`. The action is `press`, `drag` or `release`; the coordinates are the guest's own screen. Repeatable; implies `-play` |
| `-park tick[:ms]` | park the game at a tick the way a server does when the page goes away, then resume it. `-park 40` runs the Jlet's `pauseApp` and `resumeApp` back to back; `-park 40:5000` leaves it parked for five seconds first, which under `-play` is five seconds of guest clock the title discovers it lost. It is the only way to drive the lifecycle from a terminal — see [`history/ktf.md`](history/ktf.md#the-park-the-guest-is-told-about) |
| `-hold N` | how many ticks a `-key` press — or a route's `key` step — is held before its release, 1 by default |
| `-route script` | replay a scripted way back to a scene ([Routes](#routes)) |
| `-serve` | drive the run a command at a time over stdin and stdout ([below](#driving-a-run-from-outside)) |
| `-cheat` | attach the text cheat console, paced to about real time; implies `-play`. Without `-ticks` the run continues until it is interrupted |
| `-patch table.json` | apply a cheat table's byte patches before the first tick ([below](#-patch-for-the-runs-nobody-is-sitting-in-front-of)). Repeatable; combines with every other flag, including `-serve` and `-route` |
| `-gdb host:port` | serve a GDB stub over the ARM core. The run does not wait for a client; attach with `target remote host:port`. Detaching or dropping the connection removes that client's breakpoints and lets the game run on, and the stub accepts the next client; Ctrl-C stops a running target; a request packet over 64 KiB ends the connection |
| `-diag report.json` | write the runtime-boundary diagnostics ([below](#reading-a-diagnostics-report)) |
| `-audio out` | write what the guest played: `out.mid` for its MIDI events, `out.wav` for its samples |
| `-screen WxH` | the handset the game is told it runs on, 240x320 by default |
| `-scale N` | run the hqx filter over captured frames: 2, 3 or 4 |
| `-profile report.txt` | sample the ARM core and rank the result by symbol |
| `-profile-folded stacks.txt` | the same profile as flamegraph-folded lines |
| `-profile-from tick` | discard samples before that tick, so a profile covers the scene rather than the loading before it |
| `-no-auth` | turn off the offline authentication adapters ([below](#offline-authentication-compatibility)); `-auth` is still accepted and changes nothing |

`-speed`, `-key`, `-touch` and `-cheat` each turn `-play` on, because none of
them means anything to a run that steps ticks by hand. `-route` and `-serve` do
not: they stay on the manual clock, which makes a replay deterministic and as
fast as the guest can be driven. Add `-play` to watch, or for a title that
busy-waits on the clock.

**Both generations of KTF package are accepted.** `-route`, `-serve`, `-cheat`
and `-patch` work on the earlier package. `-diag`, `-gdb`, `-profile`,
`-profile-folded` and `-profile-from` are refused for it with `<flag> is not
available for the earlier KTF package` and exit code 2, because it has no
descriptor to inspect and no AOT methods to profile or symbolize. `-touch`,
`-park` and a `-speed` other than 1 are refused the same way: its loop has no
pointer and no lifecycle to park, and its session takes no speed (`-play` is
the clock it offers). A frame that fails exits 1 after the summary, as a failed
tick does on the other package. See
[`history/ktf.md`](history/ktf.md#an-earlier-ktf-package-a-module-file-with-no-descriptor-and-no-jar).

**Under `-play` a tick is the server's entry, not a round.** The descriptor
package enters its session the way the server does — rounds until the guest
parks, or 32 ms of them — and then waits out what the guest asked for, so
`-ticks`, `-key` and a route's `wait` count those entries. Without `-play` a
tick is one round followed by a jump of the manual clock. A round is the wrong
unit on the wall clock: a title whose network thread blocks in a read yields
every round and is due again at once, so a round costs a few microseconds and a
route's `wait 1500` ended before the title's next frame came due.

**`-screen` changes what the game is told, not how large the picture is
drawn** (that is `-scale`). It must be given at the start and cannot change
during a run: the screen framebuffer is built once, on the game's first request
for it. Two local titles write into the framebuffer with the stride they were
told, so a size is answered to every surface at once or to none —
[`history/session.md`](history/session.md#implementation-the-screen-is-part-of-starting-a-game)
has the contract and what the local library does at 240x400 and 320x480, the
larger sizes the browser offers. `runlgt` takes the same flag so those sizes
can be measured from a command line.

**The size is a flag rather than read from the archive, by measurement.** A KTF
archive declares `DisplaySize` in its `__adf__`. Thirteen local titles declare
`176*220` and twelve of them draw across the whole 240x320 screen, so adopting
the declaration would shrink twelve working titles to fix one band that is a
game's own artwork — see
[`history/ktf.md`](history/ktf.md#a-band-beside-a-title-screen-was-the-titles-own).
Every local archive was packaged for the 240x320 handset this carrier
standardised on; the flag remains for an archive packaged for another handset.

**Raise `-hold` before concluding a title has stopped.** A title that samples
the keypad once a frame never sees a press and release delivered in the same
tick: two local titles sit on their title screen for a whole run that way, and
one LGT title's character select answers `right` but not `OK` until the press
is held. Twenty ticks is a press a game cannot miss.

**A title that busy-waits on the clock is diagnosed with `-diag`.** The tail of
its trace is one boundary repeated — `wipic 0x1c`, the current-time call —
rather than the mix of drawing and allocation a loading screen makes. A stepped
run cannot move the guest clock from inside a Host service call, so a title
that waits inside `keyNotify` by polling `MC_knlCurrentTime` once spent its
whole service allowance and died with `exceeded its step allowance`, which
reads like an emulator hang. While a service call holds a
clock the Host owns, the clock now advances with the guest's own execution: a
half-second delay costs about fifty million steps and ends
([`history/ktf.md`](history/ktf.md#a-batch-hosts-clock-had-no-rate-and-a-guest-that-waited-paid-for-it)).
A call that never returns still fails on the allowance, in the same seconds.

**`-play` and `-play -speed 8` reach different places, so a batch must not
measure only the second.** A wait spent polling the clock costs steps in
proportion to its length, so eight times the clock is an eighth of the steps: a
title whose opening waits five seconds inside one timer callback fit under the
service allowance at 8x and not at 1x, the speed a person plays at. The wait
rather than the step count now bounds such a call
([`history/ktf.md`](history/ktf.md#the-fifteenth-round-two-host-ceilings-a-title-reached-by-playing-normally)),
but a speed multiplier still changes what a run can reach.

### Key names

`-key`, `-serve`'s `key` command and a route script share one table, so `fire`
cannot mean two things:

```
up  down  left  right  fire (= ok)  soft1  soft2  soft3 (= ez)  clear  call  hangup
```

plus any single character from `0`–`9`, `*` and `#`. Names are case-insensitive.
`runskt` translates the same names to MIDP codes and has its own subset — see
[runskt](#runskt).

- `soft1` and `soft2` are the two keys at the top corners of a handset's keypad.
  Titles name them that way — a help screen says 좌측상단키 or 메뉴키 for the
  first and 우측상단키 for the second — and hang a menu, a minimap, a world map
  or a pause on them. The browser keypad's `메뉴` button and `M` on the
  keyboard send `soft1`; its `우상단` button and `,` send `soft2`.
- `soft3` is `MH_KEY_SOFT3`, the third soft key these handsets carry beside the
  two under the screen. The browser cannot send it, so it is this command's to
  send — `key soft3` or `key ez`. One LGT title's party screen asks for it by
  its handset label ("press the EZ key for the submenu"), which is why `ez` is
  an alias.
- `call` is the handset's send key. A game that answers it usually answers with
  a quick save, which is why the browser keypad carries a `통화` button.

The page sends `-6` and `-7` for the two soft keys, and neither needs
translating: `MH_KEY_SOFT1` and `MH_KEY_SOFT2` are the same `-6` and `-7` as the
MIDP soft keys, so the WIPI platforms and the MIDP one take the same codes. The
positive `6` and `7` an earlier page sent still translate, because a shell
served from a phone's cache can be an older build. `web/keypad.test.mjs` holds
the page's end of this and `internal/session`'s key translation test holds the
server's.

### Routes

A route is a scripted way back to a scene, written in terms of what the screen
is doing rather than absolute tick numbers, which break when a fix changes the
branch the game takes. `runktf` (both packages), `runlgt` and `runskt` take the
same format and the same runner in `internal/route`.

```
# comments run to end of line
wait 400               # advance 400 ticks
wait-idle 40 [limit]   # until the screen holds still for 40 ticks
wait-change 30 [limit] # until the screen differs from where the step began
key fire               # press and release; `key down 3` repeats
key #                  # the hash key, not a comment
press left             # for games that act on a held key
release left
shot title             # capture a frame as <framedir>/title.png
mark battle            # capture, and restart the profile here
```

- `key` holds its press for `-hold` ticks, and those ticks are the route's, so a
  script of twenty-tick keys pays twenty ticks each. `press`/`release` remain
  for a hold that spans other steps.
- Both searching waits are bounded; the limit defaults to twenty times the run
  they look for.
- A script is parsed before the archive is read, so a typo exits 2 at once.
- With `-route` there is no default tick count: the run lasts until the script
  arrives, and an explicit `-ticks` is the budget that stops one that never
  does, whatever count it names (`-ticks 64` included).
- `runktf` replays a route through the route runner alone, so `-key`,
  `-touch`, `-park`, `-cheat` and `-profile-from` are refused beside `-route`
  with `<flag> cannot be combined with -route, which replays only the steps in
  its script` and exit code 2. The script's own `key` and `mark` steps are what
  a route has instead. `runlgt` and `runskt` drive their routes through their
  tick loop and take those flags.

[`history/ktf.md`](history/ktf.md#implementation-repro-routes) covers what a
route that does not arrive reports and why `mark` is where a scene's profile
starts.

**Keep the routes that took work.** Getting past a title's notices,
authentication offer, slot and character screens and a ten-minute opening can
be two hundred lines of route, and every later question about that title starts
from it. They go in `var/routes/`, beside the archives they are written
against and ignored by Git for the same reason.

**A reported session is a route.** A page log under `var/logs` carries every
key press and release with a timestamp, so dividing the elapsed milliseconds by
the guest's tick length turns it into a script that replays the player's input.
That is how a fault reported from a phone is reproduced: one title's crash
needed 1,891 ticks of someone else's play to reach, and no hand-written route
found it.

### Driving a run from outside

A route is written before the run and replays the same way every time, which
makes it a repro; it cannot look at the screen and choose the next move.
`-serve` is that interactive loop, for finding a way through a title before a
route for it exists. **One JSON command per line on stdin, one JSON answer per
line on stdout.** It is on `runktf` (both packages), `runskt`, `runlgt` and
[`run`](#run-shared-sessions-and-checkpoints).

```
$ printf '%s\n' \
    '{"id":1,"cmd":"step","ticks":400,"screen":true}' \
    '{"id":2,"cmd":"key","key":"fire","hold":20}' \
    '{"id":3,"cmd":"screen"}' \
    '{"id":4,"cmd":"quit"}' | wfeature runktf game.zip -serve
{"id":1,"ok":true,"cmd":"step","ticks":400,"total_ticks":400,"progressed":true,
 "digest":"801bbe1714a3dcc1","screen":{"width":240,"height":320,
 "rgba_sha256":"782768ca…","non_black_pixels":73746,"visible_pixels":76800,
 "flushes":399,"digest":"801bbe1714a3dcc1"}}
```

**A bad command is answered, not fatal.** An unknown command, a key name not in
the table, a script that does not parse or a path that cannot be written each
come back as `{"ok":false,"error":"…"}` and the run continues, because an
exploratory session is minutes of guest execution deep when a typo happens. The
one input that ends the session is a line longer than 1 MiB, since the stream
cannot be resynchronized in the middle of one.

Every request may carry an `id` of any JSON shape; it comes back on the answer
unchanged, so a pipelining caller can pair them.

| Command | Fields | What it does |
|---|---|---|
| `step` | `ticks` (default 1, max 1000000), `screen` | advance the run. Stops early when the guest ends, when a tick fails, or when a tick that did nothing is followed by nothing due |
| `key` | `key`, `hold` (0 to 1000000), `repeat` (1 to 1000), `action` | one key from the shared table. `action` is `tap` (default), `press` or `release` |
| `touch` | `action` (`press`/`drag`/`release`), `x`, `y` | one pointer event in the guest's own coordinates |
| `park` | `ms` (0 to 600000) | park the game the way a Host does when the page goes away, hold it for `ms`, and resume |
| `screen` | — | the screen report below |
| `pixel` | `x`, `y` | one pixel in the guest's own coordinates, with `rgba` as `#rrggbbaa` |
| `diag` | `path` | write the diagnostics report `-diag` writes |
| `shot` | `path` | write the frame `-frame` writes, through the same encoder |
| `route` | `path` | replay a route script from where the run is now |
| `quicksave` / `quickload` | — | store the archive's checkpoint, or restore it (`run -serve` only) |
| `quit` | — | answer, then end the session |

Every answer carries `ok`, `total_ticks` (what the serve session has spent
altogether) and, where a command spent any, `ticks`. `step`, `key`, `touch`,
`park` and `route` also carry `digest`, the cheap frame identity a route waits
on, which answers "did anything change". `progressed` and `stalled` say whether
the last tick ran guest work and whether anything is left that would. A guest
that ends is reported as `"ended":true` with an `end_reason`; the session stays
open, because a screen, a shot and a diagnostics report are what is wanted
about a run that just ended.

`key` with `tap` holds its press for the ticks it was given, and those ticks
are the caller's; without `hold` the run's own `-hold` applies. `press` and
`release` are the two halves on their own, for a hold that spans other
commands.

A capability a platform lacks is refused by name rather than ignored: the
earlier KTF package answers `this platform writes no diagnostics, …` and
`this platform has no park to run`, `runlgt` answers `this platform has no
pointer`, and every driver except `run` answers `this driver does not support
execution checkpoints`.

`-cheat`, `-route`, `-key`, `-touch`, `-park`, `-framedir` and `-ticks` are
refused alongside `-serve`: each instructs the loop `-serve` replaces, and the
cheat console and a serve session both want stdin and stdout. `runktf` refuses
`-profile-from` there too, because only its tick loop restarts the profile. The
summary a run normally prints at the end is not printed either, because stdout
is the protocol. `-patch` combines with `-serve`; it reads a file, not a
terminal.

#### The screen report

```json
{"width":240,"height":320,
 "rgba_sha256":"782768ca6e428e1b478b4210e7454e4399f602756caa9401d2cf5bb08d484dae",
 "non_black_pixels":73746,"visible_pixels":76800,"flushes":399,
 "digest":"801bbe1714a3dcc1"}
```

**`rgba_sha256` is the identity a regression assertion is written against.** A
PNG is not one: the encoder picks filters and a compression level, so two runs
that drew the same picture can write different files. This hashes the pixels —
the geometry first as two little-endian 64-bit words, then the normalized RGBA
bytes row by row. A fully transparent pixel contributes four zero bytes
whatever colour was under it. Two runs of the same commands against the same
archive answer the same hash.

`non_black_pixels` is the first-frame signal the probes read, which separates a
title that reached its title screen from one that painted nothing.
`visible_pixels` is how much of the screen is not transparent.

### Reading a diagnostics report

`-diag` writes JSON. `counts` says how many times the game crossed each runtime
boundary; `trace` is the most recent crossings in order and is a debug-profile
field — a release build counts but does not keep the order.

**A start that fails writes one too.** A title that dies inside its own
`startApp`, or in the initialization before it, leaves no session behind. A
failure past the client's entry still carries its trace, and the report's
`summary` is `start_error` alone. The last twenty lines usually name the
resource the title opened, the class it loaded and the method it resolved just
before it stopped.

A working game repeats `flush lcd`, `Card.repaint` and `Graphics.*` once per
tick. `stub table`, `not implemented`, `throw`, `raise` and `error` name what to
implement next. [`history/ktf.md`](history/ktf.md#implementation-runtime-diagnostics)
reads a real report line by line.

Three counters carry their argument, because a start-up gate is usually one of
them: `stream <resource> found=<bool>` for a class-path resource,
`exists <path> found=<bool>` for a filesystem test, and `sysprop <name>` for a
handset property.

A debug run also carries `arena blocks recorded on release` and
`arena blocks checked on reuse` — how much of the guest heap the
use-after-free detector covered — and `arena use after free` if a title wrote
into memory it had given back. The two coverage counts distinguish a clean
report from a build that was not watching. A release build carries none of the
three.

### Byte patches and cheat tables

`-cheat` attaches a console whose `help` lists its commands. Four of them go
through a gate rather than observe it, on every platform `-cheat` supports:

```
patch <name> <addr> <expect> <replace>   replace guest bytes
unpatch <name>|all                       put back what a patch replaced
unpatch -forget <name>                   drop the record without writing
patches                                  what is applied now
```

The rest of the console observes: a scan says where a value is, a watch says
what writes it, a trace says what the guest asked the platform for. None says
what a title stopped at a check would do if the check passed; only going
through the gate shows that.

**A patch declares the bytes it replaces, and is refused when they are not
there.** An address is valid only under the layout it was found in, and layouts
move between builds and between titles. The refusal reports what it found,
which is often the fastest way to learn the bytes:

```
> patch gate 0x00150000 deadbeef 00000000
cheat: patch "gate" span 1 at 0x00150000 expects deadbeef but memory holds 90011500
```

**An entry applies as a unit.** Skipping a check is usually a branch and the
constant it compares against, so every span of an entry is verified before any
is written, and a write that fails partway puts back what it had replaced.
Multi-span entries come from a table file rather than the one-span `patch`
command.

Guest code is writable, so a patch reaches it. A value search does not walk an
arena of platform veneers, which decodes as thousands of plausible candidates
at any width; `regions` marks those `(code, not scanned)`. The client image is
one span of instructions and data with no recorded boundary between them, so it
is swept whole.

`save <file>` writes the frozen values, the watches and the patches as JSON, and
`load <file>` applies them — patches first, because a patch usually makes the
rest reachable, and a refused patch stops the load before any value is written.
`load` reads at most 4 MiB. **Only the console that `-cheat` attaches to a
terminal reaches files.** The browser's cheat panel drives the same console
over its session socket, where a path would be one on the server chosen by
whoever is connected, so there `save` and `load` answer that the console has
no file access; the panel exchanges table text instead.

```json
{
  "image": "1b114584dce8216dd2fae93722feb49f88eb7b8989920caf91903cdef78c8929",
  "game": "the title's main class",
  "entries": [ … ],
  "patches": [
    {"name": "gate", "note": "why", "patches": [
      {"address": "0x00150000", "expect": "90011500", "replace": "00000000"}
    ]}
  ]
}
```

**The key is a hash, not a name.** The same title arrives as several archives —
repacked, renamed, one container swapped for another around the same executable
— and a patch is true of the image. `image` is the SHA-256 of the loaded
executable image; `file` is the SHA-256 of the archive and is the fallback for
a platform with no single image to hash (SKT keys tables by the archive file).
Loading checks the image, then the file, and says so when neither matches:

```
> load table.json
cheat: table patch 1: patch "gate" span 1 at 0x00150000 expects 90011500 but memory holds 311c1635
(this table was made against a different image; its addresses may mean nothing here)
```

`game` is a label beside the key. A table written before the hashes existed
carries only that and still loads; it cannot say what it was made for.
Addresses in `entries` are plain numbers; a patch address is hex because it is
read off a disassembly, and a plain number is accepted there too.

### `-patch`, for the runs nobody is sitting in front of

The console reads stdin, so it is refused beside `-serve`, and a `-route` replay
or a plain tick budget has no console. `-patch <table.json>` applies a table's
byte patches at the start of a run instead, so scripted runs — a route past a
check, a `-serve` session, a sweep over the library — can open a gate. It is on
`runktf` (both packages), `runskt` and `runlgt`, reads a file, and combines with
`-serve`, `-route`, `-cheat` and a bare `-ticks`.

```
$ wfeature runktf game.zip -patch gate.json -route past-the-notice.route
patch: applied 1 entry from gate.json
{"aid":"…","route_completed":true,"ticks":41, … }
```

- **Only the table's `patches` are applied.** Frozen values and watches need a
  tick loop maintaining them, which is the console's job; the flag says on
  stderr that it left them. Notices go to stderr because stdout is the one JSON
  summary a sweep parses.
- **It is repeatable.** A table is a unit of provenance — its own note and key
  hash — so `-patch a.json -patch b.json` applies them in order rather than
  requiring a hand merge. The rules that refuse a name applied twice and a span
  overlapping one already applied stop two disagreeing tables from layering.
- **With `-cheat`, the console continues from the flag.** Entries the flag
  applied are listed by `patches` and reverted by `unpatch`. The console's
  `load` replaces what the session holds, so loading a table there reverts what
  `-patch` applied.
- **A patch that does not apply stops the run** with exit code 1:

  ```
  $ wfeature runktf game.zip -patch gate.json -ticks 400
  gate.json: table patch 1: patch "gate" span 1 at 0x00120000 expects deadbeef but memory holds 504416f0
  (the run is refused rather than continued: a run whose patch did not apply observes exactly what the patch was written to change)
  $ echo $?
  1
  ```

  Warning and continuing would produce a run that shows exactly what the patch
  was written to change, and a sweep over hundreds of archives would file its
  summary beside the others with nobody reading the warning.
- **A table that names a different image stops the run too** (`… was made
  against a different image than this run is holding`). The console only warns,
  because a person is about to see the result; here nobody is. A table applied
  to the wrong image can match its declared bytes by coincidence, which is the
  silent corruption the declared bytes exist to prevent. Removing the `image`
  and `file` keys from a copy of the table applies it anyway, as a deliberate
  act.
- **A table with no key at all** — a hand-written one, or one saved on a
  platform whose title is a set of classes rather than one image — is not a
  mismatch. It applies with the note `patch: nothing in <file> says what it was
  made against, so its addresses are taken on trust`.
- A file with no `patches`, or with a malformed span, is refused before the
  archive is opened, for the reason `-route` parses its script first.
- **Patches land after the title's start-up call and before the first tick**,
  the earliest a Host can reach guest memory (the loader must have run for
  there to be memory to verify). A check a title makes inside its own start-up
  is not reachable this way.

## runskt

```
wfeature runskt <game.jar|game.zip> [-ticks N] [-frame out.png] [-framedir dir]
                                    [-key tick:name] [-hold N] [-route script]
                                    [-save dir] [-diag report.json] [-audio out]
                                    [-screen WxH] [-cheat] [-patch table.json]
                                    [-serve] [-trace] [-no-auth]
```

| Flag | What it does |
|---|---|
| `-ticks N` | how many ticks to run, 64 by default |
| `-frame out.png` | write one PNG of what the screen ended up holding |
| `-framedir dir` | write `tick0042.png` into `dir` every time the screen changed |
| `-key tick:name` | press a key at a tick, e.g. `-key 300:fire`. Repeatable |
| `-hold N` | how many ticks a `-key` press is held before its release, 1 by default |
| `-route script` | replay a route script ([Routes](#routes)). It is the only way to script two keys held at once, which a keyboard produces and `-key` cannot express |
| `-save dir` | the save tree. Defaults to `var/savedata/<profile>/skt/<owner>` |
| `-diag report.json` | write what the run used: classes loaded, classes missing, and a call count per registered native |
| `-audio out` | record what the run played as `out.mid` and `out.wav` |
| `-screen WxH` | the handset screen; without it the run takes the handset the archive was packaged for (below) |
| `-cheat` | attach the text cheat console. Without `-ticks` the run continues until it is interrupted |
| `-patch table.json` | apply a cheat table's byte patches before the first Host pass. Repeatable |
| `-serve` | drive the run a command at a time over stdin and stdout ([above](#driving-a-run-from-outside)) |
| `-trace` | one log line per bytecode instruction. Off unless asked for, in both build profiles |
| `-no-auth` | turn off the offline authentication adapters ([below](#offline-authentication-compatibility)) |

The summary carries `ticks` and `lit` — the count of non-black pixels — beside
the MIDlet's state, because "active with a Canvas shown" and "drawing" are
different answers and only the second means the title is running.

**The run ends by destroying the MIDlet**, after the summary, the way closing a
server session does, so a title that saves its progress in `destroyApp` keeps
it when the run ends on its tick count, its route, `-serve`'s `quit` or Ctrl-C.

**A tick is a real frame.** The run is paced at one tick every 16 ms, the
interval the server session drives the same runtime with, so 1,800 ticks take
about half a minute of real time. There is no `-play` or `-speed`: the runtime
takes a speed setting (see [One setting, three clocks](#one-setting-three-clocks)),
and only the flag is missing.

**Key names** are the shared table translated to the MIDP key codes a MIDlet
compares against: the direction pad, `fire` (`ok`), `soft1`, `soft2`, `call`,
`clear` (also `clr` and `back`), the digits, `*` and `#`. `soft3`, `ez` and
`hangup` have no MIDP code and are refused. `clear` matters here: titles of
this era draw `BACK:CLR` on every screen that can be left, and leaving a screen
often commits what was changed on it.

**`-trace` is opt-in in both profiles** because the line sits on the hottest
path: a second of play writes on the order of a million lines, a debug build
ran one title eighteen times slower with it on, and a release build built each
line and discarded it. [`jvm.md`](jvm.md#what-a-guest-instruction-cost-and-the-two-things-that-were-not-emulation)
records the measurement. `runlgt -trace N` makes the same bargain with a count.

**`-diag` answers a different question from KTF's report.** There is no ordered
trace: an SKT run is asked which of the runtime's Java surface the title
reached, and a call count of zero beside a registered native is the answer to
read. See [`history/skt.md`](history/skt.md#implementation-the-diagnostic-report-and-the-three-questions-it-answered).

**The handset comes from the archive.** An SKT descriptor declares no screen
size — no key of any local `.msd` or manifest carries one — but an archive names
its handset in its resource and directory names, and that is read when
`-screen` is absent. Two local archives branch on the screen and ship only the
120- and 176-wide artwork sets; they need no flag. `-screen` remains for an
archive the rule does not recognise and wins whenever given. See
[`history/skt.md`](history/skt.md#implementation-the-handset-a-title-was-packaged-for-is-in-its-resource-names)
for the rule and why it is narrower than KTF's.

**`-cheat`** attaches the same console the two WIPI paths take, against the
synthetic address space this platform lays over its object graph
([`history/skt.md`](history/skt.md#implementation-a-heap-with-addresses-in-it)).
Commands run between Host passes, the only time reading and freezing the graph
is safe. Two commands differ: `regions` names classes rather than a module and a
heap, so a hit says which class it is in, and `watch` answers "this platform
cannot watch writes" because nothing instruments a `putfield`.

**`-audio`** writes the recording and carries `audio_midi_messages` and
`audio_wave_samples` in the summary beside the file it wrote. Both were zero for
every archive until the timeline and the clock sounds start on were made the
same one; afterwards fifty-six local archives recorded MIDI and sixteen recorded
sampled sound, in four hundred ticks each. [`audio.md`](audio.md) has the
defect and what a recording is worth reading for.

## runlgt

```
wfeature runlgt <game.zip> [-ticks N] [-frame out.png] [-framedir dir] [-save dir]
                           [-key tick:name] [-hold N] [-steps N] [-cheat] [-screen WxH]
                           [-patch table.json] [-serve]
                           [-audio out] [-trace N] [-trace-live filter]
                           [-route script]
                           [-profile report.txt] [-profile-folded stacks.txt]
                           [-profile-from tick] [-no-auth]
```

| Flag | What it does |
|---|---|
| `-ticks N` | how many ticks to run, 64 by default. Unlike `runktf`, the run does not stop at the first lit frame — it runs the count, or until the game exits |
| `-frame out.png` | write one PNG of what the screen ended up holding |
| `-framedir dir` | write `tick0042.png` into `dir` every time a new frame is painted |
| `-save dir` | the save tree. Defaults to `var/savedata/<profile>/lgt/<PID>` |
| `-key tick:name` | press a key at a tick, e.g. `-key 300:fire`. Repeatable |
| `-hold N` | how many ticks a `-key` press — or a route's `key` step — is held before its release, 1 by default |
| `-steps N` | the instruction budget for one call into the game; default 3,000,000,000 |
| `-screen WxH` | the handset the game is told it runs on, 240x320 by default |
| `-route script` | replay a scripted way back to a scene ([Routes](#routes)) |
| `-cheat` | attach the text cheat console, paced at 50 ms a tick (about real time). Without `-ticks` the run continues until it is interrupted |
| `-patch table.json` | apply a cheat table's byte patches before the first tick. Repeatable |
| `-serve` | drive the run a command at a time over stdin and stdout ([above](#driving-a-run-from-outside)). This platform has no pointer, so `touch` is refused |
| `-audio out` | write what the guest played: `out.mid` for its MIDI events, `out.wav` for its samples |
| `-trace N` | keep the last N platform calls and dump them at the end, or on a failure |
| `-trace-live filter` | stream platform calls to stderr as they happen, keeping the lines that contain `filter`. `""` keeps every call |
| `-profile report.txt` | sample the guest and rank the hot address ranges ([below](#guest-profiling)) |
| `-profile-folded stacks.txt` | the same profile as flamegraph-folded lines |
| `-profile-from tick` | discard samples before that tick |
| `-no-auth` | turn off the offline authentication adapters ([below](#offline-authentication-compatibility)) |

`-key <tick>:<name>` releases the key `-hold` ticks later, and the release is
scheduled rather than queued behind the press, because a game that samples the
keypad once a frame would otherwise never see both. Key names are the
[shared table](#key-names).

**The run ends by closing the session**, after the summary, the way the server
closes a game: a file the title wrote and left open is stored, and its destroy
callback runs. A run that ends on its tick count, its route, `-serve`'s `quit`
or Ctrl-C therefore leaves the save the title would have left in the browser.

**Prefer `-route` to a long `-key` list.** These titles take thousands of ticks
to reach anything, and a script in absolute tick numbers lands somewhere
different on every run. A route's `key` step is held for `-hold` ticks here too,
which matters more on this platform than any other: one title's character
select ignores a one-tick press entirely.

**Raise `-steps` when a world load is being measured**: a title's whole entry
into the world happens inside one timer callback, so it is one call's budget
rather than a run's.

The JSON summary reports:

- `busy_ms`, what the run cost the host with the `-cheat` pacing left out, and
  `slowest_tick` with `slowest_tick_ms`. A load happens inside a single tick, so
  an average hides it and the slowest tick is the load; that pair checks an
  interpreter throughput change against a real title rather than a benchmark.
- `guest_ms`, the guest's own clock. A tick does not stand for a fixed span — it
  covers the wait until the guest's next scheduled work ([`lgt.md`](lgt.md)) —
  so the tick count says nothing about how much guest time a run covered.
  `guest_ms` divided by `flushes` is the milliseconds per frame the title is
  given, the number a pacing change is judged by.
- `steps` with `ns_per_step`, the guest instructions retired and what each cost
  the host. `busy_ms` moves when a change alters how much guest work a tick
  holds, which a route-reached scene does from run to run; `ns_per_step` holds
  that still and is what a throughput change must move. A pacing change moves
  neither — it moves guest milliseconds per frame — which is how one title lost
  a third of its frame rate without any profile noticing.

`runlgt` has no `-play`, `-speed`, `-diag`, `-scale`, `-gdb`, `-touch` or
`-park`. The guest clock, the frame loop and the cheat engine are shared and the
session takes a speed setting, so most of them would work; none has been needed
yet.

### Reading the platform-call trace

`-trace N` is a ring: it says **what the game was told just before it went
wrong**. That suits a fault, since a module dies at an address in its own code
hundreds of instructions past the platform call that caused it.

`-trace-live` is a stream, for **what the title ever did with something**. By
the time a screen looks wrong the interesting call is long overwritten in the
ring. A title asks for the framebuffer width tens of thousands of times a
second, so filter to a slot family:

```sh
wfeature runlgt <game.zip> -ticks 600 -save /tmp/probe -trace-live fs \
    -key 40:fire -key 330:down -key 390:fire 2>calls.txt
```

Both forms **read the arguments that are names**:

```
wipic 0x196 fsRemove("Save0.dat" | 0x400fff4c, 0x1, ...) = 0x0 (0) from 0x6df3
wipic 0x1a0 fsIsExist("Save0.dat" | 0x400fff10, 0x1, ...) = 0xfffffff4 (-12) from 0x6ce7
```

The raw registers stay after the `|`, because an address is what a fault
reports. Only the slots whose arguments are names are read from guest memory;
reading every call's arguments would change what the trace measures.

### Guest profiling

`-profile`, `-profile-folded` and `-profile-from` are the same three flags
`runktf` takes, with the same sampler. A Clet has no method names, so the report
ranks the **address range of the loop that is running**, which the LGT
disassemble probe then reads:

```sh
wfeature runlgt <game.zip> -ticks 400 -profile report.txt
# 92.33%  1204356  0x3a550-0x3a698

WFEATURE_DISASSEMBLE_ARCHIVE=<abs path> WFEATURE_DISASSEMBLE_RANGES=0x3a550-0x3a698 \
    go test ./internal/platform/lgt -run TestLocalDisassembleProbe -v
```

Across the local titles, every one spends most of its instructions in about a
kilobyte of code — see
[`history/lgt.md`](history/lgt.md#implementation-guest-profiling-and-what-it-says-about-where-the-time-goes).

## contactsheet, framediff, framestats and zoom

These review a `-framedir` run, which is a few thousand frames.

```sh
wfeature contactsheet <framedir> <out.png> [-every N] [-columns N] [-shrink N] [-from tick] [-to tick]
wfeature framediff <dirA> <dirB> [-limit N]
wfeature framestats <framedir|frame.png> [-limit N]
wfeature zoom <frame.png> <out.png> [-x N] [-y N] [-width N] [-height N] [-scale N]
```

`contactsheet` tiles every Nth frame into one labelled page — by default every
20th frame, 10 columns, at half size (`-shrink 2`). `-from` and `-to` narrow it
to a stretch worth reading frame by frame (`-every 1`). The shrink drops pixels
rather than averaging them, because a blur hides what a contact sheet is read
for.

`framediff` names the frames where two runs of the same key script disagree,
with the bounding box of the difference; `-limit` (default 40, positive) caps
how many are listed:

```
  tick1023  (67,262)-(76,272)
  tick1024  (67,262)-(90,272)
4937 of 5798 frames present in both runs differ
```

Run the same script against the build before and after a change: the first
differing tick says what the change did and where. A screen that comes back
byte-identical is one the change did not touch. [`lgt.md`](lgt.md) and its
history show how both fit into a session.

`framestats` reports each frame's distinct colours and lit pixels — `-limit`
(default 40, `0` for all) caps the frames listed — and **exits nonzero when
every frame it was given is a single colour**:

```
  tick0900.png     colours=317    lit=52190 of 76800
1 frames, 0 of one colour, 0 with nothing lit
```

That status closes two holes in a sweep's judgment. A KTF run counts as working
when any pixel is lit, so a title that fills the screen white passes with all
76,800; an LGT run counts as working when it finishes its ticks, so one that
draws nothing passes too. Both were found by inspecting frames by hand. A run
whose frames are mixed exits zero, because a boot that starts black and then
draws is a working boot.

`zoom` crops a box out of one frame and scales it up pixel for pixel (`-scale`,
default 4; `-width` and `-height` default to the frame's edge). It answers
questions such as **which way a character is facing**: a character on a
240-wide screen is about twenty pixels, too small to read at 1:1. The box is
clipped to the frame rather than refused, because its coordinates are a guess;
a box entirely outside the frame is reported. The scale repeats pixels rather
than smoothing them, because a filter would invent the facing.

```sh
wfeature zoom out/tick0900.png look.png -x 30 -y 130 -width 90 -height 70 -scale 5
# (30,130)-(120,200) at 5x -> look.png (450x350)
```

## The local probes

Investigation aids that need a local archive live as `go test` functions that
skip unless their environment variables name one. Each answers one question:

| Probe | Package | Inputs | Answers |
|---|---|---|---|
| `TestLocalDisassembleProbe` | `internal/platform/ktf`, `internal/platform/lgt` | `WFEATURE_DISASSEMBLE_ARCHIVE`, `WFEATURE_DISASSEMBLE_RANGES` | the instructions in an address range a failure or profile named |
| `TestLocalAPIDemandProbe` | `internal/platform/lgt` | `WFEATURE_API_DEMAND_ARCHIVE`, `WFEATURE_API_DEMAND_PREFIX` | every platform member an LGT Java title links against, and which are served |
| `TestLocalBreakpointProbe` | `internal/platform/ktf` | `WFEATURE_BREAKPOINT_ARCHIVE`, `WFEATURE_BREAKPOINTS`, `WFEATURE_BREAKPOINT_LIMIT`, `WFEATURE_BREAKPOINT_WATCH`, `WFEATURE_BREAKPOINT_CLASSES`, `WFEATURE_BREAKPOINT_DUMP`, `WFEATURE_BREAKPOINT_SAVE`, `WFEATURE_BREAKPOINT_ROUTE` | registers and memory at guest addresses, armed before the first guest instruction, with an optional route to reach the state |
| `TestLocalNativeWatchProbe` | `internal/platform/ktf` | `WFEATURE_NATIVE_WATCH_ARCHIVE`, `WFEATURE_NATIVE_WATCH_OFFSETS`, `WFEATURE_NATIVE_WATCH_PIXELS`, `WFEATURE_NATIVE_WATCH_TRACE`, `WFEATURE_NATIVE_WATCH_TICKS` | what writes a word of an earlier-package title's state, or its pictures, and which of a list of addresses ran |
| `TestLocalSymbolizeProbe` | `internal/platform/ktf` | `WFEATURE_SYMBOLIZE_ARCHIVE` (addresses are edited in the test) | AOT method names for crash addresses |
| `TestLocalCheatProbe` | `internal/webhost` | `WFEATURE_CHEAT_ARCHIVE` | the browser's cheat path against a running ARM game |
| `TestLocalStartEndingProbe` | `internal/webhost` | `WFEATURE_ENDING_ARCHIVE` | a title that ends inside `startApp` on its second run, through the server |

```sh
WFEATURE_DISASSEMBLE_ARCHIVE=<abs path> WFEATURE_DISASSEMBLE_RANGES=0x114e00-0x114ea0 \
    go test ./internal/platform/ktf -run TestLocalDisassembleProbe -v

WFEATURE_API_DEMAND_ARCHIVE=<abs path> WFEATURE_API_DEMAND_PREFIX=lwc \
    go test ./internal/platform/lgt -run TestLocalAPIDemandProbe -v
```

The demand probe changes how a gap is worked: a title's missing members are a
**set** in its own metadata, and finding them one failure at a time costs a run
each. See [`history/lgt.md`](history/lgt.md#implementation-what-a-title-links-against-from-the-modules-own-metadata).
The KTF equivalent scans the module's string pool, which lists the names but
not which class owns each; [`internal/tools/apiscan`](#tools-under-internaltools)
runs that scan across a directory.

## checkgames

```sh
go run ./cmd/cli checkgames [-games dir]
```

A title keys its saves by an id its own descriptor declares — a KTF or LGT PID,
an SKT program number — and none of the three is reliably unique: two titles
carrying the same one resolve to the same save directory and overwrite each
other. `checkgames` scans the game root (default `var/games`) for that collision
on **all three platforms** and exits 1 when it finds any, so it is worth running
after adding archives. A clean root prints `no save owner is claimed by more
than one title under <root>`.

The identity beside the id separates a collision from a variant. KTF and LGT
declare an AID, so a save owner claimed under two AIDs is reported and one
title shipped twice is not. **An SKT descriptor declares no AID**; the class
the title runs stands in for it, and two archives running the same class are
one title shipped twice. The report labels that column `class` rather than
`AID` for SKT titles.

**It scans exactly what a Host offers**: the game root and one level of group
below it, which is `internal/gameroot`'s boundary and the listing the picker
builds. An archive filed deeper cannot be started from a Host, so its
collisions affect no save. Walking the whole tree once turned an ignored corpus
of several hundred archives into ten collision reports over a library that had
none; point `-games` at such a corpus to check it deliberately.

**Names are printed in the form their parser left them in.** The KTF descriptor
parser decodes its own non-UTF-8 fields, so a KTF name is already UTF-8; LGT's
`app_info` is kept as the bytes it came as and decoded for the report. Decoding
an already-decoded KTF name as EUC-KR a second time once corrupted every Korean
KTF name while ASCII names survived, which kept the defect invisible.

## importsaves

```sh
wfeature importsaves <external savedata dir> [-save dir] [-games dir] [-dry-run]
```

Converts another emulator's KTF save tree into the layout this one reads. The
source is the parent of that emulator's `fs/` and `db/` scopes, where `fs`
owners are archive AIDs, `db` owners are PIDs, and every database record is its
own percent-encoded file. The destination is
`<save root>/<PID>/<db|jdb|fs>/<name>`, with a WIPI C database as one file
holding its single record and a Java `DataBase` as one file holding every
record. Copying the external tree in place would leave every save invisible.

| Flag | What it does |
|---|---|
| `-save dir` | the save root to write into; defaults to `var/savedata/<profile>/ktf` |
| `-games dir` | the archives an external owner is resolved against; defaults to `var/games` |
| `-dry-run` | report what would be written without touching the save root |

Each converted entry prints as `<source> -> <owner>/<key> (<bytes> bytes)`, and
each entry left behind prints as `skipped <reason>`; the last line is
`imported N entries, skipped M` (`would import` under `-dry-run`). The AID-keyed
`fs` scope is the lossy direction: when several games share an AID, the source
already mixed their files, and an owner is resolved by asking which candidate
archive packages files of those names — and reported rather than guessed when
that does not decide it. Each owner's save directory is claimed while it is
written. Data whose game cannot be identified yet is kept in
`var/savedata/pending-import/` (see [`var/README.md`](../var/README.md)).

## ktfdump

`cmd/ktfdump` prepares a KTF game for a disassembler. A `client.bin` relocates
itself at startup, so the bytes in the archive are not the bytes that execute;
a disassembler needs the image after relocation and a map from address to
method.

```sh
go run ./cmd/ktfdump <game.zip> -image client.bin   # the relocated image; load it at 0x100000
go run ./cmd/ktfdump <game.zip> -symbols out.txt    # every AOT method body address and its name
go run ./cmd/ktfdump <game.zip> -classes            # the registered class table
```

`-image` runs the relocation entry and nothing else. `-symbols` and `-classes`
start the game and dump what it registered on the way to `startApp`, because a
class is in the table only once the game has asked for it.

**An older relocatable module needs no entry run for `-image`**: the loader
relocates those rather than the guest, so the image is written as it will
execute without any guest instruction having run. See
[`history/ktf.md`](history/ktf.md#the-older-modules-run-under-the-platform-and-all-three-of-them-play).

**A start that fails still dumps**, with the failure reported and the classes
the title reached. That is the case the tool is most often used for: the title
stopped before drawing, and the question is which class it was in.

`-classes` prints each class's instance layout under its methods: `at N` for an
instance field's byte offset and `static` for a static field's value, which is
what the one word in a field record means in each case. A field a title reads
directly off an object, rather than through a getter, is a question about that
layout.

## Tools under internal/tools

These are run with `go run ./internal/tools/<name>`; their flags are listed here
and their use is documented where the linked document owns it.

| Tool | Flags | Documented in |
|---|---|---|
| `acceptance` (`make acceptance`) | `-out dir` (default `var/acceptance`), `-platform ktf,lgt,skt`, `-timeout 60m` (per `go test` stage), `-since file` (`auto` = newest record in the output directory, empty = none), `-cache` (default true; carry an answer forward when the file and the build are unchanged), `-compare old.ndjson new.ndjson` (compare two records, run nothing), `-games dir[,dir]` (sweep these directories recursively, platform by content), `-exclude name[,name]` | [`testing.md`](testing.md#local-acceptance) |
| `apiscan` | `-games dir` (default `var/games/skt`), `-natives report.json` (a `runskt -diag` report listing the natives SKT registers at run time; without it the scan over-reports that half), or archive paths as arguments | below |
| `distcheck` (`make dist-check`) | `-dir build/dist`, `-root dir` (default: this source tree), `-run` (default true; unpack this machine's archive and ask its server for the version), `-phones` (require the APK and IPA) | [`running.md`](running.md#the-release-archives) |
| `javastub` | `-out dir` (required) | [`jvm.md`](jvm.md) |

`apiscan` reports, per archive and then ranked by how many titles want each,
the platform surface a title links against that the runtime does not answer,
without playing it. A `-diag` report gives one gap per run; this plans a session
from one pass over a corpus. Each archive is scanned as its own platform: an
SKT title's constant pools, a KTF client image's name pool (classes only; the
method half is not recoverable), and an LGT module started far enough to read
back its resolutions, which misses lazy calls and later virtual dispatch.

## Where the data lives

Game archives, saves and reports are described in
[`running.md`](running.md#where-the-data-lives). Nothing under `var/` is
touched by a rebuild.

## provision

```
wfeature provision <game.zip> [-save dir] [-number N] [-dry-run]
```

One local title checks a certificate a server issued to a handset once, years
ago; the copy its archive carries belongs to whichever handset downloaded it.
`provision` writes one sealed for a handset number into that archive's own save
directory and clears the record of an earlier run having deleted the packaged
`cert.c2s`. [`ktf.md`](ktf.md) and [`authentication.md`](authentication.md)
describe the check it satisfies.

| Flag | What it does |
|---|---|
| `-save dir` | the save root; defaults to `var/savedata/<profile>/ktf` |
| `-number N` | the handset number to seal for; defaults to the number `PHONENUMBER` answers with (`WFEATURE_PHONE_NUMBER` when set). It must have fewer than 12 digits |
| `-dry-run` | print the target (`<AID> (<owner>): certificate for handset <N> -> <path>`) and write nothing |

It refuses an archive it does not recognise, and recognition is not a name
match: the archive must package a certificate whose own checksum agrees with
the format, the only available evidence that this scheme is the one it uses.

**Nothing in the emulator runs this.** It is a deliberate, one-off command that
writes a persistent replacement, unlike the in-memory adapters below.

## Offline authentication compatibility

`runktf`, `runskt` and `runlgt` automatically apply the same recognized
adapters as the PWA. `-no-auth` turns them off for an explicit diagnostic
comparison; the old `-auth` flag is still accepted and changes nothing. After
startup, stderr reports the selected mechanism as `authentication: <status>`:

| Platform | Statuses |
|---|---|
| KTF | `ktf-certificate-23`, `ktf-certificate-52`, `ktf-subscriber-fallback` |
| SKT | `skt-license` |
| LGT | `lgt-cached-authentication`, `lgt-certificate-58`, `lgt-certificate-100`, `lgt-offline-notification`, `lgt-offline-authentication`, `lgt-save-subscriber`, `lgt-certificate-message` |

`authentication: unsupported` leaves ordinary behavior in place; the earlier
native KTF package always reports it. The notification, binary authentication
and certificate-message statuses answer an exact exchange locally and send
nothing outside the emulator. Certificate data and adapted class files stay in
memory; ordinary progress uses `-save`. This differs from `provision`, which
writes a persistent replacement. See
[mechanisms, recognition and limitations](authentication.md).
