# ARM core implementation status

A pure-Go ARMv4T execution boundary: the interpreter the KTF and LGT guests
run on. It is an emulator for the code those archives contain rather than a
complete ARM system emulator — there are no exception modes, no banked
registers and no coprocessors, because nothing in a WIPI client uses them.

Three ARMv5T instructions are in it despite that: `BLX` immediate in both the
A32 and the Thumb encoding, and `BLX` register. LGT modules are Thumb-compiled
and use all three — the A32 and register forms to reach their own code and for
every indirect call, and the Thumb immediate pair for a Thumb caller's ARM
callee — so refusing them as "newer than ARMv4T" stopped every LGT title at its
first call. What the guests actually execute decides what belongs here, not the
architecture level the rest of the core was written to.

The Thumb immediate form shares its first halfword with `BL`: the prefix puts
the high half of the offset in `LR` either way, and only the suffix says which
state the callee is in. Its target is word aligned rather than halfword
aligned, so the low two bits of the computed sum are **dropped** rather than
carried into an ARM PC that would then fault on alignment.

A write to **CP15 register c7** — cache and write-buffer maintenance — is
accepted and does nothing. There is no cache here to flush, and every path that
changes a page's bytes already drops that page's decode cache, so
self-modifying code is coherent whether the guest asks or not. Modules issue it
right after copying code into RAM. Nothing else in CP15 is answered: the rest of
it reports what the hardware is, and inventing a value for an ID or control
register would be a wrong answer rather than a missing one.

## Execution and thread model

`internal/armcore` keeps shared guest memory in `Core` and a complete
`r0`-`r15` plus CPSR snapshot in each `Thread`. Only an instruction quantum is
serialized. When a guest reaches SVC, the core saves that thread's context and
releases the execution lock before invoking the runtime handler. The handler
may wait for a native or browser Host event while another guest thread runs.

Runtime handlers may also derive a nested function call from the suspended
context. The call applies the ARM argument layout (`r0`-`r3`, followed by a
bounded stack tail) to a temporary context, shares the parent logical thread's
registered thread-local guest words, and leaves the outer registers and stack
pointer intact when it returns. This is the execution shape required by the
AOT/JVM bridge.

The core can register selected aligned guest words as logical-thread state.
Guest load/store instructions then see the running thread's private value,
while Host memory inspection continues to see the shared backing word. KTF
uses this for its nominally global Java exception-handler head so two guest
threads suspended inside `try` regions cannot splice each other's handler
chains. Separate threads and nested calls are covered by native and race tests.

The default quantum is 1,000 instructions and the default per-run safety limit
is 1,000,000 instructions. Both are explicit `CoreOptions`, and cancellation is
checked between quanta. A thread can be externally modified only before it
starts or while it is suspended at SVC.

A thread may override the per-run ceiling with `SetStepBudget` and install a
`SetLimitHook` callback that fires when a run (or any call derived from that
thread) exhausts its budget window. Returning nil grants a fresh window and
execution resumes in place; the hook may block, which parks the goroutine with
the guest context saved — KTF builds its cooperative thread scheduler and
sliced session startup on this. Returning an error fails the run; without a
hook the run fails with `ErrStepLimit` as before. Derived call threads inherit
the parent's budget, hook, and thread-local state.

SVC currently stops directly at the runtime boundary with the instruction
immediate, address, and resume PC. It does not yet model the architectural
supervisor register bank or exception vector. This is sufficient for the KTF
Thumb stubs, which preserve their scratch register, place a service identifier
in `r12`, execute SVC, and continue with `bx lr` after the handler writes the
result registers.

## Memory

The guest address space is 32-bit, little-endian, sparse, and safe to reserve in
large ranges without allocating their backing bytes. Read, write, and execute
permissions are validated independently. Loader writes may initialize mapped
read-only code, while guest stores still require write permission.

Address overflow, unmapped access, permission failure, alignment, and the
number of distinct mappings are bounded and reported as typed errors. Newly
mapped storage reads as zero until it is committed by a loader or guest write.
Instruction fetches and direct Host memory helpers remain strictly aligned.
ARMv4T guest data loads rotate an unaligned word read from its containing
aligned word, word stores ignore address bits zero and one, and halfword
accesses ignore address bit zero.

**Two mappings may not overlap unless one permits everything the other does.**
Mapping is additive — a range mapped again with more permission is a remap, and
that stays legal — but a pair where each grants something the other withholds
is a layout mistake, and one that does not announce itself. Both mappings are
in force over the overlap, so an access there gets the union: a read-write
arena laid over a read-execute stub region has every write to it permitted, the
stubs underneath are overwritten by whatever the arena's owner stores, and the
first sign of it is a stub decoding into pixel data thousands of calls later.
KTF had exactly that pair — see `ktf.md`, "The arena and the stubs cannot share
an address" — so `Map` now refuses it and the mistake is an error at the call
that made it.

`CommittedRegions` reports the coalesced spans of committed pages carrying a
required permission, clipped to their mapping bounds. Memory scanners (the
cheat engine) sweep these instead of the mapped reservations.

`Core.Watch` records every store to an address with the instruction that made it
(`watch.go`). It was built for cheats — finding the address of a health bar is a
scan, finding the instruction that decrements it is a watch — and it answers a
second kind of question just as well: **what wrote over a structure this
platform handed the guest.** One LGT defect that had survived three passes of
reasoning was named by one run with a watch on the word that kept changing; see
`docs/lgt.md`, "A class's statics live in its class object". Reach for it before
reasoning about who could have written something.

### Not every store is a guest instruction

The watch used to record guest stores only, and that made it wrong rather than
incomplete on exactly the addresses worth watching. This platform writes into
the guest's own address space continuously: a Java object's fields are guest
words the runtime keeps in step, a supervisor call servicing `memcpy` writes the
destination itself, an image blit and a published frame land the same way. None
of those is an emulated instruction, so an address the host rewrote every tick
answered **no writers at all** — which does not read as "the instrumentation
does not cover this", it reads as "nothing touches this", and that is an answer
that ends an investigation instead of starting one.

So `Memory.Write` is instrumented too and every hit carries a `WriteOrigin`:

- **guest** — an emulated store. Its PC names the instruction exactly.
- **host** — a store by this platform. Its PC is the last guest instruction to
  have run, which is worth two very different amounts. Reached from inside a
  supervisor call it names the guest instruction that made the call, which is
  precisely the caller wanted. Reached from a host path the guest never entered
  — a field synchronised between ticks, a frame published at a tick boundary —
  it names wherever the guest happened to stop, and means nothing. The reader
  has to be told which, so the origin travels with the hit and the console
  prints `host, last pc` rather than presenting an address to disassemble.

A guest and a host writer at the same PC stay separate hits, because they are
separate facts and folding them together would hide whichever arrived second
behind the other's count.

Walking the watch list rather than the written run is what keeps this affordable:
a frame published into guest memory is a hundred kilobytes and the watched
addresses are a handful, so a host write costs one span test and then one pass
over the watches — never one pass over the bytes.

`Memory.WriteUntracked` is the deliberate hole in it, and it has exactly one
caller: the cheat engine, which rewrites its frozen values every tick and pokes
addresses on command. Recording those would bury the writer a user is hunting
under the user's own tooling, and would report a value as written by something
in the game when nothing in the game is writing it. Both ARM platforms route
`cheatTarget.WriteMemory` through it; nothing else has any business doing so.

How much this was hiding, measured on two real titles rather than argued:
watching three words of a KTF title's platform arena reported `no writes
recorded yet` before and three host writers after, and the same watch across an
LGT Clet's arena now reports eight, several of them landing `0xef5def5d` — one
RGB565 colour twice over, which is the frame being published into guest memory.
Every one of those addresses was being written several times a second the whole
time, and the tool said nothing was writing them.

`Thread.SetContext` replaces the whole guest-visible state, which is what a long
jump is: the condition flags and the instruction set of the saved point come
back with the registers, and a handler that restores a context register by
register leaves those behind. LGT's Java exceptions are built on it.

## What the guests actually spend their instructions on

The library hooks in `ktf.md`, "Recognising the C runtime in the image", answer
`memcpy`, `memset`, `strlen` and `strcpy` natively because interpreting a
byte-at-a-time fill is the worst thing this emulator can be asked to do. The
obvious next step is the same treatment for copy loops the compiler *inlined*,
which have no entry point to hook. That was measured before it was built, and
the measurement says not to build it.

Two titles profiled to the instruction, in a real scene rather than at a menu:

| share | what the hot range is |
|---|---|
| 54.5% | a halfword fill — the engine's own rectangle fill |
| 36.1% | 4-bit pixels unpacked through a palette into a 16-bit surface |
| 43.8% | packed pixels through a palette with a transparent-colour key |
| 21.9% | another halfword fill, written as pairs of byte stores |
| 16.9% | an RGB565 alpha blend |

The first two are one title and the last three another; in both, two ranges
account for over 90% and 65% of all guest execution respectively. A third
title's hottest range is an AOT Java sprite draw instead. **Not one of them is
a copy loop.** Every one is the game's own rasteriser.

Then the specific question, asked of the whole local corpus rather than of a
profile. Scanning all 32 KTF images byte-exactly for the two inline-copy shapes
worth hooking — a register-form `ldrb`/`strb` loop advancing both pointers, and
the stack-form an unoptimised build emits with `dst`, `src` and `len` all in
frame slots:

- register form: **3 sites, in 2 of 32 images**
- stack form: **0 sites in 32 images**

A looser sweep that only asks for a load, a store and both pointers advancing
inside a short backward branch finds 8 copy-shaped and 34 fill-shaped loops
across roughly ten million decoded instructions. And the one register-form site
inside a title profiled deeply **was never sampled at all** — zero share, in a
run of eight billion instructions.

So a pattern engine for those two shapes would be machinery for something the
corpus barely contains and never runs hot. What it would have to match instead
is engine code, and that does not generalise either: the two halfword fills
above are the same operation in two titles and share no instruction sequence —
one counts down a register and stores halfwords, the other walks a destination
pointer against an end pointer storing bytes in pairs from a fixed source. A
pattern that matched both would have to be a loop recogniser rather than a byte
pattern.

**Beware the tool before the conclusion.** The first version of that corpus
sweep reported zero copy loops everywhere, which looked like a clean answer and
was an artifact: the disassembler stops at the first halfword it cannot decode,
so it had read 822 instructions of the 140,000 in the first image and every
image after was the same silence. A survey that reports "none" has to be made
to report a known-present case first.

## Interpreter throughput

Two things dominated a step, and neither was the instruction itself.

The memory lock was taken and released once per guest access — per instruction
fetch, per load, per store — while nothing was ever contending it. It now moves
out to the quantum: `Engine.Run` takes it across the instructions it runs and
the guest accessors document that their caller holds it. Nothing loses the
protection, because `Core` already serializes engine runs behind its execute
mutex and supervisor-call handlers still run after the quantum returns, outside
the lock. The lock is exclusive rather than shared because a guest store
commits pages, which a read lock cannot be upgraded to do.

Decoding then repeated per execution: a hot loop of a thousand iterations
walked the same match tree a thousand times and re-read the same bytes out of
guest memory to do it. `Memory` now caches the classified instruction per
address, in an array committed lazily per page and indexed directly, so a
repeat costs an index instead of a mapping scan, a page lookup, and a walk down
the tree. `classifyThumb` answers the form and `executeThumbForm` runs it.

The cache is only as safe as its invalidation, and this platform really does
rewrite code: the KTF loader relocates itself and patches SVC stubs over pages
it has already executed. Every path that changes a page's bytes drops that
page's entries, a remap retires all of them, and a page that a mapping covers
only partly is never cached at all — a cached page answers later addresses
without re-checking permission, so its tail would otherwise execute as code.
`decode_cache_test.go` covers all three.

Guest loads and stores then had their own version of the same problem. Every
access re-scanned the mapping list and looked the page up in a map, then copied
the bytes through a temporary array — a four-byte load was an array, a copy,
four byte loads, three shifts, and three ors. Three things fixed it. The last
mapping that satisfied a validation and the last page touched are remembered,
because guest code stays inside one region for long runs, so both lookups
become a compare. Aligned accesses that no thread-local word overlaps then read
and write the page's bytes where they are. And the thread-local splice walks
words rather than bytes, behind a bounds check on the registered span, so the
ordinary access never consults the map at all.

Both caches are keyed by address, so the hazard is one region answering for
another; `decode_cache_test.go` pins that an address past a cached mapping
still faults, a read-only region is not made writable by a cached read-write
one, and a second page does not return the first page's bytes.

What was left then was finding the page at all. Pages lived in a Go map, and
an instruction fetch looked up that map every instruction. Fetch now goes
through its own cached page — held separately from the data one, so a loop
reading data does not evict the page it executes from — and the map itself is
gone, replaced by a two-level table indexed by the top and middle bits of the
address. Both lookups are array reads now.

That last pair was the largest win of the whole exercise on real games and
almost invisible on the benchmarks, which is worth remembering: a benchmark's
page map holds two entries and stays in L1, while a game maps thousands and
every lookup is a hash into cold memory. Benchmark on the shape of code that
actually runs — `BenchmarkEngineGameShapedLoop` does, being three immediates,
one halfword load, and one conditional branch, which is 97% of what a
measured KTF title executes.

Measured on an M1, per instruction, native:

| | before | after |
|---|---|---|
| ALU loop | 16.9ns (59 MIPS) | **8.8ns (114)** |
| load/store loop | 28.1ns (36 MIPS) | **11.2ns (89)** |
| game-shaped loop | — | **9.0ns (111)** |

Real KTF game frames in wasm, against the frame time the game itself asks for.
Under 1.0x is the emulator keeping up:

| | asks for | before | after |
|---|---|---|---|
| title A | 50ms | 25.1ms (0.50x) | **9.1ms (0.18x)** |
| title B | 44.8ms | 94.4ms (**2.11x**) | **30.8ms (0.69x)** |

### What is left, and two shapes of cache that do not get it

The numbers above are a benchmark's. A real title's load, measured on a manual
clock so the game's own pacing is not in the answer, retires 731M instructions
in 6.4 seconds of host CPU — **8.7ns an instruction against the game-shaped
benchmark's 5.1**. Finding what the difference is took a fourth benchmark.

`BenchmarkEngineBlitCrossPage` streams: a halfword out of one buffer, into
another, both pointers advancing. That is what every sprite blit, tile draw and
screen clear here is made of, and the three older benchmarks between them do
not have it — the game-shaped loop's one load hits the same address every
iteration. Reading the two against each other separates the two costs:

| | per instruction |
|---|---|
| an ALU instruction | **~3.5ns** |
| an aligned load or store | **~11ns** |

So the transfer path is where the remaining time is, and it is worth about
three ALU instructions. The blit also says where a transfer's extra cost comes
from: the same loop with source and destination inside one page runs at 5.2ns
and across two pages at 7.2ns. One cached data page cannot hold a blit.

**Two caches that should have fixed that were built and both lost.**

*A second cached page*, checked only after the first misses, and outlined so
that `mappedPage` — which is inlined into every aligned load and store — keeps
its body. It is worth **25%** on the cross-page blit and nothing on the other
three benchmarks. On real titles it is **3 to 5% slower**, every one of them.
Instrumented on a real load, the answer is plain: 95.4M accesses miss the first
entry and the second entry catches 10.6M of them, so 89% of misses pay an extra
call for nothing.

*A direct-mapped set of sixteen*, indexed by the page number, which should cost
the same one compare and hold a working set. It costs more than that: the array
index pushes `mappedPage` past the inline budget, and losing that inlining is
worth more than every hit the set adds — 17% slower on the load/store loop, 15%
on the blit, 3% on game-shaped code.

**The fast path's inlining is the constraint, and it is worth more than hit
rate.** Anything tried here has to leave `mappedPage` inlining, which
`go build -gcflags=-m` reports, and has to be measured on a real archive
through `TestLoadCostProbe` rather than on the benchmarks alone — the
cross-page blit and the local titles disagreed by 30 percentage points about
the first of these.

What that leaves: 13% of a real load's instructions miss the cached page, and
what a miss costs — a validation, a two-level page walk, and the remembering —
is the thing to make cheaper, rather than the number of things remembered.

### The permission moved onto the page, and a miss got a sixth cheaper

Counting *why* misses happen answered which half of a miss to attack. Across
local titles, **the mapping is what changed, not the page**: on one archive
99.6% of the accesses reaching the general path were there because the
remembered mapping did not cover them, on another 57%, and on both of them the
share that were the *same* page as the last access was under a tenth of a
percent. Guest code alternates between regions — a module's constants and a
heap buffer, a stack frame and a framebuffer — and one remembered mapping
cannot hold that. Every one of those misses paid a scan of the mapping list.

So the permission moved onto the page. `memoryPage.permission` is what the
mappings allow across *the whole* of that page, filled the first time the
general path touches it, and it stands in for the mapping test entirely: an
aligned access of at most a word cannot leave the page it starts in, so a page
that permits the operation permits the access. A page a mapping only partly
covers keeps zero and always takes the general path, where the mapping list
still answers per byte. `Map` is the only thing that changes a mapping, and it
already walks every page to retire decode caches, so forgetting the remembered
permissions costs nothing new.

The fast path lost three compares and gained two, and still inlines. The miss
path lost the scan: it walks two arrays, reads the permission out of the page
it was going to read anyway, and answers.

Measured on 24 local LGT archives, sixty ticks each, best of three:

| | before | after |
|---|---|---|
| the heaviest local title | 3.63s | **3.03s** (-17%) |
| the one after it | 3.22s | **2.60s** (-19%) |
| a title that boots slowly | 2.72s | **2.42s** (-11%) |
| the largest share of all | 1.10s | **0.87s** (-21%) |
| the median archive | — | **-5%** |

The gain tracks how much of a run is guest ARM: the heaviest quarter give
between 11% and 21%, the middle 5%, and the lightest — sixty ticks that are
mostly loading — do not move at all. Nothing measured slower than noise. KTF
titles are within noise either way, which is what a platform whose time goes to
the JVM rather than to guest ARM should do. The synthetic benchmarks agree that
this is a miss-path change and not a fast-path one: the cross-page blit gains
3% and the other four are unchanged.

An upper bound was taken first, by deleting the validation entirely in a
throwaway build — 12% and 16% on the two heaviest titles. The page permission
gets 11% and 17% of that, so what is left in the mapping list is nothing.

What is still there is **the walk**: two array reads and the remembering, on
every access that changes page. That is the remaining half of a miss.

### Two more shapes for the walk, both measured and both worse

Both were A/B'd the way this section says to: 29 local LGT archives, sixty
ticks each, best of three, against a baseline run twice so the noise floor is
in the same table. That floor is about **1%** per title.

*Remembering the directory entry as well as the page*, so a miss inside the
same four-megabyte region skips the first of the two array reads. It is **0.8%
slower overall** and 2% slower on six of the mid-weight titles, outside the
floor and in the same direction every time. The reason is the one the page
permission already established: a miss is a *region* change, not a page change
— the guest alternates between a module's constants and a heap buffer — so the
directory entry it walks to is a different one almost every time, and the
compare and the two stores are paid for nothing.

*Bigger pages*, at 16KB instead of 4KB, which should make a streaming blit
change page a quarter as often. **7.8% slower overall**, and one title 43%
slower on its own. `memoryPage.permission` is why: it is only filled when the
mappings cover *the whole* page, and at 16KB many more pages are covered only
partly, so those pages never get the fast path at all. Page size is coupled to
the mapping granularity a platform lays out, and 4KB is what the local ones
fit.

So the walk is not where the next win is either, and the three designs tried
against it now agree on the same thing: **what a miss costs is not the number
of memory reads in it.** Anything tried here next should start by measuring
where a miss's time actually goes, rather than by removing a load from it.

Two measurements to distrust when working here. The macOS Go CPU profiler
attributed 88% of a run to `runtime.pthread_cond_signal` under `Gosched`;
raising the quantum a thousandfold changed the time by 5%, so that profile was
noise. An isolated benchmark of `RLock`/`RUnlock` reported 13.8ns, which would
have made the lock 82% of a step; hoisting it out of the loop actually returned
11%, because a dependent loop over one cache line exaggerates what an
uncontended atomic costs inside real work. Both numbers pointed at the right
code for the wrong reason. A/B the whole benchmark instead.

### Where a Clet's time goes, and the quantum that is not one number

The instruction from the three failed walk designs was to measure where the
time goes rather than to remove a load from a miss. Doing that on this platform
first needs the profiler to be usable, and on macOS it is not: a Go CPU profile
of an LGT title attributes **79% of the run to `runtime.pthread_cond_signal`**
under `runtime.goschedImpl`, which is the `Gosched` the engine makes at the end
of every quantum and not work at all. Raise the quantum a thousandfold and the
same profile reads:

| | share |
|---|---|
| `Engine.Run` itself (dispatch, fetch, the switch) | 24% flat, 92% cumulative |
| `decodedThumbFast` | 9% |
| the transfer path: `directAccess`, `pageAt`, `mappedPage`, `read8/16/32` | ~25% cumulative |
| the ALU: `executeThumbALU`, `shiftImmediate`, `setNZCV`, `setNZC` | ~17% |

So there is no platform work in the answer — **it is all interpretation**. The
whole-surface copies a draw slot makes either side of itself
(`syncFromGuest`/`syncToGuest`) do not appear in the profile at all, which is
worth knowing before optimising them: a heavy title's frame is thousands of
guest instructions per drawn pixel, not a copy.

That leaves the quantum itself, and measuring it turned up a rule worth keeping.
**Host time per tick is not a throughput measurement here.** Both platforms
advance their guest clock with the work the guest does, so a change to the
quantum changes how much work a tick contains and the tick time moves for two
reasons at once. Held against the instruction count — which comes out
bit-identical across the arms on both platforms, so the comparison is exact —
the picture is clean and it is *platform-dependent*:

| quantum 1,000 → 16,384 | ns per instruction |
|---|---|
| four LGT titles | −6.2%, −7.1%, −5.0%, −1.7% |
| three KTF titles | +12%, +4.2%, +3.0% |

Every LGT title gains and every KTF title loses, so the quantum is not one
number: `internal/platform/lgt` passes 16,384 to its `Core` and the default
stays at 1,000. Why the platforms differ is not settled — a Clet runs long
stretches between platform calls while a KTF title crosses the Java bridge
constantly, so the two pay for a yield at very different rates — and the
measurement is what the choice rests on rather than that reading.

`TestLGTLoadCostProbe` is where this is measured: `WFEATURE_PERF_ARCHIVE` and
`WFEATURE_LOAD_TICKS`, reporting `ns_per_step` beside the tick time, and it is
the thing to run under `-cpuprofile` next time.

**A probe that measures cost must not call `TickFor`.** Its answer is how long
the Host should *wait* before the next tick, not what this one cost, and an
emulator running comfortably ahead of real time reports most of a tick as
wait. Read as a cost that made this title's worst tick look like three to six
seconds of world loading; measured properly around `Tick` it is **80ms**, and
the whole run got 27% cheaper once the idle came out of it. Nothing was slow —
the probe was waiting.

### Straight-line blocks were built and lost, six ways

The guest profile said the working set is a kilobyte and mostly straight-line,
which is the shape a block interpreter is for: decode a run of instructions
that cannot write PC, then execute them without going back through the loop
for each one — no PC read, no Thumb test, no `end` compare, and an increment
where `decodedThumbFast` was a lookup. The projection was 1.4 to 1.6x. **It is
worth 5 to 9% on the code it targets and costs 1 to 10% on everything else,
and every arrangement tried lands in the same place.**

The measurements, in the order they were taken, each A/B'd against the same
build with blocks compiled out, on real archives by `ns_per_step`:

| arrangement | result |
|---|---|
| block loop delegating to a shared per-instruction runner | 6–11% **slower** than no block at all |
| switch written out inside the block, form tested by a helper | still slower: the helper inlines *as a switch*, so every instruction dispatched twice |
| form tested by a `[256]bool` — no bounds check, no second switch | two heavy titles −7%/−9%, two light ones +3%/+5% |
| the whole block loop inlined into `Engine.Run` | 4–5% slower again: the hot loop grew and the ordinary path paid for it |
| one cache lookup serving both paths (the best of them) | LGT mean **−0.4%**; heavy titles −5.8%/−8.2%; one title +4.7% |
| plus refusing blocks shorter than four instructions | mean back to +0.4%, and the worst regression unmoved |

Three things that arrangement taught, which outlast it:

- **A block is short because of `mov ip, rN`.** RVCT spills through r12, so a
  title's inner pixel loop carries three or four high-register moves, and
  excluding that form — it *can* write PC — cut the average block from about
  thirty instructions to 8.5. Splitting the form at decode time by whether its
  destination is r15 doubled blocks to 15.6 and turned two titles positive.
  Deciding it at decode time is what makes it free.
- **The loss is not block entry.** Refusing short blocks did not move the worst
  regression, so what costs is the restructured loop itself: carrying a table
  pointer and an index through `Engine.Run` is register pressure the ordinary
  path pays on every instruction, and most titles are mostly ordinary path.
- **Which titles win is a property of code shape, not of platform.** The two
  LGT titles with fifteen-instruction runs win 6 to 9%; so does the one heavy
  KTF title. The titles whose loops branch every fifth instruction lose, on
  both platforms, and the worst of them — a KTF title at **+9.5%** — is the
  reason this is not shipped behind a platform switch either.

What it says about the next attempt: the dispatch overhead a block removes is
worth about 5 to 9%, and that is the whole of what this design can reach. The
rest of `Engine.Run`'s 24% is operand extraction inside the handlers — every
one re-derives its register numbers and immediates from the raw halfword on
every execution — and reaching that means widening the decode cache entry from
its four bytes to pre-extracted fields and rewriting the handlers to take them.
That is a different and much larger change, and it is where the projected 1.4x
actually lives. It also triples the decode cache's memory per page, which is
the first thing to measure about it rather than the last.

### A wider decode cache entry was built and lost, twice over

That next attempt was built. **The 1.4x is not there, and the design loses on
both of the halves it was made of.**

The memory question the section above says to measure first turned out not to
be the question at all. `Memory.DecodeCacheStats` reports how many pages hold a
committed cache and what they cost, and on real archives it is **15 to 63
pages, 0.1 to 0.5 MiB** — three LGT titles and two KTF ones. Tripling that
buys a megabyte. Nothing was ever going to be decided on those grounds.

What decides it is throughput, measured by `ns_per_step` over 600 ticks, three
runs a side, on the same machine against a tree with the change reverted. Two
titles carry the signal because their time is mostly stepping; two others sit
near 190 and 240 ns a step, where the host dominates and the interpreter barely
shows.

| title | four-byte entry | twelve-byte entry, handlers unchanged | twelve-byte entry, handlers reading it |
|---|---|---|---|
| one LGT title | 6.28 | 6.87 (**+9.4%**) | 7.49 (**+19.3%**) |
| another | 10.91 | 11.68 (**+7.0%**) | 12.30 (**+12.7%**) |
| a third | 55.3 | — | 56.8 (+2.7%) |
| a fourth | 189.4 | — | 190.0 (+0.3%) |
| a fifth | 242.6 | — | 241.8 (−0.3%) |

The middle column is the whole point of the experiment. It is the *same*
handlers as the baseline, reading the same raw halfword; the only difference is
eight bytes of padding in the entry. That alone costs 7 to 9%, which is the
decode array's footprint over a hot loop growing threefold and pushing the loop
out of the cache it was fitting in.

The right-hand column adds the rest of the design — registers, scaled offsets,
sub-selectors and resolved branch targets all pulled out at decode time, with
the ten routed handlers rewritten to take them — and it costs a further 9 to
10%. **Reading a pre-extracted field is more expensive than recomputing it.**
The halfword is already in a register because the entry was loaded anyway, and
a shift-and-mask on it is an ALU operation an out-of-order core hides behind
the load it is waiting on; a pre-extracted byte is another load, another
zero-extend, and — because a `uint8` gives the compiler no bound — another mask
to keep the register file's index provable. Nothing was saved and a load was
added.

So the two halves cannot be traded against each other: packing the entry
tighter would at best recover the width penalty and leave the extraction one,
and the extraction penalty was measured at equal width. The design is closed,
and with it the 24% that motivated it — that share is not a cost the entry can
carry somewhere cheaper, it is the arithmetic the instructions themselves
consist of.

`Memory.DecodeCacheStats` stays, and both platforms' load probes report it.
It answered its question in one run and is what any future claim about the
cache's memory has to start from.

### Both of those were about Thumb, and the slow titles are not Thumb

Everything above — the decode cache, the routed switch, the blocks, the wider
entry — is on the **Thumb** path. `Engine.Run` has two halves and they are not
comparable: Thumb reads a cached decode and routes ten forms straight to their
handler; ARM calls `fetch32` and then walks `executeARM`'s chain of mask-and-
compare tests from the top, on **every instruction, every time**. There is no
ARM decode cache at all.

Which path a title takes is not a detail. Counting steps by instruction set
over a real run of three local LGT archives:

| title | Thumb | ARM |
|---|---|---|
| a Clet | 99.8% | 0.2% |
| a Java title | 38.1% | 61.9% |
| the Java title with the reported stutter | 21.1% | **78.9%** |

**A Clet is Thumb and an ahead-of-time-compiled Java title is ARM**, which is
the missing half of "a Java title is the other shape, and it is the slow one"
(`lgt.md`). The Clets that run at five to six times the guest clock are the
ones every optimisation above was measured on; the title that manages twice the
clock spends four fifths of its instructions on the path none of them touched.

What that path costs, on the same machine as every number above (`-benchtime 2s`,
Apple M1):

| benchmark | ns/step |
|---|---|
| `BenchmarkEngineALULoop` — a whole Thumb step | 4.64 |
| `BenchmarkThumbExecuteOnly` — the same without the fetch | 4.70 |
| `BenchmarkFetchOnly` — `fetch16` alone | 8.16 |
| `BenchmarkEngineARMALULoop` — a whole ARM step | **17.03** |
| `BenchmarkARMExecuteOnly` — the match chain and the operation | 8.78 |
| `BenchmarkARMFetchOnly` — `fetch32` alone | 8.18 |
| `BenchmarkARMDataProcessingOnly` — the operation alone | 4.65 |

**The ARM step decomposes exactly**, which is what makes it worth acting on:

```
fetch 8.18  +  match chain 4.13  +  operation 4.65  =  16.96 ≈ 17.03
```

and the two facts that follow are the whole design:

- **A whole Thumb step is cheaper than a single fetch** — 4.64 against 8.16.
  That is the decode cache paying for itself: it removes the fetch, and the
  fetch is the most expensive part of an instruction.
- **An ARM operation alone costs what an entire Thumb step costs**, 4.65
  against 4.64. The arithmetic is not the problem. Everything ARM pays over
  Thumb is fetch and dispatch — 12.3ns of the 17.03 — and both are exactly what
  the Thumb path already removed.

So the cheapest thing left *inside the interpreter* is *the ARM decode cache,
with the form stored in the entry so the chain becomes a routed switch* — the
one mechanism here with a measured precedent rather than a projection, the same
change on the same engine, on the other half of the same loop. It should land
an ARM step near a Thumb one, since after it the two are the same three things
in the same order: a cached decode, a switch, and the operation. That is ~3.4x
on ARM steps and, on a title that is 78.9% ARM against 21.1% Thumb, about 2.9x
on **interpreted** time:

```
now    0.789 × 17.03 + 0.211 × 4.64 = 14.4 ns a step
after  0.789 ×  5.0  + 0.211 × 4.64 =  4.9 ns a step
```

### And none of that was worth doing, because the interpreter was 4% of the run

**This is the correction that matters most in this document.** Everything
above — the mix, the benchmarks, the projection — is about what an instruction
costs. It says nothing about how much of the emulator's time goes on
instructions, and on the title the whole exercise started from, the answer was
**4.2%**.

A host CPU profile of that title's in-game session, taken through the load
probe's `WFEATURE_PERF_ROUTE`:

| | share of host time |
|---|---|
| `lgt.(*Client).syncToGuest` | 29.0% |
| `lgt.(*Client).syncFromGuest` | 26.3% |
| `runtime.kevent` — parking and waking guest threads | 16.5% |
| `armcore.Engine.Run`, everything under it | 4.2% |
| of which `fetch32`, the part a decode cache removes | 1.4% |

The ARM decode cache's ~48% of an ARM step is 48% of 4.2%. **The whole change
was worth two per cent of the run**, and the two framebuffer syncs — which
`lgt.md` shows were copying 150 KiB in each direction around every drawing call
a Java title made, and were measured over 1.7 million calls to have never once
found a changed pixel — were worth two thirds of it. Removing them took the
same session from 366.6 to 41.0 nanoseconds a step, 150.9s to 16.9s, with every
frame byte-identical.

So the ARM cache is still the right *next* interpreter change and the numbers
above still hold for what it would do; it is simply not what the title needed,
and the reason is the whole lesson:

**A guest profile and a host profile answer different questions, and this
project had been reading one for the other.** `-profile` says which guest
instructions a title executes, which is how its own hot loops were found. Where
the *emulator's* time goes is a Go profile of a run driven to a scene, and it
took one to find that 55% of it was a copy nobody needed. Ask for the host
profile first, before any throughput reasoning at all: the fastest interpreter
in the world would have moved this title by four per cent.

The two failures above do not argue against it. Both were attempts to make an
*already cached* path faster — widening an entry that existed, blocking a loop
that already skipped its fetch — and both lost to cache footprint and register
pressure. Adding the first cache to a path that has none is the change those
two were competing against, not a repeat of them.

What to measure, in order, before believing any of it:

1. the ARM entry's footprint, from `Memory.DecodeCacheStats` — an ARM module
   is a megabyte where a Clet is a kilobyte of hot code, and the Thumb cache's
   0.1–0.5 MiB is not a prediction for it;
2. `ns_per_step` on the two ARM-heavy titles and, as the regression guard, on
   the Clets that are 99.8% Thumb and must not move;
3. only then the routed switch for ARM's ten commonest forms, which is the
   second half of what Thumb has and worth its own A/B.

### Why a translator is not the answer even with unlimited effort

Asked plainly — ignoring how much work each is — a translator still loses, and
the numbers above are why. After the cache and the switch an ARM step is about
5ns, of which **4.65 is the operation itself**. Everything a translator could
still remove is the ~0.4ns of dispatch left over. The larger prize it is
usually reached for — specialising operands at translate time — is the design
measured in "A wider decode cache entry was built and lost, twice over", where
reading a pre-extracted field came out *slower* than recomputing it from the
raw word, at equal width, by 9 to 10%.

And how this ships decides the rest. A translator that emits machine code and
marks a page executable is out on its own terms: `make dist` cross-compiles the
server to five operating systems from one machine with no cgo, which is a
constraint on the project rather than on the design (`AGENTS.md`). The portable
form, a closure per instruction, pays one indirect call per guest instruction —
measured here at 0.9ns natively — against the ~0.4ns of dispatch it would be
removing. It loses before it starts.

(The browser is not the constraint it would once have been: the page ran the
emulator in wasm once and no longer does — the emulator runs natively on a
server and the phone is a thin client, see `architecture.md`. The wasm
measurements further down are that era's, and they are kept because what they
found about Go's wasm backend is still true of the client.)

So a translator is worth at most a rounding error. The throughput left after
the ARM cache is in the operations themselves and in the 11.8% a Java title
spends in call thunks — not in dispatch, which will by then have been taken
twice.

## Why the browser was seven times slower

The same Thumb loop cost 9.0ns an instruction natively and 63.5ns in wasm.
That gap was the largest single thing left, and it applied to every game.

The obvious suspect was wrong. Go on wasm passes arguments and results through
linear memory rather than registers, and wraps every call site in resume
machinery, so a call there should be dear — but measured, an empty call cost
4.2ns against 0.9ns natively, and removing one call per instruction from the
interpreter returned the same half-nanosecond on both. Calls are not what wasm
is bad at. What it is bad at is *volume*: a V8 profile of the benchmark showed
no runtime overhead at all, only the interpreter's own functions, and the wasm
each one compiles to runs roughly one operation per cycle. A guest instruction
was around 200 wasm operations where native ARM64 was 29.

So the work was to emit fewer operations, and two patterns accounted for most
of them.

`Context.PC`, `Thumb`, `carry`, and `Register` had **value receivers**. `Context`
is sixteen registers and a CPSR — 68 bytes — and Go does not decompose an array
that size, so each of those calls copied the whole thing even when inlined, to
read one word out of the copy. The interpreter loop called two of them per
instruction and the data-processing forms called a third. Pointer receivers cut
the ALU loop nearly in half on both backends.

The flag setters then each read CPSR, masked it, and wrote it back, so one
`ADD` that sets N, Z, C, and V made four round trips through the context
pointer to produce one value. `setNZCV` and `setNZC` compute it once and store
once. Natively that is a few cheap instructions either way; on wasm the address
of a pointer field is computed in 64-bit arithmetic and narrowed to 32 for
every access, so each round trip is five operations rather than one.

The rest was reaching the general path for answers that are three compares.
`decodedThumbFast` and `mappedPage` are deliberately small enough to inline —
every condition they check is one the general path checks too, so a negative
answer only ever means "ask the general path", and the general path is unchanged.
The decode cache became a `*[decodedPerPage]decodedThumb` rather than a slice so
that a page offset, masked and halved, is provably inside it: the type carries
the bound the bounds check was re-deriving per instruction.

A report's `paints_dropped` counts **rounds that ran with their paint
suppressed, not frames lost.** A guest that spins rather than parking runs tens
of thousands of rounds a second, so the count inflates without the screen
necessarily losing anything; `paint_load` beside it is what says whether the
Host was actually behind.

`SetDecodeCacheEnabled` turns that cache off, and `DecodeCacheEnabled` reports
whether it is on. It is a diagnostic switch rather than a tuning knob: the
"is the browser thrashing its working set" hypothesis is tested by running
without the cache, and the answer was no — without it the browser was twice as
slow again, and so was the desktop. The switch stays so that measurement is
repeatable instead of remembered, and the uncached path still checks execute
permission, so what it measures is the same interpreter. It is reached from Go
only — the browser export that flipped it went with the in-page engine, and the
machine whose working set was in question no longer runs the interpreter at
all.

Per instruction, after:

| | native before | native | wasm before | wasm |
|---|---|---|---|---|
| ALU loop | 8.8ns (114 MIPS) | **4.7ns (213)** | 66.6ns (15) | **27.6ns (36)** |
| load/store loop | 11.2ns (90) | **6.1ns (163)** | 67.0ns (15) | **37.5ns (27)** |
| game-shaped loop | 9.0ns (111) | **4.9ns (206)** | 60.5ns (17) | **27.8ns (36)** |

wasm is now 5.7x native rather than 6.7x, and — what actually matters — the
browser runs guest code 2.2x faster than it did. On real games the share is
proportional to how much of a workload is guest ARM at all: rendering the first
frame of every local archive — eighteen of them at the time, and loading and JVM
work as much as interpretation — went from 17.1s to 12.1s in wasm and 3.7s to
2.9s natively.

None of the above was visible from the Go side. The wasm numbers were taken by
building the test binary, running it under node's profiler, and aggregating
self time per frame — the emulator no longer ships to the browser, but the
recipe is what the measurements rest on:

```
GOOS=js GOARCH=wasm go test -c -o /tmp/armcore.wasm ./internal/armcore
node --cpu-prof --cpu-prof-dir=/tmp/prof "$(go env GOROOT)/lib/wasm/wasm_exec_node.js" \
  /tmp/armcore.wasm -test.run='^$' -test.bench=BenchmarkEngineGameShapedLoop
```

`GOOS=js GOARCH=wasm go build -gcflags=-S` prints the wasm each line compiles
to, which is how the 64-bit address arithmetic and the per-call spills were
found; `-gcflags=-m=2` reports why a function missed the 80-node inline budget.

### What was tried after that, and did not work

The remaining gap is not uniform: pure arithmetic in wasm runs at native speed,
an array-and-branch loop at 2.5x, and this interpreter at 6x. The bottleneck is
linear-memory traffic rather than the arithmetic, and **the 6x does not close by
degrees** — three separate attempts returned between zero and four percent.
Each of these was measured and is not worth repeating:

- **`page.data` as a `*[4096]byte` to remove every bounds check.** The
  generated code was inspected first and already had *no* bounds checks left to
  remove, so the change bought nothing.
- **Masking the page offset to induce bounds-check elimination**, for the same
  reason.
- **A four-way associative mapping cache in the core.** The one-entry cache
  already answers nearly every access.
- **`GOGC=400`.** Not where the time goes.
- **Turning the decode cache off**, which is a diagnostic switch and not a
  tuning one: the desktop went from 5.26x to 2.42x slower, in the wrong
  direction.

What would close it is translating guest basic blocks to wasm, which is a
project of its own. The requirement that made browser speed urgent — running on
a phone — is met by the server session instead, so that project only becomes
worth starting if running entirely inside a desktop browser becomes a
requirement.

## Guest sampling profiler

`Core.EnableProfile(interval)` records where guest code actually spends its
instructions. It is off unless switched on: the cost is a stack walk, and a
run that is not being investigated should not pay it.

Two decisions make the numbers mean what they say.

Samples are taken per executed instruction, not per quantum. Quanta end on
every supervisor call, so a game that calls into the Host constantly would
otherwise be sampled hundreds of times more often than one that does not, at
the same instruction count.

More importantly, the quantum is *shortened* to land exactly on the sample
interval. Without that the sample count is proportional but the sampled address
is not: execution stops at supervisor calls, so the recorded PC is nearly always
the instruction after one. Measured on one title, that artefact put 37.9% of the
profile inside the platform stub region at `0x31000000`; ending the quantum on
the interval instead drops the same region to 0.8% and moves the weight onto the
game's own sprite blit, which is where the instructions were being spent all
along.

Each sample is the PC plus the return addresses above it, walked through the
Thumb r7 frame chain (`[r7]` is the caller's r7, `[r7+4]` its return address).
The walk is best effort — a leaf that has not built its frame yet, or hand
written assembly that keeps none, yields a shorter stack — and stops on a
return address without the Thumb bit, on a chain that fails to walk upwards,
and at 32 frames. The leaf PC is always right, which is what the
instruction-level ranking needs.

Distinct stacks are capped at 65536. Past the cap a sample is cut back to its
leaf PC and still counted, so a game with unbounded stack shapes loses its
callers rather than its samples; `Profile.CallersDropped` reports how many.

`ResetProfile` clears the samples and keeps sampling, which is how a profile
covers a scene rather than the minutes of loading that preceded it.

## Implemented instruction boundary

The first interpreter slice includes:

- A32 condition execution; data-processing and flag operations; multiply and
  long multiply; `B`, `BL`, and `BX`; `SWP`/`SWPB`; user-mode `MRS`/`MSR`;
  single and multiple word/byte transfers; `STRH`, `LDRH`, `LDRSB`, and
  `LDRSH`; and SVC
- Thumb shifts, arithmetic and ALU operations, high-register operations and
  interworking, literal/register/immediate/stack-relative transfers,
  push/pop, multiple transfers, conditional and unconditional branches,
  long branch-with-link, long branch-with-link-and-exchange, and SVC
- ARM7TDMI multiple-transfer ordering when the writeback base is also in the
  register list, plus the pipelined `pc + 12` value stored by A32 `STM`
- bounded quanta, total retired-instruction diagnostics, end-address stops,
  and instruction errors containing the PC, encoding, and execution state

### The hint space, and why a Thumb-1 module has an ARMv6 instruction in it

`0xbf00` and its four neighbours — `NOP`, `YIELD`, `WFE`, `WFI`, `SEV` — are one
encoding with the condition field naming which. There is nothing for any of them
to do on one core with no interrupts and no event register, so all five execute
as nothing; what matters is that they execute rather than fault.

**They should not be there at all.** These are ARMv6T2 encodings and the local
archives are Thumb-1, compiled years earlier. Where they come from is the
archive having been *patched*: a cracker takes a branch out by writing a two-byte
`nop` over it, and `0xbf00` is what an assembler emits for one. A title that had
its authentication removed that way ran into it in its own loading path, and the
core reported an undefined instruction at an address in the middle of a perfectly
ordinary basic block.

The rest of the `0xbfxx` space is `IT`, which is not a hint: it changes how the
one to four instructions after it execute. Running it as a no-op would run a
conditional body unconditionally, so it stays undefined and says so.

A32 halfword and signed transfers support immediate and register offsets,
pre/post indexing, and writeback. Halfword accesses clear address bit zero,
matching the ARMv4T behavior used by the original execution core, while signed
byte loads retain the complete byte address.

Tests execute newly authored A32 and Thumb programs, including base-in-list
load/store multiple transfers and the exact shape of the original KTF SVC
stub.

## Deliberately incomplete

- architectural exception modes, banked registers, interrupts, and exception
  return through SPSR
- privileged PSR/SPSR transfers and coprocessor instructions
- remaining architecturally unpredictable edge cases
- cycle timing, debugger hooks, profiling samples, and cheat read/write watches
- instructions and edge semantics first reached by real KTF AOT method bodies,
  the JVM bridge, and later lifecycle code

The ignored real games under `var/games/ktf` are reached through acceptance
probes. All 32 current clients complete their bounded Thumb entry paths,
including self-relocation, return validated `WIPI_exe`, `ExeInterface`, and
function table pointers, and finish both returned initialization functions. The
slowest WIPI initializer takes 8,330,820 instructions, so this explicit probe
uses a ten-million-instruction ceiling without changing the runtime's
one-million-instruction default. No additional instruction encoding was
required by these initializers, the main-class constructors, or the `startApp`
bodies: all 32 real main classes load, complete their constructors, and return
normally from `startApp` against the current KTF runtime surface.

Games are played through this subset now rather than only started through it —
display, input, timers and persistence all sit on top of it — so what the
subset lacks is no longer answered by a probe but by a title stopping
somewhere, which is tracked per title in the support matrix
([`support.md`](support.md)) rather than here. The list above is what stays
deliberately absent regardless.
