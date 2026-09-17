# Server sessions

The server executes games through `internal/session`; the browser sends input
and displays frames and audio. `internal/webhost` owns HTTP/WebSocket transport
and the encoder. Both server and CLI call the same platform composition.
[Session history](history/session.md) retains protocol rationale, previous
measurements, and investigations.

## Transport

The page opens `/api/session`. Text messages carry JSON commands and results;
binary messages carry ordinary PNG frames with dimensions in the image itself.

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

Ordinary ticks use the platform frame path. Encoding runs separately with a
bounded one-frame queue, allowing stale frames to be dropped under backpressure.
The page decodes PNGs and draws them; it does not execute guest instructions.
Speed and scaling are distinct settings.

KTF additionally offers explicit LCD flushes through `backend.FrameSink` and
`session.Options.FrameUpdates`, including while startup or a long callback holds
the execution lock. Each `FrameUpdate` owns its pixels. Sampling occurs at most
once per 1/60 wall-clock second, skips unchanged samples, and never waits for
the consumer. Scaling runs on the encoder goroutine. Hosts without a sink keep
the pull-only path. No timer invents a flush that guest code did not request.

Parking detaches the sink before its queue closes; resume installs the new
queue before guest execution resumes. Intermediate frames use the same PNG wire
format. Synthetic tests cover these boundaries; they are not real-game timing
or physical-browser acceptance measurements.

Audio uses shared backend timelines. The page's synthesizer consumes audio
messages, subject to browser audio activation. Borrowed PCM/SysEx slices must
be copied before asynchronous use. See [audio](audio.md) and
[shared service contracts](architecture.md#shared-runtime-services).

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
