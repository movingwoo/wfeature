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
- **`runskt -audio` exists now**, and the reasoning that kept it away is worth
  keeping: it was that `Player`'s registrations are among the ones no local
  title has ever called. That is still true and was still the wrong measure —
  the vendor's own `AudioSystem`/`AudioClip` surface is what these titles use.
  A surface nobody calls is a fair reason not to build a recorder; the wrong
  surface being the one measured is not.
- **The WAV is a concatenation, not a timeline.** Sampled sounds are appended
  at the first one's rate, because these games play one at a time; the file
  answers "what did it sound like", not "when".
