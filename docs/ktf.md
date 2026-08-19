# KTF platform implementation status

This platform validates a KTF archive, locates its native image, maps it into
the pure-Go ARM core and runs it: the client relocates itself, initializes, is
asked for its main class, and from there the title plays — its own Java through
the AOT bridge, its drawing through the WIPI C surface, its saves through the
shared save store. How far each title gets is tracked per title in the support
matrix ([`support.md`](support.md)); this file is how the platform works and
what it cost to find out.

## Input path

`internal/platform/ktf` currently accepts the original two-level container:

```text
KTF ZIP
  -> __adf__
  -> AID / PID / MClass
  -> <AID>.jar
  -> client.bin<decimal BSS size>
  -> native image + zero-filled BSS
```

Both ZIP levels reject unsafe paths, case-insensitive duplicate names, excessive
entry counts, oversized names, oversized individual entries, and excessive
total expansion. Required ADF fields are bounded and validated before AID is
used to select the JAR. A JAR must contain exactly one non-empty client image,
and its binary plus BSS cannot exceed 256 MiB.

One ignored local game contains a legacy JAR whose data-descriptor CRC is wrong
even though the decompressed bytes and central-directory CRC agree. The loader
accepts this narrow case only after independently checking the complete length
and CRC against the central directory. It does not disable ZIP integrity
checking for damaged contents.

## Client execution

The native image is mapped read/write/execute at `0x00100000`, matching the KTF
runtime contract. Its decimal filename suffix reserves zero-filled BSS, and the
entry point is called in Thumb state at `0x00100001` with the BSS size in `r0`.
A bounded 1 MiB stack is mapped separately.

The repository-authored entry fixture executes:

```text
push return address
  -> observe BSS size in r0
  -> SVC runtime boundary
  -> receive a result in r0
  -> continue Thumb arithmetic
  -> return to the bounded-call sentinel
```

No original game code or archive is committed as a fixture. The opt-in command
below parses ignored local games without executing their guest code:

```sh
WFEATURE_KTF_ACCEPTANCE=1 go test -run TestLocalKTFArchivesParse -v ./internal/platform/ktf
```

All 32 KTF archives currently present under `var/games/ktf` pass this parsing
probe.

A separate, explicit probe runs third-party client entry points through
self-relocation, validates that the returned `WIPI_exe`, `ExeInterface`, names,
required initialization/class-lookup functions, and function table all remain
in mapped guest memory, and calls both initialization functions:

```sh
WFEATURE_KTF_EXECUTE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesInitialize -v ./internal/platform/ktf
```

All 32 current clients return the expected `WIPI_exe`/`ExeInterface` descriptor
chain and finish both returned initialization functions with result zero. The
probe has a bounded ten-million-instruction ceiling; it does not start a game.

Initialization state remains attached to `Client`, so the next opt-in probe can
call the native image's real `ExeInterface.GetClass` function for the ADF main
class:

```sh
WFEATURE_KTF_LIFECYCLE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesLoadMainClass -v ./internal/platform/ktf
```

All 32 local archives return a non-null, name-matching, independently validated
`MClass` metadata record. Repeated explicit loads use a cache distinct from the
placeholder runtime-class table.

## Platform initialization boundary

`Client.Initialize` maps a 64 MiB platform-data arena at `0x30000000` and a
separate 1 MiB read/execute callback-stub arena above it at `0x35000000`. It constructs the five KTF initialization
arguments, including JVM/exception contexts, primitive descriptors, and the
direct callback table. Its generated Thumb stubs preserve `r4`, place a bounded
service identifier in `r12`, cross the shared ARM supervisor-call boundary, and
return through `lr`.

The startup-only callback surface supplies the WIPI C kernel and Java bridge
tables, bounded allocation, class lookup, Java object/array allocation, and
class/string registration. Individual platform allocations are limited to 4
MiB, arrays to 1,048,576 elements, strings to 65,536 UTF-16 units, and the whole
arena to its mapping. Pointers and source ranges are checked before Host reads
or writes guest memory.

### The arena and the stubs cannot share an address

The stub arena used to sit at `0x31000000`, and the data arena was grown from
16 MiB to 64 MiB without moving it — so the allocator's span swallowed the
platform's own stubs. Nothing failed at the mapping: both mappings were in
force over the overlap, so the guest's writes into what it had been handed were
permitted, and they landed on the stubs.

What that looks like from a run is worth keeping, because none of it points at
an allocator. A title that allocates past 16 MiB is handed the stub arena, and
its next screen fill writes its own 16-bit pixel value over the stub table. The
run continues until something calls one of those stubs, and then a kernel slot
executes as `strh r0, [r1, r1]` and faults on an address the game never
computed. The two failures a user reported on that title — a four-gigabyte
allocation and a null framebuffer handle — were both this, several thousand
calls downstream.

The arenas are now a gigabyte apart, a constant expression in
`initialization.go` fails to compile if they are ever made to overlap again,
and `armcore.Map` refuses an overlapping pair whose permissions differ in both
directions.

### A screen that only waits still allocates

`MC_knlFree` was an accepted no-op, on the reasoning that the arena was
bump-only and freeing was therefore a bounded lie: whatever the guest gave back
would be reclaimed with the client. That was true when the arena had no free
list. It stopped being true when the collector got one, and it was never true
for a title that allocates inside its drawing loop.

One local title's confirmation dialog runs a loop that calls `MC_knlCalloc`,
copies into the block and calls `MC_knlFree`, about sixteen times a tick. The
counts come out in pairs — 28,992 allocations against 28,962 frees over 1,845
ticks — so the game is not leaking anything. The platform was. The arena's
own growth log is what settles it: it climbs a flat ~272 KB between collection
cycles while the object collector reclaims between 0 and 144 bytes each time
and never tracks more than 29 objects, and it stops at 66,913,236 of 67,108,864
bytes. The run then dies inside a timer service on the next allocation, with a
`tick_error` naming the arena rather than any screen.

Three things about the shape of that failure are worth keeping:

- **It is counted in ticks, not in inputs.** The same screen left alone dies at
  1,813 and at 1,845 ticks across runs whose key lists differ. A user pressing
  keys sees it as intermittent, because pressing keys is what makes them sit on
  one screen long enough.
- **An idle screen is not a control.** The title screen of the same game idles
  3,000 ticks with two collection cycles and 823,308 bytes of arena. Only the
  screens whose animation allocates show it, so "it does not happen when I wait
  at the menu" says nothing.
- **The error names the wrong layer.** It surfaces as whichever service made
  the allocation that finally failed — a timer, here — several hundred thousand
  successful allocations after the one that should have been released.

`MC_knlFree` now releases the block, and `allocateWIPIC` records every block's
arena size against the identifier it handed out, because the guest's free is
given the identifier and nothing else. An identifier this platform did not
issue — a double free, an interior pointer — is counted as
`wipic free of an unknown allocation` and ignored: the specification makes it
the program's error, and a handset does not end a program over it. Releasing on
that key twice would be worse than not releasing at all, since it hands one
address to two owners.

Measured after the change, on the same title and the same key list: 6,000 ticks
against the 1,845 it used to die at, **three** collection cycles instead of 243,
the arena settling at 1,349,588 bytes instead of climbing to the mapping, and
112,086 allocations against 112,058 frees. The count of
`wipic free of an unknown allocation` is **zero**, which is what says the
identifier the guest passes free is the word calloc returned rather than the
payload pointer twelve bytes in — the assumption the whole change rests on, and
the one number worth checking first if this ever appears to regress.

Two consequences that are easy to miss, and are the reason a working free is
not a one-line change:

- **`MC_knlCalloc` has to write its zero fill now.** It used to be free: every
  block was a slice of a freshly mapped arena. A reused block carries the
  previous tenant's bytes, and a title that allocates a structure and reads a
  field before writing it would get them.
- **Host state keyed by a guest address has to be dropped with the block.** The
  transparency mask an image or framebuffer carries lives in a map keyed by its
  handle, so a released address that kept its mask would impose it on whatever
  was allocated there next — the same class of black-box-behind-the-sprite
  failure described under "A transparent pixel still has a colour" in
  `docs/lgt.md`, arriving from the opposite direction.

Deliberately not done with it: `MC_grpDestroyImage` and
`MC_grpDestroyOffScreenFrameBuffer` remain accepted no-ops. They are a much
smaller leak — 27 image creations against 15 destructions over the same 1,845
ticks — and releasing an image record that a live graphics context still points
at would corrupt drawing on every title rather than stop one screen. The
identifiers are registered, so the day a title is measured leaking through them
the release is already available; what is missing is the evidence that one
does, and a reason to believe no context outlives the image.

**What a working free newly makes possible, and how it would be found.** A
block a title frees and keeps using was harmless while free did nothing,
because the address was never handed out twice. It can now become a Java object
or another buffer, so the failure this change introduces is a title writing
into memory it gave back. Seven titles across 1,500 to 6,000 ticks, and all 32
booted, show nothing of the kind — but a short run is the wrong instrument: by
its nature this appears as intermittent corruption in a long session rather
than on a first screen.

The heaviest free-user in the table below — the one that exercises this path
two orders of magnitude harder than any other — has since been driven 6,000
ticks through a save-slot menu, a play screen, movement across two areas and an
NPC dialogue, with no fault and no corruption in any sampled frame. Its arena
climbs through four collection cycles and then **holds flat for the last 2,500
ticks of active play**, which is the shape a working free produces and the
opposite of the pre-fix climb. That lowers the prior considerably. It does not
close the question, because one long run on one title is still not the
instrument for an intermittent fault.

That run also settled the discriminator below on real data. This title's
calloc and free counts do **not** balance during play — 63,597 against 52,375
by tick 6,000 — but the gap was 12,509 at tick 3,500 and 11,222 at tick 6,000.
It shrank across 2,500 ticks of movement and a new screen, so it is a held
working set (visible sprites, an open dialogue, background tiles) rather than a
leak. An unbalanced count is not the signal; a *growing* gap is.

**The instrument was built rather than waited for.** A symptom is a poor
trigger for this one: the failure it would produce is a wrong sprite or a fault
at an address the game never computed, arriving randomly rather than on a
repeatable screen, so waiting for one to be noticed means waiting for the fault
to become visible as well as to happen. Marking makes the question decidable
instead — a released block is filled with a byte no guest value looks like, and
the fill is read back where the arena hands those bytes out again. Intact means
nothing wrote there while the block was free; a difference is a write that
happened after the release, reported as `arena use after free` with the block,
the offset and the bytes found there, which is enough to put a watchpoint on the
address and catch the writer on the next run.

Three details decide whether it works, and all three are why it belongs to
`guestArena` rather than to `MC_knlFree` (`arena_poison.go`):

- **The check has to know which bytes were marked.** A block from the free list
  is reuse; so is a cursor allocation below the arena's high-water mark, because
  a release at the top of the arena goes back to the cursor rather than to the
  list. Space past the high-water mark has never been handed out and must not be
  checked, since nothing marked it.
- **The object collector releases into the same arena.** A fill applied to the
  guest's frees alone would read every collected object as a use after free.
- **The pattern must not look like an arena address.** The collector reads every
  committed word as a possible reference, so a mark that pointed into the arena
  would keep dead objects alive for as long as a block stayed free. `0xdf`
  repeats into a word far above the arena, which the extent index rejects on two
  compares.

Both halves walk the bytes of every block released and every block reused, so
they are installed in debug builds alone; a release build's arena behaves exactly
as it did. How much they covered is reported alongside what they found —
`arena blocks marked on release` and `arena blocks checked on reuse` — because a
report with no fault in it says nothing unless it also says the detector was
running.

**What it has read so far.** All 32 archives booted for 400 ticks: no fault, and
no false positive from the platform's own paths either. The heaviest free-user
driven 5,545 ticks through its save-slot menu into play and movement: 166,396
guest frees, 166,399 arena releases, **185,969 blocks handed out again and
checked, and not one byte of a mark had been touched**. Three more titles driven
the same way add 30,481 marks and 34,976 checks, also clean — and they change
the shape of the earlier table, because two of them free almost nothing in a
boot window and tens of thousands of times in play. **A boot window does not
rank titles by how hard they use free.** That is a far stronger
reading than the frame comparisons it replaces, because it does not depend on
the fault becoming visible — but it is still evidence about the titles and the
paths that have been run, and the question stays open in the same shape: a title
that keeps a pointer past a free would be found the first time it did it, and
nothing here has.

### What the whole local set does with free

All 32 local KTF archives were booted for 400 ticks with no frames, to collect
two numbers each: how often the title calls `MC_knlFree`, and how often it
frees an identifier this platform never issued.

**Nothing frees an identifier this platform did not issue** — the second number
is zero on all 32. That retires the assumption the change rests on across the
whole set rather than on the one title it was diagnosed from: the word a KTF
title passes free is the word calloc returned, not the payload pointer twelve
bytes in.

Free is used by 13 of the 32 in that window, and the spread is two orders of
magnitude: one title calls it 21,147 times in 400 ticks, the next-heaviest 116,
and the median user of it a handful. Three things about that distribution are
worth keeping:

- **A boot window is not a title's whole behavior.** The other 19 titles free
  nothing before their title screen, which says nothing about their menus or
  their stages. A title moving from zero to non-zero the first time somebody
  runs it deeper is expected, and is not a regression.
- **Free count does not predict who was at risk.** The arena fills by bytes.
  The title that died was making about 16 allocations a tick; the heaviest
  free-user makes four times that and had never been seen to die. Whatever the
  second title's blocks are, they cannot average anything near the 2.3 KB of
  the first, or it would have reached the ceiling inside 450 ticks — which is
  a bound on the product of count and size, and the reason a count alone is the
  wrong risk proxy.
- **More frees than callocs is not a fault.** Two titles show it. Freeing null
  is legal and is counted, and several platform calls hand back a buffer the
  specification says the caller frees — `MC_grpEncodeImage` returns one — so a
  title can legitimately free more identifiers than it allocated.

**An imbalance during boot is retention, not a leak.** Every title that frees
at all holds more than it releases in this window, which is what booting *is*:
a game allocates its assets and keeps them. The heaviest holds about 5,700
blocks by tick 400. This matters for the next person to see a `tick_error`
naming the arena, because it will look exactly like the defect above and may
not be: the discriminator is whether `wipic 0x15` and `wipic 0x16` stay
*balanced in steady state* once boot is over. Balanced and still dying is the
platform's problem. Diverging steadily during play is the game holding memory,
and no amount of work on free will change it.

Java object allocation now writes the KTF `fields`/`class` pair, a bounded
vtable-table index header, and zeroed instance fields before pinning the same
address to a Go JVM object. Primitive and reference arrays additionally carry
their length and correctly sized zeroed elements. Runtime-created array class
descriptors use the same validated AOT registry.

Class lookup and Java-string registration now allocate bounded guest handles
and bind them to runtime-owned `java/lang/Class` and `java/lang/String` objects
in the shared Go JVM. Native class registration parses the KTF class,
descriptor, null-terminated method/field tables, vtable, and full-name records
with explicit bounds before storing semantic metadata in the JVM. Guest
addresses remain opaque identifiers and are never treated as Host pointers.

`GetJavaMethod` and `GetField` resolve the registered metadata with bounded
superclass traversal. `JavaJump1`, `JavaJump2`, and `JavaJump3` make nested
calls through the same ARM core while preserving the suspended outer context,
so repository-authored AOT method bodies can now execute through the Java
bridge. The current path returns the normal `r0` result. Java jumps and native
calls share a maximum nesting depth of 64 before any further guest body is
entered.

`CallNative` accepts the original two-word guest argument container, validates
its alignment and read/write range before execution, and calls the supplied
native body with that pointer in both `r0` and `r1`. A normal return writes
`{result, 0}` back to the container and returns its address without modifying
the suspended outer context.

`JavaThrow` validates and walks at most 64 linked raw handler records, reads
each method's bounded exception table, and uses the shared JVM hierarchy to
select a typed catch. A match makes that record the chain head and transports
its context base and target through `r0`/`r1` to the validated restore PC. This
low-level unwind works through both Java jumps and native calls. Each throw now
constructs the guest object layout, pins the paired Go JVM object, and returns a
typed exception retaining both forms when no guest handler matches. The handler
head is a thread-local guest word: nested calls inherit it, while independent
ARM threads cannot corrupt each other's chains.

### The same lie, one table over: the graphics destroys

`MC_knlFree` was not the only accepted no-op resting on "the arena is reclaimed
with the client". `MC_grpDestroyOffScreenFrameBuffer` and `MC_grpDestroyImage`
were the same reasoning on the graphics table, and they outlived the fix to
free because nothing in the local set had yet been driven far enough into play
to fall over on them.

A newly added title does. It draws each frame into an off-screen buffer it
creates and destroys again — 49,109 creates against 49,109 destroys over 6,100
ticks, a pair every time, so the game is not leaking anything — and each
create is two arena blocks: a 240x320 pixel allocation and the twenty-byte
record naming it. The arena climbed a flat ~264 KB between collection cycles
from the first field screen onwards and never came back down, and the run died
at tick 26,332 with the screen black:

```
"tick_error": "service KTF timer at 0x110031: handle supervisor call 0x3 at 0x35000ed8:
               allocate KTF pixel buffer: KTF platform initialization data space exhausted"
```

That is about six minutes of play. **A user would have read it as the game
freezing, not as memory** — there is no message, the last frame stays on
screen, and the failure surfaces inside a timer service rather than at any
drawing call the player could connect to what they were doing.

Two things worth keeping beside the earlier free write-up:

- **The collector's own cost is the second symptom, and it shows first.** A
  cycle scans every committed word, so it grows with the arena: 700 µs at the
  first field screen, 16 ms at 17 MB, 94 ms at 59 MB. Long before the ceiling
  the game is losing whole frames to collections that find nothing, which is
  what "it gets choppier the longer you play" looks like from a report.
- **A leaked destroy does not show up in `wipic 0x15`/`0x16` at all.** The
  balance test in the section above reads the kernel table, and these blocks
  never go through it: the create is `wipic 0x30004` and the destroy `0x30003`.
  Reading the *graphics* create/destroy slots as a pair is the same test, and
  it is what named this one.

Three allocations were being kept where the platform had promised to give them
back, and the third is the specification's rather than this platform's:

- **The off-screen framebuffer**: the record and its pixel allocation. The
  specification also says the call does nothing when it is handed the *screen*
  framebuffer, and obeying that is not politeness here — this platform hands out
  one cached record for the screen, so freeing it would take the screen from
  every later caller rather than from the one that asked.
- **The image**: the record, the pixels decoded into it, and the framebuffer
  record `MC_grpCreateImage` builds and then copies word for word into the image
  record. That inner record is dead the moment it is copied; it was leaking
  thirty-two bytes for every image any title had ever created.
- **The image's encoded source buffer.** `MC_grpCreateImage`'s contract is that
  the buffer it is handed "is released inside the image function", which is why
  the specification requires it to be an `MC_knlCalloc` identifier. The title
  relies on it: it callocs a buffer per image and frees none of them, and its
  outstanding allocation count tracked its image count exactly. Freeing it at
  destroy rather than at create covers the animated case the specification
  splits out, which this platform does not decode.

A handle the platform never issued is ignored rather than fatal, on the same
reading `MC_knlFree` takes: a double destroy arrives here as a block already
gone. It is ignored *silently*, unlike a stray free, because a Java array
reaches the image calls as a handle too and counting every one of those would
bury the diagnostic that names a real stray.

**The other two platforms do not have it.** LGT's `destroyOffscreen` and
`destroyImage` slots both call `releaseSurface`, and SKT has no guest arena at
all — an SKVM image is a Go object on the shared JVM and is collected there.
This was a KTF-only omission.

**What it cost the rest of the set.** The same 6,100-tick route now peaks at
1.1 MB of arena with three collection cycles in the whole run, against 17.5 MB
and fifty-two before. First frames of all 33 local KTF archives are unchanged
except for the six that differ from the baseline run against itself — the
documented noise floor of this comparison, below.

### The kernel table beyond the specification

The kernel interface is a flat table of 65 entries and its order is the WIPI
1.2.1 `MC_knl*` list with three extra slots inserted after `MC_knlSprintk`;
every entry from `MC_knlExit` onwards therefore sits two above the number the
specification gives it, which is what makes `0x7`, `0xe`, `0x14`-`0x1d`,
`0x1f` and `0x20` line up. Two entries past the specification's end are
implemented from what a title does with them.

**`0xd` is `MC_knlGetAccessLevel`,** a bitmask of the API groups the program
is permitted to use. **Every group is granted.** The question is what the
handset permits, not what this platform then does about a particular call, and
those are not the same question.

It used to withhold the network and serial bits, on the reasoning that a game
which checks the bit before it dials skips the attempt instead of handling a
refusal. One title showed what that costs: it reads the mask during startup,
requires `& 0xbc` to equal `0xbc`, and stops at its own error screen — the one
that says `인증오류 ... (오류번호:1001)` — when it does not. It never reaches a
network call to be refused at, so withholding the bit saved it no attempt and
simply ended the game. Granting every group was then measured title by title
across the local library: same frames, and no network call any of them was not
already making. The refusal still happens where it always did, at the call, and
`docs/network.md` carries that decision and the surface it covers.

**`0x24` is `MC_knlGetDLLInterface(name, major, minor, *rtnMajor, *rtnMinor)`,**
which answers a handset library by name. A name this handset does not carry is
answered with null, which is documented and which the caller's own error path
reads; fabricating a table would be a surface with no contract behind it.

The one library a local title asks for is a **user-space memory manager**
(`wipic_usermem.go`), and the reason it can be served rather than refused is
that the game hands it a buffer out of its own image and then carves that
buffer into fixed blocks. The pool is guest memory the caller supplies and the
sizes come from the caller too, so serving the calls is bookkeeping rather than
inventing an allocator's address space. Blocks are cut from the front and never
given back: the caller allocates its fixed set once at startup and holds it for
the run.

The call site is worth knowing about, because it is not a stub table: the game
loads the interface pointer, indexes it, and `bx`es through a veneer, so the
slot number never appears in the trace. The library was found by taking the
link register out of the failing call, disassembling there, and reading the
offset it indexed.

## AOT outer calls and runtime Java bridge

`Client.NewObject`, `InvokeVirtual`, and `InvokeStatic` share one serialized
outer-call path. Method descriptors validate arguments before guest execution;
integer, floating-point, category-2, and bound reference values are converted to
the KTF word ABI. Every pinned object also has a stable reverse guest-address
lookup. The wrapper enters normal or native method bodies with the appropriate
register/container layout, records every bounded ARM run required by exception
restore re-entry, and decodes the return registers into JVM values. A synthetic
class fixture covers constructors, instance/static methods, integer results,
and object-reference returns.

Runtime-owned Java methods use JVM `InvokeSpecial` when an AOT subclass calls a
native superclass implementation. Each currently exposed method is also
materialized as validated KTF class/method metadata. Two SVC proxies are emitted
because real clients use both observed ABI forms: a normal `fn_body` entry with
words in `r1`-`r3`/stack and a native entry with an argument-container pointer
in `r1`. Both are bounded by descriptor slot counts and guest memory checks.

Runtime-owned Java classes now publish complete KTF metadata: a guest vtable
spanning the class hierarchy with each method record's vtable slot, method and
field counts, and field records. Static fields keep their value word inline in
the field record, matching the original ABI. Object headers encode a dispatch
alias — a bounded copy of the class record inside the platform arena — as a
shifted offset relative to the JVM context. Real clients read the alias's
vtable pointer for virtual dispatch and its descriptor pointer for array store
checks; both consumers were pinned by disassembling a real client and dumping
its reads. `RegisterClass` also runs the registered class's own `<clinit>`
once, because guest statics live inside field records and stay zero otherwise.

The guest's `instanceof`/`checkcast` primitive is answered by walking the
guest's own class records rather than the Host's registry, because a game asks
about classes long before it registers them: a hierarchy whose middle class is
unregistered would otherwise be undecidable, and an undecidable answer stays
permissive. The class descriptor carries the implements list — a pointer table
at offset 16 whose length is the count at offset 30 — so an interface target is
decided from the interfaces each class in the chain declares, including the
ones those interfaces extend. Only a walk that runs out of superclasses answers
no; a read failure keeps the permissive answer and records why. This matters
beyond casts: a wrong yes sends the guest down a branch written for another
type, which is how a map's entities stopped acting while the game still drew
and still took input.

The runtime Java surface now covers the boundaries real constructors and early
`startApp` bodies cross: `java/lang/String` with the original guest field
layout (`value`/`offset`/`count` backed by a real `[C` array), `StringBuffer`,
`System` with a discarding `System.out`, `Thread` construction and queries,
`Math`, `Integer`, `Random`, `Class.getName`/`getResourceAsStream` over attached archive
entries, the CLDC IO streams (including newly authored `OutputStream`,
`ByteArrayOutputStream`, and `DataOutputStream` core classes), an in-memory
`org/kwis/msp/db/DataBase` with zero-based WIPI record identifiers,
`org/kwis/msp/io/File`/`FileSystem` over archive resources and in-memory
writes, `Clip`/`Volume`, `Font` metrics and attributes, `Graphics`, `Image`
creation, cropping, and composition, `HandsetProperty`, `BackLight`, the lwc
component hierarchy including the text components, the WIPI `EventQueue` with
its `Jlet`/`Display` listeners, the database exceptions and the
filter/comparator callbacks `DataBase.sortRecord` calls back into, `Vibrator`,
and `org/kwis/msf/io/Network`.
KTF's platform charset is EUC-KR: the shared JVM decodes `String` byte
constructors and encodes `getBytes` through `golang.org/x/text`. JVM-raised
exceptions cross back into the guest's raw handler chain, so guest `try`
blocks observe runtime failures such as a missing-file `IOException`.

### A class left out of that table still resolves, and answers nothing

A platform class the guest names is resolved by `ensureJavaClass`, and a name
that table has never heard of does not fail there: the fallback allocates a
real class record with a name, no super, and an empty method table. The guest
gets a valid class back and only finds out when it asks that class for a
method, so the gap surfaces as `method … was not found from class …` at the
first call rather than as anything at registration or startup.

That delay is the whole cost of the omission. `java/lang/Integer` was missing
from the table entirely — the JVM has had `parseInt` since the CLDC builtins
were written, and nothing published it to guest code — and the failure it
produced was a title erroring out when its quest window opened, several menus
into a save, because that screen is where it parses a number out of a string.
Nothing before that had needed one.

So the classes here are not a record of what the JVM implements; they are a
separate promise, and a method published with no implementation of its own is
promising that the JVM already owns a body for it. Nothing checks that promise
when it is made, which is what
`TestDelegatingRuntimeMethodsHaveAJVMBody` is for: it asks the VM for a body
for every delegating method at the descriptor it was published under, so a
descriptor that does not match the builtin fails in the test rather than in a
game. What that test cannot see is a class nobody published at all: `Integer`
now publishes every method the JVM implements for it, but the remaining CLDC
wrappers — `Long`, `Character`, `Boolean` — have no implementation anywhere to
publish, and none has been observed being called. They stay unwritten rather
than guessed at, and a title that wants one will say so in the same sentence
this one did.

Bound arrays synchronize bidirectionally at every runtime-call boundary:
argument arrays refresh from guest bytes before the JVM runs and JVM
mutations write back afterwards, because guest code parses array content
directly from memory. Interpreted bytecode that dispatches into AOT-only
classes crosses back into guest ARM code through the JVM's AOT invoker,
nesting on the thread suspended at the active supervisor call. The guest
filesystem mirrors the original mount: every outer-archive file is reachable
by bare name with its private `P/` or `p/` prefix removed, `File` and
`FileSystem` read that view plus in-memory writes, while
`Class.getResourceAsStream` and the WIPI C resource calls read the inner JAR.

**A deleted file is recorded as deleted, in `fs/.removed`.** Neither layer
under the writable table can be deleted from — the save boundary has no delete
and the mount is the game's own package — so dropping the in-memory entry and
nothing else leaves the file resolving from the save underneath it. A title
that deletes a file and then asks whether it is there is told yes. `unlink`,
`remove` and the old name of a `rename` all go on the list; a write to the
path takes it off; `exists` and `list` both consult it. No KTF title here has
been seen to delete and re-ask, so this is a correction made from the LGT
platform's evidence rather than from a failure of its own — two titles there
lost their entire opening sequence to it, each having deleted its save slot to
start a new game and been told the save was still there. See
[`lgt.md`](lgt.md).
WIPI C databases seed their initial content from a mounted file with the
database's name, matching packaged save data.

### The two storage tables

There are two, and a game picks whichever suits what it is storing. Table 7 is
the **stream database**: one blob, read and written through a cursor, seeded
from a packaged file of the same name. Table 5 is the **record database**
(`wipic_record_database.go`): a numbered set of records, opened by name with a
record size, addressed by a one-based id.

**The stream table's slot 6 is `MC_dbDeleteDataBase(name, mode)`** — a name,
not a handle: the caller closes the database and then names it. Deleting it has
to cover every place an open would look, and the persisted copy cannot be
unlinked because the save store has no delete, so the name goes on a removal
list (`db/.removed`) that every lookup consults, exactly as the guest
filesystem records one. Opening the name for creation takes it off again;
without that a title that deleted a database and wrote a new one would have
every later write hidden. One title deletes the certificate it could not renew
and expects the next question about it to answer "no such database".

Two things about the record table are worth stating because getting either
wrong is silent.

**The packaged file carries a `.db` suffix the database name does not.** A game
opens `FUNTER_DL`; the archive ships `P/FUNTER_DL.db`. Both spellings are tried
on open. The packaged format is a 45-byte header — magic `qtcdb`, record size
at offset 8 — followed by one slot per record, each a live flag then the
record's bytes.

**Ids are positions and a delete leaves its slot.** Reusing a freed id would
rename a record the game already holds an id for, so a deleted record becomes an
empty slot and the next insert takes a new id past the end.

Slot 6 carries two call shapes with one signature: `delete_record(handle, id)`
and a name-keyed `delete_database(name, flags)`. A handle this platform issued
is the only thing that separates them, so anything else is read as the name
form.

**Why a stub was not neutral.** Before table 5 existed a game asking for it got
zero, which its loader reads as failure — and then carried on with the buffer it
had allocated, which is zeros. One title keeps its cipher keys in a packaged
record database and decrypts its settings with them: every setting decoded to
zero, and its frame interval, chosen from a table by that zero, became the
slowest entry in the table. The game asked to be woken every 500ms and ran at
two frames a second with nothing failing anywhere and the emulator 90% idle.
Implementing the table took it to 30 rounds a second. A storage call that
answers "no" is not a safe default: it is a value the game will believe.

The WIPI C surface implements the startup and early-lifecycle kernel calls
(indirect allocation, memory queries, current time, system properties, program
name, `sprintk` formatting, resource lookup and reads, timer registration),
the graphics table
(display info, screen and offscreen framebuffers, graphics contexts, pixel
conversion, fill/line/copy, encoded-image decoding and drawing, font metrics),
the KTF database stream extension in process memory, and accepted no-ops for
media, misc, and the original runtime's stub tables. Registered timers are
recorded for the future lifecycle event loop and LCD flush is accepted without
Host presentation yet.

After startApp the Host drives progress through three client services:
`ServiceTimers` runs registered WIPI C timer callbacks, `ServiceThreads` runs
queued guest Java threads (KTF's `Thread.start` queues cooperatively instead
of spawning a goroutine, because guest threads share the single ARM core),
and `ServicePaint` invokes the top pushed display card's `paint` with a
Graphics whose color, clip, rectangle, line, and image operations draw into
the WIPI C screen framebuffer. `MC_grpFlushLcd` and `ServicePaint` convert
the RGB565 screen buffer to the Host RGBA frame exposed by `Client.Frame`.

The lifecycle probes are:

```sh
WFEATURE_KTF_CONSTRUCT_ACCEPTANCE=1 go test -run TestLocalKTFArchivesConstructMainClass -v ./internal/platform/ktf
WFEATURE_KTF_START_ACCEPTANCE=1 go test -run TestLocalKTFArchivesStartMainClass -v ./internal/platform/ktf
WFEATURE_KTF_FRAME_ACCEPTANCE=1 go test -run TestLocalKTFArchivesRenderFirstFrame -v ./internal/platform/ktf
```

All 32 ignored local archives complete their no-argument main-class
constructor and all 32 return normally from `startApp([Ljava/lang/String;)V`.
The startApp probe allows fifty million instructions because real games spend
tens of millions decompressing tables before first returning;
`WFEATURE_KTF_MAX_STEPS` overrides the ceiling for investigation. **All 32
render a real first frame** through the frame probe, including recognizable
publisher splash screens; `WFEATURE_KTF_FRAME_DIR` saves each frame as PNG,
`WFEATURE_KTF_ONLY` filters archives by substring, and
`WFEATURE_KTF_FRAME_ROUNDS` overrides the 64 service rounds (one title's
loading timer needs about 100 callbacks before its splash appears).
Guest waits run on the session clock (see [Pacing](#pacing)), and every
`Thread.start` queues for the cooperative service loop through the JVM's
GuestThreadStarter hook.
Graphics renders text with the shared runtime glyphs from `internal/glyph`:
the authored 5x7 patterns cover ASCII on the large face, and every other
character (Hangul first among them) rasterizes from an embedded pixel face,
thresholded to one bit and cached per rune.
Which face it rasterizes from is the descriptor's answer. `__adf__` declares
the screen the game was built for — `DisplaySize:176*220` or
`DisplaySize:240*320`, and the local library splits about evenly between the
two — and each handset generation carried its own Korean face: 12 dots on the
176x220 phones, 16 on the 240x320 ones. **There are two embedded faces, one per
size, rather than one face scaled to both**: NeoDGM (`fonts/neodgm.ttf`, OFL
1.1 — the face the original handsets render with) is drawn on a 16-unit em and
is exact at 16, and Galmuri9 (`fonts/galmuri9.ttf`, OFL 1.1) is exact at 10,
where its ink stands as tall as NeoDGM's does scaled to 12. A pixel font is
only itself on its own grid, and the `fonts` package is explicit about what
Galmuri is not: it is a modern face whose shapes come from the Nintendo DS
rather than a Korean handset, chosen because it is the small Korean face on
hand that is exact at its design size and free to redistribute. Screenshots of
the 176x220 titles are therefore not comparable against a reference the way the
240x320 ones are.

`descriptorFace` reads the declaration once at startup and the runtime holds
the face for the run, because a game reads its own layout out of the font's
metrics: a 176x220 title told a line is 16 pixels tall rather
than the 12 its handset answered sizes its menus too large and then draws text
too large to sit in them. Font metrics follow the chosen face — 16 tall with a
12-pixel baseline, or 11 tall with an 8-pixel one — and `MC_grpGetStringWidth`,
`Font.stringWidth`, and `Font.charWidth` measure with the same per-glyph
advances the renderers draw with. A descriptor that declares no legible size
keeps the 16-dot face, as do the platforms with no descriptor to read.

Text is drawn by coverage rather than by a bit per pixel: `Graphics.drawString`
and `MC_grpDrawString` mix the colour into the framebuffer by how much of each
pixel the outline covers, in the 5/6/5 space the framebuffer already holds.

**Neither face needs that any more, and the reason is why there are two of
them.** A face on its own grid covers whole pixels, and a fully covered pixel
writes the colour untouched — exactly what plotting a bit did. Coverage was
what made a scaled face bearable: NeoDGM asked for 12 pixels is three quarters
of its design grid, so a two-unit stroke covers a pixel and a half, and
rounding that half up gives a stroke a pixel too heavy in some places and not
others, fills in the counters of `복` and `했`, and reads bolder and muddier
than the font is. Blending softened it rather than fixing it. Giving the small
screens a face that is exact at its own size fixed it, and the coverage path
stays for the case where a face is asked for a size its design cannot hit.

Three boundaries took the first-frame probe from 15 of the eighteen archives
then present to all of them:

- `Card.repaint`/`Card.serviceRepaints` were accepted no-ops, so a game whose
  own thread loop pairs them never painted. They now implement the MIDP
  semantics: repaint records a dirty flag and serviceRepaints synchronously
  paints the receiver card into the screen framebuffer and presents it.
- Mutable images (`Image.createImage(II)`) are now backed by a guest
  offscreen framebuffer, start white per MIDP, and `Image.getGraphics`
  returns a Graphics whose operations draw into that framebuffer through the
  shared WIPI C pixel path; `Graphics.drawImage` blits framebuffer-backed
  images row by row. This makes double-buffered games visible — they
  previously composed frames into an image nothing could read.
- One title identifies itself by a program name baked into `client.bin`
  ("010100D5") rather than its ADF AID; `ProgramNameForAID` carries the same
  override table as the original runtime. Its remaining crash was
  database semantics: `MC_dbExists` must answer 0 on success and M_E_NOENT
  (-12) when missing — answering 0 for "missing" made the title parse an
  empty save and dereference null. Database slot 5 is likewise the KTF
  custom `db_stat_by_name` (name, int32[3] out, mode), not UpdateRecord.

One local title needed two more of those slots to get past its own save file.
Database slot 15 is a KTF extension the original emulator leaves unimplemented
too, so its meaning comes from the one caller: the save loader opens the
database, seeks to the end for the size, and — when the size is non-zero —
calls slot 15 with the handle alone before seeking back to the start. The
disassembly reaches it through the `bx r2` trampoline with only `r0` set, and
drops the result into a scratch local that the following seek overwrites before
anything reads it; the cursor is reset immediately after, so any positioning it
performs is unobservable. It therefore validates the open stream and answers
zero, counting a `cdb slot15 <name>` diagnostic so a second caller with a
different shape is visible rather than silently accepted. The other missing
slot was plain `MC_grpDrawRect` (graphics 10), which outlines the same box
`MC_grpFillRect` fills — a rectangle too thin to have an interior is drawn as
the solid line it describes, because four overlapping edges would plot its
pixels twice.

**Graphics 25 is `MC_grpRepaint(lcd, x, y, w, h)`,** which queues a repaint
rather than painting: the specification is explicit that it puts an event in
the queue so the paint callback is called later rather than calling it from
inside the function. The region is discarded here because a card is repainted
whole.

## A published instance field has two storages, and the boundary is where they agree

Most of what a runtime-owned class holds never reaches guest memory: the guest
asks for it through a method call and the Go side answers. A **published**
instance field is the exception — a field the class declares in its KTF
metadata, at an offset guest code compiles into a load. `java/lang/String` is
the only class that publishes any, because the client's own helpers read a
string's characters through `value`, `offset` and `count` rather than calling
`charAt`.

That leaves the one thing an array and a static field both avoid. An array made
guest memory its only storage; a static field's value word lives inside its own
field record. A published instance field cannot do either, because the Go side
of it is a Go value rather than a word — a string's characters are a Go
`string`, and no offset in guest memory holds them. So the two storages are
made to agree at the boundary the guest crosses, which is where a runtime call
already reads its arguments (`field_sync.go`).

**Out: a constructor is what decides a published field, and it runs after the
payload exists.** A guest `new` allocates the instance, and the payload it gets
describes the object as allocated — for a string, `value` is null. The
constructor then runs on the Go side and the payload is never told. One local
title builds every string it parses this way, 227 of them before its title
screen, and every one of them handed the guest a null character array; the
other nineteen archives build none, which is why nothing pointed at this until
the counter existed. Publishing runs after a method the class names as a
mutator rather than after every call: `String.charAt` is the most-called
runtime method any local title has, at 16,525 calls across the local archives,
and two guest reads apiece to re-check a string that cannot have changed is a
cost with no case behind it. A publish that finds the payload already spelling
the same characters writes nothing, so a constructor called twice does not
strand a second array.

**In: whatever the guest wrote is read, and reported rather than repaired.**
Between two runtime calls only the guest runs, so a payload that no longer
matches its Go value was written by the guest. That direction counts a `field
divergence <class>` diagnostic and changes nothing. **No local title has
produced one** — across the twenty local archives the counter is zero — and a
repair built on no observed case would be a guess about which side is right.
The detector runs in debug builds, where a crossing can afford the two reads
that would name the first title that does it.

`TestFieldSyncTableCoversPublishedInstanceFields` is what makes this the
mechanism rather than one class's fix: a class that gains a published instance
field without saying how it stays in step fails there rather than diverging
inside a game.

## Host integration

`ktf.Session` composes the whole sequence Hosts share: open archive, load
client, set the program name, attach resources and filesystem, execute the
entry and initialization functions, construct the ADF main class, and call
`startApp`. `Session.Tick` runs one cooperative service round (timers, one
queued thread slice, card paint), `Session.Frame` exposes the last flushed
RGBA frame, and `Session.SendKey` dispatches WIPI key events (`type` 1/2/3
for press/release/repeat; codes -1..-16 for navigation and soft keys, ASCII
for digits, star, and hash) to the pushed cards from top to bottom,
propagating while a card's `keyNotify` returns true. A guest `MC_knlExit`
surfaces as `ktf.ErrGuestExited`, which Hosts treat as a clean game end.

Guest Java threads run on worker goroutines with private ARM stacks (mapped
from `0x20100000`, one `ThreadStackSize` apart, reused after a thread ends)
and hand execution off strictly: `ServiceThreads` grants one step slice
(default 2M, `SessionOptions.ThreadSliceSteps`) and blocks until the worker
parks, finishes, or fails. Parking freezes the worker's whole nested guest
call stack via the armcore step-budget hook, so an endless main loop keeps
progressing tick after tick; `Thread.sleep` also ends the slice, and the worker
stays out of the queue until the sleep has elapsed (see [Pacing](#pacing)).
`StartSessionAsync`/`PendingSession.Pump` run the long startApp the same way:
each pump grants an 8M-step startup slice, keeping a browser event loop alive
during initialization.

A Host service call — a timer round, a key delivery, a paint — runs guest code
on the client thread under a renewable budget (`beginHostService`). Reaching
the step ceiling there is not a failure: the limit hook grants a fresh window
while the Host's context is live and the call still has allowance left
(`SessionOptions.ServiceSteps`, default 500M), and fails with
`ktf.ErrServiceStepLimit` once it does not. Guest code legitimately spends far
more than one window inside a single call — a title reads a save plus its
enemy, drop, and map tables inside the `keyNotify` that picks a save slot, well
past the 50M window — and failing that call is indistinguishable from the game
crashing. The allowance is what still ends a call that will never return, and
cancelling the context is the Host's abort path. It is the only one that works
mid-call: while the client thread runs, no worker thread, timer, or paint runs,
so a guest waiting inside a service call for another thread waits forever no
matter how much execution it is granted. The native CLI wires SIGINT to it
through `signal.NotifyContext`.

## A BMP declares its transparent colour in the header

One local title ships its art as 8-bit BMPs — 1,301 of them, extensions of
`.png` and `.dat` notwithstanding — and BMP has no alpha channel. The
transparency is declared in two fields the format leaves spare: **`bfReserved1`
is 1, and `biClrImportant` is the transparent palette index**. Both platforms
decode BMP through `wipic.DecodeBitmap`, which clears that palette entry's
alpha, so the transparency then travels exactly as an encoding with an alpha
channel would and every draw path reads it the same way.

Without it that title drew every sprite as a cut-out on a solid green
rectangle: its title character, its menu portraits and its map tiles all sat in
green boxes, and the field looked like flat green ground because the tile
layer's backdrop was being painted.

The reading was checked against the whole set rather than inferred from one
image. Of the 1,259 images that set `bfReserved1`, every single one names an
index holding that same backdrop green, with no exceptions; the 42 that do not
set it are the backgrounds, which have no backdrop to drop; and `map/tile`
sets it on all eleven tiles while `map/back` leaves it clear on nine of its
eleven backgrounds. Two other local titles carry one such BMP each and their
index names a pink instead — **the colour belongs to the title, the mechanism
belongs to the format** — so nothing here has a transparent colour written into
it.

The index is bounded against the palette the file actually carries rather than
trusted: one of those two titles declares an index of 304 against a 256-entry
palette, and an image that names no palette entry stays whole.

What this does not settle is what should be *behind* a transparent tile. That
turned out to be a different question with a different answer, below.

## LBMP: the handset's own bitmap

One local title ships five images no standard decoder recognises. They begin
`LBMP`, and behind a 24-byte header they are the pixels the LCD would hold —
no compression, no palette, nothing to sniff. The image router handed them to
the standard library, which reported an unknown format, and the Java side turns
that into `IllegalArgumentException("undecodable image")` — so those five
images are not drawn badly, they are not drawn at all.

It is a vendor's format and it is **not in the WIPI specification**, so the
files themselves had to settle it. `internal/wipic.DecodeLBMP` reads the header
as magic, depth, width, height, payload length and a sixth word; the five files
fix the first five fields in one arithmetic, because every one of them is
exactly `24 + width * height * (depth / 8)` bytes long.

The colour reading was the part worth checking rather than assuming. The
darkest of the five uses **eleven distinct byte values across 30,720 pixels**,
which looks like palette indices — except the values are `0x00, 0x20, 0x24,
0x44, 0x48, 0x68, 0x8c, 0xb0, 0xb1, 0xd5, 0xf9`, scattered over the whole byte
range instead of counting from zero, and there is no room in the file for a
palette. Read as RGB332 they are one clean ramp: red a little above green with
blue at zero throughout, which is a gold-on-black gradient. Decoded, the image
is a gold dragon, and the file beside it is green foliage. A palette-indexed
reading has to explain the scattered indices and cannot.

The sixth header word is zero in all five and nothing read it. If the format
had a colour key, no local file exercised it, and guessing would have put holes
in an image on no evidence.

**Where it is wired matters more than where it was found.** The format turned
up in a KTF archive, not in the vendor whose name it carries; the sweep that
found it read every local archive and found LBMP only there, hundreds of BMPs
across KTF and one in LGT. So the decoder sits in `internal/wipic` beside
`DecodeBitmap` and all three platforms route to it — the two with real callers
because they had them, and the third because an image its neighbours can read
should not be what ends a title.

### The other half of the format, from the vendor that owns the name

That third platform then got the callers. The SKT archives carry LBMP
everywhere — about nine hundred files across the corpus, against the five that
first described it — and two more halves of the format come out of that many:

- **The sixth word is a mask flag, and a 1 means a one-bit transparency mask
  follows the pixels.** Its length is `width * ceil(height / 8)` in every file
  that has one, and that is the arithmetic that identified it: the payload of a
  masked file is exactly the pixels plus that. This is what the five KTF files
  could not have shown, because all five have the flag clear.
- **A set bit is the transparent one**, which the files say twice over. Every
  pixel a set bit covers in one title's sprites is the same magenta, and no
  artist paints a solid region in one colour; read the other way, a sprite is
  that magenta with its artwork punched out of it, which is exactly what three
  sibling titles drew before the polarity was settled.
- **A depth below 8 is stored as bit planes**, and each plane is the LCD's own
  page layout: byte `(y / 8) * width + x`, one row of pixels per bit, the top
  row of the band in bit 0. `size` is then one plane rather than the whole
  payload, which is what says the plane is the unit — a two-bit image is two of
  them and its mask is a third. Read any other way the same file is noise; read
  this way it is a company logo.

**The planar ramp runs the other way from the packed depths: zero is white and
the widest value is black.** One title settles it by shipping the same glyph
sheets twice — a white font and a black font, the same shapes with the same
mask, differing only in the value under it, zero in the white one and three in
the black one — and by shipping one sheet of that white font at eight bits a
pixel instead of two, where the pixels are 0xfe and 0xff. Read as light rather
than as ink, that title's whole script is black text on a black screen, which
is what it was.

What the levels *between* mean is still unsettled: every planar file here uses
the two ends of its range and nothing between, so the ramp is the even one and
the plane order is undetermined; a file with a middle level would be the
evidence that says otherwise.

**Neither half of this is cosmetic.** A sprite drawn without its mask is a
rectangle of magenta, which is how the mask was noticed at all; a planar sheet
read as light instead of ink is invisible, which is how the ramp was. Three
sibling SKT titles drew their world in magenta blocks and a fourth drew its
whole opening narration in black on black. `docs/skvm.md` has that pass.

## The graphics context's clip is how a tile gets drawn

Every `MC_grp*` call that draws takes an `MC_GrpContext *`, and the context
carries a clipping rectangle. Ignoring it looks harmless — a game that draws a
sprite at a position gets its sprite either way — right up to the title that
draws its map like this:

```
MC_grpSetContext(gc, MC_GRP_CONTEXT_CLIP_IDX, (int[]){83, 224, 99, 240});
MC_grpDrawImage(screen, 83, 176, 320, 128, sheet, 0, 0, gc);
```

The blit is the **whole 320x128 tile sheet**, positioned so that the cell it
wants lands under a 16x16 clip. The clip is the tile selector. Every one of
that map's cells is drawn that way, several dozen a frame, so a runtime that
does not clip paints the entire sheet over the scene once per cell — which
came out as a field of hay bales and sunflowers scattered over black, and was
read for a while as a missing background layer.

Two things had to be right for that to work:

- **The clip arrives as four `M_Int32`, and the record holds four 16-bit
  fields.** `MC_grpSetContext` was copying the caller's array into the record
  byte for byte, which reads the first two ints as all four fields: the
  rectangle's own top-left became `(83, 0)`, its bottom-right became
  `(224, 0)`, and the game's real bottom-right was never read at all. The
  specification calls the value an integer array for both this field and the
  origin offset, so both are widened and narrowed rather than copied.
- **An empty clip means "no clip".** `MC_grpInitContext` zeroes the record and
  nothing there knows which framebuffer the context will be used against, so a
  context that has never been given a clip holds an empty rectangle. Reading
  that as "draw nothing" would stop every title that never sets one; reading it
  as "no clip" leaves them exactly as they were, which is what the local set
  confirmed.

The clip now bounds `MC_grpDrawImage`, `MC_grpCopyFrameBuffer`,
`MC_grpCopyArea`, `MC_grpFillRect`, `MC_grpDrawRect`, `MC_grpDrawLine`,
`MC_grpPutPixel` and both `MC_grpDrawString` forms — every call that takes a
context. The Java-side draw path has a clip of its own and is untouched.

What was left in that scene was smaller and different, and it was not about the
clip at all: see the next section.

## A font handle is an identifier, and it was being read as a height

A title's menu came back with the top row shaved off every Korean syllable —
`이어하기` with the tops of its `ㅇ` open, `예` reading as `네`, `아니오` as
`바니노` — but only inside its menu bars and its confirmation boxes. The same
title's save-slot screen, which draws its text into open space, was correct.
That split is the whole diagnosis: **the text was not drawn wrong, it was
drawn taller than the box the title clipped it to.**

The title asks for its font once:

```
MC_grpGetFont(face 0, size 8, style 0)   ->  handle
MC_grpGetFontHeight(handle)              ->  8
```

and then lays every menu entry out as a band that tall, clipping each one
before it draws: clip `(70,155)-(171,163)` with the baseline at 161, six rows
above the baseline and two below. The handset face draws a Korean syllable
nine rows tall, from `baseline-7` to `baseline+1` — row 154, one above the
clip — so exactly one row was cut, and one row is the entire top arc of a `ㅇ`.

`MC_grpGetFontHeight` was answering with the handle it was handed. That reads
as reasonable until the specification's constants are looked up:
`MC_GRP_FT_SIZE_SMALL` is **8**, `MC_GRP_FT_SIZE_MEDIUM` is **0** and
`MC_GRP_FT_SIZE_LARGE` is **16**. They are identifiers for the three sizes a
handset offers, not measurements, and 8 only looked like a pixel count because
of which identifier it is. A title that asked for the small font was told its
line was eight pixels tall and believed it.

So the three metric slots answer for the face the renderer actually draws with
— height 11, ascent 8, descent 3 — whatever handle they are given, which is
what the LGT side already does and for the reason written down there: a height
that disagrees with the glyphs is a layout that disagrees with the text inside
it. `MC_grpGetFont` still hands the requested size back as the handle, because
a handle is opaque and two different requests should stay two different
handles; nothing measures it any more.

The title's own arithmetic then does the rest. The same menu, after: clip
`(70,154)-(171,165)`, eleven rows, baseline 163 — its formula is
`baseline = top + height - 2` and it simply had the wrong height. The glyphs
land inside with two rows to spare above.

**One face means one set of metrics, and that is the real constraint.** There
is no smaller face to answer SMALL with, so answering SMALL with smaller
numbers can only ever describe a font that will not be drawn.
`TestTheFaceFitsTheMetricsItReports` pins the invariant that matters — every
glyph fits between the reported ascent and descent — so a later change to
either the face or the numbers cannot quietly reintroduce a gap for a clip to
find.

What else moved: a title whose opening notice is three lines of Korean in a
framed box had been drawing them 8 rows apart into an 11-row face, so the
lines touched; they are now spaced and the box is drawn to fit. Across the
whole local KTF set the deterministic first-frame sweep changes those two
titles and nothing outside the family that differs against itself.

The other two platforms were checked before this was changed. LGT already
answers its three font slots off the face. SKT keeps a small table of heights
per MIDP size — SMALL 8/7, MEDIUM 10/8, LARGE 16/14 — that does not describe
the face either, but every local SKT title asks only for MEDIUM, whose box the
glyphs fit inside; the mismatch there is latent and has no caller, so it is a
watch item rather than a fix made blind.

## An image is drawn through its own handle, not the framebuffer inside it

The same map drew a few of its objects — a bush, a fence, a hanging vine —
inside black rectangles. That read for a while as a *colour key* question,
because `MC_grpDrawImage` is documented to draw "with the transparency and
transparent colour the context specifies" and the context's transparent-pixel
field is stored here without any blit reading it. It was not that, and the
probe that settled it is worth keeping: sample one pixel inside a black box on
every LCD flush, and report the last draw that covered it. The answer came back
as a 39x34 blit whose source pixel was black **and marked opaque**, from a
handle that had been created by `MC_grpCreateImage`.

`MC_grpCreateImage` builds two records: a framebuffer record for the decoded
pixels, and the `MC_GrpImage` that inlines that framebuffer's fields at its
front and is what the guest is handed. The transparency the encoding declared
was recorded against the inner framebuffer handle — and no caller ever holds
that one. `MC_grpGetImageFrameBuffer` answers the image handle itself, exactly
because the record starts with those fields, so **every C-side draw arrives
with the image handle**. The lookup missed, a miss means "fully opaque", and so
every `MC_grpDrawImage` in the archive ran with no transparency whatever.

What hid it for so long is that most sprites never take this path: the Java
`Graphics.drawImage` route keeps its own framebuffer handle in the image object
and always found its mask, so characters, portraits and menus were all correct.
Only the objects a title draws through the C API were boxed. The transparency
is now recorded against both handles, and the frame a route captures on that
map goes from 45,954 lit pixels to 52,405 — the scenery that was behind the
boxes.

A/B'd across every local KTF archive, one other title changes: the black
rectangle behind its title logo is gone. Three more appear to change and do
not, which is worth knowing before reading such a run: a title screen animates,
`-play` follows the wall clock, and two runs of the *same* binary differ by
four to five thousand pixels on those three. Establish that noise floor before
calling a difference a regression.

## The transparency a title brings with it: the context's pixel operation

A graphics context can carry a function, and a title that sets one is telling
the platform how every pixel it draws is to be combined with what is already
there:

```c
M_Int32 op(M_Int32 srcpxl, M_Int32 orgpxl, M_Int32 param1)
```

Two local titles set one, and one of them **does all of its transparency this
way**. Its art is 8-bit BMPs that declare none — `bfReserved1` is zero on every
one a run decodes, unlike the title in "A BMP declares its transparent colour in
the header" — and every sprite sheet it loads holds its subject on a flat green
backdrop that is palette entry zero. Its operation is four instructions long:

```c
if (srcpxl == param1) return orgpxl;   /* the backdrop: keep the screen */
return srcpxl;
```

Ignoring it painted the backdrop, and what made that hard to see is that the
title's own field is green: the sprites looked perfect over grass and the fault
only showed where the background was not, as a green square in each corner of
every dialogue box on the title screen. The second title's operation keys white
instead and answers a colour it keeps in a global — so neither the colour nor
the rule belongs in the platform. Only the call does.

**The specification's prose for the two pixel parameters is the wrong way
round.** It describes the first argument as the pixel already in the
framebuffer, but the prototype's names say source first, and the guest code
settles it: both local operations return their *first* argument in the ordinary
case, which for a blit that draws anything at all has to be the source. The
alpha example the specification gives agrees when read that way and contradicts
itself when read the other.

**The operation and its parameter are the last two words of the record, not the
words the specification's field order gives them.** That is settled by the title
that writes its context *directly* rather than through `MC_grpSetContext`: the
function pointer it also passes to the API appears at `+0x2c`, with the green
its operation keys on at `+0x30`, and that green appears nowhere else in the
record — least of all at the offset the documented `MC_GRP_CONTEXT_PIXEL_PARAM1_IDX`
would have written. The title never calls that index at all. So the two pairs
trade places with the font and the style, which no local title reads back.

Every drawing call that takes a context runs its pixels through the operation:
the two blits, the fills and the outline, the line, the pixel and both string
forms. Two things make that affordable and safe:

- **The answer is cached.** The operation is a pure function of its three
  arguments, and a blit would otherwise ask the guest once per pixel; keyed by
  the pair of pixels, one title's dialogue-heavy run makes 7,600 guest calls
  against 24,000 `MC_grpDrawImage` calls covering millions of pixels. The cache
  is thrown away when the function or the parameter changes.
- **A field this platform did not write is not trusted to be a function.** A
  title that sets a font through what this handset uses for the operation leaves
  a small integer there, and calling it would execute an address that is not
  code. Only a Thumb address inside the loaded module counts, which is what one
  local title's font handle of `8` fails.

## XOR mode is how one title family draws everything

`Graphics.setXORMode(boolean)` was recorded and then read by nothing: every
fill, line, outline and glyph wrote its colour straight over the destination
whatever the mode said. The specification is explicit — "화면의 내용과 현재
출력하는 내용을 XOR하여 출력" — and the WIPI C context carries the same mode as
`MC_GRP_CONTEXT_XOR_MODE_IDX`, so the missing half was the write itself:
`destination ^= colour`, which is what the LGT runtime's `put` had done all
along.

What it cost is larger than a mode flag suggests, because one title family
draws **every sprite** through it:

```
setClip(x, y, w, h)
setGrayScale(0); setXORMode(true); fillRect(x, y, w, h); setXORMode(false)
drawImage(sprite, x, y, 0)
setGrayScale(0); setXORMode(true); fillRect(x, y, w, h); setXORMode(false)
```

The two fills bracket the draw and the colour is **black**, so as XOR they are
a pair of no-ops — the title is tinting through a colour it happens to leave at
zero, and the sprite is meant to arrive untouched. Painted as solid fills they
are a black rectangle drawn over the sprite twice, which is exactly what the
screen showed: a title screen of black boxes where its logo and its icons
belong, a character behind a black square, and a menu whose text was invisible
except on the one row the cursor had already darkened. Three local titles came
back with the change; a fourth, whose menu rows draw white text and then invert
the row, is where the invisible-except-at-the-cursor shape came from.

Two boundaries are deliberate. **A partly covered glyph pixel XORs its
source and then blends by coverage** — full coverage is `destination ^ colour`
and no coverage is the destination, so the antialiasing the text path already
did stays continuous. And **an image blit is not XORed**: the specification's
prose for the Java call names `drawLine` and `drawPolygon`, the LGT runtime
routes only its colour operations through `put`, and no local title draws an
image with the mode on — so the case is left as it is rather than guessed at.

Only titles that call `setXORMode` can see the change at all: across the local
KTF set, one title reaches it inside its first four hundred ticks and the rest
never call it.

**The black terrain that came back with it is the game's own art.** After the
fix that title plays through its stages with a patterned background and solid
black ground, which reads like a layer that failed to draw and was carried as an
open question for a while. Its own resources answer it without anyone having to
play it: the archive holds thirty sprite banks, and **every one of them except
the backgrounds is two or three colours** — characters, obstacles, items, the
HUD, all a single silhouette colour over transparency. The backgrounds are the
one exception, twelve banks of four 20x20 tiles at five to sixteen colours each,
one bank per stage. The first bank is the cyan cloud pattern the first stage
draws, tile for tile, which is what the screen shows behind the black. A
silhouette is what this game is drawn in, so black ground under a coloured
pattern is the art rather than a missing layer. **An asset survey answers "is
this what the game looks like" faster than a person can**, whenever the art is
regular enough for a colour count to mean something.

What the survey could not answer is where the ground went, which is the next
section: the art was right and most of it was never on the screen.

## `copyArea` is not the MIDP call it looks like

The same title's ground came back as two stubs — the first two tile columns and
the last one — with the patterned background running unbroken between them, and
it was reported as terrain that does not draw. Nothing was wrong with the
terrain. The title builds its scene in an offscreen surface and moves the whole
surface left ten pixels a frame, then draws one fresh 20-pixel column of tiles
down the right-hand edge; the shift is `Graphics.copyArea`, and it was copying
nothing at all. **The background hid that completely, because a repeating
pattern looks the same whether or not it moved.** Only the ground, which does
not repeat, could show that the surface had been standing still since the stage
began: what the screen held was the columns the very first frames drew, plus
the one column being drawn now.

The call was read in MIDP's order. `javax.microedition.lcdui.Graphics` names
the source first and ends with the destination and an anchor —
`copyArea(x_src, y_src, width, height, x_dest, y_dest, anchor)`, seven
arguments — but this platform's own class is a different one, and the
specification gives it as

```java
public void copyArea(int dx, int dy, int sx, int sy, int w, int h)
```

destination, source, size, and no anchor. Read in MIDP's order the title's
whole-surface shift of `(-10, 0, 0, 0, 240, 320)` becomes a copy of a
zero-sized rectangle, which is a call that does nothing and reports nothing.
**A six-argument method with a seven-argument twin in a neighbouring API is
worth looking up rather than recognising**; the argument shapes are close
enough that the wrong one type-checks and runs for years.

Two more things had to be right for the shift to land:

- **The clip moves the source with the destination.** The destination starts
  ten pixels off the left edge, so clipping it to the surface moves its corner
  to zero, and the source corner has to move by the same ten pixels. Holding
  the source still copies the surface onto itself unchanged — the same standing
  still, arrived at a second way.
- **A copy inside one surface reads what was there before the copy began.** The
  blit walks its rectangle in order and reads each source pixel from the
  surface it is writing into, which smears the leading edge across the whole
  rectangle whenever the copy moves right or down. An overlapping self-copy now
  takes a snapshot of its source first, which is the "as if through a temporary
  buffer" such a copy is specified to be, and the LGT side had done all along.
  A copy between two surfaces, or one whose rectangles do not meet, still reads
  memory directly and allocates nothing.

Two local KTF titles name `copyArea` at all — the scrolling one and one that
moves a dialogue box's text with it — which is why a call this wrong survived
this long, and why the fix was checked by playing both.

## A null surface is a state, not a fault

One title ends the session when the player leaves the world for the main menu:
`framebuffer handle address 0x0 is null or unaligned`, raised from the game's
own frame timer. The handle it was drawing *from* is zero. Nothing about the
archive is missing — every image the scene used loaded, and 4.5 million
`MC_grpDrawImage` calls in a run of ordinary play never pass a null.

What is passing one is a teardown window. Confirming the exit releases the
images the scene owned and starts building the menu, and the game's frame timer
keeps firing across that gap: for a few hundred frames it paints a sprite list
in which one slot is now null, and then the menu replaces it. The whole state
lasts as long as the transition — 320 draws in the run that reproduced it, none
before, none after.

**This API is C and the handle is a pointer.** A null one is the game saying
there is nothing there, which is a thing a caller is allowed to say; the
specification's own prose for these calls lists several ways there can be
nothing to copy — a non-positive width or height, a source rectangle outside
the source, a depth that does not match — and every one of them draws nothing
rather than failing. A null surface is read the same way: `MC_grpDrawImage`,
`MC_grpCopyFrameBuffer`, `MC_grpCopyArea`, the fills, the outlines, the line,
the pixel, both string forms, the RGB transfers and `MC_grpGetImageProperty`
all return having done nothing, and the debug build counts it by call name so
a report still shows it happened.

**A handle that is not null and not a frame buffer still fails.** That one is a
bug — a stale pointer, a mis-decoded record — and the whole value of the null
case is that it stops hiding among them.

## A rounded rectangle is a curve, and one title's shadow is an ellipse

`fillArc`, `drawArc`, `fillRoundRect` and `drawRoundRect` all drew the
bounding rectangle, on the reasoning that an approximate outline shows the
frame a game draws and nothing at all does not. What that costs is a shape, and
one title spends it seventeen thousand times a run: **every character's ground
shadow is a `fillRoundRect` whose corner diameters are its own width and
height**, which is an ellipse, and it came out as a hard 24x8 rectangle sitting
under the character. It reads as a rendering fault rather than as an
approximation, which is how it was reported.

Finding it needed the pixel probe rather than the frame: the shadow is a dark
grey a shade off the grass, so the frame says "something is wrong here" and
nothing more. Sampling one pixel inside the box on every LCD flush and printing
the last draw that covered it named the call, its size and its colour in one
line — the same probe that settled the black boxes in "An image is drawn
through its own handle". One thing to watch when doing that: a probe on the
shared fill and a probe on the caller both write the same slot, and the shared
one wins. The first reading said `fillRect` and the counts said the title never
calls `fillRect` seventeen thousand times, which is what sent it back.

The geometry is MIDP's and the specification restates it: `arcWidth` and
`arcHeight` are the **diameters** of the corner arcs, and an arc's angles are
degrees counter-clockwise from three o'clock. Three things are worth writing
down:

- **A row's inset is measured from its own centre.** Taking the distance as
  whole rows makes the topmost and bottommost rows of an ellipse exactly zero
  pixels wide, which flattens the shape and loses the row a real ellipse has
  there.
- **A diameter larger than the side it rounds is clamped**, which is what makes
  `fillRoundRect(x, y, w, h, w, h)` an ellipse instead of an overflow.
- **`drawArc` is the curve, not the pie.** MIDP does not draw the two straight
  edges to the centre, so the outline walks the angle rather than the bounding
  box, one step per pixel of the longer radius.

Both fills go through the same clipped row fill every other operation uses, so
the clip, the translation and XOR mode apply to them without a second path.

### The same geometry, on five surfaces

That fix was written against this platform's Java `Graphics` and stayed there,
which turned out to be four fifths of the problem. Five drawing surfaces need
these shapes and each had been answered separately:

| surface | arcs | rounded rectangles |
|---|---|---|
| this platform's Java `Graphics` | drawn as curves | drawn as curves |
| its WIPI-C graphics table, 15 and 16 | **unimplemented — fatal** | no such call |
| the other ARM platform's WIPI-C, `0xd8`/`0xd9` | **unimplemented — fatal** | no such call |
| its Java `Graphics` | **not implemented at all** | **drawn as a rectangle** |
| the MIDP platform's `Graphics` | **not declared at all** | **not declared at all** |

Three of those fail worse than the wrong shape does. An unimplemented WIPI-C
function ends the session, so a title drawing a curve did not draw it badly —
it stopped. A MIDP method that is never declared fails to resolve, which ends
the title the same way. Only one row of the table was the "approximate outline
beats nothing" trade the original note describes.

The geometry now lives once in `internal/curve`, which walks a shape and hands
out horizontal spans without drawing anything. Every surface binds those spans
to the clipped fill it already had, so each keeps its own clip, colour, blend
and transparency rules and none of them reimplements an ellipse. The
specifications agree on the contract, which is what makes one implementation
legitimate rather than merely convenient: WIPI's `MC_grpDrawArc` and MIDP's
`Graphics.drawArc` both put zero degrees at three o'clock, count positive
angles counter-clockwise, take the second angle as an extent rather than an
end, and centre the arc in the bounding rectangle.

Two things settled the slot numbers without a disassembly. This platform's
table has `copyArea` at 14 and `drawString` at 17, and the specification puts
exactly `MC_grpDrawArc` and `MC_grpFillArc` between them — a gap of two for two
functions. The other ARM platform's block is the same argument at `0xd7` and
`0xda`. See `lgt.md`, "The two arc slots come from their neighbours".

## Screen flush

The guest flushes the LCD far more often than any display refreshes — a menu
can manage a few hundred a second — and converting its RGB565 screen into the
Host's RGBA frame is a read and a rewrite of every pixel. Doing that per flush
spent most of it on frames no Host would ever ask for.

So `presentScreen` only records that a flush happened, and `Client.Frame` does
the conversion once for the Host that actually takes a frame. A run of flushes
with no collection between them now costs one conversion instead of one each,
and `Flushes` still counts every flush so a Host can tell whether anything is
new before paying for the copy.

The tradeoff is which instant the frame shows: it is the screen buffer as it
stands when the Host collects, not as it stood at the flush. A Host collects
between ticks, with no guest running, so that is the buffer the last flush
left — unless the tick ended part-way through the next paint, in which case the
frame is that far into it. For a display collecting once a frame this is what a
panel would show anyway; the frame probes in `runktf` see it as catching a
marginally different moment in a fade.

## Recognized C runtime routines

These games compile their C runtime into the image and lean on it hard, and
interpreting a byte-at-a-time fill is the worst thing this emulator can be
asked to do — every byte is several instructions, each with a fetch and a
decode — while the Host does the same fill in one copy. One title spends
most of its in-battle guest execution inside its own memset, which is what made
a special attack lurch.

`binary_hooks.go` recognizes those routines in the loaded image by code shape
and replaces each entry with a supervisor-call stub, so anything that reaches
one — a direct call, a call through a function pointer — lands on a native
implementation. Currently memcpy, memset, and strlen, each matched both as the
RVCT library helper reached through a `BX PC` thunk and, for memset, as the
same function compiled directly into the image where the thunk pattern never
sees it. Matching is codegen-keyed rather than game-keyed: every local archive
carries these, and a pattern that matches nothing costs one scan at load.

The scan runs against the image as it sits in guest memory, after the entry's
self-relocation, so the addresses hooked are the ones that will execute. A stub
is wider than some patterns, so a match overlapping one already installed is
refused rather than allowed to land inside it. On the routine itself the
hooked memset is 24x faster in one title and 41x in another; the
instructions after a stub become unreachable rather than wrong, since the stub
returns before them.

Currently memcpy, memset, strlen, and strcpy. Argument order is the hazard here
— a memset with its value and length swapped would quietly fill the wrong bytes
rather than fail — so `binary_hooks_test.go` pins each C contract, including
that only the low byte of memset's value is written, that strcpy copies its
terminator, and that each returns what the original returns.

**What is worth hooking is a measured question, and the answer is narrower than
it looks.** One supervisor-call round trip costs 131.5ns against 8.7ns for an
interpreted instruction (Apple M1, native): a trip to the Host and back is
worth about fifteen interpreted instructions, and the stub's own six come out
of that too. So a routine only pays if it runs well over twenty guest
instructions per call — which means routines that loop over a length, and only
those.

That rules out the three hottest routines the guest profiler found in the
profiled title,
which together hold roughly half its guest execution: the array-load helper at
`0x1689e4` (~15 instructions), the object-header-to-vtable resolver at
`0x167e84` (~9), and a static byte load at `0x167020`. Every one of them is
O(1), so hooking them would trade a dozen interpreted instructions for a round
trip worth fifteen and lose. Their patterns generalize cleanly across titles —
they match four of seven local images byte for byte — which makes them
tempting; the cost model is why they are not hooked. What that half of the
profile is really asking for is interpreter throughput, not a hook.

The same model answers what to hook next. The routines left are the inline
copies a compiler emits instead of calling `memcpy` — a fixed-length one and a
register-length one — and recognizing them needs a pattern language this does
not have: wildcards, a register field captured in one instruction and required
to match in another, and a digest to pin a whole match. Across the seven local
images the register-length form occurs **once**, so the language would be built
for a single site.

**Re-measured from the other end, and the answer did not move.** The site count
asks how many places a pattern would match; what decides whether to build it is
how much of a run those places hold, and a profile bounds that without a
pattern language existing. The scene profiled is the deepest one any local
title has been driven to — a map, with sprites, tiles and text redrawing every
frame, 790,695 samples over 7.9e8 instructions:

| | share | span |
|---|---|---|
| five hottest regions | 55.8% | 74-410 bytes each |
| every region 64 bytes or under, added up | 12.4% | eight regions |
| the hottest of those | 4.6% | 34 bytes |

An inline copy is short, so a short hot region is where one would be. **The
span does not say how long a routine runs**, and that matters here in the
direction that favours hooking: thirty-four bytes sampled that often could be
one pass or a loop going round a thousand times, and a loop is exactly what the
cost model says to hook. What the table does settle is the ceiling. Every short
region in the profile added together is 12.4% of the run, the largest is 4.6%,
and a hook can only take back part of what the routine costs — so the whole
pattern language, matching perfectly at every site, is worth a few percent of
one scene. The other 55.8% is in the game's own hundred-line drawing and logic
routines, which no pattern will ever match. That is the same answer the AOT
title gave in different words: what is left wants interpreter throughput.

Two things would change this. A profile in which one short region holds double
figures on its own is worth disassembling before anything else — if it is a
copy loop, it is the site the language would be built for. And the ceiling is
per scene: a load screen, where a title decompresses a thousand resources, is
the place a copy loop would dominate if one ever does.

## The load screen was profiled, and it says no as well

Every local archive was profiled over its first two hundred ticks — the entry
through to somewhere in the title screen, which is the load. **The first half
of the prediction holds, emphatically.** One region dominates almost every
boot, most of them short, and the ones that were disassembled are exactly what
was predicted:

| share | span | what the bytes are |
|---|---|---|
| 97.3% | 74 | a 16-bit rectangle fill: `strh` down a row, then the row pointer by a stride |
| 70.7% | 16 | a fill of a range with a two-byte pattern read from a fixed source |
| 64.7% | 16 | `ldrb`/`strb` down a counted length — an inline byte memcpy |
| 50.1% | 74 | an 8bpp source through a palette to 16-bit destination pixels |
| 44.5% | 66 | the same, unpacking sub-byte indices and skipping a colour key |
| 37.9% | 198 | unpacking two RGB565 pixels, mixing them, repacking — an alpha blend |

**The second half does not.** A share is not a cost, and the thing to measure
is host CPU against the guest time it buys. A wall-clock run cannot answer
that, because a title that sleeps through its splash screen takes eight seconds
to run two hundred ticks with the emulator barely working — which is what the
first measurement here looked like, and it was measuring the game's pacing. On
a manual clock skipped to each deadline, the host CPU for those same two
hundred ticks is this, worst first:

| host | guest delivered | archives |
|---|---|---|
| 6.46s | 4.03s | one |
| 1.82s | 2.99s | one |
| under 2.1s | 2.0s or more | the other eighteen |

**One archive out of twenty is slower than the guest clock it is feeding**, and
one more is close. Everything else finishes its load in a fraction of the time
the game expects to spend on it, so a hook there would buy a user nothing at
all. For the one that is slow, the load is over by tick 400: 11.1s of host for
8.0s of guest, and by tick 800 it has caught up. The stall a hook could
address is about three seconds, once, and the region holding 37.9% of it is
the alpha blend in the last row of the table — shifts and masks over two
RGB565 pixels, spilling through the stack, which no pattern written for a copy
loop would ever match.

So the pattern language stays unbuilt, and now for a second reason. The
gameplay profile said its ceiling was a few percent spread over eight regions.
The load profile says the copy loops are real and dominant but sit in the
nineteen archives that were never waiting on them, while the one archive that
is waiting is waiting on arithmetic. Both scenes point the same way: what is
left wants interpreter throughput.

Two things would still change it. A local archive whose load runs well under
real time **and** spends it in a short copy loop is the case that has not
appeared yet — the two properties keep landing on different archives. And a
handset-speed target changes the arithmetic entirely: at a tenth of this
machine's throughput every one of these loads is slower than its guest clock,
and then the copy loops are worth what the table says they are.

Note for whoever builds it that the stub needs a shape the hook table does not
have. Every routine hooked so far is an entry reached by a call, and its stub
returns through `BX LR`. None of the loops above is a routine — they are inline
loops inside a larger function, entered by falling in and left by a conditional
branch, so a stub that returned would return to whatever called the enclosing
function. A stub over an inline loop has to run the supervisor call and then
branch to the loop's own exit, which means the match has to identify that exit
and not only the loop.

## Pacing

A game sets its own speed. Its frame loop asks for a wait — `Thread.sleep`, a
`MC_knlSetTimer` delay, an idle `getNextEvent` — and the length of that wait is
the only thing that decides how fast the game runs. The Host's job is not to
pick a rate but to honour those waits, so each one resolves to a deadline on
the session clock (`SessionOptions.Clock`, `internal/platform/ktf/clock.go`)
and the service loop refuses to run the work behind it until the deadline
passes:

- a sleeping worker keeps its place in the queue but is not granted a slice
  until its `wakeAt`;
- a timer stays pending until its `due`;
- a wait declared on the client thread has nothing to park — the Host goroutine
  is inside that call — so it is deferred instead: `ServicePaint` and
  `ServiceTimers` hold off until it elapses. This is not an edge case.
  A title can run its whole frame loop inside the card's `paint` and pace it
  with a `Thread.sleep` there, so paint is exactly where its speed comes from.

`Session.NextDeadline` reports when the earliest of those falls due, so a Host
idles for exactly as long as the game asked instead of polling.

### Runnable now, or due on the next idle pass

Everything `NextDeadline` leaves out, a Host sleeps through — so what belongs in
it is not only the parked work but anything that is *ready*. Two things are
ready in different senses and conflating them broke a title in each direction.

A thread the guest started with `Thread.start` is runnable **now**; the next
round is what adopts it as a worker. Leaving it out reports the game as idle
until whatever else is parked comes due. One title starts a thread from its
paint and has one other thread asleep for a hundred seconds: the Host was told
to sleep for the hundred seconds and did.

A Runnable handed to `Display.callSerially` is due on the **next idle pass**,
which is `serialDispatchInterval` — one frame at sixty hertz — after the last
one, and exactly one comes off the queue per pass. This is not a rate limit
bolted on; it is what the original event loop does, and games depend on it. One
title's entire frame loop is a Runnable that re-queues itself, reads the clock,
and returns without drawing until its own interval is up. Dispatched as fast as
the Host turns rounds, it re-queues as fast, so the guest is never idle, the
Host never sleeps, and a core burns to produce the same twenty frames a second
— measured at 1.1 million rounds a minute against 1,675 once paced.

### A wait that has elapsed is not a deadline

The other direction is the same mistake made about a wait rather than about
work: the client thread's wait is recorded and nothing clears it, because
nothing had to — `clientThreadDue` only compares it against the clock. But
`NextDeadline` was reporting it whatever the clock said, so from the first
`Thread.sleep` a title ever declares on that thread, the session answers "work
is due, in the past" for the rest of the run.

Nothing that *waits* was hurt by that: a deadline already past means "do not
sleep", which is what a Host with a runnable client thread should do anyway.
What it broke is the Host that **skips**. A manual clock advances only by
jumping to the next deadline, and there is no jumping to one in the past, so
the clock stopped at the instant that wait elapsed and never moved again. Every
later deadline was then unreachable: a title whose whole first scene is a
`java.util.Timer` task scheduled a second out sat on the frame before it,
repainting, for as many rounds as anyone cared to run. The same run under
`-play` reached the scene, because there the wall clock moves on its own —
which is the shape of a bug that only a stepped Host can see, and the reason
the CLI's `-play` had been the only way to get past it.

An elapsed client wait is now left out of `NextDeadline` entirely, which puts
it exactly where a title that never declared one has always been: no deadline,
so a Host ticks rather than sleeps, and whatever else is parked becomes the
next deadline instead of being hidden behind a stale one.

### What frame skipping cannot pay for

Dropping a paint (`session.md`) trades the picture for the logic in the same
round. A round with no logic in it has nothing to trade, and skipping there is
not free: for the title whose frame loop runs from its card's paint, the paint
*is* the round's work, and the client thread sets its own next wake-up inside
it. Skipping left that thread perpetually due — which reads to the Host as work
waiting, so it spun, dropping the paint on every one of those rounds too. So a
round that serviced no timer and no thread paints regardless of how far behind
the Host is. The saturation case frame skipping was built and measured for is
unaffected: there, timers and threads are exactly what is running.

Two kinds of Host want different answers to "what time is it", which is why the
clock is injected rather than read:

- an interactive Host (the page, `runktf -play`) leaves `Clock` nil and gets
  the wall clock, so the game runs at the speed it was written for;
- a batch Host (the frame probes, `runktf` without `-play`, tests) passes a
  `ManualClock` and calls `Session.SkipToNextDeadline` whenever a tick found
  nothing due. It then runs the same sequence of guest work at no real cost,
  which is what keeps a 64-round frame probe instant.

### A title whose speed was the handset's to decide

One title is reported as running too fast, and the measurement says the
emulator is doing exactly what it was asked. Its logic thread is a loop that
draws a frame and calls `Thread.sleep(20)` — a constant, not a deadline
computed from the clock, and the title never reads the clock at all. So it
takes one logic step per 20ms of wait plus however long the machine needs for
the step, and its speed is whatever is left over: 49.6 steps a second here,
against a handset where the frame's own work was a real share of the budget.
**Nothing in the archive says what that share was**, and inventing a per-frame
cost to slow it with would be picking a rate rather than honouring one, which
is the opposite of how everything else here is paced.

What answers it is the multiplier, and the page was the part that fell short:
the browser's speed menu stopped at 0.5x while the core has always accepted
0.1x. It now offers 0.25x and 0.75x as well, which brackets this title's
plausible original rate between twelve and thirty-seven steps a second.

**Measuring a speed change from a route needs care.** A route's `wait` counts
ticks, not time, and an interactive tick waits at most `idlePollCeiling` (50ms)
before turning another one — so a fixed-tick run at 0.25x ends after about the
same wall time as one at 1x and looks as though the setting did nothing. The
number that moves is the guest's own work per second: 1,040 sleeps in 21s at
1x against 575 in 42s at 0.25x, which is the factor of four, and the paint
count is identical in both because paints are the Host's rounds rather than the
guest's steps.

### A second title, the same complaint, and a different place the time went

The title above is one shape of this. A second one is reported the same way and
is not the same shape at all, which is worth writing down because the obvious
fix for one does nothing for the other.

It never calls `Thread.sleep`. It builds a fresh one-shot WIPI timer every
frame — `MC_knlDefTimer`, `MC_knlSetTimer`, `MC_knlUnsetTimer`, 5,758 of each
over a route — and **every one of the 5,758 asks for 10 milliseconds**, never
anything else. Its card paints exactly once per Host round. So its contract is
"wake me 10ms after this frame ends", and its period is its own frame plus that
10ms, which the Host honours exactly.

Measured on that route with `-play`:

| | this title | the heavier one beside it |
|---|---|---|
| the wait it asks for | 10ms | 10ms |
| guest instructions a frame | **173,760** | **2,221,000** |
| host CPU a frame | 6.2ms | 22.0ms |
| period, measured | **18.0ms** (55 a second) | **33.3ms** (30 a second) |

The Host is idle two thirds of the run — 35.70s of CPU against 103.83s of wall
— so this is wait-bound rather than throughput-bound, and the arithmetic closes:
6.2 + 10 is the 18ms observed.

**The two titles ask for the same 10ms and run at half each other's rate**, and
the reason is the first column. 173,760 instructions is not a frame of drawing;
it is a frame of *asking* for drawing. The diagnostic counts say what it asks
for: about 190 graphics-table calls a frame — 75 of function 13, 61 of function
6, and a tail below those — plus 233 `memcpy` hooks and 99 pixel operations.
**This title draws through the platform, and the platform is Go here.** The
handset ran those same calls as the vendor's C on a hundred-megahertz ARM9,
which is where its frame time went and where ours does not go.

That is the measurement that decides what a work clock would be worth. LGT has
one — guest instructions converted to guest milliseconds — and giving KTF the
same thing would charge this title 1.2ms a frame at LGT's rate, on a period of
18: **it would not touch the complaint.** What it would do is slow the title
beside it by 15ms a frame, which nobody complained about. A clock that charges
guest instructions cannot see work that is not guest instructions, and for this
title almost none of it is.

What would reach it is a cost model for the platform's own drawing — pixels
touched against a rate the handset never wrote down. That is picking a number
rather than honouring one, on a far larger surface than a single instruction
rate, and it is not built. **The multiplier is still the answer**, and it is
now stored per game (`docs/session.md`, "The speed belongs to the game, not to
the page") so that a title needing a quarter speed keeps it while the title
beside it keeps full speed.

`SessionOptions.Speed` (and `Session.SetSpeed`, live) scales the pace between
0.1x and 16x. It divides every wait and multiplies the guest's own clock by the
same factor, so a game that times its animation with `MC_knlCurrentTime` speeds
up rather than compensating for the change. The top end is bounded by the
emulator's throughput rather than the setting: at 4x a 45ms wait becomes 11ms,
but the frame's own guest execution still costs what it costs.

Guest time is one timeline. `MC_knlCurrentTime` and Java's
`System.currentTimeMillis` both read `guestMillis`, which tracks the session
clock scaled by the speed multiplier — the JVM reaches it through its `Clock`
option. Because it tracks a clock rather than accumulating declared waits, a
guest that polls the time without ever sleeping still sees it move, so a
busy-wait loop terminates.

**Where that timeline starts is the Host's clock, not the wall.** It used to be
`time.Now()` regardless, which quietly cost every probe its reproducibility: a
Host on a `ManualClock` is asking for a run it can repeat, and a title that
seeds itself from the time of day repeats only if the time of day does. One
local title's repro route stopped at 877 ticks, then 2,353, then 877 again, and
five of the corpus's first frames differed against themselves — that was the
whole of the corpus A/B noise floor. Both are gone: the epoch follows
`client.now()`, `NewManualClock` starts at a fixed date when the caller names
no instant, and the same script now lands on the same tick and the same pixels
every time. A Host on the wall clock — a person playing — still sees real
dates.

The native CLI runs a KTF archive headlessly:

```sh
go run ./cmd/cli runktf <game.zip> [-ticks N] [-frame out.png] \
  [-play] [-speed N] [-key tick:name] [-framedir dir] [-save dir] [-cheat]
```

`-speed` implies `-play`: a multiplier only means anything on the wall clock.

`-play` keeps ticking past the first lit frame, `-key` injects a WIPI key
press/release pair at a tick (`fire`, `up`, `soft1`, digits, `*`, `#`, …),
and `-framedir` dumps a PNG whenever the flush count changes — enough to
probe in-game screens from the command line.

Saves persist through the `ktf.SaveStore` boundary: WIPI C databases under
`db/<name>`, Java `DataBase` records serialized under `jdb/<name>`, and
guest `File` writes under `fs/<path>`. Opens, existence checks, and stat
calls consult persisted saves before packaged archive data, and every
mutation writes through. The CLI attaches a directory store rooted at
`var/savedata/<profile>/ktf/<owner>` (override with `runktf -save <dir>`); a
session without a store keeps saves in memory. The profile comes from the
binary's own build tag, so a debug build never moves a release build's saves.

The owner is the game's **PID**, not its AID (`ktf.SaveOwner`). Distinct games
ship the same AID — `010100D5` covers four unrelated local titles — and a save
shadows the packaged file of the same name, so an AID-keyed root would let one
game's save replace another game's shipped data. PIDs are per title, and the
variants of one title (the same game reissued with modified drop rates shares
`PD004517`) keep sharing a directory, which is the sharing that is wanted.

The browser shares the same on-disk save layout, because a session runs the
emulator on the machine that holds it: the guest reads and writes
`var/savedata/<profile>/ktf` directly through the same `DirectorySaveStore` the
native CLI uses, and no save crosses the network. The save API — `GET
/api/saves/<owner>` listing every persisted entry as base64 and `PUT
/api/saves/<owner>/<key>` writing one raw entry — remains for reading that tree
out of process (`WFEATURE_SAVE_ROOT` overrides its root outright). The service
worker never caches `/api/` responses.

Save keys normalize before they hit disk (`ktf.NormalizeSaveKey`, mirrored in
the server's `normalizeSaveKey`): empty and `.` components drop out, `..` is
rejected. Guests name databases the way they name files — one opens
`./OptionSave` — and rejecting that key silently lost every save it made.

The layout differs from the original emulator's, which scoped saves the other
way round — `<root>/db/<PID>/<key><record-id>` with the key percent-encoded,
and `<root>/fs/<AID>/<path>`. Four differences matter when moving data across,
which is why copying that tree in place leaves every save invisible:

- Nesting is owner-first here (`<PID>/fs/<path>`) rather than scope-first
  (`fs/<owner>/<path>`).
- Its database owner is the ADF PID (the repository is opened with
  `System::pid`), but its guest files are keyed by AID. Both scopes live under
  the PID here, so importing guest files means resolving an AID to a game.
- The record id is glued to the database name — `save0.dat` record 1 is the
  file `save0.dat1` — and a name containing a slash is percent-encoded into one
  flat file (`data%2FXlsItem.zt11`).
- Guest `File` writes translate one-to-one, but records do not: the original
  wrote one file per record id, while `jdb/<name>` here holds every record of a
  name in one length-prefixed container (`encodeSaveRecords`), and a WIPI C
  database is the bare bytes of its single record under `db/<name>`.

`wfeature importsaves <savedata dir> [-save dir] [-games dir] [-dry-run]`
performs that translation, reading every archive under `-games` to resolve the
owners. A database whose PID no archive claims is reported rather than guessed
at. Nothing in a source file says which API wrote a database, so a multi-record
one — only a Java `DataBase` can have those — is written to `jdb/` alone, and a
single-record one to both `db/` and `jdb/`; the scopes never collide, so
whichever the game opens finds its save. A guest-file owner whose AID several
games share is decided by which of them packages files of those names (the
`010100D5` files resolve to one of the four that way), and reported when that does not
settle it.

The browser reaches all of this over a session: `internal/webhost` starts the
archive on the server, pumps the startup that KTF runs in slices, and sends the
presented frames on. The phone keypad and keyboard route to KTF key events over
the same socket. See [`session.md`](session.md).

## Cheat engine

`internal/cheat` ports the original scanner/freeze engine: a progressive
value search (u8..i32, both endians, alignment control, undo history) over a
`MemoryTarget`, a freeze list reapplied after every tick, and a text command
console shared by every Host. The KTF target scans the committed writable
guest pages reported by `armcore.Memory.CommittedRegions` — mappings reserve
far more than games touch — labeled by area (`client`, `stack`, `platform`,
`stubs`). `Session.Tick` reapplies frozen values after the service round so
they win over whatever the game just wrote.

The native CLI attaches an interactive console with `runktf <zip> -cheat`
(stdin commands between paced ticks; `help` lists them: `regions`, `scan`,
`list`, `read`, `set`, `dump`, `freeze`, `unfreeze`, `frozen`, …). The
browser reaches the same engine through the cheat panel, whose operations
travel over the session socket as a request and an answer.

## WIPI Java API coverage

The `org.kwis.*` surface is measured rather than remembered.
`internal/platform/ktf/testdata/wipi_java_surface.txt` is the reference class
and method list extracted from the original implementation, and
`TestRuntimeJavaSurfaceCoversReference` checks every entry against the
runtime registration table. What is not implemented must be listed in
`wipi_java_gaps.txt` with its reason: an unlisted gap fails the test, and so
does a listed gap that has since been implemented, which stops the list from
going stale. The reference is regenerated from a local checkout:

```sh
WFEATURE_WIPI_REFERENCE_SOURCE=<class source dir> go test -run TestRegenerateWIPISurfaceReference ./internal/platform/ktf
```

The remaining gaps are the bridge constructors that only exist because the
original builds WIPI on top of its own MIDP classes, abstract `Card.paint`,
raw device-format pixel transfers (the 0x00RRGGBB
`getRGBPixels`/`setRGBPixels` pair is implemented instead), and animated
images, whose observer completion protocol is unconfirmed.

Stubs get the same treatment for the opposite reason. An unimplemented method
fails loudly; a stub silently answers a fixed value the game then believes.
`testdata/wipi_java_stubs.txt` inventories every method bound to a fixed-value
implementation together with what it answers, and
`TestFixedValueStubInventory` fails when a stub appears that is not recorded
or when a recorded one stops being a stub. The audit already moved
`Display.hasPointerEvents`/`hasPointerMotionEvents` from `true` to `false`:
no Host delivers pointer events yet, and claiming otherwise invites a game to
wait for touches that never arrive.

Half of pointer input does exist — `Client.SendPointer` dispatches down the
card stack the way a key does. What is missing is the other half and it is a
number nobody has: the WIPI event queue tells events apart by a numeric kind,
and **the pointer's kind appears in no original, no reference implementation
and no local archive.** A game that runs its own event loop would therefore
wait on an event that never arrives, which is why the two queries stay false
until that number turns up.

The event queue is the second input path. A game may drive its own loop of
`EventQueue.getNextEvent`/`dispatchEvent` instead of letting the platform call
into its cards. `getNextEvent` answers a queued event, or — when the queue is
empty — parks the calling worker for one frame and answers the platform's own
redraw request, so a game whose whole frame loop lives in the queue keeps
drawing at a frame-paced rate. `dispatchEvent` delivers key events
down the card stack exactly as Host-driven delivery does, paints the top card
for a redraw request, and routes notify events to the listeners registered
with `Display.addJletEventListener` (or to a `grabKey` listener for a key the
application took over). The first `getNextEvent` call switches `Session.SendKey`
from direct card dispatch to the queue, because delivering a key both ways
would deliver it twice.

**Four classes are registered beyond the WIPI surface**, because titles here
reach for them through the same AOT bridge and the failure is a dead guest
thread rather than a missing method. `org.kwis.msp.media.BaseClip.setBuffer`
is `Clip`'s, moved up to the base class by the handset runtime one title was
built against, where it reports whether the data was taken instead of returning
nothing. `java.lang.Runtime` answers `totalMemory`/`freeMemory` from the same
arena `MC_knlGetTotalMemory` and `MC_knlGetFreeMemory` describe — two views of
one heap that disagreed would have a game free what it just decided it could
afford. `java.util.Calendar` reads the guest clock through the JVM builtin, so
a game that stamps a save sees the time `System.currentTimeMillis` reports.
`java.util.Date` is the instant behind that calendar, and a title reaches for
it directly when what it wants is a number rather than a set of fields — a
stamp to store, or the two ends of an interval. Its four methods and both
constructors read the same clock, which is what keeps an interval measured
across two of them from coming out negative, and `Calendar` gained the
`getTime`/`setTime` pair that moves between the fields and the instant.
**A card title played for an hour before its first `Date.getTime()` ended the
session** — the class resolved all along, as the fallback record makes any
name do, so nothing said it was missing until the call.

Two other additions change drawing. `Graphics.translate` moves the origin of
every later operation: the translation is applied inside the shared clip
step, so all fills, lines, text, blits, and pixel transfers honor it, and the
clip is kept in device coordinates while the clip getters report the caller's
translated space. `Image.setTransparentColor` makes later blits of that image
masked — pixels matching the color keep whatever the target holds — which is
how sprites composite. A decoded image is rasterized into a guest framebuffer
on first masked draw or image-to-image composition, so both kinds of image
share one pixel path.

An image's own encoding declares transparency too, and it has to survive the
same trip. The games' sprites are palette PNGs whose transparent entry lives in
a `tRNS` chunk, while guest pixels are 16-bit colour with nowhere to keep an
alpha channel; dropping it painted every sprite's unused border as opaque
black. Each decoded image therefore carries an opacity mask beside its pixels
(`internal/platform/ktf/image_opacity.go`), the mask follows the image into the
guest framebuffer it is rasterized into, and every blit consults it alongside
any transparent colour the game named. A copy or sub-image inherits the mask
for its region; compositing into a surface marks only the pixels it actually
drew, because the destination keeps its own pixels wherever the source was
absent. Opacity is a yes/no answer — the assets mark a pixel either fully drawn
or fully absent, and partial alpha is not blended, matching `Graphics.setAlpha`,
which is recorded but not applied.

## Runtime diagnostics

Every runtime boundary the client crosses — WIPI C calls, AOT class loads and
allocations, Java native dispatch, guest thread starts, database and file
access, exception throws — is recorded by `countDiagnostic`. Names carry the
guest call site (`… @0x10e68f`), which makes them high cardinality: past 4096
distinct names the call site is dropped so the event itself stays countable,
and only past a 8192-name ceiling does an event fall into `diagnostic
overflow`.

Counts alone lose ordering, which is what a game that runs but misbehaves
needs. `SessionOptions.TraceLimit` adds a ring of the most recent events in
order, with sequence numbers that keep the trimmed prefix visible as a gap.
Hosts enable it in debug builds only. `Session.Diagnostics()` snapshots both;
`FormatCounts` renders the counts most frequent first, the same shape the
acceptance probes print.

Reading a report: a healthy game shows a repeating render loop (`flush lcd`,
`Card.repaint`, `callSerially`, `Graphics.*`) advancing once per tick. Entries
naming `stub table`, `not implemented`, `throw`, `raise`, `error`, or
`found=false` are the missing surfaces to implement next. `field divergence` is
the one entry that names something not yet implemented rather than not yet
reached — see "A published instance field has two storages".

Two of those lines are weaker than they look, and reports keep asking about
them. A `raise <class>` line names the class only: it is a runtime failure being
handed to the guest's own handler chain, and reaching the chain at all means a
handler was found, so a title that raises `java/io/IOException` twice at startup
and then plays is almost certainly reading a file that a first run does not have
yet. **The exception's message is deliberately not in the name**: it would split
one boundary's count across every distinct message, and the counter can only
collapse a trailing ` @0x` site, not a message — so a line that says more would
sum to less. Read a `raise` beside whether the run continued, and reach for the
trace ring rather than the counts when it did not. A `stub table` line is the
other one: the stub is what the original runtime does too, so it is a lead
rather than a defect, and it now carries the address to disassemble.

## Repro routes

Some of what these games do only happens well inside a save — a battle, a shop,
the frame rate under a special attack — and none of it is reachable from a
fresh boot without playing there. `runktf <zip> -route <script>` writes the way
back down, so a scene becomes something a profile run, a regression check, or
whoever is looking at a reported bug can re-enter on demand.

What a route must not be built from is absolute tick numbers, because those are
exactly what breaks. Fixing the type check that made enemies stand still
changed which branch the game took, and every hand-tuned `-key <tick>:<name>`
timing written before it stopped landing on the menu it was aimed at. So a
route waits on what the screen is doing instead, and only counts ticks where
nothing better is available.

```
# comments run to end of line
wait 400              # advance 400 ticks
wait-idle 40 [limit]  # until the screen holds still for 40 ticks
wait-change 30 [limit] # until the screen differs from where the step began
key fire              # press and release; `key down 3` repeats
key #                 # the hash key, not a comment
press left            # for games that act on a held key
release left
shot title            # capture a frame as <framedir>/title.png
mark battle           # capture, and restart the profile here
```

`key` holds its press for the run's `-hold` ticks before releasing it, and
those ticks come out of the route: a press and its release in the same tick is
not a press to a title that samples the keypad once a frame, which is the same
reason `-key` has held one since it was added.

`#` is the comment marker and also a key on the handset, so it is read as the
key name when it is the word a key verb is taking and as a comment everywhere
else. Cutting the line at the character instead made `#` the one key the name
table could resolve but a route could never press.

Both searching waits are bounded — `wait-idle`/`wait-change` default their
limit to twenty times the run they are looking for — because an unbounded one
silently eats the whole run's tick budget and then reports the *next* step as
the failure. When one gives up it says so: `the screen never held still for 40
ticks within 800 — it is animating, not settling` is how a continuously
animating title screen reads. A route that does not arrive comes back as
`route_completed: false` with the line that stopped it, because "the game went
somewhere else" is an answer rather than a crash.

`mark` is the measurement checkpoint: it restarts the profile, so
`-route … -profile r.txt` reports the scene and not the minutes of loading
before it. `shot` only captures.

A route runs on the manual clock unless `-play` is also given, which makes the
replay deterministic and one tick one game frame. To get somewhere far away
faster, add `-play -speed 100`: the guest clock runs that much faster, so each
tick buys correspondingly more guest work.

## Guest profiling

`Session.EnableProfile` switches on the ARM core's sampler (see
`docs/armcore.md`) and `Session.Profile()` resolves every sampled address
against the registered AOT method bodies, because a list of hex addresses is
only useful to someone already holding a disassembly. `Profile.Report` ranks
self time by symbol and then the hottest stacks; `Profile.Folded` writes
flamegraph-folded lines for `flamegraph.pl` or `inferno-flamegraph`.

From the CLI: `runktf <zip> -profile report.txt [-profile-folded stacks.txt]
[-profile-from <tick>]`. `-profile-from` resets the samples at that tick, so the
profile covers the scene being investigated instead of the loading before it.

Symbolization is inference, and the inference has a sharp edge worth knowing.
KTF method metadata records where a body starts and never how long it is, so a
body ends where the next one starts, capped at `maxSymbolSpan`. The highest
registered body has no successor — and what sits above it is not game code at
all but client.bin's own runtime helpers. Left unbounded it claimed them: the
array-load helper came back named `ax.<clinit>()V` holding 57% of a real
profile. The highest body is now bounded by the ninetieth-percentile gap
between the game's other bodies, and an address past any body's inferred end is
reported as an address. An honest unknown beats a confident wrong name.

What the first profile found, on one KTF title at 1500 ticks (182k samples):

| share | code |
|---|---|
| 36% | `ac.a(Lorg/kwis/msp/lcdui/Graphics;IIII[BIII)V` — the game's own sprite blit |
| ~19% | `0x1689e4` — `baload`: null check, bounds check, `ldrsb r0,[array+8+index]` |
| ~17% | `0x167e84` — object header to class/vtable resolution, six instructions |
| ~13% | `0x167020` — static byte load |

The last three are tiny pure-guest client.bin runtime helpers reached by `bl`,
which makes them the natural next targets for the binary-hook table above.

## Text input

An lwc text component is editable. `Client.FocusTextComponent` gives one the
keypad and `Client.TypeKey` delivers a key to it; the editing itself is
`internal/textinput`, the multi-tap input method MIDP's `TextBox` also uses,
so a game types the same way on either platform. Keys the keypad does not
carry are left for the game's own navigation.

The multi-tap timeout is measured against the **guest** clock, so a Host
batching ticks types the same text as one running live.

## One title's startup gate

One local title used to stop at its own screen — `인증오류 ... (오류번호:NNNN)` —
and exit. Four things stood in front of it and each hid the next, so the number
on that screen is the thing to read first:

| Number | What failed |
|---|---|
| 1001 | the capability mask, `MC_knlGetAccessLevel` |
| 2001 / 2002 | the executable listing, `MC_knlGetExecNames` |
| 3001 | the certificate |

The screen looks the same either way, which is why "authentication error" was
taken at face value for several sessions and read first as a missing server and
then as the certificate. It was neither: **1001 is a permission bitmask this
platform was answering too conservatively**, and the certificate is 3001, which
nothing reached until the first three were out of the way.

**First, a capability check.** The title reads `MC_knlGetAccessLevel` and
requires `& 0xbc == 0xbc`. Withholding the network and serial bits failed it,
and the title stopped there — it never reached a network call to be refused at,
so withholding the bit saved nothing. Every group is granted now; see the
access level entry above.

**Second, `MC_knlGetExecNames`** (kernel slot `0x2`), which it asks for its own
application id:

```
name = "010100D5"                     /* the archive's AID */
if (MC_knlGetExecNames(name, 0, 0, out, 300) <= 0) fail   /* error 2001 */
tail = out + strlen(out) - 21
for (i = 0; i < 8; i++)
        if (tail[i] != tail[i + 9]) fail                  /* error 2002 */
        identity[i] = tail[i];
```

It wants a listing whose last 21 characters carry an eight character token
twice, nine apart, and it takes that token as the program's identity, then
compares it with the application id compiled into its own image. The listing's
layout is **reconstructed from that one caller** — nothing describes it, and
the reconstruction is written out at `wipicGetExecNames` along with what is
assumption and what is not.

**Third, the save budget.** `MC_dbListDataBase` reports the storage still
available, and it was counting the archive's own packaged databases against it.
This title ships its maps, its text and its event tables that way — about two
megabytes — so the budget was gone before the player had saved anything, and
starting a new game answered *단말기 저장공간이 부족해서 시작할 수 없습니다*.
Packaged bytes are not storage the player consumed: on a handset they arrive
with the program. Only what a store has grown past what the archive shipped
counts now.

Past all three the title reaches character creation and its opening.

### The certificate behind it

The same title also checks a certificate, `cert.c2s`, which is the publisher's
own file rather than anything WIPI describes: a server issued one per handset,
years ago, and the copy in the archive belongs to whichever handset first
downloaded it. The check decrypts it with the subscriber's number and requires
the plaintext to carry that same number back, so a certificate is only ever
valid for one subscriber and no more accurate answer from this platform will
satisfy it.

The format is understood — `internal/platform/ktf/provision.go` carries it,
and `wfeature provision` writes one sealed for the number this platform
answers with. It is a command a person runs deliberately for an archive they
hold; nothing in the emulator calls it.

## What the corpus names, and what it never asks for

A KTF title is AOT-compiled, so the constant pools a scan of a Java archive
reads are gone. What survives is the **name pool inside the client image**: a
run of NUL-terminated strings holding class names on their own and member
entries of the form `<descriptor>+<name>`. It is what the guest hands to the
platform's class lookup, it is in the file rather than built at startup —
relocation moves the image but does not write those strings — so reading it
costs nothing and runs nothing. `internal/tools/apiscan` does it for a whole
directory of archives and ranks the result by how many titles name each entry.

**Only the class half of the question survives compilation.** A pool entry says
a method exists by name and descriptor; which class owns it is in the records
the image builds at runtime, not in the pool. So a method missing from a class
this platform *does* publish is not something a scan can see here — that half
is what a KTF corpus loses by not being class files, and it stays a matter for
`-diag` and a run.

**A name is what a title could ask for, not what it does.** Every one of the 33
local archives names `org/kwis/msp/lcdui/PluginJlet`; the four checked with
`ktfdump -classes` all extend `Jlet` instead, and their class tables after
`startApp` have no PluginJlet in them. The pool is the SDK's as much as the
title's — a name can be there because the toolchain put it there. That is the
right bound for a pass planned before anything runs — it cannot miss a class a
title will ask for — but a name in this list is a reason to look, not a defect.

What the local corpus names and this platform does not publish, in the order
the scan ranks it:

| Named by | Class | Reading |
|---|---|---|
| 33 | `org/kwis/msp/lcdui/PluginJlet` | Named by every archive, extended by none. |
| 14 | `org/kwis/msf/io/Socket`, `org/kwis/msf/io/URL` | The network classes beside `Network`, which is published. No local run has reached one. |
| 2 | `java/lang/OutOfMemoryError` | A `catch` a title would only take if this arena ran out. |
| 1 each | `java/lang/Short`, `org/kwis/msp/lwc/DialogComponent`, `org/kwis/msp/lwc/LabelComponent` | The two lwc components sit beside the ones that are published. |

None of these is a title that fails today, which is exactly what makes the list
worth keeping written down rather than acted on: the failure mode of a missing
class is not a refusal at startup but "method … was not found from class …" at
the first call, an hour into a save (see "A class left out of that table still
resolves"). **If one of these names turns up in a report, it is already
identified and the fix is the same shape as the others in the runtime table.**

## What a batch of new archives asked for

Seven titles that had never run were driven from a fresh save, and what stopped
each one is worth keeping separately from the surfaces it added, because the
same five shapes will stop the next batch.

**A handle has to survive what the caller stores it in.** The stream database
handed out handles with a high bit set to keep them apart from the record
table's. One title keeps what an open returned in a `short` — `lsl #16; asr
#16` on the result, then a "negative means it failed" test — so the handle it
passed back was a number this platform had never issued. Every read on it was
refused, silently, which left the eight-byte header the title had asked for
untouched; it sized an allocation from the difference of two words that were
never written and asked for four gigabytes. Both database tables now tag inside
a signed 16-bit value (`0x1000` stream, `0x2000` record), which keeps them
distinguishable and keeps them small. **A handset's handles are small numbers,
and a platform's are only as good as the narrowest field a title puts one in.**

**A refusal has to be the guest's kind of refusal.** `DataBase.selectRecord` on
a record that is not there answered with a Host error, which is not catchable.
The specification says `DataBaseRecordException`, a title catches it, and one
title reads a record it has never written on its first tick — an empty save.
The difference between a Host error and a guest exception there was the
difference between its opening screen and a dead thread. Every record-keyed
DataBase call now raises the documented exception.

**A constant a class publishes is part of the class.** `Font` had its methods
and none of its `FACE_*`, `SIZE_*` and `STYLE_*` fields, so a title that picks
its font before it draws stopped at the first read. They are static reads, not
compiled-in numbers, and the values are the specification's.

**`capacity()` is not a hint.** One title walks a `Vector` with `capacity()` as
the bound and `elementAt` as the body, which only works because CLDC's Vector
grows by exactly its capacity increment. Ours ignored the increment and
doubled, so the walk ran off the end of the elements the title had added. A
collection's growth rule is observable, and a title that never calls
`capacity()` is not evidence that the rule does not matter.

**`callSerially` runs on the event thread, not on a thread of its own.** Each
queued Runnable used to become a guest worker. One title queues one from inside
its `paint`, so they arrived faster than workers could finish and the run ended
on the thread-stack limit — after first hitting the queue's own limit. They now
run on the client thread, one per pass: a frame apart while the queue holds the
single self-requeuing Runnable that is one title's whole frame loop, and one
per round while a backlog is draining.

The surfaces that batch added: `java.util.Timer` and `TimerTask` (below),
`Thread.currentThread`, `Integer`'s constructor and its narrowing accessors,
`String.compareTo`/`equalsIgnoreCase`/`endsWith`/`toUpperCase`, `Vector`'s
two-argument constructor, `Font`'s constants, `File.openOutputStream` and
`openDataOutputStream`, and image encoding on both sides of the platform —
`MC_grpEncodeImage` at graphics slot 35 and `Graphics.encodeImage`, which the
specification defines as the BMP bytes of a region of a framebuffer.

### Where the local batch stops now, and what a held key changed

Driving every title in the local set from a fresh save is a run per title and a
contact sheet per run, and two things came out of doing it again.

**A press and its release in the same tick is not a press.** The scripted keys
here used to be delivered that way, and a title that samples the keypad once a
frame never sees one: two titles sat on their own screen for a whole run, which
reads exactly like a title that has stopped. `runktf` now has `-hold`, the same
flag the LGT runner has, and the LGT side is where it cost the most — one
title's character select answers `right` at a one-tick hold and ignores `OK`
entirely, and selects, loads and enters the world at twenty.

**Two titles failed past the screen the batch had checked**, and both are now
in play. What each one turned out to be is in "Three ways a class record was
not a class" below; what they had in common is that the platform was handing
the guest a class record it could not dispatch through.

### A File's output stream is the file

The read side can be a snapshot — a `ByteArrayInputStream` over the bytes as
they are — because reading cannot change them. Writing cannot: a stream that
collected bytes of its own would only reach the file if something copied them
back, and a title that closes the stream rather than the file would have
written its save into nothing. The stream is therefore the file: it carries the
same open-file state, every write goes through to it and is persisted the way
`File.write` is, and `flush` and `close` have nothing left to do.

### java.util.Timer is due on the platform's timer service and runs on a thread

When a Java task comes due is the timer service's decision: a title already
paces itself with `MC_knlSetTimer`, the Host already runs those callbacks in
due order once per round, and a Java task is the same shape of work — a delay,
a body, and possibly a period. A timer with a task and no callback address is
still a deadline, and leaving it out of `NextDeadline` left the Host with
nothing to wait for: a title whose whole first scene is one scheduled task
never reached it, because the Host spun instead of sleeping to the moment the
task was due.

**Where it runs is a different question, and the answer is a thread.** The task
used to be invoked inline on the client thread, on the reasoning that one place
should decide when timed work runs and a task should not race the code that
scheduled it. One title says otherwise, and says it plainly: its whole battle
is a loop inside `TimerTask.run()` that never returns. Run inline, the Host
goroutine is inside that call forever — no frame collected, no key delivered,
the screen frozen on whatever the last flush left — and the run ends on the
service call's step ceiling half a billion instructions later. The
specification's Timer runs its tasks on a background thread, and this platform
already has the machinery: a due task is adopted as a guest worker, the same
way `Thread.start` is, so it parks on its step budget and its loop progresses
tick after tick. Nothing races: workers still hand off strictly, one slice at a
time, under the same run lock.

Three things had to come with it.

- **A Timer has one thread, and its tasks run on it one after another.** A
  worker per due task is not the same thing and does not survive the same
  title: it schedules a fresh task object per frame, each one a loop, and sixty
  four of them stacked up inside a second. A task whose Timer is still running
  one stays pending instead of starting a second worker, which is both the
  bound and the specification's own serial execution.
- **AOT call nesting is per call stack, not per runtime.** The depth counter was
  one number on the runtime, and a parked worker leaves its whole nested stack
  counted while the client thread paints on top of it — so a limit none of them
  reached on its own is tripped between them. Each ARM thread now carries its
  own depth.
- **A repeating timer is re-armed when it is dispatched**, not when the run
  ends, and a period that comes due during a run waits for it. No local title
  schedules a periodic Java timer, so the difference between fixed-rate and
  fixed-delay here is written down rather than measured.

### An AOT method lookup falls back to the guest's own class records

A title registers a class when it first loads it, which is long after it has
been using instances of it. A card whose superclass is not registered yet
therefore has no chain to walk, and an inherited method the platform itself
implements — `keyNotify`, in the case that found this — is reported missing on
a class that plainly has one. The registry is still asked first; a miss now
walks the guest's own class records, which are always there, because the class
being called had to be built to be called at all. This is the same reasoning
the type check already uses (see "AOT outer calls").

### One system-property table, asked from two sides

`HandsetProperty.getSystemProperty` answered `VIBRATORLEVEL` and nothing else,
while the WIPI C `MC_knlGetSystemProperty` answered from `internal/wipic`. They
are the same question about the same handset, so they now share the table. What
a missing answer costs is not a title that skips the feature: `VOLUMELEVEL` is
the number of volume steps the hardware has, a title divides by it, and an
empty string is `Integer.parseInt("")` during `startApp`.

## Three ways a class record was not a class, and the two titles they stopped

The two titles that died past their first screen died of three separate
faults, all of them in the same place: what this platform gives the guest when
the guest asks for a class. Each one is worth its own paragraph, because each
is a different way to answer that question wrongly.

**A fallback class record has to publish a vtable.** `ensureJavaClass` invents
a record for a name the platform does not implement (see "A class left out of
that table still resolves"). It used to invent it with a name, no super and an
empty method table — and no vtable pointer at all. Guest virtual dispatch
reads the vtable out of the class the object header names and indexes it with
the slot from the method record it resolved, so a zero there is a load at four
bytes past null: `ldr r4, [r2, r3]` with `r2` zero and `r3` the slot times
four. That is exactly the "load of guest address `4`" one title died on, a few
hundred ticks into its opening, on a `getClass()` — vtable slot 1 — against an
**array**. Array classes are where it bites first, because an array's whole
method set is Object's and nothing else supplies it. The fallback record now
carries `java/lang/Object` as its parent and Object's vtable slot for slot,
and its metadata is read back out of the record it just wrote rather than
composed beside it, so the registry knows about the vtable too.

**A dispatch alias is a class record the guest hands back.** The alias is the
bounded copy of a class record inside the platform arena that an object header
points at (see "AOT outer calls"), and it is a valid record in every respect
the guest can see — its first word identifies it as one. So a title that
reaches a virtual call through the object header, and then asks the platform
for a method on the same word, was asking about an address the registry had
never heard of: `KTF AOT method g()V was not found from unregistered class
0x3003b104`, on the first paint of one title's card. An alias is now
registered with the VM as an address alias for its class the moment it is
made, which is the mechanism `resolveAOTClass` already used for a class the
guest materialised at a second record. The same lookup's failure message also
reads the record now, so the next miss names a class rather than an address.

**A guest thread has to end.** `Thread.isAlive` answers a flag the JVM clears
when its own goroutine-backed `run()` returns — and this platform replaces
that goroutine with a guest worker (`workers.go`), so nothing ever cleared it.
A title whose loading screen is `while (loader.isAlive()) sleep()` therefore
waited for the whole session: the first title above, once it stopped dying,
sat on an animating `LOADING` for nine thousand ticks with 8,760 `isAlive`
calls in the counts and no missing surface anywhere in the report. The worker
now reports the end through `jvm.VM.EndGuestThread`, which is the platform's
half of having taken over `Thread.start`.

With the three fixed, one title plays — title menu, opening, loading, and its
own platform level with an HP/MP/EXP bar — and the other reaches its world map
with a character and a HUD.

### The array store check was asked about a null class

That run left one counter behind: `checktype` called with a null *class*
operand, 1,500 times, from a single call site. It was never the crash above.
Disassembling the site says what it is — the guest's **array store check**:

```
ldr r2,[r2]        ; the object header out of the array's fields word
asrs r2,r2,#5      ; header>>5 is the dispatch alias offset
ldr r3,[r3,r2]     ; the class record, relative to the JVM context
ldr r3,[r3,#8]     ; its descriptor
ldr r0,[r3,#0x14]  ; the element class — the argument that was null
```

`+0x14` is the descriptor word a class with fields spends on its field table,
and an array has no fields, so the client spends it on the element class
instead. Every array class this platform made left it zero, and a null class
can only be answered with the permissive yes. The element class is now written
there when the array class record is built, which makes the store check a real
question about a real class.

**What the platform must not do is invent that class.** Resolving the element
name through `ensureJavaClass` is the obvious way to write the word and it
breaks five titles outright, deterministically: for an array of a *title's* own
class, the fallback record invented here is the record every later lookup of
that name finds, and the title dies inside its own class construction reading a
field of it — `read guest memory at 0x10`, several hundred instructions into
`<clinit>`. So the element class is **resolved, never created**: an already
known record, a class this platform owns by name, or a nested array class.
Anything else leaves the word zero and keeps the permissive answer that array
had before.

The risk on the other side is a decidable check that answers *no*, which is how
a working title breaks. Two things guard it. An array is assignable to
`java/lang/Cloneable` and `java/io/Serializable`, which no class record of this
platform's making declares, so both are answered directly rather than walked
into a definite no. And the A/B sweep below says what actually changed across
the local set: no title stopped where it did not before, no `tick_error`
appeared, and **no new `checktype reject` was recorded anywhere** — while the
undecidable calls fell from about 4,000 across the set to 530, all of them
arrays of a title's own classes.

### A band beside a title screen was the title's own

One title paints its title screen into the top-left 176x220 of the 240x320
screen and leaves a uniform dark-red band down the right. The archive's
`__adf__` says `DisplaySize:176*220`, which makes the band look like a
platform that never sized itself to the title — so it was worth settling
rather than guessing, and it is settled: **the band is the game's own**.

Three measurements say so, in the order they were taken. The band appears in
the same frame as the picture, not before it, so both arrive from one draw.
The title paints through a single `drawImage` of its own back buffer, and
probing the blit says that buffer is **240x320**, not 176x220 — so the band is
inside the game's surface. And filling every newly created surface with green
at the platform boundary left the band red, which means the guest wrote it.

Two things fall out of this worth keeping. `DisplaySize` is **not** a screen
size to adopt: thirteen of the local KTF titles declare `176*220` and twelve of
them draw across the whole 240x320 screen, so honouring the declaration would
shrink twelve working titles to fix a band that is not a fault. And a new
off-screen surface is **not** cleared here — the specification does not promise
it (`MC_grpCreateOffScreenFrameBuffer` says nothing about content), and the
one title that would have shown the difference turned out not to depend on it,
so nothing was added for it.

### And the title behind the band is not a small-screen title

The band being the guest's own left one question open: whether to show that
corner in the middle of the screen instead, since a title written for a smaller
handset would look right that way. It was built and measured, and the answer is
no — **the title is a 240x320 title whose title art happens to be 176 wide.**

Detecting the case from pixels works, which is what makes the negative result
worth keeping. `DisplaySize` alone cannot decide it, but the frame can: the
outside of the declared rectangle holds a fill rather than a picture, and the
colour of that fill is not the picture's own background. Over roughly nine
thousand frames of all thirty-two local KTF archives, the fill colour covers
**88% to 100% of the inside for every title that draws across the screen, and
8% for the one that does not** — a gap wide enough that the threshold is not a
tuned number. The rule fired on one title, over one unbroken run of frames, and
on nothing else.

What killed it is the same title's own menu. Three measurements, all by
differencing two captured frames:

- Its load dialog is at x 60..179, **centred on 119.5** — the middle of the
  240-wide screen, not of the 176-wide art. It crosses the rectangle's edge, so
  the detection correctly switches off the moment the dialog opens, and the
  picture jumps back into the corner while the player is looking at it.
- Driven into a new game, the title fills the whole screen: its scene reaches
  the bottom and its dialogue box redraws at y 291..314, far below 220.
- So the band is not a platform artefact of the wrong screen size. It is what
  this game showed on the handset it was written for.

Centring it would clip that dialog and move a picture the game placed where it
meant to. The item is closed as *not a fault*, and the general lesson is the
one the detection rule proves rather than the one it was built for: a title
that draws its art in a corner and its menus on the whole screen is telling you
which of the two is the screen.

### The screen is a Host's answer, and it has to reach both surfaces

A Host may name another handset — `runktf -screen WxH`, or the page's setting
— and both generations of package take it. `Client.SetScreen` and
`NativePlatform.SetScreen` are where it lands, and
[`session.md`](session.md) has the cross-platform contract.

Two things about it belong here rather than there. **A size answered to one
surface and not the other shears the picture.** A title reads the screen from
`MC_grpGetDisplayInfo` and takes the pixel buffer from the screen framebuffer
record, and two local titles then write into that buffer with the stride the
info struct gave them; answer one and not the other and every row after the
first lands at the wrong offset, which looks like a decoder fault rather than a
half-wired setting. Both answers are read from one place for that reason, and
`screen_test.go` fails if they diverge — on the earlier package too, where the
pair is the display record and the image the blits land in.

And **`DisplaySize` is still not the answer**, for the reason the section above
established: thirteen local titles declare `176*220` and twelve draw across the
whole 240x320 screen. The size stays something a Host is told rather than
something the archive is asked, so a title that really was packaged for a
smaller phone is a flag away and the twelve that were not are untouched.
Measured across the 34 local archives: at the default screen 29 first frames
are byte-identical to the previous build and the 6 that differ are the family
whose noise floor is measured above; at 176x220, 33 run clean and lay
themselves out correctly.

### An A/B sweep of the local set is half noise

Driving every local title with the same script under two builds and diffing
the captured frames is the regression net for a change like the three above,
and it is only readable once the noise floor is known. Running the *same*
binary twice, at `-play -speed 20`: **16 of 32 titles differ, over 24 of 128
captured frames.** A run paced against the wall clock takes a different number
of guest instructions per tick each time, so a shot lands on a different frame
of an animation; the same nondeterminism moves which tick a title reaches a
failure on.

So a frame hash that differs between two builds is not evidence. What is:
a title that stops where the other build did not, an error text that changes,
and a run that ends at a different tick *with* one of those. On the three
fixes above, base-versus-new was 18 of 32 — inside the floor — with the two
target titles carrying the only real differences: `route_completed` false to
true, and a `tick_error` that went away.

**Those two figures are about a sweep, and they badly understate the per-frame
rate.** 24 of 128 comes from four captured frames per title; sampling that
sparsely, most shots miss the animating region. Diffing *every* frame of one
continuously animating title instead, same binary twice, gives **1,216 of 2,379
frames differing** — a little over half. Anyone who reads the sweep's 19% as a
per-frame noise floor and then diffs a whole frame directory will conclude a
change broke something.

For a title that animates, the count of differing frames carries almost no
information. What does is **where the differences are and how big**: the run
above is entirely small boxes on a loading spinner's corner and a sprite's idle
bob, and two frames picked out and looked at are indistinguishable. A real
regression is a difference that is not on an animating element, or one large
enough to be a layout rather than a phase — and those are visible in the
bounding boxes without looking at a single frame.

### How a refusal is shaped decides whether a title can act on it

`MC_netConnect(cb, param)` has two documented ways to say no, and the
specification is explicit that they are not interchangeable. Returning an error
means "the callback is never called". Returning **0** means the callback is what
reports the outcome: `void cb(M_Int32 error, void *param)`, with `error` either
0 or `M_E_ERROR` (-1). Both are legal answers for a handset with no coverage.

This platform used to refuse at the call, and for one local title that is the
same as not answering at all. It asks once, is refused, calls `MC_netClose`
twice, and then sits on its "waiting for the server" screen for twenty thousand
ticks. The evidence that it is waiting rather than looping is in what it stops
doing: `MC_knlCurrentTime` is called **92 times in 20,000 ticks**, so it is not
running a timeout of its own, and the only other kernel slot it touches is the
frame timer it re-arms once a tick. Nothing in the platform's view is blocked —
the title is waiting for a call it was owed and will never get.

A radio reports failure when it has finished trying, not at the call, so the
callback form is what the handset does. A caller that registers one now gets `0`
and, at the next service point, `cb(M_E_ERROR, param)`. That is the same "no",
delivered where the game is listening for it.

**A caller that passes no callback keeps the synchronous error**, because there
is nowhere to deliver a failure and the return value is then the only answer
available. Titles that never register one cannot be affected, which is the
property that made this safe to change without a way to run them.

This is the record-database lesson in a different table — "a storage call that
answers no is not a safe default: it is a value the game will believe" — with
the refinement that *how* the no is shaped matters as much as what it says.

**There are two network surfaces here, and only one of them had this problem.**
A title can reach the network through the WIPI C table above or through Java's
`org.kwis.msf.io.Network`, and the local titles split between them: the one that
stalled uses the C table, and the one whose documented row is a `△` uses the
Java class. The obvious next worry is that the Java side has the same defect, and
it does not — the specification makes `static int connect()` **fully
synchronous**, answering 0 if access is already available, 1 if it was just
established, and -1 on failure. There is no callback to register and therefore
no second shape the "no" could take. Returning -1 is the whole contract, which
is what that native already does.

Measured across the local KTF set with a boot probe, only three archives show
anything network-shaped at all, and one of those is a resource file whose name
contains "NET". That is a floor rather than a census — a title's network prompt
can sit behind more navigation than a boot window reaches, which is exactly
where the stalling title's was.

### An unimplemented table call names its call site

The WIPI C interface is an array of table pointers and a game indexes it, so a
table number never appears in the guest's own code and cannot be searched for.
When a call into a table this platform does not implement fails, the error now
carries the guest address it was called from, the same way an unimplemented
kernel slot has for a while: the stub returns with `bx lr`, so LR still holds
the caller. Without it, `table 1 function 1 is not implemented` is not something
that can be investigated at all — with it, it is an address to disassemble.

Tables 2, 3, 5, 7, 9, 10 and 11 are misc, graphics, the record database, the
database, UI components, media and net; **table 1 is the utility table** and
**table 4 the input method**, both named below. The first title to reach table 1
does so from a content-download prompt.

What the call site says, from the first one captured. The caller does:

```
ldr r3, [pc, …] ; add r3, r10 ; ldr r3, [r3]   ; the interface pointer
ldr r3, [r3]                                    ; interface[0]  = table 1
ldr r5, [r3, #0]                                ; table1[0], kept for later
…
ldr r3, [r3, #4]                                ; table1[1]
mov r0, r4                                      ; a sign-extended 16-bit value
bl  _call_via_r3                                ; bx r3
strh r0, [r3]                                   ; the 16-bit result, to a global
```

Three things follow. The call reaches the platform through one of RVCT's
`_call_via_rN` veneers — a table of `bx rN` at the end of the image — which is
why LR survives it and why the caller can be named at all. `interface[0]` is
table 1, which agrees with the number in the error. And function 1 takes a
signed 16-bit argument and **returns a 16-bit value the caller stores**, which
is the shape of a getter or a converter rather than of a download: a fetch would
not answer synchronously in a halfword.

That is as far as a static read goes, and on its own it was not enough to
answer the call: the record-database entry above is what happens when a platform
guesses, and a title that believes a fabricated answer fails somewhere
unrecognisable much later, which is strictly worse than a `tick_error` naming
the call.

**What identified the table was three readings agreeing, not one.** Every table
already named sits exactly one past the original runtime's own zero-based
numbering — misc, graphics, the database, UI components, media and net all line
up — and the entry before misc there is the utility table. The specification's
utility section lists six functions in one order: `MC_utilHtonl`,
`MC_utilHtons`, `MC_utilNtohl`, `MC_utilNtohs`, `MC_utilInetAddrInt`,
`MC_utilInetAddrStr`, which is the order the original runtime's own table
carries them in. And the call site above says function 1 takes a sign-extended
halfword and returns a halfword the caller stores — the shape of `MC_utilHtons`
and of nothing else in that list. A title converting a port number is exactly
what belongs in front of a download.

So table 1 now answers those six. All four byte-order conversions are one swap
on a little-endian host, kept as separate cases because a sixteen-bit swap of a
thirty-two-bit register answers a caller of the wrong one with a plausible wrong
number. `MC_utilInetAddrInt` refuses an address it cannot read with the
specification's own `-1` rather than inventing a host to connect to, and the
seventh entry the original runtime carries — a vendor hash with no published
prototype — keeps failing with its call site, which is the same rule that kept
the whole table failing until it could be identified. None of the local archives
reaches this table in a 400-tick boot, so what closed it is the specification and
the call site rather than a run.

**A stubbed table names its call site too, and it is the only place that address
is ever recorded.** Tables 5-6, 8 and 12-17 are stubbed in the original runtime,
so a call into them is accepted, counted and answered with zero rather than
failed — there is no error message for the site to ride on, and a report line
reading `wipic stub table 4 function 3` names a number that appears nowhere in
the guest's own code. The count now carries the address, in the ` @0x` form the
diagnostic counter already collapses when the name budget runs out, so two sites
for one slot still add up under the name reports have always used and a caller
in a loop cannot spend the budget on one boundary. `wipic stub ` is already one
of the prefixes whose detail survives the name limit, which is what makes the
address worth carrying on a busy run.

The first observation to arrive this way: a title that reaches its play screen
calls **table 4 functions 3 and 4, once each**, with no visible symptom and no
`tick_error` — the run continues cleanly past both. Disassembling the two
addresses says what the pair is for, and one probe then says what the table is:

```
ldr r4, [r3]        ; a global, holding a pointer to the table
ldr r3, [r4]
ldr r0, [r3, #0x10] ; table[4]
bl  _call_via_r0    ; no arguments are set up at all
adds r5, r0, #0     ; the answer is kept as a base
ldr r3, [r4]
ldr r0, [r3, #0xc]  ; table[3], again with no arguments
bl  _call_via_r0
subs r2, r0, #1     ; the answer is a count: the loop runs it down to zero
blt  <past the loop>
```

Both take **no arguments**: the register the call goes through holds the
function pointer, not a parameter. Function 4 answers a base and function 3 a
count, and the caller walks `base + index*4` comparing each word against four
five-byte names it has just copied out of a twenty-byte structure, storing the
index of each match. So the pair enumerates something the handset knows about
and the title looks its own four names up in.

**The four names settle it, and they are one probe away.** They arrive through
a static slot, so the image does not hold them where a search can find them —
but the caller's own stack does, at the moment of the call. A temporary probe
that dumps the words at `sp` and follows any of them that points at printable
bytes prints the structure whole:

```
[sp+0x4]=0x200ffedc -> "KO\0\0\0EN/S\0EN/L\0N123\0"
```

`KO`, `EN/S`, `EN/L`, `N123` — five bytes each, which is where the stride came
from. That is the **input method's** vocabulary and nothing else in the WIPI C
surface speaks in it: the specification defines an automaton's modes as an ISO
639 code, with `/S` or `/L` appended where a script has case, and `N123` for
digits. **Table 4 is the input-method block**, and it sits where it does for the
same reason the LGT runtime's does: the specification prints the `MC_im*`
functions at the end of its *graphics* section rather than in one of their own,
so the block follows graphics on both platforms.

Two entries are now answered and three are not, which is what the evidence
supports. Entry 4 is `MC_imGetSupportedModes` — the only function in the block
that returns `M_Char**` — and entry 3 is `MC_imGetSurpportModeCount`. Both take
no arguments, which is what the call site already said. `MC_imSetCurrentMode`,
`MC_imGetCurrentMode` and `MC_imHandleInput` are somewhere in entries 0 to 2;
**this vendor's order is not the specification's**, which would have put the
count first, and no local title has called one, so they stay counted stubs
carrying their call sites. The modes answered are the same four the LGT side
answers with, because the specification fixes the vocabulary rather than the
platform choosing it.

Answering zero had been safe rather than merely quiet — a count of zero makes
the caller's loop run no iterations, which is the same "nothing supported" a
handset without an automaton would give — but it left the title's four mode
slots at the `0xff` fill it had memset them to. They now hold indexes, and the
title asks for nothing further: driven to its world map, it calls no other entry
of the table.

**The next table this way is 13, and four titles call it at boot.** A sweep of
all 32 archives at 400 ticks turns up `wipic stub table 13 function 0` in four
of them, three of those pairing it with function 2 and one with function 1 — the
same three-entry shape the original runtime gives that table. Two call sites,
sixteen bytes apart in one title, say most of what a static read can:

```
ldr r0, [r3]        ; table13[0], no arguments
bl  _call_via_r0    ; and the answer is not read at all
...
ldr r0, [r3, #8]    ; table13[2], no arguments
bl  _call_via_r0
str r0, [r4]        ; kept, in a global structure at +0x810
```

So entry 0 is a void call a title makes on the way in, and entry 2 answers one
word it keeps. The same probe that named table 4 was pointed at this one across
three titles, and it did **not** name it — which is worth writing down as much
as a success would have been:

- **All three entries take no arguments.** The registers hold different junk in
  each title, and every call site sets none of them. A second title's entry 1
  is the same shape.
- **Nothing identifying is on the caller's stack.** One title happens to have a
  pointer to `save.dat` / `option.dat` a few words down, which is suggestive of
  storage and is *not* evidence: the words below a call are whatever that
  function was already doing.
- **The four titles share no publisher**, so this is something their common
  toolchain emitted rather than one engine's habit.

Three no-argument calls, one answer kept, no symptom. The specification has
no-argument getters in more than one section — `MC_fsAvailable` and
`MC_fsTotalSpace` among them — so the shape does not choose between them. What
would is the *use*: the word entry 2 answers is stored at a fixed offset in one
title's own global structure, and finding where that offset is read again is the
next thing to try. Answering it with a guess is what the record database above
is a warning about.

### Reading a guest fault

A fault used to come back as an address and an opcode — `Thumb instruction
58d4 at 0x17f086: read guest memory at 0x4` — and the address is never the
question. What was being dispatched, on which object, and from which Java
method are, and the registers at the faulting instruction hold all three.
`runAOTMethod` now composes the same evidence a guest throw already carries
when a run ends in an instruction fault, and it is what turned the first title
above from a week of disassembly into an afternoon:

```
regs=[0x1a29b8 0xbc56c 0x0 0x4 …] sp=0x202ffd24 lr=0x180cd5
operands=0x300bd7fc [Lm; runtime=20 guest=20; 0x300cded4 java/lang/String …
frames=[1]=0x12ead1 s.a([Lm;ILjava/lang/String;)V+0x84 …
```

The report is best effort and read-only: a fault has already left the guest
somewhere unexpected, so a probe that fails is dropped and the rest still
arrives. `armcore` already carries the failing context out in the run summary,
so nothing had to be added to the core to get it.

## A second download gate, and the answer that opened it

Another title in the same series opens by offering to download 600KB "to play
the game", and both answers lead back to it: yes reaches its own connection
failure, no returns to the title screen where any key brings the offer back.
The archive ships that 600KB — five data files that come to almost exactly the
figure the prompt names — so the download already happened once, on the handset
this copy came from.

The decision is made before any of it is looked at. In order, the title:
opens its 64-byte `prefs` database, reads all 64 bytes from position zero,
closes it, asks `MC_knlGetSysProperty("PHONENUMBER")`, prints the number
through `printk`, and puts the prompt on the screen. It never opens a data
file, never asks whether one exists, and never reads a second property. So what
it is deciding from is that record and this platform's subscriber number.

The record is closed and stayed closed. It is not the certificate format the
other title in this series uses (`provision.go`): its trailer checksums do not
hold, and the application id is not at the front of it under that cipher. It is
not a repeating-key exclusive-or against a subscriber number either — the byte
coincidences at any period from one to sixteen are at noise level, and forcing
an eleven-digit key produces nothing readable.

**The number was the half worth pushing on, and it is the whole gate.** Sweeping
`PHONENUMBER` and re-running the same scripted route says exactly where the
title's own branch is:

| Answer | Length | What the title does |
|---|---|---|
| `""`, `"0"`, `"911"`, `"0100"` | 0–4 | opens all five data files and plays |
| `"01000"` … `"01000000000"` | 5–11 | offers the download |

The predicate is the **length** and nothing else — `"911"` passes and `"01000"`
does not, so it is not the value, not a prefix, and not a match against the
record. A number of five digits or more is a subscriber this title can bill,
and it then checks the receipt it cannot read here; four or fewer is a handset
with no line, and it skips straight to the data it already has.

With a short number the run does what the reference behaviour describes: it
opens each packaged `.dat`, seeks `(0, SET)` then `(0, END)` for the size,
seeks `(-44, END)` and reads the 44-byte footer, and then reaches slot
selection and character creation.

```
DBG seek name=char.dat offset=0   origin=0 position=0     size=45677
DBG seek name=char.dat offset=0   origin=2 position=45677 size=45677
DBG seek name=char.dat offset=-44 origin=2 position=45633 size=45677
DBG read name=char.dat want=44             position=45633 size=45677
```

That footer check is why the seek call has to answer the **new position**
rather than a success code: a seek that answers zero makes every packaged file
look empty, which is the other way into the same prompt. `wipicDatabaseSeek`
already answers the position, so once the number stopped naming a subscriber
nothing else was in the way.

**What this costs is the other title in the same series.** That one compares
the platform's number against the one sealed into the certificate it is
provisioned with, and a four-digit answer makes it stop on its own
`인증오류 (3001)` instead of starting. No single number serves both, so the
number became a setting — `WFEATURE_PHONE_NUMBER`, or the server's `-number` —
and the default is the eleven digits that keeps the certificate title working.
Playing this one is one environment variable, and [`network.md`](network.md)
has the trade in full.

Two things are still true about the record: it cannot be read, and a `prefs`
written for a handset this platform can be would still be the honest way in if
one ever turned up. Dropping it in the save tree under `db/prefs` is where it
would go, since a saved record takes precedence over the packaged one.

## A download prompt that one saved record will not let go of

One title that ships its downloadable data inside the archive opens its start-up
asking to download it again, over a phone line that no longer exists, and both
answers end the run: yes reaches its own "server connection failed", no exits.
The archive is not missing anything — the run opens every packaged data file
and stats each at its exact size — and the emulator is not refusing anything it
should not.

**What decides it is one persisted record, not the archive and not the clock.**
With an empty save directory the title goes straight to its title screen, its
menu, and — with `-play` — into the game. It writes four small databases on that
first run, and a second run reads them back and is happy. It is one *older*
copy of the first of those four that produces the prompt: dropping that single
file into an otherwise-fresh save directory reproduces it every time, and
dropping any of the other three does not.

That record is a fixed-size array of obfuscated 32-byte entries. Comparing a
working copy with the rejected one byte by byte says almost everything: the two
differ by a keystream that repeats every eight bytes across the whole body,
which means they hold **the same content encrypted under different keys**,
except for five bytes in one entry. Those five bytes decrypt to the low 40 bits
of `MC_knlCurrentTime` at the moment the record was written, and the same value
is also in the header — a timestamp, and the only semantic difference between a
copy the title accepts and one it does not.

**The timestamp is not what it compares, though.** Driving the guest clock is
what settles that: writing a record, then re-reading it with the clock moved
forward days or weeks — including across the 31-bit truncation the header keeps
— never produces the prompt, and moving the clock so that "now" lands exactly on
the rejected record's own timestamp does not clear it. So the predicate is not
"this record is old", "this record is from the future", or a counter that
wrapped. What the record carries beyond its timestamp, this platform cannot see
from outside the obfuscation.

Three things are worth keeping from that:

- **The emulator's storage is not what is wrong here.** The bytes go out and
  come back unchanged, the title accepts its own writing across runs, and the
  packaged data files it reads through the same table are all found at their
  right sizes.
- **Deleting the title's save directory restores it.** That is the only
  recovery: the prompt's own force-delete offers to clear everything and
  download again, which needs the server.
- **A save that a title writes can put it somewhere it cannot leave** when the
  way out of that state runs through the network. It is worth suspecting
  whenever a title that used to reach its menu stops at a start-up gate, and it
  is answered in one run by pointing `-save` at an empty directory.

## An earlier KTF package: a module file with no descriptor and no JAR

Every KTF archive this platform grew up on has the same two things in it: an
`__adf__` descriptor at the top level, and a JAR holding a
`client.bin<BSS-size>` native image. `archive.go` opens on the first and
executes the second, and `internal/platform/detect` uses `__adf__` as the
discriminator that names the platform.

A local archive dated 2005 has neither. Inside a single directory named after
the title — its entry names are EUC-KR, so a UTF-8 `unzip` cannot even extract
it — sit a `.mif`, a `.mod`, a `.sig`, a `.cfg`, and the title's own resource
files. There is no JAR and no class file anywhere in it. The descriptor loader
stops at `KTF archive has no __adf__` and detection answers `unknown`, both
correctly: this is a different generation of the carrier's download package,
not a damaged copy of the one we read. `OpenNative` in `native_archive.go`
reads it, and `IsNativePackage` tells the two apart from entry names alone.

### The information file

`.mif` is a fixed header, a table of span offsets, and the spans:

```
0x00  four 16-bit header fields
0x08  offset of the first header section
0x0c  offset of the second header section
0x10  offset of the span table
0x14  span count
0x18  offset of the first span, repeating the span table's first entry
0x1c  a further offset
```

The span table holds one offset per span and then the end of the last one, and
whatever follows that end is an identity record closing with the four bytes
`1fim`. That trailer is the only self-identifying mark the format has — there is
no leading magic — so it is what the parser validates.

Three span shapes carry their own marks and are decoded: a 16-bit length
covering itself, a MIME name and its terminator introduces a typed blob (the
icons, `image/bmp`); two `0xfe` bytes introduce EUC-KR text (the vendor twice,
then the title); anything unmarked is numbers. The numbers are kept verbatim,
because what most of them mean is not established.

**Two of them are.** One header word is the module's own length rounded up to a
page, and `NativeInfo.ModuleSpan` finds it by computing that length and looking
for it. A size the platform can work out for itself is a weak thing to read out
of a file, but it is the anchor that says which of the unlabelled words are
sizes at all. The other is in a span: `0x00100000`, the same image base this
platform already maps a KTF executable at.

Reading the rest of the header would need its record stride, which is not four
bytes — a second size, `0x5000`, sits at an offset that makes the surrounding
words decode as tag-and-value pairs on a stride that has not been pinned down.
Any aligned-word scan of that area picks up boundaries as if they were values;
one did, and mapped 0x85000 for a module needing 0x25000. The loader reads none
of it: it maps the module's own page-rounded length and nothing more, which the
module turns out not to need — see "There is no BSS" below.

### The module needs no relocation

The two executables look alike and are not. The descriptor package's
`client.bin` **begins with a self-relocation loop**: it takes its own load
address from `pc`, subtracts its link address, and walks a table of offsets at
file offset 0x88 adding that delta to every word. That is why its literals are
zero-based, and why `armcore` documents a loader that "relocates itself".

The `.mod` has no such loop and **no relocation table at all** — scanning for
the ascending-offset runs that make one finds eighteen in the descriptor
package's image and zero here. Its code is uniformly PC-relative instead:

```
ldr r0, [pc, #n]      ; a link-time offset
add r0, pc, r0        ; the module's own base, position independently
```

Sweeping candidate link bases across the address space finds no absolute base it
prefers. It is position independent, so it is loaded rather than relocated, and
the load address is the platform's choice.

### The platform table is planted below the module

The module finds the platform in the word immediately **below its own load
address**:

```
ldr r0, [pc, #n] ; add r0, pc, r0   ; its own base
ldr r0, [r0, #-4]                   ; the pointer the loader planted
ldr r1, [r0, #0x68]                 ; one of its functions
bx  r1
```

So a loader has to map a page under the image and put a table pointer in its
last word. That is `nativeHeaderBase` in `native_client.go`, and it is a
different binding from the descriptor package, whose entry is handed
initialization arguments and asks for interfaces by name.

**The slot numbering is not the one this platform serves.** The module calls
slot 26 with a size, null-checks the result and fills the block with its own
function pointers — an allocator — where the kernel table here has its allocator
at 20 and its calloc at 21. Rather than guess a mapping, `LoadNativeClient`
fills every slot of both surfaces with a distinct trap stub carrying its own
number across the supervisor-call boundary, so a run reports the slots the
module asked for, in order, with the link register of each call site. `Serve`
then answers one slot at a time: name it, answer it, run again, and see what
the module asks for next.

Reaching for a static scan instead does not work here, and the way it fails is
worth knowing. A linear disassembly of the `.mod` **stops after twelve
instructions**: offset 0x2c is a Thumb veneer table, and the module is mixed
ARM and Thumb throughout. Two scans built on a linear decode reported zero
call sites each, which reads exactly like "the module never calls the platform"
rather than "the disassembler stopped". Decode each address independently in
both states, or run it.

### The start-up protocol

Both halves of the package negotiate through one reference-counted interface
shape, and they use the same one: an object is a pointer to a table whose first
two entries raise and drop a count and whose third dispatches. Nothing is named.
Every step is a number, and the numbers that matter come out of the information
file rather than out of the module. `native_client.go` performs it in three
calls:

1. **The entry returns a factory.** Called with the platform object, a spare
   argument and an out parameter, the module allocates 0x24 bytes through
   platform slot 26, fills the block's tail with four of its own functions,
   points the block's first word at them, sets its second word to 1 — the
   reference count — calls slot 0 of the platform object, and writes the block
   through the out parameter. The four functions are *not* the Clet entry
   points: they are raise, drop, dispatch and a return.

2. **The factory creates the title's object.** Dispatching on it with the
   identifier the information file carries reaches a class factory that
   allocates 0xe8 bytes and then asks the platform object for interface
   `0x01001001` — and that query is what has to succeed, because the module
   checks the pointer it passed rather than the result. Given an object it
   raises the count on both sides and writes the title's own object out.

3. **A small integer is an event.** The title's object dispatches straight into
   its own handler, which switches on 0, 1, 2, 3 and above. That is the Clet
   event contract the specification describes, reached at last: sending 0 runs
   the title's code, and it begins asking for graphics and file slots within a
   few hundred instructions.

**The identifier is picked out by two anchors landing in one record.** That
record's last word is the image base and its first is the constant the module's
own dispatch compares against. Neither alone would do: the file holds other
round numbers, and taking the first record's first word picks up a `0x1000` the
module refuses.

### Two steps that look like they succeeded when they have not

Both cost a session, and both are the same mistake in different clothes.

- **The result is not the answer; the out parameter is.** The factory writes the
  object it built through a pointer the caller passed and returns something
  else. A caller reading the result alone sees zero and calls it success.
- **A missing argument reads as a broken module.** The factory rejects a null
  second or fourth argument before doing anything, and returns exactly as it
  would on any other failure. Called with three arguments instead of four it
  does nothing, silently, and the module looks inert.

A third belongs beside them, on our side of the line: `armcore.Call` derives a
temporary context and restores the parent by leaving it untouched, so reading r0
off the *thread* after a call reports whatever the parent held — zero on a fresh
client. The result has to come off the run summary. That one turned a failure
into a success in the probe's own log for two runs.

### What the title asks for once it starts

The first run to reach app code produced this, and it was the specification for
the work that followed:

```
entry object        offset 0x0000 (slot  0)  raise
entry object        offset 0x0008 (slot  2)  query interface
interface 0x1001001 offset 0x0014 (slot  5)
platform            offset 0x0068 (slot 26)  allocate
platform            offset 0x008c (slot 35)
platform            offset 0x00ac (slot 43)
platform            offset 0x00c0 (slot 48)
```

It stopped in the title's own code dereferencing a null it was handed, which is
what an unanswered slot looks like from inside a game. Each row was answered the
same way slot 26 and the interface query were: name it, serve it, run again.

**The slot numbering is not this platform's.** Slot 26 allocates and slot 27
frees, where the kernel table serving the descriptor package has its allocator
at 20 and its calloc at 21. Nothing about the descriptor package's numbering
carries over.

### The surface is bounded, and a taint sweep is what says so

Answering one slot at a time says what to do next but never says how much is
left. A static sweep does: the module reaches the platform table through one
global — the word at `ImageBase-4`, which every function loads PC-relatively as
`base+0x3c` off `ImageBase-0x40` — so a linear pass over the image that follows
that value from register to register, adding the `adds rN, #k` it accumulates
and recording each `ldr` off it, enumerates the table.

Sweeping the whole image finds **sixteen slots**, and that is the flat half of
the package entire:

```
0x00 memcpy      0x14 strlen      0x68 allocate    0xbc  ?
0x04 memset      0x20 sprintf     0x6c free        0xc0  current application
0x08 strcpy      0x64 ?           0x8c ?           0xcc  ?
0x0c strcat      ...              0xac milliseconds 0xdc ?
```

Two things make the sweep trustworthy where the disassembly alone is not. The
module is mixed ARM and Thumb, so a *linear* decode stops after twelve
instructions and reports zero call sites — decode each address independently.
And the same base word is read from ninety-four sites and no other offset off
that base is ever read, so `base+0x3c` is the whole of what the loader has to
plant.

What each slot does came off its call sites rather than out of a name:

- **`memset` then `memcpy`.** One function clears `0x1b` bytes at a stack
  address through slot 4 and then copies ten bytes into it through slot 0. Two
  arguments and a length, a destination returned: that is the pair.
- **`strlen`.** Slot 5 is called on a buffer and its answer compared against 14,
  and the module's own write wrapper calls it when its caller passes no length.
- **`sprintf`.** Slot 8 takes a buffer, a format string from the image, and two
  halfwords. It renders through the same WIPI C formatter the descriptor
  package uses, with the variadic arguments walked from r2 through the stack.
- **A millisecond clock.** Slot 43 is read, saved, and read again in a loop that
  spins while the difference is below 2,000 — and the module seeds its own
  generator from it.
- **"Which application am I".** Slot 48 is called from thirty-nine sites and its
  result indexed at `+0x20`, `+0x28`, `+0x2c`, `+0x4c`, `+0x50`, `+0x54`,
  `+0x84`. Those are the offsets the title's own object uses on itself, so the
  answer is that object. The module does not carry `this` through its own
  calls; it asks the platform for it.

### The three surfaces, and the objects the module asks for by number

The package is not one table. It is three kinds of thing, and telling them apart
is what makes a call log readable:

1. **The flat table below the image**, above.
2. **The object handed to the entry.** Its first three vtable entries raise a
   count, drop it and answer a query by number; `+0x10` fills a display record;
   `+0x2c` takes an interval, a function and a context.
3. **One object per interface number.** The module queries five —
   `0x1001001`, `0x1001002`, `0x1001003`, `0x100100b`, `0x1002000` — and one
   more, `0x18000fe`, from the object it already holds. Each answered query gets
   a trap table of its own, so what the module calls on one is recorded apart
   from every other.

`0x1001001` is the screen, and the class factory queries it while it is still
building the title's object, writing it through a pointer it checks rather than
the result it reads. `0x1001002` is asked how much memory is free and whether a
given size can be had — the module walks it down from 64KB by halves and keeps
the largest block both it and the allocator will give. `0x1001003` is the
title's own files. `0x1002000` and `0x100100b` each take a function and a
context; one of the two functions the module registers clears the flag its own
modal wait spins on, so those are event sources it waits on rather than
observes.

### A file is an object, and the module's own wrapper names its methods

The file interface hands back an object rather than a number, so one trap table
serves every open file and a handler tells them apart by the object it was
called on — which is what the module does too.

The module's own file wrapper is what names the methods, because it calls them
in a shape that only reads one way:

```
if mode == 2:  exists(name)            ; and create(name) when that says no
handle = open(name, mode)              ; retried with mode 4 when 2 or 8 failed
information(name, this+8)              ; and the length lands at this+0x10
```

and then, through the object:

```
seek(whence, offset)   the wrapper computes its own position from the same two
                       arguments and passes them straight through, so both
                       sides agree on the codes: 0 from the start, 1 back from
                       the last byte, 2 from where it is
read(buffer, length)   at 0x0c
write(buffer, length)  at 0x14
```

One loop in the module drives read and write both and picks between them on a
flag its caller passes. Serving `0x14` as a second read is what a first pass
did, and the title loaded its data perfectly well — and then failed a gate
whose whole purpose was to find out whether it could write.

### The gate that reads as a memory error, and the write behind it

With every file call answered but the write, the title booted, loaded all five
of its data files, registered its frame and then drew this, once, and never
moved again:

> FS 메모리 공간이 부족합니다 / 최소 2KB 이상의 메모리를 확보한 뒤 다시 시작해
> 주십시오

The message names memory, and it is about the file system. Its code is a byte
the title sets in itself, and a **watchpoint on that byte** named the
instruction that set it in one run — which is worth more than any amount of
reading, because the branch that reaches it is not the one the message suggests.
What it does is: clear 64 bytes, then write them to a scratch file thirty-two
times and check that each write returns 64. Thirty-two times sixty-four is the
2KB in the message. A write that answers zero is a handset with no room.

So the title's start-up gate is a file it creates, writes 2KB to, and deletes.
Opening a name that is not there for writing has to create it, and what the
title writes has to be readable back: both are the same requirement, and
`docs/skvm.md` records the same one arrived at from a different direction.

### The title does not have a loop of its own

Its start event ends by handing the platform an interval, a function and a
context through the entry object's `+0x2c`, and everything after that happens
inside that function: it reads the clock, steps its own state machine, draws,
and asks to be called again — the re-arm is the last thing each frame does, so
the schedule replaces rather than accumulates. A Host that only starts the title
sees it load its data and stop.

The interval this title asks for is 48ms.

Because the re-arm replaces the schedule rather than editing it, a Host tick has
to read it *after* it has run the title's other callbacks and not before. The
sound and timed services are answered first, both of them reach the title's own
listener, and a listener that re-arms leaves the pointer a tick took earlier
naming a struct nothing else points at: the tick would then stamp its next due
date onto that orphan and call the function the title has just moved on from.
`NativePlatform.Tick` reads `platform.frame` after the two advances for that
reason. No local title has been seen to re-arm from a listener, so this is the
contract holding rather than a fault observed.

### What the title draws with, and the table that named the colours

The screen interface's `+0x14` takes a rectangle of four halfwords, two colours
and a mode. What the colours *are* came out of the module's own data: one call
site walks a 36-word table drawing 1x1 rectangles, and that table holds
`0xffffff00`, `0x0000ff00`, `0x00ff0000`, `0xff000000`, `0x00ffff00`,
`0xff00ff00`, `0xffff0000` and zero. White, blue, green, red, cyan, magenta,
yellow, black — so a colour is `0xRRGGBB00`, and no amount of staring at the
call site would have said which end the red was on.

`+0x1c` ends a frame: it is the only call the title makes in a frame that draws
nothing.

**The title draws its own text.** There is no glyph call in the trace — the
version string and its menus are drawn as runs of 1x1 rectangles through
`+0x14`, which is why the first screen a Host would show is legible before any
font work exists at all.

### The sprites are Windows bitmaps, and the title builds them itself

`+0x18` on the screen is the blit, and for a long while it arrived with a null
where its pixels should be. The argument order was not the problem: a
breakpoint at the call site showed the null already there, and following it up
through two frames of the title's own code reached an array of image pointers
the title fills once, at startup.

**A watchpoint on one of those array slots named the writer in a single run.**
It is `platform slot 25`, which the module calls as

	create(0x1004001, data, out, out)

with a number from the same family as the interfaces it queries. So the slot is
a factory and `0x1004001` is a class. Dumping `data` at the call settles what
the class is:

```
42 4d 18 0f 00 00 ... 36 04 00 00 28 00 00 00 36 00 00 00 32 00 00 00
01 00 08 00 00 00 00 00 ...
```

"BM", a file size, pixels at 0x436, a 40-byte information header, 54 by 50, one
plane, eight bits, no compression — a **Windows bitmap**, with its 256-entry
palette in the 0x400 between the two headers. The title decodes its own
graphics archive itself and hands the platform the one format every handset of
the era already had a decoder for.

Two details of the object matter beyond decoding it:

- **Its first word points at the bitmap.** One of the title's own paths reads
  that word and indexes the bitmap through it, so an object that only the
  platform can read would break a path that never calls the platform at all.
- **Magenta is not drawn.** Eleven of the fourteen images the local title
  builds for its title screen have `0xff00ff` in their top-left corner, and
  drawing it opaque puts a magenta box around every sprite. The blit carries no
  colour of its own and the mode it passes is always 7, so the artwork is the
  whole of the evidence.

### The title takes keys as events, and its own menus decode them

Reading the switch in the title's event handler is what named the numbers. It
compares the event against 0, 1, 2 and 3, and then against `0x101`, `0x102` and
`0x404`. The first two store a code and hand it to the current screen: a key
pressed and a key released. `0x404` is compared and ignored.

The code space came out of the screen that acts on it. The title's own key
handler compares against `0xe030` and `0xe035`, so a key is `0xE000` with a
character in the low byte — the keypad's digits are their own characters, and
the five-way pad is the ring around the 5:

```
0xe032 up      0xe034 left    0xe035 select    0xe036 right    0xe038 down
```

Sweeping codes and comparing the frame afterwards is what proved it, and it is
worth saying why the sweep had to be *shaped*: a sweep of 0 to 63 moves
nothing, because no code in that range is a key. A sweep of `0xe000` upwards
found exactly one code that changed the screen — and that one opened the
title's menu.

From there the route into the game is the title's own:

```
select   the title screen opens its menu
select   the menu enters its first item
left     the two choices under it, and the second is the new game
select   a difficulty
select   and the game starts
```

Driving that is what the acceptance run does, because every screen after the
first is one the title only reaches by being played — and it is also what says
the input contract is right, since a wrong key code is indistinguishable from a
title that ignores keys.

### The sound is SMAF, which this project already decodes

The title plays through an interface it queries by number and hands a block of
memory to, and what that block is settles what the interface is. Dumping it at
the call:

```
4d 4d 4d 44 00 00 00 c6 43 4e 54 49 ... 53 54 3a 75 6e 74 69 74 6c 65 64
```

"MMMD", a length, a `CNTI` chunk, `ST:untitled,VN:PsmPlayer V5` — a **SMAF**
file, the same format the descriptor package's titles keep their sound in. So
the earlier package needed no new decoder, only the three calls around one:
`+0x0c` sets the clip, `+0x10` starts it, `+0x14` stops it. The call passes no
length, and none is needed: a SMAF file names its own in the four bytes after
its magic.

**The end of a clip has to be reported.** The module sets a flag when it starts
a sound and clears it only inside the function its own listener calls, so a
platform that never says "that finished" leaves the title believing its music
is still playing. Its listener recognises the pair (1, 14) and nothing else,
which is the whole evidence for what those two numbers are.

Beside the player is a second source the title starts with a duration in
milliseconds and then *waits* on: it sets a flag beside the call and clears it
only when that source reports 2 or 3. Both are answered here, which is why
this platform delivers events to the listeners the module registered rather
than only collecting them.

### The last of the flat table, and the objects nobody reads the answer to

- **Slot 47 is a second deallocator.** Its call site picks between it and the
  ordinary free on a flag the module keeps beside each entry, and what it hands
  over is one of the objects the factory built. So an object is destroyed
  through the slot that knows what it is, and everything else through free.
- **The screen's `+0x10` is a status line** — the title passes a flag word and
  a message, and the message on this title is `WAIT..`. There is nowhere for a
  Host to put it: the title draws its own screen, and this is the handset's
  furniture. It is recorded rather than drawn over what the title is drawing.
- **The screen's `+0x24` is called once a second** while the title runs, which
  is what a handset's "keep the backlight on" looks like.
- **`0x18000fe` fills a 38-byte record** the title turns into a 26-character
  identifier, padding whatever the record leaves empty with `X`. It hashes the
  result and compares it against a hash it computed the same way, so what is in
  the record decides nothing on its own — but a record left empty is the same
  identifier on every handset that ever ran it, so the subscriber number this
  emulator already answers with goes in.

### The header sections have a stride, and two numbers found it

The information file's two header sections were carried verbatim because
reading them needed a stride nothing had established. The stride is **eight
bytes, with the tag in the last halfword**, and what establishes it is that the
module's own page-rounded length and the other size beside it land at the same
place in consecutive records with consecutive tags:

```
value 0x00060001  extra 1  tag 2
value 0x03e80001  extra 0  tag 4
value 0x00005000  extra 0  tag 5
value 0x00025000  extra 0  tag 6   <- the module's length, rounded to a page
```

An aligned word scan of the same bytes reads the boundary between two records
as a value of its own, which is how an earlier pass mapped 0x85000 for a module
needing 0x25000. The loader now reads tag 6 — and still checks it against the
length it can work out for itself, because a package whose tag 6 disagrees with
the module beside it is a package this reader has misread, and mapping what it
said anyway is how a wrong stride becomes a wrong map.

Only tag 6 is established. The rest are decoded and carried.

### How a Host holds it

The package is wired to every Host through the layer they already share, which
means none of them has a branch of its own:

- `internal/platform/detect` names it `ktf`. It is the same vendor and the same
  platform; what differs is the package, and telling a Host otherwise would
  send it looking for a loader that does not exist. The discriminator is a
  `.mif` beside exactly one `.mod`, and it is consulted last — after every
  descriptor marker has failed *and* after the class scan. A lone `.mod` is too
  common an extension to claim an archive for, and the pair is a shape rather
  than a marker, so a MIDlet repacked without its `.msd` could wear it by
  accident; what a class file says it links against is evidence and outranks
  it. The order is free on a real package of this generation, which carries no
  class files for the scan to read.
- `ktf.NativeSession` is the same shape on the outside as the descriptor
  package's `Session`: start, tick, take the frame, send keys, search memory.
  Underneath, a tick is "run the title's frame if it is due" and the wait it
  answers with is what is left of the interval *the title* asked for, so a Host
  that honours it runs the game at the speed it was written for.
- `internal/session` picks between the two generations inside its KTF case,
  which is what puts the earlier one in the browser, the CLI and anything else
  built on it at once.
- The identity comes out of the module information file rather than a
  descriptor: its name for a label, and its application number for the
  directory the saves belong in. That number is not a claim the package makes
  about itself and nothing else — it is the same number the module's own class
  factory is asked for, so a package whose number did not match its module
  would not start at all.
- The cheat engine attaches the way it does to the descriptor package, over a
  core of its own. The map is different and the labels say so: this title keeps
  its whole state in what the platform allocated for it, so `arena` is where a
  search for a health bar will find one.

`runktf` takes both generations, and refuses by name the options that only mean
something to the other one: there are no AOT method bodies to profile or
symbolize here, and no descriptor to inspect.

`-route` works on both. A route is written against what is on the screen and
which keys are pressed, and neither of those is a property of how the title was
packaged, so the script file and the runner are the same — `internal/route`
asks a platform for four functions rather than for an interface. What the
earlier package supplies differently is only where the frame comes from and
what a tick costs: `NativePlatform.FrameDigest` fingerprints the screen in
place so a wait can ask every tick without copying it, and `Stalled` is "no
deadline is pending", which on this package means nothing is left to run the
title at all — it has no loop of its own. A `mark` names a place on the way
rather than somewhere to start measuring from, because there is no profiler
here to reset.

### What a run keeps of the calls it traps

Every platform-table call is trapped, and for a while every one of them was also
kept. That is affordable for a probe and not for a session: the title calls the
platform for as long as it runs, about seven times a frame at rest and 237,801
times over the acceptance route alone, and at 48 bytes a record the list was
costing a player roughly 25 MB an hour and never giving any of it back.

The list was serving two questions that turn out to have different shapes. *How
many times was this slot called, and where was it first called from* is what
says whether implementing a slot matters, and it is bounded by the number of
slots — 37 on the local title — so `NativeClient` accumulates it in a map that
cannot grow with the session. *What did the module do, in order* is a start-up
diagnostic, so the ordered log keeps the first `maxNativeRecordedCalls` records
the way `Trace` and the blit list keep theirs, plus any call that failed: a
failing call is the one a reader opened the log for, and it ends the run anyway.

`Calls()` is therefore a window and `CallCount()` is the total, while
`SlotSummary()` counts every call the run ever made and reports whether the slot
was ever answered. Measured on the local title after the split: the ordered log
settles at its bound, the heap stops moving between tick 1,000 and tick 3,000,
and the slot count is unchanged.

### A save is a file the title writes

The title writes its settings and its save slots as ordinary files beside its
own data, so they go to the same `fs/` space in the save store the descriptor
package's guest filesystem uses. Two things follow, and both are what a player
would expect rather than what fell out:

- **A save wins over the package's own copy of the same name.** The title ships
  a settings file and then writes to it; a lookup that preferred the archive
  would start every session from the shipped settings.
- **A write reaches the store as it happens**, not at some close that a title
  killed mid-session never performs.

### What is still open

- **Nothing on this title's route still traps.** Every slot it reaches through
  its own menus and into its first map is served, so the next unknown will
  arrive as a trap in a run rather than as a list here. Read the acceptance
  run's table: it marks each slot `served` or `trap`.
- **The keys with no code.** The two soft keys, send and the volume pair are
  not mapped: nothing establishes what this handset numbers them, and a value
  sent as itself cannot be told from a mapping that is wrong. What the module
  compares against is a set per screen — `0xE021`, `0xE02B`, `0xE030` and
  `0xE03E` all cancel on the screens that take any of them — so there is more
  than one key here that a Host has no name for.
- **The information file's other tagged fields.** The stride is established and
  four fields decode; only the module length is understood. `0x5000` under tag
  5 sits beside it and is not a BSS size, which the run below settles.
- **A repro route runner.** `internal/route` drives the descriptor package's
  sessions; this one takes a key script on the command line instead.

### There is no BSS, and mapping nothing extra is the measurement

An earlier pass mapped a fixed `0x8000` past the image because the information
file names a second size beside the module's own and neither could be read out
of it safely. That allowance is now gone, and removing it is a result rather
than a tidy-up:

- The module holds **no absolute address at all** — it is position independent
  and addresses everything PC-relatively.
- It reads **exactly one word from outside its image**, the platform table
  pointer below it. A sweep for every other offset off that same base finds
  none.
- Everything it works on it asks the platform to allocate, including the object
  it keeps its whole state in.
- A run with no allowance and a run with `0x8000` are byte-identical through
  start-up, data loading and three thousand frames: same draws, same presents,
  same call counts.

So the second size the information file names is not a BSS size for this module.
If a module of this generation ever does need space past its image, it faults on
the first access rather than running on quietly, and the fault names the
address — which is the report that would say so.

## Sound in C, and the table that was accepted and thrown away

Fourteen of the thirty-three local archives keep their sound in the **WIPI C**
media block — table 10 — rather than in `org.kwis.msp.media`. That block was
stubbed: `MC_mdaClipCreate` answered a 32-byte record and everything else
returned zero. Every one of those calls succeeded, the game had no way to tell,
and the title was silent. Two of them were reported by the user as "no sound",
and the corpus scan afterwards said the other twelve were in the same position.

The evidence for what each function is comes from the callers rather than from
the specification's print order, which this block does **not** follow. The
specification lists twenty-one `MC_mda*` functions; a title that loads seven
sounds reaches 0, 4, 8, 11, 15 and 16, where the printed order would put the
same six operations at 0, 3, 11, 14, 17 and 18. Dumping the argument registers
of every call settles them:

| fn | arguments seen | what it is |
|---|---|---|
| 0 | `("Yamaha_MA2", 0x1a4e, cb)` | `MC_mdaClipCreate` — r0 is the media type and r1 is the byte count of the sound about to be loaded |
| 4 | `(clip, →"MMMD", 0x1a4e)` | `MC_mdaClipPutData` — r1 points at the SMAF magic and r2 is the size the create asked for |
| 8 | `(clip, 0 or 1)` | `MC_mdaPlay(clip, repeat)` — called immediately after each putData |
| 11 | `(clip)` | `MC_mdaStop` — the first of the three calls that end a clip |
| 7 | `(clip)` | `MC_mdaClipClearData` — the second |
| 3 | `(clip)` | `MC_mdaClipFree` — the third, after which the handle is never named again |
| 15 | `(5, 10, 15 … 100)` | `MC_mdaSetVolume` — one argument, walking a fade |
| 16 | `(20, 50)`, `(100, 50)`, `(0, 0)` | `MC_mdaVibrator(level, timeout)` |
| 17 | `(3, 1)` | `MC_mdaSetMuteState(source, bmute)` |

What pins the teardown run is an ordered trace rather than counts. Four titles
produce exactly:

```
fn=0  ("Yamaha_MA3", 0x3cdb, cb)   create
fn=4  (clip, →"MMMD", 0x3cdb)      put data
fn=8  (clip, 1)                    play
fn=7  (clip)                       clear
fn=11 (clip)                       stop
fn=3  (clip)                       free
fn=0  ("Yamaha_MA3", 0x3cdb, cb)   … and the next sound
```

Stop, clear, free is what a title does with a sound it has finished with, and
play/pause/resume/stop in the run 8..11 is what puts stop at 11 rather than
free. The one call whose reading is left open is **26**, which arrives as
`(clip, 45)` or `(clip, 60)` between a putData and its play: either a per-clip
volume or the water mark a streaming clip raises its callback at. Nothing here
has a per-sound gain and nothing here streams, so both readings come out as the
same accepted no-op and choosing between them on this evidence would only put a
guess in the record. `internal/platform/ktf/wipic_media.go` carries the table
and this reasoning beside it.

**The volume reaches the sound path.** `MC_mdaSetVolume` is not stored and
ignored the way the LGT block stores its own: a title fades its music out by
walking this to zero, and a runtime that remembers the number and keeps playing
at full has heard the request and disregarded it. `backend.Audio.SetVolume`
scales note velocities and wave samples on the way to the sink, and zero also
releases whatever is sounding — scaling the next note is not enough when a note
started at full volume is ringing under a volume the game has already set to
nothing. A note already sounding otherwise keeps the level it started at, so a
fade moves in note-sized steps, which is what these titles play anyway.

**The mute state is accepted and not applied.** Its argument is a *source*, and
nothing here says which source a number names; reading a per-source mute as a
global one silences every clip in the game. That is the LGT block's decision in
`docs/lgt.md` "Sound", reached again from the same evidence — one caller, once,
at startup.

**Nothing frees the clip's guest record.** Releasing it would be right if
function 3 is a free and a use-after-free if it is not, and what says it is a
free is a call order rather than a contract. Thirty-two bytes per sound a title
finishes with is the price of not having to be right about that; the bytes that
cost something are the sound's, and those do go. A title that creates a clip per
effect and frees none of them is bounded separately: past 64 live clips the
oldest one's sound is dropped, which is what a handset out of audio memory does.

The two reported titles record 1,343 and 3,243 MIDI messages over a scripted
run where both recorded zero before. Across the whole KTF corpus the first-frame
A/B is unchanged: six frames differ and they are the six that differ from
themselves.

## Deliberately incomplete

- repairing a guest write to a published instance field rather than reporting
  it: see "A published instance field has two storages". The mechanism is
  there and the counter is zero across every local archive, so which side wins
  would be decided without a case to decide it on
- pause/resume/destroy lifecycle and pointer input
- the WIPI Java entries listed in `testdata/wipi_java_gaps.txt`, each with its
  reason, and the fixed-value answers inventoried in
  `testdata/wipi_java_stubs.txt`
- animated image playback: see testdata/wipi_java_gaps.txt
- the graphics context's transparent-pixel, alpha and origin-offset fields: all
  three are stored and returned, and no drawing call reads any of them. The clip
  and the pixel operation are the context fields a draw honours. Two titles set
  the transparent pixel, both to the same magenta and once each at startup; a
  third sets the alpha through a fade in even steps, and the map title sets the
  origin offset on nearly every frame — so all three are reached rather than
  theoretical, and none of them is what boxed that map's objects (see "An image
  is drawn through its own handle") or what dropped a sprite's backdrop (see
  "The transparency a title brings with it")
- cheat write watches, hit tracing, and cheat-table save/load (Phase 4)
- in-game progression is verified by the user playing, not by automated
  probes; probes only surface missing API surfaces to implement
