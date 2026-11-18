# SKT native (WIPI-C)

The SKT carrier ships a third kind of package beside its MIDP/SKVM Java and its
GNEX/GVM scripts: a native WIPI-C program. `internal/platform/skt` detects and
opens it, and `internal/platform/skt/clet` runs it. A WIPI-C program with no
Java around it is a Clet: the platform calls it at six entry points and it
reaches the platform through a root table of interface tables, with no Jlet, no
interpreted bytecode and no card stack. An SKT executable calls almost the same
platform functions the KTF C titles do under almost the same numbers, so a Clet
runs on the WIPI-C engine KTF titles run on, `internal/platform/wipicrt`
([architecture](architecture.md#the-wipi-c-engine)). What `clet` holds is the
Clet's own: the root table and its numbering, the C library, the six entry
points, the platform's values for the engine, and its checkpoint codec. It
imports neither `internal/platform/ktf` nor `internal/jvm`, and neither does
the engine. [SKT history](history/skt.md) keeps the static analysis the
implementation started from.

## The package

A native package is a handset's installed application directory, copied whole:
one directory named by the title's ten-digit program number, holding
`<id>.daf` (the executable), `<id>.mod` (the descriptor), `<id>.wmr` (the icons
as BMP records), and the title's own data and save files. Detection claims it
when exactly one `.daf` sits beside a `.mod` of the same name; the pair is the
marker, because a `.daf` alone could be anything and a `.mod` alone is the Java
package's descriptor too. The test runs after every named marker and the class
scan, so an archive another shape already claimed keeps its answer
(`internal/platform/detect`). The descriptor names no title, screen or main
class, so the executable is what is read. The identity for the save directory
is the program number.

Every zip is bounded the way the Java container is: the input, the entry count,
each entry and the total read (`skt.OpenNative`). The title's files are filed by
their path below the executable's directory, which is the name the program
opens them by.

## The executable

The `.daf` lays out as every local example does:

- A 0x20-byte block whose word 0 is `0x20`, then a second block at 0x20 whose
  length is at `0x28`. Offsets `0x3c` to `0x50` hold the sizes of the code
  with its read-only data, the initialised data, the BSS (which is not in the
  file) and the relocation table, a word that is zero in every example, and the
  size of an embedded resource archive. Offsets `0x58` to `0x6c` are six
  offsets into the code — the entry points, odd for Thumb code and
  word-aligned for ARM code: one local title is built entirely as ARM.
- The second block is 0x50 bytes in most examples, and the regions follow the
  header back to back from `0x70`, BSS aside. Two examples have a 0x6c-byte
  block: the same sizes and entries, then at `0x70` the sizes the regions are
  stored at, and the regions from `0x8c`. Their code is stored
  zlib-compressed — stored smaller than its size — and must inflate to exactly
  that size; every other region is stored at its own size, and a layout that
  stores one otherwise is refused. In every example the stored sizes add up to
  the file length exactly. A layout that does not is refused.
- Code and read-only data are linked at zero; data and BSS at `0x01001000`. The
  first data word is a marker (`0xCAFECAFE`) the loader overwrites with the
  platform's root table pointer.
- The six entries are the start, a teardown, two that post the title's own
  pause and resume events, an empty paint, and the event handler that takes
  `(type, param1, param2)`. Only the start is required.

The fourth region is a relocation table: two byte counts, then a list for the
code region and one for the data region. Each entry advances an offset from its
region's start to the next word holding an address, and its first byte's bits
say how far (a low nibble, or a 12-, 20- or 28-bit delta) and what the word
points into (a data address, a code address, or a target less its own address).
Decoded this way every entry in every example lands on a word pointing where the
entry says, so both regions are relocatable and a loader can place them
anywhere. `Load` places the code at the KTF image base and the data at the same
distance from it they were linked at, adding the base to every code and data
address the table lists and leaving the self-relative words alone; it zero-fills
the BSS after the data. Out-of-range or misaligned entries are refused.

The resource archive opens `{0x10, 1, 0x10, "KO", directory size, 0, count}`,
then `count` pairs of offset and size, then `count` NUL-terminated names; the
files follow the directory, and the offsets count from there. One example's
directory lists 350 slots for 246 files: the slots after the last file have no
offset, no size and nothing left in the name area, and are read as unused. An
unused slot ahead of a named one is refused. The directory's order is kept,
because a resource's identifier is its position in it (below).

## Calling the platform

There is no SVC and no imported name. Every call loads the word at `0x01001000`
(the root table the loader wrote), a table pointer at `root + 4*slot`, a
function at `table + 4*function`, and branches through a `bx` veneer. The root's
slots and their meaning, read off the local examples' call sites:

| slot | table | notes |
|---|---|---|
| 0 | kernel | numbered as the KTF kernel interface on every slot through 31 a title calls; 37 (a setting by name, accepted), 50 (a browser launch, refused) and 63 (a text whose digits one title compares with 11000, refused, which leaves that title on its own default) are the handset's own |
| 1 | graphics | the KTF graphics table through `MC_grpPostEvent` at 36, then the input-method functions and the handset's own additions, among them 54, a title's list of font faces, refused: there is one face and no name for it, and the title keeps the font it has |
| 3 | filesystem | the specification's `MC_fs*` order, with `MC_fsIsExist` and `MC_fsTell` at 15 and 16 exchanged from the KTF table |
| 4 | misc | `MC_miscBackLight` at 0 |
| 5 | network | the specification's order, refused the way a KTF title's is |
| 6 | utility | byte-order and address conversion |
| 7 | media | named by its callers, a different numbering from both the specification and the KTF table; 31 is the default volume of a category named by text (`"GENERAL"`), answered with the device volume |
| 9 | serial | refused |
| 10 | phone | refused |
| 11 | terminal resources | the phonebook; refused |
| 14 | C library | `string.h` 0–18, `stdlib.h` 19–24, `time.h` 25–30, in the specification's list order |

Slots 2, 8, 12 and 13 are not reached by any local title. The function numbers
inside a table are the specification's, which the KTF C runtime follows too;
only the order of the tables in the root and the few exceptions above differ, so
`handleCletCall` routes a call to the handler the engine serves for it
(`internal/platform/skt/clet/clet_tables.go`).

The indirect handle is where the platform's data model differs from KTF's. A
memory identifier's data pointer (`MC_GETDPTR`) and a framebuffer's pixel
pointer are the identifier's first word on SKT, where they are eight bytes past
it on KTF; one SKT title copies a resource out of `*id` and reads a
framebuffer's fields at `**id`. The engine reads the layout from the platform's
`Platform.HandleFirstWord` and `Platform.HandleData`, so the payload data starts
twelve bytes in on both and only the first word differs
(`internal/platform/skt/clet/engine.go`).

A context's pixel operation is the other difference: an SKT Clet's operations
take the pixel already on the surface first and the one being drawn second,
where a KTF title's take them the other way round
(`Platform.PixelOpDestinationFirst`;
[history](history/skt.md#skt-pixel-operation-order-2026-11-17)). The graphics
context record itself is the same word record on both.

An SKT handset answers these calls its own way too
([history](history/skt.md#skt-new-archives-2026-11-18)):

- `MC_knlGetResourceID` answers a resource's position in the executable's
  resource directory, from zero (`Client.ResourceOrder`). One title sizes a
  table by a resource that lists every other resource and indexes it with these
  identifiers, stepping over that resource's own position; the name's address,
  which the engine answers a KTF title with, wrote far outside the table.
  `MC_knlGetResource` still reads an address it is handed, which a quick save
  from before can hold.
- `MC_grpDrawImage` leaves out the source pixels equal to the context's
  transparent pixel (`Platform.ContextTransparentPixel`). One title sets it to
  magenta before every image and has no other transparency. Zero, which
  `MC_grpInitContext` leaves, is no transparent pixel: another title draws every
  image through a cleared context.
- `MC_grpCreateImage` writes `0xFFF7` where the decoded picture is transparent
  (`Platform.TransparentImagePixel`). One title reads its pictures' pixels
  itself and skips `0xFFF7` in both its blitters, whatever colour a picture
  declared transparent.

Setting a context's alpha installs the platform's own blend as the pixel
operation, with the alpha as its parameter, on both platforms
(`WIPICBuiltinAlphaOp`): one title fills its card slots through an alpha of
100 and no operation of its own.

The C library at slot 14 is served in `clet_clib.go`: the string, memory,
conversion and time functions the specification lists, each named where that
list puts it and confirmed by the arguments the callers pass. A `double` comes
back in `r0` and `r1`, the low word first, which is the order one title's own
conversion routine reads it in.

## Running

A Clet runs on its own client with no Java around it. `LoadCletClient` maps the
code at the image base, the data and its zero-filled BSS at their own address,
and the client thread's stack; `prepareClet` builds the root table and writes
its address into the marker word. `startClet` runs the start entry, which is
where a title loads what it needs and arms the timer its frames come from. A
service round (`tickClet`) runs the timers, the events the title posted itself
through `MC_grpPostEvent`, a repaint it asked for, and the audio and cheat work
every round does; there are no guest threads, cards or Java objects to service.
A connection the program asks for fails the way a KTF
title's does, through the callback it registered, which the next round delivers
before its timers: one title waits on that answer behind its "checking your data
on the server" screen and shows nothing else until it comes. Pause, resume and
destroy run the program's own entries, and destroy is one-way. A pause runs
`pauseClet` and serves none of the program's timers: an LGT Clet's pause also
serves the timers armed at it, because a title there does its pausing in its
frame timer ([LGT](lgt.md#a-clets-pause)), and no SKT Clet title is known to.
Keys reach the program through its event handler — press 2 and release
3, no repeat — which is what both titles compare against. The semihosting
`svc #0xab` one title's C runtime carries is answered as a semihosting host that
took it would, and dropped.

The ARM core yields to the Go scheduler every 16,384 instructions rather than
the default 1,000, which is the figure LGT's Clet runs at. A Clet is a single
thread between platform calls, so the frequent yield only bought it scheduler
wakeups: on the wall clock at the web host's pace, with a key every three
seconds, the two local titles went from 55 to 46 and from 48 to 41 percent of a
core over the same frames. The steps a service allowance or the batch clock
counts do not depend on the quantum. A batch Host's manual clock is accepted
whichever platform's package built it: `SkipToNextDeadline` moves anything that
answers `Set`, and a service call moves anything that answers `Advance` with
the guest's execution, the way the KTF runtime moves its own.

The file tree the program sees is its own files and the resources its
executable carries, at their names below the program's directory, a title's own
file winning over a resource of the same name. A title reads the resources
through the file calls as well as the resource calls — one lists directories,
asks whether a sprite exists and opens data files, all of them resources named
`/data/...` and `/png/...` — so `ProgramFiles` exposes both
(`internal/platform/skt/native.go`).

What is the Clet's own — its root table and numbering, its C library, its
entry points and its checkpoint codec — is on no other vendor's path, and LGT
is untouched; the WIPI-C engine beneath it is the one KTF's C titles run on
(see Limitations). The identity-compatibility code regions are the
only guest bytes the loader changes, and only for the exact executables the
registry names (see Identity compatibility).

<a id="skt-native-identity"></a>

## Identity compatibility

A native package may carry certificates or saves tied to the handset it was
copied from. The compatibility registry identifies verified executable revisions
by SHA-256 and selects the handling each format needs. A recognized certificate
pair can supply that session's handset identity, some comparisons have a scoped
code fix, and specific device dumps start with fresh save data. The archive and
its files are left intact. See [authentication](authentication.md) and
[code-revision compatibility](platform-compatibility.md).

<a id="skt-native-identity-discard"></a>

### Fresh start (`skt.native_discard_owner_state`)

Four titles find the previous owner's packaged saves, validate them against the
handset, and stop when they do not match — with a data error, an error number,
or a halt that draws its message and never returns — and none falls back to a
new game on its own. Three of them, which share one engine, check their saves
against the subscriber number: without the dump's files they start fresh, show
their own server dialogs, fail to reach the server and play on. For a
recognized device dump, the loader does not present the install directory's
files, so the game finds no save and starts fresh, writing its own saves from
there (`internal/platform/skt/native.go`, `ProgramFiles`). The embedded resource
archive — the game's read-only content — is unaffected, and no file is changed.
This is the loader withholding another handset's state, not a code change, so
the session reports no authentication status for it.

<a id="skt-native-identity-save"></a>

### Save identity (`skt.native_save_identity`)

The other title keeps, in the save its directory ships, the last digits of the
number it was written under, and refuses a save whose digits do not match the
running handset's — and that refusal is on its first screen, so the title cannot
boot past it without the save accepted. The comparison does not key the save's
encoding — the save is readable either way — so the recognized executable accepts
that one comparison in the code before it is loaded and reports
`skt-native-save-subscriber`; the save's own length and checksum checks still
run, and the file is not changed (`internal/platform/skt/native_identity.go`).

<a id="skt-native-identity-cert"></a>

### Certificate subscriber (`skt.native_cert_subscriber`)

The same title then checks a `cert.c2s` certificate when play begins and stops
with error 5001 — the subscriber field of the certificate not matching the
handset. This is the specification's `cert.c2s` contract, the one a KTF title
reports 5001 for and `ktf-certificate` adapts, here in a longer variant on a
different executable. The certificate decodes to three fields compared in a row
— the subscriber (5001), a second field (5002) and a token (5003); the recognized
executable accepts only the subscriber comparison, so the other two still run,
and reports `skt-native-certificate`. No certificate is minted and no file is
changed.

The `skt.native_certdata_subscriber` rule handles an ARM title's `CERTIDATA`
record inside its packaged container. It accepts the subscriber comparison at
startup after the record has been read and verified, and reports
`skt-native-certificate`. See [the recorded-identity replay](history/skt.md#skt-new-archives-2026-11-18-registry-review).

<a id="skt-native-identity-paired-certificate"></a>

### Packaged certificate pair (`skt.native_paired_certificate`)

One recognized reader uses `MIN` for its 20-byte `usimcert.c2s` (error 1004)
and `PHONENUMBER` for its 31-byte `cert.c2s` (error 3003). The first record's
ten-digit padded field determines a bounded set of candidates. The second
record's checksum must identify exactly one. With both records present and
valid, the loader supplies that value for both properties in this session.
The guest runs its original certificate checks. With an absent, invalid, or
ambiguous pair, or with authentication compatibility disabled, it receives
the configured handset value.

Quick saves capture the session's identity with its guest memory. The restore
path checks it against the archive pair. A quick save made with the earlier
comparison patch for this revision needs a new session because the checkpoint
identifies the patched executable image. See the
[evidence and replay](history/skt.md#skt-new-archives-2026-11-18-certificates)

<a id="skt-native-quick-save"></a>

## Quick save

A Clet is called and returns, so between two service rounds nothing of it is
running: a checkpoint is its memory, the client thread's registers and the
tables the engine keeps beside them, taken there and restored without running
any of the program — not its start entry, and not the destroy entry of the
session a load displaces. It is the shared envelope's variant 8, carried by the
same slot, Host commands and page buttons as every other title's, and like
every other title's it holds no save: the restored program reads its files as
the store has them when the load commits. What the record holds, how it is
validated and how it was accepted is in [SKT checkpoints](skt-checkpoints.md#native-clet-state).

<a id="skt-native-earlier-saves"></a>

## Saves from the earlier engine

The Clet engine before the shared one stored its removal list (`db/.removed`)
and its directory list (`db/.dirs`) but began every session with both empty, so
neither was read back. A program that deleted a file in one session and wrote it
again in a later one left the name on the list over the new bytes, and that
engine answered the file from them. The shared engine reads the lists, so for a
Clet it reads a listed name whose save holds bytes as that file
(`Platform.StaleRemovals`): a removal stores the file empty in the same write
that lists the name, and a write of the file takes the name off in its own
write, so only that engine leaves a name over bytes. An empty file stays
removed. KTF never wrote such a save, and every name its list holds stays
removed.

The directory list needs no such reading. A directory holds no bytes, and one
the earlier engine recorded is one `MC_fsMkDir` now answers `M_E_EXIST` for,
which is what a handset that kept it answers.

<a id="skt-native-text-entry"></a>

## Text entry

A Clet's text field is its own: the program draws it, keeps its value and
composes characters through the input-method functions, handing each key it is
given to `MC_imHandleInput`, which answers with the completed text. The page's
text box reaches it the way a handset's input method did, one character at a
time through a carrier key the field already takes, and the field appends each
character under its own rules. The input method's activity is the evidence of a
field, so text is offered once the program has used its input method, and stops
being offered when a carrier reaches no input method until the program calls it
again or the player presses a key. The rules and their reasons are in
[native text input](native-text-input.md#skt).

Neither local title calls its input method on a screen it reaches offline, so
neither is offered text here. One title's fields, by the labels its code draws
them under, are a nickname, a mask's name and description, a gift recipient's
number and a search term, and each belongs to a feature that talks to a server
first; the other called its input method on none of the screens a route through
its menus and story reached. The
path is covered by an authored native package instead
(`internal/testfixture.SKTCletArchive`), whose field takes keys either in its
event entry or on its frame timer.

<a id="skt-native-cheats"></a>

## Cheats

A Clet runs on the ARM core like the other native titles, so the cheat panel
attaches to it through the adapter they all share
(`internal/platform/armcheat`): a search sweeps the committed writable pages,
a freeze is rewritten after every service round, and a watch reports the store
that moved an address. The CLI's `-cheat` belongs to the platform commands,
and none of them opens a Clet. The program's code and its data are both labelled as
the client's and both swept, because the loader maps them side by side and
adjacent pages come back as one region; the engine's own regions are named the
way a KTF title's are. A saved table is named after the program and keyed by
the hash of its code. A quick save is refused while a freeze or a patch is
active, as on every platform, because the slot would keep the cheat's values as
the program's own.

## Limitations

A change to the WIPI-C engine reaches KTF titles and Clets alike, which is the
point of sharing it: a handler fixed for one is fixed for the other. A Clet is
tested by its own fixture and by the engine's KTF tests, so a change to the
engine is checked against both platforms' archives. Where an SKT handset is
observed to answer differently from a KTF one, the difference becomes a
platform value or hook rather than a second copy of the handler.

The local examples are GCC-built, and three header layouts are known (above);
an executable built with another toolchain may lay its header or its
relocation table out differently. One example is an online game: offline it is
told its server cannot be reached, reports that and plays on, and the content
that needs the server stays out of reach.

<a id="skt-native-identity-load-allowance"></a>

### Service step allowance (`skt.native_service_allowance`)

One local title reaches its first stage, with its certificate accepted and the
dumping handset's saves loaded, and then builds the stage in one timer callback
that neither returns to the Host nor polls the clock: it decodes its own tables
with its own bit-serial routine, which takes about 1.26 billion ARM steps,
more than the five hundred million a single Host service call is given, and so is refused as a runaway would be. Raising the allowance for
every title would weaken the bound that stops a real runaway, so the registry
raises it for this one recognized executable instead
(`NativeHeavyLoadAllowance`, in `internal/platform/skt/native_identity.go`); the
default still bounds every other title. Under the raised allowance the title
loads its stage and reaches its character, dialogue and a fight. A checkpoint of
a title given its own allowance restores under the same one
([history](history/skt.md#skt-new-archives-2026-11-18-load-allowance)).
