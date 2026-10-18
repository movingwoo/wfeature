# Testing investigation history

This file preserves implementation investigations and dated validation records.
Statements about current behavior, missing APIs, corpus size, and planned work
apply to their original revision; later records may supersede them.
Start with [testing.md](../testing.md) for the maintained overview.
This consolidation adds no execution or acceptance evidence.

## Contents

- [Keypad grid validation (2026-10-08)](#keypad-grid-validation-2026-10-08)
- [Implementation and validation record](#implementation-testing)
- [Running them](#implementation-running-them)
- [Runtime boundary regressions](#implementation-runtime-boundary-regressions)
- [The dependencies a game's bytes reach](#implementation-the-dependencies-a-games-bytes-reach)
- [What a hostile archive may not do](#implementation-what-a-hostile-archive-may-not-do)
- [Driving a real title, and reading what it says](#implementation-driving-a-real-title-and-reading-what-it-says)
- [Compiling a Java fixture](#implementation-compiling-a-java-fixture)
- [Go](#implementation-go)
- [One command, one date](#implementation-one-command-one-date)
- [Repository-wide audit on 2026-09-01](#implementation-repository-wide-audit-on-2026-09-01)
- [Browser session retention and control](#implementation-browser-session-retention-and-control)
- [Runtime boundary validation (2026-09-17)](#runtime-runtime-boundary-follow-up-2026-09-17)
- [Implemented boundaries](#runtime-implemented-boundaries)
- [Incomplete candidates](#runtime-incomplete-candidates)
- [Save integrity](#runtime-save-integrity)
- [Intermediate-frame experiment](#runtime-intermediate-frame-experiment)
- [Fixture regeneration](#runtime-fixture-regeneration)
- [Validation](#runtime-validation)
- [Settling and key-response validation (2026-09-12)](#settling-settling-and-key-response-validation)
- [Reproduced false positive](#settling-reproduced-false-positive)
- [Three-corpus comparison](#settling-three-corpus-comparison)
- [Round-count decision](#settling-round-count-decision)
- [WebKit frame investigation (2026-09-12)](#webkit-webkit-frame-decoding-investigation)
- [Recorded failure](#webkit-recorded-failure)
- [Current phase-instrumented checks](#webkit-current-phase-instrumented-checks)
- [Decision and remaining limit](#webkit-decision-and-remaining-limit)
- [Text input exception audit (2026-09-23)](#text-input-audit-2026-09-23)

## Keypad grid validation (2026-10-08)

These observations cover the uncommitted keypad change based on
`b726341b12034180aa514ab436e9f2f2e6bc763a`, on macOS with Playwright Chromium
153.0.8010.12 and WebKit 26.6. The implementation contract is maintained in
[the web host reference](../../web/README.md#grid-keypad). Acceptance remains
open for the device and gameplay observations below.
The measurements and browser runs first describe the seven-column grid. The
fourteen-column follow-up at the end of this section has separate validation limits.

The old embedded shell's baseline measurements are retained locally in
`var/acceptance/keypad-grid-baseline/geometry.json`. Both engines measured the
same 28 px top-row button height, seven equal columns and 4 px gaps. At 390 by
844 px the old keypad occupied 296 px, including 12 px bottom clearance. Nine
rows use its 284 px content budget exactly: nine 28 px cells and eight 4 px gaps.
The first old row maps to one grid row and each old pad row maps to two.

| Viewport (px) | New board height (px) | Single cell height (px) | Game wrapper height (px) |
| --- | ---: | ---: | ---: |
| 320 by 568 | 284 | 28 | 272 |
| 390 by 844 | 284 | 28 | 520 |
| 844 by 390 | 207.89 | 19.53 | 170.08 |
| 1280 by 900 | 284 | 28 | 572 |
| 1280 by 390 | 190.30 | 17.58 | 155.69 |

Chromium and WebKit agree at the recorded precision. No tested viewport gains
vertical overflow. The smaller 320 px phone previously had a 348 px game wrapper;
its reduction to 272 px is the explicit cost of keeping all nine base-height
rows. Short landscape windows compress all rows uniformly. Additional safe-area
budget checks preserve settings and the final row without changing saved cells.

The completed embedded-grid runs are
`keypad-grid-chromium-1791388269804` and `keypad-grid-webkit-1791388270804`
(release), and `keypad-grid-chromium-1791388464527` (debug), under
`var/acceptance/`. Each retains `result.json`, rendered screenshots and server
logs. They establish migration, tap/drag selection, rectangle/L/T/ring shapes,
conflict choice, split/clear/undo, settings protection, reload, rotation, native
hit targets, internal-seam continuity, hole input, keyboard/pointer ownership,
focused activation, cancellation, direct-only checkpoint actions, failed writes
and cached-shell load with the server stopped. Chromium also exercises two
browser touch contacts. No page errors were recorded. The grid runner's save
argument was subsequently corrected to the isolated `ktf` leaf, because the
server resolves SKT/LGT saves as siblings; that corrected runner still needs a
rerun. No runtime save-layout change was made.

The Java/SGS checkpoint routes passed in
`checkpoint-skt-chromium-1791388329638` and
`checkpoint-skt-webkit-1791388330648`, retaining quicksave/load pixels, input
epochs, reconnect and ordinary-save assertions. The rapid-fire routes passed
in `rapid-fire-chromium-1791389652836` and `rapid-fire-webkit-1791389500812`.
They cover focused native Space/Enter, manual and automatic modes, shared input,
pointer release, focus loss, editor entry and session reset. Their save roots
are isolated. The Chromium route waits for observed pointer pulses instead of
assuming a fixed 250 ms wall-clock scheduling window; deterministic cadence
assertions remain in the Node suite.

The version-34-to-35 PWA route passed in
`pwa-chromium-1791388657799` and `pwa-webkit-1791388464548`. Their records identify
baseline SHA-256
`d6e21960dd1ca1c16032f8208f6f2c513635d6dcfb097377f6b339a1621dbed6`
and candidate
`e540535b2cb2537b270dc92b2d869bb3d5d86e9dbd491b570059a74bdc2b682e`.
The migrated Type2 key, legacy key/size records, uploaded archive, ordinary saves,
save export/import, touch, audio-context activation, text input and unavailable-server
shell load survived replacement. These are automation observations, not OS
installation or audible-output proof.

The earlier `pwa-chromium-1791388464539` run failed its retirement assertion:
after observing version 35, the final cache list contained only
`wfeature-shell-v34`. An identical rerun passed without a production fix.
The original worker's navigation sweep deletes every differently named shell.
A deterministic source-level reproduction starts a version-34 navigation, installs
and activates version 35, then completes the old response. Its final cache list
matches the failed run: the old worker recreates version 34 and deletes version 35.
The reproduction and operation log are retained as `cache-race.mjs` and
`cache-race.json` under `var/acceptance/keypad-grid/`. This proves that the source
permits the race; it does not trace the actual browser run's event ordering.
The sweep logic predates the grid change. Changing only the new worker's
retirement filter cannot alter already-installed version-34 code. The successful
rerun does not establish race freedom.

The subsequent repair moves version 35 to `wfeature-pwa-v35`, outside that legacy
sweep's prefix. Cleanup removes the legacy prefix and strictly older numbered
versions in the new prefix, preserving future versions and unrelated caches.
Two regression tests in `service-worker.test.mjs` fail before the repair and
pass afterward: a version-34 late navigation must leave the new page and grid
modules readable offline, and a replaced worker's delayed response must preserve
its successor's cache. The normal offline fallback and retirement tests also
pass. The two-version browser route reads the candidate name from its worker;
the grid route now does the same. The repaired binary has not yet run in a real
browser because port binding remains prohibited.

`make test` passed with 254 Node cases, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...` passed. Final model boundary
checks added migrated activation after merging, failed recovery-backup protection,
oversized-record preservation and invalid membership/action policies. The model-only
Node run passes 257 cases; the subsequent cache repair raises the passing total
to 259. Debug/release builds after the repair also pass; their source and
binary SHA-256 values are retained in
`var/acceptance/keypad-grid/final-source.json`. Those builds include the cache
repair and are not the binaries identified by the earlier
browser records.
The final pure-handler shell-serving, compression/revalidation and unknown-path
tests also pass with the sandbox active.

The local KTF archive with SHA-256
`2dab850c3aa252b26ba39dab8fdd155966db0bc8db15fac7ecf4170ab18fe000`
responded to a merged L-shaped OK button through menu and character selection.
Its `local-chromium-*` screenshots under `var/acceptance/keypad-grid/` do not
show gameplay. A route or screenshot named `ingame` is not sufficient evidence.
Finish that route through observed play, then verify a physical phone using
an L-shaped button, two fingers, rotation and reload. Neither step is complete.
The current sandbox refuses server binding with `bind: operation not permitted`,
so the previously shared preview is stopped and final embedded-browser reruns
remain pending. No real-game archives were added to tracked files.

### Horizontal refinement and available space

The follow-up uses fourteen columns and nine rows. Each former cell maps to two
horizontal cells; assigned buttons and merged shapes keep their action, activation
policy and topology, while ordinary empty cells expose independent halves.
Version 2 is saved under `wfeature:keypadGridV2`. Keeping the original
`wfeature:keypadGrid` slot intact preserves rollback data and prevents an
already-open version-1 tab from overwriting refined edits. Explicit defaults in
the new record also prevent reset from reviving old edits.

The keypad now takes the safe width of the game column, capped at 480 px, and
all height below the fitted game wrapper. Decorative side and bottom padding are
removed. Dynamic viewport height, all four safe-area insets and the desktop top
margin share one budget. The grid stays fourteen by nine on resize; cell sizes
and, in very small windows, gaps scale instead of dropping coordinates.

The complete Node suite passes 264 tests, including five new refinement cases
for shapes and activation, empty halves, denied writes, damaged sibling types,
and old-tab storage isolation. The installable-shell, compression/revalidation
and unknown-path Go handler tests pass in both debug and release profiles.
Both embedded server builds pass. Their build log contains a nonfatal module
stat-cache write warning from the sandbox; the build commands exit successfully.
Source and binary hashes, test/build logs and the browser preflight are retained
under `var/acceptance/keypad-density/`. The unchanged seven-column release server
is kept as `build/keypad-density/baseline/wfeature-server` for the update route.

The updated browser runner covers nine viewport sizes, all four safe-area inputs,
last-column hit testing, width/bottom alignment, overflow and retained storage.
It also checks migration from both old storage formats. Its JavaScript syntax
check passes, but its rendered assertions have not run. Server binding fails
with `listen EPERM: operation not permitted 127.0.0.1`, and ordinary Chromium
and WebKit launches both fail under the current sandbox. These failures occur
before any page is rendered. The earlier seven-column screenshots and PWA runs
do not validate this follow-up. Full viewport checks, version-34/35-to-36 updates,
real-game play and physical-phone observations remain pending.

### Key assignment after merging

The editor cleared its selection after a successful merge but kept multiple-selection
mode active. A later key choice only staged another merge instead of assigning the
key. The fix returns to assignment mode with the merged button selected; refused
merges retain their mode and selection. Three editor-handler regressions fail before
the fix and pass afterward, including conflicting keys and settings protection.
The full Node suite now passes 267 tests, both embedded server builds pass, and the
focused shell-handler checks pass in both profiles. The browser route includes the
interaction but remains unrun under the existing execution restriction. Shell cache
version 37 delivers the fix. Logs and hashes are retained separately under
`var/acceptance/keypad-merge-selection/`; the earlier density artifacts are unchanged.

### Single-cell label fitting

The height-based font size and wrapping label style left two-character names
large enough to wrap in a narrow cell. Labels now stay on one line, and the
editor measures their actual text width with a DOM range before shrinking the
font to the available label rectangle. It resets the font before each fit so
resize, merging and shorter text can restore the normal size. Rapid-fire mode
changes run the same fitting path.

Two editor regressions fail before the fix and pass afterward; a third checks
changing labels and repeated fitting. The font metrics are explicit DOM stubs,
not rendered-font evidence. The full Node suite passes 270 tests, both server
builds pass, and focused shell-handler checks pass in both profiles. The browser
route now checks two-character and longer single-cell labels, rapid-fire modes,
and actual text widths at every viewport, but remains unrun under the recorded
execution restriction. Shell cache version 38 carries the fix. Logs and hashes
are retained under `var/acceptance/keypad-labels/`.

### Final fourteen-column browser validation

After execution permissions changed, the embedded version-41 client passed the
rendered checks below. These runs supersede the browser-execution restrictions
recorded above; they do not establish physical-phone behavior or real-game play.

| Route | Chromium result directory | WebKit result directory |
| --- | --- | --- |
| Release grid | `keypad-grid-chromium-1791433016855` | `keypad-grid-webkit-1791433016855` |
| Debug grid | `keypad-grid-chromium-1791433068926` | Not repeated |
| Version 34 to 41 | `pwa-chromium-1791433160722` | `pwa-webkit-1791433200401` |
| Version 35 to 41 | `pwa-chromium-1791433200398` | `pwa-webkit-1791433160722` |

Each directory is under `var/acceptance/` and retains its JSON result, screenshots
and server logs. The grid runs cover all nine viewport sizes, four safe-area
inputs, unchanged coordinates, label widths, migration, editor operations,
immediate assignment after merging, input ownership, native shape hit areas,
failed storage and offline shell reload. Chromium also checks two touch contacts.
No page errors were recorded. These routes use isolated saves and authored
fixtures, so the earlier save-directory correction is now exercised.

The first retry exposed an acceptance-runner error: it tried to cycle rapid fire
before starting a game, although the existing handler requires a running game.
The check now runs after startup. The next retry found actual overflow at
240 by 320 px: start-menu controls extended the document to 346 px despite the
keypad ending at 320 px. The menu now scrolls inside the game area with safe
alignment, and the runner checks that its first and last controls remain reachable.

One Chromium upgrade run observed the legacy cache before asynchronous fetch
cleanup completed. The PWA runner now waits for the cache state after navigation
as well as network idleness; its final sole-cache assertion is unchanged. Both
baseline versions pass in both engines, preserving old layout records, archive
and save hashes, guest progress, backup restoration, text input and offline shell
availability. The current shell stays outside the legacy cleanup prefix.

`make test` (including all 270 Node tests), `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, both embedded server builds and
`git diff --check` pass. Build and test logs, source hashes and binary hashes are
retained under `var/acceptance/keypad-pr/`. Real-game gameplay and physical-phone
observations remain open in [maintenance](../maintenance.md#keypad-grid-acceptance).

<a id="implementation-testing"></a>

## Implementation and validation record

<a id="implementation-running-them"></a>

### Running them

```sh
make test          # go test ./... and the Node tests, the usual run
make test-debug    # the same Go tests under the debug build tag
make acceptance    # every local archive probe, written to var/acceptance/<date>.{md,ndjson}
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
runs locally and by hand. See [`running.md`](../running.md), "What was verified,
and where".

<a id="implementation-runtime-boundary-regressions"></a>

### Runtime boundary regressions

The authored fixtures and remaining ABI evidence for the 2026-09-17 follow-up
are recorded in [runtime boundary evidence](#runtime-runtime-boundary-follow-up-2026-09-17).
They cover thread lifecycle, archive names, UTF-16, JVM/ARM reentry, graphics and
media lifetime, save failures and intermediate PNG frames. Synthetic coverage
is separate from real-game and physical-device acceptance.

<a id="implementation-the-dependencies-a-games-bytes-reach"></a>

### The dependencies a game's bytes reach

Every push also runs `govulncheck ./...`, which reports the known
vulnerabilities *reachable* from this code rather than every advisory against a
module in `go.sum`. It is in the same workflow as the tests, so it gates a tag
too.

**The workflows build with the newest released Go rather than the one `go.mod`
names.** The directive there is the floor a checkout has to clear; the
toolchain CI installs decides what gets shipped, because these runners build
the release archives. Pinned to the floor, the first run of this check reported
twenty-seven standard-library vulnerabilities, all fixed in later patches of the
same Go line and all present in binaries a user would have downloaded. The
finding was about the toolchain, not this code.

The step exists because a game archive is untrusted input and some of what
decodes it is not this project's code. The finding that showed it:
`golang.org/x/image/bmp` decoded an 8-bit BMP whose pixels name a missing
palette entry and then panicked on the first read of one
(`index out of range [5] with length 2`), at `At` rather than at the decode, so
nothing between the archive and the drawing code could have caught it by
checking the error. In a browser session that panic is in the process every
other player's session shares. It is fixed in v0.41.0 (GO-2026-5031) and pinned
by `TestBitmapWhosePixelsNameAMissingPaletteEntryIsRejected` in
`internal/wipic`, which passes against either possible upstream answer (a
refusal or a clamp) and fails on a decoder that returns an image whose pixels
cannot be read.

<a id="implementation-what-a-hostile-archive-may-not-do"></a>

### What a hostile archive may not do

An archive is a downloaded file. Everything that reads one before a game has
proved to be a game is fuzzed, and each fuzz target holds two rules: it may not
panic, and what it accepts must be usable, not merely non-crashing (an opener
that returns no error and a nil module has moved the crash into its caller).
The maintained list of targets is in
[testing](../testing.md#fuzz-targets).

| target | what it reads |
|---|---|
| `internal/platform/detect.FuzzArchiveNeverPanics` | detection, before any loader: the zip directory, the marker names, and the class-file scan that decompresses |
| `internal/platform/ktf.FuzzOpenNeverPanics` | the KTF archive and its nested JAR |
| `internal/platform/skt.FuzzOpenNeverPanics` | the SKT archive and its manifest |
| `internal/platform/lgt.FuzzOpenNeverPanics` | the LGT archive, its descriptor and its nested JAR |
| `internal/platform/lgt.FuzzParseModuleNeverPanics` | the ELF, whose section headers are offsets and sizes a crafted file points anywhere |
| `internal/platform/lgt.FuzzParseDescriptorNeverPanics` | `app_info`, parsed before anything has said it is text |
| `internal/jvm/classfile.FuzzParseNeverPanics` | a class file |
| `internal/platform/ktf.FuzzPackagedRecordDatabaseNeverPanics` | the two shapes a KTF archive packages a record database in |
| `internal/platform/ktf.FuzzDatabaseNameSurvivesTheRemovalList` | a name a storage table takes, against the key it makes and the list it lands in |
| `internal/platform/skt.FuzzPackagedRecordStoreNeverPanics` | one `.sb` record table against the `.db` it points into |
| `internal/platform/skt.FuzzPackagedRecordStoresNeverPanics` | the whole scan of a container's stores, and what they ask for between them |
| `internal/platform/skt.FuzzRecordStoreNameSurvivesTheIndex` | a store name, against the key it makes and the index it lands in |

**The last five exist because a review found the same shape of defect nine
times.** Reading a save an archive carries added two parsers whose every field
indexes or sizes something, and a rule that a name a game chooses is also a save
key; the same question then had to be answered again at each entry point that
takes a name, on each table, on both platforms. That grid kept producing
findings: one idea applied in one place and not the next. Two of the five are
therefore **property** targets rather than parser ones, asserting what the grid
is for:

- a name a table accepts must key to a name *under its own scope* — one that
  normalises to the scope itself makes a regular file where the directory
  belongs, and every write under it fails from then on;
- it must not address one of the lists a table keeps beside its entries;
- and it must come back from that list as itself, beside another name, because
  a name that splits on the list's separator takes its neighbours with it.

Making those properties testable turned four copies of the list encoding into
one (`joinRemovalList`/`splitRemovalList`, and `joinStoreIndex`/
`splitStoreIndex` on the other platform). The copies had disagreed about
trimming, which is how deleting `"save "` came to hide `"save"`, a defect found
once per copy, one review round at a time.

```sh
go test ./internal/platform/lgt -run FuzzOpen                      # the seeds, in the usual run
go test ./internal/platform/lgt -fuzz FuzzOpenNeverPanics -fuzztime 60s
```

The seeds matter more than the time. A fuzzer starting from noise spends its
budget discovering that a zip begins with `PK`, so each target seeds one archive
of every shape its loader branches on (a descriptor with no JAR, a JAR with no
module, a wrapping folder, a zip of zips) and starts inside the parser.

**Every loader bounds four things**, because one bound alone bounds nothing:
the input it will open, how many entries a zip may declare, how large one entry
may be, and what the whole archive expands to. A per-entry limit alone lets a
zip declare eight thousand entries just under it. The LGT loader had only the
per-entry limit until this was written down; it now takes its bounds as a value
(`archiveLimits`) so `TestAnArchiveIsBoundedFourWays` can reach the far side of
each without building half a gigabyte.

<a id="implementation-driving-a-real-title-and-reading-what-it-says"></a>

### Driving a real title, and reading what it says

A local archive is the only thing that finds most defects here, so the CLI
probes are part of testing rather than beside it. `docs/cli.md` has the flags;
what follows is how to read a run.

**Play, capture, look.** `runktf`/`runlgt`/`runskt` take `-key tick:name` for a
key script and `-framedir` for a frame per tick, and `contactsheet` folds those
into one image; "the title reaches its world" is decided on that. `framestats`
asks the same question without a person: it counts a frame's colours and lit
pixels and exits nonzero when every frame is one colour, so a sweep stops
passing a screen with nothing on it. These habits are worth keeping:

- **A stuck screen is not a defect until a key has been pressed at it.** One
  title was recorded as "needs an authentication server" for several sessions;
  its dialog wanted its confirm key, and behind it the game runs to its ending
  credits.
- **A dialog's default answer is not the only answer.** Another was filed as a
  wall for the same reason one step further in: its certificate dialog offers
  two buttons, the *declining* one starts selected, and confirming it closes the
  title, which looks exactly like a title that cannot pass its own gate. One
  press of the left key moves the selection, and the other answer reaches an
  error the title handles and a menu behind it. Press what moves a selection
  before pressing what takes it, and check that the highlight moved.
- **Run every archive twice over one save.** A sweep that starts from an empty
  directory measures first runs only, and titles of this era install themselves
  on the first run: the first screen is the notice, and the game is what the
  second run does. One title's "reboot your handset" screen is its own
  first-run path, and probes that always started from an empty `-save` recorded
  it as broken. Two rounds over one save directory found a guest ending inside
  `startApp` being reported as a start failure ([`cli.md`](../cli.md)), and
  three titles whose first run is a notice and whose second is the game
  ([`ktf.md`](ktf.md#implementation-ktf-platform-implementation-status),
  [`lgt.md`](lgt.md#implementation-the-lgt-platform)).

  **Cost and yield of the full round**: 261 KTF archives run twice over one
  save directory at `-play -speed 8 -ticks 600` took about forty minutes, six
  at a time. One row failed, and it is an archive rather than a title (a RAR
  named `.zip`, which the first round refuses too). Two titles exit on their
  first round and run their six hundred ticks on the second, the installer
  shape this habit is for; everything else is identical between rounds. **It is
  deliberately not part of the standing sweep**: forty minutes with the machine
  to itself is too much to pay every round for what it now finds. Run it when a
  change touches anything a save is read by, and after a release-blocking
  regression that one run could not explain.
- **A title nobody has driven deeply is a to-do, not a limitation.** Three
  sibling titles once sat at the shallowest depth on a list of how far each had
  been taken, which looked like one shared platform gap; the cause was that
  nobody had pressed confirm enough times. All three reach a play screen with
  nine presses over three thousand ticks and produce no `tick_error`, no
  `stub table` and no `not implemented`. Read a shallow title as untested, not
  as evidence, and **chase a cluster of siblings at the same depth whichever way
  it is expected to go**: either one defect explains several of them, the
  cheapest kind to find, or the pass clears several at once.
- **The frame usually names the key, in a corner.** These titles render their
  own soft-key hints: `BACK:CLR` on a slot screen, `#:SKIP` over an opening,
  `END` and `NEXT` on a dialogue box. One pass swept a confirm key twenty times
  at a load screen and tried three others while the screen showed `BACK:CLR` in
  its bottom-right corner throughout. Read the corners of a stuck frame before
  sweeping anything or asking for the archive's help text; it is the same
  answer, already rendered.
- **A skip prompt means the opening has not ended.** A frame can show a level,
  a character and scenery and still be a cutscene: `#:SKIP` on it means the game
  is still playing something to the player. A depth of "reached the play
  screen" needs a frame with no skip prompt; anything else has reached the
  opening and no further.
- **The prompt going away is not the opening ending.** The absence of a skip
  hint is weak evidence: one title drops its `CLR:SKIP` corner partway through a
  long multi-character cutscene and keeps playing the same dialogue for
  thousands more ticks. Only positive evidence promotes a row: a HUD, a health
  bar, an item row, a menu, or a direction key that visibly moves something.
  Seventeen confirm presses that only advance dialogue are a cutscene being
  read, and the correct row is the opening depth.
- **Read the game's own help text before sweeping keys.** Many archives ship
  their key map as a resource, and extracting it is a static read that needs no
  run; it names the save key, the menu key and the shortcuts. Sweeping keys
  blind for a save menu is the expensive half of a pass, and it fails outright
  on titles whose save is a key rather than a menu entry. `docs/lgt.md`, "The
  title's own help screen is the key map", has the file names and the container
  format. Ask Dev for the extract: the archive read is on their side of the line
  and costs them minutes.
- **One key is not "a key has been pressed".** Some openings want a confirm on
  every screen (a character prompt, then several narration panels, one press
  each), so a script with two or three keys stops on whichever screen it runs
  out at and reports the same "held for N ticks" a real hang does. Before filing
  one, press the confirm key repeatedly through the whole run and see whether
  the screen count goes up. The cost of being wrong is asymmetric: an extra
  dozen key presses cost nothing, and a false hang costs a Dev investigation
  into a title that was waiting for input.

**A corpus is scanned before it is run.** When a batch of archives arrives,
`internal/tools/apiscan` answers what they link against that this runtime does
not have: what the archive names, minus what the library declares and what the
platform registers, ranked by how many titles want each entry.

```sh
go run ./internal/tools/apiscan -games var/games/skt -natives report.json
go run ./internal/tools/apiscan -games var/games/ktf
go run ./internal/tools/apiscan -games var/games/lgt
```

Each archive is scanned as the platform its own shape says it belongs to, so a
mixed directory is one pass. **What a link is differs by vendor**, which is why
the three commands differ:

- **SKT** titles are Java: every reference is in a constant pool, so the scan
  reads them and nothing runs. `-natives` takes any `runskt -diag` report,
  because the platform registers half its surface on a live runtime and a bare
  VM cannot see those registrations; without it the scan over-reports exactly
  that half.
- **KTF** titles are compiled: the constant pools are gone, and what is left is
  the name pool inside the client image. It names the platform *classes* a
  title could ask for, never which class a method belongs to. Nothing runs.
  See "What the corpus names, and what it never asks for" in `docs/ktf.md`.
  The vendor's **earlier package** has no client image and so no name pool at
  all, and the scan says `not scanned` for it rather than counting it as a
  title with nothing missing. There is no link to read there, which is a
  different answer from having read one and found no gaps.
- **LGT** titles carry no list at all: a module's imports exist only as the
  calls it makes while it starts, so the scan starts each one and reads back
  what it resolved. One boot per archive rather than a read. See "The imports a
  module resolves are its link map" in `docs/lgt.md`.

This is the cheap half of a compatibility pass: one pass instead of the
fix-run-fix loop of `-diag` reports, ranked by demand. It answers only what a
*link* can answer; a name in an archive is what a title *could* ask for, not
what it does. Everything below is how a title that starts is made to run.

**Find the gap before reading the code.** `runktf -diag` counts what the title
asked for, and the answer is usually already in it: read the `stub table`,
`not implemented`, `throw` and `raise` lines first. One title's two-frames-per-
second problem was a `wipic stub table 5 function 0` line that had been in
every report for sessions. On LGT the equivalent is `-trace N`; what matters is
what the title asked just before it stopped and what it was told, and an
`unnamed` slot in that trace is the next thing to identify.

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
clock cannot report on: a load, where a title that sleeps through its splash
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
disagree (the profile names a function and the function has five callers),
count the guest steps between two closings of the same backward branch instead,
over the whole replay and **including the branches already marked refused**,
and rank loops by the total. A dozen lines of throwaway instrumentation in
`Engine.Run` did this and found the ninth shape in `armcore.md`: the busiest
loop in one title was 28% of the replay and did not appear in any profile
ranking, because its cost was attributed to the function it called.

`WFEATURE_PERF_DIGESTS=<file>` writes the frame digest and the instruction
count of every tick, which is what says an engine change is invisible to the
guest: **two runs whose digests agree line for line and whose instruction counts
agree to the instruction have changed nothing the game can see**, and a `diff`
of the two files names the first tick where that stops being true. The
stand-in in `armcore.md`'s "The eighth shape" was judged this way, and the
accounting defect underneath it was found this way: the digests matched for a
hundred and thirty-seven ticks while the step counts drifted by a fixed amount
per tick, which no frame comparison would have shown.

It runs on a manual clock, so **two runs of one binary agree to the tenth of a
millisecond** and an A/B needs no repetition to be readable; a 3% change in one
stretch of one route is therefore measurable. Give it `-count=1`, or `go test`
answers from its cache and both arms of an A/B read the same. `WFEATURE_PERF_CPU=<file>` starts a Go CPU profile at the route's
first `mark`, so the profile covers the scene rather than the boot; read it with
the warning further down about what a macOS profile of this engine attributes to
`pthread_cond_signal`, which here was 73 to 89% of the samples and none of the
time. Raising the quantum does not fix that reading on this platform (it costs
the run 21% and changes what it executes), so the ranking to trust is the guest
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
real clock does rather than as fast as the machine allows (measuring the game's
own pace rather than throughput), and **`WFEATURE_PERF_ROUTE` drives a route
before measuring**.

The route makes the probe measure a game rather than a title screen. A profile
of a title's first few hundred ticks is a profile of a session waiting: the run
that found where this platform's time went (two thirds of it in a framebuffer
copy) was only possible once `-cpuprofile` could be pointed at a title in play
(`lgt.md`, "That profile was of the guest, and the host was doing something
else").

```sh
WFEATURE_PERF_ARCHIVE=<zip> WFEATURE_PERF_ROUTE=var/routes/<title>.route \
  WFEATURE_SAVE_ROOT=<a copy of a save> WFEATURE_LOAD_TICKS=60000 \
  go test -count=1 -run LGTLoadCost ./internal/platform/lgt -cpuprofile cpu.out -timeout 30m
```

**A route usually needs a save to go with it.** The scene worth measuring is
inside one (a field, a battle), and the same route replayed from a fresh boot
stops at the title screen having measured nothing. `WFEATURE_SAVE_ROOT` points
the session at one; **copy the directory before every run, not once for the
series**, because the run plays and a probe that writes the save it starts from
measures something different every time. The drift is silent: a series of runs
on one copy went from 788 paints to 350 and from a 42ms wall gap to 331ms,
because the route had begun landing somewhere else, and every number in between
looked like a result. Two runs that agree to the millisecond show that the save
held; that is the check, not the copying. Without a save root the probe makes a
fresh directory, which suits a route-less throughput run.

The probe reports `busy` and `tick_p50/p90/p99` beside the mean, because the
mean says nothing on either platform: a title computes almost nothing on most
ticks and a whole frame on a few, so p50 is microseconds while p90 is the
frame. **p90 against the guest's own tick is the number a "this is slow" report
is about**: a title whose tick is 44ms of guest time and whose p90 is 35ms has a
quarter of a tick in hand, and one whose p90 is 41ms has none.

`WFEATURE_PROFILE=1` turns on the guest sampling profiler and prints the
ranking at the end, which is what says *which loop* the time is in
(`armcore.md`, "What a title spends its instructions on once the stand-ins are
in"). It is off by default because the stack walk costs about 5% of the run,
enough to move the percentiles the same run reports.

**With `WFEATURE_PACED` it also reports the gap between paints on both clocks,
and that pair is the number a "not quite smooth" report is about.** The wall gap
is how long a player waited between frames; the guest gap is how far the world
moved meanwhile. They must be equal. When they are not, the emulator is either
dropping the title's frames or handing it time it did not pay for: a run that
reported a 44ms wall gap against a 100ms guest gap found the pacing defect in
`lgt.md`, "The wall has to be charged for the work, not for the intent".
Neither percentile alone shows it; the frame rate and the guest clock both
looked correct.

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
because computation then costs it no guest time. That is the control the
comparison needs, because a period measured at the rate in force mixes the
title's request with the rate's effect. Run it with `-v`, or `go test` swallows
the rows; it shows package output only when asked. `WFEATURE_LGT_CORPUS`
is read from the package's directory like every other `go test` path, so it
wants an absolute one.

<a id="implementation-ratio-is-a-floor-and-only-a-floor"></a>

#### `ratio` is a floor, and only a floor

**A row's `ratio` is measured at the title screen, and a title's problem is
usually not there.** The sweep boots each archive with a fresh save and presses
nothing, so it runs a boot, a vendor splash and whatever attract loop the title
settles into, never a scene a person plays. A row above 1 says nothing about
the title.

The size of that error was measured. The title this project's throughput work
was done against reads

    <a Clet>  paints=1500 mode=55ms (100%) guest=1m22.647s host=12.077s ratio=6.85

and the same title on the same machine, driven by a route into its field, runs
its guest clock at **0.51x**, a tick costing 53.8ms against the 27.4ms it
stands for: **thirteen times apart between the row and the scene.** The mode
column gives no warning either; it reads 55ms at 100%, a title settled on one
period, because at a title screen it is.

So the sweep answers one question and not the other:

- **`ratio` below 1 is real.** A title that cannot keep up while doing almost
  nothing will not keep up later, and the mode's percentage says which kind it
  is: a row at 100% is uniformly slow, and one at 8 to 34% with a p90 well above
  its mode is held back by its own computation in bursts.
- **`ratio` above 1 is not a pass.** It is "not refused at the title screen".

Over the thirty local LGT archives (twenty-three titles once patch variants are
merged), four are below 1 at boot and two more sit between 1.0 and 1.3: a floor
of a quarter of the library on that machine. The true number is unknown,
because the titles that fail in a scene are the ones this sweep cannot reach;
reaching them needs `WFEATURE_PERF_ROUTE` and a route per title, which is why
the routes under `var/routes/` are worth writing.

Two kinds of row are not measurements. An archive reporting `paints=2 (too few
to read)` over three thousand ticks is not running at all and belongs in a bug,
not in this table; and a ratio in the tens (one local title reads 89.88, two and
a half minutes of guest time for 1.7 seconds of host) is a title idling on a
screen, not headroom.

**Do not read a macOS Go CPU profile of one of these runs without raising the
quantum first.** It attributes 65 to 88% of the run to
`runtime.pthread_cond_signal` under the `Gosched` the engine makes at the end
of every quantum, which is not work; raise `cletQuantum` a thousandfold for the
profiling run and the ranking becomes the interpreter's own.

That the samples are noise rather than cost was checked with one run: raising
the quantum a thousandfold moved a Clet's field scene from 20.08s to 19.92s, so
the 59% those samples claimed is 0.8% of real time.

**On KTF the same method does not work, and the macOS profiler is unusable
there.** A load probe of the heaviest local KTF archive puts 87% of the run in
`runtime.pthread_cond_signal` and does not show `Engine.Run` *at all*, while
retiring 2.9 billion guest instructions in 25 seconds: the interpreter is the
whole run and none of it was attributed. Raising the quantum
changes neither the ranking nor the wall clock, and neither does `GOMAXPROCS`.
Answer a KTF throughput question with an interleaved A/B of the probe's
`ns_per_step` instead, and take the host profile on a machine whose profiler
works.

<a id="implementation-an-skt-run-is-not-a-throughput-measurement"></a>

#### An SKT run is not a throughput measurement

A MIDlet's threads pace themselves against the wall clock, so a fixed number of
`runskt` ticks is a fixed number of seconds and a faster engine spends them
doing more work rather than finishing sooner. Wall time is therefore the pacing,
CPU time is ambiguous (a slower engine burns less of it because it gets less
done), and **allocation totals rise when the engine improves**. Judge a change
to this execution core with `go test -bench Guest ./internal/jvm`, where the
work is fixed, and read a run only for whether its outcome changed. The whole of
that argument, with the numbers, is in [`jvm.md`](../jvm.md), "Why this could not
be measured end to end".

<a id="implementation-a-probe-does-not-measure-the-binary-that-ships"></a>

#### A probe does not measure the binary that ships

**`go test` builds the package's test binary, and a test binary has no
`default.pgo`.** The committed profile sits beside `cmd/cli` and `cmd/server`
and is picked up because those are main packages; nothing carries it into
`internal/platform/lgt.test`. So every number these probes report is a *no-PGO*
number, and the shipped binary is 8% faster than the probe says on a Clet's
field scene and 7 to 9% faster on a KTF load.

That does not make the probes wrong, since an A/B where both sides are built the
same way answers its question, but two things follow. A change is worth
re-checking through `build/release/wfeature`, which is the thing a player runs.
And a claim *about* the profile has to be measured either there or with
`go test -pgo=<file>`, which applies one to the whole build; without it, "no
profile, a freshly taken one, the committed one" answered 5.13, 5.14 and 5.15
nanoseconds a step, because none of them was in force.

**`arm_share` says whether the change under test can move this title at all.**
Both load probes report it beside `ns_per_step`, because the engine has two
halves with separate decode caches and separate routed switches, and a title
runs almost wholly in one of them: the local library is Clets at 0.0–2.8% ARM
and one title at 100%. A Thumb change measured on the ARM title, or the other
way round, measures the probe's noise floor. Pick the guard the same way: the
regression risk of an ARM change is on the titles with the *fewest* ARM steps,
which are the ones with the most steps overall.

`decode_cache_tables` and `decode_cache_bytes` beside it are what those caches
cost, counted per table rather than per page: a page executed in both states
holds two. It is the first number to ask for before widening a cache entry,
because a table is committed for every page a title has ever executed from
rather than for its working set.

`ns_per_step` is what a throughput change has to move, and the loop calls
`Tick` rather than `TickFor` on purpose: `TickFor` answers how long the Host
should *wait* before the next tick, so a probe that reads it as a cost measures
its own idling. Host time per tick is not a throughput measurement either:
both platforms advance the guest clock with the work the guest does, so a change
that alters how much work a tick contains moves the tick time for two reasons
at once, and the instruction count holds the comparison still. `-count=1`
matters here: a cached `go test` result reports the previous run's numbers,
which look exactly like a change that did nothing.

The browser's cheat path has a local probe of its own, because a scan against
a fixture proves the plumbing and not the search:

```sh
WFEATURE_CHEAT_ARCHIVE=<zip> go test -run TestLocalCheatProbe ./internal/webhost
```

It starts the real server over a temporary game root, opens the session socket,
and asks for regions and a scan, exercising both ends of the protocol change; a
build that compiles proves neither. It passes on all three platforms; pointed at
an SKT archive, the regions it lists are the game's own classes, over the
synthetic space `docs/skvm.md` describes.

**A probe that does not sleep for the wait it was handed measures nothing**:
spinning instead makes every entry into the guest look free, which is the one
result all of these probes exist to detect.

**A run under `-play` is not deterministic**, because it follows the wall
clock. Comparing two runs with `framediff` is only meaningful against a
control: run the same binary twice first and see how much differs on its own.
Two titles once looked like a 200-frame regression that way and were timing
noise.

**A batch is part of the wall clock too, and it moves the summary and not only
the frames.** Six runs at once starve each other, so a `-play` run's flush
count and lit-pixel count depend on what else was running: one local title
answered 2,243 lit pixels on one build and 4,024 on the other inside a
six-way sweep, and run alone both builds answered **4,252**, twice each. Two
more titles behaved the same way. So a `-play` row that differs in a sweep is a
row to re-run alone before it is a finding, and a title the sweep reports as
drawing nothing is one of those rows: the last black screen on this project's
list was a batch that never got the title past its loading phase.

The control was measured across the whole local KTF set: the same script under
the same binary, at `-play -speed 20`, differs on **16 of 32 titles and 24 of
128 captured frames**. A frame hash that changes between two builds is
therefore not evidence on its own. Evidence is a route that stops where the
other build's did not, a `tick_error` that appears or goes away, or a run that
ends at a different tick *with* one of those. See `docs/ktf.md`, "An A/B sweep of the
local set is half noise".

**Do not read 24 of 128 as a per-frame rate.** It is four captured frames per
title, sparse enough that most shots miss whatever is animating. Diffing every
frame of a single continuously animating title, same binary twice, differs on
**1,216 of 2,379**, just over half. On a title that animates, the *number* of
differing frames says nothing; where the boxes are and how big they are is what
matters, and a difference confined to a spinner or a sprite's idle bob is the
floor rather than a finding.

**The quietest control is the first frame, and it needs no route at all.**
Without `-play` a run steps ticks and stops at the first lit frame, so it is
deterministic in everything except the titles that are not: the same archive,
the same binary and a fresh save directory land on the same bytes. That makes a
whole-set sweep cheap (about a second an archive) and the right control for a
change that touches the frame path rather than the game's behaviour.

```sh
wfeature runktf <archive> -ticks 64 -frame before.png -save <fresh dir>   # each build
wfeature runktf <archive> -ticks 64 -scale 2 -frame before2x.png -save <fresh dir>
```

**The KTF floor is now zero.** It used to be one title's family, which differed
*against itself* (three runs of one binary produced three different first
frames) and set the floor for every comparison: a real change under about 290
pixels was indistinguishable from the noise. The cause was the clock. A probe
runs on a `ManualClock` so that it repeats, but the clock started at the wall,
so every timestamp the guest read differed between runs and a title seeding
itself from the time of day diverged. It starts at a fixed date now, and a sweep
over all 35 local KTF archives run twice on the same binary comes back
**byte-identical on every one**. Use a fresh save directory on both sides, or
the second run starts from the first run's save.

A difference anywhere is therefore a finding, with no family to discount; if a
floor reappears, measure it before reading anything into a diff, because
something has gone back to reading the wall.

**A route is the cheaper control.** `-route` waits on what the screen is doing
rather than on tick numbers, so the same script lands on the same screen under
both builds even though the ticks differ. Both `runktf` and `runlgt` take one.

**`framediff` finds the first divergence.** The screen a person points at is
not always the screen that is wrong: in one investigation the reported frame
was byte-identical before and after, and the real break was three hundred ticks
later.

<a id="implementation-compiling-a-java-fixture"></a>

### Compiling a Java fixture

A fixture is guest code, so it is still Java compiled with `javac`. What it
compiles *against* is no longer a directory of class files: the runtime
declares its class library in Go (see [`jvm.md`](../jvm.md)), and
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
and [`rms.md`](../rms.md), [`lcdui.md`](../lcdui.md) and [`skvm.md`](skt.md#implementation-skvm-and-the-skt-platform) have
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

<a id="implementation-go"></a>

### Go

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
- the handset facts describing one handset: a fixture puts the subscriber
  number back together from `m.CARRIER` and `MIN` the way the local titles do,
  over one number per network prefix
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
- page-owned music and effects volume restoration, including zero volume and
  denied browser storage, plus the stable loopback origin both native apps use
  so all browser settings survive a cold restart
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

Two tests check the WIPI Java surface without executing a game.
`TestRuntimeJavaSurfaceCoversReference` compares the registration table
against `testdata/wipi_java_surface.txt`, the reference surface extracted from
the original implementation, and requires every unimplemented entry to be
listed with its reason in `testdata/wipi_java_gaps.txt`; a new gap and a
stale gap both fail. `TestFixedValueStubInventory` does the same for methods
that answer a fixed value, against `testdata/wipi_java_stubs.txt`, because a
stub fails silently where an unimplemented method fails loudly. Regenerate the
reference from a local checkout of the original after pulling it; the variable
points at that implementation's WIPI Java class sources:

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

The earlier KTF package (a `.mif` beside a raw `.mod`, with no descriptor and
no JAR) has acceptance runs of its own. They parse every local archive of that
shape, map its module, plant a trapping platform table below it, perform the
start-up handshake, run the title's start-up, and then drive 300 of the frames
the title itself asks to be called back on:

```sh
WFEATURE_KTF_NATIVE_ACCEPTANCE=1 go test -run TestLocalKTFNativePackage -v ./internal/platform/ktf
```

The run asserts what is established: the handshake completes, the title
registers a frame, every frame runs without a fault, the title draws and ends
frames, **it plays notes**, and a **key route drives it through its own menus
into the game**, after which it has loaded five times the images it had. It
asserts nothing about the slots that are still traps, because that list is what
the run exists to produce. Its table marks each slot `served` or `trap`, and
each row carries the link register of its first call site, which is the module
code to disassemble; on the local title nothing on that route traps any more. `docs/ktf.md`, "An earlier KTF package", has
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
drives the package through the layer every Host shares (`internal/session`:
inspect, start, tick, take the frame, send a key), and one drives it
**through the browser's own protocol** with a real handshake,
the real archive and real frames (`internal/webhost`,
`TestSessionRunsTheEarlierKTFPackage`). A package the shared layer does not
carry is a package no Host can run, so that is where it is checked rather than
in each entry point.

The platform half has unit tests that need no local archive
(`native_platform_test.go`): they build a synthetic package around a
one-instruction module and drive the handlers directly: the C library slots,
the file interface including create-on-write and read-back, the screen's colour
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
exited nonzero (eleven of the eighteen archives then present got through it;
the rest either spun allocating across the runtime-Java bridge until the
instruction limit or stopped at an absent runtime API), and it passes because
the work those failures pointed at was done, not because the probe was
relaxed. It still calls no `startApp`, and a constructor returning is not a
claim that a game has started.

The two probes past it are what make that claim, one step at a time:

```sh
WFEATURE_KTF_START_ACCEPTANCE=1 go test -run TestLocalKTFArchivesStartMainClass -v ./internal/platform/ktf
WFEATURE_KTF_FRAME_ACCEPTANCE=1 go test -run TestLocalKTFArchivesRenderFirstFrame -v ./internal/platform/ktf
```

All 33 of the JAR-packaged archives start their main class, and **all 34
present a first frame with something lit in it**; the frame probe's last line
reads `34 of 34 JAR-packaged archives (1 earlier-package archives skipped)`.

That line used to read `33 of 35`, and neither of the two it left out was a
failing title. Both were the probe's own defects, and both are fixed:

- the earlier native package carries no JAR and no main class, so there is
  nothing here to start. It was skipped inside the loop *after* being counted
  in the denominator, which made the ratio understate itself by one for every
  archive of that generation added. It is now counted and reported separately;
  `TestLocalKTFNativePackageRuns` exercises it.
- one title reported `no frame (timers=0 flushes=0 drawn=0)` with no error
  behind it, while the same archive under `runktf` flushed a full 240x320
  screen on tick 2. It waits for a deadline, and the probe's hand-rolled
  service loop broke out of the first round that ran no timer, no thread and no
  paint, which is every round before the first one is due.

**The probe now starts a session and ticks it**, the way `runktf` and every
other Host does: `StartSession`, then `Session.Tick` on a manual clock jumped
to each next deadline, stopping when the game draws or when a round did nothing
with nothing left due. Nothing about the loop is the probe's own any more, so
it answers for the path a Host takes (a paint a round skips, a wait the client
thread declared, a guest that exits), and the whole corpus runs in about five
seconds instead of hundreds of hand-driven rounds per archive.

**A ladder has to be monotone**, and this one was not. The four rungs between
"the archive parses" and "the title paints" walk a client by hand (run its
entry, initialise it, load the main class, construct it, start it), and they
only ever walked the current generation's route. The earlier relocatable module
does not take that route: its entry is handed the platform's callback table and
publishes its own classes, where the current generation relocates itself,
returns an executable descriptor and meets a platform in `Initialize`. So an
archive of the earlier shape was refused at rungs two to five with a read of
unmapped guest memory, and then passed at six, seven and eight, which go
through `StartSession` and take the right branch. The record said `grade:
interactive` and `stopped: initialize` about the same file: one row
contradicting itself. The hand-walked rungs now make the
same branch `StartSession` makes, so they measure that generation instead of
reporting it as a fault; rung two asks it only what it can answer, since it has
no executable descriptor to read.

The ratio still does not say what plays, and nothing here does: what plays is
found by driving a title with keys and looking at the frames, one title at a
time. A first frame establishes only a first frame.

<a id="implementation-a-probes-ceiling-is-the-sessions-not-a-second-one-beside-it"></a>

#### A probe's ceiling is the session's, not a second one beside it

Every rung gives the client an ARM instruction budget, which used to be the
probe's own number: fifty million, half of what `sessionDefaultMaxSteps` grants
a real session. Two local archives spend more than that inside a single native
call in `startApp`, decompressing what they load (one needs between 91 and 92
million instructions, the other between 62 and 63), so the probe refused both
at `start` and again at `frame`, naming the ceiling in the reason. The same two archives ran three
thousand ticks under `runktf` with no error, because a real session had granted
them twice what the probe did.

**A rung that fails an archive the emulator would have run is measuring
itself**, and a report that says so twice makes it look like two defects. The
constant is now taken rather than restated
(`localStartAcceptanceMaxSteps = sessionDefaultMaxSteps`), so there is no second
number to drift or be lowered on its own; whatever raises the session's ceiling
raises the probe's, and `session.go` is the one place the reasoning lives.
`WFEATURE_KTF_MAX_STEPS` still widens it beyond that for an investigation.

<a id="implementation-two-rungs-above-a-first-frame"></a>

#### Two rungs above a first frame

Two of the questions a first frame leaves open can be asked without a person
watching:

```sh
WFEATURE_KTF_SUSTAINED_ACCEPTANCE=1   go test -run TestLocalKTFArchivesSustainAFrame -v ./internal/platform/ktf
WFEATURE_KTF_INTERACTIVE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesAnswerAKey -v ./internal/platform/ktf
WFEATURE_LGT_SUSTAINED_ACCEPTANCE=1   go test -run TestLocalLGTArchivesSustainAFrame -v ./internal/platform/lgt
WFEATURE_LGT_INTERACTIVE_ACCEPTANCE=1 go test -run TestLocalLGTArchivesAnswerAKey -v ./internal/platform/lgt
WFEATURE_SKT_SUSTAINED_ACCEPTANCE=1   go test -run TestLocalSKTArchivesSustainAFrame -v ./internal/platform/skt
WFEATURE_SKT_INTERACTIVE_ACCEPTANCE=1 go test -run TestLocalSKTArchivesAnswerAKey -v ./internal/platform/skt
```

Each platform's pair takes `WFEATURE_<platform>_SUSTAIN_TICKS` and
`WFEATURE_<platform>_HOLD_TICKS`, which widen the two windows for the
investigation that follows a rung nobody expected an archive to miss.

**sustained** is the title still running a window past its first frame, with no
error and without ending itself. It is not the same claim as "it ticked a
thousand times without failing", which is true of a loop that has stopped
doing anything and was true of titles this project later found frozen. A title
that ends itself during the window fails the rung with that reason, which made
visible the two SKT titles that paint their opening screen and destroy
themselves immediately; the boot rung passes both, because painting and then
ending is still painting.

**interactive** is a key changing what the title draws. The judgment is:

1. reach a first frame;
2. wait for the screen to settle;
3. press a key and **hold it**, then release it and keep ticking;
4. compare what is on the screen with what the settled screen was showing.

Step 2 is what makes step 4 mean anything: a change measured against a screen
that was already animating would have happened anyway, so a title that is still
changing at the end of the settle window is reported as *unanswerable* rather
than as a failure. Step 3's hold matters as much: **a press of a single tick is
missed**, because a title of this era reads the pad on its own schedule rather
than when the event arrives. Several keys are tried in turn and
the one that moved the screen is logged, because that is the key a route for
that archive has to start with.

**A blinking prompt is a screen waiting for input.** Settling was once asked as
"the same frame eight ticks in a row", and a screen drawing a caret, an arrow
or a "press any key" line in two phases never answers that however long it is
waited for. In a sweep of 457 archives it refused the question to 46 of them
(21 KTF, 17 LGT, 8 SKT), the largest single cause left in that sweep. Counting
the distinct frames each of those 46 cycled through divided them cleanly: 31 repeat at most four frames and introduce nothing new,
with periods between 2 and 16 ticks, while 15 keep producing frames never seen
before or cycle through six to thirty-two of them. Nothing fell between four
and six.

Both decisions now observe **64 frame samples**: the initial frame and 63
subsequent ticks. A screen settles if all samples are identical, or if it cycles
through at most four distinct frames with the same frame set in each 32-sample
half. The old eight-tick shortcut could mistake a held animation phase for a
still screen; the [settling follow-up](#settling-settling-and-key-response-validation) records that
regression and the corpus comparison. The settled screen leaves behind the
*set* of frames it cycles through, and step 4 asks whether the key drew
something that set never held: a blinking screen has no single frame to compare
against, since half its ticks differ from the other half whether or not a key
did anything. A screen still producing new content stays *unanswerable*; the
aim is to shrink "unknown", not to relabel it.

**A screen that a key did not move is not always a screen that answers no
key.** The rung settles a screen and presses; a title whose opening runs longer
than the press window answers nothing, because the opening is still playing. One title measured here holds its
logo for about twenty-four thousand ticks and then keeps animating, against a
press window of five keys times sixty-four ticks.

The obvious repair, widening the press window, is the wrong one. A window long
enough to outlast an opening is a window the opening's own next screen arrives
inside, and the rung credits it to the key: it would report that `fire` changed
the screen of a title that ignored `fire` and reached the end of its logo. That
is the error the settle step exists to prevent (a change measured against a
screen that was already moving), moved from before the press to after it. So
the screen is **settled again** instead: when no key moved it, the rung waits
with nothing held for the screen to leave the set it settled on; if it does,
the opening is still running and the new screen is settled and asked in turn. A
screen that does not move at all while nothing is held answers no key. The cost
falls where the evidence is: a title whose first key answers pays nothing.

**A second launch has to be a second run.** The rungs already give an archive
two launches because the handset's first-run notice ends the first one, and
that was recognised by the session ending, which is the only thing a rung could
see. It is not the only shape the notice takes: one title puts the notice up
and *waits* on it (it settles on a still screen and no key moves it), and
launched a second time against the same save directory it plays and `fire`
answers. Widening "it ended itself" to "it did not get past its first screen"
risks a free retry for a title that is legitimately stuck, and what bounds that
risk is that a rerun which reads the same bytes is not a second run at all: over
an unchanged save directory the guest takes the same branch on the same input.
So a title that did not end itself gets its second launch only when the first
one **wrote something for the second to read**, which is what the notice does.
The same test is why there is no third launch: a second launch over unchanged
bytes would already be a retry without new input. Both judgments live in `internal/ladder` beside the settle rule, for the
same reason it does.

**All three platforms ask it the same way, deliberately.** A grade is only worth
comparing across platforms if it means the same thing on each, so the judgment
(settle, hold, release, compare, and *unanswerable* rather than failed when the
screen never settles) lives in `internal/ladder` and the three probes call it,
rather than one design written three times against three session APIs. The
three copies were the reason to move it: one question answered in three places
drifts into three answers the first time it changes, and this one had to. Each
probe still owns what its platform is made of (how a tick is spent, how a key is
sent, what a frame is read from), and `internal/ladder` is given one number per
tick, the identity of what is on the screen. The platforms differ there: a KTF
or LGT tick moves a virtual clock and costs guest work
rather than seconds, while an SKT tick is real time because a MIDlet's threads
sleep against the wall clock, so only the SKT probe runs its archives in
parallel. LGT's key names are resolved through the same table the CLI's LGT
route path uses, so `fire` means the same to a route and to a rung. LGT titles
may end their first launch: the handset's restart notice writes a save, tells
the player to start the title again and ends, which at this rung looks exactly
like a title that died. So an LGT archive that ends itself gets the second launch the notice
asked for against the same save directory, and only an ending on that launch is
reported ([lgt.md](lgt.md#implementation-the-lgt-platform), "Two rungs above a first frame").

What the rungs answer for the local corpus today is in the acceptance report;
the shape of the answer is that KTF's ladder is now eight rungs and LGT's and
SKT's are three, and that the archives which stop below the top do so for
reasons that group into a handful of causes rather than into one per archive.

SKT has the same shape of probe, and it is the only test in that package that
runs a real title:

```sh
WFEATURE_SKT_ACCEPTANCE=1 go test -run TestLocalSKTArchivesBootAndPaint -v ./internal/platform/skt
```

Every archive under `var/games/skt` is started, ticked for 300 ticks and
required to end somewhere other than the error state with something lit in its
frame. That directory is the set the probe holds to, not the whole local corpus.
The earlier ninety-archive sweep is historical; its former `var/games/test_skt`
directory and local failure report are no longer authoritative corpus pointers.
Use dated `var/acceptance/` records and current archive hashes before comparing
membership. A tick here is real time (a MIDlet's threads sleep against the wall
clock), so the subtests run in parallel and the whole probe takes about half a
minute. Two of the fifteen finish early and still
pass: they check a licence against the handset's subscriber number, draw the
refusal and call `System.exit`, which is a title behaving correctly on a
handset that is not the one it was bought for (`docs/skvm.md`).

**A second probe under the same variable asks the decoder the same question of
every sound**, directly rather than through a run:

```sh
WFEATURE_SKT_ACCEPTANCE=1 go test -run TestLocalSKTArchiveSoundsDecode -v ./internal/platform/skt
```

Every SMAF resource in every archive under `var/games/skt` has to decode, carry
a length, and reach a sink with something in it: 432 sounds, 243 with MIDI and
205 with sampled audio, all passing. It is separate from the boot probe because
**a sound that will not decode is invisible from a frame**: the title runs, the
screen is right, and the only difference is silence, which is how this platform
went without emitting a note until then ([`audio.md`](../audio.md)).
The same sweep over the ninety-archive diagnostic set reads 1,693 sounds with
the same result, so a refusal here is a regression rather than a gap.

LGT has one opt-in probe, and it is the only test in that package that runs a
real module:

```sh
WFEATURE_LGT_ACCEPTANCE=1 go test -run TestLocalLGTArchivesBootAndPaint -v ./internal/platform/lgt
```

Every archive under `var/games/lgt` is started, ticked, and required to present
a frame with something lit in it. The skip path still exists and names its
reason (a Java title that stops at something the AOT bridge does not implement
reports `ErrJavaAppUnsupported`), but no local archive takes it any more. It
once took three: Java titles were the platform's open boundary, and closing it
moved them into this probe. See [`lgt.md`](lgt.md#implementation-the-lgt-platform).

This probe exists because of what the LGT fixture cannot do. Every platform
contract found wrong there (a resource read that answered a length where zero
meant success, a timer that took its callback from the arming call's parameter,
a graphics context this platform thought it owned) looked correct against a
fixture written to the same misunderstanding; only a real module disagreed. The
probe keeps the corrected contracts from drifting back.

**It stops at the title screen, and that is not where the defects are.** Two
defects found after it was passing (every line of a title's dialogue rendering
as the literal `%.*s`, and two titles losing their entire opening sequence to a
delete that left the file existing) booted, painted, reached the world and saved
through it. The screens were wrong, and nothing that runs unattended knows what
a game should show next.

So LGT's third layer is a scripted run driven by `-key`, reviewed as a contact
sheet of `-framedir` frames, diffed frame-by-frame against the build before the
change, and questioned with `-trace-live`. It is a method rather than a test
because its pass condition is a person recognising the screen. [`lgt.md`](lgt.md#implementation-the-lgt-platform)
has it in full, and [`cli.md`](../cli.md) has the flags.

<a id="implementation-one-command-one-date"></a>

### One command, one date

The probes above are fifteen `go test` runs behind fourteen environment
variables, so every count in this document used to be a sentence typed after an
afternoon of running them. Such a sentence has no date, and the corpus under it
changes: "all 28 currently pass" was written when `var/games/lgt` held 28
archives.

```sh
make acceptance                      # all three platforms
make acceptance ARGS="-platform ktf" # one of them
```

`internal/tools/acceptance` runs each probe with the variable that enables it,
reads the results from `go test -json` rather than from printed output (which
keeps a subtest's name and its reason together when several fail at once), and
writes `var/acceptance/<date>.md`: what was in each corpus
directory, a row per stage, and then every archive that did not pass with the
line that says why.

**Neither file it writes is committed and neither can be**: their rows are
archive file names, and those are the games' names. `var/` is ignored for that
reason, so what this document can carry is the command, and what a claim about
the corpus can carry is the report's date.

<a id="implementation-one-archive-one-line"></a>

#### One archive, one line

The report is prose, and two prose reports cannot be subtracted: they differ in
wording everywhere the corpus is unchanged, and the one archive that lost a rung
is a line somewhere in the middle. So the same run also writes
`var/acceptance/<date>.ndjson`: one JSON object per line, a run line and then
one line per file in every corpus directory, **including the files no probe
picked up**, because a download that did not finish is invisible in a pass count
and a file no loader claimed is invisible in a failure count.

Every line begins with a `schema` number, and a file written by a newer tool is
refused rather than half-read. Two rows are the same archive only when they are
the same file in the same corpus, which is why the corpus is on every line: a
run may sweep several group directories with one platform's ladder, and lining
those up by platform alone would report one file's grade as another's
regression. An archive line carries:

| field | what it is |
|---|---|
| `schema`, `kind`, `run` | the version, `archive`, and the run's timestamp |
| `corpus`, `platform`, `archive` | what the row is filed under, whose ladder was climbed, and the file name. For the three platform folders the first two are the same word; for a swept tree the corpus is the group directory with its platform beside it, and the platform is empty for a file nothing claimed |
| `sha256`, `size` | the bytes that were graded, so a replaced file is not read as a regression |
| `detected`, `detect_reason`, `detect_error` | what detection made of the file, and why nothing claimed it |
| `grade`, `rung`, `ladder` | the highest rung reached, its number, and how many rungs that platform had |
| `stopped`, `stopped_outcome`, `why` | the lowest rung it did not pass, whether it was refused or declined, and the reason the probe printed |
| `why_class` | that reason with the counts and addresses taken out of it, which is what a grouping counts |
| `stages` | every stage that answered, as `pass`, `skip` or `fail` with its reason |
| `cached`, `measured_run` | whether this line was carried forward rather than measured, and the run that did measure it |

Three grades are not rungs and mean different things: `none` is a file refused
at the first rung it was asked, `skipped` is one every rung knowingly declined
(a package of a shape the ladder does not drive is not a failure), and `unrun`
is one no ladder stage answered for. A comparison does not read any of them as a
fall from another.

The rows are joined to the files through the name `go test` prints rather than
the name on disk, because a subtest name has its spaces rewritten to
underscores. Joining on the wrong one reports every archive with a space in its
name as a file no probe ever ran.

<a id="implementation-a-probes-failure-has-to-end-with-its-reason"></a>

##### A probe's failure has to end with its reason

`why` is the **last line the subtest printed**, and nothing else, because that
is where a `t.Fatalf` leaves its message and because a report needs one
sentence per archive rather than a transcript. The order of a failure's parts
therefore matters: whatever a probe's message ends with is all a person reading
the sweep sees, and all `why_class` groups on.

These probes print the runtime's diagnostic counts with a failure, dozens of
lines, and a message that ended with that block spent its one line on whichever
counter sorted last. One archive's refusal was recorded as
`1 getmethod getClipX()I`, the fortieth count line of a run whose lookups had
all succeeded, naming a member the runtime declares, while the error that ended
the run never reached the report. A sweep read that way sends the next reader
after a defect that does not exist.

So a probe puts its counts first and its reason last (`withDiagnosticCounts` in
`local_acceptance_test.go`, which the rungs in `local_ladder_test.go` also
call), and `TestDiagnosticCountsDoNotDisplaceTheReason` holds it there.
Diagnostics are still printed, because they are usually the next step; only
their position in the message changed.

<a id="implementation-what-changed-and-what-they-have-in-common"></a>

#### What changed, and what they have in common

Both are readings of that file rather than new measurements, and both are
written into the report:

- **Where they stopped, grouped by cause.** The reason a probe prints is
  written for the archive in front of it, so two archives failing the same way
  print two different lines. Grouping on `why_class` turns a list of failures
  into a count per cause, and the largest count is where the next fix is. This
  used to be done by hand after a sweep.
- **Since \<the previous run>.** Which archives got further, which got worse,
  which arrived, which left, and which are the same name over different bytes.
  A run that moved nothing says so in one line, the answer a release check
  most often needs.

```sh
make acceptance                                   # compares with the newest report already there
make acceptance ARGS="-since var/acceptance/2026-09-01.ndjson"
make acceptance ARGS="-since ''"                  # no comparison
go run ./internal/tools/acceptance -compare old.ndjson new.ndjson
```

`-compare` runs no probes: it reads two record files and writes the difference
to standard output, for a release check whose two runs have already happened.

<a id="implementation-not-measuring-again-what-has-not-changed"></a>

#### Not measuring again what has not changed

A sweep of a corpus this size is mostly spent re-answering questions whose
inputs did not move: the same bytes, driven by the same build, produce the same
answer. So a run may carry an archive's line forward from the run `-since`
names instead of measuring it again, which makes the sweep cheap enough to run
often.

Two rules keep a carried line truthful.

**The identity has to cover everything that decides the answer.** The archive's
SHA-256 is the input and the build identity is the code that reads it, so a
line is carried only when the file's bytes are the same *and* both runs have
the same build identity, operating system, architecture and Go version.

A commit alone is not that identity: it names a build only while the tree *is*
that commit, and a modified tree is the state a person is in while changing the
thing being measured, which is also when a sweep is most worth re-running. So a
modified checkout is identified by the commit plus a digest of everything
differing from it: the diff against `HEAD`, and the content of every untracked
file the ignore rules do not cover. That digest is a superset of what decides an
answer, and over-counting is the safe direction: editing a document costs a
needless sweep, whereas ignoring a change that looked unrelated would report an
answer this code never gave. A tree that cannot be described carries nothing.

The identity is read from the checkout rather than from `debug.ReadBuildInfo`,
because the command runs under `go run`, which stamps no revision and compiles
the probes from whatever is in the tree when they run. The
build stamp is the fallback for a copy of the tool built elsewhere.

The report says in a line why nothing was carried when nothing was, and
`ARGS="-cache=false"` measures everything again regardless.

**A carried line has to say that it was carried.** A record that reads like a
measurement but is a copy turns a report into a claim nobody made, so every
carried line sets `cached` and names the run that measured it in
`measured_run`. That name is carried rather than rewritten, so a line copied a
second time still points at the run that asked. The run line names the file its
carried rows came from in `cached_from`, the stage table has a **carried
forward** column beside `ran`, and a stage with nothing left to measure is
recorded as `not_run` rather than as one that answered for nobody. An archive
is carried whole rather than a rung at a time: a line whose earlier run did not
answer every stage this one intends to ask is measured again, so a line is one
thing or the other.

The answers come from the most recent run, including the current day's own
file, which the comparison deliberately skips: comparing a run with itself says
nothing, while reusing its answers is the purpose of the cache.

The selection reaches a probe as `go test`'s own subtest filter, because a
subtest here *is* an archive and the runner already runs a whole test function
at a time. A flag on the probes would duplicate `-run`; what it needed is one
guard on the reader, because a filter that matches no subtest leaves the parent
test passing with nothing under it, and reading that as "the whole corpus
passed" would invent a row from the absence of one.

Because a report must be readable per archive, **each probe is one subtest per
archive**, including the three that used to loop with `t.Errorf` and print a
count at the end (a count gives a number where a report needs a name), and
**the KTF frame probe fails the archive that draws nothing** rather than logging
it: it used to pass as long as *any* archive drew, so a title that stopped
painting became an unread log line. The exit code
of `make acceptance` says whether a stage could run at all, not whether every
archive passed; a report that lists failures is a successful run of it.

<a id="implementation-sweeping-a-tree-this-repository-does-not-keep"></a>

#### Sweeping a tree this repository does not keep

The three corpus directories above are the ones a Host reads, and a release is
deliberately checked against them alone. They are not the only archives on a
machine: investigating a platform accumulates a much larger unsorted tree, filed
under hand-named group directories, and the counts over *that* tree say which
defect is worth fixing next.

```sh
make acceptance ARGS="-games 'var/games/one,var/games/two' -exclude deliberately-broken,bundles -out var/acceptance/sweep"
go run ./internal/tools/acceptance -games /some/tree -platform ktf
```

`-games` is the same flag `checkgames` has and points the same ladders at
another root. Three design decisions follow from what a sweep is for.

**The platform is what `detect.Classify` says, not what the folder is called.**
A group directory's name is a typed note; the bytes are the evidence, and this
project has told these packages apart by content since before it had a picker.
A sweep that trusted the folder would file an archive under a ladder that cannot
load it and report that loader's refusal as a loader defect. This is also the
first caller to use `detect.Reason` for its intended purpose.

**A file no platform claimed is a record, not a discard, and the reasons are
counted apart.** Half of a sweep of an unsorted tree is about the pile nothing
ran, and that pile is its most misleading number: only one of the reasons
behind it is work this project can do. A package locked before distribution is
unreachable by any loader work; a container of another format must be unpacked
before it is a question; a zip of whole packages is its holder's choice. What
remains (a readable archive carrying no marker any platform recognises) is the
number worth acting on. The report divides them under **What nothing claimed**
and names the files behind that last count, so the next investigation starts
from a list rather than another sweep.

**It recurses, and it keeps the group.** The tree files its archives a level or
two below the root, so a sweep that stopped at the root would find nothing.
(`checkgames` stops at one level for the opposite reason: it answers what a Host
can *start*, and an archive filed deeper cannot be started. What a sweep asks,
what this build makes of these bytes, does not depend on depth.) Each group
directory becomes a corpus of its own, named for the directory *and* the
platform, and rows are filed under that name: two groups may hold the same file
name, and a row naming only the file would name both. The platform is part of the name so that a group gaining its first
archive of a second platform does not rename the rows of the first.

**Nothing is excluded by default.** A tree holds deliberately broken files, bags
of whole packages, and folders on their way out. Which are worth sweeping is
the caller's judgment on the day, not a list in this tool, so `-exclude` names
directories not to enter: by base name, to skip every folder of that name, or by
path relative to the swept root, to skip exactly one.

**Point `-out` somewhere else for a one-off sweep.** A report is one run over one
corpus, and the delta lines two of them up by corpus. A sweep of another tree
written into the usual directory would be read the next day as the whole
ordinary corpus arriving and the whole swept tree leaving, which is accurate and
useless.

<a id="implementation-how-a-probe-is-pointed-at-a-directory-it-does-not-know-about"></a>

##### How a probe is pointed at a directory it does not know about

Each probe finds its corpus from its own source location: `runtime.Caller`,
then up to the repository root, then `var/games/<platform>`. That is deliberate:
a test that took its directory from the environment could run against a corpus
nobody recorded, and the corpus a release is checked against is not a
parameter.

So a sweep does not tell the probe where to look. It changes what "up to the
repository root" reaches: a temporary module root whose every top-level entry
is a symbolic link to the checkout's, except `var`, which the sweep owns and
fills with links to the files it wants asked about. The probes compile from the
same source and read a corpus this tool assembled.

Two properties are the reason for doing it this way rather than by moving
files:

- **The real corpus is never written to.** Nothing creates, moves or deletes
  anything under the checkout; the only directory written is a temporary one,
  removed when the run ends. A sweep of a diagnostic tree cannot disturb the
  corpus a release is checked against.
- **The build identity stays the checkout's.** Only `go test`'s working
  directory moves, so the revision, the diff and the untracked files that say
  what code answered are still read from the checkout and a carried row still
  means what it says.

One shadow root is built per run and its staging directory is rewritten between
corpora, so the compiled test binaries stay cached across a sweep of a hundred
groups. The links are per file rather than per directory, because a group
directory holds several platforms' archives and a probe reads one platform's.

<a id="implementation-repository-wide-audit-on-2026-09-01"></a>

### Repository-wide audit on 2026-09-01

This audit is a dated baseline, not a claim that every title plays correctly.
The ordinary Go and browser suites, the debug-profile suite, the internal race
suite, `go vet`, and a five-target `make dist VERSION=0.3.1-audit` all passed.
`govulncheck` reported no reachable vulnerability, and the direct Go modules
had no update available. The generated archives had the expected top-level
files and their checksums verified.

The opt-in local probes also passed for all 15 SKT archives and all 35 LGT
archives. The KTF frame probe rendered 43 of the 44 JAR-packaged archives and
skipped one earlier package; the remaining archive contains a malformed nested
JAR and fails before execution. **Those counts belong to this audit's date
only**: they come from one afternoon in September 2026, and the corpus has grown
since. `make acceptance` produces current counts; see "One command, one date"
above.

The review found the following boundaries that the passing suites do not yet
cover:

- The SKT outer archive and nested JAR readers did not enforce the four
  archive limits the other two loaders do: input size, entry count, per-entry
  expansion, and total expansion. The container bounded one entry and nothing
  else; the JAR bounded one entry and the total. **Since fixed**: the four
  bounds now read the same way in all three loaders (see
  [architecture.md](../architecture.md), "What an archive may declare").
- Hosts call `os.ReadFile` before any loader sees an archive, so a loader's
  input bound is a bound on what will be parsed rather than on what the
  process allocates. The input is a file the user placed in their own game
  root, and the largest in the local library is under 4 MB. Recorded rather
  than actioned: making it a bound on allocation means every Host entry point
  measures before it reads.
- `checkgames` recursively walked below the depth the Host exposes, so the
  ignored diagnostic corpus produced ten collision reports and a nonzero exit
  over a library that has none. It also applied EUC-KR decoding to a KTF
  display name the descriptor parser had already decoded, which corrupted the
  Korean name in the report. **Both since fixed**: the scan and the picker now
  share one boundary in `internal/gameroot`, and each platform's name is
  printed in the form its own parser left it in. The audit missed the tool's
  third defect: it checked KTF and LGT and **silently skipped SKT**, whose
  program number is the same kind of self-declared, copyable id. That is
  covered now too. See [cli.md](../cli.md), "checkgames".
- The release smoke job stages a fresh directory and runs a freshly built
  executable; it did not extract and validate the archives that users
  download, so file presence, modes, line endings, embedded notices, version
  output and traversal-safe extraction were a manual release check. **Since
  fixed**: `make dist-check` reads all five archives back and the release
  workflow runs it before publishing (see [running.md](../running.md), "What was
  verified, and where"). Reproducibility is not addressed: the archives still
  carry builder owner/group and time metadata, so identical source does not
  produce byte-identical packages.
- The primary distribution is a PWA, but its automated acceptance stops at Go
  HTTP/WebSocket tests and Node tests of browser modules. No real browser test
  covers installation, service-worker update behavior, canvas, audio, touch
  input, reconnect, or save restoration. Still open: what a browser in CI would
  cost has to be weighed against what the manual sweep already covers.
  Superseded: see [PWA release acceptance](../pwa-acceptance.md).
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
- The LAN server bounds individual save and debug-log bodies, sets a ten-second
  HTTP header deadline and ten-minute HTTP read deadline, and limits retained
  running/parked games to four. The upgraded session socket clears the HTTP
  read deadline and has its own lifecycle. These controls do not add a user
  identity system; use beyond a trusted local network remains a separate
  product decision.
- The release-facing Korean text still needs a publication pass. The README's
  third-party component table omitted `golang.org/x/sys`, although it is linked
  on amd64 targets and was correctly included in the embedded notices. **Since
  fixed**, and a test now fails on it: the bundled components are named in three
  places (the README's table, the notices' summary table, the notices' own
  sections) and `TestEveryListOfBundledComponentsNamesTheSameOnes` in
  `internal/licenses` compares all three, in both directions. Only the first
  column is compared, because what each list says about a component is written
  for its own reader. The pending and 0.3.1 changelog sections still contain
  proofreading defects; those are the author's to make.

The audit also read the constraint "use the same runtime data and save format
for both profiles" as requiring one save root, and reported the per-profile
root as a divergence from it. That misreading is kept here so it is not made
twice: the constraint is about the format, both profiles already read the same
`var/games` and speak the same save API, and the split root is a deliberate,
documented design, because a debug session is where a half-finished API is most
likely to write a save the game cannot read back. See
[architecture.md](../architecture.md#build-profiles).

The audit did not turn the documented semantic gaps in the JVM, widgets, or
graphics recorder into immediate work without a caller that needs them. Those
remain evidence-triggered watch items in the local plan.

<a id="implementation-browser-session-retention-and-control"></a>

### Browser session retention and control

`internal/webhost/resume_test.go` exercises token-based starts, occupied replies,
explicit takeover, old-owner detachment, explicit parking, resuming an aged
retention record, save-claim release, and late parking during
shutdown. Save-claim and save import/export tests cover shared saves.
`startapproval_test.go` covers the four-game admission limit, preserving progress
until confirmation, rejecting forged or stale approvals, and refusing eviction
of active games. Protocol tests still accept clients that omit a browser token
or send an unacknowledged stop.

`web/session-link.test.mjs` drives two controllers against one simulated server:
only an explicit takeover moves control, the old page does not automatically
reclaim it, retries survive the former attempt limit, and returning during a
pending park waits for the park before resuming. It also covers starts finishing
while hidden, connection liveness without a close event, and a restarted server
returning the existing page to its picker. The key and touch suites verify that
leaving releases held input once.

A local Chromium and WebKit run on 2026-09-10 drove the real page and Go server
with the repository-authored SKT canvas fixture. It checked two-tab takeover,
frame continuity after input, scripted hidden/visible transitions, closing and
reopening the controlling tab, explicit stop followed by another start, and
server restart followed by automatic picker recovery. These were local browser
checks, not a new browser dependency or CI job. OS-driven phone suspension,
process eviction, and hours-long pauses in real titles still require handset
checks; scripted visibility events do not establish those behaviors.

<a id="implementation-destructive-starts-and-pause-observations-2026-09-10"></a>

#### Destructive starts and pause observations (2026-09-10)

The local Chromium and WebKit check also used separate browser contexts to start
against a retained shared-save holder. Both displayed the confirmation with
Cancel focused, preserved the original session on cancellation, and replaced it
only after acceptance. This exercised the real page, dialog and Go handler.

`internal/session/local_pause_test.go` is an opt-in observation, not a claim of
full suspension. From the repository root:

```sh
WFEATURE_PAUSE_ARCHIVE="$PWD/var/games/<archive>.zip" WFEATURE_PAUSE_DURATION=10s go test ./internal/session -run '^TestLocalPauseResumeObservation$' -v -count=1 -timeout=55s
```

It attaches no persistent save store and retains no guest audio or sample data.
It drives three pause/resume cycles, checks that a paused Host tick is refused,
and checks that ticks proceed after resumption. It reports frame changes,
platform elapsed time, audio sink calls during the pause, and audio sink calls
on the first resumed tick. Lifecycle callback errors remain visible in the log;
a passing observation does not mean those callbacks succeeded.

Five local archives (three SKT, one KTF, one LGT) completed three one-second
pauses each without a changed parked picture or additional parked frame. One
other SKT archive exited before a pause could be measured and supplies no pause
evidence. One archive per platform was then observed over three ten-second
pauses. All three retained their pictures and resumed ticking. KTF and SKT time
advanced by approximately ten seconds per pause; LGT guest time advanced by zero.
No audio calls occurred during these pauses or on their first resumed ticks, so
these scenes provide no evidence about resuming an actively playing soundtrack.

The LGT sample reported an unmapped read at address `0x1` inside `resumeClet` on
each return, then continued ticking. This remains a callback compatibility
finding, not proof that its in-game resume behavior is correct. The observation
keeps that error rather than suppressing it to obtain a clean lifecycle result.

No global clock correction or thread scheduler change follows from these
samples. SKT guest threads run independently of Host ticks, and its
`System.currentTimeMillis` and elapsed/audio time share a scaled clock. KTF also
uses its clock for elapsed time and guest date APIs. Freezing that clock globally
would change guest dates as well as timer deadlines. Separating these contracts
needs a reproducible in-game timer or soundtrack failure and a dedicated core
change. Real-phone backgrounding, long gameplay pauses, audible recovery, and
progress made by guest threads outside these sampled scenes remain unverified.

<a id="implementation-release-metadata-for-042-2026-09-10"></a>

#### Release metadata for 0.4.2 (2026-09-10)

The Makefile and package metadata agree on 0.4.2, and the release workflow's
changelog extraction returns the 0.4.2 section. The ordinary, debug, internal
race and vet gates pass. A local `make mobile-ios VERSION=0.4.2` built the IPA;
its binary plist reports 0.4.2 and the current repository commit count.

The same Go entry-point package used by iOS was also built as a macOS shared
library with `-X main.version=0.4.2`. Calling `WfeatureStart` and requesting its
real `/api/status` returned version 0.4.2 with the release profile, followed by
`WfeatureStop`. This checks the stamp through the library and embedded Host,
not execution on an iPhone. `TestEmbeddedServerReportsItsReleaseVersion` covers
both a supplied version and the unstamped `dev` fallback. No device install or
release publication was performed.

<a id="runtime-runtime-boundary-follow-up-2026-09-17"></a>

## Runtime boundary validation (2026-09-17)

This implementation follows the fourteen candidates in the local comparison
queue. The evidence below comes from repository-authored fixtures, not a new
real-game or physical-device acceptance run. Two candidates remain incomplete;
their partial changes must not be read as completion of their wider contracts.

<a id="runtime-implemented-boundaries"></a>

### Implemented boundaries

- SKT retains the first uncaught JVM background-thread error and returns it at
  the next `RunPending` boundary. Terminal transitions release guest audio waits
  and close JVM waits. `async_error_test.go` and the authored
  `async-failure.jar` exercise the session boundary.
- JVM thread state belongs to the Thread object. Only active threads and the
  main thread remain roots. Finished threads and failed starts keep restart
  prohibition without remaining roots. Untimed `Thread.join()` handles multiple
  waiters, interruption and `VM.Close`; live cooperative threads report an
  unsupported error. Close does not wait for arbitrary Host native functions.
- LGT preserves descriptor-named JAR precedence. Without a named match, multiple
  candidates require exactly one valid `binary.mod`. Descriptor and archive
  names accept UTF-8 and MS949; installed `P/` and `P/<AID>/` paths are aliases.
  JAR resources retain precedence, case-insensitive lookup is deterministic,
  and canonical duplicates and traversal are rejected. Content-based selection
  shares one expanded-byte budget across candidates and rejects malformed
  auxiliary JARs instead of repeatedly spending a fresh decompression budget.
- LGT substring, lengths, char arrays and StringBuffer mutations use UTF-16
  units, including isolated surrogates. Isolated units use an internal WTF-8
  representation; they are not replaced merely by taking a substring.
- LGT Java Player preserves repeat across pause/resume, rejects duplicate play
  and resume, and distinguishes stop from pause. Resume restarts the score from
  the beginning because the mixer has no saved playback cursor.
- KTF JVM/AOT reentry carries the current JVM Invocation through the ARM
  supervisor-call boundary. `ReentryProbe.java` and `jvm_reentry_test.go` cover
  synchronized Java → ARM → Java calls, reentry during class initialization,
  one shared instruction budget and depth cleanup. AOT depth release uses the
  worker captured at entry instead of mutable current-thread state.
- KTF File input streams share the File cursor and see subsequent writes and
  seeks. Output streams keep their existing shared behavior. Stream close is a
  no-op and does not invalidate the underlying File.
- KTF events at or above `USER_EVENT` (`0x5000`) reach listeners with their
  original type and two arguments. The specification establishes the base
  constant; the supplied compatibility proposal specifies the open-ended range.
  Unknown reserved event kinds below that boundary remain errors.
- KTF NUL padding contributes neither pixels nor advance. Partial LCD flushes
  copy into a retained Host image without modifying their source; pixels outside
  the region survive. An unchanged Card paint retains that partial image, while
  a paint that changes screen bytes presents normally. An explicit flush during
  paint is not overwritten by an extra implicit full flush. Screen snapshots
  are needed only while a partial image is retained. Forced worker slices do
  not request an automatic Host paint; explicit repaint, yield and idle paths
  continue to paint.
- KTF mutable Image surfaces have weak object ownership. Graphics retains its
  Image. Guest collection and repeated guest-address reuse reclaim orphan
  surfaces; explicit framebuffer release removes stale ownership before reuse.
  Clip state also has weak ownership: stopped orphans close, playing orphans
  remain until playback completes. Repeating orphans remain while playing.

The corresponding focused tests are `archive_selection_test.go`,
`java_utf16_test.go`, `java_pause_test.go`, `thread_join_test.go`,
`file_cursor_test.go`, `lcd_flush_test.go`, `worker_paint_test.go` and
`resource_lifetime_test.go` in their platform packages.

<a id="runtime-incomplete-candidates"></a>

### Incomplete candidates

<a id="runtime-lgt-graphics-context-abi"></a>

#### LGT graphics context ABI

Translated draw rows, overlapping self-blits and nested pixel callbacks have
authored regressions and fixes. The existing revision-scoped origin correction
remains in place. The alternate direct 52-byte context layout is still unknown:
the supplied proposal does not specify field offsets or an unambiguous layout
discriminator, and the WIPI specification treats the context as opaque. Mixed
compact/direct contexts must not be declared supported without an SDK layout,
a reference implementation or an affected caller. The callback error-path regression also confirms that the client lock is
reacquired after an undefined guest instruction.

<a id="runtime-ktf-native-abi-and-stack-lifetime"></a>

#### KTF native ABI and stack lifetime

Authored fixtures now verify fieldless intermediate payload sizes, independent
ancestor/child field addresses, canonical dispatch-alias ancestry, argument
containers across nested native calls and private helper stacks across guest
collection. These paths already satisfy those fixtures and were not rewritten.
Helper stacks and argument containers still use the existing arena lifetime;
these tests establish no premature reuse, not a new reclamation policy.

The later [KTF QA investigation](ktf.md#ktf-qa-2026-09-21-native-environment-return-and-the-slow-case) supplies an affected native caller: tag 2 at environment offset 36 publishes a 32-bit result at offset 40. That convention now has bounded call scopes, logical-thread isolation, and authored return tests. Other tags and wide environment returns remain unspecified; the older module ABI still uses its existing register-return path.

<a id="runtime-save-integrity"></a>

### Save integrity

`backend.SaveReader` and `backend.ReadSave` distinguish absence from I/O errors
when supported by the Host store. `DirectorySaveStore` implements the boundary;
the legacy `LoadSave` interface remains source compatible and cannot report
errors that a legacy Host already hides. Active KTF, LGT and SKT file/record
read paths use the error-aware boundary. Authentication views preserve it.
Record decoding rejects trailing data as well as truncation. KTF and LGT retain
read failures as terminal native-boundary errors and refuse subsequent writes;
SKT RMS and XFile report guest storage exceptions.

KTF Java files, Java record stores, WIPI-C files and WIPI-C record stores stage
mutations before publishing bytes, cursors, deletion ledgers, names or handles.
Content and ledger changes use the optional `SaveBatchStore` contract. Older
Host stores retain single-key writes and explicitly refuse multi-key operations
unless they implement that contract; they never receive a partial sequence of
those writes. The authentication view transforms a batch before committing it
and publishes its private ledger only after success.

The directory implementation writes all replacements and recovery copies first,
then commits under its store lock. A commit failure restores previous entries
and removes newly created entries. If the filesystem also refuses rollback,
the error identifies the retained recovery directory rather than deleting the
only old copies. This is failure recovery, not a crash-atomic transaction across
multiple renames. Save bytes and canonical paths are unchanged; staging files
are temporary children of the existing save root.

`save_batch_test.go` injects a failed second replacement and checks both existing
and newly created first entries; unreadable-target failures happen before any
replacement. Platform failure tests cover Java file cursors, Java and C record
mutations, C file cursors/handles, deletion, recreation, rename and authentication
ledgers. Existing recreation and persistence tests cover reopening through the
unchanged on-disk layout. The earlier native package's buffered write/flush
policy remains as previously documented; its reads now report I/O errors.
SGS-specific save work remains suspended.

<a id="runtime-intermediate-frame-experiment"></a>

### Intermediate-frame experiment

The authored callback fixture holds the execution lock across two distinct LCD
flushes. The existing `Frame()` path needs that same lock, so it cannot expose
either image before callback return. With the optional `backend.FrameSink`, both
images reach a separate queue while the callback still owns the lock.

KTF samples explicit LCD flushes no more often than once per 1/60 wall-clock
second, independently of virtual game time. The deterministic 1 ms fixture
permits 50–60 changed frames over one second; unchanged samples send nothing.
Both the converter and the receiving Host have separate pixel storage. Filling
the one-frame queue and continuing for another 100 changed flushes never waits
for the consumer. Hosts without a sink keep the original pull-only path.

`session.Options.FrameUpdates` routes these owned frames to the web Host's
existing one-frame PNG encoder queue, including during session startup. Scaling
runs on the encoder goroutine. The wire format remains ordinary PNG binary
messages; the page test receives two frames while its start request is still
pending. Parking detaches the sink before its queue closes; resuming attaches
the new queue, and a scale change updates its presentation factor. Intermediate
frames may be dropped under backpressure, and there is no timer that invents a
flush when guest code does not request one. This is synthetic evidence, not a
new real-game timing measurement or a real-browser acceptance run.

<a id="runtime-fixture-regeneration"></a>

### Fixture regeneration

Compile `internal/platform/ktf/testdata/src/ReentryProbe.java` with
`javac -source 1.8 -target 1.8 -g:none` into a temporary directory and copy only
`ReentryProbe.class` into `internal/platform/ktf/testdata/`. `ReentryBridge` is a
compile-time declaration: the test installs its ARM body itself.

The SKT fixture uses the authored MIDlet stubs and the same Java 8 target as the
other SKT JAR fixtures. Package `AsyncFailureMIDlet.class` and its two anonymous
inner classes with `testdata/ASYNC_FAILURE.MF`; do not include runtime stub
classes in the JAR.

<a id="runtime-validation"></a>

### Validation

On 2026-09-17, `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...` passed after the final runtime
changes. `git diff --check` and `gofmt` checks passed. The page's intermediate
frame test is included in the Node suite. No new real-game playthrough,
physical-device check or real-browser acceptance run is claimed.

<a id="settling-settling-and-key-response-validation"></a>

## Settling and key-response validation (2026-09-12)

<a id="settling-reproduced-false-positive"></a>

### Reproduced false positive

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

<a id="settling-three-corpus-comparison"></a>

### Three-corpus comparison

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

<a id="settling-round-count-decision"></a>

### Round-count decision

Keep `SettleRounds = 2`. Doubling it produced no outcome change in any of the
three corpora. KTF already observes spontaneous movement after its final round;
LGT and SKT currently leave before that final watch. No current result changed
because of that difference in this comparison. This records the difference
without claiming it can never matter, and does not add unmeasured rounds merely
to increase a coverage number.

<a id="webkit-webkit-frame-decoding-investigation"></a>

## WebKit frame investigation (2026-09-12)

<a id="webkit-recorded-failure"></a>

### Recorded failure

The September 11 extended browser route passed its authentication and reconnect
assertions but recorded twelve Blob access-control errors and corresponding
`createImageBitmap` warnings. The preserved evidence is identified in
[authentication case history](authentication.md#cases-existing-empty-certificate-and-automatic-defaults).
The error occurred while reading the Blob argument; that message does not prove
that PNG parsing failed or that the server emitted corrupt bytes.

An upstream [WebKit report](https://bugs.webkit.org/show_bug.cgi?id=258443)
describes similar Blob access-control errors in opaque-origin workers. This page
decodes on its normal document origin, so that report is not a diagnosis of this
project's failure. No browser workaround is justified by matching error text alone.

<a id="webkit-current-phase-instrumented-checks"></a>

### Current phase-instrumented checks

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

<a id="webkit-decision-and-remaining-limit"></a>

### Decision and remaining limit

The earlier error did not recur in these bounded current checks. Keep the decoder
unchanged: neither replacing Blob transport nor retrying every decode has a
reproduced mechanism supporting it. This does not prove that the intermittent
failure is fixed. Its original cause and precise trigger remain unresolved.

A future recurrence should retain phase-tagged decode failures, browser version,
Blob size and type, page origin, navigation events and the matching server frame
record before changing transport or decoding behavior. The current evidence
narrows the investigation but is not a whole-browser acceptance claim.

<a id="text-input-audit-2026-09-23"></a>

## Text input exception audit (2026-09-23)

<a id="text-input-audit-2026-09-23-result-and-scope"></a>

### Result and scope

A review of SKT, KTF, LGT, the shared session, WebSocket ownership and the
browser dialog found five defect categories, reproduced in eleven new scenarios.
These are authored-fixture reproductions, not measurements of how often real
games encounter them, and the audit changed no product code. All five categories
were fixed afterwards: the findings below preserve the original evidence, and
the [resolution](#text-input-audit-2026-09-23-resolution) records the implementation and the regression
tests that enforce it.

The inspected worktree includes the KTF timer and clipping corrections in
PR #192. Existing relevant Go tests passed 189 leaf cases. The browser dialog
and transport Node tests passed 25 tests. Seven additional dialog scenarios
passed in both Chromium and WebKit, for 14 browser checks. The browser checks
use real DOM controls with a controlled session adapter; existing Go tests
exercise the actual WebSocket transport. Synthetic composition events do not
establish physical phone IME behavior.

<a id="text-input-audit-2026-09-23-reproduced-defects"></a>

### Reproduced defects

<a id="text-input-audit-2026-09-23-1-a-restored-target-can-revive-an-old-edit"></a>

#### 1. A restored target can revive an old edit

SKT validates the current screen, text and constraints by value, without a
lifecycle revision for ordinary MIDP fields. Open a TextBox edit, change its
text, then restore the original string: the old edit is accepted. The same
happens after leaving and returning to the screen, changing and restoring
constraints, or opening and closing the command menu. All four probes fail
the expected stale-edit rejection.

KTF explicit LWC focus has the same gap: focus another component and then the
original component before committing. The old edit replaces the field value.
The shown-shell, KFC and LGT Java paths already have stronger revision checks;
their existence does not protect this explicit-focus path.

Relevant code: `internal/platform/skt/host_text_input.go`,
`internal/platform/ktf/runtime_lwc_host_input.go`, and
`runtimeComponentSetFocus` in `internal/platform/ktf/runtime_lwc.go`.
Fix direction: track target lifecycle and mutation revisions through the native
setters and focus changes, including changes that restore earlier values.

<a id="text-input-audit-2026-09-23-2-ktf-can-offer-a-hidden-explicitly-focused-field"></a>

#### 2. KTF can offer a hidden explicitly focused field

Create a shell with a text child, show it, explicitly focus the child, and hide
the shell. A new Host request still offers that child as editable. Hiding the
shell updates its visibility and removes the shown-shell reference, but leaves
`lwc:focus` pointing at the child. The explicit-focus branch does not verify
that the field still belongs to an active visible container.

This is a fresh availability error, distinct from submitting an old snapshot.
Relevant code: `Session.TextInput` in
`internal/platform/ktf/runtime_lwc_host_input.go` and
`runtimeComponentShown` in `internal/platform/ktf/runtime_lwc.go`.
Fix direction: validate visibility/ownership for explicit focus without
requiring a shell for legitimate card-drawn components.

<a id="text-input-audit-2026-09-23-3-skt-continues-insertion-after-a-callback-removes-the-target"></a>

#### 3. SKT continues insertion after a callback removes the target

An authored TextComponent detaches itself during the first `insert(char)`
callback. Submitting `abc` still invokes all three insertions and returns
success. Canceling the context during that first callback also delivers all
three characters. `commitTextComponent` validates once before entering its
character loop and does not recheck the target, revision or cancellation
between guest callbacks.

Relevant code: `commitTextComponent` in
`internal/platform/skt/host_text_input.go`.
Fix direction: stop at the first ownership change or cancellation, invalidate
the consumed snapshot, and leave any accepted prefix owned by the guest.
Whole-field rollback is unavailable on this append interface.

<a id="text-input-audit-2026-09-23-4-lgt-permits-retry-after-text-delivery-followed-by-a-guest-fault"></a>

#### 4. LGT permits retry after text delivery followed by a guest fault

An authored Clet delivers `abc` through `MC_imHandleInput`, then faults just
before its callback returns. The completion buffer contains the text, but the
Host receives an error. After restoring the fixture's return instruction, the
same edit accepts `abc` again. Consuming completed text does not advance the
C input revision. The WebSocket handler retains an edit after generic guest
errors, so it cannot distinguish this case from a retryable rejection.

This proves repeated delivery through the same edit after partial failure.
An append widget can consequently duplicate a prefix; field-level duplication
was not measured in a real archive during this audit.

Relevant code: `cTextInput` in `internal/platform/lgt/host_text_input.go`,
`handleInputKey` in `internal/platform/lgt/wipic_im.go`, and
`internal/webhost/textinput.go`.
Fix direction: invalidate snapshots whenever delivery consumed bytes, including
error returns, while preserving retry after a rejection that consumed nothing.

<a id="text-input-audit-2026-09-23-5-host-composition-and-keypad-editing-disagree-on-length-units"></a>

#### 5. Host composition and keypad editing disagree on length units

Set a Java field limit to four UTF-16 units. Host composition accepts `AB🙂`,
which occupies exactly four units. Pressing keypad `2` then appends another
character, producing five units. This reproduces in both SKT TextBox and KTF
LWC fields. The Host validator counts UTF-16 units, while the shared keypad
editor receives that limit as a count of Go runes.

Relevant code: `textEditor` in `internal/platform/skt/lcdui_input.go`,
`textEditorFor` in `internal/platform/ktf/runtime_lwc_key.go`, and
`internal/textinput/textinput.go`.
Fix direction: preserve the platform's length unit when switching between
Host composition and keypad edits. Test supplementary characters at the limit
and deletion followed by insertion, without changing C/EUC-KR field policy.

<a id="text-input-audit-2026-09-23-supported-routes-and-deliberate-limits"></a>

### Supported routes and deliberate limits

One game can use different text-input implementations on different screens,
so these routes, not games, are the automated test units:

| Route | Primary contract and necessary evidence |
| --- | --- |
| SKT MIDP TextBox | Whole value, UTF-16 limit, screen lifecycle, command overlay |
| SKT Form TextField | Selected child, whole value, item-change notification |
| SKT XTextField | Focused field on the visible canvas, whole value |
| SKT TextComponent | Attached target, append by Java char, per-callback ownership |
| KTF LWC explicit focus | Whole value, visible/active target and focus lifetime |
| KTF LWC shown shell | Sole direct child/work component, shell and child revisions |
| KTF non-modal KFC form | Unique listened field, form lifetime, guest confirmation |
| KTF WIPI-C | EUC-KR append, synchronous or owning-timer delivery, CLR |
| LGT LWC | Focus and shown parent chain, revisions, whole value |
| LGT WIPI-C | EUC-KR append, declared capacity or recognized output-only caller |

An installed composition-delta listener on KTF/LGT LWC fields, ambiguous KTF
shell/form layouts, synchronous KFC modal entry, general KTF guest event-loop
delivery, and arbitrary game-drawn editors remain unsupported boundaries.
Their rejection is not automatically a regression.

Ten additional encoding checks confirm current C-path limits on KTF and LGT:
precomposed Korean and an empty string pass; decomposed Hangul, supplementary
Unicode and newlines are rejected. A decomposed string can look like ordinary
Korean while falling outside strict EUC-KR. This is a declared encoding limit,
not evidence that every Korean IME submission is supported.

<a id="text-input-audit-2026-09-23-making-acceptance-unambiguous"></a>

### Making acceptance unambiguous

Record an **editor route**, not just an archive launch. Each record needs the
archive hash, isolated save setup, steps to the field, expected adapter, input
and expected guest value, deletion/replacement steps, confirmation, and save
restoration if the guest promises persistence at that point.

Use separate outcomes:

- **Pass:** reached the editor; readback or captured guest output proves the
  value; confirmation and the stated persistence boundary work.
- **Unsupported:** reached the editor and identified a contract outside the
  implemented routes above.
- **Unreached:** the route never established an active editor. A startup scan
  or API reference is only a candidate for a future manual route.
- **Fail:** reached a supported contract and violated a stated invariant.

The red status combines missing targets, stale edits and invalid text. Those
cases differ internally, but the single label cannot classify a manual result,
so test or debug evidence must keep the underlying error and active adapter. The
audit did not change the compact user-facing labels.

For each supported route, share a matrix of empty/normal/maximum/over-limit
input; Korean and supplementary Unicode; newline/control policy; deletion and
replacement; target changes and restoration; callback failure after a prefix;
cancel/reopen; and session parking/resume. Keep callback and timer cases in
authored fixtures, where they are deterministic. Use representative real-game
routes to confirm field detection, display, confirmation and persistence.
Reserve physical-device testing for actual IME, clipboard, keyboard visibility,
and interruption behavior that synthetic events cannot establish.

<a id="text-input-audit-2026-09-23-reproduction-artifacts"></a>

### Reproduction artifacts

Ignored artifacts are under `var/diagnostics/text-input-audit-20260923/`:

- `overlay.json` and `*-audit.go.overlay`: authored tests appended through a
  Go overlay, preserving product code and the ordinary test suite.
- `baseline.jsonl`: 189 existing Go cases passed.
- `probes.jsonl`: 10 encoding cases passed and 11 new defect scenarios failed.
- `probes-debug.jsonl`: the same 10 passes and 11 failures in the debug profile.
- `browser.mjs`, `browser-results.json`: seven scenarios in two real browser
  engines, all passed, using the actual `web/text-input.js` module.

Run the defect probes from the original worktree with:

```sh
go test -overlay var/diagnostics/text-input-audit-20260923/overlay.json \
  ./internal/platform/skt ./internal/platform/ktf ./internal/platform/lgt \
  -run '^TestAudit' -count=1 -v
```

This overlay documents the original failing state; the regression cases are in
the ordinary suite, run with the command under [Resolution](#text-input-audit-2026-09-23-resolution). The
audit makes no new real-game or physical-device acceptance claims.

<a id="text-input-audit-2026-09-23-resolution"></a>

### Resolution

The five fixes are scoped to the platform adapters and shared keypad editor:

| Defect | Correction and regression coverage |
| --- | --- |
| Restored target | SKT display/menu, field mutation and Form selection/membership revisions; XTextField focus revisions; KTF component and Form focus revisions. Restored state rejects the old edit, while a fresh snapshot commits. |
| Hidden explicit focus | KTF records parent membership, follows a bounded ancestry chain to the shown shell, and rejects hidden, detached or replaced owners. Tests cover direct/nested/work children, a never-shown shell subclass, hide/show and remove/reinsert. Never-attached card-drawn fields retain support. |
| Callback interruption | SKT rechecks attachment, revision, display/queued display, command overlay and context between inserts and before repaint. Tests detach, detach/reattach, request another screen, cancel, and detach on the final character; a consumed snapshot cannot be reused. |
| Partial delivery retry | LGT advances the C edit revision whenever completion bytes were consumed, even on a guest fault. Authored ARM callbacks distinguish faults before delivery, faults after delivery, successful single-use commits and fresh-snapshot recovery. Existing over-capacity rejection remains retryable. |
| Length-unit mismatch | Java adapters use the shared editor's UTF-16 limit mode. SKT TextBox/XTextField and both KTF keypad routes preserve a full `AB🙂` value at four units and allow deletion/reinsertion. Shared tests cover whole-character truncation, caret/cycling, BMP text and unlimited fields; the rune-count API remains available. |

Regression files:

- `internal/platform/skt/host_text_input_regression_test.go`
- `internal/platform/ktf/host_text_input_regression_test.go`
- `internal/platform/lgt/host_text_input_regression_test.go`
- `internal/textinput/textinput_test.go`

Run the focused regression checks with:

```sh
go test ./internal/platform/skt ./internal/platform/ktf ./internal/platform/lgt \
  ./internal/textinput -run 'Test(SKT|KTF|LGT|KnownCEncoding|UTF16)' -count=1
```

Validation passed on the corrected tree: `make test` (including 227 Node tests),
`make test-debug`, `go test -race ./internal/...`, `go vet ./...`, and
`make server server-release`. The seven dialog scenarios passed again in both
Chromium and WebKit, for 14 checks against real DOM controls and the controlled
session adapter described above. Full Go gates also exercise the existing
WebSocket integration tests. Logs are retained under the ignored
`var/diagnostics/text-input-fixes-20260923/` directory.

The fixes do not add new editor contracts or change strict EUC-KR/BMP-only
boundaries. Physical IME behavior and additional real-game editor routes still
need the manual evidence described above.
