# SKVM and the SKT platform

An SKT handset game is a MIDlet JAR. There is no native code, no custom
executable format, and no ARM: what makes it an SKT game is that its world
contains `com.skt.m`, `com.skt.m3d` and `com.xce` on top of standard MIDP.

So `internal/platform/skt` is the whole Java runtime: class loading through
the shared JVM, the MIDP surface a title draws and saves through, and the SKVM
classes on top. It used to be a thin layer over a vendor-neutral "j2me"
package; that package had exactly one consumer, and keeping it neutral only
hid where its contracts came from. The class surface
lives in `internal/api/skvm` and the natives in
`internal/platform/skt/skvm*.go`, next to the framebuffer, fonts and images
they need.

**The SKVM classes come from the platform, not from the title.** A machine
built from a title's own JAR alone must not resolve `com.skt.m` — otherwise a
title could ship its own `MathFP` and quietly replace the one every other title
measures against. `TestSKVMClassesComeFromThePlatformNotTheTitle` holds that
line.

## MathFP

`MathFP` is nine-decimal fixed point: **1.0 is 1_000_000_000**. Every value a
game passes and receives is scaled that way, which is why `PI` is
`3_141_592_654` and why every product has to be rescaled. `parseFP` scales a
whole number in, `toLong` truncates toward zero on the way out.

The scale is the observable contract, so it matches the reference
implementation exactly. Overflow
saturates rather than wrapping: a game that overflows an intermediate should
see a huge number, not a sign flip.

## Files

`com.xce.io.XFile` reads and writes through the **same Host save boundary
MIDP RMS uses**, under the `fs/` scope — the same one KTF's guest filesystem
uses — so one owner directory holds everything a title persists whichever
platform wrote it:

```
var/savedata/<profile>/skt/<owner>/fs/<path>
```

A path the game never wrote falls back to the JAR resource of the same name,
which is how a game reads data it shipped. `unlink` empties a file rather than
removing it, because the save boundary has no delete — the same reason RMS
keeps a store index.

`FileInputStream` and `FileOutputStream` are written in Java on top of
`XFile`, so there is one implementation of the file semantics rather than two.

## Graphics2D

`Graphics2D` wraps a MIDP `Graphics` and adds what MIDP does not have:
per-pixel read and write, rectangle inversion, a screen capture, and a blit
with `SRC_COPY`/`AND`/`OR`/`XOR`. The pixel *mask* is the surface's alpha
channel rather than a separate plane, because these surfaces carry real
alpha.

## Input

Keys reach a Canvas as `keyPressed`, `keyReleased` and `keyRepeated`, and a
`GameCanvas` may read `getKeyStates` instead. Which of those the corpus
actually uses is a static read rather than a guess: across the fifteen local
archives, **fourteen override `keyReleased`**, five override `keyRepeated`, and
**none uses `getKeyStates`**. So a release matters to nearly every title here,
and none of them holds a set of directions the way a `GameCanvas` bitmask
would.

That is the shape a defect fits into, and one of the titles has it. Holding a
direction and letting go of a *different* key that is still down — a keyboard
does this every time two keys overlap — stops the character dead:

- With the release delivered, a side-scroller walking left stops the instant
  another held key comes up, and the screen does not change again: 0 pixels
  over the next 120 ticks.
- Without it, the same route walks on exactly like the control that never
  touched a second key.

So this runtime sends keys through the same pad the Clet platform does
([`internal/keypad`](../internal/keypad/keypad.go), and `lgt.md` "A release
stops a character the pad has moved on from" for how it was found): a pad key
released while another is still held delivers nothing, the direction now under
the thumb is announced once when the one in use comes up, and everything that
is not the pad is delivered exactly as it happens. The pad here is the four
MIDP navigation codes **and the digits 2, 4, 6 and 8** — the local
side-scroller walks on both, which is what says the digits belong in it.

A repeat is neither a press nor a release and does not move the pad; it is
delivered as it arrives, which is what the five titles that override
`keyRepeated` are waiting for.

Two things this deliberately does not do. It does not withhold a release for
any key that is not the pad: those are actions, and a title that never hears
the release of its attack key is worse off than one that stops walking. And it
does not invent a repeat: what a held key does between its press and its
release is the title's business.

## The frame loop

A MIDlet has no tick of its own. It runs on the callbacks the Host makes and on
whatever those callbacks deferred, and an SKT title defers its whole game. Three
contracts decide whether it runs at all, and each of them fails silently when it
is missing: the title starts, shows a Canvas, reports no error, and never paints
a second frame.

**`Display.callSerially` runs one Runnable per Host pass.** MIDP says a serial
Runnable follows the events already on the loop. The common frame loop here is a
Runnable that repaints and hands *itself* straight back, so a Runnable posted
directly onto the event queue re-queues itself inside the same drain: the loop
never empties and the pass ends on the event loop's own limit rather than on a
frame. So `callSerially` queues, and `RunPending` — the Host's per-frame pass —
takes exactly one off. This is the same rule the WIPI path applies to a serial
Runnable for the same reason (`docs/ktf.md`).

**The screen `Graphics` outlives the paint it arrived in.** MIDP says a Graphics
is valid only inside `paint`. On this vendor's handsets it was not, and titles
are written against what the handset did: a Canvas whose `paint` does nothing
but store the Graphics in a static field, and a game thread that draws through
it whenever it likes and pushes the result with `XDisplay.refresh`. There is one
screen Graphics for the life of the runtime; each paint resets it to that
repaint's clip, no translation, and the default font. Two guest threads drawing
at once still race — that is the game's business, as it was on the handset.

**`XDisplay.refresh` takes the picture at once and shows it with the pass.**
The call is the game's own frame boundary — the guest thread saying the screen
is complete — so the surface is copied there, on that thread, which is what
makes the picture whole rather than half-drawn. What waits for the Host pass is
the *present*, because a Host can show at most one frame per pass anyway: the
CLI reads the surface at a tick boundary and the page sends one frame a tick.

That matters because one title family pushes about a hundred times per pass —
**496,029 calls in 4,000 ticks**, against another title's 3.7 per pass. Their
loop draws as fast as the machine allows and pushes every time round. Presenting
each call allocated a copy of the framebuffer and handed it to a surface that
copied it again; coalescing took **a fifth of the host CPU** off those runs
(19.4s to 15.1s of user time for 900 ticks) with the pictures unchanged, and it
makes the guest wait for nothing.

A lifecycle dispatch presents a pending picture too, and that is not a detail: a
title that draws its notice through the direct display and then calls
`System.exit` does all of it inside `startApp`, so the only pass it ever gets is
that one. Two local titles are exactly that, and without it they show a black
screen instead of what they wanted to say.

**`XDisplay.width`, `height` and `height2` are fields, not methods.** A game
reads the screen size straight out of them, typically caching it in a Canvas
constructor and repainting that rectangle forever after, so the runtime fills
them in from the framebuffer before any guest code runs. Left at zero the game
asks to repaint a zero-sized rectangle and paints nothing at all. `height2` is
the drawable height on a handset that reserved rows for a soft-key bar; a Canvas
here gets the whole framebuffer, so both heights are the framebuffer's.

**The handset properties are answered.** `System.getProperty` answers `MIN`,
`m.MIN`, `m.VENDER`, `m.CARRIER`, `m.COLOR`, `m.SK_VM` and
`com.xce.wipi.version` alongside the MIDP names. A missing one is not read as
"unknown": a title compares the vendor string against the single device it
carries a workaround for, and a null there throws inside the constructor that
would have started its game thread. The subscriber number is the one the WIPI
platforms answer with, since the question is about the handset rather than about
which runtime is asking, and the vendor string deliberately names no real
manufacturer.

**The JVM step limit is a window, not a ceiling.** A title's own thread is the
game — it decodes images and loads a world before it draws anything — so a fixed
instruction budget kills every title partway through loading. This runtime grants
another window while its MIDlet is running and refuses once it is destroyed,
which makes destroying the MIDlet the way a runaway guest stops. See
`docs/jvm.md`.

**`System.out` and `System.err` exist.** Shipped titles are full of leftover
debug printing. The streams are runtime-owned CLDC classes in `internal/jvm`,
and what a game prints goes to the logging boundary at debug level, one line per
call. What matters is that the field resolves: a game that cannot read
`System.out` dies in whichever method happened to contain the print.

## Fifteen titles, and the pass that made room for twelve of them

The corpus went from three local titles to fifteen in one step, and the twelve
new ones arrived as a stack of lifecycle failures: eleven of the fifteen
refused to start at all. What they refused over was almost never a defect —
it was surface this runtime had never been asked for, because three titles is
not a corpus.

**The pass that fixed them did not start from a run.** `runskt -diag` reports
what a title asked for before it stopped, which means one gap per run, and a
rebuild between each. `internal/tools/apiscan` reads the classes instead: every
symbolic reference in every class of every archive, minus what the library
declares and what the platform registers, is the whole list before anything
runs — and, scanned over a corpus at once, it is ranked by how many titles want
each entry. The first scan answered 55 entries across 12 titles; the last one
answers 31 across 4, and 11 titles have nothing missing at all. Its `-natives`
flag takes a `-diag` report, because a platform registers half its surface on a
live runtime and a bare VM cannot see those registrations.

What that list turned into, roughly in the order it was worked:

- **The class library grew the java.lang a title names rather than calls.**
  `Integer`, `Math`, `Runtime`, `Throwable` and the whole exception hierarchy
  were natives with no class behind them, which works until a title writes
  `new Integer(3)` or `catch (Throwable t)`. See `docs/jvm.md`, "The classes a
  title names".
- **`Graphics.reset()`**, which is not MIDP's, is what nine titles call on the
  screen Graphics they kept. It puts back the state a fresh one would have had.
- **`Graphics2D.getGraphics2D(Graphics)`** is how six titles make the wrapper,
  and **`captureLCD` is static** — three titles call it with `invokestatic`
  before they have a wrapper at all, which is also what says it copies the
  screen rather than any surface.
- **`com.xce.lcdui.Toolkit` publishes fields, not only methods.** Ten titles
  read `FONT_HEIGHT`, `graphics`, `DEFAULT_FONT` or `MAX_CHARWIDTH`, and a
  field no class initializer filled is a zero — a title that lays out a menu on
  a font height of zero draws every line on top of the last. The initializer
  reads them from the font and the screen this runtime actually draws with.
- **`com.xce.io.ByteToCharEUC_KR`** is the vendor's decoder as a class: the
  java.io converter API of the day, `convert` filling a char array and
  answering how many characters it wrote. The counts are lengths rather than
  end indexes, which the call site settles — one title computes `end - start`
  for the input and `chars.length - offset` for the output.
- **`System.exit` reaches the lifecycle.** A MIDlet is supposed to leave
  through `notifyDestroyed`, but these titles call `exit` on the way out of an
  error dialog, and the two now land on the same teardown.
- **`m.MODEL` is answered, and the subscriber number is read late.** One title
  parses the model number before it compares it against the two handsets it
  carries workarounds for, so a null was a hard stop. And `MIN` used to be
  copied into a table at package load, which meant a Host that set the number
  afterwards — `-number`, `WFEATURE_PHONE_NUMBER` — was ignored here. It is
  read when it is asked for now, which matters because one title authenticates
  against it; see "One title checks its licence against the handset's number".
- **An EUC-KR file name in an archive stopped an archive from opening.**
  `zip.Reader.Open` takes an io/fs path and refuses a name that is not valid
  UTF-8, which is what a Korean handset's own data file has. Entries are read
  from the entry list by their exact stored name now, and an installed file
  answers to both spellings of its name — the bytes the archive stored and the
  text they decode to — because which one a title asks with is its business.

Four of the fifteen needed something beyond the library, and those have
sections of their own below.

## What the local titles still stop at

The three titles this platform started with each went further than the last, so
what is left for them is best read as a list of what has not been driven rather
than of what is broken. The one that used to head this list — a title that
would not leave its guardian-selection screen — is off it: the confirm key was
fire all along, and what the script was missing has a section of its own below.

- ~~**Progress saving and loading have not been driven** on any of the three.~~
  **All three are closed end to end now**: the `RecordStore` one — see "A
  progress save, taken in a title's own save menu" — the `XFile` one that writes
  three numbered slots, which loads a save it was shipped with and now takes one
  of its own ("The same title's save, found by reading upwards from the write"),
  and the third, whose refusal turned out to be an `IOException` this platform
  threw at it rather than a rule of its own — see "The third title's save, and
  the message that named the wrong culprit".
- ~~`runskt` has no diagnostic report.~~ It has one now — `runskt <zip> -diag
  report.json` — and what it found is below.

### The screen that would not confirm was waiting for a second choice

The title whose guardian selection nothing would confirm now reaches its
opening cutscene. Nothing was wrong with the key mapping, and the answer is
worth writing down because the same shape appears twice in one flow and reads
as a dead screen both times.

The screen wants **two** guardians. `bl.a(action, keyCode)` treats fire — or
`5`, which it accepts alongside it — as a **toggle** on whichever guardian the
cursor is over, counts what is selected, and only when the count reaches
exactly two does it raise its confirmation prompt. A script that presses fire
once and expects to move on presses it into a screen that has silently marked
one guardian and is waiting for the other. Nothing on the screen changes enough
at a contact sheet's scale to show that it did anything.

Then the prompt itself: **its cursor starts on NO**, and fire on NO clears the
selection and puts the screen back where it started — which is exactly what a
script that keeps pressing fire sees, forever. Left or right moves to YES.

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

**What settled it was the class file, not the screen.** A contact sheet of
every key this runtime can deliver, one per hundred ticks, showed the screen
unchanged for all of them, which says only that no single key confirms. The
title's own bytecode says why: `javap -c` over the Canvas names the state
handler, the debug build's instruction log says which class is running while
the screen is up (`bl`, by a wide margin), and that class's `a(int, int)` is
forty lines of counting. Reaching for the archive earlier would have saved the
sweep — the classes are obfuscated to one and two letters, but the control flow
is not.

### An SKT archive is a filesystem, not just a JAR

An SKT container holds `<id>.jar` beside `<id>.msd`, and this read the JAR and
threw the rest away. Two of the three local archives carry more than that:
bare-named files sitting at the top level — one has `c`, `dnlist2`, `o` and two
further JARs, another has `n1`, `g1`, `cf`. **They are the title's own
filesystem.** A handset was sent one archive and unpacked it into the title's
storage, and the game opens them by exactly the name they have there.

Discarding them broke one title's settings entirely, silently, every run. Its
`bs.j()` opens `"/c"` for reading, and `com.xce.io.XFile` for a file that is
neither a save nor a JAR resource throws `IOException` — so the read failed, the
title fell through to the branch that writes defaults, and **that** opens `"/c"`
for writing with no create bit, which threw for the same reason. A settings file
that could be neither read nor written, and nothing on screen to say so.

`Open` now keeps the container's other entries as the archive's own, under
their bare names, and `XFile` finds them through the resource fallback it
already had. What is excluded is named rather than guessed at — the JAR, the
`.msd`, the `.mod` and the `.wmr` are the platform's, not the title's — and a
JAR entry of the same name still wins. The change is visible in the title's own
control flow: the run that used to reach `bs.i` (write the defaults) now
reaches `bs.j` (read the file) instead.

**Settings then save and load for real.** Changing one in that title's Config
screen writes `fs/c` into the save tree, and the byte that changes is exactly
the nibble the game's own bytecode edits (`0x17` to `0x37` for the first
setting). The next run prefers the save over the packaged copy, which is what
`initXFileName` does for every path. What has still not been driven is a
**progress** save: one title's route reaches its opening cutscene and no save
point, one opens files without writing during the driven route, and the third
reaches neither. That is play, not plumbing.

### A record store, written and read back by a real title

The other save boundary is `RecordStore`, and until now nothing but the unit
tests had used it. A second title's settings go through it rather than through
`XFile`, and driving that screen closes the round trip:

```sh
wfeature runskt <zip> -ticks 900 -save /tmp/probe \
    -key 250:fire -key 300:down -key 330:down -key 380:fire \
    -key 500:left -key 560:left -key 620:down -key 680:left -key 700:clear
```

That reaches the settings screen from the main menu, lowers the two settings on
it, and leaves. The save tree then holds `rms/opdata`, three bytes long, and
the bytes are the settings: the sound level moves with each `left` or `right`
and the speed byte follows the other row. **The next run reads it back** — the
screen opens on `SOUND [1]` and `SPEED SLOW` rather than on the defaults, which
is the title's own `oploadData` running at startup.

That is not the progress save the list above still wants, and it is the same
store: the title's `saveData` writes its progress through
`openRecordStore(name, true)` and `setRecord`/`addRecord`, which is what the
settings path just exercised end to end. What is left is reaching a save point.

**The route to one is in the title's bytecode rather than on the screen.** Its
in-game menu — the one holding the save slots — is reached two ways, and both
were read out of `javap -c` rather than found by pressing keys: during a battle
the key handler's `lookupswitch` sends **`#`** to it, and on the world map it is
**item 8 of a 3×3 picture menu**, whose cursor the code lays out as
`index = row*3 + col`. Getting to either still needs the game played there.

### The CLR key had no name, so no script could send it

Every screen in that title draws `BACK:CLR` in its corner, and leaving the
settings screen is what writes them. `KeyCodeByName` had no name for that key,
so no scripted run could ever leave that screen — the settings above could be
changed and never saved, and the run above could not have existed.

`clear` (also `clr` and `back`) now resolves to the handset's `8`, which is the
value the title's own key handler switches on and the one the original runtime
carries. It is the same name the other WIPI platform's routes use, which is the
point of that table: a script reads the same whichever vendor it drives.

### A progress save, taken in a title's own save menu

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
settings round trip already wrote. Starting again with the same `-save` and
choosing *continue* opens the load screen on **`1. 전략 명성 0`** where both
slots used to read `NO SAVE DATA`, and confirming it drops straight back onto
the world map at the city the save was taken in. The diagnostic report for that
route names the path it went through: `openRecordStore` nine times,
`addRecord` twice, `setRecord` once, `closeRecordStore` once.

**Two things made the route findable, and neither was pressing keys.**

The first is that **the menu takes a digit key directly.** The tactical menu is
a 3x3 picture grid whose cursor the code lays out as `row*3 + col`, and its key
handler's `lookupswitch` has a case for each of `1`..`9` that assigns the row
and column outright — `9` is `(2,2)`, which is index 8, which is the entry that
calls `drawGameInMenu`. So the whole grid is one keypress from anywhere in it,
and the arrow keys never need to be counted. The menu that opens is a numbered
list and takes its digits the same way: `2` is 게임 저장.

The second is that **a story-driven prologue needs both of its keys.** The
intro art says `CLR:SKIP` and the dialogue over the map says `NEXT▶OK`, and the
title alternates between the two for a few thousand ticks. A script that
presses only `fire` loops in the intro forever; one that presses only `clear`
stops at the first line of dialogue. Alternating them walks the whole prologue,
and only after it does the tactical menu answer any key at all — which is why
the key sweep over that screen, earlier, had found nothing but `fire`.

The other two titles are obfuscated to one- and two-letter classes, where this
one had three classes with their own names. What survives obfuscation is the
part that mattered: the string constants name the files (`/g1`, `/g2`, `/g3`
and `/n1`, `/n2`, `/n3` for one of them, three slots and their names), and the
`lookupswitch` over key codes reads the same either way.

### One of the two loads a save it was shipped with

The XFile title above is `u.class` — `javap -p -c` over the inner JAR finds it
by the two static `String[]` its `<clinit>` builds, `{/g1,/g2,/g3}` and
`{/n1,/n2,/n3}`, and every `com/xce/io/XFile` construction in the class indexes
one of them. The write side is a `new XFile(a[slot], 2)`, a `write`, and a
`close`; the read side is the same shape with mode 1.

**The archive ships `g1` and `n1` beside the JAR**, 525 and 599 bytes: a save
the handset this container came off had taken. `addInstalledFiles` mounts a
container's files into the archive entries, and `xFileContents` falls back from
the save store to those entries, so the title sees them as `/g1` and `/n1`
without anything being imported. It shows: the title screen offers **이어하기**
rather than a new game, choosing it opens a slot screen with the save's own
level and progress on `SLOT 1`, and confirming drops into the dungeon it was
taken in. So the **load** half of that title's round trip is closed on a save
the platform did not write.

### The same title's save, found by reading upwards from the write

A key sweep in the dungeon — both soft keys, `clear`, every digit, `*`, `#` and
the send key — never found that title's save menu. Reading upwards from the
write did, in one pass, and the chain is worth keeping because the next
obfuscated title has the same shape:

- `u.g(byte)` writes `/g<slot>` and `u.f(byte)` writes `/n<slot>`; the only
  thing that calls both is `u.l()`, so `u.l()` **is** the save.
- Three classes call `u.l()`, and only one is a screen with nothing else in it:
  `ad`, a four-entry menu whose first entry raises a yes/no prompt and whose
  prompt callback sets the two-frame flag that performs the write.
- `ad` is constructed at exactly one place: case **7** of `ck.b()`, the eighth
  entry of the in-game menu.
- `ck` is opened by `u.g()`'s case 6, and the only key that requests it is
  `keyCode 8` — `clear` — in the field handler, **and only while `bb.s == 1`**,
  the walking state. A sweep run in any other state sees a key that does
  nothing, which is what a sweep in a dungeon under attack was reading.

The cursor is the base screen's and it **wraps**: `ck` is built with eight
entries, so one `up` from the entry it opens on lands on the last one. The
whole route is five keys once the world is up:

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
bytes from the nineteenth on. Starting again against the same tree loads them:
`이어하기`, `SLOT 1`, and the dungeon, with the diagnostic report counting 23
`XFile.read`, 6 `initName` and 4 `close`. **Both halves of that title's round
trip are closed, on a save this platform wrote.**

Two things about the run are worth knowing before repeating it. The shipped
save drops the party into a room with an enemy already on it, and an unattended
run is dead in about two seconds — the menu keys have to be pressed
immediately, not after a look around. And a run that presses `fire` a few
hundred ticks later than this walks the *title* screen instead, because
`GAME OVER` has returned it there and the same key means something else.

### The Korean text was drawn on the wrong grid, and the Latin beside it was not

Text outside the authored 5x7 table — Hangul first among them — reached for
`glyph.Render`, the package-level entry point, which is the 16-dot face. The
5x7 table kept answering for the characters it covers. So one line of a menu
carried two sizes of text: a numeral seven pixels tall next to a syllable
eleven above the baseline, in a line the platform had told the title was ten
pixels tall.

A game reads its own layout out of the metrics the font reports, so the ones
that matter are the ones its handset answered, and those are the small ones —
the same conclusion KTF reached from a save slot whose label ran into its
number. The evidence here is a local title's main menu: seven entries, each a
small `1.` against a syllable half again its height, the descenders of one line
touching the line under it and the whole block overflowing where the title had
put it. On the handset face the same menu is proportioned and its lines clear
each other.

`pixelFace` in `font.go` is the one place that chooses, and it answers
`glyph.Handset()`. The 5x7 table is untouched, so the Latin shapes the
platform tests pin down render exactly as they did.

**Only one of the three local titles moved.** The other two draw their Korean
with sprites of their own and never call `drawString` at all, which is worth
knowing before reading a frame diff on them: those two differ run to run
against *themselves*, in one animated corner of a splash screen, because that
animation is on the wall clock. Diffing them across a build change says
nothing until that is subtracted.

### The third title's save, and the message that named the wrong culprit

The remaining title is driven into play and saves now. The route is worth
recording, and so is how nearly the failure was written off as the title's own
rule.

**The route.** Past the guardian selection (above), the opening cutscene skips
on `*` — pressed every 150 ticks it walks the whole thing — and `fire` after it
carries the party into the world. The in-game menu is the field's `clear` key,
the same shape the other title has: `ar.keyPressed` dispatches on `m.e`, state
2 is the field and state 5 is the menu `ag`, and the field handler asks for
state 13 on `keyCode 8` while `n.h == 1`. The menu itself is a **row of icons**,
so its cursor is left/right rather than up/down (`cb.d` takes `4`/`6` and the
two game actions), it wraps, and the last icon is `c` — the system screen whose
first entry is 세이브. One `left` from where the menu opens lands on it.

**What the title answered was `실패하였습니다. 저장 공간이 부족합니다.`, and that
was this platform's fault.** It was read here for a while as the title's own
refusal — the save entry does sit behind a field, `m.a.a`, which `ac(byte)`
computes from the scene it is in — and the message was attributed to that
guard. Two readings of the archive take the attribution apart:

- **The guard has its own message, and it is a different one.** `c`'s save
  entry reads `if (m.a.a) { show strings 51 and 52 } else { proceed }`, and
  those two strings in `sgui/gm.tdf` are `보스전에서는` / `저장할 수 없습니다.`
  So `m.a.a` marks a **boss scene**, and it says so.
- **The message that did appear is a catch block.** The screen that runs the
  save is `try { m.h(); show 46 } catch (IOException) { show 47, 48 }`, with 46
  `저장되었습니다.` and 47 and 48 the pair that was on screen. The title was not
  refusing anything: it was reporting that the platform had thrown.

**And the throw was ours.** `m.h()` writes the slot by name — the three slots
are `/k`, `/s`, `/w` and the party file is `/o` — with `new XFile(name, 2)`,
mode `WRITE`. `initXFileName` required a create bit before it would open a file
that did not exist yet, and `/o` ships inside the container while `/k` does not,
so the first write of a first save threw `no such file`. **There is no create
bit in this API**: the original runtime opens a writable file the way
`RandomAccessFile("rw")` does, which creates it, and the constant this platform
had called `CREATE` is `READ_DIRECTORY` there. A save is what proves it — a
shipped title whose only way to write a slot is this call must have had it
create the slot. Opening for writing now creates; a read-only open of a missing
file still fails, because that is a title asking for something that is not
there. Driven to the same menu again, the title writes `fs/k` and `fs/o` and
answers `저장되었습니다.` — and a second run reads it back: the title screen's
second entry, which the game itself puts the cursor on once a slot exists,
shows the character, the level and a completion percentage, and choosing it
returns the party to the interior the save was made in. That is the last of the
three local SKT titles to close its progress round trip.

**Two things about the archive are worth keeping anyway**, because both were
wrong in the earlier reading and both are reusable. The scene id **is** the map
number: `ac`'s own loader asks `ce.a("/m/", m.f, 7)`, and that helper opens
`prefix + (index / 7) + ".bd"` and takes record `index % 7` out of a five-byte
offset table at the front — so twelve files hold **84 scenes**, and an id of 82
is file 11, record 5. The header that "had nothing that varies like an
identifier" was that offset table. And the scenes are named, in `m/name.tdf`: a
count byte then length-prefixed EUC-KR strings, which reads straight through to
82 = `게브의 체내` — inside a monster, with 11, 13 and 15 left blank. Four boss
arenas, which is exactly what a save guard would be for.

### The default charset is the handset's, and a title finds out through its own font

`String.getBytes()` answered UTF-8 here, and the byte a title gets back is not
text to it — it is an **index**. One title draws its own menu labels by handing
that array to its own renderer, which walks the bytes and indexes a glyph table
with each one, and UTF-8's three bytes per syllable index somewhere the table
does not reach: `ArrayIndexOutOfBoundsException: index 11034 for length 4700`,
thrown inside the title's text routine while painting its in-game system menu.
The labels beside the failing one drew correctly, and that is the tell — those
are byte arrays out of the title's own resource file, which were EUC-KR all
along. Only the label that came from a Java string literal went through
`getBytes`.

So this platform installs the same pair KTF does: `ByteDecoder` and
`ByteEncoder` are EUC-KR, which is what these handsets' default charset was.
The menu paints its five entries — 세이브 / 네트워크 등록 / 도움말 / 옵션 /
종료하기 — where before it killed the run.

Worth knowing for the next platform surface that touches bytes: the crash was
three thousand ticks into a scripted run and looked like a title bug. What
named it was the stack — the exception is thrown by the *title's* code, in the
one call that was given a `getBytes` result rather than a resource array.

#### Where a character starts is not the length of the chunk

`ByteToCharConverter.convert` takes a range at a time, so a two-byte character
can straddle two calls and the lead byte has to wait on the converter for its
trail. Deciding *which* byte is waiting is the part that has a trap in it. The
first rule here asked whether the last byte was in the lead range and whether
the chunk had an odd number of bytes, and the parity is what makes that wrong:
it stands in for "we are mid-character" only while the text is entirely
double-byte.

A title that writes `"name:"` before a Korean word breaks it in both
directions. `"abc"` followed by a lead byte is four bytes, so the even count
says the chunk ends whole, the lead byte decodes alone into a replacement
character, and its trail arrives at the head of the next chunk with nothing to
join — two bad characters from one. The other direction is worse because it
damages text that was never split: `"a"` followed by a whole `가` is three
bytes, and `가`'s trail byte `0xa1` is itself inside the lead range, so the odd
count tears the trail off a complete character.

The state is only recoverable by scanning forward from the start of the chunk:
a byte in the lead range consumes the one after it, anything else advances by
one, and the answer is whether the scan runs off the end on a lead byte.
`danglingLeadByte` is that scan, and the two chunks above are in its table
test.

### The diagnostic report, and the three questions it answered

`runskt <zip> -diag report.json` writes what a run used: the classes the VM
loaded, the classes it was asked for and did not have, and a call count for
every registered native. It was built the second time reading the debug build's
instruction log by eye was the only way to find something — the first was the
frame loop's five defects, the second a title's guardian screen, and folding
millions of lines by class and method twice is once too many.

The counting is an atomic add inside each registration, so it costs a native
call one increment and is on in both build profiles. A report from a specially
built binary would describe that binary rather than the one anyone runs.

**The zeros are what the report is for.** A native that ran tells you the
title reached it; a native that never ran is surface nothing here proves is
needed, and that is the census the two remaining questions were waiting on.

**MIDP surface: 404 registered natives, 45 called.** Driven as far as the three
local titles currently go — one into its opening cutscene, one into early play,
one to its main menu — 359 registrations were never reached, and 87 classes
were loaded between them. The heaviest unused groups are the fixed-point maths
class (23), `RecordStore` (20), the high-level `List`/`ChoiceGroup`/`Item`
widgets (51 between them), the text fields (26), `Player` (14), and the 3D
mesh and SIS image surfaces (27).

**That is a floor, not a licence.** These runs stop early, and a title that
saves at a save point reaches `RecordStore` on the first frame after it. What
the number is good for is the opposite direction: it is a guard against
growth. Adding to a surface where 89% of what is there has never been called
by a real title needs a caller in hand, and the report is how to check.

**GCF: no caller has appeared.** Every `javax.microedition.io.Connector` entry
reports zero across all three titles, so the unimplemented part of the generic
connection framework is still waiting for the first title that asks for it.
Nothing was added on the strength of the framework existing.

Taken again on the much longer route that reaches a save point — a whole
prologue, a world map, an in-game menu and a written save, against the boot-
and-menu runs the census above was taken on — it is still zero. That is the
answer worth having: the earlier count could be read as "these runs stop too
early to ask", and this one cannot.

### The handset bitmap is this vendor's format after all

`Image.createImage` here decodes PNG, JPEG and GIF through the standard
library, and the handset bitmap through `wipic.DecodeLBMP` (`ktf.md`, "LBMP:
the handset's own bitmap"). That routing used to carry a note saying no SKT
archive shipped one and it was here only because the decode was shared. **The
note was true of three titles and false of the vendor**: the wider corpus
carries about nine hundred LBMP files, and they are what the SKT titles draw
almost everything with.

They also carry the half of the format the KTF files could not show — a
one-bit transparency mask after the pixels, and bit planes for the depths below
8 — and both were found from these titles' screens. One drew its title menu not
at all, because a two-bit logo decoded as an unsupported depth and the title
caught the `IOException` and drew a null image on its own thread; three
siblings drew their world as blocks of magenta, which is what a masked sprite
looks like with its mask ignored. `ktf.md` has the format; the arithmetic that
identified the mask is `width * ceil(height / 8)` and it holds across every
masked file in the corpus.

**No local archive ships a BMP for this platform** — that path is still
missing and this is the note that says so.

### Three siblings loaded their tables through the wrong class

Three sibling titles stopped before their first frame with a null `Vector`,
inside a method that reads a table the class initializer should have filled.
The loader is `Runtime.getRuntime().getClass().getResourceAsStream("table.gft")`
and the file is plainly at the top of the JAR — but a relative resource name
resolves against its class's package, and that class is `java/lang/Runtime`,
which asks for `java/lang/table.gft`. The stream came back null, the
`DataInputStream` over it threw inside the title's own `catch (Exception)`, and
the loader returned `true` having filled nothing. The failure then landed
several methods later, on the read.

The handset runtime answered that name, or the titles would not have shipped.
So a relative name its class's package does not answer is looked up again at
the root of the archive — the strict answer still wins when it finds
something, so this only replaces a null. All three now draw.

**What this is worth remembering for**: a `catch` around a load is where a
platform's null goes to be forgotten. The exception the title swallowed named
the problem exactly, and the failure it eventually reported was a `Vector` in
an unrelated class.

### One title checks its licence against the handset's number

One title draws "인증 되지 않은 컨텐츠 입니다" and stops. Nothing is broken: its
`SecureUtil` hashes the subscriber number, a slice of the service ID out of
`MIDlet-Jar-URL`, and a constant, and compares the hex digest against the
`MIDlet-Key` in the archive's own descriptor. The key was made for the handset
the title was bought on, so it authenticates against a number this runtime does
not have.

That makes it a setting rather than a defect: the number the emulator answers
with is `-number` on the server and `WFEATURE_PHONE_NUMBER` for the CLI, and
this platform reads it late enough to see one now. The KTF platform has a title
in the same position for the same reason — `docs/network.md`, "The subscriber
number is the one property worth changing". **Whether any number the user has
is the right one is theirs to know**; nothing here recovers it from the key.

### The four that were left, and what each one actually wanted

None of the four was short of the surface the scan had listed. Each was one
contract read the wrong way round, and the scan's remaining entries — the two
`java.io` interfaces, `java.util.Timer`, `TextComponentHandler` — turned out to
be on paths none of them takes. Three of those four are answered now anyway,
for the reason "Nothing a local title names is missing" gives; the two
`java.io` interfaces are the one entry that never needed answering, and the
same section says why.

- **Three siblings drew their world in magenta blocks**: the transparency mask
  was being applied with its polarity inverted, so every sprite came out as its
  own transparent colour with the artwork cut away. `ktf.md` has how the files
  settle it. Their `XDisplay.refresh` count — 110,048 calls in 900 ticks — is
  not related and is still worth a look one day; it is a title presenting far
  more often than it draws.
- **One drew its whole opening narration in black on black**: it composes
  Korean out of its own initial/medial/final jamo sheets, which are two-bit
  planar LBMP, and the planar ramp was being read as light when it is ink.
  Inverting it turned a screen of scattered consonants into the script.
- **One stopped at a studio logo** on `XDisplay.drawImageEx`, the vendor's blit
  with a source rectangle. Thirteen call sites in that title settle its
  arguments; two of them are unexercised and this platform says so rather than
  guessing (see below). It reaches its play screen now.
- **One asked for artwork its own archive does not contain.** It branches on
  the screen width and loads `main_logo_240.png` on anything 240 wide or
  wider — and this SKU ships only the 120 and 176 sets, because it was packaged
  for a smaller handset. Nothing is wrong with the emulator except the screen
  it offers: at `-screen 176x220` the title runs to its menu, its opening and
  its first scene. **Both Hosts can choose one**: `runskt -screen WxH`, and the
  page's settings panel, which remembers the choice per game and sends it with
  `start`. Nothing detects it — an SKT descriptor declares no screen size — so
  it is a setting rather than a rule. `docs/session.md`, "The screen is part of
  starting a game".

### What drawImageEx is, and the two things it does not say

`XDisplay.drawImageEx(Graphics, Image, int x, int y, Image source, int sx,
int sy, int width, int height, int mode)` draws a rectangle of the source at a
point on the Graphics. The thirteen local call sites fix that shape completely,
and they also leave two arguments unexercised: **the second Image is null at
every one of them and the mode is zero at every one of them.**

So a non-null second image is drawn as if it were absent, and a non-zero mode
is ignored. Both readings of that Image — a destination to draw into instead of
the Graphics, or a mask for the source — are plausible, and refusing the call
would fail the one title that makes it on evidence that names neither. If a
title turns up that passes either, this paragraph is where the guess was
recorded.

**And the title says so when it does.** "This platform says so rather than
guessing" was true of this paragraph and of nothing that ran: a picture drawn
without the mask or the destination its caller asked for looks like a drawing
defect, and the guess was invisible from the frame. `drawImageEx` now writes
one line naming both arguments the first time a title passes either — once a
run, at warning level so a release build carries it too, because a call on a
draw is a call every frame. `TestDrawImageExReportsTheArgumentsItIgnores` pins
that it is one line and that an ordinary call is silent.

`XDisplay.copyLCD(Graphics, Image, int x, int y, int width, int height)` is the
neighbouring call and it copies the screen into the caller's image, which is
what `Graphics2D.captureLCD` does into a new one.

`captureLCD` is declared static, and the corpus is what decided that rather than
the shape of the class around it. Five of the fifteen local titles name it, at
eight call sites between them, and **every one of the eight is an
`invokestatic`** — read out of each class's constant pool and its bytecode
rather than from the source that is not available. It matches what the call
does: it copies the screen, not the surface a wrapper was built around, so a
title has no reason to make a wrapper first.

The consequence is worth writing down because it is a hard failure rather than a
degraded one. This VM refuses a virtual call on a static method outright —
`resolveInstanceMethod` answers `method is static: …` — so a title that reached
`captureLCD` with `invokevirtual` would stop there instead of falling back. No
local title does, and the message names its own cause, so the fallback stays
unbuilt until a caller for it turns up.

## Nothing a local title names is missing

The static scan (`internal/tools/apiscan`, and "The diagnostic report" above
for why it exists) answers one question over the whole corpus: what would a
title link against that this runtime does not have. Run over the fifteen local
archives with a live natives report, it now comes back **empty for thirteen of
them**, and what is left in the other two is not surface to build.

The entries it used to list were each on a path no local run had taken, which
is why nothing had ever failed on one. That is exactly the state worth closing
rather than watching: a class this runtime does not have is not a degraded
feature, it is `class not found` at the moment the title first touches it —
`internal/platform/skt/connector.go` argues the same thing for the connection
framework. A title that walks to its name screen after two hours of play should
not lose the session there.

### Two ways a title takes a name

This vendor has two, and a local title uses each.

**`com.xce.lcdui.TextComponent` is the title's own text buffer**, handed to the
platform's `TextComponentHandler` — a singleton a title reaches through a
static and attaches a component to. The interface's members are what settles
what the handler is: `insert`, `replace`, `delete`, `moveCursor`, `clear`,
`size`, `getMaxSize`, `getCaretPosition`, and **no way to read a character
back**. So the handset's input method never held the text. It held the
composition and sent edits, and the title kept the text — which is why
`replace` exists at all, and it is the one a multi-tap cycle needs when the
same key produces the next letter in place.

Two of the title's own classes implement the interface, and they agree on
thirteen members, so the interface is not inferred from one caller. Its own
implementations then settle each contract:

- `replace(c)` writes at `caret - 1`. That is the cycle.
- `clear()` on the *component* empties the field; `clear()` on the *handler* is
  a different thing, and the title's own `moveCursor` says which: it calls the
  handler's `clear` right after moving the caret, where a cycle still running
  would write the next letter over whatever the caret had moved to. So the
  handler's `clear` ends the composition and leaves the text alone.
- `moveCursor(int)` takes **a raw key code**, not a direction: the title
  switches on 142 and 145, which are the two the pad sends. It also inserts a
  space when the caret is already at the end, which is the component's business
  and not the platform's.

So the handler runs the shared multi-tap cycle
([`internal/textinput`](../internal/textinput/textinput.go)) against a
component it cannot read: a new character is `insert`, the same key inside the
commit delay is `replace`, `#` and the CLR key are `delete`, `*` changes the
mode, and the pad's left and right are handed to `moveCursor` as the key codes
the component expects. Whether another character fits is asked of the component
— `size` against `getMaxSize` — because the platform cannot count text it never
sees.

`keyPressed` answers whether the input method took the key, and a key it does
not take reaches the game: a title routes every key through the handler first
and its pad has to keep working while a field is on screen. Both local call
sites discard the answer, which is what a handset's own titles could afford to
do, so the answer costs nothing there and is still the right one to give.

**`getInputMode` is the one inference in this.** The five modes are a bit each,
and which bit is which is documented nowhere available here. What settles it is
the order a title draws them in: its switch maps 16, 1, 2, 8 and 4 onto
indicators 0 to 4, and the order a Korean handset showed was Hangul, capitals,
small letters, digits, symbols. So 16 is Hangul and this runtime answers 1, 2
or 8 — capitals, small letters or digits — matching what the next key press
will actually produce. If a title's indicator turns out wrong, this paragraph
is the guess to revisit.

**`XTextField` is the other way**, and there the platform holds the text, so it
is the shared editor itself rather than a cycle over someone else's buffer —
the same editor a MIDP `TextBox` on this platform types with. Its
four-argument constructor is what a name screen uses: the text it starts with,
the size it stops at, the constraints, and the Canvas it belongs to. The order
of the three is read off the one local call site, which passes `("", 10, 0)`:
10 is a plausible name length and not a constraint any of them names.

`Displayable.repaintIM` belongs to the same feature — a repaint that included
the input method's own area, which a handset drew over the screen. There is no
such area here, so what is left of it is the repaint the title expects to go
with it, asked for virtually so a Canvas gets its own.

**One defect was already sitting in the shared path**, and typing through the
vendor's field is what turned it up: a MIDP `TextBox` on this platform treated
4 and 6 as caret movement, because those digits are also the pad's left and
right for a game. In a text field they are letters, and taking them left
`ghi` and `mno` untypable. The pad moves the caret and only the pad now;
`TestTextBoxTypesThroughTheKeypad` covers both halves. The other platform's lwc
fields never had it — `isKeypadKey` there is digits, `*` and `#`, and nothing
else.

### A timer runs on a thread, because the title keeps drawing

`java.util.Timer` and `java.util.TimerTask` are the profile's rather than the
configuration's — CLDC has neither — so they are declared in
`internal/api/midp` and only a VM that installs this profile answers them. One
local title schedules a task at a fixed rate and keeps redrawing while it runs,
which is the whole reason the specification puts a task on a background thread:
invoking it on the caller would stop the frame the schedule was made from.

Each schedule gets a guest thread, the same kind a title's own `new Thread`
gets — it takes the platform's scheduler if one is installed, it dies with the
session's step budget, and it appears in a report as a thread rather than as
something the runtime is doing behind the game's back. The two cancels are
flags the worker reads before every run, so a cancel from any thread is seen
without a lock, and a task already running is not interrupted, which is what
the specification says cancel does and does not do. Fixed-rate and plain
schedules differ in what the next wait is measured from: the time the run was
due, or the time it finished.

### The two entries the scan still lists, and why neither is work

**`java.io.DataInput` and `java.io.DataOutput`** are named by a title that
writes its save through one and reads it back through the other. Neither is
declared in this runtime and neither has to be: `invokeinterface` dispatches on
the class of the receiver, and what the title passes is the
`DataOutputStream` this runtime does have. The scan sees them because an
interface method reference names its interface; the loader never does.
`TestInterfaceCallsLandOnTheStreamThatWasPassed` pins the mechanism with a
fixture that writes and reads a record through the two interfaces. Nothing in
the corpus does the one thing that would load them — a `checkcast`, an
`instanceof`, or a class declaring `implements` — and the day one does, the
CLDC 1.0 forms of both are what to declare, which is the pair without the
floating-point methods.

**`com.xce.jam.XBrowser.setNetworkMode` and `com.xce.net.Socket.setPPPPreserveTime`**
are two settings a title applies before it would open a connection. It never
reaches them here, and not by luck: the call site is behind a check on the
`m.SK_VM` property, and this runtime answers the value that skips it. There is
nothing behind them to configure either — `docs/network.md` has why no platform
here reaches a network — so the pair stays unbuilt and the property that gates
them is the thing to remember. If that property's answer ever changes, these
two become reachable in the same session.

## A heap with addresses in it

The cheat engine searches an address space. This runtime does not have one: a
MIDlet's state is Go objects in a map, and the `int` a game keeps its gold in
has no address to find, narrow down and freeze. That is why the browser removed
the cheat panel on this platform and why `session.Cheat()` used to answer nil
here.

So one is made. `internal/platform/skt/heapmap.go` walks the object graph and
gives every object, every array and every loaded class's statics a span of a
synthetic space; `heapread.go` renders a span from the fields each time it is
read and decodes a write back into the field it lands on. Nothing is copied and
nothing is cached — a scan sees the game's state as it changes, and a freeze is
a write to the object rather than to a shadow of it. The engine above is
unchanged: it asks this the same three questions it asks the two ARM platforms.

**Watching writes needs a different trap, and the interpreter is it.** On the
two ARM platforms a watch is the core noticing a store instruction against a
real address. Here there is no instruction and no address: a field is the
interpreter assigning into a map, and the address is one this map invented. So
the interpreter reports its own stores — `putfield`, `putstatic` and the array
stores hand a `jvm.StoreEvent` to the VM's store observer — and
`internal/platform/skt/watch.go` turns one of those back into the address a
search found, by the same identity the map is keyed on.

Three things about it are worth keeping.

**It costs nothing when nobody is watching.** The observer is installed by the
first watch and removed with the last, so a title being played rather than
investigated pays one nil check per store: 4,000 ticks of a local title take
2.03 seconds with the hook compiled in and 2.03 without. While a watch is
installed a store costs a lock and a map lookup on the object's identity, and
only a store to an object the search has actually mapped costs more.

**The writer is named rather than addressed.** `pc 0x40183a2c` means something
where the code is in the address space; here it is class files, so a hit
carries `com/example/Game.tick+42` instead. That is `cheat.WatchHit.Site`,
which the ARM platforms leave empty, and the console and the panel show
whichever they were given.

**The control is offered by the platform's own answer.** `CanWatch` is asked
now, and the answer travels in the `started` message. It had been asked by
nobody while this platform could not watch, which is how the browser panel came
to offer the control here and then fail its poll every interval — the hits call
threw, the error reached the page, and the candidate refresh queued behind it
never ran. The refresh runs first in that poll now regardless: an optional
feature must not be able to stop the one the panel is for.

**The addresses are grouped by shape, not by allocation order.** Every instance
of one class lands in that class's own arena, so the region listing names what
a hit is *in*:

```
0x10000000-0x100003e8      1000  statics
0x10010000-0x100143e8     17384  [B
0x10030000-0x10030378       888  [Ljava/lang/Object;
0x100b0000-0x100b0050        80  bd
0x100f0000-0x100f0008         8  rpg/RPGHero2
```

"the address is in `cj`" is something a search can act on where "somewhere in
the heap" is not, and it costs nothing: an arena is a bump allocator over 64KiB
chunks of a 32-bit space, and a shape with three instances wastes the rest of
one chunk of address space and no memory at all.

Three properties make the addresses worth what a cheat needs them to be worth.

- **They are stable.** A span is keyed by the VM's own object identity and the
  allocator never reuses an address, so an object keeps its span for the life of
  the session. A value found on one screen is at the same address on the next,
  which is the whole point of a scan. Measured on one local title, the map grew
  from 18 spans and 16KiB at the title screen to 22 spans and 21KiB in play, and
  every address from the first walk was still where it had been.
- **They do not keep the game's garbage alive.** A span holds a `weak.Pointer`,
  so mapping an object does not stop it being collected and a dead one's span is
  dropped on the next walk. A **frozen** address holds a strong reference
  instead — `serviceCheat` retains exactly the objects the freeze list names
  after every Host pass — because a freeze the collector can silently switch off
  is worse than the memory it costs. That is the same decision KTF made when it
  passed frozen addresses to its collector as extra roots.
- **A record is laid out for a search, not for a JVM.** Every field takes four
  bytes except a long or a double, which take eight, so an arena of one class's
  instances is a table the four-byte default stride walks; a `short` sits sign
  extended in its four bytes and is still found by a two-byte search. Array
  elements take their *natural* width instead — a byte array is how a game packs
  its map data, and spreading it over four bytes each would hide every pattern
  in it.

A reference field reads as the address of what it points at, so a `dump` can be
followed from one object into the next, and it refuses to be written: writing
one would mean inventing an object. A write narrower than the slot it lands in
is a read-modify-write, so a one-byte `set` changes one byte of an `int` the way
the same write against a real address space would.

### What the walk starts from, and what it therefore cannot find

The roots are every loaded class's static reference fields, every thread object
the VM holds, and what this runtime holds in Go: the MIDlet, the Display and the
Displayable being shown, the runnables `callSerially` was handed, the Canvas
mid-paint, and **every Displayable the runtime has state for** — a title keeps
its menus and its map screen as objects it comes back to, and between visits
nothing static points at them.

An object reachable only from a running method's local variable is not in that
set. Nothing durable lives there — a game's own state is field-reachable, which
is why it survives the method that made it — but a value that appears in a scan
and then vanishes from the map is this boundary rather than a bug.

**Reading a static must not initialize its class.** `VM.StaticField` runs
`<clinit>` first because that is what a `getstatic` does, and a walk that
touched every loaded class through it would run guest code the game never asked
to run, from whichever goroutine was inspecting. `VM.StaticFieldValue` reads the
map and answers "not set" instead, which is exactly right: a class that has not
initialized has no values, and zero is what its fields hold.

### What it does not do

There is **no watch**. A watch names the instruction that wrote an address, and
finding that here would mean instrumenting every `putfield` and every array
store in the interpreter. The engine answers `cheat.ErrWatchUnsupported` and a
Host reads that as "no watch control on this platform" rather than as a failure;
`Session.CanWatch()` is what the panel asks first.

There is also no object *header* in a span — no class word, no length word, no
allocation metadata — because none of that exists here to present. A span is
exactly the declared fields, or exactly the elements.

### Driven end to end

`wfeature runskt <game.zip> -cheat` attaches the same console the WIPI paths
use, and `TestLocalCheatProbe` drives the browser's socket path against a local
archive. On one local title: `regions` lists 22 spans, `scan unknown` over 5,222
candidates narrows to one under two `scan changed` passes, `read` shows it
counting up live, `freeze` holds it at 555 across seconds of the game writing
over it every tick, and `unfreeze` lets the game's own value come back. The
fixture-level tests are `TestHeapMap*` in `internal/platform/skt`.

## Deliberately incomplete

- **`SISImage` does not decode.** The container's frame and object tables are
  not documented anywhere available here and there is no SKT archive in this
  repository to reverse them from, so `getFrame` and `getObject` answer null
  and the paint methods do nothing. Answering a blank image instead would let
  a game draw nothing while believing it drew a sprite. The reference
  implementation is stubs for the same reason.
- **`Graphics3D` has no rasterizer.** It keeps and reports its render state
  truthfully; `Object3D` keeps a real mesh and a real transform (the matrix a
  game reads back after `translate`/`rotate`/`scale` is correct), but nothing
  draws it. The reference implementation is in the same position.
- **`SMS`, `Call`, `PhoneBook` have no radio.** Reads answer empty and sends
  are refused. Reporting a delivered message a game can never receive a reply
  to is worse than reporting none. `docs/network.md` carries the same decision
  for the MIDP connection framework and for the other platforms.
- **The input method types Latin and digits, not Hangul.** Both of this
  vendor's text surfaces now take the keypad — see "Two ways a title takes a
  name" — with the multi-tap editor every text field in this runtime shares.
  What a handset also did inside that input method was compose Hangul out of
  the jamo on the keypad, and nothing here does: a title that wants a Korean
  name gets Latin letters and digits in the same field. It is the same gap the
  MIDP `TextBox` and KTF's lwc text components have, because it is the same
  editor.
- `BackLight` and `Vibration` keep their state and report it back; there is no
  hardware to drive.

## Validation

The local corpus has an opt-in probe of its own now —
`WFEATURE_SKT_ACCEPTANCE=1 go test -run TestLocalSKTArchivesBootAndPaint
./internal/platform/skt` — which starts all fifteen archives, ticks each for
300 ticks and requires a frame with something lit in it. All fifteen pass.
[`testing.md`](testing.md) has what it does and does not claim.

There is still **no SKT game archive in this repository**, so nothing here is tested
against a real title in CI — only against a newly authored fixture
(`internal/platform/skt/testdata/skvm.jar`) that exercises each surface and
checks the values that come back. The fixed-point scale, the file semantics
and the pixel operations are the parts a real game would notice first if they
were wrong, so those are what the tests pin.

The frame-loop contracts above were found the other way round — by running local
titles with `runskt` and reading the frames — and each is pinned by a fixture
test afterwards: `TestCallSeriallyRunsOneRunnablePerPass` and
`TestCallSeriallyLoopAdvancesOneStepPerPass` for the serial queue,
`TestScreenGraphicsStaysUsableAfterPaint` for the Graphics that outlives its
paint, and `TestXDisplayPublishesFramebufferSize` for the screen fields.

Regenerate the fixture with:

`$stub_dir/classes` is the signature classpath from
[`testing.md`](testing.md); build it once per session.

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

The class library itself is declared in `internal/api/skvm/definitions.go` and
installed by `skvm.Define`, so changing it is a Go change with nothing to
rebuild.
