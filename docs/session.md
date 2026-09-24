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

| Page message | Purpose |
| --- | --- |
| `start` | Start an archive with supported session options. |
| `resume` | Find the browser token's retained game; explicit takeover transfers control. |
| `park` / `stop` | Retain a game without control, or end it. |
| `key` / `pointer` | Deliver supported input in guest coordinates. |
| `speed` / `scale` | Change execution rate or presentation scaling. |
| `text` | Open, commit, or cancel a supported native text edit. |
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
callbacks do not establish that all guest threads and clocks freeze. Stopping
the server ends retained games: there is no serialized emulator-state restore.
Quick save/load work remains paused. Guest-written saves survive normally.

## Presentation and audio

Ordinary ticks use `Session.FrameUpdate`, which transfers owned, unscaled pixels
and a presentation scale. Encoding runs on the encoder goroutine behind a
bounded one-frame queue, and so does protocol 1's server-side hqx; a discarded
picture therefore costs no encoding work on the emulator goroutine. The
synchronous `Session.Frame` API still returns already-scaled pixels for other
Hosts; MIDP surfaces remain at their native size and are never magnified. The
page decodes PNGs and draws them — in protocol 2 it also magnifies them — and
does not execute guest instructions. Speed and scaling are distinct settings.

The encoder compares consecutive raw pictures, dimensions and scale before
scaling or encoding. An unchanged picture needs no new PNG or network message;
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
| 16 | — | PNG; its dimensions are the rectangle's |

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
  carries no PNG.
- A rectangle with at most 256 distinct values, the transparent one included,
  is written with a palette. PNGs use zlib's default level.

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

In-game saves use the common backend store. The web Host coordinates active
save ownership and import/export within its process. A separate CLI process
is outside that claim registry. `.wfs` export/import contains guest saves,
not a CPU/JVM/session snapshot. See [RMS](rms.md) and [running](running.md).

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
