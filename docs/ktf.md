# KTF platform

`internal/platform/ktf` loads KTF packages, executes their ARM code, and composes
WIPI services with the shared backend and JVM. The CLI and server use the same
session path. Detailed layouts, call traces, earlier package investigations,
and dated measurements are in [KTF history](history/ktf.md).

## Packages and execution

Detection reads entry names only: an `__adf__` entry marks the descriptor
package, and a `.mif` beside exactly one `.mod` marks the earlier native
package (`internal/platform/detect`).

The descriptor package contains `__adf__`, an AID-selected JAR, and a
`client.bin<decimal BSS size>` image. The loader validates descriptor fields,
ZIP paths, duplicate names, entry counts, expansion bounds, and image/BSS size;
the combined image and BSS are limited to 256 MiB. A duplicate entry is
inflated to be compared with the first and counts against the 512 MiB
expansion bound like any other. It maps the image at
`0x00100000`, enters Thumb code at `0x00100001`, and validates the returned
initialization and class metadata before using it. Saves belong to the
descriptor's PID, or to its AID when the PID is absent.

An older generation carries a relocatable module in the same `client.bin<BSS>`
entry: a header naming the BSS size and a relocation table, then a segment
whose entry is handed the platform callback table and asks it for one
interface by name. The module publishes a class table that this side links
([the older modules](history/ktf.md#implementation-the-older-modules-run-under-the-platform-and-all-three-of-them-play)).

The earlier native package has no descriptor and no JAR: a module information
file, a raw ARM module and the title's resource files. Its signature chain
identifies it as a BREW module, so BREW, not WIPI, is the specification to
consult ([history](history/ktf.md#implementation-it-is-not-wipi-at-all-this-package-is-brew)).
It has its own loader and platform interfaces inside the same package, and its
saves belong to the decimal ClassID from the information file. One local
native archive still stops on its loading screen: it blits from a surface its
start-up filled and that nothing later writes
([watch evidence](history/ktf.md#implementation-what-writes-this-for-a-title-nothing-is-unanswered-for)).

The JVM context sits beside an ordinary image so representable object-header
displacements retain canonical image class addresses during compiled access
checks. Runtime aliases and inherited field payloads are described in the
[startup compatibility investigation](history/ktf.md#startup-compatibility-2026-09-19-ktf-startup-exception-1379b56de66e).

Guest workers can park with nested Go/ARM calls intact. Host callbacks and
worker execution use bounded instruction windows. AOT nesting is bounded per
logical worker, including derived ARM calls. The JVM/ARM bridge carries the
current `jvm.Invocation`, preserving monitors, class initialization, and the
instruction budget across Java → ARM → Java reentry. A client-thread serial
callback does not consume a guest worker grant, so a self-requeueing UI cannot
starve a receiver; each round retains the configured worker limit and the
existing serial callback pacing.

A title registers the same AOT class record again and again, and a second
registration refreshes the class from the record: a vtable the title changed
in between is the one the class has from then on. What reading a record read
is kept, its spans sorted and joined by address in one buffer of exactly their
size, with the revision the VM gave the class (`AOTClassRevision`).
Registering the record again compares those spans with guest memory in place
(`Memory.Holds`); only a record whose bytes changed, or a class registered
since, is parsed and validated whole again, and its ancestors are walked only
then. A static field's last word is its value, not an offset, so a store to a
static changes nothing a read depends on, and the field's offset is zero. The
records are untrusted, so one read may note 1 MiB and all kept reads 16 MiB
(`maxAOTClassReadBytes`, `maxAOTClassReadsBytes`); a class past either is read
whole at every registration. On a light title the rereading, a byte per
memory read, was most of what the server did for it. Every KTF reader of a
terminated guest string — the names in class records, text drawn with no
length, the native C library's strings, the `strlen` and `strcpy` hooks —
shares one scan that reads a chunk at a time (`scanGuestString`) and answers
what reading a byte, or a UTF-16 unit, at a time answered; a string that runs
off the top of the address space is refused rather than carried on at address
zero, which no KTF client maps. The runtime Java method behind a
native call is found by its stub's address, both to know whether the call
answers sixty-four bits and to name it in an error that a Java exception
unwinding through it carries; it used to be found by walking every stub, which
on the title that calls natives most often was half of the server's CPU.

A native body in a current image can publish a 32-bit result in the
environment instead of `r0`: it writes the value at offset 40, then tag 2 at
offset 36 (`native_return.go`). The tag is cleared on entry, a nested scope
restores its caller's tag and value, and older modules, absent tags and other
tags keep the register return. Other tags and a wide environment return remain
unconfirmed ([evidence](history/ktf.md#ktf-qa-2026-09-21-native-environment-return-and-the-slow-case)).
Existing helper stacks retain their arena lifetime. The argument container of
a native-form call — the Host's call into a native body and an older module's
native invoke — is cleared and returned to the arena when that call returns,
so a nested call never reuses its live caller's container and repeated platform
calls do not grow the arena.

## Lifecycle and input

Sessions drive startup, pause, resume, and unconditional `destroyApp(true)`
before unwinding workers. A failing teardown callback is diagnosed and cannot
prevent termination; repeated close does not call it twice.

Keys reach cards or the guest event queue according to the active path. User
events at or above `0x5000` retain their type and two arguments; unknown
reserved event kinds below that boundary remain errors. The session generates
key repeats for descriptor packages at the handset's `KEYREPEAT` cadence
(600 ms, then every 250 ms; `internal/keypad`). A native package receives a key
as press (`0x101`), typed key (`0x100`) and release (`0x102`), and no repeat.
WIPI Java receives Host pointer events, and the Host is told a title takes
touch only when one of its classes declares its own `pointerNotify(III)Z` body;
native packages have no pointer support. An uncaught guest exception in a key
or pointer callback ends that call, as in a paint or timer task, rather than
the session.

Host keyboard/IME entry supports a title's own `InputMethodHandler` field
(append-only, identified by the keys reaching the handler the title built),
active LWC fields, the sole direct text child of a shown Shell when explicit
focus is absent, the observed non-modal KFC form containing one listened text
field, the one focused text component the top card read during its last paint,
and the WIPI-C input-method editor. Every route refuses text outside KSC5601.
The guest owns acceptance and persistence. `doModal` (LWC `DialogComponent`,
KFC `GForm`) returns at once without presenting anything: a dialog answers OK,
or timeout when it has no buttons, and the call is counted in diagnostics, so
a field reached only through `doModal` gets no Host text input. See
[native text input](native-text-input.md) for target ownership and validation.

## Graphics and sound

The runtime provides guest framebuffers, clipping, pixel operations, images,
text, and the implemented WIPI drawing surface. XOR mode applies to every
drawing operation, `drawImage` included
([history](history/ktf.md#implementation-xor-mode-is-how-one-title-family-draws-everything)).
`Graphics.setAlpha` is recorded and read back but not blended, and no image
reports itself animated. KTF Java printable ASCII uses five-pixel advances;
Korean syllables retain ten-pixel advances. Measurement, anchors, and
rendering agree. NUL padding draws nothing and advances zero. Native WIPI-C
text retains its own metrics.

Image draws, `MC_grpCopyArea` and `MC_grpCopyFrameBuffer` copy a row at a time:
one read of each source span of up to 256 pixels, one of the destination when
a pixel operation combines them, and one write per run of drawn pixels
(`drawFramebufferCopy`). The result is the pixel-at-a-time walk's to the byte,
transparency marks and pixel-operation calls included
(`TestWIPICCopyRowsMatchThePixelWalk`). The walk still draws what a whole span
could draw differently: the rest of a span from the first pixel pair a guest
pixel operation has not answered yet (only the walk runs guest code), a copy
inside one surface whose source span reaches into its destination from the
left, which `MC_grpCopyFrameBuffer` smears as before, a transparency mask that
is both the source's and the destination's and marks pixels the same row tests
later, and a span that faults. Walked, a 32×32 sprite
cost 33 µs and a 240×320 screen copy 2.5 ms on an M1; by rows, 1.3 µs and
19 µs.

The screen is the Host's choice, 240×320 by default (`-screen WxH` in the
CLI). The `__adf__` `DisplaySize` field is not used: in the measured local
set, twelve of the thirteen titles declaring `176*220` draw across the whole
240×320 screen ([history](history/ktf.md#implementation-the-screen-is-a-hosts-answer-and-it-has-to-reach-both-surfaces)).

Partial LCD flushes copy the requested region into a retained Host image without
changing their source. Pixels outside the region survive. An unchanged Card
paint preserves that image; changed screen bytes present normally. An explicit
flush during paint is not overwritten by another implicit full flush. Forced
worker slices do not request unsolicited Host paints; repaint, yield, and idle
paths still paint.

Optional intermediate frames reach the Host while guest callbacks are running.
They are owned copies, sampled at most once per 1/60 wall-clock second and
sent without waiting for a consumer. The browser uses its existing PNG queue.
See [session presentation](session.md#presentation-and-audio).

Image surfaces — a mutable image's, the one a decoded image is rasterized into
on its first masked, XOR or `Image.drawImage` draw, and the copy
`createImage(Image)` and `createSubImage` make — and Clip state use weak object
ownership. Graphics keeps its Image reachable. Collection releases orphan
surfaces and stopped audio; playing orphans remain until playback finishes,
including repeating playback. A quick save prunes the orphans a round would
prune before it records the surfaces.
Shared audio timing and borrowed-buffer rules are in
[architecture.md](architecture.md#shared-runtime-services). Java and WIPI-C clip
pause/resume, device and clip volume, and Java `PlayListener` events are applied
(see [audio](audio.md)); a per-source mute is remembered and read back but
silences no clip, and the Java `Volume` mute extensions remain stubs.

Line, polygon and rectangle-copy calls walk only the part of their range that
lands on the target surface, so guest coordinates or sizes at the integer
extremes draw what lands and return. A framebuffer record whose width does not
fit its bytes per line is refused.
Arc and rounded-rectangle calls, Java and WIPI-C, walk the same way. Image
decoding reads the header first and refuses a picture of more than 16,777,216
pixels (`backend.MaxFramebufferPixels`, the rule SKT already applied) before
allocating, and the checksum repair for a picture with a wrong zlib trailer
stops at the size its header implies.

## Files and records

File input/output streams and direct File operations share one cursor. Stream
close does not close the underlying File. Reads report storage I/O failures.
Java and WIPI-C file/record mutations stage bytes, cursors, handles, and deletion
ledgers before publishing changes. Multi-key changes use `SaveBatchStore`.
A guest `DataOutputStream` call over a `File` stores what it wrote once, as the
call returns, rather than once per byte; a store it cannot make throws
`IOException` from that call and leaves the file as it was. Every write is still
stored before the guest's own call returns. WIPI C file and record-database
handles are reused once a session has issued 4095 of either; an open is refused
only while 4095 handles of its table are held at once. The WIPI C file table refuses a name that is
not a save key of its own (`/`, `.`, a `..` component) in every slot, and a
name with a line break where it would be written into a list (remove, rename,
mkdir); `MC_fsGetFileStat` reads through the removal list like
`MC_fsIsExist`.
The earlier native package retains its existing buffered write/flush policy.
See [save integrity](session.md#save-integrity) for failure recovery and limits.

A record database the archive carries, as one `qtcdb` file or as a `qtpdb`
index beside its data file, is readable through both the WIPI C record table
and Java `DataBase`; opening it writes no save. A deleted name is listed in
`rdb/.removed` or `jdb/.removed`, so neither the emptied save nor the packaged
copy returns ([history](history/ktf.md#implementation-a-packaged-database-is-packaged-in-two-shapes-and-the-commoner-one-was-unreadable)).

A saved file can share its name with a packaged resource directory. A lookup
below that saved file falls through to the archive without recording a storage
failure. The bounded local slot service also accepts the first, zero-based
slot ([evidence](history/ktf.md#ktf-save-slots-2026-09-23)). The service answers the
creation receipt with "no character yet", so the title asks the player for a
name and registers it with one more request; see
[A name the title asks for](history/ktf.md#implementation-a-name-the-title-asks-for).

Network access is refused. A WIPI C `connect` that registers a callback is
accepted and then fails through that callback; recognized local compatibility
services are listed in [network](network.md) and
[authentication](authentication.md).

## Quick save and quick load

All three kinds of KTF package can be recorded between two rounds and restored
later: the Java/AOT descriptor package, the older descriptor module and the
native package. A checkpoint is execution state only. A load leaves the title's
saves as they are, and the restored title reads and writes them there; the
rule and its accepted limits are in
[Quick load and ordinary saves](architecture.md#quick-load-and-ordinary-saves).
Particular to this platform:

- The descriptor runtime stores every write before the call that made it
  returns, so neither step has anything to store first. It keeps host copies
  of the saves: the WIPI C file table, the record databases, the Java
  databases, the bytes behind each `File` object, the table of names the
  session wrote and the five removal and directory lists. A record names them
  and holds none of their bytes, and a load fills every one from the store.
- After a load a name has one host store in each table, shared by every handle
  restored for it. A name the store has nothing for keeps its restored store
  empty and findable by name, so the next open, create, rename or write of that
  name takes the same store rather than making a second one.
- A Java database the title deletes while a `DataBase` object holds it is kept
  the same way in a running session: the next open of the name takes the store
  that object holds, and a write through the object makes the database again
  under that store. An open previously made a second store, and the two wrote
  their own record lists over each other's save. The WIPI C record table
  closes a deleted database's handles instead, so it has nothing to keep.
- `FileSystem.list` and `DataBase.listDataBases` answer the packaged names plus
  the names this run knew, without reading the disk. After a load they describe
  the run the checkpoint was taken in.
- A `File` opened in the truncating mode, and a native object opened in the
  mode that empties the file, take at most what they had written after a load,
  from the front of the file as it is. Every other object, including one whose
  open made the file, takes the whole file. The WIPI C file table empties a
  file in the store at the truncating open itself, so its handles are cursors
  on the file as it is and nothing of the truncation is left to carry.
- A native package's write waits in the platform until a file is closed or a
  frame ends. A quick save and a quick load store what is waiting first, and
  are refused when the store refuses it. A name the ordinary flush could not
  store is retried at the next quick step and when the session closes; the
  ordinary boundaries still make one attempt each.
- Reads and writes key a native package's file by its lower-cased base name,
  with a backslash read as a separator. Writes formerly did not split on a
  backslash, so a file written under such a name was refused and never saved.
- When two package entries share a lower-cased base name, a native open takes
  the one whose entry name sorts first, on every run.
- A quick save can be refused with "KTF media Clip owner is invalid or
  requires collection" when the title has dropped a Clip whose host record the
  next tick's collection has not pruned yet; a playing orphan is kept until it
  ends. The game carries on, and a later quick save succeeds once the record
  is gone. 0.5.2 refused the same moments.
- A native record names the screen its session ran at, and the restored
  session takes that size, from which the Host sizes its frame and scaling
  buffers. The record's screen is therefore held to 1,024 a side, the largest
  either Host starts a session at, as on LGT; an image the title decoded keeps
  the wider 4,096 bound.

## Diagnostics and limits

Use [CLI commands](cli.md) for `runktf`, `ktfdump`, routes, imports, profiles,
and memory watches. `go run ./internal/tools/apiscan -games <dir>` reports, per
archive, the platform classes a title names that the runtime does not answer,
without running it. [Testing](testing.md) describes opt-in local probes and
what each acceptance level establishes.

The checked inventories in `internal/platform/ktf/testdata/` are authoritative
for registered Java surface gaps and fixed-value stubs:
`wipi_java_surface.txt`, `wipi_java_gaps.txt`, and `wipi_java_stubs.txt`.
A registration or static reference is not proof that a real caller executes it.
The earlier native package's reports of the text a title drew, the files it
opened and the resource requests nobody answered keep their first 4096 entries,
as its ordered call log keeps its first calls. A second listener registration on
one of its event sources replaces the first, since only one is ever called.

Known limits include animated image playback, unapplied graphics alpha and
WIPI-C context transparency and origin fields, unpainted LWC components
(deliberately: no record of the original runtime's components exists),
dialogs answered without being shown, the native archive that stops on its
loading screen, and the lack of archive evidence for automatic landscape
selection. Published instance-field divergence is diagnosed rather than
repaired using an invented precedence rule. The full list with reasons is
[Deliberately incomplete](history/ktf.md#implementation-deliberately-incomplete).
Corpus startup counts do not establish complete gameplay or save restoration.
