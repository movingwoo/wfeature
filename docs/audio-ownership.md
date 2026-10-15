# Independent audio playback and recovery

Each loaded sound owns its output. Two clips on the same MIDI channel, including
the same pitch, stay independent. Explicit stop, close and restart cancel only
that clip's MIDI voices, PCM, percussion and release tails. Natural score
completion releases its MIDI voices without truncating PCM that extends past the
score's last event. Applying SMAF wave gates is separate open work
([PCM timing](audio.md#deliberately-incomplete)). The contract comes from the
2026-10-08 repair of the shared stop and ownership findings in the
[sound audit](history/audio.md#cross-platform-sound-audit).

## Runtime and Host contract

`backend.Audio` assigns each loaded sound a nonzero, monotonically increasing
`AudioHandle`; handles never wrap into another sound. A Host can implement
`backend.OwnedAudioSink` to receive `AudioEvent(handle, event)` and
`StopSound(handle)`, under the same serialization and borrowed-buffer contract
as `AudioSink`. The web Host implements both interfaces. Each owner has sixteen
independent MIDI channel states. The audible 24-voice budget is global; paused
owners keep bounded, inaudible voice records separately, as described below.

The CLI receives the flattened MIDI/PCM diagnostic stream, whose recording
limits are listed under [Deliberately incomplete](audio.md#deliberately-incomplete);
use the browser renderer to compare mixing and cancellation.

Output checkpoints carry owner identity for voices and remaining PCM, and a
bounded, sorted channel table for each owner. Validation rejects missing,
duplicate and unloaded owners before emitting anything. Checkpoints store raw
output separately from device and clip gain, plus paused cursors, note envelope
ages, finite remaining passes, completion counts, media position, pitch
sensitivity and RPN/NRPN selectors. Pedal-held notes keep their released-key
flag independently of sounding notes whose keys are still down. The accepted
audio state versions (6 through the current 9) and their compatibility rules
are in [audio checkpoint versions](audio.md#audio-checkpoint-versions);
incompatible checkpoints are refused and left on disk, and ordinary saves and
the shared debug/release data formats are unchanged.

PCM shares [one admission and reconstruction budget](audio.md#pcm-admission-and-restoration):
a newest wave beyond 256 references or 32 MiB never reaches output, so excess
PCM cannot later disappear during recovery or block a checkpoint. Stopping a
clip releases only its admitted references. An optional wave-resume sink
carries the fractional frame position independently of the replay timestamp.

## Guest volume

The web Host also implements `backend.AudioGainSink`. Its
`SoundGain(handle, value)` callback updates a clip's held notes, percussion,
release tails and PCM; raw velocities and samples are unchanged. A value of
10,000 is unity (`AudioGainUnity`): the effective gain is the device percentage
multiplied by the clip percentage, divided by 10,000 in the renderer. This
multiplication is the emulator's policy; the WIPI specification defines the
individual 0..100 scales but not their combined transfer function. A separate
backend mute keeps the clip's stored level.

Gain sits after the clip's envelopes and before the user's MIDI and effect
sliders. Changing or zeroing it does not stop, restart or seek sources, so
unmuting keeps the current PCM frame and melodic phase. Natural-end PCM tails
still receive gain updates. Checkpoint validation checks effective gain against
the saved device and clip levels, and reconstruction sends gain before source
events. Late sink attachment also supplies current gains before future
playback.

Connected guest controls are KTF Java Clip and Volume, KTF C device and clip
volume, LGT Java/C clip and device volume, SKVM device volume and SKT WIPI Clip
volume. SKT MIDP `VolumeControl` uses the same per-clip level and mute state.
All clamp to 0..100 and read back their stored setting. SKT accepts WIPI's
boolean `Clip.setVolume` signature and keeps the existing void extension.

KTF and LGT use device default 100. SKT Java reports the handset's default
level, 50, and reads a device level relative to it (`sktOutputVolume`): the
default and anything above play at full output, and lower levels scale down to
silence at zero. Applying 50 as half gain made every SKT title half as loud as
in 0.5.2, where device levels did not reach the output. Clips default to 100.
Ordinary reload and restart keep the clip level. LGT Java data replacement
closes the previous decoded handle, because clearing only its loaded flag left
active sound orphaned.

Legacy diagnostic sinks and older pages scale future velocities and samples and
release held MIDI notes at zero; they cannot adjust active PCM or resume held
notes after mute. Current pages apply the complete gain contract. Vendor source
mutes are remembered and read back but reach no clip, because local titles mute
sources that their own clips then play through
([evidence](history/audio.md#source-mutes-and-the-ktf-c-media-slots-read-from-callers)).
MIDP control lookup and notifications are in
[MIDP per-player volume controls](audio.md#midp-per-player-volume-controls).

Contracts: [WIPI C media](https://mirusu400.github.io/wipi-wiki/c-api/media.md),
[WIPI Clip](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Clip.md)
and [WIPI Volume](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Volume.md).

## Protocol negotiation

Current pages add `sound=resume` to the WebSocket query, which enables clip
ownership, gain and note envelope ages in either JSON or binary protocol 2. The
earlier `sound=owned` enables ownership and gain but receives ordinary note-on
events when output is reconstructed. Without either, the server sends the
legacy event format for an older cached page. An older server can ignore the
query and still drive a current page's owner-zero stream. Cached pages pick up
client changes through the service-worker shell version (`cacheName` in
`web/service-worker.js`).

The operations are laid out in [Protocol 2 sound](session.md#protocol-2-sound).
Their ownership semantics:

- JSON events add `sound`, a 32-bit handle; an omitted `sound` is owner zero.
  Binary `0x07` selects the sound for subsequent operations.
- `stopSound` (binary `0x08`) stops the selected sound, including its PCM and
  tails.
- `soundGain`, with `sound` and `value` (binary `0x09`), sets the selected
  sound's gain from 0 to 10,000.
- `noteResume`, with `sound`, `channel`, `note`, `velocity` and `age` (binary
  `0x0a`), resumes a note at its elapsed envelope age in milliseconds and
  requires `sound=resume`. Age is clamped to unsigned 32-bit milliseconds; a
  missing age means zero. An ordinary `noteOn` always starts a new attack.

Selection starts at zero in every message, so a dropped packet cannot redirect
later events to another clip. `allOff` stays global. PCM and SysEx definitions
are shared by content across owners, and stopping a clip does not discard a
reusable sample definition.

## Clip pause and resume

`Audio.Pause(handle, guestTime)` advances to the requested guest position,
cancels that owner's Host sources and freezes its score cursor, repeat mode,
MIDI envelope ages and PCM frames. `Audio.Resume` excludes the paused interval
from both clocks, reconstructs the held output and continues the remaining note
gates, keeping one-shot or repeating mode. `SetRepeat` can change the next
score-end action without resetting the cursor. Repeated backend pause or resume
calls are idempotent; explicit Stop, Close or Play discards the cursor.

KTF Java/C, LGT Java/C, SKT WIPI Java and SKVM use this path. LGT C and KTF C
resume read only their clip argument and keep the repeat mode. LGT C keeps its
PAUSED/RESUMED callbacks; KTF C, which has never delivered a C media callback,
delivers none. WIPI pause and resume reject invalid transitions. WIPI Stop is
cancellation and WIPI Play starts a new pass. MIDP stop/start and deallocate
keep position; setting media time to zero rewinds the current pass without
replenishing its remaining loop budget. MIDP finite counts, media time,
natural-end state and deferred notifications follow the
[lifecycle contract](audio.md#midp-playback-lifecycle). SKVM pause still
interrupts a blocking play or loop with `UserStopException`; resume is
nonblocking and creates no replacement wait.

The optional `AudioResumeSink` callback carries note age. The page reconstructs
only the remaining attack/decay ramps, or begins at sustain. Percussion uses its
saved noise offset and remaining tail. Melodic oscillator phase and release
tails are not kept. Legacy sinks and pages receive a fresh note-on and cannot
provide the full continuation contract; sinks without ownership cannot cancel
PCM individually.

Only 24 notes sound together. Each paused owner can keep at most 24 additional
notes within the 256-loaded-sound limit; paused records do not take audible
slots. Resuming appends voices in the same order as the page and applies the
audible limit again. PCM shares the 256-wave/32 MiB admission budget across
audible and paused owners: paused waves keep their charges, and a new wave that
cannot fit is consumed without emission, so pause can keep all admitted PCM and
resume it without exceeding the budget. Transient tones cannot be paused.
Invalid or backward pause clocks are refused.

Checkpoints keep frozen output even after a long pause or on a different Host
clock epoch. Reconnect, queue recovery and quick load skip paused owners until
their explicit resume. Validators check matching timeline and output pause
flags, guest clock bounds, drum age/tail agreement and active/retained voice
budgets. Catch-up limits on all three platforms use the frozen guest position.

Contracts: [WIPI Player](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Player.md),
[WIPI C media](https://mirusu400.github.io/wipi-wiki/c-api/media.md) and
[MIDP Player](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/Player.html).

## Queue pressure and reconnect

Ordinary audio, including stop operations, never waits for a full socket queue.
A dropped batch marks output as needing reconstruction. The next tick with
queue capacity sends `allOff` followed by the current backend output, even if
that tick produced no new audio event, and resends lost PCM definitions. A
reconnect to a parked game is rebuilt the same way, after the first tick that
brings the audio timeline to the resumed guest clock.

Collector overflow follows the same path. Normal batches are bounded to 4,096
operations (`maxPendingAudio`). Reconstruction temporarily permits 65,536
(`maxReplayAudio`), enough for the largest owned reconstruction: 58,176
operations for all 256 owners plus owner zero (fourteen operations per channel,
gains, voices and tracked samples), plus up to three controls for each of 2,048
PCM groups (6,144 more). The ordinary bound then applies again. Debug logs
distinguish collector overflow, dropped audio batches and successful
reconstruction. This keeps a missed note-off or stop from leaving a voice stuck
once delivery resumes; it does not reproduce events missed while delivery was
blocked.

A new connection reconstructs the parked session's output after lifecycle
resume. Quick load uses the same bounded collection with its epoch reset. PCM
reconstruction trims elapsed sample frames and skips expired buffers, so
repeated reconstruction cannot restart their original prefix. Capable pages
resume note envelopes at their elapsed ages. Physical phase, release tails and
output latency are not saved. Active PCM age follows the unscaled Host clock,
including time spent parked.

## Verification

Before the 2026-10-08 repair, two regressions failed: stopping one clip removed
another clip's note, and stopping an already completed score kept its long PCM
buffer. Maintained tests cover:

- Independent equal-pitch voices, program/controller/bend state, natural end,
  explicit stop/close/restart, global voice limits and whole-session reset.
- Checkpoint round trips at a different clock epoch, owned PCM tails, malformed
  owner records, handle exhaustion and per-owner PCM accounting beyond the limit.
- Real WebSocket capability negotiation for both formats and legacy clients,
  binary operation truncation, shared PCM definitions and message-local owners.
- Nonblocking cancellation under queue pressure, recovery on an otherwise empty
  tick, collector overflow and the largest supported reconstruction.
- An authored SGS session through dropped note/stop recovery, live quick load,
  park/resume and both protocol formats, using the ordinary session and Host
  paths rather than injecting events into a running guest.

`web/acceptance/audio-ownership.mjs` renders the graph in Chromium and WebKit.
Two simultaneous equal-pitch MIDI clips and two PCM clips are separated in
stereo; after one is stopped, both rendered channels match the surviving clip
rendered alone within RMS tolerance 0.000001. Surviving-channel RMS is 0.076715
for MIDI and 0.113137 for PCM in both engines. The twelve-short-note
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
their state through a full checkpoint, and concurrent SKT level setters keep
guest getters and output in agreement.

The authored `media-volume.jar` tests Java interface calls, stable control
identity, fresh control arrays, clamping, mute/restore and change notifications
while two players each emit MIDI and PCM. It restores a muted player and its
control/listener graph, verifies reentrant callbacks and keeps closed-player
controls safe. Five malformed graph variants are refused. Concurrent native
setters and getters pass under the race detector; clip levels have a single
backend record.

`web/acceptance/audio-gain.mjs` passes in Chromium and WebKit: held MIDI and
nonperiodic PCM follow zero, quarter and full gain, with each output sample
matching an uninterrupted reference within 0.000001, and the other owner's
output is unchanged. A separate regression resets the output clock with an
older gain still scheduled: the latest mute cancels that future automation, so
no unintended unmute occurs.

Pause regressions cover encoded checkpoint restoration, frozen note/drum ages
and stereo PCM suffixes, owner isolation, active/retained voice limits and
matching eviction order; bounded audible output replaced the earlier refusal of
omitted PCM. They refuse transient pauses, malformed records and invalid clocks
without cancelling valid output. KTF VM and LGT ARM Java/C calls keep remaining
note gates and repeat; C tests vary the unused register and check state
callbacks. SKT tests run existing authored JARs through MIDP paused
checkpoint/start, deallocate, rewind, changed repeat settings, WIPI
pause/resume versus a new Play, and SKVM interruption without another wait.
Reentrant deallocate listeners can restart or close without splitting guest
state from the backend. All three platforms bound checkpoint catch-up using the
frozen pause clock.

`web/audio-resume.test.mjs` covers attack/decay/sustain continuation,
percussion offset/expiry and release after clock correction. The graph in
`web/acceptance/audio-resume.mjs` passes in Chromium and WebKit: paused-owner
RMS, resumed-reference sample error and peer sample error are zero. MIDI
resumes at 291 ms with onset RMS 0.0782802, PCM resumes at frame 13,984 with
RMS 0.0924937, and percussion resumes at 91 ms with RMS 0.000726991 and no
extended tail after channel gain was separated from the velocity envelope. The
independent references use a fresh melodic oscillator phase, so these
measurements establish envelope and sample-position behavior, not phase
continuity. Gain, ownership and twelve-note coarse-clock render checks also
pass in both engines: eight browser runs in total.

The repair slices recorded passing `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...` and both `CGO_ENABLED=0` server
profiles, and repeated a six-scene local-archive smoke check (two KTF, one LGT,
three SKT). The per-slice results, Node test counts, the SKT Display refusal
that the smoke check exposed, and the anonymous logs under `build/sound-*` are
in the [repair validation record](history/audio.md#repair-validation-runs).
Source controls and the KTF C pause bindings were later settled from callers
([audio history](history/audio.md#source-mutes-and-the-ktf-c-media-slots-read-from-callers)).
Synthesis quality and physical-phone APK confirmation are outstanding.

## Checkpoint Display and audio catch-up

The WIPI static Display factories may produce a shared Display before a MIDP
owner exists, as the [WIPI Display contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Display.md)
allows. SKT checkpoints accept that ownerless state only for an actual WIPI
Display. A MIDP lookup then binds the existing Display to its first owner;
another MIDlet is refused. Capture and detached restoration check the types
before adopting roots. The authored audio fixture covers both factory orders,
retained clip aliases and gains, and malformed graphs.

Audio catch-up on restore uses the
[shared bounded calculation](audio.md#checkpoint-catch-up-work) from each saved
cursor, counting only the work actually due, including finite repeats, frozen
pauses, payload copies and held-key searches and removals. An earlier guard
counted every note start and stop in the score, including those the cursor had
consumed, and refused a long SKT score although little work was pending; raising
the limit would not have corrected that estimate. The history is in the
[repair validation record](history/audio.md#repair-validation-runs).

## WIPI callback ownership

Java `PlayListener` delivery on all three platforms, and the strong, weak and
pinned references each uses to keep a Clip and its listener alive, are
specified in [Java WIPI playback listeners](audio.md#java-wipi-playback-listeners).
Verification:

- SKT: [testing](testing.md#skt-wipi-playback-listeners) covers authored Java,
  concurrency, forced collection in both the Go collector and the JVM heap
  traversal (including release after delivery) and malformed checkpoints. A
  separate transitive-interface regression covers Java and derived-listener
  JARs.
- KTF: [testing](testing.md#ktf-wipi-playback-listeners).
- LGT: [testing](testing.md#lgt-wipi-playback-listeners).
