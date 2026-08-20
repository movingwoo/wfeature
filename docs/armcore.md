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

The same warning caught the investigation below, from the other end. `-profile`
prints a line per *region*, and a region is a run of sampled addresses with no
large gap in it — `regionIndex` in `internal/guestprofile`. Several loops in one
function land on one line, so a share read off it is the share of everything
that ran there, not of a loop. One region reading 43.59% was four loops, and the
one that looked like the answer turned out to be 0.18% of it. `-profile-folded`
answers per address, and that is what a share should be taken from.

## Standing in for the loops a rasteriser is made of

The measurement above says a pattern engine for *copy* loops would be machinery
for something the corpus barely contains. It also says what the corpus does
contain: fills and blits, 34 fill-shaped loops to 8 copy-shaped ones, and the
hot ranges in every title profiled are the game's own rasteriser. What was left
open was that matching those needs a loop recogniser rather than a byte
pattern, because two titles doing the same fill share no instruction sequence.

A user reporting a specific title as slow is the condition this section named
for revisiting it, and one arrived. Per address, in the scene reported:

| share | loop |
|---|---|
| 28.6% | a blit through a palette: a source byte indexes a table, the colour goes out sixteen bits at a time |
| 2.2% | the row loop around a fill |
| 1.9% | a second, guarded blit of the same shape |
| 0.18% | a constant halfword fill |

So the recogniser was built for the two shapes that are actually there, in
`fill_loop.go` and `table_blit.go`; two more followed once those had been
measured, in `byte_blend.go` and `word_modulate.go`. All of them are structural:
they read the body between a backward branch and its target, give each
instruction a role, and refuse a body where a role is filled twice, left empty,
or played by a register that is also playing another. Recognition is attempted only when a backward
branch is taken, so the cost on every other instruction in the program is one
compare.

Four rules keep a stand-in from being something the guest can tell apart:

- **Spans are validated whole before anything moves.** A blit that would have
  faulted partway faults where it would have, rather than after the stand-in
  has filled everything before it.
- **Only refusals are cached.** A positive answer is re-derived every time the
  loop is stood in for — six to twelve decode-cache reads against the thousands
  of pixels they authorise — because a cache of positives would have to be
  invalidated whenever the guest rewrites code, and this platform rewrites it.
  A run that merely declines is not a refusal: caching "too few iterations this
  time" would write the loop off for the session.
- **Registers and flags are left where the guest would have left them**, which
  `fill_loop_test.go` pins by running the same loop both ways and comparing all
  sixteen registers, the CPSR, and the memory it wrote.
- **The step count is charged as though every instruction had run**, so a guest
  that would have exhausted its budget still does and `MaxSteps` keeps meaning
  what it meant. The cost of that is a profile artifact: the charged steps are
  sampled at the PC after the loop, so the instructions around a stood-in loop
  read as hot. Measure a stand-in by the share of the loop body itself, which
  goes to nothing, and by the wall clock.

Measured on the title that prompted it, over the same 1,790-tick route with the
same save:

| | before | after |
|---|---|---|
| busy | 21.8s | **18.0s** |
| per tick p90 | 41.0ms | **35.2ms** |
| per tick p99 | 46.7ms | **40.8ms** |
| the blit body's share of instructions | 28.6% | **0.71%** |

A tick of this title is about 44ms of guest time, so p90 moves from 1.07x real
time — no headroom at all — to 1.25x. Every one of the 30 local LGT archives
and all 35 KTF archives render byte-identical first frames before and after,
and the engine benchmarks are unchanged, the existing blit benchmark's loop
being a shape this does not match.

### A validated span does not have to be checked again per pixel

That first stand-in still called the sized accessors per pixel, which is why
28.6% of instructions became about 17% of time rather than all of it: each
pixel paid a thread-local test, a page lookup, a permission compare and a
bounds check for a span already proved reachable, once, whole.

`raw_span.go` is the way out. It answers the page's own bytes for as much of a
validated span as sits in one page, and both stand-ins index that slice — a
page of destination at a time, with the decode cache retired once per page
instead of once per store. **What keeps it honest is what it refuses**: a
memory with anything watched (a stand-in that skipped a watch would be the one
bug watchpoints exist to find), a span that could touch a thread-local word,
and a page with no storage committed. Each refusal falls back to the checked
path for that stretch — which also commits the page, so the next stretch is
usually direct — and an odd destination stays on the checked path outright,
because a halfword store aligns its address downward and the guest would be
writing over its own previous pixel.

Measured the same way, on the same route and save, against the same probe:

| | before | after |
|---|---|---|
| busy | 18.6s | **17.5s** |
| per tick p90 | 36.4ms | **35.1ms** |
| per tick p99 | 42.5ms | **40.4ms** |
| ns per instruction retired | 6.20 | **5.84** |

The instruction count is bit-identical across the arms, which is what makes
`ns_per_step` the comparison and not the tick time. On KTF, interleaved A/B on
two titles: one −3.2% on both pairs, one unmoved — the second's loops are not
shapes the recognisers match, so there was nothing there to make cheaper. All
30 local LGT and 35 KTF archives still render byte-identical first frames.

The same profile also showed the analysis allocating a five-entry map per
stand-in to check that no two roles share a register; it is a bit per low
register now.

### What a title spends its instructions on once the stand-ins are in

Same scene, per address, with the first two stand-ins running. The ranking is
worth reading whole, because not all of it is something a recogniser can reach:

| share | what it is |
|---|---|
| 34.6% | the region holding the stood-in blit. **This is the charging artefact**, not work: the steps are charged at the PC after the loop, and the loop itself is being run in Go |
| 13.2% + 7.6% | **the guest's own allocator** — a walk down a free list of block headers, and the three leaf helpers it calls per block |
| 7.3% | a modulate: two pixels packed in a word, each channel group masked, scaled by a byte from a third stream, two rows at a time |
| 7.2% | a byte blend: `if src[i] - k > dst[i] { dst[i] = src[i] - k }` |
| 4.6%, 4.1%, 2.9% | run-length sprite draws with a saturating additive blend per channel |

**The largest single consumer left is the game's own `malloc` and `free`**, at a
fifth of every instruction executed, and that is not ours: its cost is the
length of the list the title itself keeps, its loop calls out to helpers, and
standing in for it would mean reimplementing a heap whose layout the title
reads back.

How long that list is, counted rather than estimated: over 4,000 ticks of one
title driven through a route, the search was entered **9,535 times and its body
ran 33,253,303 times** — an average of **3,488 blocks walked per call**, 28
instructions each, which is 27% of every instruction the run charged. It is a
best-fit search over a singly linked list of block headers, so its cost is
linear in how many blocks the title is holding, and nothing on our side of the
boundary shortens it. The number is here so that a later "the emulator got
slower the longer it ran" report can be checked against it rather than guessed
at: if that list is longer than this, something is leaking into the title's own
heap.

**It is also not one number — it grows across a session**, and the growth is
step-wise at scene loads rather than smooth:

| ticks into the run | blocks walked per call |
|---|---|
| 50 – 200 | 1,242 – 1,257 |
| 250 – 300, the first scene load | 3,344 |
| 450 – 850 | 5,135 – 5,305 |
| 1,000 – 1,050, the second load | 5,317 |
| 1,150 – 1,450 | 5,654 – 6,151 |

Five times longer by the end than at boot, and never shorter. Each load adds
blocks the title does not give back, with a slow drift of a percent or two per
two hundred ticks in between. **Whether any of that is ours is not settled**:
the chain is the title's own, but a platform call that allocates a record in
the guest heap and never frees it would land in exactly this list and would
cost every allocation the title makes afterwards, because the search is linear
in the chain's length. Answering it needs a census — walk the chain from the
host at two points in a run and diff the blocks — not another profile.

Where this shows up is worth stating plainly, because it decides what the next
speed work is. Per fifty ticks of the same route, windows sorted by how much of
this loop they contain:

| the loop's iterations in the window | ns per step | share of the window's instructions the stand-ins covered |
|---|---|---|
| none | 3.7 – 4.2 | 55 – 65% |
| 100k – 400k | 4.3 – 4.6 | 51 – 54% |
| 500k – 900k | 4.9 – 5.8 | 33 – 46% |
| 9.7M (a scene load) | 6.7 | 15% |
| 18.1M (a scene load) | 7.8 | 4% |

**The expensive windows are the allocating windows**, and the ranking is
monotone in both columns. The stand-ins are not failing in those windows; there
is simply nothing there for them to stand in for. A scene a person reports as
slow is far more likely to be one of these than one the recognisers missed.

The two shapes below it were built — `byte_blend.go` and `word_modulate.go` —
and the three run-length draws were not. That is the line this section drew and
it is worth keeping: each of these is a *different arithmetic*, so a recogniser
for one does not read another, and the case for building one is how much of a
reported-slow scene it is rather than how general it looks. The run-length
draws are 64 instructions a pixel across three sites with saturation branches
per channel; they are the point where a recogniser stops being a shape and
starts being a rewrite of one title's sprite routine.

### Two more shapes, and what they cost to recognise

**A guarded byte blend** (`byte_blend.go`). Eleven instructions a byte, and the
first body with a branch in it: the store is skipped by a forward conditional
branch when the incoming byte does not win. The walk therefore gained a
positional rule — the compare, the guard and the store adjacent and in that
order, the guard landing on the instruction after the store — because a role
walk alone cannot say *what* a guard guards. Its charge is the first that is
not one number: a skipped store is one instruction less, and the test runs the
same loop both ways and compares the counts.

**A two-stream modulate** (`word_modulate.go`). Forty-two instructions a step
over two rows at once, and **matched as a sequence rather than as a walk**. A
role walk works when each instruction contributes one distinct effect; here the
same thirteen-instruction computation appears twice over two stream pointers,
and "the second half's third `ands`" is a position, not a role. So the body is
read in order, every register bound where it is first met and checked wherever
it recurs, every immediate and shift amount read out rather than assumed, and
the two halves required to agree in all but which stream they walk. Nothing is
matched by encoding — a title that allocated its registers differently would
bind different numbers and still be recognised — but the sequence is one
title's blend and the trade is written down here rather than implied.

Both keep the existing rules, and both add one of their own:

- **What the loop re-reads every step must be somewhere it does not write.**
  The blend's constant lives in a frame slot; the modulate re-reads its two
  masks out of its own code and its factor pointer out of the frame. A
  stand-in reads each once, so a destination span covering any of them is
  refused rather than modelled.
- **The last iteration is left to the interpreter.** The stand-in runs all but
  one of the iterations that remain and leaves the loop pointing at the last,
  which the engine then interprets. That is what makes the scratch registers
  right for free: both bodies leave several values in registers the code after
  the loop reads, and reproducing each of them by hand is exactly the class of
  mistake that does not show up until one title's shadows are wrong. The flags
  come out right for the same reason. One interpreted iteration in a thousand
  costs nothing.

Recognition is attempted when a taken backward branch reaches no further than
the longest body any recogniser reads, which these raised from 32 bytes to 84.
The cost of that is one analysis per loop head inside the new range that is not
one of these shapes, paid once because refusals are remembered; measured
against two KTF titles, interleaved, it is inside the noise.

Measured on the title that prompted all of this, same route, same save, same
probe:

| | no stand-ins | blit and fill | + raw spans | + blend | + modulate |
|---|---|---|---|---|---|
| busy | 22.1s | 18.6s | 17.5s | 16.2s | **14.6s** |
| tick p90 | 41.5ms | 36.4ms | 35.1ms | 30.3ms | **24.1ms** |
| tick p99 | 47.0ms | 42.5ms | 40.4ms | 38.7ms | **33.9ms** |

A tick of this title is about 44ms of guest time, so p90 goes from 1.06x real
time — no headroom at all — to **1.83x**. The check that matters more than the
clock: **every one of the 770 frames the route paints is byte-identical**
before and after, at the same tick numbers, and the whole-corpus first-frame
sweep is unchanged on all 30 local LGT and 35 KTF archives.

**Compare stand-ins by busy time, not by `ns_per_step`.** A quantum that ends
inside a stand-in reports the quantum's size rather than what the stand-in
charged — `Engine.Run` answers `count` on the exhausted path — so the steps a
big charge overshoots by are lost from the accounting. It is the safe direction
for a budget and it is invisible within one build, but across builds a
recogniser that stands in for more loses more, which reads as a throughput gain
that is partly an accounting one.

### The same fill, written four ways

The recognisers above were built against the title a person reported as slow,
and the question they left open is how much of the rest of the corpus they
reach. Answering it needs no profile: the hook already knows which backward
branches it refused, so counting refusals per loop head over a boot of every
local archive ranks each title's largest un-standable loop. Thirty archives,
600 ticks each, share of the title's own instructions in the top loop:

| share of everything the title executed | how many archives |
|---|---|
| over 60% | 5 |
| 30 – 60% | 4 |
| 15 – 30% | 8 |
| under 15% or nothing hot | 13 |

**Five titles spend more than sixty percent of every instruction in a single
loop the recognisers refuse**, which is a stronger statement than the profile
of one scene could make. Disassembling the top loops turned three of them into
the same three instructions the counted fill already stands in for, refused
over how the compiler asked whether the count had run out:

| the loop, as the title emits it | share | why it was refused |
|---|---|---|
| `subs rC,#1 / strh rV,[rP] / adds rP,#2 / cmp rC,#0 / bne` | 19% | the count is tested against zero instead of read out of the subtraction's borrow |
| `subs rC,#1 / strh rV,[rP,rI] / adds rP,#2 / cmp rC,#0 / bne` | 29% | that, and the store reaches its destination through a base and an index |
| `mov rV,sp / ldrh rV,[rV,#4] / adds rC,#1 / strh rV,[rP] / adds rQ,#1 / adds rP,#2 / cmp rC,rL / ble` | 69% | the count runs up to a limit in a register, and a second register runs up beside it |

The first two were built. The ending is now a property the walk reads rather
than a fixed instruction order — `subs`+`bhs` runs one more time than the
counter says and leaves it at -1, `cmp #0`+`bne` runs exactly the counter and
leaves it at 0 — and the store may carry an index the body is required never to
touch. Measured on the same 1,500 ticks per title:

| | before | after |
|---|---|---|
| the 19% title | 13.85s busy | 11.41s (**−17.6%**) |
| the 29% title | 3.68s busy | 2.59s (**−29.8%**) |
| two titles whose loops are neither form | unchanged | unchanged |

**The third was not built, and it is the largest number in the table.** It
needs the counter to be allowed to run *up* against a limit held in a register,
with a signed branch, and it needs any low register whose only effect in the
body is `adds rD, #k` to be advanced by `k` times the iteration count rather
than refused as a second counter. That is a real generalisation — an induction
variable rather than one named pointer and one named counter — and it would
subsume the pointer advance the analyser special-cases today. The evidence for
doing it is in the table above: one archive at 69%, and its duplicate, and a
second loop in the 29% title at 28%.

What the sweep also says is that the remaining titles are *not* all fills. Two
of the five over sixty percent are bit-test loops over a packed structure, and
the shared sprite routine three titles carry is 29 – 49% of each of them. Each
is a different arithmetic, and the rule from the section above still holds: the
case for building one is how much of a reported-slow scene it is, not how
general it looks. The difference this sweep makes is that the share is now
known before anyone reports anything.

### What the hook costs where it never fires

The recogniser is reached from one compare on every taken conditional branch,
and the tables above measure it where it fires. What it costs where it does
*not* is a different question, and it is the one a person asks after a scene
the recogniser cannot reach: **a title sitting on its own menus takes a
backward branch every seventeen instructions**. Counted over 1,500 ticks of two
titles idling on a title screen and its menus, with no stand-in of any kind:

| | backward branches | guest instructions | branches per instruction | stand-ins |
|---|---|---|---|---|
| one title's title screen | 105,556,869 | 1,945,417,595 | 1 in 18.4 | 0 |
| another's | 18,814,133 | 303,944,403 | 1 in 16.2 | 0 |

Every one of those reached the refusal cache, which was a `map[uint32]bool`
keyed by the loop head. Measured against a build with the hook compiled out —
four titles, three interleaved runs each, on their own title screens and menus
where no stand-in ever fires:

| ns per step, title screen | hook compiled out | refusal in the decode entry | refusal in a map |
|---|---|---|---|
| A | 7.02 | 7.20 | 7.42 |
| B | 11.11 | 11.60 | 11.44 |
| C | 9.07 | 9.31 | 9.53 |
| D | 7.50 | 7.64 | 7.65 |

**The hook costs 2 to 6% of a run it never fires in.** Moving the refusal into
the branch's own decode-cache entry — the padding byte beside the form, so the
entry is still four bytes — returns about half of that on two of the four and
nothing on the other two, one of them slightly the wrong way inside its own
spread. It is kept for a reason the table does not show: a map keyed by the
loop head is never invalidated, so a title that rewrites a page inherits the
old code's refusal, while an answer riding on the decode entry is dropped with
the entry when the page's instructions change.

What is worth carrying out of this is the shape of the trade rather than the
half point it returned: **the hook is not free on code it cannot help, and the
code it cannot help is the code a person is looking at while nothing is
moving** — menus, text, dialogue. A change here is judged on both scenes or it
is judged on half of one.

The same measurement answers a report that "the game got faster but the menus
got slower". Per fifty ticks of one title driven through a route, before the
stand-ins and after:

| what the ticks are | before, ns/step | after, ns/step |
|---|---|---|
| boot and vendor splashes | 8.2 – 13.3 | 8.0 – 9.0 |
| the scene load | 6.6 | 6.9 |
| walking and drawing the map | 7.3 – 7.8 | 3.5 – 4.6 |

Nothing halved twice and nothing doubled: the drawing halved and the menus did
not move. The menus were always the expensive code per instruction — they
interpret every one of theirs — and they only *read* as a regression once the
scene beside them stopped being. A stand-in that reaches one scene widens the
gap between two scenes, and the gap is what a person feels.

### The block interpreter, measured before building it again

`TODO.md` left the interpreter's remaining dispatch overhead as "size unknown,
measure with a small benchmark before starting". Measured, on the game-shaped
loop, as the *ideal* a block interpreter could reach — the PC read, the Thumb
test, the `end` compare, the watch compare and the decode-cache lookup all
gone, the same handlers behind the same switch, and the PC write kept because
an instruction may still read it:

| inner loop | ns per instruction |
|---|---|
| `Engine.Run` as it is | 5.26 |
| ideal blocks of 8 instructions, one lookup per block | 4.96 (−5.8%) |
| ideal blocks of 16 | 4.94 (−6.1%) |
| ideal blocks of 32 | 4.76 (−9.5%) |
| one ideal block of 160, no lookup at all | 4.11 (−22%) |
| no dispatch whatsoever — every handler called straight | 3.20 (−39%) |

Real blocks are 8.5 instructions, or 15.6 with `mov ip, rN` split at decode
time (see the six arrangements above). **So the ceiling at real block lengths
is about 6%, on a benchmark that pays none of what a real one pays** — no
block building, no invalidation, no early exits, and no register pressure on
the ordinary path. The six arrangements that were actually built returned 5 to
9% on the code they targeted and cost 1 to 10% everywhere else, which is the
same number arriving from the other side. **This is settled: the design cannot
pay for itself, and the benchmark that says so is `BenchmarkEngineGameShapedLoop`
against the ideal loop, not another arrangement.**

A cheaper variant of the same idea was tried and dropped with it: routing five
more Thumb forms — literal load, high register, push, pop, multiple transfer —
straight from the engine's switch instead of through `executeThumbForm`, which
a profile put at 2.2% of a run in second dispatch. Interleaved A/B, three pairs
each: **−1.2% on an LGT title, +0.6% on a KTF one.** The hot switch growing
costs the ordinary path about what the second dispatch cost the forms that took
it, which is the same lesson the block arrangements taught.

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

### The host profile's next answer was a build flag, and it was 12%

The section above says to ask for a host profile before any throughput
reasoning. Asked again on a Clet — `WFEATURE_PERF_ROUTE` through a field scene
of the title a person reported as slow — it answered something that is not an
interpreter change at all:

| | share of host time |
|---|---|
| `armcore.Engine.Run`, everything under it | **93.3%** |
| of which `Engine.Run` itself — fetch, the watch and end compares, the switch | 23.5% |
| of which `decodedThumbFast` | 17.2% |
| of which every stand-in together (`runStoreLoop`) | 8.1% |
| `syncToGuest` + `syncFromGuest` | **0%** |

**This is the opposite title to the one above**, and the two rows that say so
are the first and the last. A Clet is 99.8% Thumb and does not go through the
framebuffer syncs at all, so here the interpreter really is the run — which is
what makes a change to *how Go compiles the interpreter* worth anything.

Go reads a pprof CPU profile from `default.pgo` beside a main package and needs
no flag to do it. There is now one at `cmd/server/default.pgo` and
`cmd/cli/default.pgo`, and it is a real run rather than a benchmark.

| same route, same save, one machine (Ryzen 5 3600, linux/amd64) | before | after |
|---|---|---|
| an LGT Clet in-game, busy | 27.89s | **24.44s** (−12.4%) |
| its `ns_per_step` | 8.45 | **7.22** (−14.6%) |
| its tick p90 | 37.09ms | **31.52ms** (−15.0%) |
| its tick p99 | 64.06ms | 56.48ms (−11.8%) |
| a KTF title, a route to a scene | 20.78s | **18.19s** (−12.5%) |

**All 883 frames the LGT route paints are byte-identical** before and after,
at the same tick numbers. That is the whole of the safety argument: PGO changes
inlining and code layout and nothing a guest can observe.

**The profile is three runs merged, and that is not tidiness.** The first one
taken was a single Clet, and it moved that Clet by 14.5% and the KTF title by
only 7.0% — the compiler had been handed one title's shape and optimised the
other two at its guess. `Engine.Run` has two halves that do not overlap
(`executeARM` never appears in a Clet's profile at all), so the committed
profile is a Clet in-game, an AOT-compiled LGT Java title in-game, and a KTF
archive, symbolised separately and merged. That nearly doubled the KTF number
and cost the Clet two points, which is the trade worth making: **a profile that
covers one path is worse than no profile for the paths it does not cover.**

`make pgo` regenerates it. It needs local archives and routes, which are under
`var/` and are not in the repository, so it runs only on a machine that has
them; the three it uses are named in the Makefile so that what produced the
committed file is not a guess. **There is no SKT run in it** — that platform
has no load probe, and its interpreter is `internal/jvm` rather than this one,
so it is neither covered nor harmed.

Two things this does not do, both worth stating because they are what a reader
will assume.

- **It does not lower a session's CPU.** A session that cannot keep up runs
  with `TickFor`'s wait pinned at zero and therefore at 100% of a core, and a
  cheaper tick is spent on more guest speed rather than on idling. The CPU
  falls below one core at exactly the moment the game reaches its own clock and
  not before. See `session.md`.
- **It does not survive the code it was taken from.** A profile is a build
  input that ages: it names functions, and a rename or a split silently drops
  that function's weight. Re-take it when the engine's shape changes, and read
  the table above as the measurement to repeat rather than a number to trust.

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

### The third form of translator, measured — a gate rather than a plan

The section above closes two of the three shapes a translator can take: a
closure per instruction loses on its own arithmetic, and one that emits machine
code is out on how this ships. **It does not measure the third**, which is the
one that would matter — registers allocated to the host's, a compare feeding a
branch instead of writing an NZCV nothing reads, and memory reached through an
inline page check. That form attacks what an instruction *costs* rather than
what finding it costs, and the numbers in this document are all about the
latter.

`translator_gate_bench_test.go` is that measurement, and it is built to be able
to **refuse** the design rather than to approve it. What it runs is one real
block chain, not a synthetic loop: the run-length token decoder a local LGT
title spends its interpreted time in, at the address it occupies in its own
module so its literal-pool loads resolve where they really do. A host profile
of that title's field scene puts 93.3% of the run inside `Engine.Run`, and the
largest thing still interpreted inside it — the stand-ins already cover the
pixel loops on either side — is this decoder's six addresses. One transparent
token costs seventeen instructions, which `TestTokenDecodeCostsSeventeenSteps`
holds to the code rather than to this paragraph.

**The bar is 2.4x, and it comes from Amdahl rather than from taste.** Of that
title's host time, 8.1% is already in stand-ins and 6.7% is outside the core,
leaving 85.2% a translator could reach. The session needs 1.96x to reach its
own clock, and 1/(0.148 + 0.852/N) = 1.96 at N = 2.4. The ceiling measured in
"The block interpreter, measured before building it again" — every trace of
dispatch gone, 5.26ns to 3.20ns — is N = 1.64, which is **1.50x on the run and
therefore short**. That is the whole reason the third form had to be measured
separately: the first two cannot reach the bar even in the limit.

Two models were run, and both are required to leave the interpreter's own
registers and to have charged its own step count
(`TestTranslatedTokenDecodeMatchesTheInterpreter`), because a benchmark against
a faster thing that computes something else measures nothing.

| ns per guest instruction, five runs, Ryzen 5 3600 | | against the interpreter |
|---|---|---|
| interpreted | 9.746 | — |
| interpreted, stream in the literal pool's page (diagnostic) | 8.748 | — |
| translated, literals loaded every token | **0.167** | **58x** |
| translated, literals folded into immediates | **0.123** | **79x** |

**The gate does not refuse it, and the margin is what makes that worth
believing.** The model is an upper bound by construction — it has no block
lookup, no block entry or exit, no deoptimisation edge, and it is Go rather
than emitted code — so the honest reading is not "a translator would be 58x".
It is that **the model would have to be twenty-five times too generous before
this block failed the bar**, and no amount of care about block overhead moves a
number by that.

What it does *not* say is worth as much. Amdahl caps the same title at 6.8x
however fast translation gets, the block chosen is the one most favourable to
translation in the title (seventeen guest instructions collapse to about nine
host ones because most of them are redundant — two literal loads that fold, and
flag writes nothing reads), and the stand-ins stay above a translator in the
order things must be tried: they are six times cheaper per instruction than
interpretation because a recognised fill is a slice fill, where compiled code is
still one host store per guest store. **A translator must not swallow a loop a
recogniser can take.**

So the decision this leaves is a cost decision and no longer a throughput one:
two code generators, W^X on three operating systems — `MAP_JIT` and a hardened
runtime entitlement on the one this would most want to run on, which is not a
signed build today — and the seven things the interpreter carries that compiled
code would have to keep carrying: the step charge `SetStepBudget` and KTF's
scheduler are built on, the supervisor-call boundary and its lock, `Core.Watch`,
the debugger and the GDB stub, a page's compiled code dying with its decode
cache when its bytes change, the recogniser ordering above, and the frame
byte-identity every change here is judged by.

The diagnostic row is not part of the verdict, and it turned into the section
below. This block reads two literals out of its code page and two bytes out of
the sprite stream every token; `Memory` keeps instruction fetch on a page of its
own but has one data page, so those two evict each other and every access takes
`pageFor`. Giving the stream the pool's page is worth 10.2% here — which is a
second data slot's ceiling on this block, and looked like the cheap change the
translator is not.

### The second data slot was built, both ways, and lost

That 10.2% is a ceiling on one block, and the two cache changes above are what a
number like it has to be checked against. It was built twice and it loses twice,
which is the third time this document has written that sentence.

**Both slots checked in `mappedPage`.** `mappedPage` exists to be inlined — its
own comment says reaching the general path costs a Go call and the general path
then costs two more — and a second slot puts it over the budget:

```
cannot inline (*Memory).mappedPage: function too complex: cost 88 exceeds budget 80
```

| interleaved, same session | before | after |
|---|---|---|
| the token decoder above | 9.745ns | 9.012ns (**−7.5%**) |
| `BenchmarkEngineGameShapedLoop` | 7.758ns | 8.012ns (**+3.3%**) |
| `BenchmarkEngineLoadStoreLoop` | 10.943ns | 11.153ns (+1.9%) |

The block the slot was for gains 7.5% and the shape 97% of a measured title
executes loses 3.3%. That is the trade refused everywhere else in this section.

**The second slot in `pageAt` only**, leaving the inlined path exactly as it
was. This one is clean on the benchmarks — `mappedPage` still inlines,
game-shaped moves −0.2% and load/store −0.2%, and the token decoder gains 5.4%.
It still loses, and only a real title says so:

| interleaved, both PGO builds | before | after |
|---|---|---|
| an LGT Clet in-game, busy | 23,856ms | 23,677ms (−0.75%, inside a 1.6% spread) |
| a KTF title, a route to a scene | 17.935s | 18.44s (**+2.8%**) |

The Clet's 5.4% on the benchmark arrives as nothing on the title, because the
decoder is a minority of its interpreted time and the stand-ins already own the
pixel loops on either side of it. The KTF regression is the consistent number
of the four runs, not the noisy one: that platform crosses the Java bridge
constantly and misses both slots, so it pays the second compare, the sixteen
bytes and the shift on every miss and is never paid back.

**A benchmark built to isolate a block reports that block's ceiling, and a
ceiling on a block is not a share of a run.** That is the same mistake as
reading a guest profile for a host one, arriving from the other direction, and
it is what the real-title A/B is for.

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
