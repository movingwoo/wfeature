# SKT Java platform

`internal/platform/skt` loads SKT Java archives and composes the shared JVM,
MIDP, WIPI Java, and SKVM/vendor surfaces. [JVM](jvm.md) describes execution;
[LCDUI](lcdui.md) describes screens and drawing; [SKT history](history/skt.md)
keeps call traces, compatibility investigations, and dated acceptance results.
The separate GNEX/GVM path is documented in [sgs.md](sgs.md); its development
remains paused.

## Loading and lifecycle

An SKT container holds `<id>.jar` beside `<id>.msd`. The loader validates the
descriptor, JAR entries, class names, sizes, and paths. Every other container
entry except `.jar`, `.msd`, `.mod` and `.wmr` files is installed as the
title's own file under its bare name, so `XFile` finds it through the resource
fallback; a JAR entry of the same name wins. Secondary JARs remain installed
files. `XFile(String, String)` reads a bounded, read-only entry from a named
resource JAR; it does not interpret the second string as a file mode or add
secondary classes to the primary class path. A main class that extends
`org.kwis.msp.lcdui.Jlet` runs on the WIPI Java layer in `internal/api/wipi`,
whose classes subclass their MIDP counterparts. An archive with an `.SGS` body
starts through `StartScript` instead ([sgs.md](sgs.md)).

MIDlet lifecycle work enters through bounded event processing. Guest Java
threads run independently of Host ticks. Their first uncaught asynchronous
error is retained and returned at the next `RunPending` boundary. Terminal
transitions close JVM waits and release audio waits. An exception that escapes
a Host-entered guest callback (paint, key, serial Runnable, command, listener)
ends that callback, not the MIDlet (`uncaught.go`). Lifecycle callbacks are not
absorbed: a `MIDletStateChangeException` from `startApp` pauses until retry,
and a runtime exception forces cleanup and destroys the runtime.

Two recognized class sets recreate their Canvas and workers on every
`startApp`, despite leaving the previous workers alive on pause. Their resume
correction (`skt.resume_without_start`) retains a successful initial start;
ordinary MIDP reactivation and deferred initial starts still invoke the
callback. See the [retained-session investigation](skt-resume-2026-09-30.md).

Thread state belongs to the Thread object. Only active threads and the main
thread remain roots; completion and failed starts preserve restart prohibition
without retaining inactive roots. Untimed `Thread.join()` supports multiple
waiters, interruption, and VM shutdown. Joining a live cooperative thread is
explicitly unsupported. `VM.Close` does not wait for arbitrary Host native calls.

Pause/resume callbacks and stopped Host ticks do not prove that every guest
thread or clock freezes. Real-phone suspension and active soundtrack recovery
remain separate acceptance requirements.

## Java and device surfaces

The runtime installs its library declarations from Go. CLDC `DataInput` and
`DataOutput` interfaces, assignability, and floating-point stream methods are
covered by class fixtures and a packaged MIDlet. Float/double output uses
canonical NaN encodings. The default charset is EUC-KR: `String.getBytes`,
the byte constructors and `com.xce.io.ByteToCharEUC_KR` share one decoder.

`System.getProperty` answers the MIDP names and the handset properties `MIN`,
`m.MIN`, `m.VENDER`, `m.MODEL`, `m.MONDEL`, `m.CARRIER`, `m.COLOR`,
`m.SK_VM`, `m.EXT_SW`, `m.TYPE` and `com.xce.wipi.version`. `m.SKT_API` stays
null on purpose. The subscriber number comes from `-number` on the server or
`WFEATURE_PHONE_NUMBER`, is read on each request, and `m.CARRIER` is derived
from its prefix (`subscriberCarrier`). Five recognized license-check shapes
are adapted in a session-owned class copy; see
[SKT license checks](authentication.md#skt-license-checks).

The SKVM surface includes supported file, graphics, fixed-point math, audio,
and device-property APIs. Missing methods and deliberate stubs remain bounded
by their documented callers; a successful class lookup does not establish
complete semantics. `SISImage` answers no frame, and the 3D classes keep state
without a rasterizer. Network, SMS, calls, and phonebook APIs do not provide a
live carrier service. See [network policy](network.md).

The cheat engine works on a synthetic address space built over the object
graph (`heapmap.go`, `heapread.go`), and a write watch observes the
interpreter's own stores (`watch.go`). `runskt -cheat` attaches the console;
[cli.md](cli.md#runskt) lists the other diagnostic flags.

## Graphics and input

MIDP Canvas and high-level LCDUI paths provide the supported screen, image,
font, repaint, clipping, and input contracts. Normal clipping is count-based.
`Display.callSerially` runs one Runnable per Host pass. `XDisplay.refresh`
limits producer loops to 60 refreshes per guest second, scaled by the selected
speed. This supplies pacing for loops without sleeps; slower drawing already
counts toward the interval. See the
[gameplay QA evidence](gameplay-qa-2026-09-20.md#skt-refresh-driven-speed).
An exact Java-class fingerprint can enable the recorded inclusive `setClip`
exception; a legacy profile string alone cannot. The selected correction does
not change WIPI Graphics, `clipRect`, fills, or image dimensions. See
[compatibility registry](platform-compatibility.md).

`Canvas.getHeight()` reports the display less 16 rows (`canvasReservedRows`),
because the corpus adds those rows back; the framebuffer and the `XDisplay`
`width`, `height` and `height2` fields still answer the whole display, and
drawing is not clipped to the smaller rectangle. A WIPI `Card` reports the
display.

Repaints requested from a serial Runnable wait for the next Host pass and run
before the next Runnable. This lets paint consume input arriving between
animation updates. MIDP Canvas callbacks receive handset code `190` for the
Host's Call key (`10`); WIPI Cards receive `-10`. See the
[input and tile investigation](skt-qa-2026-09-29.md) for reproduction and tests.
The Call mapping was also exercised in two additional games; the
[cross-archive audit](skt-qa-2026-09-29.md#cross-archive-call-verification)
records the evidence and its limits. It is an observed handset mapping, not a
universal MIDP Call constant. A Canvas without MIDP commands receives the first
soft key as menu code `129`. A Jlet session translates Host keys once into the
`EventQueue` game-key values the titles compare against (`wipiKeyOfDevice`),
and its soft keys reach the Card as keys rather than commands.

The runtime has Canvas pointer handlers, but the shared SKT session does not
offer browser pointer input. Host keyboard/IME entry supports known active
editors, including an append-only vendor component whose API cannot return its
existing value. Constraints and UTF-16 limits remain authoritative, and Host
text must be encodable in KSC5601; other characters are refused. See
[native text input](native-text-input.md) for the supported targets and limits.

Display selection follows resource-name evidence. `skt.PackagedScreen` reads
path components ending in `_<width>`, counting only the widths 128, 176, 240
and 320 (a suffix such as `_120` is ignored rather than counted against them);
when every counted component agrees and at least two state it, it selects
128x160, 176x208, 240x320 or 320x480. An SKT descriptor declares no screen
size. A Host's `-screen` flag or page setting overrides the selection.
Supplying `-screen 320x240` is a diagnostic override, not proof that a
package declares landscape operation.

## Saves and sound

MIDP RMS and vendor files share the Host save boundary. `XFile` keys live
under `fs/` beside RMS. A writable open of a missing path creates the file: an
empty one that every later read sees at once — `exists` and `isFile` answer
true, `filesize` and `sizeOf` 0, a later open finds it — and that is stored no
later than the handle's flush or close or the end of the session. That is
`XFile` with the write bit, WIPI `File` `WRITE`, `READ_WRITE` and
`WRITE_TRUNC`, and `com.xce.io.FileOutputStream`, appending or not. A
read-only open of a missing path is an `IOException` and makes nothing.
Error-aware stores report read failures as guest storage exceptions. Record
decoding rejects truncation and trailing bytes. [RMS](rms.md) describes record
identity and layout; [save integrity](session.md#save-integrity) describes
Host contracts.

**A path is one file for the whole session.** Every handle on it — an
`XFile`, a WIPI `File`, the input stream a `File` opens, the
`com.xce.io` streams built on `XFile` — reads and writes the same bytes with
a position of its own, under every spelling of the path (`/save`, `save`,
`./save`). A write or a truncation is therefore seen at once by every later
read: `XFile.exists` and `filesize`, `FileSystem.exists`, `isFile` and
`sizeOf`, a new open, and a handle or stream already open. A handle with a
copy of its own would answer with the file as it was stored until that copy
was closed, and its close could store the older copy over another handle's
write.

**Saves are stored at the points the KTF, LGT and SKT file layers share, and
at no other:** the title's flush or close of a handle stores its path as every
handle has left it; a truncating open stores the empty file at the open; a
removal is stored when it is made; a quick save stores what is not stored yet
before it records anything; and the end of the session — the Host's destroy,
the title's `notifyDestroyed` or `System.exit`, or a failure that ends it —
stores every file not stored yet and retries every RMS write the store
refused. A store the Host refused leaves the file dirty, and the next of those
points writes it again; `XFile`'s flush and close answer nothing, and a WIPI
output stream reports the refusal as an `IOException`. Nothing stores on a
timer, a pause, a resume or a tick. A session a quick load replaces stores its
writes in the load's own step and nothing after it.

WIPI `File` modes are the specification's enumeration, not the `XFile` bits
of the handle underneath: 1 `READ_ONLY`, 2 `WRITE` (writing starts at the end
of an existing file), 3 `WRITE_TRUNC` (the open empties the file and stores the
empty file there and then, so a close with nothing written, or no close at
all, leaves it empty) and 4 `READ_WRITE`; any other mode is an `IOException`.
`File.read` answers -1 at the end of the file, where `XFile.read` answers 0.
`com.xce.io.FileOutputStream(String)` and `(String, false)` start the file
over as their java.io counterparts do — a truncating open, through the private
`XFile.truncate`, which leaves an existing empty file rather than a removed
one — and `(String, true)` appends; `FileInputStream.read` of zero bytes
answers 0. `XFile`'s own modes never truncate. A write that would grow a file
past 16 MiB (`xFileMaxBytes`) fails with -1, so a seek far past the end cannot
make the process allocate gigabytes; `fsavail` still reports 512 KiB.

**A removed file is gone, not empty.** `XFile.unlink` and `FileSystem.remove`
make the path absent to every later read: `exists` and `isFile` answer false,
`filesize` and `sizeOf` answer -1, a read-only open throws `IOException`, and a
handle still open on it reads an empty file. The removal stores, in one batch,
the emptied file and the path on `fs/.removed` — the key and form of the KTF
guest filesystem's list, names joined by newlines, each the path of the save
key below `fs/` — so the path stays absent in later sessions, a packaged file
of the same name included. Opening the path for writing again brings it back as
an empty file, and a write through a handle left open on it brings it back
holding what was written; the store that writes it takes the name off the list
in the same batch as its bytes. Removing a name that does not exist changes and
stores nothing: `unlink` answers 0, as it always did, and `remove` throws
nothing, where the specification has it throw `IOException`; no title has been
seen to depend on either answer. Removing a file made or written in this
session that never reached the store, and that the archive does not package,
stores nothing either, since a later session would not find it. A name with a
line break cannot go on the list, so `unlink` answers -1 for it and nothing is
removed. An empty file an earlier build's `unlink` left behind is a present
empty file: nothing records which empty files were removals. `/.removed` and
paths below it are refused, as the list's own key.

An SGS save load that cannot read `nv/data` stops the script with the error
rather than handing the title zeros it would save over its progress.

Audio requests use the guest timeline and shared sink boundary. MIDI messages
and sampled-audio buffers prove software delivery, not audible playback on a
phone. One recorded MIDlet route proves an in-game save, fresh-runtime reload,
and restoration to the same map checkpoint. That result does not cover every
archive or the Jlet path.

## Quick save and quick load

SKT Java/MIDP/WIPI sessions, and GNEX/GVM SGS sessions, are recorded between
rounds and restored through the shared checkpoint envelope, the CLI `run`
command and the page's keypad controls, without replaying startup. A load
leaves ordinary saves where they are, per
[the shared rule](architecture.md#quick-load-and-ordinary-saves). The state
recorded, the refusals and the limits are in [SKT checkpoints](skt-checkpoints.md).
A file handle's record holds its path, mode, open state and position and no
bytes; a load puts every handle on one path back on the path's one file, read
from the current saves (the removal list, the save, the packaged file), and
records written before files were shared restore the same way. A handle the
record had open for writing does not make its file again when the current
saves do not have it, since a removal made after the quick save is the later
write; a write through the handle brings the file back.

## Validation and limits

Run the [standard gates and opt-in probes](testing.md). Newly authored JARs
cover lifecycle, streams, threads, clipping, and supported input; their
sources and rebuild commands are in
[the fixture README](../internal/platform/skt/testdata/README.md). The
SKVM fixture's regeneration command remains in
[SKT history](history/skt.md#implementation-validation).

Known limits include Java SIS image decoding, absent 3D rasterizers, incomplete
vendor APIs, and no general browser pointer path. The available dated corpus
contained MIDlets rather than a real Jlet acceptance route. Jlet gameplay/save
verification, landscape package evidence, physical keyboard composition, and
audible playback still need the corresponding archives or human checks.
