# SKT implementation history

This file preserves implementation investigations and dated validation records.
Statements about current behavior, missing APIs, corpus size, and planned work
apply to their original revision; later records may supersede them.
Start with [skvm.md](../skvm.md) for the maintained overview.
This consolidation adds no execution or acceptance evidence.


## Contents

- [Implementation record through 2026-09-17](#implementation-skvm-and-the-skt-platform)
- [MathFP](#implementation-mathfp)
- [Files](#implementation-files)
- [Graphics2D](#implementation-graphics2d)
- [Synchronous repaint queue](#implementation-synchronous-repaint-queue)
- [Input](#implementation-input)
- [The frame loop](#implementation-the-frame-loop)
- [Fifteen titles, and the pass that made room for twelve of them](#implementation-fifteen-titles-and-the-pass-that-made-room-for-twelve-of-them)
- [What the local titles still stop at](#implementation-what-the-local-titles-still-stop-at)
- [Nothing a local title names is missing](#implementation-nothing-a-local-title-names-is-missing)
- [A heap with addresses in it](#implementation-a-heap-with-addresses-in-it)
- [The handset a title was packaged for is in its resource names](#implementation-the-handset-a-title-was-packaged-for-is-in-its-resource-names)
- [Ninety archives, and the pass that made room for eleven of them](#implementation-ninety-archives-and-the-pass-that-made-room-for-eleven-of-them)
- [Deliberately incomplete](#implementation-deliberately-incomplete)
- [Validation](#implementation-validation)
- [Compatibility follow-up (2026-09-12)](#followup-skt-compatibility-follow-up-2026-09-12)
- [CLDC data stream interfaces](#followup-cldc-data-stream-interfaces)
- [Save, progression, and sound observations](#followup-save-progression-and-sound-observations)
- [Jlet coverage](#followup-jlet-coverage)
- [Landscape evidence](#followup-landscape-evidence)
- [Native keyboard and IME boundary](#followup-native-keyboard-and-ime-boundary)
- [Remaining acceptance](#followup-remaining-acceptance)
- [Gameplay QA, 2026-09-20](#gameplay-qa-2026-09-20)
- [SKT name input with a placeholder Canvas](#skt-name-input-2026-09-23)
- [SKT input and tile rendering (2026-09-29)](#skt-qa-2026-09-29)
- [SKT retained-session resume (2026-09-30)](#skt-resume-2026-09-30)
- [Text input offered when no strict route names a field (2026-10-20)](#skt-text-input-2026-10-20)
- [WIPI FileSystem and DataBase (2026-10-22)](#skt-wipi-storage-2026-10-22)
- [Storage contract review (2026-10-22)](#skt-storage-review-2026-10-22)

<a id="implementation-skvm-and-the-skt-platform"></a>

## Implementation record through 2026-09-17

See the [2026-09-12 coverage audit](coverage.md#ktf-skt-ktf-and-skt-coverage-audit) for current
API gaps and validation. It also identifies older limitations below that are
already implemented, including Canvas touch, curves, and Host vibration.

An SKT handset game is a MIDlet JAR: no native code, no custom executable
format, no ARM. What makes it SKT is that its world contains `com.skt.m`,
`com.skt.m3d` and `com.xce` on top of standard MIDP.

`internal/platform/skt` is therefore the whole Java runtime: class loading
through the shared JVM, the MIDP surface a title draws and saves through, and
the SKVM classes on top. **A few archives in the same container hold a WIPI
Jlet rather than a MIDlet**, and that surface is a layer over this one — see
"Three archives hold a Jlet, not a MIDlet". It used to be a thin layer over a
vendor-neutral "j2me" package; that package had exactly one consumer, and
keeping it neutral only hid where its contracts came from. The class surface
lives in `internal/api/skvm` and the natives in
`internal/platform/skt/skvm*.go`, next to the framebuffer, fonts and images
they need.

**The SKVM classes come from the platform, not from the title.** A machine
built from a title's own JAR alone must not resolve `com.skt.m`; otherwise a
title could ship its own `MathFP` and replace the one every other title
measures against. `TestSKVMClassesComeFromThePlatformNotTheTitle` holds that
line.

<a id="implementation-mathfp"></a>

### MathFP

`MathFP` is nine-decimal fixed point: **1.0 is 1_000_000_000**. Every value a
game passes and receives is scaled that way, which is why `PI` is
`3_141_592_654` and why every product is rescaled. `parseFP` scales a whole
number in; `toLong` truncates toward zero on the way out.

The scale is the observable contract, so it matches the reference
implementation exactly. Overflow saturates rather than wrapping: a game that
overflows an intermediate sees a huge number, not a sign flip.

<a id="implementation-files"></a>

### Files

`com.xce.io.XFile` reads and writes through the **same Host save boundary
MIDP RMS uses**, under the `fs/` scope that KTF's guest filesystem also uses,
so one owner directory holds everything a title persists whichever platform
wrote it:

```
var/savedata/<profile>/skt/<owner>/fs/<path>
```

A path the game never wrote falls back to the JAR resource of the same name,
which is how a game reads data it shipped. `unlink` empties a file rather than
removing it, because the save boundary has no delete — the same reason RMS
keeps a store index.

`FileInputStream` and `FileOutputStream` are written in Java on top of
`XFile`, so the file semantics have one implementation.

<a id="implementation-graphics2d"></a>

### Graphics2D

`Graphics2D` wraps a MIDP `Graphics` and adds what MIDP lacks: per-pixel read
and write, rectangle inversion, a screen capture, and a blit with
`SRC_COPY`/`AND`/`OR`/`XOR`. The pixel *mask* is the surface's alpha channel
rather than a separate plane, because these surfaces carry real alpha.

<a id="implementation-synchronous-repaint-queue"></a>

### Synchronous repaint queue

A worker may call `repaint` followed by `serviceRepaints` while the Host waits
for its next input. The synchronous call paints the dirty region immediately,
but the queued callback still exists. Previously each iteration cleared the
only queued flag and posted another callback; the 1,025th request filled the
1,024-event queue and killed the worker. A local authentication acceptance
route exposed this while the CLI was waiting between commands.

The runtime now tracks dirty pixels and a posted callback separately. Repaints
reuse the outstanding callback until the Host consumes it; repaints requested
inside paint remain deferred to the following Host pass. An authored Canvas
JAR regression performs 2,048 synchronous paints without a Host drain, then
delivers input and a later partial repaint. The earlier code failed at request
1,025; that regression and the existing paint/serial-dispatch tests now pass.

<a id="implementation-input"></a>

### Input

A Canvas with no MIDP commands receives the first soft key as handset menu
code 129, including press, repeat and release. A local SKT title switches on
that value to open its in-game menu; swallowing the key blocked that route.
Displayables with commands retain command dispatch, and Jlet Cards retain
their WIPI mapping. This is a vendor mapping, not a MIDP-standard key.

Keys reach a Canvas as `keyPressed`, `keyReleased` and `keyRepeated`, and a
`GameCanvas` may read `getKeyStates` instead. A static read of the fifteen
local archives settles which the corpus uses: **fourteen override
`keyReleased`**, five override `keyRepeated`, and **none uses
`getKeyStates`**. A release therefore matters to nearly every title, and none
holds a set of directions the way a `GameCanvas` bitmask would.

One title has the defect that shape allows. Holding a direction and releasing
a *different* key that is still down — a keyboard does this every time two
keys overlap — stops the character:

- With the release delivered, a side-scroller walking left stops the instant
  another held key comes up, and the screen does not change again: 0 pixels
  over the next 120 ticks.
- Without it, the same route walks on exactly like the control that never
  touched a second key.

This runtime therefore sends keys through the same pad the Clet platform does
([`internal/keypad`](../../internal/keypad/keypad.go), and `lgt.md` "A release
stops a character the pad has moved on from" for how it was found): a pad key
released while another is still held delivers nothing, the direction now under
the thumb is announced once when the one in use comes up, and everything that
is not the pad is delivered as it happens. The pad here is the four MIDP
navigation codes **and the digits 2, 4, 6 and 8**; the local side-scroller
walks on both, which is the evidence that the digits belong in it.

A repeat is neither a press nor a release and does not move the pad; it is
delivered to whatever the pad last reported, which is what the five titles
that override `keyRepeated` wait for. **What arrives is the handset's repeat,
not the Host's**: a browser repeats a held keyboard key at the user's keyboard
setting, thirty a second where a handset sent four, so the Host layer drops it
and generates the specification's "600:250" on the session's clock instead —
`session.md`, "A held key repeats on the handset's clock". The key it names is
this pad's answer (`Pad.Held`), because after a thumb rolls onto another
direction the Host is holding a key the title was never given.

Two things are deliberately not done. A release is never withheld for a key
that is not the pad: those are actions, and a title that never hears the
release of its attack key is worse off than one that stops walking. And no
repeat is invented: the one delivered is the Host's clock asking for it, and
what a held key does between events is the title's business.

<a id="implementation-the-frame-loop"></a>

### The frame loop

A MIDlet has no tick of its own. It runs on the callbacks the Host makes and
on whatever those callbacks deferred, and an SKT title defers its whole game.
Four contracts decide whether it runs at all, and each fails silently when
missing: the title starts, shows a Canvas, reports no error, and never paints a
second frame.

**A Canvas is told when it arrives on the screen and when it leaves.** MIDP
calls `showNotify` on the Canvas becoming current *before* it is painted, and
`hideNotify` on the one that left. Neither was declared on `Canvas` or called,
and two archives that depended on them failed in different ways. One starts
its game thread inside `showNotify`: without the call it painted one frame and
then sat still, taking keys with nothing running to act on them. The other
keeps a paint counter that only `showNotify` sets, and the paint that sees that
value reads its opening picture out of the archive; with the counter never
set, the load never ran and the next paint drew a null image. The symptom is
far from the cause: the throw is absorbed as a callback that did not happen
(below), but the title's `paint` ends with the `notify` its drawing thread is
blocked on, so a throw anywhere before that leaves the thread waiting for a
repaint nobody requests. One missing callback ends the title with no error, on
a screen holding a picture.

The order is part of the contract: a `showNotify` delivered after the first
paint is the same defect, which `TestACanvasIsToldItIsShownBeforeItPaints` and
the two tests beside it hold. Becoming current when already current is not an
arrival and is not announced. A throw out of either callback is absorbed like
every other guest callback's.

**`Display.callSerially` runs one Runnable per Host pass.** MIDP says a serial
Runnable follows the events already on the loop. The common frame loop here is
a Runnable that repaints and re-queues *itself*, so a Runnable posted directly
onto the event queue re-queues itself inside the same drain: the loop never
empties, and the pass ends on the event loop's own limit rather than on a
frame. `callSerially` therefore queues, and `RunPending` — the Host's
per-frame pass — takes exactly one. The WIPI path applies the same rule to a
serial Runnable for the same reason (`docs/ktf.md`).

**The screen `Graphics` outlives the paint it arrived in.** MIDP says a
Graphics is valid only inside `paint`. On this vendor's handsets it was not,
and titles are written against the handset: a Canvas whose `paint` only stores
the Graphics in a static field, and a game thread that draws through it at any
time and pushes the result with `XDisplay.refresh`. There is one screen
Graphics for the life of the runtime; each paint resets it to that repaint's
clip, no translation, and the default font. Two guest threads drawing at once
still race; that is the game's business, as it was on the handset.

**The Host reading the same bytes is not the game's business.** A context that
draws into an Image locks the Image for one drawing call; a context that draws
on the screen has no destination, so it locked nothing — and the screen is
what the Host copies out to present a frame. A real archive reports it under
the race detector: a guest thread inside `fillRect` against the main
goroutine's copy, on a binary two releases old as well as on the current one.
The buffer is allocated once and never grown, so the cost was a torn frame
rather than an out-of-bounds access, which is why it went unseen.

The screen context now takes the render lock for each drawing call, the same
seam the Image lock uses. **The paint call had to move**: it ran with that lock
held for its whole length, which would deadlock the paint against its own
Graphics, so it runs without it and the following copy takes the lock alone. A
frame is therefore taken between drawing calls rather than through one; it is
not a snapshot of a whole paint, and for these titles no such snapshot exists,
because their painting is a thread that never stops.

**Frames are not comparable run to run on this platform, so the check is the
race detector.** Two runs of the *same* binary over the fifteen local archives
disagree on ten of them, more than the nine that separate the binaries on
either side of this change, because guest threads interleave differently each
time. All fifteen run clean under `-race` now, where one reported a race in
two of two runs before.

**`XDisplay.refresh` takes the picture at once and shows it with the pass.**
The call is the game's own frame boundary — the guest thread declaring the
screen complete — so the surface is copied there, on that thread, which makes
the picture whole rather than half-drawn. The *present* waits for the Host
pass, because a Host shows at most one frame per pass: the CLI reads the
surface at a tick boundary and the page sends one frame a tick.

One title family pushes about a hundred times per pass — **496,029 calls in
4,000 ticks**, against another title's 3.7 per pass — because its loop draws as
fast as the machine allows and pushes every iteration. Presenting each call
allocated a framebuffer copy and handed it to a surface that copied it again;
coalescing took **a fifth of the host CPU** off those runs (19.4s to 15.1s of
user time for 900 ticks) with the pictures unchanged, and the guest waits for
nothing.

A lifecycle dispatch presents a pending picture too. A title that draws its
notice through the direct display and then calls `System.exit` does all of it
inside `startApp`, so that dispatch is its only pass. Two local titles do
exactly that; without it they show a black screen instead of their notice.

**`XDisplay.width`, `height` and `height2` are fields, not methods.** A game
reads the screen size from them, typically caching it in a Canvas constructor
and repainting that rectangle from then on, so the runtime fills them from the
framebuffer before any guest code runs. Left at zero, the game repaints a
zero-sized rectangle and paints nothing. `height2` is the drawable height on a
handset that reserved rows for a soft-key bar; a Canvas here gets the whole
framebuffer, so both heights are the framebuffer's.

**The handset properties are answered.** `System.getProperty` answers `MIN`,
`m.MIN`, `m.VENDER`, `m.CARRIER`, `m.COLOR`, `m.SK_VM` and
`com.xce.wipi.version` alongside the MIDP names. A missing one is not read as
"unknown": a title compares the vendor string against the single device it
carries a workaround for, and a null there throws inside the constructor that
would have started its game thread. The subscriber number is the one the WIPI
platforms answer with, because the question is about the handset rather than
about the runtime asking, and the vendor string deliberately names no real
manufacturer. `m.CARRIER` is not a constant: it is derived from the subscriber
number, because a title reads the two together — see "The carrier code is part
of the number, not a fact beside it".

**The JVM step limit is a window, not a ceiling.** A title's own thread is the
game — it decodes images and loads a world before it draws anything — so a
fixed instruction budget kills every title partway through loading. This
runtime grants another window while its MIDlet is running and refuses once it
is destroyed, which makes destroying the MIDlet the way a runaway guest stops.
See `docs/jvm.md`.

**`System.out` and `System.err` exist.** Shipped titles are full of leftover
debug printing. The streams are runtime-owned CLDC classes in `internal/jvm`,
and what a game prints goes to the logging boundary at debug level, one line
per call. The field must resolve: a game that cannot read `System.out` dies in
whichever method contains the print.

<a id="implementation-fifteen-titles-and-the-pass-that-made-room-for-twelve-of-them"></a>

### Fifteen titles, and the pass that made room for twelve of them

The corpus went from three local titles to fifteen in one step, and the twelve
new ones arrived as lifecycle failures: eleven of the fifteen refused to start.
The cause was almost never a defect; it was surface this runtime had never been
asked for, because three titles is not a corpus.

**The pass that fixed them did not start from a run.** `runskt -diag` reports
what a title asked for before it stopped, which yields one gap per run and a
rebuild between each. `internal/tools/apiscan` reads the classes instead: every
symbolic reference in every class of every archive, minus what the library
declares and what the platform registers, is the whole list before anything
runs, and a scan over a corpus ranks each entry by how many titles want it.
The first scan answered 55 entries across 12 titles; the last answers 31
across 4, and 11 titles have nothing missing. Its `-natives` flag takes a
`-diag` report, because a platform registers half its surface on a live
runtime and a bare VM cannot see those registrations.

What that list became, roughly in the order worked:

- **The class library grew the java.lang a title names rather than calls.**
  `Integer`, `Math`, `Runtime`, `Throwable` and the whole exception hierarchy
  were natives with no class behind them, which works until a title writes
  `new Integer(3)` or `catch (Throwable t)`. See `docs/jvm.md`, "The classes a
  title names".
- **`Graphics.reset()`**, which is not MIDP's, is what nine titles call on the
  screen Graphics they kept. It restores the state a fresh one would have.
- **`Graphics2D.getGraphics2D(Graphics)`** is how six titles make the wrapper,
  and **`captureLCD` is static**: three titles call it with `invokestatic`
  before they have a wrapper, which also shows it copies the screen rather
  than any surface.
- **`com.xce.lcdui.Toolkit` publishes fields, not only methods.** Ten titles
  read `FONT_HEIGHT`, `graphics`, `DEFAULT_FONT` or `MAX_CHARWIDTH`, and a
  field no class initializer filled is zero; a title that lays out a menu on a
  font height of zero draws every line on top of the last. The initializer
  reads them from the font and the screen this runtime draws with.
- **`com.xce.io.ByteToCharEUC_KR`** is the vendor's decoder as a class: the
  java.io converter API of the day, `convert` filling a char array and
  answering how many characters it wrote. The counts are lengths rather than
  end indexes, which the call site settles: one title computes `end - start`
  for the input and `chars.length - offset` for the output.
- **`System.exit` reaches the lifecycle.** A MIDlet should leave through
  `notifyDestroyed`, but these titles call `exit` on the way out of an error
  dialog, and the two now share one teardown.
- **`m.MODEL` is answered, and the subscriber number is read late.** One title
  parses the model number before comparing it against the two handsets it
  carries workarounds for, so a null was a hard stop. `MIN` used to be copied
  into a table at package load, so a Host that set the number afterwards —
  `-number`, `WFEATURE_PHONE_NUMBER` — was ignored. It is read on request now,
  which matters because five titles authenticate against it; see "Five titles
  check their licence against the handset's number", and "The carrier code is
  part of the number, not a fact beside it" for the second half.
- **An EUC-KR file name in an archive stopped it from opening.**
  `zip.Reader.Open` takes an io/fs path and refuses a name that is not valid
  UTF-8, which a Korean handset's data file has. Entries are now read from the
  entry list by their exact stored name, and an installed file answers to both
  spellings of its name — the stored bytes and the text they decode to —
  because which one a title asks with is its business.

Four of the fifteen needed more than the library; those have their own
sections below.

<a id="implementation-what-the-local-titles-still-stop-at"></a>

### What the local titles still stop at

Each of the three original titles went further than the last, so what remains
is a list of what has not been driven rather than of what is broken. The item
that used to head this list — a title that would not leave its
guardian-selection screen — is closed: the confirm key was fire all along, and
the missing script step has its own section below.

- ~~**Progress saving and loading have not been driven** on any of the three.~~
  **All three are closed end to end**: the `RecordStore` one — see "A progress
  save, taken in a title's own save menu"; the `XFile` one that writes three
  numbered slots, which loads a save it was shipped with and now takes one of
  its own ("The same title's save, found by reading upwards from the write");
  and the third, whose refusal was an `IOException` this platform threw rather
  than a rule of its own — see "The third title's save, and the message that
  named the wrong culprit".
- ~~`runskt` has no diagnostic report.~~ It has one — `runskt <zip> -diag
  report.json` — and its findings are below.

<a id="implementation-the-screen-that-would-not-confirm-was-waiting-for-a-second-choice"></a>

#### The screen that would not confirm was waiting for a second choice

The title whose guardian selection nothing would confirm now reaches its
opening cutscene. The key mapping was correct; the same shape appears twice in
one flow and reads as a dead screen both times.

The screen wants **two** guardians. `bl.a(action, keyCode)` treats fire — or
`5`, which it accepts alongside it — as a **toggle** on the guardian under the
cursor, counts the selection, and raises its confirmation prompt only when the
count is exactly two. A script that presses fire once has marked one guardian
on a screen that is waiting for the other, and at a contact sheet's scale the
screen does not visibly change.

**The prompt's cursor starts on NO**, and fire on NO clears the selection and
returns the screen to its start, which is what a script that keeps pressing
fire sees indefinitely. Left or right moves to YES.

The character-selection screen one step earlier has the same prompt with the
same starting cursor, so the flow needs the same three keys twice:

```sh
# a character, then YES; the guardian prompt dismissed; two guardians, then YES
wfeature runskt <zip> -ticks 2000 \
  -key 200:fire -key 400:fire \
  -key 600:fire -key 700:left -key 800:fire \
  -key 1000:fire \
  -key 1100:fire -key 1200:right -key 1300:fire \
  -key 1400:left -key 1500:fire
```

**The class file settled it, not the screen.** A contact sheet of every key
this runtime can deliver, one per hundred ticks, showed the screen unchanged
for all of them, which shows only that no single key confirms. `javap -c` over
the Canvas names the state handler, the debug build's instruction log names
the class running while the screen is up (`bl`, by a wide margin), and that
class's `a(int, int)` is forty lines of counting. The classes are obfuscated to
one and two letters, but the control flow is not; reading the archive first
would have saved the sweep.

<a id="implementation-an-skt-archive-is-a-filesystem-not-just-a-jar"></a>

#### An SKT archive is a filesystem, not just a JAR

An SKT container holds `<id>.jar` beside `<id>.msd`, and the loader read the
JAR and discarded the rest. Two of the three local archives carry more:
bare-named top-level files — one has `c`, `dnlist2`, `o` and two further JARs,
another has `n1`, `g1`, `cf`. **They are the title's own filesystem.** A
handset received one archive and unpacked it into the title's storage, and the
game opens the files by exactly those names.

Discarding them broke one title's settings silently on every run. Its `bs.j()`
opens `"/c"` for reading, and `com.xce.io.XFile` throws `IOException` for a
file that is neither a save nor a JAR resource, so the read failed and the
title fell through to the branch that writes defaults; **that** opens `"/c"`
for writing with no create bit, which threw for the same reason. The settings
file could be neither read nor written, with nothing on screen to say so.

`Open` now keeps the container's other entries as the archive's own, under
their bare names, and `XFile` finds them through its existing resource
fallback. The exclusions are named rather than guessed — the JAR, the `.msd`,
the `.mod` and the `.wmr` belong to the platform, not the title — and a JAR
entry of the same name still wins. The change is visible in the title's
control flow: the run that used to reach `bs.i` (write the defaults) now
reaches `bs.j` (read the file).

**Settings then save and load.** Changing one in that title's Config screen
writes `fs/c` into the save tree, and the changed byte is exactly the nibble
the game's bytecode edits (`0x17` to `0x37` for the first setting). The next
run prefers the save over the packaged copy, as `initXFileName` does for every
path. A **progress** save had still not been driven: one title's route reaches
its opening cutscene and no save point, one opens files without writing during
the driven route, and the third reaches neither. That is play, not plumbing.

<a id="implementation-a-relative-resource-name-is-a-name-from-the-root-too"></a>

#### A relative resource name is a name from the root too

`Class.getResourceAsStream` resolves a name without a leading slash against
the package of the class it is called through, and this runtime did exactly
that. One title in its own package asks for `tk/images/Map0.lbm` — where the
entry is, spelled from the root of its JAR — and the strict reading turned it
into `tk/tk/images/Map0.lbm` and answered null. The title catches its own
exception, keeps the null, and paints it a few calls later, so the session
ends in `drawImage` rather than at the read.

A fallback already existed for the opposite mistake — three sibling titles
that load `table.gft` through `java/lang/Runtime` (see "Three siblings loaded
their tables through the wrong class") — which looked the last element of the
name up at the root. **The name as written is now tried at the root first**,
and the last element after it. The strict reading still wins whenever
it finds something, so both fallbacks only replace a null: the handset
resolved from the root whether or not a name began with a slash, and these
titles shipped against that.

<a id="implementation-a-record-store-written-and-read-back-by-a-real-title"></a>

#### A record store, written and read back by a real title

The other save boundary is `RecordStore`, which until then only unit tests had
used. A second title's settings go through it rather than `XFile`, and driving
that screen closes the round trip:

```sh
wfeature runskt <zip> -ticks 900 -save /tmp/probe \
    -key 250:fire -key 300:down -key 330:down -key 380:fire \
    -key 500:left -key 560:left -key 620:down -key 680:left -key 700:clear
```

That reaches the settings screen from the main menu, lowers its two settings,
and leaves. The save tree then holds `rms/opdata`, three bytes long, and the
bytes are the settings: the sound level moves with each `left` or `right` and
the speed byte follows the other row. **The next run reads it back**: the
screen opens on `SOUND [1]` and `SPEED SLOW` rather than the defaults, which is
the title's own `oploadData` running at startup.

That is not the progress save the list above wants, but it is the same store:
the title's `saveData` writes progress through `openRecordStore(name, true)`
and `setRecord`/`addRecord`, which the settings path exercised end to end.
What remained was reaching a save point.

**The route to one is in the bytecode rather than on the screen.** Its in-game
menu, which holds the save slots, is reached two ways, both read out of
`javap -c` rather than found by pressing keys: during a battle the key
handler's `lookupswitch` sends **`#`** to it, and on the world map it is
**item 8 of a 3×3 picture menu**, whose cursor the code lays out as
`index = row*3 + col`. Reaching either still requires playing to that point.

<a id="implementation-the-clr-key-had-no-name-so-no-script-could-send-it"></a>

#### The CLR key had no name, so no script could send it

Every screen in that title draws `BACK:CLR` in its corner, and leaving the
settings screen is what writes them. `KeyCodeByName` had no name for that key,
so no scripted run could leave that screen: the settings above could be
changed and never saved, and the run above could not have existed.

`clear` (also `clr` and `back`) now resolves to the handset's `8`, the value
the title's key handler switches on and the one the original runtime carries.
It is the same name the other WIPI platform's routes use, so a script reads
the same whichever vendor it drives.

<a id="implementation-a-progress-save-taken-in-a-titles-own-save-menu"></a>

#### A progress save, taken in a title's own save menu

The round trip the list above was waiting on is closed on one title. It saves
in its own in-game menu, the save lands in the save tree, and the next run
reads it back and resumes where it was taken:

```sh
# skip the prologue (CLR skips, OK advances, and the story alternates between
# the two), open the tactical menu, save into slot 1
wfeature runskt <zip> -ticks 3200 -save /tmp/probe -hold 3 \
    -key 60:fire -key 160:fire -key 240:down -key 300:fire \
    $(for t in $(seq 420 60 2000); do echo -n "-key $t:clear -key $((t+30)):fire "; done) \
    -key 2100:9 -key 2160:fire \
    -key 2300:2 -key 2400:fire -key 2500:fire -key 2620:fire -key 2740:fire
```

The save tree then holds `rms/slot0a`, 1306 bytes, beside the `rms/opdata` the
settings round trip wrote. Starting again with the same `-save` and choosing
*continue* opens the load screen on **`1. 전략 명성 0`** where both slots used
to read `NO SAVE DATA`, and confirming it returns to the world map at the city
where the save was taken. The diagnostic report for that route names the path:
`openRecordStore` nine times, `addRecord` twice, `setRecord` once,
`closeRecordStore` once.

**Two findings made the route findable, and neither came from pressing keys.**

**The menu takes a digit key directly.** The tactical menu is a 3x3 picture
grid whose cursor the code lays out as `row*3 + col`, and its key handler's
`lookupswitch` has a case for each of `1`..`9` that assigns the row and column
outright: `9` is `(2,2)`, index 8, the entry that calls `drawGameInMenu`. The
whole grid is one keypress from anywhere, and arrow presses never need
counting. The menu that opens is a numbered list and takes digits the same
way: `2` is 게임 저장.

**A story-driven prologue needs both of its keys.** The intro art says
`CLR:SKIP` and the dialogue over the map says `NEXT▶OK`, and the title
alternates between the two for a few thousand ticks. A script that presses
only `fire` loops in the intro; one that presses only `clear` stops at the
first line of dialogue. Alternating them walks the whole prologue, and only
after it does the tactical menu answer any key, which is why the earlier key
sweep over that screen found nothing but `fire`.

The other two titles are obfuscated to one- and two-letter classes, where this
one had three named classes. What survives obfuscation is what mattered: the
string constants name the files (`/g1`, `/g2`, `/g3` and `/n1`, `/n2`, `/n3`
for one of them — three slots and their names), and the `lookupswitch` over
key codes reads the same either way.

<a id="implementation-one-of-the-two-loads-a-save-it-was-shipped-with"></a>

#### One of the two loads a save it was shipped with

The XFile title above is `u.class`: `javap -p -c` over the inner JAR finds it
by the two static `String[]` its `<clinit>` builds, `{/g1,/g2,/g3}` and
`{/n1,/n2,/n3}`, and every `com/xce/io/XFile` construction in the class
indexes one of them. The write side is `new XFile(a[slot], 2)`, a `write` and
a `close`; the read side is the same shape with mode 1.

**The archive ships `g1` and `n1` beside the JAR**, 525 and 599 bytes: a save
taken on the handset this container came from. `addInstalledFiles` mounts a
container's files into the archive entries, and `xFileContents` falls back
from the save store to those entries, so the title sees them as `/g1` and
`/n1` without an import. The title screen offers **이어하기** rather than a
new game, choosing it opens a slot screen with the save's level and progress
on `SLOT 1`, and confirming enters the dungeon where it was taken. The
**load** half of that title's round trip is closed on a save the platform did
not write.

<a id="implementation-the-same-titles-save-found-by-reading-upwards-from-the-write"></a>

#### The same title's save, found by reading upwards from the write

A key sweep in the dungeon — both soft keys, `clear`, every digit, `*`, `#`
and the send key — never found that title's save menu. Reading upwards from
the write found it in one pass, and the chain is kept because the next
obfuscated title has the same shape:

- `u.g(byte)` writes `/g<slot>` and `u.f(byte)` writes `/n<slot>`; only
  `u.l()` calls both, so `u.l()` **is** the save.
- Three classes call `u.l()`, and only one is a screen with nothing else in
  it: `ad`, a four-entry menu whose first entry raises a yes/no prompt and
  whose prompt callback sets the two-frame flag that performs the write.
- `ad` is constructed at exactly one place: case **7** of `ck.b()`, the eighth
  entry of the in-game menu.
- `ck` is opened by `u.g()`'s case 6, and the only key that requests it is
  `keyCode 8` — `clear` — in the field handler, **and only while
  `bb.s == 1`**, the walking state. A sweep in any other state sees a key that
  does nothing, which is what the sweep in a dungeon under attack read.

The cursor is the base screen's and it **wraps**: `ck` is built with eight
entries, so one `up` from the entry it opens on lands on the last one. Once
the world is up, the route is five keys:

```sh
# 이어하기 → SLOT 1 → the dungeon, then clear/up/fire opens 시스템,
# and fire/fire takes 게임 저장 and answers 예
wfeature runskt <zip> -ticks 1800 -hold 5 -save /tmp/probe \
    -key 950:fire -key 1100:fire -key 1300:fire \
    -key 1380:clear -key 1440:up -key 1500:fire \
    -key 1560:fire -key 1620:fire
```

The screen answers `저장 되었습니다.`, and the save tree holds `fs/g1` and
`fs/n1` — 525 and 599 bytes, the shipped sizes, differing from the shipped
bytes from the nineteenth byte on. Starting again against the same tree loads
them: `이어하기`, `SLOT 1`, and the dungeon, with the diagnostic report
counting 23 `XFile.read`, 6 `initName` and 4 `close`. **Both halves of that
title's round trip are closed, on a save this platform wrote.**

Two conditions apply when repeating it. The shipped save places the party in a
room with an enemy already present, and an unattended run dies in about two
seconds, so the menu keys must be pressed immediately. A run that presses
`fire` a few hundred ticks later than this walks the *title* screen instead,
because `GAME OVER` has returned it there and the same key means something
else.

<a id="implementation-the-korean-text-was-drawn-on-the-wrong-grid-and-the-latin-beside-it-was-not"></a>

#### The Korean text was drawn on the wrong grid, and the Latin beside it was not

Text outside the authored 5x7 table — Hangul first among it — used
`glyph.Render`, the package-level entry point, which is the 16-dot face, while
the 5x7 table kept answering for the characters it covers. One menu line
therefore carried two text sizes: a numeral seven pixels tall beside a
syllable eleven above the baseline, in a line the platform had reported as ten
pixels tall.

A game reads its layout from the metrics the font reports, so the metrics that
matter are the ones its handset answered, which are the small ones — the same
conclusion KTF reached from a save slot whose label ran into its number. The
evidence here is a local title's main menu: seven entries, each a small `1.`
against a syllable half again its height, the descenders of one line touching
the next and the block overflowing its intended area. On the handset face the
same menu is proportioned and its lines clear each other.

`pixelFace` in `font.go` is the one place that chooses, and it answers
`glyph.Handset()`. The 5x7 table is untouched, so the Latin shapes the
platform tests pin render as before.

**Only one of the three local titles changed.** The other two draw their
Korean with their own sprites and never call `drawString`. They also differ run
to run against *themselves*, in one animated corner of a splash screen,
because that animation runs on the wall clock; a frame diff across a build
change says nothing about them until that is subtracted.

<a id="implementation-a-line-box-that-was-exactly-its-own-ink"></a>

#### A line box that was exactly its own ink

Choosing the face fixed the shapes but not the metrics. The sizes still used a
hand-written table — MEDIUM ten rows tall with a baseline eight rows in, SMALL
eight and seven, LARGE sixteen and fourteen — fitted to the 5x7 Latin, not to
the face that draws everything else.

**A ten-row line holds a syllable and nothing more.** The face is eleven rows,
and its ascent places the nine rows of Korean ink one row below the top of
that box and one above the bottom. Reporting ten removes the bottom row: the
ink still fits, so a test that asks whether the glyph is inside the line still
passes, but no row separates one line from the next. A title that steps its
menu by `Font.getHeight` — most of this corpus — draws the next line's
syllables against this one's, and a title that draws each entry twice for a
shadow runs them together outright. A local title's main menu showed six
Korean entries that read as three.

**The metrics are now the face's, scaled**, as both other WIPI runtimes here
already did and as `newFontData` computes rather than naming numbers. MEDIUM
and SMALL report eleven and eight, LARGE twenty-two and sixteen. The two small
sizes report the same values because they draw the same thing: there is one
Korean face on this platform, and the sizes only ever differed in the number
they claimed. Only one local archive asks for SMALL, and nothing reaches LARGE:
the one archive that names it does so in a branch its own constructor makes
unreachable.

On screen the change is one row in one direction: text hangs from the reported
baseline, so every string sits one row lower and every line box is one row
taller. Across the ninety-one archives at 400 ticks, eight titles moved beyond
the base-versus-base floor, all text-drawing, none changing state, tick count,
or error text; the differing frames are blink phases with the text a row down.
The menu that prompted the change now reads correctly.

<a id="implementation-the-third-titles-save-and-the-message-that-named-the-wrong-culprit"></a>

#### The third title's save, and the message that named the wrong culprit

The remaining title is now driven into play and saves. The route is recorded,
together with how the failure was nearly written off as the title's own rule.

**The route.** Past the guardian selection (above), the opening cutscene skips
on `*` — pressed every 150 ticks it walks the whole cutscene — and `fire`
afterwards carries the party into the world. The in-game menu is the field's
`clear` key, the same shape the other title has: `ar.keyPressed` dispatches on
`m.e`, state 2 is the field and state 5 is the menu `ag`, and the field handler
requests state 13 on `keyCode 8` while `n.h == 1`. The menu is a **row of
icons**, so its cursor moves left/right rather than up/down (`cb.d` takes
`4`/`6` and the two game actions), it wraps, and the last icon is `c`, the
system screen whose first entry is 세이브. One `left` from where the menu opens
lands on it.

**The title answered `실패하였습니다. 저장 공간이 부족합니다.`, and that was this
platform's fault.** It was read for a while as the title's own refusal — the
save entry does sit behind a field, `m.a.a`, which `ac(byte)` computes from the
current scene — and the message was attributed to that guard. Two readings of
the archive disprove the attribution:

- **The guard has its own, different message.** `c`'s save entry reads
  `if (m.a.a) { show strings 51 and 52 } else { proceed }`, and those two
  strings in `sgui/gm.tdf` are `보스전에서는` / `저장할 수 없습니다.` So
  `m.a.a` marks a **boss scene**, and says so.
- **The message that appeared is a catch block.** The screen that runs the save
  is `try { m.h(); show 46 } catch (IOException) { show 47, 48 }`, with 46
  `저장되었습니다.` and 47 and 48 the pair on screen. The title was reporting
  that the platform had thrown.

**The throw came from this platform.** `m.h()` writes the slot by name — the
three slots are `/k`, `/s`, `/w` and the party file is `/o` — with
`new XFile(name, 2)`, mode `WRITE`. `initXFileName` required a create bit
before opening a file that did not exist yet, and `/o` ships inside the
container while `/k` does not, so the first write of a first save threw
`no such file`. **This API has no create bit**: the original runtime opens a
writable file the way `RandomAccessFile("rw")` does, which creates it, and the
constant this platform had called `CREATE` is `READ_DIRECTORY` there. A shipped
title whose only way to write a slot is this call proves that the call created
the slot. Opening for writing now creates; a read-only open of a missing file
still fails, because the title is asking for something that is not there.
Driven to the same menu again, the title writes `fs/k` and `fs/o` and answers
`저장되었습니다.`. A second run reads it back: the title screen's second entry,
on which the game places the cursor once a slot exists, shows the character,
the level and a completion percentage, and choosing it returns the party to
the interior where the save was made. That closed the progress round trip for
the last of the three local SKT titles.

**Two archive facts are kept** because both were wrong in the earlier reading
and both are reusable. The scene id **is** the map number: `ac`'s loader calls
`ce.a("/m/", m.f, 7)`, and that helper opens `prefix + (index / 7) + ".bd"` and
takes record `index % 7` from a five-byte offset table at the front, so twelve
files hold **84 scenes**, and id 82 is file 11, record 5. The header that "had
nothing that varies like an identifier" was that offset table. The scenes are
named in `m/name.tdf`: a count byte, then length-prefixed EUC-KR strings, which
read through to 82 = `게브의 체내` (inside a monster), with 11, 13 and 15 left
blank. Four boss arenas, which is what a save guard would be for.

<a id="implementation-the-default-charset-is-the-handsets-and-a-title-finds-out-through-its-own-font"></a>

#### The default charset is the handset's, and a title finds out through its own font

`String.getBytes()` answered UTF-8 here, and to a title the returned byte is an
**index**, not text. One title draws its own menu labels by passing that array
to its own renderer, which indexes a glyph table with each byte, and UTF-8's
three bytes per syllable index past the table:
`ArrayIndexOutOfBoundsException: index 11034 for length 4700`, thrown inside
the title's text routine while painting its in-game system menu. The labels
beside the failing one drew correctly, which identified the cause: those are
byte arrays from the title's own resource file, EUC-KR all along. Only the
label from a Java string literal went through `getBytes`.

This platform therefore installs the same pair KTF does: `ByteDecoder` and
`ByteEncoder` are EUC-KR, the default charset of these handsets. The menu
paints its five entries — 세이브 / 네트워크 등록 / 도움말 / 옵션 / 종료하기 —
where before it killed the run.

For the next platform surface that touches bytes: the crash came three
thousand ticks into a scripted run and looked like a title bug. The stack
identified it: the exception is thrown by the *title's* code, in the one call
that received a `getBytes` result rather than a resource array.

<a id="implementation-where-a-character-starts-is-not-the-length-of-the-chunk"></a>

##### Where a character starts is not the length of the chunk

`ByteToCharConverter.convert` takes one range at a time, so a two-byte
character can straddle two calls and the lead byte must wait in the converter
for its trail. Deciding *which* byte is waiting has a trap. The first rule
asked whether the last byte was in the lead range and whether the chunk had an
odd number of bytes; the parity stands in for "mid-character" only while the
text is entirely double-byte.

A title that writes `"name:"` before a Korean word breaks it in both
directions. `"abc"` followed by a lead byte is four bytes, so the even count
says the chunk ends whole, the lead byte decodes alone into a replacement
character, and its trail arrives at the head of the next chunk with nothing to
join — two bad characters from one. The other direction also damages text
that was never split: `"a"` followed by a whole `가` is three bytes, and `가`'s
trail byte `0xa1` is itself inside the lead range, so the odd count tears the
trail off a complete character.

The state is recoverable only by scanning forward from the start of the chunk:
a byte in the lead range consumes the byte after it, anything else advances by
one, and the answer is whether the scan runs off the end on a lead byte.
`danglingLeadByte` is that scan, and both chunks above are in its table test.

<a id="implementation-the-diagnostic-report-and-the-three-questions-it-answered"></a>

#### The diagnostic report, and the three questions it answered

`runskt <zip> -diag report.json` writes what a run used: the classes the VM
loaded, the classes it was asked for and did not have, and a call count for
every registered native. It was built after reading the debug build's
instruction log by eye had been the only way to find something twice — first
the frame loop's five defects, then a title's guardian screen — and folding
millions of lines by class and method a second time was once too many.

The counting is an atomic add inside each registration, so it costs a native
call one increment and is on in both build profiles. A report from a specially
built binary would describe that binary rather than the one in use.

**The zeros are what the report is for.** A native that ran shows the title
reached it; a native that never ran is surface nothing proves necessary, and
that census answered the two remaining questions.

**MIDP surface: 404 registered natives, 45 called.** Driven as far as the three
local titles then went — one into its opening cutscene, one into early play,
one to its main menu — 359 registrations were never reached, and 87 classes
were loaded between them. The largest unused groups were the fixed-point maths
class (23), `RecordStore` (20), the high-level `List`/`ChoiceGroup`/`Item`
widgets (51 between them), the text fields (26), `Player` (14), and the 3D mesh
and SIS image surfaces (27).

**That is a floor, not a licence.** These runs stop early, and a title that
saves at a save point reaches `RecordStore` on the first frame after it. The
number guards against growth instead: adding to a surface where 89% of what
exists has never been called by a real title requires a caller in hand, and the
report is how to check.

**GCF: no caller has appeared.** Every `javax.microedition.io.Connector` entry
reported zero across all three titles, so the unimplemented part of the generic
connection framework still awaits a first caller. Nothing was added because the
framework exists.

Taken again on the much longer route that reaches a save point — a whole
prologue, a world map, an in-game menu and a written save, against the
boot-and-menu runs of the census above — the count is still zero. The earlier
count could be read as "these runs stop too early to ask"; this one cannot.

<a id="implementation-the-handset-bitmap-is-this-vendors-format-after-all"></a>

#### The handset bitmap is this vendor's format after all

`Image.createImage` here decodes PNG, JPEG and GIF through
`wipic.DecodeStandard` — the standard library's readers, with one retry over a
PNG whose chunk checksums a title rewrote (`lgt.md`, "A title recolours a
picture and does not fix its checksum") — and the handset bitmap through
`wipic.DecodeLBMP` (`ktf.md`, "LBMP: the handset's own bitmap"). That routing
used to carry a note that no SKT archive shipped LBMP and that the path existed
only because the decoder was shared. **The note was true of three titles and
false of the vendor**: the wider corpus carries about nine hundred LBMP files,
and the SKT titles draw almost everything with them.

They also carry the half of the format the KTF files could not show — a
one-bit transparency mask after the pixels, and bit planes for depths below 8
— and both were found from these titles' screens. One title drew no title menu
because a two-bit logo decoded as an unsupported depth, and the title caught
the `IOException` and drew a null image on its own thread; three siblings drew
their world as magenta blocks, which is a masked sprite with its mask ignored.
`ktf.md` has the format; the arithmetic that identified the mask is
`width * ceil(height / 8)`, and it holds for every masked file in the corpus.

**No local archive ships a BMP for this platform**; that path is still missing,
and this note records it.

<a id="implementation-three-siblings-loaded-their-tables-through-the-wrong-class"></a>

#### Three siblings loaded their tables through the wrong class

Three sibling titles stopped before their first frame with a null `Vector`,
inside a method that reads a table the class initializer should have filled.
The loader is `Runtime.getRuntime().getClass().getResourceAsStream("table.gft")`
and the file is at the top of the JAR, but a relative resource name resolves
against its class's package, and that class is `java/lang/Runtime`, which asks
for `java/lang/table.gft`. The stream came back null, the `DataInputStream`
over it threw inside the title's own `catch (Exception)`, and the loader
returned `true` having filled nothing. The failure surfaced several methods
later, on the read.

The handset runtime answered that name, or the titles would not have shipped.
A relative name that its class's package does not answer is therefore looked
up again at the root of the archive; the strict answer still wins when it
finds something, so this only replaces a null. All three now draw.

**Lesson**: a `catch` around a load is where a platform's null is forgotten.
The swallowed exception named the problem exactly, and the failure eventually
reported was a `Vector` in an unrelated class.

<a id="implementation-five-titles-check-their-licence-against-the-handsets-number"></a>

#### Five titles check their licence against the handset's number

Automatic authentication compatibility recognizes these license-check
layouts and adapts only their final comparison in a session-owned class copy.
The original observations below describe the diagnostic disabled path. See
[current mechanism and validation](../authentication.md#skt-license-checks).

Five titles draw "인증 되지 않은 컨텐츠 입니다" and stop. Nothing is broken:
each ships the same `SecureUtil`, which hashes the subscriber number, a slice
of the service ID from `MIDlet-Jar-URL`, and a per-title constant, and compares
the hex digest against the `MIDlet-Key` in the archive's own descriptor. The
key was issued for the handset the title was bought on, so it authenticates
against a number this runtime does not have.

**It is one library under five names.** The class is `SecureUtil` in one
archive, `com.xce.security.SecureUtil` in another and a single obfuscated
letter in the other three, and all five carry the same method shape, carrier
table and notice text. Four run it inside `startApp` and call
`System.exit(-1)` in the same pass, so a run ends one tick after its first
frame; the fifth runs it behind a publisher logo and ends around a hundred
ticks in. A sweep that reads only the tick count sees five symptoms of one
cause.

**Nothing on this side fails on the way**, which matters because a
`catch (Exception)` around the whole check would have hidden a failure: a
`-trace` of the run walks `isValid` from its first instruction to the
`String.equals` at its end, so `getAppProperty` answered both descriptor keys,
the substring arithmetic ran, and the digest was computed and did not match.
The exception table was never entered.

That makes it a setting rather than a defect: the number the emulator answers
with is `-number` on the server and `WFEATURE_PHONE_NUMBER` for the CLI, and
this platform now reads it late enough to see it. The KTF platform has a title
in the same position for the same reason — `docs/network.md`, "The subscriber
number is the one property worth changing". **Whether a number the user has is
the right one is for the user to know**; nothing here recovers it from the key.

<a id="implementation-the-carrier-code-is-part-of-the-number-not-a-fact-beside-it"></a>

#### The carrier code is part of the number, not a fact beside it

The five did expose a real defect, between the number a user sets and the
number a title reads. `MIN` answers a line and `m.CARRIER` answers a network
code, and this runtime answered the second from a constant table. A title
never reads them separately: it drops the leading digits of `MIN` and restores
the prefix the carrier code names — `SKT` is 011, `STI` 017, `KTF` 016, `HSP`
018, `LGT` 019, and the unified `010` is its own code, because after the
numbering change a prefix no longer identifies a line's network.

A constant `SKT` beside a number of any other shape therefore describes **a
different line**, not the wrong network. With the default number, one call
told a title the handset was 010-0000-0000 and the other told it
011-0000-0000. Because the reconstructed number is what the licence check
above hashes, the documented lever only worked for a number beginning 011:
every other number, including the shape most numbers have today, was rebuilt
into one the user does not have.

`m.CARRIER` is now derived from the subscriber number (`subscriberCarrier`),
and an unrecognised prefix still answers `SKT`. **No other reader needs
weighing**: of the sixty-six classes in the local corpus that read
`m.CARRIER`, sixty-four carry the whole prefix table, one carries part of it,
and the last discards the answer on the next instruction.
`TestTheCarrierCodeDescribesTheNumberItIsAnsweredBeside` performs the corpus's
reconstruction in a fixture, over one number per prefix.

**This changed none of the five**, as expected: the default number is not the
number any of those keys was issued to, and finding that number is not this
project's business. What changed is that a user who knows the number can now
supply one that does not begin with 011.

<a id="implementation-the-four-that-were-left-and-what-each-one-actually-wanted"></a>

#### The four that were left, and what each one actually wanted

None of the four lacked surface the scan had listed. Each was one contract
read the wrong way round, and the scan's remaining entries — the two `java.io`
interfaces, `java.util.Timer`, `TextComponentHandler` — were on paths none of
them takes. Three of those entries are answered now anyway, for the reason
"Nothing a local title names is missing" gives; the two `java.io` interfaces
are the entry that never needed answering, and the same section says why.

- **Three siblings drew their world in magenta blocks**: the transparency mask
  was applied with its polarity inverted, so every sprite came out as its own
  transparent colour with the artwork cut away. `ktf.md` has how the files
  settle it. Their `XDisplay.refresh` count — 110,048 calls in 900 ticks — is
  unrelated and still worth examining: the title presents far more often than
  it draws.
- **One drew its whole opening narration in black on black**: it composes
  Korean from its own initial/medial/final jamo sheets, which are two-bit
  planar LBMP, and the planar ramp was read as light when it is ink. Inverting
  it turned a screen of scattered consonants into the script.
- **One stopped at a studio logo** on `XDisplay.drawImageEx`, the vendor's
  blit with a source rectangle. Thirteen call sites in that title settle its
  arguments; two are unexercised, and this platform says so rather than
  guessing (see below). It now reaches its play screen.
- **One asked for artwork its own archive does not contain.** It branches on
  the screen width and loads `main_logo_240.png` on anything 240 wide or
  wider, and this SKU ships only the 120 and 176 sets because it was packaged
  for a smaller handset. Only the offered screen is wrong: at
  `-screen 176x220` the title runs to its menu, its opening and its first
  scene. **Both Hosts can choose one**: `runskt -screen WxH`, and the page's
  settings panel, which remembers the choice per game and sends it with
  `start`. Nothing detects it — an SKT descriptor declares no screen size — so
  it is a setting rather than a rule. `docs/session.md`, "The screen is part of
  starting a game". `PackagedScreen` later read the declaration from resource
  names; see "The handset a title was packaged for is in its resource names".

<a id="implementation-what-drawimageex-is-and-the-two-things-it-does-not-say"></a>

#### What drawImageEx is, and the two things it does not say

`XDisplay.drawImageEx(Graphics, Image, int x, int y, Image source, int sx,
int sy, int width, int height, int mode)` draws a rectangle of the source at a
point on the Graphics. The thirteen local call sites fix that shape completely
and leave two arguments unexercised: **the second Image is null and the mode
is zero at every one of them.**

A non-null second image is therefore drawn as if absent, and a non-zero mode
is ignored. Both readings of that Image — a destination to draw into instead
of the Graphics, or a mask for the source — are plausible, and refusing the
call would fail the one title that makes it on evidence that names neither. If
a title passes either, this paragraph records the guess.

**The title says so when it does.** "This platform says so rather than
guessing" was true of this paragraph and of nothing that ran: a picture drawn
without the mask or destination its caller asked for looks like a drawing
defect, and the guess was invisible in the frame. `drawImageEx` now writes one
line naming both arguments the first time a title passes either — once per
run, at warning level so a release build carries it too, because a call on a
draw path recurs every frame. `TestDrawImageExReportsTheArgumentsItIgnores`
pins that it is one line and that an ordinary call is silent.

`XDisplay.copyLCD(Graphics, Image, int x, int y, int width, int height)` is
the neighbouring call; it copies the screen into the caller's image, as
`Graphics2D.captureLCD` does into a new one.

`XDisplay.clear(Graphics, Image backdrop, int x, int y)` is the third, and it
blacks out what the Graphics draws on. **One archive calls it, once**, with a
null backdrop and a zero point, and then writes "Now Loading..." in white on
what it cleared, so the result must be dark, and the title's own screen clear
beside it fills with `0xff000000` rather than a chosen colour. The Graphics'
colour is not the answer: the call takes no colour, and the one caller sets
one only afterwards, for the text.

The backdrop and the point are unexercised, like `drawImageEx`'s second image.
A picture handed to a clear is a backdrop and a point is its position, so a
non-null one is drawn at `(x, y)` over the black rather than dropped, and a
title that passes one gets one log line, for the same reason as above.

The clear does not consult the clip. It clears the destination, which is what
a title calling it between two scenes means, and its one caller has just been
drawing elsewhere on the same Graphics.

`captureLCD` is declared static because the corpus decided it, not the shape
of the surrounding class. Five of the fifteen local titles name it, at eight
call sites, and **all eight are `invokestatic`**, read from each class's
constant pool and bytecode because no source is available. It matches what the
call does: it copies the screen, not the surface a wrapper was built around, so
a title has no reason to make a wrapper first.

The consequence is a hard failure, not a degraded one. This VM refuses a
virtual call on a static method outright — `resolveInstanceMethod` answers
`method is static: …` — so a title that reached `captureLCD` with
`invokevirtual` would stop there instead of falling back. No local title does,
and the message names its own cause, so the fallback stays unbuilt until a
caller appears.

<a id="implementation-nothing-a-local-title-names-is-missing"></a>

### Nothing a local title names is missing

The static scan (`internal/tools/apiscan`; "The diagnostic report" above gives
its reason) answers one question over the whole corpus: what would a title
link against that this runtime does not have. Over the fifteen archives this
platform started with, with a live natives report, it came back **empty for
thirteen**, and what remained in the other two was not surface to build. The
ninety-archive corpus reopened it; see "Ninety archives" below for what that
pass answered and what it deliberately left alone.

Each entry it used to list was on a path no local run had taken, which is why
nothing had failed on one. That state is worth closing rather than watching: a
class this runtime lacks is not a degraded feature but `class not found` at the
moment the title first touches it — `internal/platform/skt/connector.go` makes
the same argument for the connection framework. A title that reaches its name
screen after two hours of play should not lose the session there.

<a id="implementation-two-ways-a-title-takes-a-name"></a>

#### Two ways a title takes a name

This vendor has two, and a local title uses each.

**`com.xce.lcdui.TextComponent` is the title's own text buffer**, handed to the
platform's `TextComponentHandler`, a singleton a title reaches through a static
and attaches a component to. The interface's members settle what the handler
is: `insert`, `replace`, `delete`, `moveCursor`, `clear`, `size`, `getMaxSize`,
`getCaretPosition`, and **no way to read a character back**. The handset's
input method never held the text; it held the composition and sent edits, and
the title kept the text. That is why `replace` exists: a multi-tap cycle needs
it when the same key produces the next letter in place.

Two of the title's own classes implement the interface and agree on thirteen
members, so the interface is not inferred from one caller. Those
implementations settle each contract:

- `replace(c)` writes at `caret - 1`. That is the cycle.
- `clear()` on the *component* empties the field; `clear()` on the *handler*
  differs, and the title's own `moveCursor` shows how: it calls the handler's
  `clear` right after moving the caret, where a still-running cycle would write
  the next letter over whatever the caret moved to. The handler's `clear`
  therefore ends the composition and leaves the text alone.
- `moveCursor(int)` takes **a raw key code**, not a direction: the title
  switches on 142 and 145, the two the pad sends. It also inserts a space when
  the caret is already at the end, which is the component's business and not
  the platform's.

The handler therefore runs the shared multi-tap cycle
([`internal/textinput`](../../internal/textinput/textinput.go)) against a
component it cannot read: a new character is `insert`, the same key inside the
commit delay is `replace`, `#` and the CLR key are `delete`, `*` changes the
mode, and the pad's left and right are passed to `moveCursor` as the key codes
the component expects. Whether another character fits is asked of the
component — `size` against `getMaxSize` — because the platform cannot count
text it never sees.

`keyPressed` answers whether the input method took the key, and a key it does
not take reaches the game: a title routes every key through the handler first,
and its pad must keep working while a field is on screen. Both local call sites
discard the answer, as a handset's own titles could afford to, so the answer
costs nothing there and is still the correct one.

**`getInputMode` is the one inference.** The five modes are one bit each, and
no available document says which bit is which. The order a title draws them in
settles it: its switch maps 16, 1, 2, 8 and 4 onto indicators 0 to 4, and a
Korean handset showed Hangul, capitals, small letters, digits, symbols in that
order. 16 is therefore Hangul, and this runtime answers 1, 2 or 8 — capitals,
small letters or digits — matching what the next key press produces. If a
title's indicator turns out wrong, this is the guess to revisit.

**`XTextField` is the other way**, and there the platform holds the text, so it
is the shared editor itself rather than a cycle over another buffer — the same
editor a MIDP `TextBox` on this platform types with. Its four-argument
constructor is what a name screen uses: the initial text, the maximum size,
the constraints, and the owning Canvas. The order of the three is read from
the one local call site, which passes `("", 10, 0)`: 10 is a plausible name
length and not a constraint any of them names.

`Displayable.repaintIM` belongs to the same feature: a repaint that included
the input method's own area, which a handset drew over the screen. No such
area exists here, so what remains is the repaint the title expects with it,
requested virtually so a Canvas gets its own.

**One defect was already in the shared path**, and typing through the vendor's
field exposed it: a MIDP `TextBox` on this platform treated 4 and 6 as caret
movement, because those digits are also the pad's left and right for a game.
In a text field they are letters, and taking them left `ghi` and `mno`
untypable. Only the pad moves the caret now; `TestTextBoxTypesThroughTheKeypad`
covers both halves. The other platform's lwc fields never had it: `isKeypadKey`
there is digits, `*` and `#`, and nothing else.

<a id="implementation-a-timer-runs-on-a-thread-because-the-title-keeps-drawing"></a>

#### A timer runs on a thread, because the title keeps drawing

`java.util.Timer` and `java.util.TimerTask` belong to the profile rather than
the configuration — CLDC has neither — so they are declared in
`internal/api/midp`, and only a VM that installs this profile answers them. One
local title schedules a task at a fixed rate and keeps redrawing while it runs,
which is why the specification puts a task on a background thread: running it
on the caller would stop the frame that made the schedule.

Each schedule gets a guest thread, the same kind a title's own `new Thread`
gets: it takes the platform's scheduler if one is installed, dies with the
session's step budget, and appears in a report as a thread rather than as
hidden runtime work. The two cancels are flags the worker reads before every
run, so a cancel from any thread is seen without a lock, and a running task is
not interrupted, as the specification defines cancel. Fixed-rate and plain
schedules differ in what the next wait is measured from: the time the run was
due, or the time it finished.

<a id="implementation-the-two-entries-the-scan-still-lists-and-why-neither-is-work"></a>

#### The two entries the scan still lists, and why neither is work

Superseded: see [CLDC data stream interfaces](#followup-cldc-data-stream-interfaces).

**`java.io.DataInput` and `java.io.DataOutput`** are named by a title that
writes its save through one and reads it back through the other. Neither was
declared in this runtime, and neither had to be: `invokeinterface` dispatches
on the receiver's class, and the title passes the `DataOutputStream` this
runtime has. The scan sees them because an interface method reference names
its interface; the loader never does.
`TestInterfaceCallsLandOnTheStreamThatWasPassed` pins the mechanism with a
fixture that writes and reads a record through the two interfaces. Nothing in
the corpus does what would load them — a `checkcast`, an `instanceof`, or a
class declaring `implements` — and when one does, the CLDC 1.0 forms of both
are what to declare: the pair without the floating-point methods.

**`com.xce.jam.XBrowser.setNetworkMode` and `com.xce.net.Socket.setPPPPreserveTime`**
are two settings a title applies before opening a connection. It never reaches
them here, by design: the call site is behind a check on the `m.SK_VM`
property, and this runtime answers the value that skips it. Nothing behind
them could be configured either — `docs/network.md` explains why no platform
here reaches a network — so the pair stays unbuilt, and the gating property is
what to remember. If that property's answer changes, these two become reachable
in the same session.

<a id="implementation-a-heap-with-addresses-in-it"></a>

### A heap with addresses in it

The cheat engine searches an address space, and this runtime has none: a
MIDlet's state is Go objects in a map, and the `int` holding a game's gold has
no address to find, narrow down and freeze. That is why the browser removed
the cheat panel on this platform and why `session.Cheat()` used to answer nil
here.

One is therefore synthesized. `internal/platform/skt/heapmap.go` walks the
object graph and gives every object, every array and every loaded class's
statics a span of a synthetic space; `heapread.go` renders a span from the
fields on each read and decodes a write back into the field it lands on.
Nothing is copied or cached: a scan sees the game's state as it changes, and a
freeze writes to the object rather than to a shadow of it. The engine above is
unchanged; it asks this space the same three questions it asks the two ARM
platforms.

**Watching writes needs a different trap, and the interpreter is it.** On the
two ARM platforms a watch is the core noticing a store instruction against a
real address. Here there is no instruction and no address: a field store is
the interpreter assigning into a map, at an address this map invented. The
interpreter therefore reports its own stores — `putfield`, `putstatic` and the
array stores hand a `jvm.StoreEvent` to the VM's store observer — and
`internal/platform/skt/watch.go` maps one back to the address a search found,
by the identity the map is keyed on.

Three properties of the watch:

**It costs nothing when nobody is watching.** The observer is installed by the
first watch and removed with the last, so a title being played rather than
investigated pays one nil check per store: 4,000 ticks of a local title take
2.03 seconds with the hook compiled in and 2.03 without. While a watch is
installed, a store costs a lock and a map lookup on the object's identity, and
only a store to an object the search has mapped costs more.

**The writer is named rather than addressed.** `pc 0x40183a2c` means something
where code is in the address space; here code is class files, so a hit carries
`com/example/Game.tick+42` instead. That is `cheat.WatchHit.Site`, which the
ARM platforms leave empty, and the console and the panel show whichever they
are given.

**The control is offered by the platform's own answer.** `CanWatch` is now
asked, and the answer travels in the `started` message. Nobody asked it while
this platform could not watch, so the browser panel offered the control here
and then failed its poll every interval: the hits call threw, the error reached
the page, and the candidate refresh queued behind it never ran. The refresh
now runs first in that poll regardless, because an optional feature must not
stop the one the panel exists for.

**Addresses are grouped by shape, not by allocation order.** Every instance of
one class lands in that class's own arena, so the region listing names what a
hit is *in*:

```
0x10000000-0x100003e8      1000  statics
0x10010000-0x100143e8     17384  [B
0x10030000-0x10030378       888  [Ljava/lang/Object;
0x100b0000-0x100b0050        80  bd
0x100f0000-0x100f0008         8  com/example/Game
```

"The address is in `cj`" is something a search can act on, where "somewhere in
the heap" is not, and it costs nothing: an arena is a bump allocator over 64KiB
chunks of a 32-bit space, and a shape with three instances wastes the rest of
one chunk of address space and no memory.

Three properties make the addresses useful to a cheat:

- **They are stable.** A span is keyed by the VM's object identity and the
  allocator never reuses an address, so an object keeps its span for the
  session. A value found on one screen is at the same address on the next,
  which is the point of a scan. Measured on one local title, the map grew from
  18 spans and 16KiB at the title screen to 22 spans and 21KiB in play, and
  every address from the first walk was unchanged.
- **They do not keep the game's garbage alive.** A span holds a
  `weak.Pointer`, so mapping an object does not prevent its collection, and a
  dead object's span is dropped on the next walk. A **frozen** address holds a
  strong reference instead — `serviceCheat` retains exactly the objects the
  freeze list names after every Host pass — because a freeze the collector can
  silently disable is worse than the memory it costs. KTF made the same
  decision when it passed frozen addresses to its collector as extra roots.
- **A record is laid out for a search, not for a JVM.** Every field takes four
  bytes except a long or a double, which take eight, so an arena of one
  class's instances is a table the four-byte default stride walks; a `short`
  sits sign-extended in its four bytes and is still found by a two-byte search.
  Array elements take their *natural* width instead: a byte array is how a game
  packs its map data, and spreading it over four bytes each would hide every
  pattern in it.

A reference field reads as the address of its target, so a `dump` can be
followed from one object into the next, and it refuses writes, because writing
one would mean inventing an object. A write narrower than its slot is a
read-modify-write, so a one-byte `set` changes one byte of an `int`, as the
same write against a real address space would.

<a id="implementation-what-the-walk-starts-from-and-what-it-therefore-cannot-find"></a>

#### What the walk starts from, and what it therefore cannot find

The roots are every loaded class's static reference fields, every thread
object the VM holds, and what this runtime holds in Go: the MIDlet, the
Display and the Displayable being shown, the runnables `callSerially` was
handed, the Canvas mid-paint, and **every Displayable the runtime has state
for** — a title keeps its menus and map screen as objects it returns to, and
between visits nothing static points at them.

An object reachable only from a running method's local variable is not in that
set. Nothing durable lives there — a game's state is field-reachable, which is
why it outlives the method that made it — but a value that appears in a scan
and then vanishes from the map is this boundary rather than a bug.

**Reading a static must not initialize its class.** `VM.StaticField` runs
`<clinit>` first because a `getstatic` does, and a walk that touched every
loaded class through it would run guest code the game never requested, from
whichever goroutine was inspecting. `VM.StaticFieldValue` reads the map and
answers "not set" instead, which is correct: an uninitialized class has no
values, and its fields hold zero.

<a id="implementation-what-it-does-not-do"></a>

#### What it does not do

When the map was first built there was **no watch**: finding the instruction
that wrote an address appeared to require instrumenting every `putfield` and
array store, so the engine answered `cheat.ErrWatchUnsupported`, a Host read
that as "no watch control on this platform" rather than a failure, and
`Session.CanWatch()` was what the panel asked first. The interpreter store
observer described above superseded this.

A span has no object *header* — no class word, no length word, no allocation
metadata — because none exists here to present. A span is exactly the
declared fields, or exactly the elements.

<a id="implementation-driven-end-to-end"></a>

#### Driven end to end

`wfeature runskt <game.zip> -cheat` attaches the same console the WIPI paths
use, and `TestLocalCheatProbe` drives the browser's socket path against a local
archive. On one local title: `regions` lists 22 spans, `scan unknown` over
5,222 candidates narrows to one after two `scan changed` passes, `read` shows
it counting up live, `freeze` holds it at 555 for seconds while the game
writes over it every tick, and `unfreeze` restores the game's own value. The
fixture-level tests are `TestHeapMap*` in `internal/platform/skt`.

<a id="implementation-the-handset-a-title-was-packaged-for-is-in-its-resource-names"></a>

### The handset a title was packaged for is in its resource names

One local title will not start on the 240-wide default. It branches on the
screen and builds its title art from that:

```
if (width <  176)                     "/title/main_logo_120.png"
if (width == 240 && height == 160)    "/title/main_logo_120.png"
if (width <  240)                     "/title/main_logo_176.png"
else                                  "/title/main_logo_240.png"
```

The copy in this library holds only the `_176` variants, so on the default it
asks for a missing `_240` image, catches its own `IOException`, and draws the
null it was left with; the session ends on a `NullPointerException` from
`Graphics.drawImage`, which reads like an emulator fault and is the archive
running on the wrong handset.

**The descriptor does not name that handset.** Every key of every local `.msd`
was inventoried — the MIDlet's name, vendor, version, download URL, JAR size,
install-notify URL, icons, keys, and the `DD-` block the download manager reads
— and so was every manifest inside every local JAR; only seven JARs have one,
and none carries more than `Manifest-Version` and `Created-By`. No SKT
descriptor contains a screen size.

**The resource names are the declaration, and they are read now.**
`skt.PackagedScreen` scans every component of every non-class entry path — a
directory as readily as the final name — for one ending in `_<width>`, and
answers a handset when **every** width-suffixed component in the archive
agrees on one width, at least two state it, and this project offers a handset
for that width (128, 176, 240, 320). Anything else answers nothing.

That narrowness is the design, and it is why this is a rule here and a flag on
the other WIPI platform. KTF's `__adf__` does carry a `DisplaySize`, and
thirteen local archives declare `176*220` while twelve of them draw across the
whole 240x320 screen, so honouring the declaration there would shrink twelve
working titles to fix one (`ktf.md`, "A band beside a title screen was the
title's own"). Here it answers for **three** of the ninety-one local archives,
and one of those three declares the default it would have run on anyway, so it
decides the handset for two titles and leaves eighty-nine untouched — measured
by reading back the frame size each archive is given. A Host that requests a
size still wins — `runskt -screen` and the browser's screen setting both
override this — so the rule only decides what "no answer" means.

**The second of those two declares its width as a directory.** Its whole
artwork tree is `img_176/`, and it picks the tree from the height of an
offscreen image it makes one pixel wider and sixteen rows taller than its
Canvas — which on this vendor is the display, so the ladder it indexes is a
ladder of displays:

```
buffer = createImage(Canvas.getWidth() + 1, Canvas.getHeight() + 16)
display > 250  ->  /img_240/
        > 212  ->  /img_240/
        > 166  ->  /img_176/
         else  ->  /img_120/
```

The tree it ships is the one it picks on a display of 167 to 212 rows; any
taller display asks for a missing `/img_240/` tree. It catches the
`IOException` — the trace on the way past is `printStackTrace`, not an
uncaught throw — keeps its nulls, and paints on: white for its first
eighty-eight ticks and black from then on. **That is the whole of "the
presented frame is entirely black" for this archive**, and nothing in this
runtime is wrong in it: a 240x320 handset would find no `/img_240/` tree
either.

**The directory is half of it; the height is the other half.** An archive
declares a width and never a height, so the height is this project's answer,
given in `packagedScreenHandsets`. It answered 220 rows for a declared 176,
which is the wrong side of the ladder above — measured: `-screen 240x320` and
`-screen 176x220` both draw an empty screen, and `-screen 176x196`, `176x208`
and `176x212` all draw the title. It now answers **208**, a display this
generation shipped and one both declaring archives run on: this one paints its
title screen (34,373 lit of 36,608 at 300 ticks, against 0 of 76,800 on the
default), and the other draws the same title screen it drew at 220 — the same
picture, 2,275 lit at twelve hundred ticks either way, twelve rows shorter —
and is still laying its opening text over the same picture two thousand ticks
in. The 220 option remains: `runskt -screen 176x220` and the browser's 176x220
are unchanged, and this map only decides what "no answer" means.

**Reading the whole path also prevents the rule being misled.** No local
archive does this, but nothing prevented it: a package holding
`img_176/logo.png`, `img_176/menu.png` and a pair of `_240` names was answered
**240** by the stem-only rule, which never saw the disagreeing directory. It
now answers nothing, which is what two widths in one archive has always meant
here, and that case is in `screen_test.go` beside the two real shapes.

**The probes ask the rule, because every other Host does.** The local rungs
built their framebuffer at 240x320 regardless of the archive, so they asked a
question neither `runskt` nor a server session asks: both let `PackagedScreen`
answer first. Both archives that declare a width were measured on a handset
this project would not have started them on; one found no artwork there and
painted an empty screen, which the boot rung read as a title that will not
boot. `localProbeFramebuffer` in
[`local_ladder_test.go`](../../internal/platform/skt/local_ladder_test.go) makes
that decision for all four rungs and logs the size whenever it is not the
default, so a rung's report states which handset it asked about. Over the
ninety-one local archives this moves two and nothing else: boot goes 78/1 to
79/0, and sustained, interactive and sound are unchanged, archive for archive.

<a id="implementation-the-sizes-of-the-pictures-do-not-say-what-the-names-say"></a>

#### The sizes of the pictures do not say what the names say

The next candidate after reading names is reading **pictures**: a full-screen
picture is the display's width wide and the display's height less a status bar
tall, so counting the sizes in a package should reveal its handset. This was
measured across every local archive that opens — ninety-four — before anything
was built, and **the signal is not there**.

**A quarter of the corpus has nothing to measure.** Twenty-three of the
ninety-four hold no picture a decoder can size: their artwork lives in packed
resources the title unpacks itself, which a scan sees as opaque bytes. In the
rest, the two hundred and sixty-seven distinct widths are dominated by sprites;
the twenty-five most common widths are almost all under fifty pixels.
Screen-shaped pictures are the exception.

**Where the hypothesis can be tested, it mostly contradicts itself.** Scoring
each archive against the handsets this project offers — a picture counts when
it is exactly a handset wide and within a status bar of that handset's height —
with the loosest rule, one matching picture within forty rows, twenty-one
archives answer. **Thirteen of the twenty-one are contradicted by their own
archive**, which holds a picture *wider* than the inferred handset; one infers
a 128-wide handset while holding a 936-wide picture. Tightening to two pictures
within twenty-four rows leaves six answers, five contradicted the same way. A
wide picture is a sprite sheet or a scrolling backdrop, and neither is a
screen.

**The one archive whose answer is known answers nothing.** The title the rule
in the section above exists for — the one that cannot start on the default —
has a widest picture of 169 and a most common width of 120, neither a handset.
Its handset is declared in its names and is *not* recoverable from its sizes,
so the corpus offers no case where this rule is right and independent evidence
agrees.

**The best rule the data supports is a wash.** It is the narrow one: infer a
handset when the widest picture in the archive is exactly a handset width,
nothing in the archive is wider, and the answer is narrower than the default —
never wider, since nothing supports promoting a title. It moves eight of the
seventy-nine. Run against the default at a matched twelve hundred ticks and
judged on frames, the eight come back **two better, two worse and four
unchanged**: one title that drew its title art torn across a black gap draws
it whole, and one that left stale framebuffer content either side of a narrow
splash comes back clean; against that, one loses the bottom of its copyright
line off the screen, and one has its title screen cramped until the edges cut
the artwork. Of the four unchanged, one is a licence refusal at either size,
two lay out correctly at either, and one draws the identical picture either way
and differs only in letterboxing.

**It also clears the known-bad case by one pixel.** The archive that declares
its width as an `img_176/` directory draws an empty screen on 176x220, and
escapes this rule only because its widest picture is 177 rather than 176. Had
that picture been a pixel narrower, the rule would have handed it a size
measured as blank, and would still do so today, because the width was never the
wrong part of the answer for it. A rule that avoids a known-bad outcome by one
pixel is not a rule.

This is therefore **measured and not built**. The flag remains the way a Host
requests a size, `PackagedScreen` remains the only unrequested answer, and the
default is unchanged.

**Method note, because it reversed two readings.** The first pass judged these
at three hundred ticks and called two titles broken that were mid-transition:
one was on a publisher splash it leaves at around a thousand ticks, another was
between two screens. Both looked like layout failures; neither was. A
screen-size comparison must run long enough for both sides to arrive
somewhere, and a difference at three hundred ticks is a question, not a
finding.

<a id="implementation-a-canvas-here-is-sixteen-rows-shorter-than-the-display"></a>

#### A Canvas here is sixteen rows shorter than the display

The question is whether a Canvas on this vendor is the whole display or the
part a game may draw in. It affects every title, not one archive: **nothing in
the corpus asks for full screen** — none of the ninety-one calls
`setFullScreenMode` — so whatever a Canvas reports is what every title that
reads it lays out against.

**The corpus answers in one voice. Fifty-one of the seventy-eight archives that
hold Java compute `Canvas.getHeight() + 16`**, and what each does with the sum
says what the sum is:

- one stores it in a field it named `m_RealY`;
- one makes its back buffer `createImage(getWidth(), getHeight() + 16)` and
  blits it at the origin, so the sum is the picture the display shows;
- one builds a string from `getWidth()` and the sum and opens a file by that
  name, which on its intended handset must read `240320`;
- one picks its artwork from a ladder — `> 250` the 240 set, `> 166` the 176
  set, otherwise the 120 set — indexed by an offscreen image sixteen rows
  taller than the Canvas, and every rung of that ladder is a handset display.

`Canvas.getHeight()` on this vendor therefore reports the display less sixteen
rows, every title adds them back, and a runtime that answers the display puts
all fifty-one titles' bottom-anchored drawing sixteen rows too low, off the
screen. This runtime did so until it was reported from a phone: a dialogue box
with its second line cut through the middle, a soft-key hint cut in half, a
border that ran off the bottom, in three unrelated titles.

`canvasReservedRows` in [`render.go`](../../internal/platform/skt/render.go) is
the sixteen, and it is the only change. **The framebuffer is still the whole
display, and drawing is not clipped to the smaller rectangle**: one of those
titles puts its soft-key hint at `height - 20` with a seven-row font, which
crosses three rows into the band, and a handset that clipped there would have
shipped it cut. The band is where the handset drew its own soft keys; this
project draws nothing there and leaves it to the guest.

`XDisplay.width`, `height` and `height2` still answer the display. Of the
archives that name `XDisplay`, forty-nine name `height2` and five name
`height`; `height2` is what they size a back buffer with, pass to `repaint`,
clear with `fillRect` and pass to `XDisplay.refresh` as the rectangle to push —
all of which are the whole screen. One title does both in one class: it clears
with `fillRect(0, 0, XDisplay.width, XDisplay.height2)` and lays its scene out
against `getHeight() + 16`, so for that title the two are the same number, and
that number is the display.

**What this replaces.** The earlier reading was that `getHeight()`, `height2`
and `height` were one number, based on two archives that each clear their
whole screen twice, once through `(XDisplay.width, XDisplay.height2)` and once
through `(getWidth(), getHeight())`; if the pairs differed, one clear would
leave a strip the other did not. That argument still holds for `height2`
against `height`, but never reached `getWidth()`/`getHeight()`: a clear that
stops sixteen rows short leaves a strip only where something is drawn, and the
band is the one place a title has no reason to draw. Fifty-one archives naming
the difference, one of them in a filename, outweigh an inference from two.

**The 176 archive needed a flag until this reading made its ladder legible.**
It reads its artwork set from the ladder above, and the rung it needs is
`> 166`, not `> 212`. Under the old answer `-screen 176x220` gave the ladder
236 and `-screen 176x196` gave it 212; under this one they give 220 and 196.
That made the ladder one of displays rather than displays-plus-sixteen — a
240x320 display now indexes it with 320 rather than 336 — and is why the
handset for a declared 176 could be settled at 208 rather than guessed: see
"The handset a title was packaged for is in its resource names", above. The
flag still wins wherever it is given.

**Effect on the whole corpus.** All ninety-one archives, 300 ticks each, first
frame captured, before and after: **sixty-one frames differ and no run changes
outcome** — same state, same error text, and the same tick count everywhere but
one title, which destroys itself at 126 rather than 125. Every differing frame
belongs to a title that reads `Canvas.getHeight()`, directly or through the
`+ 16`, or to one that differs from *itself* between two runs of the same
binary by as much (see "An SKT A/B has a per-title noise floor" below). The
three Jlets are byte-identical, which is what the `Card.getHeight` note in
[`wipi/definitions.go`](../../internal/api/wipi/definitions.go) is for.

**Three titles pay for it with a band.** A title that lays out against
`getHeight()` and never adds the sixteen back now leaves the last sixteen rows
unpainted, and this project draws no soft keys there, so they stay black. Two
of the three are black there anyway — the band is invisible on an already dark
title screen — and the third is a map that now stops short of the bottom edge.
That matches the handset, and it is three against the fifty that were losing a
dialogue box.

**The simpler answer, recorded before anyone reopens this**, is to fill all
three `XDisplay` fields with the full height, forward `Displayable.getHeight`
to the display with no reservation, and give every archive the same window.
This platform did that until the corpus was asked, and a reader arrives at it
from the specification alone, which is why it keeps looking right. The titles
above then draw sixteen rows off the bottom. **A reading the specification
supports is not evidence when the corpus disagrees.**

<a id="implementation-ninety-archives-and-the-pass-that-made-room-for-eleven-of-them"></a>

### Ninety archives, and the pass that made room for eleven of them

The corpus went from fifteen local titles to ninety in one step, and the
now-removed local failure report recorded the first two rounds over it.
Sixty-one archives ran their four hundred ticks with a picture on the first
round; twenty-nine did not, and this section explains the twenty-nine. Every
finding below came the same way: run all of them, group by the line each run
ended on, and take the group naming the most titles first.

**Twelve of the twenty-nine are not this platform at all**; they have their own
section below.

**Five more are the subscriber-number licence** described above, one reached
only after the `Calendar` gap below stopped killing the thread that draws it.
They are a setting rather than a defect.

<a id="implementation-a-handset-fact-read-straight-into-stringequals"></a>

#### A handset fact read straight into String.equals

Three titles stopped on a `NullPointerException` with the platform on one side
of it: `System.getProperty` answered null for a name this runtime lacked, and
the title used the answer without testing it. The comment on
`systemProperties` already explained why that shape is fatal here; what was
missing was a way to find the *next* one before a title does.

A static sweep answers it. Following every
`ldc <name>; invokestatic System.getProperty` in the corpus forward until the
answer is dereferenced or stored splits the property names into those a title
tests and those it uses directly. The ninety archives name two that the runtime
did not answer and that are dereferenced: `m.EXT_SW` in two titles and `m.TYPE`
in six, plus `m.MONDEL` in one, a title's own spelling of the model number
rather than a third fact. All three are answered now, with values chosen to
match none of the constants a title compares against: not the firmware
revision one looks for, and neither of the two network types another switches
on.

**`m.SKT_API` is deliberately left null.** One title dereferences it, and
twelve *test* it and take a different path when it is present — one of them
skipping a colour-mode workaround. Answering it would move twelve working
titles onto an undriven branch, to reach a call the thirteenth makes on a path
no local run takes. The evidence to act on is a title that stops on it, not a
title that names it.

<a id="implementation-an-exception-nothing-caught-ends-the-callback-not-the-midlet"></a>

#### An exception nothing caught ends the callback, not the MIDlet

Two titles stopped inside their own first `paint`, in a shape MIDP makes
unavoidable: `startApp` shows a Canvas and *then* starts the thread that fills
in what the Canvas paints. The handset's green-threaded VM always gave the new
thread its slice before the event loop delivered the repaint; real threads do
not, so the first paint can read a field the game has not yet written. One
title's `paint` calls a method on a null `IGraphics`; another draws a null
`Image`.

The runtime used to end the session for that. It now ends the callback — the
same contract and reasoning as the WIPI side's `absorbUncaughtCallback`
(`ktf.md`): the language says an exception nothing catches ends the thread, the
guest frame a Host built is not where the game's own `try` is, and a callback
that threw did not happen. Both titles paint nothing on their first frame and
play from the second.

**Only an exception is absorbed, never a fault**, and the count reaches the
summary as `uncaught` and `uncaught_first`, because a sweep reading only the
state would count a title that fails every paint as playing. The MIDlet's own
lifecycle calls are not absorbed: MIDP says a `startApp` that throws is a
MIDlet that failed to start, and a Host must hear that.

<a id="implementation-a-repaint-asked-for-from-inside-a-paint-is-the-next-frame"></a>

#### A repaint asked for from inside a paint is the next frame

One title's `paint` ends by calling `repaint` and `Display.callSerially` on
itself, a frame loop written across both. The repaint was posted onto the drain
running the paint, so the paint ran again inside the same Host pass, requested
again, and added a serial Runnable each time; the run ended on the serial
queue's own limit with `pending serial Runnable count exceeds 256`.

MIDP's repaint is a request the implementation services later, which the
`callSerially` pass already established for the other half of that loop. A
repaint arriving while a paint is running is now held for the next Host pass.
It is held *pending* rather than dropped, because `serviceRepaints` requests a
pending paint immediately and must still find one.

<a id="implementation-a-clip-is-a-rule-about-drawing-and-reading-is-not-drawing"></a>

#### A clip is a rule about drawing, and reading is not drawing

One title never returned from `startApp`, and the run hit its wall-clock cap
rather than producing a frame. Its opening measures how its handset clips:
fill a 20×20 square white, narrow the clip to the left half, draw a line across
the whole square, then walk `Graphics2D.getPixel` sideways from the middle
until it meets a pixel the line did not reach. The distance is the answer.

`getPixel` was bounded by the clip, so every read outside it answered zero —
the black line's value — and the walk never ended. A read is now bounded by the
surface, and the write is still bounded by the clip. The translation applies to
both, because a title's coordinates to a Graphics are the ones it draws in.

<a id="implementation-a-relative-resource-name-and-an-object-handed-to-valueof"></a>

#### A relative resource name, and an object handed to valueOf

Two more titles stopped several calls from their cause, both because a name
came out wrong and the title caught its own exception over it. One asks for a
resource by a path spelled from the root of its JAR while standing in a
package — "A relative resource name is a name from the root too" above. The
other builds every path it loads by appending into a `StringBuffer` and calling
`String.valueOf(Object)` on the buffer, which named the buffer instead of
asking it; `jvm.md`, "An object handed to valueOf is asked, not named", has
what that was waiting for and what it costs.

<a id="implementation-three-archives-hold-a-jlet-not-a-midlet"></a>

#### Three archives hold a Jlet, not a MIDlet

The container is the same — `<id>.jar` beside `<id>.msd`, the descriptor
naming a main class — and only the class inside differs. Three local archives
name one that extends `org.kwis.msp.lcdui.Jlet`: they are WIPI Java programs
distributed through this vendor's channel, and they stopped at
`class not found: org/kwis/msp/lcdui/Jlet` before anything ran.

**The surface is a layer over MIDP rather than a second runtime.**
`internal/api/wipi` declares each WIPI class as a subclass of its MIDP
counterpart — `Jlet` a `MIDlet`, `Card` a `Canvas`, and `Display`, `Graphics`,
`Image` and `Font` the classes of those names — so the display path, paint
scheduling, key delivery, pixels, fonts and save boundary are those already
built for the other eighty-eight archives. The WIPI classes add what the
standard has and MIDP lacks, and the platform adds the half a class definition
cannot carry.

The specification says each of them extends `Object`. That difference is not
observable in this corpus — no archive casts or tests one of these against a
MIDP type — and the alternative is a second display path with its own paint
scheduling and frame. A title that does test one is the reason to revisit it.

Four things had to be decided rather than inherited:

- **A Jlet session stamps the WIPI class name on the objects it hands out.**
  `invokevirtual` resolves on the receiver's class, so a Card's own code
  calling `Graphics.setAlpha` on an object named
  `javax/microedition/lcdui/Graphics` would not resolve it. The name the
  screen Graphics, an Image and a Font carry is decided once, from the
  application class; a MIDlet session is unchanged.
- **A method that forwards to the same-named MIDP method forwards specially,
  never virtually.** Such a method is an *override*, so virtual dispatch lands
  back on itself. Three of them did, and the failure is a host stack overflow
  several thousand frames deep rather than anything a title reports.
- **`Display.pushCard` shows the card before it returns.** MIDP's `setCurrent`
  is a request the next Host pass applies, and one title pushes its card from
  inside `startApp` and then spins on `isShown` until the card is up, on the
  same thread that would have to leave `startApp` for the pass to happen.
  Deferring there is a hang rather than a frame's delay. The paint is still
  deferred.
- **A Jlet is active before its `startApp` runs.** MIDP says a MIDlet becomes
  active when `startApp` returns; the WIPI specification says explicitly that
  a Jlet is active from the moment it is created, and the same spin depends on
  it.

**One MIDP rule had to give way for all three.** An anchor names where a point
sits against what is drawn, in two halves, and MIDP requires exactly one bit
from each. A WIPI title draws its title screen with `LEFT` alone: the handset
filled in the other half, and refusing it ended the title on its first image.
An omitted half now takes its default, the reading the zero anchor already
had. Naming *two* bits from one group is still refused, because that requests
two places at once rather than leaving one unsaid.

All three reach their title screen, and once given keys they can read (the
section after next), all three reach play. What the three still want from this
surface, and how each is answered, is in the package comment of
`internal/api/wipi` and in `internal/platform/skt/wipi.go`. Two things there are
declared and not built, for the reasons this project applies everywhere:
`org.kwis.msf.io` has no radio behind it (`network.md`), and
`Graphics.setAlpha` keeps its blend factor and reports it back without drawing
with it — honouring it means blending every primitive rather than only images
that carry their own alpha, and all three titles set it once, to the opaque
value.

<a id="implementation-the-key-codes-a-jlet-reads-are-not-the-ones-a-midlet-reads"></a>

#### The key codes a Jlet reads are not the ones a MIDlet reads

All three reached a title screen and none answered a key. A Card receives keys
through `keyNotify(type, key)`, which this layer bridges from MIDP's three
callbacks, and the bridge forwarded the code unchanged: this vendor's MIDP
device codes, 141, 142, 145, 146 and 148 for the pad and fire. No Jlet compares
against those.

**The specification declines to fix the values.** `EventQueue` says only that
an ITU key arrives as its ASCII value, that a control key arrives negative,
that the numbers differ between handsets, and that a title should therefore
ask `getGameAction` rather than compare. The numbers had to come from the
titles.

**Two of the three answer, and they agree.** One keeps a key map as a resource,
one table per handset, and its SKT table is a list of byte pairs: the ASCII
digits, `*` and `#`, and then `1`, `2`, `5`, `6`, `8`, `90`, `92` and `99`,
which are `EventQueue.UP`, `LEFT`, `RIGHT`, `DOWN`, `FIRE`, `SOFT1`, `SOFT3`
and `CLEAR`. The other switches on the received key over the same set, and
calls `getGameAction` only in the branch where the code is negative, which on
this handset it never is. This vendor answered the specification's "different
on every handset" with the game keys themselves.

A Jlet session therefore translates once, where a Host key enters it
(`wipiKeyOfDevice`), and `Display.getGameAction`, `getKeyCode` and
`getKeyName` use the same vocabulary: a pad code *is* its game action, so
naming one answers itself, and the ITU keys a handset lets stand in for the pad
answer the key they stand in for. A MIDlet in the same container is untouched
and still reads this platform's MIDP codes.

**Two more properties of a Jlet's keys.** Its soft keys are keys: MIDP diverts
SOFT1 and SOFT2 to a Displayable's commands because a MIDP Canvas has no other
way to reach them, whereas a WIPI Card reads them itself — one of the three
switches on 90 beside the pad. And a Jlet's `keyNotify` returns whether to pass
the event down, but nothing here has a lower Card to pass it to.

**All three reach play.** One is an isometric strategy game with its map,
resource counters and build cursor; one is a vertical shooter in its first
mission; one is a farming game in its opening scene, having passed its main
menu and save-slot screen. None of them reaches sound or saving within the
ticks these runs cover.

**One is drawn for a smaller handset than it is given.** The shooter lays out
its playfield from the reported screen — the HUD sits on the bottom edge and
the play area is centred — but tiles its scrolling background at its authored
size, so on the 240x320 default the terrain covers the upper two thirds and
the rest of the field is black. At `-screen 176x220` it fills. The archive
declares no width: its resource names end in `_0` and `_1`, which are frames,
not widths, so the rule in "The handset a title was packaged for is in its
resource names" correctly declines them. It is playable either way, which
separates it from the archive that rule was written for.

<a id="implementation-the-class-library-again"></a>

#### The class library, again

Two titles stopped on a standard library member this runtime lacked —
`java.util.Calendar.setTimeZone` inside a guest thread, and
`java.lang.StringBuffer.deleteCharAt` inside `startApp`. The scan lists the
rest on paths no local run takes, and those are answered rather than watched:
`StringBuffer.getChars`, the static `Byte.toString(byte)`,
`Thread.activeCount`, `PrintStream.println(byte[])` — this vendor's rather
than the standard library's, which prints the handset's own text rather than
an array's identity — and `java.lang.OutOfMemoryError`, which nothing here
raises and one title catches by name.

**Two are named and still not built.** `Thread.join(long)` needs a wait that
must not hold a Host pass, and no local title reaches it;
`java.lang.System.exec(String[])` is this vendor's way of launching another
archive, which is a download this project does not perform — `network.md`
records the same decision for the rest of the radio.

<a id="implementation-two-members-a-paint-stopped-on-reported-from-a-phone"></a>

#### Two members a paint stopped on, reported from a phone

Two archives ended a run inside `Canvas.paint` on a member this runtime did
not declare. That is the worst place for a gap, because resolution walks the
chain to `java.lang.Object`, and the message a player sees names `Object`
rather than the screen that would not draw:

- `javax.microedition.lcdui.Graphics.setGrayScale(int)`, MIDP's grey as one
  number instead of three. Three archives reach it, two through their own
  Graphics facade that also wants `getGrayScale`, the stroke style and the
  colour components — those three are still open, and their titles need ten
  more members besides. This one is `setColor(v, v, v)` with the
  specification's `IllegalArgumentException` outside 0..255.
- `com.xce.lcdui.XDisplay.clear(Graphics, Image, int, int)`, described under
  "What drawImageEx is" above.

A player found both, although the scan had listed them: the scan says what a
title *would* request, and neither title requests it until it is deep enough
into its own start-up to paint.

<a id="implementation-a-title-that-draws-in-three-dimensions-and-the-package-called-m"></a>

#### A title that draws in three dimensions, and the package called `m`

One archive's Canvas dies in its own class initializer on
`class not found: m/V3`. Three classes in a package named `m` — a vector, an
affine transform and a renderer — are this vendor's binding of the handset 3D
middleware of the era: `.mbac` for a model, `.mtra` for a motion, a `.bmp` for
the skin.

**The maths is answered and the renderer is not**, which is where
`com.skt.m3d.Graphics3D` has stood since it was written. `V3` and `A3` are
arithmetic a title uses for its own geometry: it composes a camera, transforms
a point, and reads three integers back to project it. `XO_World` is the half
that would need a rasterizer and the two model formats; it keeps its state and
draws nothing.

**The fixed point is the title's own arithmetic rather than a guess.** Its
perspective is `4096 * sin(fov/2) / cos(fov/2)`, and it divides the product of
a transformed coordinate and that value by 4096, so one is 4096 here. The
field of view it requests is 512, which is 45 degrees on a circle of 4096, the
second half of the convention read from the same two lines. Both are pinned by
`TestMicro3DTrigonometryIsFixedPointOverAFourThousandNinetySixCircle`, and the
composition by the two tests beside it: a quarter turn twice is a half turn
rather than a transform four thousand times too large, which is what a missing
renormalization would produce.

The title reaches its title screen but does not draw its world, which reads the
same way `Graphics3D` does: a rasterizer this project has not built, not a
feature that half works.

<a id="implementation-twelve-archives-in-the-skt-corpus-use-another-execution-format"></a>

#### Twelve archives in the SKT corpus use another execution format

SGS execution has since been added; see [sgs.md](../sgs.md) for the current scope.
The investigation below records the earlier Java-only limitation.

They hold a `.mod` or an `.inf` beside a `.SGS`, and the `.mod` identifies them
in its own header: `application/x-gnex-sgs`. An `.inf` beside one carries the
string `GVM`. That is this vendor's *other* runtime — the script platform that
ran beside the Java one on the same carrier — and it is not a MIDlet JAR, so
nothing here claimed the archive.

**Those twelve are most of what no platform claims, but not all.** A sweep over
every zip under the games tree leaves fifteen unclaimed, and the other three
are not a platform question: two are the same bag of games (a zip whose
entries are three whole archives, which `ArchiveOfArchives` already reports in
its own words) and one is a RAR with a `.zip` name, which `ContainerError`
already names. The message "the archive is not a KTF, LGT or SKT game"
therefore reached exactly the twelve script archives.

<a id="implementation-what-building-the-script-platform-would-cost-and-what-answers-the-questions"></a>

#### What building the script platform would cost, and what answers the questions

The first estimate assumed the whole instruction set and platform surface
would have to be recovered from the images, because no specification for
either is published, and searching in English and Korean still finds none.
**That premise was wrong, and what disproves it ships inside the archives.**

**Seven of the twelve carry the vendor's own PC player**, one file, byte
identical across all seven (1,056,768 bytes, SHA-256 `755666796891c415…`). It
is an unpacked PE32 x86 build with its strings in the clear, and it identifies
itself as a 2x emulator for this script format, naming the format in its own
file dialog. That is the original runtime, which this project may consult for
behaviour and file formats. It stands in for a specification: where a single
caller cannot settle a contract, the runtime that shipped with the game can.

A `strings` pass already yields the loader's vocabulary. It refuses a bad
module with messages naming the header's fields —
`Open: Application Size error`, `Open: Script LCD class error`,
`Open: Script audio type error`, `Open: Out of SSB memory error`,
`Open: Out of SSB size error` — beside the field names: `ScriptSize`,
`ScriptType`, `ScriptID`, `ScriptCPID`. A header field's meaning is therefore
a cross-reference away rather than a guess from twelve samples.

The containers, measured across all twelve:

- **The `.mod` is a length-prefixed string table**, not a binary header: a
  32-bit count before each string, carrying the content type
  (`application/x-gnex-sgs`), the body's extension (`SGS`), a version
  (`1.0.0`), the carrier's download URLs, and the word `EXECUTABLE`. The
  `.inf` the other two carry is a different, tighter shape with `GVM` and two
  dotted-quad addresses.
- **The `.SGS` header is one shape across eleven of the twelve**, and the
  game's title sits in it in EUC-KR at offset 10, so the loader can name a
  title before decoding anything. The twelfth begins `0x20` and then zeros,
  a different or obfuscated revision; it is the sample to examine last.
- The bodies weigh 125,022 to 131,022 bytes, a program budget rather than a
  truncated download, and the two `.inf` archives declare a byte count that
  matches their `.SGS` exactly.
- The body is neither ARM nor Thumb — the byte census in the first pass
  settles that — so the ARM core does not help.

**The interpreter had not been located, and the first guess was wrong.** The
two biggest switches in the binary are not it: the one at `0x402cae` takes a
dword argument over the range 12..190 and folds it into 25 targets through a
byte index table, and the cluster of nine around `0x4124ff` belongs to one
function that switches on a dword read from a structure. Both are the PC
shell's own command and event handling. The interpreter dispatches on a byte
read from a script pointer, and neither does.

The best standing candidate was a **table of 279 consecutive code pointers,
266 of them distinct, at `0x4949d8` in `.data`** — statically initialised, far
too large to be a vtable, and with no instruction in `.text` naming its address
directly, as expected for a table indexed through a base register. Whether it
was the opcode table, the platform call table, or both was unanswered.
Superseded: see [sgs.md](../sgs.md#evidence-and-current-limits).

The order of work, if this were picked up:

1. **The container and the header**, from the loader's error strings backwards:
   they name the fields, and cross-referencing each string finds the code that
   checks it. A dump tool comes out of this, as `ktfdump` did for KTF, and
   nothing can be read without one.
2. **The dispatch loop**, from the loader's exit: whatever the loader hands the
   body to is the interpreter. Finding the entry is worth more than any number
   of table censuses from outside; the two above were both wrong.
3. **The opcodes**, one at a time from the handlers the loop reaches.
4. **The platform surface** beneath them — screen, keys, sound, storage — the
   same job the two WIPI platforms needed and the part this project already
   knows how to shape.

That is still a whole platform. For scale, `internal/platform/ktf` was about
32,000 lines beside 17,000 of tests and `lgt` about 21,000 beside 11,000, and
neither had to build an instruction set. The player changes the shape of the
work rather than its size: **it has no dead ends.** The expensive part of the
two WIPI platforms was never the typing but the stretches spent guessing a
contract from one caller and telling the guesses apart by A/B. Here every
question has something to ask.

The original decision was to defer this runtime. The user superseded it on
2026-09-11; the first executable SGS path and its remaining limitations are
documented in [sgs.md](../sgs.md).

<a id="implementation-deliberately-incomplete"></a>

### Deliberately incomplete

- **`XO_World` has no rasterizer**, for the reason `Graphics3D` below has none.
  See "A title that draws in three dimensions" above: the vector and transform
  arithmetic beside it is real; the two model formats and the drawing are not.
- **`SISImage` does not decode.** The container's frame and object tables are
  not documented anywhere available here, and no SKT archive in this repository
  could be used to reverse them, so `getFrame` and `getObject` answer null and
  the paint methods do nothing. Answering a blank image instead would let a
  game draw nothing while believing it drew a sprite. The reference
  implementation is stubs for the same reason.
- **`Graphics3D` has no rasterizer.** It keeps and reports its render state
  truthfully; `Object3D` keeps a real mesh and a real transform (the matrix a
  game reads back after `translate`/`rotate`/`scale` is correct), but nothing
  draws it.
- **`SMS`, `Call`, `PhoneBook` have no radio.** Reads answer empty and sends
  are refused. Reporting a delivered message a game can never receive a reply
  to is worse than reporting none. `docs/network.md` carries the same decision
  for the MIDP connection framework and the other platforms.
- **The input method types Latin and digits, not Hangul.** Both of this
  vendor's text surfaces take the keypad — see "Two ways a title takes a name"
  — with the multi-tap editor every text field in this runtime shares. A
  handset's input method also composed Hangul from the jamo on the keypad, and
  nothing here does: a title that wants a Korean name gets Latin letters and
  digits in the same field. It is the same gap the MIDP `TextBox` and KTF's lwc
  text components have, because it is the same editor.
- `BackLight` and `Vibration` keep their state and report it back; there is no
  hardware to drive.
- **A MIDlet takes no touch.** `pointerPressed` and its siblings are a `Canvas`
  surface this runtime does not offer, so a `pointer` message from a Host is
  answered with `ErrNoPointer` and dropped. The MIDP path is a keypad path.

An item that was incomplete here and no longer is had a common shape: the
runtime had the mechanism and no Host called it.

- **`Runtime.Pause` and `Runtime.Resume` are driven now.** They existed with a
  full `StatePaused` transition and `notifyPaused` behind them, and nothing
  above ever called either, so a MIDlet whose page went away was frozen without
  being told and came back to a clock that had moved. A server parks on a
  dropped socket and runs the pair around it; see
  [`session.md`](session.md#implementation-server-sessions), "The game is told it was parked".

<a id="implementation-validation"></a>

### Validation

The local corpus has its own opt-in probe —
`WFEATURE_SKT_ACCEPTANCE=1 go test -run TestLocalSKTArchivesBootAndPaint
./internal/platform/skt` — which starts all fifteen archives, ticks each for
300 ticks and requires a frame with something lit in it. All fifteen pass.
[`testing.md`](testing.md#implementation-testing) has what it does and does not claim.

<a id="implementation-an-skt-ab-has-a-per-title-noise-floor-and-it-is-not-zero"></a>

#### An SKT A/B has a per-title noise floor, and it is not zero

Driving all fifteen archives through the same script under two builds and
diffing the captured frames is the regression net for a change to shared code.
Like the KTF sweep, and unlike LGT's, whose route replays byte-identically, it
is only readable against a floor measured first. At 300 ticks per archive,
**eleven of fifteen come back byte-identical and four differ**, and running the
*same* binary twice gives those four the same differing-pixel counts as two
different builds. The floor is per title, not per corpus: one title disagrees
with itself over roughly two hundred pixels a frame while another stays under a
hundred, so the number that matters is the one measured base-versus-base for
*that* archive before anything is read as a finding.

Evidence is what counts on KTF: a title that stops where the other build did
not, an error text that changes, or a difference far enough outside that
title's own base-versus-base count to be a layout rather than an animation
phase. See "An A/B sweep of the local set is half noise" in
[`ktf.md`](ktf.md#implementation-ktf-platform-implementation-status) for why a
differing frame count carries so little on its own.

There is still **no SKT game archive in this repository**, so CI tests nothing
against a real title — only against a newly authored fixture
(`internal/platform/skt/testdata/skvm.jar`) that exercises each surface and
checks the returned values. The fixed-point scale, the file semantics and the
pixel operations are what a real game would notice first if wrong, so the
tests pin those.

The frame-loop contracts above were found the other way round — by running
local titles with `runskt` and reading the frames — and each is pinned by a
fixture test afterwards: `TestCallSeriallyRunsOneRunnablePerPass` and
`TestCallSeriallyLoopAdvancesOneStepPerPass` for the serial queue,
`TestScreenGraphicsStaysUsableAfterPaint` for the Graphics that outlives its
paint, `TestXDisplayPublishesFramebufferSize` for the screen fields, and
`TestCanvasReportsTheDisplayLessTheReservedRows` for the sixteen rows a Canvas
reserves beside them.

Regenerate the fixture as follows. `$stub_dir/classes` is the signature
classpath from
[`testing.md`](testing.md#implementation-compiling-a-java-fixture); build it
once per session.

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-skvm-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -g:none \
  -cp "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/SKVMMIDlet.java
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/SKVM.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/skvm.jar" META-INF/MANIFEST.MF SKVMMIDlet*.class)
cp "$fixture_dir/skvm.jar" internal/platform/skt/testdata/skvm.jar
```

The class library is declared in `internal/api/skvm/definitions.go` and
installed by `skvm.Define`, so changing it is a Go change with nothing to
rebuild. The WIPI Java surface has the same shape one package over
(`internal/api/wipi`), with `TestEachClassExtendsItsMIDPCounterpart` holding
the chains the design rests on and `TestAnchorFillsInTheHalfATitleLeavesOut`
holding the one MIDP rule it relaxed.


<a id="followup-skt-compatibility-follow-up-2026-09-12"></a>

## Compatibility follow-up (2026-09-12)

This note separates declarations found in archives, calls observed during a
bounded run, and behavior reached through a Host boundary. The local set on
this date contains 15 SKT archives. All probes used fresh save directories
under `/tmp`; the game library and user save tree were read-only.

<a id="followup-cldc-data-stream-interfaces"></a>

### CLDC data stream interfaces

The published CLDC contracts declare
[`DataInput`](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataInput.md)
and
[`DataOutput`](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataOutput.md)
as interfaces, and declare that
[`DataInputStream`](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataInputStream.md)
implements `DataInput` and
[`DataOutputStream`](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataOutputStream.md)
implements `DataOutput`. The interfaces include the floating-point methods
added in CLDC 1.1.

The runtime already had the concrete integer, string, and array stream
operations the local caller uses, but omitted the two interface class
definitions and the streams' interface metadata. `invokeinterface` happened to
dispatch by receiver class, while `instanceof`, `checkcast`, and API scans
could not see the relationship. The correction adds the complete CLDC 1.1
interface declarations, marks both streams assignable, and supplies the four
floating-point stream bodies needed to make the declarations truthful.
`writeFloat` and `writeDouble` use the specified `floatToIntBits` and
`doubleToLongBits` forms, including canonical NaN encodings.

One local archive, SHA-256
`06c7a38343c7c48ad8ef362ffe1cd5ba2384a0b4e1cc8111acb6e2d72bf7b5e6`,
contains 111 `invokeinterface` instructions across five classes. They call
`readFully`, `readBoolean`, `readByte`, `readShort`, `readInt`,
`writeBoolean`, `writeByte`, `writeShort`, and `writeInt`. Two input-driven
debug traces establish both sides of that link surface:

| Route | Actual caller evidence |
| --- | --- |
| Packaged-data load | `CLEAR` at tick 60 and `FIRE` at tick 100 reached gameplay by tick 360. The trace executed 5,318 `invokeinterface` calls through `DataInput`: 3,909 calls to `readShort` from an array helper, 134 calls in one object reader, 38 primitive-field call sites each executed 33 times in another reader, six calls in a third reader, and 15 in a fourth. These are reads from packaged game data, rather than save-file reads. |
| Options write | With each key held for 12 ticks, `CLEAR` at tick 60, `DOWN` at tick 120, and `FIRE` at tick 180 opened Options. `FIRE` at tick 400 opened the selected value, `RIGHT` at tick 520 changed it, and `FIRE` at tick 640 applied it. The apply method ran once, called a `DataOutput`-typed array helper 10 times, and executed 50 interface calls to `writeShort`. It then called the vendor file write once and produced a 295-byte options file with SHA-256 `b6ab74a001a469246c7af5f030e94384d8c3652f0dee5b71084eed8a1fdf9847`. |

Neither trace logged a warning or error, so no further stream implementation
was warranted. The only missing-class diagnostic was guest class `i`: the
archive contains guest classes `a` through `h`, and its startup deliberately
tries each name through `i` with `Class.forName` and catches `Exception`, so
that diagnostic records a handled preload miss rather than a platform API gap.
The review artifacts remain under `/tmp/wfeature-skt-api-route.cs0DHI/`.

Repository-authored coverage has both layers this JVM change requires. The
class fixture passes streams through interface-typed methods, checks
assignability, round-trips every primitive category, and supplies
noncanonical NaNs from Go to verify their serialized bytes. A packaged MIDlet
fixture is opened by the SKT archive loader and performs the interface round
trip from `startApp`.

<a id="followup-save-progression-and-sound-observations"></a>

### Save, progression, and sound observations

A fresh 300-tick `runskt` probe attached diagnostics, an audio recorder, and a
separate empty save root to each of the 15 archives. All 15 remained active and
painted a lit frame.

Every archive executed a storage API during this window: six used MIDP RMS and
nine used the vendor file API. Five archives made actual record or file writes
and produced 12 files in their isolated save roots; the other calls were
existence checks, opens, closes, or reads. A file created during boot may hold
defaults, settings, or authentication state, so these writes do not prove that
play progress was saved. The probe sent no menu or gameplay input and did not
perform a second launch over the files it created.

A separate restart probe used archive SHA-256
`0ed66634d3dce7eeb05ec9ca95488f507574a1c8b62f3446018cecbd5c4d2415`.
The first 300-tick process started with an empty save root and added one RMS
record. The encoded store was 481 bytes: an eight-byte record header followed
by a 473-byte payload with SHA-256
`8f7b7c4436343c7a942dddbfd1d2be15668d20112d918e5d5426ef6515224d0a`.
A second process started a fresh runtime over the same root. Its diagnostics
recorded one `getNumRecords` and one `getRecord(1)`, with no `addRecord` or
`setRecord`; the save checksums remained unchanged.

The caller and trace establish that the bytes were consumed rather than merely
found. Static disassembly of the caller's `d.ad()` shows that a zero record
count builds a default string and calls `addRecord` at bytecode PC 167. The
fresh trace took the conditional at PC 34 to PC 37 and reached PC 167. The
restart trace took PC 34 to PC 174, called `getRecord` at PC 180, constructed
a String from the returned byte array at PC 183, and then parsed delimiters and
assigned the resulting values to guest fields from PC 218 onward. This proves
write, fresh-runtime reload, and guest consumption through RMS. Both runs
produced the same opening frame digest, so the record is initial/default
state; it does not prove restoration of user-visible gameplay progress.

An input-driven route over the same archive closes that progression gap. It
advanced from a fresh start through the introduction to a stable map screen,
using `FIRE` every four ticks from tick 30 through tick 1186. Later repeated
presses only reopened a nearby interaction, so the map checkpoint, rather than
the number of input events, is the progression evidence. The route then opened
the title's own menu with `CLR`, selected its sixth entry with five `DOWN`
presses and `FIRE`, then selected slot 1 and confirmed the write with the `1`
key. The save result and the independent reload:

| Stage | Evidence |
| --- | --- |
| Before the title's save action | The initial/default RMS file was 481 bytes with SHA-256 `d2e1b6611d5cb4b6a648814502f3070a282fb608f49c1f10c9d11d8b6bdb673a`. |
| After the save confirmation | The title displayed its saved confirmation; the RMS file grew to 519 bytes and changed to SHA-256 `20f57e639370e32154b25d87b7bcd5fb18fb1d7562e3508404c02fcf126eab5b`. |
| Fresh runtime | A separate process sent `FIRE` at ticks 30 and 60 to open Continue, then `1` at tick 90 to select slot 1. The slot displayed the saved level and location; loading it called `getRecord(1)` once, did not call `setRecord`, and left the RMS checksum unchanged. |
| Restored checkpoint | The reloaded map PNG was byte-for-byte identical to the pre-save checkpoint. Both files have SHA-256 `75c83c7911ea0f8342cbf8e9f41329ecb4f79ff9dbcee122dd0482e8e6ed9e36`. |

The raw route screenshots, diagnostics, save data, and checksum files remain
under `/tmp/wfeature-skt-progress-route.2v12cT/` for this review. The result
proves visible story progress, a title-initiated save, a new process consuming
that save, and restoration to the saved map rather than the fresh-start story.

All 15 archives requested a vendor audio clip. Thirteen proceeded through
`open` and `play` or `loop`, and all thirteen produced MIDI messages at the
Host audio sink; four also produced a WAV recording with sampled audio. The
remaining two requested a clip but did not open or play one within 300 ticks.
These results are playback-path evidence through the runtime timeline and
sink. The separate decoder sweep only proves that packaged sound resources can
be decoded; neither result establishes that a person heard sound from a
browser or physical phone.

<a id="followup-jlet-coverage"></a>

### Jlet coverage

No class in the 15-archive set of this date extends
`org/kwis/msp/lcdui/Jlet`; their application paths are MIDlets. The repository
fixtures cover the Jlet lifecycle, cards, vendor files, and event queue as
separate contracts, but this corpus cannot supply a real Jlet save or gameplay
route. The Jlet part of the acceptance item needs a Jlet archive in the local
library before it can be measured; its first run should use a fresh isolated
save and the same write, restart, restore, and audio checks above.

<a id="followup-landscape-evidence"></a>

### Landscape evidence

The descriptors contain no display width, height, or orientation key. The
resource-name rule selects one archive in this set as `176x208` and none as
`320x240`. Static bytecode in all 15 archives references screen width or
height; during the 300-tick probes, 12 called the width getter and 10 the
height getter. Those calls show that titles can adapt to the dimensions a Host
supplies, but they do not identify a landscape package: this set has no paired
`320` by `240` branch, orientation string, or consistent resource-name
declaration.

The CLI already accepts `-screen 320x240`, so the next investigation needs no
new framebuffer shape. It needs one of the packages reported to support
landscape. Once available, record its hash, inspect its descriptor, resources,
and the bytecode branch that consumes screen dimensions, then run the same
package at `240x320` and `320x240` with isolated saves. Automatic selection is
warranted only if the package carries a stable declaration that distinguishes
it from portrait packages.

<a id="followup-native-keyboard-and-ime-boundary"></a>

### Native keyboard and IME boundary

The MIDP
[`TextField`](https://mirusu400.github.io/wipi-wiki/midp/java-api/javax/microedition/lcdui/TextField.md)
contract makes the field limit and constraints authoritative. The SKT Host
adapter exposes three targets whose contents and focus are observable: the
current `TextBox`, the selected `TextField` in the current `Form`, and a
focused `XTextField` owned by the visible canvas. A commit checks the same
object, display, selection or focus, original value, maximum size, and
constraints before replacing text. An open command menu hides the editor from
the Host and invalidates an earlier snapshot. The adapter enforces input
constraints, counts the maximum in Java UTF-16 `char` units, repaints the
target, and reports a Form item-state callback. A dedicated text-state lock
serializes these operations with background guest setters and screen paint.

A title-owned `com.xce.lcdui.TextComponent` exposes editing operations and a
size, but no operation that reads its characters, so the Host cannot form an
honest initial-value snapshot or compare a pending composition with the
current value. That target remains unavailable through native IME input until
caller evidence supplies a safe readable value or an observed vendor method
extends the interface contract.

The packaged TextBox acceptance fixture passes the complete WebSocket path in
automated Chromium and WebKit runs: open, Korean commit, reopen, stale target
rejection, UTF-16 length rejection, and value retention across a pause/resume
cycle. A person still needs to enter Korean with an actual phone keyboard and a
desktop IME to verify operating-system composition, focus, and the absence of
unwanted browser zoom or layout movement.

<a id="followup-remaining-acceptance"></a>

### Remaining acceptance

The automated gameplay save and fresh-runtime restore route is complete. Four
requirements still depend on human observation or on archives absent from the
corpus of this date:

1. Listen through the browser and a phone. Decoder output, MIDI messages, and
   sampled-audio buffers prove the software path but cannot establish what a
   person hears from the operating system and speakers.
2. Enter Korean with an actual phone keyboard and a desktop IME, checking
   operating-system composition, focus, and page layout behavior.
3. Add a real Jlet archive to the local library and repeat the save, restart,
   restore, audio, and input route. The corpus contains no Jlet application
   path.
4. Supply a package reported to support landscape and compare it at `240x320`
   and `320x240`. The corpus has no stable landscape declaration from which
   automatic selection can be derived.

<a id="gameplay-qa-2026-09-20"></a>

## Gameplay QA, 2026-09-20

This investigation starts from revision `5b00ae17cdb867e86e2bfc6aac8093d872c4217d`
and the locally retained September 19 server/page reports. Five reported paths
remain distinct: SKT unpaced gameplay, SKT character-start failure, SKT tutorial
failure, LGT initial network agreement, and an LGT slowdown after selecting
the medium in-game speed. Passing one path does not establish the others.

<a id="gameplay-qa-2026-09-20-skt-separately-installed-resource-archives"></a>

### SKT separately installed resource archives

The character-start report ends in `System.arraycopy` with a null source.
The caller loads map data from a separate JAR when `getResourceAsStream`
does not find it in the primary JAR. The outer package contains that data.
Two runtime defects prevented access: the container loader dropped every
`.jar` beside the primary payload, and the two-string `XFile` constructor
treated the entry name as a textual file mode, an interpretation the old
implementation had inferred, whereas the observed caller passes an archive name
and its internal resource path.

The loader now retains secondary JARs as installed files without adding their
classes to the primary class path. `XFile(String, String)` opens an entry in
the named archive, read-only. Integer-mode constructors keep their existing
save behavior. Archive reads reuse the bounded JAR parser, including entry
count, input/expanded size, duplicate and path checks. The entry may have a
single leading slash; missing, damaged and unsafe resources fail with
`IOException`. Original archive bytes are preserved.

An authored outer container packages the existing Canvas MIDlet fixture with
a newly generated resource JAR. Tests cover both path spellings, the entry
named `r` (which must not become a file mode), actual guest reads, rejected
writes, missing resources and unsafe paths. API tests check forwarding of both
constructor arguments.

For local archive SHA-256 prefix `1f0d7e81336a`, the page's final session inputs
were converted to press/release events at 16 ms ticks. Separate empty save roots
were used. The unchanged CLI fails at tick 368 with the reported null-array
stack. The corrected CLI completes the same route plus 300 observation ticks:
674 ticks, active state, no error, 62,371 non-black pixels, and a visible opening
scene with characters and dialogue. Local artifacts are
`build/qa-case1.route`, `build/qa-case1-{baseline,fixed}.log`, and
`build/qa-case1-{before,after}.png`. This proves entry past the reported
failure, not campaign completion or progress restoration.

<a id="gameplay-qa-2026-09-20-skt-refresh-driven-speed"></a>

### SKT refresh-driven speed

The reported fast loop repeatedly calls `Thread.yield` and `XDisplay.refresh`
without a gameplay sleep. Scaling the guest clock and `Thread.sleep` therefore
does not limit its iterations. Slowing Host ticks only reduces delivered frames;
the independent guest thread continues running at interpreter throughput.

`XDisplay.refresh` now paces its producer to at most 60 refreshes per guest
second. This is an emulator policy, not a measured handset refresh contract.
The existing speed clock scales that bound. Work already spent drawing counts
toward the interval, late frames do not accumulate credit, and waits observe
live speed changes. Rendering and presentation locks are not held while waiting.
Terminal states release waits without another Host tick. Frame snapshots and
coalesced Host presentation retain their existing ownership.

The regression test previously completed two 0.25x refresh intervals in
4.25 microseconds. It now requires at least 120 ms and also exercises changing
the live speed; a separate test covers shutdown without Host ticks. The existing
clock tests retain deterministic coverage of each speed multiplier.

A local replay of the reported archive's first 30 seconds of input then measures
three consecutive intervals in the same live runtime:

| Selected speed | Refresh calls | Wall interval | State |
| --- | ---: | ---: | --- |
| 1x | 175 | 3.011 s | active |
| 0.25x | 45 | 3.006 s | active |
| 2x | 311 | 3.002 s | active |

The counter measures guest calls, not browser frame delivery. Higher speeds
remain limited by execution cost. The local probe and results are
`build/qa_speed_probe.go`, `build/qa-speed-events.json`, and
`build/qa-speed-result.log`. This establishes that the selected speed reaches
the producer; perceived handset fidelity and real-phone rendering are separate.

<a id="gameplay-qa-2026-09-20-skt-tutorial-reproduction"></a>

### SKT tutorial reproduction

Archive SHA-256 prefix `d3e3b16cefd0` reproduces the reported exception with
292 press/release events from the retained page session. The isolated run
fails at tick 4,652, after approximately 75.6 seconds, at the same drawing
instruction: `b.try(Graphics)` bytecode offset 102 reads index 9 from a row
of length 5. Local artifacts are `build/qa-case2-events.json`,
`build/qa_tutorial_probe.go`, and `build/qa-tutorial-probe.log`.

A VM store observer tracks the shared `h.w` name table and menu state. The
ability-name loader installs one row of 24 entries. At 67.673 seconds the
item loader replaces it with 28 rows of five entries. At 75.594 seconds an
equipment operation returns to menu state 6; the subsequent tutorial overlay
reads the retained item table as ability names. Its lazy loader checks only
whether the shared table is null. The table is non-null, so the required
24-entry resource is not loaded.

In the main loop, key handlers queue input, the guest worker consumes it
before updating and drawing, and `paint` only supplies the graphics object, so a
concurrent Host paint replacing the name table does not explain this trace. Do
not suppress the bounds exception or fabricate a larger array.

An independent OpenJDK 8 replay restores the failing object graph and invokes
the original drawing method. It throws the same index-9 exception on its first
frame, through the same guest drawing methods. Clearing only the name table
allows 1,000 frames with repeated confirmation inputs and leaves a one-row,
24-entry ability table. This validates the drawing/data-flow diagnosis; the
graph comes from the emulator and does not prove a complete independent
handset input route. The desktop shim preserves image dimensions but does not
render pixels. Local artifacts are `build/qa-case2-failure-openjdk.log` and
`build/qa-case2-failure-reset-openjdk.log`.

The tutorial's phase-17 block clears image resources and changes to phase 5,
but does not clear the item-name cache. Ordinary menu exit paths do clear it.
A diagnostic observer clearing it at that transition completes the original
292-event route and 5,500 ticks, active and without an exception. The production
compatibility adapter now makes this same correction for the exact class-set
digest `d70497e3b4fae79a2b552c0cdad418494e01faa282a29a2eb8b7c4a702e45347`.
It replaces the redundant phase-17 assignment with a null name-table store;
the block still sets phase 5 before its next method call. Branch offsets,
exception regions, stack limits and unrelated methods remain unchanged.
Four appended constant-pool entries provide the field reference. Original
archive bytes remain unchanged, and any class-set change disables selection.
This is an observed revision's guest-cache workaround, not a new JVM rule.

The production runtime without the diagnostic mutation completes 5,000 ticks
of the same route, remains active, and shows the next combat tutorial dialogue
and its world scene. Its observer records the null store followed by the
ordinary ability loader installing 24 names. Artifacts are
`build/qa-tutorial-production.log` and `build/qa-case2-probe.png`.
This passes the reported crash point, not the whole tutorial or campaign.
The authored `TutorialCacheMIDlet.java` JAR reproduces the stale-table bounds
exception, then proves lazy reload after the adapter and preservation of the
final phase. Tests reject truncated classes, wrong fields and offsets, and
verify unrelated method bytecode and original archive ownership.

<a id="gameplay-qa-2026-09-20-lgt-agreement-reproduction"></a>

### LGT agreement reproduction

The retained log directory has no matching original report, so this path uses
fresh isolated runs of archive `ecb1bf8d2079`. Both local package variants
contain the same module, SHA-256 prefix `41ed310172ee`. The completed route
selects the default explicit No at the customer-information agreement,
shows network progress, and eventually shows a server-communication error.
`build/qa-case3-network.route` completes 7,018 ticks; its log records
`netConnect` from guest address `0x1355c` with callback `0x13070`, followed
by two `netClose` calls. The baseline status is `unsupported`.

None of the eight existing Thumb notification patterns matches this ARM
implementation. Static inspection finds a different stream framing: the
builder formats `SMSAGREE %s %s %c`, and its sender includes one terminating
NUL rather than padding the command to 100 bytes. The agreement response
handler accepts codes 2 and 3; the stream decoder reads a signed command byte
and a payload-length byte. The socket callback is `0x131ac`. These are local
investigation addresses, not proposed recognition keys. A diagnostic overlay
injects only this module's routing contract into the existing in-process socket
service. The same 7,018-tick route then writes 32 bytes, reads a two-byte
response, and closes the socket normally. The final frame is an empty
informational dialog with an OK button, rather than the baseline's communication
error. A second fresh-save replay adds explicit confirmation and 300 observation
ticks. It
completes 7,324 ticks and reaches the title screen without the network error;
artifacts are `build/qa-case3-null-complete.{route,log,png}`. This confirms the
No path with the diagnostic adapter. Those diagnostic runs did not establish
explicit Yes, restart behavior or production module recognition, and left the
informational dialog blank. The final implementation and acceptance follow.

<a id="gameplay-qa-2026-09-20-implemented-arm-agreement-support"></a>

#### Implemented ARM agreement support

The production recognizer now joins the agreement initializer, writer and reply
handler through their virtual table, follows the constructed network object's
virtual table to the stream decoder, and validates dial/socket callback state
and transport links. It extracts the application and endpoint from the matched
initializer. Archive names, module hashes and fixed image addresses do not select
the adapter. Authored fixtures cover relocated code/data, mismatched links,
changed instruction windows and strings, ambiguous writers, truncated data and
address overflow. Unsupported compiler layouts retain the ordinary failure path.

The separate stream parser accepts only the current session's identity,
application and explicit N/Y choice, with a terminating NUL and a 64-byte bound.
It rejects oversized, trailing, unrelated and repeated commands. The response
uses code 2 for No or 3 for Yes, followed by the length and bytes of
`Handled locally. Nothing sent.` This replaces the diagnostic adapter's blank
dialog without claiming contact with the original service. Tests exercise every
split position, final-terminator handling, guest socket writes and short reads.

A release CLI built from the actual source, without diagnostic overlays, passes
both fresh-save choices. The final No route completes 7,324 ticks and the final
Yes route 7,330 ticks. Both reach the title screen after confirmation. The Yes
replay also captures the visibly selected Yes button and the readable local
notice. Traces show a 32-byte request, a two-byte response header and a separate
30-byte text read, then ordinary socket and network closure. Artifacts are
`build/qa-case3-arm-message.{log,png}` and
`build/qa-case3-arm-{yes-selected,message-dialog,final-yes}.png`, with the latter
route's log at `build/qa-case3-arm-final-yes.log`. Only isolated QA save roots
were used. A fresh runtime using the No route's saved data then completes
2,212 ticks and reaches the title without a dial or socket write. Both saved
files retain their SHA-256 hashes. Restart evidence is
`build/qa-case3-arm-restart.{route,log,png}`. This proves the reported initial
agreement and its immediate restart path, not campaign completion or any
external service functionality.

<a id="gameplay-qa-2026-09-20-outstanding-investigation"></a>

### Outstanding investigation

- The LGT medium-speed page report contains temporary drops, including 4.4 fps,
  57.1 ms per tick and 0.67x measured guest speed, followed by recovery toward
  0.96–0.98x. The server report is at 1.00x after recovery. These measurements
  do not identify the cause or establish a fix.

The final LGT session in the retained page report starts playing at
`2026-09-19T14:14:06.095Z`. Its archive SHA-256 is
`344e1285110a77089625b6d353c9766ecfd900763d7283ba0fd5eeeded07dcab`.
The page records two transient drops: at 14:14:16.425Z, 7.9 fps with 27.2 ms
ticks and measured speed 0.58x; at 14:14:56.997Z, 4.4 fps with 57.1 ms ticks
and 0.67x. Each recovers. This identifies the package and time windows to
replay; it does not yet distinguish guest loading work from a timer defect.

A full reading of the same page report finds a much more severe later window:
at 14:15:54.384Z the average tick is 228.6 ms and measured speed is 0.17x;
at 14:15:56.583Z it is 358.3 ms and 0.11x; at 14:15:59.143Z it is 635.6 ms
and 0.06x. It recovers to 0.97x by 14:16:05.207Z, with further intermittent
drops afterward. These later measurements, rather than only the early loading
dips, are the remaining investigation's target.

The module hash is
`a1467fcb4a07f8ed763b43c74fda41c9ce44566539cc21f51e1bbcaae296b696`.
A debug runtime replay with a fresh isolated save root delivers all 1,326
recorded key edges over 160 seconds without uncaught callbacks and does not
reproduce the severe window. Its captures show that the fixed timestamps reach a
main-menu confirmation instead of the speed option, so it did not reproduce the
setting change either. Logs, captures and guest profiles are
`build/qa-case4-full-*`, with input events at `build/qa-case4-events.json`.

A separate manual probe (`build/qa_lgt_manual.go`, using only
`var/savedata/qa-20260920/medium-manual/`, copied from the preceding replay's
save root) reaches the speed option. In the first navigation the first CLR
removes the system menu's selection and another CLR returns to play; shorter
holds do not change that. Later navigation verifies each screen and uses two
ticks held, two released, then 30 observation ticks. Menu-only timings do not
establish gameplay behavior after selecting medium.

- **High, combat tutorial.** The small selected glyph was first misread as
  medium; its pixel strokes identify it as high. A 300-tick real-time
  measurement completes in 7.685 s wall time, with 7.339 s of guest time,
  6.663 s busy, a 57.684 ms maximum tick and 796,673,754 instructions. This is
  neither the reported slowdown nor a medium-setting acceptance
  (`build/qa-case4-combat-high-option.png`, `build/qa-case4-combat-high.png`).
  An earlier adjacent-setting measurement is not labeled high, because its
  displayed value was not established.
- **Medium, first combat stage.** Navigation passes the tutorial; the full menu
  has Status, Skill, Item, Record and System tabs, unlike the tutorial's
  system-only menu, and first-use Item and Record explanations must be confirmed
  before tab navigation proceeds. Medium is selected and verified
  (`build/qa-case4-battle-medium-option.png`); after leaving the menu the
  battlefield stays visible with its minimap, enemies and weather. A 300-tick
  measurement takes 11.914 s wall time and advances 11.968 s guest time
  (about 1.00x), with 7.667 s busy, a 53.876 ms maximum tick and 912,392,079
  guest instructions (`build/qa-case4-battle-medium.png`; console measurements in
  `build/qa-case4-manual-evidence.txt`). This verifies that setting change
  without the reported transient; it does not explain or resolve the original
  episode.

After the probe closed, its ordinary save files were copied to the isolated
`medium-battle-checkpoint` QA save root, with hashes in
`build/qa-case4-battle-save-hashes.json`, for later runs.

The macOS CPU profile again attributes most samples to
`runtime.pthread_cond_signal`; the previously measured profiler limitation in
[ARM execution history](armcore.md#implementation-where-a-clets-time-goes-and-the-quantum-that-is-not-one-number)
applies. Do not infer a scheduler optimization from this profile alone. Guest
instruction samples and measured tick times remain the useful evidence.

<a id="gameplay-qa-2026-09-20-lgt-websocket-comparison"></a>

#### LGT WebSocket comparison

An isolated debug `webhost.Server` and real WebSocket connection repeat the
setting change using a copy of the first-stage QA save. This exercises the
shared session adapter, audio collector, PNG encoder, socket writer, key
dispatch and statistics used by the page. The existing user server is not
restarted or changed. The probe has its own ephemeral listener, game directory
under `var/games/.qa-web-case4/`, and saves under
`var/savedata/qa-20260920/medium-web/`.

The captured option screens explicitly establish high, then medium. Each
setting has a 12-second observation interval in the same first battle, with
enemies, weather and damage visible. Five complete two-second statistics
windows after the initial mixed window give:

| In-game setting | Measured guest speed | Mean tick cost | Delivered frame rate |
| --- | --- | --- | --- |
| High | 0.969–0.984x | 20.46–21.33 ms | 16.27–16.96 fps |
| Medium | 0.994–1.001x | 26.25–31.63 ms | 12.39–12.52 fps |

The transition window itself reports 1.013x and 21.943 ms per tick. No severe
drop appears immediately after selection or during the following interval.
Medium's lower presentation rate here accompanies an on-time guest clock;
it is distinct from the original report's 0.06x clock shortfall. The tick-cost
measurement brackets `session.Tick`; frame conversion, PNG encoding and socket
writing are outside that timed call. They can still compete for host resources,
so this timing boundary alone does not exclude host contention.

Artifacts are `build/qa_lgt_web.go`, `build/qa-case4-web.log`,
`build/qa-case4-web-comparison.json`, and the `high-selected`, `high-end`,
`medium-selected`, and `medium-end` PNGs under `build/qa-case4-web-*`.
The probe was stopped after observation. These results are a second
unsuccessful reproduction through the actual host path; they neither identify
nor fix the historical transient. The original report lacks a code revision and
a slow-tick execution profile, and the user service then running reported only
version `dev` and had started after that report, so it cannot establish the
historical executable either. Further diagnosis needs a reproduced slow state or
more evidence from that episode, not a speculative timing change.

<a id="gameplay-qa-2026-09-20-validation"></a>

### Validation

`make test`, `make test-debug`, `go test -race ./internal/...`,
`go vet ./...` and `git diff --check` passed at three points:

- after both SKT changes, with the Node gate at 219 passing tests;
- after connecting the ARM recognizer and adding the informational response,
  still with 219 Node tests (logs `build/qa-arm-{test,test-debug,race,vet}.log`);
- after the tutorial adapter (logs `build/qa-tutorial-{test,test-debug,race,vet}.log`),
  when the optional original-archive fingerprint test also passed with an
  absolute `WFEATURE_SKT_TUTORIAL_ARCHIVE` path.

No server restart, publication or real-phone check was part of these runs.
Original archives and user saves are unchanged; probes use
`var/savedata/qa-20260920/`.

<a id="skt-name-input-2026-09-23"></a>

## SKT name input with a placeholder Canvas

<a id="skt-name-input-2026-09-23-reproduction-and-cause"></a>

### Reproduction and cause

The two SKT reports saved at 07:42 UTC on 2026-09-23 show working key
delivery and active sessions. Replaying their final sessions with isolated
saves reached both name editors, where `Runtime.TextInput` returned
`no supported text field is active` before the correction.

Both archives construct `com.xce.lcdui.XTextField` with an empty text value,
an eight-character limit, unconstrained input, and a placeholder Canvas.
The placeholder has an empty `paint` body; the visible game Canvas paints
the focused field itself. The Host adapter required the constructor's Canvas
to be the current display, so it rejected these editors.

The archive SHA-256 values are
`2d66945008c1be8a749abd91d54e98c622268ab3c53e2a7c07cbe108a4d5780d` and
`2f5246006bd86d43e3ea85901208082b8eae8b47ca8831d28879d74301e15392`.
The routes and images remain ignored local artifacts.

This is vendor-specific observed behavior. A search of the WIPI specification
site and public documentation found no XTextField contract. The correction
does not change `m.SK_VM` or infer a different constructor argument order.

<a id="skt-name-input-2026-09-23-correction-and-boundaries"></a>

### Correction and boundaries

A focused field painted directly through this runtime's screen Graphics records
the current display and its revision. That evidence also permits Host text
input when the constructor supplies a different Canvas. An offscreen image
does not establish a visible target. Focus calls clear the evidence, and
display transitions invalidate it. Existing field, value, constraint, focus,
menu, and display revision checks still apply at commit.

After a Host commit, the visible Canvas is queued for repaint, because the
vendor field's `repaint` stub alone cannot present the changed name.
The game still handles OK, validates its name policy, and dismisses the editor.
The Host adds no handset Hangul keypad automaton.

<a id="skt-name-input-2026-09-23-validation"></a>

### Validation

`TestHostXTextFieldPaintedOnCurrentCanvas` reproduces the rejected field with
an authored Canvas and the real XTextField constructor. Restoring the original
ownership check makes it fail at opening the painted field. The corrected
path checks Korean commit and readback, immediate Canvas repaint, stale edits,
focus restoration, and a display round trip without repainting the field.
`TestHostXTextFieldRejectsOffscreenPaint` rejects an image-only field.

Both local archives accept a two-character Korean name and return the same
text on reopening in release and debug probes. Debug probes additionally
confirm with the game's ordinary OK key; both dismiss the editor and remain
active. This proves the reported name-entry route, not complete gameplay or
name persistence across a new runtime.

Chromium and WebKit also pass both original archives through the served page:
open the native-text dialog, submit Korean, reopen and read it back, then use
OK to reach the next setup screen. The dialog reports unavailable after the
game closes the field. Neither browser reports a page error. The runs use
isolated saves and do not establish physical-phone IME behavior. Results and
screenshots are under `var/acceptance/name-input-chromium-1790150858291` and
`var/acceptance/name-input-webkit-1790150937937`.

`make test` (including 227 Node tests), `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and `make server` pass.

<a id="skt-qa-2026-09-29"></a>

## SKT input and tile rendering (2026-09-29)

The later [retained-session investigation](#skt-resume-2026-09-30) adds a
resume correction for this tile-corrected revision and another verified class
set. It prevents repeated startup from replacing the retained game.

Two local MIDP archives reproduced three reported failures. Original archives,
disassembly, logs and screenshots remain under ignored `var/` directories.
Diagnostic output is in `var/diagnostics/skt-qa-20260929`; probe saves use
`var/savedata/qa-20260929`.

<a id="skt-qa-2026-09-29-serial-animation-input"></a>

### Serial animation input

A Canvas records key presses in a counter and consumes them inside `paint`.
Its serial Runnable clears the counter, requests another serial callback and
requests a repaint. The repaint ran immediately after that Runnable in the same
event drain, so input delivered between Host passes was cleared by the next
Runnable before another paint could consume it. Both `fire` and
`5` reached `keyPressed` while the title remained on its animated opening screen.

Repaints requested during a serial callback now wait for the next Host pass,
where they run before the next serial callback. This provides an input window
before painting and preserves one serial callback per Host pass. Explicit
`serviceRepaints` still paints synchronously. The rule applies to the shared
runtime used by the CLI and server.

The WIPI specification's MIDP Display entry and the
[MIDP Display contract](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/lcdui/Display.html#callSerially(java.lang.Runnable))
place serial callbacks after a repaint cycle. This fix makes that cycle boundary
observable to input delivered between Host passes.

The authored `SerialInputMIDlet` JAR reproduces the clearing pattern. Before
the fix, its paint consumes zero of four input taps; afterward it consumes all
four. Separate checks cover bounded serial progression and synchronous painting.

<a id="skt-qa-2026-09-29-midp-call-key"></a>

### MIDP call key

The page and CLI use Host code `10` for Call. A reported archive initializes its
send-key constant to `190`; its gameplay input switch handles that value and
starts a guard action. Passing `10` straight to the Canvas never reaches the
branch, so the tutorial cannot advance.

MIDP Canvas delivery now maps Host Call to `190` for press, repeat and release.
The existing WIPI Card path still maps it to WIPI `KEY_SEND` (`-10`), as defined
by the WIPI specification. Script input retains its own translation. The page
protocol and saved keyboard mappings keep Host code `10`.

<a id="skt-qa-2026-09-29-cross-archive-call-verification"></a>

#### Cross-archive Call verification

The initial fix had real-game evidence from the reported archive only; a
follow-up audit checked the common mapping against other local archives.

The scan covered 106 containers in the two local SKT library directories:
94 Java archives with 79 distinct class sets, plus 12 script archives. It
disassembled 174 candidate classes containing key handlers or possible `190`
constants. This was a candidate search, not a gameplay pass over 106 games.
Coordinates, array indexes, internal action numbers and switch entries that
share the default target were excluded from positive Call evidence.

Three different games have executable actions for raw Canvas code `190`:

| Local label | Java class-set SHA-256 | Static contract and runtime result |
| --- | --- | --- |
| `archive-017` | `ed8641c5e528fa5d62a0fcdcbfaf49e2d4e4c42e21358e89d474ab00f9cfba56` | The key handler forwards to a scene handler whose `190` branch selects action state `7`. In the treatment scene, raw `10` left scene `11`, action `2` unchanged; normal Host Call changed the action to `7` and opened the patient-transfer notice. |
| `archive-042` | `3be2da69a3a6d9f33a33c3c510684a7f29b469309f5f1366a51598e5276cc516` | `keyPressed(190)` records internal action `20` and calls the action interpreter. In combat scene `1010`, raw `10` left the action and Call sequence latch at zero. Normal Host Call set action `20` and latch `1`; a second press advanced the latch to `2`. |
| `archive-075` | `e3a656d9d672fb5db522ab5a3752208cd333c43cf68c09368e4ed2bb56c315df` | The original report: `KEY_SEND = 190`, the guard action and advancement to the next tutorial lesson, as recorded above and below. |

A fourth class set (`archive-077`,
`7cba828e0c4e479f749f749f48a94d7655f162b31412d40edea897909c630fc6`)
contains an explicit `190` case that jumps to the same exit as the default.
It is evidence of an ignored key in that handler, not a working Call action.

The two additional runtime checks used original archives, isolated saves and
normal input to reach the scenes. The probes read guest fields without changing
them. To compare the old value at the same scene, they delivered `10` directly
to the current Canvas callback, then used the normal runtime Host Call path.
This compares key values; it is not a replay of a complete older executable.
The second probe records both key-down and key-up, with observations immediately
after key-down so a later frame cannot hide the input latch.

This evidence supports keeping `190` as the emulated SKT MIDP handset's common
Call mapping. No conflicting raw Call value was established in this audit.
It does not establish that every SKT game uses Call or accepts the same device
layout. The [MIDP Canvas key-event contract](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/lcdui/Canvas.html)
does not define a universal Call constant; its standard keypad constants cover
digits, star and pound. This positive handset code is observed vendor behavior,
not a portable MIDP or WIPI standard constant.

The follow-up changed documentation only. Focused MIDP Call, WIPI key mapping
and CLI key-name parsing regressions passed. The prior browser acceptance
still covers transport and the shared input path; these two additional games
were checked in the shared runtime, not replayed in a browser. Corpus membership,
disassembly, route data, observations and screenshots remain in ignored
`var/diagnostics/skt-call-audit-20260929`; saves are isolated under
`var/savedata/skt-call-audit-20260929`.

<a id="skt-qa-2026-09-29-inclusive-tile-clipping"></a>

### Inclusive tile clipping

The second archive's tile renderer advances in 16-pixel steps and passes
`16 + adjustment` for both clip extents. Its constructor sets the adjustment
to `-1`; ordinary count-based clipping therefore drops each tile's final row
and column. The CLI framebuffer shows a black grid, so this is already present
before browser scaling or frame compression.

The existing `skt.inclusive_set_clip` correction is enabled for this exact
Java class set through `skt-inclusive-clip-002`. Its fingerprint is
`e3a656d9d672fb5db522ab5a3752208cd333c43cf68c09368e4ed2bb56c315df`.
The rule does not change standard MIDP clipping or enable an exception based on
profile, title or filename. The authored tile fixture already covers ordinary
and inclusive clipping, device bounds and WIPI exclusion. The local archive
test checks recognition and rejection after mutation of each class.

<a id="skt-qa-2026-09-29-validation"></a>

### Validation

- `make test`, `make test-debug`, `go test -race ./internal/...` and
  `go vet ./...` passed. Logs are in the diagnostic directory above.
- Debug and release CLI/server builds succeeded. Both browser acceptance runs
  finished with empty page-error lists and no server warnings or errors.
- The authored serial-input regression failed before the scheduling change
  (zero of four presses consumed) and passed afterward. Synchronous repaint,
  exception cleanup and existing serial scheduling tests also passed.
- The authored Canvas fixture received `10` before the Call mapping change and
  `190` afterward for press, repeat and release. Existing WIPI key tests passed.
- The tile fixture and registry checks passed, including the local archive's
  class fingerprint and rejection after mutation of each class.
- In the first real archive, the debug CLI reached the main menu, changed menu
  selection and entered the new-game introduction. The browser page reached
  the menu and changed selection in Chromium and WebKit.
- In the second real archive, before/after CLI captures of the same room show
  the missing tile edges restored. Chromium and WebKit also rendered the room
  without the black grid.
- The debug CLI and both browser engines completed the guard instruction and
  reached the following movement/attack lesson. Chromium used its keyboard
  mapping; WebKit used the page's Call button. A read-only runtime probe also
  observed the guard action and the tutorial state advancing from `2` to `3`.
  Input replay allowed the dialogue to finish between taps; the CLI acceptance
  replay used 80 ticks between confirmation, Call and the next confirmation.

This acceptance run covers the reported opening screens and tutorial boundary.
It is not a full playthrough or a physical iPhone test. Browser command logs,
page-error lists and framebuffer captures remain in the diagnostic directory.

<a id="skt-resume-2026-09-30"></a>

## SKT retained-session resume (2026-09-30)

<a id="skt-resume-2026-09-30-failure-and-evidence"></a>

### Failure and evidence

The user reported an installed iPhone PWA, switching to another app and
returning after approximately three seconds. The automated browser checks
below exercise the page lifecycle with WebKit; they are not physical iPhone
or installed-PWA suspension tests.

The reported app switch retained the server session. The debug log records a
pause and park at 09:14:28.355 KST and a resume at 09:14:33.722 — a
5.367-second absence with no intervening session start. The lifecycle resume
invoked `startApp` again inside the existing JVM.

The reported revision's `startApp` unconditionally constructs a new Canvas,
stores it as the current game and starts another worker. Its `pauseApp` returns
without stopping the previous worker. A direct reproduction changed Canvas
identity and doubled the JVM's reported thread objects from two to four.
The old game keeps its retained Graphics and resources while the new game
initializes and draws, which explains the return to the opening screen and
allows competing drawing after a new game is selected.

The failure was also reproduced in a different local game. Two other games
retained their Canvas and thread count under the same pause/resume sequence.
Labels refer to the corpus recorded in the [previous audit](#skt-qa-2026-09-29-cross-archive-call-verification).

| Label | Canvas retained before correction | Thread objects before/after resume |
| --- | --- | --- |
| `archive-017` | Yes | 1 / 1 |
| `archive-024` | Yes | 0 / 0 |
| `archive-042` | No | 2 / 4 |
| `archive-075` | No | 2 / 4 |

<a id="skt-resume-2026-09-30-scope-and-contract"></a>

### Scope and contract

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
those retain the existing behavior described in [SKT lifecycle](../skvm.md#loading-and-lifecycle).

<a id="skt-resume-2026-09-30-validation"></a>

### Validation

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
returns and an actual page reload, with three resumes and no page errors. WebKit also retained one start and three resumes, but the
reload route recorded a Blob access-control error. This is a recurrence of the
[previously unresolved WebKit decoder observation](testing.md#webkit-webkit-frame-decoding-investigation),
not evidence of lost JVM state. That run is not a clean browser acceptance pass.

Original PNG captures and replay of the captured WebP messages preserve the
room background. Pixel comparisons across returns locate changes within the
animated character area; no additional graphics compatibility change was made.
These checks do not establish physical-device suspension behavior or recovery
after the server process itself exits.

Local evidence is under ignored `var/diagnostics/skt-resume-20260930` and saves
are isolated under `var/savedata/skt-resume-20260930`.

<a id="skt-text-input-2026-10-20"></a>

## Text input offered when no strict route names a field (2026-10-20)

A user decision taken while fixing an LGT name screen
([LGT history](lgt.md#lgt-text-input-2026-10-20)) applies to every platform: a
field the player can see and cannot type into is the failure that matters, and
an offer on a screen without one is the lesser one. The SKT strict routes keep
their order and their picks; three fallbacks are asked only when all of them
come up empty, and none while a `TextComponent` is attached:

- **A Form's only editable `TextField`.** Selection starts on the first item,
  appending items does not move it, and only Up and Down do, so a Form of a
  `StringItem` above a `TextField` refused the page until the player pressed
  Down. Two editable fields with the selection on neither still offer nothing.
  The commit reruns the same rule and is stale once a second field becomes
  editable or the pad moves the selection.
- **The focused `XTextField` on a Canvas, whatever it was painted into.** The
  strict route needs the constructor Canvas to be current or a focused paint
  onto the screen Graphics at this display revision, so a title drawing the
  field into its own back buffer, or drawing its text itself, never qualified.
- **With no field focused, the one `XTextField` painted into any Graphics at
  this display revision.** The record holds one field and whether a second was
  painted, starts again when the Displayable changes, and is emptied by any
  focus call: a title closes its editor by turning the focus off and no longer
  painting it, and the earlier name-input work recorded the dialog reporting
  unavailable once a field is closed that way, which stays true. The record is
  not part of a checkpoint; after a load the title's next paint fills it.

Tests: `TestHostTextInputOffersTheFormsOnlyTextField`,
`TestHostTextInputLeavesTwoFormTextFieldsToTheSelection`,
`TestHostXTextFieldFocusedOnTheCurrentCanvas` (was
`TestHostXTextFieldPaintedOnCurrentCanvas`),
`TestHostXTextFieldPaintedOffscreenIsOffered` (was
`TestHostXTextFieldRejectsOffscreenPaint`),
`TestHostTextInputCommitsOnlyTheFocusedXTextField`,
`TestHostXTextFieldWithoutFocusOffersTheOnePaintedField`,
`TestHostXTextFieldClosedByItsFocusIsNotOffered`,
`TestHostXTextFieldTwoPaintedFieldsAreAmbiguous` and
`TestHostTextInputFallbacksKeepTheStrictOrder`. No local archive was driven to
a name screen for this change.

Gates left as they were: the command menu overlay (the field does not have the
keys while the menu is up, and a vendor commit's redraw key would reach the
menu), UNEDITABLE fields, constraint and encoding checks, and a full or invalid
`TextComponent`. Still refused for want of evidence: an unfocused `XTextField`
whose text a title draws without ever calling its `paint`, and, within one
display revision, a field that replaced an earlier painted one.

**Review follow-ups, the same day.** Two fallbacks were held to more evidence.
The focused `XTextField` is offered on a Canvas only when it belongs there:
built for it, or drawn while it is up — painted into any Graphics, or its text
read for the title to draw — so a field left focused on a Canvas the title has
moved on from is not offered on the next one, and an edit opened before is not
committed into it (`TestHostXTextFieldLeftFocusedOnAnotherCanvasIsNotOffered`;
`TestHostXTextFieldFocusedOnTheCurrentCanvas` now draws the field's text). A
focus call clears the record of painted fields only when it changes a field's
focus, and only then spends a pending edit: a title that turns off, after every
paint, a focus its fields do not have kept the record empty and every edit stale
(`TestHostXTextFieldRepeatedFocusOffKeepsThePaintedField`). The rest of the
review is recorded in [the LGT history](lgt.md#lgt-text-input-review-2026-10-20).

<a id="skt-wipi-storage-2026-10-22"></a>

## WIPI FileSystem and DataBase (2026-10-22)

*Later the same day these members were held to the common contract; see
[the note below](#skt-wipi-storage-contract-2026-10-22), which supersedes the
numbers and several answers here.*

The WIPI Java surface had six `FileSystem` members and no `org.kwis.msp.db`.
This adds the rest of `FileSystem` — every path member with and without its
access flag, `list`, `rename`, `rmdir`, `getCreationTime`, `available`,
`totalSpace`, `getMaxFilenameLength`, `toCString` and the constructor —
`File.write(int)`, and the whole `DataBase` class with its two exceptions and
its two callback interfaces. The current behavior is in
[the WIPI library's README](../../internal/api/wipi/README.md#storage).

**No local archive calls any of it.** A scan of the class files of the 106 SKT
archives present found no reference to `org.kwis.msp.db`, as a class or as a
name string, and the three Jlets call `FileSystem.exists`, `isFile` and
`remove` and nothing else here. So every answer comes from the specification,
and from the KTF and LGT `DataBase` where those settled a contract on a title.

### What the specification leaves open

- The WIPI 1.2.1 `FileSystem` page stops after `rmdir(String, int)`, and the
  `DataBase` page inside `updateRecord`. The rest — `rename`,
  `getCreationTime`, `toCString`, `available`, the `isFile` and `isDirectory`
  details, `sortRecord`, `listDataBases`, `getAccessMode`, the getters and the
  full throws clauses — is from the WIPI 2.0 pages of the same classes.
  `totalSpace` is described on neither and is answered as `MC_fsTotalSpace`
  describes its C counterpart.
- `DataComparator`'s `EQUIVALENT`, `FOLLOWS` and `PRECEDES` have no values on
  either page. MIDP's `RecordComparator` values, 0, 1 and -1, are published;
  a title compiles the constants into its own code, so the sort reads only the
  sign of an answer.
- The 2.0 text of `remove(String, int)` says a missing file throws nothing and,
  in the same entry, lists `IOException` for one. The 1.2.1 page and
  `remove(String)` throw, and both forms here do.
- `MAX_FILENAME_LENGTH` is `static int` on the 1.2.1 page and `static final`
  on the 2.0 one. It is declared final: a title that reads it with `getstatic`
  still finds it.

### Decisions and why

**A directory is a path with a file below it, or one this session's `mkdir`
made.** The save store keeps files only; nothing records an empty directory.
KTF and LGT answer every directory question with "accepted and ignored", and
until now `isDirectory` here always answered false and `mkdir` did nothing.
`list` makes that untenable — it names the directories it finds — so
`FileSystem.exists`, `isDirectory` and `mkdir` now answer from the same rule,
the specification's: `exists` takes a directory as well as a file, and `mkdir`
throws `IOException` for a name that has one. A title that reads that
exception as "this player has been here before" is told the truth whenever
there is a file below the directory, which is the case that matters to a save.
`XFile`'s `exists`, `mkdir` and `rmdir` are unchanged. What the rule cannot
answer is an empty directory an earlier session made: it is gone.

**`list` reads the store.** A title that lists its save folder after a restart
must find what earlier sessions wrote, so the names come from
`backend.ListSaves` as well as from the session, without the removal list and
without what it hides; a store that cannot list leaves the names the session
knows. A name is the entry without its directory, as `MC_fsList` answers —
KTF's Java `list` answers full paths. The archive's own files are listed,
because `exists` finds them.

**A rename is one store batch.** `storeXFileLocked` became
`storeXFilesLocked`, which writes any number of paths and one removal list
together; a single path is written exactly as before. A rename writes the new
path's bytes and the old path's removal in one batch, so no later session finds
the file under both names or under neither, and a batch the store refuses
undoes the rename in the session and throws `IOException`.

**Numbers.** The store keeps no creation time, so `getCreationTime` answers 0
for every file, as KTF's does: a fixed answer orders every file the same way in
every session, where the time of the reading session would make an old save
look new. `available` and `totalSpace` both answer 512 KiB, `XFile.fsavail`'s
budget, with nothing charged against it, as `XFile.fsused` answers 0; the one
relation a title can check between them, total at least available, holds. The
longest name, 255, is KTF's answer and is not enforced: no name a title uses
comes near it, and refusing one a handset took would fail a save.

**A database's existence and record size are in `jdb/.index`.** The records
are under `jdb/<name>` in the shared record form, the key KTF gives the same
class, which has no room for a record size; KTF keeps none, and its
`getRecordSize` answers 0. The index is what makes a database exist, because
the store has no delete: a deleted database leaves an emptied record list and
its name off the index. An index or a record list this runtime did not write —
one that does not parse, names a database twice, or holds a record longer than
its record size — is refused with `DataBaseException` rather than read in part,
and is never written over.

**A change the store refuses is not made.** Every change is stored as it is
made, a creation or a deletion in one batch with the index, and a refusal is
the `DataBaseException` the specification gives a record that cannot be
stored, with the session's records left as the store has them. That is KTF's
contract. SKT's RMS keeps a refused write and retries it later; here a title
that retries a refused insert would get the record twice.

**Ids, reads and the budget.** Ids start at 0, as on KTF and LGT, and a deleted
id is reused by the next insert, as the specification says and LGT does — KTF
appends. A read answers the record size's bytes, zeros past a shorter record,
because the specification says a read is a whole record and a title written for
a handset reads that many; KTF and LGT answer the stored length. A database
holds 512 KiB of record list, the RMS budget, which also bounds a record size:
a read is padded to it, so an unbounded size would be an unbounded allocation
per read.

**What stays as KTF settled it.** `openDataBase` without `create` throws for a
missing database, which is how a title learns it is starting for the first
time. A close does nothing and the object stays usable. Every object on one
name is one database; a delete empties it, and a write through an object held
across the delete creates it again with its record size, so a title saving
through a handle it kept is not saving into nothing. `getAccessMode` answers
`PRIVATE_ACCESS`, KTF's 1; LGT answers 3.

**Where this departs from KTF and LGT.** The specification declares
`DataBaseException` and `DataBaseRecordException` each extending `Exception`,
and so do these; KTF and LGT declare the record exception under the database
one, with no recorded evidence for it. A title compiled against a handset's
library wrote its handlers for the specification's hierarchy, and under the
other one a handler for `DataBaseException` takes a record error it was not
written for. `listDataBases` and `sortRecord` answer null where there is
nothing, as the specification says; KTF answers empty arrays. `sortRecord` is a
stable merge sort that reads a comparator's answer by its sign, as KTF's
insertion sort does.

**Not declared:** `DataComparatorInteger`, `DataComparatorString` and
`DataFilterInteger`, which `sortRecord`'s signature does not need.

**Quick save.** A session holding a `DataBase` object could not be quick saved
before: the object's state had no checkpoint form, and capture refused the
unknown payload. A database's record is now its name, record size and modified
time; a load reads its records from the current saves through the bounded
rebuild reader, as files and record stores are read, and refuses the load when
they cannot be read.

### Validation

Tests: `TestStorageSurfaceDeclaresTheSpecificationsMembers` and
`TestStorageExceptionsAndConstants` in `internal/api/wipi`;
`TestWIPIStorageMembersAreServed`,
`TestWIPIFileSystemListFindsEarlierSessionsFiles`,
`TestWIPIFileSystemListWithoutAListingStore`,
`TestWIPIFileSystemDirectories`,
`TestWIPIFileSystemRenameReachesLaterSessions`,
`TestWIPIFileSystemRenameRefusals`,
`TestWIPIFileSystemRenameRefusedByTheStoreChangesNothing`,
`TestWIPIFileSystemSmallMembersAndFileWriteByte`,
`TestWIPIDataBasePersistsAcrossSessions`,
`TestWIPIDataBaseOpenAndNameFailures`, `TestWIPIDataBaseRecordFailures`,
`TestWIPIDataBaseObjectsShareRecordsAcrossADelete`,
`TestWIPIDataBaseRefusedStoreChangesNothing`,
`TestWIPIDataBaseRefusesAnUnreadableSave`, `TestWIPIDataBaseSortRecord` and
`TestWIPIDataBaseCheckpointRestoresOverCurrentSaves` in
`internal/platform/skt`. The persistence tests run a second runtime over the
same directory store. Seven deliberate breakages — no store listing, no rename
rollback, the record exception under the database one, no read padding, an
unstable sort, no id reuse, no index write at creation — each failed at least
one of them.

Two of the three local Jlets ran 1,500 ticks each on this build and on the
build before it, with a scratch save directory: the same state, the same count
of lit pixels and the same final frame, with `FileSystem.exists` answered by
its new handler and no class missing.

Across every unique local SKT archive, a 500-tick run under the build before
these changes and after them writes the same set of save keys; the frames and
the save bytes that differ differ as much between two runs of the earlier
build, because SKT runs at wall-clock pace (see
[the KTF record of the same run](ktf.md#ktf-storage-spec-2026-10-22)).

<a id="skt-wipi-storage-contract-2026-10-22"></a>

### Held to the common storage contract (2026-10-22)

[The WIPI storage contract](../wipi-storage.md) now decides every answer above
for KTF, LGT and SKT alike, and SKT was brought to it. What changed:

- **Names.** A backslash separates segments as a slash does in every `File`,
  `FileSystem` and `DataBase` name, where SKT had refused it as a name the
  store cannot keep. The 255-byte bound is enforced on every `File` and
  `FileSystem` name, counted in UTF-8 on the path that leaves, so a leading
  separator does not count; a null name is `NullPointerException` to every one
  of them. `XFile` keeps taking longer names and keeps refusing a backslash.
- **Directories.** `mkdir` writes its name to a list at `wipi/directories`,
  outside every scope a title names, so a directory made and left empty is
  still there after a restart. A save tree from before the list has none,
  which is no directory made; nothing is migrated.
- **Removing a file.** `remove(name, flag)` returns for a name that is not a
  file, as the specification words the flagged form; `remove(name)` still
  throws.
- **Space.** `totalSpace` is 1 MiB, and `available` and
  `DataBase.getSizeAvailable` are 1 MiB less what the title has stored: each
  file and database less the archive's file of the same name, one shorter
  than that counting nothing, and the whole floored at zero. Nothing is
  refused for space; the bound is 4 MiB per WIPI file (`IOException`) and per
  database's records (`DataBaseException`), in place of the 512 KiB budget.
- **Databases.** A read answers the bytes stored, unpadded; the record size is
  not enforced; `getDataBaseSize` sums the record bytes; `getLastModified` is
  the last write in this session or the session's first open, no longer the
  first read. `create` with a record size below one is
  `IllegalArgumentException` whether or not the database is there. A name is
  its key's path, so `dir/save` is a database and `./save` is `save`. A record
  list the index does not name is a database, takes the record size of the
  open that finds it, and the index keeps it; the format of `jdb/` and
  `jdb/.index` is unchanged, and the index reads every line it read before.

The readings SKT took where the contract was silent became the contract the
same day: a database's null name is `DataBaseException`, which the
specification gives `getAccessMode`; a file shorter than the archive's file of
its name counts as nothing rather than as a negative amount; `create` with a
record size below one is refused even for a database that exists;
`remove(name, flag)` throws only for a removal the store refuses; and a
database name is a path. `TestWIPIFileSystem*`, `TestWIPIStorageBackslash*`,
`TestWIPIFileWritesAreBoundedAt4MiB` and
`TestWIPIDataBase*` in `internal/platform/skt` cover every rule, and twelve
deliberate breakages, one per new rule, each made one of them fail.

<a id="skt-storage-review-2026-10-22"></a>

## Storage contract review (2026-10-22)

A review of the contract ran saves earlier builds wrote through the build
before it and the build after it ([the KTF record](ktf.md#ktf-storage-review-2026-10-22)
has the method). The 54 local SKT save trees differ between the two builds
only as two runs of one build do, since SKT runs at wall-clock pace: each title
that differed differed between two runs of the earlier build too, or matched it
on a rerun. No SKT title that ran before the contract can newly be told it has
no room: `FileSystem.available` and `DataBase` did not exist here before it,
and `XFile.fsavail` and the RMS figures are unchanged. Nothing on this platform
was changed by the review.

What it found and left, because no local save or title reaches it:

- A database name over 255 bytes is accepted, where KTF and LGT refuse it. The
  check would belong in `wipiDatabaseKeyName`, which also reads the index and
  quick saves back, so placed there it would refuse a whole index that held
  one such name; it has to go on the open alone.
- One `File` or stream write of more than 1 MiB throws
  `RecordStoreFullException`, which a title's `IOException` handler does not
  take, from the array bound the RMS path shares; the 4 MiB bound is reached
  only by smaller writes. This is older than the contract.
- The 512-byte bound on a save key is checked on the spelling before it is read
  as a path, so `./` repeated 300 times before `save` is refused although the
  path is four bytes.
- `exists` of a missing name reads the made-directory list, walks the save tree
  and the archive's entries twice: about 1.5 ms with 200 RMS saves, against
  0.25 ms for `isFile`.
- No test throws from a title's filter or comparator through `sortRecord` into
  a handler in the title's bytecode; the native's part of that path is held,
  the interpreter's catch is not.
