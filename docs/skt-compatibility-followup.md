# SKT compatibility follow-up, 2026-09-12

This note separates declarations found in archives, calls observed during a
bounded run, and behavior reached through a Host boundary. The local set on
this date contains 15 SKT archives. All probes used fresh save directories
under `/tmp`; the game library and user save tree were read-only.

## CLDC data stream interfaces

The published CLDC contracts declare
[`DataInput`](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataInput.md)
and
[`DataOutput`](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataOutput.md)
as interfaces. They also declare that
[`DataInputStream`](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataInputStream.md)
implements `DataInput` and
[`DataOutputStream`](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataOutputStream.md)
implements `DataOutput`. The interfaces include the floating-point methods
added in CLDC 1.1.

The runtime already had the concrete integer, string, and array stream
operations used by the local caller. It omitted the two interface class
definitions and the streams' interface metadata. `invokeinterface` happened
to dispatch by receiver class, while `instanceof`, `checkcast`, and API scans
could not see the relationship. The correction adds the complete CLDC 1.1
interface declarations, marks both streams assignable, and supplies the four
floating-point stream bodies needed to make the declarations truthful.
`writeFloat` and `writeDouble` use the specified `floatToIntBits` and
`doubleToLongBits` forms, including canonical NaN encodings.

One local archive, SHA-256
`06c7a38343c7c48ad8ef362ffe1cd5ba2384a0b4e1cc8111acb6e2d72bf7b5e6`,
contains 111 `invokeinterface` instructions across five classes. They call
`readFully`, `readBoolean`, `readByte`, `readShort`, `readInt`,
`writeBoolean`, `writeByte`, `writeShort`, and `writeInt`. A 64-tick
instruction trace loaded the concrete stream classes but never entered those
serialization methods. The archive establishes a real link surface; the
bounded boot does not establish that its save route executed.

Repository-authored coverage now has both layers required for this JVM change.
The class fixture passes streams through interface-typed methods, checks
assignability, round-trips every primitive category, and supplies
noncanonical NaNs from Go to verify their serialized bytes. A packaged MIDlet
fixture is opened by the SKT archive loader and performs the interface round
trip from `startApp`.

## Save, progression, and sound observations

A fresh 300-tick `runskt` probe attached diagnostics, an audio recorder, and a
separate empty save root to each of the 15 archives. All 15 remained active and
painted a lit frame.

Every archive executed a storage API during this window: six used MIDP RMS and
nine used the vendor file API. Five archives made actual record or file writes
and produced 12 files in their isolated save roots. The other calls were
existence checks, opens, closes, or reads. A file created during boot may hold
defaults, settings, or authentication state, so these writes do not prove that
play progress was saved. The probe sent no menu or gameplay input and did not
perform a second launch over the files it created.

All 15 archives requested a vendor audio clip. Thirteen proceeded through
`open` and `play` or `loop`, and all thirteen produced MIDI messages at the
Host audio sink. Four also produced a WAV recording with sampled audio. The
remaining two requested a clip but did not open or play one within 300 ticks.
These results are playback-path evidence through the runtime timeline and
sink. The separate decoder sweep only proves that packaged sound resources can
be decoded; neither result establishes audibility on a physical phone.

A previously documented browser run advanced one local title through its
introduction to the first gameplay stage, continued presenting changing
frames, and accepted directional input. That observation did not verify a
save/reload cycle or physical-device audio. The new boot probe does not extend
the progression claim.

The next bounded acceptance route should start with one of the five archives
that both wrote an isolated save and reached the audio sink. The route should:

1. Enter gameplay from a fresh save and record a stable visual checkpoint.
2. Make one observable piece of progress, use the title's own save action, and
   record the exact save keys and byte changes.
3. Destroy the session, start a second session over the same isolated save,
   and reach a checkpoint that distinguishes restored progress from a new
   game.
4. Record MIDI and sampled-audio counts while that route runs. A person can
   then perform the final audible check on a phone or browser using the same
   route.

## Jlet coverage

No class in the current 15-archive set extends
`org/kwis/msp/lcdui/Jlet`; their application paths are MIDlets. The repository
fixtures cover the Jlet lifecycle, cards, vendor files, and event queue as
separate contracts, but this corpus cannot supply a real Jlet save or gameplay
route. The Jlet part of the acceptance item needs a Jlet archive placed in the
local library before it can be measured. Its first run should use a fresh
isolated save and the same write, restart, restore, and audio checks above.

## Landscape evidence

The current descriptors contain no display width, height, or orientation key.
The resource-name rule selects one archive in this set as `176x208`; it selects
none as `320x240`. Static bytecode in all 15 archives references screen width
or height. During the 300-tick probes, 12 called the width getter and 10 called
the height getter. Those calls show that titles can adapt to the dimensions a
Host supplies, but they do not identify a landscape package: this set has no
paired `320` by `240` branch, orientation string, or consistent resource-name
declaration.

The CLI can already be given `-screen 320x240`, so the next investigation does
not require a new framebuffer shape. It requires one of the packages reported
to support landscape. Once available, record its hash, inspect its descriptor,
resources, and the bytecode branch that consumes screen dimensions, then run
the same package at `240x320` and `320x240` with isolated saves. Automatic
selection is warranted only if the package carries a stable declaration that
distinguishes it from portrait packages.

## Native keyboard and IME boundary

The MIDP
[`TextField`](https://mirusu400.github.io/wipi-wiki/midp/java-api/javax/microedition/lcdui/TextField.md)
contract makes the field limit and constraints authoritative. The SKT Host
adapter exposes three targets whose contents and focus are observable: the
current `TextBox`, the selected `TextField` in the current `Form`, and a
focused `XTextField` owned by the visible canvas. A commit checks the same
object, display, selection or focus, and original value before replacing text.
It enforces input constraints, counts the maximum in Java UTF-16 `char` units,
repaints the target, and reports a Form item-state callback.

A title-owned `com.xce.lcdui.TextComponent` exposes editing operations and a
size, but no operation that reads its characters. The Host cannot form an
honest initial-value snapshot or compare a pending composition with the
current value. That target remains unavailable through native IME input until
caller evidence supplies a safe readable value or the interface contract is
extended by an observed vendor method.
