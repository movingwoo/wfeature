# Sound

The games ship their music as SMAF (`.mmf`), Yamaha's format for the MA sound
chips in 2000s handsets. Most titles package their sounds inside the game's JAR
rather than beside it, so the opt-in acceptance probe descends into nested
archives under `var/games/ktf` and plays every SMAF file it finds:

```sh
WFEATURE_SMAF_ACCEPTANCE=1 go test ./internal/audio/smaf
```

On 2026-10-09 the 38 local KTF archives held 895 SMAF files, which decoded to
546,127 events, 243,718 notes and 499 waves without a file being refused. The
wider three-carrier scan and the decoder corrections it led to are in the
[corpus audit](history/audio.md#corpus-evidence).

## What a SMAF file is

A file is a list of tagged chunks. The ones that matter are score tracks
(`MTRx`) and PCM audio tracks (`ATRx`); a score track is itself a list of
chunks holding setup sysex, a sequence, and any wave data the sequence
triggers. The sequence is a byte stream of duration-prefixed events in one of
three dialects, which are distinct encodings rather than variations:

- **Mobile standard** is MIDI-shaped — MIDI status bytes, MIDI variable length
  quantities — either stored plainly or Huffman coded.
- **Handset standard** packs a note's channel, octave, and pitch into a single
  status byte, and its variable length quantity is not MIDI's: the second byte
  contributes all eight bits and the first is biased by one. Common controller
  values are abbreviated into the event type itself.
- **Softbank** uses the handset event layout, but its sized exclusive messages
  do not have to end in the handset format's final `0xf7`.

`internal/audio/smaf` parses all three. A chunk that does not parse ends the
chunk list rather than failing the file, because these files come out of game
archives and their tails are often padding.

### HPS exclusive messages

An exclusive message in a handset (HPS) sequence is `FF F0`, a one-byte size,
then that many bytes: maker ID, format ID, device data and a final `F7`. The
size is a plain byte, not a MIDI variable length quantity, and an `F7` inside
the device data does not end the message. An HPS setup chunk (`Mtsu`) holds the
same messages back to back without durations. These are pages 38 and 28 of the
[SMAF 3.05 specification](https://img.atwiki.jp/mmfuta/attach/19/78/SMAF3.05.pdf#page=38);
a Mobile setup chunk keeps its own `F0` plus MIDI-length framing.

The sequence and setup readers both read the declared size, keep the payload
whole including its final `F7`, and emit the usual `F0`-framed event without the
size byte. A message that is truncated or whose last declared byte is not `F7`
ends that sequence or setup chunk and keeps everything before it, like any other
malformed tail; no lengthless form is guessed. Setup messages are charged to the
same event and SysEx budgets as the rest of the file. Softbank keeps its
previous reading.

The decoder formerly read an HPS message up to the first `F7`. It kept the size
byte as the maker ID and, when the device data held an `F7`, read the rest of
the message as sequence records whose durations added silence: 27 sounds in the
three-carrier scan delayed everything after such a message by one to fifteen
minutes, with the same notes in the same order. The setup reader knew only the
Mobile framing and dropped every HPS setup message.

The page configures no device from SysEx, so the audible effect of the repair
is timing. Checkpoints hold the event lists already decoded for loaded sounds:
a restored sound keeps its old list, only newly loaded sounds use this reading,
and no checkpoint field changed. The corpus measurements and the silent files
that led here are in the [audit](history/audio.md#sounds-without-audible-events).

### Mobile note velocity memory

A mobile note without a velocity operand uses the channel's last explicit
velocity, initially 64. An explicit stream-wave note updates this memory too,
including an absent or unsupported wave. CC121 resets that channel's memory to
64; other channels, banks and programs retain their state. Explicit zero stays
zero. This follows the note-message contract on page 43 of the
[SMAF 3.05 specification](https://img.atwiki.jp/mmfuta/attach/19/78/SMAF3.05.pdf#page=43).

The stream-wave mapping and its raw PCM gain are unchanged; stream velocity and
gate behavior remain [contract work](history/audio.md#pcm-contract-evidence).
Restored checkpoints keep their already decoded event lists and only newly
loaded sounds use this rule. It adds no saved field; the PCM extension below
versions its own state.

### A predictor that walked away from the signal

Almost every sampled sound in these archives is 4-bit Yamaha ADPCM (the
exceptions are in the next section). Until 2026-09-04 the decoder had two
errors that a single sound hides and nine thousand samples do not. ADPCM stores
each sample as a step from the one before it, so an error in one sample
persists in every later sample, and an error in the step size persists in every
later step. Such a bug is invisible in a unit test and sounds like a broken
speaker in a game.

- **The step table was carried in 64ths.** Its five distinct ratios are 230,
  307, 409, 512 and 614 parts in 256; as 57, 77, 102, 128 and 153 parts in 64
  they are the same ratios at a quarter of the resolution, and four of the five
  round the wrong way, 0.4% per sample.
- **The two samples in a byte were read in the wrong order.** The low nibble is
  the earlier one.

Under the old rules, seventeen sampled sounds across two KTF archives each
ended thousands of counts away from where they began; one ran from about
-9,000 to +25,000 and spent part of its length pinned to a rail, and their means
sat at -13,000, +15,000 and -19,000. Under the corrected rules each of the
seventeen has a mean inside ±120 and ends within a few hundred counts of zero.
Over a whole run, one title's captured wave went from a mean of -14,216 with 35
samples at the negative rail to a mean of -58 with none.

The audible effect was a click at the start of every sound and a crackle
through it, reported as "the sound pops"; a title whose effects are all sampled
popped on every one. The fix keeps the same multiply, shifts by eight instead
of six, and swaps the two nibbles (`internal/audio/smaf/adpcm.go`).

### The two wave forms that are not compressed at all

A score track can attach a sample to a channel and trigger it with note zero.
Until 2026-09-04 the gate in front of that admitted only 4-bit Yamaha ADPCM and
dropped everything else silently, which is the right default for a format with
no known reading and the wrong one for a format that is plain bytes.

A scan of every SMAF file in the local archives (9,065 files from 557 packages)
found 5,789 attached waves in ADPCM and four that were not. All four are 8-bit
offset-binary mono at 8 kHz, the four songs of one rhythm title, two of which
trigger their wave 138 and 114 times; the melody played and the sample the beat
is built on did not.

The format field and the bytes both say offset binary: the wave's lead-in is
`0x80` bytes, its silence, and read as offset binary the sample has a mean of
-115 with a symmetric range, where two's complement puts it at -9,221 against a
rail. Both 8-bit forms (signed and unsigned) are read. Sixteen-bit and stereo
waves stay unplayed rather than guessed at: no local archive carries one, so
there is nothing to check a reading against, and a wave played wrong is worse
than a wave not played.

### Live PCM track controls

Supported mono ADPCM `ATRx` tracks carry volume, expression and pan through the
shared runtime and the page. Each track gets four independent logical channels
in file order, beginning at 1; duplicate chunk tags do not merge their state.
Channel zero holds ungrouped score-attached and legacy waves. MIDI controllers
are a separate domain. Controls at the same score time precede wave starts, and
later controls affect every still-playing wave in their group, including a tail
left by an earlier repeat. Wave samples, duration and sampling rate are
unchanged; controls follow presentation deadlines across speed changes.

The [SMAF contract](history/audio.md#pcm-contract-evidence) defines values 0–127,
expression initially 127 and pan initially 64. PCM amplitude is
`(volume/127)^2 * (expression/127)^2`; left/right pan gains are
`cos(pi*pan/254)` and `sin(pi*pan/254)`, so center 64 yields approximately
0.702720 and 0.711466 rather than mono at unity on both speakers. Initial volume
is unspecified; this implementation uses neutral 127 until an explicit volume
arrives and records whether one was set. That is a compatibility policy, not a
claimed handset default. Out-of-range decoded controls are ignored without
dropping their elapsed time; invalid authored events are refused. The `0x36`
expression alias is a compatibility extension; `0x3b` is the documented opcode.

The page routes each mono source through independent left/right gains and a
two-input channel merger, followed by the owner's guest gain and the user's wave
slider, after the existing per-source 0.8 gain. Live control targets ramp over
5 ms, a receiver smoothing policy rather than a SMAF requirement. Controls
before a wave set its initial level. Shared sample buffers stay raw and
cacheable across owners and groups; the graph is separate from cached content.
The last completed source releases its group nodes while keeping controller
state. Explicit stop, close or restart clears that owner's state; pause freezes
it, and natural completion or repeat keeps it. A scheduled stop keeps its
retiring graph connected until the old sources end, independently of a new run.

`backend.AudioPCMSink.PCMChannels()` reports current downstream support in
addition to `OwnedAudioSink`. Pages negotiate `pcm=1` with `sound=owned` or
`sound=resume`, and reconnect updates the capability before reconstructing
output. An incapable sink receives a stereo copy with the current gain/pan
applied at wave onset and cannot change samples already emitted; device/clip
gain is applied once afterward. The retained timeline and samples are identical
for both paths. The [wire extension](session.md#protocol-2-sound) keeps older
operation sizes.

<a id="audio-checkpoint-versions"></a>

Audio checkpoints save raw routed waves and sorted, independent PCM states,
including unset volume; reconstruction sends current controls before trimmed
sample tails. The current audio state version is 9
(`audioStateVersion` in `internal/backend/audio_state.go`). Strict readers
accept four shapes:

| Version | Shape |
| --- | --- |
| 6 | Oldest accepted: [owned output](audio-ownership.md#runtime-and-host-contract), [MIDI state](#midi-sustain-pitch-range-and-reset), no PCM control/routing fields |
| 7 | Adds the PCM control and routing fields, required |
| 8 | Adds each wave's original admission charge |
| 9 | Adds each wave's phase within its first remaining sample frame |

Mixed shapes, unrelated missing fields, duplicates, unknown fields and
unreserved groups are refused before adoption. Reading v6 does not invent
routing. Versions 6 and 7 derive an initial charge from their saved sample
tails, because the original pre-trim size cannot be recovered; versions 6–8
adopt phase zero for the same reason. The next capture writes v9. Versions
below 6 are refused. Older binaries refuse v9, so rollback requires a
pre-upgrade checkpoint or a normal start; checkpoint files and ordinary saves
are left in place. Debug and release use the same format.

Native browser rendering of the PCM controls
[passes](testing.md#native-audio-rendering-acceptance); device listening is
outstanding. Bend, gate, looping, retrigger and stream-velocity rules are open
(see [Deliberately incomplete](#deliberately-incomplete)).

### Decoder resource limits

One decode shares a budget across every track, sequence and nested chunk.
The limits are receiver policy, not restrictions claimed by the SMAF format:

| Resource | Per-file limit |
| --- | --- |
| Input bytes | 64 MiB |
| Sequence bytes, after Huffman expansion | 8 MiB total |
| Sequence records, including ignored/discarded parsing work | 262,144 |
| Top-level and nested chunks, including unknown chunks | 4,096 |
| Generated MIDI/PCM events, including layers and track ends | 1,048,576 |
| Decoded PCM bytes, counted again for every trigger | 64 MiB |
| Framed output SysEx bytes | 8 MiB |

Huffman lengths are checked before allocation or conversion to `int`. PCM is
charged before expansion, including repeated references to the same wave.
Replacing a sequence or wave chunk does not refund parsing work. Standalone
`DecodeADPCM` also refuses an output above the PCM byte limit. Each new decode
starts with its own budget; the input remains unchanged.

A hard limit rejects the whole file, even when earlier tracks were valid.
`Parse` and `Decode` return an error wrapping `ErrResourceLimit`; `Play` keeps
its compatibility behavior of returning no events on failure. `backend.Audio.Load`
uses `Decode`, so callers keep the diagnostic cause without allocating a handle
or disturbing another loaded sound. Ordinary malformed tails still keep the
readable prefix. Length quantities cannot wrap their integers, and generated
event times, note gates and extra layer tails must fit unsigned 32-bit
milliseconds, so a distant note-off cannot wrap before its note-on.

When the limits were introduced, all 11,175 carrier-local unique sounds kept
every ordered event and PCM/SysEx payload hash. Boundary and fuzz evidence,
measured maxima and archive-read exclusions are in the
[audit](history/audio.md#bounded-decoding) and
[test guide](testing.md#smaf-decoder-bounds).

### Loaded-sound resources

All retained sounds together are limited to 1,048,576 event and reserved
active-key/PCM-channel entries and 128 MiB of logical payload; these are the
checkpoint limits, enforced at admission. Each sound costs 128 bytes, each event
64 bytes and each possible held key 8 bytes, plus two bytes per PCM sample and
one per SysEx byte. Each distinct PCM group reserves one entry and 16 logical
bytes, with at most 2,048 groups across all loaded sounds, including
control-only groups. Repeated payload references are charged each time. These
accounting widths bound payload retention; they do not measure the Go
allocator, the browser graph or physical device memory.

Admission reserves the distinct positive note-on keys in the sound, plus any
held keys retained in a restored record. A sound has at most 2,048 MIDI keys.
Retriggering a key replaces its held-key entry, matching the output and page,
and velocity-zero note-on releases it, so an unbalanced repeating authored
stream cannot grow an unbounded list. Held-key bookkeeping is distinct from the
24 audible voices and from keys released under sustain.

`Audio.Load`, `LoadEvents` and `PlayTransient` reject excessive combined cost
with `ErrAudioResourceLimit` before allocating a handle or changing output.
`LoadEvents` and `PlayTransient` copy events and nested PCM/SysEx buffers after
acceptance, so a small borrowed slice cannot retain a large caller allocation
and the caller may reuse its buffers after return. Decoded files already own
their buffers. Event ordering and MIDI note bounds are validated at the same
boundary.

Per-sound costs are immutable; admission sums at most 256 cached costs rather
than rescanning old buffers. Close and transient reclamation release capacity;
reusable stop, completion and pause keep it. Checkpoints recompute costs before
copying and validate active-key bounds and uniqueness, without a schema change.
Because future active keys are reserved, a slot accepted before this rule near
the limit can be refused. Ordinary save files and debug/release formats are
unchanged. [Verification](testing.md#loaded-audio-admission) covers limits,
ownership, restore and concurrent callers.

### Checkpoint catch-up work

KTF, LGT and both SKT runtimes call `backend.ValidateAudioCatchup` before
restoring a timeline. It validates the saved audio shape and bounds the work
due from each saved cursor: at most 1,048,576 event visits, 128 MiB of emitted
PCM/SysEx references and 1,048,576 held-key comparisons/copies, across all
sounds together. Controlled mono PCM is charged at four bytes per sample to
cover the stereo fallback. Consumed and future events do not count, so a long
score whose earlier notes were already consumed is not refused while excessive
first-tick work still is.

The calculation follows finite remaining passes, time-zero events at repeat
boundaries and the frozen pause clock. It keeps held-key order to count
positive note-on searches, note-off/zero-velocity removal and final silence;
controls do not clear logical held keys. Each repeat consumes a nonempty score,
so the validation itself is bounded. It neither advances live playback nor
emits or copies output payloads. The event and retention bounds still apply to
the entire saved score, and the checkpoint format is unchanged. See the
[checkpoint verification](testing.md#checkpoint-display-and-audio-catch-up).

## Why translating to MIDI is not a relabelling

`tonemap.go` solves three problems:

**Channels.** A SMAF track addresses four channels and a file holds several, so
a file can name more than sixteen logical channels. Melodic ones are allocated
real MIDI channels on first use, in an order that skips channel 10; rhythm ones
all go to channel 10. Whether a channel is rhythm must be known before its
first event, and the bank select that says so can arrive after it, so
`preclassify` walks the sequence once before playing it.

**Programs.** SMAF programs are Yamaha MA voices. Bank `0x7d` means "drum kit",
and a handful of MA voices have General MIDI stand-ins that sound closer than
the raw program number. The MA ambience voice has no single equivalent, so it
is played as three layers: the note itself plus two supporting voices, detuned
and panned apart, released later than the note that triggered them.

**Scales.** The handset dialect keeps volume and expression as separate values
that MIDI folds into one controller, its notes sit three octaves below MIDI's,
and its rhythm channels carry the drum key in the program rather than the note.
A hit's loudness is derived from volume and expression, because a handset
rhythm channel has no velocity.

`smaf_test.go` is the reference implementation's own test suite carried over.
A port that only proves it does not crash on real files would not catch a
dialect decoded plausibly but wrongly; each carried-over case pins one such
reading.

## The Host boundary

### Global score order

`Audio.Advance` merges all active scores by absolute guest deadline before
applying the shared 24-note output budget. Equal deadlines use ascending sound
handle, then original event order within that score. Repeated passes rejoin
that merge at their original boundaries; a coarse tick cannot drain one
owner's whole backlog ahead of an earlier peer event. The pending heap holds
at most one event per loaded sound and compares only deadlines already proven
due, so a future offset near the duration limit cannot wrap into the past.

`Playback` and `Pause` also advance all due scores. `Resume` merges due peers
before admitting its retained voices, so the resumed voice is newer than those
earlier attacks. `PlaybackState` observes progress without advancing; platform
services use one global advance followed by these observations for all clip
completion notifications. Paused owners keep their frozen position; completed
transients are reclaimed during the merge. Handles, score cursors and existing
checkpoint formats keep the same ordering after restoration.

SKT serializes each Player's clock read, progress reconciliation and dependent
transition with raw clips, tones and Host advancement. Its lock order is
Player, audio timeline, then notification registry; Host advancement releases
the timeline before taking Player locks. SKT listener replacement observes a
completed score with its old recipient before installing the new listener. KTF
and LGT reconcile all Java clip completions at native transition boundaries,
including LGT C pause/resume, so a peer's newly completed score cannot leave
its callback watermark or active GC root stale at quick save.

This preserves score emission and voice-admission order; the optional timed
output boundary below also keeps the intervals between those events.

### Presentation timestamps and recovery

`TimedAudioSink.AudioTime` selects presentation seconds for subsequent sink
operations, including MIDI, PCM, gain, cancellation and resumed voices. A score
event uses its absolute guest deadline; a command uses its synchronized guest
instant. `SetPlaybackRate` drains the old rate before rebasing future
intervals and changes neither pitch nor PCM sampling rate. All five execution
paths (KTF Java/C, the earlier KTF native package, LGT, SKT Java/SKVM and SGS)
initialize this mapping before playback and restore it at the saved guest
instant (`RebasePlaybackClock`), without changing the portable save format.
Restored overdue deadlines may be negative relative to the new output epoch's
zero.

Retained output follows presentation elapsed time, with unscaled Host elapsed
time between service boundaries and a monotonic Host floor when execution falls
behind. Historical events already carry an envelope/sample age at emission. A
virtual guest jump therefore preserves drum expiry and PCM position under fine
or coarse servicing, and a slow guest cannot make expired output young again.
Detached checkpoint validation is excluded at activation.

The page negotiates `timing=1` alongside `sound=resume`. Each batch carries
timed operations and a final authoritative clock (the frontier). A silent tick
sends its frontier alone at most every 100 ms after the last sound message, and
at once after a longer gap, so the first frontier after a server stall still
moves the anchor as described below. A frontier that finds the send queue full
is skipped rather than shed, because a shed batch asks for a reconstruction and
this one lost nothing. The interval and its measurements are in the
[frontier cadence record](history/audio.md#frontier-cadence). Cached pages keep
their previous wire operations. The page schedules accepted batches on Web
Audio with a 100–250 ms lead (from reported latency), a 350 ms maximum future
horizon and 5 ms late tolerance, and validates the whole batch before
scheduling any source. These are playout bounds, not a claim about handset
latency; see the rendering acceptance in the testing guide.

An out-of-window, backward or malformed batch requests `audioResume` and
suppresses further old output until a reset and current-state reconstruction
arrive. Two late cases move the anchor instead, because a reconstruction cuts
every sounding voice to replay it:

- A late batch that holds only its frontier lost nothing. The anchor moves to
  place that frontier one lead ahead of the render clock; everything already
  scheduled is behind the render clock by then, so the move cannot reorder it.
  Live sessions found two thirds of late refusals were such batches, mostly from
  a title whose server ticks stall
  ([measurements](testing.md#sustained-sound-load-in-real-titles)).
- A batch with events at most 200 ms late (`MAX_REANCHOR_LATE`) moves the
  anchor to its first event one lead ahead, as long as its last event still fits
  the 350 ms horizon; its events keep their spacing and play that much later. A
  page on a slowed processor (Chromium at a quarter speed on a busy machine,
  standing in for a slower phone) delivered batches 15–100 ms late and was
  rebuilt fourteen times in nine seconds when every late batch asked for the
  current output; 0.5.2, which had no lateness check, played them late and
  rebuilt nothing.

A batch later than 200 ms still asks, because the output it would continue is
by then further from the truth than a rebuild. The request is negotiated and
output-epoch checked. Definitions are invalidated too, so a malformed first
definition cannot lose subsequent PCM. Interruption waits for a running context
before requesting fresh output; quick load clears an outstanding recovery
request with the old epoch.

Untimed events, from a server that does not send timestamps, keep their arrival
spacing on the monotonic page clock (`eventTime` in `web/audio.js`), anchored
one reported output buffer (`baseLatency`, floor 10 ms, cap 250 ms) ahead of
the render clock. Drift beyond a further 100 ms is trimmed without moving a
release before its attack, and context state changes or timeline replacement
clear the anchor. Some Android outputs advance `currentTime` in blocks of about
90 ms, and scheduling at that reading collapsed distinct notes onto one
boundary ([APK audio timing](history/audio.md#apk-audio-timing)).

Timed clip stops retire the old source graph at their deadline. A restart gets
fresh channel/gain nodes, while old nodes stay connected until their last source
ends. Global reset cancels both current and retired sources immediately.
Expired percussion leaves voice admission before the next authored attack, even
if its browser `onended` notification has not run yet. The page schedules a
bounded time window directly; there is no unbounded JavaScript playback queue,
and the receiver bounds retained sources and PCM references as described below.
Native cost up to those bounds, and the load and mix level real titles reach,
are measured in the testing guide's
[native cost](testing.md#native-cost-of-the-largest-sound-loads) and
[sustained load](testing.md#sustained-sound-load-in-real-titles) sections.

### Playout lateness diagnostics

Debug page reports include `audio playout timing` aggregates at the existing
session-statistics cadence and immediately before saving the page report.
Each event's lateness is `max(0, currentTime - mappedAt) * 1000` milliseconds,
sampled when the complete batch reaches the scheduling-window check. The
`anchored` group covers first and reset anchors; `continuing` covers an existing
anchor. Keeping them separate prevents a new anchor's intentional lead from
being read as initial delivery latency.

Each group counts checked and admitted batches, clock-only batches, late
batches that moved the anchor (`reanchored`: clock-only or at most 200 ms late)
and the first window-refusal reason (`late`, `future`, or negative/nonfinite
time `range`). Every non-clock event in a checked batch contributes, including
events after the first refusal, to one bucket: not late, up to 5 ms, over 5
through 20 ms, over 20 through 100 ms, and over 100 ms. Invalid mapped times
and unrepresentable millisecond differences count separately. Event and
frontier maximum lateness are distinct; the final clock never inflates event
counts. Legacy, malformed, backward, resource-refused and
unavailable/suspended-context batches do not reach this measurement, and
admission does not imply audible playback.

These are render-deadline observations after binary decoding but before JSON
PCM conversion and source allocation, not network transit, dispatch cost or
speaker latency; coarse `currentTime` values keep their precision. Host
collector overflow, outbound audio drops and reconstruction keep their own log
messages. The fixed counters retain no events or samples, survive immediate
output recovery, and reset on report flush or explicit log clear. Release
disables the recorder through the page-log capture boundary. The diagnostics
add no timer, request, protocol field or persistent state. Device
distributions require a debug run on that device; authored verification is in
the [testing guide](testing.md#playout-lateness-diagnostics).

### Live source bounds

The page accepts at most 512 retained Web Audio sources (`MAX_AUDIO_SOURCES`)
and 128 MiB of logical Float32 PCM payload across live wave references
(`MAX_LIVE_WAVE_BYTES`). Scheduled starts, melodic release tails, percussion and
retired clip generations all count until their sources end or are disconnected
by an immediate stop or reset. The separate 24-note admission rule still selects
held MIDI keys. Every PCM reference is charged, including references sharing
one cached buffer; these are conservative accounting limits, not measured
native memory.

A complete batch is checked before changing controllers, scheduling nodes or
resetting the old output, and JSON PCM is sized from its encoded length before
decoding. The same limits protect direct source entry points. A timed refusal
uses the `audioResume` recovery and suppresses stale output until a current
reconstruction arrives. An older untimed server has no such handshake: the page
drops the refused batch and can accept the next one. Preflight credits
immediate owner stops inside an untimed batch, so a legacy stop/restart can
reuse that owner's capacity without freeing peers. Scheduled stops keep their
charges because the sources can still be audible. Global reset checks its
replacement output against empty budgets before clearing old output.

The largest supported reconstruction fits: 256 retained PCM waves plus 24 MIDI
notes, and 32 MiB of raw signed-16-bit PCM expanded to stereo for an older
sender and converted to Float32 is at most 128 MiB. The source ceiling leaves
room for transient tails. Backend [PCM admission](#pcm-admission-and-restoration)
shares the retained-output budget, but the receiver still needs its own bound:
MIDI release tails, sequential short waves and retired graphs can stay
connected until browser callbacks run. These policies do not establish handset
voice-stealing behavior. They bound memory and recovery rather than real-time
cost: on the development machine Chromium's audio thread stops keeping up
between 128 and 256 simultaneous waves, while real titles stay far below either
bound ([measurements](testing.md#native-cost-of-the-largest-sound-loads)).

### PCM admission and restoration

The runtime admits at most 256 PCM references with 32 MiB of original raw
signed-16-bit sample charges (`maxOutputWaves`, `maxOutputBytes`), shared by
audible and paused owners. It processes waves in global authored
deadline/owner/event order and prunes expired samples at that deadline. When
either limit is full, it consumes the newest wave event without retaining or
emitting it; existing sources and peers stay unchanged, and the refused event
is never retried by pause/resume, recovery or quick load. MIDI, controllers,
score cursors, repeats and completion continue normally. This is an emulator
resource policy, not claimed handset polyphony.

Every emitted wave has a complete reconstruction record, so overload cannot
make output capture or pause fail because of untracked PCM. Pause keeps its
reserved capacity, so resume cannot overfill the budget. Natural expiry and
explicit stop/restart/rewind/close release the affected charges; natural score
completion leaves a sample's tail running.

`AudioWaveState.BudgetBytes` records the admitted raw byte count. Capture can
trim heard frames while keeping that charge, so a partially elapsed sample does
not gain capacity through quick load. Validation requires a whole-frame charge
at least as large as the remaining samples, under the same aggregate limit.
`AudioWaveState.FramePhase` keeps the consumed fraction of the first remaining
frame as an integer from zero through 999,999,999, in billionths of a frame.
With `N` remaining frames, sample rate `R` and phase `p`, remaining nanoseconds
are `floor((N*1e9-p)/R)`. Advancing `a` nanoseconds consumes
`floor((p+a*R)/1e9)` frames and leaves `(p+a*R)%1e9` phase. This keeps future
frame selection and the floored expiry exact through repeated captures,
including rates that do not divide one billion. Expiry is checked before
multiplication; the admission bound then keeps the arithmetic in range.

An optional `AudioWaveResumeSink` receives the trimmed wave and frame phase
after PCM-group and guest-gain fallback. The web Host negotiates `phase=1` with
an owned page and carries phase separately from presentation time, because
backdating a wave would break event ordering. The page starts the same cached
buffer at `phase/(1e9*rate)` seconds and shortens its natural end accordingly;
the [Web Audio playback contract](https://www.w3.org/TR/webaudio/#playback-AudioBufferSourceNode)
permits sub-sample offsets. This preserves the requested playhead position, not
an earlier renderer's hidden interpolation history. Legacy sinks and pages keep
whole-frame replay; raw sample bytes and admission charges are unchanged.

Hosts attach their logger with `Audio.SetLogger`, including restored timelines
and startup before a sink is present. PCM refusals log the reason, owner,
retained size, incoming size and cumulative count at powers of two. The debug
profile exposes these bounded diagnostics and release filters them. Diagnostics
are not portable state, and changing sinks does not reset them.

### Decoded PCM buffer reuse

Protocol 2 definitions share immutable decoded samples between triggers. The
page marks those local events `cacheable` and reuses their Web Audio buffers;
the wire format is unchanged. Direct mutable sample arrays and JSON events take
fresh snapshots. Sample identity, channel count, frame count and sampling rate
must match; a changed format replaces the previous cached variant. Definition
replacement, forget and reconnect cannot confuse reused numeric IDs.

The cache keeps at most 256 buffers and 16 MiB of float sample payload
(`MAX_WAVE_BUFFERS`, `MAX_WAVE_BUFFER_BYTES`), evicting the least recently used
entry first. The byte ceiling matches the wire's 8 MiB definition budget after
conversion from signed 16-bit to Float32; the entry ceiling follows the
backend's 256 retained-wave bound but does not limit live sources. Larger waves
play without caching. Weak sample keys avoid keeping decoded arrays alive after
their definitions disappear. Context replacement and global `allOff` clear the
cache; stopping one clip keeps reusable payloads.

Each trigger still creates a source and gain with independent owner, start,
stop and playback position, and eviction does not stop an active source using
that buffer. The ceilings bound cache retention, not total browser memory:
active sources may retain evicted buffers under the separate live-reference
bound. The [allocation probe](testing.md#pcm-buffer-reuse-and-mobile-velocity)
counts API requests and copies; native memory, CPU and rendered output are
separate measurements.

### Guest clock rate changes

KTF Java/C keeps elapsed guest time when changing speed: its active and
prepared runtime anchors are rebased from one Host-clock reading before the new
rate is applied. The clock-age checkpoint representation is unchanged,
including delayed activation on another Host epoch. Paused audio keeps its
cursor and unscaled output age; future score gates advance at the new guest
rate. Host timer and worker deadlines already created keep their instants.

Integer nanoseconds can make exact fractional rebasing impossible. The origin
uses the smallest representable age that does not move the guest backwards and
never converts an out-of-range floating value to a duration. If the required
origin exceeds the duration range, both anchors and the prior rate stay
unchanged. Shared speed normalization (`backend.ClampSpeed`) maps NaN, zero and
negative values to the default rate 1 and clamps to 0.1–16, so positive
infinity becomes 16. Startup settings, public speed, input repeat and
checkpoint metadata all use the normalized rate the platform accepted,
including after a refused change.

The [audit reproduction](history/audio.md#ktf-speed-change-clock-continuity) and
[verification](testing.md#ktf-speed-change-continuity) cover the former
backward pause clock and prematurely consumed notes.

### Output sinks

`backend.AudioSink` is split into PCM and MIDI because SMAF is: percussion and
melody are note events for a synthesizer, sampled sounds are waveforms to mix,
and no Host renders both the same way.

`backend.Audio` owns loaded sounds and each one's playback position. It does
not own a clock: `Advance` is called with the guest's clock at the end of each
tick. A Host batching ticks through a manual clock therefore hears the same
sequence as a real-time Host, only faster, and a clock that jumps forward still
emits the events it skipped, because a skipped note-off would leave its note
sounding.

The web Host also implements `backend.OwnedAudioSink`, which scopes output to
loaded handles; [independent playback and recovery](audio-ownership.md) owns
that contract, the negotiated protocol and the queue/reconnect repair.

## The two Hosts

**Browser** (`web/audio.js`) synthesizes the MIDI itself from oscillators. The
alternative, an audio worklet plus a sound bank of about ten megabytes fetched
at page load, costs every phone that download and the memory, and an iOS tab
that heavy can be pushed past its memory ceiling and lose sound entirely. The
trade is deliberate: a soundfont piano sounds like a piano and an oscillator
does not, but this music was written for an FM chip with a handful of
operators, so an envelope over a waveform picked by program family lands closer
to the original than it would for most music, costs nothing to ship, and works
everywhere. Drums are a short filtered noise burst. Sampled sounds go straight
into an `AudioBuffer`. MIDI and PCM feed separate gain nodes so the page's
🎵 music and 🔊 effects sliders can trade against each other.
The sliders are Host preferences, not guest volume: they default to 50, are
remembered per browser under `wfeature:volume:midi` and `wfeature:volume:pcm`
(`web/audio-settings.js`), and are applied when the first user gesture creates
the audio graph.

**CLI** (`backend.RecordingSink`) writes rather than plays. Playing would need a
system MIDI port and an ALSA or CoreAudio backend, neither of which has a
cgo-free Go equivalent, and whether the sound came out right is a question a
recording answers better than a speaker. `runktf`, `runlgt` and `runskt` accept
`-audio out` and write `out.mid` (a type 0 file at one tick per millisecond, so
recorded times are the file's times with no tempo arithmetic) and `out.wav` for
the sampled sounds; a file with nothing to hold is not written. The run
summary reports `audio_midi_messages`, `audio_wave_samples` and the written
paths under `audio`, because a written file and a file with sound in it are
different answers. In the first end-to-end check (recorded 2026-08-15), 1500
ticks of one KTF title produced 3339 MIDI messages, 1405 note-ons across 45
program changes and 82 seconds of guest time, in a file that parses.

### A recording has to end, even though the music does not

A run stops at its tick count, rarely where a phrase ends, so notes still down
at that moment had nothing after them in the file. Four of the first five
recordings from real titles ended holding between four and eight notes, and a
player that honours the file drones them after the last event indefinitely.

The writer therefore counts what is sounding as it lays the track down and
releases the remainder at delta zero before end-of-track. A note-on with
velocity zero counts as a note-off, so notes already up are not released again.
The releases are emitted in channel and note order rather than map order,
because two runs of the same game must produce the same bytes for recordings to
be comparable. Those five recordings now end with nothing sounding, and
`internal/backend`'s recording tests parse the file the way a player would
rather than matching bytes.

## Two sound surfaces per platform, and the one that was silent

A WIPI title reaches sound through either the Java classes or the C block, at
its own choice. LGT implemented both from the start. KTF implemented the Java
classes and left the C block at accepted no-ops, although fourteen of the
thirty-three local KTF archives (2026-08-18) keep their sound there: they
created clips, filled them with SMAF and played them, and every call succeeded
without output.

The KTF C block now implements create, put data, play, pause, resume, stop,
clear, free, device and clip volume, vibrator and mute on the same
`backend.Audio` timeline the Java classes use. Its function numbers are read
from the callers rather than from the specification's print order, which this
vendor's table does not follow. The table and the argument shapes it was
recovered from are in
[the KTF history](history/ktf.md#implementation-sound-in-c-and-the-table-that-was-accepted-and-thrown-away);
the six functions bound later from a lifecycle sweep and from answering two
getters with marker values are in
[the audio history](history/audio.md#source-mutes-and-the-ktf-c-media-slots-read-from-callers).

A stub that answers success is a value the game believes. Nothing in a title's
behavior distinguishes a handset that played the sound from a runtime that
accepted and dropped it, so a whole surface can stay missing until someone
plays the game with sound on. The KTF record database showed the same failure
first.

The earlier KTF native package (`internal/platform/ktf/native_audio.go`) plays
SMAF through a numbered sound interface: `0x0c` sets a clip from a pointer to a
SMAF file, whose length comes from its own `MMMD` header; `0x10` plays it; and
`0x14` stops it. Setting a new clip closes the previous one. A clip that is not
SMAF, exceeds 4 MiB or fails to decode is answered with silence and counted
(`ClipRefusals`) rather than failing the run. When a played clip ends, the
platform delivers event `(1, 14)` to the module's sound listener, because the
module clears its "sounding" flag only there.

### A clip a title refills is not a longer sound

The specification says a clip's data "shrinks as the media player plays it and
grows again through `putData`". Before 2026-09-04 nothing here made it shrink,
so `putData` appended indefinitely.

One KTF title keeps a single `Clip` for every sound it makes, and its whole
protocol is `Player.stop`, `putData`, `Player.play`. Appending left the second
sound's bytes behind the first sound's, and a SMAF file followed by a second
SMAF file parses as the first one, because the chunk walk ends where the first
file does. Every sound after the first therefore replayed the first: the
title's music was its logo sting on a 1.9-second loop, and the clip grew by one
file per sound for the whole session.

The fix applies the specification's rule at the only moment this runtime can
see: a play takes what the clip holds, so a `putData` after a play starts a new
fill rather than a longer one. Playing the same clip twice without refilling
still works, as a title with a fixed set of sound effects requires. Over that
title's first nine hundred rounds, 231 note-ons on one instrument restarting
every 1.85 seconds became 2,386 note-ons on eight instruments with a
four-second loop, the piece the archive holds.

Over the 46 local KTF archives (2026-09-04), no archive lost its sound: every
one that reached the sink with a sound still did, and every one with sampled
audio kept the same sample count. Counts move by a few percent run to run (the
same binary twice gave 4,325 and 4,320 messages on one archive, and 1,075 and
1,039 on another), so only a count that falls to zero, or one that moves as far
as this title's did, is evidence.

## A play that returns at once is a busy loop in the game

`com.skt.m.AudioClip.play` does not return until the clip has finished, and
`loop` does not return until the clip is stopped. Two local SKT titles are the
evidence, from opposite directions.

The first title's music runs on its own thread, whose body is (class name
generalized):

```java
this.looping = Main.looping;
clip = AudioSystem.getAudioClip("mmf");
clip.open(data, 0, data.length);
do { clip.play(); } while (this.looping);
```

The loop holds no sleep, polled state or yield. `looping` is set true beside the
calls that start background music and false beside those that fire an effect.
The title never calls `loop`: `play` plus its own flag is how it loops, which
works only if `play` waits. With a `play` that returned at once, the loop
restarted the piece from its first note as fast as the interpreter could go. On
2026-09-03 that archive called it 1,453,986 times in four hundred ticks, against
forty-nine for the other seventy-eight archives together, and never played past
the first note. Its four hundred ticks cost 4.0 seconds of CPU before the change
and 0.19 after.

### The other title closes its own music

The second title's audio thread calls `open`, `loop` and `close` in sequence
with nothing between them, and its music is stopped by a different thread
calling `close` on the same clip. That program is coherent only if `loop`
blocks until the clip stops, so that the `close` after it is cleanup rather
than the stop. With a `loop` that returned at once, the thread closed its own
music immediately: fifty-six clips were opened, looped and closed inside four
hundred ticks, in silence.

The same title recorded a hundred MIDI messages under the debug profile and
none under release. The only difference was that logging made a tick slower, so
a timeline advance sometimes landed between the `loop` and the `close` and
emitted a handful of events that were never meant to be heard. A profile
difference in what a game does is a reason to distrust the instrument, not the
profile; `testing.md` records the same finding from a KTF investigation in
which debug instrumentation manufactured a fault.

Both waits belong to the guest thread only. A platform call entered from the
Host's own pass (a paint, a key, a lifecycle callback) returns without waiting,
because blocking there would stop the screen, the input and the timers
together, which the handset's wait did not do. That rule is
`Invocation.WaitAsGuestThread`, and it is why the wait needs the execution
rather than only the VM. A stop, a pause, a close or a second start cuts either
wait short, so a title that stops its own music is not held for the rest of the
piece. The end of the program also cuts it short, because a loop has no length
of its own and a thread waiting on one would otherwise outlive its program.

### A stopped clip is not a completed clip

A guest thread whose blocking `play` or `loop` is ended by `stop`, `close`,
`pause` or another start receives `com.skt.m.UserStopException`. A clip that
reaches its duration returns normally, and calls on the Host's event thread
return without waiting.

Before 2026-09-11 both outcomes returned normally. A local SKT title clears its
audio repeat flag in its exception handler; after an explicit stop, the
normal-return path left that flag set while its playing flag was clear. Its
worker then slept repeatedly inside a synchronized block, holding the monitor
the next serial event callback needed. The Host waited in `monitor.enter`, so
input and session teardown stopped progressing.

The correction is at the audio boundary, not in Java monitor or sleep
semantics. The playback registration identifies the interrupted call, so a
returning old call does not finish a newer call's wait. This is a compatibility
finding from the local caller and its recovery path; the WIPI specification
does not define this vendor-specific API, and the reference implementation
leaves playback unimplemented.

`TestStoppedAudioDoesNotReportNaturalCompletion` runs the repository-authored
`audio-stop.jar` (`AudioStopMIDlet`) through the SKT runtime. It checks the
guest exception branch for both playback methods and all four interruption
paths, and the normal branch for natural completion. Without the correction,
all eight interruption cases take the wrong branch.

A local Chromium run on 2026-09-11 followed the reported start/new-game menu
route, advanced the introduction and reached stage 1. The page kept receiving
changing pictures and accepted left/right input; its restart control returned
to the picker, and a repository-authored canvas fixture then started on the
same server. No page exception or server warning was recorded. This shows
recovery on the reported path, not completion of the game or audible playback
on a physical phone.

An interactive CLI probe that stopped issuing ticks while screenshots were
inspected also produced a guest division by zero in the title's frame-rate
calculation after resuming. Its Host still answered commands and quit
normally, and the continuously ticking browser run did not show that error. A
long gap between `-serve` commands is not a gameplay pacing test for this title.

### A sound started decades from now

Until 2026-09-03 the SKT platform had never emitted a note through a sink, on
either Host. A sound carries the instant it started, and a Host advances the
timeline to a reading; events fall due between the two. The start came from
`clockMillis`, the absolute wall clock the record stores stamp modification
times with (around 1.77 × 10¹² milliseconds), while the reading came from the
Host as elapsed time since the program started. The start was decades ahead of
the reading, so nothing was ever due. The other two WIPI runtimes advance from
their own guest clock and never took the reading from their Host.

Both now use one clock, the MIDlet's own elapsed time, and `AdvanceAudio` takes
no argument for the Host to get wrong. Measured with `runskt -audio` over the
local corpus on 2026-09-03 at four hundred ticks each, zero archives recorded a
MIDI message before the change and fifty-six after, sixteen of them with
sampled sound as well. Frames were unchanged: sixty-two of seventy-nine
byte-identical, the rest inside the per-title noise floor, with no title's
state, tick count or error text moved.

As with the KTF record database and the KTF C sound block, a surface that
accepts everything and drops it looks exactly like one that works. The decoder,
format and API were all built and tested; the missing piece was the number that
decides whether any event is ever due.

### The timeline is not the speaker

Until 2026-09-03 the SKT runtime had no audio timeline unless a Host attached a
sink. `AttachAudioSink` created it and a CLI run never called it, so nothing
decoded, no clip had a length, every audio call was an accepted no-op, and the
platform's sound behaved differently under the two Hosts. The other two
platforms build their timeline at start and pass the sink, which may be nil,
straight into it.

SKT now does the same, and a sink attached later swaps into the existing
timeline rather than replacing it. That matters on its own: the session
attaches its sink after `Start`, so a title that loaded its clips in `startApp`
used to lose them when the sink arrived.

With the timeline present in every run, the 2026-09-03 corpus showed that of
ninety-one SKT archives, seventy-one ask for a clip in their first four hundred
ticks and sixty open one; fifty-six reach the sink with a sound, sixteen of
those with sampled audio as well. None of them touches the MIDP `Player`
surface (see [Deliberately incomplete](#deliberately-incomplete)); their sound
goes through the vendor's own surface, which `runskt -audio` records.

### What is left silent, and why

On 2026-09-03, with the blocking waits and the clock corrected, fifteen SKT
archives asked for a sound and never made one. None of them is a defect:

- **Eleven ask for a clip and never open one** inside four hundred ticks. They
  are on a title screen; the sound comes later.
- **Two open clips and never play them.** One opens twenty-four in a row, a
  title loading its sound set before it needs it.
- **Two play once and end the program.** Both are licence-refusal titles that
  check the handset's subscriber number, draw the refusal and exit at the first
  tick, and the run stops with them.

The decoder is not among the gaps. Every SMAF resource in every local archive
decodes, carries a length and reaches a sink with content: 1,693 sounds across
the ninety-archive set, none refused, empty or decoded to silence.
`testing.md` has the probe that keeps it that way.

Looping repeats: a title whose music thread parks in `loop` records messages in
proportion to run length (351, 1,047 and 2,223 at four hundred, twelve hundred
and twenty-four hundred ticks), and the piece's opening phrase recurs at the
interval its length predicts.

## A sound the archive does not carry is not a failed program

`Clip(String type, String resourceName)` names a packaged resource, and the
specification declares no exception for the constructor. A handset therefore
had no way to tell a title that its archive lacks the requested name, and there
is no reason to believe one stopped the program over it. Until 2026-08-29 this
platform did: the KTF constructor failed, `startApp` failed with it, and the
session ended before its first frame.

The title that found it builds its whole sound set in `startApp` from a
numbering its own archive is sparse in (twelve clips in a row, then every third
one to thirty-six), so the first gap was fatal and thirteen more followed it.
KTF and SKT WIPI now answer a missing resource with a clip that has no data,
which plays nothing, and log the miss. That is the only answer the
specification leaves, and the archive's own shape shows the title expects it:
a program that could not survive a gap would not have asked for one.

A first frame is worth more than a sound. The accepted C-block no-ops made the
opposite trade and lost: they claimed success for a whole surface nobody was
watching, whereas this loses one clip out of a set the title itself indexes
past, and writes the miss to the run log every time.

## A device volume, and whose it is

`backend.Audio.SetVolume(percent)` is the level a guest asked for through its
platform's media API. It is not the Host's volume control, which belongs to the
user and is the page's own sliders: a game that fades its music out has not
turned the speaker down. Device and clip levels multiply, independently of the
page sliders, and the browser applies them to sounding output without changing
playback position. The [gain contract](audio-ownership.md#guest-volume) lists
the connected guest controls, their defaults, checkpoint compatibility, legacy
sink behavior and the unverified vendor source controls.

## Live MIDI channel controls

The page applies CC7 volume, CC11 expression and CC10 pan to each sound's
channel output. Their numbers and 7-bit ranges follow the
[MIDI control-change table](https://midi.org/midi-1-0-control-change-messages).
Channel gain is `(volume / 127) * (expression / 127)`, applied after each
note's velocity/envelope and before the clip/device gain and the page MIDI
slider. Gain and pan changes use 5 ms linear transitions, keeping an unfinished
transition when another controller arrives. Clock reanchoring cancels older
pending automation before applying the latest change.

A channel shares its gain and panner across held notes, percussion and melodic
release tails. Muting does not stop sources or reset their envelope or
position, so notes started at zero can become audible later. Other owners,
other channels and PCM stay independent. Controls received before audio
activation only update state. Stop and reset disconnect the affected channel
graph.

Reconnect, quick load and clip resume set current channel controls before
reconstructing a note, keeping its original program and envelope age; the
portable record holds both the original note metadata and the current channel
state. Browser smoothing, oscillator phase, released tails and device latency
are not saved. The normalized velocity envelope is independent of channel
volume, including its small exponential-ramp floor; sustained levels keep the
same volume/expression product.

`web/acceptance/audio-controls.mjs` compares seven authored scenarios with
independent native Web Audio graphs in Chromium and WebKit. Held volume and
expression reach zero RMS, then recover without retriggering; quarter settings
track `32/127`, and pan changes move the sounding signal between channels.
Mixed notes, releases, drums, peers, PCM and aged resumes match within the
0.000001 sample tolerance. See [verification](testing.md#live-channel-controls).

## MIDI sustain, pitch range and reset

CC64 values 64..127 defer melodic note-off, including zero-velocity note-on and
CC123 (All Notes Off). Pedal release ends only keys already released; keys still
held keep playing. CC120 (All Sound Off) cuts that owner's channel immediately,
including release tails and percussion. CC123 uses the ordinary release
envelope. The page's one-shot percussion ignores ordinary note-off and sustain
but responds to CC120/123; other channels, owners and PCM stay independent.

RPN 0 sets pitch-bend sensitivity, initially two semitones and zero cents.
CC6 sets 0..127 semitones and clears the cents; CC38 sets 0..99 cents, and
values 100..127 are clamped to 99 as an explicit receiver policy. CC96/97 add or
subtract one cent with decimal carry and saturation. RPN and NRPN selector
bytes keep independent values, and selecting one byte does not overwrite the
other; null, unsupported RPN and NRPN selections ignore Data Entry. Range and
bend changes retune held melodic notes and release tails without retriggering.
Percussion and PCM ignore MIDI bend.

CC121 (Reset All Controllers) restores expression 127, sustain off, centered
bend and null RPN/NRPN selection, and keeps volume, pan, program and the
configured pitch sensitivity. These rules follow the
[MIDI 1.0 specification](https://midi.org/midi-1-0-detailed-specification),
[reset recommendation](https://midi.org/response-to-reset-all-controllers) and
[data increment/decrement recommendation](https://midi.org/response-to-data-increment-decrement-controllers).

Audio checkpoints (version 6 and later; see
[versions](#audio-checkpoint-versions)) keep pitch sensitivity, both selector
pairs, the last selected kind and keys held only by sustain. Reconnect, quick
load and pause resume restore channel state before note output, then
immediately send note-off for a pedal-held key; the pedal keeps its
reconstructed envelope alive. Independent sinks receive channel parameters once
before all resumed notes; flattened legacy sinks instead receive each note's
owner controls before that note, a distinction the forwarding Host reports
through `AudioReplaySink`. No new wire operation is involved.

Nine independent native-reference scenarios in
`web/acceptance/audio-midi-state.mjs` cover sustain, reset, tails, percussion,
PCM/peer isolation, live sensitivity changes and aged pedal-held resumes. Both
Chromium and WebKit pass with maximum sample error 5.96e-8. Source-level tests
also cover parameter selection, limits and malformed checkpoints. See
[verification](testing.md#midi-sustain-and-parameters).

## MIDP per-player volume controls

SKT MIDP players expose one stable `VolumeControl` through `getControl` and
`getControls`. Short and fully qualified control names resolve to the same
object; unknown controls return null. Queries require a realized, open player,
and a null control name raises `IllegalArgumentException`. Returned arrays are
independent copies containing the same control object.

`setLevel` clamps to 0..100 and returns the resulting level. `setMute` keeps
that level while silencing the player's active MIDI and PCM; unmuting restores
output without restarting it. Getters read the backend's clip state directly,
so no second volume record can diverge during checkpoint restoration. Changes
send `VOLUME_CHANGED` with the associated control object; unchanged settings
send no event. Control identity, listener references and level/mute state
survive quick load, including a retained control whose player is closed.

The contracts come from [Controllable](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/Controllable.html),
[VolumeControl](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/control/VolumeControl.html)
and [PlayerListener](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/PlayerListener.html).
Delivery uses the bounded Player event queue described below: volume setters
enqueue events without invoking listeners inline, and listener changes made by
a callback take effect on a later Host pass.

Standard event names share their JVM string object with bytecode literals and
native API constants, so both `.equals` and
`event == PlayerListener.VOLUME_CHANGED` work, including for a previously
delivered name retained across quick load. The
[PlayerListener specification](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/PlayerListener.html)
demonstrates reference comparison for standard events and recommends value
comparison for proprietary names. The authored lifecycle fixture uses
reference comparison and checks retained event identity before draining
restored notifications. The shared [JVM string pool](jvm.md#implemented) also
covers repeated literal loads, class-file constants and `String.intern()`
without changing the identity of newly constructed strings.

## MIDP playback lifecycle

SKT MIDP uses the shared backend score cursor for media time and completion. A
positive loop count includes the current pass; `-1` repeats indefinitely, and
zero or values below `-1` are refused. Finite loops stop after exactly the
requested number of passes even when one Host tick spans several passes.
Natural completion changes STARTED to PREFETCHED and keeps the end position;
the next `start` begins at zero with the configured count again. Explicit
`stop` and `deallocate` keep the cursor and remaining passes for resumption.
Changing the count while stopped replaces the remaining budget, including the
retained current pass; changing it while started or closed is refused.

`getMediaTime` advances from the same guest clock as playback and reports
microseconds. `setMediaTime(0)` rewinds the current pass while keeping its
remaining loop budget and playing/paused status. Negative positions clamp to
zero; other seeks are unsupported and raise `MediaException`. Unrealized
players cannot seek; closed players cannot query duration or media time,
change loop settings, or add or remove listeners. Repeated close is harmless.

`RunPending` delivers the ordered event queue outside the player locks. Start,
explicit stop and each natural end carry a boxed `Long` media time; each loop
end precedes the next STARTED event at zero; CLOSED carries null. Listener
snapshots keep the recipients at each transition, including after a later
listener removal or player close. Callback-generated events wait for the next
Host pass, so a listener can restart playback without recursively entering
itself. The registry, undelivered events, payload objects, listener identity,
finite remaining count and observed completions survive checkpoints.

Limits are 256 loaded players, 256 listeners per player and 4,096 pending
events (`internal/platform/skt/media_events.go`); a guest transition past a
delivery limit is an explicit runtime error. Natural ends are the exception: a
parked game's guest clock keeps time, so a Player left looping across a long
park returns owing more passes than the queue holds. Those collapse into the
last pass's end and, if it keeps playing, its next start, because failing there
put the MIDlet in its error state on the first pass after resume. KTF and LGT
collapse a completion backlog the same way.

Host advancement and guest transitions share the same synchronization for each
registered Player. Raw SKVM clips have a separate clock/transition lock, which
blocking playback releases before waiting, so a Host tick cannot overtake a
sampled native-call clock and cause a false backward-time error. A successful
SKVM `open` replacement closes the prior handle, releases its blocked playback
wait and clears its loop/pause flags; failed decoding leaves the previous
playback intact.

These rules follow the [MIDP Player contract](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/Player.html)
and [PlayerListener event contract](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/PlayerListener.html).
Score completion is distinct from an owned PCM tail: completion alone keeps
that output, while explicit cancellation or replay ends it. WIPI Play starts a
new pass, and WIPI Stop is cancellation.

## Java WIPI playback listeners

KTF, LGT and SKT implement the [WIPI PlayListener contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/PlayListener.md):
`playUpdate(Clip, int event, int parm)`, with ERROR=-1, END_OF_DATA=1,
START=2, STOP=3, PAUSE=4, RESUME=5, RECORD=6 and FULL_OF_DATA=7. (The shared
interface formerly declared an incorrect `playDone(Clip)`.)
[`Clip.setListener`](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Clip.md)
replaces its single recipient; null removes it.

Successful Play queues START. KTF and SKT allow a restart; LGT keeps its
refusal of Play while already playing. Stop queues STOP only for a playing or
paused clip. Successful Pause and Resume queue their distinct events. Each
completed repeat queues END_OF_DATA without a synthetic START. No-op calls and
a failed Play on a silent unsupported clip emit no event. Recording and
asynchronous decoder-error notifications are unsupported.

The specification does not settle callback threading, queued-recipient
replacement, repeat notifications or the `parm` value, so each platform states
an explicit policy below. In all three, the recipient is captured at the
transition, `parm` is zero, and events a callback causes wait for the next
round.

### SKT delivery and ownership

SKT uses the MIDP bounded deferred queue: delivery occurs in `RunPending`, and
WIPI and MIDP share the 4,096-event limit. A callback can replace the listener
or restart playback.

Playing and paused Players keep their Clip and listener. Pending events keep
their original Clip and recipient independently. Idle Players keep only a weak
association, so a released Clip/listener graph becomes collectible after
pending delivery. Checkpoint capture and detached restoration validate the
reciprocal Clip/Player identities, listener types, event roots and cross-owner
event data before activation.

The strict checkpoint record fields are unchanged. A `skt.wipi-player` native
kind adds one trailing Clip reference for active ownership; the reader still
accepts the old `skt.player` kind and rebuilds its association from a retained
Clip, but cannot recover a Clip the old record never saved. Private WIPI event
names reuse the existing event fields. Earlier binaries refuse these kinds and
names rather than accept an incomplete owner graph; rollback uses a checkpoint
they support, or an ordinary start. Ordinary saves are unchanged.

### KTF delivery and ownership

KTF uses a separate 4,096-event queue drained by `Client.ServiceEvents` under
the guest execution lock, including when the guest owns its generic event loop.
Pending callbacks and natural score ends contribute Host deadlines, adjusted
for guest speed, so an unrelated guest sleep does not postpone delivery; paused
scores have no completion deadline. This schedules completion, not individual
MIDI events. The generic event prefix runs before the captured media prefix,
and events posted between the two queues wait for the next round. A
recoverable generic callback exception keeps waiting media notifications.

KTF stores the listener on the Clip, keeps playing and paused owners as strong
roots, and releases that reference on completion, stop or buffer replacement.
Its weak-key bookkeeping cannot pin an idle listener-to-Clip cycle. Queued
events keep their original arguments independently. The vendor
`Player(BaseClip)` overloads still support playback and checkpoints; only
callback arguments must be instances of the specified `Clip` type.

KTF checkpoints use a temporary `$wfeature.media.v1` named root with native kind
`ktf-media-v1`; the strict root and Clip fields are unchanged. The carrier
records completion watermarks, active owners and queued recipients, and is not
kept after adoption. Restoration validates audio/owner identity, actual guest
interface graphs and executable concrete callbacks after the AOT bindings exist
and before activation; a refused restore leaves the running session's playback
and ordinary saves unchanged. Completion counters must match the saved audio
exactly, and undelivered notifications are explicit queue entries. Records
without the carrier start at their saved audio completion count, without
invented historical events. Old empty interface records keep their allocated
layout: concrete callbacks work, but restoration does not add constant or
interface-method slots to that old class, and new starts provide the full
declaration. Earlier binaries reject the new native kind; rollback uses a
supported old checkpoint or an ordinary start.

### LGT delivery and ownership

LGT Java uses a separate 4,096-event queue at the media callback stage of
`Session.Tick`. LGT runs an explicit guest collector, because a Go slice of
numeric addresses cannot keep a callback argument alive. The queue pins the
entire detached batch until every callback returns, including callbacks that
trigger collection or throw, so collection inside an earlier callback cannot
free a later recipient. An uncaught guest exception ends only that callback.
Pending events and score ends shorten the guest tick; paused scores have no
deadline. LGT keeps its boolean result for stopping a loaded idle Clip without
inventing a STOP event. Buffer replacement keeps already queued Java events. C
notifications keep their separate callback addresses, statuses and queue
behavior.

The collector roots playing and paused Java Clips and queued Clip/recipient
pairs, and follows the Clip's current listener as an edge. Idle cycles stay
collectible: once stopped and delivered, an unreachable Clip/listener cycle
releases its guest objects, and sweeping a Clip also closes its native sound.

Callback targets must be issued objects with a verified executable ARM/Thumb
callback. Real LGT AOT archives can omit interface declarations while keeping
the exact `playUpdate(Clip,int,int)` member; that raw member is sufficient
evidence. Validation still checks bounded class/interface graphs read from
guest memory, including inherited and derived interfaces. Without a named
callback, a declared direct one-method PlayListener interface must identify its
method address or receiver vtable slot; cached metadata alone cannot supply
either. Imported event constants receive all eight standard values rather than
uninitialized zeroes.

LGT checkpoints with Java Clips use a version-3 media envelope around the
unchanged strict version-2 session/client records. It saves completion
watermarks and queued recipient snapshots; the reader validates every owner
and target against real guest class/interface metadata after restoring
bindings and before workers start, and requires each watermark to equal its
audio count. Repeated adoption keeps the watermark. The reader also accepts the
previous strict version-2 shape, infers ownership only from issued Clip objects
and starts at the recorded completion count without inventing historical
events; a missing or already freed legacy owner cannot be recovered. Sessions
without Java media still write version 2. Older binaries refuse the new
envelope; rollback requires an older supported checkpoint or an ordinary start.
Ordinary save files are unchanged.

## Transient tones and capability queries

`Manager.playTone` and the SKVM beep use transient one-shot sequences. Their
handles are reclaimed on completion or stop, including after a checkpoint load;
reusable clips and overlapping tones keep their own lifetimes. The loaded-sound
limit stays 256. A 512-tone regression keeps a reusable clip loaded throughout,
and an authored Java fixture restores an active tone through a full checkpoint
and verifies its eventual reclamation.

The [MIDP Manager contract](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/Manager.html)
requires positive tone duration, notes from 0 to 127, clamped volume from 0 to
100 and nonblocking calls; zero duration raises `IllegalArgumentException`.
`device://tone` and explicit `audio/x-tone-seq` player creation raise
`MediaException` because ToneControl is not implemented. Capability queries
advertise SMAF resources, honor their protocol/content filters and omit tone
sequences. Null player inputs receive the specified `IllegalArgumentException`;
empty media is refused rather than yielding an inert player.

## Deliberately incomplete

- **`Player.record`** answers false: no microphone can be offered.
- **Clip helper hooks.** `Clip.playStart(boolean)` and `Clip.playUpdate(int,int)`
  are not called by `Player`. One KTF image names both, and its run reaches
  neither; every local title that wants playback events takes them through
  `PlayListener`, which all three Java paths deliver.
- **MIDP `Player` usage in SKT titles.** No local SKT title calls the MIDP
  `Player` registrations; they use the vendor `AudioSystem`/`AudioClip`
  surface, which `runskt -audio` records. Measuring the unused MIDP surface
  was why `runskt -audio` was once judged unnecessary.
- **Source mutes reach no sound.** KTF C and LGT remember a source's mute state
  and answer it back; neither routes it to a clip, because the titles that set
  one go on playing every clip they have (see
  [the evidence](history/audio.md#source-mutes-and-the-ktf-c-media-slots-read-from-callers)).
  The KTF Java `Volume` mute and default-volume extensions stay stubs: no local
  KTF image names any of the four. LGT's source volume is remembered; its one
  caller copies the same level to the device volume itself.
- **Stream-wave assignment and HPS device commands.** 23 local files trigger
  stream notes that the current channel-plus-one mapping turns into waves they
  do not carry, and twelve HPS files hold only device commands. They stay
  silent until original runtime evidence settles the assignment and the
  commands. In 77 HPS setup chunks the first message's last declared byte is
  `7F` where `F7` belongs, so the chunk yields nothing; whether `7F` is a vendor
  terminator is unverified. See
  [sounds without audible events](history/audio.md#sounds-without-audible-events).
- **Wave end steps.** On 2026-10-09, 115 KTF and 100 LGT waves ended above a
  tenth of full scale (25 and 62 above a quarter), a step to silence when the
  buffer runs out, and 40 and 71 carried a DC offset above 5%. A short fade at a
  wave's natural end would remove the steps, but whether a handset ends a stream
  that way is unknown, so it is left for a listening decision
  ([census](history/audio.md#pcm-contract-evidence)).
- **Wave format coverage.** Score-attached waves support mono 4-bit Yamaha
  ADPCM and mono 8-bit signed/unsigned PCM. PCM audio tracks decode mono ADPCM.
  Stereo, 16-bit PCM, TwinVQ and MP3 playback remain unsupported.
- **PCM timing and tuning contracts.** ATR volume, expression and pan are
  implemented. No local sound carries an ATR bend event, so bend and its range
  wait for a caller. Section 2.2 of the SMAF specification makes every event's
  gate its internal note-off, but whether a stream wave stops there or plays its
  sample out is the handset's choice, as are short-sample looping and
  same-channel retrigger. A hard cutoff would trim more than a tenth from 5,042
  of the 8,752 local wave triggers, so the natural tail stays until a handset is
  heard. Stream and score-attached sample velocity/gain and profile-specific
  mapping need profile evidence. See [PCM controls](#live-pcm-track-controls)
  and [the census](history/audio.md#pcm-contract-evidence).
- **The WAV is a concatenation, not a timeline.** Sampled sounds are appended
  at the first one's rate, because these games play one at a time; the file
  shows what the sound was, not when it played, and does not represent
  simultaneous mixing, PCM cancellation or browser output. Use the browser
  renderer for those comparisons.
