# Server sessions

The server executes games through `internal/session`; the browser sends input
and displays frames and audio. `internal/webhost` owns the HTTP and WebSocket
transport and the encoder. Both the server and the CLI call the same platform
composition. [Session history](history/session.md) keeps the protocol's
rationale, earlier measurements and investigations. The server's other HTTP
routes (`/api/saves`, `/api/savepack`, `/api/games`, `/api/status`,
`/api/shutdown`, `/api/debug-log`) are described in [running](running.md) and
[architecture](architecture.md#debug-run-logs).

## Transport

The page opens a WebSocket at `/api/session`. Text messages carry JSON commands
and results in both directions. The query string negotiates everything else; the
current page asks for `protocol=2&sound=resume&timing=1&pcm=1&phase=1`, and adds
`pictures=webp` when its browser decodes lossless WebP:

| Query | Effect | Requires |
| --- | --- | --- |
| `protocol=2` | Pictures are binary updates at the guest's own size, magnified by the page, and sound is binary with each sample carried once. Any other value, or none, is protocol 1: complete PNGs magnified on the server, and sound as JSON. | — |
| `pictures=webp` | Protocol 2 pictures are lossless WebP instead of PNG. | `protocol=2` |
| `sound=owned` | Independent clip ownership ([below](#protocol-2-sound)). | — |
| `sound=resume` | Ownership plus note envelope ages. | — |
| `timing=1` | Presentation timestamps and a clock frontier. | `sound=resume` |
| `pcm=1` | Independent PCM channels. | `sound=owned` or `sound=resume` |
| `phase=1` | Fractional PCM resume positions. | `sound=owned` or `sound=resume` |

An unknown value is answered with the older behavior rather than refused, so
older pages remain compatible, and the current page also reads protocol 1 from
an older server that ignores the query. The page tests WebP once before its
first connection by decoding a two-pixel picture with transparency; a browser
that cannot, or that has not answered within two seconds, keeps PNG. A server
that does not know `pictures=webp` sends PNG, which the page reads as well.

Every page message is a JSON object with a `kind`. Unknown fields are refused,
a message is at most 1 MiB, and `id` (when nonzero) is echoed by the answer so
the page can match it to its request:

| Page message | Fields | Purpose |
| --- | --- | --- |
| `start` | `game`, `token`, `quick_load`, `value` (initial scale 1–4), `width`, `height` (32–1024), `confirmation` | Start an archive from its `games.json` path. Without a screen size the archive decides: a KTF descriptor's `DisplaySize`, an SKT archive's resource names, and otherwise 240×320. The page leaves it out only for `자동`; a chosen size is always sent. |
| `resume` | `token`, `takeover` | Find the browser token's retained game; `takeover: true` transfers control from another connected page. |
| `park` / `stop` | — | Retain the game without control, or end it. |
| `key` | `action` (`press`, `release`), `code` | One key event with the MIDP-style code the keypad sends. A `repeat` from an older page is dropped; the session makes the handset's repeat itself. |
| `pointer` | `action` (`press`, `drag`, `release`), `x`, `y` | One touch in the guest's own screen coordinates; the page removes its own magnification and letterboxing first. |
| `speed` / `scale` | `value` | Change the execution rate or the presentation scale. |
| `text` | `action` (`open`, `commit`, `cancel`), `edit`, `text` | Native text editing; see [native text input](native-text-input.md). |
| `quickSave` / `quickLoad` | — | Save the running game's execution state to the archive's slot, or bring the game back to it. Ordinary saves are not reverted. |
| `audioResume` | — | Ask, within the current epoch, for an `allOff` followed by the reconstructed current audio output. |
| `cheat` | `op`, `type`, `filter`, `operand`, `address`, `all`, `table` | One cheat panel operation. The text console is the CLI's (`-cheat`); a line of it sent here is refused as an unknown operation. A refused search answers an `error` in the panel's words, such as an unknown-value search over more than 4,194,304 addresses. |
| `report` | `label` | Write the session's diagnostics report. A debug build only, and within the rate `/api/debug-log` keeps (10 a minute, shared with it); a release answers with an error and writes nothing. |
| `ping` | — | Check connection liveness; answered from the reader without waiting for guest execution. |

| Server message | Carries | Meaning |
| --- | --- | --- |
| `ready` | `profile` | The connection takes a game; the server's build profile decides whether the page shows its debug half. |
| `started` | `started` | The game is up, or a `resume` found it. |
| `restored` | `started`, `epoch` | A quick load replaced the running game ([checkpoints](#ktf-checkpoints)). |
| `resumed` | `resumed`, `occupied`, `message` | No retained game under the token, or `occupied: true` when another page controls it. |
| `detached` | — | Control moved to another page; presentation stops until the user takes it back. |
| `result` | `message`, `cheat`, `textInput`, `confirmation` | An answer to a request; a `confirmation` asks the page to confirm a start that would end a retained game. |
| `error` | `message`, `exited` | A request failed. `exited: true` marks a game that ended itself rather than failed. |
| `exited` | `message` | The running game ended; the message is its reason. |
| `audio` | `audio` | Protocol 1 sound: a batch of sink calls in the order the guest made them. |
| `vibrate` | `vibrate` (`level` 0–100, `ms`) | One vibration request. `ms` zero with a level above zero runs until the next request; level zero stops. |
| `stats` | `stats` | Every two seconds: `fps`, `skipped`, `tick_ms`, `frame_bytes`, `tick_rate`, `speed` (guest over real time), `shed` and `bytes_per_second`. |

The `started` description carries `platform`, `game`, `aid`, `pid`, `name`,
`save_owner`, `main_class`, `authentication`, the screen the game actually got
(`width`, `height`), the screen a start without a size takes for that game
(`auto_width`, `auto_height`, which the page's `자동` means), the browser
`token`, `can_watch`, `can_touch`,
`can_checkpoint`, `has_checkpoint`, `restored` and `speed`. Native text editing
uses edit identifiers and stale-target checks. Detailed payloads and their
compatibility history are in [the transport record](history/session.md).

Up to 64 page commands queue while the emulator is inside a tick; past that the
newest is dropped. Up to 64 finished text messages and one picture wait for the
socket, and audio, statistics and vibration are dropped rather than waited for
when the connection is behind (`shed`). A write that does not finish within ten
seconds ends the connection. Each pass of the session loop starts guest rounds
for up to 32 ms (`tickBudget`) before it reads input again; a round already
running is not cut short.

## Retention and control

The server retains up to four active and parked games together. A game can
outlive its controlling socket with no elapsed-time eviction. Returning with the
same browser token resumes it; moving control from another connected page
requires explicit takeover, which the server completes within ten seconds or
refuses. The old page receives `detached` and does not reclaim control
automatically.

A start that would exceed the four-game limit, or that would replace a parked
game holding the same saves, is answered with a `confirmation`; the page repeats
the start with it to end the oldest parked game (or that game) and begin. A start
whose saves a game attached to another page is using is refused. Active games
cannot be evicted to make room. The four-game limit is not a
resident-memory budget or a limit on sockets or archive-inspection requests.

Parking releases held input, detaches presentation and drives the platform's
pause boundary. Resuming reattaches presentation and drives resume, then sends
the picture the game had. These callbacks do not establish that all guest
threads and clocks freeze.

Stopping the server ends every game it holds. A parked game is closed on a
goroutine of its own and keeps its save folder claimed until its close has
returned. A game whose page is still attached is asked to close: its runner
finishes the round it is in and closes the session on its own goroutine, as a
page's `stop` does, and the server waits for that before it exits. The HTTP
server's shutdown does not reach a session socket, so without this a connected
game would end with the process, and what its title had written and not yet
handed to the save store (a file it left open, keys a native package keeps until
a frame ends) would be lost. A close is bounded on its own: a title's ending
(`destroyApp`, `destroyClet`, a Jlet's `destroyApp`) runs for fifteen seconds
at most, after which its call is ended and the close stores what the title
wrote. Parked games used to be closed one after another under the server's lock
before the wait began, so one whose `destroyApp` never returned kept the server
from ever stopping. Once a stop has begun, nothing starts, resumes or parks.
Ordinary saves and quick save slots survive, and a later page can return to the
slot's moment after starting the game.

<a id="stopping-bounds"></a>

**How long a stop waits is derived from two bounds**, which every platform
holds a title's code to and which `internal/backend/bounds.go` keeps in one
place, so that each wait below moves with them:

| Bound | Value | What it is |
|---|---|---|
| `backend.WaitBound` | 15 s | one call a title makes waiting on the clock (a KTF service call; the longest legitimate one is 5.2 s), and so the longest a round of an attached game can still be running when the stop comes |
| `backend.EndingBound` | 15 s | a title's own ending: SKT `destroyApp` (`destroyWait`), LGT `destroyClet` (`closeWait`), a KTF Jlet's `destroyApp` |
| `webhost.StopDeadline` | `WaitBound` + `EndingBound` + 5 s = 35 s | the drain: the games, attached and parked side by side, and the server's own requests; a close still running then is named in the log and left behind |
| launcher `drainWait` | `StopDeadline` + 5 s = 40 s | how long `stop` waits for the drain before it signals, at each of its three steps |
| Android `ENDING_WAIT_SECONDS` | 40 s | how long a reopened app waits for the server a finished activity ended; a test holds it past `StopDeadline` |

The deadline was ten seconds, shorter than an ending, and then the ending plus
five seconds; an attached game finishes the round it is in before its ending,
so a round that had waited past those five left the title behind, still ending,
before the store after it. A stop with nothing slow in it is over in well under
a second; a title has to wait out both bounds for the deadline to matter, and a
request that stalls — an upload a phone stopped sending — holds the stop until
it. On
Windows, closing the server's console window ends the process about five
seconds after the window closes, whatever the drain is doing (see
[`running.md`](running.md#stopping-a-server)).

<a id="ktf-checkpoints"></a>

## Checkpoints

The shared session supports KTF Java/AOT, older descriptor modules and native
packages, LGT Clets and AOT Java titles, and SKT Java/MIDP/WIPI and GNEX/GVM SGS
titles ([SKT checkpoints](skt-checkpoints.md)). Each reports
`can_checkpoint: true` when the game has a save directory. A `start` with
`quick_load: true` constructs from the archive's slot without running guest
startup. The ordinary `resume` command still means reconnecting to a retained
in-memory session. The envelope, the slot and the records are in
[architecture](architecture.md#where-a-stopped-game-keeps-its-state).

The page offers quick save and quick load as keypad assignments, placed in
top-row columns 5 and 6 of the Type1–3 defaults and optional in Type4. Pressing
an assigned key sends its request immediately and shows brief inline feedback
without a confirmation or result popup. Every copy of the key is disabled while a
request is pending, and editing a cell never sends a request. The picker offers
ordinary startup only: to restore after a server restart, start the game and
press its quick-load key. Explicit startup restoration remains available to
protocol and CLI callers.

A checkpoint is execution state only, and the game's own saves come first:
neither command replaces, reverts or removes an ordinary save. See
[Quick load and ordinary saves](architecture.md#quick-load-and-ordinary-saves).

`quickSave` runs between complete rounds. It first stores any write the game has
issued and the Host still holds (an open file's buffer on LGT or SKT, a written
name not yet stored in a native KTF package), then captures, then writes one slot
per exact archive. The slot holds no save.

`quickLoad` reads the archive and the slot, validates the record into a detached
runtime that has no access to the saves, fills what that runtime has open from
the saves as they are with reads only, stores the running game's issued writes
as `quickSave` does, and then replaces the running game. The restored game reads
the saves on disk and writes on top of them. With nothing pending, a load writes
nothing.

Either command is refused, with the running game left running and no save
reverted or removed, when the archive or the slot is invalid, the runtime is in
a state a record cannot describe, the store refuses the game's pending writes, or
a save the load needs cannot be read. Three refusals reach the page as a sentence
in its own language, with the cause in the server log: a slot written in an
earlier format (which is left in place), pending writes that could not be
stored, and saves that could not be read. "Could not be stored" covers a store
that refused the write and, on LGT and SKT, a buffer the Host declined to store
because its file had changed behind it; "could not be read" covers a failed read
and saves larger than a load reads. Every other refusal passes through as the
error's own text.

One such state is audio output holding a MIDI program change, control change
or pitch bend whose fields do not fit MIDI (a channel above 15, a 7-bit field
of 128 or more, a bend of 16384 or more): from the first one played, every
quick save would be refused. A sound carrying one is therefore refused when it
is loaded, as a note with the same fault already was, so it never plays. A
checkpoint an earlier build wrote that holds such a sound still restores.

A `start` with `quick_load: true` reads and checks the slot before the game the
connection is running is stopped, so a start that is certain to be refused (no
slot, an earlier format, a damaged slot, another archive's) leaves that game
running. `has_checkpoint` is true for an earlier-format slot as well, so the
load is offered and its refusal can say why.

Slots are outside ordinary `.wfs` exports, and a `.wfs` import is unchanged: it
writes the saves it carries over the ones on disk.

A successful load sends `restored` with a new connection `epoch` and a `started`
description before new output. Mutating input, text, cheat, speed, scale,
checkpoint and `audioResume` requests carry that epoch (omitted means zero), and
requests from a prior epoch are refused with "request belongs to a previous
timeline". Both output queues and the frame encoder discard work from the prior
epoch, the page's sound definitions start over, and the next picture is
complete. The page cancels its
pending cheat, text and checkpoint requests, closes its old decoder, and clears
sound, vibration, input, text and cheat state before the fresh complete frame.
Binary messages keep their format; the ordered reset and queue ownership mark the
boundary. The Host releases saved physical input and resumes a paused slot
through the normal lifecycle. A CLI caller keeps saved input and pause until it
changes them.

On LGT a key is queued and delivered by the next tick, so the release the Host
sends for a restored hold reaches the title one tick after the load rather than
inside it. That platform has no repeat event and no pointer, and its pad rule is
the platform's own, so its checkpoint carries the Host's held keys and the pad's
state and nothing else of the three.

Loaded notes resume with their original programs, current channel controllers
and saved envelope ages on capable pages. Pitch sensitivity and RPN/NRPN
selection are restored before notes; pedal-held released keys receive note-off
immediately after resuming under sustain. PCM tails resume with their remaining
samples. Paused clips stay silent until explicitly resumed. Oscillator phase,
release tails, physical output latency and short controller smoothing
transitions are not serialized, so this is not sample-exact audio continuity.
Runtime state compatibility requires the same archive and a compatible
checkpoint schema; debug and release share the schema.

## Presentation and audio

Ordinary ticks use `Session.FrameUpdate`, which transfers owned, unscaled pixels
and a presentation scale. Encoding, and protocol 1's server-side hqx, run on the
encoder goroutine behind a one-frame queue, so a discarded picture costs no
encoding work on the emulator goroutine. The synchronous `Session.Frame` API
still returns already-scaled pixels for other Hosts; MIDP surfaces stay at their
native size and are never magnified. The page decodes pictures and draws them,
magnifying them in protocol 2, and executes no guest instructions. Speed and
scaling are separate settings.

The encoder compares consecutive raw pictures, dimensions and scale before
scaling or encoding. An unchanged picture needs no new picture or network
message, while guest callbacks, timers and drawing still execute, so a static
screen can report zero delivered frames per second while guest ticks continue.
Start, resume and display-setting changes force presentation even when the pixels
match. That request survives a full queue until it is accepted, and its queued
lifecycle answer is written before the forced picture.

KTF additionally offers explicit LCD flushes through `backend.FrameSink` and
`session.Options.FrameUpdates`, including while startup or a long callback holds
the execution lock. Each `FrameUpdate` owns its pixels. Sampling occurs at most
once per 1/60 wall-clock second, skips unchanged samples, and never waits for the
consumer. Hosts without a sink keep the pull-only path. No timer invents a flush
that guest code did not request.

Parking detaches the sink before its queue closes; resume installs the new queue
before guest execution resumes. Intermediate frames use the same negotiated
picture format. [The CPU investigation](history/session.md#cpu-saturation-investigation-2026-09-23)
records component benchmarks, authored-fixture Chromium/WebKit checks and a
bounded local KTF browser run; these do not establish all-game performance or
physical-phone acceptance.

Audio uses shared backend timelines. The page's synthesizer consumes audio
messages, subject to browser audio activation. Borrowed PCM and SysEx slices must
be copied before asynchronous use. See [audio](audio.md) and
[shared service contracts](architecture.md#shared-runtime-services).

### Protocol 2 pictures

The encoder compares each picture, at the guest's own size, with the one the
page holds once the previous message has been accepted by the writer queue. All
encoding is lossless at the game's frame rate. Every picture message starts with
a sixteen-byte big-endian header:

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 4 | ASCII `WFP2` |
| 4 | 1 | Operation: 0 complete, 1 replace, 2 masked |
| 5 | 1 | Presentation scale the page magnifies by, 1 to 4 |
| 6 | 2 | Horizontal shift of the held picture, signed (masked only) |
| 8 | 2 | Vertical shift, signed (masked only) |
| 10 | 2 | Rectangle `x` |
| 12 | 2 | Rectangle `y` |
| 14 | 2 | Reserved, zero |
| 16 | — | PNG, or lossless WebP where the page asked for it; its dimensions are the rectangle's |

- **Complete** replaces the held picture and its size. The first picture, an
  explicit redraw, and a change of size or scale are complete.
- **Masked** draws its rectangle over the held picture. A pixel equal to the one
  held is written transparent and keeps it; every other pixel in the rectangle is
  opaque. Every other update is masked. Runs of identical transparent pixels
  compress well, so a change spread across the screen costs little more than the
  pixels that changed.
- **Replace** replaces its rectangle, transparency included. It is used only when
  a changed pixel is not opaque, which a masked update cannot express.
- **Shift.** When at least a thirty-second of the pixels changed and the held
  picture is opaque, the encoder looks for the held picture moved by up to 16
  pixels along either axis, scoring candidates on every fourth row of the changed
  area. It tries the last shift it sent first and keeps it when it still matches
  seven eighths of the sampled pixels. If the best shift leaves at least a fifth
  fewer changed pixels, a masked update names it: the page first draws its held
  picture at that offset over itself, and the strip it uncovers keeps what it
  had. Scrolling fields change almost every pixel a frame and move by one or two.
  A shift that already produced every pixel carries no picture.
- **Palette.** A PNG rectangle with at most 256 distinct values, the transparent
  one included, is written with a palette. Protocol 2 PNGs use zlib's default
  level; protocol 1's complete PNGs use the fastest.
- **WebP.** `internal/vp8l` writes the rectangle as lossless WebP, implemented
  from RFC 9649 rather than taken from a library. A rectangle with at most 256
  colours goes through a colour table, packing up to eight pixels into one where
  the colours are few; any other has green subtracted from red and blue. Both
  then use backward references (copies from the left, from the rows above through
  the format's short neighbourhood codes, and from anywhere earlier through hash
  chains over runs of two and of six pixels) chosen for the bits they save, and a
  colour cache sized by estimate. The predictor transform is tried only for a
  picture still above six bits a pixel, such as a photograph, because the games'
  flat areas and repeated tiles copy far better than they predict. A transparent
  pixel's colour is chosen to cost least, since nothing on the page can show it.
  On recorded play this is a fifth to a half fewer bytes than the PNGs, for 0.8
  to 1.6 times their encoding time; see
  [the record](history/session.md#frame-bandwidth-webp-2026-09-25).

Each update depends on all prior binary messages on that connection. Raw frames
can be dropped before encoding; encoded updates stay ordered and cannot be
dropped independently.

`web/frame-stream.js` decodes messages serially and composes them on a retained
canvas at the guest's size, reporting the scale and the changed rectangle. The
display coalesces draws of that canvas without losing updates, and
`web/magnify.js` applies hqx at the scale the server named, redoing only the
magnified blocks the changed rectangles can reach; see [hqx](hqx.md). Image
dimensions are bounded at 4096 per axis. At most 32 waiting messages and 96 MiB
of queued or in-flight data are accepted. A malformed message, a decode failure
or excessive backlog closes the connection, and session recovery resumes the
retained game with a complete picture. The server does no hqx work for these
connections. A bare PNG from a protocol 1 server is read as a complete picture
already magnified.

### Protocol 2 sound

A sound message is binary: ASCII `WFA2` and the tick's calls in order, each an
operation byte and its operands, with multibyte fields big-endian.
`internal/webhost/audio_stream.go` lays the format out:

| Byte | Operation | Operands |
| --- | --- | --- |
| `0x01` | note on | channel, note, velocity |
| `0x02` | note off | channel, note, velocity |
| `0x03` | program change | channel, program |
| `0x04` | control change | channel, control, value |
| `0x05` | pitch bend | channel, 16-bit value |
| `0x06` | all off | — |
| `0x07` | select sound | 32-bit sound handle |
| `0x08` | stop sound | — (the selected sound) |
| `0x09` | sound gain | 16-bit gain, 10,000 is unity |
| `0x0a` | resume note | channel, note, velocity, 32-bit age in milliseconds |
| `0x0b` | select time | presence byte; 1 is followed by an IEEE 754 float64 time in seconds |
| `0x0c` | clock | IEEE 754 float64 presentation time in seconds |
| `0x10` | define | 32-bit id, 32-bit length, the bytes |
| `0x11` | play wave | 32-bit id, channels, 32-bit sampling rate |
| `0x12` | SysEx | 32-bit id |
| `0x13` | forget | — (drop every definition) |
| `0x14` | play PCM wave | 32-bit id, channels, 32-bit rate, 16-bit PCM group |
| `0x15` | PCM control | 16-bit PCM group, control, value |
| `0x16` | resume wave | 32-bit id, channels, 32-bit rate, 16-bit PCM group, 32-bit frame phase |

A sampled sound or SysEx message travels once as a definition under an id and is
named afterwards; ids are never reused. The definitions a page holds are bounded
at 8 MiB, past which the server tells it to forget them and starts over. A sound
message shed because the connection is behind leaves the page's definitions
unknown, so the next one starts over the same way. The page skips a sound whose
definition it does not hold.

`sound=owned` enables clip ownership in either sound format. JSON adds `sound`,
`stopSound` and `soundGain`; binary adds `0x07`, `0x08` and `0x09`. Selection
begins at zero in each message. Global `allOff` and content definitions keep
their meanings, and older clients receive the original operations. See
[audio ownership](audio-ownership.md).

`sound=resume`, which current pages request, adds envelope ages to ownership and
gain. JSON `noteResume` carries channel, note, velocity and an unsigned 32-bit
`age` in milliseconds; binary uses `0x0a`. Earlier `sound=owned` pages receive
ordinary note-on events on reconstruction instead of an unsupported opcode.
Paused owners are omitted from every output reconstruction until the guest
resumes them.

`pcm=1` enables independent ATR PCM channels. JSON wave events add a nonzero
`pcmChannel`, and `pcmControl` carries that group plus `control` (7, 11 or 10)
and `value` (0–127; omitted zero has the same meaning). Binary uses `0x14`, whose
physical-channel byte is currently mono, and `0x15`; the existing `0x11` keeps
its size and meaning. Definitions contain only sample content and can be reused
across groups and owners. Group zero is reserved for ordinary ungrouped waves,
and new controls require group 1–65535. Older pages receive ordinary waves
rendered with the current PCM gain and pan at onset. Reconnection refreshes the
capability before replay, so one parked session can move between old and new
pages, and the new page also accepts old servers.

`phase=1` enables fractional PCM resume positions, independently of PCM groups
or timestamps. JSON `playWave.framePhase` is an integer in `[0, 1000000000)`,
billionths of the first remaining sample frame; absent means zero. Binary uses
`0x16`, whose group may be zero for an ungrouped wave; a nonzero group still
requires mono. `0x11` and `0x14` keep their sizes, and zero-phase starts still use
them. Definitions stay raw samples shared across offsets. The page checks the
phase before batch dispatch and starts the buffer at `phase/(1e9*rate)` seconds
without changing its presentation timestamp. Old pages receive the same
whole-frame suffix without phase metadata, old servers ignore the query, and a
reconnect adopts the new page's capability.

`timing=1`, negotiated with `sound=resume`, preserves presentation seconds
without breaking older servers. JSON adds an optional `at` (zero is meaningful)
and a final `{kind:"clock", at:...}` frontier. A silent tick sends the frontier
alone at most every 100 ms after the last sound message (`clockOnlyInterval`),
and skips it while the send queue is full. Binary `0x0b` selects time and `0x0c`
is a frontier independent of selection. Time selection resets for each message
and after `allOff`. Finite signed event times allow overdue restored deadlines;
the frontier is nonnegative. Invalid or truncated operands reject the batch.
Earlier clients receive neither timestamps nor clock operations.

If a timed batch cannot fit the page's bounded scheduling window, or decoding
lost an operation, the page sends the epoch-bound `audioResume` command once and
ignores pending batches until `allOff` and the reconstructed output arrive. The
server clears cached definitions before replaying; guest execution never waits
for playback or socket capacity. A malformed first packet defers this request
until a valid clock confirms that the server supports timing. A new quick-load
epoch clears recovery state before acknowledging the load.

A dropped audio batch or a collector overflow requests current-output
reconstruction on the next tick with queue capacity, even without fresh events.
The reset and the reconstructed notes and remaining PCM travel together, and the
guest never waits for the socket. Reconnect and quick load also reconstruct owned
output. A reconnect does so after the resumed game's first tick: a parked game's
guest clock kept time, and a rebuild sent before a tick would anchor the page at
the pre-park clock, get the next batch refused as early and cut every voice a
second time. Normal collection is bounded to 4,096 operations and full
reconstruction to 65,536, including pitch sensitivity, both parameter selectors
and at most three controls for each of 2,048 PCM groups. PCM controls precede
remaining wave tails. The page atomically preflights PCM metadata and the group
bound before applying any events, including an untimed batch or a reset
reconstruction. Owned pages receive all channel controls before notes; for
legacy pages with flattened channels, the Host sends each note's owner controls
immediately before its note-on. Debug logs separate audio drops, collector
overflow and reconstruction. The page's
[playout timing aggregate](audio.md#playout-lateness-diagnostics) records
batch-admission deadline lateness separately and uses the existing session
statistics and report actions to flush it; it adds no wire fields or messages.

### Writes and statistics

The writer sends everything already queued in one socket write, text ahead of a
picture, and each WebSocket frame's header and payload leave together. A tick
hands its picture to the encoder and its sound to the queue at once, so the
sound arrives first; while a picture is being encoded, sound waits up to ten
milliseconds for it so that the two share a write. `bytes_per_second` counts
everything written, WebSocket framing included, and the page's run log shows it
as `net`.

The page's own files and `games.json` are answered with a content validator,
`Cache-Control: no-cache`, and gzip where the browser accepts it, so an unchanged
file costs a 304 and a changed one arrives compressed.

The [bandwidth measurements](history/session.md#frame-bandwidth-2026-09-24) cover
recorded play, the alternatives measured and rejected, and their limitations.

## Save integrity

In-game saves use the common backend store. The web Host holds a filesystem
claim for active and parked sessions and for imports. The CLI's shared `run`
command, `runktf`, `importsaves` and `provision` take the same claim, so a second
cooperating process cannot write through another live session; `runlgt` and
`runskt` do not take it.

Directory operations also take a shorter filesystem lock: a complete read,
write, batch, slot write or recovery finishes before another begins. Listing and
export remain available while a session owns its saves, and wait for an active
transaction. Separate guest writes are still separate operations. `.wfs` export
and import contain guest saves, not a CPU, JVM or session snapshot. See
[RMS](rms.md) and [running](running.md).

Where the save location cannot hold a file lock (a folder that cannot be
written, a filesystem without locks), the claim and the transaction lock exclude
callers in this process only, and the Host logs that once per directory. A start
that the claim refuses says whether another game holds the saves or the folder
itself could not be prepared. See
[the lock and its fallback](architecture.md#save-locks-and-claims).

`SaveReader` and `ReadSave` distinguish absence from I/O failure when the store
supports them, and `DirectorySaveStore` does. Three answers mean "no entry":
nothing at the key's path, a path that runs below a saved file, and a path that
is a directory. A directory is made by keys below it (`fs/saves/slot` is a file
in a directory `saves`), nothing can be stored under such a key, and reading it
answers what it answered before those keys were written and what the memory
store answers; otherwise a title that asks whether its save folder exists would
fail every run after the first. Legacy `LoadSave` remains compatible but cannot
recover errors that a legacy Host already hides.

Record decoding rejects truncation and trailing bytes. A record list keeps a
record of no bytes (length zero) apart from a deleted one (the tombstone length):
decoding returns the first as an empty record and the second as nil, so a restart
or a quick load does not read a title's empty records as deleted. The encoding is
unchanged, so saves written by earlier releases read with the meaning they were
written with. Active KTF and LGT native boundaries keep read failures and refuse
later writes; SKT RMS and XFile report guest exceptions. Authentication views
keep the error-aware contract.

KTF Java and WIPI-C files and records stage state before publishing mutations.
Content and ledger updates use the optional `SaveBatchStore`. Legacy stores keep
single-key writes and refuse multi-key operations before writing anything. The
older native package keeps its buffered write and flush policy.

Directory batches stage replacement and recovery files under the existing save
root, then commit under the store lock. A commit failure restores earlier entries
and removes newly created ones; if rollback also fails, the error names the
retained recovery files. This is not crash-atomic across multiple renames. Save
formats and canonical paths are the same in debug and release.

### Save keys on disk

A save key is the name a platform gives an entry (`rms/<store>`, `fs/<path>`,
`db/<name>`), and keys are what everything carries: the platforms, the save API
and `.wfs` packs. Only `DirectorySaveStore` turns a key into a path below the
game's save folder, and it spells out what a host file system would fold or
refuse, so a name means the same file on every host and two keys are two
files. Before, `Save` and `save` were two files on Linux and one shared file on
the case-insensitive file systems of macOS, iOS and Windows; `é` and `e` with a
combining accent were one file on macOS and iOS, which ignore Unicode
normalization; and Windows refused `slot:1` and stored `save.` as `save`.

In each segment of a key, an upper-case letter that is the upper case of its own
lower case is written `#` followed by the letter, and `#` is written `##`. What
a host would take for another character, or refuse, is written by its number,
`#+` and six lower-case hex digits:

- Any other letter outside ASCII that has case: `é` is `#+0000e9`, `ß` is
  `#+0000df`. A file system that ignores case folds such letters in more ways
  than upper to lower (APFS takes `ß` for `ss`, `ﬁ` for `fi`, the Kelvin sign
  for `k` and `ς` for `σ`).
- A character outside ASCII that Unicode normalization changes or joins to the
  character before it: a combining mark (`e` and U+0301 is `e#+000301`), a
  Hangul vowel or final consonant jamo, a kana voicing mark, a character that is
  another one's canonical spelling (the Greek question mark U+037E, the ohm sign,
  a CJK compatibility ideograph), and the seven Greek capitals with an accent
  that normalization turns into others, which therefore have no `#` form. What is
  left is its own NFC form wherever it stands, so every encoded name is its own
  NFC form, and two keys never meet on a file system that ignores
  normalization. On the APFS volume of the development Mac, each of `é` and
  `e`+U+0301, `;` and U+037E, U+F900 and U+8C48, `가` and its two jamo, `が` and
  `か`+U+3099, U+0386 and U+1FBB was one file, and each pair of their encoded
  names two. APFS normalizes by an older Unicode table than the encoder's, which
  can only make the encoder number a character APFS would have left alone.
- What Windows refuses or changes in a name: the controls below 0x20 and
  `<>:"\|?*` anywhere (`slot:1` is `slot#+00003a1`), a `.` or space at the end
  of a segment, which Windows drops (`save.` is `save#+00002e`), and the first
  letter of a device name — `con`, `prn`, `aux`, `nul`, `conin$`, `conout$`,
  `com0` to `com9`, `lpt0` to `lpt9`, and `com` or `lpt` with `¹`, `²` or `³`,
  in any case, alone or before a `.` (`con.txt` is `#+000063on.txt`; `CON` is
  `#C#O#N` already).
- A `~` before a digit, the shape of an NTFS short name: a lookup of
  `SAVEFI~1.DAT` finds `savefile.dat` there (`savefi~1.dat` is
  `savefi#+00007e1.dat`).
- A byte that is not UTF-8, as `#-` and two hex digits (an LGT C Clet's EUC-KR
  `\xbc\xbc` is `#-bc#-bc`): macOS refuses such a name, and Windows makes the
  byte U+FFFD, so two keys were one there.

Everything else is written as it is: lower-case ASCII letters, digits, the rest
of ASCII, and characters with no case that normalization leaves alone, Hangul
syllables, CJK ideographs and kana among them. `rms/Option` is stored at
`rms/#Option` and `fs/save#1` at `fs/save##1`; a key none of whose characters is
changed keeps the path it always had, which is every key of lower-case ASCII
letters, digits, Korean syllables and punctuation other than the eight above
whose segments are no device names and do not end in `.` or a space. A name
reads back as a key only when that key is written as exactly that name again,
and only as a segment a key can hold — not `.`, `..`, nor one with `\` or NUL,
each of which the encoding writes by its number — so a key has one name and a
name at most one key. Keys do not change. The owner folder is the Host's name
for a game, not a key, and is not encoded, and neither are the quick-save slots
and lock files in `.wfeature-quicksave/`. The earlier KTF native package
lowercases its file names on purpose, so its keys never differ by case.
`internal/backend/save_names.go` holds the encoding.

A key one of whose segments, written out, is longer than the host's name limit —
255 bytes on Linux and Android (ext4, f2fs), 255 UTF-16 units on macOS, iOS and
Windows (APFS, HFS+, exFAT and NTFS; on the development Mac 255 Hangul
syllables, 765 bytes, are a name and 256 are not) — or whose path is longer than
the host opens (1,023 bytes on macOS and iOS, 4,095 on Linux and Android), has
no save and cannot have one: a read or an existence check finds none, and a
write, or a batch or a restore that holds the key, is refused with an error that
names the limit, before anything is written. A platform answers that refusal as
it answers a store that failed, as a failed file or record write, and the
session goes on. Before, the file system's error reached the platform as a
failed read, and a KTF title that asked for such a name could not save again for
the rest of the session. A key is at most 512 bytes, and a segment written out
is at most eight times as long (`#+` and six digits for one ASCII character).
KTF record databases (at most 31 bytes, 248 written out) never reach the limit.
An SKT record store name (at most 32 characters) reaches it only when each of
its 32 characters is written by its number (256 bytes), and an LGT Java database
(at most 128 bytes, then `.db`) once its name written out passes 252 bytes, 32
numbered characters or 127 upper-case letters. Files on every platform (KTF C
and Java files and databases, SKT `XFile`, LGT files) are bounded only by the
key; a segment past the host's limit failed before the encoding too. Measured in
bytes on every host, a name an earlier build stored on macOS or Windows of 256
to 765 bytes could not be read, written or restored there.

The volumes macOS keeps names decomposed on — HFS+, exFAT and FAT, measured on
disk images — list every name in NFD, the composed names the store writes
included, and a folder copied from one of them onto APFS keeps the decomposed
names. An entry whose name is decomposed, and whose composed name is the same
file, is read under the composed name, which is every name the encoding
writes: read as listed, a Hangul or kana save was moved to a name with its
jamo numbered and its key found nothing, a fresh save included (HFS+ and exFAT
images: 3 of 4 fresh saves and 1 of 4 earlier ones read, and on FAT the
conversion stopped; now 4 of 4 on each). A name with a singleton such as the
Greek question mark, which decomposition replaces, is read as it is, and so is
a decomposed name where the composed one is another file (ext4, NTFS). A
decomposed key an earlier build stored on APFS reads as its composed key: the
two were one file for that build. The companion macOS writes beside a file on
exFAT and FAT, `._` and its name in the AppleDouble format, is no save: a
listing, an export and a conversion pass over it, and a restore leaves it.

On Windows a key an earlier build stored with a `.` or space at the end of a
segment is under the name Win32 made of it: every `.` and space dropped from
the end of the last segment, and a single `.` from the end of any other — so a
read of `fs/save.` or `fs./save` that finds nothing under its own names reads
`fs/save`, where it is. A key whose directory segment ends in a space or in more
than one `.` (`fs /save`, `fs../save`) was never stored: the directory was made
under the name with all of them dropped, and the path through the segment as
written reached none, so such a key has no earlier save to read. A key with
bytes that are not UTF-8 — an LGT or KTF C title's EUC-KR file name — is under
the name Go's UTF-16 made of it, as a listing reads it back: U+FFFD for each such
byte, so a read of `fs/\xbc\xbc.dat` that finds nothing under its own names
reads the file named U+FFFD U+FFFD `.dat`; and two bytes that encode a high
surrogate beside two that encode a low one as the one character the pair makes
(the EUC-KR `立고가.dat` is U+2C021 `.dat`). Such a file is also the save of every
other key Win32 spelled the same — `fs/save`, or a key with other such bytes in
the same places — which shared it there, so it is never moved, and never
written: the key's first write goes to its own name, and a restore that holds a
key whose own name is too long and that only such a file holds is refused
before it writes anything. A folder read as the earlier build left it reads
these names too, as that build did. The readings are tested with the Windows
rule forced on, on macOS; none has run on Windows.

A folder whose names are encoded holds the file `.wfeature-names#1`, written
before its first name with a `#`. No key is ever stored under that name, in any
spelling of its case, and listings, exports and imports pass over it. A key
spelled so, or as one of a conversion's temporary names
(`.wfeature-names#1-` and a number), is stored at its encoded name like any
other and is never looked up as an earlier name of the record: a read of it
through the save API answered the record as a save and moved it away, and the
folder's names read as an earlier build's from then on. It decides
the one kind of name that reads two ways: `#Top` is the key `#Top` an earlier
build wrote, or the key `Top` this build wrote. In a folder without the file
every name is its own key. The file belongs with the saves and moves with the
folder: without it, the next operation would take every name with a `#` for an
earlier build's key and convert it again.

The encoding first numbered only letters with case; that form was never
released. A folder it converted holds the same file, which says the same thing
of both forms, and its names that the encoding now writes otherwise (`#Slot:1`
for the key `Slot:1`, `con` for `con`) read as the keys they were written for
and move to their current names when they are read, as names an earlier build
wrote there do.

A folder an earlier build wrote is converted in place by the first operation on
it — a game reading its saves, a listing, an export — before anything else
happens. Every name the encoding changes moves to a temporary name and then to
its encoded name, and `.wfeature-names#1` lists them while it runs, so a
conversion stopped anywhere — the process killed between any two of its steps
— finishes on the next operation. Nothing is written over. A folder with no
name the encoding changes, which is most of them, is left byte for byte as it
is until a key that needs a `#` is stored. A folder that cannot be converted,
because it cannot be written or not all of it can be listed, is read as the
earlier build left it, each name by its exact spelling, and writing to it is
refused until it can be converted.

A name the conversion cannot move — a file locked in the Finder, or one another
program holds open on Windows — undoes it: the names that had moved go back,
the record goes, and the folder is read as the earlier build left it, each save
readable and a write refused, until the name can move. Before, the conversion
stopped at that name and every operation on the folder failed finishing it.
For a while the conversion left a name that read as its own key where it was
and converted the rest, but on a host that ignores case such a name is another
key's encoded name: `#save` stood where `Save` was to go, the second pass failed
on it and the folder could not be read again, lock or no lock; and `Save` was
read and written for the key `save`, whose first write replaced `Save`'s
progress. An undo that finds an earlier name put back while the conversion was
stopped — an earlier build that wrote the key again, a copy, a sync client —
leaves that entry, which is the later one, and the moved file goes; a moved
directory gives the one there each name it lacks, the same way below. That is
the one thing a conversion removes. A conversion that could not be carried out
is not tried again for ten seconds unless the folder changes: every try of one
a name stops renames each name twice and writes the record, about 25 ms on a
Finder-locked file against 0.13 ms for the operation itself.

A conversion that can neither finish nor be undone — a temporary name that
cannot move again, either way — is read through its record: each name it moves
is looked up where the record says it can be by then, every other name by its
exact spelling, and every read goes on. A write, an export and a checkpoint are
refused until a later operation finishes the conversion. Before, every
operation on the folder failed, and a title that took the failed read for no
save could start over and then, once the folder converted, write over the
progress it had not been shown.

An earlier name whose encoded name would be longer than a file system stores
stays as it is, still reads as its own key and is written there.

An earlier build can write into a converted folder too, when a player goes back
to it: it does not know `#Save` and writes its key `Save` under that name. When
both are there, the one written later is the save. A read moves a later earlier
name over the encoded file it continued from, and a listing or an export
chooses it the same way; an earlier name older than the encoded file is left
where it is and not read. Of two earlier names of one key, the later one
counts. A folder's names record is read once and then known by its identity,
and a listing made to tell `Save` from `save` is kept until its folder changes.

On a host that ignores case, an earlier build kept one file for `Save` and
`save`, holding whichever was written last. The file belongs to the key its
spelling on disk matches, and the other key has no save until one is written:
that is the only deterministic answer, and the data cannot be split.

`.wfs` packs carry keys, not paths. A pack exported by an earlier build imports
unchanged, and the export of a converted folder is byte for byte the export an
earlier build made of the same saves. The one exception is a pack an earlier
build made of a folder this build had converted: that build took every name for
its own key, so the pack carries the folder's names (`rms/#Save`) and its
`.wfeature-names#1`. The import reads those names as the folder's are read —
`rms/#Save` is `rms/Save`, the record is no entry, and of two names of one key
the encoded one is restored — rather than restoring `rms/#Save` and removing the
`rms/Save` the title reads. Such a pack holding nothing but the record is
refused as an empty one is: restored, it wrote nothing and removed every save.

A name an earlier build writes into a converted folder afterwards — an earlier
version run again, or files copied in by hand — reads as its own key when the
encoding would not have written it, and moves to its encoded name when it is
read, unless the encoded file is there and newer. A name like `#Top` written that
way reads as encoded. Until it moves, such a leftover, or a name of the first
form, can also answer, on a host that ignores case or normalization, for the
name it folds to. An earlier version run on a converted folder does not find the
keys that have a character the encoding changes; their files stay under their
encoded names.

## Input, compatibility, and limits

The Host owns held-key repeat timing and releases held input when control is
lost. Pointer support is reported per game in `can_touch`: a KTF WIPI Java title
whose classes declare a pointer callback takes it, while SKT, LGT and the
earlier KTF native package do not. Keypad layouts and sizing are browser
preferences; supported IME composition stays local until a complete text commit.

Recognized offline authentication is selected automatically for new sessions.
The page has no authentication switch, and an `authentication` field from an
older page is ignored. `Options.DisableAuthentication` is a local diagnostic
opt-out. Selection is not proof of successful gameplay. See
[authentication](authentication.md). Server access control and deployment are in
[running](running.md).

[Testing](testing.md) separates automated protocol and encoder tests from local
Chromium/WebKit observations and human checks. PWA installation and update,
actual phone suspension, audible recovery, touch, and complete save restoration
still need the corresponding acceptance coverage.
