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

The `.daf` lays out as both local examples do:

- A 0x20-byte block whose word 0 is `0x20`, then a 0x50-byte second block at
  0x20 whose length is at `0x28`. Offsets `0x3c` to `0x50` hold five sizes:
  the code with its read-only data, the initialised data, the BSS (which is not
  in the file), the relocation table, and an embedded resource archive. Offsets
  `0x58` to `0x6c` are six odd offsets into the code — the Thumb entry points.
- The regions follow the header back to back from `0x70`, BSS aside, and in
  both examples the four sizes add up to the file length exactly. A layout that
  does not is refused.
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
Decoded this way every entry in both examples lands on a word pointing where the
entry says, so both regions are relocatable and a loader can place them
anywhere. `Load` places the code at the KTF image base and the data at the same
distance from it they were linked at, adding the base to every code and data
address the table lists and leaving the self-relative words alone; it zero-fills
the BSS after the data. Out-of-range or misaligned entries are refused.

The resource archive opens `{0x10, 1, 0x10, "KO", directory size, 0, count}`,
then `count` pairs of offset and size, then `count` NUL-terminated names; the
files follow the directory, and the offsets count from there.

## Calling the platform

There is no SVC and no imported name. Every call loads the word at `0x01001000`
(the root table the loader wrote), a table pointer at `root + 4*slot`, a
function at `table + 4*function`, and branches through a `bx` veneer. The root's
slots and their meaning, read off the two examples' call sites:

| slot | table | notes |
|---|---|---|
| 0 | kernel | numbered as the KTF kernel interface on every slot either title calls |
| 1 | graphics | the KTF graphics table through `MC_grpPostEvent` at 36, then the input-method functions and the handset's own additions |
| 3 | filesystem | the specification's `MC_fs*` order, with `MC_fsIsExist` and `MC_fsTell` at 15 and 16 exchanged from the KTF table |
| 4 | misc | `MC_miscBackLight` at 0 |
| 5 | network | the specification's order, refused the way a KTF title's is |
| 6 | utility | byte-order and address conversion |
| 7 | media | named by its callers, a different numbering from both the specification and the KTF table |
| 9 | serial | refused |
| 10 | phone | refused |
| 11 | terminal resources | the phonebook; refused |
| 14 | C library | `string.h` 0–18, `stdlib.h` 19–24, `time.h` 25–30, in the specification's list order |

Slots 2, 8, 12 and 13 are not reached by either title. The function numbers
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
destroy run the program's own entries, and destroy is one-way. Keys reach the program through its event handler — press 2 and release
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

The engine is SKT's own, so none of this is on any other vendor's path: the KTF
and LGT platforms are untouched. The identity-compatibility code regions are the
only guest bytes the loader changes, and only for the exact executables the
registry names (see Identity compatibility).

<a id="skt-native-identity"></a>

## Identity compatibility

A native package is a handset's installed directory copied whole, so what the
title wrote there is the previous owner's state, bound to the handset it was
dumped from, and a different handset's player cannot use it: the title either
rejects a foreign save (an error that stops it) or needs a foreign-subscriber
field accepted. These are the same class of offline authentication the KTF and
LGT platforms already adapt ([authentication](authentication.md)), and they are
handled the same way — the exact executable is recognized by its SHA-256 in the
compatibility registry ([code-revision compatibility](platform-compatibility.md)),
and the fix is scoped to that executable, so nothing else is touched. Two shapes
of fix are used, neither inventing data and neither changing a packaged file.

The two local titles need different shapes, which is why this is per-title
rather than one rule: one carries a certificate the other does not, so a single
"ignore the owner's files" rule would strip a certificate the title cannot boot
without.

<a id="skt-native-identity-discard"></a>

### Fresh start (`skt.native_discard_owner_state`)

One title finds the previous owner's packaged saves, validates them against the
handset, and stops with a data error when they do not match — it does not fall
back to a new game on its own. For a recognized device dump, the loader does not
present the install directory's files, so the game finds no save and starts
fresh, writing its own saves from there (`internal/platform/skt/native.go`,
`ProgramFiles`). The embedded resource archive — the game's read-only content —
is unaffected, and no file is changed. This is the loader withholding another
handset's state, not a code change, so the session reports no authentication
status for it.

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

## Limitations

A change to the WIPI-C engine reaches KTF titles and Clets alike, which is the
point of sharing it: a handler fixed for one is fixed for the other. A Clet is
tested by its own fixture and by the engine's KTF tests, so a change to the
engine is checked against both platforms' archives. Where an SKT handset is
observed to answer differently from a KTF one, the difference becomes a
platform value or hook rather than a second copy of the handler.

The two local examples are both GCC-built, so an executable built with another
toolchain may lay its header or its relocation table out differently. One
example is an online game: offline it is told its server cannot be reached,
reports that and plays on, and the content that needs the server stays out of
reach.
