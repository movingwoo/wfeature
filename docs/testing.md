# Testing

## Running them

```sh
make test          # go test ./... and the Node tests, the usual run
make test-debug    # the same Go tests under the debug build tag
make acceptance    # every local archive probe, written to var/acceptance/<date>.md
make dist-check    # what the release archives `make dist` wrote actually carry
```

`make test` is what a change is expected to pass. The debug tag compiles the
detailed logging and diagnostics in, so `make test-debug` is what catches a
change that only builds in one profile.

The two below it need something the repository does not carry. `acceptance`
needs the ignored local corpus under `var/games` and is the subject of "One
command, one date" further down; `dist-check` needs archives, so it follows a
`make dist`.

Every push runs both of those, `go vet`, `gofmt` and `go test -race` on an
Ubuntu runner, and then starts the release server on Ubuntu, Windows and macOS
to check that it binds and serves — the one thing a single development machine
cannot answer for itself. Those live in `.github/workflows/checks.yml`, which
`verify.yml` calls on every push and `release.yml` calls before it publishes:
one definition, so a tag is gated by exactly what a push is. What CI does *not*
have is a game: no archive in this repository is real, so every probe below
runs locally and by hand. See [`running.md`](running.md), "What was verified,
and where".

## The dependencies a game's bytes reach

Every push also runs `govulncheck ./...`, which reports the known
vulnerabilities that are *reachable* from this code rather than every advisory
against a module in `go.sum`. It is in the same workflow as the tests, so a
tag is gated by it too.

**The workflows build with the newest released Go rather than the one `go.mod`
names.** The directive there is the floor a checkout has to clear; the
toolchain CI installs is a decision about what gets shipped, because these are
the runners that build the release archives. Pinned to the floor, the first run
of this check reported twenty-seven standard-library vulnerabilities — all of
them fixed in later patches of the same Go line, all of them in binaries a user
would have downloaded. That is the check working: the finding was about the
toolchain, not about this code.

The reason it is worth a step of its own is that a game archive is untrusted
input and some of what decodes it is not this project's code. The one that
found this: `golang.org/x/image/bmp` used to decode an 8-bit BMP whose pixels
name a palette entry that is not there, and then panic on the first read of
one — `index out of range [5] with length 2`, raised at `At` rather than at
the decode, so nothing between the archive and the drawing code could have
caught it by checking the error. In a browser session that panic is in the
process every other player's session is in as well. It is fixed in v0.41.0
(GO-2026-5031) and pinned here by
`TestBitmapWhosePixelsNameAMissingPaletteEntryIsRejected` in
`internal/wipic`, which passes against either possible upstream answer — a
refusal or a clamp — and fails on a decoder that hands back an image whose
pixels cannot be read.

## What a hostile archive may not do

An archive is a file somebody downloaded. Everything that reads one before a
game has proved to be a game is fuzzed, and each fuzz target holds the same two
rules: it may not panic, and what it accepts has to be usable rather than
merely non-crashing — an opener that answers no error and a nil module has
moved the crash into its caller.

| target | what it reads |
|---|---|
| `internal/platform/detect.FuzzArchiveNeverPanics` | detection, before any loader: the zip directory, the marker names, and the class-file scan that decompresses |
| `internal/platform/ktf.FuzzOpenNeverPanics` | the KTF archive and its nested JAR |
| `internal/platform/skt.FuzzOpenNeverPanics` | the SKT archive and its manifest |
| `internal/platform/lgt.FuzzOpenNeverPanics` | the LGT archive, its descriptor and its nested JAR |
| `internal/platform/lgt.FuzzParseModuleNeverPanics` | the ELF, whose section headers are offsets and sizes a crafted file points anywhere |
| `internal/platform/lgt.FuzzParseDescriptorNeverPanics` | `app_info`, parsed before anything has said it is text |
| `internal/jvm/classfile.FuzzParseNeverPanics` | a class file |

```sh
go test ./internal/platform/lgt -run FuzzOpen                      # the seeds, in the usual run
go test ./internal/platform/lgt -fuzz FuzzOpenNeverPanics -fuzztime 60s
```

The seeds matter more than the time. A fuzzer starting from noise spends its
whole budget discovering that a zip begins with `PK`, so each target seeds one
archive of every shape its loader has a branch for — a descriptor with no JAR,
a JAR with no module, a wrapping folder, a zip of zips — and starts inside the
parser.

**Every loader bounds four things**, because bounding one is not bounding any:
the input it will open at all, how many entries a zip may declare, how large
one entry may be, and what the whole archive expands to. A per-entry limit
alone lets a zip declare eight thousand entries just under it. The LGT loader
had only the per-entry limit until this was written down; it takes its bounds
as a value now (`archiveLimits`) so `TestAnArchiveIsBoundedFourWays` can reach
the far side of each one without building half a gigabyte to get there.

## Driving a real title, and reading what it says

A local archive is the only thing that finds most defects here, so the CLI
probes are part of testing rather than beside it. `docs/cli.md` has the flags;
what follows is how to read a run.

**Play, capture, look.** `runktf`/`runlgt`/`runskt` take `-key tick:name` for a
key script and `-framedir` for a frame per tick, and `contactsheet` folds those
into one image. That is what "the title reaches its world" is decided on.
`framestats` is the same question asked without a person: it counts a frame's
colours and its lit pixels and exits nonzero when every frame is one colour,
which is what a sweep needs to stop passing a screen with nothing on it. Two
habits are worth keeping:

- **A stuck screen is not a defect until a key has been pressed at it.** One
  title was written down as "needs an authentication server" for several
  sessions; its dialog simply wanted its confirm key, and behind it the game
  runs to its ending credits.
- **A dialog's default answer is not the only answer.** Another was filed as a
  wall for the same reason one step further in: its certificate dialog offers
  two buttons, the *declining* one starts selected, and pressing confirm on it
  closes the title — which reads exactly like a title that cannot get past its
  own gate. One press of the left key moves the selection, and the other answer
  reaches an error the title handles and a menu behind it. Press what moves a
  selection before pressing what takes it, and check that the highlight moved.
- **Run every archive twice over one save.** A sweep that starts from an empty
  directory is measuring first runs only, and titles of this era install
  themselves on the first run: the screen you get is the notice, and the game is
  what the second run does. Two rounds over one save directory is what found a
  guest ending inside `startApp` being reported as a start failure
  ([`cli.md`](cli.md)), and three titles whose first run is a notice and whose
  second is the game ([`ktf.md`](ktf.md), [`lgt.md`](lgt.md)).

  **What the whole round costs and what it caught**: 261 KTF archives run twice
  over one save directory at `-play -speed 8 -ticks 600`, about forty minutes
  six at a time. One row failed and it is an archive rather than a title — a
  RAR named `.zip`, which the first round refuses too. Two titles exit on their
  first round and run their six hundred ticks on the second, which is the
  installer shape this habit is for. Everything else is identical between the
  rounds. **It is deliberately not part of the standing sweep** — forty minutes
  and the machine to itself is too much to pay every round for what it now
  finds. Run it when a change touches anything a save is read by, and after a
  release-blocking regression that only one run could not explain.
- **A title nobody has driven deeply is a to-do, not a limitation.** Three
  sibling titles once sat at the shallowest depth on a list of how far each had
  been taken, which looked like one shared platform gap worth
  investigating — and it was worth
  investigating, but the answer was that nobody had pressed confirm enough
  times. All three reach a play screen with nine presses over three thousand
  ticks and produce no `tick_error`, no `stub table`, no `not implemented`.
  Read a shallow title as untested rather than as evidence of anything, and
  **chase a cluster of siblings at the same depth regardless of which way you
  expect it to go**: either it is one defect explaining several of them, which
  is the cheapest kind of defect to find, or it clears several at once. Both
  outcomes are worth the pass.
- **The frame usually names the key, in a corner.** These titles render their
  own soft-key hints: `BACK:CLR` on a slot screen, `#:SKIP` over an opening,
  `END` and `NEXT` on a dialogue box. One pass swept a confirm key twenty times
  at a load screen and tried three others, while the screen said `BACK:CLR` in
  its bottom-right corner the whole time. Read the corners of the frame you are
  stuck on before sweeping anything, and before asking for the archive's help
  text — it is the same answer, already rendered.
- **A skip prompt means the opening has not ended.** A frame can show a level,
  a character and scenery and still be a cutscene: if `#:SKIP` is on it, the
  game is telling you it is still playing something *at* you. A depth of
  "reached the play screen" needs a frame with no skip prompt on it; anything
  else has got as far as the opening and no further.
- **The prompt going away is not the opening ending.** The absence of a skip
  hint is weak evidence: one title drops its `CLR:SKIP` corner partway through a
  long multi-character cutscene and keeps playing the same dialogue at the
  player for thousands more ticks. Only the positive evidence promotes a row —
  a HUD, a health bar, an item row, a menu, or a direction key that visibly
  moves something. Seventeen confirm presses that only advance dialogue are a
  cutscene being read, not a play screen being reached, and the honest row is
  the opening depth.
- **Read the game's own help text before sweeping keys.** Many archives ship
  their key map as a resource, and pulling it out is a static read that needs no
  run at all — it names the save key, the menu key and the shortcuts directly.
  Sweeping keys blind to find a save menu is the expensive half of a pass, and
  it fails outright on the titles whose save is a key rather than a menu entry.
  `docs/lgt.md`, "The title's own help screen is the key map", has the file
  names and the container format. Ask Dev for the extract: the archive read is
  on their side of the line and costs them minutes.
- **One key is not "a key has been pressed".** Some openings want a confirm on
  every screen — a character prompt, then several narration panels, one press
  each — so a script with two or three keys stops on whichever screen it runs
  out at and reports the same "held for N ticks" a real hang does. Before
  filing one, press the confirm key repeatedly through the whole run and see
  whether the screen count goes up. The cost of being wrong is asymmetric: an
  extra dozen keypresses cost nothing, and a false hang costs a Dev
  investigation into a title that was waiting for input.
- **A first run is not a run.** Titles write state on first start and behave
  differently on the second. One title's "reboot your handset" screen is its
  own first-run path, and probes that always start from an empty `-save`
  recorded it as broken. Run it twice against the same save before calling
  anything a defect.

**A corpus is scanned before it is run.** When a batch of archives arrives,
`internal/tools/apiscan` answers what they link against that this runtime does
not have: what the archive names, minus what the library declares and what the
platform registers, ranked by how many titles want each entry.

```sh
go run ./internal/tools/apiscan -games var/games/skt -natives report.json
go run ./internal/tools/apiscan -games var/games/ktf
go run ./internal/tools/apiscan -games var/games/lgt
```

Each archive is scanned as the platform it belongs to — detection is the
archive's own shape, so a mixed directory is one pass — and **what a link is
differs by vendor**, which is the whole reason the three commands read
differently:

- **SKT** titles are Java: every reference is in a constant pool, so the scan
  reads them and nothing runs. `-natives` takes any `runskt -diag` report,
  because the platform registers half its surface on a live runtime and a bare
  VM cannot see those registrations; without it the scan over-reports exactly
  that half.
- **KTF** titles are compiled: the constant pools are gone, and what is left is
  the name pool inside the client image. It names the platform *classes* a
  title could ask for — never which class a method belongs to. Nothing runs.
  See "What the corpus names, and what it never asks for" in `docs/ktf.md`.
  The vendor's **earlier package** has no client image and so no name pool at
  all, and the scan says `not scanned` for it rather than counting it as a
  title with nothing missing. There is no link to read there, which is a
  different answer from having read one and found no gaps.
- **LGT** titles carry no list at all: a module's imports exist only as the
  calls it makes while it starts, so the scan starts each one and reads back
  what it resolved. One boot per archive rather than a read. See "The imports a
  module resolves are its link map" in `docs/lgt.md`.

This is the cheap half of a compatibility pass — one pass instead of the
fix-run-fix loop a `-diag` report puts you in, and it ranks the work — and it
is only the half a *link* can answer: a name in an archive is what a title
*could* ask for, not what it does. Everything below is still how a title that
starts is made to run.

**Find the gap before reading the code.** `runktf -diag` counts what the title
asked for, and the answer is usually already in it: read the `stub table`,
`not implemented`, `throw` and `raise` lines first. One title's two-frames-per-
second problem was a `wipic stub table 5 function 0` line that had been in
every report for sessions. On LGT the equivalent is `-trace N`, and what
matters is not where it stopped but what it asked just before, and what it was
told; an `unnamed` slot in that trace is the next thing to identify.

**Rounds per entry is the pacing health number.** A healthy host retires 1.0 to
1.1 scheduler rounds per entry into the guest. Materially more means the host
is spinning rather than parking, which costs battery and frame rate without
changing what is drawn. Sweep the library and compare, rather than trusting one
title.

**What a tick costs the host is measured, not estimated.**
`internal/platform/ktf/local_perf_test.go` holds those probes and they all take
a local archive:

```sh
WFEATURE_PERF_ARCHIVE=<zip> go test -run TestPerfProbe ./internal/platform/ktf
```

`TestPerfProbe` reports deterministic throughput on a manual clock;
`TestPageSchedulingProbe` and `TestBudgetedSchedulingProbe` compare host loop
shapes; `TestSlowHostProbe` imitates an overloaded machine through
`WFEATURE_PERF_SPEED`. `TestLoadCostProbe` measures the one scene the wall
clock cannot report on — a load, where a title that sleeps through its splash
screen makes the emulator look busy for seconds while it is idle. It reports
host CPU against the guest time that CPU bought, and a ratio over 1 is a load
the user waits through. `docs/ktf.md` has what it found across the local
archives and why that closed the binary-hook question rather than opening it.

**A scene that is a burst in the middle of a route needs a third probe.**
`TestRoutedLoadProbe` measures where a route *ends*, and a full-screen effect
that lasts a second is over long before it starts counting.
`TestHeavySceneProbe` walks the whole replay instead, keeping what every tick
cost the host and how many instructions it retired, and reports each window
between the route's marks beside the run as a whole and the twenty costliest
ticks:

```sh
WFEATURE_PERF_ARCHIVE=<zip> WFEATURE_PERF_ROUTE=var/routes/<title>.route \
  go test -count=1 ./internal/platform/ktf -run TestHeavySceneProbe -v -timeout 30m
```

**A profile ranks addresses; a slow scene is usually a loop.** When the two
disagree — the profile says a function and the function has five callers — count
the guest steps between two closings of the same backward branch instead, over
the whole replay and **including the branches already marked refused**, and rank
loops by the total. That is a dozen lines of throwaway instrumentation in
`Engine.Run` and it is what found the ninth shape in `armcore.md`: the busiest
loop in one title was 28% of the replay and did not appear in any profile
ranking, because its cost was attributed to the function it called.

`WFEATURE_PERF_DIGESTS=<file>` writes the frame digest and the instruction
count of every tick, which is what says an engine change is invisible to the
guest: **two runs whose digests agree line for line and whose instruction counts
agree to the instruction have changed nothing the game can see**, and a `diff`
of the two files names the first tick where that stops being true. That is how
the stand-in in `armcore.md`'s "The eighth shape" was judged, and how the
accounting defect underneath it was found — the digests matched for a hundred
and thirty-seven ticks while the step counts drifted by a fixed amount per tick,
which is a shape no frame comparison would have shown.

It runs on a manual clock, so **two runs of one binary agree to the tenth of a
millisecond** and an A/B needs no repetition to be readable — which is what
makes a 3% change in one stretch of one route a number rather than a hope. Give
it `-count=1` or `go test` answers from its cache and both arms of an A/B read
the same. `WFEATURE_PERF_CPU=<file>` starts a Go CPU profile at the route's
first `mark`, so the profile covers the scene rather than the boot; read it with
the warning further down about what a macOS profile of this engine attributes to
`pthread_cond_signal`, which here was 73 to 89% of the samples and none of the
time. Raising the quantum does not fix that reading on this platform — it costs
the run 21% and changes what it executes — so the ranking to trust is the guest
profiler's.

Where the marks come from matters as much as the probe. A route written for a
report carries `shot` at the scenes it was written for; a `mark` is what resets
the guest profiler, so **turning a `shot` into a `mark` in a copy of the route
is how a stretch gets a profile of its own**. The copy belongs in a scratch
directory rather than in `var/routes/`, which is for routes that name a scene.

`internal/platform/lgt/local_perf_test.go` holds the same question for a Clet,
and it reports one number the KTF probes do not:

```sh
WFEATURE_PERF_ARCHIVE=<zip> WFEATURE_LOAD_TICKS=400 \
  go test -count=1 -run LGTLoadCost ./internal/platform/lgt -cpuprofile cpu.out
```

That probe takes three knobs the KTF ones do not: `WFEATURE_TICK_MS` sets the
tick the Host would use, `WFEATURE_PACED` runs the session the way a Host on a
real clock does rather than as fast as the machine allows — which is the
difference between measuring throughput and measuring the game's own pace — and
**`WFEATURE_PERF_ROUTE` drives a route before measuring**.

That last one is what makes the probe measure a game rather than a title
screen, and it is not a convenience. A profile of a title's first few hundred
ticks is a profile of a session waiting: the run that found where this
platform's time actually went — two thirds of it in a framebuffer copy — was
only possible once `-cpuprofile` could be pointed at a title in play
(`lgt.md`, "That profile was of the guest, and the host was doing something
else").

```sh
WFEATURE_PERF_ARCHIVE=<zip> WFEATURE_PERF_ROUTE=var/routes/<title>.route \
  WFEATURE_SAVE_ROOT=<a copy of a save> WFEATURE_LOAD_TICKS=60000 \
  go test -count=1 -run LGTLoadCost ./internal/platform/lgt -cpuprofile cpu.out -timeout 30m
```

**A route usually needs a save to go with it.** The scene worth measuring is
inside one — a field, a battle — and the same route replayed from a fresh boot
stops at the title screen having measured nothing. `WFEATURE_SAVE_ROOT` points
the session at one; **copy the directory before every run, not once for the
series**, because the run plays and a probe that writes the save it starts from
measures something different every time. The drift is silent and it does not
announce itself as drift: a series of runs on one copy went from 788 paints to
350 and from a 42ms wall gap to 331ms, because the route had begun landing
somewhere else entirely, and every number in between looked like a result. Two
runs that agree to the millisecond are what says the save held — that is the
check, not the copying. Without a save root at all the probe makes a fresh
directory, which is what a route-less throughput run wants.

The probe reports `busy` and `tick_p50/p90/p99` beside the mean, because the
mean says nothing on either platform: a title computes almost nothing on most
ticks and a whole frame on a few, so p50 is microseconds while p90 is the
frame. **p90 against the guest's own tick is the number a "this is slow" report
is about** — a title whose tick is 44ms of guest time and whose p90 is 35ms has
a quarter of a tick in hand, and one whose p90 is 41ms has none.

`WFEATURE_PROFILE=1` turns on the guest sampling profiler and prints the
ranking at the end, which is what says *which loop* the time is in
(`armcore.md`, "What a title spends its instructions on once the stand-ins are
in"). It is off by default because the stack walk costs about 5% of the run —
enough to move the percentiles the same run reports.

**With `WFEATURE_PACED` it also reports the gap between paints, on both clocks,
and that pair is the number a "it is not quite smooth" report is about.** The
wall gap is how long a player waited between frames; the guest gap is how far
the world moved while they waited. They have to be the same number. When they
are not, the emulator is either dropping the title's frames or handing it time
it did not pay for — a run that reported a 44ms wall gap against a 100ms guest
gap is what found the pacing defect in `lgt.md`, "The wall has to be charged for
the work, not for the intent". Neither percentile alone shows it: the frame rate
looked fine and the guest clock looked fine.

**`WFEATURE_GUEST_MIPS` sets the speed of the handset the platform stands in
for**, for the run only. It is how the number in `guestInstructionsPerMillisecond`
was chosen and how it is checked again; nothing in the emulator writes it.

**`TestRateSweep` asks that question of the whole local set at once**: what
frame period does each title ask for, what does it get, and does the host still
keep up.

```sh
WFEATURE_LGT_CORPUS=var/games/lgt WFEATURE_GUEST_MIPS=150000 \
  WFEATURE_SWEEP_TICKS=1500 \
  go test -v -count=1 -run RateSweep ./internal/platform/lgt -timeout 90m
```

Each row is one archive booted with a fresh save: the mode of its paint interval
and how much of the run holds it, the p50 and p90 beside it, and `ratio` — guest
time over host time, which is the headroom the title has over real time. A row
whose ratio is below 1 is a title this machine cannot run at its own speed. Read
the mode with its percentage: a title settled on one period reads 100%, and one
being held back by its own computation reads 8 to 34% with a p90 well above the
mode.

**Set the rate very high and the sweep reports what each title *asks* for**,
because computation then costs it no guest time at all. That is the control the
comparison needs — a period measured at the rate in force is the title's request
and the rate's effect mixed together. It runs `-v` or its rows are swallowed;
`go test` only shows package output when it is asked to. `WFEATURE_LGT_CORPUS`
is read from the package's directory like every other `go test` path, so it
wants an absolute one.

### `ratio` is a floor, and only a floor

**A row's `ratio` is measured at the title screen, and a title's problem is
usually not there.** The sweep boots each archive with a fresh save and presses
nothing, so what it runs is a boot, a vendor splash and whatever attract loop
the title settles into — never a scene a person plays. Read a row above 1 as
saying nothing at all about the title.

How wrong that can be is measured rather than guessed. The title this project's
throughput work was done against reads

    <a Clet>  paints=1500 mode=55ms (100%) guest=1m22.647s host=12.077s ratio=6.85

and the same title, same machine, driven by a route into its field, runs its
guest clock at **0.51x** — a tick costing 53.8ms against the 27.4ms it stands
for. **Thirteen times apart, on the row and in the scene.** The mode column
does not warn either: it reads 55ms at 100%, which is a title settled on one
period, because at a title screen it is.

So the sweep answers one question honestly and one not at all:

- **`ratio` below 1 is real.** A title that cannot keep up while it is doing
  almost nothing will not keep up later, and the mode's percentage is what says
  which kind it is — a row at 100% is uniformly slow, and one at 8 to 34% with
  a p90 well above its mode is being held back by its own computation in bursts.
- **`ratio` above 1 is not a pass.** It is "not refused at the title screen".

Over the thirty local LGT archives — twenty-three titles once patch variants are
merged — four are below 1 at boot and two more sit between 1.0 and 1.3. That is
a floor of a quarter of the library on the machine it was run on, and the true
number is unknown, because the titles that fail in a scene are the ones this
sweep cannot reach. Getting them needs `WFEATURE_PERF_ROUTE` and a route per
title, which is why the routes under `var/routes/` are worth writing.

Two rows are not measurements. An archive reporting `paints=2 (too few to
read)` over three thousand ticks is not slow, it is not running, and belongs in
a bug rather than in this table; and a ratio in the tens — one local title reads
89.88, two and a half minutes of guest time for 1.7 seconds of host — is a title
idling on a screen, not headroom.

**Do not read a macOS Go CPU profile of one of these runs without raising the
quantum first.** It attributes 65 to 88% of the run to
`runtime.pthread_cond_signal` under the `Gosched` the engine makes at the end
of every quantum, which is not work at all; raise `cletQuantum` a thousandfold
for the profiling run and the ranking becomes the interpreter's own.

That the samples are noise rather than cost is worth re-checking rather than
assuming, and it is one run: raising the quantum a thousandfold moved a Clet's
field scene from 20.08s to 19.92s, so the 59% those samples claimed is 0.8% of
real time.

**On KTF the same trick does not work, and that profiler is simply unusable
here.** A load probe of the heaviest local KTF archive puts 87% of the run in
`runtime.pthread_cond_signal` and does not show `Engine.Run` *at all*, while
retiring 2.9 billion guest instructions in 25 seconds — which is to say the
interpreter is the whole run and none of it was attributed. Raising the quantum
changes neither the ranking nor the wall clock, and neither does `GOMAXPROCS`.
Answer a KTF throughput question with an interleaved A/B of the probe's
`ns_per_step` instead, and take the host profile on a machine whose profiler
works.

### An SKT run is not a throughput measurement

A MIDlet's threads pace themselves against the wall clock, so a fixed number of
`runskt` ticks is a fixed number of seconds and a faster engine spends them
doing more work rather than finishing sooner. Wall time is therefore the pacing,
CPU time is ambiguous — a slower engine burns less of it because it gets less
done — and **allocation totals rise when the engine improves**. Judge a change
to this execution core with `go test -bench Guest ./internal/jvm`, where the
work is fixed, and read a run only for whether its outcome changed. The whole of
that argument, with the numbers, is in [`jvm.md`](jvm.md), "Why this could not
be measured end to end".

### A probe does not measure the binary that ships

**`go test` builds the package's test binary, and a test binary has no
`default.pgo`.** The committed profile sits beside `cmd/cli` and `cmd/server`
and is picked up because those are main packages; nothing carries it into
`internal/platform/lgt.test`. So every number these probes report is a *no-PGO*
number, and the shipped binary is 8% faster than the probe says on a Clet's
field scene and 7 to 9% faster on a KTF load.

That does not make the probes wrong — an A/B where both sides are built the same
way answers the question it is asked — but two things follow. A change is worth
re-checking through `build/release/wfeature`, which is the thing a player runs.
And a claim *about* the profile has to be measured either there or with
`go test -pgo=<file>`, which applies one to the whole build; asked without it,
"no profile, a freshly taken one, the committed one" answered 5.13, 5.14 and
5.15 nanoseconds a step, which is three ways of saying none of them was ever in
force.

**`arm_share` says whether the change under test can move this title at all.**
Both load probes report it beside `ns_per_step`, because the engine has two
halves with separate decode caches and separate routed switches, and a title
runs almost wholly in one of them: the local library is Clets at 0.0–2.8% ARM
and one title at 100%. A Thumb change measured on the ARM title, or the other
way round, measures the probe's noise floor. Pick the guard the same way — the
regression risk of an ARM change is on the titles with the *fewest* ARM steps,
and they are the ones with the most steps overall.

`decode_cache_tables` and `decode_cache_bytes` beside it are what those caches
cost, counted per table rather than per page: a page executed in both states
holds two. It is the first number to ask for before widening a cache entry,
because a table is committed for every page a title has ever executed from
rather than for its working set.

`ns_per_step` is what a throughput change has to move, and the loop calls
`Tick` rather than `TickFor` on purpose: `TickFor` answers how long the Host
should *wait* before the next tick, so a probe that reads it as a cost measures
its own idling. Host time per tick is not a throughput measurement either: both platforms advance the guest clock with the work the guest does, so a
change that alters how much work a tick contains moves the tick time for two
reasons at once, and the instruction count is what holds the comparison still.
`-count=1` matters here — a cached `go test` result reports the previous run's
numbers, which look exactly like a change that did nothing.

The browser's cheat path has a local probe of its own, because a scan against
a fixture proves the plumbing and not the search:

```sh
WFEATURE_CHEAT_ARCHIVE=<zip> go test -run TestLocalCheatProbe ./internal/webhost
```

It starts the real server over a temporary game root, opens the session socket,
and asks for regions and a scan. Both ends of the protocol change are exercised
by that: a build that compiles proves neither. It passes on all three platforms
— point it at an SKT archive and the regions it lists are the game's own
classes, over the synthetic space `docs/skvm.md` describes. **A probe that does not sleep for the wait it was
handed measures nothing** — spinning instead makes every entry into the guest
look free, which is the one result all of these exist to detect.

**A run under `-play` is not deterministic**, because it follows the wall
clock. Comparing two runs with `framediff` is only meaningful against a
control: run the same binary twice first and see how much differs on its own.
Two titles once looked like a 200-frame regression that way and were pure
timing noise.

**A batch is part of the wall clock too, and it moves the summary and not only
the frames.** Six runs at once starve each other, so a `-play` run's flush
count and lit-pixel count depend on what else was running: one local title
answered 2,243 lit pixels on one build and 4,024 on the other inside a
six-way sweep, and run on its own both builds answered **4,252** — the same
number, twice each. Two more titles behaved the same way. So a `-play` row that
differs in a sweep is a row to re-run alone before it is a finding, and a title
the sweep reports as drawing nothing is one of those rows: the last black
screen on this project's list turned out to be a batch that never got the
title past its loading phase.

The control has been measured across the whole local KTF set, so the number is
here rather than in the next investigation's head: the same script under the
same binary, at `-play -speed 20`, differs on **16 of 32 titles and 24 of 128
captured frames**. A frame hash that changes between two builds is therefore
not evidence on its own. What is: a route that stops where the other build's
did not, a `tick_error` that appears or goes away, and a run that ends at a
different tick *with* one of those. See `docs/ktf.md`, "An A/B sweep of the
local set is half noise".

**Do not read 24 of 128 as a per-frame rate.** It is four captured frames per
title, sparse enough that most shots miss whatever is animating. Diffing every
frame of a single continuously animating title, same binary twice, differs on
**1,216 of 2,379** — just over half. On a title that animates, the *number* of
differing frames says nothing; where the boxes are and how big they are says
everything, and a difference confined to a spinner or a sprite's idle bob is
the floor rather than a finding.

**The quietest control is the first frame, and it needs no route at all.**
Without `-play` a run steps ticks and stops at the first lit frame, so it is
deterministic in everything except the titles that are not: the same archive,
the same binary and a fresh save directory land on the same bytes. That makes a
whole-set sweep cheap — about a second an archive — and it is the right control
for a change that touches the frame path rather than the game's behaviour.

```sh
wfeature runktf <archive> -ticks 64 -frame before.png -save <fresh dir>   # each build
wfeature runktf <archive> -ticks 64 -scale 2 -frame before2x.png -save <fresh dir>
```

**The KTF floor is now zero.** It used to be one title's family, which differed
*against itself* — three runs of one binary produced three different first
frames — and that family set the floor for every comparison: a real change
under about 290 pixels was indistinguishable from the noise. The cause was the
clock. A probe runs on a `ManualClock` precisely so it repeats, and the clock
started at the wall, so every timestamp the guest read differed between runs and
a title seeding itself from the time of day went somewhere else. It starts at a
fixed date now, and a sweep over all 35 local KTF archives run twice on the same
binary comes back **byte-identical on every one**. Use a fresh save directory on
both sides or the second run starts from the first run's save.

Two things follow. A difference anywhere is now a finding, with no family to
discount — and if a floor ever reappears, measure it before reading anything
into a diff, because something has gone back to reading the wall.

**A route is the cheaper control.** `-route` waits on what the screen is doing
rather than on tick numbers, so the same script lands on the same screen under
both builds even though the ticks differ. Both `runktf` and `runlgt` take one.

**`framediff` finds the first divergence.** The screen a person points at is
not always the screen that is wrong — one investigation was saved by finding
that the reported frame was byte-identical before and after, and the real
break was three hundred ticks later.

## Compiling a Java fixture

A fixture is guest code, so it is still Java compiled with `javac`. What it
compiles *against* is no longer a directory of class files: the runtime
declares its class library in Go (see [`jvm.md`](jvm.md)), and
`internal/tools/javastub` writes that library out as Java signatures for the
compiler:

```sh
stub_dir="$(mktemp -d /tmp/wfeature-stubs.XXXXXX)"
go run ./internal/tools/javastub -out "$stub_dir/src"
(cd "$stub_dir/src" && javac -source 1.8 -target 1.8 -nowarn -d "$stub_dir/classes" \
  $(find javax com net -name '*.java'))
```

`$stub_dir/classes` is then the classpath every fixture recipe in this
repository uses — `internal/platform/skt/testdata/README.md` has one per JAR,
and [`rms.md`](rms.md), [`lcdui.md`](lcdui.md) and [`skvm.md`](skvm.md) have
the rest. The stubs carry signatures, constants and `throws` clauses and no
behavior at all; the fixture runs on the runtime.

**A fixture that names nothing from the class library needs none of that.**
`internal/jvm/testdata/CallLoop.java` is the interpreter's call path and uses
only its own methods, so it compiles against the JDK alone:

```sh
javac -source 1.8 -target 1.8 -nowarn -g:none \
  -d internal/jvm/testdata internal/jvm/testdata/CallLoop.java
```

The `java.*` classes come from the JDK's own `rt.jar` as they always did. This
runtime implements a subset of them, so a fixture can compile against a method
that is not here — which shows up when the fixture runs, as a missing method
rather than a compile error.

## Go

```sh
go test ./...
go test -tags debug ./...
go test -race ./internal/...
npm test
```

The current tests cover these boundaries:

- constant pools, methods, and `Code` attributes in Java class files
- error handling for truncated/trailing input and unsupported class versions
- JVM modified UTF-8 and surrogate pairs
- MIDlet manifest continuation lines and `MIDlet-1` entry points
- prevention of path traversal inside JARs and agreement between manifest and
  class names
- branches, switches, objects, arrays, exceptions, and monitors in
  `javac -target 1.8` fixtures
- interface dispatch from a JAR containing multiple classes
- one-time class initialization and concurrent synchronized calls
- runtime-owned Object construction, finite guest Threads, monitor services,
  UTF-16 String/StringBuffer behavior, and CLDC collection operations
- JAR resource lookup plus big-endian and modified-UTF DataInputStream reads
- a record written and read back through the `java.io.DataOutput` and
  `DataInput` interfaces, which lands on the stream that was passed rather than
  on an interface this runtime never declares
- `java.util.Timer`: a one-shot and a repeating schedule running on a thread
  beside the caller, both cancels stopping them, and a refused negative delay
- the SKVM text component interface typed into through the platform's input
  method handler, and the four-argument `XTextField` a name screen builds
- FIFO ordering, pending-event bounds, and self-reposting throughput limits in
  the backend event queue
- app properties and the start/pause/resume/destroy lifecycle in a newly authored
  `MIDlet` fixture
- MIDlet lifecycle notifications, resume requests, and error states for callback
  failures
- conditional destroy refusal, state preservation, retry, and forced destroy for
  `MIDletStateChangeException`
- paused transitions and subsequent start retries when
  `MIDletStateChangeException` occurs during initial start or resume
- forced cleanup and Destroyed transitions for `RuntimeException` failures in
  start/pause callbacks, including a failing cleanup callback
- ignored `RuntimeException` failures from direct destroy callbacks
- constructor-time `Display.getDisplay()`, stable per-MIDlet identity, delayed
  and coalesced `setCurrent()`, null handling, and pause/resume visibility
- automatic Canvas paint, framebuffer dimension queries, clipped and coalesced
  partial repaints, and synchronous `serviceRepaints` in a newly authored JAR
- Graphics clip/translation state, RGB color components, clipped oversized
  fills, extreme-coordinate lines, and rectangle outlines in the same JAR
- mutable/immutable image behavior, white initialization, PNG resource and byte
  decoding, ARGB snapshots, alpha blending, all eight region transforms,
  transformed drawing, and negative-scanlength `drawRGB` validation
- font attribute validation and metrics, text anchor errors, all string/character
  drawing entry points, clipping, and deterministic bitmap raster output
- active Canvas press, repeat, and release callback ordering, ignored paused and
  non-Canvas input, callback-triggered repaint ordering, and invalid event types
- pointer press/drag/release callbacks, including paused-state suppression and
  invalid event validation
- PWA manifest, service worker, icon, portable relative URLs, and static-server
  content types
- the server's routes against the real client files: the shell, refused
  traversals, the game listing and its Korean ordering, archive revalidation by
  `ETag`, the save API's round trip, key normalization, platform rerouting and
  refused owners, and the debug-log naming rules
- WebSocket framing with a real client and server driven over an in-memory
  transport: masked and unmasked frames at each length boundary, fragment
  reassembly, interleaved control frames, the closing handshake, and the
  malformed frames a hostile page can send — reserved bits, unmasked client
  frames, oversized control frames, invalid UTF-8, and a length claiming
  gigabytes before a byte of payload arrives
- a whole emulation session end to end without a browser: a real handshake, a
  real MIDlet started from a game root, decoded PNG frames at the right size, a
  key that reaches the game, a refused game path, a refused message, the report
  the server writes itself, and stopping a game without closing the connection
- the platform-neutral session driver: what a start, a tick, a frame and a key
  mean on a platform whose runtime owns its own surface, the pacing a platform
  without a clock has to ask for, and a closed session refusing everything
- the session client's decoding in Node: sampled sound back to floats at both
  extremes of the range, and a batch of audio events replayed onto a
  synthesiser in order
- sparse ARM guest-memory mapping, permissions, overflow and alignment checks
- bounded A32 condition/branch/arithmetic execution across multiple quanta
- A32 halfword and signed byte/halfword transfers with immediate/register
  offsets, pre/post writeback, sign extension, and odd-address halfword
  alignment
- ARM/Thumb ARMv4T unaligned word-load rotation and aligned word stores
- A32 word/byte swaps, signed/unsigned long multiply and accumulation, and
  register/immediate user-mode program-status transfers
- ARM7TDMI A32/Thumb multiple-transfer writeback ordering when the base is in
  the register list, including the A32 pipelined PC store value
- the original-style KTF Thumb SVC stub, including stack/register preservation,
  service identifiers, result registers, and ARM/Thumb interworking
- independent guest register contexts while one thread waits in a Host handler
- a nested guest function call from an SVC handler, including stack arguments
  and restoration of the suspended outer context
- bounded KTF outer ZIP/ADF/AID JAR/client.bin parsing, including malformed
  paths, duplicates, BSS suffixes, expansion limits, and legacy descriptors
- a newly authored nested KTF archive whose Thumb client entry maps zero-filled
  BSS, crosses SVC, resumes, and returns
- validated KTF executable, interface, name, required-function, and function-table
  guest pointers, including malformed and out-of-image pointer rejection
- platform-owned KTF initialization contexts, fifth stack argument handling,
  indirect Thumb callback stubs, and bounded guest-requested allocation
- bounded KTF AOT class/vtable/method/field parsing and JVM registration,
  invalid descriptor rejection, JVM-owned String/Class handle binding, member
  lookup, and nested `JavaJump1/2/3` guest method-body calls
- exact KTF AOT object headers, vtable-table indices, zeroed instance fields,
  primitive/reference array lengths and element sizes, JVM bindings, and bounds
- KTF `CallNative` normal returns through a repository-authored Thumb body,
  result-container writeback, outer-context preservation, and invalid pointer
  rejection before guest execution, plus the shared AOT-call nesting limit
- KTF `JavaThrow` traversal across nested raw handlers, typed catch selection,
  matched-chain-head update, restore-PC execution, context preservation, and
  cyclic-handler rejection
- guest-layout exception allocation paired with a pinned Go JVM object, typed
  uncaught propagation, and isolated logical-thread handler heads that survive
  suspension and are inherited by nested guest calls
- bidirectional pinned-object/address lookup, explicit runtime superclass
  `InvokeSpecial`, and rejection of ambiguous AOT object bindings
- synthetic KTF AOT constructor, instance, and static outer calls with integer
  and reference argument/result conversion
- raw runtime-Java metadata and both direct register-argument and native
  argument-container SVC proxy shapes exercised by the lifecycle path
- the WIPI event queue: queued events answered in order, an idle queue
  answering the platform redraw request, key dispatch down the card stack, a
  rejected unknown event kind, and `Session.SendKey` switching to the queue
  once the guest drives the loop
- pacing on an injected clock: a timer held until its delay elapses, a sleeping
  worker denied a slice, the speed multiplier scaling a wait and the guest
  clock by the same factor, and `SkipToNextDeadline` moving a manual clock but
  never the wall clock
- decode cache invalidation: code rewritten by a Host write and by a guest
  store both run the new instruction, a remap retires cached entries, and a
  page a mapping only partly covers is never cached and still faults on its
  unmapped tail
- the guest access caches not answering for the wrong region: an address past
  a cached mapping still faults, a cached read-write mapping does not make a
  read-only region writable, and a second page returns its own bytes
- the recognized C runtime routines against their C contracts — memset writing
  only the low byte of its value, memcpy and memset returning the destination,
  strlen stopping at the terminator and crossing its scan chunk — plus the scan
  installing a trapping stub over a match and nothing over an image without one
- `DataBase.sortRecord` calling back into a guest filter and comparator,
  `Graphics.translate` moving both drawing and the clip, `FileSystem`
  listing/renaming/name encoding, `Jlet.getAppProperty` over the descriptor,
  and lwc text components keeping their text

Two tests keep the WIPI Java surface honest without executing a game.
`TestRuntimeJavaSurfaceCoversReference` compares the registration table
against `testdata/wipi_java_surface.txt`, the reference surface extracted from
the original implementation, and requires every unimplemented entry to be
listed with its reason in `testdata/wipi_java_gaps.txt` — a new gap and a
stale gap both fail. `TestFixedValueStubInventory` does the same for methods
that answer a fixed value, against `testdata/wipi_java_stubs.txt`, because a
stub fails silently where an unimplemented method fails loudly. Regenerate the
reference from a local checkout of the original after pulling it — the
variable points at that implementation's WIPI Java class sources:

```sh
WFEATURE_WIPI_REFERENCE_SOURCE=<class source dir> go test -run TestRegenerateWIPISurfaceReference ./internal/platform/ktf
```

Real game archives are not stored in the repository and are read from
`var/games`. Reference projects' own `test_data/*.zip` files are not used as
fixtures until their redistribution status is known.

The local SKT archives are a probe rather than a fixture: they are never
copied into tracked data, and what they answer on any given day is the
acceptance report rather than a sentence here.

KTF parsing has a separate opt-in local probe. It never executes game code:

```sh
WFEATURE_KTF_ACCEPTANCE=1 go test -run TestLocalKTFArchivesParse -v ./internal/platform/ktf
```

Each archive is a subtest, and what it answers about the outer archive, the
ADF, the nested JAR and client-image detection is a row in the acceptance
report rather than a number typed here.

Executing third-party client code is a separate opt-in acceptance probe:

```sh
WFEATURE_KTF_EXECUTE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesInitialize -v ./internal/platform/ktf
```

It performs bounded self-relocation, validates the returned executable descriptor
chain, and calls the interface and WIPI initialization functions. All 33 current
clients return zero from both initializers. The slowest WIPI initializer retires
8,330,820 instructions; the probe does not start the game lifecycle.

The earlier KTF package — a `.mif` beside a raw `.mod`, with no descriptor and
no JAR — has acceptance runs of its own. They parse every local archive of that
shape, map its module, plant a trapping platform table below it, perform the
start-up handshake, run the title's start-up, and then drive 300 of the frames
the title itself asks to be called back on:

```sh
WFEATURE_KTF_NATIVE_ACCEPTANCE=1 go test -run TestLocalKTFNativePackage -v ./internal/platform/ktf
```

The run asserts what is established — the handshake completes, the title
registers a frame, every frame runs without a fault, the title draws and ends
frames, **it plays notes**, and then a **key route drives it through its own
menus into the game**, after which it has loaded five times the images it had.
It asserts nothing about the slots that are still traps, because that list is
what the run exists to produce; its table marks each slot `served` or `trap`,
and on the local title nothing on that route traps any more. Its table marks each slot `served` or
`trap`, and each row carries the link register of its first call site, which is
the module code to disassemble. `docs/ktf.md`, "An earlier KTF package", has
what the current list means. **Read its output as well as its exit status.**

What the title actually draws is a picture, and only a picture says whether it
is the right one:

```sh
WFEATURE_KTF_NATIVE_ACCEPTANCE=1 \
WFEATURE_KTF_NATIVE_SHOT=/tmp/frame.png \
WFEATURE_KTF_NATIVE_FRAMES=600 \
WFEATURE_KTF_NATIVE_ROUTE=e035:60,e035:60,e034:30,e035:120,e035:400 \
go test ./internal/platform/ktf -run TestLocalKTFNativePackageScreenshot
```

A route step is a key code in hex and how many of the title's own frames to run
after it; `docs/ktf.md` has what the codes mean.

The Host wiring has its own two runs, both opt-in on the same switch. One
drives the package through the layer every Host shares
(`internal/session`) — inspect, start, tick, take the frame, send a key — and
one drives it **through the browser's own protocol** with a real handshake,
the real archive and real frames (`internal/webhost`,
`TestSessionRunsTheEarlierKTFPackage`). A package the shared layer does not
carry is a package no Host can run, so that is where it is checked rather than
in each entry point.

The platform half has unit tests that need no local archive
(`native_platform_test.go`): they build a synthetic package around a one
instruction module and drive the handlers directly — the C library slots, the
file interface including create-on-write and read-back, the screen's colour
format, and the bitmap factory and blit including the colour that is not
drawn.

Resolving the ADF main class is a separate lifecycle probe:

```sh
WFEATURE_KTF_LIFECYCLE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesLoadMainClass -v ./internal/platform/ktf
```

All 33 current archives call the real `ExeInterface.GetClass` export and return
validated metadata for their `MClass`. This probe does not allocate the class.

The next probe allocates the paired guest/JVM object and invokes `<init>()V`:

```sh
WFEATURE_KTF_CONSTRUCT_ACCEPTANCE=1 go test -run TestLocalKTFArchivesConstructMainClass -v ./internal/platform/ktf
```

All 33 constructors now return. This probe was written as a diagnostic that
exited nonzero — eleven of the eighteen archives then present got through it,
and the rest either spun allocating across the runtime-Java bridge until the
instruction limit or stopped at an absent runtime API — and it is a pass
because the work those failures pointed at was done, not because the probe was
relaxed. It still calls no `startApp`, and a constructor returning is not a
claim that a game has started.

The two probes past it are what make that claim, one step at a time:

```sh
WFEATURE_KTF_START_ACCEPTANCE=1 go test -run TestLocalKTFArchivesStartMainClass -v ./internal/platform/ktf
WFEATURE_KTF_FRAME_ACCEPTANCE=1 go test -run TestLocalKTFArchivesRenderFirstFrame -v ./internal/platform/ktf
```

All 33 of the JAR-packaged archives start their main class, and **all 34
present a first frame with something lit in it** — the frame probe's own last
line reads `34 of 34 JAR-packaged archives (1 earlier-package archives
skipped)`.

That line used to read `33 of 35`, and neither of the two it left out was a
title that failed. Both were the probe's own doing, and both are fixed:

- the earlier native package carries no JAR and no main class, so there is
  nothing here to start. It was skipped inside the loop *after* being counted
  in the denominator, which made the ratio understate itself by one for every
  archive of that generation added. It is now counted and reported separately;
  `TestLocalKTFNativePackageRuns` is what exercises that one.
- one title reported `no frame (timers=0 flushes=0 drawn=0)` with no error
  behind it, while the same archive under `runktf` flushed a full 240x320
  screen on tick 2. What it waits for is a deadline, and the probe's
  hand-rolled service loop broke out of the first round that ran no timer, no
  thread and no paint — which is every round before the first one is due.

**The probe now starts a session and ticks it**, the way `runktf` and every
other Host does: `StartSession`, then `Session.Tick` on a manual clock jumped
to each next deadline, stopping when the game draws or when a round did nothing
with nothing left due. Nothing about the loop is the probe's own any more, so
what it answers for is the path a Host takes — a paint a round skips, a wait
the client thread declared, a guest that exits — and the whole corpus runs in
about five seconds instead of hundreds of hand-driven rounds per archive.

The ratio is still not the number that says what plays. Nothing here is: what
plays is found by driving a title with keys and looking at the frames, one
title at a time. A first frame is a first frame.

SKT has the same shape of probe, and it is the only test in that package that
runs a real title:

```sh
WFEATURE_SKT_ACCEPTANCE=1 go test -run TestLocalSKTArchivesBootAndPaint -v ./internal/platform/skt
```

Every archive under `var/games/skt` is started, ticked for 300 ticks and
required to end somewhere other than the error state with something lit in its
frame. That directory is the set the probe holds to,
not the whole local corpus: the ninety-archive set is `var/games/test_skt`, it
is swept with the CLI rather than with `go test`, and what the sweep found is
`var/games/test_skt/NOT-WORKING.md` and "Ninety archives" in
[`skvm.md`](skvm.md). A tick here is real time — a MIDlet's threads
sleep against the wall clock — so the subtests run in parallel and the whole
probe takes about half a minute. Two of the fifteen finish early and still
pass: they check a licence against the handset's subscriber number, draw the
refusal and call `System.exit`, which is a title behaving correctly on a
handset that is not the one it was bought for (`docs/skvm.md`).

**A second probe under the same variable asks the decoder the same question of
every sound**, directly rather than through a run:

```sh
WFEATURE_SKT_ACCEPTANCE=1 go test -run TestLocalSKTArchiveSoundsDecode -v ./internal/platform/skt
```

Every SMAF resource in every archive under `var/games/skt` has to decode, carry
a length, and reach a sink with something in it — 432 sounds, 243 with MIDI and
205 with sampled audio, all passing. It is separate from the boot probe because
**a sound that will not decode is invisible from a frame**: the title runs, the
screen is right, and the only difference is silence, which is exactly how this
platform went its whole life without emitting a note ([`audio.md`](audio.md)).
The same sweep over the ninety-archive diagnostic set reads 1,693 sounds with
the same result, so a refusal here is a regression rather than a gap.

LGT has one opt-in probe, and it is the only test in that package that runs a
real module:

```sh
WFEATURE_LGT_ACCEPTANCE=1 go test -run TestLocalLGTArchivesBootAndPaint -v ./internal/platform/lgt
```

Every archive under `var/games/lgt` is started, ticked, and required to present
a frame with something lit in it. The skip path is still there and still names
its reason — a Java title that
stops at something the AOT bridge does not implement yet reports
`ErrJavaAppUnsupported` — but no local archive takes it any more. It once took
three: Java titles were the platform's open boundary, and closing it is what
moved them into this probe rather than around it. See [`lgt.md`](lgt.md).

This probe exists because of what the LGT fixture cannot do. Every platform
contract that turned out to be wrong there — a resource read that answered a
length where zero meant success, a timer that took its callback from the arming
call's parameter, a graphics context this platform thought it owned — looked
correct against a fixture written to the same misunderstanding. Only a real
module disagreed. The probe is what keeps the corrected ones from drifting back.

**It stops at the title screen, and that is not where the defects are.** Two
found after it was passing — every line of a title's dialogue rendering as the
literal `%.*s`, and two titles losing their entire opening sequence to a delete
that left the file existing — booted, painted, reached the world and saved
through it. The screens were simply wrong, and nothing that runs unattended
knows what a game is supposed to show next.

So LGT's third layer is a scripted run driven by `-key`, reviewed as a contact
sheet of `-framedir` frames, diffed frame-by-frame against the build before the
change, and questioned with `-trace-live`. It is a method rather than a test
because its pass condition is a person recognising the screen. [`lgt.md`](lgt.md)
has it in full, and [`cli.md`](cli.md) has the flags.

## One command, one date

The probes above are nine `go test` runs behind eight environment variables,
which is why every count in this document used to be a sentence somebody typed
after an afternoon of running them. A sentence like that has no date on it, and
the corpus underneath it changes: "all 28 currently pass" was written when
`var/games/lgt` held 28 archives.

```sh
make acceptance                      # all three platforms
make acceptance ARGS="-platform ktf" # one of them
```

`internal/tools/acceptance` runs each probe with the variable that lets it run,
reads the results out of `go test -json` rather than out of its printed output —
which is what keeps a subtest's name and its reason together when several fail
at once — and writes `var/acceptance/<date>.md`: what was in each corpus
directory, a row per stage, and then every archive that did not pass with the
line that says why. It takes about a minute for all nine.

**The report is not committed and cannot be**: its rows are archive file names,
and those are the games' names. `var/` is ignored for that reason, so what this
document can carry is the command, and what a claim about the corpus can carry
is the report's date.

Two things follow from a report having to be readable per archive. **Each probe
is one subtest per archive**, including the three that used to loop with
`t.Errorf` and print a count at the end — a count says a number where a report
needs a name. And **the KTF frame probe fails the archive that draws nothing**
rather than logging it: it used to pass as long as *any* archive drew, so a
title that stopped painting became a line in a log nobody reads. The exit code
of `make acceptance` says whether a stage could run at all, not whether every
archive passed; a report that lists failures is a successful run of it.

## Repository-wide audit on 2026-09-01

This audit is a dated baseline, not a claim that every title plays correctly.
The ordinary Go and browser suites, the debug-profile suite, the internal race
suite, `go vet`, and a five-target `make dist VERSION=0.3.1-audit` all passed.
`govulncheck` reported no reachable vulnerability, and the direct Go modules
had no update available. The generated archives had the expected top-level
files and their checksums verified.

The opt-in local probes also passed for all 15 SKT archives and all 35 LGT
archives. The KTF frame probe rendered 43 of the 44 JAR-packaged archives and
skipped one earlier package; the remaining archive contains a malformed nested
JAR and fails before execution. **Those counts are this audit's date and no
later one** — they come from one afternoon in September 2026, and the corpus
has grown since. `make acceptance` is where a current one comes from now; see
"One command, one date" above.

The review found the following boundaries that the passing suites do not yet
cover:

- The SKT outer archive and nested JAR readers did not enforce the four
  archive limits the other two loaders do: input size, entry count, per-entry
  expansion, and total expansion. The container bounded one entry and nothing
  else; the JAR bounded one entry and the total. **Since fixed**, and the four
  bounds now read the same way in all three loaders — see
  [architecture.md](architecture.md), "What an archive may declare".
- Hosts call `os.ReadFile` before any loader sees an archive, so a loader's
  input bound is a bound on what will be parsed rather than on what the
  process allocates. The input is a file the user placed in their own game
  root, and the largest in the local library is under 4 MB. Recorded rather
  than actioned: making it a bound on allocation means every Host entry point
  measures before it reads.
- `checkgames` recursively walked below the depth the Host exposes, so the
  ignored diagnostic corpus produced ten collision reports and a nonzero exit
  over a library that has none. It also applied EUC-KR decoding to a KTF
  display name the descriptor parser had already decoded, which printed
  영웅전설3 as 곸썒꾩꽕3. **Both since fixed**: the scan and the picker now
  share one boundary in `internal/gameroot`, and each platform's name is
  printed in the form its own parser left it in. The audit did not notice the
  third defect the tool had: it checked KTF and LGT and **silently skipped
  SKT**, whose program number is the same kind of self-declared, copyable id.
  That is covered now too. See [cli.md](cli.md), "checkgames".
- The release smoke job stages a fresh directory and runs a freshly built
  executable; it did not extract and validate the archives that users
  download, so file presence, modes, line endings, embedded notices, version
  output and traversal-safe extraction were a manual release check. **Since
  fixed**: `make dist-check` reads all five archives back and the release
  workflow runs it before publishing — see [running.md](running.md), "What was
  verified, and where". Reproducibility is not addressed: the archives still
  carry builder owner/group and time metadata, so identical source does not
  produce byte-identical packages.
- The primary distribution is a PWA, but its automated acceptance stops at Go
  HTTP/WebSocket tests and Node tests of browser modules. No real browser test
  covers installation, service-worker update behavior, canvas, audio, touch
  input, reconnect, or save restoration. Still open: what a browser in CI would
  cost has to be weighed against what the manual sweep already covers.
- At the audit date, the workflows used `checkout@v4`, `setup-go@v5`,
  `setup-node@v4`, and `upload-artifact@v4`, while their official examples had
  moved to major version 7. Actions were tag-pinned rather than full-SHA-pinned,
  the verify workflow had no explicit least-privilege `permissions`, and there
  was no Dependabot configuration for Actions or Go modules. **Since fixed**,
  on 2026-09-04: the four actions are at `checkout@v7.0.1`, `setup-go@v7.0.0`,
  `setup-node@v7.0.0` and `upload-artifact@v7.0.1`, each pinned to the commit
  that tag pointed at with the version in a comment beside it; `verify.yml` and
  `checks.yml` declare `contents: read`; and `.github/dependabot.yml` groups
  weekly updates for Actions and Go modules, which is what moves a commit pin
  when it goes stale. `setup-node` is told `package-manager-cache: false`,
  because from v5 it caches by itself when a `package.json` names a package
  manager and there is no lock file here to key one on. `govulncheck` is
  deliberately still installed `@latest`: what it reports moves with its
  vulnerability database, and pinning the tool would pin the question rather
  than the answer.
- The LAN server bounds individual save and debug-log bodies, but it has no
  whole-request read deadline and no cap on simultaneously live sessions. It
  also intentionally has no authentication. The first two are operational
  limits that can be added without choosing an identity model; authentication
  remains a product decision for use beyond a trusted local network.
- The release-facing Korean text still needs a publication pass. The README's
  third-party component table omitted `golang.org/x/sys`, although it is linked
  on amd64 targets and was correctly included in the embedded notices — **since
  fixed**, and since made to fail: the bundled components are named in three
  places (the README's table, the notices' summary table, the notices' own
  sections) and `TestEveryListOfBundledComponentsNamesTheSameOnes` in
  `internal/licenses` compares all three, in both directions. Only the first
  column is compared, because what each list says about a component is written
  for its own reader. The pending and 0.3.1 changelog sections still contain
  proofreading defects; those are the author's to make.

The audit also read the constraint "use the same runtime data and save format
for both profiles" as requiring one save root, and reported the per-profile
root as a divergence from it. That is a misreading kept here so it is not made
twice: the constraint is about the format, both profiles already read the same
`var/games` and speak the same save API, and the split root is a deliberate
design with its reason written down — a debug session is where a half-finished
API is most likely to write a save the game cannot read back. See
[architecture.md](architecture.md#build-profiles).

The audit did not turn the documented semantic gaps in the JVM, widgets, or
graphics recorder into immediate work without a caller that needs them. Those
remain evidence-triggered watch items in the local plan.
