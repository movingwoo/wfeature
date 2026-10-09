# Independent audio playback and recovery

The 2026-10-08 repair addresses the shared stop/ownership findings in the
[sound audit](history/audio.md#cross-platform-sound-audit). Two clips on the same MIDI channel, including
the same pitch, now remain independent. Explicit stop, close and restart cancel
only that clip's MIDI voices, PCM, percussion and release tails. Natural score
completion releases its MIDI voices without truncating PCM that extends past
the score's last event. Applying SMAF wave gates remains separate work.

## Runtime and Host contract

`backend.Audio` assigns each loaded sound a nonzero, monotonically increasing
`AudioHandle`. Handles never wrap into another sound. Hosts can additionally
implement `backend.OwnedAudioSink` to receive `AudioEvent(handle, event)` and
`StopSound(handle)`. These callbacks share the existing serialization and
borrowed-buffer contract of `AudioSink`. The web Host implements both interfaces.
Each owner has sixteen independent MIDI channel states. The audible 24-voice
budget remains global. Paused owners retain bounded, inaudible voice records
separately, as described below.

The CLI continues receiving the same flattened MIDI/PCM diagnostic stream.
Its concatenated WAV does not represent simultaneous mixing, PCM cancellation
or final browser output. Use the browser renderer for those comparisons.

Output checkpoints carry owner identity for voices and remaining PCM, and a
bounded, sorted channel table for each owner. Validation rejects missing,
duplicate and unloaded owners before emitting anything. Audio component version
6 stores raw output separately from device and clip gain, plus paused cursors,
note envelope ages, finite remaining passes, completion counts and media
position, pitch sensitivity and RPN/NRPN selectors. Pedal-held notes retain
their released-key flag independently of sounding notes whose keys remain down.
Earlier records cannot reconstruct all of this state.
Incompatible checkpoints are refused and left on disk;
ordinary saves and the shared debug/release data formats remain unchanged.

PCM now shares [one admission and reconstruction budget](audio.md#pcm-admission-and-restoration).
A newest wave beyond 256 references/32 MiB never reaches output, so excess PCM
cannot later disappear during recovery or block a checkpoint. Stopping a clip
releases only its admitted references. Version 9 preserves original byte charges
and fractional sample-frame positions when samples are trimmed; strict v6/v7/v8
readers remain available at their saved whole frames. An optional wave-resume
sink carries the fraction independently of the replay timestamp.

## Guest volume

The web Host additionally implements `backend.AudioGainSink`. Its
`SoundGain(handle, value)` callback updates a clip's held notes, percussion,
release tails and PCM. Raw velocities and samples remain unchanged. A value of
10,000 is unity; the effective gain is the device percentage multiplied by the
clip percentage, divided by 10,000 in the renderer. This multiplication is the
emulator's policy: the WIPI specification defines the individual 0..100 scales
but does not prescribe their combined transfer function. A separate backend
mute preserves the clip's stored level.

Gain sits after the clip's envelopes and before the user's MIDI/effect sliders.
Changing or zeroing it does not stop, restart or seek sources. Unmuting therefore
preserves the current PCM frame and melodic phase. Natural-end PCM tails still
receive gain updates. Checkpoint validation checks effective gain against the
saved device/clip levels, and reconstruction sends gain before source events.
Late sink attachment also supplies current gains before future playback.

Connected guest controls are KTF Java Clip and Volume, KTF C device volume,
LGT Java/C clip and device volume, SKVM device volume and SKT WIPI Clip volume.
SKT MIDP `VolumeControl` uses the same per-clip level and mute state.
They clamp to 0..100 and read back their stored setting. SKT accepts WIPI's
boolean `Clip.setVolume` signature and retains the existing void extension.
KTF and LGT use device default 100; SKT Java applies the existing SKVM reported
default of 50 to actual output. Clips default to 100. Ordinary reload and restart
retain the clip level. LGT Java data replacement also closes the previous
decoded handle; merely clearing its loaded flag left active sound orphaned.

Legacy diagnostic sinks and older pages scale future velocities/samples and
release held MIDI notes at zero. They cannot adjust active PCM or resume held
notes after mute. The new page applies the complete gain contract. Vendor
numeric source mappings (including source mute/default-volume extensions)
remain unverified and are not treated as device-wide controls. MIDP control
lookup, notifications and their remaining delivery limitation are described in
[the audio contract](audio.md#midp-per-player-volume-controls).

Contracts: [WIPI C media](https://mirusu400.github.io/wipi-wiki/c-api/media.md),
[WIPI Clip](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Clip.md)
and [WIPI Volume](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Volume.md).

## Protocol negotiation

New pages add `sound=resume` to the WebSocket query. It enables clip ownership,
gain and note envelope ages with either JSON or binary protocol 2. The earlier
`sound=owned` capability enables ownership and gain, but receives ordinary
note-on events when output is reconstructed. Without either, the server sends
the legacy event format, preserving compatibility with an older cached page.
An older server can ignore the query and still drive the new page's owner-zero
stream. The PWA shell version is 47.

JSON events add `sound`, a 32-bit handle, and `stopSound`. Omitted `sound` is
owner zero. Binary `WFA2` adds:

| Opcode | Operands | Meaning |
| --- | --- | --- |
| `0x07` | Big-endian uint32 | Select the sound for subsequent operations. |
| `0x08` | None | Stop the selected sound, including PCM and tails. |
| `0x09` | Big-endian uint16 | Set selected sound gain, from 0 to 10,000. |
| `0x0a` | Channel, note, velocity bytes; big-endian uint32 age | Resume the selected owner's note at its elapsed envelope age in milliseconds. Requires `sound=resume`. |

Selection starts at zero for every message. A dropped packet therefore cannot
redirect subsequent events to another clip. `allOff` remains global. PCM and
SysEx definitions remain shared by content across owners; stopping a clip does
not discard a reusable sample definition.
The corresponding JSON operation is `soundGain`, with `sound` and `value`.
Note reconstruction uses `noteResume`, with `sound`, `channel`, `note`,
`velocity` and `age`. Age is clamped to unsigned 32-bit milliseconds; a missing
age means zero. An ordinary `noteOn` always starts a new attack.

## Clip pause and resume

`Audio.Pause(handle, guestTime)` advances to the requested guest position,
cancels that owner's Host sources and freezes its score cursor, repeat mode,
MIDI envelope ages and PCM frames. `Audio.Resume` excludes the paused interval
from both clocks, reconstructs the held output and continues the remaining
note gates. It preserves one-shot/repeating mode. `SetRepeat` can change the
next score-end action without resetting the cursor. Repeated backend pause or
resume calls are idempotent; explicit Stop, Close or Play discards the cursor.

KTF Java, LGT Java/C, SKT WIPI Java and SKVM use this path. LGT C resume reads
only its clip argument, preserves repeat and retains PAUSED/RESUMED callbacks.
WIPI pause/resume reject invalid transitions. MIDP stop/start retains position;
deallocate retains it too; setting media time to zero rewinds the current pass
without replenishing its remaining loop budget. WIPI
Stop remains cancellation, and WIPI Play starts a new pass. SKVM pause still
interrupts a blocking play/loop with `UserStopException`; resume is nonblocking
and creates no replacement wait. KTF C pause/resume slot bindings remain
unverified. MIDP finite counts, media time, natural-end state and deferred
notifications follow the [lifecycle contract](audio.md#midp-playback-lifecycle).

The optional `AudioResumeSink` callback carries note age. The page reconstructs
only the remaining attack/decay ramps, or begins at sustain. Percussion uses
its saved noise offset and remaining tail. Melodic oscillator phase and release
tails are not retained. Legacy sinks/pages receive a fresh note-on and cannot
provide the full continuation contract; sinks without ownership cannot cancel
PCM individually.

Only 24 notes can sound together. Each paused owner can retain at most 24
additional notes within the 256-loaded-sound limit. Paused records do not steal
audible slots. Resuming appends voices in the same order as the page and applies
the audible limit again. PCM shares the 256-wave/32 MiB admission budget across
audible and paused owners. Paused waves keep their charges; a new wave that
cannot fit is consumed without emission. Pause can therefore retain all admitted
PCM and resume it without exceeding the budget.
Transient tones cannot be paused. Invalid or backward pause clocks are refused.

Checkpoints preserve frozen output even after a long pause or a different Host
clock epoch. Reconnect, queue recovery and quick load skip paused owners until
their explicit resume. Validators check matching timeline/output pause flags,
guest clock bounds, drum age/tail agreement and active/retained voice budgets.
Catch-up limits on all three platforms use the frozen guest position.

Contracts: [WIPI Player](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Player.md),
[WIPI C media](https://mirusu400.github.io/wipi-wiki/c-api/media.md) and
[MIDP Player](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/Player.html).

## Queue pressure and reconnect

Ordinary audio, including stop operations, never waits for a full socket queue.
A dropped batch marks output as needing reconstruction. The next tick with
queue capacity sends `allOff` followed by the current backend output, even if
that tick produced no new audio event. Lost PCM definitions are sent again.

Collector overflow follows the same path. Normal batches remain bounded to
4,096 operations. Reconstruction temporarily permits 65,536 operations, enough
for the 58,176-operation maximum owned reconstruction: all 256 owners plus
owner zero, fourteen operations per channel, gains, voices and tracked samples. It then
restores the ordinary bound. Debug logs distinguish collector overflow, dropped
audio batches and successful reconstruction. This prevents a missed note-off
or stop from leaving a voice stuck once delivery resumes. It does not reproduce
events missed while delivery was blocked.

A new connection reconstructs the parked session's output after lifecycle
resume. Quick load uses the same bounded collection with its existing epoch
reset. PCM reconstruction trims the already elapsed sample frames and skips
expired buffers; repeated reconstruction cannot restart their original prefix.
Capable pages resume note envelopes at their elapsed ages. Physical phase,
release tails and output latency are not saved. Active PCM age follows the
unscaled Host clock, including time spent parked.

## Verification

The original two regressions failed before the repair: stopping one clip
removed another clip's note, and stopping an already completed score retained
its long PCM buffer. Maintained tests now cover:

- Independent equal-pitch voices, program/controller/bend state, natural end,
  explicit stop/close/restart, global voice limits and whole-session reset.
- Checkpoint round trips at a different clock epoch, owned PCM tails, malformed
  owner records, handle exhaustion and per-owner PCM accounting beyond the limit.
- Real WebSocket capability negotiation for both formats and legacy clients,
  binary operation truncation, shared PCM definitions and message-local owners.
- Nonblocking cancellation under queue pressure, recovery on an otherwise empty
  tick, collector overflow and the largest supported reconstruction.
- An authored SGS session through dropped note/stop recovery, live quick load,
  park/resume and both protocol formats. The tests use the ordinary session and
  Host paths, rather than injecting events into a running guest.

`web/acceptance/audio-ownership.mjs` renders the actual graph in Chromium and
WebKit. Two simultaneous equal-pitch MIDI clips and two PCM clips are separated
in stereo; after stopping one, both rendered channels match the surviving clip
rendered alone within RMS tolerance 0.000001. Surviving-channel RMS is 0.076715
for MIDI and 0.113137 for PCM in both engines. The existing twelve-short-note
90 ms-clock acceptance route also passes in both engines.

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
  node web/acceptance/audio-ownership.mjs http://127.0.0.1:11541 chromium
```

Use `webkit` for the other engine. These authored renders prove cancellation
and isolation, not handset timbre or physical-phone listening quality.

Gain regressions cover device/clip multiplication, active mute and unmute,
preserved raw data, malformed levels, legacy fallback, late sink attachment and
gain before replay. KTF VM calls and LGT ARM Java/C dispatch exercise the real
adapters. The authored SKT `audio-gain.jar` runs both Java APIs and round-trips
their state through a full checkpoint. Concurrent SKT level setters preserve
agreement between guest getters and output.

The authored `media-volume.jar` tests Java interface calls, stable control
identity, fresh control arrays, clamping, mute/restore and change notifications
while two players each emit MIDI and PCM. It restores a muted player and its
control/listener graph, verifies reentrant callbacks and retains closed-player
controls safely. Five malformed graph variants are refused. Concurrent native
setters and getters pass under the race detector; clip levels have a single
backend record.

`web/acceptance/audio-gain.mjs` passes in Chromium and WebKit: held MIDI and
nonperiodic PCM follow zero, quarter and full gain, with each output sample
matching an uninterrupted reference within 0.000001. The other owner's output
is unchanged. A separate regression resets the output clock with an older gain
still scheduled: the latest mute cancels that future automation, preventing an
unintended unmute.

Pause regressions cover encoded checkpoint restoration, frozen note/drum ages
and stereo PCM suffixes, owner isolation, active/retained voice limits and
matching eviction order. The later admission repair replaces omitted-PCM refusal
with bounded audible output. They still refuse transient pauses, malformed
records and invalid clocks without cancelling valid output. KTF VM and LGT ARM
Java/C calls preserve remaining note gates and repeat; C tests vary the unused
register and check state callbacks. SKT tests run existing authored JARs through
MIDP paused checkpoint/start, deallocate, rewind, changed repeat settings, WIPI
pause/resume versus new Play, and SKVM interruption without another wait.
Reentrant deallocate listeners can restart or close without splitting guest
state from the backend. All three platforms bound checkpoint catch-up using
the frozen pause clock.

`web/audio-resume.test.mjs` covers attack/decay/sustain continuation, percussion
offset/expiry and release after clock correction. The actual graph in
`web/acceptance/audio-resume.mjs` passes in Chromium and WebKit: paused-owner RMS,
resumed-reference sample error and peer sample error are zero. MIDI resumes at
291 ms with onset RMS 0.0782802, PCM resumes at frame 13,984 with RMS 0.0924937,
and percussion resumes at 91 ms with RMS 0.000726991 and no extended tail after
separating channel gain from the velocity envelope.
The independent references retain a fresh melodic oscillator phase; these
measurements establish envelope and sample-position behavior, not phase continuity.
Gain, ownership and twelve-note coarse-clock render checks still pass in both
engines: eight browser runs in total.

The complete `make test`, `make test-debug`, `go test -race ./internal/...`
and `go vet ./...` gates pass. Both server profiles also build with
`CGO_ENABLED=0`.

Six local archive scenes (two KTF, one LGT and three SKT) each ran for twelve
seconds with three confirm inputs and produced owned MIDI or PCM without
start, tick or input errors. Five also restored a checkpoint and continued.
The remaining SKT WIPI scene refuses its checkpoint because its default
display has no MIDP owner, while the existing platform validator requires
one. The same refusal reproduces with the original audio implementation;
it is a pre-existing display-state defect, not an audio ownership regression.
The fixture test previously retained an owner from an initialized MIDP
display and did not cover this factory path. This scene does not count as a
successful checkpoint acceptance. No real SGS scene or physical phone was
exercised by this smoke check. Local anonymous probe results remain under
`build/sound-ownership/`.

The six scenes were repeated after the pause repair, retaining the same five
successful restores and one known Display refusal, with no start/tick/input
errors. The anonymous result is `runtime-pause-final.jsonl` in that directory.
This is a playback regression smoke check, not evidence that those scenes
exercised every platform pause call.

The six scenes were repeated after the MIDP lifecycle repair, again retaining
five restores and the same Display refusal without execution/input errors.
The result is `runtime-lifecycle-final.jsonl`. The final lifecycle gates pass
`make test` (313 Node tests), `make test-debug`, `go test -race ./internal/...`,
`go vet ./...` and both `CGO_ENABLED=0` server profiles. Logs are under
`build/sound-midp/`. The page and wire protocol did not change in this slice.

The live channel-control repair also passes all four required gates, both
cgo-free builds and 321 Node tests. Ten browser runs cover controllers, resume,
gain, ownership and coarse timing in both engines. Each engine's seven
controller scenarios have maximum sample error 5.960464477539063e-8 against
independent native graphs. Held RMS is about 0.09743595 before a controller
mute and after recovery, zero while volume or expression is zero, and
0.02455079 at level 32/127. Pan moves the sounding signal between channels;
peer owners and PCM retain their reference output. These fixtures establish
controller behavior, not physical-phone or handset-timbre acceptance. Logs:
`build/sound-controls/`.

The subsequent string-identity repair passes all four required gates, both
cgo-free builds and 321 Node tests. Authored JVM classes and the MIDP lifecycle
JAR verify canonical literals, native constants, `String.intern()` and retained
event names through heap restoration. Resource-limit, concurrent, malformed
restore and backing-allocation tests pass; reverting either text-detachment
fix makes its memory-retention test fail. Six real scenes retain their earlier
MIDI/PCM output and five successful restores; the known Display-owner refusal
is unchanged. Logs: `build/sound-identity/`.

The MIDI sustain/RPN/reset repair passes all four required gates, both cgo-free
server builds and 330 Node tests. JSON/binary reconstruction and legacy
flattening tests verify parameter ordering, released-key ownership and the
larger bounded replay. Nine native-reference MIDI scenarios pass in each
browser at maximum sample error 5.960464477539063e-8; the five earlier browser
acceptance scripts pass in both engines as well. Six real scenes retain their
output and five successful restores, with the same Display refusal. Details:
[MIDI verification](testing.md#midi-sustain-and-parameters). Logs and anonymous
scene results: `build/sound-midi-state/`.

Per-file decode budgets and aggregate loaded-sound admission now pass the
[resource verification](testing.md#smaf-decoder-bounds), including unchanged
corpus output, bounded fuzzing and the six-scene smoke check. The known Display
refusal remains unchanged at this stage.

Unverified source controls and KTF C pause bindings,
guest timestamps and synthesis quality remain tracked in the sound audit and
local `SOUND.md`. The physical-phone APK confirmation remains outstanding.

## Checkpoint Display and audio catch-up

The WIPI static Display factories may produce a shared Display before a MIDP
owner exists. This follows the [WIPI Display contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Display.md).
The checkpoint now accepts that ownerless state only for an actual WIPI
Display. A MIDP lookup subsequently binds the existing Display to its first
owner; another MIDlet remains refused. Capture and detached restoration check
the types before adopting roots. The authored audio fixture covers both
factory orders, retained clip aliases/gains and malformed graphs.

Repeating the six real scenes after that fix exposed a second refusal in the
same SKT scene: the audio guard counted all note starts and stops in the score,
even when the cursor had already consumed them. Their product exceeded the
work limit although very little playback work was pending. The five other
scenes still restored. Local evidence: `build/sound-wipi-display/runtime.jsonl`.
This guard requires a bounded calculation of the events actually due; raising
the limit would not correct its estimate.

The guard now uses the [shared bounded calculation](audio.md#checkpoint-catch-up-work)
from each saved cursor, including finite repeats, frozen pauses, payload copies
and held-key searches/removals. The same six scenes now all restore and execute
the following tick, without start, input or execution errors. The previously
refused scene retains its MIDI output. Anonymous results:
`build/sound-catchup/runtime.jsonl`; stderr is empty. This establishes the
sampled checkpoint route, not physical listening or real SGS coverage.

## WIPI callback ownership

SKT Java playback now delivers the verified `PlayListener.playUpdate(Clip,II)`
surface. The [documented delivery policy](audio.md#java-wipi-playback-listeners)
uses snapshots in the same bounded queue as MIDP, with distinct pause/resume
codes and no recursive callback delivery. The underlying Player strongly
retains a playing or paused Clip; its idle association is weak. Pending events
independently root the Clip and the listener selected at the transition.

This ownership also crosses full checkpoints. Active Players use a distinct
native kind with a trailing owner reference; strict record fields remain
unchanged. Restoring the previous Player layout rebuilds the association from
the saved Clip. Detached validation refuses broken reciprocal identities,
invalid listeners and cross-owner event data before activation. Both the Go
collector and the JVM heap traversal are covered, including release after
delivery. Ordinary saves are unchanged; earlier binaries refuse the new
native kind or event names instead of accepting an incomplete owner graph.

All four gates, both cgo-free profiles and 330 Node tests pass. All six real
scenes retain output, restore and continue; stderr is empty. Authored Java,
concurrency, forced collection and malformed-checkpoint evidence is described
in [testing](testing.md#skt-wipi-playback-listeners). Logs:
`build/sound-wipi-listener/`.
The subsequent transitive-interface repair also passes those gates and the
six-scene continuation check; its separate Java and derived-listener JAR
regressions are recorded under `build/sound-interface/`.

KTF follows the same playback event policy through `ServiceEvents`. Its
listener is a Clip field, active/paused owners are strong roots, and pending
events independently retain the original Clip and recipient. Idle cycles
remain collectible. The temporary media checkpoint carrier preserves these
edges and completion counters without changing the old strict Clip record or
retaining idle owners after restoration. Legacy no-carrier records start at
their saved completion count and keep existing class layouts. Validation checks
actual guest type graphs and executable callbacks after AOT bindings exist;
malformed preparation leaves source playback and ordinary saves unchanged.
The existing BaseClip playback overloads remain usable, while listener
arguments require Clip. See [KTF verification](testing.md#ktf-wipi-playback-listeners)
and [compatibility details](audio.md#java-wipi-playback-listeners).

LGT uses its explicit guest collector: a Go slice of numeric addresses cannot
retain a callback argument. Active/paused Clip roots, the current listener
edge, queued recipient roots and scoped pins for the entire detached batch
cover these different lifetimes. The batch pins remain until every callback
returns, including callbacks that trigger collection or throw an uncaught
guest exception. Once stopped and delivered, an unreachable Clip/listener
cycle releases both its guest objects and native sound.

The LGT version-3 media envelope carries those ownership identities, pending
recipients and exact completion watermarks alongside unchanged version-2
session/client records. Validation reads real guest class/interface metadata
after restoring bindings and before starting workers. Old version-2 records
recover issued Clip ownership without historical callbacks; older readers
refuse version 3. Repeated adoption preserves the watermark and ordinary saves.
See [LGT verification](testing.md#lgt-wipi-playback-listeners).
