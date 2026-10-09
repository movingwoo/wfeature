# Audio history

This file preserves the dated sound evidence behind the October 2026 playback
repairs: the cross-platform audit and the app timing investigation that came
before it. Statements about current behavior, corpus size and open work apply
to their original revision; later records may supersede them. Start with
[audio.md](../audio.md) and [audio ownership](../audio-ownership.md) for the
maintained contracts.

## Cross-platform sound audit

### Scope and conclusion

The 2026-10-08 audit followed the [APK timing repair](#apk-audio-timing). KTF, LGT,
SKT Java and SKT SGS use the shared `backend.Audio` output and the page's
`PageAudio`. The coarse-clock scheduling and release defects therefore apply
to any of these paths on an affected output, and the common client repair covers
them. The original report remains KTF, probably APK 0.5.0; physical-phone
confirmation and LGT/SKT APK listening comparisons remain outstanding.

This audit added no playback changes. The findings below are present in the
working tree after the timing repair. A successful diagnostic reproduction is
evidence of a remaining defect, not a passing playback acceptance test.

### Repair status

The subsequent [ownership and recovery repair](../audio-ownership.md) fixes shared
clip stop, explicit PCM cancellation, equal-pitch/channel interference, output
reconstruction after drops/overflow, and reconnect/quick-load ownership. It
retains natural PCM tails. The subsequent gain repair connects known device
and clip setters across KTF, LGT and SKT, including active MIDI/PCM attenuation
and restoration after zero volume. SKT MIDP VolumeControl now exposes independent
level/mute state, stable control identity and change notifications, including
after a checkpoint. The [pause repair](../audio-ownership.md#clip-pause-and-resume)
retains cursors, repeat mode, note envelope ages and PCM positions across the
implemented Java/MIDP/SKVM and LGT C paths. Unknown source mappings and KTF C
pause bindings remain open. Per-file decoder budgets now bound allocation and
expansion, as described below. Sustain, RPN pitch sensitivity
and controller reset now follow the [MIDI state contract](../audio.md#midi-sustain-pitch-range-and-reset).
The [MIDP lifecycle repair](../audio.md#midp-playback-lifecycle)
adds finite repeat counts, natural completion, media time, closed-player guards
and ordered deferred callbacks, including pending events across quick load.
Standard event names now preserve reference identity with JVM literals and
API constants, as demonstrated by the MIDP specification. A bounded JVM string
pool also preserves that identity through quick load; freshly constructed
strings remain separate objects. See [event delivery](../audio.md#midp-per-player-volume-controls)
and the [JVM contract](../jvm.md#implemented).
Transient tone handles now close on completion/stop and keep that lifetime
through quick load. Unsupported tone-sequence players are refused and omitted
from filtered capability queries. The tables below
retain the original audit observations, not current failures for the repaired
items. KTF Java and SKT WIPI resume now retain their repeat mode; LGT C resume
ignores the unspecified second register. The repair record separates authored and browser proof from real-phone
listening still required.

The gain work also found and repaired LGT Java data replacement dropping its
loaded flag without closing the old handle. A regression now clears an active
clip, proves its previous output and handle are released, and verifies that
the new decode retains its clip level.

### Reproduced playback gaps

| Boundary | Evidence | Required behavior |
| --- | --- | --- |
| LGT guest volume/mute | Device zero, clip zero, source zero and source mute each still emitted an authored note at velocity 100. | Apply the guest's level independently of the user's page sliders. Confirm vendor source identifiers before mapping mute. |
| SKVM guest volume | Volume zero followed by clip playback emitted velocity 100. | Make the getter/setter state control actual sound. |
| Shared clip stop | Two handles played channel 0, notes 60 and 67. Stopping the first cleared both output voices while the second handle stayed playing. | One clip's stop or natural completion must not silence another. |
| Shared PCM stop/mute | After emitting 8,000 mono samples at 8 kHz, stopping the handle and setting device volume to zero left all samples in output state. | Track playback ownership and cancel/mute the relevant PCM source. |
| LGT C resume | A repeating clip resumed with repeat disabled when unused r1 was zero. | `MC_mdaResume(clip)` must preserve the previous mode and ignore unspecified arguments. |
| SKT MIDP completion | After a 400 ms one-shot finished, state remained STARTED (400), media time remained zero, and another start emitted no note. | Finish the state transition, deliver natural-end events, report progress and permit replay. |
| SKT MIDP loop count | Count 2 produced six starts in 2.1 seconds from the authored 400 ms sound. | Preserve finite counts instead of converting them to a repeat boolean. |
| SKT transient tones | Call 257 to `Manager.playTone` failed with the 256-loaded-sound limit after previous tones had completed. | Release transient tone handles without closing reusable clips. |

Code boundaries: `internal/backend/audio.go`, `audio_output.go`;
`internal/platform/lgt/wipic_media.go`, `java_sound.go`; and
`internal/platform/skt/media.go`, `skvm.go`, `wipi.go`.
The shared stop emits CC120/123 for every used MIDI channel. The sink and wire
format have no clip identity or individual PCM-stop operation. Whole-session
page stop/reset does cancel PCM, percussion and release tails.

Code inspection also found KTF Java clip volume stored without affecting output,
KTF Java and C source mute accepted without applying it, and SKT WIPI clip volume
registered as a no-op with a constant getter. KTF C device volume already scales
new note velocities and PCM samples; zero releases active MIDI notes, but does
not cancel PCM already emitted. Individual pause/resume generally restarts the
clip. LGT Java preserves its repeat flag, while SKT WIPI resume disables repeat.
LGT C completion callbacks and SKVM blocking-play interruption already have tests
and must be preserved.

The contracts were checked against the WIPI specification's
[C media API](https://mirusu400.github.io/wipi-wiki/c-api/media.md),
[Clip API](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Clip.md)
and [MIDP Player API](https://mirusu400.github.io/wipi-wiki/midp/java-api/javax/microedition/media/Player.md).
Vendor slot/source mappings still require observed evidence.

### Unverified vendor source and slot mappings

A follow-up search compared both published specification versions with the
current bindings and local traces. The [1.2.1 C media contract](https://mirusu400.github.io/wipi-wiki/c-api/media.md)
names TONE, SOUND and RECORDER mute categories without assigning numeric IDs.
Its pause/resume functions each take only a clip pointer, notify states 4/5
on success, and distinguish unsupported from invalid state. This establishes
the function contract, not a carrier's dispatch-table slots.

The [2.0 Java media API](https://mirusu400.github.io/wipi-wiki/v20/java-api/media.md)
assigns VOICE=1, RING=2, KEYTONE=3, MESSAGE=4, ALARM=5, ALERT=6, MMEDIA=7 and
GAME=8; MMEDIA addresses all multimedia devices. The
[2.0 HAL media categories](https://mirusu400.github.io/wipi-wiki/v20/hal/media.md)
run from GENERAL through GAME. Neither proves the meaning of LGT source 11,
nor that an older carrier source 3/6 uses the Java 2.0 namespace. KTF Java's
boolean `setDefaultVolume(II)Z` also differs from the standard void signature.

KTF C table 10/function 17 accepts source/mute but has no audio effect; slots
9/10 are no-ops between known play=8 and stop=11. Their position next to play
and stop suggests pause and resume, but no SDK declaration or original-runtime
dispatch entry names them, and slot 17 has no name at all, so neither can
establish this carrier ABI. LGT source controls at
`0x4cf`–`0x4d2` retain levels/mute in getters and checkpoints but do not route
them to audio. Observed source 11 and a source-6 startup mute do not establish
those categories' target sounds.

No matching carrier ABI table was found in the published specification corpus
or existing traces. Implementation needs matching SDK/import declarations or
named original-runtime dispatch entries, followed by an authored caller test.
These unresolved mappings must not become guessed global mute controls.

### Rendered controller behavior

CC7/CC11/CC10 are now live per-channel controls, including sounding release and
percussion tails. [The repair](../audio.md#live-midi-channel-controls) separates
channel gain/pan from note velocity/envelopes and reconstructs current controls
before resumed notes. Seven native-reference scenarios pass in both browsers;
maximum sample error is 5.96e-8, mute RMS is zero and restoration preserves the
held level without another attack. Sustain, RPN pitch sensitivity and CC120/121/123
also now work, with nine additional native-reference scenarios passing in both
engines at maximum sample error 5.96e-8. The observations below remain the original
audit evidence from before those repairs.


Real OfflineAudioContext probes in Chromium and WebKit produced the same results:

- A held A4 retained late RMS about 0.05425 after CC7 became zero, identical to
  the unchanged-volume control. CC7/11/10 update future notes only.
- With CC64 set to 127, note-off still released the voice; its later output was
  silent while the pedal remained down.
- After RPN 0 requested a twelve-semitone bend range, maximum bend rendered A4
  at about 493.876 Hz. The page still used its fixed two-semitone range.

The repair updates output/checkpoint state together. Audio version 6 records
pitch sensitivity, both RPN/NRPN selectors and released keys retained by sustain.
Resume restores current controls before notes; versions below 6 are refused.

At the original audit, `pcmTrackEvents` dropped parsed PCM controls and gate
lengths. ATR volume/expression/pan now follow the contract below through decoder,
runtime state, negotiated transport and live page groups. Bend and gate behavior
remain unresolved. Score-attached waves still bypass melodic velocity handling.

#### PCM contract evidence

Yamaha's SMAF 3.05 specification, sections 2.2–2.3 and 4.5.1 (pages 9 and
51–54), defines four PCM channels, volume/expression 0–127, expression default
127, and pan/bend defaults 64. Recommended amplitude is `(value/127)^2` and
pan gains are `cos(pi*value/254)` and `sin(pi*value/254)`. Duration advances the
sequence independently of GateTime. Gate zero is reserved. The reviewed text
does not settle initial volume, bend range, short-sample looping, exact gate
cutoff or same-channel retrigger behavior. The existing `0x36` expression alias
is an unverified compatibility extension; the documented opcode is `0x3b`.
Section 4.4.2 (page 43) gives score notes a 0–127 range and omitted velocity the
previous channel value, initially 64; stream-wave assignment is implementation
dependent. [Yamaha specification, mirrored copy](https://img.atwiki.jp/mmfuta/attach/19/78/SMAF3.05.pdf#page=51).

The Yamaha MA-5 authoring manual, section 4.2.3 (page 34), separately supports
editable stream velocity, position/length and two overlapping stream voices.
Its bank-125 note-to-wave mapping in section 4.5.1 (page 46) describes that
profile's SMF authoring path; it does not establish a universal SMAF wire rule
or same-channel replacement rule. [Yamaha MA-5 manual, mirrored copy](https://manuals.plus/m/66bfe529849a714a1d213edf827619a5c58de8170e50de271d0a9ea7e8cb2892.pdf#page=34).

A further primary-text review distinguishes format definitions from playback
guidelines: ATR duration and gate use separate time bases (page 49), and the
EOS guidance recommends muting at sequence end (page 51). The current natural
PCM-tail policy differs from that recommendation; original-runtime comparison
is still required before changing it. The MA-5 manual's release, sustain,
ignore-key-off and loop-point settings (pages 66–70) concern PCM instruments;
they do not establish ATR stream looping or gate behavior.

An anonymous 1x-rate corpus census covers the same 11,175 carrier-local records,
including 5,010 supported mono ATR tracks and 8,752 wave triggers. Gate length
is shorter than the decoded sample on 5,567 triggers, longer on 2,941 and equal
on 244; 14 gates are zero. Positive gates would trim more than 10% of the sample
on 5,042 triggers, so a universal hard cutoff would materially change output.
Decoded samples cross sequence end on 2,683 triggers and overlap the next
same-channel trigger on 1,324. No PCM bend event appears, and at most one channel
is used per sound. These are encoded durations and current-decoder lengths,
not recorded handset behavior: neither overlap nor a long gate proves looping
or replacement. The 1,014 known archive-member read exclusions remain.
The scanner, per-record anonymous counts and summary remain under
`build/sound-pcm-contract/`; no game files or saves are changed.

Current score note zero selects `waves[channel+1]` and discards missing waves.
It now updates channel velocity memory before that dispatch, even when the wave
is absent or unsupported. Mobile CC121 also restores the remembered default of
64, as specified on page 43; previously both paths left later omitted-velocity
notes at the wrong level. [Authored and corpus verification](../testing.md#pcm-buffer-reuse-and-mobile-velocity)
covers this repair. Changing the wave mapping or its gain needs profile evidence.

The [PCM control implementation](../audio.md#live-pcm-track-controls) now preserves
owner/track/channel identity and raw samples, with current controls applied to
all sustained tails. Initial volume 127 is an explicit neutral compatibility
policy; documented expression 127 and pan 64 remain distinct. Checkpoint v7
retains these states and reads genuine v6 without inventing old routing.
Normal output admits at most 2,048 groups; reconstruction fits the existing
65,536-operation limit. [Verification](../testing.md#pcm-track-control-propagation)
covers authored byte fixtures, full state/wire/page paths and the anonymous
corpus. Native rendering [passes](../testing.md#native-audio-rendering-acceptance)
in Chromium and WebKit; listening remains open. No reference code or assets
were imported.

### Delivery and resource limits

#### Java WIPI PlayListener contract

The published [PlayListener](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/PlayListener.md)
contract declares `playUpdate(Clip, int event, int parm)` and event constants
ERROR=-1, END_OF_DATA=1, START=2, STOP=3, PAUSE=4, RESUME=5, RECORD=6 and
FULL_OF_DATA=7. The shared declaration formerly exposed `playDone(Clip)`
instead; it now has the verified signature and all eight constants.
[Clip.setListener](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Clip.md)
replaces one listener and null removes it.

SKT now delivers playback transitions through its bounded deferred Player
queue, preserving WIPI pause/resume event identity, original recipients and
per-pass completion. Active, paused and queued owners survive GC and complete
checkpoints; idle owners are weakly associated and become collectible after
delivery. Previous Player layouts remain readable. The selected policies and
rollback boundary are documented in [audio behavior](../audio.md#java-wipi-playback-listeners).

KTF now declares the same callback and constants and delivers through its
serialized Host event service. Its checkpoint, GC and scheduler boundaries
are described below. LGT now delivers the same callback through its existing
between-frame media service, with explicit guest GC ownership and validated
ARM/Thumb callback targets.
The specification leaves callback threading, deferred-recipient replacement,
per-loop notifications and event-specific `parm` values unclear. It separately declares
`Clip.playUpdate(int,int)` and `playStart(boolean)` without explaining the
former's boolean return, so those missing surfaces need their own contract
verification.

##### KTF callback implementation boundaries

The KTF audit found that `Clip.setListener` was a no-op and Clip bookkeeping
used weak owner keys. The repair keeps the listener on the Clip object graph,
with separate strong roots for playing/paused owners and queued recipients.
A listener retained directly in the weak-key bookkeeping would leak a
listener-to-Clip cycle. Forced-collection tests cover active, paused, idle and
pending states before and after full checkpoint restoration. Natural completion
is observed before stop, pause, restart or orphan cleanup can discard it.

`ServiceEvents` now drains the media snapshot even when the generic queue is
empty or the guest owns its event loop. Reentrant notifications wait for the
next drain. `serviceAudio` takes the guest lock and propagates bounded-queue
errors. `NextDeadline` includes pending notifications and the next natural
completion, using the shared audio query without advancing playback. A long
unrelated guest sleep therefore does not delay callbacks; pause and speed
changes adjust those deadlines.
Review reproduced same-round delivery when a media callback posted a generic
event, including a full generic queue, and loss of the media prefix when a
generic callback raised a recoverable Java exception. The service now runs
the existing generic prefix first, preserving the captured media prefix until
that succeeds; newly posted media remains for the next round. A saved media
counter also must equal the saved audio counter, preventing an implicit
completion backlog from overflowing or being lost immediately after restore.

KTF AOT metadata in the JVM omits interface lists, while guest class records
do contain them. JVM `IsInstance` alone therefore cannot validate these
listeners. Validation rereads the bounded guest superclass/interface graph
without trusting saved summaries, including derived interfaces and cycles.
The exact callback method and its executable ARM/Thumb entry are checked
against the guest binding. Native heap payload restoration happens
before AOT object/class bindings are installed, so typed validation belongs
after heap restoration and before workers start.

Existing root and Clip checkpoint structs are strict: adding optional fields
would reject previous records. The new `ktf-media-v1` native payload uses a
reserved named root and preserves the old shapes. The carrier is discarded
after its owners, counters and queue are adopted. Records without the carrier
use the audio completion count as their initial watermark. A literal old empty
interface record delivers concrete callbacks and can be saved again without
repeating startup or old END events; its field and interface-method tables
are deliberately not rewritten. Existing `Player(BaseClip)` overloads retain
playback/checkpoint support, while a BaseClip cannot be substituted for a
callback's required Clip argument. Authored regression and malformed-record
evidence is in [testing](../testing.md#ktf-wipi-playback-listeners).

##### LGT callback implementation boundaries

LGT Java now delivers playback transitions through a separate bounded queue at
`serviceMediaCallbacks`. The authored descriptor/JAR/AOT application in
`internal/testfixture/lgt_listener.go` boots a Jlet and executes a real ARM
`playUpdate(receiver, Clip, event, parm)`. Its original empty-history failures
are recorded in `build/sound-lgt-listener/lifecycle-before.log`.

Successful Play, active/paused Stop, Pause, Resume and each natural pass produce
the documented integer events with `parm` zero. Recipient snapshots survive
replacement, null removal and buffer invalidation. Reentrant events wait for
the next batch; an uncaught guest exception ends only its callback. LGT keeps
its existing boolean behavior: Play while playing is refused and stopping a
loaded idle clip succeeds without adding another STOP. C callbacks retain
their own queue and status semantics. Pending events and natural completion
shorten the guest-clock tick, including zero-duration one-shots.

The collector now roots playing/paused Java Clips and queued owner/recipient
pairs, follows each reachable Clip's listener edge, and releases idle Java
media entries and sounds when the corresponding object is swept. Every pair
in a detached callback batch is pinned until delivery finishes. Forced
collection inside the first callback proves that later recipients survive;
idle listener-to-Clip cycles become collectible after delivery.

Listeners must be issued objects with an executable ARM/Thumb callback.
Bounded raw-record validation supports inherited and derived interfaces and
checks the exact named descriptor. Stripped direct one-method interfaces
support both entry-address and receiver-vtable forms. Cached class summaries
cannot supply an interface or callback absent from guest memory.

Real-archive acceptance exposed an overstrict nominal-type check. Both
`735a579d82ac53bb` and `70d709c40e10541f` omit the interface table entirely
(header `+0x14` is zero), but their actual member runs contain the exact
`playUpdate(Lorg/kwis/msp/media/Clip;II)V` callback with an executable body.
Their receiver vtables are unambiguous; one vtable is synthesized and the other
is compiler supplied. A diagnostic overlay with the old unchecked setter and
no event delivery runs through both failing scenes, confirming a new regression.
The validator now accepts that exact raw member without nominal interface
metadata. If the callback member is stripped too, it still requires a verified
PlayListener dispatch entry. Authored live/restore tests preserve this distinction.
Evidence: `build/sound-lgt-listener/metadata/`, `legacy-setter/runtime.jsonl`
and the anonymous runtime probes. The legacy-setter overlay isolates execution
only: its deliberately disabled Java ownership causes new-format capture to
refuse, so its checkpoint result is not a compatibility test.
Review found that clearing a raw superclass could still inherit through a
cached edge; that mismatch is now refused before replacing a valid listener.
Imported PlayListener constants also previously read as zero. All eight now
receive the published values; `constants-before.log` records that regression.
A second review reproduced a stripped child's concrete callback being bypassed
by a parent's named method. Delivery now follows the receiver's verified
interface dispatch before an inherited body, and both address/slot forms
continue correctly after a full restore. The original mismatch against an
ordinary guest interface call is in `review/override-before.log`.

A version-3 media envelope preserves completion watermarks and pending event
snapshots around the unchanged strict version-2 session/client records.
Previously the queue vanished on restore; `checkpoint-before.log` reproduces
pending, paused and completed-pass losses. The new reader validates all owners,
recipients, raw records and exact audio completion counts after Java bindings
exist and before workers start. The legacy version-2 reader infers issued Clip
owners and takes their saved audio count without replaying historical events.
It cannot recover a Clip already absent from an old object graph. New snapshots
without Java media still use version 2. An older binary refuses the new envelope;
rollback uses an older supported checkpoint or ordinary startup. No ordinary
save data changes. Acceptance evidence is in
[testing](../testing.md#lgt-wipi-playback-listeners).

#### Guest event delivery

The backend now [merges due scores globally](../audio.md#global-score-order)
before applying the shared voice budget, including repeats, progress queries
and pause/resume boundaries. Native completion reconciliation and SKT audio
transactions preserve callback recipients, checkpoint validity and owner
retention when a query also advances its peers. Coarse versus fine authored
passes retain the same ordered note gates and 24 melodic voices.

The [presentation clock](../audio.md#presentation-timestamps-and-recovery) now
crosses the sink, JSON/binary transport and page. Speed transitions preserve
elapsed presentation time; silent batches retain a frontier. The page accepts
a bounded scheduling window or reconstructs current output, preserving short
gates, scheduled stops and restarts inside a packet. Retained envelopes, drums
and PCM include authored age and cannot rewind behind elapsed Host time.
New regressions cover volume/GC control timestamps, strict completion state at
rate changes, malformed-first-packet recovery and quick load during recovery.

Node and Go checks establish the scheduling contracts. Since 2026-10-09 the
port-free Chromium/WebKit rendering harness `web/acceptance/audio-presentation.mjs`
[passes in both engines](../testing.md#native-audio-rendering-acceptance), and the
Go socket negotiation tests run with real loopback binding. A
[live browser session](../testing.md#live-audio-session-acceptance) against a
running server negotiates every extension and schedules every source ahead of
the render clock on four archives in both engines; server stalls at one
title's start and one late batch fall back to reconstruction as designed.
Physical-device listening remains acceptance work. Debug reports now record separate
[batch-admission lateness](../audio.md#playout-lateness-diagnostics) distributions,
without retaining event payloads or collecting in release. First/reset anchors,
ordinary continuing anchors, clock-only batches and window refusals remain
distinguishable. Authored tests verify aggregation and unchanged recovery;
actual device distributions and later dispatch/native latency still need
measurement. The writer's picture wait remains bounded at 10 ms.

The 4,096-event collector cap and droppable outbound audio can discard note-offs
or controller changes. The [recovery path](../audio-ownership.md#queue-pressure-and-reconnect)
now resets and reconstructs current owned output after either condition,
including aged melodic envelopes and trimmed PCM. Overflow and dropped-batch
diagnostics remain separate. The preceding APK capture recorded no transport
drops, so this repair does not establish that transport caused that report.

The 24-note map is not a limit on every live PCM/percussion/release source.
The page now [reuses immutable PCM buffers](../audio.md#decoded-pcm-buffer-reuse)
within an explicit cache budget. An authored 512-trigger probe reduced buffer
creation requests from 512 to one and float payload writes from 4,096,000 to
8,000 bytes, retaining 512 independent sources and identical samples. This
measures API calls and copies, not native memory or CPU.

A subsequent authored pressure probe confirms a single valid 2,048-wave batch
can create 2,048 concurrent sources and 4,100 connected mock nodes despite
sharing one 32,000-byte buffer. Same-key melody retriggers, release tails and
percussion also reach 512 concurrent sources while the held-key map contains
one, zero and 24 entries respectively. With punctual simulated completion,
10-second PCM at 100 Hz reaches 512 live/scheduled sources before natural ends.
This is concurrent pressure, not evidence of missing cleanup.

The page now applies [source and PCM-reference bounds](../audio.md#live-source-bounds)
before batch dispatch. The 2,048-wave probe is refused before any buffer or
source allocation; the advancing-clock probe stops admission at 419 retained
320,000-byte float references under the 128 MiB cap. Timed pressure invokes
the existing reconstruction path; a full 280-source replay fits.
`build/sound-source-pressure/` retains before/after API counts and the authored
backend retention probe; these are not native-memory or rendered-output measurements.

That probe also exposed a backend mismatch: 512 PCM starts reached the sink,
but only 256 could be reconstructed, and quick save and pause failed until the
unretained tails expired. The backend now [admits PCM before emission](../audio.md#pcm-admission-and-restoration)
under its existing 256-reference/32 MiB raw budget. Excess starts are consumed
without emission; existing output and later controls continue. Paused waves
retain their reservation. Version-8 checkpoints preserve each original byte
charge even when only the sample suffix is saved, preventing quick load from
creating capacity that the live timeline did not have. Version-6/7 records
remain readable with charges derived from their saved tails.

Actual backend playback and reconstruction match all 11,175 statically decoded
sounds: 10,688 PCM starts and 2,912 retained tails. An independent periodic
interval analysis finds no individual sound exceeding either backend limit
under infinite repetition; the largest reference count is 64. These results
cover individual sounds at 1x, not concurrent game activity or native browser
cost. The [admission tests and corpus method](../testing.md#pcm-admission-and-checkpoint-charges)
also documented a subframe reconstruction limitation, now repaired below. Native
CPU/allocation, sustained browser pressure and actual mixed-output clipping
remain acceptance work.

Capturing midway through a PCM frame formerly discarded its fractional age.
For a one-second 4 Hz wave captured at 125 ms, restoration still retained a
sample after the uninterrupted wave had ended. At capacity that changed which
subsequent onset was admitted. Version 9 retains a bounded integer frame phase,
so repeated capture/restore preserves frame selection and floored expiry without
rounding through nanoseconds. A negotiated extension carries the fraction through
both protocols to Web Audio's buffer offset, also fixing fractional replay on
pause and reconnect. Older checkpoints and pages retain their whole-frame
interpretation. [Regression evidence](../testing.md#fractional-pcm-reconstruction)
distinguishes verified position/accounting from native rendering, which
[now matches exactly](../testing.md#native-audio-rendering-acceptance).

#### KTF speed-change clock continuity

The KTF Java/C client originally multiplied all time since `clockBase` by the
latest speed. Changing speed therefore changed time that had already elapsed.
An authored manual-clock reproduction at one second moves audio time backward
to 500 ms when switching to 0.5x, after which pause fails with an invalid clock.
Switching to 2x instead jumps to two seconds and immediately consumes a note
with 100 ms remaining. The complete Host/session setter path does not rebase
the origin. Native KTF's `SpeedClock` preserves the instant in both cases.

The repair must preserve the current guest instant and change only its future
rate, including paused clips and checkpoint clock relationships. This is a
prerequisite for trustworthy event timestamps; it is independent of the
browser's arrival-time scheduling repair.

The implemented repair rebases both active/prepared origins at one Host instant,
with nonbackward fractional rounding and transactional range refusal. It keeps
the existing checkpoint representation. A separate regression also showed that
shared `ClampSpeed` accepted NaN and corrupted `SpeedClock`; NaN now selects 1.
Two real KTF scenes each switched through 0.5x, 2x and 1x, retained MIDI/PCM,
restored checkpoints and executed the next tick. All six measured boundary
deltas were forward and below 9 microseconds, rather than the prior large
jumps. Local anonymous results: `build/sound-speed/runtime.jsonl`; stderr is
empty. This is clock/continuation evidence, not a listening comparison.

### Bounded decoding

Repaired. The original two-byte single-leaf Huffman reproduction expanded to
8 MiB; a safe regression confirmed that 8 MiB plus one was also allocated.
The [decoder contract](../audio.md#decoder-resource-limits) now checks declared
lengths before allocation and shares byte, record, chunk, output-event, PCM and
SysEx budgets across the whole file. Hard failures cannot become partial success
through the parser's ordinary malformed-tail tolerance. `Decode` exposes the
cause through `backend.Audio.Load`; `Play` keeps its silent-failure contract.

Implementation review also reproduced signed setup-length overflow and wrapped
event times. Length readers now refuse quantities outside their integer range;
track, gate and layer arithmetic is checked in 64 bits before narrowing.
Boundary-size valid input remains accepted. The tests use small private budgets
for aggregate cases; no hostile multi-gigabyte allocation was executed.

Separate before/after binaries scanned the same seven local carrier folders.
All 11,175 carrier-local unique sounds have identical full output hashes and
metrics: KTF 5,976, LGT 3,628 and SKT 1,571. The hash includes ordered event fields,
timestamps, PCM samples and SysEx bytes. Per-file measured maxima are:

| Resource | Maximum |
| --- | ---: |
| Raw file bytes | 63,778 |
| Top-level / total nested chunks | 6 / 20 |
| Decoded sequence bytes | 28,543 |
| Parsed / playable event records | 6,494 / 12,569 |
| PCM bytes summed over triggers | 763,800 |
| Largest PCM trigger | 145,196 |
| Output SysEx bytes | 13,184 |

There were no decoder errors, panics, diagnostic preflight refusals or oversized
members. Coverage excludes 1,014 KTF ZIP-member read failures: 927 checksum
errors and 87 other read errors. The older audit silently skipped these same
reads; sound membership and counts are unchanged. The new scanner reports exit
2 for these exclusions, while the complete output comparison passes. No
compressed score tracks appeared, so authored tests cover that dialect's bounds.
This input-boundary repair does not establish the APK report's cause.

The subsequent [admission repair](../audio.md#loaded-sound-resources) also caps the
combined payload of 256 loaded sounds, using the checkpoint's 1,048,576-entry
and 128 MiB limits and reserving possible active keys. Failed admission leaves
handles and playing peers unchanged. Authored buffers are copied after acceptance
to avoid retaining oversized caller allocations through small subslices.
A separate reproduction grew 501 active-key records from 250 repeated passes
of one key; held-key replacement and zero-velocity release now match the output.
All required gates, both cgo-free builds and six real-scene smoke checks pass;
the five successful restores and known Display-owner refusal are unchanged.
Diagnostics and comparison: `build/sound-bounds/`; maintained tests are described
in [the test guide](../testing.md#smaf-decoder-bounds).

### Corpus evidence

Seven carrier-labelled local library folders were scanned, excluding hidden QA
copies. ZIP traversal handled prepended JAR headers, descended up to four levels
and bounded members at 64 MiB; none exceeded that bound. Input counts can include
duplicate packages. Sound hashes were deduplicated within each carrier group.

| Group | ZIP/JAR inputs | Inputs with SMAF | Unique SMAF | With MIDI | With PCM | Neither |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| KTF | 300 | 275 | 5,976 | 2,632 | 3,499 | 21 |
| LGT | 131 | 114 | 3,628 | 1,494 | 2,145 | 8 |
| SKT | 106 | 92 | 1,571 | 775 | 829 | 7 |

All 11,175 carrier-local unique SMAF files parsed without a returned error;
emitted events passed order/range/basic-wave checks. Mixed files count in both
MIDI and PCM columns. The 36 files without audible events are classified below;
partial parsing can succeed, so these are not fidelity or full-decoding results.
Database/native-array/custom-packed/generated sounds are outside this scan.

Twelve-semitone RPN requests occurred in 33/37/5 KTF/LGT/SKT files; pedal-on
values in 29/40/0; and PCM volume events in 2,950/984/772. Presence does not prove
audible damage: a control can request a default, or affect no sounding voice.
Discovered wave encodings were mono ADPCM and four mono 8-bit unsigned score
waves in each of the KTF/LGT groups. No unsupported embedded wave encoding was
found. Two standalone MIDI headers in the KTF group need usage classification;
the load path is SMAF-only.

#### Sounds without audible events

A follow-up scan reproduced all 36 carrier-local records (35 distinct byte
strings). An independent size-aware cursor read all 38 of their sequences
through EOS with nothing left over; none is empty or truncated, and every
dialect parses. What they lack is something the current decoder can turn into
sound:

| Why nothing sounds | KTF | LGT | SKT | Total |
| --- | ---: | ---: | ---: | ---: |
| HPS controls and Yamaha SysEx only, no note or wave trigger | 11 | 0 | 1 | 12 |
| Mobile stream note selects a wave the file does not carry | 10 | 7 | 6 | 23 |
| Mobile stream note in a file with no embedded wave | 0 | 1 | 0 | 1 |

Each of the 23 carries one supported mono ADPCM wave numbered 1, and its
note-zero triggers sit on channel 9 (21 files) or 15 (two files), which the
current channel-plus-one mapping turns into waves 10 and 16. That explains the
silence without establishing the intended assignment: page 43 of the
[SMAF specification](https://img.atwiki.jp/mmfuta/attach/19/78/SMAF3.05.pdf#page=43)
leaves assigning stream waves to the implementation, and a fallback to the only
wave would be a guess at that contract. The single LGT record has no wave to
fall back to. The twelve HPS files hold device commands (`43 03 90 Bn`/`Cn`
headers) whose meaning is not inferred from their bytes. The original runtime
or SDK is the evidence still needed for all three groups.

Classifying them exposed the [HPS exclusive framing](../audio.md#hps-exclusive-messages)
defect. Across 2,721 HPS files with 7,763 sequences, an independent cursor
found 27,639 sized sequence messages, 82 of them with an `F7` inside the device
data (KTF 51, LGT 18, SKT 13). Reading to that `F7` invented 15,360–230,400
duration units in 30 sequences across 27 records (KTF 18, LGT 3, SKT 6): one to
fifteen minutes at their 4 ms timebase, with note, control and SysEx counts
unchanged. Every HPS setup chunk starts with `FF F0`. In 2,644 the declared
sizes end exactly at the chunk end; in the other 77 (KTF 60, LGT 13, SKT 4) the
first message's last declared byte is `7F` where `F7` belongs, so the repaired
reader emits nothing from that chunk, as before. Whether `7F` is a vendor
terminator is unverified.

After the repair, the same 11,175 records keep every note, wave and PCM size;
2,676 change output, each explained by 16,115 added setup messages (KTF 10,859,
LGT 1,327, SKT 3,929), sequence messages without their size byte, or removed
delays, and the decoded timelines match the independent cursor in every HPS
sequence. The repair does not make any of the 36 audible. Anonymous scans,
the frozen pre-repair decoder and the comparison are under
`build/sound-empty-classification/`; no game asset was copied or changed.

### Execution and validation

Six fresh host sessions ran at normal speed with in-memory saves, twelve-second
windows and three short confirm-key presses. Archive SHA-256 prefixes identify
the local inputs without naming games:

| Group | Archive prefix | MIDI starts | PCM starts |
| --- | --- | ---: | ---: |
| LGT | `87b04639cdfef6cc` | 9 | 1 |
| LGT | `3fd665ab33be4555` | 0 | 0 |
| LGT | `ea94cfa48b214c55` | 0 | 0 |
| SKT | `8bf2f80c0723fd97` | 307 | 0 |
| SKT | `e3276ce8557c1d26` | 0 | 5 |
| SKT | `14a62a8521a0d434` | 160 | 1 |

All advanced frames without a start/tick/input error. The two silent LGT scenes
are inconclusive. These are host-output checks, not Android load, mixed-output
or listening tests. No real SGS scene was exercised.

Focused sound/media Go checks produced 79 passing test/subtest results and two
skipped opt-in corpus checks. The separate scan above supplied corpus coverage.
The focused web audio/settings/session/frame suite passed 86 tests. Both browser
engines again passed the repaired twelve-note rendering check on a simulated
90 ms clock. Nine local Go diagnostic tests, including four additional volume
subcases, reproduced the remaining defects above. Diagnostic sources and results
are ignored under `build/sound-audit/`; their asserted outcomes describe current
defects and are not permanent acceptance tests.

Full debug/release, race, vet and Android build/install validation for the earlier
repair is recorded in [APK audio timing](#apk-audio-timing). No new product code was
changed here. Subsequent ownership/control repairs must cover simultaneous
clips, stop/close, loop boundaries, reconnect and checkpoint restore at both
ends of the host protocol.

## APK audio timing

### Report and scope

On 2026-10-08, a user reported smeared music in the Android app while PC and
mobile-browser playback sounded normal. They confirmed the KTF variant and
estimated the app version as 0.5.0. The phone model, Android/WebView versions,
output device and exact comparison scene remain unknown.

The APK uses the same oscillator-based synthesizer as the web page. Changing
instrument banks does not address the timing defect reproduced below. The fix
uses no external sound assets or synthesis code.

### Reproduced defect

The published 0.5.0 APK and a build from `bf716b94` were exercised in a disposable
arm64 Android emulator using the same local KTF archive (SHA-256 prefix
`1379b56de66e`), isolated saves, normal speed and default volume. The emulator
ran Android 37.1 with WebView `153.0.8010.36`; the comparison used desktop
Chromium. This emulator does not advertise Android's low-latency audio feature.

Both contexts used 48 kHz. Desktop `baseLatency` was 5.33 ms; WebView reported
90.83 ms and its `currentTime` usually advanced by 90.67 ms. In the old client,
notes started and stopped at the current render-clock reading. Distinct arrivals
within one large output block therefore acquired the same scheduled time.
For the measured opening music, the mean absolute difference between received
MIDI gate duration and render-clock duration was 30.67 ms in the 0.5.0 APK,
35.34 ms in the current baseline APK, and 1.75 ms in desktop Chromium. These
numbers measure client scheduling, not fidelity to the original handset score.
No outbound audio shedding or long page tasks were recorded in those APK runs.

After the repair, a separate run measured actual oscillator start/stop times:
186 melodic releases had a mean gate-duration error of 1.61 ms relative to
arrival spacing. The maximum was 100.53 ms, so this is a reduction in timing
error, not a guarantee against an output stall. Excluding percussion from the
old measurements gives means of 31.67 ms for 0.5.0 and 36.79 ms for the current
baseline APK. Capture lengths and note counts differ between runs.

Release handling had a second defect: it canceled future gain automation and
read `AudioParam.value` to choose the release level. That value describes the
render clock, which may not have reached the scheduled attack. It could cut an
attack or use the initial gain of 1 instead of the note's intended level.
A newly authored sequence of twelve short sine notes, captured directly from
WebView's mixed PCM through an AudioWorklet, reached a peak of 0.649 in 0.5.0.
The repaired output peaked at 0.109, consistent with the requested velocity,
channel volume and stereo pan. An idle capture retained all twelve pulses.
With a 10 ms RMS window and a 0.003 activity threshold, their measured active
lengths were 60–70 ms after repair, compared with 40–120 ms in the 0.5.0 capture.
These include the release tail; the JavaScript timer used to request 35 ms
gates is itself subject to delay.
The capture is before Android's speaker/output processing and is not a listening
test on the reporter's phone.

The Web Audio specification describes `currentTime` as the next render block's
time, with scheduled operations using that timeline. See the
[Web Audio specification](https://www.w3.org/TR/webaudio-1.1/#dom-baseaudiocontext-currenttime)
and [audio scheduling guidance](https://web.dev/articles/audio-scheduling).

### Repair

`web/audio.js` preserves event arrival spacing with the monotonic page clock,
anchored ahead of the audio render clock. Melody, percussion, pitch bends and
sampled effects share this mapping. It adds one reported output buffer of lead,
with a 10 ms floor and 250 ms cap. An additional 100 ms drift allowance is
bounded; corrections trim excess drift without jumping a release back before
its attack. Context state changes and timeline replacement clear the anchor.
Stopping a game or loading a checkpoint still cancels queued sources immediately.

Melodic release reconstructs the scheduled attack/decay level and retains the
partial ramp before fading out. It does not read a stale instantaneous gain.
The waveform families, percussion model, volume defaults and server protocol
are unchanged. The service-worker shell version advances so cached clients
receive the repair.

### Validation and remaining limits

`web/audio-timing.test.mjs` covers coarse-clock spacing, short-note attack and
decay, shared PCM/percussion/bend timing, drift correction, stalls and reset.
The existing audio tests cover activation, interrupted/stalled contexts,
percussion retriggers and stopping all pending sources.

The browser check renders twelve 35 ms notes against a simulated 90 ms clock
using the browser's real OfflineAudioContext. Every note must have an audible
body and a silent following rest. Run against a server serving the repaired
client:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-timing.mjs http://127.0.0.1:11541 chromium
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-timing.mjs http://127.0.0.1:11541 webkit
```

The repaired release APK was also installed and exercised with the local KTF
archive and an authored PCM capture. This establishes a device-dependent
scheduling defect and its repair. It does not establish that every difference
described by the original report has the same cause. Physical-phone comparison
with the reporter's scene, matching volumes and output route, remains needed.

Validation passed on 2026-10-08: `make test` (including 278 Node tests),
`make test-debug`, `go test -race ./internal/...`, `go vet ./...`, the Chromium
and WebKit audio-render checks above, and the Android release build/install.

This change preserves spacing between arrivals, not the score timestamps lost
before transport. Events already collected into one server batch, network
stalls, dropped audio batches and actual device underruns remain separate limits.
The scheduling lead also increases response latency, particularly on an output
with a large buffer. Richer instrument synthesis remains separate work.
