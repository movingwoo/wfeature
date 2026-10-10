# KTF platform

`internal/platform/ktf` loads KTF packages, executes their ARM code, and composes
WIPI services with the shared backend and JVM. The CLI and server use the same
session path. Detailed layouts, call traces, earlier package investigations,
and dated measurements are in [KTF history](history/ktf.md).

## Packages and execution

The ordinary package contains `__adf__`, an AID-selected JAR, and a
`client.bin<decimal BSS size>` image. The loader validates descriptor fields,
ZIP paths, duplicate names, entry counts, expansion bounds, and image/BSS size.
It maps the image at `0x00100000`, enters Thumb code at `0x00100001`, and
validates the returned initialization and class metadata before using it.
The combined image and BSS are limited to 256 MiB.

The JVM context sits beside an ordinary image so representable object-header
displacements retain canonical image class addresses during compiled access
checks. Runtime aliases and inherited field payloads are described in the
[startup compatibility investigation](startup-compatibility-2026-09-19.md#ktf-startup-exception-1379b56de66e).

The earlier native package path accepts the supported `.mif`/`.mod` shape.
It has its own loader and platform interfaces inside the same platform package.
A folder name containing BREW does not identify that format; detection uses
archive contents. The recorded native loading stall still needs the matching
archive before it can be reproduced.

Guest workers can park with nested Go/ARM calls intact. Host callbacks and
worker execution use bounded instruction windows. AOT nesting is bounded per
logical worker, including derived ARM calls. The JVM/ARM bridge carries the
current `jvm.Invocation`, preserving monitors, class initialization, and the
instruction budget across Java → ARM → Java reentry.
A client-thread serial callback does not consume a guest worker grant, so a
self-requeueing UI cannot starve a receiver. Each round retains the configured
worker limit and the existing serial callback pacing.

Normal native returns use the established register convention. No environment
return-slot precedence is implemented: its address, width, initialization, and
validity rules lack concrete ABI evidence. Existing helper stacks and argument
containers retain their arena lifetime; regression coverage proves no premature
reuse, not a new reclamation policy.

## Lifecycle and input

Sessions drive startup, pause, resume, and unconditional `destroyApp(true)`
before unwinding workers. A failing teardown callback is diagnosed and cannot
prevent termination; repeated close does not call it twice.

Keys reach cards or the guest event queue according to the active path. User
events at or above `0x5000` retain their type and two arguments. Unknown reserved
event kinds below that boundary remain errors. WIPI Java supports Host pointer
events; earlier native packages do not gain pointer support from that fact.

Host keyboard/IME entry supports active LWC fields, the sole direct text child
of a shown Shell when explicit focus is absent, and the observed non-modal
KFC form containing one listened text field. The guest owns acceptance and
persistence. Synchronous `doModal` remains unsupported. See
[native text input](native-text-input.md) for target ownership and validation.

## Graphics and sound

The runtime provides guest framebuffers, clipping, pixel operations, images,
text, and the implemented WIPI drawing surface. KTF Java printable ASCII uses
five-pixel advances; Korean syllables retain ten-pixel advances. Measurement,
anchors, and rendering agree. NUL padding draws nothing and advances zero.
Native WIPI-C text retains its own metrics.

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

Mutable Image surfaces and Clip state use weak object ownership. Graphics keeps
its Image reachable. Collection releases orphan surfaces and stopped audio;
playing orphans remain until playback finishes, including repeating playback.
Shared audio timing and borrowed-buffer rules are in
[architecture.md](architecture.md#shared-runtime-services).

## Files and records

File input/output streams and direct File operations share one cursor. Stream
close does not close the underlying File. Reads report storage I/O failures.
Java and WIPI-C file/record mutations stage bytes, cursors, handles, and deletion
ledgers before publishing changes. Multi-key changes use `SaveBatchStore`.
The earlier native package retains its existing buffered write/flush policy.
See [save integrity](session.md#save-integrity) for failure recovery and limits.

A saved file can share its name with a packaged resource directory. A lookup
below that saved file falls through to the archive without recording a storage
failure. The bounded local slot service also accepts the first, zero-based
slot. See [slot creation and save overlay evidence](ktf-save-slots-2026-09-23.md).
The service answers the creation receipt with "no character yet", so the title
asks the player for a name and registers it with one more request; see
[A name the title asks for](history/ktf.md#implementation-a-name-the-title-asks-for).

## Quick save and quick load

All three kinds of KTF package can be recorded between two rounds and restored
later: the Java/AOT descriptor package, the older descriptor module and the
native package. A checkpoint is execution state only. A load leaves the title's
saves as they are, the restored title reads and writes them there, and the rule
and its accepted limits are in
[Quick load and ordinary saves](architecture.md#quick-load-and-ordinary-saves).
What is particular to this platform:

- The descriptor runtime stores every write before the call that made it
  returns, so neither step has anything to store first. What it does keep are
  host copies of the saves: the WIPI C file table, the record databases, the
  Java databases, the bytes behind each `File` object, the table of names the
  session wrote and the five removal and directory lists. A record names them
  and holds none of their bytes, and a load fills every one from the store.
- After a load a name has one host store in each table. Handles restored for
  one name share it. A name the store has nothing for keeps its restored store
  empty and findable by name, so that the next open, create, rename or write of
  that name takes the same store rather than making a second one.
- A Java database the title deletes while a `DataBase` object holds it is kept
  the same way in a running session: the next open of the name takes the
  store that object holds, and a write through the object makes the database
  again under that store. An open used to make a second store, and the two
  wrote their own record lists over each other's save. The WIPI C record
  table closes a deleted database's handles instead, so it has nothing to
  keep.
- `FileSystem.list` and `DataBase.listDataBases` answer the packaged names plus
  the names this run knew, and do not read the disk. After a load they describe
  the run the checkpoint was taken in.
- A `File` opened in the truncating mode, and a native object opened in the
  mode that empties the file, take at most what they had written after a load,
  from the front of the file as it is. Every other object takes the whole
  file, one whose open made the file among them. The WIPI C file table empties
  a file in the store at the truncating open itself, so its handles are cursors
  on the file as it is and nothing of the truncation is left to carry.
- A native package's write waits in the platform until a file is closed or a
  frame ends. A quick save and a quick load store what is waiting first, and
  are refused when the store refuses it. A name the ordinary flush could not
  store is remembered and tried again at the next quick step and when the
  session closes; the ordinary boundaries still make one attempt each.
- A native package's file is kept under its lower-cased base name, with a
  backslash read as a separator. Reads always looked a name up that way; writes
  did not split on a backslash, so a file written under a name with one was
  refused by the store and never saved. Both use the same key now.
- When two package entries share a lower-cased base name, a native open takes
  the one whose entry name sorts first, on every run.

## Diagnostics and limits

Use [CLI commands](cli.md) for `runktf`, `ktfdump`, routes, imports, profiles,
and memory watches. [Testing](testing.md) describes opt-in local probes and
what each acceptance level establishes.

The checked inventories in `internal/platform/ktf/testdata/` are authoritative
for registered Java surface gaps and fixed-value stubs:
`wipi_java_surface.txt`, `wipi_java_gaps.txt`, and `wipi_java_stubs.txt`.
A registration or static reference is not proof that a real caller executes it.

Known limits include animated image callbacks, unapplied graphics alpha,
partial widget rendering, synchronous modal input, unconfirmed native return
slots, and missing archive evidence for automatic landscape selection and the
native loading stall. Published instance-field divergence is diagnosed rather
than repaired using an invented precedence rule. Corpus startup counts do not
establish complete gameplay or save restoration.
