# Armcore investigation history

This file preserves implementation investigations and dated validation records.
Statements about current behavior, missing APIs, corpus size and planned work
apply to their original revision; later records may supersede them. Start with
[armcore.md](../armcore.md) for the maintained overview. This consolidation adds
no execution or acceptance evidence.


## Contents

- [Implementation and validation record](#implementation-arm-core-implementation-status)
- [Execution and thread model](#implementation-execution-and-thread-model)
- [Memory](#implementation-memory)
- [What the guests actually spend their instructions on](#implementation-what-the-guests-actually-spend-their-instructions-on)
- [Standing in for the loops a rasteriser is made of](#implementation-standing-in-for-the-loops-a-rasteriser-is-made-of)
- [Interpreter throughput](#implementation-interpreter-throughput)
- [The supervisor-call boundary, and the slots that do not need it](#implementation-the-supervisor-call-boundary-and-the-slots-that-do-not-need-it)
- [Why the browser was seven times slower](#implementation-why-the-browser-was-seven-times-slower)
- [Guest sampling profiler](#implementation-guest-sampling-profiler)
- [Implemented instruction boundary](#implementation-implemented-instruction-boundary)
- [The backend seam, and the corpus that guards it](#implementation-the-backend-seam-and-the-corpus-that-guards-it)
- [Deliberately incomplete](#implementation-deliberately-incomplete)

<a id="implementation-arm-core-implementation-status"></a>

## Implementation and validation record

A pure-Go ARMv4T execution boundary: the interpreter the KTF and LGT guests run
on. It emulates the code those archives contain rather than a complete ARM
system; there are no exception modes, banked registers or coprocessors, because
nothing in a WIPI client uses them.

Three ARMv5T instructions are included regardless: `BLX` immediate in both the
A32 and the Thumb encoding, and `BLX` register. LGT modules are Thumb-compiled
and use all three (the A32 and register forms to reach their own code and for
every indirect call, the Thumb immediate pair for a Thumb caller's ARM callee),
so refusing them as "newer than ARMv4T" stopped every LGT title at its first
call. What the guests execute decides what belongs here, not the architecture
level the rest of the core was written to.

The Thumb immediate form shares its first halfword with `BL`: the prefix puts
the high half of the offset in `LR` either way, and only the suffix says which
state the callee is in. Its target is word aligned rather than halfword aligned,
so the low two bits of the computed sum are **dropped** rather than carried into
an ARM PC that would then fault on alignment.

A write to **CP15 register c7** (cache and write-buffer maintenance) is accepted
and does nothing. There is no cache to flush, and every path that changes a
page's bytes already drops that page's decode cache, so self-modifying code is
coherent whether the guest asks or not. Modules issue it right after copying code
into RAM. Nothing else in CP15 is answered: the rest reports what the hardware
is, and an invented value for an ID or control register would be a wrong answer
rather than a missing one.

<a id="implementation-execution-and-thread-model"></a>

### Execution and thread model

`internal/armcore` keeps shared guest memory in `Core` and a complete
`r0`-`r15` plus CPSR snapshot in each `Thread`. Only an instruction quantum is
serialized. When a guest reaches SVC, the core saves that thread's context and
releases the execution lock before invoking the runtime handler, which may wait
for a native or browser Host event while another guest thread runs.

Runtime handlers may also derive a nested function call from the suspended
context. The call applies the ARM argument layout (`r0`-`r3`, then a bounded
stack tail) to a temporary context, shares the parent logical thread's
registered thread-local guest words, and leaves the outer registers and stack
pointer intact when it returns. The AOT/JVM bridge requires this shape.

The core can register selected aligned guest words as logical-thread state.
Guest load/store instructions then see the running thread's private value, while
Host memory inspection sees the shared backing word. KTF uses this for its
nominally global Java exception-handler head, so two guest threads suspended
inside `try` regions cannot splice each other's handler chains. Native and race
tests cover separate threads and nested calls.

The default quantum is 1,000 instructions and the default per-run safety limit is
1,000,000 instructions. Both are explicit `CoreOptions`, and cancellation is
checked between quanta. A thread can be modified externally only before it
starts or while it is suspended at SVC.

A thread may override the per-run ceiling with `SetStepBudget` and install a
`SetLimitHook` callback that fires when a run (or any call derived from that
thread) exhausts its budget window. Returning nil grants a fresh window and
execution resumes in place; the hook may block, which parks the goroutine with
the guest context saved. KTF builds its cooperative thread scheduler and sliced
session startup on this. Returning an error fails the run; without a hook the
run fails with `ErrStepLimit`. Derived call threads inherit the parent's budget,
hook and thread-local state.

SVC stops directly at the runtime boundary with the instruction immediate,
address and resume PC. It does not model the architectural supervisor register
bank or exception vector. That suffices for the KTF Thumb stubs, which preserve
their scratch register, place a service identifier in `r12`, execute SVC, and
continue with `bx lr` after the handler writes the result registers.

<a id="implementation-memory"></a>

### Memory

The guest address space is 32-bit, little-endian, sparse, and safe to reserve in
large ranges without allocating backing bytes. Read, write and execute
permissions are validated independently. Loader writes may initialize mapped
read-only code; guest stores still require write permission.

Address overflow, unmapped access, permission failure, alignment, and the number
of distinct mappings are bounded and reported as typed errors. Newly mapped
storage reads as zero until a loader or guest write commits it. Instruction
fetches and direct Host memory helpers remain strictly aligned. ARMv4T guest data
loads rotate an unaligned word read from its containing aligned word, word stores
ignore address bits zero and one, and halfword accesses ignore address bit zero.

**Two mappings may not overlap unless one permits everything the other does.**
Mapping is additive (a range mapped again with more permission is a remap, and
stays legal), but a pair where each grants something the other withholds is a
silent layout mistake. Both mappings apply over the overlap, so an access there
gets the union: a read-write arena laid over a read-execute stub region permits
every write to it, the stubs are overwritten by whatever the arena's owner
stores, and the first symptom is a stub decoding into pixel data thousands of
calls later. KTF had exactly that pair (see `ktf.md`, "The arena and the stubs
cannot share an address"), so `Map` refuses it and the mistake is an error at the
call that made it.

`CommittedRegions` reports the coalesced spans of committed pages carrying a
required permission, clipped to their mapping bounds. Memory scanners (the cheat
engine) sweep these instead of the mapped reservations.

`Core.Watch` records every store to an address with the instruction that made it
(`watch.go`). It was built for cheats (finding the address of a health bar is a
scan; finding the instruction that decrements it is a watch), and it also
answers **what wrote over a structure this platform handed the guest.** One LGT
defect that had survived three passes of reasoning was named by one run with a
watch on the word that kept changing; see `docs/lgt.md`, "A class's statics live
in its class object". Use a watch before reasoning about who could have written
something.

<a id="implementation-not-every-store-is-a-guest-instruction"></a>

#### Not every store is a guest instruction

The watch used to record guest stores only, which made it wrong rather than
incomplete on the addresses most worth watching. This platform writes into the
guest's address space continuously: a Java object's fields are guest words the
runtime keeps in step, a supervisor call servicing `memcpy` writes the
destination itself, and image blits and published frames land the same way. None
of those is an emulated instruction, so an address the host rewrote every tick
reported **no writers at all**, which reads as "nothing touches this" rather than
"the instrumentation does not cover this", and ends an investigation.

`Memory.Write` is instrumented too, and every hit carries a `WriteOrigin`:

- **guest**: an emulated store. Its PC names the instruction exactly.
- **host**: a store by this platform. Its PC is the last guest instruction to
  have run. Reached from inside a supervisor call, it names the guest
  instruction that made the call, which is what the caller wants. Reached from a
  host path the guest never entered (a field synchronised between ticks, a frame
  published at a tick boundary), it names wherever the guest happened to stop and
  means nothing. The origin travels with the hit so the reader can tell, and the
  console prints `host, last pc` rather than an address to disassemble.

A guest and a host writer at the same PC stay separate hits, because folding them
together would hide whichever arrived second behind the other's count.

Walking the watch list rather than the written run keeps this affordable: a
frame published into guest memory is a hundred kilobytes and the watched
addresses are a handful, so a host write costs one span test and one pass over
the watches, never one pass over the bytes.

`Memory.WriteUntracked` is the deliberate exception, with one caller: the cheat
engine, which rewrites its frozen values every tick and pokes addresses on
command. Recording those would bury the writer a user is hunting under the
user's own tooling and report a value as written by the game when nothing in the
game writes it. Both ARM platforms route `cheatTarget.WriteMemory` through it,
and nothing else may.

Measured on two real titles: watching three words of a KTF title's platform arena
reported `no writes recorded yet` before the change and three host writers after;
the same watch across an LGT Clet's arena now reports eight, several landing
`0xef5def5d` (one RGB565 colour twice over, the frame being published into guest
memory). Every one of those addresses was written several times a second
throughout, and the tool had reported no writer.

`Thread.SetContext` replaces the whole guest-visible state, which is what a long
jump is: the condition flags and the instruction set of the saved point return
with the registers, whereas a handler that restores a context register by
register leaves them behind. LGT's Java exceptions are built on it.

<a id="implementation-what-the-guests-actually-spend-their-instructions-on"></a>

### What the guests actually spend their instructions on

The library hooks in `ktf.md`, "Recognising the C runtime in the image", answer
`memcpy`, `memset`, `strlen` and `strcpy` natively, because interpreting a
byte-at-a-time fill is the costliest thing this emulator does. The apparent next
step, the same treatment for copy loops the compiler *inlined* (which have no
entry point to hook), was measured before it was built, and the measurement
said not to build it.

Two titles profiled to the instruction, in a real scene rather than a menu:

| share | what the hot range is |
|---|---|
| 54.5% | a halfword fill — the engine's own rectangle fill |
| 36.1% | 4-bit pixels unpacked through a palette into a 16-bit surface |
| 43.8% | packed pixels through a palette with a transparent-colour key |
| 21.9% | another halfword fill, written as pairs of byte stores |
| 16.9% | an RGB565 alpha blend |

The first two rows are one title and the last three another; in each, two
ranges account for over 90% and 65% of all guest execution respectively. A
third title's hottest range is an AOT Java sprite draw. **None of them is a copy
loop**; every one is the game's own rasteriser.

The whole local corpus was then scanned. All 32 KTF images were searched
byte-exactly for the two inline-copy shapes worth hooking: a register-form
`ldrb`/`strb` loop advancing both pointers, and the stack form an unoptimised
build emits with `dst`, `src` and `len` all in frame slots:

- register form: **3 sites, in 2 of 32 images**
- stack form: **0 sites in 32 images**

A looser sweep for any load, store and two advancing pointers inside a short
backward branch found 8 copy-shaped and 34 fill-shaped loops across roughly ten
million decoded instructions. The one register-form site inside a deeply
profiled title **was never sampled**: zero share in a run of eight billion
instructions.

A pattern engine for those two shapes would therefore be machinery for
something the corpus barely contains and never runs hot. What it would have to
match instead is engine code, which does not generalise: the two halfword fills
above are the same operation in two titles and share no instruction sequence
(one counts down a register and stores halfwords; the other walks a destination
pointer against an end pointer, storing bytes in pairs from a fixed source).
Matching both requires a loop recogniser rather than a byte pattern.

**Validate the tool before the conclusion.** The first version of the corpus
sweep reported zero copy loops everywhere. That was an artifact: the
disassembler stopped at the first halfword it could not decode, so it read 822
of the 140,000 instructions in the first image, and every later image was the
same silence. A survey that reports "none" must first be shown to report a
known-present case.

The same warning applies to profiles. `-profile` prints a line per *region*, a
run of sampled addresses with no large gap (`regionIndex` in
`internal/guestprofile`). Several loops in one function land on one line, so a
share read from it covers everything that ran there, not one loop. One region
reading 43.59% was four loops, and the one that looked like the answer was 0.18%
of it. `-profile-folded` answers per address, and shares should be taken from it.

<a id="implementation-standing-in-for-the-loops-a-rasteriser-is-made-of"></a>

### Standing in for the loops a rasteriser is made of

The measurement above also says what the corpus does contain: fills and blits
(34 fill-shaped loops to 8 copy-shaped), with the game's own rasteriser hot in
every title profiled. It left open that matching them needs a loop recogniser,
because two titles doing the same fill share no instruction sequence.

A user then reported a specific title as slow, the condition the section had
named for revisiting it. Per address, in the reported scene:

| share | loop |
|---|---|
| 28.6% | a blit through a palette: a source byte indexes a table, the colour goes out sixteen bits at a time |
| 2.2% | the row loop around a fill |
| 1.9% | a second, guarded blit of the same shape |
| 0.18% | a constant halfword fill |

The recogniser was built for the two shapes present, in `fill_loop.go` and
`table_blit.go`; two more followed after measurement, in `byte_blend.go` and
`word_modulate.go`. All are structural: they read the body between a backward
branch and its target, give each instruction a role, and refuse a body where a
role is filled twice, left empty, or played by a register that also plays
another. Recognition is attempted only when a backward branch is taken, so every
other instruction pays one compare.

Four rules keep a stand-in indistinguishable to the guest:

- **Spans are validated whole before anything moves.** A blit that would have
  faulted partway faults where it would have, not after the stand-in has filled
  everything before it.
- **Only refusals are cached.** A positive answer is re-derived every time the
  loop is stood in for (six to twelve decode-cache reads against the thousands of
  pixels they authorise), because a cache of positives would need invalidating
  whenever the guest rewrites code, and this platform rewrites it. A run that
  merely declines is not a refusal: caching "too few iterations this time" would
  write the loop off for the session.
- **Registers and flags are left where the guest would have left them**, which
  `fill_loop_test.go` pins by running the same loop both ways and comparing all
  sixteen registers, the CPSR, and the memory written.
- **The step count is charged as though every instruction had run**, so a guest
  that would have exhausted its budget still does and `MaxSteps` keeps its
  meaning. The cost is a profile artifact: charged steps are sampled at the PC
  after the loop, so the instructions around a stood-in loop read as hot. Measure
  a stand-in by the share of the loop body itself, which drops to nothing, and by
  the wall clock.

Measured on the title that prompted it, over the same 1,790-tick route and save:

| | before | after |
|---|---|---|
| busy | 21.8s | **18.0s** |
| per tick p90 | 41.0ms | **35.2ms** |
| per tick p99 | 46.7ms | **40.8ms** |
| the blit body's share of instructions | 28.6% | **0.71%** |

A tick of this title is about 44ms of guest time, so p90 moved from 1.07x real
time (no headroom) to 1.25x. All 30 local LGT archives and all 35 KTF archives
rendered byte-identical first frames before and after, and the engine benchmarks
were unchanged, the existing blit benchmark's loop being a shape this does not
match.

<a id="implementation-a-validated-span-does-not-have-to-be-checked-again-per-pixel"></a>

#### A validated span does not have to be checked again per pixel

The first stand-in still called the sized accessors per pixel, which is why
28.6% of instructions became about 17% of time rather than all of it: each pixel
paid a thread-local test, a page lookup, a permission compare and a bounds check
for a span already proved reachable, once, whole.

`raw_span.go` answers the page's own bytes for as much of a validated span as
lies in one page, and both stand-ins index that slice, a page of destination at
a time, with the decode cache retired once per page instead of once per store.
**Its correctness rests on what it refuses**: a memory with anything watched (a
stand-in that skipped a watch would be the bug watchpoints exist to find), a
span that could touch a thread-local word, and a page with no committed storage.
Each refusal falls back to the checked path for that stretch, which also commits
the page, so the next stretch is usually direct. An odd destination stays on the
checked path outright, because a halfword store aligns its address downward and
the guest would write over its own previous pixel.

Measured the same way, on the same route, save and probe:

| | before | after |
|---|---|---|
| busy | 18.6s | **17.5s** |
| per tick p90 | 36.4ms | **35.1ms** |
| per tick p99 | 42.5ms | **40.4ms** |
| ns per instruction retired | 6.20 | **5.84** |

The instruction count was bit-identical across the arms, which makes
`ns_per_step` the comparison rather than tick time. On KTF, interleaved A/B on
two titles: one −3.2% on both pairs, one unmoved, because the second's loops are
not shapes the recognisers match. All 30 local LGT and 35 KTF archives still
rendered byte-identical first frames.

The same profile showed the analysis allocating a five-entry map per stand-in to
check that no two roles share a register; it is now a bit per low register.

<a id="implementation-what-a-title-spends-its-instructions-on-once-the-stand-ins-are-in"></a>

#### What a title spends its instructions on once the stand-ins are in

Same scene, per address, with the first two stand-ins running. Not all of it is
reachable by a recogniser:

| share | what it is |
|---|---|
| 34.6% | the region holding the stood-in blit. **This is the charging artefact**, not work: the steps are charged at the PC after the loop, and the loop itself is being run in Go |
| 13.2% + 7.6% | **the guest's own allocator** — a walk down a free list of block headers, and the three leaf helpers it calls per block |
| 7.3% | a modulate: two pixels packed in a word, each channel group masked, scaled by a byte from a third stream, two rows at a time |
| 7.2% | a byte blend: `if src[i] - k > dst[i] { dst[i] = src[i] - k }` |
| 4.6%, 4.1%, 2.9% | run-length sprite draws with a saturating additive blend per channel |

**The largest remaining consumer is the game's own `malloc` and `free`**, at a
fifth of every instruction executed, and it is not the emulator's: its cost is
the length of the list the title keeps, its loop calls out to helpers, and
standing in for it would mean reimplementing a heap whose layout the title reads
back.

The list's length, counted: over 4,000 ticks of one title driven through a
route, the search was entered **9,535 times and its body ran 33,253,303 times**,
an average of **3,488 blocks walked per call** at 28 instructions each, 27% of
every instruction the run charged. It is a best-fit search over a singly linked
list of block headers, so its cost is linear in the number of blocks the title
holds, and nothing on the emulator's side of the boundary shortens it. The number
is recorded so that a later "the emulator got slower the longer it ran" report
can be checked against it: a longer list means something is leaking into the
title's own heap.

**It grows across a session**, step-wise at scene loads rather than smoothly:

| ticks into the run | blocks walked per call |
|---|---|
| 50 – 200 | 1,242 – 1,257 |
| 250 – 300, the first scene load | 3,344 |
| 450 – 850 | 5,135 – 5,305 |
| 1,000 – 1,050, the second load | 5,317 |
| 1,150 – 1,450 | 5,654 – 6,151 |

It ends five times longer than at boot and never shrinks. Each load adds blocks
the title does not give back, with a drift of a percent or two per two hundred
ticks between loads. **Whether any of it is the emulator's is not settled**: the
chain is the title's, but a platform call that allocates a record in the guest
heap and never frees it would land in this list and cost every later allocation,
because the search is linear in the chain's length. Answering it needs a census
(walk the chain from the host at two points in a run and diff the blocks), not
another profile.

This decides where speed work goes. Per fifty ticks of the same route, windows
sorted by how much of this loop they contain:

| the loop's iterations in the window | ns per step | share of the window's instructions the stand-ins covered |
|---|---|---|
| none | 3.7 – 4.2 | 55 – 65% |
| 100k – 400k | 4.3 – 4.6 | 51 – 54% |
| 500k – 900k | 4.9 – 5.8 | 33 – 46% |
| 9.7M (a scene load) | 6.7 | 15% |
| 18.1M (a scene load) | 7.8 | 4% |

**The expensive windows are the allocating windows**, and the ranking is
monotone in both columns. The stand-ins are not failing there; there is nothing
for them to stand in for. A scene reported as slow is more likely one of these
than one the recognisers missed.

The two shapes below it were built (`byte_blend.go`, `word_modulate.go`); the
three run-length draws were not. Each of these is a *different arithmetic*, so a
recogniser for one does not read another, and the case for building one is how
much of a reported-slow scene it covers rather than how general it looks. The
run-length draws are 64 instructions a pixel across three sites with saturation
branches per channel; a recogniser for them would be a rewrite of one title's
sprite routine rather than a shape.

<a id="implementation-two-more-shapes-and-what-they-cost-to-recognise"></a>

#### Two more shapes, and what they cost to recognise

**A guarded byte blend** (`byte_blend.go`). Eleven instructions a byte, and the
first body with a branch in it: a forward conditional branch skips the store when
the incoming byte does not win. The walk gained a positional rule (the compare,
the guard and the store adjacent and in that order, the guard landing on the
instruction after the store), because a role walk alone cannot say *what* a guard
guards. Its charge is the first that is not one number: a skipped store is one
instruction less, and the test runs the same loop both ways and compares the
counts.

**A two-stream modulate** (`word_modulate.go`). Forty-two instructions a step
over two rows at once, **matched as a sequence rather than as a walk**. A role
walk works when each instruction contributes one distinct effect; here the same
thirteen-instruction computation appears twice over two stream pointers, and
"the second half's third `ands`" is a position, not a role. The body is read in
order, every register bound where it is first met and checked wherever it
recurs, every immediate and shift amount read out rather than assumed, and the
two halves required to agree in all but which stream they walk. Nothing is
matched by encoding (a title that allocated its registers differently would bind
different numbers and still be recognised), but the sequence is one title's
blend, and that trade is recorded here.

Both keep the existing rules and add one each:

- **What the loop re-reads every step must be somewhere it does not write.** The
  blend's constant lives in a frame slot; the modulate re-reads its two masks
  from its own code and its factor pointer from the frame. A stand-in reads each
  once, so a destination span covering any of them is refused rather than
  modelled.
- **The last iteration is left to the interpreter.** The stand-in runs all but
  one remaining iteration and leaves the loop pointing at the last, which the
  engine interprets. That makes the scratch registers right for free: both
  bodies leave several values in registers the code after the loop reads, and
  reproducing each by hand is the class of mistake that surfaces only when one
  title's shadows are wrong. The flags come out right for the same reason. One
  interpreted iteration in a thousand costs nothing.

Recognition is attempted when a taken backward branch reaches no further than
the longest body any recogniser reads, which these raised from 32 bytes to 84.
The cost is one analysis per loop head inside the new range that is not one of
these shapes, paid once because refusals are remembered; against two KTF titles,
interleaved, it was inside the noise.

Measured on the title that prompted all of this, same route, save and probe:

| | no stand-ins | blit and fill | + raw spans | + blend | + modulate |
|---|---|---|---|---|---|
| busy | 22.1s | 18.6s | 17.5s | 16.2s | **14.6s** |
| tick p90 | 41.5ms | 36.4ms | 35.1ms | 30.3ms | **24.1ms** |
| tick p99 | 47.0ms | 42.5ms | 40.4ms | 38.7ms | **33.9ms** |

A tick of this title is about 44ms of guest time, so p90 went from 1.06x real
time (no headroom) to **1.83x**. More important than the clock: **all 770 frames
the route paints were byte-identical** before and after, at the same tick
numbers, and the whole-corpus first-frame sweep was unchanged on all 30 local LGT
and 35 KTF archives.

**Compare stand-ins by busy time, not by `ns_per_step`.** A quantum that ends
inside a stand-in reports the quantum's size rather than what the stand-in
charged (`Engine.Run` answers `count` on the exhausted path), so the steps a big
charge overshoots by are lost from the accounting. That is the safe direction for
a budget and invisible within one build, but across builds a recogniser that
stands in for more loses more, which reads as a throughput gain that is partly an
accounting one. (Fixed later; see "The charge a quantum could not hold".)

<a id="implementation-the-same-fill-written-four-ways"></a>

#### The same fill, written four ways

The recognisers above were built against the title a person reported as slow,
leaving open how much of the rest of the corpus they reach. That needs no
profile: the hook already knows which backward branches it refused, so counting
refusals per loop head over a boot of every local archive ranks each title's
largest un-standable loop. Thirty archives, 600 ticks each, share of the title's
own instructions in the top loop:

| share of everything the title executed | how many archives |
|---|---|
| over 60% | 5 |
| 30 – 60% | 4 |
| 15 – 30% | 8 |
| under 15% or nothing hot | 13 |

**Five titles spent more than sixty percent of every instruction in a single loop
the recognisers refuse.** Disassembling the top loops showed three of them were
the same three instructions the counted fill already stands in for, refused over
how the compiler tested whether the count had run out:

| the loop, as the title emits it | share | why it was refused |
|---|---|---|
| `subs rC,#1 / strh rV,[rP] / adds rP,#2 / cmp rC,#0 / bne` | 19% | the count is tested against zero instead of read out of the subtraction's borrow |
| `subs rC,#1 / strh rV,[rP,rI] / adds rP,#2 / cmp rC,#0 / bne` | 29% | that, and the store reaches its destination through a base and an index |
| `mov rV,sp / ldrh rV,[rV,#4] / adds rC,#1 / strh rV,[rP] / adds rQ,#1 / adds rP,#2 / cmp rC,rL / ble` | 69% | the count runs up to a limit in a register, and a second register runs up beside it |

The first two were built. The ending is now a property the walk reads rather than
a fixed instruction order (`subs`+`bhs` runs one more time than the counter says
and leaves it at -1; `cmp #0`+`bne` runs exactly the counter and leaves it at 0),
and the store may carry an index the body must never touch. Measured on the same
1,500 ticks per title:

| | before | after |
|---|---|---|
| the 19% title | 13.85s busy | 11.41s (**−17.6%**) |
| the 29% title | 3.68s busy | 2.59s (**−29.8%**) |
| two titles whose loops are neither form | unchanged | unchanged |

**The third, the largest number in the table, was not built.** It needs the
counter to run *up* against a limit held in a register, with a signed branch, and
any low register whose only effect in the body is `adds rD, #k` to be advanced by
`k` times the iteration count rather than refused as a second counter. That is a
real generalisation (an induction variable rather than one named pointer and one
named counter) and would subsume the pointer advance the analyser special-cases.
The evidence for it: one archive at 69%, its duplicate, and a second loop in the
29% title at 28%.

The remaining titles are *not* all fills. Two of the five over sixty percent are
bit-test loops over a packed structure, and the shared sprite routine three titles
carry is 29 – 49% of each. Each is a different arithmetic, and the rule above
holds: the case for building one is how much of a reported-slow scene it covers,
not how general it looks. The sweep's contribution is that the share is known
before anyone reports anything.

<a id="implementation-what-the-hook-costs-where-it-never-fires"></a>

#### What the hook costs where it never fires

The recogniser is reached from one compare on every taken conditional branch,
and the tables above measure it where it fires. Where it does *not* fire matters
after a scene it cannot reach: **a title sitting on its own menus takes a backward
branch every seventeen instructions**. Counted over 1,500 ticks of two titles
idling on a title screen and its menus, with no stand-in of any kind:

| | backward branches | guest instructions | branches per instruction | stand-ins |
|---|---|---|---|---|
| one title's title screen | 105,556,869 | 1,945,417,595 | 1 in 18.4 | 0 |
| another's | 18,814,133 | 303,944,403 | 1 in 16.2 | 0 |

Every one of those reached the refusal cache, then a `map[uint32]bool` keyed by
the loop head. Against a build with the hook compiled out, four titles, three
interleaved runs each, on their own title screens and menus where no stand-in
fires:

| ns per step, title screen | hook compiled out | refusal in the decode entry | refusal in a map |
|---|---|---|---|
| A | 7.02 | 7.20 | 7.42 |
| B | 11.11 | 11.60 | 11.44 |
| C | 9.07 | 9.31 | 9.53 |
| D | 7.50 | 7.64 | 7.65 |

**The hook cost 2 to 6% of a run it never fires in.** Moving the refusal into
the branch's own decode-cache entry (the padding byte beside the form, so the
entry stays four bytes) returned about half of that on two of the four and
nothing on the other two, one slightly the wrong way inside its own spread. It is
kept for a reason the table does not show: a map keyed by the loop head is never
invalidated, so a title that rewrites a page inherits the old code's refusal,
whereas an answer on the decode entry is dropped with the entry when the page's
instructions change.

**The hook is not free on code it cannot help, and that code is what a person
looks at while nothing moves**: menus, text, dialogue. A change here is judged on
both kinds of scene.

The same measurement answers a report that "the game got faster but the menus got
slower". Per fifty ticks of one title driven through a route, before and after
the stand-ins:

| what the ticks are | before, ns/step | after, ns/step |
|---|---|---|
| boot and vendor splashes | 8.2 – 13.3 | 8.0 – 9.0 |
| the scene load | 6.6 | 6.9 |
| walking and drawing the map | 7.3 – 7.8 | 3.5 – 4.6 |

The drawing halved and the menus did not move. The menus were always the
expensive code per instruction, since they interpret every one of theirs; they
only *read* as a regression once the scene beside them became cheap. A stand-in
that reaches one scene widens the gap between two scenes, and the gap is what a
person notices.

<a id="implementation-the-block-interpreter-measured-before-building-it-again"></a>

#### The block interpreter, measured before building it again

The interpreter's remaining dispatch overhead had long been recorded as "size
unknown, measure with a small benchmark before starting". Measured on the
game-shaped loop as the *ideal* a block interpreter could reach (the PC read, the
Thumb test, the `end` compare, the watch compare and the decode-cache lookup all
removed, the same handlers behind the same switch, and the PC write kept because
an instruction may still read it):

| inner loop | ns per instruction |
|---|---|
| `Engine.Run` as it is | 5.26 |
| ideal blocks of 8 instructions, one lookup per block | 4.96 (−5.8%) |
| ideal blocks of 16 | 4.94 (−6.1%) |
| ideal blocks of 32 | 4.76 (−9.5%) |
| one ideal block of 160, no lookup at all | 4.11 (−22%) |
| no dispatch whatsoever — every handler called straight | 3.20 (−39%) |

Real blocks are 8.5 instructions, or 15.6 with `mov ip, rN` split at decode time
(see the six arrangements below). **The ceiling at real block lengths is therefore
about 6%, on a benchmark that pays none of what a real one pays**: no block
building, no invalidation, no early exits, and no register pressure on the
ordinary path. The six arrangements actually built returned 5 to 9% on the code
they targeted and cost 1 to 10% everywhere else, the same number from the other
side. **This is settled: the design cannot pay for itself, and the benchmark that
shows it is `BenchmarkEngineGameShapedLoop` against the ideal loop, not another
arrangement.**

A cheaper variant was tried and dropped with it: routing five more Thumb forms
(literal load, high register, push, pop, multiple transfer) straight from the
engine's switch instead of through `executeThumbForm`, which a profile put at
2.2% of a run in second dispatch. Interleaved A/B, three pairs each: **−1.2% on an
LGT title, +0.6% on a KTF one.** Growing the hot switch costs the ordinary path
about what the second dispatch cost the forms that took it, the same lesson as the
block arrangements.

<a id="implementation-interpreter-throughput"></a>

### Interpreter throughput

Two costs dominated a step, and neither was the instruction itself.

The memory lock was taken and released once per guest access (per instruction
fetch, load and store) while nothing contended it. It moved out to the quantum:
`Engine.Run` holds it across the instructions it runs, and the guest accessors
document that their caller holds it. No protection is lost, because `Core`
already serializes engine runs behind its execute mutex and supervisor-call
handlers run after the quantum returns, outside the lock. The lock is exclusive
rather than shared because a guest store commits pages, which a read lock cannot
be upgraded to do.

Decoding was repeated per execution: a hot loop of a thousand iterations walked
the same match tree a thousand times and re-read the same bytes from guest memory
to do it. `Memory` caches the classified instruction per address, in an array
committed lazily per page and indexed directly, so a repeat costs an index
instead of a mapping scan, a page lookup and a tree walk. `classifyThumb` answers
the form and `executeThumbForm` runs it.

The cache is only as safe as its invalidation, and this platform rewrites code:
the KTF loader relocates itself and patches SVC stubs over pages it has already
executed. Every path that changes a page's bytes drops that page's entries, a
remap retires all of them, and a page a mapping covers only partly is never
cached, because a cached page answers later addresses without re-checking
permission and its tail would otherwise execute as code. `decode_cache_test.go`
covers all three.

Guest loads and stores had their own version of the problem. Every access
re-scanned the mapping list, looked the page up in a map, and copied the bytes
through a temporary array: a four-byte load was an array, a copy, four byte
loads, three shifts and three ors. Three changes fixed it. The last mapping that
satisfied a validation and the last page touched are remembered, because guest
code stays inside one region for long runs, so both lookups become a compare.
Aligned accesses that no thread-local word overlaps read and write the page's
bytes in place. The thread-local splice walks words rather than bytes, behind a
bounds check on the registered span, so the ordinary access never consults the
map.

Both caches are keyed by address, so the hazard is one region answering for
another; `decode_cache_test.go` pins that an address past a cached mapping still
faults, a read-only region is not made writable by a cached read-write one, and
a second page does not return the first page's bytes.

What remained was finding the page. Pages lived in a Go map, and an instruction
fetch looked it up every instruction. Fetch now has its own cached page, held
separately from the data one so a loop reading data does not evict the page it
executes from, and the map was replaced by a two-level table indexed by the top
and middle bits of the address. Both lookups are array reads.

That last pair was the largest win on real games and almost invisible on the
benchmarks: a benchmark's page map holds two entries and stays in L1, while a
game maps thousands and every lookup is a hash into cold memory. Benchmark on the
shape of code that runs; `BenchmarkEngineGameShapedLoop` does, being three
immediates, one halfword load and one conditional branch, which is 97% of what a
measured KTF title executes.

Measured on an M1, per instruction, native:

| | before | after |
|---|---|---|
| ALU loop | 16.9ns (59 MIPS) | **8.8ns (114)** |
| load/store loop | 28.1ns (36 MIPS) | **11.2ns (89)** |
| game-shaped loop | — | **9.0ns (111)** |

Real KTF game frames in wasm, against the frame time the game asks for (under
1.0x means the emulator keeps up):

| | asks for | before | after |
|---|---|---|---|
| title A | 50ms | 25.1ms (0.50x) | **9.1ms (0.18x)** |
| title B | 44.8ms | 94.4ms (**2.11x**) | **30.8ms (0.69x)** |

<a id="implementation-what-is-left-and-two-shapes-of-cache-that-do-not-get-it"></a>

#### What is left, and two shapes of cache that do not get it

The numbers above are a benchmark's. A real title's load, measured on a manual
clock so the game's own pacing is excluded, retired 731M instructions in 6.4
seconds of host CPU: **8.7ns an instruction against the game-shaped benchmark's
5.1**. Locating the difference took a fourth benchmark.

`BenchmarkEngineBlitCrossPage` streams a halfword out of one buffer into another,
both pointers advancing. Every sprite blit, tile draw and screen clear here is
made of that, and the three older benchmarks lack it (the game-shaped loop's one
load hits the same address every iteration). Reading the two against each other
separates the costs:

| | per instruction |
|---|---|
| an ALU instruction | **~3.5ns** |
| an aligned load or store | **~11ns** |

The transfer path holds the remaining time, worth about three ALU instructions.
The blit also shows where a transfer's extra cost comes from: the same loop with
source and destination inside one page runs at 5.2ns and across two pages at
7.2ns. One cached data page cannot hold a blit.

**Two caches built to fix that both lost.**

*A second cached page*, checked only after the first misses, and outlined so
that `mappedPage` (inlined into every aligned load and store) keeps its body. It
gained **25%** on the cross-page blit and nothing on the other three benchmarks,
and was **3 to 5% slower** on every real title. Instrumented on a real load,
95.4M accesses missed the first entry and the second caught 10.6M of them, so 89%
of misses paid an extra call for nothing.

*A direct-mapped set of sixteen*, indexed by page number, which should cost the
same one compare and hold a working set. The array index pushed `mappedPage` past
the inline budget, and losing that inlining cost more than every hit the set
added: 17% slower on the load/store loop, 15% on the blit, 3% on game-shaped
code.

**The fast path's inlining is the constraint, and it is worth more than hit
rate.** Anything tried here has to leave `mappedPage` inlining, which
`go build -gcflags=-m` reports, and has to be measured on a real archive through
`TestLoadCostProbe` rather than on the benchmarks alone; the cross-page blit and
the local titles disagreed by 30 percentage points about the first design.

What remained: 13% of a real load's instructions missed the cached page, and the
cost of a miss (a validation, a two-level page walk, and the remembering) was the
thing to reduce, not the number of things remembered.

<a id="implementation-the-permission-moved-onto-the-page-and-a-miss-got-a-sixth-cheaper"></a>

#### The permission moved onto the page, and a miss got a sixth cheaper

Counting *why* misses happen showed which half of a miss to attack. Across local
titles, **the mapping changed, not the page**: on one archive 99.6% of the
accesses reaching the general path did so because the remembered mapping did not
cover them, on another 57%, and on both the share that were the *same* page as
the last access was under a tenth of a percent. Guest code alternates between
regions (a module's constants and a heap buffer, a stack frame and a
framebuffer), one remembered mapping cannot hold that, and every such miss paid
a scan of the mapping list.

So the permission moved onto the page. `memoryPage.permission` is what the
mappings allow across *the whole* page, filled the first time the general path
touches it, and it replaces the mapping test: an aligned access of at most a word
cannot leave its page, so a page that permits the operation permits the access.
A page a mapping only partly covers keeps zero and always takes the general path,
where the mapping list still answers per byte. `Map` is the only thing that
changes a mapping, and it already walks every page to retire decode caches, so
forgetting the remembered permissions costs nothing new.

The fast path lost three compares, gained two, and still inlines. The miss path
lost the scan: it walks two arrays, reads the permission from the page it was
going to read anyway, and answers.

Measured on 24 local LGT archives, sixty ticks each, best of three:

| | before | after |
|---|---|---|
| the heaviest local title | 3.63s | **3.03s** (-17%) |
| the one after it | 3.22s | **2.60s** (-19%) |
| a title that boots slowly | 2.72s | **2.42s** (-11%) |
| the largest share of all | 1.10s | **0.87s** (-21%) |
| the median archive | — | **-5%** |

The gain tracks how much of a run is guest ARM: the heaviest quarter gained 11%
to 21%, the middle 5%, and the lightest (sixty ticks of mostly loading) did not
move. Nothing measured slower than noise. KTF titles were within noise either
way, as expected of a platform whose time goes to the JVM rather than guest ARM.
The synthetic benchmarks agree it is a miss-path change: the cross-page blit
gained 3% and the other four were unchanged.

An upper bound was taken first by deleting the validation entirely in a throwaway
build: 12% and 16% on the two heaviest titles. The page permission gets 11% and
17% of that, so nothing is left to gain in the mapping list.

What remains is **the walk**: two array reads and the remembering, on every
access that changes page.

<a id="implementation-two-more-shapes-for-the-walk-both-measured-and-both-worse"></a>

#### Two more shapes for the walk, both measured and both worse

Both were A/B'd on 29 local LGT archives, sixty ticks each, best of three,
against a baseline run twice so the noise floor is in the same table. The floor
is about **1%** per title.

*Remembering the directory entry as well as the page*, so a miss inside the same
four-megabyte region skips the first array read. **0.8% slower overall** and 2%
slower on six mid-weight titles, outside the floor and in the same direction
every time. The reason is the one the page permission established: a miss is a
*region* change, not a page change, so the directory entry it walks to is almost
always a different one, and the compare and two stores buy nothing.

*Bigger pages*, 16KB instead of 4KB, so a streaming blit changes page a quarter
as often. **7.8% slower overall**, one title 43% slower. `memoryPage.permission`
is only filled when the mappings cover *the whole* page, and at 16KB many more
pages are covered only partly and never get the fast path. Page size is coupled
to the mapping granularity a platform lays out, and 4KB fits the local ones.

The three designs tried against the walk agree: **what a miss costs is not the
number of memory reads in it.** The next attempt should start by measuring where
a miss's time goes rather than by removing a load from it.

Two measurements to distrust here. The macOS Go CPU profiler attributed 88% of a
run to `runtime.pthread_cond_signal` under `Gosched`; raising the quantum a
thousandfold changed the time by 5%, so that profile was noise. An isolated
benchmark of `RLock`/`RUnlock` reported 13.8ns, which would have made the lock
82% of a step; hoisting it out of the loop returned 11%, because a dependent loop
over one cache line exaggerates what an uncontended atomic costs inside real
work. Both numbers pointed at the right code for the wrong reason. A/B the whole
benchmark instead.

<a id="implementation-where-a-clets-time-goes-and-the-quantum-that-is-not-one-number"></a>

#### Where a Clet's time goes, and the quantum that is not one number

Measuring where the time goes first needs a usable profiler, and on macOS the Go
CPU profile of an LGT title attributed **79% of the run to
`runtime.pthread_cond_signal`** under `runtime.goschedImpl`, the `Gosched` the
engine made at the end of every quantum, which is not work. With the quantum
raised a thousandfold the same profile read:

| | share |
|---|---|
| `Engine.Run` itself (dispatch, fetch, the switch) | 24% flat, 92% cumulative |
| `decodedThumbFast` | 9% |
| the transfer path: `directAccess`, `pageAt`, `mappedPage`, `read8/16/32` | ~25% cumulative |
| the ALU: `executeThumbALU`, `shiftImmediate`, `setNZCV`, `setNZC` | ~17% |

There is no platform work in the answer: **it is all interpretation**. The
whole-surface copies a draw slot makes on either side of itself
(`syncFromGuest`/`syncToGuest`) do not appear in the profile at all; a heavy
title's frame is thousands of guest instructions per drawn pixel, not a copy.

Measuring the quantum produced a rule. **Host time per tick is not a throughput
measurement here.** Both platforms advance their guest clock with the work the
guest does, so changing the quantum changes how much work a tick contains, and
the tick time moves for two reasons at once. Held against the instruction count,
which was bit-identical across the arms on both platforms so the comparison is
exact, the result is *platform-dependent*:

| quantum 1,000 → 16,384 | ns per instruction |
|---|---|
| four LGT titles | −6.2%, −7.1%, −5.0%, −1.7% |
| three KTF titles | +12%, +4.2%, +3.0% |

Every LGT title gained and every KTF title lost, so the quantum is not one
number: `internal/platform/lgt` passes 16,384 to its `Core` and the default stays
1,000. Why the platforms differ is not settled (a Clet runs long stretches
between platform calls while a KTF title crosses the Java bridge constantly, so
the two pay for a yield at very different rates); the choice rests on the
measurement, not on that reading.

`TestLGTLoadCostProbe` measures this, with `WFEATURE_PERF_ARCHIVE` and
`WFEATURE_LOAD_TICKS`, reporting `ns_per_step` beside the tick time; it is the
thing to run under `-cpuprofile`.

**A probe that measures cost must not call `TickFor`.** Its answer is how long
the Host should *wait* before the next tick, not what the tick cost, and an
emulator running ahead of real time reports most of a tick as wait. Read as a
cost, it made this title's worst tick look like three to six seconds of world
loading; measured around `Tick` it was **80ms**, and the whole run got 27%
cheaper once the idle was excluded. Nothing was slow; the probe was waiting.

<a id="implementation-straight-line-blocks-were-built-and-lost-six-ways"></a>

#### Straight-line blocks were built and lost, six ways

The guest profile said the working set is a kilobyte and mostly straight-line,
the shape a block interpreter is for: decode a run of instructions that cannot
write PC, then execute them without going back through the loop for each (no PC
read, no Thumb test, no `end` compare, and an increment where `decodedThumbFast`
was a lookup). The projection was 1.4 to 1.6x. **It was worth 5 to 9% on the code
it targets and cost 1 to 10% on everything else, and every arrangement tried
landed in the same place.**

In the order taken, each A/B'd by `ns_per_step` on real archives against the
same build with blocks compiled out:

| arrangement | result |
|---|---|
| block loop delegating to a shared per-instruction runner | 6–11% **slower** than no block at all |
| switch written out inside the block, form tested by a helper | still slower: the helper inlines *as a switch*, so every instruction dispatched twice |
| form tested by a `[256]bool` — no bounds check, no second switch | two heavy titles −7%/−9%, two light ones +3%/+5% |
| the whole block loop inlined into `Engine.Run` | 4–5% slower again: the hot loop grew and the ordinary path paid for it |
| one cache lookup serving both paths (the best of them) | LGT mean **−0.4%**; heavy titles −5.8%/−8.2%; one title +4.7% |
| plus refusing blocks shorter than four instructions | mean back to +0.4%, and the worst regression unmoved |

Three findings outlast the arrangement:

- **A block is short because of `mov ip, rN`.** RVCT spills through r12, so a
  title's inner pixel loop carries three or four high-register moves, and
  excluding that form (it *can* write PC) cut the average block from about thirty
  instructions to 8.5. Splitting the form at decode time by whether its
  destination is r15 doubled blocks to 15.6 and turned two titles positive.
  Deciding at decode time is what makes it free.
- **The loss is not block entry.** Refusing short blocks did not move the worst
  regression, so the cost is the restructured loop: carrying a table pointer and
  an index through `Engine.Run` is register pressure the ordinary path pays on
  every instruction, and most titles are mostly ordinary path.
- **Which titles win depends on code shape, not platform.** The two LGT titles
  with fifteen-instruction runs gained 6 to 9%, as did the one heavy KTF title.
  Titles whose loops branch every fifth instruction lost on both platforms, and
  the worst, a KTF title at **+9.5%**, is why this is not shipped behind a
  platform switch either.

The dispatch overhead a block removes is worth about 5 to 9%, the whole reach of
this design. The rest of `Engine.Run`'s 24% is operand extraction inside the
handlers (each re-derives its register numbers and immediates from the raw
halfword on every execution), and reaching it means widening the decode cache
entry from four bytes to pre-extracted fields and rewriting the handlers to take
them: a larger change, where the projected 1.4x would have to come from. It also
triples the decode cache's memory per page, which is the first thing to measure.

<a id="implementation-a-wider-decode-cache-entry-was-built-and-lost-twice-over"></a>

#### A wider decode cache entry was built and lost, twice over

That attempt was built. **The 1.4x is not there, and the design loses on both of
its halves.**

Memory was not the question. `Memory.DecodeCacheStats` reports how many pages
hold a committed cache and what they cost, and on real archives (three LGT
titles, two KTF) it was **15 to 63 pages, 0.1 to 0.5 MiB**; tripling that costs
a megabyte.

Throughput decides it: `ns_per_step` over 600 ticks, three runs a side, same
machine, against a tree with the change reverted. Two titles carry the signal
because their time is mostly stepping; two others sit near 190 and 240 ns a step,
where the host dominates and the interpreter barely shows.

| title | four-byte entry | twelve-byte entry, handlers unchanged | twelve-byte entry, handlers reading it |
|---|---|---|---|
| one LGT title | 6.28 | 6.87 (**+9.4%**) | 7.49 (**+19.3%**) |
| another | 10.91 | 11.68 (**+7.0%**) | 12.30 (**+12.7%**) |
| a third | 55.3 | — | 56.8 (+2.7%) |
| a fourth | 189.4 | — | 190.0 (+0.3%) |
| a fifth | 242.6 | — | 241.8 (−0.3%) |

The middle column isolates the width: the *same* handlers as the baseline,
reading the same raw halfword, with eight bytes of padding in the entry. That
alone cost 7 to 9%, the decode array's footprint over a hot loop tripling and
pushing the loop out of the cache it had fitted in.

The right-hand column adds the rest of the design (registers, scaled offsets,
sub-selectors and resolved branch targets extracted at decode time, the ten
routed handlers rewritten to take them) and cost a further 9 to 10%. **Reading a
pre-extracted field is more expensive than recomputing it.** The halfword is
already in a register because the entry was loaded anyway, and a shift-and-mask
on it is an ALU operation an out-of-order core hides behind the load it waits on;
a pre-extracted byte is another load, another zero-extend, and, because a
`uint8` gives the compiler no bound, another mask to keep the register-file index
provable.

The two halves cannot be traded against each other: packing the entry tighter
would at best recover the width penalty, and the extraction penalty was measured
at equal width. The design is closed, and with it the 24% that motivated it: that
share is the arithmetic the instructions consist of, not a cost the entry can
move elsewhere.

`Memory.DecodeCacheStats` stays, and both platforms' load probes report it; any
future claim about the cache's memory starts from it.

<a id="implementation-both-of-those-were-about-thumb-and-the-slow-titles-are-not-thumb"></a>

#### Both of those were about Thumb, and the slow titles are not Thumb

Everything above (the decode cache, the routed switch, the blocks, the wider
entry) is on the **Thumb** path. `Engine.Run` has two halves that are not
comparable: Thumb reads a cached decode and routes ten forms straight to their
handler; ARM called `fetch32` and walked `executeARM`'s chain of mask-and-compare
tests from the top on **every instruction**. There was no ARM decode cache.

(There is now. "The ARM cache was built, and it halved an ARM step", below, is
the outcome of this section and the two after it.)

Which path a title takes matters. Steps by instruction set over a real run of
three local LGT archives:

| title | Thumb | ARM |
|---|---|---|
| a Clet | 99.8% | 0.2% |
| a Java title | 38.1% | 61.9% |
| the Java title with the reported stutter | 21.1% | **78.9%** |

**A Clet is Thumb and an ahead-of-time-compiled Java title is ARM**, the missing
half of "a Java title is the other shape, and it is the slow one" (`lgt.md`).
**It is a tendency, not a rule**: the slowest title of all reports
`mclass: Clet` and runs 100% ARM, so read the share from `arm_share` rather than
from the class. The Clets that run at five to six times the guest clock are the
ones every optimisation above was measured on; the title that manages twice the
clock spends four fifths of its instructions on the path none of them touched.

What that path cost, on the same machine as every number above (`-benchtime 2s`,
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

**The ARM step decomposes exactly**:

```
fetch 8.18  +  match chain 4.13  +  operation 4.65  =  16.96 ≈ 17.03
```

Two facts follow, and they are the whole design:

- **A whole Thumb step is cheaper than a single fetch**, 4.64 against 8.16. The
  decode cache pays for itself by removing the fetch, the most expensive part of
  an instruction.
- **An ARM operation alone costs what an entire Thumb step costs**, 4.65 against
  4.64. Everything ARM pays over Thumb is fetch and dispatch, 12.3ns of the
  17.03, and both are what the Thumb path already removed.

The cheapest remaining change *inside the interpreter* is therefore *the ARM
decode cache, with the form stored in the entry so the chain becomes a routed
switch*: the one mechanism here with a measured precedent rather than a
projection, the same change on the other half of the same loop. It should land
an ARM step near a Thumb one, since afterwards both are a cached decode, a switch
and the operation. That is ~3.4x on ARM steps and, on a title that is 78.9% ARM
and 21.1% Thumb, about 2.9x on **interpreted** time:

```
now    0.789 × 17.03 + 0.211 × 4.64 = 14.4 ns a step
after  0.789 ×  5.0  + 0.211 × 4.64 =  4.9 ns a step
```

<a id="implementation-and-none-of-that-was-worth-doing-because-the-interpreter-was-4-of-the-run"></a>

#### And none of that was worth doing, because the interpreter was 4% of the run

**This is the most important correction in this document.** Everything above
(the mix, the benchmarks, the projection) concerns what an instruction costs, not
how much of the emulator's time goes on instructions; on the title the exercise
started from, that share was **4.2%**.

A host CPU profile of that title's in-game session, taken through the load
probe's `WFEATURE_PERF_ROUTE`:

| | share of host time |
|---|---|
| `lgt.(*Client).syncToGuest` | 29.0% |
| `lgt.(*Client).syncFromGuest` | 26.3% |
| `runtime.kevent` — parking and waking guest threads | 16.5% |
| `armcore.Engine.Run`, everything under it | 4.2% |
| of which `fetch32`, the part a decode cache removes | 1.4% |

The ARM decode cache's ~48% of an ARM step is 48% of 4.2%: **the whole change was
worth two per cent of the run**. The two framebuffer syncs were worth two thirds
of it; `lgt.md` shows they copied 150 KiB in each direction around every drawing
call a Java title made, and over 1.7 million calls never once found a changed
pixel. Removing them took the same session from 366.6 to 41.0 nanoseconds a step,
150.9s to 16.9s, with every frame byte-identical.

The ARM cache remained the right *next* interpreter change, and the numbers above
still describe what it would do; it was not what the title needed.

**A guest profile and a host profile answer different questions, and this project
had been reading one for the other.** `-profile` says which guest instructions a
title executes, which is how its hot loops were found. Where the *emulator's*
time goes is a Go profile of a run driven to a scene, and it took one to find that
55% of it was an unnecessary copy. Ask for the host profile first, before any
throughput reasoning: the fastest possible interpreter would have moved this
title by four per cent.

The two failures above do not argue against the ARM cache. Both tried to make an
*already cached* path faster (widening an existing entry, blocking a loop that
already skipped its fetch) and lost to cache footprint and register pressure.
Adding the first cache to a path that has none is the change they were competing
against, not a repeat of them.

What to measure, in order, before believing any of it:

1. the ARM entry's footprint, from `Memory.DecodeCacheStats`: an ARM module is a
   megabyte where a Clet is a kilobyte of hot code, and the Thumb cache's
   0.1–0.5 MiB is not a prediction for it;
2. `ns_per_step` on the two ARM-heavy titles and, as the regression guard, on the
   Clets that are 99.8% Thumb and must not move;
3. only then the routed switch for ARM's ten commonest forms, the second half of
   what Thumb has, with its own A/B.

<a id="implementation-asked-again-after-the-copy-was-gone-and-the-arm-cache-is-now-the-answer"></a>

#### Asked again after the copy was gone, and the ARM cache is now the answer

The section above is right about the order of questions and about what that
title needed, but **it is not a standing verdict on the ARM cache**: the 4.2% was
a share of a run that was mostly a copy. Two later changes (the clock served
inside the quantum, and a draw syncing only the rows it can touch; see `lgt.md`)
took the heaviest local Clet from 19.8 to 8.4 nanoseconds a step, and what
remains of that run is the interpreter: a host profile has `Engine.Run` as the
whole of the real work, `handleWIPICSVC` at 2.7%, and `memmove` off the list.

**The slowest local title was now a different one, and 100% ARM.** By
instruction set, over 400 ticks of each of the five heaviest:

| title | ARM | Thumb |
|---|---|---|
| the new slowest | **800,084,285 (100.0%)** | 84,548 |
| the old slowest | 77,355,423 (6.6%) | 1,099,025,863 |
| three others | under 0.1% each | the rest |

It makes 627 platform calls in forty ticks, so it is not waiting on the platform,
and it ran at **17.97ns a step**: `BenchmarkEngineARMALULoop`'s 17.03 plus the
rest of a real run. Over three thousand ticks it spent 106.5s of host time
against 75.1s of guest time, **1.42x slower than the handset**, the only title on
this platform that was. The projection above (an ARM step landing near a Thumb
one) was the difference between that and comfortably under 1x, and the only
thing between this platform and every local title keeping up.

The order of measurement was unchanged, and the guard in step 2 mattered more:
the titles that must not move were now 99.9% Thumb rather than 99.8%.

<a id="implementation-the-arm-cache-was-built-and-it-halved-an-arm-step"></a>

#### The ARM cache was built, and it halved an ARM step

Built in the order the two sections above set out. Every number below is an
interleaved A/B of the same probe against the build before it, five rounds a
side, on `WFEATURE_LOAD_TICKS=300`.

**The footprint question answered itself; the megabyte never arrived.** The
100%-ARM title executes from **31 code pages** (124 KiB of code) over a run that
retires 328 million instructions, and the table committed for each is a quarter
of the page rather than twice it. Its whole decode cache is **47 KiB**, ARM and
Thumb together. The earlier fear (an ARM module is a megabyte where a Clet is a
kilobyte of hot code) concerned how much code a module *contains*; a table costs
what the guest *runs*.

**The ARM entry holds the form alone, where the Thumb entry also holds its
encoding**, which is what made the footprint a quarter rather than double. A
Thumb entry that carries its halfword saves assembling one from two bytes; an ARM
instruction is a word the page already holds in the host's byte order, so a copy
in the entry buys nothing. One byte of form per four bytes of code.

That was measured, because the Thumb precedent says the opposite (there the cache
pays for itself by *removing the fetch*). The eight-byte entry was built and run
beside the one-byte one on the same title: **9.65 nanoseconds a step against
9.65**, for **264 KiB of tables against 47**. A tie is a loss for the wider
entry, and the third time in this document that widening a decode entry failed
to pay; the other two are in "A wider decode cache entry was built and lost,
twice over".

The fetch is gone regardless: `fetch32`'s cost was never the four bytes but the
validation and mapping walk around them. A cached page is one the engine has
proved wholly executable, so the word is read straight from `page.data`, and the
eight bytes an entry could have held are eight bytes of cache line spent on the
next instruction instead.

| | ns per step | |
|---|---|---|
| before | 17.37 | |
| decode cache | 9.83 | **−43.4%** |
| and the routed switch | 9.39 | **−45.9%** |

(Over three thousand ticks rather than three hundred it settled at 9.42.)

Through `build/release/wfeature`, the binary a player runs and the one carrying
the committed profile, the same three thousand ticks were **15.09 nanoseconds a
step before and 8.95 after**. `BenchmarkEngineARMALULoop`, without a game around
it: **17.84 to 8.91 nanoseconds**.

The projection two sections up said ~5. The difference is the fetch: it assumed
an ARM entry would remove the word read as the Thumb entry does, and the
measurement above shows an entry that does costs the same at eight times the
memory. Of the 8.91, 4.65 is the operation and the remaining 4.26 a page-relative
word read plus a switch. **The projection's conclusion survived its arithmetic**:
an ARM step lands beside a Thumb one, for a different reason.

The routed switch is the second half, measured separately because it is small:
routing the two commonest forms out of the dispatcher into the engine's own loop
was −3.0%, and the next two −1.2% more. The forms were counted over 328 million
instructions of the all-ARM title:

| form | share |
|---|---|
| data processing | 49.6% |
| single transfer | 19.7% |
| `B`/`BL` | 14.1% |
| `BX` | **13.3%** |
| multiply | 2.0% |
| halfword/signed transfer | 1.3% |
| everything else | under 0.1% |

**`BX` at a seventh of the run is the shape of ARM code**: a module built for
interworking leaves every function through one, so the 14.1% of branches and the
13.3% of `BX` are the same calls counted going out and coming back. That is why
the routed set is four forms, not two; in Thumb, returns are `POP {..., pc}` and
land in an already-routed form.

**The regression guard held.** Across the four heaviest local Clets (1.4 billion,
1.1 billion, 383 million and 163 million steps, all 0.0–0.1% ARM) the change was
between −0.7% and +0.6%, the probe's noise floor. Three KTF titles, also Thumb,
were flat.

Correctness was checked by boot sweeps as well as unit tests. **128 LGT archives
(the 34 local ones and the 94-archive test corpus) ran 400 ticks under both
builds with `-framedir`, and `framediff` reported every frame of every one
byte-identical.** The 43 local KTF archives were compared by first lit frame and
its JSON summary, as the earlier local failure report describes: all identical
but one, which differs from *itself* between two runs of the same build, this
platform's noise floor rather than a regression.

It did not need a wider entry (the entry got *narrower* than Thumb's, and the
extraction that lost twice on Thumb was never attempted), and it did not need a
translator: the interpreter with a cached form is inside the range the third
translator shape was measured to reach, at none of its cost.

Three thousand ticks from a fresh boot then cost **24.9 seconds of host time
against 62 seconds of guest time, 2.49x faster than the handset**; the build
before took 44.9 against the same 62, or 1.38x.

**That is not the run the section above measured.** Its 106.5s against 75.1s was
5.9 billion steps, where a fresh boot retires 2.6 billion in the same three
thousand ticks, so it was a deeper scene than this probe reaches unaided. This
title has no committed route, so the deeper scene was not re-measured; what was
measured is the cost of a step, which both runs consist of.

<a id="implementation-the-host-profiles-next-answer-was-a-build-flag-and-it-was-12"></a>

#### The host profile's next answer was a build flag, and it was 12%

Superseded for the current recipe: see [armcore.md](../armcore.md#profile-guided-build).

Following the rule above (host profile before throughput reasoning), a host
profile of a Clet driven by `WFEATURE_PERF_ROUTE` through a field scene of the
title a person reported as slow pointed at something other than an interpreter
change:

| | share of host time |
|---|---|
| `armcore.Engine.Run`, everything under it | **93.3%** |
| of which `Engine.Run` itself — fetch, the watch and end compares, the switch | 23.5% |
| of which `decodedThumbFast` | 17.2% |
| of which every stand-in together (`runStoreLoop`) | 8.1% |
| `syncToGuest` + `syncFromGuest` | **0%** |

**This is the opposite title to the one above**, as the first and last rows show.
A Clet is 99.8% Thumb and does not go through the framebuffer syncs, so the
interpreter is the run, which is what makes a change to *how Go compiles the
interpreter* worth anything.

Go reads a pprof CPU profile from `default.pgo` beside a main package without a
flag. One was committed at `cmd/server/default.pgo` and `cmd/cli/default.pgo`,
taken from a real run rather than a benchmark.

| same route, same save, one machine (Ryzen 5 3600, linux/amd64) | before | after |
|---|---|---|
| an LGT Clet in-game, busy | 27.89s | **24.44s** (−12.4%) |
| its `ns_per_step` | 8.45 | **7.22** (−14.6%) |
| its tick p90 | 37.09ms | **31.52ms** (−15.0%) |
| its tick p99 | 64.06ms | 56.48ms (−11.8%) |
| a KTF title, a route to a scene | 20.78s | **18.19s** (−12.5%) |

**All 883 frames the LGT route paints were byte-identical** before and after, at
the same tick numbers. That is the whole safety argument: PGO changes inlining
and code layout and nothing a guest can observe.

**The profile is three runs merged, deliberately.** The first profile taken was a
single Clet; it moved that Clet by 14.5% and the KTF title by only 7.0%, because
the compiler had one title's shape and optimised the other paths at its guess.
`Engine.Run` has two halves that do not overlap (`executeARM` never appears in a
Clet's profile), so the committed profile was a Clet in-game, an AOT-compiled LGT
Java title in-game, and a KTF archive, symbolised separately and merged. That
nearly doubled the KTF gain and cost the Clet two points, the right trade: **a
profile that covers one path is worse than no profile for the paths it does not
cover.**

`make pgo` regenerates it. It needs local archives and routes under `var/`, which
are not in the repository, so it runs only on a machine that has them, and it is
told which on the command line because the files are one person's copies and
their names are the games'. At this revision all five had to be given:

```sh
make pgo PGO_LGT_CLET=var/games/lgt/<clet>.zip \
         PGO_LGT_CLET_ROUTE=var/routes/<clet-in-game>.route \
         PGO_LGT_JAVA=var/games/lgt/<aot-title>.zip \
         PGO_LGT_JAVA_ROUTE=var/routes/<aot-title-in-game>.route \
         PGO_KTF=var/games/ktf/<title>.zip
```

Each is one of the three paths through the engine: a Clet in its field, an
AOT-compiled LGT Java title in-game, and a KTF title's load. The two routes drive
the LGT runs to the scene being profiled, so a route that stops at a menu
profiles a menu. What the committed profile was taken from is not recorded, for
the reason the Makefile no longer names it; three titles of those shapes
reproduce it.

**There was no SKT run in it**: that platform had no load probe, and its
interpreter is `internal/jvm` rather than this one, so it was neither covered nor
harmed.

Two things this does not do, both commonly assumed:

- **It does not lower a session's CPU.** A session that cannot keep up runs with
  `TickFor`'s wait pinned at zero and therefore at 100% of a core, and a cheaper
  tick is spent on more guest speed rather than idling. CPU falls below one core
  exactly when the game reaches its own clock. See `session.md`.
- **It does not survive the code it was taken from.** A profile is a build input
  that ages: it names functions, and a rename or split silently drops that
  function's weight. Re-take it when the engine's shape changes, and treat the
  table above as the measurement to repeat rather than a number to trust.

**Nothing measured through `go test` sees it**, because a test binary is not a
main package and has no `default.pgo` beside it. Every `ns_per_step` in the load
probes is a no-PGO number; `go test -pgo=<file>` applies one for an A/B of the
profile itself. `testing.md`, "A probe does not measure the binary that ships",
records that trap beside the probes.

**Re-taken after the four changes below, the profile was worth the same 8%, and a
freshly taken one was worth no more than the committed one**: 4.716 against
4.694 nanoseconds a step on a Clet's field scene, and a wash on three KTF titles.
The committed profile was kept: it had not aged out, and the replacement would
have come from a machine whose profiler attributes most of a run to
`runtime.pthread_cond_signal`. `make pgo` changed instead: it now takes a save
tree per LGT run and a tick count per platform, because without a save the routes
replay from a fresh boot and profile a title screen, the mistake warned about
above in the form the tooling made easy.

**Re-taken on 2026-10-11 with SKT and the server's own work in it.** The committed
profile still dated from August 2026. Since then the server's per-frame picture
encoder had been written, and SKT, whose titles run on `internal/jvm` rather than
this engine, had never been profiled. `TestSessionCostProbe` in `internal/webhost`
plays a title through the shared session with a fixed key cycle and encodes each
picture and sound batch as the server does; `make pgo` now takes a KTF and an SKT
title through it in place of the KTF load probe, beside the two LGT routes. The
development machine is macOS, where the same runs had put 50–90% of their samples
on `runtime.pthread_cond_signal`; on one P (`GOMAXPROCS=1`) that disappears and
the KTF run shows `Engine.Run` at 82% of the profile, as the Linux profile above
does, so every run is taken that way.

Against the August profile: `go test -pgo=<file>` builds of the same source, two
alternating repetitions each on one machine (Apple silicon), every repetition
within 1% of its sibling and with the same retired instruction count or the same
frames on both sides. A profile taken earlier the same day by the same recipe, run
three times, gave −3.3%, −4.8% and −3.4% on the three routes and −2% to −8% on the
interpreter benchmarks:

| Fixed work | August profile | New profile | Change |
| --- | ---: | ---: | ---: |
| LGT Clet field route, ns per step | 4.735 | 4.560 | −3.7% |
| LGT AOT Java in-game route, ns per step | 30.15 | 28.61 | −5.1% |
| KTF title through the session probe, 600 ticks | 15.84 s | 15.28 s | −3.5% |
| `BenchmarkGuestLoop` (`internal/jvm`) | 8,223 ns | 7,470 ns | −9.2% |
| `BenchmarkGuestCallLoopInstance` | 413,268 ns | 384,676 ns | −6.9% |
| `BenchmarkEngineGameShapedLoop` | 4.85 ns | 4.78 ns | −1.3% |
| `BenchmarkFramePatchBandwidth`, protocol 2 | 206,969 ns | 207,450 ns | +0.2% |

Nothing got slower beyond noise. The encoder did not move: it is in the profile
now, and what PGO changes there is within noise. The SKT title chosen runs 93% in
the interpreter, so the profile has the interpreter's dispatch rather than a
title screen waiting on its clock.

<a id="implementation-why-a-translator-is-not-the-answer-even-with-unlimited-effort"></a>

#### Why a translator is not the answer even with unlimited effort

Regardless of effort, a translator loses, for reasons in the numbers above. After
the cache and the switch an ARM step is about 5ns, of which **4.65 is the
operation itself**; a translator could remove only the ~0.4ns of dispatch left.
The larger prize usually sought, specialising operands at translate time, is the
design measured in "A wider decode cache entry was built and lost, twice over",
where reading a pre-extracted field was *slower* than recomputing it from the raw
word, at equal width, by 9 to 10%.

Shipping decides the rest. A translator that emits machine code and marks a page
executable is excluded: `make dist` cross-compiles the server to five operating
systems from one machine without cgo, a project constraint (`AGENTS.md`). The
portable form, a closure per instruction, pays one indirect call per guest
instruction (measured at 0.9ns natively) against the ~0.4ns of dispatch it would
remove, so it loses before it starts.

(The browser is no longer a constraint: the page once ran the emulator in wasm
and no longer does; the emulator runs natively on a server and the phone is a
thin client, see `architecture.md`. The wasm measurements further down are from
that era and are kept because what they found about Go's wasm backend still
applies to the client.)

A translator is therefore worth at most a rounding error. The throughput left
after the ARM cache is in the operations themselves and in the 11.8% a Java title
spends in call thunks, not in dispatch, which will by then have been reduced
twice.

<a id="implementation-the-third-form-of-translator-measured--a-gate-rather-than-a-plan"></a>

#### The third form of translator, measured — a gate rather than a plan

The section above closes two of a translator's three shapes: a closure per
instruction loses on its own arithmetic, and emitting machine code is excluded by
how this ships. **It does not measure the third**, the one that would matter:
registers allocated to the host's, a compare feeding a branch instead of writing
an NZCV nothing reads, and memory reached through an inline page check. That form
attacks what an instruction *costs* rather than what finding it costs, and every
other number in this document concerns the latter.

`translator_gate_bench_test.go` is that measurement, built to be able to
**refuse** the design. It runs one real block chain, not a synthetic loop: the
run-length token decoder a local LGT title spends its interpreted time in, at the
address it occupies in its own module so its literal-pool loads resolve where they
do. A host profile of that title's field scene puts 93.3% of the run inside
`Engine.Run`, and the largest thing still interpreted there (the stand-ins cover
the pixel loops on either side) is this decoder's six addresses. One transparent
token costs seventeen instructions, which `TestTokenDecodeCostsSeventeenSteps`
holds to the code.

**The bar is 2.4x, and it comes from Amdahl.** Of that title's host time, 8.1% is
already in stand-ins and 6.7% is outside the core, leaving 85.2% a translator
could reach. The session needs 1.96x to reach its own clock, and
1/(0.148 + 0.852/N) = 1.96 at N = 2.4. The ceiling measured in "The block
interpreter, measured before building it again" (every trace of dispatch gone,
5.26ns to 3.20ns) is N = 1.64, which is **1.50x on the run and therefore short**.
That is why the third form had to be measured separately: the first two cannot
reach the bar even in the limit.

Two models were run, both required to leave the interpreter's registers and to
charge its step count (`TestTranslatedTokenDecodeMatchesTheInterpreter`), because
a benchmark against a faster thing that computes something else measures nothing.

| ns per guest instruction, five runs, Ryzen 5 3600 | | against the interpreter |
|---|---|---|
| interpreted | 9.746 | — |
| interpreted, stream in the literal pool's page (diagnostic) | 8.748 | — |
| translated, literals loaded every token | **0.167** | **58x** |
| translated, literals folded into immediates | **0.123** | **79x** |

**The gate does not refuse it, and the margin is what makes that credible.** The
model is an upper bound by construction (no block lookup, no block entry or exit,
no deoptimisation edge, and Go rather than emitted code), so the reading is not
"a translator would be 58x" but that **the model would have to be twenty-five
times too generous before this block failed the bar**, and no amount of block
overhead moves a number by that.

What it does *not* say matters as much. Amdahl caps the same title at 6.8x however
fast translation gets. The block chosen is the one most favourable to translation
in the title: seventeen guest instructions collapse to about nine host ones
because most are redundant (two literal loads that fold, and flag writes nothing
reads). The stand-ins stay ahead of a translator in the order things must be
tried: they are six times cheaper per instruction than interpretation because a
recognised fill is a slice fill, where compiled code is still one host store per
guest store. **A translator must not swallow a loop a recogniser can take.**

The remaining decision is a cost decision, not a throughput one: two code
generators; W^X on three operating systems (`MAP_JIT` and a hardened runtime
entitlement on the one this would most want to run on, which is not a signed
build today); and the seven things the interpreter carries that compiled code
would have to keep carrying: the step charge `SetStepBudget` and KTF's scheduler
are built on, the supervisor-call boundary and its lock, `Core.Watch`, the
debugger and the GDB stub, a page's compiled code dying with its decode cache when
its bytes change, the recogniser ordering above, and the frame byte-identity every
change here is judged by.

The diagnostic row is not part of the verdict; it became the next section. This
block reads two literals from its code page and two bytes from the sprite stream
every token. `Memory` keeps instruction fetch on a page of its own but has one
data page, so those two evict each other and every access takes `pageFor`. Giving
the stream the pool's page is worth 10.2% here: a second data slot's ceiling on
this block, which looked like the cheap change a translator is not.

<a id="implementation-the-second-data-slot-was-built-both-ways-and-lost"></a>

#### The second data slot was built, both ways, and lost

That 10.2% is a ceiling on one block, to be checked against the two cache changes
above. It was built twice and lost twice, the third such result in this document.

**Both slots checked in `mappedPage`.** `mappedPage` exists to be inlined (its own
comment says reaching the general path costs a Go call and the general path then
costs two more), and a second slot puts it over the budget:

```
cannot inline (*Memory).mappedPage: function too complex: cost 88 exceeds budget 80
```

| interleaved, same session | before | after |
|---|---|---|
| the token decoder above | 9.745ns | 9.012ns (**−7.5%**) |
| `BenchmarkEngineGameShapedLoop` | 7.758ns | 8.012ns (**+3.3%**) |
| `BenchmarkEngineLoadStoreLoop` | 10.943ns | 11.153ns (+1.9%) |

The target block gained 7.5% and the shape 97% of a measured title executes lost
3.3%, the trade refused everywhere else in this section.

**The second slot in `pageAt` only**, leaving the inlined path as it was. Clean
on the benchmarks (`mappedPage` still inlines, game-shaped −0.2%, load/store
−0.2%, the token decoder +5.4%), it still lost, and only a real title showed it:

| interleaved, both PGO builds | before | after |
|---|---|---|
| an LGT Clet in-game, busy | 23,856ms | 23,677ms (−0.75%, inside a 1.6% spread) |
| a KTF title, a route to a scene | 17.935s | 18.44s (**+2.8%**) |

The Clet's 5.4% on the benchmark arrives as nothing on the title, because the
decoder is a minority of its interpreted time and the stand-ins already own the
pixel loops around it. The KTF regression was the consistent number across the
four runs: that platform crosses the Java bridge constantly and misses both slots,
so it pays the second compare, the sixteen bytes and the shift on every miss and
is never repaid.

**A benchmark built to isolate a block reports that block's ceiling, and a ceiling
on a block is not a share of a run.** It is the guest-profile-for-host-profile
mistake from the other direction, and the real-title A/B exists to catch it.

<a id="implementation-the-third-shape-of-that-slot-is-an-index-and-it-wins"></a>

#### The third shape of that slot is an index, and it wins

Both designs above searched. One compared a second slot in `mappedPage` and
swapped the pair on a hit, putting the function over the inline budget; the other
kept the search out of the inlined path and left the miss as expensive as before.
**Indexing removes both problems**: the remembered pages become an array indexed
by the page number's low bits, so `mappedPage` does the same three compares, still
inlines at every call site, and a way costs one more mask. Nothing is searched and
nothing is promoted.

Its value was counted before it was built, over one title's field scene:

| | |
|---|---|
| accesses the single remembered page answered | 242,240,322 (**48%**) |
| accesses that missed it | 263,600,825 |
| of those, ones the page remembered *before* it would have answered | 117,044,184 (**44%**) |

Each access in the last row cost a call out of the inlined path and a walk down
the two-level page table, for an answer held one access earlier.

Swept on the same scene, host nanoseconds per guest instruction:

| ways | 1 | 4 | 8 | 32 | 64 | 128 | 256 | 1024 |
|---|---|---|---|---|---|---|---|---|
| ns a step | 5.68 | 5.56 | 5.47 | 5.31 | 5.30 | 5.28 | 5.29 | 5.28 |

**256 ways shipped rather than the 64 the sweep suggests, because of a synthetic
benchmark.** A blit between two surfaces half a megabyte apart alternates between
page numbers exactly 128 apart, and at 64 ways that is the same slot every time,
the one arrangement where remembering more pages remembers nothing.
`BenchmarkEngineBlitCrossPage` reads 7.23ns with one way, **7.61 with 64**, and
**5.23 with 256**, where the two surfaces land apart. A pair of framebuffers
allocated in order has exactly that stride, so the table is sized past it.

Folding the next bits in with an exclusive or fixes the same benchmark without
the extra ways, and was refused: it cost 1 to 2% on three real titles and 1.3% on
`BenchmarkEngineGameShapedLoop`, the shape most of a run consists of.

Otherwise the synthetic benchmarks cost about a percent (one more mask on the
fast path, and the page read from an array), and real titles decide whether that
is worth it. **KTF is the platform to read**: it is the one the second slot
regressed by 2.8%, and the framebuffer change below does not touch it (nothing on
this platform keeps a second copy of a surface), so these rows carry only the
ways, the byte fast path and the routed forms.

| ns a guest instruction, five KTF loads, interleaved, PGO on both sides | before | after |
|---|---|---|
| the heaviest local archive | 8.51 | **7.64** (−10.2%) |
| a second | 7.48 | **6.55** (−12.4%) |
| a third | 13.84 | **12.67** (−8.5%) |
| a fourth | 11.54 | **10.30** (−10.7%) |
| a fifth | 23.96 | **22.66** (−5.4%) |

All five retired the same instruction count as before. What changed is not how
many pages are remembered but that finding one costs nothing.

Byte loads and stores also take the inlined path now; `read8` and `write8` had
gone straight to the general path while the wider accesses had a fast path. That
is worth 1.9% on the Clet scene alone, and a byte load is how a title's own
rasteriser reads its source.

<a id="implementation-bulk-halfword-transfers-and-the-copy-they-were-for"></a>

#### Bulk halfword transfers, and the copy they were for

`Memory.ReadHalfwords` and `WriteHalfwords` move a run of little-endian halfwords
in one memmove. They exist because the LGT platform keeps the runtime's copy of a
Clet's surfaces as `[]uint16` and the guest's as bytes, and a Clet writes pixels
into the guest's directly, so every drawing call reads the surface back before it
draws and writes it out after (`lgt.md`, "A Java title's drawing does not
synchronise, and a Clet's has to").

The transfer was never the cost. Both directions allocated a 153,600-byte scratch
buffer per call and walked it a pixel at a time, rebuilding each `uint16` from two
bytes just copied. On one title's field scene that was **16.6% of the host's time
and 8.66 GB of garbage in twenty seconds**, 95% of everything the run allocated.
Guest memory is little-endian, as is every host `make dist` builds for, so the
halfwords a caller holds already *are* the guest's bytes; the loop and the buffer
were removed, and the reassembling form remains for a big-endian host, where it
is the slow path rather than a wrong answer.

The scene fell from 20.08s to 15.62s (**−22% for four lines**), with all 886
frames byte-identical and the same 2,735,911,562 guest instructions retired.
`lgt.md` has the counts and why the copy is cheaper rather than gone.

<a id="implementation-the-recognisers-were-allocating-a-description-per-attempt"></a>

#### The recognisers were allocating a description per attempt

Each stand-in's analysis built its `storeLoop`, `tableBlit`, `byteBlend` or
`wordModulate` with `new`, and analysis runs on **every backward branch a title
takes that has not already been refused**, including, on loops it recognises,
every execution. That was an allocation per fill: 350 MB over a fifteen-second
scene, second only to the framebuffer buffers above.

Each recogniser now fills one description held on the `Memory`. That is safe
because a description never outlives the call that filled it: the stand-in reads
it and returns, analysis runs under the execution lock, and no two of the four are
live at once.

**It moved the title by nothing measurable** and is kept anyway: 448 MB of garbage
became 103 MB, and what the collector costs depends on the machine running the
server, not the one it was measured on. An eight-core laptop collects that
concurrently for free; a small always-on mini PC does not.

<a id="implementation-six-more-forms-routed"></a>

#### Six more forms routed

`Engine.Run` routed ten Thumb forms straight to their handler and reached the rest
through `executeThumbForm`, a second switch on the same value. Over a Clet's field
scene the forms taking that second dispatch were **8.85% of everything it
executes**, and six of them were 97.5% of that: the high-register operations and
the literal loads at 77 million each, the two halves of `bl` at 26 million each,
and push and pop.

Routing those six was worth 0.8% on the Clet and 1.2 to 1.6% on two KTF titles.
Three had their semantics written inside `executeThumbForm`'s case body rather
than in a function, which routing cannot tolerate (a routed form written twice is
two answers to one encoding), so they became leaf functions first, as the branches
already were.

<a id="implementation-what-the-four-together-did-across-the-local-library"></a>

#### What the four together did, across the local library

None of the four changes anything a guest can observe; every run showed identical
instruction counts and, where captured, identical frames.

**One title's field scene**, reached by a route from a real save, release build
with the committed profile on both sides:

| | before | after |
|---|---|---|
| host time for the run | 18,371ms | **12,845ms** (−30.1%) |
| ns a guest instruction | 6.715 | **4.695** |
| guest instructions retired | 2,735,911,562 | the same |
| frames the route paints | 886 | all 886 byte-identical |

The same scene **paced the way a Host paces it**, the number a player feels:

| | before | after |
|---|---|---|
| host busy over a 52s scene | 21.74s | **18.46s** |
| tick p90 | 26.69ms | **21.20ms** |
| tick p99 | 43.94ms | **32.57ms** |

That title's frame is 55ms of guest time, so the tail went from 11ms of headroom to
22ms. A machine that keeps up either way spends a third of a core instead of two
fifths; one that does not keep up gets the whole difference as speed.

**All 34 local LGT archives**, 3,000 ticks each from a fresh save (a boot and
whatever a title settles into):

| how much faster | archives |
|---|---|
| −73% to −89% | 5 |
| −20% to −31% | 6 |
| −9% to −19% | 19 |
| about −2% | 3 |
| slower | 1 |

Every one retired the same instruction count as before. The five that collapsed
are the titles that draw through `MC_grp` most (the largest went from 88.4s to
9.4s), which shows the framebuffer copy was the whole of their cost rather than a
share. Two of the three flat rows are 49ms runs, too short to read.

**The slower one was measured five more times and the change kept.** The sweep row
read +5%; interleaved at 3,000 ticks it was +2.6%, +3.1% and +0.8%, and at 1,500
ticks +1%, inside this title's own noise floor (the same archive varied by 7%
between two runs of the *unchanged* binary). Both changes that could plausibly
cost it were checked separately and both *help* it: one way instead of 256 costs
it 6%, and removing the byte fast path costs it 2%. The remainder is under the
floor and was not pursued. It is a Clet at 17ns a guest instruction, four times
the cost of the scene above, so almost none of its host time is in the
interpreter.

<a id="implementation-a-fifth-shape-the-blit-that-keeps-its-destination-on-the-stack"></a>

#### A fifth shape: the blit that keeps its destination on the stack

The four above were built and measured against LGT titles. A KTF title reported as
laggy in its busiest scenes showed what they miss: counting every backward branch
that closes a loop over a route through the reported scene, **none of the eighteen
busiest was stood in for**. The top two close 149 million and 95 million times
between them and are most of what the title executes.

Reading them shows the same three obstacles each time, none about what the loop
computes:

- **The destination lives in a frame slot.** Every pixel loads it from the slot,
  stores through it, adds two and writes it back, so no register holds the
  destination for `table_blit.go` to assign a role.
- **The palette base is reloaded every pixel** (`ldr rP, [sp, #a]` then
  `ldr rT, [rP, #b]`), and the register it lands in is reused for the destination
  before the loop closes, so reading it at the end reads the wrong value.
- **A flag decides the loop's shape and is re-tested every pixel.** The blit is
  the fall-through of `ldr rG, [rB, #c]; cmp rG, #0; bne blend`, and a branch
  inside the body made every recogniser refuse outright.

The third is the cheap one: **a recogniser runs at the backward branch**, which is
reached only by falling through the guard. The guard has therefore been evaluated
for this iteration and not taken, and if everything it reads is something the loop
cannot change, it will not take on the remaining iterations either. Proving that is
a check per register rather than an evaluation. At run time it costs one range
check: the word the guard reads, the slot the destination lives in and the record
the palette hangs off are read once and treated as constants, so a blit whose
destination covers any of them goes back to the interpreter.

`spilled_blit.go` is that shape. On the reported title, the route that replays the
scene, release build:

| | before | after |
|---|---|---|
| the route, host wall clock | 63.7s | **51.9s** (−18.6%) |
| the heavy scene, host cost a round | 26ms | **20ms** |
| the same scene, share of a core | 92% | **73%** |

The scene stopped being throughput-bound: the frame period the guest asks for paces
it, not what the Host can produce. The route replayed to the same 2,168 ticks and
2,179 flushes, and eight KTF and four LGT archives rendered byte-identical first
frames.

**Still refused in the same title:** the second-hottest loop is the same blit with
a clip test per pixel (two bounds the loop cannot move and an index walking
between them), which is a range intersection rather than an invariant. Below it is
a constant halfword fill, a shape `fill_loop.go` already knows, refused only for
the guard above it and for an ending that compares the counter against a register
holding −1 rather than zero. Neither was built at this point.

<a id="implementation-the-sixth-is-the-same-blit-with-a-clip-test-per-pixel"></a>

#### The sixth is the same blit with a clip test per pixel

The second-busiest loop in the same title (95 million closings against the fifth
shape's 149 million) is that blit with three tests in front of it:

	ldr rL, [sp, #a] ; cmp rX, rL       ; blt tail   — off the left of the clip
	ldr rH, [sp, #b] ; cmp rH, rX       ; ble tail   — off the right of it
	ldr rR, [sp, #c] ; ldr rF, [rR, #d] ; cmp rF, #0 ; bne blend

The body has three exits, two of which rejoin it: a pixel outside the clip skips
the store and still advances the source and the index, and the destination
advances only where a pixel was written.

**The clip is not an invariant.** The fifth shape's guard reads only things the
loop cannot move, so one fall-through proves it for the rest of the run. Two of
these three tests also read the index, which the loop advances. They are
tractable because it advances one way and by one: the pixels the tests pass form a
single contiguous stretch whose bounds are arithmetic rather than a search. A run
is at most three phases (skipped, drawn, skipped), and the middle one is the blit
the fifth shape already handles.

**Each path costs a different number of steps.** The step count must be what the
guest would have retired, or a run that should have hit its budget does not, so the
three phases are charged separately: a pixel off the left runs the first test and
the tail, one off the right runs two tests and the tail, and a drawn pixel runs the
whole body. `clipped_blit.go` carries all three.

On the same title and route, against the fifth shape alone:

| | before | after |
|---|---|---|
| the route, host wall clock | 54.6s | **44.2s** (−19.0%) |
| the heavy scene, host cost a round | 20ms | **13ms** |
| the same scene, share of a core | 51% | **41%** |

The same 2,168 ticks and 2,179 flushes, and twelve KTF and eight LGT archives
rendered byte-identical first frames.

<a id="implementation-the-seventh-was-built-measured-and-is-not-kept"></a>

#### The seventh was built, measured, and is not kept

The third-busiest loop in that title is a counted halfword fill behind the same
flag: `fill_loop.go`'s own shape, refused for the guard above it and for an ending
that compares the counter against a register the body builds −1 in rather than
against zero. Thirty-one million closings; a recogniser for it is a hundred and
eighty lines.

**It cost 1.9% and returned nothing**, for a reason that belongs to the chain
rather than the shape. Instrumented over the route, the analyser accepted the loop
13.9 million times and the stand-in ran 78 thousand of them. The rest declined on
the flag: this fill runs mostly in its blending form, where the guest leaves the
loop by a path the stand-in cannot follow.

**A refusal by analysis is cached in the branch's decode entry; a decline by the
run is not, and must not be**, because caching "too few iterations this time"
would write the loop off for the session. A shape that is recognised and then
declines therefore pays every analyser in the chain on every execution, whereas
the same loop with no recogniser pays one bit test. Adding the seventh turned a
cached refusal into 13.9 million six-analyser walks.

The two blits avoid this because they rarely decline: over the same route the
fifth ran 9.29 million of the 9.33 million loops it accepted, and the sixth 3.60
million of 5.81 million. **Before building a recogniser, look not at how often the
shape appears but at how often the run will decline after the analysis has been
paid for.**

**It was built later, and the reasoning above is why that became possible.** The
decline was never the shape's fault: the fill spends its life in a blending arm
nothing could follow. Once that arm could be followed there was no decline to pay
for, and the same loop turned out to be the busiest in the title; see "The ninth
shape, and the loop census that found it".

<a id="implementation-the-flag-the-fifth-shape-assumed-and-the-decline-the-sixth-paid-for-twice"></a>

#### The flag the fifth shape assumed, and the decline the sixth paid for twice

The report the fifth and sixth shapes were built for came back: the scenes they
targeted were no longer the slow ones, and a different kind of scene still was.
The same route through the same title, per tick of host cost:

| the route's stretches | before the fifth and sixth | now |
|---|---|---|
| the stretch the route names as the one to measure | 63.0ms a tick (debug) | **11.3ms a tick** |
| the stretch nobody had measured | — | **114ms a tick, 16.7M instructions** |

Reading the second stretch found two defects in the shipped shapes, both about the
same word.

**The fifth shape assumed its flag rather than reading it.** Its walk proves the
guard's own exit leaves the body, and the file argued from there that the backward
branch is reached only by falling through the guard. That does not follow: the
walk sees where the exit *goes*, not where the code there goes next, and the form
this shape was built against sends its blending arm to a block that draws one
pixel through a call and branches back into the body **after** the store. The
loop then closes with the flag set, the analysis is of the arm that did not run,
and the stand-in blits the rest of the row unblended. On that title's route it was
71 stand-ins in 806 ticks: rare, because the flag is usually clear, and wrong every
time. `spilled_blit.go` now reads the word, as `clipped_blit.go` always did, and
`TestASpilledBlitWhoseGuardIsSetIsRefused` covers the arrangement no earlier test
had: a blending arm that rejoins the body.

**The sixth shape declined correctly and paid for it once a pixel.** A refusal by
analysis is cached in the branch's decode entry and a decline by the run is not,
for the reason above. The flag is a different kind of decline: it holds for every
pixel of a blended run, and each pixel closes the loop again and walks the whole
chain to be told the same thing. Over that route there were **1.48 million
declines**, all inside the scenes a person calls slow.

The flag's address is now remembered with the branch that declined on it, and a
later attempt reads that one word instead of running six analysers. It cannot be
wrong in the direction that matters (**a decline only hands the loop to the
interpreter, which would have run it anyway**), and a zero word falls through to
the full chain, so a loop that returns to its plain form is stood in for on its
next iteration. `TestAFlagDeclineIsForgottenWhenTheFlagClears` pins that half,
the one a cached decline would get wrong. Interleaved on the reported scene, with
identical instruction counts on both arms:

| | before | after |
|---|---|---|
| the heavy stretch, host cost a tick | 118.7ms | **114.7ms** (−3.3%) |

All 34 local LGT archives and 42 of the 43 local KTF archives rendered
byte-identical first frames; the forty-third opens on neither side. The route
replayed to the same 2,168 ticks and 2,179 flushes with all six captured frames
identical.

<a id="implementation-what-is-left-in-that-scene-and-why-no-walk-reached-it"></a>

#### What is left in that scene, and why no walk reached it

The 3.3% was all that was available without new arithmetic; the remaining 96% is
one structure. Per tick of the heavy stretch, from the guest profiler with the
marks the route carries:

| share of the tick's instructions | what it is |
|---|---|
| 16.1% | a **pixel-writer dispatcher**: sixteen modes behind a jump table, entered once per pixel |
| 29.9% | the blend it dispatches to for the mode that scene uses — a per-channel alpha blend of two packed pixels, in both 5-5-5 and 5-6-5 |
| 7.8% | a second blend, per-channel through two lookup tables the title holds |
| ~10% | the blit loops themselves, around those calls |

**The title draws its full-screen effects one pixel per function call, two calls
deep.** A blended pixel costs about ninety guest instructions where a plain one
costs six, and the count is exact: 1.43 million blended pixels over the route,
about 70,000 in each heavy tick, which is one screen.

No walk reached it, for a structural reason: a body with a `bl` in it is one none
of them can reason about, and the callee holds all the cost. The only stand-in
that helps is one that **computes the blend itself**, in Go, having read the
dispatcher, the jump table, the handler for the mode in force and the blend body
behind it.

"Two more shapes" declined to cross that line for the run-length draws, by the
criterion that decides this case too: the case for a recogniser is how much of a
reported-slow scene it covers. The run-length draws were 4.6%, 4.1% and 2.9%. This
is 54% of the scene's instructions before the loops around it, the difference
between a scene that keeps its frame rate and one that does not. The eighth and
ninth shapes below were built for it.

Two measurements taken first decided the design:

- **The mode is the flag the blits already read.** The word the guard tests at the
  top of the loop is the word the dispatcher indexes its jump table with, so a
  stand-in knows which blend it will reproduce before it starts, and the remembered
  decline above already has the address.
- **Which mode matters was counted.** Per pixel over the route, the clipped blit's
  blended pixels were 63.6% one mode, 36.0% another, 2.3% a third and 0.2% a
  fourth; weighted by the scene rather than the route, the profile puts the alpha
  blend at 29.9% against the table blend's 7.8%. Two modes would have covered the
  scene, which made hand-writing the arithmetic look feasible; the fold made it
  unnecessary.

<a id="implementation-the-charge-a-quantum-could-not-hold"></a>

#### The charge a quantum could not hold

Standing in for the blending form (below) made the guest draw a different frame.
The pixels written were identical; the steps charged for them were not.

A stand-in charges for every instruction the loop it replaced would have retired.
`Engine.Run` answered `count` on the exhausted path, so a charge larger than the
quantum was **silently truncated to the quantum**. A fill of a few hundred pixels
overran a thousand-step quantum slightly and went unnoticed; the blended blit
charges a hundred and thirty-five steps a pixel and covers a row, thirty times the
quantum, so thirty-one thirty-seconds of the charge went missing. The Host
schedules on that number (`ServiceSteps`, the timer round, the budget window), so
the guest was handed work it had already done the equivalent of, and by the
hundred and thirty-eighth tick of a replay it was somewhere else entirely.

The exhausted path now answers what it retired, overrun included;
`TestAQuantumReportsTheChargeItOverran` pins it. Two consequences:

- **Instruction counts in this document taken before this change are low**, by
  whatever each title's stand-ins overran. The same route that read 3,229M reads
  **3,504M** afterwards, an 8.5% under-count entirely in the recognisers.
  `ns_per_step` moves with it in the same direction.
- The paragraph in "Two more shapes" advising comparison by busy time rather than
  `ns_per_step` described this defect. It is fixed, and the comparison is honest
  either way.

<a id="implementation-the-eighth-shape-the-pixel-a-blit-draws-through-a-call"></a>

#### The eighth shape: the pixel a blit draws through a call

`blended_blit.go`. Both blits above are the fall-through of a mode word, and when
it is set the guest leaves for an arm that draws one pixel **through a call** and
branches back into the body. A full-screen effect runs that arm, and the call holds
the cost: a blended pixel is about a hundred and thirty-five guest instructions
where a plain one is six, and a heavy tick draws seventy thousand of them, one
screen.

No walk can read a body with a `bl` in it, so the only useful stand-in performs
what the callee performs. There are two ways, and the choice is the design:

- **Reproduce the arithmetic in Go**, matched sequence by sequence as
  `word_modulate.go` matches its blend. Fastest, and specific to one title: the
  writer here dispatches sixteen modes through a jump table, each a different
  arithmetic in a 5-5-5 and a 5-6-5 form. Four sequences for the two modes one
  scene uses, and a fifth title would need a fifth.
- **Fold the call and compile what is left.** The writer is walked once per
  stand-in with the destination halfword and the colour as its only unknowns.
  Everything else it touches (the module's GOT, the mode, the jump table, the
  alpha, the mask literals, the format flag) is a word the loop cannot move, so it
  folds to a constant and the branches it decides fold with it.

The second was built, justified by the fold ratio. **Most of a per-pixel writer is
not arithmetic**: of the writer this was built against, 107 guest instructions
compile to **62 operations** for one mode and 97 to **57** for another, and most of
those are the blend proper; the prologue, the dispatch and the format test are
gone. No sequence matcher would remove those from a title whose blend it did not
already know.

It is not a translator, by the distinction "Why a translator is not the answer"
draws: the gain is the folding, not the dispatch. It compiles one straight-line
leaf reached from one loop, under the rules every stand-in follows: spans validated
whole, every word treated as constant checked against the destination, the charge
equal to what the guest would have retired, and anything the walk cannot prove
handed back.

**What the walk refuses is what makes it safe.** The writer must leave the caller's
registers and stack where it found them, which the walk proves by modelling its
frame; every branch must be decidable from what has already folded, so a writer
with a pixel-dependent loop is refused rather than approximated; and it may write
**one halfword, to the pixel it was handed, and nothing else**
(`TestAWriterThatStoresElsewhereIsRefused`).

Two rules on the loop side:

- **The arm's rejoin is read, not assumed.** It must land where the destination
  advances, and `isWriteBack` reads those three instructions rather than trusting
  an offset.
- **The iteration left to the interpreter must be one that draws.** Leaving the
  run's last iteration makes the registers a writer clobbers right for free, but
  the last iteration of a run that ends outside the clip does not call the writer,
  and two of those registers then keep whatever the stand-in left. The clipped form
  therefore stops one short of the last *drawn* pixel and leaves the rest, skipped
  pixels included, to the interpreter. Two runs of one blit differing only in where
  the clip fell found it.

On the reported title, interleaved on the route that replays the scene, **identical
instruction counts on both arms and all 806 ticks byte-identical**:

| | before | after |
|---|---|---|
| the heavy stretch, host cost a tick | 115.6ms | **94.9ms** (−17.9%) |
| the replay as a whole | 18.29s | **17.28s** (−5.5%) |
| guest instructions retired | 3,504,405,000 | the same, to the instruction |

All 34 local LGT archives and 42 of the 43 local KTF archives rendered
byte-identical first frames; the forty-third opens on neither side.

**What it does not reach, measured.** The writer is called from seven places in
that title. This covers the two that are blits these recognisers already read, and
the heavy scene still spent about a third of its instructions in the writer,
reached from loops that are **not** blits of either shape. Counting every backward
branch that closes a loop over the route identified the next one to build, which
was not a blit (below). One mode's writer is refused by the walk: the unclipped
blit's arm compiles for every mode the reported scene uses and declines one that
appears elsewhere in the route, which costs that stretch nothing it was not
already paying.

<a id="implementation-the-ninth-shape-and-the-loop-census-that-found-it"></a>

#### The ninth shape, and the loop census that found it

The heavy scene was still 94.9ms a tick against a 64ms frame period, and the guest
profile said where (the writer) but not which loop called it. **A profile ranks
addresses; what was needed was a ranking of loops.** Counting guest steps between
two closings of the same backward branch, over the whole replay and including
branches already marked refused, gives that directly:

| loop | closings | guest instructions | an iteration |
|---|---|---|---|
| a counted halfword fill behind the flag | 19,445,259 | **967,665,411** | 49.8 |
| the unclipped blit | 2,256,293 | 686,917,305 | 304.4 |
| the clipped blit | 612,079 | 239,915,014 | 392.0 |
| four others | — | under 155M each | — |

The busiest loop in the title by far, at **28% of every instruction the replay
retires**, is neither blit. It is `fill_loop.go`'s own shape behind the same flag:
**the seventh shape**, described in "was built, measured, and is not kept".

That section's reasoning was right and its conclusion had expired. It was rejected
because the analyser accepted the loop 13.9 million times and the stand-in ran 78
thousand of them: the fill lives in its blending arm, and a recogniser that
accepts and then declines pays every analyser in the chain on every execution.
**Following the arm changed that.** With the writer compiled there is no decline
left, because the recogniser answers for the flag set and clear alike (the plain
fill *and* the blended one, one shape) with no path back to the interpreter to pay
for.

`blended_blit.go` carries it. The arm is smaller than a blit's (the colour is
loop-invariant, so it is a few moves and a shift rather than a palette lookup), and
the three forms the walk allows there are the three a compiler emits to put a value
in place, replayed over the loop's own registers to get the argument the writer
would have received.

On the reported title, same route and probe, **identical instruction counts and all
806 ticks byte-identical**:

| | before the eighth | + the eighth | + the ninth |
|---|---|---|---|
| the heavy stretch, host cost a tick | 115.6ms | 94.9ms | **37.6ms** |
| its p90 | — | — | **49.1ms** |
| the replay | 18.29s | 17.28s | **11.27s** |

**A tick of this scene is 64ms of guest time, so it went from missing its frame
period by half to keeping it with a quarter of the period in hand.** Over the full
2,168-tick route every one of the three marked stretches sits under 35ms at p99,
where the worst had been 94ms at p90.

Whole-corpus controls: all 34 local LGT and 42 of the 43 local KTF archives
rendered byte-identical first frames, and two other KTF titles driven through their
own routes were unmoved to within 1%, so it costs nothing where it never fires.

<a id="implementation-the-supervisor-call-boundary-and-the-slots-that-do-not-need-it"></a>

### The supervisor-call boundary, and the slots that do not need it

A supervisor call ends the quantum, which is what makes the boundary safe: the
memory lock is released so the handler can read and write guest memory, the
execute lock with it so another guest thread can run, and the calling thread is
suspended so the handler reaches its registers through the same mutex a Host does.
A handler that blocks on a browser event, calls back into the guest, or allocates a
page needs every part of it.

**A slot that reads a number the platform already holds needs none of it, and a
title that polls one pays for all of it.** One LGT title's own loop reads the
platform clock seventy-seven thousand times per tick (3,096,685 of the 3,171,517
platform calls in forty ticks), so nearly every quantum on that run was a few tens
of instructions long, and the run's cost was the boundary rather than the call.

`Core.SetFastSupervisorCall` installs a handler the engine consults *inside* the
quantum. Returning false leaves the call to the ordinary path, so a platform opts
in one slot at a time and everything else is unchanged.
`BenchmarkCoreSupervisorLoop` and `BenchmarkCoreFastSupervisorLoop` run the loop in
question (a few ALU instructions, a stub that raises SVC, and a branch back) at two
call densities, separating the crossing from the instructions around it:

| guest instructions between calls | ordinary | inside the quantum |
|---|---|---|
| 13 | 258.5ns | **97.1ns** |
| 41 | 357.3ns | **249.2ns** |

That is about **210ns of boundary against about 25ns**. The price is what the
handler must not do: it runs with the memory lock and the execute lock held, so it
cannot touch guest memory, re-enter the guest, or take a lock either could be
waiting behind. It answers by writing the `Context` it is handed.

<a id="implementation-the-allocation-every-crossing-made"></a>

#### The allocation every crossing made

The boundary above is what a crossing *costs*; this is what one *allocated*, since
the engine was written. The two SVC forms answered with `&SupervisorCall{...}` and
every other form with `nil`, so a raised call was one heap object per crossing,
read once by `Engine.Run`, copied by value into its `RunResult`, and dead before
the next instruction.

An allocation profile of one local KTF title over two thousand ticks put
`executeThumbConditionalBranch` at **2.8 million allocations, 63% of every object
the run made**, 43 MB of sixteen-byte structs. The crossing rate makes it matter: a
title that polls the platform inside its own loop raises one every few tens of
instructions, and the allocation happened *before* the fast path was consulted, so
slots that never leave the quantum paid for it too.

The forms now answer by value. `SupervisorCall.raised()` replaces the nil test: a
raised call resumes after the instruction that raised it, so its `ResumePC` is that
instruction's address plus two or four and never zero, which makes the zero value an
unambiguous "no call". The engine's two call sites lost a dereference along with the
allocation.

**This is not a speed measurement.** The machine it was found on was running an
unrelated full-corpus sweep at a load average of 127, which makes every wall-clock
number on it noise; allocation is counted rather than timed. The instruction count
shows the engine executes the same program: four local KTF titles and two LGT ones
run before and after on a manual clock retired identical instruction counts and
flushed identical frame counts. **Its value in time is unmeasured** and needs a
quiet machine.

<a id="implementation-the-hook-cannot-live-on-engine-and-the-reason-is-worth-keeping"></a>

#### The hook cannot live on Engine, and the reason is worth keeping

The natural home is a field on `Engine`, whose loop consults it. **That made every
title slower**: `Engine` is a zero-size struct, and one function-pointer field costs
the interpreter's own loop, on the local LGT archives at 400 ticks, best of three
against a 1% noise floor:

| | with the field on Engine | with it on Memory |
|---|---|---|
| a drawing title | +15.4% | −0.9% |
| a second one | +4.4% | +1.9% |
| a third | +6.9% | +1.2% |
| the clock-polling title | +2.0% | **−24.3%** |

The field is on `Memory`, which the engine already holds as a pointer and
dereferences every instruction. `BenchmarkEngineGameShapedLoop` shows the same at a
tenth of the cost of finding it (184.8 MIPS against 174.6, a 5.5% drop) while
`BenchmarkEngineALULoop` is flat: **benchmark the shape of code that runs**.

<a id="implementation-why-the-browser-was-seven-times-slower"></a>

### Why the browser was seven times slower

The same Thumb loop cost 9.0ns an instruction natively and 63.5ns in wasm, the
largest single remaining gap, applying to every game.

The first suspect was wrong. Go on wasm passes arguments and results through linear
memory rather than registers and wraps every call site in resume machinery, so a
call should be expensive; measured, an empty call cost 4.2ns against 0.9ns natively,
and removing one call per instruction returned the same half-nanosecond on both.
Calls are not wasm's weakness; *volume* is. A V8 profile of the benchmark showed no
runtime overhead, only the interpreter's own functions, and the wasm each compiles
to runs roughly one operation per cycle. A guest instruction was around 200 wasm
operations where native ARM64 was 29.

The work was to emit fewer operations, and two patterns accounted for most of them.

`Context.PC`, `Thumb`, `carry` and `Register` had **value receivers**. `Context` is
sixteen registers and a CPSR (68 bytes), and Go does not decompose an array that
size, so each call copied the whole thing even when inlined, to read one word. The
interpreter loop called two of them per instruction and the data-processing forms a
third. Pointer receivers cut the ALU loop nearly in half on both backends.

The flag setters each read CPSR, masked it and wrote it back, so one `ADD` setting
N, Z, C and V made four round trips through the context pointer to produce one
value. `setNZCV` and `setNZC` compute it once and store once. Natively that is a few
cheap instructions either way; on wasm the address of a pointer field is computed in
64-bit arithmetic and narrowed to 32 for every access, so each round trip is five
operations rather than one.

The rest was reaching the general path for answers that are three compares.
`decodedThumbFast` and `mappedPage` are deliberately small enough to inline; every
condition they check is one the general path also checks, so a negative answer only
means "ask the general path", which is unchanged. The decode cache became a
`*[decodedPerPage]decodedThumb` rather than a slice so that a masked, halved page
offset is provably inside it: the type carries the bound the bounds check had been
re-deriving per instruction.

A report's `paints_dropped` counts **rounds that ran with their paint suppressed,
not frames lost.** A guest that spins rather than parking runs tens of thousands of
rounds a second, so the count inflates without the screen necessarily losing
anything; `paint_load` beside it says whether the Host was behind.

`SetDecodeCacheEnabled` turns the decode cache off and `DecodeCacheEnabled` reports
whether it is on. It is a diagnostic switch, not a tuning knob: the hypothesis that
the browser was thrashing its working set was tested by running without the cache,
and without it the browser was twice as slow again, as was the desktop. The switch
stays so the measurement is repeatable, and the uncached path still checks execute
permission, so it measures the same interpreter. It is reachable from Go only; the
browser export that flipped it went with the in-page engine, and the machine whose
working set was in question no longer runs the interpreter.

Per instruction, after:

| | native before | native | wasm before | wasm |
|---|---|---|---|---|
| ALU loop | 8.8ns (114 MIPS) | **4.7ns (213)** | 66.6ns (15) | **27.6ns (36)** |
| load/store loop | 11.2ns (90) | **6.1ns (163)** | 67.0ns (15) | **37.5ns (27)** |
| game-shaped loop | 9.0ns (111) | **4.9ns (206)** | 60.5ns (17) | **27.8ns (36)** |

wasm went from 6.7x native to 5.7x, and the browser ran guest code 2.2x faster than
before. On real games the gain is proportional to how much of a workload is guest
ARM: rendering the first frame of every local archive (eighteen at the time, with
loading and JVM work as much as interpretation) went from 17.1s to 12.1s in wasm and
3.7s to 2.9s natively.

None of the above was visible from the Go side. The wasm numbers were taken by
building the test binary, running it under node's profiler, and aggregating self
time per frame. The emulator no longer ships to the browser, but the measurements
rest on this recipe:

```
GOOS=js GOARCH=wasm go test -c -o /tmp/armcore.wasm ./internal/armcore
node --cpu-prof --cpu-prof-dir=/tmp/prof "$(go env GOROOT)/lib/wasm/wasm_exec_node.js" \
  /tmp/armcore.wasm -test.run='^$' -test.bench=BenchmarkEngineGameShapedLoop
```

`GOOS=js GOARCH=wasm go build -gcflags=-S` prints the wasm each line compiles to,
which is how the 64-bit address arithmetic and the per-call spills were found;
`-gcflags=-m=2` reports why a function missed the 80-node inline budget.

<a id="implementation-what-was-tried-after-that-and-did-not-work"></a>

#### What was tried after that, and did not work

The remaining gap is not uniform: pure arithmetic in wasm runs at native speed, an
array-and-branch loop at 2.5x, and this interpreter at 6x. The bottleneck is
linear-memory traffic rather than arithmetic, and **the 6x does not close by
degrees**: three separate attempts returned between zero and four percent. Each of
these was measured and is not worth repeating:

- **`page.data` as a `*[4096]byte` to remove every bounds check.** The generated
  code, inspected first, already had *no* bounds checks left, so it bought nothing.
- **Masking the page offset to induce bounds-check elimination**, for the same
  reason.
- **A four-way associative mapping cache in the core.** The one-entry cache already
  answers nearly every access.
- **`GOGC=400`.** Not where the time goes.
- **Turning the decode cache off**, a diagnostic switch rather than a tuning one:
  the desktop went from 5.26x to 2.42x slower, the wrong direction.

Closing it would take translating guest basic blocks to wasm, a project of its own.
The requirement that made browser speed urgent (running on a phone) is met by the
server session, so that project is worth starting only if running entirely inside a
desktop browser becomes a requirement.

<a id="implementation-guest-sampling-profiler"></a>

### Guest sampling profiler

`Core.EnableProfile(interval)` records where guest code spends its instructions. It
is off unless switched on, because the cost is a stack walk and a run not under
investigation should not pay it.

Two decisions make the numbers meaningful.

Samples are taken per executed instruction, not per quantum. Quanta end on every
supervisor call, so a game that calls into the Host constantly would otherwise be
sampled hundreds of times more often than one that does not, at the same
instruction count.

The quantum is also *shortened* to land exactly on the sample interval. Without
that the sample count is proportional but the sampled address is not: execution
stops at supervisor calls, so the recorded PC is nearly always the instruction
after one. On one title that artefact put 37.9% of the profile inside the platform
stub region at `0x31000000`; ending the quantum on the interval dropped the region
to 0.8% and moved the weight onto the game's own sprite blit, where the
instructions were being spent.

Each sample is the PC plus the return addresses above it, walked through the Thumb
r7 frame chain (`[r7]` is the caller's r7, `[r7+4]` its return address). The walk
is best effort (a leaf that has not built its frame yet, or hand-written assembly
that keeps none, yields a shorter stack) and stops on a return address without the
Thumb bit, on a chain that fails to walk upwards, and at 32 frames. The leaf PC is
always right, which is what the instruction-level ranking needs.

Distinct stacks are capped at 65536. Past the cap a sample is cut back to its leaf
PC and still counted, so a game with unbounded stack shapes loses its callers
rather than its samples; `Profile.CallersDropped` reports how many.

`ResetProfile` clears the samples and keeps sampling, so a profile can cover a
scene rather than the minutes of loading before it.

<a id="implementation-implemented-instruction-boundary"></a>

### Implemented instruction boundary

The first interpreter slice includes:

- A32 condition execution; data-processing and flag operations; multiply and long
  multiply; `B`, `BL` and `BX`; `SWP`/`SWPB`; user-mode `MRS`/`MSR`; single and
  multiple word/byte transfers; `STRH`, `LDRH`, `LDRSB` and `LDRSH`; and SVC
- Thumb shifts, arithmetic and ALU operations, high-register operations and
  interworking, literal/register/immediate/stack-relative transfers, push/pop,
  multiple transfers, conditional and unconditional branches, long
  branch-with-link, long branch-with-link-and-exchange, and SVC
- ARM7TDMI multiple-transfer ordering when the writeback base is also in the
  register list, plus the pipelined `pc + 12` value stored by A32 `STM`
- bounded quanta, total retired-instruction diagnostics, end-address stops, and
  instruction errors containing the PC, encoding and execution state

<a id="implementation-the-hint-space-and-why-a-thumb-1-module-has-an-armv6-instruction-in-it"></a>

#### The hint space, and why a Thumb-1 module has an ARMv6 instruction in it

`0xbf00` and its four neighbours (`NOP`, `YIELD`, `WFE`, `WFI`, `SEV`) are one
encoding with the condition field naming which. None has anything to do on one
core with no interrupts and no event register, so all five execute as nothing; what
matters is that they execute rather than fault.

**They should not be present at all.** These are ARMv6T2 encodings and the local
archives are Thumb-1, compiled years earlier. They come from the archive having
been *patched*: a cracker removes a branch by writing a two-byte `nop` over it, and
`0xbf00` is what an assembler emits for one. A title whose authentication had been
removed that way hit it in its own loading path, and the core reported an undefined
instruction in the middle of an ordinary basic block.

The rest of the `0xbfxx` space is `IT`, which is not a hint: it changes how the one
to four following instructions execute. Running it as a no-op would run a
conditional body unconditionally, so it stays undefined and says so.

A32 halfword and signed transfers support immediate and register offsets, pre/post
indexing and writeback. Halfword accesses clear address bit zero, matching the
ARMv4T behavior of the original execution core, while signed byte loads keep the
complete byte address.

Tests execute newly authored A32 and Thumb programs, including base-in-list
load/store multiple transfers and the exact shape of the original KTF SVC stub.

<a id="implementation-the-backend-seam-and-the-corpus-that-guards-it"></a>

### The backend seam, and the corpus that guards it

`Engine` used to be the implementation rather than an implementation of anything:
`Core` held one by its concrete type and called it. That left nowhere to put a
second execution strategy and, more importantly, nowhere to run one beside the
first and ask whether the two agree.

`Backend` is that seam, one method wider than the call `Core` already made:

```go
type Backend interface {
	Run(context *Context, memory *Memory, end uint32, count uint32) (RunResult, error)
	Name() string
}
```

`CoreOptions.Backend` and `CoreOptions.BackendName` are the only injection point,
and `RegisterBackend` is a process-wide registry, so a strategy that builds only on
some targets can register itself from a build-tagged file without anything
importing it elsewhere. A name nothing registered resolves to the interpreter rather
than failing, because a build without the strategy a configuration asks for still
has to run the game; `Core.BackendName` reports which one it got. Nothing outside
the package asserts a `Backend`'s concrete type, since a runtime that did could not
run a second strategy.

The interface call is once per quantum (a thousand instructions on KTF, sixteen
thousand on LGT), so it does not appear in any benchmark above.

Three rules come with the seam. The interpreter is the oracle and stays. A backend
is a black box only between synchronisation points: where it returns (a supervisor
call, a spent budget, a fault, the end address), all seventeen guest-visible words,
every byte of mapped memory and the whole `RunResult` must be exactly what the
interpreter would have left. `RunResult.Steps` is part of that, not a diagnostic,
because a Host paces frames and detects a runaway guest by it: a strategy that
retires an instruction without counting it moves the guest's own clock. "The charge
a quantum could not hold" above is the same lesson from the other side.

<a id="implementation-what-the-corpus-asks"></a>

#### What the corpus asks

`internal/armcore/conformance` is the corpus and its harness. Each case sets up
registers, condition flags, an instruction image and a scratch window, runs one
backend, and compares the whole result: sixteen registers and CPSR, the scratch
bytes, and the stop reason, retired count and supervisor-call immediate.

**The expectations are the architecture's, not a recording.** Every case names the
rule it exists for, and its `Want` is worked out from that rule. A corpus that
records the answer it was given can only catch a change; one written from the
manual can also catch the current answer being wrong. Twenty-one cases cover the
edges an execution strategy gets wrong:

| edge | what the case pins |
|---|---|
| flag survival | a carry from `ADDS` is still there for a later `ADC`, across an instruction that writes no flag |
| ADC chain | the two halves of a 64-bit add |
| shifter carry, immediate | `LSR #0` and `ASR #0` mean 32; `ROR #0` is RRX and rotates through the carry |
| shifter carry, register | a shift of exactly 32, one past 32, and one of zero, which must not touch the carry at all |
| multiply | `MULS` writes N and Z and leaves the caller's C and V alone |
| condition codes | all fourteen against one compare's flags, in one case, with the failing ones still retiring |
| LDM/STM base in list | the base written back before the transfers: a store of the lowest register still sends the original base, and a load of the base keeps what memory gave it |
| R15 read as data | instruction + 8 in ARM, instruction + 12 out of `STM`, and word-aligned instruction + 4 for the Thumb PC-relative forms |
| ARM/Thumb | `BX` in both directions on bit 0 of the target |
| transfers | the six load and store widths, sign extension, and pre- and post-indexed writeback |
| run results | a supervisor call's immediate, address and resume point, and a spent budget stopping on exactly the instruction it ran out at |

With one backend registered, the corpus is a regression net against the
architecture. The test also compares every registered backend against every other,
so a second backend makes it a differential test with no further work, including
the case where both are wrong the same way, which is checked separately from the
case where one is wrong.

The corpus holds no loops. A backward Thumb branch is where the loop recognisers
(`fill_loop.go` and the later shapes) stand in for a whole loop and charge for every
instruction it would have retired, so a case whose expected retired count came from
a stand-in would pin the stand-in rather than the architecture.
`step_charge_test.go` pins what the stand-ins retire.

<a id="implementation-deliberately-incomplete"></a>

### Deliberately incomplete

Superseded in part: debugger hooks, profiling samples and write watches exist; see
[armcore.md](../armcore.md#diagnostics-and-performance).

- architectural exception modes, banked registers, interrupts, and exception
  return through SPSR
- privileged PSR/SPSR transfers and coprocessor instructions
- remaining architecturally unpredictable edge cases
- cycle timing, debugger hooks, profiling samples, and cheat read/write watches
- instructions and edge semantics first reached by real KTF AOT method bodies,
  the JVM bridge, and later lifecycle code

The ignored real games under `var/games/ktf` are reached through acceptance probes.
All 32 clients at that revision completed their bounded Thumb entry paths,
including self-relocation, returned validated `WIPI_exe`, `ExeInterface` and
function table pointers, and finished both returned initialization functions. The
slowest WIPI initializer takes 8,330,820 instructions, so this explicit probe uses a
ten-million-instruction ceiling without changing the runtime's
one-million-instruction default. No additional instruction encoding was required by
these initializers, the main-class constructors or the `startApp` bodies: all 32
real main classes load, complete their constructors, and return normally from
`startApp` against the KTF runtime surface of that revision.

Games are now played through this subset rather than only started (display, input,
timers and persistence all sit on it), so what the subset lacks is found by a title
stopping somewhere in a sweep of a whole local set rather than by a probe or this
list; see [`testing.md`](testing.md#implementation-testing). The list above is what
stays deliberately absent regardless.
