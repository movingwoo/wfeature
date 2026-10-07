# Server sessions

The server executes games through `internal/session`; the browser sends input
and displays frames and audio. `internal/webhost` owns HTTP/WebSocket transport
and the encoder. Both server and CLI call the same platform composition.
[Session history](history/session.md) retains protocol rationale, previous
measurements, and investigations.

## Transport

The page opens `/api/session?protocol=2`. Text messages carry JSON commands and
results in both directions. In protocol 2, binary messages carry pictures and
sound: a picture at the guest's own size, as an update to the one the page
holds, which the page magnifies; and sound as compact binary that carries each
sample once. Without that exact query value the server speaks protocol 1:
complete PNGs magnified on the server, and sound as JSON. Older pages remain
compatible, and the current page also reads protocol 1 from an older server
that ignores the query.

A protocol 2 page whose browser decodes lossless WebP adds `pictures=webp`, and
its pictures come as lossless WebP instead of PNG. The page finds out once, by
decoding a two-pixel picture with transparency before its first connection;
every browser that can hold a session should, and one that cannot, or that
answers late, keeps PNG. A server that does not know the value sends PNG, which
the page reads as well.

| Page message | Purpose |
| --- | --- |
| `start` | Start an archive with supported session options. |
| `resume` | Find the browser token's retained game; explicit takeover transfers control. |
| `park` / `stop` | Retain a game without control, or end it. |
| `key` / `pointer` | Deliver supported input in guest coordinates. |
| `speed` / `scale` | Change execution rate or presentation scaling. |
| `text` | Open, commit, or cancel a supported native text edit. |
| `quickSave` / `quickLoad` | Save the running KTF or LGT game's execution state to the archive's slot, or bring the game back to it. Ordinary saves are not reverted. |
| `cheat` / `report` | Operate diagnostics or write a session report. |
| `ping` | Check connection liveness independently of guest execution. |

Server JSON includes readiness, start descriptions, command results, audio,
and diagnostics. Native text editing uses edit identifiers and stale-target
checks; see [native text input](native-text-input.md). Detailed payloads and
compatibility behavior remain in [the transport record](history/session.md).

## Retention and control

The server retains up to four active and parked games together. A game can
outlive its controlling socket without an elapsed-time eviction limit.
Returning with the same browser token resumes it; moving control from another
connected page requires explicit takeover. The old page does not automatically
reclaim control.

Capacity and shared-save replacement use explicit confirmation. Active games
cannot be evicted to make room. The four-game limit is not a resident-memory
budget or a limit on every socket and archive-inspection request.

Parking releases held input, detaches presentation, and drives the platform's
pause boundary. Resuming reattaches presentation and drives resume. These
callbacks do not establish that all guest threads and clocks freeze.

Stopping the server ends every game it holds. A parked game is closed where it
waits. A game whose page is still attached is asked to close: its runner
finishes the round it is in and closes the session on its own goroutine, the
way a page's `stop` does, and the server waits for that before it exits. The
HTTP server's own shutdown does not reach a session socket, so without this a
connected game would end with the process, and whatever its title had written
and not yet handed to the save store — a file it left open, keys a native
package keeps until a frame ends — would be lost. The wait shares the
shutdown's ten-second deadline; a runner that has not let go by then is named
in the log. Once a stop has begun, nothing starts, resumes or parks. Ordinary
saves and quick save slots survive; a later page can go back to the slot's
moment after starting the game.

<a id="ktf-checkpoints"></a>

## Checkpoints

The shared session supports KTF Java/AOT, older descriptor modules and native
packages, and LGT Clets and AOT Java titles. SKT sessions report
`can_checkpoint: false`. The `started` description reports `can_checkpoint`, `has_checkpoint`,
`restored` and `speed`. A `start` request with `quick_load: true` constructs from
the archive's slot without running guest startup. The ordinary `resume` command
still means reconnecting to a retained in-memory session.

The page offers quick save/load as optional keypad assignments, with no default
placement. Clicking an assigned key sends its request immediately and displays
brief inline feedback without a confirmation or result popup. All copies are
disabled while a request is pending; editing a cell never sends a request.
The picker offers ordinary startup only. To restore after a server restart,
start the game and press its assigned quick-load key. Explicit startup restoration
remains available to protocol and CLI callers.

A checkpoint is execution state only, and the game's own saves come first:
neither command replaces, reverts or removes an ordinary save. See
[Quick load and ordinary saves](architecture.md#quick-load-and-ordinary-saves).

`quickSave` runs between complete rounds. It first stores any write the game
has issued and the host still holds (an open file's buffer on LGT, a written
name not yet stored in a native KTF package), then captures, then writes one
slot per exact archive. The slot holds no save.

`quickLoad` reads the archive and the slot, validates the record into a
detached runtime that has no access to the saves, fills what that runtime has
open from the saves as they are with reads only, stores the running game's
issued writes as `quickSave` does, and then replaces the running game. The
restored game reads the saves on disk and writes on top of them. With nothing
pending a load writes nothing.

Either command is refused, with the running game left running and no save
reverted or removed, when the archive or the slot is invalid, the runtime is in
a state a record cannot describe, the store refuses the game's pending writes,
or a save the load needs cannot be read. Three refusals reach the page as a
sentence in its own language, with the cause in the server log: a slot written
in an earlier format (which is left in place), pending writes that could not be
stored, and saves that could not be read. "Could not be stored" covers a store
that refused the write and, on LGT, a buffer the host declined to store because
its file had changed behind it; "could not be read" covers a read that failed
and saves larger than a load reads. Every other refusal is passed through as
the error's own text.

A `start` with `quick_load: true` reads and checks the slot before the game the
connection is running is stopped, so a start that is certain to be refused — no
slot, an earlier format, a damaged slot, another archive's — leaves that game
running. `has_checkpoint` is true for an earlier-format slot as well, so that
the load is offered and its refusal can say why.

Slots are outside ordinary `.wfs` exports, and a `.wfs` import is unchanged: it
writes the saves it carries over the ones on disk.

A successful load sends `restored` with a new connection `epoch` and a `started`
description before new output. Mutating input, text, cheat, speed, scale and
checkpoint requests carry that epoch; omitted means zero. Requests from a prior
epoch are refused. Both output queues and the frame encoder discard work from
the prior epoch, and the page closes its old decoder and clears sound, vibration,
input, text and cheat state before the fresh complete frame. Binary messages keep
their existing format; the ordered reset and queue ownership establish the boundary.
The Host releases saved physical input and resumes a paused slot through the
normal lifecycle. A CLI caller retains saved input and pause until it changes them.

On LGT a key is queued and delivered by the next tick, so the release the Host
sends for a restored hold reaches the title one tick after the load rather than
inside it. That platform has no repeat event and no pointer, and its pad rule is
the platform's own, so a checkpoint carries the Host's held keys and the pad's
state and nothing else of the three.

Loaded notes resume with their saved channel settings and PCM tails with their
remaining samples. Oscillator phase, release tails and physical output latency
are not serialized; resumed notes restart their envelopes. This is not a claim of
sample-exact audio continuity. Runtime state compatibility requires the same
archive and a compatible checkpoint schema; debug and release share the schema.

## Presentation and audio

Ordinary ticks use `Session.FrameUpdate`, which transfers owned, unscaled pixels
and a presentation scale. Encoding runs on the encoder goroutine behind a
bounded one-frame queue, and so does protocol 1's server-side hqx; a discarded
picture therefore costs no encoding work on the emulator goroutine. The
synchronous `Session.Frame` API still returns already-scaled pixels for other
Hosts; MIDP surfaces remain at their native size and are never magnified. The
page decodes pictures and draws them — in protocol 2 it also magnifies them — and
does not execute guest instructions. Speed and scaling are distinct settings.

The encoder compares consecutive raw pictures, dimensions and scale before
scaling or encoding. An unchanged picture needs no new picture or network message;
guest callbacks, timers and drawing still execute. A static screen may therefore
report zero delivered frames per second while guest ticks continue normally.
Start, resume and display-setting changes force presentation even when pixels
match. That request survives a full queue until accepted, and its queued lifecycle
answer is written before the forced picture.

KTF additionally offers explicit LCD flushes through `backend.FrameSink` and
`session.Options.FrameUpdates`, including while startup or a long callback holds
the execution lock. Each `FrameUpdate` owns its pixels. Sampling occurs at most
once per 1/60 wall-clock second, skips unchanged samples, and never waits for
the consumer. Hosts without a sink keep the pull-only path. No timer invents a
flush that guest code did not request.

Parking detaches the sink before its queue closes; resume installs the new
queue before guest execution resumes. Intermediate frames use the same negotiated
picture format. [The CPU investigation](cpu-saturation-investigation-2026-09-23.md)
records component benchmarks, authored-fixture Chromium/WebKit checks and a
bounded local KTF browser run. These do not establish all-game performance or
physical-phone acceptance.

Audio uses shared backend timelines. The page's synthesizer consumes audio
messages, subject to browser audio activation. Borrowed PCM/SysEx slices must
be copied before asynchronous use. See [audio](audio.md) and
[shared service contracts](architecture.md#shared-runtime-services).

### Protocol 2 pictures

The encoder compares each picture, at the guest's own size, with the one the
page holds once the previous message is accepted by the writer queue. All
encoding is lossless at the game's frame rate. Every picture message starts
with a sixteen-byte big-endian header:

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
- **Masked** draws its rectangle over the held picture. A pixel equal to the
  one held is written transparent and keeps it; every other pixel in it is
  opaque. It is every other update. Runs of identical transparent pixels are
  what compresses, so a change spread across the screen costs little more
  than the pixels that changed.
- **Replace** replaces its rectangle, transparency included. It is used only
  when a changed pixel is not opaque, which a masked update cannot express.
- **Shift.** When at least a thirty-second of the pixels changed and the held
  picture is opaque, the encoder looks for the held picture moved by up to 16
  pixels along either axis, trying the last shift it sent first and keeping it
  when it still predicts nearly every sampled pixel. If the prediction leaves
  at least a fifth fewer changed pixels, a masked update names the shift: the
  page first draws its held picture at that offset over itself, and the strip
  it uncovers keeps what it had. Scrolling fields change almost every pixel a
  frame and move by one or two. A shift that already produced every pixel
  carries no picture.
- A rectangle with at most 256 distinct values, the transparent one included,
  is written with a palette. PNGs use zlib's default level.
- **WebP.** `internal/vp8l` writes the rectangle as lossless WebP, written for
  these pictures from RFC 9649 rather than taken from a library. A rectangle
  with at most 256 colours goes through a colour table, packing up to eight
  pixels into one where the colours are few; any other has green subtracted
  from red and blue. Both are then backward references — copies from the left,
  from the rows above through the format's short neighbourhood codes, and from
  anywhere earlier through hash chains over runs of two and of six pixels —
  chosen for the bits they save, and a colour cache sized by estimate. The
  predictor transform is tried only for a picture still above six bits a pixel,
  such as a photograph: the games' flat areas and repeated tiles copy far
  better than they predict. A transparent pixel's colour is chosen to cost
  least, because nothing on the page can show it. On recorded play this is a
  fifth to a half fewer bytes than the PNGs, for 0.8 to 1.6 times their
  encoding time; see
  [the record](history/session.md#frame-bandwidth-webp-2026-09-25).

Each update depends on all prior binary messages on that connection. Raw frames
can be dropped before encoding; encoded updates must remain ordered and cannot
be dropped independently.

`web/frame-stream.js` decodes messages serially and composes them on a retained
canvas at the guest's size, reporting the scale and the changed rectangle. The
display coalesces draws of that canvas without losing updates, and
`web/magnify.js` applies hqx at the scale the server named, redoing only the
magnified blocks the changed rectangles can reach; see [hqx](hqx.md). Image
dimensions are bounded at 4096 per axis. At most 32 waiting messages and 96 MiB
of queued/in-flight data are accepted. A malformed message, a decode failure or
excessive backlog closes the connection; existing session recovery resumes the
retained game with a complete picture. The server has no hqx work for these
connections. A bare PNG from a protocol 1 server is read as a complete picture
already magnified.

### Protocol 2 sound

A sound message is binary: ASCII `WFA2` and the tick's calls in order, each an
operation byte and its operands. `internal/webhost/audio_stream.go` lays the
format out. A sampled sound or SysEx message travels once as a definition under
an id and is named afterwards; ids are never reused. The definitions a page
holds are bounded at 8 MiB, past which the server tells it to forget them and
starts over. A sound message that is shed because the connection is behind
leaves the page's definitions unknown, so the next one starts over the same way.
The page skips a sound whose definition it does not hold.

### Writes and statistics

The writer sends everything already queued in one socket write, text ahead of a
picture, and each WebSocket frame's header and payload leave together. A tick
hands its picture to the encoder and its sound to the queue at once, and the
sound arrives first; while a picture is being encoded, sound waits up to ten
milliseconds for it so the two share a write. Statistics include
`bytes_per_second`, everything written with its WebSocket framing, which the
page's run log shows as `net`.

The page's own files and `games.json` are answered with a content validator,
`Cache-Control: no-cache` and gzip where the browser accepts it, so an
unchanged file costs a 304 and a changed one arrives compressed.

See the [bandwidth measurements](history/session.md#frame-bandwidth-2026-09-24)
for recorded play, alternatives measured and rejected, and limitations.

## Save integrity

In-game saves use the common backend store. The web Host holds a filesystem
claim for active and parked sessions and for imports. The CLI's shared `run`
command, `runktf`, KTF save imports and provisioning take the same claim, so a
second cooperating process cannot write through another live session. Other
legacy platform commands do not yet take this lifetime claim.

Directory operations also take a shorter filesystem lock: a complete read,
write, batch, slot write or recovery finishes before another begins.
Listing and export remain available while a session owns its saves, and wait
for an active transaction. Separate guest writes are still separate operations.
`.wfs` export/import contains guest saves, not a CPU/JVM/session snapshot. See
[RMS](rms.md) and [running](running.md).

Where the save location cannot hold a file lock (a folder that cannot be
written, a filesystem without locks), the claim and the transaction lock
exclude callers in this process only, and the Host logs that once per
directory. A start that the claim refuses says whether another game holds the
saves or the folder itself could not be prepared. See
[the lock and its fallback](architecture.md).

`SaveReader` and `ReadSave` distinguish absence from I/O failure when the store
supports them. `DirectorySaveStore` does. Legacy `LoadSave` remains compatible
but cannot recover errors that a legacy Host already hides. Record decoding
rejects truncation and trailing bytes. Active KTF/LGT native boundaries retain
read failures and refuse later writes; SKT RMS/XFile report guest exceptions.
Authentication views preserve the error-aware contract.

KTF Java and WIPI-C files and records stage state before publishing mutations.
Content and ledger updates use optional `SaveBatchStore`. Legacy stores retain
single-key writes and refuse multi-key operations before writing anything.
The older native package retains its previous buffered write/flush policy.

Directory batches stage replacement and recovery files under the existing save
root, then commit under the store lock. Commit failure restores earlier entries
and removes newly created ones. If rollback also fails, the error identifies
retained recovery files. This is not crash-atomic across multiple renames.
Save formats and canonical paths are unchanged across debug and release.

## Input, compatibility, and limits

The Host owns held-key repeat timing and releases held input when control is
lost. Pointer support is advertised per platform; KTF WIPI Java supports it,
while the shared SKT, LGT Clet, and earlier KTF native paths do not.
Keypad layouts and sizing remain browser preferences; supported IME composition
stays local until a complete text commit.

Recognized offline authentication is selected automatically for new sessions.
The page has no authentication switch; an obsolete incoming switch is ignored.
`Options.DisableAuthentication` is a local diagnostic opt-out. Selection is not
proof of successful gameplay. See [authentication](authentication.md).
Server access control and deployment are described in [running](running.md).

[Testing](testing.md) separates automated protocol/encoder tests from local
Chromium/WebKit observations and human checks. PWA installation/update, actual
phone suspension, audible recovery, touch, and complete save restoration still
need the corresponding acceptance coverage.
