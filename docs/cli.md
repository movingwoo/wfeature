# The command line

`cmd/cli` runs a game without a browser. It is the same emulator the server
hosts — the same loaders, the same ARM core, the same runtime — and the
difference is only what drives it: a socket and a page on one side, a terminal
and a tick count on the other. That is what makes it the debugging tool. A run
here can be given an exact number of ticks, a scripted route back to a scene, a
profiler, a GDB stub, or a cheat console, none of which a phone browsing to the
server would ever ask for.

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
subscriber number `PHONENUMBER` and `MIN` answer with. It only matters for two
titles, which want opposite things from it — [`network.md`](network.md).

## Commands

```
wfeature inspect <game.jar>                     read the archive, print its summary as JSON
wfeature runskt  <game.jar|game.zip> [flags]    run an SKT title
wfeature runlgt  <game.zip> [flags]             run an LGT title
wfeature runktf  <game.zip> [flags]             run a KTF title
wfeature invoke  <game.jar> <method> <descriptor> [arguments...]
wfeature importsaves <external savedata dir> [-save dir] [-games dir] [-dry-run]
wfeature checkgames [-games dir]                find titles that share a save directory
wfeature provision <game.zip> [flags]           write the certificate one title asks for
wfeature contactsheet <framedir> <out.png>      tile a run's frames into one page
wfeature framediff <dirA> <dirB> [-limit N]     name the frames two runs disagree on
wfeature zoom <frame.png> <out.png> [flags]     magnify a corner of one frame
wfeature licenses                               print the project and third-party notices
```

`inspect` and `invoke` both answer JSON. `invoke` calls one static method on the
archive's main class and prints what it returned, which is how a class file's
behaviour is checked without a game around it.

## What a run exits with

A run is read two ways: by a person, from the JSON summary, and by a batch
driving hundreds of archives, from the exit code. The summary was always the
complete answer and the code was not — `runktf` and `runlgt` reported a failed
tick in the summary and still exited zero, so a sweep counted a title that died
on its second tick as a title that ran.

| code | what it means |
|---|---|
| 0 | the run finished. No tick failed, or the guest ended itself |
| 1 | the run failed, or the tool did |
| 2 | the arguments were wrong |

The two kinds of 1 are told apart by whether a summary was printed. A run that
failed inside the guest prints its summary first — a failed run is exactly the
one worth reading, and its `tick_error` says what stopped it — and a run the
tool could not finish stops before it, having written the reason to stderr.

Four things deliberately exit **zero**:

- **a guest that exited on its own.** A title closing itself is what the Jlet
  or the Clet asked for, and the whole point of some runs is to reach it. It is
  the one zero that used to be silent: a run that stopped four hundred ticks
  short of `-ticks` with no error and no note is indistinguishable from a hang,
  and a sweep reading only the exit code counted a title that quit on its
  opening screen as a title that ran. Such a run now prints `the game exited at
  tick N: …` to stderr and carries `exited` and `exit_reason` in its summary —
  the platform call the guest was inside and the address it left from. The code
  stays zero, because the run did what it was asked; what changed is that the
  ending can be read. See [`session.md`](session.md).

  **A guest can end before the first tick, and that is the same ending.** A
  title whose first run installs itself quits inside `startApp` on its *second*
  run: it reads the flag it wrote, decides there is nothing to do, and calls the
  platform's exit. That used to arrive as a start failure — exit code 1 and no
  summary at all — where the identical call one tick later was an ending. Such a
  run now prints `the game exited before its first tick: …` and carries the same
  `exited` and `exit_reason` in a summary of no ticks. **A sweep that always
  starts from an empty save directory never sees this**, which is why it went
  unnoticed: it takes two runs over one save.
- **a run somebody interrupted.** Ctrl-C cancels the context rather than
  killing the process, so it reaches the exit code as the reason a tick
  stopped; the person who sent it does not need it reported back.
- **a route that did not arrive.** `route_stopped_at` and `route_reason` are in
  the summary, on stderr, and are an answer rather than a failure: a route is a
  way back to a scene, and its budget running out says the scene moved, not
  that the emulator broke.
- **a run that drew nothing.** Whether a frame is blank is a question about
  pixels, and `framestats` is the command that answers it — see below. A run
  that ticked its whole count without failing did what it was asked.
- **a callback that ended in an exception nothing caught.** All three platforms
  end the callback rather than the session for one, because that is what the
  language and the handset do; the run continued and its code says so. What
  says it happened is `uncaught` and `uncaught_first` in the summary, on
  `runktf`, `runlgt` and `runskt` alike — **a sweep reading only the exit code
  and `tick_error` counts a title that fails every paint as one that plays**,
  so a sweep has to read them. See [`ktf.md`](ktf.md), [`lgt.md`](lgt.md) and
  [`skvm.md`](skvm.md).

`checkgames` and `framestats` are the other two commands whose exit code is an
answer rather than a status; both are documented with the answer they give.

## One setting, three clocks

A person moving the browser's speed control is asking one question, and every
platform here answers it with the same numbers: 0.25 through 4 in the menu,
clamped to `[0.1, 16]` by `backend.ClampSpeed`, with zero meaning the speed the
game was written for. What a multiplier means is the same everywhere too — the
guest's clock runs at that rate, and the waits between its callbacks cost that
much less. Both halves are needed: a game that measures its own elapsed time
and steps by it would, called twice as often against an unchanged clock, simply
step half as far and not speed up at all.

What differs is only what there is to scale.

| platform | the guest's clock | what a Host waits |
| --- | --- | --- |
| KTF, both packages | a wall clock scaled by `backend.SpeedClock` | the callback interval, divided |
| LGT | virtual: a tick moves it and nothing else does, so there is no rate to change | what a tick of guest time is allowed to cost |
| SKT | the runtime's own clock, which `System.currentTimeMillis` and `RecordStore.getLastModified` both read | the VM's `Thread.sleep` and timed `Object.wait`, divided |

The MIDP case is the one where the two halves are most visibly one thing: a
title's frame loop is a thread that sleeps, so scaling the sleep without the
clock leaves it stepping less each time it wakes, and scaling the clock without
the sleep leaves it stepping further at the same rate. Both, and it runs
faster.

## runktf

```
wfeature runktf <game.zip> [-ticks N] [-frame out.png] [-framedir dir] [-save dir]
                           [-play] [-speed N] [-key tick:name] [-hold N] [-route script]
                           [-touch tick:action:x,y] [-park tick[:ms]]
                           [-cheat] [-gdb host:port] [-screen WxH]
                           [-diag report.json] [-audio out] [-scale N]
                           [-profile report.txt] [-profile-folded stacks.txt]
                           [-profile-from tick]
```

| Flag | What it does |
|---|---|
| `-ticks N` | how many ticks to run, 64 by default. Without `-play` the run stops early at the first lit frame |
| `-frame out.png` | write one PNG of what the screen ended up holding |
| `-framedir dir` | write `tick0042.png` into `dir` every time a new frame is painted |
| `-save dir` | the save tree. Defaults to `var/savedata/<profile>/ktf` |
| `-play` | run on the wall clock at the game's own pace instead of stepping ticks |
| `-speed N` | multiply the guest clock; implies `-play` |
| `-key tick:name` | press a key at a tick, e.g. `-key 300:fire`. Repeatable; implies `-play` |
| `-touch tick:action:x,y` | one touch at a tick, e.g. `-touch 300:press:120,160`. The action is `press`, `drag` or `release`; the coordinates are the guest's own screen. Repeatable; implies `-play` |
| `-park tick[:ms]` | park the game at a tick the way a server does when the page goes away, and resume it. `-park 40` runs the Jlet's `pauseApp` and `resumeApp` back to back; `-park 40:5000` leaves it parked for five seconds first, which under `-play` is five seconds of guest clock the title is about to discover it lost. It is the only way to drive the lifecycle from a terminal — see [`ktf.md`](ktf.md), "The park the guest is told about" |
| `-hold N` | how many ticks a `-key` press — or a route's `key` step — is held before its release, 1 by default |
| `-route script` | replay a scripted way back to a scene (below); works on both generations of package |
| `-serve` | drive the run a command at a time over stdin and stdout (below); works on both generations of package |
| `-cheat` | attach the text cheat console, paced to about real time; implies `-play`. Without `-ticks` the run continues until it is interrupted, on both generations of package |
| `-gdb host:port` | serve a GDB stub over the ARM core. The run does not wait for a client; attach with `target remote host:port` |
| `-diag report.json` | write the runtime-boundary diagnostics (below) |
| `-audio out` | write what the guest played: `out.mid` for its MIDI events, `out.wav` for its samples |
| `-screen WxH` | the handset the game is told it runs on, 240x320 by default |
| `-scale N` | run the hqx filter over captured frames: 2, 3 or 4 |
| `-profile report.txt` | sample the ARM core and rank the result by symbol |
| `-profile-folded stacks.txt` | the same profile as flamegraph-folded lines |
| `-profile-from tick` | throw away samples before that tick, so a profile covers the scene rather than the loading before it |

`-speed`, `-key` and `-cheat` each turn `-play` on, because none of them means
anything to a run that is stepping ticks by hand.

**`-screen` changes what the game is told, not how big the picture is drawn.**
That is `-scale`. Every local archive here was packaged for the 240x320 handset
this carrier standardised on, so the default is the answer for all of them and
this flag is for the archive that turns up packaged for something smaller — the
case `runskt` already had. It has to be given at the start and cannot change
during a run: the screen framebuffer is built once, on the game's first request
for it. What a title does with a size it has no artwork for is its own
business, and two local titles write into the framebuffer with the stride they
were told, so a size is answered to every surface at once or to none —
[`session.md`](session.md) has why that is the whole of the contract.

**A larger handset is a screen this flag takes too**, and `runlgt` has it now
for that reason: the browser has offered 240x400 and 320x480 since the menu
grew, and a size the page can ask for has to be reachable from a command line
or nothing can measure it. What the local library does on each of them is
in [`session.md`](session.md).

**It is a flag rather than a rule, and on this platform that is a measured
decision rather than a missing feature.** A KTF archive does declare a size —
`DisplaySize` in its `__adf__` — and reading it would look like the obvious
automation. Thirteen of the local titles declare `176*220` and twelve of them
draw across the whole 240x320 screen, so adopting the declaration would shrink
twelve working titles to fix one band that turned out to be a game's own
artwork. [`ktf.md`](ktf.md), "A band beside a title screen was the title's
own", is that investigation; this flag is what remains for the archive that
really does want another handset.

**`-hold` is worth raising before believing a title has stopped.** A press and
its release used to be delivered in the same tick here, and a title that samples
the keypad once a frame never sees such a press: two local titles sit on their
own title screen for a whole run that way, and one LGT title's character select
answers `right` but not `OK` until the press is held. Twenty ticks is a press a
game cannot miss.

**A title that busy-waits on the clock needs `-play`.** A stepped run holds the
guest clock still for the length of a Host service call — the clock only moves
between ticks — so a title that waits inside `keyNotify` by polling
`MC_knlCurrentTime` until enough time has passed never sees it pass. It spends
its whole service allowance instead and the run dies with `exceeded its step
allowance`, which reads exactly like a hang in the emulator. One local KTF
title does this when its menu starts a new game: stepped, it fails there every
time; with `-play`, the same route reaches the play screen. **The tell is in
`-diag`** — the tail of the trace is one boundary repeated (here `wipic 0x1c`,
the current-time call) rather than the mix of drawing and allocation a loading
screen makes. Reach for `-play` before believing the title is stuck.

**`-play` is not the same as `-play -speed 8`, and a batch that only measures
the second one misses a whole failure.** A wait the guest spends polling the
clock costs steps in proportion to how long it lasts, so eight times the clock
is an eighth of the steps: a title whose opening sequence waits five seconds
inside one timer callback fits under the service allowance at 8x and does not
at 1x, which is the speed a person plays it at. It is now the wait rather than
the steps that bounds such a call — [`ktf.md`](ktf.md), "The fifteenth round" —
but the lesson about the batch stands, because a speed multiplier changes what
a run is able to reach.

### Key names

`-key` and a route script share one table, so `fire` cannot mean two things:

```
up  down  left  right  fire (= ok)  soft1  soft2  soft3 (= ez)  clear  call  hangup
```

plus any single character from `0`–`9`, `*` and `#`.

`soft3` is `MH_KEY_SOFT3`, the third soft key these handsets carry beside the
two under the screen. It is worth knowing for the same reason `call` is: a
title can ask for it and a keypad has no other way to reach it. One LGT title's
party screen asks for it by its label on the handset — "press the EZ key for the
submenu" — which is why the name has `ez` as an alias.

`soft1` is the one of the three the browser can send: the keypad's `Menu`
button is that key, and so is `M` on the keyboard. It is the key a handset's
own screen labelled 메뉴 and the one a title of this era puts its in-game menu
on, which is what earned it the button. `soft2` and `soft3` stay this command's
to send — `key soft2|ez`. That is a gap rather than a decision that nothing
needs them: one LGT title asks for the EZ key by name, as the paragraph above
says. The keypad has only so many places a thumb looks, and the menu key is the
one every kind of title here reaches for.

The page sends `-6` for it, and that number needs no translating anywhere:
`MH_KEY_SOFT1` and the MIDP soft key a MIDlet of this era compares against are
both `-6`, so the WIPI platforms and the MIDP one take the same code. The page
sent a positive `6` when it briefly carried all three soft keys under their own
names; that value still translates, because a shell served from a phone's cache
is a page from an older build. `web/keypad.test.mjs` holds the page's end of
this and `internal/session`'s key translation test holds the server's.

`call` is the handset's send key. It is worth knowing about because a keypad has
no other way to reach it and a game that answers it usually answers with a quick
save — the reason the browser keypad carries a `Call` button of its own.

### Routes

A route is a scripted way back to a scene, written in terms of what the screen
is doing rather than absolute tick numbers — the numbers are what break when a
fix changes which branch the game takes.

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

`key` holds its press for `-hold` ticks and those ticks are the route's, so a
script that presses twenty-tick keys pays twenty ticks each; `press`/`release`
are still there for a hold a step has to span. Both searching waits are
bounded, defaulting to twenty times the run they are looking for.
[`ktf.md`](ktf.md) has the rest: what a route that never arrives reports, and
why `mark` is where a profile of a scene starts.

**Keep the ones that took work.** Getting past a title's notices, its
authentication offer, its slot and character screens and a ten-minute opening
can be two hundred lines of route and an afternoon of guessing, and the result
is what every later question about that title starts from. `var/routes/` is
where they go — beside the archives they are written against, ignored by Git
for the same reason the archives are.

**A reported session is a route.** A page log under `var/logs` carries every
key press and release with a timestamp, so dividing the elapsed milliseconds by
the guest's own tick length turns one into a script that replays what the
player did. That is how a fault reported from a phone gets reproduced here: one
title's crash needed 1,891 ticks of somebody else's play to reach, and no
hand-written route found it.

### Driving a run from outside

A route is a script: it is written before the run and replays the same way
every time, which is what makes it a repro. What it cannot do is look at the
screen and decide the next move — that decision was taken when the file was
written. Finding the way through a title nobody has a route for is the other
shape of the same work: press something, look, press something else, and write
the route down only once the way is known.

`-serve` is that loop with the screen answered as a number. **One JSON command
per line on stdin, one JSON answer per line on stdout.** It is on `runktf`
(both generations of package), `runskt` and `runlgt`.

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

**A bad command is answered, not fatal.** An unknown command, a key name that
is not in the table, a script that does not parse, a path that cannot be
written: each comes back as `{"ok":false,"error":"…"}` and the run carries on.
An exploratory session is minutes of guest execution deep by the time a typo
happens, and losing it to a typo is losing the whole point. The one input that
does end the session is a line longer than 1 MiB, because there is no way to
resynchronize in the middle of one.

Every request may carry an `id` of any JSON shape, and it comes back on the
answer unchanged, so a caller that pipelines can pair them up.

| Command | Fields | What it does |
|---|---|---|
| `step` | `ticks` (default 1, max 1000000), `screen` | advance the run. Stops early when the guest ends or when a tick that did nothing is followed by nothing due |
| `key` | `key`, `hold`, `repeat`, `action` | one key from the shared table. `action` is `tap` (default), `press` or `release` |
| `touch` | `action` (`press`/`drag`/`release`), `x`, `y` | one pointer event in the guest's own coordinates |
| `park` | `ms` | park the game the way a Host does when the page goes away, hold it for `ms`, and resume |
| `screen` | — | the screen report below |
| `pixel` | `x`, `y` | one pixel in the guest's own coordinates |
| `diag` | `path` | write the diagnostics report `-diag` writes |
| `shot` | `path` | write the frame `-frame` writes, through the same encoder |
| `route` | `path` | replay a route script from where the run is now |
| `quit` | — | answer, then end the session |

Every answer carries `ok`, `total_ticks` (what the serve session has spent
altogether) and, where a command spent any, `ticks`. `step`, `key`, `touch`,
`park` and `route` also carry `digest`, the cheap frame identity a route waits
on, which is all a caller needs to ask "did anything change". `progressed` and
`stalled` say whether the last tick ran guest work and whether anything is left
that would. A guest that ends is reported as `"ended":true` with an
`end_reason`; the session stays open afterwards, because a screen, a shot and a
diagnostics report are exactly what is wanted about a run that just ended.

**`key` holds its press for the ticks it was given**, and those ticks are the
caller's, the way a route's are. Without `hold` the run's own `-hold` applies.
The names are the table above — the one `-key` and a route script read, so
`fire` cannot mean two things. `press` and `release` are the two halves on their
own, for a hold that has to span other commands.

A capability a platform does not have is refused by name rather than ignored:
the earlier KTF package answers `this platform writes no diagnostics` and
`this platform has no park to run`, and `runlgt` answers `this platform has no
pointer`.

#### The screen report

```json
{"width":240,"height":320,
 "rgba_sha256":"782768ca6e428e1b478b4210e7454e4399f602756caa9401d2cf5bb08d484dae",
 "non_black_pixels":73746,"visible_pixels":76800,"flushes":399,
 "digest":"801bbe1714a3dcc1"}
```

**`rgba_sha256` is the identity a regression assertion is written against.** A
PNG of a frame is not one: the encoder picks filters and a compression level,
so two runs that drew the same picture can write different files, and an
assertion on the file is an assertion about the encoder as well as about the
game. This hashes the pixels instead — the geometry first as two little-endian
64-bit words, then the normalized RGBA bytes row by row. A fully transparent
pixel contributes four zero bytes whatever colour was left under it, because
what is under a transparent pixel is not part of the picture. Two runs of the
same commands against the same archive answer the same hash.

`non_black_pixels` is the same first-frame signal the probes read — a title
that reached its title screen and one that painted nothing are otherwise the
same JSON — and `visible_pixels` is how much of the screen is not transparent.

**`-serve` does not imply `-play`**, for the reason `-route` does not: a caller
that steps and then looks wants its ticks as fast as the guest can be driven,
and gets them from the manual clock. Add `-play` when the point is to watch it,
or when the title is one that busy-waits on the clock.

`-cheat`, `-route`, `-key`, `-touch`, `-park`, `-framedir` and `-ticks` are
refused alongside `-serve`. Each of them instructs the loop `-serve` replaces,
and the cheat console and a serve session both want stdin and stdout. The
summary a run normally prints at the end is not printed either: on a serve
session stdout is the protocol, one line per answer.

### Reading a diagnostics report

`-diag` writes JSON. `counts` says how many times the game crossed each runtime
boundary; `trace` is the most recent crossings in order, and is a debug-profile
field — a release build counts but does not keep the order.

**A start that fails writes one too.** A title that dies before its first tick
— inside its own `startApp`, or in the initialization before it — leaves no
session behind, and for a long time that meant no report for exactly the
archives whose failure is hardest to read. A failure past the client's entry
now carries its trace, and the report's `summary` is `start_error` alone. It is
the fastest way to see what the title was doing: the last twenty lines usually
name the resource it opened, the class it loaded and the method it resolved
just before it stopped.

A game that is working repeats `flush lcd`, `Card.repaint` and `Graphics.*`
once per tick. `stub table`, `not implemented`, `throw`, `raise` and `error`
name what to implement next. [`ktf.md`](ktf.md) reads a real report line by
line.

Three counters carry their argument rather than only their name, because a
start-up gate is usually one of them and a gate with no name in the trace says
only that the title asked something: `stream <resource> found=<bool>` for a
class-path resource, `exists <path> found=<bool>` for a filesystem test, and
`sysprop <name>` for a handset property.

A debug run also carries `arena blocks recorded on release` and
`arena blocks checked on reuse`, which are how much of the guest heap the
use-after-free detector covered, and `arena use after free` if a title wrote
into memory it had given back. The two coverage counts are there so that a
report without the third one can be told apart from a build that was not
watching — a release build is not, and carries none of the three.

### Byte patches and cheat tables

`-cheat` attaches a console whose `help` lists its commands. Two of them are
about going through a gate rather than watching one, and are worth reading
here; they work on every platform `-cheat` does.

```
patch <name> <addr> <expect> <replace>   replace guest bytes
unpatch <name>|all                       put back what a patch replaced
unpatch -forget <name>                   drop the record without writing
patches                                  what is applied now
```

Everything else the console offers observes. A scan says where a value is, a
watch says what writes it, a trace says what the guest asked the platform for
— and none of them answers the question a title stopped at a check actually
raises, which is what it would do if the check had passed. The only way to see
behind a gate is to go through it.

**A patch declares the bytes it replaces, and is refused when they are not
there.** An address is only an address under the layout it was found in, and
layouts move between builds and between titles. The refusal says what it found
instead, which is often the fastest way to learn the bytes in the first place:

```
> patch gate 0x00150000 deadbeef 00000000
cheat: patch "gate" span 1 at 0x00150000 expects deadbeef but memory holds 90011500
```

**An entry applies as a unit.** Skipping a check is rarely one word — it is a
branch and the constant it compares against — so every span of an entry is
verified before any is written, and a write that fails part of the way through
puts back what it had already replaced. Multi-span entries come from a table
file rather than from the one-span `patch` command.

Guest code is writable, so a patch reaches it. What it is not is somewhere a
value search walks: an arena of platform veneers decodes as thousands of
plausible candidates at any width. `regions` marks those `(code, not scanned)`.
The client image is one span of instructions and data with no boundary recorded
between them, so it is swept whole and stays so.

`save <file>` writes the frozen values, the watches and the patches as JSON,
and `load <file>` applies them — patches first, because a patch is usually what
makes the rest reachable, and a refused one stops the load before any value is
written.

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
— and a patch is true of the image rather than of what the file was called.
`image` is the SHA-256 of the loaded executable image; `file` is the SHA-256 of
the archive it was read from, and is the fallback for a platform with no single
image to hash. Loading checks the image first and then the file, and says so
when neither matches:

```
> load table.json
cheat: table patch 1: patch "gate" span 1 at 0x00150000 expects 90011500 but memory holds 311c1635
(this table was made against a different image; its addresses may mean nothing here)
```

`game` is a label beside the key rather than the key. A table written before
the hashes existed carries only that, and still loads — the addresses in it
were expensive to find, and it just cannot say what it was made for. Addresses
in `entries` stay plain numbers, as they always were; a patch address is hex
because it is read off a disassembly, and a plain number is accepted there too.

## runskt

```
wfeature runskt <game.jar|game.zip> [-ticks N] [-frame out.png] [-framedir dir]
                                    [-key tick:name] [-hold N] [-route script]
                                    [-save dir] [-diag report.json] [-audio out]
                                    [-screen WxH] [-cheat] [-trace]
```

| Flag | What it does |
|---|---|
| `-ticks N` | how many ticks to run, 64 by default |
| `-frame out.png` | write one PNG of what the screen ended up holding |
| `-framedir dir` | write `tick0042.png` into `dir` every time the screen changed |
| `-key tick:name` | press a key at a tick, e.g. `-key 300:fire`. Repeatable |
| `-hold N` | how many ticks a `-key` press is held before its release, 1 by default |
| `-route script` | replay a route script, the same format `runktf` and `runlgt` take. It is the only way to script two keys held at once, which is what a keyboard produces and what `-key` cannot say |
| `-save dir` | the save tree. Defaults to `var/savedata/<profile>/skt/<owner>` |
| `-diag report.json` | write what the run used: classes loaded, classes missing, and a call count per registered native |
| `-audio out` | record what the run played as `out.mid` and `out.wav`, the same recorder `runktf` and `runlgt` take |
| `-screen WxH` | the handset screen, 240x320 by default |
| `-cheat` | attach the text cheat console. Without `-ticks` the run continues until it is interrupted |
| `-serve` | drive the run a command at a time over stdin and stdout, as `runktf -serve` does |
| `-trace` | one log line per bytecode instruction. Off unless asked for, in both build profiles — see below |

The summary a run prints carries `ticks` and `lit` — the count of non-black
pixels — beside the MIDlet's state, because "active with a Canvas shown" and
"actually drawing" are different answers and only the second one means the title
is running.

Key names are the shared WIPI ones — `up`, `down`, `left`, `right`, `fire`
(`ok`), `soft1`, `soft2`, `call`, `clear` (`clr`, `back`), the digits, `*` and
`#`. **`clear` is the one worth remembering here**: titles of this era draw
`BACK:CLR` on every screen that can be left, and leaving a screen is often what
commits what was changed on it.

**`-trace` is a flag rather than something a debug build does on its own.** The
line sits on the hottest path there is: a second of play writes on the order of
a million of them, and leaving it on cost more than the emulation and moved
every timing it was opened to look at — a debug build ran one title eighteen
times slower for it, and a release build built the line and threw it away. It is
the same bargain `runlgt -trace N` makes, without the count. What it cost and
how that was measured is in [`jvm.md`](jvm.md), "What a guest instruction cost".

`-diag` answers a different question from KTF's report of the same name. There
is no ordered trace here: what an SKT run is asked is which of the runtime's
Java surface the title reached, and a call count of zero beside a registered
native is the answer worth reading. [`skvm.md`](skvm.md) has what it found
across the local titles.

`-screen` is here because the screen is not the same handset for every title on
this vendor. One local archive branches on the width and asks for a 240-wide
artwork set it does not contain, because it was packaged for a smaller phone.

**It no longer needs the flag.** An SKT descriptor declares no screen size —
every key of every local `.msd` and every manifest was inventoried and none of
them carries one — but the archive says it another way, in the names of its own
resources, and that is now read: with no `-screen` the run takes the handset
the archive was packaged for. `docs/skvm.md` has the title's own branch, the
rule, and why it is narrower than the equivalent on the other WIPI platform.
The flag is what remains for the archive the rule does not recognise, and it
still wins whenever it is given.

`-cheat` attaches the same console the two WIPI paths take, against the
synthetic address space this platform lays over its object graph
(`docs/skvm.md`, "A heap with addresses in it"). The vocabulary is the same, and
so is the pacing — commands run between Host passes, which is the only time
reading and freezing the graph is safe. Two commands read differently here:
`regions` names classes rather than a module and a heap, so a hit says which
class it is in, and `watch` answers "this platform cannot watch writes" because
nothing instruments a `putfield`.

`-audio` is the CLI's speaker, and it is also how this platform's sound was
found to be silent: the summary carries `audio_midi_messages` and
`audio_wave_samples` beside the file it wrote, and both were zero for every
archive until the timeline and the clock it starts sounds on were made the same
one. Fifty-six of the local archives record MIDI now and sixteen record
sampled sound, in four hundred ticks each. [`audio.md`](audio.md) has what was
wrong and what a recording is worth reading for.

There is no `-play` or `-speed` here, though the runtime itself now takes a
speed setting: a MIDlet's threads sleep against a clock the runtime owns, and
scaling that clock together with the sleeps taken against it is what a
multiplier does — see "One setting, three clocks" above. What this command
lacks is the flag, not the ability. A tick here is a real frame: the run is
paced at one every 16ms, the same interval the server session drives the same
runtime with, and a run of 1,800 ticks takes about half a minute of real time.

Key names are the table `runktf` uses, translated to the MIDP key codes a MIDlet
compares against — the direction pad, `fire`, `soft1`, `soft2`, `call`, and the
twelve phone keys. The names a MIDlet has no code for are refused rather than
guessed at.

## runlgt

```
wfeature runlgt <game.zip> [-ticks N] [-frame out.png] [-framedir dir] [-save dir]
                           [-key tick:name] [-hold N] [-steps N] [-cheat] [-screen WxH]
                           [-audio out] [-trace N] [-trace-live filter]
                           [-route script]
                           [-profile report.txt] [-profile-folded stacks.txt]
                           [-profile-from tick]
```

| Flag | What it does |
|---|---|
| `-ticks N` | how many ticks to run, 64 by default. Unlike `runktf` the run does not stop at the first lit frame — it runs the count, or until the game exits |
| `-frame out.png` | write one PNG of what the screen ended up holding |
| `-framedir dir` | write `tick0042.png` into `dir` every time a new frame is painted |
| `-save dir` | the save tree. Defaults to `var/savedata/<profile>/lgt/<PID>` |
| `-key tick:name` | press a key at a tick, e.g. `-key 300:fire`. Repeatable |
| `-hold N` | how many ticks a `-key` press — or a route's `key` step — is held before its release, 1 by default |
| `-steps N` | the instruction budget for one call into the game |
| `-screen WxH` | the handset the game is told it runs on, 240x320 by default |
| `-route script` | replay a scripted way back to a scene, the same script format `runktf` takes |
| `-cheat` | attach the text cheat console, paced to about real time |
| `-serve` | drive the run a command at a time over stdin and stdout, as `runktf -serve` does. This platform has no pointer, so `touch` is refused |
| `-audio out` | write what the guest played: `out.mid` for its MIDI events, `out.wav` for its samples |
| `-trace N` | keep the last N platform calls and dump them at the end, or on a failure |
| `-trace-live filter` | stream platform calls to stderr as they happen, keeping the lines that contain `filter`. `""` keeps every call |

`-key <tick>:<name>` releases the key `-hold` ticks later, and the release is
scheduled rather than queued behind the press: a game that samples the keypad
once a frame would otherwise never see both. Key names are the same table
`runktf` uses.

**`-route` is worth reaching for before a long `-key` list.** These titles take
thousands of ticks to reach anything worth looking at, and a script written in
absolute tick numbers lands somewhere different on every run — the pacing is
the wall clock, not a step count. A route waits on what the screen is doing
instead. With `-route` the run has no default tick count: it runs until the
script arrives, and `-ticks` becomes the budget that stops one that never
does. A route's `key` step is held for `-hold` ticks here too, which matters
more on this platform than on any other: one title's character select ignores
a one-tick press entirely.

`-steps` is worth raising when a world load is the thing being measured — a
title's whole entry into the world happens inside one timer callback, so it is
one call's budget rather than a run's.

The JSON summary reports `busy_ms` — what the run cost the host, with the
`-cheat` pacing left out — plus `slowest_tick` and `slowest_tick_ms`. A load
happens inside a single tick, so an average hides it and the slowest tick *is*
the load: that pair is how a change to interpreter throughput is checked
against a real title rather than against a benchmark.

It also reports `guest_ms`, the guest's own clock. **A tick no longer stands
for a fixed span** — it covers the wait until the guest's next scheduled work
(`lgt.md`) — so the tick count says nothing about how much guest time a run
covered. `guest_ms` divided by `flushes` is the milliseconds per frame the
title is being given, which is the number a pacing change is judged by: a run
of the same length before and after says what the title's own frame rate did.

And `steps` with `ns_per_step`, the guest instructions retired and what each
cost the host. **The two answer different questions and a run needs both.**
`busy_ms` alone moves when a change alters how much guest work a tick holds,
which a scene reached by a route does from run to run; `ns_per_step` holds that
still and is what a throughput change has to move. A pacing change is the
opposite case and shows up in neither — it moves guest milliseconds per frame
while the host's cost per instruction does not budge, which is exactly how one
title lost a third of its frame rate without any profile noticing.

### Reading the platform-call trace

The two trace flags answer different questions and are worth keeping straight.

`-trace N` is a ring: it says **what the game was told just before it went
wrong**. That is the right shape for a fault, since a module dies at an address
in its own code hundreds of instructions past the platform call that caused it.

`-trace-live` is a stream, for **what the title ever did with something**. The
ring cannot answer that: by the time a screen looks wrong the interesting call
is a hundred thousand calls back and long since overwritten. A title asks for
the framebuffer width tens of thousands of times a second, so the filter is
what makes the output readable — filter to a slot family and read the story:

```sh
wfeature runlgt <game.zip> -ticks 600 -save /tmp/probe -trace-live fs \
    -key 40:fire -key 330:down -key 390:fire 2>calls.txt
```

Both forms **read the arguments that are names**, which is the difference
between a line and a finding:

```
wipic 0x196 fsRemove("Save0.dat" | 0x400fff4c, 0x1, ...) = 0x0 (0) from 0x6df3
wipic 0x1a0 fsIsExist("Save0.dat" | 0x400fff10, 0x1, ...) = 0xfffffff4 (-12) from 0x6ce7
```

The raw registers stay after the `|`, because an address is what a fault
reports. Only the slots whose arguments are names are read out of guest memory;
reading every call's would change what the trace is measuring.

### Guest profiling

`-profile` ranks the guest code the title is running, `-profile-folded` writes
the same thing as flamegraph-folded lines, and `-profile-from <tick>` throws
away the samples before a tick so the profile covers the scene rather than the
loading in front of it. They are the same three flags `runktf` takes and the
same sampler behind them.

What comes back is not method names — a Clet has none — but the **address range
of the loop that is running**, which is what `cmd/ktfdump`'s LGT counterpart,
the disassemble probe in `internal/platform/lgt`, is then pointed at:

```sh
wfeature runlgt <game.zip> -ticks 400 -profile report.txt
# 92.33%  1204356  0x3a550-0x3a698

WFEATURE_DISASSEMBLE_ARCHIVE=<abs path> WFEATURE_DISASSEMBLE_RANGES=0x3a550-0x3a698 \
    go test ./internal/platform/lgt -run TestLocalDisassembleProbe -v
```

[`lgt.md`](lgt.md) has what that found across the local titles, which is that
every one of them spends most of its instructions in about a kilobyte of code.

`runlgt` has no `-play`, `-speed`, `-diag`, `-scale` or `-gdb`.
Their absence is not a decision about LGT — the guest clock, the frame loop and
the cheat engine are shared, and the session underneath does take a speed
setting, so the flags would mostly work — it is that nothing here has needed
them yet. `-route` was the one that was needed, and it
is the same script and the same runner `runktf` uses: the route machinery lives
in `internal/route` and takes the key table and the four session operations it
needs (advance, digest, key, stall) as functions, so neither platform's session
type is in it.

## contactsheet, framediff, framestats and zoom

Reviewing a `-framedir` run. A scripted run only says something if somebody
looks at the frames, and a run is a few thousand of them.

```sh
wfeature contactsheet <framedir> <out.png> [-every N] [-columns N] [-shrink N] [-from tick] [-to tick]
wfeature framediff <dirA> <dirB> [-limit N]
wfeature framestats <framedir|frame.png> [-limit N]
wfeature zoom <frame.png> <out.png> [-x N] [-y N] [-width N] [-height N] [-scale N]
```

`contactsheet` tiles every Nth frame into one labelled page — every 20th frame,
10 columns, at half size by default. `-from` and `-to` narrow it to a stretch
worth reading frame by frame (`-every 1`). The shrink drops pixels rather than
averaging them, because a blur hides exactly what a contact sheet is read for.

`framediff` names the frames where two runs of the same key script disagree,
with the bounding box of the difference:

```
  tick1023  (67,262)-(76,272)
  tick1024  (67,262)-(90,272)
4937 of 5798 frames present in both runs differ
```

Run the same script against the build before a change and the build after it,
and the first differing tick says what the change did and where. A screen that
comes back byte-identical is a screen the change did not touch — worth knowing
before believing a fix. [`lgt.md`](lgt.md) has how both fit into a session.

`framestats` says what is on each frame — how many distinct colours it holds
and how many pixels are lit — and **exits nonzero when every frame it was given
is a single colour**. That status is the point: it is what lets a whole-set
sweep close the two holes in its own judgment without a person in the loop.

```
  tick0900.png     colours=317    lit=52190 of 76800
1 frames, 0 of one colour, 0 with nothing lit
```

The holes are these. A KTF run is counted as working when any pixel is lit, so
a title that fills the screen white passes with all 76,800 of them; an LGT run
is counted as working when it finishes its ticks, so one that draws nothing at
all passes too. Both were found by looking at a frame by hand. Point this at a
sweep's captured frames and neither passes again.

A run whose frames are mixed exits zero, because a boot that starts black and
then draws is a working boot — it is only the run that never drew anything that
fails.

`zoom` crops a box out of one frame and scales it up pixel for pixel, which is
the question the other two cannot answer: **which way is the character
facing.** A handset screen is 240 across and a character on it is about twenty
pixels, so a report of the shape "the attack goes the wrong way" is checked by
reading a sprite too small to read at 1:1 — and at five times it is plain. The
box is clipped to the frame rather than refused, because its coordinates come
from guessing where the sprite is; a box entirely outside the frame is
reported. The scale repeats pixels rather than smoothing them, for the reason
the shrink drops them: a filtered sprite is one whose facing the filter
invented.

```sh
wfeature zoom out/tick0900.png look.png -x 30 -y 130 -width 90 -height 70 -scale 5
# (30,130)-(120,200) at 5x -> look.png (450x350)
```

## The local probes

Three throwaway investigation aids live as skipped tests rather than commands,
because each needs an archive path and answers one question about it:

```sh
# the instructions around an address a failure named
WFEATURE_DISASSEMBLE_ARCHIVE=<abs path> WFEATURE_DISASSEMBLE_RANGES=0x114e00-0x114ea0 \
    go test ./internal/platform/ktf -run TestLocalDisassembleProbe -v

# every platform member an LGT Java title links against, and which are served
WFEATURE_API_DEMAND_ARCHIVE=<abs path> WFEATURE_API_DEMAND_PREFIX=lwc \
    go test ./internal/platform/lgt -run TestLocalAPIDemandProbe -v
```

The demand probe is the one that changes how a gap is worked: a title's missing
members are a **set** in its own metadata, and finding them one failure at a
time costs a run each. See [`lgt.md`](lgt.md), "What a title links against".
The KTF equivalent is a scan of the module's string pool — see
[`ktf.md`](ktf.md), "A widget toolkit that is one text box" — which lists the
names but not which class owns each.

## checkgames

```sh
go run ./cmd/cli checkgames [-games dir]
```

A title keys its saves by an id its own descriptor declares — a KTF or LGT PID,
an SKT program number — and none of the three is reliably unique: two titles
carrying the same one resolve to the same save directory and overwrite each
other. `checkgames` scans the game root for that collision on **all three
platforms** and exits nonzero when it finds any, which makes it worth running
after adding archives.

What separates a collision from a variant is the identity beside the id. KTF
and LGT declare an AID, so a save owner claimed under two AIDs is the case
worth reporting and one title shipped twice is not. **An SKT descriptor
declares no AID**, and what stands in for it is the class the title runs: two
archives running the same class are one title shipped twice. The report labels
that column `class` rather than `AID` for SKT titles, because it is not the
same thing wearing a different name.

**It scans exactly what a Host offers**: the game root and one level of group
below it, which is `internal/gameroot`'s boundary and the same listing the
picker builds. An archive filed deeper cannot be started from a Host, so a
collision between two of them is not a collision any save will suffer — and a
diagnostic corpus filed under the root is made of exactly those. It used to
walk the whole tree, which turned an ignored corpus of several hundred
archives into ten collision reports and a nonzero exit over a library that had
none. Point `-games` at that corpus to check it deliberately.

**A title's name is printed in the form its parser left it in.** The KTF
descriptor parser decodes its own non-UTF-8 fields, so a KTF name is already
UTF-8 by the time the report has it; LGT's `app_info` is kept as the bytes it
came as. Reading an already-decoded name as EUC-KR a second time is what
printed 영웅전설3 as 곸썒꾩꽕3 in every KTF line, and an ASCII title survives
either way, which is why it stayed invisible.

## ktfdump

`cmd/ktfdump` prepares a KTF game for a disassembler. A `client.bin` relocates
itself at startup, so the bytes in the archive are not the bytes that execute;
what a disassembler needs is the image after relocation, and a map from address
to method.

```sh
go run ./cmd/ktfdump <game.zip> -image client.bin   # the relocated image; load it at 0x100000
go run ./cmd/ktfdump <game.zip> -symbols out.txt    # every AOT method body address and its name
go run ./cmd/ktfdump <game.zip> -classes            # the registered class table
```

`-image` runs the relocation entry and nothing else. `-symbols` and `-classes`
need more, because a class is in the table only once the game has asked for it,
so they start the game and dump what it registered on the way to `startApp`.

**An older relocatable module needs no entry run for `-image`**: the loader
relocates those rather than the guest, so what it writes out is the image as it
will execute without any guest instruction having run. See [`ktf.md`](ktf.md),
"The older modules run under the platform".

**A start that fails still dumps.** That is the case the tool is most often
reached for — the title stopped before it drew anything, and the question is
which class it was in — so the failure is reported and the dump goes ahead with
the classes the title got to.

`-classes` prints each class's instance layout under its methods: `at N` for an
instance field's byte offset and `static` for a static field's value, which is
what the one word in a field record means in each case. A field a title reads
directly off an object — rather than through a getter — is a question about
that layout.

## Where the data lives

Game archives, saves and reports are described in
[`running.md`](running.md#where-the-data-lives). Nothing under `var/` is
touched by a rebuild.

## provision

```
wfeature provision <game.zip> [-save dir] [-number N] [-dry-run]
```

One local title checks a certificate a server issued to a handset, once, years
ago; the copy its archive carries belongs to whichever handset downloaded it
then. This writes one sealed for the number this platform answers with, into
that archive's own save directory, and clears the record of an earlier run
having deleted the packaged one. `docs/ktf.md` has the check it satisfies and
what this is.

It refuses an archive it does not recognise, and recognition is not a name
match: the archive has to package a certificate whose own checksum agrees with
the format, which is the only evidence available that this scheme is the one it
uses.

**Nothing in the emulator runs this.** It is a deliberate, one-off command.
