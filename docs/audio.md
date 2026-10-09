# Sound

These games ship their music as SMAF (`.mmf`) — Yamaha's format for the MA
sound chips in 2000s handsets. 895 of them sit inside the 38 local KTF
archives, mostly packaged inside the game's JAR rather than beside it, so the
acceptance probe has to descend into the nested zip to find them at all. It
plays every one of them:

```sh
WFEATURE_SMAF_ACCEPTANCE=1 go test ./internal/audio/smaf
```

546,127 events, 243,718 notes and 499 waves currently decode without a file
being refused. The wider three-carrier scan and the decoder corrections it led
to are in the [corpus audit](history/audio.md#corpus-evidence).

## What a SMAF file is

A file is a list of tagged chunks. The ones that matter are score tracks
(`MTRx`) and PCM audio tracks (`ATRx`); a score track is itself a list of
chunks holding setup sysex, a sequence, and any wave data the sequence
triggers. The sequence is a byte stream of duration-prefixed events in **one of
three dialects**, and they are genuinely different encodings, not variations:

- **Mobile standard** is MIDI-shaped — MIDI status bytes, MIDI variable length
  quantities — either stored plainly or Huffman coded.
- **Handset standard** packs a note's channel, octave, and pitch into a single
  status byte, and its variable length quantity is not MIDI's: the second byte
  contributes all eight bits and the first is biased by one. Common controller
  values are abbreviated into the event type itself.
- **Softbank** uses the handset event layout, but its sized exclusive messages
  do not have to end in the handset format's final `0xf7`.

`internal/audio/smaf` parses all three. A chunk that does not parse ends the
chunk list rather than failing the file — these come out of game archives and
the tail of one is often padding.

### HPS exclusive messages

An exclusive message in a handset (HPS) sequence is `FF F0`, a one-byte size,
then that many bytes: maker ID, format ID, device data and a final `F7`. The
size is a plain byte, not a MIDI variable length quantity, and an `F7` inside
the device data does not end the message. An HPS setup chunk (`Mtsu`) is the
same messages back to back without durations. These are pages 38 and 28 of the
[SMAF 3.05 specification](https://img.atwiki.jp/mmfuta/attach/19/78/SMAF3.05.pdf#page=38);
a Mobile setup chunk keeps its own `F0` plus MIDI-length framing.

The decoder used to read an HPS message up to the first `F7`. That kept the size
byte as if it were the maker ID and, when the device data held an `F7`, read the
rest of the message as sequence records, whose durations added silence: 27
sounds in the three-carrier scan delayed everything after such a message by one
to fifteen minutes, with the same notes in the same order. The setup reader knew
only the Mobile framing, so it dropped every HPS setup message. Both now read
the declared size, keep the payload whole including its final `F7`, and emit the
usual `F0`-framed event without the size byte. A message that is truncated or
whose last declared byte is not `F7` ends that sequence or setup chunk and keeps
everything before it, like any other malformed tail; nothing guesses a
lengthless form. Setup messages are charged to the same event and SysEx budgets
as the rest of the file. Softbank keeps its previous reading.

The page still configures no device from SysEx, so the audible part of the
repair is the timing. Checkpoints hold the event lists already decoded for
loaded sounds, so a restored sound keeps its old list and only newly loaded
sounds use this reading; no checkpoint field changes. The corpus measurements
and the silent files that led here are in the
[audit](history/audio.md#sounds-without-audible-events).

### Mobile note velocity memory

A mobile note without a velocity operand uses the channel's last explicit
velocity, initially 64. An explicit stream-wave note updates this memory too,
including an absent or unsupported wave. CC121 resets that channel's memory to
64; other channels, banks and programs retain their state. Explicit zero stays
zero. This follows the note-message contract on page 43 of the
[SMAF 3.05 specification](https://img.atwiki.jp/mmfuta/attach/19/78/SMAF3.05.pdf#page=43).

This repairs the following melodic notes without changing the existing stream
wave mapping or its raw PCM gain. Stream velocity and gate behavior
are separate [contract work](history/audio.md#pcm-contract-evidence). Existing
checkpoints retain their already decoded event lists; newly loaded sounds use
the repaired decoder. This velocity repair itself changes no saved fields;
the separate PCM extension below versions its new state.

### Live PCM track controls

Supported mono ADPCM `ATRx` tracks now retain volume, expression and pan through
the shared runtime and the page. Each track gets four independent logical
channels in file order, beginning at 1; duplicate chunk tags do not merge their
state. Channel zero preserves ungrouped score-attached and legacy waves. MIDI
controllers remain a separate domain. Controls at the same score time precede
wave starts, and later controls affect every still-playing wave in their group,
including a tail left by an earlier repeat. Wave samples, duration and sampling
rate remain unchanged; controls follow presentation deadlines across speed changes.

The [SMAF contract](history/audio.md#pcm-contract-evidence) defines values 0–127,
expression initially 127 and pan initially 64. PCM amplitude uses
`(volume/127)^2 * (expression/127)^2`; left/right pan gains are
`cos(pi*pan/254)` and `sin(pi*pan/254)`. Center 64 therefore yields approximately
0.702720 and 0.711466, rather than duplicating mono at unity on both speakers.
Initial volume is unspecified: this implementation uses neutral 127 until an
explicit volume arrives and retains whether it was set. This is a compatibility
policy, not a claimed handset default. Out-of-range decoded controls are ignored
without dropping their elapsed time; invalid authored events are refused.

The page routes each mono source through independent left/right gains and a
two-input channel merger, followed by the owner's guest gain and the user's wave
slider. Its existing per-source 0.8 gain remains. Live control targets ramp over
5 ms, a receiver smoothing policy rather than a SMAF requirement. Controls
before a wave establish its initial level. Shared sample buffers remain raw and
cacheable across owners and groups; the graph is separate from cached content.
The last completed source releases its group nodes while retaining controller
state. Explicit stop, close or restart clears that owner's state; pause freezes
it, and natural completion/repeat retains it. Scheduled stop keeps its retiring
graph connected until the old sources end, independently of a new run.

`backend.AudioPCMSink.PCMChannels()` reports current downstream support in
addition to `OwnedAudioSink`. Pages negotiate `pcm=1` with `sound=owned` or
`sound=resume`. Reconnect updates this capability before reconstructing output.
An incapable sink receives a stereo copy with current gain/pan applied at wave
onset; it cannot change already emitted samples. Device/clip gain is applied
once afterward. The retained timeline and samples are identical for both paths.
The [wire extension](session.md#protocol-2-sound) preserves older operation sizes.

Audio checkpoints save raw routed waves and sorted, independent PCM states,
including unset volume. Reconstruction sends current controls before trimmed
sample tails. Version 8 added each wave's original admission charge; version 9
also preserves its position within the first remaining sample frame.
Strict readers accept four encoded shapes: v6 omits the PCM control/routing
fields, charge and phase; v7 requires the PCM fields but omits charge and phase;
v8 adds the charge; v9 requires all three additions. Mixed shapes, unrelated
missing fields, duplicates, unknown fields and unreserved groups are refused
before adoption. Reading v6 does not
invent routing. Versions 6/7 derive an initial charge from their available
sample tails; their original pre-trim size cannot be recovered. The next capture
writes v9. Versions 6–8 adopt phase zero because their discarded fraction cannot
be recovered. Versions below 6 remain unsupported. Older binaries refuse v9, so
rollback requires a pre-upgrade checkpoint or normal startup; checkpoint files
and ordinary saves are left in place. Debug and release use the same format.

ATR bend range, exact gate cutoff, short-sample looping and same-channel
retrigger rules still need evidence. The `0x36` expression alias remains a
compatibility extension; `0x3b` is the documented opcode. Score-attached wave
velocity/gain and profile-specific mapping remain unchanged. Native browser
rendering [passes](testing.md#native-audio-rendering-acceptance); device
listening for this extension is still outstanding.

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
`Parse` and `Decode` return an error wrapping `ErrResourceLimit`; `Play` retains
its compatibility behavior of returning no events on failure. `backend.Audio.Load`
uses `Decode` so callers retain the diagnostic cause without allocating a handle
or disturbing another loaded sound. Ordinary malformed tails still preserve the
readable prefix. Length quantities cannot wrap their integers, and generated
event times, note gates and extra layer tails must fit unsigned 32-bit
milliseconds. This prevents a distant note-off from wrapping before its note-on.

The corpus comparison covers 11,175 carrier-local unique sounds. Every ordered
event and PCM/SysEx payload hash is unchanged. Boundary and fuzz evidence,
measured maxima and archive-read exclusions are recorded in the
[audit](history/audio.md#bounded-decoding) and
[test guide](testing.md#smaf-decoder-bounds).

### Loaded-sound resources

The runtime additionally limits all retained sounds together to 1,048,576 event
and reserved active-key/PCM-channel entries and 128 MiB of logical payload. These are the
checkpoint limits, now enforced when admitting a sound. Each sound costs 128
bytes, each event 64 bytes and each possible held key 8 bytes, plus two bytes
per PCM sample and one per SysEx byte. Each distinct PCM group reserves one
entry and 16 logical bytes, with at most 2,048 groups across all loaded sounds,
including control-only groups. Repeated payload references are charged
repeatedly. These accounting widths bound payload retention; they do not claim
to measure the Go allocator, browser graph or physical device memory.

Admission reserves the distinct positive note-on keys in the sound, plus any
held keys retained in a restored record. A sound has at most 2,048 MIDI keys.
Retriggering a key replaces its held-key entry, matching the output and page;
velocity-zero note-on releases it. This prevents an unbalanced repeating
authored stream from growing an unbounded list. Held-key bookkeeping is distinct
from the 24 audible voices and from keys released under sustain.

`Audio.Load`, `LoadEvents` and `PlayTransient` reject excessive combined cost
with `ErrAudioResourceLimit` before allocating a handle or changing output.
`LoadEvents` and `PlayTransient` copy events and nested PCM/SysEx buffers after
acceptance, so a small borrowed slice cannot retain a large caller allocation.
The caller may reuse its buffers after return. Decoded files already own their
buffers. Event ordering and MIDI note bounds are validated at the same boundary.

Per-sound costs are immutable. Admission sums at most 256 cached costs rather
than rescanning old buffers. Close and transient reclamation release capacity;
reusable stop, completion and pause retain it. Checkpoints recompute costs before
copying, validate active-key bounds/uniqueness and retain the existing schema.
A previously accepted slot near the limit can now be refused because future
active keys are reserved as well. Ordinary save files and debug/release formats
remain unchanged. [Verification](testing.md#loaded-audio-admission) covers limits,
ownership, restore and concurrent callers.

### Checkpoint catch-up work

KTF, LGT and both SKT runtimes use `backend.ValidateAudioCatchup` before
restoring a timeline. It validates the saved audio shape and bounds the work
actually due from its cursor: at most 1,048,576 event visits, 128 MiB of emitted
PCM/SysEx references and 1,048,576 held-key comparisons/copies. Controlled mono
PCM is charged at four bytes per sample to cover the stereo fallback. The budgets
cover all sounds together. Consumed events and future events do not count.

The calculation follows finite remaining passes, time-zero events at repeat
boundaries and the frozen pause clock. It preserves held-key order to count
positive note-on searches, note-off/zero-velocity removal and final silence.
Controls do not clear logical held keys. Each repeat consumes a nonempty score,
so the validation itself is bounded. It neither advances live playback nor
emits or copies output payloads. Existing event/retention bounds still apply to
the entire saved score. The checkpoint format is unchanged.

This fixes false refusal of long scores whose earlier notes were already
consumed, while refusing excessive first-tick work. See the
[checkpoint verification](testing.md#checkpoint-display-and-audio-catch-up).

## Why translating to MIDI is not a relabelling

`tonemap.go` is the interesting half. Three problems it exists to solve:

**Channels.** A SMAF track addresses four channels and a file holds several, so
a file can name more than sixteen logical channels. Melodic ones are allocated
real MIDI channels on first use, in an order that skips channel 10; rhythm ones
all go to channel 10. Whether a channel is rhythm has to be known *before* its
first event, and the bank select that says so can arrive after it — hence
`preclassify`, which walks the sequence once before playing it.

**Programs.** SMAF programs are Yamaha MA voices. Bank `0x7d` means "drum kit",
and a handful of MA voices have General MIDI stand-ins that sound closer than
the raw program number. One of them, the MA ambience voice, has no single
equivalent at all, so it is played as three layers: the note itself plus two
supporting voices, detuned and panned apart, released later than the note that
triggered them.

**Scales.** The handset dialect keeps volume and expression as separate values
that MIDI folds into one controller, its notes sit three octaves below MIDI's,
and its rhythm channels carry the drum key in the *program* rather than the
note — with the hit's loudness derived from volume and expression, because a
handset rhythm channel has no velocity.

`smaf_test.go` is the reference implementation's own test suite carried over.
That matters more than it sounds: a port that only proves "does not crash on
real files" would not catch a dialect decoded plausibly but wrongly, and every
one of those cases is a specific wrong-but-plausible reading.

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
before admitting its retained voices, keeping the resumed voice newer than
those earlier attacks. `PlaybackState` observes progress without advancing;
platform services use one global advance followed by these observations for
all clip completion notifications. Paused owners retain their frozen position;
completed transients are reclaimed during the merge. Handles, score cursors
and existing checkpoint formats retain the same ordering after restoration.

SKT serializes each Player's clock read, progress reconciliation and dependent
transition with raw clips, tones and Host advancement. Its lock order is
Player, audio timeline, then notification registry; Host advancement releases
the timeline before taking Player locks. KTF and LGT reconcile all Java clip
completions at native transition boundaries, including LGT C pause/resume, so a peer's newly completed score
cannot leave its callback watermark or active GC root stale at quick save.
SKT listener replacement observes a completed score with its old recipient
before installing the new listener.

This preserves score emission and voice-admission order. The optional timed
output boundary below also retains the intervals between those events.

### Presentation timestamps and recovery

`TimedAudioSink.AudioTime` selects presentation seconds for subsequent sink
operations, including MIDI, PCM, gain, cancellation and resumed voices. A score
event uses its absolute guest deadline; a command uses its synchronized guest
instant. `SetPlaybackRate` drains the old rate before rebasing future intervals.
It changes neither pitch nor PCM sampling rate. All five execution paths
initialize this mapping before playback and restore it at the saved guest
instant, without changing the portable save format. Restored overdue deadlines
may be negative relative to the new output epoch's zero.

Retained output follows presentation elapsed time, with unscaled Host elapsed
time between service boundaries and a monotonic Host floor when execution falls
behind. Historical events already have an envelope/sample age at emission.
This makes a virtual guest jump preserve drum expiry and PCM position under
fine or coarse servicing, while a slow guest cannot make expired output young
again. Detached checkpoint validation remains excluded at activation.

The page separately negotiates `timing=1` alongside `sound=resume`. Each batch
carries timed operations and a final authoritative clock, including silent
ticks. Cached pages keep their previous wire operations. The page schedules
accepted batches on Web Audio with a 100–250 ms lead (using reported latency),
a 350 ms maximum future horizon and 5 ms late tolerance. It validates the whole
batch before scheduling any source. These are playout bounds, not a claim about
handset latency; see the rendering acceptance in the testing guide.

An out-of-window, backward or malformed batch requests `audioResume` and
suppresses further old output until a reset and current-state reconstruction
arrive. The request is negotiated and output-epoch checked. Definitions are
invalidated too, so a malformed first definition cannot lose subsequent PCM.
Interruption waits for a running context before requesting fresh output;
quick load clears an outstanding recovery request with the old epoch.

Timed clip stops retire the old source graph at their deadline. A restart gets
fresh channel/gain nodes, while old nodes remain connected until their last
source ends. Global reset cancels both current and retired sources immediately.
Expired percussion leaves voice admission before the next authored attack,
even if its browser `onended` notification has not run yet. This schedules a
bounded time window directly; there is no unbounded JavaScript playback queue.
The receiver also bounds retained sources and PCM references as described below.
Native CPU, allocation and clipping remain measurement work in the
[audit](history/audio.md#guest-event-delivery).

### Playout lateness diagnostics

Debug page reports include `audio playout timing` aggregates at the existing
session-statistics cadence and immediately before saving the page report.
Each event's lateness is `max(0, currentTime - mappedAt) * 1000` milliseconds,
sampled when the complete batch reaches the scheduling-window check. The
`anchored` group covers first/reset anchors; `continuing` covers an existing
anchor. Keeping them separate prevents the new anchor's intentional lead from
being mistaken for measured initial delivery latency.

Each group counts checked/admitted batches, clock-only batches and the first
window-refusal reason (`late`, `future`, or negative/nonfinite time `range`).
All non-clock events in a checked batch contribute, including events after
the first refusal: not late, positive lateness up to 5 ms, over 5 through 20 ms,
over 20 through 100 ms, and over 100 ms. Invalid mapped times or unrepresentable
millisecond differences count separately. Event and frontier maximum lateness
are distinct; the final clock never inflates event counts. Legacy, malformed,
backward, resource-refused and unavailable/suspended-context batches do not
reach this measurement. Admission does not imply successful audible playback.

These are render-deadline observations after binary decoding but before JSON
PCM conversion and source allocation, not network transit, dispatch cost or
speaker latency. Coarse `currentTime` values retain their existing precision.
Host collector overflow, outbound audio drops and reconstruction keep their
separate log messages. Fixed counters retain no events or samples, survive
immediate output recovery, and reset on report flush or explicit log clear.
Release disables the recorder through the existing page-log capture boundary.
There is no new timer, request, protocol field or persistent state. Actual
device distributions still require a debug run on that device; authored
verification is in the [testing guide](testing.md#playout-lateness-diagnostics).

### Live source bounds

The page accepts at most 512 retained Web Audio sources and 128 MiB of logical
Float32 PCM payload across live wave references. Scheduled starts, melodic
release tails, percussion and retired clip generations all count until their
sources end or are disconnected by an immediate stop/reset. The separate
24-note admission rule still selects held MIDI keys. Every PCM reference is
charged, including references sharing one cached buffer; these are conservative
accounting limits, not measured native memory usage.

A complete batch is checked before changing controllers, scheduling nodes or
resetting the old output. JSON PCM is sized from its encoded length before
decoding. The same limits protect direct source entry points. A timed refusal
uses the existing `audioResume` recovery and suppresses stale output until a
current reconstruction arrives. An older untimed server has no such handshake:
the page drops the refused batch and can accept the next one. Preflight credits
immediate owner stops inside an untimed batch, so a legacy stop/restart can reuse
that owner's capacity without freeing peers. Scheduled stops retain their
charges because the sources can still be audible. Global reset
checks its replacement output against empty budgets before clearing old output.

The largest supported reconstruction fits: 256 retained PCM waves plus 24
MIDI notes, and 32 MiB of raw signed-16-bit PCM expanded to stereo for an older
sender and converted to Float32 is at most 128 MiB. The source ceiling leaves
room for transient tails. Backend PCM admission now shares its retained-output
budget, as described below. The receiver still needs its own bound: MIDI
release tails, sequential short waves and retired graphs can remain connected
until browser callbacks run. These resource policies do not establish handset
voice-stealing behavior. Native performance remains acceptance work.

### PCM admission and restoration

The runtime admits at most 256 PCM references with 32 MiB of original raw
signed-16-bit sample charges, shared by audible and paused owners. It processes
waves in global authored deadline/owner/event order and prunes expired samples
at that deadline. When either limit is full, it consumes the newest wave event
without retaining or emitting it; existing sources and peers stay unchanged.
The refused event is never retried by pause/resume, recovery or quick load.
MIDI, controllers, score cursors, repeats and completion continue normally.
This is an emulator resource policy, not claimed handset polyphony.

Every emitted wave now has a complete reconstruction record, so overload no
longer makes output capture or pause fail because of untracked PCM. Pause
keeps its reserved capacity, preventing resume from overfilling the budget.
Natural expiry and explicit stop/restart/rewind/close release the affected
charges; natural score completion still leaves a sample's tail running.

`AudioWaveState.BudgetBytes` records the admitted raw byte count. Capture can
trim heard frames while preserving that charge, so a partially elapsed sample
does not acquire extra capacity through quick load. Validation requires a
whole-frame charge at least as large as the remaining samples, with the same
aggregate limit. `AudioWaveState.FramePhase` retains the consumed fraction of
the first remaining frame as an integer from zero through 999,999,999, in
billionths of a frame. With `N` remaining frames, sample rate `R` and phase `p`,
remaining nanoseconds are `floor((N*1e9-p)/R)`. Advancing `a` nanoseconds consumes
`floor((p+a*R)/1e9)` frames and leaves `(p+a*R)%1e9` phase. This preserves both
future frame selection and the existing floored expiry exactly through repeated
captures, including rates that do not divide one billion. Expiry is checked
before multiplication; the admission bound then keeps the arithmetic in range.

An optional `AudioWaveResumeSink` receives the trimmed wave and frame phase
after existing PCM-group and guest-gain fallback. The web Host negotiates
`phase=1` with an owned page and carries phase separately from presentation time;
backdating a wave would break event ordering. The page starts the same cached
buffer at `phase/(1e9*rate)` seconds and shortens its natural end accordingly.
The [Web Audio playback contract](https://www.w3.org/TR/webaudio/#playback-AudioBufferSourceNode)
permits sub-sample offsets. This preserves the requested playhead position,
not an earlier renderer's hidden interpolation history. Legacy sinks/pages
retain whole-frame replay; raw sample bytes and admission charges are unchanged.

Hosts attach their existing logger with `Audio.SetLogger`, including restored
timelines and startup before a sink is present. PCM refusals log the reason,
owner, retained size, incoming size and cumulative count at powers of two.
The debug profile exposes these bounded diagnostics; release filters them.
Diagnostics are not portable state and changing sinks does not reset them.

### Decoded PCM buffer reuse

Protocol 2 definitions share immutable decoded samples between triggers. The
page marks those local events `cacheable` and reuses their Web Audio buffers;
the wire format does not change. Direct mutable sample arrays and JSON events
keep taking fresh snapshots. Sample identity, channel count, frame count and
sampling rate must match; a changed format replaces the previous cached variant.
Definition replacement, forget and reconnect cannot confuse reused numeric IDs.

The cache retains at most 256 buffers and 16 MiB of float sample payload, evicting
the least recently used entry first. The byte ceiling matches the wire's 8 MiB
definition budget after conversion from signed 16-bit to Float32. The entry
ceiling follows the backend's 256 retained-wave bound, but does not limit live
sources. Larger waves still play without caching. Weak sample keys avoid keeping
decoded arrays alive after their definitions disappear. Cached AudioBuffers
remain bounded until eviction or clearing; context replacement and global
`allOff` clear them, while stopping one clip retains reusable payloads.

Each trigger still creates a source and gain with independent owner, start,
stop and playback position. Eviction does not stop an active source using that
buffer. These ceilings bound cache retention, not total browser memory: active
sources may retain evicted buffers under the separate live-reference bound.
The [allocation probe](testing.md#pcm-buffer-reuse-and-mobile-velocity)
counts API requests and copies; native memory, CPU and rendered-output acceptance
remain separate measurements.

### Guest clock rate changes

KTF Java/C retains elapsed guest time when changing speed. Its active and
prepared runtime anchors are rebased from one Host-clock reading before the
new rate is applied. The existing clock-age checkpoint representation is
preserved, including delayed activation on another Host epoch. Paused audio
keeps its cursor and unscaled output age; future score gates advance at the
new guest rate. Already-created Host timer/worker deadlines keep their existing
instants.

Integer nanoseconds can make exact fractional rebasing impossible. The origin
uses the smallest representable age that does not move the guest backwards;
it never converts an out-of-range floating value to a duration. If the required
origin exceeds the duration range, both anchors and the prior rate remain
unchanged. Shared speed normalization now treats NaN as the default rate 1;
the existing positive/negative infinity behavior remains 16/1.
Startup settings, public speed, input repeat and checkpoint metadata all use
the normalized rate accepted by the platform, including a refused change.

The [audit reproduction](history/audio.md#ktf-speed-change-clock-continuity) and
[verification](testing.md#ktf-speed-change-continuity) cover the previously
backward pause clock and prematurely consumed notes.

### Output sinks

`backend.AudioSink` is split into PCM and MIDI because SMAF is: percussion and
melody are note events for a synthesiser, sampled sounds are waveforms to mix,
and no Host renders both the same way.

`backend.Audio` owns loaded sounds and where each is in its playback. It does
not own a clock — `Advance` is called with the same clock the guest runs on, at
the end of `Session.Tick`. So a Host batching ticks through a manual clock
hears the same sequence a real-time Host does, only faster, and a clock that
jumps forward still emits the events it skipped, because **a note off that is
skipped never stops**.

The web Host also implements `backend.OwnedAudioSink`. Loaded handles scope
MIDI channels, equal-pitch notes and PCM sources. Explicit stop, close and restart
cancel only that handle's output, including percussion and release tails.
Natural completion releases that clip's MIDI channels while preserving PCM that
extends past the score end. [Independent playback and recovery](audio-ownership.md)
describes the negotiated protocol, checkpoint version and queue/reconnect repair.

## The two Hosts

**Browser** (`web/audio.js`) synthesises the MIDI itself, from oscillators.
The Rust build takes the obvious route instead — a worklet plus a 10.5MB sound
bank fetched at page load — which on iOS pushes the tab past its memory ceiling
and gets skipped entirely, leaving those users with no sound at all. This is a
real trade: a soundfont piano sounds like a piano and an oscillator does not.
But this music was written for an FM chip with a handful of operators, so an
envelope over a waveform picked by program family lands closer to the original
than it would for most music, costs nothing to ship, and works everywhere.
Drums are a filtered noise burst. Sampled sounds go straight into an
`AudioBuffer`, which needs no synthesis at all. The two feed separate gain
nodes so the page's music and effects sliders can trade against each other.

**CLI** (`backend.RecordingSink`) writes rather than plays. There is no audio
device this project is willing to depend on — Rust's side reaches a system MIDI
port and an ALSA or CoreAudio backend, neither of which has a cgo-free Go
equivalent — and "did the sound come out right" is a question a recording
answers better than a speaker. `runktf <game> -audio out` and `runlgt` the same
write `out.mid` (a type 0 file at one tick per millisecond, so recorded times
are the file's times with no tempo arithmetic) and `out.wav` for the sampled
sounds. Verified end to end: 1500 ticks of one KTF title produced 3339 MIDI
messages — 1405 note ons across 45 program changes, 82 seconds of guest time —
in a file that parses.

### A recording has to end, even though the music does not

A run stops where the tick count says, which is almost never where a phrase
ends, and the notes still down at that moment had nothing after them in the
file. Four of five recordings taken from real titles ended holding between four
and eight notes, and a player that honours the file drones them after the last
event forever — the recording said the game was playing a chord it never
stopped.

So the writer counts what is sounding as it lays the track down and releases
the remainder at delta zero before end-of-track. Note on with velocity zero
counts as the note off it is, or the file would release notes that were already
up. The releases are emitted in channel and note order rather than in a map's,
because two runs of the same game have to produce the same bytes for a
recording to be worth comparing.

The five recordings that found it now end with nothing sounding, and
`internal/backend`'s recording tests parse the file the way a player would
rather than matching bytes.

## Two sound surfaces per platform, and the one that was silent

A WIPI title reaches sound through either the Java classes or the C block, and
which one it takes is the title's business. LGT implemented both. KTF
implemented the Java classes and left the C block at accepted no-ops — and
**fourteen of the thirty-three local KTF archives keep their sound there**, so
they created clips, filled them with SMAF and played them, and every call
succeeded into nothing.

That block is now real: create, put data, play, stop, clear, free, volume,
vibrator and mute, on the same `backend.Audio` timeline the Java classes use.
Its function numbers are read off the callers rather than off the
specification's print order, which this vendor's table does not follow — the
table, the argument shapes it was recovered from, and the one call left
deliberately unread are in [`ktf.md`](ktf.md), "Sound in C, and the table that
was accepted and thrown away".

The lesson for the next platform is the one the KTF record database taught
first: **a stub that answers success is a value the game will believe.** Nothing
in a title's behaviour distinguishes "the handset played it" from "the runtime
accepted it and dropped it", so a whole surface can be missing for as long as
nobody plays the game with the sound on.

## A play that returns at once is a busy loop in the game

`com.skt.m.AudioClip.play` **does not return until the clip has finished**, and
`loop` does not return until the clip is stopped. Two local titles are the
evidence, and they arrive at it from opposite directions.

The first one's music runs on a thread of its own whose body is:

```java
this.looping = Kingdoms.looping;
clip = AudioSystem.getAudioClip("mmf");
clip.open(data, 0, data.length);
do { clip.play(); } while (this.looping);
```

Nothing else is in that loop — no sleep, no state to poll, no yield — and
`looping` is set true beside the calls that start background music and false
beside the ones that fire an effect. The title never calls `loop`, because
`play` plus its own flag *is* how it loops. That only works if `play` waits.

Read the other way, as a call that returns at once, the same loop restarts the
piece from its first note as fast as the interpreter can go. That is what it
was: **one archive called it 1,453,986 times in four hundred ticks**, against
forty-nine for the other seventy-eight put together, and never played a note
past the beginning. Its four hundred ticks cost 4.0 seconds of CPU; they now
cost 0.19.

### The other title closes its own music

The second title's audio thread is `open`, `loop`, `close`, one after the other
with nothing between them, and the way its music is *stopped* is a different
thread calling `close` on the same clip. That only reads as a program if `loop`
blocks until the clip stops: the `close` after it is the cleanup, not the stop.
With a `loop` that returned at once, the thread closed its own music
immediately — fifty-six clips opened, looped and closed inside four hundred
ticks, and silence.

**That title also shows what a debug build is worth here.** Under the debug
profile it recorded a hundred MIDI messages and under release none, and the
difference is only that logging makes a tick slower: in the slower run a
timeline advance sometimes landed between the `loop` and the `close`, emitting a
handful of events that were never meant to be a sound. It reads as "the debug
build has sound and the release build does not", which is a fault in a place
where there is none. A profile difference in what a game *does* is a signal to
distrust the instrument, not the profile — the same lesson `testing.md` records
from the KTF investigation where debug instrumentation manufactured a fault.

**Both waits are the guest thread's, and only the guest thread's.** A platform
call entered from the Host's own pass — a paint, a key, a lifecycle callback —
returns without waiting, because blocking there stops the screen, the input and
the timers together, which is not what the handset's wait did. That is the
whole of `Invocation.WaitAsGuestThread`, and it is why the wait needed the
execution rather than only the VM. A stop, a pause, a close or a second start
cuts either wait short, so a title that stops its own music is not held for the
rest of the piece — and so does the end of the program, because a loop has no
length of its own and a thread waiting on one would otherwise outlive the
program it belongs to.

### A stopped clip is not a completed clip

A guest thread whose blocking `play` or `loop` is ended by `stop`, `close`,
`pause`, or another start receives `com.skt.m.UserStopException`. A clip that
reaches its duration still returns normally, and calls on the Host's event
thread still return without waiting.

This distinction was missing: both outcomes returned normally. A local SKT
title clears its audio repeat flag in its exception handler. After an explicit
stop, the normal-return path left that flag set while its playing flag was
clear. Its worker then slept repeatedly inside a synchronized block, retaining
the monitor needed by the next serial event callback. The Host waited in
`monitor.enter`, so input and session teardown could no longer progress.

The correction is at the audio boundary, not in Java monitor or sleep semantics.
The playback registration identifies the interrupted call, so a returning old
call does not finish a newer call's wait. This is a compatibility finding from
the local caller and its recovery path; the WIPI specification does not define
this vendor-specific API, and the reference implementation leaves playback
unimplemented.

`TestStoppedAudioDoesNotReportNaturalCompletion` runs the repository-authored
`audio-stop.jar` through the SKT runtime. It checks the guest exception branch
for both playback methods and all four interruption paths, and the normal branch
for natural completion. Without the correction, all eight interruption cases
take the wrong branch.

A local Chromium run on 2026-09-11 followed the reported start/new-game menu
route, advanced the introduction, and reached stage 1. The page continued
receiving changing pictures and accepted left/right input. Its restart control
returned to the picker, and a repository-authored canvas fixture then started
on the same server. No page exception or server warning was recorded in that
run. This proves recovery on the reported path, not completion of the game or
audible playback on a physical phone.

An interactive CLI probe that stopped issuing ticks while inspecting screenshots
also produced a guest division by zero in its frame-rate calculation after
resuming. Its Host still answered commands and quit normally; the continuously
ticking browser run did not show that error. A long gap between `-serve`
commands is not a gameplay pacing test for this title.

### A sound started decades from now

Making the timeline unconditional made a second thing visible, and it is the
larger one: **this platform had never emitted a note through a sink, on either
Host.**

A sound carries the instant it started, and a Host advances the timeline to a
reading; events fall due between the two. Here the start came from
`clockMillis` — the absolute wall clock the record stores stamp their
modification times with, around 1.77 × 10¹² milliseconds — and the reading came
from the Host, which passes elapsed time since the program started. One is
decades ahead of the other, so nothing was ever due. The other two WIPI
runtimes advance from their own guest clock and never took the reading from
their Host at all.

They are one clock now, the MIDlet's own elapsed time, and `AdvanceAudio` no
longer takes an argument for the Host to get wrong. The measurement is
`runskt -audio` against the local corpus: **zero archives recorded a single
MIDI message before, and fifty-six record one now**, sixteen of them with
sampled sound as well, in four hundred ticks each. The frames are unchanged —
sixty-two of seventy-nine byte-identical, the rest inside the per-title noise
floor, no title's state, tick count or error text moved.

This is the lesson the KTF record database and the KTF C sound block both
taught, a third time: **a surface that accepts everything and drops it looks
exactly like one that works.** What was missing here was not a decoder, a
format or an API — all of that was built and tested — but the one number that
decides whether any of it is ever due.

### A predictor that walked away from the signal

Every sampled sound this project decodes is 4-bit Yamaha ADPCM, and **the
decoder had two errors in it that a single sound hides and nine thousand
samples do not.**

ADPCM stores each sample as a step from the one before it, so it has no way to
recover from a mistake: an error in one sample is carried in every sample after
it, and an error in the *step size* is carried into the size of every step
after it. That is what makes this class of bug look like nothing in a unit test
and like a broken speaker in a game.

The two:

- **The step table was carried in 64ths.** Its five distinct ratios are 230,
  307, 409, 512 and 614 parts in 256; as 57, 77, 102, 128 and 153 parts in 64
  they are the same ratios at a quarter of the resolution, and four of the five
  round the wrong way. 0.4% per sample.
- **The two samples in a byte were read the wrong way round.** The low nibble
  is the earlier one.

**What that did to the local set.** Seventeen sampled sounds across two KTF
archives, decoded under the old rules: every one of them ends thousands of
counts away from where it began — one runs from about -9,000 to +25,000 over
its length and spends part of that pinned to a rail — and their means sit at
-13,000, +15,000, -19,000. Under the corrected rules every one of the
seventeen has a mean inside +-120 and ends within a few hundred counts of zero,
which is what a decoded waveform looks like. Measured on a whole run rather
than one sound, one title's captured wave went from a mean of -14,216 with 35
samples at the negative rail to a mean of -58 with none.

**What it sounded like** is not distortion in the usual sense: a ramp to a rail
is a click at the start of every sound and a crackle through it, which is how
it was reported — "the sound pops". A title whose sound effects are all
sampled pops on every one of them.

The fix costs nothing: the same multiply, a shift of eight instead of six, and
the two nibbles swapped.

### The two wave forms that are not compressed at all

A score track can hang a sample off a channel and trigger it with note zero,
and the gate in front of that let only 4-bit Yamaha ADPCM through. **Everything
else was dropped without a word**, which is the right default for a format
nobody has a reading of and the wrong one for a format that is bytes.

A scan of every SMAF file in the local archives — 9,065 of them, from 557
packages — says how much this is: 5,789 attached waves are ADPCM and **four are
not**. All four are 8-bit offset-binary mono at 8 kHz, they are the four songs
of one rhythm title, and two of those songs trigger theirs 138 and 114 times.
So the melody played and the sample the beat is built on did not.

Offset binary is what the format field says and what the bytes say: the wave's
lead-in is `0x80` bytes, its silence, and read as offset binary the sample sits
at a mean of -115 with a symmetric range where two's complement puts it at
-9,221 against a rail. Both 8-bit forms are read now. **Sixteen-bit and stereo
stay unplayed rather than guessed at** — no local archive carries one, so there
is nothing to check a reading against, and a wave played wrong is worse than a
wave not played.

### A clip a title refills is not a longer sound

The specification says a clip's data "shrinks as the media player plays it and
grows again through `putData`". Nothing here made it shrink, so `putData`
appended for ever.

**A clip is a title's to reuse.** One KTF title keeps a single `Clip` for every
sound it makes, and its whole protocol is `Player.stop`, `putData`,
`Player.play`. Appending left the second sound's bytes behind the first
sound's — and a SMAF file followed by a second SMAF file parses as the first
one, because the chunk walk ends where the first file does. So **every sound
after the first played the first sound again**: the title's music was its logo
sting, on a 1.9-second loop, for as long as the title was left running. The
clip grew by one file per sound for the whole session.

The rule that fixes it is the one the specification already states, applied at
the only moment this runtime can see: a play takes what the clip holds, so a
`putData` after a play starts a new fill rather than a longer one. Playing the
same clip twice without refilling still works, which is what a title with a
fixed set of sound effects does.

Measured on that title's first nine hundred rounds: 231 note-ons on one
instrument, restarting every 1.85 seconds, became 2,386 note-ons on eight
instruments with a four-second loop — the piece the archive actually holds.

**A whole-corpus check.** Over the 46 local KTF archives, no archive loses its
sound: every one that reached the sink with a sound still does, and every one
with sampled audio still has the same sample count. The counts move by a few
per cent either way run to run — the same binary twice gives 4,325 and 4,320
messages on one archive, and 1,075 and 1,039 on another — so only a count that
goes to zero, or one that moves like this title's did, is evidence.

### What is left silent, and why

With the two waits and the clock right, the corpus was asked the question the
other way round: which archives ask for a sound and never make one. Fifteen do,
and none of them is a defect.

- **Eleven ask for a clip and never open one** inside four hundred ticks. They
  are on a title screen; the sound comes later.
- **Two open clips and never play them.** One opens twenty-four in a row, which
  is a title loading its sound set before it needs it.
- **Two play once and end the program.** Both are licence-refusal titles that
  check the handset's subscriber number, draw the refusal and exit at the first
  tick, and the run stops with them.

**The decoder is not among the gaps.** Every SMAF resource in every local
archive decodes, carries a length and reaches a sink with something in it —
1,693 sounds across the ninety-archive set, none refused, none empty, none
decoded to silence. `testing.md` has the probe that keeps it that way.

**Looping repeats.** A title whose music thread parks in `loop` records
messages in proportion to how long the run is — 351, 1,047 and 2,223 at four
hundred, twelve hundred and twenty-four hundred ticks — and the piece's own
opening phrase recurs at the interval its length predicts.

### The timeline is not the speaker

This was invisible for as long as it was, because **the SKT runtime had no
audio timeline at all unless a Host attached a sink.** `AttachAudioSink` made
one; a CLI run never called it; so nothing decoded, no clip had a length, every
audio call was an accepted no-op, and the platform's sound behaved differently
under the two Hosts. The other two platforms build their timeline at start and
pass the sink — which may be nil — straight into it.

This one does now too, and a sink attached later swaps into the timeline that
is already there rather than replacing it. That last part matters on its own:
the session attaches its sink *after* `Start`, so a title that loaded its clips
in `startApp` used to have them thrown away by the arrival of the speaker.

**What the corpus does with sound, now that a run can see it**: of
ninety-one archives, seventy-one ask for a clip in their first four hundred
ticks and sixty open one; fifty-six of them reach the sink with a sound,
sixteen of those with sampled audio as well. The MIDP `Player` surface is
still untouched by every one of them — that part of "Deliberately incomplete"
stands — but the vendor's own surface is not, so a `runskt -audio` would now
have something to catch.

## A sound the archive does not carry is not a failed program

`Clip(String type, String resourceName)` names a packaged resource, and the
specification declares the constructor no exception at all. So there is nothing
a handset could have told a title whose archive is missing the name it asked
for, and no reason to believe one stopped the program over it. This platform
did: the constructor failed, `startApp` failed with it, and the session ended
before its first frame.

The title that found it builds its whole sound set in `startApp` from a
numbering its own archive is sparse in — twelve clips in a row, then every
third one to thirty-six — so the first gap was fatal and thirteen more followed
it. It now gets a clip with no data, which plays nothing, and the miss is
logged. That is what the specification leaves as the only available answer, and
what the archive's own shape says the title expects: a program that could not
survive a gap would not have asked for one.

**A first frame is worth more than a sound.** This is the same trade the
accepted C-block no-ops used to make and lost — there, success was claimed for
a whole surface nobody was watching, and here the loss is one clip out of a set
the title itself indexes past. The difference is that this one is written into
the run log every time it happens.

## A device volume, and whose it is

`backend.Audio.SetVolume(percent)` is the level a *guest* asked for through its
platform's media API. It is not the Host's volume control — that one is the
user's, and the page has its own sliders — and the two are different settings: a
game that fades its music out has not turned the speaker down.

The browser applies device and clip levels to held MIDI, PCM, percussion and
release tails without changing playback position. Zero mutes the output;
raising the level restores the still-running sources. Clip and device
percentages multiply, independently of the page sliders. Raw output and gain
are saved separately. Diagnostic sinks and older cached pages retain future-event
scaling and MIDI release at zero; they cannot adjust active PCM.

KTF Java/C and LGT Java/C device controls, SKVM device controls and the three
platforms' Java Clip levels now reach this path. LGT C clip volume does too.
The [gain contract and verification](audio-ownership.md#guest-volume) describes
defaults, checkpoint compatibility and the unverified vendor source controls.

## Live MIDI channel controls

The page applies CC7 volume, CC11 expression and CC10 pan to each sound's
channel output. Their numbers and 7-bit ranges follow the
[MIDI control-change table](https://midi.org/midi-1-0-control-change-messages).
Channel gain is `(volume / 127) * (expression / 127)`, after each note's
velocity/envelope and before the clip/device gain and page MIDI slider.
Gain and pan changes use 5 ms linear transitions, preserving an unfinished
transition when another controller arrives. Clock reanchoring cancels older
pending automation before applying the latest change.

A channel shares its gain/panner across held notes, percussion and melodic
release tails. Muting does not stop sources or reset their envelope/position;
notes started at zero can become audible later. Other owners, other channels
and PCM stay independent. Controls received before audio activation only
update state. Stop/reset disconnects the affected channel graph.

Reconnect, quick load and clip resume set current channel controls before
reconstructing a note, while retaining its original program and envelope age.
The portable record already contains both original note metadata and current
channel state. Browser smoothing, oscillator phase, released tails and device
latency are not saved. The normalized velocity envelope now remains independent
of channel volume, including its small exponential-ramp floor; sustained
levels keep the same volume/expression product.

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
envelope. The page's one-shot percussion ignores ordinary note-off and sustain,
but responds to CC120/123; other channels, owners and PCM remain independent.

RPN 0 sets pitch-bend sensitivity, initially two semitones and zero cents.
CC6 sets 0..127 semitones and clears the cents; CC38 sets 0..99 cents. Values
100..127 are clamped to 99 as an explicit receiver policy. CC96/97 add/subtract
one cent with decimal carry and saturation. RPN/NRPN selector bytes retain
their independent values; null, unsupported RPN and NRPN selections ignore Data
Entry. Selecting one byte does not overwrite the other selector byte. Range
and bend changes retune held melodic notes and release tails without retriggering.
Percussion and PCM ignore MIDI bend.

CC121 (Reset All Controllers) restores expression 127, sustain off, centered
bend and null RPN/NRPN selection. It preserves volume, pan, program and the
configured pitch sensitivity. These rules follow the
[MIDI 1.0 specification](https://midi.org/midi-1-0-detailed-specification),
[reset recommendation](https://midi.org/response-to-reset-all-controllers) and
[data increment/decrement recommendation](https://midi.org/response-to-data-increment-decrement-controllers).

Since version 6, audio checkpoints preserve pitch sensitivity, both selector pairs,
the last selected kind and keys held only by sustain. Reconnect, quick load and
pause resume restore channel state before note output, then immediately send
note-off for a pedal-held key. The pedal keeps its reconstructed envelope alive.
Independent sinks receive channel parameters once before all resumed notes;
flattened legacy sinks instead receive each note's owner controls before that
note. The forwarding Host reports this distinction through `AudioReplaySink`.
This MIDI repair needs no new wire operation. Audio versions below 6 are refused.

Nine independent native-reference scenarios in
`web/acceptance/audio-midi-state.mjs` cover sustain, reset, tails, percussion,
PCM/peer isolation, live sensitivity changes and aged pedal-held resumes.
Both Chromium and WebKit pass with maximum sample error 5.96e-8. Source-level
tests additionally cover parameter selection, limits and malformed checkpoints.
See [verification](testing.md#midi-sustain-and-parameters).

## MIDP per-player volume controls

SKT MIDP players expose one stable `VolumeControl` through `getControl` and
`getControls`. Short and fully qualified control names resolve to the same
object; unknown controls return null. Queries require a realized, open player,
and a null control name raises `IllegalArgumentException`. Returned arrays are
independent copies containing the same control object.

`setLevel` clamps to 0..100 and returns the resulting level. `setMute` keeps
that level while silencing the player's active MIDI and PCM; unmuting restores
output without restarting it. Getters read the backend's clip state directly,
so there is no second volume record to diverge during checkpoint restoration.
Changes send `VOLUME_CHANGED` with the associated control object; unchanged
settings send no event. Control identity, listener references and level/mute
state survive quick load, including a retained control whose player is closed.

The contracts come from [Controllable](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/Controllable.html),
[VolumeControl](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/control/VolumeControl.html)
and [PlayerListener](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/PlayerListener.html).
Delivery uses the bounded Player event queue described below. Volume setters
enqueue events without invoking listeners inline. Listener changes made by a callback
are delivered on a subsequent Host pass.

Standard event names share their JVM string object with bytecode literals and
native API constants. Both `.equals` and `event == PlayerListener.VOLUME_CHANGED`
work, including a previously delivered name retained across quick load. The
[PlayerListener specification](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/PlayerListener.html)
explicitly demonstrates reference comparison for standard events and recommends
value comparison for proprietary names. The authored lifecycle fixture uses
reference comparison and checks retained event identity before draining restored
notifications. The shared [JVM string pool](jvm.md#implemented) also fixes repeated
literal loads, class-file constants and `String.intern()` without changing the
identity of newly constructed strings.

## MIDP playback lifecycle

SKT MIDP uses the shared backend score cursor for media time and completion.
A positive loop count includes the current pass; `-1` repeats indefinitely,
and zero or values below `-1` are refused. Finite loops stop after exactly the
requested number of passes even when one Host tick spans several passes.
Natural completion changes STARTED to PREFETCHED and retains the end position;
the next `start` begins at zero with the configured count again. Explicit
`stop` and `deallocate` retain the cursor and remaining passes for resumption.
Changing the count while stopped replaces the remaining budget, including the
retained current pass. Changing it while started or closed is refused.

`getMediaTime` advances from the same guest clock as playback and reports
microseconds. `setMediaTime(0)` rewinds the current pass while preserving its
remaining loop budget and playing/paused status. Negative positions clamp to
zero; other seeks remain unsupported and raise `MediaException`. Unrealized
players cannot seek; closed players cannot query duration/media time, change
loop settings, or add/remove listeners. Repeated close is harmless.

`RunPending` delivers the ordered event queue outside the player locks. Start,
explicit stop and each natural end carry a boxed `Long` media time. Each loop
end precedes the next STARTED event at zero; CLOSED carries null. Listener
snapshots preserve the recipients at each transition, including after a later
listener removal or player close. Callback-generated events wait until the
next Host pass, so a listener can restart playback without recursively
entering itself. The registry, undelivered events, payload objects, listener
identity, finite remaining count and observed completions survive checkpoints.
Limits are 256 loaded players, 256 listeners per player and 4,096 pending events;
exceeding a delivery limit is an explicit runtime error.

Host advancement and guest transitions share the same synchronization for
each registered Player. Raw SKVM clips have a separate clock/transition lock;
blocking playback releases it before waiting. This prevents a Host tick from
overtaking a sampled native-call clock and causing a false backward-time
error. A successful SKVM `open` replacement closes the prior handle, releases
its blocked playback wait and clears its loop/pause flags. Failed decoding
leaves the previous playback intact.

These rules follow the [MIDP Player contract](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/Player.html)
and [PlayerListener event contract](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/PlayerListener.html).
Score completion remains distinct from an owned PCM tail: completion alone
preserves that output; explicit cancellation or replay ends it. WIPI Play
continues to start a new pass, and WIPI Stop remains cancellation.

## Java WIPI playback listeners

KTF, LGT and SKT implement the [WIPI PlayListener contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/PlayListener.md):
`playUpdate(Clip, int event, int parm)`, with ERROR=-1, END_OF_DATA=1,
START=2, STOP=3, PAUSE=4, RESUME=5, RECORD=6 and FULL_OF_DATA=7. The shared
interface previously exposed the incorrect `playDone(Clip)` signature.
[`Clip.setListener`](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/media/Clip.md)
replaces its single recipient; null removes it.

Successful Play queues START. KTF and SKT allow a restart; LGT preserves its
existing refusal of Play while already playing. Stop queues STOP only
for a playing or paused clip. Successful Pause and Resume queue their distinct
events. Each completed repeat queues END_OF_DATA without a synthetic START.
No-op calls and a failed Play on a silent unsupported clip emit no event.
Recording and asynchronous decoder-error notifications remain unsupported.

The specification does not establish callback threading, queued-recipient
replacement, repeat notifications or the playback parameter value. SKT's
explicit policy uses the MIDP bounded deferred queue: delivery occurs in
`RunPending`, with the recipient captured at the transition and `parm` zero.
A callback can replace the listener or restart playback; resulting events wait
for the next Host pass. WIPI and MIDP share the 4,096-event limit on SKT.
KTF uses a separate 4,096-event queue drained by `Client.ServiceEvents`, under
the guest execution lock. It applies the same recipient and repeat policy,
including when the guest owns its generic event loop. Pending callbacks and
natural score ends contribute Host deadlines, adjusted for guest speed, so
an unrelated guest sleep does not postpone delivery. Paused scores have no
completion deadline. This schedules completion, not individual MIDI events.
The existing generic event prefix runs before the captured media prefix;
events posted between the two queues wait for the next round. A recoverable
generic callback exception preserves waiting media notifications.

LGT uses a separate 4,096-event Java queue at the existing media callback stage
of `Session.Tick`. It captures recipients and pins the entire detached batch
through guest calls, so collection inside an earlier callback cannot free a
later recipient. Reentrant events wait for the next round; an uncaught guest
exception ends just that callback. Pending events and score ends shorten the
guest tick, while paused scores have no deadline. LGT retains its existing
boolean result for stopping a loaded idle Clip, without inventing a STOP event.
Buffer replacement preserves already queued Java events; C notifications keep
their separate callback addresses, statuses and queue behavior.

LGT's collector roots playing/paused Java Clips and queued Clip/recipient pairs,
and follows the Clip's current listener as an edge. Idle cycles remain
collectible; sweeping a Clip also closes its native sound. Callback targets
must be issued objects with a verified executable ARM/Thumb callback. Actual
LGT AOT archives can omit interface declarations while retaining the exact
`playUpdate(Clip,int,int)` member; that raw member is sufficient evidence.
Validation still checks bounded actual class/interface graphs, including
inherited and derived interfaces. Without a named callback, a declared direct
one-method PlayListener interface must identify its method address or receiver
vtable slot. Cached metadata alone cannot supply either contract. Imported
event constants receive all eight standard values instead of uninitialized
zeroes.

LGT checkpoints with Java Clips use a version-3 media envelope around the
unchanged strict version-2 session/client records. It saves completion
watermarks and queued recipient snapshots, validates every owner and target
before workers start, and requires each watermark to equal its audio count.
The reader also accepts the previous strict version-2 shape, infers ownership
only from issued Clip objects and starts at the recorded completion count,
without inventing historical events. A missing or already freed legacy owner
cannot be recovered. Sessions without Java media still write version 2.
Older binaries refuse the new envelope; rollback requires an older supported
checkpoint or an ordinary start. Ordinary save files are unchanged.

On SKT, playing and paused Players retain their Clip and listener. Pending events
retain their original Clip and recipient independently. Idle Players keep only
a weak association, so a released Clip/listener graph becomes collectible
after pending delivery. Checkpoint capture and detached restoration validate
the reciprocal Clip/Player identities, listener types and event roots.

The strict checkpoint record fields are unchanged. A new `skt.wipi-player`
native kind adds one trailing Clip reference for active ownership, while the
reader still accepts the old `skt.player` kind and rebuilds its association
from a retained Clip. A previous record cannot recover a Clip it never saved.
Private WIPI event names reuse the existing event fields. Earlier binaries
refuse these new kinds or names; rollback uses a checkpoint they already
support, or an ordinary start. Ordinary saves are unchanged.

KTF stores the listener on the Clip, retains playing and paused owners, and
releases that strong reference on completion, stop or replacement of the
buffer. Its weak-key bookkeeping cannot pin an idle listener-to-Clip cycle.
Queued events retain their original arguments independently. The existing
vendor `Player(BaseClip)` overloads still support playback and checkpoints;
only callback arguments must be instances of the specified `Clip` type.

KTF checkpoints use a temporary `$wfeature.media.v1` named root with native
kind `ktf-media-v1`; existing strict root and Clip fields are unchanged. The
carrier records completion watermarks, active owners and queued recipients,
but is not retained after adoption. Restoration validates audio/owner identity,
actual guest interface graphs and executable concrete callbacks before
activation. Its completion counters must match the saved audio exactly;
undelivered notifications are explicit queue entries. Earlier records without
the carrier start at their saved audio
completion count, without invented historical events. Old empty interface
records keep their allocated layout: concrete callbacks work, but restoration
does not add constant or interface-method slots to that old class. New starts
provide the full declaration. Earlier binaries reject the new native kind;
rollback uses a supported old checkpoint or an ordinary start. LGT Java
delivery remains separate work; LGT C callbacks are unchanged.

## Transient tones and capability queries

`Manager.playTone` and the SKVM beep use transient one-shot sequences. Their
handles are reclaimed on completion or stop, including after a checkpoint load;
reusable clips and overlapping tones keep their own lifetimes. The loaded-sound
limit remains 256. A 512-tone regression keeps a reusable clip loaded throughout.
An authored Java fixture also restores an active tone through a full checkpoint
and verifies its eventual reclamation.

The [MIDP Manager contract](https://docs.oracle.com/javame/config/cldc/ref-impl/midp2.0/jsr118/javax/microedition/media/Manager.html)
requires positive tone duration, notes from 0 to 127, clamped volume from 0 to
100 and nonblocking calls. Zero duration now raises `IllegalArgumentException`.
`device://tone` and explicit `audio/x-tone-seq` player creation raise
`MediaException` because ToneControl is not implemented. Capability queries
advertise SMAF resources, honor their protocol/content filters and omit tone
sequences. Null player inputs receive the specified `IllegalArgumentException`;
empty media is refused rather than yielding an inert player.

## Deliberately incomplete

- **`Player.record`** is refused outright: no microphone can be offered.
- **Clip helper contracts.** The separate `playUpdate(int,int)` and
  `playStart(boolean)` methods need further verification. They are distinct
  from the PlayListener callback implemented on all three Java paths.
- **Source mute/default-volume extensions** still need verified vendor numeric
  mappings. KTF extension calls are stubs; LGT remembers these values without
  routing them to a clip. Known clip/device levels and MIDP VolumeControl,
  including zero and mute, already control active browser output.
- **KTF C pause/resume bindings** still need verified slot mappings. The Java,
  MIDP, SKVM and LGT C paths retain their cursor, repeat mode, note envelope age
  and PCM position; see [clip pause and resume](audio-ownership.md#clip-pause-and-resume).
  The evidence for both vendor items is in the
  [audio history](history/audio.md#unverified-vendor-source-and-slot-mappings).
- **Stream-wave assignment and HPS device commands.** 23 local files trigger
  stream notes that the current channel-plus-one mapping turns into waves they
  do not carry, and twelve HPS files hold only device commands. They stay
  silent until original runtime evidence settles the assignment and the
  commands; see [sounds without audible events](history/audio.md#sounds-without-audible-events).
- **Wave format coverage.** Score-attached waves support mono 4-bit Yamaha
  ADPCM and mono 8-bit signed/unsigned PCM. PCM audio tracks decode mono ADPCM.
  Stereo, 16-bit PCM, TwinVQ and MP3 playback remain unsupported.
- **PCM timing and tuning contracts.** ATR volume/expression/pan are implemented;
  bend, gate cutoff, looping and retrigger semantics remain unverified. Stream
  sample velocity/gain still needs profile evidence. See [PCM controls](#live-pcm-track-controls).
- **`runskt -audio` exists now**, and the reasoning that kept it away is worth
  keeping: it was that `Player`'s registrations are among the ones no local
  title has ever called. That is still true and was still the wrong measure —
  the vendor's own `AudioSystem`/`AudioClip` surface is what these titles use.
  A surface nobody calls is a fair reason not to build a recorder; the wrong
  surface being the one measured is not.
- **The WAV is a concatenation, not a timeline.** Sampled sounds are appended
  at the first one's rate, because these games play one at a time; the file
  answers "what did it sound like", not "when".
