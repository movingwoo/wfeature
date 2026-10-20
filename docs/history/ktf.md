# KTF implementation history

This file preserves implementation investigations and dated validation records.
Statements about current behavior, missing APIs, corpus size, and planned work
apply to their original revision; later records may supersede them.
Start with [ktf.md](../ktf.md) for the maintained overview.
This consolidation adds no execution or acceptance evidence.


## Contents

- [Implementation record through 2026-09-17](#implementation-ktf-platform-implementation-status)
- [Input path](#implementation-input-path)
- [Client execution](#implementation-client-execution)
- [Platform initialization boundary](#implementation-platform-initialization-boundary)
- [AOT outer calls and runtime Java bridge](#implementation-aot-outer-calls-and-runtime-java-bridge)
- [A published instance field has two storages, and the boundary is where they agree](#implementation-a-published-instance-field-has-two-storages-and-the-boundary-is-where-they-agree)
- [Host integration](#implementation-host-integration)
- [A BMP declares its transparent colour in the header](#implementation-a-bmp-declares-its-transparent-colour-in-the-header)
- [LBMP: the handset's own bitmap](#implementation-lbmp-the-handsets-own-bitmap)
- [The graphics context's clip is how a tile gets drawn](#implementation-the-graphics-contexts-clip-is-how-a-tile-gets-drawn)
- [A font handle is an identifier, and it was being read as a height](#implementation-a-font-handle-is-an-identifier-and-it-was-being-read-as-a-height)
- [An image is drawn through its own handle, not the framebuffer inside it](#implementation-an-image-is-drawn-through-its-own-handle-not-the-framebuffer-inside-it)
- [The transparency a title brings with it: the context's pixel operation](#implementation-the-transparency-a-title-brings-with-it-the-contexts-pixel-operation)
- [XOR mode is how one title family draws everything](#implementation-xor-mode-is-how-one-title-family-draws-everything)
- [`copyArea` is not the MIDP call it looks like](#implementation-copyarea-is-not-the-midp-call-it-looks-like)
- [Text a title composes by XOR, out of a font sheet that has no colour](#implementation-text-a-title-composes-by-xor-out-of-a-font-sheet-that-has-no-colour)
- [A null surface is a state, not a fault](#implementation-a-null-surface-is-a-state-not-a-fault)
- [A rounded rectangle is a curve, and one title's shadow is an ellipse](#implementation-a-rounded-rectangle-is-a-curve-and-one-titles-shadow-is-an-ellipse)
- [Screen flush](#implementation-screen-flush)
- [Recognized C runtime routines](#implementation-recognized-c-runtime-routines)
- [The load screen was profiled, and it says no as well](#implementation-the-load-screen-was-profiled-and-it-says-no-as-well)
- [Pacing](#implementation-pacing)
- [Cheat engine](#implementation-cheat-engine)
- [WIPI Java API coverage](#implementation-wipi-java-api-coverage)
- [Runtime diagnostics](#implementation-runtime-diagnostics)
- [Repro routes](#implementation-repro-routes)
- [Guest profiling](#implementation-guest-profiling)
- [Text input](#implementation-text-input)
- [One title's startup gate](#implementation-one-titles-startup-gate)
- [What the corpus names, and what it never asks for](#implementation-what-the-corpus-names-and-what-it-never-asks-for)
- [What a batch of new archives asked for](#implementation-what-a-batch-of-new-archives-asked-for)
- [Three ways a class record was not a class, and the two titles they stopped](#implementation-three-ways-a-class-record-was-not-a-class-and-the-two-titles-they-stopped)
- [A second download gate, and the answer that opened it](#implementation-a-second-download-gate-and-the-answer-that-opened-it)
- [A download prompt that one saved record will not let go of](#implementation-a-download-prompt-that-one-saved-record-will-not-let-go-of)
- [An earlier KTF package: a module file with no descriptor and no JAR](#implementation-an-earlier-ktf-package-a-module-file-with-no-descriptor-and-no-jar)
- [A module compiled against a longer superclass](#implementation-a-module-compiled-against-a-longer-superclass)
- [A blit measured its source from the wrong corner](#implementation-a-blit-measured-its-source-from-the-wrong-corner)
- [Sound in C, and the table that was accepted and thrown away](#implementation-sound-in-c-and-the-table-that-was-accepted-and-thrown-away)
- [Guest workers unwound together when the session closed](#implementation-guest-workers-unwound-together-when-the-session-closed)
- [A third download gate, and the lever that did not open it](#implementation-a-third-download-gate-and-the-lever-that-did-not-open-it)
- [The earlier package's title screen carries a band, and it is the title's own](#implementation-the-earlier-packages-title-screen-carries-a-band-and-it-is-the-titles-own)
- [What a sweep of a 262-archive set asked for](#implementation-what-a-sweep-of-a-262-archive-set-asked-for)
- [The park the guest is told about](#implementation-the-park-the-guest-is-told-about)
- [A touch reaches the game](#implementation-a-touch-reaches-the-game)
- [The two allocations a busy session made, and neither was doing anything](#implementation-the-two-allocations-a-busy-session-made-and-neither-was-doing-anything)
- [A game action is a subscript, and only one direction of the table knew it](#implementation-a-game-action-is-a-subscript-and-only-one-direction-of-the-table-knew-it)
- [A timer armed twice is one timer, not two](#implementation-a-timer-armed-twice-is-one-timer-not-two)
- [Five titles the interactive rung failed, and not one of them was stopped](#implementation-five-titles-the-interactive-rung-failed-and-not-one-of-them-was-stopped)
- [A directory the platform made, and the two questions that answered it differently](#implementation-a-directory-the-platform-made-and-the-two-questions-that-answered-it-differently)
- [A batch Host's clock had no rate, and a guest that waited paid for it](#implementation-a-batch-hosts-clock-had-no-rate-and-a-guest-that-waited-paid-for-it)
- [Three members a sweep that held keys down reached](#implementation-three-members-a-sweep-that-held-keys-down-reached)
- [Publishing the rest of the core library](#implementation-publishing-the-rest-of-the-core-library)
- [A text component's protected fields](#implementation-a-text-components-protected-fields)
- [A key is a callback](#implementation-a-key-is-a-callback)
- [A packaged save that holds the address of the phone that wrote it](#implementation-a-packaged-save-that-holds-the-address-of-the-phone-that-wrote-it)
- [A name the title asks for](#implementation-a-name-the-title-asks-for)
- [Deliberately incomplete](#implementation-deliberately-incomplete)
- [Compatibility follow-up (2026-09-12)](#followup-ktf-compatibility-follow-up)
- [Native loading stall](#followup-native-loading-stall)
- [Requested landscape packages](#followup-requested-landscape-packages)
- [Unconditional Jlet teardown](#followup-unconditional-jlet-teardown)
- [Host-composed text](#followup-host-composed-text)
- [Java text spacing (2026-09-15)](#text-ktf-java-text-spacing)
- [Validation](#text-validation)
- [Startup compatibility investigation, 2026-09-19](#startup-compatibility-2026-09-19)
- [KTF C input and bitmap investigation (2026-09-19)](#ktf-c-input-bitmap-investigation)
- [KTF gameplay QA, 2026-09-20](#ktf-gameplay-qa-2026-09-20)
- [KTF 1.2 QA, 2026-09-21](#ktf-qa-2026-09-21)
- [KTF dialogue clipping and deferred text input](#ktf-graphics-input-qa-2026-09-23)
- [KTF input availability after rejected confirmation](#ktf-input-lifetime-2026-09-23)
- [KTF slot creation and save overlay reads](#ktf-save-slots-2026-09-23)
- [An earlier-package title's settings, sky and sparks, 2026-10-20](#ktf-native-settings-2026-10-20)

<a id="implementation-ktf-platform-implementation-status"></a>

## Implementation record through 2026-09-17

See the [2026-09-12 coverage audit](coverage.md#ktf-skt-ktf-and-skt-coverage-audit) for current
API gaps, scanner limits, and corpus results. Historical counts below describe
their own runs.

This platform validates a KTF archive, locates its native image, maps it into
the pure-Go ARM core and runs it: the client relocates itself, initializes, is
asked for its main class, and then plays — its Java through the AOT bridge, its
drawing through the WIPI C surface, its saves through the shared save store.
Per-title progress is not recorded here; the sweep sections record what a whole
local set did on the day it was driven.

<a id="implementation-input-path"></a>

### Input path

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
although the decompressed bytes and central-directory CRC agree. The loader
accepts this case only after checking the complete length and CRC against the
central directory; ZIP integrity checking stays on for damaged contents.

<a id="implementation-client-execution"></a>

### Client execution

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

No original game code or archive is committed as a fixture. This opt-in command
parses ignored local games without executing guest code:

```sh
WFEATURE_KTF_ACCEPTANCE=1 go test -run TestLocalKTFArchivesParse -v ./internal/platform/ktf
```

All 32 KTF archives then under `var/games/ktf` pass it. A separate probe runs
client entry points through self-relocation, checks that the returned
`WIPI_exe`, `ExeInterface`, names, initialization/class-lookup functions and
function table lie in mapped guest memory, and calls both initialization
functions:

```sh
WFEATURE_KTF_EXECUTE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesInitialize -v ./internal/platform/ktf
```

All 32 return the expected `WIPI_exe`/`ExeInterface` chain and finish both
initialization functions with result zero, under a ten-million-instruction
ceiling and without starting a game. Initialization state stays on `Client`, so
the next probe calls the image's real `ExeInterface.GetClass` for the ADF main
class:

```sh
WFEATURE_KTF_LIFECYCLE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesLoadMainClass -v ./internal/platform/ktf
```

All 32 return a non-null, name-matching, independently validated `MClass`
record; repeated loads use a cache separate from the placeholder runtime-class
table.

<a id="implementation-platform-initialization-boundary"></a>

### Platform initialization boundary

`Client.Initialize` maps a 64 MiB platform-data arena at `0x30000000` and a
separate 1 MiB read/execute callback-stub arena at `0x35000000`, and builds the
five KTF initialization arguments (JVM/exception contexts, primitive
descriptors, the direct callback table). Its generated Thumb stubs preserve
`r4`, put a bounded service identifier in `r12`, cross the shared ARM
supervisor-call boundary and return through `lr`.

The startup-only callback surface supplies the WIPI C kernel and Java bridge
tables, bounded allocation, class lookup, Java object/array allocation, and
class/string registration. Individual platform allocations are limited to 4
MiB, arrays to 1,048,576 elements, strings to 65,536 UTF-16 units, and the whole
arena to its mapping. Pointers and source ranges are checked before Host reads
or writes guest memory.

<a id="implementation-the-arena-and-the-stubs-cannot-share-an-address"></a>

#### The arena and the stubs cannot share an address

The stub arena used to sit at `0x31000000`; growing the data arena from 16 MiB
to 64 MiB without moving it made the allocator's span cover the platform's own
stubs. The mapping did not fail: both mappings were in force over the overlap,
so guest writes into its allocation were permitted and landed on the stubs.

None of the symptoms points at an allocator. A title that allocates past 16 MiB
is handed the stub arena, and its next screen fill writes its 16-bit pixel value
over the stub table. When something later calls a stub, a kernel slot executes
as `strh r0, [r1, r1]` and faults on an address the game never computed. Both
failures a user reported on that title — a four-gigabyte allocation and a null
framebuffer handle — were this, several thousand calls downstream.

The arenas are now a gigabyte apart, a constant expression in
`initialization.go` fails to compile if they overlap again, and `armcore.Map`
refuses an overlapping pair whose permissions differ in both directions.

<a id="implementation-a-screen-that-only-waits-still-allocates"></a>

#### A screen that only waits still allocates

`MC_knlFree` was an accepted no-op on the reasoning that a bump-only arena is
reclaimed with the client. That stopped holding when the collector got a free
list, and never held for a title that allocates inside its drawing loop.

**Failure.** One local title's confirmation dialog calls `MC_knlCalloc`, copies
into the block and calls `MC_knlFree` about sixteen times a tick. The counts
pair up — 28,992 allocations against 28,962 frees over 1,845 ticks — so the
leak was the platform's. The arena's growth log climbs a flat ~272 KB between
collection cycles while the object collector reclaims 0–144 bytes each time and
never tracks more than 29 objects; it stops at 66,913,236 of 67,108,864 bytes,
and the run dies inside a timer service on the next allocation with a
`tick_error` naming the arena.

- **It is counted in ticks, not inputs.** The same screen left alone dies at
  1,813 and 1,845 ticks across runs with different key lists. A user sees it as
  intermittent, because pressing keys is what keeps them on one screen.
- **An idle screen is not a control.** The same game's title screen idles 3,000
  ticks with two collection cycles and 823,308 bytes of arena. Only screens
  whose animation allocates show it.
- **The error names the wrong layer** — whichever service made the final
  allocation (a timer here), several hundred thousand allocations after the one
  that should have been released.

**Fix.** An allocation that would fail now runs a collection and asks the arena
once more (`collectForAllocation`). Growth is the ordinary trigger and misses
this shape: a title reaching the end of the region between cycles is stopped
while the objects that would make room are already unreachable. Running a
collection inside a platform call is safe because the Go side of the collector
is precise: an object a Go frame still holds is retained, so the sweep drops
only the Host's strong reference, and the grace cycle frees nothing in the cycle
that first finds it unreachable. The LGT Java path has the same trigger and
reaches the same guarantee differently, because its objects are not bound to Go
objects — see `docs/lgt.md`.

`MC_knlFree` now releases the block. `allocateWIPIC` records each block's arena
size against the identifier it issued, because free receives only the
identifier. An identifier this platform did not issue — a double free, an
interior pointer — is counted as `wipic free of an unknown allocation` and
ignored: the specification makes it the program's error, a handset does not
end the program over it, and releasing on that key twice would hand one address
to two owners.

After the change, same title and key list: 6,000 ticks against the former death
at 1,845, three collection cycles instead of 243, the arena settling at
1,349,588 bytes, and 112,086 allocations against 112,058 frees. The count of
`wipic free of an unknown allocation` is zero, which confirms the identifier
passed to free is the word calloc returned, not the payload pointer twelve bytes
in — the assumption the change rests on and the first number to check on a
suspected regression.

A working free is not a one-line change:

- **`MC_knlCalloc` has to write its zero fill.** Every block used to be a slice
  of a freshly mapped arena; a reused block carries the previous tenant's bytes.
- **Host state keyed by a guest address is dropped with the block.** The
  transparency mask of an image or framebuffer lives in a map keyed by its
  handle; a released address that kept its mask would impose it on the next
  allocation — the black-box-behind-the-sprite failure described under
  [A transparent pixel still has a colour](lgt.md#implementation-a-transparent-pixel-still-has-a-colour),
  from the opposite direction.

Deliberately not done: `MC_grpDestroyImage` and
`MC_grpDestroyOffScreenFrameBuffer` stayed accepted no-ops. They were a much
smaller leak — 27 image creations against 15 destructions over the same 1,845
ticks — and releasing an image record a live graphics context still points at
would corrupt drawing on every title. The identifiers are registered, so the
release is available once a title is measured leaking through them and no
context is shown to outlive the image. (Superseded in
[The same lie, one table over](#implementation-the-same-lie-one-table-over-the-graphics-destroys).)

**New risk: use after free.** While free did nothing, a block a title freed and
kept using was harmless; now it can become a Java object or another buffer.
Seven titles across 1,500–6,000 ticks, and all 32 booted, showed nothing, but
this failure appears as intermittent corruption in a long session, not on a
first screen. The heaviest free-user (two orders of magnitude above any other)
was driven 6,000 ticks through a save-slot menu, a play screen, movement across
two areas and an NPC dialogue with no fault or corruption in any sampled frame;
its arena climbs through four collection cycles and then holds flat for the
last 2,500 ticks of play, the shape a working free produces. Its calloc and
free counts do not balance during play — 63,597 against 52,375 by tick 6,000 —
but the gap was 12,509 at tick 3,500 and 11,222 at tick 6,000, so it is a held
working set (visible sprites, an open dialogue, background tiles). An
unbalanced count is not the signal; a growing gap is.

**The detector (`arena_shadow.go`).** Waiting for a symptom means waiting for a
random wrong sprite or a fault at an address the game never computed. Instead,
a released block is copied to the Host side and compared against guest memory
when the arena hands those bytes out again. A difference is a write after the
release, reported as `arena use after free` with the block, the offset and the
bytes found — enough to place a watchpoint and catch the writer on the next run.

The first two versions filled the released block with a byte no guest value
looks like and checked the fill on reuse. That found the same writes but
destroyed the block's contents, which neither a handset's allocator nor this
arena in a release build does. A title that frees a structure and keeps reading
it then ran in release and died in debug on a pointer made of the fill
(`0xdfdfdfdf` or an offset from it). Two local titles do this — one A-grade
title runs its whole route with the copy and stops after 160 ticks with the
fill — and the server the archives are played on is a debug build, so those
reports named the instrument. Reading a freed block remains a title's own
defect, now invisible here as it is on the handset.

The copy grows to what the arena has handed out, not to the 64MB it could hand
out, since a span reserved up front would be the largest thing a debug session
holds. It belongs to `guestArena` rather than `MC_knlFree` because:

- **The check must know which bytes were marked.** A free-list block is reuse;
  so is a cursor allocation below the high-water mark, because a release at the
  top of the arena returns to the cursor. Space past the high-water mark was
  never marked and is not checked.
- **The object collector releases into the same arena**; a mark applied to guest
  frees alone would read every collected object as a use after free.
- **A freed block keeps its contents, so the collector reads them.** Old
  pointers in a freed block can keep dead objects alive until reuse. That is
  over-retention, the safe direction, and what a release build always did.

Both halves walk every released and reused block, so they are installed in
debug builds only. Coverage is reported with the findings —
`arena blocks recorded on release` and `arena blocks checked on reuse` — because
a clean report means nothing unless the detector was running.

**Results.** All 32 archives booted for 400 ticks: no fault and no false
positive from platform paths. The heaviest free-user driven 5,545 ticks into
play and movement: 166,396 guest frees, 166,399 arena releases, 185,969 blocks
handed out again and checked, no marked byte touched. Three more titles add
30,481 marks and 34,976 checks, also clean; two of them free almost nothing in a
boot window and tens of thousands of times in play, so **a boot window does not
rank titles by how hard they use free**. The question stays open in the same
shape: a title that keeps a pointer past a free will be reported the first time
it does so, and none has.

<a id="implementation-what-the-whole-local-set-does-with-free"></a>

#### What the whole local set does with free

All 32 local KTF archives were booted for 400 ticks with no frames, counting
calls to `MC_knlFree` and frees of identifiers this platform never issued.

**No title frees an identifier this platform did not issue** (zero on all 32):
across the whole set, the word a KTF title passes free is the word calloc
returned, not the payload pointer twelve bytes in.

Free is used by 13 of the 32 in that window, with a two-order spread: one title
calls it 21,147 times in 400 ticks, the next-heaviest 116, the median user a
handful.

- **A boot window is not a title's whole behavior.** The other 19 free nothing
  before their title screen; a title moving from zero to non-zero when run
  deeper is expected, not a regression.
- **Free count does not predict risk; the arena fills by bytes.** The title
  that died made about 16 allocations a tick; the heaviest free-user makes four
  times that and was never seen to die, so its blocks cannot average near the
  first title's 2.3 KB, or it would have reached the ceiling inside 450 ticks.
- **More frees than callocs is not a fault** (two titles). Freeing null is legal
  and counted, and some platform calls return a buffer the caller frees —
  `MC_grpEncodeImage` returns one.

**An imbalance during boot is retention, not a leak.** Every title that frees
holds more than it releases in this window, because booting allocates assets
and keeps them; the heaviest holds about 5,700 blocks by tick 400. For a future
`tick_error` naming the arena, the discriminator is whether `wipic 0x15` and
`wipic 0x16` stay balanced in steady state after boot. Balanced and still dying
is the platform's problem; diverging steadily during play is the game holding
memory, which no work on free will change.

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

<a id="implementation-the-same-lie-one-table-over-the-graphics-destroys"></a>

#### The same lie, one table over: the graphics destroys

`MC_grpDestroyOffScreenFrameBuffer` and `MC_grpDestroyImage` were accepted
no-ops on the same "the arena is reclaimed with the client" reasoning, and
outlived the fix to free because no local title had been driven far enough into
play to fail on them.

A newly added title draws each frame into an off-screen buffer it creates and
destroys — 49,109 creates against 49,109 destroys over 6,100 ticks — and each
create is two arena blocks: a 240x320 pixel allocation and its twenty-byte
record. The arena climbed a flat ~264 KB between collection cycles from the
first field screen and the run died at tick 26,332 (about six minutes of play)
with a black screen:

```
"tick_error": "service KTF timer at 0x110031: handle supervisor call 0x3 at 0x35000ed8:
               allocate KTF pixel buffer: KTF platform initialization data space exhausted"
```

A user would read it as the game freezing: there is no message, the last frame
stays, and the failure surfaces inside a timer service.

- **The collector's cost is the second symptom, and it shows first.** A cycle
  scans every committed word: 700 µs at the first field screen, 16 ms at 17 MB,
  94 ms at 59 MB. Long before the ceiling the game loses frames to collections
  that find nothing ("it gets choppier the longer you play").
- **A leaked destroy does not show in `wipic 0x15`/`0x16`.** These blocks never
  pass the kernel table: the create is `wipic 0x30004` and the destroy
  `0x30003`. Pairing the graphics create/destroy slots is the same balance test
  and is what named this one.

Three allocations are now released:

- **The off-screen framebuffer**: the record and its pixel allocation. The call
  does nothing when handed the screen framebuffer, as the specification says;
  this platform hands out one cached screen record, so freeing it would take the
  screen from every later caller.
- **The image**: the record, its decoded pixels, and the framebuffer record
  `MC_grpCreateImage` builds and copies word for word into the image record.
  That inner record is dead once copied; it leaked thirty-two bytes per image.
- **The image's encoded source buffer.** `MC_grpCreateImage`'s contract is that
  the buffer "is released inside the image function", which is why the
  specification requires an `MC_knlCalloc` identifier. The title callocs a
  buffer per image and frees none; its outstanding allocation count tracked its
  image count exactly. Freeing at destroy rather than at create covers the
  animated case the specification splits out, which this platform does not
  decode.

A handle the platform never issued is ignored, as `MC_knlFree` does (a double
destroy arrives as a block already gone), and ignored silently, because Java
arrays reach the image calls as handles too and counting them would bury the
diagnostic for a real stray.

This was KTF-only: LGT's `destroyOffscreen` and `destroyImage` slots both call
`releaseSurface`, and SKT has no guest arena (an SKVM image is a Go object
collected by the shared JVM).

The same 6,100-tick route now peaks at 1.1 MB of arena with three collection
cycles, against 17.5 MB and fifty-two before. First frames of all 33 local KTF
archives are unchanged except the six that differ from the baseline run against
itself — the noise floor of this comparison (see
[An A/B sweep of the local set is half noise](#implementation-an-ab-sweep-of-the-local-set-is-half-noise)).

<a id="implementation-the-kernel-table-beyond-the-specification"></a>

#### The kernel table beyond the specification

The kernel interface is a flat table of 65 entries in the WIPI 1.2.1 `MC_knl*`
order with three extra slots inserted after `MC_knlSprintk`; every entry from
`MC_knlExit` onwards sits two above the specification's number, which is what
makes `0x7`, `0xe`, `0x14`-`0x1d`, `0x1f` and `0x20` line up. Two entries past
the specification's end are implemented from what titles do with them.

**`0xd` is `MC_knlGetAccessLevel`,** a bitmask of the API groups the program
may use. **Every group is granted**, because the question is what the handset
permits, not what this platform does about a particular call. It used to
withhold the network and serial bits so that a game checking the bit would skip
the attempt. One title reads the mask during startup, requires `& 0xbc` to
equal `0xbc`, and otherwise stops at its own error screen
(`인증오류 ... (오류번호:1001)`) without reaching any network call, so
withholding the bit only ended the game. Granting every group was measured
title by title across the local library: same frames and no new network calls.
The refusal still happens at the call; `docs/network.md` carries that decision.

**`0x24` is `MC_knlGetDLLInterface(name, major, minor, *rtnMajor, *rtnMinor)`,**
which answers a handset library by name. An unknown name is answered with null,
which is documented and handled by the caller's error path; fabricating a table
would be a surface with no contract behind it.

The one library a local title asks for is a **user-space memory manager**
(`wipic_usermem.go`). It can be served because the game supplies a buffer from
its own image and the block sizes, so serving it is bookkeeping, not inventing
an allocator's address space. Blocks are cut from the front and never returned:
the caller allocates its fixed set once at startup and holds it.

The call site is not a stub table: the game loads the interface pointer,
indexes it and `bx`es through a veneer, so the slot number never appears in the
trace. The library was found by taking the link register from the failing
call, disassembling there and reading the indexed offset.

<a id="implementation-aot-outer-calls-and-runtime-java-bridge"></a>

### AOT outer calls and runtime Java bridge

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

Runtime-owned Java classes publish complete KTF metadata: a guest vtable
spanning the hierarchy with each method record's slot, method and field counts,
and field records. Static fields keep their value word inline in the field
record, as in the original ABI. Object headers encode a dispatch alias — a
bounded copy of the class record in the platform arena — as a shifted offset
from the JVM context; real clients read the alias's vtable pointer for virtual
dispatch and its descriptor pointer for array store checks, both pinned by
disassembling a client and dumping its reads. `RegisterClass` runs the class's
own `<clinit>` once, since guest statics live in field records and otherwise
stay zero.

The guest's `instanceof`/`checkcast` primitive walks the guest's own class
records rather than the Host's registry, because a game asks about classes long
before registering them; a hierarchy with an unregistered middle class would
otherwise be undecidable, and an undecidable answer stays permissive. The class
descriptor carries the implements list — a pointer table at offset 16 whose
length is the count at offset 30 — so an interface target is decided from the
interfaces each class in the chain declares, including those they extend. Only
a walk that runs out of superclasses answers no; a read failure keeps the
permissive answer and records why. A wrong yes sends the guest down a branch
written for another type, which is how a map's entities stopped acting while
the game still drew and took input.

The runtime Java surface now covers the boundaries real constructors and early
`startApp` bodies cross: `java/lang/String` with the original guest field
layout (`value`/`offset`/`count` backed by a real `[C` array), `StringBuffer`,
`System` with a `System.out` that logs whole lines beside `printk`, `Thread`
construction and queries, `Math`, `Integer`, `Random`, `Class.getName`/`getResourceAsStream` over attached archive
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

<a id="implementation-a-class-left-out-of-that-table-still-resolves-and-answers-nothing"></a>

#### A class left out of that table still resolves, and answers nothing

`ensureJavaClass` does not fail on an unknown platform class name: the fallback
allocates a real class record with a name, no super and an empty method table.
The gap surfaces only when the guest asks that class for a method, as
`method … was not found from class …` at the first call.

`java/lang/Integer` was missing from the table — the JVM had `parseInt` since
the CLDC builtins were written, but nothing published it to guest code — and a
title errored out when its quest window opened, several menus into a save,
because that screen is the first to parse a number from a string.

The published classes are therefore a separate promise from what the JVM
implements: a method published without its own implementation promises that
the JVM owns a body for it. `TestDelegatingRuntimeMethodsHaveAJVMBody` checks
that promise by asking the VM for a body for every delegating method at its
published descriptor. It cannot see a class nobody published: `Integer` now
publishes every method the JVM implements for it, but `Long`, `Character` and
`Boolean` had no implementation anywhere and were never observed being called,
so they stayed unwritten rather than guessed at; a title that wants one fails
with the same message.

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
and the mount is the game's own package — so dropping only the in-memory entry
leaves the file resolving from the save underneath. `unlink`, `remove` and the
old name of a `rename` go on the list; a write to the path takes it off;
`exists` and `list` consult it. No KTF title was seen to delete and re-ask; the
correction came from LGT, where two titles lost their opening sequence after
deleting a save slot and being told it still existed. See
[`lgt.md`](lgt.md#implementation-the-lgt-platform).
WIPI C databases seed their initial content from a mounted file with the
database's name, matching packaged save data.

<a id="implementation-the-two-storage-tables"></a>

#### The two storage tables

A game uses either. **Table 7 is the filesystem** (`wipic_filesystem.go`): one
blob per name, read and written through a cursor, seeded from a packaged file
of the same name. **Table 5 is the record database**
(`wipic_record_database.go`): a numbered set of records, opened by name with a
record size, addressed by a one-based id.

Both follow the specification's order. Table 5 is the `MC_db*` list — open,
close, delete, insert, select, update, delete-record, list-records, then the
count, the record size and the database list at 10, 11 and 12. Table 7 is the
`MC_fs*` list: open, read, write, close, seek at 0 to 4, the file attributes at
5, remove at 6, `MC_fsMkDir` at 8, `MC_fsList` at 10, total space at 11, free
space at 12, `MC_fsTell` at 15 and `MC_fsIsExist` at 16. Table 7's Go names
said "database" while it was read as one and say file now; the sweep section
below has how the two were told apart. **Two names keep the old spelling on
purpose**: save keys still begin `db/`, because renaming would orphan every
existing save, and diagnostic counters still read `cdb`, because stored reports
are read against those names.

**Table 7's slot 6 is `MC_fsRemove(name, mode)`** — a name, not a handle; the
caller closes the file first. The save store has no delete, so the name goes on
a removal list (`db/.removed`) that every lookup consults, as the guest
filesystem does. Opening the name for creation takes it off again; otherwise a
title that deleted a file and wrote a new one would have every later write
hidden. One title deletes the certificate it could not renew and expects the
next question to answer "no such file".

**`MC_fsMkDir` only records its name.** A name is a key in one flat store, so
making a directory only decides the next answer about it: zero once, then
`M_E_EXIST`, across restarts, from `db/.dirs`.

Two record-table details are silent when wrong:

- **The packaged file carries a `.db` suffix the database name does not.** A
  game opens `NAME_DL`; the archive ships `P/NAME_DL.db`. Both spellings are
  tried on open. The packaged format is a 45-byte header — magic `qtcdb`, record
  size at offset 8 — then one slot per record, each a live flag and the
  record's bytes.
- **Ids are positions and a delete leaves its slot.** Reusing a freed id would
  rename a record the game holds an id for, so a deleted record becomes an empty
  slot and the next insert takes a new id past the end.

Slot 6 carries two call shapes with one signature: `delete_record(handle, id)`
and a name-keyed `delete_database(name, flags)`. Only a handle this platform
issued is read as the first form.

**A stub was not neutral.** Before table 5 existed it answered zero, which a
loader reads as failure before continuing with its zeroed buffer. One title
keeps its cipher keys in a packaged record database and decrypts its settings
with them: every setting decoded to zero, and its frame interval, chosen from a
table by that zero, became the slowest entry. It asked to be woken every 500ms
and ran at two frames a second with nothing failing and the emulator 90% idle.
Implementing the table took it to 30 rounds a second. A storage call that
answers "no" is a value the game will believe.

The WIPI C surface implements the startup and early-lifecycle kernel calls
(indirect allocation, memory queries, current time, system properties, program
name, `sprintk` formatting, resource lookup and reads, timer registration), the
graphics table (display info, screen and offscreen framebuffers, graphics
contexts, pixel conversion, fill/line/copy, encoded-image decoding and drawing,
font metrics), the KTF database stream extension in process memory, and
accepted no-ops for media, misc, and the original runtime's stub tables.
Registered timers were then recorded for a future lifecycle event loop and LCD
flush was accepted without Host presentation.

After startApp the Host drives three client services: `ServiceTimers` runs
registered WIPI C timer callbacks; `ServiceThreads` runs queued guest Java
threads (`Thread.start` queues cooperatively rather than spawning a goroutine,
since guest threads share the single ARM core); and `ServicePaint` calls the top
pushed card's `paint` with a Graphics whose color, clip, rectangle, line and
image operations draw into the WIPI C screen framebuffer. `MC_grpFlushLcd` and
`ServicePaint` convert the RGB565 screen buffer to the RGBA frame exposed by
`Client.Frame`.

The lifecycle probes are:

```sh
WFEATURE_KTF_CONSTRUCT_ACCEPTANCE=1 go test -run TestLocalKTFArchivesConstructMainClass -v ./internal/platform/ktf
WFEATURE_KTF_START_ACCEPTANCE=1 go test -run TestLocalKTFArchivesStartMainClass -v ./internal/platform/ktf
WFEATURE_KTF_FRAME_ACCEPTANCE=1 go test -run TestLocalKTFArchivesRenderFirstFrame -v ./internal/platform/ktf
```

All 32 local archives complete their no-argument main-class constructor and
return normally from `startApp([Ljava/lang/String;)V`. The startApp probe allows
fifty million instructions, since real games spend tens of millions
decompressing tables first; `WFEATURE_KTF_MAX_STEPS` overrides it. **All 32
render a real first frame**, including recognizable publisher splash screens;
`WFEATURE_KTF_FRAME_DIR` saves each frame as PNG, `WFEATURE_KTF_ONLY` filters
archives by substring, and `WFEATURE_KTF_FRAME_ROUNDS` overrides the 64 service
rounds (one title's loading timer needs about 100 callbacks before its splash).
Guest waits run on the session clock (see [Pacing](#implementation-pacing)), and
every `Thread.start` queues for the cooperative service loop through the JVM's
GuestThreadStarter hook. Text uses the shared runtime glyphs from
`internal/glyph`: authored 5x7 patterns for ASCII on the large face, and an
embedded pixel face (Hangul first among the rest), thresholded to one bit and
cached per rune.
The descriptor selects the face. `__adf__` declares the screen the game was
built for — `DisplaySize:176*220` or `DisplaySize:240*320`, about evenly split
in the local library — and each handset generation carried its own Korean face:
12 dots on 176x220 phones, 16 on 240x320. **There are two embedded faces, one
per size, rather than one scaled**: NeoDGM (`fonts/neodgm.ttf`, OFL 1.1 — the
face the original handsets render with) is drawn on a 16-unit em and exact at
16; Galmuri9 (`fonts/galmuri9.ttf`, OFL 1.1) is exact at 10, where its ink
stands as tall as NeoDGM's scaled to 12. A pixel font is only itself on its own
grid. The `fonts` package states that Galmuri is a modern face whose shapes come
from the Nintendo DS rather than a Korean handset, chosen as the small Korean
face on hand that is exact at its design size and redistributable; 176x220
screenshots are therefore not comparable against a reference the way 240x320
ones are.

`descriptorFace` reads the declaration once at startup and the runtime holds
the face for the run, because a game derives its layout from font metrics: a
176x220 title told a line is 16 pixels tall rather than its handset's 12 sizes
its menus too large and draws text that does not fit them. Metrics follow the
chosen face — 16 tall with a 12-pixel baseline, or 11 tall with an 8-pixel one
— and `MC_grpGetStringWidth`, `Font.stringWidth` and `Font.charWidth` measure
with the renderers' per-glyph advances. A descriptor with no legible size keeps
the 16-dot face, as do platforms with no descriptor.

Text is drawn by coverage: `Graphics.drawString` and `MC_grpDrawString` mix the
colour into the framebuffer by how much of each pixel the outline covers, in the
framebuffer's 5/6/5 space.

**A NUL in a string is padding, not a character.** Titles of this era keep
dialogue in fixed-length buffers and pass the whole buffer to drawString, so
lines end in NULs. A character without a glyph is drawn as a codepoint-marked
box, which for padding gives a run of boxes after every line and a
`stringWidth` wide enough to centre the line off screen. The faces disagreed —
the 16-dot font carries a blank glyph at zero and the handset font does not —
so the same string drew clean on one screen size and broken on the other. Zero
now draws and measures nothing on both; every other control character keeps its
box. No local title draws one in a boot window: all 261 KTF archives driven 600
ticks at eight times speed, with both text paths reporting any string carrying a
NUL, produced none (the reporting title pads dialogue several screens in, past
a gap in the input-method surface). The change is justified by the two faces
answering differently, not by a measured corpus effect.

**Coverage is no longer needed by either face.** A face on its own grid covers
whole pixels, and a fully covered pixel writes the colour untouched, as bit
plotting did. Coverage made a scaled face bearable: NeoDGM at 12 pixels is three
quarters of its grid, so a two-unit stroke covers a pixel and a half; rounding
makes some strokes a pixel too heavy, fills the counters of `복` and `했`, and
reads bolder and muddier than the font. Blending softened that; an exact face
for the small screens fixed it. The coverage path stays for a face asked for a
size its design cannot hit.

Three boundaries took the first-frame probe from 15 of the eighteen archives
then present to all of them:

- `Card.repaint`/`Card.serviceRepaints` were accepted no-ops, so a game pairing
  them in its own thread loop never painted. They now follow MIDP: repaint sets a
  dirty flag, and serviceRepaints synchronously paints the receiver card into
  the screen framebuffer and presents it.
- Mutable images (`Image.createImage(II)`) are backed by a guest offscreen
  framebuffer, start white per MIDP, and `Image.getGraphics` draws into it
  through the shared WIPI C pixel path; `Graphics.drawImage` blits
  framebuffer-backed images row by row. Double-buffered games, which composed
  frames into an unreadable image, became visible.
- One title identifies itself by a program name baked into `client.bin`
  ("010100D5") rather than its ADF AID; `ProgramNameForAID` carries the original
  runtime's override table. Its remaining crash was database semantics:
  `MC_dbExists` must answer 0 on success and M_E_NOENT (-12) when missing —
  answering 0 for "missing" made the title parse an empty save and dereference
  null. Database slot 5 is likewise the KTF-specific `db_stat_by_name` (name,
  int32[3] out, mode), not UpdateRecord.

One local title needed two more slots to get past its save file. Database slot
15 is a KTF extension the original emulator also leaves unimplemented, so its
meaning comes from the one caller: the save loader opens the database, seeks to
the end for the size and, when non-zero, calls slot 15 with the handle alone
before seeking back to the start. The disassembly reaches it through the `bx r2`
trampoline with only `r0` set and stores the result in a scratch local the next
seek overwrites; the cursor is reset immediately after, so any positioning is
unobservable. It validates the open stream and answers zero, counting a
`cdb slot15 <name>` diagnostic so a caller with a different shape is visible.
The other was `MC_grpDrawRect` (graphics 10), which outlines the box
`MC_grpFillRect` fills; a rectangle too thin to have an interior is drawn as the
solid line it describes, because four overlapping edges would plot pixels twice.

**Graphics 25 is `MC_grpRepaint(lcd, x, y, w, h)`,** which queues a repaint: the
specification says it puts an event in the queue so the paint callback runs
later. The region is discarded because a card is repainted whole.

<a id="implementation-a-packaged-database-is-packaged-in-two-shapes-and-the-commoner-one-was-unreadable"></a>

#### A packaged database is packaged in two shapes, and the commoner one was unreadable

The single-file shape described above (`P/NAME_DL.db`) is true of two archives
in the local set. **Twenty-one others package the database differently**, and
every one of those databases was missing at run time.

Both shapes begin with the same 45-byte header — a five-byte magic, then the
record size and record count as **big-endian** words at offsets 5 and 9. They
differ in where the records are:

| magic | files | the records |
|---|---|---|
| `qtcdb` | `NAME.db` alone | after the header, one slot each: a live flag then the record's bytes |
| `qtpdb` | `NAME.idx` and `NAME.db` | the whole of `NAME.db`, end to end, with no flag between them |

The split shape is the one a handset writes. Its index carries only the header,
so the data file is exactly `record size × record count` bytes; that holds for
all 43 packaged databases in the local set, at record sizes from 5 to 11,255
bytes, which is what confirms the reading.

Each of these is silent when wrong:

- **A count of zero is a database, not a missing one.** Two local indexes
  declare no record, and one has no data file at all. Answering "no such
  database" sends a title down its first-run path on every launch — also when a
  stale data file the index does not describe sits beside it, which is ignored.
- **The header's byte order only shows above 255.** The record size used to be
  read as a little-endian word one field further on, which agrees below 256 and
  differs above; one local database declares 328.
- **The data file settles a record size its index disagrees with.** One local
  index has the last byte of its magic and the top byte of its record size both
  overwritten with `0xff`. The rest is intact and its 1,400-byte data file
  divides evenly by its two records, so the division is taken and the magic is
  matched on four bytes. **An intact header that disagrees with its data file
  is refused** as a stale file; with a single record the divide-evenly test
  could never reject, so any file would otherwise be read as the one record.

<a id="implementation-the-java-database-never-looked-at-the-archive-and-that-is-a-download-gate"></a>

#### The Java `DataBase` never looked at the archive, and that is a download gate

The WIPI C table always consulted packaged databases. The Java class
`org.kwis.msp.db.DataBase` looked only in the save store, so
`openDataBase(name, size, false)` threw `DataBaseException` for every database
an archive ships. Titles use that throw to detect a first run, and one local
title answers it by offering to download data it already carries:

```
KTF guest printk  text="11-----Save Error"
jdb absent op_save
```

behind a full-screen `본 게임을 즐기기 위해서는 추가 파일의 다운로드…가 필요합니다`
prompt, over a phone line that has not existed for fifteen years. The archive
holds `P/op_save.idx` and `P/op_save.db`: a one-record, 5-byte database written
on the handset this copy came from. With it found, the title goes splash →
title → main menu → slot select → prologue and into its world.

**This is the fourth download gate in this file and the first that opens.** The
other three — a subscriber number's length, a `prefs` record nothing here can
decrypt, and twelve resource containers the archive does not have — are further
down. Here nothing was missing: the data was in the archive, in a format the
platform already parsed, behind an API that never called the parser.

**A save still wins over the packaged copy** on both tables: a game that has
written since owns what it wrote. Opening a packaged database writes nothing,
because the empty-record write that makes a created database exist for the
next session would shadow the archive's copy.

**A save holding no record still wins; the opposite rule was tried and
withdrawn.** The previous release told these titles their database was absent,
and a title that then created one left an empty record list, so treating an
empty save as no save would deliver the archive's copy to players who had
launched once (four local titles). It would also resurrect the save of a player
who deleted and recreated it, which leaves the identical shape. Telling them
apart would need a third list per table, and resurrecting a cleared save is
worse than asking for one more clear. **Clearing the game's save adopts the
packaged copy**, documented in the release notes. None of the three titles the
prompt was blocking needs it: two left no save, and the third has real records
no rule should overwrite.

**Each table reserves its own bookkeeping names on its own scope.** A key is a
table's scope joined to the name, so a database or file named `.removed`
addressed that table's deletion list (all four tables keep one) and `.dirs` the
WIPI C file table's directory list; the last write won, and a list overwritten
by records read back as deleted names and hid databases nobody deleted. Moving
the bookkeeping would orphan existing lists (the reason these keys still spell
`db`), so the names are refused on all four tables:

- against the normalised key, not the raw name;
- per scope — a guest file named `.dirs` collides with nothing, because the
  directory list belongs to the neighbouring table;
- on every path that writes a key: rename and delete as well as open. Emptying
  is the sharpest case: remove writes nil over the key and rename empties its
  source, and the deletion list answers as a seed once anything was deleted, so
  either would have brought back every database the title had deleted. A rename
  is refused at both ends — onto the name it replaces the list, away from it it
  carries the list off;
- for the guest `File` surface, where a File is constructed rather than where it
  is written, so a title is never told a write worked while nothing is stored;
  its rename and unlink refuse at either end.

**A name must survive normalisation as a name.** `NormalizeSaveKey` refuses
`..` and collapses `.`, `/` and `./`, so those passed the check and the store
wrote the scope itself: a regular file named `jdb` where the directory belongs,
after which every write under that scope (the removal list included) failed
with "not a directory" across sessions. A name must normalise to the scope plus
a name under it, and must read back from the list as itself: a name with a
newline is refused because deleting it would hide the two databases its halves
spell, and deleting `save ` would hide `save`.

**The 31-byte length cap stays on the WIPI C record database**, whose
specification defines it. The Java class never had one: eleven Korean
characters are thirty-three bytes, so applying it there would refuse names that
class always took and orphan their saves.

**A table's own list hides that table's own save; the archive is hidden by
either list.** Sharing the answer for saves let a C delete hide an unrelated
Java save of the same name, and nothing on the Java side took names off the C
list, so that save was lost. Equally, a Java delete must hide the archive's copy
from the C table, or the cleared record returns through the other door.

**Writing a database takes it off the deletion list**, as writing a guest file
does. A Java `DataBase` object kept across its own delete still writes through,
and those records would otherwise land under a key the list hides for ever. The
delete empties that object's records first, so such a write stores the empty
database the title asked for.

**A database created through the WIPI C table is persisted.** The Java table
always made a created database exist for the next session; the C table did not,
so a title that created one without writing a record got `M_E_NOENT` later. The
cost: the empty save appears on first boot and wins over the archive, so for a
title whose packaged index this parser refuses (a stale data file beside an
intact header), that boot makes the refusal permanent where a later parser fix
could otherwise have surfaced the records. Clearing the game's save is the way
back.

**A WIPI C delete closes the database's handles**, as the file table always did.
A handle kept across the delete still holds the records, and a write through it
would persist them and take the name off the list, restoring the deleted
database whole. The Java class cannot be closed that way because the object is
the guest's, so the two paths differ.

**`listDataBases` still names only what this session opened.** It never named a
database that exists only as a save, because `backend.SaveStore` has `LoadSave`
and `StoreSave` and no enumeration. Naming only the packaged ones would trade
one inconsistency for a worse one, and a listing interface reaches both Hosts.
No local title was seen calling it; a title stopping because its database is
missing from that list would justify the change.

**`DataBase.deleteDataBase` reaches saved and packaged databases.** It looked
only at databases this session had opened, so a title deleting before opening
was told its save did not exist, and a packaged copy would return next run. It
now answers like the C table's name form: a name the save store or archive
holds is deleted; only a name neither holds throws.

**A deleted database is gone, not empty.** Both tables used to delete by
emptying the save, which hid the packaged copy but made the next open answer
"exists and holds nothing", so a title that cleared its slot for a new game was
told its save was still there — the shape that cost LGT two titles' opening
sequences (`guestFileRemovedKey` in `runtime_file.go`). The two storage tables
now keep removal lists (`rdb/.removed`, `jdb/.removed`): a listed name hides
its emptied save and the packaged copy, opening for creation takes it off, and
the save is still emptied so a tree written before the lists existed keeps
hiding the archive's copy.

**Across the local set:** of the 264 KTF 1.2 archives, 20 open a packaged
database in their first 64 ticks that they were previously told was absent, and
no other archive's first frame changed.

<a id="implementation-a-published-instance-field-has-two-storages-and-the-boundary-is-where-they-agree"></a>

### A published instance field has two storages, and the boundary is where they agree

Most of a runtime-owned class's state never reaches guest memory: the guest
calls a method and the Go side answers. A **published** instance field is the
exception — declared in the class's KTF metadata at an offset guest code
compiles into a load. Four classes publish any, each because something reads
the field instead of calling for it: `java/lang/String` (the client's helpers
read `value`, `offset` and `count` instead of calling `charAt`),
`org/kwis/msp/lcdui/Card` (a title reads its canvas geometry instead of calling
`getWidth`), and `java/io/ByteArrayOutputStream` and
`java/io/ByteArrayInputStream`, whose `buf` a title reads instead of copying the
array out — the sink when handing the bytes straight on, the source when
decoding straight from the array it streams.

An array makes guest memory its only storage, and a static field keeps its
value word in its field record; a published instance field can do neither,
because its Go side is a Go value (a string's characters are a Go `string`).
The two storages are made to agree at the boundary the guest crosses, where a
runtime call already reads its arguments (`field_sync.go`).

**Out: the constructor decides a published field, and it runs after the payload
exists.** A guest `new` allocates the instance with a payload describing it as
allocated — for a string, `value` is null — and the Go-side constructor never
updated it. One local title builds every string it parses this way, 227 before
its title screen, each handing the guest a null character array; the other
nineteen archives build none. Publishing runs after methods the class names as
mutators, not after every call: `String.charAt` is the most-called runtime
method in the local archives (16,525 calls), and two guest reads apiece to
re-check an immutable string has no case behind it. A publish that finds the
payload already matching writes nothing, so a constructor called twice does not
strand a second array.

**In: what the guest wrote is reported, not repaired.** Between two runtime
calls only the guest runs, so a payload that no longer matches its Go value was
written by the guest. That counts a `field divergence <class>` diagnostic and
changes nothing. The counter is zero across the twenty local archives, and a
repair with no observed case would be a guess about which side is right. The
detector runs in debug builds only.

`TestFieldSyncTableCoversPublishedInstanceFields` makes this the mechanism: a
class that gains a published instance field without a sync rule fails there.

**Publishing one field is not publishing the class.** The sink publishes `buf`
and not `count`, which nothing in the local set reads; a title that reaches for
`count` stops with the field's name in the message, better than a word no
mutator maintains answering a stale number. `buf` changes only when its array
is replaced, so the sink's mutators are the constructor, a write that outgrows
the array, and a reset.

<a id="implementation-the-sources-cursors-are-the-field-its-window-closed-on"></a>

#### The source's cursors are the field its window closed on

The source likewise published only `buf`; `pos`, `count` and `mark` were left
out because no title had been seen reading them. A report from outside the
local set named a title that does, which stops before its first frame. **The
mechanism is checkable without the archive**: a title compiled against a
handset's library resolves each protected field by name against the record
this runtime publishes, so a missing name is a failed link, not a stale number
— an undeclared field does not read zero. No local archive exercises it (the
nearest starts and plays either way, its A/B under its own noise floor), so the
change rests on the report, that mechanism, and a sweep showing nothing else
moved.

The source now declares all four in the library's order — `buf`, `pos`,
`count`, `mark`, instance size 16 rather than 4 — and publishes them together.
Every cursor-moving call is a mutator: the two constructors set all three, and
read, skip, mark and reset each move one. Because that list is hotter than the
sink's, the four words are read as one block and written only when something
changed: a `read()` moving `pos` by one costs one sixteen-byte guest read and
one write.

**Over all 264 local archives**, 400 ticks each on both builds: no change. Every
summary agrees once the guest's own timestamped log lines are set aside (a
naive comparison flags 56 of 264), and of the 79 archives that reach a first
picture in 400 ticks, no frame differs. A stream the guest never holds is never
published to.

**The reserved size is the other half.** `guestReservesRuntimeBlock` refuses to
publish into a guest subclass whose fields start inside the block this runtime
describes, now 16 bytes. A subclass compiled against a real handset's library
puts its first field after the same four words; one whose fields land inside
was compiled against something else, and its payload is not this runtime's to
write.

**A static field's initializer runs after its class is registered.**
`java/lang/Boolean.TRUE` is an instance of the class whose record is being
built, so resolving it re-entered the builder for a class that did not exist
yet. Field records are written with their offset word and patched after
registration, which also keeps the guest's `TRUE` and the core library's the
same object — a title compares a boxed flag with a pointer compare.

<a id="implementation-host-integration"></a>

### Host integration

`ktf.Session` composes the whole sequence Hosts share: open archive, load
client, set the program name, attach resources and filesystem, execute the
entry and initialization functions, construct the ADF main class, and call
`startApp`. `Session.Tick` runs one cooperative service round (timers, one
queued thread slice plus the workers that finish, card paint), `Session.Frame`
exposes the last flushed RGBA frame, and `Session.SendKey` dispatches WIPI key
events (`type` 1/2/3 for press/release/repeat; codes -1..-16 for navigation and
soft keys, ASCII for digits, star, and hash) to the pushed cards from top to
bottom, propagating while a card's `keyNotify` returns true. A guest `MC_knlExit`
surfaces as `ktf.ErrGuestExited`, which Hosts treat as a clean game end.

Guest Java threads run on worker goroutines with private ARM stacks (mapped
from `0x20100000`, one `ThreadStackSize` apart, reused after a thread ends)
and hand execution off strictly: `ServiceThreads` grants one step slice
(default 2M, `SessionOptions.ThreadSliceSteps`) and blocks until the worker
parks, finishes, or fails. Parking freezes the worker's whole nested guest
call stack via the armcore step-budget hook, so an endless main loop keeps
progressing tick after tick; `Thread.sleep` also ends the slice, and the worker
stays out of the queue until the sleep has elapsed (see [Pacing](#implementation-pacing)).
The slice limit counts only workers **still running**; a grant ending with the
worker returned from `run()` is not charged, bounded by steps (see
[the fifteenth round](#implementation-the-fifteenth-round-two-host-ceilings-a-title-reached-by-playing-normally)).
`StartSessionAsync`/`PendingSession.Pump` run the long startApp the same way:
each pump grants an 8M-step startup slice, keeping a browser event loop alive
during initialization.

A Host service call — a timer round, a key delivery, a paint — runs guest code
on the client thread under a renewable budget (`beginHostService`). Reaching
the step ceiling is not a failure: the limit hook grants a fresh window while
the Host's context is live and the call has allowance left
(`SessionOptions.ServiceSteps`, default 500M), and fails with
`ktf.ErrServiceStepLimit` afterwards. Guest code legitimately spends far more
than one window in a single call — a title reads a save plus its enemy, drop and
map tables inside the `keyNotify` that picks a save slot, well past the 50M
window — and failing that call looks like a crash. The allowance ends a call
that will never return; cancelling the context is the Host's abort path and the
only one that works mid-call, because while the client thread runs no worker,
timer or paint runs, so a guest waiting there for another thread waits forever.
The native CLI wires SIGINT to it through `signal.NotifyContext`.

**A window the guest spent waiting is charged to the clock.** A title that
paces its opening by polling `MC_knlCurrentTime` in a delay loop spends hundreds
of millions of steps passing time. A window in which the guest read the clock,
on a clock that moved under it, renews without touching `ServiceSteps`; the
call is bounded by `SessionOptions.ServiceWait` (default 15s), failing with
`ktf.ErrServiceWaitLimit`. A batch Host holds the session clock still during a
service call, so there the loop still ends on the step allowance in seconds.
The title and measurement are in
[the fifteenth round](#implementation-the-fifteenth-round-two-host-ceilings-a-title-reached-by-playing-normally).

<a id="implementation-a-bmp-declares-its-transparent-colour-in-the-header"></a>

### A BMP declares its transparent colour in the header

One local title ships its art as 8-bit BMPs — 1,301 of them, despite `.png`
and `.dat` extensions — and BMP has no alpha channel. Transparency is declared
in two spare fields: **`bfReserved1` is 1, and `biClrImportant` is the
transparent palette index**. Both platforms decode BMP through
`wipic.DecodeBitmap`, which clears that palette entry's alpha, so every draw
path reads the transparency as it would an alpha channel.

Without it the title drew every sprite — title character, menu portraits, map
tiles — in a solid green box, and the field looked like flat green ground
because the tile layer's backdrop was painted.

Checked against the whole set: all 1,259 images that set `bfReserved1` name an
index holding the same backdrop green; the 42 that do not are backgrounds;
`map/tile` sets it on all eleven tiles and `map/back` leaves it clear on nine of
eleven. Two other local titles carry one such BMP each, naming a pink instead —
**the colour belongs to the title, the mechanism to the format** — so no
transparent colour is hard-coded.

The index is bounded against the palette the file carries: one of those titles
declares index 304 against a 256-entry palette, and an image naming no palette
entry stays whole.

What belongs behind a transparent tile is a separate question, answered below.

<a id="implementation-lbmp-the-handsets-own-bitmap"></a>

### LBMP: the handset's own bitmap

One local title ships five images no standard decoder recognises. They begin
`LBMP`, and behind a 24-byte header hold the pixels the LCD would hold — no
compression, no palette. The image router passed them to the standard library,
which reported an unknown format, and the Java side raised
`IllegalArgumentException("undecodable image")`, so those images were not drawn
at all.

The vendor format is **not in the WIPI specification**, so the files settled
it. `internal/wipic.DecodeLBMP` reads the header as magic, depth, width,
height, payload length and a sixth word; every file is exactly
`24 + width * height * (depth / 8)` bytes, which fixes the first five fields.

The darkest file uses **eleven distinct byte values across 30,720 pixels** —
`0x00, 0x20, 0x24, 0x44, 0x48, 0x68, 0x8c, 0xb0, 0xb1, 0xd5, 0xf9` — scattered
across the byte range rather than counting from zero, with no room for a
palette. Read as RGB332 they form one ramp (red slightly above green, blue zero):
a gold-on-black gradient, decoding to a gold dragon; the file beside it is green
foliage. A palette-indexed reading cannot explain the scattered values.

The sixth word is zero in all five and nothing read it; a colour key, if the
format had one, was not exercised, and guessing would have put holes in images
on no evidence.

The format turned up in a KTF archive, not from the vendor whose name it
carries; the sweep found LBMP only there among hundreds of BMPs across KTF and
one in LGT. The decoder sits in `internal/wipic` beside `DecodeBitmap`, and all
three platforms route to it — two because they had callers, the third because
an image its neighbours can read should not end a title.

<a id="implementation-the-other-half-of-the-format-from-the-vendor-that-owns-the-name"></a>

#### The other half of the format, from the vendor that owns the name

The SKT archives carry LBMP everywhere — about nine hundred files across the
corpus against the five that first described it — and they settle the rest of
the format:

- **The sixth word is a mask flag; 1 means a one-bit transparency mask follows
  the pixels.** Its length is `width * ceil(height / 8)` in every masked file —
  the payload is exactly pixels plus that. The five KTF files all have the flag
  clear.
- **A set bit is transparent.** Every pixel a set bit covers in one title's
  sprites is the same magenta; read the other way, a sprite is magenta with its
  artwork punched out, which is what three sibling titles drew before the
  polarity was settled.
- **A depth below 8 is stored as bit planes** in the LCD's page layout: byte
  `(y / 8) * width + x`, one pixel row per bit, the band's top row in bit 0.
  `size` is one plane rather than the whole payload, so the plane is the unit: a
  two-bit image is two planes and its mask a third. Read any other way the file
  is noise; read this way it is a company logo.

**The planar ramp runs opposite to the packed depths: zero is white and the
widest value black.** One title ships the same glyph sheets twice — a white and
a black font with identical shapes and mask, zero under the white and three
under the black — and one sheet of the white font at eight bits a pixel, where
the pixels are 0xfe and 0xff. Read as light, its whole script was black text on
a black screen.

The intermediate levels remain unsettled: every planar file uses only the two
ends of its range, so the ramp is assumed even and the plane order is
undetermined until a file with a middle level appears.

Both halves are visible: a sprite without its mask is a magenta rectangle (three
sibling SKT titles drew their world in magenta blocks), and a planar sheet read
as light is invisible (a fourth drew its opening narration black on black).
`docs/skvm.md` has that pass.

<a id="implementation-the-graphics-contexts-clip-is-how-a-tile-gets-drawn"></a>

### The graphics context's clip is how a tile gets drawn

Every drawing `MC_grp*` call takes an `MC_GrpContext *` carrying a clipping
rectangle. Ignoring it is harmless until a title draws its map like this:

```
MC_grpSetContext(gc, MC_GRP_CONTEXT_CLIP_IDX, (int[]){83, 224, 99, 240});
MC_grpDrawImage(screen, 83, 176, 320, 128, sheet, 0, 0, gc);
```

The blit is the **whole 320x128 tile sheet**, positioned so the wanted cell
lands under a 16x16 clip: the clip is the tile selector. Every cell is drawn
that way, several dozen a frame, so without clipping the whole sheet is painted
once per cell — hay bales and sunflowers scattered over black, misread for a
while as a missing background layer.

- **The clip arrives as four `M_Int32`; the record holds four 16-bit fields.**
  `MC_grpSetContext` copied the array byte for byte, so the first two ints filled
  all four fields: top-left `(83, 0)`, bottom-right `(224, 0)`, and the real
  bottom-right was never read. The specification calls both this field and the
  origin offset integer arrays, so both are widened and narrowed.
- **An empty clip means "no clip".** `MC_grpInitContext` zeroes the record
  without knowing the target framebuffer, so a context never given a clip holds
  an empty rectangle. Reading it as "draw nothing" would stop every title that
  never sets one; the local set confirmed "no clip" leaves them unchanged.

The clip now bounds `MC_grpDrawImage`, `MC_grpCopyFrameBuffer`,
`MC_grpCopyArea`, `MC_grpFillRect`, `MC_grpDrawRect`, `MC_grpDrawLine`,
`MC_grpPutPixel` and both `MC_grpDrawString` forms — every call that takes a
context. The Java draw path has its own clip and was untouched. The defect
remaining in that scene was unrelated to the clip (next section).

<a id="implementation-a-font-handle-is-an-identifier-and-it-was-being-read-as-a-height"></a>

### A font handle is an identifier, and it was being read as a height

A title's menu lost the top row of every Korean syllable — `이어하기` with open
`ㅇ` tops, `예` reading as `네`, `아니오` as `바니노` — but only inside menu bars
and confirmation boxes; its save-slot screen, drawn into open space, was
correct. **The text was drawn taller than the box the title clipped it to.**

The title asks for its font once:

```
MC_grpGetFont(face 0, size 8, style 0)   ->  handle
MC_grpGetFontHeight(handle)              ->  8
```

and lays every menu entry out as a band that tall, clipping each before
drawing: clip `(70,155)-(171,163)`, baseline 161, six rows above and two below.
The handset face draws a Korean syllable nine rows tall, `baseline-7` to
`baseline+1` — row 154, one above the clip — so one row was cut, which is the
whole top arc of a `ㅇ`.

`MC_grpGetFontHeight` answered with the handle it was given. In the
specification `MC_GRP_FT_SIZE_SMALL` is **8**, `MC_GRP_FT_SIZE_MEDIUM` **0** and
`MC_GRP_FT_SIZE_LARGE` **16**: identifiers, not measurements. A title asking
for the small font was told its line was eight pixels tall.

The three metric slots now answer for the face the renderer draws — height 11,
ascent 8, descent 3 — for any handle, as LGT already does: a height that
disagrees with the glyphs is a layout that disagrees with its text.
`MC_grpGetFont` still returns the requested size as the handle, because handles
are opaque and different requests should stay different handles; nothing
measures it.

The title's own formula, `baseline = top + height - 2`, then yields clip
`(70,154)-(171,165)`, eleven rows, baseline 163, with two rows to spare above
the glyphs.

**One face means one set of metrics.** There is no smaller face for SMALL, so
smaller numbers would describe a font that is never drawn.
`TestTheFaceFitsTheMetricsItReports` pins that every glyph fits between the
reported ascent and descent.

Also changed: a title whose opening notice is three lines of Korean in a framed
box drew them 8 rows apart into an 11-row face, so they touched; they are now
spaced and the box fits. Across the local KTF set the deterministic first-frame
sweep changes those two titles and nothing outside the family that differs
against itself.

LGT already answered its three font slots from the face. SKT keeps a per-MIDP
size height table — SMALL 8/7, MEDIUM 10/8, LARGE 16/14 — that does not
describe the face either, but every local SKT title asks only for MEDIUM, whose
box the glyphs fit; the mismatch has no caller and stays a watch item.

<a id="implementation-an-image-is-drawn-through-its-own-handle-not-the-framebuffer-inside-it"></a>

### An image is drawn through its own handle, not the framebuffer inside it

The same map drew a few objects — a bush, a fence, a hanging vine — inside
black rectangles. It looked like a colour-key question, because
`MC_grpDrawImage` is documented to draw "with the transparency and transparent
colour the context specifies" and no blit read the context's transparent-pixel
field. The probe that settled it: sample one pixel inside a black box on every
LCD flush and report the last draw covering it. It was a 39x34 blit whose
source pixel was black **and marked opaque**, from a handle created by
`MC_grpCreateImage`.

`MC_grpCreateImage` builds two records: a framebuffer record for the decoded
pixels, and the `MC_GrpImage` the guest is handed, which inlines that
framebuffer's fields at its front. The declared transparency was recorded
against the inner framebuffer handle, which no caller holds.
`MC_grpGetImageFrameBuffer` answers the image handle itself (the record starts
with those fields), so **every C-side draw arrives with the image handle**; the
lookup missed, a miss means fully opaque, and every `MC_grpDrawImage` in the
archive ran without transparency.

Most sprites never take this path: the Java `Graphics.drawImage` route keeps
its own framebuffer handle in the image object and found its mask, so
characters, portraits and menus were correct; only objects drawn through the C
API were boxed. Transparency is now recorded against both handles, and the
route's frame on that map goes from 45,954 lit pixels to 52,405.

A/B across every local KTF archive: one other title changes (the black
rectangle behind its title logo is gone). Three more appear to change and do
not: their title screens animate, `-play` follows the wall clock, and two runs
of the same binary differ by four to five thousand pixels on them. Establish
that noise floor before calling a difference a regression.

<a id="implementation-the-transparency-a-title-brings-with-it-the-contexts-pixel-operation"></a>

### The transparency a title brings with it: the context's pixel operation

A graphics context can carry a function that tells the platform how each drawn
pixel combines with what is already there:

```c
M_Int32 op(M_Int32 srcpxl, M_Int32 orgpxl, M_Int32 param1)
```

Two local titles set one, and one **does all its transparency this way**. Its
8-bit BMPs declare none (`bfReserved1` is zero on every one a run decodes,
unlike the title in "A BMP declares its transparent colour in the header"), and
every sprite sheet holds its subject on a flat green backdrop at palette entry
zero. Its operation is four instructions:

```c
if (srcpxl == param1) return orgpxl;   /* the backdrop: keep the screen */
return srcpxl;
```

Ignoring it painted the backdrop. Because the title's field is green, sprites
looked right over grass and the fault showed only as a green square in each
corner of every title-screen dialogue box. The second title's operation keys
white and answers a colour kept in a global, so neither colour nor rule belongs
in the platform — only the call.

**The specification's prose for the two pixel parameters is reversed.** It
describes the first argument as the pixel already in the framebuffer, but the
prototype names say source first, and both local operations return their first
argument in the ordinary case, which for a blit must be the source. The
specification's alpha example is consistent only when read that way.

**The operation and its parameter are the record's last two words, not where
the specification's field order puts them.** The title that writes its context
directly rather than through `MC_grpSetContext` stores the function pointer it
also passes to the API at `+0x2c` and its key green at `+0x30`; that green
appears nowhere else in the record, not at the offset the documented
`MC_GRP_CONTEXT_PIXEL_PARAM1_IDX` would write (the title never calls that
index). The two pairs trade places with the font and style, which no local title
reads back.

Every drawing call that takes a context runs its pixels through the operation:
both blits, the fills and outline, the line, the pixel and both string forms.

- **The answer is cached.** The operation is a pure function of its arguments,
  and a blit would otherwise call the guest per pixel; keyed by the pixel pair,
  one title's dialogue-heavy run makes 7,600 guest calls against 24,000
  `MC_grpDrawImage` calls covering millions of pixels. The cache is dropped when
  the function or parameter changes.
- **A field this platform did not write is not trusted as a function.** A title
  that sets a font through the slot this handset uses for the operation leaves a
  small integer there. Only a Thumb address inside the loaded module counts,
  which one local title's font handle of `8` fails.

<a id="implementation-xor-mode-is-how-one-title-family-draws-everything"></a>

### XOR mode is how one title family draws everything

`Graphics.setXORMode(boolean)` was recorded and read by nothing: every fill,
line, outline and glyph overwrote the destination regardless of mode. The
specification is explicit — "화면의 내용과 현재 출력하는 내용을 XOR하여 출력" —
and the WIPI C context carries the same mode as `MC_GRP_CONTEXT_XOR_MODE_IDX`,
so the missing part was the write, `destination ^= colour`, which the LGT
runtime's `put` always did.

One title family draws **every sprite** through it:

```
setClip(x, y, w, h)
setGrayScale(0); setXORMode(true); fillRect(x, y, w, h); setXORMode(false)
drawImage(sprite, x, y, 0)
setGrayScale(0); setXORMode(true); fillRect(x, y, w, h); setXORMode(false)
```

The bracketing fills are **black**, so as XOR they are no-ops (a tint through a
colour left at zero) and the sprite arrives untouched. As solid fills they drew
a black rectangle over the sprite twice: a title screen of black boxes where the
logo and icons belong, a character behind a black square, and a menu whose text
was invisible except on the row the cursor had already darkened. Three local
titles came back; a fourth, whose menu rows draw white text and then invert the
row, produced the invisible-except-at-the-cursor shape.

**A partly covered glyph pixel XORs its source and then blends by coverage** —
full coverage is `destination ^ colour`, none is the destination — so the text
path's antialiasing stays continuous.

**Image blits were left un-XORed, and that was wrong** (see
[Text a title composes by XOR](#implementation-text-a-title-composes-by-xor-out-of-a-font-sheet-that-has-no-colour)).
The three grounds were that the specification's prose for the Java call names
`drawLine` and `drawPolygon`, that LGT routes only colour operations through
`put`, and that no local title draws an image with the mode on. The
specification has that sentence twice and the later one ends "모든 그리기 연산에
적용된다" — every drawing operation; `put` is the single point all LGT drawing
goes through, image blits included; and the third claim was false.

Only titles calling `setXORMode` can see the change; across the local KTF set
one reaches it inside its first four hundred ticks and the rest never call it.

**The black terrain that came back with it is the game's own art.** After the
fix that title plays its stages with a patterned background and solid black
ground, which looked like a failed layer. Its resources answer that: of thirty
sprite banks, **every one except the backgrounds is two or three colours** —
characters, obstacles, items, HUD, each a single silhouette colour over
transparency. The backgrounds are twelve banks of four 20x20 tiles at five to
sixteen colours, one per stage; the first is the cyan cloud pattern the first
stage draws tile for tile behind the black. **An asset survey answers "is this
what the game looks like" faster than playing**, when the art is regular enough
for a colour count to mean something. Where the ground went is the next
section.

<a id="implementation-copyarea-is-not-the-midp-call-it-looks-like"></a>

### `copyArea` is not the MIDP call it looks like

The same title's ground showed as two stubs — the first two tile columns and
the last — with the patterned background unbroken between them. The title
builds its scene offscreen, shifts the whole surface left ten pixels a frame
with `Graphics.copyArea`, then draws one fresh 20-pixel tile column down the
right edge; the shift copied nothing. **A repeating background hides a surface
that does not move**; only the non-repeating ground showed the screen held the
first frames' columns plus the current one.

The call was read in MIDP's order. `javax.microedition.lcdui.Graphics` has
`copyArea(x_src, y_src, width, height, x_dest, y_dest, anchor)`, seven
arguments, but this platform's class is different; the specification gives

```java
public void copyArea(int dx, int dy, int sx, int sy, int w, int h)
```

destination, source, size, no anchor. In MIDP order the title's whole-surface
shift `(-10, 0, 0, 0, 240, 320)` is a copy of a zero-sized rectangle, which
does nothing and reports nothing. **Look up a six-argument method with a
seven-argument twin in a neighbouring API rather than recognising it**; the
wrong reading type-checks and runs.

Two more details were needed:

- **The clip moves the source with the destination.** The destination starts
  ten pixels off the left edge; clipping moves its corner to zero, so the source
  corner moves the same ten pixels. Holding the source still copies the surface
  onto itself unchanged.
- **A copy inside one surface reads what was there before it began.** Walking
  the rectangle in order and reading from the surface being written smears the
  leading edge whenever the copy moves right or down. An overlapping self-copy
  now snapshots its source first — the specified "as if through a temporary
  buffer", as LGT always did. A copy between surfaces, or with disjoint
  rectangles, reads memory directly and allocates nothing.

Only two local KTF titles name `copyArea` — the scrolling one and one that moves
a dialogue box's text with it — which is how the defect survived; the fix was
checked by playing both.

<a id="implementation-text-a-title-composes-by-xor-out-of-a-font-sheet-that-has-no-colour"></a>

### Text a title composes by XOR, out of a font sheet that has no colour

A player reported one title's Korean as square blocks once the game proper
started, while its menu Korean was perfect. The menu is a picture — a 325x25
strip of all eight labels, plain above and highlighted below — and the game
draws Korean itself only in a scene.

It uses no platform font. Its archive's `font/` directory holds eight strips,
one glyph wide and hundreds of pixels tall, of syllable pieces: three banks of
초성, two of 중성, one of 종성, plus Latin and punctuation. Every strip is a
two-entry palette — index 0 white and transparent via a `tRNS` chunk, index 1
black. **The font carries no colour**, and no platform call tints a picture
while drawing it. The title composes the colour per jamo:

```
setClip(x, y, w, h)                       // the cell this piece lands in
setXORMode(true);  drawImage(ink, x, y)   // a small image filled with the text colour
setXORMode(false); drawImage(strip, x, y - glyphOffset)
setXORMode(true);  drawImage(ink, x, y)
setXORMode(false)
setClip(0, 0, screenWidth, screenHeight)
```

With `D` the ground and `C` the ink colour: the first pass leaves `D^C`; the
strip writes `0` under its ink and leaves `D^C` under its transparency; the
second pass turns the zeroes into `0^C = C` and the rest back into
`D^C^C = D`. **The glyph arrives in the ink colour over the unchanged ground**,
from a black-only sheet and without a per-pixel call. The 12x12 ink image is
refilled through `Image.getGraphics` and `fillRect` per string, which chooses
the colour: the speaker's name in orange and the line in cream come from one
font.

A `drawImage` that ignores the mode draws all three passes opaque, so every
syllable is a solid block of ink colour — the reported dialogue box of orange
and cream rectangles grouped as the words are.

The argument log, not the frame, found it. The boundary trace showed nine
thousand `drawImage` at three sites bracketed by four `setXORMode`, plus a
`getGraphics`/`setColor`/`fillRect` triple per string; printing the arguments of
one burst, armed at `getGraphics` to cover one composition, gave the sequence
above.

**Only titles calling `setXORMode` are affected, and they are countable.** The
name survives in the AOT image's import table, so a byte-string search finds
them: 20 of 264 in the largest KTF corpus, 2 more in the smaller one, 6 across
the two LGT corpora (where `put` was always correct), and **none in either SKT
corpus** — which is why the WIPI Graphics that platform composes over MIDP does
not declare the method. Every other run is byte-identical by construction.

`copyArea` also reads the mode, because the specification puts every drawing
operation under it, although nothing local copies with the mode on; its test
marks it as pinned behaviour rather than a fixed defect.

<a id="implementation-a-null-surface-is-a-state-not-a-fault"></a>

### A null surface is a state, not a fault

One title ended the session when the player left the world for the main menu:
`framebuffer handle address 0x0 is null or unaligned`, raised from its frame
timer, with a zero source handle. Nothing was missing: every image the scene
used loaded, and 4.5 million `MC_grpDrawImage` calls in ordinary play never pass
a null.

The null comes from a teardown window. Confirming the exit releases the scene's
images and starts building the menu while the frame timer keeps firing, so for
a few hundred frames it paints a sprite list with one null slot — 320 draws in
the reproducing run, none before or after.

**This API is C and the handle is a pointer**; null means nothing is there,
which a caller may say. The specification lists several ways for a copy to have
nothing to do — non-positive width or height, a source rectangle outside the
source, a mismatched depth — and each draws nothing rather than failing. A null
surface is treated the same: `MC_grpDrawImage`, `MC_grpCopyFrameBuffer`,
`MC_grpCopyArea`, the fills, outlines, line, pixel, both string forms, the RGB
transfers and `MC_grpGetImageProperty` return having done nothing, and the debug
build counts it by call name.

**A non-null handle that is not a frame buffer still fails**: that is a bug (a
stale pointer, a mis-decoded record), and separating out the null case keeps
such bugs visible.

<a id="implementation-a-rounded-rectangle-is-a-curve-and-one-titles-shadow-is-an-ellipse"></a>

### A rounded rectangle is a curve, and one title's shadow is an ellipse

`fillArc`, `drawArc`, `fillRoundRect` and `drawRoundRect` all drew the bounding
rectangle, on the reasoning that an approximate outline beats nothing. One title
calls it seventeen thousand times a run: **every character's ground shadow is a
`fillRoundRect` whose corner diameters equal its width and height** — an ellipse
— and it came out as a hard 24x8 rectangle, reported as a rendering fault.

The pixel probe found it (the shadow is a dark grey a shade off the grass):
sampling one pixel inside the box on every LCD flush and printing the last draw
covering it named the call, size and colour — the probe from "An image is drawn
through its own handle". A probe on the shared fill and one on the caller write
the same slot and the shared one wins: the first reading said `fillRect`, and
the counts showing the title never calls `fillRect` seventeen thousand times
sent it back.

The geometry is MIDP's, restated by the specification: `arcWidth` and
`arcHeight` are the corner arcs' **diameters**, and arc angles are degrees
counter-clockwise from three o'clock.

- **A row's inset is measured from its own centre.** Measuring whole rows makes
  an ellipse's top and bottom rows zero pixels wide, flattening it.
- **A diameter larger than the side it rounds is clamped**, which is what makes
  `fillRoundRect(x, y, w, h, w, h)` an ellipse instead of an overflow.
- **`drawArc` is the curve, not the pie.** MIDP does not draw the two straight
  edges to the centre, so the outline walks the angle rather than the bounding
  box, one step per pixel of the longer radius.

Both fills go through the same clipped row fill every other operation uses, so
the clip, the translation and XOR mode apply to them without a second path.

<a id="implementation-the-same-geometry-on-five-surfaces"></a>

#### The same geometry, on five surfaces

That fix covered only this platform's Java `Graphics`; five drawing surfaces
need these shapes, each answered separately:

| surface | arcs | rounded rectangles |
|---|---|---|
| this platform's Java `Graphics` | drawn as curves | drawn as curves |
| its WIPI-C graphics table, 15 and 16 | **unimplemented — fatal** | no such call |
| the other ARM platform's WIPI-C, `0xd8`/`0xd9` | **unimplemented — fatal** | no such call |
| its Java `Graphics` | **not implemented at all** | **drawn as a rectangle** |
| the MIDP platform's `Graphics` | **not declared at all** | **not declared at all** |

Three of those were worse than a wrong shape: an unimplemented WIPI-C function
ends the session, and an undeclared MIDP method fails to resolve, ending the
title the same way. Only one row was the "approximate outline beats nothing"
trade.

The geometry now lives once in `internal/curve`, which walks a shape and yields
horizontal spans without drawing. Each surface binds the spans to its existing
clipped fill, keeping its own clip, colour, blend and transparency rules. One
implementation is legitimate because the specifications agree: WIPI's
`MC_grpDrawArc` and MIDP's `Graphics.drawArc` both put zero degrees at three
o'clock, count positive angles counter-clockwise, take the second angle as an
extent, and centre the arc in the bounding rectangle.

The slot numbers needed no disassembly. This platform's table has `copyArea` at
14 and `drawString` at 17, and the specification puts exactly `MC_grpDrawArc`
and `MC_grpFillArc` between them. The other ARM platform's block is the same
argument at `0xd7` and `0xda`; see `lgt.md`, "The two arc slots come from their
neighbours".

<a id="implementation-screen-flush"></a>

### Screen flush

The guest flushes the LCD far more often than a display refreshes — a menu can
reach a few hundred a second — and converting its RGB565 screen to the Host's
RGBA frame reads and rewrites every pixel, mostly for frames no Host asks for.
`presentScreen` therefore only records the flush, and `Client.Frame` converts
once when a Host takes a frame; `Flushes` still counts every flush so a Host can
tell whether anything is new before paying for the copy.

The frame shows the screen buffer when the Host collects, not at the flush. A
Host collects between ticks with no guest running, so that is the last flush's
buffer unless the tick ended part-way through the next paint. For a display
collecting once a frame this is what a panel would show; the `runktf` frame
probes see it as a marginally different moment in a fade.

<a id="implementation-recognized-c-runtime-routines"></a>

### Recognized C runtime routines

These games compile their C runtime into the image and use it heavily.
Interpreting a byte-at-a-time fill costs several fetched and decoded
instructions per byte where the Host needs one copy; one title spends most of
its in-battle guest execution in its own memset, which made a special attack
lurch.

`binary_hooks.go` recognizes those routines in the loaded image by code shape
and replaces each entry with a supervisor-call stub, so a direct call or a call
through a function pointer lands on a native implementation. Hooked: memcpy,
memset, strlen and strcpy, each matched as the RVCT library helper reached
through a `BX PC` thunk and, for memset, also as the same function compiled
directly into the image where the thunk pattern misses it. Matching is keyed to
codegen, not to games: every local archive carries these, and a pattern that
matches nothing costs one scan at load.

The scan runs against the image in guest memory after the entry's
self-relocation, so the hooked addresses are the ones that execute. A stub is
wider than some patterns, so a match overlapping an installed stub is refused.
The hooked memset is 24x faster on the routine in one title and 41x in another;
instructions after a stub become unreachable, since the stub returns first.
Argument order is the hazard — a memset with value and length swapped fills the
wrong bytes silently — so `binary_hooks_test.go` pins each C contract,
including that memset writes only the low byte of its value, that strcpy copies
its terminator, and that each returns what the original returns.

**What is worth hooking is measured.** One supervisor-call round trip costs
131.5ns against 8.7ns per interpreted instruction (Apple M1, native): about
fifteen interpreted instructions, and the stub's own six come out of that. A
routine pays only if it runs well over twenty guest instructions per call —
only routines that loop over a length.

That rules out the three hottest routines the guest profiler found in the
profiled title, together roughly half its guest execution: the array-load
helper at `0x1689e4` (~15 instructions), the object-header-to-vtable resolver at
`0x167e84` (~9) and a static byte load at `0x167020`. All are O(1), so a hook
would lose. Their patterns generalize (byte-identical in four of seven local
images), but the cost model rules them out; that half of the profile needs
interpreter throughput.

The remaining candidates are inline copies a compiler emits instead of calling
`memcpy` — fixed-length and register-length — which need a pattern language
that does not exist: wildcards, a register field captured in one instruction
and matched in another, and a digest to pin a whole match. Across the seven
local images the register-length form occurs **once**.

**Re-measured by run share, with the same answer.** What decides whether to
build the language is how much of a run such sites hold, and a profile bounds
that. The scene profiled is the deepest any local title had reached — a map with
sprites, tiles and text redrawing every frame, 790,695 samples over 7.9e8
instructions:

| | share | span |
|---|---|---|
| five hottest regions | 55.8% | 74-410 bytes each |
| every region 64 bytes or under, added up | 12.4% | eight regions |
| the hottest of those | 4.6% | 34 bytes |

An inline copy is short, so it would sit in a short hot region. A span does not
say how long a routine runs — thirty-four bytes could be a loop going round a
thousand times, which is what the cost model says to hook — but the table sets
the ceiling: all short regions together are 12.4% of the run, the largest 4.6%,
and a hook recovers only part of a routine's cost, so a perfect pattern
language is worth a few percent of one scene. The other 55.8% is the game's own
hundred-line drawing and logic routines, which no pattern matches. As with the
AOT title, what remains needs interpreter throughput.

Two findings would change this: a profile where one short region alone holds
double figures (disassemble it first; a copy loop there is the site to build
for), and a load screen, where a title decompresses a thousand resources and a
copy loop could dominate.

<a id="implementation-the-load-screen-was-profiled-and-it-says-no-as-well"></a>

### The load screen was profiled, and it says no as well

Every local archive was profiled over its first two hundred ticks — entry to
somewhere in the title screen, i.e. the load. **The first half of the
prediction holds**: one region dominates almost every boot, mostly short, and
the disassembled ones are what was predicted:

| share | span | what the bytes are |
|---|---|---|
| 97.3% | 74 | a 16-bit rectangle fill: `strh` down a row, then the row pointer by a stride |
| 70.7% | 16 | a fill of a range with a two-byte pattern read from a fixed source |
| 64.7% | 16 | `ldrb`/`strb` down a counted length — an inline byte memcpy |
| 50.1% | 74 | an 8bpp source through a palette to 16-bit destination pixels |
| 44.5% | 66 | the same, unpacking sub-byte indices and skipping a colour key |
| 37.9% | 198 | unpacking two RGB565 pixels, mixing them, repacking — an alpha blend |

**The second half does not.** A share is not a cost; the measure is host CPU
against the guest time it buys. A wall-clock run measures the game's pacing (a
title sleeping through its splash takes eight seconds for two hundred ticks
with the emulator barely working), as the first measurement here did. On a
manual clock skipped to each deadline, host CPU for the same two hundred ticks,
worst first:

| host | guest delivered | archives |
|---|---|---|
| 6.46s | 4.03s | one |
| 1.82s | 2.99s | one |
| under 2.1s | 2.0s or more | the other eighteen |

**One archive in twenty is slower than the guest clock it feeds**, and one more
is close. The rest finish their load in a fraction of the time the game
expects, so a hook would buy nothing. The slow one finishes loading by tick 400
(11.1s host for 8.0s guest) and catches up by tick 800: a one-time stall of
about three seconds, 37.9% of it in the alpha blend in the table's last row —
shifts and masks over two RGB565 pixels, spilling through the stack, which no
copy-loop pattern matches.

The pattern language stays unbuilt for a second reason: gameplay's ceiling is a
few percent over eight regions, and at load the copy loops are dominant only in
the nineteen archives that never wait on them, while the one that waits is
waiting on arithmetic. What remains needs interpreter throughput.

It would change if an archive whose load runs slower than real time also spent
it in a short copy loop (so far the two properties land on different archives),
or with a handset-speed target: at a tenth of this machine's throughput every
load is slower than its guest clock, and the copy loops are worth their table
share.

A stub for these needs a shape the hook table lacks. Every hooked routine is an
entry reached by a call, and its stub returns through `BX LR`. These loops are
inline in a larger function, entered by falling in and left by a conditional
branch, so a returning stub would return to the enclosing function's caller. A
stub over an inline loop must run the supervisor call and branch to the loop's
exit, so the match must identify that exit too.

<a id="implementation-pacing"></a>

### Pacing

A game sets its own speed: its frame loop asks for a wait — `Thread.sleep`, an
`MC_knlSetTimer` delay, an idle `getNextEvent` — and that wait alone decides how
fast it runs. The Host honours the waits rather than picking a rate: each
resolves to a deadline on the session clock (`SessionOptions.Clock`,
`internal/platform/ktf/clock.go`), and the service loop does not run the work
behind it before the deadline:

- a sleeping worker keeps its queue place but gets no slice until its `wakeAt`;
- a timer stays pending until its `due`;
- a wait declared on the client thread has nothing to park (the Host goroutine
  is inside that call), so it is deferred: `ServicePaint` and `ServiceTimers`
  hold off until it elapses. A title can run its whole frame loop inside the
  card's `paint`, paced by a `Thread.sleep` there.

`Session.NextDeadline` reports when the earliest falls due, so a Host idles for
exactly as long as the game asked instead of polling.

<a id="implementation-runnable-now-or-due-on-the-next-idle-pass"></a>

#### Runnable now, or due on the next idle pass

A Host sleeps through everything `NextDeadline` leaves out, so it must include
anything ready, not only parked work. Two kinds of readiness were conflated,
breaking a title in each direction.

A thread started with `Thread.start` is runnable **now**; the next round adopts
it as a worker. Omitting it reported the game idle until other parked work came
due: one title starts a thread from its paint while another thread sleeps for a
hundred seconds, and the Host slept the hundred seconds.

A Runnable passed to `Display.callSerially` is due on the **next idle pass**,
`serialDispatchInterval` (one frame at sixty hertz) after the last, one per
pass. That is what the original event loop does, and games depend on it. One
title's frame loop is a Runnable that re-queues itself, reads the clock and
returns without drawing until its interval is up; dispatched as fast as the
Host turns rounds, the guest is never idle and a core burns to produce the same
twenty frames a second — 1.1 million rounds a minute against 1,675 once paced.

<a id="implementation-a-wait-that-has-elapsed-is-not-a-deadline"></a>

#### A wait that has elapsed is not a deadline

The client thread's wait is recorded and never cleared (`clientThreadDue` only
compares it with the clock), but `NextDeadline` reported it regardless of the
clock, so after a title's first `Thread.sleep` on that thread the session
answered "work is due, in the past" for the rest of the run.

A waiting Host was unaffected (a past deadline means "do not sleep"). A
**skipping** Host broke: a manual clock advances only by jumping to the next
deadline, cannot jump to one in the past, and so stopped where that wait
elapsed. A title whose first scene is a `java.util.Timer` task a second out sat
on the preceding frame, repainting, indefinitely; under `-play` the wall clock
moved and the scene appeared, which is why `-play` had been the only way past
it.

An elapsed client wait is now left out of `NextDeadline`, as for a title that
never declared one: a Host ticks rather than sleeps, and other parked work
becomes the next deadline.

<a id="implementation-work-that-cannot-run-is-not-work-that-is-due"></a>

#### Work that cannot run is not work that is due

**Rule: `NextDeadline` reports work that could run, not work that is queued.**
Anything reported as due now claims a round will take it, and a claim no round
can honour stops a manual clock.

The stale wait above was the first of a family. Two more appeared as the only
two local titles whose screen never answered a key: each had settled, redrawn
several hundred times and moved for nothing, with guest time stopped at the
first frame by a queue entry `NextDeadline` reported as due now and no round
could take. A third appeared in a later sweep, on a title whose splash never
advanced.

**A task queued behind a running task of its own `Timer`.** `ServiceTimers` runs
one task per `java.util.Timer` at a time and puts the rest back unchanged, so
the one behind keeps a long-past deadline and reports "due now" every round
while the running task is a worker parked on a sleep only the clock can end.
One local title deadlocked there before its title screen: the first task waits
a second and a half of guest time, the second pins the clock. Such a task is
now omitted; the running task's own wake-up is already in the answer.

**A thread started from inside paint.** A started thread is runnable now, but
the Host's round paint cost no guest time, so a title drawing only from the
round paint got one paint per round with no time between. One local title starts
a sound thread from its `paint`, so the queue refilled every round, "due now"
never ended, and the game's 2.4-second sleep could never elapse. **The round
paint stands in for the handset's display refresh, which has a rate**: it is now
held to `minGuestFramePeriod`, costing a frame of guest time like every other
frame here. Three conditions keep that rate from breaking other titles; each
was a corpus failure first:

- **Only the paint nobody asked for is rated.** A requested repaint is the guest
  asking for a frame now; the serial-Runnable queue waits on an outstanding
  repaint that only the paint clears, so gating it parks both on each other.
  Three archives of one title stopped drawing entirely.
- **The next frame is armed whether or not this one drew.** `paintTopCard`
  declines rounds (counted) while the guest publishes its own frames; a declined
  round with no deadline to return at leaves an otherwise idle title reporting
  no pending work, and a batch Host stops on the frame before the handover.
- **The paint deadline counts when already past, but only while the client
  thread is due.** It is the one deadline a round clears itself, and a batch
  Host skips to a deadline and asks again, so a paint reported only while ahead
  vanishes exactly when due. Reported through a client wait that has not
  elapsed, it is a deadline level with the clock that no round can clear.

**A timer that fell due inside the client thread's wait.** Timers dispatch from
the client thread, so a wait declared there holds every timer and one that comes
due meanwhile stays pending (as `ServiceTimers` states in its first lines).
`NextDeadline` reported the task at its own deadline regardless. One local title
stopped on its first screen: a task falls due 168ms into an 816ms wait, so the
session answered "due now" every round, the manual clock could not skip, and
the wait the timer queued behind never ended — guest time stopped and no
counted boundary event moved in three thousand rounds. The deadline is now
clamped to the client wake-up, as the serial queue already was, and thirty-nine
flushes became three thousand two hundred.

All three share one signature in the Host's own bookkeeping: **a deadline level
with the clock, on a round that did nothing.** `NextDeadline`'s answer, whether
`Tick` progressed, and the clock are three numbers a probe can print every round
to name the defect without knowing the guest.

<a id="implementation-what-frame-skipping-cannot-pay-for"></a>

#### What frame skipping cannot pay for

Dropping a paint (`session.md`) trades the picture for the logic in the same
round. A round without logic has nothing to trade: for the title whose frame
loop runs from its card's paint, the paint is the round's work and sets the
client thread's next wake-up. Skipping left that thread perpetually due, so the
Host spun, dropping the paint every round. A round that serviced no timer and no
thread now paints however far behind the Host is; the saturation case frame
skipping was built for is unaffected, since there timers and threads are what
run.

The clock is injected rather than read because two kinds of Host want different
answers:

- an interactive Host (the page, `runktf -play`) leaves `Clock` nil and gets
  the wall clock, so the game runs at the speed it was written for;
- a batch Host (the frame probes, `runktf` without `-play`, tests) passes a
  `ManualClock` and calls `Session.SkipToNextDeadline` whenever a tick found
  nothing due. It then runs the same sequence of guest work at no real cost,
  which is what keeps a 64-round frame probe instant.

<a id="implementation-a-title-whose-speed-was-the-handsets-to-decide"></a>

#### A title whose speed was the handset's to decide

One title reported as running too fast does what it asks. Its logic thread
draws a frame and calls `Thread.sleep(20)` — a constant, never reading the clock
— so it takes one step per 20ms plus the step's own cost: 49.6 steps a second
here, against a handset where the frame's work was a real share of the budget.
**Nothing in the archive says what that share was**, and inventing a per-frame
cost would be picking a rate rather than honouring one.

The speed multiplier answers it. The browser's speed menu stopped at 0.5x while
the core always accepted 0.1x; it now also offers 0.25x and 0.75x, bracketing
this title's plausible original rate at twelve to thirty-seven steps a second.

**Measuring a speed change from a route:** a route's `wait` counts ticks, and an
interactive tick waits at most `idlePollCeiling` (50ms), so a fixed-tick run at
0.25x takes about the same wall time as at 1x. The guest's work per second is
what moves: 1,040 sleeps in 21s at 1x against 575 in 42s at 0.25x (the factor
of four); the paint count is identical because paints are Host rounds.

<a id="implementation-a-second-title-the-same-complaint-and-a-different-place-the-time-went"></a>

#### A second title, the same complaint, and a different place the time went

A second title reported the same way has a different shape, and the fix for one
does nothing for the other.

It never calls `Thread.sleep`. It builds a fresh one-shot WIPI timer every frame
— `MC_knlDefTimer`, `MC_knlSetTimer`, `MC_knlUnsetTimer`, 5,758 of each over a
route — and **every one asks for 10 milliseconds**. Its card paints once per
Host round, so its contract is "wake me 10ms after this frame ends" and its
period is its frame plus 10ms, which the Host honours exactly. Measured on that
route with `-play`:

| | this title | the heavier one beside it |
|---|---|---|
| the wait it asks for | 10ms | 10ms |
| guest instructions a frame | **173,760** | **2,221,000** |
| host CPU a frame | 6.2ms | 22.0ms |
| period, measured | **18.0ms** (55 a second) | **33.3ms** (30 a second) |

The Host is idle two thirds of the run — 35.70s of CPU against 103.83s of wall —
so it is wait-bound, and 6.2 + 10 is the 18ms observed.

**Both ask for 10ms and run at half each other's rate** because of the first
row: 173,760 instructions is a frame of asking for drawing. The diagnostic
counts show about 190 graphics-table calls a frame — 75 of function 13, 61 of
function 6, and a tail — plus 233 `memcpy` hooks and 99 pixel operations. **This
title draws through the platform, which is Go here**; the handset ran those
calls as the vendor's C on a hundred-megahertz ARM9, where its frame time went.

That decides what a work clock is worth. LGT converts guest instructions to
guest milliseconds; at LGT's rate KTF would charge this title 1.2ms a frame on
an 18ms period, **not touching the complaint**, while slowing the heavier title
by 15ms a frame. A clock charging guest instructions cannot see work that is not
guest instructions.

A cost model for the platform's drawing (pixels touched against a rate the
handset never documented) would be picking a number on a much larger surface,
and is not built. **The multiplier remains the answer**, now stored per game
(`docs/session.md`, "The speed belongs to the game, not to the page"), so a
title needing quarter speed keeps it while others run at full speed.

<a id="implementation-the-frame-period-a-handset-would-have-charged-for"></a>

#### The frame period a handset would have charged for

The multiplier is the person's answer and remains. This is the Host's own,
added later and narrower: a cost model only for the guest's own instructions,
which the emulator can count.

This platform used to grant exactly the wait asked for, which no handset did:
every local title reported too fast at 1x asks for a frame every ten
milliseconds — the era's idiom for "as soon as you can" — and every title
reported fine asks for twenty-five or more. The specification says a timer is
accurate to the resolution of the system underneath, with error expected.

A frame period — a timer's interval, not a mid-frame sleep — is raised twice
before becoming a deadline (`chargedFramePeriod`):

- by **the frame's own work**, at 200,000 guest instructions a millisecond (the
  handset this platform stands in for); a handset's frame is its work then its
  wait, so the two add;
- then to **a floor** of 16.667ms, the sibling platform's one frame at sixty
  hertz. It is not a handset claim: it separates the two reports above and is
  the only stand-in for drawing done through the platform, which the work bound
  cannot see.

**The period runs from the deadline it answers, not from the arm.** The Host
has already spent real time on those instructions — more than the handset, on
any scene where the work bound binds, because the emulator is slower than the
handset it models. Charging from the re-arm instant charges the work twice. A
heavy in-game scene of one local title spent 26ms of host time on a frame the
handset would spend 17.7ms on, then waited another 17.7ms — 44ms a round for a
frame a handset turned in 28. `ServiceTimers` records the deadline each
callback was due at and `framePeriodDeadline` measures from it, so a Host faster
than the handset sleeps out the difference and a slower one does not sleep.

**Anchoring at the arm halves the period.** A title that re-arms at the top of
its frame arms a whole frame after its previous arm, so each deadline gets a
head start and the period converges on half the request. Four of nine local
titles doubled their round rate that way during development — a runaway that
looks like a fix, which is why every title's rate must be checked after a
pacing change.

Rounds a second at 1x, before and after. The first row is the routed heavy
scene of the complaint; the rest are blind-warmed, reaching a menu as often as
a game, which is why they move little:

| | before | after |
|---|---|---|
| heavy in-game scene, routed | 21.9 | **35.6** |
| title A | 24.9 | 33.8 |
| title B | 14.7 | 19.8 |
| title C | 9.7 | 11.0 |
| title D | 20.9 | 21.7 |
| titles E, F, G, H | 19.2, 9.4, 12.1, 94.5 | 19.0, 9.4, 12.1, 94.6 |
| title I | 27.8 | **54.1** |

Four of the nine never arm a WIPI timer and do not move. Title I's frame costs
about its period, so the round's own cost was most of its wall; it now gets the
handset's rate rather than the handset's rate plus ours.

A batch Host is unaffected: on a manual clock the answered deadline is the
instant the clock jumped to, so the anchor is a no-op and the same route replays
to the same 2,168 ticks and 2,179 flushes.

<a id="implementation-where-a-heavy-scenes-time-actually-goes"></a>

#### Where a heavy scene's time actually goes

**92% of the prompting title's guest execution in a heavy fight is its own
sprite drawing**, not the platform's. Guest profiler over a minute of play,
4.17e9 instructions:

| share | what the code is |
|---|---|
| 47.7% | the sprite blitter: a run-length source byte stream, a palette index per pixel, `ldrh` from the palette and `strh` to the frame buffer, clipped per pixel |
| 26.7% | the RGB565 alpha blend it calls per pixel on translucent runs — the 0x1f, 0x7e0 and 0xf800 masks, `(src*a + dst*(256-a))>>8` a channel, recombined |
| ~18% | the rest of the same family: run fills and a second entry into the blitter |
| ~7% | everything else the title does |

That is "the flashy skills lag": a translucent effect takes the second row's
path, a call and some thirty instructions a pixel against the first row's dozen.
The platform is barely involved — 32 graphics-table calls and one flush a frame
against 3.5M guest instructions a frame.

This is the case `docs/armcore.md` closes with: interpreter throughput, not a
hook. The blitter loops over a length, so the cost model in "Recognized C
runtime routines" would pay for it many times over, but it is the game's own
routine, and hooks stay keyed to codegen, not to games, because that is what
keeps them safe. Whether the blitter is shared across a studio's titles (making
a pattern game-keyed in name only) is not measured.

`SessionOptions.Speed` (and `Session.SetSpeed`, live) scales the pace between
0.1x and 16x. It divides every wait and multiplies the guest's clock by the same
factor, so a game timing its animation with `MC_knlCurrentTime` speeds up rather
than compensating. Throughput bounds the top end: at 4x a 45ms wait becomes
11ms, but the frame's guest execution costs the same.

Guest time is one timeline. `MC_knlCurrentTime` and Java's
`System.currentTimeMillis` both read `guestMillis`, the session clock scaled by
the speed multiplier (the JVM reaches it through its `Clock` option). Because it
tracks a clock rather than accumulating declared waits, a guest polling the
time without sleeping sees it move, so busy-wait loops terminate.

**The timeline starts at the Host's clock, not the wall.** It used to be
`time.Now()`, which cost every probe its reproducibility: a title seeding itself
from the time of day repeats only if the time of day does. One local title's
repro route stopped at 877 ticks, then 2,353, then 877 again, and five corpus
first frames differed against themselves — the whole corpus A/B noise floor at
the time. The epoch now follows `client.now()`, `NewManualClock` starts at a
fixed date when no instant is given, and the same script lands on the same tick
and pixels every time. A wall-clock Host still sees real dates.

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
game's save replace another's shipped data. PIDs are per title, and variants of
one title (a reissue with modified drop rates shares `PD004517`) share a
directory as intended.

The browser uses the same on-disk layout, because a session runs the emulator
on the machine holding the saves: the guest reads and writes
`var/savedata/<profile>/ktf` through the CLI's `DirectorySaveStore`, and no save
crosses the network. The save API — `GET /api/saves/<owner>` listing every
entry as base64 and `PUT /api/saves/<owner>/<key>` writing one raw entry —
remains for reading the tree out of process (`WFEATURE_SAVE_ROOT` overrides its
root). The service worker never caches `/api/` responses.

Save keys normalize before reaching disk (`ktf.NormalizeSaveKey`, mirrored in
the server's `normalizeSaveKey`): empty and `.` components drop out, `..` is
rejected. Guests name databases like files — one opens `./OptionSave` — and
rejecting that key silently lost every save it made.

The original emulator scoped saves the other way round —
`<root>/db/<PID>/<key><record-id>` with the key percent-encoded, and
`<root>/fs/<AID>/<path>` — so copying that tree in place leaves every save
invisible. Four differences:

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
performs that translation, reading every archive under `-games` to resolve
owners. A database whose PID no archive claims is reported, not guessed. No
source file says which API wrote a database, so a multi-record one (only a Java
`DataBase` can have those) is written to `jdb/` alone and a single-record one
to both `db/` and `jdb/`; the scopes never collide, so whichever the game opens
finds its save. A guest-file owner whose AID several games share is decided by
which of them packages files of those names (the `010100D5` files resolve to
one of the four that way), and reported when that does not settle it.

The browser reaches all of this over a session: `internal/webhost` starts the
archive on the server, pumps the startup that KTF runs in slices, and sends the
presented frames on. The phone keypad and keyboard route to KTF key events over
the same socket. See [`session.md`](session.md#implementation-server-sessions).

<a id="implementation-a-frame-loop-on-a-guest-thread-asking-for-no-frame-period-at-all"></a>

#### A frame loop on a guest thread, asking for no frame period at all

"The frame period a handset would have charged for" covers frame loops on a
WIPI timer's interval and in a wait inside the card's `paint`. A third shape
reached neither rule: the whole loop on a guest thread — `repaint`,
`serviceRepaints`, `Thread.sleep`, `Thread.yield`, repeat — where the wait goes
to `sleepCurrentWorker`, resolved through `waitDeadline` as exactly the
requested length with no floor.

**One local title asks for zero**, answered literally: `runtimeThreadSleep`
parked nothing, and `Thread.yield` (a JVM builtin) does nothing, so nothing
ended the thread's slice and it ran the whole two million guest steps every
round. `-diag` over sixty rounds counts 288,779 `Card.repaint`, 288,779
`Card.serviceRepaints`, 288,780 `Thread.sleep` and 577,558 `Thread.yield`, and
no clock read.

Reported as unbearably slow. Measured on the wall clock with
`TestLocalKTFGuestClockRate`:

| | before | after |
|---|---|---|
| flushes per round | **4,813.0** | 1.0 |
| guest clock against the wall | 1.0000 | 1.0000 |
| sixty rounds (wall, and therefore noise) | 2m 6.6s | 1.2s |

4,800 published frames per collected frame means the world steps four thousand
times between two pictures: the game sprinting and the screen crawling, both of
which the person saw. The cost is the guest's own drawing (a flush only marks
the framebuffer): the same sixty rounds carry 971,316 `Graphics.drawImage` and
866,499 `Graphics.fillRect` calls. Afterwards the splash animation runs at one
frame a round and finishes its wordmark inside six hundred rounds.

**The guest is asking for a frame as soon as possible** — the request the timer
path already floors; `sleep(0)` and `sleep(10)` are the same idiom, and the
floor is this platform's frame-period resolution, not a handset claim. It now
reaches this loop shape:

- **a frame published from a guest thread marks that thread** — `presentScreen`
  sets it on the worker, not the runtime, so one thread's frame cannot pace
  another's sleep;
- **that thread's next wait is its frame period**, raised to
  `minGuestFramePeriod` if shorter (`frameLoopPeriod`, `clock.go`); reading the
  mark clears it, so one frame floors one wait;
- **every other wait is the requested length.** A thread that has not published
  a frame is not in a frame loop, and flooring its sleeps would make a loader
  that sleeps between chunks take a frame per chunk.

**The frame's own work is deliberately not charged here.** `chargedFramePeriod`
measures from its last read, which for a thread arriving after a long start-up
is the whole start-up, and with no deadline to clamp against
(`sleepCurrentWorker` parks for exactly the length given) it could park a
thread for an hour; the floor alone was what this report needed.

It does not throttle the Host's presentation: how often frames are collected or
shown is unchanged. A manual-clock Host skips the new period like any other, so
batch runs pay no wall time.

**Reach.** Publish rate per round over the whole local set at 30 rounds of
`-play`: before, one archive is above three (4,807) and the next is 23; after,
none is above three and the highest is the same unrelated archive, unchanged
(689 flushes to 686). A final-frame A/B over the first sixty archives — 300
rounds each, with the same binary run twice as a control — gives 47 identical,
12 where the control itself differs, and **one that differs: this title.**
Without the control the A/B reported seven differences, three of them noise
(a blinking prompt lands where it lands on a wall-clock run).

<a id="implementation-the-opening-that-takes-minutes-and-whose-minutes-they-are"></a>

#### The opening that takes minutes, and whose minutes they are

Reported the same week: a title that never reaches its main screen. Its flush
rate is ordinary (0.9 a round), and its opening does end by itself, after 8,897
rounds on a stepped Host with nothing held — minutes on one picture at a
browser's twenty rounds a second.

**A wrong-rate clock would explain both reports** (a guest computing its next
frame spins if the clock runs ahead and waits for ever if behind). It does not:
on the wall clock the guest clock runs at **1.0000** of real time for this
title, the spinning one and a healthy one, as it must with `guestMillis` being
the session clock at multiplier one. This title barely reads it — 31
`MC_knlCurrentTime` and 48 `System.currentTimeMillis` against 324
`Thread.sleep` calls over the whole opening. **It counts sleeps rather than
measuring a deadline.**

Its own sleeps are the whole wait; two guest threads sleep for essentially all
of it. Over 9,000 rounds of `-play`:

| the thread | sleeps | asks for | comes to |
|---|---|---|---|
| its receive thread | 326 | 500ms | 163s |
| its main `Runnable` | 15 | ~11,035ms | 166s |

against 170 seconds of wall clock, the two overlapping. The Host answers each
within three milliseconds (503ms of wall for a 500ms request, timestamped at the
boundary), and between them the guest retires 674,607 instructions over 3,700
rounds — a third of what a single thread slice may retire. **This is a wait, not
a load** (the distinction [`cli.md`](../cli.md) refers here for), and the
boundary counts over the window name no stub, refusal or missing entry.

**The multiplier decides it.** At `-speed 8` the opening ends at 2,850 rounds
instead of 7,680, and wall time from launch to the transition falls from about
150 seconds to about 20 — the factor the waits were divided by, within
busy-machine noise. Host overhead would not have moved.

Recorded, not fixed: the title is granted the waits it asks for, and a handset
with an honest `Thread.sleep` held it on that picture as long. What it counts
toward is not established (its only logging in the window is one resource-engine
line on a loop), and a Host cannot shorten it without lying about the clock.
The multiplier is the remedy for a person who does not want to wait.

<a id="implementation-cheat-engine"></a>

### Cheat engine

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

<a id="implementation-wipi-java-api-coverage"></a>

### WIPI Java API coverage

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

Stubs are inventoried for the opposite reason: an unimplemented method fails
loudly, while a stub silently answers a fixed value the game believes.
`testdata/wipi_java_stubs.txt` lists every method bound to a fixed-value
implementation with its answer, and `TestFixedValueStubInventory` fails when an
unrecorded stub appears or a recorded one stops being a stub. The audit once
moved `Display.hasPointerEvents`/`hasPointerMotionEvents` from `true` to
`false`, because no Host delivered pointer events and claiming otherwise
invites a game to wait for touches; both answer `true` again now that one does
([A touch reaches the game](#implementation-a-touch-reaches-the-game) has the
wiring and measured cost).

The event queue is the second input path: a game may run its own
`EventQueue.getNextEvent`/`dispatchEvent` loop instead of letting the platform
call its cards. `getNextEvent` answers a queued event or, when empty, parks the
worker for one frame and answers the platform's redraw request, so a frame loop
living in the queue stays frame-paced. `dispatchEvent` delivers key events down
the card stack as Host-driven delivery does, paints the top card for a redraw
request, and routes notify events to listeners registered with
`Display.addJletEventListener` (or a `grabKey` listener for a key the
application took over). The first `getNextEvent` switches `Session.SendKey`
from direct card dispatch to the queue, so no key is delivered twice.

**Four classes are registered beyond the WIPI surface**, because titles reach
them through the AOT bridge and the failure is a dead guest thread:

- `org.kwis.msp.media.BaseClip.setBuffer` is `Clip`'s, moved to the base class
  by the handset runtime one title was built against, where it reports whether
  the data was taken.
- `java.lang.Runtime` answers `totalMemory`/`freeMemory` from the arena
  `MC_knlGetTotalMemory` and `MC_knlGetFreeMemory` describe; two disagreeing
  views of one heap would have a game free what it just decided it could afford.
- `java.util.Calendar` reads the guest clock through the JVM builtin, so a save
  stamp matches `System.currentTimeMillis`.
- `java.util.Date` is the instant behind the calendar, used directly for a
  stamp to store or an interval's ends. Its four methods and both constructors
  read the same clock, so an interval across two of them is never negative, and
  `Calendar` gained `getTime`/`setTime`. **A card title played for an hour before
  its first `Date.getTime()` ended the session**: the class resolved through the
  fallback record, so nothing reported it missing until the call.

`Graphics.translate` moves the origin of every later operation; it is applied
in the shared clip step, so fills, lines, text, blits and pixel transfers honour
it, and the clip stays in device coordinates while the clip getters report the
caller's translated space. `Image.setTransparentColor` makes later blits of
that image masked (matching pixels keep the target's content), which is how
sprites composite. A decoded image is rasterized into a guest framebuffer on
first masked draw or image-to-image composition, so both kinds share one pixel
path.

An image's encoding can declare transparency, which must survive the same trip.
Sprites are palette PNGs with a `tRNS` transparent entry, while guest pixels are
16-bit colour without alpha; dropping it painted every sprite's unused border
opaque black. Each decoded image carries an opacity mask beside its pixels
(`internal/platform/ktf/image_opacity.go`) that follows it into the guest
framebuffer, and every blit consults it alongside any transparent colour the
game named. A copy or sub-image inherits the mask for its region; compositing
into a surface marks only the pixels drawn. Opacity is yes/no — the assets mark
pixels fully drawn or absent — and partial alpha is not blended, matching
`Graphics.setAlpha`, which is recorded but not applied.

<a id="implementation-runtime-diagnostics"></a>

### Runtime diagnostics

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

Two kinds of line are weaker than they look. A `raise <class>` line names only
the class of a runtime failure handed to the guest's handler chain, and reaching
the chain means a handler was found; a title that raises `java/io/IOException`
twice at startup and then plays is almost certainly reading a file a first run
does not have yet. **The exception's message is deliberately not in the name**:
it would split one boundary's count across every message, and the counter can
collapse only a trailing ` @0x` site. Read a `raise` beside whether the run
continued, and use the trace ring when it did not. A `stub table` line is a lead
rather than a defect — the original runtime stubs it too — and carries the
address to disassemble.

<a id="implementation-repro-routes"></a>

### Repro routes

Some behaviour happens only well inside a save — a battle, a shop, the frame
rate under a special attack. `runktf <zip> -route <script>` records the way
there, so a profile run, a regression check or a bug investigation can re-enter
the scene on demand.

A route must not rely on absolute tick numbers: fixing the type check that made
enemies stand still changed which branch the game took, and every hand-tuned
`-key <tick>:<name>` timing written before it missed its menu. A route waits on
what the screen does and counts ticks only where nothing better exists.

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

`key` holds its press for the run's `-hold` ticks before releasing, and those
ticks come out of the route: a press and release in the same tick is no press to
a title sampling the keypad once a frame (the reason `-key` has always held).

`#` is both the comment marker and a handset key: it is the key name when it is
a key verb's argument and a comment elsewhere. Cutting the line at the character
made `#` the one key the name table resolved but a route could never press.

Both searching waits are bounded — `wait-idle`/`wait-change` default their limit
to twenty times the run they look for — because an unbounded one eats the whole
tick budget and then blames the next step. A give-up says so: `the screen never
held still for 40 ticks within 800 — it is animating, not settling` is a
continuously animating title screen. A route that does not arrive returns
`route_completed: false` with the line that stopped it, since "the game went
somewhere else" is an answer, not a crash.

`mark` is the measurement checkpoint: it restarts the profile, so
`-route … -profile r.txt` reports the scene, not the loading before it. `shot`
only captures.

A route runs on the manual clock unless `-play` is given, making the replay
deterministic with one tick per game frame. `-play -speed 100` reaches distant
scenes faster: the guest clock runs that much faster, so each tick buys more
guest work.

<a id="implementation-guest-profiling"></a>

### Guest profiling

`Session.EnableProfile` switches on the ARM core's sampler (see
`docs/armcore.md`) and `Session.Profile()` resolves each sampled address against
the registered AOT method bodies. `Profile.Report` ranks self time by symbol and
then the hottest stacks; `Profile.Folded` writes flamegraph-folded lines for
`flamegraph.pl` or `inferno-flamegraph`.

From the CLI: `runktf <zip> -profile report.txt [-profile-folded stacks.txt]
[-profile-from <tick>]`. `-profile-from` resets the samples at that tick so the
profile covers the scene rather than the loading.

Symbolization is inference. KTF method metadata records where a body starts but
not its length, so a body ends where the next starts, capped at
`maxSymbolSpan`. The highest body has no successor, and above it sit
client.bin's own runtime helpers; unbounded, it claimed them, and the array-load
helper came back named `ax.<clinit>()V` holding 57% of a real profile. The
highest body is now bounded by the ninetieth-percentile gap between the game's
other bodies, and an address past every inferred end is reported as an address.

What the first profile found, on one KTF title at 1500 ticks (182k samples):

| share | code |
|---|---|
| 36% | `ac.a(Lorg/kwis/msp/lcdui/Graphics;IIII[BIII)V` — the game's own sprite blit |
| ~19% | `0x1689e4` — `baload`: null check, bounds check, `ldrsb r0,[array+8+index]` |
| ~17% | `0x167e84` — object header to class/vtable resolution, six instructions |
| ~13% | `0x167020` — static byte load |

The last three are tiny pure-guest client.bin runtime helpers reached by `bl`;
they looked like hook targets until the cost model in
[Recognized C runtime routines](#implementation-recognized-c-runtime-routines)
ruled them out.

<a id="implementation-text-input"></a>

### Text input

An lwc text component is editable. `Client.FocusTextComponent` gives one the
keypad and `Client.TypeKey` delivers a key to it; the editing itself is
`internal/textinput`, the multi-tap input method MIDP's `TextBox` also uses,
so a game types the same way on either platform. Keys the keypad does not
carry are left for the game's own navigation.

The multi-tap timeout is measured against the **guest** clock, so a Host
batching ticks types the same text as one running live.

<a id="implementation-one-titles-startup-gate"></a>

### One title's startup gate

One local title used to stop at its own screen — `인증오류 ... (오류번호:NNNN)` —
and exit. Several checks stood in front of it, each hiding the next; read the
number first:

| Number | What failed |
|---|---|
| 1001 | the capability mask, `MC_knlGetAccessLevel` |
| 2001 / 2002 | the executable listing, `MC_knlGetExecNames` |
| 3001 | the certificate |

The screen is the same for each, so "authentication error" was taken at face
value for several sessions, read first as a missing server and then as the
certificate. **1001 is a permission bitmask this platform answered too
conservatively**; the certificate is 3001, reached only after the others.

**First, the capability check** (`& 0xbc == 0xbc` on `MC_knlGetAccessLevel`):
fixed by granting every group, as described under
[The kernel table beyond the specification](#implementation-the-kernel-table-beyond-the-specification).

**Second, `MC_knlGetExecNames`** (kernel slot `0x2`), asked for the title's own
application id:

```
name = "010100D5"                     /* the archive's AID */
if (MC_knlGetExecNames(name, 0, 0, out, 300) <= 0) fail   /* error 2001 */
tail = out + strlen(out) - 21
for (i = 0; i < 8; i++)
        if (tail[i] != tail[i + 9]) fail                  /* error 2002 */
        identity[i] = tail[i];
```

It wants a listing whose last 21 characters carry an eight-character token
twice, nine apart; it takes the token as its identity and compares it with the
application id compiled into its image. The listing's layout is
**reconstructed from that one caller** — nothing describes it — and
`wipicGetExecNames` documents what is assumption and what is not.

**Third, the save budget.** `MC_dbListDataBase` reports remaining storage and
counted the archive's packaged databases against it. This title ships its maps,
text and event tables that way — about two megabytes — so the budget was gone
before any save, and a new game answered
*단말기 저장공간이 부족해서 시작할 수 없습니다*. On a handset packaged bytes arrive
with the program; only what a store has grown past the archive's shipped bytes
counts now.

Past all three the title reaches character creation and its opening.

<a id="implementation-the-certificate-behind-it"></a>

#### The certificate behind it

The title also checks `cert.c2s`, the publisher's own file rather than anything
WIPI describes: a server issued one per handset, and the archive's copy belongs
to whichever handset first downloaded it. The check decrypts it with the
subscriber's number and requires the plaintext to carry that number back, so a
certificate is valid for one subscriber only and no more accurate platform
answer satisfies it.

`internal/platform/ktf/provision.go` carries the format, and
`wfeature provision` writes one sealed for the number this platform answers
with — a command a person runs deliberately for an archive they hold. Automatic
authentication compatibility later reused it with stricter executable
recognition and an in-memory certificate save wrapper; see
[authentication.md](../authentication.md#ktf-certificates-and-identity).

<a id="implementation-what-the-corpus-names-and-what-it-never-asks-for"></a>

### What the corpus names, and what it never asks for

A KTF title is AOT-compiled, so the constant pools a Java archive scan reads are
gone. The **name pool inside the client image** survives: NUL-terminated
strings holding class names and member entries of the form
`<descriptor>+<name>`, which the guest hands to the platform's class lookup.
It is in the file (relocation does not write those strings), so reading it runs
nothing. `internal/tools/apiscan` reads it for a directory of archives and
ranks entries by how many titles name them.

**Only the class half survives compilation.** A pool entry names a method by
name and descriptor; the owning class is in records built at runtime. A method
missing from a class this platform does publish is invisible to the scan and
remains a matter for `-diag` and a run.

**A name is what a title could ask for, not what it does.** All 33 local
archives name `org/kwis/msp/lcdui/PluginJlet`; the four checked with
`ktfdump -classes` extend `Jlet` instead, with no PluginJlet in their class
tables after `startApp`. The toolchain can put names in the pool. The list is a
safe upper bound for planning — it cannot miss a class a title will ask for —
but an entry is a reason to look, not a defect.

What the local corpus names and this platform does not publish, in scan rank
order:

| Named by | Class | Reading |
|---|---|---|
| 33 | `org/kwis/msp/lcdui/PluginJlet` | Named by every archive, extended by none. |
| 14 | `org/kwis/msf/io/Socket`, `org/kwis/msf/io/URL` | The network classes beside `Network`, which is published. No local run has reached one. |
| 2 | `java/lang/OutOfMemoryError` | A `catch` a title would only take if this arena ran out. |
| 1 each | `java/lang/Short`, `org/kwis/msp/lwc/DialogComponent`, `org/kwis/msp/lwc/LabelComponent` | The two lwc components sit beside the ones that are published. |

None fails a title today, so the list is recorded rather than acted on. A
missing class fails not at startup but as "method … was not found from class …"
at the first call, possibly an hour into a save (see "A class left out of that
table still resolves"). **A name from this list in a report is already
identified, and the fix has the same shape as the others in the runtime
table.**

<a id="implementation-what-a-batch-of-new-archives-asked-for"></a>

### What a batch of new archives asked for

Seven titles that had never run were driven from a fresh save. What stopped
them falls into five shapes likely to stop the next batch:

**A handle must survive the field the caller stores it in.** The stream
database's handles had a high bit set to separate them from the record table's.
One title keeps an open's result in a `short` (`lsl #16; asr #16`, then a
negative-means-failure test), so it passed back a number never issued; every
read was silently refused, leaving the requested eight-byte header unwritten,
and the title sized an allocation from two unwritten words and asked for four
gigabytes. Both database tables now tag inside a signed 16-bit value (`0x1000`
stream, `0x2000` record). **Handset handles are small numbers; a platform's
are only as good as the narrowest field a title puts one in.**

**A refusal must be catchable by the guest.** `DataBase.selectRecord` on a
missing record answered with an uncatchable Host error. The specification says
`DataBaseRecordException`, and one title reads a never-written record on its
first tick (an empty save); the Host error turned its opening screen into a dead
thread. Every record-keyed DataBase call now raises the documented exception.

**A published constant is part of the class.** `Font` had its methods but none
of its `FACE_*`, `SIZE_*` and `STYLE_*` fields, so a title choosing its font
before drawing stopped at the first read. They are static reads with the
specification's values.

**`capacity()` is not a hint.** One title walks a `Vector` bounded by
`capacity()` with `elementAt`, which works only because CLDC's Vector grows by
exactly its capacity increment. Ours doubled, so the walk ran past the added
elements. A collection's growth rule is observable.

**`callSerially` runs on the event thread.** Each queued Runnable used to become
a guest worker; one title queues one from inside `paint`, so they arrived faster
than workers finished and the run hit the queue limit and then the thread-stack
limit. They now run on the client thread, one per pass: a frame apart while the
queue holds a single self-requeuing frame-loop Runnable, one per round while a
backlog drains.

The surfaces that batch added: `java.util.Timer` and `TimerTask` (below),
`Thread.currentThread`, `Integer`'s constructor and its narrowing accessors,
`String.compareTo`/`equalsIgnoreCase`/`endsWith`/`toUpperCase`, `Vector`'s
two-argument constructor, `Font`'s constants, `File.openOutputStream` and
`openDataOutputStream`, and image encoding on both sides of the platform —
`MC_grpEncodeImage` at graphics slot 35 and `Graphics.encodeImage`, which the
specification defines as the BMP bytes of a region of a framebuffer.

<a id="implementation-where-the-local-batch-stops-now-and-what-a-held-key-changed"></a>

#### Where the local batch stops now, and what a held key changed

Driving every local title from a fresh save again (a run and a contact sheet per
title) produced two findings.

**A press and release in the same tick is not a press.** Scripted keys were
delivered that way, and a title sampling the keypad once a frame never sees
them: two titles sat on their own screen for a whole run, looking stopped.
`runktf` now has `-hold`, as the LGT runner does; on LGT it mattered most — one
title's character select answers `right` at a one-tick hold and ignores `OK`,
and selects, loads and enters the world at twenty.

**Two titles failed past the screen the batch had checked**; both now play. Each
was given a class record it could not dispatch through — see "Three ways a class
record was not a class" below.

<a id="implementation-a-files-output-stream-is-the-file"></a>

#### A File's output stream is the file

The read side can be a snapshot (a `ByteArrayInputStream` over the current
bytes) because reading changes nothing. A write stream collecting its own bytes
would reach the file only if copied back, and a title closing the stream rather
than the file would save into nothing. The stream is therefore the file: it
shares the open-file state, every write goes through and is persisted as
`File.write` is, and `flush` and `close` have nothing left to do.

<a id="implementation-javautiltimer-is-due-on-the-platforms-timer-service-and-runs-on-a-thread"></a>

#### java.util.Timer is due on the platform's timer service and runs on a thread

**When** a Java task comes due is the timer service's decision: titles already
pace themselves with `MC_knlSetTimer`, the Host runs those callbacks in due
order once per round, and a Java task is the same shape (a delay, a body,
possibly a period). A timer with a task and no callback address is still a
deadline; omitting it from `NextDeadline` made the Host spin instead of
sleeping, and a title whose first scene is one scheduled task never reached it.

**Where** it runs is a thread. Tasks used to run inline on the client thread so
that one place decided timing and a task could not race its scheduler. One
title's whole battle is a loop inside `TimerTask.run()` that never returns;
inline, the Host goroutine stays in that call — no frame collected, no key
delivered — until the service call's step ceiling half a billion instructions
later. The specification's Timer runs tasks on a background thread, so a due
task is adopted as a guest worker like `Thread.start`, parking on its step
budget so its loop progresses each tick. Nothing races: workers still hand off
strictly, one slice at a time, under the same run lock.

- **A Timer has one thread, and its tasks run serially.** A worker per due task
  failed the same title: it schedules a fresh looping task per frame, and
  sixty-four stacked up within a second. A task whose Timer is still running one
  stays pending — the bound and the specification's serial execution.
- **AOT call nesting depth is per call stack.** It was one runtime counter, so a
  parked worker's nested stack stayed counted while the client thread painted on
  top, tripping a limit neither reached alone. Each ARM thread now has its own
  depth.
- **A repeating timer is re-armed when dispatched**, not when its run ends; a
  period falling due during a run waits for it. No local title schedules a
  periodic Java timer, so fixed-rate versus fixed-delay is recorded, not
  measured.

<a id="implementation-an-aot-method-lookup-falls-back-to-the-guests-own-class-records"></a>

#### An AOT method lookup falls back to the guest's own class records

A title registers a class when it first loads it, long after using instances of
it, so a card whose superclass is unregistered has no chain to walk, and an
inherited platform method (`keyNotify`, here) was reported missing. The registry
is still asked first; a miss now walks the guest's own class records, which
must exist for the class to be called — the type check's reasoning (see "AOT
outer calls").

<a id="implementation-one-system-property-table-asked-from-two-sides"></a>

#### One system-property table, asked from two sides

`HandsetProperty.getSystemProperty` answered only `VIBRATORLEVEL`, while the
WIPI C `MC_knlGetSystemProperty` answered from `internal/wipic`; they now share
the table. A missing answer does not make a title skip the feature:
`VOLUMELEVEL` is the hardware's number of volume steps, a title divides by it,
and an empty string becomes `Integer.parseInt("")` during `startApp`.

<a id="implementation-three-ways-a-class-record-was-not-a-class-and-the-two-titles-they-stopped"></a>

### Three ways a class record was not a class, and the two titles they stopped

The two titles that died past their first screen died of three faults in what
this platform gives the guest when it asks for a class.

**A fallback class record must publish a vtable.** `ensureJavaClass` invents a
record for an unimplemented name (see "A class left out of that table still
resolves") — with a name, no super, an empty method table and no vtable pointer.
Guest virtual dispatch reads the vtable from the class the object header names
and indexes it with the resolved slot, so a zero there loads four bytes past
null: `ldr r4, [r2, r3]` with `r2` zero and `r3` the slot times four. One title
died with "load of guest address `4`" a few hundred ticks into its opening, on
`getClass()` (vtable slot 1) against an **array** — arrays hit it first, since
their whole method set is Object's. The fallback record now has
`java/lang/Object` as parent and Object's vtable slot for slot, and its metadata
is read back from the written record so the registry knows the vtable too.

**A dispatch alias is a class record the guest hands back.** The alias (the
bounded copy of a class record in the platform arena that an object header
points at; see "AOT outer calls") is a valid record to the guest — its first
word marks it as one. A title reaching a virtual call through the header and
then asking for a method on the same word asked about an address the registry
did not know: `KTF AOT method g()V was not found from unregistered class
0x3003b104`, on the first paint of its card. Each alias is now registered with
the VM as an address alias for its class when made, the mechanism
`resolveAOTClass` already used for a class materialised at a second record; the
failure message now reads the record, naming a class rather than an address.

**A guest thread must end.** `Thread.isAlive` answers a flag the JVM clears when
its goroutine-backed `run()` returns, but this platform replaces that goroutine
with a guest worker (`workers.go`), so nothing cleared it. A loading screen of
`while (loader.isAlive()) sleep()` waited for ever: the first title, once it
stopped dying, sat on an animating `LOADING` for nine thousand ticks with 8,760
`isAlive` calls and no missing surface in the report. The worker now reports
the end through `jvm.VM.EndGuestThread`.

With the three fixed, one title plays — title menu, opening, loading, and its
own platform level with an HP/MP/EXP bar — and the other reaches its world map
with a character and a HUD.

<a id="implementation-the-array-store-check-was-asked-about-a-null-class"></a>

#### The array store check was asked about a null class

That run left one counter: `checktype` called with a null *class* operand 1,500
times from a single call site, unrelated to the crashes above. The site is the
guest's **array store check**:

```
ldr r2,[r2]        ; the object header out of the array's fields word
asrs r2,r2,#5      ; header>>5 is the dispatch alias offset
ldr r3,[r3,r2]     ; the class record, relative to the JVM context
ldr r3,[r3,#8]     ; its descriptor
ldr r0,[r3,#0x14]  ; the element class — the argument that was null
```

`+0x14` is the descriptor word a class with fields uses for its field table; an
array has no fields, so the client uses it for the element class. Every array
class this platform made left it zero, and a null class can only get the
permissive yes. The element class is now written there when the array class
record is built.

**The platform must not invent that class.** Resolving the element name through
`ensureJavaClass` breaks five titles deterministically: for an array of a
title's own class, the invented fallback record becomes what every later lookup
finds, and the title dies in its own class construction reading a field of it
(`read guest memory at 0x10`, several hundred instructions into `<clinit>`).
The element class is **resolved, never created**: an already known record, a
class this platform owns by name, or a nested array class; anything else leaves
the word zero and the answer permissive.

The opposite risk is a decidable check answering *no* and breaking a working
title. An array is assignable to `java/lang/Cloneable` and
`java/io/Serializable`, which no platform-made class record declares, so both
are answered directly. The A/B sweep across the local set: no title stopped
earlier, no `tick_error` appeared, **no new `checktype reject` was recorded**,
and undecidable calls fell from about 4,000 to 530, all arrays of a title's own
classes.

<a id="implementation-a-band-beside-a-title-screen-was-the-titles-own"></a>

#### A band beside a title screen was the title's own

One title paints its title screen into the top-left 176x220 of the 240x320
screen and leaves a uniform dark-red band down the right. Its `__adf__` says
`DisplaySize:176*220`, suggesting a platform that never sized itself to the
title. **The band is the game's own**, by three measurements: it appears in the
same frame as the picture, so both come from one draw; the title paints through
a single `drawImage` of its back buffer, which the blit probe shows is
**240x320**, so the band is inside the game's surface; and filling every new
surface with green at the platform boundary left the band red, so the guest
wrote it.

`DisplaySize` is **not** a screen size to adopt: thirteen local KTF titles
declare `176*220` and twelve draw across the whole 240x320 screen, so honouring
it would shrink twelve working titles. A new off-screen surface is **not**
cleared: `MC_grpCreateOffScreenFrameBuffer` promises nothing about content, and
the one title that might have depended on it does not.

<a id="implementation-and-the-title-behind-the-band-is-not-a-small-screen-title"></a>

#### And the title behind the band is not a small-screen title

The remaining question was whether to centre that corner, as would suit a title
written for a smaller handset. It was built and measured: **this is a 240x320
title whose title art is 176 wide.**

Pixel detection works: outside the declared rectangle is a fill whose colour is
not the picture's background. Over roughly nine thousand frames of all
thirty-two local KTF archives, the fill colour covers **88% to 100% of the
inside for every title drawing across the screen, and 8% for this one** — a gap
wide enough that the threshold is not tuned. The rule fired on one title, over
one unbroken run of frames.

The title's own menu ruled it out (measured by differencing captured frames):

- Its load dialog spans x 60..179, **centred on 119.5** — the middle of the
  240-wide screen. It crosses the rectangle's edge, so detection switches off
  when the dialog opens and the picture jumps back into the corner.
- In a new game it fills the whole screen; its dialogue box redraws at
  y 291..314, far below 220.

Centring would clip that dialog and move a picture the game placed deliberately.
Closed as *not a fault*: a title drawing its art in a corner and its menus
across the whole screen indicates which is the screen.

<a id="implementation-the-screen-is-a-hosts-answer-and-it-has-to-reach-both-surfaces"></a>

#### The screen is a Host's answer, and it has to reach both surfaces

A Host may name another handset — `runktf -screen WxH`, or the page's setting
— and both generations of package take it. `Client.SetScreen` and
`NativePlatform.SetScreen` are where it lands, and
[`session.md`](session.md#implementation-server-sessions) has the cross-platform contract.

**A size answered to one surface and not the other shears the picture.** A
title reads the screen from `MC_grpGetDisplayInfo` and the pixel buffer from the
screen framebuffer record, and two local titles write into that buffer with the
info struct's stride; a mismatch puts every row after the first at the wrong
offset, which looks like a decoder fault. Both answers come from one place, and
`screen_test.go` fails if they diverge — also on the earlier package, where the
pair is the display record and the blit target image.

**`DisplaySize` is still not the answer** (see
[the band section](#implementation-a-band-beside-a-title-screen-was-the-titles-own)):
the size is something a Host is told, so a title truly packaged for a smaller
phone is a flag away and the twelve that were not are untouched. Across the 34
local archives: at the default screen 29 first frames are byte-identical to the
previous build and the 6 that differ are the family with the measured noise
floor; at 176x220, 33 run clean and lay themselves out correctly.

<a id="implementation-an-ab-sweep-of-the-local-set-is-half-noise"></a>

#### An A/B sweep of the local set is half noise

Diffing captured frames from the same script under two builds is the regression
net for changes like the three above, readable only with a known noise floor.
The same binary twice at `-play -speed 20`: **16 of 32 titles differ, over 24 of
128 captured frames.** A wall-clock-paced run executes a different number of
guest instructions per tick each time, so a shot lands on a different animation
frame, and the tick at which a title reaches a failure moves too.

A differing frame hash is therefore not evidence. Evidence is a title stopping
where the other build did not, a changed error text, or a different end tick
with one of those. On the three fixes above base-versus-new was 18 of 32 —
inside the floor — with the only real differences on the two target titles:
`route_completed` false to true, and a `tick_error` gone.

**Those sweep figures badly understate the per-frame rate.** 24 of 128 comes
from four frames per title, most missing the animating region. Diffing every
frame of one continuously animating title, same binary twice, gives **1,216 of
2,379 frames differing** — over half. Reading the sweep's 19% as a per-frame
floor and then diffing a whole frame directory suggests a breakage.

For an animating title the count carries almost no information; **where the
differences are and how big** does. That run's differences are all small boxes
on a loading spinner's corner and a sprite's idle bob. A real regression is a
difference off an animating element, or large enough to be layout rather than
phase — visible in the bounding boxes without viewing frames.

<a id="implementation-how-a-refusal-is-shaped-decides-whether-a-title-can-act-on-it"></a>

#### How a refusal is shaped decides whether a title can act on it

`MC_netConnect(cb, param)` has two documented, non-interchangeable ways to say
no. Returning an error means the callback is never called. Returning **0** means
the callback reports the outcome: `void cb(M_Int32 error, void *param)`, with
`error` 0 or `M_E_ERROR` (-1). Both are legal for a handset without coverage.

This platform refused at the call, which for one local title equals no answer:
it asks once, is refused, calls `MC_netClose` twice, and sits on its "waiting for
the server" screen for twenty thousand ticks. It is waiting, not looping:
`MC_knlCurrentTime` is called **92 times in 20,000 ticks** (no timeout of its
own), and its only other kernel slot is the frame timer re-armed once a tick.

A radio reports failure when it finishes trying, so the callback form is what a
handset does. A caller registering a callback now gets `0` and, at the next
service point, `cb(M_E_ERROR, param)`. **A caller passing no callback keeps the
synchronous error**, since the return value is its only possible answer; titles
that never register one are unaffected, which made the change safe without
running them. As with the record database, a refusal is a value the game
believes, and its shape matters as much as its content.

**Only one of the two network surfaces had this problem.** Titles reach the
network through the WIPI C table or Java's `org.kwis.msf.io.Network`: the
stalled title uses the C table, and the one whose documented row is a `△` uses
the Java class. The specification makes `static int connect()` **fully
synchronous** — 0 if access is already available, 1 if just established, -1 on
failure — with no callback, so returning -1, as the native does, is the whole
contract.

A boot probe across the local KTF set finds only three archives showing anything
network-shaped, one of them a resource file whose name contains "NET". That is
a floor, not a census: the stalling title's network prompt sat behind more
navigation than a boot window reaches.

<a id="implementation-an-unimplemented-table-call-names-its-call-site"></a>

#### An unimplemented table call names its call site

The WIPI C interface is an array of table pointers the game indexes, so a table
number never appears in guest code. A failing call into an unimplemented table
now reports the guest address it was called from, as unimplemented kernel slots
already did: the stub returns with `bx lr`, so LR still holds the caller.
`table 1 function 1 is not implemented` alone cannot be investigated; with the
site it is an address to disassemble.

Tables 2, 3, 5, 7, 9, 10 and 11 are misc, graphics, the record database, the
database (later identified as the filesystem; see
[The two storage tables](#implementation-the-two-storage-tables)), UI
components, media and net; **table 1 is the utility table** and **table 4 the
input method**, both identified below. The first title to reach table 1 does so
from a content-download prompt. The first captured call site:

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

The call goes through one of RVCT's `_call_via_rN` veneers (a table of `bx rN`
at the image's end), which is why LR names the caller. `interface[0]` is table
1, matching the error. Function 1 takes a signed 16-bit argument and **returns
a 16-bit value the caller stores** — a getter or converter, not a download,
which would not answer synchronously in a halfword. A static read alone was not
enough to answer the call: a title believing a fabricated answer fails
unrecognisably later, worse than a `tick_error` naming the call.

**Three readings agreed.** Every named table sits exactly one past the original
runtime's zero-based numbering (misc, graphics, the database, UI components,
media and net all line up), and the entry before misc there is the utility
table. The specification's utility section lists `MC_utilHtonl`,
`MC_utilHtons`, `MC_utilNtohl`, `MC_utilNtohs`, `MC_utilInetAddrInt`,
`MC_utilInetAddrStr`, the order of the original runtime's table. And function 1
takes and returns a halfword — the shape of `MC_utilHtons` alone; converting a
port number belongs in front of a download.

Table 1 now answers those six. The four byte-order conversions are one swap on
a little-endian host but stay separate cases, because a sixteen-bit swap of a
thirty-two-bit register gives a plausible wrong number. `MC_utilInetAddrInt`
refuses an unreadable address with the specification's `-1` rather than
inventing a host, and the original runtime's seventh entry — a vendor hash with
no published prototype — keeps failing with its call site. No local archive
reaches this table in a 400-tick boot; the specification and the call site
closed it.

**A stubbed table records its call site too — the only place that address is
recorded.** Tables 5-6, 8 and 12-17 are stubbed in the original runtime, so
calls are accepted, counted and answered with zero, leaving no error for the
site to ride on; `wipic stub table 4 function 3` names a number absent from
guest code. The count now carries the address in the ` @0x` form the
diagnostic counter collapses when the name budget runs out, so sites still sum
under the old name and a looping caller cannot spend the budget on one boundary.
`wipic stub ` is one of the prefixes whose detail survives the name limit.

The first observation this way: a title reaching its play screen calls **table
4 functions 3 and 4, once each**, with no symptom and no `tick_error`.
Disassembly says what the pair is for, and one probe says what the table is:

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

Both take **no arguments** (the call register holds the function pointer).
Function 4 answers a base and function 3 a count; the caller walks
`base + index*4` comparing each word against four five-byte names copied from a
twenty-byte structure, storing each match's index — enumerating something the
handset knows and looking up its own four names.

**The four names settle it.** They arrive through a static slot, so the image
does not hold them searchably, but the caller's stack does at the call. A
temporary probe dumping the words at `sp` and following any that point at
printable bytes prints the structure:

```
[sp+0x4]=0x200ffedc -> "KO\0\0\0EN/S\0EN/L\0N123\0"
```

`KO`, `EN/S`, `EN/L`, `N123` — five bytes each, the stride. That is the **input
method's** vocabulary alone: the specification defines automaton modes as an ISO
639 code, with `/S` or `/L` where a script has case, and `N123` for digits.
**Table 4 is the input-method block**, placed as on LGT because the
specification prints the `MC_im*` functions at the end of its graphics section,
so the block follows graphics on both platforms.

Two entries are answered and three are not, as the evidence supports. Entry 4
is `MC_imGetSupportedModes` — the block's only function returning `M_Char**` —
and entry 3 `MC_imGetSurpportModeCount`, both argument-free as the call site
showed. `MC_imSetCurrentMode`, `MC_imGetCurrentMode` and `MC_imHandleInput` are
among entries 0 to 2; **this vendor's order is not the specification's** (which
puts the count first), and no local title calls them, so they stay counted stubs
with call sites. The modes answered are LGT's four, because the specification
fixes the vocabulary.

Answering zero had been safe — a zero count runs the loop no times, as on a
handset without an automaton — but left the title's four mode slots at its
`0xff` memset fill. They now hold indexes, and driven to its world map the title
calls no other entry of the table.

**Table 13 is next: four titles call it at boot.** A sweep of all 32 archives at
400 ticks finds `wipic stub table 13 function 0` in four, three pairing it with
function 2 and one with function 1 — the original runtime's three-entry shape
for that table. Two call sites sixteen bytes apart in one title:

```
ldr r0, [r3]        ; table13[0], no arguments
bl  _call_via_r0    ; and the answer is not read at all
...
ldr r0, [r3, #8]    ; table13[2], no arguments
bl  _call_via_r0
str r0, [r4]        ; kept, in a global structure at +0x810
```

Entry 0 is a void call made on the way in; entry 2 answers one word that is
kept. The probe that named table 4 did **not** name this one across three
titles:

- **All three entries take no arguments**; the registers hold different junk in
  each title. A second title's entry 1 has the same shape.
- **Nothing identifying is on the caller's stack.** One title has a pointer to
  `save.dat` / `option.dat` a few words down, suggestive of storage but not
  evidence: the words below a call belong to whatever that function was doing.
- **The four titles share no publisher**, so their common toolchain emitted it.

The specification has no-argument getters in several sections (`MC_fsAvailable`
and `MC_fsTotalSpace` among them), so the shape does not decide. The use would:
entry 2's word is stored at a fixed offset in one title's global structure, and
finding where that offset is read again is the next step. Answering with a guess
is what the record database warns against.

<a id="implementation-reading-a-guest-fault"></a>

#### Reading a guest fault

A fault used to report an address and an opcode — `Thumb instruction 58d4 at
0x17f086: read guest memory at 0x4`. The questions are what was being
dispatched, on which object, from which Java method, and the registers at the
faulting instruction hold all three. `runAOTMethod` now composes the evidence a
guest throw carries when a run ends in an instruction fault, which turned the
first title above from a week of disassembly into an afternoon:

```
regs=[0x1a29b8 0xbc56c 0x0 0x4 …] sp=0x202ffd24 lr=0x180cd5
operands=0x300bd7fc [Lm; runtime=20 guest=20; 0x300cded4 java/lang/String …
frames=[1]=0x12ead1 s.a([Lm;ILjava/lang/String;)V+0x84 …
```

The report is best effort and read-only: a failing probe is dropped and the rest
still arrives. `armcore` already carries the failing context in the run summary,
so the core needed no change.

When the report says what faulted but not why the register held it, stop before
it. `TestLocalBreakpointProbe` in `internal/platform/ktf` takes an archive, guest
addresses and addresses to watch, and prints the registers, naming what each
points at, whenever execution reaches one:

```sh
WFEATURE_BREAKPOINT_ARCHIVE=<abs path> WFEATURE_BREAKPOINTS=0x105b0e,0x105b12 \
WFEATURE_BREAKPOINT_WATCH=0x179198 WFEATURE_BREAKPOINT_CLASSES=fm \
WFEATURE_BREAKPOINT_SAVE=<abs path to a save tree> \
    go test ./internal/platform/ktf -run TestLocalBreakpointProbe -v
```

The disassemble probe says what an instruction does; this says what it held;
the class dump beside it turns an AOT native's address into the archive's
method name.

**It runs on the manual clock, jumping to each next deadline** (`runktf`
without `-play`), and takes a save root — both to make the run reachable at
all: a title seeding itself from the clock branches differently on every
wall-clock run, and a title whose first run ends itself is a different program
on its second, which a probe without a save can never reach.

**Everything is armed before the first guest instruction**, through
`SessionOptions.Debug`, which receives the ARM core as soon as it exists, before
the clock, the screen or the client's entry point. The debugger used to attach
to a finished session, so client initialization and `startApp` were
unreachable, a native that seemed never to run had already run, and an address
written once during startup showed no writer. A start that fails now still
prints its breakpoints and watches, skipping only the class dump, which needs a
session.

**A watch says when as well as who.** Each hit carries the ordinals of its
writer's first and last store, counted across all watched addresses: a host
write clearing a freshly allocated block and the guest store filling it mean a
guest-owned field when the guest wrote last, and the platform wiping under the
guest when the host wrote last. Without ordinals they read identically.

One title divides by zero in its own `paint` twice a frame and paints nothing;
the divisor is an `int` field of its own object, and a watch armed before the
first instruction reports **one writer, the host, zeroing the block at
allocation** — the title never stores to it. The neighbouring field, which the
fault registers had suggested was the divisor, is written by the title and holds
an object. The question became which branch of its own start would have set it.

<a id="implementation-a-second-download-gate-and-the-answer-that-opened-it"></a>

### A second download gate, and the answer that opened it

The subsequent authentication implementation resolves the accessor's embedded
11-digit fallback and applies it to one session. The short inputs in the sweep
below select that full number internally; the fallback also passes when supplied
directly. See [current mechanism](../authentication.md#ktf-certificates-and-identity).

> The gate before these three — a title offering to download the save it already
> carries, because the Java `DataBase` class never looked in the archive — is
> fixed; see [the storage tables](#implementation-the-java-database-never-looked-at-the-archive-and-that-is-a-download-gate).

Another title in the same series opens by offering to download 600KB "to play
the game"; yes reaches its own connection failure, no returns to the title
screen where any key brings the offer back. The archive ships that 600KB — five
data files totalling almost exactly the named figure — downloaded once on the
handset this copy came from.

The decision precedes any look at them. The title opens its 64-byte `prefs`
database, reads all 64 bytes from position zero, closes it, asks
`MC_knlGetSysProperty("PHONENUMBER")`, prints the number through `printk`, and
shows the prompt. It never opens or checks a data file and reads no other
property, so it decides from that record and the subscriber number.

The record remains unreadable. It is not the certificate format the other
title in this series uses (`provision.go`): its trailer checksums do not hold
and the application id is not at its front under that cipher. Nor is it a
repeating-key exclusive-or against a subscriber number: byte coincidences at
periods one to sixteen are at noise level, and forcing an eleven-digit key
yields nothing readable.

**The number is the whole gate.** Sweeping `PHONENUMBER` over the same scripted
route locates the title's branch:

| Answer | Length | What the title does |
|---|---|---|
| `""`, `"0"`, `"911"`, `"0100"` | 0–4 | opens all five data files and plays |
| `"01000"` … `"01000000000"` | 5–11 | offers the download |

The predicate is the **length** alone — `"911"` passes and `"01000"` does not,
so it is not the value, a prefix, or a match against the record. Five digits or
more is a billable subscriber, whose receipt the title then checks (unreadable
here); four or fewer is a handset with no line, which skips to the data it has.

With a short number the run follows the reference behaviour: it opens each
packaged `.dat`, seeks `(0, SET)` then `(0, END)` for the size, seeks
`(-44, END)` and reads the 44-byte footer, then reaches slot selection and
character creation.

```
DBG seek name=char.dat offset=0   origin=0 position=0     size=45677
DBG seek name=char.dat offset=0   origin=2 position=45677 size=45677
DBG seek name=char.dat offset=-44 origin=2 position=45633 size=45677
DBG read name=char.dat want=44             position=45633 size=45677
```

The footer check is why seek must answer the **new position** rather than a
success code: a seek answering zero makes every packaged file look empty, the
other way into the same prompt. `wipicFileSeek` already answers the position.

**The cost is the other title in the series**, which compares the platform's
number with the one sealed into its provisioned certificate; a four-digit
answer stops it on `인증오류 (3001)`. No single number serves both, so the
number became a setting — `WFEATURE_PHONE_NUMBER`, or the server's `-number` —
defaulting to eleven digits for the certificate title. Playing this one takes
one environment variable; [`network.md`](../network.md) has the trade.

The record stays unreadable. A `prefs` written for a handset this platform can
impersonate would be the honest way in; it would go in the save tree under
`db/prefs`, since a saved record takes precedence over the packaged one.

<a id="implementation-a-download-prompt-that-one-saved-record-will-not-let-go-of"></a>

### A download prompt that one saved record will not let go of

One title that ships its downloadable data in the archive opens by asking to
download it again over a phone line that no longer exists; yes reaches its own
"server connection failed", no exits. Nothing is missing — the run opens every
packaged data file and stats each at its exact size — and the emulator refuses
nothing it should not.

**One persisted record decides it, not the archive or the clock.** With an empty
save directory the title reaches its title screen, its menu and, with `-play`,
the game; it writes four small databases, and a second run reads them back
happily. An *older* copy of the first of the four produces the prompt: that one
file in an otherwise fresh save directory reproduces it every time; any of the
other three does not.

The record is a fixed-size array of obfuscated 32-byte entries. A working copy
and the rejected one differ by a keystream repeating every eight bytes across
the body — **the same content under different keys** — except five bytes in one
entry, which decrypt to the low 40 bits of `MC_knlCurrentTime` when written and
also appear in the header: a timestamp, the only semantic difference.

**The title does not compare the timestamp.** Re-reading a fresh record with the
guest clock moved forward days or weeks, including across the header's 31-bit
truncation, never produces the prompt, and setting "now" exactly to the rejected
record's timestamp does not clear it. The predicate is not age, a future date or
a wrapped counter; what else the record carries is hidden by the obfuscation.

- **Storage is not at fault**: the bytes round-trip unchanged, the title accepts
  its own writes across runs, and its packaged files are found at their sizes.
- **Deleting the title's save directory is the only recovery**; the prompt's
  force-delete clears everything and downloads again, which needs the server.
- **A title's own save can trap it** when the way out runs through the network.
  Suspect it whenever a title that reached its menu stops at a start-up gate;
  one run with `-save` pointing at an empty directory answers it.

<a id="implementation-an-earlier-ktf-package-a-module-file-with-no-descriptor-and-no-jar"></a>

### An earlier KTF package: a module file with no descriptor and no JAR

Every KTF archive this platform was built on has an `__adf__` descriptor at the
top level and a JAR holding a `client.bin<BSS-size>` native image. `archive.go`
opens the first and executes the second, and `internal/platform/detect` uses
`__adf__` to name the platform.

A local archive dated 2005 has neither. A single directory named after the title
— with EUC-KR entry names a UTF-8 `unzip` cannot extract — holds a `.mif`, a
`.mod`, a `.sig`, a `.cfg` and the title's resource files, with no JAR or class
file. The descriptor loader stops at `KTF archive has no __adf__` and detection
answers `unknown`, both correctly: this is a different generation of the
carrier's download package. `OpenNative` in `native_archive.go` reads it, and
`IsNativePackage` distinguishes the two from entry names alone.

<a id="implementation-it-is-not-wipi-at-all-this-package-is-brew"></a>

#### It is not WIPI at all: this package is BREW

**This generation was read as an early KTF platform while there was one archive
of it; it is not one.** Two more arrived from QA in 2026-09, and the `.sig`
beside each module is a certificate chain whose subjects say what they are:

```
QCOM BREW Sigtool
BREW Root CA
BREW KTF Carrier Pass Through
```

The format's names follow from that. `.mif` is BREW's **Module Information
File**; `.mod` is the module and `.sig` its signature; the `1fim` trailer is
`mif1` little-endian. The number the information file is named after is the
applet's **ClassID**, as is the first word of the span the loader reads it from.
The entry point takes three arguments and writes through the third:
`AEEMod_Load(IShell *, void *, IModule **)`. The "factory" call on export 2 with
the identifier is `IModule::CreateInstance`, and the table planted below the
image is the AEE helper table, not a WIPI HAL — which is why the slot numbering
never matched: the module calls slot 26 as an allocator where the WIPI kernel
table has it at 20.

The naming stays: the code says "native package" throughout and
`IsNativePackage` still means "this shape rather than the descriptor one",
because renaming a working loader is not worth it. **The specification to
consult is BREW's, not WIPI's**, and anything inferred from the single 2005
archive is one sample's reading until another agrees — the next sections record
two that did not.

<a id="implementation-the-information-file"></a>

#### The information file

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

The span table holds one offset per span and then the end of the last; after
that end is an identity record closing with the four bytes `1fim`, the format's
only self-identifying mark (there is no leading magic), which the parser
validates.

Three span shapes carry marks and are decoded: a 16-bit self-inclusive length,
a MIME name and terminator introduce a typed blob (the icons, `image/bmp`); two
`0xfe` bytes introduce EUC-KR text (the vendor twice, then the title); anything
unmarked is numbers, kept verbatim because most meanings are not established.

**Two had looked established, each from one archive.** `NativeInfo.ModuleSpan`
computed the module's page-rounded length and searched the header for an equal
word, matching the record at tag 6. And `ApplicationIdentifier` looked for a
span word equal to `0x00100000`, the image base for KTF executables, to find
the ClassID beside it. Two later archives show both were coincidences; all three
have the **same header**, values and all:

| tag | 2005 archive | 2005 archive B | 2005 archive C |
|---|---|---|---|
| 4 | `0x03e80001` | `0x03e80001` | `0x03e80001` |
| 5 | `0x00005000` | `0x00005000` | `0x00005000` |
| **6** | **`0x00025000`** | **`0x00025000`** | **`0x00025000`** |

The three modules are 149836, 185540 and 165616 bytes, rounding to `0x25000`,
`0x2e000` and `0x29000`. **Tag 6 is a constant** the first module happened to
round to; the loader computes the rounded length itself and does not need it.
The span word is the same mistake at one bit: the later two carry `0x10100000`
where the first has `0x00100000`, so an exact match finds nothing and the
ClassID beside it goes unread.

Both are undone in "The three readings undone" below. **A number this platform
can compute is not evidence about a file that also contains it.**

Reading the rest of the header needs its record stride, which is not four bytes:
a second size, `0x5000`, sits where the surrounding words decode as tag-and-value
pairs on an unpinned stride, and an aligned-word scan picks up boundaries as
values — one mapped 0x85000 for a module needing 0x25000. The loader reads none
of it and maps only the module's page-rounded length (see "There is no BSS"
below).

<a id="implementation-the-module-needs-no-relocation"></a>

#### The module needs no relocation

The descriptor package's `client.bin` **begins with a self-relocation loop**: it
takes its load address from `pc`, subtracts its link address, and adds that
delta to every word listed in an offset table at file offset 0x88 — hence its
zero-based literals and `armcore`'s "relocates itself" loader.

The `.mod` has no such loop and **no relocation table** — a scan for
ascending-offset runs finds eighteen in the descriptor package's image and zero
here. Its code is uniformly PC-relative:

```
ldr r0, [pc, #n]      ; a link-time offset
add r0, pc, r0        ; the module's own base, position independently
```

Sweeping candidate link bases finds no preferred absolute base. The module is
position independent, loaded rather than relocated, at an address the platform
chooses.

<a id="implementation-the-platform-table-is-planted-below-the-module"></a>

#### The platform table is planted below the module

The module finds the platform in the word immediately **below its own load
address**:

```
ldr r0, [pc, #n] ; add r0, pc, r0   ; its own base
ldr r0, [r0, #-4]                   ; the pointer the loader planted
ldr r1, [r0, #0x68]                 ; one of its functions
bx  r1
```

A loader must map a page under the image and put a table pointer in its last
word — `nativeHeaderBase` in `native_client.go` — a different binding from the
descriptor package, whose entry receives initialization arguments and asks for
interfaces by name.

**The slot numbering is not the one this platform serves.** The module calls
slot 26 with a size, null-checks the result and fills the block with its own
function pointers — an allocator — and slot 27 frees, where the kernel table
here has its allocator at 20 and calloc at 21; nothing of the descriptor
package's numbering carries over. Rather than guess a mapping,
`LoadNativeClient` fills every slot of both surfaces with a distinct trap stub
carrying its number across the supervisor-call boundary, so a run reports the
requested slots in order with each call site's link register. `Serve` answers
one slot at a time: name it, answer it, run again.

A static scan fails here deceptively. A linear disassembly of the `.mod`
**stops after twelve instructions**: offset 0x2c is a Thumb veneer table, and
the module mixes ARM and Thumb throughout. Two scans built on a linear decode
reported zero call sites each, which reads like "the module never calls the
platform". Decode each address independently in both states, or run it.

<a id="implementation-the-start-up-protocol"></a>

#### The start-up protocol

Both halves negotiate through one reference-counted interface shape: an object
is a pointer to a table whose first two entries raise and drop a count and whose
third dispatches. Nothing is named; every step is a number, and the numbers
that matter come from the information file. `native_client.go` performs it in
three calls:

1. **The entry returns a factory.** Called with the platform object, a spare
   argument and an out parameter, the module allocates 0x24 bytes through
   platform slot 26, fills the block's tail with four of its functions, points
   the first word at them, sets the second word (the reference count) to 1,
   calls slot 0 of the platform object, and writes the block through the out
   parameter. The four functions are raise, drop, dispatch and a return, *not*
   the Clet entry points.
2. **The factory creates the title's object.** Dispatching with the information
   file's identifier reaches a class factory that allocates 0xe8 bytes and asks
   the platform object for interface `0x01001001`; that query must succeed,
   because the module checks the pointer it passed rather than the result. It
   then raises the count on both sides and writes the title's object out.
3. **A small integer is an event.** The title's object dispatches into its own
   handler, which switches on 0, 1, 2, 3 and above — the specification's Clet
   event contract. Sending 0 runs the title's code, which asks for graphics and
   file slots within a few hundred instructions.

**The identifier is found by two anchors in one record**: its last word is the
image base and its first is the constant the module's dispatch compares against.
Neither alone suffices — the file holds other round numbers, and the first
record's first word is a `0x1000` the module refuses.

<a id="implementation-two-steps-that-look-like-they-succeeded-when-they-have-not"></a>

#### Two steps that look like they succeeded when they have not

Each cost a session:

- **The out parameter is the answer, not the result.** The factory writes the
  object through a caller-supplied pointer and returns something else; reading
  the result alone sees zero and calls it success.
- **A missing argument reads as a broken module.** The factory rejects a null
  second or fourth argument before doing anything, returning as on any failure;
  called with three arguments instead of four it silently does nothing.

A third is on the emulator side: `armcore.Call` derives a temporary context and
restores the parent by leaving it untouched, so reading r0 from the thread after
a call reports the parent's value — zero on a fresh client. The result must come
from the run summary; this showed a failure as success in the probe's log for
two runs.

<a id="implementation-what-the-title-asks-for-once-it-starts"></a>

#### What the title asks for once it starts

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

It stopped in the title's code dereferencing a null it was handed — an
unanswered slot seen from inside a game. Each row was answered as slot 26 and
the interface query were: name it, serve it, run again.

<a id="implementation-the-surface-is-bounded-and-a-taint-sweep-is-what-says-so"></a>

#### The surface is bounded, and a taint sweep is what says so

Answering one slot at a time never says how much is left; a static sweep does.
The module reaches the platform table through one global — the word at
`ImageBase-4`, loaded PC-relatively everywhere as `base+0x3c` off
`ImageBase-0x40` — so a pass following that value through registers, adding each
`adds rN, #k` and recording each `ldr` off it, enumerates the table. The whole
image yields **sixteen slots**, the entire flat half of the package:

```
0x00 memcpy      0x14 strlen      0x68 allocate    0xbc  ?
0x04 memset      0x20 sprintf     0x6c free        0xc0  current application
0x08 strcpy      0x64 ?           0x8c ?           0xcc  ?
0x0c strcat      ...              0xac milliseconds 0xdc ?
```

The sweep decodes each address independently (a linear decode stops after twelve
instructions; see above), and the base word is read from ninety-four sites with
no other offset off that base ever read, so `base+0x3c` is all the loader must
plant.

Each slot's meaning came from its call sites:

- **`memset` then `memcpy`.** One function clears `0x1b` bytes at a stack
  address through slot 4, then copies ten bytes into it through slot 0.
- **`strlen`.** Slot 5's answer on a buffer is compared against 14, and the
  module's write wrapper calls it when its caller passes no length.
- **`sprintf`.** Slot 8 takes a buffer, a format string from the image and two
  halfwords; it renders through the descriptor package's WIPI C formatter, with
  variadic arguments walked from r2 through the stack.
- **A millisecond clock.** Slot 43 is read, saved and re-read in a loop spinning
  while the difference is below 2,000, and seeds the module's generator.
- **"Which application am I".** Slot 48 is called from thirty-nine sites and its
  result indexed at `+0x20`, `+0x28`, `+0x2c`, `+0x4c`, `+0x50`, `+0x54`,
  `+0x84` — the offsets the title's object uses on itself, so the answer is that
  object. The module asks the platform for `this` rather than carrying it.

<a id="implementation-the-three-surfaces-and-the-objects-the-module-asks-for-by-number"></a>

#### The three surfaces, and the objects the module asks for by number

The package exposes three kinds of surface, and distinguishing them makes a call
log readable:

1. **The flat table below the image**, above.
2. **The object handed to the entry.** Its first three vtable entries raise a
   count, drop it and answer a query by number; `+0x10` fills a display record;
   `+0x2c` takes an interval, a function and a context.
3. **One object per interface number.** The module queries five —
   `0x1001001`, `0x1001002`, `0x1001003`, `0x100100b`, `0x1002000` — and one
   more, `0x18000fe`, from the object it already holds. Each answered query gets
   a trap table of its own, so what the module calls on one is recorded apart
   from every other.

`0x1001001` is the screen, queried by the class factory while building the
title's object (checked through the passed pointer, not the result).
`0x1001002` answers free memory and whether a size can be had; the module walks
down from 64KB by halves and keeps the largest block both it and the allocator
grant. `0x1001003` is the title's files. `0x1002000` and `0x100100b` each take a
function and a context; one registered function clears the flag the module's
modal wait spins on, so they are event sources it waits on.

<a id="implementation-a-file-is-an-object-and-the-modules-own-wrapper-names-its-methods"></a>

#### A file is an object, and the module's own wrapper names its methods

The file interface returns an object rather than a number, so one trap table
serves every open file and a handler distinguishes them by the receiving object,
as the module does. The module's file wrapper names the methods by the only
reading its call shape allows:

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

One module loop drives both read and write, choosing by a caller flag. A first
pass served `0x14` as a second read: the title loaded its data and then failed a
gate that tests whether it can write.

<a id="implementation-the-gate-that-reads-as-a-memory-error-and-the-write-behind-it"></a>

#### The gate that reads as a memory error, and the write behind it

With every file call answered but the write, the title booted, loaded its five
data files, registered its frame, drew this once and never moved:

> FS 메모리 공간이 부족합니다 / 최소 2KB 이상의 메모리를 확보한 뒤 다시 시작해
> 주십시오

The message names memory but concerns the file system. Its code is a byte the
title sets in itself, and a **watchpoint on that byte** named the setting
instruction in one run, on a branch the message does not suggest: clear 64
bytes, write them to a scratch file thirty-two times, and check each write
returns 64 — 2KB. A write answering zero is a handset with no room.

The start-up gate is a file the title creates, writes 2KB to and deletes.
Opening a missing name for writing must create it, and what is written must read
back — one requirement, which `docs/skvm.md` records from another direction.

<a id="implementation-the-title-does-not-have-a-loop-of-its-own"></a>

#### The title does not have a loop of its own

Its start event ends by handing the platform an interval (48ms for this title),
a function and a context through the entry object's `+0x2c`; everything after
happens inside that function, which reads the clock, steps its state machine,
draws and re-arms as its last act, so the schedule replaces rather than
accumulates. A Host that only starts the title sees it load its data and stop.

Because the re-arm replaces the schedule, a Host tick must read it *after*
running the title's other callbacks. The sound and timed services are answered
first and both reach the title's listener; a listener that re-arms would leave
an earlier-read pointer naming an orphaned struct, and the tick would stamp its
next due date there and call a function the title has moved on from.
`NativePlatform.Tick` reads `platform.frame` after the two advances. No local
title re-arms from a listener; this keeps the contract rather than fixing an
observed fault.

<a id="implementation-what-the-title-draws-with-and-the-table-that-named-the-colours"></a>

#### What the title draws with, and the table that named the colours

The screen interface's `+0x14` takes a rectangle of four halfwords, two colours
and a mode. The colour format came from the module's data: one call site walks a
36-word table drawing 1x1 rectangles, holding `0xffffff00`, `0x0000ff00`,
`0x00ff0000`, `0xff000000`, `0x00ffff00`, `0xff00ff00`, `0xffff0000` and zero —
white, blue, green, red, cyan, magenta, yellow, black — so a colour is
`0xRRGGBB00`, which the call site alone could not show.

`+0x1c` ends a frame: it is the only call in a frame that draws nothing.

**The title draws its own text**: there is no glyph call; the version string and
menus are runs of 1x1 rectangles through `+0x14`, so the first screen is legible
without any font work.

<a id="implementation-the-sprites-are-windows-bitmaps-and-the-title-builds-them-itself"></a>

#### The sprites are Windows bitmaps, and the title builds them itself

The screen's `+0x18` is the blit, and for a long while it arrived with a null
pixel source. A breakpoint at the call site showed the null already present;
following it up two frames of the title's code reached an array of image
pointers filled once at startup. **A watchpoint on one slot named the writer in
a single run**: `platform slot 25`, called as

	create(0x1004001, data, out, out)

with a number from the interfaces' family: the slot is a factory and
`0x1004001` a class. Dumping `data` at the call:

```
42 4d 18 0f 00 00 ... 36 04 00 00 28 00 00 00 36 00 00 00 32 00 00 00
01 00 08 00 00 00 00 00 ...
```

"BM", a file size, pixels at 0x436, a 40-byte information header, 54 by 50, one
plane, eight bits, no compression — a **Windows bitmap** with its 256-entry
palette in the 0x400 between the headers. The title decodes its own graphics
archive and hands the platform the format every handset of the era could decode.

- **The object's first word points at the bitmap.** One of the title's paths
  indexes the bitmap through it without calling the platform, so the object must
  be guest-readable.
- **Magenta is not drawn.** Eleven of the fourteen title-screen images have
  `0xff00ff` in their top-left corner, and drawing it opaque boxes every sprite
  in magenta. The blit carries no colour and always passes mode 7, so the
  artwork is the only evidence.

<a id="implementation-the-title-takes-keys-as-events-and-its-own-menus-decode-them"></a>

#### The title takes keys as events, and its own menus decode them

The switch in the title's event handler compares the event against 0, 1, 2, 3,
then `0x101`, `0x102` and `0x404`. The first two store a code and pass it to the
current screen — key pressed and released; `0x404` is compared and ignored.

The title's key handler compares against `0xe030` and `0xe035`, so a key is
`0xE000` with a character in the low byte: digits are their own characters, and
the five-way pad is the ring around the 5:

```
0xe032 up      0xe034 left    0xe035 select    0xe036 right    0xe038 down
```

Neither KTF generation needs the pad rule the other two platforms carry (`lgt.md`
"A release stops a character the pad has moved on from"). Measured: two titles
driven into their world with the release of an already-abandoned direction
delivered in one run and withheld in the other — **every frame byte-identical**.

There is no repeat event. A browser forwards the OS repeat of a held key;
sending it as a press would give the title a second press without a release,
which no handset delivers and which a title reading presses as taps acts on
twice. A repeat therefore delivers nothing here, and a held key stays held
because the title tracks press and release itself. WIPI Java's `keyNotify`,
which takes the type, still receives a repeat — the handset's own "600:250"
generated by the Host, not the browser's (`session.md`, "A held key repeats on
the handset's clock"; `lgt.md` "A Host repeat is not a second press" has the
LGT screens).

A shaped sweep proved the key space: codes 0 to 63 move nothing, and a sweep
from `0xe000` upward found exactly one code that changed the screen, opening the
title's menu. The route into the game is then the title's own:

```
select   the title screen opens its menu
select   the menu enters its first item
left     the two choices under it, and the second is the new game
select   a difficulty
select   and the game starts
```

The acceptance run drives that route, since later screens are reached only by
playing, and it verifies the input contract: a wrong key code is
indistinguishable from a title ignoring keys.

<a id="implementation-the-sound-is-smaf-which-this-project-already-decodes"></a>

#### The sound is SMAF, which this project already decodes

The title plays sound through an interface it queries by number, handing it a
memory block. Dumped at the call:

```
4d 4d 4d 44 00 00 00 c6 43 4e 54 49 ... 53 54 3a 75 6e 74 69 74 6c 65 64
```

"MMMD", a length, a `CNTI` chunk, `ST:untitled,VN:PsmPlayer V5` — a **SMAF**
file, the descriptor package's sound format. No new decoder was needed, only
three calls: `+0x0c` sets the clip, `+0x10` starts it, `+0x14` stops it. No
length is passed; a SMAF file states its own in the four bytes after its magic.

**A clip's end must be reported.** The module sets a flag when a sound starts
and clears it only in the function its listener calls, so without "finished"
the title believes its music still plays. The listener recognises only the pair
(1, 14), the whole evidence for those numbers.

A second source is started with a duration in milliseconds and *waited* on: the
title sets a flag and clears it only when the source reports 2 or 3. Both are
answered, which is why this platform delivers events to the module's registered
listeners rather than only collecting them.

<a id="implementation-the-last-of-the-flat-table-and-the-objects-nobody-reads-the-answer-to"></a>

#### The last of the flat table, and the objects nobody reads the answer to

- **Slot 47 is a second deallocator.** The call site chooses between it and free
  by a flag kept beside each entry, and passes a factory-built object: objects
  are destroyed through the slot that knows them, everything else through free.
- **The screen's `+0x10` is a status line**: a flag word and a message (`WAIT..`
  on this title). The title draws its own screen and this is handset furniture,
  so it is recorded, not drawn.
- **The screen's `+0x24` is called once a second** while running — a handset's
  "keep the backlight on".
- **`0x18000fe` fills a 38-byte record** the title turns into a 26-character
  identifier, padding empty space with `X`, then hashes and compares it against
  a hash computed the same way, so the content decides nothing alone; an empty
  record would be the same identifier on every handset, so the subscriber number
  this emulator already answers with goes in.

<a id="implementation-the-header-sections-have-a-stride-and-two-numbers-found-it"></a>

#### The header sections have a stride, and two numbers found it

The two header sections were carried verbatim for lack of a stride. The stride
is **eight bytes, with the tag in the last halfword**: the module's page-rounded
length and the size beside it land at the same place in consecutive records
with consecutive tags:

```
value 0x00060001  extra 1  tag 2
value 0x03e80001  extra 0  tag 4
value 0x00005000  extra 0  tag 5
value 0x00025000  extra 0  tag 6   <- the module's length, rounded to a page
```

An aligned word scan reads a record boundary as a value, which is how an earlier
pass mapped 0x85000 for a module needing 0x25000. The loader then read tag 6 and
checked it against the length it computes itself, since a disagreeing tag 6
means a misread package. Only tag 6 was treated as established; the rest are
decoded and carried. (Later archives showed tag 6 is a constant; see
[The three readings undone](#implementation-the-three-readings-undone-and-what-was-behind-them).)

<a id="implementation-how-a-host-holds-it"></a>

#### How a Host holds it

The package reaches every Host through their shared layer, with no per-Host
branch:

- `internal/platform/detect` names it `ktf`: same vendor and platform, different
  package, and naming it otherwise would send a Host looking for a nonexistent
  loader. The discriminator — a `.mif` beside exactly one `.mod` — is consulted
  last, after every descriptor marker *and* the class scan: a lone `.mod` is too
  common an extension, and the pair is a shape a MIDlet repacked without its
  `.msd` could wear by accident, so what a class file links against outranks it.
  A real package of this generation carries no class files, so the order costs
  nothing.
- `ktf.NativeSession` has the descriptor `Session`'s outer shape: start, tick,
  take the frame, send keys, search memory. A tick runs the title's frame if
  due, and the wait it answers is the remainder of the title's own interval, so
  a Host honouring it runs the game at its written speed.
- `internal/session` chooses the generation inside its KTF case, putting the
  earlier one in the browser, the CLI and everything built on it at once.
- Identity comes from the module information file: its name for a label, its
  application number for the save directory. That number is the one the
  module's class factory is asked for, so a mismatched package would not start.
- The cheat engine attaches as for the descriptor package, over its own core.
  This title keeps its whole state in platform-allocated memory, so `arena` is
  where a search for a health bar finds one.

`runktf` takes both generations and refuses by name the options meaningful only
to the other: no AOT method bodies to profile or symbolize, no descriptor to
inspect.

`-route` works on both: a route depends on the screen and the keys, not the
packaging, so `internal/route` asks a platform for four functions rather than an
interface. The earlier package differs only in the frame source and tick cost:
`NativePlatform.FrameDigest` fingerprints the screen in place so a wait can ask
every tick without copying, and `Stalled` means no deadline is pending — nothing
left to run the title, which has no loop of its own. A `mark` names a waypoint
rather than a measurement start, since there is no profiler to reset.

<a id="implementation-what-a-run-keeps-of-the-calls-it-traps"></a>

#### What a run keeps of the calls it traps

Every platform-table call is trapped, and all were once kept — affordable for a
probe, not a session: about seven calls a frame at rest and 237,801 over the
acceptance route, at 48 bytes a record, cost a player roughly 25 MB an hour,
never released.

The list served two questions of different shape. *How often was this slot
called, and from where first* decides whether a slot matters and is bounded by
the slot count (37 on the local title), so `NativeClient` keeps it in a map that
cannot grow. *What did the module do, in order* is a start-up diagnostic, so the
ordered log keeps the first `maxNativeRecordedCalls` records, as `Trace` and the
blit list do, plus any failing call — the one a reader opens the log for, which
ends the run anyway.

`Calls()` is a window, `CallCount()` the total, and `SlotSummary()` counts every
call and whether each slot was answered. After the split on the local title the
ordered log settles at its bound, the heap stops moving between tick 1,000 and
3,000, and the slot count is unchanged.

<a id="implementation-a-save-is-a-file-the-title-writes"></a>

#### A save is a file the title writes

The title writes its settings and save slots as ordinary files beside its data,
so they go to the save store's `fs/` space, as the descriptor package's guest
filesystem does.

- **A save wins over the package's copy of the same name.** The title ships a
  settings file and writes to it; preferring the archive would restart every
  session from shipped settings.
- **A write reaches the store immediately**, not at a close that a title killed
  mid-session never performs.

<a id="implementation-what-is-still-open"></a>

#### What is still open

- **Nothing on this title's route still traps.** Every slot reached through its
  menus into its first map is served; the next unknown will appear as a trap in
  the acceptance run's table, which marks each slot `served` or `trap`.
- **Keys with no code.** The two soft keys, send and the volume pair are not
  mapped: nothing establishes this handset's numbers for them, and a value sent
  as itself cannot be told from a wrong mapping. The page's `Menu` button is the
  left soft key (`session.md`, "The keypad carries one soft key"), so it is inert
  on this package alone rather than guessed. The module compares against a set
  per screen — `0xE021`, `0xE02B`, `0xE030` and `0xE03E` all cancel where
  accepted — so several keys lack a Host name.
- **The information file's other tagged fields.** Four fields decode; only the
  module length was understood. `0x5000` under tag 5 is not a BSS size (next
  section).
- **A repro route runner.** `internal/route` then drove only the descriptor
  package's sessions; this one took a key script on the command line (later
  added; see [How a Host holds it](#implementation-how-a-host-holds-it)).

<a id="implementation-there-is-no-bss-and-mapping-nothing-extra-is-the-measurement"></a>

#### There is no BSS, and mapping nothing extra is the measurement

An earlier pass mapped a fixed `0x8000` past the image because the information
file names a second size beside the module's and neither could be read safely.
The allowance was removed on evidence:

- The module holds **no absolute address** — it is position independent.
- It reads **exactly one word outside its image**, the platform table pointer
  below it; a sweep finds no other offset off that base.
- It asks the platform to allocate everything it works on, including the object
  holding its whole state.
- Runs with no allowance and with `0x8000` are byte-identical through start-up,
  data loading and three thousand frames: same draws, presents and call counts.

The second size is not a BSS size for this module. A module of this generation
needing space past its image would fault on first access, naming the address.

<a id="implementation-a-later-module-refuses-the-platform-before-it-does-anything"></a>

#### A later module refuses the platform before it does anything

Two archives QA sent in 2026-09 have the same package shape but **a later module
generation**, and stop before any of the above matters. The module's first
instructions are not the 2005 archive's dispatcher call:

```
e59f2038  ldr  r2, [pc, #0x38]   ; a link-time offset
e08f2002  add  r2, pc, r2        ; its own base, position independently
e5122008  ldr  r2, [r2, #-8]     ; the word two below the image base
e3520b40  cmp  r2, #0x10000
13a00004  movne r0, #4
1a000007  bne  epilogue          ; return 4, having written nothing
...
e3a00018  mov  r0, #0x18         ; selector 0x18, where 2005 uses 0x14
```

The loader planted the table pointer at `ImageBase-4` and left `ImageBase-8`
zero, so the compare fails, the entry returns without touching its out
parameter, and `ReadEntryRecord` reports `KTF native entry wrote no record`.
**It is a version gate**: the module asks which AEE it runs on before handing
over its interface, wanting `0x10000`. Patching the branch out in a copy carries
both modules through the entry into platform slot 0, which the 2005 archive
never calls — a table this platform had not met.

Three things separate these archives from the loader's original, in the order
hit: the header word mistaken for a module length, the exact match against the
image base, and this gate. Only the gate is a real question; the other two are
readings to undo.

One of the two has a `.mif` with **two bytes past its trailer** — `1fim` then
CR LF, from a text-mode move before packing. `ParseNativeInfo` required the file
to end with the trailer and refused. The bytes are in the distributed archive
and its zip CRC is correct, so this is a shape to tolerate, not damage.

Both archives were then kept in an ignored diagnostic directory with a note on
each. The following sections record answering the gate.

<a id="implementation-the-three-readings-undone-and-what-was-behind-them"></a>

#### The three readings undone, and what was behind them

All three are undone and both archives run. Each hid the next: refusing the
information file prevents reading a span, refusing the span prevents reaching
the entry, and stopping at the entry hides the table beyond.

- **The trailer's two extra bytes.** `ParseNativeInfo` trims trailing CR and LF
  before looking for `1fim`; anything else past the trailer is still not this
  format.
- **The module's mapped length.** `nativeMappedSize` maps the module's own
  page-rounded length without consulting the file — the value the earlier pass
  computed before comparing it to a tag that matched by coincidence (see
  [The information file](#implementation-the-information-file)).
- **The ClassID.** `ApplicationIdentifier` finds the applet record by shape —
  five words whose second and fourth are zero and third is `0x3e8` — and takes
  its first word: `0x0102e0a9`, `0x010345ec` and `0x0103267f` in the three
  archives, **each module carrying its own as a literal**, which confirms the
  reading. The record's last word, the former anchor, is an applet property:
  `0x00100000` in the 2005 archive (coinciding with this platform's image base)
  and `0x10100000` in the later two.

<a id="implementation-the-version-gate-is-a-word-the-loader-had-left-zero"></a>

#### The version gate is a word the loader had left zero

The loader now plants two words below the image: the table pointer at
`ImageBase-4`, and `0x10000` at `ImageBase-8`. The later module compares that
word against `0x10000` and returns 4 without writing its out parameter when it
differs — a question the platform never answered, not a refusal. The 2005
archive never reads the word.

<a id="implementation-a-module-reads-above-its-own-stack-pointer"></a>

#### A module reads above its own stack pointer

Past the gate, one module faulted immediately: it copies a fixed `0x400` bytes
from a frame declared as `0x58`, and the guest started at the very top of its
stack mapping, so the copy ran off the end.

On the handset an applet is entered from the runtime's dispatcher, whose frames
lie above its stack pointer; the bytes are junk and never read back.
`nativeStackHeadroom` leaves a page of the mapping above the entry stack pointer
to stand in for that caller. **A guest reading uninitialised memory does not
fault on a real device**; starting flush against a mapping's end makes every
such read a fault.

<a id="implementation-the-resource-file-and-the-number-that-indexes-it"></a>

#### The resource file, and the number that indexes it

The later modules do not open data files; they ask the entry object for a
numbered resource from a named one:

```
ldr  r0, [r7, #0x10]      ; the object
movs r3, #6               ; the kind
ldr  r2, [pc, #0x40]      ; 5001, the number
ldr  r6, [r1, #0x48]      ; the loader
ldr  r1, [pc, #0x40] ; add r1, pc   ; "rpg_org.bar"
bl   veneer
...                       ; allocate 0x400, copy 0x400 from r0 + 0x1c
ldr  r2, [r1, #0x50]      ; and give the block back
```

**The call settled the format.** Item 5001 is `0x41c` bytes and the module
copies the remaining `0x400` from offset `0x1c`, so the loader returns the item
verbatim and the leading `0x1c` is the title's business: a MIME name, the same
typed blob as the information file's icons (a self-inclusive 16-bit length,
`application/octet-stream`, a terminator). The module reads the length's low
byte itself to step over it.

`.bar` is a header, a group table, an index table and the items:

```
0x00  a version halfword, 0x11 in all eight local files
0x06  how many groups
0x08  where the group table is, and how long it is
0x10  where the index table is
0x14  how many items there are
0x18  where the first item is, repeating the index table's first entry
```

The index table holds one offset per item then the end of the last, which is the
file length in all eight. **A group is a run, not a list**: four halfwords — a
kind, the first number covered, how many *further* numbers it covers, and the
item index of the first number. A number's item is its distance into the run
plus that index; thirteen groups cover a hundred and two items. Reading the
third halfword as an item count empties the group holding `0` — exactly the one
the module asks for.

<a id="implementation-four-bits-a-pixel-a-second-copy-a-second-clock-and-a-comparison"></a>

#### Four bits a pixel, a second copy, a second clock, and a comparison

The later module's other needs existed in another form:

- **Four bits a pixel.** The factory decoded eight and twenty-four. At four, a
  byte holds two pixels with the left in the high nibble, and word padding must
  be computed from bits — a byte count loses an odd width's last pixel.
- **A second copy at `0xc8`.** One call site measures a string with the length
  slot, allocates one byte more and copies the string's length into it; memcpy,
  memmove and strncpy behave the same there, so which one the carrier used is
  undecidable and irrelevant. **All text the title draws passes through that
  block**; unanswered, a dialog reads as an empty bordered box.
- **A second clock at `0xb0`.** All fifty-nine call sites subtract a saved
  reading, compare against their own interval and save a fresh reading when it
  passes. It takes nothing: the module reaches it through the veneer that calls
  through `r0`, leaving no argument register.
- **A comparison at `0xcc`.** Two call sites compare four bytes of a sound
  resource against `MMMD` and `cmid`.

<a id="implementation-zero-is-what-worked"></a>

#### Zero is what worked

The file interface answers **zero on success**; the opposite reading had the
title asking to authenticate over the network. The later module calls the
information slot and, on non-zero, asks slot `0x24` what went wrong and returns
that as its failure — the specification's sense throughout. The 2005 module
never checks the answer, so its polarity was unknown. `nativeFileLastError` is
that slot; `nativeFileFailed` is the one non-zero value reported, since the
module returns it without comparing.

Every object in this protocol is a pointer to a table whose first two entries
raise and drop a count, so `InterfaceObject` answers those two for any interface
it builds — unless the platform already gave that number its own handler at that
offset, which marks the object as shaped differently.

<a id="implementation-a-certificate-and-the-object-both-later-archives-create"></a>

#### A certificate, and the object both later archives create

Both later modules create one more object from the **second ClassID in their
information file** — `0x103028a` in both, from two different publishers, so it
is the carrier's.

One call on it is reached, handed the archive's own `gbxcerti.dat` (74 bytes,
loaded through the resource loader and freed right after). The answer is a
small signed status the module maps to one of ten messages: **zero leaves the
title on its own screen; one sends it to the dialog offering network
authentication.** Zero is acceptance, as a handset with a valid certificate
answers.

<a id="implementation-where-the-two-stand-after-the-first-pass"></a>

#### Where the two stand, after the first pass

Superseded by "Where the two stand, after the second pass"; kept because the
sections between answer what stopped each.

- **The first later archive reaches its title screen.** Start-up, data,
  publisher screen, artwork and text appear, with no trap on the route. A key is
  delivered and **taken** — the "press any key" line stops being drawn, which no
  keyless run shows — but the next screen does not arrive.
- **The second later archive starts and registers no frame.** It passes entry,
  the factory, its start event and its files, then fails inside a swallowed
  initialisation: it asks an unknown display interface for a font's line height,
  centres three text lines with it, and draws through that interface's slots
  rather than the 2005 title's surface. That interface — a font metric, a text
  draw, and a manager object told about both — is needed next and not guessable
  from the stopping point. Call sites: module `0x10c230` and `0x10c2f0`.

<a id="implementation-the-file-interface-answers-the-generation-that-asked"></a>

#### The file interface answers the generation that asked

The test call — "is this name there" — is read **in opposite senses by the two
generations**, both established by running them.

The later modules take **zero** as present. One reads its authentication marker
on zero and otherwise shows "인증 요청중입니다 / 이 요청은 한번만 / 수행됩니다" —
an unneeded network authentication for a file in its own archive. The other
loads its data on zero. The 2005 module takes **non-zero** as present: it skips
the create beside it and opens.

Flipping the answer for the later two costs the 2005 archive its save and seven
eighths of its drawing — 221342 draws to 3010, 65 images to 8, `kenviron.cfg`
never written — so the generations genuinely differ.

**The answer follows the asking module's generation, which the module declares.**
`AsksForInterfaceVersion` looks for the version gate in the entry's first sixteen
instructions: a load of `[base - 8]` and a compare against `0x10000`, matched as
instructions rather than encodings because the base-load offset varies. The
module is asking which AEE it runs on, and the interface's meaning is a property
of that AEE.

The information call answers zero on success for both, since the 2005 module
never reads it.

<a id="implementation-a-later-module-drives-itself-by-posting"></a>

#### A later module drives itself by posting

The 2005 module registers a frame (interval, function, context) and does
everything inside that function. **A later module registers nothing**; its
start-up ends:

```
ldr  r2, [r0, #8]         ; a parameter of its own
adds r3, r6, #0           ; 0x7009, an event number of the title's own
ldr  r0, [r0, #0x10]      ; the object the entry was handed
ldr  r4, [r1, #0x54]
movs r1, #0               ; no flags
bl   veneer               ; (this, flags, class, event, w, dw)
```

The class named is **its own** ClassID, and the event returns to the handler
keys go to: this generation runs by posting to itself, and dropping the post
leaves a started title with nothing to do — the "starts and registers no frame"
symptom.

- **The post is delivered on the next tick, not inside the call**, since the
  module posts from inside the running handler and immediate delivery would
  re-enter it on its own stack.
- **A tick runs whether or not a frame is registered.** A Tick returning early
  without a frame never delivers the post that creates one. Once it arrives,
  this title registers its own 80ms frame and the existing loop runs.

Slot `0x30` takes a function and a context and **no interval**: a request to be
called once more, delivered the same way and not re-armed; the title asks again
when it wants another.

<a id="implementation-two-colours-recorded-rather-than-drawn"></a>

#### Two colours, recorded rather than drawn

The screen's slot `0x28` takes an item number and a colour; a later module sets
item 2 to `0xffffff00` and item 1 to zero (the palette's RGB-plus-spare word)
before drawing text, reading neither answer. They are recorded because
**nothing on this platform then drew text for this package**: the 2005 title
draws glyphs as 1x1 rectangles, and the screen's text slot is where a later
one's dialogs go. Recording keeps the colours for when that gap closes.

<a id="implementation-where-the-two-stand-after-the-second-pass"></a>

#### Where the two stand, after the second pass

Superseded by "after the third pass" below.

- **The first later archive reaches its title screen, with sound.** Reading the
  test the later way loads `lcdinfo.dat` and its certificate and plays its
  opening music: four clips set, two started, two stopped, 400 frames presented,
  no trap. A key reaches it — the handler stores the code at `+0x960` of its
  object and answers 1 (disassembly and run agree) — but the state machine
  reading that code does not act. **Nothing is unanswered, so the next step is
  reading the title's state machine rather than watching for a trap.**
- **The second later archive starts, loads and presents.** It authenticates
  against its own archive, reads its handset record, loads eighteen resources
  from its resource archive, registers its 80ms frame and presents twice. Its
  screen is nearly empty: it blits twenty regions from a surface of its own it
  has not drawn into (re-decoding on every blit changes nothing, so the surface
  is truly empty), and its step function is gated on a flag at `+0x1748` that
  nothing sets. Next: what sets that flag, and the screen's text slot — the
  latter a platform gap.

<a id="implementation-a-key-is-three-events-and-one-title-reads-only-the-third"></a>

#### A key is three events, and one title reads only the third

The 2005 module's handler compares against `0x101` and `0x102`, which this
platform sent. **A later module's handler distinguishes three**, stored in
different places:

```
cmp r2, #0x100 -> strh r1, [r0, #0x18]     ; a slot of its own
cmp r2, #0x101 -> pressed = 1, code at +4  ; the key is down
cmp r2, #0x102 -> pressed = 0, code at +2  ; the key is up
```

Its opening screen reads **only the first**. A one-frame trace (332
instructions) shows, as the fourth instruction,

```
ldrh r1, [r5, #0x18]      ; the slot event 0x100 writes
ldr  r0, [r5, #0x28]
bl   0x11646c             ; hand it to the screen
```

while the two pressed-state getters never run. Sending press and release
without the third event takes every key and acts on none — the "key taken, next
screen never arrives" symptom. `Key` now sends the press then the typed key, and
the release alone. The 2005 module ignores `0x100` (an unknown event answers
zero), and its in-game route is byte-identical with it sent.

The first later archive then plays: title screen, a memory warning, its
save-slot screen reading the shipped `sav.dat` (`SLOT 2 · LV 3 ·
PLAYTIME 00:11:54`), the load prompt, and the game.

<a id="implementation-the-factory-owns-the-picture-because-the-title-frees-it"></a>

#### The factory owns the picture, because the title frees it

The image factory decoded the bitmap it was handed and left the object pointing
at it; the title frees that bitmap **on the very next call**:

```
40  platform 0x64  createObject(0x1004001, 0x30016f98)
41  platform 0x6c  free(0x30016f98)
```

The object then pointed into the arena's next tenant: watching the block shows
its `BM` header zeroed by the replacing allocation and three fifths of its words
rewritten, so a later blit would draw another allocation's bytes.

The factory now copies the bitmap into its own block and **the object names the
copy**. Because the title reads that first word and indexes the bitmap through
it, what it draws afterwards lands in the copy; a blit re-reads the copy and
re-decodes only when the bytes changed (reading and comparing is the cheap half
of a decode). The 2005 archive is byte-identical through its in-game route.

<a id="implementation-where-the-two-stand-after-the-third-pass"></a>

#### Where the two stand, after the third pass

- **The first later archive plays**: start-up, publisher screen, title screen
  with music, menus, its shipped save, in-game. Its layout leaves the top and
  bottom of a 240x320 screen black; a larger screen only makes the black taller,
  so that is the title's.
- **The second later archive is still on its loading screen.** Its frame is 163
  instructions, its step is gated on a flag at `+0x1748` that nothing sets, and
  its idle state waits on a word at `+0x3380`. It is busy underneath — over six
  hundred frames it sets sixty-two clips, starts sixty, stops five hundred, and
  asks to be resumed once per clip. Its blit source surface is empty (re-reading
  on every blit changes nothing), and nothing it asks for is unanswered.

<a id="implementation-the-screen-draws-text"></a>

#### The screen draws text

The slot previously read as a handset status line (recorded, not drawn) is the
display's text call; **two modules of two generations agree on its shape**:

```
[sp+4] = y ; [sp] = x ; [sp+8] = 0 ; [sp+0xc] = flags
r0 = display ; r1 = font ; r2 = text ; r3 = -1
bl veneer
```

— the specification's `DrawText(display, font, text, count, x, y, background,
flags)`. Each argument's meaning comes from the two call sites:

- **count is -1 at both**: a terminated run, not a length.
- **y is the line's top, not a baseline.** One module lays out three lines at
  `(screen height - 3 * line height) / 2`, stepping by line height, which
  centres only if y is the top.
- **the background rectangle is null at both**: nothing is filled.
- **the flags are carried, not acted on.** They are `0x8020` at one site and
  `0x8220` at the other; the differing bit belongs to the call passing the exact
  screen centre for x and y, suggesting alignment. One bit from two samples is
  recorded only; text lands at the given point.

The neighbouring slot `0x08` answers a font's line height and fills ascent and
descent through two pointers when non-null, as a module centring three lines
needs.

<a id="implementation-a-terminator-is-a-zero-halfword-not-a-zero-byte"></a>

##### A terminator is a zero halfword, not a zero byte

Reading it as a byte lost three quarters of a sentence. One module builds each
line a halfword at a time — a Korean character fills one with its two bytes, a
space fills one with a zero after it:

```
c0ce c1f5 2000 bfe4 c3bb c1df c0d4 b4cf b4d9 2e00 0000
인   증   ' '  요   청   중   입   니   다   '.'  end
```

Stopping at the first zero byte draws "인증 " where the title wrote
"인증 요청중입니다.". The other module passes plain bytes with a trailing zero.
**One rule reads both**: a zero ends the run on a halfword boundary or when the
next byte is also zero; elsewhere it pads a single-byte character. Pads are
dropped, which is safe because no EUC-KR byte is zero.

<a id="implementation-the-ink-is-the-darker-of-the-two-colours-a-title-sets"></a>

##### The ink is the darker of the two colours a title sets

A module sets item 1 to black and item 2 to white, then clears its screen to
white — so item 2 is the ground and item 1 the ink. The 2005 module sets neither
and also clears to white, so the default ink is dark. **This pairing is read
from two titles, not documented**; a title whose text matches its ground would
disprove it, as the first attempt (white on white) looked.

The face is the descriptor runtime's, for its reason there: a declared screen
does not predict the expected font, so there is one face rather than one per
size.

**Neither local title's screen changes**: the 2005 archive draws only a
`WAIT..` over its loading screen, painted over by the next clear, and its
in-game route is byte-identical; the playing title draws its own glyphs. The
call serves a third title's screen, which now reads as written.

<a id="implementation-what-writes-this-for-a-title-nothing-is-unanswered-for"></a>

#### What writes this, for a title nothing is unanswered for

The second later archive stops with **no trap left**, which a trap list cannot
address. `TestLocalNativeWatchProbe` watches words of the title's state block
(at `+0x24` of the factory-built object, where both local titles keep it) and
reports the instruction that wrote each, or that nothing did. Over three hundred
frames:

| word | what it is | who writes it |
|---|---|---|
| state `+0x1748` | the step's own gate | **nothing** |
| state `+0x3380` | what the state it idles in waits on | **nothing** |
| state `+0x174c` | the state selector | one instruction, twice, ending at 1 |
| state `+0x176c` | "there is something to draw" | set on a state change, **cleared three times and never set again** |
| state `+0x1760` | a frame counter | every frame |
| **every page of both pictures** | the surface it blits from | **nothing** |

**The title never draws into the surface it blits from.** Start-up fills it with
`0xff` (index 255 of its own palette, black), builds a bitmap header over it,
hands it to the factory and frees the original; it then loads eighteen
resources and blits twenty regions of a surface still holding only the fill. The
black screen is a picture the title has not drawn; what remains is to find what
fills that surface from the eighteen loaded resources, not an unanswered slot.

- **The three flags it idles on are never written**, by host or guest, so the
  platform is not failing to set them.
- **A caller cannot be read from the value it passes.** Two of the seventeen
  call sites reaching the state setter run, passing 0 and 3, and the word ends up
  1: the compiler pushes argument registers as stack space and pops them
  shifted, so a helper called between argument and store moves it. The trace
  shows which callers ran; only the watch shows what was stored.

<a id="implementation-a-module-compiled-against-a-longer-superclass"></a>

### A module compiled against a longer superclass

A relocatable module — the `client.bin<BSS>` kind, published as a class table
rather than registered class by class — numbers each method with the vtable
slot it claims, and `buildModuleVTable` lays those over the superclass's table.
**The numbering comes from a build against a different runtime**, so a platform
class publishing fewer methods leaves a gap: one local title's `Jlet` subclass
overrides `startApp`, `pauseApp`, `resumeApp` and `destroyApp` at slots 15 to 18,
where this platform's `Jlet` puts them, then starts its own methods at **26**.
Slots 19 to 25 belong to seven unpublished `Jlet` methods and stay empty.

Padding the gap with zeroes is right — closing it would move every later method.
The fault was afterwards: `inheritedVTable` read **every** entry of a registered
parent's table as a method record, so the first zero was a 28-byte read at
address 0 and the title died with

```
link KTF module "client.bin64": method range at 0x0 has invalid size 28
```

naming neither class nor slot. An empty slot is now an empty entry: later
indexes stay put, and a call landing on one branches to zero and faults with an
address. Filling the seven slots needs a call site naming one; none has been
reached.

<a id="implementation-the-same-archive-failed-two-different-ways-on-two-runs"></a>

#### The same archive failed two different ways on two runs

The same title reported that error on some runs and a null dereference in its
own constructor on others — nine and three of twelve — which a QA report read as
"works on desktop, not on phone". The cause: `linkModuleClasses` walked
`moduleClassByName` with a map range, and **Go randomises that order per run**;
`resolveModuleDescriptorCell` answers only cells the module left unresolved and
does nothing when the module wrote its superclass pointer itself (common for a
class declared beside its parent). Whether a subclass inherited its
superclass's table, holes and all, or an empty one depended on the draw.

A module-local superclass is now linked before its subclass, and the walk is in
name order so runs repeat. `TestAModuleClassLinksItsSuperclassFirst` holds the
first over twenty rounds (enough for both map orders), and
`TestAnEmptyInheritedVTableSlotIsNotAMethod` the second over a three-deep chain
with sparse slots. Over the 571 local KTF archives the change is byte-identical,
and the three taking this path still boot and paint.

<a id="implementation-and-its-subclass-wrote-its-fields-over-its-superclasss"></a>

#### And its subclass wrote its fields over its superclass's

With the link fixed the title ran every tick and painted nothing:
`framework/FunnyCanvas.paint` threw a `NullPointerException` every frame. The
canvas's `display` field was null although `setCurrentScreen` had set it — a
field offset that writer and reader disagree on.

**A module ships each field's offset in its own class**; the guest reads it from
the field's sixteen-byte record, adds only the instance header, and indexes the
object. Watching four words of the title's application object:

```
0x3000b9e0  written from 0x104ff4 (guest)  = FunnyAppMain.<init>+0x98  -> the canvas
0x3000b9e0  written from 0x1090e6 (guest)  = GameAppMain.<init>+0x86   -> a menu
```

`GameAppMain extends framework/FunnyAppMain`, both module-local, and both
constructors wrote their first four reference fields to the same four words. The
subclass's `menuDisplay` landed on the superclass's `curCanvas`, replacing the
canvas just pushed to the display with a menu; `setCurrentScreen` handed the
screen to that menu, and the canvas painted a null screen thereafter. The
instance size is short by the same amount.

The offsets are static in the module image (image and runtime are
word-identical), so rebasing is the platform's job. Unrebased offsets are right
under a platform superclass, whose fields are not in the guest payload —
`framework/FunnyCanvas extends Card` writes its first field at the header — and
wrong only under a module-local superclass. `rebaseModuleFields` adds the
superclass's instance size to each of the class's field records and grows its
instance size, in the guest's records where the guest reads them.

The title then reaches its menu and game screen, draws its Korean text, HP bar
and soft keys, and answers keys. `TestAModuleSubclassFieldMovesPastItsSuperclass`
holds the rebase and `TestAModuleClassUnderAPlatformClassKeepsItsOffsets` the
untouched case. Over the 571 local KTF archives all three changes are
byte-identical, and the three titles on this path paint as before — under
`-play` their frame differences are below the pair's own noise floor (424 and 156
differing frames against 427 and 235 between two runs of one binary).

Not a defect: it is a **240x240 handset title**; at the default 240x320 it
leaves the bottom third alone, and `-screen 240x240` fills the frame.

<a id="implementation-a-blit-measured-its-source-from-the-wrong-corner"></a>

### A blit measured its source from the wrong corner

The title's game screen carried a band of noise across the middle. Sampling eight
band pixels after every runtime call and recording the last drawing call to
change each named one `drawImage` of a 132x123 image; dumping the target around
that call showed a clean dialog box becoming the mess. The source was fine:
`/images/border.png` decodes byte-identically under any decoder, and its
rasterized framebuffer was unchanged throughout. The geometry:

```
blit source=132x123 bpl=264 clip=54,35 132x123 at 26,7 opacity=true
```

**The clip answers in device coordinates; the call's arguments are in the
caller's.** `state.clip` adds the translation and returns `left, top` past it,
and the blit measured its source as `left-x`, `top-y` against the
*untranslated* corner. With a Graphics translated by 28,28 the source read began
at (28,28) of the image, and the last 28 rows came from past the pixel buffer's
end — the noise. `copyArea` already corrected this, with a comment; the two
image paths did not.

Both now move the corner before subtracting. Over the 571 local KTF archives the
first frame changes for **six titles, all repairs**: three title screens with
black holes now draw whole, a garbled publisher logo is legible, a splash
showing two blue bars shows its artwork, and one title's sprites stop sitting a
few pixels into themselves. Nothing else moves by a pixel, and no tick count or
error changes. `TestATranslatedBlitReadsItsSourceFromTheCorner` fails on the old
arithmetic.

**Judge a drawing change on frames, not the summary.** The summary showed six
changed `lit_pixels` counts and no failures, the same shape as a regression;
viewing the twelve differing images showed repairs.

<a id="implementation-sound-in-c-and-the-table-that-was-accepted-and-thrown-away"></a>

### Sound in C, and the table that was accepted and thrown away

Fourteen of the thirty-three local archives keep their sound in the **WIPI C**
media block (table 10) rather than `org.kwis.msp.media`. It was stubbed:
`MC_mdaClipCreate` answered a 32-byte record and everything else returned zero,
so every call succeeded and the title was silent. The user reported two as "no
sound"; a corpus scan found the other twelve.

Each function's identity comes from its callers, because this block does **not**
follow the specification's print order: of twenty-one `MC_mda*` functions, a
title loading seven sounds reaches 0, 4, 8, 11, 15 and 16, where the printed
order puts those operations at 0, 3, 11, 14, 17 and 18. Argument registers of
every call:

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

An ordered trace pins the teardown; four titles produce exactly:

```
fn=0  ("Yamaha_MA3", 0x3cdb, cb)   create
fn=4  (clip, →"MMMD", 0x3cdb)      put data
fn=8  (clip, 1)                    play
fn=7  (clip)                       clear
fn=11 (clip)                       stop
fn=3  (clip)                       free
fn=0  ("Yamaha_MA3", 0x3cdb, cb)   … and the next sound
```

Stop, clear, free is how a title disposes of a finished sound, and
play/pause/resume/stop in the run 8..11 puts stop at 11. Left open is **26**,
arriving as `(clip, 45)` or `(clip, 60)` between putData and play: a per-clip
volume or a streaming clip's callback water mark. Nothing here has per-sound
gain or streams, so both readings are the same accepted no-op, and choosing
would record a guess. `internal/platform/ktf/wipic_media.go` carries the table
and this reasoning.

**The volume reaches the sound path.** Unlike LGT's stored-and-ignored value,
`MC_mdaSetVolume` acts: a title fades its music by walking it to zero.
`backend.Audio.SetVolume` scales note velocities and wave samples toward the
sink, and zero also releases what is sounding, since a full-volume note would
otherwise ring under a volume set to nothing. A sounding note otherwise keeps its
starting level, so a fade moves in note-sized steps.

**The mute state is accepted, not applied.** Its argument is a *source*, and no
evidence says which number names which; a global reading would silence every
clip. This is the LGT block's decision ([`lgt.md`](lgt.md#implementation-sound), "Sound") from the same
evidence — one caller, once, at startup.

**Nothing frees the clip's guest record.** Releasing is right only if function 3
is a free, which a call order suggests but no contract states; thirty-two bytes
per finished sound is the price of not guessing. The sound's own bytes are
released. A title creating a clip per effect and freeing none is bounded
separately: past 64 live clips the oldest sound is dropped, as on a handset out
of audio memory.

The two reported titles record 1,343 and 3,243 MIDI messages over a scripted run
where both recorded zero before. Across the KTF corpus the first-frame A/B is
unchanged: six frames differ, the six that differ from themselves.

<a id="implementation-guest-workers-unwound-together-when-the-session-closed"></a>

### Guest workers unwound together when the session closed

`ServiceThreads` grants one worker a step slice and blocks on its events channel
before the next, so during play exactly one goroutine is inside the runtime, as
the comment atop `workers.go` claims. **At close this did not hold**, as the race
detector showed:

```sh
go run -race -tags debug ./cmd/cli runktf <archive> -play -ticks 200
```

Over the local corpus this reported a race for **7 of the 35 archives**, three or
four per run, every report printed *after* the JSON summary while workers were
being stopped. Both sides were always the same instruction: the deferred restore
of `currentThread` and `currentContext` at the end of `handleSupervisorCall`,
from two `guestWorker` goroutines.

`StopThreads` closed every grant channel at once; each parked worker's `park`
answered `errWorkersStopped` and unwound its nested Go-and-guest stack, deferred
restores included, unordered against the others. The tick loop's ordering came
from the Host receiving one worker's event before granting the next, and at
close the Host was no longer in between.

**It was more than a detector complaint.** One archive ended twice in about
twenty runs with a Go stack over that path and no summary line (the message was
not captured). `guestWorker.run` recovers panics into a failed session, so a
process-level crash is either unrecoverable (such as a concurrent map write) or
outside the recovered call. In `runktf` it cost an exit code, but the server
runs the same `Session.Close` in a process holding every other live session.

**The close now has the tick loop's ordering.** `StopThreads` closes one
worker's grant channel, waits for its goroutine to return, then moves on. Each
worker closes a `finished` channel on every exit, including a worker stopped
before its first slice (which sends no event). The wait also drains events,
since a park racing the abort blocks on a full events channel and never returns.
It is bounded at five seconds per worker with a warning, so a guest swallowing
the abort costs a session teardown, not the Host goroutine. The channel
operations also give the race detector its happens-before: one worker's writes
precede its `finished` close, which precedes the next grant's close.

`TestStoppedWorkersUnwindOneAtATime` stands eight parked workers before a real
`StopThreads`, each writing the two runtime fields on exit; with the old close it
reports a data race and finds no worker finished (run it under `-race`). The
corpus sweep: **35 of 35 archives, no race, no crash**.

The same command reports nothing for LGT or SKT titles, whose thread models
share no such field, though neither was swept this way. LGT's `StopJavaThreads`
has the old KTF shape (every grant channel closed in one pass); a future report
there has this fix to copy.

<a id="implementation-a-third-download-gate-and-the-lever-that-did-not-open-it"></a>

### A third download gate, and the lever that did not open it

A title from a third series opens on a prompt of the same family: it offers to
fetch 700KB of extra data (charged separately) plus a 160KB install, yes or no.

- **No** calls `Jlet.notifyDestroyed` from the key handler; the Host reports an
  exit, which is correct behaviour (see "A game that ends is not a game that
  failed" in [`session.md`](session.md#implementation-server-sessions)).
- **Yes** reaches the title's `네트워크 접속이 실패 하였습니다. 다시
  시작하세요` notice and stops; there is no server and no way back.

**The initial completeness hypothesis was disproved (below).** The JAR holds 173
entries besides the client image — numbered resource trees plus two tables —
totalling 857KB, over the 700 plus 160 the prompt names; that suggested the
second gate's shape, but aggregate size does not show the required containers
are present.

**The diagnostics showed no platform failure.** Over 1,200 ticks the boundary
counts name no stub, unimplemented table entry, throw or raise; the title
repaints one card every tick and its resource engine prints
`js_commonResInvokeNativeClinit(<address>)` in a loop — circling its prompt.

**The second gate's lever does not move it.** Three- or four-digit
`WFEATURE_PHONE_NUMBER` values leave the prompt pixel-identical.

**The order before the first flush is short.** `-diag` on a run stopping at the
first lit frame shows, beyond class loading, exactly six steps: read three index
tables (`5/ZipTable`, `7/ZipTable`, `9/ZipTable`) from the JAR, ask
`HandsetProperty.getSystemProperty("PHONEMODEL")`, compare the answer to a
string, ask `FileSystem.exists("Config.dat")`, load two font descriptors, then
load and paint the form `8/DownloadMenu`. **No database is opened, no
subscriber number read, no data file touched.** `FileSystem.exists` now logs its
path and answer in the trace, since start-up gates are usually file tests.

Varying each input does not move the branch:

- **the record.** With `fs/Config.dat` present the title reads a fourteen-byte
  structure (five single bytes, two four-byte fields, one byte); filling it with
  `00`, `01` or `ff` leaves the prompt pixel-identical, so no single field is the
  flag;
- **the data.** The three tables index twelve containers by number (`5/0`–`5/1`,
  `7/0`–`7/5`, `9/0`–`9/3`, 543KB in total), addressed like every other resource
  (directory number, slash, index). **None of the twelve is in the archive**,
  while every uncovered tree is. Adding all twelve zero-filled changes nothing:
  the title never looks;
- **the form.** The AOT image keeps constants as a packed UTF-16 pool with a
  pointer array over it, so repointing one word sends the call site to
  `8/StartMenu`. The title passes the prompt, loads that form, and spends its
  whole instruction allowance in its resource engine, as a title without its
  data does.

The decision uses none of number, model, record or data, and the archive lacks
exactly what the prompt offers. Its `__adf__` `Desc` field reads "this service
runs only after the 700kb extra download and the 160kb install": a copy taken
before that download. **Only the twelve containers would open it, and this side
cannot produce them.**

**The detour found four missing runtime methods**, each stopping the run by name
once the prompt was bypassed: `java/lang/Short`, `java/lang/Byte` and
`java/lang/Long` were never published to guest AOT code (only `Integer` had been
seen named), and `java/lang/String.valueOf([C)` had no body. Form attributes are
parsed one class per attribute type, so a form with a coordinate asks for
`Short.parseShort`. All four are implemented, and
`TestTheBoxedNumbersAreAllPublished` keeps the set together.

<a id="implementation-the-earlier-packages-title-screen-carries-a-band-and-it-is-the-titles-own"></a>

### The earlier package's title screen carries a band, and it is the title's own

The native package's title screen (see "An earlier KTF package") draws a
horizontal band across the middle whose colours look unlike the artwork above
and below, with the menu drawn over it.

- **It is not a transition.** Frames across a 900-tick run are byte-identical
  from the logo's appearance (0 differing pixels between ticks 300, 450 and 899).
- **The whole screen is quantised to a small palette.** Every channel lands on a
  multiple of about 36 (0, 35, 71, 107, 143, 182, 218, 255), and a band row
  holds one to nine distinct colours — a low-bit-depth source expanded to eight
  bits, equally true of the correct-looking parts.

**Nothing about it comes from the `.mif`**: the title decodes its own graphics
archive and passes finished Windows bitmaps through slot 25 (see "The sprites
are Windows bitmaps"). Dumping the fourteen bitmaps it builds answers four ways:

- **The band is one 32x96 bitmap tiled across the screen.** Band rows repeat
  with period exactly 32 (mean per-channel difference 14.8 there against 50 at
  every other period tried), and each band row is byte-identical to a row of
  that bitmap — the blit reproduces the title's pixels exactly.
- **There is no stride to get wrong.** At eight bits per pixel and 32 wide, rows
  are 32 bytes with no padding, and a row-shift search finds no constant offset.
- **The decoder is fine.** The other thirteen bitmaps, same path and depth, are
  clean artwork — two publisher logos, the carrier's, the title's wordmark, and
  a `Powered By 7-Zip` badge legible to its lettering.
- **The tile is mirror-symmetric by design.** 95.9% of pixel pairs match across
  its vertical centre line, against 69.9% for the visibly symmetric wordmark —
  a seamless tile, an artist's decision.

**The band is the title's own artwork**: a mirrored sunset strip, dark at the
top and cloud-coloured through the middle, tiled eight times behind the logo and
the island sprite. The quantisation is the 256-entry palette of 8-bit bitmaps.

<a id="implementation-what-a-sweep-of-a-262-archive-set-asked-for"></a>

### What a sweep of a 262-archive set asked for

A local set of 262 archives — 259 descriptor-and-JAR and three WIPI 1.1
packages — was driven from a fresh save in five passes: a stepped probe to the
first lit frame; two thousand ticks at four times the guest clock; a key script
over titles whose screen had not changed; a timed four-hundred-tick run over
unfinished ones; and a second key-script run, against the save tree the first
wrote, over screens still unmoved. 157 reach a screen that keeps changing or
answers a key (whether a title plays is for the user to find). The rest stop in
six shapes, the first being most.

**Run twice before believing a screen has stopped.** The fifth pass moved
thirteen of twenty-five stuck screens across both WIPI platforms: they held the
title's own notice ("the game is closing to free memory, please start it again"
in four wordings) — the startup gate described earlier, which a single-run probe
reads as a hang. One of the thirteen faults on its second run on a null
structure pointer in a timer callback, a real stop the notice had hidden.

**A missing class-library member stops a title outright; thirty-three members
accounted for forty-eight titles.** The AOT bridge resolves library calls by name
and descriptor, so an unpublished member stops the run with class and descriptor
in the message. Publishing them was mostly metadata over existing core-library
bodies; those needing a body are named below. Requested:

- `java.io`: `OutputStream.write([BII)V` (five titles), `DataInputStream`'s
  `readFully([B)V` (four), `readChar()C` and `readUnsignedByte()I`,
  `DataOutputStream.writeUTF`, three titles'
  `InputStreamReader.<init>(Ljava/io/InputStream;)V`, and
  `PrintStream.println(Ljava/lang/Object;)V`
- `java.lang`: `Class.forName` (three), `System.getProperty` (two), `String`'s
  `<init>([C)V`, `<init>([CII)V`, `indexOf(II)I` and `lastIndexOf(I)I`,
  `StringBuffer.insert(ILjava/lang/String;)`, `Throwable.printStackTrace`
- `java.util`: `Hashtable.<init>(I)V` and `keys()`, `Calendar.set(II)V`,
  `TimeZone.getTimeZone` and `getAvailableIDs`
- the platform's own: `org/kwis/msp/handset/LED.set(I)V` (two),
  `org/kwis/msp/lcdui/Image.play`, and three `org/kwis/msp/lwc` components —
  `ShellComponent.getX`, `AnnunciatorComponent.layout`,
  `LabelComponent.<init>(Ljava/lang/String;)V`
- two vendor classes outside the specification: `wec/OEMDevice`'s
  `enableSleep(Z)Z` and `getSYSTheme()`, and `com/ktf/kfc/GForm.<init>(IIII)V`
  (which is a whole toolkit rather than a class — see below)

**Nineteen of the forty-eight stopped after the title screen**, mostly from the
java.io half: fourteen died in a paint or worker and five on the first key press,
where a title of this era writes its save — a missing stream member surfaces
only when something is stored.

Six needed more than a declaration:

- **`writeUTF` is written against `readUTF`, not Go**: both are modified UTF-8
  with a two-byte length, one round trip for a stored name.
- **A `Reader` decodes without knowing the charset**: it feeds the platform
  decoder one more byte at a time until it yields a character rather than a
  replacement, correct for single- and two-byte cases. Reading to the end first
  would make a reader over a connection wait for the far end to close.
- **`TimeZone` knows two zones: the guest clock's and GMT.** Calendar and Date
  read that clock as local time, so a disagreeing zone would break a title's
  arithmetic. The IANA table would embed tzdata in a cross-compiled release, and
  no title does more with `getAvailableIDs` than pass an element to
  `getTimeZone`.
- **`System.getProperty` answers the handset table shared with the WIPI C call
  and `HandsetProperty`**, plus configuration and profile names, and null for an
  unknown name — a capability probe reads an empty string as "present".
- **`Image.play` and `stop` left the deliberately-unimplemented list**: the
  specification makes both no-ops for a non-animated image, and none here is
  animated.
- **`wec/OEMDevice` is a vendor class with no reference**, so it answers what a
  caller can do least with: sleep not disabled, and an empty theme object. A
  title reading a colour off the theme will stop there, yielding evidence.
  **`com/ktf/kfc/GForm` was deliberately left absent**: a probe answering its
  `(IIII)` constructor with a no-op moved the title one call on, to
  `com/ktf/kfc/GTextField.<init>(Lcom/ktf/kfc/GMenubarForm;Ljava/lang/String;I)V`,
  and the image's name table lists `getPluginPosition`, `setPluginLayer`,
  `resize`, `doModal` and `showNotify` beside them — a vendor widget toolkit
  (form, menu bar, text field) the handset composites, with no reference. Empty
  classes would carry the title into a menu screen that draws nothing, so the
  probe was reverted.

**A failed field lookup named a handle, not a class.** Four of the forty-eight
stopped on `err:Ljava/io/PrintStream;` or `w:I` "from class 0x3000a9e0"; method
misses named their class, but the field path did not resolve the record to a
name. Both paths now name the class, which identified the two misses:
`java/lang/System.err` and `org/kwis/msp/lcdui/Card`'s `w`.

**Card publishes its geometry, and subclass fields move to make room.** The
specification declares `x`, `y`, `w`, `h` and `bTrans` protected on Card, and a
title reads `w` off its canvas instead of calling `getWidth`. Guest field
offsets are **not** baked into the image: the client computes them when preparing
a class, from the parent's declared instance size, so declaring twenty bytes on
Card moved every canvas subclass's first field from offset zero to twenty across
the set without the archives changing. That makes publishing runtime-class
fields safe, and `field_sync.go` checks it before writing.

**Seven titles' `startApp` was not found on the descriptor's class, and an eighth
could not find a thread class's `run`: one defect.** KTF hands the runtime one
class at a time (`GetClass` answers the requested class, and the guest registers
a class when initializing it), so a Jlet inheriting `startApp` from a base class
in the same image had no chain in the registry to walk. The chain was in the
image, since a class descriptor holds its parent's record address. Resolving a
class now reads and registers that chain — metadata only, running no guest code
or class initializer; the guest still registers the parent when initializing it.
An unreadable ancestor stops the walk and is logged, since this only enriches a
lookup that worked without it.

**What the forty-eight became.** Thirty-nine start and draw. Driving the whole
set a thousand ticks at the game's own pace found twelve more dying later, mostly
on more of the same: `Player.resume`, `Math.max(JJ)J`, `StringBuffer.setCharAt`,
`Calendar.getInstance(TimeZone)`, a `ByteArrayInputStream` constructor, two
`org/kwis/msp/lwc` members and `ByteArrayOutputStream`'s protected `buf`. **Probe
design rule: a member not needed to draw the title screen is invisible to a probe
that stops there.**

<a id="implementation-the-second-round-of-that-list-and-the-one-answer-that-is-a-guess"></a>

#### The second round of that list, and the one answer that is a guess

Every title on that list but one now runs a thousand ticks (the exception, below,
is not this list's). Most was metadata over existing core-library bodies:
`Math.max(JJ)J` and `min(JJ)J`, `StringBuffer.setCharAt` and `charAt`, and
`Player.pause` and `resume` against `Clip` as well as `BaseClip`, which the
specification declares both ways (as `play` and `stop` already were).

**Answering one member reveals the next.** Past `Math.max` one title allocates a
`java/util/Stack`, for which no class was published, and past
`ByteArrayInputStream` another walks a vector with `elements()`; neither appeared
in the first report. **A class the table does not name is the worse failure**:
the loader answers an empty record, `new` succeeds, and the stop is a field read
several calls later with nothing naming the missing class — for `Stack`, a
two-byte read at offset 22 of nothing.

Four needed more than a declaration:

- **`ByteArrayInputStream([B, int, int)` must know where it ends.** Every method
  read to the array's end, correct only for the whole-array constructor; a title
  keeping several records in one buffer opens a window per record, and an
  unbounded window hands it the next record's bytes.
- **`Calendar.getInstance(TimeZone)` is the same instant in different fields.**
  The zone changes how `get` splits the instant, and `Calendar.set` already
  carried the location forward, so choosing it at construction suffices;
  `setTime` now keeps it, so a GMT calendar set from a stored date reads back in
  GMT.
- **`org/kwis/msf/io/URL.find` throws rather than answering null.** The
  specification declares `SchemeNotFoundException`, so a networked title has a
  catch with an offline path behind it, while an unexpected null dies at the
  first read. One title's handler then faults on unmapped memory at the same
  instruction either way, so that fault belongs to the guest-memory group below.
- **`ByteArrayOutputStream` is the third class with a published instance field,
  exactly one word.** A title reads `buf` instead of calling `toByteArray` to
  avoid the copy, so the payload must name the guest array behind the Go buffer.
  `count` is deliberately unpublished (nothing reads it; a missing field fails
  loudly where an unmaintained word would be silently stale). The mutators are
  the three calls that can swap the array, and the publish compares before
  writing, so filling the existing array costs one guest read.

**A `Thread` subclass not overriding `run` had nowhere to inherit it from.** One
title subclasses `Thread` and passes a `Runnable` through `super(r)`, a published
constructor, but `run` was not on the runtime's `Thread`. The core library's body
existed and does the right thing: run the Runnable given, or nothing.

**Two `org/kwis/msp/lwc` members became three new classes and six methods on
their common base**, as happens when a title uses a component hierarchy rather
than drawing itself. `ContainerComponent`'s `repaint` is `Component`'s, declared
once and inherited, and requests the card's repaint: nothing here paints
components and the toolkit's layout is absent, so the named rectangle has nothing
to honour. Behind `DialogComponent`'s constructor came `Component`'s two colours,
then `FormComponent` and `ProgressComponent`, which keep and answer the numbers
of widgets this Host does not draw. A progress bar's numbers follow the
specification — value clamped into the bar and rounded down to whole steps, a
maximum at or below zero refused — because reading back is the only way a title
can observe an unpainted bar.

**The dialog asked a question this Host cannot answer.** `doModal` shows a dialog
and waits for a choice; nothing is shown, so the outcome comes from the type. A
`TYPE_NONE` dialog has no buttons and ends in `DLG_TIMEOUT`; a `TYPE_OK` dialog
has one, so `DLG_OK` is the only answer. **`TYPE_OK_CANCEL` has two, and this
takes the affirmative** — a guess, recorded in `testdata/wipi_java_stubs.txt`
(answers a game believes) and counted in diagnostics for every dialog answered
unseen. OK is the button a dialog opens focused on, so it is what pressing
confirm without reading yields, and a title usually offers a choice once, while
refusing every dialog keeps one asking. A Host boundary putting the dialog in
front of the person, as `internal/textinput` does for text fields, would end the
guess.

**No cost elsewhere.** The whole set was driven again from a fresh save with the
binaries before and after this pass; no title stopped reaching its first frame.
That whole-set check matters here because publishing a field on
`ByteArrayOutputStream` moves every guest subclass's fields by a word, and adding
`run` to `Thread` inserts a method into a vtable every guest thread class
inherits.

**Do not judge a title by its first run.** With `buf` published one title draws
twenty-two frames reading "the game has been installed, please run it again",
calls `notifyDestroyed` and leaves; the second run on the same save tree reaches
the title screen (the startup gate described earlier).

The nine that do not reach a first frame have varied new causes: three fault on
guest memory deep in their code, two pass a runtime method a reference not bound
to an object here, one raises `StringIndexOutOfBoundsException` from its own
`substring`, one nests a `Class.forName` that raises through a catch until the
platform arena is exhausted, one is `GForm` above, and one exceeds the
instruction limit. Of the 259 zip archives in the set, load failures went from 56
to 25, all 25 already failing before.

**Three WIPI 1.1 packages died inside the client entry because their client image
is not code at its first byte.** They share the generation's `__adf__` and
`client.bin<BSS-size>` layout, and their JARs hold no class files (the title is
the native image with Java compiled in), but the file is a relocatable module:

	u32 bssSize            what the file name also names
	u32 relocationCount
	u32 zero
	u32 relocations[]      segment offsets of words to rebase
	u32 zero               terminator
	...segment...

and the segment opens with nine words of its own before any code:

	seg[0..5]  addresses, one of them the end of the segment
	seg[6]     0x13580001
	seg[7]     the module entry, Thumb, so odd before and after rebasing
	seg[8]     0x13580001
	seg[9..]   code, beginning with the same helper in all three files

Every relocated word gets the segment's load address added. All three files agree
on the header, terminator, both markers and the shared helper, which makes the
reading certain, and `parseRelocatableClient` requires all of it, because every
other archive carries the alternative reading.

Two relocation entries remain unexplained: each module relocates the word at
segment offset 0x18 (the marker, not an address) and at 0x24 (the helper's first
instruction pair). Nothing reads either, so the table is applied as is, without
invented exceptions.

All three then reach a shared entry function and run the same twenty-odd
instructions before stopping at `ldr r3, [r0]` and a call through the loaded
value. **The entry takes a pointer to a function table**, not the newer entry's
BSS size: the newer client asks for its interface by supervisor call, this one is
handed it. The table's contents and size are next to find; the failure now reads
as a branch to a small address rather than an instruction fault in the header.

**A title drawing an image it never got raises the correct exception.** Three
titles end on a `NullPointerException` from `Graphics.drawImage`; the title
caught what `createImage` raised (`IllegalArgumentException` for an undecodable
encoding, `IOException` for a missing resource), left the slot null and painted
it later. The fix is upstream — which encoding or resource name failed — not a
null-tolerant draw.

**Six archives were refused before anything ran; four refusals were the loader
being stricter than the archives.**

- **A descriptor may carry no PID.** One archive has the key with nothing after
  it; `SaveOwner` always fell back to the AID, so refusing contradicted the
  loader's own fallback.
- **A folder entry is dropped before its name is checked.** A bare `./` entry
  cleans to nothing and refused the title, though nothing is stored under a
  folder entry and the check concerns where content lands.
- **A duplicate entry is refused for ambiguity, not duplication.** Which of two
  same-named entries a reader takes decides what runs; one archive packs its
  image folder twice, byte for byte, which has no ambiguity. A differing
  duplicate is still refused.
- **The descriptor locates the root.** One archive dumps the handset's
  application directory: `W/exe_info` beside `W/apps/<AID>/__adf__` and the
  title, with no single shared folder to strip. The convention is the
  platform's: the earlier package's `__env__` names its private area
  `/W/apps/<AID>/P/`. An archive with its descriptor at the root is untouched,
  and one with two descriptors is not re-rooted at a guess. Detection matches the
  marker on the last path element to agree with the loader.

Of the rest, one refusal stands and one was decided. Three archives zipped
together, one per episode of a game, **are not a container this Host reads**:
unpacked, each episode is an archive that reaches a first frame and its title
screen. A zip containing only zips is refused with its shape named, as a RAR is.
The standing refusal is a RAR renamed `.zip`, and three more files in the set are
ALZ archives; **they now get their format named**: "zip: not a valid zip file:
this is a RAR archive, which this emulator does not read". Every loader wraps its
zip refusal this way, so a person is told which file they picked.

**A cause inside the archive is not an emulator gap** and stays off the working
list. RAR and ALZ files need repackaging, and one title asks for a sound
resource its JAR never shipped (numbered files run 0 to 12 then skip to 15; the
game opens 13). **A half-downloaded archive is the same kind of dead end, worth
checking**: the descriptor states the expected payload size. A title whose main
class the module cannot find is *not* that — one here reports it missing while
the compiled module carries the name, so it is this platform's reading.

**Eight titles fault in guest memory and four exceed a budget.** The memory
faults are accesses at small absolute addresses (`0x0`, `0x24`, `0x48`) — a null
structure pointer being walked, not a missing mapping; one of the eight is the
title the restart notice hid. Three budget stops exceed the instruction limit
inside `startApp` and one exceeds a service call's step allowance inside
`paint`; the trace distribution, not yet read for these, separates a spin from
a load.

**Six titles hold a screen through two runs; one names its reason.** Its guest
log prints `MC_fsOpen() failed!!!(-12)` and then destroys every loaded image — a
file open this platform refused. A screen going blank after a load warrants
reading the guest's printk first. The other five are unexplained (one never
paints through both runs and the key script); a denser key sweep is next.

**Two titles stop on WIPI C table 7 function 8, and its call site renamed the
whole table.** The site passes a name pointer and a literal `1`, sets nothing
else and drops the answer — both titles, in their start-up routine, with names
`ga` and `res`.

Table 7 had been read as a second database (one blob per name, a cursor, seeded
from a packaged file). **Table 7 is the WIPI C filesystem**, matching the
specification's `MC_fs*` order slot for slot (listed under
[The two storage tables](#implementation-the-two-storage-tables)). Slots 11, 12
and 15–16 settle it — no database puts free space, cursor position and
existence at those numbers — and the earlier guess about slot 15 (a handle,
answer dropped just before a seek resets the cursor) is exactly `MC_fsTell`.
**Slot 5 confirms it byte for byte**: it takes a name and a pointer and writes
twelve bytes with the length at offset 8, and `MH_FileInfo` is
`{ attrib, creationTime, size }`. Table 5 was always the `MC_db*` record
database in that section's order. A title's own log had said so: its
`MC_fsOpen() failed!!!(-12)` names this table's slot 0 refusing a read-only open
of an absent name with `M_E_NOENT`.

Function 8 is `MC_fsMkDir(dirName, aMode)`, and `1` is `MC_DIR_PRIVATE_ACCESS`,
the program's own directory and the only area here. **A directory has no
representation** in the flat name-to-bytes store, so making one only fixes the
answer: zero when made, `M_E_EXIST` when already present. Always answering zero
would tell a checking title every run is its first, so made names are recorded
beside deleted names in the same store, to answer identically across restarts,
and `MC_fsIsExist` consults the list so a just-made directory exists.

Both titles reach their first frame. The whole 262-archive set ran 400 ticks from
an empty save tree on the binaries before and after: **two rows move from an
error to a first frame, and none of the other 260 changes flush count, lit-pixel
count or tick count.** Neither title reads the mkdir answer; the made-names list
is for one that does.

**The same pass tightened what counts as a first frame**: a run finishing its
400 ticks without error but lighting no pixel no longer passes, which moved two
more titles out of the OK column (same behaviour on the old binary).

**Not done: the other half of a directory.** `MC_fsFileAttribute` on a made name
answers "no such file", because the specification defines `MC_FILE_IS_DIR`
through the HAL without giving its value, and a guessed bit would have titles
branch on an unestablished number. No local title asked.

*(A later title asks, and the refusal ends it. The attribute word stays
unguessed; see "A directory the platform made, and the two questions that
answered it differently".)*

<a id="implementation-a-picture-a-title-recoloured-and-the-four-bytes-that-refused-it"></a>

#### A picture a title recoloured, and the four bytes that refused it

A sibling-platform title keeps one encoded PNG in a byte array, overwrites its
palette chunk at run time, and passes the array to the make-image-from-bytes
call. A handset decoder never verifies chunk CRC-32s; Go's does and refused the
picture. All three platforms' image routers now end in `wipic.DecodeStandard`,
which retries **only after a decode fails**, recomputing the PNG's chunk
checksums. Pictures that decoded before take the same path, and damage beyond a
checksum stays damage.

<a id="implementation-the-third-round-a-void-calls-return-register-and-a-guard-the-parser-ate"></a>

#### The third round: a void call's return register, and a guard the parser ate

Three of the list's guest-memory faults were one defect, in a function the
specification gives no return value.

**`MC_knlFree` is declared `void`, so what it leaves in `r0` is outside its
contract**, and answering a fixed value is a decision. One middleware, shared by
three archives, ends an initialization step by freeing the buffer it loaded a
font through, with the free in tail position; the caller then tests `r0` and
treats zero as failure. Writing zero turned "font loaded" into "font failed":
the initializer returned early, its words stayed at the image's shipped zeros,
and the first `paint` dereferenced one and faulted on unmapped guest memory at a
small offset — six calls and one early return after the cause.

Leaving `r0` as the caller set it, as a routine that never touches its return
register does, costs nothing because the value is undefined. All three archives
reach their screens; the 400-tick run over the 262-archive set, before and after,
moves three rows from tick error to first frame and leaves every other row's
flush, lit-pixel and tick counts identical.

**Two remaining rows were probed and left alone.** Three titles hand
`StringBuffer.append(Ljava/lang/Object;)` or
`PrintStream.println(Ljava/lang/Object;)` a word the JVM has no object for: a
Thumb code address in the title's image, a supervisor-call stub address, and a
small integer. Binding a synthetic `Object` moves all three walls — to a
`NullPointerException` from the title's code, to `unsupported Calendar field`,
and to a guest fault holding a `GregorianCalendar` — suggesting these are
symptoms of an earlier error. The permissive binding was not kept: fabricating
an object for an arbitrary word is the silent-wrong-answer shape this platform
refuses. (The cause was found in the fourth round.)

**One title's `drawImage` receives null although every `createImage`
succeeded.** 404 `Image.createImage([BII)` calls return images and the
decode-failure counter is zero, yet the element loaded from the title's array is
a zero word. Its array store writes guest memory directly, so the store and load
paths are the two to compare.

**The third fault hid behind a guard Go's precedence had deleted.**
`MC_knlGetResource` refuses a handle with the sign bit set, because a title not
checking `MC_knlGetResourceID` passes the error code back as the handle, and
reading `-12` as an address faults instead of failing the call. The guard was
`handle&1<<31 != 0`; in Go `&` and `<<` share a precedence level and associate
left, giving `(handle&1)<<31` — zero for every even handle, i.e. every error
code, so it never rejected anything. It is `handle&(1<<31)` now, and the tree was
swept for the same shape.

**The three older relocatable modules were entered eight bytes in.** The header
was read one word late: the relocation table starts at offset 8, its first entry
is a zero rebasing the segment's first word, and there is no terminator. The
wrong reading (a zero header word at 8 and a zero terminator) also finds two
`0x13580001` markers around a different word, since the neighbourhood is pointers
either way, so it looked equally certain. Its entry landed past
`push {r4, r5, lr}` and half the position-independent prologue, building the
static base from whatever `r3` held. Under the corrected reading all three
entries land exactly on that push.

**The table the segment header bounds must be rebased too.** The list's last
offset sits a few words below `seg[6]`, which equals the static base the entry
prologue computes from `pc` in all three modules; `seg[7]` equals the segment
length in the file. Between them lies the module's pointer table — 334, 331 and
546 words — every word a segment offset within the segment and its writable
tail. The entry passes one of those words on as a string pointer, and only the
rebased word points at a string, so the load rebases the table wholesale and
refuses bounds that do not describe one.

**The entry's request now has a name.** It calls the first word of its argument
with a string and `-1` twice, stores the answer in a static, and succeeds only on
non-null. The string in all three modules is `MNInterface`: the argument is a
table whose first function answers an interface by name and version ("any"),
and everything afterwards goes through that interface. The next caller indexes
with `(word >> 1) * 4` and calls the interface's word at `0x40`, so it is a flat
function table. Its contents are unknown, and all three modules still stop on
that first call.

**Thirteen titles are far slower than the handset; three do not finish 400
ticks.** Seven are this platform's: 400 ticks at four times the clock cost them
twenty to 103 seconds against about one second for a title that keeps up, and
one does not finish in five minutes. All are drawing, so this is a profile to
read, not a fault.

<a id="implementation-the-fourth-round-a-stream-that-could-not-be-read-twice-and-a-loop-with-nowhere-to-paint"></a>

#### The fourth round: a stream that could not be read twice, and a loop with nowhere to paint

Ten more of the 259 reach a first frame, none with a cause of its own. **The
400-tick run over every archive, before and after, moves ten rows from failure
to first frame and leaves every other row's flush, lit-pixel and tick counts
identical** — except one row already counted working, which drew 215 pixels and
now draws its publisher's logo. Three defects account for all ten; the largest is
one word.

**A resource stream could not be read twice, reported four hundred successful
decodes later.** Three titles stopped on a `NullPointerException` from
`drawImage` — the title's own null, as the previous round read. One call above,
`InputStream.reset` threw: neither the byte-array stream nor the decoding wrapper
declared `mark`, `markSupported` or `reset`, so both inherited the refusal, and a
title reading a header, resetting and decoding the same bytes caught an
`IOException` its decoder never raises. [`jvm.md`](../jvm.md) has the fix and
where the class documentation is wrong. The third title had been grouped under
another cause (spending its whole service allowance retrying), as a symptom four
hundred calls downstream appears in a list grouped by message.

**A frame loop in a `callSerially` Runnable had nowhere to paint.** Two titles
ran 400 ticks without error or a lit pixel. Their loop — repaint, re-queue,
sleep — runs on the client thread, but a wait declared inside the Runnable held
the paint and timers and not the next Runnable, so the Runnable was dispatched
every sixteen milliseconds while its sleep pushed the paint away, and the paint
never came due. Three conditions close the loop:

- **A queued Runnable waits like everything else on the client thread.**
- **A repaint queued before it goes first.** The original loop takes repaints and
  Runnables from one queue in posting order; otherwise the next Runnable takes
  the round the paint finally fell due in.
- **The reported deadline matches what will run.** A queued Runnable's reported
  deadline was its dispatch interval alone, so a manual-clock Host was told of
  work due at an instant the dispatch would refuse and never advanced. Both now
  answer the same instant.

**A catch block was never handed what it caught; it read whatever the frame
below had left.** This moved five titles. A handler record carries the exception
at a fixed offset; the try prologue builds the record on the guest stack, and
the platform's unwind filled everything but that word. The values were
plausible — a Thumb address from the frame below, a small integer, a
supervisor-call stub — and five titles failed five ways:

- `catch (e) { close(); throw e; }` re-threw a code address, reported as a word
  that is not an object.
- `System.err.println(e)` and `new StringBuffer().append(e)` passed such a word
  to a runtime method — the three "reference is not bound to a JVM object" rows.
- `e.printStackTrace()` and `e.getMessage()` dispatched through the word as an
  object header and faulted on a wild address in the title's dispatch helper,
  twice at the same instruction, which first suggested one cause.
- One title looped on the failure until the instruction limit.

**The word was found by dumping the record**: the re-thrown value sat four words
in, before the throw, so the field is at offset 16 and was never written. Writing
the pinned exception address there is the fix.

**What is left of that family, measured rather than guessed.** Five titles of the browser sweep end on
an uncaught guest exception; instrumenting the handler search on the three that
reproduce asked whether the game has no catch or one this platform cannot find.
The groups differ:

- **One title's painting thread has no handler chain.** The search reads the
  chain head as a thread-local word, zero on the client thread where the Host
  calls `paint`. The game's `catch (Exception)` is on its own thread's chain,
  pushed by the `run()` that calls paint on a handset, so a throw inside `paint`
  cannot reach it. The thirteenth round answers both this and the indexed array:
  the callback ends rather than the session, and the array is the title's own.
- **Two titles throw at a label exactly at the end of a guarded region.** A
  handler record carries the last label the guest wrote, and each entry guards a
  half-open label range. In both, the label is the range's `to` — `0xcc` against
  `[0xc5,0xcc)` and `0x1796` against `[0x1793,0x1796)` — either a genuine exit or
  an off-by-one. An inclusive bound rescued one and not the other, and re-opened
  the hazard of a throw inside a catch block matching the region it just left
  and jumping back to its top. **The label's contract settles it** — when
  compiled code writes it relative to the throwing call — read from code, not
  inferred from survival. The thirteenth round reads it: the half-open range is
  right, and both titles threw after leaving the try.

**A title's own `throw` had no platform slot.** Slot 2 of the initialization
callbacks table was zero. Slot 1 throws by class name; slot 2 throws an already
constructed exception, which `throw e` compiles to. A title throwing a built
`Exception` jumped to zero, failing with "Java jump target is null" inside its
own error handling. The message now names the jump's argument count, receiver
class and call site, which identified the slot.

**The exception hierarchy is published to guest code.** An undeclared class
resolves to a record with parent `java/lang/Object` and Object's methods, which
for an exception is worse than a miss: `new Exception()` resolves to
`Object.<init>`, `getMessage` on a caught exception resolves to nothing, and the
title stops inside its handler. The records are generated from the parent chain
the JVM's `catch` matching uses, so the two cannot disagree.

**A failed start keeps its trace.** `-diag` wrote nothing for the archives
whose failure is hardest to read, since no session remained. A failure past the
client's entry now carries the boundary trace and the command writes the report;
every finding in this section came from one.

**Running off a guest stack is named as such.** Exhausting a downward-growing
stack appears as an unmapped access a few bytes below a mapping, like a wild
pointer. One title does this; four megabytes of stack instead of one bought two
minutes and the same end, so it is recursion, not an undersized stack — opposite
directions of investigation.

<a id="implementation-the-fifth-round-a-block-that-ended-where-the-last-field-we-read-ended"></a>

#### The fifth round: a block that ended where the last field we read ended

Six more of the 262 reach a first frame. **The 400-tick run, before and after,
moves six rows from failure to first frame and leaves the rest identical** in
flush, lit-pixel and tick counts. Because the change reaches `internal/jvm`, the
fifteen SKT archives were run on both binaries (all thirty rows agree); LGT was
not, since `internal/platform/lgt` does not reference `internal/jvm`.

**The block param 1 points at was sized to the one field this platform reads,
and a middleware writes two words past it.** Two titles sharing it died on
`str r3, [r2, #0x24]` with `r2` zero, at the end of a native the Java half calls
every frame. The block is the module's context; this platform knew only the
exception handler head at offset 32, so it was nine words, and the middleware
writes offsets 36 and 40 — landing on the next two allocations, the single-word
parameters 0 and 1, so the pointer to the block itself was overwritten with a
scene number and then dereferenced. The block is now a full handler record long.
A block ending at the last field this side reads is one the next module writes
past.

**Both halves of a 64-bit answer must come back.** A Java jump runs the callee
on its own context and carried only r0 back (a `bl` would leave both), so every
`long` arrived truncated to its low word. One title builds a string from
`System.currentTimeMillis` and indexes a digit, reading ten characters where a
handset gives thirteen, failing with `StringIndexOutOfBoundsException` mid
startup. The message now carries range and length, which identified it.

In a native call's eight-byte result container the second word is filled **only
when the return type is two words wide**; otherwise it stays zero, because a
module-compiled native returns by the module's convention and r1 after a
one-word answer is the callee's leftover.

**`Graphics.getPixels` and `setPixels` are the device-format pair.** A title
reached them through its annunciator. The screen is 16-bit, so a pixel is the
two framebuffer bytes as they lie and `bpl` is a byte pitch — a different method
from `getRGBPixels`, not a respelling.

**The startApp instruction ceiling cut off a loader, not a runaway.** Two titles
spend fifty to a hundred million instructions in one native call during
`startApp`, decompressing before drawing, in under a second of Host time; the
ceiling moved to a hundred million.

**Painting before the guest threads was measured and rejected.** The two titles
start their native halves from the first paint (the annunciator hook calls the
module's `start`, the game thread its per-frame entry), so painting a new card
before threads run moved both — and broke three working titles whose `paint`
needs images their own thread loads (one raised `NullPointerException` from
`drawImage` on frame one). Six better and three worse is no improvement, and
either order is defensible on a handset with a concurrent display and game
thread. With the block correctly sized, the two titles no longer need it, so the
order stays timers, threads, paint.

<a id="implementation-the-sixth-round-a-long-jump-that-outran-the-host-call-it-was-made-in"></a>

#### The sixth round: a long jump that outran the Host call it was made in

Four more of the 262 reach a first frame. **The 400-tick run, before and after,
moves five rows and leaves 257 identical**: four failures became first frames,
and a fifth no longer faults but paints nothing. The fifteen SKT archives ran on
both binaries (the round reaches `internal/jvm`); one row's lit-pixel count
differs, but that title alternates between the same two counts on either binary.
LGT was not re-swept: the only shared change is one `armcore.Thread` field
nothing outside this platform reads, and three LGT titles agree on every flush
before and after.

**A guest long jump can be resumed only by the Host call owning the frame it
lands on.** The guest's `try` is a `setjmp`: a handler record on the guest stack
holding r4-r11, ip, sp and lr, restored by an `ldm` routine. This platform
resumed that routine wherever the throw was raised — inside whichever nested
`core.Call` the Host was in. The guest stack is shared by those calls, so a
handler saved *above* the call's entry belongs to a caller the Host has not yet
returned to; resuming it runs that caller's code inside the inner call, which
later reaches `ReturnAddress` and ends while the Go frames believe their guest
call returned, with a stack pointer into discarded memory. One title did this on
its first painted card: `execute guest memory at 0x1a`.

The fix is a comparison. `Core.Call` remembers its starting stack pointer, the
unwind carries the handler's saved one, and a resume that would land above the
entry returns the unwind instead, so the next Host call out asks the same of its
entry; the outermost call owns the whole stack, so the walk ends. The three loops
that restart the guest at a restore routine — class initializer, AOT method
invocation, supervisor-call resume — all check it.

**A catch block leaves its protected region, and the record's label must say
so.** A handler record carries a label of the current position in its method,
written by the guest between protected regions; each exception entry's target is
the first label past its range. Two of the three local titles hitting this write
the label at the top of each catch block; the third leaves it to the platform.
Without it the catch block ran inside the region it had left, threw its own
`NullPointerException`, matched the same entry and jumped back to its start —
four hundred thousand times, reported as the instruction ceiling. The label is
now written on handler resume, equivalent to leaving the region since target and
range end coincide.

**The heap a title is told about is not the arena.** `MC_knlGetTotalMemory`,
`MC_knlGetFreeMemory` and `java/lang/Runtime` reported the 64MiB data arena,
far beyond any handset these archives targeted, and titles compute on it in
32-bit ints: one multiplies free bytes by a hundred for a percentage, overflowing
above about 21MiB. It printed `-28% FREE`, collected, printed `-29% FREE`, and
looped — a livelock resembling a slow loader. The reported view is now bounded at
16MiB (below the overflow, above any WIPI handset's heap), with free being the
bound less what is allocated, so the percentage falls as the title allocates.
Both views stay one, so a title judging from `Runtime` and freeing via `MC_knl*`
sees one heap.

One local title prints the figure verbatim in its initialization log —
`Total Memory : 16777216`, `Free Memory : 16610188` — and none was seen sizing
anything from it: titles are known to *read* the figure, not to *depend on* it,
and 16MiB stays until one does.

**`openDataBase` owes the specification's exception.** Opening with `create`
false and no database throws `DataBaseException`, which titles use to detect a
first run; this platform ignored the flag and returned an empty database. One
title read record zero, caught the `DataBaseRecordException` and dereferenced the
array never produced; another recursed until its guest stack overflowed. Both
start now. A database opened for creation exists immediately, with no record, so
the next open — also in a later session, via the save store — finds it. **A call
that cannot fail is one whose failure a title never learns about**; both titles
stopped inside their first-run paths. The correct answer also broke the
catch-block title above for one measurement, which is why the label fix belongs
to this round.

**A title's own console output is evidence, and was discarded.**
`java/io/PrintStream` was all no-ops, on the assumption that guest console output
reached the Host through the logging boundary — but that is `MC_knlPrintk`, the
WIPI-C call, so `System.out` said nothing. The two whole-line overloads now log at
the same level and place as printk. They named three of this round's four
failures — the percentage, the database, and the title's report of what it could
not open — none visible in the trace, which only showed the same three
boundaries crossed repeatedly. The numeric and character overloads stay silent:
titles write those a character at a time, and the String form carries the line.

<a id="implementation-the-seventh-round-three-answers-a-titles-own-code-had-already-written-down"></a>

#### The seventh round: three answers a title's own code had already written down

Five more of the 264 reach a first frame, none needing a new mechanism: each is
a call already served with an answer no title had yet contradicted. **Four were
found by reading the title's own code rather than the trace** — the failing
call's caller, the module's string pool, and once the line the game prints about
an exception it caught.

**A null clip array clears the clip.** One title stopped in `MC_grpSetContext`
reading sixteen bytes at address zero: the clip array pointer was null. Its
setter computes the wanted rectangle, compares it with the destination surface's
size, and when they match (the whole surface) passes a zeroed array argument.
Zeroes are how a never-clipped context reads here (`MC_grpInitContext` zeroes the
record, and an empty rectangle means no clip), so the null is answered by writing
zeroes into the field.

**An `MC_GrpImage` names its framebuffer rather than inlining it.** The record
began with the five framebuffer fields (width, height, bytes per line, depth,
pixels), so the image handle doubled as a framebuffer handle. A title using the
handset's own macros follows the image handle to its record, reads word zero as a
*framebuffer handle*, follows that, and switches on depth with
`(bpp << 24) >> 27` (2 for sixteen-bit, 4 for thirty-two-bit); with inlined
fields word zero was the width, and it dereferenced 74 and faulted. The record
now names its framebuffer in word zero and its mask in word one, and every
surface read follows that naming, distinguishing the two by whether the first
word is an allocation this platform issued. The inner record lives and is freed
with the image, and `MC_grpGetImageFrameBuffer` answers the framebuffer.

**A String constructor disagreed with its ranged form.**
`new String(byte[], charset)` accepted only UTF-8 and threw `IOException`
otherwise, while `new String(byte[], int, int, charset)` and `getBytes(charset)`
used the charset table with the handset's encoding. A title decoding its own
`EUC_KR` record caught the exception, printed it, and painted an empty screen —
four hundred ticks, eight hundred flushes, no lit pixel. All three constructors
use the table, and the ranged one is now *declared* beside the other six (it had
a body but no declaration, so interpreted code found nothing).

**`printStackTrace` made it readable.** It was a no-op because this runtime does
not walk the guest's ARM frames, but the class and message are a real trace's
first line, and the title's report of a recovered failure. They now go where a
`System.out` line goes, as decided a round earlier; the printed line was the
whole finding.

**A widget toolkit that is one text box.** One title asks for `com/ktf/kfc`, the
vendor toolkit, left alone for two rounds because answering one member moved the
failure to the next. **The module's string pool answers it in one pass**: an AOT
module stores every linked name and descriptor verbatim, so scanning around
`com/ktf/kfc/` lists the demand as a set. It is five classes — a form, a form
with a menu bar, a message box, a text field and its change listener — with a
modal dialog's members: `doModal`, `setString`, `getString`, `setMaxLength`,
`setIMEModes`, `getGTextListener`, and a form's geometry: the box the title asks
a name in, not a screen-layout toolkit. Answered as a dialog that closes at once
with the field holding its given text, the title reaches its main menu. What the
demand turned on: `Component.setEventListener` had no body, and
`getGTextListener` answering null made the title throw its own
`NullPointerException` in its constructor — on a handset a listener always
exists, so one is built on first request.

**A picture carries two checksums.** Chunk CRCs were already rewritten after a
title recoloured a palette in place. `IDAT` pixels are a zlib stream ending in an
Adler-32 of the *uncompressed* data, which handset decoders also ignore; the same
title ships a sprite sheet that inflates perfectly with a disagreeing trailer. It
is repaired the same way — inflate, hash, rewrite the trailer — and only after a
decode fails, so no other picture's path changes. A stream that does not inflate
is left alone.

**A fault inside a timer callback carries the same report as one in a method**
(registers, stack frames and the objects they name); it previously did not, for
exactly the title whose whole loop is a timer, and it named the image record
above.

<a id="implementation-the-eighth-round-a-card-the-display-was-not-showing"></a>

#### The eighth round: a card the display was not showing

**`Card.serviceRepaints` was the only path that painted a card the display was
not showing.** The frame loop refuses one (`repaintQueued` and `paintTopCard`
require a pushed card), and `Card.isShown` answers against the top of the pushed
stack; this call alone could enter `paint` for a never-shown card.

One title of the 264 loads resources in stages inside `startApp`, calling
`repaint` then `serviceRepaints` between stages to move a progress bar, and
pushes its card only after loading. Entering `paint` there ran its drawing code
against unbuilt state, stopping in its own compiled null check on a static
reference a later branch would set. Answered like the rest of the runtime, it
reaches its first frame.

**Two other titles paint the same picture one tick later; nothing else in 264
rows moves.** Both also `repaint`, `serviceRepaints`, then `pushCard`, so their
first frame moved from `startApp` to the next tick: frames byte-identical, the
probe's stop tick 0 to 1 — the rule's whole measured cost.

The specification says `serviceRepaints` calls `paint` itself ("이 함수 내에서
직접 paint 함수를 부릅니다") and is silent on unshown cards, a case a handset
need not describe. The runtime had already decided in three other places that a
card not on the display has no screen to draw to.

**A concrete calendar that did not extend the abstract one.** Another title
faults in `paint` reading `0x69747544` (ASCII `Duti`) right after
`new java/util/GregorianCalendar` and a resolve of `java/util/Calendar.get(I)I`.
The class loader's fallback builds a record extending `java/lang/Object` with
Object's vtable and allocates the name string right after that table. A virtual
dispatch indexes the *receiver's* vtable at the *declaring* class's slot, so
`Calendar`'s slot overran the Object-sized table into the characters of
`java/util/GregorianCalendar`, which the guest followed as a pointer; the
neighbouring register holds `0x6974752f` (`/uti`), the same string four bytes
in.

The class is now declared, extending `java/util/Calendar`, with its one own
method; calendar methods key on the object's contents, not its class name, so a
directly built one matches a factory one. **General shape: any concrete class
whose superclass this platform implements but which reaches the fallback has a
vtable ending where callers do not expect.**

**A step-limit failure that is a clock spin, and the title behind it plays.**
One title of the 264 draws two frames, then spends its whole service allowance
inside a timer. The last three thousand trace entries are one boundary —
`wipic 0x1c`, the current-time call — the busy-wait-on-the-clock shape. A
stepped run holds the guest clock still during a Host service call, so the
awaited deadline never arrives; with `-play` it reaches its copyright screen in
six hundred ticks and an in-game record screen in three thousand. **It is not
failing; the sweep's stepped judgement cannot pass it**, nor any title that
waits inside a callback. The general answer is LGT's work clock (`lgt.md`, "The
guest clock advances with work, not only with ticks"), which would change every
title's timing and is not made on one title's evidence.

Here a `ManualClock` is installed only by this command's stepped probe; browser
sessions and `-play` run on the wall clock, where the guest clock moves during a
service call and a spin ends by itself. A work clock would change what the probe
measures and nothing a player sees — the opposite of LGT, whose clock is virtual
end to end — moving one local title to "probe passes" at the cost of
re-baselining every stepped measurement. It stays undone until a second title
needs it; `-play` serves this one.

**A screen wiped by the second of two paint paths.** Another title loads
everything it names (three textures, five pictures, a sound clip), prints its
initialization log, and then shows a uniform fill indefinitely. The boundary
trace shows two alternating paint paths, only one of which draws:

```
access context, fill rect, draw image ×3, flush lcd     <- the title's own
Clet$CletCard.paint -> paintClet(x, y, w, h)
access context, fill rect,               flush lcd      <- this platform's frame
```

The title's own path draws and flushes its screen; the following card paint
fills and flushes again, and the fill remains — drawn then covered each frame.

**The second path is entirely this Host's.** The title never calls
`MC_grpRepaint` (zero across the run); it is the per-tick `ServicePaint` of the
top pushed card, on which every other local title of this generation depends.
Right for titles drawing from their card and wrong for one drawing and flushing
from its own loop, it needs a rule, not a switch. The `MC_fsOpen() failed!!!(-12)`
line this title was previously filed under is its save not yet existing on a
first run, printed and passed.

**The rule is "who published the last frame".** A flush from outside the card
paint is the title putting up its own frame, and it keeps the screen for that
round: `paintTopCard` skips an unrequested round when the guest flushed on its own
since the last. The flag is cleared on skipping, so nothing is classified in
advance — a title publishing every frame is skipped every round, one that
flushed once at start loses one paint. A requested repaint always paints.

**Whole-set A/B: it cost one flush.** Over 261 archives, sixty rows change, and
fifty-seven only in flush count, by one or two, with the same tick and lit-pixel
count — the redundant paint gone. Three change picture: two are one title
packaged twice, going from a black screen with only the status bar to the
publisher's logo screen it was drawing all along; the third stops one flush
earlier, before its status bar is drawn, and `-play` on both builds reaches the
same title screen **byte for byte**. The target title now draws its own screen
and menu under `-play` instead of a uniform fill.

<a id="implementation-a-frame-the-title-did-not-ask-for-is-a-step-it-did-not-take"></a>

#### A frame the title did not ask for is a step it did not take

**The rule above had a missing Java twin.** A title can publish its own frame
from C (`MC_grpFlushLcd`, above) or from Java: `Card.repaint` to ask and
`Card.serviceRepaints` to enter `paint`, once per frame of its loop. The Host's
per-round paint kept running beside the latter, giving a self-driving title
**two paints per frame it asked for.**

**A frame loop inside `paint` advances the world once per entry.** One local
title scrolls there: a `copyArea` of the whole 240x320 screen by ten pixels, a
fresh twenty-pixel tile column at the edge, then sprites. Its loop asked for 583
frames in 1,250 rounds while `paint` was entered 1,833 times (583 from the title,
1,250 from the Host), so **the screen scrolled three times per step the title
took.** One probe line per round:

```
scroll dx=-10 → column x=230 surface y=300
scroll dx=-10 → column x=230 surface y=300   <- same column, same height
scroll dx=-10 → column x=230 surface y=300
scroll dx=-10 → column x=220 surface y=300
```

One terrain column laid at three offsets turns a slope into a sawtooth, and a
world advancing three times too fast downhill pushes the ground off the bottom
of the screen. It looked like a drawing bug; `copyArea`'s argument order, its
overlapping-self snapshot, the slope tiles' palette transparency, the tile
decode and the anchor were all checked against the archive and correct.

**Calling `serviceRepaints` does not mean a title drives its own screen.** Some
local titles call it every frame; others two or three times in a run (a load
screen) and then draw from unrequested round paints. Standing the round paint
down for good on the first call froze three of those (flush counts from about
600 in 600 rounds to 3, 4 and 36), and call counts do not separate the shapes.

**The initial fallback used the gap.** Over 600 rounds a self-driving title
returns within two or three rounds, at worst seven; one that has handed the
screen back stays away hundreds of rounds or for good. The round paint stands
down for eight rounds after a guest-painted frame and resumes when the guest
stops — counted in rounds, not milliseconds, because it bounds the Host's loop.

**Whole-corpus A/B.** Over the 46 local KTF archives at 600 rounds each, none
stops or changes its error, and all reach tick 600. Seventeen rows change,
fourteen only in flush count: four large ones are the defect going away — 900 to
301, 889 to 289, 851 to 249, 795 to 245 — each landing on the title's requested
frame count; the rest move by one or two. Three rows differ in lit-pixel count,
none due to this change: run alone, both builds agree on all three — a
**nondeterminism in the batch itself**, worth its own look.

**It is cheaper too.** The title above went from 1,833 `paint` entries in 1,400
rounds to 488, one per requested frame, each entry being guest code plus a frame
encode; the only new per-round work is one integer compare.

**The fault report looked like a thread problem and was not.** The stack named
an image array and a `paint`, suggesting a loading thread racing the frame loop.
The boundary trace showed every `createImage` completing and every array store
landing, one call stack from `startApp` to `paint` with no thread, and
`Card.repaint` then `Card.serviceRepaints` immediately before the fault. A trace
showing the call that caused the paint beats a stack showing where it ended.

<a id="implementation-a-slow-scene-still-belongs-to-its-live-worker"></a>

##### A slow scene still belongs to its live worker

The eight-round timeout alone is insufficient. A September 2026 input replay
reached a merchant table whose worker prepared the scene in stages. The Host
painted during a gap between those stages; the guest's `paint` cleared its
partial/full redraw flags before the next stage consumed them. The scene stayed
at stage 3, before its key handler accepted input. Another 1,000 idle ticks and
20 held-key trials left the table unchanged, although key delivery continued.

A successful `Card.serviceRepaints` now records the receiving card on the
active worker. While that worker remains live, the Host does not paint that
card without a repaint request, however long the gap. Explicit requests still
paint; another card, or a card whose worker has exited, remains eligible for
automatic painting. The existing eight-round grace period remains for paints
without a live worker owner, preserving the bootstrap fallback above. The
[WIPI Card specification](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Card)
defines `serviceRepaints` as a direct call into `paint`; ownership prevents the
Host's compatibility fallback from adding calls between the worker's requests.

Replaying the same recorded inputs from a fresh save copy completes the trade
and returns to gameplay. A separate 600-tick startup comparison covered all 36
archives currently in the local KTF directory: 35 completed with identical
final PNGs; one failed before and after. Seven successful cases changed only
their flush counts. This is a startup check, not full gameplay coverage.
`TestWorkerPaintKeepsFrameStateUntilTheNextRequest` protects staged state across
1,000 idle rounds and checks explicit repaint, worker exit, and card replacement.
The ignored replay and comparison artifacts are under
`var/acceptance/input-fix/`; the original saves were not changed.

<a id="implementation-the-ninth-round-a-card-that-was-never-told-it-was-on-the-screen"></a>

#### The ninth round: a card that was never told it was on the screen

**One title of the 264 divided by zero twice every frame; the divisor field is
filled in `showNotify`.** Its `paint` drew its pictures and threw where it needed
the number; the trace named the method, and the method's code named the field —
a `getfield` whose zero the compiler checks before throwing
`ArithmeticException`, as `x / f` compiles. The field's single writer is reached
from three call sites, the first being the card's `showNotify(boolean)`.

Nothing here called it: `Card.showNotify` was only the no-op a title's own call
lands on, and `pushCard`, `popCard`, `removeCard` and `removeAllCards` changed
the stack silently. The specification puts the call exactly there: "카드가
`pushCard`, `popCard`에 의해서 보여지거나, 보이지 않게되는 경우에 `showNotify`라는
함수가 불립니다."

**"Shown" keeps its existing rule**: the top of the pushed stack, as answered by
`Card.isShown`, required by `repaintQueued` and painted by `paintTopCard`. A
change of top hides the old top and shows the new; pushing the card already on
top notifies nobody — one rule for all four stack operations.

**Whole-set A/B: no cost.** Over 264 archives at 400 stepped ticks two rows
change: the title above, from 400 ticks of black to its publisher's logo at tick
one (and title screen and menu under `-play`), and one reaching the same first
picture (same pixel and colour counts) one tick earlier, because its `startApp`
pushes its card and the notification now runs there.

<a id="implementation-the-tenth-round-four-members-of-the-standard-library-and-the-eleven-behind-them"></a>

#### The tenth round: four members of the standard library, and the eleven behind them

A hand-played browser sweep reported four titles stopping on class-library
members the AOT link could not find: `java/lang/Boolean`'s constructor,
`StringBuffer.append(char[],int,int)`, `String.replace(char,char)` and
`java/io/ByteArrayInputStream`'s protected `buf`. **Four titles is not the
defect's size**: every title links against the standard library, and these four
were played far enough.

**Three were already implemented.** `String.replace`, the two char-array appends
and five more `StringBuffer` members had core-library bodies but no declaration —
reachable by native dispatch, not by compiled code (see [`jvm.md`](../jvm.md),
"A body with no declaration"). Eleven were in that state. The KTF table publishes
those a guest can name, and both layers have a tripwire: the core library fails a
test for a body without a declaration, and this platform already failed one for
a published method without a VM body.

**The fourth is a field, which is published rather than declared.** A title
subclasses `ByteArrayInputStream` and reads `buf` to decode straight from the
streamed array; it joins the published-field mechanism with the constructor as
its only mutator (see
[A published instance field](#implementation-a-published-instance-field-has-two-storages-and-the-boundary-is-where-they-agree),
including the `Boolean.TRUE` registration-order change).

**Whole-set A/B: no other change.** The 261 KTF and fifteen SKT archives at 400
stepped ticks are identical on both binaries (ticks, flushes, lit pixels,
errors). The underlying field-resolution change affects every interpreted class
inheriting a field, not just KTF, so the sweep shows nothing depended on the old
answer; LGT does not use this VM.

**The four titles now**: three run their reported sessions to the end (a page
log timestamps every key press, so dividing by the guest's tick length replays
the player). The fourth fails only in the browser, a few presses into a menu
whose timing replay does not reproduce; here the field is shown resolving from
the class record in a test, not in a run.

<a id="implementation-the-eleventh-round-an-address-made-of-the-detectors-own-fill"></a>

#### The eleventh round: an address made of the detector's own fill

Five titles of the browser sweep died on addresses nothing had computed; the two
most informative read `0xdfdfe02b` and `0xdfdfdff0` — the arena's use-after-free
fill plus an offset: the title freed a structure, read a pointer back out of the
freed block, and followed it.

**The instrument caused the failure**; the switch from fill to copy is described
under
[A screen that only waits still allocates](#implementation-a-screen-that-only-waits-still-allocates).
The A-grade title runs its whole reported session — two and a half thousand
ticks of another player's input replayed from the page log — with the copying
detector, and stops after 160 ticks with the fill. The detector kept working: 816
blocks recorded on release, 950 checked on reuse, no released byte written. The
261 local archives booted 400 ticks on debug builds with fill and with copy are
identical line for line.

**The other three were different defects.** Two are LGT's ([`lgt.md`](lgt.md#implementation-the-lgt-platform),
"A title draws past the surface it was given"): a fill loop walking off the LCD
onto platform records. The third writes a save on its first run, exits
deliberately, and on its second run dereferences a zero lookup result at the
same instruction every time. **A title whose first run ends itself is a
different program on its second run**, which a sweep from an empty save
directory never sees; the next round covers it.

<a id="implementation-the-twelfth-round-a-record-list-counted-in-bytes-and-the-stack-a-title-read-instead"></a>

#### The twelfth round: a record list counted in bytes, and the stack a title read instead

The title above is fixed. **Three platform calls and a resource load separate the
wrong call from the faulting instruction, with nothing reported in between.**

**Reproduction needed the wall clock removed.** Under `-play` it faulted about one
run in three — not a race: the title seeds a choice (character and course) from
the clock, and only some choices reach the broken code. A route replay stays on
the manual clock, jumping to each deadline, so the seed repeats:

```sh
printf 'wait 600\n' > /tmp/wait.route
wfeature runktf <archive> -save <a save tree the title has already written> \
    -route /tmp/wait.route
```

Every run then faults on the first tick. `TestLocalBreakpointProbe` moved onto
the same clock and gained a save root, since "the second run" is otherwise
unreachable for it.

Backwards from the fault:

- **The faulting instruction** is `ldr r3,[r0]` with `r0` zero, right after a
  resolve-resource-by-name helper (`MC_knlGetResourceID` then
  `MC_knlGetResource`) that returns zero when the first answers negative — which
  a breakpoint on its error path confirmed.
- **The name was the empty string.** The `-diag` trace lists every resource
  request in order; the last before the fault is `resource ` with nothing after.
  Eight of the round's nine names are `char_…` files in the archive.
- **The names are in a runtime-built table of 32-byte slots in the image's
  BSS.** The first eight calls index it by constant, the ninth by
  `*(M_Uint8 *)(record + 0x21) - 1`; that byte was `0x4f`, so it read slot 78,
  far past any written slot and still zero — hence empty rather than wrong.
- **`0x4f` is a byte of the title's uninitialized stack.** A watch on it names
  its writer, which copies it from a 128-byte buffer just passed to
  `MC_dbSelectRecord`; that call returned `M_E_INVALID` and wrote nothing, and the
  title does not check.

**`MC_dbSelectRecord` failed because its record id was never written either.**
The title calls `MC_dbListRecords` to collect ids and selects the fourth; this
platform wrote three.

**The list call's third argument counts identifiers, not bytes.** The
specification's "the size of the buffer" over an `M_Int32 *` reads either way,
and this platform used bytes, so a twelve-entry array got three ids and nine
untouched words. The title settles it: it reserves `0x30` bytes for the array,
passes `12`, and indexes entry three — forty-eight bytes is twelve ids, and only
the count reading fills the entry used.

**It is a contract, not a compromise.** A bytes caller passes four times an
entries caller's number, so the count reading overruns a bytes-sized array once
the database holds over a quarter of that number; no answer is safe under both,
so evidence decided. The remaining guard: a database is never asked for ids it
lacks. Two specification answers were added: a null buffer or non-positive count
is `M_E_INVALID`, not an empty list.

**General shape: an unchecked error becomes a value invented later**, in a
register nothing traces back. Here: a list call nine entries short → a record id
read from a stack frame → a refused select → a structure field made of that frame
→ index 78 into a table of fourteen → an empty resource name → a not-found lookup
→ a dereferenced null. Only the last link is in the fault report.

**Whole-set A/B.** The 264 local archives through the same routed 400 ticks on a
debug build are identical before and after, this title included — its first run
writes its save and exits at tick one, never reaching the list call. Only the
second run, against the first run's save tree, moves: from a fault on tick one to
the whole route.

<a id="implementation-the-thirteenth-round-who-catches-what-a-callback-threw"></a>

#### The thirteenth round: who catches what a callback threw

The family left open in the fourth round — five browser-sweep titles ending on
an uncaught guest exception — was one question about this platform and one about
the titles, and the earlier measurement had them reversed.

**An uncaught exception ends the callback, not the program.** Every guest
callback — a card's `paint`, a `keyNotify`, a queued Runnable, a timer task, a
thread's `run` — is entered from a Host service call, in a guest frame this Host
built. A title whose frame loop is a thread wraps its work in `catch (Exception)`
on *that* thread, while the paint this Host runs for a `repaint` is on the client
thread with an empty handler chain, so a throw its own loop would swallow reached
the bridge and ended the session.

The language ends only the *thread* on an uncaught exception, and a Host
callback is less still: a callback that threw did not happen, and the next one
runs. `Client.absorbUncaughtCallback` applies this to a tick's three service
calls. **Only a `jvm.GuestException` is absorbed**; an unmapped read, an
unpublished member or a Host limit still stops the run.

**It is counted, not swallowed**, since a session surviving every paint looks
like one that paints and an exit-code sweep would promote a dead title. Class and
callback go into diagnostics, the first into the run summary as `uncaught` and
`uncaught_first`, and each into the log at warning level.

**The handler record's label is written before each protected call and again on
exit; the half-open range was right.** Two of the five throw at a label equal to
the last range's `to`, which looked like an off-by-one (an inclusive bound rescued
one). Compiled code settles it: inside a region every call site is preceded by
`movs r3,#<label>; str r3,[sp,#<record+12>]`; region `[0x76,0x7f)` covers exactly
labels `0x76` and `0x7c` written before its two calls, while `0x7f` is written on
the branch leaving the region. `[0xc5,0xcc)` matches: `0xc5` and `0xc9` before its
calls, `0xcc` written by a `movs` branching to the shared store and out. **A
throw carrying `to` is a throw after the try**, and an inclusive bound would run a
catch block for an exception raised outside it.

Outside every region the label is *stale* — maintained only where a handler needs
it, so it holds the last protected region's value — which is why survival cannot
decide: a stale label falls inside a range as easily as outside, and widening the
test is as likely a wrong catch as a right one.

**A failed handler search reports what it looked at.** "No handler" means either
no catch or an unmatched one, and only the chain distinguishes them. A failed
search counts a diagnostic naming every chain record, its method and body
address, its label, and each entry's range and target; both readings above came
from such lines.

**The whole-set A/B measured the wrong thing.** The 264 archives through the same
routed 400 ticks are identical before and after, none reaching an absorbed
exception — each title in this family needs a dozen key presses, and a boot sweep
presses none. The corpus run is the regression net, not evidence for the change;
that came from replaying the reported sessions as routes: two titles that ended at
ticks 64 and 154 now run their whole scripts.

**Two titles' remaining failures are their own arithmetic.** One indexes a fixed
32-entry row table with `height / 10 + 1`, which is 33 on the default 240x320
screen and fits on any handset with a shorter card; at `-screen 240x310` it plays,
drawing every tick instead of 82 frames in 330. Its own thread catches the
exception, so the absorbed paint matches the handset. The other indexes a
zero-length `String[]` it allocated. Neither is a platform defect or ends a
session now.

<a id="implementation-the-fourteenth-round-three-drawing-arguments-and-only-one-of-them-was-the-titles"></a>

#### The fourteenth round: three drawing arguments, and only one of them was the title's

Three browser-sweep titles stopped on drawing calls with impossible arguments — a
colour depth of 805486848, a pixel range four times its array, a framebuffer
2464 pixels wide. Two were this platform misreading an argument; the third was a
title reading its own freed memory.

**A blit's line pitch is bytes; this platform read elements.** The specification
names `Graphics.setRGBPixels(x, y, w, h, int[] pixels, offset, bpl)`'s last
argument "한 줄의 이미지가 저장되기 위해서 필요한 바이트 수" — bytes per line — as
`MC_grpSetRGBPixels` was already read here, but the Java side ran every range
check four times too long. The reporting title passes an 88x102 picture in an
8976-element array with a line of 352 (88 four-byte pixels; 88 × 102 = 8976) and
died on tick nine, its first frame. The specification's two throws for this call
— `ArrayIndexOutOfBoundsException` for a range past the array,
`NullPointerException` for a null one — are now guest exceptions, so a title's
own catch can see them.

**A surface is bounded by cost, not side length.** A per-dimension cap of 2048
refused a 2464x32 sprite strip (154KB) while allowing 2048x2048 (8MB). The
existing byte budget is the meaningful bound; the remaining side bound only keeps
the multiplication in range.

**The third draws an image it destroyed.** The heap-address "depth" was word three
of a former record: the title calls `MC_grpDestroyImage`, the arena reissues the
span, and a later frame blits through the kept handle. On a handset the contents
outlive the free and the old picture draws — the arena-poison shape (see
[the eleventh round](#implementation-the-eleventh-round-an-address-made-of-the-detectors-own-fill))
from the other end. **Drawing nothing is the honest version of undefined
contents**, as the specification says for a source not at the screen's depth. A
released handle is distinguished from a never-issued one by remembering the last
256 addresses `MC_knlFree` returned (cleared when the arena reissues one): a
released address draws nothing and is counted; anything else still fails as this
platform's bug.

**Whole-set A/B moves one row.** Across the 264 archives through the same routed
400 ticks, only the title whose first frame used to end it changes: tick 9 and 9
flushes become 400 and 400. The other two need a dozen key presses, so their
replayed sessions show the fix: 148 ticks to 377, and 901 to 1254 with lit pixels
from 2880 to 65047.

**The reports named the argument, so two of the three took minutes.** A record
that does not decode now reports the handle, record address, the five words read,
the eight words around the handle, whether this platform issued either, and the
calling guest address; a failed range check reports rectangle, offset and line.
"words=[0x3002c1d0 …]" showed a linked structure rather than a framebuffer, and
"rect=[0 0 88 102] offset=0 bpl=352" showed 352 = 88 × 4 without disassembly.

<a id="implementation-the-fifteenth-round-two-host-ceilings-a-title-reached-by-playing-normally"></a>

#### The fifteenth round: two Host ceilings a title reached by playing normally

Two browser-sweep titles ended on limits this platform imposes — the guest worker
count and a Host service call's step allowance. Both ceilings are worth having;
each was counting the wrong thing.

**A thread that finished is not a slice.** A round grants one guest worker a step
slice, sharing rounds among threads still competing for the next round; a worker
that returned from `run()` competes for nothing, and holding the queue behind it
only lengthens the queue. One title shows it in about ten seconds: it starts a
thread per sound effect, two a round, and each round retiring one left the queue
longer — 253 started, 189 retired, 64 alive and the sixty-fifth stack refused. A
grant ending with the worker finished is no longer charged to the round's limit.

**What bounds the drain is steps, not a count.** A round grants at most one slice of
execution beyond its entitlement, so retiring a hundred threads of a few thousand
steps each is allowed, while one spending a whole slice before returning ends the
round. This protects the following frame without a second arbitrary number. The
queue now sits at two instead of climbing, and the title reaches its base screen
and holds it for three thousand ticks.

**Steps are the wrong unit for a call that is waiting.** The other title's
opening is a WIPI timer callback that draws, waits, draws again, waiting by
polling `MC_knlCurrentTime` in a counted delay loop (`r3 += 25` up to 999999,
about 160,000 instructions between polls), since a handset callback has no
scheduler to yield to. A 5.2-second wait is 828 million steps against an
allowance of 500 million, and the title died on a black screen. A window spent
reading a clock that moved under it is now renewed free, bounded instead by the
session clock through `SessionOptions.ServiceWait` and `ErrServiceWaitLimit`.
Fifteen seconds is three times the longest measured wait — a bound on the Host
freezing, since while the client thread is in that loop no worker, timer or
paint runs.

**The clock must have moved, which keeps a batch Host honest.** A stepping Host
holds the session clock still during a service call, so free renewal there would
turn a three-second failure into one that never returns. Free renewal requires
the clock to have advanced since the window opened, which on a stepped run it
never has; that run still fails on the step allowance in the same three seconds,
the case where [`cli.md`](../cli.md) recommends `-play`.

*(The second half of that is now only true of a clock nothing moves at all. A
Host that owns its clock runs it at a rate while a service call holds it, so a
guest waiting there sees its wait end — see "A batch Host's clock had no rate".
A call that never returns still fails on the step allowance, in the same
seconds.)*

**Whole-set A/B: no verdict moves.** Across 264 archives at 400 stepped ticks one
line differs: a title's first lit frame arrives a tick earlier, frames identical,
because a finished worker now retires in its own round. The second pass,
`-play -speed 8` over 600 ticks, differs in 55 lines, only in flush counts or
lit-pixel totals; re-run alone, the two largest movers match across builds. No
`tick_error` or exit code differs in either pass.

**Eight-times-speed batches had filed this as "not a fault".** At `-speed 8` a
5.2-second wait costs an eighth of the steps, under the ceiling, and the stepped
pass's failure was correctly read as the clock standing still. Nobody had run the
title at a person's speed — the one where the wait is long and the clock moves.

<a id="implementation-the-sixteenth-round-a-message-a-title-sends-itself-and-a-save-that-was-renamed"></a>

#### The sixteenth round: a message a title sends itself, and a save that was renamed

Three of the browser sweep's titles ended on a WIPI C call this platform did not
serve. Two of them are the same call, and it is one a title makes to itself.

**Kernel slot 9 is `MC_knlGetCurProgramID` and graphics slot 36 is
`MC_grpPostEvent`, which only make sense together.** Two titles from different
publishers share a middleware routine compiled to
`MC_grpPostEvent(MC_knlGetCurProgramID(), type, param1, param2)` — a message the
program queues for itself, addressed by its own identifier. Each identity settles
the other: the specification documents `MC_grpPostEvent`'s first argument as the
target program's application identifier, and its no-argument kernel-table
neighbour answers one. The numbering agrees: the kernel table's three extra slots
put `MC_knlGetCurProgramID` at 9, and the graphics table's two absences
(`MC_grpFlushLcd` and `MC_grpCreateSubImage`, fixed by identified neighbours) put
`MC_grpPostEvent` at 36. LGT has the same pairing under another engine; see
[`lgt.md`](lgt.md#implementation-the-lgt-platform).

**The identifier only has to be stable.** This platform runs one program, and
titles hand the number straight back. It is derived from the archive's own
identifier so archives never share one, and forced away from zero, which callers
read as "no program".

**A queue needs a reader.** This posted event was the one queue item nobody
drained: a title with its own `getNextEvent` loop reads its own queue, and
Host-originated events go straight to the card stack. `Client.ServiceEvents` now
drains, per round, what was waiting when the round started; a handler answering
an event with another queues it for the next round, so two cannot hold a round
for ever. It is delivered as the queue's notify event to the Jlet's registered
listener — in both titles their own card, whose `notifyEvent` the middleware
awaits. Inline delivery would re-enter the card mid-draw, since both post from
inside `paint`.

**Table 7's slot 7 is `MC_fsRename`, and a title's save waited on it.** The call
site settles it: three arguments, the third the constant 1 every call in this
table passes for the program's directory, made after closing the named file. The
title writes installed data to a scratch name and renames it over the real one —
an atomic commit — so the bytes move in the live store, the persisted store, and
the removal list (the save store has no delete). Open handles follow the file,
since only its name changed.

**A leading separator is not part of the name.** The same title opens `/lo.dsk`,
stats `lo.dsk` and renames onto `lo.dsk` — one file, because the specification
makes every name in this section an absolute path inside the area `aMode` names.
In the flat store the spellings were two files, so the title could not find what
it had written. Every name-taking slot now uses one key; what follows the leading
separator stays part of the name (a title's own directory names different
files). No local save was keyed with a leading separator, so none was orphaned.

**What the title does next is a separate question.** With rename served, it
installs its 1.7 MB resource archive (a well-formed zip, written and read back)
and calls `MC_knlExit`, as a first-run installer does to be restarted. The second
run reads the whole archive and exits again from inside `startApp`, drawing
nothing and allocating nothing — where to pick this title up.

<a id="implementation-members-of-four-vendor-tables-and-one-class-the-archive-does-not-ship"></a>

#### Members of four vendor tables, and one class the archive does not ship

The browser sweep's largest remaining family was AOT link failures — a member
missing from a published class: twelve titles in four groups, each member real,
documented and small.

**Two are the specification's lcdui.** `Jlet.getCurrentJlet` is the
specification's second name for the active Jlet; a multi-program handset
distinguishes "on top" from "running now", this platform cannot, so both answer
the one object. `Image.stopImage(ImageObserver)` releases an observer's reference
to a picture the JVM here owns and collects; the call must still exist.

**Six are the lwc toolkit, and the field mattered.** `TextComponent.imHandler` is
a protected field on the handset, and three titles take the input-method
automaton from the component and register their own listener — how a title
drawing its own text field receives characters. Since the guest loads the word
at the record's offset, the handler is published into the payload too, with a
`fieldSyncs` entry keeping both in step; the lookup walks the runtime superclass
chain, because a text field holds the same word in the same place as the text
component it extends, set by its own constructor. Also added:
`InputMethodHandler.setInputMethodListener`, `TextFieldComponent.setString`,
`ButtonComponent` with the specification's string-and-picture constructor and
action listener, and the shell's title, command and `isShown` — calls a title
makes building a screen from the toolkit and reading it back.

**None of it is drawn**, as with the vendor toolkit: a component keeps and
answers back what it was given, so a component-built screen yields its structure
and no picture. Two of the twelve stop on that rather than a missing member — a
separate, visible piece of work.

**Two are the vendor's `com/ktf/kfc`.** `ChoiceText` is a list of strings built
from the array it offers; `GFormComponent` is a form as a component — the
published lwc shell plus one call adding a child with its rectangle, which is
dropped since nothing here lays out containers.

**A class the archive does not ship is the handset's.** `wec/DMInfo` appears in a
module's string pool as a class name and a static answering an instance, with no
class file in the archive. The title asks it `gethandsetMIN`, the subscriber
number, which is answered with the number `MC_knlGetSysProperty` reports for
`PHONENUMBER` and certificates are sealed with — two calls asking who this
handset is must agree (see [`network.md`](../network.md)).

**The pool scan makes this cheap; the failures keep it honest.** Scanning a
module's verbatim names for a package prefix lists the demand as a set but not
each member's class, so the last step is still running the title and reading the
failure: one run per member. All twelve titles now run their scripted pass
without a link failure.

<a id="implementation-the-text-boxs-own-editing-calls-and-two-titles-that-were-not-stuck"></a>

#### The text box's own editing calls, and two titles that were not stuck

**A text box declares `setString`, `insert` and `delete`; a text field only the
first.** The multi-line box is driven by the handset's input method a run of
characters at a time, and a title editing its own box makes the same calls — the
half that works without drawing a widget. A range past the end appends, and a
delete wider than the text is clipped, since the caller computed it against a
caret this platform does not move.

**Two titles filed as "cannot leave its splash" were first-run installers.** Each
draws a notice (the game has been installed; the program must be closed and
restarted), writes a flag and ends itself — all an always-empty sweep sees. The
second run over the same directory reaches the menu, and one reaches character
creation, where its missing member was. **Do not conclude "stuck on the first
screen", or "the game exited", before running a title twice**;
[`lgt.md`](lgt.md#implementation-the-lgt-platform) recorded this from its first
commit, and it has been rediscovered twice.

<a id="implementation-a-container-that-answered-zero-children-and-who-uses-this-toolkit"></a>

#### A container that answered zero children, and who uses this toolkit

**Adds and reads used two types in one field.**
`ContainerComponent.addComponent` stored bound objects while
`getNumberOfComponent`, `getComponent` and `getIndexOf` read values, so a
container answered *empty* however many children it held, and `getIndexOf`
answered −1 for a held child — a well-formed wrong answer. One field reader fixes
it, tested by asking a container for what it was given.

**Toolkit use is now measured.** With the call counter on across every local KTF
archive, excluding `AnnunciatorComponent` (constructed and shown by every title,
not a widget), the toolkit is nine titles: five build a text field, four add a
child to a container, and one each reaches a dialog's `doModal`, a progress bar,
a shell's `show`, and the vendor's form with its text field.

<a id="implementation-a-card-that-forwards-a-key-to-its-own-text-field"></a>

#### A card that forwards a key to its own text field

**One of the nine was stopped here, and drawing was never the missing half.** Its
character-creation screen has two labelled boxes (a name for the daughter, a name
for the father); it builds a `TextFieldComponent` for each, focuses one, and
forwards keys from its own `keyNotify` to the component. `keyNotify` answered
"not consumed", the text never grew, and the boxes stayed empty.

**The counter shows the rest**: six key presses produce six `Component.keyNotify`
calls from one site and six `TextComponent.getString` calls from the next
address — the title reads the string back and draws it itself. Only the
component's handling of the key was missing.

`TextComponent.keyNotify` now runs the handset's multi-tap keypad
(`internal/textinput`, the other platforms' automaton) against the component's
own text, answering zero for keypad keys — digits, the mode key, the two delete
keys — and unconsumed for navigation, soft and select keys, so a screen with a
text field can still be left. The value stays the component's (`setString` and
the box's `insert` and `delete` write it), so the editor is resynchronised with
the field before each key and keeps only the cycle between keys.

**Zero means "taken", the polarity of a card's `keyNotify` here** — nonzero means
the key continues down the stack, matching the original runtime's traversal. The
title ignores the answer (it calls `getString` next regardless), so the polarity
follows the card convention; a title that reads it would settle it.

**This is an exception to the toolkit's rule, not a repeal.** A widget answers
keys unconsumed because taking one would steal it from the card, the only thing
drawing, leaving the player typing into something invisible
([`lgt.md`](lgt.md#implementation-the-lgt-platform)). A card calling `keyNotify` on
a component itself asked and draws the answer. The title now takes both names
and continues to the age and birthday screens.

**None of the nine waits on drawing.** Driven with a confirm key every two
hundred ticks for twenty-eight hundred, frames kept, all reach their own content
— a baseball match in play, an advancing opening cutscene, a dialogue scene, the
character creation above — the shallowest producing forty-seven distinct frames
and the deepest twenty-seven hundred. Their toolkit use matches the counter: a
text field's constructor and limit, a container's add, a component's height.

The counter also reports five *field divergences* on the text component class in
the title above, which is the guest writing over the `imHandler` word this
platform publishes — it installs its own input-method handler rather than taking
the one the constructor built, and reads the string rather than waiting for the
handler to call it. **Drawing a component is still not done** and is still the
honest description of this toolkit; what is no longer true is that a title is
known to be stopped by it.

<a id="implementation-the-older-modules-run-under-the-platform-and-all-three-of-them-play"></a>

#### The older modules run under the platform, and all three of them play

**Three local archives carry the older relocatable module, and all three called
the size they were handed as a function.** The loader passed the module header
as the entry's argument, assuming the entry read the BSS size from it. Instead
the entry calls the *first word* of its argument with a string and `-1` twice —
`getInterface(name, major, minor)`, the call the current generation makes
through the last of its initialization function's five parameters. With the
header, that word is the BSS size, so the three jumped to `0x28`, `0x40` and
`0x24`, their own sizes.

A module's entry therefore runs *under* the platform: the arena, callback stubs
and interface tables must exist before the entry, whereas the current generation
meets them in `Initialize`. `prepareInitialization` is that split, memoized so
both paths build it once.

**Past the entry is metadata this platform already reads**: twenty-byte class
records with the self-identifying first word, twenty-eight-byte method records
holding their vtable slot, sixteen-byte fields, the same object layout and
dispatch (`[[obj] >> 5]` added to a base, then the vtable at `+0xc`), and the
same slot numbering — `startApp` is 15 in both, Object's ten plus `Jlet`'s five.
What differs is who links it and where runtime state lives.

**A module publishes its classes rather than registering them.** `seg[0]` points
at a descriptor — a class table, its filled and total bucket counts, and its own
address — that the module's code never reads (only `seg[0]` and its
self-reference point at it), so this side reads and links it. Superclasses and
array element classes arrive as reference cells with a zero vtable word; linking
resolves the cells by name and lays out the table the method records number.

**The implements list stays unlinked.** Its entries are also reference cells the
module never reads; the type check is the platform's. One module's listener class
still names `org/kwis/msp/media/PlayListener` as cell `0xb` (name-table index 5,
unresolved bit set) when its first instance reaches `Clip.setListener`, so a walk
of the guest's class records cannot read it. The media listener check stopped
that title's `startApp` there until it learned to leave such an entry undecided
([Whole-corpus acceptance against
0.5.2](../testing.md#whole-corpus-acceptance-against-052)), and the type check's
registry fallback answered no for an interface target. None of the three modules
queried the type check in a 600-tick run with keys, so no local title is known to
depend on that answer. Both checks now read the cell's name from the name table
the module's resolvers index, and the fallback no longer claims a no it cannot
see ([Older module implements cells](../testing.md#older-module-implements-cells)).

**The rest of the runtime state is one block reached through `fp`.** Five words
are read, each named by the module's glue: `+0x24` parks the caller's stack
pointer while a helper runs, `+0x2c` heads the handler-record chain, `+0x30` is
the restore-function table every record copies, `+0x34` the stack the glue
switches to, and `+0x38` the base a virtual call adds the header's shifted alias
to — the JVM context. This side builds the block and holds it in `r11` on the
frame-loop thread and every guest worker; three words are per-thread, so the
class table and restore routine are shared while chains and scratch stacks are
not.

**Compiled code also reaches the block through a segment-header word** —
`[[seg + 0x20] + 0x2c] = head` restores the chain after a protected region. It is
one of the two words the loader reads as markers, the only one anything points
at, and carries the module's constant until this side writes the block's address
over it.

**Module constants name three classes by fixed offsets from the dispatch base.**
All 208 character arrays in one module carry header 0, all 208 strings over them
`0x280`, and all 58 class records `0x500` — offsets 0, 20 and 40 unshifted,
twenty bytes apart like class records. The base is an array of them, so `[C`,
`java/lang/String` and `java/lang/Class` must sit where the image expects. Strings
are built at compile time, so this side walks the image once and binds them;
otherwise the first resource name a title opens is unreadable.

**Fourteen interface slots, all calls this platform already answers.** A static
sweep for the veneer every interface call uses finds them, with the instruction
two before naming the slot: throw by class name, failed-allocation report, new
instance, new array, new primitive array, load class, class of an object, type
check, find field, initialize class, find method, array class of an element
class, and multi-dimensional new. Two need translation: a module names a
primitive by the *byte offset* into the eight characters `ZCFDBSIJ` the current
generation receives as an initialization parameter (`0x28` is `I`), and the
multi-dimensional allocator takes a pointer to its lengths, because the preceding
helper spills register-passed lengths and passes the stack pointer.

**Six more routines are tail-jumped through a table the segment header names**,
in six zero words the module leaves at `seg[5]`. Two are this platform's calling
forms (the helper checks the method record's `ACC_NATIVE` bit itself), entered
with the method record in `r0`, the receiver in `r1`, and the caller's `r2` and
`r3` pushed on the guest stack — arguments from three places. Two are the monitor
pair. One is a poll at 258 call sites, nothing set up or read back — a guest
checking in between loop iterations. The last is `wait`, reached twice, only from
a sound thread's `run`.

**A protected region is a `setjmp`.** Region entry is a call answering zero going
in and a *label* coming back, and the method switches on it; exception-table
ranges are in labels. The restore routine current-generation modules carry is
absent, so it is synthesized: restore the register block in the module's order,
put the label in `r0`, return to the region entry. The handler record's two words
are swapped relative to the current generation: label at `+16`, caught exception
at `+12`.

**Last: which Host call owns a long jump.** A module's handler record sits inside
its pushing frame at `sp + 8`, so a nested call from that frame enters on exactly
the saved stack pointer, and the rule "the handler's frame is at or below this
call's entry" also matched the innermost call. The catch block then ran in the
wrong Host call and the title continued as if the throwing call returned: it
caught the IOException from its missing save file, then read the file anyway.
The equal case belongs to the nested call, so for modules the test is strict, and
the invoke jump drops the helper's spilled pair *after* its call returns, so it
does not enter above the frame it calls out of.

**All three modules reach a first frame within eight ticks**, and under `-play`
run their openings: two publisher logos and a story screen; a title screen over
its artwork; a title screen into an in-game display. Over 264 archives at 400
stepped ticks, only those three rows change.

<a id="implementation-the-park-the-guest-is-told-about"></a>

### The park the guest is told about

A Host parks a game whose page has gone away instead of closing it: the game
keeps its memory, is not ticked for up to five minutes, and a returning page gets
it ([`session.md`](session.md#implementation-server-sessions), "A game outlives
its socket"). A handset did the same on an incoming call, calling `pauseApp` on
the way out and `resumeApp` on return. This side had the park but neither call,
so a title returned to a clock several minutes ahead with no explanation.

**The teardown argument did not apply.** These calls had been withheld because a
KTF guest thread parks inside a nested Go call stack, and running guest code into
it turns a close into a hang — true of *closing*. A pause leaves the workers
parked with their stacks intact and runs one short method on the client thread,
at an existing entry point: `SendKey` and `SendPointer` already enter guest code
between ticks, taking the run lock and making the client thread current. A Host
drives its session from one goroutine, so a park always happens between ticks.
`Client.PauseApp` uses that entry.

`destroyApp` is still not called; it *is* the teardown, and by session close
`StopThreads` has begun unwinding the workers.

<a id="implementation-what-driving-it-across-266-archives-cost"></a>

#### What driving it across 266 archives cost

Everything below was found by the sweep rather than by reading, and none of it
would have been found without one. `runktf -park <tick>[:<ms>]` is the
instrument ([`cli.md`](../cli.md)): it pauses where a server would, holds the game
parked for as long as asked, and resumes.

The first pass **killed eight titles that had been running fine**:

- **Three died in their own callback: `org/kwis/msp/lcdui/Jlet` had no such
  method.** Their `pauseApp` override calls `super.pauseApp()`, and the base class
  declared no lifecycle methods, since nothing had called one. The four base
  bodies are now registered and do nothing, per the specification.
- **Two threw `NullPointerException` in their own `resumeApp`**, one on a
  `java/lang/Thread` that is null at that point.
- **One reached unimplemented kernel slot `0x9`** inside its `pauseApp`.
- **Two requested a sound file their archive lacks**, because their `resumeApp`
  restarts the music.

**A failed lifecycle callback does not end the game.** All five ran fine without
the call, which is a Host courtesy, not a game request; a failure is logged,
counted as `lifecycle callback failed`, and the session continues as before the
calls existed — otherwise a phone would end a game whenever it backgrounds the
page. A guest that *exits* inside a callback is ending itself and reaches the
Host as from anywhere else.

With these two changes the sweep is **266 archives, zero regressions** against
the same set without a park: the same 256 pass and the same 10 fail for unrelated
reasons; one title's tick count moves between 12 and 13, as it does across two
control runs.

<a id="implementation-the-clock-the-game-lost-and-why-it-is-not-frozen"></a>

#### The clock the game lost, and why it is not frozen

A parked game is not ticked, but its wall clock keeps moving, so a resumed title
sees its absence as one long wait — as a suspended handset produced, but together
with the pause and resume calls. The missing notification was the gap, closed
above.

Freezing the clock across a park is **not** done, because measurement gives it
nothing to fix: the 266-archive run parked five seconds mid-run regresses
nothing, and five titles chosen for eventful `resumeApp` survive a minute parked
and keep drawing. Freezing would be an invented divergence from the handset.

Not covered: the park is at tick 40 of 120, not deep in a level, and a minute is
not the five the window allows. A title stepping physics by elapsed time is the
case to look for; none of the 266 visibly is.

<a id="implementation-a-touch-reaches-the-game"></a>

### A touch reaches the game

Pointer input reached the platform and stopped: `Client.SendPointer` dispatched a
touch down the card stack, but nothing above it sent one, and
`Display.hasPointerEvents` answered false to match, since claiming a touchscreen
and never delivering a touch is worse than the truth.

The Host end now uses the same three seams as a key: a `pointer` message on the
session socket, `session.Session.SendPointer`, and
`runktf -touch <tick>:<action>:<x>,<y>`. The page owns the geometry: the canvas is
laid out by the stylesheet at whatever size the viewport leaves, the frame is
*fitted* (a differently shaped screen leaves bezel), and it arrives magnified by
the server-side filter. `web/touch.js` undoes all three and sends guest pixels;
otherwise the server would need the canvas geometry every frame.

**A finger leaving the picture keeps its last point.** A handset's touch panel is
the screen, with nowhere beside it to drag onto, so a release in the bezel
releases at the last position. A resting thumb sends nothing after its press,
since a still finger emits a stream of `pointermove` events at the same pixel.

A finger down on the canvas still starts a keypad slide on titles taking no touch
(the gesture predates this, and titles depend on it); only a game answering
`can_touch` claims one.

**`can_touch` is the title's answer, not the platform's.** For one release cycle
`session.Session.HasPointer` read "is this a WIPI Java session?", claiming touch
for every KTF title. The page spends the gesture on that claim — a finger on the
picture is captured for the game instead of starting a slide — so titles ignoring
touch paid for those using it.

`ktf.Client.HasPointer` now decides: does any module-registered class carry its
own `pointerNotify(III)Z` body? A native body does not count — it is this
platform's registration, answering false, inherited by a title that wrote
nothing.

**The surface is thin.** Of the local 264, 27 archives answer true and 259 false
(5 do not load), agreeing with a static `ktfdump -symbols` scan on every readable
archive. All 27 bodies are one middleware base card,
`Clet$CletCard.pointerNotify`, in two builds (104 and 112 bytes), splitting on
pointer type and forwarding to three methods of another object. **No title wrote
a pointer handler under its own name.** Under `-touch` one visibly answers — a
dialog highlights `OK` when pressed, 98 pixels across two runs each way — and
another changes no pixel.

The check cannot see a title reading pointer events from its own `EventQueue`
loop (the queue kind is a runtime number); none is known. `SendPointer` therefore
still delivers wherever called, so `runktf -touch` can find one.

**`hasPointerEvents` answers true at no cost.** It reaches all 264 KTF titles, so
it was measured: 266 archives under two binaries give byte-identical summaries and
frames, and statically **no archive references `hasPointerEvents` or
`hasPointerMotionEvents`.** 260 carry the string `pointerNotify`, the base-class
name every `Card` subclass links against, and 6 do not read.

<a id="implementation-the-two-allocations-a-busy-session-made-and-neither-was-doing-anything"></a>

### The two allocations a busy session made, and neither was doing anything

An allocation profile of one local title over two thousand ticks (allocation, not
time, since an unrelated full-corpus sweep made wall-clock numbers noise) put 72%
of the run's 420 MB in one line and 63% of its 4.5 million objects in another —
both scratch nothing kept.

**The hooked C library copied through a per-call buffer.** The binary hooks
answer a title's `memcpy`, `memset` and `strcpy` natively (see
[Recognized C runtime routines](#implementation-recognized-c-runtime-routines)).
A hooked `memcpy` reads the source into a buffer and writes it out, and the
buffer was `make([]byte, length)` sized by the guest's request — a sprite moved a
few dozen times a frame allocated, zeroed, filled and dropped that much each time:
302 MB of the 420.

The runtime now lends scratch space, as the JVM lends an execution's frame:
bytes are read and written within one crossing, and no hook returns a slice or
passes one to anything outliving the call. **Nothing else runs concurrently** —
one guest thread holds a slice at a time until it parks, the invariant every
runtime map and buffer rests on (see `workers.go`). The buffer is not cleared
between calls because every caller fills it fully before reading, so `make`'s
zeroing was unneeded. Past 256 KiB a call allocates and releases, so one
megabyte copy does not leave the session holding a megabyte.

**The busiest counter still composed its name per crossing.** `diagEvent` exists
because formatting a name per boundary crossing cost more than the crossing (6%
of host CPU in formatting and most of a further 16% in the stack copying its call
depth caused); the converted kinds are listed beside it. The WIPI-C dispatcher,
the busiest crossing, was not among them: every graphics, file and kernel call
enters `handleWIPICCall`, which opened with `fmt.Sprintf("wipic %#x", id)`. It is
now a `diagWIPICCall` event with the slot as a number, the name composed once per
distinct slot when a report asks.

Together, on the release-build path (the profiler that ranks above these two is
debug-only and the probe enables it), the run went from **377 MB and 3.06 million
objects to 27.5 MB and 46 thousand**.

**The instruction count is the check**, not a frame comparison.
`TestLoadCostProbe` runs on a manual clock, so the count is exact; a frame A/B
cannot match that here, since drawing past the first lit frame needs the
non-deterministic `-play`. Four local titles retired identical instruction counts
before and after on the same ticks (1,072,183,468 / 290,899,832 / 167,660,025 /
66,661,016); two LGT titles matched on steps and flushes for the engine change.
The same profile on an LGT title found nothing similar — 50 MB over 1.43 billion
instructions, half the one-off archive read — so the waste was this platform's.

<a id="implementation-a-game-action-is-a-subscript-and-only-one-direction-of-the-table-knew-it"></a>

### A game action is a subscript, and only one direction of the table knew it

`Display.getGameAction(key)` answers what a key means (up, fire, clear) and
`Display.getKeyCode(action)` the reverse. Written as two switches, they held
different tables — six pairs forward, twelve reverse — so pairs known only in
reverse came out of the forward direction as the key code itself, an unmapped
key's answer, and **every key code on this platform is negative**.

Three archives — two titles of one family, one packaged for both module
generations — died on the first soft key held against a settled screen, in the
same two frames:

```text
notify KTF card MainApp key: java/lang/ArrayIndexOutOfBoundsException:
  thrown by guest code ... arrays=0x30009d90 [B runtime=200 guest=200
  ... MainApp.KeyPress(I)V+0x1e ... MainApp.keyNotify(II)Z+0x42
```

**The guest's code assigns the fault.** Its `keyNotify(type, key)` replaces a
negative key with `getGameAction`'s answer before branching on the event type;
`KeyPress` then reads a byte of its own field at that subscript. The fault report
gives array and length beside the index: a **200-byte** table, room for every
action and keypad character, read at **-6**. The title asked what the key meant
and was handed the key back.

The two directions are now one list read both ways (`runtimeDisplayActions` in
`runtime_java.go`): 1/2/5/6/8 for the pad, then this platform's block above them
— 90 and 91 for the two soft keys, 92 for the third, 96 and 97 for the volume
pair, 98, and 99 for clear. These are what `getKeyCode` always answered; the
change is that both directions agree. A sibling-platform title keeps its key map
as a per-handset resource listing `1`, `2`, `5`, `6`, `8`, `90`, `92` and `99` —
the pad, its soft keys, and clear (see
[`history/skt.md`](skt.md#implementation-skvm-and-the-skt-platform), "The key codes a
Jlet reads are not the ones a MIDlet reads") — the strongest evidence available,
agreeing with the reverse table.

**A key with no action reports itself**, so digits, star and hash reach a title's
branches as characters. That is also why this looked like a soft-key defect:
every pad key had an action and every keypad character is its own subscript, so
only the soft keys failed.

**Packaging makes no difference.** One archive is the same title in the earlier
relocatable-module packaging and died at the same index on the same array: the
table lives in the runtime Java bridge, above the client image's shape. All three
now take the soft key and keep drawing.

On the interactive rung of the ladder (the one where keys move things): a
257-archive local tree went from 238 passed / 9 skipped / 10 failed to
**240 / 9 / 8**, and a three-archive tree in the earlier packaging from 2 passed /
1 failed to **3 / 0 / 0** — exactly the archives dying on the soft key. The
change alters only answers for keys previously without an action, and of those
this rung presses only the two soft keys.

**Deliberately left alone:** the send and end keys have no action either way, so
they still report themselves and would underflow such a title's array the same
way; nothing names an action for them, no local archive asks, and inventing a
number for an unexercised hole stops a table being evidence. The other two
platforms carry the same six-pair forward table this one came from; whether their
titles ask has not been measured.

<a id="implementation-a-timer-armed-twice-is-one-timer-not-two"></a>

### A timer armed twice is one timer, not two

`MC_knlSetTimer` takes the address of a guest-owned timer record, and
`MC_knlUnsetTimer` cancels **every** queued entry naming that address, so one
record is one timer. Arming disagreed: it appended an entry per call, so a frame
loop re-arming the same record grew the queue by one a tick. The 256-pending-timer
ceiling is a runaway guard, not a budget, and one local title reached it in normal
play:

```
service KTF timer at 0x…: handle supervisor call 0x3 at 0x…:
KTF pending timer count exceeds 256
```

The queue then held 256 entries **identical in every field** (record, callback,
parameter, delay); the run counted `wipic 0x1a` (SetTimer) 513 times and
`wipic 0x1b` (UnsetTimer) **zero**. Re-arming without cancelling is an ordinary
frame loop against this API, free on a handset that replaced the timer.

**Arming an already-queued record now replaces its entry in place.** The
predicate is the record address *and* a queued WIPI-C timer entry, not a
`java/util/Timer` task, which shares the slice but is cancelled through its own
`Timer`; a kernel call must not take its slot. Deliberately not considered:

- **A running callback is not in the queue.** `ServiceTimers` takes the whole
  slice, dispatches from its copy and re-queues what it did not run, so a
  callback re-arming itself queues one entry — its next timer. That path, used by
  most of these titles, is unchanged.
- **A fired one-shot is gone**, removed at dispatch, so re-arming is a new timer.

The ceiling is checked only when an entry is added, so a full queue can still
re-arm a held timer — the case that failed.

**What it moved.** A unit test proves the semantics (two arms leave one entry; 512
re-arms leave one where the old code refused at 256), and the local KTF ladder
shows timers, which every title uses, undisturbed. Over the 257-archive local set
before and after in the same tree: parse, initialize, load and construct 257
passed; start 255 passed / 2 failed; frame 255 / 2; **sustained 252 → 253 passed,
4 → 3 failed** (1 skipped either way); interactive 238 / 9 skipped / 10 failed.
Stage by stage, **exactly one outcome in the corpus changed**: this title's
sustained rung, fail → pass.

The sweep had recorded that failure as `1 getmethod getClipX()I` — not a failure
but the first diagnostic-count line the probe printed *after* its reason (see
[`testing.md`](testing.md#implementation-testing) on why a probe's last line must
be its reason), so the defect spent a sweep filed under a count.

**Only a batch Host reaches it.** Under a wall clock — the page or `runktf -play`
— the title runs three thousand ticks without error, since real time passes
between deadlines; a batch Host takes each deadline when due and fills the queue.
It is the same queue, and still a defect.

<a id="implementation-five-titles-the-interactive-rung-failed-and-not-one-of-them-was-stopped"></a>

### Five titles the interactive rung failed, and not one of them was stopped

The rung's failure line `no key changed the screen` claims both that keys were
pressed and that the screen is one a key should move; the second was never
measured. Five archives were reported this way — four with
`over 1 screen its opening moved on to`, one with
`and it did not move on its own in the 4096 ticks after` — and filed as the
rung's limit, a verdict on a screen nobody had watched.

**Watched, none of the five is stopped.** Driven on from the same settled screen
with nothing held, each leaves it:

| the screen the rung failed on | leaves it after |
| --- | --- |
| the third screen of an opening | 323 ticks |
| a studio logo, one screen in | 2123 ticks |
| a publisher logo, one screen in | 4606 ticks |
| a publisher logo, one screen in | 8898 ticks |
| a developer logo, the screen it booted to | 23934 ticks |

and four reach something a key plainly answers. Driven past the opening with
`runktf -route`: a main menu whose highlight moves down two places under two
`down` presses; a `PRESS ANY KEY` title screen that leaves on the first key; a
scrolling prologue with its own skip prompt. The fifth answers `fire` on the
**fourth** screen of its opening after sitting still 8192 ticks.

<a id="implementation-the-rung-judged-its-last-round-by-a-rule-it-applied-to-no-other"></a>

#### The rung judged its last round by a rule it applied to no other

The rung's own comment states the principle: a screen that answered no key is
watched with nothing held, and the screen it moves to is settled and asked in
turn, because *a title whose opening outlasts the press window answered nothing
because its opening was still playing*. Every round applied it except the last,
where the loop exited before the watch:

```go
if round == ladder.SettleRounds {
        break
}
left, opening, err := tickUntilScreenMoves(session, screen, localLadderOpeningTicks)
```

The final screen — the only one whose verdict is reported — was the one never
watched; four of the five fail exactly there. **The watch now runs every round**,
and a screen that leaves by itself is reported like one that never settles: as
unanswerable, not failed. A skip naming how long the screen took to leave is a
measurement to start from.

The fifth hits the other ceiling: `localLadderOpeningTicks` was 4096, sized when
measured openings moved within a few hundred ticks of the press window; the
largest here is **23934** ticks on one screen. It is now 65536, a little under
three times that, at no measurable cost, since only screens that answered no key
wait there and the wait ends when the screen moves.

<a id="implementation-a-key-cannot-be-credited-on-a-screen-that-drifts"></a>

#### A key cannot be credited on a screen that drifts

Reading these needed `TestLocalKTFStoppedScreenAnswersEveryKey`, which takes one
archive by absolute path, walks the rung's shape, and holds **every** named key
against each settled screen. Its first answer was wrong, instructively.

In the rung's order — press, then watch — it credited `hangup`, which no title of
this era acts on. The screen moved on its own: `hangup` is the eleventh of
twenty-three keys at 64 ticks each, and 11 × 64 is where that logo advances; the
same span with nothing held moved at tick 651 against the sweep's 652.

An interleaved control does not fix it: with an equal idle window beside every
key, another archive's 2120-tick drift fell in a press window, crediting `5`.
**Different keys for one drift is the error's signature**: a control sharing the
timeline it controls for is no control.

The sound order is watch first, then press, as the probe does — a screen still for
8192 ticks that changes 4 ticks after `fire` changed because of `fire`. The rung
keeps press-first, because watching first would spend the opening budget on all
257 archives rather than the few that answer no key; a screen drifting within the
320 ticks of its five keys can still be credited to one. None in the corpus is
known to; read a suspicious pass with this in mind.

<a id="implementation-two-things-worth-knowing-about-these-openings"></a>

#### Two things worth knowing about these openings

**The old still shortcut misclassified a slow animation.** A menu cycles four
frames over about 38 ticks, holding each longer than the former eight-tick
shortcut, which froze on one phase and credited later unpressed phases to a key.
Both settling paths now observe the full 64-sample cycle horizon;
[the regression and three-corpus comparison](testing.md#settling-settling-and-key-response-validation)
record the change and the limits of finite observation.

**Ticks are not seconds.** These titles paint once per Host tick, so a screen a
handset holds a couple of seconds is thousands of ticks under either clock. The
title taking 23934 ticks to leave its first logo needs about 100,000 to reach its
title screen, and `runktf -play` on the wall clock also sits on that frame all
run. Tick counts describe this Host's loop, and are the ladder's only measure.
<a id="implementation-a-directory-the-platform-made-and-the-two-questions-that-answered-it-differently"></a>

### A directory the platform made, and the two questions that answered it differently

A title ends itself inside `startApp`, on both of its launches:

```
start KTF main class Clet: invoke KTF AOT Clet.startApp([Ljava/lang/String;)V:
handle supervisor call 0x2 at 0x…: execute KTF AOT native call at 0x…:
handle supervisor call 0x3 at 0x…: MC_knlExit from 0x101511: KTF guest requested exit
```

The exit is deliberate; the ordered trace shows its decision:

```
fs mkdir shared -> exists
cdb stat shared -> missing
kernel exit
```

The title makes its directory, stats the name, is told it does not exist, and
quits; on first launch the mkdir succeeds and the stat still says missing, so the
answers disagree either way.

**A directory here is only its entry in the made-names list** (see "Two titles
stop on WIPI C table 7 function 8" above). `MC_fsIsExist` already read the list;
`MC_fsGetFileStat` did not, so slots 5 and 16 answered one question two ways.
Slot 5 now reads the list.

**The attribute word stays zero, still not a guess.** The stat's caller
classifies `out[0]`:

```
bl      <slot 5>          ; (name, MH_FileInfo* out, mode=1)
ldr     r4, [r4]          ; out[0], the attribute word
movs    r3, #8
ands    r3, r4            ; bit 3 → kind 2
beq     …
movs    r3, #2
ands    r3, r4            ; bit 1 → kind 1, neither → kind 0
strh    r3, [r5, #2]
ldr     r3, [sp, #0x204]  ; out[1], the creation time
ldr     r3, [sp, #0x208]  ; out[2], the size
```

The word is a bitfield with bits 3 and 1 marking two kinds of entry — a third
independent confirmation that `MH_FileInfo` is `{ attrib, creationTime, size }`
— but not evidence of **which** bit is a directory, and the title needs none:
with the word at 0, 2 or 8 the run reaches the same 800 ticks with the same flush
and lit-pixel totals. The refusal ended the title, not the classification.

**What it moved.** The title runs its full three thousand ticks on both launches
instead of `exited` at tick 0 — 2,996 flushes and 71,275 lit pixels — and reaches
its record stores: `record`, `recordStore` and `shared` made, four `rms_*.sav`
databases opened. A unit test covers the two answers agreeing.

<a id="implementation-a-batch-hosts-clock-had-no-rate-and-a-guest-that-waited-paid-for-it"></a>

### A batch Host's clock had no rate, and a guest that waited paid for it

The other title of the same pass refuses its ninth tick:

```
service KTF timer at 0x10d2a1:
KTF Host service call exceeded its step allowance of 500000000 steps
```

Five hundred million guest instructions in eight ticks, in one timer callback:

```
guest profile: 500005 samples over 500005503 instructions (1 sample / 1000)
   99.95%    499773  0x10a522-0x10a544
```

That range is the guest's own `delay(ms)`: read `MC_knlCurrentTime`, compare the
64-bit difference with the argument, spin `r3 += 25` to 999,999 (some 160,000
instructions), read again. The caller passes `0xfa << 1`, a 500-millisecond wait
holding the opening logo; the trace's last two thousand events are all
`wipic 0x1c`. **This is a wait, not work** (the distinction
[`cli.md`](../cli.md) refers here for).

**The refusal was this platform's, and "the clock must have moved" was half a
rule.** The fifteenth round stopped charging a call for windows spent reading a
clock that moved, withholding free renewal on a stepping Host whose clock stands
still during a service call, so a hang fails in seconds. But the clock standing
still was never a decision: such a Host jumps its clock between ticks and has no
way to move it within one, so the guest waits for an instant that cannot arrive,
however patient the ceiling.

**A Host-owned clock now has a rate.** During a service call it advances with
guest execution at `batchGuestStepsPerMillisecond` — a hundred thousand steps a
millisecond, a handset ARM of the era at about one instruction a cycle. The
title's pad loop between clock reads is then a pause of a millisecond or two,
sensible only near this rate. The figure is not load-bearing within a factor of
ten: it must be slow enough that waiting still costs execution, so the wait
allowance bounds an unsatisfiable guest, and fast enough that a real delay fits
the allowance — half a second is fifty million steps against five hundred
million.

Deliberately not:

- **the wall clock's business** — real time is not the Host's to advance and
  never stands still under a call; the page and `-play` are unchanged;
- **between calls** — a batch Host jumps its clock to the next deadline itself,
  and a clock also creeping under the guest would arrive somewhere unplanned;
- **a free renewal** — a guest executing nothing between clock reads buys no
  time, so a never-ending call still ends on the step allowance in the same
  seconds, as the existing unit test covers unchanged.

**What it moved.** The title reaches its first lit frame on a stepped run (24
flushes where there were 9), and its delay loop terminates in the trace. Under a
wall clock it still answers a key: `fire` leaves the logo, passes a menu and
reaches a character screen.

<a id="implementation-three-members-a-sweep-that-held-keys-down-reached"></a>

### Three members a sweep that held keys down reached

A sweep running every local KTF archive 3,000 ticks at four times speed, pressing
a key every twelve ticks (fire, the four directions, 2, 5 and 8), ended three
titles on a platform lookup:

```
KTF AOT method notifyKeyInput(II)Z was not found from class org/kwis/msp/lcdui/InputMethodHandler
KTF AOT method <init>(C)V was not found from class java/lang/Character
```

two management titles on the first when a key reached their name field, and a
shooter's guest thread on the second well into play. With both answered, one
management title passed its name field and stopped at tick 1,654 on a third:

```
KTF AOT method <init>(Ljava/lang/StringBuffer;)V was not found from class java/lang/String
```

**Two are one omission, not the core library's.** The JVM has all of `Character`
and `String(StringBuffer)`, but a KTF title reaches only what the platform's
class table publishes: `Character` with `isDigit` alone (the one member seen when
the class was connected) and every `String` constructor but this. `Character` is
now published whole and the constructor added — declarations only, each held to
a body by `TestDelegatingRuntimeMethodsHaveAJVMBody`.

Counting the whole table (core-library members less the table's, per class) finds
47 more CLDC members with a body and no declaration, which
[`jvm.md`](../jvm.md) records for an unrun title that would still stop on one
here. Searching every local image's name pool for their signatures: `Thread`'s
`join`, `getPriority` and `activeCount` sit in some 85 images beside private
names of the original runtime's `Thread` (`interrupt0`, `setPriority0`) that no
title calls — the platform class's layout, not calls. `StringBuffer.deleteCharAt`
appears in two images and no run reached it; it is published with
`StringBuffer.getChars`, the buffer's other missing member. The other 45 — radix
forms, boxed equalities, `Math`'s floating-point functions, and a few stream,
thread and calendar members — have signatures absent from local images or too
common to mean anything (`equals`, `hashCode`, `close`), and the sweep reaches
none: a separate follow-up (see "Publishing the rest of the core library").

**The third is a contract settled by its callers.** A title drawing its own text
field builds an `InputMethodHandler`, registers its own listener, and passes the
handler its card's keys — in one title entirely
`if (type == 1) handler.notifyKeyInput(key, 1)`, return ignored. The
specification has the handler compose characters by mode and call the listener's
`notifyTextChanged(char[] chText, int len, int pMode)` with Insert (-1), Replace
(0) or Delete (1), without saying what a listener does. Three local titles
implement the listener identically:

- Insert appends `new String(chText)` — the whole array, with `len` unread;
- Delete cuts `len` characters off the end;
- Replace does nothing.

So the handler only inserts and deletes, and an insertion's array holds exactly
the inserted characters.

**The text is the Host's, as for every other field.** A handset composed letters
and Hangul in this automaton; here the browser or OS composes them and the page's
text input hands over completed text, one character per key the title forwards:
the Host presses a carrier key — `0`, the key the C input method's Host text
travels on — the title passes it to `notifyKeyInput`, and the handler answers
with an insertion of the next character instead of the key's meaning. The title
handles each key as if typed on a handset, keeping its own limit: the first title
shows its "no more than four characters" message on a fifth, the second keeps
the first six. Insertion is the only edit the Host sends, since the field cannot
be read and the page only appends. The keypad keeps what the C input method
keeps: a digit in digit mode, and clear, which the first title forwards only
while its field has text, sent as a deletion. Star, hash, directions, fire and
soft keys return unprocessed; the first title switches modes on its left soft key
and labels `#` with its own command.

**The titles number the modes, differently from the C input method.** One title
draws its own indicator (가, A, a, 1), opens the field under 가 with
`setCurrentMode(3)`, and its left soft key steps through A, a and 1 while setting
1, 0 and 2: 0 small letters, 1 capitals, 2 digits, 3 Hangul, where the C table
starts with capitals and puts Hangul third. Only digit mode changes what a key
does; Host text is taken in any mode, as in the C input method.

**The text input must be available whenever the field is, and the keys say
when.** The first title builds a handler and registers its listener when the
field opens, passes it every pad key (soft keys and call included) while open,
and switches mode with a soft key it does not forward; fire confirms and closes
the field and clear on an empty field cancels it, neither reaching the handler.
So a field opens when a listener is registered on a title-built handler, stays
open while the card's presses reach it (a mode switch counts), and closes on the
first press that does not. A text component's own handler opens nothing. The
exception: when a fifth character overflows the first title's four, the title
rebuilds the field while handling that key and shows its message over it, and
the fire dismissing the message never reaches the field behind. A field opened
while another is taking a key is that field reset, and stays open through unseen
presses until it takes a key itself; a field opened otherwise and confirmed at
once still closes. The open field and its card are runtime objects, so a quick
save with the field open loads open before any key. A title running its own
event loop is excluded, as in the C input method, since when it consumes a
carrier is its own business.

The first version typed letters with the multi-tap keypad and excluded the Host,
believing a title-drawn field gives no sign of being open; the keys are the
sign, and letters are the Host's here as everywhere.

**What it moved.** All three titles run the key script's 3,000 ticks. Routed to
the first title's field on a manual clock, the text input is offered when the
field opens; `홍길동` appears in the field and becomes the candidate's name on
fire, after which nothing is offered. Six characters bring up the title's message
after the fifth and refuse the sixth; after dismissal the rebuilt field takes
`김철수`. The second title's store name takes `닭집`, keeping eight characters to
six. A quick save with the first title's field open loads with the text input
offered before any key, and `이순신` reaches the field.

<a id="implementation-publishing-the-rest-of-the-core-library"></a>

### Publishing the rest of the core library

The 45 members the sweep left are published now: the radix forms of `Byte`,
`Short`, `Integer` and `Long`, their value equalities and hashes, the widening
`floatValue` and `doubleValue`, `Math`'s floating-point half, `String.intern`,
`Class.toString`, the floating-point reads and writes of the data streams,
`InputStream`'s constructor, `PrintStream`'s character-array forms and `close`,
`Calendar.setTimeZone`, `Random.next`, and `Thread.activeCount`, `getPriority`
and `join`. `TestEveryPublishedCoreClassDeclaresItsCLDCMembers` holds the table
to it from here on: every public or protected member the core library has for a
class the table publishes is declared, except five the library keeps for itself
and CLDC does not name (`PrintStream(int)` and its two `byte[]` prints,
`Byte.toString(byte)` and the library's own enumeration).

**Most are declarations only; three were not.** The JVM's `Thread.join` refuses
to block once a platform drives the threads (blocking holds the one guest core),
so publishing that body would turn a missing method into a refused one. Here
`join` parks the calling worker a slice at a time until the target returns from
`run`, like an untimed `wait`; on the client thread (inside a paint, a key or a
lifecycle call) it cannot park, since the target cannot run until the call
returns, so it returns at once. `setPriority` kept nothing, which would leave a
published `getPriority` answering the default; it now stores an in-range value
in the field `getPriority` reads, and still ignores out-of-range values without
throwing. The console's character-array prints log like the string forms, and
`close` releases nothing.

**Adding members to a class titles extend does not move their methods.** A
current-generation class record carries slot numbers compiled against the
original runtime — one canvas overrides at 24, 25 and 27 and starts its own at 33
— and the client renumbers them when preparing the class, from the end of this
runtime's published table: the same canvas runs with them at 30, 31, 32 and 33
under a 30-slot `Card`. A 600-tick probe of the 282 local archives registered
2,208 guest classes, and every one with a platform parent numbers its methods
from the end of that parent's table (gaps and the one in-parent-slot override
included). The three relocatable modules lay their own numbers over the parent
table, and none extends a class that gained a member. Over the whole set
`run -ticks 500` gives the same digest both sides; two titles differed once with
eight running concurrently and agree alone, as they always varied. The 44 titles
extending `Thread`, `InputStream` or `Random` (41, 2 and 1) ran the key script
1,500 ticks: 42 alike, and the other two finish alike on three repeats each.

<a id="implementation-a-text-components-protected-fields"></a>

### A text component's protected fields

One title stopped on a field rather than a method:

```
KTF AOT field m_cPos:I was not found from class org/kwis/msp/lwc/TextComponent
```

in its own `TextFieldComponent` subclass's constructor, when its main menu built
the name entry; the constructor calls the platform constructor then sets
`m_cPos` to the initial text's length. The specification declares
`protected int m_cPos` ("현재 문자 데이타의 위치") on `TextComponent` beside its
other state: the characters `m_td`, their count, the input mode `iMode`, the
limit, the constraint, the font, the display, the handler, a mode-display card
and a checker.

**Seven local titles reach for them, all from their own field built on the
platform's.** Two set `m_cPos` to the text's end after building the field; one
moves it to the end of `m_td` before passing the platform a key; one draws its own
caret from `m_td` and `m_cPos` and clamps a moved caret to `m_td.length`, so `m_td`
must be exactly the text's length; one presses right until `m_cPos` reaches the
end — never ending if the word never moves. Two compare `iMode` with the Hangul
mode, 3, before drawing, and one writes its own number there and passes it to the
handler. One reads the font in its constructor, and one reads `modeViewer`.

**Every field this runtime holds a value for is published and kept current.**
Beside `imHandler` the payload now carries `m_cPos`, `m_td`, `charCount`,
`iMode`, `maxLength`, `constraint`, `f` and `display`, and `TextComponent` gains
the members the specification declares on it but only the box had: `insert`,
`delete`, `setFont`, `getFont`, `getConstraint` and `getMaxLength`. Characters,
count, limit (-1 for none, per the specification), constraint, font and display
follow every changing call, and Host text and the keypad's focused component,
which edit outside calls, publish as they edit. **Two words are also the
title's.** With no caret kept (Host text and the keypad append), `m_cPos` is
written only when an edit moves it: to the end after a new value or append,
after an insert's run, to a delete's index. `iMode` is written at construction
and when the handler is given a mode; otherwise the title's own value stands.

Not published: `constChecker`, whose type the specification does not publish,
and `modeViewer`, the card showing the input mode, which the one reading title
pops off the display; this runtime never shows it, so handing over a card would
pop the title's own (see "Deliberately incomplete").

**A payload is written only as far as its class was registered.** A component
allocated under an earlier, smaller layout (restored from an earlier build's
checkpoint) keeps every word past its class record's size, so new fields are not
written over a neighbour; `TestAComponentUnderAnEarlierLayoutKeepsTheRestOfItsPayload`
holds that.

**What it moved.** The title runs all 3,000 ticks of the key script instead of
stopping at tick 883, through its name entry to its menus.

<a id="implementation-a-key-is-a-callback"></a>

### A key is a callback

Two titles ended whole sessions on their own exceptions in `keyNotify`, on some
sweep runs. Neither involves a wrong runtime value: one collects keys as
characters in a numeric-entry state and parses them with `Integer.parseInt`, and
a fire key (shifted into its own range) is not a digit; the other passes a key to
its text box's platform `keyNotify` then reads `getString().charAt(4)` unchecked,
throwing on a name under five characters. A handset ends the call and continues,
as the thirteenth round made this runtime do for a paint, a timer task, a thread
and a guest event loop — but Host key and pointer delivery had been left out.
Both now end only the call: the throwing key goes no further down the card
stack, and the next is delivered.

**What it moved.** Four runs of each title through the key script all reach 3,000
ticks; in two runs the parsing title absorbed 224 and 226 such keys (it spends
most of the run in numeric entry). `TestAnUncaughtExceptionInKeyNotifyEndsOnlyThatKey`
fails before the change.

<a id="implementation-a-packaged-save-that-holds-the-address-of-the-phone-that-wrote-it"></a>

### A packaged save that holds the address of the phone that wrote it

One WIPI-C title stopped on its first load, 167 ticks in:

```
service KTF timer: ... write KTF database read buffer: write guest memory at 0x1d14250 (920 bytes): guest memory is not mapped
```

**The address is in the save, which shipped with the archive.** The title saves a
192-byte structure and then the 920-byte array it points to, and loads them the
same way: the structure over its live copy, then the array into the pointer just
loaded. The array is static (at `0x166660` in the image's BSS here), so the saved
pointer is the writing handset's image load address plus the array offset. The
archive packages that handset's `save.sav`, whose word at offset 532 is
`0x01d14250` — an image loaded near `0x01cadbf0`. On any device loading the image
elsewhere, this one included, the read targets unowned memory; `MC_fsRead`
answered exactly what was asked.

**Everything the title writes here reads back here.** Choosing NEW GAME rather
than the offered load runs the opening and 2,500 ticks of play; a save from the
in-game system menu writes `0x166660` at offset 532, and a new process loading it
reads the array there and plays on. Only the packaged save cannot load, on any
other handset too, so it is recorded and kept off the list of walls like other
archive-borne causes (see "What a sweep of a 262-archive set asked for").

<a id="implementation-a-name-the-title-asks-for"></a>

### A name the title asks for

A player reported that one title used to let them name the character in a new
save slot, but here every slot was named for them — a space, the Korean word for
"local" and the slot number. The name came from this runtime: the local slot
service standing in for the carrier's slot server (see
[the 2026-09-19 startup investigation](#startup-compatibility-2026-09-19))
put it in the creation receipt, command 1430.

**The receipt's status byte zero means the name screen.** The title's reply
parser is one switch over command numbers 1400 to 1440; case 1430 reads a status
byte first. Nonzero, it reads a length-prefixed name and an eight-byte field,
stores both and advances; zero, it opens its own name screen. The service now
answers zero (an empty slot), and the remembered screen appears: a field of at
most five characters, "no special characters", and "a created name cannot be
changed".

**The name returns in one more request.** Accepting a name, the title sends
command 1440 with it as length-prefixed EUC-KR bytes. Case 1440 reads only an
eight-byte value, stored where the receipt's eight-byte field goes, and advances
to the named-receipt state. A one-byte or empty reply overran that read and left
the title on TRANSMITTING; eight zero bytes (the receipt field's value here) lead
into the opening, whose dialogue uses the name, and a restart lists the slot
under it with class and level. The title stores its own copy; the service keeps
the name only to answer a repeated receipt.

**The title's own name rule is not a fault.** It accepts digits, `A`–`Z`, space
and complete Hangul syllables, rejecting everything else — including lowercase,
which the keypad's letter mode types first; keypad names must be capitals or
digits, as the keypad has no Hangul.

**The Host could not find its field.** It is a `TextFieldComponent` subclass
built at start-up and told it has focus by `focusNotify(true)`, never via
`setFocus` or a container, so neither explicit focus nor a shown shell pointed at
it; the card draws the box and reads `getString` every paint on the name screen.
The Host now offers a focused field the top card read during its last paint
([`native-text-input.md`](../native-text-input.md#supported-editors) has the rule
and limits).

**A name the page could send broke its slot.** The page took any text, and Go's
EUC-KR encoder accepts any Hangul syllable, including the 8,822 KSC5601 lacks.
The title drew such a name with the platform font and saved it; on the next start
its slot screen drew it with its own font, raising
`ArrayIndexOutOfBoundsException` on the first byte outside KSC5601, and the slot
showed blank. Host text is now limited to KSC5601 on every platform (see
[the handset's encoding](../native-text-input.md#the-handsets-encoding)).

**Earlier builds' slots keep their names**, which the title saved itself; nothing
rewrites a save. A quick save from an earlier build's slot conversation restores
at the receipt and keeps its label; one from this build may hold a seventh phase,
the registered name, which an earlier build refuses like any unknown state.

**The terminal could not reach the screen.** `runktf -play` stood on the
connecting screen this flow passes, while the page always got through. The
title's network thread waits in a read yielding every round, so a round cost
microseconds and a route's tick-counted wait ended before the title's next frame
was due; `-play` now enters the session as the server does
([`cli.md`](../cli.md#runktf)).

`TestSlotRelayConversationAndRegisteredName`,
`TestSlotRelayKeepsAnEarlierBuildsDisplayName`,
`TestSlotRelayRejectsMalformedOrOutOfOrderRequests` (registration out of order,
lengths, bytes the guest font cannot draw) and
`TestRelayHeapCarriesTheRegisteredNamePhase` cover the service;
`TestTextInputOffersAFocusedFieldTheCardDraws` and
`TestTextInputDoesNotGuessBetweenDrawnFields` the field;
`TestEncodeKSC5601TakesOnlyWhatTheHandsetEncodingHolds` and
`TestTextInputRefusesTextThePlatformEncodingLacks` the encoding; and
`TestAKTFTickOnTheWallClockIsTheServersEntry` the terminal. On the archive via the
page's session path: a name outside KSC5601 is refused, a Korean name commits,
the confirm key registers it (service phase 6, no refusal), the opening shows it,
and a new process lists the slot under it.

<a id="implementation-deliberately-incomplete"></a>

### Deliberately incomplete

- **The "directory" bit of a file's attribute word.** `MC_fsGetFileStat` answers
  `{0, 0, 0}` for a name `MC_fsMkDir` made, which is all the ending title needed
  (see "A directory the platform made"). The real bit is a HAL number the
  specification does not print, and the one local title reading the word runs
  identically with either candidate, so nothing here decides it.
- **Showing what the last flush put on the panel instead of reading the
  framebuffer when a Host asks.** LGT made that change and three titles came back
  ([`lgt.md`](lgt.md#implementation-the-lgt-platform), "What a Host shows is what
  the last flush put on the panel"); the same hole exists here in principle (a
  title flushing a picture then blackening the framebuffer before collection
  would show black), but no title does it: both local candidates were something
  else (a card never told it was shown; an unreproducible case) and `framestats`
  over the whole set finds no third. The cost also differs: LGT's flush publishes
  a surface, while here a flush would read the whole screen from guest memory,
  hundreds of times a second (see `presentScreen` for the deferral and its
  measurement). It waits for a case, which the sweep's tool would find.
- Repairing a guest write to a published instance field instead of reporting it
  (see "A published instance field has two storages"): the counter is zero across
  every local archive, so there is no case to decide which side wins.
- **`destroyApp`.** `pauseApp` and `resumeApp` are driven (see "The park the
  guest is told about"), but at close `StopThreads` has begun unwinding the
  workers, and guest code must not run on top of that stack. (Superseded by
  [Unconditional Jlet teardown](#followup-unconditional-jlet-teardown).)
- The WIPI Java entries in `testdata/wipi_java_gaps.txt`, each with its reason,
  and the fixed-value answers inventoried in `testdata/wipi_java_stubs.txt`.
- Animated image playback: see `testdata/wipi_java_gaps.txt`.
- The graphics context's transparent-pixel, alpha and origin-offset fields are
  stored and returned but read by no drawing call; the clip and pixel operation
  are the fields a draw honours. Two titles set the transparent pixel (the same
  magenta, once each at startup), a third sets alpha through an even fade, and
  the map title sets the origin offset nearly every frame — all reached, and none
  caused the map's boxed objects (see "An image is drawn through its own handle")
  or the dropped sprite backdrop (see "The transparency a title brings with it").
- In-game progression is verified by the user playing; probes only surface
  missing API surfaces.
- **Host text into a title's own field when the title runs its own event loop**,
  excluded as in the C input method: a carrier is consumed on the title's time,
  so whether it reached the field is unknown (see "Three members a sweep that
  held keys down reached").
- **`TextComponent.modeViewer`**, the card the platform shows the input mode on
  while a text component edits. This runtime never shows it, and the one local
  title reading the field pops that card off the display through it, so an
  unshown card handed over would take the title's own card (see "A text
  component's protected fields"). Publishing waits for the platform to show it.
- **Painting LWC components.** A title expecting the platform to draw a shell and
  its text box shows an empty box; the text is present and reads back. Nothing
  records how the original runtime's components looked — see
  [`native-text-input.md`](../native-text-input.md#what-a-title-sees-after-a-commit)
  for the measurement and what would reopen it.


<a id="followup-ktf-compatibility-follow-up"></a>

## Compatibility follow-up (2026-09-12)

This note preserves the evidence needed to resume compatibility investigations
without relying on a local working tree or an archive label.

<a id="followup-native-loading-stall"></a>

### Native loading stall

The September 7 acceptance record names `var/games/KTF 1.1-BREW` as the old
corpus and records these SHA-256 values:

- `56d6865251cc6e3bb2e07065d5d460591478b8c7175e2798ce597d2534794cc3`
- `1d5831e42a8a41d4c875b81e2606e779c5559b051f536686a0bd08e20ac9e71e`
- `98db83c3bda6cdff6b1096374e8a2197fdf18445397bba7cdab24f2c0fb8d0db`

Files with all three hashes remain there, but they are ordinary WIPI packages
with an `__adf__` descriptor and a Java archive; none contains the `.mif` and
`.mod` pair the BREW loader requires. A recursive ZIP-entry scan of all 543 files
under `var/games` found no archive with that pair, and the canonical
native-package acceptance probe skips for the same reason.

The recorded stall after eighteen resources therefore could not be reproduced.
The saved evidence says those resources loaded, the target surface stayed empty,
and no later trap or surface write showed where the resource data should have
been copied. Tracing the resource destinations and blit source needs a matching
native archive; the current files do not support a loader change.

<a id="followup-requested-landscape-packages"></a>

### Requested landscape packages

The two current archives requested for a 320 by 240 check are identified by these
SHA-256 values, independent of their local filenames:

| SHA-256 | `DisplaySize` | `ResizeType` | bytes |
| --- | ---: | ---: | ---: |
| `93d5b6b8ceb5cf4ce15784e369a7af9cd8ef7a49d6f15816d665aedadc487a4a` | `176*220` | `0` | 985530 |
| `df5f0261a2f9315a745d181a52eb76ea6a89a0ab4bd4554a78e19cca076f4012` | `240*320` | `0` | 1326663 |

All current copies of either package are byte-identical to those archives. Of the
543 local files, 296 have readable WIPI descriptors: 108 declare `176*220`, 184
`240*320`, one `240*400`, and three omit `DisplaySize`; none declares `320*240`.

The archives thus give no evidence for automatically rotating either title or
overriding its declared handset. The CLI's explicit `-screen 320x240` suits a
diagnostic run; selecting it in the runtime would need a matching archive or a
handset trace demonstrating the landscape contract.

A 400-tick diagnostic confirms the override works. Automatic runs report 240 by
320 for both. The first hash paints 258 flushes and 76630 non-black pixels in
either mode; the second paints 400 flushes, with 51795 non-black pixels
automatically and 51340 forced to 320 by 240. Both forced runs produce a populated
320 by 240 frame without a start failure — inspectable, but not established as
the handset contract.

<a id="followup-unconditional-jlet-teardown"></a>

### Unconditional Jlet teardown

The [WIPI 1.2.1 Jlet lifecycle
contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Jlet.md)
says that a transition to the destroyed state invokes `destroyApp`, and that a
true `unconditional` argument means an exception cannot prevent termination.
Previously, closing a KTF session stopped audio and guest workers without
invoking that callback.

The canonical directory has 35 KTF WIPI archives once one package for a
different platform is excluded by content, and all 35 declare a non-native
`destroyApp(Z)V` override. A guest exit through `Jlet.notifyDestroyed` reaches
the same Host close path.

Session close now invokes `destroyApp(true)` while guest entry is still safe, then
stops audio and unwinds workers. A callback failure is counted in lifecycle
diagnostics but cannot refuse the unconditional teardown. The callback shares the
bounded Host-service instruction and time budgets of other guest callbacks, and
its transition is marked before guest entry, so an exit, exception, exhausted
budget or repeated close cannot enter it twice.

The regression fixture records the boolean argument in the Jlet object; focused
normal and debug tests show the callback receives true and workers still stop.
The full KTF package tests pass in both profiles.

<a id="followup-host-composed-text"></a>

### Host-composed text

Guest focus is recorded in the runtime's `lwc:focus` object by
`Component.setFocus` and `FormComponent.setFocus`. `Session.TextInput` now captures
that object, its current string, and its constraints under the client run lock. A
later whole-text commit reacquires the lock and rejects the edit if the focus
object, string value, constraint, or maximum length changed. Guest subclasses of
an LWC text field or text box follow the same path.

The [text component
specification](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lwc/TextComponent.md)
defines constraints 0 through 5 for unrestricted, numeric, numeric password,
email, URL, and phone input. A text box is multiline; a text field is
single-line. The maximum length counts Java UTF-16 code units. The
[input-method listener
specification](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodListener.md)
defines per-key insert, replace, and delete operations. It has no cursor or
range argument with which to describe an arbitrary whole-field replacement. A
vendor text field instead reports the completed change through
`textChanged(GTextField)`.

The adapter validates the Host transport, applies the active constraint, rejects
line breaks in a text field, and enforces the maximum without truncating a
composition. It stores the completed Java string as `setString` would and resets
the legacy keypad editor. A field with an explicitly installed
`InputMethodListener` is not offered to the Host, because sending the completed
value as a guessed replacement could corrupt that listener's active composition.
Installing a listener after the Host's snapshot invalidates the pending commit,
and a handler-owned mutation revision also invalidates it when the guest installs
a listener and restores the original null before commit.

One observed vendor editor is non-modal: it shows a GForm containing one listened
GTextField without an explicit focus call. The adapter supports only that bounded
shape, records form, child, listener and text revisions, and applies a Host
composition to the same field without hiding it. The field EventListener receives
FIRE, copies `getString` into guest state and hides the form. Key ownership stops
the release that opened the form from dismissing it and keeps the acceptance
release away from the card underneath.

The vendor `doModal` path keeps a separate lifecycle gap: it returns
synchronously, leaving no live editor for a later Host commit, so that modal
lifetime remains unsupported.


<a id="text-ktf-java-text-spacing"></a>

## Java text spacing (2026-09-15)

The guest draws a white sentence and then a colored string at the same origin
and anchor. The colored pass replaces each uncolored Korean syllable with two
ASCII spaces and each uncolored ASCII character with one space. It does not
calculate a separate emphasis coordinate using `Font.stringWidth`.

A temporary trace captured a white prefix containing six Korean syllables and
two spaces, followed by the emphasized word. The colored pass starts with
fourteen spaces. Both calls use `(11, 274)` and anchor `0`. The handset face
advances Korean syllables by ten pixels but its font asset advances spaces by
four. The white prefix occupied 68 pixels and the colored prefix 56, placing
the emphasis twelve pixels too far left.

An initial five-pixel space correction removed that displacement. Follow-up
screenshots showed a smaller fringe after punctuation: the font asset advances
period and exclamation mark by four pixels, while their replacement space now
advanced by five; the traced second dialogue line has a period before another
emphasized word, explaining its one-pixel rightward shift. Narrow Latin letters
and proportional digits share the mismatch with a one-space replacement.

KTF WIPI Java drawing and font measurement now use five-pixel cells for
printable ASCII (`U+0020..U+007E`). Korean syllables retain their ten-pixel
advance. Glyph shapes, control-character advances, other Unicode advances,
and the shared font asset are unchanged. Native WIPI-C text and other
platforms retain their existing metrics.

The [WIPI Font contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Font.md)
requires width measurement to describe rendered text but prescribes no pixel
widths; the cell spacing comes from the observed guest overlay algorithm.
Character, string and range measurements, anchoring and drawing share one Java
advance function.

`TestJavaTextOverlayUsesHalfWidthSpaces` and `TestJavaTextOverlayAfterASCII`
compare overlaid text with independently positioned colored glyphs. The ASCII
regression failed before the follow-up correction for punctuation, digits and
Latin letters and passes afterward. `TestJavaASCIICellMetrics` checks the public
character-width handler for every printable ASCII character and preserves
representative control and non-ASCII advances.

The replay `var/logs/ktf-overlay.route` (`-hold 5 -ticks 10000`) reaches the
instruction dialogue in the first follow-up screenshot. The final capture is
`var/logs/ktf-render-revised/quiz8.png`; the original proportional rendering and
the intermediate space-only correction are retained in
`ktf-overlay-full-before/quiz8.png` and `ktf-overlay-full-after/quiz8.png`.
The other two follow-up scenes were inspected in supplied screenshots, but
were not navigated separately; their punctuation case is covered by the
synthetic pixel regression. The final and intermediate frames differ at 358
pixels within `x=105..182, y=288..309`; the surrounding scene is identical.


<a id="text-validation"></a>

### Validation

Pixel regressions and local CLI dialogue replay passed. The isolated KTF branch passed `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...`.
The two later scenes were not replayed individually, and no new Safari check
was performed. Original assets and saves remain outside Git.

<a id="startup-compatibility-2026-09-19"></a>

## Startup compatibility investigation, 2026-09-19

Three local archives were investigated through the shared session path used by
the CLI and browser server. Archive digests identify evidence, never select
runtime behavior. All probes used isolated saves below the ignored
`var/savedata/qa-20260919/`; original user saves were not edited. Raw traces and
screenshots are under `var/acceptance/qa-20260919/` and are not distribution assets.

<a id="startup-compatibility-2026-09-19-ktf-startup-exception-1379b56de66e"></a>

### KTF startup exception (`1379b56de66e`)

The first failure was a compiled protected-field access check. It compared the
receiver's decoded class address against the canonical class record in the
image. A dispatch alias referred to the same class but failed that raw address
comparison, producing a guest `java/lang/Error` during construction.

For ordinary images the JVM context and dispatch aliases now occupy a bounded
4 MiB region immediately after the mapped image and BSS. Object headers name
canonical image records when their signed 27-bit displacement is representable.
Distant runtime records still use aliases. Earlier native modules retain their
existing context layout. Large images can still require aliases for records
outside the displacement range; this is not evidence of universal raw-address
identity support for every possible image layout.

The next startup failures exposed three independent runtime omissions:

- Fieldless runtime subclasses did not retain their parent's instance size.
  Guest subclass fields could overlap `TextComponent.imHandler`.
- The constructor stored that handler under an unqualified field key, while
  the guest-field publisher read its canonical declaring-class key.
- `Kernel.getExecNames` and `InputMethodHandler.getCurrentMode` were missing.
  The former lists the current session application with descriptor filters;
  the latter returns the input mode already maintained by the handler.

The API contracts were checked against the WIPI specification for
[Kernel](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msf/core/Kernel.md)
and [InputMethodHandler](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodHandler.md).
Both debug and release 1,200-tick runs reached the title menu with 1,200
frame flushes.
This proves startup and menu input, not campaign completion or progress restore.

<a id="startup-compatibility-2026-09-19-lgt-embedded-certificate-74b0ff2230f7"></a>

### LGT embedded certificate (`74b0ff2230f7`)

The notice requests a certificate on the first connection. It does not describe
a twelve-hour renewal interval. Ordinary offline transport correctly delivers a
connection failure, but the archive requires a separate application certificate
to pass its startup gate. Its identity/options file reads already succeeded.

The main save contains a 100-byte encrypted options header, a 100-byte
certificate at offset 100, and progress records from offset 200. The header's
first 48 plaintext bytes carry a byte-sum checksum at offset 48; byte 27 is the
certificate-presence flag. The certificate reader compares the decoded string
against the session subscriber number followed by the application identifier
and an archive-owned token. The writer and reader share those pointers and the
cipher seed. The cipher uses ciphertext feedback and two module-owned factors.

`lgt-certificate-100` recognizes connected reader, writer, startup gate,
header reader, subscriber accessor and encoder/decoder instruction contracts.
It resolves filenames, tokens, state addresses and cipher parameters from those
contracts. It rejects ambiguous matches, invalid paths, mismatched application
identity, invalid header checksums, inconsistent live state and open target
files. No game name, archive digest or absolute guest instruction address
selects this adapter. No guest instruction is patched and no server is contacted.

After Clet initialization has created or loaded the normal save, the session
exposes the certificate and presence flag privately. The matched live presence
flag stays coherent if the guest initializes its settings again after the first
notice. Publishing the flag requires that the rest of the live options match
the header being saved. Writes restore the original certificate and flag,
recompute the header checksum, and persist ordinary settings and progress.
Malformed nonempty target writes are refused rather than persisting the private
certificate in an invalid record. File deletion ledgers keep ordinary behavior.
Disabling compatibility exposes the backing state again.

Release reached save-slot selection from a fresh save in 2,200 ticks; debug
reached the opening story on a subsequent 2,200-tick session. A copy of the
release save run with `-no-auth` returned to the original certificate prompt,
confirming that compatibility did not persist a generated certificate.

A separately modified diagnostic save first proved the certificate format by
reaching the menu. It was never installed over a user's save. Automated tests
use invented application tokens and newly assembled instruction fixtures.

<a id="startup-compatibility-2026-09-19-lgt-restart-notice-e934256c238a"></a>

### LGT restart notice (`e934256c238a`)

This archive deliberately exits once before proceeding. Its initial `option`
record is 18 bytes, with a restart flag in byte 17. The notice asks for the OZ
confirmation key, represented by `fire`/OK, rather than the numeric zero key.
After its input delay, the confirmation handler clears the flag, writes and
closes the options file, then requests guest exit. Restarting the emulator
before that handler runs leaves the flag set and repeats the notice.

A fresh isolated run confirmed at ticks 50 and 150 (five-tick holds), exited at
tick 152 and persisted the cleared flag. A new session with the same saves
passed the notice, and confirming the following startup notices reached the
title. Debug and release both reproduced the exit at tick 152 and reached the
title on the next run. The case needs no emulator change or save deletion: the
game exits through its own confirmation action and is then launched again.

<a id="startup-compatibility-2026-09-19-validation-boundary"></a>

### Validation boundary

Regression tests cover canonical class identity, inherited instance payload,
handler publication, executable filtering, input mode readback, class arena
bounds, relocated certificate recognition, changed instructions, ambiguous
contracts, cipher feedback, private certificate persistence, malformed writes
and first-run reinitialization. Real archive checks exercise native CLI sessions;
a fresh browser or physical-device playthrough is outside this evidence.

`make test`, `make test-debug`, `go test -race ./internal/...` and
`go vet ./...` passed, and `make debug`, `make release`, `make server` and
`make server-release` succeeded. Additional truncated-contract tests passed in
the focused LGT certificate suite after those full checks.

<a id="startup-compatibility-2026-09-19-follow-up-entering-a-campaign"></a>

### Follow-up: entering a campaign

Evidence is under `var/acceptance/qa-20260919-followup/`. The original user's
saves remain untouched; KTF uses a fresh isolated save directory and LGT reuses
the isolated restart-check save described above.

For `1379b56de66e`, Continue failed while painting because the compiled caller
could not resolve `java/lang/Character.isDigit(C)Z`. The JVM already implements
that CLDC operation; publishing its KTF runtime class metadata connects the
compiled caller to the existing implementation. A regression test checks both
AOT resolution and digit boundaries. Continue now loads the packaged level-five
slot and draws its field scene. This does not prove campaign completion or a
save/restore cycle.

New character creation is a separate unresolved path: declining its connection
prompt returns to character customization; accepting displays the guest's
server-disconnected message and returns to the title menu. The captured run
contains `Network.disconnect`, but no `Network.connect` or `URL.find` call.
Therefore the message alone does not establish a host transport refusal or a
working remote service. Do not describe this path as fixed or substitute a
successful connection result without establishing the guest's required state
and response contract.
Superseded: see the follow-ups below and [network](../network.md#the-surface).

For `e934256c238a`, New Game stopped at the unimplemented LGT graphics slot
`0xed`. The caller passes a framebuffer, crop coordinates, dimensions and a
length pointer, then supplies the result to `CreateImage`. This identifies
`MC_grpEncodeImage`, consistent with the
[WIPI graphics contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md).
The implementation synchronizes guest-written RGB565 pixels, clips the region,
encodes BMP with the existing shared encoder, and returns a caller-owned buffer
from the normal guest heap. Tests cover round-trip pixels, cropping, overflow
boundaries, invalid pointers and release through the ordinary free operation.
New Game now passes the opening story, reaches the controls tutorial, and
accepts character movement in the first playable scene.

After these changes, `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` passed. Debug and release CLI
and server builds succeeded. The interactive checks above used the debug CLI;
a fresh browser playthrough remains outside this evidence.

<a id="startup-compatibility-2026-09-19-new-character-dependency-and-transport-investigation-2026-09-20"></a>

#### New-character dependency and transport investigation, 2026-09-20

The first failure is now localized. The archive declares
`REQLIB=01039AD6|01.00.08` and calls `Kernel.getExecNames` with the required
identifier as its name filter. The current application has a different AID.
The lookup returns no installed match; the guest caches this as a false flag.
Its connection routine reads that flag at `0x1c6e6a` and enters its own error
path before calling `Network.connect`. The displayed server-disconnected
message therefore hides a missing dependency check. A scan of 608 local ZIPs
under `var/games` and `var/ext` found the reference only in this archive's
descriptor and no archive providing the required application.

A diagnostic-only experiment changed that flag in an isolated session. It
then reached the emulator's ordinary `Network.connect` refusal. A second
experiment supplied connection success, which reached
`URL.find("socket://wipiwicgsfg.magicn.com:17096")`. DNS resolution of that
hostname failed on this machine on 2026-09-20. This observation does not prove
when or why the remote service stopped resolving.

A third experiment supplied an in-memory socket with an empty input stream
and a capture-only output stream. The guest requested both streams, then raised
a caught array-bounds exception before sending bytes. The initial attribution
to incoming data was incorrect: the follow-up below locates the exception in
the outgoing identity header. Empty input alone did not establish peer order.
Neither an installed-name alias nor blanket connection success establishes a
playable offline new-character path. The compatible service response and
subsequent character-creation protocol remain unimplemented.

All three experiments were confined to a temporary test and isolated saves.
Their injected success values, guest-memory edits, and socket stand-ins are
not present in product code. Evidence, screenshots, memory dumps and the local
probe source are under `var/acceptance/qa-network-20260920/`. No game payload,
subscriber data or save was sent to the remote host; the only external check
was DNS resolution.

`Kernel.getExecNames` now records its filters and match count through the
existing debug diagnostic boundary, so a future saved log exposes the missing
lookup instead of only the guest's generic connection message. A `REQLIB`
declaration does not fabricate an installed application. The regression suite
explicitly preserves that distinction. Release diagnostics remain disabled.

For this diagnostic-only follow-up, ordinary, debug and race KTF package tests
and `go vet ./internal/platform/ktf` passed. Both CLI and server debug/release
builds succeeded. An unmodified archive startup with the rebuilt debug CLI
recorded `kernel executable lookup name="01039AD6" version=* vendor=* matches=0`.
The earlier full-repository gates describe the preceding gameplay API fixes;
they were not rerun for this lookup diagnostic addition.

<a id="startup-compatibility-2026-09-19-relay-framing-and-worker-progress-2026-09-20"></a>

#### Relay framing and worker progress, 2026-09-20

Compatibility has three distinct owners. Handset properties and worker grants
belong to the common platform runtime. The recovered relay envelope belongs
to a protocol module in `internal/platform/ktf/relay_protocol.go`. Application
commands and service state require a separate local service contract; recognizing
an envelope is not enough to claim that service is installed or successful.
CLI and browser hosts must use the same runtime path. No title-name condition,
archive digest dispatch, or guest-address patch implements product behavior.

The outgoing header builder reads `WIPISTANDARDVERSION`, trims it, encodes it,
and marks its first byte with `0x80`. The previously empty property caused the
array-bounds exception at `0x1cdeac`. Both Java property entry points now expose
the shared value `1.2.1`. This is the emulator's chosen baseline, not a claim of
complete WIPI conformance or a recovered handset's exact version string. The
WIPI 1.2.1 property documentation consulted does not specify this property.
The regression test fails with the previous empty value and exercises the
caller-visible byte conversion after the change.

With diagnostic dependency and socket stand-ins, the guest emits a 72-byte
frame and later a 71-byte frame. Both have 52 extension bytes; the payloads
have 13 and 12 bytes respectively. Their command semantics remain unresolved.
The codec consumes and reproduces both captured frames exactly. The prefix is:

| Offset | Width | Meaning |
| --- | --- | --- |
| 0 | 1 | Zero marker |
| 1 | 4 | Big-endian total frame length, including the prefix |
| 5 | 2 | Big-endian extension length |
| 7 | variable | Extensions, followed by application payload |

The compiled writer at `0x1cdd00` establishes the prefix, and the reader at
`0x1cf340` consumes its one-, four-, and two-byte fields. Extension and payload
contents stay opaque in the codec. Tests use authored bytes, cover every
truncated prefix/body, adjacent frames, malformed lengths and the one-MiB
emulator resource limit. The codec is not yet connected to a product socket
or local service, and does not enable network success.

The initial capture also exposed a scheduler defect. A UI callback requeues
itself each round. `ServiceThreads(ctx, 1)` counted that serial callback as
the only worker grant, so a started receiver never entered `run()`. The common
scheduler now grants the requested worker slice independently of the client
thread's serial callback. Each queue retains its existing bounds and ordering.
An authored test proves both make progress on successive rounds and that a
completed worker retires; it fails before the change with `serial=1 worker=0`.
The archive then enters both its receiver (`0x1cc204`) and relay input reader.
With immediate EOF it reports disconnection and retires the receiver. This is
an expected diagnostic transport failure, not successful character creation.
A second probe supplies an extension-free envelope containing three authored
big-endian words (`12, 0, 10`). It reaches the application receive queue at
`0x1b5300`; this proves frame delivery, not the meaning or validity of a
character-creation response.

All dependency flags, connection successes, and socket stand-ins remain in the
ignored probe only. Evidence is under `var/acceptance/qa-relay-20260920/`, using
isolated saves under `var/savedata/qa-relay-20260920/`. No service payload was
sent externally. The bounded local service now implements the recovered
handshake, confirmation text, and displayable EUC-KR slot name. It is exposed
only when the archive declares the matching provider contract; unrelated
applications retain the offline refusal. The original save tree is never
modified by acceptance runs.

The first implementation returned a binary digest in the slot-name field. The
guest treated those bytes as a bitmap-font string, so selecting the new slot
raised `ArrayIndexOutOfBoundsException` from its paint callback. The service now
returns a short EUC-KR display name, and its regression test checks that the
field round-trips through the same encoding. A copied save with the corrupted
field was repaired for reproduction only; no user save was changed.
Superseded: since 2026-10-10 the receipt carries no name; its status byte says
the slot is empty, and the title asks the player for one and registers it
(command 1440) — see
[A name the title asks for](#implementation-a-name-the-title-asks-for).

After the property, framing and scheduler changes, `make test` (including
219 browser-client tests), `make test-debug`, `go test -race ./internal/...`,
and `go vet ./...` passed. Debug and release CLI/server builds succeeded.
The temporary probe was retained as text under ignored evidence, outside Go
package discovery. These checks do not establish offline new-character
completion or a browser playthrough.

The later [slot creation and save overlay check](#ktf-save-slots-2026-09-23)
validates sequential creation and restart of all six slots in both profiles,
including the zero-based first-slot request and a save/resource path collision.

<a id="ktf-c-input-bitmap-investigation"></a>

## KTF C input and bitmap investigation (2026-09-19)

<a id="ktf-c-input-bitmap-investigation-observations-2026-09-19"></a>

### Observations (2026-09-19)

A reported KTF archive (SHA-256
`5f6037b8852b74254d5e66f8066c251c3a437bb0526f190233f38864b93fdef1`) showed a
green rectangle around its startup logo and did not expose its name editor to
Host text input. These were two separate runtime limitations, not a
speed-setting fault. The implementation and validation below supersede the
initial findings.

<a id="ktf-c-input-bitmap-investigation-bitmap-evidence"></a>

### Bitmap evidence

The startup BMPs decoded successfully, but their `biClrImportant` fields are
`0x126` and `0x100`, with 256-entry palettes and `bfReserved1 == 1`.
`bitmapTransparentIndex` treated these as out-of-range palette indices and
left the images opaque. Their low bytes select palette entries 38 and 0, both
RGB `#209020`; direct decoding through the original `wipic.DecodeBitmap`
produced 2,463 and 30,536 fully opaque green pixels, which is the visible
backdrop.

The decoder now accepts the `0x100` flag only for an 8-bit palette with the
existing transparency marker, and the low-byte index must still fit the actual
palette. Other high bits remain invalid. Authored fixtures cover the extended
variant, ordinary transparency, and out-of-range indices.

A separate image-load failure: one UI BMP declares 256 used palette entries
but its pixel offset leaves room for only 43. Its important-colour value
`0x113` selects the same green. The original decoder returned
`bmp: unsupported BMP image`. For this marked, uncompressed 8-bit variant with
a 40-byte info header, a copied header now derives the palette count from the
checked pixel offset; the underlying decoder still validates pixel indices and
row lengths. Unmarked files do not receive this repair.

<a id="ktf-c-input-bitmap-investigation-input-evidence"></a>

### Input evidence

The session report records input-method table 4 calls to entries 0, 1, and 2
as stubs. The Host adapter in `runtime_lwc_host_input.go` then supported Java
LWC/KFC objects, not this game-owned WIPI-C editor. Disassembly of the caller
identifies the entries:

- At `0x124b08`, entry 1 is called with mode 3, after obtaining the mode count
  and list: `MC_imSetCurrentMode`.
- At `0x124b10`, entry 2 takes no arguments and its return replaces the stored
  mode: `MC_imGetCurrentMode`.
- At `0x124ffe`, entry 0 receives `0x9d` (the flush character), a type, two
  buffers and two size pointers: `MC_imHandleInput`.

The [WIPI graphics/input-method specification](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
defines these signatures; the caller establishes the table order. All three
entries returned zero, so setting numeric mode failed and reading the current
mode returned zero. They now implement mode state and completed/composing
output buffers.

<a id="ktf-c-input-bitmap-investigation-implemented-input-boundary"></a>

### Implemented input boundary

The Host adapter appends completed EUC-KR text through the guest card's
existing numeric-key callback; the C widget keeps its text, cursor, display,
confirmation and field policy. No game-memory offsets or archive-specific
patches are used, and the Java Shell and KFC adapters retain precedence. The
current contract is in [native text input](../native-text-input.md#supported-editors);
the findings specific to this caller were:

- The observed C input callback uses event type 2 for a pressed key, distinct
  from the Java card event constants. Its first four arguments are in
  registers; the composing buffer and size pointer are on the stack.
- The caller initializes capacities of 6 and 8 bytes and then reads the size
  words as produced byte lengths. Both output strings are terminated and both
  lengths written.
- The keypad mode selects an automaton, not a field constraint: the name editor
  selects mode 3 yet labels the field for Korean text. The Host therefore
  validates controls, encoding and length without treating that mode as a
  digits-only policy, and does not reorder the published mode list.
- The completion buffer holds an automaton result, not the whole field. A Host
  submission (up to 64 characters) is validated before any guest callback,
  then delivered one complete EUC-KR character at a time; each character and
  its terminator must fit the buffer, and a character that cannot fit is
  rejected without splitting its encoding. The guest applies its own field
  limit on each callback; the reported five-character field accepts five
  Korean characters in one submission.
- Numeric keypad input is supported; other handset keypad composition is out
  of scope because the Host IME supplies completed text.
- A flush invalidates old snapshots but does not close the editor; CLR that
  flushes composition retains availability. Key activity, mode changes,
  flushing, card replacement, a newly focused Java editor, shutdown and
  successful submission invalidate snapshots, and a callback that no longer
  routes to the C input method rejects the edit.
- A changed target or interrupted callback stops delivery and invalidates a
  snapshot that already inserted a prefix. Accepted characters remain the
  guest's; this C interface cannot promise whole-field rollback after a
  mid-submission guest failure, and the prefix is never replayed through the
  same edit token.

Queued guest event loops remained unsupported: commits require consumption
before returning. The later [timer-delivery correction](#ktf-graphics-input-qa-2026-09-23)
added cards that defer keys and CLR flushing to their owning C timer.

<a id="ktf-c-input-bitmap-investigation-validation"></a>

### Validation

Authored regressions cover extended transparency, compact palettes,
source-byte preservation, malformed palette offsets and pixel indices,
truncated rows, mode state, buffer lengths and termination, pressed-key
handling, Korean Host input, single-character capacity rejection and retry,
unsupported characters, stale targets, cancellation and stopping a
composition when the guest changes mode. Existing Java editor tests remain in
the affected package gate.

The initial isolated-save replay proved only two-character insertion. User
feedback exposed two missed requirements (the field accepts five characters,
and CLR deletion must not disable input), which then gained authored
regressions and a fresh real-archive replay. That replay submitted five
Korean characters at once, deleted one with CLR, appended a replacement,
cleared all five with CLR, and submitted five again. Captured frames and
retained EUC-KR guest strings establish the field value, and the guest's
confirmation buttons continued into the story. Old snapshots were rejected
after deletion and dismissal. Both startup logos rendered without the green
backdrop, with no image-decode errors on that route. This does not establish
whole-game rendering or saved-name restoration.

Artifacts and the opt-in replay probe are under the ignored
`var/diagnostics/ktf-c-input-logo-20260919/`. Physical iPhone IME interaction
was not repeated; the replay calls the same platform text provider as the
browser transport, and no session protocol or page change was needed. The
final code passed `make test` (including Node tests), `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and `make server`.

<a id="ktf-gameplay-qa-2026-09-20"></a>

## KTF gameplay QA, 2026-09-20

This investigation covers a rope-scrolling report, two additional-content
prompts, and four reports of excessive gameplay speed. Baseline revision is
`5a9321f`. Archive identities are SHA-256 prefixes. Real archives and probe
output remain ignored; original archives and user saves are unchanged, and all
writes use `var/savedata/ktf-qa-20260920/`.

<a id="ktf-gameplay-qa-2026-09-20-missing-additional-content"></a>

### Missing additional content

Archive `689c491586b9` shows the additional-content offer after 400 CLI
service rounds. Accepting with key 1 and advancing 200 rounds displays the
network-connection failure. The runtime reports missing `Config.dat` and
`MixTable`; neither exists in the outer package or the payload JAR, and a
similarly named menu resource is not the required data file.

The three packaged `ZipTable` resources independently prove the content is
missing. Each has a little-endian 16-bit count followed by nine-byte records:
container number, 32-bit offset, 16-bit packed length, and 16-bit unpacked
length. The maximum offset plus packed length for each container gives:

| Index | Records | Absent containers | Required packed bytes |
| --- | ---: | --- | ---: |
| `5/ZipTable` | 28 | `5/0`–`5/1` | 97,292 |
| `7/ZipTable` | 62 | `7/0`–`7/5` | 279,584 |
| `9/ZipTable` | 29 | `9/0`–`9/3` | 166,375 |

All twelve containers are absent from the payload JAR; their combined extent
is **543,251 bytes**. The outer package contains only the JAR, descriptor,
metadata, and icons. This repeats the historical completeness check against
the current archive rather than inferring completeness from archive size. The
title requires an original complete package; accepting a download without
supplying those bytes cannot establish gameplay.

Local evidence: `build/ktf-qa-273-{start,download}.{json,png,log}` where
present. The initial `start.log` contains an unsuccessful screenshot command;
the later live probe uses the supported `shot` command and captures the prompt.

<a id="ktf-gameplay-qa-2026-09-20-packaged-data-rejected-with-existing-saves"></a>

### Packaged data rejected with existing saves

Archive `93d5b6b8ceb5`, save owner `PD005362`, contains five complete data
files — `char.dat` (45,677 bytes), `map.dat` (163,179), `mon.dat` (182,527),
`pattern.dat` (76,964), and `tile.dat` (155,061) — alongside a 64-byte
`prefs`. Each data file in the current debug save is byte-identical to its
packaged copy.

A fresh isolated runtime selects `ktf-subscriber-fallback`, reads all five
files, and reaches the menu, scenario selection, empty slots, character
selection, and initial attribute selection.

Copying the existing debug save to an isolated root reproduces the 600 KB
download offer. Only `prefs` is opened before the offer; the five data files
are not consulted. A second isolated copy that changes only `db/prefs` to the
packaged 64 bytes reaches the menu, with all progress files, including
`db/save0.dat`, still present. The subscriber fallback alone therefore does
not handle the existing saved receipt. The packaged record's SHA-256 prefix is
`2a702cdf2db4`; the rejected saved record's is `ab7dbef36a21`. Replacing the
whole preference record is not an acceptable production fix: a correction
must preserve user progress and distinguish receipt fields from settings.

Evidence: `build/ktf-qa-248-{copied,prefs-only}.{json,png,log}` and the
`menu`, `scenario`, `slot`, and `character` PNGs. Copied saves are under
`copied/PD005362` and `prefs-only/PD005362` in the isolated QA root.

<a id="ktf-gameplay-qa-2026-09-20-receipt-field-diagnosis-and-correction"></a>

#### Receipt field diagnosis and correction

The guest decoder accepts the rejected record's encrypted checksum. Decoding
with its own substitution table and session identity yields marker 75,
settings 3 and 2, five zero 19-bit content lengths, two zero 32-bit fields,
and a final zero two-bit field. The packaged record has the same marker and
settings, lengths 45,633 / 163,135 / 182,483 / 76,920 / 155,017, and final
field 1. Each length is the corresponding file size minus its 44-byte footer.

Changing only those five lengths in an isolated receipt, retaining the final
field and all other bits, passes the download prompt and exposes the existing
level-one slot, so the unknown final field need not change. The decoder probe
is `build/ktf_qa_receipt_decode_test.txt` with log `build/ktf-qa-decode.log`;
the field-only comparison is `build/ktf-qa-248-fields-{menu,scenario,slot}.png`.

The production adapter applies the same field-only recovery on reads of
`db/prefs`, only for the identified compiled-image digest and recognized
subscriber accessor. Packaged receipt lengths must match all five resources;
saved overrides must match packaged bytes; deletion ledgers or read errors
prevent recovery. Existing settings and save slots are preserved, and startup
writes no replacement record. Other compiled revisions keep the existing
behavior until their private format is established.

<a id="ktf-gameplay-qa-2026-09-20-production-acceptance"></a>

#### Production acceptance

The production CLI, without diagnostic overlays or pre-edited receipts, reads
a fresh copy of the failing user save, passes the prompt, exposes the existing
level-one slot, and loads it into a visible town scene. The route completes
2,508 service ticks without error. Captures are
`build/ktf-qa-248-production-{menu,slot,play}.png`; diagnostics and console
output are `build/ktf-qa-248-production.{json,log}`. Both the production copy
and the original user tree retain receipt hash prefix `ab7dbef36a21` and slot
hash prefix `fd1d01fb8ed7` after the run; no original receipt was deleted or
overwritten.

Authored regressions cover exact field preservation, checksum regeneration,
word-crossing bit order, ordinary save/restart, incomplete or deleted content,
changed data overrides, invalid records, read failures, and rejection of other
executables. The opt-in local archive recognition test passes. No original
archive or cipher table is included in the tests.

<a id="ktf-gameplay-qa-2026-09-20-rope-scrolling-reproduction"></a>

### Rope-scrolling reproduction

The retained server report at `2026-09-20T08:27:40.666Z` corresponds to
archive `db8ef04a6504`, save owner `PD006654`. It reports guest speed 1.00x,
4,168 rounds, 1,581 flushes, and 853 `Clet$CletCard.paint` calls; timers
account for 99% of measured service work. The page report supplies 212 key
edges from the last session, beginning at `08:27:05.874Z`.

The first replay passed page key codes directly to the platform and is invalid
as reproduction evidence. The corrected replay applies the Host's key
conversion, runs 50.002 seconds, advances guest time 50.048 seconds, delivers
all 212 edges, and records 999 flushes over 6,722 service calls. Captures
progress from opening dialogue to the tutorial map with a rope visible, but
not to the reported held-rope scene; the on-time clock does not establish
scrolling correctness.

Evidence: `build/ktf_qa_replay.go`, `build/ktf-qa-rope-events.json`,
`build/ktf-qa-rope-mapped.{json,log}`, and sampled
`build/ktf-qa-rope-mapped-NNN.png` frames. The corrected replay exits normally.

<a id="ktf-gameplay-qa-2026-09-20-held-rope-reproduction-and-rejected-paint-hypothesis"></a>

#### Held-rope reproduction and rejected paint hypothesis

The manual replay must allow at least one service frame after releasing a key
before pressing the next; without that gap the game's input logic drops
actions. The route `build/ktf-qa-rope-route.txt` reaches the rope, grabs it
with two separate UP presses, waits two guest seconds, then climbs in two
350 ms segments, capturing every service frame. No original save is changed.

The stationary hanging segment reproduces the defect. Matching a fixed 30×10
pixel patch of the island above the player over frames 649–888 places it at
Y=114 or Y=130, with **40 transitions in two guest seconds**. Character and
terrain move together: a 16-pixel camera oscillation without input.

A diagnostic experiment suppressed all automatic paints after a timer directly
flushed its screen. It reduced paint calls to four startup calls, but the held
rope produced the same 114/130 coordinates and 40 transitions. Unsolicited
paint explains the four speed reports, not this defect, so the experiment was
removed from production; it is retained only under ignored build paths.

Evidence: `build/ktf-qa-rope-{before,after}.log`,
`build/ktf-qa-rope-{before,after}-frame-*.png`,
`build/ktf-qa-rope-camera.json`, and
`build/ktf-qa-rope-direct-flush-experiment.json`. The production memory probe
reuses the after-frame filenames; the camera coordinates are unchanged.

<a id="ktf-gameplay-qa-2026-09-20-camera-state-and-viewport-mismatch"></a>

#### Camera state and viewport mismatch

Snapshots at held-rope frames 651 and 657 differ in only 15 aligned words of
the relocated client image. The camera Y field at `0x1a2500` changes from 0 to
-16. Its map object has width 320 and height 304; the viewport object has
width 240 and height 320.

A hardware-style write watch identifies the camera smoothing store at
`0x11db9a` and the clamp store at `0x11dbe2`. The clamp first sets a negative
camera Y to zero; otherwise, when `cameraY + viewportHeight > mapHeight`, it
sets Y to `mapHeight - viewportHeight`, which is -16 for a 304-pixel map and a
320-pixel viewport. Smoothing followed by these two branches alternates the
visible camera position. This is guest code, not a fluctuating Host clock or a
screen-conversion offset.

A startup watch identifies viewport initialization at `0x139464`: the guest
copies width and height from its screen framebuffer record at offsets 8 and
12, and the platform supplies its default 240×320. This identifies the
geometry mismatch without establishing a different handset resolution.

Evidence: `build/ktf-qa-rope-image-{651,657}.bin`,
`build/ktf-qa-rope-watch.json`, and `build/ktf-qa-rope-viewport-watch.json`.
The image snapshots are ignored diagnostics, never bundled fixtures.

<a id="ktf-gameplay-qa-2026-09-20-revision-scoped-camera-correction-and-acceptance"></a>

#### Revision-scoped camera correction and acceptance

The WIPI [screen framebuffer contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
returns the display's framebuffer and gives no rule for subtracting 16 pixels
because one map is shorter. The correction therefore preserves the selected
240×320 screen, framebuffer stride, graphics APIs, input coordinates, and save
format, and adapts the identified guest clamp instead.

Only compiled-image SHA-256
`9ba7fbd6552f8176dfe9411ba8815a33dcf463ff50418e9f8f92707244f99b9c`
qualifies, and the relocated clamp routine must also match its own digest
before one Thumb store is replaced in emulator memory. At that instruction,
negative camera Y stores become zero; nonnegative values, all registers and
flags, the horizontal clamp, smoothing and return path keep their behavior.
The on-disk archive is never patched, and unknown images or changed routines
receive no correction. This adapts guest logic; it does not claim that ARM
stores or WIPI framebuffer heights have different semantics.

The unchanged recorded route reaches the same rope, hangs, climbs twice, and
reaches its top. Tracking the same island patch gives:

| Route segment | Samples | Island Y | Position changes |
| --- | ---: | --- | ---: |
| Hang, two seconds | 240 | 114 | 0 |
| First climb, 350 ms | 42 | 114 | 0 |
| First pause, one second | 120 | 114 | 0 |
| Second climb, 350 ms | 42 | 114 | 0 |
| Second pause, one second | 120 | 114 | 0 |

The baseline hanging segment switches between Y=114 and Y=130 forty times.
The corrected route advances 61.415 guest seconds, flushes 1,280 frames, and
records one installed adaptation and 584 corrected negative stores. The player
moves upward while the island stays still, so rendering is not frozen.
Evidence: `build/ktf-qa-rope-camera-fixed.log`,
`build/ktf-qa-rope-camera-fixed.json`,
`build/ktf-qa-rope-camera-fixed-frame-*.png`, and
`build/ktf-qa-rope-camera-acceptance.json`.

Authored Thumb regressions run the original and corrected clamp for 400 steps
on maps shorter than, equal to, and taller than the viewport: the short-map
oscillation disappears and the other bounds and ordinary scrolling are
unchanged. Further tests preserve neighboring memory, every register and
condition flag; cover signed extremes; reject unknown executables and calls
from another instruction; and reject unmapped or wrapping field addresses.

<a id="ktf-gameplay-qa-2026-09-20-excessive-speed"></a>

### Excessive speed

The four reported archives are `1d18b7cc06d1`, `8fdd8f52b5bf`,
`f286e817508f`, and `3b72d4676d5d`. Existing timing code applies a 16.667 ms
frame-period floor and an instruction cost model; these are emulator policy,
not measured handset rates. The WIPI 1.2.1
[kernel specification](https://mirusu400.github.io/wipi-wiki/c-api/kernel.md)
specifies timer delays in milliseconds and no universal game frame rate, so
neither a global speed change nor a fourfold timing defect is claimed without
tracing each archive's actual loop.

<a id="ktf-gameplay-qa-2026-09-20-asynchronous-repaint-cadence"></a>

#### Asynchronous repaint cadence

The WIPI [Card contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Card.md)
distinguishes asynchronous `repaint` from synchronous `serviceRepaints`, and
the [graphics contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
queues `MC_grpRepaint` rather than calling `paintClet` inline.

The Host respected a live worker calling `serviceRepaints` but not one calling
only `repaint`, so it painted again while the worker slept. The C entry queued
an event only for a guest-owned event loop and dropped requests in
Host-driven sessions; automatic Host painting masked that omission.

Asynchronous Java requests now give their live worker the same card
ownership. C requests set the pending flag in both event-loop modes. A C timer
requesting repaint retains ownership only through a rearm of the same record
and callback; timer cancellation, callback replacement, worker exit, and
another visible card leave automatic fallback eligible. No global speed
multiplier or timer delay changed, and bootstrap-only automatic painting
remains supported.

A manual-clock probe runs 1,200 session ticks with the same input schedule:

| Archive | Paint calls before | After | Repaint requests after | Guest seconds after |
| --- | ---: | ---: | ---: | ---: |
| `1d18b7cc06d1` | 670 | 143 | 137 | 10.660 |
| `8fdd8f52b5bf` | 1,200 | 244 | 239 | 19.200 |
| `f286e817508f` | 1,035 | 157 | 150 | 18.012 |
| `3b72d4676d5d` | 851 | 401 | 401 | 16.092 |

The extra initial calls precede the first worker/timer ownership. Stages
differ because paint can advance application state, so before/after images are
not paired speed measurements. This proves removal of unsolicited paint, not a
handset-equivalent speed for all four campaigns. Probe and outputs:
`build/ktf_qa_timing_probe.txt`, `build/ktf-qa-speed-baseline.log`,
`build/ktf-qa-speed-after.log`, and the per-archive diagnostics JSON files.

Authored regressions reproduce 100 world steps from one asynchronous request
before the correction. A Thumb callback requests repaint and rearms a 120 ms
timer through the actual platform stubs; repeated idle paint service does not
advance the world. Cancelling that timer restores fallback even while an
unrelated timer remains queued.

<a id="ktf-gameplay-qa-2026-09-20-gameplay-cadence-after-correction"></a>

#### Gameplay cadence after correction

A second manual-clock route runs 6,000 ticks, pressing OK every 120 ticks and
releasing two ticks later. All four archives reach visible gameplay. Deltas
over the last 1,200 ticks, excluding startup:

| Archive | Guest seconds | Repaint requests | Paint calls | Paints per second |
| --- | ---: | ---: | ---: | ---: |
| `1d18b7cc06d1` | 10.667 | 133 | 133 | 12.47 |
| `8fdd8f52b5bf` | 19.200 | 240 | 240 | 12.50 |
| `f286e817508f` | 16.934 | 240 | 240 | 14.17 |
| `3b72d4676d5d` | 15.995 | 399 | 399 | 24.95 |

Each requested repaint produces exactly one paint in these windows. The two C
loops declare 70 ms and 40 ms gameplay timers respectively. This establishes
guest-requested pacing at default speed without the former extra Host frames;
it does not measure graphics-call cost on a physical handset. Evidence:
`build/ktf-qa-play-window.log`, `build/ktf-qa-play-*-window.json`,
`build/ktf-qa-play-*.json`, and the four `build/ktf-qa-play-*-5999.png`
captures.

<a id="ktf-gameplay-qa-2026-09-20-validation-boundary"></a>

### Validation boundary

The missing-content question is resolved for the identified archive. The
receipt correction preserves existing progress and loads the saved slot into
play. All four speed cases reach gameplay with exactly one paint per requested
repaint. The rope route reproduces the old oscillation and verifies stable
hanging and climbing after the revision-scoped correction.

`make test`, `make test-debug`, `go test -race ./internal/...`, and
`go vet ./...` pass with the final implementation; the Node gate has 226
passing tests. The opt-in real-archive receipt recognition and authored
compatibility tests also pass. Final gate logs are
`build/ktf-qa-final-{test,debug,race,vet}.log` and
`build/ktf-qa-final-compatibility.log`; the speed acceptance rerun is
`build/ktf-qa-final-speed.log`. `git diff --check` passes.

These are local shared-session/CLI measurements with isolated saves. They do
not establish physical-handset performance or support for unidentified
compiled revisions, and missing game data still requires a complete original
package. No server restart, real-phone test, commit, or publication was done.

<a id="ktf-qa-2026-09-21"></a>

## KTF 1.2 QA, 2026-09-21

Baseline: `bcb36b4`. This investigation covers three reported startup
regressions, additional excessive-speed cases, one slow gameplay case, and a C
title with excessive speed, displaced text, and an image-lifetime crash.
Archive identities are SHA-256 prefixes; original archives and user saves
remain unchanged. Probes write under `var/savedata/ktf-qa-20260921/`. Sections
are in investigation order; an interim status in an earlier section is
resolved by a later one where noted.

<a id="ktf-qa-2026-09-21-current-status"></a>

### Current status

The three startup regressions, the erroneous native sleep result, the image
ownership crash, and four additional unsolicited-paint cases are corrected.
The reported in-game menu text is corrected for the identified C image, and
the selected screen size is preserved. The slow case reaches combat at 13.2
frames per wall second. Repeating-timer and serial-loop regressions and the
full test/debug/race/vet gates pass.

All 263 local inputs have bounded outcomes, including six unsupported inputs,
two execution failures and four callback-exception cases; a completed probe is
not full campaign acceptance. After the settings-submenu follow-up the user
confirmed their checks were complete and requested a PR. That records user
acceptance of the reported QA work, not a calibrated original handset speed
measurement. No arbitrary speed multiplier or global text offset is applied.
The six pre-existing failures remain separate follow-up work.

<a id="ktf-qa-2026-09-21-startup-regression"></a>

### Startup regression

The three reported archives are `c7d06bd34291`, `61ba00511b84`, and
`3185174d2121`. Each installs a visible card and starts a worker that exhausts
instruction slices without requesting a repaint or sleeping. The Host's
`forcedSlice` guard prevents automatic painting, and no display-originated
initial repaint was queued when the card became visible.

A 1,200-tick CLI run of the first archive produces zero flushes and no screen.
A bounded 20-tick probe reproduces the same state in all three: one card, one
worker, `forcedSlice=true`, `repaintPending=false`, and no flushes. Allowing
one paint releases all three: another 20 ticks produce 11, 5, and 4 flushes.
This isolates an initial-paint dependency; it does not show that every busy
worker should receive unsolicited paints.

The display now queues a repaint after successfully notifying a newly shown or
uncovered card. Worker instruction limits and subsequent frame ownership are
unchanged. The [WIPI Card specification](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Card.md)
describes visibility notification and repaint servicing; this supplies the
display invalidation needed for the newly visible card to appear. The
specification does not prescribe this emulator's worker scheduler.

`TestShownCardPaintUnblocksBusyWorker` reproduces a worker waiting for state
initialized by paint; it fails before the fix (`initial paint=false`) and
passes after it. Existing forced-slice and asynchronous cadence tests and the
KTF package suite pass, and all three local 20-tick probes produce frames
without forced diagnostic intervention.

Evidence: `build/ktf-next-cases.json`, `build/ktf-next-7-startup.{log,json}`,
`build/ktf-next-probe.log`, `build/ktf-next-force-paint.log`,
`build/ktf-next-fixed-probe.log`, and `build/ktf-next-unit.log`.

<a id="ktf-qa-2026-09-21-remaining-investigation"></a>

### Remaining investigation

The slow case is archive `4120f27288ab`. The C gameplay case is
`2f9822f32b78`; its retained server report at `2026-09-20T23:43:57.521Z` ends
in a timer callback at `0x124055`, drawing source handle `0x30063840` from
`0x12209b`. The record has framebuffer depth zero, and the handle is
allocated. Earlier diagnostics include draws of destroyed surfaces and
unreadable framebuffer destruction, which pointed to a resource-lifetime
problem without yet showing whether the guest or the emulator invalidated the
record (resolved under "Source ownership correction").

The C text API specifies baseline Y, so changing every C text call to
top-aligned text would contradict the contract; the reported scene had to be
reproduced first (resolved under "In-game menu text correction").
Excessive-speed coverage must include actual requested and unsolicited paints,
not only a static API scan or a first frame.

<a id="ktf-qa-2026-09-21-extended-startup-acceptance"></a>

### Extended startup acceptance

A manual-clock route runs 6,000 ticks per archive with separate saves and an
OK press every 120 ticks after startup. All three runs complete without error.
The final screens show an empty load-slot menu (`c7d06bd34291`), an opening
investigation dialogue (`61ba00511b84`), and the hospital scene with an
interaction response (`3185174d2121`). These establish startup recovery and
input response, not completed campaigns or save restoration. Captures and
counts are `build/ktf-next-play{7,9,10}-{1199,2399,3599,4799,5999}.{png,json}`.
The initial display fix passes `make test`, `make test-debug`, internal race,
and vet gates (`build/ktf-next-{test,debug,race,vet}.log`).

<a id="ktf-qa-2026-09-21-native-environment-return-and-the-slow-case"></a>

### Native environment return and the slow case

Archive `4120f27288ab` initially renders only three frames over 19.350387
guest seconds (1,200 ticks). Instrumenting the Java sleep boundary records
repeated 11,036 ms waits; this is not CPU saturation.

The compiled native at `0x1058bd` ends by storing a 32-bit result at
environment offset 40, then tag 2 at offset 36, and leaves the scratch offset
`0x2b1c` in r0. The frame loop reads the native result, subtracts elapsed
time, and passes it to `Thread.sleep`, so the register-only return path
reported 11,036 instead of the environment's value (initially zero, then 75).
The native's instructions at `0x105ac6` through `0x105ad0` and runtime
snapshots establish this convention for the affected caller. It was the
missing caller evidence noted in the earlier native-ABI review.

Production `CallNative` now recognizes an explicitly published tag-2 word.
The tag is cleared on entry, so an earlier result cannot override a later
register return. A nested scope restores its caller's tag and value on exit,
and both words use the existing logical-thread storage boundary. Older
modules, unprepared synthetic runtimes, absent tags, and unsupported tags keep
their register return. Other environment result tags and a wide environment
return convention are not established.

Authored Thumb fixtures cover nonzero, zero and negative word results,
unsupported tags and a subsequent register-only return; scope tests cover
nested results and separate logical threads; existing native-call tests pass.
A production run reaches the animated opening and produces 215 flushes over
15.950319 guest seconds at 1,200 ticks, then reaches a mostly empty menu
background (combat is established under "Sustained cadence and playable
slow-case follow-up"). A diagnostic run observes 10, 66 and 75 ms frame waits
after the correction; the unrelated worker's 500 ms wait is unchanged.

Evidence: `build/ktf-next-image5.bin`, `build/ktf-next-symbols5.txt`,
`build/ktf-next-native-slot5.log`, `build/ktf-next-native-production5.log`,
`build/ktf-next-play5-*.png`, and the ignored overlay sources under
`build/ktf-next-*`. Diagnostic runs overwrite the numbered play captures; the
named run logs distinguish baseline and corrected measurements.

<a id="ktf-qa-2026-09-21-reproduced-image-lifetime-crash"></a>

### Reproduced image-lifetime crash

The page log provides 72 input edges for its final C session. Replaying them
with a copy of the debug save reproduces the same failure at `0x12209b` after
14.767274801 guest seconds and 56 delivered edges. Allocation addresses differ
from the server report, but the record shape and failing call match.

The trace creates a framebuffer at `0x30062fdc` and its image wrapper at
`0x30062ffc`. On the next timer callback, `MC_grpDestroyImage` receives
`0x30062fdc`, which previously held an image record but now holds the new
framebuffer. The destructor accepts that framebuffer and releases it and its
pixels, leaving the live wrapper pointing at the released framebuffer. The
surface classifier tests whether that first word is currently allocated, so it
misclassifies the wrapper as a plain framebuffer and fails on its zero depth.
"Source ownership correction" below removes the earlier cause rather than
suppressing the final depth error.

Evidence: `build/ktf-next-replay-events.json`, `build/ktf-next-replay6.log`,
`build/ktf-next-lifetime6.log`, and `build/ktf-next-replay6.json`. The
lifetime instrumentation retains allocation sizes, destruction arguments, and
caller addresses. Screens are `build/ktf-next-replay6-*.png`.

<a id="ktf-qa-2026-09-21-speed-and-text-evidence-still-open"></a>

### Speed and text evidence still open

The suspected additional speed archive is `b3fee0c259f4`; its filename differs
from the report's wording, so its identity was not confirmed. The 6,000-tick
route reaches the tutorial prompt and records 3,000 explicit repaints and
3,000 paints over 100.002 guest seconds, so it does not show the extra-paint
mechanism of the previous fix; its `callSerially` loop is examined under
"Separately suspected serial title".

The C title records 3,001 automatic card paints and 1,500 timer flushes over
75.018167 guest seconds in the exploratory OK route. A dialog capture was
examined, but its heading may belong to the narrow upper title bar, so it did
not establish the reported in-game text defect or a replacement for the
specified baseline convention. Neither this route nor the crash replay proves
handset-equivalent pacing. Extra card paints were later excluded as the cause
of this title's speed ("Rejected C cadence hypothesis"), and its timer cadence
was measured ("C timer request measurement"); its timer and update loop were
not investigated further.

<a id="ktf-qa-2026-09-21-current-validation"></a>

### Current validation

After the native-return and startup changes, `make test` (including 226 Node
tests), `make test-debug`, `go test -race ./internal/...`, and `go vet ./...`
pass (`build/ktf-next-native-{test,debug,race,vet}.log`), and
`git diff --check` passes. No user save, archive, running server, Git commit,
or remote publication was changed. Combat performance, broader speed
coverage, text placement, and the image-lifetime crash were still open at this
point.

<a id="ktf-qa-2026-09-21-source-ownership-correction"></a>

### Source ownership correction

Image records retain their encoded source address for destruction. This title
also frees that source itself after creation; the address then becomes
another allocation, including another image, but the old destructor still
freed it. The unexpected destruction of a live framebuffer follows these stale
ownership links; it is not evidence that malformed depth should be accepted.

`imageSourceBuffers` now records the source allocation each live image still
owns. Any release revokes every reference to that allocation before the arena
can reuse its address. Destruction consults ownership rather than trusting a
stale address in guest word thirteen, and freeing an image removes its entry.
Non-WIPI sources are not adopted. Guest image records and drawing rules keep
their layout and behavior.

The [WIPI image creation contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
says a static image's source is released after its first frame is decoded. An
experiment that moved release directly into creation made this archive's
startup fail in a later `memcpy` of its source, so it is excluded. The
emulator keeps its delayed-release compatibility, and a caller that releases
the source first now revokes that delayed ownership. The caller establishes
this divergence; it is not inferred as a change to the standard.

`TestImageDestroyPreservesReusedSourceAllocation` creates an authored PNG,
releases its source, reuses exactly that address, and destroys the old image:
before the fix destruction frees the replacement, after it the replacement
stays live. Existing complete-destruction, repeated-allocation, and
out-of-bounds source-field tests pass.

The unchanged 72-edge crash replay now completes 40.0008 guest seconds, 4,099
ticks and 1,700 flushes without error, where the old run fails after 56 edges
and 14.767274801 seconds. A separate interactive CLI route reaches character
selection, a locked-character dialog, the stage map and a playable character
in the first arena. All writes remain in isolated saves.

Evidence: `build/ktf-source-lifetime-replay.log` (rejected eager release),
`build/ktf-source-owner-replay.log` (ownership experiment),
`build/ktf-source-production-replay.log` (production), and
`build/ktf-console6-{popup,play}.{png,json}`. The source correction passes
`make test`, `make test-debug`, internal race and vet
(`build/ktf-source-{test,debug,race,vet}.log`).

<a id="ktf-qa-2026-09-21-bounded-corpus-cadence-survey"></a>

### Bounded corpus cadence survey

All 263 files in the local KTF 1.2 directory were attempted. The runner uses
isolated fresh saves, a fixed clock origin, 20 guest seconds or 2,400 ticks,
one 100 ms OK press per guest second, and an eight-second process-internal
wall deadline, with three processes concurrently. These are startup/input and
cadence observations, not complete campaigns or wall-performance benchmarks.

The outcomes are 214 bounded runs without an error, 37 probe deadlines, six
inputs not loaded, four guest-requested exits, and two other guest failures.
Deadlines are inconclusive. The input failures include unsupported
containers; a filename extension alone is not format recognition. The two
other failures concern a text-component field and a guest database
destination; their comparison with the original HEAD is under "Comparison and
current gates".

The corpus manifest, per-archive counts, captures, and outcome summary are
`build/ktf-cadence-corpus.json`, `build/ktf-cadence-sweep/`, and
`build/ktf-cadence-summary.json`. The summary retains the compiled runner's
SHA-256 so the survey is not mistaken for a measurement of later changes.

<a id="ktf-qa-2026-09-21-repeating-java-timer-ownership"></a>

#### Repeating Java timer ownership

A repeating Java timer creates a worker for each invocation. The previous
cadence fix attached ownership to that worker, so its return restored
automatic painting even though the next invocation of the same task was still
scheduled. Three archives show substantially more paints than requests in
this gap.

A successfully returned task now passes its card ownership to the pending
entry with the same Timer and task identities; cancelling or replacing the
task releases it. Synchronous repaint ownership is preserved. The authored
scheduler regression fails before the fix with twenty world steps from one
request; after it, three invocations each advance the world once, and
replacing the task on the same Timer restores fallback painting.

The same bounded input route with fresh saves gives:

| Archive | Paints before | Requests before | Paints after | Requests after |
| --- | ---: | ---: | ---: | ---: |
| `231df14c4f50` | 1,271 | 177 | 191 | 178 |
| `6d63b4025bef` | 1,295 | 99 | 112 | 99 |
| `87bc9d3210d4` | 1,318 | 131 | 143 | 133 |

Initial display paints and pre-ownership startup account for the remaining
paints; these aggregate counts do not alone prove each campaign's
steady-state rate. Changed game state can change later requests, so the small
request-count differences are kept rather than normalized away. Results are
in `build/ktf-cadence-fixed/`.

<a id="ktf-qa-2026-09-21-rejected-c-cadence-hypothesis"></a>

#### Rejected C cadence hypothesis

An experiment gave directly flushing C timers the same persistent ownership.
The reported C replay dropped to three card paints but still produced 1,701
flushes in 40.0008 guest seconds against production's 1,700, so extra card
paints do not explain that title's excessive gameplay speed. The experiment is
excluded (`build/ktf-timer-publish-replay.log`). This result does not affect
the independently reproduced repeating-Java-task defect.

<a id="ktf-qa-2026-09-21-comparison-and-current-gates"></a>

### Comparison and current gates

Both non-exit failures reproduce identically in `bcb36b4`: archive
`4c5da24020cd` lacks `TextComponent.m_cPos:I`, and `59263295de74` supplies the
unmapped database destination `0x1d14250`. They are pre-existing failures,
not regressions from this investigation. (The field is published since
2026-10-10; see
[the KTF history](#implementation-a-text-components-protected-fields).
The destination is an address the archive's packaged save carries from the
handset that wrote it; see
[the KTF history](#implementation-a-packaged-save-that-holds-the-address-of-the-phone-that-wrote-it).)
Original-HEAD comparison results are `build/ktf-cadence-head/`.

After the repeating-timer correction, `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` pass
(`build/ktf-timer-{test,debug,race,vet}.log`), and `git diff --check` passes.
No blanket C paint suppression or eager image-source release is included in
production.

<a id="ktf-qa-2026-09-21-sustained-cadence-and-playable-slow-case-follow-up"></a>

### Sustained cadence and playable slow-case follow-up

The three corrected repeating-timer cases also complete 60 guest seconds with
a fresh save and a clock advanced by 1 ms per tick. Between seconds 10 and 60
the paint/request deltas are 360/360 (`231df14c4f50`), 250/250
(`6d63b4025bef`), and 333/333 (`87bc9d3210d4`), so the bootstrap surplus does
not grow between task invocations. The route still uses a 100 ms OK press per
second and is not full campaign coverage. Results and ten-second snapshots are
in `build/ktf-cadence-steady/`.

The slow archive reaches the town, accepts movement and dialogue, opens the
region map, and enters an arena with active enemies. Starting a new slot
requires the numeric confirmation key the guest displays; repeating OK alone
does not complete that route. The fixed 1 ms clock gives 67 additional
paints, repaint requests and flushes during five seconds of arena activity,
with movement visible and no exception. Captures and counters are
`build/ktf-slow-fixed-combat{0,5}.{png,json}`. The earlier mostly empty menu
captures therefore do not indicate a menu deadlock.

A release run on the wall clock produces 129 flushes in 9.771 seconds (13.2
per second) in its final town window, consistent with the 75 ms frame wait
established at the native return boundary (`build/ktf-slow-wall-route.log`,
`build/ktf-slow-wall-steady.{png,json}`). The large erroneous sleep is gone in
real time as well as in accelerated probes.

A further probe follows the manual input route into the arena, then switches
only the clock source to elapsed wall time, preserving the current guest
instant and all outstanding deadlines, and services `TickFor`, the entry point
the interactive Host uses. In 10.016 seconds it publishes 132 frames (13.2 per
second), and repeated attack input produces a visible 26-hit combo; the
before/after captures both show the arena and the run ends without error. This
establishes playable combat at the declared 75 ms cadence after the
native-return fix; it does not measure an original handset or the browser's
presentation rate.

Evidence: `build/ktf-slow-switch-test.log`,
`build/ktf-slow-switch-{before,after}.{png,json}`, and the ignored
`build/ktf_wall_combat_probe.txt`. Earlier whole-route wall-clock scripts
ended in town because their timed inputs reached different UI states, so files
named `wall-combat` or `wall-timed-combat` are not arena evidence.

<a id="ktf-qa-2026-09-21-c-timer-request-measurement"></a>

### C timer request measurement

The C crash replay is unchanged and still completes without error. An ignored
instrumentation overlay counts timer requests by callback, delay, and clock
second: callback `0x124055` arms 199 waits of 50 ms, then 1,501 waits of 20 ms,
and complete sampled seconds in the later interval contain 50 requests each.
Total publication remains 1,700 flushes in 40.0008 seconds. This is a
requested timer cadence, not an automatic paint surplus.

The callback disassembly confirms the distinction: it selects a literal 50 ms
on one state path and a state-provided interval on the gameplay path. The
[timer specification](https://mirusu400.github.io/wipi-wiki/llms-full.txt)
defines milliseconds and allows operating-system resolution and scheduling
error; it does not establish this archive's original handset frame rate, so no
global slowdown is inferred from the 50 Hz observation. Whether callback
deadline accounting or unmodelled handset work explains the reported speed
remains open. Evidence is `build/ktf-c-delay-replay6.json`,
`build/ktf-c-delay-replay.log`, and the relocated local image
`build/ktf-c-image6.bin`; the diagnostic overlay is excluded from production.

<a id="ktf-qa-2026-09-21-deadline-recheck-and-serial-repaint-ownership"></a>

### Deadline recheck and serial repaint ownership

The 37 inconclusive corpus entries were retried with a 30-second wall
deadline, a fresh save, and a clock advanced by 1 ms per tick toward 20 guest
seconds. Twenty-five reach the target without error; twelve still exhaust the
probe deadline and remain inconclusive (resolved under "Final bounded corpus
classification"). Corpus identities and runner hashes are in
`build/ktf-cadence-deadline-summary.json`; results are in
`build/ktf-cadence-deadline-recheck/`. This recheck predates the serial fix
below.

Archive `d373b4d1370e` exposes another unsolicited-paint path: its
self-queued serial Runnable polls each idle pass and requests repaint only
when its own frame interval expires. Over 20 seconds it makes 209 requests but
receives 1,250 paints. The worker and timer ownership rules do not cover a
serial invocation, which runs on the client thread and returns between polls.

A serial invocation now retains the shown card whose repaint it requested or
serviced, but only while the same Runnable is queued again. Polls without a
new repaint keep the ownership; returning without requeueing releases it, and
a different instance of the same class does not inherit it. An explicit
repaint still runs. Serial dispatch timing and timeout behavior are
unchanged. Ownership storage is limited to queued Runnables and removed when
each is taken.

The authored regression exercises the serial scheduler, polling passes,
asynchronous and synchronous repaint, and replacement by another instance.
Before the fix one request produces twenty asynchronous or thirteen
synchronous paints; after it each requested frame advances the world once,
and stopping the original loop restores fallback painting.

The same fresh-save, 1 ms clock route for `d373b4d1370e` now gives 214 paints
from the same 209 requests, the remaining five during bootstrap. All 27 corpus
entries observed using `callSerially` complete the 20-second recheck without
error, which does not establish completed gameplay for every entry. Results
and runner identity are in `build/ktf-cadence-serial-fixed/` and
`build/ktf-cadence-serial-summary.json`.

After this correction, `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` pass
(`build/ktf-serial-{test,debug,race,vet}.log`).

<a id="ktf-qa-2026-09-21-popup-text-path"></a>

### Popup text path

A C character-selection route captures a locked-character popup in
`build/ktf-c-text-locked.png`. Instrumentation records every string draw
through both byte and Unicode APIs. The popup body produces no additional
string calls; its glyphs are assembled from image strips such as 9-by-255,
4-by-147 and 9-by-135 pixels, and the string API calls in this route render
the footer. Changing the shared string baseline therefore cannot correct this
popup's body. The body begins near the icon/header area, but the intended
original layout was not established, so this is not a proven reproduction of
the reported scene. Image-call and string-call counters are in
`build/ktf-image-text-popup.json`, with the route in
`build/ktf-c-popup-route.jsonl`; all instrumentation is excluded from
production.

<a id="ktf-qa-2026-09-21-final-bounded-corpus-classification"></a>

### Final bounded corpus classification

All twelve remaining deadline cases complete at least 20 guest seconds when
run on the wall clock with `TickFor`, new isolated saves, and a 30-second
overall context deadline. This separates the cost of forcing short
manual-clock advances through guest waits from a runtime failure. Results and
runner identity are `build/ktf-cadence-wall-recheck/` and
`build/ktf-cadence-wall-summary.json`.

A callback exception can end a guest thread or paint without returning a fatal
session error, so such cases are counted separately. Four archives have them:
`929a1b0477c5` and `d234152636c8` report array bounds exceptions,
`868dec926b27` a null image during paint, and `53f738529d20` a null reference
in a worker. All four reproduce on original HEAD `bcb36b4` with the same
callback and exception class. The worker case's guest stack begins at
`0x123d2b` and `0x120111`; its later automatic paints follow the failed
worker's exit and are not evidence of another live-loop ownership bug.
Original comparisons are `build/ktf-cadence-head-exception/`.

The combined 263-input classification is 247 completed bounded probes without
recorded uncaught callbacks, four callback-exception cases, six inputs not
loaded, four guest-requested exits, and two fatal guest execution failures
(`build/ktf-cadence-combined-summary.json` identifies each contributing
result). It combines the recorded stages and revisions; it is not one uniform
post-fix wall-clock run and does not claim full gameplay for 247 archives. The
six exception/execution cases predate this work and remain separate
compatibility follow-ups.

<a id="ktf-qa-2026-09-21-separately-suspected-serial-title"></a>

### Separately suspected serial title

Archive `b3fee0c259f4` reaches the menu, starts the scenario, and advances to
a park scene. A wall-clock opening route publishes 863 frames in 20.007 guest
seconds, with 863 sleeps of 10 ms each; repaint counts track publication, so
this is not the unsolicited-paint failure fixed above. Earlier 30 Hz
observations used deadline-skipping probes and are not a wall frame-rate
measurement. Neither the short requested wait nor this opening scene
establishes the intended original handset speed.

Evidence: `build/ktf-cadence-serial-wall/b3fee0c259f4.{png,json}` and
`build/ktf-serial11-{title,new,settled}.png`. This archive calls no timeout
overload. The separately corrected serial-loop archive uses a 30 ms
`callSerially` timeout, which the compatibility path ignores while the title's
own elapsed-time test controls repaint requests; that timeout behavior is
unchanged and is not asserted to implement the specification's timeout
parameter. A normal-speed reference was requested from the user, who then
identified the text scene.

<a id="ktf-qa-2026-09-21-in-game-menu-text-correction"></a>

### In-game menu text correction

The user's screenshot identifies the in-game pause menu in archive
`2f9822f32b78`. A deterministic input route reproduces the displacement at
2,538 ticks. The letters are assembled from bundled bitmap glyph strips, not
drawn by the WIPI string APIs, so font ascent or baseline changes cannot move
them.

The guest draws a 172×168 dialog panel centered at screen height / 2. Its
separate text routine uses fixed title Y=85 and body Y=115, plus an origin the
guest explicitly initializes to zero; memory watches identify those stores and
no later writer. At 240×320 the panel starts at Y=76, putting its title over
the upper border and its first body line over the header box. The same route
at 240×280 puts the panel at Y=56 and aligns both text blocks. This shows a
mismatch between the fixed text layout and the selected display; it does not
identify the archive's original handset model or justify altering every
framebuffer's reported height.

The [WIPI framebuffer contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
returns the display framebuffer. The correction keeps that contract and adapts
this guest routine through the revision-scoped compatibility boundary.
Compiled-image SHA-256
`d4fd51c94269e3143b57802c00898f2ce43dee2e5726076d133b44a0304809c6`
and both relocated layout digests must match. Seven `STR r3,[sp,#4]`
instructions across the common dialog and settings layouts are replaced in
emulator memory, and their stored text Y gains `floor(screenHeight/2) - 140`.
Registers, flags, horizontal coordinates, panel rendering and the on-disk
archive keep their behavior, and unknown or changed images receive no
correction. This is a compatibility adaptation, not a claim about original
handset behavior.

At 240×320 the title now starts at Y=105 and the body at Y=135, inside their
boxes; the same routine's character-selection notice also aligns. The replay
resumes combat through menu item 1 and continues to 2,748 ticks without error.
Replaying the identical route without the correction produces pixel-identical
character-description and resumed-combat frames, with differences confined to
dialog text in the two dialog captures. Authored regression tests cover even
and odd screen heights, an unchanged 280-pixel layout, neighboring memory,
register/flag preservation, unknown images, unauthorized hook locations and
invalid stack addresses. `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` pass
(`build/ktf-menu-{test,debug,race,vet}.log`).

Evidence: `build/ktf-ge-menu-route.jsonl`, `build/ktf-menu-glyph-trace.txt`,
`build/ktf-menu-geometry.txt`, `build/ktf-menu-280-end.png`,
`build/ktf-menu-fixed-{end,popup,resume}.png`,
`build/ktf-menu-fixed-resume-run.log`, and `build/ktf-menu-fixed-diag.json`.
The traces and original assets are local diagnostics, not bundled fixtures.

<a id="ktf-qa-2026-09-21-follow-up-settings-submenu"></a>

#### Follow-up: settings submenu

The first correction covered only the common dialog layout and missed the
separate settings layout; the user reported displacement after entering
another menu. Selecting item 2 from the pause menu reproduces it at 2,648
ticks.

The settings panel is also centered at Y=76, but four text calls use fixed Y
values 85, 115, 128 and 141, and the vibration row has separate ON and OFF
argument stores that converge on one text call. Five additional stores are
therefore needed: title, both vibration states, volume and speed. Both code
regions are validated before any store is replaced, so a changed settings
layout cannot receive only the common-dialog correction.

The extended regression fails against the first correction at `0x129740` and
passes with all seven stores, checking layout-relative positions and
machine-state preservation at five screen heights, including an odd height.
Separate local save directories cover settings ON/OFF, volume/speed changes,
main-menu and world-map navigation, the exit confirmation and CLR return. The
full `make test`, `make test-debug`, internal race and vet checks pass again
(`build/ktf-submenu-{test,debug,race,vet}.log`).

Evidence: `build/ktf-submenu-before.png`,
`build/ktf-submenu-glyph-trace.txt`,
`build/ktf-submenu-regression-{before,after}.log`,
`build/ktf-submenu-{2,3,4,5}-route.jsonl`,
`build/ktf-submenu-{2,3,4,5}-isolated.log`, and
`build/ktf-submenu-fixed-*.png`.

<a id="ktf-qa-2026-09-21-user-acceptance"></a>

### User acceptance

On 2026-09-21, after the settings-submenu correction, the user confirmed that
their checks were complete and requested publication of the fixes as a PR.
The reported QA batch is accepted. The bounded corpus survey, unconfirmed
native return tags and the absence of original handset timing measurements
keep the limitations recorded above; acceptance does not expand those proofs.

<a id="ktf-graphics-input-qa-2026-09-23"></a>

## KTF dialogue clipping and deferred text input

<a id="ktf-graphics-input-qa-2026-09-23-scope-and-evidence"></a>

### Scope and evidence

The reported failures concern two local archives, identified without titles:

| Case | Archive SHA-256 | Reported path |
| --- | --- | --- |
| A | `016d344f7c328e6713c3e553555069472e82ecae9123eb25dc304bc6d1e2c890` | Dialogue borders and a four-character Korean name field |
| B | `c36cbe78011f1a794341e4a250aeb1a2d23466c0b9dfdb68e802aa5ee96a5a7e` | A five-character Korean name field |

The newer report for case B supersedes the initial authentication diagnosis:
both archives reach their name editor, and the failure occurs when the Host
submits text before the guest has processed its queued key. No authentication
change is needed for this path.

The 2026-09-23 debug logs and isolated-save replays establish the failures.
The [WIPI graphics and input-method specification](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
defines clip coordinates and completed/composing input buffers. It leaves the
graphics record opaque; the vendor layout and deferred key delivery below come
from the actual callers.

<a id="ktf-graphics-input-qa-2026-09-23-graphics-correction"></a>

### Graphics correction

Case A's SDK helper at `0x1017d8` examines the leading graphics-context word
before reading the clip through `GetContext`; zero selects the full
framebuffer. `SetContext` stored the four clip coordinates without setting
this word, so nested drawing restored full bounds and painted past the
dialogue's right and bottom edges.

Setting a clip now sets the leading flag; a null clip clears it. An invalid
source pointer leaves the context unchanged. An explicitly empty intersection
must also draw nothing: treating every empty rectangle as an absent clip let
widget border fragments escape the name editor, so the renderer distinguishes
an enabled empty clip from a disabled clip. Nonempty bounds written directly
by a caller remain supported.

The reproduced dialogue stays within its requested horizontal bounds
`[34, 206)`, and the name editor no longer draws stray border fragments. This
is evidence for those widgets, not a whole-game rendering certification.

<a id="ktf-graphics-input-qa-2026-09-23-input-correction"></a>

### Input correction

Both cards queue their numeric-key notification and consume it in a later C
timer callback through `MC_imHandleInput`. The Host required that call during
`keyNotify` itself, so it returned `ErrTextInputChanged`, discarded the
pending completed character, and disabled input availability; the timer then
received a carrier key with no completed text.

The input adapter records the C timer that activates or handles the editor. A
commit waits for that timer's deadline before injecting each complete EUC-KR
character, then runs the ordinary timer scheduler under the existing guest
execution lock. A manual probe clock advances to the deadline; an interactive
Host waits cancellably. A missing timer, or a deadline more than one second
away, rejects delivery before a key is queued. The 64-character submission
limit, encoding checks, and nested Host service budget remain in force.

Card, focus, mode and revision checks also run when the timer consumes text.
Canceled queued carriers are discarded, and an accepted prefix invalidates the
old edit token. A new synchronous editor clears the previous timer owner.
Generic guest event loops remain unsupported; this change handles the observed
C timer path, whose consumption can be checked before returning.

CLR needs the same deferred treatment. Its timer may only flush composition,
which does not close the field. The adapter recognizes that flush only for the
pending CLR, its owning timer and the same visible card. A key that dismisses
the editor without further input-method activity still disables availability.

The guest retains its cursor, value, confirmation and length policy: case A
rejects a five-character name on confirmation and accepts four, and case B
accepts five. These limits are not encoded in the emulator. Current behavior:
[native text input](../native-text-input.md#supported-editors).

<a id="ktf-graphics-input-qa-2026-09-23-validation-and-limits"></a>

### Validation and limits

Authored regressions cover SDK clip readback, invalid clip pointers,
explicitly empty clipping for blits and rectangle fills, five-character timer
delivery with manual and wall clocks, CLR and replacement input, stale
snapshots, dismissal, mode/card changes, removed or distant timers,
cancellation, queued carrier cleanup, and replacement of the timer owner. The
timer fixture uses the platform's ARM/SVC callback path; no JVM interpreter
behavior changed.

Local debug and release probes used the real archives with isolated saves
under `var/savedata/ktf-qa-20260923/`; screenshots, diagnostics and command
transcripts are under ignored `var/diagnostics/ktf-qa-20260923/`. The route
inserts Korean text, rejects reuse of the consumed edit, deletes with CLR,
reopens the Host editor, inserts a replacement and confirms through the
guest's normal key. Both cases continue into the next story scene displaying
the submitted name; case A was checked in debug and case B in release. No
game files or original saves were modified.

`make test` (including all 227 Node tests), `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` passed, and both server
profiles were rebuilt with `make server server-release`.

Physical iPhone keyboard interaction and saved-name restoration are outside
this replay. The platform text provider is shared by the CLI and browser Host;
the session protocol and page implementation were unchanged.

<a id="ktf-input-lifetime-2026-09-23"></a>

## KTF input availability after rejected confirmation

<a id="ktf-input-lifetime-2026-09-23-reproduction"></a>

### Reproduction

The 14:52:49 local debug report exposed a route the earlier
[timer-input acceptance](#ktf-graphics-input-qa-2026-09-23) omitted. Case A,
archive SHA-256 `016d344f7c328e6713c3e553555069472e82ecae9123eb25dc304bc6d1e2c890`,
keeps its name editor open when OK is pressed with an empty name; opening Host
text input afterward returned `no supported text field is active`.

The report contains two input-mode selections and no character-processing
calls. An isolated-save replay reproduced the failure on the then-current
source: the editor remains visible, but one empty confirmation makes
`Session.TextInput` unavailable. Selecting another keypad mode or entering a
digit reactivates the input method. The earlier acceptance opened Host input
immediately after the editor appeared, so it missed this key sequence.

<a id="ktf-input-lifetime-2026-09-23-cause-and-bounded-correction"></a>

### Cause and bounded correction

`dispatchKeyToCards` invalidated pending edits on external keys and also
disabled C input whenever a key produced no input-method activity. The second
inference was too broad: an ignored confirmation neither calls the automaton
nor closes the editor. Keeping every C input activation alive would instead
expose closed editors, so the correction requires evidence of the controller
lifetime.

The [WIPI input-method contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
provides composition buffers and mode selection, but no game-owned C widget
identity or visibility query. The observed Thumb caller writes its controller
state immediately before initializing the editor and selecting the input mode.
That state is unchanged after an empty confirmation, changes while a length
error message covers the editor, returns when the message is dismissed, and
changes when a valid name is accepted.

The adapter recognizes that bounded call sequence at mode activation. It checks
the mode wrapper, saved static base, initializer and controller call target,
then resolves the state pointer through the caller's PC-relative literal and
static-base table. Code/data addresses and the state value are derived from the
running guest; there is no archive-name, hash, absolute-address or
image-content shortcut. Invalid instructions, inconsistent frames and
unreadable or overflowing pointers leave the existing conservative behavior.

For a recognized controller, availability follows its live state. Every
external key still invalidates the old edit. A validation message blocks
input; returning to the editor permits a fresh snapshot. The commit checks the
controller before delivery and again inside the timer's input-method call, so
nothing is delivered after the controller changes. Unknown C callers still
require observed input-method activity after keys. Debug diagnostics count
`C input controller lifetime recognized` when the path is established; input
contents are not logged. Current behavior:
[native text input](../native-text-input.md#supported-editors).

<a id="ktf-input-lifetime-2026-09-23-validation"></a>

### Validation

`wipic_input_owner_test.go` authors a relocated Thumb call layout and saved
frames with different addresses and a different controller value. Regressions
cover rejected confirmation, stale snapshots, hidden/restored controllers,
controller changes during timer delivery, mode changes, malformed
instructions, changed call targets, signed table offsets and invalid pointers.
Existing unrecognized-caller and timer-input tests retain their behavior.

The real-archive replay uses isolated saves, with screenshots and commands
under ignored `var/diagnostics/ktf-input-recheck-20260923/`. It presses empty
confirmation three times, inserts five Korean characters, verifies that the
guest's length-error message blocks Host input, dismisses the message, deletes
the name, inserts four characters and confirms. The next story scene displays
the submitted name, and Host input is unavailable after the editor closes.

That route passed in debug and release builds. The second archive's earlier
insertion/deletion/replacement/confirmation route passed again in release. On
the isolated PR branch, `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and `make server server-release`
passed, and the local debug and release server binaries were rebuilt.

This is shared-platform-path acceptance. The original saves and the running
user session were preserved. Physical iPhone IME interaction is not automated.

<a id="ktf-save-slots-2026-09-23"></a>

## KTF slot creation and save overlay reads

<a id="ktf-save-slots-2026-09-23-report-and-cause"></a>

### Report and cause

The local 15:15:40 debug report (duplicated at 15:15:44) ends in
`FileSystem.exists("/ev/0/f000")` with `read save fs/ev/0/f000: ... not a directory`.
Archive SHA-256:
`1379b56de66ee4f90ae53aea9a7a43117d19ab61eb1c015d1fc16c2e304251b4`.

The guest writes a save named `ev` and also reads packaged resources beneath
`ev/`; these are separate entries in the mounted resource table. The host save
store maps slashes to real directories, so probing `fs/ev/0/f000` beneath the
saved regular file returns `ENOTDIR`. The resource lookup still finds the
archive entry, but the save reader retained the host error and the native call
ended the session. Clearing saves alone cannot fix this, because the guest
creates the conflicting save again.

`DirectorySaveStore` now treats `ENOTDIR` on a read as an absent save entry,
which allows the existing archive fallback. Other read failures still
propagate. Writes beneath a regular file still fail and preserve that file.
No save format, archive contents, resolution order, or platform entry point
changed.

The [WIPI FileSystem contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/io/FileSystem.md)
defines existence queries and I/O failures but not this host save overlay, so
the correction belongs to the backend storage adapter.

<a id="ktf-save-slots-2026-09-23-first-slot-creation"></a>

### First-slot creation

A fresh-save route found a separate local-service error: selecting the first
slot reaches request kind 5, command 1410, and the service rejected its
zero-based index 0 (the display-name generator already adds one). The service
now accepts 0 through 127, retaining the signed-byte bound, message-length
validation, and conversation ordering; missing or extra slot bytes remain
invalid. This is an observed application protocol, not a WIPI API contract.
The provider keeps its dependency and endpoint checks and contacts no external
service.

Since 2026-10-10 the creation receipt asks the title for a name instead of
supplying one, and the title registers the name the player gives it — see
[A name the title asks for](#implementation-a-name-the-title-asks-for).
Slots created before that keep the names they were saved with.

<a id="ktf-save-slots-2026-09-23-evidence-and-scope"></a>

### Evidence and scope

The original 18-file debug save was copied under ignored
`var/savedata/backups/ktf-save-slots-20260923/PD006309/` and verified by
per-file SHA-256 before any reset. Acceptance used separate save trees under
`var/savedata/ktf-save-slots-fixed-{debug,release}-20260923/`. An empty host
save directory still exposes a packaged first-slot record; the route replaces
it through the game's New Game confirmation.

Both build profiles created all six slots in order, committed local-service
receipts, and restored the slots and their opening scene in new processes.
Commands, screenshots, and diagnostics are under ignored
`var/diagnostics/ktf-save-slots-20260923/`. These are shared-platform-path
checks, not a physical iPhone or complete campaign playthrough.

Recreating the third slot with the reported character-selection sequence
reaches the exact `/ev/0/f000` lookup. Replaying identical commands against
copies of the same saves fails with `ENOTDIR` in the old binary and passes in
the fixed release binary; debug also passes that lookup during creation and a
subsequent restart. Both fixed runs record `exists /ev/0/f000 found=true`
without an uncaught callback or retained save-read error.

The original save separately reproduces a slot-name paint exception in both
old and fixed binaries when the second slot is selected. This is consistent
with the earlier [invalid service label](#startup-compatibility-2026-09-19-relay-framing-and-worker-progress-2026-09-20)
record and is not repaired automatically. The requested reset replaced these
old records with the newly created slots, retaining the verified backup.

After validation, only the reported game's active debug save tree was replaced
with the verified fresh slots; other games and the active release save tree
were preserved, and the debug server was restarted with the rebuilt binary.
At the user's later request to test creation personally, the six-slot debug
save was moved to the backup tree and the active game save directory left
absent. The packaged first-slot record remains part of the unchanged archive
and can still appear before New Game replaces it.

Backend and KTF regression tests fail before the correction and pass after
it. They cover packaged descendants of a saved file, missing descendants,
writes after a resource lookup, restart, preservation of the parent file, all
six zero-based slots, and malformed service messages. Existing read-error and
transaction-failure tests remain enabled. `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` passed; both server
profiles built, and the backend tests also cross-compiled for Windows amd64
with cgo disabled.

<a id="ktf-save-slots-2026-09-23-continuous-creation-and-collection-roots"></a>

### Continuous creation and collection roots

The 15:52:36 report (duplicated at 15:52:42) follows creation in slot order
6, 2, 3, 4, then 5 without restarting. Its fifth service conversation stops
after phase 1: `RelayInputStream.read([B)I` receives address `0x3015cfd0`,
which no longer has a JVM object binding. The trace had allocated that address
as a two-byte array, and the server collected guest objects immediately before
the failure. The earlier six-slot acceptance started a new process for each
slot and so did not cover this continuous allocation history.

`Core.Call` derives a temporary ARM thread whose registers differ from its
logical parent. The collector scanned only the service thread and worker
parents, missing the registers of active derived calls, including callers
beneath another nested call. An array held only there could lose its strong
JVM binding, be reclaimed by Go, and fail at the next native boundary.

The execution layer now tracks active calls until they unwind and exposes
their saved registers through `Thread.LiveContexts`. Collection scans all
these contexts while execution is parked, including suspension inside a
native call and a forced step-budget yield. Normal returns, errors, and
cancellation remove the temporary roots. Stack scanning, Go graph retention,
collection thresholds, and the save format are unchanged.

An authored ARM regression holds different arrays only in an outer and an
inner call register, runs collection across a Go GC, and checks both
bindings. It fails before the fix for service and worker threads and passes
afterward; the arrays become collectible after normal and error unwinds. An
execution test separately parks nested calls at the step limit, checks their
register snapshots from the host, and verifies cleanup after cancellation.

The user's new 24-file save tree was copied to ignored
`var/savedata/backups/ktf-slot-order-20260923/PD006309/` and verified by
SHA-256. Acceptance used separate save trees, preserving the active saves;
commands, screenshots, diagnostics, and local collection probes are under
ignored `var/diagnostics/ktf-slot-order-20260923/`.

Debug and release both complete 6, 2, 3, 4, 5 in one process, with all five
service phases recorded five times and the fifth opening scene rendered. The
ordinary replay also passes in the old binary, so creation order alone is not
a deterministic reproducer, and running Go GC every ten ticks does not
guarantee that guest collection lands in the vulnerable window.

A local build overlay forces guest collection and Go GC after phase 1 has
completed and while phase 2 is pending. With the old register scan, the first
creation fails at the same `RelayInputStream.read([B)I` boundary: a two-byte
array allocated at guest site `0x1d5ac7` loses its binding. With the fix, both
profiles complete all five creations under the identical forced window,
including 33 nonempty collection cycles, with no failed service or uncaught
callback. The overlay and forced-GC probes are local evidence only and are not
in the server builds.

`make test`, `make test-debug`, `go test -race ./internal/...`, and
`go vet ./...` pass with the new regression tests, and both server profiles
build. The active debug server was restarted with the fix, preserving all 24
save files byte for byte. Physical iPhone replay remains a user confirmation.

<a id="ktf-native-settings-2026-10-20"></a>

## An earlier-package title's settings, sky and sparks, 2026-10-20

<a id="ktf-native-settings-2026-10-20-report-and-cause"></a>

### Report and cause

A report said a title of the earlier native package showed only a black screen,
and that it had worked in v0.6.0-pre. It had not: v0.6.0-pre and main both
reach the same digest after 200 ticks from an empty save, and the title never
ends a frame. Its module information file names application 19563.

The title's start-up sets its frame interval to 50 ms, then opens its settings
file in mode 2 and, when the open is refused, keeps that default. When the open
works it reads six words, and one of them, times 150 less 100 (or times 50 in
the other mode the title keeps), replaces the interval. This platform answered
mode 2 on a missing name by making the file, so the six reads moved no bytes,
the words stayed what the stack held, and the interval came out at
`0x209f770c` ms — 152 hours — on the local run. The empty file went to the save
store at the next boundary, so every later run read it again.

The module's own code says mode 2 does not make a file. Where the title saves
its settings it opens them in mode 2 and, refused, again in mode 4 before it
writes; the 2005 module's wrapper retries a refused 2 or 8 with 4, and creates
the name before it opens it in mode 2
([A file is an object](#implementation-a-file-is-an-object-and-the-modules-own-wrapper-names-its-methods)).
Only mode 4 makes a file now, and mode 8. The 2005 module's start-up gate
creates its scratch file before it opens it, so it does not depend on the open
making one. Mode 8 is BREW's append, which refuses a missing file as read-write
does, and the first version refused it too. The review asked what that costs a
module not in the corpus that appends without retrying; the decision was to
make the file there, which is where the 2005 module's retry with 4 ends anyway,
and to keep refusing read-write, which a title reads its settings through. A
refused open that could write is now logged once for its name at debug level,
so a title that took the refusal for a save written is found in the run's log
rather than losing that save unseen. In a browser over a save tree holding the
empty settings file, the title starts at 50 ms, draws its fireworks on a black
sky, reaches stage selection, logs its one refused read-write open of the
settings, and the empty file is gone from the tree.

<a id="ktf-native-settings-2026-10-20-migration"></a>

### Migration

A save from an earlier build keeps the empty settings file, and the title would
go on reading nothing out of it. So a stored file with nothing in it, which the
package does not carry and this play did not make, is missing. The title takes
its defaults, and the next time it saves its settings it makes the file through
mode 4 and writes them. The rule cannot tell such a file from one a title left
empty on purpose; for that one it changes which path the title takes, to the
one it takes when the file is missing.

The rule is in the one lookup every call on a name makes, so open, existence,
information and create agree about it. A first version applied it to the open
alone: a title that made a file, opened it and wrote nothing was told on its
next run that the file was there, its create answered success without making
one, and every open of it was refused. Now the existence call answers that it
is missing, in the answer the asking generation reads, and the create makes it.

"This play made it" is the names a session has kept anything under, and a quick
load hands the displaced session's names to the restored one. Without that, a
file a title had emptied was a leftover once the load had cleared the session's
own table, and the same open that worked before the load was refused after it.
A load into a fresh process starts a new play, as a fresh start does.

The leftover is then removed from the store, the first time a title asks about
it, so it goes from the save folder and from what an export carries rather than
staying there unread. That is the one removal a store makes
(`backend.RemoveEmptySave`): only an entry with nothing in it, under every name
a read could find it at — the encoded one, and the names an earlier build or
Win32 gave it — so that nothing is left to answer for the key. If one of those
names holds an older save, the empty entry in front of it stays: removing it
would hand the title that older save instead of the missing file it now sees. A
guest's own removals stay on its platform's removal list.

A quick save taken while the title was stuck brings the stuck interval back,
because a checkpoint is execution state.

<a id="ktf-native-settings-2026-10-20-what-the-title-draws-with"></a>

### What the title draws with

Running at its interval, the title reached four things this platform did not
answer, each from its call sites:

- **The clear.** At the top of every frame the title sets display colour item 2
  to black and fills the whole screen with `0xffffffff`, which named no colour
  and was drawn as white. A fill that names no colour now takes item 2, white
  until a title sets it, which is what the earlier titles, which set nothing,
  already got. Every rectangle call the route makes passes `0xffffffff` for the
  frame and 2 for the mode on the stack, and the clear is the one site that
  passes `0xffffffff` for the fill, with a null rectangle. A fill is drawn when
  the mode asks for one (2); a mode without that bit, an outline, leaves the
  inside as it is rather than painting it in the background item. The colour
  means the same whatever the rectangle, so a smaller rectangle filled with
  `0xffffffff` is the background over less of the screen.
- **A counted run of text.** The title draws `ne button game` with a count of
  14: fourteen halfwords and a zero one. The count was read as bytes and drew
  `ne butt`. A count is in the units the run is made of, and this title's
  generation makes its runs of halfwords; the 2005 module's are plain bytes and
  it passes no count, so a count from it stays a count of bytes.
- **Text measurement**, display `+0x0c`: `(display, font, run, -1)` with `-1`
  and a null pointer on the stack. One caller centres a line with the answer
  and draws at six pixels a character when it is zero. The other, at
  `0x10477c`, lays a help page out: it measures the page's wide copy, divides
  the answer by the page's byte length for the width of a character, and
  divides the width of a line by that. The trap's zero made the second
  division a division by zero, and the title's runtime reports one through a
  semihosting write (`svc 0xab`), which ended the frame as
  `unknown KTF native supervisor call 0xab`. The width is now the sum of the
  advances of the face the run is drawn in, over as much of the run as drawing
  it draws: a run past the 4 KiB bound, one with no terminator and one that
  reaches the end of guest memory are measured as far as they are read, not
  answered with zero. A control character inside a run inks nothing and takes a
  space's width, drawn and measured alike: the face gives it no width, which
  suits the descriptor package's padded buffers, but a native run ends at its
  terminator, and a run of control characters measured as nothing would be the
  same division by zero. A width limit and a count of what fits are honoured,
  the count in the run's units, though no caller has asked for either.
- **Interface `0x1002001`**, queried beside the screen and used for everything
  that is not a bitmap or text: `+0x10` a pen colour `(r, g, b)` with 255 on the
  stack, `+0x28` a point size of 1 or 2, `+0x30` a clip record (a byte 1, then
  x, y, width and height as halfwords from offset 4) whose answer one caller
  reads and draws only on a non-zero one, `+0x50` a point, `+0x54` a line and
  `+0x5c` a circle of radius 4 round the aim. The stars of the title screen are
  grey points, the fireworks are points of size 2 and lines, and the aim is the
  circle. A line is the WIPI C table's line and a circle its arc all the way
  round, both walked only inside the clip, and a point, at most as large as the
  screen's longer side, is filled a row at a time. The pen, point size and clip are part of a native checkpoint, as a
  pointer: a record taken before has none and still loads, starting the
  interface at its defaults.

<a id="ktf-native-settings-2026-10-20-validation"></a>

### Validation

From an empty save the title reaches its title screen, a tip, its main menu,
stage selection and the game, where the aim, rockets and score bar draw, in a
906-tick route with no slot left a trap. A copy of a debug save holding the
empty settings file runs at a 50 ms interval with the change and at 152 hours
without it. The open and migration tests fail on the previous open.

A review then found the first version's record unreadable to a quick save of
v0.6.0-pre, the migration in the open alone, and the rest above. A quick save
that `run -quicksave` wrote with v0.6.0-pre was refused by the first version as
damaged (`checkpoint record is missing fields`) and loads with this one. Each new test —
the calls agreeing, an emptied file across a quick load, the record without the
interface's state, the units of a count, the runs read as far as they go, the
mode, the leftover's removal, a run of control characters, a point past the
screen — fails on the version before it. Along the local test route the frames
end and draw as often as before and match it byte for byte from the first menu
on; on the title screen 180 pixels of the fireworks' lines differ, where the
shared line breaks a tie the other way.

The other native archives this file's earlier records describe are no longer in
the local corpus and were not run again; the 2005 module's file wrapper is the
evidence for its path, and `TestNativeFileCreatesOnWrite` follows that path.
