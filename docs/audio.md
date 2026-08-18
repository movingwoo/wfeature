# Sound

These games ship their music as SMAF (`.mmf`) — Yamaha's format for the MA
sound chips in 2000s handsets. 670 of them sit inside the 32 local KTF
archives, mostly packaged inside the game's JAR rather than beside it, so the
acceptance probe has to descend into the nested zip to find them at all. It
plays every one of them:

```sh
WFEATURE_SMAF_ACCEPTANCE=1 go test ./internal/audio/smaf
```

471,202 events, 204,357 notes and 348 waves currently decode without a file
being refused.

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
- **Softbank** is the handset dialect with length-prefixed exclusive messages
  instead of `0xf7`-terminated ones.

`internal/audio/smaf` parses all three. A chunk that does not parse ends the
chunk list rather than failing the file — these come out of game archives and
the tail of one is often padding.

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

`backend.AudioSink` is split into PCM and MIDI because SMAF is: percussion and
melody are note events for a synthesiser, sampled sounds are waveforms to mix,
and no Host renders both the same way.

`backend.Audio` owns loaded sounds and where each is in its playback. It does
not own a clock — `Advance` is called with the same clock the guest runs on, at
the end of `Session.Tick`. So a Host batching ticks through a manual clock
hears the same sequence a real-time Host does, only faster, and a clock that
jumps forward still emits the events it skipped, because **a note off that is
skipped never stops**.

Stopping is not just "quit emitting": every note the sound started is released
and every channel it touched gets sustain, all-sound-off, and all-notes-off.
Without that a sound stopped during a sustained chord rings on under the next.

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

## A device volume, and whose it is

`backend.Audio.SetVolume(percent)` is the level a *guest* asked for through its
platform's media API. It is not the Host's volume control — that one is the
user's, and the page has its own sliders — and the two are different settings: a
game that fades its music out has not turned the speaker down.

It scales note velocities and wave samples where an event is emitted, so a note
already sounding keeps the level it started at and a fade moves in note-sized
steps, which is what the titles doing it play anyway. **Zero also releases what
is sounding**, because scaling the next note is not enough when a note started
at full volume is ringing under a volume the game has already set to nothing.

Only KTF's C block drives it so far. LGT's `MC_mdaSetVolume` still stores the
number and echoes it back, which is what its own titles needed; pointing it at
this is a one-line change whenever a title is found that fades.

## Deliberately incomplete

- **`Player.record`** is refused outright: no microphone can be offered.
- **`PlayListener`** never fires. Nothing yet reports a clip finishing back
  into the guest.
- **KTF's Java `Volume` class** answers zero, where the same platform's C
  `MC_mdaSetVolume` is honoured. The class is a device-volume *getter* surface —
  `getDefaultVolume`, `getMute` — and answering the Host's own setting through
  it would be reporting the user's slider as the game's; the C call is a setter
  a title drives on purpose. A title that reads a level back through the class
  and expects to find what it set through the block is where that split would
  have to be revisited.
- **Pause does not resume mid-phrase.** Playback is tracked by clock position,
  and these games pause only in order to stop.
- **Non-ADPCM wave formats.** `TwosComplementPCM`, `OffsetBinary`, TwinVQ, and
  MP3 parse but do not decode; every wave in the local archives is mono
  4-bit Yamaha ADPCM.
- **`runskt` has no `-audio`.** The MIDP runtime takes a sink like the other
  two and the browser gives it one, so a MIDlet that played would be heard;
  what is missing is only the CLI's recorder. It stays missing for the reason
  the surface report gives — `Player`'s registrations are among the ones **no**
  local title has ever called ([`skvm.md`](skvm.md)) — so there is nothing yet
  for a recording to catch.
- **The WAV is a concatenation, not a timeline.** Sampled sounds are appended
  at the first one's rate, because these games play one at a time; the file
  answers "what did it sound like", not "when".
