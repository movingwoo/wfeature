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
`writeBoolean`, `writeByte`, `writeShort`, and `writeInt`. Two input-driven
debug traces now establish both sides of that link surface:

| Route | Actual caller evidence |
| --- | --- |
| Packaged-data load | `CLEAR` at tick 60 and `FIRE` at tick 100 reached gameplay by tick 360. The trace executed 5,318 `invokeinterface` calls through `DataInput`: 3,909 calls to `readShort` from an array helper, 134 calls in one object reader, 38 primitive-field call sites each executed 33 times in another reader, six calls in a third reader, and 15 in a fourth. These are reads from packaged game data, rather than save-file reads. |
| Options write | With each key held for 12 ticks, `CLEAR` at tick 60, `DOWN` at tick 120, and `FIRE` at tick 180 opened Options. `FIRE` at tick 400 opened the selected value, `RIGHT` at tick 520 changed it, and `FIRE` at tick 640 applied it. The apply method ran once, called a `DataOutput`-typed array helper 10 times, and executed 50 interface calls to `writeShort`. It then called the vendor file write once and produced a 295-byte options file with SHA-256 `b6ab74a001a469246c7af5f030e94384d8c3652f0dee5b71084eed8a1fdf9847`. |

Neither trace logged a warning or error, so no further stream implementation
was warranted. The only missing-class diagnostic was guest class `i`. The
archive contains guest classes `a` through `h`; its startup deliberately
tries each name through `i` with `Class.forName` and catches `Exception`, so
that diagnostic records a handled preload miss rather than a platform API
gap. The review artifacts remain under
`/tmp/wfeature-skt-api-route.cs0DHI/`.

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

A separate restart probe then used archive SHA-256
`0ed66634d3dce7eeb05ec9ca95488f507574a1c8b62f3446018cecbd5c4d2415`.
The first 300-tick process started with an empty save root and added one RMS
record. The encoded store was 481 bytes: an eight-byte record header followed
by a 473-byte payload whose SHA-256 was
`8f7b7c4436343c7a942dddbfd1d2be15668d20112d918e5d5426ef6515224d0a`.
A second process started a fresh runtime over that same root. Its diagnostics
recorded one `getNumRecords` and one `getRecord(1)`, with no `addRecord` or
`setRecord`; the save checksums remained unchanged.

The caller and trace establish that the bytes were consumed rather than merely
found. Static disassembly of the caller's `d.ad()` shows that a zero record
count builds a default string and calls `addRecord` at bytecode PC 167. The
fresh trace took the conditional at PC 34 to PC 37 and reached PC 167. The
restart trace took PC 34 to PC 174, called `getRecord` at PC 180, constructed a
String from the returned byte array at PC 183, and then parsed delimiters and
assigned the resulting values to guest fields from PC 218 onward. This proves
write, fresh-runtime reload, and guest consumption through RMS. Both runs
produced the same opening frame digest, so the record is initial/default state;
it does not prove restoration of user-visible gameplay progress.

An input-driven route over the same archive closes that progression gap. It
advanced from a fresh start through the introduction to a stable map screen,
using `FIRE` every four ticks from tick 30 through tick 1186. Later repeated
presses only reopened a nearby interaction, so the map checkpoint, rather than
the number of input events, is the progression evidence. The route then
opened the title's own menu with `CLR`, selected its sixth entry with five
`DOWN` presses and `FIRE`, then selected slot 1 and confirmed the write with
the `1` key. The save result and the independent reload are:

| Stage | Evidence |
| --- | --- |
| Before the title's save action | The initial/default RMS file was 481 bytes with SHA-256 `d2e1b6611d5cb4b6a648814502f3070a282fb608f49c1f10c9d11d8b6bdb673a`. |
| After the save confirmation | The title displayed its saved confirmation; the RMS file grew to 519 bytes and changed to SHA-256 `20f57e639370e32154b25d87b7bcd5fb18fb1d7562e3508404c02fcf126eab5b`. |
| Fresh runtime | A separate process sent `FIRE` at ticks 30 and 60 to open Continue, then `1` at tick 90 to select slot 1. The slot displayed the saved level and location; loading it called `getRecord(1)` once, did not call `setRecord`, and left the RMS checksum unchanged. |
| Restored checkpoint | The reloaded map PNG was byte-for-byte identical to the pre-save checkpoint. Both files have SHA-256 `75c83c7911ea0f8342cbf8e9f41329ecb4f79ff9dbcee122dd0482e8e6ed9e36`. |

The raw route screenshots, diagnostics, save data, and checksum files remain
under `/tmp/wfeature-skt-progress-route.2v12cT/` for this review. This result
proves visible story progress, a title-initiated save, a new process consuming
that save, and restoration to the saved map rather than the fresh-start story.

All 15 archives requested a vendor audio clip. Thirteen proceeded through
`open` and `play` or `loop`, and all thirteen produced MIDI messages at the
Host audio sink. Four also produced a WAV recording with sampled audio. The
remaining two requested a clip but did not open or play one within 300 ticks.
These results are playback-path evidence through the runtime timeline and
sink. The separate decoder sweep only proves that packaged sound resources can
be decoded; neither result establishes that a person heard sound from a
browser or physical phone.

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
object, display, selection or focus, original value, maximum size, and
constraints before replacing text. An open command menu hides the editor from
the Host and invalidates an earlier snapshot. The adapter enforces input
constraints, counts the maximum in Java UTF-16 `char` units, repaints the
target, and reports a Form item-state callback. A dedicated text-state lock
serializes these operations with background guest setters and screen paint.

A title-owned `com.xce.lcdui.TextComponent` exposes editing operations and a
size, but no operation that reads its characters. The Host cannot form an
honest initial-value snapshot or compare a pending composition with the
current value. That target remains unavailable through native IME input until
caller evidence supplies a safe readable value or the interface contract is
extended by an observed vendor method.

The packaged TextBox acceptance fixture now passes the complete WebSocket path
in automated Chromium and WebKit runs: open, Korean commit, reopen, stale
target rejection, UTF-16 length rejection, and value retention across a
pause/resume cycle. A person still needs to enter Korean with an actual phone
keyboard and a desktop IME to verify operating-system composition behavior,
focus, and the absence of unwanted browser zoom or layout movement.

## Remaining acceptance

The automated gameplay save and fresh-runtime restore route is complete. Four
requirements still depend on human observation or on archives absent from the
current corpus:

1. Listen through the browser and a phone. Decoder output, MIDI messages, and
   sampled-audio buffers prove the software path but cannot establish what a
   person hears from the operating system and speakers.
2. Enter Korean with an actual phone keyboard and a desktop IME, checking
   operating-system composition, focus, and page layout behavior.
3. Add a real Jlet archive to the local library and repeat the save, restart,
   restore, audio, and input route. The current corpus contains no Jlet
   application path.
4. Supply a package reported to support landscape and compare it at `240x320`
   and `320x240`. The current corpus has no stable landscape declaration from
   which automatic selection can be derived.
