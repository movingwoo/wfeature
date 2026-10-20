# Session investigation history

This file preserves implementation investigations and dated validation records.
Statements about current behavior, missing APIs, corpus size and planned work
apply to their original revision and may be superseded. The maintained overview
is [session.md](../session.md). This consolidation adds no execution or
acceptance evidence.


## Contents

- [Implementation and validation record](#implementation-server-sessions)
- [The shape](#implementation-the-shape)
- [What travels](#implementation-what-travels)
- [Pacing and frame skipping](#implementation-pacing-and-frame-skipping)
- [Saves](#implementation-saves)
- [Cheats](#implementation-cheats)
- [The WebAssembly path is gone](#implementation-the-webassembly-path-is-gone)
- [What has been driven, and what has not](#implementation-what-has-been-driven-and-what-has-not)
- [A game that ends is not a game that failed](#implementation-a-game-that-ends-is-not-a-game-that-failed)
- [A game outlives its socket](#implementation-a-game-outlives-its-socket)
- [Vibration](#implementation-vibration)
- [What is not solved](#implementation-what-is-not-solved)
- [Automatic offline authentication](#implementation-automatic-offline-authentication)
- [Frame bandwidth — 2026-09-24](#frame-bandwidth-2026-09-24)
- [CPU saturation investigation (2026-09-23)](#cpu-saturation-investigation-2026-09-23)

<a id="implementation-server-sessions"></a>

## Implementation and validation record

A phone could not run the browser build fast enough to play. The same build and
game measured about fifteen times slower on an iPhone than on the desktop that
served it (2.3fps against 19.2, with 86% of the phone's main thread inside the
interpreter). The cost was Go's WebAssembly backend rather than the emulator:
pure register work carried no penalty, while an array index plus a branch cost
2.5x and the interpreter 6x. The interpreter's state is arrays, so it cannot
live in wasm locals, and three rounds of tuning moved it by 0–4% (see
[`armcore.md`](armcore.md#implementation-arm-core-implementation-status)).

The emulator therefore moved off the phone onto a server that is left running,
and the phone draws what it is sent.

| | |
|---|---|
| frame, PNG, measured over a real game | ~13 KB |
| bandwidth at 20fps | ~2 Mbps |
| server cost of one session (debug build, M1) | ~⅓ of one core |
| phone's remaining job | decode a bitmap, draw it |

<a id="implementation-the-shape"></a>

### The shape

```text
browser                          server
  │  GET /            ───────▶   embedded client files
  │  WebSocket /api/session ─▶   one controlling connection per game, retained when away
  │                              internal/session drives the platform
  │  ◀── binary frame (PNG)      internal/webhost encodes and sends
  │  ◀── text JSON               started / audio / stats / results
  │  ─── text JSON  ────────▶    start / resume / key / speed / scale / cheat
```

- `internal/session` is the platform-agnostic driver: inspect, start, tick,
  send a key, take a frame. It absorbs the differences between KTF, LGT and the
  two MIDP platforms, so a Host never switches on platform.
- `internal/webhost` owns the socket, the protocol and the encoder.
- `internal/wsproto` is the framing (see [`architecture.md`](../architecture.md#session-transport)).
- `web/session.js` is the client: it opens the socket, draws frames with
  `createImageBitmap`, and replays audio events onto the page's synthesiser.

One connection drives one session, and a session outlives its connection
without a time limit; see "A game outlives its socket" below.

<a id="implementation-what-travels"></a>

### What travels

Text messages are JSON and small. Frames are binary and are the only thing sent
at rate; a PNG carries its own dimensions, so a frame has no envelope.

| From the page | Means |
|---|---|
| `start` | run this archive, named by its `games.json` path, optionally on a named screen size |
| `key` | one press or release, with the MIDP-style code the keypad has always sent. A page may still send `repeat`; it is dropped, for the reason in the section below |
| `speed`, `scale` | the settings panel's two knobs. `speed` reaches every platform; see "One setting, three clocks" in [`cli.md`](../cli.md) for what each one scales |
| `cheat` | one panel operation, or a text console line |
| `report` | write the session's diagnostics under `logs/` |
| `stop` | end the game without closing the connection |
| `resume` | find the browser token's game; `takeover: true` only to transfer control explicitly from another connected page |
| `park` | release control and retain the game without closing this socket |
| `ping` | check connection liveness independently of guest execution |
| `text` | open, commit, or cancel a native text edit for a supported guest field; see [native text input](../native-text-input.md) for identifiers, constraints and support limits |
| `pointer` | one touch: `press`, `drag` or `release` with `x` and `y` in the guest's own screen. The page removes its own canvas geometry first, because only it knows what it put between the finger and the game |

| From the server | Means |
|---|---|
| `ready` | the session will take a game, and which build profile is running |
| `started` | the game is up, with its identity, the screen it is actually drawing, and whether a touch would reach it (`can_touch`) |
| binary | one finished frame, PNG |
| `audio` | a batch of sink calls in the order the guest made them |
| `stats` | frames sent, frames dropped, average tick cost, average frame size |
| `exited`, `error`, `result` | the game ended, something failed, an answer to a request |
| `resumed` | no retained game, or `occupied: true` when another page controls it |
| `detached` | control moved to another page; stop presentation and require explicit action to take it back |

`ready` carries the server's build profile because the page has a developer's
half (the run log beside the screen and the button that saves a report) that a
release does not use. The binary serving the files is the one thing that knows
which build is running, so the answer is the server's rather than the page's
guess.

<a id="implementation-one-touch-can-be-a-run-of-keys"></a>

#### One touch can be a run of keys

A finger dragged across the keypad presses each key it crosses and releases the
one it leaves, so a thumb rolled around the direction pad sends 2, 6, 8, 4 in
turn without lifting. This lives in the page; the server sees ordinary `key`
messages and cannot tell a slide from four separate presses.

A slide need not begin on a key. A handset thumb presses whatever it reaches, so
a finger that goes down on the screen or in the margin beside the pad is
followed too, and the first key it crosses is pressed. For that reason
`pointerdown` is watched on the document rather than bound per key, and what the
finger went down on only decides whether a key is pressed at once.

A slide runs across the single `.keypad-pad` grid, including its bottom row. The
band above it is outside the sliding region: Opts and its three assignable cells
are held individually until release. Editing changes each cell's assigned key;
its region stays fixed. Type1 through Type3 provide shipped layouts and Type4
starts empty. Each type keeps its own cell edits and size settings, and the
shared keypad editor changes both and resets the selected type. Non-key
controls, text editors, logs and cheat panels keep their normal pointer
behavior. `web/keypad.test.mjs` checks the grid and band distinction.

Two details make it behave. A press that starts on a key captures the pointer,
because otherwise the moves and the release stop arriving once the finger leaves
that button; with capture the event no longer says what is under the finger, so
the point is asked with `elementFromPoint`. A finger that started beside the
keys needs no capture: its events belong to no button and the window receives
them wherever it goes. The key being left is released before the next is
pressed, since a game reads them in arrival order and one finger never means two
keys held at once.

Off the keys a finger holds nothing, and sliding back on presses again. Two
fingers reaching the same key (one sliding onto what the other holds, or onto
the second button a key is printed on, which the type-2 and type-3 layouts both
have) are one press between them, released when the last leaves. That
bookkeeping is `web/key-holds.js`, kept out of the DOM so
`web/key-holds.test.mjs` can drive it directly; what is under the finger stays
`app.js`'s question.

<a id="implementation-key-spacing-while-changing-the-leftright-share"></a>

#### Key spacing while changing the left/right share

The keypad's left/right slider changes column and button widths together. The
visible gap between adjacent keys stays at its 50% value for the current key
size and viewport. At 50%, buttons still use the width cap derived from the key
size, and any space left inside those columns becomes a fixed inset on every
key. Moving the split then adds or removes button width without widening that
inset. Previously the columns grew while their buttons stopped at the cap,
leaving large gaps on the wider side.

The middle column still follows the wider side, and the top band keeps seven
equal columns. Key heights and vertical spacing are unchanged.

<a id="implementation-the-keypad-carries-one-soft-key-and-it-is-the-menu-key"></a>

#### The keypad carries one soft key, and it is the menu key

A handset drew two labels in the bottom corners of its screen with a key under
each, and titles of this era used the left one for their in-game menu. The page
had no way to press it: the keypad's twelve keys, the pad, CLR and Call were all
it had, and `wfeature key soft1` from the command line was the only way to send
one (`cli.md`, "Key names"). The keypad's top row now carries a `Menu` button in
the spot the layout toggle used to hold; that choice moved into the settings
panel, where a thing chosen once belongs.

**One number serves every platform.** `MH_KEY_SOFT1` is -6 and the MIDP soft key
a MIDlet of this era compares against is -6, so the page sends -6 and the WIPI
paths and the MIDP one each take it unchanged. It is the only key on the page
that needs no translation anywhere, and the only negative code the page sends,
which is why the protocol test drives one.

What a title does with it is its own business, and three answers were seen:

- **An LGT Java title opens its in-game menu on it** (계속하기, 게임방법,
  환경설정, 메인메뉴, 게임종료, with `EZ:SELECT` in its footer). That run proves
  the button end to end, and `lgt.md` has the route to it.
- **A KTF Java title's slot screen leaves the frame unchanged.** These titles go
  back on CLR and confirm on fire, and draw `BACK:CLR` to say so; the key is
  delivered and ignored, which is the title's answer rather than a gap.
- **The earlier KTF package drops it before the guest**, because nothing has
  established what that handset numbers its soft keys (`ktf.md`, "The keys with
  no code"). The button is inert on those titles until a run names the code.

On the MIDP platform the key never reaches a `Canvas`'s `keyPressed`: it runs
the first command, or opens the command menu when there are three or more. A
`Canvas` MIDlet that added no commands therefore answers the button with nothing
(`lcdui.md`, "Commands, soft keys, and the menu").

<a id="implementation-a-held-key-repeats-on-the-handsets-clock-not-the-keyboards"></a>

#### A held key repeats on the handset's clock, not the keyboard's

A handset repeated a held key, and the specification writes the cadence down:
`KEYREPEAT`, "600:250", the first repeat after 600ms and then one every 250ms. A
browser also repeats a held key, but neither the timing nor the meaning matches:
the operating system sends a keydown roughly thirty times a second, at a rate
the user configured, and it says nothing about a handset.

The page used to forward those as a `key` message with the action `repeat`, and
the result depended on the platform. Two platforms have no repeat event (a
Clet's are a press and a release, and so are the earlier KTF package's), and
there a forwarded repeat became a second press with no release between, the
fault `lgt.md` ("A Host repeat is not a second press") measured and closed. The
other two have the event (WIPI Java's `keyNotify` type 3 and a MIDP `Canvas`'s
`keyRepeated`), and there the repeat was passed straight through: the title got
the user's keyboard cadence, thirty a second, where a handset gave four.

**So a repeat is made here rather than forwarded.** The page sends nothing for
an operating-system repeat, and this layer runs the handset's clock instead
([`internal/keypad`](../../internal/keypad/repeat.go), `Session.repeatDue`):

- **What repeats is what the guest believes is down**, which the pad answers
  (`Pad.Held`) rather than the Host. After a thumb rolls off one direction onto
  another, the key the Host holds is not the one the title was given, and
  repeating the Host's would name a key the game does not think is pressed. A
  different key restarts the wait, because pressing one is what a roll is.
- **At most one repeat per tick, and the overshoot is carried.** A Host that
  stalled (a long tick, a paused session) owes the missed repeats to nobody:
  delivering the backlog at once is the burst that read as a run of taps. But
  measuring the next interval from the tick that noticed rounds every repeat up
  to a tick, and a tick is not free: on the WIPI Java platform, whose rounds are
  the more expensive, a four-second hold delivered 18 repeats where the
  handset's number is 22. Carrying the overshoot (up to one interval, beyond
  which it is a stall rather than jitter) restored 22, and the MIDP platform,
  whose ticks are a frame, gave 14 in four seconds either way.
- **The clock is the guest's.** The wall interval is scaled by the speed
  multiplier, like the guest's own clock and the waits between its callbacks: a
  handset running twice as fast repeated twice as often.
- **The platforms with no repeat event get nothing**, as before.

Doing it here rather than in the page has two effects: the on-screen keypad now
repeats as well (a thumb holding a touchscreen button is a held key, and the
page never sent a repeat for one), and the cadence no longer varies with the
keyboard in front of the emulator.

Five of the fifteen local SKT titles override `keyRepeated`, so there are
callers. The WIPI Java side cannot be asked the same question, because those
titles arrive compiled with their method names gone (`ktf.md`, "What the corpus
names, and what it never asks for"); what a title does with type 3 is known only
by running one.

Both were run. Holding a direction on a local title of each reached the game's
own class at the handset's cadence: type 3 into the card stack on one, and
`keyRepeated` on the Canvas subclass of the other, followed by the release. What
a title *does* with a repeat is its own business and differs between them.

<a id="implementation-the-speed-belongs-to-the-game-not-to-the-page"></a>

#### The speed belongs to the game, not to the page

The multiplier was one value shared by every game, but the setting is not a
taste: it corrects for **how much of a game's frame period was its own drawing
on the handset**, which is a fact about that game. One local title issues a
couple of hundred platform draw calls a frame and asks to be woken 10ms later;
the drawing is native code here, so almost the whole period comes back and it
needs a quarter speed to look right. The title beside it rasterises in its own
guest code, 2.2 million instructions a frame, and needs none. `docs/ktf.md` has
both measurements.

A shared setting meant changing it by hand on every switch, and forgetting meant
a game running at four times its own pace with nothing on screen to say why. It
is now stored per game, keyed like the screen. Unlike the screen it applies to a
running session, with nothing to restart, and the menu follows whichever game
the setting is about: the one the list shows before a game starts, the running
one afterwards.

The key an older page used for the shared speed is still **read** for a game
that has no speed of its own, and never written again. That direction is the
conservative one: somebody who slowed a game down before the change finds it
still slow, where ignoring the old key would silently speed it up. The first
choice made for any game writes that game's own value, so the shared one fades
out. `web/game-speed.js` holds the rule and its tests.

<a id="implementation-the-screen-is-part-of-starting-a-game"></a>

#### The screen is part of starting a game

A `start` may carry a `width` and a `height`, which the page takes from the
settings panel. Almost every title here was drawn for 240x320, the default, so
the field is optional and absent from an ordinary start. A few were packaged for
a smaller phone and **load their artwork by the screen they are given**: one
local archive branches on the width and asks for a set of images its own package
does not contain, so on a 240-wide screen it cannot run at all. `docs/skvm.md`
has that title's branch.

**A start with no screen in it is not the same as a start that asked for the
default.** On the MIDP platform the archive answers first: an absent `width` and
`height` let `skt.PackagedScreen` read the handset from the archive's own
resource names, so the title above starts on the phone it was built for without
anyone changing a setting. A start that carries a size is honoured as before.
The page omits the field when the stored size is the default, so choosing
240x320 by hand reads here as "no answer" and that title still starts at
176x208, a size it can run at. Two local titles answer this way, and the menu
offers neither of them that size; one of the two cannot run on any size the menu
does offer, which is why the archive answers before the settings panel.

**Since 2026-10-20 the page sends a chosen size, the default size included.**
A KTF start without a size now runs on the handset the descriptor declares
([`history/ktf.md`](ktf.md#implementation-the-handset-a-title-was-packaged-for-is-in-its-descriptor)),
so leaving 240x320 out of a start no longer meant 240x320. The menu's default
became `자동`, which is the start without a size, and every size travels. A
size the earlier page stored stays chosen, 240x320 included: that page wrote
its key only when a player changed the menu, so a stored 240x320 is a player who
tried another size and went back, and reading it as `자동` would move a title
that declares a smaller phone off the screen they settled on. A stored value
the menu does not offer reads as `자동` in the menu and in the start alike, so
the two never disagree. The two titles above still start where their archives
say unless a player picks a size for them. The picker names the symptom under
the start button, and a change made while a game runs comes with a restart
beside the note, because a note with nothing to press is what made the setting
look broken.

**The server says what `자동` takes, and the page compares screens.** Every
started message carries `auto_width` and `auto_height`, the screen a start
without a size takes for that game (`session.Summary.ScreenWidth`), on a fresh
start, a quick load and a return alike. The first version of the page learned
it from a start it had made without a size and remembered what the menu said at
that start, and both went wrong the moment `sessionStarted` ran again: a return
after a change in the menu took the new choice for the running game's, so the
note and the restart disappeared while the game stayed on the old screen, and a
game started on a size and returned to after picking `자동` taught `자동` that
size. The note now appears when the screen the menu's choice would start on
differs from the one the game is on, so picking the size `자동` already took
offers no restart that changes nothing. The restart starts the game again
without waiting on the game list: a list that failed to load, or a last-game
record that could not be saved, used to leave the game stopped and not
restarted.

**The other platforms have nothing to read, so they keep their rules.** The
same reports asked whether LGT and SKT could start on their packaged handset
too, and the archives cannot say:

- An LGT `app_info` has no size; 18 of 125 carry a `Resolution` key, all empty.
  The code is a native module, so there are no bytecode constants to read.
- An SKT descriptor has no size, and the resource-name rule above answers for 3
  of 91 archives. Probed at 240x320, three more are small packages it does not
  see — one fits only 128x160, two fit 176x220 — and about six centre a small
  picture in a black frame. The right size differs between them, so no single
  fallback serves them.
- Inferring the width from the archive's own images (every PNG, including those
  inside the titles' packs) was checked against the KTF declarations: of 277,
  91 agree, 37 disagree and 149 give no answer.
- The download address an LGT `app_info` keeps names the handset it was fetched
  for (`device_id`, 108 of 125, six models), and an SKT descriptor's carries a
  platform code; either would need a table of handsets from outside the
  archives.
- Reading a running game fails on the very titles reported: they draw across
  the whole screen. All seven LGT titles probed were still on a notice at 600
  ticks, short of any screen worth judging.

LGT therefore starts on 240x320 and SKT keeps its resource-name rule; the
hint and the restart are how a player reaches another size there.

Rules that keep the setting honest:

- **The size the server refuses is the size no handset had.** Anything outside
  32..1024 in either direction is answered with an error rather than clamped: it
  can only come from a page this server does not serve, and starting a game on a
  screen nothing was drawn for is worse than not starting it.
- **The screen range is the server's and the page mirrors it.** A stored size
  outside 32..1024 is treated as absent by the page rather than sent on to be
  refused. Previously the page checked only a floor, so a value from an older
  page or from edited storage reached the server and came back as an error
  instead of falling back to the default.
- **`started` reports `can_watch`**, whether this platform can name what wrote an
  address. The panel learns this before offering the control rather than
  discovering it as an error; `docs/skvm.md`, "A heap with addresses in it", has
  the platform that answers no and what asking anyway used to cost.
- **`started` reports the screen the platform took, not the one the page asked
  for.** All three platforms honour the request, but a page laid out for a size
  the game is not drawing would put the picture in the wrong place, so the
  frame's own size is what the page reads back.

**A screen is one answer, and it has to reach every surface a game can ask.** A
WIPI title asks the platform how big its screen is, and asks again, by another
route, where the pixels are and how far apart the rows sit. On KTF those are
`MC_grpGetDisplayInfo` and the screen framebuffer record; two local titles read
the size from the first and write straight into the buffer from the second. A
new size given through one route and not the other puts every row after the
first at the wrong offset, and the picture shears into diagonal bands that look
like a decoder bug. `internal/platform/ktf`'s `Client.SetScreen` is therefore
the single place both answers are read from, and `screen_test.go` fails if they
disagree. The Java surface (`Display.getWidth`, a `Card`'s bounds) reads the same
answer, because a title laying out against one size while the platform draws at
another is the same fault.

The page remembers the choice per game, because it is a property of the title,
and applies it at the next start: a MIDlet reads its screen once, when it starts.

**This setting does not make the picture bigger.** That is `scale`, which
magnifies a finished frame and is a taste; this setting changes what the guest
is told, and therefore which artwork it loads and how it lays out a screen. When
a report says a title "looks wrong", an old game whose proportions are off on a
modern display wants `scale` and the page's letterboxing, while a title that
draws into a corner of the screen or asks for images it does not carry wants
this setting.

**A screen larger than the default is a handset too, and the menu offers two.**
The carrier standardised on 240x320 for this library's generation, but later
handsets shipped 240x400 and 320x480, so those are the sizes above the default,
handsets rather than round numbers, as the two below it are. Every local title
was run 400 ticks on each of them, and on the 480x800 screen of the last
generation, which the menu does not offer: this library was not written for that
phone, and a screen that large is a magnifier's job.

| Screen | What the local library did |
|---|---|
| 240x400 | every title ran; the one already-failing title is unrelated |
| 320x480 | every title ran but one, which throws from its own code |
| 480x800 | the same one title, and no other — measured, not offered |

The one that throws indexes a fixed-length row of tiles sized for a 240-wide
screen and walks one past the end as soon as the screen is wider: a guest
exception, not a platform fault, and the reason the setting is per game and
reversible. The rest divide in two, both the game's own answer: most lay their
scene out across whatever they are given, and some paint their 240x320 picture
into the top-left corner and leave the rest black.

**A larger screen is shown smaller, not given more of the page.** The canvas
occupies a hole of the default screen's 3:4, capped by the height left once the
keypad under it is subtracted, whatever the game runs on. A frame of another
shape (the menu offers 3:5 and 2:3 as well) is fitted into that hole by
`object-fit: contain` and keeps its own shape, and what does not fill is bezel.
The page used to size the hole from the frame, and a 480x800 screen sized that
way put the keypad below the fold.

<a id="implementation-the-first-row-of-the-page-has-to-clear-a-status-bar"></a>

#### The first row of the page has to clear a status bar

The page asks for the whole display (`viewport-fit=cover` in the viewport meta)
so that the keypad's bottom row can clear a home indicator with
`env(safe-area-inset-bottom)`. On an iOS home-screen app the request was also
granted at the top, and nothing was reserved there: the game screen is the first
thing in the column, so its first rows sat under the clock and the battery. Two
screenshots from a phone show a title's artwork behind the status bar.

The same web view then reports a viewport **shorter** than the display it filled
(`100dvh` is the display less the status bar while the page is laid out from the
top edge of the glass), so the column's last rows hang past the bottom of the
screen. Here that cost only the keypad's bottom padding, which is why it was
invisible; a layout with anything else down there would have lost it.

Two changes, either of which would do alone:

- `apple-mobile-web-app-status-bar-style` is `black` rather than
  `black-translucent`. An opaque bar puts the page below it and makes the
  viewport accurate, so the canvas keeps its full size.
- `--safe-top` is `env(safe-area-inset-top, 0px)`, subtracted from
  `--usable-height` and spent as `padding-top` on `.stage-main`, matching the
  `--keypad-chrome` pair at the other end. It is zero on every browser that
  already lays the page out below the bar, and covers a browser that ignores the
  meta and hands the page the whole screen anyway.

The page's module tests cannot exercise either: `env()` has no value outside a
browser and the meta is read by the platform. They are checked on a phone.

<a id="implementation-what-a-frame-costs-on-its-way-out"></a>

#### What a frame costs on its way out

A presented frame is handed from the emulator's goroutine to the writer's and
compressed there, and both halves used to allocate more than they needed.

`png.Encoder` builds a fresh compressor per call unless it is given a buffer
pool: 1.25 MB and thirty allocations a frame, against 8 KB and none with one. The
pool is shared across sessions, because the buffers are interchangeable and a
household server runs few sessions at once. The bytes a page receives are
identical either way, which `session_test.go` pins.

The frame is no longer copied on the way to the encoder. Every platform answers
`session.Frame` with a picture of the caller's own rather than the one it is
still drawing into, so the copy was a copy of a copy, a third of a megabyte a
frame. That contract is the point: a Host holds a frame across ticks and hands
it to another goroutine, and one platform had been answering with its live
conversion buffer while the MIDP surface handed out the bytes its own guest
threads paint into. Both now copy where they must, under the lock they already
hold.

<a id="implementation-a-shed-message-is-counted-twice-on-purpose"></a>

#### A shed message is counted twice, on purpose

Audio and statistics are droppable: when the socket is behind they are thrown
away rather than made to wait, because the emulator waiting for the link is the
failure the pacing exists to prevent. Two consumers want to know how much was
shed, and they want different numbers.

The page wants a rate. Its statistics message carries what was shed since the
last one, so that counter is emptied every time a report is sent, and a run of
zeros after a burst says the connection recovered.

The session report wants the whole run. Reading the page's counter for it
answered with however little had been shed since the last report, so a session
that shed thousands in its first minute and none afterwards reported nothing.
`shedTotal` is the counter it reads, and it is never emptied.

<a id="implementation-pacing-and-frame-skipping"></a>

### Pacing and frame skipping

The emulator loop ticks with a budget and then waits for however long the guest
asked to be left alone; a guest that asks for two milliseconds and is given
sixteen loses the difference from every wait, and it takes one between every
pair of frames. It waits on the command channel rather than sleeping, so a key
does not wait out the game's idle time.

**The budget is 32ms here and 8ms in the browser, and the difference matters for
more than latency.** The browser's Host shares a thread with drawing the page, so
a long entry is a long freeze; nothing here shares anything. The budget still
bounds how long a key waits behind a busy game, but an entry that stops on its
budget is also read by the platform as saturation, because in a browser that is
what it means. On a server that re-enters immediately it means only that the
budget is small, and a budget short enough to cut ordinary entries has the
session giving up frames on a machine with a core to spare. `HostCosts.PaintLoad`
is that reading, and it is in the debug report: under 1 is a Host with time to
spare, over 1.1 is one that starts dropping paints to keep the game's logic on
schedule.

Measured on an M1 with a real title: 0.04–0.20 at every magnification, no paints
dropped, 19fps. Driven at sixteen times speed, the same session reports 1.57 and
drops paints as designed, holding the logic at 16x while the picture falls to
15fps.

**A round with no logic in it keeps its paint whatever the load says.** The
trade works only when there is something to trade for, and on KTF the paint can
be the round's only work: one title runs its frame loop from its card's paint and
advances its client thread's next wake-up inside it. Skipping starved that
thread, which then read as work waiting and spun the emulator. See
[`ktf.md`](ktf.md#implementation-what-frame-skipping-cannot-pay-for).

LGT and the MIDP runtimes have no clock of their own and never report themselves
idle. In a browser that went unnoticed, because the page ticked them from its
frame clock; a server that believes "no wait" spins a core and runs the game as
fast as the machine allows.

**For the MIDP runtimes `session.FramePace` is the answer, and for LGT it was the
wrong one.** Those runtimes read the wall clock directly, so a fixed wait between
ticks only decides how promptly deferred work runs. LGT's guest clock is
virtual: a tick moves it by the session's tick and nothing else does. A fixed
wait there sets the game's *speed*, not its responsiveness:

    speed = tick / (host tick cost + fixed wait)

which is right only when the tick cost happens to land on one number. Measured on
an M1 with a real title: 1.79–2.48x through the menus, where a tick cost 10ms. On
a slower server the same title ran at 0.68x in-game, where a tick cost 57.7ms:
uniform slow motion with a steady frame rate, because nothing was dropped and the
game was told less time had passed than had.

`lgt.Session.TickFor` anchors to the wall clock instead: a tick is due every
session tick of real time, and the wait is whatever is left of it. Measured at
1.00x steady, 20 ticks a second. The wait is zero once a tick overruns (a tick
costing more than it represents cannot be bought back), and the debt is capped
at one tick so that a world load inside a single call is not repaid by sprinting
through the scene after it. `lgt.Session.Tick` still steps without pacing, which
is what the CLI batches with.

**The debug report is the only window onto a phone.** No profiler attaches there,
so `tick cost:`, `over time:` and the per-phase `worst` lines are the whole
picture of what a handset spends, and a report from the phone beside one from the
desktop is how a difference is found.

One trap makes that comparison lie: **a phase average is not divided by the run's
rounds.** With frame skipping on, paint runs in only some rounds, so dividing its
total by every round reports a paint several times cheaper than it is; this once
read as "the phone paints as fast as the desktop" when it was painting a third as
often. Divide a phase by the rounds that ran it.

A frame is pushed to the encoder only when the flush counter moves, and the
channel holds one. When it is full the frame is dropped and counted: the game has
already moved on, and a backlog of stale pictures makes a phone that is behind
later still. Encoding runs on its own goroutine, so compression costs frames
rather than guest speed.

<a id="implementation-saves"></a>

### Saves

The emulator and the save tree are on the same machine, so saves never cross the
network: a session writes through `backend.DirectorySaveStore` into the same
`savedata/<profile>/<platform>/<owner>/` tree the native CLI and the save API use.
The page no longer preloads anything before starting a game. The save API remains
for the CLI's `DirectorySaveStore` layout, which both Hosts read.

<a id="implementation-one-games-saves-belong-to-one-session"></a>

#### One game's saves belong to one session

A save directory is named by the archive, not by the page that opened it, so two
pages that start the same game get one directory and two emulators writing into
it. The store does not notice: each session reads the files when the guest asks
and writes them back whole, so the second writer wins and the first player's
progress is lost with nothing reported. This is the ordinary case: a phone and a
desktop against one household server, or one browser with the game open in two
tabs.

So **a session claims its save directory for as long as it holds the game**, and
a start that cannot get the claim is refused with a message naming the holder
(`internal/webhost/saveclaim.go`). The claim belongs to the game rather than the
connection: it is taken when a game starts, released wherever the game is closed,
and travels with a parked game, which still owns the files it will write when
its page comes back.

A fresh start over a parked holder requires confirmation before its retained
progress is discarded, and an active holder is refused immediately. Reloading a
page resumes by browser token, so no fresh-start grace period is needed.
WebSocket starts use the atomic admission check in `startapproval.go`.

**The save API takes the same claim for the length of one write.** A
`PUT /api/saves/<owner>/<key>` into a directory a game holds is refused with 409
and the holder's name, because the session writes each file back whole at the
guest's next commit and would erase the entry with nothing reported. The API holds
the claim only while it writes, so it neither takes over a parked game (a
player's parked game is not traded for a tool's write) nor blocks a start for
longer than the two-second grace period of that revision absorbed.

The one road left unarbitrated was **the native CLI**, another process while this
claim lived in memory. Closing it meant a lock on disk, a different mechanism
with its own failure to get right: a lock left by a killed process has to be told
from one a running game holds, on five operating systems, without a live-process
check that lies on any of them. The CLI is a development and diagnostics tool by
design (`AGENTS.md`), so the exposure was a developer running one game twice
against one save tree on purpose. A save the CLI writes under a running session
is still the session's to overwrite.

Superseded: the claim is now also a kernel file lock that the CLI's `run`, `runktf`, `importsaves` and `provision` take; see [Save locks and claims](../architecture.md#save-locks-and-claims).

Whichever writer gets there, **a save is replaced rather than overwritten**:
`DirectorySaveStore.StoreSave` writes a dotted temporary file beside the target,
flushes it and renames it into place. A save is written whole every time, so an
interrupted overwrite would otherwise leave the front of the new save on the back
of the old one, a file the game reads as a save rather than as damage.

<a id="implementation-a-save-that-leaves-this-machine"></a>

#### A save that leaves this machine

The arbitration above solves one install's problem, two writers on one save
directory. **Between installs nothing holds at all.** The files under
`savedata/<profile>/<platform>/<owner>/` say neither which game they belong to nor
whether they arrived whole, so a save copied to another machine can land in the
wrong game's directory, and a transfer that stopped halfway leaves a file the game
reads as a save. The owner is not even unique within one install:
`SaveOwnerCollisions` exists on all three platforms because more than one distinct
title can claim one owner directory.

That matters more here than on a desktop emulator, because the page cannot see
the save tree at all. On a phone nothing else can reach it either: since Android
11 a file manager cannot open the directory an app keeps its files in, the same
wall that made a game have to arrive through the page.

So a save that leaves travels as one self-describing file, little-endian:

```
magic     8B   "WFSAVEBK"
version   1B   1
reserved  1B   0
identity  32B  raw SHA-256 of the game archive these saves belong to
length    4B   payload length
crc32     4B   IEEE, over the payload
payload   length B
```

The payload is the title's whole writable storage (on every platform the owner
directory, a tree of files keyed the way `SaveStore` keys them) as a list in key
order: a `uint16` key length, the key, a `uint32` data length, the data. It is
deliberately not a zip: a zip carries timestamps and file modes, so exporting one
untouched directory twice would give two checksums, and a re-export could not be
told from a change.

**The two refusals are kept apart**, because the person holding the file acts
differently on each:

- **The identity does not match**: the container is intact and is a real backup
  of another game, and the fix is to find the right file. Answered as 409, naming
  the first four bytes of both identities.
- **The checksum does not match, or the container does not parse**: the file is
  unusable and the transfer has to be repeated. Answered as 422.

A file that is not a container at all is a third answer (400), because the fix is
to pick a different file rather than another copy of this one.
`internal/backend/savepack.go` holds the codec and the reasoning; the routes are
in `internal/webhost/savepack.go`.

Two consequences are chosen rather than incidental:

- **A repacked archive is a different input**, so its own backups are refused.
  This build cannot tell a repack that changed nothing from one that changed the
  save format, and refusing cannot silently ruin a save.
- **An import replaces rather than merges.** The container is the whole writable
  storage, so an entry in the tree that it does not name came from a later point
  in the story, and leaving it beside the restored entries builds a state the game
  never wrote. The page asks before it runs, and the answer says how many entries
  were written and how many removed.

**A dotted file name is not a temporary file.** The save API had already got this
wrong. Every platform keeps load-bearing state under a dotted name: `rms/.index`
says a MIDP record store exists at all (`docs/rms.md`), and the file layers keep
`.removed`, `.created` and `.dirs`. `DirectorySaveStore` also leaves dotted files
on its way to a rename. Skipping every dotted name loses the first set to catch
the second, and the result is a save that restores and then appears to have lost
everything. `backend.ReadSaveTree` skips the temporary *shape* instead (a leading
dot and a trailing run of digits, which is what `os.CreateTemp` writes), and
`GET /api/saves/<owner>`, which had the same defect, now goes through it too.

<a id="implementation-exporting-and-importing-under-a-live-session"></a>

#### Exporting and importing under a live session

**An export is not arbitrated. An import is, exactly as a save API write is.**

The claim exists for one defect: a session writes the save files back whole, so a
second writer's entry is gone at the next commit with nothing reported. An export
writes nothing, so that defect cannot reach it, and refusing one would refuse the
page against its own running game, with no grace period able to help because the
game is deliberately still running. An unarbitrated export can catch a set of
files spanning one commit; every individual file is whole, since `StoreSave`
replaces by rename, so the worst case is one entry from before a commit beside
one from after, and the player can export again a moment later.

An import writes, so it takes `holdSaveDirectory` for the length of the write and
is refused with the holder's name, including when the holder is *parked*, which
it does not take over: a player's parked game is not traded for a restore that
can be repeated in a moment. This is the rule the save API's `PUT` follows,
applied unchanged rather than as a third policy.

<a id="implementation-cheats"></a>

### Cheats

The panel's operations are defined once: `internal/cheat`'s panel API answers
plain Go structs and `internal/webhost` sends them over the socket. The page's
panel calls one bridge whose methods are promises, because each is a round trip.

Cheat operations run on the emulator's own goroutine between ticks, the only
time reading and freezing guest memory is safe.

Whether a session has one is asked of the session rather than the platform:
`session.Cheat()` and `session.CheatConsole()` answer for every platform, and the
page removes the toggle where the answer is nil. That indirection made the third
platform a two-line change: the MIDP runtime used to answer nil, because its state
is Go objects with no addresses to sweep, and it now lays a synthetic address
space over its object graph and answers an engine like the others (`docs/skvm.md`,
"A heap with addresses in it"). `docs/lgt.md` has why the indirection was worth
the line it costs.

One operation differs by platform rather than by session: a **watch** needs store
instrumentation, which only the ARM platforms have. `Session.CanWatch()` is what a
panel asks, and the engine answers `cheat.ErrWatchUnsupported` elsewhere rather
than doing nothing.

**Driven end to end on running games, not fixtures.** Piping timed commands into
`runlgt -cheat` and `runktf -cheat` walks the whole vocabulary against a real
title: `regions` labels the address space, `scan unknown` over 669,696 candidates
narrows to 190 and then to 2 under `scan changed`, `list` shows those two module
globals counting up live, `set`/`read`/`dump` agree, a `freeze` survives seconds of
the game writing over it, `save` and `load` round-trip the table, and `watch` plus
`hits` names what is behind each write:
`0x01403de0 written by pc 0x00001054, 110 time(s)` for a guest instruction, and
`written by host, last pc …` for a store the platform made itself, a distinction the reader needs (`docs/armcore.md`, "Not
every store is a guest instruction"). The same flow through the socket is
`TestLocalCheatProbe`, which passes for all three platforms; on a MIDlet the
regions it lists are the game's own classes.

<a id="implementation-the-webassembly-path-is-gone"></a>

### The WebAssembly path is gone

The in-page engine was kept for a while behind `?wasm=1`, on the grounds that a
desktop could still run it. Nothing needed it: the server is a local binary, so
anything able to open the page has already started the process that emulates
better. It was removed with `cmd/wasm`, the loader, and the module the service
worker had stopped caching.

<a id="implementation-what-has-been-driven-and-what-has-not"></a>

### What has been driven, and what has not

Every platform reaches the browser through the same session, and the server half
was exercised for all three: a socket opened against a running server, a title
started by path, PNG frames counted as they arrive, a key sent by code, and audio
events counted. An LGT title answers 362 frames over eighteen seconds and
advances when sent key 148, and one of them sends twelve audio events in the same
window, so the driver, the input path and the audio path are not KTF-only.

**The browser half is checked by hand.** Nothing here automates a browser, so the
canvas draw, the oscillator synthesiser making a sound, and how the PWA feels to
hold are confirmed by a person. `web/session.test.mjs` covers the client's
protocol handling without opening a page. To check by hand, run the server and
point a browser at it.

<a id="implementation-a-game-that-ends-is-not-a-game-that-failed"></a>

### A game that ends is not a game that failed

Guest code runs inside whatever Host call reached it, so a game can end inside
one: a menu whose confirm key quits calls `MC_knlExit` from the key handler, and
the key delivery is what finds out. `Tick` always reported that as an ending; the
input path did not, so a player who quit saw **"the game failed"** over the last
frame, and the session stayed up streaming that frame until the page closed.

Both paths now answer the same way: the session closes and the page is told the
game exited. `session.ErrExited` is what an input call reports it with, and
anything else remains a failure.

<a id="implementation-an-ending-says-where-it-came-from"></a>

#### An ending says where it came from

**"The game exited" alone is the absence of a finding.** A title of this era ends
itself for ordinary reasons (a quit menu, a game over, a first-run notice that
writes a flag and asks to be launched again), and every one reached a reader as
the same four words. Nothing prints a stack for an ending, because it is not a
failure, so the reader had no address, no call, and no way to tell a working
title from one that gave up on its opening screen. A manual sweep of the corpus
filed nineteen titles as broken on that line alone; fourteen were the restart
notice and ran on the second launch.

So the platforms name the call site and the layers above keep the text:

- `MC_knlExit` wraps the sentinel with the address in the link register, which
  the import stub leaves intact because it arrives with `bx lr`. The WIPI Java
  path does the same for `Jlet.notifyDestroyed`, the other ending, which is
  examined differently.
- Every layer above matches with `errors.Is`, so the wrapping costs them nothing.
  The one place it was discarded was a re-entry check that replaced a live error
  with the bare sentinel; it now returns what it was given.
- `session.Progress.ExitReason` carries it out of the tick, and
  `Session.ExitReason()` keeps it after the session is closed, which is the only
  time a Host has to report one.
- The CLI prints `the game exited at tick N: …` and puts `exited` and
  `exit_reason` in its summary. The server writes `ended: the game exited: …`
  into the session report and sends the reason with the `exited` message, so it
  lands in the page's run log. The player's status line is unchanged: an address
  is for whoever reads the log afterwards, not for whoever holds the phone.

The reason reads as the chain it came through, ending in the sentinel: the call
the guest was inside, the supervisor call, and the address it left from.

<a id="implementation-a-start-the-server-refuses-has-to-reach-the-run-log-too"></a>

#### A start the server refuses has to reach the run log too

The `exited` path covers a game that ran. A game that never started is the worse
failure (no first frame, no session report, nothing on the screen), and the log
used to say least about it. The refusal comes back as the `error` answer to the
start request, which rejects the page's own promise rather than reaching the
session's error handler, and the page put it on the status line and into
`console.error` only. Two things then lost the sentence:

- `console.error(anError)` logged `error.stack`, on the reasoning that V8 begins
  a stack with `Name: message`. WebKit does not (its stack is frames only), and
  the phone the sweep ran on is WebKit. One session's entire record of why a title
  would not start was `#receive@…/session.js:113:53`. The log now writes the
  message and then the stack, and skips the message when the stack already
  carries it.
- Nothing recorded an event for it. `reportError` now writes one, so a refused
  start reads beside the `starting …` line that preceded it instead of as an
  unexplained end to the log.

What the server sent was already right: `startGame` answers `err.Error()`, so the
text was on the page the whole time.

<a id="implementation-two-refusals-the-server-words-itself"></a>

#### Two refusals the server words itself

`err.Error()` is written for a log. It is the right answer for a refusal nobody
can act on, such as a truncated archive or a descriptor that will not parse. Two
refusals differ: the file is intact, the person holding it can act, and what they
should do differs. Both get a sentence of their own in `startGame`, in the
language of the page's other refusals.

- **A zip of zips** is a bag of games rather than a game. There is nothing to fix
  and nothing to load: each entry is a whole archive, and which one to add is the
  person's choice rather than a loader's. `detect.ArchiveOfArchives` is the test
  and `session.ErrArchiveOfArchives` the answer.
- **A locked package** was wrapped before it was distributed: the payload the
  descriptor names is an OMA DRM container instead of a JAR. The archive around
  it is ordinary and readable, so the refusal has to be worded; the loader's
  complaint is about bytes it could not read, and reads as damage.
  `detect.ReasonDRMWrapped` is the finding and `session.ErrDRMWrapped` the answer.

The sentence for the locked package avoids two things a person would act on
wrongly. **It is not damage** (re-downloading gets the same file), and **it is not
a format this does not read yet**, which would put the work on this side. The key
belongs to the network that issued the file, so the only remedy is a copy that
was never locked, and that is what it says.

Neither needs a message kind of its own: both are `error` answers to the start
request, and the page shows the text it is given. `Inspect` decides both, so the
CLI and the page refuse the same file for the same reason, and the loader's own
sentence stays behind `ErrDRMWrapped` for a log, where it names the locked entry.

<a id="implementation-a-start-can-be-refused-by-a-game-that-ended-not-a-game-that-broke"></a>

#### A start can be refused by a game that ended, not a game that broke

**A start can also be refused by a title that simply finished**, and the two used
to be one message.

A title whose first run installs itself quits inside `startApp` on its *second*
run: it reads the flag it wrote, decides there is nothing left to do, and asks
the platform to exit before the start has returned. `Tick` reports the same
ending one tick later, but here there is no session yet, so it came back as a
start failure and the player saw the platform's chain printed as a fault. A sweep
that always begins from an empty save directory never reaches it: it takes two
runs over one save.

The ending travels in the error, because there is no session to hang it on:
`session.startEndedOrFailed` wraps the platform's chain in `ErrExited`, the
sentinel a running game's ending carries.

**The page settles a request on its id, and only the `error` answer settles a
request that produced no game**, so the mark rides on that answer rather than on
a message kind of its own. A `serverExited` with the request's id would read as a
start that worked, and one without it would leave the page waiting on a promise
nothing resolves. So `error` carries `exited: true`, the page's rejection carries
`.exited`, and the game picker comes back either way; the player is told the game
closed itself instead of being shown a guest address. The reason still goes to
the run log, where an ending is investigated.

The CLI answers the same way: `the game exited before its first tick: …`, `exited`
and `exit_reason` in a summary of no ticks, and exit code zero. See
[`cli.md`](../cli.md).

<a id="implementation-a-guest-thread-that-panics-is-a-failed-game-not-a-dead-server"></a>

#### A guest thread that panics is a failed game, not a dead server

Guest code also runs on goroutines of its own: each platform's guest threads, and
the KTF session start that keeps a Host's event loop alive while it runs. A panic
on one of those goroutines is caught by nothing (a panic in an HTTP handler is,
which is why this was invisible from the page), and it takes the process down.

The process is the wrong thing to lose. It holds the session's post-mortem
report, its ordered boundary trace, the page's log and every other session that
was fine, and a panic here means one archive doing something this emulator does
not implement. **A panic is the least informative failure this emulator has,
which argues for converting it rather than only surviving it.** Each of those
goroutines catches its own: the stack goes to the log at error level, and the
caller gets the error it was already prepared to receive, so the game fails and
the session ends like any failed session. `backend.GuestPanic` is the shape;
`internal/jvm` says the same in its own eight lines, because the Execution layer
does not import the Runtime.

Nothing retries and nothing continues past one: the thread that panicked is over.
What follows the catch (clearing the thread's alive flag, releasing the monitors
it held) runs either way, because a title waiting on `isAlive` or on a lock would
otherwise wait for the whole session.

<a id="implementation-the-one-goroutine-that-had-no-boundary-was-the-hosts-own"></a>

#### The one goroutine that had no boundary was the Host's own

A panic in an HTTP handler is caught by `net/http`, which is why the handler's own
goroutine never had a boundary of its own; being caught is not being handled.

Most guest code runs on that goroutine. `Tick` enters the guest on every platform,
and so do a key, a touch, and each half of the pause pair. A panic in any of them
unwound `sessionRunner.loop` and `run` and landed in `net/http`'s recover, which
logs "http: panic serving" and closes the socket. The process survived; the
session did not clean up, because **nothing past the tick loop ran.** The game was
never closed, so its guest memory (a KTF arena alone is 64 MiB) and its worker
goroutines were held for as long as the server lived. The frame encoder was left
blocked on a channel nobody closed. And the save directory kept a claim released
nowhere, so **that one game could not be started again until the server was
restarted**, while every other game kept working. A page asking for it got
"another session is playing this game", naming a session that had already died.

The boundary is therefore in `internal/session`, the layer both Hosts drive a
game through: `guard.go` converts a panic into the error the caller already
handles, and the server's loop then ends the game as it ends a failed one
(post-mortem written, claim released, page told). The CLI gets the same
conversion from the same place.

Two details make it more than a `recover`. The failure is **remembered**: a
recovered panic leaves the guest halfway through whatever it was doing, so every
later call answers with the same error instead of re-entering a machine that
cannot answer. `Close` is the exception: it runs *because* the session failed,
since it releases the memory and goroutines the panic left running, and it takes
each platform out of the session before closing it so that a second `Close`
cannot enter a half-closed game.

What a panicking *start* leaves behind is not recovered and cannot be from here:
a platform session is handed over only once it is built, so one that panicked
halfway is unreachable, guest threads included. That is a bounded loss per
refused archive, against a process that used to end.

<a id="implementation-a-game-outlives-its-socket"></a>

### A game outlives its socket

A game is retained when its controlling page leaves, and **elapsed time does not
close it.** The old five-minute expiry and the page's finite retry loop were
removed. The server admits at most four games in total, active and parked;
counting both reserves retention room before a page disappears, so parking never
silently evicts another game. A fifth start offers to close the oldest parked
game, and when all four are active it is refused. A fresh start over the same
shared save directory also requires confirmation. Explicit stop, an approved
fresh start, save import and server shutdown end retained state. There is no
snapshot on disk: restarting the server loses unsaved progress.

The browser keeps a random 128-bit capability in local storage under
`wfeature.browser-token`, shared by tabs of the same origin and browser profile.
It identifies the browser's current game, **not a user or a save owner**.
`web/session-link.js` adopts the old tab-local `wfeature.resume-token` when no
shared capability exists. When storage is denied, `web/storage.js` falls back to
memory, which another tab or a discarded page cannot recover. There is no
automatic identity link across browsers or devices, and no account, transfer
code or separate personal save directory.

A destructive start first answers `result` with `confirmation` and a Korean
`message`. The page uses its existing confirmation dialog, focused on Cancel, and
accepting repeats the start with the returned confirmation string. The
confirmation is bound to that connection, browser token, archive path, retained
instance and parking time. The server rechecks ownership, capacity and the victim
under one lock; a changed victim needs a new confirmation, and an active game
cannot be evicted. A pending dialog claims neither saves nor game capacity. Older
clients can still start freely when nothing would be discarded, but cannot bypass
the check.

A start supplies that token; older clients may omit it and receive a server-made
one. The server reserves the token before loading, so simultaneous tabs cannot
start two games under one browser capability. A resume answers:

- `started` with the same game's description and picture when it is parked;
- `resumed` with `occupied: true` when another connection controls it;
- `resumed` without `occupied` when nothing remains, so the page shows its picker
  and can start again without a reload.

A connection already closing finishes parking before the resumption is answered,
so a reload does not compete with its own old socket.

The occupied page offers **Continue here** (Korean in the UI), which sends
`resume` with `takeover: true`. The previous runner releases held keys and touch,
pauses the guest, and relinquishes the game between guest calls. The next runner
adopts the game and its save claim atomically under the server mutex. The old
page receives `detached`, silences audio and vibration, and does not reclaim
control automatically; input and stop on that old connection cannot reach the new
owner's game. A handoff waiting on a long guest call times out after ten seconds;
it can be requested again, and a cancelled queued request does not later detach
the owner.

When hidden, the page sends `park` and stops presentation immediately. The socket
may stay open, but it no longer owns a game. Returning resumes the retained game;
if another page has taken it, the explicit takeover choice appears. If the socket
disappears without a final message, the server parks the game the same way. Both
paths release held input before calling the guest's pause entry point. Stopping
uses an acknowledged `stop` and restores the existing picker with its event
wiring. Legacy unacknowledged stops receive no reply.

Connection failures retry while the page is visible, with delays from one to
fifteen seconds and no attempt limit; hidden pages do not retry.
`visibilitychange`, `online` and `pageshow` trigger recovery. A playing page
checks liveness every fifteen seconds, and a `ping` must answer within ten. The
server answers it outside the guest command loop, so a slow loading or rendering
call is not mistaken for a broken network. Opening the socket also has a
ten-second timeout. Browser freezing can delay these timers; they are not a
guarantee about OS scheduling.

The game's context belongs to the game, not its connection: parking keeps it
alive and stopping ends it. Guest clocks keep their wall-clock semantics,
including the elapsed time seen after a long absence. The Host stops ticking
parked games and calls platform pause and resume; this does not promise that
every guest-created thread obeys its pause callback.

**Saves are unchanged.** Formats, directories, import and export, and shared-save
claims remain game-scoped. Different browsers may play different games at once.
Two live games cannot write the same save directory. A confirmed fresh start or an
explicit save import may replace a parked holder; neither resumes someone else's
in-memory game.

<a id="implementation-the-game-is-told-it-was-parked"></a>

#### The game is told it was parked

A parked game does not tick, and it is told why. Parking calls
`session.Session.Pause` on the way out and `Resume` on the way back, and each
platform answers with the lifecycle its titles were written against: a Clet's
`pauseClet`/`resumeClet`, a MIDlet's `pauseApp`/`startApp`, and the WIPI Java
Jlet's. **Two of the three had the platform half implemented and nothing calling
it**: the calls carried a comment saying they were the lifecycle a Host uses when
the page is hidden, waiting for a Host that had not been written.

It matters because of the clock. A parked game's clock is the wall clock and keeps
moving, so a title comes back to time it did not spend. A suspended handset did
the same, *together with* these calls; the jump without the notification was the
gap, and the jump with it is what the title was built for.
[`ktf.md`](ktf.md#implementation-ktf-platform-implementation-status), "The clock
the game lost", has the measurement showing that freezing the clock instead would
be a divergence with nothing to fix.

Three rules hold across the platforms:

- **A title with nothing to say is not an error.** Half the local WIPI titles
  compile their pause body to a prologue and a return, and a Clet may leave the
  entry in its table at zero; both mean "nothing to do".
- **A failed callback does not end the game.** Driving these across a
  266-archive set killed five titles that had run correctly without them; the
  failure is now logged and the session carries on. `ktf.md` has what each one
  was.
- **A parked game refuses to tick.** `Tick` answers `ErrPaused` rather than
  running a game it has just told to stop, which also makes a forgotten resume
  loud instead of silent.

<a id="implementation-vibration"></a>

### Vibration

The guest asks the handset to vibrate on all three platforms, and until this
change none of those requests reached anything: KTF counted them as a
diagnostic, LGT returned success without reading the arguments, and SKT set a
flag that was written in two places and read nowhere. The browser has
`navigator.vibrate` and there is an Android build, so what was missing was a
boundary to carry the request across.

**The core reports; the Host decides.** A platform runtime owns
`backend.Vibrator`, which records what the guest asked for and nothing else. It
does not know whether there is a motor, whether this Host can drive one, or
whether the person wants it, and must not: a core that took the action would
have to be told about every Host, and a Host that could not act would have no way
to say so.

`session.Session.Vibration()` answers `(backend.Vibration, bool)`, and the second
value is the whole of how "this platform does not implement vibration" is said. A
Host that reads false does nothing, as it does for a guest that has not asked;
both are correctly silent on the wire.

Two rules of the guest's contract are not what a reader would guess, and getting
either wrong loses the vibration silently:

- **`level` is a percentage from 0 to 100, not an index.** Zero is off and 100 is
  the strongest the hardware has. It is *not* in the units of the `VIBRATORLEVEL`
  handset property, which counts a handset's strength steps: a title reads `"3"`
  there and still passes 1..100.
- **A duration of zero with a level above zero means until stopped**, not "do
  nothing". A title asking for a continuous rumble and getting a no-op is a
  vibration that never happens.

A request made while one is running replaces it and restarts the clock, as
specified. The vibrator measures with the wall clock rather than the guest's: a
vibration is felt in the room, so a game running at a quarter speed still asked
for the milliseconds it asked for, and scaling them would make the speed control
change how the handset feels.

<a id="implementation-how-one-request-becomes-one-message"></a>

#### How one request becomes one message

The core reports a *request that stands* until the guest makes another, so the
edge is made on the Host side. `Vibration.Request` counts the requests, and the
session sends when that number moves. Comparing the level and duration instead
would swallow the second of two identical buzzes the guest asked for on purpose.

The message is `{"kind":"vibrate","vibrate":{"level":0..100,"ms":N}}`. The level
travels unchanged even though `navigator.vibrate` has no strength: what a Host
does with a request is its own business, and an Android build has an amplitude to
give it. A request that turns the motor *off* is sent like any other, because an
indefinite buzz the page already started runs until something stops it.

On the page (`web/vibrate.js`) the level decides only whether to vibrate, because
scaling the duration by the strength would be an invention: a weak buzz is not a
short one. An indefinite request becomes a five-second call, since the API takes
a length and a page that asked for an hour would leave a phone buzzing long after
the game stopped caring. A game that ends and a socket that drops both stop the
motor, because the guest that would have stopped it is gone. The setting is a
checkbox in the settings panel, on by default where the browser has a vibrator
and removed entirely where it does not, since a switch that cannot do anything
reads as broken.

<a id="implementation-where-it-reaches-and-where-it-does-not"></a>

#### Where it reaches, and where it does not

| | Guest entry point | Reaches the Host |
|---|---|---|
| KTF | `org.kwis.msp.media.Vibrator.on/off`, and `MC_mdaVibrator` (media call 16) | yes |
| SKT | `com.skt.m.Vibration.start/stop`, and `org.kwis.msp.media.Vibrator.on/off` in Jlet mode | yes |
| LGT | `org.kwis.msp.media.Vibrator.on`, and WIPI C slot `0x4c1` | **no** |

LGT was unfinished rather than decided: both of its entry points still answered
without reading their arguments, so `Vibration()` reported false and the page did
nothing. The earlier KTF package generation has no vibrator call in its native
table at all, and reports false for the same reason.

Superseded: LGT requests now reach the Host as well; see [session.md](../session.md#transport).

Two things are recorded here rather than guessed at in code:

- **`VIBRATORLEVEL` is still answered `"0"`** (`internal/wipic/properties.go`),
  which by the specification's table means "this handset has no vibration steps".
  That was right when nothing could vibrate and is now a title's reason not to
  ask. Changing it is a guest-visible change across the whole corpus, so it needs
  an A/B sweep rather than a decision taken here.
- **SKT's `com.skt.m.Vibration` argument units are not settled.** The class is
  the vendor's and nothing in this repository states its contract, so the
  arguments are read as WIPI's (level 0..100, milliseconds), the only documented
  convention within reach. The one call in the local fixture is `start(10, 5)`,
  which fits that reading awkwardly: five milliseconds is below what a person
  feels. Reading them wrong costs a buzz of the wrong length; not reading them at
  all cost every buzz.

<a id="implementation-what-is-not-solved"></a>

### What is not solved

- **A touch reaches only one platform.** A `pointer` message carries a press,
  drag or release in the guest's own screen coordinates, and `started` says
  `can_touch` so a page knows before it sends one. The WIPI Java platform
  dispatches it; a Clet's event kinds are keys, the earlier KTF package takes one
  event callback with no pointer in it, and a MIDlet's `pointerPressed` is a
  surface this runtime does not offer. Those three answer `ErrNoPointer`, which
  the server drops rather than reports, so a thumb resting on the canvas does not
  fill the page's error rail.
- **`destroyApp` is not called on any platform's close.** Closing is where a KTF
  guest thread is being unwound, the one moment guest code must not be entered;
  the MIDP runtime runs its own `destroyApp` from inside the runtime rather than
  from here.
- **Input latency is unmeasured.** It is a LAN round trip plus one frame, and
  what that feels like on a real network has not been established.
- **Nothing bounds how many live sessions there are**; that half of the resource
  question is still open (see the last entry). The other half is closed: **the
  server has a read timeout**, so a request that stops making progress no longer
  holds its connection and goroutine until the operating system gives up. It was
  left off because setting it was believed to kill the session websocket; it does
  not. `net/http` clears both connection deadlines when a handler hijacks, so
  nothing set before the upgrade reaches the socket after it, and
  `TestTheSessionSocketOutlivesTheServersReadTimeout` keeps that as a test rather
  than a belief: a Go release that stopped clearing the deadline would end every
  session after ten minutes of play, which on a phone is indistinguishable from
  the network.
- **Two tabs on two different games are two sessions**, and nothing bounds how
  many. The same game twice and the save API are arbitrated, but a native CLI
  write under a running session is not; see "One game's saves belong to one
  session".
- **A parked game is not persisted, and will not be.** It lives in the server's
  memory, so stopping the server ends every game waiting for a page, and the page
  finds its token unknown when the server comes back. Writing one to disk is not
  a skipped small feature: a KTF guest thread parks *inside* a nested Go call
  stack (the Host is blocked in a channel receive while the worker sits in
  `InvokeVirtual` inside a supervisor call), and a Go stack cannot be written to a
  file. The guest's ARM memory could be, but the Go half of the session (the JVM
  object graph, open files, image handles, the audio sink and every parked stack)
  could not, and a snapshot of one half resumes nothing. Making it possible means
  an execution model where a parked guest holds no Go stack at all, a rewrite of
  what makes a game whose main loop never returns work in the first place. A
  restart costs what a handset losing power costs: everything since the game last
  wrote its save. The page remembers which archive it was playing, so coming back
  is one click rather than a search.
- **There is no authentication.** The server binds every interface so that a
  phone can reach it, and anything else on that network can reach it too.
- **The four-game count is not a memory budget.** Active and parked games share
  the limit, but their allocations vary. The count bounds admitted games, not
  concurrent archive inspection, socket connections or resident bytes.

Superseded in part: a quick save now records parked guest threads by their remaining work and survives a restart; see [Where a stopped game keeps its state](../architecture.md#where-a-stopped-game-keeps-its-state).

<a id="implementation-automatic-offline-authentication"></a>

### Automatic offline authentication

New sessions automatically select recognized authentication compatibility. The
Opts panel has no authentication control or notice, the page stores no policy
preference and sends no authentication switch, and the obsolete incoming
`authentication` field is ignored, including false from older clients. The shared
`Options.DisableAuthentication` opt-out is for local diagnostics and is not
exposed by the browser protocol.

The `started` description reports `authentication` as `off`, `unsupported`,
`ktf-certificate-23`, `ktf-certificate-52`, `ktf-subscriber-fallback`,
`skt-license`, `lgt-cached-authentication`, `lgt-certificate-58`,
`lgt-offline-notification` or `lgt-offline-authentication`. Resume returns the
existing runtime and its result. Unsupported keeps ordinary guest behavior.
Applied identifies a selected adapter, not successful gameplay. See
[implementation and limits](../authentication.md).

<a id="frame-bandwidth-2026-09-24"></a>

## Frame bandwidth — 2026-09-24

The encoder already omitted consecutive identical pictures, but any changed
picture still sent the entire PNG. Compressing 100 existing local startup
screenshots with default PNG compression instead of BestSpeed reduced their total
from 2,432,172 to 2,230,227 bytes (8.3%). This read-only sample is not a gameplay
traffic measurement; it motivated comparing changed regions rather than spending
more compression effort on every complete picture.

The first implemented path kept emulation and hqx on the server and sent a
lossless PNG rectangle, after a `WFP1` header, for small changes. It added one
retained composition canvas on the page and one retained scaled frame on the
server. No dependencies, guest behavior, frame-rate settings, audio messages or
save formats changed. The [current protocol](../session.md#presentation-and-audio)
specifies compatibility, ordering, redraw boundaries and recovery.

`TestFramePatchBandwidth` uses 60 authored 240x320 frames in which a 16x16 sprite
moves across a fixed tiled background, including the initial complete picture.
Complete PNG payloads total **226,646 bytes**; negotiated updates total **11,732
bytes**, a **94.82% reduction**. Both runs encode the same pixels. The 12-byte
patch header is included; WebSocket, TCP and TLS overhead and JSON and audio
traffic are excluded.

On an Apple M1, darwin/arm64, three one-second runs of
`BenchmarkFramePatchBandwidth` gave these medians:

| Encoder path | Time per frame | Payload per frame |
| --- | ---: | ---: |
| Complete PNG | 609.4 microseconds | 3,777 bytes |
| PNG rectangle | 24.2 microseconds | 137.6 bytes |

The benchmark repeats the 60-frame sequence, so its initial complete picture is
amortized more than in the fixed test. It excludes emulation, hqx (scale is 1),
socket writes and browser work. Reproduce with:

```sh
go test ./internal/webhost -run TestFramePatchBandwidth -v
go test ./internal/webhost -run '^$' -bench '^BenchmarkFramePatchBandwidth$' -benchtime=1s -count=3
```

Go reconstruction tests cover alpha replacement, edge changes, hqx factors 1–4,
forced complete frames and dimension changes. Handler tests cover negotiated,
legacy and unknown-version connections. Node tests cover ordered composition,
slow decoding, closure, invalid frames and bounded backlogs. The authored-fixture
browser route passed in Chromium and WebKit, including patch output matching a
forced complete PNG, restart, park/resume, new connections and WIPI scale
changes. The ordinary, debug, internal race and vet gates passed.

These results do not promise a 94.82% reduction for a real game. Full-screen
scrolling or widely separated changes can still require complete pictures, and
sampled audio traffic was unchanged. Actual gameplay traffic and physical-phone
CPU, memory and battery effects were not measured in this change. Large-scene
optimization should follow a measured route rather than reduce image quality or
move the emulator onto the phone by default.

### Recorded play, and protocol 2

The rectangle path above was never released; protocol 2 replaced it the same
day, after the traffic of real play was measured.

**Method.** Five in-game scenes were replayed from local routes (two KTF titles
and three LGT titles) with the CLI's manual clock and isolated saves. A local
hook recorded every finished frame with its guest time and every sound batch in
the JSON the session sent. The frames went through the encoders offline; the
server skips an identical consecutive picture, so the recorded sequence is what
it would encode, and rates are per second of guest time. Replaying one KTF route
on the wall clock gave 26.1 guest seconds against 25.1, with rates within 15%.
Recorded play is not a route through every scene, SKT titles had no local route
and were not measured, and sampling misses KTF flushes inside one tick.

Kilobytes per second of pictures at the original size, WebSocket framing
included; "Rectangle" is the path above, measured with the same frames:

| Scene | Changed pictures/s | Protocol 1 | Rectangle | Protocol 2 |
| --- | ---: | ---: | ---: | ---: |
| KTF, full-colour field, scrolling | 8.9 | 344.6 | 334.8 | 207.8 |
| KTF, side-scrolling field | 49.5 | 358.1 | 329.4 | 98.6 |
| LGT, field, character and status bar | 7.8 | 244.1 | 108.4 | 11.3 |
| LGT, field, several moving figures | 12.7 | 513.2 | 393.5 | 37.6 |
| LGT, almost still screen | 0.6 | 2.9 | 1.3 | 0.6 |

The four busy scenes total 1,460 KB/s in protocol 1, 1,166 with rectangles and
355 in protocol 2; an hour of one of them was 0.9–1.9 GB, 0.4–1.45 GB and
42–766 MB respectively. The rectangle is one bounding box that falls back to a
complete picture past half the screen, so a moving figure and a status bar far
apart were a complete picture. At hq2x, protocol 1 sent 3.4 to 3.8 times these
numbers (5,399 KB/s for the four) and at hq3x 6.3 times; protocol 2 sends the
same bytes at every scale.

Protocol 2's parts, measured separately on the same frames: transparent unchanged
pixels with a palette cut the LGT scenes to 6–7% of protocol 1 and the
side-scroller to 34%; the shift took a further 18–26% off the scenes that scroll
(the full-colour field moves one pixel a frame on 112 of 232 frames, the
side-scroller two pixels on about a thousand of 2,895) and 3% off the others. The
side-scroller moves a band of its screen, a few percent of the pixels: a search
gated at an eighth of the picture changed let 59 of its frames scroll, and a
thirty-second 1,046. Trying the last shift first took its encoding from 1.5 to
0.9 ms a frame. Encoding cost 0.5–1.2 ms a frame on an Apple M1, and 4.9 ms on
the full-colour field, whose changes exceed a palette and whose every other frame
is searched.

Sound, in the same units: the JSON of protocol 1 against binary with each sample
carried once.

| Scene | Protocol 1 | Protocol 2 |
| --- | ---: | ---: |
| KTF, side-scrolling field | 3.80 | 1.10 |
| LGT, field, character and status bar | 6.40 | 0.43 |
| LGT, field, several moving figures | 2.13 | 0.21 |
| LGT, almost still screen | 0.29 | 0.17 |

One LGT title played four distinct sampled effects 99 times, 2.4 KB/s as base64,
and its SysEx messages repeat the same way. Note events were 40–53 bytes of JSON
each and are three or four bytes now. The first KTF scene recorded no sound.

A tick's sound used to leave a millisecond or two ahead of its picture as a
separate write, and every WebSocket frame was written as two writes, header then
payload. At 9–14 sound messages a second, a write each, that was on the order of
1 KB/s of TCP and TLS headers per busy scene, before the pictures' own.

**Measured and not used.**

- *Lossy JPEG.* A complete JPEG at quality 75 was larger than protocol 2 in four
  of the five scenes (190 KB/s against 38 on the moving figures) and smaller only
  on the full-colour field, while giving up losslessness.
- *WebP lossless* of whole pictures, encoded with `cwebp` at two effort levels,
  was measured on the full-colour field only, where it came to 58–69% of
  protocol 1. It would need an encoder outside the standard library, and masking
  alone reaches 5–34% on the other scenes. Protocol 2's rectangles later moved to
  WebP through a project-written encoder; see
  [below](#frame-bandwidth-webp-2026-09-25).
- *XOR against the last picture, then deflate.* Larger than protocol 2 in every
  scene (230 KB/s against 208 on the full-colour field, its best case), and it
  needs a decoder written in the page, where a PNG uses the browser's.
- *Tiles.* Changed 8- or 16-pixel tiles packed into one PNG came to 16–23% of
  protocol 1 on the two LGT fields, above masking.
- *A frame-rate cap.* Only the side-scroller exceeds 15 pictures a second.
  Capping it at 15 cut three quarters of its traffic in the rectangle path and
  would cost the same share of its smoothness; it was not measured with protocol
  2 and was left as a possible setting rather than a default.

**Authored fixtures.** The commands above now compare protocols 1 and 2; the
rectangle numbers are from the path protocol 2 replaced. The moving sprite comes
to 11,360 bytes against 226,646 (94.99% less) and an authored scrolling field
under a fixed bar to 6,397 against 59,598 (89.27% less). Three one-second
benchmark runs on the M1: protocol 1 at 616 microseconds and 3,778 bytes a frame,
protocol 2 at 205 microseconds and 138 bytes. Protocol 2 compares every pixel as a
word rather than skipping equal rows as the rectangle did, which is why it is
slower per frame than the rectangle's 24 microseconds.

**Browser.** The frame-delivery route passed in Chromium and WebKit, including an
authored picture composed through masked, shifted and replacing updates and
compared with the expected pixels, and hq2x redone around partial changes compared
with hq2x of the whole picture. A local KTF archive ran six seconds at 60 pictures
a second, 214 KB/s on its animated opening, and a local LGT archive at 1.7 KB/s
on its title. The page's JavaScript hqx costs 4.4–16 ms for a whole 240x320 frame
on the M1, depending on the scale; see [hqx](../hqx.md). Phone CPU, battery and
actual monthly traffic remain unmeasured.

<a id="frame-bandwidth-webp-2026-09-25"></a>

### Lossless WebP pictures — 2026-09-25

Protocol 2's rectangles were PNG. Lossless WebP of the same rectangles, from an
encoder written for them in `internal/vp8l`, is what a page gets when it asks for
it; see [the protocol](../session.md#protocol-2-pictures).

**Why a project-written encoder.** The standard library has none. Published
pure-Go encoders, run on the rectangles of the recordings above, came to 75–176%
of the PNGs (most were larger on the palette scenes, because they made little of
a colour table) or decoded to different pixels. A machine translation of the WebP
project's own encoder to Go reached its sizes, but at up to 236 ms a picture,
from 2.7 MB of translated code under the WebP project's licence. The format is
specified in RFC 9649, and `golang.org/x/image` already carries a decoder to test
against.

**Method.** The five scenes above were recorded again with the same routes and a
hook that kept every changed frame with its guest time, and the frames went
through the session's own encoder, once with PNG and once with WebP. Four scenes
reproduced the earlier recordings to the message. The full-colour field was
recorded on the wall clock again and came out with fewer changed pictures a second,
178 in 46.2 guest seconds against 232 in 26.1, so its PNG rate is lower than
above; the ratios are what compare. Times are one Apple M1 core. Rates are the
messages, header included, without WebSocket framing.

| Scene | PNG, KB/s | WebP, KB/s | Encoding, ms a picture | Share of a core |
| --- | ---: | ---: | --- | --- |
| KTF, full-colour field, scrolling | 74.2 | 38.2 (−49%) | 4.4 → 3.6 | 1.7% → 1.4% |
| KTF, side-scrolling field | 98.3 | 80.3 (−18%) | 0.9 → 1.4 | 4.4% → 7.0% |
| LGT, field, character and status bar | 11.3 | 9.2 (−19%) | 0.5 → 0.75 | 0.4% → 0.6% |
| LGT, field, several moving figures | 37.5 | 28.6 (−24%) | 1.2 → 1.6 | 1.5% → 2.0% |
| LGT, almost still screen | 0.6 | 0.5 | 0.5 → 0.7 | under 0.1% |

The four busy scenes come to 156 KB/s against 221. The slowest WebP picture took
8.8 ms, on the moving figures; the side-scroller's 99th percentile is 3.2 ms
against PNG's 2.3. Encoding includes the comparison with the held picture and the
scroll search, which both formats share.

The first working encoder took twice these times on the palette scenes: the
side-scroller at 2.2 ms a picture, 10.7% of a core. Its bytes were right, but much
of its time did not depend on the picture: every image, including the colour
table's own few pixels, cleared two hash tables of 32,768 entries, tried eleven
colour-cache sizes over separate histograms and priced 2,328 symbols with a
logarithm each. Five changes, each checked to leave every recorded rectangle's
bytes unchanged, brought it to 0.45–0.59 of that time: hash chains stamped with a
running position base instead of cleared; a parse that skips candidates no copy
could beat, and a run of one colour without hashing each position; the colour
caches tried together, since a pixel a small cache holds every larger one holds
too; prices computed only where counts change; and the colour table and prefix
codes built without repeated planning. The tables an encoder keeps between
pictures are shared with any copy of it, so the matcher counts the images parsed
with them and makes its own when a copy has used them since.

Against the WebP project's `cwebp -lossless -m 4` on the same rectangles (the
first 1,500 messages of each scene): 38.2 against 40.6 KB/s on the full-colour
field, and 21.4 against 20.5, 9.6 against 9.2 and 80.3 against 76.2 on the other
three, where `cwebp` stays 4–5% smaller.

**What decided the design**, each measured on these rectangles:

- *No predictor for game pictures.* With the predictor transform the full-colour
  field came to 68% of PNG and without it to 51%, and keeping the smaller of the
  two for every rectangle of three scenes never chose it: the games' flat areas
  and repeated tiles are copies from the left and from above, which prediction
  residuals break up. It is still tried on a picture over six bits a pixel.
- *A transparent pixel's colour is free.* The page never shows it: the browser
  premultiplies it away, and a masked update's transparent pixel keeps what is
  under it. The encoder makes it one colour for the colour table and sets it to
  its prediction when predicting, as the WebP project's encoder does unless told
  to keep exact colours. That took the full-colour field from 78% to 71% of PNG
  while the predictor was still in use.
- *Copies are priced in bits.* The longest match at each position took far copies
  of two to four pixels that cost more than writing the pixels: in one 240x320
  palette rectangle, 10,117 of 14,935 copies were beyond the neighbourhood codes.
  Choosing the copy that saves the most bits over its literals, and none when none
  saves any, took 4–5% off every scene. Pricing from the rectangle's channel
  histograms alone, rather than from a first greedy parse, lands within 1% and
  takes a quarter to a third less time.
- *Two hash chains.* Chains over runs of four to six pixels found repeated tiles a
  screen back and took 1–3% off the palette scenes, but made the full-colour field
  5% larger without the chain over pairs; both are kept.

**Measured and not used.** Spatially variant prefix codes, the format's entropy
image, came to 4–6% less in an idealised clustering that ignored its own cost,
and the clustering itself was the most expensive thing tried. Writing the masked
pixels as the held picture's colours instead of transparent, keeping the smaller
of the two, saved 0.4–4% for twice the time. Two parses priced by the first came
to 1% less than one for about a third more time.

**Verification.** Round trips through `golang.org/x/image` cover the colour table
sizes, the neighbourhood codes, transparent bands and every path the defaults
avoid, and `FuzzEncode` ran a million pictures. `dwebp` decoded 404 authored
pictures through each path and all 13,268 rectangles of the recordings to the
same pixels, transparent colours aside. The frame-delivery route passed in
Chromium and WebKit with the page asking for WebP, on the authored fixtures and on
a local KTF and a local LGT archive. What a phone spends decoding WebP against
PNG, and what a small server spends encoding, remain unmeasured; the encoder takes
up to about one and a half times PNG's time on the scenes that are mostly copies.

<a id="cpu-saturation-investigation-2026-09-23"></a>

## CPU saturation investigation (2026-09-23)

The baseline presentation path had two concrete optimization candidates: discard
unchanged pictures before scaling and encoding, and move ordinary-frame scaling
off the emulator goroutine. A bounded local experiment measured their cost; it
did not reproduce the user's unspecified saturated gameplay scene.

The initial investigation changed no product code, running server or existing
save. Diagnostic programs and measurements remain under the ignored
`build/cpu-investigation-20260923/`, with fresh saves under
`var/savedata/cpu-investigation-20260923/`. The baseline measurements below
describe the earlier checkout; the implementation and its verification follow
at the end.

<a id="cpu-saturation-investigation-2026-09-23-scope-and-method"></a>

### Scope and method

- Source: `53cec34`, Apple M1, 8 logical CPUs, 16 GiB RAM, macOS,
  Go 1.27.1 darwin/arm64. Initial load averages were 3.11, 2.56 and 2.35; the
  machine was not otherwise idle.
- The existing debug server was idle at the initial observation. The affected
  platform, machine, scene, speed and scale settings were not supplied.
- One KTF archive, SHA-256 prefix `23919eb33365`, was selected from the local
  library by large inner-JAR size. This is a reproducible workload, not a claim
  that archive size predicts gameplay cost. Another candidate ran but had too
  little work in this window for the main comparison.
- The standalone harness calls the existing KTF session API, advances a manual
  clock to each deadline, and runs 100 ticks from fresh saves. It links the
  committed CLI PGO profile and omits browser transport, logging and audio sinks.
- CPU time is process user plus system time from `getrusage`; wall time and Go
  allocation counters cover the tick loop after startup and an explicit GC. The
  frame harness links the committed server PGO profile and uses the server's hqx
  scaler, PNG `BestSpeed`, buffer pool and encoded-byte copy.
- Modes ran in interleaved orders, three repetitions each, and tables show
  medians. Small differences are not presented as established improvements. The
  known macOS sampling-profile attribution issue described in the
  [testing history](testing.md) is avoided for the timing conclusions.

<a id="cpu-saturation-investigation-2026-09-23-diagnostic-overhead-is-small-in-this-workload"></a>

### Diagnostic overhead is small in this workload

All twelve core runs completed 100 ticks, retired exactly 358,036,632 measured
guest instructions, reached 5.221374215 seconds of guest time, produced 100
flushes, and ended at frame digest `6571936240797073961`.

| Diagnostic mode | CPU seconds | Wall seconds | Allocated bytes | Allocations |
| --- | ---: | ---: | ---: | ---: |
| No trace or guest sampling | 2.752 | 2.539 | 25,831,536 | 7,923 |
| Ordered trace only | 2.762 | 2.544 | 32,267,704 | 7,936 |
| Trace plus sampling every 1,000 instructions | 2.780 | 2.567 | 34,088,760 | 371,900 |
| Trace plus sampling every 10,000 instructions | 2.785 | 2.560 | 32,499,112 | 44,349 |

The debug server enables the 1,000-instruction sampler in
`internal/webhost/session.go`. That configuration costs about 1.0% more process
CPU and 1.1% more wall time than the untraced one here. This isolates those knobs
in one binary and is not a complete debug-versus-release server comparison.
Sampling produces many small allocations, but reducing its frequency did not
establish a CPU improvement in these runs, so disabling it is not a general cure
for saturation.

A separate phase observation attributes nearly all tick time to timer callbacks,
about 25.35 ms per round. These callbacks execute guest code, so the number is
not evidence that the timer queue itself is expensive. The base core workload
takes about 7.09 wall-clock nanoseconds per retired guest instruction, including
its platform calls. Because the manual-clock probe removes waits, its busy
process is not a reproduction of a paced server saturating a core.

<a id="cpu-saturation-investigation-2026-09-23-scaling-and-encoding-have-measurable-cost"></a>

### Scaling and encoding have measurable cost

The captured frame has 240 by 320 pixels and eight distinct RGBA values. Each run
warms ten frames, then processes 300 copies through the presentation pipeline.
This measures one simple image, not a representative corpus of gameplay images.

| Scale | Scaling ms/frame | PNG and output copy ms/frame | CPU ms/frame | Allocated bytes/frame |
| --- | ---: | ---: | ---: | ---: |
| Original | negligible | 0.609 | 0.609 | 6,365 |
| hq2x | 2.675 | 2.461 | 5.172 | 1,421,531 |
| hq4x | 7.001 | 9.197 | 16.304 | 5,072,116 |

At twenty frames per second the measured hq4x cost projects to about 32.6% of one
core, against 1.2% at original scale. That is arithmetic from a component
benchmark, not a measured reduction in whole-server utilization; network writes,
browser work and guest execution are outside it. Selecting original scale avoids
this cost immediately when hqx is enabled.

At baseline, the ordinary server route was `sessionRunner.pushFrame` calling
`Session.Frame`, which calls `Session.magnify` for KTF and LGT. Scaling therefore
ran on the emulator goroutine, before the nonblocking queue send, so even a
picture discarded because the queue was full had already paid for scaling. PNG
encoding ran on the encoder goroutine.

KTF intermediate frames took a different route: `FrameUpdate` carries unscaled
pixels and the scale factor, and `writeFrames` scaled them on its own goroutine.
That path established an ownership boundary suitable for ordinary frames too.
Moving work changes which goroutine pays for it; it does not by itself remove CPU
work or parallelize guest instructions.

<a id="cpu-saturation-investigation-2026-09-23-unchanged-ordinary-frames-are-a-stronger-cpu-saving-candidate"></a>

### Unchanged ordinary frames are a stronger CPU-saving candidate

A separate capture of the same 100 ticks compared complete RGBA slices with
`bytes.Equal`: 97 of the 100 pictures were identical to their predecessor. The
captured sequence was replayed through hq4x and PNG, three interleaved runs per
mode, with a scratch-only duplicate check before scaling and encoding.

| Replay mode | Pictures encoded | CPU seconds | Wall seconds |
| --- | ---: | ---: | ---: |
| Process every picture | 100 | 1.554 | 1.549 |
| Process only changed pictures | 3 | 0.048 | 0.048 |

The check reduces presentation CPU by about 96.9% for this mostly static
sequence. Guest execution continues unchanged and is not part of that saving, and
moving scenes with few repeated pictures benefit much less. This was a component
prototype, not an implemented or browser-validated server fix.

The baseline `pushFrame`/`writeFrames` path had no pixel equality check. KTF's
separate intermediate-frame publisher already compared its own retained pixels;
the missing check concerned ordinary delivery and duplicates between the two
routes. A server-level comparison must include dimensions and scale, keep
independently owned pixels, and reset or force presentation on game changes,
reconnect or takeover, resume and display-setting changes. It must not skip
guest callbacks, timers or painting because the output is unchanged.

<a id="cpu-saturation-investigation-2026-09-23-scope-selected-for-implementation"></a>

### Scope selected for implementation

Compare raw owned pictures at the encoder boundary, preserve explicit redraws,
and scale only accepted pictures there. This targets repeated presentation work
and the emulator goroutine's unnecessary scaling. Before treating it as a fix,
verify the Go session and Host tests and the page's frame behavior: changed
frames, scale changes, reconnects, intermediate and ordinary delivery, and output
pixels.

If a saturated scene already uses original scale and changes every picture,
these measurements do not identify its cause. Capture that scene's platform,
profile, scale, speed, delivered frames and guest progress, then measure the
execution path. PGO, decode caches and several guest-loop fast paths already
exist. Adding sleeps or imposing a CPU ceiling trades guest progress for lower
utilization and is not established as an optimization by this investigation.

<a id="cpu-saturation-investigation-2026-09-23-local-evidence"></a>

### Local evidence

The ignored directory contains `main.go`, `frames/main.go`, `candidates.json`,
`measurements.ndjson`, `frames.ndjson`, `observation.json`, `dedupe.ndjson`, raw
frame captures and sampled allocation profiles. Archive paths appear only in the
ignored candidate mapping. There are twelve checked core comparisons, nine
single-frame pipeline measurements and six sequence replays. The core comparisons
agree on instruction counts, guest time, flush counts and final frame digest;
this does not establish frame-by-frame equivalence across whole game routes.

No product Go or JavaScript source changed during the initial investigation, so
that stage ran no full regression gates. The diagnostic programs built and all
measured runs completed.

<a id="cpu-saturation-investigation-2026-09-23-implementation-and-verification"></a>

### Implementation and verification

The server now queues unscaled `Session.FrameUpdate` snapshots, and its encoder
compares owned raw pixels, dimensions and scale before hqx or PNG work. The
synchronous `Session.Frame` contract is unchanged. The original-scale path also
benefits from duplicate detection; moving hqx affects only enabled
magnification. No execution steps, guest callbacks, timer cadence, audio or save
formats changed.

`FrameUpdate.Force` preserves start, resume and scale-change redraws. A full
queue does not consume the request; the emulator retries until it is accepted.
The writer sends the queued text before an explicit redraw, so a reconnecting
page receives its lifecycle answer before its only static picture. Ordinary and
intermediate frames share the encoder comparison.

The new regression first failed on the baseline with five encoded pictures
instead of three changes. The production change passes it, along with scaled PNG
pixel checks, same-byte shape changes, forced redraws, queue-pressure tests,
ownership checks and lifecycle-message ordering. Existing session and retention
tests also pass.

`BenchmarkFrameEncoding` was compiled against the baseline source through a Go
overlay and against the changed code, with the same committed server PGO profile
and benchmark. Each trial processes 100 authored 240 by 320 inputs, including
initial encoding; identical frames alternate independently allocated buffers.
Three interleaved baseline and candidate trials gave these median wall times per
input:

| Path | Baseline | Changed | Difference |
| --- | ---: | ---: | ---: |
| Original, unchanged | 429.265 us | 9.125 us | -97.87% |
| Original, changing | 427.550 us | 429.336 us | +0.42% |
| hq4x, unchanged | 13.494 ms | 0.144 ms | -98.93% |
| hq4x, changing | 13.454 ms | 13.472 ms | +0.14% |

These are encoder and queue component costs, not whole-emulator CPU reductions;
they exclude snapshot capture, guest execution and network writes. The unchanged
case sends one picture, and the changing case changes a pixel every time.
Sub-percent differences in the changing cases do not establish a regression. The
raw results and baseline source overlay are kept under the ignored
`build/cpu-reduction-20260923/`.

Both debug and release servers build. The required gates passed: `make test`
(including 227 Node tests), `make test-debug`, `go test -race ./internal/...` and
`go vet ./...`. A first gate run picked up mixed-package baseline source
snapshots in the ignored build directory; renaming them to `.go.txt` excluded
them from package discovery, after which the gates passed.

`web/acceptance/frame-delivery.mjs` passed all seven authored-fixture checks in
Chromium and WebKit: a static original-scale picture stays visible without new
PNGs, identical restart and resume pictures redraw, reconnection restores the
canvas, changed input paints arrive, MIDP scale requests keep native dimensions,
and WIPI frames switch among hq4x, original and hq2x. Browser artifacts are
under:

- `var/acceptance/frame-delivery-chromium-1790123231365/`
- `var/acceptance/frame-delivery-webkit-1790123279605/`

An additional Chromium run used the same local KTF archive at original scale,
with fresh isolated saves. Over six seconds after first presentation it sent two
more pictures, and the last statistics window reported no new PNG, 23.51 guest
ticks per second and guest speed 1.00001, which shows that suppressing static
presentation did not stop that runtime's progress. The route and all seven
authored checks passed in
`var/acceptance/frame-delivery-chromium-1790123390353/`.

The user-reported saturated scene was not identified or reproduced. This change
removes demonstrated presentation work; it does not claim to solve a workload
dominated by guest instruction execution. Physical phones, long gameplay routes
and total process CPU savings remain outside this result.

<a id="cpu-saturation-investigation-2026-09-23-frame-copy-remeasurement"></a>

### Frame copy remeasurement

Frame copies account for most allocated bytes in the measured scene but very
little execution time, so the evidence does not justify changing buffer
ownership to address CPU saturation. This follow-up measured the implementation
with duplicate PNG suppression already applied and changed no product code.

The same local KTF archive (`23919eb33365`) ran through the real Go HTTP and
WebSocket session handler at its original 240 by 320 resolution, with fresh
saves, while a Go client in the same process drained messages. After three
seconds of warmup each trial measured eight seconds. Debug and release each ran
three times in alternating order, on the same M1 and Go version as above, with
the server PGO profile. Initial load averages were 2.39, 1.86 and 1.84, and the
user's existing server kept running, so the machine was not idle.

An ignored Go source overlay times only the allocation-and-copy expressions and
counts their calls and bytes, keeping independently owned buffers. Process CPU
comes from `getrusage`, allocation totals from `runtime.MemStats`, and total GC
CPU from `/cpu/classes/gc/total:cpu-seconds`. Copy timings are elapsed time inside
those expressions, including possible scheduling and allocation assists, not
CPU-profile attribution. A separate allocation-profile run used a 4 KiB sampling
interval and before/after profiles, outside the timing trials, so the macOS
CPU-profile attribution problem does not rank this cost.

Every measured interval contained 188 ordinary Host snapshots and 188 copies in
`flushLCDRegion`, which preserves the previous LCD image during a partial flush.
No changed intermediate frame was offered. Each copy moves 307,200 bytes and
allocates 311,296 heap bytes after allocator rounding. The two sites together
allocate 117,047,296 bytes per interval, about 14.6 MB/s of allocation traffic,
not retained memory or a leak. Medians:

| Quantity over eight seconds | Release | Debug |
| --- | ---: | ---: |
| Process CPU time | 6.749 s | 6.798 s |
| All allocated bytes | 117.372 MB | 121.613 MB |
| Host snapshot share of allocated bytes | 49.86% | 48.12% |
| Host plus partial-LCD copy share | 99.72% | 96.25% |
| Elapsed time inside Host copies | 2.114 ms | 2.001 ms |
| Elapsed time inside partial-LCD copies | 2.352 ms | 2.373 ms |
| Elapsed time inside both copy sites | 4.494 ms | 4.373 ms |
| Total GC CPU, including unrelated allocations | 8.013 ms | 7.754 ms |
| Total GC CPU / process CPU | 0.119% | 0.114% |

The separate allocation profile confirms both large allocation sites. Profile
serialization adds allocations of its own, so the table's percentages use the
timing runs' counters and totals. The copy timing and the GC CPU must not be
added as a precise saving, because some work can overlap and elapsed copy time
and process CPU are different clocks; both still put the remaining cost far
below the guest execution workload.

A separate fixed-work experiment made 20,000 copies while keeping four
snapshots, with normal GC, three times. At 240 by 320, allocation plus copy costs
a median 35.8 microseconds of process CPU per operation, and copying into an
existing buffer 4.3 microseconds; at 320 by 480 these are 46.5 and 11.7. The
tight allocation loop triggers far more frequent GC than the server and is not an
end-to-end measurement. At twenty copies per second the first number projects to
about 0.072% of one core for one copy site, not a cure for a saturated
interpreter.

All server intervals kept guest speed near 1.00001 and about 23.5 ticks per
second. No new PNG was needed after warmup: the screen was stationary while the
guest kept flushing and executing code. The result covers this KTF scene on this
machine, not SKT or LGT gameplay, a high-resolution workload, several
simultaneous sessions, or the user's unspecified saturated scene.

The remeasurement concluded that the ownership contract stays as it is. Revisit
it only when a relevant workload shows substantial copy time or GC CPU, not on
the share of allocated bytes alone. Diagnostic overlays, source hashes, seven
final server runs, three primitive runs and the raw results remain in the ignored
`build/frame-copy-20260923/`: `results-v2.ndjson` and `summary.json` hold the
final server counters, and `results.ndjson` the primitive results and the initial
Host-only timing pass. Saves are isolated under `var/savedata/frame-copy-*`.

Both diagnostic build profiles compiled and all seven final server probes passed.
With the overlay enabled, focused tests passed for defensive snapshots, partial
LCD preservation, raw snapshot ownership, duplicate encoding, forced redraws and
lifecycle ordering, and `git diff --check` passed. The full regression gates
were not repeated for this measurement-only follow-up; the implementation's
results are recorded above.

<a id="cpu-saturation-investigation-2026-09-23-independent-pr-validation"></a>

### Independent PR validation

Before publication, the presentation changes were applied independently to
`main` at `bb2294f`. `make test` (226 Node tests), `make test-debug`,
`go test -race ./internal/...` and `go vet ./...` passed again. Both server
profiles built and reported their expected profile. The release server passed
all seven authored browser checks in WebKit, and those checks plus the local KTF
original-resolution route in Chromium. The timing measurements above keep their
original checkout provenance; this pass verified the independent PR's behavior
and is not a new benchmark.
