# LGT platform

`internal/platform/lgt` loads ELF-based WIPI packages and provides their C and
AOT Java interfaces. It shares the ARM core and backend services with KTF, but
uses import-table binding and its own Java object model rather than the JVM.
Detailed slot layouts, traces, rendering decisions, and dated corpus results
are in [LGT history](history/lgt.md).

## Packages and loading

An archive contains `app_info` beside a JAR whose `binary.mod` entry is the ELF
module. The descriptor supplies AID, PID, and the main class; unknown keys are
kept. Descriptor-named JARs take precedence. If that match is absent, a single
fallback is accepted; multiple candidates require exactly one valid module.
Candidate inspection shares an expanded-byte budget and rejects malformed
auxiliary JARs. An archive is bounded to 128 MiB, one entry to 64 MiB, all
entries together to 512 MiB, and the entry count to 8,192.

Descriptor and entry names accept UTF-8 and MS949. Resource lookup recognizes
plain, `P/`, and `P/<AID>/` names, with JAR resources ahead of outer-archive
resources. Files packaged beside the JAR are readable by name, and the JAR
wins a name collision. Case-insensitive collisions are resolved
deterministically; canonical duplicates, traversal, and invalid paths are
rejected.

A unique outer `wrapper/<AID>/` directory is also exposed at the application
root, after canonical packaged paths. Unrelated AIDs are excluded and ambiguous
matching roots are rejected. These supplemental files keep the normal save
override and deletion rules. See [supplemental-data QA](history/lgt.md#lgt-qa-2026-09-21-supplemental-directory-mounting).

The module parser accepts ELF32 little-endian ARM executables. Addressed
sections share a bounded mapping that must end below the platform heap, and
`SHT_NOBITS` reserves zero-filled bytes. Import dispatch validates the
supported tables and reports unsupported calls. Loaded sections are bounded: at
most 64 sections with an address, and no two non-empty ones may overlap in
memory or share file bytes, because every recognition scan walks each section
once per probed address.

Saves belong to the PID, or to the AID when the PID is absent. Colliding
identifiers are not rewritten without evidence for a different identity rule;
`checkgames` reports a save owner claimed under more than one AID
([CLI](cli.md#checkgames)).

A package of the carrier's earlier Java platform is not one of these: a MIDP
1.0 JAR whose classes link against the carrier's own `mmpp/…` extension
classes, with a `.jad` descriptor and an `.idx` index beside it and no
`app_info`. No loader here runs that platform, and detection reports such an
archive as one no platform claims
([history](history/lgt.md#lgt-earlier-java-package-2026-10-21)).

## Execution and Java

The platform executes native Clets and its supported AOT Java surface. Java
metadata, objects, arrays, exceptions, monitors, workers, resource streams,
and supported library methods belong to the LGT implementation. Collection
traces guest references and platform roots, including shown/focused widgets
and the registers of every call in flight on a guest thread, and releases
unreachable resources. A wrapper over a title's own `InputStream`
holds that stream object and the buffer its `read([BII)` is handed. Byte sinks
and Vectors stop with an error at `maxJavaArrayLength` (4 Mi bytes or
references), the largest array the platform builds, and Java `putData` fills a
clip to at most 4 MiB, answering the bytes taken. Checkpoint class records must
carry an element width of 0, 1, 2, 4 or 8.
The long-array store interface takes high then low words; long results and
array storage are low-first. [ABI evidence](history/lgt.md#lgt-qa-2026-09-21-long-array-store-word-order)
records the restored timed logo transition.

A guest thread runs on a 1 MiB stack of its own, and a finished thread's stack
goes to the next thread started (only committed pages are cleared). At most 16
guest threads run at once, which is also the most stacks a checkpoint loads, so
no number of threads a title reaches makes a quick save refused or unloadable.
The bound is met at `Thread.start`, whichever way the thread was built: a start
that finds all 16 stacks taken throws `OutOfMemoryError`, which the title can
catch (it is a `VirtualMachineError`, as in CLDC), and leaves the thread
unstarted. Uncaught, it goes the way of any other uncaught exception: a
thread's `run` or a card callback ends and the session carries on. The
specification gives `start` no bound and no failure of its own, so running out
is reported as the memory a thread runs on running out. Building a `Thread`
takes no stack and is not counted, and a finished thread gives its stack back
at the next start, whichever call saw it end: a `startApp` that starts a loader
and waits for it, once per resource, finishes every loader inside its own wait
or lock, where no thread round runs, and was refused at the seventeenth load
with none running. Formerly a start on a
`Thread` subclass the module built itself was not counted, so a seventeenth
thread running at once left every later quick save of the session refused, and
building a `Thread` while 16 unfinished ones existed ended the session. Over
the 18 distinct local Java archives, 1,500 ticks each with a confirm press
every 150, no title ran more than two threads at once or built more than four.

Under allocation pressure, the decoded-image cache releases surfaces that no
Image or Graphics object owns. Live owners and the existing memory limit are
preserved. [Startup QA](history/lgt.md#lgt-qa-2026-09-21) records the regression and replay.

String indexing, lengths, substrings, char arrays, and StringBuffer mutations
use UTF-16 units. Isolated surrogates survive through an internal WTF-8
representation. Guest stream mark/reset operations reach supported overrides.

A module states what each of its classes extends, and a class may not be above
itself. A record that names itself as its superclass, or is named by a
superclass further up, fails to prepare with the class named in the error, and
the layout refuses the same shape by name. A module can also register one of
its own classes under a name the platform's hierarchy uses; a platform class
that would then be linked beneath itself the first time a type check passes
through it is left without that link instead. Every walk up a chain (a type
check, an interface call, the search for the slot an override shares) ends at
a class with no superclass; a closed chain was formerly a tick that never
returned.

`internal/tools/apiscan` starts each archive and reads back the imports it
resolves, using actual dispatch resolution, including inherited baked slots.
A method name alone does not count as an implemented body. Startup imports are
a lower bound: later lazy imports and virtual calls can reach additional
unsupported methods. The Java library remains partial. A C library slot counts
as served when it has a name, and every slot the C library handler serves has
one ([history](history/lgt.md#lgt-stdlib-names-2026-10-22)).

Vector slot 26 implements `setElementAt(Object, int)`: it replaces one reference
without resizing or shifting the list, permits null, and throws
`ArrayIndexOutOfBoundsException` for an invalid index. See the
[crash investigation](history/lgt.md#lgt-qa-2026-09-20-vector-replacement-crash).
Slot 10 implements `copyInto(Object[])`. `Clip.getVolume`, `Jlet.getEventQueue`
(one queue object; `postEvent` is not implemented) and `Card.getDisplay` (the
default display) answer the
[members one title reached first](history/lgt.md#lgt-new-archives-2026-10-19-java-surface).

The class library's core is placed by the specification
(`java_class_library.go`): Object, String, StringBuffer, the integral number
classes, Vector, Stack, Hashtable and its enumerations, InputStreamReader,
`DataInputStream.readUnsignedShort`, `Thread.join`, `System.exit` and
`getProperty`, `Class.forName`, `newInstance` and `getName`, and Throwable's
`getMessage`, `toString` and `printStackTrace`. Every slot is the
declaration-order rule's — a class's own methods from slot 10 in the order its
CLDC class page declares them, an override at the slot above — and
`TestJavaBakedSlotsFollowTheDeclarationOrder` holds every served slot of these
classes to that order. Floating point, `Boolean`, `Character`, the exception
classes' own constructors, `Class` queries such as `isArray`, and the methods a
title's own `Reader` subclass would inherit are not served.

- **hashCode, equals and toString are the receiver's own.** A Hashtable hashing
  a key, a Vector searching for an element, `append(Object)` and
  `String.valueOf(Object)` call the object's slot 2, 3 or 4 the way a call site
  does, running the title's override when it has one (`javaCallVirtual`).
  Object's own forms answer only for a class below no library class that
  declares its own: a `Date`'s or a `Thread`'s toString, which this platform
  lacks, stops the title by name instead of answering `java.util.Date@…`,
  except in the label `append(Object)` and `String.valueOf(Object)` build,
  which keeps the `Class@address` it had. These answers nest at most 256 deep,
  so a vector that holds itself stops the title instead of the process.
- **A Hashtable is the class library's, bucket for bucket**: 11 buckets, 75%
  load, `2n + 1` on rehash, a new entry at the head of its chain, enumeration
  from the last bucket. With String and number hashCodes the language's own, a
  table enumerates in a handset's order. A Vector's enumeration is live; a
  Hashtable's is the table as it was when `keys()` or `elements()` was asked.
  Both reach the title through an interface table this platform gives their
  classes (`javaPlatformInterfaces`), Enumeration's two methods at slots 10
  and 11.
- `Vector.indexOf` and the searches beside it ask the argument's `equals`,
  where they compared references before. Vector and StringBuffer keep the
  capacity `capacity()` answers by the class library's growth rules.
- An exception the platform throws carries the text naming what went wrong as
  its message. `System.getProperty` answers the CLDC table the KTF runtime
  answers, then the handset table, and null for anything else.
- `Thread.join` parks a guest thread until the other has ended, a slice at a
  time, and a checkpoint taken inside it restores to the same wait; the
  platform's own thread runs the other threads itself, for at most 1,024
  rounds. See [the history](history/lgt.md#lgt-class-library-2026-10-28).

### Key callbacks waiting on workers

Key callbacks can wait for state updated by a Java worker. Eligible workers
receive a slice at bounded compiler checkpoints inside such callbacks, except
while the platform holds a monitor. Exception regions, jump buffers and native
call depth belong to each worker across scheduling boundaries. The original
instruction ceiling still bounds the callback. See the
[gameplay hang investigation](history/lgt.md#lgt-key-callback-hang-2026-09-23) for the live
server evidence, failing input replay and corrected path.

## Display and input

The LCD is the Host's choice, 240×320 by default (`-screen WxH` in the CLI).
The Java display holds one active card. `Card.repaint` repaints the whole
card, whatever rectangle it names. `removeCard` removes the active card and
cancels its pending paint, returning false for a null or unrelated card.
General multi-card stacking and notification lifecycle remain incomplete.

Graphics contexts belong to guest memory. Supported drawing honors compact
context origins, clips, overlapping self-blits, and nested pixel callbacks.
Callback errors restore the client lock. Verified native revisions use a
56-byte context with direct callbacks; the compatibility registry selects that
ABI explicitly. See [rendering evidence](history/lgt.md#lgt-qa-2026-09-20-wide-graphics-context).
The [later blank-screen investigation](history/lgt.md#lgt-qa-2026-09-21-blank-rendering-and-context-layout)
adds another measured revision to that registry. One further
[wide-context revision](history/lgt.md#lgt-qa-2026-09-21-second-color-report-wide-context-proof)
uses exclusive clip ends in its direct image blitter; its exact fingerprint
selects that convention, and earlier wide contexts retain inclusive stored
ends. Arbitrary mixed compact/direct contexts and the previously proposed
alternate 52-byte layout remain unverified.

The panel image follows the supported flush path. A revision-specific
initialization correction removes verified 24-row strip offsets for exact
native-module fingerprints, including direct-pixel and image-path revisions.
The [direct-pixel investigation](history/lgt.md#lgt-qa-2026-09-21-direct-framebuffer-origin)
records the restored dialogue bounds. It is not a platform-wide coordinate
shift. A fingerprint-scoped
[transparency calibration correction](history/lgt.md#lgt-qa-2026-09-21-transparency-calibration-origin)
aligns one native blitter's sample write with its 24-row-adjusted read, which
prevents a black transparency key from copying magenta backgrounds into the
field without changing the shared framebuffer origin.
[Compatibility rules and evidence](platform-compatibility.md) describe selection,
mutation checks, and validation.

Fill, line, polygon, arc, rounded-rectangle, copyArea, copyFramebuffer and
drawImage walk only the part of their range that the clip and the surface can
reach, so a guest box at the integer extremes returns at once; the pixels are
those an unclipped walk draws. Image decoding reads the header first and
refuses a picture of more than 16,777,216 pixels (`backend.MaxFramebufferPixels`,
the rule SKT already applied) before allocating, and the checksum repair for a
picture with a wrong zlib trailer stops at the size its header implies.

The image slots follow the v2.x order: `MC_grpCreateImage` `0xe9`,
`MC_grpCreateSubImage` `0xea`, `MC_grpDestroyImage` `0xeb`,
`MC_grpDecodeNextImage` `0xec` (which answers that no frame follows: nothing
here decodes an animation) and `MC_grpEncodeImage` `0xed`. A destroy gives the image's pixels and record back,
and is the only release a Clet's images get: the collector runs for Java titles
only. A sub image is a copy of the source's pixels with its mask or colour key;
a region outside the source or without pixels answers NULL, and a surface region
too full for the copy stops the run as a create does. See the
[slot correction](history/lgt.md#lgt-image-slots-2026-10-22).

Keys use the platform's lifecycle and clock boundaries. Clet events are a press
and a release: the platform generates no `MV_KEY_REPEAT_EVENT`, and a Host key
repeat is dropped rather than sent as a second press, because two local titles
dash on every press (`internal/session`). Native Clets do not expose a general
pointer-event contract. Host text entry supports LWC fields — the focused one,
or the only one on screen — and the WIPI-C input-method append path, offered on
every screen once a title has used its input method, except after a commit that
reached no widget until the next key or input-method call; it does not infer
arbitrary game-owned fields. Host text must be encodable in KSC5601; other characters
are refused. See [native text input](native-text-input.md).

## Storage, audio, and offline services

File/record services use the common save boundary. Storage read errors remain
visible at native boundaries, and a retained read failure prevents subsequent
writes. Java File modes (1 read-only, 2 append, 3 truncate, 4 read-write)
are translated to the C file flags before opening the shared store; in
particular, Java mode 4 preserves existing contents. A save key keeps the case
the title gave its path, and the save formats are unchanged except for the
marker line described below. Of the WIPI-C database block, only
`MC_dbListDataBases` is served, and it answers that the program has none.

What the C file slots (`MC_fs*`, `0x190`–`0x1a0`), Java `File`, `FileSystem`
and `org.kwis.msp.db.DataBase` answer is the common contract all three
platforms keep, [WIPI storage](wipi-storage.md): names of at most 255 bytes,
what a directory is and what each call answers about one, the 1 MiB of room
and what counts against it, the 4 MiB bound on one file or one database's
records, and the database rules. On this platform a file is a save key under
`fs/`, and a database is a container of this runtime's format in a scope of
its own, `jdb/<name>.db`, with the index of names at `jdb/.index`, so a
title's file `save.db` and its database `save` are two things and no file
call sees a database. Earlier builds kept each container among the files, as
`fs/<name>.db` with the index at `fs/__databases__.db`; the first time a
session over such a store reads its databases, the containers and the index
move into the scope in one batch and the old paths go on the removal list,
once. A store that refuses the move is read where the containers were, with
the file calls not seeing them, until a session can move it. The space count
takes a database once, as its records. The directories a title made are a
list kept beside the saves, `fs/.directories`, as the removed and created
paths are (see [the change](history/lgt.md#lgt-storage-contract-2026-10-22)).

Every file surface (the C file table, Java `File`, a stream opened on a `File`,
and `DataBase`) keeps the save rule the other platforms keep. A file's writes
wait in a buffer until the title closes it; these rules keep that buffer from
hiding or losing them:

- **Visibility.** A write or a truncation is what every later read of the path
  in the session sees: through any handle, a second open, a stat, an existence
  check, a resource read, a rename and a stream. The handles open on one path
  share one buffer, as handles on one file of a handset do, so a handle opened
  before a write reads it and one closed last does not put an older copy back.
  A write into a stream opened on a `File`, directly or through a
  `DataOutputStream` over it, is a write to the `File` at once, and a stream
  opened for reading reads the buffer as it is now. A rename takes the open
  handles with it, so a later close stores under the new name.
- **Streams after their `File`.** `File` is a `StreamConnection`, and closing a
  connection whose streams are open leaves them usable, with the connection open
  under them until they close. So a stream opened on a `File` holds the file
  open once the `File` closes: an input stream goes on reading the file as it is
  now, an output stream goes on writing it, and the stream's own close, or the
  collector reclaiming it, closes the file as the `File`'s close would have. A
  `DataInputStream` over such a stream is that stream, so closing either lets
  the file go. Closing an output stream ends its binding, so the `File` can open
  another. A stream in a record an earlier build took, whose `File` was closed
  then, keeps the window the load gives it.
- **Durability.** What waits in a buffer reaches the store no later than the
  close of a handle on the path (`MC_fsClose`, `File.close`, the close of a
  stream holding its file open, or the collector closing either one when
  nothing holds it) or the flush or close of a stream on it, and in any case
  at the ending: `MC_knlExit`, `Jlet.notifyDestroyed` and closing the session
  (before `destroyClet` and again after it). The ending also gives the store
  again a database or a path list it refused earlier. A quick load stores the
  waiting writes of the session it displaces, as a quick save does (below).
- **Objects the collector reclaims.** A `File`, or a stream holding its file
  open, that nothing holds any more is closed by the collector, which stores
  what waits in its buffer as a close does: earlier than the ending, which the
  rule allows, and only what the title wrote. A store that refuses leaves the
  handle open with nothing holding it, for the ending and quick steps. A
  `DataBase` object reclaimed while it holds a change the store refused is
  closed the same way: its records are given to the store at once, and if
  the store refuses again they wait, for the ending, a quick step, or the
  next open of that database, which takes them up. Nothing waiting is lost
  with its object; as at the ending, records whose container the title has
  since written another way, through a file of the same name, give way to
  that later write.
- **Creation.** Opening a missing file for writing (`MC_fsOpen` flags 2, 4 and
  8, `File` modes 2, 3 and 4) creates it: an empty file that every later read
  sees at once, stored at the open. Opening a missing file only to read fails:
  `MC_fsOpen` answers -12 and `File` mode 1 throws `IOException`, and nothing
  is made.
- **Truncation.** A truncating open (`MC_fsOpen` flag 4, `File` mode 3) empties
  the file in the store at the open, whether or not a write follows, and every
  handle on the path reads it empty from there.
- If the store refuses a close, a flush, or the emptying or creation an open
  makes, the write stays waiting in the buffer, which every read in the
  session sees, and the next close, the ending and quick steps try again.
  `MC_fsClose` answers an error. `File.close`, and a stream's flush and
  close, throw the `IOException` each declares, and the `File` stays open
  with its streams bound. A handle opened only to read has nothing of its own
  to store, so its close succeeds whatever waits on the path for another
  handle. An open whose emptying or creation the store refuses still opens. A
  rename whose new name the store refuses answers an error and keeps the old
  name, and one onto a name with no save (below) answers -3, the
  specification's bad-name code, and keeps the old name. Closing a `File` that
  is already closed is nothing, as the specification says.
- A write that would take a file past 4 MiB fails as a write does:
  `MC_fsWrite` answers `M_E_NOSPACE`, and `File.write` and a stream write throw
  `IOException`. A seek past it fails as a bad seek does. A packaged file
  already larger is still read and written within its own length.
- **Removal.** A removal hides a file under every spelling that shares its save
  key (`save.dat`, `/save.dat`, `./save.dat`, `//save.dat`, and `.\save.dat`: a
  backslash separates a path as a slash does, as the archive lookup reads it, so
  a write under one is stored under the slash path; the store refuses a
  backslash in a key, so no earlier build stored one) and keeps it hidden in
  later sessions, through the removal list, until the path is written again.
  What waits in a buffer open on the path goes with the file, so neither a close
  of that handle nor the ending brings it back; a write through the handle
  afterwards is a write of the path. A removal comes off the stored list only
  once the store has taken the write that brings the file back. Deleting a
  database takes a write the store refused with it. A removal the store
  refuses does not happen: the list is given to the store before the session
  lets the file go, and a refusal leaves the file, in this session and the
  next, with `MC_fsRemove` answering `M_E_ERROR`, `FileSystem.remove` (either
  form) throwing `IOException` and `deleteDataBase` throwing
  `DataBaseException`. Only a rename, which has already stored the file under
  its new name, leaves a refused list waiting for the ending and quick steps.
- **Case.** Names that differ only by case are two files on every Host: the
  save key, the path lists, the buffers the handles share and a listing all
  keep the title's case. A packaged file is still found without regard to
  case, as the archive lookup finds it. The builds before this one folded the
  path lists to lower case; an entry they wrote keeps its meaning. It hides
  the file under any case, a write under any case clears it, and a listing
  shows the lower-case name unless this build has recorded or removed the file
  since. A plain entry hides every spelling, and an entry under another
  spelling (`./save.dat`) still hides only that spelling, because the file may
  have been saved under the plain name since. This build writes its own
  entries after a marker line, `#Case-sensitive paths follow`, which no entry of
  those builds can be (it holds capitals); a list with none of its own entries
  is written as those builds wrote it. A save whose name differs only by case
  from a packaged file is a file of its own and is on the created list, so a
  listing shows it beside the packaged file; a write under the packaged
  file's own spelling is not, and the listing shows that file once, whether
  the title names it in UTF-8 or in its raw EUC-KR bytes, which the archive
  lookup decodes. A save an earlier build wrote that way is listed once the
  title writes it again.
- The file scope itself, the names `.removed`, `.created` and `.directories`
  at its root, which hold the platform's own path lists, and
  `__databases__.db` there, where earlier builds kept the index of database
  names (any case, and anything beneath them), are not guest files. A file call refuses
  them as a name no file can have (`M_E_BADFILENAME`, `IOException`), as it
  refuses a path with `..`. `openDataBase` refuses the one name whose
  container would be the index, `__databases__` in any case, with
  `DataBaseException`. The path lists record only paths that have a save: an
  entry of any other kind, which earlier builds wrote for such paths and for
  the index, is read as nothing and left out when the list is next stored.
  The on-disk layout is unchanged except for the new list.
- The index of database names is written whole at every create and delete. A
  write of it the store refuses waits in the session, which lists the names
  from it, and the next create or delete, the ending and a quick step write it
  again; a quick step the store still refuses it is refused.
- `DataBase` objects opened on one database share one set of records, so a
  change one makes is what another reads, a change the store refused included.
  The records are read again when something else changed the container (a
  deletion); a deleted container reads as empty.
  Every change is stored when it is made, so `closeDataBase` stores only a
  change the store refused, as the ending and quick steps do, and leaves the
  object usable.
- Nothing else writes a save: no background, timer or lifecycle store. A quick
  save and a quick load store the title's waiting writes first, below.
- A session with no save store, or with only an authentication adapter and no
  store beneath it, keeps every file its title stores in memory for as long as
  it runs, up to 64 MiB (an adapter keeps only the file it rewrites): a closed
  file, a database and the database index read back as the title left them, and
  nothing reaches a store or the next session. Such a session takes no quick
  save. A failed save read stops the run at the platform call that made it, and
  holds back every write.

`File.openDataInputStream` returns a typed stream using the existing file
binding and primitive readers. Closing a file input stream releases its binding
and advances the file position once, allowing another stream to open there.
`File.openDataOutputStream` is `openOutputStream` with `DataOutputStream`'s
typed writes, and shares its one output stream per `File`. `File.seek` moves
the position from the start of the file, as `MC_fsSeek` does, and throws
IOException where that slot answers an error. `DataBase.sortRecord` calls the
title's own `DataFilter` and `DataComparator` objects back the way a
`Runnable`'s `run` is reached.

Java Player distinguishes pause from stop, preserves repeat across pause/resume,
and rejects duplicate play/resume. Resume continues from the paused position;
clip volume and `PlayListener` events are applied. [Audio](audio.md) and
[shared service contracts](architecture.md#shared-runtime-services) describe
sound formats, timeline ownership, and callbacks.

Network access is refused except for narrowly recognized local compatibility
services. Both observed socket-creation slots (`0x7d0` and `0x25a`) preserve that
policy. [Authentication](authentication.md) and [network](network.md) describe
the selected adapters and unsupported service-dependent paths.

A recognized native revision's save reader accepts a saved subscriber identity
from another handset when automatic authentication compatibility is enabled.
The original length and checksum checks still run, and original files and the
configured subscriber number remain unchanged. The CLI diagnostic opt-out
(`-no-auth`) retains the original identity check. See the
[save-reader evidence](history/lgt.md#lgt-qa-2026-09-21-save-subscriber-comparison).

Two recognized native revisions complete their two-stage certificate exchange
through the same bounded local socket service. Requests must match the session
identity, model and the revision's observed protocol fields (client field, code
word and product field). The nonempty local message lets
the guest set and save its own certificate-present flag. Responses are deferred
to a service boundary; no external connection or carrier credential is created.
Unknown exchanges still fail, and the authentication diagnostic opt-out disables
this behavior. See the [protocol evidence](history/lgt.md#lgt-qa-2026-09-21-certificate-message-compatibility)
and [the second revision](history/lgt.md#lgt-new-archives-2026-10-19-certificate-message).

MC_mdaClipFree returns a C clip's record to the platform arena. A clip keeps at
most 64 undelivered callback transitions; a full queue drops the oldest. The C
string functions (`strchr` through `strcat`, `atoi`) and `%s` read with
`strlen`'s 1 MiB bound rather than the 4 KiB name bound, and a counted draw or
width string over 1 MiB is refused before allocation.

## Quick save and quick load

A session can be recorded between two ticks and restored later, in the same
session or in a new process, through the shared session, the CLI and the page's
keypad. Both execution variants are covered: a Clet, and an AOT Java title with
its guest threads parked. Recording runs no guest code and does not pause the
title; restoring runs neither its entry, its initializer nor `startClet`.
[Checkpoint state and ownership](architecture.md#lgt-checkpoints) describes the
record, and [testing](testing.md#lgt-checkpoints) what was run against it.

A checkpoint is execution state only. A load leaves the title's saves as they
are, and the restored title reads and writes them there; the rule and its
accepted limits are in
[Quick load and ordinary saves](architecture.md#quick-load-and-ordinary-saves).
Three properties of this platform's storage shape how it gets there:

- A write waits in the buffer of its open file until the title closes the
  file. Recording and restoring both store such a buffer first, once for the
  handles that share it, with the file left open at its cursor, and are
  refused when the store refuses it. The handles on a path share the buffer
  and a removal or a rename of the path brings it level, so a buffer is behind
  its file only when a `DataBase` stored a container under the same name
  since; that is refused, naming the file, because storing it would put the
  older part of the buffer over newer content. So are a buffer and a
  `DataBase` on one save that both hold a write, and a buffer whose name has no
  save key. The refusal lasts until the title closes that handle, which stores
  the buffer as the title's own act. A write into a stream opened on a `File`
  is in the `File`'s buffer and is stored with it, and a truncating open has
  emptied the store at the open.
- An open file, a stream opened on a file and an open `DataBase` are recorded by
  name, cursor and record size, and filled from the store when a load commits:
  the handles on one path share one buffer again, and the objects on one
  database one set of records. A stream holding its file open after its `File`
  closed is recorded with the handle it holds, as a `File` is, so it goes on
  following the file after a load; the record's layout is unchanged. A handle
  opened with the truncating flag takes at most what the file held at the quick
  save, which is what the title had written since the open emptied it; a handle
  whose open made a file that was not there takes the whole file, like any
  other, and so does one whose `File` closed with the rewrite stored, which a
  stream may still hold: cut back, a longer save written after the quick save
  lost its tail. Like any `DataBase` object, one filled that way stores its
  container at close only if its last change was refused. A record an earlier
  build took while a truncating open had not emptied the store yet loads the
  same way: the handle holds what it had written, and the load leaves the store
  as it is. Such a record can also hold bytes a stream had not handed its `File`
  yet, because that build kept a stream's writes until a flush. The load puts
  them in the `File` at its cursor, as if they were written then, where they
  wait for the flush, the close, the ending or a quick save; the record's layout
  is unchanged.
- `File.write(int)` past the end of the buffer fills the gap with zeros, as the
  array write already did. A restored cursor can stand past the end of a file
  that is shorter now, which is how the single-byte write came to be reached
  there. The record's cursor is still accepted up to 2^31-1, so older slots
  load. A write that would take the file past the 4 MiB cap fails instead of
  filling the gap.

A guest thread is restored from wherever a title parks one: inside
`Thread.sleep` or `Thread.yield`, inside `Object.wait` or `Thread.join`, behind
a lock another thread holds, at the end of its instruction budget, inside a
static initializer it set off, and before its first slice.

The class library's tables — hashtables, enumerations, the numbers, exception
messages, and the capacities Vector and StringBuffer answer — are a record of
their own beside the session record (version 4), because the record codec
refuses a missing or unknown field: added to the Java record, they would make
every quick save an earlier build took unreadable. A session holding none of
them is written in the earlier shape, and every earlier slot loads with the
tables empty, which is what the build that wrote it could leave. A thread parked beneath any other
platform call refuses the checkpoint, naming the call, and the session carries
on. An active cheat freeze or patch refuses it too.

Two behaviors changed with the feature and matter when comparing against an
older build. Timers that come due in the same tick fire in the order they came
due, then in the order of their structures; they formerly fired in map
iteration order. A file opened for writing holds its own copy of a packaged
file's bytes; writing through one formerly changed what the archive answered
for that file for the rest of the session.

## Validation and unresolved evidence

[Testing](testing.md) covers authored fixtures and local probes. [CLI commands](cli.md)
describe `runlgt`, import traces, routes, profiles, and memory watches. Historical
archive counts apply only to the runs recorded in the history.

Remaining limits include unsupported Java methods, incomplete widget painting
and WIPI-C database operations, service-dependent guest branches, the alternate
graphics context ABI, and unverified real-phone lifecycle recovery. A sampled
`resumeClet` read at `0x1` allowed later ticks but did not establish correct
in-game resume. Forced landscape rendering does not establish an automatic
screen-selection rule. Full gameplay, audible recovery, and long-term save
restoration require their own acceptance routes.
