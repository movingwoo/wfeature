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
initialization and class metadata before using it. The callbacks table its
initialization is handed fills slots 0 to 2, 4 to 6, 8 and 11, and slot 20,
which one runtime generation asks for an object's class record; the others stay
zero, as nothing that runs calls them
([history](history/ktf.md#ktf-new-archives-2026-11-03-object-class)). Saves
belong to the descriptor's PID, or to its AID when the PID is absent.

An older generation carries a relocatable module in the same `client.bin<BSS>`
entry: a header naming the BSS size and a relocation table, then a segment
whose entry is handed the platform callback table and asks it for one
interface by name. The module publishes a class table that this side links
([the older modules](history/ktf.md#implementation-the-older-modules-run-under-the-platform-and-all-three-of-them-play)).
Linking moves a subclass's instance fields past its superclass's when both are
the module's own, and leaves its statics alone: a static's last word is its
value, not an offset
([history](history/ktf.md#ktf-new-archives-2026-10-21-statics)).

The Java classes this side publishes (`runtimeJavaClasses`) expose the shared
class library to compiled titles. On 2026-11-05 they gained the CLDC members
LGT already answered and two classes, `Writer` and `OutputStreamWriter`
([history](history/ktf.md#ktf-parity-fill-2026-11-05)). A new member takes the
next slot of its class's table and an override takes the slot of the member
above it, so a title's own subclass, which numbers its methods from the end of
the parent's table, is unaffected; the relocatable modules lay their own
numbers over the parent table, and none of them extends a class that gained a
member. `Class.newInstance` builds a title's own class the way the main class
is built. `Timer`'s Date forms count the delay from the guest clock, so a date
already past is due at once, like a zero delay. A session restored from a
checkpoint taken before a member existed keeps the class records it saved: a
title's call to that member is the same "KTF AOT method … was not found" error
the earlier build gives, until the session restarts.

The earlier native package has no descriptor and no JAR: a module information
file, a raw ARM module and the title's resource files. Its signature chain
identifies it as a BREW module, so BREW, not WIPI, is the specification to
consult ([history](history/ktf.md#implementation-it-is-not-wipi-at-all-this-package-is-brew)).
It has its own loader and platform interfaces inside the same package, and its
saves belong to the decimal ClassID from the information file. One local
native archive still stops on its loading screen: it blits from a surface its
start-up filled and that nothing later writes
([watch evidence](history/ktf.md#implementation-what-writes-this-for-a-title-nothing-is-unanswered-for)).
A later module draws points, lines and circle outlines through BREW's shape
interface (`0x1002001`) in a pen, point size and rectangle clip it sets there,
measures text with the display's `+0x0c` as the text call draws it, counts its
text in halfwords, and clears with a fill in no colour, which takes the ground
colour item 2 ([history](history/ktf.md#ktf-native-open-and-shapes-2026-10-21-drawing)).
A native colour is BREW's RGBVAL, `0xBBGGRR00`, in both generations
([history](history/ktf.md#ktf-native-open-and-shapes-2026-10-21-review)).
The flat table below the module is BREW's helper table and the object handed
to the entry is its shell, slot for slot. Beside the copies the table answers
a later module's `strcmp`, `atoi`, `memcmp`, the double arithmetic `f_op` and
`f_cmp`, STREXPAND, GETTIMESECONDS and GETJULIANDATE, and its allocator
answers a request for nothing with null, as BREW's heap does. The shell
answers ActiveApplet with the title's own ClassID, and its `+0x30` is
CancelTimer, which earlier builds ran as a request to be called once more. The
image factory is SetupNativeImage: beside a later module's image it fills the
image's description and sets the flag saying the image is a block of its own.
The 2005 module, which is not in the local corpus, keeps the answers it has
always run with from those helpers and that factory
([history](history/ktf.md#ktf-new-archives-2026-10-21-helper-table)).
A later module's image is BREW's IDIB, 5-6-5 pixels behind a header in one
block, and the blit reads any IDIB's pixels from guest memory, so what a title
draws into one, or builds from one's header, is what it blits; the 2005
module keeps its own object
([history](history/ktf.md#ktf-new-archives-2026-10-21-dib)). The sound and the
timed service end on their own clocks whether or not a frame is registered
([history](history/ktf.md#ktf-new-archives-2026-10-21-race)). The network
manager opens a socket that refuses to connect, and never answers a lookup
([history](history/ktf.md#ktf-new-archives-2026-10-21-network)). Text follows
BREW's alignment flags against its background rectangle and is clipped to it
([history](history/ktf.md#ktf-new-archives-2026-10-21-text)), and the shape
interface also fills rectangles
([history](history/ktf.md#ktf-new-archives-2026-10-21-fill)).

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
shares one scan that reads a chunk at a time (`wipicrt.ScanGuestString`) and answers
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
before unwinding workers. The main class's constructor and `startApp` run under
the step allowance every Host service call has, so a start that decompresses
its resources past one window continues
([history](history/ktf.md#ktf-new-archives-2026-10-30-start)). A failing
teardown callback is diagnosed and cannot prevent termination; repeated close
does not call it twice.

Keys reach cards or the guest event queue according to the active path. User
events at or above `0x5000` retain their type and two arguments; unknown
reserved event kinds below that boundary remain errors. The session generates
key repeats for descriptor packages at the handset's `KEYREPEAT` cadence
(600 ms, then every 250 ms; `internal/keypad`). A native package receives a key
as press (`0x101`), typed key (`0x100`) and release (`0x102`), and no repeat,
in BREW's virtual key codes: the pad 0xE031–0xE034, select 0xE035, clear
0xE030, the digits from 0xE021, star 0xE02B and hash 0xE02C; soft keys, send
and volume stay unmapped
([history](history/ktf.md#ktf-native-open-and-shapes-2026-10-21-keys)).
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
and the WIPI-C input-method editor; when none of them names a field, the same
routes are asked again under relaxed rules, so a field on screen is offered
even when the title gives no exact evidence for it. Every route refuses text
outside KSC5601.
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

A WIPI-C graphics context is the handset's `0x40`-byte record of words —
clip, colours, alpha, origin, pixel operation and its parameter, font and style,
the origin ahead of where its index number would put it — because titles read
it directly, and setting its alpha installs the platform's own blend as the
pixel operation and the alpha as its parameter, as the specification says; a
title's own operation set after it replaces the blend. A pixel operation's
answers are cached by the pixel pair; an operation whose code reads the title's
memory has them dropped at every published frame. A session restored from a checkpoint taken before 2026-11-17
keeps the earlier 52-byte record its contexts were written in
([history](history/ktf.md#ktf-new-archives-2026-11-17-graphics-context)).

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
CLI) where the archive gives no answer. A start that names no screen runs on
the handset the `__adf__` declares in `DisplaySize` (`176*220`, `240*320` or
`240*400` locally; `ktf.PackagedScreen`). A descriptor without the field or
`ResizeType` is an earlier revision of the format and starts on 176×220; one
with a `ResizeType` and no `DisplaySize`, one whose fields ran past the 256 the
parser keeps, and a field that is not two whole numbers in 32..1024 either side
of `*` answer nothing. A Host that names both sides keeps them (the page's
`📱 화면` too; anything else names nothing), and a quick save restores the
screen it was taken on. A 176×220 package started on 240×320 keeps its
pictures in a corner while its background and text spread over the rest,
which no reading of the frame can tell from a title that uses the room
([history](history/ktf.md#implementation-the-handset-a-title-was-packaged-for-is-in-its-descriptor)).
The earlier package carries no descriptor and starts on 240×320.

Partial LCD flushes copy the requested region into a retained Host image without
changing their source. Pixels outside the region survive. An unchanged Card
paint preserves that image; changed screen bytes present normally. An explicit
flush during paint is not overwritten by another implicit full flush. Forced
worker slices do not request unsolicited Host paints; repaint, yield, and idle
paths still paint.

One flush is not retained against the paint the title asks for after it: a
flush of the whole screen out of the screen framebuffer, followed by the
title's own `MC_grpRepaint` or `Card.repaint`. That paint presents the screen
whether or not it drew. A title that draws each frame straight into the screen
from a timer, asks for a repaint and has a paint entry that draws nothing
depends on this: retained, its one whole-screen flush stayed on the Host for
the rest of the run. A paint nobody in the title asked for, the Host's round
paint or the first paint of a card that was shown, still keeps the flush
([history](history/ktf.md#ktf-whole-screen-flush-2026-11-21)).

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
Arc and rounded-rectangle calls, Java and WIPI-C, walk the same way, and so
does the WIPI-C polygon fill (`curve.FillPolygonWithin`). Image
decoding reads the header first and refuses a picture of more than 16,777,216
pixels (`backend.MaxFramebufferPixels`, the rule SKT already applied) before
allocating, and the checksum repair for a picture with a wrong zlib trailer
stops at the size its header implies. A picture handed over without the end
of its `IEND` decodes, as a handset's decoder draws it
([history](history/ktf.md#ktf-new-archives-2026-11-03-png-end)).

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
mkdir); `MC_fsFileAttribute` reads through the removal list like
`MC_fsIsExist`. Table 7 follows the specification's `MC_fs*` order in every
slot: 11 is `MC_fsTotalSpace` and 15 is `MC_fsTell`, and its directory slots
are `MC_fsRmDir` 9, `MC_fsList` 10, `MC_fsSetMode` 13 and `MC_fsGetCounts` 14.
Files, directories, space and Java databases answer as
[the WIPI storage contract](wipi-storage.md) has every platform answer: a
directory is the root, a recorded `mkdir` (`db/.dirs` for C, `fs/.dirs` for
Java) or a name a file lies below; names are at most 255 bytes; a title has
1 MiB less what it stored; a Java database keeps its creation record size in
`jdb/.sizes`. The record table's `MC_dbSortRecords` orders by bytes or through
the title's own compare and filter functions, and `MC_dbGetAccessMode` answers
the private area ([history](history/ktf.md#ktf-storage-spec-2026-10-22)).
The earlier native package retains its existing buffered write/flush policy.
Its open takes BREW's modes: only create (4) and append (8) make a missing file,
and read-write (2) refuses one — and refuses an empty file the package does not
carry and the session has not written, which builds before 2026-10-21 made for
every such open; the empty file is kept
([history](history/ktf.md#ktf-native-open-and-shapes-2026-10-21-open)).
A later module seeks from the end the way BREW does, by an offset that is
negative to move back; the 2005 module's end-relative seek keeps its own
wrapper's arithmetic
([history](history/ktf.md#ktf-new-archives-2026-10-21-seek)).
See [save integrity](session.md#save-integrity) for failure recovery and limits.

A record database the archive carries, as one `qtcdb` file or as a `qtpdb`
index beside its data file, is readable through both the WIPI C record table
and Java `DataBase`; opening it writes no save but Java's note of its record
size in `jdb/.sizes`, and a store that refuses that note does not fail the open
([review](history/ktf.md#ktf-storage-review-2026-10-22)). A deleted name is listed in
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

### BREW packaged certificate

Two BREW modules call byte offset `0x08` on interface `0x103028a`. The
module passes the object in `r0`, the record address in `r1` and its length in
`r2`. A trace of both modules showed the complete 74-byte packaged record
at that address. Application 20714 reads `c2sgames.bin` and application
23682 reads `gbxcerti.dat`.

The runtime recognizes each module through the compatibility registry and
checks its MIF application ID and the SHA-256 digest of its own packaged
record. The interface returns zero only when the guest presents exactly
those bytes. An unknown module, changed or missing resource, swapped record
or unreadable guest buffer receives status one. One guest reports a pending
one-time authentication request and the other offers network authentication.
The session reports `ktf-brew-packaged-certificate` when the module and MIF
select this verifier, including when its record fails. With `-no-auth` the
interface answers one, letting the guest take its renewal path. A quick
load for either registered module requires the same authentication setting.
These checks establish an offline match to two archived installations.
Verification of a newly issued certificate would require the issuer's
verification mechanism. [Observed status codes](history/ktf.md#implementation-a-certificate-and-the-object-both-later-archives-create)

### Owner record

One registered descriptor package keeps a 12-byte owner record in its record
container and compares it with `PHONENUMBER` when it starts. Its capture
carries the container, so a first launch reads the record of the handset it was
captured from and stops at a failure notice. With the implicit handset the
session accepts that comparison and reports `ktf-save-subscriber`; a configured
handset and `-no-auth` run it as packaged.
[Archived subscriber records](authentication.md#archived-subscriber-records)

### Save owner

Another registered descriptor package begins every save with the 12-byte
subscriber that wrote it, inside the save's own encryption, and its reader
compares that field with `PHONENUMBER`. A save of another handset is treated as
no save: the title deletes the file and writes its defaults in its place,
without a notice. Its capture carries nine such saves, so a first launch
replaced all of them. With the implicit handset the session accepts the
reader's comparison and reports `ktf-save-subscriber`, and the title continues
from the capture's saves. A configured handset gets the same acceptance: the
title's answer to a save another number wrote is to delete it without a
notice, and the handset setting is shared by every title, so a number set for
another one would delete every save of this one, the player's own among them.
`-no-auth` runs the comparison as packaged, and the title starts a new game
over any save written under another number.
[Archived subscriber records](authentication.md#archived-subscriber-records),
[history](history/ktf.md#ktf-first-launch-2026-11-21-save-owner)

<a id="save-subscriber-key"></a>

### Save subscriber key

One registered descriptor package enciphers every save under a key it derives
from `PHONENUMBER`: strtol of the number after its first character. A save that
does not decipher to a matching CRC is no save, so the options record is
rewritten with defaults and a slot is offered empty. Its capture's options and
three saved games are all enciphered under key 0, the key of a handset that
answered no digits after the first character; the implicit handset's key is
another. With the implicit handset the registry fix `ktf.save_subscriber_key`
presents each of the capture's saves that deciphers under key 0 to the guest
enciphered under the session's key, with the same salt and content, and the
session reports `ktf-save-subscriber`. The archive is not changed and a save
the title writes is its own. A configured handset (`guest-managed`) and
`-no-auth` read the capture as packaged, and the title starts over. A quick
load presents the capture the way the recorded session was started, whatever
the handset setting is when it loads. Before anything is presented the session
checks the title's decoder, the constants of its cipher and the two loaders
that use it in the packaged client image, whose code the client does not
relocate, and a registered client whose code differs fails starting. A
configured handset gives the title another key altogether: it reads neither
the capture's saves nor the ones written on the implicit handset, rewrites its
options record and offers empty slots until the setting is cleared.
[History](history/ktf.md#ktf-first-launch-2026-11-21-save-subscriber-key)

<a id="save-file-time"></a>

### Save file time

One registered descriptor package records in its save the time
`MC_fsFileAttribute` gave the file when the save was written, and deletes the
save, writing its defaults, when the file's time differs from that by more
than 5000 seconds at the next load: a copied save carries a time its file no
longer has. This platform answers a time of 0 for every file, so the title's
own saves pass and the one its capture carries, which holds the original
handset's time, never does; the title reported it as damaged and started over.
The registry fix `ktf.save_file_time` sends the comparison's delete path to
the exit a matching time takes. It applies for every handset and with
`-no-auth`, since nothing on the path concerns a subscriber. Real file times
are not answered instead: every save this title wrote here holds 0, and the
first load after such a change would delete them.
[History](history/ktf.md#ktf-first-launch-2026-11-21-save-file-time)

<a id="withheld-private-files"></a>

### Withheld private files

A capture holds whatever was on the handset when it was taken, and that is not
always a consistent set. The registry fix `ktf.withheld_private_file` keeps a
private file from the guest when both its name and its SHA-256 are recorded for
the client: one capture's options record lists two saves, and the capture
carries neither. Another capture of the same client keeps its own record. With the record the
title's menu opens on Continue, and a listed save starts a character without a
level or health. Without it the menu opens on a new game with every slot empty.
The archive is not changed, the capture's other private files stay, and a
record the title writes itself is read as usual.
[History](history/ktf.md#ktf-whole-screen-flush-2026-11-21)

<a id="null-page-memory"></a>

### Null page memory

The first page of the guest address space is not mapped, and a guest that reads
or writes through a null pointer stops with the faulting instruction in the
error. That is the general rule and it stays: such an access is usually this
platform's own defect showing up late, a call answered with null where a
handset answers an object.

The registry fix `ktf.null_page_memory` maps that page as zero-filled read-write
memory for one recognized client, the save-owner package above. The routine
that writes a save's defaults looks a value up in a data table the title loads
later, so it reads the table's row count at offset `0xc` of a null pointer,
fails its own range check, and stores its error code at offset `0x10` of the
same pointer before answering zero. The title runs that routine inside
`startApp` for a save it does not accept, which is where the guest stopped. On
the mapped page the lookup takes its own failure path and the title goes on to
its first screen with default saves. A read-only page is not enough: the store
follows the read.

The mapping is selected by the client's digest alone, for every handset and
with `-no-auth`, because the routine is the title's own answer to a foreign
save rather than part of a comparison this platform accepts. It is part of the
session's memory, so a checkpoint carries it. No handset was observed running
this path; that one completes it is inferred from the title shipping it as its
ordinary answer to a save it does not accept.
[History](history/ktf.md#ktf-first-launch-2026-11-21-save-owner)

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
  session wrote, the four removal lists, the two directory lists and
  `jdb/.sizes`. A record names them
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
- `FileSystem.list` and `DataBase.listDataBases` read the saves in the store as
  well as the packaged names and the names this session wrote or opened, as
  [the WIPI storage contract](wipi-storage.md) has every platform list them.
  After a load they answer the saves as the store has them then, not the run
  the checkpoint was taken in.
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
- A native record wraps that record with the shape interface's pen, point size,
  clip and fill. A record without them, as every build before 2026-10-21 wrote,
  still loads with the interface a title starts with
  ([history](history/ktf.md#ktf-native-open-and-shapes-2026-10-21-checkpoint)),
  and one with the pen, size and clip but no fill loads with the fill off
  ([history](history/ktf.md#ktf-new-archives-2026-10-21-fill)). A socket a
  title opened is an interface object and is carried as one.

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

Known limits include animated image playback, unapplied Java graphics alpha,
the WIPI-C context's transparent pixel and origin fields, which KTF draws do not
apply (an SKT Clet's image draws apply the transparent pixel), unpainted LWC
components (deliberately: no record of the original runtime's components
exists),
dialogs answered without being shown, the native archive that stops on its
loading screen, a later native archive drawn for a 128x160 handset that has to
have that screen chosen
([history](history/ktf.md#ktf-new-archives-2026-10-21-limits)), and the lack
of archive evidence for automatic landscape selection. Published
instance-field divergence is diagnosed rather than repaired using an invented
precedence rule. The full list with reasons is
[Deliberately incomplete](history/ktf.md#implementation-deliberately-incomplete).
Corpus startup counts do not establish complete gameplay or save restoration.
