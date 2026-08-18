# Server sessions

A phone cannot run the browser build fast enough to play. The same build and
the same game measured about fifteen times slower on an iPhone than on the
desktop that served it — 2.3fps against 19.2, with 86% of the phone's main
thread inside the interpreter — and the cost is Go's WebAssembly backend rather
than the emulator: pure register work carries no penalty at all, while an
array index plus a branch costs 2.5x and the interpreter 6x. The interpreter's
state is arrays, so it cannot live in wasm locals, and three rounds of tuning
moved it by 0-4% (see [`armcore.md`](armcore.md)).

So the emulator stops running on the phone. It runs on a server that is left
running anyway, and the phone draws what it is sent.

| | |
|---|---|
| frame, PNG, measured over a real game | ~13 KB |
| bandwidth at 20fps | ~2 Mbps |
| server cost of one session (debug build, M1) | ~⅓ of one core |
| phone's remaining job | decode a bitmap, draw it |

## The shape

```text
browser                          server
  │  GET /            ───────▶   embedded client files
  │  WebSocket /api/session ─▶   one session per connection, parked if it drops
  │                              internal/session drives the platform
  │  ◀── binary frame (PNG)      internal/webhost encodes and sends
  │  ◀── text JSON               started / audio / stats / results
  │  ─── text JSON  ────────▶    start / resume / key / speed / scale / cheat
```

- `internal/session` is the platform-agnostic driver: inspect, start, tick,
  send a key, take a frame. It is where the differences between KTF, LGT and
  the two MIDP platforms are absorbed, so a Host never switches on platform.
- `internal/webhost` owns the socket, the protocol and the encoder.
- `internal/wsproto` is the framing (see [`architecture.md`](architecture.md)).
- `web/session.js` is the client: it opens the socket, draws frames with
  `createImageBitmap`, and replays audio events onto the page's synthesiser.

One connection drives one session, and a session now outlives its connection
for five minutes; see "A game outlives its socket" below.

## What travels

Text messages are JSON and small. Frames are binary and are the only thing
sent at rate; a PNG already carries its own dimensions, so a frame has no
envelope at all.

| From the page | Means |
|---|---|
| `start` | run this archive, named by its `games.json` path, optionally on a named screen size |
| `key` | one press, release or repeat, with the MIDP-style code the keypad has always sent |
| `speed`, `scale` | the settings panel's two knobs. `speed` reaches every platform; see "One setting, three clocks" in [`cli.md`](cli.md) for what each one scales |
| `cheat` | one panel operation, or a text console line |
| `report` | write the session's diagnostics under `logs/` |
| `stop` | end the game without closing the connection |
| `resume` | take back the game the server parked when the last socket closed, named by its token |

| From the server | Means |
|---|---|
| `ready` | the session will take a game, and which build profile is running |
| `started` | the game is up, with its identity and the screen it is actually drawing |
| binary | one finished frame, PNG |
| `audio` | a batch of sink calls in the order the guest made them |
| `stats` | frames sent, frames dropped, average tick cost, average frame size |
| `exited`, `error`, `result` | the game ended, something failed, an answer to a request |
| `resumed` | there was no game under that token — the answer when a resume finds nothing |

`ready` carries the server's build profile because the page has a developer's
half — the run log beside the screen and the button that saves a report — and a
release has no use for either. Which build is running is the server's answer
rather than the page's guess: they are the same files, and the binary serving
them is the one thing that knows.

### One touch can be a run of keys

A finger dragged across the keypad presses each key it crosses and releases the
one it leaves, so a thumb rolled around the direction pad sends 2, 6, 8, 4 in
turn without lifting. The page is where this lives; the server sees ordinary
`key` messages and cannot tell a slide from four separate presses.

A slide does not have to begin on a key. A thumb on a handset presses whatever
it reaches, so a finger that goes down on the screen or in the margin beside the
pad is followed too, and the first key it crosses is pressed. That is why the
keys are not bound one at a time — `pointerdown` is watched on the document, and
what the finger went down on only decides whether a key is pressed straight
away.

What a slide runs across is the pad: the two key blocks and the row of `*`, `0`
and `#` under them, which is `.keypad-main` and `.keypad-footer` in the markup.
The keypad's top row is deliberately outside it. Opts, the layout toggle, Call
and CLR are aimed at one at a time — a slide that woke one of them on its way
past would be a surprise, and CLR in the middle of a game is an expensive one —
so a press there holds that one key until the finger lifts, wherever it wanders,
and a slide crossing the row presses nothing. A slide does not begin on any
button that is not a key, nor on the keypad's own frame, nor in the run log or
the cheat panel, which scroll and are selected and belong to the finger that
lands in them. `web/keypad.test.mjs` holds the pad and the top row apart in the
markup, since which side of that line a button sits on is what decides its
behaviour.

Two details are what make it behave. A press that starts on a key captures the
pointer, because otherwise the moves and the release stop arriving the moment
the finger leaves that button — and capture then means the event no longer says
what is under the finger, so the point is asked with `elementFromPoint` instead.
A finger that started beside the keys needs no capture: its events belong to no
button and the window hears them wherever it goes. And the key being left is
released before the next is pressed, since a game reads them in the order they
arrive and one finger never means two keys held at once.

Off the keys a finger holds nothing, and sliding back on presses again. Two
fingers reaching the same key — one sliding onto what the other holds, or onto
the second button a key is printed on, which the type-2 and type-3 layouts both
do — are one press between them, released when the last of them leaves. That
bookkeeping is `web/key-holds.js`, kept out of the DOM so `web/key-holds.test.mjs`
can drive it directly; what is under the finger stays `app.js`'s question.

### The screen is part of starting a game

A `start` may carry a `width` and a `height`, and the settings panel is where a
page gets them. Almost every title here was drawn for 240x320 and that is the
default, which is why the field is optional and absent from an ordinary start.
A few were packaged for a smaller phone and **load their artwork by the screen
they are given** — one local archive branches on the width and asks for a set
of images its own package does not contain, so on a 240-wide screen it cannot
run at all. `docs/skvm.md` has that title's own branch.

Two rules keep the setting honest:

- **The size the server refuses is the size no handset had.** Anything outside
  32..1024 in either direction is answered with an error rather than clamped:
  it can only come from a page this server does not serve, and starting a game
  on a screen nothing was drawn for is worse than not starting it.
- **`started` reports the screen the platform took, not the one the page asked
  for.** All three platforms honour the request now, but a page that laid out
  for a size the game is not drawing would put the picture in the wrong place,
  so the frame's own size is what the page reads back.

**A screen is one answer, and it has to reach every surface a game can ask.**
This is the part that is easy to half-build. A WIPI title asks the platform how
big its screen is, and it asks again — by another route — where the pixels are
and how far apart the rows sit. On KTF that is `MC_grpGetDisplayInfo` and the
screen framebuffer record; two local titles read the size from the first and
then write straight into the buffer from the second. Give them a new size
through one route and not the other and every row after the first lands at the
wrong offset: the picture shears into diagonal bands and reads exactly like a
decoder bug. `internal/platform/ktf`'s `Client.SetScreen` is the single place
both answers are read from for that reason, and `screen_test.go` fails if they
ever disagree. The Java surface — `Display.getWidth`, a `Card`'s bounds — reads
the same answer, because a title that lays out against one size while the
platform draws at another is the same fault wearing different clothes.

The page remembers the choice per game rather than once, because it is a
property of the title rather than a taste, and it applies at the next start — a
MIDlet reads its screen once, when it starts.

**It is not the setting that makes the picture bigger.** That one is `scale`,
which magnifies a finished frame and is a taste; this one changes what the
guest is told and therefore which artwork it loads and how it lays a screen
out. The two are worth keeping apart when a report says a title "looks wrong":
an old game whose proportions are off on a modern display wants `scale` and the
page's own letterboxing, while a title that draws into a corner of the screen
or asks for images it does not carry is the one this setting is for.

### What a frame costs on its way out

A presented frame is compressed on the writer's goroutine and handed there by
the emulator's, and both halves used to allocate more than they needed to.

`png.Encoder` builds a fresh compressor per call unless it is given a buffer
pool — 1.25 MB and thirty allocations a frame, against 8 KB and none with one.
The pool is shared across sessions: the buffers are interchangeable, and a
household server runs few sessions at once. The bytes a page receives are
identical either way, which `session_test.go` pins.

The frame itself is no longer copied on the way to the encoder. Every platform
here answers `session.Frame` with a picture of the caller's own rather than the
one it is still drawing into, so the copy was a copy of a copy — a third of a
megabyte a frame, made only to be collected. That contract is the point: a Host
holds a frame across ticks and hands it to another goroutine, and one platform
was answering with its live conversion buffer while the MIDP surface handed out
the bytes its own guest threads paint into. Both now copy where they know they
have to, which is under the lock they already hold.

## Pacing and frame skipping

The emulator loop ticks with a budget and then waits for however long the guest
asked to be left alone — a guest that asks for two milliseconds and is given
sixteen loses the difference out of every wait it takes, and it takes one
between every pair of frames. It waits on the command channel rather than
sleeping, so a key does not have to wait out the game's idle time.

**The budget is 32ms here and 8ms in the browser, and the difference matters
for more than latency.** The browser's Host shares a thread with drawing the
page, so a long entry is a long freeze. Nothing here shares anything. What the
budget still buys is a bound on how long a key waits behind a busy game — but
an entry that stops on its budget is also read by the platform as saturation,
because in a browser that is what it means. On a server that re-enters
immediately and for free it means only that the budget is small, and a budget
short enough to cut ordinary entries has the session giving up frames on a
machine with a core to spare. `HostCosts.PaintLoad` is that reading, and it is
in the debug report: under 1 is a Host with time to spare, over 1.1 is one that
starts dropping paints to keep the game's logic on schedule.

Measured on an M1 with a real title: 0.04-0.20 at every magnification, no
paints dropped, 19fps. Driven at sixteen times speed the same session reports
1.57 and drops paints as designed, holding the logic at 16x while the picture
falls to 15fps.

**A round with no logic in it keeps its paint no matter what the load says.**
The trade only works when there is something to trade for, and on KTF the paint
can be the round's only work — one title runs its frame loop from its card's
paint and advances its client thread's next wake-up inside it. Skipping starved
that thread, which then read as work waiting and spun the emulator. See
[`ktf.md`](ktf.md#what-frame-skipping-cannot-pay-for).

LGT and the MIDP runtimes have no clock of their own and never report
themselves idle. In a browser that went unnoticed, because the page ticked them
from its frame clock; a server that believes "no wait" spins a core and runs
the game as fast as the machine allows.

**For the MIDP runtimes `session.FramePace` is the answer, and for LGT it was
the wrong one.** Those runtimes read the wall clock directly, so a fixed wait
between ticks only decides how promptly deferred work runs. LGT's guest clock
is virtual: a tick moves it by the session's tick and nothing else does. So a
fixed wait there does not set responsiveness, it sets the game's *speed* —

    speed = tick / (host tick cost + fixed wait)

which is right only when the tick cost happens to land on one number. Measured
on an M1 with a real title: 1.79-2.48x through the menus, where a tick cost
10ms. On a slower server the same title ran at 0.68x in-game, where a tick cost
57.7ms — uniform slow motion with a rock-steady frame rate, because nothing was
being dropped. The game was simply being told less time had passed than had.

`lgt.Session.TickFor` anchors to the wall clock instead: a tick is due every
session tick of real time, and the wait is whatever is left of it. Measured at
1.00x steady, 20 ticks a second. The wait is zero once a tick overruns — a tick
costing more than it represents cannot be bought back — and the debt is capped
at one tick so a world load inside a single call is not repaid by sprinting
through the scene after it. `lgt.Session.Tick` still steps without pacing, which
is what the CLI batches with.

**The debug report is the only window onto a phone.** No profiler attaches
there, so `tick cost:`, `over time:` and the per-phase `worst` lines are the
whole picture of what a handset is actually spending, and a report from the
phone beside one from the desktop is how a difference gets found at all.

One trap makes that comparison lie: **a phase average is not divided by the
run's rounds.** Once frame skipping is on, paint runs in only some of them, so
dividing its total by every round reports a paint several times cheaper than it
is — which once read as "the phone paints as fast as the desktop" when it was
painting a third as often. Divide a phase by the rounds that ran it.

A frame is pushed to the encoder only when the flush counter moves, and the
channel holds one. When it is full the frame is dropped and counted: the game
has already moved on, and sending a backlog of stale pictures to a phone that
is behind makes it later still. Encoding runs on its own goroutine, so
compression costs frames rather than guest speed.

## Saves

The emulator and the save tree are now on the same machine, so saves never
cross the network: a session writes through `backend.DirectorySaveStore` into
the same `savedata/<profile>/<platform>/<owner>/` tree the native CLI and the
save API use. The page no longer preloads anything before starting a game.

The save API remains for the CLI's `DirectorySaveStore` layout, which both
Hosts read.

## Cheats

The panel's operations are defined in one place: `internal/cheat`'s panel API
answers plain Go structs and `internal/webhost` sends them over the socket. The
panel in the page calls one bridge whose methods are promises, because every one
of them is a round trip.

Cheat operations run on the emulator's own goroutine, between ticks, because
that is the only time reading and freezing guest memory is safe.

Which sessions have one is asked of the session rather than of the platform:
`session.Cheat()` and `session.CheatConsole()` answer for every platform, and
the page removes the toggle where the answer is nil. That indirection is what
made the third one a two-line change — the MIDP runtime used to answer nil,
because its state is Go objects with no addresses to sweep, and it now lays a
synthetic address space over its object graph and answers an engine like the
others (`docs/skvm.md`, "A heap with addresses in it"). `docs/lgt.md` has why
the indirection was worth the line it costs.

One operation differs by platform rather than by session: a **watch** needs
store instrumentation, which only the ARM platforms have. `Session.CanWatch()`
is what a panel asks, and the engine answers `cheat.ErrWatchUnsupported`
elsewhere rather than doing nothing.

**Driven end to end, on running games rather than fixtures.** Piping timed
commands into `runlgt -cheat` and `runktf -cheat` walks the whole vocabulary
against a real title: `regions` labels the address space, `scan unknown` over
669,696 candidates narrows to 190 and then to 2 under `scan changed`, `list`
shows those two module globals counting up live, `set`/`read`/`dump` agree,
a `freeze` survives seconds of the game writing over it, `save` and `load`
round-trip the table, and `watch` plus `hits` names what is behind each write —
`0x01403de0 written by pc 0x00001054, 110 time(s)` for a guest instruction, and
`written by host, last pc …` for a store this platform made itself, which is a
distinction the reader needs rather than a detail (`docs/armcore.md`, "Not every
store is a guest instruction"). The
same flow through the socket is `TestLocalCheatProbe`, which passes for all
three platforms — on a MIDlet the regions it lists are the game's own classes.

## The WebAssembly path is gone

The in-page engine was kept for a while behind `?wasm=1`, on the grounds that a
desktop could still run it. Nothing needed that: the server is a local binary,
so anything able to open the page has already started the process that emulates
better. It was removed along with `cmd/wasm`, the loader, and the module the
service worker had stopped caching.

## What has been driven, and what has not

Every platform reaches the browser through the same session, and the server
half of that has been exercised for all three: a socket opened against a
running server, a title started by path, PNG frames counted as they arrive, a
key sent by code, and audio events counted. An LGT title answers 362 frames
over eighteen seconds, advances when it is sent key 148, and one of them sends
twelve audio events in the same window — so the driver, the input path and the
audio path are not KTF-only.

**The browser half is checked by hand.** Nothing here automates a browser, so
the canvas draw, the oscillator synthesiser actually making a sound, and how
the PWA feels to hold are the parts a person confirms. `web/session.test.mjs`
covers the client's protocol handling; it does not open a page. To do it by
hand: run the server and point a browser at it.

## A game that ends is not a game that failed

Guest code runs inside whatever Host call reached it, so a game can end inside
one: a menu whose confirm key quits calls `MC_knlExit` from the key handler,
and the key delivery is what finds out. `Tick` has always recognised that and
reported it as an ending; the input path had not, so a player who quit a game
saw **"the game failed"** printed over its last frame, and the session stayed
up streaming that frame until the page was closed.

Both paths now answer the same way: the session closes and the page is told the
game exited. `session.ErrExited` is what an input call reports it with, and
anything else stays a failure.

### A guest thread that panics is a failed game, not a dead server

Guest code also runs on goroutines of its own: each platform's guest threads,
and the KTF session start that keeps a Host's event loop alive while it runs. A
panic on one of those goroutines is not caught by anything — a panic in an HTTP
handler is, which is why this was invisible from the page — and it takes the
process down.

The process is the wrong thing to lose. It holds the session's post-mortem
report, its ordered boundary trace, the page's log and any other session that
was fine, and what a panic here actually means is one archive doing something
this emulator does not implement. **A panic is the least informative failure
this emulator has, and that is the argument for converting it rather than for
surviving it.** So each of those goroutines catches its own: the stack goes to
the log at error level, and the caller gets the error it was already prepared
to receive — the game fails and the session ends the way any failed session
does. `backend.GuestPanic` is the shape of it; `internal/jvm` says the same
thing in its own eight lines, because the Execution layer does not import the
Runtime.

Nothing retries and nothing continues past one: the thread that panicked is
over. What follows the catch — clearing the thread's alive flag, releasing the
monitors it held — runs either way, because a title waiting on `isAlive` or on
a lock would otherwise wait for the whole session.

## A game outlives its socket

Switching to another app on a phone suspends the page, and the browser drops
the socket behind it. One connection being one session meant that was the end
of the game: coming back a minute later found a page saying the connection had
gone and a game that would have to be started over. The phone did nothing
unusual — it backgrounded a tab.

So a session whose page goes away is **parked** rather than closed
(`internal/webhost/resume.go`). It keeps its guest memory, its save handle, its
diagnostics and the picture it had, and it waits five minutes under a **token**
the page was given in `started`. A page that comes back sends `resume` with the
token and is answered with the same `started` message and the picture the game
was holding; a page that comes back too late is answered `resumed: false` and
shows its game list again. Nothing else identifies a session — a resume names
no archive, which is what makes it a resume rather than a restart.

Four decisions are worth keeping:

- **A parked game does not tick.** Nobody is watching it, and a game driven
  with no one there can be killed while its player is away. Freezing is also
  what a handset does when a call suspends an application, so the guest sees
  the time away as one long wait — the same jump a suspended handset produces,
  and the reason the window is minutes rather than hours.
- **The game's context is not the connection's.** This is the part that does
  not work if it is missed: a KTF guest thread parks *inside* a Go call that
  captured the context it was last granted a slice under, so a session ticked
  under its socket's context dies on the first tick after a resume with
  `context canceled` surfacing from inside guest code. A game's context now
  ends when the game is closed and travels with it into the parked set.
- **Stopping is not parking.** The page's `stop` and a game that exits both
  clear the token: a game somebody closed is not one to come back to.
- **The token is random, and spent when it is used.** It is the one thing that
  hands a running game to a socket, so it is 128 bits from `crypto/rand`
  rather than a counter, and two pages racing on one token cannot both get the
  game.

On the page, the token lives in `sessionStorage` rather than in a variable,
because the phone is also where the page itself is discarded and reloaded in
the background — the reload is the case the game most needs to survive.
Reconnection is driven by `visibilitychange`, `online` and `pageshow` as well
as by a retry loop, because a backgrounded tab has its timers throttled to the
point where a loop may not run at all: the event that says the phone is back is
worth more than any interval. The restart button clears the token before it
reloads, so restarting still starts the game over.

## What is not solved

- **Input latency is unmeasured.** It is a LAN round trip plus one frame, and
  what that feels like on a real network has not been established.
- **Two tabs on one game are two sessions** writing the same save directory.
  Nothing arbitrates between them. Parking does not change that: a token names
  one parked game, and a second tab starts a second session as it always did.
- **A parked game is not persisted.** It lives in the server's memory, so
  stopping the server ends every game waiting for a page, and the page finds
  its token unknown when the server comes back.
- **There is no authentication.** The server binds every interface so a phone
  can reach it, and anything else on that network can reach it too.
- **Live sessions are not capped.** Parked games are — four, because each one
  costs a running game's guest memory — but a server takes as many live
  sessions as sockets ask for, and a KTF arena alone is mapped at 64 MiB. The
  cap is missing on purpose rather than by oversight: it would refuse a
  connection that works today, and the machine this runs on is one household's.
  If a limit is ever added it belongs beside `maxParkedSessions`.
