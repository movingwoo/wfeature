# Testing

## Running them

```sh
make test          # go test ./... and the Node tests, the usual run
make test-debug    # the same Go tests under the debug build tag
```

`make test` is what a change is expected to pass. The debug tag compiles the
detailed logging and diagnostics in, so `make test-debug` is what catches a
change that only builds in one profile.

Every push runs both of those, `go vet`, `gofmt` and `go test -race` on an
Ubuntu runner, and then starts the release server on Ubuntu, Windows and macOS
to check that it binds and serves — the one thing a single development machine
cannot answer for itself. Those live in `.github/workflows/checks.yml`, which
`verify.yml` calls on every push and `release.yml` calls before it publishes:
one definition, so a tag is gated by exactly what a push is. What CI does *not*
have is a game: no archive in this repository is real, so every probe below
runs locally and by hand. See [`running.md`](running.md), "What was verified,
and where".

## Driving a real title, and reading what it says

A local archive is the only thing that finds most defects here, so the CLI
probes are part of testing rather than beside it. `docs/cli.md` has the flags;
what follows is how to read a run.

**Play, capture, look.** `runktf`/`runlgt`/`runskt` take `-key tick:name` for a
key script and `-framedir` for a frame per tick, and `contactsheet` folds those
into one image. That is what "the title reaches its world" is decided on. Two
habits are worth keeping:

- **A stuck screen is not a defect until a key has been pressed at it.** One
  title sat in the support list as "needs an authentication server" for several
  sessions; its dialog simply wanted its confirm key, and behind it the game
  runs to its ending credits.
- **A shallow row in the support matrix is a to-do, not a limitation.** Three
  sibling titles once sat at the shallowest depth in that table, which looked
  like one shared platform gap worth investigating — and it was worth
  investigating, but the answer was that nobody had pressed confirm enough
  times. All three reach a play screen with nine presses over three thousand
  ticks and produce no `tick_error`, no `stub table`, no `not implemented`.
  Read the shallow rows as untested rather than as evidence of anything, and
  **chase a cluster of siblings at the same depth regardless of which way you
  expect it to go**: either it is one defect explaining several rows, which is
  the cheapest kind of defect to find, or it clears several rows at once. Both
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
  "reached the play screen" needs a frame with no skip prompt on it, or the row
  belongs at the opening depth the table already has a label for.
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
  WFEATURE_LOAD_TICKS=60000 \
  go test -count=1 -run LGTLoadCost ./internal/platform/lgt -cpuprofile cpu.out -timeout 30m
```

`ns_per_step` is what a throughput change has to move, and the loop calls
`Tick` rather than `TickFor` on purpose: `TickFor` answers how long the Host
should *wait* before the next tick, so a probe that reads it as a cost measures
its own idling. Host time per tick is not a throughput measurement either: both platforms advance the guest clock with the work the guest does, so a
change that alters how much work a tick contains moves the tick time for two
reasons at once, and the instruction count is what holds the comparison still.
`-count=1` matters here — a cached `go test` result reports the previous run's
numbers, which look exactly like a change that did nothing.

The browser's cheat path has a local probe of its own, because the packaged
fixtures are a MIDlet and a MIDlet is the one platform that has no guest memory
to scan:

```sh
WFEATURE_CHEAT_ARCHIVE=<zip> go test -run TestLocalCheatProbe ./internal/webhost
```

It starts the real server over a temporary game root, opens the session socket,
and asks for regions and a scan. Both ends of the protocol change are exercised
by that: a build that compiles proves neither. **A probe that does not sleep for the wait it was
handed measures nothing** — spinning instead makes every entry into the guest
look free, which is the one result all of these exist to detect.

**A run under `-play` is not deterministic**, because it follows the wall
clock. Comparing two runs with `framediff` is only meaningful against a
control: run the same binary twice first and see how much differs on its own.
Two titles once looked like a 200-frame regression that way and were pure
timing noise.

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

Measured across the whole local set, with a fresh save directory per run: **56
of 60 archives are byte-identical at scale 1, and 26 of 32 KTF archives at each
of hq2x, hq3x and hq4x** — `runlgt` has no `-scale`. The rest are one title's
family, and they differ *against themselves*: three runs of one binary produced
three different first frames for every one of them. So the floor in this mode
is that family and nothing else, and a difference anywhere outside it is a
finding rather than noise. Use a fresh save directory on both sides or the
second run starts from the first run's save.

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

The local SKT archive currently provides a non-committed compatibility probe:
startup reaches the unimplemented RMS `RecordStore` boundary on a guest thread,
and its foreground DRM loop reaches the configured instruction limit. It is not
used as a pass/fail repository fixture and is never copied into tracked data.

KTF parsing has a separate opt-in local probe. It never executes game code:

```sh
WFEATURE_KTF_ACCEPTANCE=1 go test -run TestLocalKTFArchivesParse -v ./internal/platform/ktf
```

All 32 ignored KTF archives currently present under `var/games/ktf` pass the
outer archive, ADF, nested JAR, and client-image detection boundary.

Executing third-party client code is a separate opt-in acceptance probe:

```sh
WFEATURE_KTF_EXECUTE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesInitialize -v ./internal/platform/ktf
```

It performs bounded self-relocation, validates the returned executable descriptor
chain, and calls the interface and WIPI initialization functions. All 32 current
clients return zero from both initializers. The slowest WIPI initializer retires
8,330,820 instructions; the probe does not start the game lifecycle.

Resolving the ADF main class is a separate lifecycle probe:

```sh
WFEATURE_KTF_LIFECYCLE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesLoadMainClass -v ./internal/platform/ktf
```

All 32 current archives call the real `ExeInterface.GetClass` export and return
validated metadata for their `MClass`. This probe does not allocate the class.

The next probe allocates the paired guest/JVM object and invokes `<init>()V`:

```sh
WFEATURE_KTF_CONSTRUCT_ACCEPTANCE=1 go test -run TestLocalKTFArchivesConstructMainClass -v ./internal/platform/ktf
```

All 32 constructors now return. This probe was written as a diagnostic that
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

All 32 archives start their main class, and all 32 present a first frame with
something lit in it.

LGT has one opt-in probe, and it is the only test in that package that runs a
real module:

```sh
WFEATURE_LGT_ACCEPTANCE=1 go test -run TestLocalLGTArchivesBootAndPaint -v ./internal/platform/lgt
```

Every archive under `var/games/lgt` is started, ticked, and required to present
a frame with something lit in it. **All 28 currently pass, and nothing skips.**
The skip path is still there and still names its reason — a Java title that
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
